package main

import (
	"context"
	"database/sql"
	"testing"

	"github.com/ai-efficiency/backend/internal/testdb"
	_ "github.com/lib/pq"
)

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
