package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"

	"pdf-html-service/internal/jobstore"
	appmiddleware "pdf-html-service/internal/middleware"
	"pdf-html-service/internal/pdfjobs"
)

type stubRecipientService struct {
	result     pdfjobs.RegisterRecipientsResult
	err        error
	lastJobID  string
	lastEmails []string
}

func (s *stubRecipientService) RegisterRecipients(_ context.Context, jobID string, emails []string) (pdfjobs.RegisterRecipientsResult, error) {
	s.lastJobID = jobID
	s.lastEmails = append([]string(nil), emails...)
	return s.result, s.err
}

func TestPDFRecipientsHandlerSuccess(t *testing.T) {
	svc := &stubRecipientService{result: pdfjobs.RegisterRecipientsResult{
		JobID:            "job_1",
		Accepted:         []string{"a@example.com", "b@example.com"},
		TotalRecipients:  2,
		EmailStatus:      "registered",
		ProcessingStatus: "processing",
	}}
	h := NewPDFRecipientsHandler(testLogger(), validator.New(), svc)

	r := chi.NewRouter()
	r.Use(appmiddleware.RequestID)
	r.Post("/v1/pdf/recipients", h.ServeHTTP)

	body := `{"jobId":"job_1","emails":["A@example.com","a@example.com"," b@example.com "]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/pdf/recipients", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	if svc.lastJobID != "job_1" {
		t.Fatalf("expected job_1, got %q", svc.lastJobID)
	}
	wantEmails := []string{"a@example.com", "b@example.com"}
	if !reflect.DeepEqual(svc.lastEmails, wantEmails) {
		t.Fatalf("expected deduped normalized emails %v, got %v", wantEmails, svc.lastEmails)
	}
}

func TestPDFRecipientsHandlerNotFound(t *testing.T) {
	svc := &stubRecipientService{err: jobstore.ErrNotFound}
	h := NewPDFRecipientsHandler(testLogger(), validator.New(), svc)

	r := chi.NewRouter()
	r.Use(appmiddleware.RequestID)
	r.Post("/v1/pdf/recipients", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodPost, "/v1/pdf/recipients", bytes.NewBufferString(`{"jobId":"job_x","emails":["a@example.com"]}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestPDFRecipientsHandlerValidation(t *testing.T) {
	svc := &stubRecipientService{}
	h := NewPDFRecipientsHandler(testLogger(), validator.New(), svc)

	r := chi.NewRouter()
	r.Use(appmiddleware.RequestID)
	r.Post("/v1/pdf/recipients", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodPost, "/v1/pdf/recipients", bytes.NewBufferString(`{"jobId":"","emails":["not-email"]}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPDFRecipientsHandlerServiceError(t *testing.T) {
	svc := &stubRecipientService{err: errors.New("boom")}
	h := NewPDFRecipientsHandler(testLogger(), validator.New(), svc)

	r := chi.NewRouter()
	r.Use(appmiddleware.RequestID)
	r.Post("/v1/pdf/recipients", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodPost, "/v1/pdf/recipients", bytes.NewBufferString(`{"jobId":"job_1","emails":["a@example.com"]}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

func TestNormalizeEmails(t *testing.T) {
	got := normalizeEmails([]string{" A@example.com ", "a@example.com", "", "B@example.com"})
	want := []string{"a@example.com", "b@example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestPDFRecipientsHandlerResponseShape(t *testing.T) {
	svc := &stubRecipientService{result: pdfjobs.RegisterRecipientsResult{
		JobID:            "job_1",
		Accepted:         []string{"a@example.com"},
		TotalRecipients:  1,
		EmailStatus:      "pending",
		ProcessingStatus: "completed",
	}}
	h := NewPDFRecipientsHandler(testLogger(), validator.New(), svc)

	r := chi.NewRouter()
	r.Use(appmiddleware.RequestID)
	r.Post("/v1/pdf/recipients", h.ServeHTTP)

	req := httptest.NewRequest(http.MethodPost, "/v1/pdf/recipients", bytes.NewBufferString(`{"jobId":"job_1","emails":["a@example.com"]}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var got map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["jobId"] != "job_1" || got["emailStatus"] != "pending" || got["status"] != "completed" {
		t.Fatalf("unexpected body: %+v", got)
	}
}
