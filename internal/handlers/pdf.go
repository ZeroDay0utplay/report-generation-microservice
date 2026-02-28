package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-playground/validator/v10"

	"pdf-html-service/internal/gotenberg"
	"pdf-html-service/internal/models"
	"pdf-html-service/internal/render"
	"pdf-html-service/internal/security"
	"pdf-html-service/internal/util"
)

type PDFHandler struct {
	logger          *slog.Logger
	validator       *validator.Validate
	urlPolicy       *security.URLPolicy
	storage         Storage
	renderer        PDFRenderer
	maxPairs        int
	outputPrefix    string
	uploadHTMLOnPDF bool
}

func NewPDFHandler(
	logger *slog.Logger,
	validator *validator.Validate,
	urlPolicy *security.URLPolicy,
	storage Storage,
	renderer PDFRenderer,
	maxPairs int,
	outputPrefix string,
	uploadHTMLOnPDF bool,
) *PDFHandler {
	return &PDFHandler{
		logger:          logger,
		validator:       validator,
		urlPolicy:       urlPolicy,
		storage:         storage,
		renderer:        renderer,
		maxPairs:        maxPairs,
		outputPrefix:    outputPrefix,
		uploadHTMLOnPDF: uploadHTMLOnPDF,
	}
}

func (h *PDFHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var payload models.ReportRequest
	if ok := decodeJSONBody(w, r, &payload); !ok {
		return
	}

	if len(payload.Pairs) > h.maxPairs {
		writeError(w, r, http.StatusRequestEntityTooLarge, "TOO_MANY_PAIRS", "pairs exceeds configured limit", map[string]any{"maxPairs": h.maxPairs})
		return
	}

	if err := h.validator.Struct(payload); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "payload validation failed", validationDetails(ve))
			return
		}
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "payload validation failed", nil)
		return
	}

	if err := h.urlPolicy.ValidatePayload(payload); err != nil {
		var ve *security.ValidationError
		if errors.As(err, &ve) {
			writeError(w, r, http.StatusBadRequest, "URL_POLICY_ERROR", "URL policy validation failed", ve.Violations)
			return
		}
		writeError(w, r, http.StatusBadRequest, "URL_POLICY_ERROR", "URL policy validation failed", nil)
		return
	}

	jobID := util.NewJobID()

	htmlStart := time.Now()
	htmlDoc, err := render.RenderHTML(payload)
	htmlGenMS := time.Since(htmlStart).Milliseconds()
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "RENDER_ERROR", "failed to render HTML", nil)
		return
	}

	var debug *models.PDFDebug
	if h.uploadHTMLOnPDF {
		htmlKey := htmlObjectKey(h.outputPrefix, jobID)
		if err := h.storage.UploadHTML(r.Context(), htmlKey, htmlDoc); err != nil {
			writeError(w, r, http.StatusInternalServerError, "UPLOAD_ERROR", "failed to upload debug HTML", nil)
			return
		}
		debug = &models.PDFDebug{HTMLKey: htmlKey, HTMLURL: h.storage.PublicURL(htmlKey)}
	}

	gotenbergStart := time.Now()
	pdfReader, err := h.renderer.ConvertHTMLToPDF(r.Context(), htmlDoc)
	gotenbergMS := time.Since(gotenbergStart).Milliseconds()
	if err != nil {
		var ge *gotenberg.ConvertError
		if errors.As(err, &ge) {
			writeError(w, r, http.StatusInternalServerError, "GOTENBERG_ERROR", "PDF conversion failed", map[string]any{
				"status":      ge.Status,
				"bodySnippet": ge.BodySnippet,
			})
			return
		}
		writeError(w, r, http.StatusInternalServerError, "GOTENBERG_ERROR", "PDF conversion failed", nil)
		return
	}
	defer pdfReader.Close()

	pdfKey := pdfObjectKey(h.outputPrefix, jobID)
	uploadStart := time.Now()
	if err := h.storage.UploadPDF(r.Context(), pdfKey, pdfReader); err != nil {
		writeError(w, r, http.StatusInternalServerError, "UPLOAD_ERROR", "failed to upload PDF", nil)
		return
	}
	uploadMS := time.Since(uploadStart).Milliseconds()

	h.logger.Info("pdf report generated",
		slog.String("requestId", requestID(r)),
		slog.String("jobId", jobID),
		slog.String("route", r.URL.Path),
		slog.String("invoiceNumber", models.StringOrEmpty(payload.InvoiceNumber)),
		slog.Int("pairsCount", len(payload.Pairs)),
		slog.Int64("html_gen_ms", htmlGenMS),
		slog.Int64("gotenberg_ms", gotenbergMS),
		slog.Int64("upload_ms", uploadMS),
	)

	writeJSON(w, http.StatusOK, models.PDFResponse{
		RequestID: requestID(r),
		JobID:     jobID,
		PDFKey:    pdfKey,
		PDFURL:    h.storage.PublicURL(pdfKey),
		Debug:     debug,
	})
}
