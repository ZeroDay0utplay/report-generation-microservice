package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"pdf-html-service/internal/jobstore"
	"pdf-html-service/internal/models"
)

type PDFStatusHandler struct {
	logger *slog.Logger
	store  jobstore.Store
}

func NewPDFStatusHandler(logger *slog.Logger, store jobstore.Store) *PDFStatusHandler {
	return &PDFStatusHandler{logger: logger, store: store}
}

func (h *PDFStatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	if jobID == "" {
		writeError(w, r, http.StatusBadRequest, "INVALID_PARAM", "jobId is required", nil)
		return
	}

	job, err := h.store.GetJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, jobstore.ErrNotFound) {
			writeError(w, r, http.StatusNotFound, "JOB_NOT_FOUND", "job not found", nil)
			return
		}
		h.logger.Error("failed to get job",
			slog.String("requestId", requestID(r)),
			slog.String("jobId", jobID),
			slog.String("error", err.Error()),
		)
		writeError(w, r, http.StatusInternalServerError, "STORE_ERROR", "failed to retrieve job", nil)
		return
	}

	writeJSON(w, http.StatusOK, models.PDFStatusResponse{
		RequestID:   requestID(r),
		JobID:       job.ID,
		Status:      job.Status,
		DownloadURL: job.PDFURL,
		Error:       job.Error,
	})
}
