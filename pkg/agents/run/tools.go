package run

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"github.com/gptscript-ai/clicky-chats/pkg/tools"
	"github.com/gptscript-ai/gptscript/pkg/types"
	"gorm.io/gorm"
)

// populateTools returns the function definition used for chat completion for the built-in tools. The database is
// checked first to see if the tool has already been loaded, it will be loaded from the URL again if necessary. The run
// agent will use these when making chat completion requests for runs.
func populateTools(ctx context.Context, l *slog.Logger, gdb *gorm.DB) (map[string]*openai.FunctionObject, error) {
	builtInToolDefinitions := make(map[string]*openai.FunctionObject, len(tools.GPTScriptDefinitions()))
	for toolName, toolDef := range tools.GPTScriptDefinitions() {
		if toolDef.Link == "" || toolDef.Link == tools.SkipLoadingTool {
			l.Info("Skipping tool", "name", toolName)
			continue
		}

		prg, err := db.LoadBuiltInTool(ctx, gdb, toolName, toolDef)
		if err != nil {
			return nil, err
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
