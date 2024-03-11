package db

import (
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type EmbeddingsResponse struct {
	Data  datatypes.JSONSlice[Embedding]     `json:"data"`
	Model string                             `json:"model"`
	Usage datatypes.JSONType[EmbeddingUsage] `json:"usage,omitempty"`

	// Not part of the public API
	Base        `json:",inline"`
	JobResponse `json:",inline"`
}

func (e *EmbeddingsResponse) IDPrefix() string {
	return "embed-"
}

func (e *EmbeddingsResponse) ToPublic() any {
	//nolint:govet
	return &openai.CreateEmbeddingResponse{
		embeddingObjects(e.Data).toPublic(),
		e.Model,
		openai.CreateEmbeddingResponseObjectList,
		e.Usage.Data(),
	}
}

func (e *EmbeddingsResponse) FromPublic(obj any) error {
	o, ok := obj.(*openai.CreateEmbeddingResponse)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o != nil && e != nil {
		*e = EmbeddingsResponse{
			Data:  publicEmbeddings(o.Data).toDB(),
			Model: o.Model,
			Usage: datatypes.NewJSONType(EmbeddingUsage{
				PromptTokens: o.Usage.PromptTokens,
				TotalTokens:  o.Usage.TotalTokens,
			}),
		}
	}

	return nil
}

type publicEmbeddings []openai.Embedding

func (e publicEmbeddings) toDB() (embeddings []Embedding) {
	for _, obj := range e {
		embeddings = append(embeddings, Embedding{
			Index:     obj.Index,
			Embedding: obj.Embedding,
		})
	}
	return
}

type Embedding struct {
	Index     int                          `json:"index"`
	Embedding datatypes.JSONSlice[float32] `json:"embedding"`
}

func (e *Embedding) toPublic() openai.Embedding {
	//nolint:govet
	return openai.Embedding{
		e.Embedding,
		e.Index,
		openai.EmbeddingObjectEmbedding,
	}
}

type embeddingObjects []Embedding

func (e embeddingObjects) toPublic() []openai.Embedding {
	var res []openai.Embedding
	for _, obj := range e {
		res = append(res, obj.toPublic())
	}
	return res
}

// EmbeddingUsage represents the inline CreateEmbeddingResponse.Usage struct which is not generated as a separate type
type EmbeddingUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}
