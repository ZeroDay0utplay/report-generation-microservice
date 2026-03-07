package storage

import (
	"strings"
	"testing"
)

func TestSanitizePublicBaseURLRemovesSignedQueryParams(t *testing.T) {
	raw := "https://reports.s3.eu-central-003.backblazeb2.com/public?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Expires=3600&X-Amz-Signature=abc#section"

	got := sanitizePublicBaseURL(raw)
	want := "https://reports.s3.eu-central-003.backblazeb2.com/public"

	if got != want {
		t.Fatalf("sanitizePublicBaseURL() = %q, want %q", got, want)
	}
}

func TestPublicURLDoesNotIncludeExpiryQuery(t *testing.T) {
	s := &B2Storage{
		publicBaseURL: sanitizePublicBaseURL("https://reports.s3.eu-central-003.backblazeb2.com/public?X-Amz-Expires=3600"),
	}

	got := s.PublicURL("/docs/job_123/index.html")
	want := "https://reports.s3.eu-central-003.backblazeb2.com/public/docs/job_123/index.html"

	if got != want {
		t.Fatalf("PublicURL() = %q, want %q", got, want)
	}
	if strings.Contains(got, "X-Amz-Expires") {
		t.Fatalf("PublicURL() should not include signed expiry params, got %q", got)
	}
}
