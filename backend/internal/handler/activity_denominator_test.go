package handler

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/activity"
	"github.com/ai-efficiency/backend/internal/personalusage"
	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/teamusage"
	"github.com/ai-efficiency/backend/internal/testdb"
	_ "github.com/lib/pq"
)

type fakePersonalMetrics struct {
	snapshot *personalusage.Snapshot
	request  personalusage.Request
	calls    int
}

func (f *fakePersonalMetrics) Dashboard(_ context.Context, request personalusage.Request) (*personalusage.Snapshot, error) {
	f.request = request
	f.calls++
	return f.snapshot, nil
}

type denominatorRecordedQuery struct {
	SQL  string
	Args []any
}

type denominatorRecordingDriver struct {
	dialect.Driver
	mu      sync.Mutex
	queries []denominatorRecordedQuery
}

func (d *denominatorRecordingDriver) Query(ctx context.Context, statement string, args, target any) error {
	recorded := denominatorRecordedQuery{SQL: statement}
	if values, ok := args.([]any); ok {
		recorded.Args = append([]any(nil), values...)
	}
	d.mu.Lock()
	d.queries = append(d.queries, recorded)
	d.mu.Unlock()
	return d.Driver.Query(ctx, statement, args, target)
}

func (d *denominatorRecordingDriver) snapshot() []denominatorRecordedQuery {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]denominatorRecordedQuery, len(d.queries))
	copy(result, d.queries)
	return result
}

type denominatorMemoryStore struct {
	mu     sync.Mutex
	values map[string][]byte
}

func (s *denominatorMemoryStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	if !ok {
		return nil, readcache.ErrMiss
	}
	return append([]byte(nil), value...), nil
}
func (s *denominatorMemoryStore) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = append([]byte(nil), value...)
	return nil
}
func (*denominatorMemoryStore) TryAcquireLease(context.Context, string, string, time.Duration) (bool, error) {
	return true, nil
}
func (*denominatorMemoryStore) LeaseTTL(context.Context, string) (time.Duration, error) {
	return 0, nil
}
func (*denominatorMemoryStore) ReleaseLease(context.Context, string, string) (bool, error) {
	return true, nil
}

type fakeTeamMetrics struct {
	response *teamusage.DepartmentSummaryResponse
}

func (f *fakeTeamMetrics) DepartmentSummary(context.Context, int, string, teamusage.OverviewParams) (*teamusage.DepartmentSummaryResponse, error) {
	return f.response, nil
}

func TestActivityDenominatorUsesExactFreshPersonalUsage(t *testing.T) {
	now := time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC)
	usage := &fakePersonalMetrics{snapshot: &personalusage.Snapshot{Configured: true, Range: relay.UserUsageDashboardRange{StartDate: "2026-08-01", EndDate: "2026-08-07", Granularity: "day", Timezone: "Asia/Shanghai"}, Stats: &relay.UserUsageDashboardStats{TotalTokens: 900}, UsageFreshness: &personalusage.UsageFreshness{AsOf: now.Add(-time.Second), FreshUntil: now.Add(time.Minute), SourceStatus: "ok"}}}
	resolver := &activityDenominatorResolver{personal: usage, now: func() time.Time { return now }}
	result, err := resolver.ResolveDenominator(context.Background(), activity.V2DenominatorRequest{ActorUserID: 1, Scope: activity.V2ScopeMember, SubjectUserID: 2, FromDate: "2026-08-01", ToDate: "2026-08-07", Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalTokens != 900 || !result.Fresh || !result.Complete || usage.request.UserID != 2 || usage.request.IncludeGroupQuotas {
		t.Fatalf("result/request = %+v/%+v", result, usage.request)
	}
}

func TestActivityDenominatorRejectsStaleOrScopeMismatchedUsage(t *testing.T) {
	now := time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC)
	personal := &fakePersonalMetrics{snapshot: &personalusage.Snapshot{Configured: true, Range: relay.UserUsageDashboardRange{StartDate: "2026-08-01", EndDate: "2026-08-07", Granularity: "day", Timezone: "UTC"}, Stats: &relay.UserUsageDashboardStats{TotalTokens: 9}, UsageFreshness: &personalusage.UsageFreshness{AsOf: now.Add(-time.Hour), FreshUntil: now.Add(-time.Second), SourceStatus: "ok"}}}
	resolver := &activityDenominatorResolver{personal: personal, now: func() time.Time { return now }}
	result, err := resolver.ResolveDenominator(context.Background(), activity.V2DenominatorRequest{ActorUserID: 1, Scope: activity.V2ScopePersonal, FromDate: "2026-08-01", ToDate: "2026-08-07", Timezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fresh || !result.Complete {
		t.Fatalf("stale denominator = %+v", result)
	}
	tokens := int64(10)
	team := &fakeTeamMetrics{response: &teamusage.DepartmentSummaryResponse{ScopeVersion: "old", SnapshotFreshness: teamusage.SnapshotFreshness{AsOf: now, FreshUntil: now.Add(time.Minute), SourceStatus: "ok"}, RangeTotalTokens: &tokens}}
	resolver = &activityDenominatorResolver{team: team, now: func() time.Time { return now }}
	result, err = resolver.ResolveDenominator(context.Background(), activity.V2DenominatorRequest{ActorUserID: 1, Scope: activity.V2ScopeTeam, ScopeVersion: "new"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Complete {
		t.Fatalf("scope-mismatched denominator = %+v", result)
	}
}

func TestActivityDenominatorFailsClosedForUncoveredProviderSet(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	for index := 0; index < 2; index++ {
		client.RelayProvider.Create().SetName("provider-" + string(rune('a'+index))).SetDisplayName("Provider").SetBaseURL("https://relay.example.com").SetAdminAPIKey("encrypted-test-key").SetDefaultModel("example-model").SetIsPrimary(index == 0).SetEnabled(true).SaveX(ctx)
	}
	resolver := &activityDenominatorResolver{client: client, personal: &fakePersonalMetrics{snapshot: &personalusage.Snapshot{Configured: true}}}
	result, err := resolver.ResolveDenominator(ctx, activity.V2DenominatorRequest{Scope: activity.V2ScopePersonal})
	if err != nil {
		t.Fatal(err)
	}
	if result.Complete || result.Fresh || result.TotalTokens != 0 {
		t.Fatalf("multi-provider denominator=%+v", result)
	}
}

func TestActivityDenominatorResolverStaysWithinScaleBudgets(t *testing.T) {
	const (
		disabledProviderCount = 2500
		maxLatency            = 2 * time.Second
		maxQueries            = 4
		maxScannedRows        = disabledProviderCount + 1
	)
	seed, dsn := testdb.OpenWithDSN(t)
	ctx := context.Background()
	actor := seed.User.Create().SetUsername("denominator-actor").SetEmail("denominator-actor@example.com").SetAuthSource("ldap").SaveX(ctx)
	subject := seed.User.Create().SetUsername("denominator-subject").SetEmail("denominator-subject@example.com").SetAuthSource("ldap").SaveX(ctx)
	provider := seed.RelayProvider.Create().SetName("denominator-enabled").SetDisplayName("Enabled Provider").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-key").SetEnabled(true).SaveX(ctx)
	builders := make([]*ent.RelayProviderCreate, 0, disabledProviderCount)
	for index := 0; index < disabledProviderCount; index++ {
		builders = append(builders, seed.RelayProvider.Create().SetName(fmt.Sprintf("denominator-disabled-%04d", index)).SetDisplayName("Disabled Provider").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-key").SetEnabled(false))
	}
	if _, err := seed.RelayProvider.CreateBulk(builders...).Save(ctx); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "ANALYZE relay_providers; ANALYZE users"); err != nil {
		t.Fatal(err)
	}
	driver := &denominatorRecordingDriver{Driver: entsql.OpenDB(dialect.Postgres, db)}
	client := ent.NewClient(ent.Driver(driver))
	t.Cleanup(func() { _ = client.Close() })
	cache, err := activity.NewCache(&denominatorMemoryStore{values: map[string][]byte{}}, activity.CacheOptions{Namespace: "denominator-scale"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC)
	usage := &fakePersonalMetrics{snapshot: &personalusage.Snapshot{
		Configured: true,
		Range:      relay.UserUsageDashboardRange{StartDate: "2026-08-01", EndDate: "2026-08-07", Granularity: "day", Timezone: "UTC"},
		Stats:      &relay.UserUsageDashboardStats{TotalTokens: 900},
		UsageFreshness: &personalusage.UsageFreshness{
			AsOf: now.Add(-time.Second), FreshUntil: now.Add(time.Minute), SourceStatus: "ok",
		},
	}}
	resolver := &activityDenominatorResolver{client: client, personal: usage, cache: cache, now: func() time.Time { return now }}
	request := activity.V2DenominatorRequest{ActorUserID: actor.ID, Scope: activity.V2ScopeMember, SubjectUserID: subject.ID, ScopeVersion: "scope-v1", ProviderSet: fmt.Sprintf("%d:%d", provider.ID, provider.ConfigurationVersion), FromDate: "2026-08-01", ToDate: "2026-08-07", Timezone: "UTC"}
	started := time.Now()
	for attempt := 0; attempt < 2; attempt++ {
		result, err := resolver.ResolveDenominator(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if result.TotalTokens != 900 || !result.Fresh || !result.Complete {
			t.Fatalf("denominator = %+v", result)
		}
	}
	if elapsed := time.Since(started); elapsed > maxLatency {
		t.Fatalf("two denominator resolutions took %s, exceeds %s budget", elapsed, maxLatency)
	}
	if usage.calls != 1 {
		t.Fatalf("personal Usage reads = %d, want 1 after member cache hit", usage.calls)
	}
	queries := driver.snapshot()
	if len(queries) > maxQueries {
		t.Fatalf("denominator queries = %d, exceeds %d-query budget", len(queries), maxQueries)
	}
	for _, query := range queries {
		var raw []byte
		if err := db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query.SQL, query.Args...).Scan(&raw); err != nil {
			t.Fatalf("explain denominator query: %v\nSQL: %s", err, query.SQL)
		}
		scanned, err := testdb.ExplainScannedRows(raw)
		if err != nil {
			t.Fatalf("decode denominator explain: %v", err)
		}
		if scanned > maxScannedRows {
			t.Fatalf("denominator query scanned %d rows, exceeds %d-row budget\nSQL: %s", scanned, maxScannedRows, query.SQL)
		}
	}
}
