package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"pdf-html-service/internal/jobstore"
	"pdf-html-service/internal/models"
)

type ReportStatusHandler struct {
	logger *slog.Logger
	store  jobstore.Store
}

func NewReportStatusHandler(logger *slog.Logger, store jobstore.Store) *ReportStatusHandler {
	return &ReportStatusHandler{logger: logger, store: store}
}

func (h *ReportStatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	if jobID == "" {
		writeError(w, r, http.StatusBadRequest, "MISSING_JOB_ID", "jobID is required", nil)
		return
	}

	job, err := h.store.GetJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, jobstore.ErrNotFound) {
			writeError(w, r, http.StatusNotFound, "NOT_FOUND", "job not found", nil)
			return
		}
		h.logger.Error("report status: store error",
			slog.String("requestId", requestID(r)),
			slog.String("jobId", jobID),
			slog.String("error", err.Error()),
		)
		writeError(w, r, http.StatusInternalServerError, "STORE_ERROR", "failed to read job", nil)
		return
	}

	switch job.Status {
	case "done":
		writeJSON(w, http.StatusOK, models.ReportResponse{
			RequestID: requestID(r),
			JobID:     job.ID,
			Status:    "done",
			URL:       job.PDFURL,
		})
	case "failed":
		writeJSON(w, http.StatusUnprocessableEntity, models.ErrorResponse{
			RequestID: requestID(r),
			Error: models.APIError{
				Code:    "GENERATION_FAILED",
				Message: job.Error,
			},
		})
	default:
		writeJSON(w, http.StatusAccepted, models.ReportResponse{
			RequestID: requestID(r),
			JobID:     job.ID,
			Status:    job.Status,
		})
	}
}
