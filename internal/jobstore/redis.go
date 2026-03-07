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

var unlockScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)

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

func (s *RedisStore) Update(ctx context.Context, job Job) (Job, error) {
	key := "job:" + job.ID

	data, err := json.Marshal(job)
	if err != nil {
		return Job{}, err
	}

	if err := s.client.Set(ctx, key, data, jobTTL).Err(); err != nil {
		return Job{}, err
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

func (s *RedisStore) AcquireLock(ctx context.Context, key string, owner string, ttl time.Duration) (bool, error) {
	return s.client.SetNX(ctx, key, owner, ttl).Result()
}

func (s *RedisStore) ReleaseLock(ctx context.Context, key string, owner string) error {
	_, err := unlockScript.Run(ctx, s.client, []string{key}, owner).Result()
	if err != nil && errors.Is(err, redis.Nil) {
		return nil
	}
	return err
}
