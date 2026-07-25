package telemetry

import (
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/teamusage"
)

func TestTeamUsagePrewarmMetricsPreinitializeClosedPrivacySafeLabels(t *testing.T) {
	metrics := NewMetrics("test-release")
	recorder, err := metrics.TeamUsagePrewarmRecorder([]string{"UTC", "Asia/Shanghai", "America/Los_Angeles", "Europe/Berlin"})
	if err != nil {
		t.Fatalf("TeamUsagePrewarmRecorder() error = %v", err)
	}

	wantFamilies := map[string][]string{
		"ai_efficiency_team_usage_prewarm_refresh_total":                       {"outcome"},
		"ai_efficiency_team_usage_prewarm_refresh_duration_seconds":            {},
		"ai_efficiency_team_usage_prewarm_lane_last_success_timestamp_seconds": {"timezone"},
		"ai_efficiency_team_usage_prewarm_source_duration_seconds":             {"source", "outcome"},
		"ai_efficiency_team_usage_prewarm_request_total":                       {"outcome"},
	}
	for _, labels := range wantFamilies {
		sort.Strings(labels)
	}
	gotFamilies, gotTimezones := prewarmMetricFamilies(t, metrics)
	if !reflect.DeepEqual(gotFamilies, wantFamilies) {
		t.Fatalf("prewarm metric families = %v, want exactly %v", gotFamilies, wantFamilies)
	}
	if len(gotTimezones) > 4 {
		t.Fatalf("prewarm timezone label cardinality = %d, want at most 4: %v", len(gotTimezones), gotTimezones)
	}

	before := allPrewarmMetricLabelSets(t, metrics)
	recorder.RecordRefresh(teamusage.PrewarmRefreshOutcome("invalid"), -time.Second)
	recorder.SetLaneLastSuccess("alice@example.com", time.Unix(1_721_440_000, 0))
	recorder.RecordSource(
		teamusage.PrewarmSourceClass("raw-cache-key"),
		teamusage.PrewarmSourceOutcome("credential"),
		-time.Second,
	)
	recorder.RecordRequest(teamusage.PrewarmReadOutcome("credential"))
	after := allPrewarmMetricLabelSets(t, metrics)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("invalid typed values changed prewarm series: before=%v after=%v", before, after)
	}
}

func TestTeamUsagePrewarmMetricsRecordBoundedRefreshSourceRequestAndLaneSuccess(t *testing.T) {
	metrics := NewMetrics("test-release")
	recorder, err := metrics.TeamUsagePrewarmRecorder([]string{"UTC"})
	if err != nil {
		t.Fatalf("TeamUsagePrewarmRecorder() error = %v", err)
	}
	at := time.Unix(1_721_440_000, 0).UTC()
	recorder.RecordRefresh(teamusage.PrewarmRefreshSuccess, 25*time.Millisecond)
	recorder.RecordSource(teamusage.PrewarmSourceTodayHour, teamusage.PrewarmSourceSuccess, 40*time.Millisecond)
	recorder.RecordRequest(teamusage.PrewarmReadFullHit)
	recorder.SetLaneLastSuccess("UTC", at)

	if got := counterValue(t, metrics.Gatherer(), "ai_efficiency_team_usage_prewarm_refresh_total", map[string]string{
		"outcome": "success",
	}); got != 1 {
		t.Fatalf("refresh count = %v, want 1", got)
	}
	refreshDuration := histogramValue(t, metrics.Gatherer(), "ai_efficiency_team_usage_prewarm_refresh_duration_seconds", map[string]string{})
	if refreshDuration.GetSampleCount() != 1 || math.Abs(refreshDuration.GetSampleSum()-0.025) > 0.000001 {
		t.Fatalf("refresh duration = count %d sum %v", refreshDuration.GetSampleCount(), refreshDuration.GetSampleSum())
	}
	sourceDuration := histogramValue(t, metrics.Gatherer(), "ai_efficiency_team_usage_prewarm_source_duration_seconds", map[string]string{
		"source": "today_hour", "outcome": "success",
	})
	if sourceDuration.GetSampleCount() != 1 || math.Abs(sourceDuration.GetSampleSum()-0.04) > 0.000001 {
		t.Fatalf("source duration = count %d sum %v", sourceDuration.GetSampleCount(), sourceDuration.GetSampleSum())
	}
	if got := counterValue(t, metrics.Gatherer(), "ai_efficiency_team_usage_prewarm_request_total", map[string]string{
		"outcome": "full_hit",
	}); got != 1 {
		t.Fatalf("request count = %v, want 1", got)
	}
	if got := gaugeValue(t, metrics.Gatherer(), "ai_efficiency_team_usage_prewarm_lane_last_success_timestamp_seconds", map[string]string{
		"timezone": "UTC",
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

func prewarmMetricFamilies(t *testing.T, metrics *Metrics) (map[string][]string, map[string]struct{}) {
	t.Helper()
	families, err := metrics.Gatherer().Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	got := make(map[string][]string)
	timezones := make(map[string]struct{})
	for _, family := range families {
		if !strings.HasPrefix(family.GetName(), "ai_efficiency_team_usage_prewarm_") {
			continue
		}
		labels := make(map[string]struct{})
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = struct{}{}
				if label.GetName() == "timezone" {
					timezones[label.GetValue()] = struct{}{}
				}
			}
		}
		got[family.GetName()] = make([]string, 0, len(labels))
		for label := range labels {
			got[family.GetName()] = append(got[family.GetName()], label)
		}
		sort.Strings(got[family.GetName()])
	}
	return got, timezones
}

func allPrewarmMetricLabelSets(t *testing.T, metrics *Metrics) map[string][]string {
	t.Helper()
	families, err := metrics.Gatherer().Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	sets := make(map[string][]string)
	for _, family := range families {
		if !strings.HasPrefix(family.GetName(), "ai_efficiency_team_usage_prewarm_") {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := make([]string, 0, len(metric.GetLabel()))
			for _, label := range metric.GetLabel() {
				labels = append(labels, label.GetName()+"="+label.GetValue())
			}
			sort.Strings(labels)
			sets[family.GetName()] = append(sets[family.GetName()], strings.Join(labels, "|"))
		}
		sort.Strings(sets[family.GetName()])
	}
	return sets
}
