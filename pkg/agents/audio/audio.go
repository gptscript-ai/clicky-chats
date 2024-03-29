package audio

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
	a, err := newAgent(gdb, cfg)
	if err != nil {
		return err
	}

	a.Start(ctx, wg)

	return nil
}

type Config struct {
	PollingInterval, RetentionPeriod time.Duration
	AudioBaseURL, APIKey, AgentID    string
	Trigger                          trigger.Trigger
}

type agent struct {
	pollingInterval, requestRetention             time.Duration
	id, apiKey                                    string
	speechURL, translationsURL, transcriptionsURL string
	client                                        *http.Client
	db                                            *db.DB
	trigger                                       trigger.Trigger
}

func newAgent(db *db.DB, cfg Config) (*agent, error) {
	if cfg.PollingInterval < minPollingInterval {
		return nil, fmt.Errorf("[audio] polling interval must be at least %s", minPollingInterval)
	}
	if cfg.RetentionPeriod < minRequestRetention {
		return nil, fmt.Errorf("[audio] request retention must be at least %s", minRequestRetention)
	}

	if cfg.Trigger == nil {
		slog.Warn("[audio] No trigger provided, using noop")
		cfg.Trigger = trigger.NewNoop()
	}

	return &agent{
		pollingInterval:   cfg.PollingInterval,
		requestRetention:  cfg.RetentionPeriod,
		speechURL:         cfg.AudioBaseURL + "/speech",
		translationsURL:   cfg.AudioBaseURL + "/translations",
		transcriptionsURL: cfg.AudioBaseURL + "/transcriptions",
		client:            http.DefaultClient,
		apiKey:            cfg.APIKey,
		db:                db,
		id:                cfg.AgentID,
		trigger:           cfg.Trigger,
	}, nil
}

func (a *agent) Start(ctx context.Context, wg *sync.WaitGroup) {
	// Start the "job runner"
	for _, run := range []func(context.Context) error{
		a.runSpeech,
		a.runTranslations,
		a.runTranscriptions,
	} {
		wg.Add(1)
		go func(r func(context.Context) error) {
			defer wg.Done()
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
	wg.Add(1)
	go func() {
		defer wg.Done()
		var (
			cleanupInterval = a.requestRetention / 2
			jobObjects      = []db.Storer{
				new(db.CreateSpeechRequest),
				new(db.CreateSpeechResponse),
				new(db.CreateTranslationRequest),
				new(db.CreateTranslationResponse),
				new(db.CreateTranscriptionRequest),
				new(db.CreateTranscriptionResponse),
			}
			cdb   = a.db.WithContext(ctx)
			timer = time.NewTimer(cleanupInterval)
		)
		for {
			slog.Debug("looking for expired audio requests and responses")
			expiration := time.Now().Add(-a.requestRetention)
			if err := db.DeleteExpired(cdb, expiration, jobObjects...); err != nil {
				slog.Error("failed to delete expired audio requests and responses", "err", err)
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
