package representativescope

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/internal/directorysync"
	"github.com/ai-efficiency/backend/internal/directorytree"
)

const (
	DepartmentRepresentativeIDsKey = "representative_external_ids"
	MemberLeaderDepartmentIDsKey   = "leader_department_ids"
)

type Subject struct {
	SubjectType           string `json:"subject_type"`
	UserID                int    `json:"user_id"`
	DisplayName           string `json:"display_name"`
	Email                 string `json:"email"`
	DepartmentDisplayPath string `json:"department_display_path"`
	RelayUserID           *int   `json:"relay_user_id,omitempty"`
	Selectable            bool   `json:"selectable"`
}

type DepartmentScope struct {
	ExternalID         string `json:"external_id"`
	Name               string `json:"name"`
	DisplayPath        string `json:"display_path"`
	SubtreeMemberCount int    `json:"subtree_member_count"`
	MatchedUserCount   int    `json:"matched_user_count"`
}

type Scope struct {
	ActorUserID              int
	ActorMemberExternalID    string
	IsRepresentative         bool
	RepresentedDepartmentIDs []string
	RepresentedSubtreeIDs    map[string]map[string]struct{}
	Departments              []DepartmentScope
	Subjects                 []Subject
	TargetRepresentedRoots   map[int][]string
}

func (s Scope) AllowedUserIDs() []int {
	seen := make(map[int]struct{}, len(s.Subjects))
	ids := make([]int, 0, len(s.Subjects))
	for _, subject := range s.Subjects {
		if subject.SubjectType != "member" || !subject.Selectable || subject.UserID <= 0 {
			continue
		}
		if _, ok := seen[subject.UserID]; ok {
			continue
		}
		seen[subject.UserID] = struct{}{}
		ids = append(ids, subject.UserID)
	}
	sort.Ints(ids)
	return ids
}

type Service struct {
	client *ent.Client
}

func New(client *ent.Client) *Service {
	return &Service{client: client}
}

func (s *Service) Resolve(ctx context.Context, actorUserID int) (*Scope, error) {
	sourceID, ok, err := directorysync.CurrentSourceID(ctx, s.client)
	if err != nil {
		return nil, fmt.Errorf("resolve current directory source: %w", err)
	}
	if !ok {
		return &Scope{ActorUserID: actorUserID}, nil
	}
	actor, err := s.client.User.Get(ctx, actorUserID)
	if err != nil {
		return nil, fmt.Errorf("get actor user: %w", err)
	}
	members, err := s.client.DirectoryMember.Query().
		Where(directorymember.SourceIDEQ(sourceID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query directory members: %w", err)
	}
	departments, err := s.client.DirectoryDepartment.Query().
		Where(directorydepartment.SourceIDEQ(sourceID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query directory departments: %w", err)
	}
	users, err := s.client.User.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}

	return buildScope(actor, users, members, departments), nil
}

func (s Scope) CanManageTarget(targetUserID int) (bool, string) {
	if targetUserID == s.ActorUserID {
		return false, "self_edit_forbidden"
	}
	if !s.hasMemberSubject(targetUserID) {
		return false, "out_of_scope"
	}
	targetRoots := s.TargetRepresentedRoots[targetUserID]
	if len(targetRoots) == 0 {
		return true, ""
	}
	for _, targetRoot := range targetRoots {
		if !s.strictlyOwnsDepartment(targetRoot) {
			return false, "not_upper_level_representative"
		}
	}
	return true, ""
}

func (s Scope) strictlyOwnsDepartment(targetRoot string) bool {
	targetRoot = strings.TrimSpace(targetRoot)
	if targetRoot == "" {
		return false
	}
	for actorRoot, subtree := range s.RepresentedSubtreeIDs {
		if targetRoot == actorRoot {
			continue
		}
		if _, ok := subtree[targetRoot]; ok {
			return true
		}
	}
	return false
}

func (s Scope) hasMemberSubject(targetUserID int) bool {
	if targetUserID <= 0 {
		return false
	}
	for _, subject := range s.Subjects {
		if subject.SubjectType == "member" && subject.UserID == targetUserID {
			return true
		}
	}
	return false
}

func buildScope(actor *ent.User, users []*ent.User, members []*ent.DirectoryMember, departments []*ent.DirectoryDepartment) *Scope {
	scope := &Scope{
		ActorUserID:            actor.ID,
		RepresentedSubtreeIDs:  map[string]map[string]struct{}{},
		TargetRepresentedRoots: map[int][]string{},
	}

	usersByID, usersByEmail := indexUsers(users)
	actorMember := findActorMember(actor, members)
	if actorMember == nil {
		return scope
	}
	scope.ActorMemberExternalID = strings.TrimSpace(actorMember.ExternalID)

	tree := directorytree.New(departments)
	memberRepresentedRoots := representedRootsByMemberExternalID(departments, members)
	actorRoots := compactStrings(memberRepresentedRoots[scope.ActorMemberExternalID])
	if len(actorRoots) == 0 {
		return scope
	}

	scope.IsRepresentative = true
	scope.RepresentedDepartmentIDs = actorRoots
	allowedDepartments := map[string]struct{}{}
	for _, root := range actorRoots {
		subtreeIDs := tree.SubtreeIDs(root)
		scope.RepresentedSubtreeIDs[root] = stringSet(subtreeIDs)
		for _, departmentID := range subtreeIDs {
			allowedDepartments[departmentID] = struct{}{}
		}
		scope.Departments = append(scope.Departments, departmentScope(root, tree, members, usersByID, usersByEmail))
	}
	sort.Slice(scope.Departments, func(i, j int) bool {
		return scope.Departments[i].DisplayPath < scope.Departments[j].DisplayPath
	})

	scope.Subjects = buildSubjects(actor.ID, members, usersByID, usersByEmail, allowedDepartments, tree)
	scope.TargetRepresentedRoots = buildTargetRepresentedRoots(members, usersByID, usersByEmail, memberRepresentedRoots)
	return scope
}

func indexUsers(users []*ent.User) (map[int]*ent.User, map[string]*ent.User) {
	byID := make(map[int]*ent.User, len(users))
	byEmail := make(map[string]*ent.User, len(users))
	for _, user := range users {
		if user == nil {
			continue
		}
		byID[user.ID] = user
		email := normalizeEmail(user.Email)
		if email != "" {
			byEmail[email] = user
		}
	}
	return byID, byEmail
}

func findActorMember(actor *ent.User, members []*ent.DirectoryMember) *ent.DirectoryMember {
	for _, member := range members {
		if member == nil || member.MatchedUserID == nil {
			continue
		}
		if *member.MatchedUserID == actor.ID {
			return member
		}
	}
	actorEmail := normalizeEmail(actor.Email)
	for _, member := range members {
		if member == nil {
			continue
		}
		if normalizeEmail(member.EmailNormalized) == actorEmail {
			return member
		}
	}
	return nil
}

func representedRootsByMemberExternalID(departments []*ent.DirectoryDepartment, members []*ent.DirectoryMember) map[string][]string {
	roots := map[string][]string{}
	add := func(memberExternalID, departmentID string) {
		memberExternalID = strings.TrimSpace(memberExternalID)
		departmentID = strings.TrimSpace(departmentID)
		if memberExternalID == "" || departmentID == "" {
			return
		}
		roots[memberExternalID] = append(roots[memberExternalID], departmentID)
	}

	for _, department := range departments {
		if department == nil {
			continue
		}
		for _, memberExternalID := range metadataStringValues(department.Metadata[DepartmentRepresentativeIDsKey]) {
			add(memberExternalID, department.ExternalID)
		}
	}
	for _, member := range members {
		if member == nil {
			continue
		}
		for _, departmentID := range metadataStringValues(member.Metadata[MemberLeaderDepartmentIDsKey]) {
			add(member.ExternalID, departmentID)
		}
	}
	for memberExternalID, departmentIDs := range roots {
		roots[memberExternalID] = compactStrings(departmentIDs)
	}
	return roots
}

func departmentScope(root string, tree *directorytree.Tree, members []*ent.DirectoryMember, usersByID map[int]*ent.User, usersByEmail map[string]*ent.User) DepartmentScope {
	subtreeIDs := stringSet(tree.SubtreeIDs(root))
	memberCount := 0
	matchedUserIDs := map[int]struct{}{}
	for _, member := range members {
		if member == nil {
			continue
		}
		if _, ok := subtreeIDs[member.DepartmentExternalID]; !ok {
			continue
		}
		memberCount++
		if user := resolveMemberUser(member, usersByID, usersByEmail); user != nil {
			matchedUserIDs[user.ID] = struct{}{}
		}
	}
	return DepartmentScope{
		ExternalID:         root,
		Name:               departmentName(root, tree),
		DisplayPath:        tree.DisplayPath(root),
		SubtreeMemberCount: memberCount,
		MatchedUserCount:   len(matchedUserIDs),
	}
}

func departmentName(root string, tree *directorytree.Tree) string {
	displayPath := tree.DisplayPath(root)
	if displayPath == "" {
		return root
	}
	parts := strings.Split(displayPath, " / ")
	name := strings.TrimSpace(parts[len(parts)-1])
	if name == "" {
		return root
	}
	return name
}

func buildSubjects(actorUserID int, members []*ent.DirectoryMember, usersByID map[int]*ent.User, usersByEmail map[string]*ent.User, allowedDepartments map[string]struct{}, tree *directorytree.Tree) []Subject {
	subjectByUserID := map[int]Subject{}
	for _, member := range members {
		if member == nil {
			continue
		}
		if _, ok := allowedDepartments[member.DepartmentExternalID]; !ok {
			continue
		}
		localUser := resolveMemberUser(member, usersByID, usersByEmail)
		if localUser == nil || localUser.ID == actorUserID {
			continue
		}
		subjectByUserID[localUser.ID] = Subject{
			SubjectType:           "member",
			UserID:                localUser.ID,
			DisplayName:           subjectDisplayName(localUser, member),
			Email:                 localUser.Email,
			DepartmentDisplayPath: tree.DisplayPath(member.DepartmentExternalID),
			RelayUserID:           localUser.RelayUserID,
			Selectable:            localUser.RelayUserID != nil,
		}
	}
	subjects := make([]Subject, 0, len(subjectByUserID))
	for _, subject := range subjectByUserID {
		subjects = append(subjects, subject)
	}
	sort.Slice(subjects, func(i, j int) bool {
		if subjects[i].DisplayName != subjects[j].DisplayName {
			return subjects[i].DisplayName < subjects[j].DisplayName
		}
		return subjects[i].UserID < subjects[j].UserID
	})
	return subjects
}

func buildTargetRepresentedRoots(members []*ent.DirectoryMember, usersByID map[int]*ent.User, usersByEmail map[string]*ent.User, memberRepresentedRoots map[string][]string) map[int][]string {
	out := map[int][]string{}
	for _, member := range members {
		if member == nil {
			continue
		}
		roots := memberRepresentedRoots[strings.TrimSpace(member.ExternalID)]
		if len(roots) == 0 {
			continue
		}
		localUser := resolveMemberUser(member, usersByID, usersByEmail)
		if localUser == nil {
			continue
		}
		out[localUser.ID] = compactStrings(append(out[localUser.ID], roots...))
	}
	return out
}

func resolveMemberUser(member *ent.DirectoryMember, usersByID map[int]*ent.User, usersByEmail map[string]*ent.User) *ent.User {
	if member == nil {
		return nil
	}
	if member.MatchedUserID != nil && *member.MatchedUserID > 0 {
		if user := usersByID[*member.MatchedUserID]; user != nil {
			return user
		}
	}
	return usersByEmail[normalizeEmail(member.EmailNormalized)]
}

func subjectDisplayName(localUser *ent.User, member *ent.DirectoryMember) string {
	if localUser != nil {
		if username := strings.TrimSpace(localUser.Username); username != "" {
			return username
		}
		if email := strings.TrimSpace(localUser.Email); email != "" {
			return email
		}
	}
	if member != nil {
		if name := strings.TrimSpace(member.DisplayName); name != "" {
			return name
		}
		if email := strings.TrimSpace(member.EmailNormalized); email != "" {
			return email
		}
	}
	return ""
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out[value] = struct{}{}
	}
	return out
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func metadataStringValues(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []string:
		return compactStrings(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, strings.TrimSpace(fmt.Sprint(item)))
		}
		return compactStrings(out)
	case string:
		return compactStrings(strings.Split(typed, ","))
	default:
		return compactStrings([]string{fmt.Sprint(typed)})
	}
}

func compactStrings(values []string) []string {
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
	sort.Strings(out)
	return out
}
