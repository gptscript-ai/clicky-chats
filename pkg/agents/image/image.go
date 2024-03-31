package image

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/trigger"
	"gorm.io/gorm"
)

const (
	minPollingInterval  = time.Second
	minRequestRetention = 5 * time.Minute
)

func Start(ctx context.Context, wg *sync.WaitGroup, gdb *db.DB, cfg Config) error {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default().With("agent", "image")
	}
	a, err := newAgent(gdb, cfg)
	if err != nil {
		return err
	}

	a.Start(ctx, wg)

	return nil
}

type Config struct {
	Logger                           *slog.Logger
	PollingInterval, RetentionPeriod time.Duration
	ImagesBaseURL, APIKey, AgentID   string
	Trigger                          trigger.Trigger
}

type agent struct {
	logger                                  *slog.Logger
	pollingInterval, requestRetention       time.Duration
	id, apiKey                              string
	generationsURL, editsURL, variationsURL string
	client                                  *http.Client
	db                                      *db.DB
	trigger                                 trigger.Trigger
}

func newAgent(db *db.DB, cfg Config) (*agent, error) {
	if cfg.PollingInterval < minPollingInterval {
		return nil, fmt.Errorf("[image] polling interval must be at least %s", minPollingInterval)
	}
	if cfg.RetentionPeriod < minRequestRetention {
		return nil, fmt.Errorf("[image] request retention must be at least %s", minRequestRetention)
	}

	if cfg.Trigger == nil {
		cfg.Logger.Warn("[image] No trigger provided, using noop")
		cfg.Trigger = trigger.NewNoop()
	}

	return &agent{
		logger:           cfg.Logger,
		pollingInterval:  cfg.PollingInterval,
		requestRetention: cfg.RetentionPeriod,
		generationsURL:   cfg.ImagesBaseURL + "/generations",
		editsURL:         cfg.ImagesBaseURL + "/edits",
		variationsURL:    cfg.ImagesBaseURL + "/variations",
		client:           http.DefaultClient,
		apiKey:           cfg.APIKey,
		db:               db,
		id:               cfg.AgentID,
		trigger:          cfg.Trigger,
	}, nil
}

func (a *agent) Start(ctx context.Context, wg *sync.WaitGroup) {
	// Start the "job runner"
	for _, run := range []func(context.Context, *slog.Logger) error{
		a.runGenerations,
		a.runEdits,
		a.runVariations,
	} {
		wg.Add(1)
		go func(r func(context.Context, *slog.Logger) error) {
			defer wg.Done()
			timer := time.NewTimer(a.pollingInterval)
			for {
				if err := r(ctx, a.logger); err != nil {
					if !errors.Is(err, gorm.ErrRecordNotFound) {
						a.logger.Error("failed run iteration", "err", err)
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
					// Ensure the timer channel is drained
					select {
					case <-timer.C:
					default:
					}
				}

				timer.Reset(a.pollingInterval)
			}
		}(run)
	}

	// Start cleanup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var (
			cleanupInterval = a.requestRetention / 2
			jobObjects      = []db.Storer{
				new(db.CreateImageRequest),
				new(db.CreateImageEditRequest),
				new(db.CreateImageVariationRequest),
				new(db.ImagesResponse),
			}
			cdb   = a.db.WithContext(ctx)
			timer = time.NewTimer(cleanupInterval)
		)
		for {
			a.logger.Debug("looking for expired image requests and responses")
			expiration := time.Now().Add(-a.requestRetention)
			if err := db.DeleteExpired(cdb, expiration, jobObjects...); err != nil {
				a.logger.Error("failed to delete expired image requests and responses", "err", err)
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
