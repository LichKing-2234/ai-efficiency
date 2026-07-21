package telemetry

import (
	"math"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestTeamUsagePrewarmMetricsPreinitializeClosedPrivacySafeLabels(t *testing.T) {
	metrics := NewMetrics("test-release")
	recorder, err := metrics.TeamUsagePrewarmRecorder([]string{"UTC", "Asia/Shanghai"})
	if err != nil {
		t.Fatalf("TeamUsagePrewarmRecorder() error = %v", err)
	}

	families, err := metrics.Gatherer().Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	wantFamilies := map[string]bool{
		"ai_efficiency_team_usage_prewarm_cycle_total":                    false,
		"ai_efficiency_team_usage_prewarm_cycle_duration_seconds":         false,
		"ai_efficiency_team_usage_prewarm_source_duration_seconds":        false,
		"ai_efficiency_team_usage_prewarm_source_bytes":                   false,
		"ai_efficiency_team_usage_prewarm_source_points":                  false,
		"ai_efficiency_team_usage_prewarm_source_users":                   false,
		"ai_efficiency_team_usage_prewarm_redis_duration_seconds":         false,
		"ai_efficiency_team_usage_prewarm_redis_bytes":                    false,
		"ai_efficiency_team_usage_prewarm_request_total":                  false,
		"ai_efficiency_team_usage_prewarm_last_success_timestamp_seconds": false,
	}
	for _, family := range families {
		if _, ok := wantFamilies[family.GetName()]; !ok {
			continue
		}
		wantFamilies[family.GetName()] = true
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				switch label.GetName() {
				case "class", "timezone", "outcome", "operation", "fallback_reason":
				default:
					t.Fatalf("metric %s exposes unexpected label %q", family.GetName(), label.GetName())
				}
				if label.GetName() == "timezone" && label.GetValue() != "UTC" && label.GetValue() != "Asia/Shanghai" {
					t.Fatalf("metric %s timezone = %q, want configured bounded value", family.GetName(), label.GetValue())
				}
			}
		}
	}
	for family, seen := range wantFamilies {
		if !seen {
			t.Errorf("preinitialized metric family %q missing", family)
		}
	}

	before := prewarmMetricLabelSets(t, metrics, "ai_efficiency_team_usage_prewarm_request_total")
	recorder.RecordRequest("alice@example.com", "raw-cache-key", "credential")
	after := prewarmMetricLabelSets(t, metrics, "ai_efficiency_team_usage_prewarm_request_total")
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("invalid labels changed request series: before=%v after=%v", before, after)
	}
}

func TestTeamUsagePrewarmMetricsRecordBoundedDurationsSizesAndLastSuccess(t *testing.T) {
	metrics := NewMetrics("test-release")
	recorder, err := metrics.TeamUsagePrewarmRecorder([]string{"UTC"})
	if err != nil {
		t.Fatalf("TeamUsagePrewarmRecorder() error = %v", err)
	}
	at := time.Unix(1_721_440_000, 0).UTC()
	recorder.RecordCycle("moving", "UTC", "success", 25*time.Millisecond)
	recorder.RecordSource("today_hour", "UTC", "success", 40*time.Millisecond, 512, 8, 3)
	recorder.RecordRedis("manifest_read", "hit", 5*time.Millisecond, 128)
	recorder.RecordRequest("UTC", "full_hit", "none")
	recorder.SetLastSuccess("moving", "UTC", at)

	if got := counterValue(t, metrics.Gatherer(), "ai_efficiency_team_usage_prewarm_cycle_total", map[string]string{
		"class": "moving", "timezone": "UTC", "outcome": "success",
	}); got != 1 {
		t.Fatalf("cycle count = %v, want 1", got)
	}
	cycleDuration := histogramValue(t, metrics.Gatherer(), "ai_efficiency_team_usage_prewarm_cycle_duration_seconds", map[string]string{
		"class": "moving", "timezone": "UTC", "outcome": "success",
	})
	if cycleDuration.GetSampleCount() != 1 || math.Abs(cycleDuration.GetSampleSum()-0.025) > 0.000001 {
		t.Fatalf("cycle duration = count %d sum %v", cycleDuration.GetSampleCount(), cycleDuration.GetSampleSum())
	}
	if got := gaugeValue(t, metrics.Gatherer(), "ai_efficiency_team_usage_prewarm_last_success_timestamp_seconds", map[string]string{
		"class": "moving", "timezone": "UTC",
	}); got != float64(at.Unix()) {
		t.Fatalf("last success = %v, want %v", got, at.Unix())
	}
}

func TestTeamUsagePrewarmMetricsRejectInvalidTimezoneConfiguration(t *testing.T) {
	metrics := NewMetrics("test-release")
	if recorder, err := metrics.TeamUsagePrewarmRecorder([]string{"UTC", "Asia/Shanghai", "America/Los_Angeles", "Europe/Berlin", "Etc/UTC"}); err == nil || recorder != nil {
		t.Fatalf("TeamUsagePrewarmRecorder() = %v, %v, want maximum-four validation error", recorder, err)
	}
}

func prewarmMetricLabelSets(t *testing.T, metrics *Metrics, familyName string) []string {
	t.Helper()
	families, err := metrics.Gatherer().Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	var sets []string
	for _, family := range families {
		if family.GetName() != familyName {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := make([]string, 0, len(metric.GetLabel()))
			for _, label := range metric.GetLabel() {
				labels = append(labels, label.GetName()+"="+label.GetValue())
			}
			sort.Strings(labels)
			sets = append(sets, labels[0]+"|"+labels[1]+"|"+labels[2])
		}
	}
	sort.Strings(sets)
	return sets
}
