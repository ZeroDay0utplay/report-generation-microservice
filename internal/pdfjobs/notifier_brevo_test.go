package pdfjobs

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewBrevoAPINotifierValidation(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{}))

	_, err := NewBrevoAPINotifier(logger, nil, BrevoAPINotifierConfig{
		APIKey:      "",
		SenderEmail: "sender@example.com",
	})
	if err == nil {
		t.Fatal("expected error for missing api key")
	}

	_, err = NewBrevoAPINotifier(logger, nil, BrevoAPINotifierConfig{
		APIKey:      "test-api-key",
		SenderEmail: "invalid-email",
	})
	if err == nil {
		t.Fatal("expected error for invalid sender email")
	}
}

func TestBrevoAPINotifierSendReportReady(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{}))

	var gotKey string
	var payload brevoSendEmailRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("api-key")
		if gotKey == "" {
			t.Fatal("missing api-key header")
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"messageId":"ok"}`))
	}))
	defer srv.Close()

	notifier, err := NewBrevoAPINotifier(logger, srv.Client(), BrevoAPINotifierConfig{
		APIKey:      "test-api-key",
		SenderEmail: "hama.tm662@gmail.com",
		SenderName:  "Hama",
		Subject:     "Rapport pret",
		Endpoint:    srv.URL,
	})
	if err != nil {
		t.Fatalf("new notifier: %v", err)
	}

	err = notifier.SendReportReady(
		context.Background(),
		[]string{"A@example.com", "a@example.com", "mohamed.toumi@enstab.ucar.tn"},
		"job_123",
		"https://ideo-photo-reports.s3.eu-central-003.backblazeb2.com/docs/job_123/report.pdf",
		MissionInfo{
			InterventionName: "Nettoyage de chantier - Batiment A",
			Address:          "12 Rue de la Paix, 75001 Paris",
		},
	)
	if err != nil {
		t.Fatalf("send report ready: %v", err)
	}

	if gotKey != "test-api-key" {
		t.Fatalf("unexpected api key header: %s", gotKey)
	}
	if payload.Sender.Email != "hama.tm662@gmail.com" || payload.Sender.Name != "Hama" {
		t.Fatalf("unexpected sender: %+v", payload.Sender)
	}
	if payload.Subject != "Rapport pret" {
		t.Fatalf("unexpected subject: %s", payload.Subject)
	}
	if len(payload.To) != 2 {
		t.Fatalf("expected deduplicated recipients, got %d", len(payload.To))
	}
	if !strings.Contains(payload.HTMLContent, "Edition Entreprise") {
		t.Fatal("expected enterprise edition heading in html email")
	}
	if strings.Contains(payload.HTMLContent, "Reference du job") {
		t.Fatal("job reference block should not appear in html email content")
	}
	if !strings.Contains(payload.HTMLContent, "Nettoyage de chantier - Batiment A") {
		t.Fatal("expected intervention name in html email")
	}
	if !strings.Contains(payload.HTMLContent, "12 Rue de la Paix, 75001 Paris") {
		t.Fatal("expected address in html email")
	}
	if !strings.Contains(payload.HTMLContent, "backblazeb2.com/docs/job_123/report.pdf") {
		t.Fatal("expected pdf url in html email")
	}
	if !strings.Contains(payload.TextContent, "Lien de telechargement") {
		t.Fatal("expected plain text fallback content")
	}
	if strings.Contains(payload.TextContent, "Reference du job") {
		t.Fatal("job reference block should not appear in text email content")
	}
}

func TestBrevoAPINotifierErrorStatus(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{}))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"bad request"}`))
	}))
	defer srv.Close()

	notifier, err := NewBrevoAPINotifier(logger, srv.Client(), BrevoAPINotifierConfig{
		APIKey:      "test-api-key",
		SenderEmail: "hama.tm662@gmail.com",
		Endpoint:    srv.URL,
	})
	if err != nil {
		t.Fatalf("new notifier: %v", err)
	}

	err = notifier.SendReportReady(
		context.Background(),
		[]string{"a@example.com"},
		"job_1",
		"https://example.com/report.pdf",
		MissionInfo{},
	)
	if err == nil {
		t.Fatal("expected error on non-2xx brevo response")
	}
	if !strings.Contains(err.Error(), "status 400") {
		t.Fatalf("expected status code in error, got %v", err)
	}
}
