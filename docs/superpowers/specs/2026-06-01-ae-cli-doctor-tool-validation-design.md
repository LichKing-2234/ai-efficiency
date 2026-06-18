# ae-cli Doctor Tool Validation Design

**Date:** 2026-06-01
**Status:** Updated implementation contract
**Scope:** `ae-cli/`, `docs/`
**Related:**
- [2026-05-19-ae-cli-deterministic-tool-configuration-design.md](./2026-05-19-ae-cli-deterministic-tool-configuration-design.md)
- [2026-05-23-global-git-hooks-design.md](./2026-05-23-global-git-hooks-design.md)
- [2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md](./2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md)
- [docs/architecture.md](../../architecture.md)

The current project-level architecture remains documented in [`docs/architecture.md`](../../architecture.md). This document defines the active `ae-cli doctor` diagnostic contract for this implementation branch.

## Spec Relationship

- The deterministic tool configuration spec remains authoritative for what `ae-cli discover` writes.
- This design adds verification of that configured state to `ae-cli doctor`.
- It does not introduce the historical LLM-driven `/api/v1/tools/discover` flow.
- It does not use backend `/api/v1/user/providers/:id/test` for local tool readiness. Doctor must validate the same local tool commands a user will run.

## Problem

`ae-cli doctor` currently verifies sessionless attribution readiness, hook state, sync backlog, and repository eligibility. It does not verify whether `ae-cli discover` actually left Codex, Claude, or Gemini usable on the current machine.

That creates two blind spots:

1. A user can pass repo attribution checks while Codex, Claude, or Gemini is not installed, misconfigured, or unable to return a model response through the configured local CLI.
2. The online repo eligibility check reuses the hook-time bounded timeout. That timeout is appropriate for fail-open Git hooks, but it is too short for a human-run diagnostic command and can report `context deadline exceeded` even when the backend would return a valid eligible result in a few seconds.

## Goals

1. Make `ae-cli doctor` the trusted post-setup diagnostic for both repository attribution and local AI tool readiness.
2. Verify the actual local tool commands after `ae-cli discover`, not only backend provider connectivity.
3. Keep diagnostics safe: never print API keys, never require interactive prompts, and bound every external command with a timeout.
4. Keep hook execution fast by separating doctor diagnostics timeouts from hook-time eligibility timeouts.
5. Reuse `ae-cli discover` provider and platform credential selection so local config is compared against the current user-facing credential contract.
6. Produce output that distinguishes install, config, credential, command execution, timeout, and backend eligibility failures.

## Non-Goals

1. Do not add a `--live-tools` flag. Real local CLI probing is optional and is triggered by the explicit `--probe-tools` flag.
2. Do not call `/api/v1/user/providers/:id/test` for doctor tool readiness.
3. Do not rewrite local tool configuration from doctor. Configuration writes remain owned by `ae-cli discover`.
4. Do not print credential values, API keys, tokens, or full config snippets containing secrets.
5. Do not require exact model wording beyond confirming the tool command completed and returned non-empty output.
6. Do not make hook paths wait on these tool probes.

## Command Contract

`ae-cli doctor` keeps its existing sections and adds:

```text
Tool configuration
Tool probe
Recent Codex Failures
```

The default command validates local configuration and repository readiness without executing Codex, Claude, or Gemini:

```bash
ae-cli doctor
```

Default output must make the skipped live probe explicit:

```text
Tool probe: skipped [warn] (use --probe-tools to run local CLI probes)
```

Real local CLI probes run only when the user opts in:

```bash
ae-cli doctor --probe-tools
```

First-phase failed-request diagnostics are intentionally Codex-only and do not
use OpenTelemetry. Doctor reads the local Codex log database and prints a small
copy-pasteable list of recent non-2xx Responses API requests with upstream
request identifiers when available. If the most recent failures do not carry
upstream identifiers, doctor also prints the most recent failed Codex requests
that do carry request IDs so local proxy 502s do not hide useful support IDs.
Claude, Gemini, and OTel ingestion remain out of scope for this section until a
later design explicitly adds them.

## Tool Detection

Doctor uses the same supported tool set as `ae-cli discover`:

- `codex`
- `claude`
- `gemini`

Detection rules should be shared with or equivalent to `toolconfig.DetectInstalledTools`:

1. Prefer `exec.LookPath`.
2. Keep the Codex App fallback for `~/Applications/Codex.app` and `/Applications/Codex.app`, but mark app-only Codex as "configured but probe skipped" unless a callable CLI is available.
3. Print each detected executable path and version when a version command succeeds.
4. Treat a missing executable as a tool-level readiness failure when the current provider has a credential for that tool's platform. If no matching credential exists, report the tool as skipped because discover would not configure it.

## Provider Contract Validation

Doctor should fetch the same provider surface used by `ae-cli discover`:

```text
GET /api/v1/user/providers
```

If the user providers endpoint is unavailable because the backend is older, doctor may use the same legacy fallback as `client.ListProviders`, but the output must note that platform-level validation is degraded.

Selection rules must match `ae-cli discover`:

1. If future doctor options select a provider explicitly, use that provider.
2. Otherwise select `is_primary=true`.
3. If no primary provider exists, select the first provider.
4. For each tool, select the active credential matching the tool platform:
   - `codex` -> `openai`
   - `claude` -> `anthropic`
   - `gemini` -> `gemini`

Doctor compares local configuration against the selected provider and credential without printing secrets:

- Base URLs must match after trimming whitespace.
- API keys must match the current selected credential by exact value in memory, but output only says `credential=match`, `credential=mismatch`, `credential=missing`, or `credential=unavailable`.
- If provider lookup fails, local file validation and tool probes should still run, but credential matching is reported as `unavailable`.

## Configuration Validation

Doctor validates the files that `ae-cli discover` writes.

### Codex

Validate:

- `~/.codex/config.toml` exists and parses as TOML.
- `model_provider` is set.
- `model_providers.<model_provider>.base_url` is set.
- `model_providers.<model_provider>.wire_api = "responses"`.
- `model_providers.<model_provider>.requires_openai_auth = true`.
- `~/.codex/auth.json` exists and parses as JSON.
- `OPENAI_API_KEY` exists and is non-empty.
- If provider contract data is available, the configured Codex base URL and `OPENAI_API_KEY` match the selected `openai` credential.

Doctor may compare configured `model` and `review_model` to the current discover contract, but this must be reported as `mismatch` rather than a hard failure because users may intentionally update models after discovery.

### Claude

Validate:

- `~/.claude/settings.json` exists and parses as JSON.
- `env.ANTHROPIC_BASE_URL` exists and is non-empty.
- `env.ANTHROPIC_AUTH_TOKEN` exists and is non-empty.
- `env.CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC = "1"`.
- `env.CLAUDE_CODE_ATTRIBUTION_HEADER = "0"`.
- If provider contract data is available, the configured Claude base URL and `ANTHROPIC_AUTH_TOKEN` match the selected `anthropic` credential.

### Gemini

Validate:

- `~/.ae-cli/env.sh` exists.
- It contains an AE-managed block.
- The managed block defines non-empty `GEMINI_API_KEY`.
- The managed block defines non-empty `GOOGLE_GEMINI_BASE_URL`.
- The selected shell rc file sources `~/.ae-cli/env.sh`.
- If provider contract data is available, the configured Gemini base URL and `GEMINI_API_KEY` match the selected `gemini` credential.

When `--probe-tools` is used, doctor should parse the managed env file itself and pass those variables to the Gemini probe process, so a current shell that has not sourced the rc file can still be diagnosed accurately.

## Tool Probe

When `--probe-tools` is set, doctor runs a short, non-interactive probe for each installed and configured CLI-backed tool.

Prompt:

```text
Reply exactly: AE_DOCTOR_OK
```

Probe commands:

```bash
codex --ask-for-approval never exec --ephemeral --sandbox read-only "Reply exactly: AE_DOCTOR_OK"
claude -p "Reply exactly: AE_DOCTOR_OK" --output-format text --no-session-persistence --tools ""
gemini --prompt "Reply exactly: AE_DOCTOR_OK" --output-format text --skip-trust
```

Rules:

1. Default `ae-cli doctor` must not run these commands.
2. Each opt-in probe gets an independent timeout. The initial doctor timeout should be `60s`.
3. Doctor prints a visible `running timeout=<duration>` line before starting each probe so the user can see which long-running command is active.
4. A zero exit code with non-empty stdout is success.
5. A non-zero exit code, timeout, or empty stdout is a diagnostic failure for that tool.
6. Doctor prints a bounded excerpt of stderr or stdout on failure, with secret redaction applied.
7. Doctor does not require an exact `AE_DOCTOR_OK` match because model output can include formatting or quotes. Exact-match validation may be added later if it proves stable.
8. Probe execution must not request permissions, open interactive sessions, or rely on a TTY.

## Repo Eligibility Timeout

Doctor must not reuse the hook-time eligibility timeout.

Current hook behavior keeps:

```text
hook eligibility timeout = 2s
```

Doctor adds a separate timeout:

```text
doctor repo eligibility timeout = 10s
```

Doctor should print elapsed time for the online eligibility request:

```text
Repo Eligibility: eligible (repo_config_id=2, duration=987ms)
```

If the request times out, the diagnostic should make the timeout budget visible:

```text
Repo Eligibility: unavailable (timeout after 10s)
```

## Output Shape

Example successful shape:

```text
Sessionless attribution doctor
  Logged In:     true [ok]
  State Exists:  true [ok]

Tool configuration
  provider: sub2api source=user/providers
  [warn] codex: warn config=/Users/alice/.codex/config.toml auth=present base_url=match credential=match model=gpt-5.5 model_contract=mismatch(expected=gpt-5.4)
  [ok] claude: ok settings=/Users/alice/.claude/settings.json base_url=match credential=match
  [ok] gemini: ok env=/Users/alice/.ae-cli/env.sh shell_rc=/Users/alice/.zshrc base_url=match credential=match

Tool probe: skipped [warn] (use --probe-tools to run local CLI probes)

Recent Codex Failures: 1 [warn] (most recent Codex request errors)
  - 2026-06-18 17:30:00 status=503 Service Unavailable
      url=https://relay.example.com/responses
      x-request-id=req-503
      x-client-request-id=client-503
      x-kong-request-id=kong-503

Recent Codex Failures With Request IDs: 1 [warn] (most recent Codex request errors with upstream IDs)
  - 2026-06-11 20:51:56 status=504 Gateway Timeout
      url=https://relay.example.com/responses
      x-request-id=req-older
      x-client-request-id=(none)
      x-kong-request-id=(none)
```

With opt-in probes:

```text
Tool probe [running]
  [running] codex: running timeout=1m0s
  [ok] codex: ok duration=4.2s output=AE_DOCTOR_OK
  [running] claude: running timeout=1m0s
  [ok] claude: ok duration=3.8s output=AE_DOCTOR_OK
  [running] gemini: running timeout=1m0s
  [ok] gemini: ok duration=5.1s output=AE_DOCTOR_OK
```

Status badges should be colored on interactive terminals and when `CLICOLOR_FORCE` is set. `NO_COLOR` disables ANSI color. Non-interactive logs keep plain `[ok]`, `[warn]`, `[failed]`, and `[running]` badges.

Example partial failure:

```text
Tool configuration
  provider: sub2api source=user/providers
  [ok] codex: ok config=/Users/alice/.codex/config.toml auth=present base_url=match credential=match
  [failed] claude: failed settings missing env.ANTHROPIC_AUTH_TOKEN
  [failed] gemini: failed executable not found credential=present

Tool probe: skipped [warn] (use --probe-tools to run local CLI probes)
```

## Error Handling

Doctor should continue after individual diagnostic failures and print all actionable findings in one run.

Return code policy:

- Exit `0` when core doctor execution completes, even if one diagnostic line reports failed or skipped.
- Return non-zero only for unrecoverable command errors such as not being in a Git repository, inability to derive workspace identity, or unreadable required local state that prevents the command from producing a coherent report.

This preserves the current diagnostic style while still making failures visible in output.

## Testing

Add focused tests for:

1. Codex config validation with valid config/auth.
2. Codex config validation with model mismatch reported as warning/mismatch, not hard failure.
3. Claude settings validation with missing token.
4. Gemini env parsing and child process environment injection.
5. Provider contract matching for base URLs and credentials without secret output.
6. Missing executable reported as failed when a matching credential exists, and skipped when no matching credential exists.
7. Default doctor skips real tool probes and does not call the probe runner.
8. `doctor --probe-tools` prints `running` before each probe result.
9. Tool probe success using fake executables.
10. Tool probe timeout using a fake executable that sleeps.
11. Secret redaction in failure output.
12. Doctor status output uses visible status badges and colors when enabled.
13. Doctor repo eligibility uses the doctor timeout, not the hook timeout.
14. Recent failed-request diagnostics are labeled `Recent Codex Failures` and only read Codex local logs.
15. When recent Codex failures lack request IDs, doctor also prints `Recent Codex Failures With Request IDs`.

Default test command:

```bash
cd ae-cli && go test ./...
```
