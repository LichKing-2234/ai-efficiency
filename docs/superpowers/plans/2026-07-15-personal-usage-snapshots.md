# Personal Usage Safe Snapshots Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** Complete. Implementation, review, self-test, Draft PR #148 delivery, and required CI verification are complete; the PR remains intentionally unmerged.

**Goal:** Make personal usage render from an actor-isolated, versioned, short-lived usage snapshot while quota/subscription facts and representative scope load independently and remain fresh-only.

**Architecture:** Add one `personalusage` module that owns current-user/provider resolution, the two-window Redis usage read model, freshness/error semantics, and fresh quota composition behind two small read methods. Evolve the Relay seam with one branch-selecting origin operation that logs in at most once and runs requested stats, trend, models, keys, and subscriptions concurrently under one deadline. Keep the combined dashboard response compatible, add usage-only and quota-only projections for the first-party Vue client, and make the browser independently cancel and render usage, quota, and representative-scope work.

**Tech Stack:** Go 1.23+, Gin, Ent/PostgreSQL, go-redis v9, miniredis, Vue 3 `<script setup lang="ts">`, Axios AbortSignal, TailwindCSS, Vitest, Vue Test Utils.

## Global Constraints

- Work from `docs/performance-contracts-116@7f2999a561454cb514c399839d38d3d691e590e5`, the merge commit for PR #139. Do not stack on an unmerged sibling branch.
- Preserve the default combined `GET /api/v1/user/usage/dashboard` response fields. Add `include_group_quotas=false` as an additive projection and add `GET /api/v1/user/usage/group-quotas` for fresh-only quota data.
- The first-party browser uses the usage-only projection and quota-only endpoint in parallel. Omission of `include_group_quotas` remains compatibility behavior and composes the same internal operations without an internal HTTP call.
- The Relay seam exposes one high-level `ReadUserUsageOrigin` operation with explicit `Usage` and `Quota` branches. A combined cold read authenticates once and fans out stats, trend, models, API keys, and subscriptions concurrently, with at most five branch calls and one 12-second origin deadline.
- Stats, trend, and models form one atomic generation. A refresh stores all three or none; it never mixes generations or caches quota/subscription rows.
- Usage fresh time is 30 seconds minus 10-20 percent deterministic jitter, producing 24-27 seconds. The hard stale deadline is no later than two minutes after generation and is shortened by the same 10-20 percent jitter. Redis TTL lasts through that stored stale deadline.
- Cache isolation includes deployment namespace, provider ID, persisted provider configuration version, local actor ID, Relay subject ID, current local binding version, normalized start/end date, granularity, and timezone. Hash the canonical dimensions; do not put raw query strings, email, passwords, tokens, credentials, or cached values in Redis keys or logs.
- A persisted `relay_providers.configuration_version` starts at 1 and increments in the same provider update statement as behavior-affecting mutations. The existing local provider-cache eviction remains mandatory after successful create/update/delete.
- Before `fresh_until`, return `cache_status=fresh`. A cold/refresh origin result is `miss`. After soft expiry, return `stale` only when an eligible origin refresh fails and the stored value is not past `stale_until`; stale responses use `source_status=error`.
- Invalid credentials, missing/invalid local configuration, invalid query values, authorization failure, and caller cancellation are never eligible for stale fallback. Redis read/write/lease/serialization failure bypasses the read model and uses one bounded authoritative refresh.
- Identical cold refreshes collapse inside one process and across replicas with a token-protected Redis lease. Lease acquisition double-checks the value, cancellation cannot deadlock waiters, and release uses compare-and-delete under an independent short context.
- Quota/subscription facts are always current-request facts. They use `cache_status=uncached`, return `ok`, `empty`, or `unavailable` section state, and are never copied into a stale usage value or Redis payload.
- Missing Relay credentials preserve HTTP 200 with `configured=false`, empty usage arrays, and no usage freshness. Invalid Relay credentials preserve HTTP 409. A transient origin failure without eligible stale usage preserves the current HTTP 502 behavior.
- Personal usage, quota, and representative scope start independently. Personal usage removes the page loading gate as soon as its usage-only response arrives; delayed quota/scope cannot block it. Range changes abort both superseded personal requests and only the latest generation can update the view.
- Stale usage has a visible, localized marker. Fresh and cold-miss usage has no noisy badge. Quota failure is rendered only in the quota section.
- Selected-member usage remains on the existing independently authorized team endpoint and does not read or populate the personal usage cache.
- Tests, fixtures, logs, docs, cache examples, credentials, groups, identities, and URLs use synthetic values only.
- Update `docs/architecture.md` and the active `2026-07-14` performance spec only after behavior lands. Do not rewrite the historical `2026-06-06`, `2026-06-16`, or OAuth specs.
- Maintain this file as a live ledger. Check a step only after its command/action actually succeeds, and keep `Status` consistent with remaining unchecked work.
- The draft PR targets `docs/performance-contracts-116`, links #123 and parent #115, depends on PR #138, and is not merged to `main`, tagged, released, deployed, or used for Helm work.

---

### Task 1: Relay Origin Read And Provider Version

**Files:**
- Modify: `backend/internal/relay/provider.go`
- Modify: `backend/internal/relay/types.go`
- Modify: `backend/internal/relay/sub2api.go`
- Modify: `backend/internal/relay/sub2api_test.go`
- Modify: `backend/ent/schema/relay_provider.go`
- Modify generated files under `backend/ent/`
- Modify: `backend/internal/handler/provider.go`
- Create: `backend/internal/handler/provider_configuration_version_test.go`

**Interfaces:**
- Produces `relay.UserUsageOriginBranches{Usage bool, Quota bool}`.
- Produces `relay.UserUsageOriginRequest{Login, Password string; RelayUserID int64; Params UserUsageDashboardParams; Branches UserUsageOriginBranches}`.
- Produces `relay.UserUsageOriginResult{Usage *UserUsageDashboardResponse; UsageErr error; APIKeys []APIKey; Subscriptions []UserSubscription; QuotaErr error}`.
- Produces optional interface `relay.UserUsageOriginReader` with `ReadUserUsageOrigin(context.Context, UserUsageOriginRequest) (*UserUsageOriginResult, error)`; top-level error is reserved for login/credential/request failure, while branch errors remain section-specific.
- Produces persisted `RelayProvider.ConfigurationVersion int64`, default 1, incremented by every successful admin provider update.

- [x] **Step 1: Add RED provider-version tests**

  Add a real handler test that creates a provider at version 1, updates base URL/model/key/enablement through `ProviderHandler.Update`, and asserts the saved row becomes version 2 in the same update. Assert a failed update leaves version 1.

- [x] **Step 2: Run the provider-version test and record RED**

  Run: `cd backend && go test ./internal/handler -run '^TestProviderConfigurationVersion' -count=1 -v`

  Expected: compile/assertion failure because `configuration_version` and atomic increment do not exist.

  RED evidence (2026-07-15): the exact command failed at compile time only because `ent.RelayProvider.ConfigurationVersion` did not exist; all six reported references were the intended missing contract.

- [x] **Step 3: Implement and generate the provider version**

  Add `field.Int64("configuration_version").Default(1).Positive()` to `RelayProvider`. Regenerate Ent. In `ProviderHandler.Update`, call `AddConfigurationVersion(1)` on the same update builder before `Save`; keep post-commit `invalidateCache()`. Do not expose secrets or use `updated_at` as the cache correctness version.

  GREEN evidence (2026-07-15): Ent generation completed, and the exact provider-version test passed both creation/update and failed-binding cases.

- [x] **Step 4: Add RED origin branch/concurrency tests**

  Add deterministic HTTP-server tests with five handlers that signal `started` then wait on a release channel. For a combined request, assert one login and all stats/trend/models/key/subscription handlers start before release. Add usage-only and quota-only cases proving unrequested branches are never called, one usage child error yields `UsageErr` with no partial usage, one quota child error yields `QuotaErr` without failing usage, and the 12-second request-scoped deadline/caller cancellation terminates all branches.

- [x] **Step 5: Run origin tests and record RED**

  Run: `cd backend && go test ./internal/relay -run '^TestReadUserUsageOrigin' -count=1 -v`

  Expected: compile failure because the branch-selecting origin interface is absent.

  RED evidence (2026-07-15): the exact command failed only on the missing `UserUsageOriginReader`, request/result, and branch types; the test code formatted successfully before execution.

- [x] **Step 6: Implement the branch-selecting Relay origin read**

  `ReadUserUsageOrigin` validates at least one branch, wraps the request in a 12-second context, logs in once when usage is requested, creates at most five fixed tasks, and collects them through a buffered result channel. It returns usage only after all stats/trend/models succeed, returns quota facts only after both key/subscription reads succeed, preserves `relay.ErrInvalidCredentials` through wrapping, and never starts a duplicate aggregate/component path.

- [x] **Step 7: Verify Task 1 GREEN and checkpoint**

  Run:

  ```bash
  cd backend
  gofmt -w internal/relay/provider.go internal/relay/types.go internal/relay/sub2api.go internal/relay/sub2api_test.go ent/schema/relay_provider.go internal/handler/provider.go internal/handler/provider_configuration_version_test.go
  go generate ./ent
  go test ./internal/relay ./internal/handler -run 'UserUsageOrigin|ProviderConfigurationVersion' -count=2
  git diff --check
  ```

  Commit: `perf(relay): parallelize personal usage origin reads`

  GREEN evidence (2026-07-15): the exact focused Task 1 command passed twice for `internal/relay` and `internal/handler`; a race-enabled origin/dashboard replay passed; full `internal/relay` and full `internal/handler` packages passed; Ent regenerated cleanly; `git diff --check` returned no findings.

---

### Task 2: Shared Cache Primitives And Personal Usage Module

**Files:**
- Create: `backend/internal/readcache/store.go`
- Create: `backend/internal/readcache/flight.go`
- Create: `backend/internal/readcache/store_test.go`
- Create: `backend/internal/readcache/flight_test.go`
- Modify: `backend/internal/workitems/cache.go`
- Modify: `backend/internal/workitems/cache_test.go`
- Create: `backend/internal/personalusage/types.go`
- Create: `backend/internal/personalusage/quota.go`
- Create: `backend/internal/personalusage/cache.go`
- Create: `backend/internal/personalusage/cache_test.go`
- Create: `backend/internal/personalusage/service.go`
- Create: `backend/internal/personalusage/service_test.go`

**Interfaces:**
- Produces `readcache.Store`, `readcache.RedisStore`, `readcache.ErrMiss`, `readcache.FlightGroup[T].Do`, and `readcache.Sleep`; workitems reuses these without changing its external behavior.
- Produces `personalusage.UsageFreshness{AsOf, FreshUntil, StaleUntil time.Time; CacheStatus, SourceStatus string}`.
- Produces `personalusage.QuotaFreshness{AsOf *time.Time; CacheStatus, SourceStatus string}`.
- Produces `personalusage.Snapshot` with existing usage fields plus optional `usage_freshness`, `group_quotas`, and `quota_freshness`.
- Produces `personalusage.GroupQuotaResponse{GroupQuotas, QuotaFreshness}`.
- Produces `personalusage.Request{UserID int; Params relay.UserUsageDashboardParams; IncludeGroupQuotas bool}`.
- Produces `personalusage.CacheKey{ProviderID int; ProviderVersion int64; ActorID int; RelayUserID int64; BindingVersion int64; Params relay.UserUsageDashboardParams}`.
- Produces `personalusage.OriginLoadResult{Usage *relay.UserUsageDashboardResponse; UsageErr error; Quota relay.UserUsageGroupQuotaState; QuotaFreshness QuotaFreshness; QuotaLoaded bool}` and `personalusage.OriginLoader func(context.Context, bool) (OriginLoadResult, error)`; the returned error carries non-stale-eligible credential/configuration/cancellation failures, while `UsageErr` carries an eligible source refresh failure.
- Produces `personalusage.CacheResult{Usage *relay.UserUsageDashboardResponse; UsageFreshness UsageFreshness; Quota relay.UserUsageGroupQuotaState; QuotaFreshness QuotaFreshness; QuotaLoaded bool}`.
- Produces `personalusage.CacheOptions{Namespace string; CommandTimeout, RefreshTimeout, LeaseTTL, PollInterval, ReleaseTimeout time.Duration; Now func() time.Time; RandFloat64 func() float64; NewToken func() string; Sleep func(context.Context, time.Duration) error}` and `personalusage.NewCache(readcache.Store, CacheOptions) (*Cache, error)`.
- Produces `personalusage.Service.Dashboard(ctx, Request) (*Snapshot, error)` and `Service.GroupQuotas(ctx, Request) (*GroupQuotaResponse, error)`.
- Produces `personalusage.NewService(*ent.Client, ProviderResolver, string, *Cache) *Service`, where `ProviderResolver.Resolve(context.Context, int) (relay.Provider, error)` reuses the existing handler/provider seam.
- Produces `personalusage.Cache.GetOrLoad(ctx context.Context, key CacheKey, includeQuota bool, loader OriginLoader) (*CacheResult, error)`; only atomic usage payload is serialized.

- [x] **Step 1: Add RED shared primitive tests**

  Move the production Redis adapter and waiter-counted generic flight expectations to `readcache` tests: Redis miss mapping, TTL, token compare-delete, one cancelled waiter while another succeeds, last-waiter cancellation, and no goroutine left after completion. Add compatibility assertions in workitems tests so extraction cannot change #119 key/TTL/fallback behavior.

- [x] **Step 2: Run shared primitive tests and record RED**

  Run: `cd backend && go test ./internal/readcache ./internal/workitems -run 'RedisStore|Flight|CountsCache' -count=1`

  Expected: package/compile failures because `readcache` does not exist.

  RED evidence (2026-07-15): `internal/workitems` remained GREEN while `internal/readcache` failed only on the intended missing `FlightGroup`, `NewRedisStore`, and `ErrMiss` contracts.

- [x] **Step 3: Extract the narrow shared primitives**

  Move only the Redis command adapter, `ErrMiss`, context sleep, and generic waiter-counted flight into `readcache`. Keep workitems revision, key, JSON validation, lease policy, and no-stale semantics inside `workitems`; use aliases/wrappers where necessary so existing call sites remain stable.

  GREEN evidence (2026-07-15): the new Redis/flight contract tests passed, and every focused #119 `CountsCache` test remained GREEN after extraction, including 50-call local collapse, two-instance lease collapse, cancellation, revision isolation, Redis errors, deterministic TTL, and Lua release.

- [x] **Step 4: Add RED personal cache/service tests**

  With injected clock/random/token/sleep and miniredis, cover cold miss, warm hit, both jitter endpoints, soft refresh, eligible stale fallback, hard expiry, invalid-credential rejection, caller-cancellation rejection, malformed/schema-mismatch refresh, Redis outage fallback, set/release failure tolerance, actor/provider-version/Relay-subject/binding/range/granularity/timezone isolation, 50-call local collapse, two-cache distributed collapse, second read after lease acquisition, holder cancellation, and no quota fields/values in serialized Redis bytes. Service tests cover configured=false, one primary provider resolution, decryption/configuration errors, combined cold read using one origin call, warm usage plus a fresh quota-only origin call, usage-only projection, quota-only response, `ok/empty/unavailable` quota states, and current daily/weekly/monthly presentation rules.

- [x] **Step 5: Run personal usage module tests and record RED**

  Run: `cd backend && go test ./internal/personalusage -count=1 -v`

  Expected: package/compile failure because the module does not exist.

  RED evidence (2026-07-15): the exact command failed only on the intended missing personalusage cache/options/key/load-result/service contracts; the new tests formatted successfully first.

- [x] **Step 6: Implement the personal usage read model and module**

  Build a SHA-256 key from a canonical JSON dimensions struct and format it as `ae:<namespace>:personal-usage:v1:<hex>`. Validate cached JSON with `DisallowUnknownFields`, schema version, non-nil stats, and non-nil trend/models. Use 100ms Redis commands, a 12-second shared refresh budget, a 15-second lease, 25ms polling, 100ms release, and test-injected time/randomness. Query the current user and enabled primary provider on every request, include `user.updated_at` as the binding version, decrypt only inside the module, and call `ReadUserUsageOrigin` with the minimum branches selected after the cache read.

  GREEN evidence (2026-07-15): the module now stores only atomic usage generations under hashed actor/provider/binding/range dimensions, bounds fresh/stale windows to 24-27 and 96-108 seconds, treats quota as uncached request data, and preserves configuration, credential, and cancellation errors without stale fallback.

- [x] **Step 7: Verify Task 2 GREEN and checkpoint**

  Run:

  ```bash
  cd backend
  gofmt -w internal/readcache/*.go internal/workitems/cache.go internal/personalusage/*.go
  go test ./internal/readcache ./internal/workitems ./internal/personalusage -count=2
  git diff --check
  ```

  Commit: `perf(backend): cache personal usage snapshots safely`

  GREEN evidence (2026-07-15): the exact focused command passed twice for `internal/readcache`, `internal/workitems`, and `internal/personalusage`; race-enabled tests for all three packages passed; malformed/schema values, Redis failures, set/release errors, both jitter endpoints, 50-call local collapse, two-instance lease collapse, lease-holder cancellation, quota-only service reads, and daily/weekly/monthly quota presentation all passed; `git diff --check` returned no findings.

---

### Task 3: HTTP Contracts And Runtime Wiring

**Files:**
- Modify: `backend/internal/handler/user_usage.go`
- Modify: `backend/internal/handler/user_usage_test.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/handler/router_test.go` or the nearest existing route-registration test
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/cmd/server/redis_runtime_test.go`

**Interfaces:**
- Consumes Task 2's concrete `personalusage.Service` and `personalusage.Cache`.
- Produces `handler.NewUserUsageHandler(*personalusage.Service) *UserUsageHandler`; the handler no longer owns Ent, encryption, provider, quota, or cache policy.
- Produces `GET /api/v1/user/usage/dashboard?include_group_quotas=false`.
- Produces `GET /api/v1/user/usage/group-quotas`.
- Extends `handler.RouterRuntimeOptions` with `PersonalUsageCache *personalusage.Cache`.

- [x] **Step 1: Add RED HTTP contract tests**

  Assert the default dashboard includes existing usage/group quota fields plus both freshness objects; `include_group_quotas=false` omits `group_quotas` and `quota_freshness`; the quota route returns only `group_quotas`/`quota_freshness`; invalid boolean input returns 400; missing credentials returns configured=false without freshness; invalid credentials returns 409 even with stale storage; transient usage failure uses eligible stale with 200; quota failure returns 200 unavailable; and registered routes remain auth-protected.

- [x] **Step 2: Run handler tests and record RED**

  Run: `cd backend && go test ./internal/handler -run '^TestUserUsage' -count=1`

  Expected: failures because the new projection, endpoint, freshness, and module wiring are absent.

  RED evidence (2026-07-15): the exact focused command failed only because `NewUserUsageHandler` still required the legacy Ent/resolver/encryption constructor and `UserUsageHandler.GroupQuotas` did not exist; the new HTTP contract tests formatted successfully before execution.

- [x] **Step 3: Replace handler composition with the module**

  Keep only auth-context lookup, query parsing, `include_group_quotas` parsing, module calls, and error-to-status mapping in `user_usage.go`. Move quota merge/window helpers and provider/user/credential/cache decisions to `personalusage`. Register both routes and inject one cache built from the existing Redis client/namespace in `main.go`; Redis is still optional for data-plane reads and readiness behavior is unchanged.

  GREEN evidence (2026-07-15): `user_usage.go` now contains only HTTP concerns, the Router constructs one `personalusage.Service`, production creates one personal usage cache from the existing bounded Redis client/store, and the authenticated dashboard plus quota-only routes are both registered.

- [x] **Step 4: Verify Task 3 GREEN and checkpoint**

  Run:

  ```bash
  cd backend
  gofmt -w internal/handler/user_usage.go internal/handler/user_usage_test.go internal/handler/router.go cmd/server/main.go cmd/server/redis_runtime_test.go
  go test ./internal/personalusage ./internal/handler ./cmd/server -count=2
  git diff --check
  ```

  Commit: `feat(backend): split personal usage and quota reads`

  GREEN evidence (2026-07-15): the exact Task 3 command passed twice for `internal/personalusage`, `internal/handler`, and `cmd/server`; focused HTTP tests passed combined compatibility, usage-only/quota-only projections, invalid boolean input, unconfigured state, invalid-credential no-stale behavior, stale-if-error, section-local quota failure, cold 502 behavior, and authenticated route registration; `git diff --check` returned no findings.

---

### Task 4: Independent Frontend Loading And Cancellation

**Files:**
- Modify: `frontend/src/api/userUsage.ts`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/views/DashboardView.vue`
- Modify: `frontend/src/components/user/usage/UserUsageDashboard.vue`
- Modify: `frontend/src/__tests__/user-usage-api.test.ts`
- Modify: `frontend/src/__tests__/dashboard-view.test.ts`
- Modify: `frontend/src/i18n.ts`

**Interfaces:**
- Produces `getUserUsageDashboard(params, signal)` that always sends `include_group_quotas=false` for first-party personal usage.
- Produces `getUserUsageGroupQuotas(params, signal)` for `/user/usage/group-quotas`.
- Produces TypeScript `UserUsageFreshness` and `UserQuotaFreshness` matching backend JSON.
- Preserves member-route use of `getTeamUsageSubjectDashboard` without personal caching or quota split.

- [x] **Step 1: Add RED API and route-lifecycle tests**

  Assert API functions pass both projection params and AbortSignal. Mount `/usage` with delayed scope and quota promises, resolve usage first, and assert stats render while scope/quota remain pending. Reject quota and assert usage stays visible with only quota unavailable. Return stale usage and assert a localized stale marker. Trigger two range changes, assert both first-request signals are aborted, resolve responses out of order, and assert only the newest range/data renders. Assert the member route makes no personal quota request.

- [x] **Step 2: Run focused frontend tests and record RED**

  Run: `cd frontend && npm test -- src/__tests__/user-usage-api.test.ts src/__tests__/dashboard-view.test.ts`

  Expected: failures because personal usage, quota, and scope still share lifecycle and API calls have no abort contract.

  RED evidence (2026-07-15): the exact focused command reported the missing quota API export and missing signal/projection options, while lifecycle tests remained behind the `DashboardView` page-level loading gate; stale marker and abort-generation assertions failed for the intended absent behavior.

- [x] **Step 3: Implement independent lifecycles**

  Always mount `UserUsageDashboard` immediately on personal routes. Start representative scope without controlling the usage loading gate. Inside the dashboard, create separate `usageLoading`, `quotaLoading`, `usageError`, and generation state; start usage/quota promises together; use two AbortControllers per generation; abort them before each range/refresh; apply results only when the generation remains current. Keep the previous usage visible during refresh, make quota failure section-local, render stale copy only for `cache_status=stale`, and load chart components asynchronously only after usable usage data exists.

  GREEN evidence (2026-07-15): the personal component now owns independent usage/quota requests and state, clears fresh-only quota on each generation, keeps prior usage while refreshing, aborts both superseded personal requests, ignores older generations, and lazy-loads chart modules only after a configured snapshot with stats exists; member routes still call only the selected-subject endpoint.

- [x] **Step 4: Verify Task 4 GREEN and checkpoint**

  Run:

  ```bash
  cd frontend
  npm test -- src/__tests__/user-usage-api.test.ts src/__tests__/dashboard-view.test.ts
  npm test
  npm run build
  ```

  Expected: focused tests pass; full suite reports at least the baseline 39 files/451 tests plus new cases; production build succeeds.

  Commit: `perf(frontend): render personal usage independently`

  GREEN evidence (2026-07-15): the exact focused suite passed 36 tests; the full frontend suite passed 39 files and 457 tests; `vue-tsc -b` and Vite production build succeeded; chart code emitted as separate 2.15 kB and 2.69 kB chunks; `git diff --check` returned no findings.

---

### Task 5: Documentation, Verification, Review, And PR

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md`
- Maintain: `docs/superpowers/plans/2026-07-15-personal-usage-snapshots.md`
- Review: every file changed since `7f2999a561454cb514c399839d38d3d691e590e5`

**Interfaces:**
- Consumes Tasks 1-4.
- Produces current architecture truth, complete repository evidence, review-clean commits, and a draft PR for #123.

- [x] **Step 1: Update current documentation only**

  Add the active performance spec to architecture source-of-truth order. Document the `personalusage` module, provider configuration version used by the cache key, usage-only/quota-only browser requests, usage-only Redis payload, 24-27 second fresh window, no-more-than-two-minute stale deadline, fresh-only quota, one-login concurrent Relay origin read, optional Redis fallback, and independent usage/quota/scope frontend lifecycles. In the active performance spec, mark only the #123 personal usage clauses as landed and record the exact implementation choices; do not rewrite historical specs.

  Documentation evidence (2026-07-15): `docs/architecture.md` now records the implemented module, API/origin/cache/browser boundaries and includes the active performance spec in source-of-truth order; the active spec has a #123-only implementation-status section and continues to mark unrelated rollout slices as pending. Historical specs were not modified.

- [x] **Step 2: Run full generation and repository verification**

  Run:

  ```bash
  cd backend
  gofmt -w internal/readcache/*.go internal/personalusage/*.go internal/relay/*.go internal/handler/user_usage*.go internal/handler/provider*.go internal/handler/router.go cmd/server/*.go ent/schema/relay_provider.go
  go generate ./ent
  git diff --exit-code -- ent
  go test ./...
  cd ../ae-cli
  go test ./...
  cd ../frontend
  npm test
  npm run build
  cd ..
  bash deploy/test/release-frontend-embed-test.sh
  git diff --check
  ```

  Verification evidence (2026-07-15): the complete command was rerun after review fixes and returned zero. Ent generation produced no diff; `go test ./...` passed for backend and ae-cli; frontend passed 39 files and 457 tests; TypeScript/Vite production build and `deploy/test/release-frontend-embed-test.sh` passed; final `git diff --check` returned no findings. The existing npm audit output still reports 9 findings, and the embed fixture emitted one non-fatal temporary hook-queue warning.

- [x] **Step 3: Run environment-sensitive role E2E separately**

  Start this worktree's Vite server on an owned IPv6 loopback port with a trap, run `npm run test:e2e:role`, and clean the listener. Record proxy/backend warnings separately from unit results.

  E2E evidence (2026-07-15): this worktree's Vite server ran on `[::1]:5173`, `npm run test:e2e:role` passed 16/16 role assertions, the trap removed the IPv6 listener, and the unrelated pre-existing IPv4 listener remained untouched. Because no local backend was started, Vite logged expected `ECONNREFUSED` proxy warnings for work-items, personal usage/quota, and scope requests; the role suite mocks its asserted auth/settings endpoints and still completed with zero failures.

- [x] **Step 4: Review exact spec and standards coverage**

  Inspect the complete base-to-HEAD diff against issue #123, the active performance spec, `AGENTS.md`, and current code. Resolve all Critical/Important findings. Explicitly verify: one login; branch concurrency/deadline; usage atomicity; cache dimensions/windows; Redis outage and lease cancellation; no quota in Redis; invalid credentials never stale; combined compatibility; usage/quota/scope independence; abort of superseded ranges; member-route isolation; provider version increment; docs truth.

  Review evidence (2026-07-15): exact base-to-worktree review covered all 44 changed files and every listed contract. Two Important findings were fixed with RED/GREEN tests: configured snapshots now preserve empty `trend`/`models` as JSON arrays rather than `null`, and deployment namespace now participates in the canonical cache digest as well as the visible prefix. New service errors wrap their underlying causes with `%w`. Targeted `go vet`, cache/service/handler regression tests, `git diff --check`, and changed-content synthetic-data scans passed. No Critical or Important findings remain.

- [x] **Step 5: Commit final docs/ledger and prepare PR body**

  Commit: `docs(architecture): document personal usage snapshots`

  Create ignored `.superpowers/sdd/pr-123.md` with `Closes #123`, parent #115, dependency on PR #138, the #119 merge-base OID, API/cache/Relay/frontend behavior, exact tests, review findings, rollout risk, Redis fallback, and rollback notes.

  Delivery evidence (2026-07-15): current architecture, the active #123 implementation status, and this ledger were committed as `4802cef`; ignored `.superpowers/sdd/pr-123.md` contains the required issue links, merge base, behavior, verification, review, rollout/fallback, and rollback sections.

- [x] **Step 6: Push and open the draft PR**

  ```bash
  git push -u origin perf/personal-usage-123
  gh pr create --draft --base docs/performance-contracts-116 --head perf/personal-usage-123 --title "perf(usage): serve personal usage from safe snapshots" --body-file .superpowers/sdd/pr-123.md
  gh pr view --json number,state,isDraft,baseRefName,headRefName,headRefOid,mergeable,mergeStateStatus,url
  gh pr checks --watch
  ```

  Expected: OPEN draft PR against `docs/performance-contracts-116`, all four CI jobs green, clean worktree, and no merge/tag/release/deploy/Helm action.

  Delivery evidence (2026-07-15): Draft PR #148 is OPEN against `docs/performance-contracts-116` from `perf/personal-usage-123`; the initial delivery OID was `cb70298bab2a98601d179a0d412a8e29e3f5a174`. Required checks passed: `ae-cli` (24s), `backend` (3m23s), `deploy-static` (14s), and `frontend` (55s). The branch and isolated worktree remain available for review; no merge, tag, release, deployment, Helm action, or `sub2api` source change was performed.

## Self-Review Record

- Spec coverage: Tasks 1-4 map every #123 acceptance criterion to a production seam and a RED/GREEN test.
- Cache correctness: Task 2 stores usage only, keys every answer-changing dimension, bounds both freshness windows, and exercises local/distributed collapse plus Redis outage.
- Fresh quota correctness: Tasks 1-3 keep quota as current-request origin facts with independent error metadata and no Redis serialization.
- Frontend readiness: Task 4 removes both optional scope and quota from the personal usage loading gate and cancels superseded range work.
- Compatibility: Task 3 preserves the default combined endpoint while adding only an optional projection and one additive route.
- Documentation: Task 5 updates current architecture and the active target spec without rewriting historical contracts.
