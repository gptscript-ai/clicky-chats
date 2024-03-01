package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"
	"github.com/thedadams/clicky-chats/pkg/db"
	"github.com/thedadams/clicky-chats/pkg/generated/openai"
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

func (s *Server) Run(ctx context.Context, config Config) error {
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

	openai.HandlerWithOptions(s, openai.StdHTTPServerOptions{
		BaseURL:    config.APIBase,
		BaseRouter: mux,
		Middlewares: []openai.MiddlewareFunc{
			nethttpmiddleware.OapiRequestValidatorWithOptions(swagger, &nethttpmiddleware.Options{
				SilenceServersWarning: true,
				Options: openapi3filter.Options{
					AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
				},
			}),
			SetContentType("application/json"),
			LogRequest(slog.Default()),
		},
	})

	server := http.Server{
		Addr: ":" + config.Port,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
		Handler: mux,
	}

	go func() {
		slog.Info("Starting server", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server failed", "err", err)
		}
	}()

	<-ctx.Done()

	timeoutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return server.Shutdown(timeoutCtx)
}
