# Codex Commit Token Attribution v2 Implementation Plan

**Date:** 2026-08-11
**Status:** T01-T12 and T14-T22 are implemented, released where applicable, and production-qualified. T18/T19 merged through PR #306 as `b56419d6`, T20 merged through PR #307 as `2d5b5574`, and T21 merged through PR #308 as `403f9854`; CLI-only `ae-cli/v0.2.0-preview.12` contains all three recovery stages. The exact current-runtime inline-wrapper parser repair and scanner-progress invalidation merged through PR #325 as `c757fbd5` from implementation commit `37bce887`, with the full local ae-cli suite, PR-head CI `32359847423`, merge-SHA main CI `32362180500`, and final review green. CLI-only `ae-cli/v0.2.0-preview.13` was then published from `f54184a6` by release run `32364192912`, installed through the approved proxy path, and qualified by Helm commit `c35758f5`: one `relay_official` group, 112 reconciled and materialized Requests, four direct pools, exact four-component conservation, and `21,668,159` Token visible on the repo 72 Activity row. Managed-hook replay changed none of those identities or totals. T22/#305 is complete, but this operator canary remains excluded from T13's ordinary-workflow gate. The selected-window denominator repair first shipped in platform `v0.1.0-preview.87`; exact readback established replacement #252 Day 0 at `2026-08-17T05:32:57.925948Z`, and production now runs `v0.1.0-preview.88` at Helm revision 88. The 2026-08-20 T13 evidence snapshot passed, including independently qualified ordinary PR #319 and complete SCM coverage. #252 cleanup has not run and still requires an immediate final re-read plus separate implementation/release/deployment/destructive-migration authority. The replacement Day 0 remains a historical and conservation baseline, not a fixed waiting clock.
**T13 invalidated baseline:** The provisional Day 0 at `2026-08-14T07:22:18.199843Z` is retained only as historical evidence. Its formal-pool facts remain 7 pools, 7 direct relations across 6 commits, `6,890,621` Token, 71 source requests/responses, and zero gaps, but the recorded `35,272,145,109` denominator was cumulative personal Usage rather than the selected 2026-08-13 through 2026-08-14 window. A later production reproduction showed the same cumulative `35,312,542,273` denominator for 2-day, 7-day, and 30-day selections while their Usage trend totals differed. The earlier `2026-08-21T07:22:18.199843Z` cleanup eligibility is void. Its replacement requirements were satisfied by the release, deployment, and readback recorded below; the old clock remains permanently invalid.
**T13 replacement Day 0:** The complete production readback at `2026-08-17T05:32:57.925948Z` is the active historical and conservation baseline. Personal Activity and Usage used the same `Asia/Shanghai` local-day windows: 2-day `2026-08-16..17` returned true-zero committed Token over `96,556,726` Usage Token; 7-day `2026-08-11..17` returned exact `6,890,621 / 3,564,608,255 = 0.19330654330204933%`; and 30-day `2026-07-19..08-17` returned exact `6,890,621 / 15,156,957,118 = 0.04546177010566904%`. Every Activity denominator equaled the corresponding Usage trend sum while the cumulative Usage stats value was independently `35,085,487,404`. Claim coverage was complete, readiness was active, and focused PR sync jobs 31/32 restored complete SCM coverage with zero failed, partial, unsynced, or stale Repository. The conserved formal baseline is 7 pools, 7 direct relations across 6 commits, `6,890,621` Token, 71 source requests/responses, zero gaps, and zero duplicate pool/relation/provider-scoped Request identities; the v1 tables remain `0/0`, the one shadow pool remains isolated, formal near-expiry/finalization errors are `0/0`, and v1 writes return structured `409 upgrade_required`. This baseline measures later deltas; it does not impose an earliest cleanup time.
**T13 evidence snapshot:** AI Efficiency PR #319 was created and merged after replacement Day 0 as an ordinary `feat(usage)` delivery. Its commit `429c6f00e25b21425b0d87fb9e34bdd960e661a3` owns five direct formal pools and exactly `1,203,261` input + `72,762` output + `0` cache creation + `21,934,592` cache read = `23,210,615` Token. Focused SCM jobs 33/34/35 completed `16/16`, `176/176`, and `66/66` usage refreshes with zero failures after repo 72 was bound to the existing matching Bitbucket provider and its webhook registered. Activity then reported complete claim and SCM coverage, active readiness, and an exact fresh `135,054,403 / 1,966,122,454 = 6.869073832366699%` ratio for `2026-08-17..20`. The repeatable-read production snapshot at `2026-08-20T13:31:51.123751Z` conserved 37 formal pools, 37 direct relations across 11 commits, `4,882,592` input + `324,812` output + `0` cache creation + `137,123,584` cache read = `142,330,988` Token and 946 Requests. Coverage gaps, pool/relation/provider-scoped Request duplicates, pool conservation errors, terminal Request errors, near-boundary pending Requests, finalization errors, and expired unfinalized groups were all zero; 41 pending Requests were before the final-attempt boundary, v1 remained `0/0`, the single shadow pool remained isolated, and the v1 probe returned structured `409 upgrade_required`. Live/ready and database, Redis, and Relay checks were green on platform `v0.1.0-preview.88`.
**Design:** [Codex Commit Token Attribution v2](../specs/2026-08-11-codex-commit-token-attribution-v2-design.md)

## Delivery Rules

- This is a live execution ledger. Check a box only in the same work turn that
  completes and verifies it.
- Pre-cutover v1 and `shadow_v2` data remain non-formal; only the post-cutover
  `formal_v2` epoch feeds Activity and readiness.
- Do not modify `sub2api` source or couple to its database.
- Do not release, deploy, or reset data without explicit authority in that
  execution turn.
- Each implementation ticket must preserve unrelated worktree changes and
  update this plan immediately after completing a step.

## Dependency Map

```text
T01 contract publication (#253)
  -> T02 local proof -> T05 delivery
  -> T03 ingest -> T04 reconciliation -> T07 durable pools
                                      -> T06 lifecycle -> T08 reads -> T09 UI
  -> T03 ingest -> T05 delivery
  -> T05 + T07 -> T10 lineage
  -> T05 + T06 + T08 + T09 + T10 -> T11 qualification -> T12 cutover -> T13 cleanup
  -> T14 bounded delivery + T15 automatic Repository registration -> T16 sustained qualification -> T13 adoption gate
  -> T17 WebSocket local Token expansion -> separately authorized release/canary
  -> T18 machine coordinator (#301) + T19 compaction recovery (#302)
     -> T20 deleted-worktree recovery (#303) -> T21 backlog migration (#304)
     -> T22 Helm release canary (#305)
```

## Execution Ledger

### T01 — Freeze the v2 contract and conflicting work ([#253](https://github.com/LichKing-2234/ai-efficiency/issues/253))

- [x] Merge the approved v2 design and this plan into the repository.
- [x] Publish the tracking issue and actionable child tickets.
- [x] Remove `ready-for-agent` from conflicting #232-#234 and mark them blocked
  on the appropriate v2 delivery tickets.
- [x] Verify the formal spec, plan, issue dependency graph, and source-of-truth
  navigation agree.

### T02 — Provider-aware Request/turn/mutation/commit proof ([#241](https://github.com/LichKing-2234/ai-efficiency/issues/241))

- [x] Preserve backend provider ID through discover, tool config, reporting
  state, and immutable claim-group identity.
- [x] Correlate the exact `sub2api` logical Request identity, thread, turn,
  structured mutation, and deterministic Git content without cwd/time/path
  heuristics on the forced-HTTP App Server path. The released HTTP identity
  implementation passed the fresh real canary. Responses WebSocket remains
  unsupported and fails closed.
- [x] Support multi-Request turns, late Requests, active/archived source
  recovery, explicit gaps, and a single calibration envelope.
- [x] Verify privacy and deterministic evidence fixtures.

### T03 — v2 claim ingest and item-level ACK ([#242](https://github.com/LichKing-2234/ai-efficiency/issues/242))

- [x] Add isolated v2 hot group/Request claim schema and routes.
- [x] Enforce provider/Request uniqueness, identical replay, conflict, owner,
  checkpoint, and payload bounds.
- [x] Return independent group-envelope and per-Request ACK states.
- [x] Verify response-loss replay and v1/v2 epoch isolation.

### T04 — Exact `sub2api` Request reconciliation ([#243](https://github.com/LichKing-2234/ai-efficiency/issues/243))

- [x] Implement the current admin usage HTTP contract behind a narrow
  `relay.Provider` Request usage reader.
- [x] Centralize 0/1/many, relay-owner, model, Token, overflow, and consistency
  rules.
- [x] Add database leases, bounded concurrency, retry/backoff, and
  multi-replica collapse.
- [x] Verify success and all fail-closed/retry branches with fake providers.
- [x] Run a separately authorized read-only canary against the real endpoint.
  Released CLI `ae-cli/v0.2.0-preview.4` produced two normalized Request claims
  for one exact commit. Both reconciled on attempt one; official totals were
  materialized once into one direct `shadow_v2` pool. No Request identity is
  retained in the long-lived pool.

### T05 — Git hooks, outbox, runner, and OTel exit ([#244](https://github.com/LichKing-2234/ai-efficiency/issues/244))

- [x] Make post-commit/post-rewrite/fail-open pre-push persist and wake v2 work.
- [x] Drain work arriving during a runner and recover offline unresolved
  checkpoints without another Git event.
- [x] Consume item-level ACKs without deleting unknown/conflicting data.
- [x] Stop configuring AE Codex OTel and preserve user-managed OTel.

### T06 — Claim lifecycle, finalization, and health ([#246](https://github.com/LichKing-2234/ai-efficiency/issues/246))

- [x] Implement pending/reconciled/mismatch/ambiguous/expired states and partial
  lower bounds.
- [x] Calculate the final source lookup deadline at least 24 hours before the
  nominal upstream retention boundary, then transactionally freeze,
  materialize, gap, and clean.
- [x] Add bounded operational metrics without Request IDs in product DTOs.
- [x] Verify expiry, shutdown, lease recovery, and the exact 24-hour safety
  boundary on both sides.

### T07 — Durable usage pools and global shared conservation ([#245](https://github.com/LichKing-2234/ai-efficiency/issues/245))

- [x] Add the unified usage-pool and pool-commit model keyed by user, canonical
  counting commits, requested model, and 15-minute UTC bucket.
- [x] Materialize official components and Request count exactly once.
- [x] Atomically migrate direct to shared when a later commit is proven.
- [x] Verify concurrency, conservation, 90-day cleanup, and privacy.

### T08 — Activity v2 reads and Usage denominator resolver ([#248](https://github.com/LichKing-2234/ai-efficiency/issues/248))

- [x] Return backend-owned scope totals, Repository direct/shared, PR involved,
  daily trend, coverage, and formal readiness without row-derived totals.
- [x] Reuse personal/team Usage services and add an authorization-isolated
  member metrics cache behind one denominator resolver.
- [x] Require exact range/timezone, fresh/complete denominator, and shared
  `as_of`; require complete coverage of every scoped provider/subject and
  return explicit exact/lower-bound/unavailable/zero states.
- [x] Add server search/sort/cursor pages of 20, IANA local-day aggregation,
  scope authorization, cache/cursor versioning, and query-plan tests.
- [x] Make repository management reads/mutations administrator-only without
  blocking CLI/reporting repository resolution routes.

### T09 — Activity v2 frontend and repository administration IA ([#249](https://github.com/LichKing-2234/ai-efficiency/issues/249))

- [x] Build the ratio, committed daily trend, Repository Top 5, PR Top 5, and
  full Repository/PR tabs from backend contracts only.
- [x] Label the donut “Used for committed code / Other Token”; show adjacent
  period percentage-point change only when both periods are complete and fully
  inside the comparable formal epoch.
- [x] Implement 7/30/90/custom local-day range, URL state, overall ranking
  context, and `repo_id`/`pr_record_id` in-page filtering.
- [x] Reuse Usage organization/member navigation without personal metrics or
  ranking.
- [x] Implement independent section loading/error/refresh, exact empty/zero
  states, desktop table, and mobile card rendering.
- [x] Move `/repos` into administrator navigation and remove Activity/PR/Token
  analysis from repository pages.
- [x] Verify i18n, accessibility, role E2E, build measurement, responsive
  viewports, and absence of Request/pending/gap details.

### T10 — Rewrite, orphan, and cherry-pick lineage ([#247](https://github.com/LichKing-2234/ai-efficiency/issues/247))

- [x] Migrate pools through explicit hot and post-retention rewrite mappings.
- [x] Reject mapping conflicts/cycles and keep unmapped history as orphaned.
- [x] Preserve strict inherited-non-counting cherry-pick behavior.
- [x] Verify stacked/backport PR projections do not change global Token.

### T11 — Shadow qualification and real E2E gate ([#250](https://github.com/LichKing-2234/ai-efficiency/issues/250))

- [x] Run synthetic contract, fault-injection, multi-replica, scale/query-plan,
  and full backend/CLI/frontend suites.
- [x] Verify the complete scope/range/ratio/shared/filter/loading/mobile matrix.
- [x] Explicitly verify provider-set completeness, ratio labels,
  percentage-point comparison, cutover incomparability, and omission behavior.
- [x] Prove final reconciliation is scheduled no later than 24 hours before
  nominal upstream cleanup, including exact-boundary and late-ingest cases.
- [x] Complete one controlled real Request-to-commit-to-Activity canary without
  contaminating the formal epoch. The shadow Activity aggregation read back
  19,607 committed Token, two Requests, one direct Repository pool, and no
  shared Token. The formal Activity API remained at zero/waiting-for-data.
- [x] Produce cutover checklist, dashboards, exact reset query, evidence export,
  and rollback runbook.
- [x] Close every P0/P1 finding before #251 may start.

### T12 — Explicit v2 cutover and v1 POC reset ([#251](https://github.com/LichKing-2234/ai-efficiency/issues/251))

- [x] Obtain explicit deletion, release, and deployment authority.
- [x] Gate v1, freeze epoch, exclude shadow data, switch reads, export exact v1
  evidence, and reset the resolved POC dataset in the approved order.
- [x] Update `docs/architecture.md` and current-contract navigation in the same
  delivery.
- [x] Read back v1 rejection, v2 writes, Activity/readiness, zero old totals,
  release artifacts, health, and production rollout.

### T13 — Evidence-gated legacy cleanup ([#252](https://github.com/LichKing-2234/ai-efficiency/issues/252))

- [x] Release and deploy the selected-window personal denominator repair with
  separate authority, verify the production ratio, and record replacement Day 0.
- [x] Replace the fixed seven-day wait with an execution-time evidence gate;
  retain replacement Day 0 only as the historical and conservation baseline.
- [x] From the replacement Day 0 baseline, independently classify ordinary
  AI Efficiency PR #319 and read back its five direct `formal_v2` pools.
- [x] Record one complete live/ready, structured v1 `upgrade_required`,
  reconciliation/error, near-expiry, pending-boundary, Activity/readiness/ratio,
  SCM freshness, formal conservation, duplicate/gap, and zero-v1 snapshot.
- [ ] Repeat every volatile gate immediately before mutation in the separately
  authorized cleanup execution window.
- [ ] Remove v1 ingest/read/frontend Bucket surfaces, AE OTLP route/config, and
  legacy-only credentials/fields without touching user OTel.
- [ ] Verify schema/data safety, upgrade behavior, backend/CLI/frontend tests,
  separate platform/CLI releases, and production health.
- [x] Verify #232-#234 were completed by merged PR #235 and closed before the
  cleanup window; do not defer their closure to T13 execution.
- [x] Reproduce the cumulative personal-denominator defect and add red/green
  backend and frontend coverage for exact range totals and visible non-zero ratios.

#### T13 preflight record — 2026-08-13

- [x] Correct #240/#252/#278 so closed #276/#277 and the obsolete cleanup date
  no longer appear as current blockers.
- [x] Add upgrade-seam protection for exact AE-managed OTel cleanup,
  user-modified OTel preservation, and local legacy-token removal.
- [x] Rehearse the planned legacy DDL in a rolled-back PostgreSQL transaction
  and prove formal Token components, Request count, pool count, and commit
  relations remain exact.
- [x] Strengthen the existing release workflow contract so a normal CLI-only
  release cannot publish packages/images or reference Docker/Helm paths.
- [x] Publish the removal inventory, staged deployment boundary, readbacks, and
  rollback contract in
  [`2026-08-13-attribution-v1-cleanup-preflight.md`](./2026-08-13-attribution-v1-cleanup-preflight.md).
- [x] Complete all preflight suites and code review, then merge without release,
  deployment, or production cleanup.

### T14 — Bounded and resumable v2 claim delivery ([#276](https://github.com/LichKing-2234/ai-efficiency/issues/276))

- [x] Share one 90-day-bounded Request-evidence query and active/archive source
  scan across every pending commit trigger in a runner pass.
- [x] Persist digest-only `source × trigger` completion units after claim-state
  persistence so timeout, process exit, backend failure, and newly arriving
  triggers resume without duplicate claims or full restart; invalidate units
  only when that source's digest-only turn evidence changes so late Requests
  are recovered without unrelated full restart.
- [x] Make status and doctor expose the safe failure stage/reason, first failure
  time, and exact remaining trigger count without Request identifiers.
- [x] Add deterministic multi-trigger, response-loss, source-interruption
  resume, privacy, and no-duplication coverage; qualify 2,268 expired sources
  plus one 50,000-line recent source within the five-second fixture budget.
- [x] Run the final full suites and close all code-review findings.

### T15 — Automatic Repository registration during hook delivery ([#277](https://github.com/LichKing-2234/ai-efficiency/issues/277))

- [x] Add the reporter-authenticated minimum Repository ensure route and reuse
  canonical identity uniqueness for idempotent concurrent creation.
- [x] Continue the exact first post-commit after `not_found`, including when a
  cached negative result exists; keep Git fail-open and preserve recovery work
  if registration fails.
- [x] Keep automatic registration unbound and free of credential/webhook
  mutation; a manual commit creates no claim or Token.
- [x] Add focused backend, client, hook, authorization, replay, and privacy
  tests and update the active contract/architecture.
- [x] Run the final full suites and close all code-review findings.

### T16 — Sustained production qualification and missed-commit recovery ([#278](https://github.com/LichKing-2234/ai-efficiency/issues/278))

- [x] Obtain explicit release, deployment, recovery, and production-data
  mutation authority in the execution turn.
- [x] Release/deploy the reviewed T14/T15 changes as
  `ae-cli/v0.2.0-preview.6` and platform `v0.1.0-preview.84`, then deploy Helm
  revision `84`. Ship the exact wrapper-proof follow-up as CLI-only
  `ae-cli/v0.2.0-preview.7`; do not create another platform release or Helm
  rollout for that CLI-only fix.
- [x] Exercise the normal `not_found -> ensure -> continue` hook path for
  agent-infra without `ae-cli init`, creating exactly one unbound Repository
  configuration, then replay the three post-closeout main commits through the
  released `.7` runner. The bounded scan resumed after its five-minute
  boundary and cleared its progress/task state, but no commit retained the
  complete Request -> turn -> structured mutation -> Git-content proof. The
  replay therefore failed closed with no new claim, pool, relation, Request,
  or Token. Aggregate before/after conservation stayed at one formal pool,
  one direct relation, `4,395` Token, and one Request; duplicate pool,
  relation, and Request identities were all zero; shadow stayed isolated and
  v1 stayed zero.
- [x] Qualify sustained hook delivery on the isolated
  `test/ae-attribution-hook-qualification` branch without manual sync. Two
  initial commits proved post-commit capture, trigger coalescing, five-minute
  progress persistence, and pre-push recovery but correctly produced no claim
  because their long-lived Codex session predated the HTTP-only configuration
  and still used Responses WebSocket. A third commit generated by a freshly
  started Codex TUI had direct HTTP Request-to-turn evidence and produced one
  acknowledged formal group with six reconciled Requests, one agent-infra
  direct relation, and `168,962` official Token. The resulting totals of two
  pools and `173,357` Token exactly conserve the `4,395 + 168,962` inputs. This
  total is visible through the personal Activity API with complete claim
  coverage and an exact Usage-backed ratio; its Repository page ranks
  agent-infra at `168,962` direct Token and ai-efficiency at `4,395`. This
  qualification added no unresolved Agent Infra hook event. The status command
  still exposes `1,111` pre-existing global unresolved events spanning 748
  historical workspaces, so the later global status-cleanliness gate remains
  open and no legacy queue cleanup was performed. This operator qualification
  remains excluded from T13 adoption.
- [x] Restore the current Activity SCM coverage checkpoint without batch
  mutation. Bind only agent-infra repo 70 to the single active GitHub provider,
  register its webhook, and run focused PR sync jobs 29 and 30. Agent Infra
  completed 63/63 and AI Efficiency 168/168 PR usage refreshes with zero
  failures. Activity read back complete SCM coverage with every affected
  category at zero, while formal committed Token remained `173,357`. Because
  freshness expires after 24 hours, this proves the recovery path but does not
  check the final T13 freshness gate in advance.
- [x] Observe at least two subsequent genuine ordinary commits from any eligible
  Repository through hooks without manual sync. They may come from the same or
  different Repositories; at least one workflow must produce a new ordinary
  formal pool visible in Activity. Automatic Repository registration or reuse,
  status/doctor, and the formal pool/relation/Token delta must all read back
  correctly and conserve exactly. Agent Infra commits `6c8bd9c8` and
  `1ed61c8f` were both captured as ordinary `post-commit` work and remain
  visible in formal Activity. The final replay recovered late official usage:
  `6c8bd9c8` grew from 5 Requests / `973,350` Token to 15 Requests /
  `3,069,557` Token, while `1ed61c8f` grew from 24 Requests / `2,337,635`
  Token to 30 Requests / `3,015,023` Token. The exact combined increase is
  16 Requests and `2,773,595` Token without a new pool or relation identity.
- [x] Release the single-allocation evidence-digest and terminal-conflict
  quarantine repair, then use an ordinary managed hook rather than
  `ae-cli sync` to drain the ten retained triggers. Preserve terminal conflict
  audit state while proving that it is not retransmitted and does not block a
  later trigger; `upgrade_required` and unknown acknowledgements must remain
  fail-closed. PR #297 merged as `b3349ba7`; `ae-cli/v0.2.0-preview.11`
  release run `31778152726` published six assets. Two ordinary managed
  `pre-push` wakes resumed the bounded scan, cleared all ten triggers, retained
  exactly one terminal conflict, and ended at `pending=0 conflict=1
  upgrade_required=0` with no task or scan progress.
- [x] Complete the T13 adoption and final SCM-freshness readbacks. These created
  the provisional `2026-08-14T07:22:18.199843Z` baseline, which the later
  personal-denominator defect invalidated for cleanup timing.

### T17 — Responses WebSocket Codex-local Token expansion ([#269](https://github.com/LichKing-2234/ai-efficiency/issues/269))

- [x] Replace the obsolete Relay turn-discovery dependency with trusted local
  WebSocket completion evidence plus Codex JSONL `last_token_usage`, requiring
  cumulative snapshots for exact terminal-row deduplication.
- [x] Keep HTTP `relay_official` and WebSocket `codex_local` mutually exclusive;
  upload no WebSocket Request/response identity and retain only bounded
  model/15-minute UTC aggregates in the hot group.
- [x] Materialize WebSocket aggregates transactionally into the existing pool,
  accept only monotonic late growth/allocation extension, preserve same-pool
  lineage, and remove hot local detail without deleting durable Token.
- [x] Cover cache normalization, repeated terminal rows, same-bucket and
  cross-bucket aggregation, missing cumulative usage, mixed transports,
  invalid/overflow payloads, replay, late growth, shared allocation, rewrite,
  finalization, and cleanup with focused tests.
- [x] Run final full ae-cli/backend suites, diff checks, and code review; close
  every actionable finding before publication.
- [x] Resolve the follow-up PR P1 findings as then understood by resetting the
  cumulative baseline at each turn and preserving `codex_local` across
  failed-scan recovery, with focused RED/GREEN regressions and the full ae-cli
  suite. The later real multi-turn canary superseded the reset-only assumption.
- [x] Refresh #269 to the implemented contract without the obsolete sub2api
  dependency or Relay turn-discovery design.
- [x] Merge PR #287 as `39a4c88b` and verify exact merge-SHA main CI run
  `31712698968` passes backend, frontend, ae-cli, and deploy-static without a
  release, deployment, or production mutation.
- [x] Release `ae-cli/v0.2.0-preview.8` and platform
  `v0.1.0-preview.85`, then deploy production Helm revision `85` with live and
  ready checks green.
- [x] Run two real Codex 0.147 WebSocket commit canaries through ordinary
  post-commit hooks. Both failed closed on the missing raw completion-log
  shape, and exact production readback proved zero claim, pool, relation,
  Request, or Token rows for either commit.
- [x] Reproduce that released-scanner failure with a focused regression, join
  the trusted transport and successful-sampling rows by exact thread/turn,
  keep older raw completion compatibility, pass the full ae-cli suite, and
  replay the real TUI fixture locally as one gap-free `codex_local` claim with
  `294,210` Token and one deterministic commit allocation.
- [x] Complete final two-axis code review and the first PR #290 hosted CI run;
  both review axes have zero findings and run `31721565534` passed all four
  jobs.
- [x] Merge PR #290 as `aecc7754`, verify merge-SHA main CI run `31766333707`,
  release `ae-cli/v0.2.0-preview.9` with run `31766681211`, and keep production
  on platform `v0.1.0-preview.85` / Helm revision `85` because the repair is
  CLI-only.
- [x] Run a retained real TUI first-turn canary through the ordinary
  post-commit hook. Its 11 increments materialized as two direct pools solely
  because the turn crossed a 15-minute boundary; the pools conserve exactly
  `348,018` Token with zero gaps and zero Request claims.
- [x] Replace unconditional per-turn cumulative reset with an exact first-row
  choice between the prior session baseline and zero, keep both modes strict
  thereafter, invalidate older completed scan progress once, pass the full CLI
  suite and two-axis review, and replay the retained fixture as one uploadable
  group with 8 responses and exactly `284,666` Token.
- [x] Merge PR #294 as `72746a1f`, verify merge-SHA main CI `31772012834`, and
  release CLI-only `ae-cli/v0.2.0-preview.10` through run `31772332839` without
  a platform release or Helm rollout. Install the released binary, run its
  post-install and managed-hook refresh paths, and wake the retained
  same-session commit through the ordinary managed pre-push hook without
  manual sync or a remote canary branch. The backend acknowledged one
  `codex_local` group, and exact production readback proved one hot group, one
  direct relation to one formal pool, 8 responses, exactly `284,666` Token,
  zero coverage gaps, and zero Request claims.

### T18 — Machine-wide attribution coordinator and incremental drain ([#301](https://github.com/LichKing-2234/ai-efficiency/issues/301))

- [x] Serialize matching Repository/worktree tasks behind one transient
  machine-wide owner while retaining per-workspace durable task identity.
- [x] Treat an exhausted five-minute workspace quantum as a resumable yield and
  immediately continue it without another commit, push, or manual sync.
- [x] Persist no-candidate source progress in bounded batches and deliver each
  claim-producing batch before unrelated source units finish.
- [x] Preserve response-loss replay, item-level ACK, terminal-conflict
  quarantine, process-owner recovery, and deterministic at-least-once state.
- [x] Show current-Repository task detail separately from machine-wide
  queued/running/yielded/failed totals without Request or response identifiers.
- [x] Drain a synthetic 1,800-source × 83-trigger fixture across six 300-source
  budgets with strictly increasing progress and final cleanup.
- [x] Run the full ae-cli suite and final two-axis review, address every finding,
  and commit the clean implementation branch.

### T19 — Compaction-safe WebSocket Token scanning ([#302](https://github.com/LichKing-2234/ai-efficiency/issues/302))

- [x] Accept one explicit same-turn top-level `compacted` boundary followed by
  unchanged cumulative components and a different valid last-response snapshot
  as a zero-Token, zero-request baseline restatement.
- [x] Preserve strict fail-closed behavior for the same contradiction without
  the exact compaction boundary and for invalid/decreasing/overflowing/mixed
  usage.
- [x] Accept later monotonic growth exactly once and prove the compacted turn
  still reaches one uploadable deterministic commit allocation.
- [x] Increment the scan-progress semantics version so older classifications
  rebuild and rescan once.
- [x] Run the full ae-cli suite and final two-axis review, address every finding,
  and commit the clean implementation branch.

### T20 — Deleted-worktree recovery ([#303](https://github.com/LichKing-2234/ai-efficiency/issues/303))

- [x] Freeze the Relay provider on each newly captured v2 commit trigger while
  preserving its original Repository, workspace, checkpoint, and commit.
- [x] Allow only the same reporting owner and exact canonical Repository to
  lend another checkout whose `HEAD` or refs reach every retained commit.
- [x] Keep unavailable commits and mismatched Repository/provider identities
  retained with safe local-state diagnostics and no heuristic rebinding.
- [x] Prove a real temporary worktree can be removed before pre-push recovery
  delivers exactly one allocation with every original identity preserved.
- [x] Run the full ae-cli suite and final two-axis review, address every finding,
  and commit the clean implementation branch.

### T21-T22 — Migration and production qualification ([#304](https://github.com/LichKing-2234/ai-efficiency/issues/304), [#305](https://github.com/LichKing-2234/ai-efficiency/issues/305))

- [x] Promote legacy single-trigger tasks and freeze missing providers only for
  the same persisted reporting installation, without changing existing trigger
  identities or future-version state.
- [x] Rebuild stale scanner progress, deduplicate exact trigger/progress units,
  and defer busy or conflicting workspaces without blocking unrelated tasks.
- [x] Expose current-repository state separately from machine-wide queued,
  running, yielded, recoverable, terminal, and expiring counts.
- [x] Keep terminal conflicts quarantined and preserve the local-only 90-day
  cleanup boundary without changing accepted formal pools or server data.
- [x] Cover 300 legacy workspaces with deleted roots, lock contention, and a
  conflicting task, plus existing timeout-resume and real deleted-worktree
  drain integration.
- [x] Run the full ae-cli suite and final two-axis review, address every finding,
  and commit the clean implementation branch.
- [x] Reproduce the current runtime's exact inline `apply_patch` wrapper in a
  request-correlated regression fixture, accept only the single complete
  generated form with a matching result identifier, retain ambiguous-input
  rejection, and invalidate stale scanner progress so an affected failed-
  closed source is rescanned once.
- [x] Run the full ae-cli suite and final code review for the wrapper repair,
  then address every finding before publication.
- [x] Merge the repair through PR #325 as `c757fbd5` from implementation
  commit `37bce887`; verify PR-head CI run `32359847423` and merge-SHA main CI
  run `32362180500` pass backend, frontend, ae-cli, and deploy-static.
- [x] Publish only `ae-cli/v0.2.0-preview.13` from `f54184a6` through release
  run `32364192912`, install it through the approved proxy, and verify the exact
  binary version, managed-hook template 3, `formal_v2 + upgrade_required`
  protocol, and Helm eligibility as `repo_config_id=72`. No platform tag or
  Helm rollout was created for this CLI-only release.
- [x] Establish the pre-canary baseline at 23 formal pools, 23 direct
  relations, `74,564,942` Token, 536 Requests, zero gaps, no repo 72 relation,
  zero duplicate pool/relation/provider-Request identity, and v1 tables `0/0`.
  Managed post-commit/pre-push then captured the single authorized Helm commit
  `c35758f5` and cleared its current-Repository task without manual claim
  fabrication. A later manual recovery process exited on an unrelated deleted
  AI Efficiency worktree, after the Helm task and claim were already complete;
  it is not canary failure evidence.
- [x] Read back the exact canary as one acknowledged `relay_official` group
  with 112/112 Requests reconciled and materialized into four direct pools.
  Request rows and pools both conserve input `200,342`, output `31,401`, cache
  creation `0`, cache read `21,436,416`, total `21,668,159`, and Request count
  112, with zero gaps. The repo 72 Activity row returned the same `21,668,159`
  direct Token with complete claim coverage, exact ratio, and active readiness.
  One later managed pre-push wake left the local ACK count, group, four pools,
  four relations, provider-scoped Request identities, and every Token component
  unchanged; duplicate counts and v1 tables remained zero. Concurrent pools
  from other Repositories were excluded from this exact-commit conservation
  proof. Repo 72 remains intentionally eligible but SCM-unbound, and the final
  #252 SCM gate remains incomplete rather than being changed by this canary.

## Cross-Ticket Invariants

| Invariant | Owner | Final gate |
| --- | --- | --- |
| Only deterministic committed evidence uploads | T02, T05 | T11 |
| Token authority is source-explicit: HTTP from `sub2api`, WebSocket from normalized Codex JSONL | T04, T07, T17 | T11/T17 |
| HTTP Relay owner matches the installation; WebSocket local Token is owned by the authenticated installation and is not billing truth | T02-T04, T17 | T11/T17 |
| Request replay, replica count, and shared projection never duplicate Token | T03-T07 | T11 |
| Partial/expiry never appears complete | T06, T08, T09 | T11 |
| Frontend never derives scope totals from rows | T08, T09 | T11 |
| Local-day trend is based on usage time | T07-T09 | T11 |
| Product UI contains no Request ID or operational Request counts | T08, T09, T13 | T11 |
| Usage, Activity, and Repository administration remain distinct | T08, T09 | T11 |
| Shadow/pre-cutover data never becomes formal | T11, T12 | T12 |
| v1 cannot write after reset | T12, T13 | T12/T13 |
