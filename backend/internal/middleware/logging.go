package middleware

import (
	"net/http"
	"time"

	"go.uber.org/zap"
)

// Logging wraps an HTTP handler with structured request logging.
func Logging(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(wrapped, r)

			// request_id ties this line to every other entry for the same
			// request, and to the X-Request-Id a user can read off a failed
			// response. Without it, correlating anything in a concurrent
			// server means guessing from timestamps.
			logger.Info("request completed",
				zap.String("request_id", RequestIDFromContext(r.Context())),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", wrapped.statusCode),
				zap.Duration("duration", time.Since(start)),
				zap.String("remote_addr", r.RemoteAddr),
			)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush lets Connect server streams (StreamPodLogs) push each line to the
// client immediately. Without this, wrapping ResponseWriter hides the
// underlying http.Flusher and Connect aborts with
// "*middleware.responseWriter does not implement http.Flusher".
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap exposes the inner writer so http.ResponseController can still find
// optional interfaces (Flusher, Hijacker, …) through the wrapper.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}
