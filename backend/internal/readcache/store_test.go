package readcache

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

type scriptedRedisCommandHook struct {
	mu       sync.Mutex
	failures map[string][]error
	calls    map[string]int
	after    func(command string, attempt int)
}

func newScriptedRedisCommandHook(failures map[string][]error) *scriptedRedisCommandHook {
	return &scriptedRedisCommandHook{
		failures: failures,
		calls:    make(map[string]int),
	}
}

func (h *scriptedRedisCommandHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return next(ctx, network, addr)
	}
}

func (h *scriptedRedisCommandHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		command := cmd.Name()
		h.mu.Lock()
		attempt := h.calls[command]
		h.calls[command] = attempt + 1
		var failure error
		if attempt < len(h.failures[command]) {
			failure = h.failures[command][attempt]
		}
		after := h.after
		h.mu.Unlock()
		if after != nil {
			after(command, attempt)
		}
		if failure != nil {
			return failure
		}
		return next(ctx, cmd)
	}
}

func (h *scriptedRedisCommandHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		return next(ctx, cmds)
	}
}

func (h *scriptedRedisCommandHook) callCount(command string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls[command]
}

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

func TestRedisStoreMGetPreservesOrderAndMissPositions(t *testing.T) {
	server := miniredis.RunT(t)
	server.Set("first", "alpha")
	server.Set("third", "charlie")
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	values, err := NewRedisStore(client).MGet(context.Background(), "first", "second", "third", "fourth")
	if err != nil {
		t.Fatalf("MGet() error = %v", err)
	}
	if len(values) != 4 {
		t.Fatalf("MGet() value count = %d, want 4", len(values))
	}
	want := []string{"alpha", "", "charlie", ""}
	for index, value := range values {
		if string(value) != want[index] {
			t.Fatalf("MGet() value[%d] = %q, want %q", index, value, want[index])
		}
	}
	if values[1] != nil || values[3] != nil {
		t.Fatalf("MGet() misses = %#v/%#v, want nil positions", values[1], values[3])
	}
}

func TestRedisStoreSetIfLeaseOwnedAndReleaseAreTokenChecked(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRedisStore(client)
	ctx := context.Background()

	acquired, err := store.TryAcquireLease(ctx, "lease", "owner-a", 30*time.Second)
	if err != nil || !acquired {
		t.Fatalf("TryAcquireLease() = %v, %v", acquired, err)
	}
	published, err := store.SetIfLeaseOwned(ctx, "lease", "owner-b", "manifest", []byte("wrong"), 3*time.Minute)
	if err != nil || published {
		t.Fatalf("SetIfLeaseOwned(wrong token) = %v, %v, want false, nil", published, err)
	}
	if server.Exists("manifest") {
		t.Fatal("wrong-token publication created manifest")
	}

	published, err = store.SetIfLeaseOwned(ctx, "lease", "owner-a", "manifest", []byte("committed"), 3*time.Minute)
	if err != nil || !published {
		t.Fatalf("SetIfLeaseOwned(owner token) = %v, %v, want true, nil", published, err)
	}
	if value, getErr := server.Get("manifest"); getErr != nil || value != "committed" {
		t.Fatalf("manifest = %q, %v, want committed", value, getErr)
	}
	if ttl := server.TTL("manifest"); ttl != 3*time.Minute {
		t.Fatalf("manifest TTL = %s, want 3m", ttl)
	}

	released, err := store.ReleaseLease(ctx, "lease", "owner-b")
	if err != nil || released {
		t.Fatalf("ReleaseLease(wrong token) = %v, %v, want false, nil", released, err)
	}
	released, err = store.ReleaseLease(ctx, "lease", "owner-a")
	if err != nil || !released {
		t.Fatalf("ReleaseLease(owner token) = %v, %v, want true, nil", released, err)
	}
}

func TestRedisStoreSetIfLeasesOwnedRequiresEveryTokenAtomically(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRedisStore(client)
	ctx := context.Background()
	leaseKeys := []string{"coordinator", "segment"}
	tokens := []string{"cycle-owner", "segment-owner"}
	for index := range leaseKeys {
		acquired, err := store.TryAcquireLease(ctx, leaseKeys[index], tokens[index], time.Minute)
		if err != nil || !acquired {
			t.Fatalf("TryAcquireLease(%s) = %v, %v", leaseKeys[index], acquired, err)
		}
	}

	published, err := store.SetIfLeasesOwned(ctx, leaseKeys, []string{tokens[0], "wrong"}, "manifest", []byte("wrong"), 3*time.Minute)
	if err != nil || published || server.Exists("manifest") {
		t.Fatalf("SetIfLeasesOwned(wrong token) = %v, %v, exists=%v", published, err, server.Exists("manifest"))
	}
	server.Del(leaseKeys[0])
	published, err = store.SetIfLeasesOwned(ctx, leaseKeys, tokens, "manifest", []byte("missing"), 3*time.Minute)
	if err != nil || published || server.Exists("manifest") {
		t.Fatalf("SetIfLeasesOwned(missing lease) = %v, %v, exists=%v", published, err, server.Exists("manifest"))
	}
	acquired, err := store.TryAcquireLease(ctx, leaseKeys[0], tokens[0], time.Minute)
	if err != nil || !acquired {
		t.Fatalf("reacquire coordinator = %v, %v", acquired, err)
	}
	published, err = store.SetIfLeasesOwned(ctx, leaseKeys, tokens, "manifest", []byte("committed"), 3*time.Minute)
	if err != nil || !published {
		t.Fatalf("SetIfLeasesOwned(all owned) = %v, %v", published, err)
	}
	if value, getErr := server.Get("manifest"); getErr != nil || value != "committed" {
		t.Fatalf("manifest = %q, %v, want committed", value, getErr)
	}
}

func TestRedisStoreGetRetriesOneCommandError(t *testing.T) {
	server := miniredis.RunT(t)
	server.Set("value", "payload")
	firstErr := errors.New("synthetic first GET failure")
	hook := newScriptedRedisCommandHook(map[string][]error{"get": {firstErr}})
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	client.AddHook(hook)
	t.Cleanup(func() { _ = client.Close() })

	value, err := NewRedisStore(client).Get(context.Background(), "value")
	if err != nil || string(value) != "payload" {
		t.Fatalf("Get() = %q, %v, want payload after one retry", value, err)
	}
	if got := hook.callCount("get"); got != 2 {
		t.Fatalf("GET attempts = %d, want 2", got)
	}
}

func TestRedisStoreGetRetryCanReturnMiss(t *testing.T) {
	server := miniredis.RunT(t)
	firstErr := errors.New("synthetic first GET failure")
	hook := newScriptedRedisCommandHook(map[string][]error{"get": {firstErr}})
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	client.AddHook(hook)
	t.Cleanup(func() { _ = client.Close() })

	_, err := NewRedisStore(client).Get(context.Background(), "missing")
	if !errors.Is(err, ErrMiss) || hook.callCount("get") != 2 {
		t.Fatalf("Get(missing) error/attempts = %v/%d, want ErrMiss/2", err, hook.callCount("get"))
	}
}

func TestRedisStoreGetReturnsSecondCommandError(t *testing.T) {
	server := miniredis.RunT(t)
	firstErr := errors.New("synthetic first GET failure")
	secondErr := errors.New("synthetic second GET failure")
	hook := newScriptedRedisCommandHook(map[string][]error{"get": {firstErr, secondErr}})
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	client.AddHook(hook)
	t.Cleanup(func() { _ = client.Close() })

	_, err := NewRedisStore(client).Get(context.Background(), "value")
	if !errors.Is(err, secondErr) || hook.callCount("get") != 2 {
		t.Fatalf("Get() error/attempts = %v/%d, want second error/2", err, hook.callCount("get"))
	}
}

func TestRedisStoreGetDoesNotRetryAfterContextCancellation(t *testing.T) {
	server := miniredis.RunT(t)
	firstErr := errors.New("synthetic first GET failure")
	hook := newScriptedRedisCommandHook(map[string][]error{"get": {firstErr}})
	ctx, cancel := context.WithCancel(context.Background())
	hook.after = func(command string, attempt int) {
		if command == "get" && attempt == 0 {
			cancel()
		}
	}
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	client.AddHook(hook)
	t.Cleanup(func() { _ = client.Close() })

	_, err := NewRedisStore(client).Get(ctx, "value")
	if !errors.Is(err, firstErr) || hook.callCount("get") != 1 {
		t.Fatalf("Get() error/attempts = %v/%d, want first error/1", err, hook.callCount("get"))
	}
}

func TestRedisStoreGetDoesNotRetryPreExpiredContext(t *testing.T) {
	server := miniredis.RunT(t)
	hook := newScriptedRedisCommandHook(nil)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	client.AddHook(hook)
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, err := NewRedisStore(client).Get(ctx, "value")
	if !errors.Is(err, context.DeadlineExceeded) || hook.callCount("get") != 1 {
		t.Fatalf("Get() error/attempts = %v/%d, want context.DeadlineExceeded/1", err, hook.callCount("get"))
	}
}

func TestRedisStoreGetOrdinaryResultsUseOneCommand(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		value     string
		wantValue string
		wantErr   error
	}{
		{name: "value", key: "value", value: "payload", wantValue: "payload"},
		{name: "miss", key: "missing", wantErr: ErrMiss},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := miniredis.RunT(t)
			if tt.value != "" {
				server.Set(tt.key, tt.value)
			}
			hook := newScriptedRedisCommandHook(nil)
			client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
			client.AddHook(hook)
			t.Cleanup(func() { _ = client.Close() })

			value, err := NewRedisStore(client).Get(context.Background(), tt.key)
			if string(value) != tt.wantValue || !errors.Is(err, tt.wantErr) {
				t.Fatalf("Get(%q) = %q, %v, want %q, %v", tt.key, value, err, tt.wantValue, tt.wantErr)
			}
			if got := hook.callCount("get"); got != 1 {
				t.Fatalf("GET attempts = %d, want 1", got)
			}
		})
	}
}

func TestRedisStoreRetryIsGetOnly(t *testing.T) {
	tests := []struct {
		name    string
		command string
		invoke  func(*RedisStore) error
	}{
		{
			name:    "set",
			command: "set",
			invoke: func(store *RedisStore) error {
				return store.Set(context.Background(), "value", []byte("payload"), time.Minute)
			},
		},
		{
			name:    "try acquire lease",
			command: "set",
			invoke: func(store *RedisStore) error {
				_, err := store.TryAcquireLease(context.Background(), "lease", "owner", time.Minute)
				return err
			},
		},
		{
			name:    "lease ttl",
			command: "pttl",
			invoke: func(store *RedisStore) error {
				_, err := store.LeaseTTL(context.Background(), "lease")
				return err
			},
		},
		{
			name:    "release lease",
			command: "evalsha",
			invoke: func(store *RedisStore) error {
				_, err := store.ReleaseLease(context.Background(), "lease", "owner")
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := miniredis.RunT(t)
			commandErr := errors.New("synthetic command failure")
			hook := newScriptedRedisCommandHook(map[string][]error{tt.command: {commandErr}})
			client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
			client.AddHook(hook)
			t.Cleanup(func() { _ = client.Close() })

			err := tt.invoke(NewRedisStore(client))
			if !errors.Is(err, commandErr) || hook.callCount(tt.command) != 1 {
				t.Fatalf("%s error/attempts = %v/%d, want synthetic error/1", tt.name, err, hook.callCount(tt.command))
			}
		})
	}
}
