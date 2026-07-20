package readcache

import (
	"context"
	"fmt"
	"time"

	redis "github.com/redis/go-redis/v9"
)

type SetItem struct {
	Key   string
	Value []byte
	TTL   time.Duration
}

type MultiStore interface {
	Store
	MGet(ctx context.Context, keys []string) ([][]byte, error)
	SetMany(ctx context.Context, items []SetItem) error
}

func (s *RedisStore) MGet(ctx context.Context, keys []string) ([][]byte, error) {
	if len(keys) == 0 {
		return [][]byte{}, nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		values, err := s.client.MGet(ctx, keys...).Result()
		if err == nil {
			out := make([][]byte, len(values))
			for index, value := range values {
				switch typed := value.(type) {
				case nil:
				case string:
					out[index] = []byte(typed)
				case []byte:
					out[index] = append([]byte(nil), typed...)
				default:
					return nil, fmt.Errorf("decode Redis MGET value %d: unsupported type %T", index, value)
				}
			}
			return out, nil
		}
		if ctx.Err() != nil || attempt == 1 {
			return nil, err
		}
	}
	panic("unreachable")
}

func (s *RedisStore) SetMany(ctx context.Context, items []SetItem) error {
	if len(items) == 0 {
		return nil
	}
	_, err := s.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, item := range items {
			pipe.Set(ctx, item.Key, item.Value, item.TTL)
		}
		return nil
	})
	return err
}
