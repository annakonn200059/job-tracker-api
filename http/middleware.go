package http

import (
	"log/slog"
	"net/http"
	"time"
)

// statusRecorder captures the status code written by the wrapped handler so
// it can be included in the request log line after the fact.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// Logging wraps next with a request log line (method, path, status,
// duration) written once the request completes. WriteError already logs
// 5xx failures with the underlying error, so this is deliberately just the
// access-log line, not a duplicate of that detail.
func Logging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}
