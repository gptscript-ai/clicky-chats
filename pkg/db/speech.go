package db

import (
	"github.com/thedadams/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type Speech struct {
	Input          string                                               `json:"input"`
	Model          datatypes.JSONType[openai.CreateSpeechRequest_Model] `json:"model"`
	ResponseFormat *string                                              `json:"response_format,omitempty"`
	Speed          *float32                                             `json:"speed,omitempty"`
	Voice          string                                               `json:"voice"`
}

func (s *Speech) IDPrefix() string {
	return "speech-"
}

func (s *Speech) ToPublic() any {
	//nolint:govet
	return &openai.CreateSpeechRequest{
		s.Input,
		s.Model.Data(),
		(*openai.CreateSpeechRequestResponseFormat)(s.ResponseFormat),
		s.Speed,
		openai.CreateSpeechRequestVoice(s.Voice),
	}
}

func (s *Speech) FromPublic(obj any) error {
	o, ok := obj.(*openai.CreateSpeechRequest)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o != nil && s != nil {
		//nolint:govet
		*s = Speech{
			o.Input,
			datatypes.NewJSONType(o.Model),
			(*string)(o.ResponseFormat),
			o.Speed,
			string(o.Voice),
		}
	}

	return nil
}
