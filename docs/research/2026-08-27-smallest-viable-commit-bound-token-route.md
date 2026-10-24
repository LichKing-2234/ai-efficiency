# Smallest Viable Commit-Bound Token Route

**Date:** 2026-08-27
**Question:** [#405](https://github.com/LichKing-2234/ai-efficiency/issues/405) — given the resolved LoongSuite evidence facts and the current structured-mutation root cause, what is the smallest viable route to reliable Commit-bound Token attribution?
**Map:** [#401](https://github.com/LichKing-2234/ai-efficiency/issues/401)
**Status:** Decision-ready research. Planning only; no production code was written or modified.
**Code baseline verified:** `main` at `36bb67b1`

## Evidence Classification

Every claim below is tagged:

- **Verified in code** — read at `36bb67b1` in this working tree, or executed here.
- **Asserted by spec** — stated in a governing document under `docs/superpowers/specs/`.
- **Reported by research** — concluded in the [#403](https://github.com/LichKing-2234/ai-efficiency/issues/403) or [#404](https://github.com/LichKing-2234/ai-efficiency/issues/404) artifact, not independently re-derived here unless noted.

## 1. Recommendation

**Take the artifact-first route: keep the first-party local agent artifact as the only evidence that may authorize a Commit-bound Token claim, close the one generated-wrapper grammar gap that is currently suppressing every Codex patch turn, and hold LoongSuite outside the accepted path as an optional shadow provider that is never consulted to create, complete, or authorize an allocation.**

Three facts decide it, in this order.

**The remaining distance to "reliable" is one parser grammar, not one architecture.** Everything downstream of wrapper recognition already exists, is fail-closed, and has production evidence behind it: exact trusted request identity bound to thread and turn, deterministic patch replay against the commit's parent and current content, once-only global accounting, append-only allocation, and non-additive projection when one turn's evidence lands in several commits. The current failure is that recognition happens *before* any of that machinery runs, so a turn with perfectly good trusted request evidence and perfectly good Git content produces an empty mutation set and an empty allocation. Nothing about that failure is improved by adding a second telemetry source; it is improved by teaching the recognizer the shape the tool actually emits.

**The LoongSuite-augmented route cannot shorten the route, because it cannot supply the missing artifact.** The normalized event model carries repository, branch, and workspace facts but no commit, revision, or checkpoint identity at all. Any LoongSuite-bearing design therefore still contains the entire deterministic mutation-and-Git-proof pipeline, plus a collector, plus an ingestion contract, plus a retention and outage story that current public material does not establish. It is strictly the artifact-first route plus a dependency. That makes it a candidate *augmentation* of a solved route, never a smaller route.

**The tool-neutral envelope is the largest change and the one whose preconditions are demonstrably not met.** One of the three candidate tools cannot participate at all — no request-level Token components exist in its first-party source, so it must stay unavailable rather than be estimated. A second tool has strong session, turn, and Token semantics but no counterpart to the structured mutation artifact that makes commit binding deterministic here; without that, binding its Token to a commit would have to fall back on workspace, branch, or time proximity, which the map forbids. And the persisted accounting model has no agent or tool dimension whatsoever today, so "tool-neutral" is not a relabeling — it is a schema change to the durable ledger. Tool neutrality is an outcome to earn after a second tool independently proves an exact mutation-to-commit artifact; it is not a starting shape.

The honest form of the decision is therefore: **artifact-first now, with a named, evidence-gated door to tool-neutrality later, and LoongSuite parked behind a contract it has not yet been asked to satisfy.**

### Route comparison

| | Artifact-first (recommended) | LoongSuite-augmented | Tool-neutral envelope |
|---|---|---|---|
| Supplies commit proof? | Yes — already implemented | No — must reuse artifact-first proof anyway (#403) | Only if each tool independently supplies an exact mutation artifact |
| New runtime dependency | None | Local collector + remote ingest/query | Collector-agnostic, but new durable schema dimension |
| Blocking unknowns | None material | Retention, index, transport encryption, ACK/replay all unverified (#403) | No mutation artifact for Claude here; Kiro structurally excluded |
| Change surface | CLI parser + one scanner-progress bump | CLI + collector + backend consumer + privacy/retention contract | CLI + wire schema + durable ledger + Activity reads |
| Recovers already-retained turns | Yes — retained trusted evidence plus retained Git content | Not for expired evidence; no synthesis permitted | No |
| Fail-closed posture preserved | Yes, unchanged | Only with a new explicit provider contract | Requires re-proving conservation per tool |
| Honest weakness | Grammar drift recurs with each agent release | Buys forensics, not authority | Solves a problem the product does not yet have |

The comparison is genuinely close on exactly one axis: **observability of drift**. LoongSuite's real attraction is that it would give an independent, queryable view of turns whose Token never reached a commit — which is precisely the failure class that went unnoticed here. That is a real benefit and it is why LoongSuite should not be closed out. But it is a *diagnostic* benefit, and Section 7 shows it can be obtained far more cheaply from the local claim state the CLI already maintains.

## 2. Initial Tool Scope

**In scope now: Codex only.** This matches the map's first-class scope and the current implementation.

Verified in code: the local claim scanner reads only Codex sources — active and archived Codex JSONL sessions plus the Codex log SQLite database (`ae-cli/internal/attributionlocal/claims_v2.go:226` `findCodexV2JSONLFiles`, `ae-cli/internal/attributionlocal/scanner.go:243` `findCodexSQLiteFiles`, `ae-cli/internal/attributionlocal/claims_v2.go:1065` `loadCodexV2RequestEvidence`). There is no Claude or Kiro entry point into `V2ClaimCandidate` construction.

Verified in code: Claude and Kiro reach the backend only through the legacy `LocalToolUsageEvent` compatibility path (`ae-cli/internal/attributionlocal/scanner.go:89-155`, `claude_jsonl.go`, `kiro_json.go`, `kiro_cli_sqlite.go`, `kiro_ide.go`). That path carries no commit, no checkpoint, and no Git proof, and — critically — it selects records by comparing the recorded `cwd` against the workspace root (`ae-cli/internal/attributionlocal/claude_jsonl.go:39`, calling `workspace_path.go:9` `sameWorkspacePath`). Under the map's constraint that cwd and path heuristics are forbidden on any accepted path, **this compatibility surface can never be promoted into Commit-bound Token attribution as it stands.** Reusing it would import the exact heuristic the map bans.

### Gate for adding Claude Code

Claude Code may enter Commit-bound Token scope only after all of the following hold. Each is a separate, checkable fact, and any one failing keeps Claude Code out.

1. A first-party Claude Code artifact yields a **structured file mutation record** — path plus resulting content or content digest per changed file — that can be replayed against the commit's parent tree and compared to the commit's current tree, with no reliance on cwd, branch, timestamp, or diff similarity. This is the piece Codex supplies today and the piece #403 did not find in the normalized envelope.
2. The Token components for the turn that produced those mutations bind to that turn through **native identity that survives into whatever the consumer reads** — the native prompt identity or the native tool-use identity, not a collector-derived sequence number.
3. A terminal first-party success event distinguishes completed from interrupted, failed, and replayed turns, and the non-completed cases fail closed.
4. The durable ledger has gained an explicit persisted source dimension (see Section 5's drift note) so Claude-sourced Token cannot be silently merged with Codex-sourced Token in one pool.
5. Conservation is re-proved end to end for the second tool: the four Token components sum exactly, replay is idempotent, and no pool double-counts.

Gate 1 is the hard one and it is the reason Claude Code is not in scope now.

### Gate for adding Kiro

**None available.** Reported by research (#403), from first-party documentation and adapter source: Kiro's current sources expose daily credit and message aggregates and, optionally, content-bearing prompt logs, but no request-level Token components; the collector deliberately records the source as unavailable rather than synthesizing zero. Kiro must remain `token_source_unavailable`, and credit, message count, model count, context percentage, and elapsed time must never be converted into Token. Revisit only if Kiro's own product exposes request-level Token components — not if a collector begins to estimate them.

## 3. Exact Trust Boundary

The line is: **an accepted claim is authorized by first-party local artifacts of the agent that made the change and by Git itself, and by nothing else.** Everything below is verified in code unless marked.

### What carries trust

**Request identity — trusted, narrowly.** A `relay_official` claim's Request ID comes from a row in the Codex log SQLite database that simultaneously satisfies: the HTTP client target; a completed POST to the responses path; a 2xx status matched as a whole field, not a substring; and a present `x-client-request-id` header (`claims_v2.go:1075-1085`, `1100-1104`). The extracted identity is normalized to exactly one `client:` prefix (`claims_v2.go:1470` `normalizeV2RequestID`). Thread and turn are taken from the same row's structured `thread.id=` / `turn.id=` fields (`claims_v2.go:41-42`). If the same Request ID ever appears against a different thread or turn, **both** observations are marked ambiguous and neither is usable (`claims_v2.go:1111-1121`).

**A single narrow turn-UUID fallback — trusted only when globally unique.** When a candidate has no Request ID, the CLI may fall back to matching on the turn UUID alone, but only if: the turn ID parses as a UUID; exactly one JSONL local claim identity maps to it; exactly one SQLite thread identity maps to it; no ambiguous evidence touches it; and the candidate already has a non-empty commit allocation and evidence digest (`claims_v2.go:738-759`). Anything else becomes `ambiguous_request_evidence` and stays local.

**WebSocket completion — trusted only as an intersection of two exact rows.** A `codex_local` turn requires a non-warmup transport row and a separate successful post-sampling row, joined **only** by identical thread and turn identities (`claims_v2.go:1176-1228`). An older raw `response.completed` row is accepted when it carries the same exact identities (`claims_v2.go:1240` `parseCodexV2WebSocketTurnEvidence`).

**Structured mutation — trusted only in exact shapes.** Either a direct `apply_patch` tool payload (`helpers.go:23` `isPatchTool`) or one of two anchored generated-wrapper grammars (`claims_v2.go:44-45`, applied at `claims_v2.go:762` `v2StructuredPatchInput`). The decoded value must additionally carry exact `*** Begin Patch` / `*** End Patch` framing.

**Git content — the actual authority.** Recognition of a wrapper never authorizes anything by itself. A recognized patch is turned into per-file mutations by replaying the patch blocks against the commit's parent content (`claims_v2.go:788` `v2PatchMutations`, `997` `applyV2PatchBlock`), each path is canonicalized and must resolve *inside* the repository root (`claims_v2.go:957` `canonicalClaimPath`), and only mutations whose before/after states match the commit's parent and current tree survive (`claims_v2.go:898` `introducedV2Mutations`, using `git show` at `991` `gitShowClaimFile`). If nothing survives, the candidate is `commit_content_mismatch` and produces no allocation (`claims_v2.go:667-669`).

**Checkpoint and repository identity — frozen at trigger time.** Each commit trigger freezes its server, reporting owner, repository config ID and key, workspace, relay provider, commit SHA, and checkpoint event ID (`ae-cli/internal/hooks/sync_task.go:111` `V2SyncTrigger`; consumed at `background_runner.go:451-455`). Lineage moves only on explicit Git rewrite or explicit cherry-pick evidence with a stable patch identity (`helpers.go:30` `StableCommitPatchID`; backend `attributionpool/service.go:258` `ApplyRewrite`, `590` `ApplyCherryPick`).

### What does not carry trust

- Any timestamp, elapsed time, or proximity between events. Time is used only as a retention window boundary and as a stable sort key, never as a join.
- cwd, absolute path, workspace path, or branch. An absolute path is accepted **only** after it resolves inside the repository and is rewritten to a repository-relative path; parent traversal and outside paths are rejected (`claims_v2.go:957-979`).
- Model name or Token-value similarity.
- Transport identifiers: raw `resp_*` values, connection IDs, `x-request-id`, and gateway IDs. Response and call IDs are used only as an in-turn transport set for the unique-match fallback and are never uploaded (`claims_v2.go:531-535`).
- A recognized wrapper, on its own.
- Remote UI visibility of a trace. Reported by research (#403): this is not an ingestion acknowledgement and must never be treated as one.
- **LoongSuite, in every capacity that matters here.** Under this route the collector may never create an allocation, complete a partial claim, substitute for a missing Request ID, or relax any gap reason.

### Where fail-closed sits

The gap reason is assigned before anything can be uploaded (`claims_v2.go:656-696`), and only a candidate with an empty gap reason is uploadable (`claims_v2_delivery.go:72` `v2ClaimUploadable`). The complete fail-closed set as implemented: `missing_structured_mutation`, `invalid_structured_mutation`, `commit_content_mismatch`, `missing_local_usage`, `invalid_local_usage`, `mixed_token_sources`, `missing_request_id`, `ambiguous_request_evidence`, and `request_evidence_expired`.

Two of these deserve emphasis for this decision:

- `mixed_token_sources` — a turn carrying both trusted HTTP Request IDs and trusted WebSocket completion evidence is never uploaded (`claims_v2.go:688`). The two Token authorities are mutually exclusive per group.
- `request_evidence_expired` — when the underlying trusted log has rotated past the earliest retained successful row, an older candidate is classified as expired rather than retried, and no Request ID or Token is ever synthesized (`claims_v2.go:703-705`, with the lower bound computed at `claims_v2.go:1106-1109`). **Evidence rotation is a permanent, honest loss.** This is the single most important reason to close the wrapper gap promptly rather than deliberating further: every day the gap stays open, retained turns cross that boundary and become unrecoverable, and the map correctly forbids synthetic backfill.

### The one open trust question this route inherits

The current WebSocket transport used by remote-control sessions is **not** trusted, by explicit design (asserted by spec, `2026-08-11-codex-commit-token-attribution-v2-design.md` §16). Artifact-first does not change that and must not be read as changing it. Reported by research (#404): closing the wrapper gap recovers retained HTTP turns and does nothing for that transport, which still needs its own exact transport, success, and turn-identity contract.

## 4. Privacy Boundary

### Never leaves the machine

Verified in code by the shape of the upload type (`ae-cli/internal/client/attribution.go:77` `AttributionV2ClaimGroup`) — there is no field capable of carrying any of the following, and the local candidate keeps them out of the uploaded `Group` by construction (`claims_v2.go:60-72`, comment and structure): prompt text, model response text, reasoning, patch content, diff content, source code, file paths, command arguments or output, raw JSONL rows, raw SQLite rows, raw spans, API keys, API key IDs, and account identifiers.

Mutation evidence leaves the machine **only as a SHA-256 digest** over the surviving mutations (`claims_v2.go:982` `v2MutationDigest`, hashing via `claims_v2.go:1496` `claimDigest`). The digest is computed over canonical repository-relative path, content state, and kind — the paths themselves are never transmitted.

Local claim scan progress is likewise digest-only: turn identities are persisted as digests, not raw thread and turn strings (`claims_v2.go:118` `V2ClaimTurnKeys`, `98` `SourceEvidenceKey`).

### Leaves the machine, deliberately

Thread ID and turn ID (as identifiers, for hot-claim correlation and replay idempotency), Request IDs for the HTTP path, four Token components plus requested model and a 15-minute UTC bucket for the WebSocket path, an optional calibration envelope for the HTTP path, and per-allocation repository config ID, repository key, workspace ID, checkpoint event ID, commit SHA, and evidence digest.

### Removed after the hot window

Asserted by spec (§7, §8) and consistent with the ent schema read here: hot claim detail is bounded at 90 days (`attributionclaim/service.go:28` `HotRetention`); at finalization the Request IDs, calibration, and local aggregates are removed, and the long-lived pool retains no Request ID, no response ID, no calibration, no local aggregate, no key or account, no prompt or response or code, and no path (`backend/ent/schema/attribution_usage_pool.go`, `attribution_usage_pool_commit.go`).

### Product surface

Asserted by spec (§13): product DTOs and UI exclude Request IDs, response IDs, raw claim rows, calibration, hot aggregates, keys, accounts, prompts, responses, code, local paths, pending Request counts, and coverage-gap detail.

### What "masked" would have to mean if LoongSuite were ever admitted

Reported by research (#403): the collector's masking mode defaults to `none`, model / Token / duration / branch / workspace path are explicitly not scanned as secret content, and failed OTLP spans are written to local files as fully serialized spans. Therefore, if LoongSuite is ever admitted even in shadow, the metadata-only allowlist must be enforced **before conversion and export**, not at the remote backend, and the failed-span files must be brought under an explicit retention and scrubbing policy — the current retention service does not enumerate that category at all. Until that is designed and tested, no attribution evidence may traverse a LoongSuite remote path.

## 5. Allocation And Accounting Invariant

### The invariant

> Every accepted trusted HTTP Request, and every accepted WebSocket local usage aggregate, that is bound to committed code by deterministic Git proof contributes its Token **exactly once** to the global ledger — regardless of how many commits, repositories, or pull requests that evidence touches, and regardless of how many times it is rescanned, replayed, or re-delivered.

Asserted by spec (§3, "The accounting invariant is"). Verified in code at every layer:

- **Once per Request identity:** unique on provider plus request ID; identical replay returns `duplicate_identical`, different canonical content for the same identity is a conflict that cannot be acknowledged as persisted (asserted by spec §7; enforced in `backend/internal/attributionclaim/service.go:307` `upsertRequest` and the acknowledgement statuses at `claims_v2_delivery.go:225` `v2ItemAcknowledged`).
- **Once per pool:** the canonical pool key is a digest over ledger epoch, provider, user, requested model, 15-minute bucket, **and the sorted counting-commit set** (`backend/internal/attributionpool/service.go:873` `canonicalPoolKey`). Contributions collapse into one row (`ensurePool` at `:497`, `addContribution` at `:513`).
- **Exact component conservation:** the four components must sum to the total or the contribution is rejected, with explicit overflow checks (`attributionpool/service.go:433-441` `canonicalContribution`; mirrored on ingest at `attributionclaim/service.go:374` `validate`).

### Multi-commit — this is settled, not open

The map lists "the allocation model when one Token claim spans multiple commits" as not yet specified. **That is contradicted by both the active spec and the current code, and #405 should record it as resolved rather than re-deciding it.**

The rule, verified end to end:

1. **Splitting is forbidden.** Asserted by spec (§16, explicit non-goals): "splitting one Request's Token by lines, files, commits, repositories, or PRs".
2. **Allocations are an ordered, append-only prefix.** The CLI appends a new allocation only for a checkpoint event it has not already recorded, and renumbers sequence positionally (`claims_v2.go:1442` `mergeV2Allocations`). The backend rejects any batch whose allocation list is not an exact prefix-preserving extension of what it already holds (`attributionclaim/service.go:451` `compatibleAllocations`), and requires `Sequence == index+1` (`service.go:404`).
3. **The evidence digest is stable for the single-allocation case and composite only after a second allocation.** One allocation means the group digest *is* that allocation's digest; two or more produce an ordered composite (`claims_v2.go:1459` `v2AllocationEvidenceDigest`). This is what keeps an exact rescan from mutating an already-acknowledged envelope.
4. **One counting commit makes the pool direct; more than one makes it shared; the Token is stored once either way.** Verified in code (`attributionpool/service.go:554-560` `ensureCommitRelations`) and asserted by spec (§8). The ent schema states the intent directly: shared and inherited relations are deliberately non-additive projections (`backend/ent/schema/attribution_usage_pool_commit.go`).
5. **Adding a second commit migrates and merges rather than duplicating** (`attributionpool/service.go:908` `mergePoolTotals`; §8).
6. **Reads project without summing.** Activity aggregates per pool and carries `has_direct` / `has_shared` overlap flags rather than dividing or duplicating totals (`backend/internal/activity/v2_sql.go:41`, `:153-159`, `:245`). A pool involved in several pull requests contributes its full involved total to each, and those rows are explicitly non-summable (§9).
7. **Cherry-pick is visible but non-counting**, and only with explicit lineage plus a stable patch match (`attributionpool/service.go:590` `ApplyCherryPick`; relation kind `inherited_non_counting`).

### The one genuinely open sub-question inside multi-commit

Verified in code: the CLI deduplicates allocations by **checkpoint event ID** (`claims_v2.go:1444-1447`), while the backend's pool key deduplicates by **repository plus commit SHA** (`attributionpool/service.go:457` `canonicalCommits`). These are not the same key. Two distinct checkpoint events naming the same commit — plausible after a worktree-recovery replay or a re-triggered checkpoint — would produce two client-side allocations for one commit. The backend would still count the Token once, because the commit set collapses; but the group envelope would differ from what was previously acknowledged, and envelope equality is a strict field-by-field comparison including the allocation list (`claims_v2_delivery.go:175` `sameV2GroupEnvelope`). The likely outcome is a stuck delivery state rather than double counting — accounting stays safe, delivery does not.

This is small, it is bounded, and it belongs in the commissioned work of Section 7 as a stated regression case. It does not change the route.

## 6. Operational Dependency Posture

### What the recommended route depends on

Only what is already required to run the product:

1. **Local first-party Codex artifacts** — the session JSONL files and the log SQLite database in the user's Codex home directory.
2. **A working local Git repository** — patch replay shells out to `git show` against the commit's parent and current tree (`claims_v2.go:991` `gitShowClaimFile`) and to `git patch-id --stable` for lineage (`helpers.go:41`).
3. **Managed Git hooks and the event-driven runner** — no daemon and no periodic synchronizer (asserted by spec §16; verified by the trigger-and-lease structure in `ae-cli/internal/hooks/sync_task.go` and `background_runner.go`).
4. **The backend claim ingestion endpoint**, reached with the scoped reporter credential.

There is **no** network dependency in the evidence path itself. Evidence is produced, validated, and reduced to digests entirely offline; the network is used only to deliver an already-proved claim.

### Outage behavior, verified in code

- **Backend unreachable, or response lost.** Claims persist in the local state file under the user's CLI state directory, guarded by an exclusive lock with a five-minute stale-lock recovery (`helpers.go:54` `withStateFileLock`; state at `claims_v2.go:238` `V2ClaimStatePath`). Delivery is retried on the next hook-driven pass. Nothing is lost and nothing is invented.
- **Partial or malformed acknowledgement.** Only independently acknowledged Request IDs and calibration values are removed; everything else is retained and the claim is marked for recovery (`claims_v2_delivery.go:81` `ApplyV2ClaimAcknowledgements`). Unknown, duplicate, and missing acknowledgements all become `upgrade_required`, which is a **blocking** recovery state — the runner reports failure rather than silently proceeding (`background_runner.go`, `deliverV2ClaimState`).
- **Terminal conflict.** Marked `conflict` and made permanently non-uploadable (`claims_v2_delivery.go:72-77`); asserted by spec and architecture notes, such snapshots are quarantined rather than retransmitted, so one bad claim cannot poison later triggers.
- **Process exit mid-scan.** Progress is digest-only, saved after claim-state persistence, and resumable per source-and-trigger unit (`background_runner.go:470-575`; progress record at `hooks/v2_scan_progress.go:16`).
- **Scanner semantics change.** A progress version older than the current constant clears completed units and forces exactly one rescan (`hooks/v2_scan_progress.go:78-98` `migrateV2ClaimScanProgress`). Without a bump, completed units are skipped whenever trusted Request evidence is unchanged, so a parser repair would silently fail to reprocess historical turns.
- **Trusted evidence rotated out.** Fail-closed and terminal, as described in Section 3. This is the only outage class that loses attribution, and losing it is the correct behavior.

### LoongSuite's posture — explicit

**Optional provider behind a contract that does not yet exist, currently not admitted even in shadow. Not a dependency, not on the accepted path, and not a prerequisite for any part of this route.**

Concretely, four conditions must all hold before LoongSuite may be *installed* in a shadow capacity, and a fifth before it may be *read* by anything attribution-related:

1. A named consumer-facing evidence contract exists — the #403 artifact's `CommitBoundTokenEvidence` shape — with LoongSuite as one implementation behind it, so removing the collector removes zero behavior.
2. The metadata-only allowlist is enforced before conversion and export, and failed-span files are covered by an explicit retention and scrubbing policy.
3. Local file output only, with a durable local cursor and idempotency keyed on the collector's own event identity. No remote ingest.
4. The complete pass list from the #403 POC contract is satisfied — native session and turn identity survive normalization; a terminal first-party event proves success; Token attaches to the exact turn exactly once without summing parent and child span aggregates; the join to a commit candidate is by explicit shared identity, never repository/branch/workspace proximity; and the checkpoint module independently proves the commit.
5. Before any remote path: live read-back of the actual workspace's retention, indexes, query API, least-privilege authorization, and deletion behavior, plus a proven encrypted transport and a proven replay boundary. Reported by research (#403), every one of these is currently unverified.

**If LoongSuite goes down, is uninstalled, or is never installed, Commit-bound Token attribution is unaffected.** That property is the point of the posture, and it should be treated as a hard invariant of the route rather than an implementation detail.

### Is the bounded LoongSuite shadow POC still worthwhile?

The map asks this directly. **Deferred, not cancelled, behind a named trigger.**

The diagnostic value LoongSuite offers — visibility into turns whose Token never reached a commit — is real, but Section 7's commissioned work delivers most of it for a fraction of the cost, because the CLI already computes a gap reason for every turn and already summarizes those reasons locally (`claims_v2_delivery.go:42` `SummarizeV2ClaimDelivery`). What was missing was not a second telemetry pipeline; it was a gap reason specific enough to distinguish "this agent emitted a shape we do not recognize" from "this turn simply had no patch".

Commission the LoongSuite shadow POC only when a *second* tool is a live product requirement — that is, when the Claude Code gates in Section 2 are being actively pursued — since normalization across tools is the one thing LoongSuite does that the local path does not. Until then it stays research-complete and unimplemented.

## 7. Next Spec Or Prototype To Commission — Exactly One

**Commission: an amendment to the active Codex Commit Token Attribution v2 design, Section 5, titled "Generated Structured-Mutation Wrapper Grammar And Drift Observability", together with the scanner-progress version bump it requires.**

Per the repository's documentation rules, a change to the currently effective contract is written into the newest applicable spec rather than a new document; the 2026-08-11 design is that spec, and Section 5 is that contract.

### Scope

In scope:

1. Promote the recognized generated-wrapper set from an implementation detail into an **enumerated, versioned grammar list** in the spec, with each accepted shape written out literally, and with the standing fail-closed constraints restated as applying to every member of the set: whole-wrapper anchoring, exactly one patch call, exactly one JSON string literal, matching patch identifier between binding and call, matching result identifier between binding and use, exact patch framing, and rejection of comments, template literals, extra calls, property access, mismatched identifiers, and trailing control flow.
2. Add the current Codex 0.149.1 three-statement shape to that set: bind the patch literal, bind the awaited result, pass the result directly.
3. Add a distinct, countable gap reason for **well-formed but unrecognized** wrappers, separate from the existing "no mutation at all" reason, and surface its count in the existing local delivery summary and diagnostics.
4. Specify the scanner-progress bump from v5 to v6 and the one-time exact rescan it triggers, plus the rule that any future grammar addition requires the same bump.
5. Record the allocation dedupe-key mismatch from Section 5 as a stated regression case.

Explicitly out of scope: any backend schema change, any Activity API change, any Request-correlation change, any Token authority change, any LoongSuite integration, and any change to the untrusted remote-control WebSocket transport. Reported by research (#404) and independently consistent with everything verified here: none of them is implicated in this failure.

### What it must prove

1. **The current shape is accepted and produces an allocation.** With a real repository, a real commit, and synthetic trusted Request evidence, the three-statement wrapper yields exactly one uploadable allocation — with both relative and repository-internal absolute patch paths.
2. **The near-misses still fail closed.** Mismatched result identifier, property access on the result, comments, template literals, malformed framing, multiple patch calls, and trailing statements all remain rejected.
3. **Unrecognized wrappers are now visible rather than silent.** A well-formed wrapper outside the enumerated set produces the new distinct gap reason and appears in the local summary. This is the requirement that turns the whole class of failure from invisible to observable, and it is the single most valuable line in the commission — the current defect survived a prior repair of the same class precisely because there was no counter for it.
4. **Git proof is still the authority.** A recognized wrapper whose replayed content does not match the commit still yields no allocation.
5. **The v5→v6 rescan runs exactly once and is idempotent afterwards**, and a repaired historical turn that already has retained trusted evidence produces its allocation on that rescan.
6. **Multi-commit behavior is unchanged and the dedupe-key case is defined**: separate-file and ordered same-file allocations across commits still produce one ordered append-only list and one shared pool; two checkpoint events naming the same commit have a stated, tested outcome.
7. **End-to-end read-back through the installed runner**: a real managed-hook pass produces a claim, the backend acknowledges it, and the resulting pool and commit relation are read back and match.

### Why this and not something larger

Because it is the only work item on the critical path, and because every day it is not done, retained trusted evidence crosses the rotation boundary described in Section 3 and becomes permanently unrecoverable — and the map correctly forbids synthesizing it back.

## 8. What This Decision Does Not Settle

Mapped against the map's "Not yet specified" list (#401).

| Map item | Status after this decision |
|---|---|
| Whether a bounded metadata-only Codex+Claude LoongSuite shadow POC is worthwhile after the artifact-first repair | **Answered conditionally.** Deferred behind a named trigger — commission it only when Claude Code entry is actively being pursued (Section 6). Not cancelled. |
| The assurance model for locally measured versus upstream-verifiable Token | **Not settled.** The code already separates the two authorities and makes them mutually exclusive per group, and the spec already states that locally measured Token is unsuitable for billing, chargeback, or security audit (§3). What is missing is a product-level decision: whether the two are ever presented together, and whether the distinction is disclosed to the reader. That is a product/HITL question, not a research question, and it is out of scope for #405. |
| The allocation model when one Token claim spans multiple commits | **Already settled by the active contract and the current code** — no splitting, ordered append-only allocations, one pool keyed by the sorted commit set, shared non-additive projection, non-summable PR rows (Section 5). Recommend striking this from the map's open list. One bounded sub-question survives: the CLI's checkpoint-event dedupe key versus the backend's commit-SHA dedupe key, folded into Section 7's commission. |
| The production ingestion, authentication, retention, and outage semantics if external telemetry is selected | **Not settled, and deliberately so.** External telemetry is not selected. The five preconditions in Section 6 define what would have to be settled first; each remains unverified per #403. |
| The exact formal contract or prototype needed after the owner recommends a route | **Settled** by Section 7. |

Also not settled by this decision, and worth carrying forward explicitly:

- **The remote-control WebSocket transport remains untrusted.** Unchanged non-goal (§16). Needs its own exact transport, success, and turn-identity contract if it is ever to participate.
- **Turns whose trusted evidence has already rotated stay lost.** No route recovers them; synthetic backfill remains out of scope per the map.
- **Migrating the legacy Claude and Kiro compatibility path to the generic pool contract** is named as future work in the architecture document but is not authorized or scoped here, and Section 2 shows it cannot be lifted as-is because it depends on a cwd comparison.

## 9. Spec / Code Drift Surfaced

Per the repository's Source-of-Truth rule, the code wins and the drift is surfaced rather than propagated. None of these blocks the recommendation; all should be corrected when the Section 7 amendment is written.

1. **The active spec describes only one of the two implemented wrapper grammars.** Section 5 of the 2026-08-11 design describes an inline wrapper that binds the result to an identifier and passes it to `text(JSON.stringify(...))`. The code implements that one (`claims_v2.go:45`) **and** an older shape whose result is not bound at all (`claims_v2.go:44`). A reader of the spec alone would not know the second shape exists. The amendment should enumerate the full set.

2. **The older wrapper grammar hard-codes the identifier `patch`.** Verified here by executing the two compiled patterns from `main` against six candidate bodies: the older direct-await shape matches only when the patch variable is literally named `patch`; the same shape with any other identifier matches neither pattern. The #404 artifact's characterization of the grammar as identifier-general is accurate for its *recommendation* but not for the older pattern as currently implemented. Worth stating in the amendment so the enumerated set is honest about what it actually accepts.

3. **The durable ledger has no persisted Token-source column.** The wire type carries `token_source` (`client/attribution.go:83`) and the spec presents it as an explicit, mutually exclusive per-group authority (§3), but the claim-group entity has no such field (`backend/ent/schema/attribution_claim_group.go`) — the backend re-derives it from whether the local usage array is non-empty (`attributionclaim/service.go:465` `tokenSourceForGroup`). Today this is harmless because exactly two sources exist and they are structurally distinguishable. It becomes a correctness problem the moment a third source appears, which is precisely what tool-neutrality would introduce. This is concrete evidence for the Section 1 claim that a tool-neutral envelope is a durable-schema change, and it is Claude gate 4 in Section 2.

4. **The sessionless attribution spec's "checkpoint window binding" is superseded for Codex but still reads as the first-version main contract.** It defines commit binding as attaching usage observed between the previous checkpoint and the current commit — a temporal rule the map now forbids on any accepted path. The v2 design replaced it for Codex with deterministic mutation and Git-content proof. The older spec is a legitimate historical design record and should not be rewritten, but the amendment should state the supersession relationship explicitly so no future reader treats window binding as available.

5. **The legacy compatibility path uses a cwd equality heuristic.** `sameWorkspacePath` in the Claude JSONL parser, and the equivalent workspace-key matching in the Kiro sources, select records by comparing recorded working directory to workspace root. That is correct for the legacy tool-usage-event surface, which makes no commit claim; it is disqualifying for anything Commit-bound. Recording this prevents a future tool-neutral effort from reaching for the nearest existing parser.

## 10. Verification Performed For This Document

- Read both prior research artifacts in full from their branches: the LoongSuite envelope artifact at `origin/research/loongsuite-evidence-envelope` (commit `c32c189b`) and the Codex 0.149.1 failure artifact at `origin/research/codex-01491-commit-allocation-failure` (commit `9fbe1425`).
- Read the current attribution implementation at `main` `36bb67b1`: `ae-cli/internal/attributionlocal/{claims_v2.go, claims_v2_delivery.go, helpers.go, scanner.go, types.go, claude_jsonl.go}`, `ae-cli/internal/hooks/{v2_scan_progress.go, sync_task.go, background_runner.go}`, `ae-cli/internal/client/attribution.go`, `backend/internal/attributionclaim/service.go`, `backend/internal/attributionpool/service.go`, `backend/internal/activity/v2_sql.go`, and the four attribution ent schemas.
- Read the governing spec `2026-08-11-codex-commit-token-attribution-v2-design.md` sections 1–9, 13, and 16; surveyed `2026-08-05-codex-token-attribution-ledger-poc-design.md`, `2026-05-13-sessionless-local-tool-attribution-design.md`, `2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md`, and `2026-03-26-session-pr-attribution-design.md`; read the attribution sections of `docs/architecture.md`.
- Read issues #401, #403, #404, #405 and the convergence map #393. #393 constrains architecture-backlog children only; it neither overlaps nor constrains this decision, and its children are execution tasks rather than decision tickets.
- **Confirmed the #404 repair has not landed:** `ae-cli/internal/hooks/v2_scan_progress.go:14` still holds scanner-progress version 5, and `ae-cli/internal/attributionlocal/claims_v2.go:44-45` still declares only the two older wrapper patterns.
- **Independently re-derived the #404 root cause** by extracting the two compiled patterns from `main` and running them, outside the repository, against six candidate wrapper bodies. Result: the older direct-await shape matches only with the literal identifier `patch`; the inline `JSON.stringify` shape matches; and the current Codex 0.149.1 three-statement shape, the inline-argument-plus-direct-result shape, and the property-access shape all match neither pattern. This reproduces #404's conclusion at the parser seam and adds the identifier-sensitivity finding in Section 9 item 2.

## Primary Sources

- [#401 wayfinder: find a reliable Commit-bound Token direction](https://github.com/LichKing-2234/ai-efficiency/issues/401)
- [#403 research: determine LoongSuite's exact evidence envelope](https://github.com/LichKing-2234/ai-efficiency/issues/403)
- [#404 research: explain why current patch turns produce no commit allocation](https://github.com/LichKing-2234/ai-efficiency/issues/404)
- [#405 research: recommend the smallest viable Commit-bound Token route](https://github.com/LichKing-2234/ai-efficiency/issues/405)
- `docs/research/2026-08-26-loongsuite-exact-evidence-envelope.md` on `origin/research/loongsuite-evidence-envelope`
- `docs/research/2026-08-26-codex-01491-commit-allocation-failure.md` on `origin/research/codex-01491-commit-allocation-failure`
- `docs/superpowers/specs/2026-08-11-codex-commit-token-attribution-v2-design.md`
- `docs/superpowers/specs/2026-05-13-sessionless-local-tool-attribution-design.md`
- `docs/architecture.md`
- Current implementation at `main` `36bb67b1`, as enumerated in Section 10
