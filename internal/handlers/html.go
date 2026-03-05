package handlers

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-playground/validator/v10"

	"pdf-html-service/internal/jobstore"
	"pdf-html-service/internal/models"
	"pdf-html-service/internal/security"
	"pdf-html-service/internal/util"
)

type ReportSubmitHandler struct {
	baseHandler
	store         jobstore.Store
	publicBaseURL string
}

func NewReportSubmitHandler(
	logger *slog.Logger,
	validate *validator.Validate,
	urlPolicy *security.URLPolicy,
	store jobstore.Store,
	maxPairs int,
	publicBaseURL string,
) *ReportSubmitHandler {
	return &ReportSubmitHandler{
		baseHandler:   baseHandler{logger: logger, validate: validate, urlPolicy: urlPolicy, maxPairs: maxPairs},
		store:         store,
		publicBaseURL: publicBaseURL,
	}
}

func (h *ReportSubmitHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		h.logger.Warn("report submit: empty or unreadable body",
			slog.String("requestId", requestID(r)),
			slog.String("route", r.URL.Path),
		)
		writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "request body is required", nil)
		return
	}

	_, reqAttrs, ok := h.validateReportPayload(w, r, body)
	if !ok {
		return
	}

	jobID := util.JobIDFromPayload(body)

	existing, err := h.store.GetJob(r.Context(), jobID)
	if err == nil {
		h.logger.Info("report submit: returning cached job",
			append(reqAttrs, slog.String("jobId", jobID))...,
		)
		writeJSON(w, http.StatusOK, models.ReportSubmitResponse{
			RequestID: requestID(r),
			JobID:     existing.ID,
			Status:    existing.Status,
			HTMLURL:   existing.HTMLURL,
		})
		return
	}

	htmlURL := fmt.Sprintf("%s/v1/reports/%s/html", h.publicBaseURL, jobID)
	job := jobstore.Job{
		ID:        jobID,
		Status:    "ready",
		HTMLURL:   htmlURL,
		CreatedAt: time.Now(),
		Payload:   body,
	}

	saved, saveErr := h.store.Save(r.Context(), job)
	if saveErr != nil {
		h.logger.Error("failed to save job",
			append(reqAttrs,
				slog.String("jobId", jobID),
				slog.String("error", saveErr.Error()),
			)...,
		)
		writeError(w, r, http.StatusInternalServerError, "STORE_ERROR", "failed to save job", nil)
		return
	}

	h.logger.Info("report submitted",
		append(reqAttrs, slog.String("jobId", jobID))...,
	)

	writeJSON(w, http.StatusCreated, models.ReportSubmitResponse{
		RequestID: requestID(r),
		JobID:     saved.ID,
		Status:    saved.Status,
		HTMLURL:   saved.HTMLURL,
	})
}
