# Session / PR Attribution Operations

## Local State

- Workspace marker: `<workspace>/.ae/session.json`
- Runtime bundle: `~/.ae-cli/runtime/<session-id>/`
- Shared hooks: `$(git rev-parse --git-common-dir)/ae-hooks`
- Retry queue: `~/.ae-cli/runtime/<session-id>/queue/`

## Normal Flow

1. `ae-cli login`
2. `ae-cli start`
3. Commit normally; hooks upload checkpoints fail-open
4. If the backend was unavailable, run `ae-cli flush`
5. Trigger settlement with `POST /api/v1/prs/:id/settle`

## Recovery

- Missing marker but AE env vars present: the next `post-commit` rewrites `/.ae/session.json`
- Expired or revoked session key: `ae-cli start` abandons the stale session and bootstraps a new one
- Queued events: inspect `queue/` and replay with `ae-cli flush`
