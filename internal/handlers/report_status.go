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
	return &ReportStatusHandler{
		logger: logger,
		store:  store,
	}
}

func (h *ReportStatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	if jobID == "" {
		writeError(w, r, http.StatusBadRequest, "INVALID_JOB_ID", "job id is required", nil)
		return
	}

	job, err := h.store.GetJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, jobstore.ErrNotFound) {
			writeError(w, r, http.StatusNotFound, "JOB_NOT_FOUND", "job not found", nil)
			return
		}
		h.logger.Error("report status: failed to read job",
			slog.String("requestId", requestID(r)),
			slog.String("jobId", jobID),
			slog.String("error", err.Error()),
		)
		writeError(w, r, http.StatusInternalServerError, "STORE_ERROR", "failed to fetch job status", nil)
		return
	}

	url := job.PDFURL
	if url == "" {
		url = job.HTMLURL
	}

	resp := models.ReportStatusResponse{
		RequestID: requestID(r),
		JobID:     job.ID,
		Status:    job.Status,
		HTMLURL:   job.HTMLURL,
		PDFURL:    job.PDFURL,
		URL:       url,
	}
	if job.Status == statusFailed {
		code := job.ErrorCode
		if code == "" {
			code = "PDF_PIPELINE_ERROR"
		}
		message := job.ErrorMsg
		if message == "" {
			message = "report generation failed"
		}
		resp.Error = &models.APIError{
			Code:    code,
			Message: message,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}
