# Session / PR Attribution Operations

This guide describes the current `ae-cli` session lifecycle, the local files it writes, and the normal recovery steps when session attribution data is missing or delayed.

## Local State

- Workspace marker: `<workspace>/.ae/session.json`
- Runtime root: `~/.ae-cli/runtime/<session-id>/`
- Runtime bundle: `~/.ae-cli/runtime/<session-id>/runtime.json`
- Hook retry queue: `~/.ae-cli/runtime/<session-id>/queue/hooks.jsonl`
- Proxy event spool: `~/.ae-cli/runtime/<session-id>/queue/proxy-session-events.jsonl`
- Collector cache dir: `~/.ae-cli/runtime/<session-id>/collectors/`
- Shared hooks dir: `$(git rev-parse --git-common-dir)/ae-hooks`

The workspace marker only stores non-sensitive binding metadata such as `session_id`, `workspace_id`, `runtime_ref`, `provider_name`, and `relay_api_key_id`.

The runtime bundle may contain sensitive env values and local proxy metadata, so it stays under the user runtime directory rather than the workspace.

## Normal Flow

1. Run `ae-cli login` to create or refresh `~/.ae-cli/token.json`.
2. Run `ae-cli start` inside a repo or worktree.
3. `ae-cli start` bootstraps the backend session, writes the workspace marker, writes the runtime bundle, installs shared git hooks, and starts the local session proxy.
4. Commit normally. Git hooks upload checkpoints fail-open; if the backend or proxy is temporarily unavailable, events are queued locally.
5. If local queues accumulate, run `ae-cli flush` to replay queued hook events.
6. End the session with `ae-cli stop` or let `ae-cli` best-effort stop it during shutdown.
7. Trigger PR settlement with `POST /api/v1/prs/:id/settle` from the backend API or the repo detail UI.

## What To Check During Attribution

- Marker exists under `/.ae/session.json` in the active workspace.
- Runtime bundle exists under `~/.ae-cli/runtime/<session-id>/runtime.json`.
- The current session appears in the Sessions UI and shows the expected provider, key ID, runtime ref, and last-seen metadata.
- `commit_checkpoints` rows are created after commits.
- `session_usage_events` and `session_events` continue arriving while the local proxy is active.
- The session detail page shows recent `Agent Usage Snapshots` as well as `Session Usage`, so cached-input / reasoning token snapshots should appear there when collectors have reported them.
- New non-stream request rows may also carry `session_usage_events.raw_response`, which the session detail page exposes as `Raw Response`.
- `Agent Usage Snapshots` raw expansion is sourced from `agent_metadata_events.raw_payload` and shown as `Raw Event`.

## Verified Usage Shapes

The current implementation has two different usage shapes that should not be conflated.

### Relay raw response usage

In a verified local-proxy e2e run on 2026-04-16, the raw OpenAI-compatible `/responses` body returned by relay included nested usage details rather than flattened cache/reasoning fields:

```json
{
  "usage": {
    "input_tokens": 27,
    "input_tokens_details": {
      "cached_tokens": 0
    },
    "output_tokens": 10,
    "output_tokens_details": {
      "reasoning_tokens": 0
    },
    "total_tokens": 37
  }
}
```

This means the upstream gateway can expose cache / reasoning token information, but it does so under:

- `usage.input_tokens_details.cached_tokens`
- `usage.output_tokens_details.reasoning_tokens`

For Anthropic-compatible `/v1/messages`, the verified relay-side shape differs. Cache details may appear as:

- `usage.cache_creation_input_tokens`
- `usage.cache_read_input_tokens`
- `usage.cached_tokens` (compat fallback)
- `usage.cache_creation.ephemeral_5m_input_tokens`
- `usage.cache_creation.ephemeral_1h_input_tokens`

### Persisted `session_usage_events`

The current proxy persistence path stores flattened request facts plus a small raw metadata slice:

- `input_tokens`
- `output_tokens`
- `total_tokens`
- `status`
- `raw_metadata.http_status`
- `raw_metadata.cached_input_tokens`
- `raw_metadata.reasoning_output_tokens`
- `raw_response` (original upstream non-stream response body, when available)

This preserves cache / reasoning detail when the relay raw response exposes it through the parsed OpenAI-compatible usage shape.

A verified Codex e2e rerun on 2026-04-16 confirmed the persisted request-level metadata now matches the transcript-side token facts for this flow:

- transcript `token_count.total_token_usage.cached_input_tokens = 9216`
- transcript `token_count.total_token_usage.reasoning_output_tokens = 11`
- persisted `session_usage_events.raw_metadata.cached_input_tokens = 9216`
- persisted `session_usage_events.raw_metadata.reasoning_output_tokens = 11`

Anthropic-compatible request usage is also normalized now:

- `raw_metadata.cached_input_tokens` stores the aggregate of cache-creation plus cache-read tokens
- `raw_metadata.cache_creation_input_tokens` preserves the creation slice
- `raw_metadata.cache_read_input_tokens` preserves the read slice
- when relay omits the aggregate fields, the proxy falls back to `cached_tokens` and `cache_creation.ephemeral_*`

### Persisted `agent_metadata_events`

`agent_metadata_events` remain commit/checkpoint-driven. They are created from collector snapshots attached to `post_commit`, not from request usage ingest.

After the 2026-04-16 collector fix, `post_commit` in a workspace session now reads session-local Codex transcripts from `<workspace>/.ae/codex-home/` before falling back to `~/.codex`. A verified e2e run showed:

- request usage was persisted under `session_usage_events` with matching cache / reasoning detail in `raw_metadata`
- a real commit created `commit_checkpoints`
- the same commit also created `agent_metadata_events` with non-zero `cached_input_tokens` and `reasoning_tokens`

For Codex today, request-level cache / reasoning detail is available in `session_usage_events.raw_metadata`, `Raw Response` is available from `session_usage_events.raw_response` for new non-stream rows, and checkpoint-driven `agent_metadata_events` remain the richer post-commit snapshot source used to bind those facts to commits and PR attribution intervals.

## Recovery

### Missing marker, but the session is still running

- If the workspace marker is missing but the runtime/session env is still available, the next `post-commit` hook can re-bootstrap the marker from environment context.
- If you are unsure whether the session is still valid, stop it and start a fresh one:

```bash
ae-cli stop
ae-cli start
```

### Queued hook events

- Inspect `~/.ae-cli/runtime/<session-id>/queue/hooks.jsonl`.
- Replay queued hook events with:

```bash
ae-cli flush
```

### Proxy session events were spooled locally

- Inspect `~/.ae-cli/runtime/<session-id>/queue/proxy-session-events.jsonl`.
- If the session is still active, restarting `ae-cli start` or performing new tool activity will let the local proxy resume uploads.
- If the session has already been torn down and the spool file remains, preserve the runtime directory until the missing events are either replayed manually or intentionally discarded.

### Stale session or dead tmux session

- `ae-cli start` checks the saved current session state.
- If it finds a session whose tmux session is no longer alive, it cleans up the stale local state and bootstraps a fresh session before continuing.

### Credential or provider issues

- Session-scoped provider credentials are resolved lazily through `GET /api/v1/sessions/:id/provider-credentials?platform=...`.
- If a tool stops receiving credentials, restart the session so `ae-cli` can re-bootstrap local state and re-resolve provider credentials on demand.

## Manual Settlement Checklist

When PR attribution looks incomplete, verify the following before re-running settlement:

1. The target PR has matching `commit_checkpoints` for the relevant commit SHAs.
2. The related session rows still have the expected `provider_name`, `relay_api_key_id`, and `runtime_ref`.
3. Local queues are drained with `ae-cli flush`.
4. The backend can read the PR repo config and SCM provider normally.
5. `POST /api/v1/prs/:id/settle` returns a result with `attribution_status`, `primary_token_count`, `primary_token_cost`, and `validation_summary`.
