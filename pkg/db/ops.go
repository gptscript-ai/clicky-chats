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

// GetPublicObject gets an object from the database by ID.
func GetPublicObject[T Transformer](db *gdb.DB, id string) (any, error) {
	var obj T
	slog.Debug("Getting", "id", id)
	if err := db.First(obj, "id = ?", id).Error; err != nil {
		return nil, err
	}

	return obj.ToPublic(), nil
}

// Get gets an object from the database by ID.
func Get(db *gdb.DB, dataObj Transformer, id string) error {
	slog.Debug("Getting", "id", id)
	return db.First(dataObj, "id = ?", id).Error
}

// ListPublicObjects lists objects from the database.
func ListPublicObjects[T Transformer](db *gdb.DB) ([]any, error) {
	var objs []T
	slog.Debug("Getting all objects", "type", fmt.Sprintf("%T", objs))
	if err := db.Find(&objs).Error; err != nil {
		return nil, err
	}

	validObjs := make([]any, 0, len(objs))
	for _, o := range objs {
		validObjs = append(validObjs, o.ToPublic())
	}

	return validObjs, nil
}

// CreateFromPublic creates an object from a public object and saves it to the database.
// The Transformer object passed here will be used to convert the public object to the database object.
// It is the responsibility of the caller to validate the object before calling this function.
func CreateFromPublic[T Transformer](db *gdb.DB, publicObj any) (any, error) {
	var obj T
	if err := obj.FromPublic(publicObj); err != nil {
		return nil, err
	}

	obj.SetID(uuid.New().String())
	obj.SetCreatedAt(int(time.Now().Unix()))

	slog.Debug("Creating", "id", obj.GetID())
	if err := db.Transaction(func(tx *gdb.DB) error {
		return tx.Create(obj).Error
	}); err != nil {
		return nil, err
	}

	return obj.ToPublic(), nil
}

// Create creates an object from the database type. This should only be used for objects that cannot be retrieved after creation.
func Create(db *gdb.DB, dataObj any) error {
	slog.Debug("Creating", "type", fmt.Sprintf("%T", dataObj))
	return db.Transaction(func(tx *gdb.DB) error {
		return tx.Create(dataObj).Error
	})
}

// Delete deletes an object from the database by ID.
func Delete[T Transformer](db *gdb.DB, id string) error {
	slog.Debug("Deleting", "id", id)
	return db.Transaction(func(tx *gdb.DB) error {
		return tx.Delete(*new(T), "id = ?", id).Error
	})
}

// Modify modifies the object in the database. All validation should be done before calling this function.
func Modify[T Transformer](db *gdb.DB, id string, updates any) (any, error) {
	var dataObj T
	slog.Debug("Modifying", "type", fmt.Sprintf("%T", dataObj), "id", id, "updates", updates)
	if err := db.Transaction(func(tx *gdb.DB) error {
		return tx.Model(dataObj).Clauses(clause.Returning{}).Where("id = ?", id).Updates(updates).Error
	}); err != nil {
		return nil, err
	}

	return dataObj.ToPublic(), nil
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
