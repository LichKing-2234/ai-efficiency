# Settings And Provider Metadata Performance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver issue #130 with an on-demand Settings shell, deduplicated shared Settings resources, version-bounded Relay clients, and a five-minute shared Relay group/model metadata read model.

**Architecture:** `backend/internal/relayruntime.Manager` becomes the deep module behind the existing `relay.Provider` seam: it resolves current persisted provider versions, owns `(provider_id, configuration_version)` process clients, consumes and publishes secret-free invalidations, and serves non-stale shared group/model metadata through Redis with authoritative fallback. The frontend turns `SettingsView` into a section router that imports only the active section, while `useSettingsResourcesStore` deduplicates credential summaries and Directory Sync sources across section remounts and refreshes them after owned mutations.

**Tech Stack:** Go, Gin, Ent, Redis, go-redis Pub/Sub, shared `readcache`, Vue 3, Pinia, TypeScript, Vitest, Vite.

## Global Constraints

- Work only in `/Users/admin/ai-efficiency/.worktrees/perf-settings-metadata-130` on `perf/settings-metadata-130`.
- Base is issue #128 final head `0b694ba`; target Draft PR #156 when publishing.
- Keep the persisted `relay_providers.configuration_version`, initialized at 1, and increment it in the same successful provider update statement as every behavior-affecting mutation.
- Process clients are keyed by provider ID plus configuration version, are revalidated against the current database row on `Resolve`, and live no longer than five minutes.
- Provider mutations evict the local provider immediately and publish best-effort cross-replica invalidation containing only schema version, provider ID, and configuration version. Missed notifications recover through persisted version revalidation and maximum client lifetime.
- Relay group/model metadata is fresh-only with a five-minute maximum and 10-20 percent downward TTL jitter. Keys bind deployment namespace, provider ID, persisted provider version, collection kind, platform, and group where applicable. Values contain group/model display metadata only.
- Redis read/write/lease/Pub/Sub failure must not fail user/provider reads or successful provider mutations. It bypasses shared caching/invalidation while authoritative database and Relay reads continue.
- Current local user state, Relay membership/subscriptions, API keys, quota entitlement, and authorization are checked on every relevant request and are never inferred from shared metadata.
- Passwords, JWTs, admin/user API keys, credential payloads, raw user identity, cache payloads, and provider secrets never enter Redis keys, values, Pub/Sub messages, metrics, or logs.
- `SettingsView` imports and mounts one section implementation at a time. The default route loads only AI Services and its Relay-provider request; a direct section query loads only that section and its owned requests.
- Credential summaries and Directory Sync source summaries have one Pinia owner, one in-flight request per resource, a five-minute frontend freshness window, and force-refresh after owned create/update/delete operations.
- Preserve current URL section links, keyboard tab behavior, dialogs, validation, localized copy, CRUD payloads, Directory run polling, compatibility routes, and role enforcement.
- Tests and examples use synthetic identities, domains, groups, keys, and credentials only.
- Do not merge, release, tag, deploy, run Helm, or modify `sub2api`.
- Update every checkbox immediately after the action is completed.

**Status:** Implementation complete and integrated into `feat/platform-loading-performance` through PR #157 at `37e43258`; the exact integration head `d2bc2694` also contains the later review remediations through #172. Stacked-Draft-PR details below are historical delivery evidence. This work is not merged to `main` or production-verified; #136 remains open for that external evidence.

---

### Task 1: Version And Invalidate Process-Local Relay Clients

**Files:**
- Create: `backend/internal/relayruntime/manager.go`
- Create: `backend/internal/relayruntime/manager_test.go`
- Create: `backend/internal/relayruntime/invalidation.go`
- Create: `backend/internal/relayruntime/invalidation_test.go`
- Modify: `backend/internal/handler/provider.go`
- Modify: `backend/internal/handler/provider_configuration_version_test.go`
- Modify: `backend/cmd/server/main.go`

**Interfaces:**
- Produces `relayruntime.Manager.Resolve(context.Context, int)`, `ResolveEntity(*ent.RelayProvider)`, `Invalidate(context.Context, int, int64)`, `Start(context.Context)`, and Redis/fake invalidation adapters.
- `handler.ProviderHandler` keeps its existing `Resolve` interface and delegates client lifetime/version/invalidation behavior to the manager.
- Invalidation payload is exactly `schema_version`, `provider_id`, and `configuration_version`.

- [x] **Step 1: Add RED manager and mutation tests**

  Cover same-version reuse, version-separated clients, five-minute maximum lifetime, current-row revalidation after a missed notification, remote eviction after Pub/Sub delivery, malformed notification ignore, secret-free payload serialization, local eviction before publish, failed publish as non-fatal, and create/update/delete publication only after successful database mutation.

  Test evidence (2026-07-16): manager tests define decrypted client construction, same-version reuse, TTL rebuild, database-version recovery without a notification, local/remote eviction, and failed-publish behavior; handler tests define post-commit version publication and failed-mutation silence; codec tests require an exact secret-free payload and strict decoding.

- [x] **Step 2: Run focused tests and record RED**

  Run:

  ```bash
  cd backend
  go test ./internal/relayruntime ./internal/handler -run 'RelayRuntime|ProviderConfigurationVersion|ProviderInvalidation' -count=1 -v
  ```

  Expected: compile failures for the absent runtime manager, invalidation adapter, injected handler runtime, and mutation notifications.

  RED evidence (2026-07-16): the focused command failed only because `InvalidationEvent`, its strict codec, `Manager`, `Options`, and the injected handler runtime do not exist.

- [x] **Step 3: Implement versioned clients and best-effort invalidation**

  Build providers only from decrypted current Ent rows, double-check cache insertion under concurrency, remove other versions for the same provider, and rebuild after five minutes. Start one cancellable Redis subscription in server startup. On provider create/update/delete, commit first, evict local state, then publish; log only provider ID/version and the error on best-effort failure.

  Implementation evidence (2026-07-16): `relayruntime.Manager` now revalidates the persisted row on every `Resolve`, caches only one provider/version for at most five minutes, rejects stale in-flight rows after a newer event, and uses an exact secret-free Pub/Sub payload. Server startup owns the cancellable subscription; create/update/delete evict locally after commit and publish without rolling back a successful mutation on Redis failure.

- [x] **Step 4: Verify Task 1 GREEN and checkpoint**

  Run:

  ```bash
  cd backend
  gofmt -w internal/relayruntime/*.go internal/handler/provider*.go cmd/server/main.go
  go test ./internal/relayruntime ./internal/handler ./cmd/server -count=2
  go test -race ./internal/relayruntime ./internal/handler -run 'RelayRuntime|ProviderConfigurationVersion|ProviderInvalidation' -count=1
  git diff --check
  ```

  Commit: `perf(backend): version relay provider runtime`

  GREEN evidence (2026-07-16): focused RED/GREEN tests, double `internal/relayruntime`/`internal/handler`/`cmd/server` runs, race-enabled runtime/handler invalidation tests, a final uncached runtime test, and `git diff --check` passed.

### Task 2: Cache Shared Relay Group And Model Metadata Safely

**Files:**
- Create: `backend/internal/relayruntime/metadata.go`
- Create: `backend/internal/relayruntime/metadata_test.go`
- Modify: `backend/internal/relay/provider.go`
- Modify: `backend/internal/usersetup/service.go`
- Modify: `backend/internal/usersetup/service_test.go`
- Modify: `backend/internal/handler/provider.go`
- Modify: `backend/internal/handler/handler_test.go`
- Modify: `backend/cmd/server/main.go`

**Interfaces:**
- Adds optional `relay.PlatformGroupLister.ListPlatformGroups(context.Context)` without widening the required `relay.Provider` interface.
- `relayruntime.Manager.ListAllowedGroupsForUser` always reads the current Relay user/subscriptions, then joins allowed IDs against cached shared group metadata.
- `relayruntime.Manager.Models` caches cloned `[]relay.ModelOption` by provider/version/platform/group through a loader that is invoked only on a miss or Redis fallback.
- `usersetup.Service` discovers the richer group resolver through its existing provider resolver; compatibility fake providers continue through `relay.Provider.ListAllowedGroupsForUser`.

- [x] **Step 1: Add RED metadata and authorization tests**

  Cover cross-manager group/model hits, jittered expiry below the five-minute maximum, provider-version/platform/group isolation, local and distributed refresh collapse, malformed value recovery, Redis outage authoritative fallback, clone-on-read/write, no secret fields in keys/JSON, and changed group metadata under a new provider version. Prove each warm group request still calls current `GetUser`, each warm model request still checks current membership and active group API keys, and revoked entitlement returns no cached model list.

  Test evidence (2026-07-16): runtime tests define cross-manager group hits with fresh user reads, sanitized group values, model dimension/TTL isolation, clone behavior, concurrent collapse, and Redis fallback; usersetup tests require its versioned resolver seam; handler tests require fresh membership and active-key checks before a warm model result.

- [x] **Step 2: Run focused tests and record RED**

  Run:

  ```bash
  cd backend
  go test ./internal/relayruntime ./internal/usersetup ./internal/handler -run 'ProviderMetadata|AllowedGroups|ProviderModels' -count=1 -v
  ```

  Expected: compile/test failures for the absent metadata cache, group resolver, membership guard, and model loader.

  RED evidence (2026-07-16): the focused command failed only because `MetadataTTL`, `Manager.ListAllowedGroupsForUser`, `Manager.Models`, and the handler metadata wiring do not exist; the existing uncached usersetup fallback remained green.

- [x] **Step 3: Implement fresh-only metadata read model**

  Use the shared `readcache.Store` and flight/lease patterns. Read failures, lease failures, write failures, and invalid JSON fall back to bounded authoritative Relay work; no stale metadata is served. Keep user/subscription/key selection outside cached values, reject a requested model group that is absent from the current allowed-group set, and return cloned rows.

  Implementation evidence (2026-07-16): `relayruntime.Manager` now serves versioned, sanitized group/model metadata through process-local flights and a Redis lease, including the no-Redis fallback path; caps metadata TTL at five minutes with key-derived 10-20 percent downward jitter; rejects stale provider rows; and falls back to authoritative Relay reads for malformed values and Redis read/lease/write failures. `usersetup` discovers the versioned group resolver, while provider model/test handlers recheck current membership and active group keys before using cached model display metadata.

- [x] **Step 4: Verify Task 2 GREEN and checkpoint**

  Run:

  ```bash
  cd backend
  gofmt -w internal/relayruntime/*.go internal/relay/provider.go internal/usersetup/*.go internal/handler/provider*.go cmd/server/main.go
  go test ./internal/relayruntime ./internal/relay ./internal/usersetup ./internal/handler ./cmd/server -count=2
  go test -race ./internal/readcache ./internal/relayruntime ./internal/usersetup -count=1
  git diff --check
  ```

  Commit: `perf(backend): cache relay provider metadata`

  GREEN evidence (2026-07-16): the focused metadata/usersetup/handler suite, the full target package set twice, race-enabled `readcache`/`relayruntime`/`usersetup`, and `git diff --check` passed. Follow-up RED/GREEN tests cap and jitter metadata below five minutes, reject stale provider rows, preserve process-local collapse without Redis, prove cross-manager lease collapse, and fall back to Relay when a foreign lease outlives the refresh wait budget. Independent review found no Critical issues; all Important findings were fixed before checkpointing.

### Task 3: Load One Settings Section And Shared Resource At A Time

**Files:**
- Create: `frontend/src/stores/settingsResources.ts`
- Create: `frontend/src/stores/sessionResources.ts`
- Create: `frontend/src/__tests__/settings-resources-store.test.ts`
- Modify: `frontend/src/stores/auth.ts`
- Modify: `frontend/src/views/SettingsView.vue`
- Modify: `frontend/src/components/settings/AIServiceSettings.vue`
- Modify: `frontend/src/components/settings/CodePlatformSettings.vue`
- Modify: `frontend/src/components/settings/AdvancedCredentialSettings.vue`
- Modify: `frontend/src/components/settings/DeploymentRuntimeSettings.vue`
- Modify: `frontend/src/components/settings/OrganizationLoginSettings.vue`
- Modify: `frontend/src/components/settings/DirectorySyncSettings.vue`
- Modify: `frontend/src/components/settings/QuotaResetApprovalSettings.vue`
- Modify: `frontend/src/__tests__/auth-store.test.ts`
- Modify: `frontend/src/__tests__/settings-view.test.ts`
- Modify: `frontend/src/__tests__/directory-sync-settings.test.ts`
- Modify: `frontend/src/__tests__/quota-reset-approval-settings.test.ts`

**Interfaces:**
- `SettingsView` owns only section navigation and `defineAsyncComponent` loaders; each section owns its requests, CRUD state, dialogs, and localized feedback.
- `useSettingsResourcesStore` exposes credential and Directory-source refs plus `loadCredentials({ force? })`, `loadDirectorySources({ force? })`, `replaceDirectorySources`, and invalidation/refresh actions with one in-flight promise per resource.
- Code Platform loads credential summaries only when its add/edit task opens. Organization/Login and Advanced Credentials reuse the same store result; Directory Sync source remounts reuse the same store result.

- [x] **Step 1: Add RED route, section, store, and mutation tests**

  Assert default `/settings` requests only Relay providers; every direct `?section=` link imports/renders only its requested section and issues only owned requests; hidden sections make zero requests; switching sections preserves the URL contract. Cover credential/source concurrent deduplication, five-minute reuse/expiry, error retry, force refresh, mutation refresh, Code Platform dialog-time credential loading, Organization/Advanced credential reuse, Directory source reuse across remount, and unchanged CRUD/dialog/keyboard behavior.

  Test evidence (2026-07-16): new store tests define concurrent promise reuse, exact five-minute freshness, retry, force refresh, and clone boundaries. Settings route tests define default/direct request ownership, dialog-time Code Platform credentials, Advanced-to-Organization credential reuse, and Directory source remount reuse while retaining the existing URL, localization, CRUD, dialog, and loading regressions.

- [x] **Step 2: Run focused tests and record RED**

  Run:

  ```bash
  cd frontend
  npm test -- src/__tests__/settings-resources-store.test.ts src/__tests__/settings-view.test.ts src/__tests__/directory-sync-settings.test.ts
  ```

  Expected: failures because the parent eagerly imports every section and requests every dataset, while credentials/sources have no shared freshness owner.

  RED evidence (2026-07-16): the focused command failed because `settingsResources` does not exist, default/direct Settings still request hidden SCM/credential/system/LDAP data, Code Platform loads credentials before its dialog, and Organization issues two Directory source requests. The 43 pre-existing focused regressions remained green.

- [x] **Step 3: Implement async sections and the shared store**

  Move existing section-specific state, API calls, dialogs, and handlers into the five section modules without changing visible behavior or payloads. Replace static imports with an exhaustive typed async-loader map. Keep active section URL and tab focus logic in the shell. Deduplicate shared resources through Pinia, clone API arrays at the store interface, and force-refresh after successful owned mutations.

  Implementation evidence (2026-07-16): `SettingsView` is now a navigation-only shell with one exhaustive async component map and one active panel. Each section owns its prior request/CRUD/dialog state. `useSettingsResourcesStore` owns cloned credential and Directory-source arrays, independent five-minute freshness and errors, one active request plus one serialized forced follow-up per resource, invalidation/reset actions, and mutation refresh. Code Platform loads credentials only when opening add/edit; Advanced and Organization reuse credentials; Directory Sync and Quota Reset share one Directory-source request without keeping hidden polling components alive.

- [x] **Step 4: Verify Task 3 GREEN and checkpoint**

  Run:

  ```bash
  cd frontend
  npm test
  npm run build
  git diff --check
  ```

  Inspect the Vite manifest/output and record that `SettingsView` is a small shell and the five Settings sections are separate lazy chunks.

  Commit: `perf(frontend): load settings sections on demand`

  GREEN evidence (2026-07-16): focused Settings/store/Directory/Quota regressions initially passed 58/58; after review fixes the full frontend suite passed 40 files and 500 tests; `vue-tsc` and the Vite production build passed; and `git diff --check` passed. The production output now emits `SettingsView` as a 4.17 kB shell (1.73 kB gzip) plus separate AI Services (11.81 kB), Code Platforms (13.61 kB), Organization/Login (40.31 kB), Deployment/Runtime (3.77 kB), and Advanced Credentials (10.55 kB) chunks; `settingsResources` remains a separate 2.13 kB chunk.

### Task 4: Document, Verify, Review, And Publish

**Files:**
- Modify: `docs/architecture.md`
- Modify: this plan

- [x] **Step 1: Update current architecture**

  Record active-section Settings code/data ownership, shared frontend freshness/deduplication, persisted provider version usage, five-minute/versioned process clients, secret-free invalidation, group/model cache dimensions and fallback, and fresh membership/key checks. Do not rewrite historical specs.

  Documentation evidence (2026-07-16): `docs/architecture.md` now records the navigation-only Settings shell and section-owned requests, five-minute shared Settings resources, `relayruntime` module boundary, persisted-version process clients, secret-free invalidation, sanitized fresh-only metadata dimensions/fallback, and current membership/active-key checks. Historical specs were not rewritten.

- [x] **Step 2: Run full verification**

  ```bash
  git diff --check
  cd backend && go vet ./internal/relayruntime ./internal/relay ./internal/usersetup ./internal/handler ./cmd/server && go test ./...
  cd ../frontend && npm test && npm run build
  cd ../ae-cli && go test ./...
  ```

  Verification evidence (2026-07-16): repository `git diff --check`; targeted backend `go vet`; backend `go test ./...`; frontend 40-file/500-test Vitest suite and production `vue-tsc`/Vite build; and `ae-cli go test ./...` all exited successfully after the final review fixes. The final production build retained the 4.17 kB Settings shell, independent Settings resource store, and five separate section chunks.

- [x] **Step 3: Review against issue #130 and the active performance spec**

  Audit default/direct Settings requests and chunks, shared resource ownership/freshness/mutations, provider version transactionality, client version/TTL, publish/subscribe and missed-notification recovery, cache key/value privacy, TTL/isolation/collapse, Redis failure, fresh membership/key/quota authorization, compatibility, and synthetic data. Fix every Critical/Important finding and rerun affected verification.

  Review evidence (2026-07-16): independent spec and standards reviews found no Critical issues. Important findings were fixed with RED/GREEN coverage for metadata TTL jitter, stale provider rows, abandoned foreign leases, invalid prototype-key section queries, auth-session Settings reset without eager bundling, and repeated mutations that arrive during an existing forced refresh. Both review axes confirmed no remaining Critical/Important findings against issue #130, the active performance spec, and repository standards.

- [x] **Step 4: Push and open a Draft PR**

  Target `perf/team-organization-128`, list Draft PR #156 as the direct dependency, preserve both worktrees, and do not merge or release.

  Publication evidence (2026-07-16): pushed `perf/settings-metadata-130` at implementation head `5c6fbd92451303a437d4c58dcb793dedec452280` and opened Draft PR #157 (`https://github.com/LichKing-2234/ai-efficiency/pull/157`) against `perf/team-organization-128`. The PR names Draft PR #156 as its direct dependency, closes #130, and explicitly excludes merge/release/deploy work.

- [x] **Step 5: Wait for required CI and record final state**

  Record the exact implementation-head run and backend/frontend/ae-cli/deploy-static conclusions, then push one ledger commit and wait for final ledger-head CI.

  CI evidence (2026-07-16): GitHub Actions run `29487914105` (`https://github.com/LichKing-2234/ai-efficiency/actions/runs/29487914105`) completed successfully for implementation head `5c6fbd92451303a437d4c58dcb793dedec452280`: backend job `87586730854`, frontend job `87586730683`, `ae-cli` job `87586730749`, and `deploy-static` job `87586730789` all concluded `success`.
