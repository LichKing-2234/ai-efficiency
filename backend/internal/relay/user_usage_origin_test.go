package relay_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
)

func TestReadUserUsageOriginCombinedFansOutAfterOneLogin(t *testing.T) {
	started := make(chan string, 5)
	release := make(chan struct{})
	var mu sync.Mutex
	counts := map[string]int{}

	mux := http.NewServeMux()
	registerUsageOriginLoginHandlers(mux, &mu, counts)
	registerBlockingOriginHandler(mux, "/api/v1/usage/dashboard/stats", "stats", started, release, func(w http.ResponseWriter) {
		writeOriginEnvelope(w, map[string]any{"total_requests": 3})
	})
	registerBlockingOriginHandler(mux, "/api/v1/usage/dashboard/trend", "trend", started, release, func(w http.ResponseWriter) {
		writeOriginEnvelope(w, map[string]any{"start_date": "2026-07-01", "end_date": "2026-07-15", "granularity": "day", "trend": []any{}})
	})
	registerBlockingOriginHandler(mux, "/api/v1/usage/dashboard/models", "models", started, release, func(w http.ResponseWriter) {
		writeOriginEnvelope(w, map[string]any{"models": []any{}})
	})
	registerBlockingOriginHandler(mux, "/api/v1/admin/users/7/api-keys", "keys", started, release, func(w http.ResponseWriter) {
		writeOriginEnvelope(w, map[string]any{
			"items": []any{map[string]any{"id": 11, "user_id": 7, "status": "active"}},
			"page":  1,
			"pages": 1,
		})
	})
	registerBlockingOriginHandler(mux, "/api/v1/admin/users/7/subscriptions", "subscriptions", started, release, func(w http.ResponseWriter) {
		writeOriginEnvelope(w, []any{})
	})

	reader := newTestProvider(t, mux).(relay.UserUsageOriginReader)
	type readOutcome struct {
		result *relay.UserUsageOriginResult
		err    error
	}
	done := make(chan readOutcome, 1)
	go func() {
		result, err := reader.ReadUserUsageOrigin(context.Background(), relay.UserUsageOriginRequest{
			Login:       "alice@example.com",
			Password:    "test-password",
			RelayUserID: 7,
			Params: relay.UserUsageDashboardParams{
				StartDate:   "2026-07-01",
				EndDate:     "2026-07-15",
				Granularity: "day",
				Timezone:    "Asia/Shanghai",
			},
			Branches: relay.UserUsageOriginBranches{Usage: true, Quota: true},
		})
		done <- readOutcome{result: result, err: err}
	}()

	seen := make(map[string]bool, 5)
	for len(seen) < 5 {
		select {
		case name := <-started:
			seen[name] = true
		case <-time.After(time.Second):
			t.Fatalf("only %d origin branches started before release: %v", len(seen), seen)
		}
	}
	close(release)
	outcome := <-done
	if outcome.err != nil {
		t.Fatalf("ReadUserUsageOrigin() error = %v", outcome.err)
	}
	if outcome.result == nil || outcome.result.Usage == nil || outcome.result.UsageErr != nil || outcome.result.QuotaErr != nil {
		t.Fatalf("unexpected origin result: %+v", outcome.result)
	}
	if len(outcome.result.APIKeys) != 1 || outcome.result.APIKeys[0].ID != 11 {
		t.Fatalf("API keys = %+v", outcome.result.APIKeys)
	}
	mu.Lock()
	defer mu.Unlock()
	if counts["login"] != 1 || counts["me"] != 1 {
		t.Fatalf("login/me count = %d/%d, want 1/1", counts["login"], counts["me"])
	}
}

func TestReadUserUsageOriginHonorsBranchSelection(t *testing.T) {
	var mu sync.Mutex
	counts := map[string]int{}
	mux := http.NewServeMux()
	registerUsageOriginLoginHandlers(mux, &mu, counts)
	registerCountingOriginHandlers(mux, &mu, counts, http.StatusOK, http.StatusOK)
	reader := newTestProvider(t, mux).(relay.UserUsageOriginReader)
	params := relay.UserUsageDashboardParams{StartDate: "2026-07-01", EndDate: "2026-07-15", Granularity: "day"}

	usage, err := reader.ReadUserUsageOrigin(context.Background(), relay.UserUsageOriginRequest{
		Login: "alice@example.com", Password: "test-password", RelayUserID: 7, Params: params,
		Branches: relay.UserUsageOriginBranches{Usage: true},
	})
	if err != nil || usage == nil || usage.Usage == nil || usage.UsageErr != nil {
		t.Fatalf("usage-only result=%+v err=%v", usage, err)
	}
	mu.Lock()
	if counts["keys"] != 0 || counts["subscriptions"] != 0 {
		t.Fatalf("usage-only quota calls = keys:%d subscriptions:%d", counts["keys"], counts["subscriptions"])
	}
	mu.Unlock()

	quota, err := reader.ReadUserUsageOrigin(context.Background(), relay.UserUsageOriginRequest{
		RelayUserID: 7,
		Params:      params,
		Branches:    relay.UserUsageOriginBranches{Quota: true},
	})
	if err != nil || quota == nil || quota.QuotaErr != nil {
		t.Fatalf("quota-only result=%+v err=%v", quota, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if counts["login"] != 1 || counts["stats"] != 1 || counts["trend"] != 1 || counts["models"] != 1 {
		t.Fatalf("unexpected usage/login counts: %+v", counts)
	}
	if counts["keys"] != 1 || counts["subscriptions"] != 1 {
		t.Fatalf("quota-only counts: %+v", counts)
	}
}

func TestReadUserUsageOriginUsesSubscriptionProgressForResetTimes(t *testing.T) {
	var mu sync.Mutex
	counts := map[string]int{}
	mux := http.NewServeMux()
	registerUsageOriginLoginHandlers(mux, &mu, counts)
	mux.HandleFunc("/api/v1/subscriptions/progress", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-user-token" {
			t.Fatalf("progress authorization = %q", r.Header.Get("Authorization"))
		}
		writeOriginEnvelope(w, []any{
			map[string]any{
				"subscription": map[string]any{
					"id": 12, "user_id": 7, "group_id": 42, "status": "active",
					"monthly_usage_usd": 25,
				},
				"progress": map[string]any{
					"daily":   map[string]any{"resets_at": "2099-07-16T00:00:00Z"},
					"weekly":  map[string]any{"resets_at": "2099-07-22T00:00:00Z"},
					"monthly": map[string]any{"resets_at": "2099-08-01T00:00:00Z"},
				},
			},
		})
	})
	mux.HandleFunc("/api/v1/admin/users/7/api-keys", func(w http.ResponseWriter, r *http.Request) {
		writeOriginEnvelope(w, []any{})
	})
	mux.HandleFunc("/api/v1/admin/users/7/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		writeOriginEnvelope(w, []any{map[string]any{
			"id": 12, "user_id": 7, "group_id": 42, "status": "active",
			"monthly_usage_usd": 25,
		}})
	})

	reader := newTestProvider(t, mux).(relay.UserUsageOriginReader)
	result, err := reader.ReadUserUsageOrigin(context.Background(), relay.UserUsageOriginRequest{
		Login: "alice@example.com", Password: "test-password", RelayUserID: 7,
		Branches: relay.UserUsageOriginBranches{Quota: true},
	})
	if err != nil || result == nil || result.QuotaErr != nil {
		t.Fatalf("origin result=%+v err=%v", result, err)
	}
	if len(result.Subscriptions) != 1 {
		t.Fatalf("subscriptions = %+v", result.Subscriptions)
	}
	subscription := result.Subscriptions[0]
	for label, item := range map[string]struct {
		got  *time.Time
		want time.Time
	}{
		"daily":   {subscription.DailyResetAt, time.Date(2099, 7, 16, 0, 0, 0, 0, time.UTC)},
		"weekly":  {subscription.WeeklyResetAt, time.Date(2099, 7, 22, 0, 0, 0, 0, time.UTC)},
		"monthly": {subscription.MonthlyResetAt, time.Date(2099, 8, 1, 0, 0, 0, 0, time.UTC)},
	} {
		if item.got == nil || !item.got.UTC().Equal(item.want) {
			t.Errorf("%s reset = %v, want %s", label, item.got, item.want)
		}
	}
}

func TestReadUserUsageOriginRetainsSubscriptionsOmittedByPartialProgress(t *testing.T) {
	var mu sync.Mutex
	counts := map[string]int{}
	mux := http.NewServeMux()
	registerUsageOriginLoginHandlers(mux, &mu, counts)
	mux.HandleFunc("/api/v1/subscriptions/progress", func(w http.ResponseWriter, r *http.Request) {
		writeOriginEnvelope(w, []any{map[string]any{
			"subscription": map[string]any{
				"id": 12, "user_id": 7, "group_id": 42, "status": "active",
				"monthly_usage_usd": 25,
			},
			"progress": map[string]any{
				"monthly": map[string]any{"resets_at": "2099-08-01T00:00:00Z"},
			},
		}})
	})
	mux.HandleFunc("/api/v1/admin/users/7/api-keys", func(w http.ResponseWriter, r *http.Request) {
		writeOriginEnvelope(w, []any{})
	})
	mux.HandleFunc("/api/v1/admin/users/7/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		writeOriginEnvelope(w, []any{
			map[string]any{"id": 12, "user_id": 7, "group_id": 42, "status": "active", "monthly_usage_usd": 25},
			map[string]any{"id": 13, "user_id": 7, "group_id": 43, "status": "active", "monthly_usage_usd": 7},
		})
	})

	reader := newTestProvider(t, mux).(relay.UserUsageOriginReader)
	result, err := reader.ReadUserUsageOrigin(context.Background(), relay.UserUsageOriginRequest{
		Login: "alice@example.com", Password: "test-password", RelayUserID: 7,
		Branches: relay.UserUsageOriginBranches{Quota: true},
	})
	if err != nil || result == nil || result.QuotaErr != nil {
		t.Fatalf("origin result=%+v err=%v", result, err)
	}
	if len(result.Subscriptions) != 2 {
		t.Fatalf("subscriptions = %+v, want omitted subscription retained", result.Subscriptions)
	}
	byID := make(map[int64]relay.UserSubscription, len(result.Subscriptions))
	for _, subscription := range result.Subscriptions {
		byID[subscription.ID] = subscription
	}
	if got := byID[13].MonthlyUsageUSD; got != 7 {
		t.Fatalf("omitted subscription monthly usage = %v, want 7", got)
	}
	if byID[12].MonthlyResetAt == nil || !byID[12].MonthlyResetAt.UTC().Equal(time.Date(2099, 8, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("progress reset = %v, want 2099-08-01T00:00:00Z", byID[12].MonthlyResetAt)
	}
}

func TestReadUserUsageOriginFallsBackWhenSubscriptionProgressFails(t *testing.T) {
	var mu sync.Mutex
	counts := map[string]int{}
	mux := http.NewServeMux()
	registerUsageOriginLoginHandlers(mux, &mu, counts)
	mux.HandleFunc("/api/v1/subscriptions/progress", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": http.StatusBadGateway, "message": "synthetic progress outage"})
	})
	mux.HandleFunc("/api/v1/admin/users/7/api-keys", func(w http.ResponseWriter, r *http.Request) {
		writeOriginEnvelope(w, []any{})
	})
	mux.HandleFunc("/api/v1/admin/users/7/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		writeOriginEnvelope(w, []any{map[string]any{
			"id": 12, "user_id": 7, "group_id": 42, "status": "active",
			"monthly_usage_usd": 25,
		}})
	})

	reader := newTestProvider(t, mux).(relay.UserUsageOriginReader)
	result, err := reader.ReadUserUsageOrigin(context.Background(), relay.UserUsageOriginRequest{
		Login: "alice@example.com", Password: "test-password", RelayUserID: 7,
		Branches: relay.UserUsageOriginBranches{Quota: true},
	})
	if err != nil || result == nil || result.QuotaErr != nil {
		t.Fatalf("origin result=%+v err=%v", result, err)
	}
	if len(result.Subscriptions) != 1 || result.Subscriptions[0].MonthlyUsageUSD != 25 {
		t.Fatalf("fallback subscriptions = %+v", result.Subscriptions)
	}
}

func TestReadUserUsageOriginSeparatesUsageAndQuotaErrors(t *testing.T) {
	t.Run("usage error preserves current quota facts", func(t *testing.T) {
		var mu sync.Mutex
		counts := map[string]int{}
		mux := http.NewServeMux()
		registerUsageOriginLoginHandlers(mux, &mu, counts)
		registerCountingOriginHandlers(mux, &mu, counts, http.StatusBadGateway, http.StatusOK)
		reader := newTestProvider(t, mux).(relay.UserUsageOriginReader)
		result, err := reader.ReadUserUsageOrigin(context.Background(), relay.UserUsageOriginRequest{
			Login: "alice@example.com", Password: "test-password", RelayUserID: 7,
			Branches: relay.UserUsageOriginBranches{Usage: true, Quota: true},
		})
		if err != nil {
			t.Fatalf("top-level error = %v", err)
		}
		if result == nil || result.Usage != nil || result.UsageErr == nil || result.QuotaErr != nil {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("quota error does not fail atomic usage", func(t *testing.T) {
		var mu sync.Mutex
		counts := map[string]int{}
		mux := http.NewServeMux()
		registerUsageOriginLoginHandlers(mux, &mu, counts)
		registerCountingOriginHandlers(mux, &mu, counts, http.StatusOK, http.StatusBadGateway)
		reader := newTestProvider(t, mux).(relay.UserUsageOriginReader)
		result, err := reader.ReadUserUsageOrigin(context.Background(), relay.UserUsageOriginRequest{
			Login: "alice@example.com", Password: "test-password", RelayUserID: 7,
			Branches: relay.UserUsageOriginBranches{Usage: true, Quota: true},
		})
		if err != nil {
			t.Fatalf("top-level error = %v", err)
		}
		if result == nil || result.Usage == nil || result.UsageErr != nil || result.QuotaErr == nil {
			t.Fatalf("unexpected result: %+v", result)
		}
	})
}

func TestReadUserUsageOriginCancellationTerminatesBranches(t *testing.T) {
	started := make(chan struct{}, 3)
	mux := http.NewServeMux()
	var mu sync.Mutex
	counts := map[string]int{}
	registerUsageOriginLoginHandlers(mux, &mu, counts)
	for _, path := range []string{
		"/api/v1/usage/dashboard/stats",
		"/api/v1/usage/dashboard/trend",
		"/api/v1/usage/dashboard/models",
	} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			started <- struct{}{}
			<-r.Context().Done()
		})
	}
	reader := newTestProvider(t, mux).(relay.UserUsageOriginReader)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		result, err := reader.ReadUserUsageOrigin(ctx, relay.UserUsageOriginRequest{
			Login: "alice@example.com", Password: "test-password", RelayUserID: 7,
			Branches: relay.UserUsageOriginBranches{Usage: true},
		})
		if err == nil && result != nil {
			err = result.UsageErr
		}
		done <- err
	}()
	for i := 0; i < 3; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("usage branches did not all start")
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("origin read did not terminate after cancellation")
	}
}

func registerUsageOriginLoginHandlers(mux *http.ServeMux, mu *sync.Mutex, counts map[string]int) {
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		counts["login"]++
		mu.Unlock()
		writeOriginEnvelope(w, map[string]any{"access_token": "test-user-token"})
	})
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		counts["me"]++
		mu.Unlock()
		writeOriginEnvelope(w, map[string]any{"id": 7, "username": "alice", "email": "alice@example.com"})
	})
}

func registerBlockingOriginHandler(mux *http.ServeMux, path, name string, started chan<- string, release <-chan struct{}, respond func(http.ResponseWriter)) {
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		started <- name
		select {
		case <-release:
			respond(w)
		case <-r.Context().Done():
		}
	})
}

func registerCountingOriginHandlers(mux *http.ServeMux, mu *sync.Mutex, counts map[string]int, statsStatus, subscriptionStatus int) {
	register := func(path, name string, status int, data any) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			counts[name]++
			mu.Unlock()
			if status != http.StatusOK {
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(map[string]any{"code": status, "message": "synthetic upstream failure"})
				return
			}
			writeOriginEnvelope(w, data)
		})
	}
	register("/api/v1/usage/dashboard/stats", "stats", statsStatus, map[string]any{"total_requests": 1})
	register("/api/v1/usage/dashboard/trend", "trend", http.StatusOK, map[string]any{"trend": []any{}})
	register("/api/v1/usage/dashboard/models", "models", http.StatusOK, map[string]any{"models": []any{}})
	register("/api/v1/admin/users/7/api-keys", "keys", http.StatusOK, map[string]any{"items": []any{}, "page": 1, "pages": 1})
	register("/api/v1/admin/users/7/subscriptions", "subscriptions", subscriptionStatus, []any{})
}

func writeOriginEnvelope(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
}
