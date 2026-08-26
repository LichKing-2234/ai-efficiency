# LoongSuite Exact Evidence Envelope Research

Status: decision-ready research, no production integration performed
Date: 2026-08-26
Source snapshot: `alibaba/loongsuite-pilot@be9877ec437b1c50b6b7e70e431719fdd78b01ae`

## Decision Summary

LoongSuite Pilot is a credible **forensics and normalization layer** for Codex
and Claude Code, but it is not a commit-attribution authority by itself.

- A common metadata-only Token envelope is viable in principle for **Codex and
  Claude Code**: agent type, exact session identity, turn identity, terminal
  completion evidence, model/response identity when exposed, and Token
  components can coexist on one normalized event/trace model.
- It is **not universal across Codex, Claude Code, and Kiro CLI**. Alibaba Cloud
  explicitly documents that Kiro CLI's current source does not provide Token
  usage, and the current adapter marks Token as unavailable rather than
  inventing zero.
- The normalized schema has repository, branch, and workspace fields, but no
  commit SHA field. Therefore LoongSuite cannot satisfy the product goal by
  itself. Existing `ae-cli` deterministic Git checkpoint/commit proof remains
  the only component authorized to bind a Token claim to a commit.
- The current LoongSuite Codex adapter reads the same Codex transcript facts
  (`turn_context`, `task_complete`, `token_count`) that `ae-cli` can read. It is
  useful as an alternate parser and query surface, not independent provider or
  Relay proof.
- The recommended disposition is a bounded shadow POC. Do not make LoongSuite
  a production dependency until exact joins, privacy controls, delivery
  acknowledgement, and retention have been proven with pinned versions.

This result preserves the current architecture boundary: LoongSuite may
augment a candidate claim with exact Token/turn evidence, but it must never
replace Relay identity, Token ledger reconciliation, or Git commit proof.

## Evidence Classification

The sections below use these terms deliberately:

- **Documented fact**: stated by current first-party product documentation.
- **Observed source fact**: implemented in the cited first-party source at the
  pinned commit; it can change in later Pilot releases.
- **Inference**: a conclusion from multiple documented/source facts that still
  needs an end-to-end fixture.
- **Unknown**: not established by current public first-party material.

No conclusion below treats timestamp, cwd, path, model name, Token similarity,
or Git branch as exact identity.

## What LoongSuite Emits

**Documented fact.** Alibaba Cloud describes LoongSuite Pilot as a local
collector that discovers AI coding agents, installs hooks/plugins, normalizes
their activity, and sends logs or traces. The supported-agent table states
that Codex and Claude Code support trace, log, Token, conversation, and tool
collection; Kiro CLI supports trace/log/conversation/tool collection but not
Token collection. [Alibaba Cloud: AI Coding Agent integration](https://help.aliyun.com/zh/cms/cloudmonitor-2-0/ai-application-access-ai-coding-agent/)

**Observed source fact.** The normalized event contract contains:

| Evidence need | Pilot field | Contract strength |
|---|---|---|
| Agent/tool family | `gen_ai.agent.type` | Required |
| Session/conversation | `gen_ai.session.id` | Conditional when the source has session context |
| Turn | `gen_ai.turn.id` | Recommended |
| LLM response | `gen_ai.response.id` | Recommended |
| Gateway/client request | `gen_ai.request.id` | Recommended |
| Completion | `gen_ai.response.finish_reasons` plus turn boundary | Recommended/derived |
| Token | `gen_ai.usage.*` | Recommended when the source exposes it |
| Repository context | `git.domain`, `git.repo`, `git.branch`, `workspace.*` | Recommended |

The content-bearing fields (`gen_ai.input.messages`,
`gen_ai.output.messages`, tool arguments/results) are explicitly opt-in in the
schema. The schema does not define a commit SHA, commit ID, revision ID, or
checkpoint field. [Pilot output event schema](https://github.com/alibaba/loongsuite-pilot/blob/be9877ec437b1c50b6b7e70e431719fdd78b01ae/docs/output-event-schema.md#field-reference)

**Observed source fact.** Pilot log outputs (JSONL, SLS, HTTP) receive event
records, while the trace output converts the same records into OTLP spans.
Trace export reports usage on each LLM child span and also aggregates the same
usage on its parent AGENT span; summing both levels double counts Token.
[Pilot trace output](https://github.com/alibaba/loongsuite-pilot/blob/be9877ec437b1c50b6b7e70e431719fdd78b01ae/docs/trace-output.md#token-usage-aggregation)

## Agent-Specific Findings

### Codex

**Observed source fact.** Codex `0.149.1` supports separate OTLP log, trace,
and metrics exporters. Its exported `turn` span carries exact `thread.id`,
`turn.id`, model, and per-turn Token delta components. A child
`handle_responses` span records `otel.name=completed` plus per-response Token
components only when Codex receives `ResponseEvent::Completed`; this is a
stronger completion witness than timestamp or transport proximity.
[Codex OTel configuration](https://github.com/openai/codex/blob/rust-v0.149.1/codex-rs/config/src/types.rs#L527-L591),
[turn span and Token fields](https://github.com/openai/codex/blob/rust-v0.149.1/codex-rs/core/src/tasks/mod.rs#L367-L380),
[turn Token recording](https://github.com/openai/codex/blob/rust-v0.149.1/codex-rs/core/src/tasks/mod.rs#L665-L719),
and [completed-response recording](https://github.com/openai/codex/blob/rust-v0.149.1/codex-rs/core/src/session/turn.rs#L2253-L2293)

**Observed source fact.** The WebSocket request telemetry records `success`
but does not attach an upstream Request ID. The HTTP API request event can
carry `auth.request_id`, but that is not a universal WebSocket identity.
Therefore LoongSuite may prove exact completed Token-to-turn correlation for
current WebSocket turns, but it cannot manufacture independent Relay Request
proof.
[Codex WebSocket telemetry](https://github.com/openai/codex/blob/rust-v0.149.1/codex-rs/otel/src/events/session_telemetry.rs#L688-L720)
and [HTTP request telemetry](https://github.com/openai/codex/blob/rust-v0.149.1/codex-rs/otel/src/events/session_telemetry.rs#L569-L624)

Codex batches trace export in process memory and flushes on provider shutdown;
it has no durable trace replay ledger. User prompts and tool content are
excluded from trace-safe business events, but the trace exporter accepts all
spans, so a production privacy contract still needs a collector-side field
allowlist.
[Codex batch trace provider](https://github.com/openai/codex/blob/rust-v0.149.1/codex-rs/otel/src/provider.rs#L496-L585),
[provider shutdown](https://github.com/openai/codex/blob/rust-v0.149.1/codex-rs/otel/src/provider.rs#L94-L110),
and [export routing policy](https://github.com/openai/codex/blob/rust-v0.149.1/codex-rs/otel/src/provider.rs#L297-L332)

**Documented fact.** The Codex app-server event surface is stronger for local
identity: `turn/started` and `turn/completed` carry a turn object, and turn
events expose `threadId`/`turnId`; `turn/completed` distinguishes completed,
interrupted, and failed turns. `thread/tokenUsage/updated` is a separate
thread-level event. [Official OpenAI documentation: Codex App Server events](https://learn.chatgpt.com/docs/app-server#events)

**Observed source fact.** The current Pilot Codex adapter parses native
`turn_context.payload.turn_id`, accepts `task_complete` only when its `turn_id`
matches the expected turn, accepts `turn_aborted` as interrupted, and assigns
`token_count` only to a response wave with response evidence. A repeated
cumulative sample is not treated as a new authoritative close.
[Codex transcript extractor](https://github.com/alibaba/loongsuite-pilot/blob/be9877ec437b1c50b6b7e70e431719fdd78b01ae/src/inputs/codex-transcript/codex-transcript-extractor.ts#L253-L385)

The emitted turn ID is deterministic: `<thread-id>:<native-turn-id>`. Each LLM
response carries response ID, finish reason, and input/output/cache/total Token
components. Completed tools have call/result IDs and status.
[Codex transcript builder](https://github.com/alibaba/loongsuite-pilot/blob/be9877ec437b1c50b6b7e70e431719fdd78b01ae/src/inputs/codex-transcript/codex-transcript-builder.ts#L41-L120)

**Inference.** Codex is the strongest LoongSuite candidate because the native
turn ID survives normalization. It is still an augmentation path, not a fix
for `invalid_structured_mutation` / `missing_structured_mutation`: Pilot emits
no deterministic commit proof. The POC must also verify whether a real
provider/Relay request ID is populated; the schema only says
`gen_ai.request.id` is recommended, and the cited Codex builder does not set
it.

### Claude Code

**Documented fact.** Claude Code's native opt-in OTel tracing creates one
`claude_code.interaction` span per prompt and nests `claude_code.llm_request`
and tool spans beneath it. Every span carries `session.id`; the interaction
has `interaction.sequence`; an LLM request carries input/output/cache Token,
server and client request IDs, attempt count, and explicit `success`. The
`client_request_id` correlation fields require Claude Code `2.1.214` or later.
[Claude Code tracing](https://code.claude.com/docs/en/monitoring-usage#traces-beta),
[span attributes](https://code.claude.com/docs/en/monitoring-usage#span-attributes),
and [event correlation](https://code.claude.com/docs/en/monitoring-usage#event-correlation-attributes)

The native `tool_use_id` also matches the identifier passed to hooks, providing
an exact tool-to-hook join without file, path, or time similarity. Prompt,
response, tool detail, and tool content are redacted or disabled by default,
although OAuth `user.email` is included and must be stripped for a
metadata-only envelope.
[Claude Code tool result event](https://code.claude.com/docs/en/monitoring-usage#tool-result-event)
and [security and privacy](https://code.claude.com/docs/en/monitoring-usage#security-and-privacy)

**Observed source fact.** The current Claude adapter splits the first-party
transcript by Claude `promptId`, groups streaming assistant records by native
message ID, and reads native usage and stop reason. It constructs one normalized
turn per split and one Token-bearing response per LLM call.
[Claude transcript parser](https://github.com/alibaba/loongsuite-pilot/blob/be9877ec437b1c50b6b7e70e431719fdd78b01ae/assets/hooks/claude-code/transcript-parser.mjs#L198-L248)
and [turn split](https://github.com/alibaba/loongsuite-pilot/blob/be9877ec437b1c50b6b7e70e431719fdd78b01ae/assets/hooks/claude-code/transcript-parser.mjs#L459-L518)

The adapter emits session ID, a deterministic collector turn ID
`<session-id>:t<N>`, native Anthropic message ID as response ID, stop reason,
and input/output/cache/total Token components.
[Claude turn builder](https://github.com/alibaba/loongsuite-pilot/blob/be9877ec437b1c50b6b7e70e431719fdd78b01ae/assets/hooks/claude-code-hook-processor.mjs#L849-L1009)

**Inference.** Claude Code proves that the semantic envelope can extend beyond
Codex. However, Pilot's exported turn ID is collector-derived rather than the
native `promptId`; the source `promptId` is used to split turns but is not the
exported `gen_ai.turn.id`. A POC must preserve the native `promptId` mapping or
join through the exact native `tool_use_id`, then prove stable restart/replay
behavior before the normalized ID can authorize an external claim.

The local binaries observed during this research are Codex `0.149.1` and Claude
Code `2.1.220`. The follow-up must pin those exact versions in its evidence;
this research read their version output and official contracts but did not
execute a telemetry canary.

### Kiro CLI

**Documented fact.** Alibaba Cloud states that Kiro CLI's current source does
not provide Token usage. [Alibaba Cloud: AI Coding Agent integration](https://help.aliyun.com/zh/cms/cloudmonitor-2-0/ai-application-access-ai-coding-agent/)

**Observed source fact.** The adapter comments and output agree: it writes
`kiro.token_source=unavailable`, optionally records Kiro credit, and deliberately
does not synthesize zero Token. Its turn ID can also contain collector run
segmentation (`<session>:t<N>:r<N>`), including time-gap fallback for SQLite
sources. [Kiro adapter](https://github.com/alibaba/loongsuite-pilot/blob/be9877ec437b1c50b6b7e70e431719fdd78b01ae/assets/hooks/kiro-cli-hook-processor.mjs#L518-L745)

**Decision.** Kiro cannot participate in a Token-to-commit envelope today. It
should be recorded as `token_source_unavailable`, not estimated from credit,
time, model, or neighboring events.

**Documented fact.** Kiro's first-party enterprise activity report is a daily
per-user aggregate of conversations, credits, messages, and model message
counts, not request-level Token. Its optional prompt logs include request and
conversation identifiers but also prompt/response content and still do not
provide Token components.
[Kiro per-user activity](https://kiro.dev/docs/enterprise/monitor-and-track/user-activity/)
and [Kiro prompt logging](https://kiro.dev/docs/enterprise/monitor-and-track/prompt-logging/)

## Query, Retention, And Authentication

### Local JSONL

**Documented fact.** Local JSONL is enabled by default under
`~/.loongsuite-pilot/logs/output/`, retains native JSON types, and defaults to
seven days. Size protection can remove files older than two days when a file
exceeds 512 MiB and can evict older files when the directory exceeds 2 GiB.
[Pilot local JSONL output](https://github.com/alibaba/loongsuite-pilot/blob/be9877ec437b1c50b6b7e70e431719fdd78b01ae/docs/local-jsonl-output.md#retention-and-disk-protection)

**Decision.** JSONL is suitable for a bounded shadow POC and recovery cursor,
but its default retention is not a durable production evidence SLA.

### SLS Logs

**Documented fact.** Pilot supports SLS WebTracking, AccessKey, and API-key
ingestion; API-key mode uses `Authorization: Bearer` with the direct protobuf
logstore endpoint. [Pilot SLS output](https://github.com/alibaba/loongsuite-pilot/blob/be9877ec437b1c50b6b7e70e431719fdd78b01ae/docs/sls-output.md)

SLS Logstore retention is independently configured from 1 to 3650 days or
permanent storage; expired logs are deleted. [Alibaba Cloud: configure log
retention](https://help.aliyun.com/zh/sls/log-storage-duration-setting)

SLS exposes query APIs. Exact field queries require the destination Logstore
and indexes to exist; query access should use a least-privilege RAM identity.
The documented read permission for log queries is `log:GetLogStoreLogs` scoped
to the project/logstore resource. [Alibaba Cloud: GetLogs](https://help.aliyun.com/zh/sls/developer-reference/api-sls-2020-12-30-getlogs)

**Decision.** SLS is the most practical remote query surface for a POC, but the
POC must record the actual Logstore TTL, indexes, RAM policy, and query API
result. Do not infer them from Pilot installation success.

### OTLP / ARMS / CloudMonitor Trace

**Documented fact.** Pilot accepts generic OTLP HTTP headers and CMS/ARMS
license/workspace configuration. The same converted spans can fan out to
multiple backends. [Pilot trace output](https://github.com/alibaba/loongsuite-pilot/blob/be9877ec437b1c50b6b7e70e431719fdd78b01ae/docs/trace-output.md)

Alibaba Cloud's public trace ingress documentation lists port `8090` for
OpenTelemetry gRPC and port `80` for OpenTelemetry HTTP/Jaeger HTTP for
non-Alibaba-Cloud applications. These are service-specific ingress contracts,
not evidence that the endpoint accepts generic default OTLP ports 4317/4318.
[Alibaba Cloud: trace prerequisites](https://help.aliyun.com/zh/opentelemetry/user-guide/before-you-begin-before-you-begin)

The public documentation does not state a TLS listener for those two ingest
ports, but it also does not establish that the generated CloudMonitor 2.0 CMS
endpoint uses that older path. The POC must record the actual endpoint scheme
and TLS handshake and must not send attribution evidence over an unencrypted
public path. A CMS license key or an SLS credential authenticates a sender; it
does not by itself prove transport confidentiality or a per-device principal.

ARMS exposes paged trace search by time, application, IP, span name, and tags,
with RAM authorization. [Alibaba Cloud: SearchTracesByPage](https://help.aliyun.com/zh/arms/application-monitoring/developer-reference/api-arms-2019-08-08-searchtracesbypage-apps)

**Unknown.** Current public material does not establish the actual retention
period, query API availability, or exact-tag indexing for the target
CloudMonitor 2.0 workspace. A separate product example documents 30-day trace
retention, but that cannot be generalized to this workspace. The POC must read
back the target workspace's live configuration/API behavior.

## Exporter Outage Semantics

The outputs do not share one durability contract.

- **HTTP output, observed source fact:** a failed batch is requeued only in the
  process memory buffer. Shutdown performs one final flush. There is no durable
  HTTP retry ledger in this flusher.
  [HTTP flusher](https://github.com/alibaba/loongsuite-pilot/blob/be9877ec437b1c50b6b7e70e431719fdd78b01ae/src/flushers/http-flusher.ts#L45-L72)
- **SLS output, observed source fact:** the current transport defaults to three
  attempts, 10-second request timeout, and exponential 1s/2s waits for selected
  network failures and HTTP 408/429/5xx responses. After exhaustion, Pilot
  stores bounded diagnostic metadata, not the failed payload, so that file
  cannot replay evidence.
  [SLS transport](https://github.com/alibaba/loongsuite-pilot/blob/be9877ec437b1c50b6b7e70e431719fdd78b01ae/src/flushers/sls-transport.ts#L13-L19)
  and [retry loop](https://github.com/alibaba/loongsuite-pilot/blob/be9877ec437b1c50b6b7e70e431719fdd78b01ae/src/flushers/sls-transport.ts#L136-L173)
- **OTLP trace output, observed source fact:** a failing backend is isolated and
  failed spans are written under `logs/otlp-failed`; the export promise resolves
  rather than propagating failure. The failed file contains serialized spans,
  not only diagnostics.
  [OTLP export failure path](https://github.com/alibaba/loongsuite-pilot/blob/be9877ec437b1c50b6b7e70e431719fdd78b01ae/src/flushers/otlp-trace-flusher.ts#L871-L907)
  and [failed-span writer](https://github.com/alibaba/loongsuite-pilot/blob/be9877ec437b1c50b6b7e70e431719fdd78b01ae/src/flushers/otlp-trace-flusher.ts#L1442-L1465)
- **OTLP failed-file retention, observed source fact:** the current retention
  service enumerates history, errors, debug, output, and SLS-failure categories,
  but not `otlp-failed`. No automatic OTLP failed-file cleanup or replay is
  established by the current source.
  [Retention categories](https://github.com/alibaba/loongsuite-pilot/blob/be9877ec437b1c50b6b7e70e431719fdd78b01ae/src/core/log-retention-service.ts#L19-L34)

**Decision.** No remote LoongSuite output currently provides the required
end-to-end acknowledgement/replay contract by itself. A production evidence
consumer would need a durable local cursor over JSONL, idempotent ingestion by
`event.id`, explicit ACK state, and a bounded dead-letter policy. Trace success
in the UI is not an ingestion ACK for commit attribution.

## Privacy Findings

**Documented fact.** LoongSuite can mask prompt/completion/tool content before
output, but the Alibaba Cloud integration page says the mask mode defaults to
`none`. It also says model, Token, duration, Git branch, and workspace path are
not scanned as secret-content fields. [Alibaba Cloud: AI Coding Agent
integration, data masking](https://help.aliyun.com/zh/cms/cloudmonitor-2-0/ai-application-access-ai-coding-agent/)

**Observed source fact.** The normalized contract keeps workspace and inferred
Git fields even when message capture is disabled. Failed OTLP files can contain
full serialized spans, so content controls must apply before conversion/export,
not only at the remote backend.

For the attribution use case, the allowed envelope should be metadata-only:

```text
agent_type
source_version + pilot_version
native_session_id
native_turn_id + normalized_turn_id
terminal_status + terminal_event_id
response_id/request_id when first-party exact
input_tokens/output_tokens/cache_read_tokens/cache_creation_tokens/total_tokens
event_id + source_record_digest
```

Prompt, completion, reasoning, tool arguments/results, commands, code, absolute
workspace paths, and raw transcript records are unnecessary and should be
disabled or removed before remote export. Repository identity should use the
existing canonical repo identity/digest, not an absolute path.

## POC Contract For The Owner

Run a shadow-only POC with Pilot version pinned to the exact tested release and
with Codex `0.149.1` plus the locally installed Claude Code `2.1.220` as the
initial matrix. Do not enable Kiro Token attribution.

The POC passes only if every accepted claim demonstrates all of the following:

1. A first-party native session identity and native turn identity survive into
   a normalized event without time/path/model/Token heuristics.
2. A terminal first-party event proves completed success; interrupted, failed,
   partial, and replayed turns fail closed.
3. Token components are attached to that exact turn once. Parent AGENT and LLM
   span aggregates are not summed together.
4. The exact turn joins to an `ae-cli` commit candidate through explicit shared
   identity. LoongSuite repository/branch/workspace proximity is not a join.
5. The existing checkpoint module independently proves the target commit SHA;
   LoongSuite never creates or authorizes commit allocation.
6. Restart, duplicate hook delivery, transcript replay, exporter outage, and
   delayed remote processing preserve idempotency by `event.id` and do not move
   Token between turns.
7. Remote payload inspection proves prompt, response, reasoning, code, command,
   tool arguments/results, raw paths, and credentials are absent.
8. Live readback records SLS/trace retention, query API, exact indexes/tags,
   least-privilege auth, and deletion behavior for the actual workspace.
9. Every network hop carrying evidence is encrypted, and an outage/restart
   test proves the declared replay boundary rather than assuming UI visibility
   is an ACK.

The POC fails, and LoongSuite remains forensics-only, if any accepted claim
requires timestamp proximity, cwd/path equality, branch equality, model
similarity, Token similarity, synthesized Token, or a collector turn ID with no
stable native mapping.

## Recommended Owner Direction

1. First fix the existing Codex structured-mutation/commit allocation path with
   real current-version fixtures. LoongSuite cannot repair missing commit proof.
2. In parallel, run one bounded metadata-only LoongSuite POC for Codex and
   Claude Code. Prefer local JSONL plus an explicit test consumer first; add SLS
   only to prove remote query/retention, not as the initial attribution source.
3. If both agents pass the same exact envelope, define one adapter-facing
   `CommitBoundTokenEvidence` contract and keep LoongSuite behind it as an
   optional evidence provider.
4. Keep Kiro outside Token attribution until its first-party source exposes
   exact Token components. Do not convert credit to Token.
5. Do not introduce a production LoongSuite dependency until remote ACK/replay
   and failed-file privacy/retention are explicitly designed and tested.

## Primary Sources

- [Alibaba Cloud AI Coding Agent integration](https://help.aliyun.com/zh/cms/cloudmonitor-2-0/ai-application-access-ai-coding-agent/)
- [Alibaba Cloud LoongSuite Pilot release notes](https://help.aliyun.com/zh/cms/cloudmonitor-2-0/the-loongsuite-pilot-release-notes)
- [Alibaba Cloud LLM trace field definitions](https://help.aliyun.com/zh/cms/cloudmonitor-2-0/developer-reference/llm-trace-field-definition-description-2)
- [Alibaba Cloud SLS retention](https://help.aliyun.com/zh/sls/log-storage-duration-setting)
- [Alibaba Cloud SLS GetLogs API](https://help.aliyun.com/zh/sls/developer-reference/api-sls-2020-12-30-getlogs)
- [Alibaba Cloud ARMS SearchTracesByPage API](https://help.aliyun.com/zh/arms/application-monitoring/developer-reference/api-arms-2019-08-08-searchtracesbypage-apps)
- [Alibaba Cloud trace ingress prerequisites](https://help.aliyun.com/zh/opentelemetry/user-guide/before-you-begin-before-you-begin)
- [Official OpenAI Codex observability documentation](https://learn.chatgpt.com/docs/config-file/config-advanced#observability-and-telemetry)
- [Official OpenAI Codex App Server documentation](https://learn.chatgpt.com/docs/app-server#events)
- [Official Codex `0.149.1` source](https://github.com/openai/codex/tree/rust-v0.149.1)
- [Official Claude Code monitoring documentation](https://code.claude.com/docs/en/monitoring-usage)
- [Official Kiro per-user activity documentation](https://kiro.dev/docs/enterprise/monitor-and-track/user-activity/)
- [Official Kiro prompt logging documentation](https://kiro.dev/docs/enterprise/monitor-and-track/prompt-logging/)
- [Official Alibaba LoongSuite Pilot source snapshot](https://github.com/alibaba/loongsuite-pilot/tree/be9877ec437b1c50b6b7e70e431719fdd78b01ae)
