# ae-cli Doctor Tool Validation Design

**Date:** 2026-06-01  
**Status:** Proposed design, not yet implemented  
**Scope:** `ae-cli/`, `docs/`  
**Related:**  
- [2026-05-19-ae-cli-deterministic-tool-configuration-design.md](./2026-05-19-ae-cli-deterministic-tool-configuration-design.md)
- [2026-05-23-global-git-hooks-design.md](./2026-05-23-global-git-hooks-design.md)
- [2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md](./2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md)
- [docs/architecture.md](../../architecture.md)

The current project-level architecture remains documented in [`docs/architecture.md`](../../architecture.md). This document defines the next `ae-cli doctor` diagnostic contract and does not claim the behavior already exists.

## Spec Relationship

- The deterministic tool configuration spec remains authoritative for what `ae-cli discover` writes.
- This design adds verification of that configured state to `ae-cli doctor`.
- It does not introduce the historical LLM-driven `/api/v1/tools/discover` flow.
- It does not use backend `/api/v1/user/providers/:id/test` for local tool readiness. Doctor must validate the same local tool commands a user will run.

## Problem

`ae-cli doctor` currently verifies sessionless attribution readiness, hook state, sync backlog, and repository eligibility. It does not verify whether `ae-cli discover` actually left Codex, Claude, or Gemini usable on the current machine.

That creates two blind spots:

1. A user can pass repo attribution checks while Codex, Claude, or Gemini is not installed, misconfigured, or unable to return a model response through the configured local CLI.
2. The online repo eligibility check reuses the hook-time `500ms` timeout. That timeout is appropriate for fail-open Git hooks, but it is too short for a human-run diagnostic command and can report `context deadline exceeded` even when the backend would return a valid eligible result in a few seconds.

## Goals

1. Make `ae-cli doctor` the trusted post-setup diagnostic for both repository attribution and local AI tool readiness.
2. Verify the actual local tool commands after `ae-cli discover`, not only backend provider connectivity.
3. Keep diagnostics safe: never print API keys, never require interactive prompts, and bound every external command with a timeout.
4. Keep hook execution fast by separating doctor diagnostics timeouts from hook-time eligibility timeouts.
5. Reuse `ae-cli discover` provider and platform credential selection so local config is compared against the current user-facing credential contract.
6. Produce output that distinguishes install, config, credential, command execution, timeout, and backend eligibility failures.

## Non-Goals

1. Do not add a `--live-tools` flag. Tool probing is part of the default human-run `ae-cli doctor`.
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
```

The command remains a single default diagnostic:

```bash
ae-cli doctor
```

No new flag is required to run local tool probes.

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

Doctor should parse the managed env file itself and pass those variables to the Gemini probe process, so a current shell that has not sourced the rc file can still be diagnosed accurately.

## Tool Probe

Doctor runs a short, non-interactive probe for each installed and configured CLI-backed tool.

Prompt:

```text
Reply exactly: AE_DOCTOR_OK
```

Probe commands:

```bash
codex exec --ephemeral --sandbox read-only --ask-for-approval never "Reply exactly: AE_DOCTOR_OK"
claude -p "Reply exactly: AE_DOCTOR_OK" --output-format text --no-session-persistence --tools ""
gemini --prompt "Reply exactly: AE_DOCTOR_OK" --output-format text --skip-trust
```

Rules:

1. Each probe gets an independent timeout. The initial timeout should be `30s`.
2. A zero exit code with non-empty stdout is success.
3. A non-zero exit code, timeout, or empty stdout is a diagnostic failure for that tool.
4. Doctor prints a bounded excerpt of stderr or stdout on failure, with secret redaction applied.
5. Doctor does not require an exact `AE_DOCTOR_OK` match because model output can include formatting or quotes. Exact-match validation may be added later if it proves stable.
6. Probe execution must not request permissions, open interactive sessions, or rely on a TTY.

## Repo Eligibility Timeout

Doctor must not reuse the hook-time eligibility timeout.

Current hook behavior keeps:

```text
hook eligibility timeout = 500ms
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
Tool configuration
  provider: sub2api source=user/providers
  codex:  ok config=/Users/alice/.codex/config.toml auth=present base_url=match credential=match model=gpt-5.5 model_contract=mismatch(expected=gpt-5.4)
  claude: ok settings=/Users/alice/.claude/settings.json base_url=match credential=match
  gemini: ok env=/Users/alice/.ae-cli/env.sh shell_rc=/Users/alice/.zshrc base_url=match credential=match

Tool probe
  codex:  ok duration=4.2s output=AE_DOCTOR_OK
  claude: ok duration=3.8s output=AE_DOCTOR_OK
  gemini: ok duration=5.1s output=AE_DOCTOR_OK
```

Example partial failure:

```text
Tool configuration
  provider: sub2api source=user/providers
  codex:  ok config=/Users/alice/.codex/config.toml auth=present base_url=match credential=match
  claude: failed settings missing env.ANTHROPIC_AUTH_TOKEN
  gemini: failed executable not found credential=present

Tool probe
  codex:  ok duration=4.2s output=AE_DOCTOR_OK
  claude: skipped configuration failed
  gemini: skipped executable not found
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
7. Tool probe success using fake executables.
8. Tool probe timeout using a fake executable that sleeps.
9. Secret redaction in failure output.
10. Doctor repo eligibility uses the doctor timeout, not the hook timeout.

Default test command:

```bash
cd ae-cli && go test ./...
```
