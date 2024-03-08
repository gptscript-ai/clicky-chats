package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/extendedapi"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
)

type Config struct {
	ServerURL, Port, APIBase string
}

type Server struct {
	db *db.DB
}

func NewServer(db *db.DB) *Server {
	return &Server{
		db: db,
	}
}

func (s *Server) Start(ctx context.Context, config Config) error {
	if err := s.db.AutoMigrate(); err != nil {
		return err
	}

	swagger, err := openai.GetSwagger()
	if err != nil {
		return err
	}

	swagger.Servers = openapi3.Servers{&openapi3.Server{URL: fmt.Sprintf("%s:%s%s", config.ServerURL, config.Port, config.APIBase)}}

	// The file_ids field is not required for CreateMessageRequest, but the OpenAPI spec has minItems of 1. This doesn't make sense.
	swagger.Components.Schemas["CreateMessageRequest"].Value.Properties["file_ids"].Value.MinItems = 0
	// There is not "thread_id" field for a run, it is taken from the paths.
	swagger.Components.Schemas["CreateRunRequest"].Value.Required = []string{"assistant_id"}
	// Tools is nullable in the CreateChatCompletionRequest
	swagger.Components.Schemas["CreateChatCompletionRequest"].Value.Properties["tools"].Value.Nullable = true

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.db.Check)

	// This is a modification of http.StripPrefix that pulls out "/rubra"
	// This will serve the OpenAI endpoints for those that we don't extend.
	mux.HandleFunc(config.APIBase+"/rubra/", func(w http.ResponseWriter, r *http.Request) {
		prefix := config.APIBase + "/rubra"
		p := strings.TrimPrefix(r.URL.Path, prefix)
		rp := strings.TrimPrefix(r.URL.RawPath, prefix)
		if len(p) < len(r.URL.Path) && (r.URL.RawPath == "" || len(rp) < len(r.URL.RawPath)) {
			r2 := r.Clone(extendedapi.NewExtendedContext(r.Context()))
			r2.URL = new(url.URL)
			*r2.URL = *r.URL
			r2.URL.Path = config.APIBase + p
			r2.URL.RawPath = config.APIBase + rp
			mux.ServeHTTP(w, r2)
		} else {
			http.NotFound(w, r)
		}
	})

	h := openai.HandlerWithOptions(s, openai.StdHTTPServerOptions{
		BaseURL:     config.APIBase,
		BaseRouter:  mux,
		Middlewares: []openai.MiddlewareFunc{openai.MiddlewareFunc(OpenAPIValidator(swagger))},
	})

	server := http.Server{
		Addr: ":" + config.Port,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
		Handler: ApplyMiddlewares(h, LogRequest(slog.Default()), SetContentType("application/json")),
	}

	go func() {
		slog.Info("Starting server", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server failed", "err", err)
		}
	}()

	go func() {

		<-ctx.Done()

		timeoutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(timeoutCtx); err != nil {
			slog.Error("Server shutdown failed", "err", err)
		}
	}()

	return nil
}
