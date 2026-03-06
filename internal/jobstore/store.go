package jobstore

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("job not found")

type Job struct {
	ID        string    `json:"jobId"`
	Status    string    `json:"status"`
	PDFURL    string    `json:"pdfUrl,omitempty"`
	Error     string    `json:"error,omitempty"`
	Emails    []string  `json:"emails,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Store interface {
	Create(ctx context.Context, job Job) (Job, error)
	Update(ctx context.Context, jobID string, fields map[string]any) error
	GetJob(ctx context.Context, jobID string) (Job, error)
	AppendEmails(ctx context.Context, jobID string, emails []string) error
}
