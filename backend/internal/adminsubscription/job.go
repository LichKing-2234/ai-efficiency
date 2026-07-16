package adminsubscription

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/adminsubscriptionjob"
	entuser "github.com/ai-efficiency/backend/ent/user"
)

const (
	MaxTargets                     = 500
	defaultJobTimeout              = 30 * time.Minute
	defaultPerTargetTimeout        = 30 * time.Second
	defaultFailureUpdateTimeout    = 5 * time.Second
	defaultStaleJobAfter           = time.Hour
	staleAdminSubscriptionJobError = "admin subscription job was abandoned after no progress was recorded for more than 1h."
)

var errCurrentFilterTargetResolverNotConfigured = errors.New("current filter target resolver is not configured")

type ValidationError struct {
	message string
}

func NewValidationError(message string) error {
	return &ValidationError{message: message}
}

func (e *ValidationError) Error() string {
	return e.message
}

type TooManyTargetsError struct {
	Max int
}

func NewTooManyTargetsError(max int) error {
	return &TooManyTargetsError{Max: max}
}

func (e *TooManyTargetsError) Error() string {
	return fmt.Sprintf("subscription batch targets too many; maximum is %d users", e.Max)
}

type StartJobRequest struct {
	Scope        string
	UserIDs      []int
	FilterQuery  string
	DepartmentID string
	AccessStatus string
	Operation    string
	ProviderID   int
	GroupID      string
	ValidityDays int
	Days         int
}

type ResultRow struct {
	UserID      int    `json:"user_id"`
	Username    string `json:"username,omitempty"`
	Email       string `json:"email,omitempty"`
	RelayUserID *int   `json:"relay_user_id,omitempty"`
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
}

type TargetSnapshot struct {
	UserID      int    `json:"user_id"`
	Username    string `json:"username,omitempty"`
	Email       string `json:"email,omitempty"`
	RelayUserID *int   `json:"relay_user_id,omitempty"`
	Missing     bool   `json:"missing,omitempty"`
}

type SubscriptionOperator interface {
	AssignSubscriptionForUser(ctx context.Context, userID, groupID int64, validityDays int) error
	ExtendSubscriptionForUser(ctx context.Context, userID, groupID int64, days int) error
	RemoveSubscriptionForUser(ctx context.Context, userID, groupID int64) error
	ResetSubscriptionQuotaForUser(ctx context.Context, userID, groupID int64) error
}

type CurrentFilter struct {
	Query        string
	DepartmentID string
	AccessStatus string
}

type CurrentFilterTargetResolver interface {
	ResolveCurrentFilterTargets(ctx context.Context, filter CurrentFilter, limit int) ([]*ent.User, error)
}

type CurrentFilterTargetResolverFunc func(context.Context, CurrentFilter, int) ([]*ent.User, error)

func (f CurrentFilterTargetResolverFunc) ResolveCurrentFilterTargets(ctx context.Context, filter CurrentFilter, limit int) ([]*ent.User, error) {
	return f(ctx, filter, limit)
}

type Service struct {
	client               *ent.Client
	currentFilterTargets CurrentFilterTargetResolver
	jobTimeout           time.Duration
	perTargetTimeout     time.Duration
	failureUpdateTimeout time.Duration
	staleJobAfter        time.Duration
}

func NewService(client *ent.Client, resolvers ...CurrentFilterTargetResolver) *Service {
	var currentFilterTargets CurrentFilterTargetResolver
	if len(resolvers) > 0 {
		currentFilterTargets = resolvers[0]
	}
	return &Service{
		client:               client,
		currentFilterTargets: currentFilterTargets,
		jobTimeout:           defaultJobTimeout,
		perTargetTimeout:     defaultPerTargetTimeout,
		failureUpdateTimeout: defaultFailureUpdateTimeout,
		staleJobAfter:        defaultStaleJobAfter,
	}
}

func (s *Service) StartJob(ctx context.Context, req StartJobRequest) (*ent.AdminSubscriptionJob, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("admin subscription service is not configured")
	}

	req.Scope = strings.TrimSpace(req.Scope)
	req.Operation = strings.TrimSpace(req.Operation)
	req.GroupID = strings.TrimSpace(req.GroupID)
	req.FilterQuery = strings.TrimSpace(req.FilterQuery)
	req.DepartmentID = strings.TrimSpace(req.DepartmentID)
	req.AccessStatus = strings.TrimSpace(req.AccessStatus)
	if req.ProviderID <= 0 {
		return nil, NewValidationError("provider_id is required")
	}
	if _, err := parseGroupID(req.GroupID); err != nil {
		return nil, err
	}
	scope, err := validateScope(req.Scope)
	if err != nil {
		return nil, err
	}
	operation, err := validateOperation(req)
	if err != nil {
		return nil, err
	}
	targetIDs, requestedIDs, targetSnapshots, err := s.resolveTargets(ctx, scope, req)
	if err != nil {
		return nil, err
	}
	targetSnapshotMaps, err := targetSnapshotsToMaps(targetSnapshots)
	if err != nil {
		return nil, err
	}

	return s.client.AdminSubscriptionJob.Create().
		SetStatus(adminsubscriptionjob.StatusQueued).
		SetPhase(adminsubscriptionjob.PhaseQueued).
		SetScope(scope).
		SetOperation(operation).
		SetProviderID(req.ProviderID).
		SetGroupID(req.GroupID).
		SetValidityDays(req.ValidityDays).
		SetDays(req.Days).
		SetFilterQuery(req.FilterQuery).
		SetTargetUserIds(targetIDs).
		SetTargetSnapshots(targetSnapshotMaps).
		SetRequestedUserIds(requestedIDs).
		SetTotalCount(len(targetIDs)).
		SetProcessedCount(0).
		SetSuccessCount(0).
		SetSkippedCount(0).
		SetFailedCount(0).
		SetResults([]map[string]interface{}{}).
		Save(ctx)
}

func (s *Service) RunJob(ctx context.Context, jobID int, operator SubscriptionOperator) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("admin subscription service is not configured")
	}
	if operator == nil {
		return s.failJob(ctx, jobID, fmt.Errorf("subscription operator is not configured"))
	}

	job, err := s.GetJob(ctx, jobID)
	if err != nil {
		return err
	}
	groupID, err := parseGroupID(job.GroupID)
	if err != nil {
		return s.failJob(ctx, jobID, err)
	}
	if _, err := operationFromJob(job.Operation); err != nil {
		return s.failJob(ctx, jobID, err)
	}
	targetSnapshots := TargetSnapshotsFromJob(job)
	if len(targetSnapshots) == 0 {
		targetSnapshots = s.targetSnapshotsFromIDs(ctx, job.TargetUserIds)
	}

	runCtx, cancel := s.runContext(ctx, len(targetSnapshots))
	defer cancel()

	now := time.Now()
	if _, err := s.client.AdminSubscriptionJob.UpdateOneID(job.ID).
		SetStatus(adminsubscriptionjob.StatusRunning).
		SetPhase(adminsubscriptionjob.PhaseProcessing).
		SetStartedAt(now).
		Save(runCtx); err != nil {
		return fmt.Errorf("mark subscription job running: %w", err)
	}

	results := ResultsFromJob(job)
	processed, success, skipped, failed := job.ProcessedCount, job.SuccessCount, job.SkippedCount, job.FailedCount
	for _, target := range targetSnapshots {
		if err := runCtx.Err(); err != nil {
			return s.failJobWithFreshContext(job.ID, fmt.Errorf("subscription job deadline exceeded: %w", err))
		}
		row := s.runTarget(runCtx, job, operator, groupID, target)
		results = append(results, row)
		processed++
		switch row.Status {
		case "success":
			success++
		case "skipped":
			skipped++
		default:
			failed++
		}
		if err := s.saveProgress(runCtx, job.ID, results, processed, success, skipped, failed); err != nil {
			return s.failJobWithFreshContext(job.ID, err)
		}
	}

	completedAt := time.Now()
	if _, err := s.client.AdminSubscriptionJob.UpdateOneID(job.ID).
		SetStatus(adminsubscriptionjob.StatusCompleted).
		SetPhase(adminsubscriptionjob.PhaseCompleted).
		SetCompletedAt(completedAt).
		Save(runCtx); err != nil {
		return s.failJobWithFreshContext(job.ID, fmt.Errorf("mark subscription job completed: %w", err))
	}
	return nil
}

func (s *Service) GetJob(ctx context.Context, id int) (*ent.AdminSubscriptionJob, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("admin subscription service is not configured")
	}
	return s.client.AdminSubscriptionJob.Get(ctx, id)
}

func (s *Service) GetLatestJob(ctx context.Context) (*ent.AdminSubscriptionJob, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("admin subscription service is not configured")
	}
	if err := s.abandonStaleJobs(ctx); err != nil {
		return nil, err
	}
	active, err := s.client.AdminSubscriptionJob.Query().
		Where(adminsubscriptionjob.StatusIn(adminsubscriptionjob.StatusQueued, adminsubscriptionjob.StatusRunning)).
		Order(ent.Desc(adminsubscriptionjob.FieldCreatedAt)).
		First(ctx)
	if err == nil {
		return active, nil
	}
	if !ent.IsNotFound(err) {
		return nil, err
	}
	return s.client.AdminSubscriptionJob.Query().
		Order(ent.Desc(adminsubscriptionjob.FieldCreatedAt)).
		First(ctx)
}

func ResultsFromJob(job *ent.AdminSubscriptionJob) []ResultRow {
	if job == nil || len(job.Results) == 0 {
		return nil
	}
	data, err := json.Marshal(job.Results)
	if err != nil {
		return nil
	}
	var rows []ResultRow
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil
	}
	return rows
}

func TargetSnapshotsFromJob(job *ent.AdminSubscriptionJob) []TargetSnapshot {
	if job == nil || len(job.TargetSnapshots) == 0 {
		return nil
	}
	data, err := json.Marshal(job.TargetSnapshots)
	if err != nil {
		return nil
	}
	var rows []TargetSnapshot
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil
	}
	return rows
}

func (s *Service) resolveTargets(ctx context.Context, scope adminsubscriptionjob.Scope, req StartJobRequest) ([]int, []int, []TargetSnapshot, error) {
	query := s.client.User.Query()
	var users []*ent.User
	switch scope {
	case adminsubscriptionjob.ScopeSelected:
		ids := uniquePositiveInts(req.UserIDs)
		if len(ids) == 0 {
			return nil, nil, nil, NewValidationError("user_ids is required for selected scope")
		}
		if len(ids) > MaxTargets {
			return nil, nil, nil, NewTooManyTargetsError(MaxTargets)
		}
		users, err := query.Where(entuser.IDIn(ids...)).All(ctx)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("list users: %w", err)
		}
		usersByID := make(map[int]*ent.User, len(users))
		for _, u := range users {
			usersByID[u.ID] = u
		}
		snapshots := make([]TargetSnapshot, 0, len(ids))
		for _, id := range ids {
			if u := usersByID[id]; u != nil {
				snapshots = append(snapshots, targetSnapshotFromUser(u))
				continue
			}
			snapshots = append(snapshots, TargetSnapshot{UserID: id, Missing: true})
		}
		return ids, ids, snapshots, nil
	case adminsubscriptionjob.ScopeCurrentFilter:
		if s.currentFilterTargets == nil {
			return nil, nil, nil, fmt.Errorf("resolve current filter targets: %w", errCurrentFilterTargetResolverNotConfigured)
		}
		var err error
		users, err = s.currentFilterTargets.ResolveCurrentFilterTargets(ctx, CurrentFilter{
			Query:        req.FilterQuery,
			DepartmentID: req.DepartmentID,
			AccessStatus: req.AccessStatus,
		}, MaxTargets+1)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("resolve current filter targets: %w", err)
		}
	case adminsubscriptionjob.ScopeAllMapped:
		var err error
		users, err = query.
			Where(entuser.RelayUserIDNotNil(), entuser.RelayUserIDGT(0)).
			Order(ent.Asc(entuser.FieldID)).
			Limit(MaxTargets + 1).
			All(ctx)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("list users: %w", err)
		}
	default:
		return nil, nil, nil, NewValidationError("scope must be selected, current_filter, or all_mapped")
	}

	if len(users) > MaxTargets {
		return nil, nil, nil, NewTooManyTargetsError(MaxTargets)
	}
	ids := make([]int, 0, len(users))
	snapshots := make([]TargetSnapshot, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
		snapshots = append(snapshots, targetSnapshotFromUser(u))
	}
	return ids, nil, snapshots, nil
}

func (s *Service) runTarget(ctx context.Context, job *ent.AdminSubscriptionJob, operator SubscriptionOperator, groupID int64, target TargetSnapshot) ResultRow {
	if target.Missing {
		return ResultRow{UserID: target.UserID, Status: "failed", Message: "user not found"}
	}

	row := ResultRow{
		UserID:      target.UserID,
		Username:    target.Username,
		Email:       target.Email,
		RelayUserID: target.RelayUserID,
	}
	if target.RelayUserID == nil || *target.RelayUserID <= 0 {
		row.Status = "skipped"
		row.Message = "user is not linked to a relay user"
		return row
	}

	targetCtx, cancel := s.targetContext(ctx)
	defer cancel()

	relayUserID := int64(*target.RelayUserID)
	var opErr error
	switch job.Operation {
	case adminsubscriptionjob.OperationAdd:
		opErr = operator.AssignSubscriptionForUser(targetCtx, relayUserID, groupID, job.ValidityDays)
	case adminsubscriptionjob.OperationExtend:
		opErr = operator.ExtendSubscriptionForUser(targetCtx, relayUserID, groupID, job.Days)
	case adminsubscriptionjob.OperationRemove:
		opErr = operator.RemoveSubscriptionForUser(targetCtx, relayUserID, groupID)
	case adminsubscriptionjob.OperationResetQuota:
		opErr = operator.ResetSubscriptionQuotaForUser(targetCtx, relayUserID, groupID)
	default:
		opErr = fmt.Errorf("operation must be add, extend, remove, or reset_quota")
	}
	if opErr != nil {
		row.Status = "failed"
		row.Message = opErr.Error()
		return row
	}
	row.Status = "success"
	return row
}

func (s *Service) targetSnapshotsFromIDs(ctx context.Context, userIDs []int) []TargetSnapshot {
	snapshots := make([]TargetSnapshot, 0, len(userIDs))
	for _, id := range userIDs {
		u, err := s.client.User.Get(ctx, id)
		if err != nil {
			snapshots = append(snapshots, TargetSnapshot{UserID: id, Missing: true})
			continue
		}
		snapshots = append(snapshots, targetSnapshotFromUser(u))
	}
	return snapshots
}

func targetSnapshotFromUser(u *ent.User) TargetSnapshot {
	return TargetSnapshot{
		UserID:      u.ID,
		Username:    u.Username,
		Email:       u.Email,
		RelayUserID: u.RelayUserID,
	}
}

func (s *Service) saveProgress(ctx context.Context, jobID int, rows []ResultRow, processed, success, skipped, failed int) error {
	results, err := resultRowsToMaps(rows)
	if err != nil {
		return err
	}
	if _, err := s.client.AdminSubscriptionJob.UpdateOneID(jobID).
		SetProcessedCount(processed).
		SetSuccessCount(success).
		SetSkippedCount(skipped).
		SetFailedCount(failed).
		SetResults(results).
		Save(ctx); err != nil {
		return fmt.Errorf("update subscription job progress: %w", err)
	}
	return nil
}

func (s *Service) failJob(ctx context.Context, jobID int, cause error) error {
	message := cause.Error()
	now := time.Now()
	if _, err := s.client.AdminSubscriptionJob.UpdateOneID(jobID).
		SetStatus(adminsubscriptionjob.StatusFailed).
		SetPhase(adminsubscriptionjob.PhaseFailed).
		SetLastError(message).
		SetCompletedAt(now).
		Save(ctx); err != nil {
		return fmt.Errorf("mark subscription job failed: %w", err)
	}
	return cause
}

func (s *Service) failJobWithFreshContext(jobID int, cause error) error {
	timeout := s.failureUpdateTimeout
	if timeout <= 0 {
		timeout = defaultFailureUpdateTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := s.failJob(ctx, jobID, cause); err != nil {
		return fmt.Errorf("%v; additionally failed to mark subscription job failed: %w", cause, err)
	}
	return cause
}

func (s *Service) runContext(ctx context.Context, targetCount int) (context.Context, context.CancelFunc) {
	timeout := s.jobTimeout
	if s.perTargetTimeout > 0 && targetCount > 0 {
		targetBudget := time.Duration(targetCount) * s.perTargetTimeout
		if timeout <= 0 {
			timeout = targetBudget
		} else {
			timeout += targetBudget
		}
	}
	if timeout <= 0 {
		return ctx, func() {}
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func (s *Service) targetContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if s.perTargetTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, s.perTargetTimeout)
}

func (s *Service) abandonStaleJobs(ctx context.Context) error {
	if s.staleJobAfter <= 0 {
		return nil
	}
	cutoff := time.Now().Add(-s.staleJobAfter)
	now := time.Now()
	if _, err := s.client.AdminSubscriptionJob.Update().
		Where(
			adminsubscriptionjob.StatusIn(adminsubscriptionjob.StatusQueued, adminsubscriptionjob.StatusRunning),
			adminsubscriptionjob.UpdatedAtLT(cutoff),
		).
		SetStatus(adminsubscriptionjob.StatusAbandoned).
		SetPhase(adminsubscriptionjob.PhaseFailed).
		SetLastError(staleAdminSubscriptionJobError).
		SetCompletedAt(now).
		Save(ctx); err != nil {
		return fmt.Errorf("abandon stale subscription jobs: %w", err)
	}
	return nil
}

func targetSnapshotsToMaps(rows []TargetSnapshot) ([]map[string]interface{}, error) {
	data, err := json.Marshal(rows)
	if err != nil {
		return nil, fmt.Errorf("marshal subscription job targets: %w", err)
	}
	var values []map[string]interface{}
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("unmarshal subscription job targets: %w", err)
	}
	if values == nil {
		values = []map[string]interface{}{}
	}
	return values, nil
}

func resultRowsToMaps(rows []ResultRow) ([]map[string]interface{}, error) {
	data, err := json.Marshal(rows)
	if err != nil {
		return nil, fmt.Errorf("marshal subscription job results: %w", err)
	}
	var values []map[string]interface{}
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("unmarshal subscription job results: %w", err)
	}
	if values == nil {
		values = []map[string]interface{}{}
	}
	return values, nil
}

func validateScope(scope string) (adminsubscriptionjob.Scope, error) {
	switch scope {
	case string(adminsubscriptionjob.ScopeSelected):
		return adminsubscriptionjob.ScopeSelected, nil
	case string(adminsubscriptionjob.ScopeCurrentFilter):
		return adminsubscriptionjob.ScopeCurrentFilter, nil
	case string(adminsubscriptionjob.ScopeAllMapped):
		return adminsubscriptionjob.ScopeAllMapped, nil
	default:
		return "", NewValidationError("scope must be selected, current_filter, or all_mapped")
	}
}

func validateOperation(req StartJobRequest) (adminsubscriptionjob.Operation, error) {
	switch req.Operation {
	case string(adminsubscriptionjob.OperationAdd):
		if req.ValidityDays <= 0 {
			return "", NewValidationError("validity_days is required")
		}
		return adminsubscriptionjob.OperationAdd, nil
	case string(adminsubscriptionjob.OperationExtend):
		if req.Days <= 0 {
			return "", NewValidationError("days is required")
		}
		return adminsubscriptionjob.OperationExtend, nil
	case string(adminsubscriptionjob.OperationRemove):
		return adminsubscriptionjob.OperationRemove, nil
	case string(adminsubscriptionjob.OperationResetQuota):
		return adminsubscriptionjob.OperationResetQuota, nil
	default:
		return "", NewValidationError("operation must be add, extend, remove, or reset_quota")
	}
}

func operationFromJob(operation adminsubscriptionjob.Operation) (adminsubscriptionjob.Operation, error) {
	switch operation {
	case adminsubscriptionjob.OperationAdd, adminsubscriptionjob.OperationExtend, adminsubscriptionjob.OperationRemove, adminsubscriptionjob.OperationResetQuota:
		return operation, nil
	default:
		return "", fmt.Errorf("operation must be add, extend, remove, or reset_quota")
	}
}

func parseGroupID(groupID string) (int64, error) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(groupID), 10, 64)
	if err != nil || parsed <= 0 {
		return 0, NewValidationError("group_id is required")
	}
	return parsed, nil
}

func uniquePositiveInts(values []int) []int {
	seen := make(map[int]struct{}, len(values))
	ids := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		ids = append(ids, value)
	}
	return ids
}
