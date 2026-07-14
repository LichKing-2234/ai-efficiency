package quotareset

import (
	"strings"

	"github.com/ai-efficiency/backend/ent"
)

func directoryMemberIsActive(member *ent.DirectoryMember) bool {
	return member != nil && strings.EqualFold(strings.TrimSpace(member.Status), "active")
}

func localUserHasCurrentAccess(user *ent.User) bool {
	return user != nil &&
		user.RelayDisabledAt == nil &&
		user.TokenValidAfter == nil
}

func directoryApproverIsCurrentlyUsable(user *ent.User, member *ent.DirectoryMember) bool {
	return directoryMemberIsActive(member) && localUserHasCurrentAccess(user)
}

func directoryMemberUser(member *ent.DirectoryMember, usersByID map[int]*ent.User, usersByEmail map[string]*ent.User) *ent.User {
	if member == nil {
		return nil
	}
	if member.MatchedUserID != nil {
		return usersByID[*member.MatchedUserID]
	}
	if usersByEmail == nil {
		return nil
	}
	return usersByEmail[strings.ToLower(strings.TrimSpace(member.EmailNormalized))]
}

// Keep existing workflow-summary callers on the shared directory identity semantics.
func workflowMemberIsActive(member *ent.DirectoryMember) bool {
	return directoryMemberIsActive(member)
}

func workflowApproverIsCurrentlyUsable(user *ent.User, member *ent.DirectoryMember) bool {
	return directoryApproverIsCurrentlyUsable(user, member)
}

func workflowMemberUser(member *ent.DirectoryMember, usersByID map[int]*ent.User, usersByEmail map[string]*ent.User) *ent.User {
	return directoryMemberUser(member, usersByID, usersByEmail)
}
