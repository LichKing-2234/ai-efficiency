package telemetry

import (
	"database/sql"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	redis "github.com/redis/go-redis/v9"
)

type DBStatsSource interface {
	Stats() sql.DBStats
}

type RedisPoolStatsSource interface {
	PoolStats() *redis.PoolStats
}

func (m *Metrics) RegisterDBPool(source DBStatsSource) error {
	if source == nil {
		return fmt.Errorf("database stats source is required")
	}
	return m.registry.Register(newDBPoolCollector(source))
}

func (m *Metrics) RegisterRedisPool(source RedisPoolStatsSource) error {
	if source == nil {
		return fmt.Errorf("Redis pool stats source is required")
	}
	return m.registry.Register(newRedisPoolCollector(source))
}

type dbPoolCollector struct {
	source       DBStatsSource
	connections  *prometheus.Desc
	waitTotal    *prometheus.Desc
	waitDuration *prometheus.Desc
	closedTotal  *prometheus.Desc
}

func newDBPoolCollector(source DBStatsSource) *dbPoolCollector {
	return &dbPoolCollector{
		source: source,
		connections: prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, "db", "connections"),
			"Current database connections by pool state.",
			[]string{"state"}, nil,
		),
		waitTotal: prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, "db", "wait_total"),
			"Total database connection waits.",
			nil, nil,
		),
		waitDuration: prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, "db", "wait_duration_seconds_total"),
			"Total duration spent waiting for database connections in seconds.",
			nil, nil,
		),
		closedTotal: prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, "db", "connections_closed_total"),
			"Total database connections closed by configured pool limits.",
			[]string{"reason"}, nil,
		),
	}
}

func (c *dbPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.connections
	ch <- c.waitTotal
	ch <- c.waitDuration
	ch <- c.closedTotal
}

func (c *dbPoolCollector) Collect(ch chan<- prometheus.Metric) {
	stats := c.source.Stats()
	ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(stats.OpenConnections), "open")
	ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(stats.InUse), "in_use")
	ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(stats.Idle), "idle")
	ch <- prometheus.MustNewConstMetric(c.waitTotal, prometheus.CounterValue, float64(stats.WaitCount))
	ch <- prometheus.MustNewConstMetric(c.waitDuration, prometheus.CounterValue, stats.WaitDuration.Seconds())
	ch <- prometheus.MustNewConstMetric(c.closedTotal, prometheus.CounterValue, float64(stats.MaxIdleClosed), "max_idle")
	ch <- prometheus.MustNewConstMetric(c.closedTotal, prometheus.CounterValue, float64(stats.MaxIdleTimeClosed), "max_idle_time")
	ch <- prometheus.MustNewConstMetric(c.closedTotal, prometheus.CounterValue, float64(stats.MaxLifetimeClosed), "max_lifetime")
}

type redisPoolCollector struct {
	source       RedisPoolStatsSource
	connections  *prometheus.Desc
	waitTotal    *prometheus.Desc
	waitDuration *prometheus.Desc
	timeoutTotal *prometheus.Desc
	staleTotal   *prometheus.Desc
}

func newRedisPoolCollector(source RedisPoolStatsSource) *redisPoolCollector {
	return &redisPoolCollector{
		source: source,
		connections: prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, "redis_pool", "connections"),
			"Current Redis pool connections and waiters by state.",
			[]string{"state"}, nil,
		),
		waitTotal: prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, "redis_pool", "wait_total"),
			"Total Redis pool connection waits.",
			nil, nil,
		),
		waitDuration: prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, "redis_pool", "wait_duration_seconds_total"),
			"Total duration spent waiting for Redis pool connections in seconds.",
			nil, nil,
		),
		timeoutTotal: prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, "redis_pool", "timeout_total"),
			"Total Redis pool wait timeouts.",
			nil, nil,
		),
		staleTotal: prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, "redis_pool", "stale_connections_total"),
			"Total stale Redis connections removed from the pool.",
			nil, nil,
		),
	}
}

func (c *redisPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.connections
	ch <- c.waitTotal
	ch <- c.waitDuration
	ch <- c.timeoutTotal
	ch <- c.staleTotal
}

func (c *redisPoolCollector) Collect(ch chan<- prometheus.Metric) {
	stats := c.source.PoolStats()
	if stats == nil {
		return
	}
	ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(stats.TotalConns), "total")
	ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(stats.IdleConns), "idle")
	ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(stats.PendingRequests), "pending")
	ch <- prometheus.MustNewConstMetric(c.waitTotal, prometheus.CounterValue, float64(stats.WaitCount))
	ch <- prometheus.MustNewConstMetric(c.waitDuration, prometheus.CounterValue, float64(stats.WaitDurationNs)/1e9)
	ch <- prometheus.MustNewConstMetric(c.timeoutTotal, prometheus.CounterValue, float64(stats.Timeouts))
	ch <- prometheus.MustNewConstMetric(c.staleTotal, prometheus.CounterValue, float64(stats.StaleConns))
}
