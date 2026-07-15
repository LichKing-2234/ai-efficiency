package adminusers

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"

	"entgo.io/ent/dialect/sql"

	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directorymemberdepartment"
	"github.com/ai-efficiency/backend/ent/predicate"
	entuser "github.com/ai-efficiency/backend/ent/user"
)

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
             ORDER BY LOWER(BTRIM(department.name)), cycle_members.external_id
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
