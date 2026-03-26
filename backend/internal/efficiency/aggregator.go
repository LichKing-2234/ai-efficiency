package efficiency

import (
	"context"
	"fmt"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/efficiencymetric"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"go.uber.org/zap"
)

// Aggregator computes efficiency metrics from PR records.
type Aggregator struct {
	entClient *ent.Client
	logger    *zap.Logger
}

// NewAggregator creates a new Aggregator.
func NewAggregator(entClient *ent.Client, logger *zap.Logger) *Aggregator {
	return &Aggregator{
		entClient: entClient,
		logger:    logger,
	}
}

// AggregateForRepo computes metrics for a repo over a given period.
func (a *Aggregator) AggregateForRepo(ctx context.Context, repoConfigID int, periodType string, periodStart time.Time) error {
	periodEnd := computePeriodEnd(periodType, periodStart)

	// Query PR records in the period
	prs, err := a.entClient.PrRecord.Query().
		Where(
			prrecord.HasRepoConfigWith(repoconfig.IDEQ(repoConfigID)),
			prrecord.CreatedAtGTE(periodStart),
			prrecord.CreatedAtLT(periodEnd),
		).
		All(ctx)
	if err != nil {
		return fmt.Errorf("query PRs: %w", err)
	}

	totalPRs := len(prs)
	aiPRs := 0
	humanPRs := 0
	var totalCycleTime float64
	mergedCount := 0
	var totalTokens int
	var totalTokenCost float64

	for _, pr := range prs {
		switch pr.AiLabel {
		case prrecord.AiLabelAiViaSub2api:
			aiPRs++
		default:
			humanPRs++
		}
		if pr.Status == prrecord.StatusMerged && pr.CycleTimeHours > 0 {
			totalCycleTime += pr.CycleTimeHours
			mergedCount++
		}
		totalTokenCost += pr.TokenCost
		totalTokens += int(pr.TokenCost) // approximate token count from cost
	}

	avgCycleTime := 0.0
	if mergedCount > 0 {
		avgCycleTime = totalCycleTime / float64(mergedCount)
	}

	aiRatio := 0.0
	if totalPRs > 0 {
		aiRatio = float64(aiPRs) / float64(totalPRs)
	}

	// Upsert metric
	existing, err := a.entClient.EfficiencyMetric.Query().
		Where(
			efficiencymetric.HasRepoConfigWith(repoconfig.IDEQ(repoConfigID)),
			efficiencymetric.PeriodTypeEQ(efficiencymetric.PeriodType(periodType)),
			efficiencymetric.PeriodStartEQ(periodStart),
		).
		Only(ctx)

	if err != nil && !ent.IsNotFound(err) {
		return fmt.Errorf("query existing metric: %w", err)
	}

	if existing != nil {
		// Update
		return a.entClient.EfficiencyMetric.UpdateOneID(existing.ID).
			SetTotalPrs(totalPRs).
			SetAiPrs(aiPRs).
			SetHumanPrs(humanPRs).
			SetAvgCycleTimeHours(avgCycleTime).
			SetTotalTokens(totalTokens).
			SetTotalTokenCost(totalTokenCost).
			SetAiVsHumanRatio(aiRatio).
			Exec(ctx)
	}

	// Create
	_, err = a.entClient.EfficiencyMetric.Create().
		SetRepoConfigID(repoConfigID).
		SetPeriodType(efficiencymetric.PeriodType(periodType)).
		SetPeriodStart(periodStart).
		SetTotalPrs(totalPRs).
		SetAiPrs(aiPRs).
		SetHumanPrs(humanPRs).
		SetAvgCycleTimeHours(avgCycleTime).
		SetTotalTokens(totalTokens).
		SetTotalTokenCost(totalTokenCost).
		SetAiVsHumanRatio(aiRatio).
		Save(ctx)
	return err
}

// AggregateAll runs aggregation for all repos for the given period.
func (a *Aggregator) AggregateAll(ctx context.Context, periodType string) error {
	repos, err := a.entClient.RepoConfig.Query().All(ctx)
	if err != nil {
		return fmt.Errorf("list repos: %w", err)
	}

	periodStart := computePeriodStartInternal(periodType, time.Now())

	for _, rc := range repos {
		if err := a.AggregateForRepo(ctx, rc.ID, periodType, periodStart); err != nil {
			a.logger.Warn("aggregation failed for repo",
				zap.Int("repo_id", rc.ID),
				zap.Error(err),
			)
		}
	}
	return nil
}

// ComputePeriodStart returns the start of the current period for the given type.
func ComputePeriodStart(periodType string, now ...time.Time) time.Time {
	t := time.Now()
	if len(now) > 0 {
		t = now[0]
	}
	return computePeriodStartInternal(periodType, t)
}

func computePeriodStartInternal(periodType string, now time.Time) time.Time {
	switch periodType {
	case "daily":
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	case "weekly":
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		monday := now.AddDate(0, 0, -(weekday - 1))
		return time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, now.Location())
	case "monthly":
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	default:
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	}
}

func computePeriodEnd(periodType string, start time.Time) time.Time {
	switch periodType {
	case "daily":
		return start.AddDate(0, 0, 1)
	case "weekly":
		return start.AddDate(0, 0, 7)
	case "monthly":
		return start.AddDate(0, 1, 0)
	default:
		return start.AddDate(0, 0, 1)
	}
}
