package main

import (
	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/teamusage"
	"github.com/ai-efficiency/backend/internal/telemetry"
)

type productionCacheMetrics struct {
	metrics             *telemetry.Metrics
	personalUsage       readcache.Metrics
	providerMetadata    readcache.Metrics
	representativeScope readcache.Metrics
	repositoryInventory readcache.Metrics
	teamUsageSummary    readcache.Metrics
	teamUsageTrend      readcache.Metrics
	teamUsageMembers    readcache.Metrics
	teamUsageOrg        readcache.Metrics
	teamUsageOrigin     readcache.Metrics
	workItemsCounts     readcache.Metrics
}

func newProductionCacheMetrics(metrics *telemetry.Metrics) productionCacheMetrics {
	return productionCacheMetrics{
		metrics:             metrics,
		personalUsage:       metrics.CacheRecorder("personal_usage"),
		providerMetadata:    metrics.CacheRecorder("provider_metadata"),
		representativeScope: metrics.CacheRecorder("representative_scope"),
		repositoryInventory: metrics.CacheRecorder("repository_inventory"),
		teamUsageSummary:    metrics.CacheRecorder("team_usage_summary"),
		teamUsageTrend:      metrics.CacheRecorder("team_usage_trend"),
		teamUsageMembers:    metrics.CacheRecorder("team_usage_members"),
		teamUsageOrg:        metrics.CacheRecorder("team_usage_organization"),
		teamUsageOrigin:     metrics.CacheRecorder("team_usage_origin"),
		workItemsCounts:     metrics.CacheRecorder("work_items_counts"),
	}
}

func (m productionCacheMetrics) teamUsagePrewarm(timezones []string) (teamusage.PrewarmMetrics, error) {
	return m.metrics.TeamUsagePrewarmRecorder(timezones)
}

func (m productionCacheMetrics) newTeamUsagePrewarmReader(cache *teamusage.PrewarmCache) (*teamusage.PrewarmReader, error) {
	metrics, err := m.teamUsagePrewarm(nil)
	if err != nil {
		return nil, err
	}
	return teamusage.NewPrewarmReader(cache, teamusage.PrewarmReaderOptions{Metrics: metrics})
}

func (m productionCacheMetrics) recorders() map[string]readcache.Metrics {
	return map[string]readcache.Metrics{
		"personal_usage":          m.personalUsage,
		"provider_metadata":       m.providerMetadata,
		"representative_scope":    m.representativeScope,
		"repository_inventory":    m.repositoryInventory,
		"team_usage_summary":      m.teamUsageSummary,
		"team_usage_trend":        m.teamUsageTrend,
		"team_usage_members":      m.teamUsageMembers,
		"team_usage_organization": m.teamUsageOrg,
		"team_usage_origin":       m.teamUsageOrigin,
		"work_items_counts":       m.workItemsCounts,
	}
}
