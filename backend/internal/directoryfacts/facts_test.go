package directoryfacts

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestFactsInterpretsHierarchyMembershipsMatchesAndRepresentatives(t *testing.T) {
	rootID := "department-root"
	cycleA, cycleB := "department-cycle-a", "department-cycle-b"
	matchedUserID := 7
	facts := NewFacts(
		Snapshot{SourceID: 3, RunID: 11},
		[]Department{
			{ExternalID: "department-child", EffectiveParentExternalID: &rootID, Name: "Child"},
			{ExternalID: rootID, Name: "Root", Metadata: map[string]any{DepartmentRepresentativeIDsKey: []any{"member-representative", json.Number("42")}}},
			{ExternalID: "department-missing-parent", EffectiveParentExternalID: stringPointer("absent"), Name: "Missing Parent"},
			{ExternalID: cycleA, EffectiveParentExternalID: &cycleB, Name: "Cycle A"},
			{ExternalID: cycleB, EffectiveParentExternalID: &cycleA, Name: "Cycle B"},
		},
		[]Member{
			{ID: 2, ExternalID: "member-email", EmailNormalized: " alice@EXAMPLE.com ", DepartmentExternalID: rootID, Status: "active"},
			{ID: 1, ExternalID: "member-representative", EmailNormalized: "representative@example.org", DepartmentExternalID: rootID, MatchedUserID: &matchedUserID, Status: "active", Metadata: map[string]any{MemberLeaderDepartmentIDsKey: "department-child, department-root"}},
		},
		[]Membership{
			{DirectoryMemberID: 2, DepartmentExternalID: "department-child"},
			{DirectoryMemberID: 2, DepartmentExternalID: rootID},
			{DirectoryMemberID: 2, DepartmentExternalID: "department-child"},
		},
		[]User{
			{ID: matchedUserID, Email: "representative@example.org"},
			{ID: 8, Email: "Alice@example.com"},
		},
	)

	if got, want := facts.Snapshot(), (Snapshot{SourceID: 3, RunID: 11}); got != want {
		t.Fatalf("snapshot = %+v, want %+v", got, want)
	}
	if got, want := departmentIDs(facts.Departments()), []string{"department-missing-parent", rootID, "department-child", cycleA, cycleB}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ordered departments = %#v, want %#v", got, want)
	}
	if got, want := facts.Hierarchy().DisplayPath("department-child"), "Root / Child"; got != want {
		t.Fatalf("display path = %q, want %q", got, want)
	}
	if got, want := facts.DepartmentIDsForMember(facts.Members()[1]), []string{"department-child", rootID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("multi-department memberships = %#v, want %#v", got, want)
	}
	if got := facts.UserForMember(facts.Members()[0]); got == nil || got.ID != matchedUserID {
		t.Fatalf("matched_user_id resolution = %+v, want user %d", got, matchedUserID)
	}
	if got := facts.UserForMember(facts.Members()[1]); got == nil || got.ID != 8 {
		t.Fatalf("email resolution = %+v, want user 8", got)
	}
	if got, want := facts.RepresentativeRoots("member-representative"), []string{"department-child", rootID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("representative roots = %#v, want %#v", got, want)
	}
	if got, want := facts.RepresentativeRoots("42"), []string{rootID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("numeric representative roots = %#v, want %#v", got, want)
	}
	if got, want := facts.DepartmentStats(rootID), (DepartmentStats{MemberCount: 2, MatchedUserCount: 2}); got != want {
		t.Fatalf("root stats = %+v, want %+v", got, want)
	}
}

func TestFactsOrderingDoesNotDependOnInputOrder(t *testing.T) {
	parent := "root"
	departments := []Department{
		{ExternalID: "zeta", EffectiveParentExternalID: &parent, Name: "Same"},
		{ExternalID: parent, Name: "Root"},
		{ExternalID: "alpha", EffectiveParentExternalID: &parent, Name: "Same"},
	}
	forward := NewFacts(Snapshot{}, departments, nil, nil, nil)
	reversed := NewFacts(Snapshot{}, []Department{departments[2], departments[1], departments[0]}, nil, nil, nil)
	want := []string{parent, "alpha", "zeta"}
	if got := departmentIDs(forward.Departments()); !reflect.DeepEqual(got, want) {
		t.Fatalf("forward order = %#v, want %#v", got, want)
	}
	if got := departmentIDs(reversed.Departments()); !reflect.DeepEqual(got, want) {
		t.Fatalf("reversed order = %#v, want %#v", got, want)
	}
}

func stringPointer(value string) *string { return &value }

func departmentIDs(departments []Department) []string {
	ids := make([]string, 0, len(departments))
	for _, department := range departments {
		ids = append(ids, department.ExternalID)
	}
	return ids
}
