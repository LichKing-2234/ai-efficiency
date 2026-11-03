// Package authstate stores the short-lived state that completes an interactive
// browser or CLI login: OIDC pending authorizations and handoffs, back-channel
// logout replay markers, OAuth authorization codes, and device authorizations.
//
// Every value has an explicit expiry and callers treat the backend as an
// ephemeral store. The Redis implementation lets any replica complete a flow
// that another replica started, which makes rolling updates safe and leaves
// room for more than one backend Pod. The memory implementation keeps local
// tests and deployments without a shared store working; it is process-local and
// cannot complete a flow on a different process.
package authstate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	redis "github.com/redis/go-redis/v9"
)

// ErrUnavailable marks a transient state backend failure such as a Redis
// connection error. Callers may map it to an unavailable response instead of a
// credential failure.
var ErrUnavailable = errors.New("auth state store unavailable")

// ErrUpdateAborted lets an Update mutation reject a change without treating the
// entry as missing or the backend as failed.
var ErrUpdateAborted = errors.New("auth state update aborted")

// Store is the ephemeral key/value contract used by login flows.
type Store interface {
	// Set stores value under key for ttl.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	// Get returns the current value and whether it is present.
	Get(ctx context.Context, key string) ([]byte, bool, error)
	// Take atomically reads and deletes the value. At most one caller can
	// observe a present value.
	Take(ctx context.Context, key string) ([]byte, bool, error)
	// SetIfAbsent stores value only when key is absent. It reports whether the
	// value was stored.
	SetIfAbsent(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error)
	// Update atomically applies mutate to the current value. It reports whether
	// the key was present. A mutate error aborts the update; ErrUpdateAborted is
	// returned to the caller.
	Update(ctx context.Context, key string, ttl time.Duration, mutate func(current []byte) ([]byte, error)) (bool, error)
	// Delete removes a value. Deleting an absent key is not an error.
	Delete(ctx context.Context, key string) error
}

// Unavailable reports whether err should be treated as a transient backend
// failure rather than a missing or invalid credential.
func Unavailable(err error) bool {
	return errors.Is(err, ErrUnavailable)
}

func validateTTL(ttl time.Duration) error {
	if ttl <= 0 {
		return fmt.Errorf("auth state TTL must be positive")
	}
	return nil
}

// RedisStore is the shared Redis implementation of Store.
type RedisStore struct {
	client redis.UniversalClient
	prefix string
}

// NewRedisStore builds a Redis-backed store. Keys are prefixed with the
// deployment namespace so environments that share a Redis instance cannot read
// each other's login state.
func NewRedisStore(client redis.UniversalClient, namespace string) *RedisStore {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = "ai-efficiency"
	}
	return &RedisStore{
		client: client,
		prefix: namespace + ":auth:state:",
	}
}

func (s *RedisStore) key(key string) string {
	return s.prefix + key
}

func (s *RedisStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := validateTTL(ttl); err != nil {
		return err
	}
	if err := s.client.Set(ctx, s.key(key), value, ttl).Err(); err != nil {
		return wrapUnavailable(err)
	}
	return nil
}

func (s *RedisStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	value, err := s.client.Get(ctx, s.key(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, wrapUnavailable(err)
	}
	return value, true, nil
}

var takeScript = redis.NewScript(`
local value = redis.call("GET", KEYS[1])
if value then
  redis.call("DEL", KEYS[1])
end
return value`)

func (s *RedisStore) Take(ctx context.Context, key string) ([]byte, bool, error) {
	result, err := takeScript.Run(ctx, s.client, []string{s.key(key)}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, wrapUnavailable(err)
	}
	if result == nil {
		return nil, false, nil
	}
	value, ok := result.(string)
	if !ok {
		return nil, false, fmt.Errorf("%w: unexpected take result %T", ErrUnavailable, result)
	}
	return []byte(value), true, nil
}

func (s *RedisStore) SetIfAbsent(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	if err := validateTTL(ttl); err != nil {
		return false, err
	}
	stored, err := s.client.SetNX(ctx, s.key(key), value, ttl).Result()
	if err != nil {
		return false, wrapUnavailable(err)
	}
	return stored, nil
}

func (s *RedisStore) Update(ctx context.Context, key string, ttl time.Duration, mutate func(current []byte) ([]byte, error)) (bool, error) {
	if err := validateTTL(ttl); err != nil {
		return false, err
	}
	fullKey := s.key(key)
	for attempt := 0; attempt < 3; attempt++ {
		var aborted error
		err := s.client.Watch(ctx, func(tx *redis.Tx) error {
			current, err := tx.Get(ctx, fullKey).Bytes()
			if errors.Is(err, redis.Nil) {
				return errKeyMissing
			}
			if err != nil {
				return err
			}
			next, err := mutate(current)
			if err != nil {
				aborted = err
				return errKeyPresent
			}
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, fullKey, next, ttl)
				return nil
			})
			return err
		}, fullKey)
		switch {
		case errors.Is(err, errKeyMissing):
			return false, nil
		case errors.Is(err, errKeyPresent):
			return true, aborted
		case errors.Is(err, redis.TxFailedErr):
			continue
		case err != nil:
			return false, wrapUnavailable(err)
		default:
			return true, nil
		}
	}
	return false, fmt.Errorf("%w: update contention", ErrUnavailable)
}

func (s *RedisStore) Delete(ctx context.Context, key string) error {
	if err := s.client.Del(ctx, s.key(key)).Err(); err != nil {
		return wrapUnavailable(err)
	}
	return nil
}

var (
	errKeyMissing = errors.New("auth state key missing")
	errKeyPresent = errors.New("auth state key present")
)

func wrapUnavailable(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}
