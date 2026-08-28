# Pilot-sourced usage and attribution

Status: accepted, 2026-08-28. Supersedes the per-agent usage readers as the
primary source. Resolves the source question in
[#401](https://github.com/LichKing-2234/ai-efficiency/issues/401).

## Decision

LoongSuite Pilot is the single collector. `ae-cli` reads its normalized local
output and uploads; it does not read agent files while Pilot is collecting.
Both surfaces — usage events and commit-bound claims — are sourced from it, and
both are priced from what Pilot observed.

## Why claims are priced locally rather than reconciled

Reconciling a claim against the relay only ever covered relay-routed traffic.
Measured on a developer machine running all three agents: Codex has
`base_url = sub2api`, Claude Code has no `ANTHROPIC_BASE_URL` at all, and Kiro
CLI bills in credits. Two of the three never reach the relay, so two thirds of
the traffic has no counterpart to reconcile against — not a missing identifier,
no record at all.

This is structural, not a configuration gap. The relay's key is minted on the
wire, and Pilot observes the agent rather than the wire: across all four of its
event kinds there is no HTTP identifier of any sort. Pilot can only carry a
request id where an agent happens to have logged the response header itself.
"Pilot is the only collector" and "claims are reconciled against the relay"
cannot both hold.

Pricing every agent the same way also keeps one commit from carrying two kinds
of number. The alternative — Codex reconciled, the others local — buys an
authoritative figure for the least-used third of the traffic at the cost of
maintaining two pricing paths and the conflicts between them.

## Identity

Usage is accounted per native response id, never per turn. Pilot derives
`gen_ai.turn.id` for Claude Code and Kiro CLI as `<session>:t<N>`, where `t<N>`
is a counter the collector maintains: Claude Code has no turn concept at all.
A resumed session gets a new session id and restarts that counter while the
agent replays the same transcript. Measured on one machine, 190 of 1318
responses appear under two turn ids. Native response ids do not move.

Claims are named by `digest(provider, commit, evidence_digest)` for the same
reason. Both parts are content-derived, so the same work always names the same
group however the collector segmented it. A turn that proves nothing keeps the
observational name — it has no evidence to be named by, and is not delivered.

## Token semantics

Pilot normalizes `gen_ai.usage.input_tokens` to the whole input, cache included,
for every agent. The usage surface keeps the cached part in its own field, so
the reader subtracts: the four token fields are disjoint and their sum is the
consumption. This also settles a disagreement between the readers it replaces —
the Claude reader reported input and cache as disjoint, the Codex reader carried
Codex's own convention where any sum of the two double counts.

## Liveness

Pilot's service is supervised by launchd or systemd, which start it at login and
restart it on abnormal exit. `ae-cli` does not manage the process: a second
supervisor would fight the first and would override the one way a person has of
turning collection off.

What the platform's supervision cannot see is a graceful stop nobody asked for.
Pilot's SIGTERM handler exits zero and launchd's `KeepAlive` is conditioned on
`SuccessfulExit`, so a plain kill reads as an intentional shutdown and the
service stays down until the next login. `ae-cli` reports exactly that blind
spot, escalating with the gap: a restart backfills from a byte cursor into each
agent's own transcript, but only while those transcripts exist, and Claude Code
prunes its sessions after 30 days by default.

A stopped collector also hands the usage source back to the per-agent readers.
Pilot's output directory survives being turned off, so deciding on the directory
alone would keep the fallback suppressed while nothing replaced it.

## Evidence arrives after the commit

For Claude Code a mutation reaches Pilot's output only when the turn ends, and a
developer who edits and commits inside one turn commits first. The scan that
post-commit starts then sees no evidence at all. Measured here, a commit made at
16:29 was scanned against output last written at 16:22 and proved nothing.

Scan progress therefore carries the commits a scan could not prove, and every
later scan retries them. A commit is forgotten once a candidate proves it; a
gapped candidate proves nothing and leaves it pending. Entries older than the
window a scan reads are dropped, because retrying them can no longer succeed.

Codex is not affected — the collector tails its transcript continuously rather
than waiting for a turn to end.

## Replayed sessions partition the usage; the batch adds the partitions

Compacting or resuming a Claude Code conversation creates a new session file
carrying the replayed history, and Pilot exports that replay as one giant first
turn — measured live, turns spanning five to seven hours of history against
minutes for a real turn. Such a turn contains every mutation of the history, so
it proves every commit the history produced, and its claim converges on the
same content-derived group ids as the original turns'.

The scan already prevents double pricing here: every response is priced into
exactly one turn's claim, the earliest occurrence winning, so a replayed copy
loses to its original wherever the original was exported. What remains in a
giant turn's claim is the consumption whose originals were never exported —
history predating the install, or wiped by a reinstall. Claims meeting under
one group id therefore carry disjoint partitions of the consumption, verified
live: each candidate's buckets equalled its winner partition exactly, and the
partitions summed to the history's total.

The upload batch carries each group id once — the backend rejects a repeat for
disagreeing about its session and turn — with the partitions' allocations
unioned and their usage added. Added, not deduplicated: keeping only one
partition, an earlier behaviour, silently and permanently dropped the rest,
because acknowledgements map back by group id and marked the dropped claims
delivered.

Two windows remain open and close only with response-level dedup on the
backend: a response can be re-billed when its claim was delivered and its
Pilot source then rotated out, so a later scan reassigns it to a replay turn
under a fresh group; and nothing deduplicates across machines.

## Accepted costs

- **One duplicate window at cutover.** Pilot's dedupe keys are in a different
  namespace from the per-agent readers' and the server deduplicates on
  `dedupe_key` alone, so usage both sources still hold is counted twice for as
  long as Pilot retains it — seven days by default. Accepted rather than
  carrying a migration marker, while the platform is pre-release.
- **No relay cross-check.** Commit-bound figures are what the agent reported.

## Credit is an independent unit

Some agents bill in credit instead of tokens — Kiro CLI reports only
`kiro.credit_cost`, with every token field zero and `kiro.token_source`
declaring tokens unavailable at source. Credit is carried as its own amount on
the usage bucket and the attribution pool (`credit_usage`), never converted to
or from tokens, and a bucket must carry at least one of the two. It adds across
collapsed partitions and grows through the same monotonic contribution path as
tokens. This closes the last gap in the claim surface's agent coverage: a Kiro
commit is now proven and priced, in the unit Kiro actually bills in.

## Not resolved

- Windows is not wired up: Pilot's installer there is a PowerShell script with
  different arguments.
- A Codex session already running when Pilot is installed loses its workspace
  attribution permanently: Pilot's tailer starts at the end of existing files
  and never reads the `session_meta` line carrying `cwd`. Upstream defect.
- `tool_usage_events.dedupe_key` is globally unique with no user or repo scope,
  so a collision between users is silently dropped rather than reported.
