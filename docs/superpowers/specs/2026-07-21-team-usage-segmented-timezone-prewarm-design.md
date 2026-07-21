# Team Usage Segmented Timezone Prewarm Design

**Status:** Approved design. Implementation has not started and is blocked on
the segmented-source POC hard gate in this document.

**Date:** 2026-07-21

**Replaces:**

- `docs/superpowers/specs/2026-07-21-team-usage-redis-daily-prewarm-design.md`

**Retained runtime baseline:** PR #192, exact head `627a7123`, including its
scope-origin fallback, response caches, DTOs, cursors, authorization checks,
and bounded aggregate-trend behavior.

This design refines the failed UTC-hour candidate. It is the approved contract
for the next experiment, but it does not describe the current production
runtime. Production remains on the PR #192 behavior until the POC passes, an
implementation is separately reviewed, staging acceptance passes, and a
production release is explicitly approved.

## Decision Summary

Prewarm three exact provider-wide source segments for each of at most four
configured IANA timezones. Preserve every Sub2API date or hour label as a source
label. Never reinterpret a label as UTC, attach a timezone to it, or convert it
between timezones.

For one timezone and local anchor date `D`, the three source segments are:

| Segment | `start_date` | `end_date` | Granularity | Purpose |
| --- | --- | --- | --- | --- |
| `history_29d` | `D-29` | `D-1` | `day` | Completed part of the 30-day lane |
| `history_6d` | `D-6` | `D-1` | `day` | Completed part of the 7-day lane |
| `today_hour` | `D` | `D` | `hour` | Moving today lane and moving edge of 7d/30d |

All dates in this table are calendar arithmetic in the configured IANA
timezone. `D-N` means `AddDate(0, 0, -N)` from local date `D`, not subtraction of
`N * 24h` and not UTC arithmetic. Each Relay request carries the exact
configured timezone.

At the AI Efficiency segmentation boundary, history ends at `D-1` and today
starts at `D`. The implementation may compose them only when the split-safety
guard proves that those calendar requests are also contiguous under current
Sub2API range semantics:

```text
ParseInUserLocation(D-1, timezone) + 24h
    == ParseInUserLocation(D, timezone)
```

When this instant equality holds, the history and today source query ranges are
disjoint and their union is the exact standard 7-day or 30-day source window
ending at `D`. When the previous local date is a 23-hour or 25-hour DST date,
the equality fails. That entire anchor is ineligible for segmented prewarm and
uses PR #192's exact fallback without reading segmented values. The hard POC
must still prove that split-safe Sub2API output composes to its direct
full-window output.

The `today` frontend lane remains `D..D` with `granularity=hour` and uses the
validated `today_hour` response directly. The `7d` and `30d` lanes remain
`granularity=day`. For those lanes only, AI Efficiency coalesces `today_hour`
points by the first 10-character source date prefix and adds them to the
appropriate history segment by Relay user ID plus that source label:

- `7d = history_6d + coalesce(today_hour)`;
- `30d = history_29d + coalesce(today_hour)`.

For an eligible split-safe anchor, this intentionally allows one history point
and today's coalesced point to share a source date label. Their source query
intervals are disjoint even when Sub2API's grouping label straddles that logical
boundary; their values are added, not treated as duplicates. `history_6d` is
fetched independently and is never derived by slicing `history_29d`, because
Sub2API applies its global top-user selection independently to each requested
range.

## Problem

PR #192 removes the old per-user trend fan-out and retains one bounded shared
scope origin, but a genuinely cold Team Usage page still waits for a serial
Relay chain:

1. Relay admin user-directory pages;
2. batch current usage stats; and
3. one aggregate `users-trend` request for the complete requested range.

The rejected design tried to reuse one UTC-hour history across every browser
timezone. Its POC passed capacity gates but failed direct daily equivalence for
UTC, America/Los_Angeles, and Europe/Berlin. Sub2API's `parseTimeRange` applies
the request timezone to query bounds, while `GetUserUsageTrend` groups with
`TO_CHAR(u.created_at, format)` without applying that request timezone. The
returned label is therefore not a timezone-neutral instant.

This replacement keeps the source output in the exact timezone-specific query
lane that produced it. It accepts bounded duplication across four configured
timezones to remove the standard-window Relay chain from the request path
without changing Sub2API.

## Goals

1. Prewarm current Sub2API output for the standard today, 7-day, and 30-day
   Team Usage windows in a bounded configured timezone set.
2. Preserve exact source-label semantics and direct-range equivalence without
   cross-timezone reconstruction.
3. Keep current representative authorization, Directory Sync scope, Relay
   identity mapping, and provider version authoritative on every request.
4. Share one provider-wide current-stats snapshot across all timezone lanes.
5. Refresh only the moving edge every minute and correct historical segments
   once per local day.
6. Preserve PR #192's exact scope-origin path for every ineligible, invalid, or
   unavailable prewarm case.
7. Keep Redis optional, immutable, bounded, publish-last, and free of identity
   and credential material.

## Non-Goals

- Do not modify Sub2API or require a coordinated Sub2API release.
- Do not claim to correct Sub2API timezone bucketing or label generation.
- Do not derive one configured timezone from another.
- Do not parse a source label into an instant or convert it through UTC.
- Do not prewarm arbitrary ranges or non-standard granularity combinations.
- Do not change frontend controls, response DTOs, cache keys, cursor contracts,
  freshness metadata, or stale-if-error behavior.
- Do not cache a completed Team Usage result in Pod memory. Process-local state
  may coordinate an in-flight operation only.
- Do not cache names, usernames, emails, Directory records, credentials, raw
  responses, raw usage logs, JWTs, authorization decisions, or token-revocation
  decisions in prewarm values.
- Do not make Redis authoritative for authorization or usage.
- Do not enable the feature in production as part of design or POC work.

## Configuration Contract

The feature has a deployment flag that defaults to `false`. The initial
configured IANA timezone allowlist is exactly:

```text
UTC
Asia/Shanghai
America/Los_Angeles
Europe/Berlin
```

The allowlist is operator configurable. Startup normalization trims entries,
validates each with the runtime IANA timezone database, treats the validated
trimmed name as the canonical configured name, preserves first occurrence
order, and deduplicates equal canonical configured names. The maximum after
deduplication is four, and one configured name is at most 255 bytes. Distinct
valid IANA aliases are not silently folded together.

An empty normalized list or a disabled feature flag means no prewarmer and no
prewarm reader. An invalid timezone or more than four deduplicated entries
rejects the optimization configuration, records a bounded error, and leaves the
HTTP service on PR #192's exact fallback rather than starting a partial or
silently different timezone set.

Request eligibility requires an exact match against one normalized configured
timezone. Aliases are not substituted on the request path. The configuration
digest and each timezone digest are SHA-256 values over canonical,
length-delimited strings; raw timezone names do not need to appear in Redis
keys.

## Source Semantics

### Source Labels Are Opaque

The source label is the `date` string returned by Sub2API. AI Efficiency may
validate its shape, compare it lexicographically within one granularity, copy
it, and take its first 10 characters for the defined hour-to-day coalescing. It
must not call `time.Parse` with a timezone, attach an offset, reinterpret it as
UTC, or shift it.

A valid day label is exactly `YYYY-MM-DD`. A valid hour label starts with an
exact `YYYY-MM-DD` date prefix and follows the current Sub2API hour shape
`YYYY-MM-DD HH:MM`; validation does not give that text timezone meaning. The
coalesced daily label is exactly the first 10 characters of that hour label.

The design caches current Sub2API output semantics. Passing a timezone to the
source request only selects the source query lane and its range bounds; this
design does not assert that a returned label is expressed in that timezone.

### Point Validation

Each segment response is rejected before Redis publication when any of these
conditions holds:

- Relay user ID is not positive;
- the label is blank or does not match the segment granularity's exact shape;
- tokens are present and negative;
- actual cost is absent, negative, NaN, or infinite;
- the same Relay user ID plus exact source label appears twice in one segment;
- labels for one user are not strictly increasing in source order;
- the response envelope's normalized start date, end date, or granularity does
  not match the requested segment contract, or the locally recorded request
  timezone does not match the segment's configured timezone dimension;
- the manifest's declared segment class, anchor, or coverage does not match the
  immutable value; or
- a decoded point, user, byte, or generation bound is exceeded.

Source order is checked before any sorting. A caller cannot make an invalid
response valid by reordering it locally. Separate segment responses may contain
the same user and source label because the approved composition explicitly adds
contributions across disjoint source requests.

### Truncation Guards

Each aggregate request continues to use Sub2API's maximum `limit=5000`. A
source response containing exactly 5,000 unique users is rejected as possibly
truncated. A response above the requested bound is invalid as well.

For each projected standard window, the unique-user union is computed before
authorization filtering. The 7-day union is `history_6d` plus `today_hour`; the
30-day union is `history_29d` plus `today_hour`. A composed union at or above
5,000 unique users is rejected. Authorization filtering must not hide source
truncation.

### Optional Token Semantics

Actual costs add arithmetically. Token presence remains optional:

- coalescing several hour points produces a token total only when every
  contributing hour has a token value;
- merging a history contribution with a coalesced today contribution produces
  a token total only when every present contribution has a token value;
- absence of a point in a complete, non-truncated source is zero contribution,
  not an unknown token value; and
- one explicit nil token contribution makes the composed token value nil.

The POC and tests compare token presence as well as value. Two nil values are
equivalent; nil is never converted to zero to make a comparison pass.

Actual-cost equivalence is accepted only when the absolute difference is at
most `1e-9`. Relative error, rounded display equality, and aggregate-only
equality do not satisfy the gate.

### Provider-Wide Source Completeness

The implementation adds a `ProviderWideTeamTrendProvider` capability behind
`backend/internal/relay.Provider` for prewarm source reads. Its
`GetProviderUsageTrend` method accepts only the exact
range/granularity/timezone tuple and limit; it does not accept requested Relay
IDs. It returns a `ProviderWideTrendResult` containing all decoded source rows
before authorization filtering together with:

- exact normalized source coverage;
- source response bytes;
- decoded point count;
- unique source user count; and
- an explicit completeness result derived from the 5,000-user truncation guard.

"Raw provider-wide rows" means the complete decoded trend rows needed by this
domain, not raw HTTP bytes and not rows already filtered to a representative
scope. The returned row type contains only Relay user ID, source label, optional
tokens, and actual cost. Source validation, ordering, duplicate detection,
response/point bounds, and truncation checks all run on that provider-wide result
before any authorization intersection or Redis publication. The existing
requested-ID `TeamMemberTrendProvider.GetUsageTrendForUsers` capability remains
the PR #192 fallback contract and is not a prewarm publication source.

The provider-wide directory source uses this fixed request shape for every page:

```text
/api/v1/admin/users?page=<page>&page_size=1000&include_subscriptions=false&sort_by=id&sort_order=asc
```

All five query values are mandatory; the implementation must not depend on
Sub2API defaults. It follows authoritative pagination until every declared page
is exhausted. It rejects:

- a non-positive or duplicate Relay ID on one page or across pages;
- missing or non-positive authoritative total-page metadata for a nonempty
  result;
- a response page number that differs from the requested page;
- a page size other than the requested 1,000 when that metadata is present;
- changing total-count or total-page metadata across pages;
- IDs that are not strictly ascending within a page or whose first ID is not
  greater than the preceding page's final ID;
- an empty nonterminal page, a page longer than 1,000, an undeclared extra page,
  or a final cumulative count inconsistent with authoritative metadata; and
- a directory containing 5,000 or more unique provider IDs.

Fewer than 5,000 positive unique provider IDs is required for prewarm. Reaching
the bound, invalid pagination, or incomplete pagination uses exact fallback and
does not publish a partial provider generation.

Before JSON decoding, every directory page body is subject to the POC-locked
hard cap of strictly less than 16 MiB. A limited read that reaches 16 MiB rejects
prewarm and uses exact fallback; it is never decoded as a partial page.

Current stats are fetched for that complete directory set in chunks of at most
500 positive unique IDs. Before JSON decoding, each stats chunk body is subject
to the POC-locked hard cap of strictly less than 2 MiB. A limited read that
reaches 2 MiB rejects the entire provider generation and uses exact fallback.
Each decoded chunk must contain exactly one record for every requested ID and no
extra, missing, duplicate, zero, or negative ID. The decoder rejects duplicate
JSON object keys before map conversion, and each record's embedded `UserID` must
equal its decoded object key and one requested ID. Every `TodayActualCost` and
`TotalActualCost` must be finite and non-negative; optional `TotalTokens` must be
non-negative. Chunk results must remain disjoint when combined. Any violation
rejects the provider-wide current-stats generation.

The trend capability publishes no requested-ID-filtered map. Its validated raw
rows remain provider-wide in the immutable segment, and authorization filtering
happens only on a reader. The complete directory roster and current-stats keys
must be the same positive unique ID set. The current-stats envelope records that
roster's count and deterministic digest so a reader can validate the equality.
If a currently authorized Relay ID is absent from this complete
directory/current-stats roster, the request uses PR #192's exact scope-origin
path and never synthesizes a stats record.

A complete, non-truncated provider-wide trend result is sparse usage data, not
an identity roster. Absence of a directory/current-stats user from one trend
segment, or from every segment in a standard window, means that user's
contribution for the absent source interval is zero. It does not trigger
fallback and does not require a synthetic trend row. This zero interpretation
is allowed only after provider-wide response, point, ordering, duplicate,
coverage, and truncation validation succeeds.

## Redis Contract

### Immutable Values

Redis stores immutable envelopes for:

1. one provider-wide current-stats snapshot;
2. `history_29d` for one timezone and anchor;
3. `history_6d` for one timezone and anchor; and
4. `today_hour` for one timezone and anchor.

The current-stats snapshot is fetched once per provider moving generation and
shared by every configured timezone manifest. It contains only Relay user ID,
today actual cost, default rolling-30-day `TotalActualCost`, and optional total
tokens from the validated `TeamUserUsageStats` facts. `TotalActualCost` is not a
lifetime value under the current upstream batch endpoint. `RangeActualCost` and
`RangeTotalTokens` are not serialized into this shared value. Timezone-specific
selected-window range totals come from the three trend segments, not from
duplicating current stats per timezone. Its roster count and digest prove that
its key set exactly matches the complete directory ID roster.

Trend values contain only provider/version metadata, timezone digest, anchor,
segment class, exact requested coverage, source generation time, validated
Relay user IDs, source labels, optional tokens, actual cost, and bounded size
metadata. Relay directory responses are discarded after the ID list has driven
current-stat requests.

No prewarm value contains a name, username, email, credential, API key, JWT,
raw Relay response, Directory record, representative assignment, authorized
scope, or authorization decision.

### Keys And Isolation

Every value and manifest key is namespace and schema version isolated. Segment
keys bind provider ID, persisted provider configuration version, timezone
digest, local anchor date, segment class, and an opaque generation ID. Current
stats bind provider ID, persisted provider version, and their own generation ID.
Digests and generation IDs are 64 characters, and any Redis key reference
serialized into an envelope is at most 512 bytes. An over-width dimension
rejects the optimization and uses exact fallback.

Provider version changes make all prior values unreachable. One timezone cannot
address another timezone's values. A local-date rollover creates a new anchor
rather than mutating the prior anchor.

### Publish-Last Manifest

There is one manifest per namespace, schema version, provider ID, provider
configuration version, timezone digest, and anchor date. It records:

- the exact canonical timezone and its digest;
- the anchor date and the three exact segment coverage tuples;
- immutable keys and validation metadata for current stats, `history_29d`,
  `history_6d`, and `today_hour`;
- creation, freshness, and hard-expiry timestamps by class; and
- bounded source and serialized size metadata.

Writers persist and validate every referenced immutable value before publishing
the manifest. The manifest is the only commit point. A lost lease or failed
write can leave unreachable immutable values, but cannot expose a partial
generation.

Readers resolve one manifest and never mix it with another manifest's segment
references. A newer moving generation may reuse unchanged historical values
only when every provider, version, timezone, anchor, class, coverage, schema,
and validation dimension matches.

TTL relationships come from schedule and correctness requirements, not the 20
GET source POC. A today value's hard lifetime must cover more than one 60-second
moving interval plus one bounded recovery opportunity. A historical value's
hard lifetime must cover more than one local-day interval, its maximum 30-minute
jitter, and one bounded recovery opportunity. A manifest cannot outlive its
earliest referenced value. Every value expiry must also exceed the manifest's
maximum discoverable lifetime plus the maximum bounded request duration. At
local-date rollover, prior-anchor values remain readable long enough for any
request that already resolved the prior manifest to finish. Exact TTL constants
are selected from these relationships during implementation, not inferred from
source payload timing.

## Authorization And Request Path

Every request preserves the current authorization sequence before it consumes
prewarmed facts:

1. normalize and validate range, granularity, and IANA timezone;
2. resolve the current actor, representative scope, scope version, Directory
   Sync run, enabled primary provider row, and provider configuration version;
3. recognize only one of the three exact standard windows for that timezone's
   current local anchor `D`;
4. require the split-safety guard to pass before any segmented Redis read;
5. read and validate that provider/version/timezone/anchor manifest;
6. read the current-stats value and required trend segments;
7. require every currently authorized Relay ID to exist in the validated
   complete directory/current-stats roster;
8. intersect all provider-wide facts with the currently authorized Relay IDs;
9. treat an absent row in a complete, non-truncated trend source as zero and
   compose present source labels without timezone conversion; and
10. project the existing Summary, Trend, Members, Organization, or compatibility
   Overview DTO, including its existing scope-version race check.

The exact candidate shapes are:

- today: `D..D`, `granularity=hour`;
- 7d: `D-6..D`, `granularity=day`;
- 30d: `D-29..D`, `granularity=day`.

After shape recognition, the split-safety guard compares the instant at local
`D-1` midnight plus exactly 24 hours with local `D` midnight. If they are not
equal, the request does not read a segmented manifest and immediately uses PR
#192's exact scope-origin path. The guard may parse only these configured
calendar boundaries; it never parses or converts a returned source label.

All other date arithmetic remains calendar arithmetic in the configured
timezone. DST tests must prove that the guard rejects the spring-forward and
fall-back rollover anchors while adjacent 24-hour anchors remain eligible. No
test may pass by converting an hour source label to a timezone-aware instant.

Current authorization intersection occurs at request time even on a full Redis
hit. Prewarm data never grants visibility. If the Relay mapping or
representative scope changes while a response is built, the existing final
scope-version guard remains authoritative.

Existing Team Usage response caches remain the outer acceleration layer. Their
keys, DTOs, cursor signatures, freshness windows, stale behavior, and lane
ownership do not change. There is no completed-result cache in a Pod.

## Partial Failure And Exact Fallback

When current stats and both required historical segments are valid but
`today_hour` is missing, hard-expired, or invalid, the request fetches only the
exact `today_hour` source request for its configured timezone and anchor. It
validates and composes that request-scoped value with the cached history. It
must never refetch `history_6d` or `history_29d` in this partial path. A
token-protected lease and in-flight coordination may collapse the fetch and may
publish a new complete manifest, but no completed value remains in Pod memory.

The request uses PR #192's exact scope-origin path for all of these cases:

- the feature flag is disabled or the normalized allowlist is empty;
- the request timezone is not configured;
- the range is custom or not anchored to the timezone's current local `D`;
- the granularity does not match the standard lane;
- the split-safety guard fails for the timezone and anchor;
- either required historical segment is missing, invalid, hard-expired,
  truncated, over limit, or has mismatched coverage;
- current stats or the provider/version/anchor manifest cannot be trusted;
- a currently authorized Relay ID is absent from the validated complete
  directory/current-stats roster;
- the source-label composition or unique-user union fails validation;
- Redis read, write, pool, lease, decode, or timeout behavior fails;
- Relay fails during the partial-today fetch; or
- caller cancellation, stale provider version, or stale scope prevents safe
  publication or projection.

Fallback means the exact retained scope-origin load for that request's complete
range and granularity. It does not mean a newly invented per-segment fallback,
an approximation, or a stale value outside the existing response-cache policy.

## Prewarm Lifecycle

The prewarmer starts only when the feature flag is enabled, the allowlist is
valid and non-empty, Redis is configured, and the primary Relay provider can be
resolved. It starts after core server dependencies initialize and does not
block liveness or HTTP startup. A failure to start this optional optimization
leaves requests on PR #192.

### Moving Cycle

Every 60 seconds, one moving cycle refreshes the provider-wide current-stats
snapshot once and `today_hour` for every configured, split-safe timezone anchor
`D`. Source-call concurrency across all configured timezones and all Pods is
globally limited to two. A token-checked cycle coordinator and two
token-protected distributed source-call slots enforce the deployment-wide
bound; the process-local limiter uses the same bound. If the preceding moving
cycle is still running when the next tick arrives, the next tick is skipped;
cycles never queue or overlap.

The cycle may publish one timezone manifest as soon as its new current stats,
today segment, and matching hard-valid historical segments are available. A
failure in one timezone does not invalidate another timezone's manifest.

### Historical Correction

For each timezone and split-safe local anchor `D`, refresh `history_29d` and
`history_6d` once per local day. A split-unsafe anchor schedules no segmented
source request. Eligibility begins at local midnight plus a deterministic
jitter in `[0, 30m)`, calculated from provider ID, provider version, timezone
digest, anchor, and class. This bounds cross-Pod and cross-timezone bursts while
making retries and tests reproducible.

The two historical sources are fetched independently and validated
independently. A manifest is published only when both exact history segments
and the referenced current/today values form a valid generation. Late usage and
billing corrections therefore converge without treating completed days as
permanently immutable.

### Startup Recovery

Startup inspects the current provider/version/timezone/anchor manifests and
immutable values. It fetches only a missing or hard-expired current-stats,
history, or today segment. A stale-but-hard-valid segment is retained for its
scheduled cycle; startup does not eagerly rebuild every timezone or every
anchor.

### Leases And Cancellation

Distributed segment leases bind provider ID, persisted provider version,
timezone digest, anchor date, and refresh class (`moving`, `history_29d`, or
`history_6d`). Lease values are random tokens. A worker checks the same token
before manifest publication and releases only through a compare-and-delete
operation. The provider-wide current-stats operation uses one token-checked
shared moving-cycle lease and is referenced, not copied, by timezone manifests.
The cycle coordinator and both distributed source-call slots are also
token-checked and expire after bounded worker deadlines. The coordinator binds
the provider version, normalized allowlist digest, refresh tick, and class so
different Pods cannot each start the same deployment-wide cycle.

A worker that loses its lease, observes a provider version change, or receives
cancellation may leave only unreachable immutable values. It cannot publish a
manifest. Existing process-local flights may coordinate waiters but retain no
completed prewarm result.

## Upstream And Redis Runtime Bounds

The provider-wide source path retains two independent call-count reductions from
the rejected candidate:

- request Relay admin users with `page_size=1000`,
  `include_subscriptions=false`, `sort_by=id`, and `sort_order=asc` on every
  page;
- request batch current usage stats in chunks of at most 500 Relay user IDs.

The directory loop exhausts and validates authoritative pagination before it
chunks stats or publishes any value. The new or extended provider-wide trend
capability returns raw rows plus bytes, points, users, coverage, and completeness
metadata; it does not reuse the existing requested-ID-filtered return contract
as a Redis publication source.

These changes stay behind `backend/internal/relay.Provider`; they do not couple
AI Efficiency to the Sub2API database. Sub2API source remains unchanged.

The shared Redis client is configured with at least four pre-created idle
connections and one-second dial and pool timeouts. The 20 GET POC does not
measure Redis and does not choose large-value Redis read/write command timeouts.
After cache code exists, deploy it to staging with the feature still disabled
and benchmark synthetic maximum-safe values against staging Redis. Benchmark
separate immutable writes, manifest publish, and four-lane MGET reads under the
same min-idle pool and background concurrency-two shape. Select separate read
and write command budgets as the greater of 250 milliseconds or twice the
observed p99, each capped at two seconds. If either required budget would exceed
two seconds or produces any Redis error, do not enable the feature in staging.

Existing small-cache callers retain their 100-millisecond command contexts, so
the broader transport bounds do not make unrelated cache misses wait one
second.

Redis source-body, decoded-point, serialized-segment, timezone-generation, and
all-timezone-generation limits are enforced before publication. An over-limit
value is never cached and uses exact fallback. If the deterministic POC sizing
gate passes, runtime also rejects a shared current-stats envelope at or above 2
MiB and a manifest at or above 64 KiB. Current stats are counted once per
provider generation, while one manifest is counted for each configured
timezone/anchor.

## Segmented-Source POC Hard Gate

Implementation Task 1 is a read-only staging POC against the current Sub2API
contract and exact PR #192 runtime baseline. No production cache code starts
until this gate passes and sanitized evidence is committed.

Choose one completed, split-safe historical local anchor `D` for each timezone
so all five requests are stable. Before any GET, assert the split-safety formula
for that timezone and anchor. Record the exact sanitized calendar dates, but no
user data.

For each of the four configured timezones, make exactly five read-only GETs:

1. `history_29d`: `D-29..D-1`, `granularity=day`;
2. `history_6d`: `D-6..D-1`, `granularity=day`;
3. `today_hour`: `D..D`, `granularity=hour`;
4. `direct_30d`: `D-29..D`, `granularity=day`;
5. `direct_7d`: `D-6..D`, `granularity=day`.

The probe makes exactly 20 trend GETs with global concurrency at most two. It
validates every raw response before composition. For each timezone, it then
uses the exact runtime algorithm:

1. retain the validated `today_hour` source rows and their raw labels;
2. coalesce `today_hour` by Relay user ID plus the exact first 10-character
   source-label prefix;
3. merge that daily map separately with `history_6d` and `history_29d` by Relay
   user ID plus daily source label, adding actual cost and preserving the
   optional-token rules;
4. key `direct_7d` and `direct_30d` by Relay user ID plus their unmodified daily
   source labels; and
5. compare each composed per-key map with its corresponding direct daily map.

Key sets and token presence must match; tokens must be exact; each actual-cost
delta must be at most `1e-9` in absolute value. Aggregate totals cannot hide a
per-user/daily-source-label mismatch. Coalescing is a projection operation only;
the stored `today_hour` segment retains every raw source label.

The same POC executable also evaluates completed DST rollover fixtures for
America/Los_Angeles and Europe/Berlin without making additional HTTP calls. It
must prove that spring-forward and fall-back anchors fail the split-safety guard
and select PR #192 fallback before any segmented manifest read, while the
split-safe anchors selected for the 20 GETs pass. This assertion compares only
calendar-boundary instants; it never parses a source label.

The 20 GETs measure only trend source and trend-segment capacity. The sanitized
POC record includes, per call and in total:

- HTTP duration and total wall duration;
- response body bytes;
- decoded point count;
- unique user count and composed union count;
- peak process RSS;
- serialized Redis bytes for each of the three stored trend segments; and
- serialized trend-segment bytes per timezone and across all four timezones.

The two direct daily comparison responses are measured as HTTP bodies and
decoded results but are not counted as stored Redis values. The shared current
stats value and manifests are not fetched by these 20 calls and are not inferred
from trend response sizes.

The same probe performs deterministic no-extra-HTTP source-body and structural
sizing tests. It serializes:

1. one directory page envelope with exactly 1,000 strictly ascending,
   maximum-width positive int64 IDs, authoritative `page`, `page_size=1000`,
   `pages=5`, and `total=4999` metadata, no subscriptions, every other string
   field exposed by the current no-subscription response filled to 1,024 bytes,
   and maximum-width RFC3339Nano timestamps;
2. one stats chunk envelope with exactly 500 entries whose object keys and
   embedded IDs are distinct maximum-width positive int64 values, with every
   numeric stats field present at its maximum finite non-negative JSON width and
   every optional token field present at `math.MaxInt64`;
3. one shared current-stats Redis envelope containing exactly 4,999 synthetic
   rows with the same maximum-width IDs, costs, and token values plus its roster
   count and digest; and
4. four manifest envelopes using the maximum admitted widths: 19-digit
   provider/version values, a 255-byte timezone name, 64-character digests and
   generation IDs, 512-byte Redis key references, RFC3339Nano timestamps, and
   maximum-width byte/point/user counters.

All identities and string contents are synthetic. These fixtures perform no
HTTP request and do not change the exact 20-GET count.

The candidate source-body and structural caps become implementation constants
only if the synthetic directory page is strictly smaller than 16 MiB, the
synthetic stats chunk is strictly smaller than 2 MiB, the synthetic
current-stats envelope is strictly smaller than 2 MiB, and every synthetic
manifest is strictly smaller than 64 KiB. A fixture that reaches its candidate
cap fails the POC and stops implementation; the POC does not raise a limit or
derive a different number. Passing the gate locks these exact pre-decode runtime
body constants:

- directory page body: `< 16 MiB`;
- one at-most-500-ID stats chunk body: `< 2 MiB`.

A complete Redis generation size counts the shared current-stats value once per
provider, not once per timezone, plus one manifest per timezone and all 12
stored trend segments. Direct comparison responses and transient directory and
stats HTTP bodies are excluded. Consequently, when every gate passes, the
strict Redis generation bound is less than 66.25 MiB: less than 64 MiB of trend
segments, one shared value below 2 MiB, and four manifests below 64 KiB each.

Mandatory hard gates are:

| Gate | Required result |
| --- | ---: |
| Each GET duration | `< 25s` |
| All 20 GETs wall duration | `< 5m` |
| Each response body | `< 32 MiB` |
| Each decoded result | `< 1,000,000 points` |
| Each source unique users | `< 5,000` |
| Each composed unique-user union | `< 5,000` |
| Each stored trend segment | `< 8 MiB` |
| Three trend segments for one timezone | `< 16 MiB` |
| All 12 timezone trend segments | `< 64 MiB` |
| One synthetic directory page body | `< 16 MiB` |
| One synthetic 500-ID stats chunk body | `< 2 MiB` |
| One synthetic shared current-stats value | `< 2 MiB` |
| Each synthetic manifest | `< 64 KiB` |
| Peak probe RSS | `< 192 MiB` |
| Token comparison | Exact, including nil presence |
| Actual-cost comparison | Absolute delta `<= 1e-9` per key |

Reaching a strict upper bound fails the gate. Any request error, invalid point,
coverage mismatch, duplicate, out-of-order point, exact 5,000-user source,
composed union at or above 5,000, synthetic structural-cap overrun, trend-size
overrun, or comparison mismatch also fails. On any failure, record sanitized
evidence and stop before implementation; do not relax a gate, reinterpret a
label, or proceed with a subset of timezones.

The probe receives URL and credential only through process environment, limits
body reads, and prints no URL, header, ID, source row, response body, name,
email, credential, or raw Redis value. Temporary scripts, binaries, bodies, and
secrets stay outside the repository and are deleted after the decision.

## Testing Contract

After the POC passes, implementation follows RED/GREEN TDD with synthetic
identities only. Required tests cover:

- exact recognition and rejection boundaries for today/hour, 7d/day, and
  30d/day standard windows;
- split-safety acceptance for 24-hour local dates and exact PR #192 fallback
  before manifest reads for 23-hour and 25-hour rollover dates;
- allowlist validation, canonical deduplication, empty/disabled behavior, and
  the four-timezone maximum;
- source-label preservation with no UTC reinterpretation or timezone parsing;
- hour-prefix coalescing by the exact first 10 characters;
- exact POC/runtime ordering of today coalescing, daily-label merge, and
  per-user/daily-label direct comparison;
- 7-day composition from `history_6d` plus `today_hour`;
- 30-day composition from `history_29d` plus `today_hour`;
- independent `history_6d` and `history_29d` source selection;
- optional-token propagation and `1e-9` absolute cost comparison boundaries;
- local-date rollover for UTC, Asia/Shanghai, America/Los_Angeles, and
  Europe/Berlin;
- DST spring and fall split rejection without converting source labels;
- current authorization intersection and final scope-version race checks;
- provider-version, timezone-digest, anchor, and schema isolation;
- immutable writes, publish-last manifests, and old-anchor read completion;
- token-checked multi-Pod lease collapse and skipped overlapping ticks;
- partial missing-today fallback that never refetches either history segment;
- Redis error, Relay error, cancellation, stale value, hard expiry, and lost
  lease behavior;
- invalid, duplicate, out-of-order, mismatched-coverage, oversized, and
  truncated source rejection;
- per-source and composed-union 5,000-user guards before authorization;
- raw provider-wide trend rows and bytes/points/users/completeness metadata with
  no requested-ID filtering before Redis publication;
- directory `page_size=1000`, complete and consistent pagination, positive
  ascending unique IDs, fixed no-subscription/id-sort query, and the strict
  provider-directory `< 5000` bound;
- stats chunks no larger than 500 with exact requested-ID coverage, no
  extra/missing/duplicate records, and finite non-negative fields;
- exact fallback when a currently authorized ID is absent from the complete
  directory/current-stats roster;
- zero contribution, without fallback or a synthetic row, when a validated
  complete and non-truncated trend result omits a roster ID;
- directory and stats hard body limits applied before JSON decode, including
  exact-at-limit and over-limit rejection;
- deterministic no-HTTP maximum-width directory page, stats chunk, 4,999-row
  current-stats, and manifest sizing, with one shared current-stats value counted
  per provider;
- feature-disabled staging Redis benchmarking of synthetic maximum-safe values
  before timeout constants are selected or staging is enabled;
- global moving-source concurrency no larger than two; and
- unchanged Summary, Trend, Members, Organization, and Overview DTOs, response
  caches, cursor payloads, and freshness behavior.

Run focused Relay/prewarm/Team Usage tests, full backend tests, race tests,
`go vet ./...`, and `go build ./...`. Environment-sensitive staging evidence
remains separate from deterministic test results.

## Observability

Add bounded-cardinality metrics for:

- cycle class, timezone lane, and outcome;
- cycle, source-call, Redis read, and Redis write duration;
- skipped moving ticks and lease outcomes;
- source bytes, decoded points, unique users, union users, segment bytes,
  timezone bytes, and generation bytes;
- directory pagination, provider-ID bound, stats exact-coverage, and raw-trend
  completeness rejection reasons from closed enums;
- manifest, current-stats, and segment cache outcomes;
- last successful moving and historical refresh by configured timezone; and
- request prewarm outcome or exact fallback reason from a closed enum.

Background logs use a generated operation ID. User-originated fallback keeps the
existing request ID. Logs and metrics never include Relay user IDs, names,
emails, credentials, raw keys, source rows, response bodies, or authorization
facts. Timezone metric values are bounded by the validated maximum-four
allowlist.

## Staging Acceptance

Deploy only an immutable exact-head staging image with the prewarm feature
disabled first. Run and pass the synthetic maximum-safe Redis benchmark, lock
the measured read/write command budgets, and only then enable the feature in
staging. Production remains unchanged. Run three sanitized rounds with the same
authorized account and deterministic standard window:

1. **Empty Redis:** remove only the new prewarm, existing Team Usage origin,
   response, and lease keys, then issue all four lanes concurrently.
2. **Prewarmed cold lanes:** retain valid current/history/today manifests, clear
   only the four response caches, then issue all four lanes concurrently.
3. **Immediate warm:** repeat all four lanes without clearing any key.

Acceptance requires:

- HTTP 200 and business-payload equivalence in every round;
- every prewarmed cold lane completes within five seconds;
- every immediate warm lane completes within 1.5 seconds;
- a fully prewarmed request makes no Relay call;
- Relay 429, 5xx, transport-error, and timeout counts are zero;
- Redis read, write, pool, lease, decode, and timeout errors are zero;
- empty-Redis completion is no worse than the PR #192 baseline by more than one
  second;
- one logical cycle is collapsed across concurrent Pods;
- no Pod retains a completed result; and
- production image, configuration, Helm revision, and traffic remain unchanged.

The audit records only sanitized request/operation IDs, durations, counts,
cache outcomes, image digest, and staging revision. It does not retain
credentials, user lists, response bodies, or unredacted Redis values.

## Rollout And Rollback

The feature flag defaults to false. POC completion does not enable staging or
production. Staging enablement and production release are separate approvals.

Rollback disables the prewarm feature. New readers and background cycles stop,
and every request immediately uses the retained PR #192 scope-origin path.
Immutable values and manifests expire by TTL; rollback requires no destructive
Redis flush, schema migration, Sub2API deployment, frontend change, or cursor
reset.

`docs/architecture.md` changes only after an implementation becomes current
runtime. Until then, this spec records an approved but POC-blocked design, and
the rejected UTC-hour spec remains unchanged historical evidence apart from its
status and replacement reference.

## Alternatives Rejected

### Reinterpret Source Hours As UTC

The prior POC proved that a source hour label is not a reliable UTC bucket.
Timezone conversion would repeat the rejected semantic error.

### Cache One Timezone And Convert It

Sub2API applies the requested timezone only to query bounds and does not expose
a timezone-neutral label. One timezone cannot safely satisfy another.

### Derive Seven Days From The Thirty-Day Source

Sub2API selects the global top users independently for each source range.
Slicing `history_29d` can omit a user selected by `history_6d`, so both exact
segments are required.

### Cache Three Complete Standard Windows

Complete today, 7-day, and 30-day values duplicate the moving day and force
more full-range refresh work. Two history segments plus one shared today edge
preserve the exact source selection while bounding moving refreshes.

### Add A Pod Completed-Result Cache

Pod values fragment across replicas and complicate provider, scope, and
rollover invalidation. Redis immutable generations plus process-local in-flight
coordination provide the required collapse without replica-local result state.

### Change Sub2API First

A timezone-explicit upstream grouping contract could simplify this design, but
it requires a separate Sub2API lifecycle and migration. This design deliberately
caches current output semantics through the existing Relay HTTP boundary.
