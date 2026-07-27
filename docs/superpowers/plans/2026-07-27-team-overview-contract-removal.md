# Team Overview Contract Removal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Issue:** [#137](https://github.com/LichKing-2234/ai-efficiency/issues/137)

**Status:** Implementation, verification, review, and ready PR [#198](https://github.com/LichKing-2234/ai-efficiency/pull/198) delivery are complete. The compatibility release and production zero-caller evidence are complete; implementation is isolated on `refactor/team-overview-contract-removal-137` from baseline `02bce300`. Backend, frontend, and ae-cli suites pass. After merge, #137 still requires a normal platform release and production smoke before closure. The rollback point is `v0.1.0-preview.74` / `d3292d249cf030b7454db67f46ff64ffb8a2d215`, Helm production revision 71.

**Goal:** Remove the deprecated monolithic Team Overview HTTP contract after its completed compatibility window, leaving Summary, Trend, Members, and Organization as the only current Team Usage read contracts.

**Architecture:** Delete the legacy route and adapter at the handler/service boundary, then delete only DTOs and tree projection code owned exclusively by that adapter. Preserve the split read lanes, Redis/prewarm path, authorization checks, and shared range/member types used by those lanes. Remove the unused frontend client and compatibility fixtures without renaming otherwise-current `Overview*` shared types.

**Tech Stack:** Go 1.24, Gin, Ent, Redis readcache, Vue 3, TypeScript, Vitest.

## Global Constraints

- Work only in `/Users/admin/ai-efficiency/.worktrees/team-overview-contract-removal-137` on `refactor/team-overview-contract-removal-137`.
- Do not modify Summary, Trend, Members, Organization, Redis generation/manifest, prewarmer, Relay, Sub2API, or Helm behavior.
- Do not retain a `410 Gone` tombstone route; an unmatched removed route must return the normal API 404 behavior.
- Keep shared request/member/window types when current split contracts still consume them; this ticket is not a naming migration.
- Use only synthetic identities and domains in tests.
- Update each checkbox immediately after the action completes.
- Do not release, tag, deploy, or sample production in this plan.

---

### Task 1: Remove The Backend Compatibility Contract

**Files:**
- Modify: `backend/internal/handler/team_usage_test.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/handler/team_usage.go`
- Modify: `backend/internal/teamusage/service.go`
- Modify: `backend/internal/teamusage/types.go`
- Modify: `backend/internal/teamusage/organization.go`
- Modify: `backend/internal/teamusage/scope_policy.go`
- Modify: compatibility-only tests under `backend/internal/handler` and `backend/internal/teamusage`

**Interfaces:**
- Removes: `GET /api/v1/user/team-usage/overview`, `TeamUsageHandler.Overview`, `teamUsageService.Overview`, `Service.Overview`, `OverviewResponse`, `OverviewSummary`, and `OverviewMemberNode`.
- Preserves: `Summary`, `Trend`, `Members`, and `Organization` method signatures and response contracts.

- [x] **Step 1: Add and run the failing route-removal regression**

  Add `TestDeprecatedTeamUsageOverviewRouteIsRemoved` using `setupTestEnvWithProvider` and `router.Routes()`. Assert that `GET /api/v1/user/team-usage/overview` is absent while the four split routes remain registered.

  Run:

  ```bash
  cd backend
  go test ./internal/handler -run TestDeprecatedTeamUsageOverviewRouteIsRemoved -count=1
  ```

  Expected: FAIL because the deprecated route is still registered.

- [x] **Step 2: Delete the route, handler/service adapter, compatibility DTO, tree projection, and obsolete tests**

  Remove the compatibility header helper together with the route. Delete only tree/summary helpers whose complete call graph belongs to `OverviewResponse`; retain current split-lane member ranking and organization branch helpers.

- [x] **Step 3: Run focused backend GREEN**

  ```bash
  cd backend
  gofmt -w internal/handler internal/teamusage
  go test ./internal/handler ./internal/teamusage -count=1
  ```

  Expected: PASS with no production symbol or route for the deprecated contract.

### Task 2: Remove The Frontend Compatibility Client And Fixtures

**Files:**
- Modify: `frontend/src/api/teamUsage.ts`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/__tests__/team-usage-api.test.ts`
- Modify: `frontend/src/__tests__/team-overview-view.test.ts`

**Interfaces:**
- Removes: `getTeamUsageOverview`, `TeamOverviewResponse`, and `TeamOverviewMemberNode`.
- Preserves: `getTeamUsageSummary`, `getTeamUsageTrend`, `getTeamUsageMembers`, `getTeamUsageOrganization`, and current split response/member/window types.

- [x] **Step 1: Delete the old client/type surface and compatibility-only assertions**

  Rebase the Team Overview view fixtures on the four split response types. Delete unused legacy mock setup and tests whose only signal was that split rendering ignored compatibility data.

- [x] **Step 2: Run focused frontend GREEN and compile the production bundle**

  ```bash
  cd frontend
  npm test -- src/__tests__/team-usage-api.test.ts src/__tests__/team-overview-view.test.ts
  npm run build
  ```

  Expected: PASS with no runtime or type import of the removed client/DTO.

### Task 3: Synchronize Current Documentation And Verify Delivery

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md`
- Modify: this plan

**Interfaces:**
- Current documentation records four split read contracts as the complete runtime surface and identifies the legacy adapter as removed after one compatibility release.
- Historical specs/plans remain historical records and are not rewritten.

- [x] **Step 1: Update current architecture/spec wording**

  Remove current-state descriptions of the compatibility adapter, monolithic timeout window, and recursive legacy tree. Record the completed sunset and unchanged split/prewarm boundaries.

- [x] **Step 2: Prove the legacy runtime surface is absent**

  ```bash
  rg -n 'team-usage/overview|getTeamUsageOverview|TeamOverviewResponse|OverviewResponse|func \(.*\) Overview\(' backend frontend \
    --glob '!**/node_modules/**' --glob '!**/*.map'
  ```

  Expected: no production hits; the only current-test hit is the negative route-registration regression.

- [x] **Step 3: Verify the clean baseline before implementation**

  Completed on `02bce300`:

  - `cd backend && GOPROXY=https://goproxy.cn,direct go test ./... -count=1`
  - `cd frontend && npm test` - 46 files, 683 tests passed
  - `cd ae-cli && go test ./... -count=1`

- [x] **Step 4: Run the full verification matrix**

  ```bash
  git diff --check
  cd backend && go test ./... -count=1 && go vet ./... && go build ./...
  cd ../frontend && npm test && npm run build && npm run test:e2e:role
  cd ../ae-cli && go test ./... -count=1
  cd .. && bash deploy/test/release-frontend-embed-test.sh
  ```

- [x] **Step 5: Review, commit, push, and open the PR**

  Require no Critical/Important review finding, then commit with:

  ```bash
  git commit -m "refactor(teamusage): remove deprecated overview contract"
  ```

  Push `refactor/team-overview-contract-removal-137` and open a ready PR to `main` referencing #137. Do not merge or deploy.

## Post-Merge Acceptance Remaining

- [ ] Publish the removal through a normal platform release and record its release/tag/commit.
- [ ] Smoke Summary, Trend, Members, Organization, and selected-member detail in production; confirm the retired Overview URL returns the standard 404.
- [ ] Record the production Helm revision and confirm rollback remains available to `v0.1.0-preview.74` / `d3292d249cf030b7454db67f46ff64ffb8a2d215`, Helm revision 71.
