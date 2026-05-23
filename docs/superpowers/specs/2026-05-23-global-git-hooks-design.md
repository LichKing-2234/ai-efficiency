# Git Hook Installation Design

**Date:** 2026-05-23
**Status:** Proposed design, not yet implemented
**Scope:** `ae-cli/`, `ae-cli/install.sh`, `ae-cli/install.ps1`, `backend/internal/handler/router.go`, `backend/internal/handler/repo.go`, `backend/internal/handler/checkpoint.go`, `backend/internal/handler/tool_usage.go`, `backend/internal/repo/`, `backend/internal/repoidentity/`, `backend/internal/checkpoint/`, `backend/internal/toolusage/`, `docs/`
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
6. Make hook ownership explicit: AE-managed hook installation owns the configured `core.hooksPath` layer it writes and does not preserve or chain previous non-AE hooks.
7. Make installed hooks follow normal `ae-cli` upgrades by resolving a stable executable path at runtime.
8. Remove repository working-tree workspace metadata from the active hook and attribution contract.

## Non-Goals

1. Do not add a local daemon, launch agent, or background service.
2. Do not use directory prefixes as the source of truth for whether a repository may be reported.
3. Do not make global hooks auto-register unknown repositories.
4. Do not require SCM provider binding as a hard precondition for attribution if the backend policy allows reporting to an active unbound repo.
5. Do not migrate legacy AE-managed hook scripts or user-level state roots.
6. Do not require users to enable global Git hooks if they only want attribution for one repository.
7. Do not store new attribution state in the repository working tree.
8. Do not preserve, restore, or chain non-AE hook scripts after the user chooses to let AE own the relevant hook behavior.

## Command Surface

### `ae-cli hooks enable --global [--force]`

Installs the global hook dispatcher.

Behavior:

1. Require a valid login token.
2. Use `~/.ae-cli/git-hooks` as the managed global hook path.
3. Read the current global `core.hooksPath`.
4. If it is empty or already points to the AE-managed global directory, the enable operation is authorized.
5. If it points to a non-AE path, do not overwrite it silently.
6. With `--force`, authorize overwriting the global value.
7. Without `--force`, prompt before overwriting in an interactive terminal; in non-interactive execution, fail with a diagnostic that names the existing path and suggests `--force`.
8. After the operation is authorized, write or refresh scripts for `post-commit` and `post-rewrite`.
9. Set `git config --global core.hooksPath ~/.ae-cli/git-hooks`.
10. Record or update the enabled global installation.
11. Use runtime executable resolution in the scripts instead of embedding only the current binary path.
12. Print an ownership warning that global `core.hooksPath` causes Git to bypass repository default `.git/hooks` scripts unless a repository has a local override.

The global side effect is explicit. `ae-cli init` must not silently modify global Git configuration unless a future flag explicitly requests that behavior.

### `ae-cli hooks enable --repo [--force]`

Installs the hook dispatcher only for the current repository.

Behavior:

1. Require a valid login token.
2. Detect the current Git repository, absolute git directory, git common directory, effective hook config, and repo-local target config scope.
3. If a local or worktree `core.hooksPath` already exists, the repo-local target scope is the scope that owns that value.
4. If no local or worktree `core.hooksPath` exists, the repo-local target scope is `worktree` when `extensions.worktreeConfig=true`; otherwise it is `local`.
5. Use `$(git rev-parse --git-common-dir)/ae-hooks` as the managed repo-local hook path.
6. Read the effective hook behavior Git would use for the current repository.
7. If no effective `core.hooksPath` exists, inspect Git's default hook directory for executable `post-commit` or `post-rewrite` scripts.
8. If no non-AE effective hook behavior would be replaced, or if the effective hook path already points to an AE-managed global or repo-local directory, the enable operation is authorized.
9. If the effective hook path points to a non-AE local, worktree, or global path, or if no effective hook path exists but executable default hooks would be replaced, do not overwrite hook behavior silently.
10. With `--force`, authorize replacing the current repository's effective hook behavior by writing a repo-local override. `--force` for repo-local mode must not modify global Git config.
11. Without `--force`, prompt before overwriting in an interactive terminal; in non-interactive execution, fail with a diagnostic that names the existing effective path or default hook directory and suggests `--force`.
12. After the operation is authorized, write or refresh scripts for `post-commit` and `post-rewrite`.
13. Set the repo-local target scope's `core.hooksPath` to the AE-managed repo-local directory.
14. Do not preserve, copy, restore, or chain previous repository hook logic.
15. Use the same runtime executable resolution as global hooks.
16. Use the same backend eligibility, cache, timeout, and fail-open rules as global hooks.
17. Record the repo-local installation with `git_dir`, `git_common_dir`, `config_scope`, `hooks_path`, `repo_key` if known, and `template_version`.

This command does not modify global Git configuration. It is the preferred path for users who want attribution only in the current repository.

### `ae-cli hooks disable --global`

Disables the managed global dispatcher.

Behavior:

1. Read `git config --global core.hooksPath`.
2. If it points to the AE-managed hook directory, unset the global value.
3. If it points somewhere else, do not modify it and print a diagnostic.
4. Mark the global hook installation disabled in `~/.ae-cli/state/hooks/installations.json` only after the global value is unset or already absent.
5. Leave the managed hook files on disk unless a later `--purge` option is added.

### `ae-cli hooks disable --repo`

Disables the managed hook dispatcher for the current repository.

Behavior:

1. Detect the current repository, absolute git directory, git common directory, local/worktree `core.hooksPath` values, and effective hook config.
2. Only local or worktree config layers are candidates for repo-local disable. A global `core.hooksPath` value, even an AE-managed global value, is reported but never modified by `hooks disable --repo`.
3. If the local or worktree value points to the current AE-managed repo-local hook directory, or to a recognized stale AE-managed repo-local hook path from an older contract, capture `git_dir`, `config_scope`, and `hooks_path`, then unset that value from the same local or worktree config scope.
4. After a successful unset, mark the matching repo-local hook installation disabled in `~/.ae-cli/state/hooks/installations.json`.
5. If the local or worktree value points somewhere else, do not modify it, leave repo-local installation records unchanged, and print a diagnostic.
6. If no active AE-managed repo-local hook path is found in local or worktree config, leave repo-local installation records unchanged and report that no current repo-local AE hook was disabled.
7. After unsetting a repo-local AE hook, recompute the effective hook mode. If an AE-managed global hook is still effective, report that commits in this repository remain covered by global hooks and that `ae-cli hooks disable --global` is the separate command for disabling the global hook.
8. Leave the managed hook files on disk unless a later `--purge` option is added.

### `ae-cli hooks status`

Reports hook readiness.

It should show:

1. Whether global `core.hooksPath` points to the AE-managed directory.
2. Whether the current repository has AE-managed repo-local hooks configured in local or worktree config.
3. Whether the current repository has a local or worktree `core.hooksPath` that overrides any global hook path.
4. Which hook mode Git will actually use for the current repository: no executable hook, default `.git/hooks`, AE global, AE repo-local, non-AE global, or non-AE local/worktree.
5. Whether the current repository exists in the local eligibility cache.
6. Whether the current repository has a durable observed identity in `~/.ae-cli/state/hooks/observed-repos.json`.
7. Whether the current repository resolves as eligible in the backend when online.
8. Whether the current effective `core.hooksPath` points to a stale AE-managed hook path from an older contract.
9. Which `ae-cli` executable the hook dispatcher would run.
10. Which managed hook script template version is installed and whether it is stale for the current `ae-cli` binary.
11. Whether `AE_CLI_BIN` is overriding executable resolution.
12. Whether executable default `.git/hooks` scripts exist and whether they are effective or bypassed by the effective hook mode.
13. Optional upload ledger status when requested, including last successful upload, pending count, and last error.

### `ae-cli hooks refresh`

Refreshes local hook eligibility cache entries from the backend.

Behavior:

1. Require a valid login token.
2. Read locally observed repo keys from `~/.ae-cli/state/hooks/observed-repos.json`.
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
4. Write or update the durable observed repo identity for the current repository.
5. Write a positive eligibility cache entry for the returned repository.
6. Ensure user-level attribution state directories exist under `~/.ae-cli/state/attribution/`.
7. Print the current global hook status and the next command if global hooks are not enabled.

`ae-cli init` is no longer the primary hook installer. It is the current-repository registration and cache bootstrap command.

Optional hook installation flags:

- `ae-cli init --hooks none`
- `ae-cli init --hooks repo`
- `ae-cli init --hooks global`
- `ae-cli init --hooks repo --force`
- `ae-cli init --hooks global --force`

The default is `--hooks none`. The command may print suggested follow-up commands, but it should not modify hook configuration unless the user explicitly chooses a hook mode.

If `--hooks repo` or `--hooks global` is selected, `ae-cli init` delegates to the same hook enable contract. `--force` has the same overwrite scope as the delegated command; without `--force`, interactive runs prompt and non-interactive runs fail with a diagnostic.

### `ae-cli sync`

Keeps its user-facing meaning: manually scan local tool artifacts and upload usage for the current repository.

New boundary:

1. Detect the current repository identity and write or update its durable observed repo identity.
2. Resolve current repository eligibility first.
3. If the repository is not backend-known or active, fail with a clear message that points to `ae-cli init` or admin repo configuration.
4. Do not create repositories implicitly from `sync`.

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
      observed-repos.json
      installations.json
```

Directory responsibilities:

1. `~/.ae-cli/token.json`, `~/.ae-cli/config.yaml`, and `~/.ae-cli/env.sh` hold login, user-level CLI configuration, and shell environment bootstrap.
2. `~/.ae-cli/git-hooks/` holds the managed global Git hook scripts.
3. `~/.ae-cli/state/attribution/` holds workspace scan state, hook queues, spooled usage events, collector snapshots, and upload ledgers.
4. `~/.ae-cli/state/hooks/repos.json` holds the local repository eligibility cache.
5. `~/.ae-cli/state/hooks/observed-repos.json` holds durable locally observed repository identities for refresh.
6. `~/.ae-cli/state/hooks/installations.json` tracks AE-managed hook installation locations and enabled/disabled state so `ae-cli` upgrades can rewrite active global hooks and enabled repo-local managed scripts to the latest template.
7. Repo-local hook mode still uses the Git-owned `$(git rev-parse --git-common-dir)/ae-hooks` directory through local or worktree `core.hooksPath`.

The executable is not stored in the state root. The official user-installed binary path remains `~/.local/bin/ae-cli`, and managed hook scripts must not prefer or generate any local debug binary path.

There is no compatibility fallback for old user state roots. New code must not read from, write to, migrate from, or fall back to any user-level state directory outside `~/.ae-cli/`.

The implementation must not create, read, or trust any AE-managed workspace metadata under the repository working tree. Workspace identity is derived from the current Git context at hook runtime, and repository identity is resolved from the current Git remote plus backend eligibility. Existing repository-local workspace metadata from historical versions is ignored by the new hook path and must not be used as migration input for reporting decisions.

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
3. The `ae-cli` client request structs for checkpoint, rewrite, and tool usage include optional `repo_config_id`.
4. Backend handler request structs for checkpoint, rewrite, and tool usage accept optional `repo_config_id` and pass it to their services.
5. Checkpoint and rewrite upload payloads produced by managed global and repo-local hooks include `repo_config_id`.
6. When `repo_config_id` is present, backend checkpoint and rewrite ingest resolve the existing repository by ID and must not call create-or-ensure logic.
7. If the repo is missing or inactive, ingest rejects the upload without creating any repository.
8. Tool usage upload payloads produced by hook-time sync or `ae-cli sync` also include the resolved `repo_config_id`.
9. Backend tool usage ingest uses `repo_config_id` plus the authenticated user to bind the workspace scope when no checkpoint exists yet. This avoids relying on an earlier checkpoint to infer which repository owns a workspace.
10. If `repo_config_id` is omitted, backend tool usage ingest may retain the legacy workspace-scope lookup for non-managed clients. Managed global hooks, managed repo-local hooks, and `ae-cli sync` must not rely on that legacy path.

Legacy clients that do not send `repo_config_id` may continue to use the old behavior where appropriate, but managed global and repo-local hooks must use the resolve-only path for checkpoint, rewrite, and tool usage uploads.

## Local Repository Cache

Eligibility cache file:

```text
~/.ae-cli/state/hooks/repos.json
```

Observed repo file:

```text
~/.ae-cli/state/hooks/observed-repos.json
```

The eligibility cache is keyed by backend-compatible `repo_key`, not by local checkout path. This lets multiple clones or worktrees of the same repository share eligibility.

The observed repo file is the source of locally observed repo keys for `ae-cli hooks refresh`. A hook invocation writes an observed repo identity before the backend knows the repo. `ae-cli init`, `ae-cli sync`, and `ae-cli hooks refresh --current` also write or update the current observed repo identity after they derive the same canonical repo key. That observed identity is not authorization; it only records that this machine has seen the repo and may refresh its eligibility later.

Eligibility cache example:

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

Observed repo example:

```json
{
  "version": 1,
  "updated_at": "2026-05-23T10:00:00Z",
  "repos": {
    "repo-host.example.com/org/repo": {
      "repo_key": "repo-host.example.com/org/repo",
      "remote_url": "git@repo-host.example.com:org/repo.git",
      "first_observed_at": "2026-05-23T09:00:00Z",
      "last_observed_at": "2026-05-23T10:00:00Z"
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

Expired eligibility entries do not authorize hook execution. The dispatcher may try the online resolve endpoint with a short timeout. If that lookup fails, it skips AE hook work for that commit.

Observed repo identities do not expire as part of eligibility TTL cleanup. They may be pruned only by explicit cache maintenance policy such as a long inactivity cutoff or a user-facing purge command. Expiring or deleting an eligibility record must not remove the corresponding observed repo identity.

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
      "git_dir": "/Users/alice/src/repo/.git",
      "git_common_dir": "/Users/alice/src/repo/.git",
      "config_scope": "local",
      "hooks_path": "/Users/alice/src/repo/.git/ae-hooks",
      "enabled": false,
      "template_version": 2,
      "updated_at": "2026-05-23T10:00:00Z"
    },
    {
      "mode": "repo",
      "repo_key": "repo-host.example.com/org/repo",
      "git_dir": "/Users/alice/src/repo-linked/.git",
      "git_common_dir": "/Users/alice/src/repo/.git",
      "config_scope": "worktree",
      "hooks_path": "/Users/alice/src/repo/.git/ae-hooks",
      "enabled": true,
      "template_version": 2,
      "updated_at": "2026-05-23T10:00:00Z"
    }
  ]
}
```

Rules:

1. `hooks enable --global` records or updates one enabled global installation.
2. `hooks enable --repo` records or updates one enabled repo-local installation for the repo-local target config scope selected by the enable operation. The record identity is `mode`, `git_dir`, `config_scope`, and `hooks_path`; `git_common_dir` is metadata and must not be the only key because multiple worktrees can share one common directory while using different worktree-level config.
3. `config_scope` is the repo-local config layer the CLI writes or unsets for `core.hooksPath`. Existing local or worktree values are overwritten in their original layer. Effective global values are never overwritten by `hooks enable --repo`; repo-local mode writes a local or worktree override instead. New values use `worktree` when `extensions.worktreeConfig=true`; otherwise they use `local`.
4. `hooks disable --global` and `hooks disable --repo` mark matching records disabled only after the matching Git config value is unset or already absent.
5. Installer and upgrade refresh rewrite enabled repo-local records only. Disabled repo-local records may remain for diagnostics and should not reactivate hooks.
6. The effective Git config is authoritative for global hook activation. If the current global `core.hooksPath` points to `~/.ae-cli/git-hooks`, installer or upgrade refresh treats global hooks as enabled and records or updates an enabled global installation, even if a stale disabled record exists. `hooks disable --global` must unset the global Git config and mark the record disabled; if both steps cannot be completed, status must report the mismatch.

## Repository Identity

The CLI and backend must derive the same `repo_key` from common remote URL forms:

- `git@repo-host.example.com:org/repo.git`
- `ssh://git@repo-host.example.com/org/repo.git`
- `https://repo-host.example.com/org/repo.git`

The implementation should introduce a CLI-side repository identity helper with tests that mirror the backend identity behavior. The cache lookup must not rely on raw remote URL string equality.

## Workspace Identity

The hook path must derive `workspace_id` from the current Git context every time it runs. It must use the same deterministic formula as the existing sessionless attribution design and `ae-cli/internal/session.DeriveWorkspaceID`:

```text
UUIDv5(
  "ae-workspace",
  canonical_repo_root + "\x1f" +
  canonical_workspace_root + "\x1f" +
  canonical_git_dir + "\x1f" +
  canonical_git_common_dir
)
```

Inputs:

- `git rev-parse --show-toplevel`
- `git rev-parse --absolute-git-dir`
- `git rev-parse --git-common-dir`

For this hook contract, `canonical_repo_root` and `canonical_workspace_root` both come from the canonicalized Git top-level path. All canonical paths are absolute, cleaned, and symlink-resolved before hashing.

The derived value is the only active workspace identity for hook queues, scan state, spooled usage events, collector snapshots, and upload ledgers. The same worktree must derive the same `workspace_id` across hook, `sync`, and status paths; different linked worktrees for the same repository must derive different `workspace_id` values because their absolute git directories differ. The implementation must not trust persisted workspace metadata from the repository working tree. If historical repository-local workspace metadata exists from older versions, the new hook path ignores it.

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
3. Write or update the durable observed repo identity on a best-effort basis.
4. Check the local eligibility cache.
5. If positive and unexpired, run the existing AE hook handler logic with the cached `repo_config_id`.
6. If negative and unexpired, skip AE work.
7. On cache miss or expiry, call `POST /api/v1/repos/resolve-remote` with a hard timeout.
8. If resolve returns eligible, update the positive cache and run the existing AE hook handler logic.
9. If resolve returns ineligible, update the negative cache and skip AE work.
10. If resolve times out or fails, skip AE work.

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
7. Global hook scripts are rewritten at the fixed `~/.ae-cli/git-hooks` path when the current global `core.hooksPath` points to the AE-managed global path. Installer or upgrade refresh must reconcile this effective config into an enabled global installation record, so an old disabled registry entry cannot leave an active global hook stale.
8. If global `core.hooksPath` does not point to the AE-managed global path, a disabled or missing global installation record is not rewritten or reactivated.
9. Repo-local hook scripts are rewritten from enabled records in `~/.ae-cli/state/hooks/installations.json`, which records only AE-managed hook locations installed by `ae-cli hooks enable --repo`.
10. Disabled repo-local records are ignored by installer/upgrade refresh.
11. Missing or inaccessible recorded repo-local locations are skipped with diagnostics; upgrade must not fail because a repository was deleted or moved.

## Existing Hook Compatibility

Both hook modes make ownership explicit instead of preserving existing non-AE hook behavior. A global `core.hooksPath` changes Git's default behavior because repository-local `.git/hooks/<hook>` scripts are no longer executed by Git directly. Global enable protects an existing non-AE global `core.hooksPath`, but it cannot preflight every repository's default hook directory; therefore it must warn about that Git-level ownership change. A repo-local AE `core.hooksPath` can also replace previous effective hook behavior for the current repository. AE only changes non-AE hook behavior, or a current repository default hook directory with executable hooks, when the user confirms interactively or passes `--force`.

Compatibility rules:

1. If a repository has a local or worktree `core.hooksPath`, Git uses that path and the global AE hook does not run. `ae-cli hooks status` must report this clearly.
2. If a repository has executable default hooks under `.git/hooks`, global AE hooks do not run them.
3. `ae-cli hooks enable --repo` replaces the current repository's effective non-AE hook behavior only with confirmation or `--force`. This includes a non-AE local/worktree `core.hooksPath`, a non-AE global `core.hooksPath`, and the implicit default `.git/hooks` path when it contains executable hooks for AE-managed events. If the effective non-AE value comes from global config, repo-local enable writes a local or worktree override and must not modify global Git config.
4. If the current active `core.hooksPath` points to an older AE-managed repo-local hook directory, `ae-cli hooks status` reports it as stale. `ae-cli hooks enable --repo` rewrites that active path to the current contract, and `ae-cli hooks disable --repo` may unset that current active stale path from local or worktree config. The CLI must not scan arbitrary repositories or historical directories to migrate stale hooks.
5. Managed dispatchers must avoid recursive execution of AE-managed global or repo-local hook directories.
6. `hooks enable --global` must not silently replace a non-AE global `core.hooksPath`. `hooks enable --repo` must not silently replace current-repository non-AE hook behavior, including executable default hooks. `--force` authorizes overwrite; without `--force`, interactive runs prompt and non-interactive runs fail with a diagnostic.

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
- `workspace_id` derived with the shared deterministic formula from the current Git context

It must not upload local tool artifacts, prompts, raw payloads, file paths, or usage events until repository eligibility has been confirmed.

## Testing Plan

### CLI unit tests

1. Global and repo-local hook scripts use runtime executable resolution.
2. Global hook scripts do not chain default repository hooks.
3. Repo-local hook scripts do not preserve or chain existing local hooks.
4. `post-rewrite` preserves stdin for AE handling.
5. Local `core.hooksPath` is detected and reported by `hooks status`.
6. `hooks status` reports whether Git will use no executable hook, default `.git/hooks`, AE global, AE repo-local, non-AE global, or non-AE local/worktree hooks.
7. Eligibility cache handles positive, negative, expired, and malformed entries.
8. Cache miss resolve uses the configured hard timeout.
9. Unknown repositories do not run hook handler upload logic.
10. Eligible repositories pass `repo_config_id` through CLI client request structs, hook handler upload logic, and backend handler request structs.
11. Hook execution derives `workspace_id` with the shared deterministic UUIDv5 formula and ignores historical repository-local workspace metadata.
12. `ae-cli init` creates only user-level attribution state, not repository working-tree state.
13. New code writes state only under `~/.ae-cli/` and does not read or migrate old user-level state roots.
14. Managed hook scripts resolve `~/.local/bin/ae-cli` and do not prefer local debug binary paths or install-time executable paths.
15. `hooks enable --global` refuses to overwrite a non-AE global `core.hooksPath` in non-interactive mode without `--force`.
16. `hooks enable --repo` refuses to overwrite current-repository non-AE hook behavior in non-interactive mode without `--force`, including executable default hooks.
17. `hooks enable --global --force` overwrites the global hook path, while `hooks enable --repo --force` writes a local or worktree override for the current repository; neither mode preserves previous hook paths.
18. `hooks status` reports installed hook template versions and stale managed scripts.
19. `AE_CLI_BIN` overrides executable resolution and is reported by `hooks status`.
20. Upload ledgers record pending, uploaded, failed, and skipped operational metadata without raw payloads.
21. `hooks refresh` sends only locally observed repo identities to the batch eligibility endpoint and does not accept unrelated repos.
22. Expired eligibility entries do not remove durable observed repo identities.
23. `ae-cli init`, `ae-cli sync`, hook-time resolve, and `hooks refresh --current` write or update durable observed repo identities.
24. `hooks status` reports whether the current repository has a durable observed repo identity.
25. `hooks status` reports executable default `.git/hooks` scripts and whether they are effective or bypassed by the effective hook mode.
26. `hooks status` checks stale repo-local AE hooks only through the current effective `core.hooksPath`, not by scanning historical hook directories.
27. `hooks enable --repo` records AE-managed repo-local hook locations as enabled in `~/.ae-cli/state/hooks/installations.json`.
28. `hooks enable --repo` treats executable default `.git/hooks/post-commit` or `.git/hooks/post-rewrite` scripts as hook behavior that requires confirmation or `--force` before replacement.
29. Repo-local installation records distinguish worktree-level and local config using `git_dir`, `git_common_dir`, `config_scope`, and `hooks_path`, so two worktrees sharing one common directory do not overwrite each other's registry state.
30. `hooks disable --repo` and `hooks disable --global` mark matching installation records disabled only after the matching Git config value is unset or already absent.
31. The installer or upgrade refresh rewrites active global hooks and enabled recorded repo-local AE hooks to the current template without preserving previous non-AE hooks.
32. A disabled global record is reconciled back to enabled when global `core.hooksPath` still points to `~/.ae-cli/git-hooks`; a disabled global record is not rewritten or reactivated after that Git config value is unset.
33. Disabled repo-local installation records are not rewritten or reactivated during installer/upgrade refresh.
34. The same worktree derives one stable `workspace_id` across hook, `sync`, and status paths; two linked worktrees for the same repository derive different `workspace_id` values.
35. `hooks enable --repo` with an existing non-AE global `core.hooksPath` refuses without `--force`; with `--force`, it writes a local or worktree override and leaves global Git config unchanged.
36. `hooks enable --repo` with an existing AE-managed global `core.hooksPath` succeeds without `--force`, writes or refreshes the repo-local override, records the repo-local installation, and leaves global Git config unchanged.
37. `hooks disable --repo` unsets only AE-managed local or worktree hook config; it must not unset or rewrite global Git config, and it reports when AE-managed global hooks remain effective afterward.
38. `hooks disable --repo` can unset the current active recognized stale AE-managed repo-local hook path from local or worktree config without scanning arbitrary historical directories.

### Backend unit tests

1. `resolve-remote` returns eligible for an existing active backend-known repo.
2. `resolve-remote` returns `not_found` without creating a repo.
3. `resolve-remote` returns `inactive` for inactive repos.
4. `resolve-remote` does not apply per-user repo reporting permission in v1; a valid authenticated user may report to any active backend-known repo.
5. `hook-eligible` returns active backend-known matches only for requested repo identities.
6. Checkpoint handler and service accept `repo_config_id`; when present, ingest resolves by ID without create-or-ensure fallback.
7. Rewrite handler and service accept `repo_config_id`; when present, ingest resolves by ID without create-or-ensure fallback.
8. Tool usage handler and service accept `repo_config_id` and bind workspace scope without requiring a previous checkpoint.
9. `hook-eligible` returns results only for requested repo identities and does not enumerate unrelated active repos.

### Integration checks

1. Enable global hooks.
2. Enable repo-local hooks in a separate repository without changing global Git config.
3. Commit in a backend-known eligible repository and verify a checkpoint is uploaded.
4. Commit in an unknown repository and verify no backend repository, checkpoint, rewrite, or usage event is created.
5. Commit in a repository with a default `.git/hooks/post-commit` and verify the pre-existing default hook is bypassed after AE owns the effective hook behavior.
6. Upgrade `~/.local/bin/ae-cli` and verify the dispatcher uses the upgraded binary.
7. Put stale historical repository-local workspace metadata in a repository and verify the hook ignores it.
8. Put stale state under an old user-level state root and verify the new CLI ignores it.
9. Configure a non-AE global `core.hooksPath`; verify `hooks enable --global` refuses without `--force`, and verify `hooks enable --repo` refuses without `--force` but succeeds with `--force` by writing a local or worktree override without changing global Git config.
10. Generate a stale managed hook script and verify status reports the current and installed template versions.
11. Upgrade `ae-cli` and verify managed hook scripts are rewritten to the latest template at fixed global and recorded repo-local locations.
12. Enable repo-local hooks in two worktrees that share one common directory and verify the registry keeps separate worktree/local config records.
13. Run `ae-cli init`, `ae-cli sync`, and a hook-time resolve in the same repository and verify each path updates the same durable observed repo identity.
14. Put executable default `.git/hooks/post-commit` and `.git/hooks/post-rewrite` scripts in a repo with empty `core.hooksPath` and verify `hooks enable --repo` refuses without `--force`.
15. Configure an AE-managed global hook, enable repo-local hooks in one repository, then run `hooks disable --repo`; verify only the repo-local override is removed and the global AE hook remains active.

## Rollout Plan

1. Add backend read-only repository eligibility endpoints.
2. Add `repo_config_id` support to ae-cli client payloads, backend handler requests, and checkpoint, rewrite, and tool usage ingest.
3. Add CLI repository identity and eligibility cache packages.
4. Add global and repo-local hook install, disable, refresh, and status commands.
5. Change `ae-cli init` to register the current repo and update cache instead of being the primary hook installer.
6. Remove historical repository-local workspace metadata read, write, and environment-bootstrap behavior from the active hook path.
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
