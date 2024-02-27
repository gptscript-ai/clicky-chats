package db

import (
	"github.com/thedadams/clicky-chats/pkg/generated/openai"
)

type AssistantFile struct {
	Base        `json:",inline"`
	AssistantID string `json:"assistant_id"`
}

func (af *AssistantFile) ToPublic() any {
	//nolint:govet
	return &openai.AssistantFileObject{
		af.AssistantID,
		af.CreatedAt,
		af.ID,
		openai.AssistantFile,
	}
}

func (af *AssistantFile) FromPublic(obj any) error {
	o, ok := obj.(*openai.AssistantFileObject)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}
	if o != nil && af != nil {
		//nolint:govet
		*af = AssistantFile{
			Base{
				o.Id,
				o.CreatedAt,
			},
			o.AssistantId,
		}
	}

	return nil
}
