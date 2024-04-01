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

func (a *agent) runVariations(ctx context.Context, l *slog.Logger) error {
	l.Debug("checking for an image variation request to process")
	var (
		variationRequest = new(db.CreateImageVariationRequest)
		gdb              = a.db.WithContext(ctx)
	)
	if err := db.Dequeue(gdb, variationRequest, a.id); err != nil {
		return err
	}

	l = slog.With("type", "imagevariation", "id", variationRequest.ID)
	l.Debug("processing request")

	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	publicRequest := variationRequest.ToPublic().(*openai.CreateImageVariationRequest)
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

	if model := variationRequest.Model; model != nil {
		if err := writer.WriteField("model", *model); err != nil {
			return fmt.Errorf("failed to write model field: %w", err)
		}
	}

	if n := variationRequest.N; n != nil {
		if err := writer.WriteField("n", strconv.Itoa(*n)); err != nil {
			return fmt.Errorf("failed to write n field: %w", err)
		}
	}

	if format := variationRequest.ResponseFormat; format != nil {
		if err := writer.WriteField("response_format", z.Dereference(format)); err != nil {
			return fmt.Errorf("failed to write response format field: %w", err)
		}
	}

	if size := variationRequest.Size; size != nil {
		if err := writer.WriteField("size", *size); err != nil {
			return fmt.Errorf("failed to write size field: %w", err)
		}
	}

	if user := variationRequest.User; user != nil {
		if err := writer.WriteField("user", *user); err != nil {
			return fmt.Errorf("failed to write user field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close body writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.variationsURL, &requestBody)
	if err != nil {
		return fmt.Errorf("failed to create image variation request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}

	oir, ir := new(openai.ImagesResponse), new(db.ImagesResponse)
	code, err := cclient.SendRequest(a.client, req, oir)

	// err must be shadowed here.
	if err := ir.FromPublic(oir); err != nil {
		l.Error("failed to convert image variation response", "err", err)
	}

	// Process the request error here.
	if err != nil {
		l.Error("failed to send image variation request", "err", err)
		ir.Error = z.Pointer(err.Error())
	}

	ir.StatusCode = code
	ir.RequestID = variationRequest.ID
	ir.Done = true

	// Store the completed response and mark the request as done.
	if err = gdb.Transaction(func(tx *gorm.DB) error {
		if err = db.Create(tx, ir); err != nil {
			return err
		}
		return tx.Model(variationRequest).Where("id = ?", variationRequest.ID).Update("done", true).Error
	}); err != nil {
		l.Error("failed to store image variation response", "err", err)
	}

	a.trigger.Ready(variationRequest.ID)

	return nil
}
