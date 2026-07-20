# Team Usage Redis Batch Trend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Pod-local Team Usage trend result cache with a cross-Pod Redis cache, fill it through Sub2API's users-trend batch API, and let authenticated personal usage origins warm the same per-Relay-user entries.

**Architecture:** `backend/internal/readcache` provides ordered bulk reads and pipelined writes. A private Relay primitive cache owns provider/version/user/range keys, validation, TTL, metrics, and distributed leases; the Sub2API adapter performs batch-first loading and retains the existing 24-slot individual fallback. Personal Usage writes only a reduced authenticated trend projection, while Team Usage authorization and its four response caches remain unchanged.

**Tech Stack:** Go 1.24, Redis via `github.com/redis/go-redis/v9`, `miniredis`, Gin HTTP adapters, zap structured logging, Prometheus cache metrics.

**Status:** In progress. Tasks 1-3 are complete; Tasks 4-7 remain.

## Global Constraints

- Work only in `/Users/admin/ai-efficiency/.worktrees/team-usage-batch-trend` on `perf/team-usage-batch-trend`, targeting `feat/platform-loading-performance`.
- Do not modify Sub2API source or introduce direct Sub2API database access.
- Do not retain completed Team Usage trend values in Pod memory.
- Redis cache identity is deployment namespace, schema version, provider ID, provider configuration version, Relay user ID, normalized start/end dates, granularity, and timezone; it never includes the requesting actor.
- Redis values contain only schema/version dimensions, `generated_at`, and points with `date`, `actual_cost`, and optional `total_tokens`.
- The primitive TTL is exactly 60 seconds and has no stale-if-error behavior.
- Batch loading starts at two misses. The limit is `min(max(total requested unique positive Relay IDs + 250, 500), 5000)`.
- A batch with fewer unique users than its limit is complete; exactly the limit is possibly truncated; more than the limit or duplicate `(user_id, date)` rows is invalid.
- The batch origin timeout is 12 seconds, lease TTL is 15 seconds, Redis command timeout is 100 milliseconds, and lease polling interval is 25 milliseconds.
- Individual fallback remains subject to one provider-wide 24-slot limiter and preserves the current fail-fast error contract.
- Redis is fail-open. Redis failures may duplicate upstream work but cannot grant access, cache errors, or turn a successful origin into a failed API response.
- Authorization is resolved before Team Usage reaches Relay. Filter batch rows to requested Relay IDs before caching or returning them.
- Never cache or log emails, usernames, Relay ID lists, passwords, tokens, raw batch bodies, or other real user data.
- Keep the existing Team Usage DTOs, response-cache keys, fresh/stale windows, cursor contracts, and frontend behavior unchanged.
- Update `docs/architecture.md` only in the task that makes the Redis runtime contract true.
- After every completed task, update this plan's checkboxes in the same commit or the immediately following bookkeeping commit.

---

## File Map

- `backend/internal/readcache/multi_store.go`: optional ordered `MGET` and pipelined `SET` contract plus `RedisStore` implementation.
- `backend/internal/readcache/store_test.go`: Redis bulk ordering, TTL, retry, cancellation, and no-write-retry tests.
- `backend/internal/relay/sub2api_team_trend_redis.go`: Redis key/envelope validation, bulk read/write, metrics, and distributed lease helpers.
- `backend/internal/relay/sub2api_team_trend_redis_test.go`: primitive cache identity, freshness, isolation, lease, and failure tests.
- `backend/internal/relay/sub2api_team_trend_batch.go`: private Sub2API users-trend request/response adapter and truncation classification.
- `backend/internal/relay/sub2api_team_trend_batch_test.go`: batch query, mapping, filtering, malformed response, and limit tests.
- `backend/internal/relay/sub2api_team_trend.go`: batch-first `GetUsageTrendForUsers`, lease waiting, and individual fallback orchestration.
- `backend/internal/relay/sub2api.go`: provider construction and personal-origin write-through hooks; no Team Usage orchestration remains in this already large file.
- `backend/internal/relay/sub2api_team_trend_cache.go`: delete the old completed-value Pod cache.
- `backend/internal/relay/sub2api_team_trend_cache_test.go`: rename to `sub2api_team_trend_limiter_test.go` and retain only limiter coverage.
- `backend/internal/relay/sub2api_test.go`: public adapter compatibility and Personal Usage write-through integration coverage.
- `backend/internal/telemetry/metrics.go`: allow the primitive-specific stable cache outcomes.
- `backend/internal/telemetry/metrics_test.go`: prove new outcomes are accepted without new labels.
- `backend/cmd/server/cache_metrics.go`: bind the privacy-safe `relay_user_trend` recorder.
- `backend/cmd/server/cache_metrics_test.go`: lock production recorder registration.
- `backend/cmd/server/main.go`: inject Redis, namespace, provider/version, and metrics into DB-backed Sub2API providers.
- `docs/architecture.md`: replace the process-local trend-cache description with the implemented Redis batch-first contract and Pod-local state classification.
- `docs/superpowers/specs/2026-07-20-team-usage-redis-batch-trend-design.md`: mark implemented only after all local verification passes.

---

### Task 1: Add Ordered Redis Bulk Operations

**Files:**
- Create: `backend/internal/readcache/multi_store.go`
- Modify: `backend/internal/readcache/store_test.go`

**Interfaces:**
- Consumes: existing `readcache.Store` and `readcache.RedisStore`.
- Produces:

```go
type SetItem struct {
	Key   string
	Value []byte
	TTL   time.Duration
}

type MultiStore interface {
	Store
	MGet(ctx context.Context, keys []string) ([][]byte, error)
	SetMany(ctx context.Context, items []SetItem) error
}
```

- `MGet` returns one element per input key in the same order; a Redis miss is a nil element, not `ErrMiss`.
- `MGet` retries one non-cancellation command error. `SetMany` executes once and does not retry.

- [x] **Step 1: Add failing ordered-read and per-item TTL tests**

Append tests that seed `first` and `third`, leave `second` absent, and assert exact positional output and TTLs:

```go
func TestRedisStoreMGetPreservesOrderAndMisses(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRedisStore(client)
	ctx := context.Background()

	err := store.SetMany(ctx, []SetItem{
		{Key: "first", Value: []byte("one"), TTL: 11 * time.Second},
		{Key: "third", Value: []byte("three"), TTL: 13 * time.Second},
	})
	if err != nil {
		t.Fatalf("SetMany() error = %v", err)
	}
	values, err := store.MGet(ctx, []string{"first", "second", "third"})
	if err != nil {
		t.Fatalf("MGet() error = %v", err)
	}
	got := []string{string(values[0]), string(values[1]), string(values[2])}
	if !reflect.DeepEqual(got, []string{"one", "", "three"}) {
		t.Fatalf("MGet() = %#v", got)
	}
	if values[1] != nil {
		t.Fatalf("missing value = %#v, want nil", values[1])
	}
	if server.TTL("first") != 11*time.Second || server.TTL("third") != 13*time.Second {
		t.Fatalf("unexpected TTLs: first=%s third=%s", server.TTL("first"), server.TTL("third"))
	}
}

func TestRedisStoreBulkOperationsAcceptEmptyInput(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRedisStore(client)
	values, err := store.MGet(context.Background(), nil)
	if err != nil || len(values) != 0 {
		t.Fatalf("MGet(nil) = %#v, %v", values, err)
	}
	if err := store.SetMany(context.Background(), nil); err != nil {
		t.Fatalf("SetMany(nil) error = %v", err)
	}
}
```

- [x] **Step 2: Run the focused tests and verify RED**

Run:

```bash
cd backend
go test ./internal/readcache -run 'TestRedisStore(MGet|Bulk)' -count=1
```

Expected: build failure because `SetItem`, `SetMany`, and `MGet` do not exist.

- [x] **Step 3: Implement `MultiStore` in a focused file**

Create `backend/internal/readcache/multi_store.go`:

```go
package readcache

import (
	"context"
	"fmt"
	"time"

	redis "github.com/redis/go-redis/v9"
)

type SetItem struct {
	Key   string
	Value []byte
	TTL   time.Duration
}

type MultiStore interface {
	Store
	MGet(ctx context.Context, keys []string) ([][]byte, error)
	SetMany(ctx context.Context, items []SetItem) error
}

func (s *RedisStore) MGet(ctx context.Context, keys []string) ([][]byte, error) {
	if len(keys) == 0 {
		return [][]byte{}, nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		values, err := s.client.MGet(ctx, keys...).Result()
		if err == nil {
			out := make([][]byte, len(values))
			for index, value := range values {
				switch typed := value.(type) {
				case nil:
				case string:
					out[index] = []byte(typed)
				case []byte:
					out[index] = append([]byte(nil), typed...)
				default:
					return nil, fmt.Errorf("decode Redis MGET value %d: unsupported type %T", index, value)
				}
			}
			return out, nil
		}
		if ctx.Err() != nil || attempt == 1 {
			return nil, err
		}
	}
	panic("unreachable")
}

func (s *RedisStore) SetMany(ctx context.Context, items []SetItem) error {
	if len(items) == 0 {
		return nil
	}
	_, err := s.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, item := range items {
			pipe.Set(ctx, item.Key, item.Value, item.TTL)
		}
		return nil
	})
	return err
}
```

Add `reflect` to `store_test.go` imports.

- [x] **Step 4: Add retry, cancellation, and write-no-retry tests**

Add `TestRedisStoreMGetRetriesOneCommandError` using the existing scripted hook
with one synthetic `mget` failure, then assert the second attempt returns the
seeded value and `hook.callCount("mget") == 2`. Add
`TestRedisStoreMGetStopsAfterCancellation`; cancel from `hook.after` on the
first attempt and assert exactly one call. Extend `ProcessPipelineHook` with a
counted synthetic `pipeline` failure and assert `SetMany` executes once without
retry.

- [x] **Step 5: Run and format the read-cache package**

```bash
cd backend
gofmt -w internal/readcache/multi_store.go internal/readcache/store_test.go
go test ./internal/readcache -count=1
```

Expected: `ok github.com/ai-efficiency/backend/internal/readcache`.

- [x] **Step 6: Commit Task 1**

```bash
git add backend/internal/readcache/multi_store.go backend/internal/readcache/store_test.go docs/superpowers/plans/2026-07-20-team-usage-redis-batch-trend.md
git commit -m "feat(backend): add Redis bulk read cache operations"
```

---

### Task 2: Build The Redis Per-Relay-User Trend Primitive

**Files:**
- Create: `backend/internal/relay/sub2api_team_trend_redis.go`
- Create: `backend/internal/relay/sub2api_team_trend_redis_test.go`

**Interfaces:**
- Consumes: `readcache.MultiStore`, `readcache.SetItem`, and `readcache.Metrics`.
- Produces private methods:

```go
func newTeamTrendRedisCache(options teamTrendRedisCacheOptions) (*teamTrendRedisCache, error)
func (c *teamTrendRedisCache) Read(ctx context.Context, userIDs []int64, params TeamMemberTrendParams) (map[int64][]UsageTrendPoint, []int64, error)
func (c *teamTrendRedisCache) Write(ctx context.Context, values map[int64][]UsageTrendPoint, params TeamMemberTrendParams, source string) error
func (c *teamTrendRedisCache) TryAcquireBatchLease(ctx context.Context, userIDs []int64, params TeamMemberTrendParams, limit int) (leaseKey, token string, acquired bool, err error)
func (c *teamTrendRedisCache) LeaseTTL(ctx context.Context, leaseKey string) (time.Duration, error)
func (c *teamTrendRedisCache) ReleaseBatchLease(leaseKey, token string)
```

- [x] **Step 1: Write failing identity, TTL, malformed, and clone tests**

Create two cache instances backed by the same miniredis server and a deterministic
clock. Write user 101 through the first, read users 101 and 102 through the
second, and assert 101 is a hit and 102 a miss. Mutate the returned cost and
token pointer, reread, and prove the stored value is unchanged. Generate variants
for provider ID, provider version, Relay user ID, start, end, granularity, and
timezone and prove no collision. Advance the injected clock to exactly 60
seconds and prove the envelope is rejected even when the Redis key remains.
Insert malformed JSON and a future `generated_at`; both must be misses and
record `malformed`.

- [x] **Step 2: Run the primitive tests and verify RED**

```bash
cd backend
go test ./internal/relay -run '^TestTeamTrendRedisCache' -count=1
```

Expected: build failure because `teamTrendRedisCache` does not exist.

- [x] **Step 3: Implement normalized keys and versioned envelopes**

Use these exact constants and fields:

```go
const (
	teamTrendRedisSchemaVersion  = 1
	teamTrendRedisTTL            = time.Minute
	teamTrendRedisCommandTimeout = 100 * time.Millisecond
	teamTrendRedisLeaseTTL       = 15 * time.Second
	teamTrendRedisPollInterval   = 25 * time.Millisecond
)

type teamTrendRedisCacheOptions struct {
	Store           readcache.MultiStore
	Namespace       string
	ProviderID      int
	ProviderVersion int64
	Metrics         readcache.Metrics
	Now             func() time.Time
	NewToken        func() string
	Sleep           func(context.Context, time.Duration) error
}

type teamTrendRedisEnvelope struct {
	SchemaVersion   int               `json:"schema_version"`
	ProviderID      int               `json:"provider_id"`
	ProviderVersion int64             `json:"provider_configuration_version"`
	RelayUserID     int64             `json:"relay_user_id"`
	StartDate       string            `json:"start_date"`
	EndDate         string            `json:"end_date"`
	Granularity     string            `json:"granularity"`
	Timezone        string            `json:"timezone"`
	GeneratedAt     time.Time         `json:"generated_at"`
	Points          []UsageTrendPoint `json:"points"`
}
```

Normalize trimmed dates/timezone and lowercased trimmed granularity. Hash the
NUL-delimited canonical dimensions with SHA-256 and format the Redis key as
`<namespace>:relay-user-trend:v1:<hex digest>`. Decode with unknown-field and
trailing-content rejection. Validate every envelope dimension, positive user
ID, nonfuture generation, age below 60 seconds, nonblank point dates, finite
cost, and nonnegative optional tokens. Clone slices and token pointers.

- [x] **Step 4: Implement ordered bulk read/write and metrics**

`Read` deduplicates positive IDs in input order, executes one `MGet` under the
command timeout, decodes entries independently, and returns sorted misses. A
Redis error returns every positive ID as a miss plus the error. `Write` encodes
one envelope per user, uses one `SetMany` with exact one-minute TTLs, and records
`write` plus the supplied source only after success. Encoding or Redis failure
records `error`. No actor or user profile field is accepted by these APIs.

- [x] **Step 5: Implement token-protected lease helpers and tests**

Lease identity includes provider/version, normalized range, limit, and a SHA-256
digest of sorted unique missing Relay IDs. Acquire with UUID token and 15-second
TTL. Test two instances cannot acquire the same lease, different missing sets
do not collide, the wrong token cannot release, and detached 100-millisecond
release still runs after caller cancellation.

- [x] **Step 6: Run and format primitive tests**

```bash
cd backend
gofmt -w internal/relay/sub2api_team_trend_redis.go internal/relay/sub2api_team_trend_redis_test.go
go test ./internal/relay -run 'TestTeamTrend(RedisCache|Origin)' -count=1
```

Expected: the new Redis primitive and unchanged existing Relay tests pass. The
old Pod cache remains untouched until Task 4 can remove it atomically with the
orchestration switch.

- [x] **Step 7: Commit Task 2**

```bash
git add backend/internal/relay/sub2api_team_trend_redis.go backend/internal/relay/sub2api_team_trend_redis_test.go docs/superpowers/plans/2026-07-20-team-usage-redis-batch-trend.md
git commit -m "feat(backend): add Redis Relay trend primitive"
```

---

### Task 3: Add The Private Sub2API Users-Trend Batch Adapter

**Files:**
- Create: `backend/internal/relay/sub2api_team_trend_batch.go`
- Create: `backend/internal/relay/sub2api_team_trend_batch_test.go`

**Interfaces:**
- Produces:

```go
type teamTrendBatchResult struct {
	PointsByUser    map[int64][]UsageTrendPoint
	UniqueUserCount int
	Complete        bool
}

func teamTrendBatchLimit(totalRequested int) int
func (s *sub2apiRelay) getTeamTrendBatch(ctx context.Context, requestedUserIDs []int64, params TeamMemberTrendParams, limit int) (teamTrendBatchResult, error)
```

- [x] **Step 1: Write failing limit and mapping tests**

Table-test limits `(0,500)`, `(2,500)`, `(235,500)`, `(400,650)`,
`(5000,5000)`, and `(9000,5000)`. Serve one successful envelope containing
requested users 101/102 plus out-of-scope 999. Assert the outbound query sends
the exact start/end/granularity/timezone/limit, unique count is three,
`Complete=true` for limit 500, output contains only 101/102, dates are sorted,
and `tokens` became independent `TotalTokens` pointers.

- [x] **Step 2: Run batch tests and verify RED**

```bash
cd backend
go test ./internal/relay -run '^TestTeamTrendBatch' -count=1
```

Expected: build failure because the batch types and methods do not exist.

- [x] **Step 3: Implement bounded private DTO decoding**

Use private DTOs containing only `date`, `user_id`, `tokens`, and `actual_cost`.
Issue one authenticated GET to `/api/v1/admin/dashboard/users-trend`. Send the
five required query values. Read through a 64-MiB `io.LimitReader`, require HTTP
200 and a successful envelope, and reject a response that reaches the bound.

Count unique users before filtering. Reject non-positive IDs, blank dates,
negative tokens, NaN/Inf costs, more unique users than limit, duplicate
`(user_id,date)` rows, malformed JSON, and trailing JSON. Filter through the
requested-ID set before building output. Set `Complete` only when unique count
is less than limit.

- [x] **Step 4: Add truncation, malformed, filtering, and bound tests**

Prove exactly `limit` yields `Complete=false`; fewer yields true; out-of-scope
rows affect unique count but never output; duplicate keys fail; invalid fields,
unsuccessful envelope, non-200, malformed/trailing/oversized bodies fail; and
cancellation produces only one attempted HTTP request.

- [x] **Step 5: Run and format batch tests**

```bash
cd backend
gofmt -w internal/relay/sub2api_team_trend_batch.go internal/relay/sub2api_team_trend_batch_test.go
go test ./internal/relay -run '^TestTeamTrendBatch' -count=1
```

Expected: all batch adapter tests pass.

- [x] **Step 6: Commit Task 3**

```bash
git add backend/internal/relay/sub2api_team_trend_batch.go backend/internal/relay/sub2api_team_trend_batch_test.go docs/superpowers/plans/2026-07-20-team-usage-redis-batch-trend.md
git commit -m "feat(backend): add Sub2API user trend batch adapter"
```

---

### Task 4: Implement Batch-First Team Trend Loading And Cross-Pod Collapse

**Files:**
- Create: `backend/internal/relay/sub2api_team_trend.go`
- Create: `backend/internal/relay/sub2api_team_trend_integration_test.go`
- Modify: `backend/internal/relay/sub2api.go`
- Modify: `backend/internal/relay/sub2api_test.go`
- Delete: `backend/internal/relay/sub2api_team_trend_cache.go`
- Rename: `backend/internal/relay/sub2api_team_trend_cache_test.go` to `backend/internal/relay/sub2api_team_trend_limiter_test.go`

**Interfaces:**
- Consumes Task 2's Redis cache and Task 3's batch adapter.
- Produces the unchanged public capability:

```go
func (s *sub2apiRelay) GetUsageTrendForUsers(ctx context.Context, relayUserIDs []int64, params TeamMemberTrendParams) (map[int64][]UsageTrendPoint, error)
```

- [ ] **Step 1: Write failing all-hit, one-miss, and batch-first tests**

Construct providers with a shared miniredis cache and an HTTP server that counts
`/users-trend` and `/trend?user_id=...` separately. Seed two users and assert an
all-hit call makes zero HTTP requests. Request one uncached user and assert one
individual request and zero batches. Request two uncached users and assert one
batch and zero individual requests.

For the two-miss batch, return one requested user plus unrelated users with a
unique count below the limit. Assert the returned user has data, the absent
requested user has a successful empty slice, and a second call reads both from
Redis without HTTP.

- [ ] **Step 2: Run orchestration tests and verify RED**

```bash
cd backend
go test ./internal/relay -run '^TestSub2APITeamTrendRedis' -count=1
```

Expected: assertion failure because the current method still uses the Pod cache
and individual fan-out rather than Redis and batch-first orchestration.

- [ ] **Step 3: Move orchestration out of `sub2api.go` and implement cache-first flow**

Move `GetUsageTrendForUsers` and `getTeamMemberTrend` into
`sub2api_team_trend.go`. Use this exact control shape:

```go
requested := uniqueTeamTrendUserIDs(relayUserIDs)
hits, misses := s.readTeamTrendCacheFailOpen(ctx, requested, params)
if len(misses) == 0 {
	return cloneUsageTrendMap(hits), nil
}
if len(misses) >= 2 {
	resolved, unresolved, err := s.loadTeamTrendBatchMisses(ctx, requested, misses, params)
	if err != nil {
		return nil, err
	}
	mergeUsageTrendMap(hits, resolved)
	misses = unresolved
}
fallback, err := s.loadIndividualTeamTrendMisses(ctx, misses, params)
if err != nil {
	return nil, err
}
mergeUsageTrendMap(hits, fallback)
return cloneUsageTrendMap(hits), nil
```

`uniqueTeamTrendUserIDs` deduplicates without silently dropping non-positive
IDs. Positive IDs enter Redis and batch loading; non-positive IDs bypass both
and retain the existing individual-origin behavior. `readTeamTrendCacheFailOpen`
treats nil cache or Redis error as all positive IDs missing, logs only provider
ID/count/error class, and checks cancellation before origins. The individual
loader retains the worker pool and provider-wide limiter. It caches successful
positive-ID values, including empty results. One error cancels sibling work and
returns the existing failure contract.

In the same step, change `sub2apiRelay.teamTrends` from `teamTrendCache` to
`*teamTrendRedisCache`, remove `SetAdminAPIKey`'s Pod-cache invalidation, delete
`sub2api_team_trend_cache.go`, and rename its test file. Remove only tests for
completed entries, capacity, expiry, generations, and Pod-local result reuse;
retain all provider-wide limiter and cancellation coverage. Moving these
changes together keeps both the Task 3 and Task 4 commits compilable.

- [ ] **Step 4: Implement lease-holder and waiter paths**

For two or more misses:

1. With no Redis cache, run one batch directly under a 12-second child context.
2. Try the exact batch lease.
3. On lease command failure, record `lease_failed` and run the batch directly.
4. The holder double-checks Redis, batches remaining misses, caches returned
   requested users, and caches absent requested users only when `Complete=true`.
5. A waiter records `lease_wait`, polls Redis every 25 milliseconds, checks
   lease TTL, and retries acquisition only while its caller has time.
6. Batch failure leaves unresolved users for the individual fallback.
7. Caller cancellation returns immediately and starts no later fallback.

Always release with the acquired token. Never cache users outside the requested
miss set. Never construct a partial successful return after an individual
fallback fails. Record `lease_acquired`/`lease_wait`/`lease_failed` at their
actual transitions, `batch_origin` exactly once before each batch HTTP request,
`possible_truncation` once for an exactly full batch, and
`individual_fallback` once when at least one unresolved user enters fallback.

- [ ] **Step 5: Add truncation, failure, and cancellation tests**

Prove a possibly truncated batch caches returned requested users and individually
loads only absent users. Prove batch 500, transport, and decode failures
individually load every miss. Inject MGET, pipeline, SETNX, PTTL, and release
failures and prove fail-open behavior. Cancel while waiting for a lease and
during a batch; neither path may start a later origin. Mutate a returned map,
slice, and token pointer and prove a later Redis hit is unchanged.

- [ ] **Step 6: Add a two-provider cross-Pod lease test**

Create two independent providers with separate `RedisStore` wrappers against
one miniredis server and identical provider/version options. Start identical
two-user calls simultaneously, block the first batch, let the second observe
the lease, then release. Assert exactly one batch, zero individual calls, and
equal independent results. Repeat with different missing-ID sets to prove lease
identity isolation, then prove overlapping users reuse per-user Redis entries.

- [ ] **Step 7: Replace old public adapter fan-out expectations**

Update `TestSub2APITeamUsageTrendForUsersFansOutByUserID` to expect one
`/users-trend` request for two users through the simple uncached constructor.
Add a one-user test that expects `/dashboard/trend?user_id=...`. Preserve the
individual concurrency test by returning a synthetic batch 500 before its two
individual handlers synchronize.

- [ ] **Step 8: Run focused Relay tests and race detection**

```bash
cd backend
gofmt -w internal/relay/sub2api.go internal/relay/sub2api_team_trend.go internal/relay/sub2api_team_trend_integration_test.go internal/relay/sub2api_test.go
go test ./internal/relay -run 'TeamTrend|TeamUsageTrendForUsers' -count=1
go test -race ./internal/relay -run 'TeamTrend|TeamUsageTrendForUsers' -count=1
```

Expected: both commands pass; an untruncated two-user cold test records one batch and no individual trend GET.

- [ ] **Step 9: Commit Task 4**

```bash
git add -A backend/internal/relay docs/superpowers/plans/2026-07-20-team-usage-redis-batch-trend.md
git commit -m "perf(backend): batch cold Team Usage trends"
```

---

### Task 5: Add Personal Usage Write-Through And Production Wiring

**Files:**
- Modify: `backend/internal/relay/sub2api.go`
- Modify: `backend/internal/relay/sub2api_test.go`
- Modify: `backend/internal/telemetry/metrics.go`
- Modify: `backend/internal/telemetry/metrics_test.go`
- Modify: `backend/cmd/server/cache_metrics.go`
- Modify: `backend/cmd/server/cache_metrics_test.go`
- Modify: `backend/cmd/server/main.go`
- Test: `backend/internal/relayruntime/manager_test.go`

**Interfaces:**
- Produces:

```go
type Sub2apiProviderOptions struct {
	TeamTrendStore              readcache.MultiStore
	CacheNamespace              string
	ProviderID                  int
	ProviderConfigurationVersion int64
	TeamTrendMetrics            readcache.Metrics
}

func NewSub2apiProviderWithOptions(httpClient *http.Client, baseURL, apiKey, model string, logger *zap.Logger, options Sub2apiProviderOptions) (Provider, error)
```

- Existing `NewSub2apiProvider` remains source-compatible and uncached, while
  retaining batch-first origin behavior.

- [ ] **Step 1: Write failing constructor and production-recorder tests**

Reject configured stores with invalid namespace, non-positive provider ID, or
non-positive provider version. Prove the simple constructor remains usable and
allocates no Pod result cache. Add `relay_user_trend` to server cache metrics
`wantNames`. In a runtime construction test, resolve two providers configured
against one miniredis and prove provider ID/version determine shared keys.

- [ ] **Step 2: Run constructor and wiring tests and verify RED**

```bash
cd backend
go test ./internal/relay ./internal/relayruntime ./cmd/server -run 'Sub2apiProviderOptions|RelayUserTrend|ProductionCacheMetrics' -count=1
```

Expected: failure because the options constructor and recorder do not exist.

- [ ] **Step 3: Implement provider options and inject production Redis**

Make `NewSub2apiProvider` delegate to a private base constructor without cache.
Make `NewSub2apiProviderWithOptions` enforce all-or-none cache fields and call
`newTeamTrendRedisCache`. Add `relayUserTrend readcache.Metrics` to
`productionCacheMetrics`, initialize `CacheRecorder("relay_user_trend")`, and
pass the exact store/namespace/row ID/configuration version/recorder from
`main.go`'s DB-backed provider factory. Leave the legacy startup provider on the
simple constructor because it lacks persisted provider identity and is not the
Team Usage resolver.

- [ ] **Step 4: Add failing Personal Usage write-through tests**

Use an options-backed provider and mocked login/me/stats/trend/models endpoints.
Call `ReadUserUsageOrigin` for positive Relay ID 101, then call
`GetUsageTrendForUsers` for 101 and assert no admin batch or individual trend
request occurred. Assert reduced date/cost/token equality. Add cases for Relay
identity mismatch, successful trend plus failed stats/models, cancellation,
Redis write failure, and a miniredis scan proving no email, username, password,
or bearer token is present in keys or values.

- [ ] **Step 5: Implement best-effort personal projection**

Retain the authenticated user outside the initial login block. After branch
collection, write only when the request Relay ID is positive, authenticated ID
matches, trend succeeded, trend is nonnil, request is not canceled, and Redis
cache exists. Project exactly:

```go
func projectPersonalTrend(points []UserUsageTrendPoint) []UsageTrendPoint {
	out := make([]UsageTrendPoint, len(points))
	for index, point := range points {
		tokens := point.TotalTokens
		out[index] = UsageTrendPoint{
			Date:        point.Date,
			ActualCost:  point.ActualCost,
			TotalTokens: &tokens,
		}
	}
	return out
}
```

Convert request params to `TeamMemberTrendParams` and write with source
`personal_write_through`. Log only provider ID and error class. Ignore the Redis
error when forming the personal result. A successful trend may write even when
an independent stats or models branch sets `UsageErr`.

- [ ] **Step 6: Permit primitive-specific stable metric outcomes**

Extend `telemetry.cacheOutcomes` with `malformed`, `write`, `batch_origin`,
`individual_fallback`, `possible_truncation`, and `personal_write_through`.
Extend metrics tests to record every outcome through
`CacheRecorder("relay_user_trend")` and assert counters retain exactly the
existing `cache` and `outcome` labels. Add no identity or range labels.

- [ ] **Step 7: Run provider, personal, metrics, and server tests**

```bash
cd backend
gofmt -w internal/relay/sub2api.go internal/relay/sub2api_test.go internal/telemetry/metrics.go internal/telemetry/metrics_test.go cmd/server/cache_metrics.go cmd/server/cache_metrics_test.go cmd/server/main.go
go test ./internal/readcache ./internal/relay ./internal/relayruntime ./internal/personalusage ./cmd/server -count=1
```

Expected: all selected packages pass and production metrics include `relay_user_trend`.

- [ ] **Step 8: Commit Task 5**

```bash
git add backend/internal/relay/sub2api.go backend/internal/relay/sub2api_test.go backend/internal/telemetry/metrics.go backend/internal/telemetry/metrics_test.go backend/cmd/server/cache_metrics.go backend/cmd/server/cache_metrics_test.go backend/cmd/server/main.go backend/internal/relayruntime/manager_test.go docs/superpowers/plans/2026-07-20-team-usage-redis-batch-trend.md
git commit -m "perf(backend): share personal Relay trends through Redis"
```

---

### Task 6: Update Current Architecture And Run Full Verification

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-07-20-team-usage-redis-batch-trend-design.md`
- Modify: `docs/superpowers/plans/2026-07-20-team-usage-redis-batch-trend.md`

**Interfaces:**
- Consumes the completed runtime from Tasks 1-5.
- Produces current project documentation and a locally verified implementation branch.

- [ ] **Step 1: Update the Team Usage architecture contract**

Replace the process-local 60-second/4096-entry paragraph. Document the
60-second per-provider/version/Relay-UID/range Redis primitive, authenticated
Personal Usage write-through, two-miss batch threshold, bounded users-trend
limit, truncation-aware fallback, cross-Pod Redis lease, and existing 24-slot
individual limiter. State that no completed trend result remains in Pod memory.
Classify embedded frontend representations and Relay clients as process-local
reuse; classify `FlightGroup`, limiters, and locks as coordination; record OAuth
authorization/device maps as a separate multi-replica concern. Link the July 20
spec without rewriting the July 19 historical specs.

- [ ] **Step 2: Run formatting and hygiene checks**

```bash
cd backend
gofmt -w $(rg --files internal/readcache internal/relay internal/telemetry cmd/server | rg '\.go$')
cd ..
git diff --check
rg -n 'teamTrendCache|teamTrendCacheCapacity|teamTrendCacheTTL' backend/internal/relay
rg -n 'email|username|password|token' backend/internal/relay/sub2api_team_trend_redis.go backend/internal/relay/sub2api_team_trend_batch.go
```

Expected: diff check is silent; old cache symbols have no production match; the
privacy scan finds no serialized or logged sensitive field.

- [ ] **Step 3: Run focused package tests**

```bash
cd backend
go test ./internal/readcache ./internal/relay ./internal/relayruntime ./internal/personalusage ./internal/teamusage ./cmd/server -count=1
```

Expected: all six targets report `ok`.

- [ ] **Step 4: Run the complete backend suite**

```bash
cd backend
go test ./... -count=1
```

Expected: every backend package passes.

- [ ] **Step 5: Run race detection on shared paths**

```bash
cd backend
go test -race ./internal/readcache ./internal/relay ./internal/personalusage ./internal/teamusage -count=1
```

Expected: all four packages pass with no race report.

- [ ] **Step 6: Mark the spec locally implemented**

Only after Steps 2-5 pass, set the spec status to `Implemented and locally
verified on perf/team-usage-batch-trend`. Add `Local Verification` with the
exact successful commands and date. Check completed plan items immediately;
leave every staging-only checkbox unchecked.

- [ ] **Step 7: Commit Task 6**

```bash
git add docs/architecture.md docs/superpowers/specs/2026-07-20-team-usage-redis-batch-trend-design.md docs/superpowers/plans/2026-07-20-team-usage-redis-batch-trend.md
git commit -m "docs(architecture): document Redis Relay trend sharing"
```

---

### Task 7: Deliver The Branch And Verify Staging Acceptance

**Files:**
- Modify only for evidence: `docs/superpowers/plans/2026-07-20-team-usage-redis-batch-trend.md`
- External deployment checkout: `/Users/admin/helm`

**Interfaces:**
- Consumes the locally verified commit from Task 6.
- Produces a reviewable PR and staging evidence; production remains unchanged.

- [ ] **Step 1: Review the final branch delta**

```bash
git status --short --branch
git diff --stat origin/feat/platform-loading-performance...HEAD
git log --oneline origin/feat/platform-loading-performance..HEAD
```

Expected: only this spec/plan, Redis bulk operations, Relay trend files,
production wiring, metrics, tests, and architecture are present; worktree clean.

- [ ] **Step 2: Push and open the integration PR**

Run:

```bash
git push -u origin perf/team-usage-batch-trend
gh pr create \
  --base feat/platform-loading-performance \
  --head perf/team-usage-batch-trend \
  --title "perf(backend): batch and share Relay user trends" \
  --body $'## Summary\n- replace the Pod-local trend result cache with 60-second per-Relay-UID Redis entries\n- batch two or more cold misses through Sub2API users-trend with truncation-aware fallback\n- let authenticated Personal Usage origins warm the same privacy-safe primitive\n\n## Verification\n- go test ./... -count=1\n- go test -race ./internal/readcache ./internal/relay ./internal/personalusage ./internal/teamusage -count=1\n\n## Pending\n- staging cold/warm acceptance after review and merge'
```

Do not include credentials or live user identifiers. Verify the PR base is the
integration branch before proceeding.

- [ ] **Step 3: Build and publish the exact staging image**

After the PR is reviewed and merged, verify its merge tree and fast-forward the
dedicated integration worktree before deriving the image tag:

```bash
git fetch --prune origin
git -C /Users/admin/ai-efficiency/.worktrees/feat-platform-loading-performance merge --ff-only origin/feat/platform-loading-performance
app_worktree=/Users/admin/ai-efficiency/.worktrees/feat-platform-loading-performance
commit=$(git -C "${app_worktree}" rev-parse HEAD)
image=ghcr.io/lichking-2234/ai-efficiency
tag="staging-${commit}"
builder=static-spaces-release-builder
platforms=$(docker buildx inspect "${builder}" --bootstrap | awk -F': ' '/^Platforms:/ {print $2}')
grep -q 'linux/amd64' <<<"${platforms}"
grep -q 'linux/arm64' <<<"${platforms}"
docker buildx build \
  --builder "${builder}" \
  --platform linux/amd64,linux/arm64 \
  --file deploy/Dockerfile \
  --tag "${image}:${tag}" \
  --build-arg APP_VERSION="staging-${commit:0:7}" \
  --build-arg APP_COMMIT="${commit}" \
  --build-arg APP_BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --push "${app_worktree}"
docker buildx imagetools inspect "${image}:${tag}"
```

Expected: manifest includes both `linux/amd64` and `linux/arm64`.

- [ ] **Step 4: Deploy only `ai-efficiency-staging` through the existing Helm path**

In `/Users/admin/helm`, update only `.image.tag` and
`.postgres.restore.snapshotId` in the tracked
`ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json`, without printing
secret fields:

```bash
cd /Users/admin/helm
commit=$(git -C /Users/admin/ai-efficiency/.worktrees/team-usage-batch-trend rev-parse HEAD)
tag="staging-${commit}"
tmp=$(mktemp ai-efficiency/.secrets/.ai-efficiency-staging-upgrade.XXXXXX)
jq --arg tag "${tag}" --arg snapshot "${commit:0:12}" \
  '.image.tag = $tag | .postgres.restore.snapshotId = $snapshot' \
  ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json >"${tmp}"
chmod 600 "${tmp}"
mv "${tmp}" ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json
jq '{release: ._appsuite_, namespace: ._namespace_, image: .image, redis_db: .env.AE_REDIS_DB, restore_snapshot: .postgres.restore.snapshotId}' \
  ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json
git diff --stat -- ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json
git add ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json
git commit -m "chore(ai-efficiency): publish staging ${commit:0:12}"
git push origin main
```

Run the current two-phase staging rollout in namespace
`la3-ai-efficiency-prod`. Phase A removes the old application Pod before the
staging database restore; Phase B restores and starts the exact image:

```bash
helm lint ./ai-efficiency \
  -f ./ai-efficiency/values.yaml \
  -f ./ai-efficiency/values-staging.yaml \
  -f ./ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json

helm upgrade --install ai-efficiency-staging ./ai-efficiency \
  -n la3-ai-efficiency-prod \
  -f ./ai-efficiency/values.yaml \
  -f ./ai-efficiency/values-staging.yaml \
  -f ./ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json \
  --set replicaCount=0 --set postgres.restore.enabled=false \
  --atomic --wait --timeout 20m --dry-run=server --hide-secret >/dev/null
helm upgrade --install ai-efficiency-staging ./ai-efficiency \
  -n la3-ai-efficiency-prod \
  -f ./ai-efficiency/values.yaml \
  -f ./ai-efficiency/values-staging.yaml \
  -f ./ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json \
  --set replicaCount=0 --set postgres.restore.enabled=false \
  --atomic --wait --timeout 20m
app_pods=$(kubectl get pod -n la3-ai-efficiency-prod \
  -l 'app.kubernetes.io/instance=ai-efficiency-staging,!app.kubernetes.io/component' \
  -o name)
if [[ -n "${app_pods}" ]]; then
  kubectl wait --for=delete pod -n la3-ai-efficiency-prod \
    -l 'app.kubernetes.io/instance=ai-efficiency-staging,!app.kubernetes.io/component' \
    --timeout=180s
fi

helm upgrade --install ai-efficiency-staging ./ai-efficiency \
  -n la3-ai-efficiency-prod \
  -f ./ai-efficiency/values.yaml \
  -f ./ai-efficiency/values-staging.yaml \
  -f ./ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json \
  --atomic --wait --wait-for-jobs --timeout 20m --dry-run=server --hide-secret >/dev/null
helm upgrade --install ai-efficiency-staging ./ai-efficiency \
  -n la3-ai-efficiency-prod \
  -f ./ai-efficiency/values.yaml \
  -f ./ai-efficiency/values-staging.yaml \
  -f ./ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json \
  --atomic --wait --wait-for-jobs --timeout 20m

helm status ai-efficiency-staging -n la3-ai-efficiency-prod --show-resources
kubectl rollout status statefulset/ai-efficiency-staging-postgres -n la3-ai-efficiency-prod --timeout=180s
kubectl rollout status deployment/ai-efficiency-staging -n la3-ai-efficiency-prod --timeout=180s
curl -fsS --max-time 20 https://ai-efficiency-staging.la3.agoralab.co/api/v1/health/ready
curl -fsS --max-time 20 https://ai-efficiency.la3.agoralab.co/api/v1/health/ready
```

The Helm commit must contain only the metadata selectors. Verify production
image/revision remain unchanged.

- [ ] **Step 5: Clear only relevant staging Redis keys**

Create one temporary Redis CLI Pod in the staging release namespace. Derive the
non-secret address from the Deployment and read the password only through the
existing Kubernetes Secret. Delete only the five exact cache prefixes in Redis
DB 2 and print counts, never key values:

```bash
namespace=la3-ai-efficiency-prod
redis_addr=$(kubectl get deployment ai-efficiency-staging -n "${namespace}" \
  -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="AE_REDIS_ADDR")].value}')
kubectl delete pod ae-staging-cache-clean -n "${namespace}" --ignore-not-found --wait=true
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: ae-staging-cache-clean
  namespace: ${namespace}
spec:
  restartPolicy: Never
  containers:
    - name: redis-cli
      image: redis:7-alpine
      env:
        - name: REDIS_ADDR
          value: "${redis_addr}"
        - name: REDISCLI_AUTH
          valueFrom:
            secretKeyRef:
              name: ai-efficiency-staging-env
              key: AE_REDIS_PASSWORD
      resources:
        requests:
          cpu: 20m
          memory: 32Mi
          ephemeral-storage: 32Mi
        limits:
          cpu: 20m
          memory: 32Mi
          ephemeral-storage: 32Mi
      command: ["/bin/sh", "-ec"]
      args:
        - |
          host="\${REDIS_ADDR%:*}"
          port="\${REDIS_ADDR##*:}"
          for pattern in \
            'ai-efficiency:relay-user-trend:v1:*' \
            'ae:ai-efficiency:team-usage-summary:v1:*' \
            'ae:ai-efficiency:team-usage-trend:v1:*' \
            'ae:ai-efficiency:team-usage-members:v1:*' \
            'ae:ai-efficiency:team-usage-organization:v1:*'; do
            deleted=0
            redis-cli -h "\${host}" -p "\${port}" -n 2 --scan --pattern "\${pattern}" |
            while IFS= read -r key; do
              redis-cli -h "\${host}" -p "\${port}" -n 2 DEL "\${key}" >/dev/null
              deleted=\$((deleted + 1))
              printf '%s %s\n' "\${pattern}" "\${deleted}" >/tmp/deleted-count
            done
            if [[ -f /tmp/deleted-count ]]; then
              cat /tmp/deleted-count
              rm -f /tmp/deleted-count
            else
              printf '%s 0\n' "\${pattern}"
            fi
          done
EOF
kubectl wait --for=condition=Ready pod/ae-staging-cache-clean -n "${namespace}" --timeout=120s || true
kubectl wait --for=jsonpath='{.status.phase}'=Succeeded pod/ae-staging-cache-clean -n "${namespace}" --timeout=300s
kubectl logs pod/ae-staging-cache-clean -n "${namespace}"
kubectl delete pod ae-staging-cache-clean -n "${namespace}" --wait=true
```

Do not run `FLUSHDB`, delete auth/session/OAuth keys, use another Redis DB, or
touch the production release.

- [ ] **Step 6: Run four-lane cold and warm audits**

Start Summary, Trend, Members, and Organization concurrently for the same
authorized 251-member/235-Relay-member scope and normalized range. Capture wall
time, status, cache metadata, counts, Relay dependency totals, Redis/lease
metrics, and Sub2API status classes.

Acceptance:

```text
cold complete readiness <= 9.0s
each warm endpoint <= 1.5s
untruncated users-trend requests <= 1
individual user-trend GETs = 0
total Relay requests <= 30
member count = 251
Relay-linked member count = 235
Relay 429/5xx/transport/timeout = 0
```

Compare selected-user dates, actual costs, token totals, and aggregates against
the individual-origin evidence. Keep credentials, user records, and tokens out
of the repository.

- [ ] **Step 7: Record evidence and stop at the gate**

If all thresholds pass, append image tag/digest, Helm revision, cold/warm
timings, batch/individual/total Relay calls, member counts, and error counters
to this plan and mark Task 7 complete. If cold readiness exceeds nine seconds,
record per-lane/batch timing and correctness, leave Task 7 incomplete, and stop.
Do not add a global snapshot, longer TTL, or another cache layer without a new
approved spec.

Commit and push only the evidence/checklist update:

```bash
cd /Users/admin/ai-efficiency/.worktrees/feat-platform-loading-performance
git add docs/superpowers/plans/2026-07-20-team-usage-redis-batch-trend.md
git commit -m "docs(plans): record Team Usage staging audit"
git push origin feat/platform-loading-performance
```
