package relay

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestTeamTrendOriginsUseTwentyFourProviderWideSlots(t *testing.T) {
	if maxConcurrentTeamTrendOrigins != 24 {
		t.Fatalf("maxConcurrentTeamTrendOrigins = %d, want 24", maxConcurrentTeamTrendOrigins)
	}

	var mu sync.Mutex
	active := 0
	maxActive := 0
	requestCount := 0
	started := make(chan struct{}, 64)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	provider := newTeamTrendLimiterTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/admin/dashboard/users-trend" {
			http.Error(w, "synthetic batch failure", http.StatusBadGateway)
			return
		}
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
		writeTeamTrendLimiterResponse(t, w, 1.25)
	}))
	firstUserIDs := make([]int64, 32)
	secondUserIDs := make([]int64, 32)
	for index := range firstUserIDs {
		firstUserIDs[index] = int64(index + 1)
		secondUserIDs[index] = int64(index + 1001)
	}
	type result struct {
		points map[int64][]UsageTrendPoint
		err    error
	}
	results := make(chan result, 2)
	go func() {
		points, err := provider.GetUsageTrendForUsers(context.Background(), firstUserIDs, testTeamTrendLimiterParams())
		results <- result{points: points, err: err}
	}()
	for index := 0; index < maxConcurrentTeamTrendOrigins; index++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			unblock()
			t.Fatalf("only %d origins started before timeout, want %d", index, maxConcurrentTeamTrendOrigins)
		}
	}
	go func() {
		points, err := provider.GetUsageTrendForUsers(context.Background(), secondUserIDs, testTeamTrendLimiterParams())
		results <- result{points: points, err: err}
	}()
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	startedBeforeRelease := requestCount
	maxBeforeRelease := maxActive
	mu.Unlock()
	if startedBeforeRelease != maxConcurrentTeamTrendOrigins || maxBeforeRelease != maxConcurrentTeamTrendOrigins {
		unblock()
		t.Fatalf("requests/max before release = %d/%d, want %d/%d", startedBeforeRelease, maxBeforeRelease, maxConcurrentTeamTrendOrigins, maxConcurrentTeamTrendOrigins)
	}
	unblock()
	for index := 0; index < 2; index++ {
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		if len(got.points) != 32 {
			t.Fatalf("caller %d result size = %d, want 32", index, len(got.points))
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if maxActive != maxConcurrentTeamTrendOrigins || requestCount != 64 {
		t.Fatalf("final requests/max = %d/%d, want 64/%d", requestCount, maxActive, maxConcurrentTeamTrendOrigins)
	}
}

func TestTeamTrendOriginLimiterRejectsPreCanceledContextWithAvailableSlot(t *testing.T) {
	var limiter teamTrendOriginLimiter
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for attempt := 0; attempt < 1000; attempt++ {
		started := false
		_, err := limiter.Do(ctx, func(context.Context) ([]UsageTrendPoint, error) {
			started = true
			return nil, nil
		})
		if started {
			t.Fatalf("attempt %d started an origin for a pre-canceled context", attempt)
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("attempt %d Do() error = %v, want context.Canceled", attempt, err)
		}
	}
}

func TestTeamTrendOriginLimiterRejectsCancellationAtSlotHandoff(t *testing.T) {
	for attempt := 0; attempt < 100; attempt++ {
		var limiter teamTrendOriginLimiter
		limiter.once.Do(func() {
			limiter.slots = make(chan struct{}, maxConcurrentTeamTrendOrigins)
		})
		for range maxConcurrentTeamTrendOrigins {
			limiter.slots <- struct{}{}
		}
		baseCtx, cancel := context.WithCancel(context.Background())
		ctx := &gatedDoneContext{Context: baseCtx, entered: make(chan struct{}), resume: make(chan struct{})}
		started := make(chan struct{}, 1)
		result := make(chan error, 1)
		go func() {
			_, err := limiter.Do(ctx, func(context.Context) ([]UsageTrendPoint, error) {
				started <- struct{}{}
				return nil, nil
			})
			result <- err
		}()
		<-ctx.entered
		cancel()
		<-limiter.slots
		close(ctx.resume)
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("attempt %d Do() error = %v, want context.Canceled", attempt, err)
		}
		select {
		case <-started:
			t.Fatalf("attempt %d started an origin after cancellation at slot handoff", attempt)
		default:
		}
	}
}

func TestTeamTrendOriginLimiterDoesNotStartCanceledWaiter(t *testing.T) {
	var limiter teamTrendOriginLimiter
	release := make(chan struct{})
	started := make(chan struct{}, maxConcurrentTeamTrendOrigins+1)
	done := make(chan struct{}, maxConcurrentTeamTrendOrigins)
	for range maxConcurrentTeamTrendOrigins {
		go func() {
			defer func() { done <- struct{}{} }()
			_, _ = limiter.Do(context.Background(), func(context.Context) ([]UsageTrendPoint, error) {
				started <- struct{}{}
				<-release
				return nil, nil
			})
		}()
	}
	for range maxConcurrentTeamTrendOrigins {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("limiter did not fill all origin slots")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := limiter.Do(ctx, func(context.Context) ([]UsageTrendPoint, error) {
		started <- struct{}{}
		return nil, nil
	})
	if !errors.Is(err, context.Canceled) {
		close(release)
		t.Fatalf("Do() error = %v, want context.Canceled", err)
	}
	if len(started) != 0 {
		close(release)
		t.Fatal("canceled waiter started an origin")
	}
	close(release)
	for range maxConcurrentTeamTrendOrigins {
		<-done
	}
}

type gatedDoneContext struct {
	context.Context
	entered chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (c *gatedDoneContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	<-c.resume
	return c.Context.Done()
}

func TestTeamTrendOriginLimiterSpansCredentialGenerations(t *testing.T) {
	oldStarted := make(chan struct{}, maxConcurrentTeamTrendOrigins)
	newStarted := make(chan struct{}, 1)
	releaseOld := make(chan struct{})
	provider := newTeamTrendLimiterTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/admin/dashboard/users-trend" {
			http.Error(w, "synthetic batch failure", http.StatusBadGateway)
			return
		}
		if r.URL.Query().Get("user_id") == "1001" {
			newStarted <- struct{}{}
			writeTeamTrendLimiterResponse(t, w, 2.5)
			return
		}
		oldStarted <- struct{}{}
		select {
		case <-releaseOld:
			writeTeamTrendLimiterResponse(t, w, 1.25)
		case <-r.Context().Done():
		}
	}))
	oldUserIDs := make([]int64, maxConcurrentTeamTrendOrigins)
	for index := range oldUserIDs {
		oldUserIDs[index] = int64(index + 1)
	}
	oldDone := make(chan error, 1)
	go func() {
		_, err := provider.GetUsageTrendForUsers(context.Background(), oldUserIDs, testTeamTrendLimiterParams())
		oldDone <- err
	}()
	for range maxConcurrentTeamTrendOrigins {
		select {
		case <-oldStarted:
		case <-time.After(time.Second):
			close(releaseOld)
			t.Fatalf("old credential generation did not fill maxConcurrentTeamTrendOrigins=%d slots", maxConcurrentTeamTrendOrigins)
		}
	}
	provider.SetAdminAPIKey("new-admin-key")
	newDone := make(chan error, 1)
	go func() {
		_, err := provider.GetUsageTrendForUsers(context.Background(), []int64{1001}, testTeamTrendLimiterParams())
		newDone <- err
	}()
	select {
	case <-newStarted:
		close(releaseOld)
		t.Fatal("new credential generation started before an old origin slot was released")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseOld)
	select {
	case <-newStarted:
	case <-time.After(time.Second):
		t.Fatal("new credential generation did not start after old slots were released")
	}
	if err := <-oldDone; err != nil {
		t.Fatal(err)
	}
	if err := <-newDone; err != nil {
		t.Fatal(err)
	}
}

func newTeamTrendLimiterTestProvider(t *testing.T, handler http.Handler) *sub2apiRelay {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &sub2apiRelay{
		client: server.Client(), adminURL: server.URL, baseURL: server.URL + "/v1",
		apiKey: "test-admin-key", model: "synthetic-model-v1", logger: zap.NewNop(),
	}
}

func writeTeamTrendLimiterResponse(t *testing.T, w http.ResponseWriter, actualCost float64) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	tokens := int64(actualCost * 100)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"data": []map[string]any{{
			"date": "2026-07-19", "actual_cost": actualCost, "total_tokens": tokens,
		}},
	}); err != nil {
		t.Errorf("encode synthetic trend response: %v", err)
	}
}

func testTeamTrendLimiterParams() TeamMemberTrendParams {
	return TeamMemberTrendParams{
		StartDate: "2026-07-01", EndDate: "2026-07-19", Granularity: "day", Timezone: "Asia/Shanghai",
	}
}
