package db

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/thedadams/clicky-chats/pkg/generated/openai"
	gdb "gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Get gets an object from the database by ID.
func Get(db *gdb.DB, dataObj any, id string) error {
	slog.Debug("Getting", "id", id)
	return db.First(dataObj, "id = ?", id).Error
}

// ListPublicObjects lists objects from the database.
func ListPublicObjects[T Transformer](db *gdb.DB, objs []T) error {
	slog.Debug("Getting all objects", "type", fmt.Sprintf("%T", objs))
	return db.Find(&objs).Error
}

// Create saves an object to the database. It will first set the ID and CreatedAt fields.
// It is the responsibility of the caller to validate the object before calling this function.
func Create(db *gdb.DB, obj Storer) error {
	obj.SetID(uuid.New().String())
	obj.SetCreatedAt(int(time.Now().Unix()))

	slog.Debug("Creating", "id", obj.GetID())
	return CreateAny(db, obj)
}

// CreateAny creates an object from the database type. This should only be used for objects that cannot be retrieved after creation.
func CreateAny(db *gdb.DB, dataObj any) error {
	slog.Debug("Creating", "type", fmt.Sprintf("%T", dataObj))
	return db.Transaction(func(tx *gdb.DB) error {
		return tx.Create(dataObj).Error
	})
}

// Delete deletes an object from the database by ID.
func Delete[T any](db *gdb.DB, id string) error {
	slog.Debug("Deleting", "id", id)
	return db.Transaction(func(tx *gdb.DB) error {
		return tx.Delete(*new(T), "id = ?", id).Error
	})
}

// Modify modifies the object in the database. All validation should be done before calling this function.
func Modify(db *gdb.DB, obj any, id string, updates any) error {
	slog.Debug("Modifying", "type", fmt.Sprintf("%T", obj), "id", id, "updates", updates)
	return db.Transaction(func(tx *gdb.DB) error {
		return tx.Model(obj).Clauses(clause.Returning{}).Where("id = ?", id).Updates(updates).Error
	})
}

// CancelRun cancels a run that is in progress. If the run is not in progress, it will return an error.
func CancelRun(db *gdb.DB, id string) (*openai.RunObject, error) {
	run := new(Run)
	if err := db.Transaction(func(tx *gdb.DB) error {
		if err := Get(tx, run, id); err != nil {
			return err
		}

		if run.Status != string(openai.InProgress) {
			return fmt.Errorf("cannot cancel run with status %s, must be %s", run.Status, openai.InProgress)
		}

		update := map[string]interface{}{
			"status":       string(openai.Cancelled),
			"cancelled_at": int(time.Now().Unix()),
		}

		return tx.Model(run).Clauses(clause.Returning{}).Updates(update).Error
	}); err != nil {
		return nil, err
	}

	return run.ToPublic().(*openai.RunObject), nil
}
