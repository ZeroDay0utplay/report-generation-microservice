package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"time"
)

// WebhookPayload is posted to callback.webhookUrl.
type WebhookPayload struct {
	JobID       string `json:"job_id"`
	Status      string `json:"status"`
	DownloadURL string `json:"download_url,omitempty"`
	Error       string `json:"error,omitempty"`
}

// PostWebhook fires a POST to webhookURL with a JSON body. Non-2xx responses
// are returned as errors. The call is bounded by a 15-second timeout.
func PostWebhook(ctx context.Context, client *http.Client, webhookURL string, p WebhookPayload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned non-2xx status %d", resp.StatusCode)
	}
	return nil
}

// SMTPConfig holds credentials for the outbound mailer.
type SMTPConfig struct {
	Host string
	Port int
	User string
	Pass string
	From string
}

// Enabled reports whether SMTP is configured.
func (c SMTPConfig) Enabled() bool {
	return c.Host != "" && c.From != ""
}

// SendEmail sends a plain-text email via SMTP. No-op when cfg is not Enabled.
func SendEmail(cfg SMTPConfig, to, subject, body string) error {
	if !cfg.Enabled() {
		return nil
	}

	auth := smtp.PlainAuth("", cfg.User, cfg.Pass, cfg.Host)
	msg := strings.Join([]string{
		"From: " + cfg.From,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}, "\r\n")

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	if err := smtp.SendMail(addr, auth, cfg.From, []string{to}, []byte(msg)); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
}
