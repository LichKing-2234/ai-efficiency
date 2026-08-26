package directoryfacts

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/predicate"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/adminuseraccess"
)

var ErrInvalidLocalUserAccessStatus = errors.New("invalid directory facts local-user access status")

func (s *Store) LocalUsers(ctx context.Context, snapshot *Snapshot, request LocalUserQuery) (LocalUserPage, error) {
	if s == nil || s.client == nil {
		return LocalUserPage{}, fmt.Errorf("directory facts reader is not configured")
	}
	query := s.client.User.Query()
	if accessStatus := strings.TrimSpace(request.AccessStatus); accessStatus != "" {
		var err error
		query, err = adminuseraccess.ApplyFilter(query, accessStatus)
		if err != nil {
			return LocalUserPage{}, fmt.Errorf("%w: %v", ErrInvalidLocalUserAccessStatus, err)
		}
	}
	if search := strings.TrimSpace(request.Search); search != "" {
		query = query.Where(localUserSearchPredicate(search))
	}
	if departmentID := strings.TrimSpace(request.DepartmentID); departmentID != "" {
		if snapshot == nil || snapshot.SourceID <= 0 || snapshot.RunID <= 0 {
			return LocalUserPage{IDs: []int{}}, nil
		}
		query = query.Where(departmentUserPredicate(*snapshot, departmentID))
	}

	page := LocalUserPage{IDs: []int{}}
	offset := request.Offset
	if request.IncludeTotal {
		total, err := query.Clone().Count(ctx)
		if err != nil {
			return LocalUserPage{}, fmt.Errorf("count directory-scoped local users: %w", err)
		}
		page.Total = total
		if request.Limit <= 0 {
			return page, nil
		}
		if request.Page > 0 {
			pageCount := total / request.Limit
			if total%request.Limit != 0 {
				pageCount++
			}
			if total == 0 || request.Page-1 >= pageCount {
				return page, nil
			}
			offset = (request.Page - 1) * request.Limit
		}
		if offset < 0 || offset >= total {
			return page, nil
		}
	}
	if request.Limit <= 0 || offset < 0 {
		return page, nil
	}
	ids, err := query.Order(ent.Asc(entuser.FieldID)).Offset(offset).Limit(request.Limit).IDs(ctx)
	if err != nil {
		return LocalUserPage{}, fmt.Errorf("list directory-scoped local users: %w", err)
	}
	page.IDs = ids
	return page, nil
}

func localUserSearchPredicate(query string) predicate.User {
	predicates := []predicate.User{
		entuser.UsernameContainsFold(query),
		entuser.EmailContainsFold(query),
	}
	if value, err := strconv.Atoi(query); err == nil {
		predicates = append(predicates, entuser.IDEQ(value), entuser.RelayUserIDEQ(value))
	}
	return entuser.Or(predicates...)
}

func departmentUserPredicate(snapshot Snapshot, departmentID string) predicate.User {
	return func(selector *entsql.Selector) {
		selector.Where(entsql.P(func(builder *entsql.Builder) {
			builder.Ident(selector.C(entuser.FieldID)).WriteString(" IN (")
			writeDepartmentUserCTEs(builder, snapshot, departmentID)
			builder.WriteString("\nSELECT eligible_user_ids.user_id\nFROM eligible_user_ids")
			builder.WriteByte(')')
		}))
	}
}

func writeDepartmentUserCTEs(builder *entsql.Builder, snapshot Snapshot, departmentID string) {
	builder.WriteString(`WITH RECURSIVE
hierarchy_parameters(source_id, run_id, department_id) AS MATERIALIZED (
  SELECT `)
	builder.Arg(snapshot.SourceID)
	builder.WriteString(`::bigint, `)
	builder.Arg(snapshot.RunID)
	builder.WriteString(`::bigint, `)
	builder.Arg(departmentID)
	builder.WriteString(`::text
),
subtree(external_id) AS MATERIALIZED (
  SELECT root.external_id
  FROM directory_departments AS root
  JOIN hierarchy_parameters AS parameters
    ON parameters.source_id = root.source_id
   AND parameters.run_id = root.last_seen_run_id
   AND parameters.department_id = root.external_id
  UNION
  SELECT child.external_id
  FROM directory_departments AS child
  JOIN hierarchy_parameters AS parameters
    ON parameters.source_id = child.source_id
   AND parameters.run_id = child.last_seen_run_id
  JOIN subtree AS parent
    ON child.effective_parent_external_id = parent.external_id
),
eligible_members(id, matched_user_id, email_normalized) AS MATERIALIZED (
  SELECT member.id, member.matched_user_id, member.email_normalized
  FROM directory_members AS member
  JOIN hierarchy_parameters AS parameters
    ON parameters.source_id = member.source_id
   AND parameters.run_id = member.last_seen_run_id
  WHERE (
      EXISTS (
        SELECT 1
        FROM directory_member_departments AS membership
        JOIN hierarchy_parameters AS parameters
          ON parameters.source_id = membership.source_id
         AND parameters.run_id = membership.last_seen_run_id
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
           AND parameters.run_id = current_membership.last_seen_run_id
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
