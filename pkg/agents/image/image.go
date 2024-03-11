package image

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/acorn-io/z"
	cclient "github.com/gptscript-ai/clicky-chats/pkg/client"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/gorm"
)

const (
	minPollingInterval  = 1 * time.Second
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
	ImagesURL, APIKey, AgentID       string
}

type agent struct {
	pollingInterval, requestRetention time.Duration
	id, apiKey, url                   string
	client                            *http.Client
	db                                *db.DB
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
		url:              cfg.ImagesURL,
		client:           http.DefaultClient,
		apiKey:           cfg.APIKey,
		db:               db,
		id:               cfg.AgentID,
	}, nil
}

func (a *agent) Start(ctx context.Context) {
	// Start the "job runner"
	go func() {
		for {
			if err := a.run(ctx); err != nil {
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

	// Start cleanup
	go func() {
		var (
			cleanupInterval = a.requestRetention / 2
			jobObjects      = []db.Storer{
				new(db.CreateImageRequest),
				new(db.ImagesResponse),
			}
			cdb = a.db.WithContext(ctx)
		)
		for {
			slog.Debug("Looking for expired create image requests and responses")
			expiration := time.Now().Add(-a.requestRetention)
			if err := db.DeleteExpired(cdb, expiration, jobObjects...); err != nil {
				slog.Error("failed to delete expired create image requests", "err", err)
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(cleanupInterval):
			}
		}
	}()
}

func (a *agent) run(ctx context.Context) error {
	slog.Debug("Checking for an image request to process")
	// Look for a new create image request and claim it. Also, query for the other objects we need.
	createRequest := new(db.CreateImageRequest)
	if err := a.db.WithContext(ctx).Model(createRequest).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("claimed_by IS NULL").Or("claimed_by = ? AND done = false", a.id).
			Order("created_at desc").
			First(createRequest).Error; err != nil {
			return err
		}

		if err := tx.Where("id = ?", createRequest.ID).
			Updates(map[string]interface{}{"claimed_by": a.id}).Error; err != nil {
			return err
		}

		return nil
	}); err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to get create image request: %w", err)
		}
		return err
	}

	l := slog.With("type", "createimage", "id", createRequest.ID)
	l.Debug("Processing request")

	data, err := json.Marshal(createRequest.ToPublic())
	if err != nil {
		return fmt.Errorf("failed to marshal create image request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}

	oir, ir := new(openai.ImagesResponse), new(db.ImagesResponse)
	code, err := cclient.SendRequest(a.client, req, oir)
	if err := ir.FromPublic(oir); err != nil {
		l.Error("Failed to create image", "err", err)
	}

	// Process the request error here.
	if err != nil {
		l.Error("Failed to create image", "err", err)
		ir.Error = z.Pointer(err.Error())
	}

	ir.StatusCode = code
	ir.RequestID = createRequest.ID
	ir.Done = true

	// Store the completed response and mark the request as done.
	if err = a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err = db.Create(tx, ir); err != nil {
			return err
		}
		return tx.Model(createRequest).Where("id = ?", createRequest.ID).Update("done", true).Error
	}); err != nil {
		l.Error("Failed to create images response", "err", err)
	}

	return nil
}
