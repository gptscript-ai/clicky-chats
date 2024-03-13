package image

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strconv"

	"github.com/acorn-io/z"
	cclient "github.com/gptscript-ai/clicky-chats/pkg/client"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/gorm"
)

func (a *agent) runEdits(ctx context.Context) error {
	slog.Debug("checking for an image edit request to process")
	var (
		editRequest = new(db.CreateImageEditRequest)
		gdb         = a.db.WithContext(ctx)
	)
	if err := db.Dequeue(gdb, editRequest, a.id); err != nil {
		return err
	}

	l := slog.With("type", "imageedit", "id", editRequest.ID)
	l.Debug("Processing image edit request")

	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	publicRequest := editRequest.ToPublic().(*openai.CreateImageEditRequest)
	part, err := writer.CreateFormFile("image", publicRequest.Image.Filename())
	if err != nil {
		return fmt.Errorf("failed to create form file: %w", err)
	}
	r, err := publicRequest.Image.Reader()
	if err != nil {
		return fmt.Errorf("failed to get image reader: %w", err)
	}
	if _, err := io.Copy(part, r); err != nil {
		return fmt.Errorf("failed to copy image to form file: %w", err)
	}

	if mask := publicRequest.Mask; mask != nil {
		part, err := writer.CreateFormFile("mask", mask.Filename())
		if err != nil {
			return fmt.Errorf("failed to create form file: %w", err)
		}
		r, err := mask.Reader()
		if err != nil {
			return fmt.Errorf("failed to get mask reader: %w", err)
		}
		if _, err := io.Copy(part, r); err != nil {
			return fmt.Errorf("failed to copy mask to form file: %w", err)
		}
	}

	if model := editRequest.Model; model != nil {
		if err := writer.WriteField("model", *model); err != nil {
			return fmt.Errorf("failed to write model field: %w", err)
		}
	}

	if n := editRequest.N; n != nil {
		if err := writer.WriteField("n", strconv.Itoa(*n)); err != nil {
			return fmt.Errorf("failed to write n field: %w", err)
		}
	}

	if err := writer.WriteField("prompt", editRequest.Prompt); err != nil {
		return fmt.Errorf("failed to write prompt field: %w", err)
	}

	if format := editRequest.ResponseFormat; format != nil {
		if err := writer.WriteField("response_format", z.Dereference(format)); err != nil {
			return fmt.Errorf("failed to write response format field: %w", err)
		}
	}

	if size := editRequest.Size; size != nil {
		if err := writer.WriteField("size", *size); err != nil {
			return fmt.Errorf("failed to write size field: %w", err)
		}
	}

	if user := editRequest.User; user != nil {
		if err := writer.WriteField("user", *user); err != nil {
			return fmt.Errorf("failed to write user field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close body writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.editsURL, &requestBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
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
		l.Error("failed to send image edit request", "err", err)
		ir.Error = z.Pointer(err.Error())
	}

	ir.StatusCode = code
	ir.RequestID = editRequest.ID
	ir.Done = true

	// Store the completed response and mark the request as done.
	if err = gdb.Transaction(func(tx *gorm.DB) error {
		if err = db.Create(tx, ir); err != nil {
			return err
		}
		return tx.Model(editRequest).Where("id = ?", editRequest.ID).Update("done", true).Error
	}); err != nil {
		l.Error("failed to store image edit response", "err", err)
	}

	return nil
}
