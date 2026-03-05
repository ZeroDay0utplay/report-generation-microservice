package handlers

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"pdf-html-service/internal/jobstore"
	"pdf-html-service/internal/models"
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

	job, err := h.store.GetJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, jobstore.ErrNotFound) {
			writeError(w, r, http.StatusNotFound, "JOB_NOT_FOUND", "job not found", nil)
			return
		}
		h.logger.Error("report html: failed to get job",
			slog.String("jobId", jobID),
			slog.String("error", err.Error()),
		)
		writeError(w, r, http.StatusInternalServerError, "STORE_ERROR", "failed to retrieve job", nil)
		return
	}

	if len(job.Payload) == 0 {
		writeError(w, r, http.StatusNotFound, "PAYLOAD_NOT_FOUND", "report payload not available", nil)
		return
	}

	var payload models.ReportRequest
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		h.logger.Error("report html: failed to unmarshal payload",
			slog.String("jobId", jobID),
			slog.String("error", err.Error()),
		)
		writeError(w, r, http.StatusInternalServerError, "RENDER_ERROR", "failed to parse report payload", nil)
		return
	}

	etag := fmt.Sprintf(`"%x"`, sha256.Sum256(job.Payload))
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	html, err := render.RenderHTMLWithLogo(payload, h.logoURL)
	if err != nil {
		h.logger.Error("report html: render failed",
			slog.String("jobId", jobID),
			slog.String("error", err.Error()),
		)
		writeError(w, r, http.StatusInternalServerError, "RENDER_ERROR", "failed to render report", nil)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Security-Policy", "default-src * 'unsafe-inline' 'unsafe-eval' data: blob:")

	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		fmt.Fprint(gz, html)
		return
	}

	fmt.Fprint(w, html)
}
