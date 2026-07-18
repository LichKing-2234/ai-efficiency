package repo

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recordingInventoryCacheMetrics struct {
	mu     sync.Mutex
	counts map[string]int
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
