# Codex Commit Token Attribution v2 Implementation Plan

**Date:** 2026-08-11
**Status:** T01-T12 and the reviewed T14/T15 implementation are complete. #251 finished the separate CLI/platform releases, staging rehearsal, production formal-v2 cutover, v1 POC export/reset, and final readbacks on 2026-08-12. T14/T15 passed the final backend and ae-cli suites plus Standards and Spec review on 2026-08-13; they remain unreleased and undeployed. T13 remains in its seven-day stable window, but the ordinary-workflow adoption gate cannot be satisfied until T16 qualifies sustained production reporting. #252 cleanup must not run before `2026-08-19T12:59:09Z` and remains conditional on every acceptance gate being green.
**T13 Day 0:** The read-only production baseline has one formal pool, pending `3`, near-expiry `0`, active readiness, complete claim coverage, and an exact ratio. The additional ordinary-workflow pool and final SCM freshness gates remain unsatisfied; no cleanup has run.
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

- [ ] Observe seven continuous stable days from `2026-08-12T12:59:09Z`; do not
  clean before `2026-08-19T12:59:09Z`, and restart the clock after any failed
  gate.
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

- [ ] Obtain explicit release, deployment, recovery, and production-data
  mutation authority in the execution turn.
- [ ] Release/deploy the reviewed T14/T15 changes, recover only deterministic
  post-cutover claims, and verify ordinary developer pool deltas without
  duplicate Token.
- [ ] Qualify repeated hooks across repositories and complete the T13 adoption
  gate readbacks before restarting the seven-day stability clock.

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
