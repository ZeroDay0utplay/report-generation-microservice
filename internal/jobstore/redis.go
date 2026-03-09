package jobstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const jobTTL = 7 * 24 * time.Hour
const maxUpdateRetries = 8

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

func (s *RedisStore) Update(ctx context.Context, jobID string, mutate func(*Job) error) (Job, error) {
	key := "job:" + jobID
	var updated Job

	for attempt := 0; attempt < maxUpdateRetries; attempt++ {
		err := s.client.Watch(ctx, func(tx *redis.Tx) error {
			data, err := tx.Get(ctx, key).Bytes()
			if err != nil {
				if errors.Is(err, redis.Nil) {
					return ErrNotFound
				}
				return err
			}

			var current Job
			if err := json.Unmarshal(data, &current); err != nil {
				return fmt.Errorf("unmarshal job %q: %w", jobID, err)
			}

			if err := mutate(&current); err != nil {
				return err
			}

			next, err := json.Marshal(current)
			if err != nil {
				return fmt.Errorf("marshal job %q: %w", jobID, err)
			}

			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, key, next, jobTTL)
				return nil
			})
			if err != nil {
				return err
			}

			updated = current
			return nil
		}, key)

		if err == nil {
			return updated, nil
		}
		if errors.Is(err, redis.TxFailedErr) {
			continue
		}
		return Job{}, err
	}

	return Job{}, ErrConflict
}

func (s *RedisStore) List(ctx context.Context, limit int) ([]Job, error) {
	const scanBatch = 200

	keys := make([]string, 0, scanBatch)
	var cursor uint64

	for {
		found, next, err := s.client.Scan(ctx, cursor, "job:*", scanBatch).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, found...)
		cursor = next

		if cursor == 0 {
			break
		}
		if limit > 0 && len(keys) >= limit {
			break
		}
	}

	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}
	if len(keys) == 0 {
		return nil, nil
	}

	vals, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}

	out := make([]Job, 0, len(vals))
	for _, raw := range vals {
		if raw == nil {
			continue
		}

		var payload []byte
		switch v := raw.(type) {
		case string:
			payload = []byte(v)
		case []byte:
			payload = v
		default:
			payload = []byte(strings.TrimSpace(fmt.Sprint(v)))
		}

		var job Job
		if err := json.Unmarshal(payload, &job); err != nil {
			continue
		}
		out = append(out, job)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}
