package jobstore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

const jobTTL = 7 * 24 * time.Hour

type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(client *redis.Client) *RedisStore {
	return &RedisStore{client: client}
}

func (s *RedisStore) Save(ctx context.Context, job Job) (Job, error) {
	key := "job:" + job.ID

	data, err := json.Marshal(job)
	if err != nil {
		return Job{}, err
	}

	// SET NX: only set if key does not exist (idempotent)
	set, err := s.client.SetNX(ctx, key, data, jobTTL).Result()
	if err != nil {
		return Job{}, err
	}

	if !set {
		// Key already existed — return the stored job
		return s.GetJob(ctx, job.ID)
	}

	return job, nil
}

func (s *RedisStore) GetJob(ctx context.Context, jobID string) (Job, error) {
	data, err := s.client.Get(ctx, "job:"+jobID).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return Job{}, ErrNotFound
		}
		return Job{}, err
	}

	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return Job{}, err
	}
	return job, nil
}
