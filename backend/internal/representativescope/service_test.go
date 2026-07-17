package representativescope

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

func TestIndexMemberDepartmentIDsSortsStablePrimaryDepartment(t *testing.T) {
	memberships := []*ent.DirectoryMemberDepartment{
		{DirectoryMemberID: 7, DepartmentExternalID: "department-zeta"},
		{DirectoryMemberID: 7, DepartmentExternalID: "department-alpha"},
		{DirectoryMemberID: 7, DepartmentExternalID: "department-beta"},
	}

	got := indexMemberDepartmentIDs(memberships)[7]
	want := []string{"department-alpha", "department-beta", "department-zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("member department IDs = %#v, want stable order %#v", got, want)
	}
}

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

func TestResolveRepresentativeScopeKeepsDirectoryOnlyMembersWithBlankExternalIDs(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	source := createScopeSource(t, client, true)
	actor := createScopeUser(t, client, "actor", "actor@example.com", nil)
	createScopeDepartment(t, client, source.ID, "department-team", "Department Team", nil, map[string]any{"representative_external_ids": []string{"member-actor"}})
	createScopeMember(t, client, source.ID, "member-actor", actor.Email, "department-team", &actor.ID, nil)
	createScopeMember(t, client, source.ID, "", "alice@example.com", "department-team", nil, nil)
	createScopeMember(t, client, source.ID, "", "bob@example.org", "department-team", nil, nil)

	scope, err := New(client).Resolve(ctx, actor.ID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if findSubjectByEmail(scope.Subjects, "alice@example.com") == nil {
		t.Fatalf("subjects = %#v, want alice@example.com directory-only subject", scope.Subjects)
	}
	if findSubjectByEmail(scope.Subjects, "bob@example.org") == nil {
		t.Fatalf("subjects = %#v, want bob@example.org directory-only subject", scope.Subjects)
	}
	if len(scope.Subjects) != 2 {
		t.Fatalf("subjects = %#v, want two directory-only members excluding actor", scope.Subjects)
	}
}

func TestResolveRepresentativeScopeUsesDirectoryMemberDepartments(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	source := createScopeSource(t, client, true)
	actor := createScopeUser(t, client, "actor", "actor@example.com", nil)
	targetRelayID := 1010
	target := createScopeUser(t, client, "alice", "alice@example.com", &targetRelayID)
	alpha := createScopeDepartment(t, client, source.ID, "department-alpha", "Department Alpha", nil, nil)
	beta := createScopeDepartment(t, client, source.ID, "department-beta", "Department Beta", nil, map[string]any{"representative_external_ids": []string{"member-actor"}})
	createScopeMember(t, client, source.ID, "member-actor", actor.Email, beta.ExternalID, &actor.ID, nil)
	alice := createScopeMember(t, client, source.ID, "member-alice", target.Email, alpha.ExternalID, &target.ID, nil)
	createScopeMemberDepartment(t, client, source.ID, alice, beta.ExternalID)

	scope, err := New(client).Resolve(ctx, actor.ID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got, want := scope.AllowedUserIDs(), []int{target.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("allowed users = %#v, want multi-department target %#v", got, want)
	}
	subject := findSubjectByEmail(scope.Subjects, target.Email)
	if subject == nil {
		t.Fatalf("subjects = %#v, want target subject", scope.Subjects)
	}
	if subject.DepartmentExternalID != beta.ExternalID {
		t.Fatalf("subject department = %q, want represented membership department %q", subject.DepartmentExternalID, beta.ExternalID)
	}
}

func TestResolveRepresentativeScopeBuildsLargestDepartmentTree(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	source := createScopeSource(t, client, true)
	actor := createScopeUser(t, client, "actor", "actor@example.com", nil)
	alpha := createScopeDepartment(t, client, source.ID, "department-alpha", "Department Alpha", nil, map[string]any{"representative_external_ids": []string{"member-actor"}})
	alphaChild := createScopeDepartment(t, client, source.ID, "department-alpha-team-one", "Team One", &alpha.ExternalID, map[string]any{"representative_external_ids": []string{"member-actor"}})
	beta := createScopeDepartment(t, client, source.ID, "department-beta", "Department Beta", nil, map[string]any{"representative_external_ids": []string{"member-actor"}})
	createScopeDepartment(t, client, source.ID, "department-gamma", "Department Gamma", nil, nil)
	createScopeMember(t, client, source.ID, "member-actor", actor.Email, alpha.ExternalID, &actor.ID, map[string]any{"leader_department_ids": []string{alpha.ExternalID, alphaChild.ExternalID, beta.ExternalID}})
	createScopeMember(t, client, source.ID, "member-alice", "alice@example.com", alpha.ExternalID, nil, nil)
	createScopeMember(t, client, source.ID, "member-bob", "bob@example.org", alphaChild.ExternalID, nil, nil)
	createScopeMember(t, client, source.ID, "member-carol", "carol@example.net", beta.ExternalID, nil, nil)
	createScopeMember(t, client, source.ID, "member-dana", "dana@example.net", "department-gamma", nil, nil)

	scope, err := New(client).Resolve(ctx, actor.ID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if got, want := scope.RepresentedDepartmentIDs, []string{alpha.ExternalID, alphaChild.ExternalID, beta.ExternalID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("represented departments = %#v, want raw representative roots %#v", got, want)
	}
	if got, want := scope.MemberTreeRootIDs, []string{alpha.ExternalID, beta.ExternalID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("member tree roots = %#v, want largest non-overlapping roots %#v", got, want)
	}
	if got := departmentIDs(scope.MemberTreeDepartments); !reflect.DeepEqual(got, []string{alpha.ExternalID, alphaChild.ExternalID, beta.ExternalID}) {
		t.Fatalf("member tree departments = %#v, want alpha, alpha child, beta", got)
	}
	childNode := findDepartmentNode(scope.MemberTreeDepartments, alphaChild.ExternalID)
	if childNode == nil || childNode.ParentExternalID == nil || *childNode.ParentExternalID != alpha.ExternalID || childNode.Depth != 1 {
		t.Fatalf("child node = %#v, want parent alpha and depth 1", childNode)
	}
	if got := departmentIDs(scope.Departments); !reflect.DeepEqual(got, []string{alpha.ExternalID, beta.ExternalID}) {
		t.Fatalf("scope departments = %#v, want only largest visible root summaries", got)
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

func departmentIDs(departments []DepartmentScope) []string {
	ids := make([]string, 0, len(departments))
	for _, department := range departments {
		ids = append(ids, department.ExternalID)
	}
	return ids
}

func findDepartmentNode(departments []DepartmentScope, externalID string) *DepartmentScope {
	for i := range departments {
		if departments[i].ExternalID == externalID {
			return &departments[i]
		}
	}
	return nil
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

func TestResolveRepresentativeScopeCacheSelectsNewDirectoryRunAndRoleImmediately(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	oldCompletedAt := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	newCompletedAt := oldCompletedAt.Add(time.Hour)
	oldSource := createScopeSourceWithCompletedAt(t, client, true, &oldCompletedAt)
	actorRelayID := 1100
	oldTargetRelayID := 1101
	newTargetRelayID := 1102
	actor := createScopeUser(t, client, "actor-cache", "actor-cache@example.com", &actorRelayID)
	oldTarget := createScopeUser(t, client, "old-target-cache", "old-target-cache@example.org", &oldTargetRelayID)
	createScopeDepartment(t, client, oldSource.ID, "department-old-cache", "Department Old Cache", nil, map[string]any{"representative_external_ids": []string{"member-actor"}})
	createScopeMember(t, client, oldSource.ID, "member-actor", actor.Email, "department-old-cache", &actor.ID, nil)
	createScopeMember(t, client, oldSource.ID, "member-old-target", oldTarget.Email, "department-old-cache", &oldTarget.ID, nil)

	cache, server := testScopeCache(t, "test")
	service := NewWithCache(client, cache)
	oldScope, err := service.Resolve(ctx, actor.ID)
	if err != nil {
		t.Fatalf("Resolve old scope: %v", err)
	}
	if got, want := oldScope.AllowedUserIDs(), []int{oldTarget.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("old allowed users = %#v, want %#v", got, want)
	}
	if _, err := service.Resolve(ctx, actor.ID); err != nil {
		t.Fatalf("Resolve cached old scope: %v", err)
	}

	newTarget := createScopeUser(t, client, "new-target-cache", "new-target-cache@example.net", &newTargetRelayID)
	newSource := createScopeSourceWithCompletedAt(t, client, true, &newCompletedAt)
	createScopeDepartment(t, client, newSource.ID, "department-new-cache", "Department New Cache", nil, map[string]any{"representative_external_ids": []string{"member-actor"}})
	createScopeMember(t, client, newSource.ID, "member-actor", actor.Email, "department-new-cache", &actor.ID, nil)
	createScopeMember(t, client, newSource.ID, "member-new-target", newTarget.Email, "department-new-cache", &newTarget.ID, nil)

	newScope, err := service.Resolve(ctx, actor.ID)
	if err != nil {
		t.Fatalf("Resolve new directory scope: %v", err)
	}
	if got, want := newScope.AllowedUserIDs(), []int{newTarget.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("new allowed users = %#v, want %#v", got, want)
	}
	if err := client.User.UpdateOneID(actor.ID).SetRole(entuser.RoleAdmin).Exec(ctx); err != nil {
		t.Fatalf("update actor role: %v", err)
	}
	roleScope, err := service.Resolve(ctx, actor.ID)
	if err != nil {
		t.Fatalf("Resolve new role scope: %v", err)
	}
	if got, want := roleScope.AllowedUserIDs(), []int{newTarget.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("new-role allowed users = %#v, want %#v", got, want)
	}

	valueKeys := 0
	for _, key := range server.Keys() {
		if strings.Contains(key, ":representative-scope:") && !strings.HasSuffix(key, ":lease") {
			valueKeys++
		}
	}
	if valueKeys != 3 {
		t.Fatalf("representative scope value keys = %d, want 3 for old run, new run, and new role", valueKeys)
	}
}

func TestResolveRepresentativeScopeCacheStillValidatesCurrentActor(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	source := createScopeSource(t, client, true)
	actor := createScopeUser(t, client, "actor-current-guard", "actor-current-guard@example.com", nil)
	createScopeDepartment(t, client, source.ID, "department-current-guard", "Department Current Guard", nil, map[string]any{"representative_external_ids": []string{"member-actor"}})
	createScopeMember(t, client, source.ID, "member-actor", actor.Email, "department-current-guard", &actor.ID, nil)
	cache, _ := testScopeCache(t, "test")
	service := NewWithCache(client, cache)
	if _, err := service.Resolve(ctx, actor.ID); err != nil {
		t.Fatalf("initial Resolve: %v", err)
	}
	if err := client.User.DeleteOneID(actor.ID).Exec(ctx); err != nil {
		t.Fatalf("delete current actor: %v", err)
	}
	if scope, err := service.Resolve(ctx, actor.ID); err == nil || scope != nil {
		t.Fatalf("Resolve deleted actor = %#v, %v; want nil error result", scope, err)
	}
}

func TestResolveRepresentativeScopeRechecksGuardAfterCacheRead(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	oldCompletedAt := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	newCompletedAt := oldCompletedAt.Add(time.Hour)
	oldSource := createScopeSourceWithCompletedAt(t, client, true, &oldCompletedAt)
	actorRelayID := 1200
	oldTargetRelayID := 1201
	newTargetRelayID := 1202
	actor := createScopeUser(t, client, "actor-race", "actor-race@example.com", &actorRelayID)
	oldTarget := createScopeUser(t, client, "old-target-race", "old-target-race@example.org", &oldTargetRelayID)
	createScopeDepartment(t, client, oldSource.ID, "department-old-race", "Department Old Race", nil, map[string]any{"representative_external_ids": []string{"member-actor"}})
	createScopeMember(t, client, oldSource.ID, "member-actor", actor.Email, "department-old-race", &actor.ID, nil)
	createScopeMember(t, client, oldSource.ID, "member-old-target", oldTarget.Email, "department-old-race", &oldTarget.ID, nil)

	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	store := &blockingScopeGetStore{
		Store:   readcache.NewRedisStore(redisClient),
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	cache := newTestScopeCache(t, store, "test")
	service := NewWithCache(client, cache)
	if _, err := service.Resolve(ctx, actor.ID); err != nil {
		t.Fatalf("initial Resolve: %v", err)
	}

	store.armed.Store(true)
	type resolveResult struct {
		scope *Scope
		err   error
	}
	resultCh := make(chan resolveResult, 1)
	go func() {
		scope, err := service.Resolve(ctx, actor.ID)
		resultCh <- resolveResult{scope: scope, err: err}
	}()
	select {
	case <-store.started:
	case <-time.After(time.Second):
		t.Fatal("cached read did not reach blocking store")
	}

	newTarget := createScopeUser(t, client, "new-target-race", "new-target-race@example.net", &newTargetRelayID)
	newSource := createScopeSourceWithCompletedAt(t, client, true, &newCompletedAt)
	createScopeDepartment(t, client, newSource.ID, "department-new-race", "Department New Race", nil, map[string]any{"representative_external_ids": []string{"member-actor"}})
	createScopeMember(t, client, newSource.ID, "member-actor", actor.Email, "department-new-race", &actor.ID, nil)
	createScopeMember(t, client, newSource.ID, "member-new-target", newTarget.Email, "department-new-race", &newTarget.ID, nil)
	close(store.release)

	select {
	case result := <-resultCh:
		if result.err != nil {
			t.Fatalf("Resolve after guard change: %v", result.err)
		}
		if got, want := result.scope.AllowedUserIDs(), []int{newTarget.ID}; !reflect.DeepEqual(got, want) {
			t.Fatalf("allowed users after guard race = %#v, want new snapshot %#v", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Resolve did not finish after guard change")
	}
}

func TestResolveRepresentativeScopeWarmHitAvoidsFullDirectoryReadsAtScale(t *testing.T) {
	client, dsn := testdb.OpenWithDSN(t)
	ctx := context.Background()
	source := createScopeSource(t, client, true)
	actorRelayID := 1300
	actor := createScopeUser(t, client, "actor-scale", "actor-scale@example.com", &actorRelayID)
	createScopeDepartment(t, client, source.ID, "department-scale", "Department Scale", nil, map[string]any{"representative_external_ids": []string{"member-actor"}})
	createScopeMember(t, client, source.ID, "member-actor", actor.Email, "department-scale", &actor.ID, nil)

	const representedMembers = 500
	userCreates := make([]*ent.UserCreate, 0, representedMembers)
	for index := 0; index < representedMembers; index++ {
		userCreates = append(userCreates, client.User.Create().
			SetUsername(fmt.Sprintf("member-scale-%03d", index)).
			SetEmail(fmt.Sprintf("member-scale-%03d@example.org", index)).
			SetAuthSource(entuser.AuthSourceLdap).
			SetRole(entuser.RoleUser).
			SetRelayUserID(2000+index))
	}
	users, err := client.User.CreateBulk(userCreates...).Save(ctx)
	if err != nil {
		t.Fatalf("create scale users: %v", err)
	}
	memberCreates := make([]*ent.DirectoryMemberCreate, 0, representedMembers)
	for index, user := range users {
		memberCreates = append(memberCreates, client.DirectoryMember.Create().
			SetSourceID(source.ID).
			SetExternalID(fmt.Sprintf("directory-member-scale-%03d", index)).
			SetEmailNormalized(user.Email).
			SetDisplayName(user.Username).
			SetDepartmentExternalID("department-scale").
			SetMatchedUserID(user.ID).
			SetLastSeenRunID(*source.LastSuccessfulRunID))
	}
	if _, err := client.DirectoryMember.CreateBulk(memberCreates...).Save(ctx); err != nil {
		t.Fatalf("create scale directory members: %v", err)
	}

	recorder := &representativeScopeQueryRecorder{}
	loggedClient, err := ent.Open("postgres", dsn, ent.Debug(), ent.Log(recorder.Log))
	if err != nil {
		t.Fatalf("open logged ent client: %v", err)
	}
	t.Cleanup(func() { _ = loggedClient.Close() })
	cache, _ := testScopeCache(t, "test")
	service := NewWithCache(loggedClient, cache)
	coldScope, err := service.Resolve(ctx, actor.ID)
	if err != nil {
		t.Fatalf("cold Resolve: %v", err)
	}
	if got := len(coldScope.AllowedUserIDs()); got != representedMembers {
		t.Fatalf("cold allowed users = %d, want %d", got, representedMembers)
	}

	recorder.Reset()
	warmScope, err := service.Resolve(ctx, actor.ID)
	if err != nil {
		t.Fatalf("warm Resolve: %v", err)
	}
	if got := len(warmScope.AllowedUserIDs()); got != representedMembers {
		t.Fatalf("warm allowed users = %d, want %d", got, representedMembers)
	}
	if got := recorder.Count(); got != 6 {
		t.Fatalf("warm query count = %d, want two three-query guards; queries:\n%s", got, recorder.Joined())
	}
	queries := recorder.Joined()
	for _, table := range []string{`"directory_members"`, `"directory_member_departments"`, `"directory_departments"`} {
		if strings.Contains(queries, table) {
			t.Fatalf("warm scope queried full table %s; queries:\n%s", table, queries)
		}
	}
}

type blockingScopeGetStore struct {
	readcache.Store
	armed   atomic.Bool
	blocked atomic.Bool
	started chan struct{}
	release chan struct{}
}

func (s *blockingScopeGetStore) Get(ctx context.Context, key string) ([]byte, error) {
	if s.armed.Load() && s.blocked.CompareAndSwap(false, true) {
		close(s.started)
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.Store.Get(ctx, key)
}

type representativeScopeQueryRecorder struct {
	mu      sync.Mutex
	queries []string
}

func (r *representativeScopeQueryRecorder) Log(values ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queries = append(r.queries, fmt.Sprint(values...))
}

func (r *representativeScopeQueryRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queries = nil
}

func (r *representativeScopeQueryRecorder) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.queries)
}

func (r *representativeScopeQueryRecorder) Joined() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.queries, "\n")
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

func createScopeMemberDepartment(t *testing.T, client *ent.Client, sourceID int, member *ent.DirectoryMember, departmentID string) {
	t.Helper()
	_, err := client.DirectoryMemberDepartment.Create().
		SetSourceID(sourceID).
		SetDirectoryMemberID(member.ID).
		SetMemberExternalID(member.ExternalID).
		SetMemberEmailNormalized(member.EmailNormalized).
		SetDepartmentExternalID(departmentID).
		SetLastSeenRunID(member.LastSeenRunID).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create member department %s/%s: %v", member.EmailNormalized, departmentID, err)
	}
}
