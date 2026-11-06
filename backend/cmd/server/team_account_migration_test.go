package main

import (
	"context"
	"database/sql"
	"testing"

	"github.com/ai-efficiency/backend/internal/testdb"
	_ "github.com/lib/pq"
)

func TestDropLegacyTeamAccountDepartmentUnique(t *testing.T) {
	_, dsn := testdb.OpenWithDSN(t)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX team_accounts_department_external_id_key ON team_accounts (department_external_id)`); err != nil {
		t.Fatal(err)
	}
	if err := dropLegacyTeamAccountDepartmentUnique(ctx, db); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_indexes WHERE schemaname = current_schema() AND indexname = 'team_accounts_department_external_id_key'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("legacy index still present (%d)", count)
	}
}
