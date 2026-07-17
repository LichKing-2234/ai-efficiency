package readcache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

func TestRedisStoreImplementsValueAndTokenProtectedLeaseContract(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRedisStore(client)
	ctx := context.Background()

	if _, err := store.Get(ctx, "missing"); !errors.Is(err, ErrMiss) {
		t.Fatalf("Get(missing) error = %v, want ErrMiss", err)
	}
	if err := store.Set(ctx, "value", []byte("payload"), 25*time.Second); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	value, err := store.Get(ctx, "value")
	if err != nil || string(value) != "payload" {
		t.Fatalf("Get(value) = %q, %v", value, err)
	}
	if ttl := server.TTL("value"); ttl != 25*time.Second {
		t.Fatalf("value TTL = %s, want 25s", ttl)
	}

	acquired, err := store.TryAcquireLease(ctx, "lease", "owner-a", 10*time.Second)
	if err != nil || !acquired {
		t.Fatalf("first lease acquire = %v, %v", acquired, err)
	}
	acquired, err = store.TryAcquireLease(ctx, "lease", "owner-b", 10*time.Second)
	if err != nil || acquired {
		t.Fatalf("second lease acquire = %v, %v", acquired, err)
	}
	leaseTTL, err := store.LeaseTTL(ctx, "lease")
	if err != nil || leaseTTL != 10*time.Second {
		t.Fatalf("LeaseTTL() = %s, %v", leaseTTL, err)
	}
	released, err := store.ReleaseLease(ctx, "lease", "owner-b")
	if err != nil || released {
		t.Fatalf("wrong-token release = %v, %v", released, err)
	}
	released, err = store.ReleaseLease(ctx, "lease", "owner-a")
	if err != nil || !released {
		t.Fatalf("owner release = %v, %v", released, err)
	}
	if _, err := store.LeaseTTL(ctx, "lease"); !errors.Is(err, ErrMiss) {
		t.Fatalf("LeaseTTL(released) error = %v, want ErrMiss", err)
	}
}
