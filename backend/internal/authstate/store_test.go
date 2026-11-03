package authstate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

const testTTL = 5 * time.Minute

func newRedisTestStore(t *testing.T) (*RedisStore, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedisStore(client, "test-ns"), server
}

func newMemoryTestStore(t *testing.T) (*MemoryStore, func(time.Duration)) {
	t.Helper()
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	store := newMemoryStore(func() time.Time { return now })
	advance := func(d time.Duration) { now = now.Add(d) }
	return store, advance
}

type storeFactory struct {
	name  string
	build func(*testing.T) (Store, func(time.Duration))
}

func storeFactories() []storeFactory {
	return []storeFactory{
		{name: "memory", build: func(t *testing.T) (Store, func(time.Duration)) {
			store, advance := newMemoryTestStore(t)
			return store, advance
		}},
		{name: "redis", build: func(t *testing.T) (Store, func(time.Duration)) {
			store, server := newRedisTestStore(t)
			return store, server.FastForward
		}},
	}
}

func TestStoreSetGetTake(t *testing.T) {
	for _, factory := range storeFactories() {
		t.Run(factory.name, func(t *testing.T) {
			ctx := context.Background()
			store, _ := factory.build(t)

			if err := store.Set(ctx, "key", []byte("value"), testTTL); err != nil {
				t.Fatalf("Set() error = %v", err)
			}
			value, ok, err := store.Get(ctx, "key")
			if err != nil || !ok || string(value) != "value" {
				t.Fatalf("Get() = %q, %v, %v", value, ok, err)
			}
			value, ok, err = store.Take(ctx, "key")
			if err != nil || !ok || string(value) != "value" {
				t.Fatalf("Take() = %q, %v, %v", value, ok, err)
			}
			_, ok, err = store.Take(ctx, "key")
			if err != nil || ok {
				t.Fatalf("second Take() ok = %v, err = %v, want missing", ok, err)
			}
		})
	}
}

func TestStoreSetIfAbsent(t *testing.T) {
	for _, factory := range storeFactories() {
		t.Run(factory.name, func(t *testing.T) {
			ctx := context.Background()
			store, _ := factory.build(t)

			stored, err := store.SetIfAbsent(ctx, "key", []byte("first"), testTTL)
			if err != nil || !stored {
				t.Fatalf("SetIfAbsent() = %v, %v", stored, err)
			}
			stored, err = store.SetIfAbsent(ctx, "key", []byte("second"), testTTL)
			if err != nil || stored {
				t.Fatalf("second SetIfAbsent() = %v, %v", stored, err)
			}
			value, _, _ := store.Get(ctx, "key")
			if string(value) != "first" {
				t.Fatalf("value = %q, want first", value)
			}
		})
	}
}

func TestStoreUpdate(t *testing.T) {
	for _, factory := range storeFactories() {
		t.Run(factory.name, func(t *testing.T) {
			ctx := context.Background()
			store, _ := factory.build(t)

			if _, err := store.Update(ctx, "missing", testTTL, func([]byte) ([]byte, error) {
				t.Fatal("mutate must not run for a missing key")
				return nil, nil
			}); err != nil {
				t.Fatalf("Update() missing error = %v", err)
			}

			if err := store.Set(ctx, "counter", []byte("1"), testTTL); err != nil {
				t.Fatalf("Set() error = %v", err)
			}
			found, err := store.Update(ctx, "counter", testTTL, func(current []byte) ([]byte, error) {
				if string(current) != "1" {
					t.Fatalf("current = %q, want 1", current)
				}
				return []byte("2"), nil
			})
			if err != nil || !found {
				t.Fatalf("Update() = %v, %v", found, err)
			}
			value, _, _ := store.Get(ctx, "counter")
			if string(value) != "2" {
				t.Fatalf("value = %q, want 2", value)
			}

			found, err = store.Update(ctx, "counter", testTTL, func([]byte) ([]byte, error) {
				return nil, ErrUpdateAborted
			})
			if !found || !errors.Is(err, ErrUpdateAborted) {
				t.Fatalf("aborted Update() = %v, %v", found, err)
			}
			value, _, _ = store.Get(ctx, "counter")
			if string(value) != "2" {
				t.Fatalf("value after abort = %q, want 2", value)
			}
		})
	}
}

func TestStoreExpiry(t *testing.T) {
	for _, factory := range storeFactories() {
		t.Run(factory.name, func(t *testing.T) {
			ctx := context.Background()
			store, advance := factory.build(t)

			if err := store.Set(ctx, "key", []byte("value"), time.Minute); err != nil {
				t.Fatalf("Set() error = %v", err)
			}
			advance(2 * time.Minute)
			_, ok, err := store.Get(ctx, "key")
			if err != nil || ok {
				t.Fatalf("Get() after expiry ok = %v, err = %v", ok, err)
			}
		})
	}
}

func TestStoreConcurrentTakeObservesAtMostOnce(t *testing.T) {
	for _, factory := range storeFactories() {
		t.Run(factory.name, func(t *testing.T) {
			ctx := context.Background()
			store, _ := factory.build(t)
			if err := store.Set(ctx, "key", []byte("value"), testTTL); err != nil {
				t.Fatalf("Set() error = %v", err)
			}

			var wg sync.WaitGroup
			var mu sync.Mutex
			observed := 0
			for range 8 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, ok, err := store.Take(ctx, "key")
					if err != nil {
						t.Errorf("Take() error = %v", err)
						return
					}
					if ok {
						mu.Lock()
						observed++
						mu.Unlock()
					}
				}()
			}
			wg.Wait()
			if observed != 1 {
				t.Fatalf("observed %d values, want 1", observed)
			}
		})
	}
}

func TestRedisStoreIsolatesNamespaces(t *testing.T) {
	ctx := context.Background()
	first, server := newRedisTestStore(t)
	second := NewRedisStore(first.client, "other-ns")

	if err := first.Set(ctx, "key", []byte("first"), testTTL); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if _, ok, _ := second.Get(ctx, "key"); ok {
		t.Fatal("namespace isolation failed: second store observed first store value")
	}
	if !server.Exists("test-ns:auth:state:key") {
		t.Fatal("expected namespaced key in Redis")
	}
}

func TestStoreRejectsInvalidTTL(t *testing.T) {
	ctx := context.Background()
	for _, factory := range storeFactories() {
		t.Run(factory.name, func(t *testing.T) {
			store, _ := factory.build(t)
			if err := store.Set(ctx, "key", []byte("value"), 0); err == nil {
				t.Fatal("Set() accepted zero TTL")
			}
			if _, err := store.SetIfAbsent(ctx, "key", []byte("value"), -time.Second); err == nil {
				t.Fatal("SetIfAbsent() accepted negative TTL")
			}
		})
	}
}
