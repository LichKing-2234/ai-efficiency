package workitems

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCountsCacheMetricsRecordColdAndWarmOutcomes(t *testing.T) {
	store := newFakeCountsStore()
	revisions := &fakeRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
	recorder := newRecordingCountsCacheMetrics()
	cache := testCountsCache(t, store, revisions, "test", func(options *CountsCacheOptions) {
		options.Metrics = recorder
	})
	loader := func(context.Context) (CountsLoadResult, error) {
		return CountsLoadResult{Counts: &CountsResponse{TotalCount: 1}, Cacheable: true}, nil
	}

	if _, err := cache.GetOrLoad(context.Background(), 7, "user", loader); err != nil {
		t.Fatalf("cold GetOrLoad() error = %v", err)
	}
	if _, err := cache.GetOrLoad(context.Background(), 7, "user", loader); err != nil {
		t.Fatalf("warm GetOrLoad() error = %v", err)
	}

	recorder.require(t, map[string]int{
		"miss":           1,
		"lease_acquired": 1,
		"refresh":        1,
		"fresh":          1,
	})
}

func TestCountsCacheMetricsShowOneRefreshUnderLocalCollapse(t *testing.T) {
	store := newFakeCountsStore()
	revisions := &fakeRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
	recorder := newRecordingCountsCacheMetrics()
	cache := testCountsCache(t, store, revisions, "test", func(options *CountsCacheOptions) {
		options.Metrics = recorder
	})
	started := make(chan struct{})
	release := make(chan struct{})
	loader := func(context.Context) (CountsLoadResult, error) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return CountsLoadResult{Counts: &CountsResponse{TotalCount: 2}, Cacheable: true}, nil
	}

	const callers = 20
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := cache.GetOrLoad(context.Background(), 7, "user", loader)
			errs <- err
		}()
	}
	<-started
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("GetOrLoad() error = %v", err)
		}
	}

	recorder.requireSelected(t, map[string]int{
		"miss":           1,
		"lease_acquired": 1,
		"refresh":        1,
	})
}

func TestCountsCacheMetricsShowRedisFallbackAndLeaseFailure(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*fakeCountsStore)
		want      map[string]int
	}{
		{
			name: "read error",
			configure: func(store *fakeCountsStore) {
				store.getErr = errors.New("Redis read unavailable")
			},
			want: map[string]int{"error": 1, "refresh": 1},
		},
		{
			name: "lease error",
			configure: func(store *fakeCountsStore) {
				store.acquireErr = errors.New("Redis lease unavailable")
			},
			want: map[string]int{"miss": 1, "error": 1, "lease_failed": 1, "refresh": 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeCountsStore()
			tt.configure(store)
			revisions := &fakeRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
			recorder := newRecordingCountsCacheMetrics()
			cache := testCountsCache(t, store, revisions, "test", func(options *CountsCacheOptions) {
				options.Metrics = recorder
			})

			counts, err := cache.GetOrLoad(context.Background(), 7, "user", func(context.Context) (CountsLoadResult, error) {
				return CountsLoadResult{Counts: &CountsResponse{TotalCount: 3}, Cacheable: true}, nil
			})
			if err != nil || counts.TotalCount != 3 {
				t.Fatalf("GetOrLoad() = %+v, %v, want authoritative total 3", counts, err)
			}
			recorder.require(t, tt.want)
		})
	}
}

func TestCountsCacheMetricsShowDistributedLeaseWait(t *testing.T) {
	store := newFakeCountsStore()
	revisions := &fakeRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
	holderMetrics := newRecordingCountsCacheMetrics()
	waiterMetrics := newRecordingCountsCacheMetrics()
	holder := testCountsCache(t, store, revisions, "test", func(options *CountsCacheOptions) {
		options.Metrics = holderMetrics
	})
	waiter := testCountsCache(t, store, revisions, "test", func(options *CountsCacheOptions) {
		options.Metrics = waiterMetrics
	})
	started := make(chan struct{})
	release := make(chan struct{})
	loader := func(context.Context) (CountsLoadResult, error) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return CountsLoadResult{Counts: &CountsResponse{TotalCount: 4}, Cacheable: true}, nil
	}

	errs := make(chan error, 2)
	go func() {
		_, err := holder.GetOrLoad(context.Background(), 7, "admin", loader)
		errs <- err
	}()
	<-started
	go func() {
		_, err := waiter.GetOrLoad(context.Background(), 7, "admin", loader)
		errs <- err
	}()
	deadline := time.Now().Add(time.Second)
	for waiterMetrics.count("lease_wait") == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if waiterMetrics.count("lease_wait") == 0 {
		t.Fatal("waiter did not report distributed lease contention")
	}
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("GetOrLoad() error = %v", err)
		}
	}

	holderMetrics.require(t, map[string]int{"miss": 1, "lease_acquired": 1, "refresh": 1})
	waiterMetrics.require(t, map[string]int{"miss": 1, "lease_wait": 1, "fresh": 1})
}

func TestCountsCacheMetricsRecordLeaseHolderDoubleCheckHitAsFresh(t *testing.T) {
	store := newFakeCountsStore()
	revisions := &fakeRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
	value, err := json.Marshal(countsValueEnvelope{SchemaVersion: countsCacheSchemaVersion, Counts: &CountsResponse{TotalCount: 7}})
	if err != nil {
		t.Fatalf("marshal cached counts: %v", err)
	}
	store.onAcquire = func(store *fakeCountsStore, leaseKey string) {
		store.values[leaseKey[:len(leaseKey)-len(":lease")]] = fakeCountsStoreValue{value: value, ttl: time.Minute}
	}
	recorder := newRecordingCountsCacheMetrics()
	cache := testCountsCache(t, store, revisions, "test", func(options *CountsCacheOptions) { options.Metrics = recorder })

	counts, err := cache.GetOrLoad(context.Background(), 7, "user", func(context.Context) (CountsLoadResult, error) {
		return CountsLoadResult{}, errors.New("loader must not run after double-check hit")
	})
	if err != nil || counts.TotalCount != 7 {
		t.Fatalf("GetOrLoad() = %+v, %v, want cached total 7", counts, err)
	}
	recorder.require(t, map[string]int{"miss": 1, "lease_acquired": 1, "fresh": 1})
}

func TestCountsCacheMetricsShowMalformedWriteReleaseAndLoaderErrors(t *testing.T) {
	tests := []struct {
		name       string
		configure  func(*fakeCountsStore, string)
		loader     func(context.Context) (CountsLoadResult, error)
		wantError  bool
		wantEvents map[string]int
	}{
		{
			name: "malformed value",
			configure: func(store *fakeCountsStore, key string) {
				store.values[key] = fakeCountsStoreValue{value: []byte(`{not-json`), ttl: time.Minute}
			},
			wantEvents: map[string]int{"miss": 1, "lease_acquired": 1, "refresh": 1},
		},
		{
			name: "write error",
			configure: func(store *fakeCountsStore, _ string) {
				store.setErr = errors.New("Redis write unavailable")
			},
			wantEvents: map[string]int{"miss": 1, "lease_acquired": 1, "refresh": 1, "error": 1},
		},
		{
			name: "release error",
			configure: func(store *fakeCountsStore, _ string) {
				store.releaseErr = errors.New("Redis release unavailable")
			},
			wantEvents: map[string]int{"miss": 1, "lease_acquired": 1, "refresh": 1, "error": 1, "lease_failed": 1},
		},
		{
			name: "loader error",
			loader: func(context.Context) (CountsLoadResult, error) {
				return CountsLoadResult{}, errors.New("authoritative load failed")
			},
			wantError:  true,
			wantEvents: map[string]int{"miss": 1, "lease_acquired": 1, "refresh": 1, "error": 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeCountsStore()
			key := countsCacheKey("test", "11111111-1111-4111-8111-111111111111", 7, "user")
			if tt.configure != nil {
				tt.configure(store, key)
			}
			revisions := &fakeRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
			recorder := newRecordingCountsCacheMetrics()
			cache := testCountsCache(t, store, revisions, "test", func(options *CountsCacheOptions) {
				options.Metrics = recorder
			})
			loader := tt.loader
			if loader == nil {
				loader = func(context.Context) (CountsLoadResult, error) {
					return CountsLoadResult{Counts: &CountsResponse{TotalCount: 5}, Cacheable: true}, nil
				}
			}

			_, err := cache.GetOrLoad(context.Background(), 7, "user", loader)
			if tt.wantError && err == nil {
				t.Fatal("GetOrLoad() error = nil, want loader error")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("GetOrLoad() error = %v", err)
			}
			recorder.require(t, tt.wantEvents)
		})
	}
}

func TestCountsCacheMetricsLeaseTTLErrorFallsBackAuthoritatively(t *testing.T) {
	store := newFakeCountsStore()
	key := countsCacheKey("test", "11111111-1111-4111-8111-111111111111", 7, "user")
	store.leases[key+":lease"] = fakeCountsLease{token: "other", expiresAt: store.now.Add(time.Second)}
	store.leaseTTLErr = errors.New("Redis TTL unavailable")
	revisions := &fakeRevisionReader{revision: "11111111-1111-4111-8111-111111111111"}
	recorder := newRecordingCountsCacheMetrics()
	cache := testCountsCache(t, store, revisions, "test", func(options *CountsCacheOptions) {
		options.Metrics = recorder
		options.RefreshTimeout = 100 * time.Millisecond
	})

	counts, err := cache.GetOrLoad(context.Background(), 7, "user", func(context.Context) (CountsLoadResult, error) {
		return CountsLoadResult{Counts: &CountsResponse{TotalCount: 6}, Cacheable: true}, nil
	})
	if err != nil || counts.TotalCount != 6 {
		t.Fatalf("GetOrLoad() = %+v, %v, want authoritative total 6", counts, err)
	}
	recorder.require(t, map[string]int{"miss": 1, "lease_wait": 1, "error": 1, "lease_failed": 1, "refresh": 1})
}

type recordingCountsCacheMetrics struct {
	mu       sync.Mutex
	outcomes map[string]int
}

func newRecordingCountsCacheMetrics() *recordingCountsCacheMetrics {
	return &recordingCountsCacheMetrics{outcomes: make(map[string]int)}
}

func (r *recordingCountsCacheMetrics) Record(outcome string) {
	r.mu.Lock()
	r.outcomes[outcome]++
	r.mu.Unlock()
}

func (r *recordingCountsCacheMetrics) require(t *testing.T, want map[string]int) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.outcomes) != len(want) {
		t.Fatalf("cache outcomes = %#v, want %#v", r.outcomes, want)
	}
	for outcome, count := range want {
		if r.outcomes[outcome] != count {
			t.Fatalf("cache outcome %s = %d, want %d (all %#v)", outcome, r.outcomes[outcome], count, r.outcomes)
		}
	}
}

func (r *recordingCountsCacheMetrics) requireSelected(t *testing.T, want map[string]int) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for outcome, count := range want {
		if r.outcomes[outcome] != count {
			t.Fatalf("cache outcome %s = %d, want %d (all %#v)", outcome, r.outcomes[outcome], count, r.outcomes)
		}
	}
	for outcome := range r.outcomes {
		if outcome != "fresh" {
			if _, ok := want[outcome]; !ok {
				t.Fatalf("unexpected cache outcome %s in %#v", outcome, r.outcomes)
			}
		}
	}
}

func (r *recordingCountsCacheMetrics) count(outcome string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.outcomes[outcome]
}
