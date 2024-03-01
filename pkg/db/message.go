package db

import (
	"github.com/acorn-io/z"
	"github.com/thedadams/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type Message struct {
	ThreadChild `json:",inline"`
	Role        string                                                 `json:"role"`
	Content     datatypes.JSONSlice[openai.MessageObject_Content_Item] `json:"content"`
	AssistantID *string                                                `json:"assistant_id,omitempty"`
	RunID       *string                                                `json:"run_id,omitempty"`
	FileIDs     datatypes.JSONSlice[string]                            `json:"file_ids,omitempty"`
}

func (m *Message) ToPublic() any {
	//nolint:govet
	return &openai.MessageObject{
		m.AssistantID,
		m.Content,
		m.CreatedAt,
		m.FileIDs,
		m.ID,
		z.Pointer[map[string]interface{}](m.Metadata.Metadata),
		openai.ThreadMessage,
		openai.MessageObjectRole(m.Role),
		m.RunID,
		m.ThreadID,
	}
}

func (m *Message) FromPublic(obj any) error {
	o, ok := obj.(*openai.MessageObject)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o != nil && m != nil {
		//nolint:govet
		*m = Message{
			ThreadChild{
				Metadata{
					Base{
						o.Id,
						o.CreatedAt,
					},
					z.Dereference(o.Metadata),
				},
				o.ThreadId,
			},
			string(o.Role),
			o.Content,
			o.AssistantId,
			o.RunId,
			o.FileIds,
		}
	}

	return nil
}

type MessageFile struct {
	Base      `json:",inline"`
	MessageID string `json:"message_id"`
}

func (m *MessageFile) ToPublic() any {
	//nolint:govet
	return &openai.MessageFileObject{
		m.CreatedAt,
		m.ID,
		m.MessageID,
		openai.ThreadMessageFile,
	}
}

func (m *MessageFile) FromPublic(obj any) error {
	o, ok := obj.(*openai.MessageFileObject)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o != nil && m != nil {
		*m = MessageFile{
			Base{
				o.Id,
				o.CreatedAt,
			},
			o.MessageId,
		}
	}

	return nil
}

func MessageContentFromString(message string) (*openai.MessageObject_Content_Item, error) {
	content := new(openai.MessageObject_Content_Item)
	return content, content.FromMessageContentTextObject(openai.MessageContentTextObject{
		Text: struct {
			Annotations []openai.MessageContentTextObject_Text_Annotations_Item `json:"annotations"`
			Value       string                                                  `json:"value"`
		}{
			Value: message,
		},
		Type: openai.MessageContentTextObjectTypeText,
	})
}
