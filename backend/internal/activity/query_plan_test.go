package activity

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/attributionusagepoolcommit"
	"github.com/ai-efficiency/backend/internal/testdb"
	_ "github.com/lib/pq"
)

type recordedV2Query struct {
	SQL  string
	Args []any
}

type recordingV2DB struct {
	*sql.DB
	mu      sync.Mutex
	queries []recordedV2Query
}

func (db *recordingV2DB) QueryContext(ctx context.Context, statement string, args ...any) (*sql.Rows, error) {
	db.record(statement, args)
	return db.DB.QueryContext(ctx, statement, args...)
}

func (db *recordingV2DB) QueryRowContext(ctx context.Context, statement string, args ...any) *sql.Row {
	db.record(statement, args)
	return db.DB.QueryRowContext(ctx, statement, args...)
}

func (db *recordingV2DB) record(statement string, args []any) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.queries = append(db.queries, recordedV2Query{SQL: statement, Args: append([]any(nil), args...)})
}

func (db *recordingV2DB) reset() {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.queries = nil
}

func (db *recordingV2DB) snapshot() []recordedV2Query {
	db.mu.Lock()
	defer db.mu.Unlock()
	result := make([]recordedV2Query, len(db.queries))
	copy(result, db.queries)
	return result
}

func explainV2Query(t *testing.T, db *sql.DB, query recordedV2Query) int64 {
	t.Helper()
	var raw []byte
	if err := db.QueryRowContext(context.Background(), "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query.SQL, query.Args...).Scan(&raw); err != nil {
		t.Fatalf("explain v2 query: %v\nSQL: %s\nargs: %v", err, query.SQL, query.Args)
	}
	rows, err := testdb.ExplainScannedRows(raw)
	if err != nil {
		t.Fatalf("decode v2 explain: %v\n%s", err, raw)
	}
	return rows
}

func TestCommitSHAProjectionUsesDedicatedLookupIndex(t *testing.T) {
	client, dsn := testdb.OpenWithDSN(t)
	ctx := context.Background()
	repo := client.RepoConfig.Create().SetName("repo-plan").SetFullName("acme/repo-plan").SetCloneURL("https://example.com/acme/repo-plan.git").SaveX(ctx)
	pr := client.PrRecord.Create().SetRepoConfigID(repo.ID).SetScmPrID(9001).SetTitle("Query plan PR").SaveX(ctx)
	builders := make([]*ent.PRCommitUsageSnapshotCreate, 0, 2000)
	for index := 0; index < 2000; index++ {
		builders = append(builders, client.PRCommitUsageSnapshot.Create().SetPrRecordID(pr.ID).SetCommitSha(fmt.Sprintf("sha-%04d", index)))
	}
	if _, err := client.PRCommitUsageSnapshot.CreateBulk(builders...).Save(ctx); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "ANALYZE pr_commit_usage_snapshots"); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(ctx, "EXPLAIN (COSTS OFF) SELECT pr_record_id, commit_sha FROM pr_commit_usage_snapshots WHERE commit_sha = $1", "sha-1999")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	plan := strings.Join(lines, "\n")
	if strings.Contains(plan, "Seq Scan on pr_commit_usage_snapshots") || !strings.Contains(plan, "Index") {
		t.Fatalf("commit SHA lookup plan did not use an index:\n%s", plan)
	}
}

func TestV2RepositoryPageUsesPoolRangeAndCommitLookupIndexes(t *testing.T) {
	client, dsn := testdb.OpenWithDSN(t)
	ctx := context.Background()
	user := client.User.Create().SetUsername("plan-user").SetEmail("plan-user@example.com").SetAuthSource("ldap").SaveX(ctx)
	provider := client.RelayProvider.Create().SetName("plan-provider").SetDisplayName("Plan Provider").SetBaseURL("https://relay.example.com").SetAdminAPIKey("encrypted-test-key").SetDefaultModel("example-model").SetIsPrimary(true).SetEnabled(true).SaveX(ctx)
	repo := client.RepoConfig.Create().SetName("plan").SetFullName("example/plan").SetCloneURL("https://example.com/example/plan.git").SaveX(ctx)
	poolBuilders := make([]*ent.AttributionUsagePoolCreate, 0, 2000)
	for index := 0; index < 2000; index++ {
		epoch := "other"
		if index == 1999 {
			epoch = "formal_v2"
		}
		poolBuilders = append(poolBuilders, client.AttributionUsagePool.Create().SetCanonicalPoolKey(fmt.Sprintf("plan-%04d", index)).SetLedgerEpoch(epoch).SetRelayProviderID(provider.ID).SetUserID(user.ID).SetRequestedModel("model-test").SetBucketStartUtc(time.Date(2026, 8, 1, 0, 0, index, 0, time.UTC)).SetTotalTokens(10))
	}
	pools, err := client.AttributionUsagePool.CreateBulk(poolBuilders...).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	commitBuilders := make([]*ent.AttributionUsagePoolCommitCreate, 0, len(pools))
	for index, pool := range pools {
		commitBuilders = append(commitBuilders, client.AttributionUsagePoolCommit.Create().SetPoolID(pool.ID).SetRepoConfigID(repo.ID).SetCommitSha(fmt.Sprintf("plan-sha-%04d", index)).SetRelationKind(attributionusagepoolcommit.RelationKindDirect))
	}
	if _, err := client.AttributionUsagePoolCommit.CreateBulk(commitBuilders...).Save(ctx); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "ANALYZE attribution_usage_pools; ANALYZE attribution_usage_pool_commits"); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(ctx, `EXPLAIN (COSTS OFF) SELECT c.repo_config_id,p.id FROM attribution_usage_pools p JOIN attribution_usage_pool_commits c ON c.pool_id=p.id WHERE p.ledger_epoch='formal_v2' AND p.relay_provider_id>0 AND p.user_id=$1 AND p.bucket_start_utc >= $2 AND p.bucket_start_utc < $3`, user.ID, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	plan := strings.Join(lines, "\n")
	if strings.Contains(plan, "Seq Scan on attribution_usage_pools") || (!strings.Contains(plan, "attributionusagepool_ledger_epoch_user_id_bucket_start_utc") && !strings.Contains(plan, "attributionusagepool_ledger_epoch_bucket_start_utc")) {
		t.Fatalf("v2 pool plan did not use range index:\n%s", plan)
	}
	if strings.Contains(plan, "Seq Scan on attribution_usage_pool_commits") || !strings.Contains(plan, "attributionusagepoolcommit_pool_id_repo_config_id_commit_sha") {
		t.Fatalf("v2 commit plan did not use pool lookup index:\n%s", plan)
	}
}

func TestV2ReadPathsStayWithinScaleLatencyBudget(t *testing.T) {
	const (
		poolCount       = 2500
		repositoryCount = 25
		maxReadLatency  = 2 * time.Second
		maxScannedRows  = 30000
	)
	client, dsn := testdb.OpenWithDSN(t)
	ctx := context.Background()
	user := client.User.Create().SetUsername("scale-v2-user").SetEmail("scale-v2-user@example.com").SetAuthSource("ldap").SaveX(ctx)
	provider := client.RelayProvider.Create().SetName("scale-v2-provider").SetDisplayName("Scale V2 Provider").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-key").SetEnabled(true).SaveX(ctx)
	repositories := make([]*ent.RepoConfig, 0, repositoryCount)
	pullRequests := make([]*ent.PrRecord, 0, repositoryCount)
	for index := 0; index < repositoryCount; index++ {
		repo := client.RepoConfig.Create().SetName(fmt.Sprintf("repo-%02d", index)).SetFullName(fmt.Sprintf("example/repo-%02d", index)).SetCloneURL(fmt.Sprintf("https://example.com/repo-%02d.git", index)).SaveX(ctx)
		repositories = append(repositories, repo)
		pullRequests = append(pullRequests, client.PrRecord.Create().SetRepoConfigID(repo.ID).SetScmPrID(index+1).SetTitle(fmt.Sprintf("Scale PR %02d", index)).SetScmPrURL(fmt.Sprintf("https://example.com/pr/%d", index+1)).SaveX(ctx))
	}
	poolBuilders := make([]*ent.AttributionUsagePoolCreate, 0, poolCount)
	for index := 0; index < poolCount; index++ {
		poolBuilders = append(poolBuilders, client.AttributionUsagePool.Create().SetCanonicalPoolKey(fmt.Sprintf("scale-v2-%04d", index)).SetLedgerEpoch("formal_v2").
			SetRelayProviderID(provider.ID).SetUserID(user.ID).SetRequestedModel("model-test").SetBucketStartUtc(time.Date(2026, 8, 1, 0, index%60, 0, 0, time.UTC)).SetTotalTokens(1))
	}
	pools, err := client.AttributionUsagePool.CreateBulk(poolBuilders...).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	commitBuilders := make([]*ent.AttributionUsagePoolCommitCreate, 0, poolCount)
	prCommitBuilders := make([]*ent.PRCommitUsageSnapshotCreate, 0, poolCount)
	for index, pool := range pools {
		repositoryIndex := index % repositoryCount
		commitSHA := fmt.Sprintf("scale-v2-sha-%04d", index)
		commitBuilders = append(commitBuilders, client.AttributionUsagePoolCommit.Create().SetPoolID(pool.ID).SetRepoConfigID(repositories[repositoryIndex].ID).SetCommitSha(commitSHA).SetRelationKind(attributionusagepoolcommit.RelationKindDirect))
		prCommitBuilders = append(prCommitBuilders, client.PRCommitUsageSnapshot.Create().SetPrRecordID(pullRequests[repositoryIndex].ID).SetCommitSha(commitSHA))
	}
	if _, err := client.AttributionUsagePoolCommit.CreateBulk(commitBuilders...).Save(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.PRCommitUsageSnapshot.CreateBulk(prCommitBuilders...).Save(ctx); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	recordedDB := &recordingV2DB{DB: db}
	if _, err := db.ExecContext(ctx, "ANALYZE attribution_usage_pools; ANALYZE attribution_usage_pool_commits; ANALYZE pr_commit_usage_snapshots; ANALYZE pr_records"); err != nil {
		t.Fatal(err)
	}
	service := NewService(client, ServiceOptions{
		CursorSecret: "scale-secret", V2LedgerEpoch: "formal_v2", V2DB: recordedDB,
		V2Denominator: fixedV2Denominator{V2Denominator{TotalTokens: poolCount * 2, AsOf: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), Fresh: true, Complete: true}},
	})
	query := V2Query{Scope: V2ScopePersonal, FromDate: "2026-08-01", ToDate: "2026-08-01", Timezone: "UTC"}
	withinBudget := func(name string, call func() error, requiredSQL ...string) {
		t.Helper()
		recordedDB.reset()
		started := time.Now()
		if err := call(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if elapsed := time.Since(started); elapsed > maxReadLatency {
			t.Fatalf("%s latency %s exceeds %s budget at %d pools", name, elapsed, maxReadLatency, poolCount)
		}
		queries := recordedDB.snapshot()
		for _, required := range requiredSQL {
			found := false
			for _, query := range queries {
				if !strings.Contains(query.SQL, required) {
					continue
				}
				found = true
				if scanned := explainV2Query(t, db, query); scanned > maxScannedRows {
					t.Fatalf("%s scanned %d rows, exceeds %d-row budget for %q", name, scanned, maxScannedRows, required)
				}
			}
			if !found {
				t.Fatalf("%s did not execute production SQL containing %q", name, required)
			}
		}
	}
	withinBudget("summary and daily trend", func() error {
		result, err := service.V2Overview(ctx, user.ID, query)
		if err == nil && result.CommittedTokens != poolCount {
			return fmt.Errorf("committed tokens = %d, want %d", result.CommittedTokens, poolCount)
		}
		return err
	}, "WITH scoped AS", "WITH selected AS")
	var repositoryPage *V2Page[V2RepositoryRow]
	withinBudget("repository ranking", func() error {
		var err error
		repositoryPage, err = service.V2Repositories(ctx, user.ID, V2PageQuery{V2Query: query})
		return err
	}, "WITH pool_repo AS")
	withinBudget("repository search and name sort", func() error {
		_, err := service.V2Repositories(ctx, user.ID, V2PageQuery{V2Query: query, Search: "repo-1", Sort: "name"})
		return err
	}, "WITH pool_repo AS")
	withinBudget("repository cursor page", func() error {
		_, err := service.V2Repositories(ctx, user.ID, V2PageQuery{V2Query: query, Cursor: repositoryPage.NextCursor})
		return err
	}, "WITH pool_repo AS")
	var pullRequestPage *V2Page[V2PullRequestRow]
	withinBudget("pull-request ranking", func() error {
		var err error
		pullRequestPage, err = service.V2PullRequests(ctx, user.ID, V2PageQuery{V2Query: query})
		return err
	}, "WITH pool_pr AS")
	withinBudget("pull-request search and name sort", func() error {
		_, err := service.V2PullRequests(ctx, user.ID, V2PageQuery{V2Query: query, Search: "Scale PR 1", Sort: "name"})
		return err
	}, "WITH pool_pr AS")
	withinBudget("pull-request cursor page", func() error {
		_, err := service.V2PullRequests(ctx, user.ID, V2PageQuery{V2Query: query, Cursor: pullRequestPage.NextCursor})
		return err
	}, "WITH pool_pr AS")
}
