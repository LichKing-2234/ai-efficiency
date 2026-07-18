package relayruntime

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type recordingMetadataCacheMetrics struct {
	mu     sync.Mutex
	counts map[string]int
}

func (r *recordingMetadataCacheMetrics) Record(outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.counts == nil {
		r.counts = make(map[string]int)
	}
	r.counts[outcome]++
}

func (r *recordingMetadataCacheMetrics) count(outcome string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[outcome]
}

func TestProviderMetadataCacheMetricsRecordColdAndWarmReads(t *testing.T) {
	client := testdbClient(t)
	row := createProviderRow(t, client)
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	recorder := &recordingMetadataCacheMetrics{}
	manager, err := NewManager(client, testEncryptionKey, zap.NewNop(), Options{
		Namespace: "test", Store: readcache.NewRedisStore(redisClient), MetadataTTL: 5 * time.Minute,
		MetadataMetrics: recorder,
		Factory:         func(*ent.RelayProvider, string) (relay.Provider, error) { return &taggedProvider{}, nil },
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	var loads atomic.Int32
	loader := func(context.Context) ([]relay.ModelOption, error) {
		loads.Add(1)
		return []relay.ModelOption{{ID: "model-1", DisplayName: "Model 1"}}, nil
	}
	for index := 0; index < 2; index++ {
		if _, err := manager.Models(context.Background(), row, "openai", "group-alpha", loader); err != nil {
			t.Fatalf("read %d error = %v", index, err)
		}
	}
	if loads.Load() != 1 {
		t.Fatalf("loader calls = %d, want 1", loads.Load())
	}
	for outcome, want := range map[string]int{"miss": 1, "refresh": 1, "lease_acquired": 1, "fresh": 1} {
		if got := recorder.count(outcome); got != want {
			t.Fatalf("outcome %s = %d, want %d", outcome, got, want)
		}
	}
}
