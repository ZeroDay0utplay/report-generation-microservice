package handlers

import (
	"compress/gzip"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"pdf-html-service/internal/jobstore"
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

	payload, err := h.store.GetPayload(r.Context(), jobID)
	if err != nil {
		http.Error(w, `{"error":{"code":"JOB_NOT_FOUND","message":"job not found"}}`, http.StatusNotFound)
		return
	}

	etag := `"` + jobID + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=3600")

	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	acceptsGzip := strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
	if acceptsGzip {
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		if err := render.RenderHTMLTo(r.Context(), gz, payload, h.logoURL); err != nil {
			h.logger.Error("ssr render failed",
				slog.String("jobId", jobID),
				slog.String("error", err.Error()),
			)
		}
		return
	}

	if err := render.RenderHTMLTo(r.Context(), w, payload, h.logoURL); err != nil {
		h.logger.Error("ssr render failed",
			slog.String("jobId", jobID),
			slog.String("error", err.Error()),
		)
	}
}
