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

func (s *Service) departmentsForPage(ctx context.Context, source resolvedSource, users []*ent.User) (map[int]*Department, error) {
	out := make(map[int]*Department, len(users))
	if !source.found || len(users) == 0 {
		return out, nil
	}
	userIDs := make([]int, 0, len(users))
	emails := make([]string, 0, len(users))
	knownUsers := make([]directoryfacts.User, 0, len(users))
	for _, user := range users {
		if user == nil {
			continue
		}
		userIDs = append(userIDs, user.ID)
		emails = append(emails, user.Email)
		knownUsers = append(knownUsers, directoryfacts.User{ID: user.ID, Username: user.Username, Email: user.Email})
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
	for _, userID := range userIDs {
		if department := facts.PreferredDepartmentForUser(userID); department != nil {
			if departmentsByExternalID[department.ExternalID] == nil {
				departmentsByExternalID[department.ExternalID] = &Department{
					ExternalID:  department.ExternalID,
					Name:        department.Name,
					Path:        department.Path,
					DisplayPath: facts.Hierarchy().DisplayPath(department.ExternalID),
				}
			}
			out[userID] = departmentsByExternalID[department.ExternalID]
		}
	}
	return out, nil
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
