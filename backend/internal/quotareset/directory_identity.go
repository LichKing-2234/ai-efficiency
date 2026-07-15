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

func canonicalDirectoryMember(members ...*ent.DirectoryMember) *ent.DirectoryMember {
	var selected *ent.DirectoryMember
	for _, member := range members {
		if member == nil || member.ID <= 0 {
			continue
		}
		if selected == nil || member.ID < selected.ID {
			selected = member
		}
	}
	return selected
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
