package db

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
	gdb "gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Get gets an object from the database by ID.
func Get(db *gdb.DB, dataObj any, id string) error {
	slog.Debug("Getting", "id", id)
	return db.First(dataObj, "id = ?", id).Error
}

// List lists objects from the database.
func List[T any](db *gdb.DB, objs *[]T) error {
	slog.Debug("Getting all objects", "type", fmt.Sprintf("%T", *objs))
	return db.Find(objs).Error
}

// Create saves an object to the database. It will first set the ID and CreatedAt fields.
// It is the responsibility of the caller to validate the object before calling this function.
func Create(db *gdb.DB, obj Storer) error {
	SetNewID(obj)
	obj.SetCreatedAt(int(time.Now().Unix()))

	slog.Debug("Creating", "id", obj.GetID())
	return CreateAny(db, obj)
}

// CreateAny creates an object from the database type. This should only be used for objects that cannot be retrieved after creation.
func CreateAny(db *gdb.DB, dataObj any) error {
	slog.Debug("Creating", "type", fmt.Sprintf("%T", dataObj))
	return db.Transaction(func(tx *gdb.DB) error {
		return tx.Model(dataObj).Create(dataObj).Error
	})
}

// Delete deletes an object from the database by ID.
func Delete[T any](db *gdb.DB, id string) error {
	slog.Debug("Deleting", "id", id)
	return db.Transaction(func(tx *gdb.DB) error {
		return tx.Delete(new(T), "id = ?", id).Error
	})
}

// DeleteExpired deletes objects from the database created before or at the given expiration time.
func DeleteExpired(db *gdb.DB, expiration time.Time, objs ...Storer) error {
	slog.Debug("Deleting expired", "expiration", expiration, "objs", fmt.Sprintf("%T", objs))
	return db.Transaction(func(tx *gdb.DB) error {
		for _, obj := range objs {
			if err := tx.Where("created_at <= ?", expiration.Unix()).Delete(obj).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

// Dequeue dequeues the next request from the database, marking it as claimed by the given agent.
func Dequeue(db *gdb.DB, request Storer, agentID string) error {
	err := db.Model(request).Transaction(func(tx *gdb.DB) error {
		if err := tx.Where("claimed_by IS NULL").Or("claimed_by = ? AND done = false", agentID).
			Order("created_at desc").
			First(request).Error; err != nil {
			return err
		}

		if err := tx.Where("id = ?", request.GetID()).
			Updates(map[string]interface{}{"claimed_by": agentID}).Error; err != nil {
			return err
		}

		return nil
	})
	if err != nil && !errors.Is(err, gdb.ErrRecordNotFound) {
		err = fmt.Errorf("failed to dequeue request %T: %w", request, err)
	}

	return err
}

// Modify modifies the object in the database. All validation should be done before calling this function.
func Modify(db *gdb.DB, obj any, id string, updates any) error {
	slog.Debug("Modifying", "type", fmt.Sprintf("%T", obj), "id", id, "updates", updates)
	return db.Transaction(func(tx *gdb.DB) error {
		return tx.Model(obj).Clauses(clause.Returning{}).Where("id = ?", id).Updates(updates).Error
	})
}

// CancelRun cancels a run that is in progress. If the run is not in progress, it will return an error.
func CancelRun(db *gdb.DB, id string) (*Run, error) {
	run := new(Run)
	if err := db.Transaction(func(tx *gdb.DB) error {
		if err := Get(tx, run, id); err != nil {
			return err
		}

		if run.Status != string(openai.RunObjectStatusInProgress) && run.Status != string(openai.RunObjectStatusRequiresAction) && run.Status != string(openai.RunObjectStatusQueued) {
			return fmt.Errorf("cannot cancel run with status %s", run.Status)
		}

		update := map[string]any{
			"status":       string(openai.RunObjectStatusCancelled),
			"cancelled_at": int(time.Now().Unix()),
		}

		var runSteps []RunStep
		if err := tx.Model(new(RunStep)).Where("run_id = ?", run.ID).Where("status = ?", string(openai.RunObjectStatusInProgress)).Find(&runSteps).Error; err != nil {
			return err
		}

		for _, runStep := range runSteps {
			if err := tx.Model(&runStep).Clauses(clause.Returning{}).Where("id = ?", runStep.ID).Updates(update).Error; err != nil {
				return err
			}

			run.EventIndex++
			runEvent := &RunEvent{
				EventName: string(openai.RunStepStreamEvent5EventThreadRunStepCancelled),
				JobResponse: JobResponse{
					RequestID: run.ID,
				},
				RunStep:     datatypes.NewJSONType(&runStep),
				ResponseIdx: run.EventIndex,
			}

			if err := Create(tx, runEvent); err != nil {
				return err
			}
		}

		update["event_index"] = run.EventIndex
		if err := tx.Model(run).Clauses(clause.Returning{}).Updates(update).Error; err != nil {
			return err
		}

		run.EventIndex++
		runEvent := &RunEvent{
			EventName: string(openai.RunStreamEvent7EventThreadRunCancelled),
			JobResponse: JobResponse{
				RequestID: run.ID,
				Done:      true,
			},
			Run:         datatypes.NewJSONType(run),
			ResponseIdx: run.EventIndex + 1,
		}

		return Create(tx, runEvent)
	}); err != nil {
		return nil, err
	}

	return run, nil
}
