package server

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/gptscript/pkg/assemble"
	"github.com/gptscript-ai/gptscript/pkg/loader"
	"github.com/gptscript-ai/gptscript/pkg/types"
)

func toolToProgram(ctx context.Context, tool *db.Tool) ([]byte, error) {
	var (
		err error
		prg types.Program
	)

	if url := z.Dereference(tool.URL); url != "" {
		prg, err = loader.Program(ctx, url, z.Dereference(tool.Subtool))
		if err != nil {
			err = NewAPIError(fmt.Sprintf("failed parsing request object: %v", err), InvalidRequestErrorType)
		}
	} else if contents := z.Dereference(tool.Contents); contents != "" {
		prg, err = loader.ProgramFromSource(ctx, contents, z.Dereference(tool.Subtool))
		if err != nil {
			err = NewAPIError(fmt.Sprintf("failed parsing request object: %v", err), InvalidRequestErrorType)
		}
	} else {
		err = NewMustNotBeEmptyError("url or contents")
	}
	if err != nil {
		return nil, err
	}

	b := new(bytes.Buffer)
	if err = assemble.Assemble(prg, b); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

func validateToolEnvVars(envVars []string) error {
	for _, envVar := range envVars {
		parts := strings.Split(envVar, "=")
		if len(parts) != 2 {
			return NewAPIError(fmt.Sprintf("invalid env var: %s", envVar), InvalidRequestErrorType)
		}
	}

	return nil
}
