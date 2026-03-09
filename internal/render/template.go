package render

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/microcosm-cc/bluemonday"

	"pdf-html-service/internal/models"
)

//go:embed templates/report.html templates/report_pdf.html templates/report.css
var reportTemplateFS embed.FS

var messagePolicy = bluemonday.UGCPolicy()

type pairView struct {
	Index     int    `json:"index"`
	BeforeURL string `json:"beforeUrl"`
	AfterURL  string `json:"afterUrl"`
	Date      string `json:"date"`
	Caption   string `json:"caption"`
}

type photoView struct {
	Index int
	URL   string
	Date  string
}

type templateData struct {
	Styles            template.CSS
	Date              string
	InvoiceNumber     string
	InterventionName  string
	Address           string
	HeaderLogoURL     string
	MessageHTML       template.HTML
	IncludeDates      bool
	PairGridClass     string
	Company           models.Company
	Email             string
	Phone             string
	PairsCount        int
	Pairs             []pairView
	PairsJSON         template.JS
	PairGridClassJSON template.JS
	IncludeDatesJS    template.JS
	Trucks            []photoView
	Evidences         []photoView
}

var (
	htmlOnce sync.Once
	htmlTpl  *template.Template
	htmlCSS  template.CSS
	htmlErr  error

	pdfOnce sync.Once
	pdfTpl  *template.Template
	pdfCSS  template.CSS
	pdfErr  error
)

var bufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

func loadHTMLBundle() (*template.Template, template.CSS, error) {
	htmlOnce.Do(func() {
		htmlBytes, err := reportTemplateFS.ReadFile("templates/report.html")
		if err != nil {
			htmlErr = fmt.Errorf("read report.html: %w", err)
			return
		}
		cssBytes, err := reportTemplateFS.ReadFile("templates/report.css")
		if err != nil {
			htmlErr = fmt.Errorf("read report.css: %w", err)
			return
		}
		htmlCSS = template.CSS(string(cssBytes))
		htmlTpl, htmlErr = template.New("report").Parse(string(htmlBytes))
	})
	return htmlTpl, htmlCSS, htmlErr
}

func loadPDFBundle() (*template.Template, template.CSS, error) {
	pdfOnce.Do(func() {
		htmlBytes, err := reportTemplateFS.ReadFile("templates/report_pdf.html")
		if err != nil {
			pdfErr = fmt.Errorf("read report_pdf.html: %w", err)
			return
		}
		cssBytes, err := reportTemplateFS.ReadFile("templates/report.css")
		if err != nil {
			pdfErr = fmt.Errorf("read report.css: %w", err)
			return
		}
		css := template.CSS(string(cssBytes))
		t, err := template.New("report_pdf").Parse(string(htmlBytes))
		if err != nil {
			pdfErr = fmt.Errorf("parse report_pdf.html: %w", err)
			return
		}
		pdfTpl = t
		pdfCSS = css
	})
	return pdfTpl, pdfCSS, pdfErr
}

func effectiveIncludeDates(payload models.ReportRequest) bool {
	if payload.IncludeDate != nil {
		return *payload.IncludeDate
	}
	return payload.IncludeDates
}

func sortedUniquePayloadDates(payload models.ReportRequest) string {
	dateSet := make(map[string]struct{})
	for _, pair := range payload.Pairs {
		if pair.Date != "" {
			dateSet[pair.Date] = struct{}{}
		}
	}
	for _, truck := range payload.Trucks {
		if truck.Date != "" {
			dateSet[truck.Date] = struct{}{}
		}
	}
	for _, evidence := range payload.Evidences {
		if evidence.Date != "" {
			dateSet[evidence.Date] = struct{}{}
		}
	}
	if len(dateSet) == 0 {
		return "-"
	}
	dates := make([]string, 0, len(dateSet))
	for d := range dateSet {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	return strings.Join(dates, ", ")
}

func normalizePhotoLayout(layout string) string {
	normalized := strings.ToLower(strings.TrimSpace(layout))
	return strings.NewReplacer("-", "", "_", "", " ", "").Replace(normalized)
}

func pairGridClass(photoLayout string) string {
	switch normalizePhotoLayout(photoLayout) {
	case "1", "1byrow", "onebyrow", "onerow", "single", "singlecolumn":
		return "pair-grid pair-grid-one"
	default:
		return "pair-grid"
	}
}

func stripTemporarySignatureParams(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	q := u.Query()
	removed := false
	for key := range q {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "x-amz-") ||
			strings.HasPrefix(lower, "x-bz-") ||
			lower == "expires" ||
			lower == "signature" ||
			lower == "awsaccesskeyid" ||
			lower == "authorization" {
			q.Del(key)
			removed = true
		}
	}

	if removed {
		u.RawQuery = q.Encode()
	}
	u.Fragment = ""
	return u.String()
}

func buildTemplateData(payload models.ReportRequest, styles template.CSS, logoURL string, stripSig bool) (templateData, error) {
	normalize := func(u string) string {
		if stripSig {
			return stripTemporarySignatureParams(u)
		}
		return u
	}

	pairs := make([]pairView, 0, len(payload.Pairs))
	for i, p := range payload.Pairs {
		pairs = append(pairs, pairView{
			Index:     i + 1,
			BeforeURL: normalize(p.BeforeURL),
			AfterURL:  normalize(p.AfterURL),
			Date:      p.Date,
			Caption:   p.Caption,
		})
	}

	trucks := make([]photoView, 0, len(payload.Trucks))
	for i, p := range payload.Trucks {
		trucks = append(trucks, photoView{
			Index: i + 1,
			URL:   normalize(p.URL),
			Date:  p.Date,
		})
	}

	evidences := make([]photoView, 0, len(payload.Evidences))
	for i, p := range payload.Evidences {
		evidences = append(evidences, photoView{
			Index: i + 1,
			URL:   normalize(p.URL),
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

	gridClass := pairGridClass(payload.PhotoLayout)
	includeDates := effectiveIncludeDates(payload)

	pairsJSON, err := json.Marshal(pairs)
	if err != nil {
		return templateData{}, fmt.Errorf("marshal pairs json: %w", err)
	}
	gridClassJSON, _ := json.Marshal(gridClass)

	includeDatesStr := "false"
	if includeDates {
		includeDatesStr = "true"
	}

	return templateData{
		Styles:            styles,
		Date:              sortedUniquePayloadDates(payload),
		InvoiceNumber:     models.StringOrEmpty(payload.InvoiceNumber),
		InterventionName:  payload.InterventionName,
		Address:           payload.Address,
		HeaderLogoURL:     normalize(logoURL),
		MessageHTML:       template.HTML(messagePolicy.Sanitize(strings.TrimSpace(payload.Message))),
		IncludeDates:      includeDates,
		PairGridClass:     gridClass,
		Company:           payload.Company,
		Email:             email,
		Phone:             phone,
		PairsCount:        len(pairs),
		Pairs:             pairs,
		PairsJSON:         template.JS(pairsJSON),
		PairGridClassJSON: template.JS(gridClassJSON),
		IncludeDatesJS:    template.JS(includeDatesStr),
		Trucks:            trucks,
		Evidences:         evidences,
	}, nil
}

func RenderHTMLTo(ctx context.Context, w io.Writer, payload models.ReportRequest, logoURL string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	t, styles, err := loadHTMLBundle()
	if err != nil {
		return err
	}

	data, err := buildTemplateData(payload, styles, logoURL, true)
	if err != nil {
		return err
	}

	return t.Execute(w, data)
}

func RenderPDFHTMLTo(ctx context.Context, w io.Writer, payload models.ReportRequest, logoURL string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	t, styles, err := loadPDFBundle()
	if err != nil {
		return err
	}

	data, err := buildTemplateData(payload, styles, logoURL, false)
	if err != nil {
		return err
	}

	return t.Execute(w, data)
}

func RenderHTML(payload models.ReportRequest) (string, error) {
	return RenderHTMLWithLogo(payload, "")
}

func RenderPDFHTMLWithLogo(payload models.ReportRequest, logoURL string) (string, error) {
	t, styles, err := loadPDFBundle()
	if err != nil {
		return "", err
	}

	data, err := buildTemplateData(payload, styles, logoURL, false)
	if err != nil {
		return "", err
	}

	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	if err := t.Execute(buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func RenderHTMLWithLogo(payload models.ReportRequest, logoURL string) (string, error) {
	t, styles, err := loadHTMLBundle()
	if err != nil {
		return "", err
	}

	data, err := buildTemplateData(payload, styles, logoURL, true)
	if err != nil {
		return "", err
	}

	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	if err := t.Execute(buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
