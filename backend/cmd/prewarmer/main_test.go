package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/config"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/relayruntime"
	"github.com/ai-efficiency/backend/internal/teamusage"
	"github.com/ai-efficiency/backend/internal/telemetry"
	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type refreshFunc func(context.Context) error

func (fn refreshFunc) Refresh(ctx context.Context) error { return fn(ctx) }

func TestRunLoopRefreshesImmediatelyAndSerially(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondFinished := make(chan struct{})

	var calls atomic.Int32
	var active atomic.Int32
	var maximum atomic.Int32
	refresher := refreshFunc(func(context.Context) error {
		call := calls.Add(1)
		current := active.Add(1)
		defer active.Add(-1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		if call == 1 {
			close(firstStarted)
			<-releaseFirst
		}
		if call == 2 {
			close(secondFinished)
			cancel()
		}
		return nil
	})

	done := make(chan error, 1)
	go func() { done <- runLoop(ctx, refresher, ticks, func(error) {}) }()
	<-firstStarted
	for range 3 {
		select {
		case ticks <- time.Now():
		default:
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("Refresh calls while first call is blocked = %d, want 1", got)
	}
	close(releaseFirst)
	select {
	case ticks <- time.Now():
	case <-time.After(time.Second):
		t.Fatal("runLoop did not wait for the next tick")
	}
	select {
	case <-secondFinished:
	case <-time.After(time.Second):
		t.Fatal("second Refresh did not finish")
	}
	if err := <-done; err != nil {
		t.Fatalf("runLoop() error = %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("Refresh calls = %d, want immediate call plus one tick", got)
	}
	if got := maximum.Load(); got != 1 {
		t.Fatalf("maximum concurrent Refresh calls = %d, want 1", got)
	}
}

func TestRunLoopContinuesAfterTransientRefreshError(t *testing.T) {
	sentinel := errors.New("synthetic transient refresh failure")
	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time, 1)
	ticks <- time.Now()
	var calls int
	refresher := refreshFunc(func(context.Context) error {
		calls++
		if calls == 1 {
			return sentinel
		}
		cancel()
		return nil
	})
	var reported []error

	if err := runLoop(ctx, refresher, ticks, func(err error) { reported = append(reported, err) }); err != nil {
		t.Fatalf("runLoop() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("Refresh calls = %d, want retry on the next tick", calls)
	}
	if len(reported) != 2 || !errors.Is(reported[0], sentinel) || reported[1] != nil {
		t.Fatalf("reported errors = %#v, want transient error then nil", reported)
	}
}

type recordingProviderManager struct {
	provider relay.Provider
	calls    atomic.Int32
}

func (m *recordingProviderManager) Resolve(context.Context, int) (relay.Provider, error) {
	m.calls.Add(1)
	return m.provider, nil
}

func TestWorkerBootstrapDoesNotMigrateOrStartHTTPApplicationRuntime(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	mock.ExpectPing()
	mock.ExpectQuery("SELECT .*relay_providers.*").WillReturnRows(sqlmock.NewRows([]string{
		"id", "name", "display_name", "base_url", "relay_type", "admin_api_key",
		"default_model", "is_primary", "enabled", "configuration_version", "created_at", "updated_at",
	}).AddRow(
		7, "primary", "Primary", "https://relay.example.com", "sub2api", "encrypted-test-key",
		"test-model", true, true, 11, time.Now(), time.Now(),
	))
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	manager := &recordingProviderManager{
		provider: relay.NewSub2apiProvider(http.DefaultClient, "https://relay.example.com", "test-key", "test-model", zap.NewNop()),
	}
	var dbFactoryCalls atomic.Int32
	var redisFactoryCalls atomic.Int32
	deps := defaultWorkerDependencies()
	deps.openDatabase = func(string, string) (*sql.DB, error) {
		dbFactoryCalls.Add(1)
		return db, nil
	}
	deps.newRedisClient = func(config.RedisConfig) redis.UniversalClient {
		redisFactoryCalls.Add(1)
		return redisClient
	}
	deps.newProviderManager = func(*ent.Client, string, *zap.Logger, relayruntime.Options) (providerManager, error) {
		return manager, nil
	}
	deps.newRefresher = func(
		resolver teamusage.PrimaryProviderBindingResolver,
		_ *teamusage.PrewarmCache,
		_ teamusage.RefresherOptions,
	) (teamusage.Refresher, error) {
		return refreshFunc(func(ctx context.Context) error {
			_, err := resolver.ResolvePrimaryProviderBinding(ctx)
			return err
		}), nil
	}
	cfg := &config.Config{
		DB: config.DBConfig{
			DSN:          "postgres://test:test@db.example.com/ai_efficiency?sslmode=require",
			MaxOpenConns: 3, MaxIdleConns: 1, ConnMaxLifetime: 60,
		},
		Redis:      config.RedisConfig{Addr: redisServer.Addr(), Namespace: "test-blue"},
		Encryption: config.EncryptionConfig{Key: strings.Repeat("a", 64)},
		Prewarmer:  config.PrewarmerConfig{Timezones: []string{"UTC"}},
	}
	metrics := telemetry.NewMetrics("test-release")
	runtime, err := bootstrapWorker(context.Background(), cfg, zap.NewNop(), metrics, deps)
	if err != nil {
		t.Fatalf("bootstrapWorker() error = %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	if err := runtime.refresher.Refresh(context.Background()); err != nil {
		t.Fatalf("worker Refresh() error = %v", err)
	}
	if dbFactoryCalls.Load() != 1 || redisFactoryCalls.Load() != 1 {
		t.Fatalf("factory calls db/Redis = %d/%d, want 1/1", dbFactoryCalls.Load(), redisFactoryCalls.Load())
	}
	if redisServer.CommandCount() == 0 {
		t.Fatal("Redis Ping was not issued during bootstrap")
	}
	if manager.calls.Load() != 1 {
		t.Fatalf("provider manager Resolve calls = %d, want 1", manager.calls.Load())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}

	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read worker source: %v", err)
	}
	for _, forbidden := range []string{
		".Schema.Create(",
		"dropLegacyRelayProviderAdminURL",
		"EnsureWritableConfigFile",
		"NewRedisInvalidationBus",
		"providerRuntime.Start(",
		"internal/auth",
		"internal/handler",
		"internal/scm",
		"SetupRouter(",
	} {
		if strings.Contains(string(source), forbidden) {
			t.Fatalf("worker source contains forbidden application runtime boundary %q", forbidden)
		}
	}
}
