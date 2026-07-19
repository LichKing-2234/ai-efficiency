# End-to-End Page Loading Performance Design

- **Date:** 2026-07-14
- **Status:** Approved target design; implementation status is recorded inline per ticket, while production sampling and final compatibility cleanup remain pending
- **Parent issue:** [#115](https://github.com/LichKing-2234/ai-efficiency/issues/115)
- **Contract ticket:** [#116](https://github.com/LichKing-2234/ai-efficiency/issues/116)
- **Audit baseline:** commit `70eb6ebe32298c333d4bebf144edd1b474a039dc`, production `v0.1.0-preview.71`

## Purpose and Status Boundary

This document is the active design contract for reducing end-to-end page loading latency across AI Efficiency Platform. It covers browser critical paths, API composition, database query shape, Relay calls, Redis read models, embedded static serving, runtime deadlines, readiness, and performance telemetry.

The code at the audit baseline does **not** implement most target behavior in this document. To keep project documentation honest:

1. `Current State at the Audit Commit` and statements explicitly labeled **Current** describe verified behavior at the audit commit.
2. Every other behavioral contract in this document is an approved **Target** for child tickets of #115 unless it explicitly says otherwise; `Target` in a heading is a reminder, not a requirement for that status to apply.
3. `docs/architecture.md` continues to describe current runtime behavior. Each implementation ticket updates it only after that slice becomes real.
4. This ticket changes documentation only. It does not change business behavior, API behavior, deployment, or production configuration.

## Related Documents and Supersession

This spec extends the current architecture and selectively supersedes older feature contracts. Historical specs remain unchanged so the design history stays readable.

### Personal usage

[2026-06-06-user-usage-trend-design.md](./2026-06-06-user-usage-trend-design.md) remains authoritative for:

- the personal usage range and field semantics;
- the single high-level `relay.Provider` boundary instead of handler-owned stats/trend/models calls;
- one Relay login/session per origin read;
- current-user isolation and the existing configured/invalid-credential behavior.

This spec supersedes only these parts of that design:

- usage is no longer permanently fail-fast when a transient Relay failure has an eligible recent usage snapshot;
- usage is no longer always an uncached live read;
- stats, trend, and model origin calls must use bounded concurrency rather than the historical serial-first allowance;
- the response now reports explicit usage freshness and source availability.

[2026-06-16-ai-usage-center-group-quota-design.md](./2026-06-16-ai-usage-center-group-quota-design.md) remains authoritative for `group_quotas`, including its `ok`, `empty`, and `unavailable` states and its display semantics.

This spec supersedes the assumption that the homepage has one response lifecycle and one freshness class. The frontend snapshot becomes sectioned and may be assembled from independent responses:

- usage metrics may be fresh, a cold miss result, or an eligible stale-if-error result;
- quota and subscription state must be fresh or explicitly unavailable and must never be copied from a stale usage value.

### Representative scope and Team Overview

[2026-06-26-team-usage-representative-quota-design.md](./2026-06-26-team-usage-representative-quota-design.md) remains authoritative for:

- Directory Sync-backed representative scope and fail-closed target authorization;
- team total, department comparison, top-member ranking, and billed-usage semantics;
- personal versus Team Overview information architecture;
- selected-member detail, subscription rows, rate multiplier policy, writes, and audit;
- the rule that Team Overview contains no quota cards or multiplier controls.

This spec supersedes only:

- the monolithic `GET /api/v1/user/team-usage/overview` read contract;
- full members plus recursive `member_tree` delivery in the initial response;
- request-local-only representative scope reuse;
- the first-version assumption that all Team Overview sections share one loading and error boundary.

The legacy overview contract becomes a temporary compatibility adapter during an expand-contract migration. The existing scope, selected-member, multiplier, and audit endpoints remain separate contracts.

### Administrator users and directory browsing

[2026-06-22-configurable-directory-sync-design.md](./2026-06-22-configurable-directory-sync-design.md) remains the business and snapshot contract for administrator directory reads.

The 2026-06-22 Directory Sync design remains authoritative for current-snapshot resolution from the latest successful apply, current membership authority with legacy fallback only when no membership rows exist, multi-department/subtree member deduplication, unmatched-user behavior, display-path business semantics, the union of department representative_external_ids with member leader_department_ids, matched-representative semantics, and the requirement that visible/current-filter mutation scopes agree.

The 2026-07-14 performance design extends or supersedes the administrator read transport/loading/materialization clauses: positive matched_user_id and normalized-email user mapping are both preserved and deduplicated; list/enrichment and current-filter targets become bounded SQL/page-local reads; active frontend selection/navigation uses the 20/100 option route and 25/100 immediate-child route; the complete /departments route remains response-compatible but has no active frontend caller; supplied parents are current-source validated; one shared effective relation removes the deterministic cycle-anchor edge for filtering, enrichment, options, navigation, summaries, and current-filter mutations alike. The #165 refinement persists that same relation during Directory Sync apply, versioned by source and applied run, without rewriting stored upstream parent facts. Administrator readers consume the persisted relation directly. #171 removed the transitional shared-prefix/parameter-formatter layer, made every runtime hierarchy input an explicit SQL-builder argument, and moved the complete compatibility response behind the same adminusers-owned persisted-parent summary boundary. Offboarding and source-authoring contracts do not change.

### Documented drift resolved by this spec

Current code has already drifted from a few historical Team Overview statements:

1. The historical design described a 200 `is_representative=false` overview response. The current overview implementation returns a generic 403 when an authenticated actor has no representative scope. The split endpoints retain the current fail-closed behavior; only the scope discovery endpoint returns 200 with `is_representative=false`.
2. The current overview handler parses page inputs, but the service ignores them and returns full member and tree collections. The split members and organization endpoints replace this with enforceable cursor bounds.
3. The historical design allowed member browsing when aggregate scope was too large, while the current monolithic fallback returns empty member collections. The split contract restores the intended separation: summary/trend may be unavailable while bounded members/organization remain independently retrievable.
4. Historical component names are not current contracts. API behavior, authorization, and user-visible states take precedence over old component naming.

### Deployment and health

[2026-04-08-production-deployment-packaging-design.md](./2026-04-08-production-deployment-packaging-design.md) provides historical packaging and high-level health context. This spec is the current detailed target for embedded asset delivery, server and downstream deadlines, readiness HTTP semantics, and loading-performance telemetry.

## Current State at the Audit Commit

The following facts are current at `70eb6ebe32298c333d4bebf144edd1b474a039dc`:

1. The platform is a modular monolith and one platform release unit. The Docker build embeds `frontend/dist` in the backend binary, and the backend process serves both API and SPA traffic.
2. Embedded assets and SPA fallback use Go `http.FileServer` without application-owned gzip, `Cache-Control`, ETag, or a meaningful embedded-file `Last-Modified` contract.
3. The HTTP server sets only address and handler. It has no explicit read-header or idle timeout.
4. Readiness computes body states `ready`, `degraded`, and `not_ready`, but the HTTP handler always returns status 200.
5. Redis is initialized and pinged by readiness only. No business service owns Redis keys, read models, locks, or cache metrics.
6. The configured database pool is already applied; its saturation and wait statistics are not exported.
7. Request middleware has no shared request ID, normalized route timing, response-byte metric, or downstream timing spine.
8. The frontend has a default API timeout but no sampled Web Vitals collection.
9. Personal usage fetches the usage dashboard first, then assembles group quotas from additional Relay calls. Stats, trend, and model origin reads are serial inside the current Relay adapter.
10. The personal usage page waits for both the usage dashboard and optional representative scope before leaving its page-level loading state.
11. Team Overview uses one endpoint and one response containing summary, trends, full members, and a recursive member tree. Its handler accepts page inputs, but the service currently returns the full member collections.
12. The representative scope service rebuilds directory and membership structures from current facts for each resolution.

Point-in-time production observations tied to this commit and release were:

| Surface | Observation |
| --- | --- |
| Work-item counts | 12.5-13.4 seconds backend time and requested from every protected page |
| Personal usage dashboard | 2.5-3.8 seconds backend time |
| Team overview | 5.2-6.2 seconds backend time for four members; one 12.3-second end-to-end sample |
| User providers | 3.8-4.7 seconds backend time |
| Offboarding candidates | 7.9-10.2 seconds backend time |
| Directory Sync run history | 3,968,728-byte unpaginated response |
| Entry JavaScript | Approximately 256 KB raw and 11.1 seconds transferred in the sampled environment |
| Chart chunk | Approximately 175 KB raw and 10.7 seconds transferred in the sampled environment |
| Static upstream generation | Approximately 1 ms at Kong for sampled JavaScript |

These observations are evidence for prioritization, not service-level objectives. Comparisons must retain release, route, cache state, and approximate network conditions.

## Goals

1. Make the first useful content independent from optional or slower page sections.
2. Bound database rows, Relay fan-out, response bytes, and rendered DOM by contract.
3. Reuse short-lived, reconstructible read models without weakening authorization or mutation consistency.
4. Keep Redis optional for data-plane reads: Redis failure falls back to authoritative sources.
5. Let operators distinguish browser transfer, application work, database work, and downstream Relay/SCM work.
6. Keep the existing modular monolith, provider boundaries, release units, and embedded frontend deployment.

## Non-Goals

1. CDN adoption, a separate asset domain, HTTP/3, or edge caching.
2. Splitting frontend and backend into separate repositories, containers, deployments, or release units.
3. Changing `sub2api` source code or introducing direct database coupling to it.
4. Caching authorization decisions, token revocation, credentials, or mutation results.
5. Rewriting historical specs to look like the final implementation.
6. A full OpenTelemetry collector rollout before low-cardinality request and dependency timing is established.
7. Brotli precompression before gzip and correct application cache headers are verified.

## Cross-Cutting Invariants

### Correctness before caching

1. Fix unbounded queries, N+1 behavior, duplicate requests, and oversized response contracts before caching their output.
2. A cache value is a reconstructible read model, never an authority for a write or access decision.
3. Every protected request still validates the current authenticated user, including the token revocation floor.
4. Authorization, mutation, and credential failures are not eligible stale-if-error conditions.

### Module boundaries

1. Relay and sub2api integration stays behind `backend/internal/relay.Provider` and its optional capability interfaces.
2. Handlers remain thin. Services own read-model composition, cache policy, query shape, and invalidation.
3. Frontend API access stays under `frontend/src/api`; shared request state and freshness live in API/store boundaries, not duplicated in views.
4. The platform remains a single backend process serving API and the embedded SPA.

### Bounded work

Every collection contract must define stable ordering and a default and maximum page size. Every downstream fan-out must define a concurrency limit and an overall deadline. Hidden UI must not cause unbounded requests, JSON formatting, or duplicate DOM trees.

## Redis Read-Model Contract

### Key isolation

All keys use a versioned prefix and deployment/tenant namespace. Include every dimension that changes the answer:

- provider ID and provider configuration version;
- actor or subject identity;
- directory run and representative scope version;
- date range, granularity, and timezone;
- parent node, cursor position, and limit when a cached value is a paginated response rather than the shared versioned snapshot;
- effective role where it changes visibility;
- a schema/read-model version.

Do not put passwords, JWTs, API keys, credentials, raw user email, raw query parameters, or serialized cache values into key names, metrics, or logs.

### Two-window freshness

Values that support stale-if-error store:

- `generated_at`;
- `fresh_until`;
- `stale_until`;
- the versioned payload.

The Redis hard TTL lasts through `stale_until`, not merely through the fresh window.

1. At or before `fresh_until`, return the value as `fresh`.
2. After `fresh_until` and at or before `stale_until`, perform one collapsed authoritative refresh.
3. Return the old value as `stale` only when that refresh fails with an eligible transient source error or a bounded waiter observes the failed refresh.
4. After `stale_until`, never return the value.
5. Invalid credentials, forbidden access, invalid input, provider configuration changes, and mutation conflicts are not transient source errors.

TTL jitter is 10-20 percent to avoid synchronized expiry. Jitter must not extend a value past its documented maximum stale age.

### Refresh collapse

1. Use process-local singleflight for identical keys in one replica.
2. Use a short Redis `SET NX` lease with an expiry for cross-replica refresh ownership.
3. Waiters may wait briefly for the lease holder and then read its result.
4. A cancelled or failed lease holder cannot leave a durable lock or make callers wait beyond their request deadline.
5. Redis failure bypasses cache and lease logic and performs an authoritative read under the endpoint's normal budget.

### Cache matrix

| Read model | Fresh window | Maximum stale | Required isolation |
| --- | --- | --- | --- |
| Work-item counts | 20-30 seconds | None | deployment, actor, effective role |
| Personal usage metrics | 15-30 seconds | 2 minutes | deployment, provider version, subject, credential/binding version, range, granularity, timezone |
| Team summary/trend/member/organization read models | 30-60 seconds | 5 minutes | deployment, provider version, actor, scope version/hash, range, granularity, timezone, and parent/page dimensions where applicable |
| Representative scope read model | 10-60 minutes | None across a version change | deployment, actor, current directory run, role/grant version |
| Relay group/model metadata | About 5 minutes | None | deployment, provider version, platform, group |
| Repository inventory | About 60 seconds | None | deployment and inventory version |

An implementation may cache a shared immutable snapshot and paginate it deterministically, or cache individual response pages. Only a page cache includes normalized parent/sort position and limit dimensions; never place an unvalidated raw cursor or query string in a key.

The current Directory Sync run ID is a natural scope version because representative metadata is part of that applied snapshot. If representative grants later come from another source, that source must expose a monotonic version and join the guard and cache key.

### Invalidation

Platform-owned mutations invalidate or version affected read models before returning success:

- quota request and approval state changes invalidate affected work-item counts;
- credential/provider changes persist a new version and invalidate usage/provider metadata and the mutating replica's provider client before success; other replicas follow the provider-version convergence contract below;
- Directory Sync apply and offboarding invalidate/version scope and work-item data;
- repository mutations invalidate inventory;
- subscription and quota changes invalidate any fresh quota presentation but never create a stale quota value.

Changes made directly in an external system without an AE mutation path become visible no later than the fresh TTL unless that external system supplies an explicit event.

### Provider configuration version

Provider configuration version is a shared correctness boundary, not a process-local cache counter:

1. Each provider has a persisted monotonically increasing `configuration_version`, initialized to 1.
2. Every successful mutation that can change provider behavior, credentials, enablement, primary selection, base URL, or default model increments that version in the same database transaction as the mutation.
3. Read-model keys use the persisted version. A timestamp alone is not the version contract because timestamp precision and concurrent updates must not alias.
4. Process-local provider clients are keyed by `(provider_id, configuration_version)`, revalidate the persisted version at least every 30 seconds, and have a maximum lifetime of five minutes.
5. After commit and before returning success, the mutating replica evicts its old client and publishes a best-effort cross-replica invalidation. A replica that misses the notification converges through the 30-second version check and maximum lifetime; it never keeps an old client indefinitely.
6. Secrets remain only in the authoritative provider record and process-local client. Redis read models, keys, metrics, logs, and invalidation messages contain the provider ID and version but never the secret value.

### Never cached

Do not cache or use stale values for:

- `users.token_valid_after` and current user disable state;
- quota approve/reject/reset decisions;
- Relay disable or rate-multiplier writes;
- Directory Sync apply;
- repository binding or webhook repair writes;
- checkpoint or attribution ingest decisions;
- passwords, JWTs, API keys, provider credentials, or credential payloads.

## Personal Usage Target Contract

### Implementation status for #123

The #123 slice implements the personal usage clauses in this section with these
concrete choices:

- `backend/internal/personalusage` owns current-user/provider resolution, the
  two-window usage read model, freshness semantics, and fresh-only quota
  composition. The Gin handler is a thin projection and error-mapping adapter.
- `relay.UserUsageOriginReader` is the optional migration seam. It selects usage
  and quota branches, uses one login when usage is requested, starts no more
  than five child calls concurrently, and applies one 12-second origin deadline.
- Personal usage keys use SHA-256 over the deployment namespace, provider id and
  persisted `configuration_version`, actor id, Relay subject id,
  `users.updated_at` binding version, start/end, granularity, and timezone. The
  value contains only range/stats/trend/models and freshness timestamps.
- Freshness uses 10-20 percent jitter: 24-27 seconds fresh and 96-108 seconds to
  the hard stale deadline. Invalid credentials, configuration errors, and caller
  cancellation never use stale. Redis failure bypasses the distributed read
  model while retaining the bounded process-local flight.
- The first-party browser sends the usage-only and quota-only requests in
  parallel from `UserUsageDashboard`. Representative-scope discovery runs
  independently in `DashboardView`; the API helpers remain in `frontend/src/api`.
  Each personal request branch has its own AbortController, one generation guard
  protects all state, and chart modules load only after usable usage exists.
- `/usage/members/:user_id` remains isolated on the selected-subject team API and
  does not call or populate the personal usage cache or quota endpoint.

These statements mark only #123 as implemented. Provider-client convergence,
the Team Usage split, telemetry, and every other ticket in the rollout table
remain target contracts until their own slices land.

### Endpoint and origin composition

The stable combined endpoint remains available for existing callers:

```text
GET /api/v1/user/usage/dashboard?start_date=...&end_date=...&granularity=...&timezone=...
```

To give usage and quota independent loading lifecycles without streaming a JSON response, the first-party browser issues these requests in parallel:

```text
GET /api/v1/user/usage/dashboard?start_date=...&end_date=...&granularity=...&timezone=...&include_group_quotas=false
GET /api/v1/user/usage/group-quotas?start_date=...&end_date=...&granularity=...&timezone=...
```

The additive `include_group_quotas=false` projection returns the existing usage fields plus `usage_freshness`, and omits `group_quotas` and `quota_freshness`. Omitting the parameter preserves the current combined response shape for compatibility. The independent quota endpoint returns only `group_quotas` and `quota_freshness`. The frontend keeps request definitions in its API boundary, composes the two results in the personal-usage domain component, and renders either result as soon as it arrives.

The service checks the usage-metric cache before deciding which origin branches are needed. The Relay provider boundary exposes one high-level, request-scoped origin read with explicit branch selection:

1. Authenticate once when an origin read requires user authentication.
2. Fetch requested stats, trend, models, keys, and subscriptions with bounded concurrency.
3. Do not expose stats/trend/models as separate handler calls.
4. Do not call an aggregate dashboard operation and duplicate component operations in the same request.
5. Treat stats, trend, and models as one atomic usage generation. A refresh stores all three with one generation timestamp or stores none; it never mixes live and cached components or components from different generations.
6. Treat quota/subscription facts as a separate fresh-only unit. The independent quota endpoint requests only key/subscription branches and does not refresh stats, trend, or models.

An implementation may evolve `GetUserUsageDashboard` directly or use a temporary optional capability during migration, but the stable service/handler contract is one high-level origin operation with explicit branch selection rather than handler-owned Relay orchestration. The combined compatibility response composes the same internal usage and quota operations; it does not make an internal HTTP call.

### Response freshness

The existing usage fields and `group_quotas` remain in the default combined response. Add separate usage and quota freshness metadata:

```json
{
  "usage_freshness": {
    "as_of": "2026-07-14T08:00:00Z",
    "fresh_until": "2026-07-14T08:00:30Z",
    "stale_until": "2026-07-14T08:02:00Z",
    "cache_status": "fresh",
    "source_status": "ok"
  },
  "quota_freshness": {
    "as_of": "2026-07-14T08:00:01Z",
    "cache_status": "uncached",
    "source_status": "ok"
  }
}
```

`cache_status` is one of:

- `miss`: this request read the authoritative source and populated a new value;
- `fresh`: the usage value came from the fresh window;
- `stale`: an eligible refresh failed and the value is still before `stale_until`.

For usage, `as_of` is the atomic usage generation time in UTC RFC3339 form. `source_status` is `ok` or `error`. A stale response uses `source_status=error`; it is a successful HTTP response with a degraded freshness state, not a page-level error lifecycle. The response never claims a stale value is fresh.

`quota_freshness.cache_status` is always `uncached`. Its `source_status` is `ok` when the current request completed the authoritative quota/subscription branch, including an empty result, and `error` when that branch failed or is unsupported. `quota_freshness.as_of` is the UTC RFC3339 completion time for an `ok` branch and `null` for an `error` branch.

`group_quotas.status` remains the quota availability contract:

- `ok`: fresh quota/subscription facts were read and rows are available;
- `empty`: the fresh read proves there are no displayable rows;
- `unavailable`: the fresh read failed or the provider lacks the capability.

Quota/subscription rows are not stored inside the stale usage value. A stale default combined response may therefore carry `group_quotas.status=ok`, `empty`, or `unavailable` based on the current request's fresh quota branch; the usage-only projection omits that branch entirely.

This cache contract applies to the current user's personal dashboard only. Team selected-member dashboards do not automatically read or populate the personal cache; any reuse there requires its own subject authorization, cache isolation, freshness, and invalidation contract.

### Error behavior

1. Missing Relay configuration remains a 200 response with `configured=false`, empty usage data, and no cached usage freshness.
2. Invalid or changed credentials remain a 409 configuration error and do not use stale-if-error.
3. A transient usage origin failure returns eligible stale usage with HTTP 200, `usage_freshness.cache_status=stale`, and `usage_freshness.source_status=error` when available.
4. A transient usage origin failure after the hard stale deadline returns the existing upstream failure response.
5. A quota failure returns HTTP 200 from the independent quota endpoint with `group_quotas.status=unavailable`, `quota_freshness.as_of=null`, and `quota_freshness.source_status=error`. It does not change the usage request's HTTP result or lifecycle. The default combined response preserves the same section-local unavailable state.
6. A platform-owned credential, provider, or subscription mutation invalidates the corresponding read model before success is returned.

### Frontend behavior

1. Personal usage, quota, and optional representative scope start independently. Personal usage leaves its page-level loading state when the usage-only request is available; quota owns only the quota section, and representative scope controls only Team tab visibility.
2. Usage freshness is visible when a stale value is shown. Fresh and cold-miss values need no noisy badge.
3. Quota unavailable state remains local to the quota section.
4. Superseded range requests are aborted or prevented; only the latest selected range updates the view.
5. Chart code loads after data is available or shortly before its viewport entry.

## Representative Scope Target Contract

### Lightweight authoritative guard

Every representative endpoint validates:

1. current authenticated user and token revocation state;
2. current applied Directory Sync source and successful run ID;
3. actor user ID and current role;
4. the representative grant version.

For the current Directory Sync-backed model, grant version is the applied run ID because the representative IDs and leader department IDs are facts in that run. A stable scope version may be derived from:

```text
source ID + applied run ID + actor user ID + actor role + token revocation floor + scope schema version
```

The derived version contains no secret and may be represented as an opaque hash.

### Cached scope fields

A scope read model may contain:

- represented root department IDs;
- descendant department IDs;
- normalized member subject identities and Relay IDs;
- tree-navigation metadata needed by read endpoints.

A hit avoids loading and rebuilding all departments, members, memberships, and users. A version change always selects a new value. Redis failure rebuilds from authoritative database facts.

Selected-member reads still validate the requested subject against the current versioned scope. Rate-multiplier writes and other high-impact access decisions recheck authoritative current facts and fail closed; they do not trust only a cached member list.

### No-scope behavior

`GET /api/v1/user/team-usage/scope` remains the discovery endpoint and returns 200 with `is_representative=false` when no scope exists.

Direct summary, trend, members, organization, selected-member, and write endpoints require current representative scope. They return a generic 403 for a no-scope actor rather than revealing target or organization facts. The frontend maps this to the compact no-delegated-scope state.

## Split Team Overview Target Contract

All four read endpoints share common range normalization, representative authorization, scope version, provider version, cache isolation, and freshness metadata. A failure in one endpoint does not change the HTTP result or rendered state of another section.

Common response metadata:

```json
{
  "as_of": "2026-07-14T08:00:00Z",
  "fresh_until": "2026-07-14T08:01:00Z",
  "stale_until": "2026-07-14T08:05:00Z",
  "cache_status": "fresh",
  "source_status": "ok",
  "scope_version": "opaque-version",
  "request_id": "request-id"
}
```

`as_of` is the snapshot generation time in UTC RFC3339 form. `cache_status` is `miss` when this request generated and stored the snapshot, `fresh` when it reused a value within the fresh window, or `stale` when an eligible refresh failed before `stale_until`. `source_status` is `ok` for `miss` and `fresh`, and `error` for `stale`. A stale value is a successful degraded response and never bypasses the current representative authorization guard.

### Summary

```text
GET /api/v1/user/team-usage/summary?start_date=...&end_date=...&granularity=...&timezone=...
```

The response contains:

- common freshness metadata;
- `window`, preserving the current normalized `start_date`, `end_date`, `granularity`, `today`, `rolling_days`, and `timezone` fields;
- `summary`, preserving `unavailable`, `unavailable_reason`, `member_count`, `relay_member_count`, `range_actual_cost`, `range_total_tokens`, `today_actual_cost`, `total_actual_cost`, and `unit_label`.

The existing semantics remain: totals cover the complete authorized scope, canonical members are deduplicated in team total, and an unavailable full-scope computation is explicit rather than a truncated total. `range_actual_cost` and `range_total_tokens` are the selected-window aggregate values; `today_actual_cost` and historical `total_actual_cost` remain comparison values and must not be mislabeled as selected-window totals.

The #164 refinement makes this endpoint an independent cold-cache read rather than a projection of the compatibility overview snapshot:

1. Summary has its own versioned Redis key space, process-local flight, and distributed lease while retaining the common provider, actor, scope, range, granularity, and timezone isolation dimensions and freshness/stale rules.
2. Its origin resolves only the current authorized scope, provider binding, Relay subject identities, and the summary-specific batch capability. It never calls `TeamMemberTrendProvider`, ranks members, projects trend series, or constructs an organization tree.
3. `TeamUsageSummaryProvider` receives the normalized start date, end date, granularity, and timezone. A provider may return selected-window billed usage and token totals alongside the existing today and historical comparison totals.
4. Selected-window totals are available only when the summary capability returns complete range values for the connected scope. A provider that does not support those fields or returns incomplete range fields yields a summary with `unavailable=true`, stable reason `range_aggregation_unavailable`, null range totals, and still-correct member counts plus comparison totals. Authentication, authorization, invalid input, provider configuration failure, and invalid credentials remain endpoint-level failures and never become partial data.
5. The first-party Sub2API adapter preserves compatibility with upstream deployments whose batch usage response does not yet contain selected-window fields. It first consumes complete `range_actual_cost` and `range_total_tokens` values directly. Only for returned users with either range field missing, it may internally call the existing per-user trend endpoint with the same normalized range parameters and aggregate those points to fill both fields. This is a provider implementation detail: the Summary service still calls only `TeamUsageSummaryProvider` and does not acquire or invoke `TeamMemberTrendProvider`.
6. The compatibility fallback is bounded by the adapter's existing trend-request concurrency and is skipped when the batch response is complete. If fallback retrieval fails or does not produce complete range totals for every connected user, the adapter retains the incomplete fields without converting the compatibility failure into a provider-wide error; the Summary service then returns `range_aggregation_unavailable` as described above. Authentication, authorization, provider configuration failure, invalid credentials, and failure of the primary batch request remain endpoint-level failures.
7. Summary cache values contain only normalized window and summary data. They cannot satisfy trend, members, organization, or compatibility reads, and those larger values cannot satisfy Summary.
8. The first-party frontend keeps rendering the available summary cards and the section-local selected-window warning while trend, members, or organization remain loading or unavailable.

After #172, the one-release compatibility endpoint consumes this Summary lane through its internal typed reader. It does not call the Summary HTTP endpoint and owns no compatibility cache; eventual route removal remains governed by the compatibility contract below.

### Trend

```text
GET /api/v1/user/team-usage/trend?start_date=...&end_date=...&granularity=...&timezone=...
```

The top-level DTO contains common freshness metadata, `window`, `top_members`, `top_member_trend`, and `department_trend`. It preserves the current row and series field names while applying these bounds:

1. `top_members` and `top_member_trend.series` contain at most 12 subjects, ranked by complete selected-window token usage with billed usage as the existing tie-breaker/auxiliary value. `top_member_trend.rank_basis` remains `range_total_tokens`.
2. `department_trend.series` contains one independent `series_type=team_total` row and at most 12 `series_type=department` comparison rows using the existing represented-root/first-branching-child bucketing rules.
3. When more than 12 department buckets exist, select the 12 largest by selected-window total tokens descending, then billed usage descending, then department external ID ascending. `department_trend` reports `comparison_total_count` and `comparison_truncated`; the independent team-total series still covers the complete authorized scope.
4. Trend state objects retain `unit_label`, `unavailable`, `unavailable_reason`, and `series`. Top-member series retain subject identity, display name, rank, per-series availability, and points. Department series retain series type, department identity, display name, rank, per-series availability, and points.
5. Stable unavailable reasons are `scope_too_large` and `provider_error`. A failed member or department series has `unavailable=true`, an explicit reason, and empty points. Authentication or authorization failure is an endpoint-level 401/403 and never a partial DTO.

Series use stable department external IDs or stable subject identities. Team total, group comparison, and top-member series remain separate chart areas. Partial provider failure is encoded inside the trend response without affecting a successful summary response; authorization still fails closed.

The #167 refinement makes Trend an independent read model rather than a projection of the compatibility overview snapshot:

1. Trend owns a versioned `team-usage-trend` Redis key space, process-local flight, distributed lease, freshness window, stale-if-error window, and stable `team_usage_trend` metrics name.
2. Its origin resolves only current representative scope, provider binding, Relay subject identities, batch summary stats required for comparison values, and the trend capability. The stats request does not require range backfill because Trend ranks from the separately fetched points; this prevents the Relay compatibility adapter from performing the same per-user trend fan-out twice. It does not compose the Summary DTO, rank a full member page, or construct an organization tree.
3. The cached value contains only normalized window, bounded `top_members`, `top_member_trend`, and `department_trend`. It cannot satisfy Summary, Members, or Organization, and none of those values can satisfy Trend. Compatibility Overview consumes this typed value as one part of its complete historical response.
4. #172 makes Compatibility Overview read this same Trend lane through an internal typed reader. The adapter owns no compatibility-only origin, `team-usage-snapshot` Redis key, or `team_usage_overview` metric.
5. The first-party frontend retains its independent Trend request/error/loading/stale state and async chart chunk. A delayed, failed, stale, or expired Trend request changes only the Trend section, does not set the page-wide busy/dim/disabled state after sibling sections settle, and never clears an available Summary or Members section.
6. A transient whole-origin Trend failure returns an eligible stale generation before its hard deadline. With no eligible stale generation, the endpoint returns the explicit `provider_error` Trend state without persisting that outage snapshot as a fresh cache value.

### Members

```text
GET /api/v1/user/team-usage/members?start_date=...&end_date=...&granularity=...&timezone=...&cursor=...&limit=...
```

Contract:

1. Default limit is 50; maximum is 100.
2. Stable order is selected-window total tokens descending, then stable subject key ascending. The subject key is `user:<positive-user-id>` when a positive platform user ID exists; for `user_id=0`, it is `directory:<source-scoped-directory-member-external-id>`.
3. Each row preserves the current member field contract: `rank`, `user_id`, `directory_member_external_id`, display metadata, all current department memberships, `relay_user_id`, selected-window and comparison totals, `total_tokens`, optional `subscription_count`, and `selectable`.
4. The response contains common freshness metadata, `window`, `items`, `total_count`, and `next_cursor`.
5. It does not contain the organization tree or duplicate top-member/member arrays.
6. Ranking is based on the complete supported scope, not only the returned page.

The cursor is opaque and integrity-protected. It binds to scope version, snapshot identity, range, and sort position. An invalid cursor returns 400. A valid cursor whose snapshot is no longer available returns 409 with stable code `snapshot_expired`; the frontend restarts only the member section.

The #168 refinement makes Members an independent immutable ranking read model rather than a projection of the compatibility overview snapshot:

1. Members owns a versioned `team-usage-members` Redis key space, process-local flight, distributed lease, freshness window, stale-if-error window, and stable `team_usage_members` metrics name.
2. Its origin resolves only current representative scope, provider binding, Relay subject identities, and `TeamUsageSummaryProvider` stats. It passes at most 100 Relay user IDs per batch request and sets `RequireCompleteRange=true` so selected-window billed usage and token totals are available without acquiring or invoking `TeamMemberTrendProvider` from the Members service lane.
3. The origin maps current display, department membership, Relay identity, selected-window, and comparison fields into member rows, then ranks the complete supported authorized scope once before caching. A missing range field left by a provider compatibility fallback remains an available zero/nil row rather than coupling the page to a Trend DTO or organization-tree failure.
4. The cached value contains only normalized window plus complete immutable ranked member rows. It contains no summary, top-member or department series, recursive `member_tree`, request ID, scope version, or cursor. It cannot satisfy Summary, Trend, or Organization, and none of those values can satisfy Members. Compatibility Overview consumes the complete Members value and projects its historical tree separately.
5. The existing HMAC cursor still binds actor, normalized range, scope version, complete ranked-content identity, and next offset. An unchanged authoritative rebuild during Redis failure preserves pagination, while a changed `RangeTotalTokens`, roster, identity, display, or membership generation returns `snapshot_expired`.
6. A first-page Members request never reads or writes the Summary or Trend lanes, and no compatibility Overview lane exists. Summary or Trend section failure therefore does not change an otherwise available Members response; a transient failure of the Members origin itself prefers an eligible stale Members generation until its hard deadline.
7. #172 makes Compatibility Overview consume the complete Members typed value; Organization has owned its independent branch origin/cache since #170. The first-party frontend keeps its existing Members-only loading/error/stale/pagination lifecycle and renders only the returned 50 rows from a 500-member result.

### Organization

```text
GET /api/v1/user/team-usage/organization?start_date=...&end_date=...&granularity=...&timezone=...&parent_department_external_id=...&department_cursor=...&department_limit=...&member_cursor=...&member_limit=...
```

Contract:

1. Return immediate child departments only, never a recursive full tree.
2. Department default limit is 25; maximum is 100.
3. Department order is normalized display name ascending, then external ID ascending.
4. Return direct members for the requested node as a separately paginated collection.
5. Member default limit is 50; maximum is 100.
6. Member order is selected-window total tokens descending, then the same stable subject key as the members endpoint ascending.
7. Return independent `next_department_cursor` and `next_member_cursor` values.
8. The top-level DTO contains common freshness metadata, `window`, nullable `parent_department_external_id`, `departments`, `members`, `next_department_cursor`, and `next_member_cursor`.
9. Department rows contain `department_external_id`, `parent_external_id`, `name`, `display_path`, `depth`, `child_count`, `has_children`, `direct_member_count`, `aggregate_member_count`, `connected_member_count`, `range_actual_cost`, and `range_total_tokens`. `has_children` is exactly `child_count > 0`.
10. Direct-member rows in `members` use the same member row contract and stable subject key as the members endpoint.
11. Both cursors bind to scope and snapshot version and use the same invalid/expired behavior as the members endpoint.

The omitted parent identifier represents authorized root nodes. A supplied parent outside the current scope returns the generic scoped error.

The #170 refinement makes Organization an independent branch read model rather than a projection of compatibility Overview:

1. Organization owns a versioned `team-usage-organization` Redis key space, process-local flight, distributed lease, freshness window, stale-if-error window, and stable `team_usage_organization` metrics name. The key adds the normalized nullable parent to the common provider, actor, scope, and range dimensions.
2. Each origin selects the requested parent from the current authorized flat scope, returns only its immediate department rows, and ranks only that parent's direct members. It never calls `TeamMemberTrendProvider`, invokes the compatibility Overview origin, builds `OverviewMemberNode`, or serializes a recursive tree.
3. Department aggregate facts resolve only the requested child subtrees plus the parent's direct subjects. The origin calls `TeamUsageSummaryProvider` with `RequireCompleteRange=true` in batches of at most 100 Relay user IDs, deduplicates multi-membership subjects inside each child aggregate, and preserves membership in each directly joined department. A virtual-root request returns no direct members.
4. Direct-member ranks are dense and branch-local: selected-window total tokens descending, then the same stable subject key as Members ascending. They intentionally do not preserve sparse full-scope ranks, because doing so would require loading unrelated branch usage facts or consuming the Members value, which cannot satisfy Organization.
5. The cached value contains only normalized window, nullable parent, immediate departments, and complete immutable ranked direct-member rows. The parent stored in the value must exactly match the root/child key; a root/child or child/child mismatch is rejected and rebuilt before return. Organization values cannot satisfy Summary, Trend, or Members, and none of those values can satisfy Organization. Compatibility Overview never invokes these paginated branch reads.
6. Department and member cursors remain collection-tagged, opaque HMAC values bound to actor, normalized range, parent, scope version, branch content identity, and next offset. Redis failure may rebuild identical branch content and continue pagination. A changed scope or branch identity returns `snapshot_expired` only to the requested branch.
7. A transient branch-origin failure prefers only that branch's eligible stale value. A cold failure does not populate an outage snapshot and cannot remove available Summary, Trend, Members, root, or sibling Organization responses.
8. The first-party frontend retains one generation-safe state record per nullable parent. Expiration replaces only the affected branch, invalidates only loaded descendants reachable from its current immediate departments, preserves unrelated siblings and split sections, and ignores late responses from invalidated descendants.

### Legacy overview compatibility

The migration is expand-contract:

1. Land shared normalization, authorization, scope version, and read services.
2. Add the four split endpoints and migrate the frontend in the same platform release.
3. Keep `GET /api/v1/user/team-usage/overview` for one complete platform release as an adapter over the same services and caches.
4. Continue accepting every historical query parameter. In particular, `page` and `page_size` remain accepted and ineffective during compatibility; the adapter must not silently turn them into pagination.
5. Return the complete historical response shape throughout the compatibility release, including the full member collection and recursive member tree expected by old consumers.
6. Build that response from the same authorized scope snapshot, internal services, and read models as the split endpoints. The adapter must not retain a second monolithic or Relay calculation path, use a separate cache, or make internal HTTP calls to the split routes.
7. Emit an RFC 9745 `Deprecation` structured date, an RFC 8594 `Sunset` HTTP date, and a successor link on every compatibility response, for example:

   ```http
   Deprecation: @1783987200
   Sunset: Tue, 15 Sep 2026 00:00:00 GMT
   Link: </api/v1/user/team-usage/summary>; rel="successor-version"
   ```

8. Production telemetry and code search must show no current frontend, internal caller, or verified external consumer before removal.
9. Remove the legacy route, adapter, monolithic DTO, and obsolete tests no earlier than the following platform release. If a verified consumer remains near the announced sunset, extend the sunset and compatibility period rather than breaking that consumer.

Issue #172 implements the adapter by creating one request-scoped split-read context, normalizing once, and resolving the current representative scope plus provider configuration once. It sequentially reads Summary, Trend, and complete Members through their existing typed cache lanes, then projects the recursive `member_tree` from that same authorized scope and Members snapshot. Sequential assembly avoids concurrent access to provider implementations that do not promise request-level concurrency safety. A supported soft Trend failure marks only trend fields unavailable; independently available Summary and Members values remain present. The removed `team-usage-snapshot` key and `team_usage_overview` metric have no runtime owner, and old Redis values expire naturally. This implementation does not change the announced deprecation headers, sunset, removal gate, or accepted ineffective `page` and `page_size` parameters.

## Other Bounded Read Contracts

### Work items and offboarding

1. Work-item counts use count-oriented queries and never load the full offboarding candidate list.
2. Offboarding candidates use database anti-join/batched latest-action logic and stable server pagination.
3. Quota approver containment uses an index matched to its JSON predicate.
4. Client/store freshness prevents repeated count requests during protected navigation and mobile-sidebar remounts.
5. Disable/offboarding/quota mutations invalidate affected count values before success.

### Events

1. Summary uses database aggregates over the same filters as the list.
2. List uses database filtering, count, stable ordering, and bounded pagination with default 20 and maximum 100.
3. List rows omit raw payloads; detail returns the selected full payload.
4. Hidden raw JSON is formatted only on expansion.
5. Search fields that cannot be expressed or indexed safely in SQL require an explicit contract decision before implementation; the service must not silently restore a full in-memory scan.

### Directory Sync run history

1. Run summaries use stable `started_at` descending plus run ID ordering.
2. Default page size is 20; maximum is 100.
3. Summary rows omit full source/result JSON, warnings, preview diffs, and other large diagnostic blobs.
4. Run detail remains the complete diagnostic contract.

### Repository, PR, member detail, and administrator reads

1. Repository inventory aggregates in SQL before an optional 60-second cache; repository mutations invalidate/version it.
2. Repository list has a stable default provider/scope contract and does not wait for inventory solely to determine first-page parameters.
3. PR list pagination defaults to 20 and has a maximum of 100. Invalid or nonpositive `limit` values use the default; values above 100 are capped at 100; invalid or negative `offset` values use zero. The page freshness service independently rejects more than 100 distinct PR IDs before database work. Freshness then uses three bulk fact shapes rather than per-PR or per-commit reads: snapshot rows bounded by those at-most-100 PR IDs, repository-scoped pending usage evidence, and usage-event count/latest-observation facts grouped by checkpoint ID. The checkpoint aggregate passes all collected checkpoint IDs as one PostgreSQL array argument to `= ANY(...)`, keeping bind-parameter cardinality fixed even when a page has more than 2,000 snapshots. Snapshot rows are ordered by `sort_order` then ID; the first non-`fresh` commit supplies the PR-level status and reason. With no snapshot, repository-level pending evidence selects `pending_upload` before the never-refreshed and refreshed-empty `no_checkpoint` reasons. Selected detail uses the same classifier, while list status fields, summary, filtering, ordering, and complete detail diagnostics remain compatible.
4. Selected-member group metadata uses a batch-shaped provider capability; when upstream remains per-group, the adapter uses bounded fan-out and deadlines.
5. `GET /api/v1/admin/users` keeps one-based pages, stable user ID ascending order, a default page size of 20, and a maximum of 100. Its list count, page, and enrichment are SQL-backed and bounded to the resolved current Directory Sync source; enrichment materializes only the returned page's matching members, candidate memberships, existing candidate departments, and ancestor closure.
6. Current `directory_member_departments` rows are authoritative whenever a member has any such rows; only a member with no current membership rows may use the legacy primary department. Page enrichment loads candidate existence before choosing the current primary or first ordered existing membership, so dangling candidates are skipped without restoring full-snapshot materialization.
7. Directory members map to local users through both a positive `matched_user_id` and normalized-email equality. The two paths are unioned and deduplicated, while unmatched local users remain visible only when no department scope excludes them.
8. `GET /api/v1/admin/users/department-options` defaults to 20 rows and caps at 100. `GET /api/v1/admin/users/department-children` returns immediate children only, defaults to 25 rows, caps at 100, and validates a supplied parent inside the resolved current source. `GET /api/v1/admin/users/departments` remains response-compatible for legacy consumers but has no active frontend caller.
9. One source-scoped effective-department relation is shared by filtering, enrichment, options, navigation, summaries, the complete compatibility response, and current-filter mutations. Missing parents become effective roots; each closed cycle removes the deterministic normalized-name/external-ID-first anchor edge under explicit locale-independent `C` collation semantics, so root and child navigation, anchor/non-anchor subtree filtering, and summaries expose every component row once without duplicates regardless of the database default collation. Directory Sync apply persists the same effective parent on every current department row with that row's applied run id. Administrator readers project that stored value directly and no longer count the full source, walk every raw parent path, or infer repairs per request. Requested subtree, page-candidate ancestor, requested-summary descendant, and complete-response preorder relations are the only remaining recursive administrator relations. Their SQL is composed directly by the Ent builder with explicit source, selected-department, and candidate-array arguments; no query formatter rewrites or infers a previous positional placeholder. The complete route uses one persisted-parent preorder query plus one shared summary query and therefore has a constant query-role count as source cardinality changes. An environment with rows created before effective-hierarchy storage must complete a successful apply after the storage expansion and before activating these readers: a persisted root and an unmaterialized legacy row both store `NULL`, so read paths must not guess or reconstruct a fallback.
10. Representative totals use a current-source-scoped union of department `representative_external_ids` and member `leader_department_ids`, accepting scalar and array JSON forms and deduplicating repeated or cross-field declarations before matched-representative evaluation.
11. `GET /api/v1/admin/users`, persisted `current_filter` jobs created through `POST /api/v1/admin/users/subscription-jobs`, and compatibility `current_filter` batches through `POST /api/v1/admin/users/subscriptions/batch` use the same normalized filters, effective-subtree-to-user predicate, stable ordering, and bounded target reader.

## Frontend Critical-Path Contract

### Routing and identity

1. Public routes do not wait for `/auth/me` because a token exists.
2. Authenticated non-admin route chunks and identity hydration start concurrently.
3. Admin routes do not render protected content until the current role is verified.
4. One navigation does not introduce duplicate current-user requests.

### Code and data loading

1. Chart.js loads when chart data exists or before viewport entry, not before page skeleton/API start.
2. Only the active locale dictionary is in the initial entry path; another locale loads once on switch with a stable fallback while loading.
3. Settings loads one section component and its owned requests at a time.
4. Shared credentials and Directory Sync sources are deduplicated in a shared API/store owner.
5. Quota-reset mine, approvals, and admin queues have independent states and load on demand.
6. Repository core content does not wait for provider-binding options.

### Rendering

List-heavy pages render one row/card subtree for the active viewport instead of mounting desktop and mobile copies and hiding one with CSS. Dynamic labels, loading indicators, and details must not change stable layout dimensions unexpectedly.

## Embedded Static Serving Target

1. Hashed assets under `/assets/` return `Cache-Control: public, max-age=31536000, immutable`.
2. `index.html`, root SPA responses, SPA fallback, and OAuth SPA entry responses return `Cache-Control: no-cache` or an equivalent mandatory revalidation policy.
3. JavaScript, CSS, JSON, SVG, and HTML negotiate gzip and set `Vary: Accept-Encoding`.
4. ETag may support HTML revalidation. Do not fabricate `Last-Modified` for embedded files.
5. HEAD, content type, fallback, conditional request behavior, and decompression correctness are part of the handler contract.
6. Static policy never applies to `/api/*`, OAuth token responses, or authenticated API responses.
7. CDN, separate asset domain, Brotli, and HTTP/3 remain deferred.

## Runtime Deadline and Readiness Target

1. The HTTP server defines at least `ReadHeaderTimeout` and `IdleTimeout`.
2. Read/write budgets remain configurable and must accommodate the longest intentional synchronous endpoint during migration; they are tightened after the monolithic team contract is removed.
3. Relay, SCM, Directory, and other downstream HTTP clients define connect, response-header, and overall deadlines plus bounded connection pools.
4. Browser, proxy, server, handler, and downstream budgets are ordered so inner work stops before the caller's deadline. Cancellation must propagate and must not leave background requests running.
5. Readiness completes under a short overall deadline.
6. Database down returns body status `not_ready` and HTTP 503.
7. Non-critical Redis or Relay degradation returns body status `degraded` and HTTP 200.
8. Liveness never depends on external services.
9. Redis cache failure does not make the data plane not-ready because cache-enabled reads can fall back to authoritative sources.

Concrete default durations are configuration decisions in the runtime implementation ticket and must be validated against the still-current longest endpoint before rollout. This spec does not invent a shorter production budget from point-in-time samples.

## Observability and Privacy Target

### Request and dependency telemetry

1. Accept a bounded, validated incoming request ID or generate one; return it on every response.
2. Request ID may appear in logs/traces but never as a metrics label.
3. Request telemetry includes normalized route, method, status class, duration, response bytes, in-flight count, and release.
4. Dependency telemetry uses stable dependency and operation names for database, Relay, SCM, Directory, and Redis timing/errors.
5. Slow requests and 5xx responses are logged at full rate; ordinary successful requests may be sampled.
6. The metrics scrape endpoint is reachable only from an internal network boundary or requires service authentication; it is never an unauthenticated public route.

### Pool and cache telemetry

1. Export database open/in-use/idle connections, wait count/duration, and lifetime closures.
2. Export Redis pool connections, waits, and timeouts separately from application cache outcomes.
3. Application cache outcomes are `fresh`, `miss`, `stale`, `error`, refresh, and lease acquired/wait/failed.
4. Cache metrics use a stable cache name and outcome only. They never include a key, actor, scope, provider ID, range, or cached value.

### Browser telemetry

Sample LCP, INP, CLS, and TTFB with normalized route, release, and navigation/cache class. Do not collect route parameters, query strings, user identity, email, DOM text, or page content. Ingestion requires rate limiting, retention limits, and an internal access boundary.

### Prohibited telemetry

Do not record raw path/query/body, user IDs or emails, cache keys/values, JWTs, API keys, credentials, SQL parameters, or unredacted downstream error payloads. Full OpenTelemetry rollout and `Server-Timing` remain optional follow-ups after these labels and timings stabilize.

## Testing Contract

Tests assert externally observable behavior at the highest existing seam.

### Frontend route tests

- Delay optional scope or quota and assert available personal usage renders first.
- Delay team trend/members/organization and assert summary remains available.
- Assert hidden Settings sections and quota queues make no request.
- Assert non-admin chunks start before identity hydration while admin content remains fail-closed.
- Assert one rendered row subtree per record and raw JSON formatting only after expansion.

### Backend module and contract tests

- Use fake Relay/SCM providers and controllable clocks for origin concurrency and freshness.
- Prove personal stats, trend, and models share one atomic generation, while quota freshness remains uncached and independently reported.
- Delay the independent quota endpoint and prove the usage-only response completes first; also preserve the default combined response contract.
- Cover cold miss, fresh hit, soft expiry, hard expiry, eligible stale-if-error, invalidation, version changes, Redis outage, serialization mismatch, and actor/scope isolation.
- Prove concurrent cold requests collapse to one authoritative refresh and lease cancellation cannot deadlock waiters.
- Prove quota/subscription data and authorization/mutation decisions are never served stale.
- Exercise all four team endpoints independently, including pagination bounds, stable subject-key ties, cursor integrity, snapshot expiry, and no-scope authorization.
- Prove the legacy overview accepts historical query parameters, emits standards-compliant deprecation headers, preserves its full response without internal HTTP calls or a monolithic Relay calculation/cache, and creates or reuses only Summary, Trend, and Members cache keys.
- Prove provider mutations increment the shared configuration version, old clients are evicted, and a replica that misses invalidation converges within the version-check bound.

### Handler and database tests

- Embedded handler tests cover gzip, `Vary`, cache classification, HTML fallback, HEAD, content type, and conditional requests if ETag is implemented.
- Health tests cover database 503, non-critical degraded 200, liveness, and deadlines.
- Scale fixtures include at least 500 team members, a large directory/offboarding set, large event tables, many repositories/PR checkpoints, and long Directory Sync history.
- Query-plan tests prove database pagination/aggregation and bounded row materialization.

### Regression suites

Each implementation slice runs its focused tests regularly and the repository verification commands before completion:

```text
cd backend && go test ./...
cd ae-cli && go test ./...
cd frontend && npm test
cd frontend && npm run test:e2e:role
```

Environment-sensitive browser, listener, and production checks are reported separately from unit-test results.

## Production Measurement Contract

Every production comparison records:

- platform release and commit;
- normalized route;
- cold or warm browser/read-model state;
- approximate network/device class;
- request count and transferred bytes;
- TTFB and first useful content;
- backend and dependency timing;
- cache status;
- errors and fallback state.

Browser experience uses p75 Web Vitals; server/dependency analysis uses at least p75 and p95. Group by route and release, never by user.

The standard Core Web Vitals good thresholds are target direction, not an automatic gate in this first rollout. Route-specific numerical budgets are approved only after telemetry and the corresponding serving/cache/API/frontend slice have enough production samples. Insufficient data must be reported as insufficient, not converted into a pass.

Initial comparative acceptance includes:

1. repeat work-item and usage reads do not cause duplicate origin refresh inside their fresh window;
2. eligible stale values remain available only through their hard stale deadline;
3. quota data is never stale;
4. entry transfer reflects gzip and repeat navigation reuses immutable assets;
5. team summary and personal usage render independently from optional slow sections;
6. Directory Sync and events payload/memory are bounded by pagination;
7. no cache isolation, authorization, or mutation regression is observed.

## Rollout and Documentation

Implementation is tracked by #115 child tickets:

| Stage | Tickets | Outcome |
| --- | --- | --- |
| Contract | #116 | This target design |
| Independent foundations and P0 slices | #117-#122 | Static delivery, runtime/request spine, work items, events, Directory runs, route hydration |
| Usage and team split | #123-#129 | Personal snapshot, versioned scope, summary, trend, members, organization, member detail |
| Remaining page/query slices | #130-#134 | Settings/provider, quota queues, repositories, PR freshness, admin users |
| Telemetry | #135 | Pool/cache metrics and Web Vitals baseline |
| Production verification | #136 | Cold/warm evidence, route budgets, one-release team compatibility proof |
| Contract cleanup | #137 | Remove legacy Team Overview adapter |

Rollout rules:

1. Each slice is independently deployable and updates `docs/architecture.md` only for behavior that landed.
2. New Redis keys are versioned so rollback can ignore newer values safely.
3. The frontend and matching backend API changes ship in the same platform release.
4. Team split uses expand-contract and keeps the legacy adapter for one complete release.
5. CLI release tags and Helm rules remain unchanged; this is platform work, not a CLI-only release.
6. Historical specs are not rewritten. This document records the current target and its relationship to them.

## Deferred Work

1. CDN, separate static domain, Brotli precompression, and HTTP/3.
2. Full distributed tracing/collector rollout and `Server-Timing`.
3. Upstream scoped batch trend or group-multiplier APIs in sub2api.
4. Cross-system event-driven invalidation for changes made directly outside AE.
5. Numerical route budgets before sufficient production samples exist.
