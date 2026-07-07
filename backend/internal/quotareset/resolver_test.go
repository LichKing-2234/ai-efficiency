package quotareset

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestResolveApproversUsesNearestConfiguredDepartmentPerPath(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	source := createQuotaResetDirectorySource(t, ctx, client)
	root := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Department Alpha", nil)
	child := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha-team", "Team One", &root.ExternalID)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approverRoot := createQuotaResetUser(t, ctx, client, "lead-root", "lead-root@example.com", nil, "user")
	approverChild := createQuotaResetUser(t, ctx, client, "lead-child", "lead-child@example.com", nil, "user")
	member := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, child.ExternalID, &requester.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, member, child.ExternalID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, root.ExternalID, root.Name, approverRoot.ID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, child.ExternalID, "Department Alpha / Team One", approverChild.ID)

	resolved, err := NewApproverResolver(client).Resolve(ctx, requester.ID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got, want := resolved.ApproverUserIDs, []int{approverChild.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("approvers = %#v, want nearest child approver %#v", got, want)
	}
	if len(resolved.Paths) != 1 || resolved.Paths[0].MatchedDepartmentExternalID != child.ExternalID {
		t.Fatalf("paths = %#v, want child match", resolved.Paths)
	}
}

func TestResolveApproversMergesMultiDepartmentPathsAndExcludesRequester(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	source := createQuotaResetDirectorySource(t, ctx, client)
	alpha := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Department Alpha", nil)
	beta := createQuotaResetDepartment(t, ctx, client, source.ID, "department-beta", "Department Beta", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approverAlpha := createQuotaResetUser(t, ctx, client, "lead-alpha", "lead-alpha@example.com", nil, "user")
	approverBeta := createQuotaResetUser(t, ctx, client, "lead-beta", "lead-beta@example.com", nil, "user")
	member := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, alpha.ExternalID, &requester.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, member, alpha.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, member, beta.ExternalID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, alpha.ExternalID, alpha.Name, approverAlpha.ID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, beta.ExternalID, beta.Name, approverBeta.ID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, beta.ExternalID, beta.Name, requester.ID)

	resolved, err := NewApproverResolver(client).Resolve(ctx, requester.ID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got, want := resolved.ApproverUserIDs, []int{approverAlpha.ID, approverBeta.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("approvers = %#v, want merged approvers %#v", got, want)
	}
	if len(resolved.Paths) != 2 {
		t.Fatalf("paths = %#v, want two membership paths", resolved.Paths)
	}
}

func TestResolveApproversReturnsEmptyWhenNoConfigExists(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	source := createQuotaResetDirectorySource(t, ctx, client)
	dept := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Department Alpha", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	member := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, dept.ExternalID, &requester.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, member, dept.ExternalID)

	resolved, err := NewApproverResolver(client).Resolve(ctx, requester.ID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(resolved.ApproverUserIDs) != 0 || len(resolved.Paths) != 1 || resolved.Paths[0].Resolution != "no_config_found" {
		t.Fatalf("resolved = %#v, want admin fallback evidence", resolved)
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

func intPtr(v int) *int {
	return &v
}
