package main

import (
	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/telemetry"
)

type productionCacheMetrics struct {
	personalUsage       readcache.Metrics
	providerMetadata    readcache.Metrics
	representativeScope readcache.Metrics
	repositoryInventory readcache.Metrics
	teamUsageOverview   readcache.Metrics
	teamUsageSummary    readcache.Metrics
	teamUsageTrend      readcache.Metrics
	workItemsCounts     readcache.Metrics
}

func newProductionCacheMetrics(metrics *telemetry.Metrics) productionCacheMetrics {
	return productionCacheMetrics{
		personalUsage:       metrics.CacheRecorder("personal_usage"),
		providerMetadata:    metrics.CacheRecorder("provider_metadata"),
		representativeScope: metrics.CacheRecorder("representative_scope"),
		repositoryInventory: metrics.CacheRecorder("repository_inventory"),
		teamUsageOverview:   metrics.CacheRecorder("team_usage_overview"),
		teamUsageSummary:    metrics.CacheRecorder("team_usage_summary"),
		teamUsageTrend:      metrics.CacheRecorder("team_usage_trend"),
		workItemsCounts:     metrics.CacheRecorder("work_items_counts"),
	}
}

func (m productionCacheMetrics) recorders() map[string]readcache.Metrics {
	return map[string]readcache.Metrics{
		"personal_usage":       m.personalUsage,
		"provider_metadata":    m.providerMetadata,
		"representative_scope": m.representativeScope,
		"repository_inventory": m.repositoryInventory,
		"team_usage_overview":  m.teamUsageOverview,
		"team_usage_summary":   m.teamUsageSummary,
		"team_usage_trend":     m.teamUsageTrend,
		"work_items_counts":    m.workItemsCounts,
	}
}
