package steprunner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/acorn-io/broadcaster"
	"github.com/acorn-io/z"
	"github.com/adrg/xdg"
	"github.com/gptscript-ai/clicky-chats/pkg/agents"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"github.com/gptscript-ai/gptscript/pkg/loader"
	gptopenai "github.com/gptscript-ai/gptscript/pkg/openai"
	"github.com/gptscript-ai/gptscript/pkg/repos/runtimes"
	"github.com/gptscript-ai/gptscript/pkg/runner"
	"github.com/gptscript-ai/gptscript/pkg/server"
	"github.com/gptscript-ai/gptscript/pkg/types"
	"github.com/gptscript-ai/gptscript/pkg/version"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const minPollingInterval = time.Second

type Config struct {
	PollingInterval         time.Duration
	APIURL, APIKey, AgentID string
}

func Start(ctx context.Context, gdb *db.DB, cfg Config) error {
	a, err := newAgent(gdb, cfg)
	if err != nil {
		return err
	}

	a.builtInToolDefinitions, err = populateTools(ctx)
	if err != nil {
		return err
	}

	return a.Start(ctx)
}

type agent struct {
	pollingInterval time.Duration
	id, apiKey, url string
	client          *http.Client
	db              *db.DB

	builtInToolDefinitions map[string]types.Program
}

func newAgent(db *db.DB, cfg Config) (*agent, error) {
	if cfg.PollingInterval < minPollingInterval {
		return nil, fmt.Errorf("polling interval must be at least %s", minPollingInterval)
	}

	return &agent{
		pollingInterval: cfg.PollingInterval,
		client:          http.DefaultClient,
		apiKey:          cfg.APIKey,
		db:              db,
		id:              cfg.AgentID,
		url:             cfg.APIURL,
	}, nil
}

func (a *agent) Start(ctx context.Context) error {
	caster := broadcaster.New[server.Event]()
	oaClient, err := gptopenai.NewClient(gptopenai.Options{
		APIKey:  a.apiKey,
		BaseURL: a.url,
	})
	if err != nil {
		return err
	}
	noCacheRunner, err := runner.New(oaClient, runner.Options{
		MonitorFactory: server.NewSessionFactory(caster),
		RuntimeManager: runtimes.Default(filepath.Join(xdg.CacheHome, version.ProgramName)),
	})
	if err != nil {
		return err
	}

	go func() {
		caster.Start(ctx)
		defer caster.Shutdown()

		sub := caster.Subscribe()
		defer sub.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-sub.C:
				slog.Info("Got event", "event", event)
			}
		}
	}()

	// Start the "job runner"
	go func() {
		for {
			if err := a.run(ctx, noCacheRunner); err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					slog.Error("failed step runner iteration", "err", err)
				}

				select {
				case <-ctx.Done():
					return
				case <-time.After(a.pollingInterval):
				}
			}
		}
	}()

	return nil
}

func (a *agent) run(ctx context.Context, runner *runner.Runner) (err error) {
	slog.Debug("Checking for a run")
	// Look for a new run and claim it. Also, query for the other objects we need.
	run, runStep := new(db.Run), new(db.RunStep)
	err = a.db.WithContext(ctx).Model(run).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("system_status = ?", "requires_action").Where("system_claimed_by IS NULL OR system_claimed_by = ?", a.id).Order("created_at desc").First(run).Error; err != nil {
			return err
		}

		thread := new(db.Thread)
		if err := tx.Model(new(db.Thread)).Where("id = ?", run.ThreadID).First(thread).Error; err != nil {
			return err
		}

		// If the thread is locked by another run, then return an error.
		if thread.LockedByRunID != run.ID {
			return fmt.Errorf("thread %s found to be locked by %s while processing run %s", run.ThreadID, thread.LockedByRunID, run.ID)
		}

		if err := tx.Model(runStep).Where("run_id = ?", run.ID).Where("type = ?", openai.RunStepObjectTypeToolCalls).Where("runner_type = ?", agents.GPTScriptRunnerType).Order("created_at asc").First(runStep).Error; err != nil {
			return err
		}

		if err := tx.Model(run).Where("id = ?", run.ID).Updates(map[string]interface{}{"system_claimed_by": a.id, "system_status": "in_progress"}).Error; err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to get run: %w", err)
		}
		return err
	}

	l := slog.With("run_id", run.ID, "run_step_id", runStep.ID, "type", "system_run_step")

	defer func() {
		if err != nil {
			failRunStep(l, a.db.WithContext(ctx), run, runStep, err, openai.RunObjectLastErrorCodeServerError)
		}
	}()

	stepDetails := z.Pointer(runStep.StepDetails.Data())
	toolCalls, err := extractToolCalls(stepDetails)
	if err != nil {
		return fmt.Errorf("failed to get run step function calls: %w", err)
	}

	for i, tc := range toolCalls {
		functionName, arguments, err := determineFunctionAndArguments(tc)
		if err != nil {
			return fmt.Errorf("failed to determine function and arguments: %w", err)
		}

		prg, ok := a.builtInToolDefinitions[functionName]
		if !ok {
			tool := new(db.Tool)
			if err = a.db.WithContext(ctx).Model(tool).Where("id = ?", functionName).First(tool).Error; err != nil {
				return fmt.Errorf("failed to get tool %s: %w", functionName, err)
			}

			prg, err = loader.ProgramFromSource(ctx, string(tool.Program), "")
			if err != nil {
				return fmt.Errorf("failed to load program for tool %s: %w", functionName, err)
			}
		}

		output, err := runner.Run(server.ContextWithNewID(ctx), prg, os.Environ(), arguments)
		if err != nil {
			return fmt.Errorf("failed to run: %w", err)
		}

		if err = db.SetOutputForRunStepToolCall(&tc, output); err != nil {
			return err
		}

		toolCalls[i] = tc
	}

	if err := stepDetails.FromRunStepDetailsToolCallsObject(openai.RunStepDetailsToolCallsObject{
		ToolCalls: toolCalls,
		Type:      openai.RunStepDetailsToolCallsObjectTypeToolCalls,
	}); err != nil {
		return err
	}

	if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Update the run step with a fake output
		if err := tx.Model(runStep).Where("id = ?", runStep.ID).Updates(map[string]interface{}{"status": openai.RunStepObjectStatusCompleted, "completed_at": z.Pointer(int(time.Now().Unix())), "step_details": datatypes.NewJSONType(stepDetails)}).Error; err != nil {
			return err
		}

		if err := tx.Model(run).Where("id = ?", run.ID).Updates(map[string]interface{}{"system_status": nil, "status": openai.RunObjectStatusQueued}).Error; err != nil {
			return err
		}

		return nil
	}); err != nil {
		l.Error("Failed to update run step", "err", err)
		return err
	}

	return nil
}

// populateTools loads the gptscript program from the provided link and subtool.
// The run_step agent will use this program definition to run the tool with the gptscript engine.
func populateTools(ctx context.Context) (map[string]types.Program, error) {
	builtInToolDefinitions := make(map[string]types.Program, len(agents.GPTScriptDefinitions()))
	for toolName, toolDef := range agents.GPTScriptDefinitions() {
		if toolDef.Link == "" || toolDef.Link == agents.SkipLoadingTool {
			slog.Info("Skipping tool", "name", toolName)
			continue
		}

		prg, err := loader.Program(ctx, toolDef.Link, toolDef.Subtool)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize program %q: %w", toolName, err)
		}

		builtInToolDefinitions[toolName] = prg
	}
	return builtInToolDefinitions, nil
}

func failRunStep(l *slog.Logger, gdb *gorm.DB, run *db.Run, runStep *db.RunStep, err error, errorCode openai.RunObjectLastErrorCode) {
	l.Debug("Error occurred while processing run step, failing run", "err", err)
	runError := &db.RunLastError{
		Code:    string(errorCode),
		Message: err.Error(),
	}
	if err := gdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(run).Where("id = ?", run.ID).Updates(map[string]interface{}{
			"status":        openai.RunObjectStatusFailed,
			"system_status": openai.RunObjectStatusFailed,
			"failed_at":     z.Pointer(int(time.Now().Unix())),
			"last_error":    datatypes.NewJSONType(runError),
			"usage":         run.Usage,
		}).Error; err != nil {
			return err
		}

		if err := tx.Model(runStep).Where("id = ?", runStep.ID).Updates(map[string]interface{}{
			"status":     openai.RunStepObjectStatusFailed,
			"failed_at":  z.Pointer(int(time.Now().Unix())),
			"last_error": datatypes.NewJSONType(runError),
			"usage":      runStep.Usage,
		}).Error; err != nil {
			return err
		}

		return tx.Model(new(db.Thread)).Where("id = ?", run.ThreadID).Update("locked_by_run_id", nil).Error
	}); err != nil {
		l.Error("Failed to update run", "err", err)
	}
}

func extractToolCalls(runStepDetails *openai.RunStepObject_StepDetails) ([]openai.RunStepDetailsToolCallsObject_ToolCalls_Item, error) {
	// Extract the tool call
	details, err := db.ExtractRunStepDetails(*runStepDetails)
	if err != nil {
		return nil, err
	}
	toolCallDetails, ok := details.(openai.RunStepDetailsToolCallsObject)
	if !ok {
		return nil, fmt.Errorf("run step is not a tool call")
	}

	return toolCallDetails.ToolCalls, nil
}

func determineFunctionAndArguments(toolCall openai.RunStepDetailsToolCallsObject_ToolCalls_Item) (string, string, error) {
	info, err := db.GetOutputForRunStepToolCall(toolCall)
	if err != nil {
		return "", "", err
	}

	return strings.TrimPrefix(info.Name, agents.GPTScriptToolNamePrefix), info.Arguments, nil
}
