package representativescope

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

func TestResolveRepresentativeScopeFromDepartmentMetadataIncludesSubtree(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	source := createScopeSource(t, client, true)
	actorRelayID := 1000
	targetRelayID := 1001
	actor := createScopeUser(t, client, "actor", "actor@example.com", &actorRelayID)
	target := createScopeUser(t, client, "alice", "alice@example.com", &targetRelayID)
	root := createScopeDepartment(t, client, source.ID, "department-alpha", "Department Alpha", nil, map[string]any{"representative_external_ids": []any{"member-actor"}})
	createScopeDepartment(t, client, source.ID, "department-alpha-child", "Department Alpha Child", &root.ExternalID, nil)
	createScopeMember(t, client, source.ID, "member-actor", actor.Email, root.ExternalID, &actor.ID, nil)
	createScopeMember(t, client, source.ID, "member-alice", target.Email, "department-alpha-child", &target.ID, nil)

	scope, err := New(client).Resolve(ctx, actor.ID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !scope.IsRepresentative {
		t.Fatal("scope should be representative")
	}
	if got, want := scope.AllowedUserIDs(), []int{target.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("allowed users = %#v, want %#v", got, want)
	}
}

func TestResolveRepresentativeScopeFromMemberLeaderDepartmentIDs(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	source := createScopeSource(t, client, true)
	actor := createScopeUser(t, client, "actor", "actor@example.com", nil)
	targetRelayID := 1002
	target := createScopeUser(t, client, "bob", "bob@example.org", &targetRelayID)
	createScopeDepartment(t, client, source.ID, "department-beta", "Department Beta", nil, nil)
	createScopeMember(t, client, source.ID, "member-actor", actor.Email, "department-beta", &actor.ID, map[string]any{"leader_department_ids": []string{"department-beta"}})
	createScopeMember(t, client, source.ID, "member-bob", target.Email, "department-beta", &target.ID, nil)

	scope, err := New(client).Resolve(ctx, actor.ID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got, want := scope.AllowedUserIDs(), []int{target.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("allowed users = %#v, want %#v", got, want)
	}
}

func TestResolveRepresentativeScopeParsesNumericLeaderDepartmentIDs(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	source := createScopeSource(t, client, true)
	actor := createScopeUser(t, client, "actor", "actor@example.com", nil)
	targetRelayID := 1007
	target := createScopeUser(t, client, "bob", "bob@example.org", &targetRelayID)
	createScopeDepartment(t, client, source.ID, "1684078", "Department Numeric", nil, map[string]any{"representative_external_ids": []any{"member-actor"}})
	createScopeMember(t, client, source.ID, "member-actor", actor.Email, "1684078", &actor.ID, map[string]any{"leader_department_ids": []any{float64(1684078)}})
	createScopeMember(t, client, source.ID, "member-bob", target.Email, "1684078", &target.ID, nil)

	scope, err := New(client).Resolve(ctx, actor.ID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got, want := scope.RepresentedDepartmentIDs, []string{"1684078"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("represented departments = %#v, want %#v", got, want)
	}
	if len(scope.Departments) != 1 || scope.Departments[0].ExternalID != "1684078" || scope.Departments[0].Name != "Department Numeric" {
		t.Fatalf("departments = %#v, want single numeric department", scope.Departments)
	}
	if got, want := scope.AllowedUserIDs(), []int{target.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("allowed users = %#v, want %#v", got, want)
	}
}

func TestResolveRepresentativeScopeUsesLatestSuccessfulApplyRunNotSourceUpdatedAt(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	olderCompletedAt := time.Date(2026, 6, 26, 9, 0, 0, 0, time.UTC)
	latestCompletedAt := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	olderSource := createScopeSourceWithCompletedAt(t, client, true, &olderCompletedAt)
	latestSource := createScopeSourceWithCompletedAt(t, client, true, &latestCompletedAt)
	actorRelayID := 1003
	olderRelayID := 1004
	latestRelayID := 1005
	actor := createScopeUser(t, client, "actor-current", "actor-current@example.com", &actorRelayID)
	olderTarget := createScopeUser(t, client, "older-target", "older-target@example.com", &olderRelayID)
	latestTarget := createScopeUser(t, client, "latest-target", "latest-target@example.com", &latestRelayID)
	createScopeDepartment(t, client, olderSource.ID, "department-older", "Department Older", nil, map[string]any{"representative_external_ids": []string{"member-actor"}})
	createScopeMember(t, client, olderSource.ID, "member-actor", actor.Email, "department-older", &actor.ID, nil)
	createScopeMember(t, client, olderSource.ID, "member-older-target", olderTarget.Email, "department-older", &olderTarget.ID, nil)
	createScopeDepartment(t, client, latestSource.ID, "department-latest", "Department Latest", nil, map[string]any{"representative_external_ids": []string{"member-actor"}})
	createScopeMember(t, client, latestSource.ID, "member-actor", actor.Email, "department-latest", &actor.ID, nil)
	createScopeMember(t, client, latestSource.ID, "member-latest-target", latestTarget.Email, "department-latest", &latestTarget.ID, nil)
	if _, err := client.DirectorySource.UpdateOneID(olderSource.ID).SetDescription("Edited after latest sync").Save(ctx); err != nil {
		t.Fatalf("update older source: %v", err)
	}

	scope, err := New(client).Resolve(ctx, actor.ID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got, want := scope.AllowedUserIDs(), []int{latestTarget.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("allowed users = %#v, want latest source user %#v", got, want)
	}
}

func TestResolveRepresentativeScopeDeduplicatesSelfIntoMyUsage(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	source := createScopeSource(t, client, true)
	actor := createScopeUser(t, client, "actor", "actor@example.com", nil)
	createScopeDepartment(t, client, source.ID, "department-alpha", "Department Alpha", nil, map[string]any{"representative_external_ids": []string{"member-actor"}})
	createScopeMember(t, client, source.ID, "member-actor", actor.Email, "department-alpha", &actor.ID, nil)

	scope, err := New(client).Resolve(ctx, actor.ID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got := scope.AllowedUserIDs(); len(got) != 0 {
		t.Fatalf("allowed users = %#v, want no self member rows", got)
	}
}

func TestCanManageRepresentativeRequiresStrictAncestorForAllTargetRepresentedDepartments(t *testing.T) {
	scope := Scope{
		ActorUserID: 1,
		Subjects:    []Subject{{SubjectType: "member", UserID: 2, Selectable: true}},
		RepresentedSubtreeIDs: map[string]map[string]struct{}{
			"department-root": {"department-root": {}, "department-child": {}, "department-grandchild": {}},
		},
		TargetRepresentedRoots: map[int][]string{2: {"department-grandchild"}},
	}
	ok, reason := scope.CanManageTarget(2)
	if !ok || reason != "" {
		t.Fatalf("CanManageTarget() = %v, %q; want true, empty reason", ok, reason)
	}
}

func TestCanManageRepresentativeRejectsPeerRepresentative(t *testing.T) {
	scope := Scope{
		ActorUserID:            1,
		Subjects:               []Subject{{SubjectType: "member", UserID: 2, Selectable: true}},
		RepresentedSubtreeIDs:  map[string]map[string]struct{}{"department-root": {"department-root": {}, "department-child": {}}},
		TargetRepresentedRoots: map[int][]string{2: {"department-root"}},
	}
	ok, reason := scope.CanManageTarget(2)
	if ok || reason != "not_upper_level_representative" {
		t.Fatalf("CanManageTarget() = %v, %q; want false, not_upper_level_representative", ok, reason)
	}
}

func TestResolveRepresentativeScopeIncludesNoRelaySubjectAsUnavailable(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	source := createScopeSource(t, client, true)
	actor := createScopeUser(t, client, "actor", "actor@example.com", nil)
	target := createScopeUser(t, client, "charlie", "charlie@example.net", nil)
	createScopeDepartment(t, client, source.ID, "department-gamma", "Department Gamma", nil, map[string]any{"representative_external_ids": []string{"member-actor"}})
	createScopeMember(t, client, source.ID, "member-actor", actor.Email, "department-gamma", &actor.ID, nil)
	createScopeMember(t, client, source.ID, "member-charlie", target.Email, "department-gamma", &target.ID, nil)

	scope, err := New(client).Resolve(ctx, actor.ID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got := scope.AllowedUserIDs(); len(got) != 0 {
		t.Fatalf("allowed users = %#v, want no selectable users without relay mapping", got)
	}
	if len(scope.Subjects) != 1 {
		t.Fatalf("subjects = %#v, want one unavailable no-relay subject", scope.Subjects)
	}
	subject := scope.Subjects[0]
	if subject.UserID != target.ID {
		t.Fatalf("subject user id = %d, want %d", subject.UserID, target.ID)
	}
	if subject.Selectable {
		t.Fatalf("subject selectable = true, want false for nil relay user")
	}
	if subject.RelayUserID != nil {
		t.Fatalf("subject relay user id = %#v, want nil", subject.RelayUserID)
	}
	ok, reason := scope.CanManageTarget(target.ID)
	if !ok || reason != "" {
		t.Fatalf("CanManageTarget(no relay) = %v, %q; want true, empty reason", ok, reason)
	}
}

func TestResolveRepresentativeScopeIncludesDirectoryMembersWithoutLocalUsers(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	source := createScopeSource(t, client, true)
	actorRelayID := 1008
	targetRelayID := 1009
	actor := createScopeUser(t, client, "actor", "actor@example.com", &actorRelayID)
	target := createScopeUser(t, client, "alice", "alice@example.com", &targetRelayID)
	createScopeDepartment(t, client, source.ID, "department-team", "Department Team", nil, map[string]any{"representative_external_ids": []string{"member-actor"}})
	createScopeMember(t, client, source.ID, "member-actor", actor.Email, "department-team", &actor.ID, nil)
	createScopeMember(t, client, source.ID, "member-alice", target.Email, "department-team", &target.ID, nil)
	createScopeMember(t, client, source.ID, "member-bob", "bob@example.org", "department-team", nil, nil)
	createScopeMember(t, client, source.ID, "member-carol", "carol@example.net", "department-team", nil, nil)

	scope, err := New(client).Resolve(ctx, actor.ID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(scope.Subjects) != 3 {
		t.Fatalf("subjects = %#v, want three manageable team members excluding actor", scope.Subjects)
	}
	if len(scope.OverviewSubjects) != 4 {
		t.Fatalf("overview subjects = %#v, want full directory roster including actor", scope.OverviewSubjects)
	}
	actorOverview := findSubjectByEmail(scope.OverviewSubjects, "actor@example.com")
	if actorOverview == nil {
		t.Fatalf("overview subjects = %#v, want actor row", scope.OverviewSubjects)
	}
	if actorOverview.Selectable {
		t.Fatalf("actor overview selectable = true, want false")
	}
	if got, want := scope.Departments[0].SubtreeMemberCount, 4; got != want {
		t.Fatalf("subtree member count = %d, want %d", got, want)
	}
	if got, want := scope.Departments[0].MatchedUserCount, 2; got != want {
		t.Fatalf("matched user count = %d, want %d", got, want)
	}
	if got, want := scope.AllowedUserIDs(), []int{target.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("allowed users = %#v, want only selectable local target %#v", got, want)
	}
	bob := findSubjectByEmail(scope.Subjects, "bob@example.org")
	if bob == nil {
		t.Fatalf("subjects = %#v, want bob@example.org directory-only subject", scope.Subjects)
	}
	if bob.UserID != 0 {
		t.Fatalf("bob user id = %d, want 0 for directory-only subject", bob.UserID)
	}
	if bob.DirectoryMemberExternalID != "member-bob" {
		t.Fatalf("bob directory external id = %q, want member-bob", bob.DirectoryMemberExternalID)
	}
	if bob.Selectable {
		t.Fatalf("bob selectable = true, want false for directory-only subject")
	}
	ok, reason := scope.CanManageTarget(0)
	if ok || reason != "out_of_scope" {
		t.Fatalf("CanManageTarget(0) = %v, %q; want false, out_of_scope", ok, reason)
	}
}

func TestResolveRepresentativeScopeFailsClosedWhenSuccessfulRunLacksCompletedAt(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	source := createScopeSourceWithCompletedAt(t, client, true, nil)
	actor := createScopeUser(t, client, "actor", "actor@example.com", nil)
	targetRelayID := 1006
	target := createScopeUser(t, client, "dana", "dana@example.net", &targetRelayID)
	createScopeDepartment(t, client, source.ID, "department-delta", "Department Delta", nil, map[string]any{"representative_external_ids": []string{"member-actor"}})
	createScopeMember(t, client, source.ID, "member-actor", actor.Email, "department-delta", &actor.ID, nil)
	createScopeMember(t, client, source.ID, "member-dana", target.Email, "department-delta", &target.ID, nil)

	scope, err := New(client).Resolve(ctx, actor.ID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if scope.IsRepresentative || len(scope.Subjects) != 0 {
		t.Fatalf("scope = %#v, want non-representative empty scope for source without completed_at", scope)
	}
}

func findSubjectByEmail(subjects []Subject, email string) *Subject {
	for i := range subjects {
		if subjects[i].Email == email {
			return &subjects[i]
		}
	}
	return nil
}

func TestResolveRepresentativeScopeFailsClosedWithoutCurrentSource(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	actor := createScopeUser(t, client, "actor", "actor@example.com", nil)
	scope, err := New(client).Resolve(ctx, actor.ID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if scope.IsRepresentative || len(scope.Subjects) != 0 {
		t.Fatalf("scope = %#v, want non-representative empty scope", scope)
	}
}

func createScopeUser(t *testing.T, client *ent.Client, username, email string, relayID *int) *ent.User {
	t.Helper()
	create := client.User.Create().
		SetUsername(username).
		SetEmail(strings.ToLower(strings.TrimSpace(email))).
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleUser)
	if relayID != nil {
		create.SetRelayUserID(*relayID)
	}
	user, err := create.Save(context.Background())
	if err != nil {
		t.Fatalf("create user %s: %v", username, err)
	}
	return user
}

func createScopeSource(t *testing.T, client *ent.Client, current bool) *ent.DirectorySource {
	t.Helper()
	completedAt := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	return createScopeSourceWithCompletedAt(t, client, current, &completedAt)
}

func createScopeSourceWithCompletedAt(t *testing.T, client *ent.Client, current bool, completedAt *time.Time) *ent.DirectorySource {
	t.Helper()
	ctx := context.Background()
	source, err := client.DirectorySource.Create().
		SetName("Example Directory").
		SetDescription("Synthetic organization directory").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		Save(ctx)
	if err != nil {
		t.Fatalf("create directory source: %v", err)
	}
	runCreate := client.DirectorySyncRun.Create().
		SetSourceID(source.ID).
		SetMode("apply").
		SetStatus("completed").
		SetPhase("completed").
		SetDepartmentCount(1).
		SetMemberCount(1)
	if completedAt != nil {
		runCreate.SetCompletedAt(*completedAt)
	}
	run, err := runCreate.Save(ctx)
	if err != nil {
		t.Fatalf("create directory run: %v", err)
	}
	update := client.DirectorySource.UpdateOneID(source.ID).SetLastRunID(run.ID)
	if current {
		update.SetLastSuccessfulRunID(run.ID)
	}
	source, err = update.Save(ctx)
	if err != nil {
		t.Fatalf("update directory source run pointers: %v", err)
	}
	return source
}

func createScopeDepartment(t *testing.T, client *ent.Client, sourceID int, externalID, name string, parent *string, metadata map[string]any) *ent.DirectoryDepartment {
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
	if metadata != nil {
		create.SetMetadata(metadata)
	}
	department, err := create.Save(context.Background())
	if err != nil {
		t.Fatalf("create department %s: %v", externalID, err)
	}
	return department
}

func createScopeMember(t *testing.T, client *ent.Client, sourceID int, externalID, email, departmentID string, matchedUserID *int, metadata map[string]any) *ent.DirectoryMember {
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
	if metadata != nil {
		create.SetMetadata(metadata)
	}
	member, err := create.Save(context.Background())
	if err != nil {
		t.Fatalf("create member %s: %v", externalID, err)
	}
	return member
}
