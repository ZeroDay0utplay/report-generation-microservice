package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"pdf-html-service/internal/models"
	"pdf-html-service/internal/util"
)

type contextKey string

const requestIDKey contextKey = "request_id"

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = r.Header.Get("X-Request-ID")
		}
		if id == "" {
			id = util.NewRequestID()
		}
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
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
					id := RequestIDFromContext(r.Context())
					logger.Error("panic recovered",
						slog.Any("panic", rec),
						slog.String("requestId", id),
						slog.String("method", r.Method),
						slog.String("route", r.URL.Path),
					)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(models.ErrorResponse{
						RequestID: id,
						Error:     models.APIError{Code: "INTERNAL_ERROR", Message: "internal server error"},
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
				slog.Int("bytes_sent", sw.bytes),
				slog.Int64("content_length", r.ContentLength),
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

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func RateLimit(rps rate.Limit, burst int, logger *slog.Logger) func(http.Handler) http.Handler {
	var mu sync.Mutex
	visitors := make(map[string]*visitor)

	go func() {
		for {
			time.Sleep(2 * time.Minute)
			mu.Lock()
			for ip, v := range visitors {
				if time.Since(v.lastSeen) > 5*time.Minute {
					delete(visitors, ip)
				}
			}
			mu.Unlock()
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}

			mu.Lock()
			v, ok := visitors[ip]
			if !ok {
				v = &visitor{limiter: rate.NewLimiter(rps, burst)}
				visitors[ip] = v
			}
			v.lastSeen = time.Now()
			limiter := v.limiter
			mu.Unlock()

			if !limiter.Allow() {
				logger.Warn("rate limit exceeded",
					slog.String("ip", ip),
					slog.String("method", r.Method),
					slog.String("route", r.URL.Path),
					slog.String("requestId", RequestIDFromContext(r.Context())),
				)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]string{
						"code":    "RATE_LIMIT_EXCEEDED",
						"message": "too many requests",
					},
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
