package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	cclient "github.com/gptscript-ai/clicky-chats/pkg/client"

	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/gorm"
)

const (
	minPollingInterval  = time.Second
	minRequestRetention = 5 * time.Minute
)

type Config struct {
	PollingInterval, RetentionPeriod time.Duration
	EmbeddingsURL, APIKey, AgentID   string
}

func Start(ctx context.Context, gdb *db.DB, cfg Config) error {
	a, err := newAgent(gdb, cfg)
	if err != nil {
		return err
	}

	// Models are listed and stored by the chat completion agent - this includes embedding models

	a.Start(ctx)
	return nil
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
		client:           http.DefaultClient,
		apiKey:           cfg.APIKey,
		db:               db,
		id:               cfg.AgentID,
		url:              cfg.EmbeddingsURL,
	}, nil
}

func (a *agent) Start(ctx context.Context) {
	/*
	 * Embeddings Runner
	 */

	go func() {
		for {
			if err := a.run(ctx); err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					slog.Error("failed embeddings iteration", "err", err)
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(a.pollingInterval):
				}
			}
		}
	}()

	/*
	 * Cleanup Job
	 */
	go func() {
		var (
			cleanupInterval = a.requestRetention / 2
			jobObjects      = []db.Storer{
				new(db.CreateEmbeddingRequest),
				new(db.CreateEmbeddingResponse),
			}
			cdb = a.db.WithContext(ctx)
		)
		for {
			slog.Debug("Looking for expired create embeddings requests and responses that we can cleanup")
			expiration := time.Now().Add(-a.requestRetention)
			if err := db.DeleteExpired(cdb, expiration, jobObjects...); err != nil {
				slog.Error("failed to delete expired embeddings requests/responses", "err", err)
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
	slog.Debug("Checking for an embeddings request to process")
	// Look for a new embeddings request and claim it.
	embedreq := new(db.CreateEmbeddingRequest)
	if err := a.db.WithContext(ctx).Model(embedreq).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("claimed_by IS NULL").Or("claimed_by = ? AND done = false", a.id).
			Order("created_at desc").
			First(embedreq).Error; err != nil {
			return err
		}

		if err := tx.Where("id = ?", embedreq.ID).
			Updates(map[string]interface{}{"claimed_by": a.id}).Error; err != nil {
			return err
		}

		return nil
	}); err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to get embeddings request: %w", err)
		}
		return err
	}

	embeddingsID := embedreq.ID
	l := slog.With("type", "embeddings", "id", embeddingsID)
	l.Debug("Processing request")

	url := embedreq.ModelAPI
	if url == "" {
		url = a.url
	}

	l.Debug("Found embeddings request", "er", embedreq)

	embedresp, err := makeEmbeddingsRequest(ctx, l, a.client, url, a.apiKey, embedreq)
	if err != nil {
		return fmt.Errorf("failed to make embeddings request: %w", err)
	}

	l.Debug("Made embeddings request", "status_code", embedresp.StatusCode)

	if err = a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err = db.Create(tx, embedresp); err != nil {
			return err
		}
		return tx.Model(embedreq).Where("id = ?", embeddingsID).Update("done", true).Error
	}); err != nil {
		l.Error("Failed to create embeddings response", "err", err)
	}

	return nil
}

func makeEmbeddingsRequest(ctx context.Context, l *slog.Logger, client *http.Client, url, apiKey string, er *db.CreateEmbeddingRequest) (*db.CreateEmbeddingResponse, error) {
	b, err := json.Marshal(er.ToPublic())
	if err != nil {
		return nil, err
	}

	l.Debug("Making embeddings request", "request", string(b))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp := new(openai.CreateEmbeddingResponse)

	// Wait to process this error until after we have the DB object.
	code, err := cclient.SendRequest(client, req, resp)

	embedresp := new(db.CreateEmbeddingResponse)
	// err here should be shadowed.
	if err := embedresp.FromPublic(resp); err != nil {
		l.Error("Failed to create embeddings", "err", err)
	}

	// Process the request error here.
	if err != nil {
		l.Error("Failed to create embeddings", "err", err)
		embedresp.Error = z.Pointer(err.Error())
	}

	embedresp.StatusCode = code
	embedresp.RequestID = er.ID
	embedresp.Done = true

	return embedresp, nil
}
