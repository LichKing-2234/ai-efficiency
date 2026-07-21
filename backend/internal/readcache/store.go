package readcache

import (
	"context"
	"errors"
	"fmt"
	"time"

	redis "github.com/redis/go-redis/v9"
)

var ErrMiss = errors.New("read cache miss")

var releaseLeaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0`)

var setIfLeasesOwnedScript = redis.NewScript(`
local lease_count = tonumber(ARGV[1])
for index = 1, lease_count do
  if redis.call("GET", KEYS[index]) ~= ARGV[index + 1] then
    return 0
  end
end
redis.call("SET", KEYS[lease_count + 1], ARGV[lease_count + 2], "PX", ARGV[lease_count + 3])
return 1`)

type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	TryAcquireLease(ctx context.Context, key, token string, ttl time.Duration) (bool, error)
	LeaseTTL(ctx context.Context, key string) (time.Duration, error)
	ReleaseLease(ctx context.Context, key, token string) (bool, error)
}

type BatchStore interface {
	Store
	MGet(context.Context, ...string) ([][]byte, error)
	SetIfLeaseOwned(context.Context, string, string, string, []byte, time.Duration) (bool, error)
	SetIfLeasesOwned(context.Context, []string, []string, string, []byte, time.Duration) (bool, error)
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

func (s *RedisStore) MGet(ctx context.Context, keys ...string) ([][]byte, error) {
	if len(keys) == 0 {
		return [][]byte{}, nil
	}
	results, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	values := make([][]byte, len(results))
	for index, result := range results {
		switch value := result.(type) {
		case nil:
		case string:
			values[index] = []byte(value)
		case []byte:
			values[index] = append([]byte(nil), value...)
		default:
			return nil, fmt.Errorf("decode Redis MGET result %d: unexpected %T", index, result)
		}
	}
	return values, nil
}

func (s *RedisStore) SetIfLeaseOwned(
	ctx context.Context,
	leaseKey, token, key string,
	value []byte,
	ttl time.Duration,
) (bool, error) {
	return s.SetIfLeasesOwned(ctx, []string{leaseKey}, []string{token}, key, value, ttl)
}

func (s *RedisStore) SetIfLeasesOwned(
	ctx context.Context,
	leaseKeys, tokens []string,
	key string,
	value []byte,
	ttl time.Duration,
) (bool, error) {
	if len(leaseKeys) == 0 || len(leaseKeys) != len(tokens) {
		return false, fmt.Errorf("lease keys and tokens must have the same non-zero length")
	}
	keys := append(append(make([]string, 0, len(leaseKeys)+1), leaseKeys...), key)
	arguments := make([]any, 0, len(tokens)+3)
	arguments = append(arguments, len(tokens))
	for _, token := range tokens {
		arguments = append(arguments, token)
	}
	arguments = append(arguments, value, ttl.Milliseconds())
	result, err := setIfLeasesOwnedScript.Run(ctx, s.client, keys, arguments...).Int64()
	if err != nil {
		return false, err
	}
	return result == 1, nil
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
