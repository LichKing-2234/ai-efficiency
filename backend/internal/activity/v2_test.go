package activity

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/attributionusagepoolcommit"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/testdb"
	_ "github.com/lib/pq"
)

type fixedV2Denominator struct{ value V2Denominator }

func (f fixedV2Denominator) ResolveDenominator(context.Context, V2DenominatorRequest) (V2Denominator, error) {
	return f.value, nil
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
	if _, err := service.V2Repositories(ctx, actor.ID, mismatched); err != ErrInvalidCursor {
		t.Fatalf("cursor query isolation error=%v", err)
	}
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
	if err != nil || repoOverview.Trend[0].DirectTokens != 100 || repoOverview.Trend[0].SharedTokens != 50 {
		t.Fatalf("repository trend=%+v err=%v", repoOverview, err)
	}
	prOverview, err := service.V2Overview(ctx, actor.ID, V2Query{Scope: V2ScopePersonal, FromDate: "2026-08-01", ToDate: "2026-08-01", Timezone: "Asia/Kathmandu", PRRecordID: pr.ID})
	if err != nil || prOverview.Trend[0].InvolvedTokens != 50 {
		t.Fatalf("PR trend=%+v err=%v", prOverview, err)
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

func TestV2ScopeAuthorizationIsRevalidatedForMemberAndTeam(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	makeUser := func(name string) *ent.User {
		return client.User.Create().SetUsername(name).SetEmail(name + "@example.com").SetAuthSource("ldap").SaveX(ctx)
	}
	representative, member, ordinary, outside, admin := makeUser("representative"), makeUser("member"), makeUser("ordinary"), makeUser("outside"), makeUser("admin")
	client.User.UpdateOne(admin).SetRole(entuser.RoleAdmin).ExecX(ctx)
	createActivityDirectoryScope(t, client, representative, member, ordinary, outside, admin)
	service := NewService(client, nil, ServiceOptions{CursorSecret: "secret", V2LedgerEpoch: "formal_v2"})
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
	when, err := time.Parse(time.RFC3339, at)
	if err != nil {
		t.Fatal(err)
	}
	return client.AttributionUsagePool.Create().SetCanonicalPoolKey(fmt.Sprintf("%s:%s:%d:%d", epoch, at, userID, tokens)).SetLedgerEpoch(epoch).SetUserID(userID).SetRequestedModel("model-test").SetBucketStartUtc(when).SetTotalTokens(tokens).SetCoverageGapCount(gaps).SaveX(context.Background())
}
