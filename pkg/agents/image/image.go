package image

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	"github.com/acorn-io/z"
	cclient "github.com/gptscript-ai/clicky-chats/pkg/client"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/gorm"
)

const (
	minPollingInterval  = time.Second
	minRequestRetention = 5 * time.Minute
)

func Start(ctx context.Context, gdb *db.DB, cfg Config) error {
	a, err := newAgent(gdb, cfg)
	if err != nil {
		return err
	}

	a.Start(ctx)

	return nil
}

type Config struct {
	PollingInterval, RetentionPeriod time.Duration
	ImagesBaseURL, APIKey, AgentID   string
}

type agent struct {
	pollingInterval, requestRetention time.Duration
	id, apiKey                        string
	generationsURL, editsURL          string
	client                            *http.Client
	db                                *db.DB
}

func newAgent(db *db.DB, cfg Config) (*agent, error) {
	if cfg.PollingInterval < minPollingInterval {
		return nil, fmt.Errorf("polling interval must be at least %s", minPollingInterval)
	}
	if cfg.RetentionPeriod < minRequestRetention {
		return nil, fmt.Errorf("request retention must be at least %s", minRequestRetention)
	}

	return &agent{
		pollingInterval:  cfg.PollingInterval,
		requestRetention: cfg.RetentionPeriod,
		generationsURL:   cfg.ImagesBaseURL + "/generations",
		editsURL:         cfg.ImagesBaseURL + "/edits",
		client:           http.DefaultClient,
		apiKey:           cfg.APIKey,
		db:               db,
		id:               cfg.AgentID,
	}, nil
}

func (a *agent) Start(ctx context.Context) {
	// Start the "job runner"
	for _, run := range []func(context.Context) error{
		a.runGenerations,
		a.runEdits,
	} {
		r := run
		go func() {
			for {
				if err := r(ctx); err != nil {
					if !errors.Is(err, gorm.ErrRecordNotFound) {
						slog.Error("failed run iteration", "err", err)
					}

					select {
					case <-ctx.Done():
						return
					case <-time.After(a.pollingInterval):
					}
				}
			}
		}()

	}

	// Start cleanup
	go func() {
		var (
			cleanupInterval = a.requestRetention / 2
			jobObjects      = []db.Storer{
				new(db.CreateImageRequest),
				new(db.CreateImageEditRequest),
				new(db.ImagesResponse),
			}
			cdb = a.db.WithContext(ctx)
		)
		for {
			slog.Debug("Looking for expired create image requests and responses")
			expiration := time.Now().Add(-a.requestRetention)
			if err := db.DeleteExpired(cdb, expiration, jobObjects...); err != nil {
				slog.Error("failed to delete expired create image requests", "err", err)
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(cleanupInterval):
			}
		}
	}()
}

func (a *agent) runGenerations(ctx context.Context) error {
	slog.Debug("Checking for an image request to process")
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
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}

	oir, ir := new(openai.ImagesResponse), new(db.ImagesResponse)
	code, err := cclient.SendRequest(a.client, req, oir)
	if err := ir.FromPublic(oir); err != nil {
		l.Error("Failed to create image", "err", err)
	}

	// Process the request error here.
	if err != nil {
		l.Error("Failed to create image", "err", err)
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
		l.Error("Failed to create images response", "err", err)
	}

	return nil
}

func (a *agent) runEdits(ctx context.Context) error {
	slog.Debug("Checking for an image request to process")
	// Look for a new create image request and claim it. Also, query for the other objects we need.
	var (
		editRequest = new(db.CreateImageEditRequest)
		gdb         = a.db.WithContext(ctx)
	)
	if err := dequeue(gdb, editRequest, a.id); err != nil {
		return err
	}

	l := slog.With("type", "createimageedit", "id", editRequest.ID)
	l.Debug("Processing request")

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
		l.Error("Failed to create image", "err", err)
	}

	// Process the request error here.
	if err != nil {
		l.Error("Failed to create image edit", "err", err)
		ir.Error = z.Pointer(err.Error())
	}

	ir.StatusCode = code
	ir.RequestID = editRequest.ID
	ir.Done = true

	// Store the completed response and mark the request as done.
	if err = a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err = db.Create(tx, ir); err != nil {
			return err
		}
		return tx.Model(editRequest).Where("id = ?", editRequest.ID).Update("done", true).Error
	}); err != nil {
		l.Error("Failed to create image edit response", "err", err)
	}

	return nil
}

func dequeue(gdb *gorm.DB, request db.Storer, agentID string) error {
	err := gdb.Model(request).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("claimed_by IS NULL").Or("claimed_by = ? AND done = false", agentID).
			Order("created_at desc").
			First(request).Error; err != nil {
			return err
		}

		if err := tx.Where("id = ?", request.GetID()).
			Updates(map[string]interface{}{"claimed_by": agentID}).Error; err != nil {
			return err
		}

		return nil
	})
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		err = fmt.Errorf("failed to dequeue request %T: %w", request, err)
	}

	return err
}
