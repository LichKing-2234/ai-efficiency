package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	_ "github.com/ai-efficiency/backend/ent/runtime"
	"github.com/ai-efficiency/backend/internal/dbguard"
	"github.com/google/uuid"
	_ "github.com/lib/pq"

	entsql "entgo.io/ent/dialect/sql"
)

var defaultAdminDSNs = []string{
	"postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable",
	"postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable",
}

var schemaInitMu sync.Mutex

func Open(t *testing.T) *ent.Client {
	t.Helper()
	client, _ := OpenWithDSN(t)
	return client
}

func OpenWithDSN(t *testing.T) (*ent.Client, string) {
	t.Helper()

	adminDSN := strings.TrimSpace(os.Getenv("AE_TEST_POSTGRES_DSN"))
	schemaName := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	adminDB, adminDSN, err := openAdminDB(t, adminDSN)
	if err != nil {
		t.Fatalf("open postgres admin db: %v", err)
	}

	t.Cleanup(func() {
		adminDB.Close()
	})

	ctx := context.Background()
	if _, err := adminDB.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA "%s"`, schemaName)); err != nil {
		t.Fatalf("create schema %s: %v", schemaName, err)
	}
	t.Cleanup(func() {
		if _, err := adminDB.ExecContext(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName)); err != nil {
			t.Errorf("drop schema %s: %v", schemaName, err)
		}
	})

	dsn := withSearchPath(t, adminDSN, schemaName)
	schemaInitMu.Lock()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		schemaInitMu.Unlock()
		t.Fatalf("open test database: %v", err)
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB("postgres", db)))
	t.Cleanup(func() {
		client.Close()
	})
	if err := client.Schema.Create(ctx); err != nil {
		schemaInitMu.Unlock()
		t.Fatalf("migrate schema: %v", err)
	}
	if err := dbguard.InstallQuotaResetRequestEventsAppendOnlyGuard(ctx, db); err != nil {
		schemaInitMu.Unlock()
		t.Fatalf("install database guards: %v", err)
	}
	schemaInitMu.Unlock()

	return client, dsn
}

func openAdminDB(t *testing.T, envDSN string) (*sql.DB, string, error) {
	t.Helper()

	candidates := defaultAdminDSNs
	if envDSN != "" {
		candidates = []string{envDSN}
	}

	var attempts []string
	for _, dsn := range candidates {
		adminDB, err := sql.Open("postgres", dsn)
		if err != nil {
			attempts = append(attempts, fmt.Sprintf("%s: open failed: %v", dsn, err))
			continue
		}

		if err := adminDB.PingContext(context.Background()); err != nil {
			adminDB.Close()
			attempts = append(attempts, fmt.Sprintf("%s: ping failed: %v", dsn, err))
			continue
		}

		return adminDB, dsn, nil
	}

	return nil, "", fmt.Errorf("all postgres admin DSN candidates failed (%s)", strings.Join(attempts, "; "))
}

func withSearchPath(t *testing.T, dsn, schema string) string {
	t.Helper()

	out, err := withSearchPathValue(dsn, schema)
	if err != nil {
		t.Fatalf("build postgres dsn with search_path: %v", err)
	}
	return out
}

func withSearchPathValue(dsn, schema string) (string, error) {
	if !strings.Contains(dsn, "://") {
		return "", fmt.Errorf("AE_TEST_POSTGRES_DSN must be URL-form PostgreSQL DSN, got %q", dsn)
	}

	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("AE_TEST_POSTGRES_DSN must include scheme and host, got %q", dsn)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "postgres" && scheme != "postgresql" {
		return "", fmt.Errorf("AE_TEST_POSTGRES_DSN must use postgres scheme, got %q", u.Scheme)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
