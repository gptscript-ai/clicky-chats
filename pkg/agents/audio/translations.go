package audio

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"

	"github.com/acorn-io/z"
	cclient "github.com/gptscript-ai/clicky-chats/pkg/client"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/gorm"
)

func (a *agent) runTranslations(ctx context.Context) error {
	slog.Debug("checking for an translation request to process")
	var (
		translationRequest = new(db.CreateTranslationRequest)
		gdb                = a.db.WithContext(ctx)
	)
	if err := db.Dequeue(gdb, translationRequest, a.id); err != nil {
		return err
	}

	l := slog.With("type", "translation", "id", translationRequest.ID)
	l.Debug("processing request")

	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	publicRequest := translationRequest.ToPublic().(*openai.CreateTranslationRequest)
	part, err := writer.CreateFormFile("file", publicRequest.File.Filename())
	if err != nil {
		return fmt.Errorf("failed to create form file: %w", err)
	}
	r, err := publicRequest.File.Reader()
	if err != nil {
		return fmt.Errorf("failed to get translation reader: %w", err)
	}
	if _, err := io.Copy(part, r); err != nil {
		return fmt.Errorf("failed to copy file to form file: %w", err)
	}

	if err := writer.WriteField("model", translationRequest.Model); err != nil {
		return fmt.Errorf("failed to write model field: %w", err)
	}

	if prompt := translationRequest.Prompt; prompt != nil {
		if err := writer.WriteField("prompt", *prompt); err != nil {
			return fmt.Errorf("failed to write prompt field: %w", err)
		}
	}

	if format := translationRequest.ResponseFormat; format != nil {
		if err := writer.WriteField("response_format", *format); err != nil {
			return fmt.Errorf("failed to write response format field: %w", err)
		}
	}

	if temperature := translationRequest.ResponseFormat; temperature != nil {
		if err := writer.WriteField("response_format", *temperature); err != nil {
			return fmt.Errorf("failed to write response format field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close body writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.translationsURL, &requestBody)
	if err != nil {
		return fmt.Errorf("failed to create translation request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}

	oir, ir := new(openai.CreateTranslationResponse), new(db.CreateTranslationResponse)
	code, err := cclient.SendRequest(a.client, req, oir)

	// err must be shadowed here.
	if err := ir.FromPublic(oir); err != nil {
		l.Error("failed to convert translation response", "err", err)
	}

	// Process the request error here.
	if err != nil {
		l.Error("failed to send translation request", "err", err)
		ir.Error = z.Pointer(err.Error())
	}

	ir.StatusCode = code
	ir.RequestID = translationRequest.ID
	ir.Done = true

	// Store the completed response and mark the request as done.
	if err = gdb.Transaction(func(tx *gorm.DB) error {
		if err = db.Create(tx, ir); err != nil {
			return err
		}
		return tx.Model(translationRequest).Where("id = ?", translationRequest.ID).Update("done", true).Error
	}); err != nil {
		l.Error("failed to store translation response", "err", err)
	}

	return nil
}
