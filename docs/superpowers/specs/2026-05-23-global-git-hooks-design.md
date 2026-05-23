# Git Hook Installation Design

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

- This design separates repository registration from hook installation in the current `ae-cli init` flow.
- It keeps the sessionless attribution model from the 2026-05-13 design: local tool artifacts remain the usage fact source, and git hooks remain checkpoint triggers.
- It narrows the hook eligibility boundary: a globally installed hook may observe any Git repository, but it may only run AE attribution for repositories that already exist in the backend.
- Repo-local hooks remain a first-class option for users who do not want global Git configuration changes.

## Problem

The current hook installer writes hook scripts under each repository's git common directory and points that repository's `core.hooksPath` at the generated directory. That creates two operational problems:

1. Users must initialize each repository before hook-time attribution can run.
2. The generated hook embeds the install-time `ae-cli` executable path, so updating `~/.local/bin/ae-cli` does not reliably update the binary used by existing hooks.

A machine-level hook installer can remove the per-repository install requirement for users who want that convenience, but it introduces a stricter safety requirement: a global hook must not collect or upload data for arbitrary local repositories. Users who do not want a global Git hook still need a supported single-repository installation mode with the same attribution safety boundary.

## Goals

1. Support both one-time machine-level hook installation and explicit single-repository hook installation.
2. Keep repository eligibility controlled by backend state, not by local directory prefixes.
3. Allow both admin-managed repository configuration and user-initiated repository registration.
4. Ensure the hook path never creates backend repositories as a side effect.
5. Keep hook execution fail-open and bounded so commits are not blocked by backend latency or local scans.
6. Make hook ownership explicit: AE-managed hook installation owns the selected `core.hooksPath` and does not preserve or chain previous non-AE hooks.
7. Make installed hooks follow normal `ae-cli` upgrades by resolving a stable executable path at runtime.
8. Remove the workspace-root marker directory from the active hook and attribution contract.

## Non-Goals

1. Do not add a local daemon, launch agent, or background service.
2. Do not use directory prefixes as the source of truth for whether a repository may be reported.
3. Do not make global hooks auto-register unknown repositories.
4. Do not require SCM provider binding as a hard precondition for attribution if the backend policy allows reporting to an active unbound repo.
5. Do not migrate legacy AE-managed hook scripts or user-level state roots.
6. Do not require users to enable global Git hooks if they only want attribution for one repository.
7. Do not store new attribution state in the repository working tree.
8. Do not preserve, restore, or chain non-AE hook scripts after the user chooses to let AE own the selected hook path.

## Command Surface

### `ae-cli hooks enable --global [--force]`

Installs the global hook dispatcher.

Behavior:

1. Require a valid login token.
2. Install managed hook scripts under `~/.ae-cli/git-hooks`.
3. Read the current global `core.hooksPath`.
4. If it is empty or already points to the AE-managed global directory, set `git config --global core.hooksPath ~/.ae-cli/git-hooks` and refresh the managed scripts.
5. If it points to a non-AE path, do not overwrite it silently.
6. With `--force`, overwrite the global value.
7. Without `--force`, prompt before overwriting in an interactive terminal; in non-interactive execution, fail with a diagnostic that names the existing path and suggests `--force`.
8. Write scripts for `post-commit` and `post-rewrite`.
9. Use runtime executable resolution in the scripts instead of embedding only the current binary path.

The global side effect is explicit. `ae-cli init` must not silently modify global Git configuration unless a future flag explicitly requests that behavior.

### `ae-cli hooks enable --repo [--force]`

Installs the hook dispatcher only for the current repository.

Behavior:

1. Require a valid login token.
2. Detect the current Git repository and git common directory.
3. Install managed hook scripts under `$(git rev-parse --git-common-dir)/ae-hooks`.
4. Read the current repository or worktree `core.hooksPath`.
5. If it is empty or already points to the AE-managed repo-local directory, set the current repository or worktree `core.hooksPath` to that managed directory and refresh the managed scripts.
6. If it points to a non-AE path, do not overwrite it silently.
7. With `--force`, overwrite the local value.
8. Without `--force`, prompt before overwriting in an interactive terminal; in non-interactive execution, fail with a diagnostic that names the existing path and suggests `--force`.
9. Do not preserve, copy, restore, or chain previous repository hook logic.
10. Use the same runtime executable resolution as global hooks.
11. Use the same backend eligibility, cache, timeout, and fail-open rules as global hooks.

This command does not modify global Git configuration. It is the preferred path for users who want attribution only in the current repository.

### `ae-cli hooks disable --global`

Disables the managed global dispatcher.

Behavior:

1. Read `git config --global core.hooksPath`.
2. If it points to the AE-managed hook directory, unset the global value.
3. If it points somewhere else, do not modify it and print a diagnostic.
4. Mark the global hook installation disabled in `~/.ae-cli/state/hooks/installations.json`.
5. Leave the managed hook files on disk unless a later `--purge` option is added.

### `ae-cli hooks disable --repo`

Disables the managed hook dispatcher for the current repository.

Behavior:

1. Read the current repository or worktree `core.hooksPath`.
2. If it points to the AE-managed repo-local hook directory, unset that local value.
3. If it points somewhere else, do not modify it and print a diagnostic.
4. Mark the repo-local hook installation disabled in `~/.ae-cli/state/hooks/installations.json`.
5. Leave the managed hook files on disk unless a later `--purge` option is added.

### `ae-cli hooks status`

Reports hook readiness.

It should show:

1. Whether global `core.hooksPath` points to the AE-managed directory.
2. Whether the current repository has AE-managed repo-local hooks enabled.
3. Whether the current repository has a local `core.hooksPath` that overrides the global hook.
4. Which hook mode Git will actually use for the current repository: none, global, repo-local AE, or non-AE local hooks.
5. Whether the current repository exists in the local eligibility cache.
6. Whether the current repository resolves as eligible in the backend when online.
7. Whether stale repo-local AE hooks from older contracts are still installed for the current repository.
8. Which `ae-cli` executable the hook dispatcher would run.
9. Which managed hook script template version is installed and whether it is stale for the current `ae-cli` binary.
10. Whether `AE_CLI_BIN` is overriding executable resolution.
11. Optional upload ledger status when requested, including last successful upload, pending count, and last error.

### `ae-cli hooks refresh`

Refreshes local hook eligibility cache entries from the backend.

Behavior:

1. Require a valid login token.
2. Read locally observed repo keys from `~/.ae-cli/state/hooks/repos.json`.
3. Call read-only backend resolve APIs only for those observed repo keys.
4. Update positive and negative eligibility cache entries for observed repos.
5. Preserve cache entries for repos that were not part of the refresh request.

`ae-cli hooks refresh --current` refreshes only the current repository by calling the read-only resolve endpoint.

For larger installations, the refresh contract may support batch reads of locally observed repo keys:

```text
POST /api/v1/repos/hook-eligible
```

The request contains repo identities the CLI has already observed locally. The backend must not return unrelated repos. This avoids turning hook refresh into a global repository enumeration API.

### `ae-cli init`

Registers or links the current repository as a user-initiated action.

Behavior:

1. Require a valid login token.
2. Detect the current repository remote and branch.
3. Call the existing create-or-ensure repository endpoint because this is an explicit user action, not an implicit hook action.
4. Write a positive eligibility cache entry for the returned repository.
5. Ensure user-level attribution state directories exist under `~/.ae-cli/state/attribution/`.
6. Print the current global hook status and the next command if global hooks are not enabled.

`ae-cli init` is no longer the primary hook installer. It is the current-repository registration and cache bootstrap command.

Optional hook installation flags:

- `ae-cli init --hooks none`
- `ae-cli init --hooks repo`
- `ae-cli init --hooks global`
- `ae-cli init --hooks repo --force`
- `ae-cli init --hooks global --force`

The default is `--hooks none`. The command may print suggested follow-up commands, but it should not modify hook configuration unless the user explicitly chooses a hook mode.

If `--hooks repo` or `--hooks global` is selected, `ae-cli init` delegates to the same hook enable contract. `--force` authorizes overwriting an existing non-AE `core.hooksPath`; without `--force`, interactive runs prompt and non-interactive runs fail with a diagnostic.

### `ae-cli sync`

Keeps its user-facing meaning: manually scan local tool artifacts and upload usage for the current repository.

New boundary:

1. Resolve current repository eligibility first.
2. If the repository is not backend-known or active, fail with a clear message that points to `ae-cli init` or admin repo configuration.
3. Do not create repositories implicitly from `sync`.

### `ae-cli doctor`

Keeps its current readiness role and should include hook status, repository eligibility, and cache diagnostics. It can share most checks with `ae-cli hooks status`.

### Hidden hook commands

These commands remain internal:

- `ae-cli hook post-commit`
- `ae-cli hook post-rewrite <rewrite_type>`
- `ae-cli hook attribution-sync`

They become the Go dispatch layer for hook-time eligibility. Both global and repo-local managed shell scripts stay thin: resolve the `ae-cli` executable, preserve `post-rewrite` stdin, and invoke the hidden hook command. The hidden Go command performs cache lookup, optional online resolve, and handler execution.

## Local State Boundaries

The new CLI contract uses one user-owned config and state root:

```text
~/.ae-cli/
  token.json
  config.yaml
  env.sh
  git-hooks/
  state/
    attribution/
    hooks/
      repos.json
      installations.json
```

Directory responsibilities:

1. `~/.ae-cli/token.json`, `~/.ae-cli/config.yaml`, and `~/.ae-cli/env.sh` hold login, user-level CLI configuration, and shell environment bootstrap.
2. `~/.ae-cli/git-hooks/` holds the managed global Git hook scripts.
3. `~/.ae-cli/state/attribution/` holds workspace scan state, hook queues, spooled usage events, collector snapshots, and upload ledgers.
4. `~/.ae-cli/state/hooks/repos.json` holds the local repository eligibility cache.
5. `~/.ae-cli/state/hooks/installations.json` tracks AE-managed hook installation locations and enabled/disabled state so `ae-cli` upgrades can rewrite only enabled managed scripts to the latest template.
6. Repo-local hook mode still uses the Git-owned `$(git rev-parse --git-common-dir)/ae-hooks` directory through local or worktree `core.hooksPath`.

The executable is not stored in the state root. The official user-installed binary path remains `~/.local/bin/ae-cli`, and managed hook scripts must not prefer or generate any local debug binary path.

There is no compatibility fallback for old user state roots. New code must not read from, write to, migrate from, or fall back to any user-level state directory outside `~/.ae-cli/`.

The implementation must not create, read, or trust any AE-managed marker directory under the repository working tree. Workspace identity is derived from the current Git context at hook runtime, and repository identity is resolved from the current Git remote plus backend eligibility. Existing workspace-root marker files from historical versions are ignored by the new hook path and must not be used as migration input for reporting decisions.

## Backend Contract

### Existing create-or-ensure endpoint

`POST /api/v1/repos/ensure-remote` remains the endpoint for explicit user actions such as `ae-cli init`.

It may return an existing repository or create a new unbound repository from the supplied remote metadata. Managed hook dispatchers and hidden hook upload paths must not call this endpoint.

### New read-only resolve endpoint

`POST /api/v1/repos/resolve-remote`

Purpose: determine whether a repository already exists and is active for hook attribution.

Request:

```json
{
  "remote_url": "git@repo-host.example.com:org/repo.git",
  "branch": "main",
  "client_cache_version": "repo-eligibility-v1"
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

The request may include the client cache version that produced the lookup. The backend may ignore it, but it gives future implementations a cheap way to diagnose stale local state.

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
3. The authenticated user must be valid, but v1 does not apply an additional per-user repo reporting permission check.
4. SCM binding is reported as metadata and is not a hard attribution precondition unless backend policy explicitly makes it one.

The v1 product rule is intentionally broad: if the repository is backend-known and active, a valid local user who can create a Git commit may report hook attribution. The backend does not verify SCM push permission in this hook path.

Recommended ineligible reasons:

- `not_found`
- `inactive`
- `disabled_by_policy`
- `invalid_remote`

### New batch eligible endpoint

`POST /api/v1/repos/hook-eligible`

Purpose: let `ae-cli hooks refresh` update local cache entries for repositories the CLI has already observed locally, without probing each repository one at a time.

The request contains locally observed repo identities. The response contains only active backend-known matches from that request. It does not include secrets and must not enumerate unrelated repositories.

Example request:

```json
{
  "repos": [
    {
      "repo_key": "repo-host.example.com/org/repo",
      "remote_url": "git@repo-host.example.com:org/repo.git"
    }
  ]
}
```

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
  ],
  "ineligible": [
    {
      "repo_key": "repo-host.example.com/org/private-repo",
      "reason": "not_found"
    }
  ],
  "version": "repo-eligibility-v1"
}
```

Large requests may be split by the CLI into multiple requests with a client-side page size. Server-side cursor pagination is not required because the request is bounded by local observations.

### Checkpoint, rewrite, and tool usage ingest

The current checkpoint service can resolve or create a repository from `repo_full_name` or `clone_url`. That behavior is unsafe for managed hook paths because a stale or malformed hook upload could create backend repository records.

The hook implementation must change the hook upload contract:

1. The hidden Go hook command resolves eligibility before running checkpoint, rewrite, or attribution sync logic.
2. The resolved `repo_config_id` is passed through the internal hook handler and uploader path.
3. Checkpoint and rewrite upload payloads include `repo_config_id`.
4. When `repo_config_id` is present, backend checkpoint and rewrite ingest resolve the existing repository by ID and must not call create-or-ensure logic.
5. If the repo is missing or inactive, ingest rejects the upload without creating any repository.
6. Tool usage upload payloads produced by hook-time sync or `ae-cli sync` also include the resolved `repo_config_id`.
7. Backend tool usage ingest uses `repo_config_id` plus the authenticated user to bind the workspace scope when no checkpoint exists yet. This avoids relying on an earlier checkpoint to infer which repository owns a workspace.

Legacy clients that do not send `repo_config_id` may continue to use the old behavior where appropriate, but managed global and repo-local hooks must use the resolve-only path for checkpoint, rewrite, and tool usage uploads.

## Local Eligibility Cache

Cache file:

```text
~/.ae-cli/state/hooks/repos.json
```

The cache is keyed by backend-compatible `repo_key`, not by local checkout path. This lets multiple clones or worktrees of the same repository share eligibility.

The cache is also the source of locally observed repo keys for `ae-cli hooks refresh`. A hook invocation may write an observed entry before the backend knows the repo. That observed entry is still not authorization; it only records that this machine has seen the repo and may refresh its eligibility later.

Example:

```json
{
  "version": 1,
  "updated_at": "2026-05-23T10:00:00Z",
  "etag": "\"repo-eligibility-v1\"",
  "eligibility_version": "repo-eligibility-v1",
  "repos": {
    "repo-host.example.com/org/repo": {
      "eligible": true,
      "repo_config_id": 123,
      "repo_key": "repo-host.example.com/org/repo",
      "full_name": "org/repo",
      "clone_url": "git@repo-host.example.com:org/repo.git",
      "status": "active",
      "binding_state": "unbound",
      "last_resolved_at": "2026-05-23T10:00:00Z",
      "expires_at": "2026-05-24T10:00:00Z"
    }
  },
  "negative": {
    "repo-host.example.com/org/private-repo": {
      "reason": "not_found",
      "repo_key": "repo-host.example.com/org/private-repo",
      "remote_url": "git@repo-host.example.com:org/private-repo.git",
      "last_observed_at": "2026-05-23T10:00:00Z",
      "expires_at": "2026-05-23T10:05:00Z"
    }
  }
}
```

Default TTLs:

1. Positive entries: 24 hours.
2. Negative entries: 5 minutes.

The expiration is not data retention and it is not an upload ledger. It is a bounded local authorization window for a global hook:

1. Positive TTL prevents a machine-level hook from trusting old repo status forever after a backend repo is disabled or removed.
2. Negative TTL prevents repeated backend calls from every commit in an unknown repository, but stays short so a newly configured backend repo becomes eligible quickly.
3. Backend ingest remains the final authority. Even if local cache is wrong, uploads with `repo_config_id` must be rechecked server-side.

Expired positive entries do not authorize hook execution. The dispatcher may try the online resolve endpoint with a short timeout. If that lookup fails, it skips AE hook work for that commit.

## Hook Installation Registry

Registry file:

```text
~/.ae-cli/state/hooks/installations.json
```

The registry tracks only AE-managed hook locations that `ae-cli` installed. It is not a discovery mechanism for arbitrary repositories.

Example:

```json
{
  "version": 1,
  "updated_at": "2026-05-23T10:00:00Z",
  "installations": [
    {
      "mode": "global",
      "hooks_path": "/Users/alice/.ae-cli/git-hooks",
      "enabled": true,
      "template_version": 2,
      "updated_at": "2026-05-23T10:00:00Z"
    },
    {
      "mode": "repo",
      "repo_key": "repo-host.example.com/org/repo",
      "git_common_dir": "/Users/alice/src/repo/.git",
      "hooks_path": "/Users/alice/src/repo/.git/ae-hooks",
      "enabled": false,
      "template_version": 2,
      "updated_at": "2026-05-23T10:00:00Z"
    }
  ]
}
```

Rules:

1. `hooks enable --global` records or updates one enabled global installation.
2. `hooks enable --repo` records or updates one enabled repo-local installation for the current Git common directory.
3. `hooks disable --global` and `hooks disable --repo` mark matching records disabled.
4. Installer and upgrade refresh rewrite only enabled records.
5. Disabled records may remain for diagnostics and should not reactivate hooks.

## Repository Identity

The CLI and backend must derive the same `repo_key` from common remote URL forms:

- `git@repo-host.example.com:org/repo.git`
- `ssh://git@repo-host.example.com/org/repo.git`
- `https://repo-host.example.com/org/repo.git`

The implementation should introduce a CLI-side repository identity helper with tests that mirror the backend identity behavior. The cache lookup must not rely on raw remote URL string equality.

## Workspace Identity

The hook path must derive `workspace_id` from the current Git context every time it runs.

Inputs:

- `git rev-parse --show-toplevel`
- `git rev-parse --absolute-git-dir`
- `git rev-parse --git-common-dir`

The derived value is the only active workspace identity for hook queues, scan state, spooled usage events, collector snapshots, and upload ledgers. The implementation must not trust a persisted workspace marker from the repository working tree. If historical marker files exist from older versions, the new hook path ignores them.

## Upload Ledger

Hook queues and spooled uploads already answer "what still needs retry." The upload ledger answers the operator question "what happened last" without storing raw payloads.

Ledger location:

```text
~/.ae-cli/state/attribution/workspaces/<workspace_id>/upload-ledger.jsonl
```

Each JSONL record should contain only operational metadata:

```json
{
  "version": 1,
  "kind": "checkpoint",
  "dedupe_key": "repo_config_id:123:commit:abc123",
  "repo_config_id": 123,
  "repo_key": "repo-host.example.com/org/repo",
  "workspace_id": "workspace-abc",
  "status": "uploaded",
  "attempt_count": 1,
  "attempted_at": "2026-05-23T10:00:00Z",
  "uploaded_at": "2026-05-23T10:00:01Z",
  "http_status": 200,
  "last_error": ""
}
```

Allowed `kind` values are `checkpoint`, `rewrite`, and `tool_usage`. Allowed `status` values are `pending`, `uploaded`, `failed`, and `skipped`.

The ledger must not contain prompts, model responses, tool raw payloads, file contents, or arbitrary local file paths. It may contain commit IDs because checkpoint and rewrite attribution is commit-based. `ae-cli hooks status --uploads` or `ae-cli sync status` can summarize the ledger by workspace and repo so a user can find the replay starting point quickly.

## Hook Dispatcher Flow

In this section, "dispatcher" means the managed shell launcher plus the hidden Go hook command. Shell should not parse JSON, call HTTP APIs, or implement eligibility policy.

Global and repo-local hook modes use the same dispatcher flow. They differ only in how Git reaches the managed shell script:

1. Global mode uses the user's global `core.hooksPath`.
2. Repo-local mode uses the current repository or worktree `core.hooksPath`.

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

For `post-rewrite`:

1. Preserve stdin into a temporary file.
2. Run the same eligibility flow.
3. If eligible, pass the preserved stdin to `ae-cli hook post-rewrite`.
4. Remove the temporary file.

All AE hook work remains fail-open. AE failures must not block the Git operation. AE-managed hook installation does not preserve or chain previous non-AE hook behavior after the user explicitly enables AE hook ownership.

## Executable Resolution

Generated hook scripts must resolve the executable at runtime in this order:

1. `AE_CLI_BIN`
2. `~/.local/bin/ae-cli`
3. `command -v ae-cli`

This solves the current problem where hooks can remain pinned to an old binary path after `ae-cli` is upgraded.

`AE_CLI_BIN` is an advanced override for debugging or custom package managers. The name refers to the `ae-cli` executable, not to a hook-specific binary. The CLI must not set this environment variable during normal installation; `hooks status` should warn when it is present because it can intentionally bypass the official `~/.local/bin/ae-cli` path.

If no executable can be resolved, the managed hook script skips AE work. Managed hook scripts must not embed the install-time executable path, because that can pin hooks to temporary or debug binaries.

## Managed Hook Script Versioning

Managed hook scripts are thin launchers, but they still have a template contract. Each generated script should include a small managed header:

```text
# ae-cli-managed-hook: template_version=2 generator_version=0.1.0
```

Version rules:

1. Normal `ae-cli` binary upgrades take effect on the next hook run because the script resolves the executable at runtime.
2. Hook behavior should live in the hidden Go commands whenever possible, so script template changes stay rare.
3. `ae-cli hooks enable --global` and `ae-cli hooks enable --repo` are idempotent and rewrite AE-managed hook scripts to the current template version.
4. `ae-cli hooks status` and `ae-cli doctor` compare installed template headers with the current binary's expected template version and report stale scripts.
5. Stale AE-managed scripts are not auto-rewritten from inside a Git hook invocation, because hooks must stay fast and fail-open.
6. The official `ae-cli` installer or upgrade flow must rewrite AE-managed hook scripts to the latest template after the new binary is installed.
7. Global hook scripts are rewritten at the fixed `~/.ae-cli/git-hooks` path.
8. Repo-local hook scripts are rewritten from enabled records in `~/.ae-cli/state/hooks/installations.json`, which records only AE-managed hook locations installed by `ae-cli hooks enable --repo`.
9. Disabled records are ignored by installer/upgrade refresh.
10. Missing or inaccessible recorded repo-local locations are skipped with diagnostics; upgrade must not fail because a repository was deleted or moved.

## Existing Hook Compatibility

Both hook modes make ownership explicit instead of preserving existing non-AE hook behavior. A global `core.hooksPath` changes Git's default behavior because repository-local `.git/hooks/<hook>` scripts are no longer executed by Git directly. A repo-local AE `core.hooksPath` can also replace a previous custom local hook path. AE only changes that selected path when the user confirms interactively or passes `--force`.

Compatibility rules:

1. If a repository has a local `core.hooksPath`, Git uses that local path and the global AE hook does not run. `ae-cli hooks status` must report this clearly.
2. If a repository has executable default hooks under `.git/hooks`, global AE hooks do not run them.
3. `ae-cli hooks enable --repo` overwrites the selected local `core.hooksPath` only with confirmation or `--force`; it does not preserve previous hook scripts.
4. If the current active `core.hooksPath` points to an older AE-managed repo-local hook directory, `ae-cli hooks status` reports it as stale. `ae-cli hooks enable --repo` rewrites that active path to the current contract. The CLI must not scan arbitrary repositories or historical directories to migrate stale hooks.
5. Managed dispatchers must avoid recursive execution of AE-managed global or repo-local hook directories.
6. `hooks enable --global` and `hooks enable --repo` must not silently replace a non-AE `core.hooksPath`. `--force` authorizes overwrite; without `--force`, interactive runs prompt and non-interactive runs fail with a diagnostic.

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
- Git common directory
- `repo_key`
- `repo_config_id` after eligibility resolution
- `workspace_id` derived from the current Git context

It must not upload local tool artifacts, prompts, raw payloads, file paths, or usage events until repository eligibility has been confirmed.

## Testing Plan

### CLI unit tests

1. Global and repo-local hook scripts use runtime executable resolution.
2. Global hook scripts do not chain default repository hooks.
3. Repo-local hook scripts do not preserve or chain existing local hooks.
4. `post-rewrite` preserves stdin for AE handling.
5. Local `core.hooksPath` is detected and reported by `hooks status`.
6. `hooks status` reports whether Git will use none, global, repo-local AE, or non-AE local hooks.
7. Eligibility cache handles positive, negative, expired, and malformed entries.
8. Cache miss resolve uses the configured hard timeout.
9. Unknown repositories do not run hook handler upload logic.
10. Eligible repositories pass `repo_config_id` to hook handler upload logic.
11. Hook execution derives `workspace_id` from the current Git context and ignores historical workspace markers.
12. `ae-cli init` creates only user-level attribution state, not repository working-tree state.
13. New code writes state only under `~/.ae-cli/` and does not read or migrate old user-level state roots.
14. Managed hook scripts resolve `~/.local/bin/ae-cli` and do not prefer local debug binary paths or install-time executable paths.
15. `hooks enable --global` and `hooks enable --repo` refuse to overwrite non-AE `core.hooksPath` in non-interactive mode without `--force`.
16. `hooks enable --global --force` and `hooks enable --repo --force` overwrite the selected `core.hooksPath` without preserving previous hook paths.
17. `hooks status` reports installed hook template versions and stale managed scripts.
18. `AE_CLI_BIN` overrides executable resolution and is reported by `hooks status`.
19. Upload ledgers record pending, uploaded, failed, and skipped operational metadata without raw payloads.
20. `hooks refresh` sends only locally observed repo identities to the batch eligibility endpoint and does not accept unrelated repos.
21. `hooks enable --repo` records AE-managed repo-local hook locations as enabled in `~/.ae-cli/state/hooks/installations.json`.
22. `hooks disable --repo` and `hooks disable --global` mark matching installation records disabled.
23. The installer or upgrade refresh rewrites global hooks and enabled recorded repo-local AE hooks to the current template without preserving previous non-AE hooks.

### Backend unit tests

1. `resolve-remote` returns eligible for an existing active backend-known repo.
2. `resolve-remote` returns `not_found` without creating a repo.
3. `resolve-remote` returns `inactive` for inactive repos.
4. `resolve-remote` does not apply per-user repo reporting permission in v1; a valid authenticated user may report to any active backend-known repo.
5. `hook-eligible` returns active backend-known matches only for requested repo identities.
6. Checkpoint ingest with `repo_config_id` resolves by ID without create-or-ensure fallback.
7. Rewrite ingest with `repo_config_id` resolves by ID without create-or-ensure fallback.
8. Tool usage ingest accepts `repo_config_id` and binds workspace scope without requiring a previous checkpoint.
9. `hook-eligible` returns results only for requested repo identities and does not enumerate unrelated active repos.

### Integration checks

1. Enable global hooks.
2. Enable repo-local hooks in a separate repository without changing global Git config.
3. Commit in a backend-known eligible repository and verify a checkpoint is uploaded.
4. Commit in an unknown repository and verify no backend repository, checkpoint, rewrite, or usage event is created.
5. Commit in a repository with a default `.git/hooks/post-commit` and verify the legacy hook does not run after AE owns the selected hook path.
6. Upgrade `~/.local/bin/ae-cli` and verify the dispatcher uses the upgraded binary.
7. Put a stale historical workspace marker in a repository and verify the hook ignores it.
8. Put stale state under an old user-level state root and verify the new CLI ignores it.
9. Configure a non-AE global or repo-local `core.hooksPath` and verify enable refuses without `--force` and succeeds with `--force`.
10. Generate a stale managed hook script and verify status reports the current and installed template versions.
11. Upgrade `ae-cli` and verify managed hook scripts are rewritten to the latest template at fixed global and recorded repo-local locations.

## Rollout Plan

1. Add backend read-only repository eligibility endpoints.
2. Add `repo_config_id` support to checkpoint, rewrite, and tool usage ingest.
3. Add CLI repository identity and eligibility cache packages.
4. Add global and repo-local hook install, disable, refresh, and status commands.
5. Change `ae-cli init` to register the current repo and update cache instead of being the primary hook installer.
6. Remove historical workspace marker read, write, and environment-bootstrap behavior from the active hook path.
7. Move active attribution state paths to `~/.ae-cli/state/` without migration fallback from older user-level state roots.
8. Mark only the current active older AE-managed repo-local hook path as stale in `ae-cli hooks status`; rewrite it when the user runs `ae-cli hooks enable --repo` or when the installer/upgrade refresh handles an enabled recorded repo-local installation.
9. Add managed hook installation tracking and installer/upgrade script refresh for AE-managed hook templates.
10. Add upload ledger status reporting for hook and sync replay diagnostics.
11. Update `docs/architecture.md` after the implementation is merged so it reflects the new current runtime.

## Final Contract

The intended product contract is:

1. Users may install machine-level hooks once with `ae-cli hooks enable --global`.
2. Users may instead install hooks only for the current repository with `ae-cli hooks enable --repo`.
3. Admins may configure repos in the backend, and users may explicitly register a repo with `ae-cli init`.
4. Hook execution only proceeds for backend-known active repositories.
5. Hook execution never creates repositories.
6. Unknown repositories are skipped without upload.
7. Existing repository hooks are not preserved or chained after AE hook ownership is enabled.
8. Hook scripts follow normal CLI upgrades by resolving `ae-cli` at runtime, and managed hook templates are rewritten by the installer or upgrade flow.
9. User-level CLI config, auth, hooks, cache, and attribution state all live under `~/.ae-cli/`.
