package db

import (
	"fmt"

	"github.com/acorn-io/z"
	"github.com/thedadams/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type Assistant struct {
	Metadata     `json:",inline"`
	Description  *string                                                `json:"description"`
	FileIDs      datatypes.JSONSlice[string]                            `json:"file_ids"`
	Instructions *string                                                `json:"instructions"`
	Model        string                                                 `json:"model"`
	Name         *string                                                `json:"name"`
	Tools        datatypes.JSONSlice[openai.AssistantObject_Tools_Item] `json:"tools"`
}

func (a *Assistant) ToPublic() any {
	//nolint:govet
	return &openai.AssistantObject{
		a.CreatedAt,
		a.Description,
		a.FileIDs,
		a.ID,
		a.Instructions,
		z.Pointer[map[string]interface{}](a.Metadata.Metadata),
		a.Model,
		a.Name,
		openai.AssistantObjectObjectAssistant,
		a.Tools,
	}
}

func (a *Assistant) FromPublic(obj any) error {
	o, ok := obj.(*openai.AssistantObject)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}
	if o != nil && a != nil {
		//nolint:govet
		*a = Assistant{
			Metadata{
				Base{
					o.Id,
					o.CreatedAt,
				},
				z.Dereference(o.Metadata),
			},
			o.Description,
			o.FileIds,
			o.Instructions,
			o.Model,
			o.Name,
			o.Tools,
		}
	}

	return nil
}

func (a *Assistant) ToolsToChatCompletionTools() ([]openai.ChatCompletionTool, error) {
	if a == nil || len(a.Tools) == 0 {
		return nil, nil
	}

	tools := make([]openai.ChatCompletionTool, 0, len(a.Tools))
	for _, t := range a.Tools {
		chatTool, err := AssistantToolToChatCompletionTool(&t)
		if err != nil {
			return nil, err
		}
		tools = append(tools, chatTool)
	}
	return tools, nil
}

func AssistantToolToChatCompletionTool(t *openai.AssistantObject_Tools_Item) (openai.ChatCompletionTool, error) {
	if ob, err := t.AsAssistantToolsFunction(); err == nil {
		return openai.ChatCompletionTool{
			Function: ob.Function,
			Type:     openai.ChatCompletionToolTypeFunction,
		}, nil
	}

	if ob, err := t.AsAssistantToolsCode(); err == nil {
		return openai.ChatCompletionTool{
			Function: openai.FunctionObject{
				Name: string(ob.Type),
			},
			Type: openai.ChatCompletionToolTypeFunction,
		}, nil
	}

	if ob, err := t.AsAssistantToolsRetrieval(); err == nil {
		return openai.ChatCompletionTool{
			Function: openai.FunctionObject{
				Name: string(ob.Type),
			},
			Type: openai.ChatCompletionToolTypeFunction,
		}, nil
	}

	return openai.ChatCompletionTool{}, fmt.Errorf("unknown assistant tool type")
}
