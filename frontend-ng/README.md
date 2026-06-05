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
AE_FRONTEND_GATEWAY_EXCHANGE_SECRET=dev-shared-secret
```

`VITE_BACKEND_URL` is accepted as a local fallback for compatibility, but browser code must not call that backend URL directly.

## Auth Boundary

- Browser JavaScript never reads app access or refresh tokens.
- Go backend remains the app auth authority.
- TanStack stores Go-issued app tokens in HttpOnly cookies named `ae_app_access` and `ae_app_refresh`.
- Gateway OAuth identity is exchanged server-to-server through `/api/auth/bootstrap` and Go `/api/v1/auth/gateway-exchange`.
- Manual `/login` and `/api/auth/dev-login` are local/fallback paths.

## Local Handoff

`GET /api/local` and `GET /api/local/callback` are wired as frontend-owned localdev handoff entry points. They currently return `501` until the Go backend exposes one-time handoff code issuance/redeem APIs. The implementation intentionally rejects non-localhost targets and does not copy gateway cookies or gateway tokens to localhost.

## Verification

```bash
bun run check
bun run build
```

The production server can be started after a build:

```bash
bun run start
```
