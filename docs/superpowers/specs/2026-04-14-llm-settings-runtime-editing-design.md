# LLM Settings Runtime Editing Design

**Status:** Current contract for admin-managed LLM settings runtime editing

## Context

- [`2026-03-24-oauth-cli-login-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md) defines `relay.model` and relay credentials as part of the relay integration contract.
- [`2026-03-17-ai-efficiency-platform-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-17-ai-efficiency-platform-design.md) introduced `/api/v1/settings/llm` as the admin surface for LLM settings, but it is now historical baseline material.
- The implemented admin Settings page exposes a mixed surface spanning both `relay` and `analysis.llm`. This spec defines the current runtime-editable contract so frontend and backend stay aligned.

## Scope

This spec covers:

- `GET /api/v1/settings/llm`
- `PUT /api/v1/settings/llm`
- `POST /api/v1/settings/llm/test`
- the runtime update behavior expected by the admin Settings page

This spec does not change broader relay provider architecture, multi-provider delivery, or local session proxy design.

## Contract

### `GET /api/v1/settings/llm`

Returns the currently effective admin-facing LLM settings:

- `relay_url`
- `relay_api_key` (masked)
- `relay_admin_api_key` (masked)
- `model`
- `max_tokens_per_scan`
- `system_prompt`
- `user_prompt_template`
- `enabled`

`model` is sourced from the current relay runtime config and is editable through this surface.

### `PUT /api/v1/settings/llm`

Accepts the following writable fields:

- `relay_admin_api_key`
- `model`
- `max_tokens_per_scan`
- `system_prompt`
- `user_prompt_template`

Persistence rules:

- `relay.admin_api_key` and `relay.model` are written back under the `relay` section.
- `analysis.llm.max_tokens_per_scan`, `analysis.llm.system_prompt`, and `analysis.llm.user_prompt_template` are written back under the `analysis.llm` section.
- `relay.url` and `relay.api_key` remain read-only in this admin surface. They still come from the server's relay configuration source of truth.

Runtime rules:

- updates must take effect without process restart
- the in-memory LLM analyzer reloads prompt and token settings
- the in-memory relay provider reloads `admin_api_key` and `model`
- when `AE_CONFIG_PATH` is unset, the backend materializes a writable config file at `${AE_DEPLOYMENT_STATE_DIR}/config.yaml` (or `./config.yaml` outside deployment mode) before applying admin-edited settings
- the response returns the effective current config with masked keys

### `POST /api/v1/settings/llm/test`

Sends a minimal real chat-completions request using the current live relay runtime settings:

- relay URL
- relay API key
- relay model
- optional request-scoped `prompt`

Response shape:

- `success`
- `message`
- `response` optional preview of the first returned assistant message

The goal of `response` is observability for admins. The Settings page should show the actual returned text when present instead of only a generic success banner.
The request should be a short natural-language prompt rather than a `ping`/`pong` sentinel so admins can confirm the relay is producing an actual model completion.
If the request omits `prompt` or sends an empty string, the backend uses `Hi`.
`prompt` is request-scoped only. It is not persisted to `config.yaml` and does not affect analysis/chat prompts elsewhere in the product.

## Frontend Expectations

- The Settings page may edit `model` directly.
- After a successful save, the page should rehydrate its local form state from the response payload.
- The Settings page may send a temporary `Test Prompt` value with `POST /api/v1/settings/llm/test`, defaulting to `Hi`.
- Test Connection should render both the status message and the returned response preview when available.

## Relationship To Other Specs

- This spec refines the current admin settings contract on top of [`2026-03-24-oauth-cli-login-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md), which remains the source of truth for broader relay/provider architecture.
- This spec supersedes the older implicit `/settings/llm` assumptions in [`2026-03-17-ai-efficiency-platform-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-17-ai-efficiency-platform-design.md) without rewriting that historical baseline.
