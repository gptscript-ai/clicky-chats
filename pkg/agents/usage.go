package agents

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/gptscript-ai/gptscript/pkg/runner"
	"github.com/gptscript-ai/gptscript/pkg/server"
	"github.com/gptscript-ai/gptscript/pkg/types"
	"github.com/pkoukk/tiktoken-go"
	oai "github.com/sashabaranov/go-openai"
)

type Usage struct {
	Model            string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// SumUsage returns the sum of the given usages as a single usage.
// Note: Run step completion usage assumes chat completion requests were made to a single model, however gptscript can
// make requests to many different models over the course of a run_step, so producing a single summed usage is technically
// incorrect but will have to do until we come up with something better.
func SumUsage(usages []Usage) Usage {
	var usage Usage
	for _, u := range usages {
		usage.PromptTokens += u.PromptTokens
		usage.CompletionTokens += u.CompletionTokens
		usage.TotalTokens += u.TotalTokens
	}

	return usage
}

// PromptTokens returns the total number of prompt tokens for a given OpenAI model and request.
// An error is returned if counting isn't supported for the given model.
func PromptTokens(model string, req *TokenRequest) (int, error) {
	tkm, err := tiktoken.EncodingForModel(model)
	if err != nil {
		return 0, fmt.Errorf("failed to get encoding for model %s: %w", model, err)
	}

	var fixedCost fixedTokenCost
	switch model {
	case "gpt-3.5-turbo-0613",
		"gpt-3.5-turbo-16k-0613",
		"gpt-4-0314",
		"gpt-4-32k-0314",
		"gpt-4-0613",
		"gpt-4-32k-0613":
		fixedCost.message = 3
		fixedCost.name = 1
	case "gpt-3.5-turbo-0301":
		fixedCost.message = 4 // every message follows <|start|>{role/name}\n{content}<|end|>\n
		fixedCost.name = -1   // if there's a name, the role is omitted
	default:
		if strings.Contains(model, "gpt-3.5-turbo") {
			// gpt-3.5-turbo may update over time. Returning num tokens assuming gpt-3.5-turbo-0613.
			return PromptTokens("gpt-3.5-turbo-0613", req)
		}
		if strings.Contains(model, "gpt-4") {
			// gpt-4 may update over time. Returning num tokens assuming gpt-4-0613.
			return PromptTokens("gpt-4-0613", req)
		}

		return 0, fmt.Errorf("token counting method for model %s is unknown", model)
	}

	count := func(s string) int {
		return len(tkm.Encode(s, nil, nil))
	}

	// Sum prompt tokens from explicit messages
	var tokens int
	for _, msg := range req.Messages {
		tokens += fixedCost.message
		for _, s := range []string{msg.Content, msg.Role, msg.Name} {
			tokens += count(s)
		}
		if msg.Name != "" {
			tokens += fixedCost.name
		}
	}

	// TODO(njhale): Sum prompt tokens from function definitions
	// Note: According to https://community.openai.com/t/how-to-calculate-the-tokens-when-using-function-call/266573/6,
	// tool definitions are transformed into system messages with an undocumented encoding scheme before being passed
	// to the LLM. https://community.openai.com/t/how-to-calculate-the-tokens-when-using-function-call/266573/10 suggests
	// a counting implementation based on reverse-engineering token counts for non-streaming requests with tool definitions.

	return tokens, nil
}

type fixedTokenCost struct {
	message int
	name    int
}

type TokenRequest struct {
	Messages []TokenMessage `json:"messages"`
	// TODO(njhale): Support Tool definitions
}

type TokenMessage struct {
	Name    string `json:"name"`
	Role    string `json:"role"`
	Content string `json:"content"`
	// TODO(njhale): Support tool calls
}

type usageSet map[string]Usage

func (s usageSet) addTokens(event server.Event) error {
	if event.Type != runner.EventTypeChat {
		return nil
	}

	usage := s[event.ChatCompletionID]
	if req, ok := (event.ChatRequest).(oai.ChatCompletionRequest); ok {
		usage.Model = req.Model
		treq, err := Transform[TokenRequest](req)
		if err != nil {
			return fmt.Errorf("failed to transform chat completion request to token request: %w", err)
		}

		tokens, err := PromptTokens(usage.Model, &treq)
		if err != nil {
			return fmt.Errorf("failed to count prompt tokens: %w", err)
		}

		usage.PromptTokens += tokens
		usage.TotalTokens += tokens

		s[event.ChatCompletionID] = usage

		return nil
	}

	if msg, ok := (event.ChatResponse).(types.CompletionMessage); ok &&
		msg.Role == types.CompletionMessageRoleTypeAssistant {
		// TODO(njhale): Implement me!
		slog.Warn("Received tool call", "call", fmt.Sprintf("%v", msg))
	}

	return nil
}

func (s usageSet) asSlice() []Usage {
	var usages []Usage
	for _, u := range s {
		usages = append(usages, u)
	}

	return usages
}
