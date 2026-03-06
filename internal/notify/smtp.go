package notify

import (
	"context"
	"fmt"
	"net/smtp"
	"strconv"
	"strings"
)

type Sender interface {
	Enabled() bool
	SendPDFReady(ctx context.Context, jobID string, pdfURL string, emails []string) error
}

type SMTPSender struct {
	host    string
	port    int
	user    string
	pass    string
	from    string
	enabled bool
}

func NewSMTPSender(host string, port int, user string, pass string, from string) *SMTPSender {
	host = strings.TrimSpace(host)
	from = strings.TrimSpace(from)
	if host == "" || from == "" {
		return &SMTPSender{}
	}
	if port <= 0 {
		port = 587
	}

	return &SMTPSender{
		host:    host,
		port:    port,
		user:    strings.TrimSpace(user),
		pass:    pass,
		from:    from,
		enabled: true,
	}
}

func (s *SMTPSender) Enabled() bool {
	return s.enabled
}

func (s *SMTPSender) SendPDFReady(_ context.Context, jobID string, pdfURL string, emails []string) error {
	if !s.enabled {
		return fmt.Errorf("smtp sender is not configured")
	}
	if len(emails) == 0 {
		return nil
	}

	targets := uniqueEmails(emails)
	if len(targets) == 0 {
		return nil
	}

	subject := fmt.Sprintf("Your report is ready (%s)", jobID)
	body := fmt.Sprintf("Your PDF report is ready.\n\nJob ID: %s\nURL: %s\n", jobID, pdfURL)

	msg := strings.Builder{}
	msg.WriteString("From: ")
	msg.WriteString(s.from)
	msg.WriteString("\r\n")
	msg.WriteString("To: ")
	msg.WriteString(strings.Join(targets, ","))
	msg.WriteString("\r\n")
	msg.WriteString("Subject: ")
	msg.WriteString(subject)
	msg.WriteString("\r\n")
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body)

	addr := s.host + ":" + strconv.Itoa(s.port)
	var auth smtp.Auth
	if s.user != "" {
		auth = smtp.PlainAuth("", s.user, s.pass, s.host)
	}

	if err := smtp.SendMail(addr, auth, s.from, targets, []byte(msg.String())); err != nil {
		return fmt.Errorf("send mail: %w", err)
	}

	return nil
}

func uniqueEmails(emails []string) []string {
	seen := make(map[string]struct{}, len(emails))
	out := make([]string, 0, len(emails))
	for _, e := range emails {
		normalized := strings.ToLower(strings.TrimSpace(e))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}
