package server

import (
	"log/slog"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/gptscript-ai/clicky-chats/pkg/extendedapi"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"
)

type MiddlewareFunc func(http.Handler) http.Handler

func ApplyMiddlewares(h http.Handler, middlewares ...MiddlewareFunc) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

func LogRequest(logger *slog.Logger) MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					logger.Error("Panic", "error", err)
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error": "encountered an unexpected error"}`))
				}
			}()
			logger.Info("Handling request", "method", r.Method, "url", r.URL)
			next.ServeHTTP(w, r)
		})
	}
}

func SetContentType(ct string) MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ct)
			next.ServeHTTP(w, r)
		})
	}
}

// OpenAPIValidator middleware will validate the request against the OpenAPI spec only if the request is not using the extended API.
func OpenAPIValidator(swagger *openapi3.T) MiddlewareFunc {
	f := nethttpmiddleware.OapiRequestValidatorWithOptions(swagger, &nethttpmiddleware.Options{
		SilenceServersWarning: true,
		Options: openapi3filter.Options{
			SkipSettingDefaults: true,
			AuthenticationFunc:  openapi3filter.NoopAuthenticationFunc,
		},
	})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !extendedapi.IsExtendedAPIKey(r.Context()) {
				next = f(next)
			}
			next.ServeHTTP(w, r)
		})
	}
}
