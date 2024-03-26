package run

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/agents"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"github.com/gptscript-ai/clicky-chats/pkg/trigger"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	minPollingInterval  = time.Second
	minRequestRetention = 5 * time.Minute
)

type Config struct {
	PollingInterval, RetentionPeriod time.Duration
	APIURL, APIKey, AgentID          string
	Trigger, RunStepTrigger          trigger.Trigger
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

	a.Start(ctx)

	return nil
}

type agent struct {
	pollingInterval, retentionPeriod time.Duration
	id, apiKey, url                  string
	client                           *http.Client
	db                               *db.DB
	builtInToolDefinitions           map[string]*openai.FunctionObject
	trigger, runStepTrigger          trigger.Trigger
}

func newAgent(db *db.DB, cfg Config) (*agent, error) {
	if cfg.PollingInterval < minPollingInterval {
		return nil, fmt.Errorf("[run] polling interval must be at least %s", minPollingInterval)
	}
	if cfg.RetentionPeriod < minRequestRetention {
		return nil, fmt.Errorf("[run] request retention must be at least %s", minRequestRetention)
	}

	if cfg.Trigger == nil {
		slog.Warn("[run] No trigger provided, using noop")
		cfg.Trigger = trigger.NewNoop()
	}
	if cfg.RunStepTrigger == nil {
		slog.Warn("[run] No run step trigger provided, using noop")
		cfg.RunStepTrigger = trigger.NewNoop()
	}

	return &agent{
		pollingInterval: cfg.PollingInterval,
		retentionPeriod: cfg.RetentionPeriod,
		client:          http.DefaultClient,
		apiKey:          cfg.APIKey,
		db:              db,
		id:              cfg.AgentID,
		url:             cfg.APIURL,
		trigger:         cfg.Trigger,
		runStepTrigger:  cfg.RunStepTrigger,
	}, nil
}

func (a *agent) Start(ctx context.Context) {
	// Start the "job runner"
	go func() {
		timer := time.NewTimer(a.pollingInterval)
		for {
			if err := a.run(ctx); err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					slog.Error("failed run iteration", "err", err)
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
				case <-a.trigger.Triggered():
				}
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
	go func() {
		var (
			cleanupInterval = a.retentionPeriod / 2
			jobObjects      = []db.Storer{
				new(db.RunEvent),
			}
			cdb = a.db.WithContext(ctx)

			timer = time.NewTimer(cleanupInterval)
		)
		for {
			slog.Debug("Looking for completed runs")

			// Look for a runs, runSteps, and runEvents to clean-up.
			var runs []db.Run
			if err := cdb.Transaction(func(tx *gorm.DB) error {
				if err := db.DeleteExpired(tx, time.Now().Add(-a.retentionPeriod), jobObjects...); err != nil {
					return err
				}

				// TODO(thedadams): Under which circumstances should we clean up old runs? This currently does nothing.
				if err := tx.Model(new(db.Run)).Where("id IS NULL").Order("created_at desc").Find(&runs).Error; err != nil {
					return err
				}
				if len(runs) == 0 {
					return nil
				}

				runIDs := make([]string, 0, len(runs))
				for _, run := range runs {
					runIDs = append(runIDs, run.ID)
				}

				if err := tx.Delete(new(db.RunStep), "run_id IN ?", runIDs).Error; err != nil {
					return err
				}

				return tx.Delete(runs).Error
			}); err != nil {
				slog.Error("Failed to cleanup run completions", "err", err)
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

func (a *agent) run(ctx context.Context) error {
	slog.Debug("Checking for a run")
	// Look for a new run and claim it. Also, query for the other objects we need.
	var (
		run       = new(db.Run)
		assistant = new(db.Assistant)
		runSteps  = make([]db.RunStep, 0)
		messages  = make([]db.Message, 0)
		tools     = make([]db.Tool, 0)
	)
	err := a.db.WithContext(ctx).Model(run).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("claimed_by IS NULL AND status = ?", openai.RunObjectStatusQueued).Or("claimed_by = ? AND status = ? AND system_status = ?", a.id, openai.RunObjectStatusInProgress, openai.RunObjectStatusQueued).Order("created_at desc").First(run).Error; err != nil {
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

		if err := tx.Model(assistant).Where("id = ?", run.AssistantID).First(assistant).Error; err != nil {
			return err
		}

		if err := tx.Model(new(db.Tool)).Where("id IN ?", []string(assistant.GPTScriptTools)).Find(&tools).Error; err != nil {
			return err
		}

		if err := tx.Model(new(db.Message)).Where("thread_id = ?", run.ThreadID).Where("created_at <= ?", run.CreatedAt).Order("created_at asc").Find(&messages).Error; err != nil {
			return err
		}

		if err := tx.Model(new(db.RunStep)).Where("run_id = ?", run.ID).Where("type = ?", openai.RunStepObjectTypeToolCalls).Where("created_at >= ?", run.CreatedAt).Order("created_at asc").Find(&runSteps).Error; err != nil {
			return err
		}

		startedAt := run.StartedAt
		if startedAt == nil {
			startedAt = z.Pointer(int(time.Now().Unix()))
		}

		var runEvent *db.RunEvent
		// If the run is changing to in progress, then create an event.
		if run.Status != string(openai.InProgress) {
			run.EventIndex++

			runEvent = &db.RunEvent{
				EventName: db.ThreadRunInProgressEvent,
				JobResponse: db.JobResponse{
					RequestID: run.ID,
				},
				ResponseIdx: run.EventIndex,
			}
		}

		updates := map[string]any{
			"claimed_by":  a.id,
			"status":      openai.RunObjectStatusInProgress,
			"started_at":  startedAt,
			"event_index": run.EventIndex,
		}
		if err := tx.Model(run).Clauses(clause.Returning{}).Where("id = ?", run.ID).Updates(updates).Error; err != nil {
			return err
		}

		if runEvent != nil {
			runEvent.Run = datatypes.NewJSONType(run)
			return db.Create(tx, runEvent)
		}

		return nil
	})
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to get run: %w", err)
		}
		return err
	}

	runID := run.ID
	l := slog.With("type", "run", "id", runID)

	defer func() {
		if err != nil {
			if err := failRun(a.db.WithContext(ctx), run, err, openai.RunObjectLastErrorCodeServerError); err != nil {
				l.Error("failed to fail run", "error", err)
			}
		}
	}()

	l.Debug("Found run", "run", run)
	cc, err := prepareChatCompletionRequest(ctx, a.builtInToolDefinitions, run, assistant, tools, messages, runSteps)
	if err != nil {
		l.Error("Failed to prepare chat completion request", "err", err)
		return err
	}

	stream, err := agents.StreamChatCompletionRequest(ctx, l, a.client, a.url, a.apiKey, cc)
	if err != nil {
		l.Error("Failed to make chat completion request from run", "err", err)
		return err
	}

	if err = compileChunksAndApplyStatuses(ctx, l, a.db.WithContext(ctx), run, stream); err != nil {
		// If we get an error here, then we have already failed the run. Log the error and return so that we don't try to fail the run again.
		l.Error("failed to compile chat completion chunks", "error", err)
	}

	a.runStepTrigger.Kick(runID)
	a.trigger.Ready(runID)

	return nil
}

// failRun will mark the run as failed. The caller should wrap this in a transaction.
func failRun(gdb *gorm.DB, run *db.Run, err error, errorCode openai.RunObjectLastErrorCode) error {
	runError := &db.RunLastError{
		Code:    string(errorCode),
		Message: err.Error(),
	}
	run.EventIndex++
	if err = gdb.Model(run).Clauses(clause.Returning{}).Where("id = ?", run.ID).Updates(map[string]any{
		"status":        openai.RunObjectStatusFailed,
		"system_status": nil,
		"failed_at":     z.Pointer(int(time.Now().Unix())),
		"last_error":    datatypes.NewJSONType(runError),
		"usage":         run.Usage,
		"event_index":   run.EventIndex,
	}).Error; err != nil {
		return err
	}

	failRunEvent := &db.RunEvent{
		EventName: db.ThreadRunFailedEvent,
		JobResponse: db.JobResponse{
			RequestID: run.ID,
			Done:      true,
		},
		Run:         datatypes.NewJSONType(run),
		ResponseIdx: run.EventIndex,
	}

	if err := db.Create(gdb, failRunEvent); err != nil {
		return err
	}

	return gdb.Model(new(db.Thread)).Where("id = ?", run.ThreadID).Update("locked_by_run_id", nil).Error
}
