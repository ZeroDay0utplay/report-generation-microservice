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

func TestHTMLHandlerInvalidPayload(t *testing.T) {
	storage := newMockStorage()
	h := NewHTMLHandler(testLogger(), validator.New(), security.NewURLPolicy(true, nil), storage, 5, "docs")

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/v1/html", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodPost, "/v1/html", bytes.NewBufferString(`{"invoiceNumber":""}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rr.Code)
	}

	var resp models.ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.RequestID == "" {
		t.Fatal("expected requestId in error response")
	}
	if resp.Error.Code == "" {
		t.Fatal("expected error code")
	}
}

func TestHTMLHandlerTooManyPairs(t *testing.T) {
	storage := newMockStorage()
	h := NewHTMLHandler(testLogger(), validator.New(), security.NewURLPolicy(true, nil), storage, 1, "docs")

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/v1/html", h.ServeHTTP)

	payload := samplePayload()
	payload.Pairs = append(payload.Pairs, payload.Pairs[0])
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/v1/html", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status 413, got %d", rr.Code)
	}
}

func TestHTMLHandlerAllowsEmptyInvoiceNumber(t *testing.T) {
	storage := newMockStorage()
	h := NewHTMLHandler(testLogger(), validator.New(), security.NewURLPolicy(true, nil), storage, 5, "docs")

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/v1/html", h.ServeHTTP)

	payload := samplePayload()
	payload.InvoiceNumber = models.StringPtr("")
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/v1/html", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHTMLHandlerAllowsNullInvoiceNumber(t *testing.T) {
	storage := newMockStorage()
	h := NewHTMLHandler(testLogger(), validator.New(), security.NewURLPolicy(true, nil), storage, 5, "docs")

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/v1/html", h.ServeHTTP)

	payload := samplePayload()
	payload.InvoiceNumber = nil
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/v1/html", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHTMLHandlerSuccess(t *testing.T) {
	storage := newMockStorage()
	h := NewHTMLHandler(testLogger(), validator.New(), security.NewURLPolicy(true, nil), storage, 5, "docs")

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/v1/html", h.ServeHTTP)

	payload := samplePayload()
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/v1/html", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp models.HTMLResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.RequestID == "" || resp.JobID == "" || resp.HTMLKey == "" || resp.HTMLURL == "" {
		t.Fatalf("expected non-empty response fields, got %+v", resp)
	}
	if _, ok := storage.htmlObjects[resp.HTMLKey]; !ok {
		t.Fatalf("expected html object %q to be uploaded", resp.HTMLKey)
	}
	html := storage.htmlObjects[resp.HTMLKey]
	if !strings.Contains(html, "Photos camions") || !strings.Contains(html, "Photos preuves") {
		t.Fatalf("expected uploaded HTML to include trucks and evidences sections")
	}
}
