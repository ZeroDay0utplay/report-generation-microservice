# pdf-html-service

`pdf-html-service` is a Go microservice that generates report HTML/PDF artifacts and stores them in Backblaze B2 (S3-compatible).

## Routes

- `GET /health`
- `POST /v1/reports` -> synchronous HTML generation/upload
- `GET /v1/reports/{id}` -> job status
- `POST /v1/pdf` -> asynchronous PDF generation with 10s synchronous wait window
- `POST /v1/pdf/recipients` -> register recipient emails for a PDF job

## Async PDF flow

1. Client posts report payload to `POST /v1/pdf`.
2. Service persists/reuses a durable PDF job and enqueues processing.
3. Service waits up to `PDF_SYNC_WAIT_SEC` (default 10s):
- Completed within window -> `200` with `status=completed` and `pdfUrl`.
- Still running after window -> `202` with `status=processing` and `jobId`.
4. Client posts `{jobId, emails}` to `POST /v1/pdf/recipients`.
5. Recipients are persisted immediately (`200`), then delivery is triggered:
- immediate if PDF is already completed,
- deferred automatically if PDF is still processing.

## Job states

PDF processing states:
- `queued`
- `processing`
- `completed`
- `failed`

Email delivery states:
- `none`
- `registered`
- `pending`
- `sending`
- `sent`
- `failed`

## Environment variables

Required:
- `B2_ENDPOINT`
- `B2_REGION`
- `B2_BUCKET`
- `B2_ACCESS_KEY_ID`
- `B2_SECRET_ACCESS_KEY`
- `B2_PUBLIC_BASE_URL`

Optional:
- `PORT` (default: `4000`)
- `MAX_PAIRS` (default: `200`)
- `REQUEST_BODY_LIMIT_MB` (default: `2`)
- `REQUIRE_HTTPS` (default: `true`)
- `IMAGE_HOST_ALLOWLIST` (CSV)
- `GOTENBERG_URL` (default: `http://gotenberg:8090`)
- `UPLOAD_HTML_ON_PDF` (default: `false`)
- `OUTPUT_PREFIX` (default: `docs`)
- `LOG_LEVEL` (default: `info`)
- `LOGO_URL` (default in config)
- `REDIS_URL` (empty uses in-memory store)
- `PDF_WORKER_COUNT` (default: `4`)
- `PDF_QUEUE_SIZE` (default: `128`)
- `PDF_SYNC_WAIT_SEC` (default: `10`)
- `EMAIL_WORKER_COUNT` (default: `2`)
- `EMAIL_QUEUE_SIZE` (default: `128`)
- `EMAIL_WEBHOOK_URL` (optional notifier webhook)

## API examples

### `POST /v1/pdf` completed within wait window

```json
{
  "requestId": "req_...",
  "jobId": "job_...",
  "status": "completed",
  "pdfUrl": "https://.../docs/<jobId>/report.pdf"
}
```

### `POST /v1/pdf` still processing

```json
{
  "requestId": "req_...",
  "jobId": "job_...",
  "status": "processing"
}
```

### `POST /v1/pdf/recipients`

Request:

```json
{
  "jobId": "job_...",
  "emails": ["a@example.com", "b@example.com"]
}
```

Response:

```json
{
  "requestId": "req_...",
  "jobId": "job_...",
  "accepted": ["a@example.com", "b@example.com"],
  "totalRecipients": 2,
  "emailStatus": "registered",
  "status": "processing"
}
```

### Error shape

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

## Operational behavior

- PDF processing and email delivery use bounded worker pools with bounded queues.
- Queue saturation returns `503` with `QUEUE_FULL`.
- Recipients are deduplicated case-insensitively.
- Startup recovery re-queues `queued/processing` PDF jobs and pending email deliveries.
- Redis-backed deployments persist job state across request/worker timing gaps.

## Commands

```bash
make run
make test
make lint
make docker-up
make docker-down
```
