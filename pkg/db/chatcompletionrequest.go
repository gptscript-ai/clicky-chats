package db

import (
	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type ChatCompletionRequest struct {
	FrequencyPenalty *float32                                                     `json:"frequency_penalty"`
	LogitBias        datatypes.JSONType[map[string]int]                           `json:"logit_bias"`
	Logprobs         *bool                                                        `json:"logprobs"`
	MaxTokens        *int                                                         `json:"max_tokens"`
	Messages         datatypes.JSONSlice[openai.ChatCompletionRequestMessage]     `json:"messages"`
	Model            string                                                       `json:"model"`
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
	// These are not part of the OpenAI API
	JobRequest `json:",inline"`
	ModelAPI   string `json:"model_api"`
}

func (c *ChatCompletionRequest) IDPrefix() string {
	return "chatcmpl-"
}

func (c *ChatCompletionRequest) ToPublic() any {
	var responseFormat *struct {
		Type *openai.CreateChatCompletionRequestResponseFormatType `json:"type,omitempty"`
	}

	if c.ResponseFormat != nil {
		responseFormat = &struct {
			Type *openai.CreateChatCompletionRequestResponseFormatType `json:"type,omitempty"`
		}{
			Type: (*openai.CreateChatCompletionRequestResponseFormatType)(c.ResponseFormat),
		}
	}

	model := new(openai.CreateChatCompletionRequest_Model)
	if err := model.FromCreateChatCompletionRequestModel1(openai.CreateChatCompletionRequestModel1(c.Model)); err != nil {
		if err = model.FromCreateChatCompletionRequestModel0(c.Model); err != nil {
			return nil
		}
	}

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
		*model,
		c.N,
		c.PresencePenalty,
		responseFormat,
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

func (c *ChatCompletionRequest) FromPublic(obj any) error {
	o, ok := obj.(*openai.CreateChatCompletionRequest)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o != nil && c != nil {
		var responseFormatType *string
		if o.ResponseFormat != nil {
			responseFormatType = (*string)(o.ResponseFormat.Type)
		}

		model, err := ChatCompletionModelFromPublic(o.Model)
		if err != nil {
			return err
		}
		//nolint:govet
		*c = ChatCompletionRequest{
			o.FrequencyPenalty,
			datatypes.NewJSONType(z.Dereference(o.LogitBias)),
			o.Logprobs,
			o.MaxTokens,
			o.Messages,
			model,
			o.N,
			o.PresencePenalty,
			responseFormatType,
			o.Seed,
			datatypes.NewJSONType(o.Stop),
			o.Stream,
			o.Temperature,
			datatypes.NewJSONType(o.ToolChoice),
			z.Dereference(o.Tools),
			o.TopLogprobs,
			o.TopP,
			o.User,
			JobRequest{},
			"",
		}
	}

	return nil
}

func ChatCompletionModelFromPublic(openAIModel openai.CreateChatCompletionRequest_Model) (string, error) {
	var model string
	if m, err := openAIModel.AsCreateChatCompletionRequestModel1(); err != nil {
		if m, err := openAIModel.AsCreateChatCompletionRequestModel0(); err == nil {
			model = m
		} else {
			return "", err
		}
	} else {
		model = string(m)
	}

	return model, nil
}
