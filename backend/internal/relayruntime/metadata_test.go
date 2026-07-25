package relayruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type metadataRelayProvider struct {
	relay.Provider
	mu                  sync.Mutex
	allowedGroupIDs     []int64
	platformGroups      []relay.Group
	getUserCalls        int
	platformGroupsCalls int
}

type metadataFaultStore struct {
	mu        sync.Mutex
	value     []byte
	getErr    error
	setErr    error
	leaseErr  error
	ttlErr    error
	leaseHeld bool
}

func (s *metadataFaultStore) Get(context.Context, string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.value == nil {
		return nil, readcache.ErrMiss
	}
	return append([]byte(nil), s.value...), nil
}

func (s *metadataFaultStore) Set(_ context.Context, _ string, value []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.setErr != nil {
		return s.setErr
	}
	s.value = append([]byte(nil), value...)
	return nil
}

func (s *metadataFaultStore) TryAcquireLease(context.Context, string, string, time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leaseErr != nil {
		return false, s.leaseErr
	}
	if s.leaseHeld {
		s.leaseHeld = false
		return false, nil
	}
	return true, nil
}

func (s *metadataFaultStore) LeaseTTL(context.Context, string) (time.Duration, error) {
	if s.ttlErr != nil {
		return 0, s.ttlErr
	}
	return 0, readcache.ErrMiss
}

func (s *metadataFaultStore) ReleaseLease(context.Context, string, string) (bool, error) {
	return true, nil
}

func (p *metadataRelayProvider) GetUser(_ context.Context, userID int64) (*relay.User, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.getUserCalls++
	return &relay.User{ID: userID, AllowedGroupIDs: append([]int64(nil), p.allowedGroupIDs...)}, nil
}

func (p *metadataRelayProvider) ListPlatformGroups(_ context.Context) ([]relay.Group, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.platformGroupsCalls++
	return append([]relay.Group(nil), p.platformGroups...), nil
}

func (p *metadataRelayProvider) setAllowedGroupIDs(ids ...int64) {
	p.mu.Lock()
	p.allowedGroupIDs = append([]int64(nil), ids...)
	p.mu.Unlock()
}

func (p *metadataRelayProvider) callCounts() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.getUserCalls, p.platformGroupsCalls
}

func metadataManager(t *testing.T, client *ent.Client, store readcache.Store, provider relay.Provider) *Manager {
	t.Helper()
	manager, err := NewManager(client, testEncryptionKey, zap.NewNop(), Options{
		Namespace:   "test",
		Store:       store,
		MetadataTTL: 5 * time.Minute,
		Factory: func(_ *ent.RelayProvider, _ string) (relay.Provider, error) {
			return provider, nil
		},
	})
	if err != nil {
		t.Fatalf("new metadata manager: %v", err)
	}
	return manager
}

func TestProviderMetadataCachesGroupsAcrossManagersButReadsMembershipFresh(t *testing.T) {
	client := testdbClient(t)
	row := createProviderRow(t, client)
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { redisClient.Close() })
	store := readcache.NewRedisStore(redisClient)
	dailyLimit := 25.0
	rateMultiplier := 1.5
	leftProvider := &metadataRelayProvider{
		allowedGroupIDs: []int64{5},
		platformGroups: []relay.Group{{
			ID: 5, Name: "Group Alpha", Platform: "openai",
			DailyLimitUSD: &dailyLimit, RateMultiplier: &rateMultiplier,
		}},
	}
	rightProvider := &metadataRelayProvider{
		allowedGroupIDs: []int64{5},
		platformGroups:  leftProvider.platformGroups,
	}
	left := metadataManager(t, client, store, leftProvider)
	right := metadataManager(t, client, store, rightProvider)

	first, err := left.ListAllowedGroupsForUser(context.Background(), row.ID, row.ConfigurationVersion, 42)
	if err != nil {
		t.Fatalf("left groups: %v", err)
	}
	if len(first) != 1 || first[0].Name != "Group Alpha" || first[0].DailyLimitUSD != nil || first[0].RateMultiplier != nil {
		t.Fatalf("sanitized groups = %#v", first)
	}
	first[0].Name = "mutated"
	second, err := right.ListAllowedGroupsForUser(context.Background(), row.ID, row.ConfigurationVersion, 84)
	if err != nil {
		t.Fatalf("right groups: %v", err)
	}
	if len(second) != 1 || second[0].Name != "Group Alpha" {
		t.Fatalf("cached groups = %#v", second)
	}
	leftUsers, leftGroups := leftProvider.callCounts()
	rightUsers, rightGroups := rightProvider.callCounts()
	if leftUsers != 1 || rightUsers != 1 || leftGroups != 1 || rightGroups != 0 {
		t.Fatalf("origin counts left=(users:%d groups:%d) right=(users:%d groups:%d)", leftUsers, leftGroups, rightUsers, rightGroups)
	}

	rightProvider.setAllowedGroupIDs()
	revoked, err := right.ListAllowedGroupsForUser(context.Background(), row.ID, row.ConfigurationVersion, 84)
	if err != nil {
		t.Fatalf("revoked groups: %v", err)
	}
	if len(revoked) != 0 {
		t.Fatalf("revoked membership reused cached groups: %#v", revoked)
	}
	_, rightGroups = rightProvider.callCounts()
	if rightGroups != 0 {
		t.Fatalf("warm metadata refreshed global groups %d times", rightGroups)
	}

	for _, key := range server.Keys() {
		value, err := server.Get(key)
		if err != nil {
			t.Fatalf("read Redis key %q: %v", key, err)
		}
		combined := key + value
		for _, forbidden := range []string{"test-admin-key", "daily_limit", "rate_multiplier", "password", "api_key"} {
			if strings.Contains(combined, forbidden) {
				t.Fatalf("Redis entry contains %q: %s", forbidden, combined)
			}
		}
	}
}

func TestProviderMetadataModelsUseFiveMinuteVersionedPlatformGroupKeys(t *testing.T) {
	client := testdbClient(t)
	row := createProviderRow(t, client)
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { redisClient.Close() })
	manager := metadataManager(t, client, readcache.NewRedisStore(redisClient), &taggedProvider{})
	var loads atomic.Int32
	loader := func(_ context.Context) ([]relay.ModelOption, error) {
		loads.Add(1)
		return []relay.ModelOption{{ID: "model-alpha", DisplayName: "Model Alpha"}}, nil
	}

	first, err := manager.Models(context.Background(), row, "openai", "5", loader)
	if err != nil {
		t.Fatalf("first models: %v", err)
	}
	first[0].DisplayName = "mutated"
	second, err := manager.Models(context.Background(), row, "openai", "5", loader)
	if err != nil {
		t.Fatalf("cached models: %v", err)
	}
	if loads.Load() != 1 || second[0].DisplayName != "Model Alpha" {
		t.Fatalf("cached models = %#v, loads=%d", second, loads.Load())
	}
	if _, err := manager.Models(context.Background(), row, "gemini", "5", loader); err != nil {
		t.Fatalf("platform-isolated models: %v", err)
	}
	if _, err := manager.Models(context.Background(), row, "openai", "6", loader); err != nil {
		t.Fatalf("group-isolated models: %v", err)
	}
	updated, err := client.RelayProvider.UpdateOneID(row.ID).AddConfigurationVersion(1).Save(context.Background())
	if err != nil {
		t.Fatalf("update provider version: %v", err)
	}
	if _, err := manager.Models(context.Background(), updated, "openai", "5", loader); err != nil {
		t.Fatalf("version-isolated models: %v", err)
	}
	if loads.Load() != 4 {
		t.Fatalf("isolated model loads = %d, want 4", loads.Load())
	}

	server.FastForward(5*time.Minute + time.Second)
	if _, err := manager.Models(context.Background(), updated, "openai", "5", loader); err != nil {
		t.Fatalf("expired models: %v", err)
	}
	if loads.Load() != 5 {
		t.Fatalf("post-expiry model loads = %d, want 5", loads.Load())
	}
}

func TestProviderMetadataModelsRejectStaleProviderRow(t *testing.T) {
	client := testdbClient(t)
	row := createProviderRow(t, client)
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { redisClient.Close() })
	manager := metadataManager(t, client, readcache.NewRedisStore(redisClient), &taggedProvider{})
	var loads atomic.Int32
	loader := func(_ context.Context) ([]relay.ModelOption, error) {
		loads.Add(1)
		return []relay.ModelOption{{ID: "model-alpha"}}, nil
	}

	if _, err := manager.Models(context.Background(), row, "openai", "5", loader); err != nil {
		t.Fatalf("warm models: %v", err)
	}
	if _, err := client.RelayProvider.UpdateOneID(row.ID).AddConfigurationVersion(1).Save(context.Background()); err != nil {
		t.Fatalf("update provider version: %v", err)
	}
	if _, err := manager.Models(context.Background(), row, "openai", "5", loader); !errors.Is(err, ErrStaleProviderConfiguration) {
		t.Fatalf("stale-row Models error = %v, want ErrStaleProviderConfiguration", err)
	}
	if loads.Load() != 1 {
		t.Fatalf("stale-row model loads = %d, want 1", loads.Load())
	}
}

func TestProviderMetadataTTLJittersBelowFiveMinuteMaximum(t *testing.T) {
	client := testdbClient(t)
	row := createProviderRow(t, client)
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { redisClient.Close() })
	manager, err := NewManager(client, testEncryptionKey, zap.NewNop(), Options{
		Namespace:   "test",
		Store:       readcache.NewRedisStore(redisClient),
		MetadataTTL: 10 * time.Minute,
		Factory: func(_ *ent.RelayProvider, _ string) (relay.Provider, error) {
			return &taggedProvider{}, nil
		},
	})
	if err != nil {
		t.Fatalf("new metadata manager: %v", err)
	}

	for _, groupID := range []string{"5", "6", "7", "8", "9", "10", "11", "12"} {
		if _, err := manager.Models(context.Background(), row, "openai", groupID, func(_ context.Context) ([]relay.ModelOption, error) {
			return []relay.ModelOption{{ID: "model-alpha"}}, nil
		}); err != nil {
			t.Fatalf("models for group %s: %v", groupID, err)
		}
	}
	keys := server.Keys()
	if len(keys) != 8 {
		t.Fatalf("Redis keys = %v, want eight metadata keys", keys)
	}
	ttls := make(map[time.Duration]struct{}, len(keys))
	for _, key := range keys {
		ttl := server.TTL(key)
		if ttl < 4*time.Minute || ttl > 4*time.Minute+30*time.Second {
			t.Fatalf("metadata TTL = %s, want 10-20%% jitter below 5m", ttl)
		}
		ttls[ttl] = struct{}{}
	}
	if len(ttls) < 2 {
		t.Fatalf("metadata TTLs are synchronized: %v", ttls)
	}
}

func TestProviderMetadataCollapsesConcurrentModelsAcrossManagers(t *testing.T) {
	client := testdbClient(t)
	row := createProviderRow(t, client)
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { redisClient.Close() })
	store := readcache.NewRedisStore(redisClient)
	left := metadataManager(t, client, store, &taggedProvider{})
	right := metadataManager(t, client, store, &taggedProvider{})

	var loads atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	loader := func(ctx context.Context) ([]relay.ModelOption, error) {
		loads.Add(1)
		select {
		case <-started:
		default:
			close(started)
		}
		select {
		case <-release:
			return []relay.ModelOption{{ID: "model-alpha"}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	results := make(chan error, 2)
	go func() {
		_, err := left.Models(context.Background(), row, "openai", "5", loader)
		results <- err
	}()
	<-started
	go func() {
		_, err := right.Models(context.Background(), row, "openai", "5", loader)
		results <- err
	}()
	time.Sleep(50 * time.Millisecond)
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("concurrent models: %v", err)
		}
	}
	if loads.Load() != 1 {
		t.Fatalf("cross-manager model loads = %d, want 1", loads.Load())
	}
}

func TestProviderMetadataFallsBackWhenForeignLeaseOutlivesRefreshBudget(t *testing.T) {
	client := testdbClient(t)
	row := createProviderRow(t, client)
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { redisClient.Close() })
	store := readcache.NewRedisStore(redisClient)
	manager, err := NewManager(client, testEncryptionKey, zap.NewNop(), Options{
		Namespace:      "test",
		Store:          store,
		MetadataTTL:    5 * time.Minute,
		RefreshTimeout: 300 * time.Millisecond,
		LeaseTTL:       time.Minute,
		PollInterval:   10 * time.Millisecond,
		Factory: func(_ *ent.RelayProvider, _ string) (relay.Provider, error) {
			return &taggedProvider{}, nil
		},
	})
	if err != nil {
		t.Fatalf("new metadata manager: %v", err)
	}
	key := manager.metadataKey("models", row.ID, row.ConfigurationVersion, "openai", "5")
	if acquired, err := store.TryAcquireLease(context.Background(), key+":lease", "foreign-holder", time.Minute); err != nil || !acquired {
		t.Fatalf("seed foreign lease = %v, %v", acquired, err)
	}
	var loads atomic.Int32

	models, err := manager.Models(context.Background(), row, "openai", "5", func(context.Context) ([]relay.ModelOption, error) {
		loads.Add(1)
		return []relay.ModelOption{{ID: "model-alpha"}}, nil
	})
	if err != nil {
		t.Fatalf("Models() with stalled foreign lease error = %v", err)
	}
	if loads.Load() != 1 || len(models) != 1 || models[0].ID != "model-alpha" {
		t.Fatalf("Models() = %#v, loads=%d", models, loads.Load())
	}
}

func TestProviderMetadataCollapsesConcurrentModelsWithoutRedis(t *testing.T) {
	const callers = 8

	client := testdbClient(t)
	row := createProviderRow(t, client)
	manager := metadataManager(t, client, nil, &taggedProvider{})
	var versionReads atomic.Int32
	client.RelayProvider.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, query ent.Query) (ent.Value, error) {
			value, err := next.Query(ctx, query)
			versionReads.Add(1)
			return value, err
		})
	}))

	var loads atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	loader := func(ctx context.Context) ([]relay.ModelOption, error) {
		loads.Add(1)
		select {
		case <-started:
		default:
			close(started)
		}
		select {
		case <-release:
			return []relay.ModelOption{{ID: "model-alpha"}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	results := make(chan error, callers)
	var ready sync.WaitGroup
	ready.Add(callers)
	start := make(chan struct{})
	for range callers {
		go func() {
			ready.Done()
			<-start
			_, err := manager.Models(context.Background(), row, "openai", "5", loader)
			results <- err
		}()
	}
	ready.Wait()
	close(start)
	<-started
	deadline := time.Now().Add(5 * time.Second)
	for versionReads.Load() != callers {
		if time.Now().After(deadline) {
			t.Fatalf("provider version reads = %d, want %d", versionReads.Load(), callers)
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	for range callers {
		if err := <-results; err != nil {
			t.Fatalf("concurrent models: %v", err)
		}
	}
	if loads.Load() != 1 {
		t.Fatalf("no-Redis model loads = %d, want 1", loads.Load())
	}
}

func TestProviderMetadataCollapsesConcurrentModelsAndFallsBackWhenRedisFails(t *testing.T) {
	client := testdbClient(t)
	row := createProviderRow(t, client)
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { redisClient.Close() })
	manager := metadataManager(t, client, readcache.NewRedisStore(redisClient), &taggedProvider{})
	var loads atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	loader := func(_ context.Context) ([]relay.ModelOption, error) {
		loads.Add(1)
		once.Do(func() { close(started) })
		<-release
		return []relay.ModelOption{{ID: "model-alpha"}}, nil
	}

	const callers = 8
	errCh := make(chan error, callers)
	for range callers {
		go func() {
			_, err := manager.Models(context.Background(), row, "openai", "5", loader)
			errCh <- err
		}()
	}
	<-started
	close(release)
	for range callers {
		if err := <-errCh; err != nil {
			t.Fatalf("concurrent models: %v", err)
		}
	}
	if loads.Load() != 1 {
		t.Fatalf("concurrent model loads = %d, want 1", loads.Load())
	}

	server.Close()
	if _, err := manager.Models(context.Background(), row, "openai", "redis-fallback", func(_ context.Context) ([]relay.ModelOption, error) {
		loads.Add(1)
		return []relay.ModelOption{{ID: "fallback-model"}}, nil
	}); err != nil {
		t.Fatalf("Redis fallback models: %v", err)
	}
	if loads.Load() != 2 {
		t.Fatalf("Redis fallback loads = %d, want 2", loads.Load())
	}
}

func TestProviderMetadataFallsBackAcrossRedisFailureModes(t *testing.T) {
	tests := []struct {
		name  string
		store *metadataFaultStore
	}{
		{name: "malformed value", store: &metadataFaultStore{value: []byte(`{"schema_version":`)}},
		{name: "read failure", store: &metadataFaultStore{getErr: errors.New("Redis read unavailable")}},
		{name: "lease failure", store: &metadataFaultStore{leaseErr: errors.New("Redis lease unavailable")}},
		{name: "write failure", store: &metadataFaultStore{setErr: errors.New("Redis write unavailable")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := testdbClient(t)
			row := createProviderRow(t, client)
			manager := metadataManager(t, client, tt.store, &taggedProvider{})
			var loads atomic.Int32

			models, err := manager.Models(context.Background(), row, "openai", "5", func(context.Context) ([]relay.ModelOption, error) {
				loads.Add(1)
				return []relay.ModelOption{{ID: "model-alpha", DisplayName: "Model Alpha"}}, nil
			})
			if err != nil {
				t.Fatalf("Models() error = %v", err)
			}
			if loads.Load() != 1 || len(models) != 1 || models[0].ID != "model-alpha" {
				t.Fatalf("Models() = %#v, loads=%d", models, loads.Load())
			}
			if tt.name == "malformed value" {
				tt.store.mu.Lock()
				repaired := append([]byte(nil), tt.store.value...)
				tt.store.mu.Unlock()
				if !validModelEnvelope(repaired) {
					t.Fatalf("malformed cache value was not repaired: %s", repaired)
				}
			}
		})
	}
}

func testdbClient(t *testing.T) *ent.Client {
	t.Helper()
	return testdb.Open(t)
}

func TestProviderMetadataEnvelopeDoesNotSerializeUnknownFields(t *testing.T) {
	raw, err := json.Marshal(metadataEnvelope[relay.ModelOption]{
		SchemaVersion: 1,
		Items:         []relay.ModelOption{{ID: "model-alpha", DisplayName: "Model Alpha"}},
	})
	if err != nil {
		t.Fatalf("marshal metadata envelope: %v", err)
	}
	if strings.Contains(string(raw), "key") || strings.Contains(string(raw), "user") {
		t.Fatalf("metadata envelope contains identity or key fields: %s", raw)
	}
}
