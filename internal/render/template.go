package render

import (
	"bytes"
	"encoding/base64"
	"html/template"
	"os"
	"strings"
	"sync"
	"time"

	"pdf-html-service/internal/models"
)

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
	Date             string
	InvoiceNumber    string
	InterventionName string
	Address          string
	HeaderLogoURL    string
	FooterIconsURL   string
	MessageHTML      template.HTML
	IncludeDates     bool
	PhotoLayout      string
	PairGridClass    string
	Company          models.Company
	Email            string
	Phone            string
	Pairs            []pairView
	Trucks           []photoView
	Evidences        []photoView
}

var (
	ideoLogoOnce    sync.Once
	ideoLogoDataURI string
	footerIconsOnce sync.Once
	footerIconsURI  string
)

var reportTemplate = template.Must(template.New("report").Parse(`<!doctype html>
<html lang="fr">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>{{.InterventionName}} - {{.InvoiceNumber}}</title>
  <style>
    :root {
      --bg: #edf7fd;
      --surface: #ffffff;
      --surface-alt: #f5fbff;
      --ink: #123247;
      --muted: #5a7d92;
      --line: #cde4f2;
      --accent: #3AA8D5;
      --accent-2: #79c9e8;
      --accent-3: #1d6c92;
      --radius-xl: 22px;
      --radius-lg: 16px;
      --radius-md: 12px;
      --shadow-soft: 0 10px 30px rgba(16, 38, 53, 0.08);
      --shadow-lift: 0 16px 36px rgba(11, 28, 40, 0.14);
    }

    * { box-sizing: border-box; }

    html,
    body {
      margin: 0;
      padding: 0;
      color: var(--ink);
      font-family: "Manrope", "Avenir Next", "Segoe UI", sans-serif;
      background:
        radial-gradient(1200px 700px at 10% -15%, rgba(58, 168, 213, 0.18), transparent 58%),
        radial-gradient(900px 540px at 90% -20%, rgba(121, 201, 232, 0.24), transparent 64%),
        var(--bg);
      -webkit-print-color-adjust: exact;
      print-color-adjust: exact;
    }

    .report-shell {
      position: relative;
      width: min(1120px, 100% - 34px);
      margin: 22px auto 32px;
      display: grid;
      gap: 16px;
    }

    .report-shell::before,
    .report-shell::after {
      content: "";
      position: absolute;
      pointer-events: none;
      z-index: 0;
      border-radius: 999px;
      filter: blur(0.5px);
      animation: floatBlob 9s ease-in-out infinite;
    }

    .report-shell::before {
      width: 180px;
      height: 180px;
      background: radial-gradient(circle at 30% 20%, rgba(58, 168, 213, 0.26), rgba(58, 168, 213, 0));
      top: -40px;
      right: 5%;
    }

    .report-shell::after {
      width: 140px;
      height: 140px;
      background: radial-gradient(circle at 60% 35%, rgba(121, 201, 232, 0.3), rgba(121, 201, 232, 0));
      bottom: 130px;
      left: -20px;
      animation-delay: 1.6s;
    }

    .panel {
      position: relative;
      z-index: 1;
      background: var(--surface);
      border: 1px solid var(--line);
      border-radius: var(--radius-xl);
      box-shadow: var(--shadow-soft);
      overflow: hidden;
      animation: revealUp 620ms ease both;
    }

    .hero {
      padding: 20px;
      background:
        linear-gradient(160deg, rgba(58, 168, 213, 0.12), rgba(58, 168, 213, 0) 40%),
        linear-gradient(30deg, rgba(121, 201, 232, 0.14), rgba(121, 201, 232, 0) 52%),
        var(--surface);
    }

    .hero-head {
      display: flex;
      justify-content: space-between;
      align-items: flex-start;
      gap: 20px;
      padding-bottom: 16px;
      border-bottom: 1px dashed var(--line);
    }

    .brand {
      display: flex;
      gap: 14px;
      align-items: flex-start;
      min-width: 0;
    }

    .logo {
      width: 58px;
      height: 58px;
      border-radius: 14px;
      object-fit: cover;
      border: 1px solid var(--line);
      background: #ffffff;
      box-shadow: inset 0 0 0 2px rgba(255, 255, 255, 0.65);
    }

    .eyebrow {
      margin: 0 0 6px;
      text-transform: uppercase;
      letter-spacing: 0.14em;
      font-size: 11px;
      color: var(--accent);
      font-weight: 800;
    }

    .brand h1 {
      margin: 0;
      font-size: clamp(24px, 3vw, 34px);
      line-height: 1.14;
      letter-spacing: -0.02em;
      text-wrap: balance;
    }

    .address {
      margin: 8px 0 0;
      color: var(--muted);
      font-size: 14px;
      max-width: 70ch;
    }

    .invoice-meta {
      display: grid;
      grid-template-columns: repeat(2, minmax(120px, 1fr));
      gap: 10px;
      min-width: min(300px, 100%);
    }

    .meta-tile {
      border: 1px solid var(--line);
      border-radius: var(--radius-md);
      background: #ffffff;
      padding: 10px 12px;
      min-height: 64px;
      position: relative;
      overflow: hidden;
    }

    .meta-tile::before {
      content: "";
      position: absolute;
      inset: 0;
      background: linear-gradient(120deg, transparent 0%, rgba(58, 168, 213, 0.1) 45%, transparent 100%);
      transform: translateX(-110%);
      animation: shine 5.8s linear infinite;
    }

    .meta-label {
      display: block;
      font-size: 11px;
      text-transform: uppercase;
      letter-spacing: 0.12em;
      color: var(--muted);
      margin-bottom: 6px;
      position: relative;
      z-index: 1;
    }

    .meta-value {
      display: block;
      font-size: 15px;
      font-weight: 700;
      position: relative;
      z-index: 1;
      word-break: break-word;
    }

    .company-strip {
      display: grid;
      grid-template-columns: 1fr;
      gap: 10px;
      margin-top: 16px;
    }

    .company-card {
      border: 1px solid var(--line);
      border-radius: var(--radius-md);
      background: var(--surface-alt);
      padding: 10px 12px;
      min-height: 74px;
    }

    .company-card .meta-value {
      font-size: 14px;
    }

    .section {
      padding: 18px;
    }

    .message-card {
      border: 1px solid var(--line);
      border-radius: var(--radius-lg);
      background: var(--surface-alt);
      padding: 14px;
      color: var(--ink);
      line-height: 1.55;
      font-size: 14px;
    }

    .message-card p {
      margin: 0 0 10px;
    }

    .message-card p:last-child {
      margin-bottom: 0;
    }

    .message-card strong {
      color: var(--accent-3);
    }

    .section-head {
      display: flex;
      justify-content: space-between;
      align-items: baseline;
      gap: 14px;
      margin-bottom: 14px;
    }

    .section-head h2 {
      margin: 0;
      font-size: 19px;
      letter-spacing: 0.01em;
    }

    .section-head p {
      margin: 0;
      color: var(--muted);
      font-size: 13px;
      letter-spacing: 0.02em;
      text-transform: uppercase;
      font-weight: 700;
    }

    .pair-card {
      border: 1px solid var(--line);
      border-radius: var(--radius-lg);
      padding: 12px;
      background: var(--surface-alt);
      margin-bottom: 12px;
      break-inside: avoid;
      page-break-inside: avoid;
    }

    .pair-card:last-child {
      margin-bottom: 0;
    }

    .pair-grid {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 12px;
    }

    .pair-grid-one {
      grid-template-columns: 1fr;
    }

    .image-tile {
      border: 1px solid var(--line);
      border-radius: var(--radius-md);
      overflow: hidden;
      background: #ffffff;
      box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.5);
      break-inside: avoid;
      page-break-inside: avoid;
    }

    .tile-head {
      display: flex;
      justify-content: space-between;
      align-items: center;
      padding: 9px 11px;
      border-bottom: 1px solid var(--line);
      background: #ffffff;
    }

    .pill {
      display: inline-flex;
      align-items: center;
      border-radius: 999px;
      padding: 3px 11px;
      font-size: 11px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
      font-weight: 800;
      color: var(--accent-3);
      background: rgba(58, 168, 213, 0.16);
      border: 1px solid rgba(58, 168, 213, 0.3);
    }

    .photo-date {
      font-size: 12px;
      font-weight: 700;
      color: var(--muted);
    }

    .img-wrap {
      height: 240px;
      background: #dce8f1;
    }

    .img-wrap img {
      width: 100%;
      height: 100%;
      object-fit: cover;
      display: block;
    }

    .caption {
      margin: 10px 0 0;
      color: var(--muted);
      font-size: 13px;
      line-height: 1.4;
      padding: 0 2px;
    }

    .pair-date {
      margin: 10px 0 0;
      color: var(--muted);
      font-size: 12px;
      font-weight: 700;
      letter-spacing: 0.02em;
      text-transform: uppercase;
      padding: 0 2px;
    }

    .photo-grid {
      display: grid;
      grid-template-columns: 1fr;
      gap: 12px;
    }

    .footer {
      padding: 20px;
      background:
        linear-gradient(180deg, rgba(58, 168, 213, 0.05), rgba(58, 168, 213, 0.12)),
        #f5fbff;
      border: 1px solid #cbe2f0;
      animation-delay: 120ms;
    }

    .footer-head {
      margin-bottom: 14px;
    }

    .footer-head h2 {
      margin: 0;
      font-size: 21px;
      letter-spacing: 0.01em;
    }

    .footer-head p {
      margin: 8px 0 0;
      color: var(--muted);
      font-size: 14px;
      max-width: 80ch;
    }

    .services-grid {
      list-style: none;
      margin: 0;
      padding: 0;
      display: grid;
      grid-template-columns: repeat(5, minmax(0, 1fr));
      gap: 10px;
    }

    .service-item {
      display: flex;
      flex-direction: column;
      gap: 8px;
      border: 1px solid var(--line);
      border-radius: var(--radius-md);
      background: #ffffff;
      padding: 10px;
      min-height: 188px;
      box-shadow: 0 8px 20px rgba(16, 78, 107, 0.11);
      position: relative;
      overflow: hidden;
      animation-name: chipIn, cardPulse;
      animation-duration: 580ms, 6s;
      animation-timing-function: ease, ease-in-out;
      animation-fill-mode: both, both;
      animation-delay: var(--stagger, 0ms), calc(700ms + var(--stagger, 0ms));
      animation-iteration-count: 1, infinite;
      transition: transform 220ms ease, box-shadow 220ms ease, border-color 220ms ease;
    }

    .service-item:hover {
      transform: translateY(-3px);
      box-shadow: 0 14px 28px rgba(16, 78, 107, 0.2);
      border-color: rgba(58, 168, 213, 0.6);
    }

    .service-item::after {
      content: "";
      position: absolute;
      inset: 0;
      pointer-events: none;
      background: linear-gradient(120deg, transparent 0%, rgba(58, 168, 213, 0.12) 50%, transparent 100%);
      transform: translateX(-130%);
      animation: sweep 8.5s linear infinite;
      animation-delay: calc(900ms + var(--stagger, 0ms));
    }

    .service-item:nth-child(2) { --stagger: 60ms; }
    .service-item:nth-child(3) { --stagger: 100ms; }
    .service-item:nth-child(4) { --stagger: 140ms; }
    .service-item:nth-child(5) { --stagger: 180ms; }
    .service-item:nth-child(6) { --stagger: 220ms; }
    .service-item:nth-child(7) { --stagger: 260ms; }
    .service-item:nth-child(8) { --stagger: 300ms; }
    .service-item:nth-child(9) { --stagger: 340ms; }
    .service-item:nth-child(10) { --stagger: 380ms; }

    .service-visual {
      width: 132px;
      height: 92px;
      border-radius: 10px;
      border: 1px solid #d5e7f2;
      background-repeat: no-repeat;
      background-size: 660px 216px;
      background-color: #f7fcff;
      box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.65);
      margin: 0 auto 2px;
    }

    .service-visual-1 { background-position: 0px -8px; }
    .service-visual-2 { background-position: -132px -8px; }
    .service-visual-3 { background-position: -264px -8px; }
    .service-visual-4 { background-position: -396px -8px; }
    .service-visual-5 { background-position: -528px -8px; }
    .service-visual-6 { background-position: 0px -116px; }
    .service-visual-7 { background-position: -132px -116px; }
    .service-visual-8 { background-position: -264px -116px; }
    .service-visual-9 { background-position: -396px -116px; }
    .service-visual-10 { background-position: -528px -116px; }

    .service-index {
      width: fit-content;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      font-size: 11px;
      font-weight: 800;
      letter-spacing: 0.08em;
      color: #ffffff;
      background: linear-gradient(120deg, var(--accent), var(--accent-3));
      padding: 4px 10px;
      border-radius: 999px;
      box-shadow: 0 5px 12px rgba(58, 168, 213, 0.35);
    }

    .service-text {
      font-size: 14px;
      font-weight: 700;
      line-height: 1.3;
      color: var(--ink);
    }

    .contact-band {
      margin-top: 16px;
      padding-top: 14px;
      border-top: 1px dashed #b8cddd;
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      align-items: center;
      justify-content: space-between;
    }

    .contact-left,
    .contact-right {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      align-items: center;
    }

    .contact-pill,
    .social-pill {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      gap: 7px;
      border-radius: 999px;
      border: 1px solid #bfd9e9;
      background: #ffffff;
      color: var(--accent-3);
      padding: 8px 12px;
      font-size: 13px;
      font-weight: 700;
      text-decoration: none;
      transition: transform 220ms ease, box-shadow 220ms ease, border-color 220ms ease;
    }

    .contact-pill:hover,
    .social-pill:hover {
      transform: translateY(-2px);
      box-shadow: var(--shadow-lift);
      border-color: rgba(58, 168, 213, 0.5);
    }

    .social-title {
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.1em;
      color: var(--muted);
      font-weight: 700;
      margin-right: 4px;
    }

    .dot {
      width: 7px;
      height: 7px;
      border-radius: 50%;
      background: linear-gradient(130deg, var(--accent), var(--accent-2));
      box-shadow: 0 0 0 3px rgba(58, 168, 213, 0.16);
      animation: pulseBorder 2.4s ease-in-out infinite;
    }

    .section,
    .footer {
      animation-delay: 80ms;
    }

    @keyframes revealUp {
      from {
        opacity: 0;
        transform: translateY(10px);
      }
      to {
        opacity: 1;
        transform: translateY(0);
      }
    }

    @keyframes floatBlob {
      0%, 100% { transform: translateY(0); }
      50% { transform: translateY(-10px); }
    }

    @keyframes pulseBorder {
      0%, 100% { transform: scale(1); opacity: 1; }
      50% { transform: scale(1.35); opacity: 0.6; }
    }

    @keyframes shine {
      0% { transform: translateX(-130%); }
      30% { transform: translateX(140%); }
      100% { transform: translateX(140%); }
    }

    @keyframes chipIn {
      from {
        opacity: 0;
        transform: translateY(8px) scale(0.985);
      }
      to {
        opacity: 1;
        transform: translateY(0) scale(1);
      }
    }

    @keyframes cardPulse {
      0%, 100% { box-shadow: 0 8px 20px rgba(16, 78, 107, 0.11); }
      50% { box-shadow: 0 12px 24px rgba(16, 78, 107, 0.18); }
    }

    @keyframes sweep {
      0% { transform: translateX(-130%); }
      15% { transform: translateX(140%); }
      100% { transform: translateX(140%); }
    }

    @media (max-width: 980px) {
      .company-strip {
        grid-template-columns: repeat(2, minmax(0, 1fr));
      }

      .services-grid {
        grid-template-columns: repeat(3, minmax(0, 1fr));
      }
    }

    @media (max-width: 780px) {
      .report-shell {
        width: calc(100% - 22px);
        margin: 14px auto 20px;
      }

      .hero {
        padding: 14px;
      }

      .hero-head {
        flex-direction: column;
      }

      .invoice-meta {
        width: 100%;
        min-width: 0;
      }

      .pair-grid,
      .photo-grid {
        grid-template-columns: 1fr;
      }

      .img-wrap {
        height: 220px;
      }

      .services-grid {
        grid-template-columns: repeat(2, minmax(0, 1fr));
      }

      .section,
      .footer {
        padding: 14px;
      }
    }

    @media (max-width: 520px) {
      .company-strip {
        grid-template-columns: 1fr;
      }

      .invoice-meta {
        grid-template-columns: 1fr;
      }

      .services-grid {
        grid-template-columns: 1fr;
      }

      .contact-band {
        align-items: stretch;
      }

      .contact-left,
      .contact-right {
        width: 100%;
      }

      .contact-pill,
      .social-pill {
        flex: 1 1 auto;
      }
    }

    @media (prefers-reduced-motion: reduce) {
      *,
      *::before,
      *::after {
        animation: none !important;
        transition: none !important;
      }
    }

    @page { size: A4; margin: 12mm; }
  </style>
</head>
<body>
  <main class="report-shell">
    <section class="panel hero">
      <header class="hero-head">
        <div class="brand">
          {{if .HeaderLogoURL}}
          <img class="logo" src="{{.HeaderLogoURL}}" alt="Logo IDEO Groupe" />
          {{end}}
          <div>
            <p class="eyebrow">Rapport d'intervention</p>
            <h1>{{.InterventionName}}</h1>
            <p class="address">{{.Address}}</p>
          </div>
        </div>

        <div class="invoice-meta">
          <div class="meta-tile">
            <span class="meta-label">Facture</span>
            <span class="meta-value">{{.InvoiceNumber}}</span>
          </div>
          <div class="meta-tile">
            <span class="meta-label">Date</span>
            <span class="meta-value">{{.Date}}</span>
          </div>
          <div class="meta-tile">
            <span class="meta-label">E-mail</span>
            <span class="meta-value">{{.Email}}</span>
          </div>
          <div class="meta-tile">
            <span class="meta-label">Telephone</span>
            <span class="meta-value">{{.Phone}}</span>
          </div>
        </div>
      </header>

      <div class="company-strip">
        <div class="company-card">
          <span class="meta-label">Entreprise</span>
          <span class="meta-value">{{.Company.Name}}</span>
        </div>
      </div>
    </section>

    {{if .MessageHTML}}
    <section class="panel section">
      <div class="section-head">
        <h2>Message</h2>
      </div>
      <article class="message-card">{{.MessageHTML}}</article>
    </section>
    {{end}}

    <section class="panel section">
      <div class="section-head">
        <h2>Documentation avant / apres</h2>
        <p>{{len .Pairs}} Paire(s)</p>
      </div>

      {{range .Pairs}}
      <article class="pair-card">
        <div class="{{$.PairGridClass}}">
          <div class="image-tile">
            <div class="tile-head"><span class="pill">Avant</span></div>
            <div class="img-wrap">
              <img src="{{.BeforeURL}}" alt="Photo avant {{.Index}}" />
            </div>
          </div>
          <div class="image-tile">
            <div class="tile-head"><span class="pill">Apres</span></div>
            <div class="img-wrap">
              <img src="{{.AfterURL}}" alt="Photo apres {{.Index}}" />
            </div>
          </div>
        </div>
        {{if .Caption}}<p class="caption">{{.Caption}}</p>{{end}}
        {{if $.IncludeDates}}{{if .Date}}<p class="pair-date">Date : {{.Date}}</p>{{end}}{{end}}
      </article>
      {{end}}
    </section>

    {{if .Trucks}}
    <section class="panel section">
      <div class="section-head">
        <h2>Photos camions</h2>
        <p>{{len .Trucks}} Element(s)</p>
      </div>
      <div class="photo-grid">
        {{range .Trucks}}
        <article class="image-tile">
          <div class="tile-head"><span class="pill">Camion {{.Index}}</span>{{if $.IncludeDates}}{{if .Date}}<span class="photo-date">{{.Date}}</span>{{end}}{{end}}</div>
          <div class="img-wrap">
            <img src="{{.URL}}" alt="Photo camion {{.Index}}" />
          </div>
        </article>
        {{end}}
      </div>
    </section>
    {{end}}

    {{if .Evidences}}
    <section class="panel section">
      <div class="section-head">
        <h2>Photos preuves</h2>
        <p>{{len .Evidences}} Element(s)</p>
      </div>
      <div class="photo-grid">
        {{range .Evidences}}
        <article class="image-tile">
          <div class="tile-head"><span class="pill">Preuve {{.Index}}</span>{{if $.IncludeDates}}{{if .Date}}<span class="photo-date">{{.Date}}</span>{{end}}{{end}}</div>
          <div class="img-wrap">
            <img src="{{.URL}}" alt="Photo preuve {{.Index}}" />
          </div>
        </article>
        {{end}}
      </div>
    </section>
    {{end}}

    <footer class="panel footer">
      <div class="footer-head">
        <h2>Couverture de services - Edition entreprise</h2>
        <p>Bloc services et contact inspire de votre identite visuelle, transforme en version texte animee, moderne et responsive.</p>
      </div>

      <ul class="services-grid">
        <li class="service-item"><div class="service-visual service-visual-1" style="background-image: url('{{$.FooterIconsURL}}');" aria-hidden="true"></div><span class="service-index">01</span><span class="service-text">Gestionnaire de cles</span></li>
        <li class="service-item"><div class="service-visual service-visual-2" style="background-image: url('{{$.FooterIconsURL}}');" aria-hidden="true"></div><span class="service-index">02</span><span class="service-text">Nettoyage chantier</span></li>
        <li class="service-item"><div class="service-visual service-visual-3" style="background-image: url('{{$.FooterIconsURL}}');" aria-hidden="true"></div><span class="service-index">03</span><span class="service-text">Nettoyage de cantonnements</span></li>
        <li class="service-item"><div class="service-visual service-visual-4" style="background-image: url('{{$.FooterIconsURL}}');" aria-hidden="true"></div><span class="service-index">04</span><span class="service-text">Collecte des encombrants</span></li>
        <li class="service-item"><div class="service-visual service-visual-5" style="background-image: url('{{$.FooterIconsURL}}');" aria-hidden="true"></div><span class="service-index">05</span><span class="service-text">Nettoyage sous-sol et parking</span></li>
        <li class="service-item"><div class="service-visual service-visual-6" style="background-image: url('{{$.FooterIconsURL}}');" aria-hidden="true"></div><span class="service-index">06</span><span class="service-text">Gestion des bennes</span></li>
        <li class="service-item"><div class="service-visual service-visual-7" style="background-image: url('{{$.FooterIconsURL}}');" aria-hidden="true"></div><span class="service-index">07</span><span class="service-text">Prechauffage chantier</span></li>
        <li class="service-item"><div class="service-visual service-visual-8" style="background-image: url('{{$.FooterIconsURL}}');" aria-hidden="true"></div><span class="service-index">08</span><span class="service-text">Nettoyage avant-livraison</span></li>
        <li class="service-item"><div class="service-visual service-visual-9" style="background-image: url('{{$.FooterIconsURL}}');" aria-hidden="true"></div><span class="service-index">09</span><span class="service-text">Protection parties communes</span></li>
        <li class="service-item"><div class="service-visual service-visual-10" style="background-image: url('{{$.FooterIconsURL}}');" aria-hidden="true"></div><span class="service-index">10</span><span class="service-text">Deblayage urgent</span></li>
      </ul>

      <div class="contact-band">
        <div class="contact-left">
          <a class="contact-pill" href="mailto:contact@ideogroupe.fr"><span class="dot"></span>contact@ideogroupe.fr</a>
          <a class="contact-pill" href="tel:+330188247111"><span class="dot"></span>+33 01 88 24 71 11</a>
        </div>

        <div class="contact-right">
          <span class="social-title">Reseaux</span>
          <a class="social-pill" href="https://ideogroupe.fr/" target="_blank" rel="noopener noreferrer">WEB</a>
          <a class="social-pill" href="https://fr.linkedin.com/company/ideogroupe" target="_blank" rel="noopener noreferrer">LinkedIn</a>
        </div>
      </div>
    </footer>
  </main>
</body>
</html>
`))

func resolveIdeoLogoDataURI() string {
	ideoLogoOnce.Do(func() {
		candidates := []struct {
			path string
			mime string
		}{
			{path: "assets/logo.png", mime: "image/png"},
			{path: "../../assets/logo.png", mime: "image/png"},
			{path: "assets/ideo.svg", mime: "image/svg+xml"},
			{path: "../../assets/ideo.svg", mime: "image/svg+xml"},
		}
		for _, c := range candidates {
			b, err := os.ReadFile(c.path)
			if err != nil {
				continue
			}
			ideoLogoDataURI = "data:" + c.mime + ";base64," + base64.StdEncoding.EncodeToString(b)
			break
		}
	})
	if ideoLogoDataURI == "" {
		return "assets/logo.png"
	}
	return ideoLogoDataURI
}

func resolveFooterIconsDataURI() string {
	footerIconsOnce.Do(func() {
		candidates := []string{
			"assets/footer_icons.png",
			"../../assets/footer_icons.png",
		}
		for _, p := range candidates {
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			footerIconsURI = "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)
			break
		}
	})
	if footerIconsURI == "" {
		return "assets/footer_icons.png"
	}
	return footerIconsURI
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
		Date:             time.Now().Format("2006-01-02"),
		InvoiceNumber:    models.StringOrEmpty(payload.InvoiceNumber),
		InterventionName: payload.InterventionName,
		Address:          payload.Address,
		HeaderLogoURL:    resolveIdeoLogoDataURI(),
		FooterIconsURL:   resolveFooterIconsDataURI(),
		MessageHTML:      normalizeMessageHTML(payload.Message),
		IncludeDates:     effectiveIncludeDates(payload),
		PhotoLayout:      payload.PhotoLayout,
		PairGridClass:    pairGridClass(payload.PhotoLayout),
		Company:          payload.Company,
		Email:            email,
		Phone:            phone,
		Pairs:            pairs,
		Trucks:           trucks,
		Evidences:        evidences,
	}

	var buf bytes.Buffer
	if err := reportTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
