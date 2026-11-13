package handler

import (
	"testing"

	"github.com/ai-efficiency/backend/internal/kirocredits"
	"github.com/ai-efficiency/backend/internal/representativescope"
)

func TestBuildTeamCreditsResponseAggregatesDirectAndSubtreeDepartments(t *testing.T) {
	relayOne, relayTwo := 101, 102
	scope := &representativescope.Scope{
		Departments: []representativescope.DepartmentScope{{ExternalID: "dept-root"}},
		RepresentedSubtreeIDs: map[string]map[string]struct{}{
			"dept-root": {"dept-root": {}, "dept-child": {}},
		},
		Subjects: []representativescope.Subject{
			{UserID: 1, RelayUserID: &relayOne, DepartmentExternalID: "dept-child"},
			{UserID: 2, RelayUserID: &relayTwo, DepartmentExternalID: "dept-root"},
		},
	}
	subjectsByUser := map[int]representativescope.Subject{
		1: scope.Subjects[0],
		2: scope.Subjects[1],
	}
	batch := kirocredits.TeamCredits{
		Available: true,
		Status:    kirocredits.StatusOK,
		Credits:   1.5,
		Users: []kirocredits.UserCredits{
			{UserID: 1, RelayUserID: 101, KeyFound: true, Credits: 1.0, Requests: 3},
			{UserID: 2, RelayUserID: 102, KeyFound: true, Credits: 0.5, Requests: 2},
		},
	}
	response := buildTeamCreditsResponse(scope, batch, subjectsByUser)
	if len(response.Departments) != 2 {
		t.Fatalf("departments = %+v, want 2", response.Departments)
	}
	got := map[string]float64{}
	for _, department := range response.Departments {
		got[department.DepartmentExternalID] = department.Credits
	}
	if got["dept-child"] != 1.0 {
		t.Fatalf("child credits = %v, want 1.0", got["dept-child"])
	}
	if got["dept-root"] != 1.5 {
		t.Fatalf("root subtree credits = %v, want 1.5", got["dept-root"])
	}
	if response.Credits != 1.5 {
		t.Fatalf("total credits = %v, want 1.5", response.Credits)
	}
}
