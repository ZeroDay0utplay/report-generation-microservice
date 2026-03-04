package jobstore

import (
	"context"
	"errors"
	"time"

	"pdf-html-service/internal/models"
)

var ErrNotFound = errors.New("job not found")

type Job struct {
	ID        string    `json:"jobId"`
	Status    string    `json:"status"`
	HTMLURL   string    `json:"htmlUrl,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type Store interface {
	Save(ctx context.Context, job Job, payload models.ReportRequest) (Job, error)
	GetJob(ctx context.Context, jobID string) (Job, error)
	GetPayload(ctx context.Context, jobID string) (models.ReportRequest, error)
}
