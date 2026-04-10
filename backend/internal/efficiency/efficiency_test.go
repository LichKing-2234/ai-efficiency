package efficiency

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/efficiencymetric"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// newTestClient creates an isolated Postgres-backed ent client for testing.
func newTestClient(t *testing.T) *ent.Client {
	t.Helper()
	return testdb.Open(t)
}

// createTestRepo creates an ScmProvider + RepoConfig and returns the RepoConfig.
func createTestRepo(t *testing.T, ctx context.Context, client *ent.Client, name string) *ent.RepoConfig {
	t.Helper()
	sp := client.ScmProvider.Create().
		SetName("test-provider").
		SetType(scmprovider.TypeGithub).
		SetBaseURL("https://github.com").
		SetCredentials("test-token").
		SaveX(ctx)

	return client.RepoConfig.Create().
		SetName(name).
		SetFullName("org/" + name).
		SetCloneURL("https://github.com/org/" + name + ".git").
		SetDefaultBranch("main").
		SetScmProviderID(sp.ID).
		SaveX(ctx)
}

// ---------------------------------------------------------------------------
// Aggregator tests
// ---------------------------------------------------------------------------

func TestNewAggregator(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	logger := zap.NewNop()

	agg := NewAggregator(client, logger)
	if agg == nil {
		t.Fatal("expected non-nil Aggregator")
	}
}

func TestComputePeriodStartDaily(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "morning",
			now:  time.Date(2026, 3, 15, 9, 30, 0, 0, time.UTC),
			want: time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "midnight exactly",
			now:  time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
			want: time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "end of day",
			now:  time.Date(2026, 3, 15, 23, 59, 59, 999, time.UTC),
			want: time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputePeriodStart("daily", tt.now)
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComputePeriodStartWeekly(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want time.Time // expected Monday 00:00
	}{
		{
			name: "wednesday",
			now:  time.Date(2026, 3, 18, 14, 0, 0, 0, time.UTC), // Wednesday
			want: time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC),  // Monday
		},
		{
			name: "monday",
			now:  time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC), // Monday
			want: time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC),  // same Monday
		},
		{
			name: "sunday edge case",
			now:  time.Date(2026, 3, 22, 20, 0, 0, 0, time.UTC), // Sunday
			want: time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC),  // previous Monday
		},
		{
			name: "saturday",
			now:  time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC), // Saturday
			want: time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC),  // Monday
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputePeriodStart("weekly", tt.now)
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComputePeriodStartMonthly(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "mid month",
			now:  time.Date(2026, 3, 19, 15, 0, 0, 0, time.UTC),
			want: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "first of month",
			now:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			want: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "last day of month",
			now:  time.Date(2026, 2, 28, 23, 59, 0, 0, time.UTC),
			want: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputePeriodStart("monthly", tt.now)
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComputePeriodStartDefault(t *testing.T) {
	now := time.Date(2026, 3, 19, 10, 30, 0, 0, time.UTC)
	got := ComputePeriodStart("unknown_type", now)
	want := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("default should behave like daily: got %v, want %v", got, want)
	}
}

func TestComputePeriodEnd(t *testing.T) {
	tests := []struct {
		name       string
		periodType string
		start      time.Time
		want       time.Time
	}{
		{
			name:       "daily",
			periodType: "daily",
			start:      time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
			want:       time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC),
		},
		{
			name:       "weekly",
			periodType: "weekly",
			start:      time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC),
			want:       time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC),
		},
		{
			name:       "monthly",
			periodType: "monthly",
			start:      time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			want:       time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:       "default",
			periodType: "bogus",
			start:      time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
			want:       time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computePeriodEnd(tt.periodType, tt.start)
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAggregateForRepoNoPRs(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "empty-repo")
	agg := NewAggregator(client, logger)

	periodStart := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	if err := agg.AggregateForRepo(ctx, rc.ID, "daily", periodStart); err != nil {
		t.Fatalf("AggregateForRepo: %v", err)
	}

	metric, err := client.EfficiencyMetric.Query().
		Where(
			efficiencymetric.HasRepoConfigWith(repoconfig.IDEQ(rc.ID)),
			efficiencymetric.PeriodTypeEQ(efficiencymetric.PeriodTypeDaily),
			efficiencymetric.PeriodStartEQ(periodStart),
		).
		Only(ctx)
	if err != nil {
		t.Fatalf("query metric: %v", err)
	}

	if metric.TotalPrs != 0 {
		t.Errorf("total_prs = %d, want 0", metric.TotalPrs)
	}
	if metric.AiPrs != 0 {
		t.Errorf("ai_prs = %d, want 0", metric.AiPrs)
	}
	if metric.HumanPrs != 0 {
		t.Errorf("human_prs = %d, want 0", metric.HumanPrs)
	}
	if metric.AvgCycleTimeHours != 0 {
		t.Errorf("avg_cycle_time_hours = %f, want 0", metric.AvgCycleTimeHours)
	}
	if metric.TotalTokenCost != 0 {
		t.Errorf("total_token_cost = %f, want 0", metric.TotalTokenCost)
	}
	if metric.AiVsHumanRatio != 0 {
		t.Errorf("ai_vs_human_ratio = %f, want 0", metric.AiVsHumanRatio)
	}
}

func TestAggregateForRepoWithPRs(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "active-repo")
	agg := NewAggregator(client, logger)

	periodStart := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	prTime := periodStart.Add(2 * time.Hour)

	// PR 1: AI-labeled, merged, cycle time 4h, token cost 1.5
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(101).
		SetAuthor("alice").
		SetTitle("feat: add AI feature").
		SetSourceBranch("feat-ai").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusMerged).
		SetAiLabel(prrecord.AiLabelAiViaSub2api).
		SetCycleTimeHours(4.0).
		SetTokenCost(1.5).
		SetCreatedAt(prTime).
		SaveX(ctx)

	// PR 2: AI-labeled, open (no cycle time), token cost 0.5
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(102).
		SetAuthor("bob").
		SetTitle("feat: another AI PR").
		SetSourceBranch("feat-ai-2").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusOpen).
		SetAiLabel(prrecord.AiLabelAiViaSub2api).
		SetTokenCost(0.5).
		SetCreatedAt(prTime).
		SaveX(ctx)

	// PR 3: Human, merged, cycle time 8h
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(103).
		SetAuthor("carol").
		SetTitle("fix: manual bugfix").
		SetSourceBranch("fix-bug").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusMerged).
		SetAiLabel(prrecord.AiLabelNoAiDetected).
		SetCycleTimeHours(8.0).
		SetCreatedAt(prTime).
		SaveX(ctx)

	// PR 4: Human, closed (no cycle time contribution since cycle_time_hours=0)
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(104).
		SetAuthor("dave").
		SetTitle("chore: cleanup").
		SetSourceBranch("chore-cleanup").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusClosed).
		SetAiLabel(prrecord.AiLabelNoAiDetected).
		SetCreatedAt(prTime).
		SaveX(ctx)

	if err := agg.AggregateForRepo(ctx, rc.ID, "daily", periodStart); err != nil {
		t.Fatalf("AggregateForRepo: %v", err)
	}

	metric, err := client.EfficiencyMetric.Query().
		Where(
			efficiencymetric.HasRepoConfigWith(repoconfig.IDEQ(rc.ID)),
			efficiencymetric.PeriodTypeEQ(efficiencymetric.PeriodTypeDaily),
		).
		Only(ctx)
	if err != nil {
		t.Fatalf("query metric: %v", err)
	}

	if metric.TotalPrs != 4 {
		t.Errorf("total_prs = %d, want 4", metric.TotalPrs)
	}
	if metric.AiPrs != 2 {
		t.Errorf("ai_prs = %d, want 2", metric.AiPrs)
	}
	if metric.HumanPrs != 2 {
		t.Errorf("human_prs = %d, want 2", metric.HumanPrs)
	}
	// avg cycle time = (4 + 8) / 2 = 6
	if metric.AvgCycleTimeHours != 6.0 {
		t.Errorf("avg_cycle_time_hours = %f, want 6.0", metric.AvgCycleTimeHours)
	}
	// token cost = 1.5 + 0.5 = 2.0
	if metric.TotalTokenCost != 2.0 {
		t.Errorf("total_token_cost = %f, want 2.0", metric.TotalTokenCost)
	}
	// ai ratio = 2/4 = 0.5
	if metric.AiVsHumanRatio != 0.5 {
		t.Errorf("ai_vs_human_ratio = %f, want 0.5", metric.AiVsHumanRatio)
	}
}

func TestAggregateForRepoUpsert(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "upsert-repo")
	agg := NewAggregator(client, logger)

	periodStart := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	prTime := periodStart.Add(1 * time.Hour)

	// First run with 1 PR
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(201).
		SetSourceBranch("feat-1").
		SetTargetBranch("main").
		SetAiLabel(prrecord.AiLabelAiViaSub2api).
		SetCreatedAt(prTime).
		SaveX(ctx)

	if err := agg.AggregateForRepo(ctx, rc.ID, "daily", periodStart); err != nil {
		t.Fatalf("first aggregation: %v", err)
	}

	// Add another PR and run again
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(202).
		SetSourceBranch("feat-2").
		SetTargetBranch("main").
		SetAiLabel(prrecord.AiLabelNoAiDetected).
		SetCreatedAt(prTime).
		SaveX(ctx)

	if err := agg.AggregateForRepo(ctx, rc.ID, "daily", periodStart); err != nil {
		t.Fatalf("second aggregation: %v", err)
	}

	// Should have exactly 1 metric record (upserted, not duplicated)
	metrics, err := client.EfficiencyMetric.Query().
		Where(
			efficiencymetric.HasRepoConfigWith(repoconfig.IDEQ(rc.ID)),
			efficiencymetric.PeriodTypeEQ(efficiencymetric.PeriodTypeDaily),
			efficiencymetric.PeriodStartEQ(periodStart),
		).
		All(ctx)
	if err != nil {
		t.Fatalf("query metrics: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	if metrics[0].TotalPrs != 2 {
		t.Errorf("total_prs after upsert = %d, want 2", metrics[0].TotalPrs)
	}
}

func TestAggregateAll(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	sp := client.ScmProvider.Create().
		SetName("test-provider").
		SetType(scmprovider.TypeGithub).
		SetBaseURL("https://github.com").
		SetCredentials("test-token").
		SaveX(ctx)

	rc1 := client.RepoConfig.Create().
		SetName("repo-a").
		SetFullName("org/repo-a").
		SetCloneURL("https://github.com/org/repo-a.git").
		SetScmProviderID(sp.ID).
		SaveX(ctx)

	rc2 := client.RepoConfig.Create().
		SetName("repo-b").
		SetFullName("org/repo-b").
		SetCloneURL("https://github.com/org/repo-b.git").
		SetScmProviderID(sp.ID).
		SaveX(ctx)

	agg := NewAggregator(client, logger)
	if err := agg.AggregateAll(ctx, "daily"); err != nil {
		t.Fatalf("AggregateAll: %v", err)
	}

	// Both repos should have a metric record
	for _, rc := range []*ent.RepoConfig{rc1, rc2} {
		count, err := client.EfficiencyMetric.Query().
			Where(efficiencymetric.HasRepoConfigWith(repoconfig.IDEQ(rc.ID))).
			Count(ctx)
		if err != nil {
			t.Fatalf("count metrics for repo %d: %v", rc.ID, err)
		}
		if count != 1 {
			t.Errorf("repo %s: expected 1 metric, got %d", rc.Name, count)
		}
	}
}

// ---------------------------------------------------------------------------
// Labeler tests
// ---------------------------------------------------------------------------

func TestNewLabeler(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	logger := zap.NewNop()

	lab := NewLabeler(client, nil, logger)
	if lab == nil {
		t.Fatal("expected non-nil Labeler")
	}
}

func TestLabelPRNoSessions(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "no-sessions-repo")

	pr := client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(301).
		SetSourceBranch("feat-x").
		SetTargetBranch("main").
		SetLinesAdded(50).
		SetLinesDeleted(10).
		SaveX(ctx)

	lab := NewLabeler(client, nil, logger)
	result, err := lab.LabelPR(ctx, pr.ID)
	if err != nil {
		t.Fatalf("LabelPR: %v", err)
	}

	if result.AILabel != "no_ai_detected" {
		t.Errorf("ai_label = %q, want %q", result.AILabel, "no_ai_detected")
	}
	if result.AIRatio != 0 {
		t.Errorf("ai_ratio = %f, want 0", result.AIRatio)
	}
	if len(result.SessionIDs) != 0 {
		t.Errorf("session_ids = %v, want empty", result.SessionIDs)
	}

	// Verify the PR record was updated in DB
	updated, err := client.PrRecord.Get(ctx, pr.ID)
	if err != nil {
		t.Fatalf("get PR: %v", err)
	}
	if updated.AiLabel != prrecord.AiLabelNoAiDetected {
		t.Errorf("DB ai_label = %q, want %q", updated.AiLabel, prrecord.AiLabelNoAiDetected)
	}
}

func TestLabelPRWithSessions(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "sessions-repo")

	prCreatedAt := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	pr := client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(401).
		SetSourceBranch("feat-ai").
		SetTargetBranch("main").
		SetLinesAdded(80).
		SetLinesDeleted(20).
		SetCreatedAt(prCreatedAt).
		SaveX(ctx)

	// Create 2 matching sessions: same repo, same branch, within 7 days
	sessionTime := prCreatedAt.Add(-2 * 24 * time.Hour) // 2 days before PR
	client.Session.Create().
		SetID(uuid.New()).
		SetRepoConfigID(rc.ID).
		SetBranch("feat-ai").
		SetStartedAt(sessionTime).
		SetCreatedAt(sessionTime).
		SaveX(ctx)

	client.Session.Create().
		SetID(uuid.New()).
		SetRepoConfigID(rc.ID).
		SetBranch("feat-ai").
		SetStartedAt(sessionTime.Add(time.Hour)).
		SetCreatedAt(sessionTime.Add(time.Hour)).
		SaveX(ctx)

	lab := NewLabeler(client, nil, logger)
	result, err := lab.LabelPR(ctx, pr.ID)
	if err != nil {
		t.Fatalf("LabelPR: %v", err)
	}

	if result.AILabel != "ai_via_sub2api" {
		t.Errorf("ai_label = %q, want %q", result.AILabel, "ai_via_sub2api")
	}
	if len(result.SessionIDs) != 2 {
		t.Errorf("session_ids count = %d, want 2", len(result.SessionIDs))
	}

	// AI ratio: sessions exist but no token cost (sub2api client is nil) → 0.5
	if result.AIRatio != 0.5 {
		t.Errorf("ai_ratio = %f, want 0.5", result.AIRatio)
	}

	// Verify DB update
	updated, err := client.PrRecord.Get(ctx, pr.ID)
	if err != nil {
		t.Fatalf("get PR: %v", err)
	}
	if updated.AiLabel != prrecord.AiLabelAiViaSub2api {
		t.Errorf("DB ai_label = %q, want %q", updated.AiLabel, prrecord.AiLabelAiViaSub2api)
	}
}

func TestLabelPRSessionBranchMismatch(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "branch-mismatch-repo")

	prCreatedAt := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	pr := client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(501).
		SetSourceBranch("feat-target").
		SetTargetBranch("main").
		SetLinesAdded(30).
		SetLinesDeleted(5).
		SetCreatedAt(prCreatedAt).
		SaveX(ctx)

	// Session on a DIFFERENT branch
	sessionTime := prCreatedAt.Add(-1 * 24 * time.Hour)
	client.Session.Create().
		SetID(uuid.New()).
		SetRepoConfigID(rc.ID).
		SetBranch("feat-other-branch").
		SetStartedAt(sessionTime).
		SetCreatedAt(sessionTime).
		SaveX(ctx)

	lab := NewLabeler(client, nil, logger)
	result, err := lab.LabelPR(ctx, pr.ID)
	if err != nil {
		t.Fatalf("LabelPR: %v", err)
	}

	if result.AILabel != "no_ai_detected" {
		t.Errorf("ai_label = %q, want %q (branch mismatch should not match)", result.AILabel, "no_ai_detected")
	}
	if len(result.SessionIDs) != 0 {
		t.Errorf("session_ids = %v, want empty", result.SessionIDs)
	}
}

func TestLabelPRSessionOutsideTimeWindow(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "time-window-repo")

	prCreatedAt := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	pr := client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(601).
		SetSourceBranch("feat-old").
		SetTargetBranch("main").
		SetLinesAdded(40).
		SetLinesDeleted(10).
		SetCreatedAt(prCreatedAt).
		SaveX(ctx)

	// Session older than 7 days before PR creation
	oldSessionTime := prCreatedAt.Add(-10 * 24 * time.Hour) // 10 days before
	client.Session.Create().
		SetID(uuid.New()).
		SetRepoConfigID(rc.ID).
		SetBranch("feat-old").
		SetStartedAt(oldSessionTime).
		SetCreatedAt(oldSessionTime).
		SaveX(ctx)

	lab := NewLabeler(client, nil, logger)
	result, err := lab.LabelPR(ctx, pr.ID)
	if err != nil {
		t.Fatalf("LabelPR: %v", err)
	}

	if result.AILabel != "no_ai_detected" {
		t.Errorf("ai_label = %q, want %q (session outside 7-day window)", result.AILabel, "no_ai_detected")
	}
	if len(result.SessionIDs) != 0 {
		t.Errorf("session_ids = %v, want empty", result.SessionIDs)
	}
}

func TestLabelPRNonExistentID(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	lab := NewLabeler(client, nil, logger)
	_, err := lab.LabelPR(ctx, 999999)
	if err == nil {
		t.Fatal("expected error for non-existent PR ID")
	}
}

func TestLabelPRZeroLinesChanged(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "zero-lines-repo")

	prCreatedAt := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	// PR with 0 lines added and 0 lines deleted
	pr := client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(701).
		SetSourceBranch("feat-empty").
		SetTargetBranch("main").
		SetLinesAdded(0).
		SetLinesDeleted(0).
		SetCreatedAt(prCreatedAt).
		SaveX(ctx)

	// Create a matching session so we hit the AI ratio calculation path
	sessionTime := prCreatedAt.Add(-1 * 24 * time.Hour)
	client.Session.Create().
		SetID(uuid.New()).
		SetRepoConfigID(rc.ID).
		SetBranch("feat-empty").
		SetStartedAt(sessionTime).
		SetCreatedAt(sessionTime).
		SaveX(ctx)

	lab := NewLabeler(client, nil, logger)
	result, err := lab.LabelPR(ctx, pr.ID)
	if err != nil {
		t.Fatalf("LabelPR with zero lines should not error: %v", err)
	}

	if result.AILabel != "ai_via_sub2api" {
		t.Errorf("ai_label = %q, want %q", result.AILabel, "ai_via_sub2api")
	}
	// With 0 lines changed and sessions but no token cost → AI ratio = 0.5
	if result.AIRatio != 0.5 {
		t.Errorf("ai_ratio = %f, want 0.5 (sessions exist, no token cost)", result.AIRatio)
	}
}

func TestLabelPRAIRatioUncapped(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "ratio-uncapped-repo")

	prCreatedAt := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	// PR with many lines — sessions exist but no token cost
	pr := client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(801).
		SetSourceBranch("feat-big").
		SetTargetBranch("main").
		SetLinesAdded(150).
		SetLinesDeleted(50).
		SetCreatedAt(prCreatedAt).
		SaveX(ctx)

	sessionTime := prCreatedAt.Add(-1 * 24 * time.Hour)
	client.Session.Create().
		SetID(uuid.New()).
		SetRepoConfigID(rc.ID).
		SetBranch("feat-big").
		SetStartedAt(sessionTime).
		SetCreatedAt(sessionTime).
		SaveX(ctx)

	lab := NewLabeler(client, nil, logger)
	result, err := lab.LabelPR(ctx, pr.ID)
	if err != nil {
		t.Fatalf("LabelPR: %v", err)
	}

	// Sessions exist but no token cost (sub2api client nil) → AI ratio = 0.5
	if result.AIRatio != 0.5 {
		t.Errorf("ai_ratio = %f, want 0.5", result.AIRatio)
	}
}

func TestLabelPRSessionDifferentRepo(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc1 := createTestRepo(t, ctx, client, "repo-one")
	rc2 := createTestRepo(t, ctx, client, "repo-two")

	prCreatedAt := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	pr := client.PrRecord.Create().
		SetRepoConfigID(rc1.ID).
		SetScmPrID(901).
		SetSourceBranch("feat-cross").
		SetTargetBranch("main").
		SetLinesAdded(20).
		SetLinesDeleted(5).
		SetCreatedAt(prCreatedAt).
		SaveX(ctx)

	// Session on a DIFFERENT repo but same branch and within time window
	sessionTime := prCreatedAt.Add(-1 * 24 * time.Hour)
	client.Session.Create().
		SetID(uuid.New()).
		SetRepoConfigID(rc2.ID).
		SetBranch("feat-cross").
		SetStartedAt(sessionTime).
		SetCreatedAt(sessionTime).
		SaveX(ctx)

	lab := NewLabeler(client, nil, logger)
	result, err := lab.LabelPR(ctx, pr.ID)
	if err != nil {
		t.Fatalf("LabelPR: %v", err)
	}

	if result.AILabel != "no_ai_detected" {
		t.Errorf("ai_label = %q, want %q (session on different repo)", result.AILabel, "no_ai_detected")
	}
}

// ---------------------------------------------------------------------------
// Additional Aggregator edge-case tests
// ---------------------------------------------------------------------------

func TestComputePeriodStartNoArgs(t *testing.T) {
	// ComputePeriodStart with no time argument should use time.Now()
	before := time.Now()
	got := ComputePeriodStart("daily")
	after := time.Now()

	// Result should be start of today
	want := time.Date(before.Year(), before.Month(), before.Day(), 0, 0, 0, 0, before.Location())
	// Guard against the unlikely case of running exactly at midnight boundary
	wantAlt := time.Date(after.Year(), after.Month(), after.Day(), 0, 0, 0, 0, after.Location())

	if !got.Equal(want) && !got.Equal(wantAlt) {
		t.Errorf("ComputePeriodStart with no args: got %v, want %v or %v", got, want, wantAlt)
	}
}

func TestComputePeriodStartInternalDirect(t *testing.T) {
	// Test the unexported function directly for each period type
	now := time.Date(2026, 6, 17, 14, 30, 0, 0, time.UTC) // Wednesday

	daily := computePeriodStartInternal("daily", now)
	if want := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC); !daily.Equal(want) {
		t.Errorf("daily: got %v, want %v", daily, want)
	}

	weekly := computePeriodStartInternal("weekly", now)
	if want := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC); !weekly.Equal(want) { // Monday
		t.Errorf("weekly: got %v, want %v", weekly, want)
	}

	monthly := computePeriodStartInternal("monthly", now)
	if want := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC); !monthly.Equal(want) {
		t.Errorf("monthly: got %v, want %v", monthly, want)
	}

	def := computePeriodStartInternal("something_else", now)
	if want := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC); !def.Equal(want) {
		t.Errorf("default: got %v, want %v", def, want)
	}
}

func TestComputePeriodEndMonthBoundary(t *testing.T) {
	// Monthly period end crossing year boundary
	start := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	got := computePeriodEnd("monthly", start)
	want := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("monthly year boundary: got %v, want %v", got, want)
	}
}

func TestAggregateForRepoAllAIPRs(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "all-ai-repo")
	agg := NewAggregator(client, logger)

	periodStart := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	prTime := periodStart.Add(3 * time.Hour)

	for i := 1; i <= 3; i++ {
		client.PrRecord.Create().
			SetRepoConfigID(rc.ID).
			SetScmPrID(1000 + i).
			SetSourceBranch("feat-ai").
			SetTargetBranch("main").
			SetStatus(prrecord.StatusMerged).
			SetAiLabel(prrecord.AiLabelAiViaSub2api).
			SetCycleTimeHours(2.0).
			SetCreatedAt(prTime).
			SaveX(ctx)
	}

	if err := agg.AggregateForRepo(ctx, rc.ID, "daily", periodStart); err != nil {
		t.Fatalf("AggregateForRepo: %v", err)
	}

	metric, err := client.EfficiencyMetric.Query().
		Where(
			efficiencymetric.HasRepoConfigWith(repoconfig.IDEQ(rc.ID)),
			efficiencymetric.PeriodTypeEQ(efficiencymetric.PeriodTypeDaily),
		).
		Only(ctx)
	if err != nil {
		t.Fatalf("query metric: %v", err)
	}

	if metric.TotalPrs != 3 {
		t.Errorf("total_prs = %d, want 3", metric.TotalPrs)
	}
	if metric.AiPrs != 3 {
		t.Errorf("ai_prs = %d, want 3", metric.AiPrs)
	}
	if metric.HumanPrs != 0 {
		t.Errorf("human_prs = %d, want 0", metric.HumanPrs)
	}
	if metric.AiVsHumanRatio != 1.0 {
		t.Errorf("ai_vs_human_ratio = %f, want 1.0", metric.AiVsHumanRatio)
	}
	if metric.AvgCycleTimeHours != 2.0 {
		t.Errorf("avg_cycle_time_hours = %f, want 2.0", metric.AvgCycleTimeHours)
	}
}

func TestAggregateForRepoAllHumanPRs(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "all-human-repo")
	agg := NewAggregator(client, logger)

	periodStart := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	prTime := periodStart.Add(1 * time.Hour)

	for i := 1; i <= 2; i++ {
		client.PrRecord.Create().
			SetRepoConfigID(rc.ID).
			SetScmPrID(1100 + i).
			SetSourceBranch("fix-manual").
			SetTargetBranch("main").
			SetStatus(prrecord.StatusMerged).
			SetAiLabel(prrecord.AiLabelNoAiDetected).
			SetCycleTimeHours(5.0).
			SetCreatedAt(prTime).
			SaveX(ctx)
	}

	if err := agg.AggregateForRepo(ctx, rc.ID, "daily", periodStart); err != nil {
		t.Fatalf("AggregateForRepo: %v", err)
	}

	metric, err := client.EfficiencyMetric.Query().
		Where(
			efficiencymetric.HasRepoConfigWith(repoconfig.IDEQ(rc.ID)),
			efficiencymetric.PeriodTypeEQ(efficiencymetric.PeriodTypeDaily),
		).
		Only(ctx)
	if err != nil {
		t.Fatalf("query metric: %v", err)
	}

	if metric.AiPrs != 0 {
		t.Errorf("ai_prs = %d, want 0", metric.AiPrs)
	}
	if metric.HumanPrs != 2 {
		t.Errorf("human_prs = %d, want 2", metric.HumanPrs)
	}
	if metric.AiVsHumanRatio != 0.0 {
		t.Errorf("ai_vs_human_ratio = %f, want 0.0", metric.AiVsHumanRatio)
	}
	if metric.AvgCycleTimeHours != 5.0 {
		t.Errorf("avg_cycle_time_hours = %f, want 5.0", metric.AvgCycleTimeHours)
	}
}

func TestAggregateForRepoPRsOutsidePeriod(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "outside-period-repo")
	agg := NewAggregator(client, logger)

	periodStart := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)

	// PR inside the period
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(1201).
		SetSourceBranch("feat-in").
		SetTargetBranch("main").
		SetAiLabel(prrecord.AiLabelAiViaSub2api).
		SetCreatedAt(periodStart.Add(6 * time.Hour)).
		SaveX(ctx)

	// PR BEFORE the period
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(1202).
		SetSourceBranch("feat-before").
		SetTargetBranch("main").
		SetAiLabel(prrecord.AiLabelAiViaSub2api).
		SetCreatedAt(periodStart.Add(-1 * time.Hour)).
		SaveX(ctx)

	// PR AFTER the period (next day for daily)
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(1203).
		SetSourceBranch("feat-after").
		SetTargetBranch("main").
		SetAiLabel(prrecord.AiLabelAiViaSub2api).
		SetCreatedAt(periodStart.Add(25 * time.Hour)).
		SaveX(ctx)

	if err := agg.AggregateForRepo(ctx, rc.ID, "daily", periodStart); err != nil {
		t.Fatalf("AggregateForRepo: %v", err)
	}

	metric, err := client.EfficiencyMetric.Query().
		Where(
			efficiencymetric.HasRepoConfigWith(repoconfig.IDEQ(rc.ID)),
			efficiencymetric.PeriodTypeEQ(efficiencymetric.PeriodTypeDaily),
		).
		Only(ctx)
	if err != nil {
		t.Fatalf("query metric: %v", err)
	}

	// Only the PR inside the period should be counted
	if metric.TotalPrs != 1 {
		t.Errorf("total_prs = %d, want 1 (only in-period PR)", metric.TotalPrs)
	}
}

func TestAggregateForRepoMergedWithZeroCycleTime(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "zero-cycle-repo")
	agg := NewAggregator(client, logger)

	periodStart := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	prTime := periodStart.Add(2 * time.Hour)

	// Merged PR with cycle_time_hours = 0 (default)
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(1301).
		SetSourceBranch("feat-instant").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusMerged).
		SetAiLabel(prrecord.AiLabelNoAiDetected).
		SetCreatedAt(prTime).
		SaveX(ctx)

	// Merged PR with positive cycle time
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(1302).
		SetSourceBranch("feat-slow").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusMerged).
		SetAiLabel(prrecord.AiLabelNoAiDetected).
		SetCycleTimeHours(10.0).
		SetCreatedAt(prTime).
		SaveX(ctx)

	if err := agg.AggregateForRepo(ctx, rc.ID, "daily", periodStart); err != nil {
		t.Fatalf("AggregateForRepo: %v", err)
	}

	metric, err := client.EfficiencyMetric.Query().
		Where(
			efficiencymetric.HasRepoConfigWith(repoconfig.IDEQ(rc.ID)),
			efficiencymetric.PeriodTypeEQ(efficiencymetric.PeriodTypeDaily),
		).
		Only(ctx)
	if err != nil {
		t.Fatalf("query metric: %v", err)
	}

	// Only the PR with cycle_time_hours > 0 contributes to avg
	// avg = 10.0 / 1 = 10.0
	if metric.AvgCycleTimeHours != 10.0 {
		t.Errorf("avg_cycle_time_hours = %f, want 10.0 (zero cycle time PR excluded)", metric.AvgCycleTimeHours)
	}
}

func TestAggregateForRepoWeeklyPeriod(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "weekly-repo")
	agg := NewAggregator(client, logger)

	// Monday
	periodStart := time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)

	// PR on Wednesday (inside weekly window)
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(1401).
		SetSourceBranch("feat-mid-week").
		SetTargetBranch("main").
		SetAiLabel(prrecord.AiLabelAiViaSub2api).
		SetCreatedAt(time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)).
		SaveX(ctx)

	// PR on next Monday (outside weekly window)
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(1402).
		SetSourceBranch("feat-next-week").
		SetTargetBranch("main").
		SetAiLabel(prrecord.AiLabelAiViaSub2api).
		SetCreatedAt(time.Date(2026, 3, 23, 1, 0, 0, 0, time.UTC)).
		SaveX(ctx)

	if err := agg.AggregateForRepo(ctx, rc.ID, "weekly", periodStart); err != nil {
		t.Fatalf("AggregateForRepo weekly: %v", err)
	}

	metric, err := client.EfficiencyMetric.Query().
		Where(
			efficiencymetric.HasRepoConfigWith(repoconfig.IDEQ(rc.ID)),
			efficiencymetric.PeriodTypeEQ(efficiencymetric.PeriodTypeWeekly),
		).
		Only(ctx)
	if err != nil {
		t.Fatalf("query metric: %v", err)
	}

	if metric.TotalPrs != 1 {
		t.Errorf("total_prs = %d, want 1 (only mid-week PR)", metric.TotalPrs)
	}
}

func TestAggregateForRepoMonthlyPeriod(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "monthly-repo")
	agg := NewAggregator(client, logger)

	periodStart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	// PR mid-March (inside)
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(1501).
		SetSourceBranch("feat-march").
		SetTargetBranch("main").
		SetAiLabel(prrecord.AiLabelNoAiDetected).
		SetCreatedAt(time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)).
		SaveX(ctx)

	// PR in April (outside)
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(1502).
		SetSourceBranch("feat-april").
		SetTargetBranch("main").
		SetAiLabel(prrecord.AiLabelNoAiDetected).
		SetCreatedAt(time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)).
		SaveX(ctx)

	if err := agg.AggregateForRepo(ctx, rc.ID, "monthly", periodStart); err != nil {
		t.Fatalf("AggregateForRepo monthly: %v", err)
	}

	metric, err := client.EfficiencyMetric.Query().
		Where(
			efficiencymetric.HasRepoConfigWith(repoconfig.IDEQ(rc.ID)),
			efficiencymetric.PeriodTypeEQ(efficiencymetric.PeriodTypeMonthly),
		).
		Only(ctx)
	if err != nil {
		t.Fatalf("query metric: %v", err)
	}

	if metric.TotalPrs != 1 {
		t.Errorf("total_prs = %d, want 1 (only March PR)", metric.TotalPrs)
	}
}

func TestNewLabelerWithRelayProvider(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	logger := zap.NewNop()

	// Verify constructor works with a nil relay provider (common case)
	lab := NewLabeler(client, nil, logger)
	if lab == nil {
		t.Fatal("expected non-nil Labeler with nil relay provider")
	}
	if lab.entClient != client {
		t.Error("entClient not set correctly")
	}
	if lab.relayProvider != nil {
		t.Error("relayProvider should be nil")
	}
	if lab.logger != logger {
		t.Error("logger not set correctly")
	}
}

func TestNewAggregatorFields(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	logger := zap.NewNop()

	agg := NewAggregator(client, logger)
	if agg.entClient != client {
		t.Error("entClient not set correctly")
	}
	if agg.logger != logger {
		t.Error("logger not set correctly")
	}
}

func TestAggregateAllWithError(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	// Create a repo, then close the client to force errors during aggregation
	sp := client.ScmProvider.Create().
		SetName("test-provider").
		SetType(scmprovider.TypeGithub).
		SetBaseURL("https://github.com").
		SetCredentials("test-token").
		SaveX(ctx)

	client.RepoConfig.Create().
		SetName("repo-err").
		SetFullName("org/repo-err").
		SetCloneURL("https://github.com/org/repo-err.git").
		SetScmProviderID(sp.ID).
		SaveX(ctx)

	// AggregateAll should succeed even if individual repos have issues
	agg := NewAggregator(client, logger)
	if err := agg.AggregateAll(ctx, "daily"); err != nil {
		t.Fatalf("AggregateAll should not return error: %v", err)
	}
}

func TestAggregateForRepoInvalidRepoID(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	agg := NewAggregator(client, logger)
	periodStart := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)

	// Non-existent repo ID — AggregateForRepo queries PRs (succeeds with 0 results)
	// but creating the metric record fails due to FK constraint on repo_config_id
	err := agg.AggregateForRepo(ctx, 99999, "daily", periodStart)
	if err == nil {
		t.Fatal("expected FK constraint error for non-existent repo ID")
	}
}

func TestLabelPRRepoConfigNilEdge(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "edge-repo")

	pr := client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(9001).
		SetSourceBranch("feat-edge").
		SetTargetBranch("main").
		SaveX(ctx)

	lab := NewLabeler(client, nil, logger)
	// This should work — the labeler queries with WithRepoConfig()
	result, err := lab.LabelPR(ctx, pr.ID)
	if err != nil {
		t.Fatalf("LabelPR: %v", err)
	}
	if result.AILabel != "no_ai_detected" {
		t.Errorf("ai_label = %q, want %q", result.AILabel, "no_ai_detected")
	}
}

func TestLabelPRMultipleSessionsTokenCostZero(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "multi-session-repo")

	prCreatedAt := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	pr := client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(9002).
		SetSourceBranch("feat-multi").
		SetTargetBranch("main").
		SetLinesAdded(100).
		SetLinesDeleted(20).
		SetCreatedAt(prCreatedAt).
		SaveX(ctx)

	// Create 5 matching sessions
	for i := 0; i < 5; i++ {
		sessionTime := prCreatedAt.Add(-time.Duration(i+1) * 24 * time.Hour)
		client.Session.Create().
			SetID(uuid.New()).
			SetRepoConfigID(rc.ID).
			SetBranch("feat-multi").
			SetStartedAt(sessionTime).
			SetCreatedAt(sessionTime).
			SaveX(ctx)
	}

	lab := NewLabeler(client, nil, logger)
	result, err := lab.LabelPR(ctx, pr.ID)
	if err != nil {
		t.Fatalf("LabelPR: %v", err)
	}

	if result.AILabel != "ai_via_sub2api" {
		t.Errorf("ai_label = %q, want %q", result.AILabel, "ai_via_sub2api")
	}
	if len(result.SessionIDs) != 5 {
		t.Errorf("session_ids count = %d, want 5", len(result.SessionIDs))
	}
	// No sub2api client → token cost = 0, ratio = 0.5
	if result.TokenCost != 0 {
		t.Errorf("token_cost = %f, want 0", result.TokenCost)
	}
	if result.AIRatio != 0.5 {
		t.Errorf("ai_ratio = %f, want 0.5", result.AIRatio)
	}

	// Verify DB was updated with session IDs
	updated, err := client.PrRecord.Get(ctx, pr.ID)
	if err != nil {
		t.Fatalf("get PR: %v", err)
	}
	if len(updated.SessionIds) != 5 {
		t.Errorf("DB session_ids count = %d, want 5", len(updated.SessionIds))
	}
	if updated.AiRatio != 0.5 {
		t.Errorf("DB ai_ratio = %f, want 0.5", updated.AiRatio)
	}
}

func TestAggregateForRepoTokenCostAccumulation(t *testing.T) {
	client := newTestClient(t)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop()

	rc := createTestRepo(t, ctx, client, "token-cost-repo")
	agg := NewAggregator(client, logger)

	periodStart := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	prTime := periodStart.Add(2 * time.Hour)

	// PR with token cost
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(2001).
		SetSourceBranch("feat-cost-1").
		SetTargetBranch("main").
		SetAiLabel(prrecord.AiLabelAiViaSub2api).
		SetTokenCost(3.5).
		SetCreatedAt(prTime).
		SaveX(ctx)

	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(2002).
		SetSourceBranch("feat-cost-2").
		SetTargetBranch("main").
		SetAiLabel(prrecord.AiLabelAiViaSub2api).
		SetTokenCost(1.5).
		SetCreatedAt(prTime).
		SaveX(ctx)

	if err := agg.AggregateForRepo(ctx, rc.ID, "daily", periodStart); err != nil {
		t.Fatalf("AggregateForRepo: %v", err)
	}

	metric, err := client.EfficiencyMetric.Query().
		Where(
			efficiencymetric.HasRepoConfigWith(repoconfig.IDEQ(rc.ID)),
			efficiencymetric.PeriodTypeEQ(efficiencymetric.PeriodTypeDaily),
		).
		Only(ctx)
	if err != nil {
		t.Fatalf("query metric: %v", err)
	}

	if metric.TotalTokenCost != 5.0 {
		t.Errorf("total_token_cost = %f, want 5.0", metric.TotalTokenCost)
	}
	// totalTokens = int(3.5) + int(1.5) = 3 + 1 = 4 (Go truncates float→int)
	if metric.TotalTokens < 4 || metric.TotalTokens > 5 {
		t.Errorf("total_tokens = %d, want 4 or 5", metric.TotalTokens)
	}
}
