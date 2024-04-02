package db

import (
	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type Message struct {
	Metadata          `json:",inline"`
	Role              string                                                 `json:"role"`
	Content           datatypes.JSONSlice[openai.MessageObject_Content_Item] `json:"content"`
	AssistantID       *string                                                `json:"assistant_id,omitempty"`
	ThreadID          string                                                 `json:"thread_id,omitempty"`
	RunID             *string                                                `json:"run_id,omitempty"`
	FileIDs           datatypes.JSONSlice[string]                            `json:"file_ids,omitempty"`
	Status            string                                                 `json:"status,omitempty"`
	CompletedAt       *int                                                   `json:"completed_at,omitempty"`
	IncompleteAt      *int                                                   `json:"incomplete_at,omitempty"`
	IncompleteDetails datatypes.JSONType[*struct {
		Reason openai.MessageObjectIncompleteDetailsReason `json:"reason"`
	}] `json:"incomplete_details,omitempty"`
}

func (m *Message) IDPrefix() string {
	return "msg_"
}

func (m *Message) ToPublic() any {
	//nolint:govet
	return &openai.MessageObject{
		m.AssistantID,
		m.CompletedAt,
		m.Content,
		m.CreatedAt,
		m.FileIDs,
		m.ID,
		m.IncompleteAt,
		m.IncompleteDetails.Data(),
		z.Pointer[map[string]interface{}](m.Metadata.Metadata),
		openai.ThreadMessage,
		openai.MessageObjectRole(m.Role),
		m.RunID,
		openai.MessageObjectStatus(m.Status),
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
			Metadata{
				Base{
					o.Id,
					o.CreatedAt,
				},
				z.Dereference(o.Metadata),
			},
			string(o.Role),
			o.Content,
			o.AssistantId,
			o.ThreadId,
			o.RunId,
			o.FileIds,
			string(o.Status),
			o.CompletedAt,
			o.IncompleteAt,
			datatypes.NewJSONType(o.IncompleteDetails),
		}
	}

	return nil
}

func (m *Message) WithTextContent(content string) error {
	c := new(openai.MessageObject_Content_Item)
	if err := c.FromMessageContentTextObject(openai.MessageContentTextObject{
		Text: struct {
			Annotations []openai.MessageContentTextObject_Text_Annotations_Item `json:"annotations"`
			Value       string                                                  `json:"value"`
		}{
			Value: content,
		},
		Type: openai.MessageContentTextObjectTypeText,
	}); err != nil {
		return err
	}
	m.Content = datatypes.NewJSONSlice([]openai.MessageObject_Content_Item{*c})

	return nil
}

type MessageFile struct {
	Base      `json:",inline"`
	MessageID string `json:"message_id"`
}

func (m *MessageFile) IDPrefix() string {
	return "file-"
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
