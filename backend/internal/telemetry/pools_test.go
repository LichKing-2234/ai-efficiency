package telemetry

import (
	"database/sql"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
)

func TestMetricsExportsDatabasePoolStats(t *testing.T) {
	metrics := NewMetrics("test-release")
	source := fakeDBStatsSource{stats: sql.DBStats{
		OpenConnections:   7,
		InUse:             3,
		Idle:              4,
		WaitCount:         9,
		WaitDuration:      250 * time.Millisecond,
		MaxIdleClosed:     2,
		MaxIdleTimeClosed: 5,
		MaxLifetimeClosed: 6,
	}}
	if err := metrics.RegisterDBPool(source); err != nil {
		t.Fatalf("RegisterDBPool() error = %v", err)
	}

	for state, want := range map[string]float64{"open": 7, "in_use": 3, "idle": 4} {
		if got := gaugeValue(t, metrics.Gatherer(), "ai_efficiency_db_connections", map[string]string{"state": state}); got != want {
			t.Fatalf("db connections state %s = %v, want %v", state, got, want)
		}
	}
	if got := counterValue(t, metrics.Gatherer(), "ai_efficiency_db_wait_total", nil); got != 9 {
		t.Fatalf("db wait total = %v, want 9", got)
	}
	if got := counterValue(t, metrics.Gatherer(), "ai_efficiency_db_wait_duration_seconds_total", nil); got != 0.25 {
		t.Fatalf("db wait duration = %v, want 0.25", got)
	}
	for reason, want := range map[string]float64{"max_idle": 2, "max_idle_time": 5, "max_lifetime": 6} {
		if got := counterValue(t, metrics.Gatherer(), "ai_efficiency_db_connections_closed_total", map[string]string{"reason": reason}); got != want {
			t.Fatalf("db closures reason %s = %v, want %v", reason, got, want)
		}
	}
}

func TestMetricsExportsRedisPoolStatsSeparatelyFromCacheOutcomes(t *testing.T) {
	metrics := NewMetrics("test-release")
	source := fakeRedisPoolStatsSource{stats: redis.PoolStats{
		Timeouts:        3,
		WaitCount:       8,
		WaitDurationNs:  int64(125 * time.Millisecond),
		TotalConns:      11,
		IdleConns:       7,
		StaleConns:      4,
		PendingRequests: 2,
	}}
	if err := metrics.RegisterRedisPool(source); err != nil {
		t.Fatalf("RegisterRedisPool() error = %v", err)
	}

	for state, want := range map[string]float64{"total": 11, "idle": 7, "stale": 4, "pending": 2} {
		if got := gaugeValue(t, metrics.Gatherer(), "ai_efficiency_redis_pool_connections", map[string]string{"state": state}); got != want {
			t.Fatalf("redis pool state %s = %v, want %v", state, got, want)
		}
	}
	if got := counterValue(t, metrics.Gatherer(), "ai_efficiency_redis_pool_wait_total", nil); got != 8 {
		t.Fatalf("redis pool wait total = %v, want 8", got)
	}
	if got := counterValue(t, metrics.Gatherer(), "ai_efficiency_redis_pool_wait_duration_seconds_total", nil); got != 0.125 {
		t.Fatalf("redis pool wait duration = %v, want 0.125", got)
	}
	if got := counterValue(t, metrics.Gatherer(), "ai_efficiency_redis_pool_timeout_total", nil); got != 3 {
		t.Fatalf("redis pool timeout total = %v, want 3", got)
	}
}

func TestMetricsRejectsDuplicatePoolCollectors(t *testing.T) {
	metrics := NewMetrics("test-release")
	if err := metrics.RegisterDBPool(fakeDBStatsSource{}); err != nil {
		t.Fatalf("first RegisterDBPool() error = %v", err)
	}
	if err := metrics.RegisterDBPool(fakeDBStatsSource{}); err == nil {
		t.Fatal("second RegisterDBPool() error = nil, want duplicate registration error")
	}
	if err := metrics.RegisterRedisPool(fakeRedisPoolStatsSource{}); err != nil {
		t.Fatalf("first RegisterRedisPool() error = %v", err)
	}
	if err := metrics.RegisterRedisPool(fakeRedisPoolStatsSource{}); err == nil {
		t.Fatal("second RegisterRedisPool() error = nil, want duplicate registration error")
	}
}

type fakeDBStatsSource struct {
	stats sql.DBStats
}

func (s fakeDBStatsSource) Stats() sql.DBStats {
	return s.stats
}

type fakeRedisPoolStatsSource struct {
	stats redis.PoolStats
}

func (s fakeRedisPoolStatsSource) PoolStats() *redis.PoolStats {
	stats := s.stats
	return &stats
}
