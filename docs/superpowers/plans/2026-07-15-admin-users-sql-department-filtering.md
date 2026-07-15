# Administrator Users Bounded Browsing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** Draft revision 3 prepared from `docs/performance-contracts-116@5f6c58e`; independent revision-3 plan review, implementation, task reviews, repository verification, draft PR delivery, and all three CI rounds remain pending.

**Goal:** Make the complete `/admin/users` experience bounded: SQL-backed user count/page/filtering, page-local department enrichment, lightweight department selection, lazy child-at-a-time department navigation, shared current-filter mutation targets, and exactly one responsive user-row tree.

**Architecture:** Add one concrete `backend/internal/adminusers` read module that owns a single uncorrelated department-to-local-user relation, user list/target predicates, page-local enrichment, searchable department options, and paginated department children. Compose `adminsubscription.Service` with a narrow resolver in the handler package so persisted jobs and the legacy batch endpoint delegate `current_filter` to `adminusers.Service.Targets` without creating a package cycle. Migrate the frontend in the same platform release to the two bounded department APIs; keep the complete legacy `/departments` response for compatibility, but leave no active-page caller of it.

**Tech Stack:** Go 1.23/1.24, Gin, Ent 0.14, PostgreSQL 16, `lib/pq`, Vue 3 `<script setup lang="ts">`, TailwindCSS, Vitest, Vue Test Utils.

## Global Constraints

- Work from `docs/performance-contracts-116@5f6c58e6821dfcd95eefff14ea3426d454ae86cd`; do not stack on a sibling performance branch.
- Preserve `GET /api/v1/admin/users`, one-based `page`, `page_size`, `total`, the current response fields, and stable user `id ASC` ordering. User pages default to 20 and never exceed 100.
- Preserve `GET /api/v1/admin/users/departments` as a compatibility route with its existing complete response. The released `AdminUsersView` must not import or call it on default mount, filter use, department-view entry, or tree expansion.
- Add `GET /api/v1/admin/users/department-options?q=&selected_id=&page=&page_size=`. It defaults to page 1/size 20, caps size at 100, orders by normalized name then external ID, and returns only option identity plus display path.
- Add `GET /api/v1/admin/users/department-children?parent_department_id=&page=&page_size=`. An omitted parent lists roots, a supplied parent first requires an exact parent row in the resolved current source and then lists only its immediate effective children, size defaults to 25 and caps at 100, and order is normalized name then external ID. A parent that is missing from the current source returns an empty page even if its external ID exists in another source.
- Every page calculation handles nonpositive and maximum-integer values without offset overflow. Count first; if the requested page starts beyond total, return an empty page before calculating `(page-1)*page_size`.
- Default `/admin/users` mount starts user rows, subscription options, and latest-job recovery only. It makes zero request to the complete department snapshot; bounded options load only when the picker opens or an existing `department_id` needs its label, and bounded roots load only when department view is active.
- Resolve the current directory source through `directorysync.CurrentSourceID`; do not infer it from source update time or a non-current run. One `List`, `DepartmentOptions`, or `DepartmentChildren` call resolves the current source at most once and reuses it through that call.
- Department filtering resolves the requested node and descendants with one uncorrelated recursive relation per SQL statement. The recursive node must execute once in each count, page, and target statement, independently of outer user count.
- Current `directory_member_departments` rows are authoritative when any exist for a member. The legacy `directory_members.department_external_id` is eligible only when that member has no current membership rows.
- A qualifying directory member maps to every local user identified by positive `matched_user_id` or normalized email `LOWER(BTRIM(users.email)) = directory_members.email_normalized`; union and deduplicate local user IDs.
- With no department filter, unmatched local users remain visible. With a department filter, unmatched local users are excluded because no current directory evidence places them in the selected subtree.
- Search preserves case-insensitive username/email matching and numeric local-user/Relay-user ID matching. Access-status predicates remain owned by `backend/internal/adminuseraccess`.
- Count, page, persisted-job `current_filter`, and compatibility-batch `current_filter` use the exact same `adminusers.Filters` normalization and SQL predicate. Both mutation routes fetch at most 501 ordered users and reject more than 500 before relay mutation or job creation.
- User-page enrichment starts from at most 100 users. It may read only their matching current members, those members' candidate memberships, those candidate departments, and their ancestors; it must not materialize the complete department/member/membership snapshot in Go.
- For page enrichment, load all bounded candidate department IDs before choosing a display department. With current membership rows, choose the current primary if it exists, otherwise the first ordered existing current membership, skipping dangling IDs. Only a member with zero current membership rows may fall back to its first existing legacy primary department.
- Every department ancestor read constrains both the recursive CTE and the outer `directory_departments` query by the same resolved `source_id`; equal external IDs in another source must never affect names, parents, depth, or display paths.
- Department navigation projects one source-scoped effective parent relation. Null/blank/missing current-source parents and exactly one deterministic anchor per closed cycle have no effective parent; the anchor is the cycle row ordered first by `LOWER(BTRIM(name)), external_id`. Root/child candidate count and page, `child_count`, ancestor display/depth, and descendant summaries all use that same relation. The stored `parent_external_id` remains the response fact, but the removed anchor edge is never returned by expansion, so every closed-cycle component is reachable from one root and no expansion repeats its anchor.
- Department-option display paths and department-child display paths/summary counts are computed in SQL or a bounded page-local read. Child, direct member, matched-user, subtree member, subtree matched-user, representative, and matched-representative counts must not require loading all entities into application memory. Representative totals use one current-source-scoped `UNION` of page-department `metadata.representative_external_ids` and current-member `metadata.leader_department_ids`, deduplicated by `(department_external_id, representative_external_id)` before matched-member evaluation.
- Frontend department navigation loads roots and immediate children separately. It never recursively preloads the full tree; collapsed children remain cached locally, and a parent with more children exposes bounded continuation rather than an implicit complete fetch.
- The frontend mounts either mobile user cards or the desktop user table at the existing 768px breakpoint, never both. Selection, dialogs, filters, pagination, keyboard behavior, and subscription operations remain available.
- Keep handlers thin, API calls under `frontend/src/api`, and package dependencies acyclic. Do not introduce Redis, a cache service, a new process, direct `sub2api` coupling, or Relay/provider interface changes.
- Scope no-full-snapshot assertions strictly to `adminusers.Service.List`, `DepartmentOptions`, `DepartmentChildren`, the persisted-job `ScopeCurrentFilter` path, and the compatibility-batch `current_filter` path. The unchanged `AdminUsersHandler.ListDepartments` complete-snapshot implementation is explicitly exempt and remains available only for compatibility; prove separately that no active frontend view or component imports or calls `listAdminUserDepartments`.
- Add only indexes justified by the implemented joins: current source plus matched user, and current source plus member plus department. Regenerate Ent and prove a second generation produces no tracked drift.
- Use a 24-user/12-department small PostgreSQL fixture and a 2,400-user/120-department large fixture for cross-scale query-plan assertions. Do not gate on elapsed time, cost estimates, exact buffers, or a complete planner tree.
- PostgreSQL `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)`, browser role E2E, listener ownership, and embedded-build checks are environment-sensitive evidence and remain separate from ordinary unit-test results.
- Tests, fixtures, SQL diagnostics, docs, users, emails, directory IDs, paths, credentials, tokens, provider names, and URLs use synthetic values such as `alice@example.com`, `bob@example.org`, `dept-alpha`, `Group Alpha`, and `https://relay.example.com`.
- Update `docs/architecture.md` and `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md` only after behavior lands. Add the active performance spec to the architecture source-of-truth list. In the performance spec's `Related Documents and Supersession` section, add an administrator-users subsection that links `2026-06-22-configurable-directory-sync-design.md`, preserves its organization business/count semantics, and explicitly supersedes or extends only its full-snapshot loading and administrator read-API clauses. Do not rewrite the 2026-06-22 Directory Sync spec or other historical admin-user specs.
- Each implementation task gets an exact-range SPEC review and standards review before its ledger checkpoint. Resolve every Critical and Important finding before starting the next task.
- The draft PR targets `docs/performance-contracts-116`, links issue #134 and draft PR #138, stays open and draft, and is not merged, tagged, released, deployed, or used for Helm work.
- Maintain this file as a live ledger. Check only actions actually run, keep its English `Status` consistent with unchecked work, and never mark a CI round complete before the corresponding live PR-head checks are green.
- Delivery requires three green CI rounds: implementation/documentation head, round-one-evidence head, and final-ledger head. The final-head observation is reported externally without creating a fourth self-invalidating ledger commit.

## Deep Module Interface

`backend/internal/adminusers` exposes one concrete read service. The handler does not learn its CTEs or enrichment query shape:

```go
package adminusers

type Filters struct {
    Query        string
    DepartmentID string
    AccessStatus string
}

type ListRequest struct {
    Filters  Filters
    Page     int
    PageSize int
}

type Department struct {
    ExternalID  string
    Name        string
    Path        string
    DisplayPath string
}

type Page struct {
    Users               []*ent.User
    Total               int
    Page                int
    PageSize            int
    DepartmentsByUserID map[int]*Department
    OffboardingByUserID map[int]adminuseraccess.OffboardingFact
}

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

var ErrInvalidAccessStatus = errors.New("invalid admin user access status")

func NewService(client *ent.Client) *Service
func (s *Service) List(ctx context.Context, request ListRequest) (*Page, error)
func (s *Service) Targets(ctx context.Context, filters Filters, limit int) ([]*ent.User, error)
func (s *Service) DepartmentOptions(ctx context.Context, request DepartmentOptionRequest) (*DepartmentOptionPage, error)
func (s *Service) DepartmentChildren(ctx context.Context, request DepartmentChildrenRequest) (*DepartmentChildrenPage, error)
```

`backend/internal/adminsubscription` defines the consumer-owned injection boundary. It does not import `adminusers`; the handler composition root converts the DTOs:

```go
package adminsubscription

type CurrentFilter struct {
    Query        string
    DepartmentID string
    AccessStatus string
}

type CurrentFilterTargetResolver interface {
    ResolveCurrentFilterTargets(ctx context.Context, filter CurrentFilter, limit int) ([]*ent.User, error)
}

type CurrentFilterTargetResolverFunc func(context.Context, CurrentFilter, int) ([]*ent.User, error)

func (f CurrentFilterTargetResolverFunc) ResolveCurrentFilterTargets(
    ctx context.Context,
    filter CurrentFilter,
    limit int,
) ([]*ent.User, error) {
    return f(ctx, filter, limit)
}

func NewService(client *ent.Client, resolvers ...CurrentFilterTargetResolver) *Service
```

The optional constructor argument preserves selected/all-mapped test and compatibility setup. `ScopeCurrentFilter` without a resolver returns a wrapped configuration error; production always supplies the resolver.

## HTTP Contracts

The user list contract remains unchanged. The new bounded department routes return the standard `pkg.Success` envelope with these data payloads:

```http
GET /api/v1/admin/users/department-options?q=alpha&selected_id=dept-beta&page=1&page_size=20
```

```json
{
  "items": [
    {"external_id": "dept-alpha", "name": "Department Alpha", "display_path": "Company / Department Alpha"}
  ],
  "selected": {"external_id": "dept-beta", "name": "Department Beta", "display_path": "Company / Department Beta"},
  "total": 1,
  "page": 1,
  "page_size": 20
}
```

`selected_id` is an exact, current-source-scoped lookup returned separately and does not change `items` or `total`. Missing/current-source-absent selections return `selected: null`.

```http
GET /api/v1/admin/users/department-children?parent_department_id=dept-alpha&page=1&page_size=25
```

```json
{
  "items": [
    {
      "external_id": "dept-alpha-one",
      "parent_external_id": "dept-alpha",
      "name": "Team One",
      "path": "1.10.20",
      "display_path": "Company / Department Alpha / Team One",
      "depth": 2,
      "child_count": 0,
      "has_children": false,
      "member_count": 8,
      "matched_user_count": 7,
      "subtree_member_count": 8,
      "subtree_matched_user_count": 7,
      "representative_count": 1,
      "matched_representative_count": 1
    }
  ],
  "parent_department_id": "dept-alpha",
  "total": 1,
  "page": 1,
  "page_size": 25
}
```

For root requests, `parent_department_id` is omitted and the response returns it as `""`. A department whose parent ID is missing from the current source is treated as a root, matching the current tree behavior. A supplied parent must exist in the resolved current source before any child is eligible: missing parents return an empty 200 page even when a non-current source has the same external ID. Every closed cycle contributes exactly one deterministic synthetic root anchor; expansion follows the effective parent relation and never returns that anchor through its stored parent, so a root-and-expand walk emits each cycle department once.

## File Map

- `backend/internal/adminusers/service.go`: exported service/types, filter normalization, shared user query, list paging, and targets.
- `backend/internal/adminusers/department.go`: uncorrelated subtree/user relation, page-user enrichment, ancestor closure, department options, source-scoped parent validation, deterministic cycle-anchor/effective-parent relation, department children, and deduplicated representative summary aggregates.
- `backend/internal/adminusers/service_test.go`: semantic mapping matrix, list/target parity, page normalization, source selection, and enrichment fallbacks.
- `backend/internal/adminusers/department_test.go`: option/child paging, current-source parent validation, root/orphan/cycle navigation, representative union parity, display paths, and compatibility fixtures.
- `backend/internal/adminusers/query_plan_test.go`: small/large PostgreSQL fixtures, SQL-role capture, projection/materialization bounds, target/descendant/cycle recursive-loop assertions, and cycle-walk cardinality gates.
- `backend/ent/schema/directory_member.go`: current-source/matched-user composite index.
- `backend/ent/schema/directory_member_department.go`: current-source/member/department composite index.
- `backend/ent/`: regenerated Ent schema/migration artifacts.
- `backend/internal/adminsubscription/job.go`: injected current-filter resolver and removal of duplicated directory filtering.
- `backend/internal/adminsubscription/job_test.go`: resolver delegation, no-resolver error, snapshots, selected/all-mapped compatibility, and 501st-target rejection.
- `backend/internal/handler/admin_users.go`: thin list/bounded-department HTTP mapping, composition-root resolver, compatibility-batch delegation, and unchanged exempt `ListDepartments` compatibility route.
- `backend/internal/handler/admin_users_test.go`: list wire contract plus bounded option/child parent, cycle, and representative route contracts; legacy `ListDepartments` remains separately covered.
- `backend/internal/handler/admin_users_subscription_test.go`: real-HTTP parity across persisted jobs and compatibility batch.
- `backend/internal/handler/router.go`: two new admin-only department routes.
- `frontend/src/api/adminUsers.ts`: bounded department option/child wrappers; legacy full route remains exported only for compatibility.
- `frontend/src/types/index.ts`: exact option, option-page, child-page, and summary DTOs.
- `frontend/src/components/admin/AdminDepartmentPicker.vue`: lazy searchable/paged department filter.
- `frontend/src/__tests__/admin-department-picker.test.ts`: picker request, paging, stale response, selected deep-link, and clear behavior.
- `frontend/src/views/admin/AdminUsersView.vue`: lazy child navigation, no full-snapshot mount, and one active responsive row tree.
- `frontend/src/__tests__/admin-users-view.test.ts`: request/DOM bounds, lazy tree interaction, and unchanged workflow regression.
- `docs/architecture.md`: current bounded admin-user list, navigation, target-resolution relationships, and active performance-spec source-of-truth entry.
- `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md`: landed administrator collection bounds, mapping/fallback semantics, and explicit inheritance/supersession relationship to the 2026-06-22 Directory Sync contract.
- `docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md`: live execution ledger.

---

### Task 1: Build One Uncorrelated Department-To-User Predicate

**Files:**
- Create: `backend/internal/adminusers/service.go`
- Create: `backend/internal/adminusers/department.go`
- Create: `backend/internal/adminusers/service_test.go`
- Create: `backend/internal/adminusers/query_plan_test.go`
- Maintain: `docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md`

**Interfaces:**
- Consumes: `directorysync.CurrentSourceID`, `adminuseraccess.ApplyFilter`, Ent user predicates, and current directory tables.
- Produces: `NewService`, `Filters`, private `resolvedSource`, private `filteredUsersQuery`, and `Targets(ctx, filters, limit)` from the Deep Module Interface.

- [ ] **Step 1: Add the semantic fixture and failing target tests**

Create one current successful full-company snapshot with this exact shape:

```text
dept-alpha -> dept-alpha-one; sibling dept-beta
alice: positive matched_user_id, membership dept-alpha-one
bob: nil matched_user_id, member email bob@example.org, local email Bob@Example.org, membership dept-alpha-one
carol: memberships dept-alpha-one and dept-beta, legacy primary dept-alpha
dave: membership dept-beta, legacy primary dept-alpha-one (legacy must not leak)
erin: no current memberships, legacy primary dept-alpha-one
frank: unmatched local user
grace: one member whose positive matched_user_id maps grace and whose normalized email maps a second local user; both users are eligible and IDs deduplicate
```

Add table-driven `TestTargetsDepartmentMappingMatrix` cases for direct node, ancestor subtree, sibling exclusion, multi-membership, current-membership authority, legacy fallback, positive matched ID, mixed-case normalized email, dual mapping, no filter with unmatched local user, filtered unmatched exclusion, unknown department, no current source, search intersection, access-status intersection, invalid access status, positive limit, and canceled context. Compare exact `id ASC` target IDs.

Seed two query-plan sizes with the same logical proportions:

```text
small: 24 users, 22 members, 12 departments, 36 memberships
large: 2,400 users, 2,200 members, 120 departments, 3,600 memberships
both: 10% unmatched users, mixed ID/email mappings, multi-membership rows, legacy-only rows
```

Capture the filtered target SQL and bound arguments for both sizes. Add `assertRecursiveUnionLoopsOnce` that walks the decoded JSON plan, finds exactly one `Recursive Union` node, and requires `Actual Loops == 1`.

- [ ] **Step 2: Run the focused tests and record RED**

Run:

```bash
(cd backend && go test ./internal/adminusers -run 'TestTargets|TestTargetPlan' -count=1 -v)
```

Expected: FAIL because `backend/internal/adminusers` and the shared target predicate do not exist.

- [ ] **Step 3: Implement the uncorrelated relation and target reader**

Normalize `Query`, `DepartmentID`, and `AccessStatus` once. Validate access status before composing SQL, wrap `ErrInvalidAccessStatus`, and retain the current user-facing validation message. Resolve the source once only when a department predicate needs it.

Build the department filter as one selector-qualified `users.id IN (...)` predicate with this PostgreSQL 16 shape:

```sql
users.id IN (
  WITH RECURSIVE subtree(external_id) AS MATERIALIZED (
    SELECT root.external_id
    FROM directory_departments AS root
    WHERE root.source_id = $1
      AND root.external_id = $2
    UNION
    SELECT child.external_id
    FROM directory_departments AS child
    JOIN subtree AS parent
      ON child.parent_external_id = parent.external_id
    WHERE child.source_id = $1
  ),
  eligible_members(id, matched_user_id, email_normalized) AS MATERIALIZED (
    SELECT member.id, member.matched_user_id, member.email_normalized
    FROM directory_members AS member
    WHERE member.source_id = $1
      AND (
        EXISTS (
          SELECT 1
          FROM directory_member_departments AS membership
          JOIN subtree
            ON subtree.external_id = membership.department_external_id
          WHERE membership.source_id = $1
            AND membership.directory_member_id = member.id
        )
        OR (
          NOT EXISTS (
            SELECT 1
            FROM directory_member_departments AS current_membership
            WHERE current_membership.source_id = $1
              AND current_membership.directory_member_id = member.id
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
  )
  SELECT eligible_user_ids.user_id
  FROM eligible_user_ids
)
```

Use Ent table/field constants for every interpolated identifier. Bind only runtime values. Use `UNION`, not `UNION ALL`, for cycle termination and user-ID deduplication. The CTE contains no reference to the outer `users` alias, so recursion is not evaluated per candidate user.

When no current source exists, use an always-false user predicate. `Targets` applies the exact normalized search/access/department predicates, orders by `users.id ASC`, requires a positive `limit`, and returns no more than that limit.

- [ ] **Step 4: Prove target semantics and recursive structural bounds GREEN**

Run:

```bash
(cd backend && gofmt -w internal/adminusers/*.go)
(cd backend && go test ./internal/adminusers -run 'TestTargets|TestTargetPlan' -count=2 -v)
(cd backend && go test ./internal/adminuseraccess ./internal/adminusers -count=1)
git diff --check
```

Expected: all semantic cases pass twice; target IDs are stable; both small and large target plans contain one recursive union with one actual loop; no test depends on duration, cost, or exact buffers.

- [ ] **Step 5: Complete exact-range Task 1 reviews and checkpoint**

Generate an ignored base-to-working-tree package. Obtain independent SPEC and standards reviews for Task 1, resolve every Critical/Important finding, rerun Step 4, then commit:

```bash
git add backend/internal/adminusers docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "perf(admin-users): evaluate department targets in SQL"
```

After the commit exists, check only completed Task 1 steps and record the ledger in a separate commit:

```bash
git add docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "docs(plan): record admin user target task"
```

---

### Task 2: Return Bounded User Pages And Correct Page-Local Departments

**Files:**
- Modify: `backend/internal/adminusers/service.go`
- Modify: `backend/internal/adminusers/department.go`
- Modify: `backend/internal/adminusers/service_test.go`
- Modify: `backend/internal/adminusers/query_plan_test.go`
- Modify: `backend/ent/schema/directory_member.go`
- Modify: `backend/ent/schema/directory_member_department.go`
- Regenerate: `backend/ent/`
- Modify: `backend/internal/handler/admin_users.go`
- Modify: `backend/internal/handler/admin_users_test.go`
- Maintain: `docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md`

**Interfaces:**
- Consumes: Task 1 `filteredUsersQuery`, `Filters`, and `Targets`.
- Produces: `List(ctx, ListRequest) (*Page, error)` and thin unchanged `GET /api/v1/admin/users` HTTP mapping.

- [ ] **Step 1: Add failing list, enrichment, HTTP, and plan tests**

Add service/handler tests for default page 1/size 20, maximum size 100, nonpositive normalization, stable `id ASC`, exact total/page predicate parity, empty results, canceled context, and a maximum-integer page returning empty before offset calculation. Require the unchanged HTTP envelope and row fields, including encrypted relay-password ciphertext in the API, derived access/offboarding status, and page-local department display.

Extend the semantic fixture with:

```text
colliding source: dept-alpha and dept-alpha-one exist under another source with different names/parents
dangling current membership: first ordered ID dept-aaa-missing, later existing dept-alpha-one
dangling current primary: primary dept-aaa-missing is present in memberships, later existing dept-alpha-one
all current memberships dangling plus existing legacy primary: no department is displayed because current rows remain authoritative
no current memberships plus existing legacy primary: legacy department is displayed
```

Assert selection order is member ID ascending; within a member, primary-if-current then department external ID and membership row ID. A dangling candidate is skipped only after the candidate department set has been loaded. Apply one chosen member department to both its positive-ID and normalized-email page users, without duplicate assignment.

Capture filtered count and page SQL for both 24-user and 2,400-user fixtures. For each SQL role, run `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` with its recorded arguments and require exactly one `Recursive Union` node with `Actual Loops == 1`.

- [ ] **Step 2: Run Task 2 tests and record RED**

Run:

```bash
(cd backend && go test ./internal/adminusers ./internal/handler -run 'TestList|TestAdminUsersList|TestCountPlan|TestPagePlan' -count=1 -v)
```

Expected: FAIL because `List`, bounded enrichment, new indexes, and thin handler integration are absent.

- [ ] **Step 3: Implement count/page and candidate-first enrichment**

`List` normalizes page values, resolves the source once, builds one shared filtered query, clones it for count, and returns an empty page before any offset multiplication when `(page-1) >= ceil(total/page_size)`. Otherwise it orders by ID, applies the at-most-100 limit/offset, and requests offboarding facts only for page IDs.

For department enrichment:

1. Load current members matching page positive user IDs or page normalized emails. Select only member ID, matched user ID, normalized email, and legacy primary; order by member ID.
2. Load current membership rows only for those member IDs. Select row ID, member ID, and department external ID; order by member ID, department external ID, then row ID.
3. Build each member's bounded candidate IDs. If current memberships exist, put primary first only when it occurs in those rows, then append all ordered current memberships. If none exist, use only the legacy primary.
4. Load the union of candidate departments plus ancestors for the resolved source. Do not choose a candidate before this existence read.
5. For each member, choose the first candidate present in the current-source department map. Never use legacy primary when current membership rows exist, even if every current candidate is dangling.
6. Apply the first chosen department by member ID order to every matching page user, preserving positive-ID and normalized-email mappings with user-ID deduplication.

The ancestor query must constrain the outer table and recursive relation explicitly:

```sql
SELECT outer_department.external_id,
       outer_department.parent_external_id,
       outer_department.name,
       outer_department.path
FROM directory_departments AS outer_department
WHERE outer_department.source_id = $1
  AND outer_department.external_id IN (
    WITH RECURSIVE ancestors(external_id, parent_external_id) AS MATERIALIZED (
      SELECT seed.external_id, seed.parent_external_id
      FROM directory_departments AS seed
      WHERE seed.source_id = $1
        AND seed.external_id = ANY($2)
      UNION
      SELECT parent.external_id, parent.parent_external_id
      FROM directory_departments AS parent
      JOIN ancestors AS child
        ON child.parent_external_id = parent.external_id
      WHERE parent.source_id = $1
    )
    SELECT ancestors.external_id FROM ancestors
  )
```

Bind the same resolved source to every source position. Use `pq.Array(candidateIDs)` instead of expanding one bind per ID. Build `display_path` from this page-local closure only.

Add these Ent indexes and regenerate:

```go
index.Fields("source_id", "matched_user_id")
index.Fields("source_id", "directory_member_id", "department_external_id")
```

Replace handler list filtering/enrichment with `h.users.List`; retain HTTP parsing/row mapping and the existing 400/500 behavior.

- [ ] **Step 4: Prove page and database-work bounds GREEN**

For small and large fixtures, require constant SQL roles for `adminusers.Service.List`: current-source resolution, count, bounded page, page members, page memberships, candidate/ancestor departments, and page offboarding facts. Assert:

```text
count/page share byte-equivalent normalized filter fragments and arguments
count/page recursive union count = 1 and Actual Loops = 1
page Limit materializes at most 100 user rows in id ASC order
member lookup starts from page IDs/emails and projects four fields
membership lookup starts from page member IDs and projects three fields
ancestor outer query includes source_id equality and never reads the colliding source row
candidate/ancestor output is candidates plus ancestors, not all 120 departments
adminusers.Service.List has no full-table All for departments, members, or memberships
this gate does not inspect or change the explicitly exempt AdminUsersHandler.ListDepartments compatibility snapshot
```

Run:

```bash
(cd backend && gofmt -w internal/adminusers/*.go internal/handler/admin_users.go internal/handler/admin_users_test.go ent/schema/directory_member.go ent/schema/directory_member_department.go)
(cd backend && go generate ./ent)
git add backend/ent
(cd backend && go generate ./ent)
git diff --exit-code -- backend/ent
(cd backend && go test ./internal/adminusers ./internal/handler -run 'TestList|TestAdminUsersList|TestCountPlan|TestPagePlan' -count=2 -v)
(cd backend && go test ./internal/adminusers ./internal/handler -count=1)
git diff --check
```

Expected: semantic and HTTP tests pass twice, generated Ent drift is empty, both fixture scales prove one recursion per count/page statement, and the colliding/dangling fixtures select only valid current-source departments.

- [ ] **Step 5: Complete exact-range Task 2 reviews and checkpoint**

Obtain independent Task 2 SPEC and standards reviews, resolve every Critical/Important finding, rerun Step 4, then commit implementation and ledger separately:

```bash
git add backend/internal/adminusers backend/internal/handler/admin_users.go backend/internal/handler/admin_users_test.go backend/ent docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "perf(admin-users): page directory-enriched users"
git add docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "docs(plan): record admin user page task"
```

---

### Task 3: Add Bounded Department Options And Lazy Child Reads

**Files:**
- Modify: `backend/internal/adminusers/department.go`
- Create: `backend/internal/adminusers/department_test.go`
- Modify: `backend/internal/adminusers/query_plan_test.go`
- Modify: `backend/internal/handler/admin_users.go`
- Modify: `backend/internal/handler/admin_users_test.go`
- Modify: `backend/internal/handler/router.go`
- Maintain: `docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md`

**Interfaces:**
- Consumes: Task 2 source resolution and current membership/ancestor helpers.
- Produces: `DepartmentOptions`, `DepartmentChildren`, and the two HTTP contracts above.

- [ ] **Step 1: Add failing option, child-page, summary, and scale tests**

At service and real HTTP seams, cover:

```text
options default page 1/20; size 0 -> 20; size 101 -> 100
options normalized-name/external-ID order, q filtering, second page, overflow page
selected_id returned separately even when it is outside q/page; unknown selected_id -> null
children root default page 1/25; size 101 -> 100; second root page; overflow page
supplied current-source parent returns immediate effective children only; unknown parent -> empty 200
current-source orphan dept-orphan names missing parent dept-missing; a non-current source owns dept-missing
dept-orphan appears once in roots; requesting dept-missing returns empty at both service and HTTP seams
closed cycle dept-cycle-a (Cycle Alpha) <- dept-cycle-b (Cycle Beta) <- dept-cycle-c (Cycle Gamma) <- dept-cycle-a chooses dept-cycle-a by normalized name/external ID
root contains dept-cycle-a once; expanding a returns b, b returns c, and c does not return a
the complete root/expand walk reaches every cycle row once with no duplicate external ID and is stable across insertion order
child/direct/subtree/matched counts match the legacy summary fixture over the effective navigation edges
representatives: department-only matched/unmatched, leader-only matched/unmatched, and duplicate-in-both matched
representative union returns total 5/matched 3 once at service and HTTP seams; non-current collisions do not change it
display paths/depth use the current source despite colliding external IDs elsewhere
```

Use these exact representative facts for one requested page department; repeat colliding external IDs and leader metadata under a non-current source with opposite match state:

```text
rep-department-matched: listed only in department representative_external_ids; current member matched_user_id > 0
rep-department-unmatched: listed only in department representative_external_ids; current member matched_user_id nil
rep-leader-matched: absent from department metadata; current member leader_department_ids contains the department; matched_user_id > 0
rep-leader-unmatched: absent from department metadata; current member leader_department_ids contains the department; matched_user_id nil
rep-duplicate: listed in department metadata and declared by its own leader_department_ids; matched_user_id > 0; counted once
```

Reserve three current-source department rows in each 12/120-department plan fixture for the exact `dept-cycle-a/b/c` cycle above, keeping the fixture totals unchanged; include `dept-orphan -> dept-missing` in each current source and place `dept-missing` only in a separate non-current source. For both PostgreSQL fixture scales, execute a diagnostic projection over the exact production cycle CTEs and assert `source_rows = N`, `cycle_walk_rows <= N*N`, `max_cycle_walk_path <= N`, `cycle_anchor_rows = 1`, anchor ID `dept-cycle-a`, and the same anchor after reverse insertion order. For the 2,400-user/120-department fixture, require option/child result slices to stay at their requested maximum, query role count to stay constant versus the small fixture, and no active bounded query to return full member/membership/department entities. Walk the JSON plans and require the top-level option-page, child-page, and final-summary nodes to report at most 100 actual rows. On a leaf-parent request, require descendant rows to stay below the full 120-department fixture. Require the named cycle-walk and multi-seed descendant `Recursive Union` nodes to each have `Actual Loops == 1` per statement, never once per returned department.

Name the focused service cases `TestDepartmentChildrenRequiresCurrentSourceParent`, `TestDepartmentChildrenClosedCycleNavigation`, and `TestDepartmentChildrenRepresentativeUnion`; name their real-HTTP counterparts with the `TestAdminUsersDepartmentChildren` prefix so the Step 2/5 commands execute every new seam explicitly. Extend the JSON-plan helper to identify the recursive node whose `Subplan Name` is `CTE cycle_walk` and require exactly one such node with `Actual Loops == 1`.

- [ ] **Step 2: Run Task 3 tests and record RED**

Run:

```bash
(cd backend && go test ./internal/adminusers ./internal/handler -run 'TestDepartmentOptions|TestDepartmentChildren|TestAdminUsersDepartmentOptions|TestAdminUsersDepartmentChildren|TestDepartmentReadPlan' -count=1 -v)
```

Expected: FAIL because the bounded department service methods and routes do not exist.

- [ ] **Step 3: Implement the lightweight selector page**

`DepartmentOptions` resolves one source, normalizes page values, and applies `q` to trimmed department name or external ID. It counts and pages by `LOWER(BTRIM(name)) ASC, external_id ASC`, selects only external ID/name/parent ID, and loads ancestors for at most 100 option rows plus the optional exact `selected_id` row. Its ancestor display paths use the same effective parent relation defined below, so closed-cycle labels agree with lazy navigation.

The selected exact lookup is source-scoped and independent from the option query. Build option display paths from the bounded ancestor closure. A page beyond total returns empty before offset calculation; current-source absence returns an empty page and `Selected == nil`.

- [ ] **Step 4: Implement child-at-a-time summaries with SQL aggregates**

`DepartmentChildren` trims the parent request and binds SQL `$2` as nullable text: omitted/blank becomes `NULL` for a root request; a nonblank value is a supplied parent. Candidate count and page statements use this exact current-source navigation prefix. The cycle walk follows at most `N` distinct parent rows per seed and emits at most `N*N` rows for `N` current-source departments:

```sql
WITH RECURSIVE
source_departments(
  external_id,
  parent_external_id,
  name,
  path,
  metadata
) AS MATERIALIZED (
  SELECT department.external_id,
         department.parent_external_id,
         department.name,
         department.path,
         department.metadata
  FROM directory_departments AS department
  WHERE department.source_id = $1
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
),
supplied_parent(external_id) AS MATERIALIZED (
  SELECT parent.external_id
  FROM source_departments AS parent
  WHERE $2::text IS NOT NULL
    AND parent.external_id = $2
),
candidate_departments AS MATERIALIZED (
  SELECT candidate.*
  FROM navigation_departments AS candidate
  WHERE (
      $2::text IS NULL
      AND candidate.effective_parent_external_id IS NULL
    )
    OR (
      $2::text IS NOT NULL
      AND EXISTS (SELECT 1 FROM supplied_parent)
      AND candidate.effective_parent_external_id = (
        SELECT supplied_parent.external_id FROM supplied_parent
      )
    )
)
SELECT candidate_departments.external_id,
       candidate_departments.parent_external_id,
       candidate_departments.name,
       candidate_departments.path
FROM candidate_departments
ORDER BY LOWER(BTRIM(candidate_departments.name)), candidate_departments.external_id
LIMIT $3
```

Use the same `candidate_departments` relation for total and page roles; apply the existing overflow-before-offset rule around it. The resolved source is present only in `source_departments`, so a same-ID parent from another source cannot validate `$2`. `navigation_departments` converts current-source orphans and one ordered row per closed cycle to effective roots. Keep the stored `parent_external_id` in `DepartmentSummary`, but use only `effective_parent_external_id` for root/child selection, child counts, ancestor depth/display path, and descendant traversal. This removes the anchor's back edge and makes the effective relation a forest without changing Directory Sync apply/schema contracts.

In `query_plan_test.go`, replace only the final candidate projection with this diagnostic projection while reusing the byte-identical production CTE prefix through `cycle_anchors`:

```sql
SELECT source_cardinality.row_count AS source_rows,
       (SELECT COUNT(*) FROM cycle_walk) AS cycle_walk_rows,
       COALESCE(
         (SELECT MAX(CARDINALITY(cycle_walk.path_ids)) FROM cycle_walk),
         0
       ) AS max_cycle_walk_path,
       (SELECT COUNT(*) FROM cycle_anchors) AS cycle_anchor_rows
FROM source_cardinality
```

Compare `cycle_anchor_rows` with independently seeded expected components, assert the exact anchor IDs from a separate `SELECT external_id FROM cycle_anchors ORDER BY external_id`, and run the same diagnostics after reverse-order fixture insertion. These are structural correctness gates, not elapsed-time assertions.

After the at-most-100 candidate page is known, bind its external IDs as one array and build one multi-seed recursive descendant relation `(root_external_id, external_id)` over the exact prefix above through `navigation_departments`; omit only the candidate-statement `supplied_parent` and `candidate_departments` CTEs. Append the following CTEs in that same `WITH RECURSIVE` statement. The summary core is uncorrelated and source scoped:

```sql
, requested_roots(root_external_id) AS MATERIALIZED (
  SELECT UNNEST($2::text[])
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
    ON membership.source_id = $1
   AND membership.department_external_id = descendants.external_id
  JOIN directory_members AS member
    ON member.source_id = $1
   AND member.id = membership.directory_member_id
  UNION ALL
  SELECT descendants.root_external_id,
         member.id,
         member.matched_user_id,
         member.department_external_id
  FROM descendants
  JOIN directory_members AS member
    ON member.source_id = $1
   AND member.department_external_id = descendants.external_id
  WHERE NOT EXISTS (
    SELECT 1
    FROM directory_member_departments AS current_membership
    WHERE current_membership.source_id = $1
      AND current_membership.directory_member_id = member.id
  )
)
SELECT requested_roots.root_external_id,
       COUNT(DISTINCT effective_assignments.member_id)
         FILTER (WHERE effective_assignments.department_external_id = requested_roots.root_external_id) AS member_count,
       COUNT(DISTINCT effective_assignments.matched_user_id)
         FILTER (WHERE effective_assignments.department_external_id = requested_roots.root_external_id
                   AND effective_assignments.matched_user_id > 0) AS matched_user_count,
       COUNT(DISTINCT effective_assignments.member_id) AS subtree_member_count,
       COUNT(DISTINCT effective_assignments.matched_user_id)
         FILTER (WHERE effective_assignments.matched_user_id > 0) AS subtree_matched_user_count
FROM requested_roots
LEFT JOIN effective_assignments
  ON effective_assignments.root_external_id = requested_roots.root_external_id
GROUP BY requested_roots.root_external_id
```

Use `navigation_departments.effective_parent_external_id` for a sibling grouped child-count relation, so the removed cycle-anchor edge cannot make the anchor reappear as a child. Insert these representative CTEs between the closing `effective_assignments` CTE and the summary `SELECT` in the same statement. The scalar-or-array normalization matches the existing compatibility helper, both inputs are current-source scoped, and `UNION` deduplicates representatives declared in both metadata sources:

```sql
, department_representatives(root_external_id, representative_external_id) AS MATERIALIZED (
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
    ON member.source_id = $1
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
  WHERE member.source_id = $1
    AND BTRIM(member.external_id) <> ''
    AND member.matched_user_id > 0
),
representative_counts(
  root_external_id,
  representative_count,
  matched_representative_count
) AS MATERIALIZED (
  SELECT requested_roots.root_external_id,
         COUNT(representatives.representative_external_id),
         COUNT(matched_representative_ids.representative_external_id)
  FROM requested_roots
  LEFT JOIN representatives
    ON representatives.root_external_id = requested_roots.root_external_id
  LEFT JOIN matched_representative_ids
    ON matched_representative_ids.representative_external_id = representatives.representative_external_id
  GROUP BY requested_roots.root_external_id
)
```

Join only the grouped/scalar rows needed for:

```text
effective-edge child_count and has_children
direct distinct member count under authoritative current-membership/legacy fallback rules
direct distinct positive matched_user_id count
subtree distinct member count deduplicated across multi-memberships
subtree distinct positive matched_user_id count
deduplicated page-department plus leader-only representative count
matched representative count by current-source member external_id with positive matched_user_id
effective-ancestor-derived depth and display_path
```

Perform these aggregations in PostgreSQL and scan only scalar summary rows into Go. Bind the candidate IDs as one `pq.Array`; constrain every base department/member/membership relation by the resolved source. `HasChildren` is exactly `ChildCount > 0`. Do not load representative members or metadata collections into Go and do not issue a query per requested root.

Add thin `ListDepartmentOptions` and `ListDepartmentChildren` handler methods, parse the documented parameters, map the exact DTO fields, and register:

```go
adminUsersGroup.GET("/department-options", adminUsersHandler.ListDepartmentOptions)
adminUsersGroup.GET("/department-children", adminUsersHandler.ListDepartmentChildren)
```

Leave `adminUsersGroup.GET("/departments", adminUsersHandler.ListDepartments)` and its response untouched for compatibility.

- [ ] **Step 5: Verify backend department reads GREEN**

Run:

```bash
(cd backend && gofmt -w internal/adminusers/*.go internal/handler/admin_users.go internal/handler/admin_users_test.go internal/handler/router.go)
(cd backend && go test ./internal/adminusers ./internal/handler -run 'TestDepartmentOptions|TestDepartmentChildren|TestAdminUsersDepartmentOptions|TestAdminUsersDepartmentChildren|TestDepartmentReadPlan' -count=2 -v)
(cd backend && go test ./internal/adminusers ./internal/handler -count=1)
git diff --check
```

Expected: exact bounds and summaries pass twice; missing/colliding supplied parents return empty; each closed cycle has one stable root anchor and a duplicate-free expansion; representative union reports exact department-only/leader-only/deduplicated matched totals; no active bounded route materializes the full snapshot; source collisions cannot alter parents, representatives, or display paths; legacy `/departments` tests still pass unchanged.

- [ ] **Step 6: Complete exact-range Task 3 reviews and checkpoint**

Obtain independent Task 3 SPEC and standards reviews. Require the reviewers to inspect the exact supplied-parent source predicate, cycle-walk cardinality/anchor/effective-edge SQL, representative metadata `UNION`, service plus HTTP fixtures, and `ListDepartments` compatibility exemption. Resolve every Critical/Important finding, rerun Step 5, then commit implementation and ledger separately:

```bash
git add backend/internal/adminusers backend/internal/handler/admin_users.go backend/internal/handler/admin_users_test.go backend/internal/handler/router.go docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "perf(admin-users): bound department browsing"
git add docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "docs(plan): record admin department read task"
```

---

### Task 4: Delegate Both Current-Filter Mutation Paths To Adminusers

**Files:**
- Modify: `backend/internal/adminsubscription/job.go`
- Modify: `backend/internal/adminsubscription/job_test.go`
- Modify: `backend/internal/handler/admin_users.go`
- Modify: `backend/internal/handler/admin_users_subscription_test.go`
- Maintain: `docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md`

**Interfaces:**
- Consumes: Task 1 `adminusers.Service.Targets` and `Filters`.
- Produces: the `adminsubscription.CurrentFilterTargetResolver` boundary and exact persisted-job/compatibility-batch target parity.

- [ ] **Step 1: Add failing resolver and two-route parity tests**

In `job_test.go`, add a recording resolver and prove `StartJob`:

```text
passes trimmed q/department/access values and limit 501 for ScopeCurrentFilter
stores ordered target IDs and immutable target snapshots from resolver users
returns the resolver error without creating a job
returns a configuration error when ScopeCurrentFilter has no resolver
retains selected IDs/missing snapshots and all-mapped behavior without a resolver
rejects 501 resolver users before job creation
```

In `admin_users_subscription_test.go`, drive both `POST /admin/users/subscription-jobs` and `POST /admin/users/subscriptions/batch` through one table fixture. For each filter, compare the persisted target snapshot or fake relay target IDs with the corresponding complete `GET /admin/users` IDs across all pages:

```text
positive matched_user_id
mixed-case normalized email
one member with both mappings
multi-membership and ancestor subtree
current membership overriding a conflicting legacy primary
legacy primary when no current memberships exist
search plus department plus access status
unknown department
unmatched user with and without a department filter
exactly 500 targets
501 targets rejected with 422 and zero relay calls/job rows
```

- [ ] **Step 2: Run Task 4 tests and record RED**

Run:

```bash
(cd backend && go test ./internal/adminsubscription ./internal/handler -run 'TestStartJobCurrentFilterResolver|TestAdminUsers(CurrentFilter|StartSubscriptionJob|BatchManageSubscriptions)' -count=1 -v)
```

Expected: FAIL because persisted jobs and the compatibility endpoint still own duplicate current-filter logic.

- [ ] **Step 3: Inject the resolver without a package cycle**

Add the consumer-owned `CurrentFilter`, `CurrentFilterTargetResolver`, and function adapter from the Deep Module Interface to `adminsubscription/job.go`. Store the optional resolver on `Service`; keep the variadic constructor source-compatible.

For `ScopeCurrentFilter`, call:

```go
users, err := s.currentFilterTargets.ResolveCurrentFilterTargets(ctx, CurrentFilter{
    Query:        req.FilterQuery,
    DepartmentID: req.DepartmentID,
    AccessStatus: req.AccessStatus,
}, MaxTargets+1)
```

Reject `len(users) > MaxTargets`, then snapshot those ordered users exactly as selected/all-mapped paths do. Remove `adminsubscription` search, source/tree, membership-set, and directory-member-to-user helpers only after no call site remains in that package; this does not affect compatibility helpers owned by `AdminUsersHandler.ListDepartments`.

In `NewAdminUsersHandler`, build one `userReader := adminusers.NewService(entClient)`. Store it on `AdminUsersHandler` and create the job service with this composition-root adapter:

```go
adminsubscription.NewService(entClient, adminsubscription.CurrentFilterTargetResolverFunc(
    func(ctx context.Context, filter adminsubscription.CurrentFilter, limit int) ([]*ent.User, error) {
        return userReader.Targets(ctx, adminusers.Filters{
            Query:        filter.Query,
            DepartmentID: filter.DepartmentID,
            AccessStatus: filter.AccessStatus,
        }, limit)
    },
))
```

For the legacy `subscriptionTargetsForScope` current-filter case, call the same `h.users.Targets(..., adminsubscription.MaxTargets+1)`, preserve the 500 rejection/status, and convert users to existing compatibility rows. Remove only handler helpers whose remaining callers belonged to `List` or the two migrated current-filter paths. Retain every tree/member/representative helper still used by the unchanged `ListDepartments` route. Keep selected/all-mapped behavior local to their existing owners.

Map `adminusers.ErrInvalidAccessStatus` to the existing 400 response in both endpoints. Database/resolver failures remain 500; oversize remains 422.

- [ ] **Step 4: Verify one target contract GREEN**

Run:

```bash
(cd backend && gofmt -w internal/adminsubscription/job.go internal/adminsubscription/job_test.go internal/handler/admin_users.go internal/handler/admin_users_subscription_test.go)
(cd backend && go test ./internal/adminsubscription ./internal/adminusers ./internal/handler -run 'TestStartJobCurrentFilterResolver|TestAdminUsers(CurrentFilter|StartSubscriptionJob|BatchManageSubscriptions)|TestTargets' -count=2 -v)
(cd backend && go test ./internal/adminsubscription ./internal/adminusers ./internal/handler -count=1)
git diff --check
```

Expected: both current-filter routes share exact normalized target IDs for the complete matrix and call `adminusers.Service.Targets`; 501 targets are rejected before mutation/persistence; the `adminsubscription.Service.StartJob` `ScopeCurrentFilter` branch and `AdminUsersHandler.subscriptionTargetsForScope` `current_filter` branch contain no second directory snapshot/filter implementation. This assertion explicitly excludes the unchanged `AdminUsersHandler.ListDepartments` compatibility snapshot.

- [ ] **Step 5: Complete exact-range Task 4 reviews and checkpoint**

Obtain independent Task 4 SPEC and standards reviews, resolve every Critical/Important finding, rerun Step 4, then commit implementation and ledger separately:

```bash
git add backend/internal/adminsubscription/job.go backend/internal/adminsubscription/job_test.go backend/internal/handler/admin_users.go backend/internal/handler/admin_users_subscription_test.go docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "refactor(admin-users): share current filter targets"
git add docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "docs(plan): record admin target integration task"
```

---

### Task 5: Migrate The Active Page To Bounded Department Reads And One Row Tree

**Files:**
- Modify: `frontend/src/api/adminUsers.ts`
- Modify: `frontend/src/types/index.ts`
- Create: `frontend/src/components/admin/AdminDepartmentPicker.vue`
- Create: `frontend/src/__tests__/admin-department-picker.test.ts`
- Modify: `frontend/src/views/admin/AdminUsersView.vue`
- Modify: `frontend/src/__tests__/admin-users-view.test.ts`
- Maintain: `docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md`

**Interfaces:**
- Consumes: Task 3 option/child HTTP contracts and the unchanged list contract.
- Produces: a bounded searchable filter, lazy immediate-child navigation, and one viewport-active user-row subtree.

- [ ] **Step 1: Add failing API, picker, mount, navigation, and DOM tests**

Add API/type assertions for exact paths/parameters and these frontend regressions:

```text
default /admin/users mount with a 120-department legacy-route fake never calls listAdminUserDepartments, department-options, or department-children
mount with department_id performs at most one options request with selected_id and page_size 20
opening the picker requests options page 1/20; search is trimmed/debounced and stale responses cannot replace newer results
picker next/previous uses server page/total; clear emits empty department ID and one list reload
default user mount still requests list users, subscription options, and latest job once each
mount or switch to ?view=departments requests root children page 1/25 only
expanding one parent requests only that parent's immediate children page 1/25
collapse/re-expand reuses loaded children; load-more requests only the next page and appends by external ID
opening a department switches to user view and applies its external ID without a full tree request
unknown/empty child pages render stable empty states
```

Provide a controllable `matchMedia('(min-width: 768px)')` fake. With 100 users assert exactly 100 `[data-admin-user-row]` nodes under either `data-admin-user-list="mobile"` or `"desktop"`, never 200; a media change swaps trees without a new list request or duplicate selection; unmount removes the exact listener; department view contains no hidden user row tree.

Retain regression coverage for search debounce, URL filters, page size, paging, selection, dialogs, password reveal, disable access, subscription scopes/jobs, keyboard activation, and Chinese/English labels.

- [ ] **Step 2: Run focused frontend tests and record RED**

Run:

```bash
(cd frontend && npm test -- src/__tests__/admin-department-picker.test.ts src/__tests__/admin-users-view.test.ts)
```

Expected: FAIL because the active view still loads `/departments`, uses a complete `<select>`/tree, and mounts both responsive user representations.

- [ ] **Step 3: Add exact API wrappers and picker DTOs**

In `frontend/src/api/adminUsers.ts`, add:

```ts
export interface AdminDepartmentOptionsParams {
  q?: string
  selected_id?: string
  page?: number
  page_size?: number
}

export interface AdminDepartmentChildrenParams {
  parent_department_id?: string
  page?: number
  page_size?: number
}

export function listAdminUserDepartmentOptions(params: AdminDepartmentOptionsParams) {
  return client.get<ApiResponse<AdminDepartmentOptionsResponse>>('/admin/users/department-options', { params })
}

export function listAdminUserDepartmentChildren(params: AdminDepartmentChildrenParams) {
  return client.get<ApiResponse<AdminDepartmentChildrenResponse>>('/admin/users/department-children', { params })
}
```

Add TypeScript DTOs matching the HTTP Contracts exactly. Keep `listAdminUserDepartments` exported for compatibility, but remove it from `AdminUsersView` imports and runtime calls.

Implement `AdminDepartmentPicker` with `modelValue: string`, `update:modelValue`, and `change` interfaces. It owns dropdown/search/page state, uses a monotonically increasing request generation, loads no options for an empty closed picker, resolves an initial nonempty selection through `selected_id`, and exposes explicit all-departments, previous, and next controls.

- [ ] **Step 4: Replace full-tree state with lazy child pages**

In `AdminUsersView.vue`, replace the complete `departments` array and `visibleDepartments` scan with:

```ts
type LoadedDepartmentChildren = {
  items: AdminDirectoryDepartmentSummary[]
  page: number
  page_size: number
  total: number
}

type VisibleDepartmentRow = {
  department: AdminDirectoryDepartmentSummary
  depth: number
}

const rootDepartments = ref<LoadedDepartmentChildren | null>(null)
const childrenByParentID = ref<Map<string, LoadedDepartmentChildren>>(new Map())
const expandedDepartmentIds = ref<Set<string>>(new Set())
const visibleDepartmentRows = computed<VisibleDepartmentRow[]>(() => flattenLoadedDepartmentRows(
  rootDepartments.value?.items ?? [],
  childrenByParentID.value,
  expandedDepartmentIds.value,
))
```

Implement `flattenLoadedDepartmentRows` as a depth-first walk over only the root page and already loaded child pages. It emits each loaded external ID once, descends only through expanded IDs, and stops a cycle through a per-walk visited set. Load roots only when department view is active. Expansion loads one parent page if absent; collapse changes visibility only; continuation requests page `current.page + 1` and appends new external IDs. Render `visibleDepartmentRows` through the existing single department-row markup and expose root previous/next plus per-parent load-more controls. Preserve `aria-level`, `aria-expanded`, toggle keyboard isolation, counts, display paths, and drill-in behavior.

On default mount, do not start any department request. If the route initially selects department view, start root page 1/25. If only `department_id` is present, let the picker resolve that one selected label without loading roots.

- [ ] **Step 5: Mount exactly one responsive user representation**

Create one `MediaQueryList` per component instance, mirror `.matches` into a ref, and add/remove the same `change` callback in lifecycle. Gate the existing card/table branches with stable computed values:

```ts
const showMobileUserRows = computed(() => filters.view === 'users' && rows.value.length > 0 && !desktopUserRows.value)
const showDesktopUserRows = computed(() => filters.view === 'users' && rows.value.length > 0 && desktopUserRows.value)
```

Add stable `data-admin-user-list` and `data-admin-user-row` selectors. Keep the existing row/card contents, selection bindings, dialogs, and 768px visual breakpoint.

- [ ] **Step 6: Verify focused/full frontend and production build GREEN**

Run:

```bash
(cd frontend && npm test -- src/__tests__/admin-department-picker.test.ts src/__tests__/admin-users-view.test.ts)
(cd frontend && npm test)
(cd frontend && npm run build)
! rg -n "listAdminUserDepartments" frontend/src/views frontend/src/components
git diff --check
```

Expected: focused/full Vitest and build pass; default mount has zero complete-snapshot requests; option/child collections honor their pages; one viewport mounts one row per user; all retained workflows pass.

- [ ] **Step 7: Complete exact-range Task 5 reviews and checkpoint**

Obtain independent Task 5 SPEC and standards reviews, resolve every Critical/Important finding, rerun Step 6, then commit implementation and ledger separately:

```bash
git add frontend/src/api/adminUsers.ts frontend/src/types/index.ts frontend/src/components/admin/AdminDepartmentPicker.vue frontend/src/__tests__/admin-department-picker.test.ts frontend/src/views/admin/AdminUsersView.vue frontend/src/__tests__/admin-users-view.test.ts docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "perf(frontend): bound admin department browsing"
git add docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "docs(plan): record admin users frontend task"
```

---

### Task 6: Document, Verify, Review, And Deliver Through Three CI Heads

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md`
- Maintain: `docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md`
- Review only: every file changed since `5f6c58e6821dfcd95eefff14ea3426d454ae86cd`

**Interfaces:**
- Consumes: reviewed Tasks 1-5.
- Produces: current architecture/contract truth, full verification evidence, final reviews, an open draft PR, and three green CI rounds on three explicit heads.

- [ ] **Step 1: Update only current architecture and the active performance contract**

In `docs/architecture.md`, insert `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md` as the newest entry in `## Source-of-Truth Order` -> `Topic-specific current specs`. Update the `/admin/users` architecture paragraph and module table with:

```text
adminusers owns uncorrelated SQL subtree-to-user eligibility
list count/page and current-filter targets share one predicate
page enrichment is capped by 100 users and candidate/ancestor closure
department options are 20 default/100 maximum
department navigation is immediate-child 25 default/100 maximum
the compatibility full department route has no current frontend caller
persisted and compatibility current-filter mutations use one target reader
the frontend loads department data on demand and mounts one responsive row tree
missing supplied parents are validated only in the resolved current source
closed cycles expose one deterministic effective root and expand without duplicates
representative totals union and deduplicate department and member-leader metadata in the current source
```

In `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md`, add `### Administrator users and directory browsing` under `## Related Documents and Supersession` and link `[2026-06-22-configurable-directory-sync-design.md](./2026-06-22-configurable-directory-sync-design.md)`. State this exact relationship:

```text
The 2026-06-22 Directory Sync design remains authoritative for current-snapshot resolution from the latest successful apply, current membership authority with legacy fallback only when no membership rows exist, multi-department/subtree member deduplication, unmatched-user behavior, display-path business semantics, the union of department representative_external_ids with member leader_department_ids, matched-representative semantics, and the requirement that visible/current-filter mutation scopes agree.

The 2026-07-14 performance design extends or supersedes only the administrator read transport/loading/materialization clauses: positive matched_user_id and normalized-email user mapping are both preserved and deduplicated; list/enrichment and current-filter targets become bounded SQL/page-local reads; active frontend selection/navigation uses the 20/100 option route and 25/100 immediate-child route; the complete /departments route remains response-compatible but has no active frontend caller; supplied parents are current-source validated; closed cycles use one deterministic read-side navigation anchor. Directory Sync apply, storage, offboarding, and source-authoring contracts do not change.
```

Also expand `### Repository, PR, member detail, and administrator reads` with the landed route names, page bounds, source isolation, current-membership authority, dual user mapping, dangling-candidate behavior, representative union, deterministic cycle navigation, and three-way list/job/batch predicate parity. Do not modify the 2026-06-22 Directory Sync design or other historical admin-user specs.

- [ ] **Step 2: Run generation drift and full repository verification**

Run exactly:

```bash
(cd backend && gofmt -w internal/adminusers/*.go internal/adminsubscription/job.go internal/adminsubscription/job_test.go internal/handler/admin_users.go internal/handler/admin_users_test.go internal/handler/admin_users_subscription_test.go internal/handler/router.go ent/schema/directory_member.go ent/schema/directory_member_department.go)
(cd backend && go generate ./ent)
git diff --exit-code -- backend/ent
(cd backend && go test ./...)
(cd ae-cli && go test ./...)
(cd frontend && npm test)
(cd frontend && npm run build)
bash deploy/test/release-frontend-embed-test.sh
git diff --check
```

Run the role E2E against this worktree's owned IPv6 Vite listener. `127.0.0.1:5173` may belong to another worktree, so bind only `::1`, verify `localhost` reaches that listener, and clean it through a shell trap:

```bash
pushd frontend
npm run dev -- --host ::1 --port 5173 --strictPort >/tmp/ae-admin-users-134-vite.log 2>&1 &
vite_pid=$!
trap 'kill "$vite_pid" 2>/dev/null || true; wait "$vite_pid" 2>/dev/null || true' EXIT
popd
for attempt in {1..40}; do
  curl -fsS http://localhost:5173 >/dev/null && break
  sleep 0.25
done
curl -fsS http://localhost:5173 >/dev/null
(cd frontend && npm run test:e2e:role)
kill "$vite_pid"
wait "$vite_pid" 2>/dev/null || true
trap - EXIT
```

Record PostgreSQL small/large `EXPLAIN`, role E2E, listener ownership, and embedded build separately from ordinary unit tests. Leave every unrun checkbox open.

- [ ] **Step 3: Complete final SPEC and standards gates**

Generate one complete base-to-working-tree review package and obtain independent final SPEC and standards reviews against issue #134, the active performance contract, `AGENTS.md`, and exact code. Both reviewers must answer:

```text
Can default /admin/users mount or hidden UI call the complete departments route?
Are option and child collections ordered and bounded at 20/100 and 25/100?
Can adminusers.List, DepartmentOptions, DepartmentChildren, persisted-job current_filter, or compatibility-batch current_filter load a full directory snapshot into Go?
Is the unchanged AdminUsersHandler.ListDepartments compatibility snapshot explicitly exempt while every active frontend view/component has no import or call to listAdminUserDepartments?
Does a supplied parent require an exact row in the resolved current source, leaving a current-source orphan at root and returning empty for a missing parent that collides only in another source?
Does each closed cycle expose the normalized-name/external-ID-first anchor as one root, use one bounded cycle walk, and expand every component row once without returning the anchor through its stored parent?
Does the representative relation source-scope and deduplicate department-only, leader-only, and duplicate-in-both IDs before matched/unmatched counts at service and HTTP seams?
Do count, page, Targets, persisted jobs, and compatibility batch share exact filter semantics?
Does each count/page/target recursive node have Actual Loops = 1 across fixture scales?
Do current memberships override legacy primary only when rows exist?
Are dangling current candidates skipped only after bounded existence loading?
Can positive matched ID and normalized email each map users without duplicates?
Are unmatched local users visible exactly when department scope permits?
Does every ancestor outer query constrain the resolved source and reject collisions?
Are user rows id ASC, pages capped at 100, and enrichment page-local?
Does the frontend mount one active row tree and preserve all workflows?
Does the active performance spec explicitly inherit Directory Sync business/count semantics and supersede only administrator read/loading clauses, and is it listed in architecture source-of-truth order?
```

Resolve all Critical and Important findings, regenerate the package, rerun Step 2, and obtain passing final verdicts before continuing.

- [ ] **Step 4: Commit documentation and the locally verified ledger head**

Record exact fixture totals, query roles, recursive-loop evidence, selected indexes, response bytes, department request counts, DOM counts, commands, environment notes, and final review verdicts. Set `Status` to state that implementation/reviews/local verification are complete while draft PR creation and all CI rounds remain pending.

```bash
git add docs/architecture.md docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "docs(architecture): document bounded admin user reads"
git status --short
```

Expected: clean tracked worktree.

- [ ] **Step 5: Push and open the correctly based draft PR**

Create ignored `.superpowers/sdd/pr-134.md` with `Closes #134`, dependency on draft PR #138, API/SQL/index/scale/response/DOM evidence, verification, review verdicts, rollout risk, and index rollback notes.

```bash
git push -u origin perf/admin-users-134
gh pr create --draft --base docs/performance-contracts-116 --head perf/admin-users-134 --title "perf(admin-users): push department filtering into SQL" --body-file .superpowers/sdd/pr-134.md
gh pr view --json number,state,isDraft,baseRefName,headRefName,headRefOid,mergeable,mergeStateStatus,url
```

Expected: open draft PR with exact base/head. Capture this OID as `round1_oid`; do not check round one yet.

- [ ] **Step 6: Verify round one, then create the round-two evidence head**

Wait for `backend`, `frontend`, `ae-cli`, and `deploy-static` on `round1_oid`:

```bash
gh pr checks --watch
gh pr view --json headRefOid,statusCheckRollup
```

Only after all four are green, record `round1_oid`, run IDs, conclusions, and remaining rounds in the plan. Keep `Status` explicitly incomplete with rounds two and three pending; check only the round-one evidence. Commit and push that evidence to create `round2_oid`:

```bash
git add docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "docs(plan): record admin user CI round one"
git push
gh pr view --json headRefOid,statusCheckRollup
```

- [ ] **Step 7: Verify round two, then create the final-ledger head for round three**

Wait for all four jobs on `round2_oid`, verify the live PR head equals `round2_oid`, and only then record round-two OID/run IDs/conclusions. Set the top status to this exact truthful boundary: implementation, reviews, repository verification, and CI rounds one/two complete; final-ledger round three pending. Do not claim complete and do not mark round three green.

Commit the final ledger and push it to create `round3_oid`:

```bash
git add docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "docs(plan): record admin user CI round two"
git push
gh pr view --json headRefOid,statusCheckRollup
```

The committed ledger must identify the new `round3_oid` as pending in the PR body/comment or external delivery record; it cannot pre-record a green result for its own new head.

**Final external gate (intentionally not a branch-ledger checkbox):** wait for `backend`, `frontend`, `ae-cli`, and `deploy-static` on `round3_oid`; verify all green and verify local HEAD equals the live PR head OID. Report the exact OID/run IDs/conclusions in a PR comment and the execution handoff without changing tracked files or creating a fourth CI head:

```bash
gh pr checks --watch
gh pr view --json number,state,isDraft,baseRefName,headRefName,headRefOid,mergeable,mergeStateStatus,statusCheckRollup,url
git status --short --branch
```

Expected: clean `perf/admin-users-134`; open draft PR against `docs/performance-contracts-116`; local HEAD, PR head, and `round3_oid` are equal; all three recorded heads had four green jobs. Keep the worktree and do not merge, tag, release, deploy, or run Helm.

## Self-Review Record

- Spec coverage: Tasks 1-2 implement recursive SQL filtering, exact count/page parity, stable pagination, multi-membership, dual mapping, legacy fallback, and bounded enrichment; Task 5 implements the single responsive row tree.
- Complete-page coverage: Tasks 3 and 5 remove the active page's unconditional complete department snapshot, add a 20/100 searchable selector, and add 25/100 child-at-a-time navigation while retaining the old route only for compatibility.
- Mutation coverage: Task 4 injects `adminusers.Targets` into persisted jobs and directly reuses it for the legacy batch route; the full matrix and the 501st target exercise both HTTP paths.
- Recursive-work coverage: Task 1 makes eligible local IDs an uncorrelated materialized relation; Tasks 1-2 require `Recursive Union Actual Loops == 1` for Targets, count, and page across small/large fixtures.
- Source-isolation coverage: Tasks 2-3 bind the same source in recursive and outer department queries, require supplied-parent existence inside that source, and include a current-source orphan whose missing parent external ID collides in a non-current source.
- Cycle-navigation coverage: Task 3 defines an exact read-side cycle walk bounded by `N` path entries and `N*N` emitted rows, chooses one deterministic anchor per component, applies one effective-parent relation to root/child/count/path/descendant work, and verifies root expansion without duplicates at service and HTTP seams.
- Representative coverage: Task 3 unions page-department `representative_external_ids` with source-scoped member `leader_department_ids`, deduplicates both declarations, and tests department-only, leader-only, duplicate, matched, unmatched, and non-current collision cases at service and HTTP seams.
- Candidate-fallback coverage: Task 2 loads bounded candidate departments before primary/ordered-existing selection, skips dangling memberships, and allows legacy fallback only when no current membership rows exist.
- Collection consistency: user 20/100, option 20/100, child 25/100, mutation 500/501-probe, and DOM one-per-user limits are declared in interfaces, HTTP payloads, tests, and frontend behavior.
- Compatibility-boundary consistency: no-full-snapshot gates name only `adminusers.List`, the two new bounded readers, and both `current_filter` mutation paths; `AdminUsersHandler.ListDepartments` remains intentionally exempt while the frontend import/call scan proves it is inactive.
- Documentation consistency: Task 6 adds the performance spec to architecture source-of-truth order and records exactly which Directory Sync business/count contracts remain authoritative versus which administrator read/loading clauses the newer spec extends or supersedes.
- Type consistency: `adminusers.Filters`, list/department request/page DTOs, `adminsubscription.CurrentFilter`, handler adapters, TypeScript DTOs, and API parameter names match exactly across tasks.
- Package consistency: `adminsubscription` owns its narrow resolver interface; only the handler composition root imports both modules, so no import cycle is introduced.
- Plan-detail consistency: every implementation step names exact paths, signatures, query shapes, commands, expected failures, and pass criteria; every helper and behavior is fully defined.
- Delivery consistency: the plan keeps all verification and CI items open until run, requires three explicit CI heads, and reports the final-head result externally without falsely completing it in an earlier ledger commit.
- Hygiene: every fixture and diagnostic value is synthetic; no live credential, user data, `sub2api` source, release, deploy, or Helm mutation is in scope.
