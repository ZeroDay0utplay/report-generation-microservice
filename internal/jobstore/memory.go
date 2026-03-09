package jobstore

import (
	"context"
	"sort"
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

func (s *MemoryStore) Update(_ context.Context, jobID string, mutate func(*Job) error) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[jobID]
	if !ok {
		return Job{}, ErrNotFound
	}

	job := e.job
	if err := mutate(&job); err != nil {
		return Job{}, err
	}

	s.entries[jobID] = entry{job: job}
	return job, nil
}

func (s *MemoryStore) List(_ context.Context, limit int) ([]Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Job, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, e.job)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
