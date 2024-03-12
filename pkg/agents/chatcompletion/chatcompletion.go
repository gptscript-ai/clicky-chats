package chatcompletion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/agents"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/gorm"
)

const (
	minPollingInterval  = time.Second
	minRequestRetention = 5 * time.Minute
)

type Config struct {
	PollingInterval, RetentionPeriod              time.Duration
	ModelsURL, ChatCompletionURL, APIKey, AgentID string
}

func Start(ctx context.Context, gdb *db.DB, cfg Config) error {
	a, err := newAgent(gdb, cfg)
	if err != nil {
		return err
	}

	if err = a.listAndStoreModels(ctx, cfg.ModelsURL); err != nil {
		return err
	}

	a.Start(ctx)

	return nil
}

type agent struct {
	pollingInterval, retentionPeriod time.Duration
	id, apiKey, url                  string
	client                           *http.Client
	db                               *db.DB
}

func newAgent(db *db.DB, cfg Config) (*agent, error) {
	if cfg.PollingInterval < minPollingInterval {
		return nil, fmt.Errorf("polling interval must be at least %s", minPollingInterval)
	}
	if cfg.RetentionPeriod < minRequestRetention {
		return nil, fmt.Errorf("request retention must be at least %s", minRequestRetention)
	}

	return &agent{
		pollingInterval: cfg.PollingInterval,
		retentionPeriod: cfg.RetentionPeriod,
		client:          http.DefaultClient,
		apiKey:          cfg.APIKey,
		db:              db,
		id:              cfg.AgentID,
		url:             cfg.ChatCompletionURL,
	}, nil
}

func (a *agent) listAndStoreModels(ctx context.Context, modelsURL string) error {
	// List models
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	req.Header.Add("Authorization", "Bearer "+a.apiKey)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to list models: %s", resp.Status)
	}

	type models struct {
		Data []*openai.Model `json:"data"`
	}

	var m models
	if err = json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return err
	}

	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var dbModels []db.Model
		if err = tx.Model(new(db.Model)).Find(&dbModels).Error; err != nil {
			return err
		}

		dbModelIDs := make(map[string]struct{}, len(dbModels))
		for _, model := range dbModels {
			dbModelIDs[model.ID] = struct{}{}
		}

		for _, publicModel := range m.Data {
			if _, ok := dbModelIDs[publicModel.Id]; ok {
				delete(dbModelIDs, publicModel.Id)
				continue
			}

			model := new(db.Model)
			if err = model.FromPublic(publicModel); err != nil {
				return err
			}

			// Create the model directly instead of using the db ops because the ID is already set.
			if err = tx.Model(&db.Model{}).Create(model).Error; err != nil && !errors.Is(err, gorm.ErrDuplicatedKey) {
				return err
			}

			delete(dbModelIDs, model.ID)
		}

		for id := range dbModelIDs {
			if err = tx.Model(new(db.Model)).Delete(new(db.Model), id).Error; err != nil {
				return err
			}
		}

		return nil
	})
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
		cleanupInterval := a.retentionPeriod / 2

		for {
			slog.Debug("Looking for completed chat completions")
			// Look for a new chat completion request and claim it.
			var (
				ccs  []db.CreateChatCompletionResponse
				cccs []db.ChatCompletionResponseChunk
			)
			if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				if err := tx.Model(new(db.CreateChatCompletionResponse)).Find(&ccs).Error; err != nil {
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

				if err := tx.Delete(new(db.CreateChatCompletionRequest), "id IN ? AND done = true", requestIDs).Error; err != nil {
					return err
				}

				if len(ccs) != 0 {
					if err := tx.Delete(ccs).Error; err != nil {
						return err
					}
				}

				if len(cccs) != 0 {
					if err := tx.Delete(cccs).Error; err != nil {
						return err
					}
				}

				return nil
			}); err != nil {
				slog.Error("Failed to cleanup chat completions", "err", err)
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
	slog.Debug("Checking for a chat completion request")
	// Look for a new chat completion request and claim it.
	cc := new(db.CreateChatCompletionRequest)
	if err := a.db.WithContext(ctx).Model(cc).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("claimed_by IS NULL").Or("claimed_by = ? AND done = false", a.id).Order("created_at desc").First(cc).Error; err != nil {
			return err
		}

		if err := tx.Where("id = ?", cc.ID).Updates(map[string]interface{}{"claimed_by": a.id}).Error; err != nil {
			return err
		}

		return nil
	}); err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			slog.Error("Failed to get chat completion", "err", err)
		}

		return err
	}

	chatCompletionID := cc.ID
	l := slog.With("type", "chatcompletion", "id", chatCompletionID)

	url := cc.ModelAPI
	if url == "" {
		url = a.url
	}

	l.Debug("Found chat completion", "cc", cc)
	if z.Dereference(cc.Stream) {
		l.Debug("Streaming chat completion...")
		stream, err := agents.StreamChatCompletionRequest(ctx, l, a.client, url, a.apiKey, cc)
		if err != nil {
			l.Error("Failed to stream chat completion request", "err", err)
			return err
		}

		if err = streamResponses(a.db.WithContext(ctx), chatCompletionID, stream); err != nil {
			l.Error("Failed to stream chat completion responses", "err", err)
		}

		return nil
	}

	ccr, err := agents.MakeChatCompletionRequest(ctx, l, a.client, url, a.apiKey, cc)
	if err != nil {
		l.Error("Failed to make chat completion request", "err", err)
		return err
	}

	l.Debug("Made chat completion request", "status_code", ccr.StatusCode)

	if err = a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err = db.Create(tx, ccr); err != nil {
			return err
		}
		return tx.Model(cc).Where("id = ?", chatCompletionID).Update("done", true).Error
	}); err != nil {
		l.Error("Failed to create chat completion response", "err", err)
		return err
	}

	return nil
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

		return tx.Model(new(db.CreateChatCompletionRequest)).Where("id = ?", chatCompletionID).Update("done", true).Error
	}); err != nil {
		slog.Error("Failed to create final chat completion response chunk", "err", err)
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}
