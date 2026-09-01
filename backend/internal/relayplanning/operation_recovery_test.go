package relayplanning

import (
	"context"
	"fmt"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relationshipoperation"
	"github.com/ai-efficiency/backend/ent/relationshipoperationattempt"
	"github.com/ai-efficiency/backend/ent/relationshipoperationmapping"
	"github.com/ai-efficiency/backend/ent/relationshipoperationstep"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
)

type recoveryProvider struct {
	*replanRetryProvider
	groups   []relay.Group
	accounts []relay.Account
}

func TestBuildDurableStepPlansFreezesReviewedAPIKeysAndResumeOnlyCreation(t *testing.T) {
	plan := &Plan{
		Assignments: []Assignment{{Index: 0, TargetGroupName: "Reviewed Target"}},
		TargetSummaries: []TargetChangeSummary{{
			Index:   0,
			APIKeys: []APIKeyChange{{RelayUserID: 42, FromGroupID: 101, ToGroupID: 102, Count: 2}},
		}},
		relationshipSnapshot: relationshipSnapshot{Users: []relationshipUserFact{{RelayUserID: 42, APIKeys: []relationshipAPIKeyFact{{ID: 502, GroupID: 102}, {ID: 999, GroupID: 777}, {ID: 501, GroupID: 101}}}}},
	}
	steps := buildDurableStepPlans(plan)
	if len(steps) != 2 {
		t.Fatalf("steps = %+v, want create and API Key move", steps)
	}
	var createStep, keyStep *durableStepPlan
	for index := range steps {
		if steps[index].Action == "create" {
			createStep = &steps[index]
		}
		if steps[index].RelationshipType == "api_keys" {
			keyStep = &steps[index]
		}
	}
	if createStep == nil || !createStep.ResumeSupported || createStep.RestoreSupported {
		t.Fatalf("create step = %+v, want Resume only", createStep)
	}
	if keyStep == nil || fmt.Sprint(keyStep.ReviewedResourceIDs) != "[501 502]" {
		got := "<missing>"
		if keyStep != nil {
			got = fmt.Sprint(keyStep.ReviewedResourceIDs)
		}
		t.Fatalf("reviewed API Key IDs = %s, want frozen sorted [501 502]", got)
	}
}

func (p *recoveryProvider) AssignSubscriptionForUser(_ context.Context, userID, groupID int64, validityDays int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.assignmentCalls++
	p.assignmentValidityDays = append(p.assignmentValidityDays, validityDays)
	for _, item := range p.subscriptions {
		if item.UserID == userID && item.GroupID == groupID && item.Status == "active" {
			return nil
		}
	}
	p.subscriptions = append(p.subscriptions, relay.UserSubscription{UserID: userID, GroupID: groupID, Status: "active"})
	return nil
}

func (p *recoveryProvider) GetGroup(_ context.Context, groupID int64) (*relay.Group, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, group := range p.groups {
		if group.ID == groupID {
			copy := group
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("group not found")
}

func (p *recoveryProvider) RenameGroup(_ context.Context, groupID int64, name string) (*relay.Group, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for index := range p.groups {
		if p.groups[index].ID == groupID {
			p.groups[index].Name = name
			copy := p.groups[index]
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("group not found")
}

func (p *recoveryProvider) ListAccountsForPlatform(_ context.Context, platform string) ([]relay.Account, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	items := []relay.Account{}
	for _, account := range p.accounts {
		if account.Platform == platform {
			items = append(items, account)
		}
	}
	return items, nil
}

func (p *recoveryProvider) SetAccountGroupRelationship(_ context.Context, accountID, groupID int64, _ []relay.AccountGroupRelationship, desiredPriority *int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for index := range p.accounts {
		if p.accounts[index].ID != accountID {
			continue
		}
		relationships := p.accounts[index].GroupRelationships[:0]
		for _, relationship := range p.accounts[index].GroupRelationships {
			if relationship.GroupID != groupID {
				relationships = append(relationships, relationship)
			}
		}
		if desiredPriority != nil {
			relationships = append(relationships, relay.AccountGroupRelationship{GroupID: groupID, Priority: *desiredPriority})
		}
		p.accounts[index].GroupRelationships = relationships
		return nil
	}
	return fmt.Errorf("account not found")
}

func TestRecoverRestoreConvergesReviewedRelationshipsWithoutChangingBaseline(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	service, provider, mapping, operation := createRecoveryFixture(t, ctx, client, true)

	result, err := service.Recover(ctx, RecoveryRequest{OperationID: operation.ID, Direction: RecoveryRestore, InitiatedByUserID: 1})
	if err != nil {
		t.Fatalf("Recover(restore) error = %v", err)
	}
	if result.Lifecycle != string(relationshipoperation.LifecycleRestored) {
		t.Fatalf("restore lifecycle = %q, want restored", result.Lifecycle)
	}
	assertRecoveryRelationships(t, provider, 101, true)
	assertRecoveryGroupAndAccount(t, provider, "Baseline Target", 1)
	unchanged := client.RelayGroupMapping.GetX(ctx, mapping.ID)
	if unchanged.BaselineRevision != 1 || unchanged.MemberAssignments["1"] != 101 {
		t.Fatalf("Restore changed baseline: revision=%d assignments=%v", unchanged.BaselineRevision, unchanged.MemberAssignments)
	}
	assertRecoveryTerminalState(t, ctx, client, operation.ID, relationshipoperation.LifecycleRestored, relationshipoperationattempt.DirectionRestore)
}

func TestRecoverResumePromotesTargetOnlyAfterVerifiedReadback(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	service, provider, mapping, operation := createRecoveryFixture(t, ctx, client, false)

	result, err := service.Recover(ctx, RecoveryRequest{OperationID: operation.ID, Direction: RecoveryResume, InitiatedByUserID: 1})
	if err != nil {
		t.Fatalf("Recover(resume) error = %v", err)
	}
	if result.Lifecycle != string(relationshipoperation.LifecycleApplied) {
		t.Fatalf("resume lifecycle = %q, want applied", result.Lifecycle)
	}
	assertRecoveryRelationships(t, provider, 102, false)
	assertRecoveryGroupAndAccount(t, provider, "Reviewed Target", 2)
	promoted := client.RelayGroupMapping.GetX(ctx, mapping.ID)
	if promoted.BaselineRevision != 2 || promoted.MemberAssignments["1"] != 102 || promoted.MemberSources["1"] != 101 {
		t.Fatalf("Resume baseline = revision:%d assignments:%v sources:%v", promoted.BaselineRevision, promoted.MemberAssignments, promoted.MemberSources)
	}
	assertRecoveryTerminalState(t, ctx, client, operation.ID, relationshipoperation.LifecycleApplied, relationshipoperationattempt.DirectionResume)
}

func TestRecoverBlocksOnMissingReviewedAPIKeyWithoutAnotherWrite(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	service, provider, mapping, operation := createRecoveryFixture(t, ctx, client, false)
	provider.keys = provider.keys[1:]

	_, err := service.Recover(ctx, RecoveryRequest{OperationID: operation.ID, Direction: RecoveryResume, InitiatedByUserID: 1})
	blocker, ok := err.(*ExternalRecoveryBlockerError)
	if !ok || blocker.ResourceType != "api_key" || blocker.ResourceID != 501 {
		t.Fatalf("Recover missing Key error = %#v, want exact API Key blocker", err)
	}
	blocked := client.RelationshipOperation.GetX(ctx, operation.ID)
	if blocked.Lifecycle != relationshipoperation.LifecycleBlockedExternal || blocked.ExternalBlocker["resource_type"] != "api_key" {
		t.Fatalf("blocked Operation = lifecycle:%q blocker:%v", blocked.Lifecycle, blocked.ExternalBlocker)
	}
	unchanged := client.RelayGroupMapping.GetX(ctx, mapping.ID)
	if unchanged.BaselineRevision != 1 || unchanged.MemberAssignments["1"] != 101 {
		t.Fatalf("external blocker changed baseline: revision=%d assignments=%v", unchanged.BaselineRevision, unchanged.MemberAssignments)
	}
	if provider.assignmentCalls != 0 || len(provider.bound) != 0 {
		t.Fatalf("external blocker allowed writes: assignments=%d bindings=%v", provider.assignmentCalls, provider.bound)
	}
}

func createRecoveryFixture(t *testing.T, ctx context.Context, client *ent.Client, observedTarget bool) (*Service, *recoveryProvider, *ent.RelayGroupMapping, *ent.RelationshipOperation) {
	t.Helper()
	user := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	providerRow := client.RelayProvider.Create().SetName("recovery-test").SetDisplayName("Recovery Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SaveX(ctx)
	mapping := client.RelayGroupMapping.Create().SetProviderID(providerRow.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").SetTemplateGroupID(10).SetSourceGroupID(101).SetGroupIds([]int64{101}).SetMemberAssignments(map[string]int64{fmt.Sprint(user.ID): 101}).SetMemberSources(map[string]int64{fmt.Sprint(user.ID): 101}).SetWeeklyCostTarget(2500).SaveX(ctx)
	target := Plan{ProviderID: providerRow.ID, DepartmentID: "dept-alpha", DepartmentName: "Department Alpha", Platform: "openai", TemplateGroupID: 10, SourceGroupID: 101, WeeklyCostTarget: 2500, MappingID: mapping.ID, Assignments: []Assignment{{Index: 0, TargetGroupID: 102, TargetGroupName: "Target", UserIDs: []int{user.ID}}}, Candidates: []Candidate{{UserID: user.ID, RelayUserID: 42, SourceGroupID: 101}}}
	baseline := Mapping{ID: mapping.ID, ProviderID: mapping.ProviderID, DepartmentID: mapping.DepartmentExternalID, DepartmentName: mapping.DepartmentName, Platform: mapping.Platform, TemplateGroupID: mapping.TemplateGroupID, SourceGroupID: mapping.SourceGroupID, GroupIDs: append([]int64(nil), mapping.GroupIds...), MemberAssignments: map[string]int64{fmt.Sprint(user.ID): 101}, MemberSources: map[string]int64{fmt.Sprint(user.ID): 101}, BaselineRevision: 1}
	operation := client.RelationshipOperation.Create().SetOperationKey("recovery-op").SetProviderID(providerRow.ID).SetPlatform("openai").SetLifecycle(relationshipoperation.LifecycleInterrupted).SetBaselineSnapshot(map[string]any{"mappings": []Mapping{baseline}}).SetTargetSnapshot(jsonObject(target)).SetBaselineFingerprint("baseline").SetTargetFingerprint("target").SetSupportedDirections([]string{"resume", "restore"}).SetInitiatedByUserID(user.ID).SaveX(ctx)
	client.RelationshipOperationMapping.Create().SetOperationID(operation.ID).SetMappingID(mapping.ID).SetRole(relationshipoperationmapping.RolePrimary).SetBaselineRevision(1).SetBaselineSnapshot(jsonObject(baseline)).SaveX(ctx)
	client.RelationshipOperationStep.Create().SetOperationID(operation.ID).SetStepKey("subscription-add-target").SetAction("add").SetRelationshipType("subscription").SetDirection(relationshipoperationstep.DirectionTarget).SetLocalUserID(user.ID).SetRelayUserID(42).SetTargetGroupID(102).SetReviewedResourceIds([]int64{}).SetExpectedResult(map[string]any{"target_active": true, "baseline_active": false}).SetResumeSupported(true).SetRestoreSupported(true).SetLifecycle(relationshipoperationstep.LifecycleDispatched).SaveX(ctx)
	client.RelationshipOperationStep.Create().SetOperationID(operation.ID).SetStepKey("subscription-remove-source").SetAction("remove").SetRelationshipType("subscription").SetDirection(relationshipoperationstep.DirectionTarget).SetLocalUserID(user.ID).SetRelayUserID(42).SetTargetGroupID(101).SetReviewedResourceIds([]int64{}).SetExpectedResult(map[string]any{"target_active": false, "baseline_active": true}).SetResumeSupported(true).SetRestoreSupported(true).SetLifecycle(relationshipoperationstep.LifecycleDispatched).SaveX(ctx)
	client.RelationshipOperationStep.Create().SetOperationID(operation.ID).SetStepKey("api-key-move").SetAction("move").SetRelationshipType("api_keys").SetDirection(relationshipoperationstep.DirectionTarget).SetLocalUserID(user.ID).SetRelayUserID(42).SetSourceGroupID(101).SetTargetGroupID(102).SetReviewedResourceIds([]int64{501}).SetExpectedResult(map[string]any{"target_group_id": 102, "baseline_group_id": 101}).SetResumeSupported(true).SetRestoreSupported(true).SetLifecycle(relationshipoperationstep.LifecycleDispatched).SaveX(ctx)
	client.RelationshipOperationStep.Create().SetOperationID(operation.ID).SetStepKey("target-rename").SetAction("rename").SetRelationshipType("group").SetDirection(relationshipoperationstep.DirectionTarget).SetTargetGroupID(102).SetReviewedResourceIds([]int64{}).SetExpectedResult(map[string]any{"target_name": "Reviewed Target", "baseline_name": "Baseline Target"}).SetResumeSupported(true).SetRestoreSupported(true).SetLifecycle(relationshipoperationstep.LifecycleDispatched).SaveX(ctx)
	client.RelationshipOperationStep.Create().SetOperationID(operation.ID).SetStepKey("account-priority").SetAction("update").SetRelationshipType("account_group").SetDirection(relationshipoperationstep.DirectionTarget).SetTargetGroupID(102).SetReviewedResourceIds([]int64{11}).SetExpectedResult(map[string]any{"target_priority": 2, "baseline_priority": 1}).SetResumeSupported(true).SetRestoreSupported(true).SetLifecycle(relationshipoperationstep.LifecycleDispatched).SaveX(ctx)
	client.RelationshipOperationAttempt.Create().SetOperationID(operation.ID).SetAttemptNumber(1).SetDirection(relationshipoperationattempt.DirectionInitial).SetInitiatedByUserID(user.ID).SetStatus(relationshipoperationattempt.StatusFailed).SaveX(ctx)

	observedGroup := int64(101)
	subscriptions := []relay.UserSubscription{{UserID: 42, GroupID: 101, Status: "active"}, {UserID: 42, GroupID: 777, Status: "active"}}
	if observedTarget {
		observedGroup = 102
		subscriptions = []relay.UserSubscription{{UserID: 42, GroupID: 102, Status: "active"}, {UserID: 42, GroupID: 777, Status: "active"}}
	}
	groupName, priority := "Baseline Target", 1
	if observedTarget {
		groupName, priority = "Reviewed Target", 2
	}
	provider := &recoveryProvider{
		replanRetryProvider: &replanRetryProvider{subscriptions: subscriptions, keys: []relay.APIKey{{ID: 501, UserID: 42, GroupID: observedGroup, Status: "active"}, {ID: 999, UserID: 42, GroupID: 777, Status: "active"}}},
		groups:              []relay.Group{{ID: 101, Name: "Source", Platform: "openai"}, {ID: 102, Name: groupName, Platform: "openai"}},
		accounts:            []relay.Account{{ID: 11, Platform: "openai", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 102, Priority: priority}, {GroupID: 777, Priority: 9}}}},
	}
	service := NewService(client, relayPlanningProviderResolver(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	return service, provider, mapping, operation
}

func assertRecoveryGroupAndAccount(t *testing.T, provider *recoveryProvider, name string, priority int) {
	t.Helper()
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.groups) != 2 || provider.groups[1].Name != name {
		t.Fatalf("Groups after recovery = %+v, want name %q", provider.groups, name)
	}
	if len(provider.accounts) != 1 || accountRelationshipPriority(provider.accounts[0].GroupRelationships, 102) != priority || accountRelationshipPriority(provider.accounts[0].GroupRelationships, 777) != 9 {
		t.Fatalf("Accounts after recovery = %+v, want priorities 102:%d and unrelated 777:9", provider.accounts, priority)
	}
}

func assertRecoveryRelationships(t *testing.T, provider *recoveryProvider, expectedGroup int64, sourceActive bool) {
	t.Helper()
	provider.mu.Lock()
	defer provider.mu.Unlock()
	active := map[int64]bool{}
	for _, item := range provider.subscriptions {
		if item.Status == "active" {
			active[item.GroupID] = true
		}
	}
	if active[101] != sourceActive || active[102] == sourceActive || !active[777] {
		t.Fatalf("subscriptions after recovery = %v", provider.subscriptions)
	}
	if provider.keys[0].GroupID != expectedGroup || provider.keys[1].GroupID != 777 {
		t.Fatalf("API Keys after recovery = %+v", provider.keys)
	}
}

func assertRecoveryTerminalState(t *testing.T, ctx context.Context, client *ent.Client, operationID int, lifecycle relationshipoperation.Lifecycle, direction relationshipoperationattempt.Direction) {
	t.Helper()
	operation := client.RelationshipOperation.GetX(ctx, operationID)
	if operation.Lifecycle != lifecycle {
		t.Fatalf("Operation lifecycle = %q, want %q", operation.Lifecycle, lifecycle)
	}
	owner := client.RelationshipOperationMapping.Query().Where(relationshipoperationmapping.OperationIDEQ(operationID)).OnlyX(ctx)
	if owner.Active {
		t.Fatal("terminal recovery retained active Mapping ownership")
	}
	attempts := client.RelationshipOperationAttempt.Query().Where(relationshipoperationattempt.OperationIDEQ(operationID)).AllX(ctx)
	if len(attempts) != 2 || attempts[1].Direction != direction || attempts[1].Status != relationshipoperationattempt.StatusSucceeded {
		t.Fatalf("attempts = %+v, want independent successful %s attempt", attempts, direction)
	}
}
