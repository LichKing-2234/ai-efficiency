package relayplanning

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relationshipoperationattempt"
	"github.com/ai-efficiency/backend/ent/relationshipoperationmapping"
	"github.com/ai-efficiency/backend/ent/relationshipoperationstep"
	"github.com/ai-efficiency/backend/internal/relay"
)

type RelationshipOperationStepSummary struct {
	ID                  int     `json:"id"`
	StepKey             string  `json:"step_key"`
	Action              string  `json:"action"`
	RelationshipType    string  `json:"relationship_type"`
	Lifecycle           string  `json:"lifecycle"`
	ReviewedResourceIDs []int64 `json:"reviewed_resource_ids"`
	ResumeSupported     bool    `json:"resume_supported"`
	RestoreSupported    bool    `json:"restore_supported"`
}

type RelationshipOperationSummary struct {
	ID                  int                                `json:"id"`
	Lifecycle           string                             `json:"lifecycle"`
	SupportedDirections []string                           `json:"supported_directions"`
	AffectedMappingIDs  []int                              `json:"affected_mapping_ids"`
	AttemptCount        int                                `json:"attempt_count"`
	Steps               []RelationshipOperationStepSummary `json:"steps"`
	ExternalBlocker     map[string]any                     `json:"external_blocker,omitempty"`
}

type RecoveryPreview struct {
	Operation               RelationshipOperationSummary `json:"operation"`
	Direction               RecoveryDirection            `json:"direction"`
	BaselineRevisions       map[string]int64             `json:"baseline_revisions"`
	RelationshipFingerprint string                       `json:"relationship_fingerprint"`
	ResumeOnly              bool                         `json:"resume_only"`
	ExternalBlocker         map[string]any               `json:"external_blocker,omitempty"`
	ObservedFacts           []map[string]any             `json:"observed_facts"`
}

type RecoveryConfirmRequest struct {
	OperationID                     int
	Direction                       RecoveryDirection
	ExpectedBaselineRevisions       map[string]int64
	ExpectedRelationshipFingerprint string
	InitiatedByUserID               int
}

type StaleRecoveryError struct {
	Current *RecoveryPreview
	Reason  string
}

func (e *StaleRecoveryError) Error() string { return "Relay recovery facts changed after Preview" }

func (s *Service) decorateMappingOperationState(ctx context.Context, mappings []Mapping) error {
	for index := range mappings {
		mappings[index].AlignmentDifferences, mappings[index].Warnings = classifyMappingWarnings(mappings[index].Warnings)
		mappings[index].Alignment = "aligned"
		if len(mappings[index].AlignmentDifferences) > 0 {
			mappings[index].Alignment = "drifted"
		}
	}
	if len(mappings) == 0 {
		return nil
	}
	ids := make([]int, len(mappings))
	indexByID := make(map[int]int, len(mappings))
	for index := range mappings {
		ids[index] = mappings[index].ID
		indexByID[mappings[index].ID] = index
	}
	owners, err := s.client.RelationshipOperationMapping.Query().Where(
		relationshipoperationmapping.MappingIDIn(ids...),
		relationshipoperationmapping.ActiveEQ(true),
	).All(ctx)
	if err != nil {
		return fmt.Errorf("load active Relationship Operation ownership: %w", err)
	}
	operationIDs := []int{}
	for _, owner := range owners {
		operationIDs = append(operationIDs, owner.OperationID)
	}
	operationIDs = uniqueInts(operationIDs)
	for _, operationID := range operationIDs {
		summary, err := s.relationshipOperationSummary(ctx, operationID)
		if err != nil {
			return err
		}
		for _, mappingID := range summary.AffectedMappingIDs {
			if index, exists := indexByID[mappingID]; exists {
				mappings[index].Alignment = "operating"
				mappings[index].ActiveOperation = summary
			}
		}
	}
	return nil
}

func classifyMappingWarnings(warnings []string) (differences, advisories []string) {
	for _, warning := range warnings {
		switch {
		case strings.Contains(warning, " has multiple Accounts"),
			strings.Contains(warning, " is reused across target groups "):
			advisories = append(advisories, warning)
		case strings.Contains(warning, "account relationships are unavailable"):
			differences = append(differences, "Account relationship readback is unavailable")
		case strings.Contains(warning, "relay groups are unavailable"), strings.Contains(warning, "relationship snapshot"):
			differences = append(differences, "Relay relationship readback is unavailable")
		default:
			differences = append(differences, warning)
		}
	}
	return uniqueStrings(differences), uniqueStrings(advisories)
}

func (s *Service) relationshipOperationSummary(ctx context.Context, operationID int) (*RelationshipOperationSummary, error) {
	operation, err := s.client.RelationshipOperation.Get(ctx, operationID)
	if err != nil {
		return nil, fmt.Errorf("load Relationship Operation: %w", err)
	}
	owners, err := s.client.RelationshipOperationMapping.Query().Where(relationshipoperationmapping.OperationIDEQ(operationID)).All(ctx)
	if err != nil {
		return nil, err
	}
	steps, err := s.client.RelationshipOperationStep.Query().Where(relationshipoperationstep.OperationIDEQ(operationID)).Order(ent.Asc(relationshipoperationstep.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	resolveRecoveryStepGroupIDs(steps)
	attemptCount, err := s.client.RelationshipOperationAttempt.Query().Where(relationshipoperationattempt.OperationIDEQ(operationID)).Count(ctx)
	if err != nil {
		return nil, err
	}
	summary := &RelationshipOperationSummary{ID: operation.ID, Lifecycle: string(operation.Lifecycle), SupportedDirections: append([]string(nil), operation.SupportedDirections...), AttemptCount: attemptCount, ExternalBlocker: operation.ExternalBlocker, AffectedMappingIDs: []int{}, Steps: []RelationshipOperationStepSummary{}}
	for _, owner := range owners {
		summary.AffectedMappingIDs = append(summary.AffectedMappingIDs, owner.MappingID)
	}
	sort.Ints(summary.AffectedMappingIDs)
	for _, step := range steps {
		summary.Steps = append(summary.Steps, RelationshipOperationStepSummary{ID: step.ID, StepKey: step.StepKey, Action: step.Action, RelationshipType: step.RelationshipType, Lifecycle: string(step.Lifecycle), ReviewedResourceIDs: append([]int64(nil), step.ReviewedResourceIds...), ResumeSupported: step.ResumeSupported, RestoreSupported: step.RestoreSupported})
	}
	return summary, nil
}

func (s *Service) GetRelationshipOperation(ctx context.Context, operationID int) (*RelationshipOperationSummary, error) {
	if operationID <= 0 {
		return nil, fmt.Errorf("operation ID is required")
	}
	return s.relationshipOperationSummary(ctx, operationID)
}

func (s *Service) PreviewRecovery(ctx context.Context, operationID int, direction RecoveryDirection) (*RecoveryPreview, error) {
	if operationID <= 0 || direction != RecoveryResume && direction != RecoveryRestore {
		return nil, fmt.Errorf("operation ID and recovery direction are required")
	}
	operation, err := s.client.RelationshipOperation.Get(ctx, operationID)
	if err != nil {
		return nil, fmt.Errorf("load Relationship Operation: %w", err)
	}
	if !containsString(operation.SupportedDirections, string(direction)) {
		return nil, fmt.Errorf("Relationship Operation does not support %s", direction)
	}
	summary, err := s.relationshipOperationSummary(ctx, operationID)
	if err != nil {
		return nil, err
	}
	owners, err := s.client.RelationshipOperationMapping.Query().Where(relationshipoperationmapping.OperationIDEQ(operationID)).All(ctx)
	if err != nil {
		return nil, err
	}
	steps, err := s.client.RelationshipOperationStep.Query().Where(relationshipoperationstep.OperationIDEQ(operationID)).Order(ent.Asc(relationshipoperationstep.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	preview := &RecoveryPreview{Operation: *summary, Direction: direction, BaselineRevisions: map[string]int64{}, ResumeOnly: !containsString(operation.SupportedDirections, string(RecoveryRestore)), ObservedFacts: []map[string]any{}}
	for _, owner := range owners {
		preview.BaselineRevisions[strconv.Itoa(owner.MappingID)] = owner.BaselineRevision
	}
	provider, err := s.resolver.Resolve(ctx, operation.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("resolve Relay provider: %w", err)
	}
	if _, blocker, err := preflightRecoveryResources(ctx, provider, operation.Platform, steps); err != nil {
		return nil, err
	} else if blocker != nil {
		preview.ExternalBlocker = map[string]any{"resource_type": blocker.ResourceType, "resource_id": blocker.ResourceID, "relationship": blocker.Relationship}
		preview.RelationshipFingerprint = jsonFingerprint(map[string]any{"operation_id": operationID, "direction": direction, "baseline_revisions": preview.BaselineRevisions, "facts": preview.ObservedFacts, "external_blocker": preview.ExternalBlocker})
		return preview, nil
	}
	facts, err := observeRecoveryFacts(ctx, provider, operation.Platform, steps)
	if err != nil {
		return nil, err
	}
	preview.ObservedFacts = facts
	preview.RelationshipFingerprint = jsonFingerprint(map[string]any{"operation_id": operationID, "direction": direction, "baseline_revisions": preview.BaselineRevisions, "facts": facts, "external_blocker": preview.ExternalBlocker})
	return preview, nil
}

func (s *Service) ConfirmRecovery(ctx context.Context, req RecoveryConfirmRequest) (*RecoveryResult, error) {
	current, err := s.PreviewRecovery(ctx, req.OperationID, req.Direction)
	if err != nil {
		return nil, err
	}
	if current.ExternalBlocker != nil {
		return nil, &ExternalRecoveryBlockerError{ResourceType: stringValue(current.ExternalBlocker["resource_type"]), ResourceID: int64Value(current.ExternalBlocker["resource_id"]), Relationship: stringValue(current.ExternalBlocker["relationship"])}
	}
	if req.ExpectedRelationshipFingerprint != current.RelationshipFingerprint {
		return nil, &StaleRecoveryError{Current: current, Reason: "relationship_fingerprint"}
	}
	if !equalRevisions(req.ExpectedBaselineRevisions, current.BaselineRevisions) {
		return nil, &StaleRecoveryError{Current: current, Reason: "baseline_revision"}
	}
	actual, err := s.currentRecoveryBaselineRevisions(ctx, req.OperationID)
	if err != nil {
		return nil, err
	}
	if !equalRevisions(req.ExpectedBaselineRevisions, actual) {
		return nil, &StaleRecoveryError{Current: current, Reason: "baseline_revision"}
	}
	return s.Recover(ctx, RecoveryRequest{OperationID: req.OperationID, Direction: req.Direction, InitiatedByUserID: req.InitiatedByUserID})
}

func (s *Service) currentRecoveryBaselineRevisions(ctx context.Context, operationID int) (map[string]int64, error) {
	owners, err := s.client.RelationshipOperationMapping.Query().Where(relationshipoperationmapping.OperationIDEQ(operationID)).All(ctx)
	if err != nil {
		return nil, err
	}
	result := make(map[string]int64, len(owners))
	for _, owner := range owners {
		mapping, err := s.client.RelayGroupMapping.Get(ctx, owner.MappingID)
		if err != nil {
			return nil, err
		}
		result[strconv.Itoa(owner.MappingID)] = mapping.BaselineRevision
	}
	return result, nil
}

func observeRecoveryFacts(ctx context.Context, provider relay.Provider, platform string, steps []*ent.RelationshipOperationStep) ([]map[string]any, error) {
	facts := make([]map[string]any, 0, len(steps))
	accounts, accountsLoaded := []relay.Account{}, false
	subscriptions := map[int64][]relay.UserSubscription{}
	keys := map[int64][]relay.APIKey{}
	for _, step := range steps {
		fact := map[string]any{"step_key": step.StepKey, "relationship_type": step.RelationshipType}
		switch step.RelationshipType {
		case "group":
			reader, ok := provider.(relay.GroupReader)
			if !ok {
				return nil, fmt.Errorf("Relay provider does not support Group readback")
			}
			id := int64PointerValue(step.TargetGroupID)
			if id == 0 {
				id = int64Value(step.LatestVerifiedEffect["group_id"])
			}
			group, err := reader.GetGroup(ctx, id)
			if err != nil {
				return nil, err
			}
			fact["group_id"], fact["name"] = id, group.Name
		case "account_group":
			if !accountsLoaded {
				reader, ok := provider.(relay.AccountRelationshipReader)
				if !ok {
					return nil, fmt.Errorf("Relay provider does not support Account readback")
				}
				var err error
				accounts, err = reader.ListAccountsForPlatform(ctx, platform)
				if err != nil {
					return nil, err
				}
				accountsLoaded = true
			}
			id := step.ReviewedResourceIds[0]
			account, _ := accountByID(accounts, id)
			fact["account_id"], fact["group_id"], fact["priority"] = id, int64PointerValue(step.TargetGroupID), accountRelationshipPriority(account.GroupRelationships, int64PointerValue(step.TargetGroupID))
		case "subscription":
			userID := int64PointerValue(step.RelayUserID)
			if _, loaded := subscriptions[userID]; !loaded {
				reader, ok := provider.(relay.UserSubscriptionLister)
				if !ok {
					return nil, fmt.Errorf("Relay provider does not support subscription readback")
				}
				var err error
				subscriptions[userID], err = reader.ListUserSubscriptions(ctx, userID)
				if err != nil {
					return nil, err
				}
			}
			groupID := int64PointerValue(step.TargetGroupID)
			fact["relay_user_id"], fact["group_id"], fact["active"] = userID, groupID, subscriptionActive(subscriptions[userID], groupID)
		case "api_keys":
			userID := int64PointerValue(step.RelayUserID)
			if _, loaded := keys[userID]; !loaded {
				var err error
				keys[userID], err = provider.ListUserAPIKeys(ctx, userID)
				if err != nil {
					return nil, err
				}
			}
			groups := map[string]int64{}
			for _, key := range keys[userID] {
				if containsInt64(step.ReviewedResourceIds, key.ID) {
					groups[strconv.FormatInt(key.ID, 10)] = apiKeyGroupID(key)
				}
			}
			fact["relay_user_id"], fact["reviewed_key_groups"] = userID, groups
		}
		facts = append(facts, fact)
	}
	return facts, nil
}

func subscriptionActive(items []relay.UserSubscription, groupID int64) bool {
	for _, item := range items {
		id := item.GroupID
		if id == 0 && item.Group != nil {
			id = item.Group.ID
		}
		if id == groupID && item.Status == "active" {
			return true
		}
	}
	return false
}

func equalRevisions(left, right map[string]int64) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func uniqueInts(items []int) []int {
	sort.Ints(items)
	out := items[:0]
	for _, item := range items {
		if len(out) == 0 || out[len(out)-1] != item {
			out = append(out, item)
		}
	}
	return out
}
