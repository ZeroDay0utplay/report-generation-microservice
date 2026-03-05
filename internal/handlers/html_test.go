package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"

	appmiddleware "pdf-html-service/internal/middleware"
	"pdf-html-service/internal/jobstore"
	"pdf-html-service/internal/models"
	"pdf-html-service/internal/security"
)

func newTestRouter(submitHandler *ReportSubmitHandler, store jobstore.Store) *chi.Mux {
	r := chi.NewRouter()
	r.Use(appmiddleware.RequestID)
	r.Post("/v1/reports", submitHandler.ServeHTTP)
	r.Get("/v1/reports/{id}", NewReportStatusHandler(testLogger(), store).ServeHTTP)
	r.Get("/v1/reports/{id}/html", NewReportHTMLHandler(testLogger(), store, "").ServeHTTP)
	return r
}

func TestReportSubmitInvalidPayload(t *testing.T) {
	store := jobstore.NewMemoryStore()
	h := NewReportSubmitHandler(testLogger(), validator.New(), security.NewURLPolicy(true, nil), store, 5, "")

	router := newTestRouter(h, store)
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
	store := jobstore.NewMemoryStore()
	h := NewReportSubmitHandler(testLogger(), validator.New(), security.NewURLPolicy(true, nil), store, 1, "")

	router := newTestRouter(h, store)
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
	store := jobstore.NewMemoryStore()
	h := NewReportSubmitHandler(testLogger(), validator.New(), security.NewURLPolicy(true, nil), store, 5, "https://example.com")

	router := newTestRouter(h, store)
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
	if !strings.Contains(resp.HTMLURL, resp.JobID) {
		t.Fatalf("htmlUrl should contain jobId, got %s", resp.HTMLURL)
	}
}

func TestReportSubmitIdempotent(t *testing.T) {
	store := jobstore.NewMemoryStore()
	h := NewReportSubmitHandler(testLogger(), validator.New(), security.NewURLPolicy(true, nil), store, 5, "https://example.com")

	router := newTestRouter(h, store)
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

func TestReportHTMLSSR(t *testing.T) {
	store := jobstore.NewMemoryStore()
	h := NewReportSubmitHandler(testLogger(), validator.New(), security.NewURLPolicy(true, nil), store, 5, "")

	router := newTestRouter(h, store)
	payload := samplePayload()
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/v1/reports", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var submitResp models.ReportSubmitResponse
	json.NewDecoder(rr.Body).Decode(&submitResp)

	htmlReq := httptest.NewRequest(http.MethodGet, "/v1/reports/"+submitResp.JobID+"/html", nil)
	htmlRr := httptest.NewRecorder()
	router.ServeHTTP(htmlRr, htmlReq)

	if htmlRr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", htmlRr.Code)
	}
	html := htmlRr.Body.String()
	if !strings.Contains(html, "Documentation avant / apres") {
		t.Fatal("expected HTML to contain pair section heading")
	}
	if !strings.Contains(html, "__REPORT_DATA__") {
		t.Fatal("expected HTML to contain virtual scroll data")
	}
	if !strings.Contains(html, "Photos camions") {
		t.Fatal("expected HTML to contain trucks section")
	}
	if !strings.Contains(html, "Photos preuves") {
		t.Fatal("expected HTML to contain evidences section")
	}
}

func TestReportHTMLEtag(t *testing.T) {
	store := jobstore.NewMemoryStore()
	payload := samplePayload()
	rawPayload, _ := json.Marshal(payload)
	job := jobstore.Job{ID: "job_test123", Status: "ready", HTMLURL: "/v1/reports/job_test123/html", Payload: rawPayload}
	store.Save(context.Background(), job)

	handler := NewReportHTMLHandler(testLogger(), store, "")

	r := chi.NewRouter()
	r.Get("/v1/reports/{id}/html", handler.ServeHTTP)

	// First request: get the ETag from the response.
	req1 := httptest.NewRequest(http.MethodGet, "/v1/reports/job_test123/html", nil)
	rr1 := httptest.NewRecorder()
	r.ServeHTTP(rr1, req1)
	etag := rr1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header in first response")
	}

	// Second request: send the ETag back — expect 304.
	req2 := httptest.NewRequest(http.MethodGet, "/v1/reports/job_test123/html", nil)
	req2.Header.Set("If-None-Match", etag)
	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusNotModified {
		t.Fatalf("expected 304 for matching ETag, got %d", rr2.Code)
	}
}

func TestReportStatusNotFound(t *testing.T) {
	store := jobstore.NewMemoryStore()
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
