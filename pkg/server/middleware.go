package server

import (
	"log/slog"
	"net/http"

	"github.com/thedadams/clicky-chats/pkg/generated/openai"
)

func LogRequest(logger *slog.Logger) openai.MiddlewareFunc {
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

func SetContentType(ct string) openai.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ct)
			next.ServeHTTP(w, r)
		})
	}
}
