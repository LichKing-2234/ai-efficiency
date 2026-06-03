# ae-cli Shared HTTP Request Handler Design

**Status:** Proposed implementation contract
**Scope:** `ae-cli/internal/httpx`, `ae-cli/internal/auth`, `ae-cli/internal/client`
**Related:** [2026-03-24-oauth-cli-login-design.md](./2026-03-24-oauth-cli-login-design.md), [2026-04-15-oauth-device-login-design.md](./2026-04-15-oauth-device-login-design.md), [2026-05-19-ae-cli-deterministic-tool-configuration-design.md](./2026-05-19-ae-cli-deterministic-tool-configuration-design.md)

## Context

`ae-cli` currently has two HTTP-facing areas with different protocol needs:

1. `ae-cli/internal/auth` handles login-time OAuth endpoints under `/oauth/*`.
   These calls happen before an access token exists and use `application/x-www-form-urlencoded`.
2. `ae-cli/internal/client` handles authenticated AI Efficiency API calls under `/api/v1/*`.
   These calls use JSON and normally include a bearer token.

`internal/client.Client` is the closest current request abstraction, but it is not a general request handler. It owns authenticated AE API concepts such as `baseURL`, token headers, provider payloads, repo resolution, checkpoints, and tool usage events. Some methods use the local `postJSON` helper, while others still create requests, execute them, read bodies, and format errors manually.

OAuth login intentionally does not use `client.Client` because login is pre-token and uses root-level OAuth protocol endpoints rather than authenticated `/api/v1` JSON APIs. That boundary should stay intact. The missing layer is a lower-level helper shared by both packages for request execution and error handling.

This gap became visible when an ingress returned a JSON body shaped as:

```json
{"message":"Your IP address is not allowed"}
```

The OAuth device flow only looked for the OAuth-standard `error` field, so the terminal showed an empty error. Similar bugs can recur wherever request code formats non-2xx responses independently.

## Goals

1. Introduce a shared HTTP request helper for `ae-cli` without merging OAuth login and authenticated AE API responsibilities.
2. Standardize non-2xx error handling across JSON and form requests.
3. Preserve useful user-facing messages from OAuth servers, backend APIs, and ingress/gateway layers.
4. Keep request helpers small enough that `auth` and `client` remain easy to test independently.
5. Make the first migration targeted: OAuth login paths and existing `client.postJSON` consumers move first; broader `client.Client` cleanup can happen incrementally.

## Non-Goals

1. Do not move OAuth login into `internal/client.Client`.
2. Do not redesign backend OAuth endpoints or `/api/v1` response envelopes.
3. Do not add retry policy to all requests. Existing tool usage retry behavior remains local to tool usage upload paths unless a later spec changes it.
4. Do not normalize all legacy manually implemented `client.Client` methods in the first implementation if doing so increases risk. The shared helper must support incremental adoption.
5. Do not expose secrets, full tokens, or full API keys in error messages.

## Design Overview

Add a new package:

```text
ae-cli/internal/httpx
```

`httpx` is a protocol-level helper. It has no knowledge of OAuth grants, repo config, providers, checkpoints, tool usage, token storage, or config files. Its public contract is:

- build and send JSON requests
- build and send form requests
- attach caller-supplied headers
- decode success responses into caller-provided structs
- turn non-2xx responses into a structured `StatusError`
- derive a concise, user-facing error summary from common backend and gateway response shapes

Package ownership after the change:

| Package | Responsibility |
| --- | --- |
| `internal/auth` | OAuth PKCE flow, device flow, token exchange, login-time protocol decisions |
| `internal/client` | Authenticated `/api/v1` AE API client and typed business methods |
| `internal/httpx` | HTTP request execution, response decoding, status errors, error summary parsing |

## `httpx` API

The first implementation should provide:

```go
package httpx

type Options struct {
    Headers        http.Header
    ErrorBodyLimit int64
}

type StatusError struct {
    Method     string
    URL        string
    Status     string
    StatusCode int
    Summary    string
    Body       string
}

func (e *StatusError) Error() string

func DoJSON(ctx context.Context, client *http.Client, method, url string, in any, out any, opts Options) error
func DoForm(ctx context.Context, client *http.Client, method, url string, form url.Values, out any, opts Options) error
```

Behavior:

- `client == nil` uses `http.DefaultClient`.
- `DoJSON` sets `Content-Type: application/json` when a request body exists.
- `DoForm` sets `Content-Type: application/x-www-form-urlencoded`.
- `opts.Headers` are copied onto the request after the content type is set, so callers can add `Authorization` or override defaults deliberately.
- Any `2xx` status is success.
- If `out != nil`, the helper decodes the success body as JSON into `out`.
- If `out == nil`, the helper drains and closes the body.
- Any non-2xx status returns `*StatusError`.

`StatusError.Error()` should be stable and terminal-friendly:

```text
POST https://server.example.com/oauth/device/code failed (HTTP 403 Forbidden): Your IP address is not allowed
```

The URL is useful during local diagnosis, but implementations must avoid adding request bodies or authorization headers to errors.

## Error Summary Parsing

`httpx` reads at most `ErrorBodyLimit` bytes from non-2xx responses. If `ErrorBodyLimit <= 0`, use a default such as `4096`.

Summary precedence:

1. If JSON contains both `error` and `error_description`, use `error + ": " + error_description`.
2. Else use `error_description`.
3. Else use `error`.
4. Else use `message`.
5. Else use trimmed raw response body.
6. Else use `empty response body`.

The parser should tolerate invalid JSON and fallback to raw body. It should not fail only because the response body is not JSON.

The parsed raw `Body` stored in `StatusError` should be limited to the same maximum. This keeps diagnostics useful without risking huge output.

## OAuth Integration

`internal/auth` keeps its own OAuth result and polling behavior, but replaces direct request execution with `httpx.DoForm`.

Affected calls:

- `POST /oauth/device/code`
- `POST /oauth/token` for device grant polling
- `POST /oauth/token` for authorization-code token exchange

Device polling still needs to distinguish OAuth protocol control errors from fatal HTTP/request errors:

| OAuth error | CLI behavior |
| --- | --- |
| `authorization_pending` | sleep and poll again |
| `slow_down` | increase interval and poll again |
| `access_denied` | fail immediately |
| `expired_token` | fail immediately |
| `invalid_grant` | fail immediately |
| missing `error` on non-2xx | fail with `StatusError` summary |

To support this, `auth` can either decode the OAuth error payload directly through a small response struct returned by `httpx`, or use `errors.As(err, *httpx.StatusError)` and parse the already captured body for OAuth control values. The recommended implementation is to add a `httpx.DecodeErrorBody[T]` helper only if direct body parsing from `StatusError.Body` becomes awkward; do not overbuild it in v1.

User-facing login errors should keep the outer command context:

```text
login failed: device code request failed: POST https://server.example.com/oauth/device/code failed (HTTP 403 Forbidden): Your IP address is not allowed
```

## Authenticated API Client Integration

`internal/client.Client` remains the typed AE API client.

For migrated methods, `Client` should use a small local header helper:

```go
func (c *Client) headers() http.Header
```

It returns:

- `Authorization: Bearer <token>` when `token` is non-empty
- no token header otherwise

`httpx.DoJSON` owns the JSON content type. `client.Client` owns only authenticated API semantics.

First migration target:

- Replace `postJSON` internals with `httpx.DoJSON`.
- Keep `ResolveRepoFromRemote` and `BatchHookEligible` behavior unchanged.
- Convert their non-2xx failures to expose `*httpx.StatusError`.

Follow-up migration can move manually implemented methods such as provider listing, repo ensure, checkpoints, and tool usage upload once tests are adjusted. Tool usage retry logic should continue to own retry decisions, but it can use `httpx.StatusError` for status classification.

## Compatibility

This is an internal CLI refactor. It does not change:

- OAuth endpoint paths
- request parameters
- token file format
- `/api/v1` backend response envelopes
- `ae-cli login` or `ae-cli login --device` command flags
- authenticated API behavior when responses are successful

Observable change:

- Non-2xx failures become more explicit and consistent.
- Errors that previously printed empty summaries now include HTTP status and gateway/backend message when available.

## Testing

Add focused unit tests for `internal/httpx`:

1. `DoJSON` sends JSON and decodes success.
2. `DoForm` sends form content type and decodes success.
3. Non-2xx OAuth JSON with `error` and `error_description` yields a combined summary.
4. Non-2xx gateway JSON with `message` yields that message.
5. Non-JSON body falls back to trimmed raw body.
6. Empty body reports `empty response body`.
7. Error body is capped by the configured/default limit.
8. Authorization headers supplied by callers are attached without being rendered in errors.

Update `internal/auth` tests:

1. Device code request with `{"message":"Your IP address is not allowed"}` returns an error containing `HTTP 403 Forbidden` and the message.
2. Device token polling still treats `authorization_pending` and `slow_down` as control states.
3. Device token polling fails with `access_denied: <description>` when present.
4. Authorization-code token exchange shows the same status/message format.

Update `internal/client` tests for migrated methods:

1. `ResolveRepoFromRemote` and `BatchHookEligible` still send the same paths and payloads.
2. Non-2xx responses can be inspected with `errors.As(err, *httpx.StatusError)`.

Run:

```bash
cd ae-cli && go test ./internal/httpx ./internal/auth ./internal/client
cd ae-cli && go test ./...
```

## Rollout Plan

1. Add `internal/httpx` with tests.
2. Migrate OAuth login paths to `httpx.DoForm`.
3. Migrate `client.postJSON` to `httpx.DoJSON`.
4. Keep existing broad `client.Client` methods working; only touch additional methods if tests or compile errors require it.
5. Remove any temporary auth-local error parser after the shared helper is in use.

## Documentation Impact

This is an `ae-cli` internal architecture cleanup and user-facing error improvement. It does not alter project-level deployment, backend module relationships, OAuth endpoint contracts, or CLI command contract. No update to `docs/architecture.md` is required.

This spec becomes the current implementation contract for `ae-cli` shared HTTP request handling. The OAuth flow contracts remain in the existing OAuth specs.
