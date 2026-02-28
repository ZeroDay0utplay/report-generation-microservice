package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"

	appmiddleware "pdf-html-service/internal/middleware"
	"pdf-html-service/internal/models"
	"pdf-html-service/internal/security"
)

func TestPDFHandlerInvalidPayload(t *testing.T) {
	storage := newMockStorage()
	renderer := &mockRenderer{pdfBytes: []byte("%PDF-1.7")}
	h := NewPDFHandler(testLogger(), validator.New(), security.NewURLPolicy(true, nil), storage, renderer, 5, "docs", false)

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
	storage := newMockStorage()
	renderer := &mockRenderer{pdfBytes: []byte("%PDF-1.7")}
	h := NewPDFHandler(testLogger(), validator.New(), security.NewURLPolicy(true, nil), storage, renderer, 1, "docs", false)

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

func TestPDFHandlerAllowsEmptyInvoiceNumber(t *testing.T) {
	storage := newMockStorage()
	renderer := &mockRenderer{pdfBytes: []byte("%PDF-1.7")}
	h := NewPDFHandler(testLogger(), validator.New(), security.NewURLPolicy(true, nil), storage, renderer, 5, "docs", false)

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

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPDFHandlerAllowsNullInvoiceNumber(t *testing.T) {
	storage := newMockStorage()
	renderer := &mockRenderer{pdfBytes: []byte("%PDF-1.7")}
	h := NewPDFHandler(testLogger(), validator.New(), security.NewURLPolicy(true, nil), storage, renderer, 5, "docs", false)

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

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPDFHandlerSuccess(t *testing.T) {
	storage := newMockStorage()
	renderer := &mockRenderer{pdfBytes: []byte("%PDF-1.7 test content")}
	h := NewPDFHandler(testLogger(), validator.New(), security.NewURLPolicy(true, nil), storage, renderer, 5, "docs", true)

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/v1/pdf", h.ServeHTTP)

	payload := samplePayload()
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/v1/pdf", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp models.PDFResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.RequestID == "" || resp.JobID == "" || resp.PDFKey == "" || resp.PDFURL == "" {
		t.Fatalf("expected non-empty response fields, got %+v", resp)
	}
	if resp.Debug == nil || resp.Debug.HTMLKey == "" || resp.Debug.HTMLURL == "" {
		t.Fatalf("expected debug html info in response, got %+v", resp.Debug)
	}

	if _, ok := storage.pdfObjects[resp.PDFKey]; !ok {
		t.Fatalf("expected PDF object %q to be uploaded", resp.PDFKey)
	}
	if !strings.Contains(renderer.lastHTML, "Before / After") {
		t.Fatal("expected renderer to receive generated report HTML")
	}
}
