# Codex Commit Token Attribution v2 Implementation Plan

**Date:** 2026-08-11
**Status:** T01-T12, T14/T15, and T17 are implemented, released, and production-qualified. T16 has now observed two genuine Agent Infra commits through ordinary `post-commit` hooks: `6c8bd9c8` produced 5 reconciled Requests and `973,350` Token, while `1ed61c8f` produced 24 reconciled Requests and `2,337,635` Token. Both are formal pools visible in Activity, so the ordinary-commit and new-pool evidence is satisfied. The installed preview.10 runner still cannot close its ten retained triggers: an exact rescan wrapped the evidence digest of an already acknowledged single allocation, and terminal conflicts then poisoned later unrelated work. A minimal CLI repair is in progress; release, ordinary managed-hook replay, final status/doctor/conservation/SCM readback, and Day 0 recording remain incomplete. #252 cleanup remains blocked and no cleanup has run.
**T13 candidate baseline (Day 0 not established):** Production Activity for 2026-08-13 through 2026-08-14 reads `4,112,631` committed Token, including `3,479,947` direct Token for Agent Infra, with complete claim coverage, an exact Usage ratio, and zero failed, partial, unsynced, or stale SCM coverage. This does not start Day 0 yet: the retained-trigger runner repair must first be released and exercised by an ordinary managed hook, after which status/doctor, formal pool/relation/Token conservation, v1/shadow isolation, and fresh SCM coverage must be read again.
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
- [ ] Observe at least two subsequent genuine ordinary commits from any eligible
  Repository through hooks without manual sync. They may come from the same or
  different Repositories; at least one workflow must produce a new ordinary
  formal pool visible in Activity. Automatic Repository registration or reuse,
  status/doctor, and the formal pool/relation/Token delta must all read back
  correctly and conserve exactly. The commit/pool portion is satisfied by
  Agent Infra commits `6c8bd9c8` (5 Requests, `973,350` Token) and `1ed61c8f`
  (24 Requests, `2,337,635` Token), both captured as ordinary `post-commit`
  work and visible in formal Activity. Final status/doctor and exact
  conservation remain open because ten retained triggers are blocked behind
  three local conflicts.
- [ ] Release the single-allocation evidence-digest and terminal-conflict
  quarantine repair, then use an ordinary managed hook rather than
  `ae-cli sync` to drain the ten retained triggers. Preserve terminal conflict
  audit state while proving that it is not retransmitted and does not block a
  later trigger; `upgrade_required` and unknown acknowledgements must remain
  fail-closed.
- [ ] Complete the T13 adoption and final SCM-freshness readbacks, then restart
  the seven-day stability clock from the first qualifying ordinary pool and
  aggregate Activity readback.

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
