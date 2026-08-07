# Codex Token Attribution Ledger POC Design

**Date:** 2026-08-05
**Status:** Implemented POC; not released or deployed
**Scope:** `ae-cli`, backend compact attribution writes and Activity read models, frontend `/activity`
**Related:**

- [Sessionless Local Tool Attribution](./2026-05-13-sessionless-local-tool-attribution-design.md)
- [Global Git Hooks](./2026-05-23-global-git-hooks-design.md)
- [Post-Commit Async Attribution Sync](./2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md)
- [Architecture](../../architecture.md)

## Relationship To Existing Contracts

This document is the current contract for the Codex compact Token-attribution
POC. For a reporting installation with compact reporting enabled, it replaces
the legacy Codex path that uploads one `tool_usage_event` per response and a
checkpoint `agent_snapshot`.

The older endpoints and Claude/Kiro scanner path remain for CLI compatibility.
Compact installation, checkpoint, bucket, revision, and OTLP writes live under
`/api/v1/attribution/*`, so an old CLI can keep using the existing checkpoint
and tool-usage APIs. Bounded product reads live under `/api/v1/activity/*`.
The legacy `/events` and `/attribution` browser routes redirect to `/activity`
and are no longer present in the primary navigation.

This is a local POC only. It does not authorize a Kubernetes, Helm, cloud, or
production rollout.

## Product Question

The first product question is:

> How many Codex Tokens were consumed for a repository, worktree, commit, or PR?

Delivery-effectiveness correlation is deferred. The POC does not introduce
Jenkins data, outcome scores, individual performance ranking, chargeback, or
cost accounting.

## Accounting Contract

The normalized primary metric is processed Token:

```text
processed_total_tokens
= fresh_input_tokens
+ cache_read_tokens
+ cache_write_tokens
+ output_tokens
```

For current Codex JSONL records:

```text
fresh_input_tokens
= input_tokens
- cached_input_tokens
- cache_write_input_tokens
```

Negative fresh input is clamped to zero. `reasoning_tokens` is a subset of
`output_tokens`; it is retained for explanation but never added again.
`provider_total_tokens` is retained separately for audit and must not silently
replace the normalized total.

The measured ledger must conserve Token:

```text
measured_tokens = bound_tokens + unbound_tokens
```

`multi_repo_shared` is part of `unbound_tokens` and is counted once globally.
It may be associated with several repositories for discovery, but it is not
duplicated into every repository total. `historical_advisory` and `invalid`
buckets remain outside the measured conservation equation.

## Source Policy

Codex phase 1 uses only these sources:

1. Codex rollout JSONL `token_count` records are the primary Token source.
2. `logs_2.sqlite` is a compatibility fallback only for a conversation with no
   usable measured JSONL Token facts.
3. Codex native OTel is optional Request ID correlation evidence. It is not a
   second Token source and is not sent to Langfuse.

JSONL and SQLite observations for the same conversation are never summed.
Claude and Kiro retain their legacy compatibility path during this POC.

## Privacy Boundary

The local extractor may transiently inspect structured tool metadata to find
the effective Git repository, including tool work directories and patch target
paths. It immediately reduces those values to repository/worktree evidence.

The compact upload must not contain:

- prompt, response, reasoning, code, patch, or diff content;
- command arguments or tool output;
- file names, local source paths, or environment variables;
- raw Codex JSONL/SQLite records;
- raw OTel spans or logs;
- legacy `agent_snapshot`, `raw_payload`, or `session_id` fields.

The reporter checkpoint DTO uses strict JSON decoding and rejects unknown
legacy/raw fields. The compact bucket contains only identifiers, time windows,
Token counters, counts, digests, versions, quality, and allocation metadata.

## Local Extraction And Attribution

### Baseline

`ae-cli attribution enable` stores `enabled_at` without parsing historical
Token atoms or persisting one seen ID per historical event. Normal reporting
begins after that baseline. Compact scans skip source files unchanged since the
baseline and also reject atoms observed before it. A previously existing active
file is scanned if Codex appends to it, which preserves late-write handling
without a 4.8 GB first-enable scan. The POC does not backfill historical Token
into the first later commit.

### Change set and bucket

The response-local change-set identity is:

```text
codex:<conversation_id>:<turn_id>
```

Atoms are compacted locally by change set, conversation, model, Token quality,
and allocation target. A deterministic digest identifies the exact atom set,
and the deterministic bucket ID includes the reporting installation, tool, and
source digest. This makes retry idempotent without uploading atom rows.

### Repository evidence

Repository evidence follows this priority:

```text
tool write/workdir Git root
> turn cwd Git root
> no repository evidence
```

- One output repository produces `direct` evidence.
- Several output repositories produce one `multi_repo_shared` pool.
- Cwd-only evidence remains `unbound`; it cannot become a confirmed allocation.
- Launch cwd is not treated as repository truth.

This allows Codex launched in repository A to attribute work committed in
repository B to B when the tool evidence identifies B.

### Commit boundary

Token is attributed forward. A measured direct atom is bound to the first
qualifying commit checkpoint at or after the observation. Token after the last
commit remains unbound until a later qualifying commit exists; it is not poured
backward into current `HEAD`.

Compact Git triggers are written before the detached runner starts and are
retained locally. This prevents hook-task coalescing or a late JSONL flush from
losing an intermediate commit/rewrite boundary.

### Git lineage

- amend/rebase/rewrite moves the allocation to the replacement commit through
  an append-only allocation revision;
- squash aggregates predecessor usage at the replacement lineage head;
- a cherry-pick is recognized only with explicit Git reflog evidence plus a
  stable patch ID match;
- the cherry-picked commit displays inherited Token, while the original commit
  remains the sole accounting owner, so measured and repository totals do not
  increase;
- an identical patch without explicit lineage evidence is not automatically
  treated as a cherry-pick.

## Local Scheduling And Git Setup

`ae-cli attribution enable` performs four actions:

1. ensures a reporting installation exists;
2. records the Codex baseline;
3. enables compact reporting and optional Codex OTLP correlation;
4. enables the existing machine-level managed Git hooks.

A separate `ae-cli init` is not required for every backend-known,
reporting-enabled repository. Global hooks use read-only remote resolution and
never create repositories. `ae-cli init` remains available for explicit repo
registration, eligibility bootstrap, and repo-local hook fallback.

Git still provides the commit/rewrite boundary. The change removes per-repo
hook installation as the default requirement; it does not remove Git hooks.
Git has one effective `core.hooksPath` per resolution scope. The managed global
hook path does not automatically chain an unrelated previous hook directory,
and `--force` remains the explicit overwrite authority.

The hook fast path writes minimized checkpoint/rewrite evidence, queues a
workspace sync task, and starts the detached compact runner. It does not parse
all Codex history inline. The runner consumes the durable compact state and
uploads buckets/revisions outside the commit latency budget.

## Installation Identity And Credentials

Successful `ae-cli login` best-effort enrolls one stable machine installation.
Enrollment failure prints a warning but does not roll back a successful login.
Actual Token reporting remains disabled until `ae-cli attribution enable`.

Each installation has two scoped credentials:

- `aer_*`: compact bucket, allocation revision, and minimized checkpoint upload;
- `aeo_*`: Codex OTLP trace ingestion for Request ID correlation only.

Only SHA-256 token hashes are stored by the backend. Plaintext credentials are
returned on creation/rotation and stored locally in
`~/.ae-cli/reporting.json` with mode `0600`. If the local copy is missing while
the installation still exists, authenticated enrollment rotates both
credentials and replaces the local copy. Rotation immediately invalidates the
old reporter and OTLP credentials.

## Backend Storage

The backend stores three compact durable shapes:

1. `reporting_installations`: owner, installation status, enable flags, and
   credential hashes;
2. `attribution_usage_buckets`: immutable compact measurement facts;
3. `attribution_allocation_revisions`: append-only complete allocation vectors.

Bucket immutable-content conflicts and revision conflicts are rejected. A
bound allocation is accepted only when its repo/worktree/commit is backed by a
checkpoint or rewrite belonging to the authenticated installation owner.
Allocation validation also applies to inherited commit references.

The current revision is the highest sequence. A revision replaces the complete
allocation vector for presentation; it never mutates the immutable Token fact.
Most buckets should have one revision.

The POC deliberately does not persist per-request atom rows. The local trigger
ledger is compact Git evidence, not central request telemetry. Exact claim
deadlines and automatic local-trigger retention are deferred until real POC
distributions justify a policy.

## Request ID Correlation

When enabled, Codex exports OTLP/HTTP JSON traces directly to:

```text
POST /api/v1/attribution/otel/v1/traces
```

`log_user_prompt` is forced to `false`, no local collector is installed, and no
OTLP logs endpoint is exposed. The handler parses the bounded request in memory,
keeps only conversation ID, Request ID, timestamp, transport, status, and error
classification, then discards the raw payload.

Correlation evidence is held in the shared read-cache store rather than the
main attribution tables:

- successful evidence: 24 hours;
- failed evidence: 30 days;
- maximum 128 evidence entries per conversation and retention class.

Success and failure keys are separate, so a failure does not refresh successful
Request ID retention. Retention is evaluated per evidence item from its
observation time, so later evidence in the same conversation does not extend an
older Request ID. Buckets keep only correlation quality, Request ID count, and a
set digest. Current matching is advisory because it uses conversation and
bounded time-window overlap.

## API Surface

OAuth-user endpoints:

```text
POST /api/v1/attribution/installations
PUT  /api/v1/attribution/installations/:installation_id
POST /api/v1/attribution/installations/:installation_id/credentials/rotate
GET  /api/v1/attribution/report
```

The attribution report remains a compatibility read. The current product read
surface is the protected Activity namespace:

```text
GET /api/v1/activity/scope
GET /api/v1/activity/summary
GET /api/v1/activity/members
GET /api/v1/activity/members/:user_id
GET /api/v1/activity/teams/:team_id
GET /api/v1/activity/repos/:repo_id
GET /api/v1/activity/buckets/:bucket_id
```

Activity defaults to a 30-day `[from,to)` window. Member, PR, commit, and Bucket
collections have independent signed cursors. PR counts are lower bounds when
relevant repository sync coverage is incomplete. Each request revalidates the
actor role and current directory scope before consulting the short-lived read
cache; cached membership never authorizes a request. Bucket detail and retained
Request IDs are restricted to the Bucket owner and Admin.

Reporter-token endpoints:

```text
POST /api/v1/attribution/repos/resolve-remote
POST /api/v1/attribution/checkpoints/commit
POST /api/v1/attribution/checkpoints/rewrite
POST /api/v1/attribution/usage-buckets/batch
POST /api/v1/attribution/usage-buckets/:bucket_id/revisions
```

OTLP-token endpoint:

```text
POST /api/v1/attribution/otel/v1/traces
```

The reporter-scoped resolve endpoint is read-only and allows global hooks to
keep resolving backend-known repositories after the OAuth login/refresh window
expires. The separate namespace is the compatibility boundary; no breaking
replacement of the old CLI APIs is required.

## Activity Surface

The protected `/activity` page is the canonical product UI and defaults to the
latest 30 days. Its headline fields are participating PRs, merged PRs, active
repositories, and latest activity; Token is never a hero, team ranking, or
composite performance metric. PRs appear before commits and incomplete PR sync
is shown explicitly as `≥N`. Quality counts keep unbound/shared and invalid
facts separate from repository output.

`/activity/members/:user_id` is the authorized member drill-down.
`/activity/teams` and `/activity/teams/:team_id` expose the current authorized
directory subtree to representatives and all current active teams/members to
Admins; directory-only members remain visible as unavailable. `/repos/:id` is
Activity-first, while repository configuration, webhook repair, explicit PR
sync, and legacy PR usage remain in a lazy Operations section. Opening or
filtering Activity never starts PR sync.

The owner and Admin may expand a Bucket to inspect the Token breakdown, current
allocation revision, extractor/normalization versions, correlation state, and
retained Request IDs. Representatives can inspect member/repository/PR/commit
and quality summaries but do not receive Bucket rows or raw Request IDs. The UI
never exposes prompts, responses, code, commands, local paths, raw spans, or
conversation identifiers.

## POC Acceptance Criteria

The POC is acceptable when:

1. JSONL is preferred and SQLite is fallback-only for the same conversation;
2. processed Token normalization and reasoning subset validation hold;
3. measured Token is conserved across bound and unbound pools;
4. cross-repo, late JSONL, worktree, offline retry, rewrite, squash, rebase, and
   cherry-pick behavior is covered by tests;
5. inherited Token is visible but never counted twice;
6. reporter and OTLP credentials cannot authorize each other's endpoints and
   old credentials fail after rotation;
7. strict compact checkpoint DTOs reject raw/legacy fields;
8. the hook returns within the existing fast-path budget and the detached
   runner makes compact data visible promptly;
9. compact central records are at least 90 percent fewer than the local Codex
   Token atoms in the measured sample;
10. outbound compact payloads and durable database fields contain no prompt,
    code, diff, command, tool output, raw span, or local path.

The 2026-08-05 local measurement used the 31 most recently modified rollout
files: 11,531 Token atoms compacted into 312 bucket payloads in approximately
0.8 seconds. Counting both each bucket row and its initial allocation-revision
row gives 624 central rows, a 94.59 percent reduction; bucket payload count
alone is 97.29 percent lower. Baseline initialization completed without
historical parsing, and the compact hook scheduling test completed in
approximately 0.3 seconds. These are POC measurements, not a production
latency SLO.

## Deferred Work

- Claude/Kiro migration to the generic bucket contract;
- calibrated claim deadlines and automatic cleanup of retained local triggers;
- explicit manual allocation editing;
- delivery-effectiveness correlation such as commit/PR outcomes;
- cost or billing reconciliation;
- organization policy controls beyond the current per-installation flags;
- any production rollout.
