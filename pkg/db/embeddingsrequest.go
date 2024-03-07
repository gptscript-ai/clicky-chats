package db

import (
	"github.com/thedadams/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type EmbeddingsRequest struct {
	// Required fields
	Input datatypes.JSONType[openai.CreateEmbeddingRequest_Input] `json:"input"`
	Model string                                                  `json:"model"`

	// Optional fields
	EncodingFormat *string `json:"encoding_format,omitempty"`
	Dimensions     *int    `json:"dimensions,omitempty"`
	User           *string `json:"user,omitempty"`

	// These are not part of the OpenAI API
	JobRequest `json:",inline"`
	ModelAPI   string `json:"model_api"`
}

func (e *EmbeddingsRequest) IDPrefix() string {
	return "embed-"
}

func (e *EmbeddingsRequest) ToPublic() any {

	model := new(openai.CreateEmbeddingRequest_Model)
	if err := model.FromCreateEmbeddingRequestModel1(openai.CreateEmbeddingRequestModel1(e.Model)); err != nil {
		if err = model.FromCreateEmbeddingRequestModel0(e.Model); err != nil {
			return nil
		}
	}

	//nolint:govet
	return &openai.CreateEmbeddingRequest{
		Input: e.Input.Data(),
		Model: *model,

		EncodingFormat: (*openai.CreateEmbeddingRequestEncodingFormat)(e.EncodingFormat),
		Dimensions:     e.Dimensions,
		User:           e.User,
	}
}

func (e *EmbeddingsRequest) FromPublic(obj any) error {
	o, ok := obj.(*openai.CreateEmbeddingRequest)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o != nil && e != nil {

		model, err := EmbeddingModelFromPublic(o.Model)
		if err != nil {
			return err
		}

		var encodingFormat *string
		if o.EncodingFormat != nil {
			encodingFormat = (*string)(o.EncodingFormat)
		}

		*e = EmbeddingsRequest{
			Input: datatypes.NewJSONType(o.Input),
			Model: model,

			EncodingFormat: encodingFormat,
			Dimensions:     o.Dimensions,
			User:           o.User,

			JobRequest: JobRequest{},
			ModelAPI:   "",
		}

	}

	return nil
}

func EmbeddingModelFromPublic(openAIModel openai.CreateEmbeddingRequest_Model) (string, error) {
	var model string
	if m, err := openAIModel.AsCreateEmbeddingRequestModel1(); err != nil {
		if m, err := openAIModel.AsCreateEmbeddingRequestModel0(); err == nil {
			model = m
		} else {
			return "", err
		}
	} else {
		model = string(m)
	}

	return model, nil
}
