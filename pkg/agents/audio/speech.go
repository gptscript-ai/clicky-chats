package audio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/acorn-io/z"
	cclient "github.com/gptscript-ai/clicky-chats/pkg/client"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"gorm.io/gorm"
)

func (a *agent) runSpeech(ctx context.Context) error {
	slog.Debug("checking for an transcription request to process")
	var (
		speechRequest = new(db.CreateSpeechRequest)
		gdb           = a.db.WithContext(ctx)
	)
	if err := db.Dequeue(gdb, speechRequest, a.id); err != nil {
		return err
	}

	l := slog.With("type", "speech", "id", speechRequest.ID)
	l.Debug("processing request")

	data, err := json.Marshal(speechRequest.ToPublic())
	if err != nil {
		return fmt.Errorf("failed to marshal create speech request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.speechURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create speech request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}

	sr := new(db.CreateSpeechResponse)
	code, err := cclient.SendRequest(a.client, req, &sr.Content)
	if err != nil {
		l.Error("failed to send speech create request", "err", err)
		sr.Error = z.Pointer(err.Error())
	}

	sr.StatusCode = code
	sr.RequestID = speechRequest.ID
	sr.Done = true

	if err := gdb.Transaction(func(tx *gorm.DB) error {
		if err := db.Create(tx, sr); err != nil {
			return err
		}

		return tx.Model(speechRequest).Where("id = ?", speechRequest.ID).Update("done", true).Error
	}); err != nil {
		l.Error("failed to store speech create response", "err", err)
	}

	return nil
}
