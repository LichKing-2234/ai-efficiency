package teamusage

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
)

const (
	PrewarmTrendResponseByteLimit int64 = 32 << 20
	PrewarmTrendPointLimit              = 1_000_000
	PrewarmTrendUserLimit               = 5000
	PrewarmCostTolerance                = 1e-9
	prewarmTimezoneNameMaxBytes         = 255
	prewarmTimezoneLimit                = 4
)

var defaultPrewarmTimezoneNames = [...]string{
	"UTC",
	"Asia/Shanghai",
	"America/Los_Angeles",
	"Europe/Berlin",
}

type PrewarmWindowClass string

const (
	PrewarmWindowToday PrewarmWindowClass = "today"
	PrewarmWindow7d    PrewarmWindowClass = "7d"
	PrewarmWindow30d   PrewarmWindowClass = "30d"
)

type PrewarmSegmentClass string

const (
	SegmentHistory29d PrewarmSegmentClass = "history_29d"
	SegmentHistory6d  PrewarmSegmentClass = "history_6d"
	SegmentTodayHour  PrewarmSegmentClass = "today_hour"
)

type PrewarmCoverage struct {
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Granularity string `json:"granularity"`
	Timezone    string `json:"timezone"`
}

type PrewarmWindow struct {
	Class      PrewarmWindowClass `json:"class"`
	AnchorDate string             `json:"anchor_date"`
	Coverage   PrewarmCoverage    `json:"coverage"`
}

type PrewarmCurrentStat struct {
	UserID          int64   `json:"user_id"`
	TodayActualCost float64 `json:"today_actual_cost"`
	TotalActualCost float64 `json:"total_actual_cost"`
	TotalTokens     *int64  `json:"total_tokens,omitempty"`
}

// PrewarmCurrentStatsEnvelope holds only usage facts for the complete Relay
// roster. Identity, authorization, and selected-window totals are deliberately
// absent.
type PrewarmCurrentStatsEnvelope struct {
	SchemaVersion   int                  `json:"schema_version"`
	ProviderID      int                  `json:"provider_id"`
	ProviderVersion int64                `json:"provider_version"`
	GenerationID    string               `json:"generation_id"`
	GeneratedAt     time.Time            `json:"generated_at"`
	RosterCount     int                  `json:"roster_count"`
	RosterDigest    string               `json:"roster_digest"`
	ResponseBytes   int64                `json:"response_bytes"`
	Stats           []PrewarmCurrentStat `json:"stats"`
}

// PrewarmTrendSegment is a validated provider-wide source segment. Points
// retain Relay source labels exactly as returned.
type PrewarmTrendSegment struct {
	SchemaVersion   int                            `json:"schema_version"`
	ProviderID      int                            `json:"provider_id"`
	ProviderVersion int64                          `json:"provider_version"`
	TimezoneDigest  string                         `json:"timezone_digest"`
	GenerationID    string                         `json:"generation_id"`
	GeneratedAt     time.Time                      `json:"generated_at"`
	Timezone        string                         `json:"timezone"`
	AnchorDate      string                         `json:"anchor_date"`
	Class           PrewarmSegmentClass            `json:"class"`
	Coverage        PrewarmCoverage                `json:"coverage"`
	Points          []relay.ProviderWideTrendPoint `json:"points"`
	ResponseBytes   int64                          `json:"response_bytes"`
	PointCount      int                            `json:"point_count"`
	UniqueUserCount int                            `json:"unique_user_count"`
	Complete        bool                           `json:"complete"`
}

type PrewarmSegmentSet struct {
	History29d *PrewarmTrendSegment
	History6d  *PrewarmTrendSegment
	TodayHour  *PrewarmTrendSegment
}

func DefaultPrewarmTimezones() []string {
	zones := make([]string, len(defaultPrewarmTimezoneNames))
	copy(zones, defaultPrewarmTimezoneNames[:])
	return zones
}

func NormalizePrewarmTimezones(configured []string) ([]string, error) {
	normalized := make([]string, 0, len(configured))
	seen := make(map[string]struct{}, len(configured))
	for _, raw := range configured {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if len(name) > prewarmTimezoneNameMaxBytes {
			return nil, fmt.Errorf("prewarm timezone name exceeds 255 bytes")
		}
		if _, ok := seen[name]; ok {
			continue
		}
		if _, err := loadPrewarmLocation(name); err != nil {
			return nil, fmt.Errorf("invalid prewarm timezone %q: %w", name, err)
		}
		seen[name] = struct{}{}
		normalized = append(normalized, name)
		if len(normalized) > prewarmTimezoneLimit {
			return nil, fmt.Errorf("prewarm timezone allowlist exceeds maximum four entries")
		}
	}
	return normalized, nil
}

func RecognizePrewarmWindow(params OverviewParams, now time.Time) (PrewarmWindow, bool, error) {
	timezone := strings.TrimSpace(params.Timezone)
	location, err := loadPrewarmLocation(timezone)
	if err != nil {
		return PrewarmWindow{}, false, fmt.Errorf("load prewarm window timezone %q: %w", timezone, err)
	}
	localNow := now.In(location)
	anchor := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	anchorDate := anchor.Format(time.DateOnly)
	coverage := PrewarmCoverage{
		StartDate: strings.TrimSpace(params.StartDate), EndDate: strings.TrimSpace(params.EndDate),
		Granularity: strings.TrimSpace(params.Granularity), Timezone: timezone,
	}
	candidates := []PrewarmWindow{
		{Class: PrewarmWindowToday, AnchorDate: anchorDate, Coverage: PrewarmCoverage{StartDate: anchorDate, EndDate: anchorDate, Granularity: "hour", Timezone: timezone}},
		{Class: PrewarmWindow7d, AnchorDate: anchorDate, Coverage: PrewarmCoverage{StartDate: anchor.AddDate(0, 0, -6).Format(time.DateOnly), EndDate: anchorDate, Granularity: "day", Timezone: timezone}},
		{Class: PrewarmWindow30d, AnchorDate: anchorDate, Coverage: PrewarmCoverage{StartDate: anchor.AddDate(0, 0, -29).Format(time.DateOnly), EndDate: anchorDate, Granularity: "day", Timezone: timezone}},
	}
	for _, candidate := range candidates {
		if candidate.Coverage == coverage {
			return candidate, true, nil
		}
	}
	return PrewarmWindow{}, false, nil
}

func SplitSafe(timezone, anchorDate string) (bool, error) {
	name := strings.TrimSpace(timezone)
	location, err := loadPrewarmLocation(name)
	if err != nil {
		return false, fmt.Errorf("load prewarm split timezone %q: %w", name, err)
	}
	if !validPrewarmDayLabel(anchorDate) {
		return false, fmt.Errorf("invalid prewarm anchor date %q", anchorDate)
	}
	current, err := time.ParseInLocation(time.DateOnly, anchorDate, location)
	if err != nil {
		return false, fmt.Errorf("parse prewarm anchor date %q: %w", anchorDate, err)
	}
	if current.Format(time.DateOnly) != anchorDate {
		return false, fmt.Errorf("prewarm anchor date %q is not normalized", anchorDate)
	}
	previous := current.AddDate(0, 0, -1)
	return previous.Add(24 * time.Hour).Equal(current), nil
}

func ValidateTrendSegment(segment PrewarmTrendSegment) error {
	if segment.PointCount < 0 || segment.PointCount >= PrewarmTrendPointLimit || len(segment.Points) >= PrewarmTrendPointLimit {
		return fmt.Errorf("prewarm trend point count reached limit %d", PrewarmTrendPointLimit)
	}
	if segment.UniqueUserCount < 0 || segment.UniqueUserCount >= PrewarmTrendUserLimit {
		return fmt.Errorf("prewarm trend unique user count reached limit %d", PrewarmTrendUserLimit)
	}
	if segment.ResponseBytes < 0 || segment.ResponseBytes >= PrewarmTrendResponseByteLimit {
		return fmt.Errorf("prewarm trend response bytes reached limit %d", PrewarmTrendResponseByteLimit)
	}
	if !segment.Complete {
		return fmt.Errorf("prewarm trend segment is incomplete")
	}
	expected, err := prewarmSegmentCoverage(segment.Class, segment.AnchorDate, segment.Timezone)
	if err != nil {
		return err
	}
	if segment.Coverage != expected {
		return fmt.Errorf("prewarm trend coverage does not match segment contract")
	}
	if segment.PointCount != len(segment.Points) {
		return fmt.Errorf("prewarm trend point count metadata does not match decoded points")
	}

	uniqueUsers := make(map[int64]struct{}, segment.UniqueUserCount)
	seen := make(map[prewarmSourcePointKey]struct{}, len(segment.Points))
	lastLabelByUser := make(map[int64]string, segment.UniqueUserCount)
	for index, point := range segment.Points {
		if point.UserID <= 0 {
			return fmt.Errorf("prewarm trend point %d has invalid user ID", index)
		}
		if !validPrewarmSourceLabel(point.Date, segment.Coverage.Granularity) {
			return fmt.Errorf("prewarm trend point %d has invalid source label", index)
		}
		if point.TotalTokens != nil && *point.TotalTokens < 0 {
			return fmt.Errorf("prewarm trend point %d has negative tokens", index)
		}
		if !validPrewarmCost(point.ActualCost) {
			return fmt.Errorf("prewarm trend point %d has invalid actual cost", index)
		}
		if previous, ok := lastLabelByUser[point.UserID]; ok && point.Date <= previous {
			return fmt.Errorf("prewarm trend point %d is not strictly source ordered", index)
		}
		lastLabelByUser[point.UserID] = point.Date
		key := prewarmSourcePointKey{UserID: point.UserID, Label: point.Date}
		if _, ok := seen[key]; ok {
			return fmt.Errorf("prewarm trend contains duplicate user/source-label point")
		}
		seen[key] = struct{}{}
		uniqueUsers[point.UserID] = struct{}{}
		if len(uniqueUsers) >= PrewarmTrendUserLimit {
			return fmt.Errorf("prewarm trend unique user count reached limit %d", PrewarmTrendUserLimit)
		}
	}
	if segment.UniqueUserCount != len(uniqueUsers) {
		return fmt.Errorf("prewarm trend unique user metadata does not match decoded points")
	}
	return nil
}

func ComposePrewarmedOrigin(
	window PrewarmWindow,
	current PrewarmCurrentStatsEnvelope,
	segments PrewarmSegmentSet,
	authorizedRelayUserIDs []int64,
) (*teamUsageScopeOrigin, bool, error) {
	origin, eligible, _, err := composePrewarmedOriginWithUnion(window, current, segments, authorizedRelayUserIDs)
	return origin, eligible, err
}

func composePrewarmedOriginWithUnion(
	window PrewarmWindow,
	current PrewarmCurrentStatsEnvelope,
	segments PrewarmSegmentSet,
	authorizedRelayUserIDs []int64,
) (*teamUsageScopeOrigin, bool, int, error) {
	if err := validatePrewarmWindow(window); err != nil {
		return nil, false, 0, err
	}
	required, err := requiredPrewarmSegments(window, segments)
	if err != nil {
		return nil, false, 0, err
	}
	for _, segment := range required {
		if err := ValidateTrendSegment(*segment); err != nil {
			return nil, false, 0, err
		}
		if segment.AnchorDate != window.AnchorDate || segment.Timezone != window.Coverage.Timezone {
			return nil, false, 0, fmt.Errorf("prewarm trend segment does not match window anchor or timezone")
		}
	}
	unionUsers, err := validatePrewarmComposedUnion(required)
	if err != nil {
		return nil, false, 0, err
	}
	stats, err := validatePrewarmCurrentStats(current)
	if err != nil {
		return nil, false, 0, err
	}
	authorized, err := normalizeAuthorizedRelayUserIDs(authorizedRelayUserIDs)
	if err != nil {
		return nil, false, 0, err
	}
	for _, userID := range authorized {
		if _, ok := stats[userID]; !ok {
			return nil, false, unionUsers, nil
		}
	}

	origin := &teamUsageScopeOrigin{
		RelayUserIDs: authorized, StatsByRelayUserID: make(map[int64]relay.TeamUserUsageStats, len(authorized)),
		PointsByUser: make(map[int64][]relay.UsageTrendPoint, len(authorized)),
	}
	authorizedSet := make(map[int64]struct{}, len(authorized))
	for _, userID := range authorized {
		authorizedSet[userID] = struct{}{}
		origin.PointsByUser[userID] = []relay.UsageTrendPoint{}
	}

	switch window.Class {
	case PrewarmWindowToday:
		projectRawToday(origin.PointsByUser, authorizedSet, segments.TodayHour.Points)
	case PrewarmWindow7d:
		if err := composeDailyPoints(origin.PointsByUser, authorizedSet, segments.History6d.Points, segments.TodayHour.Points); err != nil {
			return nil, false, unionUsers, fmt.Errorf("compose prewarm 7d points: %w", err)
		}
	case PrewarmWindow30d:
		if err := composeDailyPoints(origin.PointsByUser, authorizedSet, segments.History29d.Points, segments.TodayHour.Points); err != nil {
			return nil, false, unionUsers, fmt.Errorf("compose prewarm 30d points: %w", err)
		}
	default:
		return nil, false, unionUsers, fmt.Errorf("unsupported prewarm window class %q", window.Class)
	}

	for _, userID := range authorized {
		currentStat := stats[userID]
		cost, tokens, tokensComplete, err := summarizePrewarmRange(origin.PointsByUser[userID])
		if err != nil {
			return nil, false, unionUsers, fmt.Errorf("summarize prewarm composed range: %w", err)
		}
		stat := relay.TeamUserUsageStats{
			UserID: userID, TodayActualCost: currentStat.TodayActualCost, TotalActualCost: currentStat.TotalActualCost,
			TotalTokens: clonePrewarmInt64Pointer(currentStat.TotalTokens), RangeActualCost: &cost,
		}
		if tokensComplete {
			stat.RangeTotalTokens = &tokens
		}
		origin.StatsByRelayUserID[userID] = stat
	}
	return origin, true, unionUsers, nil
}

func PrewarmCostsEquivalent(left, right float64) bool {
	return validPrewarmCost(left) && validPrewarmCost(right) && math.Abs(left-right) <= PrewarmCostTolerance
}

type prewarmSourcePointKey struct {
	UserID int64
	Label  string
}

type prewarmPointAccumulator struct {
	actualCost      float64
	totalTokens     int64
	tokensComplete  bool
	hasContribution bool
}

func prewarmSegmentCoverage(class PrewarmSegmentClass, anchorDate, timezone string) (PrewarmCoverage, error) {
	if strings.TrimSpace(timezone) != timezone || timezone == "" {
		return PrewarmCoverage{}, fmt.Errorf("invalid prewarm segment timezone")
	}
	location, err := loadPrewarmLocation(timezone)
	if err != nil {
		return PrewarmCoverage{}, fmt.Errorf("load prewarm segment timezone %q: %w", timezone, err)
	}
	if !validPrewarmDayLabel(anchorDate) {
		return PrewarmCoverage{}, fmt.Errorf("invalid prewarm segment anchor %q", anchorDate)
	}
	anchor, err := time.ParseInLocation(time.DateOnly, anchorDate, location)
	if err != nil {
		return PrewarmCoverage{}, fmt.Errorf("parse prewarm segment anchor %q: %w", anchorDate, err)
	}
	if anchor.Format(time.DateOnly) != anchorDate {
		return PrewarmCoverage{}, fmt.Errorf("prewarm segment anchor %q is not normalized", anchorDate)
	}
	switch class {
	case SegmentHistory29d:
		return PrewarmCoverage{StartDate: anchor.AddDate(0, 0, -29).Format(time.DateOnly), EndDate: anchor.AddDate(0, 0, -1).Format(time.DateOnly), Granularity: "day", Timezone: timezone}, nil
	case SegmentHistory6d:
		return PrewarmCoverage{StartDate: anchor.AddDate(0, 0, -6).Format(time.DateOnly), EndDate: anchor.AddDate(0, 0, -1).Format(time.DateOnly), Granularity: "day", Timezone: timezone}, nil
	case SegmentTodayHour:
		return PrewarmCoverage{StartDate: anchorDate, EndDate: anchorDate, Granularity: "hour", Timezone: timezone}, nil
	default:
		return PrewarmCoverage{}, fmt.Errorf("invalid prewarm segment class %q", class)
	}
}

func validatePrewarmWindow(window PrewarmWindow) error {
	var startOffset int
	var granularity string
	switch window.Class {
	case PrewarmWindowToday:
		startOffset, granularity = 0, "hour"
	case PrewarmWindow7d:
		startOffset, granularity = -6, "day"
	case PrewarmWindow30d:
		startOffset, granularity = -29, "day"
	default:
		return fmt.Errorf("invalid prewarm window class %q", window.Class)
	}
	location, err := loadPrewarmLocation(window.Coverage.Timezone)
	if err != nil {
		return fmt.Errorf("load prewarm window timezone %q: %w", window.Coverage.Timezone, err)
	}
	if !validPrewarmDayLabel(window.AnchorDate) {
		return fmt.Errorf("invalid prewarm window anchor %q", window.AnchorDate)
	}
	anchor, err := time.ParseInLocation(time.DateOnly, window.AnchorDate, location)
	if err != nil {
		return fmt.Errorf("parse prewarm window anchor %q: %w", window.AnchorDate, err)
	}
	if anchor.Format(time.DateOnly) != window.AnchorDate {
		return fmt.Errorf("prewarm window anchor %q is not normalized", window.AnchorDate)
	}
	want := PrewarmCoverage{
		StartDate: anchor.AddDate(0, 0, startOffset).Format(time.DateOnly), EndDate: window.AnchorDate,
		Granularity: granularity, Timezone: window.Coverage.Timezone,
	}
	if window.Coverage != want {
		return fmt.Errorf("prewarm window coverage does not match class")
	}
	return nil
}

func requiredPrewarmSegments(window PrewarmWindow, segments PrewarmSegmentSet) ([]*PrewarmTrendSegment, error) {
	if segments.TodayHour == nil {
		return nil, fmt.Errorf("prewarm today segment is required")
	}
	switch window.Class {
	case PrewarmWindowToday:
		return []*PrewarmTrendSegment{segments.TodayHour}, nil
	case PrewarmWindow7d:
		if segments.History6d == nil {
			return nil, fmt.Errorf("prewarm history_6d segment is required")
		}
		return []*PrewarmTrendSegment{segments.History6d, segments.TodayHour}, nil
	case PrewarmWindow30d:
		if segments.History29d == nil {
			return nil, fmt.Errorf("prewarm history_29d segment is required")
		}
		return []*PrewarmTrendSegment{segments.History29d, segments.TodayHour}, nil
	default:
		return nil, fmt.Errorf("invalid prewarm window class %q", window.Class)
	}
}

func validatePrewarmComposedUnion(segments []*PrewarmTrendSegment) (int, error) {
	users := make(map[int64]struct{})
	for _, segment := range segments {
		for _, point := range segment.Points {
			users[point.UserID] = struct{}{}
			if len(users) >= PrewarmTrendUserLimit {
				return 0, fmt.Errorf("prewarm composed unique user union reached limit %d", PrewarmTrendUserLimit)
			}
		}
	}
	return len(users), nil
}

func validatePrewarmCurrentStats(current PrewarmCurrentStatsEnvelope) (map[int64]PrewarmCurrentStat, error) {
	if current.RosterCount != len(current.Stats) {
		return nil, fmt.Errorf("prewarm current stats roster count does not match stats")
	}
	if current.RosterCount < 0 || current.RosterCount >= PrewarmTrendUserLimit {
		return nil, fmt.Errorf("prewarm current stats roster reached limit %d", PrewarmTrendUserLimit)
	}
	stats := make(map[int64]PrewarmCurrentStat, len(current.Stats))
	roster := make([]int64, 0, len(current.Stats))
	var previous int64
	for index, stat := range current.Stats {
		if stat.UserID <= 0 {
			return nil, fmt.Errorf("prewarm current stat %d has invalid user ID", index)
		}
		if index > 0 && stat.UserID <= previous {
			return nil, fmt.Errorf("prewarm current stats are not strictly ordered")
		}
		previous = stat.UserID
		if !validPrewarmCost(stat.TodayActualCost) || !validPrewarmCost(stat.TotalActualCost) {
			return nil, fmt.Errorf("prewarm current stat %d has invalid actual cost", index)
		}
		if stat.TotalTokens != nil && *stat.TotalTokens < 0 {
			return nil, fmt.Errorf("prewarm current stat %d has negative tokens", index)
		}
		if _, exists := stats[stat.UserID]; exists {
			return nil, fmt.Errorf("prewarm current stats contain duplicate user ID")
		}
		stats[stat.UserID] = stat
		roster = append(roster, stat.UserID)
	}
	if prewarmRosterDigest(roster) != current.RosterDigest {
		return nil, fmt.Errorf("prewarm current stats roster digest does not match stats")
	}
	return stats, nil
}

func normalizeAuthorizedRelayUserIDs(userIDs []int64) ([]int64, error) {
	set := make(map[int64]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID <= 0 {
			return nil, fmt.Errorf("authorized Relay user ID must be positive")
		}
		set[userID] = struct{}{}
	}
	normalized := make([]int64, 0, len(set))
	for userID := range set {
		normalized = append(normalized, userID)
	}
	sort.Slice(normalized, func(left, right int) bool { return normalized[left] < normalized[right] })
	return normalized, nil
}

func projectRawToday(target map[int64][]relay.UsageTrendPoint, authorized map[int64]struct{}, points []relay.ProviderWideTrendPoint) {
	for _, point := range points {
		if _, ok := authorized[point.UserID]; !ok {
			continue
		}
		target[point.UserID] = append(target[point.UserID], relay.UsageTrendPoint{
			Date: point.Date, ActualCost: point.ActualCost, TotalTokens: clonePrewarmInt64Pointer(point.TotalTokens),
		})
	}
}

func composeDailyPoints(
	target map[int64][]relay.UsageTrendPoint,
	authorized map[int64]struct{},
	history []relay.ProviderWideTrendPoint,
	today []relay.ProviderWideTrendPoint,
) error {
	byUser := make(map[int64]map[string]*prewarmPointAccumulator, len(authorized))
	for _, point := range history {
		if _, ok := authorized[point.UserID]; !ok {
			continue
		}
		if err := addPrewarmContribution(prewarmAccumulator(byUser, point.UserID, point.Date), point.ActualCost, point.TotalTokens); err != nil {
			return fmt.Errorf("add history contribution: %w", err)
		}
	}
	todayByUser := make(map[int64]map[string]*prewarmPointAccumulator, len(authorized))
	for _, point := range today {
		if _, ok := authorized[point.UserID]; !ok {
			continue
		}
		label := point.Date[:len(time.DateOnly)]
		if err := addPrewarmContribution(prewarmAccumulator(todayByUser, point.UserID, label), point.ActualCost, point.TotalTokens); err != nil {
			return fmt.Errorf("coalesce today contribution: %w", err)
		}
	}
	for userID, byLabel := range todayByUser {
		for label, contribution := range byLabel {
			if err := addPrewarmAccumulator(prewarmAccumulator(byUser, userID, label), contribution); err != nil {
				return fmt.Errorf("merge history and today contribution: %w", err)
			}
		}
	}
	for userID := range authorized {
		byLabel := byUser[userID]
		labels := make([]string, 0, len(byLabel))
		for label := range byLabel {
			labels = append(labels, label)
		}
		sort.Strings(labels)
		points := make([]relay.UsageTrendPoint, 0, len(labels))
		for _, label := range labels {
			value := byLabel[label]
			point := relay.UsageTrendPoint{Date: label, ActualCost: value.actualCost}
			if value.tokensComplete {
				point.TotalTokens = clonePrewarmInt64Pointer(&value.totalTokens)
			}
			points = append(points, point)
		}
		target[userID] = points
	}
	return nil
}

func prewarmAccumulator(target map[int64]map[string]*prewarmPointAccumulator, userID int64, label string) *prewarmPointAccumulator {
	byLabel := target[userID]
	if byLabel == nil {
		byLabel = make(map[string]*prewarmPointAccumulator)
		target[userID] = byLabel
	}
	value := byLabel[label]
	if value == nil {
		value = &prewarmPointAccumulator{}
		byLabel[label] = value
	}
	return value
}

func addPrewarmContribution(target *prewarmPointAccumulator, actualCost float64, totalTokens *int64) error {
	newCost, err := addPrewarmCosts(target.actualCost, actualCost)
	if err != nil {
		return err
	}
	newTokens := target.totalTokens
	newTokensComplete := target.tokensComplete
	if !target.hasContribution {
		newTokensComplete = true
	}
	if totalTokens == nil {
		newTokensComplete = false
	} else {
		newTokens, err = addPrewarmTokens(newTokens, *totalTokens)
		if err != nil {
			return err
		}
	}
	target.actualCost = newCost
	target.totalTokens = newTokens
	target.tokensComplete = newTokensComplete
	target.hasContribution = true
	return nil
}

func addPrewarmAccumulator(target, contribution *prewarmPointAccumulator) error {
	newCost, err := addPrewarmCosts(target.actualCost, contribution.actualCost)
	if err != nil {
		return err
	}
	newTokens := target.totalTokens
	newTokensComplete := target.tokensComplete
	if !target.hasContribution {
		newTokensComplete = true
	}
	if !contribution.tokensComplete {
		newTokensComplete = false
	} else {
		newTokens, err = addPrewarmTokens(newTokens, contribution.totalTokens)
		if err != nil {
			return err
		}
	}
	target.actualCost = newCost
	target.totalTokens = newTokens
	target.tokensComplete = newTokensComplete
	target.hasContribution = true
	return nil
}

func summarizePrewarmRange(points []relay.UsageTrendPoint) (float64, int64, bool, error) {
	var actualCost float64
	var totalTokens int64
	tokensComplete := true
	for _, point := range points {
		var err error
		actualCost, err = addPrewarmCosts(actualCost, point.ActualCost)
		if err != nil {
			return 0, 0, false, fmt.Errorf("add range actual cost: %w", err)
		}
		if point.TotalTokens == nil {
			tokensComplete = false
			continue
		}
		totalTokens, err = addPrewarmTokens(totalTokens, *point.TotalTokens)
		if err != nil {
			return 0, 0, false, fmt.Errorf("add range tokens: %w", err)
		}
	}
	return actualCost, totalTokens, tokensComplete, nil
}

func addPrewarmCosts(left, right float64) (float64, error) {
	if !validPrewarmCost(left) || !validPrewarmCost(right) {
		return 0, fmt.Errorf("invalid actual cost addition")
	}
	total := left + right
	if !validPrewarmCost(total) {
		return 0, fmt.Errorf("actual cost overflow")
	}
	return total, nil
}

func addPrewarmTokens(left, right int64) (int64, error) {
	if left < 0 || right < 0 {
		return 0, fmt.Errorf("invalid token addition")
	}
	const maxInt64 = int64(^uint64(0) >> 1)
	if right > maxInt64-left {
		return 0, fmt.Errorf("token overflow")
	}
	return left + right, nil
}

func loadPrewarmLocation(name string) (*time.Location, error) {
	if name == "Local" {
		return nil, fmt.Errorf("timezone Local is environment-dependent")
	}
	if name == "UTC" {
		return time.UTC, nil
	}
	return time.LoadLocation(name)
}

func validPrewarmSourceLabel(label, granularity string) bool {
	if strings.TrimSpace(label) != label || label == "" {
		return false
	}
	switch granularity {
	case "day":
		return validPrewarmDayLabel(label)
	case "hour":
		return len(label) == len("2006-01-02 15:04") && validPrewarmDayLabel(label[:len(time.DateOnly)]) &&
			label[10] == ' ' && validPrewarmASCIIDigits(label, 11, 13) && label[13] == ':' && validPrewarmASCIIDigits(label, 14, 16)
	default:
		return false
	}
}

func validPrewarmDayLabel(label string) bool {
	return len(label) == len(time.DateOnly) && validPrewarmASCIIDigits(label, 0, 4) && label[4] == '-' &&
		validPrewarmASCIIDigits(label, 5, 7) && label[7] == '-' && validPrewarmASCIIDigits(label, 8, 10)
}

func validPrewarmASCIIDigits(value string, start, end int) bool {
	if start < 0 || end > len(value) || start >= end {
		return false
	}
	for index := start; index < end; index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func validPrewarmCost(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func clonePrewarmInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
