package render

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"strings"
	"sync"
	"time"

	"pdf-html-service/internal/models"
)

//go:embed templates/report.html templates/report.css
var reportTemplateFS embed.FS

type pairView struct {
	Index     int
	BeforeURL string
	AfterURL  string
	Date      string
	Caption   string
}

type photoView struct {
	Index int
	URL   string
	Date  string
}

type templateData struct {
	Styles           template.CSS
	Date             string
	InvoiceNumber    string
	InterventionName string
	Address          string
	HeaderLogoURL    string
	MessageHTML      template.HTML
	IncludeDates     bool
	PairGridClass    string
	Company          models.Company
	Email            string
	Phone            string
	Pairs            []pairView
	Trucks           []photoView
	Evidences        []photoView
}

var (
	templateOnce sync.Once
	tpl          *template.Template
	cssContent   template.CSS
	tplErr       error
)

const hostedLogoURL = "https://dev-ideo-assets.s3.eu-central-003.backblazeb2.com/logo.png"

func loadTemplateBundle() (*template.Template, template.CSS, error) {
	templateOnce.Do(func() {
		htmlBytes, err := reportTemplateFS.ReadFile("templates/report.html")
		if err != nil {
			tplErr = fmt.Errorf("read report.html: %w", err)
			return
		}
		cssBytes, err := reportTemplateFS.ReadFile("templates/report.css")
		if err != nil {
			tplErr = fmt.Errorf("read report.css: %w", err)
			return
		}

		cssContent = template.CSS(string(cssBytes))
		tpl, tplErr = template.New("report").Parse(string(htmlBytes))
	})
	return tpl, cssContent, tplErr
}

func resolveLogoURL() string {
	return hostedLogoURL
}

func effectiveIncludeDates(payload models.ReportRequest) bool {
	if payload.IncludeDate != nil {
		return *payload.IncludeDate
	}
	return payload.IncludeDates
}

func normalizePhotoLayout(layout string) string {
	normalized := strings.ToLower(strings.TrimSpace(layout))
	normalized = strings.NewReplacer("-", "", "_", "", " ", "").Replace(normalized)
	return normalized
}

func pairGridClass(photoLayout string) string {
	switch normalizePhotoLayout(photoLayout) {
	case "1", "1byrow", "onebyrow", "onerow", "single", "singlecolumn":
		return "pair-grid pair-grid-one"
	default:
		return "pair-grid"
	}
}

func normalizeMessageHTML(message string) template.HTML {
	return template.HTML(strings.TrimSpace(message))
}

func RenderHTML(payload models.ReportRequest) (string, error) {
	reportTpl, styles, err := loadTemplateBundle()
	if err != nil {
		return "", err
	}

	pairs := make([]pairView, 0, len(payload.Pairs))
	for i, p := range payload.Pairs {
		pairs = append(pairs, pairView{
			Index:     i + 1,
			BeforeURL: p.BeforeURL,
			AfterURL:  p.AfterURL,
			Date:      p.Date,
			Caption:   p.Caption,
		})
	}

	trucks := make([]photoView, 0, len(payload.Trucks))
	for i, p := range payload.Trucks {
		trucks = append(trucks, photoView{
			Index: i + 1,
			URL:   p.URL,
			Date:  p.Date,
		})
	}

	evidences := make([]photoView, 0, len(payload.Evidences))
	for i, p := range payload.Evidences {
		evidences = append(evidences, photoView{
			Index: i + 1,
			URL:   p.URL,
			Date:  p.Date,
		})
	}

	email := payload.Company.Email
	if email == "" {
		email = "-"
	}
	phone := payload.Company.Phone
	if phone == "" {
		phone = "-"
	}

	data := templateData{
		Styles:           styles,
		Date:             time.Now().Format("2006-01-02"),
		InvoiceNumber:    models.StringOrEmpty(payload.InvoiceNumber),
		InterventionName: payload.InterventionName,
		Address:          payload.Address,
		HeaderLogoURL:    resolveLogoURL(),
		MessageHTML:      normalizeMessageHTML(payload.Message),
		IncludeDates:     effectiveIncludeDates(payload),
		PairGridClass:    pairGridClass(payload.PhotoLayout),
		Company:          payload.Company,
		Email:            email,
		Phone:            phone,
		Pairs:            pairs,
		Trucks:           trucks,
		Evidences:        evidences,
	}

	var buf bytes.Buffer
	if err := reportTpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
