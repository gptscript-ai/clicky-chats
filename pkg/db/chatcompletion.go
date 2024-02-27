package db

import (
	"github.com/acorn-io/z"
	"github.com/thedadams/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type ChatCompletion struct {
	FrequencyPenalty *float32                                                     `json:"frequency_penalty"`
	LogitBias        datatypes.JSONType[map[string]int]                           `json:"logit_bias"`
	Logprobs         *bool                                                        `json:"logprobs"`
	MaxTokens        *int                                                         `json:"max_tokens"`
	Messages         datatypes.JSONSlice[openai.ChatCompletionRequestMessage]     `json:"messages"`
	Model            datatypes.JSONType[openai.CreateChatCompletionRequest_Model] `json:"model"`
	N                *int                                                         `json:"n"`
	PresencePenalty  *float32                                                     `json:"presence_penalty"`
	ResponseFormat   *string                                                      `json:"response_format,omitempty"`
	Seed             *int                                                         `json:"seed"`
	Stop             datatypes.JSONType[*openai.CreateChatCompletionRequest_Stop] `json:"stop,omitempty"`
	Stream           *bool                                                        `json:"stream"`
	Temperature      *float32                                                     `json:"temperature"`
	ToolChoice       datatypes.JSONType[*openai.ChatCompletionToolChoiceOption]   `json:"tool_choice,omitempty"`
	Tools            datatypes.JSONSlice[openai.ChatCompletionTool]               `json:"tools,omitempty"`
	TopLogprobs      *int                                                         `json:"top_logprobs"`
	TopP             *float32                                                     `json:"top_p"`
	User             *string                                                      `json:"user,omitempty"`
}

func (c *ChatCompletion) ToPublic() any {
	//nolint:govet
	return &openai.CreateChatCompletionRequest{
		c.FrequencyPenalty,

		// These two fields are deprecated and will never be set.
		nil,
		nil,

		z.Pointer(c.LogitBias.Data()),
		c.Logprobs,
		c.MaxTokens,
		c.Messages,
		c.Model.Data(),
		c.N,
		c.PresencePenalty,
		&struct {
			Type *openai.CreateChatCompletionRequestResponseFormatType `json:"type,omitempty"`
		}{
			Type: (*openai.CreateChatCompletionRequestResponseFormatType)(c.ResponseFormat),
		},
		c.Seed,
		c.Stop.Data(),
		c.Stream,
		c.Temperature,
		c.ToolChoice.Data(),
		z.Pointer[[]openai.ChatCompletionTool](c.Tools),
		c.TopLogprobs,
		c.TopP,
		c.User,
	}
}

func (c *ChatCompletion) FromPublic(obj any) error {
	o, ok := obj.(*openai.CreateChatCompletionRequest)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o != nil && c != nil {
		//nolint:govet
		*c = ChatCompletion{
			o.FrequencyPenalty,
			datatypes.NewJSONType(z.Dereference(o.LogitBias)),
			o.Logprobs,
			o.MaxTokens,
			o.Messages,
			datatypes.NewJSONType(o.Model),
			o.N,
			o.PresencePenalty,
			(*string)(o.ResponseFormat.Type),
			o.Seed,
			datatypes.NewJSONType(o.Stop),
			o.Stream,
			o.Temperature,
			datatypes.NewJSONType(o.ToolChoice),
			z.Dereference(o.Tools),
			o.TopLogprobs,
			o.TopP,
			o.User,
		}
	}

	return nil
}
