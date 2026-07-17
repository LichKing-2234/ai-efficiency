package repo

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/ai-efficiency/backend/ent/systemsetting"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/google/uuid"
)

func TestInventoryRevisionEnsureConcurrentInitializesOneUUID(t *testing.T) {
	client := testdb.Open(t)
	store := NewInventoryRevisionStore(client)
	ctx := context.Background()

	const callers = 25
	errs := make(chan error, callers)
	var start sync.WaitGroup
	start.Add(1)
	for i := 0; i < callers; i++ {
		go func() {
			start.Wait()
			errs <- store.Ensure(ctx)
		}()
	}
	start.Done()
	for i := 0; i < callers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("Ensure() error = %v", err)
		}
	}

	revision, err := store.Current(ctx)
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if parsed, err := uuid.Parse(revision); err != nil || parsed.String() != revision {
		t.Fatalf("revision = %q, want canonical UUID", revision)
	}
	count, err := client.SystemSetting.Query().Where(systemsetting.KeyEQ(repoInventoryRevisionKey)).Count(ctx)
	if err != nil || count != 1 {
		t.Fatalf("revision rows = %d, error = %v, want 1", count, err)
	}
}

func TestInventoryRevisionCurrentRejectsMissingAndMalformedRows(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		store := NewInventoryRevisionStore(testdb.Open(t))
		if _, err := store.Current(context.Background()); err == nil || !strings.Contains(err.Error(), "not initialized") {
			t.Fatalf("Current() error = %v, want not initialized", err)
		}
	})

	t.Run("malformed", func(t *testing.T) {
		client := testdb.Open(t)
		ctx := context.Background()
		if _, err := client.SystemSetting.Create().SetKey(repoInventoryRevisionKey).SetValue("not-a-uuid").Save(ctx); err != nil {
			t.Fatalf("create malformed revision: %v", err)
		}
		store := NewInventoryRevisionStore(client)
		if _, err := store.Current(ctx); err == nil || !strings.Contains(err.Error(), "malformed") {
			t.Fatalf("Current() error = %v, want malformed", err)
		}
		if err := store.Ensure(ctx); err == nil || !strings.Contains(err.Error(), "malformed") {
			t.Fatalf("Ensure() error = %v, want malformed winner rejection", err)
		}
	})
}

func TestInventoryRevisionInvalidationRollbackPreservesPreviousRevision(t *testing.T) {
	client := testdb.Open(t)
	store := NewInventoryRevisionStore(client)
	ctx := context.Background()
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	before, err := store.Current(ctx)
	if err != nil {
		t.Fatalf("Current() before error = %v", err)
	}

	tx, err := client.Tx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := store.InvalidateTx(ctx, tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("InvalidateTx() error = %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	after, err := store.Current(ctx)
	if err != nil {
		t.Fatalf("Current() after error = %v", err)
	}
	if after != before {
		t.Fatalf("revision after rollback = %q, want %q", after, before)
	}
}

func TestInventoryRevisionInvalidationChangesOneCanonicalUUID(t *testing.T) {
	client := testdb.Open(t)
	store := NewInventoryRevisionStore(client)
	ctx := context.Background()
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	before, err := store.Current(ctx)
	if err != nil {
		t.Fatalf("Current() before error = %v", err)
	}

	tx, err := client.Tx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := store.InvalidateTx(ctx, tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("InvalidateTx() error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	after, err := store.Current(ctx)
	if err != nil {
		t.Fatalf("Current() after error = %v", err)
	}
	if after == before {
		t.Fatalf("revision after invalidation = %q, want a new UUID", after)
	}
	if parsed, err := uuid.Parse(after); err != nil || parsed.String() != after {
		t.Fatalf("revision = %q, want canonical UUID", after)
	}
}

func TestInventoryRevisionInvalidationRequiresInitializedRow(t *testing.T) {
	client := testdb.Open(t)
	store := NewInventoryRevisionStore(client)
	ctx := context.Background()
	tx, err := client.Tx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()
	if err := store.InvalidateTx(ctx, tx); err == nil || !strings.Contains(err.Error(), "affected 0 rows") {
		t.Fatalf("InvalidateTx() error = %v, want affected 0 rows", err)
	}
}
