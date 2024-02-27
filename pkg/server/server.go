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

	mux := http.NewServeMux()

	openai.HandlerWithOptions(s, openai.StdHTTPServerOptions{
		BaseURL:     config.APIBase,
		BaseRouter:  mux,
		Middlewares: []openai.MiddlewareFunc{
			// The OpenAPI spec has some errors in it. It would be great if we could use the spec to validate requests.
			//nethttpmiddleware.OapiRequestValidatorWithOptions(swagger, &nethttpmiddleware.Options{
			//	SilenceServersWarning: true,
			//	Options: openapi3filter.Options{
			//		AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
			//	},
			//}),
		},
	})

	server := http.Server{
		Addr: ":" + config.Port,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
		Handler: LogRequest(slog.Default(), SetContentType("application/json", RequireContentType("application/json", mux))),
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
