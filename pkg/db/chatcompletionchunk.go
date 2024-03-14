package db

import (
	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type ChatCompletionResponseChunk struct {
	Base              `json:",inline"`
	Choices           datatypes.JSONSlice[ChunkChoice] `json:"choices"`
	Model             string                           `json:"model"`
	SystemFingerprint *string                          `json:"system_fingerprint,omitempty"`
	// Not part of the public API
	JobResponse `json:",inline"`
	ResponseIdx int `json:"response_idx"`
}

func (c *ChatCompletionResponseChunk) IDPrefix() string {
	return "chatcmpl-"
}

func (c *ChatCompletionResponseChunk) GetIndex() int {
	return c.ResponseIdx
}

func (c *ChatCompletionResponseChunk) ToPublic() any {
	//nolint:govet
	return &openai.CreateChatCompletionStreamResponse{
		chunkChoices(c.Choices).toPublic(),
		c.CreatedAt,
		c.ID,
		c.Model,
		openai.CreateChatCompletionStreamResponseObjectChatCompletionChunk,
		c.SystemFingerprint,
	}
}

func (c *ChatCompletionResponseChunk) FromPublic(obj any) error {
	o, ok := obj.(*openai.CreateChatCompletionStreamResponse)
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
			JobResponse{},
			0,
		}
	}

	return nil
}

type ChunkChoice struct {
	FinishReason string                                                       `json:"finish_reason"`
	Index        int                                                          `json:"index"`
	Logprobs     datatypes.JSONType[Lobprob]                                  `json:"logprobs"`
	Delta        datatypes.JSONType[openai.ChatCompletionStreamResponseDelta] `json:"delta"`
}

func (c *ChunkChoice) toPublic() publicChunkChoice {
	var finishReason *openai.CreateChatCompletionStreamResponseChoicesFinishReason
	if c.FinishReason != "" {
		finishReason = z.Pointer(openai.CreateChatCompletionStreamResponseChoicesFinishReason(c.FinishReason))
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
	Delta        openai.ChatCompletionStreamResponseDelta                      `json:"delta"`
	FinishReason *openai.CreateChatCompletionStreamResponseChoicesFinishReason `json:"finish_reason"`
	Index        int                                                           `json:"index"`
	Logprobs     *struct {
		Content *[]openai.ChatCompletionTokenLogprob `json:"content"`
	} `json:"logprobs"`
}

func (pc publicChunkChoice) toDBChoice() ChunkChoice {
	var lobProbs Lobprob
	if pc.Logprobs != nil {
		lobProbs = Lobprob{
			Content: z.Dereference(pc.Logprobs.Content),
		}
	}

	return ChunkChoice{
		FinishReason: z.Dereference((*string)(pc.FinishReason)),
		Index:        pc.Index,
		Logprobs:     datatypes.NewJSONType(lobProbs),
		Delta:        datatypes.NewJSONType(pc.Delta),
	}
}

type publicChunkChoices []struct {
	Delta        openai.ChatCompletionStreamResponseDelta                      `json:"delta"`
	FinishReason *openai.CreateChatCompletionStreamResponseChoicesFinishReason `json:"finish_reason"`
	Index        int                                                           `json:"index"`
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
