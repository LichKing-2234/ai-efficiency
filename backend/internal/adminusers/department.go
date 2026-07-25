package adminusers

import (
	"context"
	"fmt"
	"strings"

	"entgo.io/ent/dialect/sql"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directorymemberdepartment"
	"github.com/ai-efficiency/backend/ent/predicate"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/lib/pq"
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

func writeDepartmentUserCTEs(builder *sql.Builder, sourceID int, departmentID string) {
	builder.WriteString(`WITH RECURSIVE
hierarchy_parameters(source_id, department_id) AS MATERIALIZED (
  SELECT `)
	builder.Arg(sourceID)
	builder.WriteString(`::bigint, `)
	builder.Arg(departmentID)
	builder.WriteString(`::text
),
subtree(external_id) AS MATERIALIZED (
  SELECT root.external_id
  FROM directory_departments AS root
  JOIN hierarchy_parameters AS parameters
    ON parameters.source_id = root.source_id
   AND parameters.department_id = root.external_id
  UNION
  SELECT child.external_id
  FROM directory_departments AS child
  JOIN hierarchy_parameters AS parameters
    ON parameters.source_id = child.source_id
  JOIN subtree AS parent
    ON child.effective_parent_external_id = parent.external_id
),
eligible_members(id, matched_user_id, email_normalized) AS MATERIALIZED (
  SELECT member.id, member.matched_user_id, member.email_normalized
  FROM directory_members AS member
  JOIN hierarchy_parameters AS parameters
    ON parameters.source_id = member.source_id
  WHERE (
      EXISTS (
        SELECT 1
        FROM directory_member_departments AS membership
        JOIN hierarchy_parameters AS parameters
          ON parameters.source_id = membership.source_id
        JOIN subtree
          ON subtree.external_id = membership.department_external_id
        WHERE membership.directory_member_id = member.id
      )
      OR (
        NOT EXISTS (
          SELECT 1
          FROM directory_member_departments AS current_membership
          JOIN hierarchy_parameters AS parameters
            ON parameters.source_id = current_membership.source_id
          WHERE current_membership.directory_member_id = member.id
        )
        AND member.department_external_id IN (SELECT external_id FROM subtree)
      )
    )
),
eligible_user_ids(user_id) AS MATERIALIZED (
  SELECT eligible_members.matched_user_id
  FROM eligible_members
  WHERE eligible_members.matched_user_id > 0
  UNION
  SELECT candidate.id
  FROM users AS candidate
  JOIN eligible_members
    ON eligible_members.email_normalized = LOWER(BTRIM(candidate.email))
)`)
}

func departmentUserPredicate(sourceID int, departmentID string) predicate.User {
	return func(selector *sql.Selector) {
		selector.Where(sql.P(func(builder *sql.Builder) {
			builder.Ident(selector.C(entuser.FieldID)).WriteString(" IN (")
			writeDepartmentUserCTEs(builder, sourceID, departmentID)
			builder.WriteString("\nSELECT eligible_user_ids.user_id\nFROM eligible_user_ids")
			builder.WriteByte(')')
		}))
	}
}

type pageMemberRow struct {
	ID                   int    `json:"id"`
	MatchedUserID        *int   `json:"matched_user_id"`
	EmailNormalized      string `json:"email_normalized"`
	DepartmentExternalID string `json:"department_external_id"`
}

type pageMembershipRow struct {
	ID                   int    `json:"id"`
	DirectoryMemberID    int    `json:"directory_member_id"`
	DepartmentExternalID string `json:"department_external_id"`
}

type pageDepartmentRow struct {
	ExternalID                string  `json:"external_id"`
	ParentExternalID          *string `json:"parent_external_id"`
	EffectiveParentExternalID *string `json:"effective_parent_external_id"`
	Name                      string  `json:"name"`
	Path                      string  `json:"path"`
}

func (s *Service) departmentsForPage(ctx context.Context, source resolvedSource, users []*ent.User) (map[int]*Department, error) {
	out := make(map[int]*Department, len(users))
	if !source.found || len(users) == 0 {
		return out, nil
	}

	pageUserIDs := make([]int, 0, len(users))
	pageUserByID := make(map[int]struct{}, len(users))
	pageUserIDsByEmail := make(map[string][]int, len(users))
	emails := make([]string, 0, len(users))
	seenEmails := make(map[string]struct{}, len(users))
	for _, user := range users {
		if user == nil {
			continue
		}
		pageUserIDs = append(pageUserIDs, user.ID)
		pageUserByID[user.ID] = struct{}{}
		email := normalizeEmail(user.Email)
		if email == "" {
			continue
		}
		pageUserIDsByEmail[email] = append(pageUserIDsByEmail[email], user.ID)
		if _, ok := seenEmails[email]; !ok {
			seenEmails[email] = struct{}{}
			emails = append(emails, email)
		}
	}

	memberPredicates := []predicate.DirectoryMember{directorymember.MatchedUserIDIn(pageUserIDs...)}
	if len(emails) > 0 {
		memberPredicates = append(memberPredicates, directorymember.EmailNormalizedIn(emails...))
	}
	var members []pageMemberRow
	if err := s.client.DirectoryMember.Query().
		Where(
			directorymember.SourceIDEQ(source.id),
			directorymember.Or(memberPredicates...),
		).
		Order(ent.Asc(directorymember.FieldID)).
		Select(
			directorymember.FieldID,
			directorymember.FieldMatchedUserID,
			directorymember.FieldEmailNormalized,
			directorymember.FieldDepartmentExternalID,
		).
		Scan(ctx, &members); err != nil {
		return nil, fmt.Errorf("list current directory members for admin user page: %w", err)
	}
	if len(members) == 0 {
		return out, nil
	}

	memberIDs := make([]int, 0, len(members))
	for _, member := range members {
		memberIDs = append(memberIDs, member.ID)
	}
	var memberships []pageMembershipRow
	if err := s.client.DirectoryMemberDepartment.Query().
		Where(
			directorymemberdepartment.SourceIDEQ(source.id),
			directorymemberdepartment.DirectoryMemberIDIn(memberIDs...),
		).
		Order(
			ent.Asc(directorymemberdepartment.FieldDirectoryMemberID),
			ent.Asc(directorymemberdepartment.FieldDepartmentExternalID),
			ent.Asc(directorymemberdepartment.FieldID),
		).
		Select(
			directorymemberdepartment.FieldID,
			directorymemberdepartment.FieldDirectoryMemberID,
			directorymemberdepartment.FieldDepartmentExternalID,
		).
		Scan(ctx, &memberships); err != nil {
		return nil, fmt.Errorf("list current directory memberships for admin user page: %w", err)
	}
	membershipsByMemberID := make(map[int][]pageMembershipRow, len(members))
	for _, membership := range memberships {
		membershipsByMemberID[membership.DirectoryMemberID] = append(membershipsByMemberID[membership.DirectoryMemberID], membership)
	}

	candidatesByMemberID := make(map[int][]string, len(members))
	candidateIDs := make([]string, 0, len(memberships)+len(members))
	for _, member := range members {
		candidates := memberDepartmentCandidates(member, membershipsByMemberID[member.ID])
		candidatesByMemberID[member.ID] = candidates
		candidateIDs = appendUniqueDepartmentIDs(candidateIDs, candidates...)
	}
	departmentsByExternalID, err := s.loadPageDepartments(ctx, source.id, candidateIDs)
	if err != nil {
		return nil, err
	}

	for _, member := range members {
		var chosen *Department
		for _, candidateID := range candidatesByMemberID[member.ID] {
			if department := departmentsByExternalID[candidateID]; department != nil {
				chosen = department
				break
			}
		}
		if chosen == nil {
			continue
		}

		matchingUserIDs := make([]int, 0, 1+len(pageUserIDsByEmail[normalizeEmail(member.EmailNormalized)]))
		if member.MatchedUserID != nil && *member.MatchedUserID > 0 {
			matchingUserIDs = append(matchingUserIDs, *member.MatchedUserID)
		}
		matchingUserIDs = append(matchingUserIDs, pageUserIDsByEmail[normalizeEmail(member.EmailNormalized)]...)
		seenUserIDs := make(map[int]struct{}, len(matchingUserIDs))
		for _, userID := range matchingUserIDs {
			if _, onPage := pageUserByID[userID]; !onPage {
				continue
			}
			if _, duplicate := seenUserIDs[userID]; duplicate {
				continue
			}
			seenUserIDs[userID] = struct{}{}
			if out[userID] == nil {
				out[userID] = chosen
			}
		}
	}
	return out, nil
}

func memberDepartmentCandidates(member pageMemberRow, memberships []pageMembershipRow) []string {
	primaryID := strings.TrimSpace(member.DepartmentExternalID)
	if len(memberships) == 0 {
		if primaryID == "" {
			return nil
		}
		return []string{primaryID}
	}

	candidates := make([]string, 0, len(memberships))
	if primaryID != "" {
		for _, membership := range memberships {
			if strings.TrimSpace(membership.DepartmentExternalID) == primaryID {
				candidates = append(candidates, primaryID)
				break
			}
		}
	}
	for _, membership := range memberships {
		candidates = appendUniqueDepartmentIDs(candidates, membership.DepartmentExternalID)
	}
	return candidates
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

func (s *Service) loadPageDepartments(ctx context.Context, sourceID int, candidateIDs []string) (map[string]*Department, error) {
	presentations, err := s.loadDepartmentPresentations(ctx, sourceID, candidateIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*Department, len(presentations))
	for externalID, presentation := range presentations {
		out[externalID] = presentation.department
	}
	return out, nil
}

func (s *Service) loadDepartmentPresentations(ctx context.Context, sourceID int, candidateIDs []string) (map[string]*departmentPresentation, error) {
	out := make(map[string]*departmentPresentation, len(candidateIDs))
	if len(candidateIDs) == 0 {
		return out, nil
	}
	var rows []pageDepartmentRow
	if err := s.client.DirectoryDepartment.Query().
		Where(pageDepartmentClosurePredicate(sourceID, candidateIDs)).
		Select(
			directorydepartment.FieldExternalID,
			directorydepartment.FieldParentExternalID,
			directorydepartment.FieldName,
			directorydepartment.FieldPath,
		).
		Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("load candidate directory departments and ancestors: %w", err)
	}

	rowsByID := make(map[string]pageDepartmentRow, len(rows))
	for _, row := range rows {
		rowsByID[row.ExternalID] = row
	}
	displayPaths := make(map[string]string, len(rows))
	depths := make(map[string]int, len(rows))
	for externalID, row := range rowsByID {
		displayPath := effectiveDisplayPath(externalID, rowsByID, displayPaths, map[string]bool{})
		out[externalID] = &departmentPresentation{
			department: &Department{
				ExternalID:  externalID,
				Name:        row.Name,
				Path:        row.Path,
				DisplayPath: displayPath,
			},
			depth: effectiveDepartmentDepth(externalID, rowsByID, depths, map[string]bool{}),
		}
	}
	return out, nil
}

func effectiveDisplayPath(externalID string, rows map[string]pageDepartmentRow, cached map[string]string, visiting map[string]bool) string {
	if path := cached[externalID]; path != "" {
		return path
	}
	row, ok := rows[externalID]
	if !ok {
		return ""
	}
	name := strings.TrimSpace(row.Name)
	if name == "" {
		name = externalID
	}
	if visiting[externalID] {
		return name
	}
	visiting[externalID] = true
	if row.EffectiveParentExternalID != nil {
		parentID := strings.TrimSpace(*row.EffectiveParentExternalID)
		if parentID != "" {
			if parentPath := effectiveDisplayPath(parentID, rows, cached, visiting); parentPath != "" {
				name = parentPath + " / " + name
			}
		}
	}
	delete(visiting, externalID)
	cached[externalID] = name
	return name
}

func effectiveDepartmentDepth(externalID string, rows map[string]pageDepartmentRow, cached map[string]int, visiting map[string]bool) int {
	if depth, ok := cached[externalID]; ok {
		return depth
	}
	row, ok := rows[externalID]
	if !ok || visiting[externalID] {
		return 0
	}
	visiting[externalID] = true
	depth := 0
	if row.EffectiveParentExternalID != nil {
		parentID := strings.TrimSpace(*row.EffectiveParentExternalID)
		if parentID != "" {
			if _, exists := rows[parentID]; exists {
				depth = effectiveDepartmentDepth(parentID, rows, cached, visiting) + 1
			}
		}
	}
	delete(visiting, externalID)
	cached[externalID] = depth
	return depth
}

func normalizeEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func pageDepartmentClosurePredicate(sourceID int, candidateIDs []string) predicate.DirectoryDepartment {
	return func(selector *sql.Selector) {
		outerDepartment := sql.Table(directorydepartment.Table).As("outer_department")
		ancestors := sql.Table("ancestors")
		selector.Prefix(sql.ExprFunc(func(builder *sql.Builder) {
			writePageDepartmentAncestorCTEs(builder, sourceID, candidateIDs)
		}))
		selector.From(outerDepartment)
		selector.Join(ancestors).On(
			ancestors.C(directorydepartment.FieldExternalID),
			outerDepartment.C(directorydepartment.FieldExternalID),
		)
		selector.Select(
			outerDepartment.C(directorydepartment.FieldExternalID),
			outerDepartment.C(directorydepartment.FieldParentExternalID),
			outerDepartment.C(directorydepartment.FieldName),
			outerDepartment.C(directorydepartment.FieldPath),
			ancestors.C("effective_parent_external_id"),
		)
		selector.Where(sql.EQ(outerDepartment.C(directorydepartment.FieldSourceID), sourceID))
	}
}

func writePageDepartmentAncestorCTEs(builder *sql.Builder, sourceID int, candidateIDs []string) {
	builder.WriteString(`WITH RECURSIVE
hierarchy_parameters(source_id, candidate_ids) AS MATERIALIZED (
  SELECT `)
	builder.Arg(sourceID)
	builder.WriteString(`::bigint, `)
	builder.Arg(pq.Array(candidateIDs))
	builder.WriteString(`::text[]
),
requested_candidates(external_id) AS MATERIALIZED (
  SELECT UNNEST(hierarchy_parameters.candidate_ids)
  FROM hierarchy_parameters
),
ancestors(external_id, effective_parent_external_id) AS MATERIALIZED (
  SELECT seed.external_id, seed.effective_parent_external_id
  FROM directory_departments AS seed
  JOIN hierarchy_parameters AS parameters
    ON parameters.source_id = seed.source_id
  JOIN requested_candidates
    ON requested_candidates.external_id = seed.external_id
  UNION
  SELECT parent.external_id, parent.effective_parent_external_id
  FROM directory_departments AS parent
  JOIN hierarchy_parameters AS parameters
    ON parameters.source_id = parent.source_id
  JOIN ancestors AS child
    ON child.effective_parent_external_id = parent.external_id
)`)
}

type departmentOptionRow struct {
	ExternalID       string  `json:"external_id"`
	ParentExternalID *string `json:"parent_external_id"`
	Name             string  `json:"name"`
}

type departmentCandidateRow struct {
	ExternalID       string  `json:"external_id"`
	ParentExternalID *string `json:"parent_external_id"`
	Name             string  `json:"name"`
	Path             string  `json:"path"`
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

type departmentCountRow struct {
	Count int `json:"count"`
}

type completeDepartmentRow struct {
	ExternalID                string  `json:"external_id"`
	ParentExternalID          *string `json:"parent_external_id"`
	EffectiveParentExternalID *string `json:"effective_parent_external_id"`
	Name                      string  `json:"name"`
	Path                      string  `json:"path"`
	DisplayPath               string  `json:"display_path"`
	Depth                     int     `json:"depth"`
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

	total, err := s.countDepartmentOptions(ctx, source.id, request.Query)
	if err != nil {
		return nil, err
	}
	page.Total = total
	var rows []departmentOptionRow
	if !pageStartsBeyondTotal(total, request.Page, request.PageSize) {
		offset := (request.Page - 1) * request.PageSize
		if err := s.client.DirectoryDepartment.Query().
			Where(departmentOptionsPredicate(source.id, request.Query, true)).
			Limit(request.PageSize).
			Offset(offset).
			Select(
				directorydepartment.FieldExternalID,
				directorydepartment.FieldParentExternalID,
				directorydepartment.FieldName,
			).
			Scan(ctx, &rows); err != nil {
			return nil, fmt.Errorf("list bounded department options: %w", err)
		}
	}

	candidateIDs := make([]string, 0, len(rows)+1)
	for _, row := range rows {
		candidateIDs = append(candidateIDs, row.ExternalID)
	}
	if request.SelectedID != "" {
		candidateIDs = appendUniqueDepartmentIDs(candidateIDs, request.SelectedID)
	}
	presentations, err := s.loadDepartmentPresentations(ctx, source.id, candidateIDs)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
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

	total, err := s.countDepartmentChildren(ctx, source.id, request.ParentDepartmentID)
	if err != nil {
		return nil, err
	}
	page.Total = total
	if pageStartsBeyondTotal(total, request.Page, request.PageSize) {
		return page, nil
	}

	offset := (request.Page - 1) * request.PageSize
	var candidates []departmentCandidateRow
	if err := s.client.DirectoryDepartment.Query().
		Where(departmentChildrenPredicate(source.id, request.ParentDepartmentID, true)).
		Limit(request.PageSize).
		Offset(offset).
		Select(
			directorydepartment.FieldExternalID,
			directorydepartment.FieldParentExternalID,
			directorydepartment.FieldName,
			directorydepartment.FieldPath,
		).
		Scan(ctx, &candidates); err != nil {
		return nil, fmt.Errorf("list bounded department children: %w", err)
	}
	if len(candidates) == 0 {
		return page, nil
	}

	candidateIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidateIDs = append(candidateIDs, candidate.ExternalID)
	}
	presentations, err := s.loadDepartmentPresentations(ctx, source.id, candidateIDs)
	if err != nil {
		return nil, err
	}
	aggregates, err := s.departmentAggregates(ctx, source.id, candidateIDs)
	if err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
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

	var departments []completeDepartmentRow
	if err := s.client.DirectoryDepartment.Query().
		Where(completeDepartmentHierarchyPredicate(source.id)).
		Select(
			directorydepartment.FieldExternalID,
			directorydepartment.FieldParentExternalID,
			directorydepartment.FieldEffectiveParentExternalID,
			directorydepartment.FieldName,
			directorydepartment.FieldPath,
		).
		Scan(ctx, &departments); err != nil {
		return nil, fmt.Errorf("list complete persisted department hierarchy: %w", err)
	}
	if len(departments) == 0 {
		return []DepartmentSummary{}, nil
	}

	externalIDs := make([]string, 0, len(departments))
	for _, department := range departments {
		externalIDs = append(externalIDs, department.ExternalID)
	}
	aggregates, err := s.departmentAggregates(ctx, source.id, externalIDs)
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
			DisplayPath:                department.DisplayPath,
			Depth:                      department.Depth,
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

func completeDepartmentHierarchyPredicate(sourceID int) predicate.DirectoryDepartment {
	return func(selector *sql.Selector) {
		hierarchy := sql.Table("department_hierarchy")
		selector.Prefix(sql.ExprFunc(func(builder *sql.Builder) {
			writeCompleteDepartmentHierarchyCTEs(builder, sourceID)
		}))
		selector.From(hierarchy)
		selector.Select(
			hierarchy.C(directorydepartment.FieldExternalID),
			hierarchy.C(directorydepartment.FieldParentExternalID),
			hierarchy.C(directorydepartment.FieldEffectiveParentExternalID),
			hierarchy.C(directorydepartment.FieldName),
			hierarchy.C(directorydepartment.FieldPath),
			hierarchy.C("display_path"),
			hierarchy.C("depth"),
		)
		selector.OrderBy(hierarchy.C("traversal_order"))
	}
}

func writeCompleteDepartmentHierarchyCTEs(builder *sql.Builder, sourceID int) {
	builder.WriteString(`WITH RECURSIVE
hierarchy_parameters(source_id) AS MATERIALIZED (
  SELECT `)
	builder.Arg(sourceID)
	builder.WriteString(`::bigint
),
department_hierarchy(
  external_id,
  parent_external_id,
  effective_parent_external_id,
  name,
  path,
  display_path,
  depth,
  sort_name,
  sort_external_id
) AS MATERIALIZED (
  SELECT department.external_id,
         department.parent_external_id,
         department.effective_parent_external_id,
         department.name,
         department.path,
         COALESCE(NULLIF(BTRIM(department.name), ''), department.external_id),
         0,
         LOWER(BTRIM(department.name) COLLATE "C") COLLATE "C",
         department.external_id COLLATE "C"
  FROM directory_departments AS department
  JOIN hierarchy_parameters AS parameters
    ON parameters.source_id = department.source_id
  WHERE department.effective_parent_external_id IS NULL
  UNION ALL
  SELECT child.external_id,
         child.parent_external_id,
         child.effective_parent_external_id,
         child.name,
         child.path,
         parent.display_path || ' / ' || COALESCE(NULLIF(BTRIM(child.name), ''), child.external_id),
         parent.depth + 1,
         LOWER(BTRIM(child.name) COLLATE "C") COLLATE "C",
         child.external_id COLLATE "C"
  FROM directory_departments AS child
  JOIN hierarchy_parameters AS parameters
    ON parameters.source_id = child.source_id
  JOIN department_hierarchy AS parent
    ON child.effective_parent_external_id = parent.external_id
)
SEARCH DEPTH FIRST BY sort_name, sort_external_id SET traversal_order`)
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

func pageStartsBeyondTotal(total, page, pageSize int) bool {
	if total == 0 {
		return true
	}
	pageCount := total / pageSize
	if total%pageSize != 0 {
		pageCount++
	}
	return page-1 >= pageCount
}

func (s *Service) countDepartmentOptions(ctx context.Context, sourceID int, query string) (int, error) {
	var rows []departmentCountRow
	if err := s.client.DirectoryDepartment.Query().
		Where(departmentOptionsPredicate(sourceID, query, false)).
		Select().
		Aggregate(ent.As(ent.Count(), "count")).
		Scan(ctx, &rows); err != nil {
		return 0, fmt.Errorf("count bounded department options: %w", err)
	}
	if len(rows) != 1 {
		return 0, fmt.Errorf("count bounded department options: expected one row, got %d", len(rows))
	}
	return rows[0].Count, nil
}

func (s *Service) countDepartmentChildren(ctx context.Context, sourceID int, parentDepartmentID string) (int, error) {
	var rows []departmentCountRow
	if err := s.client.DirectoryDepartment.Query().
		Where(departmentChildrenPredicate(sourceID, parentDepartmentID, false)).
		Select().
		Aggregate(ent.As(ent.Count(), "count")).
		Scan(ctx, &rows); err != nil {
		return 0, fmt.Errorf("count bounded department children: %w", err)
	}
	if len(rows) != 1 {
		return 0, fmt.Errorf("count bounded department children: expected one row, got %d", len(rows))
	}
	return rows[0].Count, nil
}

func (s *Service) departmentAggregates(ctx context.Context, sourceID int, candidateIDs []string) (map[string]departmentAggregateRow, error) {
	rows := make([]departmentAggregateRow, 0, len(candidateIDs))
	if err := s.client.DirectoryDepartment.Query().
		Where(departmentSummaryPredicate(sourceID, candidateIDs)).
		Select(directorydepartment.FieldExternalID).
		Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("summarize bounded department children: %w", err)
	}
	out := make(map[string]departmentAggregateRow, len(rows))
	for _, row := range rows {
		out[row.ExternalID] = row
	}
	return out, nil
}

func departmentOptionsPredicate(sourceID int, query string, ordered bool) predicate.DirectoryDepartment {
	return func(selector *sql.Selector) {
		selector.Where(sql.EQ(selector.C(directorydepartment.FieldSourceID), sourceID))
		if query != "" {
			selector.Where(sql.Or(
				sql.ContainsFold(selector.C(directorydepartment.FieldName), query),
				sql.ContainsFold(selector.C(directorydepartment.FieldExternalID), query),
			))
		}
		if ordered {
			selector.OrderExpr(sql.Expr("LOWER(BTRIM(" + selector.C(directorydepartment.FieldName) + "))"))
			selector.OrderBy(selector.C(directorydepartment.FieldExternalID))
		}
	}
}

func departmentChildrenPredicate(sourceID int, parentDepartmentID string, ordered bool) predicate.DirectoryDepartment {
	return func(selector *sql.Selector) {
		selector.Where(sql.EQ(selector.C(directorydepartment.FieldSourceID), sourceID))
		if parentDepartmentID == "" {
			selector.Where(sql.IsNull(selector.C(directorydepartment.FieldEffectiveParentExternalID)))
		} else {
			parent := sql.Table(directorydepartment.Table).As("supplied_parent")
			selector.Where(sql.EQ(selector.C(directorydepartment.FieldEffectiveParentExternalID), parentDepartmentID))
			selector.Where(sql.Exists(sql.Select(parent.C(directorydepartment.FieldExternalID)).
				From(parent).
				Where(sql.And(
					sql.EQ(parent.C(directorydepartment.FieldSourceID), sourceID),
					sql.EQ(parent.C(directorydepartment.FieldExternalID), parentDepartmentID),
				))))
		}
		if ordered {
			selector.OrderExpr(sql.Expr("LOWER(BTRIM(" + selector.C(directorydepartment.FieldName) + "))"))
			selector.OrderBy(selector.C(directorydepartment.FieldExternalID))
		}
	}
}

func departmentSummaryPredicate(sourceID int, candidateIDs []string) predicate.DirectoryDepartment {
	return func(selector *sql.Selector) {
		summaries := sql.Table("department_summaries")
		selector.Prefix(sql.ExprFunc(func(builder *sql.Builder) {
			writeDepartmentSummaryCTEs(builder, sourceID, candidateIDs)
		}))
		selector.From(summaries)
		selector.Select(
			summaries.C(directorydepartment.FieldExternalID),
			summaries.C("child_count"),
			summaries.C("member_count"),
			summaries.C("matched_user_count"),
			summaries.C("subtree_member_count"),
			summaries.C("subtree_matched_user_count"),
			summaries.C("representative_count"),
			summaries.C("matched_representative_count"),
		)
	}
}

func writeDepartmentSummaryCTEs(builder *sql.Builder, sourceID int, candidateIDs []string) {
	builder.WriteString(`WITH RECURSIVE
hierarchy_parameters(source_id, root_ids) AS MATERIALIZED (
  SELECT `)
	builder.Arg(sourceID)
	builder.WriteString(`::bigint, `)
	builder.Arg(pq.Array(candidateIDs))
	builder.WriteString(`::text[]
),
requested_roots(root_external_id) AS MATERIALIZED (
  SELECT UNNEST(hierarchy_parameters.root_ids) COLLATE "C"
  FROM hierarchy_parameters
),
descendants(root_external_id, external_id) AS MATERIALIZED (
  SELECT requested_roots.root_external_id, requested_roots.root_external_id
  FROM requested_roots
  UNION
  SELECT descendants.root_external_id, child.external_id COLLATE "C"
  FROM descendants
  JOIN directory_departments AS child
    ON child.effective_parent_external_id COLLATE "C" = descendants.external_id
   AND child.source_id = (SELECT source_id FROM hierarchy_parameters)
),
effective_assignments(root_external_id, member_id, matched_user_id, department_external_id) AS MATERIALIZED (
  SELECT descendants.root_external_id,
         member.id,
         member.matched_user_id,
         membership.department_external_id
  FROM descendants
  JOIN directory_member_departments AS membership
    ON membership.source_id = (SELECT source_id FROM hierarchy_parameters)
   AND membership.department_external_id COLLATE "C" = descendants.external_id
  JOIN directory_members AS member
    ON member.source_id = (SELECT source_id FROM hierarchy_parameters)
   AND member.id = membership.directory_member_id
  UNION ALL
  SELECT descendants.root_external_id,
         member.id,
         member.matched_user_id,
         member.department_external_id
  FROM descendants
  JOIN directory_members AS member
    ON member.source_id = (SELECT source_id FROM hierarchy_parameters)
   AND member.department_external_id COLLATE "C" = descendants.external_id
  WHERE NOT EXISTS (
    SELECT 1
    FROM directory_member_departments AS current_membership
    WHERE current_membership.source_id = (SELECT source_id FROM hierarchy_parameters)
      AND current_membership.directory_member_id = member.id
  )
),
department_representatives(root_external_id, representative_external_id) AS MATERIALIZED (
  SELECT requested_roots.root_external_id,
         BTRIM(representative_value.external_id)
  FROM requested_roots
  JOIN directory_departments AS department
    ON department.external_id COLLATE "C" = requested_roots.root_external_id
   AND department.source_id = (SELECT source_id FROM hierarchy_parameters)
  CROSS JOIN LATERAL JSONB_ARRAY_ELEMENTS_TEXT(
    CASE
      WHEN JSONB_TYPEOF(department.metadata -> 'representative_external_ids') = 'array'
        THEN department.metadata -> 'representative_external_ids'
      WHEN department.metadata ? 'representative_external_ids'
        THEN JSONB_BUILD_ARRAY(department.metadata -> 'representative_external_ids')
      ELSE '[]'::jsonb
    END
  ) AS representative_value(external_id)
  WHERE BTRIM(representative_value.external_id) <> ''
),
leader_representatives(root_external_id, representative_external_id) AS MATERIALIZED (
  SELECT requested_roots.root_external_id,
         BTRIM(member.external_id)
  FROM requested_roots
  JOIN directory_members AS member
    ON member.source_id = (SELECT source_id FROM hierarchy_parameters)
  CROSS JOIN LATERAL JSONB_ARRAY_ELEMENTS_TEXT(
    CASE
      WHEN JSONB_TYPEOF(member.metadata -> 'leader_department_ids') = 'array'
        THEN member.metadata -> 'leader_department_ids'
      WHEN member.metadata ? 'leader_department_ids'
        THEN JSONB_BUILD_ARRAY(member.metadata -> 'leader_department_ids')
      ELSE '[]'::jsonb
    END
  ) AS leader_department(department_external_id)
  WHERE BTRIM(member.external_id) <> ''
    AND BTRIM(leader_department.department_external_id) COLLATE "C" = requested_roots.root_external_id
),
representatives(root_external_id, representative_external_id) AS MATERIALIZED (
  SELECT root_external_id, representative_external_id
  FROM department_representatives
  UNION
  SELECT root_external_id, representative_external_id
  FROM leader_representatives
),
matched_representative_ids(representative_external_id) AS MATERIALIZED (
  SELECT DISTINCT BTRIM(member.external_id)
  FROM directory_members AS member
  WHERE member.source_id = (SELECT source_id FROM hierarchy_parameters)
    AND BTRIM(member.external_id) <> ''
    AND member.matched_user_id > 0
),
representative_counts(root_external_id, representative_count, matched_representative_count) AS MATERIALIZED (
  SELECT requested_roots.root_external_id,
         COUNT(representatives.representative_external_id),
         COUNT(matched_representative_ids.representative_external_id)
  FROM requested_roots
  LEFT JOIN representatives
    ON representatives.root_external_id = requested_roots.root_external_id
  LEFT JOIN matched_representative_ids
    ON matched_representative_ids.representative_external_id = representatives.representative_external_id
  GROUP BY requested_roots.root_external_id
),
child_counts(root_external_id, child_count) AS MATERIALIZED (
  SELECT requested_roots.root_external_id, COUNT(child.external_id)
  FROM requested_roots
  LEFT JOIN directory_departments AS child
    ON child.source_id = (SELECT source_id FROM hierarchy_parameters)
   AND child.effective_parent_external_id COLLATE "C" = requested_roots.root_external_id
  GROUP BY requested_roots.root_external_id
),
department_summaries(
  external_id,
  child_count,
  member_count,
  matched_user_count,
  subtree_member_count,
  subtree_matched_user_count,
  representative_count,
  matched_representative_count
) AS MATERIALIZED (
  SELECT requested_roots.root_external_id,
         child_counts.child_count,
         COUNT(DISTINCT effective_assignments.member_id)
           FILTER (WHERE effective_assignments.department_external_id = requested_roots.root_external_id),
         COUNT(DISTINCT effective_assignments.matched_user_id)
           FILTER (WHERE effective_assignments.department_external_id = requested_roots.root_external_id
                     AND effective_assignments.matched_user_id > 0),
         COUNT(DISTINCT effective_assignments.member_id),
         COUNT(DISTINCT effective_assignments.matched_user_id)
           FILTER (WHERE effective_assignments.matched_user_id > 0),
         representative_counts.representative_count,
         representative_counts.matched_representative_count
  FROM requested_roots
  LEFT JOIN effective_assignments
    ON effective_assignments.root_external_id = requested_roots.root_external_id
  JOIN child_counts
    ON child_counts.root_external_id = requested_roots.root_external_id
  JOIN representative_counts
    ON representative_counts.root_external_id = requested_roots.root_external_id
  GROUP BY requested_roots.root_external_id,
           child_counts.child_count,
           representative_counts.representative_count,
           representative_counts.matched_representative_count
)`)
}
