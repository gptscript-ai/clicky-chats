package agents

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/acorn-io/z"
	cclient "github.com/gptscript-ai/clicky-chats/pkg/client"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	// Blank import to register the github loader
	_ "github.com/gptscript-ai/gptscript/pkg/loader/github"
)

const (
	GPTScriptRunnerType     = "gptscript"
	GPTScriptToolNamePrefix = "gptscript_"
	SkipLoadingTool         = "<skip>"
)

var builtInFunctionNameToDefinition = map[string]ToolDefinition{
	// Skipping the loading of the web_search tool because it is currently not working.
	"web_browsing": {Link: "github.com/gptscript-ai/question-answerer", SubTool: "question-answerer-ddg"},
	// TODO(thedadams): This will be moved to gptscript-ai in the future.
	"code_interpreter": {Link: "github.com/thedadams/code-interpreter"},
}

type ToolDefinition struct {
	Link    string
	SubTool string
}

func GPTScriptDefinitions() map[string]ToolDefinition {
	return builtInFunctionNameToDefinition
}

func StreamChatCompletionRequest(ctx context.Context, l *slog.Logger, client *http.Client, url, apiKey string, cc *db.CreateChatCompletionRequest) (<-chan db.ChatCompletionResponseChunk, error) {
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

			noPrefixLine := bytes.TrimPrefix(bytes.TrimSpace(rawLine), []byte(`data: `))
			if len(bytes.TrimSpace(noPrefixLine)) == 0 {
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
