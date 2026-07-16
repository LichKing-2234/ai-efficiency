# Team Organization Performance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver issue #128 with a snapshot-bound organization endpoint and a Vue organization tree that loads only authorized immediate children and direct members for expanded branches.

**Architecture:** `teamusage.Service.Organization` projects two independently bounded collections from the same actor/provider/scope/range-isolated snapshot used by summary, trend, members, and the compatibility overview. Domain-separated HMAC cursors bind collection kind, actor, normalized range, parent branch, scope version, deterministic organization snapshot identity, and offset; the frontend keeps one request state per parent branch, appends only the requested collection, and restarts only that branch after `snapshot_expired`.

**Tech Stack:** Go, Gin, Ent, shared Redis snapshot read model, HMAC-SHA256, Vue 3, TypeScript, Vitest, Vite.

## Global Constraints

- Work only in `/Users/admin/ai-efficiency/.worktrees/perf-team-organization-128` on `perf/team-organization-128`.
- Base is issue #127 final head `d568905`; target Draft PR #155 when publishing.
- PostgreSQL, current representative scope, current provider configuration, and token revocation remain authoritative on every request.
- Reuse the existing team snapshot, cursor secret, origin generation, stable subject ordering, and request-local metadata; do not add an organization cache, internal HTTP call, or second Relay calculation.
- Omitted `parent_department_external_id` represents authorized roots and returns no virtual-root members; a supplied parent must exist inside the current authorized tree.
- Immediate departments default to 25 and never exceed 100; direct members default to 50 and never exceed 100.
- Department order is normalized display name ascending and external ID ascending; direct-member order is selected-window tokens descending and the same stable subject identity used by issue #127.
- Each cursor is opaque, integrity protected, collection-specific, and bound to actor, normalized range, parent branch, scope version, deterministic organization snapshot identity, and offset.
- Invalid/tampered/cross-actor/cross-range/cross-parent/cross-collection cursors return 400 `invalid_cursor`; changed scope or organization snapshot returns 409 `snapshot_expired`.
- Keep `GET /api/v1/user/team-usage/overview` and its complete historical response shape for compatibility, but remove it from the current Team Overview frontend request path.
- Tests and examples use synthetic identities only.
- Do not merge, release, tag, deploy, run Helm, or modify `sub2api`.
- Update every checkbox immediately after the action is completed.

**Status:** In progress. The backend, frontend, frontend production build, and `ae-cli` baselines passed at `d568905` before implementation.

---

### Task 1: Add Snapshot-Bound Organization Projection

**Files:**
- Create: `backend/internal/teamusage/organization_cursor.go`
- Create: `backend/internal/teamusage/organization_cursor_test.go`
- Create: `backend/internal/teamusage/organization.go`
- Create: `backend/internal/teamusage/organization_test.go`
- Modify: `backend/internal/teamusage/service.go`
- Modify: `backend/internal/teamusage/types.go`

**Interfaces:**
- Produces `OrganizationParams`, `OrganizationResponse`, `OrganizationDepartment`, `Service.Organization(context.Context, int, OrganizationParams)`, `ErrInvalidOrganizationCursor`, and `ErrOrganizationSnapshotExpired`.
- Extends the existing optional pagination-secret initialization without changing production secret ownership or existing constructor call sites.
- Uses collection kinds `departments` and `members`; one request may independently page either collection.

- [x] **Step 1: Add RED cursor and projection tests**

  Cover root projection, scoped parent projection, generic out-of-scope parent failure, default/max/invalid limits, normalized department ordering, stable direct-member ordering and global ranks, independent cursors, exact row metadata/counts, no recursive children, response bounds for a wide organization, tamper/cross-actor/cross-range/cross-parent/cross-collection rejection, scope/content expiry, same-content authoritative reconstruction during Redis failure, and one shared snapshot generation across pages.

  Test evidence (2026-07-16): cursor tests define collection-separated signed payloads; service tests define virtual-root and scoped-parent semantics, 30-child/120-member dual pagination, exact metadata and shallow JSON, invalid limits and parents, cursor isolation/expiry, deterministic Redis fallback, and shared origin reuse.

- [x] **Step 2: Run focused tests and record RED**

  Run: `cd backend && go test ./internal/teamusage -run 'OrganizationCursor|Organization' -count=1 -v`

  Expected: compile failures for the absent cursor codec, DTOs, and service projection.

  RED evidence (2026-07-16): the focused command failed only because the organization cursor codec/payload/errors, DTOs, and `Service.Organization` did not exist.

- [x] **Step 3: Implement cursor, canonical identity, and shallow dual pagination**

  Flatten the existing compatibility tree only into an internal lookup/canonical identity. Return root nodes or one parent's immediate children as department rows, filter globally ranked snapshot members to exact direct membership for a supplied parent, and return empty direct members for the virtual root. Clone all response slices, strip recursive fields, and reject unauthorized parents before projection.

  Implementation evidence (2026-07-16): `Service.Organization` now reuses the shared snapshot, indexes the compatibility tree without returning it, projects sorted shallow departments and globally ranked direct members, and signs collection/actor/range/parent/scope/content/offset-bound cursors with a separate domain key.

- [x] **Step 4: Verify Task 1 GREEN and checkpoint**

  Run:

  ```bash
  cd backend
  gofmt -w internal/teamusage/*.go
  go test ./internal/teamusage -count=2
  go test -race ./internal/readcache ./internal/representativescope ./internal/teamusage -count=1
  git diff --check
  ```

  Commit: `perf(backend): page team organization branches`

  GREEN evidence (2026-07-16): focused organization tests, double `internal/teamusage` runs, race-enabled `internal/readcache`/`internal/representativescope`/`internal/teamusage`, and `git diff --check` passed.

### Task 2: Expose The Authenticated Organization Route

**Files:**
- Modify: `backend/internal/handler/team_usage.go`
- Modify: `backend/internal/handler/team_usage_test.go`
- Modify: `backend/internal/handler/router.go`

**Interfaces:**
- Produces authenticated `GET /api/v1/user/team-usage/organization`.
- Accepts `parent_department_external_id`, `department_cursor`, `department_limit`, `member_cursor`, and `member_limit` alongside the normalized range.
- Returns request-local `X-Request-ID` plus matching `request_id`, with stable 400/409 mappings and no compatibility deprecation headers.

- [ ] **Step 1: Add RED HTTP tests**

  Cover normalized range and parent forwarding, both default/explicit limits and cursors, response fields without recursive `children`/`member_tree`, unique request IDs, auth, no scope, out-of-scope parent, non-integer/oversized limits, invalid cursors, and `snapshot_expired`.

- [ ] **Step 2: Run focused handler tests and record RED**

  Run: `cd backend && go test ./internal/handler -run 'TeamUsageOrganization' -count=1 -v`

  Expected: compile failure because `TeamUsageHandler.Organization` is absent.

- [ ] **Step 3: Implement handler, error mapping, and production route**

  Reuse the summary/trend/members range parser and optional integer parser. Generate request IDs only after successful service projection and keep the legacy overview route unchanged.

- [ ] **Step 4: Verify Task 2 GREEN and checkpoint**

  Run: `cd backend && go test ./internal/teamusage ./internal/handler ./cmd/server -count=2 && git diff --check`

  Commit: `feat(backend): expose paged team organization`

### Task 3: Lazy-Load One Organization Branch At A Time

**Files:**
- Create: `frontend/src/composables/useTeamUsageOrganization.ts`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/api/teamUsage.ts`
- Modify: `frontend/src/views/TeamOverviewView.vue`
- Modify: `frontend/src/components/team-usage/TeamOverviewMemberTable.vue`
- Modify: `frontend/src/components/team-usage/TeamOverviewDepartmentNode.vue`
- Modify: `frontend/src/i18n.ts`
- Modify: `frontend/src/__tests__/team-usage-api.test.ts`
- Modify: `frontend/src/__tests__/team-overview-view.test.ts`

**Interfaces:**
- Produces `TeamUsageOrganizationParams`, `TeamUsageOrganizationResponse`, `TeamUsageOrganizationDepartment`, and `getTeamUsageOrganization(params)` with the existing 45-second shared-origin budget.
- `useTeamUsageOrganization` owns exact range parameters plus a generation-safe branch map keyed by nullable parent ID. It supports replace, append-departments, and append-members loads, deduplicates appended rows by stable identity, and locally replaces only the affected branch after 409.
- The organization UI renders root rows, fetches one branch on first expansion, reuses loaded state after collapse/re-expand, and exposes independent localized load-more/error/loading controls for child departments and direct members.

- [ ] **Step 1: Add RED API/view/component tests**

  Cover API parameter/timeout behavior, a wide root with only 25 department rows initially, expansion fetching only the selected parent, collapse/re-expand without refetch, independent department/member pagination, exact range retention across midnight, branch-local 409 recovery, one-branch failure isolation, range reset, direct-member access actions, bounded DOM, and zero current frontend calls to the compatibility overview.

- [ ] **Step 2: Run focused frontend tests and record RED**

  Run: `cd frontend && npm test -- src/__tests__/team-usage-api.test.ts src/__tests__/team-overview-view.test.ts`

  Expected: failures for the absent organization API/state, eager recursive compatibility tree, and missing local branch controls/recovery.

- [ ] **Step 3: Implement independent branch state and shallow recursive rendering**

  Start root organization loading independently with the same fixed range as sibling sections. Remove compatibility overview from the current view lifecycle, keep ranking independent, render only returned shallow nodes, fetch children/direct members on first expansion, and append only the requested collection. A branch error or recovery must not refetch/hide summary, trend, ranking, roots, or sibling branches.

- [ ] **Step 4: Verify Task 3 GREEN and checkpoint**

  Run: `cd frontend && npm test && npm run build && git diff --check`

  Commit: `perf(frontend): lazy-load team organization branches`

### Task 4: Document, Verify, Review, And Publish

**Files:**
- Modify: `docs/architecture.md`
- Modify: this plan

- [ ] **Step 1: Update current architecture**

  Record the fifth independent Team Overview request, shallow department/direct-member collections, bounds/order, cursor dimensions and Redis fallback, node-local frontend lifecycle, removal of the current compatibility frontend caller, and unchanged compatibility API response.

- [ ] **Step 2: Run full verification**

  ```bash
  git diff --check
  cd backend && go vet ./internal/representativescope ./internal/teamusage ./internal/handler ./cmd/server && go test ./...
  cd ../frontend && npm test && npm run build
  cd ../ae-cli && go test ./...
  ```

- [ ] **Step 3: Review against issue #128 and the active performance spec**

  Audit shallow response shape, direct versus aggregate counts, root semantics, stable ordering, cursor collection/parent integrity, expiry and Redis-outage behavior, authorization, response/DOM bounds, exact range retention, sibling/branch lifecycle isolation, compatibility preservation, and synthetic data. Fix every finding and rerun affected verification.

- [ ] **Step 4: Push and open a Draft PR**

  Target `perf/team-members-127`, list Draft PR #155 as the direct dependency, preserve the worktree, and do not merge or release.

- [ ] **Step 5: Wait for required CI and record final state**

  Record the exact implementation-head run and backend/frontend/ae-cli/deploy-static conclusions, then push one ledger commit and wait for final ledger-head CI.
