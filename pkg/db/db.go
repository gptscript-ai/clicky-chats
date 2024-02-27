package db

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type DB struct {
	*gorm.DB
	autoMigrate bool
}

func New(dsn string, autoMigrate bool) (*DB, error) {
	var (
		gdb   gorm.Dialector
		conns = 1
	)
	if strings.HasPrefix(dsn, "sqlite://") {
		gdb = sqlite.Open(strings.TrimPrefix(dsn, "sqlite://"))
	} else {
		dsn = strings.TrimPrefix(dsn, "mysql://")
		conns = 5
		gdb = mysql.Open(dsn)
	}
	db, err := gorm.Open(gdb, &gorm.Config{
		SkipDefaultTransaction: true,
		Logger: logger.New(log.Default(), logger.Config{
			SlowThreshold: 200 * time.Millisecond,
			Colorful:      true,
			LogLevel:      logger.Silent,
		}),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetConnMaxLifetime(3 * time.Minute)
	sqlDB.SetMaxIdleConns(conns)
	sqlDB.SetMaxOpenConns(conns)

	return &DB{
		DB:          db,
		autoMigrate: autoMigrate,
	}, nil
}

func (db *DB) AutoMigrate() error {
	if !db.autoMigrate {
		return nil
	}

	return db.DB.AutoMigrate(
		Thread{},
		Message{},
		Run{},
		MessageFile{},
		File{},
		Assistant{},
		AssistantFile{},
		FineTuningJob{},
		Model{},
		Speech{},
		ChatCompletionRequest{},
		ChatCompletionResponse{},
	)
}

func (db *DB) Check(w http.ResponseWriter, _ *http.Request) {
	sqlDB, err := db.DB.DB()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	if err = sqlDB.Ping(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	_, _ = w.Write([]byte("ok"))
}
