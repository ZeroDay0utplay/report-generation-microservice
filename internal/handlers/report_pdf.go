package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"pdf-html-service/internal/gotenberg"
	"pdf-html-service/internal/jobstore"
	"pdf-html-service/internal/middleware"
	"pdf-html-service/internal/models"
	"pdf-html-service/internal/render"
)

// PDFStreamRenderer is the narrow interface required by ReportPDFHandler.
// It is satisfied by *gotenberg.Client.
type PDFStreamRenderer interface {
	ConvertHTMLReaderToPDF(ctx context.Context, r io.Reader) (io.ReadCloser, error)
}

type ReportPDFHandler struct {
	logger   *slog.Logger
	store    jobstore.Store
	renderer PDFStreamRenderer
	logoURL  string
}

func NewReportPDFHandler(
	logger *slog.Logger,
	store jobstore.Store,
	renderer PDFStreamRenderer,
	logoURL string,
) *ReportPDFHandler {
	return &ReportPDFHandler{
		logger:   logger,
		store:    store,
		renderer: renderer,
		logoURL:  logoURL,
	}
}

func (h *ReportPDFHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	reqID := middleware.RequestIDFromContext(r.Context())
	jobID := chi.URLParam(r, "id")

	job, err := h.store.GetJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, jobstore.ErrNotFound) {
			h.logger.Warn("pdf render: job not found",
				slog.String("requestId", reqID),
				slog.String("jobId", jobID),
			)
		} else {
			h.logger.Error("pdf render: store error",
				slog.String("requestId", reqID),
				slog.String("jobId", jobID),
				slog.String("error", err.Error()),
			)
		}
		writeError(w, r, http.StatusNotFound, "JOB_NOT_FOUND", "job not found", nil)
		return
	}
	if len(job.Payload) == 0 {
		writeError(w, r, http.StatusNotFound, "PAYLOAD_NOT_FOUND", "report payload not available", nil)
		return
	}
	var payload models.ReportRequest
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		h.logger.Error("pdf render: failed to unmarshal payload",
			slog.String("requestId", reqID),
			slog.String("jobId", jobID),
			slog.String("error", err.Error()),
		)
		writeError(w, r, http.StatusInternalServerError, "RENDER_ERROR", "failed to parse report payload", nil)
		return
	}

	etag := `"pdf-` + jobID + `"`
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		h.logger.Debug("pdf render: cache hit",
			slog.String("requestId", reqID),
			slog.String("jobId", jobID),
		)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// Pipeline: RenderPDFHTMLTo → io.Pipe → ConvertHTMLReaderToPDF → client
	// The template engine and Gotenberg HTTP request run concurrently:
	// Chrome begins receiving HTML bytes before rendering is complete.
	htmlPR, htmlPW := io.Pipe()
	go func() {
		err := render.RenderPDFHTMLTo(r.Context(), htmlPW, payload, h.logoURL)
		htmlPW.CloseWithError(err)
	}()

	start := time.Now()
	pdfReader, err := h.renderer.ConvertHTMLReaderToPDF(r.Context(), htmlPR)
	if err != nil {
		renderErr := new(gotenberg.ConvertError)
		if errors.As(err, &renderErr) {
			h.logger.Error("pdf render: gotenberg error",
				slog.String("requestId", reqID),
				slog.String("jobId", jobID),
				slog.Int("gotenbergStatus", renderErr.Status),
				slog.String("bodySnippet", renderErr.BodySnippet),
			)
			writeError(w, r, http.StatusBadGateway, "GOTENBERG_ERROR", "PDF conversion failed",
				map[string]any{"status": renderErr.Status})
			return
		}
		h.logger.Error("pdf render: conversion failed",
			slog.String("requestId", reqID),
			slog.String("jobId", jobID),
			slog.String("error", err.Error()),
		)
		writeError(w, r, http.StatusInternalServerError, "PDF_ERROR", "PDF generation failed", nil)
		return
	}
	defer pdfReader.Close()

	filename := fmt.Sprintf("report-%s.pdf", jobID)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Cache-Control", "public, max-age=3600")

	n, copyErr := io.Copy(w, pdfReader)
	totalMS := time.Since(start).Milliseconds()

	if copyErr != nil {
		h.logger.Error("pdf render: stream to client failed",
			slog.String("requestId", reqID),
			slog.String("jobId", jobID),
			slog.String("error", copyErr.Error()),
		)
		return
	}

	h.logger.Info("pdf render: ok",
		slog.String("requestId", reqID),
		slog.String("jobId", jobID),
		slog.Int64("pdf_bytes", n),
		slog.Int64("total_ms", totalMS),
	)
}

