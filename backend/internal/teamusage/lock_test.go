package teamusage

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func TestNilAdvisoryLockerRunsCallback(t *testing.T) {
	var locker *PostgresAdvisoryLocker
	called := false
	if err := locker.WithProviderGroupLock(context.Background(), 7, 42, func(context.Context) error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("WithProviderGroupLock() error = %v", err)
	}
	if !called {
		t.Fatal("callback was not invoked")
	}
}

func TestPostgresAdvisoryLockerSerializesSameProviderGroup(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("AE_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("AE_TEST_DATABASE_URL is not set")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("sql.Open(): %v", err)
	}
	defer db.Close()
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("db.PingContext(): %v", err)
	}

	locker := NewPostgresAdvisoryLocker(db)
	firstAcquired := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	secondEntered := make(chan struct{}, 1)
	secondDone := make(chan error, 1)

	go func() {
		firstDone <- locker.WithProviderGroupLock(context.Background(), 11, 99, func(context.Context) error {
			close(firstAcquired)
			<-releaseFirst
			return nil
		})
	}()

	select {
	case <-firstAcquired:
	case <-time.After(5 * time.Second):
		t.Fatal("first lock was not acquired")
	}

	go func() {
		secondDone <- locker.WithProviderGroupLock(context.Background(), 11, 99, func(context.Context) error {
			secondEntered <- struct{}{}
			return nil
		})
	}()

	select {
	case <-secondEntered:
		t.Fatal("second lock entered before first lock released")
	case <-time.After(250 * time.Millisecond):
	}

	close(releaseFirst)

	if err := <-firstDone; err != nil {
		t.Fatalf("first lock error = %v", err)
	}
	select {
	case <-secondEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("second lock did not enter after first lock released")
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second lock error = %v", err)
	}
}
