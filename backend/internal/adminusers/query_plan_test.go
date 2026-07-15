package adminusers

import (
	"context"
	stdsql "database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	entdialect "entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directorymemberdepartment"
	"github.com/ai-efficiency/backend/ent/directorysyncrun"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/testdb"
	_ "github.com/lib/pq"
)

type targetPlanScale struct {
	name        string
	users       int
	members     int
	departments int
	memberships int
}

type departmentReadPlanRole string

const (
	departmentOptionCountRole          departmentReadPlanRole = "option count"
	departmentOptionPageRole           departmentReadPlanRole = "option page"
	departmentChildCountRole           departmentReadPlanRole = "child count"
	departmentChildPageRole            departmentReadPlanRole = "child page"
	departmentAncestorPresentationRole departmentReadPlanRole = "ancestor presentation"
	departmentFinalSummaryRole         departmentReadPlanRole = "final summary"
)

func TestDepartmentReadPlanCycleWalkBoundsAcrossScales(t *testing.T) {
	scales := []targetPlanScale{
		{name: "small", users: 24, members: 22, departments: 12, memberships: 36},
		{name: "large", users: 2400, members: 2200, departments: 120, memberships: 3600},
	}
	for _, scale := range scales {
		var wantAnchor string
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reverse=%v", scale.name, reverse), func(t *testing.T) {
				client, dsn := testdb.OpenWithDSN(t)
				sourceID := seedTargetPlanFixtureOrdered(t, client, scale, reverse)
				db, err := stdsql.Open("postgres", dsn)
				if err != nil {
					t.Fatalf("open diagnostic database: %v", err)
				}
				t.Cleanup(func() { _ = db.Close() })
				query := effectiveDepartmentCTEs("$1") + `
SELECT source_cardinality.row_count AS source_rows,
       (SELECT COUNT(*) FROM cycle_walk) AS cycle_walk_rows,
       COALESCE((SELECT MAX(CARDINALITY(cycle_walk.path_ids)) FROM cycle_walk), 0) AS max_cycle_walk_path,
       (SELECT COUNT(*) FROM cycle_anchors) AS cycle_anchor_rows
FROM source_cardinality`
				var sourceRows, cycleWalkRows, maxCycleWalkPath, cycleAnchorRows int
				if err := db.QueryRowContext(context.Background(), query, sourceID).Scan(&sourceRows, &cycleWalkRows, &maxCycleWalkPath, &cycleAnchorRows); err != nil {
					t.Fatalf("run department diagnostic: %v", err)
				}
				if sourceRows != scale.departments || cycleWalkRows > scale.departments*scale.departments || maxCycleWalkPath > scale.departments || cycleAnchorRows != 1 {
					t.Fatalf("diagnostic = source %d walk %d max_path %d anchors %d, want source %d/walk <= %d/max_path <= %d/anchors 1", sourceRows, cycleWalkRows, maxCycleWalkPath, cycleAnchorRows, scale.departments, scale.departments*scale.departments, scale.departments)
				}
				anchorQuery := effectiveDepartmentCTEs("$1") + ` SELECT external_id FROM cycle_anchors ORDER BY external_id`
				var anchor string
				if err := db.QueryRowContext(context.Background(), anchorQuery, sourceID).Scan(&anchor); err != nil {
					t.Fatalf("load cycle anchor: %v", err)
				}
				if anchor != "dept-cycle-a" {
					t.Fatalf("anchor = %q, want dept-cycle-a", anchor)
				}
				if wantAnchor == "" {
					wantAnchor = anchor
				} else if anchor != wantAnchor {
					t.Fatalf("anchor changed with insertion order: first=%q reverse=%q", wantAnchor, anchor)
				}
			})
		}
	}
}

func TestDepartmentReadPlanBoundedRolesAcrossScales(t *testing.T) {
	scales := []targetPlanScale{
		{name: "small", users: 24, members: 22, departments: 12, memberships: 36},
		{name: "large", users: 2400, members: 2200, departments: 120, memberships: 3600},
	}
	var optionRoleCount, childRoleCount int
	for _, scale := range scales {
		t.Run(scale.name, func(t *testing.T) {
			client, dsn := testdb.OpenWithDSN(t)
			sourceID := seedTargetPlanFixture(t, client, scale)
			assertTargetPlanFixtureCounts(t, client, scale)
			analyzeTargetPlanTables(t, dsn)

			optionClient, optionRecorder := newTargetQueryCaptureClient(t, dsn)
			options, err := NewService(optionClient).DepartmentOptions(context.Background(), DepartmentOptionRequest{PageSize: 100})
			if err != nil {
				t.Fatalf("DepartmentOptions: %v", err)
			}
			if len(options.Items) > 100 || (scale.departments == 120 && len(options.Items) != 100) {
				t.Fatalf("option items = %d, want bounded at 100 and full maximum for large fixture", len(options.Items))
			}
			optionQueries := optionRecorder.recordedQueries()
			if optionRoleCount == 0 {
				optionRoleCount = len(optionQueries)
			} else if len(optionQueries) != optionRoleCount {
				t.Fatalf("option SQL roles changed across scale: got %d want %d", len(optionQueries), optionRoleCount)
			}

			childClient, childRecorder := newTargetQueryCaptureClient(t, dsn)
			children, err := NewService(childClient).DepartmentChildren(context.Background(), DepartmentChildrenRequest{PageSize: 100})
			if err != nil {
				t.Fatalf("DepartmentChildren: %v", err)
			}
			if len(children.Items) > 100 || (scale.departments == 120 && len(children.Items) != 100) {
				t.Fatalf("child items = %d, want bounded at 100 and full maximum for large fixture", len(children.Items))
			}
			childQueries := childRecorder.recordedQueries()
			if childRoleCount == 0 {
				childRoleCount = len(childQueries)
			} else if len(childQueries) != childRoleCount {
				t.Fatalf("child SQL roles changed across scale: got %d want %d", len(childQueries), childRoleCount)
			}

			roles := append(capturedQueriesContaining(optionQueries, "WITH RECURSIVE"), capturedQueriesContaining(childQueries, "WITH RECURSIVE")...)
			if len(roles) == 0 {
				t.Fatal("no bounded department SQL roles captured")
			}
			var canonical string
			roleCounts := make(map[departmentReadPlanRole]int)
			for _, role := range roles {
				planRole := classifyDepartmentReadPlanRole(t, role.query)
				roleCounts[planRole]++
				sourcePlaceholder := placeholderForBoundValue(t, role.args, int64(sourceID))
				prefix := canonicalEffectivePrefix(t, role.query, sourcePlaceholder)
				if canonical == "" {
					canonical = prefix
				} else if prefix != canonical {
					t.Fatalf("department role shared prefix drifted\n--- got ---\n%s\n--- want ---\n%s", prefix, canonical)
				}
				if strings.Contains(role.query, `SELECT "directory_departments"."id", "directory_departments"."source_id"`) {
					t.Fatalf("bounded role selected full department entities:\n%s", role.query)
				}
				plan := explainTargetPlan(t, dsn, role.query, role.args)
				assertNamedRecursiveUnionLoopsOnce(t, plan, "cycle_walk")
				switch planRole {
				case departmentOptionPageRole, departmentChildPageRole, departmentFinalSummaryRole:
					if rows := planActualRows(t, plan); rows > 100 {
						t.Fatalf("%s top-level rows = %.0f, want <= 100; query=%s", planRole, rows, role.query)
					}
				}
				if planRole == departmentAncestorPresentationRole {
					assertNamedRecursiveUnionLoopsOnce(t, plan, "ancestors")
				}
				if planRole == departmentFinalSummaryRole {
					assertNamedRecursiveUnionLoopsOnce(t, plan, "descendants")
				}
			}
			wantRoleCounts := map[departmentReadPlanRole]int{
				departmentOptionCountRole:          1,
				departmentOptionPageRole:           1,
				departmentChildCountRole:           1,
				departmentChildPageRole:            1,
				departmentAncestorPresentationRole: 2,
				departmentFinalSummaryRole:         1,
			}
			if !reflect.DeepEqual(roleCounts, wantRoleCounts) {
				t.Fatalf("department SQL role counts = %v, want %v", roleCounts, wantRoleCounts)
			}

			if scale.departments == 120 {
				leafClient, leafRecorder := newTargetQueryCaptureClient(t, dsn)
				leafPage, err := NewService(leafClient).DepartmentChildren(context.Background(), DepartmentChildrenRequest{ParentDepartmentID: "dept-118", PageSize: 100})
				if err != nil {
					t.Fatalf("DepartmentChildren leaf parent: %v", err)
				}
				if got := departmentSummaryIDs(leafPage.Items); !reflect.DeepEqual(got, []string{"dept-119"}) {
					t.Fatalf("leaf child ids = %v, want dept-119", got)
				}
				summary := requireOneCapturedQuery(t, leafRecorder.recordedQueries(), "descendants(")
				plan := explainTargetPlan(t, dsn, summary.query, summary.args)
				assertNamedRecursiveUnionLoopsOnce(t, plan, "descendants")
				assertNamedPlanActualRowsBelow(t, plan, "CTE descendants", float64(scale.departments))
			}
		})
	}
}

func classifyDepartmentReadPlanRole(t *testing.T, query string) departmentReadPlanRole {
	t.Helper()
	switch {
	case strings.Contains(query, "ancestors("):
		return departmentAncestorPresentationRole
	case strings.Contains(query, "descendants("):
		return departmentFinalSummaryRole
	case strings.Contains(query, "filtered_departments AS MATERIALIZED"):
		if strings.Contains(query, " LIMIT ") {
			return departmentOptionPageRole
		}
		return departmentOptionCountRole
	case strings.Contains(query, "candidate_departments AS MATERIALIZED"):
		if strings.Contains(query, " LIMIT ") {
			return departmentChildPageRole
		}
		return departmentChildCountRole
	default:
		t.Fatalf("unclassified recursive department SQL role: %s", query)
		return ""
	}
}

func TestTargetPlanRecursiveRelationsRunOnceAcrossScales(t *testing.T) {
	scales := []targetPlanScale{
		{name: "small", users: 24, members: 22, departments: 12, memberships: 36},
		{name: "large", users: 2400, members: 2200, departments: 120, memberships: 3600},
	}

	for _, scale := range scales {
		t.Run(scale.name, func(t *testing.T) {
			client, dsn := testdb.OpenWithDSN(t)
			ctx := context.Background()
			sourceID := seedTargetPlanFixture(t, client, scale)
			assertTargetPlanFixtureCounts(t, client, scale)
			analyzeTargetPlanTables(t, dsn)

			captureClient, recorder := newTargetQueryCaptureClient(t, dsn)
			users, err := NewService(captureClient).Targets(ctx, Filters{DepartmentID: "dept-cycle-b"}, scale.users+1)
			if err != nil {
				t.Fatalf("Targets: %v", err)
			}
			if len(users) == 0 {
				t.Fatal("Targets returned no cycle-b users; fixture does not exercise the filtered statement")
			}
			query, args := recorder.targetQuery(t)

			sourcePlaceholder := placeholderForBoundValue(t, args, int64(sourceID))
			departmentPlaceholder := placeholderForBoundValue(t, args, "dept-cycle-b")
			assertExactEffectivePrefix(t, query, sourcePlaceholder, departmentPlaceholder)
			if !strings.Contains(query, `"users"."id" IN (WITH RECURSIVE`) {
				t.Fatalf("target predicate is not selector-qualified and uncorrelated:\n%s", query)
			}

			plan := explainTargetPlan(t, dsn, query, args)
			assertNamedRecursiveUnionLoopsOnce(t, plan, "cycle_walk")
			assertNamedRecursiveUnionLoopsOnce(t, plan, "subtree")
		})
	}
}

func TestCountPlanAndPagePlanReuseEffectivePredicatesAcrossScales(t *testing.T) {
	scales := []targetPlanScale{
		{name: "small", users: 24, members: 22, departments: 12, memberships: 36},
		{name: "large", users: 2400, members: 2200, departments: 120, memberships: 3600},
	}

	for _, scale := range scales {
		t.Run(scale.name, func(t *testing.T) {
			client, dsn := testdb.OpenWithDSN(t)
			sourceID := seedTargetPlanFixture(t, client, scale)
			assertTargetPlanFixtureCounts(t, client, scale)
			analyzeTargetPlanTables(t, dsn)

			captureClient, recorder := newTargetQueryCaptureClient(t, dsn)
			page, err := NewService(captureClient).List(context.Background(), ListRequest{
				Filters:  Filters{DepartmentID: "dept-cycle-b"},
				Page:     1,
				PageSize: 100,
			})
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(page.Users) == 0 || len(page.Users) > 100 {
				t.Fatalf("page users = %d, want between 1 and 100", len(page.Users))
			}
			if got := targetIDs(page.Users...); !sortIntsAscending(got) {
				t.Fatalf("page user ids are not stable ascending: %v", got)
			}

			queries := recorder.recordedQueries()
			assertListSQLRoleCount(t, queries, `FROM "directory_sources"`, 1)
			assertListSQLRoleCount(t, queries, `FROM "directory_sync_runs"`, 1)
			assertListSQLRoleCount(t, queries, `eligible_user_ids`, 2)
			assertListSQLRoleCount(t, queries, `FROM "directory_members"`, 1)
			assertListSQLRoleCount(t, queries, `FROM "directory_member_departments"`, 1)
			assertListSQLRoleCount(t, queries, `requested_candidates`, 1)
			assertListSQLRoleCount(t, queries, `FROM "directory_offboarding_actions"`, 1)
			if len(queries) != 8 {
				t.Fatalf("List SQL statements = %d, want constant 8 roles; queries=%s", len(queries), describeCapturedQueries(queries))
			}

			filtered := capturedQueriesContaining(queries, "eligible_user_ids")
			countQuery := requireOneCapturedQueryPrefix(t, filtered, `SELECT COUNT("users"."id")`)
			pageQuery := requireOneCapturedQueryPrefix(t, filtered, `SELECT "users"."id"`)
			if got, want := normalizedFilterFragment(pageQuery.query), normalizedFilterFragment(countQuery.query); got != want {
				t.Fatalf("count/page filter fragments differ\n--- count ---\n%s\n--- page ---\n%s", want, got)
			}
			if got, want := normalizedBoundValues(t, pageQuery.args), normalizedBoundValues(t, countQuery.args); !reflect.DeepEqual(got, want) {
				t.Fatalf("count/page args differ: count=%#v page=%#v", want, got)
			}
			if !strings.Contains(pageQuery.query, `ORDER BY "users"."id" ASC LIMIT 100`) {
				t.Fatalf("page query is not bounded and stably ordered:\n%s", pageQuery.query)
			}

			prefixes := make([]string, 0, 3)
			for _, role := range []capturedQuery{countQuery, pageQuery} {
				sourcePlaceholder := placeholderForBoundValue(t, role.args, int64(sourceID))
				departmentPlaceholder := placeholderForBoundValue(t, role.args, "dept-cycle-b")
				assertExactEffectivePrefix(t, role.query, sourcePlaceholder, departmentPlaceholder)
				prefixes = append(prefixes, canonicalEffectivePrefix(t, role.query, sourcePlaceholder))
				plan := explainTargetPlan(t, dsn, role.query, role.args)
				assertNamedRecursiveUnionLoopsOnce(t, plan, "cycle_walk")
				assertNamedRecursiveUnionLoopsOnce(t, plan, "subtree")
				if role.query == pageQuery.query {
					assertPlanMaterializesAtMost(t, plan, "Limit", 100)
				}
			}

			memberQuery := requireOneCapturedQuery(t, queries, `FROM "directory_members"`)
			if !strings.Contains(memberQuery.query, `SELECT "directory_members"."id", "directory_members"."matched_user_id", "directory_members"."email_normalized", "directory_members"."department_external_id"`) ||
				!strings.Contains(memberQuery.query, `"directory_members"."source_id" =`) ||
				!strings.Contains(memberQuery.query, `"directory_members"."matched_user_id" IN`) {
				t.Fatalf("page member query is not four-field and page-bounded:\n%s", memberQuery.query)
			}
			membershipQuery := requireOneCapturedQuery(t, queries, `FROM "directory_member_departments"`)
			if !strings.Contains(membershipQuery.query, `SELECT "directory_member_departments"."id", "directory_member_departments"."directory_member_id", "directory_member_departments"."department_external_id"`) ||
				!strings.Contains(membershipQuery.query, `"directory_member_departments"."directory_member_id" IN`) {
				t.Fatalf("membership query is not three-field and page-member-bounded:\n%s", membershipQuery.query)
			}

			ancestorQuery := requireOneCapturedQuery(t, queries, "requested_candidates")
			ancestorSourcePlaceholder := placeholderForBoundValue(t, ancestorQuery.args, int64(sourceID))
			prefixes = append(prefixes, canonicalEffectivePrefix(t, ancestorQuery.query, ancestorSourcePlaceholder))
			if strings.Count(ancestorQuery.query, "WITH RECURSIVE") != 1 ||
				strings.Count(ancestorQuery.query, "ancestors(") != 1 ||
				strings.Contains(ancestorQuery.query, "child.parent_external_id = parent.external_id") ||
				!strings.Contains(ancestorQuery.query, "child.effective_parent_external_id = parent.external_id") {
				t.Fatalf("ancestor query does not use one shared effective closure:\n%s", ancestorQuery.query)
			}
			ancestorPlan := explainTargetPlan(t, dsn, ancestorQuery.query, ancestorQuery.args)
			assertNamedRecursiveUnionLoopsOnce(t, ancestorPlan, "cycle_walk")
			assertNamedRecursiveUnionLoopsOnce(t, ancestorPlan, "ancestors")
			if got := planActualRows(t, ancestorPlan); scale.name == "large" && got >= float64(scale.departments) {
				t.Fatalf("large ancestor output rows = %.0f, want candidates plus ancestors below all %d departments", got, scale.departments)
			}
			for i := 1; i < len(prefixes); i++ {
				if prefixes[i] != prefixes[0] {
					t.Fatalf("shared effective prefix role %d drifted\n--- first ---\n%s\n--- got ---\n%s", i, prefixes[0], prefixes[i])
				}
			}
		})
	}
}

func TestListJoinIndexesExist(t *testing.T) {
	_, dsn := testdb.OpenWithDSN(t)
	db, err := stdsql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open index database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	assertIndexColumns := func(table string, columns ...string) {
		t.Helper()
		rows, err := db.QueryContext(context.Background(), `
SELECT indexdef
FROM pg_indexes
WHERE schemaname = current_schema()
  AND tablename = $1
`, table)
		if err != nil {
			t.Fatalf("list %s indexes: %v", table, err)
		}
		defer rows.Close()
		want := "(" + strings.Join(columns, ", ") + ")"
		for rows.Next() {
			var definition string
			if err := rows.Scan(&definition); err != nil {
				t.Fatalf("scan %s index: %v", table, err)
			}
			if strings.Contains(strings.ReplaceAll(definition, `"`, ""), want) {
				return
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate %s indexes: %v", table, err)
		}
		t.Fatalf("%s has no index on %s", table, strings.Join(columns, ", "))
	}

	assertIndexColumns(directorymember.Table, directorymember.FieldSourceID, directorymember.FieldMatchedUserID)
	assertIndexColumns(directorymemberdepartment.Table, directorymemberdepartment.FieldSourceID, directorymemberdepartment.FieldDirectoryMemberID, directorymemberdepartment.FieldDepartmentExternalID)
}

func assertExactEffectivePrefix(t *testing.T, query, sourcePlaceholder, departmentPlaceholder string) {
	t.Helper()
	start := strings.Index(query, "WITH RECURSIVE")
	if start < 0 {
		t.Fatalf("target SQL has no WITH RECURSIVE prefix:\n%s", query)
	}
	want := effectiveDepartmentCTEs(sourcePlaceholder) + effectiveSubtreeCTE(departmentPlaceholder)
	if len(query) < start+len(want) {
		t.Fatalf("target SQL shorter than shared effective prefix:\n%s", query)
	}
	if got := query[start : start+len(want)]; got != want {
		t.Fatalf("effective prefix drifted\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func assertNamedRecursiveUnionLoopsOnce(t *testing.T, plan any, cteName string) {
	t.Helper()
	wantName := "CTE " + cteName
	matches := make([]map[string]any, 0, 1)
	walkPlanJSON(plan, func(node map[string]any) {
		if node["Subplan Name"] == wantName {
			matches = append(matches, node)
		}
	})
	if len(matches) != 1 {
		t.Fatalf("plan nodes named %q = %d, want exactly 1; plan=%s", wantName, len(matches), compactPlanJSON(plan))
	}
	node := matches[0]
	if got := node["Node Type"]; got != "Recursive Union" {
		t.Fatalf("%s node type = %v, want Recursive Union", wantName, got)
	}
	if got := node["Actual Loops"]; got != float64(1) {
		t.Fatalf("%s actual loops = %v, want 1", wantName, got)
	}
}

func assertNamedPlanActualRowsBelow(t *testing.T, plan any, subplanName string, maximum float64) {
	t.Helper()
	matches := make([]map[string]any, 0, 1)
	walkPlanJSON(plan, func(node map[string]any) {
		if node["Subplan Name"] == subplanName {
			matches = append(matches, node)
		}
	})
	if len(matches) != 1 {
		t.Fatalf("plan nodes named %q = %d, want exactly 1", subplanName, len(matches))
	}
	rows, ok := matches[0]["Actual Rows"].(float64)
	if !ok || rows >= maximum {
		t.Fatalf("%s actual rows = %v, want < %.0f", subplanName, matches[0]["Actual Rows"], maximum)
	}
}

func walkPlanJSON(value any, visit func(map[string]any)) {
	switch value := value.(type) {
	case map[string]any:
		visit(value)
		for _, child := range value {
			walkPlanJSON(child, visit)
		}
	case []any:
		for _, child := range value {
			walkPlanJSON(child, visit)
		}
	}
}

func compactPlanJSON(plan any) string {
	data, err := json.Marshal(plan)
	if err != nil {
		return fmt.Sprintf("<marshal plan: %v>", err)
	}
	return string(data)
}

func explainTargetPlan(t *testing.T, dsn, query string, args []any) any {
	t.Helper()
	db, err := stdsql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open explain database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var raw []byte
	if err := db.QueryRowContext(
		context.Background(),
		"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query,
		args...,
	).Scan(&raw); err != nil {
		t.Fatalf("explain target query: %v\nSQL:\n%s\nargs=%#v", err, query, args)
	}
	var plan any
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatalf("decode target plan: %v; raw=%s", err, raw)
	}
	return plan
}

type targetQueryRecorder struct {
	entdialect.Driver
	mu      sync.Mutex
	query   string
	args    []any
	queries []capturedQuery
}

type capturedQuery struct {
	query string
	args  []any
}

func (r *targetQueryRecorder) Query(ctx context.Context, query string, args, rows any) error {
	values, _ := args.([]any)
	clonedArgs := append([]any(nil), values...)
	r.mu.Lock()
	r.queries = append(r.queries, capturedQuery{query: query, args: clonedArgs})
	if strings.Contains(query, "eligible_user_ids") {
		r.query = query
		r.args = clonedArgs
	}
	r.mu.Unlock()
	return r.Driver.Query(ctx, query, args, rows)
}

func (r *targetQueryRecorder) targetQuery(t *testing.T) (string, []any) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.query == "" {
		t.Fatal("filtered target SQL was not captured")
	}
	if r.args == nil {
		t.Fatal("filtered target SQL arguments were not captured as []any")
	}
	return r.query, append([]any(nil), r.args...)
}

func (r *targetQueryRecorder) recordedQueries() []capturedQuery {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]capturedQuery, len(r.queries))
	for i := range r.queries {
		out[i] = capturedQuery{query: r.queries[i].query, args: append([]any(nil), r.queries[i].args...)}
	}
	return out
}

func assertListSQLRoleCount(t *testing.T, queries []capturedQuery, fragment string, want int) {
	t.Helper()
	if got := len(capturedQueriesContaining(queries, fragment)); got != want {
		t.Fatalf("queries containing %q = %d, want %d; queries=%s", fragment, got, want, describeCapturedQueries(queries))
	}
}

func capturedQueriesContaining(queries []capturedQuery, fragment string) []capturedQuery {
	out := make([]capturedQuery, 0, len(queries))
	for _, query := range queries {
		if strings.Contains(query.query, fragment) {
			out = append(out, query)
		}
	}
	return out
}

func requireOneCapturedQuery(t *testing.T, queries []capturedQuery, fragment string) capturedQuery {
	t.Helper()
	matches := capturedQueriesContaining(queries, fragment)
	if len(matches) != 1 {
		t.Fatalf("queries containing %q = %d, want 1; queries=%s", fragment, len(matches), describeCapturedQueries(queries))
	}
	return matches[0]
}

func requireOneCapturedQueryPrefix(t *testing.T, queries []capturedQuery, prefix string) capturedQuery {
	t.Helper()
	matches := make([]capturedQuery, 0, 1)
	for _, query := range queries {
		if strings.HasPrefix(query.query, prefix) {
			matches = append(matches, query)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("queries beginning with %q = %d, want 1; queries=%s", prefix, len(matches), describeCapturedQueries(queries))
	}
	return matches[0]
}

func describeCapturedQueries(queries []capturedQuery) string {
	parts := make([]string, 0, len(queries))
	for i, query := range queries {
		parts = append(parts, fmt.Sprintf("%d:%s", i+1, strings.Join(strings.Fields(query.query), " ")))
	}
	return strings.Join(parts, " | ")
}

func normalizedFilterFragment(query string) string {
	start := strings.Index(query, " WHERE ")
	if start < 0 {
		return ""
	}
	end := len(query)
	for _, marker := range []string{" ORDER BY ", " LIMIT ", " OFFSET "} {
		if index := strings.Index(query[start:], marker); index >= 0 && start+index < end {
			end = start + index
		}
	}
	return query[start:end]
}

func normalizedBoundValues(t *testing.T, args []any) []any {
	t.Helper()
	out := make([]any, 0, len(args))
	for i, arg := range args {
		value := arg
		if valuer, ok := arg.(driver.Valuer); ok {
			var err error
			value, err = valuer.Value()
			if err != nil {
				t.Fatalf("resolve bound argument %d: %v", i+1, err)
			}
		}
		out = append(out, value)
	}
	return out
}

func canonicalEffectivePrefix(t *testing.T, query, sourcePlaceholder string) string {
	t.Helper()
	start := strings.Index(query, "WITH RECURSIVE")
	if start < 0 {
		t.Fatalf("query has no shared effective prefix:\n%s", query)
	}
	want := effectiveDepartmentCTEs(sourcePlaceholder)
	if len(query) < start+len(want) || query[start:start+len(want)] != want {
		t.Fatalf("query does not contain the exact effective department prefix:\n%s", query)
	}
	return strings.ReplaceAll(want, sourcePlaceholder, "$SOURCE")
}

func assertPlanMaterializesAtMost(t *testing.T, plan any, nodeType string, maximum float64) {
	t.Helper()
	found := false
	walkPlanJSON(plan, func(node map[string]any) {
		if node["Node Type"] == nodeType {
			found = true
			if rows, ok := node["Actual Rows"].(float64); !ok || rows > maximum {
				t.Fatalf("%s actual rows = %v, want <= %.0f", nodeType, node["Actual Rows"], maximum)
			}
		}
	})
	if !found {
		t.Fatalf("plan has no %s node: %s", nodeType, compactPlanJSON(plan))
	}
}

func planActualRows(t *testing.T, plan any) float64 {
	t.Helper()
	root, ok := plan.([]any)
	if !ok || len(root) != 1 {
		t.Fatalf("unexpected EXPLAIN root: %s", compactPlanJSON(plan))
	}
	entry, ok := root[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected EXPLAIN entry: %s", compactPlanJSON(plan))
	}
	rootPlan, ok := entry["Plan"].(map[string]any)
	if !ok {
		t.Fatalf("EXPLAIN entry has no root Plan: %s", compactPlanJSON(plan))
	}
	rows, ok := rootPlan["Actual Rows"].(float64)
	if !ok {
		t.Fatalf("EXPLAIN root has no Actual Rows: %s", compactPlanJSON(plan))
	}
	return rows
}

func sortIntsAscending(values []int) bool {
	for i := 1; i < len(values); i++ {
		if values[i-1] > values[i] {
			return false
		}
	}
	return true
}

func newTargetQueryCaptureClient(t *testing.T, dsn string) (*ent.Client, *targetQueryRecorder) {
	t.Helper()
	db, err := stdsql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open capture database: %v", err)
	}
	recorder := &targetQueryRecorder{Driver: entsql.OpenDB(entdialect.Postgres, db)}
	client := ent.NewClient(ent.Driver(recorder))
	t.Cleanup(func() { _ = client.Close() })
	return client, recorder
}

func placeholderForBoundValue(t *testing.T, args []any, want any) string {
	t.Helper()
	for i, arg := range args {
		value := arg
		if valuer, ok := arg.(driver.Valuer); ok {
			var err error
			value, err = valuer.Value()
			if err != nil {
				t.Fatalf("resolve bound argument %d: %v", i+1, err)
			}
		}
		if reflect.DeepEqual(value, want) || fmt.Sprint(value) == fmt.Sprint(want) {
			return fmt.Sprintf("$%d", i+1)
		}
	}
	t.Fatalf("bound value %#v not found in args %#v", want, args)
	return ""
}

func seedTargetPlanFixture(t *testing.T, client *ent.Client, scale targetPlanScale) int {
	return seedTargetPlanFixtureOrdered(t, client, scale, false)
}

func seedTargetPlanFixtureOrdered(t *testing.T, client *ent.Client, scale targetPlanScale, reverse bool) int {
	t.Helper()
	ctx := context.Background()
	staleSource, staleRun := createTargetSourceSnapshot(t, client, "Stale Plan Directory "+scale.name, time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC))
	createTargetDepartment(t, client, staleSource.ID, staleRun.ID, "dept-missing", "", "Stale Missing Parent")
	source, run := createTargetSourceSnapshot(t, client, "Plan Directory "+scale.name, time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC))
	if _, err := client.DirectorySyncRun.UpdateOneID(run.ID).
		SetDepartmentCount(scale.departments).
		SetMemberCount(scale.members).
		Save(ctx); err != nil {
		t.Fatalf("set plan run counts: %v", err)
	}

	userBuilders := make([]*ent.UserCreate, 0, scale.users)
	for i := 0; i < scale.users; i++ {
		userBuilders = append(userBuilders, client.User.Create().
			SetUsername(fmt.Sprintf("plan-user-%04d", i)).
			SetEmail(fmt.Sprintf("plan-user-%04d@example.com", i)).
			SetAuthSource(entuser.AuthSourceLdap).
			SetRole(entuser.RoleUser).
			SetRelayUserID(100000+i))
	}
	users, err := client.User.CreateBulk(userBuilders...).Save(ctx)
	if err != nil {
		t.Fatalf("create plan users: %v", err)
	}

	departmentIDs := make([]string, 0, scale.departments)
	departmentBuilders := make([]*ent.DirectoryDepartmentCreate, 0, scale.departments)
	for i := 0; i < scale.departments; i++ {
		externalID := fmt.Sprintf("dept-%03d", i)
		name := fmt.Sprintf("Department %03d", i)
		parentID := ""
		switch i {
		case 0:
			externalID, parentID, name = "dept-cycle-a", "dept-cycle-c", "Cycle Alpha"
		case 1:
			externalID, parentID, name = "dept-cycle-b", "dept-cycle-a", "Cycle Beta"
		case 2:
			externalID, parentID, name = "dept-cycle-c", "dept-cycle-b", "Cycle Gamma"
		case 3:
			externalID, parentID, name = "dept-orphan", "dept-missing", "Current Orphan"
		default:
			if i == scale.departments-1 {
				parentID = fmt.Sprintf("dept-%03d", i-1)
			}
		}
		departmentIDs = append(departmentIDs, externalID)
		builder := client.DirectoryDepartment.Create().
			SetSourceID(source.ID).
			SetExternalID(externalID).
			SetName(name).
			SetPath("synthetic/" + externalID).
			SetLastSeenRunID(run.ID)
		if parentID != "" {
			builder.SetParentExternalID(parentID)
		}
		departmentBuilders = append(departmentBuilders, builder)
	}
	if reverse {
		for left, right := 0, len(departmentBuilders)-1; left < right; left, right = left+1, right-1 {
			departmentBuilders[left], departmentBuilders[right] = departmentBuilders[right], departmentBuilders[left]
		}
	}
	if _, err := client.DirectoryDepartment.CreateBulk(departmentBuilders...).Save(ctx); err != nil {
		t.Fatalf("create plan departments: %v", err)
	}

	legacyOnlyCount := scale.members / 10
	if legacyOnlyCount < 1 {
		legacyOnlyCount = 1
	}
	memberBuilders := make([]*ent.DirectoryMemberCreate, 0, scale.members)
	memberEmails := make([]string, scale.members)
	memberDepartments := make([]string, scale.members)
	for i := 0; i < scale.members; i++ {
		legacyDepartmentID := departmentIDs[i%len(departmentIDs)]
		email := users[i].Email
		builder := client.DirectoryMember.Create().
			SetSourceID(source.ID).
			SetExternalID(fmt.Sprintf("plan-member-%04d", i)).
			SetDisplayName(fmt.Sprintf("Plan Member %04d", i)).
			SetDepartmentExternalID(legacyDepartmentID).
			SetLastSeenRunID(run.ID)
		if i%2 == 0 {
			email = fmt.Sprintf("directory-only-%04d@example.org", i)
			builder.SetMatchedUserID(users[i].ID)
		}
		memberEmails[i] = strings.ToLower(strings.TrimSpace(email))
		memberDepartments[i] = legacyDepartmentID
		memberBuilders = append(memberBuilders, builder.SetEmailNormalized(memberEmails[i]))
	}
	members, err := client.DirectoryMember.CreateBulk(memberBuilders...).Save(ctx)
	if err != nil {
		t.Fatalf("create plan members: %v", err)
	}

	currentMemberCount := scale.members - legacyOnlyCount
	membershipPairs := make([][2]int, 0, scale.memberships)
	seenPairs := make(map[[2]int]struct{}, scale.memberships)
	addPair := func(memberIndex, departmentIndex int) {
		pair := [2]int{memberIndex, departmentIndex}
		if _, exists := seenPairs[pair]; exists || len(membershipPairs) >= scale.memberships {
			return
		}
		seenPairs[pair] = struct{}{}
		membershipPairs = append(membershipPairs, pair)
	}
	for i := 0; i < currentMemberCount; i++ {
		addPair(i, i%len(departmentIDs))
	}
	for round := 1; len(membershipPairs) < scale.memberships; round++ {
		for memberIndex := 0; memberIndex < currentMemberCount && len(membershipPairs) < scale.memberships; memberIndex++ {
			addPair(memberIndex, (memberIndex+round)%len(departmentIDs))
		}
	}
	membershipBuilders := make([]*ent.DirectoryMemberDepartmentCreate, 0, len(membershipPairs))
	for _, pair := range membershipPairs {
		memberIndex, departmentIndex := pair[0], pair[1]
		member := members[memberIndex]
		membershipBuilders = append(membershipBuilders, client.DirectoryMemberDepartment.Create().
			SetSourceID(source.ID).
			SetDirectoryMemberID(member.ID).
			SetMemberExternalID(member.ExternalID).
			SetMemberEmailNormalized(memberEmails[memberIndex]).
			SetDepartmentExternalID(departmentIDs[departmentIndex]).
			SetLastSeenRunID(run.ID))
	}
	if _, err := client.DirectoryMemberDepartment.CreateBulk(membershipBuilders...).Save(ctx); err != nil {
		t.Fatalf("create plan memberships: %v", err)
	}
	return source.ID
}

func assertTargetPlanFixtureCounts(t *testing.T, client *ent.Client, scale targetPlanScale) {
	t.Helper()
	ctx := context.Background()
	got := targetPlanScale{
		name:        scale.name,
		users:       client.User.Query().CountX(ctx),
		members:     client.DirectoryMember.Query().CountX(ctx),
		departments: client.DirectoryDepartment.Query().CountX(ctx),
		memberships: client.DirectoryMemberDepartment.Query().CountX(ctx),
	}
	want := scale
	want.departments++ // The non-current source owns only the missing parent collision.
	if got != want {
		t.Fatalf("fixture counts = %s, want %s", describeTargetCounts(got.users, got.members, got.departments, got.memberships), describeTargetCounts(want.users, want.members, want.departments, want.memberships))
	}
}

func analyzeTargetPlanTables(t *testing.T, dsn string) {
	t.Helper()
	db, err := stdsql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open analyze database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, table := range []string{
		entuser.Table,
		directorydepartment.Table,
		directorymember.Table,
		directorymemberdepartment.Table,
		directorysyncrun.Table,
	} {
		if _, err := db.ExecContext(context.Background(), "ANALYZE "+table); err != nil {
			t.Fatalf("analyze %s: %v", table, err)
		}
	}
}
