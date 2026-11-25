# Claude Code mutation evidence for commit-bound Token attribution (#405)

Research date: 2026-08-27. Scope: can Claude Code supply **edge 2** (turn -> commit) deterministically and
fail-closed, at the same evidentiary bar the Codex v2 path already clears?

Every claim below is tagged:

- **[DOC]** documented fact — Claude Code official docs, with URL + section.
- **[SRC]** observed source fact — this repository's Go source, with file + symbol.
- **[OBS]** observed locally — real artifacts on this machine, path redacted to
  `~/.claude/projects/<redacted-slug>/<redacted-session>.jsonl`. Content of user files is never quoted;
  only shapes, key names and counts.
- **[INF]** inference — my reasoning on top of the above. Not verified.
- **[UNVERIFIED]** could not be established. Called out rather than reasoned around.

Local environment: `claude --version` reports `2.1.197 (Claude Code)` **[OBS]**. Transcripts written today on
this machine record `"version": "2.1.246"` **[OBS]**, so the binary on `PATH` lags the runtime that actually
produced the newest sessions (auto-update in flight). Both are far above every version floor cited here.

---

## 0. The bar: what the Codex machinery actually consumes

**[SRC]** `ae-cli/internal/attributionlocal/claims_v2.go`.

- `v2StructuredPatchInput` (~line 751) pulls an `apply_patch` patch literal out of the transcript. It accepts
  the payload only if, after unquoting, it starts with `*** Begin Patch\n` and ends with `\n*** End Patch`.
  Anything else returns `""`. No path, time, or model input participates.
- `v2PatchMutations` (~line 786) parses that literal into `[]v2Mutation`. For each `*** Add File:` /
  `*** Update File:` / `*** Delete File:` header it canonicalises the path and then **replays the hunk against
  the commit's parent tree**: `gitShowClaimFile(ctx, repoRoot, commitSHA+"^", path)`. On success it records
  `hash: claimDigest(expected)` — the digest of the *resulting whole-file content*. On any failure it records a
  mutation with an **empty hash**.
- `validV2Mutations` rejects the whole claim if *any* mutation has an empty `path` or empty `hash`. This is the
  fail-closed gate: a mutation that cannot be replayed poisons the claim rather than being skipped.
- `introducedV2Mutations` then keeps only mutations the commit actually introduced.
- A nice detail worth carrying over: when forward application fails, `v2PatchMutations` retries with
  `reverseV2PatchBlock`, covering the case where the parent tree already contains the result.

**So the required artifact shape is:** per tool call, a record from which a *whole-file post-state digest* can
be derived by deterministic replay against `commit^`, keyed to a turn identity, with an unambiguous
"cannot replay" outcome. That is the bar. It is emphatically **not** "a list of files touched".

---

## 1. Agent hooks — `PostToolUse`

### 1.1 The payload, quoted

**[DOC]** https://code.claude.com/docs/en/hooks — section *PostToolUse -> PostToolUse input*:

> `PostToolUse` hooks fire after a tool has already executed successfully. The input includes both `tool_input`,
> the arguments sent to the tool, and `tool_response`, the result it returned. The exact schema for both depends
> on the tool. File-tool `tool_input` paths arrive in the same format as for PreToolUse: always absolute, with
> the platform's native separators, so backslashes on Windows.

```json
{
  "session_id": "abc123",
  "transcript_path": "/Users/.../.claude/projects/.../00893aaf-19fa-41d2-8238-13269b9b3ca0.jsonl",
  "cwd": "/Users/...",
  "permission_mode": "default",
  "hook_event_name": "PostToolUse",
  "tool_name": "Write",
  "tool_input": {
    "file_path": "/path/to/file.txt",
    "content": "file content"
  },
  "tool_response": {
    "filePath": "/path/to/file.txt",
    "success": true
  },
  "tool_use_id": "toolu_01ABC123...",
  "duration_ms": 12
}
```

Plus the **Common input fields** table **[DOC]**, same page, which adds `prompt_id`, `effort`, and (in
subagents) `agent_id` / `agent_type`:

> `prompt_id` | UUID identifying the user prompt currently being processed. Matches the `prompt.id` attribute on
> OpenTelemetry events, so you can correlate hook output with telemetry for a single prompt. Absent until the
> first user input. **Requires Claude Code v2.1.196 or later**

> The `tool_name`, `tool_input`, and `tool_use_id` fields are event-specific.

### 1.2 Answers to the specific questions

| Question | Answer |
|---|---|
| Carries `tool_name`, `tool_input`, `tool_response`? | Yes **[DOC]**, for every tool event. |
| `tool_input` has file path? | Yes, and **documented as always absolute** **[DOC]**. |
| `tool_input` has the content (`content`, `old_string`/`new_string`)? | Yes for `Write` per the quoted example **[DOC]**. For `Edit` the doc does not show an example; the transcript's mirror of the same arguments carries `file_path`, `old_string`, `new_string`, `replace_all` **[OBS]**. |
| `tool_use_id` present? | Yes **[DOC]**, top level. |
| Contract documented and stable? | Documented, **not versioned**. The docs carry no stability guarantee and no "shape may change" warning; the page states only that "The exact schema for both depends on the tool" **[DOC]**. Treat `tool_response` as **incidental**, see §1.3. |
| `MultiEdit` / `NotebookEdit`? | Both are documented tool names, but **neither exists in this Claude Code build's tool surface**: across 120 recent local transcripts the only file-mutating tools observed are `Edit` and `Write` (`Edit` absorbed MultiEdit via `replace_all`) **[OBS]**. |

### 1.3 The one thing I could not verify — and it is the load-bearing one

**[UNVERIFIED]** Whether `tool_response` for `Edit`/`Write` carries the *rich* structured result
(`originalFile`, `structuredPatch`, `oldString`, `newString`) or only the *thin* shape the doc example shows
(`{filePath, success}`).

The doc's own `Write` example shows the **thin** shape. The transcript's `toolUseResult` for the same tool
carries the **rich** shape (§3.2). These are different objects, or the doc example is abridged. I attempted an
empirical test — an isolated harness in the session scratchpad, a `PostToolUse` matcher on `Edit|Write` writing
raw stdin to a capture directory, driven by `claude -p ... --settings <scratchpad>/settings.json` in a
throwaway directory, touching no user config. The run aborted with `Not logged in · Please run /login`; a bare
`claude -p "say hi"` fails identically, so **non-interactive auth is unavailable in this environment** and the
test could not be completed **[OBS]**. This is the single highest-value follow-up: it is a five-minute check in
an interactive session and it decides whether hooks alone suffice.

One corroborating hint, not proof: the `updatedToolOutput` documentation says **[DOC]** "Built-in tools return
structured objects rather than plain strings" and "The replacement value must match the tool's output shape".
That confirms `tool_response` is a structured object for built-ins, but not which fields it has for `Edit`.

### 1.4 A disqualifying gap in the hook mechanism, stated by the docs

**[DOC]** https://code.claude.com/docs/en/hooks — section *PostToolUse*:

> Claude Code doesn't run a `PostToolUse` hook matching `Edit|Write` when a `Bash` command or a process outside
> Claude Code rewrites the same file.

This is not a corner case here. In one real local session, tool-use counts were **Bash 91, Write 1, Edit 0**
**[OBS]**. This repository's own agent operating instructions push edits through Bash ("make file changes with
`sed`, heredocs, or short scripts, rather than using the dedicated Read, Edit, or Write tools"), so under the
prevailing configuration the *majority* of mutations produce **no structured mutation record at all**. See §6.

---

## 2. `tool_use_id` and `prompt_id` as join keys

Prior research (#403) claimed `tool_use_id` gives an exact tool-to-hook join. **Confirmed, and it is stronger
than claimed — it is a documented three-way join, and there is a second, independent turn-level join key.**

**[DOC]** https://code.claude.com/docs/en/monitoring-usage — `claude_code.tool` span attributes:

> `tool_use_id` | The model's `tool_use` block id for this call. Matches the `tool_use_id` on the tool_result and
> tool_decision events **and in hook payloads**, so you can join the span to those records

**[DOC]** same page, *Tool result event*:

> `tool_use_id`: Unique identifier for this tool invocation. Matches the `tool_use_id` passed to hooks, allowing
> correlation between OTel events and hook-captured data.

**[DOC]** same page, *Event correlation attributes*:

> `prompt.id` | UUID v4 identifier linking all events produced while processing a single user prompt

> To trace all activity triggered by a single prompt, filter your events by a specific `prompt.id` value. This
> returns the user_prompt event, any api_request events, and any tool_result events that occurred while
> processing that prompt.

And from the hooks side, quoted in §1.1: `prompt_id` "Matches the `prompt.id` attribute on OpenTelemetry events".

### The join matrix

| Key | Hook payload | Session transcript | OTel |
|---|---|---|---|
| Tool call | `tool_use_id` **[DOC]** | `tool_use.id` on assistant records; `tool_result.tool_use_id` on the paired user record **[OBS]** | `tool_use_id` on `claude_code.tool` span, `tool_result` event, `tool_decision` event **[DOC]** |
| Turn | `prompt_id` **[DOC]** (v2.1.196+) | `promptId` **[OBS]** | `prompt.id` **[DOC]** |
| Message | — | `uuid` / `parentUuid` **[OBS]** | `message.uuid` **[DOC]** (v2.1.214+) |
| API request | — | `requestId` (e.g. `req_011C…`) **[OBS]** | `request_id` on `api_request` and `llm_request` **[DOC]** |

**[OBS]** Verified directly in a real local transcript: an assistant record's `tool_use` id and the
`tool_use_id` on the paired user/`tool_result` record are identical, and that user record carries `promptId`
on the same line — a *direct* turn+tool join with no chain walking. Across 127 file-mutation records sampled
from 60 recent transcripts, **`promptId` and `tool_use_id` were present on 127/127 — zero missing** **[OBS]**.

Assistant records do **not** carry `promptId` directly **[OBS]**; they reach it by walking `parentUuid` (7 hops
in the case I traced, resolving to the correct `promptId`) **[OBS]**. This walk is a pointer chase over
explicit UUIDs, not a heuristic.

**This is the crux, and it holds.** Nothing in this join uses timestamp, cwd, path, model, or token similarity.

---

## 3. The session transcript — the strongest source, and the closest analogue to Codex

### 3.1 Location and record types

**[OBS]** `~/.claude/projects/<slug>/<session-uuid>.jsonl`, one JSONL line per record. **[DOC]** the
`message.uuid` attribute description names the same location: "the `~/.claude/projects/*/*.jsonl` files".

**[OBS]** Record `type` values observed: `user`, `assistant`, `system`, `attachment`, `queue-operation`,
`bridge-session`, `last-prompt`, `custom-title`, `atis-latch`, `mode`, `relocated`.

`assistant` record top-level keys **[OBS]**:
`cwd, effort, entrypoint, gitBranch, isSidechain, message, parentUuid, requestId, sessionId, slug, timestamp, type, userType, uuid, version`

`user` record carrying a `tool_result` **[OBS]**:
`cwd, entrypoint, gitBranch, isSidechain, message, parentUuid, promptId, sessionId, slug, sourceToolAssistantUUID, timestamp, toolUseResult, type, userType, uuid, version`

Subagent activity lives in the **same file**, flagged `isSidechain: true` **[OBS]**, and `tool_use` blocks carry
a `caller` field **[OBS]**.

### 3.2 `toolUseResult` — Claude Code already writes a structured mutation record

This is the finding that matters most. For file-mutating tools the transcript stores, alongside the tool
result, a structured object. Observed key sets **[OBS]**:

```
Edit  -> filePath, oldString, newString, replaceAll, originalFile, structuredPatch, userModified
         (sometimes also: memdirStamped)
Write -> filePath, content, originalFile, structuredPatch, type, userModified
         (type is "create" or "update"; sometimes also: memdirStamped)
```

`structuredPatch` is a standard unified-diff hunk array **[OBS]**:

```
hunk keys: oldStart, oldLines, newStart, newLines, lines
e.g.       {oldStart: 128, oldLines: 10, newStart: 128, newLines: 62}
lines      array of strings prefixed ' ', '-', '+'
```

`filePath` is **absolute** **[OBS]**. `userModified` was `false` on 127/127 sampled records **[OBS]** — it
appears to flag a file the user changed by hand mid-tool; any record with `userModified: true` should be
treated as unreplayable and rejected **[INF]**.

This is materially *better* than the Codex `apply_patch` literal: it is already-parsed structured JSON written
by Claude Code itself, not a string scraped out of a tool argument.

### 3.3 Durability

**[OBS]** In a 727-record local transcript: a single `sessionId` throughout, no `isCompactSummary` records,
`system` subtypes limited to `stop_hook_summary` (22) and `api_error` (1), and 9 distinct `promptId` values.
The file is append-only; compaction summarises what the *model* sees and does not rewrite prior lines **[INF]**.

**[DOC]** Caveat, from the hooks *Common input fields* table:

> The transcript file is written asynchronously and may lag the in-memory conversation, so it may not yet
> include the current turn's most recent messages when a hook fires.

**[INF]** This lag is a hazard only for a hook reading the transcript live. A collector that reads the
transcript *at commit time* (or at `Stop` / `SessionEnd`) is past the lag window. It does mean a hard-killed
process can lose the tail of a session — a coverage hole, addressed in §6.

---

## 4. Token for the same turn

Two independent sources, both keyed to the same turn identity.

**Transcript [OBS].** Every `assistant` record carries `message.usage` and a top-level `requestId`. Observed
shape (values are from a real record; they are token counts, not content):

```
usage: input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens,
       output_tokens_details{thinking_tokens}, cache_creation{ephemeral_1h_input_tokens,
       ephemeral_5m_input_tokens}, server_tool_use{...}, service_tier, speed, iterations[...]
requestId: "req_011C..."
```

The very same assistant record contains the `tool_use` block for the mutation. **Token and mutation are on the
same line of the same file.** Binding them requires no join at all **[OBS]**.

**OTel [DOC].** `claude_code.api_request` event carries `input_tokens`, `output_tokens`, `cache_read_tokens`,
`cache_creation_tokens`, `cost_usd_micros`, `model`, `request_id`. The `claude_code.llm_request` span carries
the same four token attributes plus `request_id`, `client_request_id`, `attempt`, `response.has_tool_call`.
`prompt.id` is documented as an event-correlation attribute present on `api_request` events.

**[INF]** These reconcile through `requestId` == `request_id`, giving a cross-check between the local
transcript and any org telemetry pipeline — useful for detecting a tampered transcript.

**[DOC]** Span hierarchy makes subagent attribution explicit rather than inferred:

```
claude_code.interaction
├── claude_code.llm_request
├── claude_code.hook
└── claude_code.tool
    ├── claude_code.tool.blocked_on_user
    ├── claude_code.tool.execution
    └── (Agent tool) subagent claude_code.llm_request / claude_code.tool spans
```

---

## 5. Replayability — precisely what is and is not reconstructible

I tested replay against **211 real `Edit` records and 165 real `Write` records** drawn from 120 recent local
transcripts **[OBS]**.

### Write — fully reconstructible, always

`content` is the complete post-write file. **165/165 records carried `content` as a string** **[OBS]**.
`type` distinguishes `create` (145) from `update` (20) **[OBS]**, mapping cleanly onto the Codex `add` /
`update` mutation kinds. Post-state digest = `claimDigest(content)`. No replay needed.

### Edit — reconstructible, but the anchor must be the commit parent tree

An `Edit` record gives a *fragment*: `oldString` -> `newString` (+ `replace_all`), plus `structuredPatch` hunk
positions. To reach a whole-file post-state you need the pre-state. Two candidate anchors:

1. **`originalFile` from the record.** Where present, replay is clean: of the 112 `Edit` records that carried a
   string `originalFile`, **112/112 replayed deterministically** (single unambiguous `oldString` occurrence, or
   `replace_all`) **[OBS]**. Line-count deltas of the replayed result were cross-checked against
   `sum(newLines) - sum(oldLines)` from `structuredPatch` and agreed **[OBS]**.

   **But `originalFile` is not reliably present.** It was `null` in **99 of 211 `Edit` records (47%)** and
   **147 of 165 `Write` records (89%)** **[OBS]**. I could not explain when it is omitted: it does not track
   payload size (median `newString` 869 bytes when present vs 347 when null — the *opposite* of a size cutoff),
   and it does not track version (v2.1.234 alone produced 26 present / 40 null) **[OBS]**. Cause
   **[UNVERIFIED]**. **Conclusion: do not build on `originalFile`.**

2. **`git show <commit>^:<path>`** — exactly what `v2PatchMutations` already does **[SRC]**. This anchor is
   always available, is the same anchor the Codex path uses, and sidesteps the `originalFile` gap entirely.

**[INF]** The port is therefore small and mechanical: reuse `gitShowClaimFile` for the pre-state; for `Edit`,
require `oldString` to occur **exactly once** in that content (or apply `replace_all` when set), emit
`claimDigest(result)`; for `Write`, emit `claimDigest(content)` directly. Feed the same `replayFiles` map so
multiple edits to one file within a turn chain, and keep the existing reverse-apply fallback. Any ambiguous or
non-matching `oldString` yields an empty hash, and `validV2Mutations` **[SRC]** then rejects the claim. The
fail-closed gate needs no modification.

### Not reconstructible

- **Deletions.** Neither `Edit` nor `Write` deletes a file. Deletion happens via Bash (`rm`) — no structured
  record **[OBS]**. The Codex `delete` mutation kind has no Claude Code analogue.
- **Renames.** Same — Bash `mv` **[INF]**.
- **Anything Bash did.** See §6.
- **`NotebookEdit` / `MultiEdit`.** Not present in this build's tool surface; shape **[UNVERIFIED]**.

---

## 6. Fail-closed analysis

### 6.1 Hooks are fail-**open**. Do not use them as the sole record.

**[DOC]** https://code.claude.com/docs/en/hooks, *Exit code 2 behavior per event*:

> `PostToolUse` | Can block? **No** | Shows stderr to Claude; the tool already ran

**[DOC]** *Other exit codes*:

> With stdout that Claude Code treats as plain text, or with empty stdout, it's a non-blocking error for most
> hook events: the action proceeds, and the transcript shows a `<hook name> hook error` notice...

> A hook that can't start lands in the same non-blocking bucket. When the script path doesn't exist or isn't
> executable, the shell exits with a code like 127 and you see the same notice... For most hook events, the
> action proceeds. When you set up a policy hook, watch for this notice on its first run: a mistyped path in
> `settings.json` leaves the gate silently disabled.

**[DOC]** on timeout: a hook that reaches its `timeout` "is canceled: Claude Code discards the hook's output,
and the hook renders no decision."

So: hook crashes, hook missing, hook times out — **the mutation still lands and the record is silently lost**.
`PostToolUse` cannot block. That is precisely the "mutation record that can silently go missing" risk class the
ticket flags as worse than a wrong record.

### 6.2 Which events fire, and which do not

**[DOC]** *PostToolUseFailure*:

> This event doesn't fire for tool calls rejected before execution: an unknown tool name, input that fails
> schema or tool-specific validation, or a permission denial. Validation rejections are returned as
> `tool_use_error` results and happen before hooks run, so they fire neither `PreToolUse` nor
> `PostToolUseFailure`. Permission denials fire `PreToolUse` but not this event.

**[DOC]** `tool_result` event, `decision_type`: "Always `accept`, since this event is only emitted after the
tool runs. Rejected calls don't produce a tool result."

**[INF]** Good news for correctness: rejected and interrupted calls produce no mutation record, and they also
produce no mutation. Rejection is fail-*closed by construction* — no false positives.

### 6.3 `--resume` / `--continue`

**[DOC]** > Claude Code saves the injected text in the session transcript. For mid-session events like
> `PostToolUse` or `UserPromptSubmit`, when you resume with `--continue` or `--resume`, Claude Code replays the
> saved text rather than re-running the hook for past turns, so values like timestamps or commit SHAs become
> stale. `SessionStart` hooks run again on resume with `source` set to `"resume"`.

**[INF]** Hooks are **not** re-run for past turns on resume. A hook-based collector that missed a turn during
the original run never gets a second chance. The transcript, by contrast, retains those turns. Another point
for the transcript.

### 6.4 Killed session

**[INF]** Transcript writes are asynchronous **[DOC]**, so a `SIGKILL` can lose the tail. Both mechanisms lose
data here. Detectable only by reconciliation (§6.6).

### 6.5 The Bash hole — the dominant coverage gap

Restating §1.4 because it is the biggest number in this document. Mutations performed via `Bash` produce:

- no `PostToolUse` `Edit|Write` hook **[DOC]**;
- no `toolUseResult` with `structuredPatch` — the Bash `toolUseResult` is `{stdout, stderr, interrupted, ...}`
  **[OBS]**;
- an OTel `full_command` attribute only when `OTEL_LOG_TOOL_DETAILS=1`, and even then it is a shell string, not
  a replayable mutation **[DOC]**.

Observed local ratio in one session: **Bash 91 vs Write 1 vs Edit 0** **[OBS]**.

`FileChanged` does **not** rescue this. **[DOC]** it "runs the hook no matter what changed the file", but its
matcher "is split on `|` and each segment is registered as a **literal filename** in the working directory" —
a pre-declared watch list, useless for files not known in advance — and its payload carries only `file_path`
and `event` (`change`/`add`/`unlink`). **No `tool_use_id`, no `prompt_id`.** Binding a `FileChanged` event to a
turn would require *time proximity*, which is forbidden. `FileChanged` is disqualified for edge 2 **[INF]**.

### 6.6 What makes it fail-closed anyway

**[INF]** The gaps above are *coverage* gaps, not *correctness* gaps. Every record that exists replays exactly
or not at all. The system becomes fail-closed if the claim gate reconciles evidence against the commit diff:

> Compute the set of paths the commit touched. Compute the set of paths covered by replayable mutation records
> for the candidate turns. If the first is not a subset of the second, **reject the claim** rather than
> attributing the covered subset.

`introducedV2Mutations` + `validV2Mutations` **[SRC]** already provide the second half of this. The
reconciliation against the full commit diff is the piece to add. With it, the Bash hole degrades to
"Bash-heavy commits are unattributable" — an honest, safe, loud outcome — rather than "Bash-heavy commits are
silently under-attributed".

---

## 7. Does anything here depend on cwd, path, time, or model? — Audit

| Signal | Verdict |
|---|---|
| `tool_use_id` join | Clean. Explicit id, documented to match across all three surfaces **[DOC]**. |
| `prompt_id` / `promptId` / `prompt.id` join | Clean. Explicit UUID **[DOC][OBS]**. |
| `requestId` -> `request_id` | Clean. Explicit id **[DOC][OBS]**. |
| `uuid` / `parentUuid` walk | Clean. Explicit pointer chase, not proximity **[OBS]**. |
| `filePath` in the record | **Used as a key, not as evidence.** It names *which* file the replay targets; the *proof* is the content digest matching the commit tree. Same role `canonicalClaimPath` plays for Codex **[SRC]**. Not a path heuristic. |
| `timestamp` on records | **Not needed. Do not use it.** |
| `cwd` / `gitBranch` on records | **Not needed. Do not use it.** Note `type: "relocated"` records exist **[OBS]**, so `cwd` is not even stable within a session. |
| `FileChanged` | **Disqualified** — no turn identity, joinable only by time **[DOC]**. |
| OTel as mutation source | **Disqualified** — "Spans redact user prompt text, tool input details, and tool content by default" and content is truncated at 60 KB **[DOC]**. OTel supplies Token and join keys only, never the mutation body. |

No forbidden signal is load-bearing in the recommended design.

---

## 8. Version floors

| Capability | Floor | Source |
|---|---|---|
| `prompt_id` in hook payloads | **v2.1.196** | **[DOC]** hooks, Common input fields |
| `message.uuid` on OTel events | **v2.1.214** | **[DOC]** monitoring-usage, Event correlation attributes |
| `client_request_id` on `api_request`/`llm_request` | **v2.1.214** | **[DOC]** same |
| `tool_source` on `tool_decision` | **v2.1.214** | **[DOC]** same |
| `classifierContext` on PostToolUse | **v2.1.236** | **[DOC]** hooks, PostToolUse decision control |
| `tool_use_id` in hooks / OTel | no floor stated | **[DOC]** |
| `toolUseResult.structuredPatch` in transcripts | **no floor — undocumented entirely** | **[OBS]** present in v2.1.234 / .235 / .237 / .246 |
| Local install | `2.1.197` on PATH; `2.1.246` in today's transcripts | **[OBS]** |

**Practical floor for the recommended design: v2.1.196** (for `prompt_id`), and in practice v2.1.234+ is what
has been observed to emit `structuredPatch`.

---

## Verdict on edge 2 for Claude Code

**Yes — deterministically. Fail-closed only with one addition the current Codex gate does not yet make.**

**By which mechanism.** Not hooks. The **session transcript's `toolUseResult` object** is the right primary
source. For `Write` it carries the complete post-state (`content`), making the digest direct. For `Edit` it
carries `oldString`/`newString`/`replaceAll` plus a `structuredPatch` hunk array, which replays against
`git show <commit>^:<path>` exactly the way `v2PatchMutations` **[SRC]** already replays Codex `apply_patch`
blocks — same anchor, same whole-file digest, same empty-hash-on-failure contract, so `validV2Mutations` needs
no change. Turn binding is a **documented three-way join** on `tool_use_id` and `prompt_id`
(hooks <-> transcript <-> OTel) **[DOC]**, and — better still — for the transcript alone no join is even
required, because the `usage` block and the `tool_use` block for a mutation sit **on the same JSONL record**
**[OBS]**. Nothing in the design touches timestamp, cwd, path-similarity, model, or Token-similarity.

**Why the transcript rather than hooks.** Hooks are structurally fail-open: `PostToolUse` cannot block **[DOC]**,
a hook that crashes, times out, or has a mistyped path is a *non-blocking error* and the mutation lands with no
record **[DOC]**, and hooks are **not re-run for past turns on `--resume`** **[DOC]**. The transcript is written
by Claude Code itself, requires no user configuration to be correct, and survives resume. Hooks remain useful as
a real-time *trigger* and as a corroborating second channel, but must not be the record of truth.

**The single biggest unresolved risk.** The **Bash hole**. **[DOC]** "Claude Code doesn't run a `PostToolUse`
hook matching `Edit|Write` when a `Bash` command or a process outside Claude Code rewrites the same file", and
the transcript's Bash `toolUseResult` is `{stdout, stderr, ...}` with no mutation structure **[OBS]**. In a real
local session the split was **Bash 91 / Write 1 / Edit 0** **[OBS]**, and this repository's own agent
instructions actively direct edits through Bash. Under prevailing configuration, most mutations are invisible to
both mechanisms. This is a *coverage* gap, not a correctness gap, and it is closed by making the claim gate
reconcile the commit's touched-path set against the replayable-mutation path set and **reject any claim that is
not fully covered** — turning silent under-attribution into a loud, honest refusal. `FileChanged` cannot patch
this hole: it carries no `tool_use_id` and no `prompt_id`, so binding it to a turn would require time proximity
**[DOC]**.

### Open items, stated plainly

1. **[UNVERIFIED]** Whether the `PostToolUse` `tool_response` for `Edit`/`Write` carries the rich
   `originalFile`/`structuredPatch` object or the thin `{filePath, success}` shape the doc example shows.
   Non-interactive auth was unavailable, so the empirical capture could not run **[OBS]**. Five-minute check in
   an interactive session. Only affects whether hooks can serve as a *corroborating* channel; the transcript
   route does not depend on it.
2. **[UNVERIFIED]** Why `toolUseResult.originalFile` is `null` in 47% of `Edit` records — not size-driven, not
   version-driven **[OBS]**. Mitigated by anchoring on `commit^` instead, so this is a curiosity rather than a
   blocker.
3. **[UNVERIFIED]** `MultiEdit` / `NotebookEdit` result shapes — neither appears in this build **[OBS]**.
4. `toolUseResult` is **entirely undocumented** — an observed-only contract with no stability guarantee. It
   should be validated defensively and version-gated, and a shape change must fail the claim rather than
   silently produce empty mutations.
