package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/internal/activity"
	"github.com/ai-efficiency/backend/internal/personalusage"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/teamusage"
)

type activityDenominatorResolver struct {
	personal personalDashboardReader
	team     teamSummaryReader
	now      func() time.Time
	client   *ent.Client
	cache    *activity.Cache
}

type personalDashboardReader interface {
	Dashboard(context.Context, personalusage.Request) (*personalusage.Snapshot, error)
}
type teamSummaryReader interface {
	DepartmentSummary(context.Context, int, string, teamusage.OverviewParams) (*teamusage.DepartmentSummaryResponse, error)
}

func (r *activityDenominatorResolver) ResolveDenominator(ctx context.Context, request activity.V2DenominatorRequest) (activity.V2Denominator, error) {
	// Usage currently has one authoritative primary-provider read contract.
	// Fail closed instead of silently returning a partial denominator when the
	// configured provider set expands beyond that contract.
	var providerID int
	var providerVersion int64
	providerSet := ""
	if r.client != nil {
		providers, err := r.client.RelayProvider.Query().Where(relayprovider.EnabledEQ(true)).Order(ent.Asc(relayprovider.FieldID)).All(ctx)
		if err != nil {
			return activity.V2Denominator{}, fmt.Errorf("resolve Usage provider set: %w", err)
		}
		parts := make([]string, 0, len(providers))
		for _, provider := range providers {
			parts = append(parts, fmt.Sprintf("%d:%d", provider.ID, provider.ConfigurationVersion))
		}
		providerSet = strings.Join(parts, ",")
		if providerSet != request.ProviderSet || len(providers) != 1 {
			return activity.V2Denominator{}, nil
		}
		providerID, providerVersion = providers[0].ID, providers[0].ConfigurationVersion
	}
	params := relay.UserUsageDashboardParams{StartDate: request.FromDate, EndDate: request.ToDate, Granularity: "day", Timezone: request.Timezone}
	if request.Scope == activity.V2ScopeTeam {
		if r.team == nil {
			return activity.V2Denominator{}, fmt.Errorf("team Usage service unavailable")
		}
		result, err := r.team.DepartmentSummary(ctx, request.ActorUserID, request.TeamID, teamusage.OverviewParams{StartDate: request.FromDate, EndDate: request.ToDate, Granularity: "day", Timezone: request.Timezone})
		if err != nil {
			return activity.V2Denominator{}, err
		}
		if result.ScopeVersion != request.ScopeVersion || result.RangeTotalTokens == nil || *result.RangeTotalTokens < 0 || result.SourceStatus != "ok" || result.AsOf.IsZero() || result.Window.StartDate != request.FromDate || result.Window.EndDate != request.ToDate || result.Window.Timezone != request.Timezone || result.Window.Granularity != "day" {
			return activity.V2Denominator{}, nil
		}
		return activity.V2Denominator{TotalTokens: *result.RangeTotalTokens, AsOf: result.AsOf, FreshUntil: result.FreshUntil, Fresh: r.currentTime().Before(result.FreshUntil), Complete: true, ProviderSet: providerSet}, nil
	}
	if r.personal == nil {
		return activity.V2Denominator{}, fmt.Errorf("personal Usage service unavailable")
	}
	userID := request.ActorUserID
	if request.Scope == activity.V2ScopeMember {
		userID = request.SubjectUserID
	}
	var memberKey activity.V2MemberDenominatorCacheKey
	if request.Scope == activity.V2ScopeMember && r.client != nil && r.cache != nil {
		subject, err := r.client.User.Get(ctx, userID)
		if err != nil {
			return activity.V2Denominator{}, fmt.Errorf("resolve member Usage binding: %w", err)
		}
		memberKey = activity.V2MemberDenominatorCacheKey{ActorUserID: request.ActorUserID, SubjectUserID: userID, ScopeVersion: request.ScopeVersion, ProviderID: providerID, ProviderVersion: providerVersion, BindingVersion: subject.UpdatedAt.UTC().UnixNano(), FromDate: request.FromDate, ToDate: request.ToDate, Timezone: request.Timezone}
		var cached activity.V2Denominator
		if r.cache.ReadMemberDenominator(ctx, memberKey, &cached) {
			cached.Fresh = r.currentTime().Before(cached.FreshUntil)
			return cached, nil
		}
	}
	result, err := r.personal.Dashboard(ctx, personalusage.Request{UserID: userID, Params: params, IncludeGroupQuotas: false})
	if err != nil {
		return activity.V2Denominator{}, err
	}
	if !result.Configured || result.Stats == nil || result.Stats.TotalTokens < 0 || result.UsageFreshness == nil || result.UsageFreshness.AsOf.IsZero() || result.UsageFreshness.SourceStatus != "ok" || result.Range.StartDate != request.FromDate || result.Range.EndDate != request.ToDate || result.Range.Timezone != request.Timezone || result.Range.Granularity != "day" {
		return activity.V2Denominator{}, nil
	}
	denominator := activity.V2Denominator{TotalTokens: result.Stats.TotalTokens, AsOf: result.UsageFreshness.AsOf, FreshUntil: result.UsageFreshness.FreshUntil, Fresh: r.currentTime().Before(result.UsageFreshness.FreshUntil), Complete: true, ProviderSet: providerSet}
	if request.Scope == activity.V2ScopeMember && r.cache != nil && memberKey.SubjectUserID > 0 {
		r.cache.WriteMemberDenominator(ctx, memberKey, denominator)
	}
	return denominator, nil
}

func (r *activityDenominatorResolver) currentTime() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now().UTC()
}
