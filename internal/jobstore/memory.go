package jobstore

import (
	"context"
	"sync"
)

type entry struct {
	job Job
}

type MemoryStore struct {
	mu      sync.RWMutex
	entries map[string]entry
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{entries: make(map[string]entry)}
}

func (s *MemoryStore) Save(_ context.Context, job Job) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e, ok := s.entries[job.ID]; ok {
		return e.job, nil
	}
	s.entries[job.ID] = entry{job: job}
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
