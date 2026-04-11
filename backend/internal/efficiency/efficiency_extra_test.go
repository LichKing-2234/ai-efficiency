package efficiency

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// AggregateAll – cover the per-repo error warn path (line 131-134)
// ---------------------------------------------------------------------------

func TestAggregateAllPerRepoError(t *testing.T) {
	client, dsn := testdb.OpenWithDSN(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	sp := client.ScmProvider.Create().
		SetName("test-provider").
		SetType(scmprovider.TypeGithub).
		SetBaseURL("https://github.com").
		SetCredentials("test-token").
		SaveX(ctx)

	client.RepoConfig.Create().
		SetName("repo-agg-fail-1").
		SetFullName("org/repo-agg-fail-1").
		SetCloneURL("https://github.com/org/repo-agg-fail-1.git").
		SetScmProviderID(sp.ID).
		SaveX(ctx)

	client.RepoConfig.Create().
		SetName("repo-agg-fail-2").
		SetFullName("org/repo-agg-fail-2").
		SetCloneURL("https://github.com/org/repo-agg-fail-2.git").
		SetScmProviderID(sp.ID).
		SaveX(ctx)

	// Drop the efficiency_metrics table to force AggregateForRepo to fail
	rawDB, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()
	if _, err := rawDB.Exec("DROP TABLE IF EXISTS efficiency_metrics CASCADE"); err != nil {
		t.Fatal(err)
	}

	agg := NewAggregator(client, logger)

	// AggregateAll should NOT return an error — it logs warnings per repo
	err = agg.AggregateAll(ctx, "daily")
	if err != nil {
		t.Fatalf("AggregateAll should not return error: %v", err)
	}
}

func TestAggregateAllListReposError(t *testing.T) {
	client := testdb.Open(t)
	logger := zap.NewNop()

	agg := NewAggregator(client, logger)
	client.Close()

	err := agg.AggregateAll(context.Background(), "daily")
	if err == nil {
		t.Fatal("expected error when DB is closed")
	}
}

// ---------------------------------------------------------------------------
// AggregateForRepo – cover the "query existing metric" non-NotFound error
// ---------------------------------------------------------------------------

func TestAggregateForRepoMetricQueryNonNotFoundError(t *testing.T) {
	client, dsn := testdb.OpenWithDSN(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "metric-nf-err-repo")
	agg := NewAggregator(client, logger)

	periodStart := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)

	// First, run a successful aggregation to create the metric
	if err := agg.AggregateForRepo(ctx, rc.ID, "daily", periodStart); err != nil {
		t.Fatalf("first aggregation: %v", err)
	}

	// Rename the table so the query fails with a non-NotFound error
	rawDB, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()
	if _, err := rawDB.Exec("ALTER TABLE efficiency_metrics RENAME TO efficiency_metrics_bak"); err != nil {
		t.Fatal(err)
	}

	err = agg.AggregateForRepo(ctx, rc.ID, "daily", periodStart)
	if err == nil {
		t.Fatal("expected error when efficiency_metrics table is missing")
	}
}

// ---------------------------------------------------------------------------
// AggregateForRepo – closed DB error
// ---------------------------------------------------------------------------

func TestAggregateForRepoClosedDB(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "closed-db-repo")
	agg := NewAggregator(client, logger)

	client.Close()

	periodStart := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	err := agg.AggregateForRepo(ctx, rc.ID, "daily", periodStart)
	if err == nil {
		t.Fatal("expected error when DB is closed")
	}
}

// ---------------------------------------------------------------------------
// LabelPR – cover the "no sessions" update error path (line 80-83)
// Use a hook to inject an error on PrRecord update.
// ---------------------------------------------------------------------------

func TestLabelPRNoSessionsUpdateErrorViaHook(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "no-sess-hook-repo")

	pr := client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(6001).
		SetSourceBranch("feat-no-sess-hook").
		SetTargetBranch("main").
		SaveX(ctx)

	// Install a hook that makes PrRecord updates fail
	client.PrRecord.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if m.Op().Is(ent.OpUpdateOne) {
				return nil, fmt.Errorf("injected update error")
			}
			return next.Mutate(ctx, m)
		})
	})

	lab := NewLabeler(client, nil, logger)
	_, err := lab.LabelPR(ctx, pr.ID)
	if err == nil {
		t.Fatal("expected error from injected hook")
	}
}

// ---------------------------------------------------------------------------
// LabelPR – cover the "sessions found, update PR record" error path (line 138)
// ---------------------------------------------------------------------------

func TestLabelPRWithSessionsUpdateErrorViaHook(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "sess-hook-repo")

	prCreatedAt := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	pr := client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(6002).
		SetSourceBranch("feat-sess-hook").
		SetTargetBranch("main").
		SetCreatedAt(prCreatedAt).
		SaveX(ctx)

	sessionTime := prCreatedAt.Add(-1 * 24 * time.Hour)
	client.Session.Create().
		SetID(uuid.New()).
		SetRepoConfigID(rc.ID).
		SetBranch("feat-sess-hook").
		SetStartedAt(sessionTime).
		SetCreatedAt(sessionTime).
		SaveX(ctx)

	// Install a hook that makes PrRecord updates fail
	client.PrRecord.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if m.Op().Is(ent.OpUpdateOne) {
				return nil, fmt.Errorf("injected update error")
			}
			return next.Mutate(ctx, m)
		})
	})

	lab := NewLabeler(client, nil, logger)
	_, err := lab.LabelPR(ctx, pr.ID)
	if err == nil {
		t.Fatal("expected error from injected hook on sessions path")
	}
}

// ---------------------------------------------------------------------------
// LabelPR – cover the session query error path (line 69-71)
// ---------------------------------------------------------------------------

func TestLabelPRSessionQueryError(t *testing.T) {
	client, dsn := testdb.OpenWithDSN(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "sess-query-err-repo")

	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(6003).
		SetSourceBranch("feat-sess-q-err").
		SetTargetBranch("main").
		SaveX(ctx)

	// Drop the sessions table to make the session query fail
	rawDB, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()
	if _, err := rawDB.Exec("DROP TABLE IF EXISTS sessions CASCADE"); err != nil {
		t.Fatal(err)
	}

	lab := NewLabeler(client, nil, logger)
	// Use the PR ID = 1 (first created)
	prs, _ := client.PrRecord.Query().All(ctx)
	if len(prs) == 0 {
		t.Fatal("no PR records found")
	}
	_, err = lab.LabelPR(ctx, prs[0].ID)
	if err == nil {
		t.Fatal("expected error when sessions table is dropped")
	}
}

// ---------------------------------------------------------------------------
// LabelPR – sessions with API key but no relay provider
// ---------------------------------------------------------------------------

func TestLabelPRWithSessionsHavingAPIKeyButNoRelayProvider(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "apikey-no-client-repo")

	prCreatedAt := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	pr := client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(5001).
		SetSourceBranch("feat-apikey").
		SetTargetBranch("main").
		SetLinesAdded(60).
		SetLinesDeleted(15).
		SetCreatedAt(prCreatedAt).
		SaveX(ctx)

	sessionTime := prCreatedAt.Add(-1 * 24 * time.Hour)
	apiKeyID := 42
	client.Session.Create().
		SetID(uuid.New()).
		SetRepoConfigID(rc.ID).
		SetBranch("feat-apikey").
		SetStartedAt(sessionTime).
		SetCreatedAt(sessionTime).
		SetRelayAPIKeyID(apiKeyID).
		SaveX(ctx)

	lab := NewLabeler(client, nil, logger)
	result, err := lab.LabelPR(ctx, pr.ID)
	if err != nil {
		t.Fatalf("LabelPR: %v", err)
	}

	if result.AILabel != "ai_via_sub2api" {
		t.Errorf("ai_label = %q, want %q", result.AILabel, "ai_via_sub2api")
	}
	if result.TokenCost != 0 {
		t.Errorf("token_cost = %f, want 0", result.TokenCost)
	}
	if result.AIRatio != 0.5 {
		t.Errorf("ai_ratio = %f, want 0.5", result.AIRatio)
	}
}

func TestLabelPRWithSessionHavingEndedAt(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "ended-at-repo")

	prCreatedAt := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	pr := client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(5002).
		SetSourceBranch("feat-ended").
		SetTargetBranch("main").
		SetLinesAdded(40).
		SetLinesDeleted(10).
		SetCreatedAt(prCreatedAt).
		SaveX(ctx)

	sessionTime := prCreatedAt.Add(-1 * 24 * time.Hour)
	endedAt := sessionTime.Add(2 * time.Hour)
	client.Session.Create().
		SetID(uuid.New()).
		SetRepoConfigID(rc.ID).
		SetBranch("feat-ended").
		SetStartedAt(sessionTime).
		SetCreatedAt(sessionTime).
		SetEndedAt(endedAt).
		SaveX(ctx)

	lab := NewLabeler(client, nil, logger)
	result, err := lab.LabelPR(ctx, pr.ID)
	if err != nil {
		t.Fatalf("LabelPR: %v", err)
	}

	if result.AILabel != "ai_via_sub2api" {
		t.Errorf("ai_label = %q, want %q", result.AILabel, "ai_via_sub2api")
	}
	if result.AIRatio != 0.5 {
		t.Errorf("ai_ratio = %f, want 0.5", result.AIRatio)
	}
}

// ---------------------------------------------------------------------------
// LabelPR – cover the "rc == nil" path (line 53-54)
// Create a PR record, then delete its repo config with FK disabled,
// so WithRepoConfig() returns nil edges.
// ---------------------------------------------------------------------------

func TestLabelPRRepoConfigNilAfterDelete(t *testing.T) {
	client, dsn := testdb.OpenWithDSN(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "rc-nil-repo")

	pr := client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(7001).
		SetSourceBranch("feat-rc-nil").
		SetTargetBranch("main").
		SaveX(ctx)

	// Use raw SQL with FK disabled to delete the repo config
	rawDB, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()
	if _, err := rawDB.Exec("ALTER TABLE pr_records DROP CONSTRAINT pr_records_repo_configs_pr_records"); err != nil {
		t.Fatal(err)
	}
	// Delete the repo config after removing the FK in this test schema to reproduce a dangling edge.
	if _, err := rawDB.Exec("DELETE FROM repo_configs WHERE id = $1", rc.ID); err != nil {
		t.Fatal(err)
	}

	lab := NewLabeler(client, nil, logger)
	_, err = lab.LabelPR(ctx, pr.ID)
	if err == nil {
		t.Fatal("expected error when repo config is nil")
	}
	// Should contain "no repo config" or similar
	t.Logf("error: %v", err)
}
