package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"time"

	"github.com/acorn-io/broadcaster"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"github.com/gptscript-ai/gptscript/pkg/gptscript"
	"github.com/gptscript-ai/gptscript/pkg/server"
	"github.com/gptscript-ai/gptscript/pkg/types"
	"gorm.io/gorm"
)

type Statuser interface {
	GetStatus() string
}

func RunTool(ctx context.Context, l *slog.Logger, caster *broadcaster.Broadcaster[server.Event], gdb *gorm.DB, opts *gptscript.Options, prg types.Program, envs []string, arguments, runID, runStepID string) (string, Usage, error) {
	var (
		usage  = make(usageSet)
		idCtx  = server.ContextWithNewID(ctx)
		id     = server.IDFromContext(idCtx)
		events = caster.Subscribe()
	)
	go func() {
		var index int
		for e := range events.C {
			if e.RunID != id {
				continue
			}

			if err := usage.addTokens(e); err != nil {
				l.Error("failed to count usage for event", "gptscript-completion-id", e.ChatCompletionID)
			}

			runStepEvent := db.FromGPTScriptEvent(e, runID, runStepID, index, false)
			if err := db.Create(gdb, runStepEvent); err != nil {
				l.Error("failed to create run step event", "error", err)
			}
			index++
		}

		// Create final event that just says we're done with this run step.
		runStepEvent := db.FromGPTScriptEvent(server.Event{}, runID, runStepID, index, true)
		if err := db.Create(gdb, runStepEvent); err != nil {
			l.Error("failed to create run step event", "error", err)
		}
		l.Debug("done receiving events")
	}()

	output, err := runToolCall(idCtx, opts, prg, envs, arguments)
	events.Close()
	if errors.Is(err, context.DeadlineExceeded) {
		output = "The tool call took too long to complete, aborting"
	} else if execErr := new(exec.ExitError); errors.As(err, &execErr) {
		output = fmt.Sprintf("The tool call returned an exit code of %d with message %q, aborting", execErr.ExitCode(), execErr.String())
	} else if err != nil {
		return "", SumUsage(usage.asSlice()), err
	}

	return output, SumUsage(usage.asSlice()), nil
}

// Transform marshals in into JSON then unmarshalls it into out
func Transform[O any](in any) (out O, err error) {
	data, err := json.Marshal(in)
	if err != nil {
		return out, err
	}

	return out, json.Unmarshal(data, &out)
}

func runToolCall(ctx context.Context, opts *gptscript.Options, prg types.Program, envs []string, arguments string) (string, error) {
	gpt, err := gptscript.New(opts)
	if err != nil {
		return "", err
	}
	defer gpt.Close()

	output, err := gpt.Run(ctx, prg, envs, arguments)
	if err != nil {
		return "", err
	}

	return output, nil
}

// PollForCancellation will poll for the run step with the given id. If the run step
// has been canceled, then the corresponding context will be canceled.
func PollForCancellation(ctx context.Context, cancel func(), gdb *gorm.DB, obj Statuser, id string, pollingInterval time.Duration) {
	timer := time.NewTimer(pollingInterval)
	for {
		select {
		case <-ctx.Done():
			// Ensure that the timer channel is drained.
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
		}

		if err := gdb.Model(obj).Where("id = ?", id).First(obj).Error; err != nil || obj.GetStatus() != string(openai.RunStepObjectStatusInProgress) {
			cancel()
			return
		}

		timer.Reset(pollingInterval)
	}
}
