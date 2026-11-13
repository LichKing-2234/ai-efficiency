package kirocredits

import (
	"testing"
	"time"
)

func TestMatchKey(t *testing.T) {
	tests := []struct {
		name          string
		keys          []adminKey
		userID        int64
		wantFound     bool
		wantID        int64
		wantAmbiguity bool
	}{
		{
			name:      "modern key",
			keys:      []adminKey{{ID: 5, Name: "sub2api-1-alice@example.com", Enabled: true}},
			userID:    1,
			wantFound: true,
			wantID:    5,
		},
		{
			name:      "legacy key",
			keys:      []adminKey{{ID: 3, Name: "sub2api-user-1", Enabled: true}},
			userID:    1,
			wantFound: true,
			wantID:    3,
		},
		{
			name:      "user id prefix must not match a longer id",
			keys:      []adminKey{{ID: 6, Name: "sub2api-16-bob@example.com", Enabled: true}},
			userID:    1,
			wantFound: false,
		},
		{
			name:      "unrelated admin key is ignored",
			keys:      []adminKey{{ID: 1, Name: "claude-code", Enabled: true}},
			userID:    1,
			wantFound: false,
		},
		{
			name: "duplicate keys resolve to the newest and are flagged",
			keys: []adminKey{
				{ID: 5, Name: "sub2api-1-alice@example.com"},
				{ID: 9, Name: "sub2api-1-alice@example.com"},
			},
			userID:        1,
			wantFound:     true,
			wantID:        9,
			wantAmbiguity: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			key, found, ambiguous := matchKey(test.keys, test.userID)
			if found != test.wantFound {
				t.Fatalf("found = %v, want %v", found, test.wantFound)
			}
			if found && key.ID != test.wantID {
				t.Fatalf("key id = %d, want %d", key.ID, test.wantID)
			}
			if ambiguous != test.wantAmbiguity {
				t.Fatalf("ambiguous = %v, want %v", ambiguous, test.wantAmbiguity)
			}
		})
	}
}

func TestParseWindowUsesCallerTimezone(t *testing.T) {
	now := time.Date(2026, 11, 13, 3, 0, 0, 0, time.UTC)
	window, err := parseWindow(Params{StartDate: "2026-11-13", EndDate: "2026-11-13", Timezone: "Asia/Shanghai"}, now)
	if err != nil {
		t.Fatalf("parseWindow: %v", err)
	}
	wantStart := time.Date(2026, 11, 12, 16, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 11, 13, 16, 0, 0, 0, time.UTC)
	if !window.start.Equal(wantStart) {
		t.Fatalf("start = %v, want %v", window.start, wantStart)
	}
	if !window.endExclusive.Equal(wantEnd) {
		t.Fatalf("end = %v, want %v", window.endExclusive, wantEnd)
	}
}

func TestParseWindowDefaultsToLastSevenDays(t *testing.T) {
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	window, err := parseWindow(Params{Timezone: "UTC"}, now)
	if err != nil {
		t.Fatalf("parseWindow: %v", err)
	}
	if got := window.start.Format(dateLayout); got != "2026-11-07" {
		t.Fatalf("start = %s, want 2026-11-07", got)
	}
	if got := window.endExclusive.Format(dateLayout); got != "2026-11-14" {
		t.Fatalf("end = %s, want 2026-11-14", got)
	}
}

func TestAggregateRecordsBucketsByModelAndTimezoneDay(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	window := window{
		start:        time.Date(2026, 11, 11, 16, 0, 0, 0, time.UTC),
		endExclusive: time.Date(2026, 11, 13, 16, 0, 0, 0, time.UTC),
		location:     location,
	}
	credits := 0.5
	saved := 0.25
	result := aggregateRecords(window, []usageRecord{
		{Model: "claude-opus-5.5", InputTokens: 100, OutputTokens: 10, CreditsUsed: &credits, CreditsSaved: &saved, CreatedAt: time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)},
		{Model: "claude-opus-5.5", InputTokens: 200, OutputTokens: 20, CreditsUsed: &credits, CreatedAt: time.Date(2026, 11, 12, 17, 0, 0, 0, time.UTC)},
		{Model: "auto", InputTokens: 50, OutputTokens: 5, CreatedAt: time.Date(2026, 11, 13, 1, 0, 0, 0, time.UTC)},
	})
	if result.requests != 3 {
		t.Fatalf("requests = %d, want 3", result.requests)
	}
	if result.credits != 1.0 {
		t.Fatalf("credits = %v, want 1.0", result.credits)
	}
	if result.creditsSaved != 0.25 {
		t.Fatalf("creditsSaved = %v, want 0.25", result.creditsSaved)
	}
	if !result.missingCredits {
		t.Fatal("missingCredits = false, want true for a record without metering")
	}
	if len(result.byModel) != 2 || result.byModel[0].Model != "claude-opus-5.5" || result.byModel[0].Credits != 1.0 {
		t.Fatalf("byModel = %+v", result.byModel)
	}
	if len(result.byDay) != 2 {
		t.Fatalf("byDay = %+v", result.byDay)
	}
	if result.byDay[0].Date != "2026-11-12" || result.byDay[0].Requests != 1 {
		t.Fatalf("day buckets = %+v", result.byDay)
	}
	if result.byDay[1].Date != "2026-11-13" || result.byDay[1].Requests != 2 {
		t.Fatalf("day buckets = %+v", result.byDay)
	}
}
