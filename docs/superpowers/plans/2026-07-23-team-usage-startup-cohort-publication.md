# Team Usage Startup Cohort Publication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make one deployment-wide startup owner fetch four timezone lanes with the existing Relay concurrency of two and publish their manifests as a sub-five-second cohort before any scheduled tick.

**Architecture:** Keep the existing startup coordinator, Redis schema, immutable values, leases, and request fallback. Replace the serial timezone/class loop with a package-private two-worker startup batch; drain every task before a publication barrier, then publish each complete lane consecutively. Add one no-label scheduler-tick counter so staging proves the cohort completed before scheduled work instead of inferring a timestamp.

**Tech Stack:** Go 1.24, go-redis v9, miniredis, Prometheus client_golang, zap, Docker Buildx, GHCR, Helm, Kubernetes.

**Status:** Tasks 1-4 are complete. Task 4 published exact reviewed head
`c5d3f6af15ea4db7ef424139822b043ee787f79d`, deployed it disabled to staging,
and passed the seven-class Redis benchmark with 100 valid samples per class at
the unchanged `250ms` budgets. The original Task 5 replay retained no observer
result. Its separately authorized guarded replacement replay ended in an
operational observation failure, so none of the product diagnostic gates is
proven. A later, separately user-authorized Task 9 HTTP acceptance preflight
failed before login or API requests on three Redis `command_deadline` errors
and four cycle errors. One still later, separately authorized latency-only
observation continued past the preflight solely to measure the current image.
All four cold lanes returned HTTP 200 and `full_hit`, but three exceeded five
seconds; all immediate warm lanes met `1.5s`. Background ticks made the
aggregate Relay delta non-attributable. The guardian restored exact-image
disabled staging revision 72. A separate full-page cold observation then
measured `/usage/team` data-rendered completion at `8.786s`, also failing the
five-second goal. A cache-verified Google Chrome 150 repeat measured `7.796s`,
also failing the goal, before restoring disabled staging revision 76.
Production revision 69 remained unchanged. Task 5 failed, Task 9 Steps 3-6
remain unchecked, and neither result authorizes implementation work.

**Closure Status (2026-07-24):** The user accepted the cache-verified Google
Chrome 30-day page result of `7.796s` and closed further performance work. The
superseding closeout contract is
`docs/superpowers/specs/2026-07-24-team-usage-prewarm-closeout-design.md`.
Tasks 1-4 are accepted for merge. Task 5 and Task 9 Steps 3-6 remain visibly
incomplete and are closed/deferred to issue #194; they must not be resumed in
PR #193. The feature remains disabled by default, no startup completion marker
will be added here, and `docs/architecture.md` remains unchanged.

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
- Task 9 Steps 3-6 remain unchecked as historical evidence and are closed/deferred to issue #194 by the 2026-07-24 closeout contract.
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
    batchCtx context.Context,
    cancelBatch context.CancelCauseFunc,
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
            hasFetchedTask := false
            for task := range tasks {
                if cause := context.Cause(batchCtx); cause != nil {
                    results <- startupSegmentResult{task: task, err: cause}
                    continue
                }
                if hasFetchedTask {
                    if ownershipErr := p.requireCoordinatorOwned(batchCtx); ownershipErr != nil {
                        cancelBatch(ownershipErr)
                        results <- startupSegmentResult{task: task, err: ownershipErr}
                        continue
                    }
                }
                identity := plans[task.laneIndex].identity
                leased, err := p.fetchLeasedSegment(
                    batchCtx, binding, identity.Timezone, identity.AnchorDate,
                    task.class, startupRefreshClass(task.class),
                )
                hasFetchedTask = true
                if err != nil {
                    if coordinatorErr := p.requireCoordinatorOwned(batchCtx); coordinatorErr != nil {
                        cancelBatch(coordinatorErr)
                    }
                }
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
                case <-batchCtx.Done():
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

The final implementation receives the batch context and its explicit cancel
owner from `runStartup`. It avoids producer leakage when `batchCtx` is
canceled: task submission selects on `batchCtx.Done`, every started worker
exits, and every result channel is drained. Between-task ownership loss cancels
the batch before another segment lease acquisition. `errPrewarmLeaseBusy` is a
lane failure during owner startup; it is not silently converted to success.

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
        publicationCtx, finishPublication := startupPublicationContext(ctx, plan.leased)
        publicationErr := context.Cause(publicationCtx)
        if publicationErr == nil {
            publicationErr = p.requireCoordinatorOwned(publicationCtx)
        }
        if publicationErr == nil {
            publicationErr = p.publishIfCurrent(
                publicationCtx, binding, startupSegmentClaims(plan.leased), plan.manifest,
            )
        }
        finishPublication()
        if publicationErr != nil {
            failures = append(failures, newPrewarmLifecycleFailure(
                PrewarmCycleStartup, plan.identity.Timezone, false, publicationErr,
            ))
            continue
        }
        p.options.Metrics.SetLastSuccess("startup", plan.identity.Timezone, p.options.Now())
    }
    return failures
}
```

`startupSegmentClaims` emits claims in fixed segment-class order for
deterministic tests. `publishIfCurrent` already adds the startup coordinator
claim from the worker context, so at most three extra segment claims are
passed. `runStartup` checks the batch context cause and coordinator ownership
once immediately before entering the publication barrier. That batch check
does not replace the per-lane checks: each complete lane derives a
`publicationCtx` from its original leased segment contexts, reads that
context's cause, requires coordinator ownership on that context, and publishes
on that context only if both checks pass. Call `finishPublication()` before
evaluating every publication result so all segment-context callbacks are
detached on both failure and success.

On every return path, call `releaseLeasedReference` exactly once for each successful task. Do not store plans or leased references on `Prewarmer`.

Keep the original `leased.ctx` values returned by `fetchLeasedSegment` alive
through barrier publication. Do not reparent or replace them; publication must
still reject a segment context that expires while waiting behind the barrier.

- [x] **Step 6: Replace the serial startup loop and run GREEN**

Keep `runStartup` ownership and retain-on-success semantics, but replace its timezone/class source loop with:

```go
batchTime := p.options.Now()
plans, failures := p.planStartupLanes(workerCtx, binding, batchTime)
if sharedCurrentNeeded(plans) {
    current, err := p.buildCurrentStats(workerCtx, binding, "startup")
    if err != nil {
        return errors.Join(append(
            failures, fmt.Errorf("startup current stats: %w", err),
        )...)
    }
    ref, err := p.cache.WriteCurrentStats(workerCtx, current)
    if err != nil {
        return errors.Join(append(
            failures, fmt.Errorf("startup write current stats: %w", err),
        )...)
    }
    applyStartupCurrentReference(plans, ref)
}
batchCtx, cancelBatch := context.WithCancelCause(workerCtx)
defer cancelBatch(nil)
failures = append(failures, p.fetchStartupSegments(
    batchCtx, cancelBatch, binding, plans,
)...)
defer func() {
    for index := range plans {
        for _, leased := range plans[index].leased {
            p.releaseLeasedReference(leased)
        }
    }
}()
if err := context.Cause(batchCtx); err != nil {
    return errors.Join(append(failures, err)...)
}
if err := p.requireCoordinatorOwned(batchCtx); err != nil {
    return errors.Join(append(failures, err)...)
}
failures = append(failures, p.publishStartupCohort(batchCtx, binding, plans)...)
if err := errors.Join(failures...); err != nil {
    return err
}
retainCoordinator = true
return nil
```

The batch cancel defer is registered before the successful-reference release
defer. LIFO execution therefore releases every original leased context and
segment lease before normal batch cancellation, while ownership-loss calls can
still cancel the same batch immediately with their explicit cause.

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

- [x] **Step 1: Run the full backend verification ladder**

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

- [x] **Step 2: Run a broad startup-branch review**

Generate a review package from Task 15's design commit through the exact code head. Review Standards and Spec, focusing on worker/channel cancellation, maximum source concurrency, publication ordering, lease release exactly once, coordinator retention, lane isolation, metric privacy, and no completed Pod state. Resolve every Critical and Important finding with covering tests and re-review.

- [x] **Step 3: Update current documentation status**

Record only commit SHAs, exact commands, pass/fail counts, and review findings. Mark the new design as implemented and reviewed but staging-disabled. Do not update `docs/architecture.md` or claim current runtime.

- [x] **Step 4: Commit the local review ledger**

```bash
git add docs/superpowers/specs/2026-07-23-team-usage-startup-cohort-publication-design.md \
  docs/superpowers/plans/2026-07-23-team-usage-startup-cohort-publication.md \
  docs/superpowers/plans/2026-07-21-team-usage-segmented-timezone-prewarm.md
git commit -m "docs(teamusage): complete startup cohort review"
```

Verify the tracked worktree is clean and record this commit as the only image-eligible Task 15 head.

**Completed local verification and review (2026-07-23):** The exact reviewed
code head is `30279888db6dad6c0f5e433879ba9573642fc461`. The Step 1 ladder ran
the six commands above in order: the focused test and race commands each
passed three packages with zero failures and zero race findings; the full
backend run passed 38 packages, reported 36 packages with no test files, and
had zero failures; vet, build, and diff checks exited zero. Two non-fatal macOS
linker warnings were recorded separately.

Before broad review, Task 3 vet/lifetime correction `0e442bc1` made batch cancel
ownership explicit at `runStartup` and removed the pointer-copy vet failure.
Initial Standards review reported `0 Critical / 2 Important / 1 Minor`: missing
startup phase error context, stale documentation/live-plan status, and a
duplicate test refresh mapping. Code correction `30279888` resolved the code
Important and Minor findings. Documentation commit
`f028b72ce61150b005b788b5518f97bc52b8a882` attempted to resolve the remaining
Important finding, but its first post-commit documentation review reported `0
Critical / 1 Important / 0 Minor` because the checked publication snippet did
not preserve the reviewed segment-context lifetime sequence. This follow-up
corrects that snippet. It does not claim its own approval: the clean
post-commit documentation review result and current only image-eligible head
are recorded in ignored SDD ledgers after review.

Initial Spec review reported `0 Critical / 1 Important / 0 Minor` only because
the already recorded final ladder rerun had not been read. After that evidence
was reviewed, the finding was withdrawn without a code change; final Spec
review reports `0 Critical / 0 Important / 0 Minor`.

At the Task 3 checkpoint no Task 15 image had been built or deployed. The clean
post-commit documentation review later established
`c5d3f6af15ea4db7ef424139822b043ee787f79d` as the image-eligible head, and the
Task 4 evidence below records its subsequent disabled publication, rollout, and
benchmark. Task 5 and Task 9 Steps 3-6 remain unchecked, and
`docs/architecture.md` remains unchanged. Because a tracked commit cannot
truthfully pre-record its own review result, the Task 3 image-eligibility
decision remains recorded in the ignored progress and task report ledgers.

---

### Task 4: Publish Exact Code And Repeat The Disabled Redis Benchmark

**Files:**
- Modify in `/Users/admin/helm`: `ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json`
- Modify with sanitized evidence: current Task 15 plan and design spec.

**Interfaces:**
- Consumes: exact reviewed Task 3 head and the Task 13 package-native benchmark harness by its recorded SHA.
- Produces: a multi-architecture staging image and a passing feature-disabled Redis command gate.

- [x] **Step 1: Verify immutable baselines**

  **Execution evidence (2026-07-23):** The application worktree was clean at
  exact reviewed head `c5d3f6af15ea4db7ef424139822b043ee787f79d` before the
  rollout. Staging was revision 56, feature-disabled, one of one replicas ready
  with zero restarts, and HTTP 200 live/readiness. Production was revision 69
  on `v0.1.0-preview.73`, feature-disabled, one of one replicas ready with zero
  restarts, and HTTP 200 live/readiness. The staging selector was mode `0600`;
  only that allowed selector was inspected, and unrelated Helm changes were
  neither inspected nor modified.

Require the application worktree clean. Record staging revision/image/flag/health and production revision/image/flag/health. Verify the staging selector is mode `0600`. Preserve and do not inspect unrelated Helm changes.

- [x] **Step 2: Build and verify the exact image**

  **Execution evidence (2026-07-23):** The exact head was published as
  `ghcr.io/lichking-2234/ai-efficiency:staging-c5d3f6af15ea4db7ef424139822b043ee787f79d`.
  The remote OCI index is
  `sha256:1e683cd90c1a5366e7b1d6a6ffff509ac99efb8a9079303383af9b539214df38`,
  with `linux/amd64` manifest
  `sha256:d7a265a9416669679f231ed7b6bb368f22f150f183ed9aa1ade01f47c44e1d76`
  and `linux/arm64` manifest
  `sha256:09932200dd0da885bfc3159e9ea06b6d66ebe93f8e6df0a32150dcc627622516`.

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

- [x] **Step 3: Deploy disabled through paused and restore-enabled phases**

  **Execution evidence (2026-07-23):** The tracked refresh script selected the
  exact image tag and snapshot `c5d3f6af15ea` while preserving every other
  selector byte. Render and server dry-run showed one replica and no Team Usage
  prewarm environment entry. Paused revision 57 reached zero application Pods;
  restore-enabled revision 58 reached one of one ready replicas on the exact
  image with zero restarts, no prewarm environment entry, and HTTP 200
  live/readiness. Production remained unchanged at revision 69.

Use the tracked staging refresh script and staging playbook. Server dry-run and rendered Deployment must show the exact image, one replica, and all Team Usage prewarm environment entries absent. Verify deployed, `1/1` ready, zero restarts, and HTTP 200 live/readiness.

- [x] **Step 4: Repeat the exact seven-class benchmark**

  **Execution evidence (2026-07-23):** The exact Task 13 package-native harness
  was restored mechanically with SHA-256
  `85702939d9775cb2980847ee04eb7ac06a3dde79ecd0f9a744565224e9448cb8`.
  Its static `linux/amd64` binary was `51,309,011` bytes. Smoke passed, followed
  by one full run with the following accepted aggregates:

  | Command class | Samples | p99 ms | Max ms | Budget ms | Returned command errors |
  | --- | ---: | ---: | ---: | ---: | ---: |
  | Current compressed write | 100 | 13.772 | 14.228 | 250 | 0 |
  | Segment compressed write | 100 | 19.497 | 49.805 | 250 | 0 |
  | Five-lease manifest publication | 100 | 13.456 | 13.717 | 250 | 0 |
  | Full-generation MGET | 100 | 73.820 | 78.506 | 250 | 0 |
  | Four-lane request MGET | 100 | 54.993 | 61.472 | 250 | 0 |
  | Lease acquire | 100 | 12.757 | 13.906 | 250 | 0 |
  | Lease release | 100 | 12.650 | 12.869 | 250 | 0 |

  Returned command, transport, INFO, and cleanup error counts were all zero.
  Final synthetic namespace key count was zero; eviction,
  rejected-connection, and peak-memory deltas were zero. The Redis lifetime
  `error-replies` counter increased by 18 during the observation window, but it
  is a global background counter, no benchmark command returned an error, and
  the increase cannot be attributed to this harness. Read, write, lease, and
  release budgets therefore remain `250ms`.

  The Pod binary was deleted and verified absent. Eleven task-specific local
  source, binary, and raw-report artifacts matching the established Task 15
  pattern were deleted; the final scan found zero matching paths.

Run smoke, then 100 valid samples for current write, segment write, five-lease manifest publication, full-generation MGET, four-lane request MGET, lease acquire, and lease release. Require zero returned command, transport, INFO, and cleanup errors; zero remaining synthetic keys; and zero eviction/rejected-connection deltas. Keep all four budgets at `250ms`.

Delete local source, binary, raw reports, and Pod binary; verify zero task-specific temporary artifacts. Any gate failure leaves staging disabled, keeps Task 4 unchecked, records sanitized evidence, and stops.

- [x] **Step 5: Record the passing disabled gate**

  **Execution evidence (2026-07-23):** Staging ended at revision 58 on the
  exact tested image, feature-disabled, with one ready replica, zero restarts,
  no prewarm environment entry, and HTTP 200 live/readiness. Production remained
  revision 69 on `v0.1.0-preview.73`, feature-disabled and healthy. The
  mode-`0600` staging selector intentionally retains only the exact-image and
  snapshot update and remains uncommitted and unpushed; unrelated Helm changes
  remain untouched. Task 5 and Task 9 Steps 3-6 remain unchecked, and
  `docs/architecture.md` remains unchanged.

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

  **Attempt evidence (2026-07-23, incomplete):** The Task 14 capture watcher
  was mechanically recovered from structured session
  `019f8948-18f6-79f3-aad8-d02da8457cbb` with SHA-256
  `624b2def3420b0cabc04c42594c765540baa1eb1fa9dff3f96221b3a324107dc`.
  The adapted observer had SHA-256
  `d5915970ded740b1f4480ccd84455cdea22537b91d8e1a004ca214083e4e2f27`
  and passed syntax, fixed-field parsing, and forbidden-command checks. It was
  launched before the enabled upgrade, but exited without a retained result.
  No per-Pod counter-zero baseline survived, so this step remains unchecked.

Create only session-temporary watcher files. For each new Pod capture counter-zero baselines as soon as the metrics listener is available. Retain only durations, closed counts/outcomes, digests, and revisions. Do not use an account, call Team Usage API lanes, print response bodies, or read raw Redis values.

- [ ] **Step 2: Enable exact code on two staging replicas**

  **Attempt evidence (2026-07-23, incomplete):** Deployment-only render and
  server dry-run completed before the staging-only atomic upgrade. Revision 59
  completed on the exact image with two desired, ready, updated, and available
  replicas, `AE_TEAM_USAGE_PREWARM_ENABLED=true`, and the exact four approved
  timezones. The observer/orchestration then exited before retaining the
  required per-Pod zero-restart and HTTP health proof, so this step remains
  unchecked even though the enabled revision itself completed.

Render and server-dry-run the exact image, two replicas, `AE_TEAM_USAGE_PREWARM_ENABLED=true`, and the approved four timezones. Apply atomically only to staging. Verify `2/2` ready, exact image, zero restarts, and HTTP 200 health.

- [ ] **Step 3: Enforce every diagnostic gate**

  **Failure evidence (2026-07-23):** No observer result was retained. Therefore
  none of the following is proven or disproven: owner startup-success count,
  loser lease-busy count, loser source count, either scheduler-tick count,
  complete schema-v2 manifest/reference count, publication spread, maximum
  deployment-wide source concurrency, Relay error count, Redis operation/error-
  class deltas, or bounded pool pending/wait/timeout values. The closed failure
  is missing observation evidence, not a product-gate failure. This step remains
  unchecked, and the one approved replay has been consumed.

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

- [x] **Step 4: Restore disabled regardless of result**

  **Execution evidence (2026-07-23):** The orchestration PTY retained only exit
  `1`, no trap result, and no observer result. Its recorded source installs an
  EXIT/INT/TERM handler whose guarded rollback precedes internal cleanup. The
  internal cleanup ran, but the selector remained enabled on deployed revision
  59 and Helm history contained no automatic restore revision. With no external
  cleanup or restore, this proves the guarded rollback branch was bypassed; the
  guard's runtime boolean value was not retained, so no deeper cause is claimed.

  A manual staging-only atomic upgrade then restored the exact image disabled
  with one replica at revision 60. Fresh verification found `1/1` ready,
  updated, and available, zero restarts, every prewarm environment entry absent,
  and HTTP 200 live/readiness. Production remained revision 69 on
  `v0.1.0-preview.73`, disabled, zero restarts, and HTTP 200 live/readiness. The
  mode-`0600` selector is exact-image disabled and remains uncommitted and
  unpushed with every non-selector byte unchanged.

Immediately apply the exact-image disabled selector with one replica. Verify a new Helm revision is deployed, `1/1` ready, all prewarm environment entries absent, and HTTP 200 live/readiness. Reverify production unchanged. Restore the tracked selector to its deliberate exact-image disabled bytes, keep mode `0600`, and preserve unrelated Helm changes.

- [x] **Step 5: Record, clean, review, and commit**

  **Execution evidence (2026-07-23):** The failed orchestration removed its
  session-temporary files before its result could be retained; the deleted count
  is therefore unknown. Fresh final scans found zero `/tmp/ae-task15-*` paths
  locally and zero in the final staging application Pod, and no task-specific
  temporary values file remains. This plan and the current design spec record
  the failure without raw metrics, Redis keys/values, manifests, response
  bodies, credentials, identities, or user data. Task 5 remains failed; Task 9
  Steps 3-6 remain unchecked; `docs/architecture.md` remains unchanged. A second
  replay is not authorized by this result.

Delete watcher source, binaries, metrics, and raw reports; verify zero task-specific `/tmp` and Pod artifacts. On pass, check Task 5 and record that Task 9 Step 3 may restart; on failure leave it unchecked and record the exact closed blocker. In both cases keep Task 9 Steps 3-6 unchecked and `docs/architecture.md` unchanged.

Commit with accurate pass/fail wording:

```bash
git commit -m "docs(teamusage): record startup cohort staging replay"
```

Generate a task-scoped ledger review. Resolve every Critical and Important evidence finding before reporting the final state.

#### Guarded Replacement Replay Evidence

The one replacement replay authorized by the replay-guardian design ran once.
One enable completed at staging revision 61 and one guardian rollback completed
at revision 62. The feed closed `old_pod_set_changed`; the controller detected
that failure and requested restoration before continuing enabled observation
and marker completion. The
observer's `fresh_pod_selection_invalid` result consequently overlapped rollback
and is not an authoritative product result.

Guardian state was `restore_succeeded:explicit_restore`. The controller then
performed one transient final Pod read and recorded `restore_failed` during
normal rollback convergence. Fresh final verification passed revision 62 as
exact-image disabled `1/1`, zero restart, no prewarm environment, frozen image
digest, and HTTP 200 live/readiness. Production revision 69 remained unchanged.

The authoritative classification is `operational failure`; product pass and
product-gate failure are both unproven. Task 5 and Task 9 Steps 3-6 remain
unchecked, final Task 16 local/Pod artifact counts are zero,
`docs/architecture.md` remains unchanged, and no third replay is authorized.

#### Separately Authorized HTTP Acceptance Preflight

On 2026-07-24, one later user-authorized Task 9 HTTP acceptance preflight began
from healthy exact-image disabled staging revision 64 and unchanged production
revision 69. A rollback guardian was armed before the exact image was enabled
only in staging at revision 65 with two ready replicas, zero restarts, the four
approved timezones, and HTTP 200 live/readiness.

The pre-request metrics gate failed before login, cache deletion, or any Team
Usage API request. The anonymous replicas recorded `4 startup/success` with
four successful startup source observations versus `4 startup/lease_busy` with
zero startup source observations. Redis contained four schema-v2 manifest keys,
but its counters already contained three `command_deadline` errors: two
`generation_read` and one `manifest_read`. Four background cycles ended in
error. Relay recorded 150 requests and zero errors.

Fail-fast restoration therefore began without accepting lane completeness,
cold or warm timings, `full_hit`, payload hashes, or request-time Relay deltas.
No account was authenticated, no Redis key was deleted, no application API was
called, and no response body was retained. The guardian produced healthy
exact-image disabled staging revision 66 at one ready replica with zero
restarts, no prewarm environment, and HTTP 200 live/readiness. Production
revision 69 remained unchanged. Task 9 Steps 3-6 remain unchecked and
`docs/architecture.md` remains unchanged.

#### Separately Authorized Latency-Only Observation

On 2026-07-24, the user separately authorized one measurement of the current
image after the preflight failure. This permission covered staging testing
only: it did not relax any acceptance gate or authorize another implementation
task. The run began from healthy exact-image disabled staging revision 70 and
unchanged production revision 69. A fresh independent guardian was armed before
the exact image was enabled only in staging at revision 71 with two ready
replicas and the four approved timezones.

Before login, metrics showed four complete schema-v2 manifests with a
`1.000s` cohort spread, zero scheduler ticks, zero Redis/Relay/cache/pool
errors, and positive manifest TTL. By the request baseline, two scheduler ticks
had started. No response-cache key needed deletion and the scope-origin cache
was empty. The four concurrent Asia/Shanghai 30-day cold requests all returned
HTTP 200 and recorded `full_hit`; end-to-end durations were Summary `4.939s`,
Organization `5.139s`, Trend `5.956s`, and Members `7.344s`. Corresponding
server durations were `4.480s`, `4.676s`, `5.129s`, and `6.504s`. Only Summary
met the five-second cold bound.

The immediate warm requests returned HTTP 200 in `0.512s`, `0.519s`, `0.785s`,
and `0.863s` respectively, all below `1.5s`; server durations were between
`0.035s` and `0.043s`. Every cold/warm business hash matched. Redis,
Relay-error, cache-error, non-full-hit, and pool-timeout counters remained zero.
Relay request counters increased by 12 during cold and two during warm, split
across both Pods, while the scheduler counter remained at two. Because the
scheduled background cycles were already in flight, those aggregate deltas
cannot establish whether the application requests themselves made Relay calls.
The zero-request-time-Relay gate is therefore unproven, not passed.

The result is an acceptance failure on three cold latency lanes, background
tick interference, and unproven request-time Relay isolation. No response body,
credential, identity, Redis key, or Redis value was retained. The guardian
performed the only rollback and produced healthy exact-image disabled staging
revision 72 at one ready replica with zero restarts and HTTP 200 live/readiness.
Production remained healthy and unchanged at revision 69. Task 9 Steps 3-6
remain unchecked and `docs/architecture.md` remains unchanged.

#### Separately Authorized Full-Page Cold Observation

On 2026-07-24, the user separately requested the real `/usage/team` page cold
time rather than API-only timings. Exact image staging revision 73 ran two ready
replicas with four complete manifests. A fresh 1440x1000 browser context had an
authenticated local-storage state but no site resources or application state.
Only staging Redis DB 2 was touched: four page response keys were removed while
prewarm values remained intact. Production Redis DB 0 was not touched.

From navigation start, response start was `0.155s`, DOMContentLoaded/load was
`0.724s`, FCP was `1.192s`, and LCP was `6.032s`. The four business requests
started at `1.142s`. Summary and Members ended at `5.989s`, Trend at `8.601s`,
and Organization at `8.772s`. All required data components were rendered at
`8.765s`; the two-frame data-rendered completion marker was `8.786s`. The page
loaded 18 resources and 10 scripts, transferring 250,595 bytes and decoding
563,316 bytes. There were zero console, page, request, or loading-state errors.

Metric deltas proved four outer-cache misses, zero fresh hits, four prewarm
`full_hit` results, and zero non-full-hit or Redis/Relay error deltas. During
the same page window, five background scheduler ticks and 50 Relay requests
occurred, so background work again contaminated request-time attribution. The
full-page cold result fails the five-second goal. No screenshot, response body,
credential, identity, Redis key, or Redis value was retained. The guardian
restored healthy exact-image disabled staging revision 74 at one ready replica;
production revision 69 remained unchanged. Task 9 Steps 3-6 remain unchecked.

One later user-requested repeat used the installed Google Chrome
`150.0.7871.182` binary with a fresh temporary profile. Metrics proved four
outer-cache misses, zero fresh hits, four prewarm `full_hit` results, and zero
error deltas. The request was the default 30-day Asia/Shanghai window. TTFB was
`0.157s`, DOMContentLoaded was `0.745s`, FCP was `1.228s`, and LCP was `6.336s`.
Summary, Members, Trend, and Organization ended at `5.516s`, `6.300s`,
`7.627s`, and `7.627s`; fully rendered completion was `7.796s`. Four background
scheduler ticks and 42 Relay requests overlapped the page window. The guardian
restored healthy exact-image disabled staging revision 76; production revision
69 remained unchanged. No browser profile or authentication artifact was
retained.
