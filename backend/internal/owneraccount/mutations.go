package owneraccount

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/owneraccountoperation"
	"github.com/ai-efficiency/backend/internal/relay"
)

// ResetCreditInput is one request to consume a banked reset credit.
type ResetCreditInput struct {
	ActorUserID int
	AccountID   int64
	Confirm     bool
	Reason      string
}

// InvitationInput is one request to send a referral invitation.
type InvitationInput struct {
	ActorUserID int
	AccountID   int64
	Email       string
	ProgramID   string
	Consent     bool
	Reason      string
}

// ResetCredit admits one reset-credit mutation. Every gate runs server-side and a
// rejected attempt writes no operation row (gate rejections are not attempts).
func (s *Service) ResetCredit(ctx context.Context, input ResetCreditInput) (*AcceptedOperation, error) {
	if !s.options.MutationsEnabled {
		return nil, errActionsDisabled()
	}
	provider, account, err := s.ownedAccount(ctx, input.ActorUserID, input.AccountID)
	if err != nil {
		return nil, err
	}
	if !input.Confirm {
		return nil, errConsentRequired()
	}
	return s.admitMutation(ctx, mutationPlan{
		provider: provider,
		account:  account,
		action:   "reset_credit",
		actorID:  input.ActorUserID,
		reason:   input.Reason,
	})
}

// Invite admits one invitation mutation. The program id always comes from the
// fresh referral observation; a request carrying a different program is rejected
// rather than silently retargeted.
func (s *Service) Invite(ctx context.Context, input InvitationInput) (*AcceptedOperation, error) {
	if !s.options.MutationsEnabled {
		return nil, errActionsDisabled()
	}
	provider, account, err := s.ownedAccount(ctx, input.ActorUserID, input.AccountID)
	if err != nil {
		return nil, err
	}
	if !input.Consent {
		return nil, errConsentRequired()
	}
	email := strings.TrimSpace(input.Email)
	if !validInvitationEmail(email) {
		return nil, errInvalidEmail()
	}
	return s.admitMutation(ctx, mutationPlan{
		provider:    provider,
		account:     account,
		action:      "send_invitation",
		actorID:     input.ActorUserID,
		reason:      input.Reason,
		inviteEmail: email,
		programID:   strings.TrimSpace(input.ProgramID),
	})
}

type mutationPlan struct {
	provider    *ent.RelayProvider
	account     *relay.OwnerAccount
	action      string
	actorID     int
	reason      string
	inviteEmail string
	programID   string
}

// admitMutation runs the local gates, claims the single-flight slot and starts the
// detached worker.
func (s *Service) admitMutation(ctx context.Context, plan mutationPlan) (*AcceptedOperation, error) {
	provider := plan.provider
	account := plan.account
	if _, err := s.sweepStaleAccounts(ctx, provider.ID, []int64{account.ID}); err != nil {
		return nil, err
	}
	facts, err := s.operationFacts(ctx, provider.ID, []int64{account.ID})
	if err != nil {
		return nil, err
	}
	accountFacts := facts[account.ID]
	if accountFacts == nil {
		accountFacts = &accountOperationFacts{Latest: map[string]latestOperation{}}
	}
	now, err := s.databaseNow(ctx)
	if err != nil {
		return nil, err
	}

	switch plan.action {
	case "reset_credit":
		if err := s.requireFreshQuotaObservation(account, accountFacts, now); err != nil {
			return nil, err
		}
		if err := s.requireResetCapacity(ctx, provider.ID, account.ID, "reset_credit", s.options.ResetDailyCap, now); err != nil {
			return nil, err
		}
	case "send_invitation":
		programID, err := s.requireFreshReferralObservation(account, accountFacts, now, plan.programID)
		if err != nil {
			return nil, err
		}
		plan.programID = programID
		if err := s.requireResetCapacity(ctx, provider.ID, account.ID, "send_invitation", s.options.InvitationDailyCap, now); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("admit owner mutation: unsupported action %q", plan.action)
	}

	operationID, err := s.insertRunningOperation(ctx, mutationAttempt{
		ProviderID:  provider.ID,
		AccountID:   account.ID,
		ActorUserID: plan.actorID,
		Action:      plan.action,
		Reason:      plan.reason,
		InviteEmail: plan.inviteEmail,
	})
	if err != nil {
		return nil, err
	}
	s.startOperationWorker(workerRequest{
		requestCtx:  ctx,
		operationID: operationID,
		providerID:  provider.ID,
		accountID:   account.ID,
		actorUserID: plan.actorID,
		action:      plan.action,
		programID:   plan.programID,
		inviteEmail: plan.inviteEmail,
	})
	return &AcceptedOperation{OperationID: operationID}, nil
}

// requireFreshQuotaObservation enforces the reset precondition: a successful
// quota observation newer than the last reset attempt, no older than 15 minutes,
// reporting at least one unexpired credit.
func (s *Service) requireFreshQuotaObservation(account *relay.OwnerAccount, facts *accountOperationFacts, now time.Time) error {
	if facts.QuotaObservedAt == nil {
		return errResetFreshness()
	}
	if facts.LastResetAt != nil && !facts.QuotaObservedAt.After(*facts.LastResetAt) {
		return errResetFreshness()
	}
	if now.Sub(*facts.QuotaObservedAt) > s.options.QuotaObservationMaxAge {
		return errResetFreshness()
	}
	if liveResetCredits(account, now) <= 0 {
		return errCreditsExpired()
	}
	return nil
}

// requireFreshReferralObservation enforces the invitation precondition: a fresh
// referral observation newer than the last invitation attempt, no older than 10
// minutes, with a visible program and remaining capacity. It returns the program
// id taken from that observation.
func (s *Service) requireFreshReferralObservation(account *relay.OwnerAccount, facts *accountOperationFacts, now time.Time, requestedProgramID string) (string, error) {
	if facts.ReferralObservedAt == nil {
		return "", errInviteFreshness()
	}
	if facts.LastInviteAt != nil && !facts.ReferralObservedAt.After(*facts.LastInviteAt) {
		return "", errInviteFreshness()
	}
	if now.Sub(*facts.ReferralObservedAt) > s.options.InvitationObservationAge {
		return "", errInviteFreshness()
	}
	referral := account.Referral
	if referral == nil || !referral.ShouldShow {
		return "", errReferralUnavailable()
	}
	programID := strings.TrimSpace(referral.ProgramID)
	if programID == "" {
		return "", errReferralUnavailable()
	}
	if referral.AvailableInvites == nil || *referral.AvailableInvites <= 0 {
		return "", errReferralUnavailable()
	}
	if requestedProgramID != "" && requestedProgramID != programID {
		return "", errProgramChanged()
	}
	return programID, nil
}

// requireResetCapacity enforces the rolling 24-hour per-account attempt cap. Gate
// rejections never write rows, so every existing row is an attempt.
func (s *Service) requireResetCapacity(ctx context.Context, providerID int, accountID int64, action string, limit int, now time.Time) error {
	count, oldest, err := s.countRecentMutations(ctx, providerID, accountID, action)
	if err != nil {
		return err
	}
	if count < limit {
		return nil
	}
	retryAfter := 0
	if oldest != nil {
		retryAfter = int(oldest.Add(24 * time.Hour).Sub(now).Seconds())
	}
	if retryAfter < 1 {
		retryAfter = 1
	}
	return errMutationRateLimited(retryAfter)
}

// liveResetCredits clamps the cached reset-credit snapshot to the credits whose
// expiry is still in the future. The adapter already clamps, but the gate repeats
// it so a stale snapshot can never spend an expired credit.
func liveResetCredits(account *relay.OwnerAccount, now time.Time) int {
	if account == nil || account.ResetCredits == nil {
		return 0
	}
	live := 0
	for _, expiry := range account.ResetCredits.ExpiresAt {
		if expiry.After(now) {
			live++
		}
	}
	if account.ResetCredits.Available < live {
		return account.ResetCredits.Available
	}
	return live
}

func validInvitationEmail(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 254 {
		return false
	}
	if strings.Count(value, "@") != 1 {
		return false
	}
	local, domain, _ := strings.Cut(value, "@")
	if strings.TrimSpace(local) == "" || strings.TrimSpace(domain) == "" {
		return false
	}
	if strings.ContainsAny(value, " \t\r\n") {
		return false
	}
	return strings.Contains(domain, ".") && !strings.HasPrefix(domain, ".") && !strings.HasSuffix(domain, ".")
}

// ListOperations returns one page of the actor's operation history for one owned
// account, sweeping stale running rows first.
func (s *Service) ListOperations(ctx context.Context, actorUserID int, accountID int64, page, pageSize int) (*OperationPage, error) {
	page, pageSize, err := normalizePagination(page, pageSize)
	if err != nil {
		return nil, err
	}
	provider, _, err := s.ownedAccount(ctx, actorUserID, accountID)
	if err != nil {
		return nil, err
	}
	if _, err := s.sweepStaleAccounts(ctx, provider.ID, []int64{accountID}); err != nil {
		return nil, err
	}
	return s.operationPage(ctx, s.client.OwnerAccountOperation.Query().
		Where(
			owneraccountoperation.ProviderIDEQ(provider.ID),
			owneraccountoperation.AccountIDEQ(accountID),
			owneraccountoperation.ActorUserIDEQ(actorUserID),
		), page, pageSize)
}

// Operation returns one operation belonging to the actor.
func (s *Service) Operation(ctx context.Context, actorUserID int, operationID int) (*OperationView, error) {
	if operationID <= 0 {
		return nil, errOperationNotFound()
	}
	if _, err := s.loadActor(ctx, actorUserID); err != nil {
		return nil, err
	}
	row, err := s.client.OwnerAccountOperation.Get(ctx, operationID)
	if ent.IsNotFound(err) {
		return nil, errOperationNotFound()
	}
	if err != nil {
		return nil, fmt.Errorf("load owner operation: %w", err)
	}
	if row.ActorUserID != actorUserID {
		return nil, errOperationNotFound()
	}
	if _, err := s.sweepStaleAccounts(ctx, row.ProviderID, []int64{row.AccountID}); err != nil {
		return nil, err
	}
	row, err = s.client.OwnerAccountOperation.Get(ctx, operationID)
	if ent.IsNotFound(err) {
		return nil, errOperationNotFound()
	}
	if err != nil {
		return nil, fmt.Errorf("reload owner operation: %w", err)
	}
	view := operationView(row)
	return &view, nil
}

// AdminListOperations is the read-only support surface. There is deliberately no
// admin mutation and no override of an unknown outcome.
type AdminListParams struct {
	Page      int
	PageSize  int
	Status    string
	Action    string
	AccountID int64
}

// AdminListOperations returns a filtered, paginated operation list for support.
func (s *Service) AdminListOperations(ctx context.Context, params AdminListParams) (*OperationPage, error) {
	page, pageSize, err := normalizePagination(params.Page, params.PageSize)
	if err != nil {
		return nil, err
	}
	query := s.client.OwnerAccountOperation.Query()
	if status := strings.TrimSpace(params.Status); status != "" {
		if !validOperationStatus(status) {
			return nil, errInvalidFilter("status")
		}
		query = query.Where(owneraccountoperation.StatusEQ(owneraccountoperation.Status(status)))
	}
	if action := strings.TrimSpace(params.Action); action != "" {
		if !validOperationAction(action) {
			return nil, errInvalidFilter("action")
		}
		query = query.Where(owneraccountoperation.ActionEQ(owneraccountoperation.Action(action)))
	}
	if params.AccountID < 0 {
		return nil, errInvalidFilter("account_id")
	}
	if params.AccountID > 0 {
		query = query.Where(owneraccountoperation.AccountIDEQ(params.AccountID))
	}
	return s.operationPage(ctx, query, page, pageSize)
}

func (s *Service) operationPage(ctx context.Context, query *ent.OwnerAccountOperationQuery, page, pageSize int) (*OperationPage, error) {
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count owner operations: %w", err)
	}
	rows, err := query.
		Order(ent.Desc(owneraccountoperation.FieldCreatedAt), ent.Desc(owneraccountoperation.FieldID)).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list owner operations: %w", err)
	}
	items := make([]OperationView, 0, len(rows))
	for _, row := range rows {
		items = append(items, operationView(row))
	}
	return &OperationPage{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func operationView(row *ent.OwnerAccountOperation) OperationView {
	view := OperationView{
		ID:          row.ID,
		AccountID:   row.AccountID,
		ActorUserID: row.ActorUserID,
		Action:      string(row.Action),
		Status:      string(row.Status),
		ErrorCode:   row.UpstreamCode,
		WarningCode: row.WarningCode,
		InviteEmail: row.InviteEmail,
		Reason:      row.Reason,
		StartedAt:   utcPtr(row.StartedAt),
		FinishedAt:  utcPtr(row.FinishedAt),
		CreatedAt:   row.CreatedAt.UTC(),
	}
	if len(row.Result) > 0 {
		view.Result = row.Result
	}
	return view
}

func validOperationStatus(status string) bool {
	switch owneraccountoperation.Status(status) {
	case owneraccountoperation.StatusRunning, owneraccountoperation.StatusSucceeded,
		owneraccountoperation.StatusFailed, owneraccountoperation.StatusUnknown:
		return true
	default:
		return false
	}
}

func validOperationAction(action string) bool {
	switch owneraccountoperation.Action(action) {
	case owneraccountoperation.ActionRefreshQuota, owneraccountoperation.ActionRefreshReferral,
		owneraccountoperation.ActionResetCredit, owneraccountoperation.ActionSendInvitation:
		return true
	default:
		return false
	}
}

func utcPtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}
