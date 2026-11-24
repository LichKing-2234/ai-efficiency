package personalusage

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
)

func TestOpenAICycleStartsOneWindowBeforeTheUpstreamReset(t *testing.T) {
	service, origin, userID, _ := createPersonalUsageFixture(t, true)
	resetAt := time.Date(2026, 7, 22, 9, 30, 0, 0, time.UTC)
	origin.pool = relay.UserUsageGroupPoolUsageState{
		Status: "ok",
		Groups: []relay.UserUsageGroupPoolUsageGroupItem{
			{GroupID: "42", Status: "ok", NextResetAt: &resetAt},
		},
	}

	response, err := service.OpenAICycle(context.Background(), userID)
	if err != nil {
		t.Fatalf("openai cycle: %v", err)
	}
	if response.Status != "ok" {
		t.Fatalf("status = %q, want ok (%s)", response.Status, response.Message)
	}
	if response.GroupID != "42" {
		t.Fatalf("group id = %q, want 42", response.GroupID)
	}
	if response.ResetAt == nil || !response.ResetAt.Equal(resetAt) {
		t.Fatalf("reset at = %v, want %v", response.ResetAt, resetAt)
	}
	wantStart := resetAt.Add(-7 * 24 * time.Hour)
	if response.StartAt == nil || !response.StartAt.Equal(wantStart) {
		t.Fatalf("start at = %v, want %v", response.StartAt, wantStart)
	}
}

func TestOpenAICyclePicksTheEarliestResetAndIgnoresForeignGroups(t *testing.T) {
	service, origin, userID, _ := createPersonalUsageFixture(t, true)
	early := time.Date(2026, 7, 21, 3, 0, 0, 0, time.UTC)
	late := time.Date(2026, 7, 23, 3, 0, 0, 0, time.UTC)
	origin.pool = relay.UserUsageGroupPoolUsageState{
		Status: "ok",
		Groups: []relay.UserUsageGroupPoolUsageGroupItem{
			{GroupID: "42", Status: "ok", NextResetAt: &late},
			{GroupID: "43", Status: "ok", NextResetAt: &early},
			{GroupID: "77", Status: "ok", NextResetAt: timePtr(time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC))},
		},
	}

	response, err := service.OpenAICycle(context.Background(), userID)
	if err != nil {
		t.Fatalf("openai cycle: %v", err)
	}
	if response.Status != "ok" {
		t.Fatalf("status = %q, want ok (%s)", response.Status, response.Message)
	}
	if response.ResetAt == nil || !response.ResetAt.Equal(late) {
		t.Fatalf("reset at = %v, want the allowed group reset %v", response.ResetAt, late)
	}
}

func TestOpenAICycleReportsUnavailableWithoutAReset(t *testing.T) {
	service, origin, userID, _ := createPersonalUsageFixture(t, true)
	origin.pool = relay.UserUsageGroupPoolUsageState{
		Status: "ok",
		Groups: []relay.UserUsageGroupPoolUsageGroupItem{{GroupID: "42", Status: "ok"}},
	}

	response, err := service.OpenAICycle(context.Background(), userID)
	if err != nil {
		t.Fatalf("openai cycle: %v", err)
	}
	if response.Status != "unavailable" {
		t.Fatalf("status = %q, want unavailable", response.Status)
	}
	if response.StartAt != nil || response.ResetAt != nil {
		t.Fatalf("unavailable response must not carry timestamps")
	}
}

func TestQuotaWindowHonoursThePinnedWindow(t *testing.T) {
	params := relay.UserUsageDashboardParams{
		StartDate:   "2026-07-15",
		EndDate:     "2026-07-22",
		Granularity: "day",
		QuotaWindow: "weekly",
	}
	if got := quotaWindow(params); got != "weekly" {
		t.Fatalf("quota window = %q, want weekly", got)
	}

	params.QuotaWindow = "bogus"
	if got := quotaWindow(params); got != "monthly" {
		t.Fatalf("quota window = %q, want the span-derived monthly window", got)
	}
}
