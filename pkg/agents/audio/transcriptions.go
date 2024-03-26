package audio

import (
	"bytes"
	"context"
	"encoding/json"
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

func (a *agent) runTranscriptions(ctx context.Context) error {
	slog.Debug("checking for an transcription request to process")
	var (
		transcriptionRequest = new(db.CreateTranscriptionRequest)
		gdb                  = a.db.WithContext(ctx)
	)
	if err := db.Dequeue(gdb, transcriptionRequest, a.id); err != nil {
		return err
	}

	l := slog.With("type", "transcription", "id", transcriptionRequest.ID)
	l.Debug("processing request")

	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	publicRequest := transcriptionRequest.ToPublic().(*openai.CreateTranscriptionRequest)
	part, err := writer.CreateFormFile("file", publicRequest.File.Filename())
	if err != nil {
		return fmt.Errorf("failed to create form file: %w", err)
	}
	r, err := publicRequest.File.Reader()
	if err != nil {
		return fmt.Errorf("failed to get transcription reader: %w", err)
	}
	if _, err := io.Copy(part, r); err != nil {
		return fmt.Errorf("failed to copy file to form file: %w", err)
	}

	if language := transcriptionRequest.Language; language != nil {
		if err := writer.WriteField("language", *language); err != nil {
			return fmt.Errorf("failed to write language field: %w", err)
		}
	}

	if err := writer.WriteField("model", transcriptionRequest.Model); err != nil {
		return fmt.Errorf("failed to write model field: %w", err)
	}

	if prompt := transcriptionRequest.Prompt; prompt != nil {
		if err := writer.WriteField("prompt", *prompt); err != nil {
			return fmt.Errorf("failed to write prompt field: %w", err)
		}
	}

	if format := transcriptionRequest.ResponseFormat; format != nil {
		if err := writer.WriteField("response_format", *format); err != nil {
			return fmt.Errorf("failed to write response format field: %w", err)
		}
	}

	if temperature := transcriptionRequest.Temperature; temperature != nil {
		if err := writer.WriteField("temperature", fmt.Sprintf("%f", *temperature)); err != nil {
			return fmt.Errorf("failed to write response format field: %w", err)
		}
	}

	if granularities := transcriptionRequest.TimestampGranularities; granularities != nil {
		data, err := json.Marshal(granularities)
		if err != nil {
			return fmt.Errorf("failed to marshal timestamp granularities: %w", err)
		}

		if err := writer.WriteField("timestamp_granularities", string(data)); err != nil {
			return fmt.Errorf("failed to write timestamp granularities field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close body writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.transcriptionsURL, &requestBody)
	if err != nil {
		return fmt.Errorf("failed to create transcription request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}

	oir, ir := new(openai.CreateTranscriptionResponse), new(db.CreateTranscriptionResponse)
	code, err := cclient.SendRequest(a.client, req, oir)

	// err must be shadowed here.
	if err := ir.FromPublic(oir); err != nil {
		l.Error("failed to convert transcription response", "err", err)
	}

	// Process the request error here.
	if err != nil {
		l.Error("failed to send transcription request", "err", err)
		ir.Error = z.Pointer(err.Error())
	}

	ir.StatusCode = code
	ir.RequestID = transcriptionRequest.ID
	ir.Done = true

	// Store the completed response and mark the request as done.
	if err = gdb.Transaction(func(tx *gorm.DB) error {
		if err = db.Create(tx, ir); err != nil {
			return err
		}
		return tx.Model(transcriptionRequest).Where("id = ?", transcriptionRequest.ID).Update("done", true).Error
	}); err != nil {
		l.Error("failed to store transcription response", "err", err)
	}

	a.trigger.Ready(transcriptionRequest.ID)

	return nil
}
