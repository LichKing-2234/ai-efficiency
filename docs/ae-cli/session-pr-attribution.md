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
