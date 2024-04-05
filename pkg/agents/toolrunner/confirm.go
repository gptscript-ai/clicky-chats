package toolrunner

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/acorn-io/broadcaster"
	"github.com/gptscript-ai/clicky-chats/pkg/agents"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/gptscript/pkg/engine"
	"github.com/gptscript-ai/gptscript/pkg/runner"
	"github.com/gptscript-ai/gptscript/pkg/server"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type toolRunnerConfirm struct {
	confirm bool
	db      *gorm.DB
	tool    *db.RunToolObject
	events  *broadcaster.Broadcaster[server.Event]
}

func (t *toolRunnerConfirm) Confirm(ctx context.Context, cmd string) error {
	if !t.confirm {
		return nil
	}

	engineCtx, ok := engine.FromContext(ctx)
	if !ok {
		return nil
	}

	// Send the event to say that we're waiting for confirmation.
	t.events.C <- server.Event{
		Event: runner.Event{
			Time:        time.Now(),
			CallContext: engineCtx,
			Type:        agents.EventTypeCallConfirm,
		},
		RunID:   engineCtx.Parent.ID,
		Program: engineCtx.Program,
		Input:   cmd,
	}

	var confirmed *bool
	timer := time.NewTimer(time.Second)
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("confirmation timed out for tool run %s", t.tool.ID)
		case <-timer.C:
		}

		if err := t.db.Transaction(func(tx *gorm.DB) error {
			if err := db.Get(tx, t.tool, t.tool.ID); err != nil {
				return err
			}

			if t.tool.Confirmed == nil {
				return nil
			}

			confirmed = t.tool.Confirmed

			// Set the confirmed field back to nil so future confirm requests are properly handled.
			return tx.Model(t.tool).Clauses(clause.Returning{}).Where("id = ?", t.tool.ID).Updates(map[string]any{
				"confirmed": nil,
			}).Error
		}); err != nil {
			return err
		}

		// If we have a result...
		if confirmed != nil {
			// Include an event to say that we've confirmed.
			t.events.C <- server.Event{
				Event: runner.Event{
					Time:        time.Now(),
					CallContext: engineCtx,
					Type:        agents.EventTypeCallConfirmResponse,
				},
				RunID:   engineCtx.Parent.ID,
				Program: engineCtx.Program,
				Output:  strconv.FormatBool(*confirmed),
			}

			// ...and it's true, we're done.
			if *confirmed {
				return nil
			}

			// ...and it's false, we need to indicate that by returning an error.
			return fmt.Errorf("confirmation was not given for tool run %s", t.tool.ID)
		}

		timer.Reset(time.Second)
	}
}
