package image

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/trigger"
	"gorm.io/gorm"
)

const (
	minPollingInterval  = time.Second
	minRequestRetention = 5 * time.Minute
)

func Start(ctx context.Context, gdb *db.DB, cfg Config) error {
	a, err := newAgent(gdb, cfg)
	if err != nil {
		return err
	}

	a.Start(ctx)

	return nil
}

type Config struct {
	PollingInterval, RetentionPeriod time.Duration
	ImagesBaseURL, APIKey, AgentID   string
	Trigger                          trigger.Trigger
}

type agent struct {
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
		slog.Warn("[image] No trigger provided, using noop")
		cfg.Trigger = trigger.NewNoop()
	}

	return &agent{
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

func (a *agent) Start(ctx context.Context) {
	// Start the "job runner"
	for _, run := range []func(context.Context) error{
		a.runGenerations,
		a.runEdits,
		a.runVariations,
	} {
		go func(r func(context.Context) error) {
			timer := time.NewTimer(a.pollingInterval)
			for {
				if err := r(ctx); err != nil {
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
	go func() {
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
			slog.Debug("looking for expired image requests and responses")
			expiration := time.Now().Add(-a.requestRetention)
			if err := db.DeleteExpired(cdb, expiration, jobObjects...); err != nil {
				slog.Error("failed to delete expired image requests and responses", "err", err)
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
