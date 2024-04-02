package db

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"github.com/gptscript-ai/clicky-chats/pkg/tools"
	"github.com/gptscript-ai/gptscript/pkg/assemble"
	"github.com/gptscript-ai/gptscript/pkg/loader"
	"github.com/gptscript-ai/gptscript/pkg/types"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Tool struct {
	Base        `json:",inline"`
	Name        string                      `json:"name"`
	Description string                      `json:"description"`
	Contents    *string                     `json:"contents"`
	URL         *string                     `json:"url"`
	Subtool     *string                     `json:"subtool"`
	EnvVars     datatypes.JSONSlice[string] `json:"env_vars"`
	// Not part of the public API
	Program datatypes.JSON `json:"program"`
}

func (t *Tool) IDPrefix() string {
	return "tool-"
}

func (t *Tool) ToPublic() any {
	//nolint:govet
	return &openai.XToolObject{
		t.Contents,
		t.CreatedAt,
		&t.Description,
		z.Pointer[[]string](t.EnvVars),
		t.ID,
		&t.Name,
		openai.XToolObjectObjectTool,
		t.Subtool,
		t.URL,
	}
}

func (t *Tool) FromPublic(obj any) error {
	o, ok := obj.(*openai.XToolObject)
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
			z.Dereference(o.Name),
			z.Dereference(o.Description),
			o.Contents,
			o.Url,
			o.Subtool,
			datatypes.NewJSONSlice(z.Dereference(o.EnvVars)),
			nil,
		}
	}

	return nil
}

type BuiltInTool struct {
	Tool   `json:",inline"`
	Commit string `json:"commit"`
}

func LoadBuiltInTool(ctx context.Context, gdb *gorm.DB, toolName string, toolDef tools.ToolDefinition) (types.Program, error) {
	var (
		prg types.Program
		err error

		reloadPrg = true
	)

	if toolDef.Commit != "" {
		// Check to see if the existing tool needs to be updated.
		builtInTool := new(BuiltInTool)
		if err = gdb.Model(builtInTool).Where("name = ?", toolName).First(builtInTool).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return prg, err
		}

		if builtInTool.Commit == toolDef.Commit {
			slog.Info("Using existing builtin tool", "name", toolName, "commit", toolDef.Commit)
			prg, err = loader.ProgramFromSource(ctx, string(builtInTool.Program), toolDef.SubTool)
			if err != nil {
				slog.Warn("Failed to load builtin tool, reloading from source URL", "name", toolName, "commit", toolDef.Commit, "error", err)
			} else {
				reloadPrg = false
			}
		}
	}

	if reloadPrg {
		if strings.HasPrefix(toolDef.Link, "github.com") && toolDef.Commit != "" {
			// Ensure we are getting the right commit for the tool.
			toolDef.Link = toolDef.Link + "@" + toolDef.Commit
		}
		prg, err = loader.Program(ctx, toolDef.Link, toolDef.SubTool)
		if err != nil {
			return prg, fmt.Errorf("failed to initialize program %q: %w", toolName, err)
		}

		b := new(bytes.Buffer)
		if err = assemble.Assemble(prg, b); err != nil {
			return prg, fmt.Errorf("failed to assemble program %q: %w", toolName, err)
		}

		builtInTool := &BuiltInTool{
			Tool: Tool{
				Name:        toolName,
				Description: prg.ToolSet[prg.EntryToolID].Description,
				Contents:    nil,
				URL:         &toolDef.Link,
				Subtool:     &toolDef.SubTool,
				EnvVars:     nil,
				Program:     b.Bytes(),
			},
			Commit: toolDef.Commit,
		}

		if err = Create(gdb, builtInTool); err != nil {
			// Warn, but don't error. The program will still run if we fail to create the builtin tool in the database.
			slog.Warn("Failed to create builtin tool", "name", toolName, "commit", toolDef.Commit, "error", err)
		}
	}

	return prg, nil
}
