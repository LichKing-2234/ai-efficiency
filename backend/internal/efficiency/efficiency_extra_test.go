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
// LabelPR – cover the "no usage" update error path
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
// LabelPR – cover the "usage found, update PR record" error path
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

	seedLabelerCheckpointUsage(t, ctx, client, rc.ID, "feat-sess-hook", prCreatedAt.Add(-24*time.Hour), "codex-hook")

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
		t.Fatal("expected error from injected hook on usage path")
	}
}

// ---------------------------------------------------------------------------
// LabelPR – cover the checkpoint query error path
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

	// Drop the checkpoints table to make the checkpoint query fail
	rawDB, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()
	if _, err := rawDB.Exec("DROP TABLE IF EXISTS commit_checkpoints CASCADE"); err != nil {
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
		t.Fatal("expected error when commit_checkpoints table is dropped")
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
