package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-playground/validator/v10"

	"pdf-html-service/internal/jobstore"
	"pdf-html-service/internal/models"
	"pdf-html-service/internal/pdfjobs"
)

type PDFRecipientService interface {
	RegisterRecipients(ctx context.Context, jobID string, emails []string) (pdfjobs.RegisterRecipientsResult, error)
}

type PDFRecipientsHandler struct {
	logger   *slog.Logger
	validate *validator.Validate
	service  PDFRecipientService
}

func NewPDFRecipientsHandler(logger *slog.Logger, validate *validator.Validate, service PDFRecipientService) *PDFRecipientsHandler {
	return &PDFRecipientsHandler{logger: logger, validate: validate, service: service}
}

func (h *PDFRecipientsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req models.RegisterRecipientsRequest
	if ok := decodeJSONBody(w, r, &req); !ok {
		return
	}

	req.JobID = strings.TrimSpace(req.JobID)
	req.Emails = normalizeEmails(req.Emails)
	if err := h.validate.Struct(req); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "payload validation failed", validationDetails(ve))
			return
		}
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "payload validation failed", nil)
		return
	}

	if len(req.Emails) == 0 {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "at least one valid email is required", nil)
		return
	}

	result, err := h.service.RegisterRecipients(r.Context(), req.JobID, req.Emails)
	if err != nil {
		switch {
		case errors.Is(err, jobstore.ErrNotFound):
			writeError(w, r, http.StatusNotFound, "JOB_NOT_FOUND", "job not found", nil)
		default:
			h.logger.Error("recipient registration failed",
				slog.String("requestId", requestID(r)),
				slog.String("jobId", req.JobID),
				slog.String("error", err.Error()),
			)
			writeError(w, r, http.StatusInternalServerError, "RECIPIENT_REGISTRATION_ERROR", "failed to register recipients", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, models.RegisterRecipientsResponse{
		RequestID:        requestID(r),
		JobID:            result.JobID,
		Accepted:         result.Accepted,
		TotalRecipients:  result.TotalRecipients,
		EmailStatus:      result.EmailStatus,
		ProcessingStatus: result.ProcessingStatus,
	})
}

func normalizeEmails(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, email := range in {
		normalized := strings.ToLower(strings.TrimSpace(email))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}
