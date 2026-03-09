package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-playground/validator/v10"

	"pdf-html-service/internal/models"
	"pdf-html-service/internal/pdfjobs"
	"pdf-html-service/internal/security"
)

type PDFSubmitService interface {
	Submit(ctx context.Context, rawPayload []byte) (pdfjobs.SubmitResult, error)
}

type PDFHandler struct {
	baseHandler
	service PDFSubmitService
}

func NewPDFHandler(
	logger *slog.Logger,
	validate *validator.Validate,
	urlPolicy *security.URLPolicy,
	service PDFSubmitService,
	maxPairs int,
) *PDFHandler {
	return &PDFHandler{
		baseHandler: baseHandler{logger: logger, validate: validate, urlPolicy: urlPolicy, maxPairs: maxPairs},
		service:     service,
	}
}

func (h *PDFHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	baseLogAttrs := []any{
		slog.String("requestId", requestID(r)),
		slog.String("route", r.URL.Path),
	}

	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		h.logger.Warn("pdf request rejected", append(baseLogAttrs, slog.String("errorCode", "INVALID_JSON"))...)
		writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "request body is required", nil)
		return
	}

	_, _, ok := h.validateReportPayload(w, r, body)
	if !ok {
		return
	}

	result, err := h.service.Submit(r.Context(), body)
	if err != nil {
		switch {
		case errors.Is(err, pdfjobs.ErrQueueFull):
			writeError(w, r, http.StatusServiceUnavailable, "QUEUE_FULL", "pdf queue is full, retry later", nil)
		default:
			h.logger.Error("pdf submit failed", append(baseLogAttrs, slog.String("error", err.Error()))...)
			writeError(w, r, http.StatusInternalServerError, "PDF_JOB_ERROR", "failed to submit PDF job", nil)
		}
		return
	}

	switch result.Status {
	case "completed":
		writeJSON(w, http.StatusOK, models.PDFJobResponse{
			RequestID: requestID(r),
			JobID:     result.JobID,
			Status:    result.Status,
			PDFURL:    result.PDFURL,
		})
	case "failed":
		code := result.ErrorCode
		if code == "" {
			code = "PDF_JOB_FAILED"
		}
		msg := result.ErrorMessage
		if msg == "" {
			msg = "pdf generation failed"
		}
		writeError(w, r, http.StatusInternalServerError, code, msg, map[string]any{"jobId": result.JobID})
	default:
		writeJSON(w, http.StatusAccepted, models.PDFJobResponse{
			RequestID: requestID(r),
			JobID:     result.JobID,
			Status:    "processing",
		})
	}
}
