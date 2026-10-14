package opsmonitoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
)

type opsProviderStub struct {
	relay.Provider

	mu            sync.Mutex
	groups        []relay.Group
	groupsErr     error
	overviews     map[int64]json.RawMessage
	overviewErrs  map[int64]error
	accounts      map[int64][]relay.GroupAccount
	accountsErr   error
	egress        map[int64]*relay.ProxyEgressCheck
	egressErr     error
	overviewCalls int
	egressCalls   int
}

func (p *opsProviderStub) ListPlatformGroups(context.Context) ([]relay.Group, error) {
	if p.groupsErr != nil {
		return nil, p.groupsErr
	}
	return p.groups, nil
}

func (p *opsProviderStub) ReadGroupOpsHealth(_ context.Context, groupID int64, start, end time.Time) (*relay.GroupOpsHealth, error) {
	p.mu.Lock()
	p.overviewCalls++
	p.mu.Unlock()
	if start.IsZero() || !end.After(start) {
		return nil, fmt.Errorf("invalid window passed for group %d", groupID)
	}
	if err := p.overviewErrs[groupID]; err != nil {
		return nil, err
	}
	payload, ok := p.overviews[groupID]
	if !ok {
		return nil, fmt.Errorf("no overview for group %d", groupID)
	}
	return &relay.GroupOpsHealth{GroupID: groupID, Overview: payload}, nil
}

func (p *opsProviderStub) ListGroupAccounts(_ context.Context, groupID int64) ([]relay.GroupAccount, error) {
	if p.accountsErr != nil {
		return nil, p.accountsErr
	}
	return p.accounts[groupID], nil
}

func (p *opsProviderStub) TestProxy(_ context.Context, proxyID int64) (*relay.ProxyEgressCheck, error) {
	p.mu.Lock()
	p.egressCalls++
	p.mu.Unlock()
	if p.egressErr != nil {
		return nil, p.egressErr
	}
	check, ok := p.egress[proxyID]
	if !ok {
		return nil, fmt.Errorf("no egress result for proxy %d", proxyID)
	}
	return check, nil
}

func (p *opsProviderStub) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.overviewCalls
}

type opsResolverStub struct {
	provider relay.Provider
	err      error
	calls    int
}

func (r *opsResolverStub) Resolve(context.Context, int) (relay.Provider, error) {
	r.calls++
	return r.provider, r.err
}

type memoryStore struct {
	mu    sync.Mutex
	items map[string][]byte
}

func newMemoryStore() *memoryStore { return &memoryStore{items: map[string][]byte{}} }

func (s *memoryStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.items[key]
	if !ok {
		return nil, nil
	}
	return append([]byte(nil), value...), nil
}

func (s *memoryStore) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[key] = append([]byte(nil), value...)
	return nil
}

func (s *memoryStore) TryAcquireLease(context.Context, string, string, time.Duration) (bool, error) {
	return true, nil
}

func (s *memoryStore) LeaseTTL(context.Context, string) (time.Duration, error) { return 0, nil }

func (s *memoryStore) ReleaseLease(context.Context, string, string) (bool, error) { return true, nil }

func (s *memoryStore) snapshots() []GroupsSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	var snapshots []GroupsSnapshot
	for _, raw := range s.items {
		var snapshot GroupsSnapshot
		if json.Unmarshal(raw, &snapshot) == nil {
			snapshots = append(snapshots, snapshot)
		}
	}
	return snapshots
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(delta time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(delta)
}

func newTestClock() *testClock {
	return &testClock{now: time.Date(2026, 10, 14, 7, 0, 0, 0, time.UTC)}
}

func newStubProvider() *opsProviderStub {
	return &opsProviderStub{
		groups: []relay.Group{
			{ID: 7, Name: "Group Alpha", Platform: "openai", AccountCount: 12, ActiveAccountCount: 11},
			{ID: 8, Name: "Group Beta", Platform: "openai", AccountCount: 3, ActiveAccountCount: 3},
			{ID: 9, Name: "Group Gamma", Platform: "deepseek", AccountCount: 1, ActiveAccountCount: 1},
		},
		overviews: map[int64]json.RawMessage{
			7: json.RawMessage(`{"health_score":10,"upstream_error_rate":0.5,"request_count_total":100}`),
			8: json.RawMessage(`{"health_score":90,"upstream_error_rate":0.01,"request_count_total":5}`),
			9: json.RawMessage(`{"health_score":80,"upstream_error_rate":0.02,"request_count_total":3}`),
		},
	}
}

func newTestService(provider relay.Provider, cache *Cache) (*Service, *testClock) {
	clock := newTestClock()
	service := NewService(&opsResolverStub{provider: provider}, cache)
	service.now = clock.Now
	return service, clock
}

func overviewHealth(t *testing.T, row GroupRow) float64 {
	t.Helper()
	if len(row.Overview) == 0 {
		t.Fatalf("group %d has no overview", row.GroupID)
	}
	var parsed struct {
		HealthScore float64 `json:"health_score"`
	}
	if err := json.Unmarshal(row.Overview, &parsed); err != nil {
		t.Fatalf("decode overview for group %d: %v", row.GroupID, err)
	}
	return parsed.HealthScore
}

func TestGroupsFansOutEveryGroupWithVerbatimOverview(t *testing.T) {
	provider := newStubProvider()
	service, _ := newTestService(provider, nil)

	snapshot, err := service.Groups(context.Background(), GroupsRequest{ProviderID: 1})
	if err != nil {
		t.Fatalf("Groups() error = %v", err)
	}
	if snapshot.Window != DefaultWindow {
		t.Fatalf("window = %q, want %q", snapshot.Window, DefaultWindow)
	}
	if snapshot.Refreshing {
		t.Fatal("cold load must not report refreshing")
	}
	if snapshot.FailedGroupCount != 0 {
		t.Fatalf("failed group count = %d, want 0", snapshot.FailedGroupCount)
	}
	if len(snapshot.Groups) != 3 {
		t.Fatalf("rows = %d, want 3", len(snapshot.Groups))
	}
	if got := snapshot.EndTime.Sub(snapshot.StartTime); got != time.Hour {
		t.Fatalf("window span = %v, want 1h", got)
	}
	if !snapshot.AsOf.Equal(snapshot.EndTime) {
		t.Fatalf("as_of = %v, want %v", snapshot.AsOf, snapshot.EndTime)
	}

	row := snapshot.Groups[0]
	if row.GroupID != 7 || row.GroupName != "Group Alpha" || row.Platform != "openai" {
		t.Fatalf("row identity = %+v", row)
	}
	if row.AccountCount != 12 || row.ActiveAccountCount != 11 {
		t.Fatalf("account facts = %d/%d, want 12/11", row.AccountCount, row.ActiveAccountCount)
	}
	if row.Status != GroupStatusOK || row.Error != "" {
		t.Fatalf("row status = %q error = %q", row.Status, row.Error)
	}
	// The overview must reach the caller byte-for-byte: this platform never
	// re-models upstream metrics.
	if string(row.Overview) != string(provider.overviews[7]) {
		t.Fatalf("overview mutated:\n got %s\nwant %s", row.Overview, provider.overviews[7])
	}
}

func TestGroupsIsolatesOneGroupFailure(t *testing.T) {
	provider := newStubProvider()
	provider.overviewErrs = map[int64]error{8: errors.New("upstream timeout")}
	service, _ := newTestService(provider, nil)

	snapshot, err := service.Groups(context.Background(), GroupsRequest{ProviderID: 1})
	if err != nil {
		t.Fatalf("Groups() error = %v", err)
	}
	if snapshot.FailedGroupCount != 1 {
		t.Fatalf("failed group count = %d, want 1", snapshot.FailedGroupCount)
	}
	if len(snapshot.Groups) != 3 {
		t.Fatalf("rows = %d, want 3 (failures must not drop rows)", len(snapshot.Groups))
	}

	failed := snapshot.Groups[1]
	if failed.GroupID != 8 || failed.Status != GroupStatusFailed {
		t.Fatalf("failed row = %+v", failed)
	}
	if failed.Error == "" {
		t.Fatal("failed row must carry the upstream error")
	}
	if len(failed.Overview) != 0 {
		t.Fatal("failed row must not carry an overview")
	}
	if failed.GroupName != "Group Beta" {
		t.Fatalf("failed row lost its identity: %+v", failed)
	}
	for _, index := range []int{0, 2} {
		if snapshot.Groups[index].Status != GroupStatusOK {
			t.Fatalf("group %d status = %q, want ok", snapshot.Groups[index].GroupID, snapshot.Groups[index].Status)
		}
	}
}

func TestGroupsForceSkipsTheCachedSnapshot(t *testing.T) {
	provider := newStubProvider()
	store := newMemoryStore()
	cache, err := NewCache(store, CacheOptions{Namespace: "test"})
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	service, _ := newTestService(provider, cache)

	if _, err := service.Groups(context.Background(), GroupsRequest{ProviderID: 1}); err != nil {
		t.Fatalf("Groups() error = %v", err)
	}
	if provider.calls() != 3 {
		t.Fatalf("first load upstream calls = %d, want 3", provider.calls())
	}

	// A second read inside the freshness window is answered from cache.
	if _, err := service.Groups(context.Background(), GroupsRequest{ProviderID: 1}); err != nil {
		t.Fatalf("Groups() error = %v", err)
	}
	if provider.calls() != 3 {
		t.Fatalf("cached read upstream calls = %d, want 3", provider.calls())
	}

	// An explicit operator refresh is not: it must go upstream again.
	if _, err := service.Groups(context.Background(), GroupsRequest{ProviderID: 1, Force: true}); err != nil {
		t.Fatalf("forced Groups() error = %v", err)
	}
	if provider.calls() != 6 {
		t.Fatalf("forced read upstream calls = %d, want 6", provider.calls())
	}
}

func TestGroupsDoesNotCacheASnapshotWithFailedGroups(t *testing.T) {
	provider := newStubProvider()
	provider.overviewErrs = map[int64]error{8: errors.New("upstream timeout")}
	store := newMemoryStore()
	cache, err := NewCache(store, CacheOptions{Namespace: "test"})
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	service, _ := newTestService(provider, cache)

	first, err := service.Groups(context.Background(), GroupsRequest{ProviderID: 1})
	if err != nil {
		t.Fatalf("Groups() error = %v", err)
	}
	if first.FailedGroupCount != 1 {
		t.Fatalf("failed group count = %d, want 1", first.FailedGroupCount)
	}

	// A cached failure would outlive the failure. The next read must retry.
	delete(provider.overviewErrs, 8)
	second, err := service.Groups(context.Background(), GroupsRequest{ProviderID: 1})
	if err != nil {
		t.Fatalf("Groups() error = %v", err)
	}
	if second.FailedGroupCount != 0 {
		t.Fatalf("second read failed groups = %d, want 0 (a failed snapshot was cached)", second.FailedGroupCount)
	}
	if provider.calls() != 6 {
		t.Fatalf("upstream calls = %d, want 6", provider.calls())
	}
}

func TestGroupsReportsProviderLevelFailure(t *testing.T) {
	provider := newStubProvider()
	provider.groupsErr = errors.New("relay unreachable")
	service, _ := newTestService(provider, nil)

	if _, err := service.Groups(context.Background(), GroupsRequest{ProviderID: 1}); err == nil {
		t.Fatal("provider-level failure must fail the whole read")
	}
}

func TestGroupsRejectsProviderWithoutOpsSupport(t *testing.T) {
	service, _ := newTestService(struct{ relay.Provider }{}, nil)
	if _, err := service.Groups(context.Background(), GroupsRequest{ProviderID: 1}); err == nil {
		t.Fatal("provider without ops support must be rejected")
	}
}

func TestParseWindow(t *testing.T) {
	for _, testCase := range []struct {
		raw      string
		wantKey  string
		wantSpan time.Duration
	}{
		{"", "1h", time.Hour},
		{"1h", "1h", time.Hour},
		{"6H", "6h", 6 * time.Hour},
		{" 24h ", "24h", 24 * time.Hour},
	} {
		key, span, err := ParseWindow(testCase.raw)
		if err != nil {
			t.Fatalf("ParseWindow(%q) error = %v", testCase.raw, err)
		}
		if key != testCase.wantKey || span != testCase.wantSpan {
			t.Fatalf("ParseWindow(%q) = %q/%v, want %q/%v", testCase.raw, key, span, testCase.wantKey, testCase.wantSpan)
		}
	}
	if _, _, err := ParseWindow("3h"); err == nil {
		t.Fatal("unsupported window must be rejected instead of silently substituted")
	}
}

func TestGroupsServesFreshCacheWithoutUpstreamCall(t *testing.T) {
	provider := newStubProvider()
	store := newMemoryStore()
	cache, err := NewCache(store, CacheOptions{Namespace: "test"})
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	service, clock := newTestService(provider, cache)

	first, err := service.Groups(context.Background(), GroupsRequest{ProviderID: 1})
	if err != nil {
		t.Fatalf("Groups() error = %v", err)
	}
	if got := provider.calls(); got != 3 {
		t.Fatalf("cold load upstream calls = %d, want 3", got)
	}

	clock.Advance(30 * time.Second)
	second, err := service.Groups(context.Background(), GroupsRequest{ProviderID: 1})
	if err != nil {
		t.Fatalf("Groups() error = %v", err)
	}
	if got := provider.calls(); got != 3 {
		t.Fatalf("fresh cache still hit upstream: %d calls", got)
	}
	if second.Refreshing {
		t.Fatal("fresh cache must not report refreshing")
	}
	if !second.AsOf.Equal(first.AsOf) {
		t.Fatalf("fresh cache returned as_of %v, want %v", second.AsOf, first.AsOf)
	}
}

func TestGroupsServesStaleCacheAndRefreshesBehindTheResponse(t *testing.T) {
	provider := newStubProvider()
	store := newMemoryStore()
	cache, err := NewCache(store, CacheOptions{Namespace: "test"})
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	service, clock := newTestService(provider, cache)

	if _, err := service.Groups(context.Background(), GroupsRequest{ProviderID: 1}); err != nil {
		t.Fatalf("Groups() error = %v", err)
	}
	if got := provider.calls(); got != 3 {
		t.Fatalf("cold load upstream calls = %d, want 3", got)
	}

	provider.mu.Lock()
	provider.overviews[7] = json.RawMessage(`{"health_score":99,"upstream_error_rate":0.5,"request_count_total":100}`)
	provider.mu.Unlock()
	clock.Advance(2 * time.Minute)

	stale, err := service.Groups(context.Background(), GroupsRequest{ProviderID: 1})
	if err != nil {
		t.Fatalf("Groups() error = %v", err)
	}
	if !stale.Refreshing {
		t.Fatal("stale cache must report refreshing")
	}
	if health := overviewHealth(t, stale.Groups[0]); health != 10 {
		t.Fatalf("stale read returned health %v, want cached 10", health)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		refreshed := false
		for _, snapshot := range store.snapshots() {
			if len(snapshot.Groups) > 0 && strings.Contains(string(snapshot.Groups[0].Overview), `"health_score":99`) {
				refreshed = true
			}
		}
		if refreshed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background refresh never updated the cached snapshot")
		}
		time.Sleep(5 * time.Millisecond)
	}

	fresh, err := service.Groups(context.Background(), GroupsRequest{ProviderID: 1})
	if err != nil {
		t.Fatalf("Groups() error = %v", err)
	}
	if fresh.Refreshing {
		t.Fatal("refreshed snapshot must serve as fresh")
	}
	if health := overviewHealth(t, fresh.Groups[0]); health != 99 {
		t.Fatalf("refreshed health = %v, want 99", health)
	}
}

func TestGroupAccountsReturnsProxyBinding(t *testing.T) {
	proxyID := int64(3)
	provider := newStubProvider()
	provider.accounts = map[int64][]relay.GroupAccount{
		7: {{
			AccountID:   50,
			Name:        "alice@example.com",
			Platform:    "openai",
			Type:        "oauth",
			Status:      "active",
			Schedulable: true,
			ProxyID:     &proxyID,
			Proxy:       &relay.GroupProxy{ID: 3, Name: "proxy-alpha", Protocol: "socks5h", Host: "198.51.100.7", Port: 1080, Status: "active"},
		}},
	}
	service, _ := newTestService(provider, nil)

	snapshot, err := service.GroupAccounts(context.Background(), GroupAccountsRequest{ProviderID: 1, GroupID: 7})
	if err != nil {
		t.Fatalf("GroupAccounts() error = %v", err)
	}
	if snapshot.GroupID != 7 || len(snapshot.Accounts) != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Accounts[0].Proxy == nil || snapshot.Accounts[0].Proxy.Host != "198.51.100.7" {
		t.Fatalf("proxy binding lost: %+v", snapshot.Accounts[0])
	}
}

func TestProxyEgressCachesProbeResult(t *testing.T) {
	provider := newStubProvider()
	provider.egress = map[int64]*relay.ProxyEgressCheck{
		3: {Success: true, Message: "Proxy is accessible", LatencyMS: 203, IPAddress: "198.51.100.7", City: "Example City", Country: "Example Country", CountryCode: "US"},
	}
	store := newMemoryStore()
	cache, err := NewCache(store, CacheOptions{Namespace: "test"})
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	service, _ := newTestService(provider, cache)

	first, err := service.ProxyEgress(context.Background(), ProxyEgressRequest{ProviderID: 1, ProxyID: 3})
	if err != nil {
		t.Fatalf("ProxyEgress() error = %v", err)
	}
	if first.Cached || first.IPAddress != "198.51.100.7" {
		t.Fatalf("first probe = %+v", first)
	}

	second, err := service.ProxyEgress(context.Background(), ProxyEgressRequest{ProviderID: 1, ProxyID: 3})
	if err != nil {
		t.Fatalf("ProxyEgress() error = %v", err)
	}
	if !second.Cached {
		t.Fatal("second probe must be served from cache")
	}
	if provider.egressCalls != 1 {
		t.Fatalf("upstream probe calls = %d, want 1", provider.egressCalls)
	}
}

func TestProxyEgressForceReprobesInsteadOfReplayingCache(t *testing.T) {
	provider := newStubProvider()
	provider.egress = map[int64]*relay.ProxyEgressCheck{
		3: {Success: true, Message: "Proxy is accessible", LatencyMS: 203, IPAddress: "198.51.100.7", City: "Example City", Country: "Example Country", CountryCode: "US"},
	}
	store := newMemoryStore()
	cache, err := NewCache(store, CacheOptions{Namespace: "test"})
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	service, _ := newTestService(provider, cache)

	if _, err := service.ProxyEgress(context.Background(), ProxyEgressRequest{ProviderID: 1, ProxyID: 3}); err != nil {
		t.Fatalf("ProxyEgress() error = %v", err)
	}

	// The proxy was fixed upstream: a forced probe must observe the new address
	// instead of replaying the six-hour-old cached measurement.
	provider.egress[3] = &relay.ProxyEgressCheck{Success: true, Message: "Proxy is accessible", LatencyMS: 42, IPAddress: "203.0.113.9", City: "Example City", Country: "Example Country", CountryCode: "US"}

	forced, err := service.ProxyEgress(context.Background(), ProxyEgressRequest{ProviderID: 1, ProxyID: 3, Force: true})
	if err != nil {
		t.Fatalf("forced ProxyEgress() error = %v", err)
	}
	if forced.Cached {
		t.Fatal("forced probe must not be reported as cached")
	}
	if forced.IPAddress != "203.0.113.9" || forced.LatencyMS != 42 {
		t.Fatalf("forced probe replayed the cache: %+v", forced)
	}
	if provider.egressCalls != 2 {
		t.Fatalf("upstream probe calls = %d, want 2", provider.egressCalls)
	}
}

func TestGroupAccountsRejectsInvalidGroupID(t *testing.T) {
	service, _ := newTestService(newStubProvider(), nil)
	if _, err := service.GroupAccounts(context.Background(), GroupAccountsRequest{ProviderID: 1}); err == nil {
		t.Fatal("missing group id must be rejected")
	}
}
