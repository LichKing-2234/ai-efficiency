package telemetry

import (
	"fmt"
	"time"

	"github.com/ai-efficiency/backend/internal/teamusage"
	"github.com/prometheus/client_golang/prometheus"
)

type teamUsagePrewarmMetrics struct {
	timezones map[string]struct{}

	refreshTotal    *prometheus.CounterVec
	refreshDuration prometheus.Histogram
	laneLastSuccess *prometheus.GaugeVec
	sourceDuration  *prometheus.HistogramVec
	requestTotal    *prometheus.CounterVec
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
		refreshTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "team_usage_prewarm_refresh_total",
			Help:      "Completed Team Usage prewarm refresh outcomes.",
		}, []string{"outcome"}),
		refreshDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "team_usage_prewarm_refresh_duration_seconds",
			Help:      "Team Usage prewarm refresh duration in seconds.",
			Buckets:   prometheus.ExponentialBuckets(0.01, 2, 16),
		}),
		laneLastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "team_usage_prewarm_lane_last_success_timestamp_seconds",
			Help:      "Unix timestamp of the last successful Team Usage prewarm lane publication.",
		}, []string{"timezone"}),
		sourceDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "team_usage_prewarm_source_duration_seconds",
			Help:      "Team Usage prewarm source duration in seconds.",
			Buckets:   prometheus.ExponentialBuckets(0.05, 2, 13),
		}, []string{"source", "outcome"}),
		requestTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "team_usage_prewarm_request_total",
			Help:      "Team Usage request prewarm outcomes.",
		}, []string{"outcome"}),
	}
	for _, timezone := range normalized {
		recorder.timezones[timezone] = struct{}{}
	}
	m.registry.MustRegister(
		recorder.refreshTotal,
		recorder.refreshDuration,
		recorder.laneLastSuccess,
		recorder.sourceDuration,
		recorder.requestTotal,
	)
	recorder.preinitialize(normalized)
	return recorder, nil
}

func (m *teamUsagePrewarmMetrics) preinitialize(timezones []string) {
	for _, outcome := range teamusage.AllPrewarmRefreshOutcomes() {
		m.refreshTotal.WithLabelValues(string(outcome)).Add(0)
	}
	for _, timezone := range timezones {
		m.laneLastSuccess.WithLabelValues(timezone).Set(0)
	}
	for _, source := range teamusage.AllPrewarmSourceClasses() {
		for _, outcome := range teamusage.AllPrewarmSourceOutcomes() {
			m.sourceDuration.WithLabelValues(string(source), string(outcome))
		}
	}
	for _, outcome := range teamusage.AllPrewarmReadOutcomes() {
		m.requestTotal.WithLabelValues(string(outcome)).Add(0)
	}
}

func (m *teamUsagePrewarmMetrics) RecordRefresh(outcome teamusage.PrewarmRefreshOutcome, duration time.Duration) {
	if !outcome.Valid() {
		return
	}
	m.refreshTotal.WithLabelValues(string(outcome)).Inc()
	m.refreshDuration.Observe(nonnegativePrewarmDuration(duration).Seconds())
}

func (m *teamUsagePrewarmMetrics) SetLaneLastSuccess(timezone string, at time.Time) {
	if !m.validTimezone(timezone) || at.IsZero() {
		return
	}
	m.laneLastSuccess.WithLabelValues(timezone).Set(float64(at.Unix()))
}

func (m *teamUsagePrewarmMetrics) RecordSource(
	source teamusage.PrewarmSourceClass,
	outcome teamusage.PrewarmSourceOutcome,
	duration time.Duration,
) {
	if !source.Valid() || !outcome.Valid() {
		return
	}
	m.sourceDuration.WithLabelValues(string(source), string(outcome)).Observe(
		nonnegativePrewarmDuration(duration).Seconds(),
	)
}

func (m *teamUsagePrewarmMetrics) RecordRequest(outcome teamusage.PrewarmReadOutcome) {
	if !outcome.Valid() {
		return
	}
	m.requestTotal.WithLabelValues(string(outcome)).Inc()
}

func (m *teamUsagePrewarmMetrics) validTimezone(timezone string) bool {
	_, ok := m.timezones[timezone]
	return ok
}

func nonnegativePrewarmDuration(value time.Duration) time.Duration {
	if value < 0 {
		return 0
	}
	return value
}

var _ teamusage.PrewarmMetrics = (*teamUsagePrewarmMetrics)(nil)
