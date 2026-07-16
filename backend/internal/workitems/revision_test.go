package workitems

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/ai-efficiency/backend/ent/systemsetting"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/google/uuid"
)

func TestRevisionStoreEnsureConcurrentInitializesOneUUID(t *testing.T) {
	client := testdb.Open(t)
	store := NewRevisionStore(client)
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
	count, err := client.SystemSetting.Query().Where(systemsetting.KeyEQ(workItemCountsRevisionKey)).Count(ctx)
	if err != nil || count != 1 {
		t.Fatalf("revision rows = %d, error = %v, want 1", count, err)
	}
}

func TestRevisionStoreCurrentRejectsMissingAndMalformedRows(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		store := NewRevisionStore(testdb.Open(t))
		if _, err := store.Current(context.Background()); err == nil || !strings.Contains(err.Error(), "not initialized") {
			t.Fatalf("Current() error = %v, want not initialized", err)
		}
	})

	t.Run("malformed", func(t *testing.T) {
		client := testdb.Open(t)
		ctx := context.Background()
		if _, err := client.SystemSetting.Create().SetKey(workItemCountsRevisionKey).SetValue("not-a-uuid").Save(ctx); err != nil {
			t.Fatalf("create malformed revision: %v", err)
		}
		store := NewRevisionStore(client)
		if _, err := store.Current(ctx); err == nil || !strings.Contains(err.Error(), "malformed") {
			t.Fatalf("Current() error = %v, want malformed", err)
		}
		if err := store.Ensure(ctx); err == nil || !strings.Contains(err.Error(), "malformed") {
			t.Fatalf("Ensure() error = %v, want malformed winner rejection", err)
		}
	})
}

func TestRevisionStoreInvalidationRollbackPreservesPreviousRevision(t *testing.T) {
	client := testdb.Open(t)
	store := NewRevisionStore(client)
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
	if err := store.InvalidateWorkItemCountsTx(ctx, tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("InvalidateWorkItemCountsTx() error = %v", err)
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

func TestRevisionStoreConcurrentCommittedBumpsRemainValid(t *testing.T) {
	client := testdb.Open(t)
	store := NewRevisionStore(client)
	ctx := context.Background()
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	const callers = 20
	errs := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() {
			tx, err := client.Tx(ctx)
			if err == nil {
				err = store.InvalidateWorkItemCountsTx(ctx, tx)
			}
			if err == nil {
				err = tx.Commit()
			} else if tx != nil {
				_ = tx.Rollback()
			}
			errs <- err
		}()
	}
	for i := 0; i < callers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent invalidation error = %v", err)
		}
	}
	revision, err := store.Current(ctx)
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if parsed, err := uuid.Parse(revision); err != nil || parsed.String() != revision {
		t.Fatalf("revision = %q, want canonical UUID", revision)
	}
}

func TestRevisionStoreInvalidationRequiresInitializedRow(t *testing.T) {
	client := testdb.Open(t)
	store := NewRevisionStore(client)
	ctx := context.Background()
	tx, err := client.Tx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()
	if err := store.InvalidateWorkItemCountsTx(ctx, tx); err == nil || !strings.Contains(err.Error(), "affected 0 rows") {
		t.Fatalf("InvalidateWorkItemCountsTx() error = %v, want affected 0 rows", err)
	}
}
