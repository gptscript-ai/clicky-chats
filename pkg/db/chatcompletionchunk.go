package db

import (
	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type ChatCompletionResponseChunk struct {
	Base              `json:",inline"`
	Choices           datatypes.JSONSlice[ChunkChoice]            `json:"choices"`
	Model             string                                      `json:"model"`
	SystemFingerprint *string                                     `json:"system_fingerprint,omitempty"`
	Usage             datatypes.JSONType[*openai.CompletionUsage] `json:"usage,omitempty"`

	// Not part of the public API
	User        string `json:"user"`
	JobResponse `json:",inline"`
	ResponseIdx int `json:"response_idx"`
}

func (c *ChatCompletionResponseChunk) IDPrefix() string {
	return "chatcmpl-"
}

func (c *ChatCompletionResponseChunk) GetIndex() int {
	return c.ResponseIdx
}

func (c *ChatCompletionResponseChunk) GetEvent() string {
	return ""
}

func (c *ChatCompletionResponseChunk) ToPublic() any {
	//nolint:govet
	return &openai.ExtendedCreateChatCompletionStreamResponse{
		chunkChoices(c.Choices).toPublic(),
		c.CreatedAt,
		c.ID,
		c.Model,
		openai.ExtendedCreateChatCompletionStreamResponseObjectChatCompletionChunk,
		c.SystemFingerprint,
		c.Usage.Data(),
	}
}

func (c *ChatCompletionResponseChunk) FromPublic(obj any) error {
	o, ok := obj.(*openai.ExtendedCreateChatCompletionStreamResponse)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o != nil && c != nil {
		*c = ChatCompletionResponseChunk{
			Base{
				CreatedAt: o.Created,
				ID:        o.Id,
			},
			publicChunkChoices(o.Choices).toDBChoices(),
			o.Model,
			o.SystemFingerprint,
			datatypes.NewJSONType(o.Usage),
			"",
			JobResponse{},
			0,
		}
	}

	return nil
}

type ChunkChoice struct {
	FinishReason string                                                       `json:"finish_reason"`
	Index        int                                                          `json:"index"`
	Logprobs     datatypes.JSONType[Logprob]                                  `json:"logprobs"`
	Delta        datatypes.JSONType[openai.ChatCompletionStreamResponseDelta] `json:"delta"`
}

func (c *ChunkChoice) toPublic() publicChunkChoice {
	var finishReason *openai.ExtendedCreateChatCompletionStreamResponseChoicesFinishReason
	if c.FinishReason != "" {
		finishReason = z.Pointer(openai.ExtendedCreateChatCompletionStreamResponseChoicesFinishReason(c.FinishReason))
	}
	return publicChunkChoice{
		FinishReason: finishReason,
		Index:        c.Index,
		Logprobs: &struct {
			Content *[]openai.ChatCompletionTokenLogprob `json:"content"`
		}{
			Content: z.Pointer(c.Logprobs.Data().Content),
		},
		Delta: c.Delta.Data(),
	}
}

type chunkChoices []ChunkChoice

func (c chunkChoices) toPublic() publicChunkChoices {
	choices := make(publicChunkChoices, 0, len(c))
	for _, choice := range c {
		choices = append(choices, choice.toPublic())
	}
	return choices
}

type publicChunkChoice struct {
	Delta        openai.ChatCompletionStreamResponseDelta                              `json:"delta"`
	FinishReason *openai.ExtendedCreateChatCompletionStreamResponseChoicesFinishReason `json:"finish_reason"`
	Index        int                                                                   `json:"index"`
	Logprobs     *struct {
		Content *[]openai.ChatCompletionTokenLogprob `json:"content"`
	} `json:"logprobs"`
}

func (pc publicChunkChoice) toDBChoice() ChunkChoice {
	var logProbs Logprob
	if pc.Logprobs != nil {
		logProbs = Logprob{
			Content: z.Dereference(pc.Logprobs.Content),
		}
	}

	return ChunkChoice{
		FinishReason: z.Dereference((*string)(pc.FinishReason)),
		Index:        pc.Index,
		Logprobs:     datatypes.NewJSONType(logProbs),
		Delta:        datatypes.NewJSONType(pc.Delta),
	}
}

type publicChunkChoices []struct {
	Delta        openai.ChatCompletionStreamResponseDelta                              `json:"delta"`
	FinishReason *openai.ExtendedCreateChatCompletionStreamResponseChoicesFinishReason `json:"finish_reason"`
	Index        int                                                                   `json:"index"`
	Logprobs     *struct {
		Content *[]openai.ChatCompletionTokenLogprob `json:"content"`
	} `json:"logprobs"`
}

func (pc publicChunkChoices) toDBChoices() (choices []ChunkChoice) {
	for _, choice := range pc {
		choices = append(choices, publicChunkChoice(choice).toDBChoice())
	}
	return choices
}
