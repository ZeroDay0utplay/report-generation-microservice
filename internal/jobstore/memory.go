package jobstore

import (
	"context"
	"sync"
	"time"
)

type MemoryStore struct {
	mu   sync.RWMutex
	jobs map[string]Job
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{jobs: make(map[string]Job)}
}

func (s *MemoryStore) Create(_ context.Context, job Job) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.jobs[job.ID]; ok {
		return existing, nil
	}
	now := time.Now()
	job.CreatedAt = now
	job.UpdatedAt = now
	s.jobs[job.ID] = job
	return job, nil
}

func (s *MemoryStore) Update(_ context.Context, jobID string, fields map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return ErrNotFound
	}

	if v, ok := fields["status"]; ok {
		job.Status = v.(string)
	}
	if v, ok := fields["pdfUrl"]; ok {
		job.PDFURL = v.(string)
	}
	if v, ok := fields["error"]; ok {
		job.Error = v.(string)
	}
	job.UpdatedAt = time.Now()
	s.jobs[jobID] = job
	return nil
}

func (s *MemoryStore) GetJob(_ context.Context, jobID string) (Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return Job{}, ErrNotFound
	}
	return job, nil
}

func (s *MemoryStore) AppendEmails(_ context.Context, jobID string, emails []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return ErrNotFound
	}

	seen := make(map[string]struct{}, len(job.Emails))
	for _, e := range job.Emails {
		seen[e] = struct{}{}
	}
	for _, e := range emails {
		if _, ok := seen[e]; !ok {
			job.Emails = append(job.Emails, e)
			seen[e] = struct{}{}
		}
	}
	job.UpdatedAt = time.Now()
	s.jobs[jobID] = job
	return nil
}
