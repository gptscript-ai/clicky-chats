package db

import (
	"fmt"

	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
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

func (a *Assistant) IDPrefix() string {
	return "asst_"
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

func (a *Assistant) ToolsToChatCompletionTools(gptScriptToolDefinitions, tools map[string]*openai.FunctionObject) ([]openai.ChatCompletionTool, error) {
	if a == nil || len(a.Tools) == 0 {
		return nil, nil
	}

	chatCompletionTools := make([]openai.ChatCompletionTool, 0, len(a.Tools))
	for _, t := range a.Tools {
		chatTool, err := assistantToolToChatCompletionTool(&t, gptScriptToolDefinitions, tools)
		if err != nil {
			return nil, err
		}
		chatCompletionTools = append(chatCompletionTools, chatTool)
	}

	return chatCompletionTools, nil
}

func (a *Assistant) ExtractGPTScriptTools(gptScriptToolDefinitions map[string]*openai.FunctionObject) ([]string, error) {
	if a == nil || len(a.Tools) == 0 {
		return nil, nil
	}

	toolIDs := make([]string, 0, len(a.Tools))
	for _, t := range a.Tools {
		if ob, err := t.AsXAssistantToolsGPTScript(); err == nil && ob.Type == openai.Gptscript {
			if _, ok := gptScriptToolDefinitions[ob.XTool]; !ok {
				toolIDs = append(toolIDs, ob.XTool)
			}
		}
	}

	return toolIDs, nil
}

func assistantToolToChatCompletionTool(t *openai.AssistantObject_Tools_Item, gptScriptToolDefinitions, tools map[string]*openai.FunctionObject) (openai.ChatCompletionTool, error) {
	if ob, err := t.AsAssistantToolsFunction(); err == nil && ob.Type == openai.AssistantToolsFunctionTypeFunction {
		return openai.ChatCompletionTool{
			Function: ob.Function,
			Type:     openai.ChatCompletionToolTypeFunction,
		}, nil
	}

	if ob, err := t.AsAssistantToolsCode(); err == nil && ob.Type == openai.AssistantToolsCodeTypeCodeInterpreter {
		return openai.ChatCompletionTool{
			Function: z.Dereference(gptScriptToolDefinitions[string(ob.Type)]),
			Type:     openai.ChatCompletionToolTypeFunction,
		}, nil
	}

	if ob, err := t.AsAssistantToolsRetrieval(); err == nil && ob.Type == openai.AssistantToolsRetrievalTypeRetrieval {
		return openai.ChatCompletionTool{
			Function: z.Dereference(gptScriptToolDefinitions[string(ob.Type)]),
			Type:     openai.ChatCompletionToolTypeFunction,
		}, nil
	}

	if ob, err := t.AsXAssistantToolsGPTScript(); err == nil && ob.Type == openai.Gptscript {
		function := gptScriptToolDefinitions[ob.XTool]
		if function == nil {
			function = tools[ob.XTool]
			if function == nil {
				return openai.ChatCompletionTool{}, fmt.Errorf("tool %s not found", t)
			}
		}

		return openai.ChatCompletionTool{
			Function: *function,
			Type:     openai.ChatCompletionToolTypeFunction,
		}, nil
	}

	return openai.ChatCompletionTool{}, fmt.Errorf("unknown built-in assistant tool type")
}
