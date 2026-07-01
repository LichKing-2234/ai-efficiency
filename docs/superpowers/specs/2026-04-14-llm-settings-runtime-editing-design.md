# LLM Settings Runtime Editing Design

**Status:** Current compatibility contract for runtime relay LLM editing

## Context

- [`2026-03-24-oauth-cli-login-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md) defines `relay.model` and relay credentials as part of the relay integration contract.
- [`2026-03-17-ai-efficiency-platform-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-17-ai-efficiency-platform-design.md) introduced `/api/v1/settings/llm` as the admin surface for LLM settings, but it is now historical baseline material.
- The scan/chat runtime has been retired.
- The primary admin Settings UI now manages DB-backed `RelayProvider` records through `/api/v1/admin/providers`, not a single-provider `Relay Configuration` form.
- `/api/v1/settings/llm*` remains a compatibility and runtime-edit surface for the bootstrapped primary relay config, not the main multi-provider management surface.

## Scope

This spec covers:

- `GET /api/v1/settings/llm`
- `PUT /api/v1/settings/llm`
- `POST /api/v1/settings/llm/test`
- the runtime update behavior expected by compatibility or operator-only flows that still edit the bootstrapped primary relay config

This spec does not change broader relay provider architecture, multi-provider delivery, or local session proxy design.

## Contract

### `GET /api/v1/settings/llm`

Returns the currently effective admin-facing relay LLM settings:

- `relay_url`
- `relay_admin_api_key` (masked)
- `model`
- `enabled`

`model` is sourced from the current relay runtime config and is editable through this surface.

### `PUT /api/v1/settings/llm`

Accepts the following writable fields:

- `relay_admin_api_key`
- `model`

Persistence rules:

- `relay.admin_api_key` and `relay.model` are written back under the `relay` section.
- `relay.url` remains read-only in this admin surface. `relay.api_key` / `AE_RELAY_API_KEY` and `relay.admin_url` / `AE_RELAY_ADMIN_URL` are removed from the backend config contract.
- No values are written under `analysis.llm`; that config surface is retired.

Runtime rules:

- updates must take effect without process restart
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
`prompt` is request-scoped only. It is not persisted to `config.yaml`.

## Frontend Expectations

- The main Settings page should use `/api/v1/admin/providers` for multi-provider relay management.
- If a compatibility UI still exposes `/api/v1/settings/llm`, it may edit `model` and `relay_admin_api_key` for the bootstrapped primary relay runtime.
- After a successful compatibility save, that UI should rehydrate its local form state from the response payload.
- A compatibility UI may send a temporary `Test Prompt` value with `POST /api/v1/settings/llm/test`, defaulting to `Hi`.
- Compatibility test flows should render both the status message and the returned response preview when available.

## Relationship To Other Specs

- This spec refines the current admin settings contract on top of [`2026-03-24-oauth-cli-login-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md), which remains the source of truth for broader relay/provider architecture.
- This spec supersedes the older implicit `/settings/llm` assumptions in [`2026-03-17-ai-efficiency-platform-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-17-ai-efficiency-platform-design.md) without rewriting that historical baseline.
