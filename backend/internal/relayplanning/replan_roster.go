package relayplanning

import (
	"fmt"
	"sort"
)

type replanRosterInput struct {
	TargetGroupIDs   []int64
	SavedAssignments map[int]int64
	Members          []replanRosterMember
	UnmanagedCosts   map[int64]float64
	HasReview        bool
	ReviewedTargets  []replanRosterTargetReview
	RemovedUserIDs   []int
}

type replanRosterMember struct {
	UserID            int
	Assignable        bool
	IdentityAvailable bool
	RangeCost         float64
	CurrentGroupIDs   []int64
}

type replanRosterTarget struct {
	Index     int
	GroupID   int64
	UserIDs   []int
	TotalCost float64
}

type replanRosterTargetReview struct {
	Index   int
	UserIDs []int
}

type replanRosterResult struct {
	Targets        []replanRosterTarget
	BlockedUserIDs []int
}

func reviewReplanRoster(input replanRosterInput) (replanRosterResult, error) {
	result := replanRosterResult{Targets: make([]replanRosterTarget, len(input.TargetGroupIDs))}
	targetIndexes := make(map[int64]int, len(input.TargetGroupIDs))
	for index, groupID := range input.TargetGroupIDs {
		result.Targets[index] = replanRosterTarget{
			Index:     index,
			GroupID:   groupID,
			UserIDs:   []int{},
			TotalCost: input.UnmanagedCosts[groupID],
		}
		targetIndexes[groupID] = index
	}

	members := make(map[int]replanRosterMember, len(input.Members))
	for _, member := range input.Members {
		members[member.UserID] = member
	}
	if input.HasReview {
		if len(input.ReviewedTargets) != len(result.Targets) {
			return replanRosterResult{}, fmt.Errorf("assignments must contain exactly %d target groups", len(result.Targets))
		}
		seenTargets := make(map[int]struct{}, len(input.ReviewedTargets))
		seenUsers := make(map[int]struct{})
		for _, reviewed := range input.ReviewedTargets {
			if reviewed.Index < 0 || reviewed.Index >= len(result.Targets) {
				return replanRosterResult{}, fmt.Errorf("assignment index %d is out of range", reviewed.Index)
			}
			if _, exists := seenTargets[reviewed.Index]; exists {
				return replanRosterResult{}, fmt.Errorf("assignment index %d is duplicated", reviewed.Index)
			}
			seenTargets[reviewed.Index] = struct{}{}
			for _, userID := range reviewed.UserIDs {
				member, found := members[userID]
				if !found {
					return replanRosterResult{}, &assignmentCandidateError{UserID: userID, Difference: "Relay user mappings changed"}
				}
				if _, exists := seenUsers[userID]; exists {
					return replanRosterResult{}, fmt.Errorf("user %d is assigned more than once", userID)
				}
				seenUsers[userID] = struct{}{}
				if !member.Assignable {
					if savedGroupID, saved := input.SavedAssignments[userID]; saved && !member.IdentityAvailable && savedGroupID > 0 {
						continue
					}
					difference := "Relay user mappings changed"
					return replanRosterResult{}, &assignmentCandidateError{UserID: userID, Difference: difference}
				}
				result.Targets[reviewed.Index].UserIDs = append(result.Targets[reviewed.Index].UserIDs, userID)
				result.Targets[reviewed.Index].TotalCost += member.RangeCost
			}
		}
		for index := range result.Targets {
			sort.Ints(result.Targets[index].UserIDs)
		}
	}

	userIDs := make([]int, 0, len(input.SavedAssignments))
	for userID := range input.SavedAssignments {
		userIDs = append(userIDs, userID)
	}
	sort.Ints(userIDs)
	for _, userID := range userIDs {
		targetIndex, ok := targetIndexes[input.SavedAssignments[userID]]
		if !ok {
			continue
		}
		member, found := members[userID]
		blocked := !found || !member.IdentityAvailable
		if blocked {
			result.Targets[targetIndex].UserIDs = append(result.Targets[targetIndex].UserIDs, userID)
			if found {
				result.Targets[targetIndex].TotalCost += member.RangeCost
			}
			result.BlockedUserIDs = append(result.BlockedUserIDs, userID)
		} else if !input.HasReview {
			result.Targets[targetIndex].UserIDs = append(result.Targets[targetIndex].UserIDs, userID)
			result.Targets[targetIndex].TotalCost += member.RangeCost
		}
	}
	blocked := make(map[int]struct{}, len(result.BlockedUserIDs))
	for _, userID := range result.BlockedUserIDs {
		blocked[userID] = struct{}{}
	}
	removed := make(map[int]struct{}, len(input.RemovedUserIDs))
	for _, userID := range input.RemovedUserIDs {
		removed[userID] = struct{}{}
	}
	for index := range result.Targets {
		kept := result.Targets[index].UserIDs[:0]
		for _, userID := range result.Targets[index].UserIDs {
			_, remove := removed[userID]
			_, unavailable := blocked[userID]
			if !remove || unavailable {
				kept = append(kept, userID)
			}
		}
		result.Targets[index].UserIDs = kept
	}
	for index := range result.Targets {
		sort.Ints(result.Targets[index].UserIDs)
	}
	if !input.HasReview {
		assigned := make(map[int]struct{})
		for _, target := range result.Targets {
			for _, userID := range target.UserIDs {
				assigned[userID] = struct{}{}
			}
		}
		for _, member := range input.Members {
			if !member.Assignable {
				continue
			}
			if _, found := assigned[member.UserID]; found {
				continue
			}
			for _, groupID := range member.CurrentGroupIDs {
				if targetIndex, found := targetIndexes[groupID]; found {
					result.Targets[targetIndex].TotalCost += member.RangeCost
					break
				}
			}
		}
	}

	return result, nil
}
