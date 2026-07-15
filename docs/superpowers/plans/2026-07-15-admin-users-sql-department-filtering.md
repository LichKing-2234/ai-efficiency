# Administrator Users SQL Department Filtering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** Draft plan prepared from `docs/performance-contracts-116@5f6c58e`; independent plan review, implementation, task reviews, delivery, and CI remain pending.

**Goal:** Keep administrator user browsing bounded by evaluating directory subtree membership, user matching, counts, ordering, and pagination in PostgreSQL while mounting only one responsive user-row representation.

**Architecture:** Add one concrete, deep `backend/internal/adminusers` module whose small interface owns filter semantics, count/page reads, page-bounded directory enrichment, and current-filter target resolution. It hides recursive PostgreSQL predicates and bounded enrichment from the Gin handler, reuses the same filter for list and subscription-job scopes, and leaves the existing full department-summary route unchanged.

**Tech Stack:** Go 1.23/1.24, Gin, Ent 0.14, PostgreSQL 16, `lib/pq`, Vue 3 `<script setup lang="ts">`, TailwindCSS, Vitest, Vue Test Utils.

## Global Constraints

- Work from `docs/performance-contracts-116@5f6c58e6821dfcd95eefff14ea3426d454ae86cd`; do not stack on sibling performance branches.
- Preserve `GET /api/v1/admin/users`, `GET /api/v1/admin/users/departments`, subscription-job routes, query parameter names, response envelopes, one-based `page`, `page_size`, `total`, and stable user `id ASC` ordering.
- List page size defaults to 20 and never exceeds 100; page defaults to 1. Count and page use the same normalized search, access-status, and department predicate. After count, a requested page beyond the last result returns an empty page without calculating an overflowing offset; even a maximum-integer page value must not wrap `(page-1)*page_size`.
- With no department filter, unmatched local users remain visible. A department filter excludes unmatched local users because no current directory member supplies evidence for the selected subtree.
- Resolve the current directory source through `directorysync.CurrentSourceID`; do not use source edit time or a non-current apply run. One `List` call resolves it at most once and reuses that result for both department filtering and page enrichment.
- Department scope is the requested department plus all descendants by `parent_external_id`, computed by a recursive PostgreSQL expression. User-provided department values are always bound parameters, never interpolated SQL.
- Current `directory_member_departments` rows are authoritative when any exist for a member. The legacy `directory_members.department_external_id` is considered only when that member has no current membership rows.
- A directory member maps to local users by positive `matched_user_id` or normalized email `LOWER(BTRIM(users.email)) = directory_members.email_normalized`; both mappings remain eligible and results are deduplicated by local user ID.
- Search preserves case-insensitive username/email matching plus numeric local-user and Relay-user ID matching. Access-status predicates remain owned by `backend/internal/adminuseraccess`.
- The same filter implementation drives list reads and `current_filter` subscription-job target resolution so visible scope and mutation target scope cannot diverge.
- Page enrichment may load only current members matched to the at-most-100 page users, memberships for those members, and the ancestor closure needed for selected department display paths. It must not load the complete department/member/membership snapshot.
- Keep `GET /admin/users/departments` behavior and response unchanged in this ticket. It remains the department-tree/navigation contract; this ticket removes full-snapshot work from filtered user list and current-filter target paths.
- Add only indexes that match the implemented predicates: current source + matched user and current source + member + department. Ent schema changes require generation and a clean generated-drift check.
- The frontend mounts either mobile user cards or the desktop user table at the existing 768px breakpoint, never both. Selection, dialogs, filters, pagination, keyboard behavior, and department view remain unchanged.
- Keep handlers thin, API calls in `frontend/src/api`, and view state in `AdminUsersView.vue`. Do not introduce Redis, a cache/read model, a new service process, direct `sub2api` coupling, or changes to Relay/provider interfaces.
- PostgreSQL large-directory/query-plan tests and browser role E2E are environment-sensitive and must be reported separately from ordinary unit tests.
- Tests, fixtures, SQL diagnostics, docs, usernames, emails, directory IDs, paths, credentials, and URLs use only synthetic values such as `alice@example.com`, `bob@example.org`, `dept-alpha`, and `org/alpha`.
- Update `docs/architecture.md` and the active 2026-07-14 performance contract only after behavior lands. Do not rewrite historical Directory Sync or admin-user specs.
- The draft PR targets `docs/performance-contracts-116`, links #134 and draft PR #138, remains open for review, and is not merged, released, deployed, or used for Helm work in this plan.
- Maintain this plan as a live ledger: check a step only after it actually runs and keep the top `Status` consistent with remaining work.

## Deep Module Interface

The handler and subscription scope learn only these exported facts:

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

var ErrInvalidAccessStatus = errors.New("invalid admin user access status")

func NewService(client *ent.Client) *Service
func (s *Service) List(ctx context.Context, request ListRequest) (*Page, error)
func (s *Service) Targets(ctx context.Context, filters Filters, limit int) ([]*ent.User, error)
```

The concrete module, not a new Go interface/adapter hierarchy, hides SQL composition, current-source resolution, normalization, pagination, and enrichment.

## File Map

- `backend/internal/adminusers/service.go`: exported types, request normalization, shared filtered query, list page, and target resolution.
- `backend/internal/adminusers/department.go`: recursive department/member SQL predicates and page-bounded department enrichment.
- `backend/internal/adminusers/service_test.go`: semantic parity, normalization, list/target consistency, error, and cancellation tests.
- `backend/internal/adminusers/query_plan_test.go`: recording driver, 2,400-user fixture, SQL-role/plan/row-bound evidence.
- `backend/ent/schema/directory_member.go`: current-source/matched-user index.
- `backend/ent/schema/directory_member_department.go`: current-source/member/department index.
- `backend/internal/handler/admin_users.go`: thin HTTP/row mapping and shared service use for list plus current-filter subscription targets.
- `backend/internal/handler/admin_users_test.go`: HTTP compatibility, response bytes, and list/subscription scope parity.
- `frontend/src/views/admin/AdminUsersView.vue`: one active mobile/desktop user-row tree.
- `frontend/src/__tests__/admin-users-view.test.ts`: media lifecycle, DOM bounds, and unchanged interactions.
- `docs/architecture.md`: current bounded admin-user read relationship.
- `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md`: exact landed department matching/fallback semantics.

---

### Task 1: Lock Shared SQL Department-Filter Semantics

**Files:**
- Create: `backend/internal/adminusers/service.go`
- Create: `backend/internal/adminusers/department.go`
- Create: `backend/internal/adminusers/service_test.go`
- Maintain: `docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md`

**Interfaces:**
- Consumes: `directorysync.CurrentSourceID`, Ent `UserQuery`, `adminuseraccess.ApplyFilter`, current directory tables.
- Produces: `NewService`, `Filters`, a private shared `filteredUsersQuery`, and `Targets(ctx, filters, limit)` from the Deep Module Interface.

- [ ] **Step 1: Add semantic fixtures and failing target/filter tests**

Build one current successful full-company apply fixture with this exact shape:

```text
departments: dept-alpha -> dept-alpha-one; sibling dept-beta
alice: matched_user_id in dept-alpha-one
bob: nil matched_user_id, directory email bob@example.org, local email Bob@Example.org
carol: memberships in dept-alpha-one and dept-beta, legacy primary dept-alpha
dave: membership only in dept-beta, legacy primary dept-alpha-one (primary must not leak)
erin: no membership rows, legacy primary dept-alpha-one
frank: unmatched local user with no directory member
grace: directory member matched_user_id=grace plus normalized email that maps another synthetic local user; both mappings remain eligible and deduplicate by user ID
```

Add table-driven tests for direct department, parent subtree, sibling exclusion, multi-membership, membership-over-primary, legacy fallback, matched ID, normalized mixed-case email, unmatched no-filter visibility, unmatched filtered exclusion, search/access-status intersections, no-current-source, unknown department, and request cancellation.

For every filter case compare `Targets` IDs in `id ASC` order with golden expected IDs. Assert no query or error text includes the raw synthetic search/department value.

- [ ] **Step 2: Run Task 1 tests and record RED**

Run:

```bash
(cd backend && go test ./internal/adminusers -run 'TestTargets|TestDepartment' -count=1 -v)
```

Expected: FAIL because the `adminusers` package and shared SQL filter module do not exist.

- [ ] **Step 3: Implement the shared filtered user query**

Normalize filters once. Search uses the existing case-insensitive username/email and numeric ID semantics. Validate access status before query composition; wrap exported `ErrInvalidAccessStatus` while preserving the current user-facing message, then delegate the valid predicate to `adminuseraccess.ApplyFilter`.

Keep the source result in one private resolved-source value accepted by the shared query builder. `Targets` resolves it once when `DepartmentID` is non-empty; Task 2 `List` resolves it once for both this predicate and enrichment. When `DepartmentID` is non-empty, add one Ent predicate whose PostgreSQL shape is equivalent to:

```sql
EXISTS (
  WITH RECURSIVE subtree(external_id) AS (
    SELECT d.external_id
    FROM directory_departments AS d
    WHERE d.source_id = ? AND d.external_id = ?
    UNION
    SELECT child.external_id
    FROM directory_departments AS child
    JOIN subtree AS parent
      ON child.parent_external_id = parent.external_id
    WHERE child.source_id = ?
  )
  SELECT 1
  FROM directory_members AS member
  WHERE member.source_id = ?
    AND (
      member.matched_user_id = users.id
      OR member.email_normalized = LOWER(BTRIM(users.email))
    )
    AND (
      EXISTS (
        SELECT 1
        FROM directory_member_departments AS membership
        JOIN subtree ON subtree.external_id = membership.department_external_id
        WHERE membership.source_id = ?
          AND membership.directory_member_id = member.id
      )
      OR (
        NOT EXISTS (
          SELECT 1
          FROM directory_member_departments AS current_membership
          WHERE current_membership.source_id = ?
            AND current_membership.directory_member_id = member.id
        )
        AND EXISTS (
          SELECT 1 FROM subtree
          WHERE subtree.external_id = member.department_external_id
        )
      )
    )
)
```

Use Ent table/field constants and selector-qualified outer columns. Interpolate only trusted identifiers; bind `source_id` and `department_id` values as arguments. `UNION`, not `UNION ALL`, prevents a malformed cycle from recursing forever.

If no current source exists, use an always-false user predicate. `Targets` applies the exact shared query, orders by user ID, enforces a positive caller-supplied limit, and returns wrapped operation-specific errors.

- [ ] **Step 4: Verify Task 1 GREEN and current-filter consistency**

Run:

```bash
(cd backend && go test ./internal/adminusers -run 'TestTargets|TestDepartment' -count=2 -v)
(cd backend && go test ./internal/adminuseraccess ./internal/adminusers -count=1)
git diff --check
```

Expected: all semantic cases PASS twice, no full directory entity list is loaded into Go, cancellation propagates, and target IDs remain stable.

- [ ] **Step 5: Commit Task 1 and record the checkpoint**

```bash
git add backend/internal/adminusers/service.go backend/internal/adminusers/department.go backend/internal/adminusers/service_test.go docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "perf(admin-users): evaluate department filters in SQL"
```

After commit, check Step 5 and commit the ledger:

```bash
git add docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "docs(plan): record admin user filter task 1"
```

---

### Task 2: Return Bounded User Pages And Page-Local Departments

**Files:**
- Modify: `backend/internal/adminusers/service.go`
- Modify: `backend/internal/adminusers/department.go`
- Modify: `backend/internal/adminusers/service_test.go`
- Create: `backend/internal/adminusers/query_plan_test.go`
- Modify: `backend/ent/schema/directory_member.go`
- Modify: `backend/ent/schema/directory_member_department.go`
- Regenerate: `backend/ent/`
- Maintain: `docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md`

**Interfaces:**
- Consumes: Task 1 shared filtered query and Department predicate.
- Produces: `List(ctx, ListRequest) (*Page, error)` and the complete Deep Module Interface.

- [ ] **Step 1: Add page, enrichment, scale, and SQL recording tests**

Add ordinary page tests for default page 1/size 20, maximum 100, nonpositive values, out-of-range pages including a maximum-integer page that returns empty without offset overflow, stable `id ASC`, total/page predicate parity, empty results, and cancellation. Assert page enrichment returns the same visible department fields as the current handler for direct, multi-membership, and legacy members.

Add a mutex-protected recording PostgreSQL driver and one exact scale fixture:

```text
2,400 local users
2,200 current directory members
120 departments across 4 levels
3,600 current membership rows
200 unmatched local users
mixed matched_user_id and normalized-email mappings
600 multi-department members
300 legacy-primary-only members
repeated names and mixed-case local emails
```

Seed in bounded batches with fixed synthetic UTC timestamps. Compare small and large fixtures by SQL role, not call order. Current-source resolution keeps its existing two SQL roles (candidate sources and latest successful apply) but runs only once per `List`; then require constant roles for count, bounded page, page-member lookup, page memberships, ancestor closure, and page offboarding facts. A filtered query must not issue full-table `.All()` reads for departments, members, or memberships.

- [ ] **Step 2: Record RED against missing page module and indexes**

Run:

```bash
(cd backend && go test ./internal/adminusers -run 'TestList|TestLargeAdminUser' -count=1 -v)
```

Expected: FAIL because `List`/page enrichment and the matching composite indexes do not exist.

- [ ] **Step 3: Implement bounded page and ancestor enrichment**

`List` defensively normalizes page/size, resolves the current directory source once, and passes the same resolved value to the Task 1 query and page enrichment. It clones that query for total, returns an empty page immediately when the requested one-based page starts beyond `total`, and only then computes the offset and loads the ordered bounded page. This ordering prevents integer overflow for hostile maximum-page inputs. It calls `adminuseraccess.OffboardingFactsForUsers` only for page IDs.

For page departments:

1. Load current directory members whose positive `matched_user_id` is in page IDs or whose normalized email is in page normalized emails. Project only member ID, matched user ID, normalized email, and legacy primary department.
2. Load membership rows only for those member IDs, ordered by department external ID and row ID. Project only row ID, member ID, and department external ID.
3. Preserve current membership-over-primary selection and deterministic first visible department behavior: order members by ID, order memberships by department external ID then membership ID, prefer the primary department only when it is one of the current membership rows, and otherwise choose the first current membership.
4. Load only selected departments plus their ancestors with a bound-parameter recursive predicate equivalent to:

```sql
external_id IN (
  WITH RECURSIVE ancestors(external_id, parent_external_id) AS (
    SELECT external_id, parent_external_id
    FROM directory_departments
    WHERE source_id = ? AND external_id = ANY(?)
    UNION
    SELECT parent.external_id, parent.parent_external_id
    FROM directory_departments AS parent
    JOIN ancestors AS child
      ON child.parent_external_id = parent.external_id
    WHERE parent.source_id = ?
  )
  SELECT external_id FROM ancestors
)
```

The outer department query selects only external ID, parent external ID, name, and path; it does not load metadata or other result blobs. Build `display_path` from that page-local ancestor closure. Apply a member's chosen department to every matching page user: its positive `matched_user_id` and any page user whose normalized email equals the member email, with user-ID deduplication. Do not call the existing full-snapshot `departmentsForUsers` helper.

Add Ent indexes:

```go
index.Fields("source_id", "matched_user_id")
index.Fields("source_id", "directory_member_id", "department_external_id")
```

Run `go generate ./ent` and commit every generated change.

- [ ] **Step 4: Prove scale/query-plan bounds without timing gates**

Capture count, page, page-member, membership, and ancestor SQL separately and run `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` with recorded arguments. Assert:

```text
page has Limit with at most 100 returned/materialized user rows and exact id ASC order
count and page contain the same recursive department/member predicate
regular no-department page has no directory subtree predicate and retains unmatched users
membership lookup is bounded by page member IDs and uses a matching membership index
matched-user lookup has a matching current-source/matched-user index path
normalized-email lookup uses the existing current-source/email index path
ancestor query returns only selected departments and ancestors, never all 120 for a leaf page
member, membership, and ancestor projections omit metadata and unrelated columns
no assertion depends on elapsed time, planner cost, exact buffer count, or the entire plan tree
```

Run:

```bash
(cd backend && go generate ./ent)
git add backend/ent
(cd backend && go generate ./ent)
git diff --exit-code -- backend/ent
(cd backend && go test ./internal/adminusers -run 'TestList|TestLargeAdminUser' -count=2 -v)
(cd backend && go test ./internal/adminusers -count=1)
git diff --check
```

Expected: repeatable visible results, maximum 100 page users, constant SQL roles across scale, clean Ent generation drift.

- [ ] **Step 5: Commit Task 2 and record the checkpoint**

```bash
git add backend/internal/adminusers backend/ent/schema/directory_member.go backend/ent/schema/directory_member_department.go backend/ent docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "perf(admin-users): page directory-enriched users"
```

After commit, check Step 5 and commit the ledger:

```bash
git add docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "docs(plan): record admin user page task 2"
```

---

### Task 3: Adopt The Deep Module In HTTP And Subscription Scopes

**Files:**
- Modify: `backend/internal/handler/admin_users.go`
- Modify: `backend/internal/handler/admin_users_test.go`
- Maintain: `docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md`

**Interfaces:**
- Consumes: Task 2 `adminusers.Service`, `List`, `Targets`, `Page`, and `Department`.
- Produces: unchanged admin-user HTTP envelopes and current-filter subscription target behavior through one module.

- [ ] **Step 1: Add failing HTTP parity and bounded-response tests**

Update the existing handler fixture to cover the Task 1 mapping matrix through real HTTP. For each search/department/access/page request, assert exact IDs, total, page, page size, access status, offboarding status, encrypted relay password field, and department display path.

Add one `page_size=100` response assertion over the real handler body and require a wire body below 256 KiB for synthetic bounded rows. Assert unknown department yields an empty page; no department filter includes unmatched users.

For `current_filter` subscription jobs, assert the target IDs exactly match the corresponding list filter across all pages, including normalized email, multi-membership, and legacy fallback. No Relay call is needed; inspect the persisted target snapshot/fake executor input.

- [ ] **Step 2: Run handler tests and record RED**

Run:

```bash
(cd backend && go test ./internal/handler -run 'TestAdminUsers(List|Subscription).*' -count=1 -v)
```

Expected: FAIL because the handler still executes its in-memory filter/enrichment helpers and the current-filter target path does not use the new module.

- [ ] **Step 3: Make handlers thin and remove obsolete full-snapshot helpers**

Add one concrete `*adminusers.Service` field initialized by `NewAdminUsersHandler`. `List` parses HTTP values, calls `service.List`, maps the existing JSON row/envelope, and owns no department/member query composition.

Replace `current_filter` target resolution with `service.Targets(ctx, filters, adminSubscriptionBatchMaxUsers+1)`, retaining the existing 500-target rejection and selected/all-mapped behavior. Remove obsolete handler-only search/department/member-set/full-department enrichment helpers only after both call sites migrate; keep helpers still used by `ListDepartments`.

Wrap errors with stable operation context and preserve current HTTP status behavior: `errors.Is(err, adminusers.ErrInvalidAccessStatus)` maps to 400 with the current message; query failures are 500; authorization remains middleware-owned.

- [ ] **Step 4: Verify handler/module compatibility GREEN**

Run:

```bash
(cd backend && go test ./internal/adminusers ./internal/handler -run 'TestAdminUsers|TestTargets|TestList' -count=1)
(cd backend && go test ./internal/adminusers ./internal/handler -count=1)
git diff --check
```

Expected: exact existing envelopes/fields remain, list/target filters match, 100-row wire response is bounded, and handler code contains no full-snapshot filtered-list path.

- [ ] **Step 5: Commit Task 3 and record the checkpoint**

```bash
git add backend/internal/handler/admin_users.go backend/internal/handler/admin_users_test.go docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "refactor(admin-users): use bounded user reader"
```

After commit, check Step 5 and commit the ledger:

```bash
git add docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "docs(plan): record admin user handler task 3"
```

---

### Task 4: Mount One Responsive User Row Tree

**Files:**
- Modify: `frontend/src/views/admin/AdminUsersView.vue`
- Modify: `frontend/src/__tests__/admin-users-view.test.ts`
- Maintain: `docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md`

**Interfaces:**
- Consumes: unchanged `AdminUsersListResponse` and existing 768px responsive design.
- Produces: exactly one active user-row representation with unchanged interactions.

- [ ] **Step 1: Add failing active-row-tree and lifecycle tests**

Provide a controllable `matchMedia('(min-width: 768px)')` fake. Mount 100 synthetic users and assert:

```text
mobile: one data-admin-user-list=mobile and exactly 100 data-admin-user-row nodes
desktop: one data-admin-user-list=desktop and exactly 100 data-admin-user-row nodes
never 200 user row nodes
media change swaps trees without duplicating selection IDs or requests
unmount removes the exact media change listener
department view contains no hidden user row tree
```

Retain tests for select-all, cross-page selection, search debounce, department/access filters, page-size changes, dialogs, disable access, and subscription jobs.

- [ ] **Step 2: Run the frontend regression and record RED**

Run:

```bash
(cd frontend && npm test -- src/__tests__/admin-users-view.test.ts)
```

Expected: FAIL because CSS currently mounts both the mobile cards and desktop table for every user.

- [ ] **Step 3: Gate the existing row trees by one media query**

Create one module-lifetime-per-component `MediaQueryList` for `(min-width: 768px)`, mirror `.matches` into a ref, and add/remove the same `change` listener in component lifecycle.

Add stable computed values:

```ts
const showMobileUserRows = computed(() => rows.value.length > 0 && !desktopUserRows.value)
const showDesktopUserRows = computed(() => rows.value.length > 0 && desktopUserRows.value)
```

Replace only the two current CSS-only `v-if="rows.length > 0"` branches with those computed guards and stable `data-admin-user-list` / `data-admin-user-row` selectors. Keep child markup, 768px Tailwind visibility classes, selection bindings, dialogs, keyboard behavior, and department view unchanged.

- [ ] **Step 4: Verify focused/full frontend and build GREEN**

Run:

```bash
(cd frontend && npm test -- src/__tests__/admin-users-view.test.ts)
(cd frontend && npm test)
(cd frontend && npm run build)
git diff --check
```

Expected: focused/full Vitest and build PASS, one active row tree, listener cleanup, no request or visible interaction regression.

- [ ] **Step 5: Commit Task 4 and record the checkpoint**

```bash
git add frontend/src/views/admin/AdminUsersView.vue frontend/src/__tests__/admin-users-view.test.ts docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "perf(frontend): mount one admin user row tree"
```

After commit, check Step 5 and commit the ledger:

```bash
git add docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "docs(plan): record admin user rendering task 4"
```

---

### Task 5: Architecture, Verification, Reviews, And Draft PR Delivery

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md`
- Maintain: `docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md`
- Review only: every file changed since `5f6c58e`

**Interfaces:**
- Consumes: Tasks 1-4 and their task review evidence.
- Produces: current architecture/contract truth, full verification, final review gates, and a draft PR with two green CI rounds.

- [ ] **Step 1: Update current architecture and active contract**

Update the `/admin/users` architecture paragraph and module table to record the concrete `backend/internal/adminusers` reader, recursive SQL subtree/member matching, SQL count/page, page-local enrichment, shared current-filter target semantics, and one active responsive user-row tree.

Clarify the active performance contract with the landed current-membership-over-primary, matched-user-or-normalized-email, unmatched-user, `id ASC`, default 20/maximum 100 semantics. Do not rewrite historical Directory Sync/admin-user specs.

- [ ] **Step 2: Run generation drift and full repository verification**

Run exactly:

```bash
(cd backend && gofmt -w internal/adminusers/*.go internal/handler/admin_users.go internal/handler/admin_users_test.go ent/schema/directory_member.go ent/schema/directory_member_department.go)
(cd backend && go generate ./ent)
git diff --exit-code -- backend/ent
(cd backend && go test ./...)
(cd ae-cli && go test ./...)
(cd frontend && npm test)
(cd frontend && npm run build)
bash deploy/test/release-frontend-embed-test.sh
(cd frontend && npm run test:e2e:role)
git diff --check
```

Expected: all commands PASS. Report the 2,400-user PostgreSQL/query-plan run, role E2E, and embedded build separately as environment-sensitive evidence. Do not check an unrun command.

- [ ] **Step 3: Complete task and final SPEC/standards review gates**

Obtain independent spec/quality review for Tasks 1-4 against their exact base/head ranges and resolve every Critical/Important finding. Then review one complete base-to-working-tree package against issue #134, the active performance contract, and AGENTS.md.

Final reviewers must answer:

```text
Can a department-filtered list load the full directory snapshot into Go?
Do count, page, and current-filter targets share exact predicates?
Do current memberships override legacy primary department only when present?
Can matched_user_id and normalized email each map a user without duplicates?
Are unmatched users visible only when department scope allows them?
Is ordering exactly user id ASC and page size capped at 100?
Is page enrichment bounded to page users/memberships/ancestors?
Do indexes and EXPLAIN evidence prove structural bounds without timing gates?
Does the frontend mount one user row tree and preserve all interactions?
```

- [ ] **Step 4: Commit documentation and verified evidence**

Set top status to implementation/reviews/verification complete with draft PR CI pending. Record exact fixture totals, SQL roles, selected indexes, response bytes, DOM counts, commands, review verdicts, and environment notes.

```bash
git add docs/architecture.md docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "docs(architecture): document bounded admin user reads"
git status --short
```

Expected: clean worktree.

- [ ] **Step 5: Push and open the correctly based draft PR**

Create ignored `.superpowers/sdd/pr-134.md` with `Closes #134`, dependency on draft PR #138, SQL/index/scale evidence, response/DOM bounds, verification, review results, and rollback/index-migration notes.

```bash
git push -u origin perf/admin-users-134
gh pr create --draft --base docs/performance-contracts-116 --head perf/admin-users-134 --title "perf(admin-users): push department filtering into SQL" --body-file .superpowers/sdd/pr-134.md
gh pr view --json number,state,isDraft,baseRefName,headRefName,mergeable,mergeStateStatus,url
```

Expected: open draft PR, exact base/head, mergeable or clean state.

- [ ] **Step 6: Require first-round CI, finalize ledger, and require replacement CI**

Wait for `backend`, `frontend`, `ae-cli`, and `deploy-static` to succeed. Only then check delivery steps, set `Status: Complete`, commit, and push the final ledger:

```bash
git add docs/superpowers/plans/2026-07-15-admin-users-sql-department-filtering.md
git commit -m "docs(plan): record admin user SQL delivery"
git push
gh pr checks --watch
```

Expected: replacement CI green for all four jobs.

- [ ] **Step 7: Verify final branch and PR state**

```bash
git status --short --branch
gh pr view --json number,state,isDraft,baseRefName,headRefName,headRefOid,mergeable,mergeStateStatus,statusCheckRollup,url
```

Expected: clean `perf/admin-users-134`, local HEAD equals PR head OID, draft PR remains open against `docs/performance-contracts-116`, and both CI rounds are green. Keep worktree; do not merge, tag, release, deploy, or run Helm.

## Self-Review Record

- Spec coverage: Task 1 locks exact department/user mapping and shared target filters; Task 2 supplies bounded count/page/enrichment plus indexes and scale evidence; Task 3 migrates HTTP and subscription scopes; Task 4 bounds DOM; Task 5 supplies current docs, reviews, verification, and delivery.
- Placeholder scan: no TBD/TODO/unspecified code or test step remains.
- Interface consistency: the concrete `adminusers.Service` exposes only `List` and `Targets`; handlers/tests consume the exact `Filters`, `ListRequest`, `Page`, and `Department` types defined once.
- SQL consistency: every runtime value is bound; recursive CTEs use Ent table/field constants; count/page/targets share one predicate; current membership overrides legacy fallback.
- Bound consistency: page default 20/max 100; page enrichment starts from at most 100 users; 2,400-user scale and 100-row response/DOM gates are exact.
- Compatibility consistency: route/query/envelope/fields/id ordering/list-department route/subscription scope behavior remain; unmatched users remain visible with no department filter.
- Hygiene: every fixture and diagnostic value is synthetic; no live service or `sub2api` source is touched.
