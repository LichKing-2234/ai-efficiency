package quotareset

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/quotaresetrequest"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestevent"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/relay"
)

const (
	defaultPage     = 1
	defaultPageSize = 20
	maxPageSize     = 100
)

type Service struct {
	client           *ent.Client
	providerResolver ProviderResolver
	approverResolver *ApproverResolver
	notifier         Notifier
}

func NewService(client *ent.Client, providerResolver ProviderResolver, approverResolver *ApproverResolver, notifier Notifier) *Service {
	return &Service{
		client:           client,
		providerResolver: providerResolver,
		approverResolver: approverResolver,
		notifier:         notifier,
	}
}

func (s *Service) Options(ctx context.Context, userID int) (*OptionsResponse, error) {
	user, providerRow, provider, err := s.resolveRequesterAndPrimaryProvider(ctx, userID)
	if err != nil {
		return nil, err
	}
	subscriptions, err := listActiveSubscriptions(ctx, provider, int64(*user.RelayUserID))
	if err != nil {
		return nil, err
	}
	options := make([]SubscriptionOption, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		options = append(options, subscriptionOption(subscription))
	}
	sort.SliceStable(options, func(i, j int) bool {
		if options[i].GroupName != options[j].GroupName {
			return options[i].GroupName < options[j].GroupName
		}
		return options[i].GroupID < options[j].GroupID
	})
	return &OptionsResponse{ProviderID: providerRow.ID, Groups: options}, nil
}

func (s *Service) CreateRequest(ctx context.Context, input CreateRequestInput) (*ent.QuotaResetRequest, error) {
	input.GroupID = strings.TrimSpace(input.GroupID)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" {
		return nil, ErrReasonRequired
	}
	requester, providerRow, provider, err := s.resolveRequesterAndPrimaryProvider(ctx, input.RequesterUserID)
	if err != nil {
		return nil, err
	}
	subscriptions, err := listActiveSubscriptions(ctx, provider, int64(*requester.RelayUserID))
	if err != nil {
		return nil, err
	}
	subscription, err := findSubscription(subscriptions, input.GroupID)
	if err != nil {
		return nil, err
	}
	if err := activeRequestExists(ctx, s.client, requester.ID, providerRow.ID, input.GroupID); err != nil {
		return nil, err
	}
	resolution := &ApproverResolution{}
	if s.approverResolver != nil {
		resolution, err = s.approverResolver.Resolve(ctx, requester.ID)
		if err != nil {
			return nil, err
		}
	}
	pathMaps, err := departmentPathEvidenceToMaps(resolution.Paths)
	if err != nil {
		return nil, err
	}
	relayUserID := int64(*requester.RelayUserID)
	req, err := s.client.QuotaResetRequest.Create().
		SetRequesterUserID(requester.ID).
		SetRequesterRelayUserID(relayUserID).
		SetProviderID(providerRow.ID).
		SetGroupID(input.GroupID).
		SetGroupName(subscriptionGroupName(subscription)).
		SetGroupPlatform(subscriptionGroupPlatform(subscription)).
		SetReason(input.Reason).
		SetResolvedApproverUserIds(resolution.ApproverUserIDs).
		SetMatchedDepartmentPaths(pathMaps).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create quota reset request: %w", err)
	}
	if err := s.writeEvent(ctx, req.ID, &requester.ID, quotaresetrequestevent.EventTypeCreated, map[string]any{
		"group_id": input.GroupID,
	}, ""); err != nil {
		return nil, err
	}
	if err := s.writeEvent(ctx, req.ID, nil, quotaresetrequestevent.EventTypeApproverResolved, map[string]any{
		"approver_user_ids": resolution.ApproverUserIDs,
		"path_count":        len(resolution.Paths),
	}, ""); err != nil {
		return nil, err
	}
	_ = s.notify(ctx, "quota_reset_request_created", req)
	return req, nil
}

func (s *Service) Cancel(ctx context.Context, actorUserID, requestID int) (*ent.QuotaResetRequest, error) {
	req, err := s.client.QuotaResetRequest.Get(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if req.RequesterUserID != actorUserID {
		return nil, ErrNotApprover
	}
	if req.Status != quotaresetrequest.StatusPending {
		return nil, ErrInvalidStatus
	}
	updated, err := s.client.QuotaResetRequest.UpdateOneID(requestID).
		Where(quotaresetrequest.StatusEQ(quotaresetrequest.StatusPending)).
		SetStatus(quotaresetrequest.StatusCancelled).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("cancel quota reset request: %w", err)
	}
	if err := s.writeEvent(ctx, requestID, &actorUserID, quotaresetrequestevent.EventTypeCancelled, nil, ""); err != nil {
		return nil, err
	}
	_ = s.notify(ctx, "quota_reset_request_cancelled", updated)
	return updated, nil
}

func (s *Service) Approve(ctx context.Context, input DecisionInput) (*ent.QuotaResetRequest, error) {
	req, err := s.requireDecisionAllowed(ctx, input, quotaresetrequest.StatusPending)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	updated, err := s.client.QuotaResetRequest.UpdateOneID(input.RequestID).
		Where(quotaresetrequest.StatusEQ(quotaresetrequest.StatusPending)).
		SetApprovedByUserID(input.ActorUserID).
		SetDecisionReason(strings.TrimSpace(input.DecisionReason)).
		SetDecidedAt(now).
		SetStatus(quotaresetrequest.StatusApprovedResetting).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("approve quota reset request: %w", err)
	}
	if err := s.writeEvent(ctx, req.ID, &input.ActorUserID, quotaresetrequestevent.EventTypeApproved, map[string]any{
		"admin": input.Admin,
	}, ""); err != nil {
		return nil, err
	}
	return s.executeReset(ctx, updated.ID, input.ActorUserID, false)
}

func (s *Service) Reject(ctx context.Context, input DecisionInput) (*ent.QuotaResetRequest, error) {
	input.DecisionReason = strings.TrimSpace(input.DecisionReason)
	if input.DecisionReason == "" {
		return nil, ErrDecisionRequired
	}
	req, err := s.requireDecisionAllowed(ctx, input, quotaresetrequest.StatusPending)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	updated, err := s.client.QuotaResetRequest.UpdateOneID(input.RequestID).
		Where(quotaresetrequest.StatusEQ(quotaresetrequest.StatusPending)).
		SetRejectedByUserID(input.ActorUserID).
		SetDecisionReason(input.DecisionReason).
		SetDecidedAt(now).
		SetStatus(quotaresetrequest.StatusRejected).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("reject quota reset request: %w", err)
	}
	if err := s.writeEvent(ctx, req.ID, &input.ActorUserID, quotaresetrequestevent.EventTypeRejected, map[string]any{
		"admin": input.Admin,
	}, ""); err != nil {
		return nil, err
	}
	_ = s.notify(ctx, "quota_reset_request_rejected", updated)
	return updated, nil
}

func (s *Service) RetryReset(ctx context.Context, input DecisionInput) (*ent.QuotaResetRequest, error) {
	_, err := s.requireDecisionAllowed(ctx, input, quotaresetrequest.StatusApprovedResetFailed)
	if err != nil {
		return nil, err
	}
	if err := s.writeEvent(ctx, input.RequestID, &input.ActorUserID, quotaresetrequestevent.EventTypeResetRetried, map[string]any{
		"admin": input.Admin,
	}, ""); err != nil {
		return nil, err
	}
	return s.executeReset(ctx, input.RequestID, input.ActorUserID, true)
}

func (s *Service) ListMine(ctx context.Context, actorUserID int, params ListParams) (*RequestListResponse, error) {
	return s.list(ctx, params, func(q *ent.QuotaResetRequestQuery) *ent.QuotaResetRequestQuery {
		return q.Where(quotaresetrequest.RequesterUserIDEQ(actorUserID))
	})
}

func (s *Service) ListApprovals(ctx context.Context, actorUserID int, params ListParams) (*RequestListResponse, error) {
	return s.list(ctx, params, func(q *ent.QuotaResetRequestQuery) *ent.QuotaResetRequestQuery {
		return q.Where(func(selector *sql.Selector) {
			selector.Where(sql.ExprP(fmt.Sprintf("%s::jsonb @> ?", selector.C(quotaresetrequest.FieldResolvedApproverUserIds)), fmt.Sprintf("[%d]", actorUserID)))
		})
	})
}

func (s *Service) ListAdmin(ctx context.Context, params ListParams) (*RequestListResponse, error) {
	return s.list(ctx, params, nil)
}

func (s *Service) resolveRequesterAndPrimaryProvider(ctx context.Context, userID int) (*ent.User, *ent.RelayProvider, relay.Provider, error) {
	if s == nil || s.client == nil || s.providerResolver == nil {
		return nil, nil, nil, ErrProviderUnsupported
	}
	user, err := s.client.User.Get(ctx, userID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("get requester: %w", err)
	}
	if user.RelayUserID == nil {
		return nil, nil, nil, ErrNoRelayMapping
	}
	providerRow, provider, err := s.resolvePrimaryProvider(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	return user, providerRow, provider, nil
}

func (s *Service) resolvePrimaryProvider(ctx context.Context) (*ent.RelayProvider, relay.Provider, error) {
	providerRow, err := s.client.RelayProvider.Query().
		Where(relayprovider.Enabled(true), relayprovider.IsPrimaryEQ(true)).
		Only(ctx)
	if ent.IsNotFound(err) {
		providerRow, err = s.client.RelayProvider.Get(ctx, 1)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("resolve primary provider: %w", err)
	}
	provider, err := s.providerResolver.Resolve(ctx, providerRow.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve provider: %w", err)
	}
	return providerRow, provider, nil
}

func listActiveSubscriptions(ctx context.Context, provider relay.Provider, relayUserID int64) ([]relay.UserSubscription, error) {
	lister, ok := provider.(relay.UserSubscriptionLister)
	if !ok {
		return nil, ErrProviderUnsupported
	}
	subscriptions, err := lister.ListUserSubscriptions(ctx, relayUserID)
	if err != nil {
		return nil, fmt.Errorf("list user subscriptions: %w", err)
	}
	active := make([]relay.UserSubscription, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		if strings.EqualFold(strings.TrimSpace(subscription.Status), "active") {
			active = append(active, subscription)
		}
	}
	return active, nil
}

func findSubscription(subscriptions []relay.UserSubscription, groupID string) (relay.UserSubscription, error) {
	parsedGroupID, err := strconv.ParseInt(strings.TrimSpace(groupID), 10, 64)
	if err != nil || parsedGroupID <= 0 {
		return relay.UserSubscription{}, ErrInactiveSubscription
	}
	for _, subscription := range subscriptions {
		if subscription.GroupID == parsedGroupID {
			return subscription, nil
		}
	}
	return relay.UserSubscription{}, ErrInactiveSubscription
}

func activeRequestExists(ctx context.Context, client *ent.Client, requesterUserID int, providerID int, groupID string) error {
	exists, err := client.QuotaResetRequest.Query().
		Where(
			quotaresetrequest.RequesterUserIDEQ(requesterUserID),
			quotaresetrequest.ProviderIDEQ(providerID),
			quotaresetrequest.GroupIDEQ(groupID),
			quotaresetrequest.StatusIn(
				quotaresetrequest.StatusPending,
				quotaresetrequest.StatusApprovedResetting,
				quotaresetrequest.StatusApprovedResetFailed,
			),
		).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check active quota reset request: %w", err)
	}
	if exists {
		return ErrActiveRequestExists
	}
	return nil
}

func (s *Service) requireDecisionAllowed(ctx context.Context, input DecisionInput, requiredStatus quotaresetrequest.Status) (*ent.QuotaResetRequest, error) {
	req, err := s.client.QuotaResetRequest.Get(ctx, input.RequestID)
	if err != nil {
		return nil, err
	}
	if req.Status != requiredStatus {
		return nil, ErrInvalidStatus
	}
	if req.RequesterUserID == input.ActorUserID {
		return nil, ErrSelfApprovalForbidden
	}
	if input.Admin {
		return req, nil
	}
	if !isResolvedApprover(req, input.ActorUserID) {
		return nil, ErrNotApprover
	}
	return req, nil
}

func isResolvedApprover(request *ent.QuotaResetRequest, actorUserID int) bool {
	if request == nil {
		return false
	}
	for _, id := range request.ResolvedApproverUserIds {
		if id == actorUserID {
			return true
		}
	}
	return false
}

func (s *Service) executeReset(ctx context.Context, requestID int, actorUserID int, retry bool) (*ent.QuotaResetRequest, error) {
	req, err := s.client.QuotaResetRequest.Get(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if req.Status != quotaresetrequest.StatusApprovedResetting && req.Status != quotaresetrequest.StatusApprovedResetFailed {
		return nil, ErrInvalidStatus
	}
	groupID, err := strconv.ParseInt(req.GroupID, 10, 64)
	if err != nil || groupID <= 0 {
		return nil, ErrInactiveSubscription
	}
	now := time.Now()
	running, err := s.client.QuotaResetRequest.UpdateOneID(requestID).
		SetStatus(quotaresetrequest.StatusApprovedResetting).
		SetResetError("").
		SetResetStartedAt(now).
		ClearResetCompletedAt().
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("mark reset started: %w", err)
	}
	if err := s.writeEvent(ctx, requestID, &actorUserID, quotaresetrequestevent.EventTypeResetStarted, map[string]any{
		"retry": retry,
	}, ""); err != nil {
		return nil, err
	}
	provider, err := s.providerResolver.Resolve(ctx, running.ProviderID)
	if err != nil {
		return nil, err
	}
	resetter, ok := provider.(relay.UserSubscriptionQuotaResetter)
	if !ok {
		return nil, ErrProviderUnsupported
	}
	if err := resetter.ResetSubscriptionQuotaForUser(ctx, running.RequesterRelayUserID, groupID); err != nil {
		failed, saveErr := s.client.QuotaResetRequest.UpdateOneID(requestID).
			SetStatus(quotaresetrequest.StatusApprovedResetFailed).
			SetResetError(err.Error()).
			SetResetCompletedAt(time.Now()).
			Save(ctx)
		if saveErr != nil {
			return nil, fmt.Errorf("store reset failure: %w", saveErr)
		}
		_ = s.writeEvent(ctx, requestID, &actorUserID, quotaresetrequestevent.EventTypeResetFailed, nil, err.Error())
		_ = s.notify(ctx, "quota_reset_request_reset_failed", failed)
		return failed, nil
	}
	succeeded, err := s.client.QuotaResetRequest.UpdateOneID(requestID).
		SetStatus(quotaresetrequest.StatusApprovedResetSucceeded).
		SetResetError("").
		SetResetCompletedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("store reset success: %w", err)
	}
	if err := s.writeEvent(ctx, requestID, &actorUserID, quotaresetrequestevent.EventTypeResetSucceeded, nil, ""); err != nil {
		return nil, err
	}
	_ = s.notify(ctx, "quota_reset_request_reset_succeeded", succeeded)
	return succeeded, nil
}

func (s *Service) writeEvent(ctx context.Context, requestID int, actorUserID *int, eventType quotaresetrequestevent.EventType, metadata map[string]any, errorMessage string) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	create := s.client.QuotaResetRequestEvent.Create().
		SetRequestID(requestID).
		SetEventType(eventType).
		SetMetadata(metadata).
		SetErrorMessage(strings.TrimSpace(errorMessage))
	if actorUserID != nil {
		create.SetActorUserID(*actorUserID)
	}
	if _, err := create.Save(ctx); err != nil {
		return fmt.Errorf("write quota reset event: %w", err)
	}
	return nil
}

func (s *Service) notify(ctx context.Context, event string, req *ent.QuotaResetRequest) error {
	if s.notifier == nil || req == nil {
		return nil
	}
	if err := s.notifier.NotifyRequestEvent(ctx, event, req); err != nil {
		_ = s.writeEvent(ctx, req.ID, nil, quotaresetrequestevent.EventTypeNotificationFailed, map[string]any{
			"event": event,
		}, err.Error())
		return err
	}
	return s.writeEvent(ctx, req.ID, nil, quotaresetrequestevent.EventTypeNotificationSent, map[string]any{
		"event": event,
	}, "")
}

func (s *Service) list(ctx context.Context, params ListParams, filter func(*ent.QuotaResetRequestQuery) *ent.QuotaResetRequestQuery) (*RequestListResponse, error) {
	page, pageSize := normalizePage(params.Page, params.PageSize)
	query := s.client.QuotaResetRequest.Query()
	if filter != nil {
		query = filter(query)
	}
	status := strings.TrimSpace(params.Status)
	if status != "" {
		query = query.Where(quotaresetrequest.StatusEQ(quotaresetrequest.Status(status)))
	}
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count quota reset requests: %w", err)
	}
	requests, err := query.
		Order(ent.Desc(quotaresetrequest.FieldCreatedAt)).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list quota reset requests: %w", err)
	}
	items, err := s.summaries(ctx, requests)
	if err != nil {
		return nil, err
	}
	return &RequestListResponse{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = defaultPage
	}
	if pageSize < 1 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}

func (s *Service) summaries(ctx context.Context, requests []*ent.QuotaResetRequest) ([]RequestSummary, error) {
	if len(requests) == 0 {
		return []RequestSummary{}, nil
	}
	userIDs := make([]int, 0, len(requests))
	seenUsers := map[int]struct{}{}
	requestIDs := make([]int, 0, len(requests))
	for _, req := range requests {
		requestIDs = append(requestIDs, req.ID)
		if _, ok := seenUsers[req.RequesterUserID]; !ok {
			seenUsers[req.RequesterUserID] = struct{}{}
			userIDs = append(userIDs, req.RequesterUserID)
		}
	}
	users, err := s.client.User.Query().Where(entuser.IDIn(userIDs...)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load request users: %w", err)
	}
	usersByID := make(map[int]*ent.User, len(users))
	for _, user := range users {
		usersByID[user.ID] = user
	}
	events, err := s.client.QuotaResetRequestEvent.Query().
		Where(quotaresetrequestevent.RequestIDIn(requestIDs...)).
		Order(ent.Asc(quotaresetrequestevent.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load request events: %w", err)
	}
	eventsByRequest := make(map[int][]RequestEvent)
	for _, event := range events {
		eventsByRequest[event.RequestID] = append(eventsByRequest[event.RequestID], requestEventSummary(event))
	}
	items := make([]RequestSummary, 0, len(requests))
	for _, req := range requests {
		item, err := requestSummary(req, usersByID[req.RequesterUserID])
		if err != nil {
			return nil, err
		}
		item.Events = eventsByRequest[req.ID]
		items = append(items, item)
	}
	return items, nil
}

func requestSummary(req *ent.QuotaResetRequest, requester *ent.User) (RequestSummary, error) {
	paths, err := mapsToDepartmentPathEvidence(req.MatchedDepartmentPaths)
	if err != nil {
		return RequestSummary{}, err
	}
	item := RequestSummary{
		ID:                      req.ID,
		RequesterUserID:         req.RequesterUserID,
		ProviderID:              req.ProviderID,
		GroupID:                 req.GroupID,
		GroupName:               req.GroupName,
		GroupPlatform:           req.GroupPlatform,
		Reason:                  req.Reason,
		Status:                  req.Status.String(),
		ResolvedApproverUserIDs: req.ResolvedApproverUserIds,
		MatchedDepartmentPaths:  paths,
		ApprovedByUserID:        req.ApprovedByUserID,
		RejectedByUserID:        req.RejectedByUserID,
		DecisionReason:          req.DecisionReason,
		ResetError:              req.ResetError,
		CreatedAt:               req.CreatedAt,
		UpdatedAt:               req.UpdatedAt,
	}
	if requester != nil {
		item.RequesterDisplayName = requester.Username
		item.RequesterEmail = requester.Email
	}
	return item, nil
}

func requestEventSummary(event *ent.QuotaResetRequestEvent) RequestEvent {
	return RequestEvent{
		ID:           event.ID,
		RequestID:    event.RequestID,
		ActorUserID:  event.ActorUserID,
		EventType:    event.EventType.String(),
		Metadata:     event.Metadata,
		ErrorMessage: event.ErrorMessage,
		CreatedAt:    event.CreatedAt,
	}
}

func departmentPathEvidenceToMaps(paths []DepartmentPathEvidence) ([]map[string]any, error) {
	if len(paths) == 0 {
		return []map[string]any{}, nil
	}
	raw, err := json.Marshal(paths)
	if err != nil {
		return nil, fmt.Errorf("marshal department path evidence: %w", err)
	}
	var result []map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("unmarshal department path evidence: %w", err)
	}
	return result, nil
}

func mapsToDepartmentPathEvidence(rawPaths []map[string]any) ([]DepartmentPathEvidence, error) {
	if len(rawPaths) == 0 {
		return []DepartmentPathEvidence{}, nil
	}
	raw, err := json.Marshal(rawPaths)
	if err != nil {
		return nil, fmt.Errorf("marshal stored department path evidence: %w", err)
	}
	var result []DepartmentPathEvidence
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("unmarshal stored department path evidence: %w", err)
	}
	return result, nil
}

func subscriptionOption(subscription relay.UserSubscription) SubscriptionOption {
	option := SubscriptionOption{
		GroupID:         strconv.FormatInt(subscription.GroupID, 10),
		GroupName:       subscriptionGroupName(subscription),
		Platform:        subscriptionGroupPlatform(subscription),
		DailyUsageUSD:   subscription.DailyUsageUSD,
		WeeklyUsageUSD:  subscription.WeeklyUsageUSD,
		MonthlyUsageUSD: subscription.MonthlyUsageUSD,
	}
	if subscription.Group != nil {
		option.DailyLimitUSD = subscription.Group.DailyLimitUSD
		option.WeeklyLimitUSD = subscription.Group.WeeklyLimitUSD
		option.MonthlyLimitUSD = subscription.Group.MonthlyLimitUSD
	}
	return option
}

func subscriptionGroupName(subscription relay.UserSubscription) string {
	if subscription.Group != nil && strings.TrimSpace(subscription.Group.Name) != "" {
		return strings.TrimSpace(subscription.Group.Name)
	}
	return strconv.FormatInt(subscription.GroupID, 10)
}

func subscriptionGroupPlatform(subscription relay.UserSubscription) string {
	if subscription.Group != nil {
		return strings.TrimSpace(subscription.Group.Platform)
	}
	return ""
}
