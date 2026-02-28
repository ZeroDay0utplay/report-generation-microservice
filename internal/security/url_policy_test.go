package security

import (
	"testing"

	"pdf-html-service/internal/models"
)

func TestURLPolicyRequireHTTPS(t *testing.T) {
	policy := NewURLPolicy(true, nil)
	payload := validPayload()
	payload.Pairs[0].BeforeURL = "http://images.example.com/before.jpg"

	err := policy.ValidatePayload(payload)
	if err == nil {
		t.Fatal("expected error for non-https URL")
	}
}

func TestURLPolicyAllowlist(t *testing.T) {
	policy := NewURLPolicy(true, []string{"allowed.example.com"})
	payload := validPayload()
	payload.Pairs[0].BeforeURL = "https://blocked.example.com/before.jpg"

	err := policy.ValidatePayload(payload)
	if err == nil {
		t.Fatal("expected allowlist rejection")
	}
}

func TestURLPolicyRejectsIPLiteral(t *testing.T) {
	policy := NewURLPolicy(true, nil)
	payload := validPayload()
	payload.Pairs[0].AfterURL = "https://127.0.0.1/img.jpg"

	err := policy.ValidatePayload(payload)
	if err == nil {
		t.Fatal("expected rejection for IP literal host")
	}
}

func TestURLPolicyValidPayload(t *testing.T) {
	policy := NewURLPolicy(true, []string{"images.example.com", "assets.example.com", "www.example.com"})
	payload := validPayload()
	payload.Company.Website = "https://www.example.com"
	payload.Company.LogoURL = "https://assets.example.com/logo.png"

	err := policy.ValidatePayload(payload)
	if err != nil {
		t.Fatalf("expected valid payload, got error: %v", err)
	}
}

func validPayload() models.ReportRequest {
	return models.ReportRequest{
		InvoiceNumber:    models.StringPtr("INV-2026-0001"),
		InterventionName: "Kitchen renovation",
		Address:          "123 Main St",
		Company: models.Company{
			Name:    "ACME",
			Contact: "+216 00 000 000",
		},
		Pairs: []models.Pair{
			{
				BeforeURL: "https://images.example.com/before.jpg",
				AfterURL:  "https://images.example.com/after.jpg",
			},
		},
	}
}
