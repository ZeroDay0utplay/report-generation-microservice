package handlers

import (
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/hibiken/asynq"

	"pdf-html-service/internal/jobstore"
	"pdf-html-service/internal/models"
	"pdf-html-service/internal/security"
	"pdf-html-service/internal/util"
	"pdf-html-service/internal/worker"
)

type PDFHandler struct {
	baseHandler
	store        jobstore.Store
	asynqClient  *asynq.Client
	outputPrefix string
	logoURL      string
}

func NewPDFHandler(
	logger *slog.Logger,
	validate *validator.Validate,
	urlPolicy *security.URLPolicy,
	store jobstore.Store,
	asynqClient *asynq.Client,
	maxPairs int,
	outputPrefix string,
	logoURL string,
) *PDFHandler {
	return &PDFHandler{
		baseHandler:  baseHandler{logger: logger, validate: validate, urlPolicy: urlPolicy, maxPairs: maxPairs},
		store:        store,
		asynqClient:  asynqClient,
		outputPrefix: outputPrefix,
		logoURL:      logoURL,
	}
}

func (h *PDFHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	baseLogAttrs := []any{
		slog.String("requestId", requestID(r)),
		slog.String("route", r.URL.Path),
	}

	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		h.logger.Warn("pdf request rejected",
			append(baseLogAttrs, slog.String("errorCode", "INVALID_JSON"))...,
		)
		writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "request body is required", nil)
		return
	}

	jobID := util.JobIDFromPayload(body)

	// Idempotency: if a job already exists for this payload, return its status.
	if existing, err := h.store.GetJob(r.Context(), jobID); err == nil {
		writeJSON(w, http.StatusAccepted, models.AsyncPDFResponse{
			RequestID: requestID(r),
			JobID:     existing.ID,
			Status:    existing.Status,
		})
		return
	}

	payload, _, ok := h.validateReportPayload(w, r, body)
	if !ok {
		return
	}

	task, err := worker.NewPDFGenerateTask(jobID, body)
	if err != nil {
		h.logger.Error("failed to build pdf task",
			append(baseLogAttrs, slog.String("jobId", jobID), slog.String("error", err.Error()))...,
		)
		writeError(w, r, http.StatusInternalServerError, "QUEUE_ERROR", "failed to enqueue job", nil)
		return
	}

	if _, err := h.asynqClient.EnqueueContext(r.Context(), task); err != nil {
		h.logger.Error("failed to enqueue pdf task",
			append(baseLogAttrs, slog.String("jobId", jobID), slog.String("error", err.Error()))...,
		)
		writeError(w, r, http.StatusInternalServerError, "QUEUE_ERROR", "failed to enqueue job", nil)
		return
	}

	if err := h.store.Update(r.Context(), jobstore.Job{
		ID:        jobID,
		Status:    "queued",
		Payload:   body,
		CreatedAt: time.Now(),
	}); err != nil {
		h.logger.Warn("failed to persist queued job",
			append(baseLogAttrs, slog.String("jobId", jobID), slog.String("error", err.Error()))...,
		)
	}

	h.logger.Info("pdf job enqueued",
		append(baseLogAttrs,
			slog.String("jobId", jobID),
			slog.Int("pairsCount", len(payload.Pairs)),
		)...,
	)

	writeJSON(w, http.StatusAccepted, models.AsyncPDFResponse{
		RequestID: requestID(r),
		JobID:     jobID,
		Status:    "queued",
	})
}
