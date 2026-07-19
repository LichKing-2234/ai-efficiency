package relay

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestTeamTrendCacheReusesNormalizedSuccessfulResult(t *testing.T) {
	now := time.Date(2026, 7, 19, 8, 0, 0, 0, time.UTC)
	cache := teamTrendCache{now: func() time.Time { return now }}
	calls := 0
	load := func(context.Context) ([]UsageTrendPoint, error) {
		calls++
		tokens := int64(42)
		return []UsageTrendPoint{{Date: "2026-07-19", ActualCost: 1.5, TotalTokens: &tokens}}, nil
	}

	first, err := cache.GetOrLoad(context.Background(), 101, TeamMemberTrendParams{
		StartDate: " 2026-07-01 ", EndDate: "2026-07-19", Granularity: " day ", Timezone: " Asia/Shanghai ",
	}, load)
	if err != nil {
		t.Fatal(err)
	}
	second, err := cache.GetOrLoad(context.Background(), 101, TeamMemberTrendParams{
		StartDate: "2026-07-01", EndDate: " 2026-07-19 ", Granularity: "day", Timezone: "Asia/Shanghai",
	}, load)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(first) != 1 || len(second) != 1 {
		t.Fatalf("calls/results = %d/%d/%d, want 1/1/1", calls, len(first), len(second))
	}
	*first[0].TotalTokens = 99
	first[0].ActualCost = 9.9
	if *second[0].TotalTokens != 42 || second[0].ActualCost != 1.5 {
		t.Fatalf("cached result mutated through caller: %#v", second[0])
	}
}

func TestTeamTrendCacheExpiresAtSixtySeconds(t *testing.T) {
	now := time.Date(2026, 7, 19, 8, 0, 0, 0, time.UTC)
	cache := teamTrendCache{now: func() time.Time { return now }}
	calls := 0
	load := func(context.Context) ([]UsageTrendPoint, error) {
		calls++
		return []UsageTrendPoint{{Date: "2026-07-19", ActualCost: float64(calls)}}, nil
	}
	params := testTeamTrendCacheParams()

	first, err := cache.GetOrLoad(context.Background(), 101, params, load)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(teamTrendCacheTTL - time.Nanosecond)
	second, err := cache.GetOrLoad(context.Background(), 101, params, load)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Nanosecond)
	third, err := cache.GetOrLoad(context.Background(), 101, params, load)
	if err != nil {
		t.Fatal(err)
	}

	if calls != 2 || first[0].ActualCost != 1 || second[0].ActualCost != 1 || third[0].ActualCost != 2 {
		t.Fatalf("calls/costs = %d/%v/%v/%v, want 2/1/1/2", calls, first[0].ActualCost, second[0].ActualCost, third[0].ActualCost)
	}
}

func TestTeamTrendCacheStoresSuccessfulEmptyResult(t *testing.T) {
	cache := teamTrendCache{}
	calls := 0
	load := func(context.Context) ([]UsageTrendPoint, error) {
		calls++
		return nil, nil
	}

	for range 2 {
		points, err := cache.GetOrLoad(context.Background(), 101, testTeamTrendCacheParams(), load)
		if err != nil {
			t.Fatal(err)
		}
		if points != nil {
			t.Fatalf("points = %#v, want nil successful result", points)
		}
	}
	if calls != 1 {
		t.Fatalf("origin calls = %d, want 1", calls)
	}
}

func TestTeamTrendCacheSeparatesEveryIdentityDimension(t *testing.T) {
	cache := teamTrendCache{}
	base := testTeamTrendCacheParams()
	cases := []struct {
		userID int64
		params TeamMemberTrendParams
	}{
		{userID: 101, params: base},
		{userID: 102, params: base},
		{userID: 101, params: withTeamTrendStartDate(base, "2026-07-02")},
		{userID: 101, params: withTeamTrendEndDate(base, "2026-07-18")},
		{userID: 101, params: withTeamTrendGranularity(base, "hour")},
		{userID: 101, params: withTeamTrendTimezone(base, "UTC")},
	}
	calls := 0
	load := func(context.Context) ([]UsageTrendPoint, error) {
		calls++
		return []UsageTrendPoint{{Date: "2026-07-19", ActualCost: float64(calls)}}, nil
	}

	for _, test := range cases {
		if _, err := cache.GetOrLoad(context.Background(), test.userID, test.params, load); err != nil {
			t.Fatal(err)
		}
	}
	if calls != len(cases) {
		t.Fatalf("origin calls = %d, want %d distinct identities", calls, len(cases))
	}
}

func TestTeamTrendCacheBypassesNonPositiveUserIDs(t *testing.T) {
	cache := teamTrendCache{}
	calls := 0
	load := func(context.Context) ([]UsageTrendPoint, error) {
		calls++
		return []UsageTrendPoint{{Date: "2026-07-19"}}, nil
	}

	for range 2 {
		if _, err := cache.GetOrLoad(context.Background(), 0, testTeamTrendCacheParams(), load); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("origin calls = %d, want 2 uncached calls", calls)
	}
	if len(cache.entries) != 0 {
		t.Fatalf("cache entries = %d, want 0", len(cache.entries))
	}
}

func TestTeamTrendCacheNeverExceedsCapacity(t *testing.T) {
	now := time.Date(2026, 7, 19, 8, 0, 0, 0, time.UTC)
	cache := teamTrendCache{now: func() time.Time { return now }}
	params := testTeamTrendCacheParams()

	for index := 1; index <= teamTrendCacheCapacity+1; index++ {
		userID := int64(index)
		_, err := cache.GetOrLoad(context.Background(), userID, params, func(context.Context) ([]UsageTrendPoint, error) {
			return []UsageTrendPoint{{Date: "2026-07-19", ActualCost: float64(userID)}}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(cache.entries) > teamTrendCacheCapacity {
			t.Fatalf("cache entries after user %d = %d, maximum %d", userID, len(cache.entries), teamTrendCacheCapacity)
		}
		now = now.Add(time.Nanosecond)
	}
	if len(cache.entries) != teamTrendCacheCapacity {
		t.Fatalf("cache entries = %d, want %d", len(cache.entries), teamTrendCacheCapacity)
	}
	if _, ok := cache.entries[normalizedTeamTrendCacheKey(1, params)]; ok {
		t.Fatal("earliest-expiring entry was not evicted")
	}
}

func TestTeamTrendCacheCollapsesFourLaneFanout(t *testing.T) {
	var upstreamCalls atomic.Int64
	started := make(chan struct{}, 64)
	release := make(chan struct{})
	provider := newTeamTrendCacheTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		writeTeamTrendCacheResponse(t, w, 1.25, false)
	}))
	userIDs := make([]int64, 235)
	for index := range userIDs {
		userIDs[index] = int64(index + 1)
	}

	start := make(chan struct{})
	type result struct {
		points map[int64][]UsageTrendPoint
		err    error
	}
	results := make(chan result, 4)
	for range 4 {
		go func() {
			<-start
			points, err := provider.GetUsageTrendForUsers(context.Background(), userIDs, testTeamTrendCacheParams())
			results <- result{points: points, err: err}
		}()
	}
	close(start)
	for index := 0; index < 8; index++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatalf("only %d upstream requests started before timeout, want at least 8", index)
		}
	}
	time.Sleep(50 * time.Millisecond)
	close(release)

	for index := 0; index < 4; index++ {
		select {
		case got := <-results:
			if got.err != nil {
				t.Fatal(got.err)
			}
			if len(got.points) != len(userIDs) {
				t.Fatalf("caller %d result size = %d, want %d", index, len(got.points), len(userIDs))
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("caller %d did not finish", index)
		}
	}
	if got := upstreamCalls.Load(); got != int64(len(userIDs)) {
		t.Fatalf("upstream trend calls = %d, want %d for four identical callers", got, len(userIDs))
	}
}

func TestTeamTrendCachePreservesEightWorkerCallerLimit(t *testing.T) {
	var mu sync.Mutex
	active := 0
	maxActive := 0
	requestCount := 0
	started := make(chan struct{}, 16)
	release := make(chan struct{})
	provider := newTeamTrendCacheTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		active++
		requestCount++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		defer func() {
			mu.Lock()
			active--
			mu.Unlock()
		}()
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		writeTeamTrendCacheResponse(t, w, 1.25, false)
	}))
	userIDs := make([]int64, 16)
	for index := range userIDs {
		userIDs[index] = int64(index + 1)
	}
	done := make(chan error, 1)
	go func() {
		_, err := provider.GetUsageTrendForUsers(context.Background(), userIDs, testTeamTrendCacheParams())
		done <- err
	}()
	for index := 0; index < 8; index++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatalf("only %d requests started before timeout, want 8", index)
		}
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	startedBeforeRelease := requestCount
	maxBeforeRelease := maxActive
	mu.Unlock()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if startedBeforeRelease != 8 || maxBeforeRelease != 8 || maxActive != 8 || requestCount != 16 {
		t.Fatalf("requests/max = before %d/%d final %d/%d, want 8/8 and 16/8", startedBeforeRelease, maxBeforeRelease, requestCount, maxActive)
	}
}

func TestTeamTrendCacheDoesNotCacheErrors(t *testing.T) {
	var calls atomic.Int64
	provider := newTeamTrendCacheTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "synthetic failure", http.StatusBadGateway)
			return
		}
		writeTeamTrendCacheResponse(t, w, 2.5, false)
	}))

	if _, err := provider.GetUsageTrendForUsers(context.Background(), []int64{101}, testTeamTrendCacheParams()); err == nil {
		t.Fatal("first request error = nil, want synthetic upstream failure")
	}
	points, err := provider.GetUsageTrendForUsers(context.Background(), []int64{101}, testTeamTrendCacheParams())
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || len(points[101]) != 1 || points[101][0].ActualCost != 2.5 {
		t.Fatalf("calls/points = %d/%#v, want 2/success", calls.Load(), points[101])
	}
}

func TestTeamTrendCacheCachesSuccessfulEmptyTrend(t *testing.T) {
	var calls atomic.Int64
	provider := newTeamTrendCacheTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeTeamTrendCacheResponse(t, w, 0, true)
	}))

	for range 2 {
		points, err := provider.GetUsageTrendForUsers(context.Background(), []int64{101}, testTeamTrendCacheParams())
		if err != nil {
			t.Fatal(err)
		}
		if len(points[101]) != 0 {
			t.Fatalf("points = %#v, want successful empty trend", points[101])
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want 1", calls.Load())
	}
}

func TestTeamTrendCacheKeepsFlightForRemainingWaiterAfterCancel(t *testing.T) {
	var calls atomic.Int64
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	provider := newTeamTrendCacheTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-release:
			writeTeamTrendCacheResponse(t, w, 3.5, false)
		case <-r.Context().Done():
		}
	}))

	canceledCtx, cancel := context.WithCancel(context.Background())
	type result struct {
		points map[int64][]UsageTrendPoint
		err    error
	}
	results := make(chan result, 2)
	go func() {
		points, err := provider.GetUsageTrendForUsers(canceledCtx, []int64{101}, testTeamTrendCacheParams())
		results <- result{points: points, err: err}
	}()
	go func() {
		points, err := provider.GetUsageTrendForUsers(context.Background(), []int64{101}, testTeamTrendCacheParams())
		results <- result{points: points, err: err}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("shared origin did not start")
	}
	time.Sleep(50 * time.Millisecond)
	cancel()
	close(release)

	var canceledResult, successResult *result
	for range 2 {
		got := <-results
		if errors.Is(got.err, context.Canceled) {
			copy := got
			canceledResult = &copy
		} else if got.err == nil {
			copy := got
			successResult = &copy
		} else {
			t.Fatalf("unexpected waiter error: %v", got.err)
		}
	}
	if canceledResult == nil || successResult == nil || len(successResult.points[101]) != 1 {
		t.Fatalf("waiter results = canceled %#v success %#v", canceledResult, successResult)
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want 1 shared origin", calls.Load())
	}
}

func TestTeamTrendCacheDoesNotStoreWhenOnlyWaiterCancels(t *testing.T) {
	var calls atomic.Int64
	started := make(chan struct{}, 1)
	provider := newTeamTrendCacheTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if call == 1 {
			started <- struct{}{}
			<-r.Context().Done()
			return
		}
		writeTeamTrendCacheResponse(t, w, 4.5, false)
	}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := provider.GetUsageTrendForUsers(ctx, []int64{101}, testTeamTrendCacheParams())
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request error = %v, want context.Canceled", err)
	}
	points, err := provider.GetUsageTrendForUsers(context.Background(), []int64{101}, testTeamTrendCacheParams())
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || len(points[101]) != 1 {
		t.Fatalf("calls/points = %d/%#v, want 2/success", calls.Load(), points[101])
	}
}

func TestTeamTrendCacheDoesNotStoreSuccessReturnedAfterOnlyWaiterCancels(t *testing.T) {
	cache := teamTrendCache{}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	originReturned := make(chan struct{})
	calls := 0
	done := make(chan error, 1)
	go func() {
		_, err := cache.GetOrLoad(ctx, 101, testTeamTrendCacheParams(), func(context.Context) ([]UsageTrendPoint, error) {
			calls++
			close(started)
			<-release
			close(originReturned)
			return []UsageTrendPoint{{Date: "2026-07-19", ActualCost: 1}}, nil
		})
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request error = %v, want context.Canceled", err)
	}
	close(release)
	<-originReturned
	time.Sleep(10 * time.Millisecond)

	points, err := cache.GetOrLoad(context.Background(), 101, testTeamTrendCacheParams(), func(context.Context) ([]UsageTrendPoint, error) {
		calls++
		return []UsageTrendPoint{{Date: "2026-07-19", ActualCost: 2}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || points[0].ActualCost != 2 {
		t.Fatalf("calls/points = %d/%#v, want uncached second origin", calls, points)
	}
}

func TestTeamTrendCacheSeparatesCredentialGenerations(t *testing.T) {
	var calls atomic.Int64
	oldStarted := make(chan struct{}, 1)
	releaseOld := make(chan struct{})
	provider := newTeamTrendCacheTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.Header.Get("X-API-Key") {
		case "test-admin-key":
			oldStarted <- struct{}{}
			select {
			case <-releaseOld:
				writeTeamTrendCacheResponse(t, w, 1, false)
			case <-r.Context().Done():
			}
		case "new-admin-key":
			writeTeamTrendCacheResponse(t, w, 2, false)
		default:
			http.Error(w, "unexpected synthetic key", http.StatusUnauthorized)
		}
	}))
	oldDone := make(chan error, 1)
	go func() {
		_, err := provider.GetUsageTrendForUsers(context.Background(), []int64{101}, testTeamTrendCacheParams())
		oldDone <- err
	}()
	<-oldStarted
	provider.SetAdminAPIKey("new-admin-key")
	newPoints, err := provider.GetUsageTrendForUsers(context.Background(), []int64{101}, testTeamTrendCacheParams())
	if err != nil {
		close(releaseOld)
		t.Fatal(err)
	}
	close(releaseOld)
	if err := <-oldDone; err != nil {
		t.Fatal(err)
	}
	reused, err := provider.GetUsageTrendForUsers(context.Background(), []int64{101}, testTeamTrendCacheParams())
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || newPoints[101][0].ActualCost != 2 || reused[101][0].ActualCost != 2 {
		t.Fatalf("calls/new/reused = %d/%#v/%#v, want 2/new-generation values", calls.Load(), newPoints[101], reused[101])
	}
}

func TestTeamTrendCacheKeepsEntriesForEquivalentCredentialAndModelChanges(t *testing.T) {
	var calls atomic.Int64
	provider := newTeamTrendCacheTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeTeamTrendCacheResponse(t, w, 5.5, false)
	}))
	if _, err := provider.GetUsageTrendForUsers(context.Background(), []int64{101}, testTeamTrendCacheParams()); err != nil {
		t.Fatal(err)
	}
	provider.SetAdminAPIKey(" test-admin-key ")
	provider.SetModel("synthetic-model-v2")
	if _, err := provider.GetUsageTrendForUsers(context.Background(), []int64{101}, testTeamTrendCacheParams()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want 1 after equivalent key and model change", calls.Load())
	}
}

func newTeamTrendCacheTestProvider(t *testing.T, handler http.Handler) *sub2apiRelay {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &sub2apiRelay{
		client: server.Client(), adminURL: server.URL, baseURL: server.URL + "/v1",
		apiKey: "test-admin-key", model: "synthetic-model-v1", logger: zap.NewNop(),
	}
}

func writeTeamTrendCacheResponse(t *testing.T, w http.ResponseWriter, actualCost float64, empty bool) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	data := []map[string]any{}
	if !empty {
		tokens := int64(actualCost * 100)
		data = append(data, map[string]any{
			"date": "2026-07-19", "actual_cost": actualCost, "total_tokens": tokens,
		})
	}
	if err := json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data}); err != nil {
		t.Errorf("encode synthetic trend response: %v", err)
	}
}

func testTeamTrendCacheParams() TeamMemberTrendParams {
	return TeamMemberTrendParams{
		StartDate: "2026-07-01", EndDate: "2026-07-19", Granularity: "day", Timezone: "Asia/Shanghai",
	}
}

func withTeamTrendStartDate(params TeamMemberTrendParams, value string) TeamMemberTrendParams {
	params.StartDate = value
	return params
}

func withTeamTrendEndDate(params TeamMemberTrendParams, value string) TeamMemberTrendParams {
	params.EndDate = value
	return params
}

func withTeamTrendGranularity(params TeamMemberTrendParams, value string) TeamMemberTrendParams {
	params.Granularity = value
	return params
}

func withTeamTrendTimezone(params TeamMemberTrendParams, value string) TeamMemberTrendParams {
	params.Timezone = value
	return params
}
