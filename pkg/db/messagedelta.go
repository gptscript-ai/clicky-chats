package db

import (
	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

func NewMessageDeltaWithText(index int, id, text string) (*MessageDelta, error) {
	content := new(openai.MessageDeltaObject_Delta_Content_Item)
	//nolint:govet
	if err := content.FromMessageDeltaContentTextObject(openai.MessageDeltaContentTextObject{
		index,
		(*struct {
			Annotations *[]openai.MessageDeltaContentTextObject_Text_Annotations_Item `json:"annotations,omitempty"`
			Value       *string                                                       `json:"value,omitempty"`
		})(&MessageDeltaContentTextObjectText{
			nil,
			&text,
		}),
		openai.MessageDeltaContentTextObjectTypeText,
	}); err != nil {
		return nil, err
	}

	return &MessageDelta{
		id,
		//nolint:govet
		datatypes.NewJSONType(MessageDeltaDelta{
			z.Pointer([]openai.MessageDeltaObject_Delta_Content_Item{*content}),
			nil,
			nil,
		}),
	}, nil
}

type MessageDelta struct {
	ID    string                                `json:"id"`
	Delta datatypes.JSONType[MessageDeltaDelta] `json:"delta"`
}

func (m *MessageDelta) ToPublic() any {
	//nolint:govet
	return &openai.MessageDeltaObject{
		m.Delta.Data(),
		m.ID,
		openai.MessageDeltaObjectObjectThreadMessageDelta,
	}
}

func (m *MessageDelta) FromPublic(obj any) error {
	o, ok := obj.(*openai.MessageDeltaObject)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o != nil && m != nil {
		//nolint:govet
		*m = MessageDelta{
			o.Id,
			datatypes.NewJSONType[MessageDeltaDelta](o.Delta),
		}
	}

	return nil
}

type MessageDeltaDelta struct {
	Content *[]openai.MessageDeltaObject_Delta_Content_Item `json:"content,omitempty"`

	// Keeping this field as FileIds makes the casting to the openai types much easier.
	//nolint:revive
	FileIds *[]string                           `json:"file_ids,omitempty"`
	Role    *openai.MessageDeltaObjectDeltaRole `json:"role,omitempty"`
}

type MessageDeltaContentTextObjectText struct {
	Annotations *[]openai.MessageDeltaContentTextObject_Text_Annotations_Item `json:"annotations,omitempty"`
	Value       *string                                                       `json:"value,omitempty"`
}
