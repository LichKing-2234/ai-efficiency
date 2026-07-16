package telemetry

import (
	"math"
	"strings"
	"testing"
)

func TestNormalizeBrowserRouteUsesClosedTemplates(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{raw: "/usage", want: "/usage"},
		{raw: "/usage/members/42?email=alice@example.com#private", want: "/usage/members/:user_id"},
		{raw: "/repos/9001?token=private", want: "/repos/:id"},
		{raw: "/admin/directory/offboarding/", want: "/admin/directory/offboarding"},
		{raw: "/private/alice@example.com", want: "unmatched"},
		{raw: "https://app.example.com/repos/7", want: "unmatched"},
		{raw: strings.Repeat("/a", 200), want: "unmatched"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			if got := NormalizeBrowserRoute(tt.raw); got != tt.want {
				t.Fatalf("NormalizeBrowserRoute(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestMetricsRecordsWebVitalsWithServerReleaseAndCorrectUnits(t *testing.T) {
	metrics := NewMetrics("server-release")
	for _, sample := range []WebVitalSample{
		{Metric: "LCP", Value: 2500, Route: "/repos/44?private=yes", NavigationType: "navigate"},
		{Metric: "INP", Value: 200, Route: "/repos/:id", NavigationType: "reload"},
		{Metric: "TTFB", Value: 800, Route: "/repos/:id", NavigationType: "back-forward"},
		{Metric: "CLS", Value: 0.12, Route: "/repos/:id", NavigationType: "back-forward-cache"},
	} {
		if err := metrics.ObserveWebVital(sample); err != nil {
			t.Fatalf("ObserveWebVital(%+v) error = %v", sample, err)
		}
	}

	for metric, wantSeconds := range map[string]float64{"LCP": 2.5, "INP": 0.2, "TTFB": 0.8} {
		navigationType := map[string]string{"LCP": "navigate", "INP": "reload", "TTFB": "back-forward"}[metric]
		histogram := histogramValue(t, metrics.Gatherer(), "ai_efficiency_browser_web_vital_seconds", map[string]string{
			"metric":          metric,
			"navigation_type": navigationType,
			"release":         "server-release",
			"route":           "/repos/:id",
		})
		if histogram.GetSampleCount() != 1 || math.Abs(histogram.GetSampleSum()-wantSeconds) > 0.000001 {
			t.Fatalf("%s histogram = count %d sum %v, want count 1 sum %v", metric, histogram.GetSampleCount(), histogram.GetSampleSum(), wantSeconds)
		}
	}
	cls := histogramValue(t, metrics.Gatherer(), "ai_efficiency_browser_web_vital_ratio", map[string]string{
		"metric":          "CLS",
		"navigation_type": "back-forward-cache",
		"release":         "server-release",
		"route":           "/repos/:id",
	})
	if cls.GetSampleCount() != 1 || cls.GetSampleSum() != 0.12 {
		t.Fatalf("CLS histogram = count %d sum %v, want count 1 sum 0.12", cls.GetSampleCount(), cls.GetSampleSum())
	}
}

func TestMetricsRejectsInvalidWebVitalSamplesWithoutCreatingLabels(t *testing.T) {
	metrics := NewMetrics("test-release")
	tests := []WebVitalSample{
		{Metric: "FID", Value: 1, Route: "/usage", NavigationType: "navigate"},
		{Metric: "LCP", Value: -1, Route: "/usage", NavigationType: "navigate"},
		{Metric: "LCP", Value: math.NaN(), Route: "/usage", NavigationType: "navigate"},
		{Metric: "CLS", Value: 11, Route: "/usage", NavigationType: "navigate"},
		{Metric: "LCP", Value: 600001, Route: "/usage", NavigationType: "navigate"},
		{Metric: "LCP", Value: 1, Route: "/usage", NavigationType: "private-navigation"},
	}
	for _, sample := range tests {
		if err := metrics.ObserveWebVital(sample); err == nil {
			t.Fatalf("ObserveWebVital(%+v) error = nil, want validation error", sample)
		}
	}
	families, err := metrics.Gatherer().Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	for _, family := range families {
		if strings.HasPrefix(family.GetName(), "ai_efficiency_browser_web_vital") && len(family.GetMetric()) > 0 {
			t.Fatalf("invalid samples created metric family %s", family.GetName())
		}
	}
}
