package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/hibiken/asynq"

	appmiddleware "pdf-html-service/internal/middleware"
	"pdf-html-service/internal/models"
	"pdf-html-service/internal/security"
)

// newTestPDFHandler creates a PDFHandler with a non-functional Asynq client
// (no Redis) for validation-only tests.
func newTestPDFHandler(maxPairs int) *PDFHandler {
	asynqClient := asynq.NewClient(asynq.RedisClientOpt{Addr: "localhost:0"})
	return NewPDFHandler(
		testLogger(),
		validator.New(),
		security.NewURLPolicy(true, nil),
		newMockStore(),
		asynqClient,
		maxPairs,
		"docs",
		"",
	)
}

func TestPDFHandlerInvalidPayload(t *testing.T) {
	h := newTestPDFHandler(5)

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/v1/pdf", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodPost, "/v1/pdf", bytes.NewBufferString(`{"invoiceNumber":""}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rr.Code)
	}
}

func TestPDFHandlerTooManyPairs(t *testing.T) {
	h := newTestPDFHandler(1)

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/v1/pdf", h.ServeHTTP)

	payload := samplePayload()
	payload.Pairs = append(payload.Pairs, payload.Pairs[0])
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/v1/pdf", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status 413, got %d", rr.Code)
	}
}

// TestPDFHandlerValidPayloadPassesValidation ensures a well-formed payload
// passes all validation checks. With no Redis in tests, the handler reaches
// the enqueue step and returns 500 (queue error), not a 4xx.
func TestPDFHandlerValidPayloadPassesValidation(t *testing.T) {
	h := newTestPDFHandler(5)

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/v1/pdf", h.ServeHTTP)

	payload := samplePayload()
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/v1/pdf", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code == http.StatusBadRequest || rr.Code == http.StatusRequestEntityTooLarge {
		t.Fatalf("expected validation to pass (non-4xx), got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPDFHandlerAllowsEmptyInvoiceNumber(t *testing.T) {
	h := newTestPDFHandler(5)

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/v1/pdf", h.ServeHTTP)

	payload := samplePayload()
	payload.InvoiceNumber = models.StringPtr("")
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/v1/pdf", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code == http.StatusBadRequest {
		t.Fatalf("expected non-400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPDFHandlerAllowsNullInvoiceNumber(t *testing.T) {
	h := newTestPDFHandler(5)

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/v1/pdf", h.ServeHTTP)

	payload := samplePayload()
	payload.InvoiceNumber = nil
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/v1/pdf", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code == http.StatusBadRequest {
		t.Fatalf("expected non-400, got %d body=%s", rr.Code, rr.Body.String())
	}
}
