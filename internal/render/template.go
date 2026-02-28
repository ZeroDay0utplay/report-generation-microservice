package render

import (
	"bytes"
	"html/template"
	"time"

	"pdf-html-service/internal/models"
)

type pairView struct {
	Index     int
	BeforeURL string
	AfterURL  string
	Caption   string
}

type templateData struct {
	Date             string
	InvoiceNumber    string
	InterventionName string
	Address          string
	Company          models.Company
	Email            string
	Phone            string
	Pairs            []pairView
}

var reportTemplate = template.Must(template.New("report").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Intervention Report {{.InvoiceNumber}}</title>
  <style>
    :root {
      --bg: #ffffff;
      --text: #0f172a;
      --muted: #64748b;
      --border: #e2e8f0;
      --soft: #f8fafc;
      --pill: #eaf2ff;
      --pill-text: #1e3a8a;
      --radius-lg: 14px;
      --radius-md: 10px;
    }
    * { box-sizing: border-box; }
    html, body {
      margin: 0;
      padding: 0;
      background: var(--bg);
      color: var(--text);
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
      line-height: 1.45;
      -webkit-print-color-adjust: exact;
      print-color-adjust: exact;
    }
    .report {
      width: 100%;
      margin: 0 auto;
    }
    .card {
      border: 1px solid var(--border);
      border-radius: var(--radius-lg);
      background: var(--bg);
      padding: 16px;
      margin-bottom: 14px;
    }
    .header {
      display: flex;
      justify-content: space-between;
      align-items: flex-start;
      gap: 20px;
      background: linear-gradient(135deg, #f8fafc 0%, #ffffff 100%);
    }
    .header-left {
      display: flex;
      gap: 12px;
      align-items: center;
      min-width: 0;
    }
    .logo {
      width: 54px;
      height: 54px;
      border-radius: 12px;
      border: 1px solid var(--border);
      object-fit: cover;
      background: #fff;
      flex: 0 0 auto;
    }
    .company-title {
      font-weight: 700;
      font-size: 18px;
      letter-spacing: 0.01em;
      margin-bottom: 2px;
    }
    .muted {
      color: var(--muted);
      font-size: 13px;
      word-break: break-word;
    }
    .header-right {
      text-align: right;
      min-width: 170px;
      padding: 2px 0;
    }
    .k {
      display: block;
      color: var(--muted);
      font-size: 12px;
      margin-top: 6px;
    }
    .v {
      display: block;
      font-size: 14px;
      font-weight: 600;
      color: var(--text);
      letter-spacing: 0.01em;
    }
    .title-card {
      background: var(--soft);
    }
    .title {
      margin: 0;
      font-size: 28px;
      line-height: 1.2;
      letter-spacing: -0.02em;
    }
    .subtitle {
      margin-top: 8px;
      color: var(--muted);
      font-size: 14px;
    }
    .meta-grid {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 10px;
    }
    .meta-item {
      background: #fff;
      border: 1px solid var(--border);
      border-radius: var(--radius-md);
      padding: 10px 12px;
    }
    .section-title {
      font-size: 17px;
      font-weight: 700;
      margin: 0 0 12px;
      letter-spacing: 0.01em;
    }
    .pair-row {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 12px;
      margin-bottom: 14px;
      break-inside: avoid;
      page-break-inside: avoid;
    }
    .image-tile {
      border: 1px solid var(--border);
      border-radius: var(--radius-md);
      overflow: hidden;
      background: #fff;
      break-inside: avoid;
      page-break-inside: avoid;
    }
    .tile-head {
      padding: 10px 12px 8px;
      border-bottom: 1px solid var(--border);
      background: #fff;
    }
    .pill {
      display: inline-block;
      font-size: 12px;
      color: var(--pill-text);
      background: var(--pill);
      border: 1px solid #dbeafe;
      border-radius: 999px;
      padding: 3px 10px;
      font-weight: 600;
      letter-spacing: 0.02em;
      text-transform: uppercase;
    }
    .img-wrap {
      background: #f8fafc;
      height: 220px;
    }
    .img-wrap img {
      width: 100%;
      height: 100%;
      display: block;
      object-fit: cover;
    }
    .caption {
      margin: 8px 0 0;
      grid-column: 1 / -1;
      color: var(--muted);
      font-size: 13px;
      padding: 2px 4px;
      break-inside: avoid;
      page-break-inside: avoid;
    }
    @media (max-width: 860px) {
      .pair-row {
        grid-template-columns: 1fr;
      }
      .caption {
        grid-column: auto;
      }
      .meta-grid {
        grid-template-columns: 1fr;
      }
      .header {
        flex-direction: column;
      }
      .header-right {
        text-align: left;
      }
    }
    @page { size: A4; margin: 14mm 12mm; }
  </style>
</head>
<body>
  <main class="report">
    <section class="card header">
      <div class="header-left">
        {{if .Company.LogoURL}}
        <img class="logo" src="{{.Company.LogoURL}}" alt="{{.Company.Name}} logo" />
        {{end}}
        <div>
          <div class="company-title">{{.Company.Name}}</div>
          <div class="muted">{{.Company.Contact}}</div>
          {{if .Company.Website}}<div class="muted">{{.Company.Website}}</div>{{end}}
        </div>
      </div>
      <div class="header-right">
        <span class="k">Invoice</span>
        <span class="v">{{.InvoiceNumber}}</span>
        <span class="k">Date</span>
        <span class="v">{{.Date}}</span>
      </div>
    </section>

    <section class="card title-card">
      <h1 class="title">{{.InterventionName}}</h1>
      <div class="subtitle">{{.Address}}</div>
    </section>

    <section class="card">
      <h2 class="section-title">Metadata</h2>
      <div class="meta-grid">
        <div class="meta-item"><span class="k">Invoice</span><span class="v">{{.InvoiceNumber}}</span></div>
        <div class="meta-item"><span class="k">Date</span><span class="v">{{.Date}}</span></div>
        <div class="meta-item"><span class="k">Email</span><span class="v">{{.Email}}</span></div>
        <div class="meta-item"><span class="k">Phone</span><span class="v">{{.Phone}}</span></div>
      </div>
    </section>

    <section class="card">
      <h2 class="section-title">Before / After</h2>
      {{range .Pairs}}
      <article class="pair-row">
        <div class="image-tile">
          <div class="tile-head"><span class="pill">Before</span></div>
          <div class="img-wrap">
            <img src="{{.BeforeURL}}" alt="Before image {{.Index}}" />
          </div>
        </div>
        <div class="image-tile">
          <div class="tile-head"><span class="pill">After</span></div>
          <div class="img-wrap">
            <img src="{{.AfterURL}}" alt="After image {{.Index}}" />
          </div>
        </div>
        {{if .Caption}}<p class="caption">{{.Caption}}</p>{{end}}
      </article>
      {{end}}
    </section>
  </main>
</body>
</html>
`))

func RenderHTML(payload models.ReportRequest) (string, error) {
	pairs := make([]pairView, 0, len(payload.Pairs))
	for i, p := range payload.Pairs {
		pairs = append(pairs, pairView{
			Index:     i + 1,
			BeforeURL: p.BeforeURL,
			AfterURL:  p.AfterURL,
			Caption:   p.Caption,
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
		Date:             time.Now().Format("2006-01-02"),
		InvoiceNumber:    models.StringOrEmpty(payload.InvoiceNumber),
		InterventionName: payload.InterventionName,
		Address:          payload.Address,
		Company:          payload.Company,
		Email:            email,
		Phone:            phone,
		Pairs:            pairs,
	}

	var buf bytes.Buffer
	if err := reportTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
