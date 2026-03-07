package util

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"pdf-html-service/internal/models"
)

// NewJobID generates a random, time-prefixed job ID.
// Use JobIDFromPayload for idempotent generation.
func NewJobID() string {
	return newID("job")
}

// NewRequestID generates a random, time-prefixed request ID.
func NewRequestID() string {
	return newID("req")
}

// JobIDFromPayload returns a deterministic job ID derived from the SHA-256 hash
// of the raw request body. Identical payloads always produce the same ID,
// enabling idempotent report generation.
func JobIDFromPayload(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("job_%x", h[:12])
}

func JobIDFromReportRequest(payload models.ReportRequest) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload for job id: %w", err)
	}
	return JobIDFromPayload(raw), nil
}

func newID(prefix string) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixMilli(), hex.EncodeToString(b))
}
