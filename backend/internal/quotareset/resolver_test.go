package quotareset

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestResolveWorkflowMergesExactDepartments(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	alpha := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-alpha", "Group Alpha", nil)
	beta := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-beta", "Group Beta", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	configured := createQuotaResetUser(t, ctx, client, "configured", "configured@example.org", nil, "user")
	alphaRepresentative := createQuotaResetUser(t, ctx, client, "alpha-representative", "alpha-representative@example.org", nil, "user")
	betaRepresentative := createQuotaResetUser(t, ctx, client, "beta-representative", "beta-representative@example.org", nil, "user")

	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, alpha.ExternalID, &requester.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, alpha.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, beta.ExternalID)
	createQuotaResetMemberInDepartment(t, ctx, client, source.ID, "member-configured", configured, alpha.ExternalID)
	alphaRepresentativeMember := createQuotaResetMemberInDepartment(t, ctx, client, source.ID, "member-alpha-representative", alphaRepresentative, alpha.ExternalID)
	betaRepresentativeMember := createQuotaResetMemberInDepartment(t, ctx, client, source.ID, "member-beta-representative", betaRepresentative, beta.ExternalID)
	client.DirectoryDepartment.UpdateOneID(alpha.ID).SetMetadata(map[string]any{
		"representative_external_ids": []any{alphaRepresentativeMember.ExternalID},
	}).SaveX(ctx)
	client.DirectoryDepartment.UpdateOneID(beta.ID).SetMetadata(map[string]any{
		"representative_external_ids": []any{betaRepresentativeMember.ExternalID},
	}).SaveX(ctx)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, alpha.ExternalID, alpha.Name, configured.ID)

	workflow, paths, err := NewApproverResolver(client).ResolveWorkflow(ctx, requester)
	if err != nil {
		t.Fatalf("ResolveWorkflow() error = %v", err)
	}
	if len(workflow.Steps) != 1 {
		t.Fatalf("steps = %#v, want one exact-department step", workflow.Steps)
	}
	if got, want := workflowApproverIDs(workflow.Steps[0]), []int{configured.ID, betaRepresentative.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("exact approvers = %v, want %v", got, want)
	}
	if got, want := paths[0].Resolution, "matched"; got != want {
		t.Fatalf("alpha resolution = %q, want %q", got, want)
	}
	if got, want := paths[1].Resolution, "no_config_found"; got != want {
		t.Fatalf("beta resolution = %q, want %q", got, want)
	}
}

func TestResolveWorkflowPreservesExactDepartmentEvidencePaths(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	root := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-root", "Company", nil)
	parent := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-parent", "Parent", &root.ExternalID)
	configured := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-configured", "Configured", &parent.ExternalID)
	unconfigured := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-unconfigured", "Unconfigured", &parent.ExternalID)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	configuredApprover := createQuotaResetUser(t, ctx, client, "configured-approver", "configured-approver@example.org", nil, "user")
	representative := createQuotaResetUser(t, ctx, client, "representative", "representative@example.org", nil, "user")

	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, configured.ExternalID, &requester.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, configured.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, unconfigured.ExternalID)
	createQuotaResetMemberInDepartment(t, ctx, client, source.ID, "member-configured-approver", configuredApprover, configured.ExternalID)
	representativeMember := createQuotaResetMemberInDepartment(t, ctx, client, source.ID, "member-representative", representative, unconfigured.ExternalID)
	client.DirectoryDepartment.UpdateOneID(unconfigured.ID).SetMetadata(map[string]any{
		"representative_external_ids": []any{representativeMember.ExternalID},
	}).SaveX(ctx)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, configured.ExternalID, configured.Name, configuredApprover.ID)

	_, paths, err := NewApproverResolver(client).ResolveWorkflow(ctx, requester)
	if err != nil {
		t.Fatalf("ResolveWorkflow() error = %v", err)
	}
	gotByStart := make(map[string][]DepartmentPathNode, len(paths))
	for _, path := range paths {
		gotByStart[path.StartDepartmentExternalID] = path.Path
	}
	wantConfiguredPath := []DepartmentPathNode{
		{ExternalID: configured.ExternalID, DisplayPath: "Company / Parent / Configured"},
		{ExternalID: parent.ExternalID, DisplayPath: "Company / Parent"},
		{ExternalID: root.ExternalID, DisplayPath: "Company"},
	}
	if got := gotByStart[configured.ExternalID]; !reflect.DeepEqual(got, wantConfiguredPath) {
		t.Fatalf("configured exact path = %#v, want %#v", got, wantConfiguredPath)
	}
	wantUnconfiguredPath := []DepartmentPathNode{
		{ExternalID: unconfigured.ExternalID, DisplayPath: "Company / Parent / Unconfigured"},
		{ExternalID: parent.ExternalID, DisplayPath: "Company / Parent"},
		{ExternalID: root.ExternalID, DisplayPath: "Company"},
	}
	if got := gotByStart[unconfigured.ExternalID]; !reflect.DeepEqual(got, wantUnconfiguredPath) {
		t.Fatalf("unconfigured exact path = %#v, want %#v", got, wantUnconfiguredPath)
	}
}

func TestResolveWorkflowWalksMergedAncestorRounds(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	root := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-root", "Company", nil)
	alpha := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-alpha", "Group Alpha", &root.ExternalID)
	beta := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-beta", "Group Beta", &root.ExternalID)
	alphaTeam := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-alpha-team", "Alpha Team", &alpha.ExternalID)
	betaTeam := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-beta-team", "Beta Team", &beta.ExternalID)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	alphaApprover := createQuotaResetUser(t, ctx, client, "alpha-approver", "alpha-approver@example.org", nil, "user")
	betaApprover := createQuotaResetUser(t, ctx, client, "beta-approver", "beta-approver@example.org", nil, "user")
	rootApprover := createQuotaResetUser(t, ctx, client, "root-approver", "root-approver@example.org", nil, "user")

	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, alphaTeam.ExternalID, &requester.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, alphaTeam.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, betaTeam.ExternalID)
	createQuotaResetMemberInDepartment(t, ctx, client, source.ID, "member-alpha-approver", alphaApprover, alpha.ExternalID)
	createQuotaResetMemberInDepartment(t, ctx, client, source.ID, "member-beta-approver", betaApprover, beta.ExternalID)
	createQuotaResetMemberInDepartment(t, ctx, client, source.ID, "member-root-approver", rootApprover, root.ExternalID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, alpha.ExternalID, "Company / Group Alpha", alphaApprover.ID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, beta.ExternalID, "Company / Group Beta", betaApprover.ID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, root.ExternalID, "Company", rootApprover.ID)

	workflow, _, err := NewApproverResolver(client).ResolveWorkflow(ctx, requester)
	if err != nil {
		t.Fatalf("ResolveWorkflow() error = %v", err)
	}
	if len(workflow.Steps) != 2 {
		t.Fatalf("steps = %#v, want merged parent and root rounds", workflow.Steps)
	}
	if got, want := workflow.Steps[0].DepartmentExternalIDs, []string{alpha.ExternalID, beta.ExternalID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first round departments = %v, want %v", got, want)
	}
	if got, want := workflowApproverIDs(workflow.Steps[0]), []int{alphaApprover.ID, betaApprover.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first round approvers = %v, want %v", got, want)
	}
	if got, want := workflow.Steps[1].DepartmentExternalIDs, []string{root.ExternalID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("root round departments = %v, want %v", got, want)
	}
	if got, want := workflowApproverIDs(workflow.Steps[1]), []int{rootApprover.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("root approvers = %v, want %v", got, want)
	}
}

func TestResolveWorkflowSkipsUnconfiguredAncestors(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	root := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-root", "Company", nil)
	parent := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-parent", "Parent", &root.ExternalID)
	exact := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-exact", "Exact", &parent.ExternalID)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	exactApprover := createQuotaResetUser(t, ctx, client, "exact-approver", "exact-approver@example.org", nil, "user")
	parentRepresentative := createQuotaResetUser(t, ctx, client, "parent-representative", "parent-representative@example.org", nil, "user")
	rootApprover := createQuotaResetUser(t, ctx, client, "root-approver", "root-approver@example.org", nil, "user")

	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, exact.ExternalID, &requester.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, exact.ExternalID)
	createQuotaResetMemberInDepartment(t, ctx, client, source.ID, "member-exact-approver", exactApprover, exact.ExternalID)
	parentRepresentativeMember := createQuotaResetMemberInDepartment(t, ctx, client, source.ID, "member-parent-representative", parentRepresentative, parent.ExternalID)
	createQuotaResetMemberInDepartment(t, ctx, client, source.ID, "member-root-approver", rootApprover, root.ExternalID)
	client.DirectoryDepartment.UpdateOneID(parent.ID).SetMetadata(map[string]any{
		"representative_external_ids": []any{parentRepresentativeMember.ExternalID},
	}).SaveX(ctx)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, exact.ExternalID, exact.Name, exactApprover.ID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, root.ExternalID, root.Name, rootApprover.ID)

	workflow, _, err := NewApproverResolver(client).ResolveWorkflow(ctx, requester)
	if err != nil {
		t.Fatalf("ResolveWorkflow() error = %v", err)
	}
	if len(workflow.Steps) != 2 {
		t.Fatalf("steps = %#v, want exact and configured root only", workflow.Steps)
	}
	if got, want := workflowApproverIDs(workflow.Steps[1]), []int{rootApprover.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("root approvers = %v, want %v", got, want)
	}
	if got := workflowApproverIDs(workflow.Steps[1]); reflect.DeepEqual(got, []int{parentRepresentative.ID}) {
		t.Fatalf("parent representative incorrectly retained in ancestor round: %v", got)
	}
}

func TestResolveWorkflowUsesAdminFallbacks(t *testing.T) {
	t.Run("unusable exact config retains first fallback", func(t *testing.T) {
		ctx := context.Background()
		client := testdb.Open(t)
		source := createQuotaResetDirectorySource(t, ctx, client)
		exact := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-exact", "Exact", nil)
		requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
		unusable := createQuotaResetUser(t, ctx, client, "unusable", "unusable@example.org", nil, "user")
		requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, exact.ExternalID, &requester.ID)
		createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, exact.ExternalID)
		createQuotaResetApproverConfig(t, ctx, client, source.ID, exact.ExternalID, exact.Name, unusable.ID)

		workflow, paths, err := NewApproverResolver(client).ResolveWorkflow(ctx, requester)
		if err != nil {
			t.Fatalf("ResolveWorkflow() error = %v", err)
		}
		if len(workflow.Steps) != 1 || !workflow.Steps[0].AdminFallback || len(workflow.Steps[0].Approvers) != 0 {
			t.Fatalf("steps = %#v, want exact admin fallback", workflow.Steps)
		}
		if got, want := paths[0].Resolution, "matched"; got != want {
			t.Fatalf("exact resolution = %q, want %q", got, want)
		}
	})

	t.Run("unusable ancestor config retains later fallback", func(t *testing.T) {
		ctx := context.Background()
		client := testdb.Open(t)
		source := createQuotaResetDirectorySource(t, ctx, client)
		parent := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-parent", "Parent", nil)
		exact := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-exact", "Exact", &parent.ExternalID)
		requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
		exactApprover := createQuotaResetUser(t, ctx, client, "exact-approver", "exact-approver@example.org", nil, "user")
		unusable := createQuotaResetUser(t, ctx, client, "unusable", "unusable@example.org", nil, "user")
		requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, exact.ExternalID, &requester.ID)
		createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, exact.ExternalID)
		createQuotaResetMemberInDepartment(t, ctx, client, source.ID, "member-exact-approver", exactApprover, exact.ExternalID)
		createQuotaResetApproverConfig(t, ctx, client, source.ID, exact.ExternalID, exact.Name, exactApprover.ID)
		createQuotaResetApproverConfig(t, ctx, client, source.ID, parent.ExternalID, parent.Name, unusable.ID)

		workflow, _, err := NewApproverResolver(client).ResolveWorkflow(ctx, requester)
		if err != nil {
			t.Fatalf("ResolveWorkflow() error = %v", err)
		}
		if len(workflow.Steps) != 2 || !workflow.Steps[1].AdminFallback || len(workflow.Steps[1].Approvers) != 0 {
			t.Fatalf("steps = %#v, want ancestor admin fallback", workflow.Steps)
		}
	})

	t.Run("no exact config or representative skips first step", func(t *testing.T) {
		ctx := context.Background()
		client := testdb.Open(t)
		source := createQuotaResetDirectorySource(t, ctx, client)
		parent := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-parent", "Parent", nil)
		exact := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-exact", "Exact", &parent.ExternalID)
		requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
		parentApprover := createQuotaResetUser(t, ctx, client, "parent-approver", "parent-approver@example.org", nil, "user")
		requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, exact.ExternalID, &requester.ID)
		createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, exact.ExternalID)
		createQuotaResetMemberInDepartment(t, ctx, client, source.ID, "member-parent-approver", parentApprover, parent.ExternalID)
		createQuotaResetApproverConfig(t, ctx, client, source.ID, parent.ExternalID, parent.Name, parentApprover.ID)

		workflow, paths, err := NewApproverResolver(client).ResolveWorkflow(ctx, requester)
		if err != nil {
			t.Fatalf("ResolveWorkflow() error = %v", err)
		}
		if len(workflow.Steps) != 1 || workflow.Steps[0].DepartmentExternalIDs[0] != parent.ExternalID {
			t.Fatalf("steps = %#v, want parent as first retained step", workflow.Steps)
		}
		if got, want := paths[0].Resolution, "no_config_found"; got != want {
			t.Fatalf("exact resolution = %q, want %q", got, want)
		}
	})

	t.Run("no resolved steps creates final fallback", func(t *testing.T) {
		ctx := context.Background()
		client := testdb.Open(t)
		requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")

		workflow, _, err := NewApproverResolver(client).ResolveWorkflow(ctx, requester)
		if err != nil {
			t.Fatalf("ResolveWorkflow() error = %v", err)
		}
		if len(workflow.Steps) != 1 || !workflow.Steps[0].AdminFallback || workflow.Steps[0].Status != WorkflowStepActive {
			t.Fatalf("steps = %#v, want final active admin fallback", workflow.Steps)
		}
	})
}

func TestResolveWorkflowRejectsStaleConfiguredMembership(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	configuredDepartment := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-configured", "Configured", nil)
	staleDepartment := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-stale", "Stale", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	staleConfigured := createQuotaResetUser(t, ctx, client, "stale-configured", "stale-configured@example.org", nil, "user")
	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, configuredDepartment.ExternalID, &requester.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, configuredDepartment.ExternalID)
	createQuotaResetMemberInDepartment(t, ctx, client, source.ID, "member-stale-configured", staleConfigured, staleDepartment.ExternalID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, configuredDepartment.ExternalID, configuredDepartment.Name, staleConfigured.ID)

	workflow, _, err := NewApproverResolver(client).ResolveWorkflow(ctx, requester)
	if err != nil {
		t.Fatalf("ResolveWorkflow() error = %v", err)
	}
	if len(workflow.Steps) != 1 || !workflow.Steps[0].AdminFallback || len(workflow.Steps[0].Approvers) != 0 {
		t.Fatalf("steps = %#v, want configured-department fallback without stale user", workflow.Steps)
	}
}

func createQuotaResetDirectorySource(t *testing.T, ctx context.Context, client *ent.Client) *ent.DirectorySource {
	t.Helper()
	source, err := client.DirectorySource.Create().
		SetName("Example Directory").
		SetDescription("Synthetic organization directory").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		Save(ctx)
	if err != nil {
		t.Fatalf("create directory source: %v", err)
	}
	run, err := client.DirectorySyncRun.Create().
		SetSourceID(source.ID).
		SetMode("apply").
		SetStatus("completed").
		SetPhase("completed").
		SetDepartmentCount(3).
		SetMemberCount(1).
		SetCompletedAt(time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)).
		Save(ctx)
	if err != nil {
		t.Fatalf("create directory sync run: %v", err)
	}
	source, err = client.DirectorySource.UpdateOneID(source.ID).
		SetLastRunID(run.ID).
		SetLastSuccessfulRunID(run.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("update directory source: %v", err)
	}
	return source
}

func createQuotaResetDepartment(t *testing.T, ctx context.Context, client *ent.Client, sourceID int, externalID, name string, parent *string) *ent.DirectoryDepartment {
	t.Helper()
	create := client.DirectoryDepartment.Create().
		SetSourceID(sourceID).
		SetExternalID(externalID).
		SetName(name).
		SetPath(name).
		SetLastSeenRunID(1)
	if parent != nil {
		create.SetParentExternalID(*parent)
	}
	department, err := create.Save(ctx)
	if err != nil {
		t.Fatalf("create department %s: %v", externalID, err)
	}
	return department
}

func createQuotaResetUser(t *testing.T, ctx context.Context, client *ent.Client, username, email string, relayUserID *int, role string) *ent.User {
	t.Helper()
	create := client.User.Create().
		SetUsername(username).
		SetEmail(strings.ToLower(strings.TrimSpace(email))).
		SetAuthSource(entuser.AuthSourceLdap)
	if role == "admin" {
		create.SetRole(entuser.RoleAdmin)
	} else {
		create.SetRole(entuser.RoleUser)
	}
	if relayUserID != nil {
		create.SetRelayUserID(*relayUserID)
	}
	user, err := create.Save(ctx)
	if err != nil {
		t.Fatalf("create user %s: %v", username, err)
	}
	return user
}

func createQuotaResetMember(t *testing.T, ctx context.Context, client *ent.Client, sourceID int, externalID, email, departmentID string, matchedUserID *int) *ent.DirectoryMember {
	t.Helper()
	create := client.DirectoryMember.Create().
		SetSourceID(sourceID).
		SetExternalID(externalID).
		SetEmailNormalized(strings.ToLower(strings.TrimSpace(email))).
		SetDisplayName(externalID).
		SetDepartmentExternalID(departmentID).
		SetLastSeenRunID(1)
	if matchedUserID != nil {
		create.SetMatchedUserID(*matchedUserID)
	}
	member, err := create.Save(ctx)
	if err != nil {
		t.Fatalf("create member %s: %v", externalID, err)
	}
	return member
}

func createQuotaResetMemberInDepartment(t *testing.T, ctx context.Context, client *ent.Client, sourceID int, externalID string, user *ent.User, departmentID string) *ent.DirectoryMember {
	t.Helper()
	member := createQuotaResetMember(t, ctx, client, sourceID, externalID, user.Email, departmentID, &user.ID)
	createQuotaResetMemberDepartment(t, ctx, client, sourceID, member, departmentID)
	return member
}

func createQuotaResetMemberDepartment(t *testing.T, ctx context.Context, client *ent.Client, sourceID int, member *ent.DirectoryMember, departmentID string) {
	t.Helper()
	if _, err := client.DirectoryMemberDepartment.Create().
		SetSourceID(sourceID).
		SetDirectoryMemberID(member.ID).
		SetMemberExternalID(member.ExternalID).
		SetMemberEmailNormalized(member.EmailNormalized).
		SetDepartmentExternalID(departmentID).
		SetLastSeenRunID(member.LastSeenRunID).
		Save(ctx); err != nil {
		t.Fatalf("create member department %s/%s: %v", member.EmailNormalized, departmentID, err)
	}
}

func createQuotaResetApproverConfig(t *testing.T, ctx context.Context, client *ent.Client, sourceID int, departmentID, displayPath string, approverUserID int) {
	t.Helper()
	if _, err := client.QuotaResetApproverConfig.Create().
		SetDirectorySourceID(sourceID).
		SetDepartmentExternalID(departmentID).
		SetDepartmentDisplayPath(displayPath).
		SetApproverUserID(approverUserID).
		SetEnabled(true).
		SetCreatedByUserID(1).
		SetUpdatedByUserID(1).
		Save(ctx); err != nil {
		t.Fatalf("create approver config %s/%d: %v", departmentID, approverUserID, err)
	}
}

func workflowApproverIDs(step WorkflowStep) []int {
	ids := make([]int, 0, len(step.Approvers))
	for _, approver := range step.Approvers {
		ids = append(ids, approver.UserID)
	}
	sort.Ints(ids)
	return ids
}

func intPtr(v int) *int {
	return &v
}
