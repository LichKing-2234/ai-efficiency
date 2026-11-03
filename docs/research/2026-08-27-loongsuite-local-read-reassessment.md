# LoongSuite Local-Read Reassessment

Status: decision-ready research for wayfinder #405. No production integration, no code change.
Date: 2026-08-27
Ticket: [#405](https://github.com/LichKing-2234/ai-efficiency/issues/405), parent map [#401](https://github.com/LichKing-2234/ai-efficiency/issues/401)
Supersedes the framing of: `docs/research/2026-08-26-loongsuite-exact-evidence-envelope.md` (#403, commit `c32c189b`)

**Source snapshot.** `alibaba/loongsuite-pilot@dea3c6b4b26ac19178c3946a3f5aa45f39b466f4`
(`main`, 2026-08-27 11:21 +0800), latest release `v1.5.2` (2026-08-24).
Prior research (#403) pinned `be9877ec`; findings below are re-verified against the newer tree,
and differences from #403 are called out explicitly.

**Method and its limits.** I read the Pilot repository (`docs/`, `src/`, `assets/hooks/`) and
Alibaba Cloud's public documentation. I did **not** install Pilot, did **not** run it, and did
**not** inspect a real `~/.loongsuite-pilot/logs/output/*.jsonl` file. Every statement about
runtime behaviour below is derived from source or documentation, and is labelled as such. Where a
question can only be settled by running the thing, I say so rather than guessing.

## Evidence Classification

- **Documented fact** — stated by current first-party documentation.
- **Observed source fact** — implemented in first-party source at the pinned commit. It can change.
- **Inference** — a conclusion drawn from the above that still needs a live fixture.
- **Unverified** — I could not establish it from primary sources. Not filled in with plausible reasoning.

---

## What The Prior Assessment Got Wrong

The repo owner's challenge is substantially correct. #403 evaluated LoongSuite as a remote
telemetry-ingestion product and then priced the route accordingly. In the local-read shape —
`ae-cli` reads `~/.loongsuite-pilot/logs/output/*.jsonl` directly, no SLS, no OTLP, no cloud
account — most of that critique is inapplicable.

### Objections that do NOT survive the reframing

| #403 objection | Why it does not apply |
|---|---|
| SLS Logstore retention (1–3650 days) is not a durable evidence SLA | No SLS in this shape. |
| RAM least-privilege policy, `log:GetLogStoreLogs` scoping | No cloud identity is involved at all. |
| OTLP/CloudMonitor ingress ports `8090`/`80`, unproven TLS on the evidence hop | Nothing leaves the machine. There is no network hop to encrypt. |
| ARMS `SearchTracesByPage` availability, exact-tag indexing, workspace read-back | No remote query surface is used. |
| Remote ACK / replay / dead-letter contract, `logs/otlp-failed` retention gap, HTTP flusher in-memory requeue | These are properties of the SLS, HTTP, and OTLP flushers. The JSONL writer is not one of them. |
| Trace export double-counts Token across parent AGENT and child LLM spans | **Observed source fact.** That aggregation happens in the OTLP conversion path (`src/flushers/otlp-trace-flusher.ts`), not in the JSONL record stream. A JSONL consumer reads flat `llm.response` records and never sees the synthesized parent aggregate. |
| Claude Code includes OAuth `user.email` and it must be stripped | **Miscarried.** That fact belongs to *Claude Code's own* native OTel exporter, not to Pilot. Pilot sets `user.id` itself: `assets/hooks/agent-event-normalizer.mjs:291-298` resolves it to the configured `--userId` or, failing that, `os.hostname()`. Pilot's Claude adapter never reads an email — `grep -i email` over `assets/hooks/claude-code-hook-processor.mjs` and `assets/hooks/claude-code/*.mjs` returns nothing. |
| "Do not introduce a production LoongSuite dependency until remote ACK/replay is designed" | The dependency in the local-read shape is a different animal — a *file-format* coupling, not a *service* coupling. It is real, but it is a different risk with a different mitigation. See Finding 1. |

### Objections that DO survive — and are stronger than #403 stated

1. **No commit field.** Confirmed against the schema and against the TypeScript type. Strengthened: a
   `commitHash`-bearing type exists in the codebase and is dead. See Finding 2.
2. **Claude Code's exported turn ID is collector-derived.** Confirmed, and it is worse than #403
   described: the counter is seeded from Pilot's *own persisted state file*, so the same
   `<session-id>:t<N>` string can be reissued for a different turn. See Finding 3.
3. **Kiro cannot supply Token.** Confirmed, unchanged, and its turn ID additionally depends on a
   30-second wall-clock gap heuristic — categorically forbidden by #401. See Finding 3.
4. **Pilot's Codex adapter reads the same transcript facts `ae-cli` already reads.** Confirmed, and
   this is the decisive finding: in the local-read shape Pilot is not an independent witness, it is
   a *lossy re-serialization* of a file `ae-cli` opens anyway. See Finding 4.
5. **Content capture defaults are permissive.** Confirmed and sharpened: `mask.mode` defaults to
   `none` *and* `captureMessageContent` defaults to `true`. See Finding 6.

### The one thing #403 under-weighted

#403 dismissed Pilot's supported-agent table as "Codex, Claude Code, Kiro". At the pinned commit it
covers **21 agents** (`docs/overview.md:23-45`) — Cursor, Cursor CLI, Grok Build, Hermes, MiMo Code,
OpenClaw, OpenCode, Pi Coding Agent, five Qoder variants, two Qwen variants, Wukong, WorkBuddy — of
which all but Kiro CLI are marked `Token Usage: Yes`. That is a genuinely larger tool-neutrality
surface than the prior assessment credited, and it is the strongest argument for the route.
Finding 4 quantifies what it would actually buy.

---

## Finding 1 — Local JSONL As A Consumable Artifact

### On-disk contract

**Documented fact.** Local JSONL output "is enabled by default"
(`docs/local-jsonl-output.md:5`) and stays on even with no backend configured: "If no remote backend
is configured, JSONL remains enabled by default so collected data is still visible locally"
(`docs/overview.md:91`). The owner's premise holds — the local-read shape needs no cloud account.

**Documented fact.** Path, naming, and record granularity (`docs/local-jsonl-output.md:9-19`):

```text
~/.loongsuite-pilot/logs/output/<agent-id>-YYYY-MM-DD.jsonl
```

One normalized event per line. Native JSON types are preserved — "token counts remain numbers,
flags remain booleans" (`docs/local-jsonl-output.md:21-24`). Configurable via `jsonl.enabled`,
`jsonl.outputDir`, `jsonl.rotateDaily`, or `JSONL_ENABLED` / `JSONL_OUTPUT_DIR`
(`docs/local-jsonl-output.md:28-59`).

### Retention and eviction

**Documented fact** (`docs/local-jsonl-output.md:44-52`, `docs/configuration.md:140-155`):

- Global retention default `outputDays: 7`.
- A single output file over **512 MiB** may be removed once it is older than 2 days.
- If `logs/output` exceeds **2 GiB**, older dated files are removed until the directory is back under.
- Today's file is never removed by retention cleanup; size-pressure cleanup also spares yesterday's.

**Inference.** A consumer that reads at least once every ~24 h is safe under normal conditions; the
7-day floor gives a wide recovery margin. Size-pressure eviction is the only mechanism that can
destroy evidence inside the retention window, and it cannot touch today or yesterday. For an
`ae-cli` that syncs on a commit or on a periodic timer, this is adequate — provided the consumer
keeps a durable byte-offset cursor and treats a vanished file as a gap, not as a zero.

### Is there a contract for third-party consumers? No.

**Observed/documented fact.** Every documented use of the JSONL files is human inspection.
`docs/overview.md:86` describes the destination as "Local backup, debugging, and simple offline
inspection." `docs/local-jsonl-output.md:61-76` and `docs/masking.md:125-136` show `ls` and
`tail -f`. `docs/agents.md:208-213` shows the same. There is no documented programmatic consumer
API, no local query endpoint, no stability statement, and no schema-version field on the record.

**Observed source fact.** The record type `src/types/events.ts:39-128` carries **no** version,
schema, or producer-version field. Grepping the tree for `schema_version` / `schemaVersion` finds
versions on the SLS *failure* log (`src/flushers/sls-failure-log-writer.ts:28`), on deployment
markers (`src/deployment/directory-plugin-strategy.ts:27`), on the Kiro hook queue
(`assets/hooks/kiro-cli/state.mjs:416`), and on the local-worker store
(`src/local-workers/instance-store.ts:11`) — but never on an emitted output event. A consumer
cannot detect which format version it is reading.

### How risky is this coupling, plainly?

**Observed fact — churn rate.** Since the repo's first public commit (2026-06-12), there have been
**13 releases in ~11 weeks** (`v1.0.1` → `v1.5.2`, roughly one every five days).
`docs/output-event-schema.md` has been modified in **15 commits** and `src/types/events.ts` in
**10 commits** over the same window. The most recent schema-doc change, PR #289 on 2026-08-20
("standardize workspace and Git context across agents"), touched exactly the `git.*` / `workspace.*`
fields — i.e. the fields most relevant to this use case changed one week before this assessment.

**Assessment.** This is a young, fast-moving, actively restructured internal format with no version
stamp and no compatibility promise, published under Apache-2.0 by a vendor whose documented purpose
for the artifact is debugging. Reading it is not like consuming an API; it is like consuming another
program's log file. That is tolerable for a shadow POC or an analytics side-channel. It is a poor
foundation for a **fail-closed** attribution system, where a silently renamed field must produce a
refused claim rather than a wrong one — and with no version field, `ae-cli` cannot even tell that
the format moved. Any adoption must therefore pin an exact Pilot version, assert the presence of
every field it depends on per record, and fail closed on any record it cannot fully validate.

---

## Finding 2 — The Normalized Field Set, And The No-Commit-Field Claim

### The claim holds. Confirmed.

**Documented fact.** `docs/output-event-schema.md#field-reference` (lines 41-100) enumerates the
complete field set. There is **no** `commit`, `commit.sha`, `commit_hash`, `revision`, `checkpoint`,
or equivalent. The repository-context fields are exactly four:

| Field | Level | Description (verbatim intent) |
|---|---|---|
| `git.domain` | Recommended | Git hosting domain for the active workspace |
| `git.repo` | Recommended | Repository name or URL |
| `git.branch` | Recommended | Active branch |
| `workspace.current_root` | Recommended | Git top-level directory |
| `workspace.path` | Recommended | Absolute process cwd, independent of git |

**Observed source fact — the type agrees.** `src/types/events.ts:113-123` declares exactly
`git.repo`, `git.branch`, `git.repo_root`, `git.domain`, `workspace.current_root`, `workspace.path`.
Nothing more.

**Observed source fact — and it is worse than "absent".** These fields are not even a *turn-time*
record of repository state. `src/normalization/enrich-git-context.ts:19-35` fills them by calling
`inferGitContext(probeDir)` at **normalization** time, and `src/utils/git-context.ts:41-64` implements
that by shelling out live:

```
git -C <dir> rev-parse --show-toplevel
git -C <dir> rev-parse --abbrev-ref HEAD
git -C <dir> config --get remote.origin.url
```

with results cached for `GIT_CONTEXT_TTL_MS = 30_000` (`src/utils/git-context.ts:7`). So
`git.branch` describes whatever branch was checked out when Pilot got round to normalizing — up to
30 seconds stale on top of that — not the branch the turn ran on. `ae-cli` must never treat it as a
turn-time fact, and #401 forbids branch equality as a join anyway.

### There is a dead commit-bearing type in the tree

**Observed source fact.** `src/types/events.ts:192-200` declares:

```ts
/** Git hook event from post-commit / pre-push hooks. */
export interface GitHookEvent {
  eventType: 'post-commit' | 'pre-push';
  repoRoot: string;
  commitHash: string;
  branchName: string;
  changedFiles: string[];
  timestamp: number;
}
```

`grep -rn "GitHookEvent" src/` matches **only** this declaration. Nothing imports it, nothing
produces it, no input implements git hooks, no output field carries it, and it is absent from the
schema doc.

**Inference.** Someone scoped a git-hook input and did not build it. This is an interesting signal
about Pilot's roadmap and nothing more. It is not a capability, and it must not be planned against.
Separately, `src/pipeline/input/qoder-api/qoder-api-input.ts:698` does emit a real `commit_hash` —
but that comes from polling Qoder's *enterprise cloud API* for per-commit AI-authored line counts.
It is not local, not token-bearing, not agent-neutral, and not part of the local-read shape.

### Tool arguments and results ARE capturable — and this is the interesting part

**Documented fact.** `gen_ai.tool.call.arguments` and `gen_ai.tool.call.result` are typed `json`,
level **Opt-In** (`docs/output-event-schema.md:85-86`), gated per agent by `captureMessageContent`
(`docs/agents.md:203`).

**Observed source fact — Claude Code.** `assets/hooks/claude-code-hook-processor.mjs:1059` emits
`'gen_ai.tool.call.arguments': toJsonValue(toolBlock.input || {})` — the tool's raw input object,
unmodified. For an `Edit` call that is `{file_path, old_string, new_string}`; for `Write`,
`{file_path, content}`. **So yes: with content capture on, a Claude Code file edit carries the exact
path and the exact new content.**

**Observed source fact — Codex.** `assets/hooks/codex/tool-arguments.mjs:23-32` and
`src/inputs/codex-transcript/codex-transcript-extractor.ts:682` normalize an `apply_patch` call into
`{command: <patch text>}`, emitted at
`src/inputs/codex-transcript/codex-transcript-builder.ts:262`. The full
`*** Begin Patch … *** End Patch` envelope is preserved verbatim.

**Does this deliver edge 2 after all? No — and Finding 4 explains why it is actually a downgrade.**
Three reasons:

1. **It is content, not a commit.** Knowing that a turn wrote bytes `X` to path `P` does not prove
   those bytes landed in commit `C`. They may be overwritten before the commit, split across
   commits, reverted and re-applied, or independently typed by a human. Turning content into a
   commit binding requires replaying the edit against the commit's parent tree and checking the
   result against the commit — which is a git operation on the local repo, not something a
   telemetry record can carry.
2. **`ae-cli` already has this data, from the same file, in better form.** See Finding 4.
3. **Pilot drops the stronger half.** `grep -rn "patch_apply_end\|content_sha256"` across
   `src/inputs/codex-transcript/`, `src/inputs/codex-aborted-turn/`, and `assets/hooks/codex/`
   returns **nothing**. Pilot does not parse Codex's `patch_apply_end` event, so it never carries
   `changes[path].content_sha256` — the *post-application content hash* that `ae-cli` currently uses
   as one of its two mutation-harvest sources (`ae-cli/internal/attributionlocal/claims_v2.go:571-581`).
   Routing Codex mutation evidence through Pilot would strictly lose information.

---

## Finding 3 — Tool-Neutrality, Concretely

The question that matters for #401 is not "does the field exist" but "is the turn identity **native**
(reversible to a first-party identifier) or **collector-derived** (invented by Pilot)".

### Codex — native identity survives. The strongest case.

**Observed source fact.** `src/inputs/codex-transcript/codex-transcript-builder.ts:43,48,51`:

```ts
const turnId = `${turn.sessionId}:${turn.transcriptTurnId}`;
// ...
'gen_ai.turn.id': turnId,
'agent.codex.transcript_turn_id': turn.transcriptTurnId,
```

`transcriptTurnId` is Codex's own `turn_context.payload.turn_id`. The composite is reversible, and
the native ID is *also* carried verbatim in a dedicated extension field. Event IDs are deterministic
(`hashId([sessionId, transcriptTurnId, kind, index], 32)` at builder lines 63, 88, and throughout),
so a consumer can dedupe by `event.id` across restarts and replays.

Populated per turn: session, native turn, provider, model, per-response `gen_ai.usage.*`,
finish reasons, tool call/result IDs, tool arguments (opt-in, incl. the patch), `agent.codex.cwd`,
`agent.codex.turn_status` when interrupted.

### Claude Code — collector-derived, and stateful. Not usable as an authorization.

**Observed source fact.** `assets/hooks/claude-code-hook-processor.mjs:863`:

```js
const turnId = `${sessionId}:t${turnIndex + 1}`;
```

`turnIndex` is `baseTurnCount + i` where `baseTurnCount = state.turn_count || 0`
(lines 744, 769), and `state.turn_count` is Pilot's own persisted counter, advanced at line 838.
The native `promptId` is used to *split* turns (`assets/hooks/claude-code/transcript-parser.mjs:15`)
and to key a turn-context file (`assets/hooks/claude-code/tool-context.mjs:46-49`), but it is
**never emitted as an output field**. The only `agent.claude-code.*` field on the record is
`cwd` (line 889).

Three consequences, all disqualifying under #401:

- **The ID is not reversible.** A `<session>:t3` cannot be mapped back to a Claude `promptId`.
- **The ID can be reissued.** If Pilot's state file is lost — reinstall, `~/.loongsuite-pilot`
  cleanup, data-dir change — `turn_count` resets to 0 and the *next* turn in a resumed session is
  emitted as `:t1` again. Line 748's `isFirstRun` guard then exports only `turns.slice(-1)`
  (line 750), suppressing the historical turns but **not** preventing the counter collision.
- **There is a documented fallback that guesses.** Lines 754-760: when the transcript omits
  `promptId`, the hook's `stopPromptId` is substituted — guarded to the unambiguous single-turn case,
  which is careful engineering, but it is still an inference rather than a native fact.

**Observed source fact — Claude event IDs are random.** `assets/hooks/claude-code-hook-processor.mjs`
lines 901, 955, 987, 1051, 1067, 1114, 1130 all set `'event.id': crypto.randomUUID()`. Unlike Codex,
a Claude record cannot be deduped by `event.id` across a re-export. `src/normalization/entry-builder.ts:82`
shows the same default (`?? uuidv4()`) for entries that do not supply one.

**Assessment — can a collector-derived ID ever authorize a claim in a fail-closed system?**
No. A fail-closed system must be able to refuse. Refusal requires the ability to *detect* that the
identity is wrong or reused. A monotonic counter over a resettable local state file offers no such
detection: `<session>:t1` after a reinstall is byte-identical to `<session>:t1` before it, and no
field on the record distinguishes them. That is precisely the shape of a silently-wrong claim. A
collector-derived ID is acceptable as an *index* into evidence whose authority comes from elsewhere;
it can never be the authority.

### Kiro CLI — disqualified twice over.

**Documented fact.** "Token usage is not exposed by the source" (`docs/agents.md:31`);
`docs/overview.md:31` marks Kiro CLI `Token Usage: No`.

**Observed source fact.** `assets/hooks/kiro-cli-hook-processor.mjs:744,890` emit
`'kiro.token_source': 'unavailable'` — the adapter deliberately refuses to synthesize zero. That is
correct behaviour and should be respected.

**Observed source fact — the turn ID uses a wall-clock heuristic.** Lines 522-524:

```js
const turnNumber = loadTurnCount(cwd) + 1;   // per-cwd counter file
saveTurnCount(cwd, turnNumber);
const turnIdBase = `${sessionId}:t${turnNumber}`;
```

and lines 541-545, 623: runs are split by `RUN_GAP_MS = 30_000` — a 30-second inter-step timestamp
gap — producing `${turnIdBase}:r${runIndex}`. #401 forbids timestamp heuristics outright. Kiro stays
outside Token attribution, exactly as #403 concluded.

### The other 18 agents

**Documented fact.** `docs/overview.md:23-45` lists 21 agents, 20 marked `Token Usage: Yes`.
**Unverified.** I did not audit turn-identity nativeness for the other 18 adapters. Spot checks show
the pattern is mixed and mostly unfavourable: `assets/hooks/qwen-code-cli-hook-processor.mjs:407`
uses the same `${sessionId}:t${N}` counter as Claude; `assets/hooks/qoder-hook-processor.mjs:621`
uses `crypto.randomUUID()` for the turn ID outright; `assets/hooks/grok-build-hook-processor.mjs:504`
prefers the native `promptId` and falls back to `${sessionId}:turn`. Any expansion beyond Codex would
need this audit done per agent, and on the evidence Codex is the exception rather than the rule.

---

## Finding 4 — What `ae-cli` Would No Longer Need To Write

This is the strongest argument FOR the route, so it deserves real numbers.

**Observed source fact — measured LOC in this repo.**

| Bucket | Non-test LOC | Test LOC |
|---|---|---|
| Per-agent token/turn parsing (a collector could plausibly absorb) | **1,820** | ~369 |
| Per-agent parsing for request-ID / failure evidence | 425 | 160 |
| Commit evidence + claim machinery + cursor/spool/delivery (a collector cannot absorb) | **2,591** | ~3,915 |
| **Total** | **4,836** | **4,444** |

Bucket 1 breaks down as: `codex_jsonl.go` 190, `codex_sqlite.go` 219, `claude_jsonl.go` 89,
`kiro_json.go` 83, `kiro_cli_sqlite.go` 133, `kiro_ide.go` 237, `types.go` 148,
`workspace_path.go` 71, `collector/{claude,codex,kiro,types}.go` 411, plus ~239 lines of token
parsing inside `claims_v2.go`. All under
`/Users/huangshen/Desktop/AI_Native/GitHub/LichKing-2234/ai-efficiency/ae-cli/internal/`.

So: **~1,820 LOC, 38% of non-test code across the two packages** — and it is the churn-prone 38%,
covering six distinct on-disk formats, two of which (`kiro_cli_sqlite.go`, `kiro_ide.go`, 370 LOC
combined) have **no test file at all**. `types.go`'s `LocalToolUsageEvent` is already the normalized
shape, which means the integration seam is clean: substitute the producers, leave `scanner.go` and
`sync.go` untouched.

That is a genuine win and it should not be waved away. Four things cut against it.

**(1) The Codex portion cannot actually be dropped.** `ae-cli`'s v2 claim path
(`ae-cli/internal/attributionlocal/claims_v2.go`) does not merely need a token total. It needs
`turn_context.payload.turn_id`, `payload.thread_id`, `session_meta`, `compacted` markers, and — 
critically — per-turn **delta** token accounting that self-validates against the cumulative sample
(`parseV2TokenUsageValues`, `v2TokenUsageDeltaMatches`: it rejects non-integral floats, requires
`cacheRead <= input`, `total == input + output`, and requires `cumulative − previous == delta`,
setting `invalid_local_usage` otherwise). Pilot emits a per-response `gen_ai.usage.*` snapshot with
no cumulative counterpart, so the cross-check that currently makes a bad Codex sample *fail closed*
would have nothing to check against. Adopting Pilot for Codex means giving up a working
fail-closed validator.

**(2) It costs the strongest mutation source.** As Finding 2 established, Pilot never parses
`patch_apply_end` and therefore never carries `content_sha256`. `ae-cli` currently harvests
mutations from **two** sources — the `apply_patch` tool arguments *and* the post-application content
hashes (`claims_v2.go:525-540` and `:571-581`). Routing through Pilot leaves only the first,
forcing every claim through patch replay with no independent hash to corroborate it.

**(3) The commit machinery — the hard half — is untouched.** The ~394 LOC of structured-mutation
code (`v2StructuredPatchInput` 26, `v2PatchMutations` 86, `applyV2PatchBlock` 68,
`introducedV2Mutations` 45, `canonicalClaimPath` 25, `reverseV2PatchBlock` 15, `v2MutationDigest` 9,
`gitShowClaimFile` 6, plus types and harvest branches) works by replaying the agent's own declared
patch against `git show <commit>^:<path>`, hashing the result, diffing it against
`git show <commit>:<path>`, and emitting **only a SHA-256 digest** — never a path, never content.
`missing_structured_mutation` and `invalid_structured_mutation` (`claims_v2.go:662,664`) are the
gates. That is the mechanism that satisfies #401's edge 2, it requires the local git repository and
the raw patch text at scan time, and no telemetry collector carries either. Plus `sync.go` (603),
`scanner.go` (365), `claims_v2_delivery.go` (247) — cursors, spool, replay, dead-letter, ack
reconciliation — all stay.

**(4) The parsing is not deleted, it is traded.** Dropping 1,820 LOC of Go that reads six formats
means adding a reader for a seventh — an unversioned, undocumented, fast-churning JSONL format,
plus per-record field-presence validation, plus a byte-offset cursor over rotating files, plus
eviction detection, plus a per-agent nativeness policy table. It also converts a self-contained CLI
into one that requires a separately installed background daemon.

**Honest summary.** The win is real and it is about **breadth**, not depth: Pilot's 20
token-bearing adapters vs. `ae-cli`'s three. If the product goal were "token usage per developer per
repo, analytics-grade", Pilot would be an excellent buy. For **commit-bound, fail-closed
attribution** it is a bad trade on the agents `ae-cli` already supports — measurably lossy on Codex,
identity-disqualified on Claude Code, token-disqualified on Kiro. Its value is on the agents
`ae-cli` does *not* support, and there it delivers edge 1 only.

---

## Finding 5 — Failure Modes In The Local-Read Shape

The test is: does each failure produce **no claim** (acceptable) or a **wrong claim** (not acceptable)?

| Failure | Behaviour | Verdict |
|---|---|---|
| Pilot not installed | `~/.loongsuite-pilot/logs/output/` absent. Consumer reads nothing. | **Fail closed.** Lossy, safe. |
| Pilot installed, service stopped | Hooks are fail-open by contract: `assets/hooks/README.md:7,23`; `docs/agent-onboarding.md:311,367` ("Hook and plugin code must fail open so telemetry cannot block the source agent"); `assets/hooks/claude-code-loongsuite-pilot-hook.sh:12,53-58,158-170` prints `{}` and `exit 0` on every error path. Events are simply not written. | **Fail closed.** Silent from the agent's side; the consumer sees a gap. |
| Pilot stopped mid-session, restarted | **Documented fact** (`docs/codex-aborted-turn-recovery.md:38-42`): "Existing transcript files are baselined on the first enablement; historical aborted turns are not replayed. A transcript created while Pilot is stopped is also baselined on restart." No backfill by design. | **Fail closed**, lossy by design. |
| Claude Code specifically, after a Pilot reinstall | `state.turn_count` resets to 0. The `isFirstRun` guard (`claude-code-hook-processor.mjs:748-751`) suppresses history but does **not** prevent `<session>:t1` being reissued for a different turn in a resumed session. Combined with `crypto.randomUUID()` event IDs, a consumer cannot dedupe or detect the collision. | **RISK OF SILENTLY WRONG.** Disqualifying on its own. |
| JSONL evicted by size protection | File disappears. A consumer with a byte-offset cursor sees the file gone. **Inference:** if it treats a missing file as "no events", it under-claims — fail closed. If it silently re-baselines and treats the surviving partial file as complete, it can attribute a commit to the wrong turn. This is entirely a property of the consumer, not of Pilot. | **Fail closed only if `ae-cli` is written to make it so.** Must be an explicit requirement. |
| Agent version Pilot does not recognize | **Unverified.** Pilot declares a minimum only for OpenClaw (`>=2026.5.12`, `docs/overview.md:47`) and states it "never launches the OpenClaw CLI to determine its version" (`docs/agents.md`). For Codex and Claude Code I found no version gate at all: the parsers read whatever fields are present and omit what is missing. **Inference:** an unrecognized agent version most likely yields records with fields silently absent rather than an error — e.g. a turn with no `gen_ai.usage.*`. A consumer that treats missing usage as zero would produce a wrong claim; one that treats it as a gap would not. | **Depends entirely on consumer strictness.** Must assert every required field per record. |
| Adapter/schema version skew (Pilot upgraded under `ae-cli`) | No version field on the record (Finding 1). `ae-cli` cannot detect that the format changed. A renamed or re-semanticized field reads as absent or, worse, as a plausible wrong value. | **RISK OF SILENTLY WRONG.** Only mitigable by pinning the Pilot version and validating every field per record. |

**Overall.** Most failure modes are fail-closed-and-lossy, which #405 accepts. But two are not:
Claude Code turn-ID reissue, and undetectable schema skew. Both are structural, both stem from the
absence of a stable identity/version contract, and neither can be fixed on `ae-cli`'s side —
only defended against by pinning and refusing.

---

## Finding 6 — Privacy In The Local-Read Shape

### Nothing leaves the machine

**Documented fact.** With only JSONL enabled and no SLS/HTTP/OTLP backend configured, Pilot writes
to `~/.loongsuite-pilot/logs/output/` and nothing else (`docs/overview.md:80-91,101-113`). The
`logs/sls-failed-logs/` and `logs/otlp-failed` concerns from #403 arise only when those flushers are
configured. The remote-egress objection genuinely does not apply.

### But local capture defaults to maximal

**Documented fact.** `mask.mode` defaults to `none`: "Do not mask fields. This is the default when
no mask mode is configured" (`docs/masking.md:43`).

**Observed source fact.** `captureMessageContent` defaults to **`true`**:
`src/core/config-loader.ts:461` — `parseOptionalBool(policy.captureMessageContent) ?? true` — and
`assets/hooks/agent-event-normalizer.mjs:352-359` returns `true` for any unparseable value.
`docs/agents.md:191-195` shows `false` only in an *example*, not as a default.

**Inference.** A stock `curl | bash` install (`docs/installation.md:15-19`) writes, in plaintext,
to a local file: full prompts, full model outputs, full reasoning, full tool arguments, and full
tool results — i.e. every file the agent read and every edit it made, unmasked.

### What content capture removes when disabled

**Observed source fact.** `assets/hooks/agent-event-normalizer.mjs:7-40` — with
`captureMessageContent: false`, `removeContentFields` strips `gen_ai.input.messages`,
`gen_ai.input.messages_delta`, `gen_ai.output.messages`, `gen_ai.tool.call.arguments`,
`gen_ai.tool.call.result`, `agent.content`, `agent.inline_diff_message`, and any key whose last
dotted segment is `attachments`, `content`, `input`, `new_string`, `old_string`, `prompt`,
`result_json`, `text`, `toolUseResult`, `tool_input`, `tool_output`, `tool_results`.

**Documented fact.** `workspace.*` and inferred `git.*` survive content-capture-off
(`docs/output-event-schema.md:102`), as do model names, token counts, and durations
(`docs/masking.md:113`).

### What would still have to be stripped for a metadata-only envelope

**Inference**, from the field reference and the adapters:

- `workspace.path` and `workspace.current_root` — absolute paths carrying the OS username. Replace
  with `ae-cli`'s existing canonical repo identity/digest.
- `host.name`, `host.ip`, `service.name` — machine fingerprinting.
- `user.id` — defaults to `os.hostname()` (`agent-event-normalizer.mjs:291-298`), which is often a
  personal device name. Not an email, contra #403, but still identifying.
- `gen_ai.input.messages_hash` — a content hash; low risk but unnecessary.
- All `agent.*` extension fields except an explicit allowlist; `addSourceAttributes`
  (`agent-event-normalizer.mjs:344-350`) copies **every unmapped source key** into
  `agent.<source>.<key>`, so this namespace is open-ended by construction and cannot be audited
  once and trusted.

**The unavoidable tension.** Edge 2 via tool arguments (Finding 2) requires
`captureMessageContent: true`, which is exactly the setting that puts full prompts, outputs, and
file contents on disk unmasked. Pilot offers no middle setting — no "tool arguments only, no
messages". `captureMessageContent` is all-or-nothing per agent. So the LoongSuite content route to
edge 2 is not merely weak; it is only available at the maximum-exposure privacy setting.

---

## Verdict On LoongSuite In The Local-Read Shape

### What it can supply

- **Edge 1 (turn → Token), for Codex, at parity.** Native `turn_id` survives normalization and is
  additionally carried verbatim as `agent.codex.transcript_turn_id`; `event.id` is deterministic;
  `gen_ai.usage.*` is complete. This is a correct, usable Token-per-turn record.
- **Edge 1, tool-neutrally, for ~20 agents** — including 17 that `ae-cli` does not support at all
  (Cursor, OpenCode, OpenClaw, Qwen, Qoder, Grok Build, WorkBuddy, and others). This is the real
  and only substantial value on offer.
- **A stable normalization vocabulary.** The GenAI-audit field set is a reasonable shape, and
  `ae-cli`'s `LocalToolUsageEvent` is close enough to it that the seam is clean.

### What it cannot supply

- **Edge 2 (turn → commit), for any agent.** No commit, SHA, revision, or checkpoint field exists.
  The only commit-bearing type in the codebase is dead. `git.branch`/`git.repo` are live probes of
  the working directory at normalization time with a 30-second cache — not turn-time facts, and
  forbidden as joins by #401 regardless.
- **A fail-closed turn identity for Claude Code.** `<session-id>:t<N>` is a counter over Pilot's own
  resettable state file, the native `promptId` is never exported, and `event.id` is a random UUID.
  Reuse is possible and undetectable.
- **Any Token for Kiro CLI**, whose turn ID additionally depends on a 30-second time-gap heuristic.
- **A detectable format contract.** No schema version on the record; 13 releases in 11 weeks; the
  `git.*`/`workspace.*` fields restructured one week ago.
- **A privacy-safe path to content evidence.** Tool arguments require
  `captureMessageContent: true`, which is all-or-nothing and also writes full prompts and outputs
  to disk unmasked.

### Under what conditions it would be worth adopting

**Adopt, if and only if all of these hold:**

1. The goal is **edge 1 breadth** — Token per turn across agents `ae-cli` does not parse — and edge 2
   remains entirely owned by `ae-cli`'s existing structured-mutation + `git show` replay.
2. It is added **behind `ae-cli`'s existing `LocalToolUsageEvent` seam** as one more optional
   producer, never replacing the Codex, Claude, or Kiro readers that exist today.
3. `ae-cli` **pins an exact Pilot version**, asserts the presence and type of every field it consumes
   on every record, and refuses the record — no claim — on any mismatch. With no schema version to
   check, per-record validation is the only available detector.
4. Records are accepted **only from agents whose turn identity is native and reversible**. On current
   evidence that is Codex and possibly Grok Build; it is explicitly not Claude Code, Kiro CLI, Qoder,
   or Qwen Code CLI. The nativeness audit must be done per adapter and re-done on every version bump.
5. `captureMessageContent: false` and `mask.mode: all` are **required configuration**, and `ae-cli`
   refuses to consume a Pilot instance configured otherwise. This forecloses the tool-argument route
   to edge 2, which is the correct trade.
6. The JSONL reader keeps a **durable byte-offset cursor**, treats an evicted or truncated file as a
   gap rather than a completion, and dedupes on `event.id` only for agents that generate it
   deterministically.

**Do not adopt** — and this is my recommendation for the near term — if the objective is to fix
`invalid_structured_mutation` / `missing_structured_mutation` on Codex. LoongSuite is orthogonal to
that failure and, on Codex specifically, would make the evidence base *weaker*: it drops
`patch_apply_end.content_sha256` and removes the cumulative-vs-delta token cross-check that
currently makes a bad sample fail closed. Fix the structured-mutation path first, against real
current-version fixtures. LoongSuite is a plausible later answer to "how do we cover the other
seventeen agents", and it is not an answer to "how do we bind a turn to a commit."

---

## Primary Sources

**LoongSuite Pilot** — `alibaba/loongsuite-pilot@dea3c6b4b26ac19178c3946a3f5aa45f39b466f4`
(`main`, 2026-08-27), release `v1.5.2`:

- `docs/output-event-schema.md` — field reference (lines 41-100), event names (11-31), required levels (34-39)
- `docs/local-jsonl-output.md` — default-on (5), path/naming (9-19), config (28-59), retention and disk protection (44-52), privacy notes (78-83)
- `docs/overview.md` — supported agents (23-45), data collected (65-78), output destinations (80-91), local runtime layout (93-113)
- `docs/agents.md` — agent IDs and notes (13-40), content-capture policy (185-206)
- `docs/masking.md` — mask modes and `none` default (38-46), what gets masked (102-113)
- `docs/configuration.md` — retention defaults (140-161)
- `docs/installation.md` — installer and footprint (7-19)
- `docs/codex-aborted-turn-recovery.md` — checkpointing, no-backfill policy, deterministic IDs (18-46)
- `docs/agent-onboarding.md` — fail-open requirement (311, 367), checkpoint rules (263-295)
- `src/types/events.ts` — normalized event type (39-128), dead `GitHookEvent` (192-200)
- `src/normalization/enrich-git-context.ts` — git enrichment at normalization time (19-35)
- `src/utils/git-context.ts` — live `git rev-parse` probe, 30 s TTL cache (7, 41-64)
- `src/core/config-loader.ts` — `captureMessageContent ?? true` (461)
- `src/inputs/codex-transcript/codex-transcript-builder.ts` — Codex turn/event IDs (43-51), tool arguments (262)
- `src/inputs/codex-transcript/codex-transcript-extractor.ts` — `apply_patch` handling (682)
- `src/pipeline/input/qoder-api/qoder-api-input.ts` — remote Qoder `commit_hash` (698)
- `assets/hooks/claude-code-hook-processor.mjs` — turn-count state (744-751), promptId fallback (754-760), turn ID (863), random event IDs (901, 955, 987, 1051, 1067), tool arguments (1059)
- `assets/hooks/claude-code/transcript-parser.mjs` — promptId turn split (15)
- `assets/hooks/kiro-cli-hook-processor.mjs` — turn counter (522-524), `RUN_GAP_MS` (541-545, 623), `kiro.token_source` (744, 890)
- `assets/hooks/codex/tool-arguments.mjs` — `apply_patch` normalization (23-32)
- `assets/hooks/agent-event-normalizer.mjs` — content field sets (7-40), `resolveUserId` (291-298), content policy (310-342), `addSourceAttributes` (344-350)
- `assets/hooks/claude-code-loongsuite-pilot-hook.sh` — fail-open contract (12, 53-58, 158-172)
- `assets/hooks/README.md` — fail-open principle (7, 23)

**Alibaba Cloud:**

- [AI Coding Agent integration](https://help.aliyun.com/zh/cms/cloudmonitor-2-0/ai-application-access-ai-coding-agent/) — supported-agent capability matrix, Kiro Token unavailability, masking defaults

**This repository** (`/Users/huangshen/Desktop/AI_Native/GitHub/LichKing-2234/ai-efficiency/`):

- `ae-cli/internal/attributionlocal/claims_v2.go` — token validation, structured mutation, patch replay, gap reasons (525-540, 571-581, 662-664, 762-1053)
- `ae-cli/internal/attributionlocal/claims_v2_delivery.go` — uploadability gate (72)
- `ae-cli/internal/attributionlocal/helpers.go` — `isPatchTool`, `StableCommitPatchID` (23-30)
- `ae-cli/internal/attributionlocal/{codex_jsonl,codex_sqlite,claude_jsonl,kiro_json,kiro_cli_sqlite,kiro_ide,types,workspace_path,scanner,sync}.go`
- `ae-cli/internal/collector/{claude,codex,kiro,collector,types}.go`
- `docs/research/2026-08-26-loongsuite-exact-evidence-envelope.md` (#403, at `origin/research/loongsuite-evidence-envelope`, `c32c189b`)
