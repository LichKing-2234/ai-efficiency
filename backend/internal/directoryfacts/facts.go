package directoryfacts

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

type Facts struct {
	snapshot                    Snapshot
	hierarchy                   *Hierarchy
	members                     []Member
	membershipsByMemberID       map[int][]string
	usersByID                   map[int]*User
	usersByEmail                map[string]*User
	representativeRootsByMember map[string][]string
}

func NewFacts(snapshot Snapshot, departments []Department, members []Member, memberships []Membership, users []User) *Facts {
	sort.SliceStable(members, func(i, j int) bool {
		if members[i].ID != members[j].ID {
			return members[i].ID < members[j].ID
		}
		if members[i].ExternalID != members[j].ExternalID {
			return members[i].ExternalID < members[j].ExternalID
		}
		return members[i].EmailNormalized < members[j].EmailNormalized
	})
	facts := &Facts{
		snapshot:              snapshot,
		hierarchy:             newHierarchy(departments),
		members:               append([]Member(nil), members...),
		membershipsByMemberID: map[int][]string{},
		usersByID:             make(map[int]*User, len(users)),
		usersByEmail:          make(map[string]*User, len(users)),
	}
	for index := range users {
		user := &users[index]
		facts.usersByID[user.ID] = user
		if email := NormalizeEmail(user.Email); email != "" {
			facts.usersByEmail[email] = user
		}
	}
	for _, membership := range memberships {
		facts.membershipsByMemberID[membership.DirectoryMemberID] = append(
			facts.membershipsByMemberID[membership.DirectoryMemberID],
			membership.DepartmentExternalID,
		)
	}
	for memberID, departmentIDs := range facts.membershipsByMemberID {
		facts.membershipsByMemberID[memberID] = compactStrings(departmentIDs)
	}
	facts.representativeRootsByMember = facts.buildRepresentativeRoots()
	return facts
}

func (f *Facts) Snapshot() Snapshot {
	if f == nil {
		return Snapshot{}
	}
	return f.snapshot
}

func (f *Facts) Hierarchy() *Hierarchy {
	if f == nil {
		return nil
	}
	return f.hierarchy
}

func (f *Facts) Departments() []Department {
	if f == nil {
		return nil
	}
	return f.hierarchy.Ordered()
}

func (f *Facts) Members() []Member {
	if f == nil {
		return nil
	}
	return append([]Member(nil), f.members...)
}

func (f *Facts) User(userID int) *User {
	if f == nil {
		return nil
	}
	return f.usersByID[userID]
}

func (f *Facts) UserForMember(member Member) *User {
	users := f.UsersForMember(member)
	if len(users) == 0 {
		return nil
	}
	return users[0]
}

func (f *Facts) UsersForMember(member Member) []*User {
	if f == nil {
		return nil
	}
	users := make([]*User, 0, 2)
	if member.MatchedUserID != nil && *member.MatchedUserID > 0 {
		if user := f.usersByID[*member.MatchedUserID]; user != nil {
			users = append(users, user)
		}
	}
	if emailUser := f.usersByEmail[NormalizeEmail(member.EmailNormalized)]; emailUser != nil {
		if len(users) == 0 || users[0].ID != emailUser.ID {
			users = append(users, emailUser)
		}
	}
	return users
}

func (f *Facts) MemberForUser(userID int) *Member {
	if f == nil || userID <= 0 {
		return nil
	}
	for index := range f.members {
		member := &f.members[index]
		if member.MatchedUserID != nil && *member.MatchedUserID == userID {
			return member
		}
	}
	user := f.usersByID[userID]
	if user == nil {
		return nil
	}
	email := NormalizeEmail(user.Email)
	for index := range f.members {
		if NormalizeEmail(f.members[index].EmailNormalized) == email {
			return &f.members[index]
		}
	}
	return nil
}

func (f *Facts) MemberByExternalID(externalID string) *Member {
	externalID = strings.TrimSpace(externalID)
	if f == nil || externalID == "" {
		return nil
	}
	for index := range f.members {
		if strings.TrimSpace(f.members[index].ExternalID) == externalID {
			return &f.members[index]
		}
	}
	return nil
}

func (f *Facts) DepartmentIDsForMember(member Member) []string {
	if f == nil {
		return nil
	}
	if explicit := f.membershipsByMemberID[member.ID]; len(explicit) > 0 {
		return append([]string(nil), explicit...)
	}
	return compactStrings([]string{member.DepartmentExternalID})
}

func (f *Facts) ExplicitDepartmentIDsForMember(member Member) []string {
	if f == nil {
		return nil
	}
	return append([]string(nil), f.membershipsByMemberID[member.ID]...)
}

func (f *Facts) RepresentativeRoots(memberExternalID string) []string {
	if f == nil {
		return nil
	}
	return append([]string(nil), f.representativeRootsByMember[strings.TrimSpace(memberExternalID)]...)
}

func (f *Facts) RepresentativesByDepartment() map[string][]string {
	out := map[string][]string{}
	if f == nil {
		return out
	}
	for memberExternalID, roots := range f.representativeRootsByMember {
		for _, departmentID := range roots {
			out[departmentID] = append(out[departmentID], memberExternalID)
		}
	}
	for departmentID := range out {
		out[departmentID] = compactStrings(out[departmentID])
	}
	return out
}

func (f *Facts) DepartmentStats(root string) DepartmentStats {
	if f == nil {
		return DepartmentStats{}
	}
	allowed := stringSet(f.hierarchy.SubtreeIDs(root))
	matched := map[int]struct{}{}
	memberCount := 0
	for _, member := range f.members {
		if !f.MemberBelongsToAnyDepartment(member, allowed) {
			continue
		}
		memberCount++
		if user := f.UserForMember(member); user != nil {
			matched[user.ID] = struct{}{}
		}
	}
	return DepartmentStats{MemberCount: memberCount, MatchedUserCount: len(matched)}
}

func (f *Facts) MemberBelongsToAnyDepartment(member Member, allowed map[string]struct{}) bool {
	for _, departmentID := range f.DepartmentIDsForMember(member) {
		if _, ok := allowed[departmentID]; ok {
			return true
		}
	}
	return false
}

func (f *Facts) PreferredDepartmentForUser(userID int) *Department {
	if f == nil || userID <= 0 {
		return nil
	}
	for _, member := range f.members {
		matched := false
		for _, user := range f.UsersForMember(member) {
			if user.ID == userID {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		departmentIDs := f.DepartmentIDsForMember(member)
		primaryID := strings.TrimSpace(member.DepartmentExternalID)
		if primaryID != "" {
			for index, departmentID := range departmentIDs {
				if departmentID == primaryID {
					departmentIDs = append([]string{primaryID}, append(departmentIDs[:index], departmentIDs[index+1:]...)...)
					break
				}
			}
		}
		for _, departmentID := range departmentIDs {
			if department := f.hierarchy.Department(departmentID); department != nil {
				return department
			}
		}
	}
	return nil
}

func (f *Facts) buildRepresentativeRoots() map[string][]string {
	roots := map[string][]string{}
	add := func(memberExternalID, departmentID string) {
		memberExternalID = strings.TrimSpace(memberExternalID)
		departmentID = strings.TrimSpace(departmentID)
		if memberExternalID != "" && departmentID != "" {
			roots[memberExternalID] = append(roots[memberExternalID], departmentID)
		}
	}
	for _, department := range f.Departments() {
		for _, memberExternalID := range MetadataStringValues(department.Metadata[DepartmentRepresentativeIDsKey]) {
			add(memberExternalID, department.ExternalID)
		}
	}
	for _, member := range f.members {
		for _, departmentID := range MetadataStringValues(member.Metadata[MemberLeaderDepartmentIDsKey]) {
			add(member.ExternalID, departmentID)
		}
	}
	for memberExternalID := range roots {
		roots[memberExternalID] = compactStrings(roots[memberExternalID])
	}
	return roots
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func MetadataStringValues(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []string:
		return compactStrings(typed)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			values = append(values, metadataScalarString(item))
		}
		return compactStrings(values)
	case string:
		return compactStrings(strings.Split(typed, ","))
	default:
		return compactStrings([]string{metadataScalarString(typed)})
	}
}

func metadataScalarString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		if math.Trunc(typed) == typed {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strings.TrimSpace(strconv.FormatFloat(typed, 'f', -1, 64))
	case float32:
		value := float64(typed)
		if math.Trunc(value) == value {
			return strconv.FormatInt(int64(value), 10)
		}
		return strings.TrimSpace(strconv.FormatFloat(value, 'f', -1, 32))
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func compactStrings(values []string) []string {
	out := compactStringsPreservingOrder(values)
	sort.Strings(out)
	return out
}

func compactStringsPreservingOrder(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
