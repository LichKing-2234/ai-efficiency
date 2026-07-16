package telemetry

import (
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const metricsNamespace = "ai_efficiency"

type RequestObserver interface {
	Start(method string)
	Finish(route, method, statusClass string, duration time.Duration, responseBytes int)
}

type DependencyObserver interface {
	Observe(dependency, operation, method, statusClass string, duration time.Duration)
}

type Metrics struct {
	release  string
	registry *prometheus.Registry

	httpRequests        *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec
	httpResponseBytes   *prometheus.HistogramVec
	httpRequestsActive  *prometheus.GaugeVec
	dependencyRequests  *prometheus.CounterVec
	dependencyDuration  *prometheus.HistogramVec
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
			Buckets:   prometheus.ExponentialBuckets(0.005, 2, 13),
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
			Buckets:   prometheus.ExponentialBuckets(0.005, 2, 13),
		}, []string{"dependency", "operation", "method", "status_class", "release"}),
	}
	m.registry.MustRegister(
		m.httpRequests,
		m.httpRequestDuration,
		m.httpResponseBytes,
		m.httpRequestsActive,
		m.dependencyRequests,
		m.dependencyDuration,
	)
	return m
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
