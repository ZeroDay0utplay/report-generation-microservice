package pdfjobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
)

const brevoSendEmailEndpoint = "https://api.brevo.com/v3/smtp/email"

type BrevoAPINotifierConfig struct {
	APIKey      string
	SenderEmail string
	SenderName  string
	Subject     string
	Endpoint    string
}

type BrevoAPINotifier struct {
	logger     *slog.Logger
	httpClient *http.Client
	apiKey     string
	sender     mail.Address
	subject    string
	endpoint   string
}

type brevoSendEmailRequest struct {
	Sender      brevoRecipient   `json:"sender"`
	To          []brevoRecipient `json:"to"`
	Subject     string           `json:"subject"`
	HTMLContent string           `json:"htmlContent"`
	TextContent string           `json:"textContent,omitempty"`
}

type brevoRecipient struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

func NewBrevoAPINotifier(logger *slog.Logger, httpClient *http.Client, cfg BrevoAPINotifierConfig) (*BrevoAPINotifier, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("APIKey is required")
	}

	sender, err := mail.ParseAddress(strings.TrimSpace(cfg.SenderEmail))
	if err != nil {
		return nil, fmt.Errorf("invalid SenderEmail: %w", err)
	}
	sender.Name = strings.TrimSpace(cfg.SenderName)

	subject := sanitizeEmailSubject(cfg.Subject)
	if subject == "" {
		subject = "Votre rapport PDF est pret"
	}

	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = brevoSendEmailEndpoint
	}

	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &BrevoAPINotifier{
		logger:     logger,
		httpClient: httpClient,
		apiKey:     apiKey,
		sender:     *sender,
		subject:    subject,
		endpoint:   endpoint,
	}, nil
}

func (n *BrevoAPINotifier) SendReportReady(ctx context.Context, recipients []string, jobID, pdfURL string, mission MissionInfo) error {
	cleanRecipients, err := normalizeRecipients(recipients)
	if err != nil {
		return err
	}
	if len(cleanRecipients) == 0 {
		return nil
	}

	to := make([]brevoRecipient, 0, len(cleanRecipients))
	for _, recipient := range cleanRecipients {
		to = append(to, brevoRecipient{Email: recipient, Name: recipientDisplayName(recipient)})
	}

	htmlBody := buildEnterpriseFrenchEmailHTML(mission, pdfURL)
	textBody := buildEnterpriseFrenchEmailText(mission, pdfURL)

	payload, err := json.Marshal(brevoSendEmailRequest{
		Sender: brevoRecipient{
			Email: n.sender.Address,
			Name:  n.sender.Name,
		},
		To:          to,
		Subject:     n.subject,
		HTMLContent: htmlBody,
		TextContent: textBody,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("api-key", n.apiKey)
	req.Header.Set("content-type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		n.logger.Info("brevo email acknowledged",
			slog.String("jobId", jobID),
			slog.Int("recipients", len(cleanRecipients)),
		)
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("brevo api status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

func normalizeRecipients(recipients []string) ([]string, error) {
	out := make([]string, 0, len(recipients))
	seen := make(map[string]struct{}, len(recipients))

	for _, recipient := range recipients {
		normalized := strings.ToLower(strings.TrimSpace(recipient))
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		if _, err := mail.ParseAddress(normalized); err != nil {
			return nil, fmt.Errorf("invalid recipient %q: %w", normalized, err)
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}

	return out, nil
}

func recipientDisplayName(email string) string {
	email = strings.TrimSpace(email)
	at := strings.IndexByte(email, '@')
	if at <= 0 {
		return ""
	}
	name := strings.TrimSpace(email[:at])
	name = strings.ReplaceAll(name, ".", " ")
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, "-", " ")
	parts := strings.Fields(name)
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func buildEnterpriseFrenchEmailHTML(mission MissionInfo, pdfURL string) string {
	safeMission := html.EscapeString(strings.TrimSpace(mission.InterventionName))
	safeAddress := html.EscapeString(strings.TrimSpace(mission.Address))
	safeURL := html.EscapeString(strings.TrimSpace(pdfURL))
	if safeMission == "" {
		safeMission = "-"
	}
	if safeAddress == "" {
		safeAddress = "-"
	}
	if safeURL == "" {
		safeURL = "-"
	}

	return fmt.Sprintf(
		"<!doctype html><html lang=\"fr\"><body style=\"margin:0;padding:0;background:#f2f5f9;font-family:'Segoe UI',Arial,sans-serif;color:#12263a;\">"+
			"<table role=\"presentation\" width=\"100%%\" cellspacing=\"0\" cellpadding=\"0\" style=\"background:#f2f5f9;padding:28px 12px;\">"+
			"<tr><td align=\"center\">"+
			"<table role=\"presentation\" width=\"620\" cellspacing=\"0\" cellpadding=\"0\" style=\"max-width:620px;background:#ffffff;border-radius:16px;overflow:hidden;border:1px solid #d8e2ee;\">"+
			"<tr><td style=\"background:linear-gradient(135deg,#0f4c81,#0b6fb8);padding:28px 32px;color:#ffffff;\">"+
			"<h1 style=\"margin:10px 0 0 0;font-size:25px;line-height:1.25;font-weight:700;\">Votre rapport PDF est prêt</h1>"+
			"</td></tr>"+
			"<tr><td style=\"padding:28px 32px 8px 32px;\">"+
			"<p style=\"margin:0 0 16px 0;font-size:15px;line-height:1.7;color:#28435b;\">Bonjour,<br/>Le traitement de votre rapport est terminé Vous pouvez acceder au document final via le lien securisé ci-dessous.</p>"+
			"<table role=\"presentation\" width=\"100%%\" cellspacing=\"0\" cellpadding=\"0\" style=\"margin:0 0 18px 0;background:#f7fafc;border:1px solid #d8e2ee;border-radius:12px;\">"+
			"<tr><td style=\"padding:14px 16px;font-size:13px;color:#48627a;\"><strong style=\"color:#1a3248;\">Intervention :</strong> %s</td></tr>"+
			"<tr><td style=\"padding:0 16px 14px 16px;font-size:13px;color:#48627a;\"><strong style=\"color:#1a3248;\">Adresse :</strong> %s</td></tr>"+
			"</table>"+
			"<table role=\"presentation\" cellspacing=\"0\" cellpadding=\"0\" style=\"margin:0 0 18px 0;\"><tr><td>"+
			"<a href=\"%s\" target=\"_blank\" rel=\"noopener noreferrer\" style=\"display:inline-block;background:#0f4c81;color:#ffffff;text-decoration:none;font-weight:600;font-size:14px;padding:13px 22px;border-radius:10px;\">Telecharger le rapport</a>"+
			"</td></tr></table>"+
			"<p style=\"margin:0 0 6px 0;font-size:12px;color:#6b8094;\">Si le bouton ne fonctionne pas, copiez ce lien dans votre navigateur :</p>"+
			"<p style=\"margin:0 0 20px 0;font-size:12px;line-height:1.6;word-break:break-all;\"><a href=\"%s\" target=\"_blank\" rel=\"noopener noreferrer\" style=\"color:#0b6fb8;text-decoration:none;\">%s</a></p>"+
			"</td></tr>"+
			"<tr><td style=\"padding:18px 32px 26px 32px;border-top:1px solid #e5ecf3;background:#fbfdff;\">"+
			"<p style=\"margin:0;font-size:12px;line-height:1.7;color:#6b8094;\">Cordialement,<br/>Service Reporting - IDEO Groupe</p>"+
			"</td></tr>"+
			"</table>"+
			"</td></tr></table>"+
			"</body></html>",
		safeMission,
		safeAddress,
		safeURL,
		safeURL,
		safeURL,
	)
}

func buildEnterpriseFrenchEmailText(mission MissionInfo, pdfURL string) string {
	name := strings.TrimSpace(mission.InterventionName)
	if name == "" {
		name = "-"
	}
	address := strings.TrimSpace(mission.Address)
	if address == "" {
		address = "-"
	}
	link := strings.TrimSpace(pdfURL)
	if link == "" {
		link = "-"
	}

	return fmt.Sprintf(
		"Bonjour,\n\nVotre rapport PDF est pret.\nIntervention: %s\nAdresse: %s\nLien de telechargement: %s\n\nCordialement,\nService Reporting - IDEO Groupe\n",
		name,
		address,
		link,
	)
}

func sanitizeEmailSubject(subject string) string {
	clean := strings.ReplaceAll(subject, "\r", "")
	clean = strings.ReplaceAll(clean, "\n", "")
	return strings.TrimSpace(clean)
}
