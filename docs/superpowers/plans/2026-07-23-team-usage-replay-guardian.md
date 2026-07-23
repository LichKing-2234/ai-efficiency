# Team Usage Replay Guardian Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Safely obtain one durable two-Pod startup-cohort result while an independent guardian guarantees restoration to the exact disabled staging baseline.

**Architecture:** Build session-only Bash guardian and observer processes with separate raw and evidence directories. Prove the guardian against fake Helm/kubectl before any cluster mutation, then run the single authorized staging replay with the observer started before enablement and restoration owned by the guardian.

**Tech Stack:** Bash 3.2-compatible shell, jq, curl, Helm, kubectl, Redis CLI through the existing staging Pod, Prometheus text parsing, Git, Kubernetes.

**Status:** Task 1, Task 2 Steps 1-5, and Task 3 are complete. The single
authorized live replay ended in an operational failure, while final restoration
and cleanup passed. Task 2 Step 6 remains unchecked because review found
unresolved session-tooling gaps that cannot be validated by another replay. No
third replay is authorized.

## Global Constraints

- The approved design is `docs/superpowers/specs/2026-07-23-team-usage-replay-guardian-design.md`.
- Exactly one additional live two-Pod replay is authorized. The fake-command drill and server dry-runs do not consume it. No third replay is authorized.
- Use exact image `ghcr.io/lichking-2234/ai-efficiency:staging-c5d3f6af15ea4db7ef424139822b043ee787f79d`.
- Reverify, do not assume, disabled staging revision 60 and production revision 69 before enablement. Any drift blocks the replay.
- Use kube context `luxuhui-agora-hci-losangeles3s`, namespace `la3-ai-efficiency-prod`, and release `ai-efficiency-staging`.
- The exact four timezones are `UTC,Asia/Shanghai,America/Los_Angeles,Europe/Berlin`.
- Do not use an account, call a Team Usage API, read or print response bodies, or delete business, response, origin, manifest, immutable-value, or lease keys.
- Do not modify application code, rebuild the image, rerun the Task 4 Redis benchmark, modify Sub2API, mutate production, or update `docs/architecture.md`.
- Preserve and do not inspect unrelated Helm changes. The AI Efficiency staging selector remains mode `0600`, exact-image disabled, unstaged, uncommitted, and unpushed after every outcome.
- Guardian raw/state, observer raw, and durable evidence directories are distinct mode-`0700` directories. Cleanup names exact directories and never uses a shared wildcard before evidence retention.
- No credentials, selector contents, Redis keys/values, manifests, Relay payloads, response bodies, identities, users, emails, names, or user lists may enter stdout, metrics summaries, reports, plans, or specs.
- Every live outcome ends with the exact image feature-disabled at one ready staging replica, zero restarts, no prewarm environment, HTTP 200 live/readiness, production unchanged, and zero Task 16 local/Pod artifacts.
- A pass permits a fresh Task 9 Step 3 attempt only. Task 9 Steps 3-6 remain unchecked in this plan.

---

### Task 1: Build And Prove The Independent Guardian And Durable Observer

**Files:**
- Create temporarily: `/tmp/ae-task16-guardian.<nonce>/guardian.sh`
- Create temporarily: `/tmp/ae-task16-raw.<nonce>/observer.sh`
- Create temporarily: `/tmp/ae-task16-raw.<nonce>/controller.sh`
- Create temporarily: `/tmp/ae-task16-evidence.<nonce>/`
- Modify ignored: `.superpowers/sdd/task-16-1-report.md`
- Modify ignored: `.superpowers/sdd/progress.md`

**Interfaces:**
- Consumes: exact disabled selector, captured baseline revision, Task 15 observer metric contracts.
- Produces: reviewed guardian/controller/observer SHA-256 values, passing rollback drills, and a passing synthetic observer parser suite.

- [x] **Step 1: Create private, non-overlapping session directories**

Run from the application worktree:

```bash
umask 077
GUARD_DIR="$(mktemp -d /tmp/ae-task16-guardian.XXXXXX)"
RAW_DIR="$(mktemp -d /tmp/ae-task16-raw.XXXXXX)"
EVIDENCE_DIR="$(mktemp -d /tmp/ae-task16-evidence.XXXXXX)"
test "${GUARD_DIR%/*}" = /tmp
test "${RAW_DIR%/*}" = /tmp
test "${EVIDENCE_DIR%/*}" = /tmp
test "${GUARD_DIR}" != "${RAW_DIR}"
test "${GUARD_DIR}" != "${EVIDENCE_DIR}"
test "${RAW_DIR}" != "${EVIDENCE_DIR}"
```

Store the three absolute paths only in the ignored task report. Do not print directory contents.

- [x] **Step 2: Write the Bash guardian with explicit environment inputs**

`guardian.sh` must be Bash 3.2-compatible and start with:

```bash
#!/usr/bin/env bash
set -Eeuo pipefail

: "${KUBE_CONTEXT:?}"
: "${NAMESPACE:?}"
: "${RELEASE:?}"
: "${BASELINE_REVISION:?}"
: "${EXACT_IMAGE:?}"
: "${SELECTOR_PATH:?}"
: "${DISABLED_SELECTOR_COPY:?}"
: "${HEARTBEAT_PATH:?}"
: "${RESTORE_REQUEST_PATH:?}"
: "${STATE_PATH:?}"
: "${RESTORE_CLAIM_DIR:?}"
: "${ABSOLUTE_DEADLINE_EPOCH:?}"
: "${CONTROLLER_PID:?}"
: "${CONTROLLER_PGID:?}"
: "${ENABLE_IN_PROGRESS_PATH:?}"
: "${HELM_BIN:?}"
: "${KUBECTL_BIN:?}"
HEARTBEAT_STALE_SECONDS="${HEARTBEAT_STALE_SECONDS:-15}"
HELM_TIMEOUT="${HELM_TIMEOUT:-10m}"
CONTROLLER_KILL_WAIT_SECONDS="${CONTROLLER_KILL_WAIT_SECONDS:-15}"
```

Implement these focused functions without unrelated runtime behavior:

```text
write_state(state, reason)       atomic temp-file + rename, closed strings only
restore_selector()               install mode-0600 disabled copy at selector path
live_is_exact_disabled()         exact image, desired/ready/updated/available 1, zero prewarm env
claim_restore()                  ignore INT/TERM/HUP, then atomic mkdir
validate_controller_group()      prove the live controller PID owns the supplied PGID
drain_controller_group(reason)   quiesce and reap the complete controller PGID when required
restore(reason)                  selector restore, optional one Helm rollback, verify, state, exit
heartbeat_is_stale(now_epoch)    stat -f %m and 15-second comparison
main_loop()                      restore request, stale heartbeat, absolute deadline
```

`restore()` must call Helm at most once:

```bash
helm rollback "${RELEASE}" "${BASELINE_REVISION}" \
  --namespace "${NAMESPACE}" \
  --kube-context "${KUBE_CONTEXT}" \
  --wait --cleanup-on-fail --timeout "${HELM_TIMEOUT}"
```

It skips Helm only when `live_is_exact_disabled` already proves the final Deployment shape. `INT`, `TERM`, and `HUP` invoke `restore signal`; `EXIT` does not own rollback or cleanup. At the start of `restore()`, run `trap '' INT TERM HUP` before `claim_restore`; losing callers wait for the existing terminal state and never invoke Helm. Before any live read or rollback, every non-explicit restore drains the validated controller PGID. Explicit restore preserves the controller only when the atomic enable-in-progress marker is absent; when that marker exists, explicit restore also drains and waits for the complete controller group. The script never deletes its state, enable marker, or disabled-selector copy. Helm and kubectl must match the frozen live paths and hashes; fake proofs run only generated private copies whose differences are limited to reviewed path/hash substitutions.

Write `controller.sh` with `emergency_restore()` and
`quiesce_controller_group()` using the same selector copy, baseline revision,
exact Helm argv, validated live controller PID/PGID, and enable-in-progress
marker. It runs only when
guardian state is `restore_failed`, missing after
`GUARDIAN_TERMINAL_WAIT_SECONDS=660`, or the
guardian PID exited without `restore_succeeded`. The 660 seconds start at the
explicit restore-request file mtime, or an earlier `restore_started` state
mtime. On timeout, terminate the independent guardian process group, wait up to
15 seconds, send `KILL` only if still alive, and wait for exit. Then restore
selector bytes. Every emergency path must first terminate, wait, and if needed
`KILL` the complete live controller PGID before calling
`live_is_exact_disabled`; skip emergency Helm if it
passes, otherwise invoke the same rollback argv once. It must freshly prove
disabled runtime before returning and must never cleanup on failure.

- [x] **Step 3: Run the fake-controller-death guardian drill**

Create private fake `helm` and `kubectl` executables that operate only on files in `GUARD_DIR`. The fake runtime begins enabled. The fake Helm rollback appends one sanitized argument line, changes the fake runtime to disabled, and exits zero. The fake kubectl returns that state without keys or values.

Run the same guardian with `PATH` prefixed by the fake bin, `HEARTBEAT_STALE_SECONDS=3`, and a 30-second deadline. Start a heartbeat writer tied to a short-lived fake controller PID, then let that controller exit without restore.

Require the fake Helm to receive this complete ordered argv:

```bash
test "$(wc -l < "${GUARD_DIR}/fake-helm.calls" | tr -d ' ')" = 1
grep -Fx -- "rollback ai-efficiency-staging 60 --namespace la3-ai-efficiency-prod --kube-context luxuhui-agora-hci-losangeles3s --wait --cleanup-on-fail --timeout 10m" "${GUARD_DIR}/fake-helm.calls"
cmp -s "${GUARD_DIR}/disabled.json" "${GUARD_DIR}/selector.json"
jq -e '.state=="restore_succeeded" and .reason=="heartbeat_stale"' "${GUARD_DIR}/state.json"
```

The fake kubectl accepts only:

```text
--context luxuhui-agora-hci-losangeles3s --namespace la3-ai-efficiency-prod get deployment ai-efficiency-staging -o json
```

It rejects any other argv and returns this shape from the private fake runtime
state, with `enabled` changing to `disabled` only after fake Helm rollback:

```json
{
  "spec": {
    "replicas": 1,
    "template": {"spec": {"containers": [{"name": "ai-efficiency", "image": "exact-image", "env": []}]}}
  },
  "status": {"readyReplicas": 1, "updatedReplicas": 1, "availableReplicas": 1}
}
```

Run a second drill that creates explicit restore at
the same instant the heartbeat becomes stale and require exactly one rollback
and terminal state. Run a third drill that kills guardian before terminal state;
the fake controller emergency path must make exactly one rollback and preserve
all evidence. Run a fourth drill that kills guardian after fake Helm changes
runtime to disabled but before the terminal state write; controller must skip
emergency Helm and the combined fake Helm call count must remain one. The drill
fails if any process exits without bounded handling, invokes its rollback
twice, accepts different argv, or deletes state/evidence.

- [x] **Step 4: Write and test the durable observer**

Recover only the metric/Redis parsing behavior represented by Task 15 observer SHA-256 `d5915970ded740b1f4480ccd84455cdea22537b91d8e1a004ca214083e4e2f27`; do not recover its controller or cleanup trap. The new `observer.sh` owns these atomic files in `EVIDENCE_DIR`:

```text
pod-a-baseline.json
pod-b-baseline.json
summary.json
failure.json
```

Exactly one of `summary.json` or `failure.json` may exist. A successful summary uses this closed schema:

```json
{
  "status": "pass",
  "revision": 0,
  "image_digest": "sha256:hex",
  "baseline_count": 2,
  "complete_manifests": 4,
  "references_per_manifest": 4,
  "cohort_spread_ms": 0,
  "max_source_concurrency": 0,
  "owner_startup_success": 4,
  "loser_startup_lease_busy": 4,
  "loser_startup_source": 0,
  "owner_scheduler_ticks": 0,
  "loser_scheduler_ticks": 0,
  "relay_errors": 0,
  "redis_operation_errors": 0,
  "redis_error_class_delta": 0,
  "pool_pending_max": 0,
  "pool_wait_delta": 0,
  "pool_timeout_delta": 0
}
```

Use synthetic Prometheus/Redis fixtures to prove: an old disabled Pod plus two
new enabled-ReplicaSet Pods yields exactly the two new baselines; two baselines
are mandatory; the first four-manifest cohort writes one atomic summary;
scheduler tick one fails; missing baseline fails with `baseline_missing`;
source occupancy three fails; simultaneous success/timeout paths create exactly
one result; raw workspace deletion leaves evidence intact. Result finalization
must first claim `EVIDENCE_DIR/result.claim` with atomic `mkdir`, then use this
macOS-valid sequence:

```bash
mkdir "${EVIDENCE_DIR}/result.claim"
python3 - "${EVIDENCE_DIR}/result.tmp" "${EVIDENCE_DIR}/summary.json" "${EVIDENCE_DIR}" <<'PY'
import os
import sys

source, destination, directory = sys.argv[1:]
with open(source, "rb+") as stream:
    stream.flush()
    os.fsync(stream.fileno())
os.replace(source, destination)
directory_fd = os.open(directory, os.O_RDONLY)
try:
    os.fsync(directory_fd)
finally:
    os.close(directory_fd)
PY
```

Failure finalization uses the same sequence with `failure.json`. A losing
`mkdir` path exits without writing. No fixture contains real keys, Pod names,
credentials, or identities.

The observer writes its transient lifecycle state only to
`RAW_DIR/observer-state.json`. Before enablement the exact state must be
`waiting_for_fresh_pods`; this file is never copied into documentation.

- [x] **Step 5: Run static safety, syntax, and focused review checks**

Run:

```bash
/bin/bash -n "${GUARD_DIR}/guardian.sh" "${RAW_DIR}/controller.sh" "${RAW_DIR}/observer.sh"
! rg -n 'DEL|UNLINK|/api/v1/usage/team|authorization|bearer|password|token=' \
  "${GUARD_DIR}/guardian.sh" "${RAW_DIR}/controller.sh" "${RAW_DIR}/observer.sh"
shasum -a 256 "${GUARD_DIR}/guardian.sh" "${RAW_DIR}/controller.sh" "${RAW_DIR}/observer.sh"
find "${EVIDENCE_DIR}" -maxdepth 1 -type f -print | sort
```

Run all drills explicitly with `/bin/bash`, never the shebang-selected Bash.
Write sanitized commands/results and all three SHAs to
`.superpowers/sdd/task-16-1-report.md`. Generate an independent task review.
Resolve every Critical and Important finding before Task 2. Do not commit
temporary scripts.

---

### Task 2: Run The Single Authorized Guarded Staging Replay

**Files:**
- Modify temporarily: `/Users/admin/helm/ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json`
- Modify ignored: `.superpowers/sdd/task-16-2-report.md`
- Modify ignored: `.superpowers/sdd/progress.md`

**Interfaces:**
- Consumes: Task 1 reviewed script SHAs and passing fake drill.
- Produces: one durable pass/fail replay summary and a guardian-proven disabled final state.

- [x] **Step 1: Reverify immutable live baselines**

Require application HEAD clean, exact image index/architectures unchanged, staging revision 60 deployed exact-image disabled `1/1` with zero restarts and HTTP 200, production revision 69 unchanged, selector mode `0600`, and zero Task 16 artifacts outside the three owned directories. If a revision or runtime value drifted, stop without enablement.

- [x] **Step 2: Render and server-dry-run both selectors**

Copy the disabled selector to `GUARD_DIR/disabled.json` mode `0600`. Create `RAW_DIR/enabled.json` with only:

```json
{
  "replicaCount": 2,
  "env": {
    "AE_TEAM_USAGE_PREWARM_ENABLED": "true",
    "AE_TEAM_USAGE_PREWARM_TIMEZONES": "UTC,Asia/Shanghai,America/Los_Angeles,Europe/Berlin"
  }
}
```

Preserve every other selector value. Render and server-dry-run both selectors through the existing staging playbook. Disabled must render one replica and no prewarm environment. Enabled must render two replicas, exact image, enabled true, and the exact timezone string. Production resources must not appear in either rendered diff.

- [x] **Step 3: Arm guardian, heartbeat, and observer before enablement**

Capture the existing disabled Pod UIDs and template hash. Start the
controller-bound heartbeat writer before guardian, and touch the heartbeat
once. Start guardian with baseline revision 60 and a ten-minute absolute
deadline in an independent session. macOS has no `setsid` binary, so use the
installed Python runtime only as a process launcher:

```bash
nohup python3 -c \
  'import os,sys; os.setsid(); os.execv("/bin/bash", ["/bin/bash", sys.argv[1]])' \
  "${GUARD_DIR}/guardian.sh" \
  >"${RAW_DIR}/guardian.log" 2>&1 &
```

Guardian may write `armed` only after reading a heartbeat no older than two
seconds. Wait up to ten seconds for that state. Start observer before Helm
upgrade, give it the pre-enable UID/hash set and a 150-second result deadline,
and require `RAW_DIR/observer-state.json` to contain the exact state
`waiting_for_fresh_pods`.

Before enablement, also start an independent controller-liveness watcher and
require its closed waiting state. The live controller atomically records an
enable-attempt marker, creates `ENABLE_IN_PROGRESS_PATH` immediately before
spawning Helm, waits and fully reaps that exact Helm PID, and only then removes
the in-progress marker with a directory fsync. A normal finalization writes an
atomic controller-complete marker before the liveness watcher exits. If the
controller dies after the attempt marker and before controller-complete, the
watcher atomically records closed `controller_died` operational evidence and
projects it into the report. `restore_failed` has higher precedence than every
earlier operational reason. Production scripts accept only frozen live
binary, host, and dependency boundaries; all fake behavior runs generated
private copies with an explicit substitution manifest and byte-difference
proof.

Record PIDs only in the private raw report. Missing armed/waiting state blocks enablement.

- [x] **Step 4: Consume the one authorized live replay**

Install the enabled selector bytes locally and run one staging-only atomic Helm upgrade with `--wait --wait-for-jobs`. Do not retry enablement. Record the new revision and require two desired, ready, updated, and available replicas on the exact image.

Wait for exactly one `summary.json` or `failure.json`. At first result or observer deadline, atomically copy the sanitized result into `.superpowers/sdd/task-16-2-report.md`, fsync it, and only then create the guardian explicit-restore request.

- [x] **Step 5: Require guardian restoration and fresh final state**

Wait for guardian `restore_succeeded`. Measure the 660-second bound from the
explicit restore request or an earlier `restore_started` state. If guardian
reports `restore_failed`, has no terminal state at that bound, or dies, stop and
wait for its complete process group before running exactly one controller
`emergency_restore()` attempt. That path restores selector bytes and skips Helm
when fresh runtime is already exact-disabled. Freshly verify the newest Helm revision is
deployed, exact image disabled `1/1`, zero restarts, no prewarm environment,
HTTP 200 live/readiness, and production revision 69 unchanged. If both restore
paths fail, retain all files and escalate; never cleanup. Only after disabled
runtime is proven may the controller stop heartbeat and remove raw/guardian
directories.

Keep the sanitized evidence until it is copied into the ignored report. Then remove the evidence directory and verify zero `/tmp/ae-task16-*` paths locally and in the final staging Pod. Restore the selector to its exact disabled bytes, mode `0600`, unstaged, uncommitted, and unpushed.

- [ ] **Step 6: Task review before documentation**

Record the exact enabled/restore revisions, observer closed result, guardian states, final runtime, cleanup counts, and script SHAs in `.superpowers/sdd/task-16-2-report.md`. An independent reviewer must classify the replay as product pass, product-gate failure, or operational failure. Resolve every Critical and Important evidence finding without another enablement.

## Guarded Replay Execution Ledger

The one authorized replay ran once on 2026-07-23. Preflight passed at ledger
head `ac81c1cd`, disabled staging revision 60, unchanged production revision 69,
and image index digest
`sha256:1e683cd90c1a5366e7b1d6a6ffff509ac99efb8a9079303383af9b539214df38`.
The single enable completed at revision 61 and the single guardian rollback
completed at revision 62; the emergency path did not run.

```text
guardian.sh                    f4a328b00c0a68ac5a880035bd04b039b320dc4c009447f6e4230a45b0479eef
controller.sh                  4123c1167c381c84d985280c4b3f5e7a578ae5961cbb441c50b9598463c77230
observer.sh                    d002a33a157d730461c578ec376554c15e72ab9df161cc61dd2af8bcb62a2bc3
live-snapshot-command.sh       7daccf118441a763c192ab02276cea5087c7a3da9d22a127b6678f842a27bca0
write-enabled-marker.sh        04958cf6014fde31202d3bd1d90179d92b7e2a36af7afb1ca976e405b5f49080
live-observer-feed.sh          69fc20bf39a4bc0bb2ecc1d9eda7121ddf44dc15db2ee6f451e6a646d7c72834
enabled-marker-watcher.sh      10cab49d6fd7c261da1f0025af221011922d8a7e52410258fd88a0e7103adc0b
controller-liveness-watcher.sh 6b1199b9bbd7265541c09970329a980d0b6a08dfe7e6af728c3325b3bcd58613
operational recorder           e7670dc1be49982b76513411177317e721f80015c9288854a11852beb558297f
live replay controller         c1fb85986fec101e0b460010214d5b0094894e467fffb22733bb4713bd7b10cb
```

The enable attempt began 292 seconds before the enabled marker and restore
request. The feed closed with `old_pod_set_changed`; the controller requested
restoration, but enabled observation and marker completion continued. The
observer then
wrote `fresh_pod_selection_invalid`, so its product result overlapped rollback
and is non-authoritative. Guardian state reached
`restore_succeeded:explicit_restore` 30 seconds after the request. The
controller's one strict final Pod read about two seconds later recorded
`restore_failed`, but a fresh bounded terminal audit passed completely.

Independent review classifies the replay as `operational failure`, with no
product pass and no independently attributable product-gate failure. It found
that operational stop did not short-circuit later observation and that final
disabled verification lacked a bounded convergence wait. The outer launch
wrapper also returned early because it checked the child process group before
`setsid`; the independent controller continued and was never relaunched.

Final staging revision 62 is exact-image disabled `1/1`, zero restart, no
prewarm environment, frozen image digest, and HTTP 200 live/readiness.
Production revision 69 is unchanged and healthy. The selector is exact disabled
at mode `0600`. Runtime processes, local Task 16 artifacts, and final-Pod Task
16 artifacts are all zero. No account or Team Usage API was used, Redis access
was read-only, and no business key was deleted.

---

### Task 3: Record The Guarded Replay Decision

**Files:**
- Modify: `docs/superpowers/specs/2026-07-23-team-usage-replay-guardian-design.md`
- Modify: `docs/superpowers/plans/2026-07-23-team-usage-replay-guardian.md`
- Modify: `docs/superpowers/specs/2026-07-23-team-usage-startup-cohort-publication-design.md`
- Modify: `docs/superpowers/plans/2026-07-23-team-usage-startup-cohort-publication.md`
- Modify: `docs/superpowers/plans/2026-07-21-team-usage-segmented-timezone-prewarm.md` only for the Task 9 resume decision.

**Interfaces:**
- Consumes: reviewed Task 2 runtime evidence.
- Produces: the authoritative pass/fail decision and clean final branch/runtime state.

- [x] **Step 1: Write the sanitized replay ledger**

Record only script SHAs, Helm revisions, exact image digest, relative durations, closed counts/outcomes, guardian states, cleanup counts, and final runtime. Do not retain process IDs, directory paths, raw output, selector bytes, Redis keys/values, manifests, Pod names, credentials, identities, or response bodies.

- [x] **Step 2: Apply the decision without weakening gates**

On pass, check Task 16 and state only that a fresh Task 9 Step 3 may restart; keep Task 9 Steps 3-6 unchecked. On product failure, record the exact nonzero/missing gate. On operational failure, record the exact guardian/observer/evidence failure. In all cases state that no third replay is authorized and leave `docs/architecture.md` unchanged.

- [x] **Step 3: Commit and independently review the ledger**

Run `git diff --check`, verify the exact allowed documentation scope, and commit:

```bash
git add docs/superpowers/specs/2026-07-23-team-usage-replay-guardian-design.md \
  docs/superpowers/plans/2026-07-23-team-usage-replay-guardian.md \
  docs/superpowers/specs/2026-07-23-team-usage-startup-cohort-publication-design.md \
  docs/superpowers/plans/2026-07-23-team-usage-startup-cohort-publication.md \
  docs/superpowers/plans/2026-07-21-team-usage-segmented-timezone-prewarm.md
git commit -m "docs(teamusage): record guarded cohort replay"
```

Generate a task-scoped review package. Resolve every Critical and Important documentation finding without another replay. Verify the application worktree clean, final staging disabled, production unchanged, selector deliberate and uncommitted, and Task 16 artifacts zero.
