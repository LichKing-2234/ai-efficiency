package relayplanning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/directorysource"
	"github.com/ai-efficiency/backend/ent/directorysyncrun"
	"github.com/ai-efficiency/backend/ent/relationshipoperation"
	"github.com/ai-efficiency/backend/ent/relationshipoperationattempt"
	"github.com/ai-efficiency/backend/ent/relationshipoperationmapping"
	"github.com/ai-efficiency/backend/ent/relationshipoperationstep"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/teamusage"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestRedactedProviderReadErrorHidesMessageAndPreservesCause(t *testing.T) {
	cause := errors.New("synthetic sensitive provider detail")
	err := fmt.Errorf("read Relay relationships: %w", redactProviderReadError(cause))
	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is(%v, cause) = false, want true", err)
	}
	if strings.Contains(err.Error(), cause.Error()) {
		t.Fatalf("error %q exposes provider detail", err)
	}
}

func TestPreviewRequestJSONUsesSnakeCase(t *testing.T) {
	var got PreviewRequest
	if err := json.Unmarshal([]byte(`{"provider_id":7,"department_id":"dept-alpha","platform":"openai","template_group_id":84,"source_group_id":42,"weekly_cost_target":12.5,"group_count":2,"selected_user_ids":[1,2],"existing_mapping_id":9}`), &got); err != nil {
		t.Fatalf("unmarshal preview request: %v", err)
	}
	if got.ProviderID != 7 || got.DepartmentID != "dept-alpha" || got.Platform != "openai" || got.TemplateGroupID != 84 || got.SourceGroupID != 42 || got.WeeklyCostTarget != 12.5 || got.GroupCount != 2 || len(got.SelectedUserIDs) != 2 || got.ExistingMappingID != 9 {
		t.Fatalf("decoded preview request = %#v", got)
	}
}

func TestNormalizeRequestKeepsTemplateAndSourceIndependent(t *testing.T) {
	got := normalizeRequest(PreviewRequest{DepartmentID: " dept-alpha ", Platform: " openai ", TemplateGroupID: 84, SourceGroupID: 42})
	if got.TemplateGroupID != 84 || got.SourceGroupID != 42 || got.DepartmentID != "dept-alpha" || got.Platform != "openai" {
		t.Fatalf("normalized request = %#v", got)
	}
	got = normalizeRequest(PreviewRequest{TemplateGroupID: 84})
	if got.TemplateGroupID != 84 || got.SourceGroupID != 0 {
		t.Fatalf("source-free request did not remain target-only: %#v", got)
	}
}

func TestValidateAssignmentsAllowsExplicitNonSourceMemberOnce(t *testing.T) {
	assignments, err := validateAssignments([]Assignment{{Index: 0, UserIDs: []int{2}}}, []Candidate{
		{UserID: 1, RangeCost: 10, SourceMember: true, CanAdd: true},
		{UserID: 2, RangeCost: 3, SourceMember: false, CanAdd: true},
	}, 1)
	if err != nil {
		t.Fatalf("validateAssignments() error = %v", err)
	}
	if assignments[0].TotalCost != 3 || len(assignments[0].UserIDs) != 1 || assignments[0].UserIDs[0] != 2 {
		t.Fatalf("validated assignments = %#v", assignments)
	}
}

func TestAssignCandidateDispositionsDistinguishesRetainedAndReviewedChanges(t *testing.T) {
	mapping := &ent.RelayGroupMapping{
		MemberAssignments: map[string]int64{"1": 101, "6": 101, "7": 101},
		OperationState:    map[string]map[string]string{"member:6": {"status": "failed"}},
	}
	candidates := []Candidate{
		{UserID: 1, CanAdd: true, CurrentGroupIDs: []int64{101}, SourceGroupID: 20},
		{UserID: 2, CanAdd: true},
		{UserID: 3, CanAdd: true, SourceGroupID: 20},
		{UserID: 4, CanAdd: true},
		{UserID: 5},
		{UserID: 6, CanAdd: true, CurrentGroupIDs: []int64{101}, SourceGroupID: 20},
		{UserID: 7, CanAdd: true, CurrentGroupIDs: []int64{101}, SourceGroupID: 20, replanUnavailableReason: replanRosterUnavailableSubscription},
	}
	assignments := []Assignment{{Index: 0, TargetGroupID: 101, UserIDs: []int{1, 2, 3}}}

	assignCandidateDispositions(mapping, candidates, assignments)

	want := []string{"retained", "target_only", "migration", "available", "excluded", "available", "available"}
	for index := range candidates {
		if candidates[index].Disposition != want[index] {
			t.Fatalf("candidate %d disposition = %q, want %q", candidates[index].UserID, candidates[index].Disposition, want[index])
		}
	}
	if !candidates[0].CanRetain || candidates[5].CanRetain || candidates[6].CanRetain {
		t.Fatalf("can_retain facts = %v/%v/%v, want aligned only", candidates[0].CanRetain, candidates[5].CanRetain, candidates[6].CanRetain)
	}
}

func TestZeroChangeReplanRequiresIdenticalPersistedRelationshipState(t *testing.T) {
	mapping := Mapping{
		ID: 9, ProviderID: 7, DepartmentID: "dept-alpha", DepartmentName: "Department Alpha", Platform: "openai",
		TemplateGroupID: 10, TemplateGroupName: "Template", SourceGroupID: 20, SourceGroupName: "Source",
		GroupIDs: []int64{101}, MemberAssignments: map[string]int64{"1": 101}, MemberSources: map[string]int64{"1": 20},
		AccountManagementInitialized: true, DesiredAccounts: map[string][]AccountIntent{"101": {{AccountID: 11, Priority: 1}}}, WeeklyCostTarget: 2500,
	}
	plan := &Plan{
		ProviderID: 7, DepartmentID: "dept-alpha", DepartmentName: "Department Alpha", Platform: "openai",
		TemplateGroupID: 10, TemplateGroupName: "Template", SourceGroupID: 20, SourceGroupName: "Source", WeeklyCostTarget: 2500,
		Candidates:      []Candidate{{UserID: 1, SourceGroupID: 20}},
		Assignments:     []Assignment{{Index: 0, TargetGroupID: 101, UserIDs: []int{1}, DesiredAccounts: []AccountIntent{{AccountID: 11, Priority: 1}}}},
		TargetSummaries: []TargetChangeSummary{{Index: 0}}, AccountsReviewed: true,
	}
	if !isZeroChangeReplan(mapping, plan, ExecuteRequest{}) {
		t.Fatal("identical Replan was not recognized as zero-change")
	}
	changed := *plan
	changed.WeeklyCostTarget = 3000
	if isZeroChangeReplan(mapping, &changed, ExecuteRequest{}) {
		t.Fatal("changed Mapping cost was treated as zero-change")
	}
}

func TestValidateAssignmentsRejectsDuplicateMember(t *testing.T) {
	_, err := validateAssignments([]Assignment{{Index: 0, UserIDs: []int{1}}, {Index: 1, UserIDs: []int{1}}}, []Candidate{{UserID: 1, CanAdd: true}}, 2)
	if err == nil || !strings.Contains(err.Error(), "assigned more than once") {
		t.Fatalf("validateAssignments() error = %v, want duplicate-member error", err)
	}
}

func TestMappingAvailabilityWarningsArePlatformScoped(t *testing.T) {
	warnings := mappingAvailabilityWarnings(Mapping{Platform: "openai", TemplateGroupID: 11, SourceGroupID: 12, GroupIDs: []int64{13}}, []relay.Group{
		{ID: 11, Platform: "openai"},
		{ID: 12, Platform: "anthropic"},
		{ID: 13, Platform: "openai"},
	})
	if len(warnings) != 1 || warnings[0] != "migration source group 12 is unavailable" {
		t.Fatalf("mapping availability warnings = %#v", warnings)
	}
}

func TestFindGroupForPlatformRejectsCrossPlatformAndMissingGroups(t *testing.T) {
	groups := []relay.Group{{ID: 11, Name: "Group Alpha", Platform: "openai"}, {ID: 12, Platform: "anthropic"}}
	if _, err := findGroupForPlatform(groups, 12, "openai", "template"); err == nil || !strings.Contains(err.Error(), "does not belong to platform") {
		t.Fatalf("cross-platform group error = %v", err)
	}
	if _, err := findGroupForPlatform(groups, 99, "openai", "target"); err == nil || !strings.Contains(err.Error(), "is unavailable") {
		t.Fatalf("missing group error = %v", err)
	}
}

func TestExecutionStateRetainsRetryableStepErrors(t *testing.T) {
	state := executionState("op-1", []GroupResult{{Index: 0, ID: 41, Status: "succeeded"}, {Index: 1, Status: "failed", Error: "upstream timeout"}}, []MemberResult{{Action: "remove", UserID: 7, TargetGroupID: 41, Subscription: "skipped", SourceRemoval: "skipped", Error: "assign failed", reviewedAPIKeys: reviewedAPIKeySelection{IDs: []int64{7, 9}, Frozen: true}}, {UserID: 8, TargetGroupID: 41, Subscription: "succeeded", SourceRemoval: "skipped", APIKeys: []string{"9:failed:bind timeout"}}, {Action: "remove", UserID: 9, TargetGroupID: 41, Subscription: "skipped", SourceRemoval: "skipped", Error: "assign failed", reviewedAPIKeys: reviewedAPIKeySelection{Frozen: true}}})
	if state["operation"]["status"] != "needs_retry" || state["group:1"]["error"] != "upstream timeout" || state["member:7"]["error"] != "assign failed" {
		t.Fatalf("execution state = %#v", state)
	}
	if state["member:7"]["reviewed_api_key_ids"] != "7,9" {
		t.Fatalf("reviewed API Key state = %#v, want IDs persisted before writes", state["member:7"])
	}
	if reviewed, exists := state["member:9"]["reviewed_api_key_ids"]; !exists || reviewed != "" {
		t.Fatalf("empty reviewed API Key state = %#v, want explicit empty marker", state["member:9"])
	}
	if !operationStateNeedsRetry(state, "member:7") || !operationStateNeedsRetry(state, "member:8") || !operationStateNeedsRetry(state, "member:9") || operationStateNeedsRetry(state, "member:10") {
		t.Fatalf("retry lookup = member7:%v member8:%v member9:%v member10:%v", operationStateNeedsRetry(state, "member:7"), operationStateNeedsRetry(state, "member:8"), operationStateNeedsRetry(state, "member:9"), operationStateNeedsRetry(state, "member:10"))
	}
}

func TestLegacyReplanIntentBindsExactReviewedDirection(t *testing.T) {
	build := func() (*Mapping, *Plan, ExecuteRequest) {
		mapping := &Mapping{ID: 7, ProviderID: 9, Platform: "openai", TemplateGroupID: 10, SourceGroupID: 20, GroupIDs: []int64{101, 102}, MemberAssignments: map[string]int64{"1": 101}, MemberSources: map[string]int64{"1": 20}, OperationState: map[string]map[string]string{}}
		plan := &Plan{Candidates: []Candidate{{UserID: 1, RelayUserID: 42, CurrentGroupIDs: []int64{101}, relationshipAPIKeys: []relationshipAPIKeyFact{{ID: 501, GroupID: 101}}}}, Assignments: []Assignment{
			{Index: 0, TargetGroupID: 101, TargetGroupName: "Group Alpha", DesiredAccounts: []AccountIntent{{AccountID: 11, Priority: 1}}},
			{Index: 1, TargetGroupID: 102, TargetGroupName: "Group Beta", UserIDs: []int{1}, DesiredAccounts: []AccountIntent{{AccountID: 12, Priority: 1}}},
		}}
		return mapping, plan, ExecuteRequest{PreviewRequest: PreviewRequest{Assignments: plan.Assignments, MemberActions: map[string]MemberAction{}}}
	}
	baseMapping, basePlan, baseReq := build()
	baseHash, baseMembers, err := buildLegacyReplanIntent(baseMapping, basePlan, baseReq)
	if err != nil {
		t.Fatalf("build base legacy intent: %v", err)
	}
	if got := baseMembers[1]; got.Action != "migrate" || got.SourceGroupID != 101 || got.TargetGroupID != 102 || fmt.Sprint(got.APIKeyIDs) != "[501]" {
		t.Fatalf("base member intent = %+v, want exact 101 -> 102 migration for Key 501", got)
	}
	tests := []struct {
		name   string
		mutate func(*Mapping, *Plan, *ExecuteRequest)
	}{
		{name: "action", mutate: func(_ *Mapping, _ *Plan, req *ExecuteRequest) {
			req.MemberActions["1"] = MemberAction{Mode: "add_additionally"}
		}},
		{name: "source", mutate: func(mapping *Mapping, _ *Plan, _ *ExecuteRequest) { mapping.MemberAssignments["1"] = 20 }},
		{name: "target", mutate: func(_ *Mapping, plan *Plan, _ *ExecuteRequest) { plan.Assignments[1].TargetGroupID = 103 }},
		{name: "local identity", mutate: func(mapping *Mapping, plan *Plan, _ *ExecuteRequest) {
			mapping.MemberAssignments = map[string]int64{"2": 101}
			plan.Assignments[1].UserIDs = []int{2}
			plan.Candidates[0].UserID = 2
		}},
		{name: "Relay identity", mutate: func(_ *Mapping, plan *Plan, _ *ExecuteRequest) { plan.Candidates[0].RelayUserID = 43 }},
		{name: "reviewed API Key set", mutate: func(_ *Mapping, plan *Plan, _ *ExecuteRequest) { plan.Candidates[0].relationshipAPIKeys[0].ID = 502 }},
		{name: "Account priority", mutate: func(_ *Mapping, plan *Plan, _ *ExecuteRequest) { plan.Assignments[1].DesiredAccounts[0].Priority = 2 }},
		{name: "Target name", mutate: func(_ *Mapping, plan *Plan, _ *ExecuteRequest) { plan.Assignments[1].TargetGroupName = "Group Gamma" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapping, plan, req := build()
			tt.mutate(mapping, plan, &req)
			hash, _, err := buildLegacyReplanIntent(mapping, plan, req)
			if err != nil {
				t.Fatalf("build changed intent: %v", err)
			}
			if hash == baseHash {
				t.Fatalf("changed %s retained intent hash %q", tt.name, hash)
			}
		})
	}
	changedExpected := baseMembers[1]
	changedExpected.ExpectedResult = "different"
	if legacyMemberStepIdentity(changedExpected) == legacyMemberStepIdentity(baseMembers[1]) {
		t.Fatal("changed expected result retained member step identity")
	}
	changedRelationship := baseMembers[1]
	changedRelationship.RelationshipType = "different"
	if legacyMemberStepIdentity(changedRelationship) == legacyMemberStepIdentity(baseMembers[1]) {
		t.Fatal("changed relationship type retained member step identity")
	}
}

func TestLegacyReplanIntentStaysStableAcrossExactRetryAndFreezesEmptyKeys(t *testing.T) {
	mapping := &Mapping{ID: 7, ProviderID: 9, Platform: "openai", TemplateGroupID: 10, SourceGroupID: 20, GroupIDs: []int64{101, 102}, MemberAssignments: map[string]int64{"1": 101}, OperationState: map[string]map[string]string{}}
	plan := &Plan{Candidates: []Candidate{{UserID: 1, RelayUserID: 42, relationshipAPIKeys: []relationshipAPIKeyFact{{ID: 501, GroupID: 101}}}}, Assignments: []Assignment{{Index: 0, TargetGroupID: 101, TargetGroupName: "Group Alpha"}, {Index: 1, TargetGroupID: 102, TargetGroupName: "Group Beta", UserIDs: []int{1}}}}
	req := ExecuteRequest{PreviewRequest: PreviewRequest{Assignments: plan.Assignments}}
	initialHash, _, err := buildLegacyReplanIntent(mapping, plan, req)
	if err != nil {
		t.Fatalf("build initial intent: %v", err)
	}
	mapping.Status = "needs_retry"
	mapping.MemberAssignments["1"] = 102
	mapping.OperationState = map[string]map[string]string{
		"operation": {"status": "needs_retry", "intent_hash": initialHash},
		"member:1":  {"from_group_id": "101", "reviewed_api_key_ids": "501", "subscription": "succeeded", "source_removal": "failed", "error": "synthetic retry"},
	}
	plan.Candidates[0].relationshipAPIKeys = []relationshipAPIKeyFact{{ID: 501, GroupID: 102}, {ID: 502, GroupID: 101}}
	retryHash, retryMembers, err := buildLegacyReplanIntent(mapping, plan, req)
	if err != nil {
		t.Fatalf("build retry intent: %v", err)
	}
	if retryHash != initialHash || fmt.Sprint(retryMembers[1].APIKeyIDs) != "[501]" {
		t.Fatalf("retry hash/Keys = %q/%v, want %q/[501]", retryHash, retryMembers[1].APIKeyIDs, initialHash)
	}
	state := executionState("empty-keys", nil, []MemberResult{{UserID: 2, reviewedAPIKeys: reviewedAPIKeySelection{Frozen: true}}})
	if value, exists := state["member:2"]["reviewed_api_key_ids"]; !exists || value != "" {
		t.Fatalf("explicit empty reviewed Key set = %q/%v, want present empty value", value, exists)
	}
}

func TestLegacyRetryValidationRejectsIncompleteMemberIdentityAndAllowsActiveMapping(t *testing.T) {
	mapping := &Mapping{Status: "needs_retry", OperationState: map[string]map[string]string{
		"operation": {"status": "needs_retry", "intent_hash": "v1:reviewed"},
		"member:1":  {"subscription": "failed", "error": "synthetic retry"},
	}}
	err := validateLegacyRetryIntent(mapping, "v1:reviewed")
	var conflict *LegacyOperationConflictError
	if !errors.As(err, &conflict) || conflict.Reason != "incomplete_identity" {
		t.Fatalf("incomplete member identity error = %v, want incomplete_identity", err)
	}
	if err := blockUnrelatedLegacyMutation(&ent.RelayGroupMapping{Status: "active"}); err != nil {
		t.Fatalf("active Mapping mutation blocked: %v", err)
	}
}

func TestOperationStateRetryPrefersOriginalSourceForFailedMigration(t *testing.T) {
	state := map[string]map[string]string{
		"member:7": {"subscription": "succeeded", "source_removal": "failed"},
	}
	if !operationStateNeedsRetry(state, "member:7") {
		t.Fatal("failed source removal must remain retryable")
	}
}

func TestPreviewDoesNotCreateDefaultGroupForNonSourceOnlyCandidates(t *testing.T) {
	recommended, count := resolveGroupCount(PreviewRequest{WeeklyCostTarget: 2500}, nil)
	if recommended != 0 || count != 0 {
		t.Fatalf("empty eligible recommendation = %d/%d, want 0/0", recommended, count)
	}
}

type departmentWarningPreviewProvider struct {
	relay.Provider
	users map[int64]*relay.User
}

func (p departmentWarningPreviewProvider) GetUser(_ context.Context, userID int64) (*relay.User, error) {
	return p.users[userID], nil
}

func (departmentWarningPreviewProvider) ListUserSubscriptions(context.Context, int64) ([]relay.UserSubscription, error) {
	return []relay.UserSubscription{{GroupID: 42, Status: "active"}}, nil
}

func (departmentWarningPreviewProvider) ListUserAPIKeys(context.Context, int64) ([]relay.APIKey, error) {
	return nil, nil
}

func (departmentWarningPreviewProvider) ListPlatformGroups(context.Context) ([]relay.Group, error) {
	return []relay.Group{
		{ID: 42, Name: "Group Source", Platform: "openai"},
		{ID: 84, Name: "Group Template", Platform: "openai"},
	}, nil
}

func (departmentWarningPreviewProvider) ListAccountsForPlatform(context.Context, string) ([]relay.Account, error) {
	return []relay.Account{}, nil
}

func (departmentWarningPreviewProvider) GetBatchUserUsageStats(_ context.Context, userIDs []int64, _ relay.TeamUsageSummaryParams) (map[int64]relay.TeamUserUsageStats, error) {
	stats := make(map[int64]relay.TeamUserUsageStats, len(userIDs))
	for _, userID := range userIDs {
		cost, tokens := 1.0, int64(1)
		stats[userID] = relay.TeamUserUsageStats{UserID: userID, RangeActualCost: &cost, RangeTotalTokens: &tokens}
	}
	return stats, nil
}

func TestPreviewDepartmentMembershipWarningUsesCurrentEffectiveHierarchy(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	selectedDepartment := "dept-selected"
	source, currentRun := createRelayPlanningDirectorySnapshot(t, ctx, client, selectedDepartment)
	selected := client.DirectoryDepartment.Query().Where(
		directorydepartment.SourceIDEQ(source.ID),
		directorydepartment.ExternalIDEQ(selectedDepartment),
	).OnlyX(ctx)
	client.DirectoryDepartment.UpdateOne(selected).SetEffectiveParentExternalID("").SaveX(ctx)
	client.DirectoryDepartment.Create().SetSourceID(source.ID).SetExternalID("dept-child").SetName("Department Child").SetPath("synthetic/dept-selected/dept-child").SetEffectiveParentExternalID(selectedDepartment).SetLastSeenRunID(currentRun.ID).SaveX(ctx)
	client.DirectoryDepartment.Create().SetSourceID(source.ID).SetExternalID("dept-outside").SetName("Department Outside").SetPath("synthetic/dept-outside").SetEffectiveParentExternalID("").SetLastSeenRunID(currentRun.ID).SaveX(ctx)

	staleRun := client.DirectorySyncRun.Create().SetSourceID(source.ID).SetMode(directorysyncrun.ModeApply).SetStatus(directorysyncrun.StatusCompleted).SetPhase(directorysyncrun.PhaseCompleted).SetCompletedAt(time.Now().UTC().Add(-time.Hour)).SaveX(ctx)

	tests := []struct {
		name         string
		email        string
		memberEmail  string
		memberships  []string
		noRelay      bool
		duplicate    bool
		staleOutside bool
		wantWarning  string
	}{
		{name: "selected department", email: "alice@example.com", memberships: []string{selectedDepartment}},
		{name: "descendant department", email: "bob@example.org", memberships: []string{"dept-child"}},
		{name: "outside selected subtree", email: "carol@example.net", memberships: []string{"dept-outside"}, wantWarning: "user is not in the selected department"},
		{name: "duplicate current membership", email: "dana@example.edu", memberships: []string{"dept-child"}, duplicate: true},
		{name: "multiple effective memberships", email: "erin@example.com", memberships: []string{selectedDepartment, "dept-outside"}, wantWarning: "user belongs to multiple departments"},
		{name: "stale membership ignored", email: "frank@example.org", memberships: []string{selectedDepartment}, staleOutside: true},
		{name: "matched local user without relay identity", email: "grace@example.net", memberEmail: "directory-grace@example.org", memberships: []string{"dept-outside"}, noRelay: true, wantWarning: "user is not in the selected department"},
	}
	users := make([]*ent.User, 0, len(tests))
	for index, tt := range tests {
		userBuilder := client.User.Create().SetUsername(fmt.Sprintf("candidate-%d", index)).SetEmail(tt.email).SetAuthSource(entuser.AuthSourceLdap)
		if !tt.noRelay {
			userBuilder.SetRelayUserID(1000 + index)
		}
		user := userBuilder.SaveX(ctx)
		users = append(users, user)
		memberEmail := tt.memberEmail
		if memberEmail == "" {
			memberEmail = tt.email
		}
		member := client.DirectoryMember.Create().SetSourceID(source.ID).SetExternalID(fmt.Sprintf("member-%d", index)).SetEmailNormalized(memberEmail).SetDisplayName(user.Username).SetDepartmentExternalID(tt.memberships[0]).SetMatchedUserID(user.ID).SetLastSeenRunID(currentRun.ID).SaveX(ctx)
		if tt.duplicate || len(tt.memberships) > 1 {
			// Explicit membership supersedes the identical primary fallback without creating a second effective membership.
			for _, departmentID := range tt.memberships {
				client.DirectoryMemberDepartment.Create().SetSourceID(source.ID).SetDirectoryMemberID(member.ID).SetMemberExternalID(member.ExternalID).SetMemberEmailNormalized(member.EmailNormalized).SetDepartmentExternalID(departmentID).SetLastSeenRunID(currentRun.ID).SaveX(ctx)
			}
		}
		if tt.staleOutside {
			client.DirectoryMemberDepartment.Create().SetSourceID(source.ID).SetDirectoryMemberID(member.ID).SetMemberExternalID(member.ExternalID).SetMemberEmailNormalized(member.EmailNormalized).SetDepartmentExternalID("dept-outside").SetLastSeenRunID(staleRun.ID).SaveX(ctx)
		}
	}
	provider := departmentWarningPreviewProvider{users: make(map[int64]*relay.User, len(users))}
	for _, user := range users {
		if user.RelayUserID == nil {
			continue
		}
		provider.users[int64(*user.RelayUserID)] = &relay.User{ID: int64(*user.RelayUserID), Username: user.Username, Email: user.Email}
	}
	providerRow := client.RelayProvider.Create().
		SetName("relay-planning-department-warning").
		SetDisplayName("Relay Planning Department Warning").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetRelayType("sub2api").
		SaveX(ctx)
	service := NewService(client, relayPlanningProviderResolver(func(context.Context, int) (relay.Provider, error) {
		return provider, nil
	}), nil)
	selectedUserIDs := make([]int, 0, len(users))
	for _, user := range users {
		selectedUserIDs = append(selectedUserIDs, user.ID)
	}
	request := PreviewRequest{
		ProviderID: providerRow.ID, DepartmentID: selectedDepartment, Platform: "openai",
		TemplateGroupID: 84, SourceGroupID: 42, WeeklyCostTarget: 100, GroupCount: 1, SelectedUserIDs: selectedUserIDs,
	}
	plan, err := service.Preview(ctx, request)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal Preview: %v", err)
	}
	var responsePlan Plan
	if err := json.Unmarshal(encoded, &responsePlan); err != nil {
		t.Fatalf("unmarshal Preview: %v", err)
	}
	byUserID := make(map[int]Candidate, len(responsePlan.Candidates))
	for _, candidate := range responsePlan.Candidates {
		byUserID[candidate.UserID] = candidate
	}
	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := byUserID[users[index].ID]
			got := ""
			for _, warning := range candidate.Warnings {
				if warning == "user is not in the selected department" || warning == "user belongs to multiple departments" {
					got = warning
				}
			}
			if got != tt.wantWarning {
				t.Fatalf("serialized Preview department warning = %q, want %q; warnings=%v", got, tt.wantWarning, candidate.Warnings)
			}
		})
	}
}

func TestPreviewFailsWithoutCurrentSuccessfulDirectorySnapshot(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	user := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource(entuser.AuthSourceLdap).SetRelayUserID(1001).SaveX(ctx)
	provider := departmentWarningPreviewProvider{users: map[int64]*relay.User{
		1001: {ID: 1001, Username: user.Username, Email: user.Email},
	}}
	providerRow := client.RelayProvider.Create().
		SetName("relay-planning-missing-directory-snapshot").
		SetDisplayName("Relay Planning Missing Directory Snapshot").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetRelayType("sub2api").
		SaveX(ctx)
	service := NewService(client, relayPlanningProviderResolver(func(context.Context, int) (relay.Provider, error) {
		return provider, nil
	}), nil)
	_, err := service.Preview(ctx, PreviewRequest{
		ProviderID: providerRow.ID, DepartmentID: "dept-selected", Platform: "openai",
		TemplateGroupID: 84, SourceGroupID: 42, WeeklyCostTarget: 100, GroupCount: 1, SelectedUserIDs: []int{user.ID},
	})
	if err == nil || !strings.Contains(err.Error(), "current successful directory snapshot is unavailable") {
		t.Fatalf("Preview() error = %v, want missing current successful snapshot", err)
	}
}

func TestAllocatePlacesNextMemberInLowestCostGroup(t *testing.T) {
	candidates := []Candidate{
		{UserID: 1, RangeCost: 8, Eligible: true},
		{UserID: 2, RangeCost: 7, Eligible: true},
		{UserID: 3, RangeCost: 4, Eligible: true},
		{UserID: 4, RangeCost: 3, Eligible: true},
	}
	got := allocate(candidates, 2)
	if len(got) != 2 {
		t.Fatalf("assignment count = %d, want 2", len(got))
	}
	if got[0].TotalCost != 11 || got[1].TotalCost != 11 {
		t.Fatalf("balanced costs = %.1f/%.1f, want 11/11", got[0].TotalCost, got[1].TotalCost)
	}
	if len(got[0].UserIDs) != 2 || len(got[1].UserIDs) != 2 {
		t.Fatalf("user distribution = %#v, want two users per group", got)
	}
}

func TestAllocateUsesStableGroupTieBreak(t *testing.T) {
	got := allocate([]Candidate{{UserID: 2, RangeCost: 1}, {UserID: 1, RangeCost: 1}}, 2)
	if got[0].UserIDs[0] != 2 || got[1].UserIDs[0] != 1 {
		t.Fatalf("tie break assignments = %#v, want insertion order across empty groups", got)
	}
}

func TestAllocateSerializesEmptyGroupsAsEmptyUserLists(t *testing.T) {
	got, err := json.Marshal(allocate(nil, 2))
	if err != nil {
		t.Fatalf("marshal assignments: %v", err)
	}
	if string(got) != `[{"index":0,"total_cost":0,"user_ids":[]},{"index":1,"total_cost":0,"user_ids":[]}]` {
		t.Fatalf("assignments JSON = %s, want empty user arrays", got)
	}
}

func TestResolveGroupCountCapsRecommendationAtEligibleMembers(t *testing.T) {
	candidates := []Candidate{
		{UserID: 1, RangeCost: 10000},
		{UserID: 2, RangeCost: 10000},
		{UserID: 3, RangeCost: 10000},
		{UserID: 4, RangeCost: 10000},
	}
	recommended, count := resolveGroupCount(PreviewRequest{WeeklyCostTarget: 2500, GroupCount: 2}, candidates)
	if recommended != 4 || count != 4 {
		t.Fatalf("group counts = recommended %d / planned %d, want 4 / 4", recommended, count)
	}
}

func TestResolveGroupCountAllowsExplicitReplanResize(t *testing.T) {
	candidates := []Candidate{{UserID: 1, RangeCost: 10000}, {UserID: 2, RangeCost: 10000}}
	recommended, count := resolveGroupCount(PreviewRequest{WeeklyCostTarget: 2500, GroupCount: 1, ExistingMappingID: 9}, candidates)
	if recommended != 2 || count != 1 {
		t.Fatalf("group counts = recommended %d / planned %d, want 2 / 1", recommended, count)
	}
}

func TestMappingRenewalServiceBindsTermAndRetriesFailedMemberWithStableKey(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	alice := client.User.Create().SetUsername("renewal-service-alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(41).SaveX(ctx)
	bob := client.User.Create().SetUsername("renewal-service-bob").SetEmail("bob@example.org").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	carol := client.User.Create().SetUsername("renewal-service-carol").SetEmail("carol@example.net").SetAuthSource("ldap").SetRelayUserID(43).SaveX(ctx)
	dana := client.User.Create().SetUsername("renewal-service-dana").SetEmail("dana@example.edu").SetAuthSource("ldap").SetRelayUserID(44).SaveX(ctx)
	mapping := client.RelayGroupMapping.Create().SetProviderID(7).SetDepartmentExternalID("dept-renewal-service").SetDepartmentName("Department Renewal").SetPlatform("openai").SetTemplateGroupID(10).SetGroupIds([]int64{101, 102, 103, 104}).SetMemberAssignments(map[string]int64{fmt.Sprint(alice.ID): 101, fmt.Sprint(bob.ID): 102, fmt.Sprint(carol.ID): 103, fmt.Sprint(dana.ID): 104}).SaveX(ctx)
	provider := &renewalServiceTestProvider{
		users: map[int64]*relay.User{
			41: {ID: 41, Username: alice.Username, Email: alice.Email},
			42: {ID: 42, Username: bob.Username, Email: bob.Email},
			43: {ID: 43, Username: carol.Username, Email: carol.Email},
			44: {ID: 44, Username: dana.Username, Email: dana.Email},
		},
		groups: []relay.Group{{ID: 101, Name: "Group Active", Platform: "openai"}, {ID: 102, Name: "Group Expired", Platform: "openai"}, {ID: 103, Name: "Group Missing", Platform: "openai"}, {ID: 104, Name: "Group Suspended", Platform: "openai"}, {ID: 999, Name: "Group Drift", Platform: "openai"}},
		subscriptions: map[int64][]relay.UserSubscription{
			41: {{ID: 1, UserID: 41, GroupID: 101, Status: "active", ExpiresAt: time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)}, {ID: 4, UserID: 41, GroupID: 999, Status: "active", ExpiresAt: time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)}},
			42: {{ID: 2, UserID: 42, GroupID: 102, Status: "expired", ExpiresAt: time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)}},
			43: {},
			44: {{ID: 3, UserID: 44, GroupID: 104, Status: "suspended", ExpiresAt: time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)}},
		},
		failures: make(map[int64]error),
	}
	service := NewService(client, relayPlanningProviderResolver(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	clock := time.Date(2029, time.December, 31, 0, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return clock }
	days365 := 365
	preview365, err := service.PreviewMappingRenewal(ctx, mapping.ID, MappingRenewalPreviewRequest{RenewalDays: &days365})
	if err != nil {
		t.Fatalf("preview 365 days: %v", err)
	}
	days30 := 30
	preview30, err := service.PreviewMappingRenewal(ctx, mapping.ID, MappingRenewalPreviewRequest{RenewalDays: &days30})
	if err != nil {
		t.Fatalf("preview 30 days: %v", err)
	}
	if preview365.RelationshipFingerprint == preview30.RelationshipFingerprint {
		t.Fatalf("renewal fingerprints = %q, want reviewed term bound", preview365.RelationshipFingerprint)
	}
	reviewed := make([]MappingRenewalReviewedMember, 0, len(preview365.Members))
	for _, member := range preview365.Members {
		reviewed = append(reviewed, MappingRenewalReviewedMember{UserID: member.UserID, TargetGroupID: member.ExpectedTargetGroupID, PlannedAction: member.PlannedAction})
	}
	_, err = service.ExecuteMappingRenewal(ctx, mapping.ID, MappingRenewalExecuteRequest{RenewalDays: days30, Members: reviewed, ExpectedRelationshipFingerprint: preview365.RelationshipFingerprint, OperationKey: "renewal-service-1"})
	var stale *StaleMappingRenewalError
	if !errors.As(err, &stale) || stale.RefreshedPreview == nil || stale.RefreshedPreview.RenewalDays != days30 || len(provider.writes) != 0 {
		t.Fatalf("term mismatch error/writes = %v/%+v, want stale 30-day preview before writes", err, provider.writes)
	}
	clock = time.Date(2030, time.January, 2, 0, 0, 0, 0, time.UTC)
	_, err = service.ExecuteMappingRenewal(ctx, mapping.ID, MappingRenewalExecuteRequest{RenewalDays: days365, Members: reviewed, ExpectedRelationshipFingerprint: preview365.RelationshipFingerprint, OperationKey: "renewal-natural-expiry"})
	if !errors.As(err, &stale) || stale.RefreshedPreview == nil || stale.RefreshedPreview.RelationshipFingerprint == preview365.RelationshipFingerprint || stale.RefreshedPreview.Members[0].Status != "expired" || stale.RefreshedPreview.Members[0].PlannedAction != "renew" || len(stale.RefreshedPreview.Members[0].Drift) != 1 || stale.RefreshedPreview.Members[0].Drift[0].Status != "expired" || len(provider.writes) != 0 {
		t.Fatalf("natural-expiry stale/writes = %v/%+v, want refreshed expired expected and drift facts before writes", err, provider.writes)
	}
	clock = time.Date(2029, time.December, 31, 0, 0, 0, 0, time.UTC)
	provider.failures[42] = errors.New("synthetic expired renewal failure")
	result, err := service.ExecuteMappingRenewal(ctx, mapping.ID, MappingRenewalExecuteRequest{RenewalDays: days365, Members: reviewed, ExpectedRelationshipFingerprint: preview365.RelationshipFingerprint, OperationKey: "renewal-service-1"})
	if err != nil {
		t.Fatalf("execute renewal: %v", err)
	}
	if len(result.Members) != 4 || result.Members[0].Status != "succeeded" || result.Members[0].Action != "extend" || result.Members[1].Status != "failed" || result.Members[1].Action != "renew" || result.Members[2].Status != "succeeded" || result.Members[2].Action != "create" || result.Members[3].Status != "skipped" || result.Members[3].Action != "skip" {
		t.Fatalf("member results = %+v, want active/expired/missing/suspended outcomes", result.Members)
	}
	writeKeys := make(map[int64]string, len(provider.writes))
	for _, write := range provider.writes {
		writeKeys[write.userID] = write.operationKey
	}
	if len(provider.writes) != 3 || writeKeys[41] == "" || writeKeys[42] == "" || writeKeys[43] == "" || writeKeys[41] == writeKeys[42] || writeKeys[42] == writeKeys[43] || writeKeys[41] == writeKeys[43] {
		t.Fatalf("renewal writes = %+v, want three distinct deterministic member keys", provider.writes)
	}
	failedKey := writeKeys[42]
	delete(provider.failures, int64(42))
	retry, err := service.ExecuteMappingRenewal(ctx, mapping.ID, MappingRenewalExecuteRequest{RenewalDays: days365, Members: []MappingRenewalReviewedMember{{UserID: result.Members[1].UserID, TargetGroupID: result.Members[1].TargetGroupID, PlannedAction: result.Members[1].Action}}, ExpectedRelationshipFingerprint: result.Preview.RelationshipFingerprint, OperationKey: "renewal-service-1", Retry: true})
	lastWrite := provider.writes[len(provider.writes)-1]
	if err != nil || len(retry.Members) != 1 || retry.Members[0].Status != "succeeded" || len(provider.writes) != 4 || lastWrite.userID != 42 || lastWrite.operationKey != failedKey {
		t.Fatalf("retry/result/writes = %v/%+v/%+v, want failed member with stable key", err, retry, provider.writes)
	}
}

type renewalServiceTestProvider struct {
	relay.Provider
	users         map[int64]*relay.User
	groups        []relay.Group
	subscriptions map[int64][]relay.UserSubscription
	failures      map[int64]error
	writes        []renewalServiceWrite
	mu            sync.Mutex
	writeStarted  chan int64
	writeRelease  chan struct{}
	writeDelay    time.Duration
}

type renewalServiceWrite struct {
	action       string
	userID       int64
	groupID      int64
	days         int
	operationKey string
}

func (p *renewalServiceTestProvider) GetUser(_ context.Context, userID int64) (*relay.User, error) {
	return p.users[userID], nil
}

func (p *renewalServiceTestProvider) ListPlatformGroups(context.Context) ([]relay.Group, error) {
	return append([]relay.Group(nil), p.groups...), nil
}

func (p *renewalServiceTestProvider) ListUserSubscriptions(_ context.Context, userID int64) ([]relay.UserSubscription, error) {
	return append([]relay.UserSubscription(nil), p.subscriptions[userID]...), nil
}

func (p *renewalServiceTestProvider) ListUserRelationships(context.Context) ([]relay.UserRelationship, error) {
	relationships := make([]relay.UserRelationship, 0, len(p.users))
	for userID, user := range p.users {
		relationships = append(relationships, relay.UserRelationship{User: *user, Subscriptions: append([]relay.UserSubscription(nil), p.subscriptions[userID]...)})
	}
	return relationships, nil
}

func (p *renewalServiceTestProvider) AssignSubscriptionForUserWithOperationKey(ctx context.Context, userID, groupID int64, days int, operationKey string) error {
	return p.write(ctx, "create", userID, groupID, days, operationKey)
}

func (p *renewalServiceTestProvider) ExtendSubscriptionForUserWithOperationKey(ctx context.Context, userID, groupID int64, days int, operationKey string) error {
	return p.write(ctx, "extend", userID, groupID, days, operationKey)
}

func (p *renewalServiceTestProvider) ExtendSubscriptionByIDWithOperationKey(ctx context.Context, subscriptionID int64, days int, operationKey string) error {
	p.mu.Lock()
	var userID, groupID int64
	for currentUserID, subscriptions := range p.subscriptions {
		for _, subscription := range subscriptions {
			if subscription.ID == subscriptionID {
				userID, groupID = currentUserID, subscription.GroupID
				break
			}
		}
	}
	p.mu.Unlock()
	if userID <= 0 || groupID <= 0 {
		return fmt.Errorf("reviewed subscription %d not found", subscriptionID)
	}
	return p.write(ctx, "extend", userID, groupID, days, operationKey)
}

func (p *renewalServiceTestProvider) write(ctx context.Context, action string, userID, groupID int64, days int, operationKey string) error {
	if p.writeStarted != nil && p.writeRelease != nil {
		p.writeStarted <- userID
		<-p.writeRelease
	}
	if p.writeDelay > 0 {
		timer := time.NewTimer(p.writeDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writes = append(p.writes, renewalServiceWrite{action: action, userID: userID, groupID: groupID, days: days, operationKey: operationKey})
	return p.failures[userID]
}

func TestMappingRenewalExecutionCompletesLargeRosterBeforeDeadlineAndKeepsResultOrder(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	assignments := make(map[string]int64)
	users := make(map[int64]*relay.User)
	subscriptions := make(map[int64][]relay.UserSubscription)
	reviewed := make([]MappingRenewalReviewedMember, 0, 24)
	for index := 1; index <= 24; index++ {
		relayUserID := int64(100 + index)
		local := client.User.Create().SetUsername(fmt.Sprintf("renewal-user-%02d", index)).SetEmail(fmt.Sprintf("renewal-%02d@example.com", index)).SetAuthSource("ldap").SetRelayUserID(int(relayUserID)).SaveX(ctx)
		assignments[strconv.Itoa(local.ID)] = 101
		users[relayUserID] = &relay.User{ID: relayUserID, Username: local.Username, Email: local.Email}
		subscriptions[relayUserID] = []relay.UserSubscription{{ID: int64(1000 + index), UserID: relayUserID, GroupID: 101, Status: "active", ExpiresAt: time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)}}
		reviewed = append(reviewed, MappingRenewalReviewedMember{UserID: local.ID, TargetGroupID: 101, PlannedAction: "extend"})
	}
	mapping := client.RelayGroupMapping.Create().SetProviderID(7).SetDepartmentExternalID("dept-renewal-concurrency").SetDepartmentName("Department Renewal").SetPlatform("openai").SetTemplateGroupID(10).SetGroupIds([]int64{101}).SetMemberAssignments(assignments).SaveX(ctx)
	provider := &renewalServiceTestProvider{
		users: users, groups: []relay.Group{{ID: 101, Name: "Group Target", Platform: "openai"}}, subscriptions: subscriptions, failures: make(map[int64]error), writeDelay: 100 * time.Millisecond,
	}
	service := NewService(client, relayPlanningProviderResolver(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	service.now = func() time.Time { return time.Date(2029, time.January, 1, 0, 0, 0, 0, time.UTC) }
	days := 365
	preview, err := service.PreviewMappingRenewal(ctx, mapping.ID, MappingRenewalPreviewRequest{RenewalDays: &days})
	if err != nil {
		t.Fatalf("preview renewal: %v", err)
	}
	started := make(chan int64, len(reviewed))
	release := make(chan struct{})
	provider.writeStarted = started
	provider.writeRelease = release
	done := make(chan struct{})
	var result *MappingRenewalExecution
	var executeErr error
	executeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	go func() {
		result, executeErr = service.ExecuteMappingRenewal(executeCtx, mapping.ID, MappingRenewalExecuteRequest{RenewalDays: days, Members: reviewed, ExpectedRelationshipFingerprint: preview.RelationshipFingerprint, OperationKey: "renewal-concurrency"})
		close(done)
	}()
	startedUsers := make(map[int64]bool)
	for len(startedUsers) < maxCandidateWorkers {
		select {
		case userID := <-started:
			startedUsers[userID] = true
		case <-time.After(time.Second):
			close(release)
			t.Fatalf("concurrent writes started for %d users, want %d before release", len(startedUsers), maxCandidateWorkers)
		}
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("renewal execution did not finish after releasing writes")
	}
	if executeErr != nil {
		t.Fatalf("execute renewal: %v", executeErr)
	}
	if executeCtx.Err() != nil {
		t.Fatalf("renewal execution exceeded request deadline: %v", executeCtx.Err())
	}
	if len(result.Members) != len(reviewed) {
		t.Fatalf("result count = %d, want %d", len(result.Members), len(reviewed))
	}
	for index, member := range result.Members {
		if member.UserID != reviewed[index].UserID || member.Status != "succeeded" {
			t.Fatalf("result[%d] = %+v, want ordered success for user %d", index, member, reviewed[index].UserID)
		}
	}
}

func TestProposedGroupNamesSkipOccupiedDepartmentSequence(t *testing.T) {
	groups := []relay.Group{
		{Name: "Group Alpha"},
		{Name: "Department Alpha-openai-01"},
		{Name: "Department Alpha-openai-03"},
	}
	got := proposedGroupNames("Department Alpha", "openai", groups, 3)
	want := []string{"Department Alpha-openai-02", "Department Alpha-openai-04", "Department Alpha-openai-05"}
	if len(got) != len(want) {
		t.Fatalf("proposed names = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("proposed names = %#v, want %#v", got, want)
		}
	}
}

func TestMergeTrendUsageFillsMissingRangeTokens(t *testing.T) {
	stats := map[int64]relay.TeamUserUsageStats{
		101: {UserID: 101},
		102: {UserID: 102, RangeTotalTokens: int64Pointer(9)},
	}
	firstTokens, secondTokens := int64(120), int64(80)
	mergeTrendUsage(stats, map[int64][]relay.UsageTrendPoint{
		101: {
			{Date: "2026-08-01", TotalTokens: &firstTokens, ActualCost: 1.25},
			{Date: "2026-08-02", TotalTokens: &secondTokens, ActualCost: 2.75},
		},
	})
	if stats[101].RangeTotalTokens == nil || *stats[101].RangeTotalTokens != 200 {
		t.Fatalf("range tokens = %#v, want 200", stats[101].RangeTotalTokens)
	}
	if stats[101].RangeActualCost == nil || *stats[101].RangeActualCost != 4 {
		t.Fatalf("range cost = %#v, want 4", stats[101].RangeActualCost)
	}
	if *stats[102].RangeTotalTokens != 9 {
		t.Fatalf("existing range tokens overwritten: %d", *stats[102].RangeTotalTokens)
	}
}

type usageStatsTestProvider struct {
	relay.Provider
	trend map[int64][]relay.UsageTrendPoint
}

func (p usageStatsTestProvider) GetBatchUserUsageStats(_ context.Context, ids []int64, _ relay.TeamUsageSummaryParams) (map[int64]relay.TeamUserUsageStats, error) {
	stats := make(map[int64]relay.TeamUserUsageStats, len(ids))
	for _, id := range ids {
		cost := 7.5
		stats[id] = relay.TeamUserUsageStats{UserID: id, RangeActualCost: &cost}
	}
	return stats, nil
}

func (p usageStatsTestProvider) GetUsageTrendForUsers(_ context.Context, _ []int64, _ relay.TeamMemberTrendParams) (map[int64][]relay.UsageTrendPoint, error) {
	return p.trend, nil
}

func TestUsageStatsUsesTrendTokensWhenBatchSummaryOmitsThem(t *testing.T) {
	first, second := int64(120), int64(80)
	provider := usageStatsTestProvider{trend: map[int64][]relay.UsageTrendPoint{
		101: {{TotalTokens: &first}, {TotalTokens: &second}},
	}}
	got, err := usageStats(context.Background(), provider, []int64{101})
	if err != nil {
		t.Fatalf("usageStats() error = %v", err)
	}
	if got[101].RangeTotalTokens == nil || *got[101].RangeTotalTokens != 200 {
		t.Fatalf("range tokens = %#v, want 200", got[101].RangeTotalTokens)
	}
}

type prewarmStatsReaderStub struct {
	stats   map[int64]relay.TeamUserUsageStats
	outcome teamusage.PrewarmReadOutcome
	err     error
	calls   int
}

func (r *prewarmStatsReaderStub) ReadAuthorizedStats(context.Context, teamusage.PrewarmReadRequest) (map[int64]relay.TeamUserUsageStats, teamusage.PrewarmReadOutcome, error) {
	r.calls++
	return r.stats, r.outcome, r.err
}

func TestLoadUsageStatsUsesPrewarmFullHitWithoutRelayFallback(t *testing.T) {
	tokens := int64(321)
	cost := 12.5
	reader := &prewarmStatsReaderStub{outcome: teamusage.PrewarmReadFullHit, stats: map[int64]relay.TeamUserUsageStats{
		101: {UserID: 101, RangeActualCost: &cost, RangeTotalTokens: &tokens},
	}}
	service := &Service{prewarmReader: reader}

	got, err := service.loadUsageStats(context.Background(), nil, 7, 11, []int64{101})
	if err != nil {
		t.Fatalf("loadUsageStats() error = %v", err)
	}
	if reader.calls != 1 || got[101].RangeTotalTokens == nil || *got[101].RangeTotalTokens != 321 {
		t.Fatalf("loadUsageStats() = %#v after %d cache calls, want prewarm values", got, reader.calls)
	}
}

func TestLoadUsageStatsFallsBackWhenPrewarmMisses(t *testing.T) {
	first, second := int64(120), int64(80)
	provider := usageStatsTestProvider{trend: map[int64][]relay.UsageTrendPoint{
		101: {{TotalTokens: &first}, {TotalTokens: &second}},
	}}
	reader := &prewarmStatsReaderStub{outcome: teamusage.PrewarmReadMiss}
	service := &Service{prewarmReader: reader}

	got, err := service.loadUsageStats(context.Background(), provider, 7, 11, []int64{101})
	if err != nil {
		t.Fatalf("loadUsageStats() fallback error = %v", err)
	}
	if reader.calls != 1 || got[101].RangeTotalTokens == nil || *got[101].RangeTotalTokens != 200 {
		t.Fatalf("loadUsageStats() fallback = %#v after %d cache calls, want exact trend values", got, reader.calls)
	}
}

type concurrentCandidateFactsProvider struct {
	relay.Provider
	started chan string
	release chan struct{}
}

func (p concurrentCandidateFactsProvider) ListUserSubscriptions(context.Context, int64) ([]relay.UserSubscription, error) {
	p.started <- "subscriptions"
	<-p.release
	return []relay.UserSubscription{
		{GroupID: 42, Status: "active"},
		{GroupID: 84, Status: "active"},
	}, nil
}

func (p concurrentCandidateFactsProvider) ListUserAPIKeys(context.Context, int64) ([]relay.APIKey, error) {
	p.started <- "api_keys"
	<-p.release
	return []relay.APIKey{{ID: 7, GroupID: 42, Status: "active"}, {ID: 8, GroupID: 42, Status: "inactive"}}, nil
}

func (p concurrentCandidateFactsProvider) ListAllowedGroupsForUser(context.Context, int64) ([]relay.Group, error) {
	return nil, errors.New("slow allowed-group fallback should not run")
}

func TestLoadCandidateRelayFactsUsesSubscriptionsAndRunsIndependentReadsConcurrently(t *testing.T) {
	provider := concurrentCandidateFactsProvider{
		started: make(chan string, 2),
		release: make(chan struct{}),
	}
	done := make(chan candidateRelayFacts, 1)
	go func() {
		done <- loadCandidateRelayFacts(context.Background(), provider, newPlanningRequestFacts(), nil, 101, relay.Group{ID: 42, Platform: "openai"}, "openai")
	}()

	started := map[string]bool{}
	for len(started) < 2 {
		select {
		case operation := <-provider.started:
			started[operation] = true
		case <-time.After(250 * time.Millisecond):
			close(provider.release)
			t.Fatalf("candidate Relay reads started sequentially: %#v", started)
		}
	}
	close(provider.release)
	facts := <-done
	if facts.groupErr != nil || facts.keyErr != nil {
		t.Fatalf("candidate facts errors = %v / %v", facts.groupErr, facts.keyErr)
	}
	if !facts.eligible || facts.migratableKeyCount != 1 {
		t.Fatalf("candidate facts = %#v, want eligible with one migratable key", facts)
	}
	if len(facts.relationshipAPIKeys) != 1 || facts.relationshipAPIKeys[0].ID != 7 {
		t.Fatalf("candidate API Key facts = %#v, want only active key 7", facts.relationshipAPIKeys)
	}
	if len(facts.relationshipObservedAPIKeys) != 2 || facts.relationshipObservedAPIKeys[0].ID != 7 || facts.relationshipObservedAPIKeys[0].Status != "active" || facts.relationshipObservedAPIKeys[1].ID != 8 || facts.relationshipObservedAPIKeys[1].Status != "inactive" {
		t.Fatalf("observed candidate API Key facts = %#v, want active key 7 and inactive key 8", facts.relationshipObservedAPIKeys)
	}
	if len(facts.currentGroupIDs) != 1 || facts.currentGroupIDs[0] != 84 {
		t.Fatalf("current group IDs = %#v, want [84]", facts.currentGroupIDs)
	}
}

type candidateFactsFallbackProvider struct{ relay.Provider }

func (candidateFactsFallbackProvider) ListUserSubscriptions(context.Context, int64) ([]relay.UserSubscription, error) {
	return nil, errors.New("synthetic subscription failure")
}

func (candidateFactsFallbackProvider) ListAllowedGroupsForUser(context.Context, int64) ([]relay.Group, error) {
	return []relay.Group{{ID: 42, Platform: "openai"}}, nil
}

func (candidateFactsFallbackProvider) ListUserAPIKeys(context.Context, int64) ([]relay.APIKey, error) {
	return nil, nil
}

func TestLoadCandidateRelayFactsFallsBackWhenSubscriptionReadFails(t *testing.T) {
	facts := loadCandidateRelayFacts(context.Background(), candidateFactsFallbackProvider{}, newPlanningRequestFacts(), nil, 101, relay.Group{ID: 42, Platform: "openai"}, "openai")
	if facts.groupErr != nil || !facts.eligible {
		t.Fatalf("candidate fallback facts = %#v, want eligible allowed-group result", facts)
	}
}

func TestClassifyManagedRosterCandidatesUsesOnlyManagedTargetEvidence(t *testing.T) {
	mapping := &ent.RelayGroupMapping{
		MemberAssignments: map[string]int64{"1": 101},
		OperationState: map[string]map[string]string{
			"member:1": {"target_group_id": "101", "api_keys": "501:succeeded"},
		},
	}
	tests := []struct {
		name       string
		candidate  Candidate
		wantReason replanRosterUnavailableReason
		wantSource bool
	}{
		{
			name: "aligned managed member",
			candidate: Candidate{UserID: 1, CurrentGroupIDs: []int64{101}, Warnings: []string{
				"user is not a member of the selected source group",
				"30-day usage is unknown; capacity may be underestimated",
			}, relationshipAPIKeys: []relationshipAPIKeyFact{{ID: 501, GroupID: 101}}, relationshipObservedAPIKeys: []relationshipAPIKeyFact{{ID: 501, GroupID: 101, Status: "active"}}},
		},
		{
			name:      "inactive reviewed API Key remains aligned",
			candidate: Candidate{UserID: 1, CurrentGroupIDs: []int64{101}, relationshipObservedAPIKeys: []relationshipAPIKeyFact{{ID: 501, GroupID: 101, Status: "inactive"}}},
		},
		{
			name:       "missing managed Target subscription",
			candidate:  Candidate{UserID: 1, Warnings: []string{"user is not a member of the selected source group"}, relationshipObservedAPIKeys: []relationshipAPIKeyFact{{ID: 501, GroupID: 101, Status: "active"}}},
			wantReason: replanRosterMissingTargetSubscription,
		},
		{
			name:       "reviewed API Key outside managed Target",
			candidate:  Candidate{UserID: 1, CurrentGroupIDs: []int64{101}, relationshipObservedAPIKeys: []relationshipAPIKeyFact{{ID: 501, GroupID: 20, Status: "active"}}},
			wantReason: replanRosterMismatchedTargetAPIKey,
		},
		{
			name:       "reviewed API Key with unknown status outside managed Target",
			candidate:  Candidate{UserID: 1, CurrentGroupIDs: []int64{101}, relationshipObservedAPIKeys: []relationshipAPIKeyFact{{ID: 501, GroupID: 20}}},
			wantReason: replanRosterMismatchedTargetAPIKey,
		},
		{
			name:       "reviewed API Key with unknown status on managed Target",
			candidate:  Candidate{UserID: 1, CurrentGroupIDs: []int64{101}, relationshipObservedAPIKeys: []relationshipAPIKeyFact{{ID: 501, GroupID: 101}}},
			wantReason: replanRosterMismatchedTargetAPIKey,
		},
		{
			name:       "reviewed API Key missing from current facts",
			candidate:  Candidate{UserID: 1, CurrentGroupIDs: []int64{101}},
			wantReason: replanRosterMismatchedTargetAPIKey,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates := []Candidate{tt.candidate}
			classifyManagedRosterCandidates(mapping, candidates)
			if candidates[0].replanUnavailableReason != tt.wantReason {
				t.Fatalf("reason = %v, want %v", candidates[0].replanUnavailableReason, tt.wantReason)
			}
			gotSource := slices.Contains(candidates[0].Warnings, "user is not a member of the selected source group")
			if gotSource != tt.wantSource {
				t.Fatalf("Source warning present = %v, want %v; warnings=%v", gotSource, tt.wantSource, candidates[0].Warnings)
			}
		})
	}

	noOwnedKeyEvidence := &ent.RelayGroupMapping{MemberAssignments: map[string]int64{"1": 101}}
	candidates := []Candidate{{UserID: 1, CurrentGroupIDs: []int64{101}, relationshipAPIKeys: []relationshipAPIKeyFact{{ID: 999, GroupID: 20}}}}
	classifyManagedRosterCandidates(noOwnedKeyEvidence, candidates)
	if candidates[0].replanUnavailableReason != 0 {
		t.Fatalf("unreviewed API Key produced managed drift reason %v", candidates[0].replanUnavailableReason)
	}

	unresolved := &ent.RelayGroupMapping{
		MemberAssignments: map[string]int64{"1": 101},
		OperationState: map[string]map[string]string{
			"member:1": {"target_group_id": "101", "subscription": "failed", "error": "synthetic retry"},
		},
	}
	candidates = []Candidate{{UserID: 1, Warnings: []string{"user is not a member of the selected source group"}}}
	classifyManagedRosterCandidates(unresolved, candidates)
	if candidates[0].replanUnavailableReason != 0 || slices.Contains(candidates[0].Warnings, "user is not a member of the selected source group") {
		t.Fatalf("unresolved legacy member = reason:%v warnings:%v, want retry classification deferred without Source warning", candidates[0].replanUnavailableReason, candidates[0].Warnings)
	}
}

func int64Pointer(value int64) *int64 {
	return &value
}

func TestExecuteReplanPersistsDurableOperationBeforeWriteAndBlocksConcurrentConfirm(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerRow, err := client.RelayProvider.Create().
		SetName("relay-planning-test").
		SetDisplayName("Relay Planning Test").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetRelayType("sub2api").
		Save(ctx)
	if err != nil {
		t.Fatalf("create relay provider: %v", err)
	}

	departmentID := "dept-alpha"
	source, run := createRelayPlanningDirectorySnapshot(t, ctx, client, departmentID)
	user := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRelayUserID(1001).
		SaveX(ctx)
	member := client.DirectoryMember.Create().
		SetSourceID(source.ID).
		SetExternalID("member-alice").
		SetEmailNormalized(user.Email).
		SetDisplayName(user.Username).
		SetDepartmentExternalID(departmentID).
		SetMatchedUserID(user.ID).
		SetLastSeenRunID(run.ID).
		SaveX(ctx)
	client.DirectoryMemberDepartment.Create().
		SetSourceID(source.ID).
		SetDirectoryMemberID(member.ID).
		SetMemberExternalID(member.ExternalID).
		SetMemberEmailNormalized(member.EmailNormalized).
		SetDepartmentExternalID(departmentID).
		SetLastSeenRunID(run.ID).
		SaveX(ctx)

	fake := &replanRetryProvider{
		groups: []relay.Group{
			{ID: 10, Name: "Template", Platform: "openai"},
			{ID: 20, Name: "Source", Platform: "openai"},
			{ID: 101, Name: "Target A", Platform: "openai"},
			{ID: 102, Name: "Target B", Platform: "openai"},
		},
		subscriptions: []relay.UserSubscription{
			{UserID: 1001, GroupID: 20, Status: "active"},
			{UserID: 1001, GroupID: 101, Status: "active"},
		},
		keys:         []relay.APIKey{{ID: 501, UserID: 1001, GroupID: 101, Status: "active"}},
		bindFailures: 1,
	}
	resolver := relayPlanningProviderResolver(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != providerRow.ID {
			return nil, fmt.Errorf("provider id = %d, want %d", providerID, providerRow.ID)
		}
		return fake, nil
	})
	service := NewService(client, resolver, nil)
	mappingRow := client.RelayGroupMapping.Create().
		SetProviderID(providerRow.ID).
		SetDepartmentExternalID(departmentID).
		SetDepartmentName("Department Alpha").
		SetPlatform("openai").
		SetTemplateGroupID(10).
		SetTemplateGroupName("Template").
		SetSourceGroupID(20).
		SetSourceGroupName("Source").
		SetGroupIds([]int64{101, 102}).
		SetMemberAssignments(map[string]int64{fmt.Sprint(user.ID): 101}).
		SetMemberSources(map[string]int64{fmt.Sprint(user.ID): 20}).
		SetStatus("active").
		SetWeeklyCostTarget(2500).
		SaveX(ctx)
	durableBeforeWrite := false
	fake.beforeWrite = func() {
		operation := client.RelationshipOperation.Query().OnlyX(ctx)
		owner := client.RelationshipOperationMapping.Query().Where(relationshipoperationmapping.OperationIDEQ(operation.ID), relationshipoperationmapping.ActiveEQ(true)).OnlyX(ctx)
		steps := client.RelationshipOperationStep.Query().Where(relationshipoperationstep.OperationIDEQ(operation.ID), relationshipoperationstep.LifecycleEQ(relationshipoperationstep.LifecycleDispatched)).CountX(ctx)
		attempt := client.RelationshipOperationAttempt.Query().Where(relationshipoperationattempt.OperationIDEQ(operation.ID)).OnlyX(ctx)
		current := client.RelayGroupMapping.GetX(ctx, mappingRow.ID)
		durableBeforeWrite = owner.MappingID == mappingRow.ID && steps > 0 && attempt.Status == relationshipoperationattempt.StatusRunning && current.BaselineRevision == 1 && current.MemberAssignments[fmt.Sprint(user.ID)] == 101
	}

	replanRequest := ExecuteRequest{
		PreviewRequest: PreviewRequest{Assignments: []Assignment{
			{Index: 0, UserIDs: []int{}},
			{Index: 1, UserIDs: []int{user.ID}},
		}},
		OperationKey: "replan-op-1",
	}
	preview, err := service.Replan(ctx, mappingRow.ID, nil, replanRequest.Assignments, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("first Replan() error = %v", err)
	}
	replanRequest.ExpectedRelationshipFingerprint = preview.RelationshipFingerprint
	first, err := service.ExecuteReplan(ctx, mappingRow.ID, replanRequest)
	if err != nil {
		t.Fatalf("first ExecuteReplan() error = %v", err)
	}
	if len(first.Members) != 1 || first.Members[0].Error == "" {
		t.Fatalf("first replan members = %#v, want API key failure", first.Members)
	}
	if len(first.Members[0].APIKeys) != 1 || !strings.Contains(first.Members[0].APIKeys[0], "failed") {
		t.Fatalf("first replan API keys = %#v, want failed move", first.Members[0].APIKeys)
	}
	if !durableBeforeWrite {
		t.Fatal("durable Operation/ownership/dispatched steps/running attempt were not visible before the first Relay write")
	}
	if fake.assignmentCalls != 1 {
		t.Fatalf("subscription assignments after first attempt = %d, want 1", fake.assignmentCalls)
	}
	if fmt.Sprint(fake.assignmentValidityDays) != "[365]" {
		t.Fatalf("subscription validity after first attempt = %v, want [365]", fake.assignmentValidityDays)
	}

	interrupted := client.RelationshipOperation.Query().OnlyX(ctx)
	if interrupted.Lifecycle != relationshipoperation.LifecycleInterrupted {
		t.Fatalf("Operation lifecycle = %q, want interrupted", interrupted.Lifecycle)
	}
	unchanged := client.RelayGroupMapping.GetX(ctx, mappingRow.ID)
	if unchanged.BaselineRevision != 1 || unchanged.MemberAssignments[fmt.Sprint(user.ID)] != 101 {
		t.Fatalf("partial operation changed baseline: revision=%d assignments=%v", unchanged.BaselineRevision, unchanged.MemberAssignments)
	}
	retryRequest := ExecuteRequest{
		PreviewRequest: PreviewRequest{Assignments: []Assignment{
			{Index: 0, UserIDs: []int{}},
			{Index: 1, UserIDs: []int{user.ID}},
		}},
		OperationKey: "replan-op-2",
	}
	preview, err = service.Replan(ctx, mappingRow.ID, nil, retryRequest.Assignments, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("retry Replan() error = %v", err)
	}
	retryRequest.ExpectedRelationshipFingerprint = preview.RelationshipFingerprint
	_, err = service.ExecuteReplan(ctx, mappingRow.ID, retryRequest)
	var active *ActiveRelationshipOperationError
	if !errors.As(err, &active) || active.MappingID != mappingRow.ID {
		t.Fatalf("concurrent Confirm error = %v, want active Relationship Operation conflict", err)
	}
	if fake.assignmentCalls != 1 {
		t.Fatalf("subscription assignments after blocked Confirm = %d, want 1", fake.assignmentCalls)
	}
	if fmt.Sprint(fake.assignmentValidityDays) != "[365]" {
		t.Fatalf("subscription validity after blocked Confirm = %v, want [365]", fake.assignmentValidityDays)
	}
}

func createRelayPlanningDirectorySnapshot(t *testing.T, ctx context.Context, client *ent.Client, departmentID string) (*ent.DirectorySource, *ent.DirectorySyncRun) {
	t.Helper()
	now := time.Now().UTC()
	source := client.DirectorySource.Create().
		SetName("relay-planning-source").
		SetDescription("Synthetic directory source").
		SetScope(directorysource.ScopeFullCompany).
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		SaveX(ctx)
	run := client.DirectorySyncRun.Create().
		SetSourceID(source.ID).
		SetMode(directorysyncrun.ModeApply).
		SetStatus(directorysyncrun.StatusCompleted).
		SetPhase(directorysyncrun.PhaseCompleted).
		SetCompletedAt(now).
		SaveX(ctx)
	client.DirectorySource.UpdateOneID(source.ID).
		SetLastRunID(run.ID).
		SetLastSuccessfulRunID(run.ID).
		SaveX(ctx)
	client.DirectoryDepartment.Create().
		SetSourceID(source.ID).
		SetExternalID(departmentID).
		SetName("Department Alpha").
		SetPath("synthetic/" + departmentID).
		SetLastSeenRunID(run.ID).
		SaveX(ctx)
	return source, run
}

type replanRetryProvider struct {
	relay.Provider
	mu                     sync.Mutex
	groups                 []relay.Group
	subscriptions          []relay.UserSubscription
	keys                   []relay.APIKey
	bindFailures           int
	bound                  []string
	assignmentCalls        int
	assignmentValidityDays []int
	beforeWrite            func()
	writeOnce              sync.Once
}

type relayPlanningProviderResolver func(context.Context, int) (relay.Provider, error)

func (f relayPlanningProviderResolver) Resolve(ctx context.Context, providerID int) (relay.Provider, error) {
	return f(ctx, providerID)
}

func (p *replanRetryProvider) ListPlatformGroups(context.Context) ([]relay.Group, error) {
	return append([]relay.Group(nil), p.groups...), nil
}

func (p *replanRetryProvider) GetUser(_ context.Context, userID int64) (*relay.User, error) {
	return &relay.User{ID: userID, Username: "alice", Email: "alice@example.com"}, nil
}

func (p *replanRetryProvider) ListUserSubscriptions(_ context.Context, userID int64) ([]relay.UserSubscription, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	items := make([]relay.UserSubscription, 0, len(p.subscriptions))
	for _, item := range p.subscriptions {
		if item.UserID == 0 || item.UserID == userID {
			items = append(items, item)
		}
	}
	return items, nil
}

func (p *replanRetryProvider) ListUserAPIKeys(context.Context, int64) ([]relay.APIKey, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]relay.APIKey(nil), p.keys...), nil
}

func (p *replanRetryProvider) ListAccountsForPlatform(context.Context, string) ([]relay.Account, error) {
	return nil, nil
}

func (p *replanRetryProvider) GetUsageStats(context.Context, int64, time.Time, time.Time) (*relay.UsageStats, error) {
	return &relay.UsageStats{TotalTokens: 100, TotalCost: 10}, nil
}

func (p *replanRetryProvider) AssignSubscriptionForUser(_ context.Context, _, _ int64, validityDays int) error {
	p.writeOnce.Do(func() {
		if p.beforeWrite != nil {
			p.beforeWrite()
		}
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	p.assignmentCalls++
	p.assignmentValidityDays = append(p.assignmentValidityDays, validityDays)
	return nil
}

func (p *replanRetryProvider) RemoveSubscriptionForUser(_ context.Context, userID, groupID int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	filtered := p.subscriptions[:0]
	for _, item := range p.subscriptions {
		if item.UserID == userID && item.GroupID == groupID {
			continue
		}
		filtered = append(filtered, item)
	}
	p.subscriptions = filtered
	return nil
}

func (p *replanRetryProvider) BindAPIKeyToGroup(_ context.Context, keyID, groupID int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bindFailures > 0 {
		p.bindFailures--
		return errors.New("synthetic key move failure")
	}
	for index := range p.keys {
		if p.keys[index].ID == keyID {
			p.keys[index].GroupID = groupID
		}
	}
	p.bound = append(p.bound, fmt.Sprintf("%d:%d", keyID, groupID))
	return nil
}
