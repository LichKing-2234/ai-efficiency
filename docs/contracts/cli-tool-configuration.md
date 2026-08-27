# CLI Tool Configuration Contract

This contract describes how `ae-cli discover` selects Relay credentials and
configures supported local AI tools. Read it before changing provider
discovery, tool detection, local config ownership, or the reporting activation
that follows configuration.

User-facing Relay provider, Access Group, and personal-key semantics are
defined in [`relay-user-access.md`](./relay-user-access.md).

## Deterministic Discovery

`ae-cli discover` is a deterministic configuration command. It does not use an
LLM to inspect local files, infer a provider, or conduct a multi-turn tool-call
session.

The command requires an authenticated platform client and then:

1. Loads available Relay providers and their credentials.
2. Selects one provider.
3. Detects installed supported tools, unless the user explicitly selects them.
4. Matches each tool to one active credential by group platform.
5. Writes only the configs for tools that have both user intent/detection and a
   matching credential.

There is no backend discovery endpoint and login does not automatically run
this command. Discover writes configuration; it does not make a live model
request to prove the resulting client works.

## Provider and Credential Selection

The CLI first requests the group-aware user-provider projection. A non-empty
response preserves every active `provider + group.platform` credential. A
missing endpoint or an empty response falls back to the legacy provider-level
endpoint; other errors fail the command.

An explicit provider value must exactly match provider name or display name.
Without one, the first primary provider wins, then the first returned provider.

Tool-to-platform matching is fixed:

| Tool | Required group platform |
| --- | --- |
| Codex | `openai` |
| Claude | `anthropic` |
| Gemini | `gemini` |

An installed or explicitly selected tool without a matching active credential
is skipped. A group-aware provider never substitutes its first key for another
platform. Provider-level key fallback exists only when the legacy response has
no group credential list.

## Tool Selection

Without explicit tools, the CLI searches `PATH` for Codex, Claude, and Gemini.
When Codex is not on `PATH`, macOS detection checks the current ChatGPT app and
then the legacy Codex app in user and system Applications directories. These
apps share the Codex config directory.

One or more `--tool` values bypass installation detection for this invocation.
Values may be repeated or comma-separated. The command trims, validates, and
deduplicates all elements in first-seen order before writing anything. Empty or
unsupported elements reject the command rather than falling back to detection.
Explicit tool selection does not bypass provider or credential validation.

## Local Configuration Ownership

| Tool | Owned output | Merge boundary |
| --- | --- | --- |
| Codex | Codex TOML config and auth JSON | Preserve unrelated TOML settings; replace the auth JSON with the selected `OPENAI_API_KEY` only. |
| Claude | Claude settings JSON | Preserve unrelated top-level settings; own the Relay-related environment keys and remove the legacy Anthropic API-key key. |
| Gemini | AE-managed shell environment block and one shell-rc source line | Preserve content outside the managed block; do not persist a model choice. |

Codex configuration selects the chosen Relay provider, its base URL, the
Responses wire protocol, current fixed Codex model/review policy, required
OpenAI auth, and the current non-WebSocket gateway boundary. A provider-level
default model is not group-specific and does not override the Codex model.

Claude configuration supplies the Relay base URL and personal Anthropic auth
token while disabling nonessential traffic and attribution headers. It does
not add a top-level model field.

Gemini configuration writes the personal key and canonical Gemini base-URL
environment variables into the AE-managed environment block. The command
reports the shell rc file and prints an explicit current-shell model hint. The
model hint is not written into the managed environment file.

JSON/TOML parent directories and secret-bearing files use user-only
permissions. The shell environment block is replaceable and sorted; the shell
rc source line is appended at most once. Config writers do not claim ownership
of unrelated user fields except for the deliberately complete Codex auth JSON.

## Dry Run and Reporting

`--dry-run` performs provider, tool, and credential selection and reports the
target paths without writing files, changing shell rc files, or activating
reporting.

After at least one real tool configuration succeeds, the CLI records the
selected Relay provider in reporting state and attempts reporting activation.
Configuration success is not rolled back when that activation fails; the CLI
emits a warning and leaves the configured tool usable.

## Failure Boundaries

- Missing login, provider, or base URL fails before configuration.
- No detected tools or no matching credentials is a successful no-op with an
  explanatory result.
- A parse or write error stops the command and identifies the failing config.
- Tool configuration does not change platform, Relay, repo binding, hook, or
  release state.
- New tools, inference rules, config ownership, or automatic discovery require
  a current GitHub spec/ticket before implementation.
