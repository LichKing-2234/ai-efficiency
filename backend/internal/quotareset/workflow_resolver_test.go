package quotareset

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestWorkflowResolverUsesExactConfigWithoutWalkingParent(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	parent := createQuotaResetDepartment(t, ctx, client, source.ID, "department-parent", "Parent", nil)
	child := createQuotaResetDepartment(t, ctx, client, source.ID, "department-child", "Child", &parent.ExternalID)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	parentApprover := createQuotaResetUser(t, ctx, client, "parent-lead", "parent-lead@example.com", nil, "user")
	childRepresentative := createQuotaResetUser(t, ctx, client, "child-lead", "child-lead@example.com", nil, "user")
	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, child.ExternalID, &requester.ID)
	childRepresentativeMember := createQuotaResetMember(t, ctx, client, source.ID, "member-child-lead", childRepresentative.Email, child.ExternalID, &childRepresentative.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, child.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, childRepresentativeMember, child.ExternalID)
	client.DirectoryDepartment.UpdateOneID(child.ID).SetMetadata(map[string]any{"representative_external_ids": []any{childRepresentativeMember.ExternalID}}).SaveX(ctx)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, parent.ExternalID, parent.Name, parentApprover.ID)

	snapshot := resolveWorkflowSnapshot(t, ctx, client, requester.ID, 1, "group-alpha")
	assertResolvedNodeApproverIDs(t, snapshot.Nodes[0], childRepresentative.ID)
	if got := snapshot.Nodes[0].Departments[0].Resolution; got != "directory_representative" {
		t.Fatalf("resolution = %q, want directory_representative", got)
	}
}

func TestWorkflowResolverFallsBackToRepresentativeOfSameDepartment(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	department := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Alpha", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	representative := createQuotaResetUser(t, ctx, client, "lead", "lead@example.com", nil, "user")
	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, department.ExternalID, &requester.ID)
	representativeMember := createQuotaResetMember(t, ctx, client, source.ID, "member-lead", representative.Email, department.ExternalID, &representative.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, department.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, representativeMember, department.ExternalID)
	client.DirectoryDepartment.UpdateOneID(department.ID).SetMetadata(map[string]any{"representative_external_ids": []string{representativeMember.ExternalID}}).SaveX(ctx)

	snapshot := resolveWorkflowSnapshot(t, ctx, client, requester.ID, 1, "group-alpha")
	assertResolvedNodeApproverIDs(t, snapshot.Nodes[0], representative.ID)
	if got := snapshot.Nodes[0].Approvers[0].Source; got != "directory_representative" {
		t.Fatalf("source = %q, want directory_representative", got)
	}
}

func TestWorkflowResolverAcceptsCommaSeparatedDepartmentRepresentativeMetadata(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	department := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Alpha", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	representative := createQuotaResetUser(t, ctx, client, "lead", "lead@example.com", nil, "user")
	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, department.ExternalID, &requester.ID)
	representativeMember := createQuotaResetMember(t, ctx, client, source.ID, "member-lead", representative.Email, department.ExternalID, &representative.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, department.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, representativeMember, department.ExternalID)
	client.DirectoryDepartment.UpdateOneID(department.ID).
		SetMetadata(map[string]any{"representative_external_ids": " missing-member, member-lead , member-lead "}).
		SaveX(ctx)

	snapshot := resolveWorkflowSnapshot(t, ctx, client, requester.ID, 1, "group-alpha")
	assertResolvedNodeApproverIDs(t, snapshot.Nodes[0], representative.ID)
}

func TestWorkflowResolverAcceptsNumericMemberLeaderDepartmentMetadata(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	department := createQuotaResetDepartment(t, ctx, client, source.ID, "1684078", "Numeric", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	representative := createQuotaResetUser(t, ctx, client, "lead", "lead@example.com", nil, "user")
	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, department.ExternalID, &requester.ID)
	representativeMember := createQuotaResetMember(t, ctx, client, source.ID, "member-lead", representative.Email, department.ExternalID, &representative.ID)
	representativeMember = client.DirectoryMember.UpdateOneID(representativeMember.ID).
		SetMetadata(map[string]any{"leader_department_ids": []any{float64(1684078)}}).
		SaveX(ctx)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, department.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, representativeMember, department.ExternalID)

	snapshot := resolveWorkflowSnapshot(t, ctx, client, requester.ID, 1, "group-alpha")
	assertResolvedNodeApproverIDs(t, snapshot.Nodes[0], representative.ID)
}

func TestWorkflowResolverFallsBackToMemberDeclaredRepresentative(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	department := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Alpha", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	representative := createQuotaResetUser(t, ctx, client, "lead", "lead@example.com", nil, "user")
	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, department.ExternalID, &requester.ID)
	representativeMember := createQuotaResetMember(t, ctx, client, source.ID, "member-lead", representative.Email, department.ExternalID, &representative.ID)
	representativeMember = client.DirectoryMember.UpdateOneID(representativeMember.ID).
		SetMetadata(map[string]any{"leader_department_ids": []any{" department-alpha ", "department-alpha"}}).
		SaveX(ctx)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, department.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, representativeMember, department.ExternalID)

	snapshot := resolveWorkflowSnapshot(t, ctx, client, requester.ID, 1, "group-alpha")
	assertResolvedNodeApproverIDs(t, snapshot.Nodes[0], representative.ID)
	if got := snapshot.Nodes[0].Approvers[0].Source; got != "directory_representative" {
		t.Fatalf("source = %q, want directory_representative", got)
	}
}

func TestWorkflowResolverDoesNotEmailFallbackWhenRepresentativeMatchedUserIsUnavailable(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	department := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Alpha", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	representative := createQuotaResetUser(t, ctx, client, "lead", "lead@example.com", nil, "user")
	staleUser := createQuotaResetUser(t, ctx, client, "stale", "stale@example.com", nil, "user")
	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, department.ExternalID, &requester.ID)
	representativeMember := createQuotaResetMember(t, ctx, client, source.ID, "member-lead", representative.Email, department.ExternalID, &staleUser.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, department.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, representativeMember, department.ExternalID)
	client.DirectoryDepartment.UpdateOneID(department.ID).SetMetadata(map[string]any{"representative_external_ids": representativeMember.ExternalID}).SaveX(ctx)
	client.User.DeleteOneID(staleUser.ID).ExecX(ctx)

	snapshot := resolveWorkflowSnapshot(t, ctx, client, requester.ID, 1, "group-alpha")
	if got := len(snapshot.Nodes[0].Approvers); got != 0 {
		t.Fatalf("approvers = %#v, want none", snapshot.Nodes[0].Approvers)
	}
	if got := snapshot.Nodes[0].InitialStatus; got != "skipped_no_approver" {
		t.Fatalf("initial status = %q, want skipped_no_approver", got)
	}
}

func TestWorkflowResolverMergesConfiguredAndRepresentativeDepartments(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	alpha := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Alpha", nil)
	beta := createQuotaResetDepartment(t, ctx, client, source.ID, "department-beta", "Beta", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	configured := createQuotaResetUser(t, ctx, client, "alpha-lead", "alpha-lead@example.com", nil, "user")
	representative := createQuotaResetUser(t, ctx, client, "beta-lead", "beta-lead@example.com", nil, "user")
	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, alpha.ExternalID, &requester.ID)
	configuredMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alpha-lead", configured.Email, alpha.ExternalID, &configured.ID)
	representativeMember := createQuotaResetMember(t, ctx, client, source.ID, "member-beta-lead", representative.Email, beta.ExternalID, &representative.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, alpha.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, beta.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, configuredMember, alpha.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, representativeMember, beta.ExternalID)
	client.DirectoryDepartment.UpdateOneID(beta.ID).SetMetadata(map[string]any{"representative_external_ids": []any{representativeMember.ExternalID}}).SaveX(ctx)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, alpha.ExternalID, alpha.Name, configured.ID)

	snapshot := resolveWorkflowSnapshot(t, ctx, client, requester.ID, 1, "group-alpha")
	assertResolvedNodeApproverIDs(t, snapshot.Nodes[0], configured.ID, representative.ID)
	if got, want := snapshot.Nodes[0].Departments[0].Resolution, "configured"; got != want {
		t.Fatalf("first resolution = %q, want %q", got, want)
	}
	if got, want := snapshot.Nodes[0].Departments[1].Resolution, "directory_representative"; got != want {
		t.Fatalf("second resolution = %q, want %q", got, want)
	}
}

func TestWorkflowResolverUsesMembershipRowsBeforePrimaryCompatibilityFallback(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	primary := createQuotaResetDepartment(t, ctx, client, source.ID, "department-primary", "Primary", nil)
	direct := createQuotaResetDepartment(t, ctx, client, source.ID, "department-direct", "Direct", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	primaryApprover := createQuotaResetUser(t, ctx, client, "primary-lead", "primary-lead@example.com", nil, "user")
	directApprover := createQuotaResetUser(t, ctx, client, "direct-lead", "direct-lead@example.com", nil, "user")
	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, primary.ExternalID, &requester.ID)
	directApproverMember := createQuotaResetMember(t, ctx, client, source.ID, "member-direct-lead", directApprover.Email, direct.ExternalID, &directApprover.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, direct.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, directApproverMember, direct.ExternalID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, primary.ExternalID, primary.Name, primaryApprover.ID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, direct.ExternalID, direct.Name, directApprover.ID)

	snapshot := resolveWorkflowSnapshot(t, ctx, client, requester.ID, 1, "group-alpha")
	assertResolvedNodeApproverIDs(t, snapshot.Nodes[0], directApprover.ID)
	if got := snapshot.Requester.DepartmentPaths; len(got) != 1 || got[0] != direct.Name {
		t.Fatalf("requester department paths = %#v, want direct membership only", got)
	}
}

func TestWorkflowResolverSkipsEmptyInitialNode(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	department := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Alpha", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	member := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, department.ExternalID, &requester.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, member, department.ExternalID)

	snapshot := resolveWorkflowSnapshot(t, ctx, client, requester.ID, 1, "group-alpha")
	if got, want := snapshot.Nodes[0].InitialStatus, "skipped_no_approver"; got != want {
		t.Fatalf("initial status = %q, want %q", got, want)
	}
	if len(snapshot.Nodes[0].Approvers) != 0 {
		t.Fatalf("initial approvers = %#v, want none", snapshot.Nodes[0].Approvers)
	}
}

func TestWorkflowResolverConfiguredChainNeverFallsBackToRepresentative(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	requesterDepartment := createQuotaResetDepartment(t, ctx, client, source.ID, "department-requester", "Requester", nil)
	chainDepartment := createQuotaResetDepartment(t, ctx, client, source.ID, "department-chain", "Chain", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	representative := createQuotaResetUser(t, ctx, client, "chain-lead", "chain-lead@example.com", nil, "user")
	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, requesterDepartment.ExternalID, &requester.ID)
	representativeMember := createQuotaResetMember(t, ctx, client, source.ID, "member-chain-lead", representative.Email, chainDepartment.ExternalID, &representative.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, requesterDepartment.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, representativeMember, chainDepartment.ExternalID)
	client.DirectoryDepartment.UpdateOneID(chainDepartment.ID).SetMetadata(map[string]any{"representative_external_ids": []any{representativeMember.ExternalID}}).SaveX(ctx)
	createWorkflowChain(t, ctx, client, 7, "group-alpha", source.ID, chainDepartment)

	snapshot := resolveWorkflowSnapshot(t, ctx, client, requester.ID, 7, " group-alpha ")
	if len(snapshot.Nodes) != 2 {
		t.Fatalf("node count = %d, want initial plus configured node", len(snapshot.Nodes))
	}
	later := snapshot.Nodes[1]
	if len(later.Approvers) != 0 || !later.AdminFallbackRequired || later.InitialStatus != "queued" {
		t.Fatalf("configured node = %#v, want queued empty admin fallback", later)
	}
}

func TestWorkflowResolverStaleChainSourceCannotBindCurrentDepartment(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	staleSource := createQuotaResetDirectorySource(t, ctx, client)
	source := createQuotaResetDirectorySource(t, ctx, client)
	initialDepartment := createQuotaResetDepartment(t, ctx, client, source.ID, "department-initial", "Initial", nil)
	chainDepartment := createQuotaResetDepartment(t, ctx, client, source.ID, "department-chain", "Current Chain", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "lead", "lead@example.com", nil, "user")
	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, initialDepartment.ExternalID, &requester.ID)
	approverMember := createQuotaResetMember(t, ctx, client, source.ID, "member-lead", approver.Email, initialDepartment.ExternalID, &approver.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, initialDepartment.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, approverMember, initialDepartment.ExternalID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, chainDepartment.ExternalID, chainDepartment.Name, approver.ID)
	createWorkflowChain(t, ctx, client, 7, "group-alpha", staleSource.ID, chainDepartment)

	snapshot := resolveWorkflowSnapshot(t, ctx, client, requester.ID, 7, "group-alpha")
	later := snapshot.Nodes[1]
	if len(later.Approvers) != 0 || !later.AdminFallbackRequired || later.Label != chainDepartment.Name {
		t.Fatalf("configured node = %#v, want preserved stale snapshot with admin fallback", later)
	}
}

func TestWorkflowResolverRemovedCurrentChainDepartmentRequiresAdminFallback(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	initialDepartment := createQuotaResetDepartment(t, ctx, client, source.ID, "department-initial", "Initial", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "lead", "lead@example.com", nil, "user")
	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, initialDepartment.ExternalID, &requester.ID)
	approverMember := createQuotaResetMember(t, ctx, client, source.ID, "member-lead", approver.Email, initialDepartment.ExternalID, &approver.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, initialDepartment.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, approverMember, initialDepartment.ExternalID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, "department-removed", "Removed", approver.ID)
	createWorkflowChain(t, ctx, client, 7, "group-alpha", source.ID, &ent.DirectoryDepartment{ExternalID: "department-removed", Name: "Removed"})

	snapshot := resolveWorkflowSnapshot(t, ctx, client, requester.ID, 7, "group-alpha")
	later := snapshot.Nodes[1]
	if len(later.Approvers) != 0 || !later.AdminFallbackRequired || later.Label != "Removed" {
		t.Fatalf("configured node = %#v, want preserved removed snapshot with admin fallback", later)
	}
}

func TestWorkflowResolverExcludesConfiguredUserMissingCurrentDirectoryMember(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	initialDepartment := createQuotaResetDepartment(t, ctx, client, source.ID, "department-initial", "Initial", nil)
	chainDepartment := createQuotaResetDepartment(t, ctx, client, source.ID, "department-chain", "Chain", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	localOnlyApprover := createQuotaResetUser(t, ctx, client, "local-only", "local-only@example.com", nil, "user")
	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, initialDepartment.ExternalID, &requester.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, initialDepartment.ExternalID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, initialDepartment.ExternalID, initialDepartment.Name, localOnlyApprover.ID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, chainDepartment.ExternalID, chainDepartment.Name, localOnlyApprover.ID)
	createWorkflowChain(t, ctx, client, 7, "group-alpha", source.ID, chainDepartment)

	snapshot := resolveWorkflowSnapshot(t, ctx, client, requester.ID, 7, "group-alpha")
	if len(snapshot.Nodes) != 2 {
		t.Fatalf("node count = %d, want initial plus configured node", len(snapshot.Nodes))
	}
	if initial := snapshot.Nodes[0]; initial.InitialStatus != "skipped_no_approver" || len(initial.Approvers) != 0 {
		t.Fatalf("initial node = %#v, want skipped without local-only user", initial)
	}
	if later := snapshot.Nodes[1]; len(later.Approvers) != 0 || !later.AdminFallbackRequired {
		t.Fatalf("configured node = %#v, want empty admin fallback", later)
	}
}

func TestWorkflowResolverExcludesRequesterFromEveryNode(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	initialDepartment := createQuotaResetDepartment(t, ctx, client, source.ID, "department-initial", "Initial", nil)
	chainDepartment := createQuotaResetDepartment(t, ctx, client, source.ID, "department-chain", "Chain", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	member := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, initialDepartment.ExternalID, &requester.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, member, initialDepartment.ExternalID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, initialDepartment.ExternalID, initialDepartment.Name, requester.ID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, chainDepartment.ExternalID, chainDepartment.Name, requester.ID)
	createWorkflowChain(t, ctx, client, 7, "group-alpha", source.ID, chainDepartment)

	snapshot := resolveWorkflowSnapshot(t, ctx, client, requester.ID, 7, "group-alpha")
	for _, node := range snapshot.Nodes {
		if len(node.Approvers) != 0 {
			t.Fatalf("node %d approvers = %#v, want requester excluded", node.Position, node.Approvers)
		}
	}
}

func TestWorkflowResolverExcludesInactiveConfiguredAndRepresentativeUsers(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	configuredDepartment := createQuotaResetDepartment(t, ctx, client, source.ID, "department-configured", "Configured", nil)
	representativeDepartment := createQuotaResetDepartment(t, ctx, client, source.ID, "department-representative", "Representative", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	inactiveConfigured := createQuotaResetUser(t, ctx, client, "inactive-configured", "inactive-configured@example.com", nil, "user")
	offboardedConfigured := createQuotaResetUser(t, ctx, client, "offboarded-configured", "offboarded-configured@example.com", nil, "user")
	inactiveRepresentative := createQuotaResetUser(t, ctx, client, "inactive-representative", "inactive-representative@example.com", nil, "user")
	offboardedRepresentative := createQuotaResetUser(t, ctx, client, "offboarded-representative", "offboarded-representative@example.com", nil, "user")
	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, configuredDepartment.ExternalID, &requester.ID)
	inactiveConfiguredMember := createQuotaResetMember(t, ctx, client, source.ID, "member-inactive-configured", inactiveConfigured.Email, configuredDepartment.ExternalID, &inactiveConfigured.ID)
	offboardedConfiguredMember := createQuotaResetMember(t, ctx, client, source.ID, "member-offboarded-configured", offboardedConfigured.Email, configuredDepartment.ExternalID, &offboardedConfigured.ID)
	inactiveRepresentativeMember := createQuotaResetMember(t, ctx, client, source.ID, "member-inactive-representative", inactiveRepresentative.Email, representativeDepartment.ExternalID, &inactiveRepresentative.ID)
	offboardedRepresentativeMember := createQuotaResetMember(t, ctx, client, source.ID, "member-offboarded-representative", offboardedRepresentative.Email, representativeDepartment.ExternalID, &offboardedRepresentative.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, configuredDepartment.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, representativeDepartment.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, inactiveConfiguredMember, configuredDepartment.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, offboardedConfiguredMember, configuredDepartment.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, inactiveRepresentativeMember, representativeDepartment.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, offboardedRepresentativeMember, representativeDepartment.ExternalID)
	client.DirectoryMember.UpdateOneID(inactiveConfiguredMember.ID).SetStatus("inactive").SaveX(ctx)
	client.DirectoryMember.UpdateOneID(inactiveRepresentativeMember.ID).SetStatus("inactive").SaveX(ctx)
	client.User.UpdateOneID(offboardedConfigured.ID).SetRelayDisabledAt(time.Now()).SaveX(ctx)
	client.User.UpdateOneID(offboardedRepresentative.ID).SetRelayDisabledAt(time.Now()).SaveX(ctx)
	client.DirectoryDepartment.UpdateOneID(representativeDepartment.ID).SetMetadata(map[string]any{"representative_external_ids": []any{inactiveRepresentativeMember.ExternalID, offboardedRepresentativeMember.ExternalID}}).SaveX(ctx)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, configuredDepartment.ExternalID, configuredDepartment.Name, inactiveConfigured.ID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, configuredDepartment.ExternalID, configuredDepartment.Name, offboardedConfigured.ID)

	snapshot := resolveWorkflowSnapshot(t, ctx, client, requester.ID, 1, "group-alpha")
	if len(snapshot.Nodes[0].Approvers) != 0 || snapshot.Nodes[0].InitialStatus != "skipped_no_approver" {
		t.Fatalf("initial node = %#v, want no inactive or offboarded candidates", snapshot.Nodes[0])
	}
}

func resolveWorkflowSnapshot(t *testing.T, ctx context.Context, client *ent.Client, requesterUserID, providerID int, groupID string) *WorkflowSnapshot {
	t.Helper()
	snapshot, err := NewWorkflowResolver(client).Resolve(ctx, requesterUserID, providerID, groupID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	return snapshot
}

func assertResolvedNodeApproverIDs(t *testing.T, node ResolvedWorkflowNode, want ...int) {
	t.Helper()
	got := make([]int, 0, len(node.Approvers))
	for _, approver := range node.Approvers {
		got = append(got, approver.UserID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("approver IDs = %#v, want %#v", got, want)
	}
}

func createWorkflowChain(t *testing.T, ctx context.Context, client *ent.Client, providerID int, groupID string, sourceID int, departments ...*ent.DirectoryDepartment) {
	t.Helper()
	chain := client.QuotaResetApprovalChain.Create().
		SetProviderID(providerID).
		SetGroupID(groupID).
		SetGroupName("Group Alpha").
		SetEnabled(true).
		SaveX(ctx)
	for position, department := range departments {
		client.QuotaResetApprovalChainNode.Create().
			SetChainID(chain.ID).
			SetPosition(position).
			SetDirectorySourceID(sourceID).
			SetDepartmentExternalID(department.ExternalID).
			SetDepartmentDisplayPath(department.Name).
			SaveX(ctx)
	}
}
