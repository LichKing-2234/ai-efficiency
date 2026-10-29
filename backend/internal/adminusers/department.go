package adminusers

import (
	"context"
	"fmt"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/directoryfacts"
)

type DepartmentOptionRequest struct {
	Query      string
	SelectedID string
	Page       int
	PageSize   int
}

type DepartmentOption struct {
	ExternalID  string `json:"external_id"`
	Name        string `json:"name"`
	DisplayPath string `json:"display_path"`
}

type DepartmentOptionPage struct {
	Items    []DepartmentOption
	Selected *DepartmentOption
	Total    int
	Page     int
	PageSize int
}

type DepartmentChildrenRequest struct {
	ParentDepartmentID string
	Page               int
	PageSize           int
}

type DepartmentSummary struct {
	ExternalID                 string
	ParentExternalID           *string
	Name                       string
	Path                       string
	DisplayPath                string
	Depth                      int
	ChildCount                 int
	HasChildren                bool
	MemberCount                int
	MatchedUserCount           int
	SubtreeMemberCount         int
	SubtreeMatchedUserCount    int
	RepresentativeCount        int
	MatchedRepresentativeCount int
}

type DepartmentChildrenPage struct {
	Items              []DepartmentSummary
	ParentDepartmentID string
	Total              int
	Page               int
	PageSize           int
}

// DepartmentsForUsers resolves the current directory departments for the
// supplied users in one batch. It is intended for list pages outside this
// package that need the full department projection rather than the single
// chosen department of the admin users list.
//
// The result is keyed by user ID and each value holds that user's current
// departments in selection order: the current primary department first when it
// is among the current memberships, then the remaining current memberships
// ordered by department external id. A user matched by several directory
// member rows gets their union, deduplicated by department external id.
//
// Users without a resolvable current department are absent from the map, and
// the map is empty when there is no current directory source or no membership
// matches. Dangling departments are skipped rather than reported as errors,
// matching the admin users list. The returned departments are shared with the
// list projection and must be treated as read-only by callers.
func (s *Service) DepartmentsForUsers(ctx context.Context, users []*ent.User) (map[int][]*Department, error) {
	source, err := s.currentSource(ctx)
	if err != nil {
		return nil, err
	}
	return s.departmentProjectionsForPage(ctx, source, users)
}

func (s *Service) departmentsForPage(ctx context.Context, source resolvedSource, users []*ent.User) (map[int]*Department, error) {
	projections, err := s.departmentProjectionsForPage(ctx, source, users)
	if err != nil {
		return nil, err
	}
	out := make(map[int]*Department, len(projections))
	for userID, departments := range projections {
		if len(departments) == 0 || departments[0] == nil {
			continue
		}
		out[userID] = departments[0]
	}
	return out, nil
}

func (s *Service) departmentProjectionsForPage(ctx context.Context, source resolvedSource, users []*ent.User) (map[int][]*Department, error) {
	out := make(map[int][]*Department, len(users))
	if !source.found || len(users) == 0 {
		return out, nil
	}
	userIDs := make([]int, 0, len(users))
	emails := make([]string, 0, len(users))
	knownUsers := make([]directoryfacts.User, 0, len(users))
	pageUserIDs := make(map[int]struct{}, len(users))
	userIDsByEmail := make(map[string][]int, len(users))
	for _, user := range users {
		if user == nil {
			continue
		}
		userIDs = append(userIDs, user.ID)
		emails = append(emails, user.Email)
		pageUserIDs[user.ID] = struct{}{}
		knownUsers = append(knownUsers, directoryfacts.User{ID: user.ID, Username: user.Username, Email: user.Email})
		if email := directoryfacts.NormalizeEmail(user.Email); email != "" {
			userIDsByEmail[email] = append(userIDsByEmail[email], user.ID)
		}
	}
	facts, err := source.view.Load(ctx, directoryfacts.Query{
		MemberUserIDs:              userIDs,
		MemberEmails:               emails,
		IncludeMemberships:         true,
		KnownUsers:                 knownUsers,
		IncludeDepartmentAncestors: true,
	})
	if err != nil {
		return nil, fmt.Errorf("load directory facts for admin user page: %w", err)
	}
	departmentsByExternalID := map[string]*Department{}
	resolveDepartment := func(externalID string) *Department {
		externalID = strings.TrimSpace(externalID)
		if externalID == "" {
			return nil
		}
		if cached := departmentsByExternalID[externalID]; cached != nil {
			return cached
		}
		department := facts.Hierarchy().Department(externalID)
		if department == nil {
			return nil
		}
		resolved := &Department{
			ExternalID:  department.ExternalID,
			Name:        department.Name,
			Path:        department.Path,
			DisplayPath: facts.Hierarchy().DisplayPath(externalID),
		}
		departmentsByExternalID[externalID] = resolved
		return resolved
	}
	for _, member := range facts.Members() {
		departments := make([]*Department, 0, 1)
		for _, externalID := range memberDepartmentSelectionOrder(facts, member) {
			if department := resolveDepartment(externalID); department != nil {
				departments = append(departments, department)
			}
		}
		if len(departments) == 0 {
			continue
		}
		matchingUserIDs := append([]int(nil), userIDsByEmail[directoryfacts.NormalizeEmail(member.EmailNormalized)]...)
		if member.MatchedUserID != nil {
			matchingUserIDs = append(matchingUserIDs, *member.MatchedUserID)
		}
		seenUserIDs := make(map[int]struct{}, len(matchingUserIDs))
		for _, userID := range matchingUserIDs {
			if _, onPage := pageUserIDs[userID]; !onPage {
				continue
			}
			if _, duplicate := seenUserIDs[userID]; duplicate {
				continue
			}
			seenUserIDs[userID] = struct{}{}
			out[userID] = appendUniqueDepartments(out[userID], departments...)
		}
	}
	return out, nil
}

// memberDepartmentSelectionOrder lists the current departments of one directory
// member in display selection order: the current primary department first when
// it is among the current memberships, then the remaining memberships ordered
// by department external id. A member without current memberships falls back to
// the legacy primary department field.
func memberDepartmentSelectionOrder(facts *directoryfacts.Facts, member directoryfacts.Member) []string {
	primaryID := strings.TrimSpace(member.DepartmentExternalID)
	membershipIDs := facts.ExplicitDepartmentIDsForMember(member)
	if len(membershipIDs) == 0 {
		if primaryID == "" {
			return nil
		}
		return []string{primaryID}
	}
	selection := make([]string, 0, len(membershipIDs))
	if primaryID != "" {
		for _, membershipID := range membershipIDs {
			if strings.TrimSpace(membershipID) == primaryID {
				selection = append(selection, primaryID)
				break
			}
		}
	}
	return appendUniqueDepartmentIDs(selection, membershipIDs...)
}

func appendUniqueDepartments(current []*Department, values ...*Department) []*Department {
	seen := make(map[string]struct{}, len(current)+len(values))
	for _, department := range current {
		if department == nil {
			continue
		}
		seen[department.ExternalID] = struct{}{}
	}
	for _, department := range values {
		if department == nil {
			continue
		}
		if _, ok := seen[department.ExternalID]; ok {
			continue
		}
		seen[department.ExternalID] = struct{}{}
		current = append(current, department)
	}
	return current
}

func appendUniqueDepartmentIDs(current []string, values ...string) []string {
	seen := make(map[string]struct{}, len(current)+len(values))
	for _, value := range current {
		seen[value] = struct{}{}
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		current = append(current, value)
	}
	return current
}

type departmentPresentation struct {
	department *Department
	depth      int
}

func (s *Service) loadDepartmentPresentations(ctx context.Context, source resolvedSource, candidateIDs []string) (map[string]*departmentPresentation, error) {
	out := make(map[string]*departmentPresentation, len(candidateIDs))
	if !source.found || len(candidateIDs) == 0 {
		return out, nil
	}
	facts, err := source.view.Load(ctx, directoryfacts.Query{
		DepartmentIDs:              candidateIDs,
		IncludeDepartmentAncestors: true,
	})
	if err != nil {
		return nil, fmt.Errorf("load candidate directory department facts: %w", err)
	}
	for _, externalID := range appendUniqueDepartmentIDs(nil, candidateIDs...) {
		department := facts.Hierarchy().Department(externalID)
		if department == nil {
			continue
		}
		out[externalID] = &departmentPresentation{
			department: &Department{
				ExternalID:  externalID,
				Name:        department.Name,
				Path:        department.Path,
				DisplayPath: facts.Hierarchy().DisplayPath(externalID),
			},
			depth: facts.Hierarchy().Depth(externalID),
		}
	}
	return out, nil
}

type departmentAggregateRow struct {
	ExternalID                 string `json:"external_id"`
	ChildCount                 int    `json:"child_count"`
	MemberCount                int    `json:"member_count"`
	MatchedUserCount           int    `json:"matched_user_count"`
	SubtreeMemberCount         int    `json:"subtree_member_count"`
	SubtreeMatchedUserCount    int    `json:"subtree_matched_user_count"`
	RepresentativeCount        int    `json:"representative_count"`
	MatchedRepresentativeCount int    `json:"matched_representative_count"`
}

func (s *Service) DepartmentOptions(ctx context.Context, request DepartmentOptionRequest) (*DepartmentOptionPage, error) {
	request = normalizeDepartmentOptionRequest(request)
	page := &DepartmentOptionPage{
		Items:    []DepartmentOption{},
		Page:     request.Page,
		PageSize: request.PageSize,
	}
	source, err := s.currentSource(ctx)
	if err != nil {
		return nil, err
	}
	if !source.found {
		return page, nil
	}

	offset := (request.Page - 1) * request.PageSize
	directoryPage, err := source.view.DepartmentPage(ctx, directoryfacts.DepartmentPageQuery{
		Search: request.Query,
		Offset: offset,
		Limit:  request.PageSize,
	})
	if err != nil {
		return nil, err
	}
	page.Total = directoryPage.Total

	candidateIDs := make([]string, 0, len(directoryPage.Items)+1)
	for _, row := range directoryPage.Items {
		candidateIDs = append(candidateIDs, row.ExternalID)
	}
	if request.SelectedID != "" {
		candidateIDs = appendUniqueDepartmentIDs(candidateIDs, request.SelectedID)
	}
	presentations, err := s.loadDepartmentPresentations(ctx, source, candidateIDs)
	if err != nil {
		return nil, err
	}
	for _, row := range directoryPage.Items {
		presentation := presentations[row.ExternalID]
		if presentation == nil {
			continue
		}
		page.Items = append(page.Items, DepartmentOption{
			ExternalID:  row.ExternalID,
			Name:        row.Name,
			DisplayPath: presentation.department.DisplayPath,
		})
	}
	if request.SelectedID != "" {
		if presentation := presentations[request.SelectedID]; presentation != nil {
			page.Selected = &DepartmentOption{
				ExternalID:  presentation.department.ExternalID,
				Name:        presentation.department.Name,
				DisplayPath: presentation.department.DisplayPath,
			}
		}
	}
	return page, nil
}

func (s *Service) DepartmentChildren(ctx context.Context, request DepartmentChildrenRequest) (*DepartmentChildrenPage, error) {
	request = normalizeDepartmentChildrenRequest(request)
	page := &DepartmentChildrenPage{
		Items:              []DepartmentSummary{},
		ParentDepartmentID: request.ParentDepartmentID,
		Page:               request.Page,
		PageSize:           request.PageSize,
	}
	source, err := s.currentSource(ctx)
	if err != nil {
		return nil, err
	}
	if !source.found {
		return page, nil
	}

	offset := (request.Page - 1) * request.PageSize
	parentID := request.ParentDepartmentID
	directoryPage, err := source.view.DepartmentPage(ctx, directoryfacts.DepartmentPageQuery{
		ParentID: &parentID,
		Offset:   offset,
		Limit:    request.PageSize,
	})
	if err != nil {
		return nil, err
	}
	page.Total = directoryPage.Total
	if len(directoryPage.Items) == 0 {
		return page, nil
	}

	candidateIDs := make([]string, 0, len(directoryPage.Items))
	for _, candidate := range directoryPage.Items {
		candidateIDs = append(candidateIDs, candidate.ExternalID)
	}
	presentations, err := s.loadDepartmentPresentations(ctx, source, candidateIDs)
	if err != nil {
		return nil, err
	}
	aggregates, err := s.departmentAggregates(ctx, source, candidateIDs)
	if err != nil {
		return nil, err
	}
	for _, candidate := range directoryPage.Items {
		presentation := presentations[candidate.ExternalID]
		if presentation == nil {
			continue
		}
		aggregate := aggregates[candidate.ExternalID]
		page.Items = append(page.Items, DepartmentSummary{
			ExternalID:                 candidate.ExternalID,
			ParentExternalID:           candidate.ParentExternalID,
			Name:                       candidate.Name,
			Path:                       candidate.Path,
			DisplayPath:                presentation.department.DisplayPath,
			Depth:                      presentation.depth,
			ChildCount:                 aggregate.ChildCount,
			HasChildren:                aggregate.ChildCount > 0,
			MemberCount:                aggregate.MemberCount,
			MatchedUserCount:           aggregate.MatchedUserCount,
			SubtreeMemberCount:         aggregate.SubtreeMemberCount,
			SubtreeMatchedUserCount:    aggregate.SubtreeMatchedUserCount,
			RepresentativeCount:        aggregate.RepresentativeCount,
			MatchedRepresentativeCount: aggregate.MatchedRepresentativeCount,
		})
	}
	return page, nil
}

func (s *Service) Departments(ctx context.Context) ([]DepartmentSummary, error) {
	source, err := s.currentSource(ctx)
	if err != nil {
		return nil, err
	}
	if !source.found {
		return []DepartmentSummary{}, nil
	}

	facts, err := source.view.Load(ctx, directoryfacts.Query{AllDepartments: true})
	if err != nil {
		return nil, fmt.Errorf("load complete directory hierarchy facts: %w", err)
	}
	departments := facts.Departments()
	if len(departments) == 0 {
		return []DepartmentSummary{}, nil
	}

	externalIDs := make([]string, 0, len(departments))
	for _, department := range departments {
		externalIDs = append(externalIDs, department.ExternalID)
	}
	aggregates, err := s.departmentAggregates(ctx, source, externalIDs)
	if err != nil {
		return nil, err
	}

	rows := make([]DepartmentSummary, 0, len(departments))
	for _, department := range departments {
		aggregate := aggregates[department.ExternalID]
		rows = append(rows, DepartmentSummary{
			ExternalID:                 department.ExternalID,
			ParentExternalID:           department.ParentExternalID,
			Name:                       department.Name,
			Path:                       department.Path,
			DisplayPath:                facts.Hierarchy().DisplayPath(department.ExternalID),
			Depth:                      facts.Hierarchy().Depth(department.ExternalID),
			ChildCount:                 aggregate.ChildCount,
			HasChildren:                aggregate.ChildCount > 0,
			MemberCount:                aggregate.MemberCount,
			MatchedUserCount:           aggregate.MatchedUserCount,
			SubtreeMemberCount:         aggregate.SubtreeMemberCount,
			SubtreeMatchedUserCount:    aggregate.SubtreeMatchedUserCount,
			RepresentativeCount:        aggregate.RepresentativeCount,
			MatchedRepresentativeCount: aggregate.MatchedRepresentativeCount,
		})
	}
	return rows, nil
}

func normalizeDepartmentOptionRequest(request DepartmentOptionRequest) DepartmentOptionRequest {
	request.Query = strings.TrimSpace(request.Query)
	request.SelectedID = strings.TrimSpace(request.SelectedID)
	request.Page, request.PageSize = normalizeDepartmentPage(request.Page, request.PageSize, 20)
	return request
}

func normalizeDepartmentChildrenRequest(request DepartmentChildrenRequest) DepartmentChildrenRequest {
	request.ParentDepartmentID = strings.TrimSpace(request.ParentDepartmentID)
	request.Page, request.PageSize = normalizeDepartmentPage(request.Page, request.PageSize, 25)
	return request
}

func normalizeDepartmentPage(page, pageSize, defaultPageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	switch {
	case pageSize <= 0:
		pageSize = defaultPageSize
	case pageSize > 100:
		pageSize = 100
	}
	return page, pageSize
}

func (s *Service) departmentAggregates(ctx context.Context, source resolvedSource, candidateIDs []string) (map[string]departmentAggregateRow, error) {
	aggregates, err := source.view.DepartmentAggregates(ctx, candidateIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]departmentAggregateRow, len(aggregates))
	for externalID, aggregate := range aggregates {
		out[externalID] = departmentAggregateRow{
			ExternalID:                 externalID,
			ChildCount:                 aggregate.ChildCount,
			MemberCount:                aggregate.MemberCount,
			MatchedUserCount:           aggregate.MatchedUserCount,
			SubtreeMemberCount:         aggregate.SubtreeMemberCount,
			SubtreeMatchedUserCount:    aggregate.SubtreeMatchedUserCount,
			RepresentativeCount:        aggregate.RepresentativeCount,
			MatchedRepresentativeCount: aggregate.MatchedRepresentativeCount,
		}
	}
	return out, nil
}
