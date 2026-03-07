package jobstore

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrNotFound = errors.New("job not found")

type Job struct {
	ID        string          `json:"jobId"`
	Status    string          `json:"status"`
	HTMLURL   string          `json:"htmlUrl,omitempty"`
	PDFURL    string          `json:"pdfUrl,omitempty"`
	ErrorCode string          `json:"errorCode,omitempty"`
	ErrorMsg  string          `json:"errorMessage,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type Store interface {
	Save(ctx context.Context, job Job) (Job, error)
	Update(ctx context.Context, job Job) (Job, error)
	GetJob(ctx context.Context, jobID string) (Job, error)
	AcquireLock(ctx context.Context, key string, owner string, ttl time.Duration) (bool, error)
	ReleaseLock(ctx context.Context, key string, owner string) error
}
