package quotareset

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"

	"github.com/ai-efficiency/backend/ent"
	entcredential "github.com/ai-efficiency/backend/ent/credential"
	"github.com/ai-efficiency/backend/ent/quotaresetapproverconfig"
	"github.com/ai-efficiency/backend/ent/quotaresetnotificationsetting"
	"github.com/ai-efficiency/backend/ent/quotaresetrequest"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestevent"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/ent/systemsetting"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/directorysync"
	"github.com/ai-efficiency/backend/internal/relay"
)

const (
	defaultPage     = 1
	defaultPageSize = 20
	maxPageSize     = 100

	ApproverConfigSaveModeReplaceDepartments = "replace_departments"
	ApproverConfigSaveModeReplaceAll         = "replace_all"

	quotaResetNotificationSettingsLockKey = "quota_reset_notification_settings"
	defaultResetExecutionTimeout          = 30 * time.Second
)

type Service struct {
	client                *ent.Client
	providerResolver      ProviderResolver
	notifier              Notifier
	resetExecutionTimeout time.Duration
}

func NewService(client *ent.Client, providerResolver ProviderResolver, _ *ApproverResolver, notifier Notifier) *Service {
	return &Service{
		client:                client,
		providerResolver:      providerResolver,
		notifier:              notifier,
		resetExecutionTimeout: defaultResetExecutionTimeout,
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
	workflow, paths, err := s.resolveWorkflowSnapshot(ctx, requester)
	if err != nil {
		return nil, err
	}
	return s.createWorkflowRequest(ctx, requester, providerRow, subscription, input, workflow, paths)
}

func (s *Service) Cancel(ctx context.Context, actorUserID, requestID int) (*ent.QuotaResetRequest, error) {
	req, err := s.client.QuotaResetRequest.Get(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if req.RequesterUserID != actorUserID {
		return nil, ErrNotApprover
	}
	if req.Status != quotaresetrequest.StatusPending && req.Status != quotaresetrequest.StatusWorkflowPending {
		return nil, ErrInvalidStatus
	}
	updated, err := s.client.QuotaResetRequest.UpdateOneID(requestID).
		Where(quotaresetrequest.StatusEQ(req.Status)).
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
	request, err := s.loadDecisionRequest(ctx, &input)
	if err != nil {
		return nil, err
	}
	if request.WorkflowVersion == workflowVersionV2 {
		return s.decideWorkflowRequest(ctx, request, input, true)
	}
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
		return s.storeResetFailure(ctx, updated.ID, input.ActorUserID, err)
	}
	return s.executeReset(ctx, updated.ID, input.ActorUserID, false, input.Admin)
}

func (s *Service) Reject(ctx context.Context, input DecisionInput) (*ent.QuotaResetRequest, error) {
	request, err := s.loadDecisionRequest(ctx, &input)
	if err != nil {
		return nil, err
	}
	if request.WorkflowVersion == workflowVersionV2 {
		return s.decideWorkflowRequest(ctx, request, input, false)
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

func (s *Service) loadDecisionRequest(ctx context.Context, input *DecisionInput) (*ent.QuotaResetRequest, error) {
	input.DecisionReason = strings.TrimSpace(input.DecisionReason)
	if input.DecisionReason == "" {
		return nil, ErrDecisionRequired
	}
	request, err := s.client.QuotaResetRequest.Get(ctx, input.RequestID)
	if err != nil {
		return nil, err
	}
	if request.WorkflowVersion != 1 && request.WorkflowVersion != workflowVersionV2 {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrInvalidWorkflow, request.WorkflowVersion)
	}
	return request, nil
}

func (s *Service) RetryReset(ctx context.Context, input DecisionInput) (*ent.QuotaResetRequest, error) {
	request, err := s.client.QuotaResetRequest.Get(ctx, input.RequestID)
	if err != nil {
		return nil, err
	}
	if request.Status != quotaresetrequest.StatusApprovedResetFailed {
		return nil, ErrInvalidStatus
	}
	if request.WorkflowVersion != 1 && request.WorkflowVersion != workflowVersionV2 {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrInvalidWorkflow, request.WorkflowVersion)
	}
	if !input.Admin {
		approvedV2 := request.WorkflowVersion == workflowVersionV2 && request.ApprovedByUserID != nil && *request.ApprovedByUserID == input.ActorUserID
		if !approvedV2 && (request.WorkflowVersion != 1 || !isResolvedApprover(request, input.ActorUserID)) {
			return nil, ErrNotApprover
		}
	}
	return s.executeReset(ctx, input.RequestID, input.ActorUserID, true, input.Admin)
}

func (s *Service) ListMine(ctx context.Context, actorUserID int, params ListParams) (*RequestListResponse, error) {
	return s.list(ctx, params, func(q *ent.QuotaResetRequestQuery) *ent.QuotaResetRequestQuery {
		return q.Where(quotaresetrequest.RequesterUserIDEQ(actorUserID))
	})
}

func (s *Service) ListApprovals(ctx context.Context, actorUserID int, params ListParams) (*RequestListResponse, error) {
	decisionNeedle := fmt.Sprintf(`{"steps":[{"decision":{"actor_user_id":%d}}]}`, actorUserID)
	return s.list(ctx, params, func(q *ent.QuotaResetRequestQuery) *ent.QuotaResetRequestQuery {
		return q.Where(func(selector *sql.Selector) {
			selector.Where(sql.Or(
				sql.P(func(builder *sql.Builder) {
					builder.WriteString(selector.C(quotaresetrequest.FieldResolvedApproverUserIds)).
						WriteString("::jsonb @> ").
						Arg(fmt.Sprintf("[%d]", actorUserID))
				}),
				sql.P(func(builder *sql.Builder) {
					builder.WriteString(selector.C(quotaresetrequest.FieldWorkflow)).
						WriteString("::jsonb @> ").
						Arg(decisionNeedle)
				}),
			))
		})
	})
}

func (s *Service) ListAdmin(ctx context.Context, params ListParams) (*RequestListResponse, error) {
	return s.list(ctx, params, nil)
}

func (s *Service) ListApproverConfigs(ctx context.Context) (*ApproverConfigListResponse, error) {
	sourceID, ok, err := directorysync.CurrentSourceID(ctx, s.client)
	if err != nil {
		return nil, err
	}
	if !ok {
		return &ApproverConfigListResponse{Items: []ApproverConfig{}}, nil
	}
	rows, err := s.client.QuotaResetApproverConfig.Query().
		Where(quotaresetapproverconfig.DirectorySourceIDEQ(sourceID)).
		Order(ent.Asc(quotaresetapproverconfig.FieldDepartmentDisplayPath), ent.Asc(quotaresetapproverconfig.FieldApproverUserID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list quota reset approver configs: %w", err)
	}
	response, err := s.approverConfigResponse(ctx, rows)
	if err != nil {
		return nil, err
	}
	response.CurrentDirectorySourceID = &sourceID
	return response, nil
}

func (s *Service) ListApproverCandidates(ctx context.Context, sourceID int, departmentExternalID string) (*ApproverCandidateListResponse, error) {
	departmentExternalID = strings.TrimSpace(departmentExternalID)
	if sourceID <= 0 || departmentExternalID == "" {
		return nil, fmt.Errorf("%w: source_id and department_external_id are required", ErrInvalidApproverConfig)
	}
	candidates, unmatched, err := s.approverCandidates(ctx, sourceID, departmentExternalID)
	if err != nil {
		return nil, err
	}
	return &ApproverCandidateListResponse{Items: candidates, UnmatchedRepresentatives: unmatched}, nil
}

func (s *Service) SaveApproverConfigs(ctx context.Context, input SaveApproverConfigsInput) (*ApproverConfigListResponse, error) {
	sourceID, ok, err := directorysync.CurrentSourceID(ctx, s.client)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrDirectoryUnavailable
	}
	items := normalizeApproverConfigInputs(input.Items)
	if err := s.validateApproverConfigs(ctx, sourceID, items); err != nil {
		return nil, err
	}
	replaceAll := input.Mode == ApproverConfigSaveModeReplaceAll
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin quota reset approver config tx: %w", err)
	}
	defer tx.Rollback()
	switch {
	case replaceAll:
		if _, err := tx.QuotaResetApproverConfig.Delete().
			Where(quotaresetapproverconfig.DirectorySourceIDEQ(sourceID)).
			Exec(ctx); err != nil {
			return nil, fmt.Errorf("delete quota reset approver configs: %w", err)
		}
	default:
		departmentIDs := approverConfigDepartmentIDs(items)
		if len(departmentIDs) > 0 {
			if _, err := tx.QuotaResetApproverConfig.Delete().
				Where(
					quotaresetapproverconfig.DirectorySourceIDEQ(sourceID),
					quotaresetapproverconfig.DepartmentExternalIDIn(departmentIDs...),
				).
				Exec(ctx); err != nil {
				return nil, fmt.Errorf("delete quota reset approver configs: %w", err)
			}
		}
	}
	for _, item := range items {
		if _, err := tx.QuotaResetApproverConfig.Create().
			SetDirectorySourceID(sourceID).
			SetDepartmentExternalID(item.DepartmentExternalID).
			SetDepartmentDisplayPath(item.DepartmentDisplayPath).
			SetApproverUserID(item.ApproverUserID).
			SetEnabled(item.Enabled).
			SetCreatedByUserID(input.ActorUserID).
			SetUpdatedByUserID(input.ActorUserID).
			Save(ctx); err != nil {
			return nil, fmt.Errorf("create quota reset approver config: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit quota reset approver configs: %w", err)
	}
	return s.ListApproverConfigs(ctx)
}

func (s *Service) GetNotificationSettings(ctx context.Context) (*NotificationSettings, error) {
	row, err := s.client.QuotaResetNotificationSetting.Query().
		Order(ent.Asc(quotaresetnotificationsetting.FieldID)).
		First(ctx)
	if ent.IsNotFound(err) {
		return notificationSettingsResponse(nil), nil
	}
	if err != nil {
		return nil, fmt.Errorf("load quota reset notification settings: %w", err)
	}
	row, err = s.backfillLegacyNotificationChannel(ctx, row)
	if err != nil {
		return nil, err
	}
	return notificationSettingsResponse(row), nil
}

func (s *Service) UpdateNotificationSettings(ctx context.Context, input UpdateNotificationSettingsInput) (*NotificationSettings, error) {
	input.URL = strings.TrimSpace(input.URL)
	input.Channel = strings.TrimSpace(input.Channel)
	if input.Channel == "" {
		input.Channel = quotaresetnotificationsetting.ChannelGenericWebhook.String()
	}
	channel := quotaresetnotificationsetting.Channel(input.Channel)
	if channel != quotaresetnotificationsetting.ChannelGenericWebhook && channel != quotaresetnotificationsetting.ChannelWecomGroupRobot {
		return nil, fmt.Errorf("%w: invalid notification channel", ErrInvalidNotification)
	}
	input.AuthType = strings.TrimSpace(input.AuthType)
	if input.AuthType == "" {
		input.AuthType = quotaresetnotificationsetting.AuthTypeNone.String()
	}
	authType := quotaresetnotificationsetting.AuthType(input.AuthType)
	if err := quotaresetnotificationsetting.AuthTypeValidator(authType); err != nil {
		return nil, fmt.Errorf("invalid notification auth type: %w", err)
	}
	if err := s.validateNotificationSettings(ctx, input, authType); err != nil {
		return nil, err
	}
	if authType == quotaresetnotificationsetting.AuthTypeNone {
		input.CredentialID = nil
	}
	if err := s.ensureNotificationSettingsLockRow(ctx); err != nil {
		return nil, err
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin quota reset notification settings tx: %w", err)
	}
	defer tx.Rollback()
	if err := lockNotificationSettings(ctx, tx); err != nil {
		return nil, err
	}
	rows, err := tx.QuotaResetNotificationSetting.Query().
		Order(ent.Asc(quotaresetnotificationsetting.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load quota reset notification settings: %w", err)
	}
	var row *ent.QuotaResetNotificationSetting
	if len(rows) == 0 {
		create := tx.QuotaResetNotificationSetting.Create().
			SetEnabled(input.Enabled).
			SetChannel(channel).
			SetURL(input.URL).
			SetAuthType(authType).
			SetCreatedByUserID(input.ActorUserID).
			SetUpdatedByUserID(input.ActorUserID)
		if input.CredentialID != nil {
			create.SetCredentialID(*input.CredentialID)
		}
		row, err = create.Save(ctx)
	} else {
		update := tx.QuotaResetNotificationSetting.UpdateOneID(rows[0].ID).
			SetEnabled(input.Enabled).
			SetChannel(channel).
			SetURL(input.URL).
			SetAuthType(authType).
			SetUpdatedByUserID(input.ActorUserID)
		if input.CredentialID != nil {
			update.SetCredentialID(*input.CredentialID)
		} else {
			update.ClearCredentialID()
		}
		row, err = update.Save(ctx)
		if err == nil && len(rows) > 1 {
			duplicateIDs := make([]int, 0, len(rows)-1)
			for _, duplicate := range rows[1:] {
				duplicateIDs = append(duplicateIDs, duplicate.ID)
			}
			if _, err := tx.QuotaResetNotificationSetting.Delete().
				Where(quotaresetnotificationsetting.IDIn(duplicateIDs...)).
				Exec(ctx); err != nil {
				return nil, fmt.Errorf("delete duplicate quota reset notification settings: %w", err)
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("save quota reset notification settings: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit quota reset notification settings: %w", err)
	}
	return notificationSettingsResponse(row), nil
}

func (s *Service) TestNotificationSettings(ctx context.Context, actorUserID int) error {
	if s.notifier == nil {
		return nil
	}
	setting, err := s.client.QuotaResetNotificationSetting.Query().
		Order(ent.Asc(quotaresetnotificationsetting.FieldID)).
		First(ctx)
	if ent.IsNotFound(err) {
		return fmt.Errorf("%w: enabled webhook url is required before sending test notification", ErrInvalidNotification)
	}
	if err != nil {
		return fmt.Errorf("load quota reset notification settings: %w", err)
	}
	if !setting.Enabled || strings.TrimSpace(setting.URL) == "" {
		return fmt.Errorf("%w: enabled webhook url is required before sending test notification", ErrInvalidNotification)
	}
	workflow := &Workflow{
		Version: workflowVersionV2,
		Requester: WorkflowPerson{
			UserID:          actorUserID,
			DisplayName:     "Alice Example",
			Email:           "alice@example.com",
			DepartmentPaths: []string{"Company / Group Alpha"},
		},
		Steps: []WorkflowStep{{
			Kind:  WorkflowStepRequesterDepartments,
			Label: "Company / Group Alpha",
			Approvers: []WorkflowApprover{{
				UserID:          actorUserID,
				DisplayName:     "Bob Example",
				Email:           "bob@example.org",
				NotificationIDs: map[string]string{"wecom": "bob"},
			}},
			Status: WorkflowStepActive,
		}},
	}
	rawWorkflow, err := EncodeWorkflow(workflow)
	if err != nil {
		return err
	}
	return s.notifier.NotifyRequestEvent(ctx, "quota_reset_notification_test", &ent.QuotaResetRequest{
		RequesterUserID: actorUserID,
		GroupName:       "Group Alpha",
		Reason:          "Synthetic quota reset notification test",
		WorkflowVersion: workflowVersionV2,
		Workflow:        rawWorkflow,
		Status:          quotaresetrequest.StatusPending,
	})
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
				quotaresetrequest.StatusWorkflowPending,
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

func activeRequestCreateWasDuplicate(ctx context.Context, client *ent.Client, err error, requesterUserID int, providerID int, groupID string) bool {
	if !ent.IsConstraintError(err) {
		return false
	}
	return activeRequestExists(ctx, client, requesterUserID, providerID, groupID) == ErrActiveRequestExists
}

func (s *Service) requireDecisionAllowed(ctx context.Context, input DecisionInput, requiredStatus quotaresetrequest.Status) (*ent.QuotaResetRequest, error) {
	req, err := s.client.QuotaResetRequest.Get(ctx, input.RequestID)
	if err != nil {
		return nil, err
	}
	if req.Status != requiredStatus {
		return nil, ErrInvalidStatus
	}
	if input.Admin {
		return req, nil
	}
	if req.RequesterUserID == input.ActorUserID {
		return nil, ErrSelfApprovalForbidden
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

func (s *Service) executeReset(ctx context.Context, requestID int, actorUserID int, retry bool, admin bool) (*ent.QuotaResetRequest, error) {
	ctx = context.WithoutCancel(ctx)
	req, err := s.client.QuotaResetRequest.Get(ctx, requestID)
	if err != nil {
		return nil, err
	}
	requiredStatus := quotaresetrequest.StatusApprovedResetting
	if retry {
		requiredStatus = quotaresetrequest.StatusApprovedResetFailed
	}
	if req.Status != requiredStatus {
		return nil, ErrInvalidStatus
	}
	groupID, err := strconv.ParseInt(req.GroupID, 10, 64)
	if err != nil || groupID <= 0 {
		return nil, ErrInactiveSubscription
	}
	now := time.Now()
	running, err := s.client.QuotaResetRequest.UpdateOneID(requestID).
		Where(quotaresetrequest.StatusEQ(requiredStatus)).
		SetStatus(quotaresetrequest.StatusApprovedResetting).
		SetResetError("").
		SetResetStartedAt(now).
		ClearResetCompletedAt().
		Save(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrInvalidStatus
	}
	if err != nil {
		return nil, fmt.Errorf("mark reset started: %w", err)
	}
	if retry {
		if err := s.writeEvent(ctx, requestID, &actorUserID, quotaresetrequestevent.EventTypeResetRetried, map[string]any{
			"admin": admin,
		}, ""); err != nil {
			return s.storeResetFailure(ctx, requestID, actorUserID, err)
		}
	}
	if err := s.writeEvent(ctx, requestID, &actorUserID, quotaresetrequestevent.EventTypeResetStarted, map[string]any{
		"retry": retry,
	}, ""); err != nil {
		return s.storeResetFailure(ctx, requestID, actorUserID, err)
	}
	provider, err := s.providerResolver.Resolve(ctx, running.ProviderID)
	if err != nil {
		return s.storeResetFailure(ctx, requestID, actorUserID, err)
	}
	resetter, ok := provider.(relay.UserSubscriptionQuotaResetter)
	if !ok {
		return s.storeResetFailure(ctx, requestID, actorUserID, ErrProviderUnsupported)
	}
	resetCtx, cancelReset := context.WithTimeout(ctx, s.resetExecutionTimeout)
	defer cancelReset()
	if err := resetter.ResetSubscriptionQuotaForUser(resetCtx, running.RequesterRelayUserID, groupID); err != nil {
		return s.storeResetFailure(ctx, requestID, actorUserID, err)
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

func (s *Service) storeResetFailure(ctx context.Context, requestID int, actorUserID int, resetErr error) (*ent.QuotaResetRequest, error) {
	errorMessage := ""
	if resetErr != nil {
		errorMessage = resetErr.Error()
	}
	failed, saveErr := s.client.QuotaResetRequest.UpdateOneID(requestID).
		SetStatus(quotaresetrequest.StatusApprovedResetFailed).
		SetResetError(errorMessage).
		SetResetCompletedAt(time.Now()).
		Save(ctx)
	if saveErr != nil {
		return nil, fmt.Errorf("store reset failure: %w", saveErr)
	}
	_ = s.writeEvent(ctx, requestID, &actorUserID, quotaresetrequestevent.EventTypeResetFailed, nil, errorMessage)
	_ = s.notify(ctx, "quota_reset_request_reset_failed", failed)
	return failed, nil
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
	if status == quotaresetrequest.StatusPending.String() {
		query = query.Where(quotaresetrequest.StatusIn(quotaresetrequest.StatusPending, quotaresetrequest.StatusWorkflowPending))
	} else if status != "" {
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
		Status:                  publicQuotaResetStatus(req.Status),
		WorkflowVersion:         req.WorkflowVersion,
		ResolvedApproverUserIDs: req.ResolvedApproverUserIds,
		MatchedDepartmentPaths:  paths,
		ApprovedByUserID:        req.ApprovedByUserID,
		RejectedByUserID:        req.RejectedByUserID,
		DecisionReason:          req.DecisionReason,
		ResetError:              req.ResetError,
		CreatedAt:               req.CreatedAt,
		UpdatedAt:               req.UpdatedAt,
	}
	if req.WorkflowVersion == workflowVersionV2 {
		workflow, err := DecodeWorkflow(req.Workflow)
		if err != nil {
			return RequestSummary{}, err
		}
		item.CurrentStep = workflow.CurrentStep
		item.WorkflowSteps = append([]WorkflowStep(nil), workflow.Steps...)
	}
	if requester != nil {
		item.RequesterDisplayName = requester.Username
		item.RequesterEmail = requester.Email
	}
	return item, nil
}

func publicQuotaResetStatus(status quotaresetrequest.Status) string {
	if status == quotaresetrequest.StatusWorkflowPending {
		return quotaresetrequest.StatusPending.String()
	}
	return status.String()
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
	result, err := convertJSON[[]map[string]any](paths)
	if err != nil {
		return nil, fmt.Errorf("normalize department path evidence: %w", err)
	}
	return result, nil
}

func mapsToDepartmentPathEvidence(rawPaths []map[string]any) ([]DepartmentPathEvidence, error) {
	if len(rawPaths) == 0 {
		return []DepartmentPathEvidence{}, nil
	}
	result, err := convertJSON[[]DepartmentPathEvidence](rawPaths)
	if err != nil {
		return nil, fmt.Errorf("decode stored department path evidence: %w", err)
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

func (s *Service) approverCandidates(ctx context.Context, sourceID int, departmentExternalID string) ([]ApproverCandidate, []UnmatchedApproverRepresentative, error) {
	facts, err := s.currentWorkflowDirectoryFacts(ctx, sourceID)
	if err != nil {
		return nil, nil, err
	}
	departmentExternalID = strings.TrimSpace(departmentExternalID)
	representativeIDs := facts.representativesByDept[departmentExternalID]
	mappedRepresentativeIDs := map[string]struct{}{}
	candidates := make([]ApproverCandidate, 0)
	for userID, member := range facts.membersByUserID {
		if _, belongs := facts.departmentIDsByMember[member.ID][departmentExternalID]; !belongs {
			continue
		}
		user := facts.usersByID[userID]
		if !workflowCandidateUsable(user, member) {
			continue
		}
		_, representative := representativeIDs[member.ExternalID]
		if representative {
			mappedRepresentativeIDs[member.ExternalID] = struct{}{}
		}
		candidates = append(candidates, ApproverCandidate{
			UserID:                    user.ID,
			Username:                  user.Username,
			Email:                     user.Email,
			DisplayName:               strings.TrimSpace(member.DisplayName),
			DirectoryMemberExternalID: strings.TrimSpace(member.ExternalID),
			Representative:            representative,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left := quotaResetSortLabel(candidates[i].DisplayName, candidates[i].Username)
		right := quotaResetSortLabel(candidates[j].DisplayName, candidates[j].Username)
		return left < right || (left == right && candidates[i].UserID < candidates[j].UserID)
	})
	unmatched := make([]UnmatchedApproverRepresentative, 0)
	for externalID := range representativeIDs {
		if _, mapped := mappedRepresentativeIDs[externalID]; mapped {
			continue
		}
		member := facts.membersByExternalID[externalID]
		item := UnmatchedApproverRepresentative{DirectoryMemberExternalID: externalID}
		if member != nil {
			item.DisplayName = strings.TrimSpace(member.DisplayName)
			item.Email = strings.TrimSpace(member.EmailNormalized)
		}
		unmatched = append(unmatched, item)
	}
	sort.SliceStable(unmatched, func(i, j int) bool {
		left := quotaResetSortLabel(unmatched[i].DisplayName, unmatched[i].DirectoryMemberExternalID)
		right := quotaResetSortLabel(unmatched[j].DisplayName, unmatched[j].DirectoryMemberExternalID)
		return left < right || (left == right && unmatched[i].DirectoryMemberExternalID < unmatched[j].DirectoryMemberExternalID)
	})
	return candidates, unmatched, nil
}

func quotaResetSortLabel(values ...string) string {
	return strings.ToLower(firstWorkflowValue(values...))
}

func (s *Service) validateApproverConfigs(ctx context.Context, sourceID int, items []ApproverConfigInput) error {
	if len(items) == 0 {
		return nil
	}
	facts, err := s.currentWorkflowDirectoryFacts(ctx, sourceID)
	if err != nil {
		return err
	}
	for _, item := range items {
		departmentID := strings.TrimSpace(item.DepartmentExternalID)
		if departmentID == "" || item.ApproverUserID <= 0 {
			continue
		}
		member := facts.membersByUserID[item.ApproverUserID]
		user := facts.usersByID[item.ApproverUserID]
		belongs := false
		if member != nil {
			_, belongs = facts.departmentIDsByMember[member.ID][departmentID]
		}
		if !belongs || !workflowCandidateUsable(user, member) {
			return fmt.Errorf("%w: approver_user_id %d is not an active member of department %s", ErrInvalidApproverConfig, item.ApproverUserID, departmentID)
		}
	}
	return nil
}

func (s *Service) currentWorkflowDirectoryFacts(ctx context.Context, sourceID int) (*workflowDirectoryFacts, error) {
	currentSourceID, ok, err := directorysync.CurrentSourceID(ctx, s.client)
	if err != nil {
		return nil, fmt.Errorf("current directory source: %w", err)
	}
	if !ok || sourceID != currentSourceID {
		return nil, ErrDirectoryUnavailable
	}
	return NewApproverResolver(s.client).loadWorkflowDirectoryFacts(ctx, sourceID)
}

func representativeExternalIDsByDepartment(departments []*ent.DirectoryDepartment, members []*ent.DirectoryMember) map[string]map[string]struct{} {
	representatives := make(map[string]map[string]struct{}, len(departments))
	add := func(departmentID, representativeExternalID string) {
		departmentID = strings.TrimSpace(departmentID)
		representativeExternalID = strings.TrimSpace(representativeExternalID)
		if departmentID == "" || representativeExternalID == "" {
			return
		}
		if representatives[departmentID] == nil {
			representatives[departmentID] = map[string]struct{}{}
		}
		representatives[departmentID][representativeExternalID] = struct{}{}
	}
	for _, department := range departments {
		if department == nil {
			continue
		}
		for _, representativeExternalID := range quotaResetMetadataStringValues(department.Metadata["representative_external_ids"]) {
			add(department.ExternalID, representativeExternalID)
		}
	}
	for _, member := range members {
		if member == nil {
			continue
		}
		for _, departmentID := range quotaResetMetadataStringValues(member.Metadata["leader_department_ids"]) {
			add(departmentID, member.ExternalID)
		}
	}
	return representatives
}

func quotaResetMetadataStringValues(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []string:
		return compactQuotaResetStrings(typed)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			values = append(values, quotaResetMetadataScalarString(item))
		}
		return compactQuotaResetStrings(values)
	case string:
		return compactQuotaResetStrings(strings.Split(typed, ","))
	default:
		return compactQuotaResetStrings([]string{quotaResetMetadataScalarString(typed)})
	}
}

func quotaResetMetadataScalarString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		if math.Trunc(typed) == typed {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strings.TrimSpace(strconv.FormatFloat(typed, 'f', -1, 64))
	case float32:
		value := float64(typed)
		if math.Trunc(value) == value {
			return strconv.FormatInt(int64(value), 10)
		}
		return strings.TrimSpace(strconv.FormatFloat(value, 'f', -1, 32))
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func compactQuotaResetStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func normalizeApproverConfigInputs(items []ApproverConfigInput) []ApproverConfigInput {
	out := make([]ApproverConfigInput, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		item.DepartmentExternalID = strings.TrimSpace(item.DepartmentExternalID)
		item.DepartmentDisplayPath = strings.TrimSpace(item.DepartmentDisplayPath)
		if item.DepartmentExternalID == "" || item.ApproverUserID <= 0 {
			continue
		}
		key := fmt.Sprintf("%s/%d", item.DepartmentExternalID, item.ApproverUserID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].DepartmentDisplayPath != out[j].DepartmentDisplayPath {
			return out[i].DepartmentDisplayPath < out[j].DepartmentDisplayPath
		}
		if out[i].DepartmentExternalID != out[j].DepartmentExternalID {
			return out[i].DepartmentExternalID < out[j].DepartmentExternalID
		}
		return out[i].ApproverUserID < out[j].ApproverUserID
	})
	return out
}

func approverConfigDepartmentIDs(items []ApproverConfigInput) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		departmentID := strings.TrimSpace(item.DepartmentExternalID)
		if departmentID == "" {
			continue
		}
		if _, ok := seen[departmentID]; ok {
			continue
		}
		seen[departmentID] = struct{}{}
		out = append(out, departmentID)
	}
	sort.Strings(out)
	return out
}

func (s *Service) approverConfigResponse(ctx context.Context, rows []*ent.QuotaResetApproverConfig) (*ApproverConfigListResponse, error) {
	userIDs := make([]int, 0, len(rows))
	seen := map[int]struct{}{}
	for _, row := range rows {
		if _, ok := seen[row.ApproverUserID]; ok {
			continue
		}
		seen[row.ApproverUserID] = struct{}{}
		userIDs = append(userIDs, row.ApproverUserID)
	}
	usersByID := map[int]*ent.User{}
	if len(userIDs) > 0 {
		users, err := s.client.User.Query().Where(entuser.IDIn(userIDs...)).All(ctx)
		if err != nil {
			return nil, fmt.Errorf("load quota reset approver users: %w", err)
		}
		for _, user := range users {
			usersByID[user.ID] = user
		}
	}
	items := make([]ApproverConfig, 0, len(rows))
	for _, row := range rows {
		item := ApproverConfig{
			ID:                    row.ID,
			DirectorySourceID:     row.DirectorySourceID,
			DepartmentExternalID:  row.DepartmentExternalID,
			DepartmentDisplayPath: row.DepartmentDisplayPath,
			ApproverUserID:        row.ApproverUserID,
			Enabled:               row.Enabled,
			CreatedAt:             row.CreatedAt,
			UpdatedAt:             row.UpdatedAt,
		}
		if user := usersByID[row.ApproverUserID]; user != nil {
			item.ApproverUsername = user.Username
			item.ApproverEmail = user.Email
		}
		items = append(items, item)
	}
	return &ApproverConfigListResponse{Items: items}, nil
}

func (s *Service) ensureNotificationSettingsLockRow(ctx context.Context) error {
	if _, err := s.client.SystemSetting.Create().
		SetKey(quotaResetNotificationSettingsLockKey).
		SetValue(quotaResetNotificationSettingsLockKey).
		Save(ctx); err != nil {
		if ent.IsConstraintError(err) {
			return nil
		}
		return fmt.Errorf("ensure quota reset notification settings lock: %w", err)
	}
	return nil
}

func lockNotificationSettings(ctx context.Context, tx *ent.Tx) error {
	affected, err := tx.SystemSetting.Update().
		Where(systemsetting.KeyEQ(quotaResetNotificationSettingsLockKey)).
		SetValue(quotaResetNotificationSettingsLockKey).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("lock quota reset notification settings: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("lock quota reset notification settings: affected %d rows", affected)
	}
	return nil
}

func (s *Service) validateNotificationSettings(ctx context.Context, input UpdateNotificationSettingsInput, authType quotaresetnotificationsetting.AuthType) error {
	if input.Enabled {
		parsed, err := url.Parse(strings.TrimSpace(input.URL))
		if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("%w: invalid webhook url", ErrInvalidNotification)
		}
	}
	if authType != quotaresetnotificationsetting.AuthTypeBearerToken {
		return nil
	}
	if input.CredentialID == nil || *input.CredentialID <= 0 {
		return fmt.Errorf("%w: bearer token credential is required", ErrInvalidNotification)
	}
	credential, err := s.client.Credential.Get(ctx, *input.CredentialID)
	if err != nil {
		return fmt.Errorf("%w: load webhook credential: %v", ErrInvalidNotification, err)
	}
	if credential.Kind != entcredential.KindSecretText {
		return fmt.Errorf("%w: webhook credential must be secret_text", ErrInvalidNotification)
	}
	return nil
}

func notificationSettingsResponse(row *ent.QuotaResetNotificationSetting) *NotificationSettings {
	if row == nil {
		return &NotificationSettings{
			Channel:  quotaresetnotificationsetting.ChannelGenericWebhook.String(),
			AuthType: quotaresetnotificationsetting.AuthTypeNone.String(),
		}
	}
	return &NotificationSettings{
		Enabled:      row.Enabled,
		Channel:      row.Channel.String(),
		URL:          row.URL,
		AuthType:     row.AuthType.String(),
		CredentialID: row.CredentialID,
		UpdatedAt:    row.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func (s *Service) backfillLegacyNotificationChannel(ctx context.Context, row *ent.QuotaResetNotificationSetting) (*ent.QuotaResetNotificationSetting, error) {
	if row == nil || row.Channel != quotaresetnotificationsetting.ChannelLegacyAuto {
		return row, nil
	}
	channel := quotaresetnotificationsetting.ChannelGenericWebhook
	if parsed, err := url.Parse(strings.TrimSpace(row.URL)); err == nil && isWeComRobotWebhookURL(parsed) {
		channel = quotaresetnotificationsetting.ChannelWecomGroupRobot
	}
	updated, err := s.client.QuotaResetNotificationSetting.UpdateOneID(row.ID).SetChannel(channel).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("backfill quota reset notification channel: %w", err)
	}
	return updated, nil
}
