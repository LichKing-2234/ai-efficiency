package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/teamusage"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func TestSetupRouterReportsAllMissingPerformanceInputs(t *testing.T) {
	router, err := SetupRouter(
		nil, nil, nil, nil, nil, nil, nil, "", "", nil, nil, nil, nil, nil, nil,
		RouterOptions{},
	)
	if err == nil {
		t.Fatal("SetupRouter() error = nil, want missing dependency error")
	}
	if router != nil {
		t.Fatalf("SetupRouter() router = %v, want nil", router)
	}
	for _, expected := range []string{
		"provider runtime",
		"cursor secret",
		"directory service",
		"personal usage cache",
		"work items cache",
		"work items revision store",
		"representative scope cache",
		"team usage snapshot cache",
		"team usage origin cache",
		"webhook HTTP client",
		"request logger",
		"request observer",
		"Web Vitals handler",
		"release",
		"request timeout",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("SetupRouter() error = %q, want %q", err, expected)
		}
	}
}

func TestNewProviderHandlerRequiresExplicitRuntime(t *testing.T) {
	client := testdb.Open(t)
	handler, err := NewProviderHandler(client, "test-encryption-key", zap.NewNop(), nil)
	if err == nil || !strings.Contains(err.Error(), "relay runtime") {
		t.Fatalf("NewProviderHandler() handler=%v error=%v, want relay runtime error", handler, err)
	}
}

func TestNewTeamUsageServiceRejectsImplicitUncachedFallback(t *testing.T) {
	client := testdb.Open(t)
	service, err := newTeamUsageService(client, nil, nil, nil, nil, nil, nil, "test-cursor-secret")
	if err == nil || !strings.Contains(err.Error(), "snapshot cache") {
		t.Fatalf("newTeamUsageService() service=%v error=%v, want snapshot cache error", service, err)
	}
}

func TestRouterDependenciesPassOptionalTeamUsagePrewarmReader(t *testing.T) {
	client := testdb.Open(t)
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	store := readcache.NewRedisStore(redisClient)
	snapshot, err := teamusage.NewSnapshotCache(store, teamusage.SnapshotCacheOptions{Namespace: "test"})
	if err != nil {
		t.Fatalf("NewSnapshotCache() error = %v", err)
	}
	origin, err := teamusage.NewOriginCache(store, teamusage.OriginCacheOptions{Namespace: "test"})
	if err != nil {
		t.Fatalf("NewOriginCache() error = %v", err)
	}
	prewarmCache, err := teamusage.NewPrewarmCache(store, teamusage.PrewarmCacheOptions{Namespace: "test"})
	if err != nil {
		t.Fatalf("NewPrewarmCache() error = %v", err)
	}
	reader, err := teamusage.NewPrewarmReader(prewarmCache, handlerPrewarmLimiter{}, teamusage.PrewarmReaderOptions{Timezones: []string{"UTC"}})
	if err != nil {
		t.Fatalf("NewPrewarmReader() error = %v", err)
	}
	readerSlot := teamusage.NewPrewarmReaderSlot()
	readerSlot.Store(reader)
	service, err := newTeamUsageService(client, nil, nil, nil, snapshot, origin, readerSlot, "test-cursor-secret")
	if err != nil || service == nil {
		t.Fatalf("newTeamUsageService() service=%v error=%v, want optional reader injected", service, err)
	}
}

type handlerPrewarmLimiter struct{}

func (handlerPrewarmLimiter) Do(ctx context.Context, call func(context.Context) error) error {
	return call(ctx)
}
