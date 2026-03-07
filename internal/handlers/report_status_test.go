package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"pdf-html-service/internal/jobstore"
	appmiddleware "pdf-html-service/internal/middleware"
	"pdf-html-service/internal/models"
)

func TestReportStatusHandlerSuccess(t *testing.T) {
	store := newMockStore()
	_, _ = store.Update(t.Context(), jobstore.Job{
		ID:        "job_123",
		Status:    statusReady,
		PDFURL:    "https://public.example.com/docs/job_123/report.pdf",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	h := NewReportStatusHandler(testLogger(), store)
	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Get("/v1/reports/{id}", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/v1/reports/job_123", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp models.ReportStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.JobID != "job_123" || resp.Status != statusReady || resp.URL == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestReportStatusHandlerNotFound(t *testing.T) {
	h := NewReportStatusHandler(testLogger(), newMockStore())
	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Get("/v1/reports/{id}", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/v1/reports/missing", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}
