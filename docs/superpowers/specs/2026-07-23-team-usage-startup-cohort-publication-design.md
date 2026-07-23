# Team Usage Startup Cohort Publication Design

**Status:** Implementation and the verification ladder are complete at exact
reviewed code head `30279888db6dad6c0f5e433879ba9573642fc461`. Documentation
commit `f028b72ce61150b005b788b5518f97bc52b8a882` received one Important
post-commit Standards finding for a stale publication snippet; this follow-up
corrects it without pre-recording its own review result. The clean post-commit
documentation review result and current only image-eligible head are recorded
in ignored SDD ledgers after review. No Task 15 image has been published or
deployed and no Task 15 staging replay has run. Staging remains
feature-disabled according to retained Task 14 evidence; Tasks 4-5 remain
pending.

**Date:** 2026-07-23

**Refines:**

- `docs/superpowers/specs/2026-07-21-team-usage-segmented-timezone-prewarm-design.md`

The segmented timezone prewarm design remains the source of truth for value
semantics, Redis schema v2, authorization, fallback, TTLs, and production
boundaries. This document replaces only its startup fetch ordering, cohort
publication, and first-tick evidence contract. It does not describe current
staging or production runtime until a new staging replay passes.

## Evidence And Problem

Task 14 proved deterministic deployment-wide startup ownership on staging:

- exactly one Pod recorded four `startup/success` labels and 16 startup source
  observations;
- exactly one Pod recorded four `startup/lease_busy` labels and zero startup
  source observations; and
- the successful startup coordinator marker remained retained.

The same replay failed the remaining gates. Four complete manifests spanned
`50.936s` from first to last instead of at most five seconds. The pre-ticker
gate was not proven because no direct scheduler-tick evidence existed. The
final scrape also recorded one `lease_acquire` and one `manifest_read`
`command_deadline`; bounded pool scrapes and wait/timeout deltas were zero, but
that evidence does not prove why the deadlines occurred.

The pre-change implementation explained the observed cohort shape.
`runStartup` looped over timezones and missing segment classes sequentially.
Each source call still used the deployment-wide limiter, but startup presented
only one segment call at a time, so the configured concurrency of two was
unused. Each lane could publish as soon as its last missing segment arrived,
spreading manifests across the entire serial source-fetch interval. A
lease-busy Pod could also reach its fixed ticker while the owner was still
working, adding scheduled Redis and source work before the startup cohort
settled.

## Decision

Use a bounded two-worker startup fetch phase followed by a publication barrier.
The owner builds provider-wide current stats once, plans every safe timezone
lane, fetches missing segments through at most two workers, waits for all tasks
to settle, and only then publishes every complete successful lane. This uses the
existing global source limiter and does not increase deployment-wide Relay
concurrency above two.

The design keeps the accepted `250ms` Redis command budgets and the five-second
manifest cohort gate. Raising timeouts alone cannot fix serial source work, and
relaxing the cohort gate would preserve the cold-loading regression.

## Startup Phases

### 1. Ownership And Preflight

The owner retains the current startup coordinator key dimensions and six-minute
TTL: provider ID, persistent provider version, and normalized timezone-allowlist
digest. A concurrent or staggered loser reports `lease_busy`, performs no
startup source work, and does not mutate Redis generations.

The owner resolves one binding and captures one batch time. For each configured
timezone it derives a split-safe identity, reads the current manifest, and
builds a `startupLanePlan` containing:

- identity and previous manifest, when present;
- existing hard-valid references;
- whether provider-wide current stats are missing or hard-expired; and
- the exact missing segment classes.

Invalid or split-unsafe lanes retain the existing isolated outcome behavior.
One lane's preflight failure does not erase another lane's usable plan.

### 2. Shared Current Stats

If any usable lane needs current stats, the owner builds the provider-wide
current-stats envelope once and writes one immutable value. Every lane that
needs current stats references that value. If the shared build or write fails,
startup cannot form a valid cohort: it releases acquired work, returns an
error, and releases the startup coordinator so a later owner may retry.

### 3. Bounded Segment Fetch

Expand all planned missing segments into at most 12 tasks: three segment classes
for each of at most four timezones. Feed them to exactly two startup workers.
Every task still uses `fetchLeasedSegment`, including:

- the existing segment lease and 90-second TTL;
- the existing process-local and deployment-wide source limiters;
- current provider/version revalidation; and
- immutable Redis value publication.

The worker count is a submission bound, not a second source-concurrency policy.
The existing `SourceCallLimiter` remains authoritative and also covers request-
time recovery and every other background cycle.

Each successful task returns an in-flight leased reference. These references
may exist only inside the active startup batch. They are not stored on
`Prewarmer`, exposed to requests, or retained after publication or failure.
This is bounded orchestration state, not a completed Pod cache.

Task failures are collected per lane. A failure does not cancel unrelated lane
tasks, preserving timezone isolation. Coordinator loss or parent cancellation
cancels the entire batch and forbids all later manifest publication.

### 4. Cohort Publication Barrier

No startup manifest is published until every planned segment task has settled.
After the barrier, construct one candidate manifest per lane from hard-valid
existing references, the shared current reference, and successful task results.

For each complete lane, atomically publish with these claims:

- the startup coordinator claim; and
- every newly acquired segment claim for that lane.

The maximum is four claims, below the existing five-claim limit. Existing
references need no new segment claim. Publication revalidates provider binding,
all reference metadata, hard-expiry margin, and lease ownership exactly as it
does today.

Successful lane manifests are written consecutively only after all source work
settles. A failed or incomplete lane is not published, but it does not block a
complete lane. Startup returns the joined lane failures, releases every segment
lease/context, and releases the startup coordinator on any failure. When all
eligible lanes publish successfully, the owner retains the startup coordinator
marker until its existing TTL expires.

## Scheduler Evidence

The local implementation adds one no-label monotonic counter for received
scheduled ticks. It increments synchronously in `runTicks` before starting
moving, recovery, or historical workers. It contains no timezone, operation
ID, provider, or user dimension.

The staging pre-ticker gate reads this counter directly. At the scrape that
first observes all four complete manifests:

- both new Pods must report scheduler-tick total zero;
- one Pod must report the four startup successes;
- the other must report the four startup lease-busy outcomes; and
- the loser must still report zero startup source observations.

This replaces inferred ticker timestamps. Absence of completed cycle metrics is
not sufficient because a scheduled cycle may be in flight.

## Failure And Cleanup Semantics

- Source or immutable-write failure: mark only the affected lane/task, finish
  the bounded task set, publish other complete lanes, return a joined error, and
  release the startup coordinator.
- Shared current-stats failure: publish no startup manifests and release the
  coordinator.
- Coordinator loss, provider-version change, parent cancellation, or worker
  deadline: cancel the batch, publish no later manifests, and release all held
  segment resources.
- Segment context or lease expiry before the barrier: reject that lane at
  atomic publication; never extend the 90-second lease or weaken token checks.
- Redis command deadline: retain the `250ms` budget and closed error class. If
  it recurs after the scheduled-work overlap is removed, address it in a
  separate measured budget task.

Every task result is drained. Every acquired segment lease is released on every
exit path. No goroutine, slice, map, or `Prewarmer` field retains decoded source
data after `runStartup` returns.

## Test Contract

RED/GREEN tests must prove:

1. Four empty lanes submit 12 segment tasks but source concurrency is exactly
   two, never one or more than two.
2. No manifest exists while any planned source task remains blocked.
3. Once all tasks settle, four complete manifests publish as one cohort and use
   one shared current-stats reference.
4. A single lane failure does not prevent other complete lanes from publishing,
   but startup returns an error and releases its coordinator marker.
5. Coordinator loss or cancellation before the barrier publishes no later
   manifests and releases every acquired segment lease.
6. Concurrent and staggered startup losers remain source-free and `lease_busy`.
7. The scheduler-tick counter increments before scheduled workers start and is
   preinitialized without labels.
8. Repeated and race runs leave no retained completed result or goroutine.

Run focused startup/telemetry tests, a repeated concurrency test, race tests for
`teamusage`, `telemetry`, and `cmd/server`, the full backend suite, vet, and
build. Independent task review must clear every Critical and Important finding
before an image is published.

## Local Implementation And Review

The exact locally reviewed code head is
`30279888db6dad6c0f5e433879ba9573642fc461`. The final verification ladder ran:

```text
go test ./internal/teamusage ./internal/telemetry ./cmd/server -count=1
go test -race ./internal/teamusage ./internal/telemetry ./cmd/server -count=1
go test ./... -count=1
go vet ./...
go build ./...
git diff --check
```

All six commands exited zero. The focused test and race commands each passed
three packages with zero failures and zero race findings. The full backend run
passed 38 packages, reported 36 packages with no test files, and had zero
failures. Vet, build, and diff checks had zero findings. The race command
emitted two non-fatal macOS linker warnings.

Initial Standards review reported `0 Critical / 2 Important / 1 Minor`.
Correction `30279888` resolved the code Important and Minor findings.
Documentation commit `f028b72ce61150b005b788b5518f97bc52b8a882` attempted to
resolve the remaining documentation Important finding, but its first
post-commit documentation review reported `0 Critical / 1 Important / 0 Minor`
because the checked publication snippet did not preserve the reviewed
segment-context lifetime sequence. This follow-up corrects that snippet but
does not claim its own approval. A clean post-commit documentation review
result and the current only image-eligible head are recorded in ignored SDD
ledgers after review, because a tracked commit cannot truthfully pre-record its
own review result. Final Spec review remains `0 Critical / 0 Important / 0
Minor`.

This status is local evidence only: no Task 15 image has been built or
deployed, no Task 15 staging replay has run, and this design does not claim
current staging or production runtime behavior. Retained Task 14 evidence
remains the source for the feature-disabled staging state. Tasks 4-5 remain
pending, Task 9 Steps 3-6 remain unchecked, and `docs/architecture.md` remains
unchanged.

## Staging Gates

Because exact code changes, first publish a new multi-architecture immutable
image and repeat the feature-disabled seven-class Redis benchmark with 100
samples per class. Any returned command, transport, INFO, cleanup, eviction, or
rejected-connection error blocks enablement.

Then run one two-Pod diagnostic replay, with no API acceptance round and no
business-key deletion. Require:

- one startup owner and one lease-busy loser;
- exactly four complete manifests;
- first-to-last manifest publication at most five seconds;
- scheduler-tick total zero on both Pods at cohort completion;
- deployment-wide source concurrency at most two;
- zero Relay errors;
- zero Redis operation errors and zero Redis error-class deltas; and
- zero recorded Redis pool pending values and wait/timeout deltas at bounded
  scrapes, without claiming that interval pressure is impossible.

Whether the replay passes or fails, restore staging to one disabled replica on
the exact image and verify production unchanged. A pass only permits a fresh
Task 9 Step 3 acceptance attempt; it does not count as the three API rounds.

## Alternatives Rejected

### Raise Redis Budgets First

The feature-disabled exact-code benchmark passed all seven command classes at
`250ms`. A larger timeout may hide the two observed deadlines but cannot reduce
the `50.936s` serial cohort or prove completion before scheduled work.

### Publish Each Lane As Soon As It Is Ready

This preserves the pre-change first-to-last spread. Fetch concurrency alone can
still let one fast lane publish far ahead of the last lane. The barrier is what
makes publication a cohort.

### Relax The Five-Second Gate

The gate protects the user-visible cold-loading goal. Relaxing it would accept
the regression rather than remove its cause.
