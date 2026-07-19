# Team Usage Organization Bounded Origin Implementation Plan

**Issue:** [#170](https://github.com/LichKing-2234/ai-efficiency/issues/170)

**Status:** Implementation and verification complete. Only final self-review and the requested local commit remain.

**Goal:** Replace the Team Usage Organization compatibility-Overview dependency with an independently cached, branch-bounded origin while preserving the existing HTTP and frontend contracts.

## Execution Ledger

- [x] **Step 1: Verify source-of-truth context and current behavior**

  Read `docs/architecture.md`, the active `2026-07-14` performance spec, issue #170, the integrated #168 Members lane, current Organization backend/frontend code, and relevant tests. Confirmed that Organization currently calls `readOverviewSnapshot`, invokes Trend, and constructs the recursive compatibility tree.

- [x] **Step 2: Add focused RED backend tests**

  Cover an Organization-only Redis lane and payload, requested-branch-only projection, no Trend/Overview cache use, wide/deep/multi-membership behavior, independent cursor isolation/expiration, Redis fallback, authorization, response size, stale/failure isolation, and low-cardinality metrics.

  RED evidence (2026-07-19): `go test ./internal/teamusage ./cmd/server -run 'Organization|CacheMetrics' -count=1` failed on the missing Organization typed cache API/options and the absent production `team_usage_organization` recorder, before production code was changed.

- [x] **Step 3: Implement the typed Organization cache lane**

  Add the narrow branch snapshot/value contract, parent-bound cache dimensions, validation, stable `team_usage_organization` metrics wiring, and the same readcache lease/stale/authoritative-fallback policy used by Summary, Trend, and Members.

  GREEN evidence (2026-07-19): the Organization lane now has a parent-bound key, schema-validated branch-only payload, readcache flight/lease/stale/fallback behavior, mismatch-aware parent validation/rebuild, and production metric wiring. Focused Organization/cache/server tests pass.

- [x] **Step 4: Implement the bounded Organization origin**

  Resolve current authorization and provider facts, select only the requested parent, its immediate departments, and direct members, batch complete-range usage reads at 100 IDs, compute department aggregate facts without constructing or serializing `member_tree`, and page the two collections with opaque independent cursors.

  GREEN evidence (2026-07-19): the origin now filters the authorized flat scope to the requested branch, calls only the Summary capability in batches of at most 100, calculates deduplicated direct/subtree aggregates, normalizes depth, and never calls Trend or constructs an `OverviewMemberNode` tree. Focused wide/deep/multi-membership/cursor/Redis/failure tests pass.

- [x] **Step 5: Verify frontend branch lifecycle**

  Keep current response/API contracts. Add or adjust tests so snapshot expiration restarts only the affected branch, invalidates only its loaded descendants, ignores late invalidated requests, and leaves summary, trend, members, root, and sibling branches usable during branch failure.

  Verification evidence (2026-07-19): existing first-party branch lifecycle already matched the #170 response contract; focused Team Usage API/View verification passed 2 files / 62 tests, including lazy expansion, independent collection pagination, single-branch expiry restart, descendant-only invalidation, late-response suppression, and sibling/section failure isolation.

- [x] **Step 6: Update current documentation**

  Update `docs/architecture.md` and `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md` with the implemented Organization lane and compatibility boundary. Do not rewrite historical specs.

  Documentation evidence (2026-07-19): the current architecture, active performance spec, and observability operator README now record the parent-bound Organization origin/cache/value/metric, branch-local direct-member ranking, exact mismatch recovery, branch-local stale/failure semantics, and legacy-only Overview boundary. Historical specs remain unchanged.

- [x] **Step 7: Run focused and full verification**

  Run focused RED/GREEN tests, Team Usage handler/cache/race tests, frontend Team Usage API/view tests, full relevant backend/frontend suites, `go vet`, backend/frontend builds, `git diff --check`, safety scans, and worktree hygiene. Record any environment-only gaps without marking them complete.

  Exact-head evidence (2026-07-19): focused Organization/cache/handler/server tests passed twice; the mismatch/concurrent-rebuild regressions passed; `go test ./... -count=1`, `go vet ./...`, and `go build ./...` passed under `backend`; affected `readcache`/`teamusage`/`handler` race tests passed with only the known macOS linker `LC_DYSYMTAB` warning. Frontend focused Team Usage API/View tests passed 62/62, the full suite passed 46 files / 680 tests with only existing jsdom canvas warnings, and the production build passed. `git diff --check`, generated-artifact hygiene, and the synthetic-data/secret scan were clean. Initial frontend commands encountered a missing worktree `node_modules` and one duplicate `--run` invocation; `npm ci` restored the lockfile-defined environment and the corrected commands passed, so no environment gap remains.

- [ ] **Step 8: Self-review and commit**

  Review standards and issue acceptance criteria, fix all Critical/Important findings, rerun affected verification, then commit all completed changes using Conventional Commits without pushing or opening a PR.
