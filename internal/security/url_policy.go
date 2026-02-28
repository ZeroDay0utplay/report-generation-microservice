package security

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"pdf-html-service/internal/models"
)

type Violation struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ValidationError struct {
	Violations []Violation `json:"violations"`
}

func (e *ValidationError) Error() string {
	if len(e.Violations) == 0 {
		return "url policy validation failed"
	}
	return fmt.Sprintf("url policy validation failed: %s", e.Violations[0].Field)
}

type URLPolicy struct {
	requireHTTPS bool
	allowlist    map[string]struct{}
}

func NewURLPolicy(requireHTTPS bool, allowlist []string) *URLPolicy {
	wl := make(map[string]struct{}, len(allowlist))
	for _, h := range allowlist {
		h = strings.ToLower(strings.TrimSpace(h))
		if h != "" {
			wl[h] = struct{}{}
		}
	}

	return &URLPolicy{
		requireHTTPS: requireHTTPS,
		allowlist:    wl,
	}
}

func (p *URLPolicy) ValidatePayload(payload models.ReportRequest) error {
	violations := make([]Violation, 0)

	if payload.Company.LogoURL != "" {
		if err := p.validateURL("company.logoUrl", payload.Company.LogoURL); err != nil {
			violations = append(violations, Violation{Field: "company.logoUrl", Message: err.Error()})
		}
	}

	if payload.Company.Website != "" {
		if err := p.validateURL("company.website", payload.Company.Website); err != nil {
			violations = append(violations, Violation{Field: "company.website", Message: err.Error()})
		}
	}

	for i, pair := range payload.Pairs {
		beforeField := fmt.Sprintf("pairs[%d].beforeUrl", i)
		afterField := fmt.Sprintf("pairs[%d].afterUrl", i)

		if err := p.validateURL(beforeField, pair.BeforeURL); err != nil {
			violations = append(violations, Violation{Field: beforeField, Message: err.Error()})
		}
		if err := p.validateURL(afterField, pair.AfterURL); err != nil {
			violations = append(violations, Violation{Field: afterField, Message: err.Error()})
		}
	}

	if len(violations) > 0 {
		return &ValidationError{Violations: violations}
	}
	return nil
}

func (p *URLPolicy) validateURL(field, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL")
	}

	if p.requireHTTPS && !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("must use https")
	}

	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" {
		return fmt.Errorf("missing host")
	}

	if isBlockedHost(host) {
		return fmt.Errorf("host is not allowed")
	}

	if len(p.allowlist) > 0 {
		if _, ok := p.allowlist[host]; !ok {
			return fmt.Errorf("host not in allowlist")
		}
	}

	_ = field
	return nil
}

func isBlockedHost(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return true
	}

	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}

	for _, suffix := range []string{".local", ".internal", ".intranet", ".home"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}

	return false
}
