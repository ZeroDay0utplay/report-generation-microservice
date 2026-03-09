package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"

	appmiddleware "pdf-html-service/internal/middleware"
	"pdf-html-service/internal/models"
	"pdf-html-service/internal/pdfjobs"
	"pdf-html-service/internal/security"
)

type stubPDFSubmitService struct {
	result pdfjobs.SubmitResult
	err    error
}

func (s *stubPDFSubmitService) Submit(_ context.Context, _ []byte) (pdfjobs.SubmitResult, error) {
	return s.result, s.err
}

func TestPDFHandlerReturnsCompleted(t *testing.T) {
	svc := &stubPDFSubmitService{result: pdfjobs.SubmitResult{JobID: "job_1", Status: "completed", PDFURL: "https://example.com/report.pdf"}}
	h := NewPDFHandler(testLogger(), validator.New(), security.NewURLPolicy(true, nil), svc, 5)

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
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp models.PDFJobResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "completed" || resp.PDFURL == "" || resp.JobID == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestPDFHandlerReturnsProcessing(t *testing.T) {
	svc := &stubPDFSubmitService{result: pdfjobs.SubmitResult{JobID: "job_1", Status: "processing"}}
	h := NewPDFHandler(testLogger(), validator.New(), security.NewURLPolicy(true, nil), svc, 5)

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/v1/pdf", h.ServeHTTP)

	payload := samplePayload()
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/pdf", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPDFHandlerReturnsQueueFull(t *testing.T) {
	svc := &stubPDFSubmitService{err: pdfjobs.ErrQueueFull}
	h := NewPDFHandler(testLogger(), validator.New(), security.NewURLPolicy(true, nil), svc, 5)

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/v1/pdf", h.ServeHTTP)

	payload := samplePayload()
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/pdf", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPDFHandlerReturnsFailedTerminalState(t *testing.T) {
	svc := &stubPDFSubmitService{result: pdfjobs.SubmitResult{JobID: "job_1", Status: "failed", ErrorCode: "UPLOAD_ERROR", ErrorMessage: "failed to upload PDF"}}
	h := NewPDFHandler(testLogger(), validator.New(), security.NewURLPolicy(true, nil), svc, 5)

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/v1/pdf", h.ServeHTTP)

	payload := samplePayload()
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/pdf", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPDFHandlerReturnsValidationError(t *testing.T) {
	svc := &stubPDFSubmitService{}
	h := NewPDFHandler(testLogger(), validator.New(), security.NewURLPolicy(true, nil), svc, 5)

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/v1/pdf", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodPost, "/v1/pdf", bytes.NewBufferString(`{"invoiceNumber":""}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPDFHandlerReturnsGenericServiceError(t *testing.T) {
	svc := &stubPDFSubmitService{err: errors.New("boom")}
	h := NewPDFHandler(testLogger(), validator.New(), security.NewURLPolicy(true, nil), svc, 5)

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/v1/pdf", h.ServeHTTP)

	payload := samplePayload()
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/pdf", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}
