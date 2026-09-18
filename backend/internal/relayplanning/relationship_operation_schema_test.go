package relayplanning

import (
	"context"
	"testing"

	"github.com/ai-efficiency/backend/ent/migrate"
	"github.com/ai-efficiency/backend/ent/relationshipoperationattempt"
	"github.com/ai-efficiency/backend/ent/relationshipoperationmapping"
	"github.com/ai-efficiency/backend/ent/relaygroupmapping"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestRelationshipOperationSchemaPersistsIndependentRecoveryEvidence(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	user := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SaveX(ctx)
	provider := client.RelayProvider.Create().SetName("relay-schema-test").SetDisplayName("Relay Schema Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SaveX(ctx)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(provider.ID).
		SetDepartmentExternalID("dept-alpha").
		SetDepartmentName("Department Alpha").
		SetPlatform("openai").
		SetGroupIds([]int64{101}).
		SetMemberAssignments(map[string]int64{"1": 101}).
		SaveX(ctx)
	if mapping.BaselineRevision != 1 {
		t.Fatalf("Mapping baseline revision = %d, want migration default 1", mapping.BaselineRevision)
	}

	operation := client.RelationshipOperation.Create().
		SetOperationKey("relationship-op-1").
		SetProviderID(provider.ID).
		SetPlatform("openai").
		SetBaselineSnapshot(map[string]any{"members": map[string]any{"1": float64(101)}}).
		SetTargetSnapshot(map[string]any{"members": map[string]any{"1": float64(102)}}).
		SetBaselineFingerprint("baseline-fingerprint-1").
		SetTargetFingerprint("target-fingerprint-1").
		SetSupportedDirections([]string{"resume", "restore"}).
		SetInitiatedByUserID(user.ID).
		SaveX(ctx)
	client.RelationshipOperationMapping.Create().
		SetOperationID(operation.ID).
		SetMappingID(mapping.ID).
		SetRole(relationshipoperationmapping.RolePrimary).
		SetBaselineRevision(mapping.BaselineRevision).
		SetBaselineSnapshot(map[string]any{"group_ids": []any{float64(101)}}).
		SaveX(ctx)

	forward := client.RelationshipOperationStep.Create().
		SetOperationID(operation.ID).
		SetStepKey("member:1:migrate:101:102").
		SetAction("migrate").
		SetRelationshipType("member_api_keys").
		SetDirection("target").
		SetLocalUserID(user.ID).
		SetRelayUserID(1001).
		SetSourceGroupID(101).
		SetTargetGroupID(102).
		SetReviewedResourceIds([]int64{501}).
		SetExpectedResult(map[string]any{"api_key_group_id": float64(102)}).
		SetResumeSupported(true).
		SetRestoreSupported(true).
		SaveX(ctx)
	reverse := client.RelationshipOperationStep.Create().
		SetOperationID(operation.ID).
		SetStepKey("member:1:restore:102:101").
		SetAction("restore").
		SetRelationshipType("member_api_keys").
		SetDirection("baseline").
		SetLocalUserID(user.ID).
		SetRelayUserID(1001).
		SetSourceGroupID(102).
		SetTargetGroupID(101).
		SetReviewedResourceIds([]int64{501}).
		SetExpectedResult(map[string]any{"api_key_group_id": float64(101)}).
		SetResumeSupported(false).
		SetRestoreSupported(true).
		SaveX(ctx)
	if forward.ID == reverse.ID || forward.StepKey == reverse.StepKey || forward.ExpectedResult["api_key_group_id"] == reverse.ExpectedResult["api_key_group_id"] {
		t.Fatalf("forward and restore evidence were not independent: forward=%+v restore=%+v", forward, reverse)
	}

	initialAttempt := client.RelationshipOperationAttempt.Create().
		SetOperationID(operation.ID).
		SetAttemptNumber(1).
		SetDirection(relationshipoperationattempt.DirectionInitial).
		SetInitiatedByUserID(user.ID).
		SaveX(ctx)
	restoreAttempt := client.RelationshipOperationAttempt.Create().
		SetOperationID(operation.ID).
		SetAttemptNumber(2).
		SetDirection(relationshipoperationattempt.DirectionRestore).
		SetInitiatedByUserID(user.ID).
		SaveX(ctx)
	if initialAttempt.Direction == restoreAttempt.Direction {
		t.Fatalf("attempt directions = %q / %q, want independent initial and restore", initialAttempt.Direction, restoreAttempt.Direction)
	}
	if _, err := client.RelationshipOperationAttempt.Create().
		SetOperationID(operation.ID).
		SetAttemptNumber(2).
		SetDirection(relationshipoperationattempt.DirectionResume).
		SetInitiatedByUserID(user.ID).
		Save(ctx); err == nil {
		t.Fatal("duplicate attempt number for one Operation succeeded")
	}

	unchanged := client.RelayGroupMapping.GetX(ctx, mapping.ID)
	if unchanged.BaselineRevision != 1 || unchanged.MemberAssignments["1"] != 101 {
		t.Fatalf("non-terminal Operation changed Mapping baseline: revision=%d assignments=%v", unchanged.BaselineRevision, unchanged.MemberAssignments)
	}
}

func TestRelationshipOperationSchemaEnforcesOwnershipAndBaselineCAS(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	user := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SaveX(ctx)
	provider := client.RelayProvider.Create().SetName("relay-ownership-test").SetDisplayName("Relay Ownership Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SaveX(ctx)
	createMapping := func(department string, target int64) int {
		return client.RelayGroupMapping.Create().SetProviderID(provider.ID).SetDepartmentExternalID(department).SetDepartmentName(department).SetPlatform("openai").SetGroupIds([]int64{target}).SaveX(ctx).ID
	}
	mappingA, mappingB := createMapping("dept-alpha", 101), createMapping("dept-beta", 102)
	createOperation := func(key string) int {
		return client.RelationshipOperation.Create().SetOperationKey(key).SetProviderID(provider.ID).SetPlatform("openai").SetBaselineSnapshot(map[string]any{}).SetTargetSnapshot(map[string]any{}).SetBaselineFingerprint(key + "-baseline").SetTargetFingerprint(key + "-target").SetSupportedDirections([]string{"resume"}).SetInitiatedByUserID(user.ID).SaveX(ctx).ID
	}
	operationA := createOperation("ownership-op-a")
	client.RelationshipOperationMapping.Create().SetOperationID(operationA).SetMappingID(mappingA).SetRole(relationshipoperationmapping.RolePrimary).SetBaselineRevision(1).SetBaselineSnapshot(map[string]any{}).SaveX(ctx)
	operationB := createOperation("ownership-op-b")
	if _, err := client.RelationshipOperationMapping.Create().SetOperationID(operationB).SetMappingID(mappingA).SetRole(relationshipoperationmapping.RolePrimary).SetBaselineRevision(1).SetBaselineSnapshot(map[string]any{}).Save(ctx); err == nil {
		t.Fatal("second active Operation ownership for one Mapping succeeded")
	}
	if _, err := client.RelationshipOperationMapping.Create().SetOperationID(operationB).SetMappingID(mappingA).SetRole(relationshipoperationmapping.RoleAffected).SetBaselineRevision(1).SetBaselineSnapshot(map[string]any{}).SetActive(false).Save(ctx); err != nil {
		t.Fatalf("released historical ownership should be retained: %v", err)
	}

	tx, err := client.Tx(ctx)
	if err != nil {
		t.Fatalf("begin multi-Mapping ownership transaction: %v", err)
	}
	operationC, err := tx.RelationshipOperation.Create().SetOperationKey("ownership-op-c").SetProviderID(provider.ID).SetPlatform("openai").SetBaselineSnapshot(map[string]any{}).SetTargetSnapshot(map[string]any{}).SetBaselineFingerprint("c-baseline").SetTargetFingerprint("c-target").SetSupportedDirections([]string{"resume", "restore"}).SetInitiatedByUserID(user.ID).Save(ctx)
	if err == nil {
		_, err = tx.RelationshipOperationMapping.Create().SetOperationID(operationC.ID).SetMappingID(mappingB).SetRole(relationshipoperationmapping.RoleDestination).SetBaselineRevision(1).SetBaselineSnapshot(map[string]any{}).Save(ctx)
	}
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("create transactional ownership: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit transactional ownership: %v", err)
	}

	updated, err := client.RelayGroupMapping.Update().Where(relaygroupmapping.IDEQ(mappingB), relaygroupmapping.BaselineRevisionEQ(1)).AddBaselineRevision(1).Save(ctx)
	if err != nil || updated != 1 {
		t.Fatalf("baseline CAS update = %d, error %v, want one row", updated, err)
	}
	updated, err = client.RelayGroupMapping.Update().Where(relaygroupmapping.IDEQ(mappingB), relaygroupmapping.BaselineRevisionEQ(1)).AddBaselineRevision(1).Save(ctx)
	if err != nil || updated != 0 {
		t.Fatalf("stale baseline CAS update = %d, error %v, want zero rows", updated, err)
	}
}

func TestRelationshipOperationSchemaKeepsActiveOwnershipPartialUnique(t *testing.T) {
	for _, index := range migrate.RelationshipOperationMappingsTable.Indexes {
		if index.Name == "relationshipoperationmapping_active_mapping_unique" {
			if !index.Unique || index.Annotation == nil || index.Annotation.Where != "active" {
				t.Fatalf("active ownership index = unique:%v annotation:%+v", index.Unique, index.Annotation)
			}
			return
		}
	}
	t.Fatal("active Mapping ownership partial unique index is missing")
}
