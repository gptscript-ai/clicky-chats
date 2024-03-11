package db

import (
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type ImagesResponse struct {
	// The following fields are not exposed in the public API
	JobResponse `json:",inline"`

	// The following fields are exposed in the public API
	Base `json:",inline"`
	Data datatypes.JSONSlice[openai.Image] `json:"data"`
}

func (*ImagesResponse) IDPrefix() string {
	return "image-"
}

func (i *ImagesResponse) ToPublic() any {
	if i == nil {
		return nil
	}

	//nolint:govet
	return &openai.ImagesResponse{
		i.CreatedAt,
		i.Data,
	}
}

func (i *ImagesResponse) FromPublic(obj any) error {
	o, ok := obj.(*openai.ImagesResponse)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o == nil || i == nil {
		return nil
	}

	//nolint:govet
	*i = ImagesResponse{
		JobResponse{},
		Base{
			"",
			o.Created,
		},
		datatypes.NewJSONSlice(o.Data),
	}

	return nil
}
