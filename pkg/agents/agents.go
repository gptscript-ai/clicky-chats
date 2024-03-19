package agents

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/acorn-io/z"
	cclient "github.com/gptscript-ai/clicky-chats/pkg/client"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"

	// Blank import to register the github loader
	_ "github.com/gptscript-ai/gptscript/pkg/loader/github"
)

var emptyMessagesLimit = 500

func init() {
	if limit, err := strconv.Atoi(os.Getenv("CLICKY_CHATS_EMPTY_MESSAGES_LIMIT")); err == nil {
		emptyMessagesLimit = limit
	}
}

func StreamChatCompletionRequest(ctx context.Context, l *slog.Logger, client *http.Client, url, apiKey string, cc *db.CreateChatCompletionRequest) (<-chan db.ChatCompletionResponseChunk, error) {
	// Ensure that streaming is enabled.
	cc.Stream = z.Pointer(true)

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

func MakeChatCompletionRequest(ctx context.Context, l *slog.Logger, client *http.Client, url, apiKey string, cc *db.CreateChatCompletionRequest) (*db.CreateChatCompletionResponse, error) {
	if z.Dereference(cc.Stream) {
		l.Warn("Non-streaming chat completion call with streaming enabled, disabling streaming")
		cc.Stream = nil
	}

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
	code, err := cclient.SendRequest(client, req, resp)

	ccr := new(db.CreateChatCompletionResponse)
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
	ccr.RequestID = cc.ID
	ccr.Done = true

	return ccr, nil
}

func streamResponses(ctx context.Context, response *http.Response) <-chan db.ChatCompletionResponseChunk {
	var (
		emptyMessagesCount int
		hasError           bool

		reader = bufio.NewReader(response.Body)
		errBuf = bytes.Buffer{}
		stream = make(chan db.ChatCompletionResponseChunk, 500)
	)

	go func() {
		defer close(stream)
		defer response.Body.Close()

		for {
			rawLine, readErr := reader.ReadBytes('\n')
			if readErr != nil {
				sendChunk(ctx, stream, db.ChatCompletionResponseChunk{
					JobResponse: db.JobResponse{
						StatusCode: http.StatusInternalServerError,
						Error:      z.Pointer(readErr.Error()),
					},
				})
				return
			}

			noPrefixLine := bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(rawLine), []byte(`data: `)))

			hasError = hasError || strings.HasPrefix(string(noPrefixLine), `{"error":`)

			if len(noPrefixLine) == 0 || hasError {
				if hasError {
					_, err := errBuf.Write(noPrefixLine)
					if err != nil {
						sendChunk(ctx, stream, db.ChatCompletionResponseChunk{
							JobResponse: db.JobResponse{
								StatusCode: http.StatusInternalServerError,
								Error:      z.Pointer(fmt.Sprintf("failed to write error buffer: %v", err)),
							},
						})
						return
					}

					var ccr db.ChatCompletionResponseChunk
					if err = json.Unmarshal(errBuf.Bytes(), &ccr); err == nil {
						sendChunk(ctx, stream, ccr)
						return
					}
					// If we can't unmarshal the error yet, then we haven't received it all. Continue until we get the whole error.
				}

				emptyMessagesCount++
				if emptyMessagesCount > emptyMessagesLimit {
					sendChunk(ctx, stream, db.ChatCompletionResponseChunk{
						JobResponse: db.JobResponse{
							StatusCode: http.StatusInternalServerError,
							Error:      z.Pointer("stream has sent too many empty messages, limit is " + strconv.Itoa(emptyMessagesLimit)),
						},
					})
					return
				}

				continue
			}

			if string(noPrefixLine) == "[DONE]" {
				return
			}

			dbResponse := new(db.ChatCompletionResponseChunk)
			unmarshalErr := json.Unmarshal(noPrefixLine, dbResponse)
			if unmarshalErr != nil {
				sendChunk(ctx, stream, db.ChatCompletionResponseChunk{
					JobResponse: db.JobResponse{
						StatusCode: http.StatusInternalServerError,
						Error:      z.Pointer(fmt.Sprintf("failed to unmarshal stream message: %v", noPrefixLine)),
					},
				})
				return
			}

			if !sendChunk(ctx, stream, *dbResponse) {
				return
			}
		}
	}()

	return stream
}

// sendChunk sends a chunk to the stream. It returns false if the context is done and the stream should not continue.
func sendChunk(ctx context.Context, stream chan db.ChatCompletionResponseChunk, chunk db.ChatCompletionResponseChunk) bool {
	select {
	case <-ctx.Done():
		go func() {
			//nolint:revive
			for range stream {
			}
		}()
		return false
	case stream <- chunk:
		return true
	}
}
