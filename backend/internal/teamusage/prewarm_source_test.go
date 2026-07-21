package teamusage

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
)

func TestPrewarmSourceBuildCurrentStatsExhaustsRosterBeforeChunks(t *testing.T) {
	provider := &prewarmSourceProvider{}
	provider.directory = relay.ProviderDirectoryResult{
		UserIDs:       descendingIDs(1001),
		ResponseBytes: 4096,
		PageCount:     2,
	}
	provider.statsFn = func(ids []int64) (relay.ProviderCurrentStatsResult, error) {
		provider.record("stats:" + fmt.Sprint(ids[0], "-", ids[len(ids)-1]))
		stats := make(map[int64]relay.TeamUserUsageStats, len(ids))
		for _, id := range ids {
			tokens := id * 10
			stats[id] = relay.TeamUserUsageStats{UserID: id, TodayActualCost: float64(id), TotalActualCost: float64(id * 2), TotalTokens: &tokens}
		}
		return relay.ProviderCurrentStatsResult{Stats: stats, ResponseBytes: int64(len(ids) * 8)}, nil
	}
	provider.directoryHook = func() { provider.record("directory-complete") }

	source := mustPrewarmSource(t, passthroughSourceLimiter{}, fixedGenerationOptions())
	got, err := source.BuildCurrentStats(context.Background(), prewarmBinding(provider))
	if err != nil {
		t.Fatalf("BuildCurrentStats() error = %v", err)
	}
	if got.RosterCount != 1001 || len(got.Stats) != 1001 {
		t.Fatalf("BuildCurrentStats() roster = %d/%d, want 1001", got.RosterCount, len(got.Stats))
	}
	for index, stat := range got.Stats {
		if stat.UserID != int64(index+1) {
			t.Fatalf("stats[%d].UserID = %d, want %d", index, stat.UserID, index+1)
		}
	}
	wantEvents := []string{"directory-complete", "stats:1-500", "stats:501-1000", "stats:1001-1001"}
	if !reflect.DeepEqual(provider.eventsCopy(), wantEvents) {
		t.Fatalf("source events = %#v, want %#v", provider.eventsCopy(), wantEvents)
	}
	if got.ResponseBytes != 4096+500*8+500*8+8 {
		t.Fatalf("ResponseBytes = %d, want directory plus all stats chunks", got.ResponseBytes)
	}
	wantDigest := prewarmRosterDigest([]int64{1, 2, 10})
	if wantDigest != "521e8134c8efbc88efa1d96ee43182dafe35f72cd0b4ce7825b8c24212c3bb89" {
		t.Fatalf("length-delimited roster digest = %q, contract changed", wantDigest)
	}
	if got.RosterDigest != prewarmRosterDigest(ascendingIDs(1001)) {
		t.Fatalf("RosterDigest = %q, want digest of exact sorted roster", got.RosterDigest)
	}
}

func TestPrewarmSourceBuildCurrentStatsRequiresExactRosterCoverage(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[int64]relay.TeamUserUsageStats)
	}{
		{name: "missing", mutate: func(stats map[int64]relay.TeamUserUsageStats) { delete(stats, 2) }},
		{name: "extra", mutate: func(stats map[int64]relay.TeamUserUsageStats) { stats[9] = relay.TeamUserUsageStats{UserID: 9} }},
		{name: "embedded mismatch", mutate: func(stats map[int64]relay.TeamUserUsageStats) { value := stats[2]; value.UserID = 3; stats[2] = value }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &prewarmSourceProvider{directory: relay.ProviderDirectoryResult{UserIDs: []int64{1, 2}, PageCount: 1}}
			provider.statsFn = func([]int64) (relay.ProviderCurrentStatsResult, error) {
				stats := map[int64]relay.TeamUserUsageStats{1: {UserID: 1}, 2: {UserID: 2}}
				test.mutate(stats)
				return relay.ProviderCurrentStatsResult{Stats: stats}, nil
			}
			source := mustPrewarmSource(t, passthroughSourceLimiter{}, fixedGenerationOptions())
			if _, err := source.BuildCurrentStats(context.Background(), prewarmBinding(provider)); err == nil {
				t.Fatalf("BuildCurrentStats() error = nil, want %s coverage rejection", test.name)
			}
		})
	}
}

func TestPrewarmSourceBuildCurrentStatsRejectsExactUserLimitBeforeStats(t *testing.T) {
	provider := &prewarmSourceProvider{directory: relay.ProviderDirectoryResult{UserIDs: ascendingIDs(PrewarmTrendUserLimit), PageCount: 5}}
	provider.statsFn = func([]int64) (relay.ProviderCurrentStatsResult, error) {
		t.Fatal("stats source called for rejected 5,000-ID roster")
		return relay.ProviderCurrentStatsResult{}, nil
	}
	source := mustPrewarmSource(t, passthroughSourceLimiter{}, fixedGenerationOptions())
	if _, err := source.BuildCurrentStats(context.Background(), prewarmBinding(provider)); err == nil {
		t.Fatal("BuildCurrentStats() error = nil, want exact-5,000 rejection")
	}
}

func TestPrewarmSourceFetchSegmentUsesProviderWideLimitAndValidates(t *testing.T) {
	provider := &prewarmSourceProvider{}
	provider.trendFn = func(params relay.TeamMemberTrendParams, limit int) (relay.ProviderWideTrendResult, error) {
		if limit != PrewarmTrendUserLimit {
			t.Fatalf("trend limit = %d, want %d", limit, PrewarmTrendUserLimit)
		}
		want := relay.TeamMemberTrendParams{StartDate: "2026-07-15", EndDate: "2026-07-20", Granularity: "day", Timezone: "UTC"}
		if params != want {
			t.Fatalf("trend params = %+v, want %+v", params, want)
		}
		return relay.ProviderWideTrendResult{
			Points: []relay.ProviderWideTrendPoint{
				{UserID: 1, Date: "2026-07-15", ActualCost: 1},
				{UserID: 999, Date: "2026-07-20", ActualCost: 2},
			},
			Coverage: params, ResponseBytes: 123, PointCount: 2, UniqueUserCount: 2, Complete: true,
		}, nil
	}
	source := mustPrewarmSource(t, passthroughSourceLimiter{}, fixedGenerationOptions())
	segment, err := source.FetchSegment(context.Background(), prewarmBinding(provider), "UTC", "2026-07-21", SegmentHistory6d)
	if err != nil {
		t.Fatalf("FetchSegment() error = %v", err)
	}
	if len(segment.Points) != 2 || segment.Points[1].UserID != 999 {
		t.Fatalf("FetchSegment() filtered provider-wide rows: %+v", segment.Points)
	}

	provider.trendFn = func(params relay.TeamMemberTrendParams, _ int) (relay.ProviderWideTrendResult, error) {
		return relay.ProviderWideTrendResult{
			Points:   []relay.ProviderWideTrendPoint{{UserID: 1, Date: "2026-07-20", ActualCost: 1}},
			Coverage: params, PointCount: 1, UniqueUserCount: 1, Complete: false,
		}, nil
	}
	if _, err := source.FetchSegment(context.Background(), prewarmBinding(provider), "UTC", "2026-07-21", SegmentHistory6d); err == nil {
		t.Fatal("FetchSegment(incomplete) error = nil, want validation rejection")
	}
}

func TestPrewarmSourceEveryRelayCallUsesSharedLimiter(t *testing.T) {
	limiter := &countingSourceLimiter{}
	provider := &prewarmSourceProvider{directory: relay.ProviderDirectoryResult{UserIDs: ascendingIDs(501), PageCount: 1}}
	provider.statsFn = func(ids []int64) (relay.ProviderCurrentStatsResult, error) {
		stats := make(map[int64]relay.TeamUserUsageStats, len(ids))
		for _, id := range ids {
			stats[id] = relay.TeamUserUsageStats{UserID: id}
		}
		return relay.ProviderCurrentStatsResult{Stats: stats}, nil
	}
	provider.trendFn = func(params relay.TeamMemberTrendParams, _ int) (relay.ProviderWideTrendResult, error) {
		return relay.ProviderWideTrendResult{Coverage: params, Complete: true}, nil
	}
	source := mustPrewarmSource(t, limiter, fixedGenerationOptions())
	if _, err := source.BuildCurrentStats(context.Background(), prewarmBinding(provider)); err != nil {
		t.Fatalf("BuildCurrentStats() error = %v", err)
	}
	if _, err := source.FetchSegment(context.Background(), prewarmBinding(provider), "UTC", "2026-07-21", SegmentTodayHour); err != nil {
		t.Fatalf("FetchSegment() error = %v", err)
	}
	if got, want := limiter.calls, 4; got != want {
		t.Fatalf("limiter calls = %d, want directory + two stats chunks + trend = %d", got, want)
	}
}

func TestPrewarmSourceMetricsRecordStructuredValidationChecks(t *testing.T) {
	metrics := &recordingPrewarmRequestMetrics{}
	provider := &prewarmSourceProvider{
		directory: relay.ProviderDirectoryResult{UserIDs: []int64{1}, PageCount: 1},
		statsFn: func([]int64) (relay.ProviderCurrentStatsResult, error) {
			return relay.ProviderCurrentStatsResult{Stats: map[int64]relay.TeamUserUsageStats{1: {UserID: 1}}}, nil
		},
		trendFn: func(params relay.TeamMemberTrendParams, _ int) (relay.ProviderWideTrendResult, error) {
			return relay.ProviderWideTrendResult{Coverage: params, Complete: true}, nil
		},
	}
	options := fixedGenerationOptions()
	options.Metrics = metrics
	source := mustPrewarmSource(t, passthroughSourceLimiter{}, options)
	if _, err := source.BuildCurrentStats(context.Background(), prewarmBinding(provider)); err != nil {
		t.Fatalf("BuildCurrentStats() error = %v", err)
	}
	if _, err := source.FetchSegment(context.Background(), prewarmBinding(provider), "UTC", "2026-07-21", SegmentTodayHour); err != nil {
		t.Fatalf("FetchSegment() error = %v", err)
	}
	wantAccepted := []prewarmValidationMetric{
		{check: PrewarmValidationDirectoryPagination, outcome: PrewarmValidationAccepted},
		{check: PrewarmValidationProviderIDBound, outcome: PrewarmValidationAccepted},
		{check: PrewarmValidationStatsExactCoverage, outcome: PrewarmValidationAccepted},
		{check: PrewarmValidationRawTrendCompleteness, outcome: PrewarmValidationAccepted},
		{check: PrewarmValidationRawTrendCoverage, outcome: PrewarmValidationAccepted},
		{check: PrewarmValidationRawTrendLimit, outcome: PrewarmValidationAccepted},
	}
	if !reflect.DeepEqual(metrics.validations, wantAccepted) {
		t.Fatalf("accepted validation metrics = %#v, want %#v", metrics.validations, wantAccepted)
	}

	provider.trendFn = func(params relay.TeamMemberTrendParams, _ int) (relay.ProviderWideTrendResult, error) {
		return relay.ProviderWideTrendResult{Coverage: params, Complete: false}, nil
	}
	if _, err := source.FetchSegment(context.Background(), prewarmBinding(provider), "UTC", "2026-07-21", SegmentTodayHour); err == nil {
		t.Fatal("FetchSegment(incomplete) error = nil")
	}
	last := metrics.validations[len(metrics.validations)-1]
	if last != (prewarmValidationMetric{check: PrewarmValidationRawTrendCompleteness, outcome: PrewarmValidationRejected}) {
		t.Fatalf("incomplete validation metric = %#v", last)
	}
}

type passthroughSourceLimiter struct{}

func (passthroughSourceLimiter) Do(ctx context.Context, call func(context.Context) error) error {
	return call(ctx)
}

type countingSourceLimiter struct{ calls int }

func (l *countingSourceLimiter) Do(ctx context.Context, call func(context.Context) error) error {
	l.calls++
	return call(ctx)
}

type prewarmSourceProvider struct {
	relay.Provider

	mu            sync.Mutex
	directory     relay.ProviderDirectoryResult
	directoryErr  error
	directoryHook func()
	statsFn       func([]int64) (relay.ProviderCurrentStatsResult, error)
	trendFn       func(relay.TeamMemberTrendParams, int) (relay.ProviderWideTrendResult, error)
	events        []string
}

func (p *prewarmSourceProvider) GetProviderUserIDs(context.Context) (relay.ProviderDirectoryResult, error) {
	if p.directoryHook != nil {
		p.directoryHook()
	}
	return p.directory, p.directoryErr
}

func (p *prewarmSourceProvider) GetProviderCurrentUsageStats(_ context.Context, ids []int64) (relay.ProviderCurrentStatsResult, error) {
	if p.statsFn == nil {
		return relay.ProviderCurrentStatsResult{}, errors.New("unexpected stats call")
	}
	return p.statsFn(append([]int64(nil), ids...))
}

func (p *prewarmSourceProvider) GetProviderUsageTrend(_ context.Context, params relay.TeamMemberTrendParams, limit int) (relay.ProviderWideTrendResult, error) {
	if p.trendFn == nil {
		return relay.ProviderWideTrendResult{}, errors.New("unexpected trend call")
	}
	return p.trendFn(params, limit)
}

func (p *prewarmSourceProvider) record(event string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, event)
}

func (p *prewarmSourceProvider) eventsCopy() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.events...)
}

func prewarmBinding(provider relay.Provider) ProviderBinding {
	return ProviderBinding{ProviderID: 7, ProviderVersion: 11, Provider: provider}
}

func fixedGenerationOptions() PrewarmSourceOptions {
	return PrewarmSourceOptions{
		Now:             func() time.Time { return time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC) },
		NewGenerationID: func() string { return strings.Repeat("a", 64) },
	}
}

func mustPrewarmSource(t *testing.T, limiter SourceCallLimiter, options PrewarmSourceOptions) *PrewarmSource {
	t.Helper()
	source, err := NewPrewarmSource(limiter, options)
	if err != nil {
		t.Fatalf("NewPrewarmSource() error = %v", err)
	}
	return source
}

func ascendingIDs(count int) []int64 {
	ids := make([]int64, count)
	for index := range ids {
		ids[index] = int64(index + 1)
	}
	return ids
}

func descendingIDs(count int) []int64 {
	ids := ascendingIDs(count)
	sort.Slice(ids, func(left, right int) bool { return ids[left] > ids[right] })
	return ids
}
