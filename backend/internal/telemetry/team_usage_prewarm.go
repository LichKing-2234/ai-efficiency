package telemetry

import (
	"fmt"
	"time"

	"github.com/ai-efficiency/backend/internal/teamusage"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	prewarmCycleClasses    = []string{"moving", "recovery", "startup", "history_29d", "history_6d"}
	prewarmCycleOutcomes   = []string{"success", "error", "canceled", "rejected", "skipped", "tick_skipped", "lease_busy"}
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
	prewarmQuantityKinds = []teamusage.PrewarmQuantityKind{
		teamusage.PrewarmQuantityUnionUsers,
		teamusage.PrewarmQuantitySegmentBytes,
		teamusage.PrewarmQuantityTimezoneBytes,
	}
	prewarmValidationChecks = []teamusage.PrewarmValidationCheck{
		teamusage.PrewarmValidationDirectoryPagination,
		teamusage.PrewarmValidationProviderIDBound,
		teamusage.PrewarmValidationStatsExactCoverage,
		teamusage.PrewarmValidationRawTrendCompleteness,
		teamusage.PrewarmValidationRawTrendCoverage,
		teamusage.PrewarmValidationRawTrendLimit,
	}
	prewarmValidationOutcomes = []teamusage.PrewarmValidationOutcome{
		teamusage.PrewarmValidationAccepted,
		teamusage.PrewarmValidationRejected,
	}
	prewarmCacheKinds = []teamusage.PrewarmCacheKind{
		teamusage.PrewarmCacheManifest,
		teamusage.PrewarmCacheCurrentStats,
		teamusage.PrewarmCacheSegment,
	}
	prewarmCacheOutcomes = []teamusage.PrewarmCacheOutcome{
		teamusage.PrewarmCacheFresh,
		teamusage.PrewarmCacheStale,
		teamusage.PrewarmCacheMiss,
		teamusage.PrewarmCacheInvalid,
		teamusage.PrewarmCacheHardExpired,
		teamusage.PrewarmCacheError,
	}
)

type teamUsagePrewarmMetrics struct {
	timezones map[string]struct{}

	cycleTotal      *prometheus.CounterVec
	cycleDuration   *prometheus.HistogramVec
	sourceDuration  *prometheus.HistogramVec
	sourceBytes     *prometheus.HistogramVec
	sourcePoints    *prometheus.HistogramVec
	sourceUsers     *prometheus.HistogramVec
	redisDuration   *prometheus.HistogramVec
	redisBytes      *prometheus.HistogramVec
	requestTotal    *prometheus.CounterVec
	lastSuccess     *prometheus.GaugeVec
	quantity        *prometheus.HistogramVec
	generationBytes prometheus.Gauge
	validationTotal *prometheus.CounterVec
	cacheTotal      *prometheus.CounterVec
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
		quantity: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Name: "team_usage_prewarm_quantity",
			Help:    "Bounded Team Usage prewarm composition and publication quantities.",
			Buckets: prometheus.ExponentialBuckets(1, 2, 25),
		}, []string{"quantity", "timezone"}),
		generationBytes: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace, Name: "team_usage_prewarm_generation_bytes",
			Help: "Latest full Team Usage prewarm generation bytes with provider-wide current stats counted once.",
		}),
		validationTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Name: "team_usage_prewarm_validation_total",
			Help: "Structured bounded Team Usage prewarm source validation outcomes.",
		}, []string{"check", "outcome"}),
		cacheTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Name: "team_usage_prewarm_cache_total",
			Help: "Distinct Team Usage prewarm manifest, current-stats, and segment cache outcomes.",
		}, []string{"cache", "outcome"}),
	}
	for _, timezone := range normalized {
		recorder.timezones[timezone] = struct{}{}
	}
	m.registry.MustRegister(
		recorder.cycleTotal, recorder.cycleDuration,
		recorder.sourceDuration, recorder.sourceBytes, recorder.sourcePoints, recorder.sourceUsers,
		recorder.redisDuration, recorder.redisBytes, recorder.requestTotal, recorder.lastSuccess,
		recorder.quantity, recorder.generationBytes, recorder.validationTotal, recorder.cacheTotal,
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
		for _, kind := range prewarmQuantityKinds {
			recorder := m.quantity.WithLabelValues(string(kind), timezone)
			_ = recorder
		}
	}
	m.generationBytes.Set(0)
	for _, check := range prewarmValidationChecks {
		for _, outcome := range prewarmValidationOutcomes {
			m.validationTotal.WithLabelValues(string(check), string(outcome)).Add(0)
		}
	}
	for _, cache := range prewarmCacheKinds {
		for _, outcome := range prewarmCacheOutcomes {
			m.cacheTotal.WithLabelValues(string(cache), string(outcome)).Add(0)
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

func (m *teamUsagePrewarmMetrics) RecordQuantity(kind teamusage.PrewarmQuantityKind, timezone string, value int) {
	if !containsPrewarmQuantityKind(kind) || !m.validTimezone(timezone) {
		return
	}
	m.quantity.WithLabelValues(string(kind), timezone).Observe(float64(nonnegativeInt(value)))
}

func (m *teamUsagePrewarmMetrics) SetGenerationBytes(value int) {
	m.generationBytes.Set(float64(nonnegativeInt(value)))
}

func (m *teamUsagePrewarmMetrics) RecordValidation(check teamusage.PrewarmValidationCheck, outcome teamusage.PrewarmValidationOutcome) {
	if !containsPrewarmValidationCheck(check) || !containsPrewarmValidationOutcome(outcome) {
		return
	}
	m.validationTotal.WithLabelValues(string(check), string(outcome)).Inc()
}

func (m *teamUsagePrewarmMetrics) RecordCache(cache teamusage.PrewarmCacheKind, outcome teamusage.PrewarmCacheOutcome) {
	if !containsPrewarmCacheKind(cache) || !containsPrewarmCacheOutcome(outcome) {
		return
	}
	m.cacheTotal.WithLabelValues(string(cache), string(outcome)).Inc()
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

func containsPrewarmQuantityKind(value teamusage.PrewarmQuantityKind) bool {
	for _, allowed := range prewarmQuantityKinds {
		if value == allowed {
			return true
		}
	}
	return false
}

func containsPrewarmValidationCheck(value teamusage.PrewarmValidationCheck) bool {
	for _, allowed := range prewarmValidationChecks {
		if value == allowed {
			return true
		}
	}
	return false
}

func containsPrewarmValidationOutcome(value teamusage.PrewarmValidationOutcome) bool {
	for _, allowed := range prewarmValidationOutcomes {
		if value == allowed {
			return true
		}
	}
	return false
}

func containsPrewarmCacheKind(value teamusage.PrewarmCacheKind) bool {
	for _, allowed := range prewarmCacheKinds {
		if value == allowed {
			return true
		}
	}
	return false
}

func containsPrewarmCacheOutcome(value teamusage.PrewarmCacheOutcome) bool {
	for _, allowed := range prewarmCacheOutcomes {
		if value == allowed {
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
