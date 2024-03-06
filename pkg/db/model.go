package db

import "github.com/thedadams/clicky-chats/pkg/generated/openai"

type Model struct {
	Base    `json:",inline"`
	OwnedBy string `json:"owned_by"`
}

func (m *Model) IDPrefix() string {
	return "model-"
}

func (m *Model) ToPublic() any {
	//nolint:govet
	return &openai.Model{
		m.CreatedAt,
		m.ID,
		openai.ModelObjectModel,
		m.OwnedBy,
	}
}

func (m *Model) FromPublic(obj any) error {
	o, ok := obj.(*openai.Model)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o != nil && m != nil {
		//nolint:govet
		*m = Model{
			Base{
				o.Id,
				o.Created,
			},
			o.OwnedBy,
		}
	}

	return nil
}
