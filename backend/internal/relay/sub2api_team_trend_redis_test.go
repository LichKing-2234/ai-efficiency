package relay

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

func TestTeamTrendRedisCacheSharesNormalizedValuesAndClonesPoints(t *testing.T) {
	server := miniredis.RunT(t)
	store := newTeamTrendRedisTestStore(t, server)
	now := time.Date(2026, 7, 20, 9, 30, 0, 0, time.UTC)
	writerMetrics := &teamTrendRedisTestMetrics{}
	readerMetrics := &teamTrendRedisTestMetrics{}
	writer := newTeamTrendRedisTestCache(t, store, teamTrendRedisCacheOptions{
		Namespace: "test", ProviderID: 7, ProviderVersion: 3, Metrics: writerMetrics, Now: func() time.Time { return now },
	})
	reader := newTeamTrendRedisTestCache(t, store, teamTrendRedisCacheOptions{
		Namespace: "test", ProviderID: 7, ProviderVersion: 3, Metrics: readerMetrics, Now: func() time.Time { return now },
	})
	tokens := int64(123)
	writeParams := TeamMemberTrendParams{
		StartDate: " 2026-07-01 ", EndDate: " 2026-07-20 ", Granularity: " DAILY ", Timezone: " Asia/Shanghai ",
	}
	if err := writer.Write(context.Background(), map[int64][]UsageTrendPoint{
		101: {{Date: "2026-07-20", ActualCost: 1.25, TotalTokens: &tokens}},
	}, writeParams, "batch_origin"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	values, misses, err := reader.Read(context.Background(), []int64{102, 101, 101, 0, -1}, TeamMemberTrendParams{
		StartDate: "2026-07-01", EndDate: "2026-07-20", Granularity: "daily", Timezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !reflect.DeepEqual(misses, []int64{102}) {
		t.Fatalf("Read() misses = %v, want [102]", misses)
	}
	if got := values[101]; len(got) != 1 || got[0].ActualCost != 1.25 || got[0].TotalTokens == nil || *got[0].TotalTokens != 123 {
		t.Fatalf("Read() value = %#v", got)
	}
	values[101][0].ActualCost = 99
	*values[101][0].TotalTokens = 999

	again, againMisses, err := reader.Read(context.Background(), []int64{101}, writeParams)
	if err != nil {
		t.Fatalf("second Read() error = %v", err)
	}
	if len(againMisses) != 0 {
		t.Fatalf("second Read() misses = %v", againMisses)
	}
	if got := again[101][0]; got.ActualCost != 1.25 || got.TotalTokens == nil || *got.TotalTokens != 123 {
		t.Fatalf("second Read() value = %#v, want unchanged clone", got)
	}
	if got := writerMetrics.outcomesSnapshot(); !reflect.DeepEqual(got, []string{"write", "batch_origin"}) {
		t.Fatalf("write metrics = %v", got)
	}
	if readerMetrics.count("fresh") != 2 || readerMetrics.count("miss") != 1 {
		t.Fatalf("read metrics = %v", readerMetrics.outcomesSnapshot())
	}
	keys := server.Keys()
	if len(keys) != 1 || len(keys[0]) != len("test:relay-user-trend:v1:")+64 || keys[0][:len("test:relay-user-trend:v1:")] != "test:relay-user-trend:v1:" {
		t.Fatalf("Redis keys = %v, want one hashed relay-user-trend key", keys)
	}
	if ttl := server.TTL(keys[0]); ttl != teamTrendRedisTTL {
		t.Fatalf("Redis TTL = %s, want %s", ttl, teamTrendRedisTTL)
	}
}

func TestTeamTrendRedisCacheRoundTripsProvenEmptyTrend(t *testing.T) {
	server := miniredis.RunT(t)
	store := newTeamTrendRedisTestStore(t, server)
	metrics := &teamTrendRedisTestMetrics{}
	cache := newTeamTrendRedisTestCache(t, store, teamTrendRedisCacheOptions{
		Namespace: "test", ProviderID: 7, ProviderVersion: 3, Metrics: metrics,
	})
	if err := cache.Write(context.Background(), map[int64][]UsageTrendPoint{101: nil}, testTeamTrendRedisParams(), "batch_origin"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	keys := server.Keys()
	if len(keys) != 1 {
		t.Fatalf("Redis keys = %v", keys)
	}
	raw, err := server.Get(keys[0])
	if err != nil {
		t.Fatalf("Get(%q) error = %v", keys[0], err)
	}
	var encoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &encoded); err != nil {
		t.Fatalf("decode Redis envelope: %v", err)
	}
	if string(encoded["points"]) != "[]" {
		t.Fatalf("encoded points = %s, want []", encoded["points"])
	}

	values, misses, err := cache.Read(context.Background(), []int64{101}, testTeamTrendRedisParams())
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(misses) != 0 {
		t.Fatalf("Read() misses = %v", misses)
	}
	points, ok := values[101]
	if !ok || points == nil || len(points) != 0 {
		t.Fatalf("Read() points/present = %#v/%v, want non-nil empty hit", points, ok)
	}
	if metrics.count("fresh") != 1 || metrics.count("malformed") != 0 {
		t.Fatalf("metrics = %v", metrics.outcomesSnapshot())
	}
}

func TestTeamTrendRedisCacheIdentityDimensionsDoNotCollide(t *testing.T) {
	server := miniredis.RunT(t)
	store := newTeamTrendRedisTestStore(t, server)
	now := time.Date(2026, 7, 20, 9, 30, 0, 0, time.UTC)
	params := testTeamTrendRedisParams()
	baseline := newTeamTrendRedisTestCache(t, store, teamTrendRedisCacheOptions{
		Namespace: "test", ProviderID: 7, ProviderVersion: 3, Now: func() time.Time { return now },
	})
	if err := baseline.Write(context.Background(), map[int64][]UsageTrendPoint{
		101: {{Date: "2026-07-20", ActualCost: 1}},
	}, params, "batch_origin"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	tests := []struct {
		name            string
		providerID      int
		providerVersion int64
		userID          int64
		params          TeamMemberTrendParams
	}{
		{name: "provider ID", providerID: 8, providerVersion: 3, userID: 101, params: params},
		{name: "provider version", providerID: 7, providerVersion: 4, userID: 101, params: params},
		{name: "Relay user ID", providerID: 7, providerVersion: 3, userID: 102, params: params},
		{name: "start", providerID: 7, providerVersion: 3, userID: 101, params: withTeamTrendRedisStart(params, "2026-07-02")},
		{name: "end", providerID: 7, providerVersion: 3, userID: 101, params: withTeamTrendRedisEnd(params, "2026-07-19")},
		{name: "granularity", providerID: 7, providerVersion: 3, userID: 101, params: withTeamTrendRedisGranularity(params, "weekly")},
		{name: "timezone", providerID: 7, providerVersion: 3, userID: 101, params: withTeamTrendRedisTimezone(params, "UTC")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cache := newTeamTrendRedisTestCache(t, store, teamTrendRedisCacheOptions{
				Namespace: "test", ProviderID: test.providerID, ProviderVersion: test.providerVersion, Now: func() time.Time { return now },
			})
			values, misses, err := cache.Read(context.Background(), []int64{test.userID}, test.params)
			if err != nil {
				t.Fatalf("Read() error = %v", err)
			}
			if len(values) != 0 || !reflect.DeepEqual(misses, []int64{test.userID}) {
				t.Fatalf("Read() values/misses = %v/%v, want isolated miss", values, misses)
			}
		})
	}
}

func TestTeamTrendRedisCacheRejectsExpiredMalformedAndFutureEnvelopes(t *testing.T) {
	server := miniredis.RunT(t)
	store := newTeamTrendRedisTestStore(t, server)
	now := time.Date(2026, 7, 20, 9, 30, 0, 0, time.UTC)
	metrics := &teamTrendRedisTestMetrics{}
	cache := newTeamTrendRedisTestCache(t, store, teamTrendRedisCacheOptions{
		Namespace: "test", ProviderID: 7, ProviderVersion: 3, Metrics: metrics, Now: func() time.Time { return now },
	})
	params := testTeamTrendRedisParams()
	if err := cache.Write(context.Background(), map[int64][]UsageTrendPoint{
		101: {{Date: "2026-07-20", ActualCost: 1}},
	}, params, "batch_origin"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	keys := server.Keys()
	if len(keys) != 1 {
		t.Fatalf("Redis keys = %v", keys)
	}
	key := keys[0]
	original, err := server.Get(key)
	if err != nil {
		t.Fatalf("Get(%q) error = %v", key, err)
	}

	now = now.Add(teamTrendRedisTTL)
	values, misses, err := cache.Read(context.Background(), []int64{101}, params)
	if err != nil {
		t.Fatalf("expired Read() error = %v", err)
	}
	if len(values) != 0 || !reflect.DeepEqual(misses, []int64{101}) || !server.Exists(key) {
		t.Fatalf("expired Read() values/misses/key = %v/%v/%v", values, misses, server.Exists(key))
	}

	now = now.Add(-teamTrendRedisTTL)
	future := decodeTeamTrendRedisJSON(t, original)
	future["generated_at"] = now.Add(time.Second).Format(time.RFC3339Nano)
	unknown := decodeTeamTrendRedisJSON(t, original)
	unknown["actor_id"] = 44
	tests := []struct {
		name string
		raw  string
	}{
		{name: "malformed JSON", raw: "{"},
		{name: "future generated_at", raw: encodeTeamTrendRedisJSON(t, future)},
		{name: "unknown field", raw: encodeTeamTrendRedisJSON(t, unknown)},
		{name: "trailing content", raw: original + " {}"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := metrics.count("malformed")
			server.Set(key, test.raw)
			values, misses, err := cache.Read(context.Background(), []int64{101}, params)
			if err != nil {
				t.Fatalf("Read() error = %v", err)
			}
			if len(values) != 0 || !reflect.DeepEqual(misses, []int64{101}) {
				t.Fatalf("Read() values/misses = %v/%v, want malformed miss", values, misses)
			}
			if got := metrics.count("malformed"); got != before+1 {
				t.Fatalf("malformed metrics count = %d, want %d; outcomes %v", got, before+1, metrics.outcomesSnapshot())
			}
		})
	}
}

func TestTeamTrendRedisCacheUsesOneOrderedBulkReadAndWrite(t *testing.T) {
	server := miniredis.RunT(t)
	baseStore := newTeamTrendRedisTestStore(t, server)
	store := &teamTrendRedisSpyStore{MultiStore: baseStore}
	now := time.Date(2026, 7, 20, 9, 30, 0, 0, time.UTC)
	cache := newTeamTrendRedisTestCache(t, store, teamTrendRedisCacheOptions{
		Namespace: "test", ProviderID: 7, ProviderVersion: 3, Now: func() time.Time { return now },
	})
	tokens := int64(10)
	if err := cache.Write(context.Background(), map[int64][]UsageTrendPoint{
		102: {{Date: "2026-07-20", ActualCost: 2}},
		101: {{Date: "2026-07-20", ActualCost: 1, TotalTokens: &tokens}},
	}, testTeamTrendRedisParams(), "batch_origin"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if store.setManyCalls != 1 || len(store.setItems) != 2 {
		t.Fatalf("SetMany calls/items = %d/%d, want 1/2", store.setManyCalls, len(store.setItems))
	}
	for index, item := range store.setItems {
		if item.TTL != teamTrendRedisTTL {
			t.Fatalf("SetMany item %d TTL = %s", index, item.TTL)
		}
		var envelope teamTrendRedisEnvelope
		if err := json.Unmarshal(item.Value, &envelope); err != nil {
			t.Fatalf("decode SetMany item %d: %v", index, err)
		}
		if envelope.RelayUserID != int64(101+index) {
			t.Fatalf("SetMany item %d user = %d, want %d", index, envelope.RelayUserID, 101+index)
		}
	}

	values, misses, err := cache.Read(context.Background(), []int64{102, 101, 102, -1, 0}, testTeamTrendRedisParams())
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if store.mgetCalls != 1 || len(values) != 2 || len(misses) != 0 {
		t.Fatalf("MGet calls/values/misses = %d/%v/%v", store.mgetCalls, values, misses)
	}
}

func TestTeamTrendRedisCacheReturnsAllPositiveIDsOnReadError(t *testing.T) {
	server := miniredis.RunT(t)
	metrics := &teamTrendRedisTestMetrics{}
	wantErr := errors.New("MGET unavailable")
	store := &teamTrendRedisSpyStore{MultiStore: newTeamTrendRedisTestStore(t, server), mgetErr: wantErr}
	cache := newTeamTrendRedisTestCache(t, store, teamTrendRedisCacheOptions{
		Namespace: "test", ProviderID: 7, ProviderVersion: 3, Metrics: metrics,
	})
	values, misses, err := cache.Read(context.Background(), []int64{102, 101, 102, -1, 0}, testTeamTrendRedisParams())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Read() error = %v, want %v", err, wantErr)
	}
	if len(values) != 0 || !reflect.DeepEqual(misses, []int64{101, 102}) {
		t.Fatalf("Read() values/misses = %v/%v", values, misses)
	}
	if !reflect.DeepEqual(metrics.outcomesSnapshot(), []string{"error"}) {
		t.Fatalf("metrics = %v", metrics.outcomesSnapshot())
	}
}

func TestTeamTrendRedisCacheRecordsOnlyErrorWhenWriteFails(t *testing.T) {
	t.Run("Redis failure", func(t *testing.T) {
		server := miniredis.RunT(t)
		metrics := &teamTrendRedisTestMetrics{}
		wantErr := errors.New("pipeline unavailable")
		store := &teamTrendRedisSpyStore{MultiStore: newTeamTrendRedisTestStore(t, server), setManyErr: wantErr}
		cache := newTeamTrendRedisTestCache(t, store, teamTrendRedisCacheOptions{
			Namespace: "test", ProviderID: 7, ProviderVersion: 3, Metrics: metrics,
		})
		err := cache.Write(context.Background(), map[int64][]UsageTrendPoint{
			101: {{Date: "2026-07-20", ActualCost: 1}},
		}, testTeamTrendRedisParams(), "batch_origin")
		if !errors.Is(err, wantErr) {
			t.Fatalf("Write() error = %v, want %v", err, wantErr)
		}
		if !reflect.DeepEqual(metrics.outcomesSnapshot(), []string{"error"}) {
			t.Fatalf("metrics = %v", metrics.outcomesSnapshot())
		}
	})

	t.Run("encoding failure", func(t *testing.T) {
		server := miniredis.RunT(t)
		metrics := &teamTrendRedisTestMetrics{}
		store := &teamTrendRedisSpyStore{MultiStore: newTeamTrendRedisTestStore(t, server)}
		cache := newTeamTrendRedisTestCache(t, store, teamTrendRedisCacheOptions{
			Namespace: "test", ProviderID: 7, ProviderVersion: 3, Metrics: metrics,
		})
		err := cache.Write(context.Background(), map[int64][]UsageTrendPoint{
			101: {{Date: "2026-07-20", ActualCost: math.NaN()}},
		}, testTeamTrendRedisParams(), "batch_origin")
		if err == nil {
			t.Fatal("Write() error = nil, want JSON encoding failure")
		}
		if store.setManyCalls != 0 {
			t.Fatalf("SetMany calls = %d, want 0", store.setManyCalls)
		}
		if !reflect.DeepEqual(metrics.outcomesSnapshot(), []string{"error"}) {
			t.Fatalf("metrics = %v", metrics.outcomesSnapshot())
		}
	})
}

func TestTeamTrendRedisCacheBatchLeaseIsTokenProtectedAndSetSpecific(t *testing.T) {
	server := miniredis.RunT(t)
	store := newTeamTrendRedisTestStore(t, server)
	metricsA := &teamTrendRedisTestMetrics{}
	metricsB := &teamTrendRedisTestMetrics{}
	cacheA := newTeamTrendRedisTestCache(t, store, teamTrendRedisCacheOptions{
		Namespace: "test", ProviderID: 7, ProviderVersion: 3, Metrics: metricsA, NewToken: func() string { return "owner-a" },
	})
	cacheB := newTeamTrendRedisTestCache(t, store, teamTrendRedisCacheOptions{
		Namespace: "test", ProviderID: 7, ProviderVersion: 3, Metrics: metricsB, NewToken: func() string { return "owner-b" },
	})

	leaseKeyA, tokenA, acquired, err := cacheA.TryAcquireBatchLease(context.Background(), []int64{202, 101, 202, 0}, testTeamTrendRedisParams(), 500)
	if err != nil || !acquired || tokenA != "owner-a" {
		t.Fatalf("first acquire key/token/acquired/error = %q/%q/%v/%v", leaseKeyA, tokenA, acquired, err)
	}
	leaseKeyB, tokenB, acquired, err := cacheB.TryAcquireBatchLease(context.Background(), []int64{101, 202}, testTeamTrendRedisParams(), 500)
	if err != nil || acquired || leaseKeyB != leaseKeyA || tokenB != "owner-b" {
		t.Fatalf("second acquire key/token/acquired/error = %q/%q/%v/%v", leaseKeyB, tokenB, acquired, err)
	}
	if ttl := server.TTL(leaseKeyA); ttl != teamTrendRedisLeaseTTL {
		t.Fatalf("lease TTL = %s, want %s", ttl, teamTrendRedisLeaseTTL)
	}
	if ttl, err := cacheB.LeaseTTL(context.Background(), leaseKeyA); err != nil || ttl <= 0 || ttl > teamTrendRedisLeaseTTL {
		t.Fatalf("LeaseTTL() = %s, %v", ttl, err)
	}

	leaseKeyOther, tokenOther, acquired, err := cacheB.TryAcquireBatchLease(context.Background(), []int64{101, 303}, testTeamTrendRedisParams(), 500)
	if err != nil || !acquired || leaseKeyOther == leaseKeyA {
		t.Fatalf("different-set acquire key/token/acquired/error = %q/%q/%v/%v", leaseKeyOther, tokenOther, acquired, err)
	}
	cacheA.ReleaseBatchLease(leaseKeyA, tokenB)
	if !server.Exists(leaseKeyA) {
		t.Fatal("wrong token released batch lease")
	}
	cacheA.ReleaseBatchLease(leaseKeyA, tokenA)
	if server.Exists(leaseKeyA) {
		t.Fatal("owner token did not release batch lease")
	}
	cacheB.ReleaseBatchLease(leaseKeyOther, tokenOther)
	if !reflect.DeepEqual(metricsA.outcomesSnapshot()[:1], []string{"lease_acquired"}) || metricsB.count("lease_wait") != 1 || metricsB.count("lease_acquired") != 1 {
		t.Fatalf("lease metrics A/B = %v/%v", metricsA.outcomesSnapshot(), metricsB.outcomesSnapshot())
	}
}

func TestTeamTrendRedisCacheReleaseIsDetachedWithOneHundredMillisecondTimeout(t *testing.T) {
	server := miniredis.RunT(t)
	baseStore := newTeamTrendRedisTestStore(t, server)
	store := &teamTrendRedisReleaseInspectStore{MultiStore: baseStore}
	cache := newTeamTrendRedisTestCache(t, store, teamTrendRedisCacheOptions{
		Namespace: "test", ProviderID: 7, ProviderVersion: 3, NewToken: func() string { return "detached-owner" },
	})
	callerCtx, cancelCaller := context.WithCancel(context.Background())
	leaseKey, token, acquired, err := cache.TryAcquireBatchLease(callerCtx, []int64{101, 202}, testTeamTrendRedisParams(), 500)
	if err != nil || !acquired {
		t.Fatalf("TryAcquireBatchLease() acquired/error = %v/%v", acquired, err)
	}
	cancelCaller()
	cache.ReleaseBatchLease(leaseKey, token)

	called, canceled, timeout := store.snapshot()
	if !called || canceled {
		t.Fatalf("ReleaseLease called/canceled = %v/%v", called, canceled)
	}
	if timeout < 75*time.Millisecond || timeout > 110*time.Millisecond {
		t.Fatalf("ReleaseLease context timeout = %s, want approximately 100ms", timeout)
	}
	if server.Exists(leaseKey) {
		t.Fatal("detached release did not remove lease")
	}
}

func TestTeamTrendRedisCacheConstructorRejectsInvalidIdentity(t *testing.T) {
	server := miniredis.RunT(t)
	store := newTeamTrendRedisTestStore(t, server)
	tests := []struct {
		name    string
		options teamTrendRedisCacheOptions
	}{
		{name: "missing store", options: teamTrendRedisCacheOptions{Namespace: "test", ProviderID: 7, ProviderVersion: 3}},
		{name: "invalid namespace", options: teamTrendRedisCacheOptions{Store: store, Namespace: "bad namespace", ProviderID: 7, ProviderVersion: 3}},
		{name: "invalid provider", options: teamTrendRedisCacheOptions{Store: store, Namespace: "test", ProviderVersion: 3}},
		{name: "invalid version", options: teamTrendRedisCacheOptions{Store: store, Namespace: "test", ProviderID: 7}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := newTeamTrendRedisCache(test.options); err == nil {
				t.Fatal("newTeamTrendRedisCache() error = nil")
			}
		})
	}
}

type teamTrendRedisTestMetrics struct {
	mu       sync.Mutex
	outcomes []string
}

func (m *teamTrendRedisTestMetrics) Record(outcome string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outcomes = append(m.outcomes, outcome)
}

func (m *teamTrendRedisTestMetrics) outcomesSnapshot() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.outcomes...)
}

func (m *teamTrendRedisTestMetrics) count(want string) int {
	count := 0
	for _, outcome := range m.outcomesSnapshot() {
		if outcome == want {
			count++
		}
	}
	return count
}

type teamTrendRedisSpyStore struct {
	readcache.MultiStore
	mgetCalls    int
	setManyCalls int
	setItems     []readcache.SetItem
	mgetErr      error
	setManyErr   error
}

func (s *teamTrendRedisSpyStore) MGet(ctx context.Context, keys []string) ([][]byte, error) {
	s.mgetCalls++
	if s.mgetErr != nil {
		return nil, s.mgetErr
	}
	return s.MultiStore.MGet(ctx, keys)
}

func (s *teamTrendRedisSpyStore) SetMany(ctx context.Context, items []readcache.SetItem) error {
	s.setManyCalls++
	s.setItems = make([]readcache.SetItem, len(items))
	for index, item := range items {
		s.setItems[index] = readcache.SetItem{Key: item.Key, Value: append([]byte(nil), item.Value...), TTL: item.TTL}
	}
	if s.setManyErr != nil {
		return s.setManyErr
	}
	return s.MultiStore.SetMany(ctx, items)
}

type teamTrendRedisReleaseInspectStore struct {
	readcache.MultiStore
	mu       sync.Mutex
	called   bool
	canceled bool
	timeout  time.Duration
}

func (s *teamTrendRedisReleaseInspectStore) ReleaseLease(ctx context.Context, key, token string) (bool, error) {
	deadline, hasDeadline := ctx.Deadline()
	s.mu.Lock()
	s.called = true
	s.canceled = ctx.Err() != nil
	if hasDeadline {
		s.timeout = time.Until(deadline)
	}
	s.mu.Unlock()
	return s.MultiStore.ReleaseLease(ctx, key, token)
}

func (s *teamTrendRedisReleaseInspectStore) snapshot() (bool, bool, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.called, s.canceled, s.timeout
}

func newTeamTrendRedisTestStore(t *testing.T, server *miniredis.Miniredis) readcache.MultiStore {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return readcache.NewRedisStore(client)
}

func newTeamTrendRedisTestCache(t *testing.T, store readcache.MultiStore, options teamTrendRedisCacheOptions) *teamTrendRedisCache {
	t.Helper()
	options.Store = store
	cache, err := newTeamTrendRedisCache(options)
	if err != nil {
		t.Fatalf("newTeamTrendRedisCache() error = %v", err)
	}
	return cache
}

func testTeamTrendRedisParams() TeamMemberTrendParams {
	return TeamMemberTrendParams{StartDate: "2026-07-01", EndDate: "2026-07-20", Granularity: "daily", Timezone: "Asia/Shanghai"}
}

func withTeamTrendRedisStart(params TeamMemberTrendParams, value string) TeamMemberTrendParams {
	params.StartDate = value
	return params
}

func withTeamTrendRedisEnd(params TeamMemberTrendParams, value string) TeamMemberTrendParams {
	params.EndDate = value
	return params
}

func withTeamTrendRedisGranularity(params TeamMemberTrendParams, value string) TeamMemberTrendParams {
	params.Granularity = value
	return params
}

func withTeamTrendRedisTimezone(params TeamMemberTrendParams, value string) TeamMemberTrendParams {
	params.Timezone = value
	return params
}

func decodeTeamTrendRedisJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	return value
}

func encodeTeamTrendRedisJSON(t *testing.T, value map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode JSON: %v", err)
	}
	return string(raw)
}
