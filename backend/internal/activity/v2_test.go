package activity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/attributionusagepoolcommit"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/testdb"
	_ "github.com/lib/pq"
)

type fixedV2Denominator struct{ value V2Denominator }

func (f fixedV2Denominator) ResolveDenominator(_ context.Context, request V2DenominatorRequest) (V2Denominator, error) {
	f.value.ProviderSet = request.ProviderSet
	return f.value, nil
}

type errorV2Denominator struct{ err error }

func (f errorV2Denominator) ResolveDenominator(context.Context, V2DenominatorRequest) (V2Denominator, error) {
	return V2Denominator{}, f.err
}

func TestV2OverviewCountsPoolsOnceAndClampsRatioToUsageAsOf(t *testing.T) {
	client, dsn := testdb.OpenWithDSN(t)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	actor := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SaveX(ctx)
	repoA := client.RepoConfig.Create().SetName("a").SetFullName("example/a").SetCloneURL("https://example.com/example/a.git").SaveX(ctx)
	repoB := client.RepoConfig.Create().SetName("b").SetFullName("example/b").SetCloneURL("https://example.com/example/b.git").SaveX(ctx)
	first := createV2Pool(t, client, actor.ID, "formal_v2", "2026-03-08T06:45:00Z", 60, 0)
	second := createV2Pool(t, client, actor.ID, "formal_v2", "2026-03-08T07:15:00Z", 40, 1)
	shadow := createV2Pool(t, client, actor.ID, "shadow_v2", "2026-03-08T06:45:00Z", 999, 0)
	client.AttributionUsagePoolCommit.Create().SetPoolID(first.ID).SetRepoConfigID(repoA.ID).SetCommitSha("one").SetRelationKind(attributionusagepoolcommit.RelationKindShared).SaveX(ctx)
	client.AttributionUsagePoolCommit.Create().SetPoolID(first.ID).SetRepoConfigID(repoB.ID).SetCommitSha("two").SetRelationKind(attributionusagepoolcommit.RelationKindShared).SaveX(ctx)
	client.AttributionUsagePoolCommit.Create().SetPoolID(second.ID).SetRepoConfigID(repoA.ID).SetCommitSha("three").SetRelationKind(attributionusagepoolcommit.RelationKindDirect).SaveX(ctx)
	client.AttributionUsagePoolCommit.Create().SetPoolID(shadow.ID).SetRepoConfigID(repoA.ID).SetCommitSha("shadow").SetRelationKind(attributionusagepoolcommit.RelationKindDirect).SaveX(ctx)
	asOf := time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC)
	service := NewService(client, nil, ServiceOptions{CursorSecret: "secret", V2LedgerEpoch: "formal_v2", V2DB: db, V2Denominator: fixedV2Denominator{V2Denominator{TotalTokens: 120, AsOf: asOf, Fresh: true, Complete: true}}})
	result, err := service.V2Overview(ctx, actor.ID, V2Query{Scope: V2ScopePersonal, FromDate: "2026-03-08", ToDate: "2026-03-08", Timezone: "America/New_York"})
	if err != nil {
		t.Fatal(err)
	}
	if result.CommittedTokens != 100 || result.Ratio.CommittedTokens != 60 || result.Ratio.State != "exact" || result.Ratio.Percent == nil || *result.Ratio.Percent != 50 {
		t.Fatalf("overview = %+v", result)
	}
	if len(result.Trend) != 1 || result.Trend[0].DirectTokens != 100 {
		t.Fatalf("trend = %+v", result.Trend)
	}
	if result.Readiness.State != "active" {
		t.Fatalf("readiness = %+v", result.Readiness)
	}
	oldProvider := client.RelayProvider.Create().SetName("old-provider").SetDisplayName("Old Provider").SetBaseURL("https://old-relay.example.com").SetAdminAPIKey("encrypted-test-key").SetDefaultModel("example-model").SetEnabled(false).SaveX(ctx)
	oldPool := client.AttributionUsagePool.Create().SetCanonicalPoolKey("old-provider-pool").SetLedgerEpoch("formal_v2").SetRelayProviderID(oldProvider.ID).SetUserID(actor.ID).SetRequestedModel("model-test").SetBucketStartUtc(time.Date(2026, 3, 8, 6, 45, 0, 0, time.UTC)).SetTotalTokens(777).SaveX(ctx)
	client.AttributionUsagePoolCommit.Create().SetPoolID(oldPool.ID).SetRepoConfigID(repoA.ID).SetCommitSha("old-provider").SetRelationKind(attributionusagepoolcommit.RelationKindDirect).SaveX(ctx)
	mismatched, err := service.V2Overview(ctx, actor.ID, V2Query{Scope: V2ScopePersonal, FromDate: "2026-03-08", ToDate: "2026-03-08", Timezone: "America/New_York"})
	if err != nil || mismatched.CommittedTokens != 877 || mismatched.Trend[0].DirectTokens != 877 || mismatched.Ratio.State != "denominator_unavailable" {
		t.Fatalf("provider-mismatched overview=%+v err=%v", mismatched, err)
	}
	client.AttributionUsagePoolCommit.Update().SetOrphaned(true).ExecX(ctx)
	orphaned, err := service.V2Overview(ctx, actor.ID, V2Query{Scope: V2ScopePersonal, FromDate: "2026-03-08", ToDate: "2026-03-08", Timezone: "America/New_York"})
	if err != nil || orphaned.Readiness.State != "active" || orphaned.CommittedTokens != 877 || orphaned.Trend[0].DirectTokens != 877 {
		t.Fatalf("orphaned readiness=%+v err=%v", orphaned, err)
	}
	currentProvider := client.RelayProvider.Query().Where(relayprovider.EnabledEQ(true)).OnlyX(ctx)
	client.RelayProvider.UpdateOne(currentProvider).SetEnabled(false).ExecX(ctx)
	withoutProvider, err := service.V2Overview(ctx, actor.ID, V2Query{Scope: V2ScopePersonal, FromDate: "2026-03-08", ToDate: "2026-03-08", Timezone: "America/New_York"})
	if err != nil || withoutProvider.CommittedTokens != 877 || withoutProvider.Readiness.State != "active" || withoutProvider.Ratio.State != "denominator_unavailable" {
		t.Fatalf("providerless overview=%+v err=%v", withoutProvider, err)
	}
	client.RelayProvider.Create().SetName("replacement-provider").SetDisplayName("Replacement Provider").SetBaseURL("https://replacement-relay.example.com").SetAdminAPIKey("encrypted-test-key").SetDefaultModel("example-model").SetEnabled(true).SaveX(ctx)
	switched, err := service.V2Overview(ctx, actor.ID, V2Query{Scope: V2ScopePersonal, FromDate: "2026-03-08", ToDate: "2026-03-08", Timezone: "America/New_York"})
	if err != nil || switched.Readiness.State != "active" {
		t.Fatalf("provider-switched readiness=%+v err=%v", switched, err)
	}
}

func TestV2RepositoryAndPRPagesKeepSharedValuesNonAdditive(t *testing.T) {
	client, dsn := testdb.OpenWithDSN(t)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	actor := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SaveX(ctx)
	var firstRepo *ent.RepoConfig
	for i := 0; i < 21; i++ {
		repo := client.RepoConfig.Create().SetName("repo").SetFullName("example/repo-" + string(rune('a'+i))).SetCloneURL("https://example.com/repo-" + string(rune('a'+i)) + ".git").SaveX(ctx)
		if i == 0 {
			firstRepo = repo
		}
		pool := createV2Pool(t, client, actor.ID, "formal_v2", "2026-08-01T00:00:00Z", int64(100-i), 0)
		client.AttributionUsagePoolCommit.Create().SetPoolID(pool.ID).SetRepoConfigID(repo.ID).SetCommitSha("sha-" + repo.Name + repo.FullName).SetRelationKind(attributionusagepoolcommit.RelationKindDirect).SaveX(ctx)
	}
	shared := createV2Pool(t, client, actor.ID, "formal_v2", "2026-08-01T01:00:00Z", 50, 0)
	client.AttributionUsagePoolCommit.Create().SetPoolID(shared.ID).SetRepoConfigID(firstRepo.ID).SetCommitSha("shared").SetRelationKind(attributionusagepoolcommit.RelationKindShared).SaveX(ctx)
	pr := client.PrRecord.Create().SetRepoConfigID(firstRepo.ID).SetScmPrID(7).SetTitle("Shared work").SetScmPrURL("https://example.com/pr/7").SaveX(ctx)
	client.PRCommitUsageSnapshot.Create().SetPrRecordID(pr.ID).SetCommitSha("shared").SaveX(ctx)
	secondPR := client.PrRecord.Create().SetRepoConfigID(firstRepo.ID).SetScmPrID(8).SetTitle("Also shared").SetScmPrURL("https://example.com/pr/8").SaveX(ctx)
	client.PRCommitUsageSnapshot.Create().SetPrRecordID(secondPR.ID).SetCommitSha("shared").SaveX(ctx)
	service := NewService(client, nil, ServiceOptions{CursorSecret: "secret", V2LedgerEpoch: "formal_v2", V2DB: db})
	query := V2PageQuery{V2Query: V2Query{Scope: V2ScopePersonal, FromDate: "2026-08-01", ToDate: "2026-08-01", Timezone: "Asia/Kathmandu"}}
	page, err := service.V2Repositories(ctx, actor.ID, query)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 20 || page.NextCursor == "" {
		t.Fatalf("first page = %+v", page)
	}
	newRepo := client.RepoConfig.Create().SetName("new").SetFullName("example/repo-new").SetCloneURL("https://example.com/repo-new.git").SaveX(ctx)
	newPool := createV2Pool(t, client, actor.ID, "formal_v2", "2026-08-01T02:00:00Z", 1000, 0)
	client.AttributionUsagePoolCommit.Create().SetPoolID(newPool.ID).SetRepoConfigID(newRepo.ID).SetCommitSha("new").SetRelationKind(attributionusagepoolcommit.RelationKindDirect).SaveX(ctx)
	query.Cursor = page.NextCursor
	next, err := service.V2Repositories(ctx, actor.ID, query)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Items) != 1 {
		t.Fatalf("next page = %+v", next)
	}
	mismatched := query
	mismatched.Search = "repo"
	if _, err := service.V2Repositories(ctx, actor.ID, mismatched); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("cursor query isolation error=%v", err)
	}
	provider := client.RelayProvider.Query().OnlyX(ctx)
	client.RelayProvider.UpdateOne(provider).AddConfigurationVersion(1).ExecX(ctx)
	if _, err := service.V2Repositories(ctx, actor.ID, query); !errors.Is(err, ErrSnapshotExpired) {
		t.Fatalf("provider-version cursor error=%v", err)
	}
	query.Cursor = ""
	prs, err := service.V2PullRequests(ctx, actor.ID, V2PageQuery{V2Query: query.V2Query})
	if err != nil {
		t.Fatal(err)
	}
	if len(prs.Items) != 2 || prs.Items[0].InvolvedTokens != 50 || prs.Items[1].InvolvedTokens != 50 || prs.Items[0].OverlapState != "shared" {
		t.Fatalf("PR page = %+v", prs)
	}
	namePage, err := service.V2Repositories(ctx, actor.ID, V2PageQuery{V2Query: query.V2Query, Sort: "name", Search: "repo-a"})
	if err != nil || len(namePage.Items) != 1 || namePage.Items[0].RepoConfigID != firstRepo.ID {
		t.Fatalf("name search page=%+v err=%v", namePage, err)
	}
	literalWildcard, err := service.V2Repositories(ctx, actor.ID, V2PageQuery{V2Query: query.V2Query, Search: "%"})
	if err != nil || len(literalWildcard.Items) != 0 {
		t.Fatalf("literal wildcard page=%+v err=%v", literalWildcard, err)
	}
	repoOverview, err := service.V2Overview(ctx, actor.ID, V2Query{Scope: V2ScopePersonal, FromDate: "2026-08-01", ToDate: "2026-08-01", Timezone: "Asia/Kathmandu", RepoID: firstRepo.ID})
	if err != nil || repoOverview.CommittedTokens != 2940 || repoOverview.Trend[0].DirectTokens != 100 || repoOverview.Trend[0].SharedTokens != 50 || repoOverview.SCMCoverage.Complete || repoOverview.SCMCoverage.UnsyncedRepositories != 1 {
		t.Fatalf("repository trend=%+v err=%v", repoOverview, err)
	}
	prOverview, err := service.V2Overview(ctx, actor.ID, V2Query{Scope: V2ScopePersonal, FromDate: "2026-08-01", ToDate: "2026-08-01", Timezone: "Asia/Kathmandu", PRRecordID: pr.ID})
	if err != nil || prOverview.CommittedTokens != 2940 || prOverview.Trend[0].InvolvedTokens != 50 {
		t.Fatalf("PR trend=%+v err=%v", prOverview, err)
	}
	emptyRepo := client.RepoConfig.Create().SetName("empty").SetFullName("example/empty").SetCloneURL("https://example.com/empty.git").SaveX(ctx)
	historical := createV2Pool(t, client, actor.ID, "formal_v2", "2026-07-01T00:00:00Z", 1, 0)
	client.AttributionUsagePoolCommit.Create().SetPoolID(historical.ID).SetRepoConfigID(emptyRepo.ID).SetCommitSha("historical").SetRelationKind(attributionusagepoolcommit.RelationKindDirect).SaveX(ctx)
	emptyOverview, err := service.V2Overview(ctx, actor.ID, V2Query{Scope: V2ScopePersonal, FromDate: "2026-08-01", ToDate: "2026-08-01", Timezone: "Asia/Kathmandu", RepoID: emptyRepo.ID})
	if err != nil || emptyOverview.SCMCoverage.Complete || emptyOverview.SCMCoverage.UnsyncedRepositories != 1 {
		t.Fatalf("empty repository coverage=%+v err=%v", emptyOverview.SCMCoverage, err)
	}
	foreignRepo := client.RepoConfig.Create().SetName("foreign").SetFullName("example/foreign").SetCloneURL("https://example.com/foreign.git").SaveX(ctx)
	client.AttributionUsagePoolCommit.Create().SetPoolID(shared.ID).SetRepoConfigID(foreignRepo.ID).SetCommitSha("inherited-only").SetRelationKind(attributionusagepoolcommit.RelationKindInheritedNonCounting).SaveX(ctx)
	foreignOverview, err := service.V2Overview(ctx, actor.ID, V2Query{Scope: V2ScopePersonal, FromDate: "2026-08-01", ToDate: "2026-08-01", Timezone: "Asia/Kathmandu", RepoID: foreignRepo.ID})
	if err != nil || !foreignOverview.SCMCoverage.Complete || foreignOverview.SCMCoverage.UnsyncedRepositories != 0 {
		t.Fatalf("foreign repository leaked coverage=%+v err=%v", foreignOverview.SCMCoverage, err)
	}
	missingPR, err := service.V2Overview(ctx, actor.ID, V2Query{Scope: V2ScopePersonal, FromDate: "2026-08-01", ToDate: "2026-08-01", Timezone: "Asia/Kathmandu", PRRecordID: 999999})
	if err != nil || !missingPR.SCMCoverage.Complete || missingPR.SCMCoverage.UnsyncedRepositories != 0 {
		t.Fatalf("missing PR leaked coverage=%+v err=%v", missingPR.SCMCoverage, err)
	}
}

func TestV2OverviewKeepsActivityWhenDenominatorErrors(t *testing.T) {
	client, dsn := testdb.OpenWithDSN(t)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	actor := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SaveX(ctx)
	client.RelayProvider.Create().SetName("activity-test").SetDisplayName("Activity Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("encrypted-test-key").SetDefaultModel("example-model").SetIsPrimary(true).SetEnabled(true).SaveX(ctx)
	service := NewService(client, nil, ServiceOptions{V2LedgerEpoch: "formal_v2", V2DB: db, V2Denominator: errorV2Denominator{err: fmt.Errorf("Usage failed")}})
	result, err := service.V2Overview(ctx, actor.ID, V2Query{Scope: V2ScopePersonal, FromDate: "2026-08-01", ToDate: "2026-08-01", Timezone: "UTC"})
	if err != nil || result.Ratio.State != "denominator_unavailable" || !result.Ratio.Retryable || len(result.Trend) != 1 {
		t.Fatalf("overview=%+v error=%v", result, err)
	}
}

func TestV2ProductDTOContainsNoRequestDetail(t *testing.T) {
	payload, _ := json.Marshal(V2Overview{})
	text := strings.ToLower(string(payload))
	for _, forbidden := range []string{"request_id", "api_key", "calibration", "gap_request"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("DTO leaked %q: %s", forbidden, text)
		}
	}
}

func TestV2RatioStates(t *testing.T) {
	now := time.Now().UTC()
	hundred := int64(100)
	tests := []struct {
		name        string
		committed   int64
		coverage    V2Coverage
		denominator V2Denominator
		state       string
		total       *int64
	}{
		{"unavailable", 10, V2Coverage{Complete: true}, V2Denominator{}, "denominator_unavailable", nil},
		{"zero usage", 0, V2Coverage{Complete: true}, V2Denominator{Fresh: true, Complete: true, AsOf: now}, "complete_zero_usage", new(int64)},
		{"true zero", 0, V2Coverage{Complete: true}, V2Denominator{TotalTokens: 100, Fresh: true, Complete: true, AsOf: now}, "true_zero_committed", &hundred},
		{"lower bound", 20, V2Coverage{LowerBound: true}, V2Denominator{TotalTokens: 100, Fresh: true, Complete: true, AsOf: now}, "lower_bound", &hundred},
		{"exact", 20, V2Coverage{Complete: true}, V2Denominator{TotalTokens: 100, Fresh: true, Complete: true, AsOf: now}, "exact", &hundred},
		{"contradictory", 101, V2Coverage{Complete: true}, V2Denominator{TotalTokens: 100, Fresh: true, Complete: true, AsOf: now}, "denominator_unavailable", nil},
		{"incomplete zero", 0, V2Coverage{LowerBound: true}, V2Denominator{TotalTokens: 100, Fresh: true, Complete: true, AsOf: now}, "lower_bound", &hundred},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := v2Ratio(tt.committed, tt.coverage, tt.denominator)
			if got.State != tt.state || ((got.TotalTokens == nil) != (tt.total == nil)) {
				t.Fatalf("ratio=%+v", got)
			}
		})
	}
}

func TestPreviousV2WindowUsesLocalCalendarDays(t *testing.T) {
	tests := []struct {
		name, timezone, from, to, wantFromDate, wantToDate string
		wantFromUTC, wantToUTC                             time.Time
	}{
		{"UTC", "UTC", "2026-08-01", "2026-08-07", "2026-07-25", "2026-07-31", time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)},
		{"Shanghai", "Asia/Shanghai", "2026-08-01", "2026-08-07", "2026-07-25", "2026-07-31", time.Date(2026, 7, 24, 16, 0, 0, 0, time.UTC), time.Date(2026, 7, 31, 16, 0, 0, 0, time.UTC)},
		{"DST", "America/New_York", "2026-03-08", "2026-03-14", "2026-03-01", "2026-03-07", time.Date(2026, 3, 1, 5, 0, 0, 0, time.UTC), time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			location, err := time.LoadLocation(tt.timezone)
			if err != nil {
				t.Fatal(err)
			}
			previous, from, to := previousV2Window(V2Query{FromDate: tt.from, ToDate: tt.to, Timezone: tt.timezone}, location)
			if previous.FromDate != tt.wantFromDate || previous.ToDate != tt.wantToDate || !from.Equal(tt.wantFromUTC) || !to.Equal(tt.wantToUTC) {
				t.Fatalf("previous=%+v bounds=[%s,%s), want dates %s..%s bounds [%s,%s)", previous, from, to, tt.wantFromDate, tt.wantToDate, tt.wantFromUTC, tt.wantToUTC)
			}
		})
	}
}

func TestV2OverviewReturnsExactAdjacentPercentagePointChange(t *testing.T) {
	client, dsn := testdb.OpenWithDSN(t)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	actor := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SaveX(ctx)
	repo := client.RepoConfig.Create().SetName("repo").SetFullName("example/repo").SetCloneURL("https://example.com/example/repo.git").SaveX(ctx)
	seed := createV2Pool(t, client, actor.ID, "formal_v2", "2026-08-06T00:00:00Z", 1, 0)
	previous := createV2Pool(t, client, actor.ID, "formal_v2", "2026-08-07T00:00:00Z", 50, 0)
	current := createV2Pool(t, client, actor.ID, "formal_v2", "2026-08-08T00:00:00Z", 100, 0)
	for index, pool := range []*ent.AttributionUsagePool{seed, previous, current} {
		client.AttributionUsagePoolCommit.Create().SetPoolID(pool.ID).SetRepoConfigID(repo.ID).SetCommitSha(fmt.Sprintf("sha-%d", index)).SetRelationKind(attributionusagepoolcommit.RelationKindDirect).SaveX(ctx)
	}
	service := NewService(client, nil, ServiceOptions{
		V2LedgerEpoch: "formal_v2",
		V2DB:          db,
		V2Denominator: fixedV2Denominator{V2Denominator{TotalTokens: 200, AsOf: time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC), Fresh: true, Complete: true}},
	})
	result, err := service.V2Overview(ctx, actor.ID, V2Query{Scope: V2ScopePersonal, FromDate: "2026-08-08", ToDate: "2026-08-08", Timezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Ratio.PercentagePointChange == nil || *result.Ratio.PercentagePointChange != 25 {
		t.Fatalf("ratio=%+v, want +25 percentage points", result.Ratio)
	}
}

func TestV2ScopeAuthorizationIsRevalidatedForMemberAndTeam(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	makeUser := func(name string) *ent.User {
		return client.User.Create().SetUsername(name).SetEmail(name + "@example.com").SetAuthSource("ldap").SaveX(ctx)
	}
	representative, member, ordinary, outside, admin := makeUser("representative"), makeUser("member"), makeUser("ordinary"), makeUser("outside"), makeUser("admin")
	client.User.UpdateOne(admin).SetRole(entuser.RoleAdmin).ExecX(ctx)
	createActivityDirectoryScope(t, client, representative, member, ordinary, outside, admin)
	service := NewService(client, nil, ServiceOptions{CursorSecret: "secret"})
	base := V2Query{FromDate: "2026-08-01", ToDate: "2026-08-07", Timezone: "Asia/Shanghai"}
	allowed := base
	allowed.Scope = V2ScopeMember
	allowed.SubjectID = member.ID
	if _, err := service.V2Overview(ctx, representative.ID, allowed); err != nil {
		t.Fatalf("authorized member: %v", err)
	}
	denied := allowed
	denied.SubjectID = outside.ID
	if _, err := service.V2Overview(ctx, representative.ID, denied); err != ErrForbidden {
		t.Fatalf("outside member error=%v", err)
	}
	team := base
	team.Scope = V2ScopeTeam
	team.TeamID = "team-child"
	if _, err := service.V2Overview(ctx, representative.ID, team); err != nil {
		t.Fatalf("authorized team: %v", err)
	}
}

func TestV2MemberDenominatorCacheIsAuthorizationAndProviderIsolated(t *testing.T) {
	cache, err := NewCache(&activityMemoryStore{values: map[string][]byte{}}, CacheOptions{Namespace: "activity-v2-test"})
	if err != nil {
		t.Fatal(err)
	}
	key := V2MemberDenominatorCacheKey{ActorUserID: 1, SubjectUserID: 2, ScopeVersion: "scope-a", ProviderID: 3, ProviderVersion: 4, BindingVersion: 5, FromDate: "2026-08-01", ToDate: "2026-08-07", Timezone: "UTC"}
	want := V2Denominator{TotalTokens: 99, Complete: true}
	cache.WriteMemberDenominator(context.Background(), key, want)
	var got V2Denominator
	if !cache.ReadMemberDenominator(context.Background(), key, &got) || got.TotalTokens != 99 {
		t.Fatalf("cached denominator=%+v", got)
	}
	otherActor := key
	otherActor.ActorUserID = 9
	if cache.ReadMemberDenominator(context.Background(), otherActor, &got) {
		t.Fatal("member denominator crossed actor authorization")
	}
	otherProvider := key
	otherProvider.ProviderVersion++
	if cache.ReadMemberDenominator(context.Background(), otherProvider, &got) {
		t.Fatal("member denominator crossed provider version")
	}
}

func createV2Pool(t *testing.T, client *ent.Client, userID int, epoch, at string, tokens int64, gaps int) *ent.AttributionUsagePool {
	t.Helper()
	providers := client.RelayProvider.Query().AllX(context.Background())
	if len(providers) == 0 {
		providers = append(providers, client.RelayProvider.Create().SetName("activity-test").SetDisplayName("Activity Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("encrypted-test-key").SetDefaultModel("example-model").SetIsPrimary(true).SetEnabled(true).SaveX(context.Background()))
	}
	when, err := time.Parse(time.RFC3339, at)
	if err != nil {
		t.Fatal(err)
	}
	return client.AttributionUsagePool.Create().SetCanonicalPoolKey(fmt.Sprintf("%s:%s:%d:%d", epoch, at, userID, tokens)).SetLedgerEpoch(epoch).SetRelayProviderID(providers[0].ID).SetUserID(userID).SetRequestedModel("model-test").SetBucketStartUtc(when).SetTotalTokens(tokens).SetCoverageGapCount(gaps).SaveX(context.Background())
}
