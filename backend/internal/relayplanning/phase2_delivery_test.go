package relayplanning

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relationshipoperation"
	"github.com/ai-efficiency/backend/ent/relationshipoperationattempt"
	"github.com/ai-efficiency/backend/ent/relationshipoperationmapping"
	"github.com/ai-efficiency/backend/ent/relationshipoperationstep"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestPhase2CrossMappingOwnershipUsesStableOrderAndLeavesUnrelatedWritable(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	actor := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SaveX(ctx)
	provider := client.RelayProvider.Create().SetName("ownership-order-test").SetDisplayName("Ownership Order Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SaveX(ctx)
	createMapping := func(department string, groupID int64) Mapping {
		row := client.RelayGroupMapping.Create().SetProviderID(provider.ID).SetDepartmentExternalID(department).SetDepartmentName(department).SetPlatform("openai").SetGroupIds([]int64{groupID}).SetMemberAssignments(map[string]int64{"1": groupID}).SaveX(ctx)
		return mappingFromEnt(row)
	}
	mappingA, mappingB, unrelated := createMapping("dept-alpha", 101), createMapping("dept-beta", 102), createMapping("dept-gamma", 103)
	service := NewService(client, nil, nil)
	type result struct {
		execution *durableExecution
		err       error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	run := func(destination, source Mapping, key string, userID int) {
		<-start
		plan := &Plan{ProviderID: provider.ID, Platform: "openai", MappingID: destination.ID, RelationshipFingerprint: key + "-target"}
		req := ExecuteRequest{OperationKey: key, InitiatedByUserID: actor.ID, PreviewRequest: PreviewRequest{MemberActions: map[string]MemberAction{fmt.Sprint(userID): {Mode: "move_here", FromMappingID: source.ID}}}}
		execution, err := service.beginReplanDurableExecution(ctx, destination, plan, req)
		results <- result{execution: execution, err: err}
	}
	go run(mappingA, mappingB, "ownership-a", 1)
	go run(mappingB, mappingA, "ownership-b", 2)
	close(start)
	var succeeded, conflicted int
	for index := 0; index < 2; index++ {
		select {
		case item := <-results:
			if item.err == nil {
				succeeded++
			} else {
				var active *ActiveRelationshipOperationError
				if !errors.As(item.err, &active) {
					t.Fatalf("concurrent ownership error = %v", item.err)
				}
				conflicted++
			}
		case <-time.After(5 * time.Second):
			t.Fatal("cross-Mapping ownership acquisition deadlocked")
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("ownership results = succeeded:%d conflicted:%d", succeeded, conflicted)
	}
	owners := client.RelationshipOperationMapping.Query().Where(relationshipoperationmapping.ActiveEQ(true)).AllX(ctx)
	if len(owners) != 2 || owners[0].OperationID != owners[1].OperationID {
		t.Fatalf("active ownership rows = %+v, want one Operation owning both Mappings", owners)
	}
	if _, err := client.RelayGroupMapping.UpdateOneID(unrelated.ID).SetWeeklyCostTarget(99).Save(ctx); err != nil {
		t.Fatalf("unrelated Mapping write blocked: %v", err)
	}
}

func TestPhase2RestartRecoveryAtEveryManagedWriteBoundary(t *testing.T) {
	tests := []struct {
		name   string
		adjust func(context.Context, *ent.Client, *recoveryProvider, *ent.RelationshipOperation)
	}{
		{name: "operation persistence"},
		{name: "subscription assignment", adjust: func(_ context.Context, _ *ent.Client, provider *recoveryProvider, _ *ent.RelationshipOperation) {
			provider.subscriptions = append(provider.subscriptions, relay.UserSubscription{UserID: 42, GroupID: 102, Status: "active"})
		}},
		{name: "API Key binding", adjust: func(_ context.Context, _ *ent.Client, provider *recoveryProvider, _ *ent.RelationshipOperation) {
			provider.subscriptions = append(provider.subscriptions, relay.UserSubscription{UserID: 42, GroupID: 102, Status: "active"})
			provider.keys[0].GroupID = 102
		}},
		{name: "Source removal", adjust: func(_ context.Context, _ *ent.Client, provider *recoveryProvider, _ *ent.RelationshipOperation) {
			setRecoveryProviderTarget(provider)
			provider.subscriptions = []relay.UserSubscription{{UserID: 42, GroupID: 102, Status: "active"}, {UserID: 42, GroupID: 777, Status: "active"}}
		}},
		{name: "Account update", adjust: func(_ context.Context, _ *ent.Client, provider *recoveryProvider, _ *ent.RelationshipOperation) {
			setRecoveryProviderTarget(provider)
			provider.groups[1].Name = "Baseline Target"
		}},
		{name: "rename", adjust: func(_ context.Context, _ *ent.Client, provider *recoveryProvider, _ *ent.RelationshipOperation) {
			setRecoveryProviderTarget(provider)
		}},
		{name: "Group creation", adjust: func(ctx context.Context, client *ent.Client, provider *recoveryProvider, operation *ent.RelationshipOperation) {
			setRecoveryProviderTarget(provider)
			client.RelationshipOperationStep.Create().SetOperationID(operation.ID).SetStepKey("target:0:create").SetAction("create").SetRelationshipType("group").SetDirection(relationshipoperationstep.DirectionTarget).SetReviewedResourceIds([]int64{}).SetExpectedResult(map[string]any{"name": "Reviewed Target", "status": "active"}).SetResumeSupported(true).SetRestoreSupported(false).SetLifecycle(relationshipoperationstep.LifecycleReadbackVerified).SetLatestVerifiedEffect(map[string]any{"group_id": 102, "name": "Reviewed Target"}).SaveX(ctx)
		}},
		{name: "Relay readback", adjust: func(_ context.Context, _ *ent.Client, provider *recoveryProvider, _ *ent.RelationshipOperation) {
			setRecoveryProviderTarget(provider)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			client := testdb.Open(t)
			_, provider, mapping, operation := createRecoveryFixture(t, ctx, client, false)
			if tt.adjust != nil {
				tt.adjust(ctx, client, provider, operation)
			}
			restarted := NewService(client, relayPlanningProviderResolver(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
			result, err := restarted.Recover(ctx, RecoveryRequest{OperationID: operation.ID, Direction: RecoveryResume, InitiatedByUserID: 1})
			if err != nil {
				t.Fatalf("Recover() after %s: %v", tt.name, err)
			}
			if result.Lifecycle != string(relationshipoperation.LifecycleApplied) {
				t.Fatalf("lifecycle after %s = %q", tt.name, result.Lifecycle)
			}
			if got := client.RelayGroupMapping.GetX(ctx, mapping.ID); got.BaselineRevision != 2 || got.MemberAssignments["1"] != 102 {
				t.Fatalf("baseline after %s = revision:%d assignments:%v", tt.name, got.BaselineRevision, got.MemberAssignments)
			}
			assertRecoveryRelationships(t, provider, 102, false)
			assertRecoveryGroupAndAccount(t, provider, "Reviewed Target", 2)
		})
	}
}

type droppedBindResponseProvider struct {
	*recoveryProvider
	mu       sync.Mutex
	dropOnce bool
}

func (p *droppedBindResponseProvider) BindAPIKeyToGroup(ctx context.Context, keyID, groupID int64) error {
	if err := p.recoveryProvider.BindAPIKeyToGroup(ctx, keyID, groupID); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dropOnce {
		p.dropOnce = false
		return errors.New("synthetic dropped write response")
	}
	return nil
}

func TestPhase2DroppedWriteResponseUsesReadbackWithoutBlindReplay(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	_, base, _, operation := createRecoveryFixture(t, ctx, client, false)
	provider := &droppedBindResponseProvider{recoveryProvider: base, dropOnce: true}
	resolver := relayPlanningProviderResolver(func(context.Context, int) (relay.Provider, error) { return provider, nil })
	first := NewService(client, resolver, nil)
	if _, err := first.Recover(ctx, RecoveryRequest{OperationID: operation.ID, Direction: RecoveryResume, InitiatedByUserID: 1}); err == nil {
		t.Fatal("dropped write response unexpectedly completed the first attempt")
	}
	if len(base.bound) != 1 || base.keys[0].GroupID != 102 {
		t.Fatalf("first attempt bindings=%v Keys=%v, want one applied unknown outcome", base.bound, base.keys)
	}
	restarted := NewService(client, resolver, nil)
	if _, err := restarted.Recover(ctx, RecoveryRequest{OperationID: operation.ID, Direction: RecoveryResume, InitiatedByUserID: 1}); err != nil {
		t.Fatalf("restart Recover() error = %v", err)
	}
	if len(base.bound) != 1 {
		t.Fatalf("unknown API Key effect replayed blindly: bindings=%v", base.bound)
	}
}

func TestPhase2RestoreDoesNotReuseResumeEvidence(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	_, provider, _, operation := createRecoveryFixture(t, ctx, client, false)
	steps := client.RelationshipOperationStep.Query().Where(relationshipoperationstep.OperationIDEQ(operation.ID)).AllX(ctx)
	for _, step := range steps {
		client.RelationshipOperationStep.UpdateOneID(step.ID).
			SetLifecycle(relationshipoperationstep.LifecycleReadbackVerified).
			SetLatestVerifiedEffect(map[string]any{"direction": "resume", "synthetic": "forward-evidence"}).
			ExecX(ctx)
	}

	restarted := NewService(client, relayPlanningProviderResolver(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	result, err := restarted.Recover(ctx, RecoveryRequest{OperationID: operation.ID, Direction: RecoveryRestore, InitiatedByUserID: 1})
	if err != nil {
		t.Fatalf("Recover(restore) with forward evidence: %v", err)
	}
	if result.Lifecycle != string(relationshipoperation.LifecycleRestored) {
		t.Fatalf("restore lifecycle = %q", result.Lifecycle)
	}
	attempts := client.RelationshipOperationAttempt.Query().Where(relationshipoperationattempt.OperationIDEQ(operation.ID)).Order(ent.Asc(relationshipoperationattempt.FieldAttemptNumber)).AllX(ctx)
	if len(attempts) != 2 || attempts[0].Direction != relationshipoperationattempt.DirectionInitial || attempts[0].Status != relationshipoperationattempt.StatusFailed || attempts[1].Direction != relationshipoperationattempt.DirectionRestore || attempts[1].Status != relationshipoperationattempt.StatusSucceeded {
		t.Fatalf("attempt evidence = %+v, want independent failed initial and successful Restore attempts", attempts)
	}
	for _, step := range client.RelationshipOperationStep.Query().Where(relationshipoperationstep.OperationIDEQ(operation.ID)).AllX(ctx) {
		if step.LatestVerifiedEffect["synthetic"] == "forward-evidence" {
			t.Fatalf("Restore reused forward evidence for step %q", step.StepKey)
		}
	}
	assertRecoveryRelationships(t, provider, 101, true)
	assertRecoveryGroupAndAccount(t, provider, "Baseline Target", 1)
}

func TestPhase2RestartAfterBaselinePromotionOnlyFinishesDurableEvidence(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	_, provider, mapping, operation := createRecoveryFixture(t, ctx, client, true)
	client.RelayGroupMapping.UpdateOneID(mapping.ID).SetGroupIds([]int64{102}).SetMemberAssignments(map[string]int64{"1": 102}).SetMemberSources(map[string]int64{"1": 101}).AddBaselineRevision(1).SaveX(ctx)
	restarted := NewService(client, relayPlanningProviderResolver(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	result, err := restarted.Recover(ctx, RecoveryRequest{OperationID: operation.ID, Direction: RecoveryResume, InitiatedByUserID: 1})
	if err != nil {
		t.Fatalf("Recover() after baseline promotion: %v", err)
	}
	if result.Lifecycle != string(relationshipoperation.LifecycleApplied) {
		t.Fatalf("lifecycle = %q, want applied", result.Lifecycle)
	}
	if got := client.RelayGroupMapping.GetX(ctx, mapping.ID); got.BaselineRevision != 2 {
		t.Fatalf("baseline revision = %d, want no duplicate promotion", got.BaselineRevision)
	}
	owner := client.RelationshipOperationMapping.Query().Where(relationshipoperationmapping.OperationIDEQ(operation.ID)).OnlyX(ctx)
	if owner.Active {
		t.Fatal("terminal evidence restart retained ownership")
	}
}

func TestPhase2MultiMappingPromotionRollsBackTogetherAndSucceedsAfterRestart(t *testing.T) {
	ctx := context.Background()
	client, dsn := testdb.OpenWithDSN(t)
	_, provider, destination, operation := createRecoveryFixture(t, ctx, client, true)
	source := client.RelayGroupMapping.Create().SetProviderID(destination.ProviderID).SetDepartmentExternalID("dept-source").SetDepartmentName("Source").SetPlatform("openai").SetGroupIds([]int64{101}).SetMemberAssignments(map[string]int64{"1": 101}).SetMemberSources(map[string]int64{"1": 101}).SaveX(ctx)
	sourceBaseline := mappingFromEnt(source)
	client.RelationshipOperationMapping.Create().SetOperationID(operation.ID).SetMappingID(source.ID).SetRole(relationshipoperationmapping.RoleAffected).SetBaselineRevision(1).SetBaselineSnapshot(jsonObject(sourceBaseline)).SaveX(ctx)
	raw, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open raw DB: %v", err)
	}
	defer raw.Close()
	trigger := fmt.Sprintf(`
CREATE FUNCTION reject_phase2_source_promotion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.id = %d AND NEW.baseline_revision > OLD.baseline_revision THEN
    RAISE EXCEPTION 'synthetic source promotion failure';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER reject_phase2_source_promotion BEFORE UPDATE ON relay_group_mappings
FOR EACH ROW EXECUTE FUNCTION reject_phase2_source_promotion();`, source.ID)
	if _, err := raw.ExecContext(ctx, trigger); err != nil {
		t.Fatalf("install source promotion failure: %v", err)
	}
	resolver := relayPlanningProviderResolver(func(context.Context, int) (relay.Provider, error) { return provider, nil })
	first := NewService(client, resolver, nil)
	if _, err := first.Recover(ctx, RecoveryRequest{OperationID: operation.ID, Direction: RecoveryResume, InitiatedByUserID: 1}); err == nil {
		t.Fatal("synthetic source persistence failure unexpectedly succeeded")
	}
	failedDestination, failedSource := client.RelayGroupMapping.GetX(ctx, destination.ID), client.RelayGroupMapping.GetX(ctx, source.ID)
	if failedDestination.BaselineRevision != 1 || failedDestination.MemberAssignments["1"] != 101 || failedSource.BaselineRevision != 1 || failedSource.MemberAssignments["1"] != 101 {
		t.Fatalf("partial promotion escaped rollback: destination=%d/%v source=%d/%v", failedDestination.BaselineRevision, failedDestination.MemberAssignments, failedSource.BaselineRevision, failedSource.MemberAssignments)
	}
	if _, err := raw.ExecContext(ctx, `DROP TRIGGER reject_phase2_source_promotion ON relay_group_mappings; DROP FUNCTION reject_phase2_source_promotion()`); err != nil {
		t.Fatalf("remove source promotion failure: %v", err)
	}
	restarted := NewService(client, resolver, nil)
	if _, err := restarted.Recover(ctx, RecoveryRequest{OperationID: operation.ID, Direction: RecoveryResume, InitiatedByUserID: 1}); err != nil {
		t.Fatalf("restart Recover() error = %v", err)
	}
	appliedDestination, appliedSource := client.RelayGroupMapping.GetX(ctx, destination.ID), client.RelayGroupMapping.GetX(ctx, source.ID)
	if appliedDestination.BaselineRevision != 2 || appliedDestination.MemberAssignments["1"] != 102 || appliedSource.BaselineRevision != 2 {
		t.Fatalf("atomic promotion = destination:%d/%v source:%d/%v", appliedDestination.BaselineRevision, appliedDestination.MemberAssignments, appliedSource.BaselineRevision, appliedSource.MemberAssignments)
	}
	if _, exists := appliedSource.MemberAssignments["1"]; exists {
		t.Fatalf("source Mapping retained transferred member: %v", appliedSource.MemberAssignments)
	}
}

func setRecoveryProviderTarget(provider *recoveryProvider) {
	provider.subscriptions = []relay.UserSubscription{{UserID: 42, GroupID: 102, Status: "active"}, {UserID: 42, GroupID: 777, Status: "active"}}
	provider.keys[0].GroupID = 102
	provider.groups[1].Name = "Reviewed Target"
	provider.accounts[0].GroupRelationships = []relay.AccountGroupRelationship{{GroupID: 102, Priority: 2}, {GroupID: 777, Priority: 9}}
}
