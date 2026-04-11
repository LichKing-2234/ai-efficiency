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
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

const defaultAdminDSN = "postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable"

var schemaInitMu sync.Mutex

func Open(t *testing.T) *ent.Client {
	t.Helper()
	client, _ := OpenWithDSN(t)
	return client
}

func OpenWithDSN(t *testing.T) (*ent.Client, string) {
	t.Helper()

	adminDSN := strings.TrimSpace(os.Getenv("AE_TEST_POSTGRES_DSN"))
	if adminDSN == "" {
		adminDSN = defaultAdminDSN
	}

	schemaName := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	adminDB, err := sql.Open("postgres", adminDSN)
	if err != nil {
		t.Fatalf("open postgres admin db: %v", err)
	}

	t.Cleanup(func() {
		adminDB.Close()
	})

	ctx := context.Background()
	if err := adminDB.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres admin db: %v", err)
	}

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
	client, err := ent.Open("postgres", dsn)
	if err != nil {
		schemaInitMu.Unlock()
		t.Fatalf("open ent client: %v", err)
	}
	if err := client.Schema.Create(ctx); err != nil {
		schemaInitMu.Unlock()
		t.Fatalf("migrate schema: %v", err)
	}
	schemaInitMu.Unlock()
	t.Cleanup(func() {
		client.Close()
	})

	return client, dsn
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
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
