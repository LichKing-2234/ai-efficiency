package relayplanning

import (
	"fmt"
	"sort"
)

type replanRosterInput struct {
	Targets          []replanRosterTargetInput
	SavedAssignments map[int]int64
	Members          []replanRosterMember
	UnmanagedCosts   map[int64]float64
	HasReview        bool
	ReviewedTargets  []replanRosterTargetReview
	RemovedUserIDs   []int
}

type replanRosterTargetInput struct {
	GroupID   int64
	Available bool
}

type replanRosterMember struct {
	UserID            int
	Assignable        bool
	UnavailableReason replanRosterUnavailableReason
	RangeCost         float64
	CurrentGroupIDs   []int64
}

type replanRosterTarget struct {
	Index       int
	GroupID     int64
	UserIDs     []int
	TotalCost   float64
	Unavailable bool
}

type replanRosterTargetReview struct {
	Index   int
	UserIDs []int
}

type replanRosterUnavailableReason uint8

const (
	replanRosterUnavailableIdentity replanRosterUnavailableReason = iota + 1
	replanRosterUnavailableSubscription
	replanRosterMissingTargetSubscription
	replanRosterMismatchedTargetAPIKey
)

type replanRosterBlocker struct {
	UserID int
	Reason replanRosterUnavailableReason
}

type replanRosterMemberError struct {
	UserID int
	Reason replanRosterUnavailableReason
}

func (e *replanRosterMemberError) Error() string {
	return fmt.Sprintf("user %d cannot be added to a target group", e.UserID)
}

type replanRosterResult struct {
	Targets                   []replanRosterTarget
	Blockers                  []replanRosterBlocker
	UnavailableTargetGroupIDs []int64
}

func reviewReplanRoster(input replanRosterInput) (replanRosterResult, error) {
	result := replanRosterResult{Targets: make([]replanRosterTarget, len(input.Targets))}
	targetIndexes := make(map[int64]int, len(input.Targets))
	for index, target := range input.Targets {
		groupID := target.GroupID
		result.Targets[index] = replanRosterTarget{
			Index:       index,
			GroupID:     groupID,
			UserIDs:     []int{},
			TotalCost:   input.UnmanagedCosts[groupID],
			Unavailable: !target.Available,
		}
		targetIndexes[groupID] = index
		if !target.Available {
			result.UnavailableTargetGroupIDs = append(result.UnavailableTargetGroupIDs, groupID)
		}
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
				if _, exists := seenUsers[userID]; exists {
					return replanRosterResult{}, fmt.Errorf("user %d is assigned more than once", userID)
				}
				seenUsers[userID] = struct{}{}
				member, found := members[userID]
				if !found {
					if savedGroupID, saved := input.SavedAssignments[userID]; saved && savedGroupID > 0 {
						continue
					}
					return replanRosterResult{}, &replanRosterMemberError{UserID: userID, Reason: replanRosterUnavailableIdentity}
				}
				if !member.Assignable {
					if savedGroupID, saved := input.SavedAssignments[userID]; saved && member.UnavailableReason != 0 && savedGroupID > 0 {
						continue
					}
					reason := member.UnavailableReason
					if reason == 0 {
						reason = replanRosterUnavailableIdentity
					}
					return replanRosterResult{}, &replanRosterMemberError{UserID: userID, Reason: reason}
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
		blocked := !found || member.UnavailableReason != 0
		if blocked {
			result.Targets[targetIndex].UserIDs = append(result.Targets[targetIndex].UserIDs, userID)
			if found {
				result.Targets[targetIndex].TotalCost += member.RangeCost
			}
			reason := replanRosterUnavailableIdentity
			if found && member.UnavailableReason != 0 {
				reason = member.UnavailableReason
			}
			result.Blockers = append(result.Blockers, replanRosterBlocker{UserID: userID, Reason: reason})
		} else if !input.HasReview {
			result.Targets[targetIndex].UserIDs = append(result.Targets[targetIndex].UserIDs, userID)
			result.Targets[targetIndex].TotalCost += member.RangeCost
		}
	}
	blocked := make(map[int]struct{}, len(result.Blockers))
	for _, blocker := range result.Blockers {
		blocked[blocker.UserID] = struct{}{}
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
