package db

import (
	"fmt"

	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type Assistant struct {
	Metadata     `json:",inline"`
	Description  *string                                                        `json:"description"`
	FileIDs      datatypes.JSONSlice[string]                                    `json:"file_ids"`
	Instructions *string                                                        `json:"instructions"`
	Model        string                                                         `json:"model"`
	Name         *string                                                        `json:"name"`
	Tools        datatypes.JSONSlice[openai.ExtendedAssistantObject_Tools_Item] `json:"tools"`
	// Not part of the OpenAI API
	GPTScriptTools datatypes.JSONSlice[string] `json:"gptscript_tools"`
}

func (a *Assistant) IDPrefix() string {
	return "asst_"
}

func (a *Assistant) ToPublic() any {
	//nolint:govet
	return &openai.ExtendedAssistantObject{
		a.CreatedAt,
		a.Description,
		a.FileIDs,
		z.Pointer[[]string](a.GPTScriptTools),
		a.ID,
		a.Instructions,
		z.Pointer[map[string]interface{}](a.Metadata.Metadata),
		a.Model,
		a.Name,
		openai.ExtendedAssistantObjectObjectAssistant,
		a.Tools,
	}
}

func (a *Assistant) ToPublicOpenAI() any {
	var tools []openai.AssistantObject_Tools_Item
	for _, t := range a.Tools {
		tools = append(tools, openai.AssistantObject_Tools_Item(t))
	}

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
		tools,
	}
}

func (a *Assistant) FromPublic(obj any) error {
	o, ok := obj.(*openai.ExtendedAssistantObject)
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
			datatypes.NewJSONSlice(z.Dereference(o.GptscriptTools)),
		}
	}

	return nil
}

func (a *Assistant) ToolsToChatCompletionTools(gptScriptToolDefinitions, tools map[string]*openai.FunctionObject) ([]openai.ChatCompletionTool, error) {
	if a == nil || len(a.Tools)+len(a.GPTScriptTools) == 0 {
		return nil, nil
	}

	chatCompletionTools := make([]openai.ChatCompletionTool, 0, len(a.Tools)+len(a.GPTScriptTools))
	for _, t := range a.Tools {
		chatTool, err := assistantToolToChatCompletionTool(&t, gptScriptToolDefinitions)
		if err != nil {
			return nil, err
		}
		chatCompletionTools = append(chatCompletionTools, chatTool)
	}

	for _, t := range a.GPTScriptTools {
		function := gptScriptToolDefinitions[t]
		if function == nil {
			function = tools[t]
			if function == nil {
				return nil, fmt.Errorf("tool %s not found", t)
			}
		}

		chatTool := openai.ChatCompletionTool{
			Function: *function,
			Type:     openai.ChatCompletionToolTypeFunction,
		}
		chatCompletionTools = append(chatCompletionTools, chatTool)
	}
	return chatCompletionTools, nil
}

func assistantToolToChatCompletionTool(t *openai.ExtendedAssistantObject_Tools_Item, gptScriptToolDefinitions map[string]*openai.FunctionObject) (openai.ChatCompletionTool, error) {
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

	return openai.ChatCompletionTool{}, fmt.Errorf("unknown built-in assistant tool type")
}
