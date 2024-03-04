package chatcompletion

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/acorn-io/z"
	"github.com/thedadams/clicky-chats/pkg/agents"
	"github.com/thedadams/clicky-chats/pkg/db"
	"gorm.io/gorm"
)

type Config struct {
	PollingInterval, CleanupTickTime time.Duration
	APIURL, APIKey, AgentID          string
}

func Start(ctx context.Context, gdb *db.DB, cfg Config) error {
	a, err := newAgent(gdb, cfg)
	if err != nil {
		return err
	}

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
		url:    cfg.APIURL,
	}, nil
}

func (c *agent) Start(ctx context.Context, pollingInterval time.Duration, cleanupTickTime time.Duration) {
	// Start the "job runner"
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				slog.Debug("Checking for a chat completion request")
				// Look for a new chat completion request and claim it.
				cc := new(db.ChatCompletionRequest)
				if err := c.db.WithContext(ctx).Model(cc).Transaction(func(tx *gorm.DB) error {
					if err := tx.Where("claimed_by IS NULL").Or("claimed_by = ? AND done = false", c.id).Order("created_at desc").First(cc).Error; err != nil {
						return err
					}

					if err := tx.Where("id = ?", cc.ID).Updates(map[string]interface{}{"claimed_by": c.id}).Error; err != nil {
						return err
					}

					return nil
				}); err != nil {
					if !errors.Is(err, gorm.ErrRecordNotFound) {
						slog.Error("Failed to get chat completion", "err", err)
					}
					time.Sleep(pollingInterval)
					continue
				}

				chatCompletionID := cc.ID
				l := slog.With("type", "chatcompletion", "id", chatCompletionID)

				url := cc.ModelAPI
				if url == "" {
					url = c.url
				}

				l.Debug("Found chat completion", "cc", cc)
				if z.Dereference(cc.Stream) {
					l.Debug("Streaming chat completion...")
					stream, err := agents.StreamChatCompletionRequest(ctx, l, c.client, url, c.apiKey, cc)
					if err != nil {
						l.Error("Failed to stream chat completion request", "err", err)
						time.Sleep(pollingInterval)
						continue
					}

					if err = streamResponses(c.db.WithContext(ctx), chatCompletionID, stream); err != nil {
						l.Error("Failed to stream chat completion responses", "err", err)
					}

					continue
				}

				ccr, err := agents.MakeChatCompletionRequest(ctx, l, c.client, url, c.apiKey, cc)
				if err != nil {
					l.Error("Failed to make chat completion request", "err", err)
					time.Sleep(pollingInterval)
					continue
				}

				l.Debug("Made chat completion request", "status_code", ccr.StatusCode)

				if err = c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
					if err = tx.Create(ccr).Error; err != nil {
						return err
					}
					return tx.Model(cc).Where("id = ?", chatCompletionID).Update("done", true).Error
				}); err != nil {
					l.Error("Failed to create chat completion response", "err", err)
				}
			}
		}
	}()

	// Start cleanup
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(cleanupTickTime):
				slog.Debug("Looking for completed chat completions")
				// Look for a new chat completion request and claim it.
				var (
					ccs  []db.ChatCompletionResponse
					cccs []db.ChatCompletionResponseChunk
				)
				if err := c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
					if err := tx.Model(new(db.ChatCompletionResponse)).Find(&ccs).Error; err != nil {
						return err
					}
					if err := tx.Model(new(db.ChatCompletionResponseChunk)).Find(&cccs).Error; err != nil {
						return err
					}
					if len(ccs)+len(cccs) == 0 {
						return nil
					}

					requestIDs := make([]string, 0, len(ccs)+len(cccs))
					for _, cc := range ccs {
						if id := cc.RequestID; id != "" {
							requestIDs = append(requestIDs, id)
						}
					}
					for _, ccc := range cccs {
						if id := ccc.RequestID; id != "" {
							requestIDs = append(requestIDs, id)
						}
					}

					if err := tx.Delete(new(db.ChatCompletionRequest), "id IN ? AND done = true", requestIDs).Error; err != nil {
						return err
					}

					if err := tx.Delete(ccs).Error; err != nil {
						return err
					}

					if err := tx.Delete(cccs).Error; err != nil {
						return err
					}

					return nil
				}); err != nil {
					slog.Error("Failed to cleanup chat completions", "err", err)
				}
			}
		}
	}()
}

func streamResponses(gdb *gorm.DB, chatCompletionID string, stream <-chan db.ChatCompletionResponseChunk) error {
	var (
		index int
		errs  []error
	)
	for chunk := range stream {
		chunk.RequestID = chatCompletionID
		chunk.ResponseIdx = index
		index++
		if err := db.Create(gdb, &chunk); err != nil {
			slog.Error("Failed to create chat completion response chunk", "err", err)
			errs = append(errs, err)
		}
	}

	chunk := &db.ChatCompletionResponseChunk{
		JobResponse: db.JobResponse{
			RequestID: chatCompletionID,
			Done:      true,
		},
		ResponseIdx: index,
	}
	if err := gdb.Transaction(func(tx *gorm.DB) error {
		if err := db.Create(tx, chunk); err != nil {
			return err
		}

		return tx.Model(new(db.ChatCompletionRequest)).Where("id = ?", chatCompletionID).Update("done", true).Error
	}); err != nil {
		slog.Error("Failed to create final chat completion response chunk", "err", err)
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}
