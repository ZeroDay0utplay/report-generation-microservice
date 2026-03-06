# CODEX.md - Project Memory for Codex

## Hard Rules

- Never push to git. Never commit unless explicitly asked.
- Never add comments unless the logic is genuinely non-obvious.
- No emojis in code or output.
- Before writing any new service, handler, utility, or type, search the codebase first.
- No backward-compat shims, no dead code, no unused variables or imports.
- No over-engineering. Prefer the minimum complexity that solves the current task.

## Engineering Standards

- Keep code clean and testable (SOLID where practical).
- Keep external dependencies behind interfaces in the consumer package.
- Use constructor injection. No globals and no `init()` side effects.
- Use structured logging with `slog` JSON.
- Include `requestId` in logs and error responses.
- Keep API errors in JSON shape: `{requestId, error:{code,message,details}}`.
- Validate at HTTP boundaries; keep internals simple.
- Propagate `context.Context` through I/O paths.

## Project Snapshot (Verified in Current Code)

- Module: `pdf-html-service`
- Go version in repo: `1.24.0`
- Router: `chi/v5`
- Validation: `validator/v10`
- Storage: Backblaze B2 via AWS SDK v2 S3 client (`path-style`)
- PDF renderer: Gotenberg v8 (`chromium` convert + `pdfengines` merge)
- Job store: Redis (`REDIS_URL`) or in-memory fallback
- Logging: `slog` JSON middleware + request-level logs

Important: current PDF flow is synchronous inside `POST /v1/pdf` (chunked + parallel + merge). There is no active Asynq worker pipeline in this repo right now.

## Actual Routes

- `GET /health`
- `POST /v1/reports` -> render HTML, upload to B2, save job, return `201` (`200` on cached idempotent hit)
- `POST /v1/pdf` -> render PDF pipeline, upload to B2, save job, return `200` (cached `200` when existing `pdfUrl` found)

There is currently no wired status endpoint in `main.go`.

## Idempotency and Keys

- Job IDs are deterministic from raw request bytes: `util.JobIDFromPayload` (SHA-256 prefix).
- HTML key format: `{OUTPUT_PREFIX}/{jobId}/index.html`
- PDF key format: `{OUTPUT_PREFIX}/{jobId}/report.pdf`
- HTML cache control: `no-cache`
- PDF cache control: `public, max-age=31536000, immutable`
- Public URLs are built via `storage.PublicURL` (no presigned download flow in current code).

## PDF Pipeline (Current)

1. Validate payload and URL policy.
2. Split `pairs` into chunks (`PDF_CHUNK_SIZE`, default `50`).
3. Render each chunk HTML with:
   - cover on first chunk
   - footer (trucks/evidences) on last chunk
4. Convert chunks to PDF in parallel with bounded concurrency (`GOTENBERG_CONCURRENCY`, default `4`).
5. Per chunk retries: 3 total attempts (initial + 2 retries) with exponential backoff (1s, 2s).
6. Merge if multiple chunks.
7. Upload final PDF to B2 and return URL.

## Middleware and Guardrails

- Request ID middleware supports `X-Request-Id` / `X-Request-ID`, generates one if missing.
- Security headers enabled globally.
- Request body limited via `REQUEST_BODY_LIMIT_MB`.
- Per-IP in-memory rate limiting:
  - `/v1/reports`: `20 rps`, burst `30`
  - `/v1/pdf`: `5 rps`, burst `8`
- URL policy:
  - enforce HTTPS when `REQUIRE_HTTPS=true`
  - reject IP literals and local/internal hostnames
  - optional host allowlist via `IMAGE_HOST_ALLOWLIST`

## File Map (High Value)

- `cmd/server/main.go` - composition root and route wiring
- `internal/config/config.go` - env config + defaults
- `internal/handlers/common.go` - shared validation/error helpers/interfaces
- `internal/handlers/html.go` - `POST /v1/reports`
- `internal/handlers/pdf.go` - `POST /v1/pdf` chunked pipeline
- `internal/middleware/middleware.go` - request ID, recoverer, logging, body limit, rate limit
- `internal/security/url_policy.go` - URL safety enforcement
- `internal/render/template.go` - HTML/PDF template rendering + sanitization
- `internal/gotenberg/client.go` - convert + merge client
- `internal/storage/b2_s3.go` - B2 upload + public URL
- `internal/jobstore/{store,memory,redis}.go` - idempotent job persistence
- `internal/models/models.go` - API contract types

Note: `internal/handlers/report_html.go`, `report_pdf.go`, and `report_status.go` are placeholder files and not part of active logic.

## Environment Variables

Required:

- `B2_ENDPOINT`
- `B2_REGION`
- `B2_BUCKET`
- `B2_ACCESS_KEY_ID`
- `B2_SECRET_ACCESS_KEY`
- `B2_PUBLIC_BASE_URL`

Common optional:

- `PORT` (default `4000`)
- `MAX_PAIRS` (default `200`)
- `REQUEST_BODY_LIMIT_MB` (default `2`)
- `REQUIRE_HTTPS` (default `true`)
- `IMAGE_HOST_ALLOWLIST` (CSV)
- `GOTENBERG_URL` (default `http://gotenberg:8090`)
- `UPLOAD_HTML_ON_PDF` (default `false`)
- `OUTPUT_PREFIX` (default `docs`)
- `LOG_LEVEL` (default `info`)
- `LOGO_URL` (default value in config)
- `REDIS_URL` (empty -> memory store)
- `PDF_CHUNK_SIZE` (default `50`)
- `GOTENBERG_CONCURRENCY` (default `4`)
- `CHUNK_TIMEOUT_SEC` (default `90`)
- `MERGE_TIMEOUT_SEC` (default `120`)
- `PUBLIC_BASE_URL` (currently loaded but not used in handlers)

## Dev Workflow

- Run server: `make run`
- Run tests: `make test`
- Lint/check: `make lint` (`gofmt -w .` + `go vet ./...`)
- Docker stack: `make docker-up` / `make docker-down`

When changing handlers, always update/add tests in `internal/handlers/*_test.go`.

## Known Drift to Watch

- `CLAUDE.md` and `README.md` contain some stale details (for example `/v1/html` docs and async worker wording).
- Treat runtime behavior in code and tests as source of truth unless docs are updated in the same change.
