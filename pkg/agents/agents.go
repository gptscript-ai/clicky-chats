package agents

import (
	"bufio"
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

	"github.com/acorn-io/broadcaster"
	"github.com/acorn-io/z"
	"github.com/gptscript-ai/gptscript/pkg/loader"
	"github.com/gptscript-ai/gptscript/pkg/runner"
	"github.com/gptscript-ai/gptscript/pkg/server"
	"github.com/thedadams/clicky-chats/pkg/db"
	"github.com/thedadams/clicky-chats/pkg/generated/openai"
	gdb "gorm.io/gorm"
)

func Start(ctx context.Context, db *db.DB) error {
	controller, err := newController(db)
	if err != nil {
		return err
	}

	controller.Start(ctx)
	return nil
}

type controller struct {
	runner      *runner.Runner
	db          *db.DB
	broadcaster *broadcaster.Broadcaster[server.Event]
}

func newController(db *db.DB) (*controller, error) {
	caster := broadcaster.New[server.Event]()
	noCacheRunner, err := runner.New(runner.Options{
		CacheOptions: runner.CacheOptions{
			Cache: new(bool),
		},
		MonitorFactory: server.NewSessionFactory(caster),
	})
	if err != nil {
		return nil, err
	}

	return &controller{
		runner:      noCacheRunner,
		db:          db,
		broadcaster: caster,
	}, nil
}

func StreamChatCompletionRequest(ctx context.Context, l *slog.Logger, client *http.Client, url, apiKey string, cc *db.ChatCompletionRequest) (<-chan db.ChatCompletionResponseChunk, error) {
	b, err := json.Marshal(cc.ToPublic())
	if err != nil {
		return nil, err
	}

	l.Debug("Making stream chat completion request", "request", string(b))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		l.Error("Failed to create chat completion", "err", err)
		return nil, err
	}

	return streamResponses(ctx, resp), nil
}

func MakeChatCompletionRequest(ctx context.Context, l *slog.Logger, client *http.Client, url, apiKey string, cc *db.ChatCompletionRequest) (*db.ChatCompletionResponse, error) {
	b, err := json.Marshal(cc.ToPublic())
	if err != nil {
		return nil, err
	}

	l.Debug("Making chat completion request", "request", string(b))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp := new(openai.CreateChatCompletionResponse)

	// Wait to process this error until after we have the DB object.
	code, err := sendRequest(client, req, resp)

	ccr := new(db.ChatCompletionResponse)
	// err here should be shadowed.
	if err := ccr.FromPublic(resp); err != nil {
		l.Error("Failed to create chat completion", "err", err)
	}

	// Process the request error here.
	if err != nil {
		l.Error("Failed to create chat completion", "err", err)
		ccr.Error = z.Pointer(err.Error())
	}

	ccr.StatusCode = code
	ccr.Base = db.Base{
		ID:        db.NewID(),
		CreatedAt: int(time.Now().Unix()),
	}
	ccr.RequestID = cc.ID
	ccr.Done = true

	return ccr, nil
}

func streamResponses(ctx context.Context, response *http.Response) <-chan db.ChatCompletionResponseChunk {
	var (
		emptyMessagesCount int

		dbResponse = new(db.ChatCompletionResponseChunk)
		reader     = bufio.NewReader(response.Body)
		stream     = make(chan db.ChatCompletionResponseChunk, 1)
	)
	go func() {
		defer close(stream)
		defer response.Body.Close()

		for {
			select {
			case <-ctx.Done():
				for range stream {
					// drain
				}
				return
			default:
			}
			rawLine, readErr := reader.ReadBytes('\n')
			if readErr != nil {
				stream <- db.ChatCompletionResponseChunk{
					JobResponse: db.JobResponse{
						Error: z.Pointer(readErr.Error()),
					},
				}
				return
			}

			noSpaceLine := bytes.TrimSpace(rawLine)
			if !bytes.HasPrefix(noSpaceLine, []byte(`data: `)) {
				emptyMessagesCount++
				if emptyMessagesCount > 300 {
					stream <- db.ChatCompletionResponseChunk{
						JobResponse: db.JobResponse{
							Error: z.Pointer("stream has sent too many empty messages"),
						},
					}
					return
				}

				continue
			}

			noPrefixLine := bytes.TrimPrefix(noSpaceLine, []byte(`data: `))
			if string(noPrefixLine) == "[DONE]" {
				return
			}

			resp := new(openai.CreateChatCompletionStreamResponse)
			unmarshalErr := json.Unmarshal(noPrefixLine, resp)
			if unmarshalErr != nil {
				stream <- db.ChatCompletionResponseChunk{
					JobResponse: db.JobResponse{
						Error: z.Pointer(unmarshalErr.Error()),
					},
				}
				return
			}

			if err := dbResponse.FromPublic(resp); err != nil {
				stream <- db.ChatCompletionResponseChunk{
					JobResponse: db.JobResponse{
						Error: z.Pointer(err.Error()),
					},
				}
				return
			}

			stream <- *dbResponse
		}
	}()

	return stream
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

// FIXME: This doesn't work and is leftover from an old implementation. Saving it for now to see if we can revive it.
func (c *controller) Start(ctx context.Context) {
	throttle := make(chan struct{}, 10)
	go func() {
		c.broadcaster.Start(ctx)
		defer c.broadcaster.Shutdown()

		sub := c.broadcaster.Subscribe()
		defer sub.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-sub.C:
				slog.Info("Got event", "event", event)
			}
		}
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				run := &db.Run{}
				if err := c.db.WithContext(ctx).Where("started_at = 0").Order("created_at desc").Limit(1).Find(run).Error; err != nil {
					if errors.Is(err, gdb.ErrRecordNotFound) {
						continue
					}
					slog.Error("Failed to find run", "err", err)
					continue
				} else if run.ID == "" || len(run.FileIDs) == 0 {
					continue
				}

				prg, err := loader.ProgramFromSource(ctx, run.Instructions, "")
				if err != nil {
					slog.Error("Failed to initialize program", "run_id", run.ID, "err", err)
					continue
				}

				select {
				case throttle <- struct{}{}:
				default:
					continue
				}

				go func() {
					defer func() {
						<-throttle
					}()

					slog.Info("Running", "run_id", run.ID)
					if err = c.db.WithContext(ctx).Model(run).Update("started_at", time.Now().Unix()).Error; err != nil {
						slog.Error("Failed to update run", "run_id", run.ID, "err", err)
						return
					}

					output, err := c.runner.Run(server.ContextWithNewID(ctx), prg, os.Environ(), run.Instructions)
					if err != nil {
						slog.Error("Failed to run", "run_id", run.ID, "err", err)
						if err = c.db.WithContext(ctx).Model(run).Update("failed_at", time.Now().Unix()).Error; err != nil {
							slog.Error("Failed to update run", "run_id", run.ID, "err", err)
						}
						return
					}

					slog.Info("Finished running", "run_id", run.ID, "output", output)

					if err = c.db.WithContext(ctx).Model(run).Update("completed_at", time.Now().Unix()).Error; err != nil {
						slog.Error("Failed to update run", "run_id", run.ID, "err", err)
					}
				}()
			}
		}
	}()
}
