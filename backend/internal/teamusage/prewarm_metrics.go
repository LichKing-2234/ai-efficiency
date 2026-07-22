package teamusage

import "time"

type PrewarmQuantityKind string

const (
	PrewarmQuantityUnionUsers    PrewarmQuantityKind = "union_users"
	PrewarmQuantitySegmentBytes  PrewarmQuantityKind = "segment_bytes"
	PrewarmQuantityTimezoneBytes PrewarmQuantityKind = "timezone_bytes"
)

type PrewarmValidationCheck string

const (
	PrewarmValidationDirectoryPagination  PrewarmValidationCheck = "directory_pagination"
	PrewarmValidationProviderIDBound      PrewarmValidationCheck = "provider_id_bound"
	PrewarmValidationStatsExactCoverage   PrewarmValidationCheck = "stats_exact_coverage"
	PrewarmValidationRawTrendCompleteness PrewarmValidationCheck = "raw_trend_completeness"
	PrewarmValidationRawTrendCoverage     PrewarmValidationCheck = "raw_trend_coverage"
	PrewarmValidationRawTrendLimit        PrewarmValidationCheck = "raw_trend_limit"
)

type PrewarmValidationOutcome string

const (
	PrewarmValidationAccepted PrewarmValidationOutcome = "accepted"
	PrewarmValidationRejected PrewarmValidationOutcome = "rejected"
)

type PrewarmCacheKind string

const (
	PrewarmCacheManifest     PrewarmCacheKind = "manifest"
	PrewarmCacheCurrentStats PrewarmCacheKind = "current_stats"
	PrewarmCacheSegment      PrewarmCacheKind = "segment"
)

type PrewarmCacheOutcome string

const (
	PrewarmCacheFresh       PrewarmCacheOutcome = "fresh"
	PrewarmCacheStale       PrewarmCacheOutcome = "stale"
	PrewarmCacheMiss        PrewarmCacheOutcome = "miss"
	PrewarmCacheInvalid     PrewarmCacheOutcome = "invalid"
	PrewarmCacheHardExpired PrewarmCacheOutcome = "hard_expired"
	PrewarmCacheError       PrewarmCacheOutcome = "error"
)

type PrewarmRedisErrorClass string

const (
	PrewarmRedisErrorValidation        PrewarmRedisErrorClass = "validation"
	PrewarmRedisErrorCallerCanceled    PrewarmRedisErrorClass = "caller_canceled"
	PrewarmRedisErrorCommandDeadline   PrewarmRedisErrorClass = "command_deadline"
	PrewarmRedisErrorNetworkTimeout    PrewarmRedisErrorClass = "network_timeout"
	PrewarmRedisErrorNetwork           PrewarmRedisErrorClass = "network_error"
	PrewarmRedisErrorCommand           PrewarmRedisErrorClass = "redis_command"
	PrewarmRedisErrorDecodeOrReference PrewarmRedisErrorClass = "decode_or_reference"
)

type PrewarmCycleClass string

const (
	PrewarmCycleMoving     PrewarmCycleClass = "moving"
	PrewarmCycleRecovery   PrewarmCycleClass = "recovery"
	PrewarmCycleStartup    PrewarmCycleClass = "startup"
	PrewarmCycleHistory29d PrewarmCycleClass = "history_29d"
	PrewarmCycleHistory6d  PrewarmCycleClass = "history_6d"
)

type PrewarmCycleOutcome string

const (
	PrewarmCycleSuccess   PrewarmCycleOutcome = "success"
	PrewarmCycleError     PrewarmCycleOutcome = "error"
	PrewarmCycleCanceled  PrewarmCycleOutcome = "canceled"
	PrewarmCycleRejected  PrewarmCycleOutcome = "rejected"
	PrewarmCycleLeaseBusy PrewarmCycleOutcome = "lease_busy"
)

type PrewarmBackgroundEvent struct {
	OperationID     string
	ProviderID      int
	ProviderVersion int64
	Timezone        string
	Class           PrewarmCycleClass
	Outcome         PrewarmCycleOutcome
	Duration        time.Duration
	Bytes           int
	Points          int
	Users           int
}

type PrewarmReporter interface {
	ReportPrewarmBackground(PrewarmBackgroundEvent)
}

// PrewarmMetrics is the bounded observability boundary shared by lifecycle,
// Redis, and request-time prewarm paths.
type PrewarmMetrics interface {
	RecordCycle(class, timezone, outcome string, duration time.Duration)
	RecordSource(class, timezone, outcome string, duration time.Duration, bytes, points, users int)
	RecordRedis(operation, outcome string, duration time.Duration, bytes int)
	RecordRedisError(operation string, class PrewarmRedisErrorClass)
	RecordRequest(timezone, outcome, fallbackReason string)
	SetLastSuccess(class, timezone string, at time.Time)
	RecordQuantity(kind PrewarmQuantityKind, timezone string, value int)
	SetGenerationBytes(value int)
	RecordValidation(check PrewarmValidationCheck, outcome PrewarmValidationOutcome)
	RecordCache(cache PrewarmCacheKind, outcome PrewarmCacheOutcome)
}

type noopPrewarmMetrics struct{}

func (noopPrewarmMetrics) RecordCycle(string, string, string, time.Duration) {}
func (noopPrewarmMetrics) RecordSource(string, string, string, time.Duration, int, int, int) {
}
func (noopPrewarmMetrics) RecordRedis(string, string, time.Duration, int)  {}
func (noopPrewarmMetrics) RecordRedisError(string, PrewarmRedisErrorClass) {}
func (noopPrewarmMetrics) RecordRequest(string, string, string)            {}
func (noopPrewarmMetrics) SetLastSuccess(string, string, time.Time)        {}
func (noopPrewarmMetrics) RecordQuantity(PrewarmQuantityKind, string, int) {}
func (noopPrewarmMetrics) SetGenerationBytes(int)                          {}
func (noopPrewarmMetrics) RecordValidation(PrewarmValidationCheck, PrewarmValidationOutcome) {
}
func (noopPrewarmMetrics) RecordCache(PrewarmCacheKind, PrewarmCacheOutcome) {}

type noopPrewarmReporter struct{}

func (noopPrewarmReporter) ReportPrewarmBackground(PrewarmBackgroundEvent) {}

func prewarmMetricsOrNoop(metrics PrewarmMetrics) PrewarmMetrics {
	if metrics == nil {
		return noopPrewarmMetrics{}
	}
	return metrics
}

func prewarmReporterOrNoop(reporter PrewarmReporter) PrewarmReporter {
	if reporter == nil {
		return noopPrewarmReporter{}
	}
	return reporter
}
