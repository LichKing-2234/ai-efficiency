package teamusage

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
)

func TestPrewarmTimezoneDefaultsAndNormalization(t *testing.T) {
	wantDefaults := []string{"UTC", "Asia/Shanghai", "America/Los_Angeles", "Europe/Berlin"}
	if got := DefaultPrewarmTimezones(); !reflect.DeepEqual(got, wantDefaults) {
		t.Fatalf("DefaultPrewarmTimezones() = %#v, want %#v", got, wantDefaults)
	}

	got, err := NormalizePrewarmTimezones([]string{
		" Asia/Shanghai ", "UTC", "Asia/Shanghai", "Etc/UTC", " UTC ", "Europe/Berlin",
	})
	if err != nil {
		t.Fatalf("NormalizePrewarmTimezones() error = %v", err)
	}
	want := []string{"Asia/Shanghai", "UTC", "Etc/UTC", "Europe/Berlin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizePrewarmTimezones() = %#v, want trim/dedup/order %#v", got, want)
	}
	got[0] = "changed"
	if defaults := DefaultPrewarmTimezones(); defaults[0] != "UTC" {
		t.Fatalf("DefaultPrewarmTimezones() returned mutable shared storage: %#v", defaults)
	}

	empty, err := NormalizePrewarmTimezones([]string{"", "  "})
	if err != nil || len(empty) != 0 {
		t.Fatalf("NormalizePrewarmTimezones(blank) = %#v, %v, want empty success", empty, err)
	}
}

func TestPrewarmTimezoneNormalizationRejectsInvalidBounds(t *testing.T) {
	for _, test := range []struct {
		name  string
		zones []string
		part  string
	}{
		{name: "invalid IANA name", zones: []string{"Mars/Olympus"}, part: "invalid"},
		{name: "fifth zone", zones: []string{"UTC", "Asia/Shanghai", "America/Los_Angeles", "Europe/Berlin", "Asia/Tokyo"}, part: "four"},
		{name: "over 255 bytes", zones: []string{strings.Repeat("x", 256)}, part: "255"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NormalizePrewarmTimezones(test.zones); err == nil || !strings.Contains(err.Error(), test.part) {
				t.Fatalf("NormalizePrewarmTimezones() error = %v, want %q rejection", err, test.part)
			}
		})
	}
}

func TestPrewarmTimezoneRejectsLocalAndPreservesAliases(t *testing.T) {
	if _, err := NormalizePrewarmTimezones([]string{"Local"}); err == nil {
		t.Fatal("NormalizePrewarmTimezones(Local) error = nil, want environment-dependent timezone rejection")
	}
	aliases, err := NormalizePrewarmTimezones([]string{"UTC", "Etc/UTC", "UTC"})
	if err != nil {
		t.Fatalf("NormalizePrewarmTimezones(aliases) error = %v", err)
	}
	if want := []string{"UTC", "Etc/UTC"}; !reflect.DeepEqual(aliases, want) {
		t.Fatalf("NormalizePrewarmTimezones(aliases) = %#v, want distinct valid aliases %#v", aliases, want)
	}

	localParams := OverviewParams{
		StartDate: "2026-07-21", EndDate: "2026-07-21", Granularity: "hour", Timezone: "Local",
	}
	if _, _, err := RecognizePrewarmWindow(localParams, time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("RecognizePrewarmWindow(Local) error = nil, want environment-dependent timezone rejection")
	}
	if _, err := SplitSafe("Local", "2026-07-21"); err == nil {
		t.Fatal("SplitSafe(Local) error = nil, want environment-dependent timezone rejection")
	}

	segment := validPrewarmTestSegment(SegmentTodayHour, nil)
	segment.Timezone = "Local"
	segment.Coverage.Timezone = "Local"
	if err := ValidateTrendSegment(segment); err == nil {
		t.Fatal("ValidateTrendSegment(Local) error = nil, want shared location loader rejection")
	}
}

func TestPrewarmWindowRecognizesOnlyCurrentLocalStandardShapes(t *testing.T) {
	tests := []struct {
		name        string
		now         time.Time
		timezone    string
		start       string
		end         string
		granularity string
		wantClass   PrewarmWindowClass
		wantAnchor  string
	}{
		{name: "UTC today", now: time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC), timezone: "UTC", start: "2026-07-21", end: "2026-07-21", granularity: "hour", wantClass: PrewarmWindowToday, wantAnchor: "2026-07-21"},
		{name: "Shanghai seven days at rollover", now: time.Date(2026, 7, 20, 16, 0, 0, 0, time.UTC), timezone: "Asia/Shanghai", start: "2026-07-15", end: "2026-07-21", granularity: "day", wantClass: PrewarmWindow7d, wantAnchor: "2026-07-21"},
		{name: "Los Angeles thirty days before rollover", now: time.Date(2026, 7, 21, 6, 59, 59, 0, time.UTC), timezone: "America/Los_Angeles", start: "2026-06-21", end: "2026-07-20", granularity: "day", wantClass: PrewarmWindow30d, wantAnchor: "2026-07-20"},
		{name: "Los Angeles today at rollover", now: time.Date(2026, 7, 21, 7, 0, 0, 0, time.UTC), timezone: "America/Los_Angeles", start: "2026-07-21", end: "2026-07-21", granularity: "hour", wantClass: PrewarmWindowToday, wantAnchor: "2026-07-21"},
		{name: "Berlin seven days at rollover", now: time.Date(2026, 7, 20, 22, 0, 0, 0, time.UTC), timezone: "Europe/Berlin", start: "2026-07-15", end: "2026-07-21", granularity: "day", wantClass: PrewarmWindow7d, wantAnchor: "2026-07-21"},
		{name: "calendar AddDate across spring DST", now: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC), timezone: "America/Los_Angeles", start: "2026-02-08", end: "2026-03-09", granularity: "day", wantClass: PrewarmWindow30d, wantAnchor: "2026-03-09"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, recognized, err := RecognizePrewarmWindow(OverviewParams{
				StartDate: test.start, EndDate: test.end, Granularity: test.granularity, Timezone: test.timezone,
			}, test.now)
			if err != nil || !recognized {
				t.Fatalf("RecognizePrewarmWindow() = %#v, %v, %v, want recognized", got, recognized, err)
			}
			if got.Class != test.wantClass || got.AnchorDate != test.wantAnchor {
				t.Fatalf("RecognizePrewarmWindow() = %#v, want class %q anchor %q", got, test.wantClass, test.wantAnchor)
			}
			wantCoverage := (PrewarmCoverage{StartDate: test.start, EndDate: test.end, Granularity: test.granularity, Timezone: test.timezone})
			if got.Coverage != wantCoverage {
				t.Fatalf("window coverage = %#v, want %#v", got.Coverage, wantCoverage)
			}
		})
	}
}

func TestPrewarmWindowRejectsCustomAndMismatchedShapes(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name   string
		params OverviewParams
	}{
		{name: "today day", params: OverviewParams{StartDate: "2026-07-21", EndDate: "2026-07-21", Granularity: "day", Timezone: "UTC"}},
		{name: "seven day hour", params: OverviewParams{StartDate: "2026-07-15", EndDate: "2026-07-21", Granularity: "hour", Timezone: "UTC"}},
		{name: "six days", params: OverviewParams{StartDate: "2026-07-16", EndDate: "2026-07-21", Granularity: "day", Timezone: "UTC"}},
		{name: "old anchor", params: OverviewParams{StartDate: "2026-07-14", EndDate: "2026-07-20", Granularity: "day", Timezone: "UTC"}},
		{name: "custom range", params: OverviewParams{StartDate: "2026-07-01", EndDate: "2026-07-21", Granularity: "day", Timezone: "UTC"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got, recognized, err := RecognizePrewarmWindow(test.params, now); err != nil || recognized {
				t.Fatalf("RecognizePrewarmWindow() = %#v, %v, %v, want clean rejection", got, recognized, err)
			}
		})
	}
	if _, _, err := RecognizePrewarmWindow(OverviewParams{
		StartDate: "2026-07-21", EndDate: "2026-07-21", Granularity: "hour", Timezone: "Mars/Olympus",
	}, now); err == nil {
		t.Fatal("RecognizePrewarmWindow() error = nil, want invalid timezone rejection")
	}
}

func TestPrewarmSplitSafeRejectsDSTAndAcceptsAdjacentAnchors(t *testing.T) {
	for _, test := range []struct {
		name, timezone, anchor string
		wantSafe               bool
	}{
		{name: "UTC", timezone: "UTC", anchor: "2026-07-21", wantSafe: true},
		{name: "Shanghai", timezone: "Asia/Shanghai", anchor: "2026-07-21", wantSafe: true},
		{name: "LA spring rollover", timezone: "America/Los_Angeles", anchor: "2026-03-09"},
		{name: "LA spring before", timezone: "America/Los_Angeles", anchor: "2026-03-08", wantSafe: true},
		{name: "LA spring after", timezone: "America/Los_Angeles", anchor: "2026-03-10", wantSafe: true},
		{name: "LA fall rollover", timezone: "America/Los_Angeles", anchor: "2026-11-02"},
		{name: "LA fall before", timezone: "America/Los_Angeles", anchor: "2026-11-01", wantSafe: true},
		{name: "LA fall after", timezone: "America/Los_Angeles", anchor: "2026-11-03", wantSafe: true},
		{name: "Berlin spring rollover", timezone: "Europe/Berlin", anchor: "2026-03-30"},
		{name: "Berlin spring before", timezone: "Europe/Berlin", anchor: "2026-03-29", wantSafe: true},
		{name: "Berlin spring after", timezone: "Europe/Berlin", anchor: "2026-03-31", wantSafe: true},
		{name: "Berlin fall rollover", timezone: "Europe/Berlin", anchor: "2026-10-26"},
		{name: "Berlin fall before", timezone: "Europe/Berlin", anchor: "2026-10-25", wantSafe: true},
		{name: "Berlin fall after", timezone: "Europe/Berlin", anchor: "2026-10-27", wantSafe: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := SplitSafe(test.timezone, test.anchor)
			if err != nil || got != test.wantSafe {
				t.Fatalf("SplitSafe(%q, %q) = %v, %v, want %v", test.timezone, test.anchor, got, err, test.wantSafe)
			}
		})
	}
	for _, test := range []struct{ timezone, anchor string }{{"Mars/Olympus", "2026-07-21"}, {"UTC", "2026-7-21"}} {
		if _, err := SplitSafe(test.timezone, test.anchor); err == nil {
			t.Fatalf("SplitSafe(%q, %q) error = nil, want validation failure", test.timezone, test.anchor)
		}
	}
}

func TestPrewarmSegmentValidatesOpaqueLabelsCoverageAndBounds(t *testing.T) {
	tokens := int64(10)
	valid := validPrewarmTestSegment(SegmentHistory6d, []relay.ProviderWideTrendPoint{
		{UserID: 1, Date: "2026-07-15", ActualCost: 1, TotalTokens: &tokens},
		{UserID: 2, Date: "2026-99-99", ActualCost: 2},
		{UserID: 1, Date: "2026-07-20", ActualCost: 3, TotalTokens: &tokens},
	})
	if err := ValidateTrendSegment(valid); err != nil {
		t.Fatalf("ValidateTrendSegment(valid opaque labels) error = %v", err)
	}

	negativeTokens := int64(-1)
	for _, test := range []struct {
		name   string
		mutate func(*PrewarmTrendSegment)
	}{
		{name: "nonpositive user", mutate: func(s *PrewarmTrendSegment) { s.Points[0].UserID = 0 }},
		{name: "invalid day shape", mutate: func(s *PrewarmTrendSegment) { s.Points[0].Date = "2026-7-15" }},
		{name: "negative tokens", mutate: func(s *PrewarmTrendSegment) { s.Points[0].TotalTokens = &negativeTokens }},
		{name: "negative cost", mutate: func(s *PrewarmTrendSegment) { s.Points[0].ActualCost = -1 }},
		{name: "NaN cost", mutate: func(s *PrewarmTrendSegment) { s.Points[0].ActualCost = math.NaN() }},
		{name: "infinite cost", mutate: func(s *PrewarmTrendSegment) { s.Points[0].ActualCost = math.Inf(1) }},
		{name: "duplicate", mutate: func(s *PrewarmTrendSegment) { s.Points[2] = s.Points[0] }},
		{name: "out of source order", mutate: func(s *PrewarmTrendSegment) { s.Points[2].Date = "2026-07-14" }},
		{name: "start coverage", mutate: func(s *PrewarmTrendSegment) { s.Coverage.StartDate = "2026-07-14" }},
		{name: "end coverage", mutate: func(s *PrewarmTrendSegment) { s.Coverage.EndDate = "2026-07-21" }},
		{name: "granularity coverage", mutate: func(s *PrewarmTrendSegment) { s.Coverage.Granularity = "hour" }},
		{name: "timezone coverage", mutate: func(s *PrewarmTrendSegment) { s.Coverage.Timezone = "Asia/Shanghai" }},
		{name: "unknown class", mutate: func(s *PrewarmTrendSegment) { s.Class = "unknown" }},
		{name: "invalid anchor", mutate: func(s *PrewarmTrendSegment) { s.AnchorDate = "2026-7-21" }},
		{name: "metadata point count", mutate: func(s *PrewarmTrendSegment) { s.PointCount++ }},
		{name: "metadata user count", mutate: func(s *PrewarmTrendSegment) { s.UniqueUserCount++ }},
		{name: "incomplete", mutate: func(s *PrewarmTrendSegment) { s.Complete = false }},
		{name: "negative bytes", mutate: func(s *PrewarmTrendSegment) { s.ResponseBytes = -1 }},
		{name: "response byte bound", mutate: func(s *PrewarmTrendSegment) { s.ResponseBytes = PrewarmTrendResponseByteLimit }},
		{name: "point bound", mutate: func(s *PrewarmTrendSegment) { s.PointCount = PrewarmTrendPointLimit }},
	} {
		t.Run(test.name, func(t *testing.T) {
			segment := clonePrewarmTestSegment(valid)
			test.mutate(&segment)
			if err := ValidateTrendSegment(segment); err == nil {
				t.Fatalf("ValidateTrendSegment() error = nil, want %s rejection", test.name)
			}
		})
	}

	usersAtLimit := make([]relay.ProviderWideTrendPoint, PrewarmTrendUserLimit)
	for index := range usersAtLimit {
		usersAtLimit[index] = relay.ProviderWideTrendPoint{UserID: int64(index + 1), Date: "2026-07-15", ActualCost: 1}
	}
	if err := ValidateTrendSegment(validPrewarmTestSegment(SegmentHistory6d, usersAtLimit)); err == nil {
		t.Fatalf("ValidateTrendSegment() error = nil, want exact-%d user rejection", PrewarmTrendUserLimit)
	}
}

func TestPrewarmSegmentPreservesRawHourLabels(t *testing.T) {
	today := validPrewarmTestSegment(SegmentTodayHour, []relay.ProviderWideTrendPoint{
		{UserID: 1, Date: "2026-07-20 23:00", ActualCost: 1},
		{UserID: 1, Date: "2026-07-21 00:00", ActualCost: 2},
	})
	if err := ValidateTrendSegment(today); err != nil {
		t.Fatalf("ValidateTrendSegment(today) error = %v", err)
	}
	for _, label := range []string{"2026-7-21 00:00", "2026-07-21T00:00", "2026-07-21 0:00", "2026-07-21 00:00 "} {
		invalid := clonePrewarmTestSegment(today)
		invalid.Points[0].Date = label
		if err := ValidateTrendSegment(invalid); err == nil {
			t.Fatalf("ValidateTrendSegment(hour label %q) error = nil, want strict shape rejection", label)
		}
	}
}

func TestPrewarmComposeUsesIndependentHistoryAndCoalescedToday(t *testing.T) {
	ten, twenty, forty := int64(10), int64(20), int64(40)
	current := prewarmTestCurrentStats([]PrewarmCurrentStat{
		{UserID: 1, TodayActualCost: 3, TotalActualCost: 30, TotalTokens: int64Pointer(300)},
		{UserID: 2, TodayActualCost: 1, TotalActualCost: 20},
		{UserID: 3, TodayActualCost: 0, TotalActualCost: 0, TotalTokens: int64Pointer(0)},
	})
	today := validPrewarmTestSegment(SegmentTodayHour, []relay.ProviderWideTrendPoint{
		{UserID: 1, Date: "2026-07-21 00:00", ActualCost: 1, TotalTokens: &ten},
		{UserID: 1, Date: "2026-07-21 01:00", ActualCost: 2, TotalTokens: &twenty},
		{UserID: 2, Date: "2026-07-21 00:00", ActualCost: 0.5},
		{UserID: 2, Date: "2026-07-21 01:00", ActualCost: 0.5, TotalTokens: &ten},
		{UserID: 99, Date: "2026-07-21 00:00", ActualCost: 9, TotalTokens: &ten},
	})
	history6 := validPrewarmTestSegment(SegmentHistory6d, []relay.ProviderWideTrendPoint{
		{UserID: 1, Date: "2026-07-15", ActualCost: 4, TotalTokens: &forty},
		{UserID: 1, Date: "2026-07-21", ActualCost: 5, TotalTokens: &forty},
		{UserID: 2, Date: "2026-07-20", ActualCost: 6, TotalTokens: &forty},
	})
	history29 := validPrewarmTestSegment(SegmentHistory29d, []relay.ProviderWideTrendPoint{
		{UserID: 1, Date: "2026-06-22", ActualCost: 29, TotalTokens: &forty},
	})
	segments := PrewarmSegmentSet{History29d: &history29, History6d: &history6, TodayHour: &today}

	sevenWindow := mustRecognizePrewarmTestWindow(t, OverviewParams{
		StartDate: "2026-07-15", EndDate: "2026-07-21", Granularity: "day", Timezone: "UTC",
	})
	seven, eligible, err := ComposePrewarmedOrigin(sevenWindow, current, segments, []int64{3, 2, 1, 1})
	if err != nil || !eligible {
		t.Fatalf("ComposePrewarmedOrigin(7d) = %#v, %v, %v", seven, eligible, err)
	}
	if !reflect.DeepEqual(seven.RelayUserIDs, []int64{1, 2, 3}) {
		t.Fatalf("7d RelayUserIDs = %#v, want sorted unique authorized IDs", seven.RelayUserIDs)
	}
	wantUser1 := []relay.UsageTrendPoint{
		{Date: "2026-07-15", ActualCost: 4, TotalTokens: int64Pointer(40)},
		{Date: "2026-07-21", ActualCost: 8, TotalTokens: int64Pointer(70)},
	}
	if !reflect.DeepEqual(seven.PointsByUser[1], wantUser1) {
		t.Fatalf("7d user 1 points = %#v, want independently supplied history6 + coalesced/merged today %#v", seven.PointsByUser[1], wantUser1)
	}
	if points := seven.PointsByUser[2]; len(points) != 2 || points[1].TotalTokens != nil {
		t.Fatalf("7d user 2 points = %#v, want explicit nil today tokens preserved", points)
	}
	if points, ok := seven.PointsByUser[3]; !ok || len(points) != 0 {
		t.Fatalf("7d sparse user points = %#v, %v, want dense empty row", points, ok)
	}
	if stat := seven.StatsByRelayUserID[3]; stat.RangeActualCost == nil || *stat.RangeActualCost != 0 || stat.RangeTotalTokens == nil || *stat.RangeTotalTokens != 0 {
		t.Fatalf("7d sparse user range totals = %#v/%#v, want complete zero contribution", stat.RangeActualCost, stat.RangeTotalTokens)
	}
	if _, leaked := seven.PointsByUser[99]; leaked {
		t.Fatal("7d origin leaked unauthorized provider-wide user")
	}

	thirtyWindow := mustRecognizePrewarmTestWindow(t, OverviewParams{
		StartDate: "2026-06-22", EndDate: "2026-07-21", Granularity: "day", Timezone: "UTC",
	})
	thirty, eligible, err := ComposePrewarmedOrigin(thirtyWindow, current, segments, []int64{1})
	if err != nil || !eligible {
		t.Fatalf("ComposePrewarmedOrigin(30d) = %#v, %v, %v", thirty, eligible, err)
	}
	if got := thirty.PointsByUser[1]; len(got) != 2 || got[0].ActualCost != 29 || got[0].Date != "2026-06-22" {
		t.Fatalf("30d points = %#v, want independently supplied history29", got)
	}

	todayWindow := mustRecognizePrewarmTestWindow(t, OverviewParams{
		StartDate: "2026-07-21", EndDate: "2026-07-21", Granularity: "hour", Timezone: "UTC",
	})
	todayOrigin, eligible, err := ComposePrewarmedOrigin(todayWindow, current, segments, []int64{1})
	if err != nil || !eligible {
		t.Fatalf("ComposePrewarmedOrigin(today) = %#v, %v, %v", todayOrigin, eligible, err)
	}
	if got := todayOrigin.PointsByUser[1]; len(got) != 2 || got[0].Date != "2026-07-21 00:00" || got[1].Date != "2026-07-21 01:00" {
		t.Fatalf("today points = %#v, want raw source hours retained", got)
	}
}

func TestPrewarmComposeRejectsMissingRosterAndUnionTruncationBeforeAuthorization(t *testing.T) {
	today := validPrewarmTestSegment(SegmentTodayHour, []relay.ProviderWideTrendPoint{{UserID: 1, Date: "2026-07-21 00:00", ActualCost: 1}})
	history := validPrewarmTestSegment(SegmentHistory6d, []relay.ProviderWideTrendPoint{{UserID: 1, Date: "2026-07-15", ActualCost: 1}})
	window := mustRecognizePrewarmTestWindow(t, OverviewParams{
		StartDate: "2026-07-15", EndDate: "2026-07-21", Granularity: "day", Timezone: "UTC",
	})
	current := prewarmTestCurrentStats([]PrewarmCurrentStat{{UserID: 1}})
	if origin, eligible, err := ComposePrewarmedOrigin(window, current, PrewarmSegmentSet{History6d: &history, TodayHour: &today}, []int64{1, 2}); err != nil || eligible || origin != nil {
		t.Fatalf("missing roster ComposePrewarmedOrigin() = %#v, %v, %v, want ineligible without synthetic stat", origin, eligible, err)
	}

	historyPoints := make([]relay.ProviderWideTrendPoint, PrewarmTrendUserLimit-1)
	for index := range historyPoints {
		historyPoints[index] = relay.ProviderWideTrendPoint{UserID: int64(index + 1), Date: "2026-07-15", ActualCost: 1}
	}
	history = validPrewarmTestSegment(SegmentHistory6d, historyPoints)
	today = validPrewarmTestSegment(SegmentTodayHour, []relay.ProviderWideTrendPoint{{UserID: PrewarmTrendUserLimit, Date: "2026-07-21 00:00", ActualCost: 1}})
	if _, _, err := ComposePrewarmedOrigin(window, current, PrewarmSegmentSet{History6d: &history, TodayHour: &today}, []int64{1}); err == nil {
		t.Fatalf("ComposePrewarmedOrigin() error = nil, want exact-%d union rejection before authorization", PrewarmTrendUserLimit)
	}
}

func TestPrewarmComposeValidatesCurrentStatsAndCostTolerance(t *testing.T) {
	window := mustRecognizePrewarmTestWindow(t, OverviewParams{
		StartDate: "2026-07-21", EndDate: "2026-07-21", Granularity: "hour", Timezone: "UTC",
	})
	today := validPrewarmTestSegment(SegmentTodayHour, nil)
	duplicate := prewarmTestCurrentStats([]PrewarmCurrentStat{{UserID: 1}, {UserID: 1}})
	nonpositive := prewarmTestCurrentStats([]PrewarmCurrentStat{{UserID: 0}})
	rosterCount := prewarmTestCurrentStats([]PrewarmCurrentStat{{UserID: 1}})
	rosterCount.RosterCount = 2
	negativeToday := prewarmTestCurrentStats([]PrewarmCurrentStat{{UserID: 1, TodayActualCost: -1}})
	nonfiniteTotal := prewarmTestCurrentStats([]PrewarmCurrentStat{{UserID: 1, TotalActualCost: math.Inf(1)}})
	negativeTokens := prewarmTestCurrentStats([]PrewarmCurrentStat{{UserID: 1, TotalTokens: int64Pointer(-1)}})
	tamperedDigest := prewarmTestCurrentStats([]PrewarmCurrentStat{{UserID: 1}})
	tamperedDigest.RosterDigest = strings.Repeat("f", 64)
	for _, test := range []struct {
		name    string
		current PrewarmCurrentStatsEnvelope
	}{
		{name: "duplicate ID", current: duplicate},
		{name: "nonpositive ID", current: nonpositive},
		{name: "roster count", current: rosterCount},
		{name: "negative today cost", current: negativeToday},
		{name: "nonfinite total cost", current: nonfiniteTotal},
		{name: "negative optional tokens", current: negativeTokens},
		{name: "tampered roster digest", current: tamperedDigest},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := ComposePrewarmedOrigin(window, test.current, PrewarmSegmentSet{TodayHour: &today}, []int64{1}); err == nil {
				t.Fatalf("ComposePrewarmedOrigin() error = nil, want %s rejection", test.name)
			}
		})
	}

	if !PrewarmCostsEquivalent(0, 1e-9) {
		t.Fatal("PrewarmCostsEquivalent() rejected absolute 1e-9 boundary")
	}
	if PrewarmCostsEquivalent(0, 1.0001e-9) {
		t.Fatal("PrewarmCostsEquivalent() accepted value above absolute 1e-9 boundary")
	}
	if PrewarmCostsEquivalent(math.NaN(), math.NaN()) || PrewarmCostsEquivalent(math.Inf(1), math.Inf(1)) {
		t.Fatal("PrewarmCostsEquivalent() accepted nonfinite values")
	}
}

func TestPrewarmComposeRejectsUsageOverflow(t *testing.T) {
	maxTokens := int64(^uint64(0) >> 1)
	oneToken := int64(1)
	current := prewarmTestCurrentStats([]PrewarmCurrentStat{{UserID: 1}})
	todayWindow := mustRecognizePrewarmTestWindow(t, OverviewParams{
		StartDate: "2026-07-21", EndDate: "2026-07-21", Granularity: "hour", Timezone: "UTC",
	})
	sevenWindow := mustRecognizePrewarmTestWindow(t, OverviewParams{
		StartDate: "2026-07-15", EndDate: "2026-07-21", Granularity: "day", Timezone: "UTC",
	})

	tests := []struct {
		name     string
		window   PrewarmWindow
		segments PrewarmSegmentSet
	}{
		{
			name:   "cost within coalesced today",
			window: sevenWindow,
			segments: prewarmTestDailySegments(nil, []relay.ProviderWideTrendPoint{
				{UserID: 1, Date: "2026-07-21 00:00", ActualCost: math.MaxFloat64},
				{UserID: 1, Date: "2026-07-21 01:00", ActualCost: math.MaxFloat64},
			}),
		},
		{
			name:   "tokens within coalesced today",
			window: sevenWindow,
			segments: prewarmTestDailySegments(nil, []relay.ProviderWideTrendPoint{
				{UserID: 1, Date: "2026-07-21 00:00", ActualCost: 1, TotalTokens: &maxTokens},
				{UserID: 1, Date: "2026-07-21 01:00", ActualCost: 1, TotalTokens: &oneToken},
			}),
		},
		{
			name:   "cost across history and today",
			window: sevenWindow,
			segments: prewarmTestDailySegments(
				[]relay.ProviderWideTrendPoint{{UserID: 1, Date: "2026-07-21", ActualCost: math.MaxFloat64}},
				[]relay.ProviderWideTrendPoint{{UserID: 1, Date: "2026-07-21 00:00", ActualCost: math.MaxFloat64}},
			),
		},
		{
			name:   "tokens across history and today",
			window: sevenWindow,
			segments: prewarmTestDailySegments(
				[]relay.ProviderWideTrendPoint{{UserID: 1, Date: "2026-07-21", ActualCost: 1, TotalTokens: &maxTokens}},
				[]relay.ProviderWideTrendPoint{{UserID: 1, Date: "2026-07-21 00:00", ActualCost: 1, TotalTokens: &oneToken}},
			),
		},
		{
			name:   "cost while summarizing raw range",
			window: todayWindow,
			segments: prewarmTestTodaySegments([]relay.ProviderWideTrendPoint{
				{UserID: 1, Date: "2026-07-21 00:00", ActualCost: math.MaxFloat64},
				{UserID: 1, Date: "2026-07-21 01:00", ActualCost: math.MaxFloat64},
			}),
		},
		{
			name:   "tokens while summarizing raw range",
			window: todayWindow,
			segments: prewarmTestTodaySegments([]relay.ProviderWideTrendPoint{
				{UserID: 1, Date: "2026-07-21 00:00", ActualCost: 1, TotalTokens: &maxTokens},
				{UserID: 1, Date: "2026-07-21 01:00", ActualCost: 1, TotalTokens: &oneToken},
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			origin, eligible, err := ComposePrewarmedOrigin(test.window, current, test.segments, []int64{1})
			if err == nil || !strings.Contains(err.Error(), "overflow") {
				t.Fatalf("ComposePrewarmedOrigin() error = %v, want contextual overflow rejection", err)
			}
			if origin != nil || eligible {
				t.Fatalf("ComposePrewarmedOrigin() = %#v, %v, want no eligible overflow facts", origin, eligible)
			}
		})
	}
}

func prewarmTestDailySegments(history, today []relay.ProviderWideTrendPoint) PrewarmSegmentSet {
	historySegment := validPrewarmTestSegment(SegmentHistory6d, history)
	todaySegment := validPrewarmTestSegment(SegmentTodayHour, today)
	return PrewarmSegmentSet{History6d: &historySegment, TodayHour: &todaySegment}
}

func prewarmTestTodaySegments(today []relay.ProviderWideTrendPoint) PrewarmSegmentSet {
	todaySegment := validPrewarmTestSegment(SegmentTodayHour, today)
	return PrewarmSegmentSet{TodayHour: &todaySegment}
}

func validPrewarmTestSegment(class PrewarmSegmentClass, points []relay.ProviderWideTrendPoint) PrewarmTrendSegment {
	coverage := PrewarmCoverage{Timezone: "UTC"}
	switch class {
	case SegmentHistory29d:
		coverage.StartDate, coverage.EndDate, coverage.Granularity = "2026-06-22", "2026-07-20", "day"
	case SegmentHistory6d:
		coverage.StartDate, coverage.EndDate, coverage.Granularity = "2026-07-15", "2026-07-20", "day"
	case SegmentTodayHour:
		coverage.StartDate, coverage.EndDate, coverage.Granularity = "2026-07-21", "2026-07-21", "hour"
	}
	users := make(map[int64]struct{}, len(points))
	for _, point := range points {
		users[point.UserID] = struct{}{}
	}
	return PrewarmTrendSegment{
		Class: class, Timezone: "UTC", AnchorDate: "2026-07-21", Coverage: coverage,
		Points: points, ResponseBytes: 1, PointCount: len(points), UniqueUserCount: len(users), Complete: true,
	}
}

func clonePrewarmTestSegment(segment PrewarmTrendSegment) PrewarmTrendSegment {
	segment.Points = append([]relay.ProviderWideTrendPoint(nil), segment.Points...)
	return segment
}

func mustRecognizePrewarmTestWindow(t *testing.T, params OverviewParams) PrewarmWindow {
	t.Helper()
	window, recognized, err := RecognizePrewarmWindow(params, time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC))
	if err != nil || !recognized {
		t.Fatalf("RecognizePrewarmWindow(%#v) = %#v, %v, %v", params, window, recognized, err)
	}
	return window
}

func int64Pointer(value int64) *int64 {
	return &value
}

func prewarmTestCurrentStats(stats []PrewarmCurrentStat) PrewarmCurrentStatsEnvelope {
	roster := make([]int64, len(stats))
	for index, stat := range stats {
		roster[index] = stat.UserID
	}
	return PrewarmCurrentStatsEnvelope{
		RosterCount: len(stats), RosterDigest: prewarmRosterDigest(roster), Stats: stats,
	}
}
