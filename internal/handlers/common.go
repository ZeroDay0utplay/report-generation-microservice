package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-playground/validator/v10"

	"pdf-html-service/internal/middleware"
	"pdf-html-service/internal/models"
	"pdf-html-service/internal/security"
)

type Storage interface {
	UploadHTML(ctx context.Context, key string, html string) error
	UploadPDF(ctx context.Context, key string, reader io.Reader) error
	PublicURL(key string) string
}

type PDFRenderer interface {
	ConvertHTMLToPDF(ctx context.Context, html string) (io.ReadCloser, error)
}

type baseHandler struct {
	logger    *slog.Logger
	validate  *validator.Validate
	urlPolicy *security.URLPolicy
	maxPairs  int
}

func (b *baseHandler) validateReportPayload(
	w http.ResponseWriter, r *http.Request,
	rawBody []byte,
) (*models.ReportRequest, []any, bool) {
	baseAttrs := []any{
		slog.String("requestId", requestID(r)),
		slog.String("route", r.URL.Path),
	}

	var payload models.ReportRequest
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		if isBodyTooLarge(err) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "request body exceeds limit", nil)
			return nil, baseAttrs, false
		}
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		switch {
		case errors.As(err, &syntaxErr):
			writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "malformed JSON", map[string]any{"offset": syntaxErr.Offset})
		case errors.As(err, &typeErr):
			writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid field type", map[string]any{"field": typeErr.Field, "offset": typeErr.Offset})
		default:
			writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid JSON payload", map[string]any{"error": err.Error()})
		}
		b.logger.Warn("request rejected: invalid json", append(baseAttrs, slog.String("errorCode", "INVALID_JSON"))...)
		return nil, baseAttrs, false
	}

	reqAttrs := append(baseAttrs,
		slog.String("invoiceNumber", models.StringOrEmpty(payload.InvoiceNumber)),
		slog.Int("pairsCount", len(payload.Pairs)),
	)

	if len(payload.Pairs) > b.maxPairs {
		b.logger.Warn("request rejected: pairs exceeds limit",
			append(reqAttrs,
				slog.String("errorCode", "TOO_MANY_PAIRS"),
				slog.Int("maxPairs", b.maxPairs),
			)...,
		)
		writeError(w, r, http.StatusRequestEntityTooLarge, "TOO_MANY_PAIRS",
			"pairs exceeds configured limit", map[string]any{"maxPairs": b.maxPairs})
		return nil, reqAttrs, false
	}

	if err := b.validate.Struct(payload); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			b.logger.Warn("request rejected: validation failed",
				append(reqAttrs, slog.String("errorCode", "VALIDATION_ERROR"))...)
			writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR",
				"payload validation failed", validationDetails(ve))
			return nil, reqAttrs, false
		}
		b.logger.Error("unexpected validator error",
			append(reqAttrs, slog.String("error", err.Error()))...)
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "payload validation failed", nil)
		return nil, reqAttrs, false
	}

	if err := b.urlPolicy.ValidatePayload(payload); err != nil {
		var ve *security.ValidationError
		if errors.As(err, &ve) {
			b.logger.Warn("request rejected: url policy failed",
				append(reqAttrs, slog.String("errorCode", "URL_POLICY_ERROR"))...)
			writeError(w, r, http.StatusBadRequest, "URL_POLICY_ERROR",
				"URL policy validation failed", ve.Violations)
			return nil, reqAttrs, false
		}
		b.logger.Error("unexpected url policy error",
			append(reqAttrs, slog.String("error", err.Error()))...)
		writeError(w, r, http.StatusBadRequest, "URL_POLICY_ERROR", "URL policy validation failed", nil)
		return nil, reqAttrs, false
	}

	return &payload, reqAttrs, true
}

func requestID(r *http.Request) string {
	return middleware.RequestIDFromContext(r.Context())
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")

	b, err := json.Marshal(payload)
	if err == nil {
		var m map[string]json.RawMessage
		if json.Unmarshal(b, &m) == nil {
			m["statusCode"], _ = json.Marshal(status)
			b, err = json.Marshal(m)
		}
	}

	w.WriteHeader(status)
	if err != nil {
		_ = json.NewEncoder(w).Encode(payload)
		return
	}
	_, _ = w.Write(b)
	_, _ = w.Write([]byte("\n"))
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, details any) {
	writeJSON(w, status, models.ErrorResponse{
		RequestID: requestID(r),
		Error: models.APIError{
			Code:    code,
			Message: message,
			Details: details,
		},
	})
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		if isBodyTooLarge(err) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "request body exceeds limit", nil)
			return false
		}
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		switch {
		case errors.Is(err, io.EOF):
			writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "request body is required", nil)
		case errors.As(err, &syntaxErr):
			writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "malformed JSON", map[string]any{"offset": syntaxErr.Offset})
		case errors.As(err, &typeErr):
			writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid field type", map[string]any{"field": typeErr.Field, "offset": typeErr.Offset})
		default:
			writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid JSON payload", map[string]any{"error": err.Error()})
		}
		return false
	}

	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "only one JSON object is allowed", nil)
		return false
	}
	return true
}

func validationDetails(err validator.ValidationErrors) []map[string]string {
	details := make([]map[string]string, 0, len(err))
	for _, fe := range err {
		details = append(details, map[string]string{
			"field": fe.Namespace(),
			"rule":  fe.Tag(),
			"param": fe.Param(),
		})
	}
	return details
}

func isBodyTooLarge(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}

func htmlObjectKey(prefix, jobID string) string {
	return fmt.Sprintf("%s/%s/index.html", strings.Trim(prefix, "/"), jobID)
}

func pdfObjectKey(prefix, jobID string) string {
	return fmt.Sprintf("%s/%s/report.pdf", strings.Trim(prefix, "/"), jobID)
}
