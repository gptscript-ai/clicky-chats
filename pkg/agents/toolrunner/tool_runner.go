package toolrunner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/acorn-io/broadcaster"
	"github.com/gptscript-ai/clicky-chats/pkg/agents"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"github.com/gptscript-ai/clicky-chats/pkg/trigger"
	gogptscript "github.com/gptscript-ai/go-gptscript"
	"github.com/gptscript-ai/gptscript/pkg/server"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	minPollingInterval = time.Second
	toolCallTimeout    = 15 * time.Minute
)

type Config struct {
	Logger                           *slog.Logger
	PollingInterval, RetentionPeriod time.Duration
	APIURL, APIKey, AgentID          string
	Cache, Confirm                   bool
	Trigger                          trigger.Trigger
}

func Start(ctx context.Context, wg *sync.WaitGroup, gdb *db.DB, cfg Config) error {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default().With("agent", "tool runner")
	}
	a, err := newAgent(gdb, cfg)
	if err != nil {
		return err
	}

	a.Start(ctx, wg)

	return nil
}

type agent struct {
	logger                           *slog.Logger
	pollingInterval, retentionPeriod time.Duration
	id, apiKey, url                  string
	cache, confirm                   bool
	client                           *http.Client
	db                               *db.DB
	trigger                          trigger.Trigger
}

func newAgent(db *db.DB, cfg Config) (*agent, error) {
	if cfg.PollingInterval < minPollingInterval {
		return nil, fmt.Errorf("polling interval must be at least %s", minPollingInterval)
	}

	if cfg.Trigger == nil {
		cfg.Logger.Warn("No trigger provided, using noop")
		cfg.Trigger = trigger.NewNoop()
	}

	return &agent{
		logger:          cfg.Logger,
		pollingInterval: cfg.PollingInterval,
		retentionPeriod: cfg.RetentionPeriod,
		cache:           cfg.Cache,
		confirm:         cfg.Confirm,
		client:          http.DefaultClient,
		apiKey:          cfg.APIKey,
		db:              db,
		id:              cfg.AgentID,
		url:             cfg.APIURL,
		trigger:         cfg.Trigger,
	}, nil
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

	// Start cleanup
	wg.Add(1)
	go func() {
		defer wg.Done()
		cleanupInterval := a.retentionPeriod / 2
		timer := time.NewTimer(cleanupInterval)

		for {
			a.logger.Debug("Looking for completed tool runs")
			var runToolObjects []db.RunToolObject
			if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				if err := tx.Model(new(db.RunToolObject)).Where("done = true").Find(&runToolObjects).Error; err != nil {
					return err
				}
				if len(runToolObjects) == 0 {
					return nil
				}

				requestIDs := make([]string, 0, len(runToolObjects))
				for _, rt := range runToolObjects {
					requestIDs = append(requestIDs, rt.ID)
				}

				if err := tx.Delete(new(db.RunStepEvent), "request_id IN ?", requestIDs).Error; err != nil {
					return err
				}

				return tx.Delete(runToolObjects).Error
			}); err != nil {
				a.logger.Error("Failed to cleanup chat completions", "err", err)
			}

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
			}

			timer.Reset(cleanupInterval)
		}
	}()
}

func (a *agent) run(ctx context.Context) {
	a.logger.Debug("Checking for a tool to run")
	// Look for a new run tool and claim it.
	runToolObject := new(db.RunToolObject)
	if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(runToolObject).Where("status = ?", "queued").Where("claimed_by IS NULL OR claimed_by = ?", a.id).Order("created_at desc").First(runToolObject).Error; err != nil {
			return err
		}

		updates := map[string]any{
			"claimed_by": a.id,
			"status":     string(openai.RunObjectStatusInProgress),
		}
		return tx.Model(runToolObject).Clauses(clause.Returning{}).Where("id = ?", runToolObject.ID).Updates(updates).Error
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
		if err := a.processToolRun(ctx, runToolObject); err != nil {
			a.logger.Error("failed to process tool run", "err", err)
		}
	}()
}

func (a *agent) processToolRun(ctx context.Context, runToolObject *db.RunToolObject) error {
	var err error
	timeoutCtx, cancel := context.WithTimeout(ctx, toolCallTimeout)
	defer cancel()

	l := a.logger.With("run_tool_id", runToolObject.ID)
	gdb := a.db.WithContext(ctx)
	runToolObject.Output, err = runTool(timeoutCtx, l, gdb, runToolObject)
	if err != nil {
		return fmt.Errorf("failed to run tool: %w", err)
	}

	// Update the run tool with the output
	if err = gdb.Model(runToolObject).Where("id = ?", runToolObject.ID).Updates(
		map[string]any{
			"output": runToolObject.Output,
			"status": string(openai.RunObjectStatusCompleted),
			"done":   true,
		}).Error; err != nil {
		return err
	}

	a.trigger.Ready(runToolObject.ID)

	return nil
}

func runTool(ctx context.Context, l *slog.Logger, gdb *gorm.DB, runToolObject *db.RunToolObject) (string, error) {
	stdOut, stdErr, events, wait := gogptscript.StreamExecFileWithEvents(ctx, runToolObject.File, runToolObject.Input, gogptscript.Opts{
		DisableCache: runToolObject.DisableCache,
		Chdir:        runToolObject.Chdir,
		SubTool:      runToolObject.Subtool,
	})

	var (
		index       int
		lastRunID   string
		eventBuffer []server.Event
		buffer      = bufio.NewScanner(events)
	)
	for buffer.Scan() {
		if len(buffer.Bytes()) == 0 {
			// If there is no event, then continue.
			continue
		}

		e := server.Event{}
		err := json.Unmarshal(buffer.Bytes(), &e)
		if err != nil {
			l.Error("failed to unmarshal event", "error", err, "event", buffer.Text())
			continue
		}
		// Ensure that the callConfirm event is after an event with the same runID.
		if (len(eventBuffer) > 0 || e.Type == agents.EventTypeCallConfirm) && lastRunID != e.RunID {
			eventBuffer = append(eventBuffer, e)
			lastRunID = e.RunID
			continue
		}

		for _, ev := range eventBuffer {
			runStepEvent := db.FromGPTScriptEvent(ev, "", runToolObject.ID, index, false)
			if err := db.Create(gdb, runStepEvent); err != nil {
				l.Error("failed to create run step event", "error", err)
			}
			index++
		}

		eventBuffer = nil
		lastRunID = e.RunID

		runStepEvent := db.FromGPTScriptEvent(e, "", runToolObject.ID, index, false)
		if err := db.Create(gdb, runStepEvent); err != nil {
			l.Error("failed to create run step event", "error", err)
			continue
		}

		index++
	}
	l.Debug("done receiving events")

	// Read the output of the script.
	out, err := io.ReadAll(stdOut)
	if err != nil {
		return "", fmt.Errorf("failed to read output: %w", err)
	}

	output := string(out)

	stderr, err := io.ReadAll(stdErr)
	if err != nil {
		return output, fmt.Errorf("failed to read stderr: %w", err)
	}

	if index == 0 {
		// If no events were created, then an error occurred when trying to run the tool.
		// Create an event with the error and the run tool object ID.
		runStepEvent := &db.RunStepEvent{
			JobResponse: db.JobResponse{
				RequestID: runToolObject.ID,
			},
			Err:         string(stderr),
			ResponseIdx: index,
		}

		if err = db.Create(gdb, runStepEvent); err != nil {
			l.Error("failed to create run step event", "error", err)
		} else {
			index++
		}
	}

	// Create final event that just says we're done with this run step.
	runStepEvent := db.FromGPTScriptEvent(server.Event{}, "", runToolObject.ID, index, true)
	if err := db.Create(gdb, runStepEvent); err != nil {
		l.Error("failed to create run step event", "error", err)
	}

	err = wait()
	if errors.Is(err, context.DeadlineExceeded) {
		output = "The tool call took too long to complete, aborting"
	} else if execErr := new(exec.ExitError); errors.As(err, &execErr) {
		output = fmt.Sprintf("The tool call returned an exit code of %d with message %q and output %q, aborting", execErr.ExitCode(), execErr.String(), stderr)
	} else if err != nil {
		return string(stderr), fmt.Errorf("failed to wait: %w, error output: %s", err, stderr)
	}

	return output, nil
}
