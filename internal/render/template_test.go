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
		"alt=\"Photo avant 1\"",
		"alt=\"Photo apres 1\"",
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
