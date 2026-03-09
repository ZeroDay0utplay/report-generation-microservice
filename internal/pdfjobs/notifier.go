package pdfjobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

type LogNotifier struct {
	logger *slog.Logger
}

func NewLogNotifier(logger *slog.Logger) *LogNotifier {
	return &LogNotifier{logger: logger}
}

func (n *LogNotifier) SendReportReady(_ context.Context, recipients []string, jobID, pdfURL string) error {
	n.logger.Info("email notification acknowledged",
		slog.String("jobId", jobID),
		slog.Int("recipients", len(recipients)),
		slog.String("pdfUrl", pdfURL),
	)
	return nil
}

type WebhookNotifier struct {
	endpoint   string
	httpClient *http.Client
}

type webhookPayload struct {
	JobID      string   `json:"jobId"`
	PDFURL     string   `json:"pdfUrl"`
	Recipients []string `json:"recipients"`
}

func NewWebhookNotifier(endpoint string, httpClient *http.Client) *WebhookNotifier {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &WebhookNotifier{endpoint: strings.TrimSpace(endpoint), httpClient: httpClient}
}

func (n *WebhookNotifier) SendReportReady(ctx context.Context, recipients []string, jobID, pdfURL string) error {
	payload, err := json.Marshal(webhookPayload{JobID: jobID, PDFURL: pdfURL, Recipients: recipients})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Idempotency-Key", "pdf-email:"+jobID)

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("webhook notifier status %d: %s", resp.StatusCode, string(body))
}
