package representativescope

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/directoryfacts"
)

type Subject struct {
	SubjectType               string   `json:"subject_type"`
	UserID                    int      `json:"user_id"`
	DirectoryMemberExternalID string   `json:"directory_member_external_id,omitempty"`
	DisplayName               string   `json:"display_name"`
	Email                     string   `json:"email"`
	DepartmentExternalID      string   `json:"department_external_id,omitempty"`
	DepartmentExternalIDs     []string `json:"department_external_ids,omitempty"`
	DepartmentDisplayPath     string   `json:"department_display_path"`
	RelayUserID               *int     `json:"relay_user_id,omitempty"`
	Selectable                bool     `json:"selectable"`
}

type DepartmentScope struct {
	ExternalID         string  `json:"external_id"`
	ParentExternalID   *string `json:"parent_external_id,omitempty"`
	Name               string  `json:"name"`
	DisplayPath        string  `json:"display_path"`
	Depth              int     `json:"depth"`
	ChildCount         int     `json:"child_count"`
	SubtreeMemberCount int     `json:"subtree_member_count"`
	MatchedUserCount   int     `json:"matched_user_count"`
}

type Scope struct {
	Version                  string                         `json:"version"`
	DirectorySourceID        int                            `json:"directory_source_id"`
	DirectoryRunID           int                            `json:"directory_run_id"`
	ActorUserID              int                            `json:"actor_user_id"`
	ActorMemberExternalID    string                         `json:"actor_member_external_id"`
	IsRepresentative         bool                           `json:"is_representative"`
	RepresentedDepartmentIDs []string                       `json:"represented_department_ids"`
	RepresentedSubtreeIDs    map[string]map[string]struct{} `json:"represented_subtree_ids"`
	Departments              []DepartmentScope              `json:"departments"`
	MemberTreeRootIDs        []string                       `json:"member_tree_root_ids"`
	MemberTreeDepartments    []DepartmentScope              `json:"member_tree_departments"`
	Subjects                 []Subject                      `json:"subjects"`
	OverviewSubjects         []Subject                      `json:"overview_subjects"`
	TargetRepresentedRoots   map[int][]string               `json:"target_represented_roots"`
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
	cache  *Cache
	facts  directoryfacts.Reader
}

func New(client *ent.Client) *Service {
	return &Service{client: client, facts: directoryfacts.New(client)}
}

func NewWithCache(client *ent.Client, cache *Cache) *Service {
	return &Service{client: client, cache: cache, facts: directoryfacts.New(client)}
}

func (s *Service) Resolve(ctx context.Context, actorUserID int) (*Scope, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("representative scope service is not configured")
	}
	if actorUserID <= 0 {
		return nil, fmt.Errorf("representative scope actor ID must be positive")
	}
	for {
		guard, actor, view, ok, err := s.currentGuard(ctx, actorUserID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return &Scope{ActorUserID: actorUserID}, nil
		}

		loader := func(loadCtx context.Context) (*Scope, error) {
			return s.loadAuthoritativeScope(loadCtx, view, actor)
		}
		var scope *Scope
		if s.cache == nil {
			scope, err = loader(ctx)
			if err == nil && scope != nil {
				scope.Version = scopeVersion(guard)
			}
		} else {
			scope, err = s.cache.GetOrLoad(ctx, guard, loader)
		}
		if err != nil {
			return nil, err
		}

		currentGuard, _, _, current, err := s.currentGuard(ctx, actorUserID)
		if err != nil {
			return nil, err
		}
		if !current {
			return &Scope{ActorUserID: actorUserID}, nil
		}
		if currentGuard == guard {
			return scope, nil
		}
	}
}

func (s *Service) currentGuard(ctx context.Context, actorUserID int) (scopeGuard, *ent.User, directoryfacts.View, bool, error) {
	actor, err := s.client.User.Get(ctx, actorUserID)
	if err != nil {
		return scopeGuard{}, nil, nil, false, fmt.Errorf("get actor user: %w", err)
	}
	view, ok, err := s.facts.Current(ctx)
	if err != nil {
		return scopeGuard{}, nil, nil, false, fmt.Errorf("resolve current directory snapshot: %w", err)
	}
	if !ok {
		return scopeGuard{}, actor, nil, false, nil
	}
	snapshot := view.Snapshot()
	return scopeGuard{
		ActorUserID:       actor.ID,
		ActorRole:         string(actor.Role),
		DirectorySourceID: snapshot.SourceID,
		DirectoryRunID:    snapshot.RunID,
	}, actor, view, true, nil
}

func (s *Service) loadAuthoritativeScope(ctx context.Context, view directoryfacts.View, actor *ent.User) (*Scope, error) {
	if actor == nil {
		return nil, fmt.Errorf("representative scope actor is required")
	}
	facts, err := view.Load(ctx, directoryfacts.Query{
		AllDepartments:     true,
		AllMembers:         true,
		IncludeMemberships: true,
		AllUsers:           true,
	})
	if err != nil {
		return nil, fmt.Errorf("load current representative directory facts: %w", err)
	}
	return buildScope(actor, facts), nil
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

func buildScope(actor *ent.User, facts *directoryfacts.Facts) *Scope {
	snapshot := facts.Snapshot()
	scope := &Scope{
		DirectorySourceID:      snapshot.SourceID,
		DirectoryRunID:         snapshot.RunID,
		ActorUserID:            actor.ID,
		RepresentedSubtreeIDs:  map[string]map[string]struct{}{},
		TargetRepresentedRoots: map[int][]string{},
	}
	actorMember := facts.MemberForUser(actor.ID)
	if actorMember == nil {
		return scope
	}
	scope.ActorMemberExternalID = strings.TrimSpace(actorMember.ExternalID)

	tree := facts.Hierarchy()
	actorRoots := compactStrings(facts.RepresentativeRoots(scope.ActorMemberExternalID))
	if len(actorRoots) == 0 {
		return scope
	}

	scope.IsRepresentative = true
	scope.RepresentedDepartmentIDs = actorRoots
	scope.MemberTreeRootIDs = largestRepresentedRoots(actorRoots, tree)
	allowedDepartments := map[string]struct{}{}
	for _, root := range scope.MemberTreeRootIDs {
		subtreeIDs := tree.SubtreeIDs(root)
		scope.RepresentedSubtreeIDs[root] = stringSet(subtreeIDs)
		for _, departmentID := range subtreeIDs {
			allowedDepartments[departmentID] = struct{}{}
		}
		scope.Departments = append(scope.Departments, departmentScope(root, facts))
	}
	scope.MemberTreeDepartments = memberTreeDepartments(scope.MemberTreeRootIDs, facts)
	sort.Slice(scope.Departments, func(i, j int) bool {
		return scope.Departments[i].DisplayPath < scope.Departments[j].DisplayPath
	})

	scope.Subjects = buildSubjects(actor.ID, facts, allowedDepartments, false)
	scope.OverviewSubjects = buildSubjects(actor.ID, facts, allowedDepartments, true)
	scope.TargetRepresentedRoots = buildTargetRepresentedRoots(facts)
	return scope
}

func largestRepresentedRoots(roots []string, tree *directoryfacts.Hierarchy) []string {
	rootSet := stringSet(roots)
	largest := make([]string, 0, len(roots))
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		containedByAnotherRoot := false
		for otherRoot := range rootSet {
			if otherRoot == root {
				continue
			}
			for _, subtreeID := range tree.SubtreeIDs(otherRoot) {
				if subtreeID == root {
					containedByAnotherRoot = true
					break
				}
			}
			if containedByAnotherRoot {
				break
			}
		}
		if !containedByAnotherRoot {
			largest = append(largest, root)
		}
	}
	return compactStrings(largest)
}

func memberTreeDepartments(roots []string, facts *directoryfacts.Facts) []DepartmentScope {
	tree := facts.Hierarchy()
	allowed := map[string]struct{}{}
	for _, root := range roots {
		for _, departmentID := range tree.SubtreeIDs(root) {
			allowed[departmentID] = struct{}{}
		}
	}
	out := make([]DepartmentScope, 0, len(allowed))
	for _, department := range tree.Ordered() {
		if _, ok := allowed[department.ExternalID]; !ok {
			continue
		}
		out = append(out, departmentScope(department.ExternalID, facts))
	}
	return out
}

func departmentScope(root string, facts *directoryfacts.Facts) DepartmentScope {
	tree := facts.Hierarchy()
	stats := facts.DepartmentStats(root)
	return DepartmentScope{
		ExternalID:         root,
		ParentExternalID:   departmentParentExternalID(root, tree),
		Name:               departmentName(root, tree),
		DisplayPath:        tree.DisplayPath(root),
		Depth:              tree.Depth(root),
		ChildCount:         tree.ChildCount(root),
		SubtreeMemberCount: stats.MemberCount,
		MatchedUserCount:   stats.MatchedUserCount,
	}
}

func departmentParentExternalID(root string, tree *directoryfacts.Hierarchy) *string {
	if tree == nil {
		return nil
	}
	parent := tree.ParentID(root)
	if parent == "" {
		return nil
	}
	return &parent
}

func departmentName(root string, tree *directoryfacts.Hierarchy) string {
	if tree != nil {
		if department := tree.Department(root); department != nil {
			if name := strings.TrimSpace(department.Name); name != "" {
				return name
			}
		}
	}
	return root
}

func buildSubjects(actorUserID int, facts *directoryfacts.Facts, allowedDepartments map[string]struct{}, includeActor bool) []Subject {
	tree := facts.Hierarchy()
	subjectsByKey := map[string]Subject{}
	for _, member := range facts.Members() {
		departmentIDs := filterAllowedMemberDepartments(facts.DepartmentIDsForMember(member), allowedDepartments)
		if len(departmentIDs) == 0 {
			continue
		}
		localUser := facts.UserForMember(member)
		isActor := localUser != nil && localUser.ID == actorUserID
		if isActor && !includeActor {
			continue
		}
		subject := Subject{
			SubjectType:               "member",
			DirectoryMemberExternalID: strings.TrimSpace(member.ExternalID),
			DisplayName:               subjectDisplayName(localUser, member),
			Email:                     subjectEmail(localUser, member),
			DepartmentExternalID:      departmentIDs[0],
			DepartmentExternalIDs:     departmentIDs,
			DepartmentDisplayPath:     tree.DisplayPath(departmentIDs[0]),
		}
		if localUser != nil {
			subject.UserID = localUser.ID
			subject.RelayUserID = localUser.RelayUserID
			subject.Selectable = !isActor && localUser.RelayUserID != nil
		}
		key := subjectIdentityKey(localUser, &member)
		if existing, ok := subjectsByKey[key]; ok {
			existing.DepartmentExternalIDs = appendUniqueStrings(existing.DepartmentExternalIDs, subject.DepartmentExternalIDs...)
			if existing.DepartmentExternalID == "" && len(existing.DepartmentExternalIDs) > 0 {
				existing.DepartmentExternalID = existing.DepartmentExternalIDs[0]
				existing.DepartmentDisplayPath = tree.DisplayPath(existing.DepartmentExternalID)
			}
			subjectsByKey[key] = existing
			continue
		}
		subjectsByKey[key] = subject
	}
	subjects := make([]Subject, 0, len(subjectsByKey))
	for _, subject := range subjectsByKey {
		subjects = append(subjects, subject)
	}
	sort.Slice(subjects, func(i, j int) bool {
		if subjects[i].DisplayName != subjects[j].DisplayName {
			return subjects[i].DisplayName < subjects[j].DisplayName
		}
		if subjects[i].UserID != subjects[j].UserID {
			return subjects[i].UserID < subjects[j].UserID
		}
		if subjects[i].DirectoryMemberExternalID != subjects[j].DirectoryMemberExternalID {
			return subjects[i].DirectoryMemberExternalID < subjects[j].DirectoryMemberExternalID
		}
		return subjects[i].Email < subjects[j].Email
	})
	return subjects
}

func subjectIdentityKey(localUser *directoryfacts.User, member *directoryfacts.Member) string {
	if localUser != nil {
		return fmt.Sprintf("user:%d", localUser.ID)
	}
	if member == nil {
		return ""
	}
	externalID := strings.TrimSpace(member.ExternalID)
	if externalID != "" {
		return "directory:" + externalID
	}
	email := directoryfacts.NormalizeEmail(member.EmailNormalized)
	if email != "" {
		return "email:" + email
	}
	return fmt.Sprintf("member:%d", member.ID)
}

func filterAllowedMemberDepartments(departmentIDs []string, allowedDepartments map[string]struct{}) []string {
	out := make([]string, 0, len(departmentIDs))
	for _, departmentID := range departmentIDs {
		departmentID = strings.TrimSpace(departmentID)
		if departmentID == "" {
			continue
		}
		if _, ok := allowedDepartments[departmentID]; !ok {
			continue
		}
		out = appendUniqueStrings(out, departmentID)
	}
	return out
}

func buildTargetRepresentedRoots(facts *directoryfacts.Facts) map[int][]string {
	out := map[int][]string{}
	for _, member := range facts.Members() {
		roots := facts.RepresentativeRoots(member.ExternalID)
		if len(roots) == 0 {
			continue
		}
		localUser := facts.UserForMember(member)
		if localUser == nil {
			continue
		}
		out[localUser.ID] = compactStrings(append(out[localUser.ID], roots...))
	}
	return out
}

func subjectDisplayName(localUser *directoryfacts.User, member directoryfacts.Member) string {
	if name := strings.TrimSpace(member.DisplayName); name != "" {
		return name
	}
	if localUser != nil {
		if username := strings.TrimSpace(localUser.Username); username != "" {
			return username
		}
		if email := strings.TrimSpace(localUser.Email); email != "" {
			return email
		}
	}
	if email := strings.TrimSpace(member.EmailNormalized); email != "" {
		return email
	}
	return ""
}

func subjectEmail(localUser *directoryfacts.User, member directoryfacts.Member) string {
	if localUser != nil {
		if email := strings.TrimSpace(localUser.Email); email != "" {
			return email
		}
	}
	return strings.TrimSpace(member.EmailNormalized)
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

func appendUniqueStrings(current []string, values ...string) []string {
	seen := make(map[string]struct{}, len(current)+len(values))
	out := make([]string, 0, len(current)+len(values))
	for _, value := range current {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
