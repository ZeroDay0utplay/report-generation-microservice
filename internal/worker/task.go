package worker

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

const (
	TypePDFGenerate = "pdf:generate"
	MaxRetry        = 3
	JobTTL          = 24 * time.Hour
)

// TaskPayload is the Asynq task payload for TypePDFGenerate.
type TaskPayload struct {
	JobID   string          `json:"jobId"`
	RawBody json.RawMessage `json:"rawBody"`
}

// NewPDFGenerateTask builds an Asynq task for PDF generation.
// The task uses MaxRetry Asynq-level retries for infrastructure failures;
// per-chunk retries are handled inside the processor.
func NewPDFGenerateTask(jobID string, rawBody json.RawMessage) (*asynq.Task, error) {
	payload, err := json.Marshal(TaskPayload{JobID: jobID, RawBody: rawBody})
	if err != nil {
		return nil, fmt.Errorf("marshal task payload: %w", err)
	}
	return asynq.NewTask(
		TypePDFGenerate,
		payload,
		asynq.MaxRetry(MaxRetry),
		asynq.Timeout(15*time.Minute), // overall task budget
		asynq.Retention(JobTTL),       // keep in Asynq's completed/archived for 24h
	), nil
}
