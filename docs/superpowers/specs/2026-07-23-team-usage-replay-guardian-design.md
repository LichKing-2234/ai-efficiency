# Team Usage Replay Guardian Design

**Status:** Approved for one additional staging replay.

**Date:** 2026-07-23

**Refines:**

- `docs/superpowers/specs/2026-07-23-team-usage-startup-cohort-publication-design.md`

The startup cohort implementation, Redis contracts, Task 4 disabled benchmark,
and Task 5 failure ledger remain authoritative. This design replaces only the
operational control and evidence-retention procedure for one user-authorized
replacement replay. It does not change application code, Redis data, Team Usage
API behavior, production, Sub2API, or `docs/architecture.md`.

## Failure Being Corrected

Task 5 enabled exact code at staging revision 59 with two desired, ready,
updated, and available replicas. The orchestration process then exited without
retaining its observer result or per-Pod counter baselines. Its internal cleanup
ran, but its conditional rollback branch did not create a Helm revision. A
manual atomic restore produced healthy, feature-disabled revision 60.

The retained evidence cannot determine the missing in-memory guard value or any
product diagnostic result. Every owner, loser, scheduler, manifest, source,
Relay, Redis, and pool gate remains unproven rather than passed or failed. The
replacement replay must correct evidence ownership and rollback independence,
not weaken a product gate.

## Decision

Run one additional two-Pod staging replay under two independent session-level
processes:

1. an external rollback guardian owns restoration to the exact disabled
   baseline; and
2. a durable observer owns the sanitized replay result.

The enable controller owns neither guarantee. Its exit, signal, or cleanup
cannot suppress rollback or delete the observer summary.

## External Rollback Guardian

Before changing the selector, capture the deployed disabled Helm revision and
copy the exact disabled selector into a private mode-`0600` guardian directory.
Start the guardian as an independent process group and wait until it writes an
atomic `armed` state. The guardian receives only these bounded inputs:

- kube context `luxuhui-agora-hci-losangeles3s`;
- namespace `la3-ai-efficiency-prod`;
- release `ai-efficiency-staging`;
- the captured disabled revision;
- exact image tag;
- private disabled-selector path;
- heartbeat, explicit-restore, and disarm paths; and
- a ten-minute absolute deadline.

A separate heartbeat writer updates every two seconds. The guardian requests
restoration when the heartbeat is stale for 15 seconds, an explicit restore
file appears, or the absolute deadline expires. It first restores the private
disabled selector bytes locally. It then reads the live Deployment. If the
exact image is already one replica with no Team Usage prewarm environment, it
skips Helm. Otherwise it runs one bounded `helm rollback` to the captured
disabled revision with `--wait`, `--cleanup-on-fail`, and a ten-minute timeout.
It writes only atomic closed states: `armed`, `restore_started`,
`restore_succeeded`, or `restore_failed`.

The controller may write `disarm` only after a fresh read proves the exact
image, one ready replica, zero restarts, no prewarm environment, and HTTP 200
live/readiness. A missing or failed guardian state blocks enablement or final
success.

## Pre-Enable Failure Drill

Before any live selector change, run the exact guardian script against fake
`helm` and `kubectl` executables in a private sandbox. Stop the fake controller
heartbeat without writing restore or disarm. Require the guardian to:

- detect the stale heartbeat;
- invoke exactly one fake rollback for the captured revision;
- restore the fake disabled selector bytes;
- write `restore_succeeded`; and
- exit without deleting its state.

Then run real chart render and server dry-run for both the disabled selector and
the proposed enabled selector. This drill creates no Kubernetes revision and
does not count as the authorized live replay.

## Durable Observer

Start the observer before the enabled Helm upgrade. Its private raw workspace
and its evidence directory are siblings, not parent/child, and cleanup patterns
must name the raw workspace exactly. The evidence directory is never removed by
controller or guardian cleanup.

For each fresh Pod, the observer captures the first available metrics scrape
and atomically writes a counter baseline. It then polls bounded metrics, source
slot occupancy, and schema-v2 completeness. Redis manifest keys and decoded
values may exist only in process memory; they are never written or printed.

At the first observation with four complete manifests and all references, the
observer atomically renames a sanitized summary containing only:

- anonymous Pod roles;
- relative durations;
- closed counter deltas and outcomes;
- manifest and reference counts;
- first-to-last publication spread;
- maximum source-slot occupancy;
- Relay and Redis error deltas;
- bounded Redis pool pending observations and wait/timeout deltas; and
- Helm revision and exact image digest.

On timeout or internal error, it atomically writes a sanitized failure summary
with the phase and closed failure class. The controller must fsync and copy the
summary into the ignored SDD report before requesting cleanup. Absence of a
summary is a replay failure, but cannot prevent guardian restoration.

## Live Replay Sequence

1. Verify application HEAD and worktree, exact image, healthy disabled staging
   revision 60, healthy unchanged production revision 69, selector mode
   `0600`, and zero task artifacts.
2. Run the fake-command guardian failure drill.
3. Render and server-dry-run exact disabled and enabled selectors.
4. Arm guardian and heartbeat, then start the observer.
5. Atomically enable the exact image with two replicas and the four approved
   timezones only in staging.
6. Let the observer write exactly one pass/fail summary at the first complete
   cohort or its bounded deadline.
7. Write the guardian explicit-restore request regardless of observer result.
8. Verify the guardian result and fresh disabled runtime before disarming.
9. Delete watcher, guardian, raw metrics, binaries, private selector copy, and
   state only after the sanitized summary is retained and final runtime is
   verified.

No account is used. No Team Usage API request is made. No business, response,
origin, manifest, value, or lease key is deleted.

## Diagnostic Gates

At the first complete cohort, require all existing Task 15 gates without
relaxation:

```text
owner startup success labels = 4
loser startup lease_busy labels = 4
loser startup source observations = 0
scheduler_tick_total on owner = 0
scheduler_tick_total on loser = 0
complete manifests = 4
references per manifest = 4
first-to-last manifest spread <= 5s
maximum deployment-wide source concurrency <= 2
Relay errors = 0
Redis operation errors = 0
Redis error-class deltas = 0
```

Record bounded pool pending observations and wait/timeout deltas without
claiming that pressure outside the sampled intervals is impossible. Missing
proof or any nonzero hard-gate error is failure.

## Final-State And Decision Rules

Every outcome ends with the exact image feature-disabled at one ready staging
replica, zero restarts, no prewarm environment, and HTTP 200 live/readiness.
Production revision 69 remains unchanged. The staging selector remains mode
`0600`, exact-image disabled, uncommitted, and unpushed. All session artifacts
must be absent after sanitized evidence is retained.

A pass permits a fresh Task 9 Step 3 attempt only. It does not count as an API
acceptance round. A product-gate failure records the closed blocker. A guardian,
observer, or evidence-retention failure records an operational blocker. No
third replay is authorized by this design.

## Alternatives Rejected

### Kubernetes Guardian Job

A Job would add chart and runtime resources for a one-time operational control,
expanding the application deployment boundary. The external guardian needs no
repository runtime change and can be deleted after verification.

### Controller-Local Trap

Task 5 proved that cleanup can run while a conditional rollback branch is
bypassed. Keeping rollback in the same process repeats the failed ownership
model.

### Manual Rollback

Manual restoration recovered revision 60 but leaves an avoidable enabled
interval when the controller fails. It remains an emergency fallback, not the
primary safety mechanism.
