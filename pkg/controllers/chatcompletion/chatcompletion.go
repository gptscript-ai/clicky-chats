package chatcompletion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/acorn-io/z"
	"github.com/google/uuid"
	"github.com/thedadams/clicky-chats/pkg/db"
	"github.com/thedadams/clicky-chats/pkg/generated/openai"
	gdb "gorm.io/gorm"
)

const (
	openAIAPIURL             = "https://api.openai.com/v1"
	openAIChatCompletionsURL = openAIAPIURL + "/chat/completions"
)

func Start(ctx context.Context, gdb *gdb.DB, cleanupTickTime time.Duration) error {
	controller, err := newController(gdb)
	if err != nil {
		return err
	}

	controller.Start(ctx, cleanupTickTime)
	return nil
}

type controller struct {
	id, apiKey string
	client     *http.Client
	db         *gdb.DB
}

func newController(db *gdb.DB) (*controller, error) {
	return &controller{
		client: http.DefaultClient,
		apiKey: os.Getenv("OPENAI_API_KEY"),
		db:     db,
		id:     "cha boy",
	}, nil
}

func (c *controller) Start(ctx context.Context, cleanupTickTime time.Duration) {
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
				if err := c.db.Model(cc).Transaction(func(tx *gdb.DB) error {
					if err := tx.Where("claimed_by IS NULL").Or("claimed_by = ? AND response_id IS NULL", c.id).Order("created_at desc").First(cc).Error; err != nil {
						return err
					}

					if err := tx.Where("id = ?", cc.ID).Updates(map[string]interface{}{"claimed_by": c.id}).Error; err != nil {
						return err
					}

					return nil
				}); err != nil {
					if !errors.Is(err, gdb.ErrRecordNotFound) {
						slog.Error("Failed to get chat completion", "err", err)
					}
					time.Sleep(5 * time.Second)
					continue
				}

				chatCompletionID := cc.ID

				// Reset job information so that it doesn't get included in the request.
				cc.JobRequest = db.JobRequest{}

				slog.Debug("Found chat completion", "cc", cc, "id", chatCompletionID)
				ccr, err := c.makeChatCompletionRequest(ctx, cc)
				if err != nil {
					slog.Error("Failed to make chat completion request", "err", err)
					time.Sleep(5 * time.Second)
					continue
				}

				slog.Debug("Made chat completion request", "status_code", ccr.StatusCode)

				if err = c.db.Transaction(func(tx *gdb.DB) error {
					if err = tx.Create(ccr).Error; err != nil {
						return err
					}
					return tx.Model(cc).Where("id = ?", chatCompletionID).Update("response_id", ccr.ID).Error
				}); err != nil {
					slog.Error("Failed to create chat completion response", "err", err)
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
				var ccs []db.ChatCompletionRequest
				if err := c.db.Transaction(func(tx *gdb.DB) error {
					if err := tx.Model(new(db.ChatCompletionRequest)).Where("response_id IS NOT NULL").Order("created_at desc").Find(&ccs).Error; err != nil {
						return err
					}
					if len(ccs) == 0 {
						return nil
					}

					responseIDs := make([]string, 0, len(ccs))
					for _, cc := range ccs {
						if id := z.Dereference(cc.ResponseID); id != "" {
							responseIDs = append(responseIDs, id)
						}
					}

					if err := tx.Delete(new(db.ChatCompletionResponse), "id IN ?", responseIDs).Error; err != nil {
						return err
					}

					return tx.Delete(ccs).Error
				}); err != nil {
					slog.Error("Failed to cleanup chat completions", "err", err)
				}
			}
		}
	}()
}

func (c *controller) makeChatCompletionRequest(ctx context.Context, cc *db.ChatCompletionRequest) (*db.ChatCompletionResponse, error) {
	b, err := json.Marshal(cc.ToPublic())
	if err != nil {
		return nil, err
	}

	slog.Debug("Making chat completion request", "request", string(b))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIChatCompletionsURL, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp := new(openai.CreateChatCompletionResponse)

	// Wait to process this error until after we have the DB object.
	code, err := sendRequest(c.client, req, resp)

	ccr := new(db.ChatCompletionResponse)
	// err here should be shadowed.
	if err := ccr.FromPublic(resp); err != nil {
		slog.Error("Failed to create chat completion", "err", err)
	}

	// Process the request error here.
	if err != nil {
		slog.Error("Failed to create chat completion", "err", err)
		ccr.Error = z.Pointer(err.Error())
	}

	ccr.StatusCode = code
	ccr.Base = db.Base{
		ID:        uuid.New().String(),
		CreatedAt: int(time.Now().Unix()),
	}

	return ccr, nil
}

func sendRequest(client *http.Client, req *http.Request, respObj any) (int, error) {
	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}

	defer res.Body.Close()

	statusCode := res.StatusCode
	if statusCode < http.StatusOK || statusCode >= http.StatusBadRequest {
		return statusCode, decodeError(res)
	}

	if err = json.NewDecoder(res.Body).Decode(respObj); err != nil {
		return http.StatusInternalServerError, err
	}

	return statusCode, nil
}

func decodeError(resp *http.Response) error {
	s, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read body for error response: %w", err)
	}

	return fmt.Errorf("%s", s)
}
