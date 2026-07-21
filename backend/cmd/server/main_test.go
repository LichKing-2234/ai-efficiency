package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
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
		store    readcache.BatchStore
		ping     func(context.Context) error
		resolver teamusage.PrimaryProviderBindingResolver
	}{
		{name: "Redis unavailable", resolver: staticServerPrewarmResolver{provider: supported}},
		{name: "Redis ping unavailable", store: store, ping: func(context.Context) error { return errors.New("Redis unavailable") }, resolver: staticServerPrewarmResolver{provider: supported}},
		{name: "provider unsupported", store: store, ping: func(context.Context) error { return nil }, resolver: staticServerPrewarmResolver{provider: unsupported}},
		{name: "provider resolution unavailable", store: store, ping: func(context.Context) error { return nil }, resolver: staticServerPrewarmResolver{err: errors.New("provider unavailable")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zap.InfoLevel)
			logger := zap.New(core)
			runtime := prepareTeamUsagePrewarm(teamUsagePrewarmRuntimeOptions{
				Config:         config.TeamUsagePrewarmConfig{Enabled: true, Timezones: []string{"UTC"}},
				RedisNamespace: "test", Store: tt.store, PingRedis: tt.ping, Resolver: tt.resolver,
				MetricsFactory: telemetry.NewMetrics("test-release").TeamUsagePrewarmRecorder,
				Logger:         logger,
			})
			if runtime == nil {
				t.Fatal("prepareTeamUsagePrewarm() = nil, want lazy fail-open runtime")
			}
			runtime.Start(context.Background())
			select {
			case <-runtime.initDone:
			case <-time.After(time.Second):
				t.Fatal("fail-open initializer did not finish")
			}
			runtime.Stop()
			if runtime.ReaderSource().Load() != nil || runtime.prewarmer != nil {
				t.Fatalf("failed initializer installed reader/prewarmer = %v/%v", runtime.ReaderSource().Load(), runtime.prewarmer)
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
	runtime := prepareTeamUsagePrewarm(teamUsagePrewarmRuntimeOptions{
		Config:         config.TeamUsagePrewarmConfig{Enabled: true, Timezones: []string{"UTC"}},
		RedisNamespace: "test", Store: store, PingRedis: func(context.Context) error { return nil },
		Resolver:       staticServerPrewarmResolver{provider: provider},
		MetricsFactory: telemetry.NewMetrics("test-release").TeamUsagePrewarmRecorder,
		Logger:         zap.NewNop(),
	})

	startedAt := time.Now()
	runtime.Start(context.Background())
	if elapsed := time.Since(startedAt); elapsed > 100*time.Millisecond {
		t.Fatalf("Start() blocked for %s, want asynchronous HTTP startup", elapsed)
	}
	select {
	case <-runtime.initDone:
	case <-time.After(time.Second):
		t.Fatal("lazy initializer did not finish")
	}
	if runtime.prewarmer == nil || runtime.reader == nil || runtime.ReaderSource().Load() != runtime.reader {
		t.Fatalf("runtime reader/prewarmer = %v/%v, want atomically installed reader", runtime.reader, runtime.prewarmer)
	}
	if got, want := runtime.reader.SourceCallLimiter(), runtime.prewarmer.SourceCallLimiter(); got != want {
		t.Fatalf("reader limiter = %p, prewarmer limiter = %p, want exact shared instance", got, want)
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

func TestTeamUsagePrewarmCompositionStartDoesNotBlockOnRedisPing(t *testing.T) {
	pingEntered := make(chan struct{})
	pingCanceled := make(chan struct{})
	var resolverCalls atomic.Int32
	runtime := prepareTeamUsagePrewarm(teamUsagePrewarmRuntimeOptions{
		Config:         config.TeamUsagePrewarmConfig{Enabled: true, Timezones: []string{"UTC"}},
		RedisNamespace: "test",
		Store:          newRecordingServerBatchStore(),
		PingRedis: func(ctx context.Context) error {
			close(pingEntered)
			<-ctx.Done()
			close(pingCanceled)
			return ctx.Err()
		},
		Resolver:       countingServerPrewarmResolver{calls: &resolverCalls, provider: &serverPrewarmProvider{}},
		MetricsFactory: telemetry.NewMetrics("test-release").TeamUsagePrewarmRecorder,
		Logger:         zap.NewNop(),
		InitTimeout:    time.Minute,
	})
	if runtime == nil || runtime.ReaderSource() == nil || runtime.ReaderSource().Load() != nil {
		t.Fatalf("prepareTeamUsagePrewarm() = %#v, want empty atomic reader source", runtime)
	}
	select {
	case <-pingEntered:
		t.Fatal("prepareTeamUsagePrewarm performed Redis work before Start")
	default:
	}

	startedAt := time.Now()
	runtime.Start(context.Background())
	if elapsed := time.Since(startedAt); elapsed > 100*time.Millisecond {
		t.Fatalf("Start() blocked on Redis for %s", elapsed)
	}
	select {
	case <-pingEntered:
	case <-time.After(time.Second):
		t.Fatal("lazy initializer did not start Redis ping")
	}
	if runtime.ReaderSource().Load() != nil || resolverCalls.Load() != 0 {
		t.Fatalf("reader/resolver before ping = %v/%d, want exact fallback and no provider work", runtime.ReaderSource().Load(), resolverCalls.Load())
	}

	stopped := make(chan struct{})
	go func() {
		runtime.Stop()
		close(stopped)
	}()
	select {
	case <-pingCanceled:
	case <-time.After(time.Second):
		t.Fatal("Stop() did not cancel in-flight Redis initializer")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop() did not join in-flight Redis initializer")
	}
	if runtime.ReaderSource().Load() != nil || runtime.prewarmer != nil {
		t.Fatalf("failed initializer installed reader/prewarmer: %v/%v", runtime.ReaderSource().Load(), runtime.prewarmer)
	}
}

func TestTeamUsagePrewarmCompositionStartDoesNotBlockOnProviderResolution(t *testing.T) {
	resolverEntered := make(chan struct{})
	resolverCanceled := make(chan struct{})
	runtime := prepareTeamUsagePrewarm(teamUsagePrewarmRuntimeOptions{
		Config:         config.TeamUsagePrewarmConfig{Enabled: true, Timezones: []string{"UTC"}},
		RedisNamespace: "test",
		Store:          newRecordingServerBatchStore(),
		PingRedis:      func(context.Context) error { return nil },
		Resolver: blockingServerPrewarmResolver{
			entered: resolverEntered, canceled: resolverCanceled,
		},
		MetricsFactory: telemetry.NewMetrics("test-release").TeamUsagePrewarmRecorder,
		Logger:         zap.NewNop(),
		InitTimeout:    time.Minute,
	})
	startedAt := time.Now()
	runtime.Start(context.Background())
	if elapsed := time.Since(startedAt); elapsed > 100*time.Millisecond {
		t.Fatalf("Start() blocked on provider resolution for %s", elapsed)
	}
	select {
	case <-resolverEntered:
	case <-time.After(time.Second):
		t.Fatal("lazy initializer did not enter provider resolution")
	}
	if runtime.ReaderSource().Load() != nil {
		t.Fatal("reader installed before provider resolution completed")
	}
	runtime.Stop()
	select {
	case <-resolverCanceled:
	case <-time.After(time.Second):
		t.Fatal("Stop() did not cancel and join provider resolution")
	}
}

func TestTeamUsagePrewarmCompositionSkipsAllWorkForDisabledEmptyAndInvalid(t *testing.T) {
	for _, cfg := range []config.TeamUsagePrewarmConfig{
		{Enabled: false, Timezones: []string{"UTC"}},
		{Enabled: true},
		{Enabled: true, Timezones: []string{"not-a-real-timezone"}},
	} {
		var pingCalls, resolverCalls atomic.Int32
		runtime := prepareTeamUsagePrewarm(teamUsagePrewarmRuntimeOptions{
			Config: cfg, RedisNamespace: "test", Store: newRecordingServerBatchStore(),
			PingRedis:      func(context.Context) error { pingCalls.Add(1); return nil },
			Resolver:       countingServerPrewarmResolver{calls: &resolverCalls, provider: &serverPrewarmProvider{}},
			MetricsFactory: telemetry.NewMetrics("test-release").TeamUsagePrewarmRecorder,
			Logger:         zap.NewNop(),
		})
		if runtime != nil {
			t.Fatalf("prepareTeamUsagePrewarm(%#v) = %#v, want nil", cfg, runtime)
		}
		if pingCalls.Load() != 0 || resolverCalls.Load() != 0 {
			t.Fatalf("disabled/empty/invalid external calls = ping %d resolver %d", pingCalls.Load(), resolverCalls.Load())
		}
	}
}

func TestTeamUsagePrewarmCompositionUnsupportedProviderInstallsNothing(t *testing.T) {
	runtime := prepareTeamUsagePrewarm(teamUsagePrewarmRuntimeOptions{
		Config:         config.TeamUsagePrewarmConfig{Enabled: true, Timezones: []string{"UTC"}},
		RedisNamespace: "test",
		Store:          newRecordingServerBatchStore(),
		PingRedis:      func(context.Context) error { return nil },
		Resolver:       staticServerPrewarmResolver{provider: &unsupportedServerPrewarmProvider{}},
		MetricsFactory: telemetry.NewMetrics("test-release").TeamUsagePrewarmRecorder,
		Logger:         zap.NewNop(),
	})
	runtime.Start(context.Background())
	select {
	case <-runtime.initDone:
	case <-time.After(time.Second):
		t.Fatal("unsupported provider initialization did not finish")
	}
	defer runtime.Stop()
	if runtime.ReaderSource().Load() != nil || runtime.prewarmer != nil {
		t.Fatalf("unsupported provider installed reader/prewarmer = %v/%v", runtime.ReaderSource().Load(), runtime.prewarmer)
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

func TestTeamUsagePrewarmBackgroundReporterUsesOnlyBoundedFields(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	reporter := newTeamUsagePrewarmReporter(zap.New(core))
	reporter.ReportPrewarmBackground(teamusage.PrewarmBackgroundEvent{
		OperationID: strings.Repeat("a", 32), ProviderID: 7, ProviderVersion: 11,
		Timezone: "UTC", Class: teamusage.PrewarmCycleStartup, Outcome: teamusage.PrewarmCycleError,
		Duration: 25 * time.Millisecond, Bytes: 128, Points: 2, Users: 1,
	})
	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("background log entries = %d, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	allowed := map[string]bool{
		"operation_id": true, "provider_id": true, "provider_version": true,
		"timezone": true, "class": true, "outcome": true, "duration_ms": true,
		"bytes": true, "points": true, "users": true,
	}
	for name := range fields {
		if !allowed[name] {
			t.Fatalf("background log exposes unbounded field %q in %#v", name, fields)
		}
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

type countingServerPrewarmResolver struct {
	calls    *atomic.Int32
	provider relay.Provider
}

func (r countingServerPrewarmResolver) ResolvePrimaryProviderBinding(context.Context) (teamusage.ProviderBinding, error) {
	r.calls.Add(1)
	return teamusage.ProviderBinding{ProviderID: 1, ProviderVersion: 1, Provider: r.provider}, nil
}

type blockingServerPrewarmResolver struct {
	entered  chan struct{}
	canceled chan struct{}
}

func (r blockingServerPrewarmResolver) ResolvePrimaryProviderBinding(ctx context.Context) (teamusage.ProviderBinding, error) {
	close(r.entered)
	<-ctx.Done()
	close(r.canceled)
	return teamusage.ProviderBinding{}, ctx.Err()
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

type recordingServerBatchStore struct{}

func newRecordingServerBatchStore() *recordingServerBatchStore { return &recordingServerBatchStore{} }

func (*recordingServerBatchStore) Get(context.Context, string) ([]byte, error) {
	return nil, readcache.ErrMiss
}
func (*recordingServerBatchStore) Set(context.Context, string, []byte, time.Duration) error {
	return nil
}
func (*recordingServerBatchStore) TryAcquireLease(context.Context, string, string, time.Duration) (bool, error) {
	return true, nil
}
func (*recordingServerBatchStore) LeaseTTL(context.Context, string) (time.Duration, error) {
	return time.Second, nil
}
func (*recordingServerBatchStore) ReleaseLease(context.Context, string, string) (bool, error) {
	return true, nil
}
func (*recordingServerBatchStore) MGet(_ context.Context, keys ...string) ([][]byte, error) {
	return make([][]byte, len(keys)), nil
}
func (*recordingServerBatchStore) SetIfLeaseOwned(context.Context, string, string, string, []byte, time.Duration) (bool, error) {
	return true, nil
}
func (*recordingServerBatchStore) SetIfLeasesOwned(context.Context, []string, []string, string, []byte, time.Duration) (bool, error) {
	return true, nil
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
