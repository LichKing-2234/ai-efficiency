# Platform Loading Contract

This contract describes the current authoritative-source, reconstructible-read-
model, request deadline, readiness, frontend loading, embedded serving, and
performance-observability boundaries. Read it before changing backend caches,
request middleware, health probes, embedded frontend delivery, page-loading
orchestration, or performance telemetry.

Team Usage has additional [prewarm](./team-usage-prewarm.md) rules. Collection
state and navigation follow the [collection navigation](./collection-navigation.md)
contract. Usage authorization and business semantics remain in the
[usage and quota](./usage-and-quota.md) contract.

## Runtime and Authority

The platform is a modular monolith and one platform release unit. The frontend
is built into the backend binary, and the backend process serves both API and
SPA traffic. Redis and the optional prewarm worker accelerate reads without
creating another business authority or release line.

PostgreSQL remains authoritative for local state and persisted revisions.
Relay, SCM, and Directory facts remain authoritative behind their existing
module/provider boundaries. Authentication, current role, token revocation,
credentials, authorization scope, fresh quota facts, and mutation decisions
are resolved from their authoritative sources.

Redis values are bounded, reconstructible read models. Redis unavailability,
malformed values, or lease failure selects the bounded authoritative path for
that read. It cannot authorize a subject, validate a token, supply a secret,
approve a mutation, or make a failed write appear successful.

## Shared Read-Model Rules

Each read model owns its answer dimensions, freshness, fallback, serialization,
and invalidation policy inside the domain module. Shared `readcache` primitives
provide Redis values, token-protected leases, cancellation-aware sleep, and a
waiter-aware process-local flight; they do not define business freshness.

Every Redis key is versioned and deployment-namespaced, and includes every
dimension that can change the answer. Depending on the model, those dimensions
include provider/configuration version, actor or subject, effective role,
Directory source/run or scope version, range, timezone, parent, sort position,
and normalized page input. Raw queries, cursors, emails, secrets, credentials,
tokens, or serialized values are absent from key names.

Refresh behavior preserves these invariants:

- Identical work in one process shares one bounded flight. One caller leaving
  does not cancel work needed by remaining waiters; the load is canceled when
  no waiter remains or its shared timeout expires.
- Cross-replica refresh ownership uses a random-token Redis lease. Publication
  and release verify the token, so a late owner cannot overwrite or release a
  newer owner's work.
- A lease winner double-checks current cache state before loading. Waiters are
  bounded by their request context and then read the winner's result or use the
  authoritative fallback.
- Redis command failure bypasses distributed coordination. Origin errors are
  not cached as successful values.
- Values are size- and schema-validated before use. A version or authority-guard
  change selects another key or rejects the old value.

A cache may be fresh-only or may define a soft and hard freshness window. A
stale value is returned only when its owning contract permits stale-if-error,
the authoritative refresh fails with an eligible transient error, and the hard
deadline has not passed. Authorization, credentials, current provider version,
invalid input, mutation conflicts, and quota decisions are never stale-eligible.

Platform-owned writes advance the relevant persisted version or revision in the
same transaction as the mutation. Best-effort cache eviction or notification
after commit reduces convergence time; correctness comes from the persisted
version and bounded cache/client lifetime when that notification is missed.

## Request and Dependency Budgets

The first-party browser default is 45 seconds. The server request default is 35
seconds, and the shared downstream HTTP-client overall default is 30 seconds.
Configuration validation preserves the ordering from outer caller to inner
dependency. The server also sets a 5-second read-header timeout and 120-second
idle timeout; downstream transports set bounded connect, TLS handshake,
response-header, overall, and idle-connection limits.

Request cancellation propagates through handlers, domain services, database
queries, Redis waits, and downstream HTTP calls. Background cache work uses an
explicit bounded shared context only while callers still need it. Per-operation
deadlines may be shorter, but an inner operation cannot outlive its enclosing
request budget.

Production router construction is explicit and error-returning. Required
caches, revision stores, Directory services, HTTP clients, telemetry,
readiness, release metadata, request timeout, and Team Usage cursor/prewarm
dependencies must be supplied rather than silently replaced by a production
uncached test path.

## Liveness and Readiness

Liveness reports the process and build version and never probes an external
dependency.

Readiness probes PostgreSQL, Redis, and Relay concurrently under one short
deadline, two seconds by default. The response always reports each check:

- PostgreSQL down produces `not_ready` and HTTP 503.
- PostgreSQL up with Redis or Relay down/not configured produces `degraded` and
  HTTP 200.
- All configured checks up produces `ready` and HTTP 200.

Redis is non-critical because normal cache-backed reads have authoritative
fallbacks. The optional Team Usage worker is not a readiness dependency.

## Frontend Loading

API calls remain in `frontend/src/api`; stores and domain composables own shared
request state, deduplication, and freshness. Views compose those owners and keep
independent data branches independent.

Page loading follows these rules:

- Required identity gates protected content, while independent route chunks and
  allowed data branches start concurrently.
- A slower optional section does not hold back usable sibling content. Personal
  usage/quota and Team Usage summary/trend/members/organization retain separate
  loading, success, empty, stale, and error states.
- Each replaceable request branch has cancellation or a generation guard so an
  older response cannot overwrite a newer range, filter, route, or subject.
- Chart and other heavy modules load behind async boundaries when their data is
  usable. Hidden Settings sections and unopened queues do not issue requests.
- Responsive layouts render one record subtree from one loaded collection.
  Desktop and mobile presentations share data and request state instead of
  mounting duplicate hidden trees.

## Embedded Frontend Serving

The embedded server resolves the requested regular file or the actual
`index.html` SPA fallback before applying representation metadata. It memoizes
raw bytes and lazily builds one gzip representation for HTML, JavaScript, CSS,
JSON, and SVG.

- Gzip negotiation preserves existing `Vary` values and adds
  `Accept-Encoding`.
- Hashed files under `assets/` use
  `public, max-age=31536000, immutable`.
- HTML, SPA fallbacks, OAuth SPA entry responses, and non-hashed files use
  `no-cache`.
- GET and HEAD share content type, content length, encoding, cache policy, and
  fallback behavior.
- Static policy never applies to API or OAuth protocol responses.

The current embedded handler deliberately removes fabricated `Last-Modified`
metadata and does not implement Brotli or ETag validation. Adding another
representation or validator requires matching negotiation, cache, HEAD, and
decompression tests.

## Observability and Privacy

Each request accepts a bounded valid `X-Request-ID` or generates one, returns it
to the caller, and propagates it to instrumented dependency requests. Request
telemetry records normalized route, method, status class, duration, response
bytes, in-flight work, and release. Dependency telemetry records bounded
dependency/operation, method, status/error class, duration, and release.

Metrics separately expose database-pool, Redis-pool, application-cache, request,
dependency, and browser behavior. Cache metrics use a stable cache name and a
closed outcome vocabulary. Browser telemetry accepts only bounded LCP, INP,
CLS, and TTFB samples with normalized route and navigation type.

Metric labels and structured events exclude request IDs as labels, raw paths or
queries, request/response bodies, SQL parameters, user identity, email, cache
keys or values, Relay IDs, credentials, tokens, and unredacted downstream
errors. The metrics endpoint remains inside an authenticated or internal
network boundary.

Performance claims identify the exact release, route, cache state, request and
transfer counts, browser/network conditions, backend/dependency timing, and
fallback state. Route budgets require representative production samples;
missing evidence is reported as insufficient data.
