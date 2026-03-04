package render

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/microcosm-cc/bluemonday"

	"pdf-html-service/internal/models"
)

//go:embed templates/report.html templates/report.css
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
	PairsCount       int
	PairsJSON         template.JS
	PairGridClassJSON template.JS
	IncludeDatesJS    template.JS
	Trucks    []photoView
	Evidences []photoView
}

var (
	templateOnce sync.Once
	tpl          *template.Template
	cssContent   template.CSS
	tplErr       error
)

var bufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

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

func buildTemplateData(payload models.ReportRequest, styles template.CSS, logoURL string) (templateData, error) {
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
		trucks = append(trucks, photoView{Index: i + 1, URL: p.URL, Date: p.Date})
	}

	evidences := make([]photoView, 0, len(payload.Evidences))
	for i, p := range payload.Evidences {
		evidences = append(evidences, photoView{Index: i + 1, URL: p.URL, Date: p.Date})
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
		HeaderLogoURL:     logoURL,
		MessageHTML:       template.HTML(messagePolicy.Sanitize(strings.TrimSpace(payload.Message))),
		IncludeDates:      includeDates,
		PairGridClass:     gridClass,
		Company:           payload.Company,
		Email:             email,
		Phone:             phone,
		PairsCount:        len(pairs),
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

	reportTpl, styles, err := loadTemplateBundle()
	if err != nil {
		return err
	}

	data, err := buildTemplateData(payload, styles, logoURL)
	if err != nil {
		return err
	}

	return reportTpl.Execute(w, data)
}

func RenderHTML(payload models.ReportRequest) (string, error) {
	return RenderHTMLWithLogo(payload, "")
}

func RenderHTMLWithLogo(payload models.ReportRequest, logoURL string) (string, error) {
	reportTpl, styles, err := loadTemplateBundle()
	if err != nil {
		return "", err
	}

	data, err := buildTemplateData(payload, styles, logoURL)
	if err != nil {
		return "", err
	}

	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	if err := reportTpl.Execute(buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
