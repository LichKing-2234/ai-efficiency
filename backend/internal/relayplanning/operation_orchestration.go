package relayplanning

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relationshipoperation"
	"github.com/ai-efficiency/backend/ent/relationshipoperationattempt"
	"github.com/ai-efficiency/backend/ent/relationshipoperationmapping"
	"github.com/ai-efficiency/backend/ent/relationshipoperationstep"
)

type ActiveRelationshipOperationError struct {
	MappingID int
}

func (e *ActiveRelationshipOperationError) Error() string {
	return fmt.Sprintf("mapping %d already has an active Relationship Operation", e.MappingID)
}

type durableStepPlan struct {
	Key                 string
	Action              string
	RelationshipType    string
	Direction           relationshipoperationstep.Direction
	LocalUserID         int
	RelayUserID         int64
	SourceGroupID       int64
	TargetGroupID       int64
	ReviewedResourceIDs []int64
	ReviewedPriority    int
	ReviewedStatus      string
	ExpectedResult      map[string]any
	ResumeSupported     bool
	RestoreSupported    bool
}

type durableExecution struct {
	client      *ent.Client
	operationID int
	attemptID   int
	finished    bool
}

func (s *Service) beginInitialDurableExecution(ctx context.Context, plan *Plan, req ExecuteRequest) (*durableExecution, *Mapping, error) {
	if active := s.activeOperationForKey(ctx, req.OperationKey); active != nil {
		return nil, nil, active
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin relationship operation transaction: %w", err)
	}
	rollback := func(cause error) (*durableExecution, *Mapping, error) {
		_ = tx.Rollback()
		return nil, nil, cause
	}
	row, err := tx.RelayGroupMapping.Create().
		SetProviderID(plan.ProviderID).
		SetDepartmentExternalID(plan.DepartmentID).
		SetDepartmentName(plan.DepartmentName).
		SetPlatform(plan.Platform).
		SetTemplateGroupID(plan.TemplateGroupID).
		SetTemplateGroupName(plan.TemplateGroupName).
		SetSourceGroupID(plan.SourceGroupID).
		SetSourceGroupName(plan.SourceGroupName).
		SetGroupIds([]int64{}).
		SetMemberAssignments(map[string]int64{}).
		SetMemberSources(map[string]int64{}).
		SetDesiredAccounts(map[string][]map[string]int64{}).
		SetWeeklyCostTarget(plan.WeeklyCostTarget).
		Save(ctx)
	if err != nil {
		return rollback(fmt.Errorf("persist empty Replan baseline: %w", err))
	}
	mapping := mappingFromEnt(row)
	plan.MappingID = mapping.ID
	execution, err := persistDurableExecution(ctx, tx.Client(), []Mapping{mapping}, plan, req, buildDurableStepPlans(plan))
	if err != nil {
		return rollback(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit relationship operation transaction: %w", err)
	}
	execution.client = s.client
	return execution, &mapping, nil
}

func (s *Service) beginReplanDurableExecution(ctx context.Context, mapping Mapping, plan *Plan, req ExecuteRequest) (*durableExecution, error) {
	if active := s.activeOperationForKey(ctx, req.OperationKey); active != nil {
		return nil, active
	}
	mappingIDs := map[int]struct{}{mapping.ID: {}}
	for _, action := range req.MemberActions {
		if action.Mode == "move_here" && action.FromMappingID > 0 {
			mappingIDs[action.FromMappingID] = struct{}{}
		}
	}
	ids := make([]int, 0, len(mappingIDs))
	for id := range mappingIDs {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	mappings := make([]Mapping, 0, len(ids))
	for _, id := range ids {
		row, loadErr := s.client.RelayGroupMapping.Get(ctx, id)
		if loadErr != nil {
			return nil, fmt.Errorf("load affected Mapping %d: %w", id, loadErr)
		}
		mappings = append(mappings, mappingFromEnt(row))
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin relationship operation transaction: %w", err)
	}
	execution, err := persistDurableExecution(ctx, tx.Client(), mappings, plan, req, buildDurableStepPlans(plan))
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit relationship operation transaction: %w", err)
	}
	execution.client = s.client
	return execution, nil
}

func (s *Service) activeOperationForKey(ctx context.Context, operationKey string) *ActiveRelationshipOperationError {
	operation, err := s.client.RelationshipOperation.Query().Where(relationshipoperation.OperationKeyEQ(operationKey)).Only(ctx)
	if err != nil {
		return nil
	}
	owner, err := s.client.RelationshipOperationMapping.Query().Where(relationshipoperationmapping.OperationIDEQ(operation.ID), relationshipoperationmapping.ActiveEQ(true)).First(ctx)
	if err != nil {
		return nil
	}
	return &ActiveRelationshipOperationError{MappingID: owner.MappingID}
}

func persistDurableExecution(ctx context.Context, client *ent.Client, mappings []Mapping, plan *Plan, req ExecuteRequest, steps []durableStepPlan) (*durableExecution, error) {
	actorID := req.InitiatedByUserID
	if actorID <= 0 {
		actorID = 1
	}
	baseline := map[string]any{"mappings": mappings}
	target := jsonObject(plan)
	baselineFingerprint := jsonFingerprint(baseline)
	directions := []string{"resume", "restore"}
	for _, step := range steps {
		if !step.RestoreSupported {
			directions = []string{"resume"}
			break
		}
	}
	operation, err := client.RelationshipOperation.Create().
		SetOperationKey(req.OperationKey).
		SetProviderID(plan.ProviderID).
		SetPlatform(plan.Platform).
		SetBaselineSnapshot(baseline).
		SetTargetSnapshot(target).
		SetBaselineFingerprint(baselineFingerprint).
		SetTargetFingerprint(plan.RelationshipFingerprint).
		SetSupportedDirections(directions).
		SetInitiatedByUserID(actorID).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			existing, queryErr := client.RelationshipOperation.Query().Where(relationshipoperation.OperationKeyEQ(req.OperationKey)).Only(ctx)
			if queryErr == nil {
				owner, ownerErr := client.RelationshipOperationMapping.Query().Where(relationshipoperationmapping.OperationIDEQ(existing.ID), relationshipoperationmapping.ActiveEQ(true)).First(ctx)
				if ownerErr == nil {
					return nil, &ActiveRelationshipOperationError{MappingID: owner.MappingID}
				}
			}
		}
		return nil, fmt.Errorf("persist Relationship Operation: %w", err)
	}
	for _, mapping := range mappings {
		role := relationshipoperationmapping.RoleAffected
		if mapping.ID == plan.MappingID {
			role = relationshipoperationmapping.RolePrimary
		}
		_, err := client.RelationshipOperationMapping.Create().
			SetOperationID(operation.ID).
			SetMappingID(mapping.ID).
			SetRole(role).
			SetBaselineRevision(mapping.BaselineRevision).
			SetBaselineSnapshot(jsonObject(mapping)).
			Save(ctx)
		if err != nil {
			if ent.IsConstraintError(err) {
				return nil, &ActiveRelationshipOperationError{MappingID: mapping.ID}
			}
			return nil, fmt.Errorf("persist Relationship Operation ownership for Mapping %d: %w", mapping.ID, err)
		}
	}
	for _, step := range steps {
		create := client.RelationshipOperationStep.Create().
			SetOperationID(operation.ID).
			SetStepKey(step.Key).
			SetAction(step.Action).
			SetRelationshipType(step.RelationshipType).
			SetDirection(step.Direction).
			SetReviewedResourceIds(step.ReviewedResourceIDs).
			SetExpectedResult(step.ExpectedResult).
			SetResumeSupported(step.ResumeSupported).
			SetRestoreSupported(step.RestoreSupported)
		if step.LocalUserID > 0 {
			create.SetLocalUserID(step.LocalUserID)
		}
		if step.RelayUserID > 0 {
			create.SetRelayUserID(step.RelayUserID)
		}
		if step.SourceGroupID > 0 {
			create.SetSourceGroupID(step.SourceGroupID)
		}
		if step.TargetGroupID > 0 {
			create.SetTargetGroupID(step.TargetGroupID)
		}
		if step.ReviewedPriority > 0 {
			create.SetReviewedPriority(step.ReviewedPriority)
		}
		if step.ReviewedStatus != "" {
			create.SetReviewedStatus(step.ReviewedStatus)
		}
		if _, err := create.Save(ctx); err != nil {
			return nil, fmt.Errorf("persist Relationship Operation step %q: %w", step.Key, err)
		}
	}
	attempt, err := client.RelationshipOperationAttempt.Create().
		SetOperationID(operation.ID).
		SetAttemptNumber(1).
		SetDirection(relationshipoperationattempt.DirectionInitial).
		SetInitiatedByUserID(actorID).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("persist Relationship Operation attempt: %w", err)
	}
	return &durableExecution{operationID: operation.ID, attemptID: attempt.ID}, nil
}

func buildDurableStepPlans(plan *Plan) []durableStepPlan {
	steps := make([]durableStepPlan, 0)
	for _, assignment := range plan.Assignments {
		if assignment.TargetGroupID == 0 {
			steps = append(steps, durableStepPlan{Key: fmt.Sprintf("target:%d:create", assignment.Index), Action: "create", RelationshipType: "group", Direction: relationshipoperationstep.DirectionTarget, ExpectedResult: map[string]any{"name": assignment.TargetGroupName, "status": "active"}, ResumeSupported: true})
		}
	}
	for _, target := range plan.TargetSummaries {
		if target.Rename != nil {
			steps = append(steps, durableStepPlan{Key: fmt.Sprintf("target:%d:rename", target.Index), Action: "rename", RelationshipType: "group", Direction: relationshipoperationstep.DirectionTarget, TargetGroupID: target.TargetGroupID, ExpectedResult: map[string]any{"name": target.Rename.ToName}, ResumeSupported: true, RestoreSupported: true})
		}
		for _, account := range target.Accounts {
			steps = append(steps, durableStepPlan{Key: fmt.Sprintf("target:%d:account:%d:%s", target.Index, account.AccountID, account.Action), Action: account.Action, RelationshipType: "account_group", Direction: relationshipoperationstep.DirectionTarget, TargetGroupID: target.TargetGroupID, ReviewedResourceIDs: []int64{account.AccountID}, ReviewedPriority: account.NewPriority, ExpectedResult: map[string]any{"priority": account.NewPriority}, ResumeSupported: true, RestoreSupported: true})
		}
		for _, member := range target.Members {
			steps = append(steps, durableStepPlan{Key: fmt.Sprintf("target:%d:member:%d:%s", target.Index, member.UserID, member.Action), Action: member.Action, RelationshipType: "managed_member", Direction: relationshipoperationstep.DirectionTarget, LocalUserID: member.UserID, RelayUserID: member.RelayUserID, SourceGroupID: member.FromGroupID, TargetGroupID: member.ToGroupID, ExpectedResult: map[string]any{"target_group_id": member.ToGroupID}, ResumeSupported: true, RestoreSupported: true})
		}
		for _, subscription := range target.Subscriptions {
			steps = append(steps, durableStepPlan{Key: fmt.Sprintf("target:%d:subscription:%d:%s:%d", target.Index, subscription.RelayUserID, subscription.Action, subscription.GroupID), Action: subscription.Action, RelationshipType: "subscription", Direction: relationshipoperationstep.DirectionTarget, LocalUserID: subscription.UserID, RelayUserID: subscription.RelayUserID, TargetGroupID: subscription.GroupID, ExpectedResult: map[string]any{"active": subscription.Action == "add"}, ResumeSupported: true, RestoreSupported: true})
		}
		for _, key := range target.APIKeys {
			steps = append(steps, durableStepPlan{Key: fmt.Sprintf("target:%d:api-keys:%d:%d:%d", target.Index, key.RelayUserID, key.FromGroupID, key.ToGroupID), Action: "move", RelationshipType: "api_keys", Direction: relationshipoperationstep.DirectionTarget, LocalUserID: key.UserID, RelayUserID: key.RelayUserID, SourceGroupID: key.FromGroupID, TargetGroupID: key.ToGroupID, ExpectedResult: map[string]any{"count": key.Count, "group_id": key.ToGroupID}, ResumeSupported: true, RestoreSupported: true})
		}
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].Key < steps[j].Key })
	return steps
}

func jsonObject(value any) map[string]any {
	encoded, _ := json.Marshal(value)
	result := map[string]any{}
	_ = json.Unmarshal(encoded, &result)
	return result
}

func jsonFingerprint(value any) string {
	encoded, _ := json.Marshal(value)
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("sha256:%x", sum)
}

func (execution *durableExecution) dispatch(ctx context.Context) error {
	now := time.Now().UTC()
	if _, err := execution.client.RelationshipOperationStep.Update().Where(relationshipoperationstep.OperationIDEQ(execution.operationID)).SetLifecycle(relationshipoperationstep.LifecycleDispatched).Save(ctx); err != nil {
		return fmt.Errorf("mark Relationship Operation steps dispatched: %w", err)
	}
	if _, err := execution.client.RelationshipOperationAttempt.UpdateOneID(execution.attemptID).SetStatus(relationshipoperationattempt.StatusRunning).SetStartedAt(now).Save(ctx); err != nil {
		return fmt.Errorf("start Relationship Operation attempt: %w", err)
	}
	return nil
}

func (execution *durableExecution) verifyStep(ctx context.Context, key string, effect map[string]any) error {
	count, err := execution.client.RelationshipOperationStep.Update().Where(
		relationshipoperationstep.OperationIDEQ(execution.operationID),
		relationshipoperationstep.StepKeyEQ(key),
	).SetLifecycle(relationshipoperationstep.LifecycleReadbackVerified).SetLatestVerifiedEffect(effect).Save(ctx)
	if err != nil {
		return fmt.Errorf("verify Relationship Operation step %q: %w", key, err)
	}
	if count != 1 {
		return fmt.Errorf("verify Relationship Operation step %q: persisted step is missing", key)
	}
	return nil
}

func (execution *durableExecution) finish(ctx context.Context, applied bool, result map[string]any) error {
	now := time.Now().UTC()
	tx, err := execution.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin Relationship Operation finish transaction: %w", err)
	}
	rollback := func(cause error) error {
		_ = tx.Rollback()
		return cause
	}
	if applied {
		if _, err := tx.RelationshipOperationStep.Update().Where(relationshipoperationstep.OperationIDEQ(execution.operationID)).SetLifecycle(relationshipoperationstep.LifecycleReadbackVerified).SetLatestVerifiedEffect(result).Save(ctx); err != nil {
			return rollback(err)
		}
		if _, err := tx.RelationshipOperationMapping.Update().Where(relationshipoperationmapping.OperationIDEQ(execution.operationID)).SetActive(false).SetReleasedAt(now).Save(ctx); err != nil {
			return rollback(err)
		}
		if _, err := tx.RelationshipOperation.UpdateOneID(execution.operationID).SetLifecycle(relationshipoperation.LifecycleApplied).SetTerminalResult(result).SetCompletedAt(now).Save(ctx); err != nil {
			return rollback(err)
		}
		if _, err := tx.RelationshipOperationAttempt.UpdateOneID(execution.attemptID).SetStatus(relationshipoperationattempt.StatusSucceeded).SetResult(result).SetCompletedAt(now).Save(ctx); err != nil {
			return rollback(err)
		}
	} else {
		if _, err := tx.RelationshipOperation.UpdateOneID(execution.operationID).SetLifecycle(relationshipoperation.LifecycleInterrupted).Save(ctx); err != nil {
			return rollback(err)
		}
		if _, err := tx.RelationshipOperationAttempt.UpdateOneID(execution.attemptID).SetStatus(relationshipoperationattempt.StatusFailed).SetResult(result).SetCompletedAt(now).Save(ctx); err != nil {
			return rollback(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Relationship Operation finish transaction: %w", err)
	}
	execution.finished = true
	return nil
}

func (execution *durableExecution) interrupt(ctx context.Context) {
	if execution == nil || execution.finished {
		return
	}
	now := time.Now().UTC()
	_, _ = execution.client.RelationshipOperation.UpdateOneID(execution.operationID).SetLifecycle(relationshipoperation.LifecycleInterrupted).Save(context.WithoutCancel(ctx))
	_, _ = execution.client.RelationshipOperationAttempt.UpdateOneID(execution.attemptID).SetStatus(relationshipoperationattempt.StatusInterrupted).SetCompletedAt(now).Save(context.WithoutCancel(ctx))
}
