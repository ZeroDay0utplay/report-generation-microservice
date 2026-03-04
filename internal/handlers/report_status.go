package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"pdf-html-service/internal/jobstore"
)

type ReportStatusHandler struct {
	logger *slog.Logger
	store  jobstore.Store
}

func NewReportStatusHandler(logger *slog.Logger, store jobstore.Store) *ReportStatusHandler {
	return &ReportStatusHandler{logger: logger, store: store}
}

func (h *ReportStatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	job, err := h.store.GetJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, jobstore.ErrNotFound) {
			writeError(w, r, http.StatusNotFound, "JOB_NOT_FOUND", "job not found", nil)
			return
		}
		h.logger.Error("failed to get job",
			slog.String("jobId", jobID),
			slog.String("error", err.Error()),
		)
		writeError(w, r, http.StatusInternalServerError, "STORE_ERROR", "failed to retrieve job", nil)
		return
	}

	writeJSON(w, http.StatusOK, job)
}
