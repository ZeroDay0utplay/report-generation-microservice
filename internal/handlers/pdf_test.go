package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"

	appmiddleware "pdf-html-service/internal/middleware"
	"pdf-html-service/internal/models"
	"pdf-html-service/internal/security"
)

const (
	testChunkSize    = 50
	testConcurrency  = 4
	testChunkTimeout = 90 * time.Second
	testMergeTimeout = 120 * time.Second
	testJobLockTTL   = 30 * time.Second
	testJobPollDelay = 20 * time.Millisecond
)

func newPDFHandler(maxPairs int, storage *mockStorage, renderer *mockRenderer) *PDFHandler {
	return NewPDFHandler(
		testLogger(), validator.New(), security.NewURLPolicy(true, nil),
		newMockStore(), storage, renderer,
		maxPairs, "docs", false, "",
		testChunkSize, testConcurrency, testChunkTimeout, testMergeTimeout, testJobLockTTL, testJobPollDelay,
	)
}

func newPDFRouter(h *PDFHandler) *chi.Mux {
	router := chi.NewRouter()
	router.Use(appmiddleware.RequestID)
	router.Post("/v1/pdf", h.ServeHTTP)
	return router
}

func TestPDFHandlerInvalidPayload(t *testing.T) {
	h := newPDFHandler(5, newMockStorage(), &mockRenderer{pdfBytes: []byte("%PDF-1.7")})
	router := newPDFRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/v1/pdf", bytes.NewBufferString(`{"invoiceNumber":""}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rr.Code)
	}
}

func TestPDFHandlerTooManyPairs(t *testing.T) {
	h := newPDFHandler(1, newMockStorage(), &mockRenderer{pdfBytes: []byte("%PDF-1.7")})
	router := newPDFRouter(h)

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
	h := newPDFHandler(5, newMockStorage(), &mockRenderer{pdfBytes: []byte("%PDF-1.7")})
	router := newPDFRouter(h)

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
	h := newPDFHandler(5, newMockStorage(), &mockRenderer{pdfBytes: []byte("%PDF-1.7")})
	router := newPDFRouter(h)

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
	h := NewPDFHandler(
		testLogger(), validator.New(), security.NewURLPolicy(true, nil),
		newMockStore(), storage, renderer,
		5, "docs", true, "",
		testChunkSize, testConcurrency, testChunkTimeout, testMergeTimeout, testJobLockTTL, testJobPollDelay,
	)
	router := newPDFRouter(h)

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

	if resp.RequestID == "" || resp.JobID == "" || resp.PDFKey == "" || resp.URL == "" {
		t.Fatalf("expected non-empty response fields, got %+v", resp)
	}
	if resp.Status != statusReady {
		t.Fatalf("expected status %q, got %q", statusReady, resp.Status)
	}
	if resp.Debug == nil || resp.Debug.HTMLKey == "" || resp.Debug.HTMLURL == "" {
		t.Fatalf("expected debug html info in response, got %+v", resp.Debug)
	}

	if _, ok := storage.pdfObjects[resp.PDFKey]; !ok {
		t.Fatalf("expected PDF object %q to be uploaded", resp.PDFKey)
	}
	if !strings.Contains(renderer.lastHTML, "avant") || !strings.Contains(renderer.lastHTML, "apres") {
		t.Fatal("expected renderer to receive generated PDF report HTML")
	}
}

func TestPDFHandlerIdempotent(t *testing.T) {
	storage := newMockStorage()
	renderer := &mockRenderer{pdfBytes: []byte("%PDF-1.7")}
	store := newMockStore()
	h := NewPDFHandler(
		testLogger(), validator.New(), security.NewURLPolicy(true, nil),
		store, storage, renderer,
		5, "docs", false, "",
		testChunkSize, testConcurrency, testChunkTimeout, testMergeTimeout, testJobLockTTL, testJobPollDelay,
	)
	router := newPDFRouter(h)

	payload := samplePayload()
	body, _ := json.Marshal(payload)

	submit := func() models.PDFResponse {
		req := httptest.NewRequest(http.MethodPost, "/v1/pdf", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		var resp models.PDFResponse
		json.NewDecoder(rr.Body).Decode(&resp)
		return resp
	}

	first := submit()
	second := submit()

	if first.JobID != second.JobID {
		t.Fatalf("expected same jobId, got %s vs %s", first.JobID, second.JobID)
	}
	if first.URL != second.URL {
		t.Fatalf("expected same url, got %s vs %s", first.URL, second.URL)
	}
}

func TestPDFHandlerChunkedPipeline(t *testing.T) {
	storage := newMockStorage()
	renderer := &mockRenderer{pdfBytes: []byte("%PDF-1.7")}
	h := NewPDFHandler(
		testLogger(), validator.New(), security.NewURLPolicy(true, nil),
		newMockStore(), storage, renderer,
		200, "docs", false, "",
		2, testConcurrency, testChunkTimeout, testMergeTimeout, testJobLockTTL, testJobPollDelay,
	)
	router := newPDFRouter(h)

	payload := samplePayload()
	payload.Pairs = make([]models.Pair, 5)
	for i := range payload.Pairs {
		payload.Pairs[i] = models.Pair{
			BeforeURL: "https://img.example.com/before.jpg",
			AfterURL:  "https://img.example.com/after.jpg",
		}
	}
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
	if resp.URL == "" {
		t.Fatal("expected non-empty url")
	}
}

func TestPDFHandlerConversionError(t *testing.T) {
	renderer := &mockRenderer{err: errors.New("gotenberg unavailable")}
	h := newPDFHandler(5, newMockStorage(), renderer)
	router := newPDFRouter(h)

	payload := samplePayload()
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/v1/pdf", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rr.Code)
	}

	var resp models.ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error.Code != "PDF_PIPELINE_ERROR" {
		t.Fatalf("expected PDF_PIPELINE_ERROR, got %s", resp.Error.Code)
	}
}

func TestBuildPDFChunksSingleChunk(t *testing.T) {
	pairs := make([]models.Pair, 10)
	chunks := buildPDFChunks(pairs, 50)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if !chunks[0].isFirst || !chunks[0].isLast {
		t.Fatal("single chunk should be both first and last")
	}
	if len(chunks[0].pairs) != 10 {
		t.Fatalf("expected 10 pairs, got %d", len(chunks[0].pairs))
	}
}

func TestBuildPDFChunksMultipleChunks(t *testing.T) {
	pairs := make([]models.Pair, 7)
	chunks := buildPDFChunks(pairs, 3)

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	if !chunks[0].isFirst || chunks[0].isLast {
		t.Fatal("first chunk: isFirst=true, isLast=false")
	}
	if chunks[1].isFirst || chunks[1].isLast {
		t.Fatal("middle chunk: isFirst=false, isLast=false")
	}
	if chunks[2].isFirst || !chunks[2].isLast {
		t.Fatal("last chunk: isFirst=false, isLast=true")
	}

	if len(chunks[0].pairs) != 3 || len(chunks[1].pairs) != 3 || len(chunks[2].pairs) != 1 {
		t.Fatalf("expected chunk sizes [3,3,1], got [%d,%d,%d]",
			len(chunks[0].pairs), len(chunks[1].pairs), len(chunks[2].pairs))
	}

	if chunks[0].indexOffset != 0 || chunks[1].indexOffset != 3 || chunks[2].indexOffset != 6 {
		t.Fatalf("expected offsets [0,3,6], got [%d,%d,%d]",
			chunks[0].indexOffset, chunks[1].indexOffset, chunks[2].indexOffset)
	}

	for _, c := range chunks {
		if c.totalPairs != 7 {
			t.Fatalf("expected totalPairs=7, got %d", c.totalPairs)
		}
	}
}

func TestBuildPDFChunksZeroChunkSize(t *testing.T) {
	pairs := make([]models.Pair, 5)
	chunks := buildPDFChunks(pairs, 0)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for chunkSize=0, got %d", len(chunks))
	}
}
