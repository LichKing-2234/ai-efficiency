# LoongSuite Pilot: every path to Kiro CLI token or credit usage (#401)

Research date: 2026-08-27. Question: **are there other ways to make LoongSuite Pilot yield Kiro CLI token or
credit usage, beyond the single `kiro.credit_cost` custom attribute it emits today — or is there none?**

Context: the owner wants `ae-cli` to stop parsing each agent's own output and instead consume Pilot's
normalized output for Claude Code, Codex, and Kiro CLI. Kiro is the blocker.

Every claim below is tagged:

- **[SRC]** observed in source — repo + path + line, read at the pinned revision.
- **[DOC]** documented — official docs, with file/URL + section.
- **[OBS]** observed locally — real artifacts on this machine. No user conversation content is quoted; only
  schema shapes, key names, counts, and numeric usage values.
- **[INF]** inference on top of the above. Not verified.
- **[UNVERIFIED]** could not be established here. Called out rather than reasoned around.

Revisions pinned for this report:

- This repo: `main` @ `36bb67b1` (working tree).
- Upstream: `alibaba/loongsuite-pilot` `origin/main` @ `dea3c6b4` (fetched 2026-08-27). **Caveat [OBS]:** the
  local clone at `~/Desktop/AI_Native/GitHub/alibaba/loongsuite-pilot` is a **shallow clone** (4 commits).
  `git log -S` against it falsely attributes every file to the graft boundary `be9877e`. All history claims
  below come from the GitHub commits API, not from that clone.
- Local Kiro CLI: `kiro-cli 2.20.0` **[OBS]**. Pilot's Kiro fixtures were captured from **kiro-cli v2.8.0**
  **[SRC]** (`assets/hooks/kiro-cli/session-parser.mjs:20`). That version gap matters — see §2 and §5.

---

## 1. Answer

**There are other paths, but none of them produces a Kiro token count.** Every additional surface I found —
the local JSONL event log, OTLP trace spans, the `agents.d.local` definition override, a user-owned hook
wrapper, and an upstream schema change — yields **credit**, not token, because the gap is at Kiro's source and
not in Pilot's plumbing. I confirmed that three independent ways (§2). The most viable path is therefore not a
new extraction route but a **unit change**: consume the `kiro.credit_cost` attribute Pilot already emits, which
already flows into the normalized JSONL event log unmodified, needs only a one-line config addition
(`otlpTrace.spanAttributePassthroughPrefixes: ["kiro."]`) to also reach trace spans, and maps onto a data model
`ae-cli` already has — `UsageUnitCredit` **[SRC]** (`ae-cli/internal/attributionlocal/types.go:16`). Kiro then
becomes a credit-unit agent alongside token-unit Codex and Claude, with no synthesized numbers and no
heuristics. There is exactly one genuinely new lead that could change this answer: **Kiro CLI 2.20.0 is fully
wired client-side for per-turn tokens** — a `metadata` stream event carrying `inputTokens` / `outputTokens` /
`cachedTokens` feeds a `lastTurnTokens` value in the TUI context bar **[OBS]** — while Pilot's "token is
permanently unavailable" verdict rests on a July 2026 investigation of **v2.8.0**. Whether the 2.20.0 backend
actually populates those fields is one cheap experiment (§5), and it is the only thing that would reopen a
token path.

---

## 2. The contradiction, resolved

**The question.** `ae-cli/internal/attributionlocal/kiro_json.go` declares `input_token_count` /
`output_token_count` and maps them to `InputTokens` / `OutputTokens` **[SRC]** (lines 24-25, 71-72), implying
Kiro exposes per-turn token counts. Pilot declares `kiro.token_source: 'unavailable'` and issue #403 concluded
"Kiro has no request-level Token source". At least one had to be wrong.

**Resolution: both are literally true. Kiro *declares* the fields and does *not* populate them.** The
`ae-cli` struct tags are correct — those JSON keys exist. The values behind them are always zero. Pilot is
correct that no usable token number is available. #403's conclusion stands.

Evidence, in descending order of strength:

**(a) Pilot's own real-capture fixtures carry the fields, set to zero. [SRC]** All three Kiro sidecar fixtures
contain `input_token_count` and `output_token_count`, and in every case the value is `0`, while
`metering_usage` credits alongside them carry real, non-round floating-point values:

| Fixture | Line | `input_token_count` | `output_token_count` | `metering_usage` values |
|---|---|---|---|---|
| `tests/unit/hooks/kiro-cli/fixtures/session_sidecar.json` | 51 | `0` | `0` | `0.04262794941956882`, `0.0222537767827529` |
| `.../session_2prompt_sidecar.json` | 20, 34 | `0` | `0` | `0.01` ×4 |
| `.../session_mcp_sidecar.json` | 51 | `0` | `0` | `0.015`, `0.01` |

The credit values in `session_sidecar.json` are 17-significant-digit floats — unmistakably real backend
output, not hand-written fixture data. The zeros sit in the same object. **[INF]** A capture that produced
genuine credits and zero tokens in the same record is a populated-credit / unpopulated-token backend, not a
capture artifact.

**(b) The SQLite transcript has a richer token schema, entirely null. [SRC]**
`tests/unit/hooks/kiro-cli/fixtures/round3_conv_raw.json` is a real `conversations_v2.value` capture. Each of
its three `history[].request_metadata` objects declares a full five-field token breakdown — `total_tokens`,
`uncached_input_tokens`, `output_tokens`, `cache_read_input_tokens`, `cache_write_input_tokens` — and **all
five are `null` on all three entries** (`total_tokens` at lines 143, 312, 427; repeated under
`user_turn_metadata.requests[]` at 906, 1023). In the same document, `user_turn_metadata.usage_info` (line
1030) carries three populated credit entries: `0.030340291376451077`, `0.025912616749585407`,
`0.018781607296849086`. Same document, same turn: credits real, tokens null.

**(c) This repo's own SQLite reader independently agrees. [SRC]** `ae-cli/internal/attributionlocal/kiro_cli_sqlite.go`
reads the identical source (`conversations_v2`, keyed by workspace root, line 48) and its
`kiroCLIHistoryItem.RequestMetadata` struct (lines 32-38) declares **no token fields at all** — only
`request_id`, `context_usage_percentage`, `message_id`, and the two timestamps. It emits `UsageUnitCredit`
(line 115). The same is true of the Kiro IDE reader, `kiro_ide.go:167`. So two of this repo's three Kiro
parsers already model Kiro as credit-only; `kiro_json.go` is the lone outlier.

**(d) No test in this repo asserts a non-zero token. [SRC]** `kiro_json_test.go` is the only Kiro test file,
and it contains no reference to `InputTokens`, `OutputTokens`, `input_token_count`, or `output_token_count`.
That is why the always-zero behavior was never noticed.

**(e) Pilot's rule is "don't fabricate zero". [SRC]** `assets/hooks/kiro-cli/transcript-parser.mjs:21-23`
states the token fields are constantly null and that `usage.input/output_tokens` are therefore left null
rather than emitted as `0` (`不臆造 0` — "do not fabricate zero"). The hook repeats it at
`kiro-cli-hook-processor.mjs:743`. This is a deliberate design choice, not an oversight: emitting `0` would
be a synthesized usage number, which #401's constraints forbid.

**(f) Upstream reviewed and accepted this as a source limitation.** PR #36 (merged 2026-07-08 as `901d070de`)
states that an exhaustive investigation across ACP recordings, CodeWhisperer wire payloads, debug logs, AWS
metering, and a full SQLite table scan found no token anywhere, and declared Kiro credit-only. Reviewer
`linrunqi08` listed it under *positive practices* on 2026-07-08. PR #156 (`9b9cf0f32`, 2026-07-17) then wrote
it into the docs. Kiro CLI is the **only** agent whose "Token Usage" column reads `No` in `README.md:52` and
`docs/overview.md:31` **[SRC]**, and `docs/agents.md:22` says plainly: *"Token usage is not exposed by the
source."* **[DOC]**

**Practical consequence for this repo. [INF]** `kiro_json.go:71-72` is dead weight: it copies two always-zero
fields into `LocalToolUsageEvent`. It is not *wrong* (0 is what the source says), but it invites exactly the
misreading that prompted this research. Because `LocalToolUsageEvent.UsageUnit` is `credit` for that event
**[SRC]** (`kiro_json.go:68`), any consumer keying off `UsageUnit` is already safe; a consumer that reads
`InputTokens` unconditionally would silently record zero usage.

**Confidence.**

- That kiro-cli **v2.8.0** did not populate tokens: **high**. Three independent captures, two independent
  codebases, one reviewed upstream investigation.
- That kiro-cli **2.20.0** does not populate tokens: **[UNVERIFIED]**, and I would not assume it. See §3.5
  and §5 — the 2.20.0 client contains the complete token plumbing, and no one has re-tested since July.

**What would settle 2.20.0 definitively:** run one real Kiro CLI chat turn on 2.20.0, then read
`user_turn_metadatas[].input_token_count` in `~/.kiro/sessions/cli/<id>.json` and
`history[].request_metadata.total_tokens` in `conversations_v2`. Non-zero / non-null in either location
reopens the token path. Exact commands in §5.

---

## 3. Every path found

### 3.1 `kiro.credit_cost` in the normalized JSONL event log — available today, no change to Pilot

**Yields:** credit (float), per **LLM response step**, not per turn. Plus the sentinel
`kiro.token_source: 'unavailable'`. No tokens.

**How it works. [SRC]** The hook attaches `kiro.credit_cost` to each `llm.response` record
(`assets/hooks/kiro-cli-hook-processor.mjs:726-745`), and it survives into the normalized event because the
hook-record transform spreads the record wholesale — `...record` at
`src/inputs/base/hook-record-transform.ts:30` — with no attribute allowlist. `src/inputs/kiro-cli-log/kiro-cli-log-input.ts`
contains no token or credit handling at all (39 lines, verified); it is pure passthrough.

**Fork or upstream change needed:** **none**. This works on stock Pilot right now.

**Maintenance cost:** low, but with one real risk. `kiro.credit_cost` and `kiro.token_source` are **not
documented anywhere** — `git grep 'credit_cost\|token_source' -- docs/` returns nothing across the entire
upstream docs tree **[SRC]**. They are undocumented vendor-namespaced attributes pinned only by a unit test
(`tests/unit/hooks/kiro-cli/hook-processor.test.mjs:279-290`). An upstream refactor could rename them without
a docs change or a changelog entry. Mitigation: consume defensively and fail closed when the key is absent —
which is the correct behavior anyway, since a missing credit must never become a zero.

**Deterministic / fail-closed:** **yes, with one caveat.** The value is read directly from the source with no
inference. The caveat is the *alignment* of credit to step, covered in §3.6.

### 3.2 `spanAttributePassthroughPrefixes: ["kiro."]` — surfaces the same credit onto OTLP trace spans

**Yields:** the same credit value, additionally on ENTRY / AGENT / STEP / LLM / TOOL spans. No tokens.

**How. [DOC]** `docs/trace-output.md:317-320` documents `otlpTrace.spanAttributePassthroughPrefixes` as the
list of key prefixes the daemon surfaces onto spans; line 337 shows the exact analogous case (`"opencode."`
for the built-in `opencode.message.id` attribute). Line 341 states passthrough attributes are ordinary
top-level record fields and therefore appear in **both** the event log (SLS / JSONL) **and** trace spans.
Config:

```json
{ "otlpTrace": { "spanAttributePassthroughPrefixes": ["kiro."] } }
```

**[OBS]** This machine's `~/.loongsuite-pilot/config.json` currently has no `otlpTrace` block at all — only
`agents` and `jsonl` — so nothing is being surfaced to spans today.

**Fork or upstream change needed:** none. Config only. **Maintenance cost:** negligible.
**Deterministic / fail-closed:** yes — same value, same caveat as §3.1.

### 3.3 `agents.d.local/kiro-cli.json` override → a user-owned hook processor

This is the only real *extension point* Pilot offers for Kiro, and it is officially documented.

**How it works. [SRC]** `src/deployment/agent-def-loader.ts:33-52` loads built-in definitions from
`agents.d/*.json`, then loads `~/.loongsuite-pilot/agents.d.local/*.json` and merges by `id` with **local
winning** (`merged.set(def.id, def)` in the second loop). Validation checks only that `id`, `displayName`,
`deployMode`, and `detection` are present and that `deployMode` is one of six known values (lines 82-105);
`hook.hookCommand` is an unconstrained string with `$PILOT_DATA` / `$PILOT_DIR` / `~` expansion
(`resolveString`). There is no signature check and no command allowlist. **[DOC]**
`docs/agent-onboarding.md:76`: *"Built-in definitions are loaded from `agents.d/*.json`; local runtime
definitions can override them from `~/.loongsuite-pilot/agents.d.local/`."*

So dropping `~/.loongsuite-pilot/agents.d.local/kiro-cli.json` — a copy of `agents.d/kiro-cli.json` with
`hook.hookCommand` pointed at your own wrapper — is a supported, fork-free way to interpose your own
processor. **[OBS]** That directory does not exist on this machine yet.

**Yields:** whatever your wrapper emits. Realistically: the same credit, re-keyed into a namespace you
control and therefore stable against upstream renames. **It cannot manufacture tokens** — the wrapper reads
the same sidecar and the same SQLite rows.

**Hard limit on what a wrapper may emit. [DOC]** `docs/trace-output.md:342` and **[SRC]**
`assets/hooks/shared/resource-context.mjs:31-41`: the prefixes `gen_ai.`, `git.`, `workspace.`, `event.`,
`trace_`, `user.`, `cost_`, `agent.`, `time_unix_nano`, `observed_time_unix_nano` are **reserved and dropped**
for caller-supplied attributes, plus anything matching a sensitive-name regex. You therefore **cannot** map
Kiro credit into `gen_ai.usage.*` from outside — that namespace is closed to user attributes by design. A
wrapper must use its own namespace (e.g. `aeff.kiro.credit`).

**Maintenance cost: high.** Your wrapper either re-implements or shells out to the upstream 1048-line
`kiro-cli-hook-processor.mjs` plus its four `kiro-cli/*.mjs` modules, and must track their changes. Upstream
touched these files three times (all in July 2026) but the surrounding shared modules move constantly.

**Deterministic / fail-closed:** yes, if the wrapper is written that way. But it buys namespace stability,
not new data — a poor trade for the cost unless §3.1's undocumented-attribute risk actually materializes.

### 3.4 Upstream change: normalize credit into the event schema

**Yields:** credit as a first-class, documented field rather than a vendor attribute.

**Why it is needed. [SRC]** `docs/output-event-schema.md:67-76` defines `gen_ai.usage.*` as tokens and USD
only — `input_tokens`, `output_tokens`, `cache_read.input_tokens`, `cache_creation.input_tokens`,
`total_tokens`, and the five `*_cost` fields. There is no credit field and no usage-unit discriminator
anywhere in the schema. Confirmed by the starting facts and re-verified here.

**Shape:** add a documented `gen_ai.usage.credits` (or a `usage.unit` discriminator), and have the Kiro hook
populate it from `usage_info[].value` instead of the ad-hoc `kiro.credit_cost`.

**Fork or upstream change needed:** upstream PR. **Likelihood: unclear but not obviously hostile.** Upstream
has *no* open issue or PR on Kiro token/credit — zero Kiro issues exist at all; all four Kiro items are merged
PRs (#36, #117 revert, #119 re-land, #156 docs), and Kiro token handling has been untouched since `426fbebf6`
on 2026-07-13. The team's position is not "we refuse" but "the source does not provide it", and they already
built the credit-only compensating design deliberately. **[INF]** A PR that promotes credit to a documented
schema field is consistent with their stated design rather than opposed to it — but it is upstream-owned work
on an upstream timeline, so it cannot be a dependency for a near-term plan.

**Maintenance cost:** zero once merged. **Deterministic / fail-closed:** yes.

### 3.5 Re-verify tokens on kiro-cli 2.20.0 — the only path that could yield actual tokens

This is the one genuinely new finding, and the only lead not already closed.

**What I observed. [OBS]** Kiro CLI 2.20.0 on this machine ships four binaries under
`/Applications/Kiro CLI.app/Contents/MacOS/` — `kiro-cli` (103 MB), `kiro-cli-chat` (1.59 GB),
`kiro-cli-term`, `kiro_cli_desktop` — plus a bundled Bun runtime and a 13.5 MB `tui.js` at
`~/Library/Application Support/kiro-cli/tui.js`. The chat binary contains every relevant field name as a
literal string:

| String | Occurrences in `kiro-cli-chat` |
|---|---|
| `conversations_v2` | 30 |
| `input_token_count` / `output_token_count` | 13 / 13 |
| `uncached_input_tokens` | 15 |
| `cache_read_input_tokens` | 17 |
| `total_tokens` | 19 |
| `metering_usage` | 17 |
| `inputTokens` / `outputTokens` / `cachedTokens` (wire form) | 13 / 16 / 6 |

And `tui.js` contains a live consumer of those wire fields **[OBS]**:

```js
case "metadata" /* Metadata */:
  if (event.inputTokens !== undefined || event.outputTokens !== undefined) {
    get().setLastTurnTokens({
      input: event.inputTokens ?? 0,
      output: event.outputTokens ?? 0,
      cached: event.cachedTokens ?? 0
    });
  }
  break;
```

`lastTurnTokens` is exposed through `useContextState` alongside `contextUsagePercent` and `currentModel`
**[OBS]** — i.e. it feeds the context/status display. Separately, a `turn_summary` stream event carries
`meteringUsage` (credits), and a `/usage` slash command maps to `showUsagePanel` with backend-supplied
`usageData` **[OBS]** (`commandEffects.usage = "showUsagePanel"`).

**What this means. [INF]** The 2.20.0 client is wired end to end for per-turn tokens: wire struct → stream
event → TUI state → display, plus the persistence field names. The `!== undefined` guard means the client
tolerates their absence, so the plumbing's existence does **not** prove the server fills it. But it does mean
that *if* the backend ever starts returning tokens, they appear in the client with no client change — and
Pilot's `unavailable` verdict would silently become wrong.

**Why this is worth an hour. [INF]** Pilot's verdict was formed against v2.8.0 in July 2026
**[SRC]** (`session-parser.mjs:20`). Kiro is now at 2.20.0 — twelve minor versions later, and the CLI has
visibly been re-architected around a Bun/TypeScript TUI in the meantime. Nobody has re-tested. The test is
nearly free: run one turn and look at the screen (§5).

**Yields if it works:** real per-turn tokens at the source, which would make an upstream fix viable and would
turn Kiro into a token-unit agent. **If it fails**, `unavailable` is reconfirmed on current Kiro and §3.1
becomes the settled answer rather than the provisional one. Either outcome is decision-grade.

**Fork/upstream:** none to test; upstream PR to exploit. **Deterministic / fail-closed:** yes — it is a
direct read of a source field, no inference.

### 3.6 Alignment robustness — the real fragility in the credit path

This is not a separate path but a defect risk attaching to §§3.1-3.4, and #401's determinism constraint makes
it load-bearing.

**How credit is bound to a step. [SRC]** By **positional index**, not by any shared identifier:

- SQLite transcript path — `transcript-parser.mjs:255-256, 288`: `credits = utm.usage_info.map(u => u.value)`
  and `creditIndex: i < credits.length ? i : -1`, where `i` is the index into `history[]`.
- Session-JSONL path — `session-parser.mjs:117-124, 239`: credits are collected by **flattening
  `metering_usage` across all turns into one array**, then `creditIndex: assistantIndex < credits.length ?
  assistantIndex : -1`, where `assistantIndex` counts `AssistantMessage` lines across the whole session.

The hook comment calls this out as empirically derived: `// credit 对齐到 step（usage_info 与 history 等长；
round3 实证对齐）` — "aligned empirically in round 3" **[SRC]** (`kiro-cli-hook-processor.mjs:726`).

**The alignment holds in every fixture. [SRC]** I verified the counts match exactly:

| Fixture | `AssistantMessage` lines | flattened credits | match |
|---|---|---|---|
| `session_interactive.jsonl` / `session_sidecar.json` | 2 | 2 (1 turn × 2) | ✅ |
| `session_2prompt_interactive.jsonl` / `session_2prompt_sidecar.json` | 4 | 4 (2 turns × 2) | ✅ |
| `session_mcp_interactive.jsonl` / `session_mcp_sidecar.json` | 2 | 2 (1 turn × 2) | ✅ |
| `round3_conv_raw.json` | 3 `history[]` entries | 3 `usage_info[]` | ✅ |

**But a sibling array in the same object does *not* line up, and this repo depends on it. [SRC]** In
`round3_conv_raw.json`, `user_turn_metadata.requests[]` has **2** entries while `history[]` and `usage_info[]`
both have **3**. Pilot is unaffected — it indexes credit against `history`, never against `requests`. But
`ae-cli/internal/attributionlocal/kiro_cli_sqlite.go:95` sets `RequestCount = len(doc.UserTurnMetadata.Requests)`,
so for this real capture `ae-cli` would report **2 requests for a turn that actually made 3**. **[INF]** On the
same evidence, `len(history)` (or `len(usage_info)`) is the correct request count and `requests[]` appears to
be a partial or differently-scoped array. Worth confirming in step 3 of §5 before relying on `RequestCount`
for anything billable.

**What would break it. [INF]** Any event that makes the two sequences differ in length or order silently
mis-assigns credit — there is no key to detect the mismatch, and both parsers fail *open* by clamping to `-1`
only when the index runs past the end. Candidate breakers, none of which appear in any fixture:

- **An aborted or interrupted turn** — an `AssistantMessage` written with no metering entry emitted, shifting
  every subsequent credit by one.
- **A retried request** — a metering entry with no corresponding assistant message, shifting the other way.
- **Parallel tool calls** — safe as written, since one `AssistantMessage` carries multiple `toolUse` entries
  and still counts as one step. Worth confirming rather than assuming.
- **Subagents / `/spawn`** — `tui.js` exposes a `spawn: "spawnSession"` command **[OBS]**; whether a spawned
  session's metering lands in the parent's `user_turn_metadatas` is **[UNVERIFIED]**.
- **A resumed session** — `readSessionJsonl` picks the single most-recently-updated sidecar whose `cwd`
  matches, then re-parses the **whole** JSONL from the start (`session-parser.mjs:302-383`). Step-level dedup
  is handled upstream by `emitted-steps` state, but the flattened credit array is rebuilt each time, so a
  sidecar that was truncated or compacted between reads would re-index everything.
- **Compaction** — `tui.js` handles a `compaction_status` event **[OBS]**; if compaction rewrites history
  while metering accumulates, lengths diverge.

**[INF] Verdict:** the credit value itself is deterministic; its *attribution to a specific step* is an
empirical positional join that is fail-open, not fail-closed. For #401's purposes this matters only if credit
must be bound to a sub-turn step. **If credit is consumed at session or turn granularity — summing
`usage_info[].value` — the alignment problem disappears entirely, because the sum is order-independent.**
That is what `ae-cli` already does today: both `kiro_cli_sqlite.go:89-94` and `kiro_json.go:56-61` accumulate
credits into a single total per conversation/turn rather than per step. **[INF]** Keeping that granularity is
the right call; adopting Pilot's per-step credit would import a fragility this repo does not currently have.

### 3.7 Other Pilot output surfaces — checked, nothing extra

**[SRC]/[DOC]** All three normalized outputs carry the same record. `docs/trace-output.md:341` states
passthrough attributes appear in both the event log and the spans. `docs/http-output.md` POSTs "normalized
Pilot events" with no separate schema. `docs/local-jsonl-output.md` contains no attribute filtering language.
There is no raw/debug output mode that exposes more than the hook already wrote, and the hook is the thing
that discards nothing — it never had tokens to discard. **Conclusion: no output surface carries usage the
JSONL does not.** The only per-surface difference is that trace spans require the §3.2 prefix opt-in.

---

## 4. Ruled out

| Path | Why rejected |
|---|---|
| **Read `input_token_count` / `output_token_count` from the sidecar** | Present in the schema, **always `0`** in every real capture **[SRC]** (§2a). This is what `kiro_json.go:71-72` does today; it records zero usage, not real usage. |
| **Read `request_metadata.{total_tokens,uncached_input_tokens,output_tokens,cache_read_input_tokens,cache_write_input_tokens}` from `conversations_v2`** | All five **`null`** on every history entry in the real capture **[SRC]** (§2b). |
| **Convert credit → token** | No deterministic published mapping. The only rate signal in the data is `model_info.rate_multiplier: 1.0` / `rate_unit: "Credit"` **[SRC]** (`round3_conv_raw.json`), which converts requests to credits, not credits to tokens. Any credit→token figure would be an estimate. **#401 forbids heuristics; a credit→token estimate is a heuristic.** Ruled out on the constraint, independent of feasibility. |
| **Derive tokens from `context_usage_percentage` × `context_window_tokens`** | Both fields are real and populated (`context_window_tokens: 1000000`, `context_usage_percentage: 0.7571` **[SRC]**), and the product is arithmetically tempting. But context occupancy is cumulative conversation state, not per-request input/output tokens; it excludes output entirely and double-counts cache. It is a model-based inference — a heuristic. Ruled out. |
| **Emit `gen_ai.usage.*` for Kiro via `LOONGSUITE_PILOT_SPAN_ATTRIBUTES` or `globalSpanAttributes`** | Structurally impossible. `gen_ai.` is a reserved prefix and caller-supplied keys matching it are **dropped by the hook** **[DOC]** `docs/trace-output.md:342`, **[SRC]** `assets/hooks/shared/resource-context.mjs:31-41`. Only `gen_ai.session.id` and `gen_ai.user.id` are excepted. Separately, `globalSpanAttributes` is static key/value read once at startup — it cannot carry per-event usage. |
| **`loongsuite-pilot agent register` a custom Kiro agent** | The register subcommand supports **only** `pi-sdk` **[SRC]** (`src/pi-sdk/pi-sdk-agent-cli.ts:232`; the sole documented form in `docs/agent-onboarding.md:31`). It requires an agent built on `@earendil-works/pi-coding-agent`. Kiro is not. Not applicable. |
| **Patch the installed `~/.loongsuite-pilot/hooks/kiro-cli-hook-processor.mjs` in place** | **[OBS]** the installed file is byte-identical to upstream `origin/main` (1048 lines, `diff` clean), so patching is *possible*. But it is not durable — assets are redeployed from `assets/hooks` on install/upgrade, and `src/core/hook-watchdog.ts` repairs Pilot-owned hook state. §3.3's `agents.d.local` override is the supported version of the same idea. And it would not yield tokens regardless. |
| **Kiro IDE (desktop) execution files as a token source** | `ae-cli/internal/attributionlocal/kiro_ide.go` reads `usageSummary[].usage` and emits `UsageUnitCredit` (line 167) — credit, same as the CLI. Upstream PR #165 (Kiro Desktop CDP capture) is open but stale since 2026-07-23 and its description never mentions tokens or credits. |
| **Wait for an upstream fix** | There is none to wait for: zero Kiro issues upstream, no open Kiro token/credit PR, and no commit to any Kiro file since 2026-07-13 while ~200 other PRs shipped. Not planned, not in progress. |

---

## 5. What must be verified on a real machine

Someone with Kiro CLI installed should run this. Steps 1-3 are the decision-grade ones; **step 2 is the whole
point of this section** and takes about five minutes.

**0. Record the version.** `kiro-cli --version`. This report's local observation is `2.20.0`; Pilot's evidence
is from `2.8.0`. If you are on 2.8.x, §2 already answers you and step 2 is redundant.

**1. Snapshot the two persistence sources before the turn.**

```bash
cp ~/Library/Application\ Support/kiro-cli/data.sqlite3 /tmp/kiro-before.sqlite3
ls -la ~/.kiro/sessions/cli/
sqlite3 /tmp/kiro-before.sqlite3 "select count(*) from conversations_v2"
```

**[OBS]** On this machine that count is `0` and `~/.kiro/sessions/cli/` holds a single empty session from
2026-04-27 (`user_turn_metadatas: []`, zero-byte `.jsonl`). **[UNVERIFIED]** I could not tell whether that
means "no chat has run recently" or "2.20.0 persists chat somewhere else" — the `conversations_v2` schema
string is still present 30× in `kiro-cli-chat`, which suggests the former, but only a real turn proves it.
**This matters directly to `ae-cli`:** `ParseKiroCLISQLite` queries `conversations_v2`
(`kiro_cli_sqlite.go:48`), so if 2.20.0 stopped writing that table, this repo's Kiro CLI collection is already
silently dead.

**2. Run one real turn in an interactive Kiro CLI session and watch the screen.** Ask something that forces at
least one tool call. Then:

- **Look at the context/status bar for a token readout.** `tui.js` sets `lastTurnTokens` from the `metadata`
  stream event's `inputTokens` / `outputTokens` / `cachedTokens` and exposes it via `useContextState`
  **[OBS]**. **If real token numbers appear on screen, the source has tokens and Pilot's `unavailable` is
  stale — this reopens the entire token path and is the single highest-value observation in this checklist.**
- Run **`/usage`** and record what it reports — units (credits? tokens? requests?) and granularity (session?
  billing period?). `commandEffects.usage → showUsagePanel(result.data)` **[OBS]**; the payload comes from the
  backend and I could not inspect it statically.
- Run **`/context`** and record whether it breaks context down in tokens.

**3. Dump both persistence sources after the turn and check for populated tokens.**

```bash
# sidecar
python3 -c "
import json,glob,os
f=max(glob.glob(os.path.expanduser('~/.kiro/sessions/cli/*.json')), key=os.path.getmtime)
d=json.load(open(f))
for i,t in enumerate(d['session_state']['conversation_metadata']['user_turn_metadatas']):
    print(i, {k:v for k,v in t.items() if 'token' in k or 'metering' in k or 'request' in k})
"

# sqlite transcript
sqlite3 ~/Library/Application\ Support/kiro-cli/data.sqlite3 \
  "select value from conversations_v2 order by updated_at desc limit 1" \
| python3 -c "
import json,sys
d=json.load(sys.stdin)
for i,h in enumerate(d.get('history',[])):
    rm=h.get('request_metadata',{})
    print(i,{k:rm.get(k) for k in ('total_tokens','uncached_input_tokens','output_tokens','cache_read_input_tokens','cache_write_input_tokens')})
print('usage_info:', d.get('user_turn_metadata',{}).get('usage_info'))
"
```

**Decision rule.** `input_token_count > 0` **or** any `request_metadata` token field non-null ⇒ tokens exist
at the source; escalate to an upstream Pilot PR and stop treating Kiro as credit-only. All zero/null ⇒ §2 is
reconfirmed on current Kiro; adopt §3.1 + §3.2 and move on.

**4. Confirm what Pilot actually emits for that turn.** With the Kiro agent enabled, inspect the normalized
JSONL for the same turn and grep for `kiro.credit_cost`, `kiro.token_source`, and `gen_ai.usage`. Verify that
credit is present, that no `gen_ai.usage.*` key appears, and that credit values match `usage_info[].value`
from step 3. **[OBS]** `~/.loongsuite-pilot/logs/kiro-cli/` and `~/.loongsuite-pilot/state/kiro-cli/` are both
empty on this machine, so I could not do this end-to-end.

**5. Test the §3.2 config.** Add `{"otlpTrace": {"spanAttributePassthroughPrefixes": ["kiro."]}}` to
`~/.loongsuite-pilot/config.json`, restart Pilot, run another turn, confirm `kiro.credit_cost` appears on the
LLM span. **[OBS]** This machine's config has no `otlpTrace` block at all, so this is untested here.

**6. Probe the alignment breakers from §3.6.** For each of: abort a turn mid-stream (Ctrl-C), trigger a
retry, issue a prompt causing parallel tool calls, `/spawn` a subagent, resume a session. After each, compare
`len(usage_info)` against the number of `history[]` entries (SQLite path) and `len(flattened metering_usage)`
against the number of `AssistantMessage` lines (session path). **Any inequality is a silent credit
mis-attribution in Pilot's per-step output.** If you adopt per-turn credit sums (§3.6) this test is
unnecessary.

**7. Cross-platform check for `ae-cli`.** **[SRC]** `findKiroCLISQLiteFiles` (`scanner.go:262-263`) hardcodes
`~/Library/Application Support/kiro-cli/data.sqlite3` — macOS only. Pilot's `db-path.mjs` resolves
`KIRO_CLI_DB`, then `KIRO_CLI_DATA_DIR`, then per-platform defaults including `$XDG_DATA_HOME` and
`%APPDATA%`. On Linux or Windows this repo finds nothing. Independent of the token question, but it will bite.

---

## 6. Primary sources

**Upstream `alibaba/loongsuite-pilot` @ `origin/main` = `dea3c6b4` (2026-08-27).**

| Claim | Source |
|---|---|
| Token declared permanently null; credit is a custom attribute only | `assets/hooks/kiro-cli-hook-processor.mjs:22` |
| Credit aligned to step by empirical index; `kiro.token_source: 'unavailable'`; `kiro.credit_cost` emitted | `assets/hooks/kiro-cli-hook-processor.mjs:726-745` |
| Same sentinel on the synthesized final step | `assets/hooks/kiro-cli-hook-processor.mjs:890` |
| Transcript token fields all null; "do not fabricate zero"; credit from `usage_info` | `assets/hooks/kiro-cli/transcript-parser.mjs:21-23` |
| Credit array built from `usage_info[].value`; `creditIndex` = history index | `assets/hooks/kiro-cli/transcript-parser.mjs:255-256, 288` |
| Session path: credits flattened across turns; `creditIndex` = assistant-message index | `assets/hooks/kiro-cli/session-parser.mjs:117-124, 239` |
| Fixtures captured from kiro-cli **v2.8.0** | `assets/hooks/kiro-cli/session-parser.mjs:20` |
| Session sidecar discovery: newest `cwd`-matching sidecar, full re-parse | `assets/hooks/kiro-cli/session-parser.mjs:302-383` |
| Cross-platform DB resolution (`KIRO_CLI_DB`, `KIRO_CLI_DATA_DIR`, XDG, `%APPDATA%`) | `assets/hooks/kiro-cli/db-path.mjs:35-71` |
| `input_token_count: 0`, `output_token_count: 0` with real credits | `tests/unit/hooks/kiro-cli/fixtures/session_sidecar.json:51`; `session_2prompt_sidecar.json:20,34`; `session_mcp_sidecar.json:51` |
| `request_metadata` token fields all `null`; `usage_info` credits populated | `tests/unit/hooks/kiro-cli/fixtures/round3_conv_raw.json:143,312,427,906,1023,1030` |
| Behavior pinned by test: usage tokens undefined, `token_source` unavailable, `credit_cost` numeric | `tests/unit/hooks/kiro-cli/hook-processor.test.mjs:279-290` |
| Hook records pass through unfiltered (`...record`) | `src/inputs/base/hook-record-transform.ts:30` |
| Kiro log input is pure passthrough, no token/credit handling | `src/inputs/kiro-cli-log/kiro-cli-log-input.ts` (39 lines) |
| `agents.d.local` overrides built-ins by `id`, local wins; no hookCommand constraint | `src/deployment/agent-def-loader.ts:33-52, 82-105` |
| `agents.d.local` documented as the local override directory | `docs/agent-onboarding.md:76` |
| `agent register` supports only `pi-sdk` | `docs/agent-onboarding.md:31`; `src/pi-sdk/pi-sdk-agent-cli.ts:232` |
| Kiro agent definition: hook deploy, `~/.kiro/agents/pilot-kiro.json`, requires `node:sqlite` ≥ 22.5.0 | `agents.d/kiro-cli.json` |
| **"Token usage is not exposed by the source."** | `docs/agents.md:22` |
| Kiro CLI is the only agent with Token Usage = `No` | `README.md:52`; `docs/overview.md:31` |
| `gen_ai.usage.*` schema is tokens + USD only; no credit field | `docs/output-event-schema.md:67-76` |
| `spanAttributePassthroughPrefixes` config; `opencode.` precedent | `docs/trace-output.md:317-320, 337` |
| Passthrough attributes reach both event log and spans | `docs/trace-output.md:341` |
| `gen_ai.` and other reserved prefixes dropped by the hook | `docs/trace-output.md:342`; `assets/hooks/shared/resource-context.mjs:31-41` |
| `kiro.credit_cost` / `kiro.token_source` undocumented | `git grep 'credit_cost\|token_source' -- docs/` → no results |
| Hook watchdog repairs Pilot-owned hook state | `src/core/hook-watchdog.ts:192, 346, 479` |

**Upstream history (GitHub commits/search API — *not* the shallow local clone).**

| Item | Detail |
|---|---|
| PR #36 `feat(kiro-cli): add Kiro CLI probe` | merged 2026-07-08, `901d070de`. Body declares token unavailable across wire payloads, SQLite, logs, AWS metering, MCP events; credit-only. |
| Review of #36 by `linrunqi08` | 2026-07-08, lists "token 恒 null 不臆造、credit-only 语义严谨" under positive practices. |
| PR #117 `Revert "feat(kiro-cli)..."` | merged 2026-07-09, `b8e8ac2a7`. Reverted for unrelated reasons (event-emitter pollution, uninstall data loss) — **not** token-related. |
| PR #119 `add Kiro CLI probe with all fixes (v2)` | merged 2026-07-13, `426fbebf6`. Re-landed the same credit-only design. Last commit to any Kiro file. |
| PR #156 `docs: add Kiro CLI to supported agents` | merged 2026-07-17, `9b9cf0f32`. Added "Token usage is not exposed by the source." |
| PR #165 `feat(kiro-desktop-session)` | open, stale since 2026-07-23, merge-conflicted; Kiro **Desktop** CDP capture; body never mentions tokens or credits. |
| Kiro **issues** | zero. `token_source` search across issues+PRs: zero hits. No planned or in-progress Kiro token work. |

**This repo `LichKing-2234/ai-efficiency` @ `36bb67b1`.**

| Claim | Source |
|---|---|
| Sidecar parser declares and maps the two token fields | `ae-cli/internal/attributionlocal/kiro_json.go:24-25, 71-72` |
| Same parser emits `UsageUnitCredit` and sums `metering_usage` credits | `ae-cli/internal/attributionlocal/kiro_json.go:56-61, 68` |
| SQLite parser declares **no** token fields; credit only | `ae-cli/internal/attributionlocal/kiro_cli_sqlite.go:32-38, 89-94, 115` |
| SQLite parser queries `conversations_v2` keyed by workspace root | `ae-cli/internal/attributionlocal/kiro_cli_sqlite.go:48` |
| Kiro IDE parser is credit-only | `ae-cli/internal/attributionlocal/kiro_ide.go:150-152, 167` |
| `UsageUnit` already distinguishes token vs credit | `ae-cli/internal/attributionlocal/types.go:12-17, 31` |
| Scanner wires all three Kiro parsers | `ae-cli/internal/attributionlocal/scanner.go:114, 132, 151` |
| SQLite discovery is macOS-only | `ae-cli/internal/attributionlocal/scanner.go:262-263` |
| No test asserts non-zero Kiro tokens | `ae-cli/internal/attributionlocal/kiro_json_test.go` (no token references) |
| #401 constraints: deterministic, fail-closed, heuristics forbidden | GitHub issue #401 |
| #403 conclusion: "Kiro has no request-level Token source" | GitHub issue #403 — **corroborated by this research** |

**Local observations (kiro-cli 2.20.0, macOS, this machine).**

| Observation | Where |
|---|---|
| Four binaries incl. `kiro-cli-chat` (1.59 GB) | `/Applications/Kiro CLI.app/Contents/MacOS/` |
| Chat binary contains `conversations_v2` ×30, `input_token_count` ×13, `metering_usage` ×17, `total_tokens` ×19, `inputTokens` ×13, `cachedTokens` ×6 | `strings`/`grep -a` on `kiro-cli-chat` |
| `metadata` stream event → `setLastTurnTokens({input,output,cached})`; `lastTurnTokens` in `useContextState`; `turn_summary` carries `meteringUsage`; `commandEffects.usage → showUsagePanel` | `~/Library/Application Support/kiro-cli/tui.js` |
| `conversations_v2` and `conversations` both **0 rows**; only `history` (1548 shell rows) and `state` (17 keys) populated | `~/Library/Application Support/kiro-cli/data.sqlite3` |
| One session sidecar, 2026-04-27, `user_turn_metadatas: []`, zero-byte `.jsonl` | `~/.kiro/sessions/cli/` |
| `run-receipts/` and `telemetry-export-drops.lock` exist but hold no data | `~/Library/Application Support/kiro-cli/` |
| Installed Kiro hook is byte-identical to upstream (1048 lines) | `~/.loongsuite-pilot/hooks/kiro-cli-hook-processor.mjs` |
| Pilot config has only `agents` + `jsonl`; no `otlpTrace`; Kiro enabled | `~/.loongsuite-pilot/config.json` |
| `agents.d.local/` does not exist; Kiro log and state dirs empty | `~/.loongsuite-pilot/` |

**Not verified.** Whether kiro-cli 2.20.0's backend populates any token field; what `/usage` reports and in
what unit; whether 2.20.0 still writes `conversations_v2` during a real chat; whether subagent/`spawn` or
compaction breaks the credit-to-step index alignment; whether Kiro's own docs publish a credit↔token mapping
(no such mapping appears anywhere in the captured data).
