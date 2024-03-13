package db

import (
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type CreateEmbeddingRequest struct {
	// The following fields are not exposed in the public API
	JobRequest `json:",inline"`
	ModelAPI   string `json:"model_api"`

	// The following fields are exposed in the public API
	// Required fields
	Input datatypes.JSONType[openai.CreateEmbeddingRequest_Input] `json:"input"`
	Model string                                                  `json:"model"`

	// Optional fields
	EncodingFormat *string `json:"encoding_format,omitempty"`
	Dimensions     *int    `json:"dimensions,omitempty"`
	User           *string `json:"user,omitempty"`
}

func (e *CreateEmbeddingRequest) IDPrefix() string {
	return "embed-"
}

func (e *CreateEmbeddingRequest) ToPublic() any {
	model := new(openai.CreateEmbeddingRequest_Model)
	if err := model.FromCreateEmbeddingRequestModel1(openai.CreateEmbeddingRequestModel1(e.Model)); err != nil {
		if err = model.FromCreateEmbeddingRequestModel0(e.Model); err != nil {
			return nil
		}
	}

	//nolint:govet
	return &openai.CreateEmbeddingRequest{
		e.Dimensions,
		(*openai.CreateEmbeddingRequestEncodingFormat)(e.EncodingFormat),
		e.Input.Data(),
		*model,
		e.User,
	}
}

func (e *CreateEmbeddingRequest) FromPublic(obj any) error {
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

		*e = CreateEmbeddingRequest{
			JobRequest{},
			"",

			datatypes.NewJSONType(o.Input),
			model,

			encodingFormat,
			o.Dimensions,
			o.User,
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
