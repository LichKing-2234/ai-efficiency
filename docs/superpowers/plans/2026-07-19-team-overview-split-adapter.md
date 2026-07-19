# Team Overview Split Adapter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Issue:** [#172](https://github.com/LichKing-2234/ai-efficiency/issues/172)

**Status:** Complete. Implementation, contract synchronization, full verification, acceptance review, and commit are finished; push, PR, CI, and integration are tracked in GitHub rather than this implementation ledger.

**Goal:** Make the deprecated Team Overview endpoint assemble its historical response only from the current split read models and shared organization projection, then delete the monolithic Overview Relay origin, Redis lane, and production metric.

**Architecture:** One request-scoped split-read context normalizes the range and resolves the authorized representative scope plus current provider configuration once. Compatibility Overview reads Summary, Trend, and complete Members through their existing typed caches, then projects the recursive legacy tree from the same scope and member snapshot; Organization remains the only branch-serving runtime lane and the compatibility adapter owns no origin or cache.

**Tech Stack:** Go 1.24, Gin, Ent, Redis readcache, miniredis, Vue 3/Vitest.

## Global Constraints

- Work only in `/Users/admin/ai-efficiency/.worktrees/team-overview-compatibility-adapter-172` on `refactor/team-overview-compatibility-adapter-172`.
- Preserve the complete historical Overview JSON shape, accepted `page`/`page_size` no-op behavior, authorization mapping, and compatibility headers.
- Do not make internal HTTP calls or invoke paginated Organization branches to rebuild the full legacy tree.
- Summary, Trend, Members, and Organization remain the only Relay/read-cache origin owners; split endpoints must never call Overview.
- Delete the `team-usage-snapshot`/`team_usage_overview` lane rather than leaving a dormant second cache.
- Use only synthetic identities and domains in tests.
- Update each checkbox immediately after the action completes.
- Do not release, tag, deploy, sample production, or run Helm.

---

### Task 1: Lock The Adapter Contract With RED Tests

**Files:**
- Modify: `backend/internal/teamusage/service_test.go`
- Modify: `backend/internal/handler/team_usage_test.go`
- Modify: `backend/internal/teamusage/cache_metrics_test.go`
- Modify: `backend/cmd/server/cache_metrics_test.go`

**Interfaces:**
- Compatibility Overview must reuse the split Summary, Trend, and Members cache keys and build `member_tree` from the same authorized scope/member value.
- A soft Trend failure may mark only trend fields unavailable; it must not overwrite an independently available Summary or Members value.
- No Redis key or production metric named `team-usage-snapshot` or `team_usage_overview` remains.

- [x] **Step 1: Add adapter/cache/failure-isolation regressions**

  Add focused tests that cold and warm Overview calls create/reuse only split keys, preserve the historical DTO/tree, ignore `page`/`page_size`, retain hard authorization errors, and keep Summary plus Members available when Trend returns its supported `provider_error` state. Update real-HTTP coverage to prove compatibility headers on success/failure and no compatibility cache population.

- [x] **Step 2: Run focused RED**

  ```bash
  cd backend
  go test ./internal/teamusage ./internal/handler ./cmd/server -run 'Overview|Compatibility|CacheMetrics' -count=1
  ```

  Expected: fail because Overview still calls `readOverviewSnapshot`, uses its monolithic origin/cache, couples Summary to Trend failure, and production metrics still include `team_usage_overview`.

### Task 2: Compose Overview From Shared Split Read Models

**Files:**
- Modify: `backend/internal/teamusage/service.go`
- Modify: `backend/internal/teamusage/organization.go`
- Modify: `backend/internal/teamusage/service_test.go`

**Interfaces:**
- `splitReadRequest` carries actor, normalized params, authorized scope, current provider configuration, and the deterministic scope hash for one request.
- `readSummarySnapshotForRequest`, `readTrendSnapshotForRequest`, and `readMembersSnapshotForRequest` retain the existing cache/origin behavior while public split endpoints create their own request context.
- `Overview` creates one request context, reads the three typed values, maps `SummaryAggregate` to `OverviewSummary`, and uses the organization-owned pure compatibility projection for `member_tree`.

- [x] **Step 1: Introduce one guarded split-read request context**

  Refactor the three split readers without changing their public response DTOs, cache keys, stale policy, or error classification. Keep provider origin resolution inside the owning lane loader.

- [x] **Step 2: Replace the monolithic Overview calculation**

  Assemble the legacy response from Summary, Trend, Members, and the same scope hierarchy. Preserve large-scope behavior and non-nil empty slices. Do not expose split freshness/scope metadata in the legacy DTO.

- [x] **Step 3: Run focused GREEN**

  ```bash
  cd backend
  gofmt -w internal/teamusage/service.go internal/teamusage/organization.go internal/teamusage/service_test.go
  go test ./internal/teamusage ./internal/handler -run 'Overview|Summary|Trend|Members|Organization|Compatibility' -count=2
  ```

### Task 3: Delete The Monolithic Cache And Metric

**Files:**
- Modify: `backend/internal/teamusage/summary_cache.go`
- Modify: `backend/internal/teamusage/types.go`
- Modify: `backend/internal/teamusage/summary_cache_test.go`
- Modify: `backend/internal/teamusage/cache_metrics_test.go`
- Modify: `backend/cmd/server/cache_metrics.go`
- Modify: `backend/cmd/server/cache_metrics_test.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `deploy/observability/README.md`

**Interfaces:**
- `SnapshotCache` retains only Summary, Trend, Members, and Organization typed lanes.
- `SnapshotCacheOptions` has no `OverviewMetrics`; server startup registers no `team_usage_overview` recorder.
- Old `team-usage-snapshot` Redis values become unreachable and expire naturally under their existing TTL.

- [x] **Step 1: Remove Overview cache APIs, envelope types, validation, tests, and telemetry wiring**

  Delete `GetOrLoad`, `snapshotCacheKey`, Overview cache schema/value types, overview-only validation, production metric registration, and tests tied solely to the removed lane. Keep shared generic read-cache machinery and split-lane tests unchanged.

- [x] **Step 2: Prove there is one origin/cache set**

  ```bash
  rg -n 'readOverviewSnapshot|generateOverviewSnapshot|team-usage-snapshot|team_usage_overview|OverviewMetrics|SnapshotOriginLoader|SnapshotCacheResult' backend deploy
  ```

  Expected: no production hits; test names or historical plan text are reviewed separately and do not keep runtime APIs alive.

- [x] **Step 3: Run backend GREEN and race checks**

  ```bash
  cd backend
  go test ./internal/readcache ./internal/teamusage ./internal/handler ./cmd/server -count=1
  go test -race ./internal/readcache ./internal/teamusage ./internal/handler -run 'Overview|Compatibility|Cache' -count=1
  ```

### Task 4: Synchronize Current Contracts And Deliver

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md`
- Modify: this plan

**Interfaces:**
- Current docs distinguish the temporary legacy response adapter from the deleted monolithic origin/cache.
- Compatibility removal still waits for the announced release/window and production consumer evidence tracked outside #172.

- [x] **Step 1: Update architecture, active spec, observability docs, and this ledger**

  Record split-cache assembly, one guarded scope snapshot, tree projection ownership, removed Overview metric/key, accepted no-op pagination parameters, and unchanged deprecation/removal timing.

- [x] **Step 2: Run the full required verification matrix**

  ```bash
  git diff --check
  cd backend && go test ./... -count=1 && go vet ./... && go build ./...
  cd ../frontend && npm test && npm run build && npm run test:e2e:role
  cd ../ae-cli && go test ./... -count=1
  cd .. && bash deploy/test/release-frontend-embed-test.sh
  ```

  Run role E2E against an owned strict-port Vite listener, stop it afterward, and record environment-only warnings separately.

- [x] **Step 3: Review standards and issue acceptance criteria**

  Require no Critical or Important findings, no current frontend/internal Overview caller, no monolithic cache/origin production symbol, synthetic-only fixtures, and a clean worktree diff.

- [x] **Step 4: Commit without pushing**

  ```bash
  git add backend deploy/observability/README.md docs/architecture.md docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md docs/superpowers/plans/2026-07-19-team-overview-split-adapter.md
  git commit -m "refactor(teamusage): compose overview from split read models"
  ```
