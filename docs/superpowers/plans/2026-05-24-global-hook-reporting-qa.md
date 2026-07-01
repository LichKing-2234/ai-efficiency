# Global Hook Reporting QA Report

**Date:** 2026-05-24
**Branch:** `fix/hook-codex-scan-timeout`
**Scope:** CLI hook installation and runtime upload, local upload cache/replay, backend ingest/query APIs, and frontend Events visibility.
**QA workspaces:**

- Online reporting and frontend QA: `/tmp/aeqa_20260524183051_63550`
- Local upload cache/replay QA: `/tmp/aeqa_cache_20260524191040_68561`
- Extended spec QA: `/tmp/aeqa_spec_20260524194706_13462`
- Public release installer QA: `/tmp/aeqa_public_installer_proxy_20260524221832_65763`
- Command and registry edge QA: `/tmp/aeqa_uncovered3_20260524225103_22802`
- Active positive TTL expiry QA: `/tmp/aeqa_cache_20260524191040_68561/evidence/91-real-positive-cache-ttl-expiry-qa.txt`

## Summary

The core reporting path is working in the real local QA environment:

1. A real Git `post-commit` hook completed within 1 second and created a backend commit checkpoint for an eligible backend-known repository.
2. The same hook run scanned a real Codex JSONL artifact and uploaded one managed `tool_usage_events` row with `repo_config_id`, `user_id`, and `workspace_id`.
3. The uploaded usage event was bound to the commit checkpoint and is visible through both backend Events APIs and the frontend Events page.
4. Unknown and inactive repositories did not upload checkpoints or usage from hook-triggered commits.
5. Managed uploads did not persist raw source path, raw source locator, or raw payload.
6. Local hook upload replay is working for checkpoint uploads: failed uploads were queued under the workspace state directory, retry attempts were written to `upload-ledger.jsonl`, backend recovery replayed the queued checkpoints before the current commit upload, and a mismatched queued event was skipped instead of being uploaded under the wrong repo context.
7. Cross-repository isolation worked in the replay QA: a commit in repo B uploaded only repo B's checkpoint and did not flush repo A's pending queue.
8. Hook ownership lifecycle checks passed for non-AE global refusal, repo-local `--force` scope, default executable hook replacement, repo/global Git config separation, stale template status, and basic managed template refresh.
9. Status mode reporting identified all exercised effective modes: `none`, `git_default`, `non_ae_global`, `ae_global`, `ae_repo`, and `non_ae_repo`.
10. Token safety checks passed for missing tokens, malformed tokens, expired tokens without refresh, and expired tokens with bad refresh. An expired token with a valid refresh token refreshed successfully and uploaded, which is the expected usable-credential path.
11. Historical state isolation passed: stale `~/.ai-efficiency` state and repository-local `.ae/session.json` remained ignored by the active hook path.
12. Tool usage spool replay passed for a real offline `ae-cli sync`: the offline run wrote a workspace-scoped `spool.json`, backend recovery replayed it into `tool_usage_events`, and backend ingest omitted raw local source fields.
13. `post-rewrite` queue/replay passed through real Git hooks: an offline `git commit --amend` queued a rewrite event, and a later real commit replayed it into `commit_rewrites`.
14. Linked worktree QA passed for workspace separation: two linked worktrees for the same backend repo produced different `workspace_id` values and two real hook checkpoints.
15. Runtime binary resolution passed without `AE_CLI_BIN`: the managed hook resolved `~/.local/bin/ae-cli` and a real commit uploaded a checkpoint.
16. Upload-cache QA passed with CLI-created state only: real `ae-cli init` created the positive eligibility cache, two real backend-down commits queued checkpoint uploads and wrote a failed ledger entry, and a later real commit replayed the queue into the backend.
17. Negative eligibility cache TTL passed with real elapsed time: an unknown repo wrote a `not_found` cache entry, remained skipped immediately after the backend repo was created, then uploaded after the 5 minute TTL expired.
18. Backend eligibility edge cases passed for `webhook_failed`, remote URL normalization, inactive `repo_config_id` rejection, missing `repo_config_id` rejection, batch endpoint scope, and explicit `repo_config_id` tool-usage binding.
19. Additional command and registry edge QA passed for `ae-cli doctor` including hook status and online repo eligibility diagnostics, disabled global record reconciliation when Git config still points at the canonical AE global hook path, no reactivation after global Git config is unset, one local-scope registry record for a linked worktree pair sharing the same common directory, and no arbitrary historical hook-directory scan in `hooks status`.

Real issues exposed:

- Backend CORS defaults blocked a valid isolated frontend/backend QA topology using `http://127.0.0.1:<port>`, even when `AE_SERVER_FRONTEND_URL` was set. Fixed in this branch by passing the configured frontend URL into CORS and allowing same-port localhost/127.0.0.1 loopback variants. Unit coverage: `cd backend && go test ./internal/middleware -count=1`.
- `hooks enable --repo` under an existing AE-managed global hook refused without `--force` when executable default hooks existed in the global AE hook directory. Fixed in this branch by only protecting executable default hooks when no effective `core.hooksPath` exists. Unit coverage: `cd ae-cli && go test ./internal/hooks -count=1`.
- `hooks refresh --current` updated the eligibility cache but did not update the durable observed repo identity for the current repo. Fixed in this branch by writing the current observed repo binding during refresh. Unit coverage: `cd ae-cli && go test ./internal/hooks -count=1`.
- Batch `hooks refresh` was a no-op and did not call `POST /api/v1/repos/hook-eligible`. Fixed in this branch by reading context-matched observed repos, calling the batch endpoint, and updating positive/negative cache entries. Unit coverage: `cd ae-cli && go test ./internal/hooks -count=1`.
- Tool-usage spool context mismatch was dropped without an upload-ledger `skipped` record, while the spec requires skipped ledger diagnostics for both hook queues and tool-usage spool replay. Fixed in this branch for tool-usage spool replay. Unit coverage: `cd ae-cli && go test ./internal/attributionlocal -count=1`.
- `hooks status` was thinner than the spec: it reported the effective mode, template, binary, and eligibility, but not the current context fingerprint, observed repo binding state, default-hook effective/bypassed detail, or installed/current template version numbers. Fixed in this branch for status output. Unit coverage: `cd ae-cli && go test ./cmd -count=1`.
- `hooks disable --repo` stopped after unsetting the worktree AE hook layer and could leave a lower-precedence local AE repo-local hook still effective for that worktree. Fixed in this branch by repeatedly unsetting effective AE repo-local layers until none remain. Unit coverage: `cd ae-cli && go test ./internal/hooks -count=1`.
- The current public releases do not yet satisfy the installer hook-refresh contract: real installs of `v0.1.0-preview.10` and latest `v0.1.0-preview.11` succeed, but both installed binaries lack the `hooks` command, so installer-triggered `hooks refresh-installations` fails with a warning.
- `hooks disable --repo` could leave the matching repo-local installation record enabled after the Git config value was unset, so `hooks refresh-installations` rewrote a repo-local hook the user just disabled. Fixed in this branch, including already-absent registry reconciliation. Unit coverage: `cd ae-cli && go test ./internal/hooks -count=1`.
- `hooks refresh-installations` recreated recorded repo-local hook paths even after the backing repository directory was deleted, instead of skipping the inaccessible location with diagnostics. Fixed in this branch by skipping inaccessible repo-local records with diagnostics. Unit coverage: `cd ae-cli && go test ./internal/hooks -count=1`.
- `ae-cli sync status` was not implemented as a real status command; positional `status` was treated as an ignored argument to `ae-cli sync`. Fixed in this branch by adding a real `sync status` subcommand that reuses upload status reporting. Unit coverage: `cd ae-cli && go test ./cmd -count=1`.
- Local-scope repo disable affected linked worktrees through shared local Git config, but the command printed no shared-common-directory warning. Fixed in this branch by reporting the shared local config impact when a local-scoped repo hook is disabled. Unit coverage: `cd ae-cli && go test ./cmd -count=1`.

## Environment

- Backend binary: `/tmp/ai-efficiency-server-qa-build`, built from the current worktree, version output `dev`.
- CLI binary: `/tmp/ae-cli-qa-build`, built from the current worktree, version output `ae-cli v0.1.0`.
- Database: Postgres container `ai-efficiency-postgres-1`, isolated database `aeqa_20260524183051`.
- Redis: container `ai-efficiency-redis-1`.
- Test auth: backend debug `POST /api/v1/auth/dev-login`, user id `1`, role `admin`.
- Temporary CLI home: `/tmp/aeqa_20260524183051_63550/home`.
- Temporary global Git config: `/tmp/aeqa_20260524183051_63550/gitconfig`.
- Active test repo remote: `https://repo-host.example.com/org/repo.git`.

## Verification Commands

- `cd ae-cli && go test ./internal/attributionlocal ./internal/hooks ./cmd -count=1`
- `cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/handler ./internal/checkpoint ./internal/toolusage ./internal/repo -count=1`
- `cd frontend && npm install`
- `cd frontend && pnpm test`
- `cd ae-cli && go build -o /tmp/ae-cli-qa-build .`
- `cd backend/cmd/server && go build -o /tmp/ai-efficiency-server-qa-build .`
- Real Git commands with `HOME=/tmp/aeqa_20260524183051_63550/home`, `GIT_CONFIG_GLOBAL=/tmp/aeqa_20260524183051_63550/gitconfig`, and `AE_CLI_BIN=/tmp/ae-cli-qa-build`.
- Browser validation through Playwright CLI against `http://localhost:5173`.
- Cache/replay QA build commands:
  - `cd ae-cli && go build -o /tmp/aeqa_cache_20260524191040_68561/bin/ae-cli .`
  - `cd backend/cmd/server && go build -o /tmp/aeqa_cache_20260524191040_68561/bin/server .`
- Cache/replay QA real Git commands used `HOME=/tmp/aeqa_cache_20260524191040_68561/home`, `GIT_CONFIG_GLOBAL=/tmp/aeqa_cache_20260524191040_68561/gitconfig`, and `AE_CLI_BIN=/tmp/aeqa_cache_20260524191040_68561/bin/ae-cli`.
- Cache/replay QA database checks used Postgres database `aeqa_cache_20260524191040_68561`.
- Extended spec QA build commands:
  - `cd ae-cli && go build -o /tmp/aeqa_spec_20260524194706_13462/bin/ae-cli .`
  - `cd backend/cmd/server && go build -o /tmp/aeqa_spec_20260524194706_13462/bin/server .`
- Extended spec QA real Git commands used `HOME=/tmp/aeqa_spec_20260524194706_13462/home`, `GIT_CONFIG_GLOBAL=/tmp/aeqa_spec_20260524194706_13462/gitconfig`, and either `AE_CLI_BIN=/tmp/aeqa_spec_20260524194706_13462/bin/ae-cli` or no `AE_CLI_BIN` when testing stable `~/.local/bin/ae-cli` runtime resolution.
- Extended spec QA backend ran at `http://127.0.0.1:19083` against Postgres database `aeqa_spec_20260524194706_13462`.
- Public release installer QA used the current worktree `ae-cli/install.sh`, an isolated `HOME`, an isolated `GIT_CONFIG_GLOBAL`, real GitHub release metadata/assets, and the machine HTTP proxy `http://127.0.0.1:6666` because direct release-asset downloads timed out.
- Command and registry edge QA built `/tmp/ae-cli-realqa` from the current worktree and used real Git and CLI commands under `HOME=/tmp/aeqa_uncovered3_20260524225103_22802/home` and `GIT_CONFIG_GLOBAL=/tmp/aeqa_uncovered3_20260524225103_22802/gitconfig`.
- Positive TTL expiry QA uses the existing real CLI-created positive cache from `/tmp/aeqa_cache_20260524191040_68561`; the waiting runner is `/tmp/aeqa_cache_20260524191040_68561/run-positive-ttl-expiry-qa.sh`.

## Local Cache And Upload Replay Coverage

This report includes local cache and upload replay validation. The authoritative evidence is from real CLI, backend, and Git behavior only:

| State file | Real coverage | Evidence |
| --- | --- | --- |
| `~/.ae-cli/state/hooks/repos.json` | Positive eligibility cache was written by real `ae-cli init`; negative cache was written by a real unknown-repo hook resolve. Positive cache authorized backend-down hook work only with a usable token. Negative cache skipped immediate uploads until the real 5 minute TTL expired. | `87-real-upload-cache-from-cli-qa.txt`, `82-real-negative-cache-ttl-qa.txt`, `20-refresh-auth-old-status.txt` |
| `~/.ae-cli/state/hooks/observed-repos.json` | Real `ae-cli init`, `ae-cli sync`, and hook-time resolve wrote the same observed identity for one repo context. `hooks refresh --current` did not update the observed repo file, which is tracked as QA-3. | `80-real-uncovered-spec-qa.txt`, `20-refresh-auth-old-status.txt` |
| Workspace `hooks.jsonl` | Two real backend-down Git commits queued checkpoint uploads with `server_url`, `auth_subject`, `repo_config_id`, `repo_key`, `workspace_id`, and commit SHA. A later real commit replayed them and cleared the queue. | `87-real-upload-cache-from-cli-qa.txt` |
| Workspace `spool.json` | A real backend-down `ae-cli sync` wrote tool-usage spool entries with backend/account/repo/workspace binding, and a later real sync replayed them into `tool_usage_events`. | `30-spool-rewrite-worktree.txt` |
| Workspace `upload-ledger.jsonl` | Real failed checkpoint replay wrote `failed`; real checkpoint/rewrite replay wrote `uploaded`; real hook context mismatch wrote `skipped`. Tool-usage spool mismatch still clears without a skipped ledger row, tracked as QA-5. | `87-real-upload-cache-from-cli-qa.txt`, `30-spool-rewrite-worktree.txt`, `70-real-hook-context-switch.txt`, `60-real-spool-context-switch.txt` |
| Backend rows | Replay created only rows for the current repo/workspace context. Repo B commits did not flush repo A queues. Context-switched queued data was skipped instead of uploaded under the wrong server context. | `87-real-upload-cache-from-cli-qa.txt`, cache/replay QA evidence, `70-real-hook-context-switch.txt` |

## CLI And Hook Results

| Case | Result | Evidence |
| --- | --- | --- |
| Global hook install | Pass | `core.hooksPath=/tmp/aeqa_20260524183051_63550/home/.ae-cli/git-hooks`; scripts `post-commit` and `post-rewrite` were executable. |
| Active repo real commit | Pass | Commit `23a7e1480d1ae28f8152bfeb9b99bc656e0a6d0f`; command completed in 1 second. |
| Active repo amend | Pass | New commit `eabc453f4eaaaae8704d0f7c1c5f049930e06103`; one `commit_rewrites` row recorded the amend pair. |
| Unknown repo hook commit | Pass | Commit completed; repo count did not change during the hook path; no checkpoint or usage event was created for the unknown remote. |
| Inactive repo hook commit | Pass | Commit completed; no checkpoint or usage event was created for the inactive backend repo. |
| `ae-cli sync` for fresh unknown repo | Pass | Exit status `1`; backend repo count for `repo-host.example.com/org/no-sync` stayed `0 -> 0`. |
| `ae-cli init --hooks none` | Pass | Repo was linked as explicit user action; local and worktree `core.hooksPath` stayed empty. |
| `ae-cli hooks enable --repo --force` | Pass | Repo-local path set to `/private/tmp/aeqa_20260524183051_63550/repos/repo-local/.git/ae-hooks`; global path stayed unchanged. |
| `ae-cli hooks status --uploads` | Pass | Reported `Global: enabled`, `Effective: ae_global`, `Binary: /tmp/ae-cli-qa-build`, `Eligibility: eligible repo_config_id=1`. |

## Local Upload Cache, Replay, And Isolation Results

The upload cache QA used a second isolated environment so the test could stop and restart the backend without affecting the online reporting evidence:

- Backend: `http://127.0.0.1:19082`
- Backend binary: `/tmp/aeqa_cache_20260524191040_68561/bin/server`, built from the current worktree, version output `dev`.
- CLI binary: `/tmp/aeqa_cache_20260524191040_68561/bin/ae-cli`, built from the current worktree, version output `ae-cli v0.1.0`.
- Database: Postgres container `ai-efficiency-postgres-1`, isolated database `aeqa_cache_20260524191040_68561`.
- Temporary CLI home: `/tmp/aeqa_cache_20260524191040_68561/home`.
- Temporary global Git config: `/tmp/aeqa_cache_20260524191040_68561/gitconfig`.
- Repo A: `repo_config_id=1`, remote `https://repo-host.example.com/org/cache-a.git`, workspace `c5727cd0-1ab0-5bf7-830c-908a688aefd0`.
- Repo B: `repo_config_id=2`, remote `https://repo-host.example.com/org/cache-b.git`, workspace `f4452236-f0c4-55cd-a963-0e5a35a70592`.

The setup wrote positive eligibility cache entries for both repo A and repo B under the isolated home:

```text
repo_config_id=1 repo_key=repo-host.example.com/org/cache-a server=http://127.0.0.1:19082 auth_subject=user:1
repo_config_id=2 repo_key=repo-host.example.com/org/cache-b server=http://127.0.0.1:19082 auth_subject=user:1
```

### Backend-Down Failure Queue

After positive eligibility was present for repo A, the backend process was stopped. Two real Git commits were created in repo A with the global hook enabled and `AE_CLI_BIN` pointing to the freshly built QA CLI:

```text
A_COMMIT1=e42f38c6d0c904731bd553f60e5fe282001c5ad6 seconds=1.577
A_COMMIT2=d3903c7649e2faaf38405635d9944038048e0b8b seconds=0.098
```

The commits completed fail-open while the backend was unreachable. The backend database still had zero checkpoint rows at that point:

```text
select count(*) from commit_checkpoints;
0
```

The local queue file was created under the repo A workspace:

```text
/tmp/aeqa_cache_20260524191040_68561/home/.ae-cli/state/attribution/workspaces/c5727cd0-1ab0-5bf7-830c-908a688aefd0/hooks.jsonl
```

It contained both failed commit uploads, each carrying the stable replay binding dimensions:

```text
commit=e42f38c6d0c904731bd553f60e5fe282001c5ad6 repo_config_id=1 repo_key=repo-host.example.com/org/cache-a workspace=c5727cd0-1ab0-5bf7-830c-908a688aefd0
commit=d3903c7649e2faaf38405635d9944038048e0b8b repo_config_id=1 repo_key=repo-host.example.com/org/cache-a workspace=c5727cd0-1ab0-5bf7-830c-908a688aefd0
```

The second real commit also attempted to flush the first queued event while the backend was still down. That retry wrote a failed ledger record and kept the queue pending:

```text
status=failed repo_config_id=1 repo_key=repo-host.example.com/org/cache-a workspace=c5727cd0-1ab0-5bf7-830c-908a688aefd0 last_error="sending checkpoint request: Post \"http://127.0.0.1:19082/api/v1/checkpoints/commit\": dial tcp 127.0.0.1:19082: connect: connection refused"
```

### Cross-Repository Isolation

The backend was restarted against the same isolated database. Before replaying repo A, a real commit was made in repo B:

```text
B_COMMIT=436a202e37a76c87c868547033d1ee1dfc05a60d
```

Repo B uploaded its own checkpoint, but repo A's queue and ledger were unchanged:

```text
A queue lines before repo B commit: 2
A queue lines after repo B commit: 2
A ledger lines before repo B commit: 1
A ledger lines after repo B commit: 1
```

This proves a repo B hook run did not flush repo A's workspace queue.

### Backend Recovery Replay

After the repo B isolation check, a third real commit was made in repo A with the backend online:

```text
A_COMMIT3=4071bd4ffe917730b0845a1a26e716c1a52c6797
```

The repo A hook run first replayed the two queued checkpoint uploads, then uploaded the current commit. The repo A queue file was removed because it became empty:

```text
A queue lines before recovery commit: 2
A queue exists after recovery commit: no
A ledger lines before recovery commit: 1
A ledger lines after recovery commit: 3
```

The repo A ledger then contained one failed retry record and two successful replay records:

```text
failed   repo_config_id=1 repo_key=repo-host.example.com/org/cache-a workspace=c5727cd0-1ab0-5bf7-830c-908a688aefd0 dedupe_key=ca56f57ac476b596023c6f6fb5ab203a2637393f91b47a10c39b285943a2cb3e
uploaded repo_config_id=1 repo_key=repo-host.example.com/org/cache-a workspace=c5727cd0-1ab0-5bf7-830c-908a688aefd0 dedupe_key=ca56f57ac476b596023c6f6fb5ab203a2637393f91b47a10c39b285943a2cb3e
uploaded repo_config_id=1 repo_key=repo-host.example.com/org/cache-a workspace=c5727cd0-1ab0-5bf7-830c-908a688aefd0 dedupe_key=41f1fd628f8797dd095bc1752a6da02288a636c9088406cb99dde8e41f02a8d9
```

### Positive Cache TTL Expiry

The 24 hour positive eligibility TTL cannot be truthfully compressed without altering local state or time. A real-time QA runner has therefore been started from the earlier real CLI-created cache for repo A. It does not edit cache timestamps, prewrite queues, or seed upload state.

Current active evidence:

```text
qa_root=/tmp/aeqa_cache_20260524191040_68561
repo=/tmp/aeqa_cache_20260524191040_68561/repos/cache-a
repo_key=repo-host.example.com/org/cache-a
repo_config_id=1
workspace_id=c5727cd0-1ab0-5bf7-830c-908a688aefd0
last_resolved_at=2026-05-24T19:13:09.439365+08:00
expires_at=2026-05-25T19:13:09.439365+08:00
preflight_utc=2026-05-24T15:07:13Z
preflight_local=2026-05-24T23:07:13+0800
seconds_until_expiry=72357
queue_lines_before_wait=0
ledger_lines_before_wait=4
cache_sha_before_wait=cfe16b047c2dbef77acbcc3ecf42ab3222f9575daba0df9951b59c256b11101e
db_checkpoints_before_wait=3
backend_listener_19082=
git_head_before_wait=4071bd4ffe917730b0845a1a26e716c1a52c6797
wait_start_utc=2026-05-24T15:07:13Z
sleep_seconds=72359
```

The QA is scheduled through a temporary macOS LaunchAgent because a shell-owned 24 hour sleep is not reliable across tool sessions. Launchd has minute granularity, so the runner recomputes `expires_at` and sleeps until after the exact expiry before committing. The schedule evidence is recorded in:

```text
/tmp/aeqa_cache_20260524191040_68561/evidence/91-real-positive-cache-ttl-expiry-qa.schedule.txt
```

```text
launch_agent=/Users/admin/Library/LaunchAgents/com.ai-efficiency.qa.positive-ttl-expiry.plist
label=com.ai-efficiency.qa.positive-ttl-expiry
scheduled_local=2026-05-25T19:13:00+0800
expires_at=2026-05-25T19:13:09.439365+08:00
note=launchd has minute granularity; runner recomputes expires_at and sleeps until after exact expiry before committing
installed_utc=2026-05-24T15:10:50Z
last_checked_utc=2026-05-24T15:20:08Z
launchd_runs=0
launchd_last_exit_code=never exited
```

The runner will resume after natural expiry and make a real Git commit with the backend still unavailable on port `19082`. Passing evidence requires the commit to complete while queue line count, upload-ledger line count, and backend checkpoint count do not increase. Until that after-expiry commit runs, this case remains in progress rather than passed.

The backend checkpoint table after recovery contained repo B's checkpoint plus all three repo A checkpoints:

```text
1 repo_config_id=2 user_id=1 commit=436a202e37a76c87c868547033d1ee1dfc05a60d workspace=f4452236-f0c4-55cd-a963-0e5a35a70592 binding_source=unbound
2 repo_config_id=1 user_id=1 commit=e42f38c6d0c904731bd553f60e5fe282001c5ad6 workspace=c5727cd0-1ab0-5bf7-830c-908a688aefd0 binding_source=unbound
3 repo_config_id=1 user_id=1 commit=d3903c7649e2faaf38405635d9944038048e0b8b workspace=c5727cd0-1ab0-5bf7-830c-908a688aefd0 binding_source=unbound
4 repo_config_id=1 user_id=1 commit=4071bd4ffe917730b0845a1a26e716c1a52c6797 workspace=c5727cd0-1ab0-5bf7-830c-908a688aefd0 binding_source=unbound
```

### Context Mismatch Skip

The extended spec QA used real token-context switching and live queue generation. It logged in against a real alternate backend context at `http://127.0.0.1:19084`, stopped that backend, and made a real Git commit in `spec-beta`. The hook created a real queue item bound to the alternate server:

```text
alt_server_commit=f6d1c4c86796ccc449461b0049401d22ef04b36b
queue_exists_after_alt_offline=yes
server_url=http://127.0.0.1:19084
repo_config_id=2
repo_key=repo-host.example.com/org/spec-beta
```

The test then switched the token context back to the original backend at `http://127.0.0.1:19083` and made another real Git commit. The original-context hook skipped the alternate-server queued item, uploaded only the current commit, removed the queue, and wrote a `skipped` ledger row:

```text
original_context_commit=f2cdbdc6415c3b711c8ac0f3eb4ed3690085ecf7
alt_checkpoint_count=0
orig_checkpoint_count=1
queue_exists_after_original=no
ledger_lines=1
status=skipped last_error=context mismatch
```

No tool usage events were created during the cache/replay-only QA run:

```text
select count(*) from tool_usage_events;
0
```

## Extended Spec QA Results

The extended spec QA used a third isolated environment:

- Backend: `http://127.0.0.1:19083`
- Backend binary: `/tmp/aeqa_spec_20260524194706_13462/bin/server`
- CLI binary: `/tmp/aeqa_spec_20260524194706_13462/bin/ae-cli`
- Database: `aeqa_spec_20260524194706_13462`
- Temporary home: `/tmp/aeqa_spec_20260524194706_13462/home`
- Temporary global Git config: `/tmp/aeqa_spec_20260524194706_13462/gitconfig`
- Evidence files: `/tmp/aeqa_spec_20260524194706_13462/evidence/`

### Hook Lifecycle And Runtime Binary Resolution

Lifecycle checks were executed through real `ae-cli hooks ...` commands and real Git commits:

```text
RC_GLOBAL_REFUSE=1
RC_REPO_REFUSE=1
RC_DEFAULT_REFUSE=1
DEFAULT_MARKER_EXISTS=no
BETA_EFFECTIVE_AFTER=/tmp/aeqa_spec_20260524194706_13462/home/.ae-cli/git-hooks
GLOBAL_AFTER_REPO_DISABLE=/tmp/aeqa_spec_20260524194706_13462/home/.ae-cli/git-hooks
GLOBAL_AFTER_DISABLE=
```

Confirmed:

- `hooks enable --global` refused a non-AE global `core.hooksPath` without `--force`.
- `hooks enable --repo` refused a non-AE global `core.hooksPath` without `--force`.
- `hooks enable --repo --force` wrote a repo-local override and did not change global Git config.
- Executable default hooks were not chained after AE took ownership; the default marker file stayed absent after a real commit.
- `hooks disable --repo` removed repo-local AE hooks while leaving an AE global hook effective.
- `hooks disable --global` removed the global AE hook config.
- A stale managed template was reported as `Template: stale`, and hidden `hooks refresh-installations` rewrote it to `Template: current`.

Runtime binary resolution was also validated without `AE_CLI_BIN`. The managed global hook resolved `/tmp/aeqa_spec_20260524194706_13462/home/.local/bin/ae-cli`; a real commit uploaded checkpoint `6ead4f1700947f2c5b710c11160fec0c85010317` and increased repo `1` checkpoints from `1` to `2`.

The `command -v ae-cli` fallback was validated by temporarily moving the stable `~/.local/bin/ae-cli` aside and putting a wrapper on `PATH`. A real hook commit called the wrapper and uploaded one checkpoint:

```text
commandv_repo_id=12 checkpoint_before=0 checkpoint_after=1 wrapper_lines=1
command-v-wrapper-called hook post-commit
```

Missing executable fallback was also fail-open. With no `AE_CLI_BIN`, no `~/.local/bin/ae-cli`, and no `ae-cli` on `PATH`, a real commit completed and checkpoint count stayed unchanged:

```text
missing_binary_repo_id=13 checkpoint_before=0 checkpoint_after=0
```

### Status Modes

`ae-cli hooks status --uploads` reported the exercised effective hook modes:

```text
Effective: none
Effective: git_default
Effective: non_ae_global
Effective: ae_global
Effective: ae_repo
Effective: non_ae_repo
```

This proves the current implementation can distinguish the major Git hook execution modes. The same evidence also shows a remaining status-reporting gap: the command does not print the context fingerprint, observed repo binding state, default-hook bypass/effective detail, or explicit installed/current template version numbers required by the spec.

### Refresh, Observed Repos, And Auth Boundary

`hooks refresh --current` updated the eligibility cache for the current repo but did not update `observed-repos.json`:

```text
refresh_current_counts before_obs=1 after_obs=1 before_cache=1 after_cache=2
```

The batch `hooks refresh` path did not modify cache state after inserting a mismatched observed repo entry:

```text
batch_refresh_repos_changed=no
```

A later real CLI run used a home where `observed-repos.json` and `repos.json` had been created by real `ae-cli init`. With no backend listening on port `19083`, `ae-cli hooks refresh` still exited `0` with empty stdout/stderr and left both files byte-identical:

```text
backend_listener_19083=
observed_before=1
cache_repos_before=1
rc=0
stdout=
stderr=
observed_after=1
cache_repos_after=1
observed_sha_before=1dd6b7bae25032afce1e82d57e5de5792349079c5a472e383d936480ceebe108
observed_sha_after=1dd6b7bae25032afce1e82d57e5de5792349079c5a472e383d936480ceebe108
cache_sha_before=d0a090a0264aa8d4305fc77ee89df5dc3efb23ac1a2a5cf73cf9e6d293fba084
cache_sha_after=d0a090a0264aa8d4305fc77ee89df5dc3efb23ac1a2a5cf73cf9e6d293fba084
```

Token safety was validated with real hook-triggered commits and backend DB checks:

```text
checkpoint_count_missing=0 sha=1cd74a917f56dced827095768fb5f6bb966356d3
checkpoint_count_malformed=0 sha=21fc2b2b342e670cebb7d7f1297e1814d9212216
checkpoint_count_expired_no_refresh=0
checkpoint_count_expired_bad_refresh=0
```

An expired token with a valid refresh token refreshed during CLI startup and uploaded checkpoint `0389c5eae767c0b22cb46c4420016399138b7a77`; this is not a failure because the token became usable through the real refresh flow.

When the CLI had an authenticated token that could not yield a stable local `auth_subject`, a real hook commit performed only immediate non-replayable work. The backend accepted one checkpoint, but no positive cache, negative cache, hook queue, upload ledger, or context-bound observed repo state was written:

```text
unstable_repo_id=20 checkpoint_before=0 checkpoint_after=1 cache_repos=missing cache_negative=missing queue_files=0 ledger_files=0 observed_binding=|;
```

### Historical State Isolation

The test created both historical user-level and repository-local state:

```text
old-root scan-state file under /tmp/aeqa_spec_20260524194706_13462/home/.ai-efficiency/attribution/workspaces/
/tmp/aeqa_spec_20260524194706_13462/repos/spec-old/.ae/session.json
```

A real hook commit in `spec-old` uploaded under backend repo `7` and the derived workspace `90693568-87b1-5923-9c94-7d9bc44a69e0`, while the stale files remained untouched:

```text
checkpoint_count_old=1 sha=ba3ccccae9a9b965ac096e4d599ac05634e47dd5
old_root_file_still_present=yes
repo_ae_session_still_present=yes
```

### Tool Usage Spool And Managed Uploads

A real `ae-cli sync` in `spec-sync` was run while the backend was offline. It completed fail-open and wrote workspace-scoped spool state:

```text
sync_workspace=0caacab0-1022-54dc-8d7d-05528ab4b272
spool_exists_after_offline=yes
scan_state_exists_after_offline=yes
```

The spooled event carried the required binding fields:

```text
ServerURL=http://127.0.0.1:19083
AuthSubject=user:1
RepoConfigID=5
RepoKey=repo-host.example.com/org/spec-sync
WorkspaceID=0caacab0-1022-54dc-8d7d-05528ab4b272
```

After backend recovery, a second real `ae-cli sync` replayed the event and removed the spool:

```text
tool_usage_before_sync=0
tool_usage_after_replay=1
spool_exists_after_replay=no
```

Backend ingest stored only normalized fields:

```text
repo_config_id=5 tool=codex session=codex-sync-real-1 input=31 output=7 cache=6 reasoning=3 has_raw_path=false has_raw_locator=false has_raw_payload=false
```

The context-mismatch spool path exposed a spec mismatch. The test logged in against a real alternate backend context at `http://127.0.0.1:19084`, stopped that backend, and ran a real `ae-cli sync`. That offline sync wrote a real spooled event with alternate-server binding:

```text
spool_exists_after_alt_offline=yes
ServerURL=http://127.0.0.1:19084
RepoConfigID=5
RepoKey=repo-host.example.com/org/spec-sync
ToolSessionID=codex-sync-context-switch
ToolEventID=resp-context-switch
```

After switching the token context back to `http://127.0.0.1:19083`, a real `ae-cli sync` did not upload the alternate-server spooled event, cleared the spool, and still wrote no ledger row:

```text
tool_usage_before_alt_offline=1
tool_usage_after_original_sync=1
spool_exists_after_original_sync=no
ledger_lines_after_original_sync=0
```

### Post-Rewrite Queue And Replay

The backend was stopped and a real `git commit --amend` was run in `spec-rewrite`. The hook queued both the amended checkpoint and the rewrite event:

```text
rewrite_old=0bfe9d495afbc41a36567255b8be90ab04ea06d3
rewrite_new=8750795b9cfc2d4abdb79be46bbc6cf4bf3e7089
queue_exists_after_offline=yes
```

After backend recovery, a real follow-up commit replayed the queue before uploading the current checkpoint:

```text
rewrites_before=0
rewrites_after=1
checkpoints_before=0
checkpoints_after=2
queue_exists_after_replay=no
```

The upload ledger recorded `uploaded` rows for both the replayed checkpoint and the rewrite. Backend `commit_rewrites` stored the amend pair for repo `6`.

### Upload Cache From Real CLI-Created State

The upload-cache replay path was rechecked without prewriting cache, queue, or ledger files. A real `ae-cli init --hooks none` in `spec-upload-cache-real2` created the backend repo, observed repo record, and positive eligibility cache:

```text
repo_config_id=22 workspace=c52259cd-0f6d-5e0f-96df-d3bd910ee3e3 checkpoints_before=0
positive_cache_entry=http://127.0.0.1:19083	user:1	repo-host.example.com/org/spec-upload-cache-real2	22	true
```

The backend was stopped and two real Git commits completed through the global hook. They created a workspace-scoped `hooks.jsonl` with two checkpoint uploads and wrote one failed ledger row after the second commit retried the first queued item:

```text
queue_lines_after_offline=2 ledger_lines_after_offline=1
http://127.0.0.1:19083	user:1	22	repo-host.example.com/org/spec-upload-cache-real2	c52259cd-0f6d-5e0f-96df-d3bd910ee3e3	4afe0382bb1f303ac7f0d2dfb31af09a39920a28
http://127.0.0.1:19083	user:1	22	repo-host.example.com/org/spec-upload-cache-real2	c52259cd-0f6d-5e0f-96df-d3bd910ee3e3	57f883fb46a332bd812769d444f0136bad4bf025
failed	http://127.0.0.1:19083	user:1	22	repo-host.example.com/org/spec-upload-cache-real2	c52259cd-0f6d-5e0f-96df-d3bd910ee3e3	sending checkpoint request: Post "http://127.0.0.1:19083/api/v1/checkpoints/commit": dial tcp 127.0.0.1:19083: connect: connection refused
```

After backend restart, a third real Git commit replayed both queued checkpoints, uploaded the current checkpoint, cleared the queue, and left three backend checkpoint rows for the same repo/user/workspace:

```text
checkpoints_after=3 queue_exists_after_replay=no ledger_lines_after_replay=3
uploaded	http://127.0.0.1:19083	user:1	22	repo-host.example.com/org/spec-upload-cache-real2	c52259cd-0f6d-5e0f-96df-d3bd910ee3e3
uploaded	http://127.0.0.1:19083	user:1	22	repo-host.example.com/org/spec-upload-cache-real2	c52259cd-0f6d-5e0f-96df-d3bd910ee3e3
4afe0382bb1f303ac7f0d2dfb31af09a39920a28|22|1|c52259cd-0f6d-5e0f-96df-d3bd910ee3e3
57f883fb46a332bd812769d444f0136bad4bf025|22|1|c52259cd-0f6d-5e0f-96df-d3bd910ee3e3
4344ae17676af4a6021c2be754d0f55492468c59|22|1|c52259cd-0f6d-5e0f-96df-d3bd910ee3e3
```

### Backend Eligibility And Cache TTL

Backend eligibility was checked with real API calls. `webhook_failed` is eligible, and the three common remote URL forms resolve to the same `repo_key`:

```text
eligible=true repo_config_id=9 repo_key=repo-host.example.com/org/spec-webhook-failed status=webhook_failed reason=
resolve_remote=git@repo-host.example.com:org/spec-webhook-failed.git
eligible=true repo_config_id=9 repo_key=repo-host.example.com/org/spec-webhook-failed status=webhook_failed reason=
resolve_remote=ssh://git@repo-host.example.com/org/spec-webhook-failed.git
eligible=true repo_config_id=9 repo_key=repo-host.example.com/org/spec-webhook-failed status=webhook_failed reason=
```

Backend ingest with explicit `repo_config_id` rejected inactive or missing repos without creating backend rows:

```text
inactive_checkpoint_http=422 repo_count_before=10 repo_count_after=10 inactive_checkpoints=0
missing_toolusage_http=422 missing_toolusage_rows=0
```

Negative cache TTL was validated with real elapsed time. An unknown repo commit created a `not_found` cache entry. Creating the backend repo immediately afterward did not authorize upload until the 5 minute TTL expired:

```text
negative_cache_entries_after_unknown=1 repo_count=0 checkpoints=0
immediate_after_backend_repo repo_id=15 before=0 after=0
sleep_start=2026-05-24T13:32:02Z duration_seconds=315
after_negative_ttl_expired before=0 after=1
```

The backend batch endpoint returned only requested repos and one requested missing repo; it did not enumerate an unrequested backend repo. Tool-usage ingest with explicit `repo_config_id` also bound rows to the requested repo ids even when legacy workspace scope conflicted:

```text
hook_eligible_requested_count=1 ineligible_count=1 contains_unrequested=false
scope_rows=scope-a-session:18:1
scope-b-session:19:1
```

### Resolve Timeout

A real local HTTP server delayed `POST /api/v1/repos/resolve-remote` by 2 seconds. The hook commit returned fail-open in under 1 second, did not create a repo, and did not upload checkpoints:

```text
timeout_commit_duration_ms=926 slow_server_lines=1 repo_count_before=0 repo_count_after=0 checkpoints=0
2026-05-24T13:37:14.028Z POST /api/v1/repos/resolve-remote auth=yes body={"remote_url":"https://repo-host.example.com/org/spec-timeout-rerun.git","branch":"main","client_cache_version":"repo-eligibility-v1"}
```

### Public Release Installer And Upgrade

The public release installer path was checked with real GitHub release assets in an isolated home. Release metadata confirmed that both `v0.1.0-preview.10` and latest `v0.1.0-preview.11` publish Darwin arm64 `ae-cli` archives plus `checksums.txt`:

```text
{"tagName":"v0.1.0-preview.10","isPrerelease":true,"publishedAt":"2026-05-22T04:11:23Z"}
{"tagName":"v0.1.0-preview.11","isPrerelease":false,"publishedAt":"2026-05-22T09:27:28Z"}
```

The first direct GitHub release-asset download hung on `checksums.txt`; an independent `curl -I -L --max-time 15` to the same asset timed out. Retrying through the machine HTTP proxy reached GitHub and the redirected release asset with HTTP 200, so the installer run below still used real public release downloads rather than local archives.

Installing the older public release succeeded as a binary install and wrote config, but the post-install hook refresh failed because that published binary does not know the `hooks` command:

```text
Installing ae-cli v0.1.0-preview.10...
Wrote CLI config to /tmp/aeqa_public_installer_proxy_20260524221832_65763/home/.ae-cli/config.yaml
Warning: installed ae-cli but failed to refresh managed hook scripts.
Installed ae-cli v0.1.0-preview.10 to /tmp/aeqa_public_installer_proxy_20260524221832_65763/home/.local/bin/ae-cli
old_version=ae-cli v0.1.0-preview.10
old_refresh_result=Error: unknown command "hooks" for "ae-cli" Run 'ae-cli --help' for usage. unknown command "hooks" for "ae-cli"
```

Upgrading through the same real public installer path to latest `v0.1.0-preview.11` also installed the binary, but the latest published binary still lacks `hooks`, so installer hook refresh still failed and no managed hook scripts were created:

```text
Installing ae-cli v0.1.0-preview.11...
Updated CLI config at /tmp/aeqa_public_installer_proxy_20260524221832_65763/home/.ae-cli/config.yaml
Warning: installed ae-cli but failed to refresh managed hook scripts.
Installed ae-cli v0.1.0-preview.11 to /tmp/aeqa_public_installer_proxy_20260524221832_65763/home/.local/bin/ae-cli
ae-cli v0.1.0-preview.11
Error: unknown command "hooks" for "ae-cli"
unknown command "hooks" for "ae-cli"
```

This means the current branch implementation has installer refresh support, but the current public release surface does not yet prove the upgrade contract. A release containing the `hooks` command is required before this can pass as public installer QA.

### Linked Worktree Semantics

The test created a real linked worktree for `spec-worktree` and enabled repo-local hooks in both worktrees with `extensions.worktreeConfig=true`.

The installation registry kept separate worktree-scoped records:

```text
git_dir=/private/tmp/aeqa_spec_20260524194706_13462/repos/spec-worktree/.git
git_dir=/private/tmp/aeqa_spec_20260524194706_13462/repos/spec-worktree/.git/worktrees/spec-worktree-linked
config_scope=worktree
hooks_path=/private/tmp/aeqa_spec_20260524194706_13462/repos/spec-worktree/.git/ae-hooks
```

The two worktrees derived different workspace IDs:

```text
main_workspace=bc51b301-c6d3-5c65-8f2c-3b65e23f89fc
linked_workspace=9ccb4024-f609-5df8-951f-b68192a2af97
workspaces_differ=yes
```

Real hook-triggered commits in both worktrees created two backend checkpoints for repo `4`:

```text
main_commit=03836b96f59e39c3694e0207e958a6be3a12448b
linked_commit=7cc2494bf065bc4693d71e3092df5f69030a9542
checkpoints_before=0
checkpoints_after=2
```

### Repo-Local Disable Precedence

The spec requires `hooks disable --repo` to keep unsetting AE-managed repo-local layers if unsetting the effective worktree layer exposes a lower-precedence local AE hook. Real linked-worktree QA shows the current command stops too early:

```text
before_disable_link_status
Effective:     ae_repo
after_disable_link_status
Effective:     ae_repo
disable_precedence worktree_after= local_after=/private/tmp/aeqa_spec_20260524194706_13462/repos/spec-disable-precedence/.git/ae-hooks main_effective=/private/tmp/aeqa_spec_20260524194706_13462/repos/spec-disable-precedence/.git/ae-hooks link_effective=/private/tmp/aeqa_spec_20260524194706_13462/repos/spec-disable-precedence/.git/ae-hooks
```

The command correctly unset the worktree value, but the local AE hook remained effective for the linked worktree.

### Command, Registry, And Refresh Edge QA

A final isolated command-surface and registry QA pass used the current worktree binary at `/tmp/ae-cli-realqa` through an isolated install path:

```text
qa_dir=/tmp/aeqa_uncovered3_20260524225103_22802
ae_version=ae-cli v0.1.0
home=/tmp/aeqa_uncovered3_20260524225103_22802/home
git_config_global=/tmp/aeqa_uncovered3_20260524225103_22802/gitconfig
```

`ae-cli doctor` includes hook status and an online repository eligibility diagnostic. In this run the backend URL intentionally pointed at an unused local port so the command could prove diagnostic behavior without uploading data:

```text
Sessionless attribution doctor
  Repo:          /private/tmp/aeqa_uncovered3_20260524225103_22802/repos/doctor-repo
  Workspace ID:  1f306109-5078-5de2-886c-78470bf9b6fa
  Git Dir:       /private/tmp/aeqa_uncovered3_20260524225103_22802/repos/doctor-repo/.git
  Git Common:    /private/tmp/aeqa_uncovered3_20260524225103_22802/repos/doctor-repo/.git
  State Dir:     /tmp/aeqa_uncovered3_20260524225103_22802/home/.ae-cli/state/attribution
  Logged In:     true
  State Exists:  false
Hook status
  Global:        disabled
  Repo-local:    enabled
  Effective:     ae_repo
  Binary:        /tmp/aeqa_uncovered3_20260524225103_22802/home/.local/bin/ae-cli
  AE_CLI_BIN:    unset
  Template:      current
  Eligibility:   missing
Repo Eligibility: unavailable (send request: Post "http://127.0.0.1:9/api/v1/repos/resolve-remote": dial tcp 127.0.0.1:9: connect: connection refused)
```

The same run showed that `ae-cli sync status` is not implemented as a real status command. Help for `sync status` still prints the plain `ae-cli sync` usage, and executing it attempts the normal sync path:

```text
$ ae-cli help sync status
Usage:
  ae-cli sync [flags]

$ ae-cli sync status
Error: repository is not registered or reporting-enabled; run 'ae-cli init' or ask an admin to configure it
rc=1
```

Repo-local disable and installer refresh registry behavior exposed additional spec mismatches. After a real `hooks enable --repo --force` followed by a real `hooks disable --repo`, the matching installation record remained enabled. Removing the scripts and running `hooks refresh-installations` recreated the disabled repo-local hooks:

```text
disabled_repo_hooks_path=/private/tmp/aeqa_uncovered3_20260524225103_22802/repos/disabled-repo/.git/ae-hooks
disabled_repo_enabled_records_after_disable=1
disabled_repo_disabled_records_after_disable=0
disabled_repo_post_commit_recreated_after_refresh=yes
```

Deleting a repository after a real repo-local install also did not cause refresh to skip the inaccessible recorded location. Refresh recreated the recorded hook path and printed it as refreshed:

```text
deleted_repo_hooks_path=/private/tmp/aeqa_uncovered3_20260524225103_22802/repos/deleted-repo/.git/ae-hooks
deleted_repo_hook_path_recreated=yes
deleted_repo_refresh_mentions_path=yes
```

Global installation reconciliation matched the spec in both directions. If global Git config still pointed at the canonical AE path, refresh reactivated the global record and rewrote the scripts; after global Git config was unset, refresh did not reactivate it:

```text
disabled_global_enabled_records_after_disable=0
disabled_global_reactivated_post_commit=yes
disabled_global_enabled_records_after_reconfig=1
disabled_global_unset_post_commit_recreated=no
disabled_global_enabled_records_after_unset=0
```

The same run covered linked worktree local-scope registry identity. Two linked worktrees without `extensions.worktreeConfig=true` shared one local-scoped registry record keyed by `git_common_dir`, but disabling from the linked worktree printed no warning that local config is shared:

```text
local_scope_common_dir=/private/tmp/aeqa_uncovered3_20260524225103_22802/repos/local-scope/.git
local_scope_hooks_path=/private/tmp/aeqa_uncovered3_20260524225103_22802/repos/local-scope/.git/ae-hooks
local_scope_linked_hooks_path=/private/tmp/aeqa_uncovered3_20260524225103_22802/repos/local-scope/.git/ae-hooks
local_scope_record_count=1
local_scope_git_dirs=/private/tmp/aeqa_uncovered3_20260524225103_22802/repos/local-scope/.git/worktrees/local-scope-linked
local_scope_main_effective_after_disable=
local_scope_linked_effective_after_disable=
local_scope_disable_stdout_contains_shared_warning=no
```

If the repo-local Git config was already externally unset, a real `hooks disable --repo` did not reconcile the still-enabled registry record:

```text
already_absent_hooks_path=/private/tmp/aeqa_uncovered3_20260524225103_22802/repos/already-absent/.git/ae-hooks
already_absent_enabled_records_after_disable=1
already_absent_disabled_records_after_disable=0
```

`hooks status` did not scan an arbitrary historical AE-looking hook directory, which matches the spec boundary. It still omitted default-hook detail even though an executable default `post-commit` existed:

```text
Effective:     git_default
arbitrary_old_hook_path=/tmp/aeqa_uncovered3_20260524225103_22802/repos/status-scan/.git/old-ae-hooks
status_mentions_arbitrary_old_hook=no
status_mentions_default_hook_detail=no
```

## Backend Data Evidence

After the active commit and amend:

```text
repo_configs:
1 repo-host.example.com/org/repo active
2 repo-host.example.com/org/inactive inactive

commit_checkpoints:
1 repo_config_id=1 user_id=1 commit=23a7e1480d1ae28f8152bfeb9b99bc656e0a6d0f workspace=ba08306e-5649-51bd-a748-3a9d3765d1b2
2 repo_config_id=1 user_id=1 commit=eabc453f4eaaaae8704d0f7c1c5f049930e06103 workspace=ba08306e-5649-51bd-a748-3a9d3765d1b2

commit_rewrites:
1 repo_config_id=1 user_id=1 old=23a7e1480d1ae28f8152bfeb9b99bc656e0a6d0f new=eabc453f4eaaaae8704d0f7c1c5f049930e06103 type=amend

tool_usage_events:
1 repo_config_id=1 user_id=1 tool=codex session=qa-codex-active input=101 output=23 cache=7 reasoning=5 commit_checkpoint_id=1 has_raw_path=false has_raw_payload=false
```

Backend Events API evidence:

- `GET /api/v1/events/summary`: `total_events=1`, `bound_events=1`, `unbound_events=0`, `tool_counts=[codex:1]`.
- `GET /api/v1/events`: returned event id `1`, repo `org/repo`, binding `bound`, commit `23a7e148...`, input `101`, output `23`, cache `7`.
- `GET /api/v1/events/1`: returned workspace `ba08306e-5649-51bd-a748-3a9d3765d1b2`, checkpoint id `1`, full commit SHA, and no raw source path or raw payload fields.

## Frontend Results

Default dev topology passed:

- Backend: `http://localhost:8081`
- Frontend: `http://localhost:5173`
- Login: real `Dev Login (Admin)` click returned `200` through Vite `/api` proxy.
- Events page showed:
  - `TOTAL EVENTS = 1`
  - `BOUND TO COMMIT = 1`
  - `UNBOUND = 0`
  - table row `codex / qa-codex-active / org/repo / bound / 23a7e148 / 101 / 23 / 7 / admin`
- Event detail drawer showed:
  - workspace `ba08306e-5649-51bd-a748-3a9d3765d1b2`
  - tool session `qa-codex-active`
  - tool event `qa-response-1`
  - dedupe key `codex-jsonl:qa-codex-active:qa-response-1`
  - checkpoint id `1`
  - full commit `23a7e1480d1ae28f8152bfeb9b99bc656e0a6d0f`
  - Source Path and Locator as empty display placeholders.

Playwright request evidence:

```text
POST http://localhost:5173/api/v1/auth/dev-login => 200
GET  http://localhost:5173/api/v1/auth/me => 200
GET  http://localhost:5173/api/v1/events/summary?... => 200
GET  http://localhost:5173/api/v1/events?... => 200
GET  http://localhost:5173/api/v1/events/1 => 200
```

## Issues Found

### QA-1: Backend CORS ignores the configured frontend URL for non-default dev ports

**Severity:** Medium for local QA and custom frontend deployments.

Reproduction:

1. Start backend at `http://127.0.0.1:19081` with `AE_SERVER_FRONTEND_URL=http://127.0.0.1:15173`.
2. Start frontend at `http://127.0.0.1:15173` with `VITE_API_URL=http://127.0.0.1:19081/api/v1`.
3. Click `Dev Login (Admin)`.

Observed:

- Browser console: request to `http://127.0.0.1:19081/api/v1/auth/dev-login` was blocked by CORS because no `Access-Control-Allow-Origin` header was present.
- Direct preflight and POST with `Origin: http://127.0.0.1:15173` returned `403 Forbidden`.

Likely cause:

- `backend/internal/middleware/cors.go` defaults to `http://localhost:5173` and `http://localhost:8081`.
- `backend/cmd/server/main.go` calls `middleware.CORS(nil)` and does not pass `cfg.Server.FrontendURL`.

Expected:

- The configured frontend URL should be allowed, or the supported dev topology should be explicitly limited to `localhost:5173`.

### QA-2: Repo-local enable refuses under AE global because default AE scripts are executable

**Severity:** Medium for the hook lifecycle contract.

Spec expectation:

- `hooks enable --repo` with an existing AE-managed global `core.hooksPath` should succeed without `--force`, write or refresh the repo-local override, record the repo-local installation, and leave global Git config unchanged.

Observed:

```text
RC_BETA_ENABLE_WITH_AE_GLOBAL=1
Error: executable default hooks exist (post-commit, post-rewrite); use --force to overwrite
BETA_EFFECTIVE_AFTER=/tmp/aeqa_spec_20260524194706_13462/home/.ae-cli/git-hooks
```

Likely cause:

- `EnableRepo` checks executable hooks under `git rev-parse --git-path hooks` even when the effective hook path is already AE-managed global. Under a global `core.hooksPath`, `git rev-parse --git-path hooks` resolves into the global AE hook directory, so the AE-managed scripts are mistaken for non-AE default hook behavior.

Expected:

- If the current effective hook path is AE-managed, repo-local enable should be authorized without treating those AE scripts as replace-protected default hooks.

### QA-3: `hooks refresh --current` does not update observed repo identity

**Severity:** Medium for refresh and diagnostics.

Spec expectation:

- `hooks refresh --current` should refresh the current repository and attach the current context binding to the durable observed repo identity when a stable `auth_subject` exists.

Observed:

```text
refresh_current_counts before_obs=1 after_obs=1 before_cache=1 after_cache=2
```

The eligibility cache gained the current repo, but `observed-repos.json` stayed at one entry.

Expected:

- The observed repo count or matching observed record should update for the current repo, with `server_url`, `auth_subject`, and `repo_key`.

### QA-4: Batch `hooks refresh` is a no-op

**Severity:** Medium for larger installations and cache recovery.

Spec expectation:

- Batch `hooks refresh` should send only locally observed repos whose `server_url` and `auth_subject` match the current stable context to `POST /api/v1/repos/hook-eligible`, then update positive and negative eligibility cache entries while preserving unrelated entries.

Observed:

```text
batch_refresh_repos_changed=no
rc=0 stdout= stderr=
observed_sha_before=1dd6b7bae25032afce1e82d57e5de5792349079c5a472e383d936480ceebe108
observed_sha_after=1dd6b7bae25032afce1e82d57e5de5792349079c5a472e383d936480ceebe108
cache_sha_before=d0a090a0264aa8d4305fc77ee89df5dc3efb23ac1a2a5cf73cf9e6d293fba084
cache_sha_after=d0a090a0264aa8d4305fc77ee89df5dc3efb23ac1a2a5cf73cf9e6d293fba084
```

This matches current code behavior: `RefreshObserved` returns `nil` without calling the backend.

Expected:

- Matching observed repo entries should be sent to the backend batch endpoint, mismatched entries should be ignored, and matching results should update the cache.

### QA-5: Tool-usage spool context mismatch is dropped without ledger diagnostics

**Severity:** Medium for replay observability.

Spec expectation:

- Hook queue and tool-usage spool replay should skip items whose binding fields do not match the current stable context and record a `skipped` upload-ledger entry. Hook queue replay already does this.

Observed:

```text
tool_usage_before_alt_offline=1
tool_usage_after_original_sync=1
spool_exists_after_original_sync=no
ledger_lines_after_original_sync=0
```

The alternate-server spooled item was not uploaded under the original server context and the spool was cleared, but no upload-ledger record was written.

Expected:

- The spool replay path should append a `skipped` ledger record with the current backend, account, repo, workspace, and `last_error=context mismatch`.

### QA-6: `hooks status` omits several spec diagnostics

**Severity:** Low to medium for operator support.

Spec expectation:

- Status should include the current backend/account context fingerprint, whether the current repo has a context-bound observed identity or only an unbound hint, default hook effective/bypassed detail, and installed/current template version information.

Observed:

- The command reported `Global`, `Repo-local`, `Effective`, `Binary`, `AE_CLI_BIN`, `Template`, `Eligibility`, and optional upload groups.
- It did not print the context fingerprint, observed repo binding state, default-hook bypass/effective detail, or installed/current template version numbers.

Expected:

- Keep the current concise fields, but add the missing diagnostics so status can explain cache, observed-repo, and template decisions without requiring manual JSON inspection.

### QA-7: `hooks disable --repo` leaves lower-precedence AE repo-local hook effective

**Severity:** Medium for linked worktrees and users trying to disable repo-local AE hooks.

Spec expectation:

- If disabling an effective worktree-level AE hook exposes a lower-precedence local AE hook, `hooks disable --repo` should continue until AE repo-local hooks are no longer effective for that worktree.

Observed:

```text
after_disable_link_status
Effective:     ae_repo
disable_precedence worktree_after= local_after=/private/tmp/aeqa_spec_20260524194706_13462/repos/spec-disable-precedence/.git/ae-hooks link_effective=/private/tmp/aeqa_spec_20260524194706_13462/repos/spec-disable-precedence/.git/ae-hooks
```

Expected:

- The command should unset each effective AE-managed repo-local layer exposed during the disable operation, then report the newly effective non-repo-local behavior.

### QA-8: Current public ae-cli release cannot refresh managed hooks after install

**Severity:** Medium for released CLI users upgrading from the public installer.

Spec expectation:

- The official `ae-cli` installer or upgrade flow should rewrite AE-managed hook scripts to the latest template after the new binary is installed.

Observed:

```text
Installed ae-cli v0.1.0-preview.11 to /tmp/aeqa_public_installer_proxy_20260524221832_65763/home/.local/bin/ae-cli
Warning: installed ae-cli but failed to refresh managed hook scripts.
ae-cli v0.1.0-preview.11
Error: unknown command "hooks" for "ae-cli"
unknown command "hooks" for "ae-cli"
```

Expected:

- A public release that includes the current `hooks refresh-installations` command should install, refresh the active AE-managed global hook path, and leave executable `post-commit` and `post-rewrite` scripts in `~/.ae-cli/git-hooks`.

### QA-9: Repo-local disable leaves the installation record enabled

**Severity:** Medium for users who disable repo-local hooks before upgrading.

Spec expectation:

- `hooks disable --repo` marks matching repo-local installation records disabled only after the matching Git config value is unset or already absent. Installer and upgrade refresh should rewrite enabled repo-local records only.

Observed:

```text
disabled_repo_hooks_path=/private/tmp/aeqa_uncovered3_20260524225103_22802/repos/disabled-repo/.git/ae-hooks
disabled_repo_enabled_records_after_disable=1
disabled_repo_disabled_records_after_disable=0
disabled_repo_post_commit_recreated_after_refresh=yes
```

Expected:

- The disabled repo-local record should have `enabled=false`, and `hooks refresh-installations` should not recreate scripts for it.

### QA-10: Refresh recreates repo-local hook paths for deleted repositories

**Severity:** Medium for upgrade safety and diagnostics.

Spec expectation:

- Missing or inaccessible recorded repo-local locations should be skipped with diagnostics, and upgrade should not fail because a repository was deleted or moved.

Observed:

```text
deleted_repo_hooks_path=/private/tmp/aeqa_uncovered3_20260524225103_22802/repos/deleted-repo/.git/ae-hooks
deleted_repo_hook_path_recreated=yes
deleted_repo_refresh_mentions_path=yes
```

Expected:

- Refresh should detect that the recorded repo-local location is no longer accessible as an installed Git hook location, skip it, and print a diagnostic instead of recreating the deleted repository path.

### QA-11: `ae-cli sync status` is not implemented

**Severity:** Low to medium for upload replay observability.

Spec expectation:

- `ae-cli hooks status --uploads` or `ae-cli sync status` can summarize upload ledgers by workspace and repo so a user can find the replay starting point quickly.

Observed:

```text
$ ae-cli help sync status
Usage:
  ae-cli sync [flags]

$ ae-cli sync status
Error: repository is not registered or reporting-enabled; run 'ae-cli init' or ask an admin to configure it
rc=1
```

Expected:

- `ae-cli sync status` should be a real status subcommand, or the spec should state that upload status is only exposed through `ae-cli hooks status --uploads`.

### QA-12: Local-scope repo disable does not warn about shared linked-worktree impact

**Severity:** Low to medium for linked worktree operator clarity.

Spec expectation:

- If the disabled effective layer was `config_scope=local`, the command should print that local Git config is shared by linked worktrees for the same common directory, so disabling it may affect those worktrees too.

Observed:

```text
local_scope_record_count=1
local_scope_main_effective_after_disable=
local_scope_linked_effective_after_disable=
local_scope_disable_stdout_contains_shared_warning=no
```

Expected:

- The command should print a shared-common-directory warning when disabling a local-scoped repo hook.

### QA-13: Already-absent repo-local Git config does not reconcile enabled registry records

**Severity:** Medium for registry accuracy.

Spec expectation:

- If no effective AE-managed repo-local hook path is found in local or worktree config, but the registry has an enabled repo-local record whose recorded config scope no longer has a `core.hooksPath` value, `hooks disable --repo` should mark that record disabled and report that the repository hook was already inactive.

Observed:

```text
already_absent_hooks_path=/private/tmp/aeqa_uncovered3_20260524225103_22802/repos/already-absent/.git/ae-hooks
already_absent_enabled_records_after_disable=1
already_absent_disabled_records_after_disable=0
```

Expected:

- The enabled record should be reconciled to disabled when Git config no longer points at the managed hook path.

## Not Covered By Real QA

- Positive eligibility TTL expiry is specified as 24 hours. A real-time wait is now active from a real CLI-created positive cache expiring at `2026-05-25T19:13:09.439365+08:00`; this case is not covered as passing until the after-expiry real Git commit runs and verifies that the expired cache no longer authorizes hook work.
- Batch `ae-cli hooks refresh` is fixed in code and covered by unit tests in this branch, but it is not yet covered as passing real end-to-end QA after the fix.
- Public release upgrade is covered as a real failing release-surface QA item, not as a passing behavior; QA-8 tracks that the current latest published binary lacks `hooks refresh-installations`.
- Repo-local disable, disabled-record reconciliation, already-absent reconciliation, lower-precedence worktree cleanup, and local-scope warning are fixed in code and covered by unit tests in this branch, but they are not yet covered as passing real end-to-end QA after the fix.
- Repo-local upgrade refresh for missing or inaccessible locations is fixed in code and covered by unit tests in this branch, but it is not yet covered as passing real installer/upgrade QA after the fix.

## Test Notes

- An early QA command ran `ae-cli init --hooks none` from the wrong working directory and explicitly registered `org/missing` as repo id `3`. This was a QA script mistake, not a hook-path product behavior. The `sync` no-create check was rerun against a fresh `org/no-sync` remote and passed with repo count `0 -> 0`.
- During extended spec QA, DB counting inside an isolated `HOME` initially failed because Docker CLI could not find the user's Colima Docker context. The product commands and Git hooks had already run; DB evidence was then collected with explicit `DOCKER_HOST=unix:///Users/admin/.colima/default/docker.sock`.
- An expired-token hook commit with a valid refresh token uploaded successfully after real token refresh. Separate expired-token cases without refresh and with a bad refresh token were then run and did not upload from the positive cache.
- `npm install` reported `7 vulnerabilities (3 moderate, 4 high)`. This was not investigated because it is dependency hygiene rather than the reporting path under test.
- The first backend attempt started with shell backgrounding and exited after readiness because the tool session reclaimed the child process. Persistent exec sessions were used for real backend/frontend validation.
- `87-real-upload-cache-from-cli-qa.txt` contains a first script-pipe abort note before the continued run. The product actions used for evidence after that note are real `ae-cli init`, real Git commits, real backend stop/restart, real queue/ledger files, and real database rows.
- Public release installer QA first confirmed direct release-asset download timeout, then used the machine HTTP proxy to complete real GitHub release downloads and checksum-verified installs.
- An earlier command-registry QA attempt did not consistently run `ae-cli` from the temporary repositories and was discarded. The command-registry evidence in this report comes from `/tmp/aeqa_uncovered3_20260524225103_22802`, where each repository-sensitive command was run from its own temporary Git repository.

## Conclusion

The reporting core logic is normal in the real QA run:

- eligible backend-known repo commits upload checkpoints;
- managed Codex usage uploads include the correct repo/user/workspace dimensions;
- usage binds to the commit checkpoint;
- unknown and inactive repos do not upload from hook-triggered commits;
- frontend Events reads and displays the backend-ingested data;
- local hook queues and tool-usage spools replay under the correct backend/account/repo/workspace binding;
- upload-cache replay from CLI-created state works without prewriting queue, spool, cache, or ledger files;
- linked worktrees are separated by deterministic workspace identity;
- the managed hook dispatcher follows the stable `~/.local/bin/ae-cli` runtime binary path when `AE_CLI_BIN` is unset.

The main implementation gaps found by real spec QA were lifecycle/status/refresh polish, repo-local disable and registry reconciliation, deleted repo-local refresh handling, missing `sync status`, release-surface installer refresh, and spool-ledger observability, plus CORS configuration for custom local frontend ports. This branch now contains code fixes and unit coverage for every local-code gap except the public release-surface installer result, which still requires a new release containing the `hooks` command. Positive cache expiry over the full 24 hour TTL remains outside this real-time QA run.
