package util

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

func NewJobID() string {
	return newID("job")
}

func NewRequestID() string {
	return newID("req")
}

func newID(prefix string) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}

	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixMilli(), hex.EncodeToString(b))
}
