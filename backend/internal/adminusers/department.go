package adminusers

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strconv"
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

func effectiveDepartmentCTEs(sourcePlaceholder string) string {
	return fmt.Sprintf(`WITH RECURSIVE
source_departments(
  %s,
  %s,
  %s,
  %s,
  %s
) AS MATERIALIZED (
  SELECT department.%s,
         department.%s,
         department.%s,
         department.%s,
         department.%s
  FROM %s AS department
  WHERE department.%s = %s
),
source_cardinality(row_count) AS MATERIALIZED (
  SELECT COUNT(*) FROM source_departments
),
cycle_walk(seed_external_id, external_id, parent_external_id, path_ids) AS (
  SELECT department.external_id,
         department.external_id,
         NULLIF(BTRIM(department.parent_external_id), ''),
         ARRAY[department.external_id]::text[]
  FROM source_departments AS department
  UNION ALL
  SELECT cycle_walk.seed_external_id,
         parent.external_id,
         NULLIF(BTRIM(parent.parent_external_id), ''),
         cycle_walk.path_ids || parent.external_id
  FROM cycle_walk
  JOIN source_departments AS parent
    ON parent.external_id = cycle_walk.parent_external_id
  WHERE NOT parent.external_id = ANY(cycle_walk.path_ids)
    AND CARDINALITY(cycle_walk.path_ids) < (
      SELECT source_cardinality.row_count FROM source_cardinality
    )
),
closed_cycle_paths(cycle_path) AS MATERIALIZED (
  SELECT DISTINCT cycle_walk.path_ids[
    ARRAY_POSITION(cycle_walk.path_ids, cycle_walk.parent_external_id):
    CARDINALITY(cycle_walk.path_ids)
  ]
  FROM cycle_walk
  WHERE cycle_walk.parent_external_id = ANY(cycle_walk.path_ids)
),
cycle_members(cycle_key, external_id) AS MATERIALIZED (
  SELECT DISTINCT
         (
           SELECT MIN(component.external_id)
           FROM UNNEST(closed_cycle_paths.cycle_path) AS component(external_id)
         ),
         member.external_id
  FROM closed_cycle_paths
  CROSS JOIN LATERAL UNNEST(closed_cycle_paths.cycle_path) AS member(external_id)
),
cycle_anchors(external_id) AS MATERIALIZED (
  SELECT ranked.external_id
  FROM (
    SELECT cycle_members.external_id,
	           ROW_NUMBER() OVER (
	             PARTITION BY cycle_members.cycle_key
	             ORDER BY LOWER(BTRIM(department.name) COLLATE "C") COLLATE "C",
	                      cycle_members.external_id COLLATE "C"
	           ) AS anchor_rank
    FROM cycle_members
    JOIN source_departments AS department
      ON department.external_id = cycle_members.external_id
  ) AS ranked
  WHERE ranked.anchor_rank = 1
),
navigation_departments(
  external_id,
  parent_external_id,
  effective_parent_external_id,
  name,
  path,
  metadata
) AS MATERIALIZED (
  SELECT department.external_id,
         department.parent_external_id,
         CASE
           WHEN NULLIF(BTRIM(department.parent_external_id), '') IS NULL THEN NULL
           WHEN NOT EXISTS (
             SELECT 1
             FROM source_departments AS current_parent
             WHERE current_parent.external_id = BTRIM(department.parent_external_id)
           ) THEN NULL
           WHEN EXISTS (
             SELECT 1
             FROM cycle_anchors
             WHERE cycle_anchors.external_id = department.external_id
           ) THEN NULL
           ELSE BTRIM(department.parent_external_id)
         END,
         department.name,
         department.path,
         department.metadata
  FROM source_departments AS department
)`,
		directorydepartment.FieldExternalID,
		directorydepartment.FieldParentExternalID,
		directorydepartment.FieldName,
		directorydepartment.FieldPath,
		directorydepartment.FieldMetadata,
		directorydepartment.FieldExternalID,
		directorydepartment.FieldParentExternalID,
		directorydepartment.FieldName,
		directorydepartment.FieldPath,
		directorydepartment.FieldMetadata,
		directorydepartment.Table,
		directorydepartment.FieldSourceID,
		sourcePlaceholder,
	)
}

func effectiveSubtreeCTE(departmentPlaceholder string) string {
	return fmt.Sprintf(`, subtree(%s) AS MATERIALIZED (
  SELECT root.%s
  FROM navigation_departments AS root
  WHERE root.%s = %s
  UNION
  SELECT child.%s
  FROM navigation_departments AS child
  JOIN subtree AS parent
    ON child.effective_parent_external_id = parent.%s
)`,
		directorydepartment.FieldExternalID,
		directorydepartment.FieldExternalID,
		directorydepartment.FieldExternalID,
		departmentPlaceholder,
		directorydepartment.FieldExternalID,
		directorydepartment.FieldExternalID,
	)
}

func eligibleUserCTEs(sourcePlaceholder string) string {
	return fmt.Sprintf(`,
eligible_members(id, matched_user_id, email_normalized) AS MATERIALIZED (
  SELECT member.%s, member.%s, member.%s
  FROM %s AS member
  WHERE member.%s = %s
    AND (
      EXISTS (
        SELECT 1
        FROM %s AS membership
        JOIN subtree
          ON subtree.external_id = membership.%s
        WHERE membership.%s = %s
          AND membership.%s = member.%s
      )
      OR (
        NOT EXISTS (
          SELECT 1
          FROM %s AS current_membership
          WHERE current_membership.%s = %s
            AND current_membership.%s = member.%s
        )
        AND member.%s IN (SELECT external_id FROM subtree)
      )
    )
),
eligible_user_ids(user_id) AS MATERIALIZED (
  SELECT eligible_members.matched_user_id
  FROM eligible_members
  WHERE eligible_members.matched_user_id > 0
  UNION
  SELECT candidate.%s
  FROM %s AS candidate
  JOIN eligible_members
    ON eligible_members.email_normalized = LOWER(BTRIM(candidate.%s))
)`,
		directorymember.FieldID,
		directorymember.FieldMatchedUserID,
		directorymember.FieldEmailNormalized,
		directorymember.Table,
		directorymember.FieldSourceID,
		sourcePlaceholder,
		directorymemberdepartment.Table,
		directorymemberdepartment.FieldDepartmentExternalID,
		directorymemberdepartment.FieldSourceID,
		sourcePlaceholder,
		directorymemberdepartment.FieldDirectoryMemberID,
		directorymember.FieldID,
		directorymemberdepartment.Table,
		directorymemberdepartment.FieldSourceID,
		sourcePlaceholder,
		directorymemberdepartment.FieldDirectoryMemberID,
		directorymember.FieldID,
		directorymember.FieldDepartmentExternalID,
		entuser.FieldID,
		entuser.Table,
		entuser.FieldEmail,
	)
}

func departmentUserPredicate(sourceID int, departmentID string) predicate.User {
	return func(selector *sql.Selector) {
		selector.Where(sql.P(func(builder *sql.Builder) {
			builder.Ident(selector.C(entuser.FieldID)).WriteString(" IN (")
			builder.Arg(effectiveDepartmentParameter(sourceID))
			builder.Arg(effectiveSubtreeParameter(departmentID))
			builder.WriteString("\nSELECT eligible_user_ids.user_id\nFROM eligible_user_ids")
			builder.WriteByte(')')
		}))
	}
}

// These parameters bind runtime values while rendering the shared SQL at the
// exact placeholder positions assigned by Ent's PostgreSQL builder. The source
// and department arguments stay consecutive so the subtree can reuse source.
type effectiveDepartmentParameter int

func (parameter effectiveDepartmentParameter) Value() (driver.Value, error) {
	return int64(parameter), nil
}

func (parameter effectiveDepartmentParameter) FormatParam(placeholder string, _ *sql.StmtInfo) string {
	return effectiveDepartmentCTEs(placeholder)
}

type effectiveSubtreeParameter string

func (parameter effectiveSubtreeParameter) Value() (driver.Value, error) {
	return string(parameter), nil
}

func (parameter effectiveSubtreeParameter) FormatParam(placeholder string, _ *sql.StmtInfo) string {
	sourcePlaceholder := previousPostgresPlaceholder(placeholder)
	return effectiveSubtreeCTE(placeholder) + eligibleUserCTEs(sourcePlaceholder)
}

func previousPostgresPlaceholder(placeholder string) string {
	if !strings.HasPrefix(placeholder, "$") {
		panic(fmt.Sprintf("adminusers: expected PostgreSQL placeholder, got %q", placeholder))
	}
	position, err := strconv.Atoi(strings.TrimPrefix(placeholder, "$"))
	if err != nil || position <= 1 {
		panic(fmt.Sprintf("adminusers: invalid subtree placeholder %q", placeholder))
	}
	return fmt.Sprintf("$%d", position-1)
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
		outerDepartment := sql.Table("navigation_departments").As("outer_department")
		ancestors := sql.Table("ancestors")
		selector.Prefix(sql.ExprFunc(func(builder *sql.Builder) {
			builder.Arg(effectiveDepartmentParameter(sourceID))
			builder.Arg(pageDepartmentCandidatesParameter(candidateIDs))
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
	}
}

type pageDepartmentCandidatesParameter []string

func (parameter pageDepartmentCandidatesParameter) Value() (driver.Value, error) {
	return pq.Array([]string(parameter)).Value()
}

func (parameter pageDepartmentCandidatesParameter) FormatParam(placeholder string, _ *sql.StmtInfo) string {
	return pageDepartmentAncestorCTEs(placeholder)
}

func pageDepartmentAncestorCTEs(candidatePlaceholder string) string {
	return fmt.Sprintf(`, requested_candidates(%s) AS MATERIALIZED (
  SELECT UNNEST(%s::text[])
),
ancestors(%s, effective_parent_external_id) AS MATERIALIZED (
  SELECT seed.%s, seed.effective_parent_external_id
  FROM navigation_departments AS seed
  JOIN requested_candidates
    ON requested_candidates.%s = seed.%s
  UNION
  SELECT parent.%s, parent.effective_parent_external_id
  FROM navigation_departments AS parent
  JOIN ancestors AS child
    ON child.effective_parent_external_id = parent.%s
)`,
		directorydepartment.FieldExternalID,
		candidatePlaceholder,
		directorydepartment.FieldExternalID,
		directorydepartment.FieldExternalID,
		directorydepartment.FieldExternalID,
		directorydepartment.FieldExternalID,
		directorydepartment.FieldExternalID,
		directorydepartment.FieldExternalID,
	)
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

type departmentOptionQueryParameter string

func (parameter departmentOptionQueryParameter) Value() (driver.Value, error) {
	return string(parameter), nil
}

func (parameter departmentOptionQueryParameter) FormatParam(placeholder string, _ *sql.StmtInfo) string {
	return departmentOptionCTEs(placeholder)
}

func departmentOptionCTEs(queryPlaceholder string) string {
	return fmt.Sprintf(`, filtered_departments AS MATERIALIZED (
  SELECT candidate.external_id,
         candidate.parent_external_id,
         candidate.name
  FROM navigation_departments AS candidate
  WHERE BTRIM(%[1]s::text) = ''
     OR STRPOS(LOWER(BTRIM(candidate.name)), LOWER(BTRIM(%[1]s::text))) > 0
     OR STRPOS(LOWER(BTRIM(candidate.external_id)), LOWER(BTRIM(%[1]s::text))) > 0
)`, queryPlaceholder)
}

func departmentOptionsPredicate(sourceID int, query string, ordered bool) predicate.DirectoryDepartment {
	return func(selector *sql.Selector) {
		options := sql.Table("filtered_departments")
		selector.Prefix(sql.ExprFunc(func(builder *sql.Builder) {
			builder.Arg(effectiveDepartmentParameter(sourceID))
			builder.Arg(departmentOptionQueryParameter(query))
		}))
		selector.From(options)
		selector.Select(
			options.C(directorydepartment.FieldExternalID),
			options.C(directorydepartment.FieldParentExternalID),
			options.C(directorydepartment.FieldName),
		)
		if ordered {
			selector.OrderExpr(sql.Expr("LOWER(BTRIM(" + options.C(directorydepartment.FieldName) + "))"))
			selector.OrderBy(options.C(directorydepartment.FieldExternalID))
		}
	}
}

type departmentChildParentParameter string

func (parameter departmentChildParentParameter) Value() (driver.Value, error) {
	if strings.TrimSpace(string(parameter)) == "" {
		return nil, nil
	}
	return strings.TrimSpace(string(parameter)), nil
}

func (parameter departmentChildParentParameter) FormatParam(placeholder string, _ *sql.StmtInfo) string {
	return departmentChildCandidateCTEs(placeholder)
}

func departmentChildCandidateCTEs(parentPlaceholder string) string {
	return fmt.Sprintf(`, supplied_parent(external_id) AS MATERIALIZED (
  SELECT parent.external_id
  FROM source_departments AS parent
  WHERE %[1]s::text IS NOT NULL
    AND parent.external_id = %[1]s
),
candidate_departments AS MATERIALIZED (
  SELECT candidate.*
  FROM navigation_departments AS candidate
  WHERE (
      %[1]s::text IS NULL
      AND candidate.effective_parent_external_id IS NULL
    )
    OR (
      %[1]s::text IS NOT NULL
      AND EXISTS (SELECT 1 FROM supplied_parent)
      AND candidate.effective_parent_external_id = (
        SELECT supplied_parent.external_id FROM supplied_parent
      )
    )
)`, parentPlaceholder)
}

func departmentChildrenPredicate(sourceID int, parentDepartmentID string, ordered bool) predicate.DirectoryDepartment {
	return func(selector *sql.Selector) {
		candidates := sql.Table("candidate_departments")
		selector.Prefix(sql.ExprFunc(func(builder *sql.Builder) {
			builder.Arg(effectiveDepartmentParameter(sourceID))
			builder.Arg(departmentChildParentParameter(parentDepartmentID))
		}))
		selector.From(candidates)
		selector.Select(
			candidates.C(directorydepartment.FieldExternalID),
			candidates.C(directorydepartment.FieldParentExternalID),
			candidates.C(directorydepartment.FieldName),
			candidates.C(directorydepartment.FieldPath),
		)
		if ordered {
			selector.OrderExpr(sql.Expr("LOWER(BTRIM(" + candidates.C(directorydepartment.FieldName) + "))"))
			selector.OrderBy(candidates.C(directorydepartment.FieldExternalID))
		}
	}
}

type departmentSummaryRootsParameter []string

func (parameter departmentSummaryRootsParameter) Value() (driver.Value, error) {
	return pq.Array([]string(parameter)).Value()
}

func (parameter departmentSummaryRootsParameter) FormatParam(placeholder string, _ *sql.StmtInfo) string {
	return departmentSummaryCTEs(placeholder)
}

func departmentSummaryPredicate(sourceID int, candidateIDs []string) predicate.DirectoryDepartment {
	return func(selector *sql.Selector) {
		summaries := sql.Table("department_summaries")
		selector.Prefix(sql.ExprFunc(func(builder *sql.Builder) {
			builder.Arg(effectiveDepartmentParameter(sourceID))
			builder.Arg(departmentSummaryRootsParameter(candidateIDs))
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

func departmentSummaryCTEs(rootsPlaceholder string) string {
	sourcePlaceholder := previousPostgresPlaceholder(rootsPlaceholder)
	return fmt.Sprintf(`, requested_roots(root_external_id) AS MATERIALIZED (
  SELECT UNNEST(%[1]s::text[])
),
descendants(root_external_id, external_id) AS MATERIALIZED (
  SELECT requested_roots.root_external_id, requested_roots.root_external_id
  FROM requested_roots
  UNION
  SELECT descendants.root_external_id, child.external_id
  FROM descendants
  JOIN navigation_departments AS child
    ON child.effective_parent_external_id = descendants.external_id
),
effective_assignments(root_external_id, member_id, matched_user_id, department_external_id) AS MATERIALIZED (
  SELECT descendants.root_external_id,
         member.id,
         member.matched_user_id,
         membership.department_external_id
  FROM descendants
  JOIN directory_member_departments AS membership
    ON membership.source_id = %[2]s
   AND membership.department_external_id = descendants.external_id
  JOIN directory_members AS member
    ON member.source_id = %[2]s
   AND member.id = membership.directory_member_id
  UNION ALL
  SELECT descendants.root_external_id,
         member.id,
         member.matched_user_id,
         member.department_external_id
  FROM descendants
  JOIN directory_members AS member
    ON member.source_id = %[2]s
   AND member.department_external_id = descendants.external_id
  WHERE NOT EXISTS (
    SELECT 1
    FROM directory_member_departments AS current_membership
    WHERE current_membership.source_id = %[2]s
      AND current_membership.directory_member_id = member.id
  )
),
department_representatives(root_external_id, representative_external_id) AS MATERIALIZED (
  SELECT requested_roots.root_external_id,
         BTRIM(representative_value.external_id)
  FROM requested_roots
  JOIN source_departments AS department
    ON department.external_id = requested_roots.root_external_id
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
    ON member.source_id = %[2]s
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
    AND BTRIM(leader_department.department_external_id) = requested_roots.root_external_id
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
  WHERE member.source_id = %[2]s
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
  LEFT JOIN navigation_departments AS child
    ON child.effective_parent_external_id = requested_roots.root_external_id
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
)`, rootsPlaceholder, sourcePlaceholder)
}
