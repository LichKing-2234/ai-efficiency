package main

import (
	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/telemetry"
)

type productionCacheMetrics struct {
	personalUsage       readcache.Metrics
	providerMetadata    readcache.Metrics
	relayUserTrend      readcache.Metrics
	representativeScope readcache.Metrics
	repositoryInventory readcache.Metrics
	teamUsageSummary    readcache.Metrics
	teamUsageTrend      readcache.Metrics
	teamUsageMembers    readcache.Metrics
	teamUsageOrg        readcache.Metrics
	workItemsCounts     readcache.Metrics
}

func newProductionCacheMetrics(metrics *telemetry.Metrics) productionCacheMetrics {
	return productionCacheMetrics{
		personalUsage:       metrics.CacheRecorder("personal_usage"),
		providerMetadata:    metrics.CacheRecorder("provider_metadata"),
		relayUserTrend:      metrics.CacheRecorder("relay_user_trend"),
		representativeScope: metrics.CacheRecorder("representative_scope"),
		repositoryInventory: metrics.CacheRecorder("repository_inventory"),
		teamUsageSummary:    metrics.CacheRecorder("team_usage_summary"),
		teamUsageTrend:      metrics.CacheRecorder("team_usage_trend"),
		teamUsageMembers:    metrics.CacheRecorder("team_usage_members"),
		teamUsageOrg:        metrics.CacheRecorder("team_usage_organization"),
		workItemsCounts:     metrics.CacheRecorder("work_items_counts"),
	}
}

func (m productionCacheMetrics) recorders() map[string]readcache.Metrics {
	return map[string]readcache.Metrics{
		"personal_usage":          m.personalUsage,
		"provider_metadata":       m.providerMetadata,
		"relay_user_trend":        m.relayUserTrend,
		"representative_scope":    m.representativeScope,
		"repository_inventory":    m.repositoryInventory,
		"team_usage_summary":      m.teamUsageSummary,
		"team_usage_trend":        m.teamUsageTrend,
		"team_usage_members":      m.teamUsageMembers,
		"team_usage_organization": m.teamUsageOrg,
		"work_items_counts":       m.workItemsCounts,
	}
}
