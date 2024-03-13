package image

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gptscript-ai/clicky-chats/pkg/db"
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
}

type agent struct {
	pollingInterval, requestRetention       time.Duration
	id, apiKey                              string
	generationsURL, editsURL, variationsURL string
	client                                  *http.Client
	db                                      *db.DB
}

func newAgent(db *db.DB, cfg Config) (*agent, error) {
	if cfg.PollingInterval < minPollingInterval {
		return nil, fmt.Errorf("polling interval must be at least %s", minPollingInterval)
	}
	if cfg.RetentionPeriod < minRequestRetention {
		return nil, fmt.Errorf("request retention must be at least %s", minRequestRetention)
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
	}, nil
}

func (a *agent) Start(ctx context.Context) {
	// Start the "job runner"
	for _, run := range []func(context.Context) error{
		a.runGenerations,
		a.runEdits,
		a.runVariations,
	} {
		r := run
		go func() {
			for {
				if err := r(ctx); err != nil {
					if !errors.Is(err, gorm.ErrRecordNotFound) {
						slog.Error("failed run iteration", "err", err)
					}

					select {
					case <-ctx.Done():
						return
					case <-time.After(a.pollingInterval):
					}
				}
			}
		}()
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
			cdb = a.db.WithContext(ctx)
		)
		for {
			slog.Debug("looking for expired image requests and responses")
			expiration := time.Now().Add(-a.requestRetention)
			if err := db.DeleteExpired(cdb, expiration, jobObjects...); err != nil {
				slog.Error("failed to delete expired image requests and responses", "err", err)
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(cleanupInterval):
			}
		}
	}()
}
