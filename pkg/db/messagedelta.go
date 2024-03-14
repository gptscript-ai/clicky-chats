package db

import (
	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

func NewMessageDeltaWithText(index int, id, text string) (*MessageDelta, error) {
	content := new(openai.XMessageDeltaObjectDeltaContent_Item)
	//nolint:govet
	if err := content.FromXMessageDeltaContentTextObject(openai.XMessageDeltaContentTextObject{
		z.Pointer(index),
		&openai.XMessageDeltaContentTextObjectText{
			nil,
			&text,
		},
		openai.Text,
	}); err != nil {
		return nil, err
	}

	return &MessageDelta{
		id,
		//nolint:govet
		datatypes.NewJSONType(openai.XMessageDeltaObjectDelta{
			[]openai.XMessageDeltaObjectDeltaContent_Item{*content},
			nil,
			nil,
		}),
	}, nil
}

type MessageDelta struct {
	ID    string                                              `json:"id"`
	Delta datatypes.JSONType[openai.XMessageDeltaObjectDelta] `json:"delta"`
}

func (m *MessageDelta) ToPublic() any {
	//nolint:govet
	return &openai.XMessageDeltaObject{
		z.Pointer(m.Delta.Data()),
		m.ID,
		openai.ThreadMessageDelta,
	}
}

func (m *MessageDelta) FromPublic(obj any) error {
	o, ok := obj.(*openai.XMessageDeltaObject)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o != nil && m != nil {
		//nolint:govet
		*m = MessageDelta{
			o.Id,
			datatypes.NewJSONType(z.Dereference(o.Delta)),
		}
	}

	return nil
}
