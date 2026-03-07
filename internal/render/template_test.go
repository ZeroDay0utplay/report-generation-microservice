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
		PhotoLayout:      "one_by_row",
		Company: models.Company{
			Name:    "ACME Services",
			Contact: "+216 00 000 000",
		},
		Pairs: []models.Pair{
			{
				BeforeURL: "https://img.example.com/before.jpg",
				AfterURL:  "https://img.example.com/after.jpg",
				Date:      "2026-02-20",
				Caption:   "Angle 1",
			},
		},
	}

	html, err := RenderHTML(payload)
	if err != nil {
		t.Fatalf("RenderHTML failed: %v", err)
	}

	checks := []string{
		"Documentation avant / apres",
		"@page { size: A4; margin: 12mm; }",
		"__REPORT_DATA__",
		"https://img.example.com/before.jpg",
		"https://img.example.com/after.jpg",
		"pair-grid pair-grid-one",
		"grid-template-columns: 1fr;",
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

func TestRenderHTMLIncludesTrucksAndEvidences(t *testing.T) {
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
				Date:      "2026-02-20",
			},
		},
		Trucks: []models.Photo{
			{URL: "https://img.example.com/truck.jpg", Date: "2026-02-21"},
		},
		Evidences: []models.Photo{
			{URL: "https://img.example.com/evidence.jpg", Date: "2026-02-22"},
		},
		IncludeDates: true,
	}

	html, err := RenderHTML(payload)
	if err != nil {
		t.Fatalf("RenderHTML failed: %v", err)
	}

	checks := []string{
		"Photos camions",
		"Photos preuves",
		"alt=\"Photo camion 1\"",
		"alt=\"Photo preuve 1\"",
		"2026-02-20",
		"2026-02-21",
		"2026-02-22",
		"https://img.example.com/truck.jpg",
		"https://img.example.com/evidence.jpg",
	}
	for _, c := range checks {
		if !strings.Contains(html, c) {
			t.Fatalf("expected rendered HTML to contain %q", c)
		}
	}
}

func TestRenderHTMLRendersMessageHTML(t *testing.T) {
	payload := models.ReportRequest{
		InvoiceNumber:    models.StringPtr("INV-2026-0001"),
		InterventionName: "Kitchen Renovation",
		Address:          "123 Main St",
		Message:          "<p><strong>Important</strong> chantier termine.</p>",
		Company: models.Company{
			Name:    "ACME Services",
			Contact: "+216 00 000 000",
		},
		Pairs: []models.Pair{
			{
				BeforeURL: "https://img.example.com/before.jpg",
				AfterURL:  "https://img.example.com/after.jpg",
			},
		},
	}

	html, err := RenderHTML(payload)
	if err != nil {
		t.Fatalf("RenderHTML failed: %v", err)
	}

	if !strings.Contains(html, "<strong>Important</strong>") {
		t.Fatal("expected message HTML tags to be rendered")
	}
}

func TestRenderHTMLHidesDatesWhenIncludeDatesDisabled(t *testing.T) {
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
				Date:      "2026-02-20",
			},
		},
		Trucks: []models.Photo{
			{URL: "https://img.example.com/truck.jpg", Date: "2026-02-21"},
		},
		IncludeDates: false,
	}

	html, err := RenderHTML(payload)
	if err != nil {
		t.Fatalf("RenderHTML failed: %v", err)
	}

	if strings.Contains(html, "class=\"pair-date-badge\">2026-02-20</span>") ||
		strings.Contains(html, "class=\"photo-date\">2026-02-21</span>") {
		t.Fatal("expected per-photo date badges to be hidden when includeDates is false")
	}
}

func TestRenderHTMLIncludeDateAliasOverridesIncludeDates(t *testing.T) {
	includeDate := false
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
				Date:      "2026-02-20",
			},
		},
		IncludeDates: true,
		IncludeDate:  &includeDate,
	}

	html, err := RenderHTML(payload)
	if err != nil {
		t.Fatalf("RenderHTML failed: %v", err)
	}

	if strings.Contains(html, "class=\"pair-date-badge\">2026-02-20</span>") {
		t.Fatal("expected includeDate alias to hide per-photo date badges")
	}
}

func TestRenderHTMLHeaderDatesAreSortedUniqueAndAggregated(t *testing.T) {
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
				BeforeURL: "https://img.example.com/before-1.jpg",
				AfterURL:  "https://img.example.com/after-1.jpg",
				Date:      "2026-02-05",
			},
			{
				BeforeURL: "https://img.example.com/before-2.jpg",
				AfterURL:  "https://img.example.com/after-2.jpg",
				Date:      "2026-02-01",
			},
		},
		Trucks: []models.Photo{
			{URL: "https://img.example.com/truck.jpg", Date: "2026-02-05"},
		},
		Evidences: []models.Photo{
			{URL: "https://img.example.com/evidence.jpg", Date: "2026-02-08"},
		},
	}

	html, err := RenderHTML(payload)
	if err != nil {
		t.Fatalf("RenderHTML failed: %v", err)
	}

	if !strings.Contains(html, "2026-02-01, 2026-02-05, 2026-02-08") {
		t.Fatal("expected header dates to be sorted, unique, and aggregated from image payload")
	}
}

func TestRenderHTMLStripsTemporarySignatureParamsFromImageURLs(t *testing.T) {
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
				BeforeURL: "https://img.example.com/before.jpg?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Expires=3600&w=1200",
				AfterURL:  "https://img.example.com/after.jpg?signature=abc123&fit=cover",
			},
		},
		Trucks: []models.Photo{
			{URL: "https://img.example.com/truck.jpg?Expires=1700000000&h=600"},
		},
		Evidences: []models.Photo{
			{URL: "https://img.example.com/evidence.jpg?AWSAccessKeyId=test&size=large"},
		},
	}

	html, err := RenderHTML(payload)
	if err != nil {
		t.Fatalf("RenderHTML failed: %v", err)
	}

	expectedKept := []string{
		"https://img.example.com/before.jpg?w=1200",
		"https://img.example.com/after.jpg?fit=cover",
		"https://img.example.com/truck.jpg?h=600",
		"https://img.example.com/evidence.jpg?size=large",
	}
	for _, url := range expectedKept {
		if !strings.Contains(html, url) {
			t.Fatalf("expected rendered HTML to contain sanitized URL %q", url)
		}
	}

	unwanted := []string{"X-Amz-Expires", "signature=abc123", "Expires=1700000000", "AWSAccessKeyId=test"}
	for _, token := range unwanted {
		if strings.Contains(html, token) {
			t.Fatalf("expected rendered HTML to remove temporary signature token %q", token)
		}
	}
}

func TestRenderHTMLWithLogoStripsTemporarySignatureParams(t *testing.T) {
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
			},
		},
	}

	html, err := RenderHTMLWithLogo(payload, "https://cdn.example.com/logo.png?X-Amz-Expires=3600&v=1")
	if err != nil {
		t.Fatalf("RenderHTMLWithLogo failed: %v", err)
	}

	if !strings.Contains(html, "https://cdn.example.com/logo.png?v=1") {
		t.Fatal("expected logo URL to keep non-signing params")
	}
	if strings.Contains(html, "X-Amz-Expires=") {
		t.Fatal("expected logo URL to remove temporary signing params")
	}
}
