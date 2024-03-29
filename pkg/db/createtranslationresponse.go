package db

import (
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
)

type CreateTranslationResponse struct {
	// The following fields are not exposed in the public API
	JobResponse `json:",inline"`

	// The following fields are exposed in the public API
	Base `json:",inline"`
	Text string
}

func (*CreateTranslationResponse) IDPrefix() string {
	return "translation-"
}

func (c *CreateTranslationResponse) ToPublic() any {
	if c == nil {
		return nil
	}

	//nolint:govet
	return &openai.CreateTranslationResponseJson{
		c.Text,
	}
}

func (c *CreateTranslationResponse) FromPublic(obj any) error {
	o, ok := obj.(*openai.CreateTranslationResponseJson)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o == nil || c == nil {
		return nil
	}

	//nolint:govet
	*c = CreateTranslationResponse{
		JobResponse{},
		Base{},
		o.Text,
	}

	return nil
}
