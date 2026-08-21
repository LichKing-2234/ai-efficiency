# Attribution v1 and AE OTel Cleanup Preflight

**Date:** 2026-08-13
**Status:** Complete. Preflight merged through [PR #284](https://github.com/LichKing-2234/ai-efficiency/pull/284), the evidence gate was satisfied by ordinary AI Efficiency PR #319 plus the 2026-08-20 snapshot, and the operator authorized the staged cleanup. Phase 2 merged through [PR #330](https://github.com/LichKing-2234/ai-efficiency/pull/330) as `319735ac`, shipped in CLI `ae-cli/v0.2.0-preview.14` and platform `v0.1.0-preview.89`, and completed at Helm revision 89. Phase 3 source contraction merged through [PR #332](https://github.com/LichKing-2234/ai-efficiency/pull/332) as `996c23ea`; hosted CI `32442883145` passed and release workflow `32443741864` published platform `v0.1.0-preview.90`. The exact legacy schema was exported, the final volatile gate passed, the old application roles were drained before transactional no-`CASCADE` DDL, and preview.90 was deployed as Helm revision 90 with chart `0.1.76`. Post-deploy schema, formal conservation, health, Activity, SCM, and removed-route readbacks all passed. No fixed elapsed-time wait applied.
**Parent:** [Codex Commit Token Attribution v2 Implementation Plan](./2026-08-11-codex-commit-token-attribution-v2.md)
**Issue:** [#252](https://github.com/LichKing-2234/ai-efficiency/issues/252)

## Authority Boundary

This preflight may add protection tests, harden the already-supported local
OTel scrubber, inventory removal surfaces, and rehearse DDL in a rolled-back
test transaction. It does not authorize any of the following:

- deleting legacy application code or production rows;
- changing production Redis data;
- creating a platform or CLI release;
- deploying or running production DDL;
- treating qualification canaries as ordinary adoption.

## Entry Gates

Cleanup cannot start until all of these are true in the same execution window:

- at least one independently classified ordinary developer workflow after the
  replacement baseline has produced a new `formal_v2` direct/shared pool
  visible in aggregate Activity;
- the final SCM coverage read has no failed, partial, unsynced, or stale
  Repository;
- the final live/ready, structured v1 rejection before Phase 2 or route absence
  thereafter, reconciliation/error, near-expiry, pending-boundary, claim
  coverage, exact Usage ratio,
  conservation, duplicate/gap, and zero-v1-table readbacks are green;
- the operator has separately authorized implementation, releases,
  deployment, and destructive migration.

The earlier cutover-based date ending on 2026-08-19 and the provisional date
ending on 2026-08-21 are both superseded. The replacement Day 0 at
`2026-08-17T05:32:57.925948Z` is retained only as a historical and conservation
baseline. It is not a waiting clock and imposes no earliest cleanup time. Any
failed gate blocks cleanup until it is corrected and read back again; it does
not start or restart a fixed elapsed-time clock.

## Evidence Snapshot — 2026-08-20

Ordinary AI Efficiency PR #319 was created and merged after replacement Day 0.
Commit `429c6f00e25b21425b0d87fb9e34bdd960e661a3` produced five direct formal
pools and `23,210,615` conserved Token. Focused SCM jobs 33/34/35 completed with
zero failed usage refreshes, leaving claim and SCM coverage complete, readiness
active, and the selected-window Activity ratio exact and fresh.

The `2026-08-20T13:31:51.123751Z` repeatable-read snapshot conserved 37 formal
pools, 37 direct relations across 11 commits, 946 Requests, and `142,330,988`
Token with zero coverage gaps, duplicates, conservation errors, terminal
Request errors, near-boundary pending Requests, finalization errors, or expired
unfinalized groups. All 41 pending Requests remained before the final-attempt
boundary; v1 stayed `0/0`, the shadow pool remained isolated, structured v1
rejection returned `409 upgrade_required`, and live/ready dependencies were up.
Because Activity, SCM freshness, lifecycle, and pending state are volatile, the
authorized cleanup turn must repeat them immediately before any mutation.

## Phase 2 Production Closeout — 2026-08-21

The final same-window gate conserved 46 formal pools and 46 direct relations
across 11 commits, `189,117,509` Token, and 1,272 formal Request/response
observations. It found zero claim or SCM coverage gaps, duplicate accounting,
terminal claims, near-expiry groups, lifecycle errors, or v1 rows. All four
pending claims remained before their final-attempt boundary. The two
`codex_local` groups and 1,253 reconciled Relay Requests conserved exactly to
the same pools.

CLI release workflow `32389268066` published `ae-cli/v0.2.0-preview.14` from
`319735ac` as five OS/architecture archives plus `checksums.txt` without
replacing the platform-owned repository latest release. Platform workflow `32389679261`
published `v0.1.0-preview.89` from the same commit, including linux/amd64 and
linux/arm64 GHCR manifests. The Helm repository recorded chart `0.1.75` and
application version `v0.1.0-preview.89` in commit `17d695a`.

Production rollout `ai-efficiency-prod` in `la3-ai-efficiency-prod` completed
as Helm revision 89. The backend and prewarmer were Ready with zero restarts
and the same preview.89 image digest, proving that no older application binary
remained in either role. Live/readiness reported PostgreSQL, Redis, and Relay
healthy. The removed v1 batch/revision, AE OTLP, and legacy Activity summary
routes returned 404. Authenticated attribution readiness remained `active`;
Activity v2 retained an exact Usage ratio with complete claim and SCM coverage,
Repository and PR reads returned three and five rows respectively, formal
conservation was unchanged, and the v1 tables remained `0/0`.

## Phase 3 Production Closeout — 2026-08-21

PR #332 removed the legacy Ent schemas and installation columns, strengthened
the idempotent no-`CASCADE` conservation test, and regenerated Ent output.
Hosted CI `32442883145` passed all four jobs. Release workflow `32443741864`
published platform `v0.1.0-preview.90` from `996c23ea` with linux/amd64 and
linux/arm64 support; this schema-only application change required no CLI
release. Helm commit `a43b0ee` recorded chart `0.1.76` and preview.90.

The protected operator evidence bundle contains the exact schema-only exports,
formal before digest, executable DDL contract, final gate, post-deploy schema
and digest readback, and runtime/API summary. Its `SHA256SUMS` manifest hashes
to `872f0d51b501694d070a78a9e7d0d96bca368531ebc58a33bc4ce2d26cb55098`;
the directory was mode `0700`, every file was mode `0600`, and the temporary
credential-bearing PostgreSQL helper pod was deleted after verification.

The final `2026-08-21T03:53:54.469201Z` gate conserved 48 formal pools, 48
direct relations, 1,313 Requests, and `192,289,908` Token with identical pool
and relation digests. Five pending claims all remained before their
final-attempt boundary. Terminal claims, reconciled claims without pools,
near-expiry and hard-expired groups, finalization errors, duplicate identities,
component mismatches, coverage gaps, and v1 rows were all zero. Activity was
`active` with an exact Usage ratio and complete claim and SCM coverage;
Repository and PR reads returned three and five rows.

Both preview.89 application deployments were then scaled to zero and their
pods were proven absent before DDL. The transaction committed at
`2026-08-21T03:55:47.242887Z` after dropping only the two legacy tables and two
installation columns and comparing the same formal snapshot inside the
transaction. Production rollout `ai-efficiency-prod` completed as Helm
revision 90. The Ready backend and prewarmer run preview.90 from `996c23ea`
with zero restarts and the same runtime image digest. Post-deploy readback
proved the legacy schema absent and the 48-pool/48-relation digests unchanged;
live/ready dependencies were up, attribution remained `active`, Activity ratio
and both coverage dimensions remained exact/complete, and all eleven removed
v1, AE OTLP, and legacy Activity routes returned 404.

## Protection Seams

### CLI upgrade and user-managed OTel

The public seams are both installers, the hidden `ae-cli update post-install`
compatibility command, and `toolconfig.DisableCodexOTLP`.

- Removal requires the exact AE endpoint, credential, JSON protocol, exporter
  shape, and header shape.
- A changed protocol, extra exporter option, extra header, different endpoint,
  or different credential is user-modified and must be preserved.
- After replacing the executable, each installer invokes the hidden command on
  the newly installed binary; cleanup therefore does not depend on the older
  updater process and remains effective when users skip intermediate releases.
- The local legacy OTLP plaintext and enable flag are removed from
  `reporting.json` only after exact managed-exporter removal or when no
  `otlp-http` exporter exists.
- A mismatched exporter preserves both Codex configuration and local ownership
  evidence. The installer emits a warning but does not fail the completed
  binary installation.
- Missing reporting state is a no-op. A malformed local file or unsafe config
  mismatch is warning-only and never converts a completed binary update into
  a failed install.

The scrubber is a compatibility shim, not an active telemetry surface. It must
remain until pre-cleanup clients are outside the supported upgrade horizon;
removing it in the first cleanup CLI release would strand users who skip that
release.

### Formal schema conservation

The public seam is the PostgreSQL schema. The preflight test runs the planned
legacy DDL inside an isolated schema and transaction, snapshots formal pools
and relations before and after, and rolls the transaction back. The snapshot
covers:

- formal pool count;
- input, output, cache-creation, cache-read, and total Token;
- formal Request count;
- formal commit-relation count.

The destructive order is fixed because allocation revisions reference usage
buckets:

1. `attribution_allocation_revisions`;
2. `attribution_usage_buckets`;
3. `reporting_installations.otlp_token_hash` and
   `reporting_installations.otel_enabled`.

No `CASCADE` is permitted. An unexpected dependency must abort the migration.
The long-lived `attribution_usage_pools` and
`attribution_usage_pool_commits` tables are never migration targets.

### Release-unit isolation

The existing `test/release-workflow-contract-test.sh` remains the executable
release seam. It must prove that `ae-cli/v*` uses only the CLI GoReleaser
configuration, cannot write packages, and contains no GHCR, Dockerfile, or Helm
path. Platform `v*` and the one-time bridge keep their separate contracts.

## Removal Inventory

| Surface | Remove during #252 | Preserve |
| --- | --- | --- |
| ae-cli v1 upload | v1 bucket/revision DTOs and client methods; v1 delivery branches in compact sync and hook runner | v2 claim delivery, deterministic local proof, 90-day local recovery, non-Codex compatibility collector/spool |
| ae-cli AE OTel | active configure/inspect commands and obsolete status wording after the compatibility horizon | strict update-time scrubber until skip-version upgrades are safe; all user-managed OTel |
| Backend v1 writes | `/attribution/usage-buckets/*`, v1 ledger service/DTOs, revision validation, v1 report fields | installation reporter auth, protocol contract, v2 claim ACKs, Repository ensure/resolve, checkpoints |
| Backend AE OTLP | `/attribution/otel/v1/traces`, OTLP authentication/extraction, correlation writes/reads | v2 SQLite-backed exact HTTP Request identity; Redis itself and unrelated read caches |
| Backend Activity v1 | legacy summary/member/team/repository/bucket routes and bucket Request detail | `/activity/v2/overview`, `/v2/repositories`, `/v2/pull-requests`, shared authorization and SCM coverage used by v2 |
| Frontend Activity v1 | unused bucket inspector, v1 activity API/types/composable/tests and legacy i18n | current Activity v2 analytics, Repository/PR lists, Usage-backed ratio and readiness guide |
| PostgreSQL | the two reset v1 tables plus `aeo_*` hash/enable fields after application drain | every v2 hot claim, formal/shadow pool, commit relation, checkpoint, Repository and PR fact |
| Redis | stop creating/reading AE OTLP correlation entries; allow their bounded TTL to expire | no broad key deletion; all unrelated caches and queues |
| Historical queue | nothing | the global unresolved hook queue and non-Codex compatibility paths; #252 does not authorize replay or deletion |

Generated Ent code is regenerated only after source schemas change. Generated
files are not an independent removal target.

## Safe Delivery Sequence

### Phase 0 — preflight PR

- land the protection tests and update-time scrubber hardening;
- run backend, ae-cli, frontend, release-boundary, role E2E, and diff checks;
- merge without releasing, deploying, or mutating production.

### Phase 1 — CLI drain release

- after separate authorization, publish a CLI-only release containing the
  strict scrubber and local legacy-token removal;
- prove the CLI workflow published no platform image or Helm input;
- do not remove the scrubber in this release.

This release may precede the destructive cleanup because production already
has AE OTel disabled and v2 does not depend on it.

### Phase 2 — application contract release

- remove v1/OTLP backend routes, frontend v1 Activity surfaces, and active
  issuance/use of `aeo_*` credentials;
- keep the legacy PostgreSQL tables/columns for this deployment so rollback is
  non-destructive;
- deploy every backend replica and verify old binaries are no longer serving;
- read back v2 ingest, Activity, readiness, health, SCM coverage, and formal
  conservation. Removed v1/OTLP routes should be absent, not silently accepted.

### Phase 3 — schema contraction

- export a schema-only rollback artifact for the exact legacy tables/columns;
- snapshot the formal aggregate and relation digest without Request IDs;
- run explicit idempotent DDL only after every serving replica is on Phase 2;
- prohibit `CASCADE`, re-read the snapshot inside the same transaction, and
  commit only on exact equality;
- deploy the schema-contracted code and repeat production readbacks.

Phase 2 and Phase 3 may be collapsed only if the authorized deployment
procedure proves that no old and new backend binaries overlap. The default is
two phases because a rolling deployment can overlap replicas.

## Rollback Boundary

- Before Phase 3 commits, rollback is a normal application rollback because
  the legacy schema still exists.
- After Phase 3 commits, restore only the captured legacy schema before rolling
  back to a binary that references it. Never restore or replace v2 pool or
  commit-relation data.
- Any conservation mismatch rolls back the DDL transaction and blocks another
  attempt until the cause is corrected and every volatile gate is read again.
- Redis OTLP correlation entries require no restoration; they are bounded
  compatibility cache data, not accounting truth.

## Verification Ledger

- [x] Correct #240/#252/#278 to remove the obsolete clock and closed blockers.
- [x] Add red/green coverage for user-modified OTel preservation.
- [x] Add red/green coverage for update-time managed OTel cleanup and local
  legacy-token removal.
- [x] Rehearse legacy DDL with formal pool/relation conservation in a rolled-back
  PostgreSQL transaction.
- [x] Strengthen the existing release-unit contract test for normal CLI
  releases.
- [x] Run the complete backend suite.
- [x] Run the complete ae-cli suite.
- [x] Run the complete frontend suite and build measurement.
- [x] Run frontend role E2E. The first cold Vite pass completed 125/126 with
  one `/usage` mobile selector timeout; the unchanged warm rerun completed
  126/126.
- [x] Complete code review and close every accepted finding.
- [x] Open and merge the preflight PR.
- [x] Obtain authority for any later release, deployment, or cleanup.

## Cleanup Execution Ledger

### Phase 2 — application contract release

- [x] Remove backend v1 bucket/revision/report, legacy Activity reads, AE OTLP,
  and active `aeo_*` credential issuance/authentication while retaining the
  legacy tables and columns.
- [x] Remove ae-cli v1 bucket/revision delivery and baseline/status branches
  while retaining v2 claims, checkpoints, non-Codex collection, the strict
  update-time OTel scrubber, and local legacy credential fields.
- [x] Move Activity team navigation to Team Usage organization reads plus a
  formal-v2-only member availability read.
- [x] Run backend, ae-cli, frontend, release-contract, role E2E, build, and
  focused schema-safety verification. Backend and ae-cli full suites, frontend
  `777/777`, build measurement, release contract, and focused schema tests
  passed. Role E2E first found one cold `/usage` timeout plus five stale
  Activity team mocks; after replacing the v1 mocks with Team Usage and
  formal-v2 availability fixtures, the full rerun passed `126/126`.
- [x] Complete code review and close every accepted finding.
- [x] Open and merge the Phase 2 PR.
- [x] Repeat every volatile production gate in one window immediately before
  mutation. The final snapshot conserved 46 formal pools, 46 direct relations,
  11 commits, `189,117,509` Token, and 1,272 formal Request/response
  observations with every gate green.
- [x] Publish the separate CLI and platform release units, deploy every Phase 2
  backend replica, and prove no old binary remains serving. CLI preview.14 and
  platform preview.89 workflows passed; Helm revision 89 ran Ready backend and
  prewarmer roles from the same preview.89 digest with zero restarts.
- [x] Re-read v2 ingest, Activity, readiness, health, SCM coverage, formal
  conservation, and absence of the removed v1/OTLP routes. All removed routes
  returned 404, readiness remained `active`, v2 coverage and ratio were exact,
  formal conservation was unchanged, and v1 remained `0/0`.

### Phase 3 — schema contraction

- [x] Export the exact legacy schema-only rollback artifact and formal digest.
  The protected evidence bundle includes both schema exports, the original and
  final-before formal snapshots, executable SQL, post-deploy readbacks, and a
  verified SHA-256 manifest.
- [x] Prove every serving replica is on Phase 2 and repeat the volatile gates.
  Both preview.89 roles were Ready with zero restarts before the final gate;
  the same-window snapshot conserved 48 pools, 48 direct relations, 1,313
  Requests, and `192,289,908` Token with every lifecycle, integrity, coverage,
  SCM, Activity, and v1-row gate green.
- [x] Run explicit no-`CASCADE` DDL with in-transaction formal conservation.
  Both application deployments were drained to zero first. The transaction
  committed only after the before/after pool and relation digests matched.
- [x] Deploy schema-contracted code and repeat every production readback.
  Platform preview.90 runs as Helm revision 90/chart `0.1.76`; backend and
  prewarmer are Ready with zero restarts and the same digest, the legacy schema
  is absent, formal conservation is exact, and health, Activity, SCM,
  Repository/PR, readiness, and removed-route readbacks passed.
