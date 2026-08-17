# Attribution v1 and AE OTel Cleanup Preflight

**Date:** 2026-08-13
**Status:** Preflight implementation, verification, and review merged through [PR #284](https://github.com/LichKing-2234/ai-efficiency/pull/284) as `f6c9de84`; hosted CI `31685184654` passed backend, ae-cli, frontend, and deploy/static validation. Local PowerShell runtime testing was unavailable because `pwsh` is not installed, while the hosted PowerShell installer validation passed. #278 completed its ordinary-workflow qualification. The cumulative personal Usage denominator invalidated the provisional 2026-08-14 clock; PR #299's repair is now deployed as platform `v0.1.0-preview.87`, and exact production readback established replacement Day 0 at `2026-08-17T05:32:57.925948Z`. Production cleanup remains blocked on seven continuous stable days, a later qualifying ordinary pool, every final execution-time gate, and separate implementation/release/deployment/data-mutation authority.
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

- #278 has observed at least two genuine subsequent ordinary commits from any
  eligible Repository through hooks without manual sync;
- at least one of those workflows has produced a new ordinary
  `formal_v2` direct/shared pool and aggregate Activity has confirmed it;
- seven continuous days have elapsed from that qualifying pool without a
  failed stability gate;
- the final health, claim coverage, exact Usage ratio, SCM freshness,
  conservation, and zero-v1-table readbacks are green;
- the operator has separately authorized implementation, releases,
  deployment, and destructive migration.

The earlier cutover-based date ending on 2026-08-19 and the provisional date
ending on 2026-08-21 are both superseded. The replacement Day 0 is
`2026-08-17T05:32:57.925948Z`; `2026-08-24T05:32:57.925948Z` is only the
earliest possible cleanup time. Any failed continuous gate restarts that clock,
and final adoption, SCM freshness, conservation, zero-v1, and authority gates
still apply at execution time.

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
