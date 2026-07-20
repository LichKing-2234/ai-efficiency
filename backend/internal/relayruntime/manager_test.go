package relayruntime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const testEncryptionKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type taggedProvider struct {
	relay.Provider
	tag string
}

type sharedInvalidationBus struct {
	mu          sync.Mutex
	subscribers []func(InvalidationEvent)
	events      []InvalidationEvent
	publishErr  error
}

func (b *sharedInvalidationBus) Publish(_ context.Context, event InvalidationEvent) error {
	b.mu.Lock()
	b.events = append(b.events, event)
	subscribers := append([]func(InvalidationEvent){}, b.subscribers...)
	err := b.publishErr
	b.mu.Unlock()
	if err != nil {
		return err
	}
	for _, subscriber := range subscribers {
		subscriber(event)
	}
	return nil
}

func (b *sharedInvalidationBus) Subscribe(ctx context.Context, handler func(InvalidationEvent)) error {
	b.mu.Lock()
	b.subscribers = append(b.subscribers, handler)
	b.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}

func (b *sharedInvalidationBus) eventCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.events)
}

func (b *sharedInvalidationBus) subscriberCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subscribers)
}

func waitForSubscribers(t *testing.T, bus *sharedInvalidationBus, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if bus.subscriberCount() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("subscriber count = %d, want %d", bus.subscriberCount(), want)
}

func createProviderRow(t *testing.T, client *ent.Client) *ent.RelayProvider {
	t.Helper()
	encrypted, err := pkg.Encrypt("test-admin-key", testEncryptionKey)
	if err != nil {
		t.Fatalf("encrypt admin key: %v", err)
	}
	row, err := client.RelayProvider.Create().
		SetName("relay-primary").
		SetDisplayName("Relay Primary").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey(encrypted).
		SetDefaultModel("model-v1").
		SetIsPrimary(true).
		SetEnabled(true).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create provider row: %v", err)
	}
	return row
}

func TestRelayUserTrendRuntimeProvidersUsePersistedIdentity(t *testing.T) {
	client := testdb.Open(t)
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	store := readcache.NewRedisStore(redisClient)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/dashboard/trend" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    []map[string]any{{"date": "2026-07-20", "actual_cost": 1.01, "total_tokens": 101}},
		})
	}))
	t.Cleanup(upstream.Close)

	createRow := func(name string, version int64) *ent.RelayProvider {
		encrypted, err := pkg.Encrypt("test-admin-key", testEncryptionKey)
		if err != nil {
			t.Fatalf("encrypt admin key: %v", err)
		}
		row, err := client.RelayProvider.Create().
			SetName(name).
			SetDisplayName(name).
			SetBaseURL(upstream.URL).
			SetAdminAPIKey(encrypted).
			SetDefaultModel("test-model").
			SetConfigurationVersion(version).
			SetEnabled(true).
			Save(context.Background())
		if err != nil {
			t.Fatalf("create provider row: %v", err)
		}
		return row
	}
	firstRow := createRow("relay-first", 3)
	secondRow := createRow("relay-second", 5)

	manager, err := NewManager(client, testEncryptionKey, zap.NewNop(), Options{
		Factory: func(row *ent.RelayProvider, adminAPIKey string) (relay.Provider, error) {
			return relay.NewSub2apiProviderWithOptions(
				upstream.Client(), row.BaseURL, adminAPIKey, row.DefaultModel, zap.NewNop(), relay.Sub2apiProviderOptions{
					TeamTrendStore: store, CacheNamespace: "runtime-test", ProviderID: row.ID,
					ProviderConfigurationVersion: row.ConfigurationVersion,
				},
			)
		},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	params := relay.TeamMemberTrendParams{
		StartDate: "2026-07-01", EndDate: "2026-07-20", Granularity: "day", Timezone: "Asia/Shanghai",
	}
	for _, row := range []*ent.RelayProvider{firstRow, secondRow} {
		provider, err := manager.Resolve(context.Background(), row.ID)
		if err != nil {
			t.Fatalf("resolve provider %d: %v", row.ID, err)
		}
		trendProvider, ok := provider.(relay.TeamMemberTrendProvider)
		if !ok {
			t.Fatalf("provider %d does not implement TeamMemberTrendProvider", row.ID)
		}
		if _, err := trendProvider.GetUsageTrendForUsers(context.Background(), []int64{101}, params); err != nil {
			t.Fatalf("provider %d trend: %v", row.ID, err)
		}
	}

	keys := redisServer.Keys()
	sort.Strings(keys)
	if len(keys) != 2 || keys[0] == keys[1] {
		t.Fatalf("shared Redis keys = %v, want two provider/version-isolated values", keys)
	}
	gotIdentities := make([][2]int64, 0, len(keys))
	for _, key := range keys {
		var envelope struct {
			ProviderID      int   `json:"provider_id"`
			ProviderVersion int64 `json:"provider_configuration_version"`
		}
		raw, err := redisServer.Get(key)
		if err != nil {
			t.Fatalf("read Redis value %q: %v", key, err)
		}
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
			t.Fatalf("decode Redis value %q: %v", key, err)
		}
		gotIdentities = append(gotIdentities, [2]int64{int64(envelope.ProviderID), envelope.ProviderVersion})
	}
	sort.Slice(gotIdentities, func(i, j int) bool { return gotIdentities[i][0] < gotIdentities[j][0] })
	wantIdentities := [][2]int64{{int64(firstRow.ID), 3}, {int64(secondRow.ID), 5}}
	if len(gotIdentities) != len(wantIdentities) || gotIdentities[0] != wantIdentities[0] || gotIdentities[1] != wantIdentities[1] {
		t.Fatalf("Redis provider identities = %v, want %v", gotIdentities, wantIdentities)
	}
}

func TestRelayRuntimeReusesVersionedClientUntilMaximumLifetime(t *testing.T) {
	client := testdb.Open(t)
	row := createProviderRow(t, client)
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	created := 0
	manager, err := NewManager(client, testEncryptionKey, zap.NewNop(), Options{
		ClientTTL: 5 * time.Minute,
		Now:       func() time.Time { return now },
		Factory: func(row *ent.RelayProvider, adminKey string) (relay.Provider, error) {
			created++
			if adminKey != "test-admin-key" {
				t.Fatalf("decrypted admin key = %q", adminKey)
			}
			return &taggedProvider{tag: row.BaseURL}, nil
		},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	first, err := manager.Resolve(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	second, err := manager.Resolve(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if first != second || created != 1 {
		t.Fatalf("same-version clients = %p/%p, creates = %d", first, second, created)
	}

	now = now.Add(5*time.Minute + time.Nanosecond)
	third, err := manager.Resolve(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("resolve after ttl: %v", err)
	}
	if third == first || created != 2 {
		t.Fatalf("expired client was reused: first=%p third=%p creates=%d", first, third, created)
	}
}

func TestRelayRuntimeCreatesUncachedUserScopedProvidersThroughConfiguredFactory(t *testing.T) {
	client := testdb.Open(t)
	row := createProviderRow(t, client)
	created := 0
	manager, err := NewManager(client, testEncryptionKey, zap.NewNop(), Options{
		Factory: func(factoryRow *ent.RelayProvider, apiKey string) (relay.Provider, error) {
			created++
			return &taggedProvider{tag: apiKey + "/" + factoryRow.DefaultModel}, nil
		},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	first, err := manager.NewUserScopedProvider(row, "user-key", "model-user")
	if err != nil {
		t.Fatalf("first user-scoped provider: %v", err)
	}
	second, err := manager.NewUserScopedProvider(row, "user-key", "model-user")
	if err != nil {
		t.Fatalf("second user-scoped provider: %v", err)
	}

	if first == second || created != 2 {
		t.Fatalf("user-scoped provider was cached: first=%p second=%p creates=%d", first, second, created)
	}
	if got := first.(*taggedProvider).tag; got != "user-key/model-user" {
		t.Fatalf("user-scoped factory inputs = %q", got)
	}
}

func TestRelayRuntimeRevalidatesPersistedVersionAfterMissedInvalidation(t *testing.T) {
	client := testdb.Open(t)
	row := createProviderRow(t, client)
	created := 0
	manager, err := NewManager(client, testEncryptionKey, zap.NewNop(), Options{
		Factory: func(row *ent.RelayProvider, _ string) (relay.Provider, error) {
			created++
			return &taggedProvider{tag: row.BaseURL}, nil
		},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	first, err := manager.Resolve(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	updated, err := client.RelayProvider.UpdateOneID(row.ID).
		SetBaseURL("https://relay-v2.example.com").
		AddConfigurationVersion(1).
		Save(context.Background())
	if err != nil {
		t.Fatalf("update provider row: %v", err)
	}
	second, err := manager.Resolve(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("resolve updated row: %v", err)
	}
	if first == second || created != 2 {
		t.Fatalf("new version reused old client: first=%p second=%p creates=%d", first, second, created)
	}
	if got := second.(*taggedProvider).tag; got != updated.BaseURL {
		t.Fatalf("updated provider tag = %q, want %q", got, updated.BaseURL)
	}
}

func TestRelayRuntimeInvalidationEvictsRemoteAndLocalClients(t *testing.T) {
	client := testdb.Open(t)
	row := createProviderRow(t, client)
	bus := &sharedInvalidationBus{}
	newManager := func() *Manager {
		manager, err := NewManager(client, testEncryptionKey, zap.NewNop(), Options{
			Bus: bus,
			Factory: func(_ *ent.RelayProvider, _ string) (relay.Provider, error) {
				return &taggedProvider{tag: time.Now().String()}, nil
			},
		})
		if err != nil {
			t.Fatalf("new manager: %v", err)
		}
		return manager
	}
	left := newManager()
	right := newManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	left.Start(ctx)
	right.Start(ctx)
	waitForSubscribers(t, bus, 2)

	leftBefore, err := left.ResolveEntity(row)
	if err != nil {
		t.Fatalf("left resolve: %v", err)
	}
	rightBefore, err := right.ResolveEntity(row)
	if err != nil {
		t.Fatalf("right resolve: %v", err)
	}
	if err := left.Invalidate(context.Background(), row.ID, row.ConfigurationVersion); err != nil {
		t.Fatalf("publish invalidation: %v", err)
	}
	leftAfter, err := left.ResolveEntity(row)
	if err != nil {
		t.Fatalf("left resolve after invalidation: %v", err)
	}
	rightAfter, err := right.ResolveEntity(row)
	if err != nil {
		t.Fatalf("right resolve after invalidation: %v", err)
	}
	if leftBefore == leftAfter || rightBefore == rightAfter {
		t.Fatalf("invalidation did not evict clients: left=%p/%p right=%p/%p", leftBefore, leftAfter, rightBefore, rightAfter)
	}
}

func TestRelayRuntimeEvictsLocallyWhenPublishFails(t *testing.T) {
	client := testdb.Open(t)
	row := createProviderRow(t, client)
	bus := &sharedInvalidationBus{publishErr: errors.New("redis unavailable")}
	manager, err := NewManager(client, testEncryptionKey, zap.NewNop(), Options{
		Bus: bus,
		Factory: func(_ *ent.RelayProvider, _ string) (relay.Provider, error) {
			return &taggedProvider{}, nil
		},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	before, err := manager.ResolveEntity(row)
	if err != nil {
		t.Fatalf("resolve before invalidation: %v", err)
	}
	updated, err := client.RelayProvider.UpdateOneID(row.ID).AddConfigurationVersion(1).Save(context.Background())
	if err != nil {
		t.Fatalf("update provider version: %v", err)
	}
	if err := manager.Invalidate(context.Background(), row.ID, updated.ConfigurationVersion); err == nil {
		t.Fatal("expected publish error")
	}
	after, err := manager.Resolve(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("resolve after invalidation: %v", err)
	}
	if before == after {
		t.Fatal("local client survived failed publish")
	}
}
