package jobstore

import (
	"context"
	"sync"

	"pdf-html-service/internal/models"
)

type entry struct {
	job     Job
	payload models.ReportRequest
}

type MemoryStore struct {
	mu      sync.RWMutex
	entries map[string]entry
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{entries: make(map[string]entry)}
}

func (s *MemoryStore) Save(_ context.Context, job Job, payload models.ReportRequest) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e, ok := s.entries[job.ID]; ok {
		return e.job, nil
	}
	s.entries[job.ID] = entry{job: job, payload: payload}
	return job, nil
}

func (s *MemoryStore) GetJob(_ context.Context, jobID string) (Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e, ok := s.entries[jobID]
	if !ok {
		return Job{}, ErrNotFound
	}
	return e.job, nil
}

func (s *MemoryStore) GetPayload(_ context.Context, jobID string) (models.ReportRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e, ok := s.entries[jobID]
	if !ok {
		return models.ReportRequest{}, ErrNotFound
	}
	return e.payload, nil
}
