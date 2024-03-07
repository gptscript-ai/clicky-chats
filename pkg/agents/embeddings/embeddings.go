package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/agents/utils"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/gorm"
)

type Config struct {
	PollingInterval, CleanupTickTime time.Duration
	EmbeddingsURL, APIKey, AgentID   string
}

func Start(ctx context.Context, gdb *db.DB, cfg Config) error {
	a, err := newAgent(gdb, cfg)
	if err != nil {
		return err
	}

	// Models are listed and stored by the chat completion agent - this includes embedding models

	a.Start(ctx, cfg.PollingInterval, cfg.CleanupTickTime)
	return nil
}

type agent struct {
	id, apiKey, url string
	client          *http.Client
	db              *db.DB
}

func newAgent(db *db.DB, cfg Config) (*agent, error) {
	return &agent{
		client: http.DefaultClient,
		apiKey: cfg.APIKey,
		db:     db,
		id:     cfg.AgentID,
		url:    cfg.EmbeddingsURL,
	}, nil
}

func (c *agent) Start(ctx context.Context, pollingInterval, cleanupTickTime time.Duration) {
	/*
	 * Embeddings Runner
	 */

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				slog.Debug("Checking for an embeddings request")
				// Look for a new embeddings request and claim it.
				embedreq := new(db.EmbeddingsRequest)
				if err := c.db.WithContext(ctx).Model(embedreq).Transaction(func(tx *gorm.DB) error {
					if err := tx.Where("claimed_by IS NULL").Or("claimed_by = ? AND done = false", c.id).Order("created_at desc").First(embedreq).Error; err != nil {
						return err
					}

					if err := tx.Where("id = ?", embedreq.ID).Updates(map[string]interface{}{"claimed_by": c.id}).Error; err != nil {
						return err
					}

					return nil
				}); err != nil {
					if !errors.Is(err, gorm.ErrRecordNotFound) {
						slog.Error("Failed to get embeddings requests", "err", err)
					}
					time.Sleep(pollingInterval)
					continue
				}

				embeddingsID := embedreq.ID
				l := slog.With("type", "embeddings", "id", embeddingsID)

				url := embedreq.ModelAPI
				if url == "" {
					url = c.url
				}

				l.Debug("Found embeddings request", "er", embedreq)

				embedresp, err := makeEmbeddingsRequest(ctx, l, c.client, url, c.apiKey, embedreq)
				if err != nil {
					l.Error("Failed to make embeddings request", "err", err)
					time.Sleep(pollingInterval)
					continue
				}

				l.Debug("Made embeddings request", "status_code", embedresp.StatusCode)

				if err = c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
					if err = db.Create(tx, embedresp); err != nil {
						return err
					}
					return tx.Model(embedreq).Where("id = ?", embeddingsID).Update("done", true).Error
				}); err != nil {
					l.Error("Failed to create embeddings response", "err", err)
				}
			}
		}
	}()

	/*
	 * Cleanup Job
	 */
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(cleanupTickTime):
				slog.Debug("Looking for completed embeddings that we can cleanup")

				var (
					er []db.EmbeddingsResponse
				)
				if err := c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
					if err := tx.Model(new(db.EmbeddingsResponse)).Find(&er).Error; err != nil {
						return err
					}
					if len(er) == 0 {
						return nil
					}

					requestIDs := make([]string, 0, len(er))
					for _, cc := range er {
						if id := cc.RequestID; id != "" {
							requestIDs = append(requestIDs, id)
						}
					}

					// Delete related embeddings requests
					if err := tx.Delete(new(db.EmbeddingsRequest), "id IN ? AND done = true", requestIDs).Error; err != nil {
						return err
					}

					// Delete the embeddings response itself
					if err := tx.Delete(er).Error; err != nil {
						return err
					}

					return nil
				}); err != nil {
					slog.Error("Failed to cleanup embeddings requests/responses", "err", err)
				}
			}
		}
	}()
}

func makeEmbeddingsRequest(ctx context.Context, l *slog.Logger, client *http.Client, url, apiKey string, er *db.EmbeddingsRequest) (*db.EmbeddingsResponse, error) {

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
	code, err := utils.SendRequest(client, req, resp)

	embedresp := new(db.EmbeddingsResponse)
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
