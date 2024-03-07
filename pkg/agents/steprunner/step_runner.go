package steprunner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/acorn-io/broadcaster"
	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/agents"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"github.com/gptscript-ai/gptscript/pkg/loader"
	gptopenai "github.com/gptscript-ai/gptscript/pkg/openai"
	"github.com/gptscript-ai/gptscript/pkg/runner"
	"github.com/gptscript-ai/gptscript/pkg/server"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	codeInterpreterFunctionName = "code_interpreter"
	retrievalFunctionName       = "retrieval"
	webSearchFunctionName       = "web_browsing"

	webBrowsingTool = `
tools: web-browsing
Get the contents of the provided url.
---
name: web-browsing
description: I am a tool that can visit web pages and retrieve the content.
args: url: The url to visit
tools: sys.http.get?

Download the content of "${url}" and return the content.
`

	codeInterpreterTool = `
name: code-interpreter
description: I am a tool that can run python code
arg: code: The code to run

#!/bin/bash
printf '%b\n' "${code}" | python3 -i -
`
)

var builtInFunctionNameToTool = map[string]string{
	webSearchFunctionName:       webBrowsingTool,
	codeInterpreterFunctionName: codeInterpreterTool,
}

type Config struct {
	PollingInterval         time.Duration
	APIURL, APIKey, AgentID string
}

func Start(ctx context.Context, gdb *db.DB, cfg Config) error {
	a, err := newAgent(gdb, cfg)
	if err != nil {
		return err
	}

	return a.Start(ctx, cfg.PollingInterval)
}

type agent struct {
	id, apiKey, url string
	client          *http.Client
	db              *db.DB
}

func newAgent(db *db.DB, cfg Config) (*agent, error) {
	return &agent{
		client: http.DefaultClient,
		apiKey: cfg.APIKey,
		db:     db,
		id:     cfg.AgentID,
		url:    cfg.APIURL,
	}, nil
}

func (c *agent) Start(ctx context.Context, pollingInterval time.Duration) error {
	caster := broadcaster.New[server.Event]()
	oaClient, err := gptopenai.NewClient(gptopenai.Options{
		APIKey:  c.apiKey,
		BaseURL: c.url,
	})
	if err != nil {
		return err
	}
	noCacheRunner, err := runner.New(oaClient, runner.Options{
		MonitorFactory: server.NewSessionFactory(caster),
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
			select {
			case <-ctx.Done():
				return
			default:
				if err := c.run(ctx, noCacheRunner); err != nil {
					if !errors.Is(err, gorm.ErrRecordNotFound) {
						slog.Error("failed step runner iteration", "err", err)
					}
					time.Sleep(pollingInterval)
				}
			}
		}
	}()

	return nil
}

func (c *agent) run(ctx context.Context, runner *runner.Runner) error {
	slog.Debug("Checking for a run")
	// Look for a new run and claim it. Also, query for the other objects we need.
	run, runStep := new(db.Run), new(db.RunStep)
	err := c.db.WithContext(ctx).Model(run).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("system_status = ?", "requires_action").Where("system_claimed_by IS NULL OR system_claimed_by = ?", c.id).Order("created_at desc").First(run).Error; err != nil {
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

		if err := tx.Model(run).Where("id = ?", run.ID).Updates(map[string]interface{}{"system_claimed_by": c.id, "system_status": "in_progress"}).Error; err != nil {
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
			failRunStep(l, c.db.WithContext(ctx), run, runStep, err, openai.RunObjectLastErrorCodeServerError)
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

		prg, err := loader.ProgramFromSource(ctx, builtInFunctionNameToTool[strings.TrimPrefix(functionName, agents.GPTScriptToolNamePrefix)], "")
		if err != nil {
			return fmt.Errorf("failed to initialize program: %w", err)
		}

		output, err := runner.Run(server.ContextWithNewID(ctx), prg, os.Environ(), arguments)
		if err != nil {
			return fmt.Errorf("failed to run: %w", err)
		}

		if err := db.SetOutputForRunStepToolCall(&tc, output); err != nil {
			return err
		}

		toolCalls[i] = tc
	}

	if err := stepDetails.FromRunStepDetailsToolCallsObject(openai.RunStepDetailsToolCallsObject{
		ToolCalls: toolCalls,
		Type:      openai.ToolCalls,
	}); err != nil {
		return err
	}

	if err := c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Update the run step with a fake output
		if err := tx.Model(runStep).Where("id = ?", runStep.ID).Updates(map[string]interface{}{"status": openai.Completed, "completed_at": z.Pointer(int(time.Now().Unix())), "step_details": datatypes.NewJSONType(stepDetails)}).Error; err != nil {
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

func failRunStep(l *slog.Logger, gdb *gorm.DB, run *db.Run, runStep *db.RunStep, err error, errorCode openai.RunObjectLastErrorCode) {
	runError := &db.RunLastError{
		Code:    string(errorCode),
		Message: err.Error(),
	}
	if err := gdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(run).Where("id = ?", run.ID).Updates(map[string]interface{}{
			"status":        openai.RunObjectStatusFailed,
			"system_status": nil,
			"failed_at":     z.Pointer(int(time.Now().Unix())),
			"last_error":    datatypes.NewJSONType(runError),
			"usage":         run.Usage,
		}).Error; err != nil {
			return err
		}

		if err := tx.Model(runStep).Where("id = ?", run.ID).Updates(map[string]interface{}{
			"status":     openai.Failed,
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

	return info.Name, info.Arguments, nil
}
