package steprunner

import (
	"context"
	"fmt"
	"time"

	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type stepConfirmer struct {
	confirm    bool
	db         *gorm.DB
	run        *db.Run
	runStep    *db.RunStep
	toolCallID string
}

func (s *stepConfirmer) Confirm(ctx context.Context, cmd string) error {
	if !s.confirm {
		return nil
	}

	if s.run.RequiredAction.Data() == nil {
		s.run.RequiredAction = datatypes.NewJSONType(new(db.RunRequiredAction))
	}

	s.run.RequiredAction.Data().XConfirm = &db.RunRequiredActionXConfirm{
		Action: cmd,
		ID:     s.toolCallID,
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		// Set the required_action field on the run
		if err := tx.Model(s.run).Clauses(clause.Returning{}).Where("id = ?", s.run.ID).Updates(map[string]any{
			"required_action": s.run.RequiredAction,
			"event_index":     s.run.EventIndex + 1,
			"status":          string(openai.RunObjectStatusRequiresConfirmation),
		}).Error; err != nil {
			return err
		}

		// Create an event that indicates that the run needs to be confirmed.
		runEvent := &db.RunEvent{
			JobResponse: db.JobResponse{
				RequestID: s.run.ID,
			},
			EventName:   "thread.run.requires_confirmation",
			Run:         datatypes.NewJSONType(s.run),
			ResponseIdx: s.run.EventIndex,
		}

		return db.Create(tx, runEvent)
	}); err != nil {
		return fmt.Errorf("failed to set required action on run %s: %w", s.run.ID, err)
	}

	// Wait for the run to be confirmed.
	timer := time.NewTimer(time.Second)
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("confirmation timed out for run %s and call %s", s.run.ID, s.toolCallID)
		case <-timer.C:
		}

		if err := s.db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(s.run).Where("id = ?", s.run.ID).First(s.run).Error; err != nil {
				return err
			}
			if s.run.Status == string(openai.RunObjectStatusRequiresConfirmation) {
				return nil
			}

			return tx.Model(s.runStep).Where("id = ?", s.runStep.ID).First(s.runStep).Error
		}); err != nil {
			return fmt.Errorf("failed to check run [%s] and runstep [%s] status: %w", s.run.ID, s.runStep.ID, err)
		}

		if s.run.Status == string(openai.RunObjectStatusInProgress) {
			if c, err := s.runStep.RunStepConfirmed(s.toolCallID); err != nil || c {
				return err
			}

			return fmt.Errorf("runstep %s tool call %s not confirmed", s.runStep.ID, s.toolCallID)
		}

		timer.Reset(time.Second)
	}
}
