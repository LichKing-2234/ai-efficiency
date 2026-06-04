package adminsubscription

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/adminsubscriptionjob"
	"github.com/ai-efficiency/backend/ent/predicate"
	entuser "github.com/ai-efficiency/backend/ent/user"
)

const MaxTargets = 500

type StartJobRequest struct {
	Scope        string
	UserIDs      []int
	FilterQuery  string
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

type SubscriptionOperator interface {
	AssignSubscriptionForUser(ctx context.Context, userID, groupID int64, validityDays int) error
	ExtendSubscriptionForUser(ctx context.Context, userID, groupID int64, days int) error
	RemoveSubscriptionForUser(ctx context.Context, userID, groupID int64) error
}

type Service struct {
	client *ent.Client
}

func NewService(client *ent.Client) *Service {
	return &Service{client: client}
}

func (s *Service) StartJob(ctx context.Context, req StartJobRequest) (*ent.AdminSubscriptionJob, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("admin subscription service is not configured")
	}

	req.Scope = strings.TrimSpace(req.Scope)
	req.Operation = strings.TrimSpace(req.Operation)
	req.GroupID = strings.TrimSpace(req.GroupID)
	req.FilterQuery = strings.TrimSpace(req.FilterQuery)
	if req.ProviderID <= 0 {
		return nil, fmt.Errorf("provider_id is required")
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
	targetIDs, requestedIDs, err := s.resolveTargets(ctx, scope, req)
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

	now := time.Now()
	if _, err := s.client.AdminSubscriptionJob.UpdateOneID(job.ID).
		SetStatus(adminsubscriptionjob.StatusRunning).
		SetPhase(adminsubscriptionjob.PhaseProcessing).
		SetStartedAt(now).
		Save(ctx); err != nil {
		return fmt.Errorf("mark subscription job running: %w", err)
	}

	results := ResultsFromJob(job)
	processed, success, skipped, failed := job.ProcessedCount, job.SuccessCount, job.SkippedCount, job.FailedCount
	for _, userID := range job.TargetUserIds {
		row := s.runTarget(ctx, job, operator, groupID, userID)
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
		if err := s.saveProgress(ctx, job.ID, results, processed, success, skipped, failed); err != nil {
			return err
		}
	}

	completedAt := time.Now()
	if _, err := s.client.AdminSubscriptionJob.UpdateOneID(job.ID).
		SetStatus(adminsubscriptionjob.StatusCompleted).
		SetPhase(adminsubscriptionjob.PhaseCompleted).
		SetCompletedAt(completedAt).
		Save(ctx); err != nil {
		return fmt.Errorf("mark subscription job completed: %w", err)
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

func (s *Service) resolveTargets(ctx context.Context, scope adminsubscriptionjob.Scope, req StartJobRequest) ([]int, []int, error) {
	query := s.client.User.Query()
	switch scope {
	case adminsubscriptionjob.ScopeSelected:
		ids := uniquePositiveInts(req.UserIDs)
		if len(ids) == 0 {
			return nil, nil, fmt.Errorf("user_ids is required for selected scope")
		}
		if len(ids) > MaxTargets {
			return nil, nil, fmt.Errorf("subscription batch targets too many; maximum is %d users", MaxTargets)
		}
		return ids, ids, nil
	case adminsubscriptionjob.ScopeCurrentFilter:
		if req.FilterQuery != "" {
			query = query.Where(searchPredicate(req.FilterQuery))
		}
	case adminsubscriptionjob.ScopeAllMapped:
		query = query.Where(entuser.RelayUserIDNotNil(), entuser.RelayUserIDGT(0))
	default:
		return nil, nil, fmt.Errorf("scope must be selected, current_filter, or all_mapped")
	}

	users, err := query.
		Order(ent.Asc(entuser.FieldID)).
		Limit(MaxTargets + 1).
		All(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list users: %w", err)
	}
	if len(users) > MaxTargets {
		return nil, nil, fmt.Errorf("subscription batch targets too many; maximum is %d users", MaxTargets)
	}
	ids := make([]int, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	return ids, nil, nil
}

func (s *Service) runTarget(ctx context.Context, job *ent.AdminSubscriptionJob, operator SubscriptionOperator, groupID int64, userID int) ResultRow {
	u, err := s.client.User.Get(ctx, userID)
	if err != nil {
		row := ResultRow{UserID: userID, Status: "failed"}
		if ent.IsNotFound(err) {
			row.Message = "user not found"
		} else {
			row.Message = fmt.Sprintf("get user: %v", err)
		}
		return row
	}

	row := ResultRow{
		UserID:      u.ID,
		Username:    u.Username,
		Email:       u.Email,
		RelayUserID: u.RelayUserID,
	}
	if u.RelayUserID == nil || *u.RelayUserID <= 0 {
		row.Status = "skipped"
		row.Message = "user is not linked to a relay user"
		return row
	}

	var opErr error
	switch job.Operation {
	case adminsubscriptionjob.OperationAdd:
		opErr = operator.AssignSubscriptionForUser(ctx, int64(*u.RelayUserID), groupID, job.ValidityDays)
	case adminsubscriptionjob.OperationExtend:
		opErr = operator.ExtendSubscriptionForUser(ctx, int64(*u.RelayUserID), groupID, job.Days)
	case adminsubscriptionjob.OperationRemove:
		opErr = operator.RemoveSubscriptionForUser(ctx, int64(*u.RelayUserID), groupID)
	default:
		opErr = fmt.Errorf("operation must be add, extend, or remove")
	}
	if opErr != nil {
		row.Status = "failed"
		row.Message = opErr.Error()
		return row
	}
	row.Status = "success"
	return row
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
		return "", fmt.Errorf("scope must be selected, current_filter, or all_mapped")
	}
}

func validateOperation(req StartJobRequest) (adminsubscriptionjob.Operation, error) {
	switch req.Operation {
	case string(adminsubscriptionjob.OperationAdd):
		if req.ValidityDays <= 0 {
			return "", fmt.Errorf("validity_days is required")
		}
		return adminsubscriptionjob.OperationAdd, nil
	case string(adminsubscriptionjob.OperationExtend):
		if req.Days <= 0 {
			return "", fmt.Errorf("days is required")
		}
		return adminsubscriptionjob.OperationExtend, nil
	case string(adminsubscriptionjob.OperationRemove):
		return adminsubscriptionjob.OperationRemove, nil
	default:
		return "", fmt.Errorf("operation must be add, extend, or remove")
	}
}

func operationFromJob(operation adminsubscriptionjob.Operation) (adminsubscriptionjob.Operation, error) {
	switch operation {
	case adminsubscriptionjob.OperationAdd, adminsubscriptionjob.OperationExtend, adminsubscriptionjob.OperationRemove:
		return operation, nil
	default:
		return "", fmt.Errorf("operation must be add, extend, or remove")
	}
}

func parseGroupID(groupID string) (int64, error) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(groupID), 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("group_id is required")
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

func searchPredicate(q string) predicate.User {
	predicates := []predicate.User{
		entuser.UsernameContainsFold(q),
		entuser.EmailContainsFold(q),
	}
	if n, err := strconv.Atoi(q); err == nil {
		predicates = append(predicates, entuser.IDEQ(n), entuser.RelayUserIDEQ(n))
	}
	return entuser.Or(predicates...)
}
