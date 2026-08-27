# Team Usage Prewarm Contract

This contract describes the current Team Usage prewarm worker lifecycle,
schema-v3 generation publication, authorization-first backend reads, exact
fallback, readiness independence, and operational evidence. Read it before
changing `backend/cmd/prewarmer`, Team Usage prewarm Redis data, backend prewarm
reads, worker Helm resources, or prewarm metrics.

Prewarm is an optional optimization under the
[platform loading](./platform-loading.md) contract. It cannot change Team Usage
authorization or business semantics defined by
[usage and quota](./usage-and-quota.md).

## Topology and Ownership

Backend Pods own HTTP traffic, authentication, current representative scope,
read-only prewarm lookup, and exact fallback. They do not schedule prewarm
source work or publish generations.

One optional worker process owns scheduled provider-wide source reads and Redis
generation publication. It runs from the same image and platform release as the
backend, but as a separate Helm Deployment. Enabling the worker creates exactly
one replica with `Recreate` strategy and dedicated resources. It has no Service,
Ingress, or persistent volume, and backend scaling does not change its replica
count or source-call rate.

The Helm path is the current worker integration. Docker Compose and systemd omit
the worker and continue through exact request-time fallback. Supporting multiple
worker replicas or another deployment path requires a separate contract.

Helm owns the enabled flag. The worker process receives one to four
deduplicated valid IANA timezones; when Helm enables it, invalid or empty
timezone configuration fails bootstrap. The worker uses current database,
Redis, encryption, and Relay-provider configuration; it does not receive
browser traffic or own authorization decisions.

## Process Lifecycle

The command depends on one `Refresher` interface. It runs one refresh
immediately, then attempts another every 60 seconds. Refresh calls are serial;
a slow call is never overlapped by the next local tick. Ordinary source or Redis
failure is a bounded cycle outcome and the loop continues. An unrecoverable
bootstrap error exits the process so its deployment can restart after repair.

Each refresh:

1. Resolves the enabled primary Relay provider, persisted configuration version,
   and provider adapter.
2. Acquires one deployment-scoped Redis lease with a random token.
3. Captures one cycle time and derives the split-safe local anchor for each
   configured timezone.
4. Reads current manifests and plans only source work required for those
   anchors.
5. Fetches the complete provider directory and current stats once, then fetches
   `today_hour` for each eligible timezone and only the missing/hard-invalid
   `history_6d` and `history_29d` segments.
6. Runs Relay source calls through one process-local concurrency limit of two,
   validates results at the adapter boundary, and writes immutable values.
7. Re-resolves the primary provider version and publishes each complete timezone
   manifest atomically while the original lease token is still owned.
8. Releases the lease with token comparison and discards cycle-local state.

The refresh deadline is five minutes and the lease lifetime is six minutes.
Loss of the deadline, lease, or original provider version prevents subsequent
publication. Current-stats failure prevents all new manifests. One timezone's
trend failure preserves that timezone's old manifest while complete sibling
timezones may publish.

The worker keeps no local checkpoint. Restart planning is reconstructed from
the database clock, current provider configuration, authoritative source facts,
and Redis manifests.

## Schema-V3 Publication

Redis stores bounded schema-v3 values:

1. One immutable provider-wide current-stats value per moving generation.
2. Immutable `history_29d` and `history_6d` values per provider version,
   timezone, and local anchor.
3. One immutable `today_hour` value per provider version, timezone, and anchor.
4. One publish-last manifest per provider version, timezone, and anchor.

Keys isolate deployment namespace, schema version, provider ID, persisted
provider version, timezone digest, anchor, segment class, and opaque generation
where applicable. Current stats and segments are bounded zstd JSON; manifests
are bounded plain JSON.

All immutable values are written before their manifest. The manifest is the only
discoverable commit point. One Redis script verifies lease ownership and writes
the manifest atomically. Lost ownership may leave unreachable immutable values
until TTL expiry, but cannot expose a partial generation.

| Value | Fresh | Hard-valid | Redis TTL |
| --- | ---: | ---: | ---: |
| Current stats | 75 seconds | 4 minutes | 6 minutes |
| `today_hour` | 75 seconds | 4 minutes | 6 minutes |
| History segments | 25 hours | 49 hours | 50 hours |
| Manifest | n/a | 3 minutes | 3 minutes |

Schema-v2 values are not migrated or read; their bounded TTLs remove them.

## Authorization-First Read Path

Every backend request preserves this order:

1. Validate normalized range, granularity, and timezone.
2. Resolve the authenticated actor's current representative scope and Relay
   mappings.
3. Resolve the current primary provider and persisted configuration version.
4. Recognize only the standard Today, 7 Days, or 30 Days window.
5. Read the matching schema-v3 manifest and MGET only the required values.
6. Decode and validate the complete generation, intersect its Relay IDs with the
   currently authorized scope, and compose the existing scope origin and typed
   Team Usage lanes.

The reader is read-only: it owns no source-call limiter, lease, repair path, or
manifest writer. Redis data contains usage facts, never representative grants.
Provider-wide source absence means zero only after completeness/truncation
validation. A currently authorized Relay ID missing from the complete
current-stats roster selects exact fallback.

Any ineligible range, cache miss, Redis error, corrupt or incomplete value,
hard-expired reference, provider-version mismatch, scope mismatch, or incomplete
roster selects the same bounded scope-origin path used without prewarm. Request
handlers never repair or publish Redis state.

## Failure and Readiness

- Worker loss leaves existing generations usable only through their documented
  validity; backend requests then use exact fallback.
- Redis loss in the worker publishes nothing. Redis loss in a backend bypasses
  prewarm for that request.
- Directory/current-stats failure publishes no new manifest; a timezone-local
  trend failure affects only that timezone.
- Lease loss, cancellation, or provider-version change forbids later publish.
- Corrupt data records one bounded invalid outcome and uses exact fallback.
- Worker restart runs an immediate stateless refresh.

The worker is not part of liveness or backend readiness. Its outage may increase
latency but cannot make an API response incorrect, unauthorized, unhealthy, or
unready.

## Data and Observability

Prewarm values may contain bounded Relay IDs and usage facts. They exclude names,
usernames, emails, credentials, API keys, JWTs, Directory records,
representative grants, authorization decisions, and raw HTTP responses.

The worker and backend expose bounded refresh outcome/duration, per-configured-
timezone last success, source duration/outcome, and request outcome metrics.
Closed request outcomes are `full_hit`, `miss`, `ineligible`, `invalid`, and
`fallback`; source and refresh outcomes are likewise closed vocabularies.

Timezone is the only configurable prewarm label and is capped by the four-value
configuration bound. Metrics and logs exclude provider/user IDs, Relay IDs,
cache keys, payloads, credentials, and raw downstream errors. One structured
cycle event may include duration and bounded planned/published/source counts.

Correctness evidence covers deterministic dual-refresher lease ownership,
restart reconstruction, publish-last completeness, provider-version change,
hard-expiry/corruption fallback, exact/prewarm semantic equivalence, Helm
topology, and backend readiness independence.
