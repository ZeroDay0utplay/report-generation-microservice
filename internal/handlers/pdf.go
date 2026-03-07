package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-playground/validator/v10"

	"pdf-html-service/internal/gotenberg"
	"pdf-html-service/internal/jobstore"
	"pdf-html-service/internal/models"
	"pdf-html-service/internal/render"
	"pdf-html-service/internal/security"
	"pdf-html-service/internal/util"
)

type PDFHandler struct {
	baseHandler
	store            jobstore.Store
	storage          Storage
	renderer         PDFRenderer
	outputPrefix     string
	uploadHTMLOnPDF  bool
	logoURL          string
	chunkSize        int
	concurrency      int
	chunkTimeout     time.Duration
	mergeTimeout     time.Duration
	jobLockTTL       time.Duration
	jobWaitPollDelay time.Duration
}

func NewPDFHandler(
	logger *slog.Logger,
	validate *validator.Validate,
	urlPolicy *security.URLPolicy,
	store jobstore.Store,
	storage Storage,
	renderer PDFRenderer,
	maxPairs int,
	outputPrefix string,
	uploadHTMLOnPDF bool,
	logoURL string,
	chunkSize int,
	concurrency int,
	chunkTimeout time.Duration,
	mergeTimeout time.Duration,
	jobLockTTL time.Duration,
	jobWaitPollDelay time.Duration,
) *PDFHandler {
	return &PDFHandler{
		baseHandler:      baseHandler{logger: logger, validate: validate, urlPolicy: urlPolicy, maxPairs: maxPairs},
		store:            store,
		storage:          storage,
		renderer:         renderer,
		outputPrefix:     outputPrefix,
		uploadHTMLOnPDF:  uploadHTMLOnPDF,
		logoURL:          logoURL,
		chunkSize:        chunkSize,
		concurrency:      concurrency,
		chunkTimeout:     chunkTimeout,
		mergeTimeout:     mergeTimeout,
		jobLockTTL:       jobLockTTL,
		jobWaitPollDelay: jobWaitPollDelay,
	}
}

func (h *PDFHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		h.logger.Warn("pdf request rejected",
			slog.String("requestId", requestID(r)),
			slog.String("route", r.URL.Path),
			slog.String("errorCode", "INVALID_JSON"),
		)
		writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "request body is required", nil)
		return
	}

	payload, reqAttrs, ok := h.validateReportPayload(w, r, body)
	if !ok {
		return
	}

	jobID, err := util.JobIDFromReportRequest(*payload)
	if err != nil {
		h.logger.Error("pdf: failed to compute job id",
			append(reqAttrs, slog.String("error", err.Error()))...,
		)
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to compute job id", nil)
		return
	}
	reqAttrs = append(reqAttrs, slog.String("jobId", jobID))

	existing, err := h.readJob(r.Context(), jobID)
	if err != nil {
		h.logger.Error("pdf: failed to load existing job", append(reqAttrs, slog.String("error", err.Error()))...)
		writeError(w, r, http.StatusInternalServerError, "STORE_ERROR", "failed to read existing job", nil)
		return
	}
	if existing.PDFURL != "" && existing.Status == statusReady {
		h.respondWithCachedJob(w, r, jobID, existing, reqAttrs)
		return
	}

	lockKey := pdfLockKey(jobID)
	lockOwner := requestID(r)
	acquired, err := h.store.AcquireLock(r.Context(), lockKey, lockOwner, h.jobLockTTL)
	if err != nil {
		h.logger.Error("pdf: failed to acquire lock",
			append(reqAttrs, slog.String("error", err.Error()))...,
		)
		writeError(w, r, http.StatusInternalServerError, "STORE_ERROR", "failed to coordinate duplicate requests", nil)
		return
	}

	if !acquired {
		waitCtx, cancel := context.WithTimeout(r.Context(), h.jobLockTTL)
		defer cancel()

		job, waitErr := h.waitForTerminalState(waitCtx, jobID)
		if waitErr != nil {
			h.logger.Warn("pdf: duplicate request still processing",
				append(reqAttrs, slog.String("error", waitErr.Error()))...,
			)
			writeError(w, r, http.StatusConflict, "JOB_IN_PROGRESS", "report generation already in progress", nil)
			return
		}
		if job.Status == statusReady && job.PDFURL != "" {
			h.respondWithCachedJob(w, r, jobID, job, reqAttrs)
			return
		}
		if job.Status == statusFailed {
			code := job.ErrorCode
			if code == "" {
				code = "PDF_PIPELINE_ERROR"
			}
			message := job.ErrorMsg
			if message == "" {
				message = "PDF generation failed"
			}
			writeError(w, r, http.StatusInternalServerError, code, message, nil)
			return
		}

		writeError(w, r, http.StatusConflict, "JOB_IN_PROGRESS", "report generation already in progress", nil)
		return
	}

	defer func() {
		if releaseErr := h.store.ReleaseLock(context.Background(), lockKey, lockOwner); releaseErr != nil {
			h.logger.Error("pdf: failed to release lock",
				append(reqAttrs, slog.String("error", releaseErr.Error()))...,
			)
		}
	}()

	existing, err = h.readJob(r.Context(), jobID)
	if err != nil {
		h.logger.Error("pdf: failed to refresh existing job", append(reqAttrs, slog.String("error", err.Error()))...)
		writeError(w, r, http.StatusInternalServerError, "STORE_ERROR", "failed to read existing job", nil)
		return
	}
	if existing.PDFURL != "" && existing.Status == statusReady {
		h.respondWithCachedJob(w, r, jobID, existing, reqAttrs)
		return
	}

	now := time.Now()
	job := existing
	job.ID = jobID
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now
	job.Status = statusProcessing
	job.ErrorCode = ""
	job.ErrorMsg = ""
	if _, err := h.store.Update(r.Context(), job); err != nil {
		h.logger.Error("pdf: failed to mark job processing",
			append(reqAttrs, slog.String("error", err.Error()))...,
		)
		writeError(w, r, http.StatusInternalServerError, "STORE_ERROR", "failed to initialize job status", nil)
		return
	}

	chunks := buildPDFChunks(payload.Pairs, h.chunkSize)
	h.logger.Info("pdf: starting pipeline",
		append(reqAttrs, slog.Int("chunks", len(chunks)))...,
	)

	var debug *models.PDFDebug
	if h.uploadHTMLOnPDF {
		htmlDoc, err := render.RenderPDFHTMLWithLogo(*payload, h.logoURL)
		if err != nil {
			h.failJob(r.Context(), reqAttrs, job, "RENDER_ERROR", "failed to render HTML", err)
			writeError(w, r, http.StatusInternalServerError, "RENDER_ERROR", "failed to render HTML", nil)
			return
		}
		htmlKey := htmlObjectKey(h.outputPrefix, jobID)
		if err := h.storage.UploadHTML(r.Context(), htmlKey, htmlDoc); err != nil {
			h.failJob(r.Context(), reqAttrs, job, "UPLOAD_ERROR", "failed to upload debug HTML", err)
			writeError(w, r, http.StatusInternalServerError, "UPLOAD_ERROR", "failed to upload debug HTML", nil)
			return
		}
		debug = &models.PDFDebug{HTMLKey: htmlKey, HTMLURL: h.storage.PublicURL(htmlKey)}
	}

	pipelineStart := time.Now()
	chunkArtifacts, err := h.processChunks(r.Context(), *payload, chunks, reqAttrs, jobID)
	if err != nil {
		h.failJob(r.Context(), reqAttrs, job, "PDF_PIPELINE_ERROR", "PDF generation failed", err)
		writeError(w, r, http.StatusInternalServerError, "PDF_PIPELINE_ERROR", "PDF generation failed", nil)
		return
	}
	defer cleanupChunkArtifacts(chunkArtifacts)

	pdfKey := pdfObjectKey(h.outputPrefix, jobID)
	uploadStart := time.Now()
	if err := h.uploadFinalPDF(r.Context(), pdfKey, chunkArtifacts); err != nil {
		h.failJob(r.Context(), reqAttrs, job, "UPLOAD_ERROR", "failed to upload PDF", err)
		writeError(w, r, http.StatusInternalServerError, "UPLOAD_ERROR", "failed to upload PDF", nil)
		return
	}
	uploadMS := time.Since(uploadStart).Milliseconds()
	pipelineMS := time.Since(pipelineStart).Milliseconds()

	pdfURL, err := h.storage.DownloadURL(r.Context(), pdfKey)
	if err != nil {
		h.failJob(r.Context(), reqAttrs, job, "UPLOAD_ERROR", "failed to build PDF URL", err)
		writeError(w, r, http.StatusInternalServerError, "UPLOAD_ERROR", "failed to build PDF URL", nil)
		return
	}

	job.Status = statusReady
	job.PDFURL = pdfURL
	job.ErrorCode = ""
	job.ErrorMsg = ""
	job.UpdatedAt = time.Now()
	updated, err := h.store.Update(r.Context(), job)
	if err != nil {
		h.logger.Error("pdf: failed to persist final status",
			append(reqAttrs, slog.String("error", err.Error()))...,
		)
		writeError(w, r, http.StatusInternalServerError, "STORE_ERROR", "failed to persist final status", nil)
		return
	}

	h.logger.Info("pdf report generated",
		append(reqAttrs,
			slog.Int("chunks", len(chunks)),
			slog.Int64("pipeline_ms", pipelineMS),
			slog.Int64("upload_ms", uploadMS),
		)...,
	)

	writeJSON(w, http.StatusOK, models.PDFResponse{
		RequestID: requestID(r),
		JobID:     updated.ID,
		Status:    updated.Status,
		PDFKey:    pdfKey,
		URL:       pdfURL,
		Debug:     debug,
	})
}

type pdfChunk struct {
	pairs       []models.Pair
	isFirst     bool
	isLast      bool
	indexOffset int
	totalPairs  int
}

type chunkArtifact struct {
	path string
	size int64
}

func buildPDFChunks(pairs []models.Pair, chunkSize int) []pdfChunk {
	if chunkSize <= 0 || len(pairs) <= chunkSize {
		return []pdfChunk{{
			pairs:      pairs,
			isFirst:    true,
			isLast:     true,
			totalPairs: len(pairs),
		}}
	}

	total := len(pairs)
	chunks := make([]pdfChunk, 0, (total+chunkSize-1)/chunkSize)
	for i := 0; i < total; i += chunkSize {
		end := i + chunkSize
		if end > total {
			end = total
		}
		chunks = append(chunks, pdfChunk{
			pairs:       pairs[i:end],
			isFirst:     i == 0,
			isLast:      end == total,
			indexOffset: i,
			totalPairs:  total,
		})
	}
	return chunks
}

func (h *PDFHandler) processChunks(ctx context.Context, payload models.ReportRequest, chunks []pdfChunk, logAttrs []any, jobID string) ([]chunkArtifact, error) {
	workerCount := h.concurrency
	if workerCount <= 0 {
		workerCount = 1
	}
	if workerCount > len(chunks) {
		workerCount = len(chunks)
	}

	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type chunkResult struct {
		index    int
		artifact chunkArtifact
		err      error
	}

	tasks := make(chan int)
	results := make(chan chunkResult, len(chunks))

	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range tasks {
				chunk := chunks[idx]

				html, err := render.RenderPDFChunk(payload, h.logoURL, chunk.pairs, render.ChunkOpts{
					ShowCover:   chunk.isFirst,
					ShowFooter:  chunk.isLast,
					IndexOffset: chunk.indexOffset,
					TotalPairs:  chunk.totalPairs,
				})
				if err != nil {
					results <- chunkResult{index: idx, err: fmt.Errorf("render chunk %d: %w", idx, err)}
					cancel()
					return
				}

				artifact, err := h.convertWithRetry(workCtx, html, idx, jobID)
				if err != nil {
					results <- chunkResult{index: idx, err: err}
					cancel()
					return
				}

				results <- chunkResult{index: idx, artifact: artifact}

				h.logger.Debug("chunk converted",
					append(logAttrs,
						slog.Int("chunk", idx),
						slog.Int64("pdfBytes", artifact.size),
					)...,
				)
			}
		}()
	}

	go func() {
		defer close(tasks)
		for idx := range chunks {
			select {
			case <-workCtx.Done():
				return
			case tasks <- idx:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	artifacts := make([]chunkArtifact, len(chunks))
	var firstErr error

	for res := range results {
		if res.err != nil {
			if firstErr == nil {
				firstErr = res.err
			}
			continue
		}
		artifacts[res.index] = res.artifact
	}

	if firstErr != nil {
		cleanupChunkArtifacts(artifacts)
		return nil, firstErr
	}

	for i := range artifacts {
		if artifacts[i].path == "" {
			cleanupChunkArtifacts(artifacts)
			return nil, fmt.Errorf("chunk %d did not produce output", i)
		}
	}

	return artifacts, nil
}

const maxChunkRetries = 2

func (h *PDFHandler) convertWithRetry(ctx context.Context, html string, chunkIdx int, jobID string) (chunkArtifact, error) {
	var lastErr error

	for attempt := 0; attempt <= maxChunkRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(1<<(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return chunkArtifact{}, ctx.Err()
			case <-time.After(delay):
			}
		}

		convertCtx, cancel := context.WithTimeout(ctx, h.chunkTimeout)
		reader, err := h.renderer.ConvertHTMLToPDF(convertCtx, html)
		if err != nil {
			cancel()
			lastErr = fmt.Errorf("chunk %d attempt %d: %w", chunkIdx, attempt+1, err)
			continue
		}

		artifact, err := writeChunkToTempFile(reader, chunkIdx, jobID)
		reader.Close()
		cancel()
		if err != nil {
			lastErr = fmt.Errorf("chunk %d attempt %d: %w", chunkIdx, attempt+1, err)
			continue
		}

		return artifact, nil
	}

	return chunkArtifact{}, fmt.Errorf("chunk %d failed after %d attempts: %w", chunkIdx, maxChunkRetries+1, lastErr)
}

func writeChunkToTempFile(reader io.Reader, chunkIdx int, jobID string) (chunkArtifact, error) {
	f, err := os.CreateTemp("", fmt.Sprintf("report-%s-%03d-*.pdf", jobID, chunkIdx))
	if err != nil {
		return chunkArtifact{}, fmt.Errorf("create temp chunk file: %w", err)
	}

	size, copyErr := io.Copy(f, reader)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(f.Name())
		return chunkArtifact{}, fmt.Errorf("write temp chunk file: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(f.Name())
		return chunkArtifact{}, fmt.Errorf("close temp chunk file: %w", closeErr)
	}

	return chunkArtifact{path: f.Name(), size: size}, nil
}

func cleanupChunkArtifacts(artifacts []chunkArtifact) {
	for _, artifact := range artifacts {
		if artifact.path == "" {
			continue
		}
		_ = os.Remove(artifact.path)
	}
}

func (h *PDFHandler) uploadFinalPDF(ctx context.Context, pdfKey string, artifacts []chunkArtifact) error {
	if len(artifacts) == 1 {
		f, err := os.Open(artifacts[0].path)
		if err != nil {
			return fmt.Errorf("open chunk file: %w", err)
		}
		defer f.Close()
		if err := h.storage.UploadPDF(ctx, pdfKey, f); err != nil {
			return err
		}
		return nil
	}

	files := make([]*os.File, len(artifacts))
	readers := make([]io.Reader, len(artifacts))
	for i := range artifacts {
		f, err := os.Open(artifacts[i].path)
		if err != nil {
			closeFiles(files)
			return fmt.Errorf("open chunk %d for merge: %w", i, err)
		}
		files[i] = f
		readers[i] = f
	}
	defer closeFiles(files)

	mergeCtx, cancel := context.WithTimeout(ctx, h.mergeTimeout)
	defer cancel()

	merged, err := h.renderer.MergePDFs(mergeCtx, readers)
	if err != nil {
		return fmt.Errorf("merge pdf chunks: %w", err)
	}
	defer merged.Close()

	if err := h.storage.UploadPDF(ctx, pdfKey, merged); err != nil {
		return err
	}
	return nil
}

func closeFiles(files []*os.File) {
	for _, f := range files {
		if f != nil {
			_ = f.Close()
		}
	}
}

func (h *PDFHandler) failJob(ctx context.Context, logAttrs []any, job jobstore.Job, code, message string, cause error) {
	job.Status = statusFailed
	job.ErrorCode = code
	job.ErrorMsg = message
	job.UpdatedAt = time.Now()
	if _, err := h.store.Update(ctx, job); err != nil {
		h.logger.Error("pdf: failed to persist failed status",
			append(logAttrs,
				slog.String("error", err.Error()),
				slog.String("cause", cause.Error()),
			)...,
		)
		return
	}

	attrs := append(logAttrs,
		slog.String("errorCode", code),
		slog.String("message", message),
		slog.String("cause", cause.Error()),
	)
	var ge *gotenberg.ConvertError
	if errors.As(cause, &ge) {
		attrs = append(attrs,
			slog.Int("gotenbergStatus", ge.Status),
			slog.String("bodySnippet", ge.BodySnippet),
		)
	}
	h.logger.Error("pdf pipeline failed", attrs...)
}

func (h *PDFHandler) readJob(ctx context.Context, jobID string) (jobstore.Job, error) {
	job, err := h.store.GetJob(ctx, jobID)
	if err == nil {
		return job, nil
	}
	if errors.Is(err, jobstore.ErrNotFound) {
		return jobstore.Job{}, nil
	}
	return jobstore.Job{}, err
}

func (h *PDFHandler) waitForTerminalState(ctx context.Context, jobID string) (jobstore.Job, error) {
	ticker := time.NewTicker(h.jobWaitPollDelay)
	defer ticker.Stop()

	for {
		job, err := h.readJob(ctx, jobID)
		if err != nil {
			return jobstore.Job{}, err
		}
		if job.Status == statusReady || job.Status == statusFailed {
			return job, nil
		}

		select {
		case <-ctx.Done():
			return jobstore.Job{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (h *PDFHandler) respondWithCachedJob(w http.ResponseWriter, r *http.Request, jobID string, job jobstore.Job, reqAttrs []any) {
	pdfKey := pdfObjectKey(h.outputPrefix, jobID)
	url, err := h.storage.DownloadURL(r.Context(), pdfKey)
	if err != nil {
		h.logger.Error("pdf: failed to refresh cached URL",
			append(reqAttrs, slog.String("error", err.Error()))...,
		)
		writeError(w, r, http.StatusInternalServerError, "UPLOAD_ERROR", "failed to build PDF URL", nil)
		return
	}

	status := job.Status
	if status == "" {
		status = statusReady
	}

	h.logger.Info("pdf: returning cached job", reqAttrs...)
	writeJSON(w, http.StatusOK, models.PDFResponse{
		RequestID: requestID(r),
		JobID:     jobID,
		Status:    status,
		PDFKey:    pdfKey,
		URL:       url,
	})
}

func pdfLockKey(jobID string) string {
	return "lock:pdf:" + jobID
}
