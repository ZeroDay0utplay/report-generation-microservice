package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-playground/validator/v10"

	"pdf-html-service/internal/jobstore"
	"pdf-html-service/internal/models"
	"pdf-html-service/internal/pipeline"
	"pdf-html-service/internal/security"
	"pdf-html-service/internal/util"
)

type ReportHandler struct {
	baseHandler
	store       jobstore.Store
	pipe        *pipeline.Pipeline
	wg          *sync.WaitGroup
	syncTimeout time.Duration
}

func NewReportHandler(
	logger *slog.Logger,
	validate *validator.Validate,
	urlPolicy *security.URLPolicy,
	store jobstore.Store,
	p *pipeline.Pipeline,
	wg *sync.WaitGroup,
	maxPairs int,
	syncTimeout time.Duration,
) *ReportHandler {
	return &ReportHandler{
		baseHandler: baseHandler{logger: logger, validate: validate, urlPolicy: urlPolicy, maxPairs: maxPairs},
		store:       store,
		pipe:        p,
		wg:          wg,
		syncTimeout: syncTimeout,
	}
}

func (h *ReportHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	logAttrs := []any{
		slog.String("requestId", requestID(r)),
		slog.String("route", r.URL.Path),
	}

	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		h.logger.Warn("report: empty or unreadable body", logAttrs...)
		writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "request body is required", nil)
		return
	}

	_, _, ok := h.validateReportPayload(w, r, body)
	if !ok {
		return
	}

	jobID := util.JobIDFromPayload(body)
	logAttrs = append(logAttrs, slog.String("jobId", jobID))

	existing, err := h.store.GetJob(r.Context(), jobID)
	if err == nil {
		switch existing.Status {
		case "done":
			h.logger.Info("report: returning cached result", logAttrs...)
			writeJSON(w, http.StatusOK, models.ReportResponse{
				RequestID: requestID(r),
				JobID:     existing.ID,
				Status:    "done",
				URL:       existing.PDFURL,
			})
			return
		case "queued", "generating":
			h.logger.Info("report: job already in progress", logAttrs...)
			writeJSON(w, http.StatusAccepted, models.ReportResponse{
				RequestID: requestID(r),
				JobID:     existing.ID,
				Status:    existing.Status,
			})
			return
		}
	}

	if _, err := h.store.Create(r.Context(), jobstore.Job{
		ID:     jobID,
		Status: "queued",
	}); err != nil {
		h.logger.Error("report: failed to create job", append(logAttrs, slog.String("error", err.Error()))...)
		writeError(w, r, http.StatusInternalServerError, "STORE_ERROR", "failed to create job", nil)
		return
	}

	resultCh := make(chan pipeline.Result, 1)
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		resultCh <- h.pipe.Run(context.Background(), jobID, body)
	}()

	timer := time.NewTimer(h.syncTimeout)
	defer timer.Stop()

	select {
	case result := <-resultCh:
		if result.Err != nil {
			h.logger.Error("report: pipeline failed", append(logAttrs, slog.String("error", result.Err.Error()))...)
			writeError(w, r, http.StatusInternalServerError, "GENERATION_ERROR", "report generation failed", nil)
			return
		}
		h.logger.Info("report: sync response", append(logAttrs, slog.String("pdfUrl", result.PDFURL))...)
		writeJSON(w, http.StatusOK, models.ReportResponse{
			RequestID: requestID(r),
			JobID:     jobID,
			Status:    "done",
			URL:       result.PDFURL,
		})

	case <-timer.C:
		h.logger.Info("report: async fallback, generation continues in background", logAttrs...)
		writeJSON(w, http.StatusAccepted, models.ReportResponse{
			RequestID: requestID(r),
			JobID:     jobID,
			Status:    "queued",
		})
	}
}
