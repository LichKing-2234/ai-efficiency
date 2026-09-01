package relayplanning

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relationshipoperation"
	"github.com/ai-efficiency/backend/ent/relationshipoperationattempt"
	"github.com/ai-efficiency/backend/ent/relationshipoperationmapping"
	"github.com/ai-efficiency/backend/ent/relaygroupmapping"
)

type LegacyMigrationClassification string

const (
	LegacyAligned                  LegacyMigrationClassification = "aligned"
	LegacyReconstructibleCandidate LegacyMigrationClassification = "reconstructible_candidate"
	LegacyMigratedResumeOnly       LegacyMigrationClassification = "migrated_resume_only"
	LegacyBlockedManualReview      LegacyMigrationClassification = "blocked_manual_review"
	LegacyAlreadyOwned             LegacyMigrationClassification = "already_owned"
)

type LegacyMigrationItem struct {
	MappingID      int                           `json:"mapping_id"`
	Classification LegacyMigrationClassification `json:"classification"`
	Reason         string                        `json:"reason,omitempty"`
}

type LegacyMigrationReport struct {
	Apply  bool                  `json:"apply"`
	Counts map[string]int        `json:"counts"`
	Items  []LegacyMigrationItem `json:"items"`
}

type LegacyMigrationRequest struct {
	Apply             bool
	InitiatedByUserID int
}

func (s *Service) AuditLegacyOperations(ctx context.Context) (*LegacyMigrationReport, error) {
	return s.migrateLegacyOperations(ctx, LegacyMigrationRequest{})
}

func (s *Service) MigrateLegacyOperations(ctx context.Context, req LegacyMigrationRequest) (*LegacyMigrationReport, error) {
	if req.Apply && req.InitiatedByUserID <= 0 {
		return nil, fmt.Errorf("initiating administrator is required")
	}
	return s.migrateLegacyOperations(ctx, req)
}

func (s *Service) migrateLegacyOperations(ctx context.Context, req LegacyMigrationRequest) (*LegacyMigrationReport, error) {
	rows, err := s.client.RelayGroupMapping.Query().Order(ent.Asc(relaygroupmapping.FieldID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list legacy Relay Mappings: %w", err)
	}
	report := &LegacyMigrationReport{Apply: req.Apply, Counts: map[string]int{}, Items: []LegacyMigrationItem{}}
	for _, row := range rows {
		item := LegacyMigrationItem{MappingID: row.ID}
		owned, err := s.client.RelationshipOperationMapping.Query().Where(relationshipoperationmapping.MappingIDEQ(row.ID), relationshipoperationmapping.ActiveEQ(true)).Exist(ctx)
		if err != nil {
			return nil, err
		}
		if owned {
			item.Classification = LegacyAlreadyOwned
		} else if row.Status != "needs_retry" && operationStatus(row.OperationState) != "needs_retry" {
			item.Classification = LegacyAligned
		} else if reason := structuralLegacyAmbiguity(row.OperationState); reason != "" {
			item.Classification, item.Reason = LegacyBlockedManualReview, reason
		} else {
			item.Classification = LegacyReconstructibleCandidate
			if req.Apply {
				mapping := mappingFromEnt(row)
				operation, reason, migrateErr := s.migrateExactLegacyMapping(ctx, &mapping, req.InitiatedByUserID)
				if migrateErr != nil {
					return nil, migrateErr
				}
				if operation != nil {
					item.Classification = LegacyMigratedResumeOnly
				} else {
					item.Classification, item.Reason = LegacyBlockedManualReview, reason
					if err := s.persistBlockedLegacyOperation(ctx, mapping, req.InitiatedByUserID, reason); err != nil {
						return nil, err
					}
				}
			}
		}
		if req.Apply && item.Classification == LegacyBlockedManualReview && !owned {
			mapping := mappingFromEnt(row)
			if err := s.persistBlockedLegacyOperation(ctx, mapping, req.InitiatedByUserID, item.Reason); err != nil {
				return nil, err
			}
		}
		report.Items = append(report.Items, item)
		report.Counts[string(item.Classification)]++
	}
	return report, nil
}

func structuralLegacyAmbiguity(state map[string]map[string]string) string {
	operation := state["operation"]
	if operation == nil || operation["intent_hash"] == "" {
		return "missing_intent_identity"
	}
	found := false
	for key, entry := range state {
		if !operationStateNeedsRetry(state, key) || key == "operation" {
			continue
		}
		if !strings.HasPrefix(key, "member:") {
			return "unsupported_legacy_effect"
		}
		found = true
		if entry["step_identity"] == "" {
			return "missing_step_identity"
		}
		if _, frozen := entry["reviewed_api_key_ids"]; !frozen {
			return "missing_reviewed_resource_set"
		}
		if _, err := strconv.Atoi(strings.TrimPrefix(key, "member:")); err != nil {
			return "malformed_member_identity"
		}
		if entry["action"] == "move_here" || entry["action"] == "add_additionally" {
			return "cross_mapping_or_additional_ownership"
		}
	}
	if !found {
		return "missing_retry_effect"
	}
	return ""
}

func (s *Service) migrateExactLegacyMapping(ctx context.Context, mapping *Mapping, actorID int) (*ent.RelationshipOperation, string, error) {
	removed := legacyRemovedUserIDs(mapping.OperationState)
	actions := memberActionsWithRetries(mapping.OperationState, nil)
	sources, _, err := memberSourcesWithRemovalRetries(mapping.OperationState, nil, removed)
	if err != nil {
		return nil, "malformed_removal_source", nil
	}
	plan, err := s.Replan(ctx, mapping.ID, nil, nil, sources, removed, actions, nil)
	if err != nil {
		return nil, "unverifiable_relay_readback", nil
	}
	req := ExecuteRequest{PreviewRequest: PreviewRequest{Assignments: plan.Assignments, MemberSources: sources, RemovedUserIDs: removed, MemberActions: actions, ExistingMappingID: mapping.ID}}
	intentHash, members, err := buildLegacyReplanIntent(mapping, plan, req)
	if err != nil || intentHash != mapping.OperationState["operation"]["intent_hash"] {
		return nil, "intent_identity_mismatch", nil
	}
	for userID, intent := range members {
		entry := mapping.OperationState["member:"+strconv.Itoa(userID)]
		if operationStateNeedsRetry(mapping.OperationState, "member:"+strconv.Itoa(userID)) && entry["step_identity"] != legacyMemberStepIdentity(intent) {
			return nil, "step_identity_mismatch", nil
		}
	}
	if err := validateLegacyRetryReadback(mapping, plan, members); err != nil {
		return nil, "reviewed_resource_readback_mismatch", nil
	}
	baseline := reconstructLegacyBaseline(*mapping, members)
	steps := legacyDurableStepPlans(members)
	if len(steps) == 0 {
		return nil, "missing_effect_graph", nil
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, "", err
	}
	operationKey := fmt.Sprintf("legacy-migration:%d:%s", mapping.ID, strings.TrimPrefix(intentHash, "v1:")[:12])
	execution, err := persistDurableExecution(ctx, tx.Client(), []Mapping{baseline}, plan, ExecuteRequest{OperationKey: operationKey, InitiatedByUserID: actorID}, steps)
	if err == nil {
		_, err = tx.RelationshipOperation.UpdateOneID(execution.operationID).SetLifecycle(relationshipoperation.LifecycleInterrupted).Save(ctx)
	}
	if err == nil {
		_, err = tx.RelationshipOperationAttempt.UpdateOneID(execution.attemptID).SetStatus(relationshipoperationattempt.StatusInterrupted).SetErrorMessage("migrated legacy retry requires explicit Resume Preview").SetCompletedAt(time.Now().UTC()).Save(ctx)
	}
	if err != nil {
		_ = tx.Rollback()
		return nil, "", err
	}
	if err := tx.Commit(); err != nil {
		return nil, "", err
	}
	operation, err := s.client.RelationshipOperation.Get(ctx, execution.operationID)
	return operation, "", err
}

func legacyDurableStepPlans(members map[int]legacyMemberIntent) []durableStepPlan {
	userIDs := make([]int, 0, len(members))
	for userID := range members {
		userIDs = append(userIDs, userID)
	}
	sort.Ints(userIDs)
	steps := []durableStepPlan{}
	for _, userID := range userIDs {
		intent := members[userID]
		prefix := fmt.Sprintf("legacy:member:%d", userID)
		steps = append(steps, durableStepPlan{Key: prefix + ":intent", Action: intent.Action, RelationshipType: "managed_member", Direction: "target", LocalUserID: userID, RelayUserID: intent.RelayUserID, SourceGroupID: intent.SourceGroupID, TargetGroupID: intent.TargetGroupID, ReviewedResourceIDs: append([]int64(nil), intent.APIKeyIDs...), ExpectedResult: map[string]any{"target_group_id": intent.TargetGroupID, "baseline_group_id": intent.SourceGroupID}, ResumeSupported: true})
		if intent.Action == "remove" {
			steps = append(steps, durableStepPlan{Key: prefix + ":subscription:target", Action: "remove", RelationshipType: "subscription", Direction: "target", LocalUserID: userID, RelayUserID: intent.RelayUserID, TargetGroupID: intent.TargetGroupID, ReviewedResourceIDs: []int64{}, ExpectedResult: map[string]any{"target_active": false, "baseline_active": true}, ResumeSupported: true})
			if intent.SourceGroupID > 0 {
				steps = append(steps, durableStepPlan{Key: prefix + ":subscription:source", Action: "add", RelationshipType: "subscription", Direction: "target", LocalUserID: userID, RelayUserID: intent.RelayUserID, TargetGroupID: intent.SourceGroupID, ReviewedResourceIDs: []int64{}, ExpectedResult: map[string]any{"target_active": true, "baseline_active": false}, ResumeSupported: true})
			}
			if len(intent.APIKeyIDs) > 0 {
				steps = append(steps, durableStepPlan{Key: prefix + ":api-keys", Action: "move", RelationshipType: "api_keys", Direction: "target", LocalUserID: userID, RelayUserID: intent.RelayUserID, SourceGroupID: intent.TargetGroupID, TargetGroupID: intent.SourceGroupID, ReviewedResourceIDs: append([]int64(nil), intent.APIKeyIDs...), ExpectedResult: map[string]any{"target_group_id": intent.SourceGroupID, "baseline_group_id": intent.TargetGroupID}, ResumeSupported: true})
			}
			continue
		}
		steps = append(steps, durableStepPlan{Key: prefix + ":subscription:target", Action: "add", RelationshipType: "subscription", Direction: "target", LocalUserID: userID, RelayUserID: intent.RelayUserID, TargetGroupID: intent.TargetGroupID, ReviewedResourceIDs: []int64{}, ExpectedResult: map[string]any{"target_active": true, "baseline_active": false}, ResumeSupported: true})
		if intent.SourceGroupID > 0 {
			steps = append(steps, durableStepPlan{Key: prefix + ":subscription:source", Action: "remove", RelationshipType: "subscription", Direction: "target", LocalUserID: userID, RelayUserID: intent.RelayUserID, TargetGroupID: intent.SourceGroupID, ReviewedResourceIDs: []int64{}, ExpectedResult: map[string]any{"target_active": false, "baseline_active": true}, ResumeSupported: true})
		}
		if len(intent.APIKeyIDs) > 0 {
			steps = append(steps, durableStepPlan{Key: prefix + ":api-keys", Action: "move", RelationshipType: "api_keys", Direction: "target", LocalUserID: userID, RelayUserID: intent.RelayUserID, SourceGroupID: intent.SourceGroupID, TargetGroupID: intent.TargetGroupID, ReviewedResourceIDs: append([]int64(nil), intent.APIKeyIDs...), ExpectedResult: map[string]any{"target_group_id": intent.TargetGroupID, "baseline_group_id": intent.SourceGroupID}, ResumeSupported: true})
		}
	}
	return steps
}

func reconstructLegacyBaseline(mapping Mapping, members map[int]legacyMemberIntent) Mapping {
	baseline := mapping
	baseline.MemberAssignments = cloneInt64Map(mapping.MemberAssignments)
	baseline.MemberSources = cloneInt64Map(mapping.MemberSources)
	for userID, intent := range members {
		key := strconv.Itoa(userID)
		switch intent.Action {
		case "remove", "retain":
			baseline.MemberAssignments[key] = intent.TargetGroupID
			if intent.SourceGroupID > 0 {
				baseline.MemberSources[key] = intent.SourceGroupID
			}
		case "migrate":
			if containsInt64(mapping.GroupIDs, intent.SourceGroupID) {
				baseline.MemberAssignments[key] = intent.SourceGroupID
			} else {
				delete(baseline.MemberAssignments, key)
				delete(baseline.MemberSources, key)
			}
		default:
			delete(baseline.MemberAssignments, key)
			delete(baseline.MemberSources, key)
		}
	}
	return baseline
}

func legacyRemovedUserIDs(state map[string]map[string]string) []int {
	result := []int{}
	for key, entry := range state {
		if strings.HasPrefix(key, "member:") && entry["action"] == "remove" && operationStateNeedsRetry(state, key) {
			if id, err := strconv.Atoi(strings.TrimPrefix(key, "member:")); err == nil && id > 0 {
				result = append(result, id)
			}
		}
	}
	sort.Ints(result)
	return result
}

func (s *Service) persistBlockedLegacyOperation(ctx context.Context, mapping Mapping, actorID int, reason string) error {
	key := fmt.Sprintf("legacy-blocked:%d:%d", mapping.ID, mapping.BaselineRevision)
	exists, err := s.client.RelationshipOperation.Query().Where(relationshipoperation.OperationKeyEQ(key)).Exist(ctx)
	if err != nil || exists {
		return err
	}
	now := time.Now().UTC()
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	fact := map[string]any{"resource_type": "legacy_operation", "resource_id": mapping.ID, "relationship": reason}
	operation, err := tx.RelationshipOperation.Create().SetOperationKey(key).SetProviderID(mapping.ProviderID).SetPlatform(mapping.Platform).SetLifecycle(relationshipoperation.LifecycleBlockedExternal).SetBaselineSnapshot(map[string]any{"mappings": []Mapping{mapping}}).SetTargetSnapshot(map[string]any{}).SetBaselineFingerprint(jsonFingerprint(mapping)).SetTargetFingerprint("legacy:ambiguous").SetSupportedDirections([]string{}).SetInitiatedByUserID(actorID).SetExternalBlocker(fact).Save(ctx)
	if err == nil {
		_, err = tx.RelationshipOperationMapping.Create().SetOperationID(operation.ID).SetMappingID(mapping.ID).SetRole(relationshipoperationmapping.RolePrimary).SetBaselineRevision(mapping.BaselineRevision).SetBaselineSnapshot(jsonObject(mapping)).Save(ctx)
	}
	if err == nil {
		_, err = tx.RelationshipOperationAttempt.Create().SetOperationID(operation.ID).SetAttemptNumber(1).SetDirection(relationshipoperationattempt.DirectionInitial).SetInitiatedByUserID(actorID).SetStatus(relationshipoperationattempt.StatusBlockedExternal).SetResult(fact).SetCompletedAt(now).Save(ctx)
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
