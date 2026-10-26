package relayruntime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
	"go.uber.org/zap"
)

// ownerAccountProvider is a provider whose directory capability is scripted by
// the test. It embeds the optional capability only, so the manager has to type
// assert exactly like production does.
type ownerAccountProvider struct {
	relay.Provider
	load func(context.Context) ([]relay.OwnerAccount, error)
}

func (p *ownerAccountProvider) ListOwnerAccounts(ctx context.Context, _ string) ([]relay.OwnerAccount, error) {
	return p.load(ctx)
}

type cappedProvider struct {
	relay.Provider
}

func newOwnerAccountManager(t *testing.T, client *ent.Client, now func() time.Time, provider relay.Provider) *Manager {
	t.Helper()
	manager, err := NewManager(client, testEncryptionKey, zap.NewNop(), Options{
		Now:                  now,
		DirectoryLoadTimeout: 30 * time.Millisecond,
		Factory: func(_ *ent.RelayProvider, _ string) (relay.Provider, error) {
			return provider, nil
		},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	return manager
}

func TestOwnerAccountsServesTheCachedDirectoryUntilItsTTLExpires(t *testing.T) {
	client := testdb.Open(t)
	row := createProviderRow(t, client)
	now := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
	var loads atomic.Int32
	manager := newOwnerAccountManager(t, client, func() time.Time { return now }, &ownerAccountProvider{
		load: func(context.Context) ([]relay.OwnerAccount, error) {
			loads.Add(1)
			return []relay.OwnerAccount{{ID: 11, Name: "alice@example.com"}}, nil
		},
	})

	first, err := manager.OwnerAccounts(context.Background(), row.ID, row.ConfigurationVersion, "openai")
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	second, err := manager.OwnerAccounts(context.Background(), row.ID, row.ConfigurationVersion, "openai")
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if loads.Load() != 1 {
		t.Fatalf("directory loads = %d, want 1 within the TTL", loads.Load())
	}
	if len(first) != 1 || len(second) != 1 || first[0].ID != second[0].ID {
		t.Fatalf("cached directory = %+v / %+v", first, second)
	}

	now = now.Add(5*time.Minute + time.Nanosecond)
	if _, err := manager.OwnerAccounts(context.Background(), row.ID, row.ConfigurationVersion, "openai"); err != nil {
		t.Fatalf("read after TTL: %v", err)
	}
	if loads.Load() != 2 {
		t.Fatalf("directory loads = %d, want 2 after the TTL expired", loads.Load())
	}
}

func TestOwnerAccountsCollapsesConcurrentMissesIntoSingleFlight(t *testing.T) {
	client := testdb.Open(t)
	row := createProviderRow(t, client)
	now := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
	var loads atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	manager := newOwnerAccountManager(t, client, func() time.Time { return now }, &ownerAccountProvider{
		load: func(context.Context) ([]relay.OwnerAccount, error) {
			if loads.Add(1) == 1 {
				close(entered)
			}
			<-release
			return []relay.OwnerAccount{{ID: 11}}, nil
		},
	})

	var wg sync.WaitGroup
	results := make([][]relay.OwnerAccount, 2)
	errs := make([]error, 2)
	for index := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index], errs[index] = manager.OwnerAccounts(context.Background(), row.ID, row.ConfigurationVersion, "openai")
		}(index)
	}
	<-entered
	close(release)
	wg.Wait()

	for index, err := range errs {
		if err != nil {
			t.Fatalf("read %d error = %v", index, err)
		}
		if len(results[index]) != 1 {
			t.Fatalf("read %d accounts = %+v", index, results[index])
		}
	}
	if loads.Load() != 1 {
		t.Fatalf("directory loads = %d, want one shared load", loads.Load())
	}
}

func TestOwnerAccountsServesTheStaleDirectoryWhenAReloadFails(t *testing.T) {
	client := testdb.Open(t)
	row := createProviderRow(t, client)
	now := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
	var fail atomic.Bool
	manager := newOwnerAccountManager(t, client, func() time.Time { return now }, &ownerAccountProvider{
		load: func(context.Context) ([]relay.OwnerAccount, error) {
			if fail.Load() {
				return nil, errors.New("upstream unavailable")
			}
			return []relay.OwnerAccount{{ID: 11, Name: "alice@example.com"}}, nil
		},
	})

	if _, err := manager.OwnerAccounts(context.Background(), row.ID, row.ConfigurationVersion, "openai"); err != nil {
		t.Fatalf("first read: %v", err)
	}
	fail.Store(true)
	now = now.Add(5*time.Minute + time.Nanosecond)

	stale, err := manager.OwnerAccounts(context.Background(), row.ID, row.ConfigurationVersion, "openai")
	if err != nil {
		t.Fatalf("stale read error = %v, want the last good directory", err)
	}
	if len(stale) != 1 || stale[0].ID != 11 {
		t.Fatalf("stale directory = %+v, want the last good entry", stale)
	}
}

func TestOwnerAccountsReportsAFailedFirstLoadAndTheLoadDeadline(t *testing.T) {
	client := testdb.Open(t)
	row := createProviderRow(t, client)
	now := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)

	t.Run("first load failure", func(t *testing.T) {
		manager := newOwnerAccountManager(t, client, func() time.Time { return now }, &ownerAccountProvider{
			load: func(context.Context) ([]relay.OwnerAccount, error) {
				return nil, errors.New("upstream unavailable")
			},
		})
		if _, err := manager.OwnerAccounts(context.Background(), row.ID, row.ConfigurationVersion, "openai"); err == nil {
			t.Fatal("expected the first load failure to surface")
		}
	})

	t.Run("load deadline", func(t *testing.T) {
		manager := newOwnerAccountManager(t, client, func() time.Time { return now }, &ownerAccountProvider{
			load: func(ctx context.Context) ([]relay.OwnerAccount, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
		})
		started := time.Now()
		_, err := manager.OwnerAccounts(context.Background(), row.ID, row.ConfigurationVersion, "openai")
		if err == nil {
			t.Fatal("expected the load deadline to surface")
		}
		if elapsed := time.Since(started); elapsed > 5*time.Second {
			t.Fatalf("directory load took %s, want the configured deadline", elapsed)
		}
	})
}

func TestOwnerAccountsInvalidationForcesTheNextReadToReload(t *testing.T) {
	client := testdb.Open(t)
	row := createProviderRow(t, client)
	now := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
	var loads atomic.Int32
	manager := newOwnerAccountManager(t, client, func() time.Time { return now }, &ownerAccountProvider{
		load: func(context.Context) ([]relay.OwnerAccount, error) {
			count := loads.Add(1)
			return []relay.OwnerAccount{{ID: int64(count)}}, nil
		},
	})

	if _, err := manager.OwnerAccounts(context.Background(), row.ID, row.ConfigurationVersion, "openai"); err != nil {
		t.Fatalf("first read: %v", err)
	}
	manager.InvalidateOwnerAccounts(row.ID, "openai")
	refreshed, err := manager.OwnerAccounts(context.Background(), row.ID, row.ConfigurationVersion, "openai")
	if err != nil {
		t.Fatalf("read after invalidation: %v", err)
	}
	if loads.Load() != 2 || len(refreshed) != 1 || refreshed[0].ID != 2 {
		t.Fatalf("loads = %d, refreshed = %+v; want a reload after invalidation", loads.Load(), refreshed)
	}
}

func TestOwnerAccountsReportsAnUnsupportedCapabilityAndStaleConfiguration(t *testing.T) {
	client := testdb.Open(t)
	row := createProviderRow(t, client)
	now := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)

	t.Run("capability unsupported", func(t *testing.T) {
		manager := newOwnerAccountManager(t, client, func() time.Time { return now }, &cappedProvider{})
		_, err := manager.OwnerAccounts(context.Background(), row.ID, row.ConfigurationVersion, "openai")
		if err == nil || !errors.Is(err, relay.ErrCapabilityUnsupported) {
			t.Fatalf("error = %v, want ErrCapabilityUnsupported", err)
		}
	})

	t.Run("stale configuration version", func(t *testing.T) {
		manager := newOwnerAccountManager(t, client, func() time.Time { return now }, &ownerAccountProvider{
			load: func(context.Context) ([]relay.OwnerAccount, error) {
				return []relay.OwnerAccount{{ID: 11}}, nil
			},
		})
		_, err := manager.OwnerAccounts(context.Background(), row.ID, row.ConfigurationVersion+1, "openai")
		if err == nil || !errors.Is(err, ErrStaleProviderConfiguration) {
			t.Fatalf("error = %v, want ErrStaleProviderConfiguration", err)
		}
	})
}

func TestResolveLongRunningUsesItsOwnFactoryAndCachedClient(t *testing.T) {
	client := testdb.Open(t)
	row := createProviderRow(t, client)
	normalCreates := 0
	longCreates := 0
	manager, err := NewManager(client, testEncryptionKey, zap.NewNop(), Options{
		Factory: func(_ *ent.RelayProvider, _ string) (relay.Provider, error) {
			normalCreates++
			return &taggedProvider{tag: "normal"}, nil
		},
		LongRunningFactory: func(_ *ent.RelayProvider, _ string) (relay.Provider, error) {
			longCreates++
			return &taggedProvider{tag: "long"}, nil
		},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	normal, err := manager.Resolve(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if normal.(*taggedProvider).tag != "normal" {
		t.Fatalf("normal provider tag = %q, want normal", normal.(*taggedProvider).tag)
	}
	first, err := manager.ResolveLongRunning(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("resolve long running: %v", err)
	}
	second, err := manager.ResolveLongRunning(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("resolve long running again: %v", err)
	}
	if first.(*taggedProvider).tag != "long" {
		t.Fatalf("long running provider tag = %q, want long", first.(*taggedProvider).tag)
	}
	if first != second {
		t.Fatalf("long running client was recreated: %p/%p", first, second)
	}
	if normalCreates != 1 || longCreates != 1 {
		t.Fatalf("creates = %d normal / %d long, want one each", normalCreates, longCreates)
	}

	if err := manager.Invalidate(context.Background(), row.ID, row.ConfigurationVersion); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	if _, err := manager.ResolveLongRunning(context.Background(), row.ID); err != nil {
		t.Fatalf("resolve long running after invalidation: %v", err)
	}
	if longCreates != 2 {
		t.Fatalf("long running creates = %d, want the invalidation to evict it", longCreates)
	}
}

// TestOwnerAccountsDoesNotRepopulateAfterAnInvalidationDuringALoad asserts the
// generation guard: a directory load that started before a mutation invalidated the
// entry must not plant pre-mutation snapshots for the next five minutes.
func TestOwnerAccountsDoesNotRepopulateAfterAnInvalidationDuringALoad(t *testing.T) {
	client := testdb.Open(t)
	row := createProviderRow(t, client)
	now := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
	var loads atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	manager := newOwnerAccountManager(t, client, func() time.Time { return now }, &ownerAccountProvider{
		load: func(context.Context) ([]relay.OwnerAccount, error) {
			if loads.Add(1) == 1 {
				close(entered)
				<-release
				return []relay.OwnerAccount{{ID: 1}}, nil
			}
			return []relay.OwnerAccount{{ID: 2}}, nil
		},
	})

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		if _, err := manager.OwnerAccounts(context.Background(), row.ID, row.ConfigurationVersion, "openai"); err != nil {
			t.Errorf("first read: %v", err)
		}
	}()
	<-entered
	manager.InvalidateOwnerAccounts(row.ID, "openai")
	close(release)
	<-firstDone

	refreshed, err := manager.OwnerAccounts(context.Background(), row.ID, row.ConfigurationVersion, "openai")
	if err != nil {
		t.Fatalf("read after invalidation: %v", err)
	}
	if loads.Load() != 2 {
		t.Fatalf("directory loads = %d, want the pre-invalidation load not to be cached", loads.Load())
	}
	if len(refreshed) != 1 || refreshed[0].ID != 2 {
		t.Fatalf("refreshed directory = %+v, want the post-invalidation load", refreshed)
	}
}
