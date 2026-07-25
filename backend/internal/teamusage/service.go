package teamusage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/ent/teamusageratemultiplieraudit"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
)

const (
	systemDefaultMultiplier              = 1.0
	defaultTeamOverviewTrendTimeout      = 20 * time.Second
	multiplierMetadataUnavailableMessage = "Multiplier metadata is temporarily unavailable."
)

type ProviderResolver interface {
	Resolve(ctx context.Context, providerID int) (relay.Provider, error)
}

type ScopeResolver interface {
	Resolve(ctx context.Context, actorUserID int) (*representativescope.Scope, error)
}

type Service struct {
	client                   *ent.Client
	scopeResolver            ScopeResolver
	providerResolver         ProviderResolver
	locker                   AdvisoryLocker
	fullScopeCap             int
	teamOverviewTrendTimeout time.Duration
	maxMultiplier            float64
	snapshotCache            *SnapshotCache
	originCache              *OriginCache
	prewarmReader            *PrewarmReader
	memberCursorCodec        *memberCursorCodec
	organizationCursorCodec  *organizationCursorCodec
}

type ServiceOptions struct {
	SnapshotCache *SnapshotCache
	OriginCache   *OriginCache
	PrewarmReader *PrewarmReader
	CursorSecret  string
}

type splitReadRequest struct {
	actorUserID    int
	params         OverviewParams
	scope          *representativescope.Scope
	providerConfig primaryProviderConfig
	scopeHash      string
	bypassPrewarm  bool
}

func (s *Service) newSplitReadRequest(ctx context.Context, actorUserID int, params OverviewParams) (*splitReadRequest, error) {
	normalized, err := normalizeOverviewParams(params)
	if err != nil {
		return nil, err
	}
	scope, err := s.requireRepresentativeScope(ctx, actorUserID)
	if err != nil {
		return nil, err
	}
	providerConfig, err := s.resolvePrimaryProviderConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve primary relay provider configuration: %w", err)
	}
	request := &splitReadRequest{
		actorUserID: actorUserID, params: normalized, scope: scope, providerConfig: *providerConfig,
	}
	if s.snapshotCache != nil || s.originCache != nil {
		request.scopeHash, err = effectiveScopeHash(scope)
		if err != nil {
			return nil, err
		}
	}
	return request, nil
}

func (r *splitReadRequest) snapshotCacheKey() SnapshotCacheKey {
	return SnapshotCacheKey{
		ProviderID: r.providerConfig.ID, ProviderVersion: r.providerConfig.ConfigurationVersion,
		ActorID: r.actorUserID, ScopeVersion: r.scope.Version, ScopeHash: r.scopeHash, Params: r.params,
	}
}

func NewService(client *ent.Client, scopeResolver ScopeResolver, providerResolver ProviderResolver, locker AdvisoryLocker, options ServiceOptions) (*Service, error) {
	if client == nil {
		return nil, fmt.Errorf("team usage Ent client is required")
	}
	if scopeResolver == nil {
		return nil, fmt.Errorf("team usage scope resolver is required")
	}
	if providerResolver == nil {
		return nil, fmt.Errorf("team usage provider resolver is required")
	}
	if options.SnapshotCache == nil {
		return nil, fmt.Errorf("team usage snapshot cache is required")
	}
	if options.OriginCache == nil {
		return nil, fmt.Errorf("team usage origin cache is required")
	}
	cursorSecret := strings.TrimSpace(options.CursorSecret)
	if cursorSecret == "" {
		return nil, fmt.Errorf("team usage cursor secret is required")
	}
	service := newService(client, scopeResolver, providerResolver, locker, options.SnapshotCache, options.OriginCache, cursorSecret)
	service.prewarmReader = options.PrewarmReader
	return service, nil
}

func newService(client *ent.Client, scopeResolver ScopeResolver, providerResolver ProviderResolver, locker AdvisoryLocker, snapshotCache *SnapshotCache, originCache *OriginCache, cursorSecret string) *Service {
	if locker == nil {
		locker = &PostgresAdvisoryLocker{}
	}
	service := &Service{
		client:                   client,
		scopeResolver:            scopeResolver,
		providerResolver:         providerResolver,
		locker:                   locker,
		fullScopeCap:             500,
		teamOverviewTrendTimeout: defaultTeamOverviewTrendTimeout,
		maxMultiplier:            defaultMaxMultiplier,
		snapshotCache:            snapshotCache,
		originCache:              originCache,
	}
	if cursorSecret != "" {
		service.memberCursorCodec = newMemberCursorCodec(cursorSecret)
		service.organizationCursorCodec = newOrganizationCursorCodec(cursorSecret)
	}
	return service
}

func (s *Service) Scope(ctx context.Context, actorUserID int) (*ScopeResponse, error) {
	scope, err := s.resolveScope(ctx, actorUserID)
	if err != nil {
		return nil, err
	}
	return &ScopeResponse{
		IsRepresentative: scope.IsRepresentative,
		Departments:      append([]representativescope.DepartmentScope(nil), scope.Departments...),
	}, nil
}

func (s *Service) Subjects(ctx context.Context, actorUserID int, q string, page, pageSize int) (*SubjectsResponse, error) {
	scope, err := s.requireRepresentativeScope(ctx, actorUserID)
	if err != nil {
		return nil, err
	}
	page, pageSize = normalizePage(page, pageSize)
	filtered := filterSubjects(scope.Subjects, q)
	start := (page - 1) * pageSize
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	return &SubjectsResponse{
		Subjects: append([]representativescope.Subject(nil), filtered[start:end]...),
		Page:     page,
		PageSize: pageSize,
		Total:    len(filtered),
	}, nil
}

func (s *Service) SubjectDashboard(ctx context.Context, actorUserID, targetUserID int, params relay.UserUsageDashboardParams) (*SubjectDashboardResponse, error) {
	scope, err := s.requireRepresentativeScope(ctx, actorUserID)
	if err != nil {
		return nil, err
	}
	subject, ok := findScopedSubject(scope.Subjects, targetUserID)
	if !ok {
		return nil, &ForbiddenError{Reason: ErrOutOfScope.Error()}
	}
	if subject.RelayUserID == nil {
		return nil, &ForbiddenError{Reason: ErrNoRelayMapping.Error()}
	}
	canManageTarget, manageReason := scope.CanManageTarget(targetUserID)

	_, provider, err := s.resolvePrimaryProvider(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve primary relay provider: %w", err)
	}
	relayUserID, resolvedSubject, err := s.resolveSubjectRelayUserID(ctx, provider, *subject)
	if err != nil {
		if errors.Is(err, ErrNoRelayMapping) {
			return nil, &ForbiddenError{Reason: ErrNoRelayMapping.Error()}
		}
		return nil, err
	}
	dashboardProvider, ok := provider.(relay.SubjectUsageDashboardProvider)
	if !ok {
		return nil, ErrProviderUnsupported
	}
	snapshot, err := dashboardProvider.GetUsageDashboardForUser(ctx, relayUserID, params)
	if err != nil {
		return nil, fmt.Errorf("get subject usage dashboard: %w", err)
	}
	if snapshot == nil {
		return nil, errors.New("get subject usage dashboard: empty response")
	}

	rows, quotaState, err := s.buildSubjectSubscriptionRows(ctx, provider, int(relayUserID), params, canManageTarget, manageReason)
	if err != nil {
		return nil, err
	}
	return &SubjectDashboardResponse{
		Subject:                   resolvedSubject,
		Configured:                snapshot.Configured,
		Range:                     snapshot.Range,
		Stats:                     snapshot.Stats,
		Trend:                     snapshot.Trend,
		Models:                    snapshot.Models,
		GroupQuotas:               quotaState,
		SubjectSubscriptionGroups: rows,
	}, nil
}

func (s *Service) Summary(ctx context.Context, actorUserID int, params OverviewParams) (*SummaryResponse, error) {
	result, scopeVersion, err := s.readSummarySnapshot(ctx, actorUserID, params)
	if err != nil {
		return nil, err
	}
	return &SummaryResponse{
		SnapshotFreshness: result.Freshness,
		ScopeVersion:      scopeVersion,
		Window:            result.Snapshot.Window,
		Summary:           result.Snapshot.Summary,
	}, nil
}

func (s *Service) Trend(ctx context.Context, actorUserID int, params OverviewParams) (*TrendResponse, error) {
	result, scopeVersion, err := s.readTrendSnapshot(ctx, actorUserID, params)
	if err != nil {
		return nil, err
	}
	return &TrendResponse{
		SnapshotFreshness: result.Freshness,
		ScopeVersion:      scopeVersion,
		Window:            result.Snapshot.Window,
		TopMembers:        append([]OverviewMember(nil), result.Snapshot.TopMembers...),
		TopMemberTrend:    result.Snapshot.TopMemberTrend,
		DepartmentTrend:   result.Snapshot.DepartmentTrend,
	}, nil
}

func (s *Service) readTrendSnapshot(ctx context.Context, actorUserID int, params OverviewParams) (*TrendCacheResult, string, error) {
	request, err := s.newSplitReadRequest(ctx, actorUserID, params)
	if err != nil {
		return nil, "", err
	}
	result, err := s.readTrendSnapshotForRequest(ctx, request)
	if errors.Is(err, errPrewarmAuthorizationChanged) {
		request, err = s.newSplitReadRequest(ctx, actorUserID, params)
		if err != nil {
			return nil, "", err
		}
		request.bypassPrewarm = true
		result, err = s.readTrendSnapshotForRequest(ctx, request)
	}
	return result, request.scope.Version, err
}

func (s *Service) readTrendSnapshotForRequest(ctx context.Context, request *splitReadRequest) (*TrendCacheResult, error) {
	loader := func(loadCtx context.Context) (TrendOriginLoadResult, error) {
		overviewSubjects := request.scope.OverviewSubjects
		if len(overviewSubjects) == 0 {
			overviewSubjects = request.scope.Subjects
		}
		origin, handled, loadErr := s.loadPrewarmFirstScopeOrigin(loadCtx, request)
		if handled {
			if loadErr != nil {
				if isHardTrendSnapshotOriginError(loadCtx, loadErr) {
					return TrendOriginLoadResult{}, loadErr
				}
				return TrendOriginLoadResult{Snapshot: trendUnavailableSnapshot(request.params, "provider_error"), SnapshotErr: loadErr}, nil
			}
			snapshot, sourceErr := buildTrendSnapshotFromScopeOrigin(request.scope, request.params, origin)
			return TrendOriginLoadResult{Snapshot: snapshot, SnapshotErr: sourceErr}, nil
		}
		var provider relay.Provider
		if len(overviewSubjects) <= s.fullScopeCap {
			resolvedProvider, resolveErr := s.providerResolver.Resolve(loadCtx, request.providerConfig.ID)
			if resolveErr != nil {
				return TrendOriginLoadResult{}, fmt.Errorf("resolve primary relay provider origin: %w", resolveErr)
			}
			provider = resolvedProvider
		}
		snapshot, loadErr := s.generateTrendSnapshot(loadCtx, request.scope, provider, request.params)
		if loadErr == nil {
			return TrendOriginLoadResult{Snapshot: snapshot}, nil
		}
		if isHardTrendSnapshotOriginError(loadCtx, loadErr) {
			return TrendOriginLoadResult{}, loadErr
		}
		return TrendOriginLoadResult{Snapshot: snapshot, SnapshotErr: loadErr}, nil
	}

	if s.snapshotCache == nil {
		loaded, loadErr := loader(ctx)
		if loadErr != nil {
			return nil, loadErr
		}
		if loaded.SnapshotErr != nil && !validTrendSnapshot(loaded.Snapshot) {
			return nil, loaded.SnapshotErr
		}
		now := time.Now().UTC()
		return &TrendCacheResult{
			Snapshot: loaded.Snapshot,
			Freshness: SnapshotFreshness{
				AsOf: now, FreshUntil: now, StaleUntil: now, CacheStatus: "miss", SourceStatus: "ok",
			},
		}, nil
	}

	result, err := s.snapshotCache.GetTrendOrLoad(ctx, request.snapshotCacheKey(), loader)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) Overview(ctx context.Context, actorUserID int, params OverviewParams) (*OverviewResponse, error) {
	request, err := s.newSplitReadRequest(ctx, actorUserID, params)
	if err != nil {
		return nil, err
	}
	summary, trend, members, err := s.readOverviewSnapshotsForRequest(ctx, request)
	if errors.Is(err, errPrewarmAuthorizationChanged) {
		request, err = s.newSplitReadRequest(ctx, actorUserID, params)
		if err != nil {
			return nil, err
		}
		request.bypassPrewarm = true
		summary, trend, members, err = s.readOverviewSnapshotsForRequest(ctx, request)
	}
	if err != nil {
		return nil, err
	}

	overviewSubjects := request.scope.OverviewSubjects
	if len(overviewSubjects) == 0 {
		overviewSubjects = request.scope.Subjects
	}
	memberTree := []OverviewMemberNode{}
	if len(overviewSubjects) <= s.fullScopeCap || len(members.Snapshot.Members) > 0 {
		memberTree = projectOverviewCompatibilityMemberTree(request.scope, members.Snapshot.Members)
	}
	return &OverviewResponse{
		Configured:       true,
		IsRepresentative: true,
		Window:           summary.Snapshot.Window,
		Summary:          overviewSummaryFromAggregate(summary.Snapshot.Summary),
		TopMembers:       append([]OverviewMember{}, trend.Snapshot.TopMembers...),
		TopMemberTrend:   trend.Snapshot.TopMemberTrend,
		DepartmentTrend:  trend.Snapshot.DepartmentTrend,
		Members:          append([]OverviewMember{}, members.Snapshot.Members...),
		MemberTree:       memberTree,
	}, nil
}

func (s *Service) readOverviewSnapshotsForRequest(
	ctx context.Context,
	request *splitReadRequest,
) (*SummaryCacheResult, *TrendCacheResult, *MembersCacheResult, error) {
	summary, err := s.readSummarySnapshotForRequest(ctx, request)
	if err != nil {
		return nil, nil, nil, err
	}
	trend, err := s.readTrendSnapshotForRequest(ctx, request)
	if err != nil {
		return nil, nil, nil, err
	}
	members, err := s.readMembersSnapshotForRequest(ctx, request)
	if err != nil {
		return nil, nil, nil, err
	}
	return summary, trend, members, nil
}

func overviewSummaryFromAggregate(summary SummaryAggregate) OverviewSummary {
	return OverviewSummary{
		Unavailable: summary.Unavailable, UnavailableReason: summary.UnavailableReason,
		MemberCount: summary.MemberCount, RelayMemberCount: summary.RelayMemberCount,
		RangeActualCost: summary.RangeActualCost, RangeTotalTokens: summary.RangeTotalTokens,
		TodayActualCost: summary.TodayActualCost, TotalActualCost: summary.TotalActualCost,
		UnitLabel: summary.UnitLabel,
	}
}

func (s *Service) readSummarySnapshot(ctx context.Context, actorUserID int, params OverviewParams) (*SummaryCacheResult, string, error) {
	request, err := s.newSplitReadRequest(ctx, actorUserID, params)
	if err != nil {
		return nil, "", err
	}
	result, err := s.readSummarySnapshotForRequest(ctx, request)
	if errors.Is(err, errPrewarmAuthorizationChanged) {
		request, err = s.newSplitReadRequest(ctx, actorUserID, params)
		if err != nil {
			return nil, "", err
		}
		request.bypassPrewarm = true
		result, err = s.readSummarySnapshotForRequest(ctx, request)
	}
	return result, request.scope.Version, err
}

func (s *Service) readSummarySnapshotForRequest(ctx context.Context, request *splitReadRequest) (*SummaryCacheResult, error) {
	loader := func(loadCtx context.Context) (SummaryOriginLoadResult, error) {
		overviewSubjects := request.scope.OverviewSubjects
		if len(overviewSubjects) == 0 {
			overviewSubjects = request.scope.Subjects
		}
		origin, handled, loadErr := s.loadPrewarmFirstScopeOrigin(loadCtx, request)
		if handled {
			if loadErr == nil {
				return SummaryOriginLoadResult{Snapshot: buildSummarySnapshotFromScopeOrigin(request.scope, request.params, origin)}, nil
			}
			if isHardSnapshotOriginError(loadErr) {
				return SummaryOriginLoadResult{}, loadErr
			}
			return SummaryOriginLoadResult{SnapshotErr: loadErr}, nil
		}
		var provider relay.Provider
		if len(overviewSubjects) <= s.fullScopeCap {
			resolvedProvider, resolveErr := s.providerResolver.Resolve(loadCtx, request.providerConfig.ID)
			if resolveErr != nil {
				return SummaryOriginLoadResult{}, fmt.Errorf("resolve primary relay provider origin: %w", resolveErr)
			}
			provider = resolvedProvider
		}
		snapshot, loadErr := s.generateSummarySnapshot(loadCtx, request.scope, provider, request.params)
		if loadErr == nil {
			return SummaryOriginLoadResult{Snapshot: snapshot}, nil
		}
		if isHardSnapshotOriginError(loadErr) {
			return SummaryOriginLoadResult{}, loadErr
		}
		return SummaryOriginLoadResult{SnapshotErr: loadErr}, nil
	}

	if s.snapshotCache == nil {
		loaded, loadErr := loader(ctx)
		if loadErr != nil {
			return nil, loadErr
		}
		if loaded.SnapshotErr != nil {
			return nil, loaded.SnapshotErr
		}
		now := time.Now().UTC()
		return &SummaryCacheResult{
			Snapshot: loaded.Snapshot,
			Freshness: SnapshotFreshness{
				AsOf: now, FreshUntil: now, StaleUntil: now, CacheStatus: "miss", SourceStatus: "ok",
			},
		}, nil
	}

	result, err := s.snapshotCache.GetSummaryOrLoad(ctx, request.snapshotCacheKey(), loader)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) readMembersSnapshot(ctx context.Context, actorUserID int, params OverviewParams) (*MembersCacheResult, string, error) {
	request, err := s.newSplitReadRequest(ctx, actorUserID, params)
	if err != nil {
		return nil, "", err
	}
	result, err := s.readMembersSnapshotForRequest(ctx, request)
	if errors.Is(err, errPrewarmAuthorizationChanged) {
		request, err = s.newSplitReadRequest(ctx, actorUserID, params)
		if err != nil {
			return nil, "", err
		}
		request.bypassPrewarm = true
		result, err = s.readMembersSnapshotForRequest(ctx, request)
	}
	return result, request.scope.Version, err
}

func (s *Service) readMembersSnapshotForRequest(ctx context.Context, request *splitReadRequest) (*MembersCacheResult, error) {
	loader := func(loadCtx context.Context) (MembersOriginLoadResult, error) {
		overviewSubjects := request.scope.OverviewSubjects
		if len(overviewSubjects) == 0 {
			overviewSubjects = request.scope.Subjects
		}
		origin, handled, loadErr := s.loadPrewarmFirstScopeOrigin(loadCtx, request)
		if handled {
			if loadErr == nil {
				return MembersOriginLoadResult{Snapshot: buildMembersSnapshotFromScopeOrigin(request.params, origin)}, nil
			}
			if isHardSnapshotOriginError(loadErr) {
				return MembersOriginLoadResult{}, loadErr
			}
			return MembersOriginLoadResult{SnapshotErr: loadErr}, nil
		}
		var provider relay.Provider
		if len(overviewSubjects) <= s.fullScopeCap {
			resolvedProvider, resolveErr := s.providerResolver.Resolve(loadCtx, request.providerConfig.ID)
			if resolveErr != nil {
				return MembersOriginLoadResult{}, fmt.Errorf("resolve primary relay provider origin: %w", resolveErr)
			}
			provider = resolvedProvider
		}
		snapshot, loadErr := s.generateMembersSnapshot(loadCtx, request.scope, provider, request.params)
		if loadErr == nil {
			return MembersOriginLoadResult{Snapshot: snapshot}, nil
		}
		if isHardSnapshotOriginError(loadErr) {
			return MembersOriginLoadResult{}, loadErr
		}
		return MembersOriginLoadResult{SnapshotErr: loadErr}, nil
	}

	if s.snapshotCache == nil {
		loaded, loadErr := loader(ctx)
		if loadErr != nil {
			return nil, fmt.Errorf("load team members snapshot origin: %w", loadErr)
		}
		if loaded.SnapshotErr != nil {
			return nil, fmt.Errorf("load team members snapshot origin: %w", loaded.SnapshotErr)
		}
		now := time.Now().UTC()
		return &MembersCacheResult{
			Snapshot: loaded.Snapshot,
			Freshness: SnapshotFreshness{
				AsOf: now, FreshUntil: now, StaleUntil: now, CacheStatus: "miss", SourceStatus: "ok",
			},
		}, nil
	}

	result, err := s.snapshotCache.GetMembersOrLoad(ctx, request.snapshotCacheKey(), loader)
	if err != nil {
		return nil, fmt.Errorf("read team members snapshot: %w", err)
	}
	return result, nil
}

func (s *Service) generateMembersSnapshot(ctx context.Context, scope *representativescope.Scope, provider relay.Provider, params OverviewParams) (*MembersSnapshot, error) {
	overviewSubjects := scope.OverviewSubjects
	if len(overviewSubjects) == 0 {
		overviewSubjects = scope.Subjects
	}
	if len(overviewSubjects) > s.fullScopeCap {
		return &MembersSnapshot{Window: buildOverviewWindow(params), Members: []OverviewMember{}}, nil
	}
	summaryProvider, ok := provider.(relay.TeamUsageSummaryProvider)
	if !ok {
		return nil, fmt.Errorf("team members summary capability: %w", ErrProviderUnsupported)
	}
	subjects, relayUserIDs, err := s.resolveOverviewSubjects(ctx, scope, provider)
	if err != nil {
		return nil, fmt.Errorf("resolve team members Relay subjects: %w", err)
	}
	statsByRelayUserID, err := s.loadTeamUsageStats(ctx, summaryProvider, relayUserIDs, relayUserIDs, relay.TeamUsageSummaryParams{
		StartDate: strings.TrimSpace(params.StartDate), EndDate: strings.TrimSpace(params.EndDate),
		Granularity: strings.TrimSpace(params.Granularity), Timezone: strings.TrimSpace(params.Timezone),
		RequireCompleteRange: true,
	})
	if err != nil {
		return nil, fmt.Errorf("load team members usage stats: %w", err)
	}
	windowTotals := make(map[int64]overviewWindowTotal, len(statsByRelayUserID))
	for relayUserID, stat := range statsByRelayUserID {
		total := overviewWindowTotal{TotalTokens: stat.RangeTotalTokens}
		if stat.RangeActualCost != nil {
			total.ActualCost = *stat.RangeActualCost
		}
		windowTotals[relayUserID] = total
	}
	members := BuildOverviewMemberDetails(subjects, statsByRelayUserID, windowTotals)
	return &MembersSnapshot{
		Window:  buildOverviewWindow(params),
		Members: rankMembersForPagination(members),
	}, nil
}

func (s *Service) generateSummarySnapshot(ctx context.Context, scope *representativescope.Scope, provider relay.Provider, params OverviewParams) (*SummarySnapshot, error) {
	overviewSubjects := scope.OverviewSubjects
	if len(overviewSubjects) == 0 {
		overviewSubjects = scope.Subjects
	}
	if len(overviewSubjects) > s.fullScopeCap {
		reason := "scope_too_large"
		return &SummarySnapshot{
			Window: buildOverviewWindow(params),
			Summary: SummaryAggregate{
				Unavailable: true, UnavailableReason: &reason,
				MemberCount: len(overviewSubjects), UnitLabel: teamOverviewCostUnitLabel,
			},
		}, nil
	}
	summaryProvider, ok := provider.(relay.TeamUsageSummaryProvider)
	if !ok {
		return nil, ErrProviderUnsupported
	}
	_, relayUserIDs, err := s.resolveOverviewSubjects(ctx, scope, provider)
	if err != nil {
		return nil, err
	}
	statsByRelayUserID, err := s.loadTeamUsageStats(ctx, summaryProvider, relayUserIDs, relayUserIDs, relay.TeamUsageSummaryParams{
		StartDate: strings.TrimSpace(params.StartDate), EndDate: strings.TrimSpace(params.EndDate),
		Granularity: strings.TrimSpace(params.Granularity), Timezone: strings.TrimSpace(params.Timezone),
		RequireCompleteRange: true,
	})
	if err != nil {
		return nil, err
	}

	rangeCostValue := 0.0
	rangeTokensValue := int64(0)
	rangeComplete := true
	for _, relayUserID := range uniqueInt64s(relayUserIDs) {
		stat, found := statsByRelayUserID[relayUserID]
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
	todayCost, totalCost := sumOverviewComparisonCosts(statsByRelayUserID)
	return &SummarySnapshot{
		Window: buildOverviewWindow(params),
		Summary: SummaryAggregate{
			Unavailable:       !rangeComplete,
			UnavailableReason: unavailableReason,
			MemberCount:       len(overviewSubjects),
			RelayMemberCount:  len(relayUserIDs),
			RangeActualCost:   rangeCost,
			RangeTotalTokens:  rangeTokens,
			TodayActualCost:   todayCost,
			TotalActualCost:   totalCost,
			UnitLabel:         teamOverviewCostUnitLabel,
		},
	}, nil
}

type teamTrendOriginData struct {
	subjects           []representativescope.Subject
	relayUserIDs       []int64
	statsByRelayUserID map[int64]relay.TeamUserUsageStats
	pointsByUser       map[int64][]relay.UsageTrendPoint
	unavailableReason  *string
	sourceErr          error
}

func (s *Service) generateTrendSnapshot(ctx context.Context, scope *representativescope.Scope, provider relay.Provider, params OverviewParams) (*TrendSnapshot, error) {
	overviewSubjects := scope.OverviewSubjects
	if len(overviewSubjects) == 0 {
		overviewSubjects = scope.Subjects
	}
	if len(overviewSubjects) > s.fullScopeCap {
		return trendUnavailableForLargeScope(params), nil
	}
	data, err := s.loadTeamTrendOrigin(ctx, scope, provider, params)
	if err != nil {
		if isHardTrendSnapshotOriginError(ctx, err) {
			return nil, err
		}
		return trendUnavailableSnapshot(params, "provider_error"), err
	}
	return buildTrendSnapshot(scope, params, data), data.sourceErr
}

func (s *Service) loadTeamTrendOrigin(ctx context.Context, scope *representativescope.Scope, provider relay.Provider, params OverviewParams) (*teamTrendOriginData, error) {
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
	statsByRelayUserID, err := s.loadTeamUsageStats(ctx, summaryProvider, relayUserIDs, nil, relay.TeamUsageSummaryParams{
		StartDate: strings.TrimSpace(params.StartDate), EndDate: strings.TrimSpace(params.EndDate),
		Granularity: strings.TrimSpace(params.Granularity), Timezone: strings.TrimSpace(params.Timezone),
	})
	if err != nil {
		return nil, err
	}

	data := &teamTrendOriginData{
		subjects: subjects, relayUserIDs: relayUserIDs, statsByRelayUserID: statsByRelayUserID,
		pointsByUser: map[int64][]relay.UsageTrendPoint{},
	}
	if len(relayUserIDs) == 0 {
		return data, nil
	}
	trendCtx := ctx
	var cancel context.CancelFunc
	if s.teamOverviewTrendTimeout > 0 {
		trendCtx, cancel = context.WithTimeout(ctx, s.teamOverviewTrendTimeout)
	}
	if cancel != nil {
		defer cancel()
	}
	data.pointsByUser, err = trendProvider.GetUsageTrendForUsers(trendCtx, relayUserIDs, relay.TeamMemberTrendParams{
		StartDate:   strings.TrimSpace(params.StartDate),
		EndDate:     strings.TrimSpace(params.EndDate),
		Granularity: strings.TrimSpace(params.Granularity),
		Timezone:    strings.TrimSpace(params.Timezone),
	})
	if err == nil {
		return data, nil
	}
	if isHardOverviewTrendError(err) {
		return nil, fmt.Errorf("get usage trend for top members: %w", err)
	}
	reason := "provider_error"
	data.unavailableReason = &reason
	data.sourceErr = err
	data.pointsByUser = map[int64][]relay.UsageTrendPoint{}
	return data, nil
}

func buildTrendSnapshot(scope *representativescope.Scope, params OverviewParams, data *teamTrendOriginData) *TrendSnapshot {
	trendState := TopMemberTrendState{
		UnitLabel: teamOverviewCostUnitLabel,
		RankBasis: topMemberRankBasisTokens,
		Series:    []TopMemberTrendSeries{},
	}
	departmentTrendState := DepartmentTrendState{
		UnitLabel: teamOverviewCostUnitLabel,
		Series:    []DepartmentTrendSeries{},
	}
	windowTotals := buildOverviewWindowTotals(data.pointsByUser)
	topMembers := []OverviewMember{}
	if data.unavailableReason == nil {
		topMembers = RankTopMembers(data.subjects, data.statsByRelayUserID, windowTotals, 12)
		departmentTrendState = BuildOverviewDepartmentTrend(
			scope.MemberTreeDepartments,
			scope.MemberTreeRootIDs,
			data.subjects,
			data.pointsByUser,
		)
	} else {
		trendState.Unavailable = true
		trendState.UnavailableReason = data.unavailableReason
		departmentTrendState.Unavailable = true
		departmentTrendState.UnavailableReason = data.unavailableReason
	}
	for _, member := range topMembers {
		series := TopMemberTrendSeries{
			UserID:                    member.UserID,
			DirectoryMemberExternalID: member.DirectoryMemberExternalID,
			DisplayName:               member.DisplayName,
			Rank:                      member.Rank,
			Points:                    []relay.UsageTrendPoint{},
		}
		if member.RelayUserID == nil {
			reason := "provider_error"
			series.Unavailable = true
			series.UnavailableReason = &reason
		} else {
			series.Points = append(series.Points, data.pointsByUser[int64(*member.RelayUserID)]...)
		}
		trendState.Series = append(trendState.Series, series)
	}
	return &TrendSnapshot{
		Window:          buildOverviewWindow(params),
		TopMembers:      topMembers,
		TopMemberTrend:  trendState,
		DepartmentTrend: departmentTrendState,
	}
}

func trendUnavailableForLargeScope(params OverviewParams) *TrendSnapshot {
	return trendUnavailableSnapshot(params, "scope_too_large")
}

func trendUnavailableSnapshot(params OverviewParams, unavailableReason string) *TrendSnapshot {
	reason := unavailableReason
	return &TrendSnapshot{
		Window:     buildOverviewWindow(params),
		TopMembers: []OverviewMember{},
		TopMemberTrend: TopMemberTrendState{
			UnitLabel: teamOverviewCostUnitLabel, RankBasis: topMemberRankBasisTokens,
			Unavailable: true, UnavailableReason: &reason, Series: []TopMemberTrendSeries{},
		},
		DepartmentTrend: DepartmentTrendState{
			UnitLabel:   teamOverviewCostUnitLabel,
			Unavailable: true, UnavailableReason: &reason, Series: []DepartmentTrendSeries{},
		},
	}
}

func (s *Service) resolveOverviewSubjects(ctx context.Context, scope *representativescope.Scope, provider relay.Provider) ([]representativescope.Subject, []int64, error) {
	overviewSubjects := scope.OverviewSubjects
	if len(overviewSubjects) == 0 {
		overviewSubjects = scope.Subjects
	}
	return s.resolveSubjects(ctx, overviewSubjects, provider)
}

func (s *Service) resolveSubjects(ctx context.Context, source []representativescope.Subject, provider relay.Provider) ([]representativescope.Subject, []int64, error) {
	overviewRelayResolver, err := s.newOverviewRelayUserResolver(ctx, provider)
	if err != nil {
		return nil, nil, err
	}
	return s.resolveSubjectsWithResolver(ctx, source, overviewRelayResolver)
}

func (s *Service) resolveSubjectsWithResolver(ctx context.Context, source []representativescope.Subject, resolver *overviewRelayUserResolver) ([]representativescope.Subject, []int64, error) {
	subjects := make([]representativescope.Subject, 0, len(source))
	relayUserIDs := make([]int64, 0, len(source))
	for _, subject := range source {
		relayUserID, resolvedSubject, err := resolver.Resolve(ctx, subject)
		if err != nil {
			if errors.Is(err, ErrNoRelayMapping) {
				subjects = append(subjects, subject)
				continue
			}
			return nil, nil, err
		}
		subjects = append(subjects, resolvedSubject)
		relayUserIDs = append(relayUserIDs, relayUserID)
	}
	return subjects, relayUserIDs, nil
}

func (s *Service) loadTeamUsageStats(
	ctx context.Context,
	provider relay.TeamUsageSummaryProvider,
	relayUserIDs []int64,
	completeRangeUserIDs []int64,
	params relay.TeamUsageSummaryParams,
) (map[int64]relay.TeamUserUsageStats, error) {
	uniqueRelayUserIDs := uniqueInt64s(relayUserIDs)
	statsByRelayUserID := make(map[int64]relay.TeamUserUsageStats, len(uniqueRelayUserIDs))
	statsParams := params
	statsParams.RequireCompleteRange = false
	for _, chunk := range chunkInt64s(uniqueRelayUserIDs, 100) {
		stats, err := provider.GetBatchUserUsageStats(ctx, chunk, statsParams)
		if err != nil {
			return nil, fmt.Errorf("get batch team usage stats: %w", err)
		}
		requested := make(map[int64]struct{}, len(chunk))
		for _, relayUserID := range chunk {
			requested[relayUserID] = struct{}{}
		}
		for relayUserID, stat := range stats {
			if _, ok := requested[relayUserID]; ok {
				statsByRelayUserID[relayUserID] = stat
			}
		}
	}
	if !params.RequireCompleteRange {
		return statsByRelayUserID, nil
	}

	incompleteUserIDs := make([]int64, 0)
	for _, relayUserID := range uniqueRelayUserIDs {
		stat, found := statsByRelayUserID[relayUserID]
		if found && (stat.RangeActualCost == nil || stat.RangeTotalTokens == nil) {
			incompleteUserIDs = append(incompleteUserIDs, relayUserID)
		}
	}
	if len(incompleteUserIDs) == 0 {
		return statsByRelayUserID, nil
	}
	trendProvider, ok := provider.(relay.TeamMemberTrendProvider)
	if !ok {
		return statsByRelayUserID, nil
	}
	fullScopeUserIDs := uniqueInt64s(completeRangeUserIDs)
	if len(fullScopeUserIDs) == 0 {
		return statsByRelayUserID, nil
	}
	pointsByUser, err := trendProvider.GetUsageTrendForUsers(ctx, fullScopeUserIDs, relay.TeamMemberTrendParams{
		StartDate: strings.TrimSpace(params.StartDate), EndDate: strings.TrimSpace(params.EndDate),
		Granularity: strings.TrimSpace(params.Granularity), Timezone: strings.TrimSpace(params.Timezone),
	})
	if err != nil {
		return statsByRelayUserID, nil
	}
	for _, relayUserID := range incompleteUserIDs {
		points, found := pointsByUser[relayUserID]
		if !found {
			continue
		}
		actualCost, totalTokens, tokensComplete := summarizeTeamUsageRange(points)
		stat := statsByRelayUserID[relayUserID]
		if stat.RangeActualCost == nil {
			stat.RangeActualCost = &actualCost
		}
		if stat.RangeTotalTokens == nil && tokensComplete {
			stat.RangeTotalTokens = &totalTokens
		}
		statsByRelayUserID[relayUserID] = stat
	}
	return statsByRelayUserID, nil
}

func summarizeTeamUsageRange(points []relay.UsageTrendPoint) (float64, int64, bool) {
	var actualCost float64
	var totalTokens int64
	tokensComplete := true
	for _, point := range points {
		actualCost += point.ActualCost
		if point.TotalTokens == nil {
			tokensComplete = false
			continue
		}
		totalTokens += *point.TotalTokens
	}
	return actualCost, totalTokens, tokensComplete
}

type overviewRelayUserResolver struct {
	service      *Service
	provider     relay.Provider
	useDirectory bool
	usersByID    map[int64]relay.User
	usersByEmail map[string]relay.User
}

func (s *Service) newOverviewRelayUserResolver(ctx context.Context, provider relay.Provider) (*overviewRelayUserResolver, error) {
	resolver := &overviewRelayUserResolver{
		service:  s,
		provider: provider,
	}
	directoryProvider, ok := provider.(relay.UserDirectoryProvider)
	if !ok {
		return resolver, nil
	}
	users, err := directoryProvider.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list relay users for team overview: %w", err)
	}
	resolver.useDirectory = true
	resolver.usersByID = make(map[int64]relay.User, len(users))
	resolver.usersByEmail = make(map[string]relay.User, len(users))
	for _, user := range users {
		if user.ID > 0 {
			resolver.usersByID[user.ID] = user
		}
		if email := overviewEmailKey(user.Email); email != "" {
			resolver.usersByEmail[email] = user
		}
	}
	return resolver, nil
}

func (r *overviewRelayUserResolver) Resolve(ctx context.Context, subject representativescope.Subject) (int64, representativescope.Subject, error) {
	if r == nil {
		return 0, subject, ErrNoRelayMapping
	}
	if !r.useDirectory {
		return r.service.resolveOverviewSubjectRelayUserID(ctx, r.provider, subject)
	}
	return r.resolveFromDirectory(ctx, subject)
}

func (r *overviewRelayUserResolver) resolveFromDirectory(ctx context.Context, subject representativescope.Subject) (int64, representativescope.Subject, error) {
	email := strings.TrimSpace(subject.Email)
	if subject.RelayUserID != nil && *subject.RelayUserID > 0 {
		cachedRelayUserID := int64(*subject.RelayUserID)
		if email == "" {
			return cachedRelayUserID, subject, nil
		}
		if current, ok := r.usersByID[cachedRelayUserID]; ok && strings.EqualFold(strings.TrimSpace(current.Email), email) {
			return cachedRelayUserID, subject, nil
		}
		if relayUser, ok := r.usersByEmail[overviewEmailKey(email)]; ok && relayUser.ID > 0 {
			resolved, err := r.withResolvedRelayUserID(ctx, subject, relayUser.ID)
			return relayUser.ID, resolved, err
		}
		return cachedRelayUserID, subject, nil
	}

	if email == "" {
		return 0, subject, ErrNoRelayMapping
	}
	relayUser, ok := r.usersByEmail[overviewEmailKey(email)]
	if !ok || relayUser.ID <= 0 || !strings.EqualFold(strings.TrimSpace(relayUser.Email), email) {
		return 0, subject, ErrNoRelayMapping
	}
	resolved, err := r.withResolvedRelayUserID(ctx, subject, relayUser.ID)
	return relayUser.ID, resolved, err
}

func (r *overviewRelayUserResolver) withResolvedRelayUserID(ctx context.Context, subject representativescope.Subject, relayUserID int64) (representativescope.Subject, error) {
	resolved := subject
	resolvedID := int(relayUserID)
	resolved.RelayUserID = &resolvedID
	if subject.UserID > 0 && subject.RelayUserID != nil && *subject.RelayUserID != resolvedID && r.service != nil && r.service.client != nil {
		if err := r.service.client.User.UpdateOneID(subject.UserID).SetRelayUserID(resolvedID).Exec(ctx); err != nil {
			return subject, fmt.Errorf("persist relay user binding: %w", err)
		}
	}
	return resolved, nil
}

func overviewEmailKey(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (s *Service) UpdateMultiplier(ctx context.Context, actorUserID, targetUserID int, groupID int64, req UpdateMultiplierRequest) (*UpdateMultiplierResponse, error) {
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	auditAction := auditActionForMode(mode)
	audit, err := s.client.TeamUsageRateMultiplierAudit.Create().
		SetActorUserID(actorUserID).
		SetGroupID(strconv.FormatInt(groupID, 10)).
		SetAction(auditAction).
		SetStatus(teamusageratemultiplieraudit.StatusRunning).
		SetRequestMetadata(redactedRequestMetadata(req)).
		SetReason(trimReason(req.Reason)).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create team usage audit: %w", err)
	}

	scope, err := s.resolveScope(ctx, actorUserID)
	if err != nil {
		return nil, s.failAudit(ctx, audit.ID, fmt.Errorf("resolve representative scope: %w", err))
	}
	if scope == nil || !scope.IsRepresentative {
		baseErr := &ForbiddenError{Reason: ErrNotRepresentative.Error()}
		return nil, s.rejectAudit(ctx, audit.ID, teamusageratemultiplieraudit.RejectionReasonNotRepresentative, ErrNotRepresentative, nil, baseErr)
	}
	if err := s.setAuditScopeEvidence(ctx, audit.ID, scope, targetUserID); err != nil {
		return nil, s.failAudit(ctx, audit.ID, err)
	}

	ok, reason := scope.CanManageTarget(targetUserID)
	if !ok {
		rejection, sentinel := rejectionForReason(reason)
		baseErr := &ForbiddenError{Reason: sentinel.Error()}
		targetAuditUserID, err := s.captureRejectedTargetMetadata(ctx, audit.ID, targetUserID)
		if err != nil {
			return nil, s.failAudit(ctx, audit.ID, err)
		}
		return nil, s.rejectAudit(ctx, audit.ID, rejection, sentinel, targetAuditUserID, baseErr)
	}

	targetUser, err := s.client.User.Get(ctx, targetUserID)
	if err != nil {
		return nil, s.failAudit(ctx, audit.ID, fmt.Errorf("get target user: %w", err))
	}
	if targetUser.RelayUserID == nil {
		baseErr := &ForbiddenError{Reason: ErrNoRelayMapping.Error()}
		return nil, s.rejectAudit(ctx, audit.ID, teamusageratemultiplieraudit.RejectionReasonNoRelayMapping, ErrNoRelayMapping, targetUserIDPtr(targetUserID), baseErr)
	}

	providerID, provider, err := s.resolvePrimaryProvider(ctx)
	if err != nil {
		return nil, s.failAudit(ctx, audit.ID, fmt.Errorf("resolve primary relay provider: %w", err))
	}
	if _, ok := provider.(relay.SubjectUsageDashboardProvider); !ok {
		return nil, s.rejectAudit(ctx, audit.ID, teamusageratemultiplieraudit.RejectionReasonProviderUnsupported, ErrProviderUnsupported, targetUserIDPtr(targetUserID), ErrProviderUnsupported)
	}
	subscriptionLister, ok := provider.(relay.UserSubscriptionLister)
	if !ok {
		return nil, s.rejectAudit(ctx, audit.ID, teamusageratemultiplieraudit.RejectionReasonProviderUnsupported, ErrProviderUnsupported, targetUserIDPtr(targetUserID), ErrProviderUnsupported)
	}
	manager, ok := provider.(relay.GroupRateMultiplierManager)
	if !ok {
		return nil, s.rejectAudit(ctx, audit.ID, teamusageratemultiplieraudit.RejectionReasonProviderUnsupported, ErrProviderUnsupported, targetUserIDPtr(targetUserID), ErrProviderUnsupported)
	}

	relayUserID, err := s.resolveEntUserRelayUserID(ctx, provider, targetUser)
	if err != nil {
		if errors.Is(err, ErrNoRelayMapping) {
			baseErr := &ForbiddenError{Reason: ErrNoRelayMapping.Error()}
			return nil, s.rejectAudit(ctx, audit.ID, teamusageratemultiplieraudit.RejectionReasonNoRelayMapping, ErrNoRelayMapping, targetUserIDPtr(targetUserID), baseErr)
		}
		return nil, s.failAudit(ctx, audit.ID, fmt.Errorf("resolve relay user: %w", err))
	}

	subscriptions, err := subscriptionLister.ListUserSubscriptions(ctx, relayUserID)
	if err != nil {
		return nil, s.failAudit(ctx, audit.ID, fmt.Errorf("list user subscriptions: %w", err))
	}
	subscription := findActiveSubscription(subscriptions, groupID)
	if subscription == nil {
		baseErr := &ForbiddenError{Reason: ErrInactiveSubscription.Error()}
		return nil, s.rejectAudit(ctx, audit.ID, teamusageratemultiplieraudit.RejectionReasonInactiveSubscription, ErrInactiveSubscription, targetUserIDPtr(targetUserID), baseErr)
	}

	currentEntries, err := manager.ListGroupRateMultipliers(ctx, groupID)
	if err != nil {
		return nil, s.failAudit(ctx, audit.ID, fmt.Errorf("list group rate multipliers: %w", err))
	}
	currentEntry := findRateEntry(currentEntries, relayUserID)
	currentRow := buildSubscriptionRowFromSubscription(*subscription, currentEntry, false, nil)
	if err := s.setAuditCurrentState(ctx, audit.ID, targetUserID, providerID, relayUserID, currentRow); err != nil {
		return nil, s.failAudit(ctx, audit.ID, err)
	}

	var requested *float64
	switch mode {
	case "set":
		if req.RateMultiplier == nil {
			err := fmt.Errorf("rate_multiplier is required for mode set")
			return nil, s.rejectAudit(ctx, audit.ID, teamusageratemultiplieraudit.RejectionReasonPolicyDenied, err, targetUserIDPtr(targetUserID), err)
		}
		normalized, err := ValidateSetMultiplier(*req.RateMultiplier, currentRow.InheritedDefaultMultiplier, s.maxMultiplier)
		if err != nil {
			return nil, s.rejectAudit(ctx, audit.ID, teamusageratemultiplieraudit.RejectionReasonPolicyDenied, err, targetUserIDPtr(targetUserID), err)
		}
		requested = &normalized
	case "reset":
		requested = nil
	default:
		err := fmt.Errorf("unsupported mode %q", req.Mode)
		return nil, s.rejectAudit(ctx, audit.ID, teamusageratemultiplieraudit.RejectionReasonPolicyDenied, err, targetUserIDPtr(targetUserID), err)
	}

	if SameMultiplierState(currentRow.UserMultiplier, requested) {
		if err := s.markAuditOutcome(ctx, audit.ID, teamusageratemultiplieraudit.StatusSucceeded, false, currentRow, currentRow.UserMultiplier, ""); err != nil {
			return nil, err
		}
		updatedAudit, getErr := s.client.TeamUsageRateMultiplierAudit.Get(ctx, audit.ID)
		if getErr != nil {
			return nil, fmt.Errorf("get no-op team usage audit: %w", getErr)
		}
		return buildUpdateMultiplierResponse(updatedAudit), nil
	}

	var lockResult SubscriptionRow
	lockErr := s.locker.WithProviderGroupLock(ctx, providerID, groupID, func(lockCtx context.Context) error {
		result, err := s.replaceMultiplierInsideLockResult(lockCtx, manager, providerID, relayUserID, groupID, requested, audit.ID)
		if err == nil {
			lockResult = result
		}
		return err
	})
	if lockErr != nil {
		return nil, lockErr
	}

	updatedAudit, err := s.client.TeamUsageRateMultiplierAudit.Get(ctx, audit.ID)
	if err != nil {
		return nil, fmt.Errorf("get updated team usage audit: %w", err)
	}
	if updatedAudit.Status == teamusageratemultiplieraudit.StatusRunning {
		if err := s.markAuditOutcome(ctx, audit.ID, teamusageratemultiplieraudit.StatusSucceeded, true, lockResult, requested, ""); err != nil {
			return nil, err
		}
		updatedAudit, err = s.client.TeamUsageRateMultiplierAudit.Get(ctx, audit.ID)
		if err != nil {
			return nil, fmt.Errorf("reload updated team usage audit: %w", err)
		}
	}
	if updatedAudit.Status == teamusageratemultiplieraudit.StatusPartialFailed {
		message := strings.TrimSpace(updatedAudit.ErrorMessage)
		if message == "" {
			message = ErrPartialFailed.Error()
		}
		return nil, fmt.Errorf("%w: %s", ErrPartialFailed, message)
	}
	return buildUpdateMultiplierResponse(updatedAudit), nil
}

func (s *Service) ListAudit(ctx context.Context, actorUserID int, params AuditListParams) (*AuditListResponse, error) {
	return s.listAudit(ctx, &actorUserID, &AdminAuditListParams{
		Page:         params.Page,
		PageSize:     params.PageSize,
		TargetUserID: params.TargetUserID,
	}, true)
}

func (s *Service) ListAdminAudit(ctx context.Context, params AdminAuditListParams) (*AuditListResponse, error) {
	return s.listAudit(ctx, params.ActorUserID, &params, false)
}

func (s *Service) replaceMultiplierInsideLock(ctx context.Context, provider relay.GroupRateMultiplierManager, providerID int, relayUserID int64, groupID int64, requested *float64, auditID int) error {
	_, err := s.replaceMultiplierInsideLockResult(ctx, provider, providerID, relayUserID, groupID, requested, auditID)
	return err
}

func (s *Service) replaceMultiplierInsideLockResult(ctx context.Context, provider relay.GroupRateMultiplierManager, providerID int, relayUserID int64, groupID int64, requested *float64, auditID int) (SubscriptionRow, error) {
	subscriptionLister, ok := any(provider).(relay.UserSubscriptionLister)
	if !ok {
		return SubscriptionRow{}, ErrProviderUnsupported
	}
	subscriptions, err := subscriptionLister.ListUserSubscriptions(ctx, relayUserID)
	if err != nil {
		baseErr := fmt.Errorf("list user subscriptions inside lock: %w", err)
		return SubscriptionRow{}, s.failAudit(ctx, auditID, baseErr)
	}
	subscription := findActiveSubscription(subscriptions, groupID)
	if subscription == nil {
		baseErr := &ForbiddenError{Reason: ErrInactiveSubscription.Error()}
		return SubscriptionRow{}, s.rejectAudit(ctx, auditID, teamusageratemultiplieraudit.RejectionReasonInactiveSubscription, ErrInactiveSubscription, nil, baseErr)
	}

	currentEntries, err := provider.ListGroupRateMultipliers(ctx, groupID)
	if err != nil {
		baseErr := fmt.Errorf("re-list group rate multipliers inside lock: %w", err)
		return SubscriptionRow{}, s.failAudit(ctx, auditID, baseErr)
	}
	currentEntry := findRateEntry(currentEntries, relayUserID)
	currentRow := buildSubscriptionRowFromSubscription(*subscription, currentEntry, false, nil)
	if SameMultiplierState(currentRow.UserMultiplier, requested) {
		if err := s.markAuditOutcome(ctx, auditID, teamusageratemultiplieraudit.StatusSucceeded, false, currentRow, currentRow.UserMultiplier, ""); err != nil {
			return SubscriptionRow{}, err
		}
		return currentRow, nil
	}

	merged := MergeRateEntries(currentEntries, relayUserID, requested)
	if err := provider.ReplaceGroupRateMultipliers(ctx, groupID, merged); err != nil {
		baseErr := fmt.Errorf("replace group rate multipliers: %w", err)
		return SubscriptionRow{}, s.failAudit(ctx, auditID, baseErr)
	}

	readBackEntries, err := provider.ListGroupRateMultipliers(ctx, groupID)
	if err != nil {
		baseErr := fmt.Errorf("read back group rate multipliers: %w", err)
		return SubscriptionRow{}, s.failAudit(ctx, auditID, baseErr)
	}
	readBackEntry := findRateEntry(readBackEntries, relayUserID)
	readBackRow := buildSubscriptionRowFromSubscription(*subscription, readBackEntry, false, nil)
	if !SameMultiplierState(readBackRow.UserMultiplier, requested) {
		if err := s.markAuditOutcome(ctx, auditID, teamusageratemultiplieraudit.StatusPartialFailed, true, readBackRow, readBackRow.UserMultiplier, "readback state did not match requested multiplier"); err != nil {
			return SubscriptionRow{}, err
		}
		return readBackRow, nil
	}
	if err := s.markAuditOutcome(ctx, auditID, teamusageratemultiplieraudit.StatusSucceeded, true, readBackRow, requested, ""); err != nil {
		return SubscriptionRow{}, err
	}
	return readBackRow, nil
}

func (s *Service) buildSubjectSubscriptionRows(ctx context.Context, provider relay.Provider, relayUserID int, params relay.UserUsageDashboardParams, canManageTarget bool, manageReason string) ([]SubscriptionRow, relay.UserUsageGroupQuotaState, error) {
	subscriptionLister, ok := provider.(relay.UserSubscriptionLister)
	if !ok {
		return []SubscriptionRow{}, relay.UserUsageGroupQuotaState{
			Status:  "unavailable",
			Message: "Group quotas are temporarily unavailable.",
			Groups:  []relay.UserUsageGroupQuotaGroupItem{},
		}, nil
	}
	subscriptions, err := subscriptionLister.ListUserSubscriptions(ctx, int64(relayUserID))
	if err != nil {
		return nil, relay.UserUsageGroupQuotaState{}, fmt.Errorf("list subject subscriptions: %w", err)
	}
	activeSubscriptions := make([]relay.UserSubscription, 0, len(subscriptions))
	groupIDs := make([]int64, 0, len(subscriptions))
	seenGroupIDs := make(map[int64]struct{}, len(subscriptions))
	for _, subscription := range subscriptions {
		if !strings.EqualFold(strings.TrimSpace(subscription.Status), "active") || subscription.Group == nil {
			continue
		}
		activeSubscriptions = append(activeSubscriptions, subscription)
		if _, seen := seenGroupIDs[subscription.GroupID]; seen {
			continue
		}
		seenGroupIDs[subscription.GroupID] = struct{}{}
		groupIDs = append(groupIDs, subscription.GroupID)
	}

	metadataByGroup := make(map[int64]relay.GroupRateMultiplierReadResult, len(groupIDs))
	duplicateMetadataGroupIDs := make(map[int64]struct{})
	batchReader, batchReaderOK := provider.(relay.GroupRateMultiplierBatchReader)
	if batchReaderOK && len(groupIDs) > 0 {
		for _, result := range batchReader.GroupRateMultipliersForGroups(ctx, groupIDs) {
			if _, requested := seenGroupIDs[result.GroupID]; !requested {
				continue
			}
			if _, duplicate := metadataByGroup[result.GroupID]; duplicate {
				duplicateMetadataGroupIDs[result.GroupID] = struct{}{}
				continue
			}
			metadataByGroup[result.GroupID] = result
		}
	}

	rows := make([]SubscriptionRow, 0, len(activeSubscriptions))
	for _, subscription := range activeSubscriptions {
		var editableReason *string
		editable := canManageTarget
		if !canManageTarget {
			reason := manageReason
			if strings.TrimSpace(reason) == "" {
				reason = ErrPolicyDenied.Error()
			}
			editableReason = &reason
		}

		metadataStatus := MultiplierMetadataStatusUnavailable
		var metadataMessage *string
		var currentEntry *relay.UserGroupRateEntry
		_, duplicateMetadata := duplicateMetadataGroupIDs[subscription.GroupID]
		if result, found := metadataByGroup[subscription.GroupID]; batchReaderOK && found && !duplicateMetadata && result.Err == nil {
			metadataStatus = MultiplierMetadataStatusOK
			currentEntry = findRateEntry(result.Entries, int64(relayUserID))
		} else {
			message := multiplierMetadataUnavailableMessage
			metadataMessage = &message
			if editable {
				editable = false
				reason := ErrMultiplierMetadataUnavailable.Error()
				editableReason = &reason
			}
		}
		rows = append(rows, buildSubscriptionRowFromSubscriptionWithMultiplierMetadata(
			subscription,
			currentEntry,
			editable,
			editableReason,
			metadataStatus,
			metadataMessage,
		))
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].GroupName == rows[j].GroupName {
			return rows[i].GroupID < rows[j].GroupID
		}
		return rows[i].GroupName < rows[j].GroupName
	})
	return rows, buildQuotaStateFromRows(rows, quotaWindowFromDashboardParams(params)), nil
}

func (s *Service) resolveScope(ctx context.Context, actorUserID int) (*representativescope.Scope, error) {
	if s.scopeResolver == nil {
		return nil, errors.New("teamusage scope resolver is not configured")
	}
	scope, err := s.scopeResolver.Resolve(ctx, actorUserID)
	if err != nil {
		return nil, err
	}
	if scope == nil {
		return &representativescope.Scope{ActorUserID: actorUserID}, nil
	}
	return scope, nil
}

func (s *Service) requireRepresentativeScope(ctx context.Context, actorUserID int) (*representativescope.Scope, error) {
	scope, err := s.resolveScope(ctx, actorUserID)
	if err != nil {
		return nil, err
	}
	if !scope.IsRepresentative {
		return nil, &ForbiddenError{Reason: ErrNotRepresentative.Error()}
	}
	return scope, nil
}

func (s *Service) resolvePrimaryProvider(ctx context.Context) (int, relay.Provider, error) {
	binding, err := s.resolvePrimaryProviderBinding(ctx)
	if err != nil {
		return 0, nil, err
	}
	return binding.ID, binding.Provider, nil
}

type primaryProviderBinding struct {
	ID                   int
	ConfigurationVersion int64
	Provider             relay.Provider
}

type primaryProviderConfig struct {
	ID                   int
	ConfigurationVersion int64
}

func (s *Service) resolvePrimaryProviderBinding(ctx context.Context) (*primaryProviderBinding, error) {
	config, err := s.resolvePrimaryProviderConfig(ctx)
	if err != nil {
		return nil, err
	}
	provider, err := s.providerResolver.Resolve(ctx, config.ID)
	if err != nil {
		return nil, err
	}
	return &primaryProviderBinding{
		ID: config.ID, ConfigurationVersion: config.ConfigurationVersion, Provider: provider,
	}, nil
}

func (s *Service) resolvePrimaryProviderConfig(ctx context.Context) (*primaryProviderConfig, error) {
	if s.client == nil {
		return nil, errors.New("teamusage ent client is not configured")
	}
	if s.providerResolver == nil {
		return nil, errors.New("teamusage provider resolver is not configured")
	}
	providers, err := s.client.RelayProvider.Query().
		Where(relayprovider.IsPrimary(true), relayprovider.Enabled(true)).
		Order(ent.Asc(relayprovider.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	if len(providers) == 0 {
		return &primaryProviderConfig{ID: 1, ConfigurationVersion: 1}, nil
	}
	providerRow := providers[0]
	return &primaryProviderConfig{
		ID: providerRow.ID, ConfigurationVersion: providerRow.ConfigurationVersion,
	}, nil
}

func (s *Service) resolveSubjectRelayUserID(ctx context.Context, provider relay.Provider, subject representativescope.Subject) (int64, representativescope.Subject, error) {
	if subject.RelayUserID == nil {
		return 0, subject, ErrNoRelayMapping
	}
	relayUserID, err := s.resolveRelayUserID(ctx, provider, subject.UserID, subject.Email, *subject.RelayUserID)
	if err != nil {
		return 0, subject, err
	}
	resolved := subject
	resolvedID := int(relayUserID)
	resolved.RelayUserID = &resolvedID
	return relayUserID, resolved, nil
}

func (s *Service) resolveOverviewSubjectRelayUserID(ctx context.Context, provider relay.Provider, subject representativescope.Subject) (int64, representativescope.Subject, error) {
	if subject.RelayUserID != nil {
		return s.resolveSubjectRelayUserID(ctx, provider, subject)
	}
	email := strings.TrimSpace(subject.Email)
	if email == "" {
		return 0, subject, ErrNoRelayMapping
	}
	relayUser, err := provider.FindUserByEmail(ctx, email)
	if err != nil {
		return 0, subject, fmt.Errorf("find overview relay user by email: %w", err)
	}
	if relayUser == nil || relayUser.ID <= 0 || !strings.EqualFold(strings.TrimSpace(relayUser.Email), email) {
		return 0, subject, ErrNoRelayMapping
	}
	resolved := subject
	relayUserID := int(relayUser.ID)
	resolved.RelayUserID = &relayUserID
	return relayUser.ID, resolved, nil
}

func (s *Service) resolveEntUserRelayUserID(ctx context.Context, provider relay.Provider, user *ent.User) (int64, error) {
	if user == nil || user.RelayUserID == nil {
		return 0, ErrNoRelayMapping
	}
	return s.resolveRelayUserID(ctx, provider, user.ID, user.Email, *user.RelayUserID)
}

func (s *Service) resolveRelayUserID(ctx context.Context, provider relay.Provider, localUserID int, email string, cachedRelayUserID int) (int64, error) {
	if cachedRelayUserID <= 0 {
		return 0, ErrNoRelayMapping
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return int64(cachedRelayUserID), nil
	}

	if current, err := provider.GetUser(ctx, int64(cachedRelayUserID)); err != nil {
		return 0, fmt.Errorf("get cached relay user: %w", err)
	} else if current != nil && strings.EqualFold(strings.TrimSpace(current.Email), email) {
		return int64(cachedRelayUserID), nil
	}

	relayUser, err := provider.FindUserByEmail(ctx, email)
	if err != nil {
		return 0, fmt.Errorf("find relay user by email: %w", err)
	}
	if relayUser == nil || relayUser.ID <= 0 {
		return int64(cachedRelayUserID), nil
	}
	if !strings.EqualFold(strings.TrimSpace(relayUser.Email), email) {
		return int64(cachedRelayUserID), nil
	}

	resolvedRelayUserID := int(relayUser.ID)
	if resolvedRelayUserID != cachedRelayUserID && s.client != nil && localUserID > 0 {
		if err := s.client.User.UpdateOneID(localUserID).SetRelayUserID(resolvedRelayUserID).Exec(ctx); err != nil {
			return 0, fmt.Errorf("persist relay user binding: %w", err)
		}
	}
	return relayUser.ID, nil
}

func (s *Service) listAudit(ctx context.Context, actorFilter *int, params *AdminAuditListParams, redactOutOfScope bool) (*AuditListResponse, error) {
	page, pageSize := normalizePage(params.Page, params.PageSize)
	query := s.client.TeamUsageRateMultiplierAudit.Query()
	if actorFilter != nil {
		query = query.Where(teamusageratemultiplieraudit.ActorUserIDEQ(*actorFilter))
	}
	if params.TargetUserID != nil {
		query = query.Where(teamusageratemultiplieraudit.TargetUserIDEQ(*params.TargetUserID))
	}
	if status := strings.TrimSpace(params.Status); status != "" {
		query = query.Where(teamusageratemultiplieraudit.StatusEQ(teamusageratemultiplieraudit.Status(status)))
	}
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count team usage audit rows: %w", err)
	}
	rows, err := query.
		Order(
			teamusageratemultiplieraudit.ByCreatedAt(entsql.OrderDesc()),
			teamusageratemultiplieraudit.ByID(entsql.OrderDesc()),
		).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list team usage audit rows: %w", err)
	}
	items := make([]AuditRecord, 0, len(rows))
	for _, row := range rows {
		items = append(items, buildAuditRecord(row, redactOutOfScope))
	}
	return &AuditListResponse{
		Items:    items,
		Page:     page,
		PageSize: pageSize,
		Total:    total,
	}, nil
}

func (s *Service) setAuditScopeEvidence(ctx context.Context, auditID int, scope *representativescope.Scope, targetUserID int) error {
	if s.client == nil {
		return errors.New("teamusage ent client is not configured")
	}
	scopeEvidence := map[string]any{
		"represented_department_ids": append([]string(nil), scope.RepresentedDepartmentIDs...),
		"subject_count":              len(scope.Subjects),
		"target_user_id":             targetUserID,
	}
	if subject, ok := findScopedSubject(scope.Subjects, targetUserID); ok {
		scopeEvidence["target_display_name"] = strings.TrimSpace(subject.DisplayName)
		scopeEvidence["target_email"] = strings.TrimSpace(subject.Email)
	}
	return s.client.TeamUsageRateMultiplierAudit.UpdateOneID(auditID).
		SetScopeEvidence(scopeEvidence).
		Exec(ctx)
}

func (s *Service) setAuditCurrentState(ctx context.Context, auditID, targetUserID, providerID int, relayUserID int64, row SubscriptionRow) error {
	return s.client.TeamUsageRateMultiplierAudit.UpdateOneID(auditID).
		SetTargetUserID(targetUserID).
		SetProviderID(providerID).
		SetRelayUserID(relayUserID).
		SetGroupName(row.GroupName).
		SetNillableOldMultiplier(row.UserMultiplier).
		SetOldMultiplierSource(oldMultiplierSourceFromString(row.MultiplierSource)).
		SetOldEffectiveLimits(effectiveLimitsMap(row)).
		Exec(ctx)
}

func (s *Service) captureRejectedTargetMetadata(ctx context.Context, auditID int, targetUserID int) (*int, error) {
	targetUser, err := s.client.User.Get(ctx, targetUserID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get target user for rejected audit: %w", err)
	}
	if err := s.setAuditTargetMetadata(ctx, auditID, targetUser); err != nil {
		return nil, err
	}
	return targetUserIDPtr(targetUserID), nil
}

func (s *Service) setAuditTargetMetadata(ctx context.Context, auditID int, targetUser *ent.User) error {
	row, err := s.client.TeamUsageRateMultiplierAudit.Get(ctx, auditID)
	if err != nil {
		return fmt.Errorf("get team usage audit target metadata: %w", err)
	}
	scopeEvidence := cloneMap(row.ScopeEvidence)
	if scopeEvidence == nil {
		scopeEvidence = map[string]any{}
	}
	scopeEvidence["target_display_name"] = strings.TrimSpace(targetUser.Username)
	scopeEvidence["target_email"] = strings.TrimSpace(targetUser.Email)
	return s.client.TeamUsageRateMultiplierAudit.UpdateOneID(auditID).
		SetScopeEvidence(scopeEvidence).
		Exec(ctx)
}

func (s *Service) markAuditRejected(ctx context.Context, auditID int, rejection teamusageratemultiplieraudit.RejectionReason, cause error, targetUserID *int) error {
	update := s.client.TeamUsageRateMultiplierAudit.UpdateOneID(auditID).
		SetStatus(teamusageratemultiplieraudit.StatusRejected).
		SetRejectionReason(rejection)
	if targetUserID != nil {
		update.SetTargetUserID(*targetUserID)
	}
	if cause != nil {
		update.SetErrorMessage(cause.Error())
	}
	return update.Exec(ctx)
}

func (s *Service) markAuditFailed(ctx context.Context, auditID int, status teamusageratemultiplieraudit.Status, message string) error {
	if status == "" {
		status = teamusageratemultiplieraudit.StatusFailed
	}
	return s.client.TeamUsageRateMultiplierAudit.UpdateOneID(auditID).
		SetStatus(status).
		SetErrorMessage(strings.TrimSpace(message)).
		Exec(ctx)
}

func (s *Service) markAuditOutcome(ctx context.Context, auditID int, status teamusageratemultiplieraudit.Status, changed bool, row SubscriptionRow, newMultiplier *float64, errorMessage string) error {
	update := s.client.TeamUsageRateMultiplierAudit.UpdateOneID(auditID).
		SetStatus(status).
		SetChanged(changed).
		SetNillableNewMultiplier(newMultiplier).
		SetNewMultiplierSource(newMultiplierSourceFromString(row.MultiplierSource)).
		SetNewEffectiveLimits(effectiveLimitsMap(row)).
		SetErrorMessage(strings.TrimSpace(errorMessage))
	return update.Exec(ctx)
}

func buildSubscriptionRowFromSubscription(subscription relay.UserSubscription, entry *relay.UserGroupRateEntry, editable bool, editableReason *string) SubscriptionRow {
	return buildSubscriptionRowFromSubscriptionWithMultiplierMetadata(
		subscription,
		entry,
		editable,
		editableReason,
		MultiplierMetadataStatusOK,
		nil,
	)
}

func buildSubscriptionRowFromSubscriptionWithMultiplierMetadata(subscription relay.UserSubscription, entry *relay.UserGroupRateEntry, editable bool, editableReason *string, metadataStatus string, metadataMessage *string) SubscriptionRow {
	var userMultiplier *float64
	if entry != nil {
		userMultiplier = entry.RateMultiplier
	}
	input := SubscriptionInput{
		GroupID:                   strconv.FormatInt(subscription.GroupID, 10),
		GroupName:                 "",
		Platform:                  "",
		SubscriptionStatus:        strings.TrimSpace(subscription.Status),
		GroupDefaultMultiplier:    nil,
		SystemDefaultMultiplier:   systemDefaultMultiplier,
		UserMultiplier:            userMultiplier,
		DailyUsageUSD:             subscription.DailyUsageUSD,
		WeeklyUsageUSD:            subscription.WeeklyUsageUSD,
		MonthlyUsageUSD:           subscription.MonthlyUsageUSD,
		UsageValueBasis:           "raw_actual_cost",
		Editable:                  editable,
		EditableReason:            editableReason,
		MultiplierMetadataStatus:  metadataStatus,
		MultiplierMetadataMessage: metadataMessage,
	}
	if subscription.Group != nil {
		input.GroupName = strings.TrimSpace(subscription.Group.Name)
		input.Platform = strings.TrimSpace(subscription.Group.Platform)
		input.GroupDefaultMultiplier = subscription.Group.RateMultiplier
		input.DailyLimitUSD = subscription.Group.DailyLimitUSD
		input.WeeklyLimitUSD = subscription.Group.WeeklyLimitUSD
		input.MonthlyLimitUSD = subscription.Group.MonthlyLimitUSD
	}
	return BuildSubscriptionRow(input)
}

func buildQuotaStateFromRows(rows []SubscriptionRow, window string) relay.UserUsageGroupQuotaState {
	if len(rows) == 0 {
		return relay.UserUsageGroupQuotaState{
			Status: "empty",
			Groups: []relay.UserUsageGroupQuotaGroupItem{},
		}
	}
	items := make([]relay.UserUsageGroupQuotaGroupItem, 0, len(rows))
	for _, row := range rows {
		used, quota, source := quotaDisplayForRow(row, window)
		items = append(items, relay.UserUsageGroupQuotaGroupItem{
			GroupID:     row.GroupID,
			GroupName:   row.GroupName,
			Platform:    row.Platform,
			UsedAmount:  used,
			QuotaAmount: quota,
			IsUnlimited: quota == nil,
			QuotaSource: source,
		})
	}
	return relay.UserUsageGroupQuotaState{
		Status:    "ok",
		UnitLabel: "USD",
		Groups:    items,
	}
}

func quotaDisplayForRow(row SubscriptionRow, window string) (*float64, *float64, string) {
	source := "group_monthly_subscription"
	switch window {
	case "daily":
		used := row.DailyDisplayUsedUSD
		if row.DailyEffectiveAllowanceUnlimited {
			return &used, nil, "group_daily_subscription"
		}
		return &used, row.DailyEffectiveAllowanceUSD, "group_daily_subscription"
	case "weekly":
		used := row.WeeklyDisplayUsedUSD
		if row.WeeklyEffectiveAllowanceUnlimited {
			return &used, nil, "group_weekly_subscription"
		}
		return &used, row.WeeklyEffectiveAllowanceUSD, "group_weekly_subscription"
	default:
		used := row.MonthlyDisplayUsedUSD
		if row.MonthlyEffectiveAllowanceUnlimited {
			return &used, nil, source
		}
		return &used, row.MonthlyEffectiveAllowanceUSD, source
	}
}

func quotaWindowFromDashboardParams(params relay.UserUsageDashboardParams) string {
	if strings.EqualFold(strings.TrimSpace(params.Granularity), "hour") {
		return "daily"
	}
	start, errStart := time.Parse("2006-01-02", strings.TrimSpace(params.StartDate))
	end, errEnd := time.Parse("2006-01-02", strings.TrimSpace(params.EndDate))
	if errStart == nil && errEnd == nil {
		days := int(end.Sub(start).Hours()/24) + 1
		if days <= 1 {
			return "daily"
		}
		if days <= 7 {
			return "weekly"
		}
	}
	return "monthly"
}

func normalizeOverviewParams(params OverviewParams) (OverviewParams, error) {
	normalized := OverviewParams{
		StartDate: strings.TrimSpace(params.StartDate), EndDate: strings.TrimSpace(params.EndDate),
		Granularity: strings.ToLower(strings.TrimSpace(params.Granularity)), Timezone: strings.TrimSpace(params.Timezone),
		Page: params.Page, PageSize: params.PageSize,
	}
	if normalized.Timezone == "" {
		normalized.Timezone = "UTC"
	}
	if normalized.Granularity != "day" && normalized.Granularity != "hour" {
		return OverviewParams{}, fmt.Errorf("%w: granularity must be day or hour", ErrInvalidOverviewParams)
	}
	location, err := time.LoadLocation(normalized.Timezone)
	if err != nil {
		return OverviewParams{}, fmt.Errorf("%w: invalid timezone", ErrInvalidOverviewParams)
	}
	start, err := time.ParseInLocation("2006-01-02", normalized.StartDate, location)
	if err != nil {
		return OverviewParams{}, fmt.Errorf("%w: invalid start date", ErrInvalidOverviewParams)
	}
	end, err := time.ParseInLocation("2006-01-02", normalized.EndDate, location)
	if err != nil {
		return OverviewParams{}, fmt.Errorf("%w: invalid end date", ErrInvalidOverviewParams)
	}
	if end.Before(start) {
		return OverviewParams{}, fmt.Errorf("%w: end date precedes start date", ErrInvalidOverviewParams)
	}
	return normalized, nil
}

func buildOverviewWindow(params OverviewParams) OverviewWindow {
	location := time.UTC
	if timezone := strings.TrimSpace(params.Timezone); timezone != "" {
		if loaded, err := time.LoadLocation(timezone); err == nil {
			location = loaded
		}
	}
	today := time.Now().In(location).Format("2006-01-02")
	rollingDays := 0
	start, errStart := time.ParseInLocation("2006-01-02", strings.TrimSpace(params.StartDate), location)
	end, errEnd := time.ParseInLocation("2006-01-02", strings.TrimSpace(params.EndDate), location)
	if errStart == nil && errEnd == nil && !end.Before(start) {
		rollingDays = int(end.Sub(start).Hours()/24) + 1
	}
	return OverviewWindow{
		StartDate:   strings.TrimSpace(params.StartDate),
		EndDate:     strings.TrimSpace(params.EndDate),
		Granularity: strings.TrimSpace(params.Granularity),
		Today:       today,
		RollingDays: rollingDays,
		Timezone:    strings.TrimSpace(params.Timezone),
	}
}

type overviewWindowTotal struct {
	ActualCost  float64
	TotalTokens *int64
}

func buildOverviewWindowTotals(pointsByUser map[int64][]relay.UsageTrendPoint) map[int64]overviewWindowTotal {
	totals := make(map[int64]overviewWindowTotal, len(pointsByUser))
	for relayUserID, points := range pointsByUser {
		var total overviewWindowTotal
		var tokenTotal int64
		hasTokens := false
		for _, point := range points {
			total.ActualCost += point.ActualCost
			if point.TotalTokens == nil {
				continue
			}
			tokenTotal += *point.TotalTokens
			hasTokens = true
		}
		if hasTokens {
			total.TotalTokens = &tokenTotal
		}
		totals[relayUserID] = total
	}
	return totals
}

func sumOverviewWindowCosts(totals map[int64]overviewWindowTotal) *float64 {
	total := 0.0
	for _, item := range totals {
		total += item.ActualCost
	}
	return &total
}

func sumOverviewWindowTokens(totals map[int64]overviewWindowTotal) *int64 {
	var total int64
	hasTokens := false
	for _, item := range totals {
		if item.TotalTokens == nil {
			continue
		}
		total += *item.TotalTokens
		hasTokens = true
	}
	if !hasTokens {
		return nil
	}
	return &total
}

func sumOverviewComparisonCosts(stats map[int64]relay.TeamUserUsageStats) (*float64, *float64) {
	today := 0.0
	total := 0.0
	for _, item := range stats {
		today += item.TodayActualCost
		total += item.TotalActualCost
	}
	return &today, &total
}

func filterSubjects(subjects []representativescope.Subject, q string) []representativescope.Subject {
	if strings.TrimSpace(q) == "" {
		return append([]representativescope.Subject(nil), subjects...)
	}
	needle := strings.ToLower(strings.TrimSpace(q))
	filtered := make([]representativescope.Subject, 0, len(subjects))
	for _, subject := range subjects {
		haystack := strings.ToLower(strings.Join([]string{
			subject.DisplayName,
			subject.Email,
			subject.DepartmentDisplayPath,
		}, "\n"))
		if strings.Contains(haystack, needle) {
			filtered = append(filtered, subject)
		}
	}
	return filtered
}

func findScopedSubject(subjects []representativescope.Subject, targetUserID int) (*representativescope.Subject, bool) {
	for i := range subjects {
		if subjects[i].SubjectType == "member" && subjects[i].UserID == targetUserID {
			return &subjects[i], true
		}
	}
	return nil, false
}

func findActiveSubscription(subscriptions []relay.UserSubscription, groupID int64) *relay.UserSubscription {
	for i := range subscriptions {
		if subscriptions[i].GroupID == groupID && strings.EqualFold(strings.TrimSpace(subscriptions[i].Status), "active") {
			return &subscriptions[i]
		}
	}
	return nil
}

func findRateEntry(entries []relay.UserGroupRateEntry, relayUserID int64) *relay.UserGroupRateEntry {
	for i := range entries {
		if entries[i].UserID == relayUserID {
			return &entries[i]
		}
	}
	return nil
}

func effectiveLimitsMap(row SubscriptionRow) map[string]any {
	return map[string]any{
		"daily_effective_allowance_usd":   nullableFloatMapValue(row.DailyEffectiveAllowanceUSD),
		"weekly_effective_allowance_usd":  nullableFloatMapValue(row.WeeklyEffectiveAllowanceUSD),
		"monthly_effective_allowance_usd": nullableFloatMapValue(row.MonthlyEffectiveAllowanceUSD),
	}
}

func nullableFloatMapValue(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func trimReason(reason string) string {
	reason = strings.TrimSpace(reason)
	runes := []rune(reason)
	if len(runes) > 500 {
		return string(runes[:500])
	}
	return reason
}

func redactedRequestMetadata(req UpdateMultiplierRequest) map[string]any {
	return map[string]any{
		"mode":                strings.TrimSpace(req.Mode),
		"has_rate_multiplier": req.RateMultiplier != nil,
	}
}

func auditActionForMode(mode string) teamusageratemultiplieraudit.Action {
	if strings.EqualFold(strings.TrimSpace(mode), "reset") {
		return teamusageratemultiplieraudit.ActionResetRateMultiplier
	}
	return teamusageratemultiplieraudit.ActionSetRateMultiplier
}

func rejectionForReason(reason string) (teamusageratemultiplieraudit.RejectionReason, error) {
	switch strings.TrimSpace(reason) {
	case ErrSelfEditForbidden.Error():
		return teamusageratemultiplieraudit.RejectionReasonSelfEditForbidden, ErrSelfEditForbidden
	case ErrOutOfScope.Error():
		return teamusageratemultiplieraudit.RejectionReasonOutOfScope, ErrOutOfScope
	case ErrNotUpperLevelRepresentative.Error():
		return teamusageratemultiplieraudit.RejectionReasonNotUpperLevelRepresentative, ErrNotUpperLevelRepresentative
	default:
		return teamusageratemultiplieraudit.RejectionReasonPolicyDenied, ErrPolicyDenied
	}
}

func oldMultiplierSourceFromString(source string) teamusageratemultiplieraudit.OldMultiplierSource {
	switch strings.TrimSpace(source) {
	case "user":
		return teamusageratemultiplieraudit.OldMultiplierSourceUser
	case "group":
		return teamusageratemultiplieraudit.OldMultiplierSourceGroup
	case "system":
		return teamusageratemultiplieraudit.OldMultiplierSourceSystem
	default:
		return teamusageratemultiplieraudit.OldMultiplierSourceUnknown
	}
}

func newMultiplierSourceFromString(source string) teamusageratemultiplieraudit.NewMultiplierSource {
	switch strings.TrimSpace(source) {
	case "user":
		return teamusageratemultiplieraudit.NewMultiplierSourceUser
	case "group":
		return teamusageratemultiplieraudit.NewMultiplierSourceGroup
	case "system":
		return teamusageratemultiplieraudit.NewMultiplierSourceSystem
	default:
		return teamusageratemultiplieraudit.NewMultiplierSourceUnknown
	}
}

func buildAuditRecord(row *ent.TeamUsageRateMultiplierAudit, redactOutOfScope bool) AuditRecord {
	record := AuditRecord{
		ID:                row.ID,
		ActorUserID:       row.ActorUserID,
		TargetUserID:      row.TargetUserID,
		TargetDisplayName: stringFromMap(row.ScopeEvidence, "target_display_name"),
		TargetEmail:       stringFromMap(row.ScopeEvidence, "target_email"),
		GroupID:           row.GroupID,
		GroupName:         row.GroupName,
		Action:            row.Action.String(),
		Status:            row.Status.String(),
		OldMultiplier:     row.OldMultiplier,
		NewMultiplier:     row.NewMultiplier,
		Changed:           row.Changed,
		RequestMetadata:   cloneMap(row.RequestMetadata),
		Reason:            row.Reason,
		ErrorMessage:      row.ErrorMessage,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
	if row.RejectionReason != nil {
		reason := row.RejectionReason.String()
		record.RejectionReason = &reason
		if redactOutOfScope && reason == teamusageratemultiplieraudit.RejectionReasonOutOfScope.String() {
			record.TargetUserID = nil
			record.TargetDisplayName = ""
			record.TargetEmail = ""
			record.RequestMetadata = nil
		}
	}
	return record
}

func (s *Service) failAudit(ctx context.Context, auditID int, baseErr error) error {
	if baseErr == nil {
		return nil
	}
	auditErr := s.markAuditFailed(ctx, auditID, teamusageratemultiplieraudit.StatusFailed, baseErr.Error())
	return combineAuditUpdateError(baseErr, auditErr)
}

func (s *Service) rejectAudit(ctx context.Context, auditID int, rejection teamusageratemultiplieraudit.RejectionReason, cause error, targetUserID *int, baseErr error) error {
	if baseErr == nil {
		baseErr = cause
	}
	auditErr := s.markAuditRejected(ctx, auditID, rejection, cause, targetUserID)
	return combineAuditUpdateError(baseErr, auditErr)
}

func combineAuditUpdateError(baseErr error, auditErr error) error {
	if auditErr == nil {
		return baseErr
	}
	if baseErr == nil {
		return fmt.Errorf("update team usage audit: %w", auditErr)
	}
	return fmt.Errorf("%w; team usage audit update failed: %v", baseErr, auditErr)
}

func isHardOverviewTrendError(err error) bool {
	switch {
	case errors.Is(err, relay.ErrInvalidCredentials):
		return true
	case errors.Is(err, ErrNotRepresentative):
		return true
	case errors.Is(err, ErrOutOfScope):
		return true
	case errors.Is(err, ErrSelfEditForbidden):
		return true
	case errors.Is(err, ErrNotUpperLevelRepresentative):
		return true
	case errors.Is(err, ErrNoRelayMapping):
		return true
	}
	var forbidden *ForbiddenError
	if errors.As(err, &forbidden) {
		switch strings.TrimSpace(forbidden.Reason) {
		case ErrNotRepresentative.Error(), ErrOutOfScope.Error(), ErrSelfEditForbidden.Error(), ErrNotUpperLevelRepresentative.Error(), ErrNoRelayMapping.Error():
			return true
		}
	}
	return false
}

func isHardTrendSnapshotOriginError(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return true
	}
	return errors.Is(err, relay.ErrInvalidCredentials) ||
		errors.Is(err, errPrewarmAuthorizationChanged) ||
		errors.Is(err, ErrProviderUnsupported) ||
		errors.Is(err, ErrInvalidOverviewParams) ||
		isHardOverviewTrendError(err)
}

func isHardSnapshotOriginError(err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, errPrewarmAuthorizationChanged) ||
		errors.Is(err, relay.ErrInvalidCredentials) ||
		errors.Is(err, ErrProviderUnsupported) ||
		errors.Is(err, ErrInvalidOverviewParams)
}

func cloneMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func stringFromMap(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	text, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func buildUpdateMultiplierResponse(row *ent.TeamUsageRateMultiplierAudit) *UpdateMultiplierResponse {
	return &UpdateMultiplierResponse{
		Status:                          row.Status.String(),
		AuditID:                         row.ID,
		GroupID:                         row.GroupID,
		OldMultiplier:                   row.OldMultiplier,
		OldMultiplierSource:             row.OldMultiplierSource.String(),
		NewMultiplier:                   row.NewMultiplier,
		NewMultiplierSource:             row.NewMultiplierSource.String(),
		Changed:                         row.Changed,
		OldEffectiveMonthlyAllowanceUSD: floatFromMap(row.OldEffectiveLimits, "monthly_effective_allowance_usd"),
		NewEffectiveMonthlyAllowanceUSD: floatFromMap(row.NewEffectiveLimits, "monthly_effective_allowance_usd"),
	}
}

func floatFromMap(values map[string]any, key string) *float64 {
	if len(values) == 0 {
		return nil
	}
	raw, ok := values[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case float64:
		return &v
	case float32:
		out := float64(v)
		return &out
	case int:
		out := float64(v)
		return &out
	case int64:
		out := float64(v)
		return &out
	default:
		return nil
	}
}

func chunkInt64s(values []int64, chunkSize int) [][]int64 {
	if chunkSize <= 0 || len(values) == 0 {
		return [][]int64{values}
	}
	out := make([][]int64, 0, (len(values)+chunkSize-1)/chunkSize)
	for start := 0; start < len(values); start += chunkSize {
		end := start + chunkSize
		if end > len(values) {
			end = len(values)
		}
		out = append(out, values[start:end])
	}
	return out
}

func uniqueInt64s(values []int64) []int64 {
	unique := make([]int64, 0, len(values))
	seen := make(map[int64]struct{}, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func normalizePage(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return page, pageSize
}

func targetUserIDPtr(value int) *int {
	return &value
}
