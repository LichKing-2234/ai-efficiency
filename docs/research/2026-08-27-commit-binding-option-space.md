# The Commit-Binding Option Space (#405)

Research for wayfinder map [#401](https://github.com/LichKing-2234/ai-efficiency/issues/401), ticket
[#405](https://github.com/LichKing-2234/ai-efficiency/issues/405). Planning only. No production code was
touched.

This document deliberately does **not** re-derive what its two siblings already established:

- `docs/research/2026-08-27-claude-code-mutation-evidence.md` — Claude Code's hook payload, transcript
  `toolUseResult`, and the `Bash` coverage hole.
- `docs/research/2026-08-27-loongsuite-local-read-reassessment.md` — LoongSuite's local-read envelope.
- `docs/research/2026-08-27-smallest-viable-commit-bound-token-route.md` — the artifact-first recommendation.

Its job is the one the owner asked for and those three do not answer: **what is the full option space, and
where does the map's three-way framing hide something?**

## Evidence classification

Every claim below carries one of:

- **[DOC]** — documented fact, vendor documentation, URL + section given.
- **[OBS]** — observed source fact: a file/symbol in this repository, or something I ran or read on this
  machine. Reproduction given.
- **[INF]** — inference. My reasoning, marked as mine.

Where I could not verify, I say so in the same sentence as the claim.

---

## 1. The two-edge framing, and what the existing machinery actually requires

Commit-bound Token needs two edges:

- **Edge 1 (turn → Token)** — how many tokens this agent turn cost.
- **Edge 2 (turn → commit)** — proof this turn produced content that landed in a specific commit.

Before evaluating mechanisms, it matters *exactly* what edge 2 has to deliver, because the map's language
("structured mutation evidence", "structured patch") overstates it.

**[OBS]** `ae-cli/internal/attributionlocal/claims_v2.go`, type `v2Mutation` (line 365):

```go
type v2Mutation struct {
	path string
	hash string
	kind string
}
```

**[OBS]** `validV2Mutations` accepts a mutation set only when every element has a non-empty `path` **and** a
non-empty `hash`. `v2MutationState` maps a mutation to `"deleted"` or to `strings.ToLower(mutation.hash)`.
`introducedV2Mutations` then reads the committed blob and its parent with `gitShowClaimFile`, computes
`v2TreeState` for each, and selects a mutation only when the mutation's post-state equals the committed
content state and differs from the parent state. `v2MutationDigest` sorts `kind\0path\0hash` triples.

The consequence, stated plainly **[INF]**:

> The commit proof is **content-addressed post-state matching**. The unit of evidence is
> `(turn key, path, kind, hash of the file's content *after* the mutation)`. A patch is not required — a
> patch is merely one way `v2PatchMutations` currently *computes* that post-state, by replaying an
> `*** Begin Patch` block against the commit's parent tree via `applyV2PatchBlock`.

This is the single most important structural fact in this document, and the three-way framing obscures it.
Any evidence source that can produce a post-state content hash per path per turn plugs into
`introducedV2Mutations` unchanged. Sources that produce full post-state content (a `Write` payload, a shadow
git blob, a checkpoint snapshot) are *easier* to consume than a patch, not harder, because they skip replay
entirely.

Edge 1's requirement, for contrast **[OBS]** (`claims_v2.go` §`buildCodexV2ClaimCandidates`, and spec §4 of
`docs/superpowers/specs/2026-08-11-codex-commit-token-attribution-v2-design.md`): either trusted relay
`x-client-request-id` values bound to the same `thread.id + turn.id` (`relay_official`), or locally
aggregated per-model/15-minute token buckets after trusted WebSocket completion evidence (`codex_local`). A
turn carrying both fails closed as `mixed_token_sources`.

---

## 2. Did I falsify the two-family hypothesis?

The hypothesis: under the #401 constraints, edge 2 has only two families —
**(a)** the agent emits a replayable structured mutation record keyed to a turn, or
**(b)** something intercepts the mutation as it happens and keys it to a turn identity.

**Verdict: partially falsified. Two further families exist. Both survive the "is it a heuristic?" test and
both fail the "is it per-turn and verifiable?" test — but they fail for *different* reasons than (a) and (b),
which is why naming them is worth the space.**

### Family (c) — bind at the moment of the commit, not the mutation

Instead of recording mutations at all, capture a turn identity at the instant the commit object is created.
The commit is then bound to a turn by construction. Nothing about the file changes is ever observed.

This is genuinely not (a) or (b): no mutation record exists anywhere in the design. Two concrete carriers:

1. **Environment-propagated identity into a `pre-commit`/`post-commit` hook.** The hook runs in a process
   descended from the agent, so it can read the agent's identity from its own environment.
2. **A commit trailer or `git notes` entry written by the agent (or by a `prepare-commit-msg` hook).**

**Why it fails here, empirically.** **[OBS]** Running `env` from inside a Claude Code `Bash` tool call on this
machine (reproduce: any `Bash` tool invocation) shows the agent exports:

```
AI_AGENT=claude-code_2-1-246_agent
CLAUDECODE=1
CLAUDE_CODE_SESSION_ID=e02267ac-c57c-41ba-89ae-4f40f36a6922
CLAUDE_CODE_HOST_SESSION_ID=local_e02267ac-...
CLAUDE_PID=1243
```

There is a **session** id and no turn, prompt, or request id. **[INF]** Family (c) therefore yields
session-granular binding only. Attributing a whole session's Token to one commit is not per-turn attribution;
splitting it across the session's commits would require some allocation rule that is precisely the kind of
proximity heuristic #401 forbids. Family (c) is real, but it degrades to the wrong granularity — not to a
heuristic, which is why the hypothesis did not anticipate it.

Trailers do not rescue it. **[DOC]** Claude Code's `attribution` setting
(https://code.claude.com/docs/en/settings-reference) takes a *static* string appended to every commit;
**[DOC]** Cursor's Git attribution (https://cursor.com/help/integrations/git) writes a fixed
`Made with Cursor` trailer with no user-controlled text; **[DOC]** Aider's `--attribute-co-authored-by`
(https://aider.chat/docs/git.html, *Commit attribution*) writes a fixed `Co-authored-by` trailer. None of the
three exposes a per-turn value. Trailers carry **tool identity**, never **turn identity**.

### Family (d) — make the commit boundary equal the turn boundary

Have the agent create one commit per turn. Then edge 2 is an identity, not a proof.

**[DOC]** Aider does exactly this: it commits each set of edits itself and pre-commits pre-existing dirty
state first, so its edits are isolated in their own commit (https://aider.chat/docs/git.html, *Commit
behavior*). **[OBS]** Codex once had this too: `codex features list` on this machine reports
`codex_git_commit` with stage `removed` and state `false`.

**[INF]** This is a fourth family and it is the only one that makes edge 2 *free*. It is unusable as a route
because it requires controlling how every developer commits, which the platform explicitly does not do, and
because the one tool that ships it (Aider) has no per-turn Token contract at all (§4). Worth recording as the
shape a future first-party agent could adopt, not as an option on the table.

### What the hypothesis got right

For evidence that is both **per-turn** and **verifiable against commit content**, families (a) and (b) are
exhaustive, and the reason is a counting argument rather than an enumeration **[INF]**: a turn key is not
derivable from repository content. Content hashes are symmetric — a blob does not know who wrote it. So the
turn key must be *injected* by something that knows it at the time, which is either the agent itself (a) or a
process the agent invokes or notifies (b). Mechanism 7 in §3 is the direct test of this and it fails exactly
as predicted.

### The dimension the hypothesis hides

The (a)/(b) split is about **who produces the record**. The decision that actually matters is **what the
record contains**, and on that axis §1 shows the bar is far lower than "replayable patch": a post-state
content hash per path. Re-cutting on that axis is what §5 does.

---

## 3. Every mechanism evaluated

| # | Mechanism | Family | Verdict |
|---|---|---|---|
| 1 | Agent transcript / tool-call artifact (status quo, Codex rollout JSONL) | a | **Viable-with-conditions** — works, but is coupled to a *model-generated* wrapper grammar that is broken again at HEAD today (§3.1) |
| 2 | `PostToolUse`-class agent hook | b | **Viable** — the strongest mechanism; first-party, turn-keyed, content-bearing on 5 of 7 agents surveyed, including Codex (§3.2) |
| 3 | `FileChanged`-class agent event | b | **Disqualified** — path but no content and no turn key; binding would need time proximity |
| 4a | `post-commit` git hook | — | **Necessary but never sufficient** — supplies the commit side of edge 2 only (§3.3) |
| 4b | Commit trailers | c | **Disqualified as turn evidence** — carries tool identity only, no vendor exposes a per-turn value |
| 4c | `git notes` | — | **Not an evidence mechanism** — a storage option, orthogonal to both edges (§3.3) |
| 4d | `pre-commit` hook + env-propagated identity | c | **Disqualified** — session granularity only, verified on this machine (§2) |
| 5 | Filesystem / process interception (FSEvents, `inotify`, `fanotify`, `LD_PRELOAD`, FUSE) | b* | **Disqualified** — yields `(path, content, time)` with no turn key (§3.4) |
| 6 | Editor / LSP / IDE-extension capture | b | **Disqualified as a general mechanism** — same missing-turn-key problem for CLI agents; for IDE agents the vendor hook (#2) strictly dominates; no headless/CI coverage |
| 7 | Content-addressed reconstruction from commit content alone | — | **Disqualified** — cannot produce a turn key; this is the direct test of the hypothesis and it fails as predicted |
| 8 | Shadow-git / checkpoint capture | a/b | **Viable-with-conditions** — documented for Gemini CLI, undocumented internals for Cline/Roo (§3.5) |
| 9 | Agent-emitted per-turn diff (Codex `TurnDiffEvent`) | a | **Viable-with-conditions** — first-party and *not* model-generated, so immune to §3.1's treadmill; not persisted to rollout JSONL (§3.6) |
| 10 | Relay / proxy interception (`sub2api`) | — | **Edge 1 only** — already the token source; supplies nothing for edge 2 |
| 11 | External telemetry (LoongSuite / OTLP) | a-derived | **Tool-dependent, and #403's conclusion is narrower than it reads** (§3.7) |
| 12 | Agent creates the commit | d | **Disqualified as a route** — not enforceable across a fleet; see §2 |
| 13 | MCP tool interposition (route edits through an AE-owned MCP server) | b | **Disqualified** — MCP carries no turn id to the server, and agents do not route their native edit tools through MCP |
| 14 | Per-turn git worktree / sandbox around the agent | b | **Disqualified for interactive use** — requires driving the agent process; conceivable for CI only |

### 3.1 Mechanism 1 — the artifact route's real cost, measured today

**[OBS]** I ran a live Codex 0.149.1 turn against the org relay in an isolated `CODEX_HOME`
(scratchpad `probe/`, `codex exec --skip-git-repo-check`, model `gpt-5.6-sol`, provider base_url
`https://sub2api.agoralab.co`). The turn edited one file. The rollout JSONL recorded the edit as a
`custom_tool_call` with `name: "exec"` and this exact `input` (174 bytes):

```js
const patch = "*** Begin Patch\n*** Update File: marker.py\n@@\n-MARKER = \"before\"\n+MARKER = \"after\"\n*** End Patch";
const p = await tools.apply_patch(patch);
text(p);
```

**[OBS]** The two recognizers shipped at HEAD (`claims_v2.go` lines 44–45) are:

- `v2WrappedPatchPattern` — requires exactly `const patch = "..."; text(await tools.apply_patch(patch));`
- `v2InlinePatchPattern` — requires exactly `const X = await tools.apply_patch("..."); text(JSON.stringify(X));`

Both are anchored `^...$` over the whole payload. **[OBS]** I compiled both regexes verbatim into a scratch Go
program and fed them the exact bytes extracted from the rollout: **both return `false`.**

The observed wrapper is a *third* shape — three statements, patch bound to a named identifier and passed by
name, result returned via `text(p)` rather than `text(JSON.stringify(p))`. It matches neither.

**[INF]** This is the decisive fact about mechanism 1. HEAD is commit `36bb67b1`,
*"fix(ae-cli): restore Codex 0.149.1 evidence matching (#395)"*, and the wrapper Codex 0.149.1 actually emits
still does not match. The sibling route document's central claim — *"the remaining distance to reliable is one
parser grammar, not one architecture"* — is true about the architecture and misleading about the cost,
because the grammar is not a contract. It is JavaScript **written by the model**, re-sampled every turn, and
therefore a recurring liability rather than a one-time repair. I am not asserting #395 was wrong; I am
asserting the class of fix does not converge.

### 3.2 Mechanism 2 — `PostToolUse`, including on Codex

This is the mechanism the map's three routes do not name, and it is the reason §5 re-cuts them.

**[OBS]** Codex 0.149.1 on this machine ships a hook system, and it is **stable and on by default**:

```
$ codex features list
hooks                                    stable             true
```

**[OBS]** The Codex binary
(`.../@openai/codex-darwin-arm64/vendor/aarch64-apple-darwin/bin/codex`, reproduce with
`strings -a`) contains the hook event set and the hook payload field set as contiguous Rust string-table
data:

- events: `pre_tool_use`, `permission_request`, `post_tool_use`, `pre_compact`, `post_compact`,
  `session_start`, `session_end`, `user_prompt_submit`, `subagent_start`, `subagent_stop`
- payload fields: `session_id`, **`turn_id`**, `agent_id`, `agent_type`, `transcript_path`,
  `hook_event_name`, `model`, `permission_mode`, `trigger`, `tool_name`, **`tool_input`**, **`tool_use_id`**,
  **`tool_response`**
- embedded JSON Schema definitions including `PostToolUseHookSpecificOutputWire` and
  `PreToolUseHookSpecificOutputWire`, with `hookEventName`, `permissionDecision`, `additionalContext`

**[OBS]** `~/.codex/hooks.json` on this machine is a live, working example of the config format
(`{"hooks": {"<Event>": [{"matcher": "*", "hooks": [{"type": "command", "command": "..."}]}]}}`), currently
wired to a LoongSuite pilot script for `SessionStart`, `UserPromptSubmit`, `SubagentStart`, `SubagentStop`,
`Stop`.

**[INF]** This is the single most consequential finding in this document. Codex — the tool the map treats as
the *artifact-first* case — already exposes a `PostToolUse` hook carrying `turn_id` **and** `tool_input`
**and** `tool_response`, in a schema that is field-for-field near-identical to Claude Code's
(**[DOC]** https://code.claude.com/docs/en/hooks, *PostToolUse*: `session_id`, `prompt_id`, `tool_name`,
`tool_input`, `tool_response`, `tool_use_id`). A "tool-neutral evidence contract" is therefore not a distant
aspiration requiring a second tool to independently earn its way in; it is the *shortest* description of what
two of the three in-scope tools already emit.

**Three honest caveats, all load-bearing:**

1. **[OBS, negative]** I could not make a Codex hook fire. In my isolated probe the `PostToolUse` and
   `PreToolUse` hooks were configured and never invoked, with no warning printed. **[OBS]** The real
   `~/.codex/config.toml` contains `[hooks.state."<path>:<event>:<idx>:<idx>"] trusted_hash = "sha256:..."`
   entries; I tried and failed to reproduce that hash from the command string or the script file, so I could
   not authorize hooks in a fresh `CODEX_HOME`. **[INF]** Codex hooks appear to require a trust step that a
   fleet installer would have to satisfy. **This is the highest-value follow-up probe in this document** and
   it is a ten-minute check in an interactive Codex session.
2. **[OBS]** In my probe, *all three* tool calls in the turn were `name: "exec"` — including the patch, which
   reached `tools.apply_patch` **inside** the JS wrapper. **[INF]** A Codex `PostToolUse` hook would therefore
   most likely receive `tool_name: "exec"` and `tool_input: <the JS wrapper>`, which means **the hook does not
   automatically escape §3.1's wrapper-grammar problem**. Whether Codex fires a nested hook for
   `tools.apply_patch` itself, and whether `tool_response` carries a structured apply-patch result, are
   unverified and are the *second* half of the same follow-up probe. I want to be blunt: if the answer is
   "one `exec` hook per wrapper", mechanism 2 buys reliability of *delivery* but not of *parsing* for Codex.
3. **[DOC]** Hooks are fail-open by construction across every vendor (a missing or crashed hook does not stop
   the agent), and Claude Code's docs state it does not fire an `Edit|Write` `PostToolUse` hook when a `Bash`
   command rewrites the same file (https://code.claude.com/docs/en/hooks, *PostToolUse*). The sibling Claude
   Code document measured the resulting hole at Bash 91 / Write 1 / Edit 0 in one session. Hooks must
   therefore be an *additional* evidence source that fails closed downstream, never the sole record.

### 3.3 Mechanism 4 — what git-native capture can and cannot know

**[OBS]** This repository already installs managed `post-commit`, `post-rewrite`, and `pre-push` hooks via
`core.hooksPath` — `ae-cli/internal/hooks/script.go:WriteManagedScripts` writes all three; `install.go`
(`EnableGlobal` / `EnableRepo`) sets `core.hooksPath` at global, local, or worktree scope.

**[OBS]** What `post-commit` currently captures (`ae-cli/internal/hooks/handler.go:PostCommitResolved`):
`CommitSHA` (from `git rev-parse HEAD`), `ParentSHAs`, `BranchSnapshot`, `HeadSnapshot`, a deterministic
`CheckpointEventID`, the frozen server/owner/repo/workspace identity, and — for cherry-picks only — a
`LineageKind`, `SourceCommitSHA`, and matching `CommitPatchID`/`SourcePatchID` via
`attributionlocal.StableCommitPatchID`, discarded unless the two patch IDs are equal. In compact mode the
agent snapshot is skipped entirely.

**Could it capture more without heuristics?** **[INF]** Yes, but only *commit-side* facts. A `post-commit`
hook can deterministically know every blob in the commit and every blob in its parents, i.e. exactly the
inputs `introducedV2Mutations` already reads on demand. What it structurally cannot know is **who authored
which hunk** — the commit object carries no such information, and neither the index nor the reflog records
it. So the honest framing is: the git hook is one endpoint of edge 2 and can never be both endpoints. Its one
genuine expansion opportunity is *precomputation* — snapshotting the commit's per-path post-state hashes at
commit time so late-arriving turn evidence can be matched without re-reading git — which is a performance and
retention change, not a new evidence source.

**`git notes`.** **[OBS]** `git notes --help` (git-notes(1), *DESCRIPTION*): notes attach data to an object
"without touching the objects themselves", default ref `refs/notes/commits`. **[INF]** Notes are a *storage*
mechanism for evidence you already have; they answer "where do I put it", never "which turn". They also
carry two operational costs this design does not want: notes are not pushed by default (so they are
machine-local unless explicitly propagated, duplicating the backend ledger), and they do not survive rewrite
without `notes.rewriteRef` configuration — whereas `post-rewrite` lineage is already handled server-side.
**[OBS]** No agent in the §4 survey documents writing git notes.

### 3.4 Mechanism 5 — the rigorous answer on filesystem/process interception

The question posed was whether any such mechanism yields a **turn identity** or only "a change happened at
time T".

**[INF]** Only the latter, and the argument is structural rather than empirical. FSEvents/`inotify`/`fanotify`
deliver `(path, event kind)`; a FUSE layer or `LD_PRELOAD` shim additionally delivers content and the writing
**pid**. None of them delivers a turn. The best case is `LD_PRELOAD`/`ptrace` on a process the agent spawned,
which recovers process ancestry — and §2 already measured what ancestry gets you on the richest available
agent: `CLAUDE_CODE_SESSION_ID`, a **session**, no turn. To go from a session-scoped write event to a turn you
must ask "which turn was active when this write happened", which is timestamp proximity, explicitly forbidden
by #401.

The only escape is to have the agent announce turn boundaries — at which point mechanism 2 is already
present and supplies strictly more (it gives you `tool_input` and `tool_use_id` too), for far less
deployment cost than a filesystem shim. **Disqualified, and specifically disqualified for the reason the
brief suspected.**

### 3.5 Mechanism 8 — shadow-git / checkpoint capture

**[DOC]** Gemini CLI documents a shadow git repository at `~/.gemini/history/<project_hash>` plus checkpoint
files at `~/.gemini/tmp/<project_hash>/checkpoints`, where each checkpoint stores a git snapshot, the full
conversation history, and "The specific tool call that was about to be executed"
(https://github.com/google-gemini/gemini-cli/blob/main/docs/cli/checkpointing.md).

**[DOC]** Cline maintains "a shadow Git repository separate from your project's actual Git history" and
"After each tool use (file edits, commands, etc.), Cline commits the current state of your files to this
shadow repo" (https://docs.cline.bot/features/checkpoints). **[DOC]** Roo Code documents the same pattern
(https://roocodeinc.github.io/Roo-Code/features/checkpoints). Neither documents the storage path, the
message-to-checkpoint mapping, or any programmatic access — both are UI-only.

**[INF]** Architecturally this is the most *convenient* possible edge-2 source, because a shadow commit
already **is** a set of content-addressed post-states, which is exactly `v2Mutation` (§1) with no replay step.
It also converts naturally into the existing `StableCommitPatchID` lineage machinery. It is viable-with-
conditions only: for Gemini the path is documented but Gemini is not in scope; for Cline/Roo everything about
it is undocumented internals. **A shadow-git capture that `ae-cli` writes itself is not a variant of this
mechanism** — it would need a turn boundary signal, which returns you to mechanism 2.

### 3.6 Mechanism 9 — Codex's own per-turn diff

**[OBS]** The Codex 0.149.1 binary contains `TurnDiffEvent` and `TurnDiffUpdatedNotification` symbols, an
app-server notification set including `TurnDiffUpdated`, and an event-message variant `turn_diff`.
**[OBS]** `codex features list` reports `cwd_relative_turn_diffs` at stage `under development`, state `false`.
**[OBS]** `codex exec` printed a per-turn unified diff at the end of my probe run. **[OBS]** No `turn_diff`
event was written to the probe's rollout JSONL; across all local Codex sessions I found exactly one rollout
file containing the string, and could not parse a `turn_diff` payload from it.

**[INF]** This is the most interesting under-explored option for Codex specifically, because a turn diff is
produced by **Codex itself**, not sampled from the model, and therefore has none of §3.1's grammar
instability. The obstacle is transport: it appears on the app-server/notification surface rather than in the
rollout file `ae-cli` scans today. Reaching it would mean `ae-cli` consuming Codex's app-server protocol —
a real integration, and one whose stability contract is unknown to me. I could not verify whether the
notification is available to third parties, whether the feature flag gates it, or what identity it carries.

### 3.7 Mechanism 11 — a narrower reading of #403

#403 concluded LoongSuite carries no commit proof. **[INF]** That conclusion is correct and I am not
reopening it — but it is a fact about *the LoongSuite normalized event model*, not about telemetry as a
class, and the map's route names encode the broader reading.

**[DOC]** Gemini CLI's OpenTelemetry export emits `gemini_cli.tool_call` with `function_name` **and
`function_args`**, stamped with `prompt_id` and `session.id`, alongside `gemini_cli.api_response` carrying
`input_token_count` / `output_token_count` / `cached_content_token_count` / `total_token_count`, with a
documented local file sink
(https://github.com/google-gemini/gemini-cli/blob/main/docs/cli/telemetry.md). For that tool, telemetry alone
carries *both* edges. **[INF]** So "telemetry cannot prove commits" is a property of one vendor's schema, not
a law. It does not change the recommendation — Gemini is not in scope — but it does mean the route should be
named for the *shape* of the contract, not for a vendor.

---

## 4. Cross-agent survey

Sources: vendor documentation only, URLs inline. Columns: **(a)** per-turn Token components, **(b)** a
structured file-mutation record keyed to a turn, **(c)** a hook/extension point that could supply one.

| Agent | (a) Per-turn Token | (b) Turn-keyed mutation record | (c) Hook with content + turn key | Net |
|---|---|---|---|---|
| **Claude Code** | **[DOC]** Yes — OTel `claude_code.api_request` with `input_tokens`/`output_tokens`/`cache_read_tokens`/`cache_creation_tokens`, plus `prompt.id`, `message.uuid`, `client_request_id` (https://code.claude.com/docs/en/monitoring-usage). **[OBS]** transcript assistant rows carry `requestId` + full `message.usage` | **[OBS]** Yes — transcript `toolUseResult` carries `{filePath, oldString, newString, originalFile, replaceAll, structuredPatch[], userModified}`; 5,731 such rows across local transcripts (3,198 + 1,388 edits, 999 + 136 creates/updates). Caveat **[OBS]**: on edit rows `originalFile` is `null` 3,198 times vs. a string 1,388 times, and the longest observed string is 9,990 chars ⇒ **[INF]** it is omitted above ~10 KB. In all 3,198 of those null cases `oldString`, `newString` **and** a non-empty `structuredPatch` are present, so parent-anchored replay still works. Write/`create` rows carry full post-state `content` instead | **[DOC]** Yes — `PostToolUse` with `session_id`, `prompt_id`, `tool_name`, `tool_input`, `tool_response`, `tool_use_id` (https://code.claude.com/docs/en/hooks); Edit `tool_input` = `{file_path, old_string, new_string, replace_all}` (https://code.claude.com/docs/en/tools-reference) | **Qualifies on all three.** Strongest surface of any agent |
| **Codex** | **[OBS]** Yes — rollout `token_count` events with `total_token_usage` + `last_token_usage` component split; relay `x-client-request-id` for `relay_official` | **[OBS]** Yes but fragile — `custom_tool_call` `input` carrying a model-generated JS `apply_patch` wrapper; `internal_chat_message_metadata_passthrough.turn_id` present. Shape drifts (§3.1) | **[OBS]** Yes — `post_tool_use` with `turn_id`, `tool_input`, `tool_use_id`, `tool_response`; feature `hooks` **stable/enabled**. Gated by a `trusted_hash` I could not reproduce (§3.2) | **Qualifies, with the trust-gate and wrapper-in-`exec` caveats** |
| **Cursor** | **[DOC]** Yes — Admin API `tokenUsage {inputTokens, outputTokens, cacheWriteTokens, cacheReadTokens}` with `conversationId` (https://cursor.com/docs/account/teams/admin-api); Enterprise OTel `cursor.token.usage` + `cursor.api.request` (https://cursor.com/docs/enterprise/opentelemetry-export) | **[DOC]** Partly — CLI `stream-json` `tool_call` events carry full tool args incl. `writeToolCall.args.{path,fileText}` (https://cursor.com/docs/cli/reference/output-format). Transcript JSONL is announced in the changelog; on-disk path **not documented** | **[DOC]** Yes — `afterFileEdit` with `file_path` + `edits[].{old_string, new_string}`, base payload carrying `conversation_id` and **`generation_id`** ("changes per user message") (https://cursor.com/docs/agent/hooks) | **Qualifies.** Weak seam: usage events expose `conversationId` but no documented `generation_id`, so the Token join may degrade to per-conversation |
| **Gemini CLI** | **[DOC]** Yes — richest of the set; `gemini_cli.api_response` with 6 token fields + `prompt_id` + `session.id`, documented local sink | **[DOC]** Yes — `gemini_cli.tool_call.function_args` with `prompt_id`; plus documented shadow-git checkpoints (§3.5) | **[DOC]** Hooks exist (`BeforeTool`/`AfterTool`, base fields `session_id`, `transcript_path`, `cwd`) but **no dedicated file-edit event**, and `AfterModel` exposes only `totalTokenCount` (https://github.com/google-gemini/gemini-cli/blob/main/docs/hooks/reference.md) | **Qualifies via telemetry, not hooks** |
| **Kiro** | **[DOC]** **No.** Billed in credits/requests; no token counts anywhere in the hooks docs or the 2.x CLI reference (https://kiro.dev/docs/billing/related-questions/) | **[DOC]** No documented equivalent | **[DOC]** Yes — `PreToolUse`/`PostToolUse` with `hook_event_name`, `cwd`, `session_id`, `tool_name`, `tool_input` (https://kiro.dev/docs/hooks/types/). **No turn id**. `PostFileSave`/`PostFileCreate`/`PostFileDelete` have **no documented payload at all** | **Fails edge 1 outright.** Consistent with #403 |
| **GitHub Copilot CLI** | **[DOC]** Not documented for the CLI. SDK only: `assistant.usage` with 5 token fields (https://docs.github.com/en/copilot/how-tos/copilot-sdk/features/usage-and-billing) | **[DOC]** Location documented (`~/.copilot/session-state/<sessionId>/events.jsonl`), **schema not** | **[DOC]** `postToolUse` carries `sessionId`, `toolName`, `toolArgs: unknown`, and `toolResult: {textResultForLlm}` — **no turn id, and the result is a text summary, not a diff** (https://docs.github.com/en/copilot/reference/hooks-reference) | **Fails (c) by design** |
| **Aider** | **[DOC]** No documented per-exchange contract; `--analytics-log` schema is explicitly deferred to source (https://aider.chat/docs/more/analytics.html) | **[DOC]** No — but auto-commit makes the commit boundary the turn boundary (§2, family d) | **[DOC]** No hook or extension point | **Fails edge 1.** Solves edge 2 by accident |
| **Cline / Roo Code** | **[DOC]** Per-**task** only, field names and paths undocumented | **[DOC]** Shadow-git checkpoints per tool use, **path and schema undocumented**, UI-only | **[DOC]** Cline hooks exist (`PreToolUse`/`PostToolUse`); the stdin schema page rendered as a stub — could not verify whether the payload carries edit content or a turn id | **Everything usable is undocumented internals** |

**Does a tool-neutral contract have takers?** **[INF]** Yes — and this is the survey's payload. Ranked by
what is definable *today* over documented surfaces: Claude Code and Cursor qualify on all three axes with a
genuine per-turn key (`prompt_id`, `generation_id`); Codex qualifies pending the trust-gate probe; Gemini CLI
qualifies through telemetry. Kiro fails on edge 1 and cannot be rescued by any edge-2 mechanism. Copilot CLI
and Aider fail on the axes they fail on for deliberate product reasons. Cline/Roo are undocumented.

That is **four of the eight**, including all three in the map's stated scope except Kiro. A tool-neutral
contract is not "a contract almost nobody can satisfy".

**One cross-cutting pattern worth carrying into any spec** **[INF]**: `FileChanged`-style events are
consistently weaker than `PostToolUse`-style events across every vendor — they carry the path but drop both
the content and the turn key (Claude Code's `FileChanged` has no `prompt_id`; Kiro's file triggers have no
documented payload). **Bind to the tool-call event, never to the file-change event.**

---

## 5. Proposed re-cut of the route options

### Where the map's three-way split is misleading

**It puts three things on one axis that live on three different axes.** "Artifact-first" is an *acquisition
method* for one tool. "LoongSuite-augmented" is a *vendor dependency*. "Tool-neutral evidence contract" is a
*scope*. They are not mutually exclusive and never were: **[OBS]** Claude Code's transcript is an artifact
that carries strictly more than Codex's — `oldString`, `newString`, `originalFile`, `structuredPatch`, and a
`requestId` with full token components — so "artifact-first" and "tool-neutral" are the *same route* for that
tool. Presenting them as alternatives forces a false choice.

**It omits the mechanism that actually changes the answer.** `PostToolUse` is not a variant of any of the
three. It is first-party, documented, turn-keyed, content-bearing, and — per §3.2 — **already stable and
enabled in the very Codex build that the artifact route is currently failing to parse.**

**It over-generalizes #403.** "LoongSuite-augmented" reads as "telemetry as an evidence source", and #403's
finding is narrower than that (§3.7). LoongSuite's real value is *diagnostic coverage* — seeing turns whose
Token never reached a commit — which is a delivery/observability question, not an evidence question. It
belongs on a different axis, where it does not compete with the other two.

**It frames the artifact route's cost as one-time when it is recurring.** §3.1 measures this: at HEAD, after a
commit whose stated purpose was restoring 0.149.1 matching, the live wrapper still does not match.

### The re-cut: two axes, four routes

**Axis A — acquisition (per tool):** A1 passive artifact scan · A2 active hook capture · A3 vendor telemetry.
**Axis B — contract shape:** B1 per-tool adapters into an internal shape (status quo) · B2 a *published*
evidence envelope every adapter must produce.

| Route | Shape | What it costs | What it buys | Honest weakness |
|---|---|---|---|---|
| **R1 — Repair and hold** | A1 + B1, Codex only | One parser change per Codex release | Nothing new to build | §3.1: not one repair. The grammar is model-generated and does not converge |
| **R2 — Hook-first, artifact-fallback** *(my recommendation)* | A2 primary + A1 fallback, per tool | A `PostToolUse` installer per agent; the Codex trust-gate probe | Removes the dependency on model-generated text; extends to Claude Code and Cursor with no new proof machinery | Hooks are fail-open everywhere; the `Bash` hole is real; §3.2 caveat 2 means Codex may still need wrapper parsing inside `tool_input` |
| **R3 — Publish the envelope** | B2, orthogonal to A | Small — §1 shows the envelope is already `v2Mutation` | Makes "tool-neutral" a naming exercise rather than a schema migration | Needs an agent/tool dimension in the durable ledger, which does not exist today |
| **R4 — Telemetry as diagnostics** | A3, non-authorizing | A shadow lane that never authorizes an allocation | Visibility into turns that produce no allocation — the failure class that hid §3.1 | Buys forensics, not authority (#403 stands) |

R1–R4 are **not alternatives**. The coherent recommendation is **R3 + R2, with R1 retained as the fallback
lane and R4 kept explicitly non-authorizing**:

1. **State the envelope first (R3).** `(turn key, path, kind, post-state content hash)` plus
   `(turn key, token components | trusted request ids)`. §1 shows this is what the machinery already
   consumes. Writing it down is nearly free and it is what makes every later source pluggable.
2. **Add hook capture as a second, independent producer of that envelope (R2)** — starting with whichever of
   Codex or Claude Code the trust-gate probe clears first. Keep artifact scanning as the fallback and as
   backfill for turns that predate hook installation. Two independent producers of the same
   content-addressed envelope is also a free cross-check: when both fire, they must agree, and disagreement
   is a fail-closed signal rather than a silent gap.
3. **Do not treat tool-neutrality as a later gate.** §4 shows the precondition the sibling route document
   requires — "a second tool independently proves an exact mutation-to-commit artifact" — **is already met by
   Claude Code today** (transcript `toolUseResult`, and a documented hook), and arguably by Cursor. The gate
   should be re-stated as a ledger-schema question (does the durable pool need a tool dimension?), which is a
   real cost, rather than as an evidence question, which is settled.
4. **Keep Kiro out on edge 1**, not edge 2. **[DOC]** No request-level token source exists. No edge-2
   mechanism rescues that, and this is the one place where the existing three-way framing is exactly right.

### The next artifact to commission

**[INF]** Not a spec. A one-hour probe, in this order, because two of its four answers can invalidate R2:

1. Does a Codex `PostToolUse` hook fire at all, and what authorizes it? (Reproduce the `trusted_hash`.)
2. When it fires for a patch, is `tool_name` `exec` with the JS wrapper in `tool_input`, or is there a nested
   `apply_patch` call? What is in `tool_response`?
3. Does Claude Code's `tool_response` for `Edit`/`Write` carry the rich shape (`originalFile`,
   `structuredPatch`) or only the thin documented shape? — the sibling document's open question, still open.
4. Is Codex's `TurnDiffUpdated` notification reachable by a third party without the app-server?

---

## 6. What I could not verify

- **Whether any Codex hook fires**, and what the `trusted_hash` is computed over **[OBS, negative]**. My
  isolated-`CODEX_HOME` probe configured `PreToolUse`/`PostToolUse` hooks and captured nothing, with no
  warning. Everything in §3.2 about Codex's hook *payload* comes from binary string tables and the
  `codex features list` self-report, not from an observed hook invocation. If the trust gate cannot be
  automated, R2's Codex leg is materially weaker.
- **Whether Codex's `PostToolUse` sees `apply_patch` or only `exec`** — §3.2 caveat 2. This decides whether
  R2 removes the §3.1 treadmill for Codex or merely relocates it.
- **Whether `TurnDiffUpdated` is a third-party-reachable surface** and what identity it carries (§3.6).
- **Claude Code's `tool_response` shape for `Edit`/`Write`** — the sibling document could not complete this
  test either (non-interactive auth unavailable); I did not retry it.
- **Cursor's transcript on-disk path** — announced in the CLI changelog, not documented in the configuration
  reference. Community-sourced paths exist; I am not asserting them.
- **Cline's hook stdin schema** — the dedicated reference page rendered as a stub on fetch.
- **Whether Codex's two turn-id namespaces are equivalent.** **[OBS]** In my probe, `task_started.turn_id`
  was `01a0422c-...` (UUIDv7-shaped) while `custom_tool_call.internal_chat_message_metadata_passthrough.turn_id`
  was `93b938b8-...` (UUIDv4-shaped) *for the same turn*. Spec §4 already documents thread/turn identity
  divergence in 0.149.1; whether these two fields are the divergence it describes, or a third distinct
  identity, I did not establish. Any hook-based contract must pin down which `turn_id` the hook emits.
