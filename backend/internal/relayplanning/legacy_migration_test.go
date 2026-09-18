package relayplanning

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relationshipoperation"
	"github.com/ai-efficiency/backend/ent/relationshipoperationmapping"
	"github.com/ai-efficiency/backend/ent/relationshipoperationstep"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestStructuralLegacyAmbiguityRequiresExactMemberIdentityAndFrozenResources(t *testing.T) {
	valid := map[string]map[string]string{
		"operation": {"status": "needs_retry", "intent_hash": "v1:reviewed"},
		"member:1":  {"subscription": "failed", "step_identity": "v1:step", "reviewed_api_key_ids": ""},
	}
	if reason := structuralLegacyAmbiguity(valid); reason != "" {
		t.Fatalf("valid explicit empty Key set reason = %q", reason)
	}
	tests := []struct {
		name, want string
		state      map[string]map[string]string
	}{
		{name: "missing intent", want: "missing_intent_identity", state: map[string]map[string]string{"operation": {"status": "needs_retry"}}},
		{name: "missing step", want: "missing_step_identity", state: map[string]map[string]string{"operation": {"intent_hash": "v1:x"}, "member:1": {"subscription": "failed", "reviewed_api_key_ids": ""}}},
		{name: "missing Key set", want: "missing_reviewed_resource_set", state: map[string]map[string]string{"operation": {"intent_hash": "v1:x"}, "member:1": {"subscription": "failed", "step_identity": "v1:y"}}},
		{name: "malformed member", want: "malformed_member_identity", state: map[string]map[string]string{"operation": {"intent_hash": "v1:x"}, "member:alice": {"subscription": "failed", "step_identity": "v1:y", "reviewed_api_key_ids": ""}}},
		{name: "unsupported effect", want: "unsupported_legacy_effect", state: map[string]map[string]string{"operation": {"intent_hash": "v1:x"}, "group:0": {"status": "failed"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := structuralLegacyAmbiguity(tt.state); got != tt.want {
				t.Fatalf("reason = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLegacyMigrationDryRunAndApplyAreFailClosedAndRelayReadOnly(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	service, provider, actor, exactMapping := createLegacyMigrationFixture(t, ctx, client)
	aligned := client.RelayGroupMapping.Create().SetProviderID(exactMapping.ProviderID).SetDepartmentExternalID("dept-aligned").SetDepartmentName("Aligned").SetPlatform("openai").SetTemplateGroupID(10).SetGroupIds([]int64{201}).SetStatus("active").SaveX(ctx)
	ambiguous := client.RelayGroupMapping.Create().SetProviderID(exactMapping.ProviderID).SetDepartmentExternalID("dept-ambiguous").SetDepartmentName("Ambiguous").SetPlatform("openai").SetTemplateGroupID(10).SetGroupIds([]int64{301}).SetStatus("needs_retry").SetOperationState(map[string]map[string]string{"operation": {"status": "needs_retry"}, "member:999": {"subscription": "failed"}}).SaveX(ctx)

	dry, err := service.AuditLegacyOperations(ctx)
	if err != nil {
		t.Fatalf("AuditLegacyOperations() error = %v", err)
	}
	if dry.Apply || dry.Counts[string(LegacyAligned)] != 1 || dry.Counts[string(LegacyReconstructibleCandidate)] != 1 || dry.Counts[string(LegacyBlockedManualReview)] != 1 {
		t.Fatalf("dry-run report = %+v", dry)
	}
	if count := client.RelationshipOperation.Query().CountX(ctx); count != 0 {
		t.Fatalf("dry-run created %d Operations", count)
	}
	legacyBefore := client.RelayGroupMapping.GetX(ctx, exactMapping.ID).OperationState

	report, err := service.MigrateLegacyOperations(ctx, LegacyMigrationRequest{Apply: true, InitiatedByUserID: actor.ID})
	if err != nil {
		t.Fatalf("MigrateLegacyOperations() error = %v", err)
	}
	if report.Counts[string(LegacyMigratedResumeOnly)] != 1 || report.Counts[string(LegacyBlockedManualReview)] != 1 || report.Counts[string(LegacyAligned)] != 1 {
		t.Fatalf("apply report = %+v", report)
	}
	if provider.assignmentCalls != 0 || len(provider.bound) != 0 {
		t.Fatalf("legacy migration wrote Relay: assignments=%d bindings=%v", provider.assignmentCalls, provider.bound)
	}
	if got := client.RelayGroupMapping.GetX(ctx, aligned.ID); got.BaselineRevision != 1 {
		t.Fatalf("aligned baseline revision = %d, want 1", got.BaselineRevision)
	}
	if client.RelationshipOperationMapping.Query().Where(relationshipoperationmapping.MappingIDEQ(aligned.ID)).ExistX(ctx) {
		t.Fatal("aligned Mapping received synthetic Operation history")
	}
	if got := client.RelayGroupMapping.GetX(ctx, exactMapping.ID); !reflect.DeepEqual(got.OperationState, legacyBefore) {
		t.Fatalf("legacy evidence changed: before=%v after=%v", legacyBefore, got.OperationState)
	}
	exactOwner := client.RelationshipOperationMapping.Query().Where(relationshipoperationmapping.MappingIDEQ(exactMapping.ID), relationshipoperationmapping.ActiveEQ(true)).OnlyX(ctx)
	exactOperation := client.RelationshipOperation.GetX(ctx, exactOwner.OperationID)
	if exactOperation.Lifecycle != relationshipoperation.LifecycleInterrupted || !reflect.DeepEqual(exactOperation.SupportedDirections, []string{"resume"}) {
		t.Fatalf("exact Operation = lifecycle:%q directions:%v", exactOperation.Lifecycle, exactOperation.SupportedDirections)
	}
	steps := client.RelationshipOperationStep.Query().Where(relationshipoperationstep.OperationIDEQ(exactOperation.ID)).AllX(ctx)
	if len(steps) == 0 {
		t.Fatal("exact migration created no immutable steps")
	}
	for _, step := range steps {
		if !step.ResumeSupported || step.RestoreSupported {
			t.Fatalf("migrated step %q directions = resume:%t restore:%t", step.StepKey, step.ResumeSupported, step.RestoreSupported)
		}
		if step.RelationshipType == "api_keys" && fmt.Sprint(step.ReviewedResourceIds) != "[501]" {
			t.Fatalf("migrated reviewed Key IDs = %v, want [501]", step.ReviewedResourceIds)
		}
	}
	blockedOwner := client.RelationshipOperationMapping.Query().Where(relationshipoperationmapping.MappingIDEQ(ambiguous.ID), relationshipoperationmapping.ActiveEQ(true)).OnlyX(ctx)
	blocked := client.RelationshipOperation.GetX(ctx, blockedOwner.OperationID)
	if blocked.Lifecycle != relationshipoperation.LifecycleBlockedExternal || blocked.ExternalBlocker["relationship"] != "missing_intent_identity" || len(blocked.SupportedDirections) != 0 {
		t.Fatalf("ambiguous Operation = lifecycle:%q blocker:%v directions:%v", blocked.Lifecycle, blocked.ExternalBlocker, blocked.SupportedDirections)
	}
}

func createLegacyMigrationFixture(t *testing.T, ctx context.Context, client *ent.Client) (*Service, *replanRetryProvider, *ent.User, *ent.RelayGroupMapping) {
	t.Helper()
	providerRow := client.RelayProvider.Create().SetName("legacy-migration-test").SetDisplayName("Legacy Migration Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SaveX(ctx)
	source, run := createRelayPlanningDirectorySnapshot(t, ctx, client, "dept-alpha")
	actor := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource(entuser.AuthSourceLdap).SetRelayUserID(1001).SaveX(ctx)
	member := client.DirectoryMember.Create().SetSourceID(source.ID).SetExternalID("member-alice").SetEmailNormalized(actor.Email).SetDisplayName(actor.Username).SetDepartmentExternalID("dept-alpha").SetMatchedUserID(actor.ID).SetLastSeenRunID(run.ID).SaveX(ctx)
	client.DirectoryMemberDepartment.Create().SetSourceID(source.ID).SetDirectoryMemberID(member.ID).SetMemberExternalID(member.ExternalID).SetMemberEmailNormalized(member.EmailNormalized).SetDepartmentExternalID("dept-alpha").SetLastSeenRunID(run.ID).SaveX(ctx)
	provider := &replanRetryProvider{
		groups:        []relay.Group{{ID: 10, Name: "Template", Platform: "openai"}, {ID: 20, Name: "Source", Platform: "openai"}, {ID: 101, Name: "Target A", Platform: "openai"}, {ID: 102, Name: "Target B", Platform: "openai"}},
		subscriptions: []relay.UserSubscription{{UserID: 1001, GroupID: 20, Status: "active"}, {UserID: 1001, GroupID: 101, Status: "active"}, {UserID: 1001, GroupID: 102, Status: "active"}},
		keys:          []relay.APIKey{{ID: 501, UserID: 1001, GroupID: 102, Status: "active"}},
	}
	service := NewService(client, relayPlanningProviderResolver(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	mapping := client.RelayGroupMapping.Create().SetProviderID(providerRow.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").SetTemplateGroupID(10).SetTemplateGroupName("Template").SetSourceGroupID(20).SetSourceGroupName("Source").SetGroupIds([]int64{101, 102}).SetMemberAssignments(map[string]int64{fmt.Sprint(actor.ID): 101}).SetMemberSources(map[string]int64{fmt.Sprint(actor.ID): 20}).SetStatus("active").SetWeeklyCostTarget(2500).SaveX(ctx)
	base := mappingFromEnt(mapping)
	plan := &Plan{ProviderID: providerRow.ID, DepartmentID: "dept-alpha", DepartmentName: "Department Alpha", Platform: "openai", TemplateGroupID: 10, SourceGroupID: 20, MappingID: mapping.ID, Candidates: []Candidate{{UserID: actor.ID, RelayUserID: 1001, relationshipAPIKeys: []relationshipAPIKeyFact{{ID: 501, GroupID: 101}}}}, Assignments: []Assignment{{Index: 0, TargetGroupID: 101, TargetGroupName: "Target A"}, {Index: 1, TargetGroupID: 102, TargetGroupName: "Target B", UserIDs: []int{actor.ID}}}}
	req := ExecuteRequest{PreviewRequest: PreviewRequest{Assignments: plan.Assignments}}
	intentHash, intents, err := buildLegacyReplanIntent(&base, plan, req)
	if err != nil {
		t.Fatalf("build legacy fixture intent: %v", err)
	}
	intent := intents[actor.ID]
	state := map[string]map[string]string{
		"operation":                      {"status": "needs_retry", "intent_hash": intentHash, "key": "legacy-op"},
		"member:" + fmt.Sprint(actor.ID): {"from_group_id": "101", "target_group_id": "102", "reviewed_api_key_ids": "501", "step_identity": legacyMemberStepIdentity(intent), "subscription": "succeeded", "source_removal": "failed", "api_keys": "501:succeeded", "error": "synthetic source removal failure"},
	}
	mapping = client.RelayGroupMapping.UpdateOneID(mapping.ID).SetMemberAssignments(map[string]int64{fmt.Sprint(actor.ID): 102}).SetOperationState(state).SetStatus("needs_retry").SaveX(ctx)
	return service, provider, actor, mapping
}
