package jobstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const jobTTL = 72 * time.Hour

type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(client *redis.Client) *RedisStore {
	return &RedisStore{client: client}
}

func redisKey(jobID string) string { return "job:" + jobID }

func (s *RedisStore) Create(ctx context.Context, job Job) (Job, error) {
	key := redisKey(job.ID)
	now := time.Now()
	job.CreatedAt = now
	job.UpdatedAt = now

	data, err := json.Marshal(job)
	if err != nil {
		return Job{}, fmt.Errorf("marshal job: %w", err)
	}

	set, err := s.client.SetNX(ctx, key, data, jobTTL).Result()
	if err != nil {
		return Job{}, fmt.Errorf("redis setnx: %w", err)
	}
	if !set {
		return s.GetJob(ctx, job.ID)
	}
	return job, nil
}

func (s *RedisStore) Update(ctx context.Context, jobID string, fields map[string]any) error {
	key := redisKey(jobID)

	existing, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return ErrNotFound
		}
		return fmt.Errorf("redis get for update: %w", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(existing, &raw); err != nil {
		return fmt.Errorf("unmarshal job for update: %w", err)
	}

	for k, v := range fields {
		raw[k] = v
	}
	raw["updatedAt"] = time.Now().Format(time.RFC3339Nano)

	data, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal updated job: %w", err)
	}

	return s.client.Set(ctx, key, data, jobTTL).Err()
}

func (s *RedisStore) GetJob(ctx context.Context, jobID string) (Job, error) {
	data, err := s.client.Get(ctx, redisKey(jobID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return Job{}, ErrNotFound
		}
		return Job{}, fmt.Errorf("redis get: %w", err)
	}

	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return Job{}, fmt.Errorf("unmarshal job: %w", err)
	}
	return job, nil
}

func (s *RedisStore) AppendEmails(ctx context.Context, jobID string, emails []string) error {
	key := redisKey(jobID)

	existing, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return ErrNotFound
		}
		return fmt.Errorf("redis get for emails: %w", err)
	}

	var job Job
	if err := json.Unmarshal(existing, &job); err != nil {
		return fmt.Errorf("unmarshal job for emails: %w", err)
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

	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal job after email append: %w", err)
	}

	return s.client.Set(ctx, key, data, jobTTL).Err()
}
