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

const systemDefaultMultiplier = 1.0

type ProviderResolver interface {
	Resolve(ctx context.Context, providerID int) (relay.Provider, error)
}

type ScopeResolver interface {
	Resolve(ctx context.Context, actorUserID int) (*representativescope.Scope, error)
}

type Service struct {
	client           *ent.Client
	scopeResolver    ScopeResolver
	providerResolver ProviderResolver
	locker           AdvisoryLocker
	fullScopeCap     int
	maxMultiplier    float64
}

func NewService(client *ent.Client, scopeResolver ScopeResolver, providerResolver ProviderResolver, locker AdvisoryLocker) *Service {
	if locker == nil {
		locker = &PostgresAdvisoryLocker{}
	}
	return &Service{
		client:           client,
		scopeResolver:    scopeResolver,
		providerResolver: providerResolver,
		locker:           locker,
		fullScopeCap:     500,
		maxMultiplier:    defaultMaxMultiplier,
	}
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
	dashboardProvider, ok := provider.(relay.SubjectUsageDashboardProvider)
	if !ok {
		return nil, ErrProviderUnsupported
	}
	snapshot, err := dashboardProvider.GetUsageDashboardForUser(ctx, int64(*subject.RelayUserID), params)
	if err != nil {
		return nil, fmt.Errorf("get subject usage dashboard: %w", err)
	}
	if snapshot == nil {
		return nil, errors.New("get subject usage dashboard: empty response")
	}

	rows, quotaState, err := s.buildSubjectSubscriptionRows(ctx, provider, *subject.RelayUserID, params, canManageTarget, manageReason)
	if err != nil {
		return nil, err
	}
	return &SubjectDashboardResponse{
		Subject:                   *subject,
		Configured:                snapshot.Configured,
		Range:                     snapshot.Range,
		Stats:                     snapshot.Stats,
		Trend:                     snapshot.Trend,
		Models:                    snapshot.Models,
		GroupQuotas:               quotaState,
		SubjectSubscriptionGroups: rows,
	}, nil
}

func (s *Service) Overview(ctx context.Context, actorUserID int, params OverviewParams) (*OverviewResponse, error) {
	scope, err := s.requireRepresentativeScope(ctx, actorUserID)
	if err != nil {
		return nil, err
	}

	subjects := make([]representativescope.Subject, 0, len(scope.Subjects))
	relayUserIDs := make([]int64, 0, len(scope.Subjects))
	for _, subject := range scope.Subjects {
		if subject.RelayUserID == nil || !subject.Selectable {
			continue
		}
		subjects = append(subjects, subject)
		relayUserIDs = append(relayUserIDs, int64(*subject.RelayUserID))
	}
	if len(relayUserIDs) > s.fullScopeCap {
		response := BuildOverviewUnavailableForLargeScope(subjects, s.fullScopeCap)
		response.Window = buildOverviewWindow(params)
		response.Summary.RelayMemberCount = len(relayUserIDs)
		return &response, nil
	}

	_, provider, err := s.resolvePrimaryProvider(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve primary relay provider: %w", err)
	}
	summaryProvider, ok := provider.(relay.TeamUsageSummaryProvider)
	if !ok {
		return nil, ErrProviderUnsupported
	}

	statsByRelayUserID := make(map[int64]relay.TeamUserUsageStats, len(relayUserIDs))
	for _, chunk := range chunkInt64s(relayUserIDs, 100) {
		stats, err := summaryProvider.GetBatchUserUsageStats(ctx, chunk, relay.TeamUsageSummaryParams{Timezone: strings.TrimSpace(params.Timezone)})
		if err != nil {
			return nil, fmt.Errorf("get batch team usage stats: %w", err)
		}
		for relayUserID, stat := range stats {
			statsByRelayUserID[relayUserID] = stat
		}
	}

	members := RankTopMembers(subjects, statsByRelayUserID, 0)
	topMembers := RankTopMembers(subjects, statsByRelayUserID, 12)

	trendState := TopMemberTrendState{
		UnitLabel: "USD",
		RankBasis: "last_30d_actual_cost",
		Series:    make([]TopMemberTrendSeries, 0, len(topMembers)),
	}
	trendProvider, ok := provider.(relay.TeamMemberTrendProvider)
	if !ok {
		return nil, ErrProviderUnsupported
	}
	for _, member := range topMembers {
		series := TopMemberTrendSeries{
			UserID:      member.UserID,
			DisplayName: member.DisplayName,
			Rank:        member.Rank,
			Points:      []relay.UsageTrendPoint{},
		}
		if member.RelayUserID == nil {
			reason := "provider_error"
			series.Unavailable = true
			series.UnavailableReason = &reason
			trendState.Series = append(trendState.Series, series)
			continue
		}
		pointsByUser, err := trendProvider.GetUsageTrendForUsers(ctx, []int64{int64(*member.RelayUserID)}, relay.TeamMemberTrendParams{
			StartDate:   strings.TrimSpace(params.StartDate),
			EndDate:     strings.TrimSpace(params.EndDate),
			Granularity: strings.TrimSpace(params.Granularity),
			Timezone:    strings.TrimSpace(params.Timezone),
		})
		if err != nil {
			if isHardOverviewTrendError(err) {
				return nil, fmt.Errorf("get usage trend for relay user %d: %w", *member.RelayUserID, err)
			}
			reason := "provider_error"
			series.Unavailable = true
			series.UnavailableReason = &reason
			trendState.Series = append(trendState.Series, series)
			continue
		}
		series.Points = append(series.Points, pointsByUser[int64(*member.RelayUserID)]...)
		trendState.Series = append(trendState.Series, series)
	}

	todayCost, last30dCost := sumOverviewStats(statsByRelayUserID)
	return &OverviewResponse{
		Configured:       true,
		IsRepresentative: true,
		Window:           buildOverviewWindow(params),
		Summary: OverviewSummary{
			MemberCount:       len(scope.Subjects),
			RelayMemberCount:  len(relayUserIDs),
			TodayActualCost:   todayCost,
			Last30dActualCost: last30dCost,
			UnitLabel:         "USD",
		},
		TopMembers:     topMembers,
		TopMemberTrend: trendState,
		Members:        members,
	}, nil
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
	relayUserID := int64(*targetUser.RelayUserID)

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
	manager, managerOK := provider.(relay.GroupRateMultiplierManager)

	rows := make([]SubscriptionRow, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		if !strings.EqualFold(strings.TrimSpace(subscription.Status), "active") || subscription.Group == nil {
			continue
		}
		var editableReason *string
		editable := canManageTarget
		switch {
		case !canManageTarget:
			reason := manageReason
			if strings.TrimSpace(reason) == "" {
				reason = ErrPolicyDenied.Error()
			}
			editableReason = &reason
		case !managerOK:
			editable = false
			reason := ErrProviderUnsupported.Error()
			editableReason = &reason
		}

		var currentEntry *relay.UserGroupRateEntry
		if managerOK {
			entries, err := manager.ListGroupRateMultipliers(ctx, subscription.GroupID)
			if err != nil {
				editable = false
				reason := ErrProviderUnsupported.Error()
				editableReason = &reason
			} else {
				currentEntry = findRateEntry(entries, int64(relayUserID))
			}
		}
		rows = append(rows, buildSubscriptionRowFromSubscription(subscription, currentEntry, editable, editableReason))
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
	if s.client == nil {
		return 0, nil, errors.New("teamusage ent client is not configured")
	}
	if s.providerResolver == nil {
		return 0, nil, errors.New("teamusage provider resolver is not configured")
	}
	providers, err := s.client.RelayProvider.Query().
		Where(relayprovider.IsPrimary(true)).
		All(ctx)
	if err != nil {
		return 0, nil, err
	}
	if len(providers) == 0 {
		provider, err := s.providerResolver.Resolve(ctx, 1)
		return 1, provider, err
	}
	providerID := providers[0].ID
	provider, err := s.providerResolver.Resolve(ctx, providerID)
	return providerID, provider, err
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
	var userMultiplier *float64
	if entry != nil {
		userMultiplier = entry.RateMultiplier
	}
	input := SubscriptionInput{
		GroupID:                 strconv.FormatInt(subscription.GroupID, 10),
		GroupName:               "",
		Platform:                "",
		SubscriptionStatus:      strings.TrimSpace(subscription.Status),
		GroupDefaultMultiplier:  nil,
		SystemDefaultMultiplier: systemDefaultMultiplier,
		UserMultiplier:          userMultiplier,
		DailyUsageUSD:           subscription.DailyUsageUSD,
		WeeklyUsageUSD:          subscription.WeeklyUsageUSD,
		MonthlyUsageUSD:         subscription.MonthlyUsageUSD,
		UsageValueBasis:         "raw_actual_cost",
		Editable:                editable,
		EditableReason:          editableReason,
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
		return &used, row.DailyEffectiveAllowanceUSD, "group_daily_subscription"
	case "weekly":
		used := row.WeeklyDisplayUsedUSD
		return &used, row.WeeklyEffectiveAllowanceUSD, "group_weekly_subscription"
	default:
		used := row.MonthlyDisplayUsedUSD
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

func sumOverviewStats(statsByRelayUserID map[int64]relay.TeamUserUsageStats) (*float64, *float64) {
	today := 0.0
	last30d := 0.0
	for _, stat := range statsByRelayUserID {
		today += stat.TodayActualCost
		last30d += stat.Last30dActualCost
	}
	return &today, &last30d
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
