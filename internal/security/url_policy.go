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

// NewURLPolicy creates a URLPolicy. When requireHTTPS is true every image URL
// must use the https scheme. allowlist restricts hosts to the given list when
// non-empty.
func NewURLPolicy(requireHTTPS bool, allowlist []string) *URLPolicy {
	wl := make(map[string]struct{}, len(allowlist))
	for _, h := range allowlist {
		h = strings.ToLower(strings.TrimSpace(h))
		if h != "" {
			wl[h] = struct{}{}
		}
	}
	return &URLPolicy{requireHTTPS: requireHTTPS, allowlist: wl}
}

// ValidatePayload checks every URL in the payload against the policy.
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
		before := fmt.Sprintf("pairs[%d].beforeUrl", i)
		after := fmt.Sprintf("pairs[%d].afterUrl", i)
		if err := p.validateURL(before, pair.BeforeURL); err != nil {
			violations = append(violations, Violation{Field: before, Message: err.Error()})
		}
		if err := p.validateURL(after, pair.AfterURL); err != nil {
			violations = append(violations, Violation{Field: after, Message: err.Error()})
		}
	}

	for i, truck := range payload.Trucks {
		field := fmt.Sprintf("trucks[%d].url", i)
		if err := p.validateURL(field, truck.URL); err != nil {
			violations = append(violations, Violation{Field: field, Message: err.Error()})
		}
	}

	for i, evidence := range payload.Evidences {
		field := fmt.Sprintf("evidences[%d].url", i)
		if err := p.validateURL(field, evidence.URL); err != nil {
			violations = append(violations, Violation{Field: field, Message: err.Error()})
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
		return fmt.Errorf("field %s: invalid URL", field)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("field %s: unsupported scheme %q (must be http or https)", field, scheme)
	}
	if p.requireHTTPS && scheme != "https" {
		return fmt.Errorf("field %s: must use https", field)
	}

	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" {
		return fmt.Errorf("field %s: missing host", field)
	}

	if isBlockedHost(host) {
		return fmt.Errorf("field %s: host is not allowed", field)
	}

	if len(p.allowlist) > 0 {
		if _, ok := p.allowlist[host]; !ok {
			return fmt.Errorf("field %s: host not in allowlist", field)
		}
	}

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
