package extendedapi

import (
	"fmt"

	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
)

var allowedGPTScriptTools = map[string]struct{}{
	"web_browsing": {},
}

type CreateAssistantRequest struct {
	openai.CreateAssistantRequest
	GPTScriptTools []string `json:"gptscript_tools,omitempty"`
}

type Assistant struct {
	openai.AssistantObject `json:",inline"`
	GPTScriptTools         []string `json:"gptscript_tools,omitempty"`
}

type ModifyAssistantRequest struct {
	openai.ModifyAssistantRequest
	GPTScriptTools []string `json:"gptscript_tools,omitempty"`
}

func ValidateGPTScriptTools(toolNames []string) error {
	for _, tool := range toolNames {
		if _, ok := allowedGPTScriptTools[tool]; !ok {
			return fmt.Errorf("invalid tool %s", tool)
		}
	}
	return nil
}
