package workitems

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

type fakeRevisionReader struct {
	mu       sync.RWMutex
	revision string
	err      error
}

func (f *fakeRevisionReader) Current(context.Context) (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.revision, f.err
}

func (f *fakeRevisionReader) set(revision string) {
	f.mu.Lock()
	f.revision = revision
	f.mu.Unlock()
}

type fakeCountsStoreValue struct {
	value []byte
	ttl   time.Duration
}

type fakeCountsLease struct {
	token     string
	expiresAt time.Time
}

type fakeCountsStore struct {
	mu sync.Mutex

	now        time.Time
	values     map[string]fakeCountsStoreValue
	leases     map[string]fakeCountsLease
	getErr     error
	acquireErr error
	setErr     error
	releaseErr error

	getCalls        int
	acquireCalls    int
	contendedLeases int
	setCalls        int
	releaseCalls    int
	onAcquire       func(*fakeCountsStore, string)
}

type blockingHitStore struct {
	*fakeCountsStore
	key     string
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingHitStore) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := s.fakeCountsStore.Get(ctx, key)
	if key == s.key && err == nil {
		s.once.Do(func() {
			close(s.started)
			select {
			case <-s.release:
			case <-ctx.Done():
			}
		})
	}
	return value, err
}

type staggeredReadErrorStore struct {
	*fakeCountsStore
	mu       sync.Mutex
	gates    []chan struct{}
	getCalls int
}

func newStaggeredReadErrorStore(callers int) *staggeredReadErrorStore {
	gates := make([]chan struct{}, callers)
	for i := range gates {
		gates[i] = make(chan struct{})
	}
	return &staggeredReadErrorStore{fakeCountsStore: newFakeCountsStore(), gates: gates}
}

func (s *staggeredReadErrorStore) Get(ctx context.Context, _ string) ([]byte, error) {
	s.mu.Lock()
	index := s.getCalls
	s.getCalls++
	s.mu.Unlock()
	if index >= len(s.gates) {
		return nil, errors.New("redis read unavailable")
	}
	select {
	case <-s.gates[index]:
		return nil, errors.New("redis read unavailable")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *staggeredReadErrorStore) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getCalls
}

func newFakeCountsStore() *fakeCountsStore {
	return &fakeCountsStore{
		now:    time.Unix(1_700_000_000, 0),
		values: make(map[string]fakeCountsStoreValue),
		leases: make(map[string]fakeCountsLease),
	}
}

func (f *fakeCountsStore) Get(_ context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	value, ok := f.values[key]
	if !ok {
		return nil, ErrCountsCacheMiss
	}
	return append([]byte(nil), value.value...), nil
}

func (f *fakeCountsStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCalls++
	if f.setErr != nil {
		return f.setErr
	}
	f.values[key] = fakeCountsStoreValue{value: append([]byte(nil), value...), ttl: ttl}
	return nil
}

func (f *fakeCountsStore) TryAcquireLease(_ context.Context, key string, token string, ttl time.Duration) (bool, error) {
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
	f.leases[key] = fakeCountsLease{token: token, expiresAt: f.now.Add(ttl)}
	if f.onAcquire != nil {
		f.onAcquire(f, key)
	}
	return true, nil
}

func (f *fakeCountsStore) LeaseTTL(_ context.Context, key string) (time.Duration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	lease, ok := f.leases[key]
	if !ok {
		return 0, ErrCountsCacheMiss
	}
	ttl := lease.expiresAt.Sub(f.now)
	if ttl <= 0 {
		delete(f.leases, key)
		return 0, ErrCountsCacheMiss
	}
	return ttl, nil
}

func (f *fakeCountsStore) ReleaseLease(_ context.Context, key string, token string) (bool, error) {
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

func (f *fakeCountsStore) advance(duration time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(duration)
	f.mu.Unlock()
}

func testCountsCache(t *testing.T, store CountsStore, revisions RevisionReader, namespace string, mutate func(*CountsCacheOptions)) *CountsCache {
	t.Helper()
	opts := CountsCacheOptions{
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
		mutate(&opts)
	}
	cache, err := NewCountsCache(store, revisions, opts)
	if err != nil {
		t.Fatalf("NewCountsCache() error = %v", err)
	}
	return cache
}

func TestCountsCacheCollapsesFiftyConcurrentLoadsInOneProcess(t *testing.T) {
	store := newFakeCountsStore()
	revisions := &fakeRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
	cache := testCountsCache(t, store, revisions, "test", nil)

	var loads atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	loader := func(ctx context.Context) (CountsLoadResult, error) {
		if loads.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return CountsLoadResult{Counts: &CountsResponse{AIAccessSetupCount: 1, TotalCount: 1}, Cacheable: true}, nil
		case <-ctx.Done():
			return CountsLoadResult{}, ctx.Err()
		}
	}

	const callers = 50
	start := make(chan struct{})
	errs := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() {
			<-start
			counts, err := cache.GetOrLoad(context.Background(), 7, "user", loader)
			if err == nil && (counts == nil || counts.TotalCount != 1) {
				err = fmt.Errorf("counts = %+v, want total 1", counts)
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
	if got := loads.Load(); got != 1 {
		t.Fatalf("loader calls = %d, want 1", got)
	}
}

func TestCountsCacheCollapsesColdLoadsAcrossTwoInstances(t *testing.T) {
	store := newFakeCountsStore()
	revisions := &fakeRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
	cacheA := testCountsCache(t, store, revisions, "test", nil)
	cacheB := testCountsCache(t, store, revisions, "test", nil)

	var loads atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	loader := func(ctx context.Context) (CountsLoadResult, error) {
		if loads.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return CountsLoadResult{Counts: &CountsResponse{TotalCount: 3}, Cacheable: true}, nil
		case <-ctx.Done():
			return CountsLoadResult{}, ctx.Err()
		}
	}

	type result struct {
		counts *CountsResponse
		err    error
	}
	results := make(chan result, 2)
	go func() {
		counts, err := cacheA.GetOrLoad(context.Background(), 7, "admin", loader)
		results <- result{counts: counts, err: err}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("lease holder did not start loading")
	}
	go func() {
		counts, err := cacheB.GetOrLoad(context.Background(), 7, "admin", loader)
		results <- result{counts: counts, err: err}
	}()
	time.Sleep(10 * time.Millisecond)
	close(release)

	for i := 0; i < 2; i++ {
		result := <-results
		if result.err != nil {
			t.Fatalf("GetOrLoad() error = %v", result.err)
		}
		if result.counts == nil || result.counts.TotalCount != 3 {
			t.Fatalf("counts = %+v, want total 3", result.counts)
		}
	}
	if got := loads.Load(); got != 1 {
		t.Fatalf("loader calls = %d, want 1", got)
	}
	store.mu.Lock()
	contended := store.contendedLeases
	store.mu.Unlock()
	if contended == 0 {
		t.Fatal("second cache never observed the distributed lease")
	}
}

func TestCountsCacheReadsAgainAfterAcquiringLease(t *testing.T) {
	store := newFakeCountsStore()
	revisions := &fakeRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
	store.onAcquire = func(store *fakeCountsStore, leaseKey string) {
		valueKey := strings.TrimSuffix(leaseKey, ":lease")
		store.values[valueKey] = fakeCountsStoreValue{value: []byte(`{"schema_version":1,"counts":{"total_count":9}}`), ttl: 25 * time.Second}
	}
	cache := testCountsCache(t, store, revisions, "test", nil)

	var loads atomic.Int32
	counts, err := cache.GetOrLoad(context.Background(), 7, "user", func(context.Context) (CountsLoadResult, error) {
		loads.Add(1)
		return CountsLoadResult{Counts: &CountsResponse{TotalCount: 1}, Cacheable: true}, nil
	})
	if err != nil {
		t.Fatalf("GetOrLoad() error = %v", err)
	}
	if counts == nil || counts.TotalCount != 9 {
		t.Fatalf("counts = %+v, want value published before lease acquisition", counts)
	}
	if got := loads.Load(); got != 0 {
		t.Fatalf("loader calls = %d, want 0 after second cache read", got)
	}
}

func TestCountsCacheCancelledWaiterDoesNotCancelRemainingWaiter(t *testing.T) {
	store := newFakeCountsStore()
	revisions := &fakeRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
	cache := testCountsCache(t, store, revisions, "test", nil)

	started := make(chan struct{})
	release := make(chan struct{})
	loaderCancelled := make(chan struct{}, 1)
	loader := func(ctx context.Context) (CountsLoadResult, error) {
		close(started)
		select {
		case <-release:
			return CountsLoadResult{Counts: &CountsResponse{TotalCount: 4}, Cacheable: true}, nil
		case <-ctx.Done():
			loaderCancelled <- struct{}{}
			return CountsLoadResult{}, ctx.Err()
		}
	}

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstErr := make(chan error, 1)
	go func() {
		_, err := cache.GetOrLoad(firstCtx, 7, "user", loader)
		firstErr <- err
	}()
	<-started
	secondResult := make(chan error, 1)
	go func() {
		counts, err := cache.GetOrLoad(context.Background(), 7, "user", loader)
		if err == nil && (counts == nil || counts.TotalCount != 4) {
			err = fmt.Errorf("counts = %+v, want total 4", counts)
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
		t.Fatal("shared loader was cancelled while another waiter remained")
	default:
	}
	close(release)
	if err := <-secondResult; err != nil {
		t.Fatalf("remaining waiter error = %v", err)
	}
}

func TestCountsCacheFinalWaiterCancellationStopsSharedLoader(t *testing.T) {
	store := newFakeCountsStore()
	revisions := &fakeRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
	cache := testCountsCache(t, store, revisions, "test", nil)

	loaderStarted := make(chan struct{})
	loaderStopped := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := cache.GetOrLoad(ctx, 7, "user", func(loaderCtx context.Context) (CountsLoadResult, error) {
			close(loaderStarted)
			<-loaderCtx.Done()
			close(loaderStopped)
			return CountsLoadResult{}, loaderCtx.Err()
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
}

func TestCountsCachePreCancelledCallerDoesNotStartLoader(t *testing.T) {
	store := newFakeCountsStore()
	revisions := &fakeRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
	cache := testCountsCache(t, store, revisions, "test", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var loads atomic.Int32
	_, err := cache.GetOrLoad(ctx, 7, "user", func(context.Context) (CountsLoadResult, error) {
		loads.Add(1)
		return CountsLoadResult{Counts: &CountsResponse{TotalCount: 1}, Cacheable: true}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GetOrLoad() error = %v, want context.Canceled", err)
	}
	time.Sleep(20 * time.Millisecond)
	if got := loads.Load(); got != 0 {
		t.Fatalf("loader calls = %d, want 0 for pre-cancelled caller", got)
	}
}

func TestCountsCacheIsolatesActorRoleDeploymentAndRevision(t *testing.T) {
	store := newFakeCountsStore()
	revisions := &fakeRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
	cacheA := testCountsCache(t, store, revisions, "alpha", nil)
	cacheB := testCountsCache(t, store, revisions, "beta", nil)

	var loads atomic.Int32
	loader := func(context.Context) (CountsLoadResult, error) {
		value := int(loads.Add(1))
		return CountsLoadResult{Counts: &CountsResponse{TotalCount: value}, Cacheable: true}, nil
	}

	assertTotal := func(cache *CountsCache, actorID int, role string, want int) {
		t.Helper()
		counts, err := cache.GetOrLoad(context.Background(), actorID, role, loader)
		if err != nil {
			t.Fatalf("GetOrLoad(%d, %q) error = %v", actorID, role, err)
		}
		if counts == nil || counts.TotalCount != want {
			t.Fatalf("GetOrLoad(%d, %q) counts = %+v, want total %d", actorID, role, counts, want)
		}
	}

	assertTotal(cacheA, 7, "user", 1)
	assertTotal(cacheA, 7, "user", 1)
	assertTotal(cacheA, 8, "user", 2)
	assertTotal(cacheA, 7, "admin", 3)
	revisions.set("22222222-2222-4222-8222-222222222222")
	assertTotal(cacheA, 7, "user", 4)
	assertTotal(cacheB, 7, "user", 5)

	if got := loads.Load(); got != 5 {
		t.Fatalf("loader calls = %d, want 5 isolated loads", got)
	}
	wantKey := "ae:alpha:work-items:counts:v1:rev:22222222-2222-4222-8222-222222222222:actor:7:role:user"
	store.mu.Lock()
	_, ok := store.values[wantKey]
	store.mu.Unlock()
	if !ok {
		t.Fatalf("cache did not write exact versioned key %q", wantKey)
	}
}

func TestCountsCacheRetriesOldRevisionRedisHitUnderNewRevision(t *testing.T) {
	const (
		oldRevision = "11111111-1111-4111-8111-111111111111"
		newRevision = "22222222-2222-4222-8222-222222222222"
	)
	oldKey := countsCacheKey("test", oldRevision, 7, "user")
	newKey := countsCacheKey("test", newRevision, 7, "user")
	base := newFakeCountsStore()
	base.values[oldKey] = fakeCountsStoreValue{value: []byte(`{"schema_version":1,"counts":{"total_count":1}}`), ttl: time.Minute}
	base.values[newKey] = fakeCountsStoreValue{value: []byte(`{"schema_version":1,"counts":{"total_count":2}}`), ttl: time.Minute}
	store := &blockingHitStore{
		fakeCountsStore: base,
		key:             oldKey,
		started:         make(chan struct{}),
		release:         make(chan struct{}),
	}
	revisions := &fakeRevisionReader{revision: oldRevision}
	cache := testCountsCache(t, store, revisions, "test", nil)
	var loads atomic.Int32

	type loadResult struct {
		counts *CountsResponse
		err    error
	}
	resultCh := make(chan loadResult, 1)
	go func() {
		counts, err := cache.GetOrLoad(context.Background(), 7, "user", func(context.Context) (CountsLoadResult, error) {
			loads.Add(1)
			return CountsLoadResult{Counts: &CountsResponse{TotalCount: 99}, Cacheable: true}, nil
		})
		resultCh <- loadResult{counts: counts, err: err}
	}()
	<-store.started
	revisions.set(newRevision)
	close(store.release)

	got := <-resultCh
	if got.err != nil {
		t.Fatalf("GetOrLoad() error = %v", got.err)
	}
	if got.counts == nil || got.counts.TotalCount != 2 {
		t.Fatalf("counts = %+v, want new-revision cached total 2", got.counts)
	}
	if got := loads.Load(); got != 0 {
		t.Fatalf("loader calls = %d, want 0 with both revisions cached", got)
	}
}

func TestCountsCacheRetriesOriginResultWhenRevisionChangesDuringLoad(t *testing.T) {
	const (
		oldRevision = "11111111-1111-4111-8111-111111111111"
		newRevision = "22222222-2222-4222-8222-222222222222"
	)
	store := newFakeCountsStore()
	revisions := &fakeRevisionReader{revision: oldRevision}
	cache := testCountsCache(t, store, revisions, "test", nil)
	var loads atomic.Int32

	counts, err := cache.GetOrLoad(context.Background(), 7, "user", func(context.Context) (CountsLoadResult, error) {
		call := loads.Add(1)
		if call == 1 {
			revisions.set(newRevision)
			return CountsLoadResult{Counts: &CountsResponse{TotalCount: 1}, Cacheable: true}, nil
		}
		return CountsLoadResult{Counts: &CountsResponse{TotalCount: 2}, Cacheable: true}, nil
	})
	if err != nil {
		t.Fatalf("GetOrLoad() error = %v", err)
	}
	if counts == nil || counts.TotalCount != 2 {
		t.Fatalf("counts = %+v, want new-revision origin total 2", counts)
	}
	if got := loads.Load(); got != 2 {
		t.Fatalf("loader calls = %d, want old and new revision loads", got)
	}
	store.mu.Lock()
	_, oldWritten := store.values[countsCacheKey("test", oldRevision, 7, "user")]
	_, newWritten := store.values[countsCacheKey("test", newRevision, 7, "user")]
	store.mu.Unlock()
	if oldWritten || !newWritten {
		t.Fatalf("cache writes old=%v new=%v, want old=false new=true", oldWritten, newWritten)
	}
}

func TestCountsCacheCollapsesStaggeredRedisReadErrorsWithFastLoader(t *testing.T) {
	const callers = 12
	store := newStaggeredReadErrorStore(callers)
	revisions := &fakeRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
	cache := testCountsCache(t, store, revisions, "test", func(options *CountsCacheOptions) {
		options.CommandTimeout = 2 * time.Second
	})
	key := countsCacheKey("test", "11111111-1111-4111-8111-111111111111", 7, "user")
	var loads atomic.Int32
	start := make(chan struct{})
	results := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() {
			<-start
			counts, err := cache.GetOrLoad(context.Background(), 7, "user", func(context.Context) (CountsLoadResult, error) {
				loads.Add(1)
				return CountsLoadResult{Counts: &CountsResponse{TotalCount: 1}, Cacheable: true}, nil
			})
			if err == nil && (counts == nil || counts.TotalCount != 1) {
				err = fmt.Errorf("counts = %+v, want total 1", counts)
			}
			results <- err
		}()
	}
	close(start)

	mode := ""
	deadline := time.Now().Add(time.Second)
	for mode == "" && time.Now().Before(deadline) {
		cache.flights.mu.Lock()
		waiters := 0
		if call := cache.flights.calls[key]; call != nil {
			waiters = call.waiters
		}
		cache.flights.mu.Unlock()
		if waiters == callers {
			mode = "inside-flight"
		} else if store.calls() == callers {
			mode = "outside-flight"
		} else {
			time.Sleep(time.Millisecond)
		}
	}
	if mode == "" {
		t.Fatalf("callers did not converge inside or outside flight; Redis gets=%d", store.calls())
	}

	if mode == "inside-flight" {
		close(store.gates[0])
		for i := 0; i < callers; i++ {
			if err := <-results; err != nil {
				t.Fatalf("GetOrLoad() error = %v", err)
			}
		}
	} else {
		for i := 0; i < callers; i++ {
			close(store.gates[i])
			if err := <-results; err != nil {
				t.Fatalf("GetOrLoad() error = %v", err)
			}
		}
	}
	if got := loads.Load(); got != 1 {
		t.Fatalf("loader calls = %d, want 1 across staggered Redis read errors", got)
	}
}

func TestCountsCacheUsesDeterministicTwentyFourToTwentySevenSecondTTL(t *testing.T) {
	for _, test := range []struct {
		name   string
		random float64
		want   time.Duration
	}{
		{name: "minimum jitter", random: 0, want: 27 * time.Second},
		{name: "maximum jitter", random: 1, want: 24 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeCountsStore()
			revisions := &fakeRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
			cache := testCountsCache(t, store, revisions, "test", func(options *CountsCacheOptions) {
				options.RandFloat64 = func() float64 { return test.random }
			})
			_, err := cache.GetOrLoad(context.Background(), 7, "user", func(context.Context) (CountsLoadResult, error) {
				return CountsLoadResult{Counts: &CountsResponse{TotalCount: 1}, Cacheable: true}, nil
			})
			if err != nil {
				t.Fatalf("GetOrLoad() error = %v", err)
			}
			store.mu.Lock()
			defer store.mu.Unlock()
			if len(store.values) != 1 {
				t.Fatalf("stored values = %d, want 1", len(store.values))
			}
			for _, value := range store.values {
				if value.ttl != test.want {
					t.Fatalf("value TTL = %s, want %s", value.ttl, test.want)
				}
			}
		})
	}
}

func TestCountsCacheRefreshesMalformedAndSchemaMismatchedValues(t *testing.T) {
	const key = "ae:test:work-items:counts:v1:rev:11111111-1111-4111-8111-111111111111:actor:7:role:user"
	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "malformed", value: `{not-json`},
		{name: "wrong schema", value: `{"schema_version":2,"counts":{"total_count":8}}`},
		{name: "nil counts", value: `{"schema_version":1,"counts":null}`},
		{name: "negative count", value: `{"schema_version":1,"counts":{"total_count":-1}}`},
		{name: "trailing junk", value: `{"schema_version":1,"counts":{"total_count":8}} trailing`},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeCountsStore()
			store.values[key] = fakeCountsStoreValue{value: []byte(test.value), ttl: time.Minute}
			revisions := &fakeRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
			cache := testCountsCache(t, store, revisions, "test", nil)
			var loads atomic.Int32
			counts, err := cache.GetOrLoad(context.Background(), 7, "user", func(context.Context) (CountsLoadResult, error) {
				loads.Add(1)
				return CountsLoadResult{Counts: &CountsResponse{TotalCount: 2}, Cacheable: true}, nil
			})
			if err != nil {
				t.Fatalf("GetOrLoad() error = %v", err)
			}
			if counts == nil || counts.TotalCount != 2 || loads.Load() != 1 {
				t.Fatalf("counts = %+v, loads = %d, want refreshed total 2", counts, loads.Load())
			}
		})
	}
}

func TestCountsCacheRedisReadAndLeaseErrorsFallBackToOneLoad(t *testing.T) {
	for _, test := range []struct {
		name      string
		configure func(*fakeCountsStore)
	}{
		{name: "read error", configure: func(store *fakeCountsStore) { store.getErr = errors.New("redis read unavailable") }},
		{name: "lease error", configure: func(store *fakeCountsStore) { store.acquireErr = errors.New("redis lease unavailable") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeCountsStore()
			test.configure(store)
			revisions := &fakeRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
			cache := testCountsCache(t, store, revisions, "test", nil)
			var loads atomic.Int32
			started := make(chan struct{})
			release := make(chan struct{})
			loader := func(ctx context.Context) (CountsLoadResult, error) {
				if loads.Add(1) == 1 {
					close(started)
				}
				select {
				case <-release:
					return CountsLoadResult{Counts: &CountsResponse{TotalCount: 6}, Cacheable: true}, nil
				case <-ctx.Done():
					return CountsLoadResult{}, ctx.Err()
				}
			}

			const callers = 25
			start := make(chan struct{})
			errs := make(chan error, callers)
			for i := 0; i < callers; i++ {
				go func() {
					<-start
					counts, err := cache.GetOrLoad(context.Background(), 7, "user", loader)
					if err == nil && (counts == nil || counts.TotalCount != 6) {
						err = fmt.Errorf("counts = %+v, want total 6", counts)
					}
					errs <- err
				}()
			}
			close(start)
			<-started
			time.Sleep(10 * time.Millisecond)
			close(release)
			for i := 0; i < callers; i++ {
				if err := <-errs; err != nil {
					t.Fatalf("GetOrLoad() error = %v", err)
				}
			}
			if got := loads.Load(); got != 1 {
				t.Fatalf("loader calls = %d, want 1 during Redis outage", got)
			}
		})
	}
}

func TestCountsCacheWriteAndReleaseErrorsDoNotReload(t *testing.T) {
	for _, test := range []struct {
		name      string
		configure func(*fakeCountsStore)
	}{
		{name: "write error", configure: func(store *fakeCountsStore) { store.setErr = errors.New("redis write unavailable") }},
		{name: "release error", configure: func(store *fakeCountsStore) { store.releaseErr = errors.New("redis release unavailable") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeCountsStore()
			test.configure(store)
			revisions := &fakeRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
			cache := testCountsCache(t, store, revisions, "test", nil)
			var loads atomic.Int32
			counts, err := cache.GetOrLoad(context.Background(), 7, "user", func(context.Context) (CountsLoadResult, error) {
				loads.Add(1)
				return CountsLoadResult{Counts: &CountsResponse{TotalCount: 7}, Cacheable: true}, nil
			})
			if err != nil {
				t.Fatalf("GetOrLoad() error = %v", err)
			}
			if counts == nil || counts.TotalCount != 7 || loads.Load() != 1 {
				t.Fatalf("counts = %+v, loads = %d, want one successful authoritative load", counts, loads.Load())
			}
		})
	}
}

func TestCountsCacheLeaseHolderTimeoutRecoversByRecompeting(t *testing.T) {
	store := newFakeCountsStore()
	revisions := &fakeRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
	const key = "ae:test:work-items:counts:v1:rev:11111111-1111-4111-8111-111111111111:actor:7:role:user"
	store.leases[key+":lease"] = fakeCountsLease{token: "abandoned", expiresAt: store.now.Add(20 * time.Millisecond)}
	cache := testCountsCache(t, store, revisions, "test", func(options *CountsCacheOptions) {
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

	var loads atomic.Int32
	counts, err := cache.GetOrLoad(context.Background(), 7, "user", func(context.Context) (CountsLoadResult, error) {
		loads.Add(1)
		return CountsLoadResult{Counts: &CountsResponse{TotalCount: 8}, Cacheable: true}, nil
	})
	if err != nil {
		t.Fatalf("GetOrLoad() error = %v", err)
	}
	if counts == nil || counts.TotalCount != 8 || loads.Load() != 1 {
		t.Fatalf("counts = %+v, loads = %d, want recovered load", counts, loads.Load())
	}
}

func TestCountsCacheDegradedResultIsNotCached(t *testing.T) {
	store := newFakeCountsStore()
	revisions := &fakeRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
	cache := testCountsCache(t, store, revisions, "test", nil)
	counts, err := cache.GetOrLoad(context.Background(), 7, "user", func(context.Context) (CountsLoadResult, error) {
		return CountsLoadResult{Counts: &CountsResponse{TotalCount: 1}, Cacheable: false}, nil
	})
	if err != nil || counts == nil || counts.TotalCount != 1 {
		t.Fatalf("GetOrLoad() counts = %+v, error = %v", counts, err)
	}
	store.mu.Lock()
	setCalls := store.setCalls
	store.mu.Unlock()
	if setCalls != 0 {
		t.Fatalf("cache writes = %d, want 0", setCalls)
	}
}

func TestCountsCacheRedisAdapterUsesSetNXPXTTLAndTokenCheckedLuaRelease(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRedisCountsStore(client)
	ctx := context.Background()

	if _, err := store.Get(ctx, "missing"); !errors.Is(err, ErrCountsCacheMiss) {
		t.Fatalf("Get(missing) error = %v, want ErrCountsCacheMiss", err)
	}
	if err := store.Set(ctx, "value", []byte(`{"schema_version":1}`), 24*time.Second); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if ttl := server.TTL("value"); ttl != 24*time.Second {
		t.Fatalf("value TTL = %s, want 24s", ttl)
	}

	acquired, err := store.TryAcquireLease(ctx, "lease", "owner-a", 1250*time.Millisecond)
	if err != nil || !acquired {
		t.Fatalf("first TryAcquireLease() = %v, %v, want true, nil", acquired, err)
	}
	if ttl := server.TTL("lease"); ttl != 1250*time.Millisecond {
		t.Fatalf("lease TTL = %s, want 1.25s SET NX PX expiry", ttl)
	}
	acquired, err = store.TryAcquireLease(ctx, "lease", "owner-b", 1250*time.Millisecond)
	if err != nil || acquired {
		t.Fatalf("second TryAcquireLease() = %v, %v, want false, nil", acquired, err)
	}

	released, err := store.ReleaseLease(ctx, "lease", "owner-b")
	if err != nil || released {
		t.Fatalf("mismatched ReleaseLease() = %v, %v, want false, nil", released, err)
	}
	if got, err := server.Get("lease"); err != nil || got != "owner-a" {
		t.Fatalf("lease after mismatched release = %q, %v, want owner-a", got, err)
	}
	released, err = store.ReleaseLease(ctx, "lease", "owner-a")
	if err != nil || !released {
		t.Fatalf("matching ReleaseLease() = %v, %v, want true, nil", released, err)
	}
	if server.Exists("lease") {
		t.Fatal("matching Lua compare-delete left lease key behind")
	}
}
