package relay

import (
	"errors"
	"fmt"
)

type ProviderSourceRejectionKind string

const (
	ProviderSourceRejectionDirectoryPagination  ProviderSourceRejectionKind = "directory_pagination"
	ProviderSourceRejectionProviderIDBound      ProviderSourceRejectionKind = "provider_id_bound"
	ProviderSourceRejectionStatsExactCoverage   ProviderSourceRejectionKind = "stats_exact_coverage"
	ProviderSourceRejectionRawTrendCoverage     ProviderSourceRejectionKind = "raw_trend_coverage"
	ProviderSourceRejectionRawTrendCompleteness ProviderSourceRejectionKind = "raw_trend_completeness"
	ProviderSourceRejectionRawTrendLimit        ProviderSourceRejectionKind = "raw_trend_limit"
)

type ProviderSourceRejection struct {
	kind ProviderSourceRejectionKind
	err  error
}

func (e *ProviderSourceRejection) Error() string {
	return fmt.Sprintf("relay provider source rejected (%s): %v", e.kind, e.err)
}

func (e *ProviderSourceRejection) Unwrap() error { return e.err }

func NewProviderSourceRejection(kind ProviderSourceRejectionKind, err error) error {
	if err == nil {
		err = errors.New("source validation failed")
	}
	if !validProviderSourceRejectionKind(kind) {
		return fmt.Errorf("relay provider source rejected with unknown kind %q: %w", kind, err)
	}
	return &ProviderSourceRejection{kind: kind, err: err}
}

func ProviderSourceRejectionKindOf(err error) (ProviderSourceRejectionKind, bool) {
	var rejection *ProviderSourceRejection
	if !errors.As(err, &rejection) || rejection == nil || !validProviderSourceRejectionKind(rejection.kind) {
		return "", false
	}
	return rejection.kind, true
}

func validProviderSourceRejectionKind(kind ProviderSourceRejectionKind) bool {
	switch kind {
	case ProviderSourceRejectionDirectoryPagination,
		ProviderSourceRejectionProviderIDBound,
		ProviderSourceRejectionStatsExactCoverage,
		ProviderSourceRejectionRawTrendCoverage,
		ProviderSourceRejectionRawTrendCompleteness,
		ProviderSourceRejectionRawTrendLimit:
		return true
	default:
		return false
	}
}
