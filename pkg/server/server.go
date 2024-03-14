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
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"
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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.db.Check)

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
			SetExtendedContext(config.APIBase + "/rubra"),
		},
	})

	server := http.Server{
		Addr: ":" + config.Port,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
		Handler: h,
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
