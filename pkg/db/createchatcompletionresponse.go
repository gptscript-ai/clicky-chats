package db

import (
	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type CreateChatCompletionResponse struct {
	// The following fields are not exposed in the public API
	JobResponse `json:",inline"`
	Base        `json:",inline"`

	// The following fields are exposed in the public API
	Choices           datatypes.JSONSlice[Choice]                 `json:"choices"`
	Model             string                                      `json:"model"`
	SystemFingerprint *string                                     `json:"system_fingerprint,omitempty"`
	Usage             datatypes.JSONType[*openai.CompletionUsage] `json:"usage,omitempty"`
}

func (c *CreateChatCompletionResponse) IDPrefix() string {
	return "chatcmpl-"
}

func (c *CreateChatCompletionResponse) FromPublic(obj any) error {
	o, ok := obj.(*openai.CreateChatCompletionResponse)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o != nil && c != nil {
		//nolint:govet
		*c = CreateChatCompletionResponse{
			JobResponse{},
			Base{
				CreatedAt: o.Created,
				ID:        o.Id,
			},
			publicChoices(o.Choices).toDBChoices(),
			o.Model,
			o.SystemFingerprint,
			datatypes.NewJSONType(o.Usage),
		}
	}

	return nil
}

func (c *CreateChatCompletionResponse) ToPublic() any {
	//nolint:govet
	return &openai.CreateChatCompletionResponse{
		choices(c.Choices).toPublic(),
		c.CreatedAt,
		c.ID,
		c.Model,
		openai.CreateChatCompletionResponseObjectChatCompletion,
		c.SystemFingerprint,
		c.Usage.Data(),
	}
}

type Choice struct {
	FinishReason string                                                   `json:"finish_reason"`
	Index        int                                                      `json:"index"`
	Logprobs     datatypes.JSONType[Logprob]                              `json:"logprobs"`
	Message      datatypes.JSONType[openai.ChatCompletionResponseMessage] `json:"message"`
}

func (c *Choice) toPublic() publicChoice {
	return publicChoice{
		FinishReason: openai.CreateChatCompletionResponseChoicesFinishReason(c.FinishReason),
		Index:        c.Index,
		Logprobs: &struct {
			Content *[]openai.ChatCompletionTokenLogprob `json:"content"`
		}{
			Content: z.Pointer(c.Logprobs.Data().Content),
		},
		Message: c.Message.Data(),
	}
}

type publicChoice struct {
	FinishReason openai.CreateChatCompletionResponseChoicesFinishReason `json:"finish_reason"`
	Index        int                                                    `json:"index"`
	Logprobs     *struct {
		Content *[]openai.ChatCompletionTokenLogprob `json:"content"`
	} `json:"logprobs"`
	Message openai.ChatCompletionResponseMessage `json:"message"`
}

func (pc publicChoice) toDBChoice() Choice {
	var lobProbs Logprob
	if pc.Logprobs != nil {
		lobProbs = Logprob{
			Content: z.Dereference(pc.Logprobs.Content),
		}
	}
	return Choice{
		FinishReason: string(pc.FinishReason),
		Index:        pc.Index,
		Logprobs:     datatypes.NewJSONType(lobProbs),
		Message:      datatypes.NewJSONType(pc.Message),
	}
}

type choices []Choice

func (c choices) toPublic() publicChoices {
	choices := make(publicChoices, 0, len(c))
	for _, choice := range c {
		choices = append(choices, choice.toPublic())
	}
	return choices
}

type publicChoices []struct {
	FinishReason openai.CreateChatCompletionResponseChoicesFinishReason `json:"finish_reason"`
	Index        int                                                    `json:"index"`
	Logprobs     *struct {
		Content *[]openai.ChatCompletionTokenLogprob `json:"content"`
	} `json:"logprobs"`
	Message openai.ChatCompletionResponseMessage `json:"message"`
}

func (pc publicChoices) toDBChoices() (choices []Choice) {
	for _, choice := range pc {
		choices = append(choices, publicChoice(choice).toDBChoice())
	}
	return choices
}

type Logprob struct {
	Content []openai.ChatCompletionTokenLogprob `json:"content"`
}
