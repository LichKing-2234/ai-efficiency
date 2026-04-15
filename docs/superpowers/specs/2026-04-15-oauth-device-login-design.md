# OAuth Device Login Design

**Status:** Current contract for OAuth device login
**Scope:** `backend/internal/oauth`, `ae-cli/internal/auth`, `ae-cli/cmd`, `frontend/src/views/oauth`, `docs/`
**Related:** [2026-03-24-oauth-cli-login-design.md](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md), [2026-03-26-session-pr-attribution-design.md](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-26-session-pr-attribution-design.md), [docs/architecture.md](/Users/admin/ai-efficiency/docs/architecture.md)

**Implementation Note:** Device Authorization Flow 已在 `backend/internal/oauth`、`ae-cli login --device` 和前端 `/oauth/device` 页面中落地；普通浏览器 PKCE 登录仍是默认路径。

## Context

当前 `ae-cli login` 的主链路依赖：

1. CLI 本机打开浏览器
2. 浏览器跳回本机 `localhost` 回调端口
3. CLI 再用 authorization code 换取 JWT

这条链路在桌面开发机上工作正常，但在 Linux 开发机、远程堡垒机、无 GUI 容器环境中存在明显缺口：

- 没有图形浏览器可打开
- 没有可用的本机回调端口供浏览器回跳
- 用户往往需要在另一台有浏览器的机器上完成授权

因此需要在保留现有浏览器 PKCE 登录的同时，为 `ae-cli` 增加标准的 OAuth 2.0 Device Authorization Flow（RFC 8628）入口。

## Spec Relationship

- 本文扩展 [`2026-03-24-oauth-cli-login-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md) 中的 ae-cli OAuth 登录合同。
- 相比 `2026-03-24` 中“Device Authorization Flow（RFC 8628），后续单独实现”的约束，本文将该后续实现收敛为具体合同。
- `2026-03-24` 中关于 relay/provider、LDAP、浏览器 PKCE 登录、`token.json`、以及 relay API key delivery 的其余内容仍保持有效；仅 device login 相关主题以本文为准。
- 本文现已成为 device login 的当前生效合同；项目级运行时描述同步以 [`docs/architecture.md`](/Users/admin/ai-efficiency/docs/architecture.md) 为准。

## Goals

1. 允许用户在无 GUI、无本机浏览器、无本机回调能力的机器上完成 `ae-cli` 登录
2. 保留当前浏览器 PKCE 登录作为默认主链路，不破坏已有桌面使用方式
3. 支持“CLI 在机器 A，浏览器在机器 B”完成授权
4. 保持 OAuth/JWT/token 存储模型统一，不新增旁路用户名密码登录
5. 将改动范围控制在 OAuth 层、CLI 登录入口和前端授权页，不改变 session bootstrap 主链路

## Non-Goals

1. 不引入单独的 CLI 用户名/密码登录
2. 不引入 `verification_uri_complete` 直达模式；v1 只支持标准 `verification_uri + user_code`
3. 不借此引入新的 OAuth scope 体系或多公共客户端注册体系
4. 不新增数据库表来持久化短生命周期 device login 状态
5. 不改动 `ae-cli start` 的 session bootstrap 契约；登录成功后的 token 消费方式保持现状

## High-Level Flow

### Browser PKCE

`ae-cli login` 默认继续沿用现有浏览器 PKCE 流程：

1. CLI 启动本地回调监听器
2. CLI 打开 `/oauth/authorize`
3. 浏览器完成登录与授权
4. 浏览器回跳本机回调 URI
5. CLI 调用 `/oauth/token` 换取 JWT

### Device Flow

`ae-cli login --device` 走新的 device flow：

1. CLI 调用 `POST /oauth/device/code`
2. 服务端返回 `device_code`、`user_code`、`verification_uri`、`expires_in`、`interval`
3. CLI 在终端打印 `verification_uri` 与 `user_code`
4. 用户在任意有浏览器的机器打开 `verification_uri`
5. 浏览器登录 AI Efficiency Web；若尚未登录，则跳转 `/login`，登录成功后返回 device 授权页
6. 用户在 device 授权页输入 `user_code` 并确认授权
7. CLI 按 `interval` 轮询 `POST /oauth/token` 的 device grant
8. 授权完成后，CLI 收到 access token / refresh token 并写入现有 `~/.ae-cli/token.json`

## CLI Contract

### `ae-cli login`

- 默认行为不变，继续走浏览器 PKCE
- 在 Linux headless 场景下，CLI 不再盲目等待浏览器回调，而是直接提示用户改用 device flow
- v1 的 headless 判定规则保持保守：
  - `runtime.GOOS == "linux"`
  - `DISPLAY` 为空
  - `WAYLAND_DISPLAY` 为空
- 当满足上述条件时，`ae-cli login` 直接失败，并输出明确提示：
  - `No browser environment detected. Use 'ae-cli login --device'.`
- 如果不是 headless，即使自动打开浏览器失败，也仍打印授权 URL 并继续等待回调；因为此类场景通常仍可手工在本机浏览器中打开链接

### `ae-cli login --device`

- 新增 `--device` flag，允许用户在任何环境显式选择 device flow，包括有 GUI 的机器
- CLI 行为：
  1. 调用 `POST /oauth/device/code`
  2. 打印 `verification_uri`
  3. 打印 `user_code`
  4. 打印过期时间与推荐轮询间隔
  5. 进入轮询，直到成功、被拒绝、过期或上下文取消
- CLI 成功后写入现有 `~/.ae-cli/token.json`
- `token.json` 结构不变：
  - `access_token`
  - `refresh_token`
  - `expires_at`
  - `server_url`

### Device Polling Rules

- CLI 使用 `POST /oauth/token`，参数：
  - `grant_type=urn:ietf:params:oauth:grant-type:device_code`
  - `device_code`
  - `client_id=ae-cli`
- 默认服务端返回：
  - `expires_in = 900`
  - `interval = 5`
- CLI 轮询规则：
  - 初始按 `interval` 秒轮询
  - 收到 `authorization_pending` 时继续等待
  - 收到 `slow_down` 时将本地轮询间隔额外增加 5 秒
  - 收到 `access_denied` 时立即失败退出
  - 收到 `expired_token` 时立即失败退出
  - 收到 token 成功响应时立即停止轮询并保存 token

## Backend OAuth Contract

### `POST /oauth/device/code`

为公共客户端 `ae-cli` 签发一组短期 device login 凭证。

请求：

```x-www-form-urlencoded
client_id=ae-cli
```

响应：

```json
{
  "device_code": "high-entropy-secret",
  "user_code": "ABCD-EFGH",
  "verification_uri": "https://server.example.com/oauth/device",
  "expires_in": 900,
  "interval": 5
}
```

约束：

- `client_id` 目前只允许 `ae-cli`
- 不返回 `verification_uri_complete`
- `device_code` 必须是高熵随机值，只用于 CLI 与 token endpoint 之间
- `user_code` 用于人工输入，采用 `ABCD-EFGH` 形式
- `user_code` 比较时大小写不敏感；服务端需先做规范化再匹配

错误：

- 未注册 `client_id` 返回 `invalid_client`

### `POST /oauth/token`

在当前已支持的 `grant_type=authorization_code` 基础上，新增：

```text
grant_type=urn:ietf:params:oauth:grant-type:device_code
```

device grant 请求参数：

```x-www-form-urlencoded
grant_type=urn:ietf:params:oauth:grant-type:device_code
device_code=<device_code>
client_id=ae-cli
```

成功响应沿用当前 token 结构：

```json
{
  "access_token": "jwt-access-token",
  "refresh_token": "jwt-refresh-token",
  "token_type": "Bearer",
  "expires_in": 7200
}
```

失败响应语义：

- `authorization_pending`
  - device code 合法，但浏览器侧尚未完成授权
- `slow_down`
  - CLI 轮询过快，需增加轮询间隔
- `access_denied`
  - 用户在浏览器侧显式拒绝授权
- `expired_token`
  - device code 已过期
- `invalid_grant`
  - device code 未知、已消费，或与 `client_id` 不匹配

device grant 仍复用当前 `tokenGen.GenerateAccessToken(...)` 生成 JWT，不引入新的 token 发行逻辑。

### `GET /oauth/device`

该路由提供浏览器侧的 device 授权页。

行为要求：

- 若请求命中后端嵌入前端页面，且请求来源与 `frontendURL` 同源，则可直接返回嵌入前端入口，行为与当前 `/oauth/authorize` 一致
- 否则跳转到前端路由 `/oauth/device`
- 该页面不依赖本机回调 URL，也不依赖 CLI 所在机器

### `POST /oauth/device/verify`

该接口用于浏览器侧提交 `user_code` 并确认或拒绝授权。

请求要求：

- 必须带当前 Web 登录 JWT
- 请求体包含：
  - `user_code`
  - `approved`

语义：

1. 服务端根据 `user_code` 找到对应 device entry
2. 如果 code 无效、已过期、已消费，返回统一的用户可理解错误
3. 如果 `approved=false`，将 entry 状态标记为 `denied`
4. 如果 `approved=true`，将 entry 状态标记为 `approved`，并记录授权用户身份

响应：

- 成功时返回简单状态，例如 `status: approved` 或 `status: denied`
- 不向浏览器泄露 CLI 侧的 `device_code`
- 不向浏览器泄露多余的终端侧上下文

### Device Entry Model

device login 的短生命周期状态与当前 authorization code 一样，先保存在后端进程内内存中，不落数据库。

建议 entry 字段：

```go
type deviceEntry struct {
    DeviceCode      string
    UserCode        string
    ClientID        string
    Status          string
    UserID          int
    Username        string
    Role            string
    CreatedAt       time.Time
    ExpiresAt       time.Time
    LastPolledAt    time.Time
    PollIntervalSec int
}
```

状态机：

- `pending`
  - CLI 已拿到 code，尚未完成浏览器授权
- `approved`
  - 浏览器用户已确认授权，等待 CLI 下一次成功换 token
- `denied`
  - 浏览器用户拒绝授权
- `expired`
  - 超出 `expires_at`
- `consumed`
  - token 已成功签发，不能重复使用

状态约束：

- 只有 `pending` 可转为 `approved` 或 `denied`
- `approved` 在成功签发 token 后必须立刻转为 `consumed`
- 任一状态超时后都应视为 `expired`
- `consumed` 的 device code 再次轮询必须返回 `invalid_grant`

## Frontend Contract

前端新增 `/oauth/device` 页面，v1 不引入额外的 code lookup/introspect API，而是直接围绕 `POST /oauth/device/verify` 完成交互。

页面职责分为三个阶段：

1. **未登录**
   - 自动跳转到现有 `/login?redirect=...`
   - 登录成功后回到 `/oauth/device`

2. **输入 code**
   - 展示 `user_code` 输入框
   - 说明用户需要从 CLI 终端复制该 code
   - code 输入大小写不敏感

3. **提交决定**
   - 用户输入 `user_code` 后，直接点击“授权”或“拒绝”
   - 页面调用 `POST /oauth/device/verify`
   - 服务端完成 code 校验与状态转换
   - 完成后展示明确结果：
     - `Approved. You can return to the terminal.`
     - `Access denied.`

文案约束：

- 对无效、过期、已消费 code 统一展示可理解但不过度泄露状态细节的错误，例如 `Code invalid or expired`
- 页面不展示 `device_code`
- 页面不展示 CLI 所在主机、回调地址或其他非必要终端细节

## Security Boundaries

- device flow 只扩展 OAuth 授权方式，不引入新的用户名密码入口
- `POST /oauth/device/verify` 继续依赖现有 Web JWT，防止浏览器侧无认证地激活 code
- `device_code` 只在 CLI 与 `/oauth/token` 之间流转，不出现在浏览器页面
- `user_code` 是人工输入码，不应具备单独换 token 的能力
- 不引入 scope，device flow 成功后的权限与当前 ae-cli 登录保持一致
- 不引入新的 public client；`client_id` 仍限定为 `ae-cli`

## Failure and Restart Semantics

- 由于 device entry 为进程内内存态，后端重启后，所有尚未完成的 device login 都会失效
- CLI 在这种情况下会收到 `invalid_grant` 或 `expired_token`，并需要重新发起 `ae-cli login --device`
- 这是 v1 可接受的行为；如果后续出现多实例后端或长时间授权需求，再考虑将 device entry 持久化

## Testing

后端测试补充到 `backend/internal/oauth/handler_test.go`：

- `POST /oauth/device/code` 成功签发 code
- 未知 `client_id` 返回 `invalid_client`
- device grant 在 `pending`、`approved`、`denied`、`expired`、`consumed` 状态下的返回
- 轮询过快触发 `slow_down`
- `user_code` 大小写不敏感
- 已登录用户完成 `/oauth/device/verify` 后，CLI 轮询可拿到 token

CLI 测试补充到 `ae-cli/internal/auth/oauth_test.go` 与 `ae-cli/cmd/login_test.go`：

- `--device` 模式能够拿到 device code 并轮询成功
- `authorization_pending` 继续轮询
- `slow_down` 会增加轮询间隔
- `access_denied` 与 `expired_token` 正确失败
- headless Linux 场景下普通 `login` 给出 `--device` 提示

前端测试补充到 device 页相关测试：

- 未登录时跳转 `/login`
- 无效 code 提示
- 有效 code 的授权/拒绝交互

默认验证命令保持项目约定：

- `cd backend && go test ./...`
- `cd ae-cli && go test ./...`
- `cd frontend && pnpm test`

## Rollout Notes

- 本 spec 只定义 device flow 的新增合同，不改变现有浏览器 PKCE 登录的默认路径
- 文档实现顺序：
  1. 新增本文档
  2. 代码实现落地
  3. 代码落地后再更新 [`docs/architecture.md`](/Users/admin/ai-efficiency/docs/architecture.md) 中的当前运行时描述
