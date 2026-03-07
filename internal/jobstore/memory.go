package jobstore

import (
	"context"
	"sync"
	"time"
)

type entry struct {
	job Job
}

type MemoryStore struct {
	mu      sync.RWMutex
	entries map[string]entry
	locks   map[string]memoryLock
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		entries: make(map[string]entry),
		locks:   make(map[string]memoryLock),
	}
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

func (s *MemoryStore) Update(_ context.Context, job Job) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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

func (s *MemoryStore) AcquireLock(_ context.Context, key string, owner string, ttl time.Duration) (bool, error) {
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	lock, ok := s.locks[key]
	if ok && lock.expiresAt.After(now) && lock.owner != owner {
		return false, nil
	}

	s.locks[key] = memoryLock{
		owner:     owner,
		expiresAt: now.Add(ttl),
	}
	return true, nil
}

func (s *MemoryStore) ReleaseLock(_ context.Context, key string, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	lock, ok := s.locks[key]
	if !ok {
		return nil
	}
	if lock.owner == owner {
		delete(s.locks, key)
	}
	return nil
}

type memoryLock struct {
	owner     string
	expiresAt time.Time
}
