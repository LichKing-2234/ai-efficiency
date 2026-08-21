package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/testdb"
	_ "github.com/lib/pq"
)

type formalAttributionSnapshot struct {
	pools               int
	relations           int
	inputTokens         int64
	outputTokens        int64
	cacheCreationTokens int64
	cacheReadTokens     int64
	totalTokens         int64
	requestCount        int64
}

func TestDropLegacyAttributionIdentityIndexes(t *testing.T) {
	_, dsn := testdb.OpenWithDSN(t)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	statements := []string{
		`CREATE UNIQUE INDEX commitcheckpoint_repo_config_id_commit_sha ON commit_checkpoints (repo_config_id, commit_sha)`,
		`CREATE UNIQUE INDEX commitrewrite_repo_config_id_old_commit_sha_new_commit_sha_rewrite_type ON commit_rewrites (repo_config_id, old_commit_sha, new_commit_sha, rewrite_type)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := dropLegacyAttributionIdentityIndexes(ctx, db); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_indexes WHERE schemaname = current_schema() AND indexname IN ($1, $2)`,
		"commitcheckpoint_repo_config_id_commit_sha", "commitrewrite_repo_config_id_old_commit_sha_new_commit_sha_rewrite_type").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("legacy attribution indexes remaining = %d", count)
	}
}

func TestLegacyAttributionCleanupDDLConservesFormalPoolsAndCommitRelations(t *testing.T) {
	client, dsn := testdb.OpenWithDSN(t)
	ctx := context.Background()
	pool := client.AttributionUsagePool.Create().
		SetCanonicalPoolKey("cleanup-preflight-formal-pool").
		SetLedgerEpoch("formal_v2").
		SetRelayProviderID(7).
		SetUserID(11).
		SetRequestedModel("model-test").
		SetBucketStartUtc(time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)).
		SetInputTokens(101).
		SetOutputTokens(202).
		SetCacheCreationTokens(303).
		SetCacheReadTokens(404).
		SetTotalTokens(1010).
		SetRequestCount(2).
		SaveX(ctx)
	client.AttributionUsagePoolCommit.Create().
		SetPoolID(pool.ID).
		SetRepoConfigID(13).
		SetCommitSha("cleanup-preflight-commit").
		SetRelationKind("direct").
		SaveX(ctx)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	for _, statement := range []string{
		`CREATE TABLE attribution_usage_buckets (id bigint PRIMARY KEY)`,
		`CREATE TABLE attribution_allocation_revisions (id bigint PRIMARY KEY, usage_bucket_id bigint NOT NULL REFERENCES attribution_usage_buckets(id))`,
		`ALTER TABLE reporting_installations ADD COLUMN otlp_token_hash text NOT NULL DEFAULT 'retired-otlp-test', ADD COLUMN otel_enabled boolean NOT NULL DEFAULT false`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			t.Fatalf("prepare legacy schema %q: %v", statement, err)
		}
	}

	before := readFormalAttributionSnapshot(t, ctx, tx)
	cleanupStatements := []string{
		`DROP TABLE IF EXISTS attribution_allocation_revisions`,
		`DROP TABLE IF EXISTS attribution_usage_buckets`,
		`ALTER TABLE reporting_installations DROP COLUMN IF EXISTS otlp_token_hash, DROP COLUMN IF EXISTS otel_enabled`,
	}
	for pass := 1; pass <= 2; pass++ {
		for _, statement := range cleanupStatements {
			if strings.Contains(strings.ToUpper(statement), "CASCADE") {
				t.Fatalf("cleanup statement uses CASCADE: %q", statement)
			}
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				t.Fatalf("rehearse cleanup pass %d %q: %v", pass, statement, err)
			}
		}
	}
	after := readFormalAttributionSnapshot(t, ctx, tx)
	if after != before {
		t.Fatalf("formal attribution changed across cleanup rehearsal: before=%+v after=%+v", before, after)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got := client.AttributionUsagePoolCommit.Query().CountX(ctx); got != 1 {
		t.Fatalf("formal commit relations after rollback = %d, want 1", got)
	}
}

func readFormalAttributionSnapshot(t *testing.T, ctx context.Context, query interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) formalAttributionSnapshot {
	t.Helper()
	var snapshot formalAttributionSnapshot
	if err := query.QueryRowContext(ctx, `
		SELECT count(*),
		       COALESCE(sum(input_tokens), 0),
		       COALESCE(sum(output_tokens), 0),
		       COALESCE(sum(cache_creation_tokens), 0),
		       COALESCE(sum(cache_read_tokens), 0),
		       COALESCE(sum(total_tokens), 0),
		       COALESCE(sum(request_count), 0)
		FROM attribution_usage_pools
		WHERE ledger_epoch = 'formal_v2'`).Scan(
		&snapshot.pools,
		&snapshot.inputTokens,
		&snapshot.outputTokens,
		&snapshot.cacheCreationTokens,
		&snapshot.cacheReadTokens,
		&snapshot.totalTokens,
		&snapshot.requestCount,
	); err != nil {
		t.Fatal(err)
	}
	if err := query.QueryRowContext(ctx, `
		SELECT count(*)
		FROM attribution_usage_pool_commits relations
		JOIN attribution_usage_pools pools ON pools.id = relations.pool_id
		WHERE pools.ledger_epoch = 'formal_v2'`).Scan(&snapshot.relations); err != nil {
		t.Fatal(err)
	}
	return snapshot
}
