package db

import (
	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type CreateImageRequest struct {
	JobRequest `json:",inline"`

	// The following fields map to OpenAI API resource fields.
	Model          *string `json:"model"`
	N              *int    `json:"n"`
	Prompt         string  `json:"prompt"`
	Quality        *string `json:"quality,omitempty"`
	ResponseFormat *string `json:"response_format"`
	Size           *string `json:"size"`
	Style          *string `json:"style"`
	User           *string `json:"user,omitempty"`
}

func (*CreateImageRequest) IDPrefix() string {
	return "image-"
}

func (i *CreateImageRequest) ToPublic() any {
	if i == nil {
		return nil
	}

	model := new(openai.CreateImageRequest_Model)
	if err := model.FromCreateImageRequestModel0(z.Dereference(i.Model)); err != nil {
		return nil
	}

	//nolint:govet
	return &openai.CreateImageRequest{
		model,
		i.N,
		i.Prompt,
		(*openai.CreateImageRequestQuality)(i.Quality),
		(*openai.CreateImageRequestResponseFormat)(i.ResponseFormat),
		(*openai.CreateImageRequestSize)(i.Size),
		(*openai.CreateImageRequestStyle)(i.Style),
		i.User,
	}
}

func (i *CreateImageRequest) FromPublic(obj any) error {
	o, ok := obj.(*openai.CreateImageRequest)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o == nil || i == nil {
		return nil
	}

	// TODO(njhale): Models can only be strings, so maybe there's a way we can disable generating a union type for this field.
	// It's just annoying and unnecessary to have to handle both types.
	// See https://github.com/openai/openai-openapi/issues/56#issuecomment-1702960180 for details on why the OpenAPI schema is written this way.
	model, err := z.Dereference(o.Model).AsCreateImageRequestModel0()
	if err != nil {
		return err
	}

	//nolint:govet
	*i = CreateImageRequest{
		JobRequest{},
		&model,
		o.N,
		o.Prompt,
		(*string)(o.Quality),
		(*string)(o.ResponseFormat),
		(*string)(o.Size),
		(*string)(o.Style),
		o.User,
	}

	return nil
}

type ImagesResponse struct {
	Base        `json:",inline"`
	JobResponse `json:",inline"`

	// The following fields map to OpenAI API resource fields.
	Created int                               `json:"created"`
	Data    datatypes.JSONSlice[openai.Image] `json:"data"`
}

func (*ImagesResponse) IDPrefix() string {
	return "image-"
}

func (i *ImagesResponse) ToPublic() any {
	if i == nil {
		return nil
	}

	//nolint:govet
	return &openai.ImagesResponse{
		i.Created,
		i.Data,
	}
}

func (i *ImagesResponse) FromPublic(obj any) error {
	o, ok := obj.(*openai.ImagesResponse)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o == nil || i == nil {
		return nil
	}

	//nolint:govet
	*i = ImagesResponse{
		Base{},
		JobResponse{},
		o.Created,
		datatypes.NewJSONSlice(o.Data),
	}

	return nil
}
