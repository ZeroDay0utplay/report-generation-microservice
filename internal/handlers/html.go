package handlers

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-playground/validator/v10"

	"pdf-html-service/internal/jobstore"
	"pdf-html-service/internal/models"
	"pdf-html-service/internal/render"
	"pdf-html-service/internal/security"
	"pdf-html-service/internal/util"
)

type ReportSubmitHandler struct {
	baseHandler
	store        jobstore.Store
	storage      Storage
	outputPrefix string
	logoURL      string
}

func NewReportSubmitHandler(
	logger *slog.Logger,
	validate *validator.Validate,
	urlPolicy *security.URLPolicy,
	store jobstore.Store,
	storage Storage,
	maxPairs int,
	outputPrefix string,
	logoURL string,
) *ReportSubmitHandler {
	return &ReportSubmitHandler{
		baseHandler:  baseHandler{logger: logger, validate: validate, urlPolicy: urlPolicy, maxPairs: maxPairs},
		store:        store,
		storage:      storage,
		outputPrefix: outputPrefix,
		logoURL:      logoURL,
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

	payload, reqAttrs, ok := h.validateReportPayload(w, r, body)
	if !ok {
		return
	}

	jobID, err := util.JobIDFromReportRequest(*payload)
	if err != nil {
		h.logger.Error("report submit: failed to compute job id",
			append(reqAttrs, slog.String("error", err.Error()))...,
		)
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to compute job id", nil)
		return
	}

	existing, err := h.store.GetJob(r.Context(), jobID)
	if err == nil && existing.HTMLURL != "" {
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
	if err != nil && !errors.Is(err, jobstore.ErrNotFound) {
		h.logger.Error("report submit: failed to read existing job",
			append(reqAttrs,
				slog.String("jobId", jobID),
				slog.String("error", err.Error()),
			)...,
		)
		writeError(w, r, http.StatusInternalServerError, "STORE_ERROR", "failed to read existing job", nil)
		return
	}
	html, err := render.RenderHTMLWithLogo(*payload, h.logoURL)
	if err != nil {
		h.logger.Error("report submit: failed to render html",
			append(reqAttrs,
				slog.String("jobId", jobID),
				slog.String("error", err.Error()),
			)...,
		)
		writeError(w, r, http.StatusInternalServerError, "RENDER_ERROR", "failed to render HTML", nil)
		return
	}

	htmlKey := htmlObjectKey(h.outputPrefix, jobID)
	if err := h.storage.UploadHTML(r.Context(), htmlKey, html); err != nil {
		h.logger.Error("report submit: failed to upload html",
			append(reqAttrs,
				slog.String("jobId", jobID),
				slog.String("error", err.Error()),
			)...,
		)
		writeError(w, r, http.StatusInternalServerError, "UPLOAD_ERROR", "failed to upload HTML", nil)
		return
	}

	htmlURL := h.storage.PublicURL(htmlKey)

	now := time.Now()
	job := jobstore.Job{
		ID:        jobID,
		Status:    statusReady,
		HTMLURL:   htmlURL,
		CreatedAt: now,
		UpdatedAt: now,
		Payload:   body,
	}
	if !existing.CreatedAt.IsZero() {
		job.CreatedAt = existing.CreatedAt
		job.PDFURL = existing.PDFURL
		job.ErrorCode = existing.ErrorCode
		job.ErrorMsg = existing.ErrorMsg
	}

	saved, saveErr := h.store.Save(r.Context(), job)
	if saveErr != nil {
		h.logger.Error("report submit: failed to save job",
			append(reqAttrs,
				slog.String("jobId", jobID),
				slog.String("error", saveErr.Error()),
			)...,
		)
		writeError(w, r, http.StatusInternalServerError, "STORE_ERROR", "failed to save job", nil)
		return
	}
	if saved.HTMLURL == "" {
		job.CreatedAt = saved.CreatedAt
		job.PDFURL = saved.PDFURL
		job.ErrorCode = saved.ErrorCode
		job.ErrorMsg = saved.ErrorMsg
		saved, saveErr = h.store.Update(r.Context(), job)
		if saveErr != nil {
			h.logger.Error("report submit: failed to update job",
				append(reqAttrs,
					slog.String("jobId", jobID),
					slog.String("error", saveErr.Error()),
				)...,
			)
			writeError(w, r, http.StatusInternalServerError, "STORE_ERROR", "failed to update job", nil)
			return
		}
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
