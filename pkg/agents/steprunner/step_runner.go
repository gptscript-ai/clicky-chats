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
	"sync"
	"time"

	"github.com/acorn-io/broadcaster"
	"github.com/acorn-io/z"
	"github.com/adrg/xdg"
	"github.com/gptscript-ai/clicky-chats/pkg/agents"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	kb "github.com/gptscript-ai/clicky-chats/pkg/knowledgebases"
	"github.com/gptscript-ai/clicky-chats/pkg/tools"
	"github.com/gptscript-ai/clicky-chats/pkg/trigger"
	"github.com/gptscript-ai/gptscript/pkg/cache"
	"github.com/gptscript-ai/gptscript/pkg/confirm"
	"github.com/gptscript-ai/gptscript/pkg/gptscript"
	"github.com/gptscript-ai/gptscript/pkg/loader"
	gptopenai "github.com/gptscript-ai/gptscript/pkg/openai"
	"github.com/gptscript-ai/gptscript/pkg/repos/runtimes"
	"github.com/gptscript-ai/gptscript/pkg/runner"
	"github.com/gptscript-ai/gptscript/pkg/server"
	"github.com/gptscript-ai/gptscript/pkg/types"
	"github.com/gptscript-ai/gptscript/pkg/version"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	minPollingInterval = time.Second
	toolCallTimeout    = 15 * time.Minute
)

type Config struct {
	Logger                  *slog.Logger
	PollingInterval         time.Duration
	APIURL, APIKey, AgentID string
	Cache, Confirm          bool
	Trigger, RunTrigger     trigger.Trigger
}

var inputModifiers = map[string]func(*agent, *db.RunStep, []string, string) ([]string, string, error){
	"retrieval": func(agent *agent, runStep *db.RunStep, env []string, _ string) ([]string, string, error) {
		// extra environment variables for knowledge retrieval
		env = append(env,
			// leading http:// removed, since GPTScript needs to have it in the #!http:// instruction to determine that it's an HTTP call
			"knowledge_retrieval_api_url="+strings.TrimSuffix(strings.TrimPrefix(agent.kbm.KnowledgeRetrievalAPIURL, "http://"), "/"),
			"knowledge_retrieval_dataset="+strings.ToLower(runStep.AssistantID),
		)
		return env, runStep.RetrievalArguments, nil
	},
}

func Start(ctx context.Context, wg *sync.WaitGroup, gdb *db.DB, kbm *kb.KnowledgeBaseManager, cfg Config) error {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default().With("agent", "step runner")
	}
	a, err := newAgent(gdb, kbm, cfg)
	if err != nil {
		return err
	}

	a.builtInToolDefinitions, err = populateTools(ctx, cfg.Logger, gdb.WithContext(ctx))
	if err != nil {
		return err
	}

	a.Start(ctx, wg)

	return nil
}

type agent struct {
	logger              *slog.Logger
	pollingInterval     time.Duration
	id, apiKey, url     string
	cache, confirm      bool
	client              *http.Client
	db                  *db.DB
	kbm                 *kb.KnowledgeBaseManager
	trigger, runTrigger trigger.Trigger

	builtInToolDefinitions map[string]types.Program
}

func newAgent(db *db.DB, kbm *kb.KnowledgeBaseManager, cfg Config) (*agent, error) {
	if cfg.PollingInterval < minPollingInterval {
		return nil, fmt.Errorf("polling interval must be at least %s", minPollingInterval)
	}

	if cfg.Trigger == nil {
		cfg.Logger.Warn("[step runner] No trigger provided, using noop")
		cfg.Trigger = trigger.NewNoop()
	}
	if cfg.RunTrigger == nil {
		cfg.Logger.Warn("[step runner] No run trigger provided, using noop")
		cfg.RunTrigger = trigger.NewNoop()
	}

	return &agent{
		logger:          cfg.Logger,
		pollingInterval: cfg.PollingInterval,
		cache:           cfg.Cache,
		confirm:         cfg.Confirm,
		client:          http.DefaultClient,
		apiKey:          cfg.APIKey,
		db:              db,
		kbm:             kbm,
		id:              cfg.AgentID,
		url:             cfg.APIURL,
		trigger:         cfg.Trigger,
		runTrigger:      cfg.RunTrigger,
	}, nil
}

func (a *agent) newOpts(caster *broadcaster.Broadcaster[server.Event]) *gptscript.Options {
	return &gptscript.Options{
		Cache: cache.Options{
			Cache: z.Pointer(!a.cache),
		},
		Runner: runner.Options{
			MonitorFactory: server.NewSessionFactory(caster),
			RuntimeManager: runtimes.Default(filepath.Join(xdg.CacheHome, version.ProgramName)),
		},
		OpenAI: gptopenai.Options{
			APIKey:  a.apiKey,
			BaseURL: a.url,
		},
	}
}

func (a *agent) Start(ctx context.Context, wg *sync.WaitGroup) {
	// Start the "job runner"
	wg.Add(1)
	go func() {
		defer wg.Done()

		timer := time.NewTimer(a.pollingInterval)
		for {
			a.run(ctx)
			select {
			case <-ctx.Done():
				// Ensure the timer channel is drained
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return
			case <-timer.C:
			case <-a.trigger.Triggered():
			}

			if !timer.Stop() {
				// Ensure the timer channel has been drained.
				select {
				case <-timer.C:
				default:
				}
			}

			timer.Reset(a.pollingInterval)
		}
	}()
}

func (a *agent) run(ctx context.Context) {
	a.logger.Debug("Checking for a run")
	// Look for a new run and claim it. Also, query for the other objects we need.
	run, runStep := new(db.Run), new(db.RunStep)
	if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(run).Where("system_status = ?", string(openai.RunObjectStatusRequiresAction)).Where("system_claimed_by IS NULL OR system_claimed_by = ?", a.id).Order("created_at desc").First(run).Error; err != nil {
			return err
		}

		thread := new(db.Thread)
		if err := tx.Model(thread).Where("id = ?", run.ThreadID).First(thread).Error; err != nil {
			return err
		}

		// If the thread is locked by another run, then return an error.
		if thread.LockedByRunID != run.ID {
			return fmt.Errorf("thread %s found to be locked by %s while processing run %s", run.ThreadID, thread.LockedByRunID, run.ID)
		}

		if err := tx.Model(runStep).
			Where("run_id = ?", run.ID).
			Where("status = ?", openai.RunObjectStatusInProgress).
			Where("type = ?", openai.RunStepDetailsToolCallsObjectTypeToolCalls).
			Where("runner_type = ?", tools.GPTScriptRunnerType).
			Order("created_at asc").
			First(runStep).Error; err != nil {
			return err
		}

		updates := map[string]any{
			"system_claimed_by": a.id,
			"system_status":     string(openai.RunObjectStatusInProgress),
			"event_index":       run.EventIndex,
		}
		return tx.Model(run).Clauses(clause.Returning{}).Where("id = ?", run.ID).Updates(updates).Error
	}); err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			a.logger.Error("failed to get run", "error", err)
		}
		return
	}

	caster := broadcaster.New[server.Event]()
	go caster.Start(ctx)

	go func() {
		defer caster.Shutdown()
		if err := a.processRunStep(ctx, caster.Subscribe(), a.newOpts(caster), run, runStep); err != nil {
			a.logger.Error("failed to process run step", "err", err)
		}
	}()
}

func (a *agent) processRunStep(ctx context.Context, events *broadcaster.Subscription[server.Event], opts *gptscript.Options, run *db.Run, runStep *db.RunStep) (err error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, toolCallTimeout)
	defer cancel()

	go agents.PollForCancellation(timeoutCtx, cancel, a.db.WithContext(timeoutCtx), runStep, runStep.ID, a.pollingInterval)

	l := a.logger.With("run_id", run.ID, "run_step_id", runStep.ID)

	defer func() {
		if err != nil && !errors.Is(err, context.Canceled) {
			failRunStep(l, a.db.WithContext(ctx), run, runStep, err, openai.RunObjectLastErrorCodeServerError)
		}
	}()

	stepDetails := z.Pointer(runStep.StepDetails.Data())
	toolCalls, err := extractToolCalls(stepDetails)
	if err != nil {
		return fmt.Errorf("failed to get run step function calls: %w", err)
	}

	for i := range toolCalls {
		tc := &toolCalls[i]
		id, functionName, arguments, err := determineFunctionAndArguments(tc)
		if err != nil {
			return fmt.Errorf("failed to determine function and arguments: %w", err)
		}

		envs := os.Environ()

		// Modify the input (env and args) if necessary
		if inputModifier, ok := inputModifiers[functionName]; ok {
			var err error
			envs, arguments, err = inputModifier(a, runStep, envs, arguments)
			if err != nil {
				return fmt.Errorf("[tool: %s] failed to modify input: %w", functionName, err)
			}
		}

		prg, ok := a.builtInToolDefinitions[functionName]
		if !ok {
			tool := new(db.Tool)
			if err = a.db.WithContext(timeoutCtx).Model(tool).Where("id = ?", functionName).First(tool).Error; err != nil {
				return fmt.Errorf("failed to get tool %s: %w", functionName, err)
			}

			prg, err = loader.ProgramFromSource(timeoutCtx, string(tool.Program), "")
			if err != nil {
				return fmt.Errorf("failed to load program for tool %s: %w", functionName, err)
			}

			envs = append(envs, tool.EnvVars...)
		}

		gdb := a.db.WithContext(ctx)
		confirmCtx := confirm.WithConfirm(timeoutCtx, &stepConfirmer{db: gdb, run: run, runStep: runStep, toolCallID: id, confirm: a.confirm})
		output, err := agents.RunTool(confirmCtx, l, events, gdb, opts, prg, envs, arguments, run.ID, runStep.ID)
		if err != nil {
			return fmt.Errorf("failed to run tool call at index %d: %w", i, err)
		}

		if err = db.SetOutputForRunStepToolCall(tc, output); err != nil {
			return fmt.Errorf("failed to set output for tool call at index %d: %w", i, err)
		}

		if err = db.EmitRunStepDeltaOutputEvent(gdb, run, tc, i); err != nil {
			return fmt.Errorf("failed to emit event for tool call at index %d: %w", i, err)
		}

		toolCalls[i] = *tc
	}

	if err = stepDetails.FromRunStepDetailsToolCallsObject(openai.RunStepDetailsToolCallsObject{
		ToolCalls: toolCalls,
		Type:      openai.RunStepDetailsToolCallsObjectTypeToolCalls,
	}); err != nil {
		return err
	}

	if err = a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Update the run step with the output
		if err = tx.Model(runStep).Clauses(clause.Returning{}).Where("id = ?", runStep.ID).Updates(
			map[string]any{
				"status":       openai.RunObjectStatusCompleted,
				"completed_at": z.Pointer(int(time.Now().Unix())),
				"step_details": datatypes.NewJSONType(stepDetails),
			}).Error; err != nil {
			return err
		}

		run.EventIndex++
		runEvent := &db.RunEvent{
			EventName: string(openai.ThreadRunStepCompleted),
			JobResponse: db.JobResponse{
				RequestID: run.ID,
			},
			ResponseIdx: run.EventIndex,
			RunStep:     datatypes.NewJSONType(runStep),
		}
		if err = db.Create(tx, runEvent); err != nil {
			return err
		}

		return tx.Model(run).Where("id = ?", run.ID).Updates(map[string]any{
			"system_status": string(openai.RunObjectStatusQueued),
			"event_index":   run.EventIndex,
		}).Error
	}); err != nil {
		l.Error("Failed to update run step", "err", err)
		return err
	}

	a.trigger.Ready(run.ID)
	a.runTrigger.Kick(run.ID)

	return nil
}

// populateTools loads the gptscript program from the provided link and subtool. The database is checked first to see if
// the tool has already been loaded, it will be loaded from the URL again if necessary. The run_step agent will use this
// program definition to run the tool with the gptscript engine.
func populateTools(ctx context.Context, l *slog.Logger, gdb *gorm.DB) (map[string]types.Program, error) {
	var err error
	builtInToolDefinitions := make(map[string]types.Program, len(tools.GPTScriptDefinitions()))
	for toolName, toolDef := range tools.GPTScriptDefinitions() {
		if toolDef.Link == "" || toolDef.Link == tools.SkipLoadingTool {
			l.Info("Skipping tool", "name", toolName)
			continue
		}

		builtInToolDefinitions[toolName], err = db.LoadBuiltInTool(ctx, gdb, toolName, toolDef)
		if err != nil {
			return nil, err
		}
	}
	return builtInToolDefinitions, nil
}

func failRunStep(l *slog.Logger, gdb *gorm.DB, run *db.Run, runStep *db.RunStep, err error, errorCode openai.RunObjectLastErrorCode) {
	l.Debug("Error occurred while processing run step, failing run", "err", err)
	runError := &db.RunLastError{
		Code:    string(errorCode),
		Message: err.Error(),
	}

	updates := map[string]any{
		"status":     openai.RunObjectStatusFailed,
		"failed_at":  z.Pointer(int(time.Now().Unix())),
		"last_error": datatypes.NewJSONType(runError),
		"usage":      runStep.Usage,
	}
	if err = gdb.Transaction(func(tx *gorm.DB) error {
		if err = tx.Model(runStep).Where("id = ?", runStep.ID).Updates(updates).Error; err != nil {
			return err
		}

		run.EventIndex++
		runEvent := &db.RunEvent{
			EventName: string(openai.ThreadRunStepFailed),
			JobResponse: db.JobResponse{
				RequestID: run.ID,
			},
			RunStep:     datatypes.NewJSONType(runStep),
			ResponseIdx: run.EventIndex,
		}

		if err = db.Create(tx, runEvent); err != nil {
			return err
		}

		run.EventIndex++
		updates["system_status"] = openai.RunObjectStatusFailed
		updates["event_index"] = run.EventIndex
		if err = tx.Model(run).Where("id = ?", run.ID).Updates(updates).Error; err != nil {
			return err
		}

		runEvent = &db.RunEvent{
			EventName: string(openai.ThreadRunFailed),
			JobResponse: db.JobResponse{
				RequestID: run.ID,
				Done:      true,
			},
			Run:         datatypes.NewJSONType(run),
			ResponseIdx: run.EventIndex,
		}
		if err = db.Create(tx, runEvent); err != nil {
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

func determineFunctionAndArguments(toolCall *openai.RunStepDetailsToolCallsObject_ToolCalls_Item) (string, string, string, error) {
	info, err := db.GetOutputForRunStepToolCall(toolCall)
	if err != nil {
		return "", "", "", err
	}

	return info.ID, strings.TrimPrefix(info.Name, tools.GPTScriptToolNamePrefix), info.Arguments, nil
}
