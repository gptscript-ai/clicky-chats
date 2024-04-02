package server

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	kb "github.com/gptscript-ai/clicky-chats/pkg/knowledgebases"
	"github.com/gptscript-ai/clicky-chats/pkg/trigger"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"
)

//go:embed openapi.yaml
var openapiSpec embed.FS

type Triggers struct {
	ChatCompletion, Run, RunStep, RunTool, Image, Embeddings, Audio trigger.Trigger
}

func (t *Triggers) Complete() {
	if t.ChatCompletion == nil {
		t.ChatCompletion = trigger.NewNoop()
	}
	if t.Run == nil {
		t.Run = trigger.NewNoop()
	}
	if t.RunStep == nil {
		t.RunStep = trigger.NewNoop()
	}
	if t.RunTool == nil {
		t.RunTool = trigger.NewNoop()
	}
	if t.Image == nil {
		t.Image = trigger.NewNoop()
	}
	if t.Embeddings == nil {
		t.Embeddings = trigger.NewNoop()
	}
	if t.Audio == nil {
		t.Audio = trigger.NewNoop()
	}
}

type Config struct {
	ServerURL, Port, APIBase string
	Triggers                 *Triggers
}

type Server struct {
	db       *db.DB
	kbm      *kb.KnowledgeBaseManager
	triggers *Triggers
}

func NewServer(db *db.DB, kbm *kb.KnowledgeBaseManager) *Server {
	return &Server{
		db:  db,
		kbm: kbm,
	}
}

func (s *Server) Start(ctx context.Context, wg *sync.WaitGroup, config Config) error {
	// Setup triggers
	config.Triggers.Complete()
	s.triggers = config.Triggers

	// Treat image/png as files during decoding.
	// This is required to pass body validation for image and mask fields for the following endpoints:
	// - /v1/images/edits
	openapi3filter.RegisterBodyDecoder("image/png", openapi3filter.FileBodyDecoder)
	openapi3filter.RegisterBodyDecoder("text/plain", plainBodyDecoder)

	if err := s.db.AutoMigrate(); err != nil {
		return err
	}

	swagger, err := openai.GetSwagger()
	if err != nil {
		return err
	}

	swagger.Servers = openapi3.Servers{&openapi3.Server{URL: fmt.Sprintf("%s:%s%s", config.ServerURL, config.Port, config.APIBase)}}

	mux := http.DefaultServeMux
	mux.HandleFunc("GET /healthz", s.db.Check)
	mux.Handle("/v1/openapi.yaml", http.StripPrefix("/v1/", http.FileServerFS(openapiSpec)))

	h := openai.HandlerWithOptions(s, openai.StdHTTPServerOptions{
		BaseURL:    config.APIBase,
		BaseRouter: mux,
		Middlewares: []openai.MiddlewareFunc{
			nethttpmiddleware.OapiRequestValidatorWithOptions(swagger, &nethttpmiddleware.Options{
				SilenceServersWarning: true,
				Options: openapi3filter.Options{
					AuthenticationFunc:  openapi3filter.NoopAuthenticationFunc,
					SkipSettingDefaults: true,
				},
			}),
			LogRequest(slog.Default()),
			SetContentType("application/json"),
		},
	})

	server := http.Server{
		Addr:    ":" + config.Port,
		Handler: h,
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		slog.Info("Starting server", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server failed", "err", err)
		}
	}()

	wg.Add(1)
	context.AfterFunc(ctx, func() {
		defer wg.Done()
		timeoutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(timeoutCtx); err != nil {
			slog.Error("Server shutdown failed", "err", err)
		}
		slog.Info("Server shutdown")
	})

	return nil
}
