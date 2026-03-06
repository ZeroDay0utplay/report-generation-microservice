package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"

	"pdf-html-service/internal/jobstore"
	"pdf-html-service/internal/models"
	"pdf-html-service/internal/notify"
)

type ReportNotifyHandler struct {
	logger   *slog.Logger
	validate *validator.Validate
	store    jobstore.Store
	mailer   notify.Sender
}

func NewReportNotifyHandler(
	logger *slog.Logger,
	validate *validator.Validate,
	store jobstore.Store,
	mailer notify.Sender,
) *ReportNotifyHandler {
	return &ReportNotifyHandler{
		logger:   logger,
		validate: validate,
		store:    store,
		mailer:   mailer,
	}
}

func (h *ReportNotifyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	if jobID == "" {
		writeError(w, r, http.StatusBadRequest, "MISSING_JOB_ID", "jobID is required", nil)
		return
	}

	var req models.NotifyRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}

	if err := h.validate.Struct(req); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR",
				"invalid email addresses", validationDetails(ve))
			return
		}
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request", nil)
		return
	}

	emails := normalizeEmails(req.Emails)
	if len(emails) == 0 {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "at least one valid email is required", nil)
		return
	}

	job, err := h.store.GetJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, jobstore.ErrNotFound) {
			writeError(w, r, http.StatusNotFound, "NOT_FOUND", "job not found", nil)
			return
		}
		h.logger.Error("notify: store error",
			slog.String("requestId", requestID(r)),
			slog.String("jobId", jobID),
			slog.String("error", err.Error()),
		)
		writeError(w, r, http.StatusInternalServerError, "STORE_ERROR", "failed to read job", nil)
		return
	}

	switch job.Status {
	case "done":
		if h.mailer != nil && h.mailer.Enabled() {
			if err := h.mailer.SendPDFReady(r.Context(), jobID, job.PDFURL, emails); err != nil {
				h.logger.Error("notify: send failed",
					slog.String("requestId", requestID(r)),
					slog.String("jobId", jobID),
					slog.String("error", err.Error()),
				)
				writeError(w, r, http.StatusInternalServerError, "EMAIL_ERROR", "failed to send emails", nil)
				return
			}
		}
		writeJSON(w, http.StatusOK, models.NotifyResponse{
			RequestID:  requestID(r),
			JobID:      jobID,
			Status:     "sent",
			Recipients: emails,
		})

	case "queued", "generating":
		if err := h.store.AppendEmails(r.Context(), jobID, emails); err != nil {
			h.logger.Error("notify: failed to persist emails",
				slog.String("requestId", requestID(r)),
				slog.String("jobId", jobID),
				slog.String("error", err.Error()),
			)
			writeError(w, r, http.StatusInternalServerError, "STORE_ERROR", "failed to save email list", nil)
			return
		}
		writeJSON(w, http.StatusAccepted, models.NotifyResponse{
			RequestID:  requestID(r),
			JobID:      jobID,
			Status:     "queued",
			Recipients: emails,
			Message:    "Emails will be sent when the report is ready.",
		})

	case "failed":
		writeJSON(w, http.StatusUnprocessableEntity, models.ErrorResponse{
			RequestID: requestID(r),
			Error: models.APIError{
				Code:    "GENERATION_FAILED",
				Message: "report generation failed: " + job.Error,
			},
		})
	}
}
