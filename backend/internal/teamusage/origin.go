package teamusage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
)

var errPrewarmAuthorizationChanged = errors.New("team usage prewarm authorization changed")

func (s *Service) loadPrewarmFirstScopeOrigin(ctx context.Context, request *splitReadRequest) (*teamUsageScopeOrigin, bool, error) {
	overviewSubjects := request.scope.OverviewSubjects
	if len(overviewSubjects) == 0 {
		overviewSubjects = request.scope.Subjects
	}
	prewarmReader := s.currentPrewarmReader()
	if request.bypassPrewarm || prewarmReader == nil {
		if len(overviewSubjects) > s.fullScopeCap || s.originCache == nil {
			return nil, false, nil
		}
		origin, err := s.loadSharedScopeOrigin(ctx, request)
		return origin, true, err
	}

	provider, err := s.providerResolver.Resolve(ctx, request.providerConfig.ID)
	if err != nil {
		return nil, true, fmt.Errorf("resolve primary relay provider origin: %w", err)
	}
	subjects, relayUserIDs, err := s.resolveOverviewSubjects(ctx, request.scope, provider)
	if err != nil {
		return nil, true, err
	}
	origin, _, readErr := prewarmReader.ReadAuthorizedOrigin(ctx, PrewarmReadRequest{
		ProviderID: request.providerConfig.ID, ActorUserID: request.actorUserID,
		ProviderVersion: request.providerConfig.ConfigurationVersion, ScopeVersion: request.scope.Version,
		Params: request.params, AuthorizedRelayUserIDs: sortedUniqueInt64s(relayUserIDs), Provider: provider,
	})
	if ctx.Err() != nil {
		return nil, true, ctx.Err()
	}
	if readErr == nil && origin != nil {
		currentScope, scopeErr := s.requireRepresentativeScope(ctx, request.actorUserID)
		if scopeErr != nil {
			return nil, true, fmt.Errorf("%w: re-resolve representative scope: %w", errPrewarmAuthorizationChanged, scopeErr)
		}
		currentProvider, providerErr := s.resolvePrimaryProviderConfig(ctx)
		if providerErr != nil {
			return nil, true, fmt.Errorf("%w: re-resolve primary provider configuration: %w", errPrewarmAuthorizationChanged, providerErr)
		}
		if currentScope.Version != request.scope.Version || currentProvider.ID != request.providerConfig.ID ||
			currentProvider.ConfigurationVersion != request.providerConfig.ConfigurationVersion {
			return nil, true, errPrewarmAuthorizationChanged
		}
		origin.subjects = subjects
		return origin, true, nil
	}

	if len(overviewSubjects) > s.fullScopeCap {
		return nil, false, nil
	}
	if s.originCache == nil {
		origin, err = s.loadTeamUsageScopeOrigin(ctx, request.scope, provider, request.params)
		return origin, true, err
	}
	origin, err = s.loadSharedScopeOriginWithProvider(ctx, request, provider)
	return origin, true, err
}

func (s *Service) loadSharedScopeOrigin(ctx context.Context, request *splitReadRequest) (*teamUsageScopeOrigin, error) {
	if s == nil || s.originCache == nil {
		return nil, fmt.Errorf("team usage origin cache is not configured")
	}
	provider, err := s.providerResolver.Resolve(ctx, request.providerConfig.ID)
	if err != nil {
		return nil, fmt.Errorf("resolve primary relay provider origin: %w", err)
	}
	return s.loadSharedScopeOriginWithProvider(ctx, request, provider)
}

func (s *Service) loadSharedScopeOriginWithProvider(ctx context.Context, request *splitReadRequest, provider relay.Provider) (*teamUsageScopeOrigin, error) {
	origin, err := s.originCache.GetOrLoad(ctx, OriginCacheKey{
		ProviderID: request.providerConfig.ID, ProviderVersion: request.providerConfig.ConfigurationVersion,
		ScopeVersion: request.scope.Version, ScopeHash: request.scopeHash, Params: request.params,
	}, func(loadCtx context.Context) (*teamUsageScopeOrigin, error) {
		return s.loadTeamUsageScopeOrigin(loadCtx, request.scope, provider, request.params)
	})
	if err != nil {
		return nil, err
	}
	if len(origin.subjects) == 0 {
		origin, err = s.hydrateCachedScopeOrigin(ctx, request.scope, provider, request.params, origin)
		if err != nil {
			return nil, err
		}
	}
	return origin, nil
}

func (s *Service) loadTeamUsageScopeOrigin(
	ctx context.Context,
	scope *representativescope.Scope,
	provider relay.Provider,
	params OverviewParams,
) (*teamUsageScopeOrigin, error) {
	summaryProvider, ok := provider.(relay.TeamUsageSummaryProvider)
	if !ok {
		return nil, ErrProviderUnsupported
	}
	trendProvider, ok := provider.(relay.TeamMemberTrendProvider)
	if !ok {
		return nil, ErrProviderUnsupported
	}
	subjects, relayUserIDs, err := s.resolveOverviewSubjects(ctx, scope, provider)
	if err != nil {
		return nil, err
	}
	relayUserIDs = sortedUniqueInt64s(relayUserIDs)
	statsByRelayUserID, err := s.loadTeamUsageStats(ctx, summaryProvider, relayUserIDs, nil, relay.TeamUsageSummaryParams{
		StartDate: strings.TrimSpace(params.StartDate), EndDate: strings.TrimSpace(params.EndDate),
		Granularity: strings.TrimSpace(params.Granularity), Timezone: strings.TrimSpace(params.Timezone),
	})
	if err != nil {
		return nil, err
	}
	origin := &teamUsageScopeOrigin{
		RelayUserIDs: relayUserIDs, StatsByRelayUserID: statsByRelayUserID,
		PointsByUser: make(map[int64][]relay.UsageTrendPoint, len(relayUserIDs)), subjects: subjects,
	}
	if len(relayUserIDs) == 0 {
		return origin, nil
	}

	trendCtx := ctx
	var cancel context.CancelFunc
	if s.teamOverviewTrendTimeout > 0 {
		trendCtx, cancel = context.WithTimeout(ctx, s.teamOverviewTrendTimeout)
	}
	if cancel != nil {
		defer cancel()
	}
	loadedPoints, err := trendProvider.GetUsageTrendForUsers(trendCtx, relayUserIDs, relay.TeamMemberTrendParams{
		StartDate: strings.TrimSpace(params.StartDate), EndDate: strings.TrimSpace(params.EndDate),
		Granularity: strings.TrimSpace(params.Granularity), Timezone: strings.TrimSpace(params.Timezone),
	})
	if err != nil {
		if isHardOverviewTrendError(err) {
			return nil, fmt.Errorf("get usage trend for team scope origin: %w", err)
		}
		origin.sourceErr = err
		for _, relayUserID := range relayUserIDs {
			origin.PointsByUser[relayUserID] = []relay.UsageTrendPoint{}
		}
		return origin, nil
	}

	authorized := make(map[int64]struct{}, len(relayUserIDs))
	for _, relayUserID := range relayUserIDs {
		authorized[relayUserID] = struct{}{}
		origin.PointsByUser[relayUserID] = []relay.UsageTrendPoint{}
	}
	for relayUserID, points := range loadedPoints {
		if _, ok := authorized[relayUserID]; !ok {
			continue
		}
		cloned := append([]relay.UsageTrendPoint{}, points...)
		sort.Slice(cloned, func(left, right int) bool { return cloned[left].Date < cloned[right].Date })
		origin.PointsByUser[relayUserID] = cloned
	}
	completeScopeOriginRanges(origin)
	return origin, nil
}

func (s *Service) hydrateCachedScopeOrigin(
	ctx context.Context,
	scope *representativescope.Scope,
	provider relay.Provider,
	params OverviewParams,
	origin *teamUsageScopeOrigin,
) (*teamUsageScopeOrigin, error) {
	subjects, currentRelayUserIDs, err := s.resolveOverviewSubjects(ctx, scope, provider)
	if err != nil {
		return nil, fmt.Errorf("hydrate team usage cached scope origin subjects: %w", err)
	}
	currentRelayUserIDs = sortedUniqueInt64s(currentRelayUserIDs)
	if !equalSortedInt64s(currentRelayUserIDs, origin.RelayUserIDs) {
		return s.loadTeamUsageScopeOrigin(ctx, scope, provider, params)
	}
	authorized := make(map[int64]struct{}, len(currentRelayUserIDs))
	for _, relayUserID := range currentRelayUserIDs {
		authorized[relayUserID] = struct{}{}
	}
	hydrated := &teamUsageScopeOrigin{
		RelayUserIDs:       currentRelayUserIDs,
		StatsByRelayUserID: make(map[int64]relay.TeamUserUsageStats, len(currentRelayUserIDs)),
		PointsByUser:       make(map[int64][]relay.UsageTrendPoint, len(currentRelayUserIDs)),
		subjects:           subjects,
	}
	for relayUserID, stat := range origin.StatsByRelayUserID {
		if _, ok := authorized[relayUserID]; ok {
			hydrated.StatsByRelayUserID[relayUserID] = stat
		}
	}
	for relayUserID, points := range origin.PointsByUser {
		if _, ok := authorized[relayUserID]; ok {
			hydrated.PointsByUser[relayUserID] = points
		}
	}
	return hydrated, nil
}

func equalSortedInt64s(left, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func completeScopeOriginRanges(origin *teamUsageScopeOrigin) {
	for _, relayUserID := range origin.RelayUserIDs {
		stat, found := origin.StatsByRelayUserID[relayUserID]
		if !found || (stat.RangeActualCost != nil && stat.RangeTotalTokens != nil) {
			continue
		}
		points, found := origin.PointsByUser[relayUserID]
		if !found {
			continue
		}
		actualCost, totalTokens, tokensComplete := summarizeTeamUsageRange(points)
		if stat.RangeActualCost == nil {
			stat.RangeActualCost = &actualCost
		}
		if stat.RangeTotalTokens == nil && tokensComplete {
			stat.RangeTotalTokens = &totalTokens
		}
		origin.StatsByRelayUserID[relayUserID] = stat
	}
}

func buildSummarySnapshotFromScopeOrigin(
	scope *representativescope.Scope,
	params OverviewParams,
	origin *teamUsageScopeOrigin,
) *SummarySnapshot {
	overviewSubjects := scope.OverviewSubjects
	if len(overviewSubjects) == 0 {
		overviewSubjects = scope.Subjects
	}
	rangeCostValue := 0.0
	rangeTokensValue := int64(0)
	rangeComplete := true
	for _, relayUserID := range origin.RelayUserIDs {
		stat, found := origin.StatsByRelayUserID[relayUserID]
		if !found || stat.RangeActualCost == nil || stat.RangeTotalTokens == nil {
			rangeComplete = false
			break
		}
		rangeCostValue += *stat.RangeActualCost
		rangeTokensValue += *stat.RangeTotalTokens
	}
	var rangeCost *float64
	var rangeTokens *int64
	var unavailableReason *string
	if rangeComplete {
		rangeCost = &rangeCostValue
		rangeTokens = &rangeTokensValue
	} else {
		reason := "range_aggregation_unavailable"
		unavailableReason = &reason
	}
	todayCost, totalCost := sumOverviewComparisonCosts(origin.StatsByRelayUserID)
	relayMemberCount := 0
	for _, subject := range origin.subjects {
		if subject.RelayUserID != nil && *subject.RelayUserID > 0 {
			relayMemberCount++
		}
	}
	return &SummarySnapshot{
		Window: buildOverviewWindow(params),
		Summary: SummaryAggregate{
			Unavailable: !rangeComplete, UnavailableReason: unavailableReason,
			MemberCount: len(overviewSubjects), RelayMemberCount: relayMemberCount,
			RangeActualCost: rangeCost, RangeTotalTokens: rangeTokens,
			TodayActualCost: todayCost, TotalActualCost: totalCost, UnitLabel: teamOverviewCostUnitLabel,
		},
	}
}

func buildMembersSnapshotFromScopeOrigin(params OverviewParams, origin *teamUsageScopeOrigin) *MembersSnapshot {
	windowTotals := make(map[int64]overviewWindowTotal, len(origin.StatsByRelayUserID))
	for relayUserID, stat := range origin.StatsByRelayUserID {
		total := overviewWindowTotal{TotalTokens: stat.RangeTotalTokens}
		if stat.RangeActualCost != nil {
			total.ActualCost = *stat.RangeActualCost
		}
		windowTotals[relayUserID] = total
	}
	members := BuildOverviewMemberDetails(origin.subjects, origin.StatsByRelayUserID, windowTotals)
	return &MembersSnapshot{Window: buildOverviewWindow(params), Members: rankMembersForPagination(members)}
}

func buildTrendSnapshotFromScopeOrigin(
	scope *representativescope.Scope,
	params OverviewParams,
	origin *teamUsageScopeOrigin,
) (*TrendSnapshot, error) {
	data := &teamTrendOriginData{
		subjects: origin.subjects, relayUserIDs: origin.RelayUserIDs,
		statsByRelayUserID: origin.StatsByRelayUserID, pointsByUser: origin.PointsByUser,
		sourceErr: origin.sourceErr,
	}
	if origin.sourceErr != nil {
		reason := "provider_error"
		data.unavailableReason = &reason
	}
	return buildTrendSnapshot(scope, params, data), origin.sourceErr
}

func buildOrganizationSnapshotFromScopeOrigin(
	branch organizationBranchSelection,
	params OverviewParams,
	origin *teamUsageScopeOrigin,
) *OrganizationSnapshot {
	branchIdentities := make(map[string]struct{}, len(branch.subjects))
	for _, subject := range branch.subjects {
		if identity := teamUsageSubjectIdentity(subject); identity != "" {
			branchIdentities[identity] = struct{}{}
		}
	}
	resolvedSubjects := make([]representativescope.Subject, 0, len(branch.subjects))
	for _, subject := range origin.subjects {
		if _, ok := branchIdentities[teamUsageSubjectIdentity(subject)]; ok {
			resolvedSubjects = append(resolvedSubjects, subject)
		}
	}
	statsByRelayUserID := make(map[int64]relay.TeamUserUsageStats, len(resolvedSubjects))
	for _, subject := range resolvedSubjects {
		if subject.RelayUserID == nil || *subject.RelayUserID <= 0 {
			continue
		}
		relayUserID := int64(*subject.RelayUserID)
		if stat, ok := origin.StatsByRelayUserID[relayUserID]; ok {
			statsByRelayUserID[relayUserID] = stat
		}
	}
	return buildOrganizationSnapshot(branch, params, resolvedSubjects, statsByRelayUserID)
}

func teamUsageSubjectIdentity(subject representativescope.Subject) string {
	if subject.UserID > 0 {
		return fmt.Sprintf("user:%d", subject.UserID)
	}
	if externalID := strings.TrimSpace(subject.DirectoryMemberExternalID); externalID != "" {
		return "directory:" + externalID
	}
	if email := strings.ToLower(strings.TrimSpace(subject.Email)); email != "" {
		return "email:" + email
	}
	return ""
}

func sortedUniqueInt64s(values []int64) []int64 {
	unique := uniqueInt64s(values)
	sort.Slice(unique, func(left, right int) bool { return unique[left] < unique[right] })
	return unique
}
