package jobstore

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrNotFound = errors.New("job not found")
var ErrConflict = errors.New("job update conflict")

const (
	JobTypeHTML = "html"
	JobTypePDF  = "pdf"
)

const (
	JobStatusReady      = "ready"
	JobStatusQueued     = "queued"
	JobStatusProcessing = "processing"
	JobStatusCompleted  = "completed"
	JobStatusFailed     = "failed"
)

const (
	EmailStatusNone       = "none"
	EmailStatusRegistered = "registered"
	EmailStatusPending    = "pending"
	EmailStatusSending    = "sending"
	EmailStatusSent       = "sent"
	EmailStatusFailed     = "failed"
)

type Job struct {
	ID                     string          `json:"jobId"`
	Type                   string          `json:"type,omitempty"`
	Status                 string          `json:"status"`
	HTMLURL                string          `json:"htmlUrl,omitempty"`
	PDFURL                 string          `json:"pdfUrl,omitempty"`
	ErrorCode              string          `json:"errorCode,omitempty"`
	ErrorMessage           string          `json:"errorMessage,omitempty"`
	Recipients             []string        `json:"recipients,omitempty"`
	EmailStatus            string          `json:"emailStatus,omitempty"`
	EmailError             string          `json:"emailError,omitempty"`
	CreatedAt              time.Time       `json:"createdAt"`
	UpdatedAt              time.Time       `json:"updatedAt,omitempty"`
	ProcessingStartedAt    *time.Time      `json:"processingStartedAt,omitempty"`
	CompletedAt            *time.Time      `json:"completedAt,omitempty"`
	FailedAt               *time.Time      `json:"failedAt,omitempty"`
	RecipientsRegisteredAt *time.Time      `json:"recipientsRegisteredAt,omitempty"`
	EmailSentAt            *time.Time      `json:"emailSentAt,omitempty"`
	Payload                json.RawMessage `json:"payload,omitempty"`
}

type Store interface {
	Save(ctx context.Context, job Job) (Job, error)
	GetJob(ctx context.Context, jobID string) (Job, error)
}

type PDFStore interface {
	Store
	Update(ctx context.Context, jobID string, mutate func(*Job) error) (Job, error)
	List(ctx context.Context, limit int) ([]Job, error)
}
