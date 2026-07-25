package teamusage

import "time"

type PrewarmRefreshOutcome string

const (
	PrewarmRefreshSuccess PrewarmRefreshOutcome = "success"
	PrewarmRefreshPartial PrewarmRefreshOutcome = "partial"
	PrewarmRefreshSkipped PrewarmRefreshOutcome = "skipped"
	PrewarmRefreshError   PrewarmRefreshOutcome = "error"
)

func (value PrewarmRefreshOutcome) Valid() bool {
	for _, allowed := range AllPrewarmRefreshOutcomes() {
		if value == allowed {
			return true
		}
	}
	return false
}

func AllPrewarmRefreshOutcomes() []PrewarmRefreshOutcome {
	return []PrewarmRefreshOutcome{
		PrewarmRefreshSuccess,
		PrewarmRefreshPartial,
		PrewarmRefreshSkipped,
		PrewarmRefreshError,
	}
}

type PrewarmSourceClass string

const (
	PrewarmSourceDirectory    PrewarmSourceClass = "directory"
	PrewarmSourceCurrentStats PrewarmSourceClass = "current_stats"
	PrewarmSourceTodayHour    PrewarmSourceClass = "today_hour"
	PrewarmSourceHistory6d    PrewarmSourceClass = "history_6d"
	PrewarmSourceHistory29d   PrewarmSourceClass = "history_29d"
)

func (value PrewarmSourceClass) Valid() bool {
	for _, allowed := range AllPrewarmSourceClasses() {
		if value == allowed {
			return true
		}
	}
	return false
}

func AllPrewarmSourceClasses() []PrewarmSourceClass {
	return []PrewarmSourceClass{
		PrewarmSourceDirectory,
		PrewarmSourceCurrentStats,
		PrewarmSourceTodayHour,
		PrewarmSourceHistory6d,
		PrewarmSourceHistory29d,
	}
}

type PrewarmSourceOutcome string

const (
	PrewarmSourceSuccess  PrewarmSourceOutcome = "success"
	PrewarmSourceError    PrewarmSourceOutcome = "error"
	PrewarmSourceCanceled PrewarmSourceOutcome = "canceled"
	PrewarmSourceRejected PrewarmSourceOutcome = "rejected"
)

func (value PrewarmSourceOutcome) Valid() bool {
	for _, allowed := range AllPrewarmSourceOutcomes() {
		if value == allowed {
			return true
		}
	}
	return false
}

func AllPrewarmSourceOutcomes() []PrewarmSourceOutcome {
	return []PrewarmSourceOutcome{
		PrewarmSourceSuccess,
		PrewarmSourceError,
		PrewarmSourceCanceled,
		PrewarmSourceRejected,
	}
}

type PrewarmReadOutcome string

const (
	PrewarmReadFullHit    PrewarmReadOutcome = "full_hit"
	PrewarmReadMiss       PrewarmReadOutcome = "miss"
	PrewarmReadIneligible PrewarmReadOutcome = "ineligible"
	PrewarmReadInvalid    PrewarmReadOutcome = "invalid"
	PrewarmReadFallback   PrewarmReadOutcome = "fallback"
)

func (value PrewarmReadOutcome) Valid() bool {
	for _, allowed := range AllPrewarmReadOutcomes() {
		if value == allowed {
			return true
		}
	}
	return false
}

func AllPrewarmReadOutcomes() []PrewarmReadOutcome {
	return []PrewarmReadOutcome{
		PrewarmReadFullHit,
		PrewarmReadMiss,
		PrewarmReadIneligible,
		PrewarmReadInvalid,
		PrewarmReadFallback,
	}
}

type RefreshReport struct {
	Outcome        PrewarmRefreshOutcome
	Duration       time.Duration
	PlannedLanes   int
	PublishedLanes int
	SourceCounts   map[PrewarmSourceClass]int
}

type RefreshReporter interface {
	ReportRefresh(RefreshReport)
}

type PrewarmMetrics interface {
	RecordRefresh(PrewarmRefreshOutcome, time.Duration)
	SetLaneLastSuccess(string, time.Time)
	RecordSource(PrewarmSourceClass, PrewarmSourceOutcome, time.Duration)
	RecordRequest(PrewarmReadOutcome)
}

type noopPrewarmMetrics struct{}

func (noopPrewarmMetrics) RecordRefresh(PrewarmRefreshOutcome, time.Duration) {}
func (noopPrewarmMetrics) SetLaneLastSuccess(string, time.Time)               {}
func (noopPrewarmMetrics) RecordSource(PrewarmSourceClass, PrewarmSourceOutcome, time.Duration) {
}
func (noopPrewarmMetrics) RecordRequest(PrewarmReadOutcome) {}

type noopRefreshReporter struct{}

func (noopRefreshReporter) ReportRefresh(RefreshReport) {}

func prewarmMetricsOrNoop(metrics PrewarmMetrics) PrewarmMetrics {
	if metrics == nil {
		return noopPrewarmMetrics{}
	}
	return metrics
}

func refreshReporterOrNoop(reporter RefreshReporter) RefreshReporter {
	if reporter == nil {
		return noopRefreshReporter{}
	}
	return reporter
}
