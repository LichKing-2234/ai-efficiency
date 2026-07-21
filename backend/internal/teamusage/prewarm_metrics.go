package teamusage

import "time"

// PrewarmMetrics is the bounded observability boundary shared by lifecycle,
// Redis, and request-time prewarm paths.
type PrewarmMetrics interface {
	RecordCycle(class, timezone, outcome string, duration time.Duration)
	RecordSource(class, timezone, outcome string, duration time.Duration, bytes, points, users int)
	RecordRedis(operation, outcome string, duration time.Duration, bytes int)
	RecordRequest(timezone, outcome, fallbackReason string)
	SetLastSuccess(class, timezone string, at time.Time)
}

type noopPrewarmMetrics struct{}

func (noopPrewarmMetrics) RecordCycle(string, string, string, time.Duration) {}
func (noopPrewarmMetrics) RecordSource(string, string, string, time.Duration, int, int, int) {
}
func (noopPrewarmMetrics) RecordRedis(string, string, time.Duration, int) {}
func (noopPrewarmMetrics) RecordRequest(string, string, string)           {}
func (noopPrewarmMetrics) SetLastSuccess(string, string, time.Time)       {}

func prewarmMetricsOrNoop(metrics PrewarmMetrics) PrewarmMetrics {
	if metrics == nil {
		return noopPrewarmMetrics{}
	}
	return metrics
}
