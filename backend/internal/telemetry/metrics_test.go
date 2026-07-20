package telemetry

import (
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestMetricsRecordsNormalizedRequestEvidence(t *testing.T) {
	metrics := NewMetrics("test-release")
	observer := metrics.RequestObserver()

	observer.Start("GET")
	observer.Finish("/repos/:id", "GET", "2xx", 25*time.Millisecond, 128)

	requestLabels := map[string]string{
		"method":       "GET",
		"release":      "test-release",
		"route":        "/repos/:id",
		"status_class": "2xx",
	}
	if got := counterValue(t, metrics.Gatherer(), "ai_efficiency_http_requests_total", requestLabels); got != 1 {
		t.Fatalf("http request count = %v, want 1", got)
	}
	requestDuration := histogramValue(t, metrics.Gatherer(), "ai_efficiency_http_request_duration_seconds", requestLabels)
	if requestDuration.GetSampleCount() != 1 || math.Abs(requestDuration.GetSampleSum()-0.025) > 0.000001 {
		t.Fatalf("request duration = count %d sum %v, want count 1 sum 0.025", requestDuration.GetSampleCount(), requestDuration.GetSampleSum())
	}
	responseBytes := histogramValue(t, metrics.Gatherer(), "ai_efficiency_http_response_bytes", requestLabels)
	if responseBytes.GetSampleCount() != 1 || responseBytes.GetSampleSum() != 128 {
		t.Fatalf("response bytes = count %d sum %v, want count 1 sum 128", responseBytes.GetSampleCount(), responseBytes.GetSampleSum())
	}
	if got := gaugeValue(t, metrics.Gatherer(), "ai_efficiency_http_requests_in_flight", map[string]string{
		"method":  "GET",
		"release": "test-release",
	}); got != 0 {
		t.Fatalf("in-flight requests = %v, want 0 after Finish", got)
	}
}

func TestMetricsRecordsNormalizedDependencyEvidence(t *testing.T) {
	metrics := NewMetrics("test-release")
	metrics.DependencyObserver().Observe("relay", "http_request", "POST", "5xx", 40*time.Millisecond)

	labels := map[string]string{
		"dependency":   "relay",
		"method":       "POST",
		"operation":    "http_request",
		"release":      "test-release",
		"status_class": "5xx",
	}
	if got := counterValue(t, metrics.Gatherer(), "ai_efficiency_dependency_requests_total", labels); got != 1 {
		t.Fatalf("dependency request count = %v, want 1", got)
	}
	duration := histogramValue(t, metrics.Gatherer(), "ai_efficiency_dependency_request_duration_seconds", labels)
	if duration.GetSampleCount() != 1 || math.Abs(duration.GetSampleSum()-0.04) > 0.000001 {
		t.Fatalf("dependency duration = count %d sum %v, want count 1 sum 0.04", duration.GetSampleCount(), duration.GetSampleSum())
	}
}

func TestMetricsDurationHistogramsCoverConfiguredRuntimeBudgets(t *testing.T) {
	metrics := NewMetrics("test-release")
	requestObserver := metrics.RequestObserver()
	requestObserver.Start("GET")
	requestObserver.Finish("/usage", "GET", "2xx", 35*time.Second, 128)
	metrics.DependencyObserver().Observe("relay", "http_request", "GET", "2xx", 30*time.Second)

	requestDuration := histogramValue(t, metrics.Gatherer(), "ai_efficiency_http_request_duration_seconds", map[string]string{
		"method":       "GET",
		"release":      "test-release",
		"route":        "/usage",
		"status_class": "2xx",
	})
	if got := highestFiniteBucket(requestDuration); got < 35 {
		t.Fatalf("request duration highest finite bucket = %v seconds, want at least 35", got)
	}
	dependencyDuration := histogramValue(t, metrics.Gatherer(), "ai_efficiency_dependency_request_duration_seconds", map[string]string{
		"dependency":   "relay",
		"method":       "GET",
		"operation":    "http_request",
		"release":      "test-release",
		"status_class": "2xx",
	})
	if got := highestFiniteBucket(dependencyDuration); got < 30 {
		t.Fatalf("dependency duration highest finite bucket = %v seconds, want at least 30", got)
	}
}

func TestMetricsCacheRecorderUsesOnlyStableCacheAndOutcomeLabels(t *testing.T) {
	metrics := NewMetrics("test-release")
	recorder := metrics.CacheRecorder("work_items_counts")
	for _, outcome := range []string{"fresh", "miss", "refresh", "lease_acquired", "lease_wait", "lease_failed", "error"} {
		recorder.Record(outcome)
	}

	for _, outcome := range []string{"fresh", "miss", "stale", "error", "refresh", "lease_acquired", "lease_wait", "lease_failed"} {
		want := float64(1)
		if outcome == "stale" {
			want = 0
		}
		if got := counterValue(t, metrics.Gatherer(), "ai_efficiency_cache_events_total", map[string]string{
			"cache":   "work_items_counts",
			"outcome": outcome,
		}); got != want {
			t.Fatalf("cache outcome %s = %v, want %v", outcome, got, want)
		}
	}
}

func gatheredMetric(t *testing.T, gatherer prometheus.Gatherer, name string, labels map[string]string) *dto.Metric {
	t.Helper()
	families, err := gatherer.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if metricLabelsMatch(metric, labels) {
				return metric
			}
		}
		t.Fatalf("metric %s has no sample for labels %#v", name, labels)
	}
	t.Fatalf("metric family %s not found", name)
	return nil
}

func metricLabelsMatch(metric *dto.Metric, want map[string]string) bool {
	if len(metric.GetLabel()) != len(want) {
		return false
	}
	for _, pair := range metric.GetLabel() {
		if want[pair.GetName()] != pair.GetValue() {
			return false
		}
	}
	return true
}

func counterValue(t *testing.T, gatherer prometheus.Gatherer, name string, labels map[string]string) float64 {
	t.Helper()
	return gatheredMetric(t, gatherer, name, labels).GetCounter().GetValue()
}

func gaugeValue(t *testing.T, gatherer prometheus.Gatherer, name string, labels map[string]string) float64 {
	t.Helper()
	return gatheredMetric(t, gatherer, name, labels).GetGauge().GetValue()
}

func histogramValue(t *testing.T, gatherer prometheus.Gatherer, name string, labels map[string]string) *dto.Histogram {
	t.Helper()
	return gatheredMetric(t, gatherer, name, labels).GetHistogram()
}

func highestFiniteBucket(histogram *dto.Histogram) float64 {
	buckets := histogram.GetBucket()
	if len(buckets) == 0 {
		return 0
	}
	return buckets[len(buckets)-1].GetUpperBound()
}
