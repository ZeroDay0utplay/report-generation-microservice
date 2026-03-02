package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-playground/validator/v10"

	"pdf-html-service/internal/models"
	"pdf-html-service/internal/render"
	"pdf-html-service/internal/security"
	"pdf-html-service/internal/util"
)

type HTMLHandler struct {
	logger       *slog.Logger
	validator    *validator.Validate
	urlPolicy    *security.URLPolicy
	storage      Storage
	maxPairs     int
	outputPrefix string
}

func NewHTMLHandler(
	logger *slog.Logger,
	validator *validator.Validate,
	urlPolicy *security.URLPolicy,
	storage Storage,
	maxPairs int,
	outputPrefix string,
) *HTMLHandler {
	return &HTMLHandler{
		logger:       logger,
		validator:    validator,
		urlPolicy:    urlPolicy,
		storage:      storage,
		maxPairs:     maxPairs,
		outputPrefix: outputPrefix,
	}
}

func (h *HTMLHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	baseLogAttrs := []any{
		slog.String("requestId", requestID(r)),
		slog.String("route", r.URL.Path),
	}

	var payload models.ReportRequest
	if ok := decodeJSONBody(w, r, &payload); !ok {
		h.logger.Warn("html request rejected",
			append(baseLogAttrs, slog.String("errorCode", "INVALID_JSON"))...,
		)
		return
	}

	requestLogAttrs := append(baseLogAttrs,
		slog.String("invoiceNumber", models.StringOrEmpty(payload.InvoiceNumber)),
		slog.Int("pairsCount", len(payload.Pairs)),
	)

	if len(payload.Pairs) > h.maxPairs {
		h.logger.Warn("html request rejected: pairs exceeds configured limit",
			append(requestLogAttrs,
				slog.String("errorCode", "TOO_MANY_PAIRS"),
				slog.Int("maxPairs", h.maxPairs),
			)...,
		)
		writeError(w, r, http.StatusRequestEntityTooLarge, "TOO_MANY_PAIRS", "pairs exceeds configured limit", map[string]any{"maxPairs": h.maxPairs})
		return
	}

	if err := h.validator.Struct(payload); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			h.logger.Warn("html request rejected: payload validation failed",
				append(requestLogAttrs,
					slog.String("errorCode", "VALIDATION_ERROR"),
					slog.Any("validationErrors", validationDetails(ve)),
				)...,
			)
			writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "payload validation failed", validationDetails(ve))
			return
		}
		h.logger.Error("html request validation failed with unexpected validator error",
			append(requestLogAttrs,
				slog.String("errorCode", "VALIDATION_ERROR"),
				slog.String("error", err.Error()),
			)...,
		)
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "payload validation failed", nil)
		return
	}

	if err := h.urlPolicy.ValidatePayload(payload); err != nil {
		var ve *security.ValidationError
		if errors.As(err, &ve) {
			h.logger.Warn("html request rejected: url policy validation failed",
				append(requestLogAttrs,
					slog.String("errorCode", "URL_POLICY_ERROR"),
					slog.Any("violations", ve.Violations),
				)...,
			)
			writeError(w, r, http.StatusBadRequest, "URL_POLICY_ERROR", "URL policy validation failed", ve.Violations)
			return
		}
		h.logger.Error("html request url policy check failed unexpectedly",
			append(requestLogAttrs,
				slog.String("errorCode", "URL_POLICY_ERROR"),
				slog.String("error", err.Error()),
			)...,
		)
		writeError(w, r, http.StatusBadRequest, "URL_POLICY_ERROR", "URL policy validation failed", nil)
		return
	}

	jobID := util.NewJobID()

	htmlStart := time.Now()
	htmlDoc, err := render.RenderHTML(payload)
	htmlGenMS := time.Since(htmlStart).Milliseconds()
	if err != nil {
		h.logger.Error("failed to render html report",
			append(requestLogAttrs,
				slog.String("errorCode", "RENDER_ERROR"),
				slog.String("jobId", jobID),
				slog.String("error", err.Error()),
			)...,
		)
		writeError(w, r, http.StatusInternalServerError, "RENDER_ERROR", "failed to render HTML", nil)
		return
	}

	htmlKey := htmlObjectKey(h.outputPrefix, jobID)
	uploadStart := time.Now()
	if err := h.storage.UploadHTML(r.Context(), htmlKey, htmlDoc); err != nil {
		h.logger.Error("failed to upload html report",
			append(requestLogAttrs,
				slog.String("errorCode", "UPLOAD_ERROR"),
				slog.String("jobId", jobID),
				slog.String("htmlKey", htmlKey),
				slog.String("error", err.Error()),
			)...,
		)
		writeError(w, r, http.StatusInternalServerError, "UPLOAD_ERROR", "failed to upload HTML", nil)
		return
	}
	uploadMS := time.Since(uploadStart).Milliseconds()

	h.logger.Info("html report generated",
		slog.String("requestId", requestID(r)),
		slog.String("jobId", jobID),
		slog.String("route", r.URL.Path),
		slog.String("invoiceNumber", models.StringOrEmpty(payload.InvoiceNumber)),
		slog.Int("pairsCount", len(payload.Pairs)),
		slog.Int64("html_gen_ms", htmlGenMS),
		slog.Int64("upload_ms", uploadMS),
	)

	writeJSON(w, http.StatusOK, models.HTMLResponse{
		RequestID: requestID(r),
		JobID:     jobID,
		HTMLKey:   htmlKey,
		HTMLURL:   h.storage.PublicURL(htmlKey),
	})
}
