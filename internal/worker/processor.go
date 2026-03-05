package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/hibiken/asynq"

	"pdf-html-service/internal/jobstore"
	"pdf-html-service/internal/models"
	"pdf-html-service/internal/render"
)

const maxChunkRetries = 3

// GotenbergConverter abstracts the Gotenberg client for testability.
type GotenbergConverter interface {
	ConvertHTMLToPDF(ctx context.Context, html string) (io.ReadCloser, error)
	MergePDFs(ctx context.Context, chunks [][]byte) (io.ReadCloser, error)
}

// PDFStorage abstracts upload + presign operations for testability.
type PDFStorage interface {
	UploadPDF(ctx context.Context, key string, r io.Reader) error
	PresignGetURL(ctx context.Context, key string, ttl time.Duration) (string, error)
}

// Processor implements the Asynq handler for TypePDFGenerate.
type Processor struct {
	store        jobstore.Store
	gotenberg    GotenbergConverter
	storage      PDFStorage
	httpClient   *http.Client
	logoURL      string
	outputPrefix string
	chunkSize    int
	concurrency  int
	chunkTimeout time.Duration
	mergeTimeout time.Duration
	downloadTTL  time.Duration
	smtp         SMTPConfig
	logger       *slog.Logger
}

// ProcessorConfig bundles all Processor constructor parameters.
type ProcessorConfig struct {
	Store        jobstore.Store
	Gotenberg    GotenbergConverter
	Storage      PDFStorage
	HTTPClient   *http.Client
	LogoURL      string
	OutputPrefix string
	ChunkSize    int
	Concurrency  int
	ChunkTimeout time.Duration
	MergeTimeout time.Duration
	DownloadTTL  time.Duration
	SMTP         SMTPConfig
	Logger       *slog.Logger
}

func NewProcessor(cfg ProcessorConfig) *Processor {
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = 50
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
	}
	if cfg.ChunkTimeout <= 0 {
		cfg.ChunkTimeout = 90 * time.Second
	}
	if cfg.MergeTimeout <= 0 {
		cfg.MergeTimeout = 120 * time.Second
	}
	if cfg.DownloadTTL <= 0 {
		cfg.DownloadTTL = 24 * time.Hour
	}
	return &Processor{
		store:        cfg.Store,
		gotenberg:    cfg.Gotenberg,
		storage:      cfg.Storage,
		httpClient:   cfg.HTTPClient,
		logoURL:      cfg.LogoURL,
		outputPrefix: cfg.OutputPrefix,
		chunkSize:    cfg.ChunkSize,
		concurrency:  cfg.Concurrency,
		chunkTimeout: cfg.ChunkTimeout,
		mergeTimeout: cfg.MergeTimeout,
		downloadTTL:  cfg.DownloadTTL,
		smtp:         cfg.SMTP,
		logger:       cfg.Logger,
	}
}

// ProcessTask is the Asynq handler entrypoint.
func (p *Processor) ProcessTask(ctx context.Context, t *asynq.Task) error {
	start := time.Now()

	var tp TaskPayload
	if err := json.Unmarshal(t.Payload(), &tp); err != nil {
		return fmt.Errorf("unmarshal task payload: %w", asynq.SkipRetry)
	}

	var req models.ReportRequest
	if err := json.Unmarshal(tp.RawBody, &req); err != nil {
		return fmt.Errorf("unmarshal report request: %w", asynq.SkipRetry)
	}

	p.logger.Info("pdf job started", slog.String("jobId", tp.JobID))

	// Mark processing
	if err := p.setStatus(ctx, tp.JobID, tp.RawBody, "processing", "", ""); err != nil {
		return fmt.Errorf("update job to processing: %w", err) // retriable
	}

	downloadURL, pdfSizeBytes, err := p.generate(ctx, tp.JobID, req)
	if err != nil {
		p.logger.Error("pdf job failed",
			slog.String("jobId", tp.JobID),
			slog.String("error", err.Error()),
			slog.Int64("totalDuration_ms", time.Since(start).Milliseconds()),
		)
		_ = p.setStatus(ctx, tp.JobID, tp.RawBody, "failed", "", err.Error())
		p.notify(req, tp.JobID, "failed", "", err.Error())
		return fmt.Errorf("%w: %s", asynq.SkipRetry, err.Error())
	}

	_ = p.setStatus(ctx, tp.JobID, tp.RawBody, "done", downloadURL, "")
	p.notify(req, tp.JobID, "done", downloadURL, "")

	p.logger.Info("pdf job done",
		slog.String("jobId", tp.JobID),
		slog.Int64("pdfSizeBytes", pdfSizeBytes),
		slog.Int64("totalDuration_ms", time.Since(start).Milliseconds()),
	)
	return nil
}

// generate runs the full chunked pipeline and returns the presigned download URL.
func (p *Processor) generate(ctx context.Context, jobID string, req models.ReportRequest) (string, int64, error) {
	chunks := BuildChunks(req, p.chunkSize)

	pdfChunks, err := p.convertChunksConcurrently(ctx, jobID, req, chunks)
	if err != nil {
		return "", 0, fmt.Errorf("convert chunks: %w", err)
	}

	// Merge step (or pass-through for a single chunk).
	var finalPDF []byte
	if len(pdfChunks) == 1 {
		finalPDF = pdfChunks[0]
	} else {
		mergeCtx, cancel := context.WithTimeout(ctx, p.mergeTimeout)
		defer cancel()

		reader, err := p.gotenberg.MergePDFs(mergeCtx, pdfChunks)
		if err != nil {
			return "", 0, fmt.Errorf("merge pdfs: %w", err)
		}
		defer reader.Close()

		finalPDF, err = io.ReadAll(reader)
		if err != nil {
			return "", 0, fmt.Errorf("read merged pdf: %w", err)
		}
	}

	// Upload
	pdfKey := fmt.Sprintf("%s/%s/report.pdf", p.outputPrefix, jobID)
	if err := p.storage.UploadPDF(ctx, pdfKey, bytes.NewReader(finalPDF)); err != nil {
		return "", 0, fmt.Errorf("upload pdf: %w", err)
	}

	// Presign
	url, err := p.storage.PresignGetURL(ctx, pdfKey, p.downloadTTL)
	if err != nil {
		return "", 0, fmt.Errorf("presign url: %w", err)
	}

	return url, int64(len(finalPDF)), nil
}

// convertChunksConcurrently sends each chunk to Gotenberg with bounded parallelism.
// Results are collected in order. A single chunk failure cancels remaining work.
func (p *Processor) convertChunksConcurrently(
	ctx context.Context,
	jobID string,
	req models.ReportRequest,
	chunks []Chunk,
) ([][]byte, error) {
	results := make([][]byte, len(chunks))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, p.concurrency)
	var wg sync.WaitGroup
	errOnce := &sync.Once{}
	var firstErr error

	for i, chunk := range chunks {
		wg.Add(1)
		go func(idx int, c Chunk) {
			defer wg.Done()

			select {
			case sem <- struct{}{}: // acquire
			case <-ctx.Done():
				errOnce.Do(func() { firstErr = ctx.Err() })
				return
			}
			defer func() { <-sem }()

			pdf, err := p.convertChunkWithRetry(ctx, jobID, req, c)
			if err != nil {
				errOnce.Do(func() {
					firstErr = fmt.Errorf("chunk %d: %w", idx, err)
					cancel() // stop remaining goroutines
				})
				return
			}
			results[idx] = pdf
		}(i, chunk)
	}

	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

// convertChunkWithRetry renders + converts one chunk with up to maxChunkRetries attempts.
func (p *Processor) convertChunkWithRetry(
	ctx context.Context,
	jobID string,
	req models.ReportRequest,
	c Chunk,
) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < maxChunkRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		chunkCtx, cancel := context.WithTimeout(ctx, p.chunkTimeout)
		pdf, err := p.convertChunk(chunkCtx, jobID, req, c, attempt)
		cancel()

		if err == nil {
			return pdf, nil
		}
		lastErr = err
		p.logger.Warn("chunk attempt failed",
			slog.String("jobId", jobID),
			slog.Int("chunkIndex", c.Index),
			slog.Int("totalChunks", c.Total),
			slog.Int("attempt", attempt+1),
			slog.String("error", err.Error()),
		)
	}
	return nil, fmt.Errorf("all %d attempts failed for chunk %d: %w", maxChunkRetries, c.Index, lastErr)
}

// convertChunk renders the HTML for one chunk and converts it to PDF bytes.
func (p *Processor) convertChunk(
	ctx context.Context,
	jobID string,
	req models.ReportRequest,
	c Chunk,
	attempt int,
) ([]byte, error) {
	start := time.Now()

	// Build the modified request for this chunk.
	chunkReq := req
	chunkReq.Pairs = c.Pairs
	chunkReq.Trucks = c.Trucks
	chunkReq.Evidences = c.Evidences

	html, err := render.RenderPDFChunk(chunkReq, p.logoURL, c.ShowCover, c.ShowFooter)
	if err != nil {
		return nil, fmt.Errorf("render: %w", err)
	}

	reader, err := p.gotenberg.ConvertHTMLToPDF(ctx, html)
	if err != nil {
		return nil, fmt.Errorf("gotenberg convert: %w", err)
	}
	defer reader.Close()

	pdf, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read pdf: %w", err)
	}

	p.logger.Info("chunk converted",
		slog.String("jobId", jobID),
		slog.Int("chunkIndex", c.Index),
		slog.Int("totalChunks", c.Total),
		slog.Int("attempt", attempt+1),
		slog.Int64("chunkDuration_ms", time.Since(start).Milliseconds()),
	)
	return pdf, nil
}

// setStatus updates the job's status in the store.
func (p *Processor) setStatus(ctx context.Context, jobID string, rawBody json.RawMessage, status, downloadURL, errMsg string) error {
	job := jobstore.Job{
		ID:        jobID,
		Status:    status,
		PDFURL:    downloadURL,
		Error:     errMsg,
		Payload:   rawBody,
		CreatedAt: time.Now(),
	}
	return p.store.Update(ctx, job)
}

// notify fires webhook and/or email if the request payload has a Callback.
func (p *Processor) notify(req models.ReportRequest, jobID, status, downloadURL, errMsg string) {
	if req.Callback == nil {
		return
	}
	wp := WebhookPayload{
		JobID:       jobID,
		Status:      status,
		DownloadURL: downloadURL,
		Error:       errMsg,
	}
	if req.Callback.WebhookURL != "" {
		if err := PostWebhook(context.Background(), p.httpClient, req.Callback.WebhookURL, wp); err != nil {
			p.logger.Warn("webhook delivery failed",
				slog.String("jobId", jobID),
				slog.String("webhookUrl", req.Callback.WebhookURL),
				slog.String("error", err.Error()),
			)
		}
	}
	if req.Callback.Email != "" {
		subject := fmt.Sprintf("[IDEO] Rapport PDF %s — %s", jobID, status)
		var body string
		if status == "done" {
			body = fmt.Sprintf("Votre rapport PDF est prêt.\n\nTélécharger : %s\n\nCe lien expire dans 24 heures.", downloadURL)
		} else {
			body = fmt.Sprintf("La génération du rapport PDF a échoué.\n\nErreur : %s", errMsg)
		}
		if err := SendEmail(p.smtp, req.Callback.Email, subject, body); err != nil {
			p.logger.Warn("email delivery failed",
				slog.String("jobId", jobID),
				slog.String("email", req.Callback.Email),
				slog.String("error", err.Error()),
			)
		}
	}
}
