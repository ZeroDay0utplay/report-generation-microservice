package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port               string
	MaxPairs           int
	RequestBodyLimitMB int
	RequireHTTPS       bool
	ImageHostAllowlist []string
	GotenbergURL       string
	UploadHTMLOnPDF    bool
	B2Endpoint         string
	B2Region           string
	B2Bucket           string
	B2AccessKeyID      string
	B2SecretAccessKey  string
	B2PublicBaseURL    string
	OutputPrefix       string
	LogLevel           string
	LogoURL            string
	PublicBaseURL      string
}

func Load() (Config, error) {
	cfg := Config{
		Port:               getEnv("PORT", "4000"),
		MaxPairs:           getEnvInt("MAX_PAIRS", 200),
		RequestBodyLimitMB: getEnvInt("REQUEST_BODY_LIMIT_MB", 2),
		RequireHTTPS:       getEnvBool("REQUIRE_HTTPS", true),
		ImageHostAllowlist: parseCSV(getEnv("IMAGE_HOST_ALLOWLIST", "")),
		GotenbergURL:       strings.TrimRight(getEnv("GOTENBERG_URL", "http://gotenberg:4000"), "/"),
		UploadHTMLOnPDF:    getEnvBool("UPLOAD_HTML_ON_PDF", false),
		B2Endpoint:         strings.TrimRight(os.Getenv("B2_ENDPOINT"), "/"),
		B2Region:           os.Getenv("B2_REGION"),
		B2Bucket:           os.Getenv("B2_BUCKET"),
		B2AccessKeyID:      os.Getenv("B2_ACCESS_KEY_ID"),
		B2SecretAccessKey:  os.Getenv("B2_SECRET_ACCESS_KEY"),
		B2PublicBaseURL:    strings.TrimRight(os.Getenv("B2_PUBLIC_BASE_URL"), "/"),
		OutputPrefix:       strings.Trim(getEnv("OUTPUT_PREFIX", "docs"), "/"),
		LogLevel:           strings.ToLower(getEnv("LOG_LEVEL", "info")),
		LogoURL:            getEnv("LOGO_URL", "https://dev-ideo-assets.s3.eu-central-003.backblazeb2.com/logo.png"),
		PublicBaseURL:      strings.TrimRight(getEnv("PUBLIC_BASE_URL", ""), "/"),
	}

	if cfg.MaxPairs <= 0 {
		return Config{}, fmt.Errorf("MAX_PAIRS must be > 0")
	}
	if cfg.RequestBodyLimitMB <= 0 {
		return Config{}, fmt.Errorf("REQUEST_BODY_LIMIT_MB must be > 0")
	}

	missing := make([]string, 0, 6)
	for _, name := range []struct {
		key string
		val string
	}{
		{key: "B2_ENDPOINT", val: cfg.B2Endpoint},
		{key: "B2_REGION", val: cfg.B2Region},
		{key: "B2_BUCKET", val: cfg.B2Bucket},
		{key: "B2_ACCESS_KEY_ID", val: cfg.B2AccessKeyID},
		{key: "B2_SECRET_ACCESS_KEY", val: cfg.B2SecretAccessKey},
		{key: "B2_PUBLIC_BASE_URL", val: cfg.B2PublicBaseURL},
	} {
		if strings.TrimSpace(name.val) == "" {
			missing = append(missing, name.key)
		}
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}

	if cfg.OutputPrefix == "" {
		cfg.OutputPrefix = "docs"
	}

	return cfg, nil
}

func (c Config) BodyLimitBytes() int64 {
	return int64(c.RequestBodyLimitMB) * 1024 * 1024
}

func getEnv(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func getEnvInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func parseCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		h := strings.ToLower(strings.TrimSpace(p))
		if h != "" {
			out = append(out, h)
		}
	}
	return out
}
