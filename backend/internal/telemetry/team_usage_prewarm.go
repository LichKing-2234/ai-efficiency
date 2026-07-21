package telemetry

import (
	"fmt"
	"time"

	"github.com/ai-efficiency/backend/internal/teamusage"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	prewarmCycleClasses    = []string{"moving", "recovery", "startup", "history_29d", "history_6d"}
	prewarmCycleOutcomes   = []string{"success", "error", "canceled", "skipped", "tick_skipped", "lease_busy"}
	prewarmSourceClasses   = []string{"moving", "recovery", "startup", "history_29d", "history_6d", "today_hour"}
	prewarmSourceOutcomes  = []string{"success", "error", "canceled", "rejected"}
	prewarmRedisOperations = []string{
		"manifest_read", "generation_read", "immutable_write", "manifest_write",
		"lease_acquire", "lease_ttl", "lease_release",
	}
	prewarmRedisOutcomes = []string{
		"hit", "miss", "success", "error", "acquired", "busy", "released", "not_owned",
	}
	prewarmRequestOutcomes = []string{"full_hit", "partial_today", "ineligible", "miss", "fallback"}
	prewarmFallbackReasons = []string{
		"none", "cache_miss", "ineligible", "invalid_request", "redis_error",
		"generation_invalid", "roster_incomplete", "source_error",
	}
)

type teamUsagePrewarmMetrics struct {
	timezones map[string]struct{}

	cycleTotal     *prometheus.CounterVec
	cycleDuration  *prometheus.HistogramVec
	sourceDuration *prometheus.HistogramVec
	sourceBytes    *prometheus.HistogramVec
	sourcePoints   *prometheus.HistogramVec
	sourceUsers    *prometheus.HistogramVec
	redisDuration  *prometheus.HistogramVec
	redisBytes     *prometheus.HistogramVec
	requestTotal   *prometheus.CounterVec
	lastSuccess    *prometheus.GaugeVec
}

func (m *Metrics) TeamUsagePrewarmRecorder(timezones []string) (teamusage.PrewarmMetrics, error) {
	if m == nil || m.registry == nil {
		return nil, fmt.Errorf("telemetry metrics registry is required")
	}
	normalized, err := teamusage.NormalizePrewarmTimezones(timezones)
	if err != nil {
		return nil, err
	}
	if len(normalized) == 0 {
		return nil, fmt.Errorf("at least one Team Usage prewarm telemetry timezone is required")
	}

	recorder := &teamUsagePrewarmMetrics{
		timezones: make(map[string]struct{}, len(normalized)),
		cycleTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Name: "team_usage_prewarm_cycle_total",
			Help: "Completed and skipped Team Usage prewarm cycles.",
		}, []string{"class", "timezone", "outcome"}),
		cycleDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Name: "team_usage_prewarm_cycle_duration_seconds",
			Help: "Team Usage prewarm cycle duration in seconds.", Buckets: prometheus.ExponentialBuckets(0.01, 2, 16),
		}, []string{"class", "timezone", "outcome"}),
		sourceDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Name: "team_usage_prewarm_source_duration_seconds",
			Help: "Team Usage prewarm Relay source duration in seconds.", Buckets: prometheus.ExponentialBuckets(0.05, 2, 13),
		}, []string{"class", "timezone", "outcome"}),
		sourceBytes: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Name: "team_usage_prewarm_source_bytes",
			Help: "Bounded Team Usage prewarm Relay response bytes.", Buckets: prometheus.ExponentialBuckets(1024, 2, 15),
		}, []string{"class", "timezone", "outcome"}),
		sourcePoints: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Name: "team_usage_prewarm_source_points",
			Help: "Bounded Team Usage prewarm decoded source points.", Buckets: prometheus.ExponentialBuckets(1, 2, 20),
		}, []string{"class", "timezone", "outcome"}),
		sourceUsers: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Name: "team_usage_prewarm_source_users",
			Help: "Bounded Team Usage prewarm unique source users.", Buckets: prometheus.ExponentialBuckets(1, 2, 13),
		}, []string{"class", "timezone", "outcome"}),
		redisDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Name: "team_usage_prewarm_redis_duration_seconds",
			Help: "Team Usage prewarm Redis operation duration in seconds.", Buckets: prometheus.ExponentialBuckets(0.001, 2, 12),
		}, []string{"operation", "outcome"}),
		redisBytes: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Name: "team_usage_prewarm_redis_bytes",
			Help: "Bounded Team Usage prewarm Redis payload bytes.", Buckets: prometheus.ExponentialBuckets(64, 2, 19),
		}, []string{"operation", "outcome"}),
		requestTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Name: "team_usage_prewarm_request_total",
			Help: "Team Usage request prewarm outcomes and exact-fallback reasons.",
		}, []string{"timezone", "outcome", "fallback_reason"}),
		lastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsNamespace, Name: "team_usage_prewarm_last_success_timestamp_seconds",
			Help: "Unix timestamp of the last successful Team Usage prewarm publication.",
		}, []string{"class", "timezone"}),
	}
	for _, timezone := range normalized {
		recorder.timezones[timezone] = struct{}{}
	}
	m.registry.MustRegister(
		recorder.cycleTotal, recorder.cycleDuration,
		recorder.sourceDuration, recorder.sourceBytes, recorder.sourcePoints, recorder.sourceUsers,
		recorder.redisDuration, recorder.redisBytes, recorder.requestTotal, recorder.lastSuccess,
	)
	recorder.preinitialize(normalized)
	return recorder, nil
}

func (m *teamUsagePrewarmMetrics) preinitialize(timezones []string) {
	for _, timezone := range timezones {
		for _, class := range prewarmCycleClasses {
			m.lastSuccess.WithLabelValues(class, timezone).Set(0)
			for _, outcome := range prewarmCycleOutcomes {
				m.cycleTotal.WithLabelValues(class, timezone, outcome).Add(0)
				m.cycleDuration.WithLabelValues(class, timezone, outcome)
			}
		}
		for _, class := range prewarmSourceClasses {
			for _, outcome := range prewarmSourceOutcomes {
				m.sourceDuration.WithLabelValues(class, timezone, outcome)
				m.sourceBytes.WithLabelValues(class, timezone, outcome)
				m.sourcePoints.WithLabelValues(class, timezone, outcome)
				m.sourceUsers.WithLabelValues(class, timezone, outcome)
			}
		}
		for _, outcome := range prewarmRequestOutcomes {
			for _, reason := range prewarmFallbackReasons {
				m.requestTotal.WithLabelValues(timezone, outcome, reason).Add(0)
			}
		}
	}
	for _, operation := range prewarmRedisOperations {
		for _, outcome := range prewarmRedisOutcomes {
			m.redisDuration.WithLabelValues(operation, outcome)
			m.redisBytes.WithLabelValues(operation, outcome)
		}
	}
}

func (m *teamUsagePrewarmMetrics) RecordCycle(class, timezone, outcome string, duration time.Duration) {
	if !containsMetricValue(prewarmCycleClasses, class) || !m.validTimezone(timezone) || !containsMetricValue(prewarmCycleOutcomes, outcome) {
		return
	}
	duration = nonnegativeDuration(duration)
	m.cycleTotal.WithLabelValues(class, timezone, outcome).Inc()
	m.cycleDuration.WithLabelValues(class, timezone, outcome).Observe(duration.Seconds())
}

func (m *teamUsagePrewarmMetrics) RecordSource(class, timezone, outcome string, duration time.Duration, bytes, points, users int) {
	if !containsMetricValue(prewarmSourceClasses, class) || !m.validTimezone(timezone) || !containsMetricValue(prewarmSourceOutcomes, outcome) {
		return
	}
	labels := []string{class, timezone, outcome}
	m.sourceDuration.WithLabelValues(labels...).Observe(nonnegativeDuration(duration).Seconds())
	m.sourceBytes.WithLabelValues(labels...).Observe(float64(nonnegativeInt(bytes)))
	m.sourcePoints.WithLabelValues(labels...).Observe(float64(nonnegativeInt(points)))
	m.sourceUsers.WithLabelValues(labels...).Observe(float64(nonnegativeInt(users)))
}

func (m *teamUsagePrewarmMetrics) RecordRedis(operation, outcome string, duration time.Duration, bytes int) {
	if !containsMetricValue(prewarmRedisOperations, operation) || !containsMetricValue(prewarmRedisOutcomes, outcome) {
		return
	}
	labels := []string{operation, outcome}
	m.redisDuration.WithLabelValues(labels...).Observe(nonnegativeDuration(duration).Seconds())
	m.redisBytes.WithLabelValues(labels...).Observe(float64(nonnegativeInt(bytes)))
}

func (m *teamUsagePrewarmMetrics) RecordRequest(timezone, outcome, fallbackReason string) {
	if !m.validTimezone(timezone) || !containsMetricValue(prewarmRequestOutcomes, outcome) || !containsMetricValue(prewarmFallbackReasons, fallbackReason) {
		return
	}
	m.requestTotal.WithLabelValues(timezone, outcome, fallbackReason).Inc()
}

func (m *teamUsagePrewarmMetrics) SetLastSuccess(class, timezone string, at time.Time) {
	if !containsMetricValue(prewarmCycleClasses, class) || !m.validTimezone(timezone) || at.IsZero() {
		return
	}
	m.lastSuccess.WithLabelValues(class, timezone).Set(float64(at.Unix()))
}

func (m *teamUsagePrewarmMetrics) validTimezone(timezone string) bool {
	_, ok := m.timezones[timezone]
	return ok
}

func containsMetricValue(allowed []string, value string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func nonnegativeDuration(value time.Duration) time.Duration {
	if value < 0 {
		return 0
	}
	return value
}

func nonnegativeInt(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

var _ teamusage.PrewarmMetrics = (*teamUsagePrewarmMetrics)(nil)
