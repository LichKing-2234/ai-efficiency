# Team Usage Startup Cohort Publication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make one deployment-wide startup owner fetch four timezone lanes with the existing Relay concurrency of two and publish their manifests as a sub-five-second cohort before any scheduled tick.

**Architecture:** Keep the existing startup coordinator, Redis schema, immutable values, leases, and request fallback. Replace the serial timezone/class loop with a package-private two-worker startup batch; drain every task before a publication barrier, then publish each complete lane consecutively. Add one no-label scheduler-tick counter so staging proves the cohort completed before scheduled work instead of inferring a timestamp.

**Tech Stack:** Go 1.24, go-redis v9, miniredis, Prometheus client_golang, zap, Docker Buildx, GHCR, Helm, Kubernetes.

## Global Constraints

- The approved design is `docs/superpowers/specs/2026-07-23-team-usage-startup-cohort-publication-design.md`; the 2026-07-21 segmented spec remains authoritative outside startup fetch ordering, cohort publication, and scheduler evidence.
- Do not modify Sub2API, frontend behavior, request DTOs, authorization, PR #192 fallback, Redis schema v2, or `docs/architecture.md` before staging acceptance.
- Keep the exact read, write, lease, and release defaults at `250ms`.
- Keep the startup/historical coordinator TTL at six minutes, segment/source-slot TTL at 90 seconds, and deployment-wide source concurrency at two.
- Support at most four normalized timezones and at most 12 startup segment tasks.
- Use exactly two startup workers, while `SourceCallLimiter` remains the authoritative deployment-wide concurrency boundary.
- Do not add a completed Pod cache. In-flight leased references exist only inside one `runStartup` call and must be released before it returns.
- Do not retain raw Redis keys or values, Relay payloads, response bodies, credentials, user IDs, emails, names, or user lists in tests, metrics, logs, plans, or reports.
- Preserve timezone failure isolation: one failed lane cannot erase another complete lane, but any startup failure releases the startup coordinator so retry remains possible.
- Coordinator loss, provider-version change, cancellation, or worker deadline forbids all later manifest publication.
- Task 9 Steps 3-6 remain unchecked. A Task 15 replay pass only permits a fresh Task 9 Step 3 attempt.
- Production and Sub2API remain unchanged. Every staging replay ends disabled at one replica on the exact tested image.

---

### Task 1: Add Direct Scheduler-Tick Evidence

**Files:**
- Modify: `backend/internal/teamusage/prewarm_metrics.go`
- Modify: `backend/internal/teamusage/prewarmer.go`
- Modify: `backend/internal/teamusage/prewarmer_test.go`
- Modify: `backend/internal/teamusage/prewarm_reader_test.go`
- Modify: `backend/internal/telemetry/team_usage_prewarm.go`
- Modify: `backend/internal/telemetry/team_usage_prewarm_test.go`
- Modify: any existing `PrewarmMetrics` fake required by compilation.

**Interfaces:**
- Consumes: existing `PrewarmMetrics`, `Prewarmer.runTicks`, and the fixed 60-second runtime ticker.
- Produces: `PrewarmMetrics.RecordSchedulerTick()` and Prometheus metric `ai_efficiency_team_usage_prewarm_scheduler_tick_total` with no labels.

- [x] **Step 1: Write scheduler metric RED tests**

Add `TestPrewarmerRecordsSchedulerTickBeforeWorkers` using the test-only tick channel. The recorder hook must observe one scheduler tick before any moving, recovery, or historical worker records a cycle:

```go
func TestPrewarmerRecordsSchedulerTickBeforeWorkers(t *testing.T) {
    ticks := make(chan time.Time, 1)
    tickRecorded := make(chan struct{}, 1)
    metrics := &recordingPrewarmRequestMetrics{schedulerTickRecorded: tickRecorded}
    metrics.schedulerTickHook = func() {
        if len(metrics.cyclesCopy()) != 0 {
            t.Error("scheduled worker recorded before scheduler tick")
        }
    }
    options := lifecycleOptions([]string{"UTC"}, time.Now)
    options.tick, options.Metrics = ticks, metrics
    prewarmer := mustPrewarmer(
        t,
        staticBindingResolver{binding: prewarmBinding(newLifecycleProvider([]int64{101}))},
        mustNewPrewarmCache(t, newRecordingPrewarmStore(), time.Now),
        options,
    )

    prewarmer.Start(context.Background())
    select {
    case <-prewarmer.startupDone:
    case <-time.After(2 * time.Second):
        t.Fatal("startup did not finish")
    }
    ticks <- time.Now()
    select {
    case <-tickRecorded:
    case <-time.After(2 * time.Second):
        t.Fatal("scheduler tick was not recorded")
    }
    prewarmer.Stop()
}
```

Extend `recordingPrewarmRequestMetrics` with `schedulerTicks int`,
`schedulerTickHook func()`, and `schedulerTickRecorded chan struct{}`.
`RecordSchedulerTick` increments under its mutex, copies the hook/channel,
unlocks, then invokes/sends so test callbacks cannot deadlock the recorder.
Add `cyclesCopy()` as a locked slice copy used only by this test.

Add telemetry tests that gather the new family, require value zero immediately after recorder construction, call `RecordSchedulerTick`, require value one, and assert the metric has no labels.

- [x] **Step 2: Run RED and verify missing behavior**

Run:

```bash
cd backend
go test ./internal/teamusage ./internal/telemetry \
  -run 'SchedulerTick|PrewarmMetricsPreinitialize' -count=1
```

Expected: compilation or assertions fail only because `RecordSchedulerTick` and the metric family do not exist. Fix fixture or syntax errors until the failure is behavioral.

- [x] **Step 3: Implement the no-label counter**

Extend the interface and no-op recorder:

```go
type PrewarmMetrics interface {
    RecordCycle(class, timezone, outcome string, duration time.Duration)
    RecordSource(class, timezone, outcome string, duration time.Duration, bytes, points, users int)
    RecordRedis(operation, outcome string, duration time.Duration, bytes int)
    RecordRedisError(operation string, class PrewarmRedisErrorClass)
    RecordSchedulerTick()
    RecordRequest(timezone, outcome, fallbackReason string)
    SetLastSuccess(class, timezone string, at time.Time)
    RecordQuantity(kind PrewarmQuantityKind, timezone string, value int)
    SetGenerationBytes(value int)
    RecordValidation(check PrewarmValidationCheck, outcome PrewarmValidationOutcome)
    RecordCache(cache PrewarmCacheKind, outcome PrewarmCacheOutcome)
}

func (noopPrewarmMetrics) RecordSchedulerTick() {}
```

Add one `prometheus.Counter` named `team_usage_prewarm_scheduler_tick_total`, register it with the existing recorder collectors, and increment it in `RecordSchedulerTick`.

In `runTicks`, record synchronously before any worker starts:

```go
case <-ticks:
    p.options.Metrics.RecordSchedulerTick()
    p.startMoving(ctx)
    p.startRecovery(ctx)
    for _, timezone := range p.timezones {
        p.startHistorical(ctx, timezone, p.options.Now())
    }
```

Update every test fake by embedding `noopPrewarmMetrics` or implementing the new method. Do not add labels or a structured log field.

- [x] **Step 4: Run GREEN, race, and package verification**

Run:

```bash
cd backend
gofmt -w internal/teamusage internal/telemetry
go test ./internal/teamusage ./internal/telemetry \
  -run 'SchedulerTick|PrewarmMetricsPreinitialize' -count=1
go test -race ./internal/teamusage ./internal/telemetry \
  -run 'SchedulerTick|PrewarmMetricsPreinitialize' -count=1
go test ./internal/teamusage ./internal/telemetry -count=1
```

Expected: all commands exit zero with no test failure. Record any existing linker-only warning separately.

- [x] **Step 5: Commit and task-review**

```bash
git add backend/internal/teamusage backend/internal/telemetry
git commit -m "feat(teamusage): expose prewarm scheduler ticks"
```

Generate a task-scoped review package from the recorded pre-task base through this commit. Resolve every Critical and Important finding and repeat the covering focused/race commands before marking Task 1 complete.

---

### Task 2: Fetch Startup Segments With Two Workers And Publish After A Barrier

**Files:**
- Create: `backend/internal/teamusage/prewarmer_startup.go`
- Create: `backend/internal/teamusage/prewarmer_startup_test.go`
- Modify: `backend/internal/teamusage/prewarmer.go`
- Modify: `backend/internal/teamusage/prewarmer_test.go` only for shared test fixtures that cannot live in the new file.

**Interfaces:**
- Consumes: `Prewarmer.fetchLeasedSegment`, `Prewarmer.publishIfCurrent`, `Prewarmer.releaseLeasedReference`, `newPrewarmManifestCandidate`, `startupNeedsRecovery`, `startupMissingSegmentClasses`, and the Task 14 startup coordinator behavior.
- Produces: package-private `startupLanePlan`, `startupSegmentTask`, `startupSegmentResult`, `planStartupLanes`, `fetchStartupSegments`, and `publishStartupCohort` used only by `runStartup`.

- [x] **Step 1: Write the two-worker and publication-barrier RED test**

Create a provider fixture that blocks every trend fetch, records active and maximum calls, and exposes entered/release channels. Start `runStartup` with four empty lanes and assert:

```go
func TestPrewarmStartupFetchesAtTwoAndPublishesAfterBarrier(t *testing.T) {
    prewarmer, provider, cache := newBlockedFourLaneStartup(t)
    result := make(chan error, 1)
    go func() { result <- prewarmer.runStartup(context.Background()) }()

    provider.waitForActive(t, 2)
    if got := provider.maxActive(); got != 2 {
        t.Fatalf("startup source concurrency = %d, want 2", got)
    }
    assertNoStartupManifest(t, cache)

    provider.releaseAll()
    if err := <-result; err != nil {
        t.Fatalf("runStartup() error = %v", err)
    }
    assertFourCompleteStartupManifests(t, cache)
    assertOneSharedCurrentReference(t, cache)
}
```

The fixture must count actual provider calls, not goroutine creation. Before the fix, the test must fail because maximum source concurrency is one or a manifest appears before the barrier.

- [x] **Step 2: Write RED tests for failure isolation and cleanup**

Add these exact tests:

```go
func TestPrewarmStartupPublishesHealthyLanesAfterOneLaneFailure(t *testing.T)
func TestPrewarmStartupCoordinatorLossPreventsBarrierPublication(t *testing.T)
func TestPrewarmStartupCancellationDrainsTasksAndReleasesLeases(t *testing.T)
func TestPrewarmStartupBatchRetainsNoCompletedResult(t *testing.T)
```

Required assertions:

- one synthetic lane source failure returns a joined error, publishes the other three complete lanes only, and leaves no six-minute startup coordinator marker;
- dropping the coordinator before barrier publication leaves no newly published manifest;
- cancellation drains all task results and releases every acquired segment lease;
- after `runStartup` returns, no startup worker remains and no `Prewarmer` field references a decoded current or segment result.

Run:

```bash
cd backend
go test ./internal/teamusage -run 'PrewarmStartup.*(Barrier|LaneFailure|CoordinatorLoss|Cancellation|Retains)' -count=1
```

Expected: behavioral failures from serial fetch/publication and missing batch helpers, never fixture compilation errors.

- [x] **Step 3: Add the package-private startup batch model**

Create these types in `prewarmer_startup.go`:

```go
type startupLanePlan struct {
    identity PrewarmCacheIdentity
    manifest PrewarmManifest
    missing  []PrewarmSegmentClass
    leased   map[PrewarmSegmentClass]leasedPrewarmReference
    failures []error
}

type startupSegmentTask struct {
    laneIndex int
    class     PrewarmSegmentClass
}

type startupSegmentResult struct {
    task   startupSegmentTask
    leased leasedPrewarmReference
    err    error
}

const startupSegmentWorkerCount = prewarmSourceSlotCount

func startupRefreshClass(class PrewarmSegmentClass) string {
    if class == SegmentTodayHour {
        return prewarmMovingRefreshClass
    }
    return string(class)
}

func startupTaskCount(plans []startupLanePlan) int {
    count := 0
    for _, plan := range plans {
        count += len(plan.missing)
    }
    return count
}
```

Also add these package-private helpers with the exact signatures used by the
later steps:

```go
func sharedCurrentNeeded(plans []startupLanePlan) bool
func applyStartupCurrentReference(plans []startupLanePlan, ref PrewarmValueReference)
func applyStartupSegmentReferences(manifest *PrewarmManifest, leased map[PrewarmSegmentClass]leasedPrewarmReference)
func startupSegmentClaims(leased map[PrewarmSegmentClass]leasedPrewarmReference) []PrewarmLeaseClaim
```

`startupSegmentClaims` returns claims in `history_29d`, `history_6d`,
`today_hour` order. None of these helpers stores state outside the supplied
batch.

`planStartupLanes` captures one batch time, validates split safety, reads each prior generation, preserves hard-valid references, and returns lane-scoped failures rather than discarding healthy lanes:

```go
func (p *Prewarmer) planStartupLanes(
    ctx context.Context,
    binding ProviderBinding,
    batchTime time.Time,
) ([]startupLanePlan, []error)
```

If any plan needs current stats, `runStartup` calls `buildCurrentStats` and `WriteCurrentStats` exactly once, then assigns the returned reference only to plans that need it. Shared current failure returns before any segment worker starts.

- [x] **Step 4: Implement the fixed two-worker fetch and complete drain**

Use a task channel, a result channel sized to the exact task count, two workers, and one collector goroutine. The collector is the only writer to lane plans:

```go
func (p *Prewarmer) fetchStartupSegments(
    ctx context.Context,
    binding ProviderBinding,
    plans []startupLanePlan,
) []error {
    tasks := make(chan startupSegmentTask)
    results := make(chan startupSegmentResult, startupTaskCount(plans))
    var workers sync.WaitGroup
    for worker := 0; worker < startupSegmentWorkerCount; worker++ {
        workers.Add(1)
        go func() {
            defer workers.Done()
            for task := range tasks {
                plan := plans[task.laneIndex]
                leased, err := p.fetchLeasedSegment(
                    ctx, binding, plan.identity.Timezone,
                    plan.identity.AnchorDate, task.class,
                    startupRefreshClass(task.class),
                )
                results <- startupSegmentResult{task: task, leased: leased, err: err}
            }
        }()
    }
    go func() {
        defer close(tasks)
        for laneIndex := range plans {
            for _, class := range plans[laneIndex].missing {
                select {
                case tasks <- startupSegmentTask{laneIndex: laneIndex, class: class}:
                case <-ctx.Done():
                    return
                }
            }
        }
    }()
    go func() {
        workers.Wait()
        close(results)
    }()

    var failures []error
    for result := range results {
        if result.err != nil {
            failure := newPrewarmLifecycleFailure(
                PrewarmCycleStartup,
                plans[result.task.laneIndex].identity.Timezone,
                false,
                result.err,
            )
            plans[result.task.laneIndex].failures = append(
                plans[result.task.laneIndex].failures, failure,
            )
            failures = append(failures, failure)
            continue
        }
        plans[result.task.laneIndex].leased[result.task.class] = result.leased
    }
    return failures
}
```

The real implementation must avoid producer leakage when `ctx` is canceled: task submission selects on `ctx.Done`, every started worker exits, and every result channel is drained. `errPrewarmLeaseBusy` is a lane failure during owner startup; it is not silently converted to success.

- [x] **Step 5: Implement barrier publication and unconditional lease cleanup**

After all fetch results settle, defer release for every successful leased reference. Apply results to candidate manifests, then publish only complete lanes:

```go
func (p *Prewarmer) publishStartupCohort(
    ctx context.Context,
    binding ProviderBinding,
    plans []startupLanePlan,
) []error {
    var failures []error
    for index := range plans {
        plan := &plans[index]
        applyStartupSegmentReferences(&plan.manifest, plan.leased)
        if len(plan.failures) != 0 || !prewarmManifestReferencesPresent(plan.manifest) {
            continue
        }
        claims := startupSegmentClaims(plan.leased)
        if err := p.publishIfCurrent(ctx, binding, claims, plan.manifest); err != nil {
            failures = append(failures, newPrewarmLifecycleFailure(
                PrewarmCycleStartup, plan.identity.Timezone, false, err,
            ))
            continue
        }
        p.options.Metrics.SetLastSuccess("startup", plan.identity.Timezone, p.options.Now())
    }
    return failures
}
```

Sort claims by segment class for deterministic tests. `publishIfCurrent` already adds the startup coordinator claim from the worker context, so at most three extra segment claims are passed. Recheck `ctx.Err()` and coordinator ownership immediately before the barrier and before each publication.

On every return path, call `releaseLeasedReference` exactly once for each successful task. Do not store plans or leased references on `Prewarmer`.

- [x] **Step 6: Replace the serial startup loop and run GREEN**

Keep `runStartup` ownership and retain-on-success semantics, but replace its timezone/class source loop with:

```go
batchTime := p.options.Now()
plans, failures := p.planStartupLanes(workerCtx, binding, batchTime)
if sharedCurrentNeeded(plans) {
    current, err := p.buildCurrentStats(workerCtx, binding, "startup")
    if err != nil {
        return errors.Join(append(failures, err)...)
    }
    ref, err := p.cache.WriteCurrentStats(workerCtx, current)
    if err != nil {
        return errors.Join(append(failures, err)...)
    }
    applyStartupCurrentReference(plans, ref)
}
failures = append(failures, p.fetchStartupSegments(workerCtx, binding, plans)...)
if err := workerCtx.Err(); err != nil {
    return errors.Join(append(failures, err)...)
}
if err := p.requireCoordinatorOwned(workerCtx); err != nil {
    return errors.Join(append(failures, err)...)
}
failures = append(failures, p.publishStartupCohort(workerCtx, binding, plans)...)
if err := errors.Join(failures...); err != nil {
    return err
}
retainCoordinator = true
return nil
```

Do not call `beginPublicationBatch` from startup or create a dummy current
value. Preserve the existing startup generation-metrics behavior; moving and
recovery remain the owners of full participating-lane batch accounting.

Run:

```bash
cd backend
gofmt -w internal/teamusage
go test ./internal/teamusage -run 'PrewarmStartup' -count=1
go test ./internal/teamusage -run 'PrewarmStartup.*(Barrier|LaneFailure|CoordinatorLoss|Cancellation|Retains)' -count=50
go test -race ./internal/teamusage -run 'PrewarmStartup' -count=1
go test ./internal/teamusage -count=1
```

Expected: all commands pass, observed source maximum is exactly two in the empty four-lane fixture, and no test weakens existing lost-lease or timezone-isolation assertions.

- [x] **Step 7: Commit and task-review**

```bash
git add backend/internal/teamusage/prewarmer.go \
  backend/internal/teamusage/prewarmer_startup.go \
  backend/internal/teamusage/prewarmer_startup_test.go \
  backend/internal/teamusage/prewarmer_test.go
git commit -m "perf(teamusage): publish startup lanes as one cohort"
```

Generate a task-scoped review package from the recorded pre-task base. Resolve every Critical and Important finding, rerun all covering focused/repeated/race commands, and re-review before marking Task 2 complete.

---

### Task 3: Run Full Verification And Record The Reviewed Head

**Files:**
- Modify: `docs/superpowers/specs/2026-07-23-team-usage-startup-cohort-publication-design.md`
- Modify: `docs/superpowers/plans/2026-07-23-team-usage-startup-cohort-publication.md`
- Modify: `docs/superpowers/plans/2026-07-21-team-usage-segmented-timezone-prewarm.md` only to link the superseding startup contract and retained Task 14 evidence.

**Interfaces:**
- Consumes: reviewed Task 1 and Task 2 commits.
- Produces: one exact reviewed application head eligible for immutable image publication.

- [ ] **Step 1: Run the full backend verification ladder**

```bash
cd backend
go test ./internal/teamusage ./internal/telemetry ./cmd/server -count=1
go test -race ./internal/teamusage ./internal/telemetry ./cmd/server -count=1
go test ./... -count=1
go vet ./...
go build ./...
cd ..
git diff --check
```

Expected: every command exits zero. Record existing toolchain-only warnings separately; any test, race, vet, build, or diff failure blocks Task 3.

- [ ] **Step 2: Run a broad startup-branch review**

Generate a review package from Task 15's design commit through the exact code head. Review Standards and Spec, focusing on worker/channel cancellation, maximum source concurrency, publication ordering, lease release exactly once, coordinator retention, lane isolation, metric privacy, and no completed Pod state. Resolve every Critical and Important finding with covering tests and re-review.

- [ ] **Step 3: Update current documentation status**

Record only commit SHAs, exact commands, pass/fail counts, and review findings. Mark the new design as implemented and reviewed but staging-disabled. Do not update `docs/architecture.md` or claim current runtime.

- [ ] **Step 4: Commit the local review ledger**

```bash
git add docs/superpowers/specs/2026-07-23-team-usage-startup-cohort-publication-design.md \
  docs/superpowers/plans/2026-07-23-team-usage-startup-cohort-publication.md \
  docs/superpowers/plans/2026-07-21-team-usage-segmented-timezone-prewarm.md
git commit -m "docs(teamusage): complete startup cohort review"
```

Verify the tracked worktree is clean and record this commit as the only image-eligible Task 15 head.

---

### Task 4: Publish Exact Code And Repeat The Disabled Redis Benchmark

**Files:**
- Modify in `/Users/admin/helm`: `ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json`
- Modify with sanitized evidence: current Task 15 plan and design spec.

**Interfaces:**
- Consumes: exact reviewed Task 3 head and the Task 13 package-native benchmark harness by its recorded SHA.
- Produces: a multi-architecture staging image and a passing feature-disabled Redis command gate.

- [ ] **Step 1: Verify immutable baselines**

Require the application worktree clean. Record staging revision/image/flag/health and production revision/image/flag/health. Verify the staging selector is mode `0600`. Preserve and do not inspect unrelated Helm changes.

- [ ] **Step 2: Build and verify the exact image**

```bash
APP_WORKTREE=/Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm
COMMIT="$(git -C "${APP_WORKTREE}" rev-parse HEAD)"
IMAGE="ghcr.io/lichking-2234/ai-efficiency:staging-${COMMIT}"
docker buildx build --builder static-spaces-release-builder \
  --platform linux/amd64,linux/arm64 \
  --file "${APP_WORKTREE}/deploy/Dockerfile" \
  --tag "${IMAGE}" \
  --build-arg APP_VERSION="staging-${COMMIT:0:7}" \
  --build-arg APP_COMMIT="${COMMIT}" \
  --build-arg APP_BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --push "${APP_WORKTREE}"
docker buildx imagetools inspect "${IMAGE}"
```

Record index, amd64, and arm64 digests. A missing architecture blocks deployment.

- [ ] **Step 3: Deploy disabled through paused and restore-enabled phases**

Use the tracked staging refresh script and staging playbook. Server dry-run and rendered Deployment must show the exact image, one replica, and all Team Usage prewarm environment entries absent. Verify deployed, `1/1` ready, zero restarts, and HTTP 200 live/readiness.

- [ ] **Step 4: Repeat the exact seven-class benchmark**

Run smoke, then 100 valid samples for current write, segment write, five-lease manifest publication, full-generation MGET, four-lane request MGET, lease acquire, and lease release. Require zero returned command, transport, INFO, and cleanup errors; zero remaining synthetic keys; and zero eviction/rejected-connection deltas. Keep all four budgets at `250ms`.

Delete local source, binary, raw reports, and Pod binary; verify zero task-specific temporary artifacts. Any gate failure leaves staging disabled, keeps Task 4 unchecked, records sanitized evidence, and stops.

- [ ] **Step 5: Record the passing disabled gate**

Check Task 4 only after the exact-code full run passes. Commit sanitized plan/spec evidence with:

```bash
git commit -m "docs(teamusage): record startup cohort Redis benchmark"
```

Do not commit or push the staging selector yet; Step 5 must restore it to a deliberate final disabled state first.

---

### Task 5: Run The Two-Pod Cohort Replay And Restore Disabled

**Files:**
- Modify in `/Users/admin/helm`: `ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json`
- Modify with sanitized evidence: current Task 15 plan and design spec.
- Modify `docs/architecture.md` only after a later full Task 9 acceptance, not in this task.

**Interfaces:**
- Consumes: exact Task 4 image, no-label scheduler-tick metric, and four approved timezones.
- Produces: a pass/fail decision that either permits a fresh Task 9 Step 3 or records the next blocker.

- [ ] **Step 1: Prepare bounded fresh-Pod observation**

Create only session-temporary watcher files. For each new Pod capture counter-zero baselines as soon as the metrics listener is available. Retain only durations, closed counts/outcomes, digests, and revisions. Do not use an account, call Team Usage API lanes, print response bodies, or read raw Redis values.

- [ ] **Step 2: Enable exact code on two staging replicas**

Render and server-dry-run the exact image, two replicas, `AE_TEAM_USAGE_PREWARM_ENABLED=true`, and the approved four timezones. Apply atomically only to staging. Verify `2/2` ready, exact image, zero restarts, and HTTP 200 health.

- [ ] **Step 3: Enforce every diagnostic gate**

At the first observation where four complete schema-v2 manifests and all references exist, require:

```text
owner startup success labels = 4
loser startup lease_busy labels = 4
loser startup source observations = 0
scheduler_tick_total on owner = 0
scheduler_tick_total on loser = 0
first-to-last manifest spread <= 5s
maximum deployment-wide source concurrency <= 2
Relay errors = 0
Redis operation errors = 0
Redis error-class deltas = 0
```

Record bounded pool pending values and wait/timeout deltas without claiming interval pressure is impossible. Any missing proof or nonzero error is failure.

- [ ] **Step 4: Restore disabled regardless of result**

Immediately apply the exact-image disabled selector with one replica. Verify a new Helm revision is deployed, `1/1` ready, all prewarm environment entries absent, and HTTP 200 live/readiness. Reverify production unchanged. Restore the tracked selector to its deliberate exact-image disabled bytes, keep mode `0600`, and preserve unrelated Helm changes.

- [ ] **Step 5: Record, clean, review, and commit**

Delete watcher source, binaries, metrics, and raw reports; verify zero task-specific `/tmp` and Pod artifacts. On pass, check Task 5 and record that Task 9 Step 3 may restart; on failure leave it unchecked and record the exact closed blocker. In both cases keep Task 9 Steps 3-6 unchecked and `docs/architecture.md` unchanged.

Commit with accurate pass/fail wording:

```bash
git commit -m "docs(teamusage): record startup cohort staging replay"
```

Generate a task-scoped ledger review. Resolve every Critical and Important evidence finding before reporting the final state.
