package server

import (
	"fmt"
	"log/slog"
	"net/http"
)

func LogRequest(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info("Handling request", "method", r.Method, "url", r.URL)
		next.ServeHTTP(w, r)
	})
}

func SetContentType(ct string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ct)
		next.ServeHTTP(w, r)
	})
}

func RequireContentType(ct string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != ct {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "header Content-Type has unexpected value %q"}`, r.Header.Get("Content-Type"))))
			return
		}
		next.ServeHTTP(w, r)
	})
}
