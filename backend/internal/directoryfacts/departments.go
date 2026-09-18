package directoryfacts

import (
	"context"
	"fmt"
	"strings"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/predicate"
	"github.com/lib/pq"
)

type departmentPageRow struct {
	ID                        int            `json:"id"`
	ExternalID                string         `json:"external_id"`
	ParentExternalID          *string        `json:"parent_external_id"`
	EffectiveParentExternalID *string        `json:"effective_parent_external_id"`
	Name                      string         `json:"name"`
	Path                      string         `json:"path"`
	Metadata                  map[string]any `json:"metadata"`
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

func (v *currentView) DepartmentPage(ctx context.Context, query DepartmentPageQuery) (DepartmentPage, error) {
	if v == nil || v.store == nil || v.store.client == nil {
		return DepartmentPage{}, fmt.Errorf("directory facts view is not configured")
	}
	predicate := departmentPagePredicate(v.snapshot, query, false)
	var counts []departmentCountRow
	if err := v.store.client.DirectoryDepartment.Query().
		Where(predicate).
		Select().
		Aggregate(ent.As(ent.Count(), "count")).
		Scan(ctx, &counts); err != nil {
		return DepartmentPage{}, fmt.Errorf("count current directory departments: %w", err)
	}
	if len(counts) != 1 {
		return DepartmentPage{}, fmt.Errorf("count current directory departments: expected one row, got %d", len(counts))
	}
	page := DepartmentPage{Items: []Department{}, Total: counts[0].Count}
	if query.Limit <= 0 || query.Offset < 0 || query.Offset >= page.Total {
		return page, nil
	}
	var rows []departmentPageRow
	if err := v.store.client.DirectoryDepartment.Query().
		Where(departmentPagePredicate(v.snapshot, query, true)).
		Offset(query.Offset).
		Limit(query.Limit).
		Select(
			directorydepartment.FieldID,
			directorydepartment.FieldExternalID,
			directorydepartment.FieldParentExternalID,
			directorydepartment.FieldEffectiveParentExternalID,
			directorydepartment.FieldName,
			directorydepartment.FieldPath,
			directorydepartment.FieldMetadata,
		).
		Scan(ctx, &rows); err != nil {
		return DepartmentPage{}, fmt.Errorf("list bounded current directory departments: %w", err)
	}
	for _, row := range rows {
		page.Items = append(page.Items, Department{
			ID:                        row.ID,
			ExternalID:                row.ExternalID,
			ParentExternalID:          row.ParentExternalID,
			EffectiveParentExternalID: row.EffectiveParentExternalID,
			Name:                      row.Name,
			Path:                      row.Path,
			Metadata:                  row.Metadata,
		})
	}
	return page, nil
}

func departmentPagePredicate(snapshot Snapshot, query DepartmentPageQuery, ordered bool) predicate.DirectoryDepartment {
	return func(selector *entsql.Selector) {
		selector.Where(entsql.EQ(selector.C(directorydepartment.FieldSourceID), snapshot.SourceID))
		selector.Where(entsql.EQ(selector.C(directorydepartment.FieldLastSeenRunID), snapshot.RunID))
		if query.ParentID == nil {
			if search := strings.TrimSpace(query.Search); search != "" {
				selector.Where(entsql.Or(
					entsql.ContainsFold(selector.C(directorydepartment.FieldName), search),
					entsql.ContainsFold(selector.C(directorydepartment.FieldExternalID), search),
				))
			}
		} else if parentID := strings.TrimSpace(*query.ParentID); parentID == "" {
			selector.Where(entsql.IsNull(selector.C(directorydepartment.FieldEffectiveParentExternalID)))
		} else {
			parent := entsql.Table(directorydepartment.Table).As("supplied_parent")
			selector.Where(entsql.EQ(selector.C(directorydepartment.FieldEffectiveParentExternalID), parentID))
			selector.Where(entsql.Exists(entsql.Select(parent.C(directorydepartment.FieldExternalID)).
				From(parent).
				Where(entsql.And(
					entsql.EQ(parent.C(directorydepartment.FieldSourceID), snapshot.SourceID),
					entsql.EQ(parent.C(directorydepartment.FieldLastSeenRunID), snapshot.RunID),
					entsql.EQ(parent.C(directorydepartment.FieldExternalID), parentID),
				))))
		}
		if ordered {
			selector.OrderExpr(entsql.Expr("LOWER(BTRIM(" + selector.C(directorydepartment.FieldName) + "))"))
			selector.OrderBy(selector.C(directorydepartment.FieldExternalID))
		}
	}
}

func (v *currentView) DepartmentAggregates(ctx context.Context, candidateIDs []string) (map[string]DepartmentAggregate, error) {
	candidateIDs = compactStrings(candidateIDs)
	if len(candidateIDs) == 0 {
		return map[string]DepartmentAggregate{}, nil
	}
	rows := make([]departmentAggregateRow, 0, len(candidateIDs))
	if err := v.store.client.DirectoryDepartment.Query().
		Where(departmentSummaryPredicate(v.snapshot, candidateIDs)).
		Select(directorydepartment.FieldExternalID).
		Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("summarize bounded current directory departments: %w", err)
	}
	out := make(map[string]DepartmentAggregate, len(rows))
	for _, row := range rows {
		out[row.ExternalID] = DepartmentAggregate{
			ChildCount:                 row.ChildCount,
			MemberCount:                row.MemberCount,
			MatchedUserCount:           row.MatchedUserCount,
			SubtreeMemberCount:         row.SubtreeMemberCount,
			SubtreeMatchedUserCount:    row.SubtreeMatchedUserCount,
			RepresentativeCount:        row.RepresentativeCount,
			MatchedRepresentativeCount: row.MatchedRepresentativeCount,
		}
	}
	return out, nil
}

func departmentSummaryPredicate(snapshot Snapshot, candidateIDs []string) predicate.DirectoryDepartment {
	return func(selector *entsql.Selector) {
		summaries := entsql.Table("department_summaries")
		selector.Prefix(entsql.ExprFunc(func(builder *entsql.Builder) {
			writeDepartmentSummaryCTEs(builder, snapshot, candidateIDs)
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

func writeDepartmentSummaryCTEs(builder *entsql.Builder, snapshot Snapshot, candidateIDs []string) {
	builder.WriteString(`WITH RECURSIVE
hierarchy_parameters(source_id, run_id, root_ids) AS MATERIALIZED (
  SELECT `)
	builder.Arg(snapshot.SourceID)
	builder.WriteString(`::bigint, `)
	builder.Arg(snapshot.RunID)
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
   AND child.last_seen_run_id = (SELECT run_id FROM hierarchy_parameters)
),
effective_assignments(root_external_id, member_id, matched_user_id, department_external_id) AS MATERIALIZED (
  SELECT descendants.root_external_id,
         member.id,
         member.matched_user_id,
         membership.department_external_id
  FROM descendants
  JOIN directory_member_departments AS membership
    ON membership.source_id = (SELECT source_id FROM hierarchy_parameters)
   AND membership.last_seen_run_id = (SELECT run_id FROM hierarchy_parameters)
   AND membership.department_external_id COLLATE "C" = descendants.external_id
  JOIN directory_members AS member
    ON member.source_id = (SELECT source_id FROM hierarchy_parameters)
   AND member.last_seen_run_id = (SELECT run_id FROM hierarchy_parameters)
   AND member.id = membership.directory_member_id
  UNION ALL
  SELECT descendants.root_external_id,
         member.id,
         member.matched_user_id,
         member.department_external_id
  FROM descendants
  JOIN directory_members AS member
    ON member.source_id = (SELECT source_id FROM hierarchy_parameters)
   AND member.last_seen_run_id = (SELECT run_id FROM hierarchy_parameters)
   AND member.department_external_id COLLATE "C" = descendants.external_id
  WHERE NOT EXISTS (
    SELECT 1
    FROM directory_member_departments AS current_membership
    WHERE current_membership.source_id = (SELECT source_id FROM hierarchy_parameters)
      AND current_membership.last_seen_run_id = (SELECT run_id FROM hierarchy_parameters)
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
   AND department.last_seen_run_id = (SELECT run_id FROM hierarchy_parameters)
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
   AND member.last_seen_run_id = (SELECT run_id FROM hierarchy_parameters)
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
    AND member.last_seen_run_id = (SELECT run_id FROM hierarchy_parameters)
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
   AND child.last_seen_run_id = (SELECT run_id FROM hierarchy_parameters)
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
