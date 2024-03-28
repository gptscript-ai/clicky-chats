package image

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
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/gorm"
)

func (a *agent) runGenerations(ctx context.Context) error {
	slog.Debug("checking for an image create request to process")
	var (
		createRequest = new(db.CreateImageRequest)
		gdb           = a.db.WithContext(ctx)
	)
	if err := db.Dequeue(gdb, createRequest, a.id); err != nil {
		return err
	}

	l := slog.With("type", "createimage", "id", createRequest.ID)
	l.Debug("processing request")

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
	if err = gdb.Transaction(func(tx *gorm.DB) error {
		if err = db.Create(tx, ir); err != nil {
			return err
		}
		return tx.Model(createRequest).Where("id = ?", createRequest.ID).Update("done", true).Error
	}); err != nil {
		l.Error("failed to store image create response", "err", err)
	}

	a.trigger.Ready(createRequest.ID)

	return nil
}
