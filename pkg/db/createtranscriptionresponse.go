package db

import (
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
)

type CreateTranscriptionResponse struct {
	// The following fields are not exposed in the public API
	JobResponse `json:",inline"`

	// The following fields are exposed in the public API
	Base `json:",inline"`
	Text string
}

func (*CreateTranscriptionResponse) IDPrefix() string {
	return "transcription-"
}

func (c *CreateTranscriptionResponse) ToPublic() any {
	if c == nil {
		return nil
	}

	//nolint:govet
	return &openai.CreateTranscriptionResponseJson{
		c.Text,
	}
}

func (c *CreateTranscriptionResponse) FromPublic(obj any) error {
	o, ok := obj.(*openai.CreateTranscriptionResponseJson)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o == nil || c == nil {
		return nil
	}

	//nolint:govet
	*c = CreateTranscriptionResponse{
		JobResponse{},
		Base{},
		o.Text,
	}

	return nil
}
