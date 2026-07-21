package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/config"
	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/teamusage"
	"github.com/ai-efficiency/backend/internal/telemetry"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	redis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestInitializeRepoInventoryWiresRevisionAndCache(t *testing.T) {
	client := testdb.Open(t)
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })

	cache, revisions, err := initializeRepoInventory(context.Background(), client, redisClient, "test", nil)
	if err != nil {
		t.Fatalf("initializeRepoInventory() error = %v", err)
	}
	revision, err := revisions.Current(context.Background())
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if parsed, err := uuid.Parse(revision); err != nil || parsed.String() != revision {
		t.Fatalf("revision = %q, want canonical UUID", revision)
	}

	inventory, err := cache.GetOrLoad(context.Background(), func(context.Context) ([]repo.InventoryProviderSummary, error) {
		return []repo.InventoryProviderSummary{}, nil
	})
	if err != nil || inventory == nil || len(inventory) != 0 {
		t.Fatalf("GetOrLoad() inventory = %#v, error = %v, want empty cached inventory", inventory, err)
	}
	wantKey := "ae:test:repos:inventory:v1:rev:" + revision
	if !server.Exists(wantKey) {
		t.Fatalf("Redis key %q missing after cache load", wantKey)
	}
}

func TestTeamUsagePrewarmRuntimeFailOpenConstructionBoundary(t *testing.T) {
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	store := readcache.NewRedisStore(redisClient)
	supported := &serverPrewarmProvider{}
	unsupported := &unsupportedServerPrewarmProvider{}

	tests := []struct {
		name     string
		cfg      config.TeamUsagePrewarmConfig
		store    readcache.BatchStore
		resolver teamusage.PrimaryProviderBindingResolver
	}{
		{name: "disabled", cfg: config.TeamUsagePrewarmConfig{Enabled: false, Timezones: []string{"UTC"}}, store: store, resolver: staticServerPrewarmResolver{provider: supported}},
		{name: "empty allowlist", cfg: config.TeamUsagePrewarmConfig{Enabled: true}, store: store, resolver: staticServerPrewarmResolver{provider: supported}},
		{name: "Redis unavailable", cfg: config.TeamUsagePrewarmConfig{Enabled: true, Timezones: []string{"UTC"}}, resolver: staticServerPrewarmResolver{provider: supported}},
		{name: "provider unsupported", cfg: config.TeamUsagePrewarmConfig{Enabled: true, Timezones: []string{"UTC"}}, store: store, resolver: staticServerPrewarmResolver{provider: unsupported}},
		{name: "provider resolution unavailable", cfg: config.TeamUsagePrewarmConfig{Enabled: true, Timezones: []string{"UTC"}}, store: store, resolver: staticServerPrewarmResolver{err: errors.New("provider unavailable")}},
		{name: "invalid allowlist", cfg: config.TeamUsagePrewarmConfig{Enabled: true, Timezones: []string{"not-a-real-timezone"}}, store: store, resolver: staticServerPrewarmResolver{provider: supported}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zap.InfoLevel)
			logger := zap.New(core)
			runtime := initializeTeamUsagePrewarm(
				context.Background(), tt.cfg, "test", tt.store, tt.resolver,
				telemetry.NewMetrics("test-release"), logger,
			)
			if runtime != nil {
				t.Fatalf("initializeTeamUsagePrewarm() = %#v, want disabled fail-open runtime", runtime)
			}
			for _, entry := range logs.All() {
				if entry.ContextMap()["timezones"] != nil || entry.ContextMap()["error"] != nil {
					t.Fatalf("fail-open log includes raw configuration/error: %#v", entry.ContextMap())
				}
			}
		})
	}
}

func TestTeamUsagePrewarmRuntimeSharesLimiterAndStartsWithoutBlocking(t *testing.T) {
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	store := readcache.NewRedisStore(redisClient)
	provider := &serverPrewarmProvider{
		directoryEntered: make(chan struct{}, 1),
		directoryRelease: make(chan struct{}),
	}
	runtime := initializeTeamUsagePrewarm(
		context.Background(),
		config.TeamUsagePrewarmConfig{Enabled: true, Timezones: []string{"UTC"}},
		"test",
		store,
		staticServerPrewarmResolver{provider: provider},
		telemetry.NewMetrics("test-release"),
		zap.NewNop(),
	)
	if runtime == nil || runtime.prewarmer == nil || runtime.reader == nil {
		t.Fatalf("initializeTeamUsagePrewarm() = %#v, want reader and prewarmer", runtime)
	}
	if got, want := runtime.reader.SourceCallLimiter(), runtime.prewarmer.SourceCallLimiter(); got != want {
		t.Fatalf("reader limiter = %p, prewarmer limiter = %p, want exact shared instance", got, want)
	}

	startedAt := time.Now()
	runtime.Start(context.Background())
	if elapsed := time.Since(startedAt); elapsed > 100*time.Millisecond {
		t.Fatalf("Start() blocked for %s, want asynchronous HTTP startup", elapsed)
	}
	select {
	case <-provider.directoryEntered:
	case <-time.After(time.Second):
		t.Fatal("prewarmer did not start after dependencies were constructed")
	}

	stopped := make(chan struct{})
	go func() {
		runtime.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop() did not cancel and join active prewarm work")
	}
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("Redis closed before prewarmer stopped: %v", err)
	}
	if err := redisClient.Close(); err != nil {
		t.Fatalf("close Redis: %v", err)
	}
}

func TestTeamUsagePrewarmShutdownStopsBeforeRedisClose(t *testing.T) {
	var order []string
	prewarm := stoppingRecorder{stop: func() { order = append(order, "prewarm") }}
	err := closeTeamUsagePrewarmResources(prewarm, func() error {
		order = append(order, "redis")
		return nil
	})
	if err != nil {
		t.Fatalf("closeTeamUsagePrewarmResources() error = %v", err)
	}
	if want := []string{"prewarm", "redis"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("shutdown order = %v, want %v", order, want)
	}
}

type stoppingRecorder struct {
	stop func()
}

func (r stoppingRecorder) Stop() {
	r.stop()
}

type staticServerPrewarmResolver struct {
	provider relay.Provider
	err      error
}

func (r staticServerPrewarmResolver) ResolvePrimaryProviderBinding(context.Context) (teamusage.ProviderBinding, error) {
	if r.err != nil {
		return teamusage.ProviderBinding{}, r.err
	}
	return teamusage.ProviderBinding{ProviderID: 1, ProviderVersion: 1, Provider: r.provider}, nil
}

type unsupportedServerPrewarmProvider struct {
	relay.Provider
}

type serverPrewarmProvider struct {
	relay.Provider
	directoryEntered chan struct{}
	directoryRelease chan struct{}
}

func (p *serverPrewarmProvider) GetProviderUserIDs(ctx context.Context) (relay.ProviderDirectoryResult, error) {
	if p.directoryEntered != nil {
		select {
		case p.directoryEntered <- struct{}{}:
		default:
		}
	}
	if p.directoryRelease != nil {
		select {
		case <-ctx.Done():
			return relay.ProviderDirectoryResult{}, ctx.Err()
		case <-p.directoryRelease:
		}
	}
	return relay.ProviderDirectoryResult{UserIDs: []int64{101}, PageCount: 1}, nil
}

func (*serverPrewarmProvider) GetProviderCurrentUsageStats(context.Context, []int64) (relay.ProviderCurrentStatsResult, error) {
	return relay.ProviderCurrentStatsResult{Stats: map[int64]relay.TeamUserUsageStats{101: {}}}, nil
}

func (*serverPrewarmProvider) GetProviderUsageTrend(context.Context, relay.TeamMemberTrendParams, int) (relay.ProviderWideTrendResult, error) {
	return relay.ProviderWideTrendResult{}, nil
}
