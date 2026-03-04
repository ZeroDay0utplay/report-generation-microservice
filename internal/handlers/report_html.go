package handlers

import (
	"compress/gzip"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"pdf-html-service/internal/jobstore"
	"pdf-html-service/internal/middleware"
	"pdf-html-service/internal/render"
)

type ReportHTMLHandler struct {
	logger  *slog.Logger
	store   jobstore.Store
	logoURL string
}

func NewReportHTMLHandler(logger *slog.Logger, store jobstore.Store, logoURL string) *ReportHTMLHandler {
	return &ReportHTMLHandler{logger: logger, store: store, logoURL: logoURL}
}

func (h *ReportHTMLHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	reqID := middleware.RequestIDFromContext(r.Context())

	payload, err := h.store.GetPayload(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, jobstore.ErrNotFound) {
			h.logger.Warn("html render: job not found",
				slog.String("requestId", reqID),
				slog.String("jobId", jobID),
			)
		} else {
			h.logger.Error("html render: store error",
				slog.String("requestId", reqID),
				slog.String("jobId", jobID),
				slog.String("error", err.Error()),
			)
		}
		http.Error(w, `{"error":{"code":"JOB_NOT_FOUND","message":"job not found"}}`, http.StatusNotFound)
		return
	}

	etag := `"` + jobID + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=3600")

	if r.Header.Get("If-None-Match") == etag {
		h.logger.Debug("html render: cache hit",
			slog.String("requestId", reqID),
			slog.String("jobId", jobID),
		)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; img-src * data:; script-src 'unsafe-inline'; frame-ancestors 'none';",
	)

	acceptsGzip := strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
	start := time.Now()

	if acceptsGzip {
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		if err := render.RenderHTMLTo(r.Context(), gz, payload, h.logoURL); err != nil {
			h.logger.Error("html render: failed",
				slog.String("requestId", reqID),
				slog.String("jobId", jobID),
				slog.Bool("gzip", true),
				slog.String("error", err.Error()),
			)
			return
		}
		h.logger.Info("html render: ok",
			slog.String("requestId", reqID),
			slog.String("jobId", jobID),
			slog.Bool("gzip", true),
			slog.Int64("render_ms", time.Since(start).Milliseconds()),
		)
		return
	}

	if err := render.RenderHTMLTo(r.Context(), w, payload, h.logoURL); err != nil {
		h.logger.Error("html render: failed",
			slog.String("requestId", reqID),
			slog.String("jobId", jobID),
			slog.Bool("gzip", false),
			slog.String("error", err.Error()),
		)
		return
	}
	h.logger.Info("html render: ok",
		slog.String("requestId", reqID),
		slog.String("jobId", jobID),
		slog.Bool("gzip", false),
		slog.Int64("render_ms", time.Since(start).Milliseconds()),
	)
}
