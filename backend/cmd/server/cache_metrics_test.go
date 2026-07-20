package main

import (
	"sort"
	"testing"

	"github.com/ai-efficiency/backend/internal/telemetry"
)

func TestProductionCacheMetricsBindStablePrivacySafeNames(t *testing.T) {
	metrics := telemetry.NewMetrics("test-release")
	cacheMetrics := newProductionCacheMetrics(metrics)
	recorders := cacheMetrics.recorders()
	wantNames := []string{
		"personal_usage",
		"provider_metadata",
		"representative_scope",
		"repository_inventory",
		"team_usage_summary",
		"team_usage_trend",
		"team_usage_members",
		"team_usage_organization",
		"team_usage_origin",
		"work_items_counts",
	}
	sort.Strings(wantNames)
	gotNames := make([]string, 0, len(recorders))
	for name, recorder := range recorders {
		gotNames = append(gotNames, name)
		recorder.Record("fresh")
	}
	sort.Strings(gotNames)
	if len(gotNames) != len(wantNames) {
		t.Fatalf("cache metric names = %v, want %v", gotNames, wantNames)
	}
	for index := range wantNames {
		if gotNames[index] != wantNames[index] {
			t.Fatalf("cache metric names = %v, want %v", gotNames, wantNames)
		}
	}

	families, err := metrics.Gatherer().Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	seen := make(map[string]bool, len(wantNames))
	for _, family := range families {
		if family.GetName() != "ai_efficiency_cache_events_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := metric.GetLabel()
			if len(labels) != 2 {
				t.Fatalf("cache metric labels = %v, want exactly cache/outcome", labels)
			}
			values := make(map[string]string, 2)
			for _, label := range labels {
				values[label.GetName()] = label.GetValue()
			}
			if values["outcome"] == "fresh" {
				seen[values["cache"]] = true
			}
		}
	}
	for _, name := range wantNames {
		if !seen[name] {
			t.Fatalf("cache metric %q was not gathered from production wiring", name)
		}
	}
}
