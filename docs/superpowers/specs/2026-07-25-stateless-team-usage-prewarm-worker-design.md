# Stateless Team Usage Prewarm Worker Design

**Status:** Implemented and staging-verified on 2026-07-25

**Date:** 2026-07-25

**Supersedes:**

- the embedded background lifecycle in
  `docs/superpowers/specs/2026-07-21-team-usage-segmented-timezone-prewarm-design.md`;
- `docs/superpowers/specs/2026-07-23-team-usage-startup-cohort-publication-design.md`;
- `docs/superpowers/specs/2026-07-23-team-usage-replay-guardian-design.md`; and
- the merge-ready decision in
  `docs/superpowers/specs/2026-07-24-team-usage-prewarm-closeout-design.md`.

**Retains from the 2026-07-21 segmented design:**

- exact timezone-specific Sub2API source semantics;
- the independent `history_29d`, `history_6d`, and `today_hour` segments;
- provider-wide current usage stats shared across timezone lanes;
- schema-bounded zstd values and publish-last manifests;
- current provider-version and representative-scope authorization on every
  request; and
- PR #192's exact scope-origin fallback.

The superseded documents never merged to the integration branch and have been
removed from the final PR diff. This design, its implementation plan, and short
sanitized acceptance evidence retain the necessary source semantics. One-off
replay procedures and execution ledgers do not belong in the final product
contract.

## Context

Before this implementation, PR #193 embedded a Team Usage prewarm scheduler in
every AI Efficiency backend process. Each backend replica constructed its own
startup lifecycle and 60-second ticker, then coordinated startup, moving,
recovery, historical, source-slot, segment, and publication work through several
Redis leases. The feature was disabled by default, and multi-replica enablement
was blocked because a startup lease loser could begin scheduled work before the
owner finished.

That pre-implementation design coupled web-replica count to background scheduler
count. It also required every stateless backend Pod to own process-local
lifecycle state even though the optimization's durable state belonged in Redis.
The resulting coordination and observability implementation was larger than the
retained cache and authorization problem, and its complexity had already caused
metric-vocabulary and partial-success reporting defects.

AI Efficiency backend Pods must remain horizontally scalable and stateless.
Adding or removing a web replica must not create, remove, or reschedule prewarm
work. A background optimization may fail independently without changing API
availability or readiness.

## Goals

1. Keep every AI Efficiency backend Pod stateless with respect to prewarm
   scheduling, generation, and recovery.
2. Run prewarm in one independently deployed worker process built from the same
   repository, image, version, and release unit as the backend.
3. Keep all reconstructible generation and coordination state in Redis.
4. Replace the startup, moving, recovery, and historical lifecycle machines
   with one serial refresh cycle and one deployment-wide Redis lease.
5. Preserve exact today, 7-day, and 30-day Team Usage results for supported
   timezone lanes without changing Sub2API.
6. Keep request-time provider resolution, representative authorization, Relay
   identity mapping, and PR #192 fallback authoritative.
7. Let worker failure degrade only cache availability and page latency, never
   API correctness, health, or authorization.
8. Reduce production interfaces, metric vocabularies, tests, and permanent
   documentation to the behavior that the enabled runtime actually needs.

## Non-Goals

- Do not create a separately versioned service, repository, image, or release
  line.
- Do not introduce a message queue, Kubernetes CronJob, StatefulSet, or
  multi-replica worker deployment.
- Do not keep an embedded scheduler in backend Pods as a fallback worker.
- Do not cache a completed Team Usage result in Pod memory.
- Do not make Redis authoritative for authentication, authorization, provider
  configuration, quota decisions, or mutations.
- Do not perform request-time prewarm repair or write prewarm generations from
  backend request handlers.
- Do not change frontend routes, request DTOs, response DTOs, cursors, or
  Sub2API source code.
- Do not treat the representative staging merge gate as a production latency
  SLO.
- Do not enable the worker in production as part of the implementation PR.

## Deployment Architecture

AI Efficiency remains one modular-monolith codebase and one release unit. The
container image carries two process entrypoints:

```text
/app/ai-efficiency-server
/app/ai-efficiency-prewarmer
```

The runtime topology is:

```text
AI Efficiency Backend Deployment
  replicas: N
  process: ai-efficiency-server
  owns: HTTP, authentication, authorization, Redis reads, exact fallback
  does not own: prewarm scheduler, source work, generation writes

AI Efficiency Prewarmer Deployment
  replicas: exactly 1
  process: ai-efficiency-prewarmer
  owns: scheduled provider-wide source reads and Redis generation publication
  does not own: HTTP user traffic, authorization decisions, API readiness
```

Both process types use the same image tag and are published by the same platform
release. This is a standard web/worker process split, not a microservice split.

The Helm interface is intentionally small:

```yaml
prewarmer:
  enabled: false
  timezones:
    - UTC
    - Asia/Shanghai
    - America/Los_Angeles
    - Europe/Berlin
  resources: {}
```

`enabled` defaults to `false`. The timezone list is passed only to the worker,
is normalized with the retained segmented-timezone rules, and contains at most
four valid IANA names after deduplication. Invalid or empty enabled-worker
configuration makes the worker exit before source work; it never silently runs
a partial allowlist. Backend Pods receive no prewarm enablement, timezone, or
scheduler configuration.

The worker Deployment has no Service, Ingress, or persistent volume. It uses a
`Recreate` rollout strategy. Kubernetes restarts the process after an exit.
Backend HPA and manual backend scaling do not alter the worker replica count.
The worker replica count is fixed by the chart rather than exposed as a normal
operator setting. Supporting an active-active or standby worker fleet requires
a later design.

The first delivery integrates the worker only through the Helm deployment path.
Docker Compose and systemd installations omit the worker and retain the exact
fallback. The shared image may be used to add an explicit second process there
later without changing the worker contract.

The worker needs only the current database, Redis, encryption, and Relay
configuration required to resolve the primary provider and call its HTTP
adapter. It does not need user-authentication, OAuth, frontend, webhook, SCM, or
other backend runtime initialization.

## Statelessness Contract

Neither process type stores prewarm correctness state on local disk or in a
Pod-local completed-result cache.

The backend may hold ordinary request-local values and existing in-flight
singleflight coordination, but no backend-local value determines whether a
prewarm generation exists or is current. Every backend replica derives that
answer from the current database/provider version and Redis.

The worker may hold only one cycle's temporary decoded values, work queue,
local semaphore, cancellation context, and random lease token. All are released
when `Refresh` returns. A restart reconstructs the complete next cycle from the
database clock, provider configuration, and Redis manifests. No local checkpoint
or startup-completion marker exists.

## Worker Module

The worker command depends on one deep Team Usage prewarm module. Its external
interface is intentionally small:

```go
type Refresher interface {
	Refresh(context.Context) error
}
```

Provider resolution, refresh planning, source selection, bounded concurrency,
validation, immutable writes, manifest construction, publication, and metrics
remain inside the implementation. The command owns process signals and the
fixed schedule; callers and tests exercise the same `Refresh` interface.

The command runs one refresh immediately, then attempts one refresh every 60
seconds. Calls are serial in one process. A slow call is never overlapped by the
next local tick. A transient refresh error is recorded and the loop continues;
a bootstrap error that makes the process incapable of ever running causes the
process to exit so Kubernetes can restart it after configuration is corrected.

## Refresh Cycle

Every call to `Refresh` performs these steps:

1. Resolve the current enabled primary Relay provider row, its persisted
   `configuration_version`, and its Relay adapter.
2. Acquire one deployment-scoped Redis refresh lease with a random token. The
   lease is independent of backend replica count and covers the entire cycle.
3. Capture one cycle time and derive the current split-safe local anchor for
   every configured timezone.
4. Read the current per-timezone manifests and plan only the source work needed
   for those anchors.
5. Fetch the complete provider directory and provider-wide current stats once
   for the cycle.
6. Fetch `today_hour` for every eligible timezone. Fetch `history_6d` and
   `history_29d` only when the current anchor lacks a hard-valid reference.
7. Run source calls through one process-local semaphore of size two. There is no
   Redis source-slot semaphore because the refresh lease already selects one
   deployment-wide source owner.
8. Validate source results once at the Relay adapter seam and construct bounded
   domain values. Encode and write immutable values to Redis.
9. Build one complete manifest candidate per timezone from the new current and
   today references plus new or retained history references.
10. Re-resolve the primary provider version and atomically publish each complete
    timezone manifest only while the original refresh lease token is still
    owned.
11. Release the refresh lease with token comparison and discard all cycle-local
    data.

The refresh call has a five-minute overall deadline. The Redis lease lasts six
minutes, preserving a publication margin. Individual Relay calls keep their
bounded deadline. If the cycle deadline or lease expires, no later manifest may
publish. These values bound failure; they are not latency goals.

Current-stats failure prevents every new manifest because all lanes require the
same authoritative roster. A trend failure affects only its timezone lane.
Successful lanes may publish independently. There is no deployment-wide
four-timezone cohort or startup-specific publication barrier because one Team
Usage request consumes exactly one timezone lane.

## Redis Schema

The simplified runtime uses schema version 3. It does not migrate or read
schema-v2 values. Existing schema-v2 keys expire under their bounded TTLs.

Redis stores:

1. one immutable provider-wide current-stats value per moving generation;
2. one immutable `history_29d` value per provider/version/timezone/anchor;
3. one immutable `history_6d` value per provider/version/timezone/anchor;
4. one immutable `today_hour` value per provider/version/timezone/anchor; and
5. one publish-last manifest per provider/version/timezone/anchor.

Keys remain isolated by deployment namespace, schema version, provider ID,
provider configuration version, timezone digest, anchor date, segment class,
and opaque generation ID where applicable. Redis values contain only bounded
Relay IDs and usage facts. They never contain names, usernames, emails,
credentials, API keys, JWTs, Directory records, representative grants,
authorization decisions, or raw HTTP responses.

Current-stats and segment values remain bounded zstd-encoded JSON. Manifests
remain bounded plain JSON. The worker writes every immutable value before its
manifest. A manifest is the only discoverable commit point.

Manifest publication uses one Redis script that verifies the refresh lease
token and writes the manifest in the same command. It does not require segment,
source-slot, moving, historical, recovery, or startup lease claims. A lost lease
may leave unreachable immutable values, which expire normally, but cannot expose
a partial generation.

The retained freshness contract is:

| Value | Fresh | Hard-valid | Redis TTL |
| --- | ---: | ---: | ---: |
| current stats | 75 seconds | 4 minutes | 6 minutes |
| `today_hour` | 75 seconds | 4 minutes | 6 minutes |
| history segments | 25 hours | 49 hours | 50 hours |
| manifest | n/a | 3 minutes | 3 minutes |

The worker runs every minute, so current stats and `today_hour` normally advance
before their fresh deadline. History references survive one missed local-day
refresh and are replaced by the next anchor. A manifest never references a
value whose hard-valid interval cannot cover the manifest lifetime.

## Backend Read Path

Backend requests preserve the current authorization-first order:

1. validate the normalized range, granularity, and timezone;
2. resolve the authenticated actor's current representative scope and Relay
   mappings;
3. resolve the current primary provider ID and persisted version;
4. recognize only the standard today, 7-day, or 30-day window;
5. read the matching schema-v3 timezone/anchor manifest;
6. MGET only current stats and the segment references required by the requested
   window;
7. decode and validate those values;
8. intersect provider-wide usage facts with the currently authorized Relay
   IDs; and
9. compose the existing scope origin and typed response lanes.

The reader is read-only. It has no source-call limiter, Redis lease writer,
request-time `today_hour` recovery, manifest writer, or mutable reader slot. A
missing, stale beyond the hard deadline, corrupt, incomplete, ineligible, or
provider-version-mismatched generation immediately selects PR #192's exact
scope-origin fallback.

Provider-wide source absence means zero contribution only after the stored
source passed its completeness and truncation checks. An authorized Relay ID
missing from the complete current-stats roster always selects the exact
fallback. Redis never replaces current authorization.

## Failure Semantics

- **Worker unavailable:** existing manifests remain usable only through their
  documented validity. Requests then use exact fallback.
- **Redis unavailable to the worker:** the cycle publishes nothing and retries
  on a later tick.
- **Redis unavailable to a backend:** that request bypasses prewarm and uses
  exact fallback.
- **Directory or current-stats failure:** no new timezone manifest publishes.
- **One timezone source failure:** that timezone retains its old manifest;
  other complete timezones may publish.
- **Provider version change:** the cycle is discarded before publication and
  the next cycle resolves the new provider version.
- **Lease loss or cycle cancellation:** all later publication is forbidden;
  unreachable immutable values expire.
- **Corrupt value or manifest:** the reader records one bounded invalid outcome
  and uses exact fallback. It does not repair Redis from the request path.
- **Worker Pod deletion or restart:** the replacement runs an immediate cycle
  from Redis and authoritative provider state.

The worker is not part of backend readiness. A worker outage must not make the
API unready or unhealthy. The worker process exits on an unrecoverable bootstrap
failure; ordinary source and Redis failures remain bounded cycle outcomes.

## Observability

The worker and backend expose only the metrics needed to operate this design:

```text
ai_efficiency_team_usage_prewarm_refresh_total{outcome}
ai_efficiency_team_usage_prewarm_refresh_duration_seconds
ai_efficiency_team_usage_prewarm_lane_last_success_timestamp_seconds{timezone}
ai_efficiency_team_usage_prewarm_source_duration_seconds{source,outcome}
ai_efficiency_team_usage_prewarm_request_total{outcome}
```

Closed values are:

- refresh outcome: `success`, `partial`, `skipped`, `error`;
- source: `directory`, `current_stats`, `today_hour`, `history_6d`,
  `history_29d`;
- source outcome: `success`, `error`, `canceled`, `rejected`; and
- request outcome: `full_hit`, `miss`, `ineligible`, `invalid`, `fallback`.

Timezone is the only configured label and remains bounded by the maximum-four
allowlist. The implementation has one typed source for each closed vocabulary;
it does not duplicate string allowlists across domain, telemetry, and logging.

Existing Redis pool metrics remain authoritative for pool behavior. The new
design does not expose per-lease TTL/release metrics, scheduler ticks, startup
cohorts, validation substeps, generation quantity histograms, provider IDs,
user IDs, cache keys, credentials, or raw errors as metric labels.

One structured cycle log may include bounded outcome, duration, planned and
published lane counts, and source class counts. It never includes users, Relay
IDs, keys, payloads, credentials, or unredacted downstream response text.

## Security And Data Handling

The worker reuses the existing Relay Provider HTTP adapter and encrypted
provider configuration. It does not access Sub2API storage directly. It reads
the minimum database records needed to resolve the current provider and version.

Redis contains reconstructible usage read models only. Backend requests perform
current authentication, token-revocation, role, representative scope, Directory
run, provider-version, and Relay-mapping checks before using them. No cached
value authorizes a user or mutation.

Tests, fixtures, plans, logs, and acceptance evidence use synthetic identities
and sanitized aggregates. No real account, email, password, token, API key,
group name, or unredacted response body enters the repository.

## Testing

### Unit And Package Tests

1. Preserve the approved split-safety, opaque source-label, truncation,
   overflow, token-presence, and exact composition tests.
2. Prove a refresh fetches current stats and `today_hour` each moving cycle but
   reuses hard-valid history for the same anchor.
3. Prove a new local anchor fetches both independent history segments.
4. Prove one timezone failure leaves its old manifest while other complete
   lanes publish.
5. Prove current-stats failure publishes no new manifests.
6. Run two `Refresher` instances against one Redis adapter and prove exactly one
   performs source work under lease contention.
7. Prove lease expiry, token replacement, cancellation, and provider-version
   change prevent publication.
8. Prove a worker restart derives all planning decisions from Redis rather than
   process state.
9. Prove the backend reader never calls a prewarm source or writes Redis.
10. Prove missing, invalid, corrupt, expired, unsupported-window, and roster-
    incomplete generations select exact fallback.
11. Prove every prewarm hit filters through the currently authorized Relay ID
    set and current provider version.
12. Run focused tests, `go test -race` for the affected packages, the full
    backend suite, `go vet ./...`, and `go build ./...`.

The deterministic two-refresher test replaces a two-worker-Pod staging gate.
The production topology deliberately supports one worker replica. Future
multi-replica worker support requires a new contract and staging acceptance.

### Deployment Tests

Helm rendering must prove:

- backend replica count remains independently configurable;
- enabling the worker creates exactly one worker Deployment;
- backend Pods contain no prewarm enablement or scheduler configuration;
- worker and backend use the exact same image tag;
- the worker has `Recreate` strategy and separate configurable resources;
- the worker has no Service, Ingress, or persistent volume; and
- disabling the worker removes only the worker Deployment.

Staging runs with two backend replicas and one worker replica. Scaling the
backend from one to two must not change worker cycle or Relay source counts.
Deleting the worker Pod must create a replacement that refreshes successfully
without local recovery data. The final staging state is explicitly recorded and
does not modify production.

### Performance Acceptance

Use installed Google Chrome, a fresh profile per run, the default 30-day
Asia/Shanghai window, cold typed response caches, and a valid Redis prewarm
generation. Run three comparable page navigations and retain all three results.

The representative merge gate is:

- HTTP 200 and matching business hashes for each Chrome cold response and its
  immediate warm response from the same typed response generation;
- four response-cache misses and four prewarm full hits;
- median fully rendered completion at or below eight seconds; and
- every immediate warm API lane below 1.5 seconds.

The manifest-expiry live fallback check uses a sampling-aware semantic equality
contract. Exact fallback and the next restored prewarm generation necessarily
read an active provider at different times, so strict whole-response hash
equality is invalid for a large active team. The two responses must have exact
shape and cardinality, and must match exactly for HTTP/API code,
`scope_version`, every window field, `member_count`, `relay_member_count`,
unavailable status and reason, and `unit_label`. Only these four
current/today-derived leaves may differ because of source sampling time:
`range_actual_cost`, `range_total_tokens`, `today_actual_cost`, and
`total_actual_cost`. Any other changed or missing field, shape difference, or
cardinality difference fails the live gate.

This live contract does not relax composer correctness. The deterministic
same-source automated equivalence test continues to require strict full JSON
equality between exact fallback and prewarm composition, including response
shape, freshness, cursors, and cache dimensions.

This is a staging merge gate, not a production p95 SLO. No replay guardian,
multi-Pod startup cohort, or five-second manifest-spread gate is required.
Rollback disables the worker workload through the normal Helm path; backend
requests continue through exact fallback.

## Implementation Scope And Deletions

Implementation must replace the current branch design rather than layer the
worker on top of it.

Retain and simplify:

- provider-wide Relay capability interfaces and bounded Sub2API HTTP adapters;
- timezone/window domain recognition and exact composition;
- zstd value encoding with bounded decoding;
- immutable Redis values, selected-reference MGET, and one publish-last
  manifest per timezone;
- the read-only authorized prewarm reader; and
- configuration needed by the worker process.

Remove or replace:

- the `teamUsagePrewarmRuntime` lifecycle in `cmd/server`;
- backend-owned prewarmer startup and shutdown;
- startup, moving, recovery, and historical coordinator state machines;
- segment leases, source-slot leases, multi-lease publication, lease-TTL reads,
  startup lane plans, and startup cohort publication;
- request-time partial-today recovery and its local flight;
- mutable prewarm reader slots;
- duplicated metric allowlists and the current large prewarm dashboard section;
- backend prewarm environment entries in Compose examples; and
- unmerged daily-prewarm, startup-cohort, replay-guardian, and execution-ledger
  documents that no longer describe the final feature.

`docs/architecture.md` must describe the optional worker Deployment, schema-v3
Redis data flow, read-only backend integration, and fallback. The current spec
must explicitly supersede the old embedded lifecycle contract without rewriting
unrelated historical specs that remain in the repository.

PR #193 returns to draft while this replacement is implemented and reviewed.
Issue #194's embedded multi-Pod scheduler problem becomes obsolete when backend
Pods no longer run schedulers. It may close as superseded after the worker design
is implemented and verified. Any future request for multiple worker replicas is
a new design problem rather than a continuation of the embedded scheduler.

## Acceptance Criteria

1. Backend Pods own no prewarm scheduler, source limiter, generation writer,
   recovery flow, or durable prewarm state.
2. One optional worker Deployment uses the same image and release as the
   backend and has no public traffic surface or persistent volume.
3. All prewarm correctness and coordination state is reconstructible from the
   database/provider version and Redis.
4. One refresh lease and one local two-slot source pool replace the current
   distributed lifecycle and source-slot coordination.
5. Each timezone manifest is independently atomic and never exposes partial or
   provider-version-stale data.
6. Backend reads remain authorization-first and read-only, with exact fallback
   for every unavailable or invalid prewarm case.
7. Worker failure never affects backend readiness or correctness.
8. Metrics use the five approved families and one typed closed vocabulary.
9. Focused, race, full-backend, vet, build, Helm, stateless-restart, and staging
   acceptance tests pass.
10. Three comparable Chrome cold runs meet the representative median gate and
    warm-lane requirement with matching same-generation business hashes; the
    manifest-expiry replay meets the sampling-aware live semantic equality
    contract while same-source automated equivalence remains strict full JSON.
11. The final PR removes superseded implementation and operational artifacts
    instead of adding another lifecycle layer.
