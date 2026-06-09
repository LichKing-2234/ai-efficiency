# AI Efficiency Frontend NG

TanStack Start replacement frontend for the AI Efficiency console.

## Local Development

```bash
bun install
bun run dev --host 127.0.0.1 --port 4317
```

The browser should use the TanStack origin only. Business API calls go to same-origin `/api/*` routes, then the TanStack server proxies to the Go backend.

Useful environment variables:

```bash
AE_FRONTEND_BACKEND_URL=http://localhost:8081
```

`VITE_BACKEND_URL` is accepted as a local fallback for compatibility, but browser code must not call that backend URL directly.
`AE_FRONTEND_GATEWAY_EXCHANGE_SECRET` should only be configured after the Go backend exposes `/api/v1/auth/gateway-exchange` and the shared header contract is agreed.

## Auth Boundary

- Browser JavaScript never reads app access or refresh tokens.
- Go backend remains the app auth authority.
- TanStack stores Go-issued app tokens in HttpOnly cookies named `ae_app_access` and `ae_app_refresh`.
- Gateway OAuth identity is exchanged server-to-server through `/api/auth/bootstrap` and Go `/api/v1/auth/gateway-exchange`.
- Manual `/login` and `/api/auth/dev-login` are local/fallback paths.

## Local Handoff

Gateway deployments should use the OAuth-plugin-compatible path:

```text
https://ai-efficiency-web.la3.agoralab.co/oauth2/local?target=http%3A%2F%2F127.0.0.1%3A4317
```

`GET /oauth2/local?target=http://127.0.0.1:4317` redirects an active online app session to the local frontend's `/oauth2/local` callback. The callback writes localhost-scoped HttpOnly app cookies and redirects to `/`.

`GET /api/local?target=http://127.0.0.1:4317` remains available as a same-origin API compatibility path, but gateway-protected deployments should prefer `/oauth2/local` so the gateway OAuth client can use its registered `/oauth2/callback` redirect URI.

The handoff only accepts localhost targets. It copies Go-issued app tokens, not gateway cookies or gateway tokens.

## Verification

```bash
bun run check
bun run build
```

The production server can be started after a build:

```bash
bun run start
```
