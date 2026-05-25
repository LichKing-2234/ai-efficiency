# Redis-Backed Auth Session Design

**Date:** 2026-05-25  
**Status:** Proposed current design  
**Scope:** `backend/internal/auth/`, `backend/internal/oauth/`, `backend/internal/handler/`, `backend/cmd/server/`, `frontend/src/api/`, `frontend/src/stores/`, `deploy/`, Helm deployment values  
**Related:**  
- [2026-03-24-oauth-cli-login-design.md](./2026-03-24-oauth-cli-login-design.md)  
- [2026-04-15-oauth-device-login-design.md](./2026-04-15-oauth-device-login-design.md)  
- [`docs/architecture.md`](../../architecture.md)

## Spec Relationship

- 本文收敛 Web 登录态、CLI OAuth token、refresh token 轮转、登出撤销，以及 OAuth 短生命周期授权状态的 Redis 持久化合同。
- 本文不改变 `relay.Provider` / sub2api 集成边界；ai-efficiency 仍只通过 HTTP API 使用 sub2api。
- 本文不新增一套 Redis 部署配置。ai-efficiency 继续复用现有 `AE_REDIS_ADDR`、`AE_REDIS_PASSWORD`、`AE_REDIS_DB`，生产环境可继续与 sub2api 指向同一个阿里云 Redis 实例。
- 本文补齐的是 ai-efficiency 自身没有真正使用 Redis 保存登录态的问题；当前代码只创建 Redis client 并用于 health readiness。

## Current Behavior

当前 ai-efficiency 登录态链路是：

1. Web 登录、OAuth PKCE、Device Flow 都调用 `auth.Service.GenerateTokenPairForUser` 或 `auth.Service.Login`
2. `auth.Service` 生成 access token 与 refresh token
3. access token 和 refresh token 都是用 `AE_AUTH_JWT_SECRET` 签名的无状态 JWT
4. 前端把 token 存在 `localStorage`
5. `/api/v1/auth/refresh` 只校验 refresh JWT 签名、类型、过期时间，再从 Postgres 重读用户并签发新 JWT
6. Redis 仅在 `/api/v1/health/ready` 中被 ping，没有参与 auth/session

因此：

- 发布后如果 `AE_AUTH_JWT_SECRET` 变化，所有旧 access/refresh token 立即失效，用户必须重新登录。
- 即使 secret 稳定，refresh token 也没有服务端会话记录，无法单点登出、全端登出、检测 refresh token 重放，也无法按用户批量撤销。
- OAuth authorization code 与 device code 当前保存在进程内 map。发布、重启、多副本负载均衡时，正在进行的 CLI 登录授权可能丢失或命中不同 pod。

## Goals

1. 发布新版本、pod 重启、滚动部署后，已经登录的 Web 用户和 `ae-cli` refresh token 继续可用。
2. refresh token 改为服务端有状态会话：服务端只保存 token hash，不保存原始 token。
3. 支持 refresh token 轮转：每次刷新立即撤销旧 refresh token，返回新的 refresh token。
4. 支持按用户撤销全部 refresh sessions，支持登出撤销单个 refresh token。
5. OAuth PKCE authorization code 与 device authorization state 从进程内 map 迁移到 Redis，避免发布中断 CLI 登录流程，并为后续多副本部署留出空间。
6. 复用现有 Redis 配置与 Helm values，不增加新的 Redis 地址、密码、database 配置项。
7. 与 sub2api 共用 Redis 实例时必须通过 key prefix 隔离，避免 key 冲突。

## Non-Goals

1. 不把浏览器登录改成 HttpOnly cookie。当前仍沿用 Bearer token + `localStorage`，安全模型单独规划。
2. 不把 access token 改为服务端 session。access token 继续是短生命周期 JWT。
3. 不重新引入 sub2api DB 直连。
4. 不新增一个独立 Redis 实例，不新增 Helm Redis dependency。
5. 不要求 Redis 保存长期业务事实；用户、角色、relay 绑定等 durable state 仍以 Postgres 为准。

## Sub2api Reference

sub2api 的 Redis 使用可以拆成几类，ai-efficiency 只参考其中与登录态直接相关的模式：

| sub2api Redis 用法 | sub2api key / 行为 | ai-efficiency 取舍 |
| --- | --- | --- |
| Refresh token session | `refresh_token:{hash}` 保存 token 元数据，`user_refresh_tokens:{user_id}` 跟踪用户全部 token，`token_family:{family_id}` 跟踪同一登录会话族 | 直接参考。ai-efficiency 使用自己的 `ai-efficiency:auth:*` key prefix |
| 高风险 auth 限流 | `rate_limit:<scope>:<ip>`，Lua `INCR + PTTL`，注册、登录、refresh 等入口 Redis 故障时 fail-close | 第一版参考 `/auth/login`、`/auth/refresh` 限流思路；是否落地可独立于 refresh session |
| TOTP / email / password reset 短期状态 | `totp:*`、`verify_code:*`、`password_reset:*`，都带 TTL | 当前 ai-efficiency 没有对应功能，不实现 |
| sticky session / concurrency / user queue | `sticky_session:*`、`concurrency:*`、`session_limit:*`、`umq:*`，大量使用 Lua 和 Redis TIME | 属于 gateway 调度域，不搬到 ai-efficiency auth |
| API key auth cache / pubsub invalidation | `apikey:auth:*`、`auth:cache:invalidate` | ai-efficiency 的 relay API key delivery 当前不走本地 API key auth cache，不实现 |

关键参考点：

1. refresh token 必须是随机 opaque token，而不是可自校验的 refresh JWT。
2. Redis 只保存 token hash 与 metadata。
3. 每次 refresh 都旋转 refresh token，并删除旧 token。
4. 同一 family 内如果出现异常复用，可撤销整个 family。
5. Redis key 需要明确 TTL；集合 key 的 TTL 要随 token TTL 延长。

## Proposed Auth Model

### Token Shape

Access token:

- 继续使用 JWT
- 生命周期较短，由现有 `AE_AUTH_ACCESS_TOKEN_TTL` 控制
- 包含 `user_id`、`username`、`role`、`type=access`、`iat`、`exp`
- 不写入 Redis

Refresh token:

- 改为随机 opaque token
- 字符串建议形态：`rt_<base64url-random>` 或 `rt_<hex-random>`
- 原始 refresh token 只返回给客户端，不写入日志、不写入数据库、不写入 Redis
- Redis 中只保存 SHA-256 hash 对应的 session metadata

### Redis Key Contract

所有 key 必须带 ai-efficiency prefix。即使生产上与 sub2api 共用同一个 Redis DB，也不能使用 sub2api 的裸 key prefix。

```text
ai-efficiency:auth:refresh:{token_hash}        -> JSON RefreshSession, TTL = refresh token TTL
ai-efficiency:auth:user:{user_id}:refresh      -> Set<token_hash>, TTL = refresh token TTL
ai-efficiency:auth:family:{family_id}:refresh  -> Set<token_hash>, TTL = refresh token TTL
ai-efficiency:oauth:code:{code_hash}           -> JSON AuthorizationCodeSession, TTL = 5 minutes
ai-efficiency:oauth:device:{device_code_hash}  -> JSON DeviceSession, TTL = 15 minutes
ai-efficiency:oauth:user_code:{normalized}     -> device_code_hash, TTL = 15 minutes
```

`RefreshSession` fields:

```json
{
  "user_id": 123,
  "username": "alice",
  "role": "user",
  "family_id": "random-family-id",
  "created_at": "2026-05-25T00:00:00Z",
  "expires_at": "2026-06-01T00:00:00Z"
}
```

### Refresh Flow

1. Client calls `POST /api/v1/auth/refresh` with `refresh_token`.
2. Server validates opaque token prefix and length.
3. Server hashes the token and reads `ai-efficiency:auth:refresh:{hash}`.
4. Missing record returns 401. This includes expired, already rotated, logged out, or unknown token.
5. Server reads the user from Postgres by `user_id`.
6. Inactive/deleted users fail refresh and delete the token family.
7. Server deletes the old refresh token key.
8. Server generates a new access JWT and a new opaque refresh token with the same family id.
9. Server stores the new refresh session in Redis and returns the token pair.

The refresh endpoint is fail-closed if Redis is unavailable. A degraded Redis must not silently mint new sessions.

### Login Flow

1. Login still authenticates against LDAP and/or relay SSO according to existing provider order.
2. After `ensureLocalUser`, server generates access JWT plus Redis-backed refresh token.
3. Response shape remains compatible:

```json
{
  "data": {
    "tokens": {
      "access_token": "jwt-access-token",
      "refresh_token": "opaque-refresh-token",
      "expires_in": 900
    },
    "user": {}
  }
}
```

### Logout Flow

Add `POST /api/v1/auth/logout`.

- Request may include `refresh_token`.
- If refresh token is present, revoke only that refresh session.
- If no refresh token is present but access token is valid, do not attempt to revoke all sessions implicitly.
- Client always clears local tokens after calling logout.

Add `POST /api/v1/auth/logout-all` as an authenticated endpoint.

- Deletes `ai-efficiency:auth:user:{user_id}:refresh` and all referenced refresh token records.
- This is optional in the first UI, but the service contract should support it.

Access tokens remain valid until expiration. Keeping `AE_AUTH_ACCESS_TOKEN_TTL` short limits this window.

### OAuth Code and Device State

Current `backend/internal/oauth.Handler` keeps authorization code and device code state in process memory. Replace those maps with a store interface backed by Redis.

Authorization code:

- Store by code hash, not raw code.
- TTL = existing `codeExpiry` (5 minutes).
- Consumed atomically: token exchange must delete the code so it cannot be reused.

Device flow:

- Store device session by device code hash.
- Store normalized user code index to device code hash.
- Verify endpoint resolves by normalized user code, then updates the device session status.
- Token polling endpoint reads by device code hash.
- Consuming an approved device session deletes both the device key and user-code index.
- Poll throttling must use Redis state, not per-process `LastPolledAt`.

This makes CLI login survive rolling restarts and avoids same-pod affinity requirements.

## Deployment Contract

### Redis Configuration

No new Redis config is introduced.

ai-efficiency continues to use:

```text
AE_REDIS_ADDR
AE_REDIS_PASSWORD
AE_REDIS_DB
```

Helm values already expose those env vars. Production can continue to point them at the same Alibaba Cloud Redis instance used by sub2api. Isolation is handled by key prefix, not by a new config surface.

### JWT Secret Stability

`AE_AUTH_JWT_SECRET` must be a stable secret across releases.

- It must not be regenerated by release scripts.
- It must not be replaced by placeholder/dev values in production upgrade values.
- Rotating it is an explicit security operation and will invalidate outstanding access tokens. Redis-backed refresh sessions can still be invalidated separately by deleting session keys or rotating the refresh store prefix through a deliberate migration.

### Redis Failure Behavior

- Readiness already checks Redis; production traffic should only route to ready pods.
- Login, refresh, logout, and OAuth token exchange fail closed when Redis writes/reads required auth state fail.
- Existing non-auth API routes can keep their current behavior unless they add Redis-backed state of their own later.

## Frontend Contract

The existing frontend token storage can remain compatible:

- `frontend/src/stores/auth.ts` stores `token` and `refresh_token`.
- `frontend/src/api/client.ts` retries one 401 by calling `/auth/refresh`.
- After this change, refresh responses return a rotated refresh token; the client must keep replacing `refresh_token` with the returned value.
- Logout should call `/auth/logout` with the stored refresh token, then clear local tokens regardless of API success.

No router changes are required for the core session fix.

## Security Considerations

1. Redis stores token hashes only.
2. Refresh token reuse after rotation returns 401. A later hardening pass may revoke the whole family on confirmed reuse.
3. Refresh tokens must not be logged.
4. OAuth device/user codes must be high entropy enough for their role and always TTL-bound.
5. Redis key prefix is mandatory because production can share the Redis DB with sub2api.
6. Keeping access tokens short-lived reduces the post-logout access-token validity window.

## Open Questions

1. Should production shorten `AE_AUTH_ACCESS_TOKEN_TTL` from the current 2 hours to 15 minutes in Helm values?
2. Should refresh-token reuse immediately delete the whole family in v1, or first log and return 401 while keeping other family members?
3. Should `logout-all` be exposed in the Web UI immediately, or remain backend-only for the first patch?

## Acceptance Criteria

1. A user can log in, refresh the page, and remain authenticated.
2. A user can log in, restart the backend process, and refresh access token successfully using the existing refresh token.
3. A rolling deployment does not force re-login when `AE_AUTH_JWT_SECRET` is unchanged and Redis data remains.
4. Refreshing twice with the same old refresh token fails on the second attempt.
5. Refreshing with the latest rotated refresh token succeeds.
6. `POST /api/v1/auth/logout` revokes the submitted refresh token.
7. OAuth PKCE authorization code cannot be reused.
8. Device flow can be approved on one process/pod and polled on another process/pod if both share Redis.
9. Helm deployment continues to use existing `AE_REDIS_*` values; no new Redis values are required.
