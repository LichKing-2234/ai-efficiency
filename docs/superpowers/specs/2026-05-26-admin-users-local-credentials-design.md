# Admin Users Local Credentials Design

**Date:** 2026-05-26  
**Status:** Approved design for implementation planning  
**Scope:** `backend/internal/handler/`, `frontend/src/router/`, `frontend/src/views/`, `frontend/src/api/`, `frontend/src/types/`, `frontend/src/__tests__/`, `docs/architecture.md`  
**Related:**  
- [2026-03-24-oauth-cli-login-design.md](./2026-03-24-oauth-cli-login-design.md)  
- [2026-05-21-user-page-cli-self-serve-design.md](./2026-05-21-user-page-cli-self-serve-design.md)  
- [docs/architecture.md](../../architecture.md)

## Spec Relationship

- 本文定义一个新的 admin-only local user inspection surface，用于查看本地 `users` 表中的用户信息和本地保存的 encrypted relay credential。
- 本文不改变 login、LDAP bind、Relay SSO、OAuth、`/user` credential self-serve、provider CRUD 或 relay identity provisioning 的既有合同。
- 本文不把 relay 远端用户详情、API keys、usage 或 subscription/group facts 纳入第一版 admin users 页面。
- 本文明确允许 admin 通过显式 copy action 获取某个用户当前存储的 relay password 明文，但列表接口不得批量返回明文。
- 当前项目级架构文档应在实现阶段同步更新，记录新的 admin users surface 和 relay credential reveal 边界。

## Problem

当前系统已有本地 `users` 表，字段包括 `username`、`email`、`role`、`auth_source`、`relay_user_id` 和 encrypted `relay_auth_password`。这些字段支撑 Relay SSO、LDAP relay identity provisioning 和 `/user` 侧的用户态 API key 创建流程。

管理员目前没有一个独立页面可以查看本地用户绑定状态，也无法在需要排障时取出某个用户已保存的 relay password 明文。直接把明文放进用户列表响应会扩大泄露面，因为列表加载、浏览器状态、日志和测试夹具都更容易无意扩散敏感数据。

## Goals

1. 新增 admin-only `/admin/users` 页面，展示本地用户列表。
2. 支持按 `username` / `email` 搜索，纯数字查询同时匹配 `id` / `relay_user_id`。
3. 支持分页和 page size 选择。
4. 列表展示 encrypted `relay_auth_password`，但不展示明文。
5. 支持 admin 对单个用户执行 `Copy plaintext`，通过专用 reveal endpoint 解密并写入剪贴板。
6. 明文只在显式操作时返回，不在列表响应、页面表格或 local storage 中长期存在。
7. 保持第一版只读，不引入用户编辑、角色修改、密码重置或 relay 远端同步。

## Non-Goals

1. 不新增用户创建、删除、角色编辑、auth source 编辑或 relay binding 编辑能力。
2. 不调用 relay 远端 API 拉取用户详情、allowed groups、API keys 或 usage。
3. 不把 relay password 明文放进 `GET /api/v1/admin/users` 列表响应。
4. 不提供浏览器 fallback 明文展示来替代 clipboard copy。
5. 不改变 `users.relay_auth_password` 的存储格式或加密机制。
6. 不改变 LDAP password handling；LDAP bind password 仍不能写入本地 relay credential，也不能转发给 relay。

## Approaches Considered

### Option A: Dedicated Admin Users Page And Admin API

新增 `/admin/users` 页面和 `/api/v1/admin/users` API。列表响应返回本地字段和 encrypted relay credential，明文复制走单独 reveal endpoint。

优点：

1. 用户管理边界清楚，不污染 Settings 页面。
2. 列表查看和明文取用分离，敏感操作可单独权限控制和测试。
3. 后续如果需要审计、重置或 relay sync，可以在同一 admin users surface 内扩展。

缺点：

1. 需要新增后端 handler、前端页面、router entry 和 API 封装。

### Option B: Add Users Section To Settings

在现有 Settings 页面中加入用户列表和 copy plaintext 动作。

优点：

1. 入口少，初始页面改动略小。

缺点：

1. Settings 已经承载 provider、LDAP、deployment 和 credential 管理，继续加入用户敏感数据会让页面职责过杂。
2. 测试和权限边界更容易和现有 settings behavior 纠缠。

### Option C: API Only

只新增 admin API，不新增前端页面。

优点：

1. 实现最快。

缺点：

1. 不满足 admin 可以在产品中查看和复制的体验目标。
2. 容易变成临时调试接口，后续仍需要补 UI。

## Decision

采用 **Option A: Dedicated Admin Users Page And Admin API**。

## Backend API Contract

### List Local Users

```text
GET /api/v1/admin/users?q=alice&page=1&page_size=20
```

Access:

- Requires authenticated user.
- Requires admin role.

Query parameters:

| Parameter | Default | Contract |
| --- | --- | --- |
| `q` | empty | Optional search string. Matches `username` and `email` by contains. If numeric, also matches local `id` and `relay_user_id`. |
| `page` | `1` | 1-based page index. Values less than 1 are treated as 1. |
| `page_size` | `20` | Allowed UI values are `10`, `20`, `50`, `100`; backend caps at `100`. |

Response data:

```json
{
  "items": [
    {
      "id": 1,
      "username": "alice",
      "email": "alice@example.com",
      "role": "user",
      "auth_source": "ldap",
      "relay_user_id": 42,
      "relay_auth_password": "encrypted-ciphertext",
      "created_at": "2026-05-26T00:00:00Z",
      "updated_at": "2026-05-26T00:00:00Z"
    }
  ],
  "total": 123,
  "page": 1,
  "page_size": 20
}
```

Notes:

- `relay_auth_password` is the encrypted ciphertext currently stored in the local DB.
- Missing `relay_auth_password` returns an empty string.
- The handler must not decrypt passwords while serving this list.
- The handler must not call relay remote APIs.

### Reveal Relay Password For Copy

```text
POST /api/v1/admin/users/:id/relay-password/reveal
```

Access:

- Requires authenticated user.
- Requires admin role.

Response data:

```json
{
  "password": "test-password"
}
```

Contract:

1. The handler loads exactly one local user by `id`.
2. It requires a non-empty stored `relay_auth_password`.
3. It decrypts with the current backend `encryption.key`.
4. It returns the plaintext only for this explicit reveal request.
5. It never logs ciphertext or plaintext.

Expected errors:

| Case | Status | Message shape |
| --- | --- | --- |
| Non-admin | `403` | Existing admin middleware response |
| User not found | `404` | User not found |
| Missing stored relay password | `422` | Relay auth password is not stored |
| Missing encryption key | `500` | Relay auth password cannot be decrypted |
| Decrypt failure | `500` | Relay auth password cannot be decrypted |

## Frontend Design

### Route And Entry

- Route: `/admin/users`
- Route meta: `requireAdmin: true`
- Sidebar: show `Users` only for admin users.

### Page Layout

The page is a dense admin table, not a profile or marketing-style page.

Top controls:

1. Search input.
2. Refresh button.
3. Page size selector with `10`, `20`, `50`, `100`.

Search behavior:

- Debounce user input by about 300 ms.
- Reset to page 1 when search text or page size changes.
- Show a loading state while fetching.

Table columns:

1. `ID`
2. `Username`
3. `Email`
4. `Role`
5. `Auth Source`
6. `Relay User ID`
7. `Relay Auth Password`
8. `Created`
9. `Updated`
10. Actions

`Relay Auth Password` column:

- Shows the encrypted ciphertext.
- Long ciphertext may be visually truncated in the middle.
- Provides `Copy encrypted` using the ciphertext already present in the row.

Actions:

- `Copy plaintext` calls `POST /api/v1/admin/users/:id/relay-password/reveal`.
- On success, write `password` directly to `navigator.clipboard.writeText`.
- Show a short row-level status such as `Copied`.
- Do not render the plaintext in the table.
- Do not store plaintext in local storage or Pinia.

Pagination:

- Footer shows total count, current page, and current page size.
- Provide previous and next controls.
- Disable previous on page 1.
- Disable next when the current page already covers `total`.

## Security And Error Handling

1. All `/api/v1/admin/users*` endpoints must use `RequireAuth` and `RequireAdmin`.
2. List responses must not include plaintext.
3. Reveal responses return plaintext only for the one selected user and only after an explicit admin action.
4. Frontend clipboard failures should be shown as an error. The page must not fallback to rendering plaintext for manual copy.
5. Backend logs may include user id and error category, but must not include plaintext or ciphertext.
6. Tests, fixtures, and docs must use sanitized values such as `alice@example.com`, `bob@example.org`, and `test-password`.
7. Existing `users.relay_auth_password` remains an Ent sensitive field and stays encrypted at rest.

## Testing

Backend tests should cover:

1. Admin can list users with default pagination.
2. Admin can search by username and email.
3. Numeric search matches local `id` and `relay_user_id`.
4. Page and page size return the expected slice and total.
5. Non-admin receives `403`.
6. List response includes encrypted `relay_auth_password` but not plaintext.
7. Reveal returns decrypted plaintext for admin.
8. Reveal returns expected errors for missing user, missing stored password, missing encryption key, and decrypt failure.

Frontend tests should cover:

1. Admin users route renders search, page size selector, table rows, and pagination controls.
2. Search changes request params and resets to page 1.
3. Page size and page navigation change request params.
4. `Copy encrypted` writes the row ciphertext to clipboard.
5. `Copy plaintext` calls reveal endpoint and writes returned plaintext to clipboard.
6. Plaintext is not rendered in the DOM after copy.
7. Non-admin route guard redirects away from `/admin/users`.

Recommended verification:

```bash
cd backend && go test ./internal/handler
cd frontend && pnpm test
```

If implementation touches shared auth, router, or generated Ent behavior, broaden backend verification to:

```bash
cd backend && go test ./...
```

## Documentation

Implementation should update `docs/architecture.md` to describe:

1. Admin-only `/admin/users` local user inspection surface.
2. Search and pagination over local users.
3. Encrypted relay password display in list responses.
4. Plaintext relay password reveal only through an explicit copy action.
5. First-version non-goals: no relay remote user details, no user API keys, no usage data, and no user mutation.

Historical specs should not be rewritten to match this feature. If a future change expands this surface into mutation, audit logging, relay sync, or password reset, write a new spec or update the newest active admin users spec rather than backfilling older design history.
