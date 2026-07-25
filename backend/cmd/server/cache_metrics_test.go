package main

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/teamusage"
	"github.com/ai-efficiency/backend/internal/telemetry"
	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
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

func TestProductionCacheMetricsConstructTeamUsagePrewarmMetricsOnlyOnDemand(t *testing.T) {
	metrics := telemetry.NewMetrics("test-release")
	cacheMetrics := newProductionCacheMetrics(metrics)
	before, err := metrics.Gatherer().Gather()
	if err != nil {
		t.Fatalf("Gather() before prewarm recorder error = %v", err)
	}
	for _, family := range before {
		if family.GetName() == "ai_efficiency_team_usage_prewarm_refresh_total" {
			t.Fatal("prewarm metrics constructed while feature path is disabled")
		}
	}

	recorder, err := cacheMetrics.teamUsagePrewarm([]string{"UTC"})
	if err != nil || recorder == nil {
		t.Fatalf("teamUsagePrewarm() recorder=%v error=%v", recorder, err)
	}
	after, err := metrics.Gatherer().Gather()
	if err != nil {
		t.Fatalf("Gather() after prewarm recorder error = %v", err)
	}
	for _, family := range after {
		if family.GetName() == "ai_efficiency_team_usage_prewarm_refresh_total" {
			return
		}
	}
	t.Fatal("prewarm metrics missing after enabled-path construction")
}

func TestProductionCacheMetricsConstructPrewarmReaderWithRequestMetricsOnly(t *testing.T) {
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	cache, err := teamusage.NewPrewarmCache(
		readcache.NewRedisStore(redisClient),
		teamusage.PrewarmCacheOptions{Namespace: "test"},
	)
	if err != nil {
		t.Fatalf("NewPrewarmCache() error = %v", err)
	}
	metrics := telemetry.NewMetrics("test-release")
	reader, err := newProductionCacheMetrics(metrics).newTeamUsagePrewarmReader(cache)
	if err != nil {
		t.Fatalf("newTeamUsagePrewarmReader() error = %v", err)
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	end := time.Now().In(location)
	origin, outcome, err := reader.ReadAuthorizedOrigin(context.Background(), teamusage.PrewarmReadRequest{
		ProviderID:      7,
		ProviderVersion: 11,
		Params: teamusage.OverviewParams{
			StartDate:   end.AddDate(0, 0, -29).Format("2006-01-02"),
			EndDate:     end.Format("2006-01-02"),
			Granularity: "day",
			Timezone:    "Asia/Shanghai",
		},
		AuthorizedRelayUserIDs: []int64{101},
	})
	if err != nil || origin != nil || outcome != teamusage.PrewarmReadMiss {
		t.Fatalf("ReadAuthorizedOrigin() = origin %v, outcome %q, error %v; want nil, miss, nil", origin, outcome, err)
	}

	families, err := metrics.Gatherer().Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	foundMiss := false
	for _, family := range families {
		switch family.GetName() {
		case "ai_efficiency_team_usage_prewarm_request_total":
			for _, metric := range family.GetMetric() {
				for _, label := range metric.GetLabel() {
					if label.GetName() == "outcome" && label.GetValue() == "miss" && metric.GetCounter().GetValue() == 1 {
						foundMiss = true
					}
				}
			}
		case "ai_efficiency_team_usage_prewarm_lane_last_success_timestamp_seconds":
			if len(family.GetMetric()) != 0 {
				t.Fatalf("Backend prewarm lane series = %v, want none", family.GetMetric())
			}
		}
	}
	if !foundMiss {
		t.Fatal("production-wired prewarm request miss counter = 0, want 1")
	}
}
