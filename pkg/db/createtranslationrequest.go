package db

import (
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	oapitypes "github.com/oapi-codegen/runtime/types"
)

type CreateTranslationRequest struct {
	JobRequest `json:",inline"`

	File           []byte   `json:"file"`
	Model          string   `json:"model"`
	Prompt         *string  `json:"prompt,omitempty"`
	ResponseFormat *string  `json:"response_format,omitempty"`
	Temperature    *float32 `json:"temperature,omitempty"`
}

func (*CreateTranslationRequest) IDPrefix() string {
	return "translation-"
}

func (c *CreateTranslationRequest) ToPublic() any {
	if c == nil {
		return nil
	}

	model := new(openai.CreateTranslationRequest_Model)
	if err := model.FromCreateTranslationRequestModel0(c.Model); err != nil {
		return nil
	}

	file := new(oapitypes.File)
	if len(c.File) > 0 {
		file.InitFromBytes(c.File, "image")
	}

	//nolint:govet
	return &openai.CreateTranslationRequest{
		*file,
		*model,
		c.Prompt,
		c.ResponseFormat,
		c.Temperature,
	}
}

func (c *CreateTranslationRequest) FromPublic(obj any) error {
	o, ok := obj.(*openai.CreateTranslationRequest)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o == nil || c == nil {
		return nil
	}

	model, err := o.Model.AsCreateTranslationRequestModel0()
	if err != nil {
		return err
	}

	file, err := o.File.Bytes()
	if err != nil {
		return err
	}

	//nolint:govet
	*c = CreateTranslationRequest{
		JobRequest{},
		file,
		model,
		o.Prompt,
		o.ResponseFormat,
		o.Temperature,
	}

	return nil
}
