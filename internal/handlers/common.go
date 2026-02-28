package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-playground/validator/v10"

	"pdf-html-service/internal/middleware"
	"pdf-html-service/internal/models"
)

type Storage interface {
	UploadHTML(ctx context.Context, key string, html string) error
	UploadPDF(ctx context.Context, key string, reader io.Reader) error
	PublicURL(key string) string
}

type PDFRenderer interface {
	ConvertHTMLToPDF(ctx context.Context, html string) (io.ReadCloser, error)
}

func requestID(r *http.Request) string {
	return middleware.RequestIDFromContext(r.Context())
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
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
	if errors.As(err, &maxBytesErr) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "request body too large")
}

func htmlObjectKey(prefix, jobID string) string {
	return fmt.Sprintf("%s/%s/index.html", strings.Trim(prefix, "/"), jobID)
}

func pdfObjectKey(prefix, jobID string) string {
	return fmt.Sprintf("%s/%s/report.pdf", strings.Trim(prefix, "/"), jobID)
}
