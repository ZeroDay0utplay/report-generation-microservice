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

func newSubmitHandler(maxPairs int, publicURL string) (*ReportSubmitHandler, *mockStorage) {
	st := newMockStorage()
	return NewReportSubmitHandler(
		testLogger(),
		validator.New(),
		security.NewURLPolicy(true, nil),
		newMockStore(),
		st,
		maxPairs,
		"docs",
		"",
	), st
}

func TestReportSubmitInvalidPayload(t *testing.T) {
	h, _ := newSubmitHandler(5, "")

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/v1/reports", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodPost, "/v1/reports", bytes.NewBufferString(`{"invoiceNumber":""}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	var resp models.ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RequestID == "" || resp.Error.Code == "" {
		t.Fatal("expected requestId and error code")
	}
}

func TestReportSubmitTooManyPairs(t *testing.T) {
	h, _ := newSubmitHandler(1, "")

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/v1/reports", h.ServeHTTP)

	payload := samplePayload()
	payload.Pairs = append(payload.Pairs, payload.Pairs[0])
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/v1/reports", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", rr.Code)
	}
}

func TestReportSubmitSuccess(t *testing.T) {
	h, storage := newSubmitHandler(5, "")

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/v1/reports", h.ServeHTTP)

	payload := samplePayload()
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/v1/reports", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp models.ReportSubmitResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.JobID == "" || resp.HTMLURL == "" || resp.Status != "ready" {
		t.Fatalf("expected jobId, htmlUrl, status=ready, got %+v", resp)
	}
	// htmlUrl should be the B2 public URL containing the jobID.
	if !strings.Contains(resp.HTMLURL, resp.JobID) {
		t.Fatalf("htmlUrl should contain jobId, got %s", resp.HTMLURL)
	}
	// HTML should have been uploaded to storage.
	if len(storage.htmlObjects) == 0 {
		t.Fatal("expected HTML to be uploaded to storage")
	}
}

func TestReportSubmitIdempotent(t *testing.T) {
	h, _ := newSubmitHandler(5, "")

	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/v1/reports", h.ServeHTTP)

	payload := samplePayload()
	body, _ := json.Marshal(payload)

	submit := func() models.ReportSubmitResponse {
		req := httptest.NewRequest(http.MethodPost, "/v1/reports", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		var resp models.ReportSubmitResponse
		json.NewDecoder(rr.Body).Decode(&resp)
		return resp
	}

	first := submit()
	second := submit()

	if first.JobID != second.JobID {
		t.Fatalf("expected same jobId for same payload, got %s vs %s", first.JobID, second.JobID)
	}
}

func TestReportStatusNotFound(t *testing.T) {
	store := newMockStore()
	handler := NewReportStatusHandler(testLogger(), store)

	r := chi.NewRouter()
	r.Use(appmiddleware.RequestID)
	r.Get("/v1/reports/{id}", handler.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/v1/reports/doesnotexist", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}
