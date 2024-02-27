package controllers

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/acorn-io/broadcaster"
	"github.com/gptscript-ai/gptscript/pkg/loader"
	"github.com/gptscript-ai/gptscript/pkg/runner"
	"github.com/gptscript-ai/gptscript/pkg/server"
	"github.com/thedadams/clicky-chats/pkg/db"
	gdb "gorm.io/gorm"
)

func Start(ctx context.Context, db *gdb.DB) error {
	controller, err := newController(db)
	if err != nil {
		return err
	}

	controller.Start(ctx)
	return nil
}

type controller struct {
	runner      *runner.Runner
	db          *gdb.DB
	broadcaster *broadcaster.Broadcaster[server.Event]
}

func newController(db *gdb.DB) (*controller, error) {
	caster := broadcaster.New[server.Event]()
	noCacheRunner, err := runner.New(runner.Options{
		CacheOptions: runner.CacheOptions{
			Cache: new(bool),
		},
		MonitorFactory: server.NewSessionFactory(caster),
	})
	if err != nil {
		return nil, err
	}

	return &controller{
		runner:      noCacheRunner,
		db:          db,
		broadcaster: caster,
	}, nil
}

func (c *controller) Start(ctx context.Context) {
	throttle := make(chan struct{}, 10)
	go func() {
		c.broadcaster.Start(ctx)
		defer c.broadcaster.Shutdown()

		sub := c.broadcaster.Subscribe()
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

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				run := &db.Run{}
				if db := c.db.Where("started_at = 0").Order("created_at desc").Limit(1).Find(run); db.Error != nil {
					if errors.Is(db.Error, gdb.ErrRecordNotFound) {
						continue
					}
					slog.Error("Failed to find run", "err", db.Error)
					continue
				} else if run.ID == "" || len(run.FileIDs) == 0 {
					continue
				}

				prg, err := loader.ProgramFromSource(ctx, run.Instructions, "")
				if err != nil {
					slog.Error("Failed to initialize program", "run_id", run.ID, "err", err)
					continue
				}

				select {
				case throttle <- struct{}{}:
				default:
					continue
				}

				go func() {
					defer func() {
						<-throttle
					}()

					slog.Info("Running", "run_id", run.ID)
					if err := c.db.Model(run).Update("started_at", time.Now().Unix()).Error; err != nil {
						slog.Error("Failed to update run", "run_id", run.ID, "err", err)
						return
					}

					output, err := c.runner.Run(server.ContextWithNewID(ctx), prg, os.Environ(), run.Instructions)
					if err != nil {
						slog.Error("Failed to run", "run_id", run.ID, "err", err)
						if err := c.db.Model(run).Update("failed_at", time.Now().Unix()).Error; err != nil {
							slog.Error("Failed to update run", "run_id", run.ID, "err", err)
						}
						return
					}

					slog.Info("Finished running", "run_id", run.ID, "output", output)

					if err := c.db.Model(run).Update("completed_at", time.Now().Unix()).Error; err != nil {
						slog.Error("Failed to update run", "run_id", run.ID, "err", err)
					}
				}()
			}
		}
	}()
}
