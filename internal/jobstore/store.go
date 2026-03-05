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
	CreatedAt time.Time       `json:"createdAt"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Error     string          `json:"error,omitempty"`
}

type Store interface {
	Save(ctx context.Context, job Job) (Job, error)
	Update(ctx context.Context, job Job) error
	GetJob(ctx context.Context, jobID string) (Job, error)
}
