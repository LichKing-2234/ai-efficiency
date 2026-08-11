package telemetry

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const metricsNamespace = "ai_efficiency"

var (
	cacheNameRE   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	cacheOutcomes = []string{
		"fresh",
		"miss",
		"stale",
		"error",
		"refresh",
		"lease_acquired",
		"lease_wait",
		"lease_failed",
	}
)

type RequestObserver interface {
	Start(method string)
	Finish(route, method, statusClass string, duration time.Duration, responseBytes int)
}

type DependencyObserver interface {
	Observe(dependency, operation, method, statusClass string, duration time.Duration)
}

type CacheObserver interface {
	Record(outcome string)
}

type Metrics struct {
	release  string
	registry *prometheus.Registry

	httpRequests                 *prometheus.CounterVec
	httpRequestDuration          *prometheus.HistogramVec
	httpResponseBytes            *prometheus.HistogramVec
	httpRequestsActive           *prometheus.GaugeVec
	dependencyRequests           *prometheus.CounterVec
	dependencyDuration           *prometheus.HistogramVec
	cacheEvents                  *prometheus.CounterVec
	browserVitalSeconds          *prometheus.HistogramVec
	browserVitalRatio            *prometheus.HistogramVec
	attributionPending           prometheus.Gauge
	attributionOldest            prometheus.Gauge
	attributionNearExpiry        prometheus.Gauge
	attributionReconciliations   *prometheus.CounterVec
	attributionReconciliationAge *prometheus.HistogramVec
	attributionLifecycle         *prometheus.CounterVec
}

func NewMetrics(release string) *Metrics {
	release = strings.TrimSpace(release)
	if release == "" {
		release = "unknown"
	}
	m := &Metrics{
		release:  release,
		registry: prometheus.NewRegistry(),
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "http_requests_total",
			Help:      "Completed first-party HTTP requests.",
		}, []string{"route", "method", "status_class", "release"}),
		httpRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "http_request_duration_seconds",
			Help:      "First-party HTTP request duration in seconds.",
			Buckets:   prometheus.ExponentialBuckets(0.005, 2, 15),
		}, []string{"route", "method", "status_class", "release"}),
		httpResponseBytes: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "http_response_bytes",
			Help:      "First-party HTTP response size in bytes.",
			Buckets:   prometheus.ExponentialBuckets(128, 2, 18),
		}, []string{"route", "method", "status_class", "release"}),
		httpRequestsActive: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "http_requests_in_flight",
			Help:      "First-party HTTP requests currently in flight.",
		}, []string{"method", "release"}),
		dependencyRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "dependency_requests_total",
			Help:      "Completed downstream dependency requests.",
		}, []string{"dependency", "operation", "method", "status_class", "release"}),
		dependencyDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "dependency_request_duration_seconds",
			Help:      "Downstream dependency request duration in seconds.",
			Buckets:   prometheus.ExponentialBuckets(0.005, 2, 15),
		}, []string{"dependency", "operation", "method", "status_class", "release"}),
		cacheEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "cache_events_total",
			Help:      "Application cache events by stable cache name and outcome.",
		}, []string{"cache", "outcome"}),
		browserVitalSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "browser_web_vital_seconds",
			Help:      "Sampled browser LCP, INP, and TTFB values in seconds.",
			Buckets:   []float64{0.05, 0.1, 0.2, 0.5, 0.8, 1, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10, 15, 30, 60, 120, 300, 600},
		}, []string{"metric", "route", "navigation_type", "release"}),
		browserVitalRatio: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "browser_web_vital_ratio",
			Help:      "Sampled browser CLS values.",
			Buckets:   []float64{0.01, 0.025, 0.05, 0.075, 0.1, 0.15, 0.25, 0.4, 0.6, 1, 2, 5, 10},
		}, []string{"metric", "route", "navigation_type", "release"}),
		attributionPending:           prometheus.NewGauge(prometheus.GaugeOpts{Namespace: metricsNamespace, Name: "attribution_requests_pending", Help: "Pending v2 attribution Request claims."}),
		attributionOldest:            prometheus.NewGauge(prometheus.GaugeOpts{Namespace: metricsNamespace, Name: "attribution_oldest_pending_age_seconds", Help: "Age of the oldest pending v2 attribution Request claim."}),
		attributionNearExpiry:        prometheus.NewGauge(prometheus.GaugeOpts{Namespace: metricsNamespace, Name: "attribution_groups_near_expiry", Help: "V2 attribution claim groups at or beyond the final-attempt boundary."}),
		attributionReconciliations:   prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: metricsNamespace, Name: "attribution_reconciliations_total", Help: "V2 attribution reconciliation outcomes."}, []string{"outcome", "release"}),
		attributionReconciliationAge: prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: metricsNamespace, Name: "attribution_reconciliation_age_seconds", Help: "Age of v2 Request claims when a reconciliation attempt completes.", Buckets: prometheus.ExponentialBuckets(60, 2, 18)}, []string{"outcome", "release"}),
		attributionLifecycle:         prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: metricsNamespace, Name: "attribution_lifecycle_total", Help: "V2 attribution finalization and cleanup outcomes."}, []string{"operation", "outcome", "release"}),
	}
	m.registry.MustRegister(
		m.httpRequests,
		m.httpRequestDuration,
		m.httpResponseBytes,
		m.httpRequestsActive,
		m.dependencyRequests,
		m.dependencyDuration,
		m.cacheEvents,
		m.browserVitalSeconds,
		m.browserVitalRatio,
		m.attributionPending,
		m.attributionOldest,
		m.attributionNearExpiry,
		m.attributionReconciliations,
		m.attributionReconciliationAge,
		m.attributionLifecycle,
	)
	return m
}

type AttributionRecorder struct{ metrics *Metrics }

func (m *Metrics) AttributionRecorder() AttributionRecorder { return AttributionRecorder{metrics: m} }

func (r AttributionRecorder) SetAttributionHealth(pending int, oldestPendingAge time.Duration, nearExpiry int) {
	if pending < 0 {
		pending = 0
	}
	if nearExpiry < 0 {
		nearExpiry = 0
	}
	if oldestPendingAge < 0 {
		oldestPendingAge = 0
	}
	r.metrics.attributionPending.Set(float64(pending))
	r.metrics.attributionOldest.Set(oldestPendingAge.Seconds())
	r.metrics.attributionNearExpiry.Set(float64(nearExpiry))
}

func (r AttributionRecorder) ObserveAttributionReconciliation(outcome string, age time.Duration) {
	if !attributionOutcomeAllowed(outcome) {
		outcome = "unknown"
	}
	if age < 0 {
		age = 0
	}
	r.metrics.attributionReconciliations.WithLabelValues(outcome, r.metrics.release).Inc()
	r.metrics.attributionReconciliationAge.WithLabelValues(outcome, r.metrics.release).Observe(age.Seconds())
}

func (r AttributionRecorder) AddAttributionLifecycle(operation, outcome string, count int) {
	if count <= 0 {
		return
	}
	if operation != "finalization" && operation != "cleanup" {
		operation = "unknown"
	}
	if outcome != "succeeded" && outcome != "deferred" && outcome != "hard_expired" && outcome != "error" {
		outcome = "unknown"
	}
	r.metrics.attributionLifecycle.WithLabelValues(operation, outcome, r.metrics.release).Add(float64(count))
}

func attributionOutcomeAllowed(outcome string) bool {
	switch outcome {
	case "pending", "reconciled", "owner_mismatch", "ambiguous", "provider_unavailable", "invalid_usage", "source_expired":
		return true
	default:
		return false
	}
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Metrics) Gatherer() prometheus.Gatherer {
	return m.registry
}

func (m *Metrics) RequestObserver() RequestObserver {
	return requestMetrics{metrics: m}
}

func (m *Metrics) DependencyObserver() DependencyObserver {
	return dependencyMetrics{metrics: m}
}

func (m *Metrics) CacheRecorder(name string) CacheObserver {
	if !cacheNameRE.MatchString(name) {
		panic("invalid cache metrics name")
	}
	for _, outcome := range cacheOutcomes {
		m.cacheEvents.WithLabelValues(name, outcome).Add(0)
	}
	return cacheMetrics{name: name, events: m.cacheEvents}
}

type cacheMetrics struct {
	name   string
	events *prometheus.CounterVec
}

func (m cacheMetrics) Record(outcome string) {
	for _, allowed := range cacheOutcomes {
		if outcome == allowed {
			m.events.WithLabelValues(m.name, outcome).Inc()
			return
		}
	}
}

type requestMetrics struct {
	metrics *Metrics
}

func (o requestMetrics) Start(method string) {
	method = HTTPMethod(method)
	o.metrics.httpRequestsActive.WithLabelValues(method, o.metrics.release).Inc()
}

func (o requestMetrics) Finish(route, method, statusClass string, duration time.Duration, responseBytes int) {
	method = HTTPMethod(method)
	statusClass = normalizeMetricsStatusClass(statusClass)
	if route == "" {
		route = "unmatched"
	}
	if duration < 0 {
		duration = 0
	}
	if responseBytes < 0 {
		responseBytes = 0
	}
	labels := []string{route, method, statusClass, o.metrics.release}
	o.metrics.httpRequests.WithLabelValues(labels...).Inc()
	o.metrics.httpRequestDuration.WithLabelValues(labels...).Observe(duration.Seconds())
	o.metrics.httpResponseBytes.WithLabelValues(labels...).Observe(float64(responseBytes))
	o.metrics.httpRequestsActive.WithLabelValues(method, o.metrics.release).Dec()
}

type dependencyMetrics struct {
	metrics *Metrics
}

func (o dependencyMetrics) Observe(dependency, operation, method, statusClass string, duration time.Duration) {
	method = HTTPMethod(method)
	statusClass = normalizeMetricsStatusClass(statusClass)
	if duration < 0 {
		duration = 0
	}
	labels := []string{dependency, operation, method, statusClass, o.metrics.release}
	o.metrics.dependencyRequests.WithLabelValues(labels...).Inc()
	o.metrics.dependencyDuration.WithLabelValues(labels...).Observe(duration.Seconds())
}

func normalizeMetricsStatusClass(statusClass string) string {
	switch statusClass {
	case "1xx", "2xx", "3xx", "4xx", "5xx", "error", "unknown":
		return statusClass
	default:
		return "unknown"
	}
}
