# Usage Events SQL Aggregation and Pagination Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep the `/events` Usage Records experience bounded as `tool_usage_events` grows by performing filtering, aggregation, stable ordering, and pagination in PostgreSQL while deferring raw diagnostics and duplicate responsive row trees.

**Architecture:** Keep the existing `QueryService`, Gin routes, `limit + offset` API, and Vue page contract. Build one Ent query factory for authorization and filter semantics, use cloned aggregate/count queries for summary and totals, select only bounded list fields before loading bounded display edges, and use PostgreSQL query-plan tests to prove the database performs the work. The frontend retains the same visible mobile cards, desktop table, and detail drawer, but mounts only the active viewport representation and pretty-prints raw JSON only after the advanced detail section opens.

**Tech Stack:** Go 1.23/1.24 toolchain, Gin, Ent 0.14, PostgreSQL, `lib/pq`, Vue 3 `<script setup lang="ts">`, Vue Router, Pinia, TailwindCSS, Vitest, Vue Test Utils.

**Status:** Tasks 1-4 and their review remediation are complete and committed. Task 5 Steps 1-2 are complete in the worktree: current architecture records bounded SQL event delivery and single active row-tree behavior, and the prescribed formatting, Ent generation-drift, full backend/CLI/frontend, production build, role E2E, and diff checks all pass, including PostgreSQL scale/query-plan evidence. Because another worktree already owned port 5173, role E2E was isolated onto a temporary current-worktree Vite port and its fixed test base was restored afterward. Remediation for the final SPEC review's two Important findings and the standards review's two Important plus two Minor findings is complete. Regular-list username privacy now belongs to the actor-aware service and skips the user edge for regular actors; restored pagination is canonicalized to safe integers before loading; aggregate tests permit fewer SQL round trips; and all in-scope event paths are synthetic. Focused recorded-SQL, handler, and frontend regressions, the affected backend packages, the frontend tests/build, and diff checks pass. Task 5 Steps 3-7 remain pending, and Step 3 must stay unchecked until fresh clean SPEC and separate standards reviews complete. Issue [#120](https://github.com/LichKing-2234/ai-efficiency/issues/120) is blocked only by contract PR [#138](https://github.com/LichKing-2234/ai-efficiency/pull/138); implement from `docs/performance-contracts-116@5f6c58e` on `perf/events-120`, and open the draft PR against `docs/performance-contracts-116`.

## Global Constraints

- Preserve `GET /api/v1/events/summary`, `GET /api/v1/events`, `GET /api/v1/events/:id`, and admin-only `GET /api/v1/events/users` paths and response envelopes.
- Preserve list pagination parameters as `limit + offset`; default `limit` is `20`, maximum is `100`, negative offset normalizes to `0`, and the response continues returning zero-based `page`, `page_size`, `total`, and `items`.
- Event list order is `observed_end_at DESC, id DESC`. Do not substitute `created_at` or `observed_start_at`; `observed_start_at` remains a detail field. Directory-run `started_at` ordering belongs to #121 and is outside this plan.
- Summary covers the complete filtered result and is never affected by list `limit` or `offset`.
- Regular users remain forcibly scoped to their own `user_id`; only admins may apply the existing `user_id` filter or receive usernames and raw source/payload diagnostics.
- Preserve inclusive `from`/`to`, exact `tool`, exact `repo_id`, `bound`/`unbound`, and existing unrecognized binding-status behavior.
- Preserve case-insensitive `q` matching for all five historical fields: `tool_session_id`, `tool_event_id`, `dedupe_key`, checkpoint `commit_sha`, and derived `source_basename`.
- `source_basename` search means basename/fallback search, not full directory-path search: trimmed `raw_source_path` basename, otherwise trimmed `raw_source_locator`, otherwise trimmed `tool_session_id`.
- If exact source-basename matching cannot be expressed safely in PostgreSQL, stop and update the active contract before proceeding; never fall back to loading all rows and filtering in Go.
- List SQL must omit `raw_payload`. Admin detail continues loading the selected event's complete `raw_payload`, while regular-user detail continues redacting it.
- Keep handlers thin and all query composition in `backend/internal/toolusage`; do not introduce Redis, change `sub2api`, or add direct external-system coupling.
- Use only synthetic identities and payloads such as `alice@example.com`, `bob@example.org`, `org/alpha`, and generated strings in tests and examples.
- Ent schema changes require `cd backend && go generate ./ent`; commit every generated change that the command produces.
- Maintain this plan as a live ledger: check each step only after running it, and update `Status` immediately when implementation or verification state changes.
- Environment-sensitive role E2E and PostgreSQL plan evidence must be reported separately from ordinary unit-test results.

## API Compatibility Matrix

| Surface | Preserved request | Preserved response and authorization |
| --- | --- | --- |
| Summary | `from`, `to`, `tool`, `repo_id`, `binding_status`, admin `user_id`, `q` | `total_events`, `bound_events`, `unbound_events`, alphabetically stable `tool_counts`; regular users see only self |
| List | Summary filters plus `limit`, `offset` | `items`, `total`, zero-based `page`, `page_size`; row DTO stays raw-payload-free |
| Detail | Path `id` | Existing full DTO; admin raw fields remain complete, regular-user raw fields remain absent |
| User search | `q`, default `limit=20`, maximum `50` | Existing admin-only user options; no response-shape change in this issue |

## File Map

- Modify `backend/internal/toolusage/query.go`: shared SQL filters, exact `q`, aggregates, bounds, projected list, and stable ordering.
- Modify `backend/internal/toolusage/query_test.go`: filter parity, authorization, aggregates, bounds, ties, and selected detail behavior.
- Create `backend/internal/toolusage/query_plan_test.go`: recording driver, scale fixture, `EXPLAIN`, projection, and row-bound proof.
- Modify `backend/internal/toolusage/test_helpers_test.go`: reusable synthetic event builders and batched inserts.
- Modify `backend/internal/handler/events.go` and `events_test.go`: HTTP clamp, metadata, authorization, and compatibility.
- Modify `backend/ent/schema/tool_usage_event.go`; regenerate `backend/ent/`: stable-order indexes and generated metadata.
- Modify `frontend/src/views/events/EventsView.vue` and `frontend/src/__tests__/events-view.test.ts`: active viewport tree and expansion-gated formatting.
- Modify `docs/architecture.md`: current SQL page/projection and frontend rendering behavior.
- Maintain `docs/superpowers/plans/2026-07-15-events-sql-pagination.md`: steps, reviews, verification, PR, and CI state.

---

### Task 1: Shared SQL Filters and Database Summary Aggregates

**Files:**
- Modify: `backend/internal/toolusage/query.go`
- Modify: `backend/internal/toolusage/query_test.go`
- Modify: `backend/internal/toolusage/test_helpers_test.go`
- Maintain: `docs/superpowers/plans/2026-07-15-events-sql-pagination.md`

**Interfaces:**
- Consumes: existing `SummaryRequest`, `ListEventsRequest`, `queryFilter`, Ent predicates, and current authorization rules.
- Produces: `filteredEventsQuery(queryFilter) (*ent.ToolUsageEventQuery, error)`, `eventSearchPredicate(string) predicate.ToolUsageEvent`, and SQL-backed `GetSummary` for Task 2.

- [x] **Step 1: Add synthetic filter fixtures and failing aggregate/query-shape tests**

Add table-driven tests named `TestEventSummaryAndListShareFilterSemantics` and `TestGetSummaryUsesDatabaseAggregates`. Seed two users, two repositories, bound/unbound checkpoints, three tools, inclusive boundary timestamps, and one match for each `q` field. The filter matrix must be explicit:

```go
tests := []struct{ name string; filter queryFilter; want []string }{
	{"time inclusive", queryFilter{From: from, To: to}, []string{"time-from", "time-to"}},
	{"tool", queryFilter{Tool: "codex"}, []string{"q-session"}},
	{"repo", queryFilter{RepoID: alpha.RepoConfigID}, []string{"q-dedupe"}},
	{"bound", queryFilter{BindingStatus: "bound"}, []string{"q-commit"}},
	{"unbound", queryFilter{BindingStatus: "unbound"}, []string{"q-source"}},
}
```

Add five more cases with `Q` values `SESSION-NEEDLE`, `EVENT-NEEDLE`, `DEDUPE-NEEDLE`, `COMMIT-NEEDLE`, and `SOURCE-NEEDLE.JSONL`, each expecting only its corresponding fixture.

Also seed `/private/directory-only-needle/source.jsonl` and assert `q=directory-only-needle` returns no row. Capture Ent SQL and require summary queries to contain `COUNT(` or `GROUP BY`, and never a full entity projection containing `raw_payload`.

- [x] **Step 2: Run the focused tests and verify RED**

Run:

```bash
cd backend && go test ./internal/toolusage -run 'TestEventSummaryAndListShareFilterSemantics|TestGetSummaryUsesDatabaseAggregates' -count=1
```

Expected: filter-visible results still pass under the old implementation, but the aggregate query-shape assertion fails because `GetSummary` calls `queryEvents().All()` and counts rows in Go.

- [x] **Step 3: Implement one authorization/filter query factory and exact SQL search**

Move every predicate out of `queryEvents` into one factory. Keep the PostgreSQL basename expression parameterized and mirror `sourceBasename` fallback order:

```go
func (s *QueryService) filteredEventsQuery(filter queryFilter) (*ent.ToolUsageEventQuery, error)
func eventSearchPredicate(q string) predicate.ToolUsageEvent
func filterFromSummary(req SummaryRequest) queryFilter
func filterFromList(req ListEventsRequest) queryFilter
```

The factory validates client and positive actor ID, enforces regular-user self scope or admin `UserID`, then adds inclusive UTC time, trimmed tool, positive repo, recognized binding state, and nonblank `q` predicates in that order.

`eventSearchPredicate` must OR the three direct string predicates, `HasCommitCheckpointWith(commitcheckpoint.CommitShaContainsFold(q))`, and a PostgreSQL `CASE` expression that strips trailing `/`, takes the final path segment, and then falls back to locator/session. Keep `%` in a bound argument rather than SQL text.

- [x] **Step 4: Replace Go summary loops with cloned SQL aggregates**

Implement `GetSummary` from the shared query:

```go
total, err := base.Clone().Count(ctx)
bound, err := base.Clone().Where(toolusageevent.CommitCheckpointIDNotNil()).Count(ctx)
unbound, err := base.Clone().Where(toolusageevent.CommitCheckpointIDIsNil()).Count(ctx)
err = base.Clone().Order(ent.Asc(toolusageevent.FieldTool)).GroupBy(toolusageevent.FieldTool).
	Aggregate(ent.As(ent.Count(), "count")).Scan(ctx, &tools)
```

Wrap each failure with operation context. Preserve `ToolCounts: []` rather than `null`, alphabetical tool order, and current bound/unbound behavior even when `binding_status` is already present in `base`.

- [x] **Step 5: Run focused and package tests and verify GREEN**

Run:

```bash
cd backend && go test ./internal/toolusage -run 'TestEventSummaryAndListShareFilterSemantics|TestGetSummaryUsesDatabaseAggregates|TestListEventsScopesRegularUserToOwnRows|TestGetEventDetail' -count=1
cd backend && go test ./internal/toolusage -count=1
```

Expected: PASS; captured summary SQL contains only aggregate/count projections, and all five `q` fields retain their visible results.

- [x] **Step 6: Update the live ledger and commit Task 1**

Mark completed Task 1 steps immediately, then run:

```bash
git add backend/internal/toolusage/query.go backend/internal/toolusage/query_test.go backend/internal/toolusage/test_helpers_test.go docs/superpowers/plans/2026-07-15-events-sql-pagination.md
git commit -m "perf(events): aggregate filtered summaries in SQL"
```

Expected: one Task 1 commit; no frontend, handler, or architecture changes yet.

---

### Task 2: Bounded List Projection, Stable Pagination, and Indexes

**Files:**
- Modify: `backend/internal/toolusage/query.go`
- Modify: `backend/internal/toolusage/query_test.go`
- Modify: `backend/ent/schema/tool_usage_event.go`
- Regenerate: `backend/ent/`
- Maintain: `docs/superpowers/plans/2026-07-15-events-sql-pagination.md`

**Interfaces:**
- Consumes: Task 1 `filteredEventsQuery` and existing `EventListRow`/edge display helpers.
- Produces: `DefaultEventPageSize`, `MaxEventPageSize`, `normalizeEventPage`, a raw-payload-free projected page, and stable indexes used by Task 3.

- [x] **Step 1: Write failing service tests for bounds, ties, projection, and compatibility**

Add these tests:

```text
TestListEventsDefaultsToTwenty
TestListEventsClampsLimitToOneHundred
TestListEventsNormalizesNegativeOffset
TestListEventsOrdersEqualObservedEndAtByIDDescending
TestListEventsPagesWithoutTieDuplicatesOrOmissions
TestListEventsPrimarySelectOmitsRawPayload
TestListEventsKeepsAdminDetailRawPayloadComplete
```

Seed 125 events with the same `observed_end_at`, deterministic IDs, and a 16 KiB synthetic raw JSON string. Request limits `0`, `101`, and `1000`; expect 20, 100, and 100 rows respectively. Request offsets `0`, `20`, and `-5`; expect deterministic descending IDs and no overlap between valid pages. Capture the primary list SQL and assert it contains `LIMIT`, contains both descending order fields, and does not contain `raw_payload` in the selected column list.

- [x] **Step 2: Run the bounded-list tests and verify RED**

Run:

```bash
cd backend && go test ./internal/toolusage -run 'TestListEvents(Default|Clamp|Normalize|Order|Pages|Primary|Keeps)' -count=1
```

Expected: FAIL because the current service treats limit `0` as unbounded, accepts values above 100, selects full entities including `raw_payload`, and orders ties only by `observed_end_at`.

- [x] **Step 3: Add defensive page normalization and stable database pagination**

Add exported constants for the handler and normalize again inside the service:

```go
const DefaultEventPageSize = 20
const MaxEventPageSize = 100
func normalizeEventPage(limit, offset int) (normalizedLimit, normalizedOffset int)
```

The function maps nonpositive limit to 20, values above 100 to 100, negative offset to 0, and otherwise returns inputs unchanged.

In `ListEvents`, count `base.Clone()` before paging, then apply `Order(ent.Desc(observed_end_at), ent.Desc(id))`, `Offset`, and `Limit` before `All`. Explicitly select only ID, foreign keys, row metrics, session/event/dedupe fields, observed end, and raw source path/locator needed to derive `source_basename`; do not select `raw_payload`, workspace, observed start, context percentage, or created time. Eager-load repo/user/checkpoint edges only after the page is bounded, and restrict each edge to its display fields.

- [x] **Step 4: Add stable-order indexes and regenerate Ent**

Keep existing indexes and add:

```go
index.Fields("observed_end_at", "id").Annotations(entsql.DescColumns("observed_end_at", "id")),
index.Fields("user_id", "observed_end_at", "id").Annotations(entsql.DescColumns("observed_end_at", "id")),
index.Fields("repo_config_id", "observed_end_at", "id").Annotations(entsql.DescColumns("observed_end_at", "id")),
index.Fields("tool", "observed_end_at", "id").Annotations(entsql.DescColumns("observed_end_at", "id")),
```

Run:

```bash
cd backend && go generate ./ent
gofmt -w internal/toolusage/query.go internal/toolusage/query_test.go ent/schema/tool_usage_event.go
git diff --check
```

Expected: Ent generated migration metadata contains the four indexes, all descending annotations target valid columns, and formatting is clean.

- [x] **Step 5: Run focused, schema, and package tests and verify GREEN**

Run:

```bash
cd backend && go test ./internal/toolusage -run 'TestListEvents|TestGetEventDetail' -count=1
cd backend && go test ./internal/attribution ./internal/toolusage -count=1
```

Expected: PASS; list length never exceeds 100, total remains the complete filtered count, tie pages are repeatable, and admin detail still contains the selected raw payload.

- [x] **Step 6: Update the live ledger and commit Task 2**

```bash
git add backend/ent backend/internal/toolusage/query.go backend/internal/toolusage/query_test.go docs/superpowers/plans/2026-07-15-events-sql-pagination.md
git commit -m "perf(events): page lightweight event rows in SQL"
```

Expected: Task 2 is independently reviewable, including schema and generated code.

---

### Task 3: Large Synthetic Fixture and PostgreSQL Query-Plan Proof

**Files:**
- Create: `backend/internal/toolusage/query_plan_test.go`
- Modify: `backend/internal/toolusage/test_helpers_test.go`
- Modify only if evidence requires it: `backend/ent/schema/tool_usage_event.go`
- Regenerate only if indexes change: `backend/ent/`
- Maintain: `docs/superpowers/plans/2026-07-15-events-sql-pagination.md`

**Interfaces:**
- Consumes: Task 1 filter factory, Task 2 bounds/projection/indexes, and `testdb.OpenWithDSN`.
- Produces: repeatable query-plan and bounded-materialization evidence for every #120 backend acceptance criterion.

- [x] **Step 1: Add a recording PostgreSQL driver and scale fixture helper**

Create a test-only `recordingDriver` that embeds `dialect.Driver`, overrides `Query`, copies SQL plus arguments under a mutex, and delegates unchanged. Open it with the schema DSN returned by `testdb.OpenWithDSN` through `entsql.OpenDB(dialect.Postgres, db)` and `ent.NewClient(ent.Driver(recorder))`.

Seed exactly 2,400 synthetic events in batches of 200:

```text
2 users: alice@example.com, bob@example.org
2 repositories: org/alpha, org/beta
3 tools: codex, claude, kiro
50 percent bound and 50 percent unbound
64 repeated observed_end_at values to force ID tie-breaking
16 KiB raw_payload string per event
distinct session/event/dedupe/source values for q coverage
```

Use Ent bulk creates, fixed UTC timestamps, and generated repeated strings. Run `ANALYZE tool_usage_events` after insertion so the planner has current statistics.

- [x] **Step 2: Write failing large-fixture behavior and SQL-shape tests**

Add `TestLargeEventFixturePreservesFiltersAndBounds` with subtests for regular actor scope, admin `user_id`, time, tool, repository, bound/unbound, and all five `q` fields. For each subtest:

1. call `GetSummary` and `ListEvents` with limit 100;
2. compare `summary.TotalEvents` with list `total`;
3. require `len(rows) <= 100`;
4. compare visible DTOs with deterministic expected IDs/fields;
5. require no raw payload marker appears in captured list SQL.

Add `TestLargeEventFixtureStablePages` and concatenate every page at limits 20, 50, and 100. Assert no duplicate ID, no omitted ID, and global `(observed_end_at DESC, id DESC)` order.

- [x] **Step 3: Write exact `EXPLAIN` assertions for list and summary queries**

Identify captured SQL by operation shape rather than call order: list SQL contains `FROM "tool_usage_events"`, `ORDER BY`, and `LIMIT`; summary SQL contains `COUNT(` or `GROUP BY`. Execute the exact captured SQL with its captured bound arguments under:

```sql
EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
```

Parse the JSON and assert:

```text
list plan contains a Limit node
list Actual Rows is at most 100
list SQL projection excludes raw_payload
list order contains observed_end_at DESC and id DESC
representative default user/global queries use one of the new event order indexes
summary plans contain Aggregate nodes
summary SQL has no LIMIT or OFFSET and returns aggregate rows, not event entities
```

Do not assert exact costs, timings, buffer counts, or one complete node tree; those vary by PostgreSQL version and machine.

- [x] **Step 4: Run the new tests and verify RED, then make the smallest evidence-driven adjustment**

Run:

```bash
cd backend && go test ./internal/toolusage -run 'TestLargeEventFixture' -count=1 -v
```

Expected first run: any missing query capture, projection, or index-use assertion fails with the recorded SQL/plan in the test message. Fix only the demonstrated query/index mismatch; do not add speculative indexes for every filter combination. If schema changes, regenerate Ent and rerun Task 2 tests.

- [x] **Step 5: Run scale tests twice and the package suite and verify GREEN**

Run:

```bash
cd backend && go test ./internal/toolusage -run 'TestLargeEventFixture' -count=2 -v
cd backend && go test ./internal/toolusage -count=1
```

Expected: both scale runs PASS with identical visible ordering and bounds. Record fixture size, payload size, selected indexes, and plan node assertions in this task's ledger notes; do not report elapsed time as a performance budget.

Task 3 verification evidence (2026-07-15):

- The fixture persists exactly 2,400 events in batches of 200 across two synthetic users, two repositories, and three tools. It contains 1,200 bound and 1,200 unbound events, 64 repeated `observed_end_at` values, unique session/event/dedupe/source fields, and one 16 KiB generated raw payload string per event.
- The controlled RED mutation removed the `id DESC` tie-breaker and produced SQL-shape failures plus duplicate and out-of-order concatenated pages. Restoring the existing Task 2 tie-breaker returned the suite to GREEN; no query or schema/index adjustment was required in the final Task 3 diff.
- `go test ./internal/toolusage -run 'TestLargeEventFixture' -count=2 -v` passed twice with identical visible ordering and bounds. `go test ./internal/toolusage -count=1` also passed.
- PostgreSQL selected `toolusageevent_observed_end_at_id` for the representative global list and `toolusageevent_user_id_observed_end_at_id` for the representative regular-user list in both scale runs.
- List plans contain `Limit`, materialize at most 100 rows at that node, retain `observed_end_at DESC, id DESC`, and exclude `raw_payload` from the projection. Captured scalar and tool-count summary plans contain `Aggregate`, have no `LIMIT` or `OFFSET`, and return respectively one row or the exact number of visible tool groups rather than event entities.
- Review remediation added first-page evidence for default/clamped/negative inputs, exact-tool and basename-only negative cases, an opposite-case commit positive, and exact `ORDER BY` expression parsing. A controlled mutation made all new assertions fail before production behavior was restored with no remaining production diff.
- After restoration, `go test ./internal/toolusage -run 'TestLargeEventFixture' -count=2 -v` and `go test ./internal/toolusage -count=1` passed, and `git diff --check` passed.

- [x] **Step 6: Update the live ledger and commit Task 3**

```bash
git add backend/internal/toolusage/query_plan_test.go backend/internal/toolusage/test_helpers_test.go backend/ent docs/superpowers/plans/2026-07-15-events-sql-pagination.md
git commit -m "test(events): prove bounded SQL event reads"
```

Expected: the commit contains synthetic test evidence and only evidence-required schema refinements.

#### Task 3 Review Follow-up

- [x] Prove on the 2,400-row fixture that limit `0` defaults to `20`, a limit above `100` clamps to `100`, and a negative offset returns the first page while preserving the complete total.
- [x] Add discriminating summary/list cases for a partial tool that must not match, an opposite-case commit needle that must match, and a directory-only source-path fragment that must not match.
- [x] Parse the captured list SQL and require the exact `ORDER BY` expression list `observed_end_at DESC, id DESC` before `LIMIT`, rejecting any extra expression.
- [x] Replace the exact four-summary-query count and generic three-row cap with at-least-one aggregate capture plus role-specific scalar/tool-group row evidence.
- [x] Run focused mutation-backed RED checks, restore production behavior, then pass the scale suite twice, the full `toolusage` package, and `git diff --check`.
- [x] Append review-remediation evidence to `.superpowers/sdd/120-task-3-report.md`, self-review the focused diff, and commit without pushing.

---

### Task 4: HTTP Bounds and Frontend Single-Tree/Lazy-JSON Rendering

**Files:**
- Modify: `backend/internal/handler/events.go`
- Modify: `backend/internal/handler/events_test.go`
- Modify: `frontend/src/views/events/EventsView.vue`
- Modify: `frontend/src/__tests__/events-view.test.ts`
- Maintain: `docs/superpowers/plans/2026-07-15-events-sql-pagination.md`

**Interfaces:**
- Consumes: Task 2 exported page constants and unchanged API DTOs from `frontend/src/types/index.ts`.
- Produces: bounded HTTP metadata, one responsive row subtree per event, and lazy raw-payload formatting without API/type changes.

- [x] **Step 1: Write failing handler compatibility and boundary tests**

Add tests named:

```text
TestEventsListDefaultsToTwenty
TestEventsListClampsLimitToOneHundred
TestEventsListNormalizesInvalidAndNegativePaging
TestEventsListPreservesZeroBasedPageMetadata
TestEventsListOmitsRawPayloadAndRegularUsername
TestEventDetailPreservesAdminPayloadAndRegularRedaction
```

Exercise no limit, `limit=0`, `limit=101`, `limit=1000`, `offset=-20`, and `limit=20&offset=40`. Assert `page_size` is 20 or 100 after normalization, `page` is 0 or 2, no list item contains `raw_payload`, and current admin/regular detail behavior is unchanged.

- [x] **Step 2: Run handler tests and verify RED, then share service limits**

Run:

```bash
cd backend && go test ./internal/handler -run 'TestEventsList|TestEventDetail' -count=1
```

Expected: requests above 100 initially report the unbounded requested `page_size`. Update `parseEventsListRequest` to clamp with `toolusage.DefaultEventPageSize` and `toolusage.MaxEventPageSize`; keep the service's defensive clamp and existing zero-based response calculation. Rerun the command and expect PASS.

- [x] **Step 3: Write failing viewport and formatting tests**

In Vitest, install a controllable `window.matchMedia` mock before mounting. Seed three rows and add stable selectors `data-event-row="mobile"` and `data-event-row="desktop"`. Add tests:

```text
renders exactly one mobile row subtree per event below 768px
renders exactly one desktop row subtree per event at or above 768px
switches representation once when the media query changes
does not stringify raw payload while advanced details are closed
formats raw payload once after advanced details opens
resets formatting state when the detail drawer closes
```

Spy on `JSON.stringify` only around drawer interaction, restore it in `finally`, and assert the large payload marker is absent before the `<details>` toggle event.

- [x] **Step 4: Run frontend tests and verify RED**

Run:

```bash
cd frontend && npm test -- src/__tests__/events-view.test.ts
```

Expected: row count is doubled because both responsive branches are mounted, and `JSON.stringify` runs while the native details element is closed.

- [x] **Step 5: Implement active viewport rendering and expansion-gated formatting**

Import `onUnmounted`, create one `MediaQueryList` for `(min-width: 768px)`, mirror `.matches` into `desktopEventRows`, and add/remove the `change` listener with component lifecycle. Replace CSS-only row branches with:

```ts
const showMobileEventRows = computed(() => rows.value.length > 0 && !desktopEventRows.value)
const showDesktopEventRows = computed(() => rows.value.length > 0 && desktopEventRows.value)
```

Use `v-if="showMobileEventRows"` on the existing mobile container and `v-if="showDesktopEventRows"` on the existing desktop container; add the stable `data-event-list` and `data-event-row` selectors without changing their child markup.

Track `advancedDetailsOpen`, reset it in `openDetail` and `closeDetail`, update it from `<details @toggle>`, and render the `<pre>` only when true. Put `JSON.stringify` in a lazy computed value referenced only by that `v-if`. Preserve existing visible card/table fields, keyboard behavior, drawer fetch timing, and admin guards.

- [x] **Step 6: Run focused tests, frontend suite, and build and verify GREEN**

Run:

```bash
cd frontend && npm test -- src/__tests__/events-view.test.ts src/__tests__/api-events.test.ts
cd frontend && npm test
cd frontend && npm run build
```

Expected: focused and full tests PASS, TypeScript/build PASS, exactly one row subtree exists per event in either viewport, and no API/type/i18n change is required.

- [x] **Step 7: Update the live ledger and commit Task 4**

```bash
git add backend/internal/handler/events.go backend/internal/handler/events_test.go frontend/src/views/events/EventsView.vue frontend/src/__tests__/events-view.test.ts docs/superpowers/plans/2026-07-15-events-sql-pagination.md
git commit -m "perf(events): bound API pages and defer row detail work"
```

Expected: one full-stack compatibility commit with handler and frontend evidence.

---

### Task 5: Architecture, Review, Verification, and Draft PR Delivery

**Files:**
- Modify: `docs/architecture.md`
- Maintain: `docs/superpowers/plans/2026-07-15-events-sql-pagination.md`
- Review only: all files changed since `5f6c58e`

**Interfaces:**
- Consumes: Tasks 1-4 and the active performance contract.
- Produces: current architecture truth, complete verification evidence, two review gates, and draft PR delivery against the contract branch.

- [x] **Step 1: Update current architecture without rewriting historical specs**

Update the `/events` paragraph in `docs/architecture.md` to state:

```text
Summary and list share database-side authorization/filter predicates. Summary values are SQL aggregates; list totals and pages are counted/ordered in PostgreSQL with default 20, maximum 100, and observed_end_at/id descending order. List projection excludes raw payloads and detail loads the selected diagnostic record on demand. The Vue page mounts only the active mobile or desktop row representation and formats admin raw JSON only after expansion.
```

Do not mark unrelated #115 targets as implemented and do not rewrite `2026-05-21-global-tool-usage-events-page-design.md`.

- [x] **Step 2: Run formatting, generation-drift, and full repository verification**

Run exactly:

```bash
cd backend && gofmt -w internal/toolusage/query.go internal/toolusage/query_test.go internal/toolusage/query_plan_test.go internal/toolusage/test_helpers_test.go internal/handler/events.go internal/handler/events_test.go ent/schema/tool_usage_event.go
cd backend && go generate ./ent
git diff --exit-code -- backend/ent
cd backend && go test ./...
cd ae-cli && go test ./...
cd frontend && npm test
cd frontend && npm run build
cd frontend && npm run test:e2e:role
git diff --check
```

Expected: all commands PASS. Report PostgreSQL/query-plan and role E2E as environment-sensitive evidence, and keep any unrun command unchecked.

- [ ] **Step 3: Perform spec review and standards review as separate gates**

Request one review against issue #120 plus the active performance spec and one review against `AGENTS.md` plus repo conventions. Required review questions:

```text
Does every summary/list filter share one SQL predicate path?
Can any list request exceed 100 or select raw_payload?
Is ordering exactly observed_end_at DESC, id DESC across ties and pages?
Are all five q fields preserved without full-path broadening or Go scans?
Do regular-user scope and raw-detail redaction remain fail-closed?
Do scale and EXPLAIN tests prove bounded rows without brittle timing/cost checks?
Does the frontend mount one row subtree and defer JSON formatting until expansion?
```

Fix every Critical or Important finding with a focused RED/GREEN cycle, rerun affected suites, and record Minor findings explicitly if intentionally deferred.

**Remediation status (2026-07-15):** The initial final SPEC review reported two Important findings and no Critical or Minor findings. Focused RED/GREEN cycles now cover a `limit=101` deep link advancing from offset 0 to 100 without a gap, and the complete regular/admin detail response matrix for `username`, `raw_source_path`, `raw_source_locator`, and `raw_payload`. The affected backend packages, focused frontend tests, and frontend production build pass. This step remains unchecked until the controller obtains a clean replacement SPEC review and a separate standards review.

**Standards remediation status (2026-07-15):** The initial standards review reported two Important and two Minor findings. All four are remediated with no Minor deferral. Strict RED/GREEN cycles prove that regular service results omit username without a user-edge SQL query while admin results preserve it, and decimal deep-link pagination normalizes to aligned integer requests and route state. The aggregate test now requires at least one aggregate statement without prescribing four round trips, and every `/Users/admin/...` event path in the changed handler/toolusage tests is replaced with synthetic `alice`/`bob` paths. Focused recorded-SQL, handler, and frontend tests, the affected backend packages, the frontend production build, and diff checks pass. Step 3 remains unchecked pending clean replacement reviews.

- [ ] **Step 4: Record delivery evidence and commit architecture/ledger state**

Set `Status` to state that implementation, full verification, and reviews are complete while draft PR CI remains pending. Record test commands, scale fixture facts, selected indexes, review outcomes, and any environment note. Then run:

```bash
git add docs/architecture.md docs/superpowers/plans/2026-07-15-events-sql-pagination.md
git commit -m "docs(architecture): document bounded event delivery"
git status --short
```

Expected: clean worktree after the commit.

- [ ] **Step 5: Push and open the correctly based draft PR**

Run:

```bash
git push -u origin perf/events-120
gh pr create --draft --base docs/performance-contracts-116 --head perf/events-120 --title "perf(events): aggregate and page usage events in SQL" --body-file .superpowers/sdd/pr-120.md
gh pr view --json number,state,isDraft,baseRefName,headRefName,mergeable,mergeStateStatus,url
```

Create `.superpowers/sdd/pr-120.md` as an ignored delivery artifact with `Closes #120`, dependency on draft PR #138, summary, SQL/fixture evidence, frontend evidence, verification commands, and rollback/index-migration notes. Expected PR state: `OPEN`, draft, base `docs/performance-contracts-116`, head `perf/events-120`.

- [ ] **Step 6: Wait for first-round CI, finalize the ledger, and run replacement CI**

Wait for `backend`, `frontend`, `ae-cli`, and `deploy-static` to succeed. Only then mark every completed checkbox, set `Status: Complete`, and commit the final ledger:

```bash
git add docs/superpowers/plans/2026-07-15-events-sql-pagination.md
git commit -m "docs(plan): record events SQL pagination delivery"
git push
gh pr checks --watch
```

Expected: replacement CI is green for all four jobs.

- [ ] **Step 7: Verify final branch and PR state**

Run:

```bash
git status --short --branch
gh pr view --json number,state,isDraft,baseRefName,headRefName,headRefOid,mergeable,mergeStateStatus,statusCheckRollup,url
```

Expected: clean `perf/events-120`, draft PR remains open, base/head are exact, merge state is clean or mergeable, head OID equals local `HEAD`, and all checks report success. Keep the worktree for review iteration; do not tag, release, deploy, run Helm, or merge #138/#120 from this plan.

## Self-Review Record

- Spec coverage: Tasks 1-3 cover shared filters, SQL aggregates, bounded stable pages, omitted list payloads, selected detail payload, scale fixtures, and query plans; Task 4 covers one row subtree and lazy JSON; Task 5 covers current architecture and delivery.
- API compatibility: all existing routes, query names, zero-based page metadata, DTOs, authorization, and detail redaction are explicitly preserved.
- Search consistency: all five historical `q` fields are enumerated in constraints, fixtures, SQL implementation, scale tests, and review gates.
- Ordering consistency: every task uses `observed_end_at DESC, id DESC`; no event step introduces `started_at`, `created_at`, or cursor pagination.
- Bound consistency: service and handler both use default 20 and maximum 100; frontend options remain 20/50/100.
- Test-data hygiene: every identity, repository, and payload is synthetic.
- Type consistency: Task 1 produces `filteredEventsQuery`; Tasks 2-3 consume it. Task 2 exports page constants; Task 4 consumes them. No API or frontend type rename is planned.
- Scope control: Redis, CDN, route hydration, Directory Sync runs, team usage, `sub2api`, releases, and Helm are excluded.
