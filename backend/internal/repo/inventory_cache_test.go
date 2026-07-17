package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

type fakeInventoryRevisionReader struct {
	mu       sync.RWMutex
	revision string
	err      error
}

func (f *fakeInventoryRevisionReader) Current(context.Context) (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.revision, f.err
}

func (f *fakeInventoryRevisionReader) set(revision string) {
	f.mu.Lock()
	f.revision = revision
	f.mu.Unlock()
}

type fakeInventoryValue struct {
	value []byte
	ttl   time.Duration
}

type fakeInventoryLease struct {
	token     string
	expiresAt time.Time
}

type fakeInventoryStore struct {
	mu sync.Mutex

	now        time.Time
	values     map[string]fakeInventoryValue
	leases     map[string]fakeInventoryLease
	getErr     error
	acquireErr error
	setErr     error
	releaseErr error

	getCalls        int
	acquireCalls    int
	contendedLeases int
	setCalls        int
	releaseCalls    int
}

func newFakeInventoryStore() *fakeInventoryStore {
	return &fakeInventoryStore{
		now:    time.Unix(1_700_000_000, 0),
		values: make(map[string]fakeInventoryValue),
		leases: make(map[string]fakeInventoryLease),
	}
}

func (f *fakeInventoryStore) Get(_ context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	value, ok := f.values[key]
	if !ok {
		return nil, ErrInventoryCacheMiss
	}
	return append([]byte(nil), value.value...), nil
}

func (f *fakeInventoryStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCalls++
	if f.setErr != nil {
		return f.setErr
	}
	f.values[key] = fakeInventoryValue{value: append([]byte(nil), value...), ttl: ttl}
	return nil
}

func (f *fakeInventoryStore) TryAcquireLease(_ context.Context, key, token string, ttl time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acquireCalls++
	if f.acquireErr != nil {
		return false, f.acquireErr
	}
	if lease, ok := f.leases[key]; ok && lease.expiresAt.After(f.now) {
		f.contendedLeases++
		return false, nil
	}
	f.leases[key] = fakeInventoryLease{token: token, expiresAt: f.now.Add(ttl)}
	return true, nil
}

func (f *fakeInventoryStore) LeaseTTL(_ context.Context, key string) (time.Duration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	lease, ok := f.leases[key]
	if !ok {
		return 0, ErrInventoryCacheMiss
	}
	ttl := lease.expiresAt.Sub(f.now)
	if ttl <= 0 {
		delete(f.leases, key)
		return 0, ErrInventoryCacheMiss
	}
	return ttl, nil
}

func (f *fakeInventoryStore) ReleaseLease(_ context.Context, key, token string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releaseCalls++
	if f.releaseErr != nil {
		return false, f.releaseErr
	}
	lease, ok := f.leases[key]
	if !ok || lease.token != token {
		return false, nil
	}
	delete(f.leases, key)
	return true, nil
}

func (f *fakeInventoryStore) advance(duration time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(duration)
	f.mu.Unlock()
}

func testInventoryCache(t *testing.T, store InventoryStore, revisions InventoryRevisionReader, namespace string, mutate func(*InventoryCacheOptions)) *InventoryCache {
	t.Helper()
	options := InventoryCacheOptions{
		Namespace:      namespace,
		CommandTimeout: 100 * time.Millisecond,
		RefreshTimeout: 2 * time.Second,
		LeaseTTL:       250 * time.Millisecond,
		PollInterval:   time.Millisecond,
		ReleaseTimeout: 100 * time.Millisecond,
		RandFloat64:    func() float64 { return 0 },
		NewToken:       func() string { return "test-lease-token" },
	}
	if mutate != nil {
		mutate(&options)
	}
	cache, err := NewInventoryCache(store, revisions, options)
	if err != nil {
		t.Fatalf("NewInventoryCache() error = %v", err)
	}
	return cache
}

func testInventory(total int) []InventoryProviderSummary {
	return []InventoryProviderSummary{{
		ProviderKey: "scm_provider:7",
		ProviderID:  intPointer(7),
		Name:        "GitHub",
		Type:        "github",
		TotalRepos:  total,
		BoundRepos:  total,
		Scopes: []InventoryScopeSummary{{
			Scope:      "org",
			TotalRepos: total,
			BoundRepos: total,
		}},
	}}
}

func intPointer(value int) *int { return &value }

func inventoryTotal(items []InventoryProviderSummary) int {
	if len(items) == 0 {
		return 0
	}
	return items[0].TotalRepos
}

func TestInventoryCacheCollapsesFiftyConcurrentLoadsInOneProcess(t *testing.T) {
	store := newFakeInventoryStore()
	revisions := &fakeInventoryRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
	cache := testInventoryCache(t, store, revisions, "test", nil)

	var loads atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	loader := func(ctx context.Context) ([]InventoryProviderSummary, error) {
		if loads.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return testInventory(1), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	const callers = 50
	start := make(chan struct{})
	errs := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() {
			<-start
			inventory, err := cache.GetOrLoad(context.Background(), loader)
			if err == nil && inventoryTotal(inventory) != 1 {
				err = fmt.Errorf("inventory = %+v, want total 1", inventory)
			}
			errs <- err
		}()
	}
	close(start)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("authoritative loader did not start")
	}
	time.Sleep(10 * time.Millisecond)
	close(release)
	for i := 0; i < callers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("GetOrLoad() error = %v", err)
		}
	}
	if loads.Load() != 1 {
		t.Fatalf("loader calls = %d, want 1", loads.Load())
	}
}

func TestInventoryCacheCollapsesColdLoadsAcrossTwoInstances(t *testing.T) {
	store := newFakeInventoryStore()
	revisions := &fakeInventoryRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
	cacheA := testInventoryCache(t, store, revisions, "test", nil)
	cacheB := testInventoryCache(t, store, revisions, "test", nil)

	var loads atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	loader := func(ctx context.Context) ([]InventoryProviderSummary, error) {
		if loads.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return testInventory(3), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	type result struct {
		inventory []InventoryProviderSummary
		err       error
	}
	results := make(chan result, 2)
	go func() {
		inventory, err := cacheA.GetOrLoad(context.Background(), loader)
		results <- result{inventory: inventory, err: err}
	}()
	<-started
	go func() {
		inventory, err := cacheB.GetOrLoad(context.Background(), loader)
		results <- result{inventory: inventory, err: err}
	}()
	time.Sleep(10 * time.Millisecond)
	close(release)
	for i := 0; i < 2; i++ {
		result := <-results
		if result.err != nil || inventoryTotal(result.inventory) != 3 {
			t.Fatalf("GetOrLoad() inventory = %+v, error = %v", result.inventory, result.err)
		}
	}
	if loads.Load() != 1 {
		t.Fatalf("loader calls = %d, want 1", loads.Load())
	}
	store.mu.Lock()
	contended := store.contendedLeases
	store.mu.Unlock()
	if contended == 0 {
		t.Fatal("second cache never observed distributed lease")
	}
}

func TestInventoryCacheCancellationHonorsRemainingAndFinalWaiters(t *testing.T) {
	t.Run("one cancelled waiter", func(t *testing.T) {
		store := newFakeInventoryStore()
		revisions := &fakeInventoryRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
		cache := testInventoryCache(t, store, revisions, "test", nil)
		started := make(chan struct{})
		release := make(chan struct{})
		loaderCancelled := make(chan struct{}, 1)
		loader := func(ctx context.Context) ([]InventoryProviderSummary, error) {
			close(started)
			select {
			case <-release:
				return testInventory(4), nil
			case <-ctx.Done():
				loaderCancelled <- struct{}{}
				return nil, ctx.Err()
			}
		}
		firstCtx, cancelFirst := context.WithCancel(context.Background())
		firstErr := make(chan error, 1)
		go func() {
			_, err := cache.GetOrLoad(firstCtx, loader)
			firstErr <- err
		}()
		<-started
		secondResult := make(chan error, 1)
		go func() {
			inventory, err := cache.GetOrLoad(context.Background(), loader)
			if err == nil && inventoryTotal(inventory) != 4 {
				err = fmt.Errorf("inventory total = %d, want 4", inventoryTotal(inventory))
			}
			secondResult <- err
		}()
		time.Sleep(10 * time.Millisecond)
		cancelFirst()
		if err := <-firstErr; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled waiter error = %v, want context.Canceled", err)
		}
		select {
		case <-loaderCancelled:
			t.Fatal("shared loader cancelled while another waiter remained")
		default:
		}
		close(release)
		if err := <-secondResult; err != nil {
			t.Fatalf("remaining waiter error = %v", err)
		}
	})

	t.Run("final waiter", func(t *testing.T) {
		store := newFakeInventoryStore()
		revisions := &fakeInventoryRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
		cache := testInventoryCache(t, store, revisions, "test", nil)
		loaderStarted := make(chan struct{})
		loaderStopped := make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() {
			_, err := cache.GetOrLoad(ctx, func(loaderCtx context.Context) ([]InventoryProviderSummary, error) {
				close(loaderStarted)
				<-loaderCtx.Done()
				close(loaderStopped)
				return nil, loaderCtx.Err()
			})
			result <- err
		}()
		<-loaderStarted
		cancel()
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("GetOrLoad() error = %v, want context.Canceled", err)
		}
		select {
		case <-loaderStopped:
		case <-time.After(200 * time.Millisecond):
			t.Fatal("last waiter cancellation did not stop shared loader")
		}
	})
}

func TestInventoryCacheFreshHitTTLNamespaceAndRevisionIsolation(t *testing.T) {
	for _, ttlCase := range []struct {
		name   string
		random float64
		want   time.Duration
	}{
		{name: "minimum jitter", random: 0, want: 54 * time.Second},
		{name: "maximum jitter", random: 1, want: 48 * time.Second},
	} {
		t.Run(ttlCase.name, func(t *testing.T) {
			store := newFakeInventoryStore()
			revisions := &fakeInventoryRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
			cache := testInventoryCache(t, store, revisions, "alpha", func(options *InventoryCacheOptions) {
				options.RandFloat64 = func() float64 { return ttlCase.random }
			})
			var loads atomic.Int32
			loader := func(context.Context) ([]InventoryProviderSummary, error) {
				return testInventory(int(loads.Add(1))), nil
			}
			first, err := cache.GetOrLoad(context.Background(), loader)
			if err != nil {
				t.Fatalf("first GetOrLoad() error = %v", err)
			}
			second, err := cache.GetOrLoad(context.Background(), loader)
			if err != nil || inventoryTotal(first) != 1 || inventoryTotal(second) != 1 || loads.Load() != 1 {
				t.Fatalf("fresh hit first=%+v second=%+v loads=%d err=%v", first, second, loads.Load(), err)
			}
			key := inventoryCacheKey("alpha", revisions.revision)
			store.mu.Lock()
			value, ok := store.values[key]
			store.mu.Unlock()
			if !ok || value.ttl != ttlCase.want {
				t.Fatalf("stored value = %#v, want key %q TTL %s", value, key, ttlCase.want)
			}

			revisions.set("22222222-2222-4222-8222-222222222222")
			if got, err := cache.GetOrLoad(context.Background(), loader); err != nil || inventoryTotal(got) != 2 {
				t.Fatalf("new revision inventory=%+v error=%v, want total 2", got, err)
			}
			cacheB := testInventoryCache(t, store, revisions, "beta", nil)
			if got, err := cacheB.GetOrLoad(context.Background(), loader); err != nil || inventoryTotal(got) != 3 {
				t.Fatalf("new namespace inventory=%+v error=%v, want total 3", got, err)
			}
		})
	}
}

func TestInventoryCacheRefreshesMalformedAndSchemaMismatchedValues(t *testing.T) {
	const key = "ae:test:repos:inventory:v1:rev:11111111-1111-4111-8111-111111111111"
	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "malformed", value: `{not-json`},
		{name: "wrong schema", value: `{"schema_version":2,"inventory":[]}`},
		{name: "nil inventory", value: `{"schema_version":1,"inventory":null}`},
		{name: "negative count", value: `{"schema_version":1,"inventory":[{"provider_key":"unbound","name":"Needs platform binding","type":"unbound","total_repos":-1,"scopes":[]}]}`},
		{name: "trailing junk", value: `{"schema_version":1,"inventory":[]} trailing`},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeInventoryStore()
			store.values[key] = fakeInventoryValue{value: []byte(test.value), ttl: time.Minute}
			revisions := &fakeInventoryRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
			cache := testInventoryCache(t, store, revisions, "test", nil)
			var loads atomic.Int32
			inventory, err := cache.GetOrLoad(context.Background(), func(context.Context) ([]InventoryProviderSummary, error) {
				loads.Add(1)
				return testInventory(2), nil
			})
			if err != nil || inventoryTotal(inventory) != 2 || loads.Load() != 1 {
				t.Fatalf("inventory=%+v loads=%d error=%v, want refreshed total 2", inventory, loads.Load(), err)
			}
		})
	}
}

func TestInventoryCacheRedisCommandFailuresFallBackWithoutFailingRead(t *testing.T) {
	for _, test := range []struct {
		name      string
		configure func(*fakeInventoryStore)
	}{
		{name: "get", configure: func(store *fakeInventoryStore) { store.getErr = errors.New("redis get unavailable") }},
		{name: "lease", configure: func(store *fakeInventoryStore) { store.acquireErr = errors.New("redis lease unavailable") }},
		{name: "set", configure: func(store *fakeInventoryStore) { store.setErr = errors.New("redis set unavailable") }},
		{name: "release", configure: func(store *fakeInventoryStore) { store.releaseErr = errors.New("redis release unavailable") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeInventoryStore()
			test.configure(store)
			revisions := &fakeInventoryRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
			cache := testInventoryCache(t, store, revisions, "test", nil)
			var loads atomic.Int32
			inventory, err := cache.GetOrLoad(context.Background(), func(context.Context) ([]InventoryProviderSummary, error) {
				loads.Add(1)
				return testInventory(6), nil
			})
			if err != nil || inventoryTotal(inventory) != 6 || loads.Load() != 1 {
				t.Fatalf("inventory=%+v loads=%d error=%v, want one authoritative load", inventory, loads.Load(), err)
			}
		})
	}
}

func TestInventoryCacheLeaseExpiryAndRevisionChangeRecover(t *testing.T) {
	t.Run("lease expiry", func(t *testing.T) {
		store := newFakeInventoryStore()
		revisions := &fakeInventoryRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
		key := inventoryCacheKey("test", revisions.revision)
		store.leases[key+":lease"] = fakeInventoryLease{token: "abandoned", expiresAt: store.now.Add(20 * time.Millisecond)}
		cache := testInventoryCache(t, store, revisions, "test", func(options *InventoryCacheOptions) {
			options.LeaseTTL = 20 * time.Millisecond
			options.PollInterval = 5 * time.Millisecond
			options.Sleep = func(ctx context.Context, duration time.Duration) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				store.advance(duration)
				return nil
			}
		})
		inventory, err := cache.GetOrLoad(context.Background(), func(context.Context) ([]InventoryProviderSummary, error) {
			return testInventory(8), nil
		})
		if err != nil || inventoryTotal(inventory) != 8 {
			t.Fatalf("inventory=%+v error=%v, want recovered total 8", inventory, err)
		}
	})

	t.Run("revision changes during load", func(t *testing.T) {
		store := newFakeInventoryStore()
		revisions := &fakeInventoryRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
		cache := testInventoryCache(t, store, revisions, "test", nil)
		var loads atomic.Int32
		inventory, err := cache.GetOrLoad(context.Background(), func(context.Context) ([]InventoryProviderSummary, error) {
			call := loads.Add(1)
			if call == 1 {
				revisions.set("22222222-2222-4222-8222-222222222222")
			}
			return testInventory(int(call)), nil
		})
		if err != nil || inventoryTotal(inventory) != 2 || loads.Load() != 2 {
			t.Fatalf("inventory=%+v loads=%d error=%v, want new revision total 2", inventory, loads.Load(), err)
		}
		store.mu.Lock()
		_, oldWritten := store.values[inventoryCacheKey("test", "11111111-1111-4111-8111-111111111111")]
		_, newWritten := store.values[inventoryCacheKey("test", "22222222-2222-4222-8222-222222222222")]
		store.mu.Unlock()
		if oldWritten || !newWritten {
			t.Fatalf("cache writes old=%v new=%v, want false/true", oldWritten, newWritten)
		}
	})
}

func TestInventoryCacheRedisAdapterUsesSetNXPXAndTokenCheckedRelease(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRedisInventoryStore(client)
	ctx := context.Background()

	if _, err := store.Get(ctx, "missing"); !errors.Is(err, ErrInventoryCacheMiss) {
		t.Fatalf("Get(missing) error = %v, want ErrInventoryCacheMiss", err)
	}
	if err := store.Set(ctx, "value", []byte(`{"schema_version":1}`), 48*time.Second); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if ttl := server.TTL("value"); ttl != 48*time.Second {
		t.Fatalf("value TTL = %s, want 48s", ttl)
	}
	acquired, err := store.TryAcquireLease(ctx, "lease", "owner-a", 1250*time.Millisecond)
	if err != nil || !acquired {
		t.Fatalf("first TryAcquireLease() = %v, %v", acquired, err)
	}
	if ttl := server.TTL("lease"); ttl != 1250*time.Millisecond {
		t.Fatalf("lease TTL = %s, want 1.25s", ttl)
	}
	acquired, err = store.TryAcquireLease(ctx, "lease", "owner-b", 1250*time.Millisecond)
	if err != nil || acquired {
		t.Fatalf("second TryAcquireLease() = %v, %v, want false, nil", acquired, err)
	}
	released, err := store.ReleaseLease(ctx, "lease", "owner-b")
	if err != nil || released {
		t.Fatalf("mismatched ReleaseLease() = %v, %v", released, err)
	}
	if got, err := server.Get("lease"); err != nil || got != "owner-a" {
		t.Fatalf("lease after mismatched release = %q, %v", got, err)
	}
	released, err = store.ReleaseLease(ctx, "lease", "owner-a")
	if err != nil || !released || server.Exists("lease") {
		t.Fatalf("matching ReleaseLease() = %v, %v, exists=%v", released, err, server.Exists("lease"))
	}
	if strings.Contains(inventoryCacheKey("prod", "11111111-1111-4111-8111-111111111111"), "github") {
		t.Fatal("inventory cache key unexpectedly contains repository/provider data")
	}
}
