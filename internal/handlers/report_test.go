package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"

	appmiddleware "pdf-html-service/internal/middleware"
	"pdf-html-service/internal/models"
	"pdf-html-service/internal/pipeline"
	"pdf-html-service/internal/security"
)

func newTestReportHandler(maxPairs int) (*ReportHandler, *mockStorage, *mockRenderer) {
	store := newMockStore()
	storage := newMockStorage()
	renderer := &mockRenderer{pdfBytes: []byte("%PDF-1.7")}
	mailer := &mockMailer{}
	logger := testLogger()

	p := pipeline.New(store, storage, renderer, mailer, logger, "docs", "")
	wg := &sync.WaitGroup{}

	return NewReportHandler(
		logger,
		validator.New(),
		security.NewURLPolicy(true, nil),
		store,
		p,
		wg,
		maxPairs,
		10*time.Second,
	), storage, renderer
}

func TestReportInvalidPayload(t *testing.T) {
	h, _, _ := newTestReportHandler(5)

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/reports", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodPost, "/reports", bytes.NewBufferString(`{"invoiceNumber":""}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestReportTooManyPairs(t *testing.T) {
	h, _, _ := newTestReportHandler(1)

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/reports", h.ServeHTTP)

	payload := samplePayload()
	payload.Pairs = append(payload.Pairs, payload.Pairs[0])
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/reports", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", rr.Code)
	}
}

func TestReportSyncSuccess(t *testing.T) {
	h, storage, _ := newTestReportHandler(5)

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/reports", h.ServeHTTP)

	payload := samplePayload()
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/reports", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp models.ReportResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.JobID == "" || resp.URL == "" || resp.Status != "done" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	storage.mu.Lock()
	defer storage.mu.Unlock()
	if len(storage.pdfObjects) == 0 {
		t.Fatal("expected PDF to be uploaded")
	}
}

func TestReportIdempotent(t *testing.T) {
	h, _, _ := newTestReportHandler(5)

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/reports", h.ServeHTTP)

	payload := samplePayload()
	body, _ := json.Marshal(payload)

	submit := func() models.ReportResponse {
		req := httptest.NewRequest(http.MethodPost, "/reports", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		var resp models.ReportResponse
		json.NewDecoder(rr.Body).Decode(&resp)
		return resp
	}

	first := submit()
	second := submit()

	if first.JobID != second.JobID {
		t.Fatalf("expected same jobId, got %s vs %s", first.JobID, second.JobID)
	}
	if second.Status != "done" {
		t.Fatalf("expected done, got %s", second.Status)
	}
}
