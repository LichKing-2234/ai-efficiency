# Team Usage Cold-Load Experiment Matrix Design

**Status:** Approved for implementation and staging comparison.

**Date:** 2026-07-20

## Baseline

All candidates start from PR #160 head `f184a7b2`, whose runtime tree matches
the final reviewed PR #190 state at `14098806` / image commit `55302b62`.
PR #191 is withdrawn from PR #160.

The recorded #190 staging result is the comparison baseline:

- cold complete readiness: 12.364 seconds;
- immediate warm readiness: 1.222 seconds;
- cold Relay calls: 255, all 2xx;
- warm Relay calls: zero;
- response-cache and Redis error deltas: zero.

The #190 Pod-local completed trend cache is not an acceptable final design.
Every candidate must remove it before staging.

## Common Problem

Summary, Members, and Organization request usage stats in at-most-100-user
chunks. Missing range totals currently cause trend fallback inside a chunk.
Trend independently requests the same complete user set. This makes the chunk
boundary, rather than the authorized team scope, the aggregation boundary.

Every candidate must first:

1. finish all stats chunks;
2. collect incomplete range rows;
3. request the complete authorized Relay ID set through Sub2API
   `GET /api/v1/admin/dashboard/users-trend`;
4. fill only incomplete authorized rows;
5. preserve nil token totals when any point omits tokens;
6. keep a failed range completion soft; and
7. keep Trend's existing provider-error behavior.

Sub2API `snapshot-v2` is excluded because its user-trend limit is 50 and its
stats payload is not per-user Team Usage data.

## Candidate A: Upstream Aggregate Cache Only

AI Efficiency retains no completed trend primitive in Pod memory or Redis.
Each cold Team Usage lane may call the same `/users-trend` query once after its
stats chunks. Sub2API's existing 30-second in-process `snapshotCache` and
singleflight collapse identical concurrent database loads.

Expected benefits:

- smallest AI Efficiency implementation;
- no primitive Redis MGET/SET or lease traffic;
- one Sub2API database aggregation when requests reach the same Sub2API Pod.

Risks:

- up to four large HTTP responses still cross the service boundary;
- no cross-Pod Sub2API collapse;
- Personal Usage cannot warm Team Usage;
- a multi-Pod Sub2API deployment may execute duplicate origins.

Candidate A wins when it meets every staging gate because it has the smallest
new state surface.

## Candidate B: Redis Per-User Primitives

AI Efficiency uses one full-request Redis lease around `/users-trend`, filters
the result to authorized users, and writes one 60-second value per Relay user.
The lease identity uses the complete requested set, not the current misses.
Waiters poll only lease TTL every 100 milliseconds and perform one MGET after
release.

Expected benefits:

- cross-Pod AI Efficiency collapse;
- Personal Usage can warm individual Team Usage entries;
- overlapping authorized scopes reuse users without sharing authorization.

Risks:

- one MGET over the scope plus a large pipelined SET;
- more keys and envelope validation;
- Redis failure can duplicate the upstream origin.

Candidate B is preferred over C when A fails and B meets every gate.

## Candidate C: Redis Scope-Origin Snapshot

AI Efficiency stores one 60-second origin payload per provider version,
authorized scope version/hash, normalized range, granularity, and timezone.
The payload contains only Relay IDs, per-user stats, and validated trend points.
It contains no names, emails, credentials, or response DTOs.

One loader performs the bounded stats chunks and one `/users-trend` request.
Summary, Trend, Members, and Organization project their existing independent
response snapshots from the shared origin. The four response caches remain
separate.

Expected benefits:

- one stats generation and one user-trend aggregation for the complete page;
- smallest expected cold critical path;
- one Redis GET per waiting lane after the holder writes.

Risks:

- largest Redis value;
- new coarse-grained origin cache and validation contract;
- less reuse across partially overlapping scopes;
- higher implementation and review cost.

Candidate C is accepted only if A and B fail and C meets every gate.

## Shared Safety Contract

- Do not modify Sub2API.
- Do not add a completed Team Usage trend cache in AI Efficiency Pod memory.
- Do not change frontend requests, HTTP DTOs, response-cache keys, cursors,
  freshness windows, or stale windows.
- Resolve authorization before projecting or returning Relay data.
- Redis remains fail-open and cannot grant access or cache errors.
- Do not log or persist credentials, tokens, raw bodies, user-ID lists, Redis
  keys, emails, or usernames.
- Tests use only synthetic identities and values.
- Production remains unchanged throughout the experiment.

## Local Verification

Every candidate requires focused RED/GREEN tests for its collapse boundary,
then:

```bash
cd backend
go test ./internal/readcache ./internal/relay ./internal/relayruntime ./internal/teamusage ./cmd/server -count=1
go test ./... -count=1
go test -race ./internal/readcache ./internal/relay ./internal/teamusage -count=1
go vet ./...
go build ./...
```

## Staging Method

Candidates are deployed sequentially, never concurrently. For each candidate:

1. publish the exact reviewed commit as a multi-architecture immutable image;
2. use the normal two-phase staging restore rollout;
3. wait longer than every in-flight lease;
4. clear only candidate-specific origin/primitive keys and the four Team Usage
   response keys;
5. verify no lease remains;
6. run one concurrent four-lane cold round;
7. run one immediate four-lane warm round;
8. record sanitized timing, counts, cache events, Redis pool events, and Relay
   status classes; and
9. proceed to the next candidate only after deleting temporary credentials and
   audit artifacts.

The test must use the current authorized member count. It must not hard-code
the historical 235 Relay-linked count.

## Acceptance And Selection

Each candidate must satisfy all of:

```text
cold complete readiness <= 9.0s
each warm lane <= 1.5s
warm Relay calls = 0
individual trend GETs = 0
Relay 429/5xx/transport/timeout = 0
cold/warm member counts and aggregates equal
response-cache/origin-cache/lease/Redis errors = 0
no AI Efficiency Pod completed-result cache
```

Candidate-specific proof:

- A: Sub2API users-trend HTTP calls and database-origin behavior are recorded;
- B: at most one `batch_origin`, no full-value polling, and bounded MGET/SET;
- C: exactly one scope-origin load and bounded serialized payload size.

Selection order is A, then B, then C. A simpler candidate wins over a more
stateful candidate when both pass. If none passes, keep PR #160 at the #190
baseline, record the matrix failure, and do not promote an experiment.

## Delivery Boundary

Each candidate has its own branch, worktree, commits, image tag, Helm revision,
and evidence. Do not merge candidate branches into each other during testing.
After selection, rebase or replay only the winning implementation onto the
current PR #160 head, run full review and CI, then clean the losing branches,
worktrees, and staging-only images conservatively.
