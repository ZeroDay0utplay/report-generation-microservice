package render

import (
	"strings"
	"testing"

	"pdf-html-service/internal/models"
)

func TestRenderHTMLIncludesCoreSections(t *testing.T) {
	payload := models.ReportRequest{
		InvoiceNumber:    models.StringPtr("INV-2026-0001"),
		InterventionName: "Kitchen Renovation",
		Address:          "123 Main St",
		Company: models.Company{
			Name:    "ACME Services",
			Contact: "+216 00 000 000",
		},
		Pairs: []models.Pair{
			{
				BeforeURL: "https://img.example.com/before.jpg",
				AfterURL:  "https://img.example.com/after.jpg",
				Caption:   "Angle 1",
			},
		},
	}

	html, err := RenderHTML(payload)
	if err != nil {
		t.Fatalf("RenderHTML failed: %v", err)
	}

	checks := []string{
		"Before / After",
		"@page { size: A4; margin: 14mm 12mm; }",
		"alt=\"Before image 1\"",
		"alt=\"After image 1\"",
		"Kitchen Renovation",
	}
	for _, c := range checks {
		if !strings.Contains(html, c) {
			t.Fatalf("expected rendered HTML to contain %q", c)
		}
	}
}

func TestRenderHTMLEscapesUserContent(t *testing.T) {
	payload := models.ReportRequest{
		InvoiceNumber:    models.StringPtr("INV-2026-0001"),
		InterventionName: "<script>alert(1)</script>",
		Address:          "A",
		Company: models.Company{
			Name:    "<b>ACME</b>",
			Contact: "<img src=x onerror=alert(1)>",
		},
		Pairs: []models.Pair{{
			BeforeURL: "https://img.example.com/before.jpg",
			AfterURL:  "https://img.example.com/after.jpg",
		}},
	}

	html, err := RenderHTML(payload)
	if err != nil {
		t.Fatalf("RenderHTML failed: %v", err)
	}

	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Fatal("expected script tag to be escaped")
	}
	if !strings.Contains(html, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatal("expected escaped script content")
	}
	if strings.Contains(html, "<b>ACME</b>") {
		t.Fatal("expected HTML tags in company name to be escaped")
	}
}
