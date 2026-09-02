package relayplanning

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relationshipoperation"
	"github.com/ai-efficiency/backend/ent/relationshipoperationattempt"
	"github.com/ai-efficiency/backend/ent/relationshipoperationmapping"
	"github.com/ai-efficiency/backend/ent/relationshipoperationstep"
	"github.com/ai-efficiency/backend/ent/relaygroupmapping"
	"github.com/ai-efficiency/backend/internal/relay"
)

type RecoveryDirection string

const (
	RecoveryResume  RecoveryDirection = "resume"
	RecoveryRestore RecoveryDirection = "restore"
)

type RecoveryRequest struct {
	OperationID       int
	Direction         RecoveryDirection
	InitiatedByUserID int
}

type RecoveryResult struct {
	OperationID int               `json:"operation_id"`
	Direction   RecoveryDirection `json:"direction"`
	Lifecycle   string            `json:"lifecycle"`
	AttemptID   int               `json:"attempt_id"`
}

type ExternalRecoveryBlockerError struct {
	ResourceType string
	ResourceID   int64
	Relationship string
}

func (e *ExternalRecoveryBlockerError) Error() string {
	return fmt.Sprintf("recovery is blocked by unavailable %s %d for %s", e.ResourceType, e.ResourceID, e.Relationship)
}

func (s *Service) Recover(ctx context.Context, req RecoveryRequest) (*RecoveryResult, error) {
	if req.OperationID <= 0 || (req.Direction != RecoveryResume && req.Direction != RecoveryRestore) {
		return nil, fmt.Errorf("operation ID and recovery direction are required")
	}
	operation, err := s.client.RelationshipOperation.Get(ctx, req.OperationID)
	if err != nil {
		return nil, fmt.Errorf("load Relationship Operation: %w", err)
	}
	if operation.Lifecycle == relationshipoperation.LifecycleBlockedExternal {
		return nil, fmt.Errorf("Relationship Operation is blocked by an external resource change")
	}
	if operation.Lifecycle != relationshipoperation.LifecycleInterrupted {
		return nil, fmt.Errorf("Relationship Operation lifecycle %q cannot recover", operation.Lifecycle)
	}
	if !containsString(operation.SupportedDirections, string(req.Direction)) {
		return nil, fmt.Errorf("Relationship Operation does not support %s", req.Direction)
	}
	steps, err := s.client.RelationshipOperationStep.Query().Where(relationshipoperationstep.OperationIDEQ(operation.ID)).Order(ent.Asc(relationshipoperationstep.FieldID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load Relationship Operation steps: %w", err)
	}
	resolveRecoveryStepGroupIDs(steps)
	for _, step := range steps {
		if req.Direction == RecoveryResume && !step.ResumeSupported || req.Direction == RecoveryRestore && !step.RestoreSupported {
			return nil, fmt.Errorf("step %q does not support %s", step.StepKey, req.Direction)
		}
	}
	attempt, err := s.startRecoveryAttempt(ctx, operation, req)
	if err != nil {
		return nil, err
	}
	provider, err := s.resolver.Resolve(ctx, operation.ProviderID)
	if err != nil {
		_ = s.failRecoveryAttempt(ctx, operation.ID, attempt.ID, fmt.Errorf("resolve Relay provider: %w", err))
		return nil, fmt.Errorf("resolve Relay provider: %w", err)
	}
	if step, blocker, err := preflightRecoveryResources(ctx, provider, operation.Platform, steps); err != nil {
		_ = s.failRecoveryAttempt(ctx, operation.ID, attempt.ID, err)
		return nil, err
	} else if blocker != nil {
		_ = s.blockRecovery(ctx, operation.ID, attempt.ID, step.ID, blocker)
		return nil, blocker
	}
	for _, step := range steps {
		if err := s.convergeRecoveryStep(ctx, provider, step, req.Direction, operation.Platform); err != nil {
			var blocker *ExternalRecoveryBlockerError
			if ent.IsNotFound(err) {
				blocker = &ExternalRecoveryBlockerError{ResourceType: step.RelationshipType, Relationship: step.StepKey}
			} else if candidate, ok := err.(*ExternalRecoveryBlockerError); ok {
				blocker = candidate
			}
			if blocker != nil {
				_ = s.blockRecovery(ctx, operation.ID, attempt.ID, step.ID, blocker)
				return nil, blocker
			}
			_ = s.failRecoveryAttempt(ctx, operation.ID, attempt.ID, err)
			return nil, fmt.Errorf("recover step %q: %w", step.StepKey, err)
		}
	}
	if req.Direction == RecoveryResume {
		if err := s.promoteRecoveryTarget(ctx, operation, attempt.ID); err != nil {
			_ = s.failRecoveryAttempt(ctx, operation.ID, attempt.ID, err)
			return nil, err
		}
		return &RecoveryResult{OperationID: operation.ID, Direction: req.Direction, Lifecycle: string(relationshipoperation.LifecycleApplied), AttemptID: attempt.ID}, nil
	}
	lifecycle := relationshipoperation.LifecycleRestored
	if err := s.finishRecovery(ctx, operation.ID, attempt.ID, lifecycle, req.Direction); err != nil {
		_ = s.failRecoveryAttempt(ctx, operation.ID, attempt.ID, err)
		return nil, err
	}
	return &RecoveryResult{OperationID: operation.ID, Direction: req.Direction, Lifecycle: string(lifecycle), AttemptID: attempt.ID}, nil
}

func resolveRecoveryStepGroupIDs(steps []*ent.RelationshipOperationStep) {
	created := map[string]int64{}
	for _, step := range steps {
		if step.RelationshipType == "group" && step.Action == "create" {
			created[recoveryTargetPrefix(step.StepKey)] = int64Value(step.LatestVerifiedEffect["group_id"])
		}
	}
	for _, step := range steps {
		if int64PointerValue(step.TargetGroupID) > 0 {
			continue
		}
		if groupID := created[recoveryTargetPrefix(step.StepKey)]; groupID > 0 {
			step.TargetGroupID = &groupID
		}
	}
}

func recoveryTargetPrefix(stepKey string) string {
	parts := strings.SplitN(stepKey, ":", 3)
	if len(parts) < 2 || parts[0] != "target" {
		return ""
	}
	return parts[0] + ":" + parts[1]
}

func preflightRecoveryResources(ctx context.Context, provider relay.Provider, platform string, steps []*ent.RelationshipOperationStep) (*ent.RelationshipOperationStep, *ExternalRecoveryBlockerError, error) {
	groupIDs := map[int64]*ent.RelationshipOperationStep{}
	accountIDs := map[int64]*ent.RelationshipOperationStep{}
	keySteps := map[int64][]*ent.RelationshipOperationStep{}
	for _, step := range steps {
		id := int64PointerValue(step.TargetGroupID)
		if id == 0 && step.RelationshipType == "group" {
			id = int64Value(step.LatestVerifiedEffect["group_id"])
		}
		if id > 0 {
			groupIDs[id] = step
		}
		if step.RelationshipType == "account_group" {
			for _, id := range step.ReviewedResourceIds {
				accountIDs[id] = step
			}
		}
		if step.RelationshipType == "api_keys" {
			userID := int64PointerValue(step.RelayUserID)
			keySteps[userID] = append(keySteps[userID], step)
		}
	}
	if len(groupIDs) > 0 {
		reader, ok := provider.(relay.GroupReader)
		if !ok {
			return nil, nil, fmt.Errorf("Relay provider does not support Group recovery readback")
		}
		for id, step := range groupIDs {
			group, err := reader.GetGroup(ctx, id)
			if err != nil {
				if isNotFoundError(err) {
					return step, &ExternalRecoveryBlockerError{ResourceType: "group", ResourceID: id, Relationship: step.StepKey}, nil
				}
				return nil, nil, err
			}
			if group == nil {
				return step, &ExternalRecoveryBlockerError{ResourceType: "group", ResourceID: id, Relationship: step.StepKey}, nil
			}
		}
	}
	if len(accountIDs) > 0 {
		reader, ok := provider.(relay.AccountRelationshipReader)
		if !ok {
			return nil, nil, fmt.Errorf("Relay provider does not support Account recovery readback")
		}
		accounts, err := reader.ListAccountsForPlatform(ctx, platform)
		if err != nil {
			return nil, nil, err
		}
		for id, step := range accountIDs {
			if _, found := accountByID(accounts, id); !found {
				return step, &ExternalRecoveryBlockerError{ResourceType: "account", ResourceID: id, Relationship: step.StepKey}, nil
			}
		}
	}
	for userID, userSteps := range keySteps {
		keys, err := provider.ListUserAPIKeys(ctx, userID)
		if err != nil {
			return nil, nil, err
		}
		available := map[int64]struct{}{}
		for _, key := range keys {
			available[key.ID] = struct{}{}
		}
		for _, step := range userSteps {
			for _, id := range step.ReviewedResourceIds {
				if _, found := available[id]; !found {
					return step, &ExternalRecoveryBlockerError{ResourceType: "api_key", ResourceID: id, Relationship: step.StepKey}, nil
				}
			}
		}
	}
	return nil, nil, nil
}

func (s *Service) startRecoveryAttempt(ctx context.Context, operation *ent.RelationshipOperation, req RecoveryRequest) (*ent.RelationshipOperationAttempt, error) {
	actorID := req.InitiatedByUserID
	if actorID <= 0 {
		actorID = operation.InitiatedByUserID
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	count, err := tx.RelationshipOperationAttempt.Query().Where(relationshipoperationattempt.OperationIDEQ(operation.ID)).Count(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	direction := relationshipoperationattempt.DirectionResume
	lifecycle := relationshipoperation.LifecycleResuming
	if req.Direction == RecoveryRestore {
		direction = relationshipoperationattempt.DirectionRestore
		lifecycle = relationshipoperation.LifecycleRestoring
	}
	now := time.Now().UTC()
	updated, err := tx.RelationshipOperation.Update().Where(
		relationshipoperation.IDEQ(operation.ID),
		relationshipoperation.LifecycleEQ(relationshipoperation.LifecycleInterrupted),
	).SetLifecycle(lifecycle).Save(ctx)
	if err == nil && updated != 1 {
		err = fmt.Errorf("Relationship Operation recovery is already in progress")
	}
	var attempt *ent.RelationshipOperationAttempt
	if err == nil {
		attempt, err = tx.RelationshipOperationAttempt.Create().SetOperationID(operation.ID).SetAttemptNumber(count + 1).SetDirection(direction).SetInitiatedByUserID(actorID).SetStatus(relationshipoperationattempt.StatusRunning).SetStartedAt(now).Save(ctx)
	}
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("start recovery attempt: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit recovery attempt: %w", err)
	}
	return attempt, nil
}

func (s *Service) convergeRecoveryStep(ctx context.Context, provider relay.Provider, step *ent.RelationshipOperationStep, direction RecoveryDirection, platform string) error {
	if _, err := s.client.RelationshipOperationStep.UpdateOneID(step.ID).SetLifecycle(relationshipoperationstep.LifecycleDispatched).Save(ctx); err != nil {
		return err
	}
	desiredSide := "target"
	if direction == RecoveryRestore {
		desiredSide = "baseline"
	}
	var effect map[string]any
	var err error
	switch step.RelationshipType {
	case "managed_member":
		effect = map[string]any{
			"direction":       string(direction),
			"action":          step.Action,
			"local_user_id":   intPointerValue(step.LocalUserID),
			"relay_user_id":   int64PointerValue(step.RelayUserID),
			"source_group_id": int64PointerValue(step.SourceGroupID),
			"target_group_id": int64PointerValue(step.TargetGroupID),
		}
	case "group":
		effect, err = recoverGroupStep(ctx, provider, step, desiredSide)
	case "account_group":
		effect, err = recoverAccountStep(ctx, provider, step, desiredSide, platform)
	case "subscription":
		effect, err = recoverSubscriptionStep(ctx, provider, step, desiredSide)
	case "api_keys":
		effect, err = recoverAPIKeyStep(ctx, provider, step, desiredSide)
	default:
		err = fmt.Errorf("unsupported relationship type %q", step.RelationshipType)
	}
	if err != nil {
		_, _ = s.client.RelationshipOperationStep.UpdateOneID(step.ID).SetLifecycle(relationshipoperationstep.LifecycleFailed).Save(context.WithoutCancel(ctx))
		return err
	}
	_, err = s.client.RelationshipOperationStep.UpdateOneID(step.ID).SetLifecycle(relationshipoperationstep.LifecycleReadbackVerified).SetLatestVerifiedEffect(effect).Save(ctx)
	return err
}

func recoverGroupStep(ctx context.Context, provider relay.Provider, step *ent.RelationshipOperationStep, side string) (map[string]any, error) {
	reader, ok := provider.(relay.GroupReader)
	if !ok {
		return nil, fmt.Errorf("Relay provider does not support Group readback")
	}
	groupID := int64PointerValue(step.TargetGroupID)
	if groupID == 0 {
		groupID = int64Value(step.LatestVerifiedEffect["group_id"])
	}
	group, err := reader.GetGroup(ctx, groupID)
	if err != nil {
		if isNotFoundError(err) {
			return nil, &ExternalRecoveryBlockerError{ResourceType: "group", ResourceID: groupID, Relationship: step.StepKey}
		}
		return nil, err
	}
	if group == nil {
		return nil, &ExternalRecoveryBlockerError{ResourceType: "group", ResourceID: groupID, Relationship: step.StepKey}
	}
	if step.Action == "create" {
		want := stringValue(step.ExpectedResult["name"])
		if group.Name != want {
			renamer, ok := provider.(relay.GroupRenamer)
			if !ok {
				return nil, fmt.Errorf("Relay provider does not support Group rename")
			}
			if _, err := renamer.RenameGroup(ctx, group.ID, want); err != nil {
				return nil, err
			}
			group, err = reader.GetGroup(ctx, group.ID)
			if err != nil || group == nil || group.Name != want {
				return nil, fmt.Errorf("Group create readback did not match")
			}
		}
		return map[string]any{"group_id": group.ID, "name": group.Name}, nil
	}
	want := stringValue(step.ExpectedResult[side+"_name"])
	if group.Name != want {
		renamer, ok := provider.(relay.GroupRenamer)
		if !ok {
			return nil, fmt.Errorf("Relay provider does not support Group rename")
		}
		if _, err := renamer.RenameGroup(ctx, group.ID, want); err != nil {
			return nil, err
		}
		group, err = reader.GetGroup(ctx, group.ID)
		if err != nil || group == nil || group.Name != want {
			return nil, fmt.Errorf("Group rename readback did not match")
		}
	}
	return map[string]any{"group_id": group.ID, "name": group.Name}, nil
}

func recoverAccountStep(ctx context.Context, provider relay.Provider, step *ent.RelationshipOperationStep, side, platform string) (map[string]any, error) {
	reader, readOK := provider.(relay.AccountRelationshipReader)
	updater, writeOK := provider.(relay.AccountRelationshipUpdater)
	if !readOK || !writeOK || len(step.ReviewedResourceIds) != 1 {
		return nil, fmt.Errorf("Relay provider does not support exact Account recovery")
	}
	accountID := step.ReviewedResourceIds[0]
	accounts, err := reader.ListAccountsForPlatform(ctx, platform)
	if err != nil {
		return nil, err
	}
	account, found := accountByID(accounts, accountID)
	if !found {
		return nil, &ExternalRecoveryBlockerError{ResourceType: "account", ResourceID: accountID, Relationship: step.StepKey}
	}
	want := intValue(step.ExpectedResult[side+"_priority"])
	groupID := int64PointerValue(step.TargetGroupID)
	if accountRelationshipPriority(account.GroupRelationships, groupID) != want {
		var desired *int
		if want > 0 {
			desired = intPointer(want)
		}
		if err := updater.SetAccountGroupRelationship(ctx, accountID, groupID, account.GroupRelationships, desired); err != nil {
			return nil, err
		}
		accounts, err = reader.ListAccountsForPlatform(ctx, platform)
		if err != nil {
			return nil, err
		}
		account, found = accountByID(accounts, accountID)
		if !found || accountRelationshipPriority(account.GroupRelationships, groupID) != want {
			return nil, fmt.Errorf("Account relationship readback did not match")
		}
	}
	return map[string]any{"account_id": accountID, "group_id": groupID, "priority": want}, nil
}

func recoverSubscriptionStep(ctx context.Context, provider relay.Provider, step *ent.RelationshipOperationStep, side string) (map[string]any, error) {
	lister, ok := provider.(relay.UserSubscriptionLister)
	if !ok {
		return nil, fmt.Errorf("Relay provider does not support subscription readback")
	}
	want := boolValue(step.ExpectedResult[side+"_active"])
	relayUserID, groupID := int64PointerValue(step.RelayUserID), int64PointerValue(step.TargetGroupID)
	active, err := activeSubscription(ctx, lister, relayUserID, groupID)
	if err != nil {
		return nil, err
	}
	if active != want {
		if want {
			writer, ok := provider.(subscriptionAssigner)
			if !ok {
				return nil, fmt.Errorf("Relay provider does not support subscription assignment")
			}
			if err := writer.AssignSubscriptionForUser(ctx, relayUserID, groupID, defaultValidityDays); err != nil && !isAlreadyAssignedError(err) {
				return nil, err
			}
		} else {
			writer, ok := provider.(subscriptionRemover)
			if !ok {
				return nil, fmt.Errorf("Relay provider does not support subscription removal")
			}
			if err := writer.RemoveSubscriptionForUser(ctx, relayUserID, groupID); err != nil && !isNotFoundError(err) {
				return nil, err
			}
		}
		active, err = activeSubscription(ctx, lister, relayUserID, groupID)
		if err != nil || active != want {
			return nil, fmt.Errorf("subscription readback did not match")
		}
	}
	return map[string]any{"relay_user_id": relayUserID, "group_id": groupID, "active": want}, nil
}

func recoverAPIKeyStep(ctx context.Context, provider relay.Provider, step *ent.RelationshipOperationStep, side string) (map[string]any, error) {
	binder, ok := provider.(relay.APIKeyGroupBinder)
	if !ok {
		return nil, fmt.Errorf("Relay provider does not support API Key binding")
	}
	want := int64Value(step.ExpectedResult[side+"_group_id"])
	if want == 0 && side == "target" {
		want = int64PointerValue(step.TargetGroupID)
	}
	relayUserID := int64PointerValue(step.RelayUserID)
	keys, err := provider.ListUserAPIKeys(ctx, relayUserID)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]relay.APIKey, len(keys))
	for _, key := range keys {
		byID[key.ID] = key
	}
	for _, keyID := range step.ReviewedResourceIds {
		key, found := byID[keyID]
		if !found {
			return nil, &ExternalRecoveryBlockerError{ResourceType: "api_key", ResourceID: keyID, Relationship: step.StepKey}
		}
		if apiKeyGroupID(key) != want {
			if err := binder.BindAPIKeyToGroup(ctx, keyID, want); err != nil {
				return nil, err
			}
		}
	}
	keys, err = provider.ListUserAPIKeys(ctx, relayUserID)
	if err != nil {
		return nil, err
	}
	byID = make(map[int64]relay.APIKey, len(keys))
	for _, key := range keys {
		byID[key.ID] = key
	}
	for _, keyID := range step.ReviewedResourceIds {
		if key, found := byID[keyID]; !found || apiKeyGroupID(key) != want {
			return nil, fmt.Errorf("API Key readback did not match")
		}
	}
	return map[string]any{"reviewed_api_key_ids": append([]int64(nil), step.ReviewedResourceIds...), "group_id": want}, nil
}

func (s *Service) promoteRecoveryTarget(ctx context.Context, operation *ent.RelationshipOperation, attemptID int) error {
	var plan Plan
	if err := decodeJSONMap(operation.TargetSnapshot, &plan); err != nil {
		return fmt.Errorf("decode recovery target: %w", err)
	}
	owners, err := s.client.RelationshipOperationMapping.Query().Where(relationshipoperationmapping.OperationIDEQ(operation.ID)).All(ctx)
	if err != nil {
		return err
	}
	steps, err := s.client.RelationshipOperationStep.Query().Where(relationshipoperationstep.OperationIDEQ(operation.ID)).All(ctx)
	if err != nil {
		return err
	}
	for _, assignment := range plan.Assignments {
		if assignment.TargetGroupID != 0 {
			continue
		}
		key := fmt.Sprintf("target:%d:create", assignment.Index)
		for _, step := range steps {
			if step.StepKey == key {
				assignment.TargetGroupID = int64Value(step.LatestVerifiedEffect["group_id"])
			}
		}
	}
	groupIDs := make([]int64, len(plan.Assignments))
	for index := range plan.Assignments {
		groupIDs[index] = plan.Assignments[index].TargetGroupID
	}
	state := map[string]map[string]string{"operation": {"key": operation.OperationKey, "status": "succeeded"}}
	alreadyPromoted, err := s.recoveryTargetAlreadyPromoted(ctx, owners, &plan, groupIDs)
	if err != nil {
		return err
	}
	if alreadyPromoted {
		return s.finishRecovery(ctx, operation.ID, attemptID, relationshipoperation.LifecycleApplied, RecoveryResume)
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	rollback := func(cause error) error { _ = tx.Rollback(); return cause }
	result, err := saveMappingWithClient(ctx, tx.Client(), &plan, groupIDs, state)
	if err != nil {
		return rollback(err)
	}
	primaryRevision := int64(0)
	for _, owner := range owners {
		if owner.MappingID == result.ID {
			primaryRevision = owner.BaselineRevision
			break
		}
	}
	updated, err := tx.RelayGroupMapping.Update().Where(relaygroupmapping.IDEQ(result.ID), relaygroupmapping.BaselineRevisionEQ(primaryRevision)).AddBaselineRevision(1).Save(ctx)
	if err != nil || updated != 1 {
		return rollback(fmt.Errorf("promote recovery target baseline: revision changed"))
	}
	targetUsers := map[string]struct{}{}
	for _, assignment := range plan.Assignments {
		for _, userID := range assignment.UserIDs {
			targetUsers[strconv.Itoa(userID)] = struct{}{}
		}
	}
	for _, owner := range owners {
		if owner.MappingID == result.ID {
			continue
		}
		var baseline Mapping
		if err := decodeJSONMap(owner.BaselineSnapshot, &baseline); err != nil {
			return rollback(err)
		}
		assignments := cloneInt64Map(baseline.MemberAssignments)
		sources := cloneInt64Map(baseline.MemberSources)
		for userID := range targetUsers {
			delete(assignments, userID)
			delete(sources, userID)
		}
		updated, err := tx.RelayGroupMapping.Update().Where(relaygroupmapping.IDEQ(owner.MappingID), relaygroupmapping.BaselineRevisionEQ(owner.BaselineRevision)).SetMemberAssignments(assignments).SetMemberSources(sources).AddBaselineRevision(1).Save(ctx)
		if err != nil || updated != 1 {
			return rollback(fmt.Errorf("promote affected Mapping %d: revision changed", owner.MappingID))
		}
	}
	now := time.Now().UTC()
	resultFact := map[string]any{"direction": string(RecoveryResume), "status": string(relationshipoperation.LifecycleApplied)}
	if err := finishRecoveryWithClient(ctx, tx.Client(), operation.ID, attemptID, relationshipoperation.LifecycleApplied, RecoveryResume, resultFact, now); err != nil {
		return rollback(err)
	}
	return tx.Commit()
}

func (s *Service) recoveryTargetAlreadyPromoted(ctx context.Context, owners []*ent.RelationshipOperationMapping, plan *Plan, groupIDs []int64) (bool, error) {
	current := make(map[int]Mapping, len(owners))
	baselineCount, promotedCount := 0, 0
	for _, owner := range owners {
		row, err := s.client.RelayGroupMapping.Get(ctx, owner.MappingID)
		if err != nil {
			return false, err
		}
		current[owner.MappingID] = mappingFromEnt(row)
		switch row.BaselineRevision {
		case owner.BaselineRevision:
			baselineCount++
		case owner.BaselineRevision + 1:
			promotedCount++
		default:
			return false, fmt.Errorf("Mapping %d baseline revision changed outside the Operation", owner.MappingID)
		}
	}
	if baselineCount == len(owners) {
		return false, nil
	}
	if promotedCount != len(owners) {
		return false, fmt.Errorf("Relationship Operation baseline promotion is incomplete")
	}
	expectedAssignments := map[string]int64{}
	expectedSources := map[string]int64{}
	for _, assignment := range plan.Assignments {
		if assignment.Index < 0 || assignment.Index >= len(groupIDs) || groupIDs[assignment.Index] <= 0 {
			continue
		}
		for _, userID := range assignment.UserIDs {
			key := strconv.Itoa(userID)
			expectedAssignments[key] = groupIDs[assignment.Index]
			if candidate := candidateByUserID(plan.Candidates, userID); candidate != nil && candidate.SourceGroupID > 0 {
				expectedSources[key] = candidate.SourceGroupID
			}
		}
	}
	targetUsers := map[string]struct{}{}
	for userID := range expectedAssignments {
		targetUsers[userID] = struct{}{}
	}
	for _, owner := range owners {
		mapping := current[owner.MappingID]
		if owner.MappingID == plan.MappingID {
			if !reflect.DeepEqual(mapping.GroupIDs, groupIDs) || !reflect.DeepEqual(mapping.MemberAssignments, expectedAssignments) || !reflect.DeepEqual(mapping.MemberSources, expectedSources) {
				return false, fmt.Errorf("promoted primary Mapping does not match the reviewed Target")
			}
			if plan.AccountsReviewed && !reflect.DeepEqual(mapping.DesiredAccounts, desiredAccountsForGroupIDs(plan.Assignments, groupIDs)) {
				return false, fmt.Errorf("promoted Account baseline does not match the reviewed Target")
			}
			continue
		}
		var baseline Mapping
		if err := decodeJSONMap(owner.BaselineSnapshot, &baseline); err != nil {
			return false, err
		}
		expected := cloneInt64Map(baseline.MemberAssignments)
		expectedMemberSources := cloneInt64Map(baseline.MemberSources)
		for userID := range targetUsers {
			delete(expected, userID)
			delete(expectedMemberSources, userID)
		}
		if !reflect.DeepEqual(mapping.MemberAssignments, expected) || !reflect.DeepEqual(mapping.MemberSources, expectedMemberSources) {
			return false, fmt.Errorf("promoted affected Mapping %d does not match the reviewed Target", owner.MappingID)
		}
	}
	return true, nil
}

func (s *Service) finishRecovery(ctx context.Context, operationID, attemptID int, lifecycle relationshipoperation.Lifecycle, direction RecoveryDirection) error {
	now := time.Now().UTC()
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	result := map[string]any{"direction": string(direction), "status": string(lifecycle)}
	if err := finishRecoveryWithClient(ctx, tx.Client(), operationID, attemptID, lifecycle, direction, result, now); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func finishRecoveryWithClient(ctx context.Context, client *ent.Client, operationID, attemptID int, lifecycle relationshipoperation.Lifecycle, direction RecoveryDirection, result map[string]any, now time.Time) error {
	if err := validateVerifiedOperationSteps(ctx, client, operationID, direction); err != nil {
		return err
	}
	if _, err := client.RelationshipOperationMapping.Update().Where(relationshipoperationmapping.OperationIDEQ(operationID)).SetActive(false).SetReleasedAt(now).Save(ctx); err != nil {
		return fmt.Errorf("release recovery ownership: %w", err)
	}
	if _, err := client.RelationshipOperation.UpdateOneID(operationID).SetLifecycle(lifecycle).SetTerminalResult(result).SetCompletedAt(now).Save(ctx); err != nil {
		return fmt.Errorf("complete recovery Operation: %w", err)
	}
	if _, err := client.RelationshipOperationAttempt.UpdateOneID(attemptID).SetStatus(relationshipoperationattempt.StatusSucceeded).SetResult(result).SetCompletedAt(now).Save(ctx); err != nil {
		return fmt.Errorf("complete recovery attempt: %w", err)
	}
	return nil
}

func (s *Service) failRecoveryAttempt(ctx context.Context, operationID, attemptID int, cause error) error {
	_ = cause
	now := time.Now().UTC()
	tx, err := s.client.Tx(context.WithoutCancel(ctx))
	if err != nil {
		return err
	}
	_, err = tx.RelationshipOperation.UpdateOneID(operationID).SetLifecycle(relationshipoperation.LifecycleInterrupted).Save(context.WithoutCancel(ctx))
	if err == nil {
		_, err = tx.RelationshipOperationAttempt.UpdateOneID(attemptID).SetStatus(relationshipoperationattempt.StatusFailed).SetErrorMessage("recovery attempt failed").SetCompletedAt(now).Save(context.WithoutCancel(ctx))
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Service) blockRecovery(ctx context.Context, operationID, attemptID, stepID int, blocker *ExternalRecoveryBlockerError) error {
	now := time.Now().UTC()
	fact := map[string]any{"resource_type": blocker.ResourceType, "resource_id": blocker.ResourceID, "relationship": blocker.Relationship}
	tx, err := s.client.Tx(context.WithoutCancel(ctx))
	if err != nil {
		return err
	}
	_, err = tx.RelationshipOperationStep.UpdateOneID(stepID).SetLifecycle(relationshipoperationstep.LifecycleBlockedExternal).Save(context.WithoutCancel(ctx))
	if err == nil {
		_, err = tx.RelationshipOperation.UpdateOneID(operationID).SetLifecycle(relationshipoperation.LifecycleBlockedExternal).SetExternalBlocker(fact).Save(context.WithoutCancel(ctx))
	}
	if err == nil {
		_, err = tx.RelationshipOperationAttempt.UpdateOneID(attemptID).SetStatus(relationshipoperationattempt.StatusBlockedExternal).SetResult(fact).SetCompletedAt(now).Save(context.WithoutCancel(ctx))
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func activeSubscription(ctx context.Context, lister relay.UserSubscriptionLister, userID, groupID int64) (bool, error) {
	items, err := lister.ListUserSubscriptions(ctx, userID)
	if err != nil {
		return false, err
	}
	for _, item := range items {
		id := item.GroupID
		if id == 0 && item.Group != nil {
			id = item.Group.ID
		}
		if id == groupID && strings.EqualFold(item.Status, "active") {
			return true, nil
		}
	}
	return false, nil
}

func accountByID(items []relay.Account, id int64) (relay.Account, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return relay.Account{}, false
}

func decodeJSONMap(value map[string]any, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func stringValue(value any) string { result, _ := value.(string); return result }
func boolValue(value any) bool     { result, _ := value.(bool); return result }
func intValue(value any) int       { return int(int64Value(value)) }
func int64Value(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		result, _ := typed.Int64()
		return result
	default:
		return 0
	}
}

func int64PointerValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func intPointerValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
