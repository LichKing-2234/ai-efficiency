package quotareset

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/lib/pq"

	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestApprovalHistoryPredicateUsesBothGINIndexes(t *testing.T) {
	_, dsn := testdb.OpenWithDSN(t)
	ctx := context.Background()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	indexNames := map[string]string{}
	rows, err := db.QueryContext(ctx, `
		SELECT indexed_attribute.attname, index_relation.relname
		FROM pg_index AS index_metadata
		JOIN pg_class AS table_relation ON table_relation.oid = index_metadata.indrelid
		JOIN pg_class AS index_relation ON index_relation.oid = index_metadata.indexrelid
		JOIN pg_namespace AS table_namespace ON table_namespace.oid = table_relation.relnamespace
		JOIN pg_am AS access_method ON access_method.oid = index_relation.relam
		JOIN pg_attribute AS indexed_attribute
		  ON indexed_attribute.attrelid = table_relation.oid
		 AND indexed_attribute.attnum = ANY(index_metadata.indkey)
		WHERE table_namespace.nspname = current_schema()
		  AND table_relation.relname = 'quota_reset_requests'
		  AND access_method.amname = 'gin'
		  AND indexed_attribute.attname IN ('resolved_approver_user_ids', 'workflow')
	`)
	if err != nil {
		t.Fatalf("list quota reset GIN indexes: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var column, indexName string
		if err := rows.Scan(&column, &indexName); err != nil {
			t.Fatalf("scan quota reset GIN index: %v", err)
		}
		indexNames[column] = indexName
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read quota reset GIN indexes: %v", err)
	}
	for _, column := range []string{"resolved_approver_user_ids", "workflow"} {
		if indexNames[column] == "" {
			t.Fatalf("missing GIN index for quota_reset_requests.%s", column)
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin explain transaction: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatalf("disable sequential scans: %v", err)
	}
	planRows, err := tx.QueryContext(ctx, `
		EXPLAIN (COSTS OFF)
		SELECT id
		FROM quota_reset_requests
		WHERE resolved_approver_user_ids::jsonb @> '[123]'::jsonb
		   OR workflow::jsonb @> '{"steps":[{"decision":{"actor_user_id":123}}]}'::jsonb
	`)
	if err != nil {
		t.Fatalf("explain approval history predicate: %v", err)
	}
	defer planRows.Close()
	var planLines []string
	for planRows.Next() {
		var line string
		if err := planRows.Scan(&line); err != nil {
			t.Fatalf("scan explain line: %v", err)
		}
		planLines = append(planLines, line)
	}
	if err := planRows.Err(); err != nil {
		t.Fatalf("read explain plan: %v", err)
	}
	plan := strings.Join(planLines, "\n")
	for column, indexName := range indexNames {
		if !strings.Contains(plan, indexName) {
			t.Fatalf("EXPLAIN did not select %s for %s predicate:\n%s", indexName, column, plan)
		}
	}
	if !strings.Contains(plan, "BitmapOr") {
		t.Fatalf("EXPLAIN did not combine approval indexes with BitmapOr:\n%s", plan)
	}
}

func TestWorkflowRepresentativeLookupsUseDirectoryMemberIndexes(t *testing.T) {
	_, dsn := testdb.OpenWithDSN(t)
	ctx := context.Background()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	indexNames := map[string]string{}
	rows, err := db.QueryContext(ctx, `
		SELECT index_relation.relname, pg_get_indexdef(index_relation.oid)
		FROM pg_index AS index_metadata
		JOIN pg_class AS table_relation ON table_relation.oid = index_metadata.indrelid
		JOIN pg_class AS index_relation ON index_relation.oid = index_metadata.indexrelid
		JOIN pg_namespace AS table_namespace ON table_namespace.oid = table_relation.relnamespace
		WHERE table_namespace.nspname = current_schema()
		  AND table_relation.relname = 'directory_members'
	`)
	if err != nil {
		t.Fatalf("list directory member indexes: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, definition string
		if err := rows.Scan(&name, &definition); err != nil {
			t.Fatalf("scan directory member index: %v", err)
		}
		normalized := strings.ToLower(definition)
		if strings.Contains(normalized, "using gin (metadata jsonb_path_ops)") {
			indexNames["metadata"] = name
		}
		if strings.Contains(normalized, "(source_id, external_id)") {
			indexNames["external_id"] = name
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read directory member indexes: %v", err)
	}
	for _, lookup := range []string{"metadata", "external_id"} {
		if indexNames[lookup] == "" {
			t.Fatalf("missing directory_members index for workflow %s lookup", lookup)
		}
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO directory_members (
			source_id, external_id, email_normalized, display_name,
			department_external_id, status, metadata, last_seen_run_id, created_at, updated_at
		)
		SELECT
			((sequence - 1) % 10) + 1,
			'member-' || sequence,
			'member-' || sequence || '@example.com',
			'Member ' || sequence,
			'dept-' || sequence,
			'active',
			CASE WHEN sequence IN (1, 11)
				THEN '{"leader_department_ids":["dept-alpha"]}'::jsonb
				ELSE '{}'::jsonb
			END,
			1,
			NOW(),
			NOW()
		FROM generate_series(1, 10000) AS sequence
	`); err != nil {
		t.Fatalf("seed directory member plan fixtures: %v", err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE directory_members"); err != nil {
		t.Fatalf("analyze directory member plan fixtures: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin explain transaction: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatalf("disable sequential scans: %v", err)
	}
	assertExplainUsesIndex(t, ctx, tx, indexNames["metadata"], `
		SELECT id FROM directory_members
		WHERE source_id = 1
		  AND metadata @> '{"leader_department_ids":"dept-alpha"}'::jsonb
	`)
	assertExplainUsesIndex(t, ctx, tx, indexNames["metadata"], `
		SELECT id FROM directory_members
		WHERE source_id = 1
		  AND metadata @> '{"leader_department_ids":["dept-alpha"]}'::jsonb
	`)
	assertExplainUsesIndex(t, ctx, tx, indexNames["external_id"], `
		SELECT id FROM directory_members
		WHERE source_id = 1 AND external_id IN ('member-alpha', 'member-beta')
	`)
}

func assertExplainUsesIndex(t *testing.T, ctx context.Context, tx *sql.Tx, indexName, query string) {
	t.Helper()
	rows, err := tx.QueryContext(ctx, "EXPLAIN (COSTS OFF) "+query)
	if err != nil {
		t.Fatalf("explain indexed query: %v", err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan explain line: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read explain plan: %v", err)
	}
	plan := strings.Join(lines, "\n")
	if !strings.Contains(plan, indexName) {
		t.Fatalf("EXPLAIN did not select %s:\n%s", indexName, plan)
	}
}
