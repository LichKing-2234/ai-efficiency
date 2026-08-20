# Attribution v1 and AE OTel Cleanup Preflight

**Date:** 2026-08-13
**Status:** Preflight implementation, verification, and review merged through [PR #284](https://github.com/LichKing-2234/ai-efficiency/pull/284) as `f6c9de84`; hosted CI `31685184654` passed backend, ae-cli, frontend, and deploy/static validation. Local PowerShell runtime testing was unavailable because `pwsh` is not installed, while the hosted PowerShell installer validation passed. #278 completed its earlier ordinary-workflow qualification. PR #299's deployed denominator repair established replacement Day 0 at `2026-08-17T05:32:57.925948Z`; ordinary AI Efficiency PR #319 and the complete 2026-08-20 evidence snapshot now satisfy the non-destructive gate. Production cleanup remains blocked on an immediate final re-read plus separate implementation/release/deployment/data-mutation authority; no fixed elapsed-time wait applies.
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
- the final live/ready, structured v1 rejection, reconciliation/error,
  near-expiry, pending-boundary, claim coverage, exact Usage ratio,
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
- Any conservation mismatch rolls back the DDL transaction and restarts the
  stability clock after diagnosis.
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
- [ ] Obtain authority for any later release, deployment, or cleanup.
