# PR List Bulk Freshness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** Plan review is complete with no findings. Tasks 1-2 are complete at implementation commits `7659d82` and `34fb70c`; Task 3 is next, while later task review, delivery, and CI remain pending. The branch is based on `docs/performance-contracts-116@5f6c58e`.

**Goal:** Keep repository PR pages bounded by evaluating one page's usage freshness with a constant set of bulk SQL queries while preserving the current response fields, list ordering, detail diagnostics, and visible status/reason precedence.

**Architecture:** Preserve `prusage.Service` as the owner of freshness semantics and `PRHandler` as a thin HTTP adapter. First make the existing first-anomalous-commit rule deterministic using snapshot `sort_order`, then add one page-shaped evaluator that loads snapshots, repo-level pending evidence, and checkpoint usage facts in bulk before applying the same pure classifier. The list handler consumes that page result once; single-PR detail delegates through the same classifier and still returns commit diagnostics.

**Tech Stack:** Go 1.23/1.24 toolchain, Gin, Ent 0.14, PostgreSQL, `lib/pq`, existing `prusage.Service`, existing PR list/detail API contracts.

## Global Constraints

- Work from `docs/performance-contracts-116@5f6c58e6821dfcd95eefff14ea3426d454ae86cd`; do not stack on #117, #118, #119, or #120.
- Preserve `GET /api/v1/repos/:id/prs`, `GET /api/v1/prs/:id`, and `POST /api/v1/prs/:id/refresh-usage` paths and response envelopes.
- Preserve list filtering and ordering exactly: the existing status/month predicates, then open before merged before other statuses, then `created_at DESC`. This ticket does not introduce cursor pagination or a new ID tie-breaker.
- Preserve list fields `usage_status`, `usage_status_reason`, and optional `usage_status_checked_at`; list rows continue omitting `commit_freshness`, while detail continues returning it.
- Preserve current visible precedence deterministically: snapshots are evaluated by `sort_order ASC, id ASC`, and the first non-`fresh` commit supplies the PR-level status and reason. When there are no snapshots, repo-level unbound usage evidence selects `pending_upload` before either `no_checkpoint` reason.
- Preserve the exact current status and reason strings. Do not invent durable `refresh_failed` or `unbound` facts in this ticket; those statuses remain reserved because the current schema has no per-PR marker for them.
- A page freshness read uses a constant number of SQL statements as PR count and commit count grow within supported list bounds. It must not call `EvaluatePRFreshness` in a loop.
- Bulk reads must remain request-context-bound. Do not detach cancellation, create background work, introduce Redis, or add a maintained cache/read model when three bounded SQL fact queries suffice.
- Keep `prusage.Service` and current handler/provider boundaries. Do not modify `sub2api`, SCM provider interfaces, authentication, relay behavior, or frontend code.
- PostgreSQL scale/query-count evidence is environment-sensitive and must be reported separately from ordinary unit tests.
- Tests, fixtures, documentation, and logs use only synthetic identities and repositories such as `alice@example.com`, `bob@example.org`, and `org/alpha`.
- Update `docs/architecture.md` and the active 2026-07-14 performance contract only after behavior lands; do not rewrite older PR freshness specs.
- Maintain this plan as a live ledger. Check each step immediately after it actually completes, and keep the top `Status` consistent with the checkboxes.

## API Compatibility Matrix

| Surface | Preserved request | Preserved response and behavior |
| --- | --- | --- |
| PR list | `status`, `months`, `limit`, `offset` | Existing `items`, `summary`, and `total`; same row fields and ordering; freshness evaluated once for the returned page |
| PR detail | Path `id` | Existing PR entity edges plus complete `commit_freshness`; one selected PR uses the shared classifier |
| Usage refresh | Path `id` | Existing authoritative refresh, reload, and detail response; no SCM/provider behavior change |
| Summary | List filters | Existing database counts; `refresh_failed` remains zero and `pending_upload` remains conservative |

## File Map

- Modify `backend/internal/prusage/freshness.go`: deterministic classifier, bulk fact loading, page evaluator, and single-detail delegation.
- Modify `backend/internal/prusage/freshness_test.go`: exact status/reason precedence and single-detail compatibility.
- Create `backend/internal/prusage/freshness_bulk_test.go`: synthetic scale fixture, recording driver, query-count proof, mixed-state parity, and cancellation/error coverage.
- Modify `backend/internal/handler/interfaces.go`: page-shaped internal freshness capability.
- Modify `backend/internal/handler/pr.go`: one page evaluation and serialization from precomputed results.
- Modify `backend/internal/handler/pr_usage_test.go` and `backend/internal/handler/handler_extended_test.go`: list batching, response compatibility, ordering, and detail regression coverage.
- Modify `docs/architecture.md`: current bounded PR list behavior.
- Modify `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md`: clarify the now-ratified visible precedence under the existing PR freshness contract.
- Maintain this plan with RED/GREEN, review, verification, PR, and two-round CI evidence.

---

### Task 1: Lock Deterministic Freshness Precedence

**Files:**
- Modify: `backend/internal/prusage/freshness.go`
- Modify: `backend/internal/prusage/freshness_test.go`
- Maintain: `docs/superpowers/plans/2026-07-15-pr-list-bulk-freshness.md`

**Interfaces:**
- Consumes: existing `Service.EvaluatePRFreshness`, `PRFreshness`, `CommitFreshness`, and persisted `PRCommitUsageSnapshot.SortOrder`.
- Produces: deterministic in-memory snapshot ordering and a pure `evaluateLoadedPRFreshness` classifier that Task 2 reuses.

- [x] **Step 1: Add exact golden tests for competing anomaly states**

  Add table-driven tests that call the not-yet-existing pure classifier with deliberately unsorted in-memory snapshot slices. Include equal-`sort_order` rows whose IDs and input positions disagree, so `sort_order ASC, id ASC` is proved without relying on PostgreSQL's unspecified order. Cover these exact outcomes:

  ```text
  first commit no_checkpoint, later no_usage_events => no_checkpoint / "No checkpoint matched this PR commit."
  first commit no_usage_events, later stale_snapshot => no_usage_events / "Checkpoint exists but no usage events are bound to it."
  first commit stale_snapshot, later no_checkpoint => stale_snapshot / "Usage events newer than the PR snapshot are bound to this checkpoint."
  all commits have included usage => fresh / "Usage snapshot is current."
  no snapshots plus repo unbound event => pending_upload / existing pending-upload reason
  no snapshots, no pending event, never refreshed => no_checkpoint / "No PR commit snapshot has been generated yet."
  no snapshots, no pending event, refreshed => no_checkpoint / "Snapshot refresh ran but no PR commit rows were recorded."
  ```

  Assert `Commits` is returned by `sort_order ASC, id ASC`, `CheckedAt` is UTC, and the exact current status/reason strings do not change. Keep public `EvaluatePRFreshness` database cases for the three no-snapshot outcomes, but the competing-anomaly RED must come from the pure classifier input.

- [x] **Step 2: Run the precedence tests and record genuine RED**

  Run:

  ```bash
  cd backend && go test ./internal/prusage -run 'TestEvaluate(LoadedPRFreshness|PRFreshnessNoSnapshots)' -count=1
  ```

  Expected: FAIL deterministically because `evaluateLoadedPRFreshness` does not exist. This RED does not depend on a database planner returning an unordered query in one particular order.

  RED evidence (2026-07-15): the command exited 1 with only the expected compile errors for undefined `checkpointUsageFact` and `evaluateLoadedPRFreshness`.

- [x] **Step 3: Extract the pure classifier and order the existing single read**

  Introduce private fact types and a pure function with no database access:

  ```go
  type checkpointUsageFact struct {
      Count         int
      LatestObserved *time.Time
  }

  func evaluateLoadedPRFreshness(
      pr *ent.PrRecord,
      snapshots []*ent.PRCommitUsageSnapshot,
      pendingUnbound bool,
      usageByCheckpoint map[int]checkpointUsageFact,
      checkedAt time.Time,
  ) *PRFreshness
  ```

  The function must preserve the exact branch order and strings currently in `EvaluatePRFreshness`. Sort a copy of the supplied snapshot slice in memory by `SortOrder` and then ID; do not mutate caller-owned ordering. For commit rows, classify checkpoint missing, no events, and newer-event stale in that order; the first non-fresh item in sorted order supplies the overall status and reason.

  Keep the current database calls in `EvaluatePRFreshness` for Task 1, but route the final decision through this classifier. Do not add page/bulk behavior yet.

- [x] **Step 4: Verify focused and package GREEN**

  Run separately:

  ```bash
  cd backend && go test ./internal/prusage -run 'TestEvaluatePRFreshness' -count=1
  cd backend && go test ./internal/prusage -count=1
  git diff --check
  ```

  Expected: PASS; the single-PR method returns the same public fields and exact reasons, with deterministic commit ordering.

  GREEN evidence (2026-07-15): the focused `TestEvaluatePRFreshness` command and full `internal/prusage` package command passed; `git diff --check` exited 0.

- [x] **Step 5: Commit Task 1 and record the checkpoint**

  Commit implementation plus checked Steps 1-4:

  ```bash
  git add backend/internal/prusage/freshness.go backend/internal/prusage/freshness_test.go docs/superpowers/plans/2026-07-15-pr-list-bulk-freshness.md
  git commit -m "test(prs): lock freshness status precedence"
  ```

  After the commit succeeds, check Step 5 and commit:

  ```bash
  git add docs/superpowers/plans/2026-07-15-pr-list-bulk-freshness.md
  git commit -m "docs(plan): record PR freshness contract task 1"
  ```

  Checkpoint evidence (2026-07-15): implementation commit `7659d82` records the classifier, exact contract tests, and Steps 1-4 verification ledger.

---

### Task 2: Evaluate One PR Page With Constant Bulk Queries

**Files:**
- Modify: `backend/internal/prusage/freshness.go`
- Create: `backend/internal/prusage/freshness_bulk_test.go`
- Modify: `backend/internal/prusage/freshness_test.go`
- Maintain: `docs/superpowers/plans/2026-07-15-pr-list-bulk-freshness.md`

**Interfaces:**
- Consumes: Task 1 `evaluateLoadedPRFreshness`, the repository ID already known by the list route, and loaded page `[]*ent.PrRecord` values containing ID and `usage_refreshed_at`.
- Produces: `Service.EvaluatePRFreshnessPage(context.Context, int, []*ent.PrRecord) (map[int]*PRFreshness, error)`, where the integer is `repoConfigID`; existing `EvaluatePRFreshness(context.Context, int)` loads its selected PR plus repo edge and delegates through the same method.

- [x] **Step 1: Add a recording PostgreSQL driver and bounded scale fixture**

  In `freshness_bulk_test.go`, add a test-only driver that embeds `dialect.Driver`, records copied SQL/arguments under a mutex, and delegates unchanged. Open it against the per-test schema DSN from `testdb.OpenWithDSN` via `entsql.OpenDB(dialect.Postgres, db)` and `ent.NewClient(ent.Driver(recorder))`.

  Seed one mixed-state parity fixture and two separate all-snapshot query-count fixtures, resetting the recorder after setup:

  ```text
  parity: 5 PRs, 4 snapshots, 3 checkpoints, and 3 events
          (fresh, snapshot-without-checkpoint, checkpoint-without-event,
           stale snapshot, and one no-snapshot pending-upload PR)
  count-small: 5 PRs x 1 fresh snapshot = 5 snapshots/checkpoints/events
  count-large: 100 PRs x 20 fresh snapshots = 2,000 snapshots/checkpoints/events
  repository: org/alpha
  identities: alice@example.com and bob@example.org
  parity creation/input order deliberately differs from sort_order and ID
  ```

  Use fixed UTC timestamps, Ent bulk creates in bounded batches, and generated synthetic SHAs/dedupe keys. Do not use a real SCM or Relay service.

- [x] **Step 2: Write failing page parity, scale, and query-count tests**

  Add:

  ```text
  TestEvaluatePRFreshnessPageMatchesGoldenSingleResults
  TestEvaluatePRFreshnessPageQueryCountIsConstant
  TestEvaluatePRFreshnessPageHandlesEmptyInputWithoutQueries
  TestEvaluatePRFreshnessPageHonorsCancellation
  ```

  For every mixed-state parity row, compare page status, exact reason, checked-at presence, and ordered commit diagnostics against Task 1's golden outcomes. Assert all five requested PR IDs appear exactly once in the result.

  Identify relevant SQL by table/shape rather than call order and assert the small and large fixture execute the same fact-query shapes:

  ```text
  one snapshot SELECT bounded by pr_record_id IN (...)
  one repo pending-event count scoped by the explicit repo_config_id
  one checkpoint-event aggregate grouped by commit_checkpoint_id with COUNT and MAX(observed_end_at)
  no per-PR or per-commit SELECT
  ```

  Exact argument counts may grow; SQL statement count must not. Compare the exact three fact-query roles for count-small and count-large, and record the exact fixture totals above. Empty input returns an empty non-nil map and records zero SQL statements.

  For cancellation, configure the recording driver to block the snapshot fact query until `ctx.Done()`. Start the page evaluation, wait until that query is in flight, cancel the context, and assert `errors.Is(err, context.Canceled)`. Assert the driver returns from the blocked call, records no pending-event or checkpoint-event query afterward, and has no in-flight test goroutine when the method returns.

- [x] **Step 3: Run Task 2 tests and record RED**

  Run:

  ```bash
  cd backend && go test ./internal/prusage -run 'TestEvaluatePRFreshnessPage' -count=1 -v
  ```

  Expected: FAIL because `EvaluatePRFreshnessPage` does not exist and the current single evaluator issues per-PR/per-checkpoint queries.

  RED evidence (2026-07-15): the command exited 1 with only the expected compile errors that `Service.EvaluatePRFreshnessPage` was undefined at each new page-contract call site.

- [x] **Step 4: Implement the three-query page fact loader**

  Add:

  ```go
  func (s *Service) EvaluatePRFreshnessPage(
      ctx context.Context,
      repoConfigID int,
      prs []*ent.PrRecord,
  ) (map[int]*PRFreshness, error)
  ```

  Validate the service/client and positive `repoConfigID`, reject nil PR elements with an operation-wrapped error, and deduplicate PR IDs. Use the supplied context for all reads. The page API is intentionally one-repository-shaped because `ListByRepo` already owns that path parameter and `PrRecord` does not expose its required repo edge as a scalar field.

  Load all page snapshots with one query bounded by the deduplicated PR IDs. Count pending unbound events for the explicit repository with one query. Collect non-nil checkpoint IDs from the snapshots, then load `COUNT(*)` and `MAX(observed_end_at)` by checkpoint with one grouped query. If no checkpoint IDs exist, skip only that third query; never replace it with a loop. Task 1's classifier, not unspecified database row order, owns final snapshot ordering.

  Use one `checkedAt := time.Now().UTC()` for the page, group facts in Go by IDs, and call Task 1's classifier once per requested PR. Return operation-specific wrapped errors without embedding SQL or fixture values.

  Change `EvaluatePRFreshness(ctx, prID)` to load the selected PR with `WithRepoConfig`, resolve that edge once, and delegate to `EvaluatePRFreshnessPage(ctx, repo.ID, []*ent.PrRecord{pr})`. Preserve selected-detail `CommitFreshness` and all existing errors at the public method boundary. The selected-detail repo-edge load is outside the page query-count assertion.

- [x] **Step 5: Verify repeated scale GREEN and single-detail compatibility**

  Run separately:

  ```bash
  cd backend && go test ./internal/prusage -run 'TestEvaluatePRFreshnessPage' -count=2 -v
  cd backend && go test ./internal/prusage -run 'TestEvaluatePRFreshness' -count=1
  cd backend && go test ./internal/prusage -count=1
  git diff --check
  ```

  Expected: PASS twice with identical visible results and exactly the same three fact-query roles for the 5/5/5 and 100/2,000/2,000 fixtures. Record PR, snapshot, checkpoint, event, and query totals plus skipped-empty-query and cancellation behavior in the plan; do not report elapsed time as a budget.

  GREEN evidence (2026-07-15): the repeated page command passed both runs with `5 PRs / 5 snapshots / 5 checkpoints / 5 events / 3 fact queries` and `100 PRs / 2,000 snapshots / 2,000 checkpoints / 2,000 events / 3 fact queries`. Both runs recorded exactly `snapshots`, `pending_events`, and `checkpoint_facts`; empty input returned a non-nil empty map with zero SQL, and cancellation returned `context.Canceled` after the blocked snapshot query with no later fact query and zero driver calls left in flight. The focused single-detail command, full `internal/prusage` package command, and `git diff --check` also passed.

- [x] **Step 6: Commit Task 2 and record the checkpoint**

  Commit implementation plus checked Steps 1-5:

  ```bash
  git add backend/internal/prusage/freshness.go backend/internal/prusage/freshness_test.go backend/internal/prusage/freshness_bulk_test.go docs/superpowers/plans/2026-07-15-pr-list-bulk-freshness.md
  git commit -m "perf(prs): evaluate freshness facts in bulk"
  ```

  After the commit succeeds, check Step 6 and commit:

  ```bash
  git add docs/superpowers/plans/2026-07-15-pr-list-bulk-freshness.md
  git commit -m "docs(plan): record PR freshness bulk task 2"
  ```

  Checkpoint evidence (2026-07-15): implementation commit `34fb70c` records the three-query page evaluator, bounded PostgreSQL query-count fixtures, cancellation coverage, single-detail delegation, and Steps 1-5 verification ledger.

---

### Task 3: Adopt Page Freshness In The PR List Handler

**Files:**
- Modify: `backend/internal/handler/interfaces.go`
- Modify: `backend/internal/handler/pr.go`
- Modify: `backend/internal/handler/pr_usage_test.go`
- Modify: `backend/internal/handler/handler_extended_test.go`
- Maintain: `docs/superpowers/plans/2026-07-15-pr-list-bulk-freshness.md`

**Interfaces:**
- Consumes: Task 2 `EvaluatePRFreshnessPage` and existing single/detail evaluator.
- Produces: internal `prUsagePageFreshnessEvaluator` capability and one list-time page evaluation without any per-row evaluator loop.

- [ ] **Step 1: Add failing handler batching and compatibility tests**

  Add a spy implementing both single and page evaluation. For a five-row response assert:

  ```text
  page evaluator calls = 1
  page evaluator repo_config_id = route repository ID
  page evaluator receives the five returned PR IDs in response order
  single evaluator calls during list = 0
  list rows preserve title/status/usage counters and supplied freshness fields
  commit_freshness is absent from list rows
  ```

  Add four bounded fallback cases: page success; page evaluator error; a successful map missing one requested PR ID; and an injected single evaluator that has no page capability. The latter three remain HTTP 200 and use the exact existing `unknown` / `Usage freshness has not been evaluated.` fallback for affected rows, with absent checked-at/commit details and zero single-evaluator calls during list handling.

  Preserve and strengthen list ordering coverage for open/merged/other plus `created_at DESC`. Keep the current summary counts unchanged. Extend selected-detail and refresh-response tests to assert the single evaluator still runs once and complete commit diagnostics remain present.

- [ ] **Step 2: Run handler tests and record RED**

  Run:

  ```bash
  cd backend && go test ./internal/handler -run 'TestPR(ListByRepo|HandlerGet|HandlerRefresh).*Fresh|TestPRListByRepo' -count=1
  ```

  Expected: FAIL because the list currently calls `EvaluatePRFreshness` once per returned PR and has no page capability.

- [ ] **Step 3: Add the page capability and serialize precomputed facts**

  Add the internal interface:

  ```go
  type prUsagePageFreshnessEvaluator interface {
      EvaluatePRFreshnessPage(context.Context, int, []*ent.PrRecord) (map[int]*prusage.PRFreshness, error)
  }
  ```

  During `NewPRHandler`, type-assert the injected long-lived `prusage.Service` once and store both single/detail and page evaluators. Split response construction so list serialization accepts an already-computed `*PRFreshness`; do not let it call the single evaluator.

  In `ListByRepo`, after the ordered, offset, limited PR query, call the page evaluator once with the already parsed `repoID`. On an evaluator error or missing map item, retain the existing unknown fallback for that row. When no page capability is configured, also use the bounded unknown fallback; do not fall back to a per-row loop.

  Keep `Get` and `RefreshUsage` on the selected single evaluator path, including `includeCommits=true`. Do not change summary queries or public DTO tags.

- [ ] **Step 4: Verify handler, prusage, and response regression suites**

  Run separately:

  ```bash
  cd backend && go test ./internal/handler -run 'TestPR(ListByRepo|HandlerGet|HandlerRefresh)|TestPRListByRepo' -count=1
  cd backend && go test ./internal/prusage ./internal/handler -count=1
  git diff --check
  ```

  Expected: PASS; one page call produces list freshness, list order and summary are unchanged, and detail/refresh still include commit diagnostics.

- [ ] **Step 5: Commit Task 3 and record the checkpoint**

  Commit implementation plus checked Steps 1-4:

  ```bash
  git add backend/internal/handler/interfaces.go backend/internal/handler/pr.go backend/internal/handler/pr_usage_test.go backend/internal/handler/handler_extended_test.go docs/superpowers/plans/2026-07-15-pr-list-bulk-freshness.md
  git commit -m "perf(prs): batch list freshness evaluation"
  ```

  After the commit succeeds, check Step 5 and commit:

  ```bash
  git add docs/superpowers/plans/2026-07-15-pr-list-bulk-freshness.md
  git commit -m "docs(plan): record PR list freshness task 3"
  ```

---

### Task 4: Architecture, Full Verification, Review, And Draft PR Delivery

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md`
- Maintain: `docs/superpowers/plans/2026-07-15-pr-list-bulk-freshness.md`
- Review only: every file changed since `5f6c58e`

**Interfaces:**
- Consumes: Tasks 1-3 and their test/review evidence.
- Produces: current architecture/contract truth, full verification, independent reviews, and a draft PR targeting `docs/performance-contracts-116` with two green CI rounds.

- [ ] **Step 1: Update current architecture and active contract**

  Update the current PR list paragraph in `docs/architecture.md` to state that page freshness uses three bulk fact shapes rather than per-PR/per-commit reads, selected detail uses the same classifier, and list status fields/order remain compatible.

  Clarify the active performance spec's PR freshness clause with the implemented visible precedence:

  ```text
  snapshot rows are ordered by sort_order then ID; the first non-fresh commit supplies the PR-level status/reason; with no snapshot, repo-level pending evidence precedes the two no-checkpoint reasons
  ```

  Do not rewrite `2026-05-28-pr-sync-job-progress-usage-freshness-design.md` or `2026-06-03-pr-sync-large-repo-recovery-design.md`; they remain historical design records.

- [ ] **Step 2: Run formatting, generation drift, and full repository verification**

  Run separately:

  ```bash
  cd backend && gofmt -w internal/prusage/freshness.go internal/prusage/freshness_test.go internal/prusage/freshness_bulk_test.go internal/handler/interfaces.go internal/handler/pr.go internal/handler/pr_usage_test.go internal/handler/handler_extended_test.go
  cd backend && go test ./...
  cd ae-cli && go test ./...
  cd frontend && npm test
  cd frontend && npm run build
  bash deploy/test/release-frontend-embed-test.sh
  cd frontend && npm run test:e2e:role
  git diff --check
  ```

  Expected: PASS. Report the PostgreSQL scale/query-count tests and role E2E separately as environment-sensitive evidence. Do not check an unrun command.

- [ ] **Step 3: Complete per-task and whole-branch reviews**

  Obtain independent spec/quality review for Tasks 1-3 against their recorded base/head ranges. Resolve every Critical or Important finding with a focused RED/GREEN cycle and re-review.

  Then generate a review package from `5f6c58e` and obtain final spec and standards gates. The final reviewers must explicitly answer:

  ```text
  Is visible status/reason precedence exact and deterministic?
  Can list execution call the single evaluator in a loop?
  Does SQL statement count remain constant across the exact 5/5/5 and 100/2,000/2,000 fixtures?
  Are all fact reads request-context-bound and grouped by stable IDs?
  Are list fields, summary, filtering, and ordering unchanged?
  Do detail and refresh still return complete commit diagnostics?
  Are tests synthetic, non-timing-based, and free of external services?
  ```

- [ ] **Step 4: Commit documentation and verification evidence**

  Set the top status to state implementation, full verification, and reviews are complete while draft PR CI remains pending. Record exact commands, scale shapes/query counts, review verdicts, and environment notes. Then run:

  ```bash
  git add docs/architecture.md docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md docs/superpowers/plans/2026-07-15-pr-list-bulk-freshness.md
  git commit -m "docs(architecture): document bulk PR freshness"
  git status --short
  ```

  Expected: clean worktree after the commit.

- [ ] **Step 5: Push and open the correctly based draft PR**

  Create ignored `.superpowers/sdd/pr-133.md` with `Closes #133`, dependency on draft PR #138, compatibility summary, scale/query-count evidence, verification, review results, and rollback notes. Then run:

  ```bash
  git push -u origin perf/pr-freshness-133
  gh pr create --draft --base docs/performance-contracts-116 --head perf/pr-freshness-133 --title "perf(prs): evaluate list freshness in bulk" --body-file .superpowers/sdd/pr-133.md
  gh pr view --json number,state,isDraft,baseRefName,headRefName,mergeable,mergeStateStatus,url
  ```

  Expected: OPEN draft, base `docs/performance-contracts-116`, head `perf/pr-freshness-133`.

- [ ] **Step 6: Wait for first CI, finalize the ledger, and verify replacement CI**

  Wait for `backend`, `frontend`, `ae-cli`, and `deploy-static` to succeed. Only then check completed delivery steps, set `Status: Complete`, and commit/push the final ledger:

  ```bash
  git add docs/superpowers/plans/2026-07-15-pr-list-bulk-freshness.md
  git commit -m "docs(plan): record PR freshness delivery"
  git push
  gh pr checks --watch
  ```

  Expected: the replacement run is green for all four jobs.

- [ ] **Step 7: Verify final branch and PR state**

  Run:

  ```bash
  git status --short --branch
  gh pr view --json number,state,isDraft,baseRefName,headRefName,headRefOid,mergeable,mergeStateStatus,statusCheckRollup,url
  ```

  Expected: clean local branch, draft PR open, exact base/head, local HEAD equals remote head OID, and all checks succeed. Keep the worktree; do not merge, tag, release, deploy, or run Helm.

## Self-Review Record

- Spec coverage: Task 1 locks exact visible precedence; Task 2 proves constant bulk SQL and scale parity; Task 3 removes list-time per-row evaluation while preserving response behavior; Task 4 covers current docs, review, verification, and delivery.
- Placeholder scan: no TBD/TODO or unspecified implementation/testing step remains.
- Type consistency: Task 1 produces the pure classifier; Task 2 produces `EvaluatePRFreshnessPage(ctx, repoConfigID, prs)`; Task 3 passes the route repository ID through that exact internal handler interface.
- Query-bound consistency: the plan permits one PR-ID-bounded snapshot query, one explicit-repository pending count, and one checkpoint-event aggregate; the separate 5/5/5 and 100/2,000/2,000 fixtures change only bound argument/result counts, not SQL statement count.
- Contract consistency: existing routes, fields, summary, filters, list ordering, detail diagnostics, and exact reason strings are preserved.
- Scope control: no frontend, Redis, relay, SCM, auth, `sub2api`, CDN, release, or Helm behavior enters this ticket.
- Data hygiene: all planned identities, repositories, SHAs, and events are synthetic.
