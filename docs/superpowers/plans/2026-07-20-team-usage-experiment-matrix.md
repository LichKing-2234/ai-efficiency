# Team Usage Cold-Load Experiment Matrix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to execute this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement and stage-test three mutually exclusive no-Pod-cache Team Usage cold-loading strategies from one stable PR #190 baseline.

**Architecture:** All candidates move range completion above stats chunks and use Sub2API users-trend over the complete authorized scope. A relies on Sub2API's own cache, B adds per-user Redis primitives, and C adds one Redis scope-origin snapshot; identical staging gates decide the winner.

**Tech Stack:** Go 1.24, Redis/go-redis, miniredis, Gin, Ent-backed Team Usage scope, Sub2API HTTP, Docker buildx, Helm, Kubernetes, Prometheus metrics.

**Status:** Complete. No candidate met every staging gate; staging is restored to the #190 baseline, while Candidate C is retained as the next experimental reference rather than promoted as a winner.

## Global Constraints

- Common base is `feat/platform-loading-performance@f184a7b2`.
- Candidate branches never merge into one another during testing.
- Do not modify Sub2API or production.
- Do not retain completed Team Usage trend results in AI Efficiency Pod memory.
- Keep existing Team Usage HTTP DTOs, frontend, response caches, cursors, and freshness/stale windows.
- Use only synthetic identities in tests and only sanitized aggregate evidence in docs.
- Update each checkbox only after the corresponding action actually completes.

---

### Task 1: Lock The Matrix And Create Candidate Worktrees

- [x] **Step 1: Verify the coordinator diff**

```bash
git diff --check
git status --short --branch
```

Expected: only this spec and plan are new.

- [x] **Step 2: Commit the matrix contract**

```bash
git add docs/superpowers/specs/2026-07-20-team-usage-experiment-matrix-design.md docs/superpowers/plans/2026-07-20-team-usage-experiment-matrix.md
git commit -m "docs(teamusage): define cold-load experiment matrix"
```

- [x] **Step 3: Create three ignored linked worktrees from the matrix commit**

```bash
git worktree add .worktrees/team-usage-upstream-aggregate -b exp/team-usage-upstream-aggregate perf/team-usage-experiment-matrix
git worktree add .worktrees/team-usage-redis-primitives -b exp/team-usage-redis-primitives perf/team-usage-experiment-matrix
git worktree add .worktrees/team-usage-scope-origin -b exp/team-usage-scope-origin perf/team-usage-experiment-matrix
```

Expected: each worktree has the same HEAD and a clean status.

---

### Task 2: Candidate A - Upstream Aggregate Cache Only

**Worktree:** `/Users/admin/ai-efficiency/.worktrees/team-usage-upstream-aggregate`

**Primary files:**

- `backend/internal/relay/sub2api.go`
- `backend/internal/relay/sub2api_team_trend_batch.go`
- `backend/internal/relay/sub2api_team_trend_batch_test.go`
- `backend/internal/relay/sub2api_team_trend_cache.go` (delete)
- `backend/internal/relay/sub2api_team_trend_cache_test.go` (delete)
- `backend/internal/teamusage/service.go`
- `backend/internal/teamusage/organization.go`
- focused tests in both packages

- [x] **Step 1: Add RED tests**

Tests must prove all stats chunks finish before one full-scope trend call,
Organization uses branch IDs for stats but full represented IDs for trend,
range completion remains soft, and `sub2apiRelay` has no completed-value map.
An HTTP adapter test must assert the only trend route used is
`/api/v1/admin/dashboard/users-trend`.

- [x] **Step 2: Run RED**

```bash
cd backend
go test ./internal/teamusage -run 'CompleteRange|Organization.*Aggregate|Members.*Aggregate' -count=1 -v
go test ./internal/relay -run 'UsersTrendBatch|NoPodTrendCache' -count=1 -v
```

Expected: failures are caused by chunk-local fallback and the existing Pod
cache, not malformed fixtures.

- [x] **Step 3: Implement A**

Remove `teamTrendCache`. Add one validated users-trend HTTP adapter with the
existing bounded limit and authorization filtering. Move complete-range policy
to Team Usage after all stats chunks. Do not add an AI Efficiency Redis
primitive or aggregate value.

- [x] **Step 4: Verify A**

```bash
cd backend
gofmt -w internal/relay internal/teamusage
go test ./internal/relay ./internal/teamusage -count=1
go test -race ./internal/relay ./internal/teamusage -count=1
go vet ./internal/relay ./internal/teamusage
```

- [x] **Step 5: Commit A, then record the exact SHA in the coordinator plan**

```bash
git add backend
git commit -m "perf(teamusage): test upstream aggregate cache strategy"
```

After the implementation commit exists, update this coordinator plan with its
exact SHA in a separate bookkeeping commit.

Candidate A implementation commits are
`da32a9d780a4cfe5e2dfec2f18cad7f9b76c68f4` and
`191537ff2e4a06629b3324b14b764d9809394fcb`, followed by test-contract commit
`41cec4c693c0e0303eca89370f7a010a98f7e635`; final candidate head is
`41cec4c693c0e0303eca89370f7a010a98f7e635`. Focused, full-backend, race, vet,
and build verification passed. Task-scoped spec and quality re-review,
including the final test-only diff, reported zero Critical, Important, or
Minor findings.

---

### Task 3: Candidate B - Redis Per-User Primitives

**Worktree:** `/Users/admin/ai-efficiency/.worktrees/team-usage-redis-primitives`

**Primary files:** candidate A files plus:

- `backend/internal/readcache/multi_store.go`
- `backend/internal/relay/sub2api_team_trend_redis.go`
- integration and Redis primitive tests
- server cache metrics/provider construction

- [x] **Step 1: Replay the common A orchestration commit without merging A**

Use `git cherry-pick` with the exact Candidate A implementation commits recorded
in this plan, in order. Resolve no unrelated files; B must contain the same
common range-completion behavior before adding Redis.

- [x] **Step 2: Add RED tests**

Tests must prove two Pods with the same complete requested set but different
current miss subsets produce one users-trend request; waiters make no MGET
while lease TTL is positive; Personal Usage can warm one matching user; and
only authorized requested IDs are stored or returned.

- [x] **Step 3: Run RED**

```bash
cd backend
go test ./internal/readcache ./internal/relay -run 'Multi|FullRequestLease|Waiter|WriteThrough' -count=1 -v
```

- [x] **Step 4: Implement B**

Add ordered MGET/pipelined SET, per-user 60-second envelopes, a 15-second
full-request lease, 100-millisecond TTL-only waiting, one post-release MGET,
12-second users-trend origin, current truncation behavior, and 24-slot
individual fallback. Do not add a Pod result map.

- [x] **Step 5: Verify B**

```bash
cd backend
gofmt -w internal/readcache internal/relay cmd/server
go test ./internal/readcache ./internal/relay ./internal/relayruntime ./internal/teamusage ./internal/personalusage ./cmd/server -count=1
go test -race ./internal/readcache ./internal/relay ./internal/teamusage ./internal/personalusage -count=1
```

- [x] **Step 6: Commit B, then record the exact SHA in the coordinator plan**

```bash
git add backend
git commit -m "perf(teamusage): test Redis primitive strategy"
```

After the implementation commit exists, update this coordinator plan with its
exact SHA in a separate bookkeeping commit.

Candidate B implementation commits are
`dab8b31baa5fc39ac443e2859771d65930e4ead2` and
`fe2c8d72527c8c2ab93c5559f45bf65e4b23555a`; final candidate head is
`fe2c8d72527c8c2ab93c5559f45bf65e4b23555a`. Focused, full-backend, race,
vet, and build verification passed. Task-scoped re-review reported zero
Critical, Important, or Minor findings.

---

### Task 4: Candidate C - Redis Scope-Origin Snapshot

**Worktree:** `/Users/admin/ai-efficiency/.worktrees/team-usage-scope-origin`

**Primary files:** candidate A common orchestration plus new focused files:

- `backend/internal/teamusage/origin_cache.go`
- `backend/internal/teamusage/origin_cache_test.go`
- `backend/internal/teamusage/origin.go`
- service/organization integration tests
- server cache construction and metrics

- [x] **Step 1: Replay the common A orchestration commit without merging A**

Cherry-pick the exact Candidate A implementation commits recorded in this plan,
in order.

- [x] **Step 2: Add RED tests**

Tests must prove four concurrent typed lanes load one origin, the origin performs
all stats chunks plus one users-trend request, branch projection remains
authorized, Redis payload omits names/emails/credentials, malformed values
reload, Redis errors fail open, and serialized payload size is bounded.

- [x] **Step 3: Run RED**

```bash
cd backend
go test ./internal/teamusage -run 'OriginCache|SharedOrigin|OriginPayload' -count=1 -v
```

- [x] **Step 4: Implement C**

Create a 60-second Redis origin keyed by namespace, provider/version,
scope version/hash, normalized range, granularity, and timezone. Store only
Relay IDs, stats, and validated points. Coordinate through the existing
`readcache` flight/lease pattern. Project existing Summary, Trend, Members, and
Organization snapshots without changing their response caches.

- [x] **Step 5: Verify C**

```bash
cd backend
gofmt -w internal/teamusage cmd/server
go test ./internal/readcache ./internal/relay ./internal/teamusage ./cmd/server -count=1
go test -race ./internal/readcache ./internal/relay ./internal/teamusage -count=1
```

- [x] **Step 6: Commit C, then record the exact SHA in the coordinator plan**

```bash
git add backend
git commit -m "perf(teamusage): test Redis scope origin strategy"
```

After the implementation commit exists, update this coordinator plan with its
exact SHA in a separate bookkeeping commit.

Candidate C implementation commits are
`8774521a110875df9a2535dab536493f3d4b4aaa` and
`bbac0366282c71529dc8b1772e13349f7f981d80`; final candidate head is
`bbac0366282c71529dc8b1772e13349f7f981d80`. Focused, full-backend, race,
vet, and build verification passed. Task-scoped re-review reported zero
Critical or Important findings. One non-blocking Minor remains: a legal long
day/hour window can exceed the 2 MiB payload bound and lose Redis reuse, while
the request still succeeds through the authoritative result.

---

### Task 5: Full Local Verification

- [x] **Step 1: Run the full ladder independently in A, B, and C**

```bash
cd backend
go test ./... -count=1
go test -race ./internal/readcache ./internal/relay ./internal/teamusage -count=1
go vet ./...
go build ./...
```

Record exact results per branch. A missing package in A is acceptable only when
the package/file belongs exclusively to B or C; no failed test is acceptable.

Final uncontended verification passed independently on Candidate A
`41cec4c693c0e0303eca89370f7a010a98f7e635`, Candidate B
`fe2c8d72527c8c2ab93c5559f45bf65e4b23555a`, and Candidate C
`bbac0366282c71529dc8b1772e13349f7f981d80`. Each exact head passed
`go test ./... -count=1`, the required three-package race run, `go vet ./...`,
and `go build ./...`.

Candidate A's first full run exposed one stale handler assertion that still
expected chunk-local `RequireCompleteRange=true`; a test-only RED/GREEN commit
aligned it with the reviewed all-chunks-first contract before the complete
ladder passed. Candidate B's first coordinator run hit one unrelated
quota-reset test cancellation under full-suite scheduling; the focused test
passed three consecutive runs and the exact full ladder then passed without a
code change.

- [x] **Step 2: Verify no candidate contains a Pod completed-result cache**

```bash
rg -n 'teamTrendCache|map\[.*UsageTrendPoint|completed.*trend' backend/internal/relay backend/internal/teamusage
```

Inspect every match. In-flight coordination types may remain; completed values
may exist only in Redis-backed envelopes or request-local variables.

The scan found only tests, request-local/result maps, projection inputs, the
Candidate B Redis envelope adapter, and the Candidate C Redis origin payload.
Candidate B retains only its provider-wide in-flight 24-slot limiter in Pod
state. Candidate C retains only cancellation-aware in-flight `FlightGroup`
coordination. No candidate contains `teamTrendCache` or another Pod-local
completed-result map.

---

### Task 6: Sequential Staging Matrix

- [x] **Step 1: Publish A, B, and C as exact multi-architecture staging images**

Use immutable `staging-<full-sha>` tags and record OCI index digests plus
amd64/arm64 manifests.

Published and remotely verified images:

| Candidate | Tag | OCI index | amd64 manifest | arm64 manifest |
| --- | --- | --- | --- | --- |
| A | `staging-41cec4c693c0e0303eca89370f7a010a98f7e635` | `sha256:f51bd64d9a0fac27c8c30ca40995944dbad1139e56c24488fe9e22d402d6e84a` | `sha256:c8d4669dcdb279684a55a3b24f93b60df81f4bb70b6f7c6385e16a9516b510f5` | `sha256:b770257145b5ed615d3449b344333f29e707e2bad9f84bda4fc129eb2bc82a0b` |
| B | `staging-fe2c8d72527c8c2ab93c5559f45bf65e4b23555a` | `sha256:c0abbbc8523881f45881532d2109748377742eab666b10fabd1af6233de223a9` | `sha256:6937d17ed99182158cc100ce3e3b5d3a692016e2cbb04371d2eee4edfee78bc9` | `sha256:e32b021dd840051ff05bd98b82adabec6d820a65e30ce1884154b50da6ea6213` |
| C | `staging-bbac0366282c71529dc8b1772e13349f7f981d80` | `sha256:dc1abb4a0f7441ea28abf4a45f9a2bebe075227bf59b25db3808793b7a5f530c` | `sha256:fdd1d3d7bbeb1d4e9291bf61cf0a23d04836fe03c02be1800a9d28c2f1a92613` | `sha256:727f9474f9062faf0dfab65221ab3e1701e47c1982c8dc044b76cbbb035ab126` |

- [x] **Step 2: Deploy and audit A**
- [x] **Step 3: Deploy and audit B**
- [x] **Step 4: Deploy and audit C**

For each candidate, use the existing two-phase staging playbook, clear only
its approved Redis keys plus the four response keys, verify no lease, run one
four-lane cold and one immediate warm round, and record only sanitized evidence.

Acceptance:

```text
cold <= 9.0s
each warm lane <= 1.5s
warm Relay = 0
individual trend GET = 0
Relay/cache/Redis errors = 0
cold/warm counts and aggregates equal
```

Stop a candidate after its first failed gate; do not tune it in staging before
the other candidates receive their first comparable run.

Candidate A was deployed through paused revision `35` and restore-enabled
revision `36`; the Pod ran the expected amd64 manifest
`sha256:c8d4669dcdb279684a55a3b24f93b60df81f4bb70b6f7c6385e16a9516b510f5`.
Its first comparable run failed the cold gate: the four cold lanes completed in
`17.969s`, `18.521s`, `18.167s`, and `18.166s`. Immediate warm lanes completed
in `0.557s`, `0.854s`, `0.769s`, and `0.552s`; warm Relay calls were zero.
Cold Relay deltas were 14 successful GETs and 12 successful POSTs. Time-bounded
Sub2API access logs showed four successful `/api/v1/admin/dashboard/users-trend`
requests and no individual trend route. There were no Relay, cache, or Redis
errors, no response-cache leases remained, and cold/warm payloads matched for
all four lanes. The sanitized scope contained 252 members, 236 with Relay
mappings. Candidate A is rejected without staging tuning solely because its
`18.521s` cold completion exceeds `9.0s`.

Candidate B was deployed through paused revision `37` and restore-enabled
revision `38`; the Pod ran the expected amd64 manifest
`sha256:6937d17ed99182158cc100ce3e3b5d3a692016e2cbb04371d2eee4edfee78bc9`.
Its first comparable cold lanes completed in `21.427s`, `21.581s`, `21.722s`,
and `21.529s`. Immediate warm lanes completed in `7.354s`, `0.975s`, `7.995s`,
and `7.473s`; only Trend hit its response cache. Cold Relay deltas were 11
successful GETs and 12 successful POSTs; warm still issued 7 successful GETs
and 9 successful POSTs. Sub2API access logs showed one successful aggregate
`/api/v1/admin/dashboard/users-trend` request and no individual trend route.
The Redis primitive coordinated as designed (`batch_origin=1`, one acquired
lease, three waiters, one write), but the concurrent cold round recorded one
response-cache error each for Summary, Members, and Organization. No lease
remained and no Relay error occurred. Counts and range aggregates matched;
Summary's independently recomputed today/lifetime costs differed only by
floating-point rounding at `1e-10`. Candidate B is rejected without staging
tuning because it fails the cold, warm, warm-Relay, and cache-error gates.

Candidate C was deployed through paused revision `39` and restore-enabled
revision `40`; Kubernetes resolved the expected OCI index
`sha256:dc1abb4a0f7441ea28abf4a45f9a2bebe075227bf59b25db3808793b7a5f530c`.
Its first comparable cold lanes completed in `13.303s`, `13.928s`, `14.214s`,
and `13.306s`. Immediate warm lanes completed in `0.791s`, `1.122s`, `0.948s`,
and `0.602s`; warm Relay calls were zero. Cold Relay deltas were 5 successful
GETs and 3 successful POSTs. Time-bounded Sub2API access logs showed one
successful `/api/v1/admin/dashboard/users-trend` request and no individual
trend route. The Redis scope origin recorded exactly one miss, one acquired
lease, and one refresh; all four response caches recorded one normal cold
miss/lease/refresh and one warm fresh hit. There were no Relay, cache, or Redis
errors, no lease remained, and cold/warm business payloads matched for all four
lanes. Candidate C passes every gate except cold completion and is the best
experimental reference, but its `14.214s` cold completion still exceeds `9.0s`.

- [x] **Step 5: Select the simplest passing candidate**

Selection order is A, B, C. If none passes, keep PR #160 at `f184a7b2`, record
failure, and promote nothing. If one passes, replay only the winner onto current
PR #160, run independent review and exact-head CI, then request merge/release
approval.

No candidate passed every gate: A failed cold completion; B failed cold, warm,
warm-Relay, and cache-error gates; C failed cold completion only. Per the
approved selection contract, there is no promotion winner and PR #160 remains
at the #190 baseline. Candidate C remains the measured reference for any next
optimization round rather than being treated as equivalent to the other failed
experiments.

- [x] **Step 6: Clean experiment state**

Delete temporary tokens/scripts, remove clean losing worktrees and branches,
and preserve every unrelated user worktree/change. Leave staging on the
selected candidate only when it passed; otherwise restore the #190 baseline
image and verify health.

The staging release was restored through paused revision `41` and
restore-enabled revision `42` to exact #190 image
`staging-55302b62c795054c56a700c6cb817eac06c49a5b`. Staging live/ready checks
passed with database, Redis, and Relay up. Production remained healthy at
revision `69` on `v0.1.0-preview.73`. The final Redis scan found zero response,
origin, primitive, or lease keys from the experiment. The static Helm selector
was committed and pushed to Helm `main` as `6b7a0a5`.

All session-only tokens and audit artifacts were deleted. Clean Candidate A and
B worktrees and local branches were removed; no matching remote branches
existed. Candidate C's clean branch/worktree is intentionally retained as the
measured next-round baseline. The unrelated dirty single-aggregate worktree and
all pre-existing main-checkout changes were preserved.
