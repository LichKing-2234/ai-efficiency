package relayplanning

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relationshipoperation"
	"github.com/ai-efficiency/backend/ent/relationshipoperationattempt"
	"github.com/ai-efficiency/backend/ent/relationshipoperationmapping"
	"github.com/ai-efficiency/backend/ent/relationshipoperationstep"
	"github.com/ai-efficiency/backend/internal/relay"
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
			steps = append(steps, durableStepPlan{Key: fmt.Sprintf("target:%d:rename", target.Index), Action: "rename", RelationshipType: "group", Direction: relationshipoperationstep.DirectionTarget, TargetGroupID: target.TargetGroupID, ExpectedResult: map[string]any{"target_name": target.Rename.ToName, "baseline_name": target.Rename.FromName}, ResumeSupported: true, RestoreSupported: true})
		}
		for _, account := range target.Accounts {
			steps = append(steps, durableStepPlan{Key: fmt.Sprintf("target:%d:account:%d:%s", target.Index, account.AccountID, account.Action), Action: account.Action, RelationshipType: "account_group", Direction: relationshipoperationstep.DirectionTarget, TargetGroupID: target.TargetGroupID, ReviewedResourceIDs: []int64{account.AccountID}, ReviewedPriority: account.NewPriority, ExpectedResult: map[string]any{"target_priority": account.NewPriority, "baseline_priority": account.OldPriority}, ResumeSupported: true, RestoreSupported: true})
		}
		for _, member := range target.Members {
			steps = append(steps, durableStepPlan{Key: fmt.Sprintf("target:%d:member:%d:%s", target.Index, member.UserID, member.Action), Action: member.Action, RelationshipType: "managed_member", Direction: relationshipoperationstep.DirectionTarget, LocalUserID: member.UserID, RelayUserID: member.RelayUserID, SourceGroupID: member.FromGroupID, TargetGroupID: member.ToGroupID, ExpectedResult: map[string]any{"target_group_id": member.ToGroupID}, ResumeSupported: true, RestoreSupported: true})
		}
		for _, subscription := range target.Subscriptions {
			steps = append(steps, durableStepPlan{Key: fmt.Sprintf("target:%d:subscription:%d:%s:%d", target.Index, subscription.RelayUserID, subscription.Action, subscription.GroupID), Action: subscription.Action, RelationshipType: "subscription", Direction: relationshipoperationstep.DirectionTarget, LocalUserID: subscription.UserID, RelayUserID: subscription.RelayUserID, TargetGroupID: subscription.GroupID, ExpectedResult: map[string]any{"target_active": subscription.Action == "add", "baseline_active": subscription.Action != "add"}, ResumeSupported: true, RestoreSupported: true})
		}
		for _, key := range target.APIKeys {
			ids := reviewedAPIKeyIDs(plan.relationshipSnapshot, key.RelayUserID, key.FromGroupID, key.ToGroupID)
			steps = append(steps, durableStepPlan{Key: fmt.Sprintf("target:%d:api-keys:%d:%d:%d", target.Index, key.RelayUserID, key.FromGroupID, key.ToGroupID), Action: "move", RelationshipType: "api_keys", Direction: relationshipoperationstep.DirectionTarget, LocalUserID: key.UserID, RelayUserID: key.RelayUserID, SourceGroupID: key.FromGroupID, TargetGroupID: key.ToGroupID, ReviewedResourceIDs: ids, ExpectedResult: map[string]any{"target_group_id": key.ToGroupID, "baseline_group_id": key.FromGroupID}, ResumeSupported: true, RestoreSupported: true})
		}
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].Key < steps[j].Key })
	return steps
}

func reviewedAPIKeyIDs(snapshot relationshipSnapshot, relayUserID, sourceGroupID, targetGroupID int64) []int64 {
	ids := []int64{}
	for _, user := range snapshot.Users {
		if user.RelayUserID != relayUserID {
			continue
		}
		for _, key := range user.APIKeys {
			if key.GroupID == sourceGroupID || key.GroupID == targetGroupID {
				ids = append(ids, key.ID)
			}
		}
		break
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
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

func (s *Service) verifyInitialOperationSteps(ctx context.Context, provider relay.Provider, execution *durableExecution, platform string) error {
	steps, err := execution.client.RelationshipOperationStep.Query().Where(
		relationshipoperationstep.OperationIDEQ(execution.operationID),
	).All(ctx)
	if err != nil {
		return fmt.Errorf("load Relationship Operation steps for initial readback: %w", err)
	}
	resolveRecoveryStepGroupIDs(steps)
	for _, step := range steps {
		if err := s.convergeRecoveryStep(ctx, provider, step, RecoveryResume, platform); err != nil {
			return fmt.Errorf("verify initial Relationship Operation step %q: %w", step.StepKey, err)
		}
	}
	return nil
}

func (execution *durableExecution) finishInterrupted(ctx context.Context, result map[string]any) error {
	now := time.Now().UTC()
	tx, err := execution.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin Relationship Operation finish transaction: %w", err)
	}
	rollback := func(cause error) error {
		_ = tx.Rollback()
		return cause
	}
	if _, err := tx.RelationshipOperation.UpdateOneID(execution.operationID).SetLifecycle(relationshipoperation.LifecycleInterrupted).Save(ctx); err != nil {
		return rollback(fmt.Errorf("interrupt Relationship Operation: %w", err))
	}
	if _, err := tx.RelationshipOperationAttempt.UpdateOneID(execution.attemptID).SetStatus(relationshipoperationattempt.StatusFailed).SetResult(result).SetCompletedAt(now).Save(ctx); err != nil {
		return rollback(fmt.Errorf("fail Relationship Operation attempt: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Relationship Operation finish transaction: %w", err)
	}
	execution.finished = true
	return nil
}

func (execution *durableExecution) finishApplied(ctx context.Context, client *ent.Client, result map[string]any, now time.Time) error {
	if err := validateVerifiedOperationSteps(ctx, client, execution.operationID, RecoveryResume); err != nil {
		return err
	}
	if _, err := client.RelationshipOperationMapping.Update().Where(relationshipoperationmapping.OperationIDEQ(execution.operationID)).SetActive(false).SetReleasedAt(now).Save(ctx); err != nil {
		return fmt.Errorf("release Relationship Operation ownership: %w", err)
	}
	if _, err := client.RelationshipOperation.UpdateOneID(execution.operationID).SetLifecycle(relationshipoperation.LifecycleApplied).SetTerminalResult(result).SetCompletedAt(now).Save(ctx); err != nil {
		return fmt.Errorf("apply Relationship Operation: %w", err)
	}
	if _, err := client.RelationshipOperationAttempt.UpdateOneID(execution.attemptID).SetStatus(relationshipoperationattempt.StatusSucceeded).SetResult(result).SetCompletedAt(now).Save(ctx); err != nil {
		return fmt.Errorf("complete Relationship Operation attempt: %w", err)
	}
	return nil
}

func validateVerifiedOperationSteps(ctx context.Context, client *ent.Client, operationID int, direction RecoveryDirection) error {
	steps, err := client.RelationshipOperationStep.Query().Where(relationshipoperationstep.OperationIDEQ(operationID)).All(ctx)
	if err != nil {
		return fmt.Errorf("load terminal Relationship Operation steps: %w", err)
	}
	resolveRecoveryStepGroupIDs(steps)
	side := "target"
	if direction == RecoveryRestore {
		side = "baseline"
	}
	for _, step := range steps {
		if step.Lifecycle != relationshipoperationstep.LifecycleReadbackVerified {
			return fmt.Errorf("Relationship Operation step %q is not readback verified", step.StepKey)
		}
		effect := step.LatestVerifiedEffect
		groupID := int64PointerValue(step.TargetGroupID)
		valid := false
		switch step.RelationshipType {
		case "group":
			wantName := stringValue(step.ExpectedResult[side+"_name"])
			if step.Action == "create" {
				wantName = stringValue(step.ExpectedResult["name"])
			}
			effectGroupID, hasGroupID := int64Evidence(effect, "group_id")
			effectName, hasName := stringEvidence(effect, "name")
			valid = hasGroupID && effectGroupID > 0 && hasName && effectName == wantName
		case "account_group":
			accountID, hasAccountID := int64Evidence(effect, "account_id")
			effectGroupID, hasGroupID := int64Evidence(effect, "group_id")
			priority, hasPriority := int64Evidence(effect, "priority")
			valid = len(step.ReviewedResourceIds) == 1 && hasAccountID && accountID == step.ReviewedResourceIds[0] && hasGroupID && effectGroupID == groupID && hasPriority && int(priority) == intValue(step.ExpectedResult[side+"_priority"])
		case "subscription":
			relayUserID, hasRelayUserID := int64Evidence(effect, "relay_user_id")
			effectGroupID, hasGroupID := int64Evidence(effect, "group_id")
			active, hasActive := boolEvidence(effect, "active")
			valid = hasRelayUserID && relayUserID == int64PointerValue(step.RelayUserID) && hasGroupID && effectGroupID == groupID && hasActive && active == boolValue(step.ExpectedResult[side+"_active"])
		case "api_keys":
			wantGroupID := int64Value(step.ExpectedResult[side+"_group_id"])
			if wantGroupID == 0 && side == "target" {
				wantGroupID = groupID
			}
			reviewedIDs, hasReviewedIDs := int64SliceEvidence(effect, "reviewed_api_key_ids")
			effectGroupID, hasGroupID := int64Evidence(effect, "group_id")
			valid = hasReviewedIDs && reflect.DeepEqual(reviewedIDs, step.ReviewedResourceIds) && hasGroupID && effectGroupID == wantGroupID
		case "managed_member":
			effectDirection, hasDirection := stringEvidence(effect, "direction")
			action, hasAction := stringEvidence(effect, "action")
			localUserID, hasLocalUserID := int64Evidence(effect, "local_user_id")
			relayUserID, hasRelayUserID := int64Evidence(effect, "relay_user_id")
			sourceGroupID, hasSourceGroupID := int64Evidence(effect, "source_group_id")
			targetGroupID, hasTargetGroupID := int64Evidence(effect, "target_group_id")
			valid = hasDirection && effectDirection == string(direction) && hasAction && action == step.Action && hasLocalUserID && int(localUserID) == intPointerValue(step.LocalUserID) && hasRelayUserID && relayUserID == int64PointerValue(step.RelayUserID) && hasSourceGroupID && sourceGroupID == int64PointerValue(step.SourceGroupID) && hasTargetGroupID && targetGroupID == groupID
		}
		if !valid {
			return fmt.Errorf("Relationship Operation step %q has mismatched readback evidence", step.StepKey)
		}
	}
	return nil
}

func int64Evidence(effect map[string]any, key string) (int64, bool) {
	value, exists := effect[key]
	if !exists {
		return 0, false
	}
	return int64EvidenceValue(value)
}

func int64EvidenceValue(value any) (int64, bool) {
	switch value.(type) {
	case int, int64, float64, json.Number:
		return int64Value(value), true
	default:
		return 0, false
	}
}

func stringEvidence(effect map[string]any, key string) (string, bool) {
	value, exists := effect[key]
	result, ok := value.(string)
	return result, exists && ok
}

func boolEvidence(effect map[string]any, key string) (bool, bool) {
	value, exists := effect[key]
	result, ok := value.(bool)
	return result, exists && ok
}

func int64SliceEvidence(effect map[string]any, key string) ([]int64, bool) {
	value, exists := effect[key]
	if !exists {
		return nil, false
	}
	items, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]int64); ok {
			return append([]int64(nil), typed...), true
		}
		return nil, false
	}
	result := make([]int64, len(items))
	for index, item := range items {
		value, ok := int64EvidenceValue(item)
		if !ok {
			return nil, false
		}
		result[index] = value
	}
	return result, true
}

func (execution *durableExecution) interrupt(ctx context.Context) {
	if execution == nil || execution.finished {
		return
	}
	now := time.Now().UTC()
	_, _ = execution.client.RelationshipOperation.UpdateOneID(execution.operationID).SetLifecycle(relationshipoperation.LifecycleInterrupted).Save(context.WithoutCancel(ctx))
	_, _ = execution.client.RelationshipOperationAttempt.UpdateOneID(execution.attemptID).SetStatus(relationshipoperationattempt.StatusInterrupted).SetCompletedAt(now).Save(context.WithoutCancel(ctx))
}
