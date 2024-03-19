package db

import (
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type CreateEmbeddingResponse struct {
	// The following fields are not exposed in the public API
	JobResponse `json:",inline"`
	Base        `json:",inline"`

	// The following fields are exposed in the public API
	Data  datatypes.JSONSlice[Embedding]     `json:"data"`
	Model string                             `json:"model"`
	Usage datatypes.JSONType[EmbeddingUsage] `json:"usage,omitempty"`
}

func (e *CreateEmbeddingResponse) IDPrefix() string {
	return "embed-"
}

func (e *CreateEmbeddingResponse) ToPublic() any {
	//nolint:govet
	return &openai.CreateEmbeddingResponse{
		embeddingObjects(e.Data).toPublic(),
		e.Model,
		openai.CreateEmbeddingResponseObjectList,
		e.Usage.Data(),
	}
}

func (e *CreateEmbeddingResponse) FromPublic(obj any) error {
	o, ok := obj.(*openai.CreateEmbeddingResponse)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o != nil && e != nil {
		*e = CreateEmbeddingResponse{
			JobResponse{},
			Base{},
			publicEmbeddings(o.Data).toDB(),
			o.Model,
			datatypes.NewJSONType(EmbeddingUsage{
				o.Usage.PromptTokens,
				o.Usage.TotalTokens,
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
			Embedding: datatypes.NewJSONType(obj.Embedding),
		})
	}
	return
}

type Embedding struct {
	Index     int                                            `json:"index"`
	Embedding datatypes.JSONType[openai.Embedding_Embedding] `json:"embedding"`
}

func (e *Embedding) toPublic() openai.Embedding {
	//nolint:govet
	return openai.Embedding{
		e.Embedding.Data(),
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
