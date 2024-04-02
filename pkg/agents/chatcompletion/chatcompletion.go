package chatcompletion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/agents"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"github.com/gptscript-ai/clicky-chats/pkg/trigger"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	minPollingInterval  = time.Second
	minRequestRetention = 5 * time.Minute
)

var (
	supportedModels = map[string]struct{}{
		"gpt-3.5":             {},
		"gpt-3.5-turbo":       {},
		"gpt-4":               {},
		"gpt-4-turbo-preview": {},
	}
)

type Config struct {
	Logger                                        *slog.Logger
	PollingInterval, RetentionPeriod              time.Duration
	ModelsURL, ChatCompletionURL, APIKey, AgentID string
	Trigger                                       trigger.Trigger
}

func Start(ctx context.Context, wg *sync.WaitGroup, gdb *db.DB, cfg Config) error {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default().With("agent", "chat completion")
	}
	a, err := newAgent(gdb, cfg)
	if err != nil {
		return err
	}

	if err = a.listAndStoreModels(ctx, cfg.ModelsURL); err != nil {
		return err
	}

	a.Start(ctx, wg)

	return nil
}

type agent struct {
	logger                           *slog.Logger
	pollingInterval, retentionPeriod time.Duration
	id, apiKey, url                  string
	client                           *http.Client
	db                               *db.DB
	trigger                          trigger.Trigger
}

func newAgent(db *db.DB, cfg Config) (*agent, error) {
	if cfg.PollingInterval < minPollingInterval {
		return nil, fmt.Errorf("[chatcompletion] polling interval must be at least %s", minPollingInterval)
	}
	if cfg.RetentionPeriod < minRequestRetention {
		return nil, fmt.Errorf("[chatcompletion] request retention must be at least %s", minRequestRetention)
	}

	if cfg.Trigger == nil {
		cfg.Logger.Warn("[chat completion] No trigger provided, using noop")
		cfg.Trigger = trigger.NewNoop()
	}

	return &agent{
		logger:          cfg.Logger,
		pollingInterval: cfg.PollingInterval,
		retentionPeriod: cfg.RetentionPeriod,
		client:          http.DefaultClient,
		apiKey:          cfg.APIKey,
		db:              db,
		id:              cfg.AgentID,
		url:             cfg.ChatCompletionURL,
		trigger:         cfg.Trigger,
	}, nil
}

func (a *agent) listAndStoreModels(ctx context.Context, modelsURL string) error {
	// List models
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

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
			if _, ok := supportedModels[publicModel.Id]; !ok {
				continue
			}
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
			if err = tx.Model(new(db.Model)).Delete(new(db.Model), "id = ?", id).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

func (a *agent) Start(ctx context.Context, wg *sync.WaitGroup) {
	// Start the "job runner"
	wg.Add(1)
	go func() {
		defer wg.Done()
		timer := time.NewTimer(a.pollingInterval)
		for {
			if err := a.run(ctx); err != nil {
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
	}()

	// Start cleanup
	wg.Add(1)
	go func() {
		defer wg.Done()
		cleanupInterval := a.retentionPeriod / 2
		timer := time.NewTimer(cleanupInterval)

		for {
			a.logger.Debug("Looking for completed chat completions")
			var runToolObjects []db.RunToolObject

			if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				if err := tx.Model(new(db.RunToolObject)).Where("created_at < ? AND done = true", int(time.Now().Add(-a.retentionPeriod).Unix())).Find(&runToolObjects).Error; err != nil {
					return err
				}
				if len(runToolObjects) == 0 {
					return nil
				}

				requestIDs := make([]string, 0, len(runToolObjects))
				for _, rt := range runToolObjects {
					requestIDs = append(requestIDs, rt.ID)
				}

				if err := tx.Delete(new(db.RunStepEvent), "request_id IN ?", requestIDs).Error; err != nil {
					return err
				}

				return tx.Delete(runToolObjects).Error
			}); err != nil {
				a.logger.Error("Failed to cleanup chat completions", "err", err)
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

func (a *agent) run(ctx context.Context) error {
	a.logger.Debug("Checking for a chat completion request")
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
			a.logger.Error("Failed to get chat completion", "err", err)
		}

		return err
	}

	chatCompletionID := cc.ID
	l := a.logger.With("id", chatCompletionID)

	url := cc.ModelAPI
	if url == "" {
		url = a.url
	}

	l.Debug("Found chat completion", "cc", cc)
	if z.Dereference(cc.Stream) {
		l.Debug("Counting prompt tokens...")
		var promptTokens int
		if treq, err := agents.Transform[agents.TokenRequest](cc.ToPublic()); err != nil {
			l.Error("failed to transform chat completion to token request, prompt token usage omitted", "err", err)
		} else {
			promptTokens, err = agents.PromptTokens(cc.Model, &treq)
			if err != nil {
				l.Error("failed to count prompt tokens", "err", err)
			}
		}

		l.Debug("Streaming chat completion...")

		stream, err := agents.StreamChatCompletionRequest(ctx, l, a.client, url, a.apiKey, cc)
		if err != nil {
			l.Error("Failed to stream chat completion request", "err", err)
			return err
		}

		if err = streamResponses(l, a.db.WithContext(ctx), chatCompletionID, promptTokens, stream); err != nil {
			l.Error("Failed to stream chat completion responses", "err", err)
		}

		return nil
	}

	ccr, err := agents.MakeChatCompletionRequest(ctx, l, a.client, url, a.apiKey, cc)
	if err != nil {
		l.Error("Failed to make chat completion request", "err", err)
		return err
	}

	l.Debug("Made chat completion request", "status_code", ccr.StatusCode, "err", ccr.Error)

	if err = a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err = db.Create(tx, ccr); err != nil {
			return err
		}
		return tx.Model(cc).Where("id = ?", chatCompletionID).Update("done", true).Error
	}); err != nil {
		l.Error("Failed to create chat completion response", "err", err)
		return err
	}

	a.trigger.Ready(chatCompletionID)
	return nil
}

func streamResponses(l *slog.Logger, gdb *gorm.DB, chatCompletionID string, promptTokens int, stream <-chan db.ChatCompletionResponseChunk) error {
	var (
		index int
		errs  []error
	)
	for chunk := range stream {
		chunk.RequestID = chatCompletionID
		chunk.ResponseIdx = index
		index++
		if err := db.Create(gdb, &chunk); err != nil {
			l.Error("Failed to create chat completion response chunk", "err", err)
			errs = append(errs, err)
		}
	}

	completionTokens := index - 1
	chunk := &db.ChatCompletionResponseChunk{
		JobResponse: db.JobResponse{
			RequestID: chatCompletionID,
			Done:      true,
		},
		ResponseIdx: index,
		Usage: datatypes.NewJSONType(&openai.CompletionUsage{
			CompletionTokens: completionTokens,
			PromptTokens:     promptTokens,
			TotalTokens:      completionTokens + promptTokens,
		}),
	}
	if err := gdb.Transaction(func(tx *gorm.DB) error {
		if err := db.Create(tx, chunk); err != nil {
			return err
		}

		return tx.Model(new(db.CreateChatCompletionRequest)).Where("id = ?", chatCompletionID).Update("done", true).Error
	}); err != nil {
		l.Error("Failed to create final chat completion response chunk", "err", err)
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}
