package image

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/acorn-io/z"
	cclient "github.com/gptscript-ai/clicky-chats/pkg/client"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/gorm"
)

func (a *agent) runGenerations(ctx context.Context) error {
	slog.Debug("checking for an image create request to process")
	// Look for a new create image request and claim it. Also, query for the other objects we need.
	createRequest := new(db.CreateImageRequest)
	if err := a.db.WithContext(ctx).Model(createRequest).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("claimed_by IS NULL").Or("claimed_by = ? AND done = false", a.id).
			Order("created_at desc").
			First(createRequest).Error; err != nil {
			return err
		}

		if err := tx.Where("id = ?", createRequest.ID).
			Updates(map[string]interface{}{"claimed_by": a.id}).Error; err != nil {
			return err
		}

		return nil
	}); err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to get create image request: %w", err)
		}
		return err
	}

	l := slog.With("type", "createimage", "id", createRequest.ID)
	l.Debug("Processing request")

	data, err := json.Marshal(createRequest.ToPublic())
	if err != nil {
		return fmt.Errorf("failed to marshal create image request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.generationsURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create image request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}

	oir, ir := new(openai.ImagesResponse), new(db.ImagesResponse)
	code, err := cclient.SendRequest(a.client, req, oir)
	if err := ir.FromPublic(oir); err != nil {
		l.Error("failed to convert image response", "err", err)
	}

	// Process the request error here.
	if err != nil {
		l.Error("failed to send image create request", "err", err)
		ir.Error = z.Pointer(err.Error())
	}

	ir.StatusCode = code
	ir.RequestID = createRequest.ID
	ir.Done = true

	// Store the completed response and mark the request as done.
	if err = a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err = db.Create(tx, ir); err != nil {
			return err
		}
		return tx.Model(createRequest).Where("id = ?", createRequest.ID).Update("done", true).Error
	}); err != nil {
		l.Error("failed to store image create response", "err", err)
	}

	return nil
}
