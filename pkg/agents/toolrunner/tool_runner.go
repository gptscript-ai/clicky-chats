package toolrunner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/acorn-io/broadcaster"
	"github.com/acorn-io/z"
	"github.com/adrg/xdg"
	"github.com/gptscript-ai/clicky-chats/pkg/agents"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"github.com/gptscript-ai/clicky-chats/pkg/trigger"
	"github.com/gptscript-ai/gptscript/pkg/cache"
	"github.com/gptscript-ai/gptscript/pkg/gptscript"
	"github.com/gptscript-ai/gptscript/pkg/loader"
	gptopenai "github.com/gptscript-ai/gptscript/pkg/openai"
	"github.com/gptscript-ai/gptscript/pkg/repos/runtimes"
	"github.com/gptscript-ai/gptscript/pkg/runner"
	"github.com/gptscript-ai/gptscript/pkg/server"
	"github.com/gptscript-ai/gptscript/pkg/version"
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
	Cache                            bool
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
	cache                            bool
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
		client:          http.DefaultClient,
		apiKey:          cfg.APIKey,
		db:              db,
		id:              cfg.AgentID,
		url:             cfg.APIURL,
		trigger:         cfg.Trigger,
	}, nil
}

func (a *agent) Start(ctx context.Context, wg *sync.WaitGroup) {
	caster := broadcaster.New[server.Event]()
	opts := &gptscript.Options{
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

	go caster.Start(ctx)

	// Start the "job runner"
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer caster.Shutdown()

		timer := time.NewTimer(a.pollingInterval)

		for {
			a.run(ctx, caster, opts)
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

func (a *agent) run(ctx context.Context, caster *broadcaster.Broadcaster[server.Event], opts *gptscript.Options) {
	a.logger.Debug("Checking for a tool to run")
	// Look for a new run tool and claim it.
	runTool := new(db.RunToolObject)
	if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(runTool).Where("status = ?", "queued").Where("claimed_by IS NULL OR claimed_by = ?", a.id).Order("created_at desc").First(runTool).Error; err != nil {
			return err
		}

		updates := map[string]any{
			"claimed_by": a.id,
			"status":     string(openai.RunObjectStatusInProgress),
		}
		return tx.Model(runTool).Clauses(clause.Returning{}).Where("id = ?", runTool.ID).Updates(updates).Error
	}); err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			a.logger.Error("failed to get run", "error", err)
		}
		return
	}

	go func() {
		if err := a.processToolRun(ctx, caster, opts, runTool); err != nil {
			a.logger.Error("failed to process tool run", "err", err)
		}
	}()
}

func (a *agent) processToolRun(ctx context.Context, caster *broadcaster.Broadcaster[server.Event], opts *gptscript.Options, runTool *db.RunToolObject) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, toolCallTimeout)
	defer cancel()

	l := a.logger.With("run_tool_id", runTool.ID)

	prg, err := loader.Program(timeoutCtx, runTool.File, runTool.Subtool)
	if err != nil {
		return fmt.Errorf("failed to load program for tool %s: %w", runTool.ID, err)
	}

	envs := append(os.Environ(), runTool.EnvVars...)

	gdb := a.db.WithContext(ctx)
	runTool.Output, err = agents.RunTool(timeoutCtx, l, caster, gdb, opts, prg, envs, runTool.Input, "", runTool.ID)
	if err != nil {
		return fmt.Errorf("failed to run tool: %w", err)
	}

	// Update the run tool with the output
	if err = gdb.Model(runTool).Where("id = ?", runTool.ID).Updates(
		map[string]any{
			"output": runTool.Output,
			"status": string(openai.RunObjectStatusCompleted),
			"done":   true,
		}).Error; err != nil {
		return err
	}

	a.trigger.Ready(runTool.ID)

	return nil
}
