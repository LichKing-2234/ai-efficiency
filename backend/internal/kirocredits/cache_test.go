package kirocredits

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"go.uber.org/zap"
)

type memoryStore struct {
	mu     sync.Mutex
	values map[string][]byte
}

func newMemoryStore() *memoryStore { return &memoryStore{values: map[string][]byte{}} }

func (m *memoryStore) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.values[key]
	if !ok {
		return nil, readcache.ErrMiss
	}
	return value, nil
}

func (m *memoryStore) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[key] = value
	return nil
}

func (m *memoryStore) TryAcquireLease(context.Context, string, string, time.Duration) (bool, error) {
	return true, nil
}

func (m *memoryStore) LeaseTTL(context.Context, string) (time.Duration, error) {
	return 0, readcache.ErrMiss
}

func (m *memoryStore) ReleaseLease(context.Context, string, string) (bool, error) {
	return true, nil
}

func testCache(t *testing.T) (*Cache, *memoryStore) {
	t.Helper()
	store := newMemoryStore()
	cache, err := NewCache(store, CacheOptions{Namespace: "test", FreshTTL: 60 * time.Second, StaleTTL: 5 * time.Minute})
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	return cache, store
}

func sampleValue(t *testing.T) cachedCredits {
	t.Helper()
	return cachedCredits{
		KeyFound: true,
		Credits:  12.5,
		Requests: 3,
		ByModel:  []ModelCredits{{Model: "claude", Credits: 12.5, Requests: 3}},
		ByDay:    []DayCredits{{Date: "2026-11-27", Credits: 12.5, Requests: 3}},
	}
}

func TestCacheServesFreshValueWithoutLoading(t *testing.T) {
	cache, _ := testCache(t)
	params := Params{StartDate: "2026-11-27", EndDate: "2026-11-27", Granularity: "day", Timezone: "Asia/Shanghai"}
	key := cache.creditsKey(3, 2, 42, params)
	cache.write(context.Background(), key, sampleValue(t))

	calls := 0
	value, err := cache.GetOrLoad(context.Background(), key, func(context.Context) (cachedCredits, error) {
		calls++
		return cachedCredits{}, errors.New("must not load")
	})
	if err != nil {
		t.Fatalf("GetOrLoad() error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("loader calls = %d, want 0 for a fresh value", calls)
	}
	if value.Credits != 12.5 {
		t.Fatalf("credits = %v, want 12.5", value.Credits)
	}
}

func TestCacheRefreshesStaleValue(t *testing.T) {
	cache, _ := testCache(t)
	now := time.Date(2026, 11, 27, 3, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	params := Params{StartDate: "2026-11-27", EndDate: "2026-11-27", Granularity: "day", Timezone: "Asia/Shanghai"}
	key := cache.creditsKey(3, 2, 42, params)
	cache.write(context.Background(), key, sampleValue(t))

	cache.now = func() time.Time { return now.Add(2 * time.Minute) }
	refreshed := sampleValue(t)
	refreshed.Credits = 99
	value, err := cache.GetOrLoad(context.Background(), key, func(context.Context) (cachedCredits, error) {
		return refreshed, nil
	})
	if err != nil {
		t.Fatalf("GetOrLoad() error = %v", err)
	}
	if value.Credits != 99 {
		t.Fatalf("credits = %v, want the refreshed 99", value.Credits)
	}
}

func TestCacheKeepsStaleValueWhenRefreshFails(t *testing.T) {
	cache, _ := testCache(t)
	now := time.Date(2026, 11, 27, 3, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	params := Params{StartDate: "2026-11-27", EndDate: "2026-11-27", Granularity: "day", Timezone: "Asia/Shanghai"}
	key := cache.creditsKey(3, 2, 42, params)
	cache.write(context.Background(), key, sampleValue(t))

	cache.now = func() time.Time { return now.Add(2 * time.Minute) }
	value, err := cache.GetOrLoad(context.Background(), key, func(context.Context) (cachedCredits, error) {
		return cachedCredits{}, errors.New("kiro2cc timeout")
	})
	if err != nil {
		t.Fatalf("GetOrLoad() error = %v, want stale fallback", err)
	}
	if value.Credits != 12.5 {
		t.Fatalf("credits = %v, want the stale 12.5", value.Credits)
	}
}

func TestCacheKeepsCompleteValueWhenRefreshIsTruncated(t *testing.T) {
	cache, _ := testCache(t)
	now := time.Date(2026, 11, 27, 3, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	params := Params{StartDate: "2026-11-27", EndDate: "2026-11-27", Granularity: "day", Timezone: "Asia/Shanghai"}
	key := cache.creditsKey(3, 2, 42, params)
	cache.write(context.Background(), key, sampleValue(t))

	cache.now = func() time.Time { return now.Add(2 * time.Minute) }
	truncated := sampleValue(t)
	truncated.Credits = 3
	truncated.MissingCredits = true
	value, err := cache.GetOrLoad(context.Background(), key, func(context.Context) (cachedCredits, error) {
		return truncated, nil
	})
	if err != nil {
		t.Fatalf("GetOrLoad() error = %v", err)
	}
	if value.Credits != 12.5 || value.partial() {
		t.Fatalf("credits = %v partial = %v, want the complete 12.5", value.Credits, value.partial())
	}
}

func TestCacheReturnsLoaderErrorWithoutValue(t *testing.T) {
	cache, _ := testCache(t)
	params := Params{StartDate: "2026-11-27", EndDate: "2026-11-27", Granularity: "day", Timezone: "Asia/Shanghai"}
	key := cache.creditsKey(3, 2, 42, params)
	if _, err := cache.GetOrLoad(context.Background(), key, func(context.Context) (cachedCredits, error) {
		return cachedCredits{}, errors.New("boom")
	}); err == nil {
		t.Fatal("GetOrLoad() error = nil, want the loader error")
	}
}

func TestCacheSharesKeyList(t *testing.T) {
	cache, _ := testCache(t)
	calls := 0
	loader := func(context.Context) ([]adminKey, error) {
		calls++
		return []adminKey{{ID: 1, Name: "sub2api-42-alice@example.com", Enabled: true}}, nil
	}
	for i := 0; i < 2; i++ {
		if _, err := cache.Keys(context.Background(), 3, 2, loader); err != nil {
			t.Fatalf("Keys() error = %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("loader calls = %d, want 1 shared list", calls)
	}
}

func TestRelayUserIDsFromKeys(t *testing.T) {
	keys := []adminKey{
		{ID: 1, Name: "sub2api-42-alice@example.com"},
		{ID: 2, Name: "sub2api-user-43"},
		{ID: 3, Name: "claude"},
		{ID: 4, Name: "sub2api-42-bob@example.com"},
	}
	ids := relayUserIDsFromKeys(keys)
	if len(ids) != 2 || ids[0] != 42 || ids[1] != 43 {
		t.Fatalf("ids = %v, want [42 43]", ids)
	}
}

func TestWarmPresetsCoverDashboardRanges(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, location)

	// Short pass: today + 7d only. The default 30d window costs dozens of
	// kiro2cc pages per heavy key, so it is refreshed on its own slower cadence.
	presets := warmPresets(now, location, false)
	if len(presets) != 2 {
		t.Fatalf("presets = %d, want 2", len(presets))
	}
	if presets[0].StartDate != "2026-11-27" || presets[1].StartDate != "2026-11-21" {
		t.Fatalf("presets = %+v", presets)
	}

	// Long pass adds the 30d window so the default dashboard view is served
	// from the cache instead of paying pagination on the request path.
	long := warmPresets(now, location, true)
	if len(long) != 3 {
		t.Fatalf("long presets = %d, want 3", len(long))
	}
	if long[2].StartDate != "2026-10-29" || long[2].EndDate != "2026-11-27" {
		t.Fatalf("long preset = %+v", long[2])
	}
}

func TestBudgetForGivesLongWindowsTheLongBudget(t *testing.T) {
	service := &Service{collectBudget: 6 * time.Second, longBudget: 30 * time.Second}
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)

	week, err := parseWindow(Params{StartDate: "2026-11-21", EndDate: "2026-11-27"}, now)
	if err != nil {
		t.Fatalf("parseWindow() error = %v", err)
	}
	if got := service.budgetFor(week); got != 6*time.Second {
		t.Fatalf("week budget = %v, want 6s", got)
	}

	month, err := parseWindow(Params{StartDate: "2026-10-29", EndDate: "2026-11-27"}, now)
	if err != nil {
		t.Fatalf("parseWindow() error = %v", err)
	}
	if got := service.budgetFor(month); got != 30*time.Second {
		t.Fatalf("month budget = %v, want 30s", got)
	}
}

func TestCollectAggregateUsesRangeEndpointWithoutPaging(t *testing.T) {
	records := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/usage/records") {
			records++
			_ = json.NewEncoder(w).Encode(usageRecordsPage{Page: 1, PageSize: recordPageSize, TotalPages: 1})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/usage/range") {
			if got, want := r.URL.Query().Get("start"), "2026-11-21"; got != want {
				t.Errorf("start = %q, want %q", got, want)
			}
			if got, want := r.URL.Query().Get("end"), "2026-11-27"; got != want {
				t.Errorf("end = %q, want %q", got, want)
			}
			_ = json.NewEncoder(w).Encode(usageRangeSummary{
				APIKeyID:          5,
				TotalRequests:     120,
				TotalInputTokens:  1000,
				TotalOutputTokens: 50,
				TotalCredits:      42.5,
				TotalCreditsSaved: 3.25,
				ByDay:             []usageRangeDay{{Date: "2026-11-27", Requests: 120, Credits: 42.5}},
				ByModel:           []usageRangeModel{{Model: "claude", Requests: 120, InputTokens: 1000, OutputTokens: 50, Credits: 42.5}},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	service := &Service{logger: zap.NewNop(), now: time.Now}
	client := newAdminClient(server.URL, "token", 2*time.Second, 0)
	location, err := time.LoadLocation(defaultWarmTimezone)
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	window, err := parseWindow(Params{StartDate: "2026-11-21", EndDate: "2026-11-27", Timezone: defaultWarmTimezone}, time.Now())
	if err != nil {
		t.Fatalf("parseWindow() error = %v", err)
	}

	aggregated, err := service.collectAggregate(context.Background(), client, 5, window, "day")
	if err != nil {
		t.Fatalf("collectAggregate() error = %v", err)
	}
	if records != 0 {
		t.Fatalf("record pages = %d, want 0 when the range endpoint answers", records)
	}
	if aggregated.credits != 42.5 || aggregated.requests != 120 || aggregated.missingCredits {
		t.Fatalf("aggregate = %+v", aggregated)
	}
	if len(aggregated.byDay) != 1 || aggregated.byDay[0].Date != "2026-11-27" {
		t.Fatalf("byDay = %+v", aggregated.byDay)
	}
	if len(aggregated.byModel) != 1 || aggregated.byModel[0].Model != "claude" {
		t.Fatalf("byModel = %+v", aggregated.byModel)
	}
	_ = location
}

func TestCollectAggregateFallsBackWhenRangeEndpointIsMissing(t *testing.T) {
	credits := 2.0
	records := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/usage/range") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		records++
		_ = json.NewEncoder(w).Encode(usageRecordsPage{
			Page:       1,
			PageSize:   recordPageSize,
			TotalPages: 1,
			Records: []usageRecord{{
				Model:       "claude",
				InputTokens: 10,
				CreditsUsed: &credits,
				CreatedAt:   time.Date(2026, 11, 27, 3, 0, 0, 0, time.UTC),
			}},
		})
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	service := &Service{logger: zap.NewNop(), now: time.Now}
	client := newAdminClient(server.URL, "token", 2*time.Second, 0)
	window, err := parseWindow(Params{StartDate: "2026-11-21", EndDate: "2026-11-27", Timezone: defaultWarmTimezone}, time.Now())
	if err != nil {
		t.Fatalf("parseWindow() error = %v", err)
	}

	aggregated, err := service.collectAggregate(context.Background(), client, 5, window, "day")
	if err != nil {
		t.Fatalf("collectAggregate() error = %v", err)
	}
	if records == 0 {
		t.Fatal("record pages = 0, want the pagination fallback")
	}
	if aggregated.credits != 2.0 || aggregated.requests != 1 {
		t.Fatalf("aggregate = %+v", aggregated)
	}
}

func TestCollectRangeKeepsPartialResultOnDeadline(t *testing.T) {
	credits := 1.5
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if raw := r.URL.Query().Get("page"); raw != "" {
			page, _ = strconv.Atoi(raw)
		}
		if page > 1 {
			// The second page is slow enough that the caller's budget expires.
			time.Sleep(200 * time.Millisecond)
		}
		pageData := usageRecordsPage{
			Page:       page,
			PageSize:   recordPageSize,
			TotalPages: 5,
			Records: []usageRecord{{
				Model:        "claude",
				InputTokens:  10,
				OutputTokens: 2,
				CreditsUsed:  &credits,
				CreatedAt:    time.Date(2026, 11, 27, 3, 0, 0, 0, time.UTC),
			}},
		}
		_ = json.NewEncoder(w).Encode(pageData)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	service := &Service{logger: zap.NewNop(), now: time.Now}
	client := newAdminClient(server.URL, "token", 2*time.Second, 0)
	window := window{
		start:        time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC),
		endExclusive: time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC),
		location:     time.UTC,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	aggregated, partial, err := service.collectRange(ctx, client, 1, window, "day")
	if err != nil {
		t.Fatalf("collectRange() error = %v, want a partial result", err)
	}
	if !partial {
		t.Fatal("partial = false, want true after the budget expired")
	}
	if aggregated.requests != 1 || aggregated.credits != 1.5 {
		t.Fatalf("aggregate = %+v, want the first page only", aggregated)
	}
}

func TestSnapshotFromValueMapsNoKey(t *testing.T) {
	now := time.Now()
	snapshot := snapshotFromValue(cachedCredits{KeyFound: false}, now)
	if snapshot.Status != StatusNoKey || snapshot.Available || snapshot.KeyFound {
		t.Fatalf("snapshot = %+v, want the no-key state", snapshot)
	}
}
