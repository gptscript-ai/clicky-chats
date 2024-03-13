package db

import (
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	oapitypes "github.com/oapi-codegen/runtime/types"
	"gorm.io/datatypes"
)

type CreateTranscriptionRequest struct {
	JobRequest `json:",inline"`

	FileName               string                      `json:"file_name"`
	File                   []byte                      `json:"file"`
	Language               *string                     `json:"language,omitempty"`
	Model                  string                      `json:"model"`
	Prompt                 *string                     `json:"prompt,omitempty"`
	ResponseFormat         *string                     `json:"response_format,omitempty"`
	Temperature            *float32                    `json:"temperature,omitempty"`
	TimestampGranularities datatypes.JSONSlice[string] `json:"timestamp_granularities,omitempty"`
}

func (*CreateTranscriptionRequest) IDPrefix() string {
	return "transcription-"
}

func (c *CreateTranscriptionRequest) ToPublic() any {
	if c == nil {
		return nil
	}

	model := new(openai.CreateTranscriptionRequest_Model)
	if err := model.FromCreateTranscriptionRequestModel1(openai.CreateTranscriptionRequestModel1(c.Model)); err != nil {
		if err = model.FromCreateTranscriptionRequestModel0(c.Model); err != nil {
			return nil
		}
	}

	file := new(oapitypes.File)
	if len(c.File) > 0 {
		file.InitFromBytes(c.File, c.FileName)
	}

	var granularities *[]openai.CreateTranscriptionRequestTimestampGranularities
	if c.TimestampGranularities != nil {
		granularities = new([]openai.CreateTranscriptionRequestTimestampGranularities)
		for _, g := range c.TimestampGranularities {
			*granularities = append(*granularities, openai.CreateTranscriptionRequestTimestampGranularities(g))
		}
	}

	//nolint:govet
	return &openai.CreateTranscriptionRequest{
		*file,
		c.Language,
		*model,
		c.Prompt,
		(*openai.CreateTranscriptionRequestResponseFormat)(c.ResponseFormat),
		c.Temperature,
		granularities,
	}
}

func (c *CreateTranscriptionRequest) FromPublic(obj any) error {
	o, ok := obj.(*openai.CreateTranscriptionRequest)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o == nil || c == nil {
		return nil
	}

	model, err := o.Model.AsCreateTranscriptionRequestModel0()
	if err != nil {
		return err
	}

	file, err := o.File.Bytes()
	if err != nil {
		return err
	}

	var granularities []string
	if o.TimestampGranularities != nil {
		for _, g := range *o.TimestampGranularities {
			granularities = append(granularities, string(g))
		}
	}

	//nolint:govet
	*c = CreateTranscriptionRequest{
		JobRequest{},
		o.File.Filename(),
		file,
		o.Language,
		model,
		o.Prompt,
		(*string)(o.ResponseFormat),
		o.Temperature,
		granularities,
	}

	return nil
}
