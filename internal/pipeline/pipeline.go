package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"pdf-html-service/internal/jobstore"
	"pdf-html-service/internal/notify"
	"pdf-html-service/internal/render"
)

type Storage interface {
	UploadPDF(ctx context.Context, key string, reader io.Reader) error
	PublicURL(key string) string
}

type PDFRenderer interface {
	ConvertHTMLToPDF(ctx context.Context, html string) (io.ReadCloser, error)
}

type Pipeline struct {
	store    jobstore.Store
	storage  Storage
	renderer PDFRenderer
	mailer   notify.Sender
	logger   *slog.Logger
	prefix   string
	logoURL  string
}

func New(
	store jobstore.Store,
	storage Storage,
	renderer PDFRenderer,
	mailer notify.Sender,
	logger *slog.Logger,
	prefix string,
	logoURL string,
) *Pipeline {
	return &Pipeline{
		store:    store,
		storage:  storage,
		renderer: renderer,
		mailer:   mailer,
		logger:   logger,
		prefix:   prefix,
		logoURL:  logoURL,
	}
}

type Result struct {
	PDFURL string
	Err    error
}

func (p *Pipeline) Run(ctx context.Context, jobID string, payload []byte) Result {
	p.updateStatus(ctx, jobID, "generating")

	pdfURL, err := p.generate(ctx, jobID, payload)
	if err != nil {
		p.fail(ctx, jobID, err)
		return Result{Err: err}
	}

	if err := p.store.Update(ctx, jobID, map[string]any{
		"status": "done",
		"pdfUrl": pdfURL,
	}); err != nil {
		p.logger.Error("failed to persist done status",
			slog.String("jobId", jobID),
			slog.String("pdfUrl", pdfURL),
			slog.String("error", err.Error()),
		)
	}

	p.dispatchEmails(ctx, jobID, pdfURL)

	return Result{PDFURL: pdfURL}
}

func (p *Pipeline) generate(ctx context.Context, jobID string, payload []byte) (string, error) {
	req, err := parsePayload(payload)
	if err != nil {
		return "", fmt.Errorf("parse payload: %w", err)
	}

	htmlDoc, err := render.RenderPDFHTMLWithLogo(req, p.logoURL)
	if err != nil {
		return "", fmt.Errorf("render html: %w", err)
	}

	pdfReader, err := p.renderer.ConvertHTMLToPDF(ctx, htmlDoc)
	if err != nil {
		return "", fmt.Errorf("convert pdf: %w", err)
	}
	defer pdfReader.Close()

	pdfBytes, err := io.ReadAll(pdfReader)
	if err != nil {
		return "", fmt.Errorf("read pdf: %w", err)
	}

	pdfKey := pdfObjectKey(p.prefix, jobID)
	if err := p.storage.UploadPDF(ctx, pdfKey, bytes.NewReader(pdfBytes)); err != nil {
		return "", fmt.Errorf("upload pdf: %w", err)
	}

	return p.storage.PublicURL(pdfKey), nil
}

func (p *Pipeline) fail(ctx context.Context, jobID string, genErr error) {
	if err := p.store.Update(ctx, jobID, map[string]any{
		"status": "failed",
		"error":  genErr.Error(),
	}); err != nil {
		p.logger.Error("failed to persist failed status",
			slog.String("jobId", jobID),
			slog.String("error", err.Error()),
		)
	}
}

func (p *Pipeline) updateStatus(ctx context.Context, jobID, status string) {
	if err := p.store.Update(ctx, jobID, map[string]any{"status": status}); err != nil {
		p.logger.Error("failed to update status",
			slog.String("jobId", jobID),
			slog.String("status", status),
			slog.String("error", err.Error()),
		)
	}
}

func (p *Pipeline) dispatchEmails(ctx context.Context, jobID, pdfURL string) {
	if p.mailer == nil || !p.mailer.Enabled() {
		return
	}

	job, err := p.store.GetJob(ctx, jobID)
	if err != nil {
		p.logger.Error("failed to read job for email dispatch",
			slog.String("jobId", jobID),
			slog.String("error", err.Error()),
		)
		return
	}

	if len(job.Emails) == 0 {
		return
	}

	if err := p.mailer.SendPDFReady(ctx, jobID, pdfURL, job.Emails); err != nil {
		p.logger.Error("email dispatch failed",
			slog.String("jobId", jobID),
			slog.Int("recipientCount", len(job.Emails)),
			slog.String("error", err.Error()),
		)
	} else {
		p.logger.Info("email dispatch succeeded",
			slog.String("jobId", jobID),
			slog.Int("recipientCount", len(job.Emails)),
		)
	}
}

func pdfObjectKey(prefix, jobID string) string {
	return fmt.Sprintf("%s/%s/report.pdf", strings.Trim(prefix, "/"), jobID)
}
