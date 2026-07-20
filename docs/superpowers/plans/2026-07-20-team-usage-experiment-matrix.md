# Team Usage Cold-Load Experiment Matrix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to execute this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement and stage-test three mutually exclusive no-Pod-cache Team Usage cold-loading strategies from one stable PR #190 baseline.

**Architecture:** All candidates move range completion above stats chunks and use Sub2API users-trend over the complete authorized scope. A relies on Sub2API's own cache, B adds per-user Redis primitives, and C adds one Redis scope-origin snapshot; identical staging gates decide the winner.

**Tech Stack:** Go 1.24, Redis/go-redis, miniredis, Gin, Ent-backed Team Usage scope, Sub2API HTTP, Docker buildx, Helm, Kubernetes, Prometheus metrics.

**Status:** Experiment contract written; candidate worktrees and implementations remain.

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

- [ ] **Step 2: Commit the matrix contract**

```bash
git add docs/superpowers/specs/2026-07-20-team-usage-experiment-matrix-design.md docs/superpowers/plans/2026-07-20-team-usage-experiment-matrix.md
git commit -m "docs(teamusage): define cold-load experiment matrix"
```

- [ ] **Step 3: Create three ignored linked worktrees from the matrix commit**

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

- [ ] **Step 1: Add RED tests**

Tests must prove all stats chunks finish before one full-scope trend call,
Organization uses branch IDs for stats but full represented IDs for trend,
range completion remains soft, and `sub2apiRelay` has no completed-value map.
An HTTP adapter test must assert the only trend route used is
`/api/v1/admin/dashboard/users-trend`.

- [ ] **Step 2: Run RED**

```bash
cd backend
go test ./internal/teamusage -run 'CompleteRange|Organization.*Aggregate|Members.*Aggregate' -count=1 -v
go test ./internal/relay -run 'UsersTrendBatch|NoPodTrendCache' -count=1 -v
```

Expected: failures are caused by chunk-local fallback and the existing Pod
cache, not malformed fixtures.

- [ ] **Step 3: Implement A**

Remove `teamTrendCache`. Add one validated users-trend HTTP adapter with the
existing bounded limit and authorization filtering. Move complete-range policy
to Team Usage after all stats chunks. Do not add an AI Efficiency Redis
primitive or aggregate value.

- [ ] **Step 4: Verify A**

```bash
cd backend
gofmt -w internal/relay internal/teamusage
go test ./internal/relay ./internal/teamusage -count=1
go test -race ./internal/relay ./internal/teamusage -count=1
go vet ./internal/relay ./internal/teamusage
```

- [ ] **Step 5: Commit A and record the exact SHA in this plan**

```bash
git add backend docs/superpowers/plans/2026-07-20-team-usage-experiment-matrix.md
git commit -m "perf(teamusage): test upstream aggregate cache strategy"
```

---

### Task 3: Candidate B - Redis Per-User Primitives

**Worktree:** `/Users/admin/ai-efficiency/.worktrees/team-usage-redis-primitives`

**Primary files:** candidate A files plus:

- `backend/internal/readcache/multi_store.go`
- `backend/internal/relay/sub2api_team_trend_redis.go`
- integration and Redis primitive tests
- server cache metrics/provider construction

- [ ] **Step 1: Replay the common A orchestration commit without merging A**

Use `git cherry-pick` with the exact Candidate A implementation commit recorded
in this plan. Resolve no unrelated files; B must contain the same common
range-completion behavior before adding Redis.

- [ ] **Step 2: Add RED tests**

Tests must prove two Pods with the same complete requested set but different
current miss subsets produce one users-trend request; waiters make no MGET
while lease TTL is positive; Personal Usage can warm one matching user; and
only authorized requested IDs are stored or returned.

- [ ] **Step 3: Run RED**

```bash
cd backend
go test ./internal/readcache ./internal/relay -run 'Multi|FullRequestLease|Waiter|WriteThrough' -count=1 -v
```

- [ ] **Step 4: Implement B**

Add ordered MGET/pipelined SET, per-user 60-second envelopes, a 15-second
full-request lease, 100-millisecond TTL-only waiting, one post-release MGET,
12-second users-trend origin, current truncation behavior, and 24-slot
individual fallback. Do not add a Pod result map.

- [ ] **Step 5: Verify B**

```bash
cd backend
gofmt -w internal/readcache internal/relay cmd/server
go test ./internal/readcache ./internal/relay ./internal/relayruntime ./internal/teamusage ./internal/personalusage ./cmd/server -count=1
go test -race ./internal/readcache ./internal/relay ./internal/teamusage ./internal/personalusage -count=1
```

- [ ] **Step 6: Commit B and record the exact SHA in this plan**

```bash
git add backend docs/superpowers/plans/2026-07-20-team-usage-experiment-matrix.md
git commit -m "perf(teamusage): test Redis primitive strategy"
```

---

### Task 4: Candidate C - Redis Scope-Origin Snapshot

**Worktree:** `/Users/admin/ai-efficiency/.worktrees/team-usage-scope-origin`

**Primary files:** candidate A common orchestration plus new focused files:

- `backend/internal/teamusage/origin_cache.go`
- `backend/internal/teamusage/origin_cache_test.go`
- `backend/internal/teamusage/origin.go`
- service/organization integration tests
- server cache construction and metrics

- [ ] **Step 1: Replay the common A orchestration commit without merging A**

Cherry-pick the exact Candidate A implementation commit recorded in this plan.

- [ ] **Step 2: Add RED tests**

Tests must prove four concurrent typed lanes load one origin, the origin performs
all stats chunks plus one users-trend request, branch projection remains
authorized, Redis payload omits names/emails/credentials, malformed values
reload, Redis errors fail open, and serialized payload size is bounded.

- [ ] **Step 3: Run RED**

```bash
cd backend
go test ./internal/teamusage -run 'OriginCache|SharedOrigin|OriginPayload' -count=1 -v
```

- [ ] **Step 4: Implement C**

Create a 60-second Redis origin keyed by namespace, provider/version,
scope version/hash, normalized range, granularity, and timezone. Store only
Relay IDs, stats, and validated points. Coordinate through the existing
`readcache` flight/lease pattern. Project existing Summary, Trend, Members, and
Organization snapshots without changing their response caches.

- [ ] **Step 5: Verify C**

```bash
cd backend
gofmt -w internal/teamusage cmd/server
go test ./internal/readcache ./internal/relay ./internal/teamusage ./cmd/server -count=1
go test -race ./internal/readcache ./internal/relay ./internal/teamusage -count=1
```

- [ ] **Step 6: Commit C and record the exact SHA in this plan**

```bash
git add backend docs/superpowers/plans/2026-07-20-team-usage-experiment-matrix.md
git commit -m "perf(teamusage): test Redis scope origin strategy"
```

---

### Task 5: Full Local Verification

- [ ] **Step 1: Run the full ladder independently in A, B, and C**

```bash
cd backend
go test ./... -count=1
go test -race ./internal/readcache ./internal/relay ./internal/teamusage -count=1
go vet ./...
go build ./...
```

Record exact results per branch. A missing package in A is acceptable only when
the package/file belongs exclusively to B or C; no failed test is acceptable.

- [ ] **Step 2: Verify no candidate contains a Pod completed-result cache**

```bash
rg -n 'teamTrendCache|map\[.*UsageTrendPoint|completed.*trend' backend/internal/relay backend/internal/teamusage
```

Inspect every match. In-flight coordination types may remain; completed values
may exist only in Redis-backed envelopes or request-local variables.

---

### Task 6: Sequential Staging Matrix

- [ ] **Step 1: Publish A, B, and C as exact multi-architecture staging images**

Use immutable `staging-<full-sha>` tags and record OCI index digests plus
amd64/arm64 manifests.

- [ ] **Step 2: Deploy and audit A**
- [ ] **Step 3: Deploy and audit B**
- [ ] **Step 4: Deploy and audit C**

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

- [ ] **Step 5: Select the simplest passing candidate**

Selection order is A, B, C. If none passes, keep PR #160 at `f184a7b2`, record
failure, and promote nothing. If one passes, replay only the winner onto current
PR #160, run independent review and exact-head CI, then request merge/release
approval.

- [ ] **Step 6: Clean experiment state**

Delete temporary tokens/scripts, remove clean losing worktrees and branches,
and preserve every unrelated user worktree/change. Leave staging on the
selected candidate only when it passed; otherwise restore the #190 baseline
image and verify health.
