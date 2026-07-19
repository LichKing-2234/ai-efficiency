package relay

import (
	"context"
	"testing"
	"time"
)

func TestTeamTrendCacheReusesNormalizedSuccessfulResult(t *testing.T) {
	now := time.Date(2026, 7, 19, 8, 0, 0, 0, time.UTC)
	cache := teamTrendCache{now: func() time.Time { return now }}
	calls := 0
	load := func(context.Context) ([]UsageTrendPoint, error) {
		calls++
		tokens := int64(42)
		return []UsageTrendPoint{{Date: "2026-07-19", ActualCost: 1.5, TotalTokens: &tokens}}, nil
	}

	first, err := cache.GetOrLoad(context.Background(), 101, TeamMemberTrendParams{
		StartDate: " 2026-07-01 ", EndDate: "2026-07-19", Granularity: " day ", Timezone: " Asia/Shanghai ",
	}, load)
	if err != nil {
		t.Fatal(err)
	}
	second, err := cache.GetOrLoad(context.Background(), 101, TeamMemberTrendParams{
		StartDate: "2026-07-01", EndDate: " 2026-07-19 ", Granularity: "day", Timezone: "Asia/Shanghai",
	}, load)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(first) != 1 || len(second) != 1 {
		t.Fatalf("calls/results = %d/%d/%d, want 1/1/1", calls, len(first), len(second))
	}
	*first[0].TotalTokens = 99
	first[0].ActualCost = 9.9
	if *second[0].TotalTokens != 42 || second[0].ActualCost != 1.5 {
		t.Fatalf("cached result mutated through caller: %#v", second[0])
	}
}

func TestTeamTrendCacheExpiresAtSixtySeconds(t *testing.T) {
	now := time.Date(2026, 7, 19, 8, 0, 0, 0, time.UTC)
	cache := teamTrendCache{now: func() time.Time { return now }}
	calls := 0
	load := func(context.Context) ([]UsageTrendPoint, error) {
		calls++
		return []UsageTrendPoint{{Date: "2026-07-19", ActualCost: float64(calls)}}, nil
	}
	params := testTeamTrendCacheParams()

	first, err := cache.GetOrLoad(context.Background(), 101, params, load)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(teamTrendCacheTTL - time.Nanosecond)
	second, err := cache.GetOrLoad(context.Background(), 101, params, load)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Nanosecond)
	third, err := cache.GetOrLoad(context.Background(), 101, params, load)
	if err != nil {
		t.Fatal(err)
	}

	if calls != 2 || first[0].ActualCost != 1 || second[0].ActualCost != 1 || third[0].ActualCost != 2 {
		t.Fatalf("calls/costs = %d/%v/%v/%v, want 2/1/1/2", calls, first[0].ActualCost, second[0].ActualCost, third[0].ActualCost)
	}
}

func TestTeamTrendCacheStoresSuccessfulEmptyResult(t *testing.T) {
	cache := teamTrendCache{}
	calls := 0
	load := func(context.Context) ([]UsageTrendPoint, error) {
		calls++
		return nil, nil
	}

	for range 2 {
		points, err := cache.GetOrLoad(context.Background(), 101, testTeamTrendCacheParams(), load)
		if err != nil {
			t.Fatal(err)
		}
		if points != nil {
			t.Fatalf("points = %#v, want nil successful result", points)
		}
	}
	if calls != 1 {
		t.Fatalf("origin calls = %d, want 1", calls)
	}
}

func TestTeamTrendCacheSeparatesEveryIdentityDimension(t *testing.T) {
	cache := teamTrendCache{}
	base := testTeamTrendCacheParams()
	cases := []struct {
		userID int64
		params TeamMemberTrendParams
	}{
		{userID: 101, params: base},
		{userID: 102, params: base},
		{userID: 101, params: withTeamTrendStartDate(base, "2026-07-02")},
		{userID: 101, params: withTeamTrendEndDate(base, "2026-07-18")},
		{userID: 101, params: withTeamTrendGranularity(base, "hour")},
		{userID: 101, params: withTeamTrendTimezone(base, "UTC")},
	}
	calls := 0
	load := func(context.Context) ([]UsageTrendPoint, error) {
		calls++
		return []UsageTrendPoint{{Date: "2026-07-19", ActualCost: float64(calls)}}, nil
	}

	for _, test := range cases {
		if _, err := cache.GetOrLoad(context.Background(), test.userID, test.params, load); err != nil {
			t.Fatal(err)
		}
	}
	if calls != len(cases) {
		t.Fatalf("origin calls = %d, want %d distinct identities", calls, len(cases))
	}
}

func TestTeamTrendCacheBypassesNonPositiveUserIDs(t *testing.T) {
	cache := teamTrendCache{}
	calls := 0
	load := func(context.Context) ([]UsageTrendPoint, error) {
		calls++
		return []UsageTrendPoint{{Date: "2026-07-19"}}, nil
	}

	for range 2 {
		if _, err := cache.GetOrLoad(context.Background(), 0, testTeamTrendCacheParams(), load); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("origin calls = %d, want 2 uncached calls", calls)
	}
	if len(cache.entries) != 0 {
		t.Fatalf("cache entries = %d, want 0", len(cache.entries))
	}
}

func TestTeamTrendCacheNeverExceedsCapacity(t *testing.T) {
	now := time.Date(2026, 7, 19, 8, 0, 0, 0, time.UTC)
	cache := teamTrendCache{now: func() time.Time { return now }}
	params := testTeamTrendCacheParams()

	for index := 1; index <= teamTrendCacheCapacity+1; index++ {
		userID := int64(index)
		_, err := cache.GetOrLoad(context.Background(), userID, params, func(context.Context) ([]UsageTrendPoint, error) {
			return []UsageTrendPoint{{Date: "2026-07-19", ActualCost: float64(userID)}}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(cache.entries) > teamTrendCacheCapacity {
			t.Fatalf("cache entries after user %d = %d, maximum %d", userID, len(cache.entries), teamTrendCacheCapacity)
		}
		now = now.Add(time.Nanosecond)
	}
	if len(cache.entries) != teamTrendCacheCapacity {
		t.Fatalf("cache entries = %d, want %d", len(cache.entries), teamTrendCacheCapacity)
	}
	if _, ok := cache.entries[normalizedTeamTrendCacheKey(1, params)]; ok {
		t.Fatal("earliest-expiring entry was not evicted")
	}
}

func testTeamTrendCacheParams() TeamMemberTrendParams {
	return TeamMemberTrendParams{
		StartDate: "2026-07-01", EndDate: "2026-07-19", Granularity: "day", Timezone: "Asia/Shanghai",
	}
}

func withTeamTrendStartDate(params TeamMemberTrendParams, value string) TeamMemberTrendParams {
	params.StartDate = value
	return params
}

func withTeamTrendEndDate(params TeamMemberTrendParams, value string) TeamMemberTrendParams {
	params.EndDate = value
	return params
}

func withTeamTrendGranularity(params TeamMemberTrendParams, value string) TeamMemberTrendParams {
	params.Granularity = value
	return params
}

func withTeamTrendTimezone(params TeamMemberTrendParams, value string) TeamMemberTrendParams {
	params.Timezone = value
	return params
}
