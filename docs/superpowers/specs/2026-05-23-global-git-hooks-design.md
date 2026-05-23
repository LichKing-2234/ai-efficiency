# Global Git Hooks Design

**Date:** 2026-05-23
**Status:** Proposed design, not yet implemented
**Scope:** `ae-cli/`, `backend/internal/handler/repo.go`, `backend/internal/checkpoint/`, `docs/`
**Related:**
- [2026-05-13-sessionless-local-tool-attribution-design.md](./2026-05-13-sessionless-local-tool-attribution-design.md)
- [2026-05-20-pr-usage-snapshots-design.md](./2026-05-20-pr-usage-snapshots-design.md)
- [2026-03-26-session-pr-attribution-design.md](./2026-03-26-session-pr-attribution-design.md)
- [docs/architecture.md](../../architecture.md)

The current project-level architecture remains documented in [`docs/architecture.md`](../../architecture.md). This document describes the next hook installation contract and does not claim that the behavior already exists.

## Spec Relationship

- This design supersedes the repo-by-repo hook installation assumption in the current `ae-cli init` flow.
- It keeps the sessionless attribution model from the 2026-05-13 design: local tool artifacts remain the usage fact source, and git hooks remain checkpoint triggers.
- It narrows the hook eligibility boundary: a globally installed hook may observe any Git repository, but it may only run AE attribution for repositories that already exist in the backend and are eligible for the authenticated user.
- Historical repo-local hooks remain a compatibility path, not the preferred installation model.

## Problem

The current hook installer writes hook scripts under each repository's git common directory and points that repository's `core.hooksPath` at the generated directory. That creates two operational problems:

1. Users must initialize each repository before hook-time attribution can run.
2. The generated hook embeds the install-time `ae-cli` executable path, so updating `~/.local/bin/ae-cli` does not reliably update the binary used by existing hooks.

A machine-level hook installer can remove the per-repository install requirement, but it introduces a stricter safety requirement: a global hook must not collect or upload data for arbitrary local repositories.

## Goals

1. Install git hooks once per machine instead of once per repository.
2. Keep repository eligibility controlled by backend state, not by local directory prefixes.
3. Allow both admin-managed repository configuration and user-initiated repository registration.
4. Ensure the hook path never creates backend repositories as a side effect.
5. Keep hook execution fail-open and bounded so commits are not blocked by backend latency or local scans.
6. Preserve existing non-AE repository hooks where Git would otherwise skip them because of a global `core.hooksPath`.
7. Make installed hooks follow normal `ae-cli` upgrades by resolving a stable executable path at runtime.

## Non-Goals

1. Do not add a local daemon, launch agent, or background service.
2. Do not use directory prefixes as the source of truth for whether a repository may be reported.
3. Do not make global hooks auto-register unknown repositories.
4. Do not require SCM provider binding as a hard precondition for attribution if the backend policy allows reporting to an active unbound repo.
5. Do not remove existing repo-local AE hooks in the first implementation.

## Command Surface

### `ae-cli hooks enable --global`

Installs the global hook dispatcher.

Behavior:

1. Require a valid login token.
2. Install managed hook scripts under `~/.ai-efficiency/git-hooks`.
3. Set `git config --global core.hooksPath ~/.ai-efficiency/git-hooks`.
4. Write scripts for `post-commit` and `post-rewrite`.
5. Use runtime executable resolution in the scripts instead of embedding only the current binary path.

The global side effect is explicit. `ae-cli init` must not silently modify global Git configuration unless a future flag explicitly requests that behavior.

### `ae-cli hooks disable --global`

Disables the managed global dispatcher.

Behavior:

1. Read `git config --global core.hooksPath`.
2. If it points to the AE-managed hook directory, unset the global value.
3. If it points somewhere else, do not modify it and print a diagnostic.
4. Leave the managed hook files on disk unless a later `--purge` option is added.

### `ae-cli hooks status`

Reports hook readiness.

It should show:

1. Whether global `core.hooksPath` points to the AE-managed directory.
2. Whether the current repository has a local `core.hooksPath` that overrides the global hook.
3. Whether the current repository exists in the local eligibility cache.
4. Whether the current repository resolves as eligible in the backend when online.
5. Whether legacy repo-local AE hooks are still installed for the current repository.
6. Which `ae-cli` executable the hook dispatcher would run.

### `ae-cli hooks refresh`

Refreshes the local hook eligibility cache from the backend.

Behavior:

1. Require a valid login token.
2. Call a read-only backend endpoint that returns repositories the current user may report.
3. Update the positive eligibility cache.
4. Preserve unexpired negative cache entries unless the refreshed positive list contains the same repo key.

`ae-cli hooks refresh --current` refreshes only the current repository by calling the read-only resolve endpoint.

### `ae-cli init`

Registers or links the current repository as a user-initiated action.

Behavior:

1. Require a valid login token.
2. Detect the current repository remote and branch.
3. Call the existing create-or-ensure repository endpoint because this is an explicit user action, not an implicit hook action.
4. Write a positive eligibility cache entry for the returned repository.
5. Ensure local attribution state directories exist.
6. Print the current global hook status and the next command if global hooks are not enabled.

`ae-cli init` is no longer the primary hook installer. It is the current-repository registration and cache bootstrap command.

### `ae-cli sync`

Keeps its user-facing meaning: manually scan local tool artifacts and upload usage for the current repository.

New boundary:

1. Resolve current repository eligibility first.
2. If the repository is not backend-known or not reportable by the user, fail with a clear message that points to `ae-cli init` or admin repo configuration.
3. Do not create repositories implicitly from `sync`.

### `ae-cli doctor`

Keeps its current readiness role and should include hook status, repository eligibility, and cache diagnostics. It can share most checks with `ae-cli hooks status`.

### Hidden hook commands

These commands remain internal:

- `ae-cli hook post-commit`
- `ae-cli hook post-rewrite <rewrite_type>`
- `ae-cli hook attribution-sync`

They become the Go dispatch layer for hook-time eligibility. The managed shell scripts stay thin: resolve the `ae-cli` executable, preserve `post-rewrite` stdin, invoke the hidden hook command, and chain legacy hooks. The hidden Go command performs cache lookup, optional online resolve, and handler execution.

## Backend Contract

### Existing create-or-ensure endpoint

`POST /api/v1/repos/ensure-remote` remains the endpoint for explicit user actions such as `ae-cli init`.

It may return an existing repository or create a new unbound repository from the supplied remote metadata. The global hook dispatcher and hidden hook upload path must not call this endpoint.

### New read-only resolve endpoint

`POST /api/v1/repos/resolve-remote`

Purpose: determine whether a repository already exists and whether the authenticated user may report hook attribution for it.

Request:

```json
{
  "remote_url": "git@repo-host.example.com:org/repo.git",
  "branch": "main"
}
```

Eligible response:

```json
{
  "eligible": true,
  "repo_config_id": 123,
  "repo_key": "repo-host.example.com/org/repo",
  "full_name": "org/repo",
  "clone_url": "git@repo-host.example.com:org/repo.git",
  "status": "active",
  "binding_state": "unbound"
}
```

Ineligible response:

```json
{
  "eligible": false,
  "repo_key": "repo-host.example.com/org/repo",
  "reason": "not_found"
}
```

The endpoint is read-only:

1. It must not create `repo_configs`.
2. It must not modify SCM binding state.
3. It must not create checkpoints, rewrites, or usage events.

The backend policy decides eligibility, but the default contract should be:

1. The repo must already exist in `repo_configs`.
2. The repo must be active.
3. The authenticated user must be allowed to report attribution to that repo.
4. SCM binding is reported as metadata and is not a hard attribution precondition unless backend policy explicitly makes it one.

Recommended ineligible reasons:

- `not_found`
- `inactive`
- `no_permission`
- `disabled_by_policy`
- `invalid_remote`

### New eligible list endpoint

`GET /api/v1/repos/hook-eligible`

Purpose: let `ae-cli hooks refresh` update the local positive cache without probing each repository one at a time.

The response contains only repositories the authenticated user may report. It does not include secrets.

Example response:

```json
{
  "repos": [
    {
      "repo_config_id": 123,
      "repo_key": "repo-host.example.com/org/repo",
      "full_name": "org/repo",
      "clone_url": "git@repo-host.example.com:org/repo.git",
      "status": "active",
      "binding_state": "unbound"
    }
  ]
}
```

### Checkpoint and rewrite ingest

The current checkpoint service can resolve or create a repository from `repo_full_name` or `clone_url`. That behavior is unsafe for the global hook path because a stale or malformed hook upload could create backend repository records.

The global hook implementation must change the hook upload contract:

1. The hidden Go hook command resolves eligibility before running checkpoint, rewrite, or attribution sync logic.
2. The resolved `repo_config_id` is passed through the internal hook handler and uploader path.
3. Checkpoint and rewrite upload payloads include `repo_config_id`.
4. When `repo_config_id` is present, backend checkpoint and rewrite ingest resolve the existing repository by ID and must not call create-or-ensure logic.
5. If the repo is missing, inactive, or not reportable by the authenticated user, ingest rejects the upload without creating any repository.

Legacy clients that do not send `repo_config_id` may continue to use the old behavior where appropriate, but global hooks must use the resolve-only path.

## Local Eligibility Cache

Cache file:

```text
~/.ai-efficiency/hooks/repos.json
```

The cache is keyed by backend-compatible `repo_key`, not by local checkout path. This lets multiple clones or worktrees of the same repository share eligibility.

Example:

```json
{
  "version": 1,
  "updated_at": "2026-05-23T10:00:00Z",
  "repos": {
    "repo-host.example.com/org/repo": {
      "eligible": true,
      "repo_config_id": 123,
      "repo_key": "repo-host.example.com/org/repo",
      "full_name": "org/repo",
      "clone_url": "git@repo-host.example.com:org/repo.git",
      "status": "active",
      "binding_state": "unbound",
      "expires_at": "2026-05-24T10:00:00Z"
    }
  },
  "negative": {
    "repo-host.example.com/org/private-repo": {
      "reason": "not_found",
      "expires_at": "2026-05-23T10:05:00Z"
    }
  }
}
```

Default TTLs:

1. Positive entries: 24 hours.
2. Negative entries: 5 minutes.

Expired positive entries do not authorize hook execution. The dispatcher may try the online resolve endpoint with a short timeout. If that lookup fails, it skips AE hook work for that commit.

## Repository Identity

The CLI and backend must derive the same `repo_key` from common remote URL forms:

- `git@repo-host.example.com:org/repo.git`
- `ssh://git@repo-host.example.com/org/repo.git`
- `https://repo-host.example.com/org/repo.git`

The implementation should introduce a CLI-side repository identity helper with tests that mirror the backend identity behavior. The cache lookup must not rely on raw remote URL string equality.

## Global Dispatcher Flow

In this section, "dispatcher" means the managed shell launcher plus the hidden Go hook command. Shell should not parse JSON, call HTTP APIs, or implement eligibility policy.

For `post-commit`:

1. Resolve the current Git repository and remote.
2. Compute the canonical `repo_key`.
3. Check the local eligibility cache.
4. If positive and unexpired, run the existing AE hook handler logic with the cached `repo_config_id`.
5. If negative and unexpired, skip AE work.
6. On cache miss or expiry, call `POST /api/v1/repos/resolve-remote` with a hard timeout.
7. If resolve returns eligible, update the positive cache and run the existing AE hook handler logic.
8. If resolve returns ineligible, update the negative cache and skip AE work.
9. If resolve times out or fails, skip AE work.
10. Run any legacy repository hook that Git would otherwise have skipped.

For `post-rewrite`:

1. Preserve stdin into a temporary file.
2. Run the same eligibility flow.
3. If eligible, pass the preserved stdin to `ae-cli hook post-rewrite`.
4. Pass the same preserved stdin to the legacy repository hook if one exists.
5. Remove the temporary file.

All AE hook work remains fail-open. AE failures must not block the Git operation. Legacy hook behavior should remain as close as possible to Git's original behavior.

## Executable Resolution

Generated hook scripts must resolve the executable at runtime in this order:

1. `AE_CLI_HOOK_BIN`
2. `~/.local/bin/ae-cli`
3. `command -v ae-cli`
4. The install-time executable path as a final fallback

This solves the current problem where hooks can remain pinned to an old binary path after `ae-cli` is upgraded.

## Existing Hook Compatibility

A global `core.hooksPath` changes Git's default behavior: repository-local `.git/hooks/<hook>` scripts are no longer executed by Git directly. The AE dispatcher must compensate by chaining the default repository hook when it exists and is executable.

Compatibility rules:

1. If a repository has a local `core.hooksPath`, Git uses that local path and the global AE hook does not run. `ae-cli hooks status` must report this clearly.
2. If a repository has executable default hooks under `.git/hooks`, the global AE dispatcher runs them after AE's fail-open work.
3. If an existing repo-local AE hook is already installed under `.git/ae-hooks`, it may continue to work. The first global-hook implementation does not need to migrate or delete it.
4. The global dispatcher must avoid recursive chaining into the AE-managed global hook directory.

## Failure Behavior

The hook path must be bounded and fail-open:

1. Cache reads and writes are local file operations and should be best-effort.
2. Online resolve on cache miss has a hard timeout. The default timeout is 500ms.
3. Resolve timeout, network failure, invalid token, or backend 5xx means skip AE work for that hook invocation.
4. Ineligible responses write short-lived negative cache entries.
5. Hidden hook commands keep their existing total timeout and upload queue behavior.
6. No unknown repository should produce checkpoint, rewrite, or tool usage records.

## Data Boundary

The dispatcher needs only local Git metadata and backend eligibility metadata:

- Remote URL
- Current branch
- Git root and git directory
- `repo_key`
- `repo_config_id` after eligibility resolution

It must not upload local tool artifacts, prompts, raw payloads, file paths, or usage events until repository eligibility has been confirmed.

## Testing Plan

### CLI unit tests

1. Global hook scripts use runtime executable resolution.
2. Global hook scripts chain default repository hooks.
3. `post-rewrite` preserves stdin for both AE and legacy hooks.
4. Local `core.hooksPath` is detected and reported by `hooks status`.
5. Eligibility cache handles positive, negative, expired, and malformed entries.
6. Cache miss resolve uses the configured hard timeout.
7. Unknown repositories do not call hidden hook commands.
8. Eligible repositories pass `repo_config_id` to hidden hook commands.

### Backend unit tests

1. `resolve-remote` returns eligible for an existing active reportable repo.
2. `resolve-remote` returns `not_found` without creating a repo.
3. `resolve-remote` returns `inactive` for inactive repos.
4. `resolve-remote` returns `no_permission` when the user cannot report to the repo.
5. `hook-eligible` lists only reportable repos.
6. Checkpoint ingest with `repo_config_id` resolves by ID without create-or-ensure fallback.
7. Rewrite ingest with `repo_config_id` resolves by ID without create-or-ensure fallback.

### Integration checks

1. Enable global hooks.
2. Commit in a backend-known eligible repository and verify a checkpoint is uploaded.
3. Commit in an unknown repository and verify no backend repository, checkpoint, rewrite, or usage event is created.
4. Commit in a repository with a default `.git/hooks/post-commit` and verify the legacy hook still runs.
5. Upgrade `~/.local/bin/ae-cli` and verify the dispatcher uses the upgraded binary.

## Rollout Plan

1. Add backend read-only repository eligibility endpoints.
2. Add `repo_config_id` support to checkpoint and rewrite ingest.
3. Add CLI repository identity and eligibility cache packages.
4. Add global hook install, disable, refresh, and status commands.
5. Change `ae-cli init` to register the current repo and update cache instead of being the primary hook installer.
6. Keep old repo-local hooks working for existing installations.
7. Update `docs/architecture.md` after the implementation is merged so it reflects the new current runtime.

## Final Contract

The intended product contract is:

1. Users install machine-level hooks once with `ae-cli hooks enable --global`.
2. Admins may configure repos in the backend, and users may explicitly register a repo with `ae-cli init`.
3. Hook execution only proceeds for backend-known, user-reportable repositories.
4. Hook execution never creates repositories.
5. Unknown repositories are skipped without upload.
6. Existing repository hooks continue to run.
7. Hook scripts follow normal CLI upgrades by resolving `ae-cli` at runtime.
