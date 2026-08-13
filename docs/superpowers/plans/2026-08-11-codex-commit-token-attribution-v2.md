# Codex Commit Token Attribution v2 Implementation Plan

**Date:** 2026-08-11
**Status:** T01-T12 and T14/T15 are implemented and released. `ae-cli/v0.2.0-preview.6` and platform `v0.1.0-preview.84` shipped T14/T15, and production runs Helm revision `84`; the CLI-only wrapper-proof follow-up shipped as `ae-cli/v0.2.0-preview.7`. T16 verified automatic Repository registration, bounded/resumable fail-closed recovery, and a three-commit qualification branch. The first two commits proved event-driven capture, coalescing, timeout persistence, and pre-push recovery; a third commit from a freshly started HTTP-only Codex TUI produced the first new agent-infra formal pool without manual sync. Agent Infra is now bound to the GitHub provider with a registered webhook, and focused Agent Infra/AI Efficiency PR syncs restored current Activity SCM coverage to complete with zero failed, partial, unsynced, or stale Repository. These qualification and recovery actions do not satisfy the two-subsequent-ordinary-commit or T13 adoption gates. The seven-day stability clock must restart from the first qualifying ordinary pool and aggregate Activity readback; #252 cleanup remains blocked and no cleanup has run.
**T13 Day 0:** The production baseline had one formal pool, one direct relation, `4,395` formal Token, and one formal Request. The excluded qualification canary increased the current readback to two formal pools, two direct relations, `173,357` formal Token, and seven formal Requests; its exact delta is one pool, one relation, `168,962` Token, and six Requests. Shadow remains isolated at `19,607` Token and v1 bucket/revision tables remain zero. The additional ordinary-workflow pool and final-at-execution SCM freshness gates remain unsatisfied; the current SCM recovery readback is green but expires under the 24-hour freshness contract.
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

### T13 — Stable-window legacy cleanup ([#252](https://github.com/LichKing-2234/ai-efficiency/issues/252))

- [ ] Observe seven continuous stable days from the first qualifying ordinary
  pool confirmed by aggregate Activity readback. The earlier cutover-based
  clock ending on `2026-08-19T12:59:09Z` is superseded; restart the new clock
  after any failed gate.
- [ ] From the Day 0 baseline of one formal pool, read back at least one
  additional non-canary ordinary-workflow `formal_v2` direct/shared pool.
- [ ] Keep live/ready, structured v1 `upgrade_required`, reconciliation/error,
  near-expiry, pending-boundary, Activity/readiness/ratio, final SCM freshness,
  formal pool/relation conservation, and zero-v1-table gates green.
- [ ] Remove v1 ingest/read/frontend Bucket surfaces, AE OTLP route/config, and
  legacy-only credentials/fields without touching user OTel.
- [ ] Verify schema/data safety, upgrade behavior, backend/CLI/frontend tests,
  separate platform/CLI releases, and production health.
- [x] Verify #232-#234 were completed by merged PR #235 and closed before the
  cleanup window; do not defer their closure to T13 execution.

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
- [ ] Complete all preflight suites and code review, then merge without release,
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
- [ ] Observe at least two subsequent genuine ordinary commits through hooks
  without manual sync, including a new agent-infra formal pool visible in
  Activity. Status/doctor must remain clear, and the formal pool/relation/Token
  delta must conserve exactly.
- [ ] Complete the T13 adoption and final SCM-freshness readbacks, then restart
  the seven-day stability clock from the first qualifying ordinary pool and
  aggregate Activity readback.

## Cross-Ticket Invariants

| Invariant | Owner | Final gate |
| --- | --- | --- |
| Only deterministic committed evidence uploads | T02, T05 | T11 |
| Official Token comes only from `sub2api` | T04, T07 | T11 |
| Provider and relay owner never cross-account | T02-T04 | T11 |
| Request replay, replica count, and shared projection never duplicate Token | T03-T07 | T11 |
| Partial/expiry never appears complete | T06, T08, T09 | T11 |
| Frontend never derives scope totals from rows | T08, T09 | T11 |
| Local-day trend is based on usage time | T07-T09 | T11 |
| Product UI contains no Request ID or operational Request counts | T08, T09, T13 | T11 |
| Usage, Activity, and Repository administration remain distinct | T08, T09 | T11 |
| Shadow/pre-cutover data never becomes formal | T11, T12 | T12 |
| v1 cannot write after reset | T12, T13 | T12/T13 |
