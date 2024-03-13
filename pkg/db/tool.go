package db

import (
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type Tool struct {
	Base        `json:",inline"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Contents    *string `json:"contents"`
	URL         *string `json:"url"`
	SubTool     *string `json:"subtool"`
	// Not part of the public API
	Program datatypes.JSON `json:"program"`
}

func (t *Tool) IDPrefix() string {
	return "tool-"
}

func (t *Tool) ToPublic() any {
	//nolint:govet
	return &openai.ToolObject{
		t.Contents,
		t.CreatedAt,
		t.Description,
		t.ID,
		t.Name,
		openai.Tool,
		t.SubTool,
		t.URL,
	}
}

func (t *Tool) FromPublic(obj any) error {
	o, ok := obj.(*openai.ToolObject)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o != nil && t != nil {
		//nolint:govet
		*t = Tool{
			Base{
				o.Id,
				o.CreatedAt,
			},
			o.Name,
			o.Description,
			o.Contents,
			o.Url,
			o.SubTool,
			nil,
		}
	}

	return nil
}
