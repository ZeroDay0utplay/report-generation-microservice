# pdf-html-service

`pdf-html-service` is a Go microservice that generates premium report HTML and PDF artifacts from a shared payload.

- `POST /v1/html`: render HTML and upload to Backblaze B2
- `POST /v1/pdf`: render same HTML, convert via Gotenberg Chromium, upload PDF to Backblaze B2
- `GET /health`: health check

Both generation routes share one HTML template to guarantee visual consistency.

## Stack

- Go 1.22+
- Router: `go-chi/chi`
- Validation: `go-playground/validator/v10`
- Storage: AWS SDK v2 S3 client (Backblaze B2 S3-compatible endpoint)
- PDF rendering: Gotenberg (`/forms/chromium/convert/html`)
- Logging: `log/slog` JSON logs
- Tests: `go test ./...`

## Environment Variables

Required:

- `B2_ENDPOINT`
- `B2_REGION`
- `B2_BUCKET`
- `B2_ACCESS_KEY_ID`
- `B2_SECRET_ACCESS_KEY`
- `B2_PUBLIC_BASE_URL`

Optional with defaults:

- `PORT` (default: `4000`)
- `MAX_PAIRS` (default: `200`)
- `REQUEST_BODY_LIMIT_MB` (default: `2`)
- `REQUIRE_HTTPS` (default: `true`)
- `IMAGE_HOST_ALLOWLIST` (optional CSV)
- `GOTENBERG_URL` (default: `http://gotenberg:4000`)
- `UPLOAD_HTML_ON_PDF` (default: `false`)
- `OUTPUT_PREFIX` (default: `docs`)
- `LOG_LEVEL` (default: `info`)

Copy `.env.example` to `.env` and fill values.

## Local Development

1. Create env file:

```bash
cp .env.example .env
# edit .env with real Backblaze credentials
```

2. Run with Docker Compose:

```bash
docker compose up --build
```

3. Test health:

```bash
curl -s http://localhost:4000/health
```

### Services in `docker-compose.yml`

- `gotenberg`: `gotenberg/gotenberg:8`, internal port `4000`
- `orchestrator`: this Go service, exposed on host `:4000`

## API

### `POST /v1/html`

Returns:

```json
{
  "requestId": "req_...",
  "jobId": "job_...",
  "htmlKey": "docs/<jobId>/index.html",
  "htmlUrl": "https://.../docs/<jobId>/index.html"
}
```

### `POST /v1/pdf`

Returns:

```json
{
  "requestId": "req_...",
  "jobId": "job_...",
  "pdfKey": "docs/<jobId>/report.pdf",
  "pdfUrl": "https://.../docs/<jobId>/report.pdf",
  "debug": {
    "htmlKey": "docs/<jobId>/index.html",
    "htmlUrl": "https://.../docs/<jobId>/index.html"
  }
}
```

`debug` is included only when `UPLOAD_HTML_ON_PDF=true`.

### Error Shape

```json
{
  "requestId": "req_...",
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "payload validation failed",
    "details": []
  }
}
```

## Curl Examples

Use the same payload for both routes:

```bash
cat > /tmp/report.json <<'JSON'
{
  "invoiceNumber": "INV-2026-0001",
  "interventionName": "Kitchen renovation",
  "address": "123 Main St, Tunis",
  "company": {
    "name": "ACME Services",
    "contact": "+216 00 000 000",
    "email": "hello@acme.tn",
    "phone": "+216 11 111 111",
    "website": "https://acme.tn",
    "logoUrl": "https://cdn.example.com/logo.png"
  },
  "message": "<p><strong>Rapport</strong> valide.</p>",
  "includeDates": true,
  "photoLayout": "one_by_row",
  "pairs": [
    {
      "beforeUrl": "https://cdn.example.com/before1.jpg",
      "afterUrl": "https://cdn.example.com/after1.jpg",
      "date": "2026-02-20",
      "caption": "Angle 1"
    }
  ],
  "trucks": [
    {
      "url": "https://cdn.example.com/truck1.jpg",
      "date": "2026-02-21"
    }
  ],
  "evidences": [
    {
      "url": "https://cdn.example.com/evidence1.jpg",
      "date": "2026-02-22"
    }
  ]
}
JSON
```

Generate HTML:

```bash
curl -sS -X POST http://localhost:4000/v1/html \
  -H 'Content-Type: application/json' \
  --data @/tmp/report.json
```

Generate PDF:

```bash
curl -sS -X POST http://localhost:4000/v1/pdf \
  -H 'Content-Type: application/json' \
  --data @/tmp/report.json
```

## Render Deployment

Deploy two services:

1. **Gotenberg service**
- Create a Render Web Service using image: `gotenberg/gotenberg:8`
- Internal port: `4000`
- Keep it private/internal if possible

2. **Orchestrator service** (this repo)
- Deploy with `docker/Dockerfile`
- Set all env vars from `.env.example`
- Set `GOTENBERG_URL` to the internal URL of the Render Gotenberg service

## Backblaze B2 Notes

- For testing, you can use a public bucket and a `B2_PUBLIC_BASE_URL` that serves objects directly.
- Future improvement: switch to signed URLs for tighter access control and time-limited downloads.

## Commands

```bash
make run
make test
make lint
make docker-up
make docker-down
```
# report-generation-microservice
