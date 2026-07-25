package repo

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
)

type recordingInventoryCacheMetrics struct {
	mu     sync.Mutex
	counts map[string]int
}

type inventoryTTLFailureStore struct {
	mu           sync.Mutex
	acquireCalls int
}

func (s *inventoryTTLFailureStore) Get(context.Context, string) ([]byte, error) {
	return nil, readcache.ErrMiss
}
func (s *inventoryTTLFailureStore) Set(context.Context, string, []byte, time.Duration) error {
	return nil
}
func (s *inventoryTTLFailureStore) TryAcquireLease(context.Context, string, string, time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acquireCalls++
	return s.acquireCalls > 1, nil
}
func (s *inventoryTTLFailureStore) LeaseTTL(context.Context, string) (time.Duration, error) {
	return 0, errors.New("Redis TTL unavailable")
}
func (s *inventoryTTLFailureStore) ReleaseLease(context.Context, string, string) (bool, error) {
	return true, nil
}

func TestRepositoryInventoryCacheMetricsLeaseTTLErrorFallsBackAuthoritatively(t *testing.T) {
	revisions := &fakeInventoryRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
	recorder := &recordingInventoryCacheMetrics{}
	cache := testInventoryCache(t, &inventoryTTLFailureStore{}, revisions, "test", func(options *InventoryCacheOptions) {
		options.Metrics = recorder
	})
	result, err := cache.GetOrLoad(context.Background(), func(context.Context) ([]InventoryProviderSummary, error) {
		return testInventory(1), nil
	})
	if err != nil || inventoryTotal(result) != 1 {
		t.Fatalf("fallback result = %+v, error = %v", result, err)
	}
	for outcome, want := range map[string]int{"miss": 1, "lease_wait": 1, "error": 1, "lease_failed": 1, "refresh": 1} {
		if got := recorder.count(outcome); got != want {
			t.Fatalf("outcome %s = %d, want %d", outcome, got, want)
		}
	}
	if recorder.count("lease_acquired") != 0 {
		t.Fatalf("lease_acquired = %d, want immediate fallback", recorder.count("lease_acquired"))
	}
}

func TestRepositoryInventoryCacheMetricsRecordDistributedLeaseWait(t *testing.T) {
	store := newFakeInventoryStore()
	revisions := &fakeInventoryRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
	holderMetrics := &recordingInventoryCacheMetrics{}
	waiterMetrics := &recordingInventoryCacheMetrics{}
	holder := testInventoryCache(t, store, revisions, "test", func(options *InventoryCacheOptions) { options.Metrics = holderMetrics })
	waiter := testInventoryCache(t, store, revisions, "test", func(options *InventoryCacheOptions) { options.Metrics = waiterMetrics })

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
	type result struct{ err error }
	results := make(chan result, 2)
	go func() { _, err := holder.GetOrLoad(context.Background(), loader); results <- result{err: err} }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("holder loader did not start")
	}
	go func() { _, err := waiter.GetOrLoad(context.Background(), loader); results <- result{err: err} }()
	time.Sleep(20 * time.Millisecond)
	close(release)
	for index := 0; index < 2; index++ {
		if result := <-results; result.err != nil {
			t.Fatalf("distributed read error = %v", result.err)
		}
	}
	if loads.Load() != 1 {
		t.Fatalf("loader calls = %d, want 1", loads.Load())
	}
	if holderMetrics.count("lease_acquired") != 1 {
		t.Fatalf("holder lease_acquired = %d, want 1", holderMetrics.count("lease_acquired"))
	}
	if waiterMetrics.count("lease_wait") == 0 || waiterMetrics.count("fresh") == 0 {
		t.Fatalf("waiter lease_wait/fresh = %d/%d, want both positive", waiterMetrics.count("lease_wait"), waiterMetrics.count("fresh"))
	}
}

func (r *recordingInventoryCacheMetrics) Record(outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.counts == nil {
		r.counts = make(map[string]int)
	}
	r.counts[outcome]++
}

func (r *recordingInventoryCacheMetrics) count(outcome string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[outcome]
}

func TestRepositoryInventoryCacheMetricsRecordColdAndWarmReads(t *testing.T) {
	store := newFakeInventoryStore()
	revisions := &fakeInventoryRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
	recorder := &recordingInventoryCacheMetrics{}
	cache := testInventoryCache(t, store, revisions, "test", func(options *InventoryCacheOptions) {
		options.Metrics = recorder
	})
	loader := func(context.Context) ([]InventoryProviderSummary, error) { return testInventory(1), nil }
	for index := 0; index < 2; index++ {
		if _, err := cache.GetOrLoad(context.Background(), loader); err != nil {
			t.Fatalf("read %d error = %v", index, err)
		}
	}
	for outcome, want := range map[string]int{"miss": 1, "refresh": 1, "lease_acquired": 1, "fresh": 1} {
		if got := recorder.count(outcome); got != want {
			t.Fatalf("outcome %s = %d, want %d", outcome, got, want)
		}
	}
}
