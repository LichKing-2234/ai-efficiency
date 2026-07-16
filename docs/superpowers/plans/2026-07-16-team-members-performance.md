# Team Member Ranking Performance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver issue #127 with a snapshot-bound, integrity-protected member-ranking endpoint and an independently paginated frontend section that never renders the full authorized roster at once.

**Architecture:** `teamusage.Service.Members` projects one bounded page from the same actor/provider/scope/range-isolated snapshot used by summary, trend, and compatibility overview. A domain-separated HMAC cursor binds actor, normalized range, scope version, deterministic member-snapshot identity, and offset; the frontend owns cursor history and restarts only the member section after `snapshot_expired`. Organization data remains on the compatibility overview until issue #128.

**Tech Stack:** Go, Gin, Ent, shared Redis snapshot read model, HMAC-SHA256, Vue 3, TypeScript, Vitest, Vite.

## Global Constraints

- Work only in `/Users/admin/ai-efficiency/.worktrees/perf-team-members-127` on `perf/team-members-127`.
- Base is issue #126 head `597c7f4`; target the Draft PR for #126 when publishing.
- PostgreSQL, current representative scope, current provider configuration, and token revocation remain authoritative on every request.
- Reuse the #125/#126 snapshot cache and origin generation; do not add a member cache, internal HTTP call, or second Relay calculation.
- The members response defaults to 50 rows and rejects limits above 100.
- Rank by complete selected-window token total descending, then stable subject identity. Within local-user identities numeric user id is ascending; directory-only identities use external id ascending.
- The cursor is opaque and HMAC protected, contains no email/display metadata, and binds actor, normalized range, scope version, member snapshot identity, and next offset.
- Invalid/tampered/cross-actor/cross-range cursors return 400 `invalid_cursor`; a valid cursor whose scope or member snapshot changed returns 409 `snapshot_expired`.
- The frontend restarts only the member section on `snapshot_expired` and does not refetch or hide summary, trend, or organization state.
- Keep organization rendering on the compatibility overview until issue #128; do not implement organization pagination here.
- Tests and examples use synthetic identities only.
- Do not merge, release, tag, deploy, run Helm, or modify `sub2api`.
- Update every checkbox immediately after the action is completed.

**Status:** In progress. Implementation, review remediation, and full local verification are complete; Draft PR publication and required CI remain.

---

### Task 1: Add Snapshot-Bound Member Paging

**Files:**
- Create: `backend/internal/teamusage/member_cursor.go`
- Create: `backend/internal/teamusage/member_cursor_test.go`
- Create: `backend/internal/teamusage/members.go`
- Create: `backend/internal/teamusage/members_test.go`
- Modify: `backend/internal/teamusage/types.go`
- Modify: `backend/internal/teamusage/service.go`
- Modify: `backend/internal/handler/team_usage.go` for production cursor-secret injection only

**Interfaces:**
- Produces `MembersParams`, `MembersResponse`, `Service.Members(context.Context, int, MembersParams)`, `ErrInvalidMemberCursor`, and `ErrMemberSnapshotExpired`.
- Extends `NewServiceWithSnapshotCache` with an optional member cursor secret while preserving existing call sites.

- [x] **Step 1: Add RED cursor and 500-member projection tests**

  Cover default 50, explicit 100, limit 101 rejection, global ranks, token ties, numeric local-user ties, directory-only ties, response JSON below 64 KiB for a 50-row page from 500 members, next cursor, tamper, cross-actor, cross-range, changed scope/snapshot 409, same-content authoritative reconstruction, and shared snapshot reuse.

- [x] **Step 2: Run focused tests and record RED**

  Run: `cd backend && go test ./internal/teamusage -run 'MemberCursor|Members' -count=1 -v`

  Expected: compile failures for the absent cursor codec, DTO, and service projection.

  RED evidence (2026-07-16): the focused command failed only because `Service.Members`, `MembersParams`, the cursor codec/payload, and cursor errors were absent.

- [x] **Step 3: Implement codec, stable ranking, snapshot identity, and bounded projection**

  Derive a domain-separated HMAC key from the configured encryption secret. Hash the complete newly ranked member rows for deterministic snapshot identity so Redis failure can rebuild unchanged authoritative content without making pagination impossible. Reject scope/content changes before slicing.

- [x] **Step 4: Verify Task 1 GREEN and checkpoint**

  Run:

  ```bash
  cd backend
  gofmt -w internal/teamusage/*.go internal/handler/team_usage.go
  go test ./internal/teamusage -count=2
  go test -race ./internal/readcache ./internal/teamusage -count=1
  git diff --check
  ```

  Commit: `perf(backend): page snapshot-bound team members`

  GREEN evidence (2026-07-16): focused cursor/member tests, double teamusage runs, race-enabled readcache/teamusage, and `git diff --check` passed. The 500-member default page serialized below 64 KiB and shared one Relay generation across cursor pages.

### Task 2: Expose The Authenticated Members Route

**Files:**
- Modify: `backend/internal/handler/team_usage.go`
- Modify: `backend/internal/handler/team_usage_test.go`
- Modify: `backend/internal/handler/router.go`

**Interfaces:**
- Produces authenticated `GET /api/v1/user/team-usage/members`.
- Returns request-local `X-Request-ID` plus matching response `request_id`.

- [x] **Step 1: Add RED HTTP tests**

  Cover normalized range forwarding, missing/default and explicit limit, cursor forwarding, response fields without `member_tree`/duplicate collections, unique request ids, auth, 403 no scope, 400 invalid limit/cursor, and 409 `snapshot_expired`.

- [x] **Step 2: Run focused handler tests and record RED**

  Run: `cd backend && go test ./internal/handler -run 'TeamUsageMembers' -count=1 -v`

  RED evidence (2026-07-16): the focused command failed only because `TeamUsageHandler.Members` was absent.

- [x] **Step 3: Implement handler, stable error mapping, and production route**

  Reuse the summary/trend range parser. Generate request IDs only after service projection and expose no legacy deprecation headers.

- [x] **Step 4: Verify Task 2 GREEN and checkpoint**

  Run: `cd backend && go test ./internal/teamusage ./internal/handler ./cmd/server -count=2 && git diff --check`

  Commit: `feat(backend): expose paged team members`

  GREEN evidence (2026-07-16): focused members handler tests and double runs of teamusage, handler, and cmd/server passed; `git diff --check` was clean.

### Task 3: Render One Independent Member Page

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/api/teamUsage.ts`
- Modify: `frontend/src/views/TeamOverviewView.vue`
- Modify: `frontend/src/components/team-usage/TeamOverviewMemberTable.vue`
- Modify: `frontend/src/i18n.ts`
- Modify: `frontend/src/__tests__/team-usage-api.test.ts`
- Modify: `frontend/src/__tests__/team-overview-view.test.ts`

**Interfaces:**
- Produces `TeamUsageMembersResponse` and `getTeamUsageMembers(params)` with the existing 45-second shared-origin budget.
- Adds independent member request sequence/loading/data/error/current cursor/history state and Next/Previous controls.

- [x] **Step 1: Add RED API/view/component tests**

  Cover a 500-member total with 50 rendered ranking rows, no compatibility `members` use, member delay/failure isolation, compatibility failure while rankings remain, Next/Previous cursor calls with unchanged range, pagination without sibling refetch, stale member marker, and 409 recovery that reloads only the first member page.

- [x] **Step 2: Run focused frontend tests and record RED**

  Run: `cd frontend && npm test -- src/__tests__/team-usage-api.test.ts src/__tests__/team-overview-view.test.ts`

  RED evidence (2026-07-16): 8 focused failures showed the missing members API, 500-row legacy DOM, absent independent member states, compatibility coupling, missing pagination, missing 409 recovery, and missing local stale marker.

- [x] **Step 3: Implement independent member lifecycle and bounded controls**

  Pass only split endpoint items to the ranking table and only compatibility `member_tree` to organization rendering. Keep current range selection while paging and clear cursor history only when range changes or the backend reports `snapshot_expired`.

- [x] **Step 4: Verify Task 3 GREEN and checkpoint**

  Run: `cd frontend && npm test && npm run build && git diff --check`

  Commit: `perf(frontend): paginate team member rankings`

  GREEN evidence (2026-07-16): focused API/view tests passed 50/50, the full frontend suite passed 39 files/475 tests, the production build passed, and `git diff --check` was clean.

### Task 4: Document, Verify, Review, And Publish

**Files:**
- Modify: `docs/architecture.md`
- Modify: this plan

- [x] **Step 1: Update current architecture**

  Record the split members route, HMAC cursor dimensions, deterministic snapshot identity and Redis fallback, bounds/order, independent frontend lifecycle, and compatibility organization-only role.

  Documentation evidence (2026-07-16): `docs/architecture.md` now records the fourth independent request, 50/100 limits, global ranking order, HMAC cursor dimensions and stable errors, deterministic Redis-outage identity, local 409 recovery, and compatibility overview's organization-only frontend role.

- [x] **Step 2: Run full verification**

  ```bash
  git diff --check
  cd backend && go vet ./internal/teamusage ./internal/handler ./cmd/server && go test ./...
  cd ../frontend && npm test && npm run build
  cd ../ae-cli && go test ./...
  ```

  Post-fix verification evidence (2026-07-16): diff checks and focused `go vet` including `internal/representativescope` passed; the complete backend suite and race-enabled `internal/readcache`/`internal/representativescope`/`internal/teamusage` suites passed; the frontend suite passed 39 files/476 tests and the production build completed; the complete `ae-cli` suite passed.

- [x] **Step 3: Review against issue #127 and the active performance spec**

  Audit limits, global rank/order, cursor integrity and dimensions, scope/snapshot expiry, Redis outage behavior, response bytes, DOM rows, sibling lifecycle isolation, no organization duplication, and synthetic test data. Fix every finding and rerun affected verification.

  Review evidence (2026-07-16): independent review found and the implementation fixed two Important edge cases: member pagination had rebuilt absolute dates after midnight, and member snapshot identity had retained nondeterministic department-membership ordering across authoritative rebuilds. Regression tests cover both findings, final focused re-review was clean, the cursor secret still fails closed when missing, compatibility overview keeps its historical payload, and organization pagination remains outside this ticket.

- [ ] **Step 4: Push and open a Draft PR**

  Target `perf/team-trend-126`, list Draft PR #154 as the direct dependency, preserve the worktree, and do not merge or release.

- [ ] **Step 5: Wait for required CI and record final state**

  Record the exact implementation-head run and backend/frontend/ae-cli/deploy-static conclusions.
