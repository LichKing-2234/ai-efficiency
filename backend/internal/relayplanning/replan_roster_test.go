package relayplanning

import (
	"reflect"
	"strings"
	"testing"
)

func TestReviewReplanRosterKeepsUnavailableSavedMemberAndBlocksExecution(t *testing.T) {
	result, err := reviewReplanRoster(replanRosterInput{
		TargetGroupIDs: []int64{101},
		SavedAssignments: map[int]int64{
			1: 101,
			2: 101,
		},
		Members: []replanRosterMember{
			{UserID: 1, Assignable: true, RangeCost: 12.5},
			{UserID: 2, UnavailableReason: replanRosterUnavailableIdentity},
		},
		UnmanagedCosts: map[int64]float64{101: 3.5},
	})
	if err != nil {
		t.Fatalf("reviewReplanRoster() error = %v", err)
	}
	if len(result.Targets) != 1 || result.Targets[0].GroupID != 101 {
		t.Fatalf("targets = %+v, want saved Target 101", result.Targets)
	}
	if got := result.Targets[0].UserIDs; !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("saved roster = %v, want [1 2]", got)
	}
	if got := result.Targets[0].TotalCost; got != 16 {
		t.Fatalf("saved roster total cost = %v, want 16", got)
	}
	wantBlockers := []replanRosterBlocker{{UserID: 2, Reason: replanRosterUnavailableIdentity}}
	if !reflect.DeepEqual(result.Blockers, wantBlockers) {
		t.Fatalf("blockers = %v, want %v", result.Blockers, wantBlockers)
	}
}

func TestReviewReplanRosterAppliesExplicitEditsWithoutDroppingUnavailableSavedMember(t *testing.T) {
	result, err := reviewReplanRoster(replanRosterInput{
		TargetGroupIDs: []int64{101, 102},
		SavedAssignments: map[int]int64{
			1: 101,
			2: 101,
		},
		Members: []replanRosterMember{
			{UserID: 1, Assignable: true, RangeCost: 10},
			{UserID: 2, UnavailableReason: replanRosterUnavailableIdentity},
			{UserID: 3, Assignable: true, RangeCost: 5},
		},
		HasReview: true,
		ReviewedTargets: []replanRosterTargetReview{
			{Index: 0, UserIDs: []int{}},
			{Index: 1, UserIDs: []int{3, 1}},
		},
	})
	if err != nil {
		t.Fatalf("reviewReplanRoster() error = %v", err)
	}
	if got := result.Targets[0].UserIDs; !reflect.DeepEqual(got, []int{2}) {
		t.Fatalf("first target roster = %v, want unavailable saved member [2]", got)
	}
	if got := result.Targets[1].UserIDs; !reflect.DeepEqual(got, []int{1, 3}) {
		t.Fatalf("second target roster = %v, want explicit members [1 3]", got)
	}
	if result.Targets[0].TotalCost != 0 || result.Targets[1].TotalCost != 15 {
		t.Fatalf("target costs = %v / %v, want 0 / 15", result.Targets[0].TotalCost, result.Targets[1].TotalCost)
	}
	wantBlockers := []replanRosterBlocker{{UserID: 2, Reason: replanRosterUnavailableIdentity}}
	if !reflect.DeepEqual(result.Blockers, wantBlockers) {
		t.Fatalf("blockers = %v, want %v", result.Blockers, wantBlockers)
	}
}

func TestReviewReplanRosterRemovesOnlyAvailableSavedMembers(t *testing.T) {
	result, err := reviewReplanRoster(replanRosterInput{
		TargetGroupIDs: []int64{101},
		SavedAssignments: map[int]int64{
			1: 101,
			2: 101,
		},
		Members: []replanRosterMember{
			{UserID: 1, Assignable: true},
			{UserID: 2, UnavailableReason: replanRosterUnavailableIdentity},
		},
		RemovedUserIDs: []int{1, 2},
	})
	if err != nil {
		t.Fatalf("reviewReplanRoster() error = %v", err)
	}
	if got := result.Targets[0].UserIDs; !reflect.DeepEqual(got, []int{2}) {
		t.Fatalf("saved roster after removals = %v, want unavailable member [2]", got)
	}
	wantBlockers := []replanRosterBlocker{{UserID: 2, Reason: replanRosterUnavailableIdentity}}
	if !reflect.DeepEqual(result.Blockers, wantBlockers) {
		t.Fatalf("blockers = %v, want %v", result.Blockers, wantBlockers)
	}
}

func TestReviewReplanRosterRejectsInvalidExplicitEdits(t *testing.T) {
	tests := []struct {
		name     string
		members  []replanRosterMember
		reviewed []replanRosterTargetReview
		wantErr  string
	}{
		{
			name:    "duplicate member",
			members: []replanRosterMember{{UserID: 1, Assignable: true}},
			reviewed: []replanRosterTargetReview{
				{Index: 0, UserIDs: []int{1}},
				{Index: 1, UserIDs: []int{1}},
			},
			wantErr: "user 1 is assigned more than once",
		},
		{
			name:     "unknown member",
			members:  []replanRosterMember{},
			reviewed: []replanRosterTargetReview{{Index: 0, UserIDs: []int{9}}, {Index: 1, UserIDs: []int{}}},
			wantErr:  "user 9 cannot be added to a target group",
		},
		{
			name:     "unavailable new member",
			members:  []replanRosterMember{{UserID: 9, UnavailableReason: replanRosterUnavailableIdentity}},
			reviewed: []replanRosterTargetReview{{Index: 0, UserIDs: []int{9}}, {Index: 1, UserIDs: []int{}}},
			wantErr:  "user 9 cannot be added to a target group",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := reviewReplanRoster(replanRosterInput{
				TargetGroupIDs:  []int64{101, 102},
				Members:         tt.members,
				HasReview:       true,
				ReviewedTargets: tt.reviewed,
			})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("reviewReplanRoster() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestReviewReplanRosterCountsObservedTargetOccupancy(t *testing.T) {
	result, err := reviewReplanRoster(replanRosterInput{
		TargetGroupIDs: []int64{101},
		Members: []replanRosterMember{{
			UserID:          3,
			Assignable:      true,
			RangeCost:       5,
			CurrentGroupIDs: []int64{101},
		}},
		UnmanagedCosts: map[int64]float64{101: 3},
	})
	if err != nil {
		t.Fatalf("reviewReplanRoster() error = %v", err)
	}
	if len(result.Targets) != 1 || len(result.Targets[0].UserIDs) != 0 || result.Targets[0].TotalCost != 8 {
		t.Fatalf("observed target = %+v, want no managed members and total cost 8", result.Targets)
	}
}

func TestReviewReplanRosterKeepsMissingSavedMemberDuringReview(t *testing.T) {
	result, err := reviewReplanRoster(replanRosterInput{
		TargetGroupIDs:   []int64{101},
		SavedAssignments: map[int]int64{7: 101},
		HasReview:        true,
		ReviewedTargets:  []replanRosterTargetReview{{Index: 0, UserIDs: []int{7}}},
	})
	if err != nil {
		t.Fatalf("reviewReplanRoster() error = %v", err)
	}
	if got := result.Targets[0].UserIDs; !reflect.DeepEqual(got, []int{7}) {
		t.Fatalf("saved roster = %v, want missing saved member [7]", got)
	}
	wantBlockers := []replanRosterBlocker{{UserID: 7, Reason: replanRosterUnavailableIdentity}}
	if !reflect.DeepEqual(result.Blockers, wantBlockers) {
		t.Fatalf("blockers = %v, want %v", result.Blockers, wantBlockers)
	}
}
