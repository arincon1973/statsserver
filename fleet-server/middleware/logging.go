package middleware

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// responseRecorder wraps http.ResponseWriter to capture the status code and
// response body written by the downstream handler without altering the wire
// response seen by the caller.
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	body       bytes.Buffer
}

func (r *responseRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

// RequestLogger returns middleware that logs every inbound request and its
// outbound response using the provided slog.Logger.
//
// Each log entry contains:
//   - timestamp (added automatically by slog)
//   - method, path, device_id (from URL)
//   - request_body (raw JSON, empty for bodyless requests)
//   - status (HTTP response status code)
//   - response_body (raw JSON written to the caller)
//   - duration_ms (handler wall-clock time in milliseconds)
func RequestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Buffer the request body so the downstream handler can still read it.
		var reqBody []byte
		if r.Body != nil {
			var err error
			reqBody, err = io.ReadAll(r.Body)
			if err != nil {
				logger.Error("failed to read request body", "error", err)
			}
			r.Body = io.NopCloser(bytes.NewBuffer(reqBody))
		}

		rec := &responseRecorder{
			ResponseWriter: w,
			statusCode:     http.StatusOK, // default; overridden by WriteHeader
		}

		next.ServeHTTP(rec, r)

		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"device_id", r.PathValue("device_id"),
			"request_body", string(reqBody),
			"status", rec.statusCode,
			"response_body", rec.body.String(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}
