package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"pdf-html-service/internal/models"
	"pdf-html-service/internal/util"
)

type contextKey string

const requestIDKey contextKey = "request_id"

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-Id")
		if requestID == "" {
			requestID = r.Header.Get("X-Request-ID")
		}
		if requestID == "" {
			requestID = util.NewRequestID()
		}

		w.Header().Set("X-Request-Id", requestID)
		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	if v == "" {
		return util.NewRequestID()
	}
	return v
}

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none';")
		next.ServeHTTP(w, r)
	})
}

func BodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					requestID := RequestIDFromContext(r.Context())
					logger.Error("panic recovered",
						slog.Any("panic", rec),
						slog.String("requestId", requestID),
						slog.String("route", r.URL.Path),
					)

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(models.ErrorResponse{
						RequestID: requestID,
						Error: models.APIError{
							Code:    "INTERNAL_ERROR",
							Message: "internal server error",
						},
					})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)

			logger.Info("request completed",
				slog.String("requestId", RequestIDFromContext(r.Context())),
				slog.String("method", r.Method),
				slog.String("route", r.URL.Path),
				slog.Int("status", sw.status),
				slog.Int("bytes", sw.bytes),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(statusCode int) {
	w.status = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}
