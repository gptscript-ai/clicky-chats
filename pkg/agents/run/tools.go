package run

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"github.com/gptscript-ai/clicky-chats/pkg/tools"
	"github.com/gptscript-ai/gptscript/pkg/loader"
	"github.com/gptscript-ai/gptscript/pkg/types"
)

// populateTools returns the function definition used for chat completion from the provided link and subtool.
// The run agent will use these when making chat completion requests for runs.
func populateTools(ctx context.Context) (map[string]*openai.FunctionObject, error) {
	builtInToolDefinitions := make(map[string]*openai.FunctionObject, len(tools.GPTScriptDefinitions()))
	for toolName, toolDef := range tools.GPTScriptDefinitions() {
		if toolDef.Link == "" || toolDef.Link == tools.SkipLoadingTool {
			slog.Info("Skipping tool", "name", toolName)
			continue
		}

		prg, err := loader.Program(ctx, toolDef.Link, toolDef.Subtool)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize program %q: %w", toolName, err)
		}

		builtInToolDefinitions[toolName], err = programToFunction(&prg, toolName)
		if err != nil {
			return nil, err
		}
	}

	return builtInToolDefinitions, nil
}

func programToFunction(prg *types.Program, toolName string) (*openai.FunctionObject, error) {
	b, err := json.Marshal(prg.ToolSet[prg.EntryToolID].Parameters.Arguments)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal parameters for tool %q: %w", toolName, err)
	}

	var fp *openai.FunctionParameters
	if err = json.Unmarshal(b, &fp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal parameters for tool %q: %w", toolName, err)
	}

	return &openai.FunctionObject{
		Name:        tools.GPTScriptToolNamePrefix + toolName,
		Description: z.Pointer(prg.ToolSet[prg.EntryToolID].Description),
		Parameters:  fp,
	}, nil
}
