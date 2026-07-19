package readcache

import (
	"context"
	"errors"
	"time"

	redis "github.com/redis/go-redis/v9"
)

var ErrMiss = errors.New("read cache miss")

var releaseLeaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0`)

type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	TryAcquireLease(ctx context.Context, key, token string, ttl time.Duration) (bool, error)
	LeaseTTL(ctx context.Context, key string) (time.Duration, error)
	ReleaseLease(ctx context.Context, key, token string) (bool, error)
}

type RedisStore struct {
	client redis.UniversalClient
}

func NewRedisStore(client redis.UniversalClient) *RedisStore {
	return &RedisStore{client: client}
}

func (s *RedisStore) Get(ctx context.Context, key string) ([]byte, error) {
	for attempt := 0; attempt < 2; attempt++ {
		value, err := s.client.Get(ctx, key).Bytes()
		if err == nil {
			return value, nil
		}
		if errors.Is(err, redis.Nil) {
			return nil, ErrMiss
		}
		if ctx.Err() != nil || attempt == 1 {
			return nil, err
		}
	}
	panic("unreachable")
}

func (s *RedisStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return s.client.Set(ctx, key, value, ttl).Err()
}

func (s *RedisStore) TryAcquireLease(ctx context.Context, key, token string, ttl time.Duration) (bool, error) {
	return s.client.SetNX(ctx, key, token, ttl).Result()
}

func (s *RedisStore) LeaseTTL(ctx context.Context, key string) (time.Duration, error) {
	ttl, err := s.client.PTTL(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if ttl <= 0 {
		return 0, ErrMiss
	}
	return ttl, nil
}

func (s *RedisStore) ReleaseLease(ctx context.Context, key string, token string) (bool, error) {
	result, err := releaseLeaseScript.Run(ctx, s.client, []string{key}, token).Int64()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func Sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
