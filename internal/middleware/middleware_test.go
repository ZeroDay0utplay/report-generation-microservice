package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSAddsHeadersToSimpleRequests(t *testing.T) {
	router := http.NewServeMux()
	router.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := CORS("*")(router)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://frontend.example")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expected Access-Control-Allow-Origin=*, got %q", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Methods"); got != corsAllowMethods {
		t.Fatalf("expected Access-Control-Allow-Methods=%q, got %q", corsAllowMethods, got)
	}
	if got := rr.Header().Get("Access-Control-Expose-Headers"); got != corsExposeHeaders {
		t.Fatalf("expected Access-Control-Expose-Headers=%q, got %q", corsExposeHeaders, got)
	}
}

func TestCORSHandlesPreflightRequests(t *testing.T) {
	called := false
	handler := CORS("*")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/v1/pdf", nil)
	req.Header.Set("Origin", "https://frontend.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, X-Request-Id")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if called {
		t.Fatal("expected preflight request to bypass next handler")
	}
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expected Access-Control-Allow-Origin=*, got %q", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type, X-Request-Id" {
		t.Fatalf("expected echoed allow headers, got %q", got)
	}
}
