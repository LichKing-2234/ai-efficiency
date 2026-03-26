# OAuth CLI Login & Auth System Enhancement Design

## Overview

为 ae-cli 实现 OAuth 授权登录流程，打通 relay server（当前为 sub2api）SSO 认证，新增 LDAP 前端管理界面。使 ae-cli 用户无需手动配置 token，通过浏览器授权即可完成登录。引入 Relay Provider 抽象层，统一封装所有 relay server 交互（认证、LLM 调用、用量查询、API key 管理），移除 sub2api 数据库直连，支持多个独立 relay provider 实例（每个即一个 LLM 端点），API key 和 base URL 通过 server 自动下发。

本 spec 聚焦后端 Relay Provider 抽象、OAuth 登录、API Key 下发。ae-cli 侧的工具自动发现、provider-工具映射、以及各工具原生配置文件写入，由独立的 **Spec 2: ae-cli 智能工具发现与自动配置** 处理。

**Current Alignment Note (2026-03-26):**
- relay/OAuth/provider delivery 仍然是当前系统的基础设计来源。
- 但本文中关于用户身份、session API key 生命周期、PR 精确归因的部分，已经被 `2026-03-26-session-pr-attribution-design.md` 进一步细化。
- 当前代码仍主要落在本文这版合同上；后续如实现 username 主键、session bootstrap、session-bound primary key，应以 2026-03-26 设计为准，并同步更新本文。

## Scope

1. **Relay Provider 抽象层** — 定义统一接口封装 relay server 交互，移除 sub2api 数据库直连
2. **OAuth2 授权服务器** — 轻量自定义实现，支持 Authorization Code Flow with PKCE
3. **Relay SSO 打通** — 通过 Relay Provider 接口调用 relay server 的 login API 实现认证
4. **ae-cli login/logout 命令** — 本地回调 OAuth 登录，token 存储到独立文件，server URL 编译时固化
5. **前端 OAuth 授权页** — 新建独立授权页面
6. **LDAP 前端管理界面** — 管理员配置 LDAP 参数
7. **Relay Provider 配置与 API Key 下发** — 后端管理多 relay provider，自动为用户查询/创建 API key 并下发给 ae-cli
8. **Session 优化** — start 自动触发 login，session 自动关联用户

不包含：
- Device Authorization Flow（RFC 8628），后续单独实现
- ae-cli 工具自动发现与原生配置写入（见 Spec 2）

## Technical Decisions

| 决策 | 选择 | 理由 |
|------|------|------|
| Relay Server 抽象 | `relay.Provider` 接口 | 统一封装所有 relay server 交互，便于替换不同实现（sub2api → 其他） |
| sub2api 数据库直连 | 移除，全部走 REST API | sub2api 已有完整 REST API（用量、API key、用户），无需维护 DB 直连 |
| OAuth2 server 实现 | 轻量自定义实现（早期评估过 go-oauth2/oauth2） | 与当前 Gin/JWT 代码路径更一致，减少额外抽象层 |
| PKCE 验证 | 自建（~50 行代码） | go-oauth2/oauth2 不原生支持 PKCE server 端验证，但逻辑简单（SHA256 + base64url 比对） |
| ae-cli OAuth client | golang.org/x/oauth2 | Go 官方库，原生支持 PKCE client 端 |
| Relay SSO | 通过 relay.Provider 接口调用 | 不直接读密码哈希，解耦且安全（relay server 有自己的安全机制） |
| LLM 调用 | ae-cli 直接调 provider（不走后端代理） | 用户用自己的 API key，用量天然按用户区分；延迟更低 |
| API key 管理 | 后端自动查询/创建并下发 | 用户无需手动配置，登录后自动获取 |
| 多 provider 支持 | 后端管理 RelayProvider 列表 | 管理员可配置多个独立 relay provider 实例，每个即一个 LLM 端点 |
| Token 存储 | ~/.ae-cli/token.json (0600) | 和 config 分离，不易误提交，业界主流做法（gh、gcloud 等） |

## 1. Relay Provider 抽象层

### 设计目标

将所有 relay server（当前为 sub2api）的交互统一封装到 `relay.Provider` 接口中，使后端各模块（SSO 认证、LLM 分析、用量查询、AI 代理）不直接依赖具体的 relay server 实现。后续替换 relay server 时只需实现新的 adapter。

### 接口定义

```go
// backend/internal/relay/provider.go

// 哨兵错误
var (
    // ErrInvalidCredentials 表示凭据错误（用户名或密码不正确）。
    // 调用方收到此错误应 fallthrough 到下一个认证 provider。
    ErrInvalidCredentials = errors.New("relay: invalid credentials")

    // ErrExtraVerificationRequired 表示 relay server 要求额外验证（如 TOTP、Turnstile）。
    // v1 不支持中继，调用方应 fallthrough。
    ErrExtraVerificationRequired = errors.New("relay: extra verification required")
)

// Provider 定义 relay server 的统一能力接口。
// 所有与 relay server 的交互都通过此接口，便于替换不同实现。
type Provider interface {
    // --- 健康检查 ---

    // Ping 检查 relay server 是否可达，用于启动验证和健康检查。
    Ping(ctx context.Context) error

    // Name 返回此 provider 实例的标识名称，用于日志和错误信息。
    Name() string

    // --- 认证 ---

    // Authenticate 通过 relay server 验证用户凭据。
    // 成功返回 User；凭据错误返回 ErrInvalidCredentials；
    // 需要额外验证返回 ErrExtraVerificationRequired；服务异常返回其他 error。
    Authenticate(ctx context.Context, username, password string) (*User, error)

    // GetUser 通过 relay server 用户 ID 获取用户信息。
    GetUser(ctx context.Context, userID int64) (*User, error)

    // FindUserByEmail 通过邮箱查找 relay server 用户。
    // 用于 LDAP 登录的用户关联 relay server 账号（如查询用量）。
    // 未找到返回 nil, nil。
    FindUserByEmail(ctx context.Context, email string) (*User, error)

    // --- LLM ---

    // ChatCompletion 发送对话请求到 relay server 的 LLM 端点。
    ChatCompletion(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error)

    // ChatCompletionWithTools 发送带工具定义的对话请求。
    ChatCompletionWithTools(ctx context.Context, req ChatCompletionRequest, tools []ToolDef) (*ChatCompletionWithToolsResponse, error)

    // --- 用量 ---

    // GetUsageStats 查询指定 relay 用户在时间范围内的用量统计。
    // 通过 admin API key 调用 relay server 的管理端点。
    // 仅对有 relay 账号的用户可用，LDAP-only 用户无用量数据。
    GetUsageStats(ctx context.Context, userID int64, from, to time.Time) (*UsageStats, error)

    // ListUserAPIKeys 查询指定 relay 用户的 API key 列表。
    // 通过 admin API key 调用 relay server 的管理端点。
    // 调用方可按 ID 过滤获取特定 key。
    ListUserAPIKeys(ctx context.Context, userID int64) ([]APIKey, error)

    // CreateUserAPIKey 为指定 relay 用户创建 API key。
    // keyName 用于标识 key 用途，如 "ae-cli-auto"。
    CreateUserAPIKey(ctx context.Context, userID int64, keyName string) (*APIKeyWithSecret, error)
}
```

### 数据类型

```go
// backend/internal/relay/types.go

type User struct {
    ID       int64  `json:"id"`
    Email    string `json:"email"`
    Username string `json:"username"`
    Role     string `json:"role"` // 可能为空（如 Authenticate 返回时）
}

type APIKey struct {
    ID     int64  `json:"id"`
    UserID int64  `json:"user_id"`
    Name   string `json:"name"`
    Status string `json:"status"`
}

type UsageStats struct {
    TotalTokens int64   `json:"total_tokens"`
    TotalCost   float64 `json:"total_cost"`
}

type ChatCompletionRequest struct {
    Model       string        `json:"model"`
    Messages    []ChatMessage `json:"messages"`
    Temperature *float64      `json:"temperature,omitempty"` // 可选，控制生成随机性
    MaxTokens   *int          `json:"max_tokens,omitempty"` // 可选，限制输出长度
}

type ChatMessage struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}

type ChatCompletionResponse struct {
    Content    string `json:"content"`
    TokensUsed int    `json:"tokens_used"`
}

type ToolDef struct {
    Type     string      `json:"type"`
    Function ToolFuncDef `json:"function"`
}

type ToolFuncDef struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Parameters  json.RawMessage `json:"parameters"`
}

type ToolCall struct {
    ID       string `json:"id"`
    Type     string `json:"type"`
    Function struct {
        Name      string `json:"name"`
        Arguments string `json:"arguments"`
    } `json:"function"`
}

type ChatCompletionWithToolsResponse struct {
    Content    string     `json:"content"`
    ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
    TokensUsed int        `json:"tokens_used"`
}
```

### Sub2api 实现

`sub2apiRelay` 作为第一个实现，全部通过 sub2api REST API 交互：

```go
// backend/internal/relay/sub2api.go

type sub2apiRelay struct {
    client *http.Client
    apiURL string // sub2api REST API 地址，如 http://localhost:3000
    apiKey string // sub2api API key（用于 LLM 端点的 Bearer token）
    model  string // 默认模型
    logger *zap.Logger
}

// baseURL: LLM API 端点地址（如 http://localhost:3000/v1）
// adminURL: 管理 API 地址（如 http://localhost:3000），用于用户管理和 API key 操作
// 对于 sub2api，两者通常指向同一 server 的不同路径前缀
func NewSub2apiProvider(httpClient *http.Client, baseURL, adminURL, apiKey, model string, logger *zap.Logger) Provider {
    return &sub2apiRelay{...}
}
```

各方法对应的 sub2api REST API：

| Provider 方法 | sub2api 端点 | 认证方式 | 说明 |
|--------------|-------------|---------|------|
| `Ping` | `GET /health` | 无 | 健康检查 |
| `Authenticate` | `POST /api/v1/auth/login` + `GET /api/v1/auth/me` | 用户凭据 → session token | 401 → `ErrInvalidCredentials`；`requires_2fa` → `ErrExtraVerificationRequired` |
| `GetUser` | `GET /api/v1/admin/users/:id` | admin API key | — |
| `FindUserByEmail` | `GET /api/v1/admin/users?email=xxx` | admin API key | 用于 LDAP 用户关联 relay 账号 |
| `ChatCompletion` | `POST /v1/chat/completions` | Bearer API key | — |
| `ChatCompletionWithTools` | `POST /v1/chat/completions`（带 tools 参数） | Bearer API key | — |
| `GetUsageStats` | `GET /api/v1/admin/users/:id/usage?from=xxx&to=xxx` | admin API key | 通过 admin 端点按用户 ID 查询，需传 from/to 时间参数 |
| `ListUserAPIKeys` | `GET /api/v1/admin/users/:id/api-keys` | admin API key | 通过 admin 端点查询，不依赖用户 session |
| `CreateUserAPIKey` | `POST /api/v1/keys`（以用户身份）或 admin 端点 | admin API key | 为指定用户创建 API key |

### 移除 sub2apidb 包

- 删除 `backend/internal/sub2apidb/` 整个包
- 移除 `config.Sub2apiDB`（`sub2api_db.dsn`）配置
- 移除 `main.go` 中的 sub2api 数据库连接初始化
- 移除 `go.mod` 中的 `github.com/Masterminds/squirrel` 依赖（如无其他使用方）

### 配置变更

```yaml
# 旧配置（移除）
sub2api_db:
  dsn: "..."

# 新配置（合并）
relay:
  provider: "sub2api"              # relay server 类型，便于后续扩展
  url: "http://localhost:3000"     # relay server REST API 地址
  api_key: ""                      # relay server API key（用于 LLM 和管理端点）
  model: "claude-sonnet-4-20250514" # 默认 LLM 模型
```

对应 config struct 变更：

```go
type Config struct {
    Server     ServerConfig     `mapstructure:"server"`
    DB         DBConfig         `mapstructure:"db"`
    // Sub2apiDB 移除
    Auth       AuthConfig       `mapstructure:"auth"`
    Encryption EncryptionConfig `mapstructure:"encryption"`
    Analysis   AnalysisConfig   `mapstructure:"analysis"`
    Relay      RelayConfig      `mapstructure:"relay"`       // 新增
}

type RelayConfig struct {
    Provider string `mapstructure:"provider"` // "sub2api" 或后续其他实现
    URL      string `mapstructure:"url"`
    APIKey   string `mapstructure:"api_key"`
    Model    string `mapstructure:"model"`
}
```

### Provider 实例化策略

Section 1 的 `relay.*` 配置用于创建后端的"主 provider"实例（用于 SSO 认证、LLM 分析等后端内部功能）。

Section 7 的 `RelayProvider` Ent 记录用于为每个用户下发 API key。后端为每个 `enabled=true` 的 RelayProvider Ent 记录调用 `NewSub2apiProvider()`，使用该记录的 `admin_url` + `admin_api_key` 创建独立的 Provider 实例。

`is_primary=true` 的 RelayProvider Ent 记录应与 Section 1 的 `relay.*` 配置指向同一个 relay server。后续可考虑移除 `relay.*` 配置，完全从数据库加载。

### 消费方改造

| 消费方 | 旧依赖 | 新依赖 |
|--------|--------|--------|
| `auth.SSOProvider` | `*sub2apidb.Client` | `relay.Provider`（用 `Authenticate`，返回 `*User`） |
| `llm.Analyzer` | 直接 HTTP 调用 sub2api | `relay.Provider`（用 `ChatCompletion`、`ChatCompletionWithTools`） |
| `efficiency.Labeler` | `*sub2apidb.Client` | `relay.Provider`（用 `GetUsageStats`、`GetAPIKey`、`FindUserByEmail`） |
| AI 代理 handler | 新增 | `relay.Provider`（用 `ChatCompletion`） |
| `handler.Session` | 存 `provider_name` / `relay_api_key_id` / `tool_configs` | 通过当前 session handler 合同与后续 attribution 设计衔接 |

`llm.Analyzer` 内部的 `Sub2apiURL`、`Sub2apiAPIKey` 等配置字段移除，改为注入 `relay.Provider`。`Analyzer` 保留 prompt 管理、结果解析等逻辑，LLM 调用委托给 Provider。

### 新增包结构

```
backend/internal/relay/
├── provider.go     # Provider 接口定义
├── types.go        # 共享数据类型
└── sub2api.go      # sub2api 实现
```

## 2. OAuth2 Authorization Server（后端）

### 新增端点

| Method | Path | 说明 |
|--------|------|------|
| GET | `/oauth/authorize` | 授权端点，校验参数后 302 重定向到前端页面 `{frontend_url}/oauth/authorize?{原始query参数}` |
| POST | `/oauth/authorize/approve` | 用户授权确认端点（前端调用，需要用户 JWT） |
| POST | `/oauth/token` | Token 交换端点（authorization code → JWT） |

### 前端 URL 配置

OAuth 授权端点需要知道前端地址以进行重定向。新增配置：

```yaml
server:
  frontend_url: "http://localhost:5173"  # 前端地址，用于 OAuth 重定向
```

对应 `ServerConfig` 新增 `FrontendURL string` 字段。

### Client 管理

ae-cli 作为预注册的 public client：
- `client_id`: `ae-cli`
- 无 `client_secret`（public client）
- 允许的 `redirect_uri`：使用 URL 解析（非字符串前缀匹配），host 必须为 `localhost` 或 `127.0.0.1`（两者均接受，规范化后统一比较），port 为纯数字，path 为 `/callback`。防止 `http://localhost.evil.com` 绕过

初期 client 信息硬编码，后续可扩展为 Ent 表存储。

### Authorization Code 生命周期

- 有效期：5 分钟
- 一次性使用
- 绑定 `redirect_uri` + `code_challenge`
- 存储在后端自定义的内存 authorization code store 中
- 已知限制：内存 store 在后端重启时丢失，用户需重新授权。v1 可接受，后续可迁移到 Redis/DB 持久化

### PKCE 验证（自建）

在 `/oauth/token` 端点中：
1. 从 authorization code 关联数据中取出 `code_challenge`
2. 对请求中的 `code_verifier` 做 `base64url(SHA256(code_verifier))`
3. 比对结果，不匹配则返回 `invalid_grant`

### Token 生成

当前实现不依赖外部 OAuth server 框架，而是在 `/oauth/token` handler 中直接桥接到现有 JWT 生成逻辑：

- `/oauth/token` 端点内部委托给 `auth.Service.GenerateTokenPair()`
- 这样 `/oauth/token` 端点产出的 token 格式与 `/auth/login` 完全一致
- Access token: JWT, 2h TTL
- Refresh token: JWT, 7d TTL
- 现有 `RequireAuth()` 中间件无需改动

### 授权确认端点

`POST /oauth/authorize/approve`（需要用户 JWT 认证）：

请求体：
```json
{
  "client_id": "ae-cli",
  "redirect_uri": "http://localhost:18234/callback",
  "code_challenge": "...",
  "code_challenge_method": "S256",
  "state": "...",
  "approved": true
}
```

流程：
1. 从 JWT 中提取当前用户信息
2. 验证 `client_id` 是否为已注册的 client
3. 重新验证 `redirect_uri`（同 `GET /oauth/authorize` 的校验规则：URL 解析后 host 为 `localhost` 或 `127.0.0.1`，port 为纯数字，path 为 `/callback`，两者均接受并规范化后统一比较），防止攻击者直接 POST 恶意 redirect_uri
4. 如果 `approved=true`，生成 authorization code，返回 `{ "redirect_uri": "http://localhost:18234/callback?code=xxx&state=xxx" }`
5. 如果 `approved=false`，返回 `{ "redirect_uri": "http://localhost:18234/callback?error=access_denied&state=xxx" }`
6. 前端收到响应后执行 `window.location.href` 重定向

### 新增包结构

```
backend/internal/oauth/
├── server.go       # 轻量 OAuth server 配置与 client 校验
├── handler.go      # /oauth/authorize, /oauth/token handler
└── pkce.go         # PKCE code_challenge 验证逻辑
```

### 路由挂载

OAuth 端点挂载在根路径 `/oauth/*`，不在 `/api/v1` 下（遵循 OAuth2 惯例）：

```go
// router.go
// 公开端点（无需认证）
r.GET("/oauth/authorize", oauthHandler.Authorize)
r.POST("/oauth/token", oauthHandler.Token)

// 需要用户 JWT 认证
authGroup := r.Group("/oauth")
authGroup.Use(middleware.RequireAuth())
authGroup.POST("/authorize/approve", oauthHandler.Approve)
```

CORS 配置需要包含 `/oauth/*` 路径。

## 3. Relay SSO 打通

### 实现方式

改造 `backend/internal/auth/sso.go`，通过 `relay.Provider` 接口实现认证：

```
SSOProvider.Authenticate(username, password)
  → 委托给 relay.Provider.Authenticate(ctx, username, password)
  → 成功: 返回 UserInfo{ username, email, auth_source: "relay_sso", relay_user_id }
```

SSO provider 不再关心底层是 sub2api 还是其他 relay server。

### 构造函数变更

```go
// 旧签名
func NewSSOProvider(client *sub2apidb.Client, logger *zap.Logger) *SSOProvider

// 新签名
func NewSSOProvider(relayProvider relay.Provider, logger *zap.Logger) *SSOProvider
```

`main.go` 中注册 SSO provider 的条件从"sub2apiClient 可用"改为"relay.Provider 可用"（即 `relay.url` 已配置）。

### 错误处理

- relay server 返回凭据错误：`relay.Provider.Authenticate` 返回 `ErrInvalidCredentials`，SSO provider fallthrough 到 LDAP
- relay server 不可用（网络错误等）：`relay.Provider.Authenticate` 返回其他 error，SSO provider 记录 warn 日志，fallthrough 到 LDAP
- relay server 要求额外验证（如 Turnstile、TOTP）：`relay.Provider.Authenticate` 返回 `ErrExtraVerificationRequired`，SSO provider fallthrough 到 LDAP。后续可在 OAuth 授权页中增加额外验证输入框

### Provider Chain

认证顺序不变：Relay SSO → LDAP → dev login

SSO provider 仅在 `relay.Provider` 可用时注册。

## 4. ae-cli OAuth 登录流程

### 新增命令

**`ae-cli login`**

如果 `~/.ae-cli/token.json` 已存在且 token 有效，提示用户已登录并显示当前用户名。用户可通过 `ae-cli login --force` 强制重新登录。

1. 生成 PKCE `code_verifier` + `code_challenge`（使用 `golang.org/x/oauth2`）
2. 使用 `net.Listen("tcp", "localhost:0")` 启动本地临时 HTTP server（OS 分配随机可用端口，避免端口冲突）
3. 打开浏览器访问：
   ```
   {server_url}/oauth/authorize?
     response_type=code&
     client_id=ae-cli&
     redirect_uri=http://localhost:{port}/callback&
     code_challenge={challenge}&
     code_challenge_method=S256&
     state={random}
   ```
4. 等待浏览器回调 `localhost:{port}/callback?code=xxx&state=xxx`（超时 3 分钟，超时后输出错误信息并退出）
5. 验证 state 参数
6. 用 code + code_verifier 调用 `POST {server_url}/oauth/token` 换取 token
7. 写入 `~/.ae-cli/token.json`（权限 0600）
8. 关闭本地 HTTP server，输出登录成功信息

**`ae-cli logout`**

清除 `~/.ae-cli/token.json`。

### Token 文件格式

```json
{
  "access_token": "eyJ...",
  "refresh_token": "eyJ...",
  "expires_at": "2026-03-24T19:36:00Z",
  "server_url": "http://localhost:8081"
}
```

路径：`~/.ae-cli/token.json`，权限 `0600`。

### Token 自动刷新

ae-cli HTTP client 每次请求前检查 `expires_at`：
- 距过期 < 5 分钟：自动用 refresh_token 调用 `POST /api/v1/auth/refresh`（JSON body: `{"refresh_token": "..."}`）刷新
- 注意：现有 refresh 端点使用 JSON body 而非 OAuth2 标准的 form POST，ae-cli 直接调用此端点而不走 golang.org/x/oauth2 的 refresh 逻辑
- 刷新成功：原子写入更新 token.json（先写临时文件再 rename，防止进程中断导致文件损坏）
- 刷新失败：提示用户重新 `ae-cli login`

注意：ae-cli 使用两种不同的 HTTP 调用约定：
- 初始 token 交换：`POST /oauth/token`，`application/x-www-form-urlencoded`（OAuth2 标准）
- 后续 token 刷新：`POST /api/v1/auth/refresh`，`application/json`（现有端点）

选择复用现有 refresh 端点而非 `/oauth/token` 的 `grant_type=refresh_token`，是因为现有端点已被前端使用且经过验证，避免引入新的 token 刷新路径。

### Token 文件中的 server_url

`token.json` 中的 `server_url` 记录登录时使用的 server 地址，用于 token 刷新。如果与编译时固化的 server URL 不一致（如切换了环境），ae-cli 提示用户重新 `login`。

### 新增包结构

```
ae-cli/
├── cmd/
│   ├── login.go      # login 命令
│   └── logout.go     # logout 命令
└── internal/
    ├── auth/
    │   ├── oauth.go   # OAuth 登录流程（本地 server、浏览器打开、code 交换）
    │   └── token.go   # token 文件读写、自动刷新
    └── client/
        └── client.go  # 改造：使用编译时 server URL + token.json，支持自动刷新
```

### ae-cli 配置简化

#### server.url 编译时固化

`server.url` 通过 `go build -ldflags` 在编译时注入，不再需要用户配置：

```go
// ae-cli/internal/buildinfo/buildinfo.go
var (
    ServerURL = "http://localhost:8081" // 默认值，编译时覆盖
    Version   = "dev"
)
```

构建命令：
```bash
go build -ldflags "-X ae-cli/internal/buildinfo.ServerURL=https://ae.example.com" ./cmd/ae-cli
```

#### 移除 sub2api 客户端配置

ae-cli 不再需要用户手动配置 relay provider 相关参数：

- 移除 `Sub2apiConfig` struct（`url`、`api_key_env`、`model`）
- 移除 `ServerConfig.Token`（改用 `token.json`）
- API key 按用户隔离，用量天然按用户区分

#### 移除 config.yaml

OAuth 后 ae-cli 不再需要 `~/.ae-cli/config.yaml`：

```yaml
# 旧配置（全部移除）
server:
  url: "http://localhost:8081"       # → 编译时固化
  token: "jwt-token-here"           # → token.json
sub2api:
  url: "http://..."                 # → 后端自动下发
  api_key: "sk-..."                 # → 后端自动下发
  model: "claude-sonnet-4-20250514" # → 后端自动下发
tools:
  claude:
    command: claude                  # → Spec 2: LLM 自动发现
    args: ["-p"]
```

ae-cli 只需要：
- 编译时注入的 server URL
- `~/.ae-cli/token.json`（OAuth 登录后自动生成）

Provider 配置（base URL + API key）通过 `GET /api/v1/providers` 自动获取（见 Section 7）。工具发现和原生配置文件写入由独立的 **Spec 2: ae-cli 智能工具发现与自动配置** 处理。

向后兼容：如果检测到旧 `config.yaml`，打印 deprecation warning 并忽略。

## 5. 前端 OAuth 授权页

### 路由

`/oauth/authorize` — 新建独立页面，不复用现有登录页。路由配置需要 `meta: { public: true }`，绕过现有的 auth guard（否则未登录用户会被重定向到 `/login`）。页面内部自行处理认证状态。

### 页面逻辑

1. 从 URL query 读取 `client_id`、`redirect_uri`、`code_challenge`、`code_challenge_method`、`state`
2. 检查用户是否已登录（localStorage 中有有效 token，调用 `/auth/me` 验证）
3. 未登录：显示登录表单（支持 LDAP 和 sub2api SSO 两种方式）
4. 已登录：直接显示授权确认页面

### 授权确认页面

- 标题："ae-cli 请求访问你的账号"
- v1 不实现 scope 机制，授权即获得完整访问权限，页面不显示权限范围
- 两个按钮："授权" / "拒绝"
- 授权：调用后端 `POST /oauth/authorize/approve`（带用户 JWT），后端返回包含 code 的 redirect URL，前端执行 `window.location.href` 重定向到 `redirect_uri?code=xxx&state=xxx`
- 拒绝：前端直接重定向到 `redirect_uri?error=access_denied&state=xxx`

### 新增文件

```
frontend/src/
├── views/oauth/
│   └── AuthorizePage.vue    # OAuth 授权页面
├── api/
│   └── oauth.ts             # OAuth 相关 API 调用
└── router/
    └── index.ts             # 新增 /oauth/authorize 路由
```

## 6. LDAP 前端管理界面

### 后端端点

| Method | Path | 说明 | 权限 |
|--------|------|------|------|
| GET | `/api/v1/admin/settings/ldap` | 获取 LDAP 配置（密码脱敏） | admin |
| PUT | `/api/v1/admin/settings/ldap` | 更新 LDAP 配置 | admin |
| POST | `/api/v1/admin/settings/ldap/test` | 测试 LDAP 连接 | admin |

### 配置持久化

新建 Ent schema `SystemSetting`：
- `key` (string, unique) — 配置键，如 `ldap.url`、`ldap.base_dn`
- `value` (text) — 配置值（敏感字段加密存储）
- `updated_at` (timestamp)

后端启动时从数据库加载配置，覆盖 config.yaml 中的默认值。

### 配置热加载

PUT 接口保存配置后，需要热更新运行中的 `LDAPProvider`：
- `LDAPProvider` 改造为接受 `*atomic.Pointer[config.LDAPConfig]`，而非直接持有 `config.LDAPConfig` struct
- PUT handler 保存到数据库后，更新 atomic pointer
- 下次认证请求自动使用新配置，无需重启

### 敏感字段加密

LDAP `bind_password` 使用 AES-256-GCM 加密存储：
- 加密密钥：使用已有的 `encryption.key` 配置（32 字节 AES-256 密钥），不从 jwt_secret 派生，避免 JWT secret 轮换导致已存储密码不可解密
- GET 接口返回时密码字段显示为 `"***"`，不返回密文

### LDAP 配置项

| 字段 | 类型 | 说明 |
|------|------|------|
| url | string | LDAP 服务器地址，如 `ldap://ldap.example.com:389` |
| base_dn | string | 搜索基础 DN，如 `dc=example,dc=com` |
| bind_dn | string | 绑定 DN |
| bind_password | string | 绑定密码（加密存储，前端脱敏显示） |
| user_filter | string | 用户搜索过滤器，如 `(uid=%s)` |
| tls | bool | 是否启用 TLS |

### 前端页面

管理后台新增 LDAP 配置页面（仅管理员可见）：
- 表单展示所有配置项
- 密码字段脱敏显示（`***`），编辑时可输入新值
- "测试连接"按钮 — 调用 `/api/v1/admin/settings/ldap/test`，使用请求体中的配置（而非已保存的配置）进行测试，允许用户在保存前验证配置是否可用
- "保存"按钮 — 调用 PUT 接口保存配置

### 新增文件

```
frontend/src/views/admin/
└── LdapSettingsView.vue     # LDAP 配置管理页面

backend/
├── ent/schema/
│   └── system_setting.go    # SystemSetting schema
└── internal/handler/
    └── admin_settings.go    # LDAP 配置管理 handler
```

## 7. Relay Provider 配置与 API Key 下发

ae-cli 直接调用 relay provider（不走后端代理），支持多个独立 relay provider 实例（如 sub2api-claude、sub2api-codex 等）。每个 relay provider 就是一个 LLM 端点，API key 和 base URL 通过后端自动下发，用户无需手动配置。

### 设计思路

```
管理员在后端配置可用的 relay provider（名称、base URL、默认模型等）
→ 用户 OAuth 登录后，ae-cli 调后端获取 provider 列表
→ 后端为用户在各 provider 查询/创建 API key
→ 下发给 ae-cli（provider 列表 + 各自的 base URL 和 API key）
→ ae-cli 直接调各 provider 的 LLM 端点
→ 用量自然按用户的 API key 区分
```

### 后端端点

| Method | Path | 说明 | 权限 |
|--------|------|------|------|
| GET | `/api/v1/providers` | 获取当前用户可用的 provider 列表（含 API key） | auth |
| GET | `/api/v1/admin/providers` | 管理员获取所有 provider 配置 | admin |
| POST | `/api/v1/admin/providers` | 管理员新增 provider | admin |
| PUT | `/api/v1/admin/providers/:id` | 管理员更新 provider | admin |
| DELETE | `/api/v1/admin/providers/:id` | 管理员删除 provider | admin |

### Provider 配置（管理员管理）

新建 Ent schema `RelayProvider`：

| 字段 | 类型 | 说明 |
|------|------|------|
| name | string, unique | provider 名称，如 `sub2api-claude`、`sub2api-codex` |
| display_name | string | 显示名称，如 "Claude (Sub2API)"、"Codex (Sub2API)" |
| base_url | string | LLM API 地址，如 `http://localhost:3000/v1` |
| admin_url | string | relay server 管理 API 地址（用于查询/创建 API key），可与 base_url 不同 |
| relay_type | string | 对应的 relay.Provider 实现类型，如 `sub2api` |
| admin_api_key | string | relay server 管理 API key（加密存储，用于后端代为创建用户 key） |
| default_model | string | 默认模型，如 `claude-sonnet-4-20250514` |
| is_primary | bool | 是否为主 provider（主 provider 用于 SSO 认证和用量统计），仅一个为 true |
| enabled | bool | 是否启用 |

### API Key 下发流程（`GET /api/v1/providers`）

1. 从 JWT 中提取当前用户信息
2. 查询所有 `enabled=true` 的 RelayProvider
3. 对每个 provider，通过 `relay.Provider` 接口查询该用户是否已有 API key：
   - 有：直接返回
   - 没有：通过 `relay.Provider` 自动创建一个，关联到该用户
4. 返回 provider 列表：

```json
{
  "providers": [
    {
      "name": "sub2api-claude",
      "display_name": "Claude (Sub2API)",
      "base_url": "http://localhost:3000/v1",
      "api_key": "sk-user-xxx",
      "default_model": "claude-sonnet-4-20250514",
      "is_primary": true
    },
    {
      "name": "sub2api-codex",
      "display_name": "Codex (Sub2API)",
      "base_url": "http://localhost:3000/v1",
      "api_key": "sk-user-yyy",
      "default_model": "codex-latest",
      "is_primary": false
    }
  ]
}
```

### LDAP-only 用户处理

`GET /api/v1/providers` 流程中，后端需要 relay user ID 来查询/创建 API key。用户的 `relay_user_id` 来源：

- Relay SSO 用户：登录时已关联，直接使用
- LDAP 用户：通过 `relay.Provider.FindUserByEmail(email)` 尝试关联。如果找到匹配的 relay 账号，将 `relay_user_id` 存入 Ent User 记录
- LDAP-only 用户（无 relay 账号）：无法获取 API key。`GET /api/v1/providers` 返回空列表 `{"providers": []}`。ae-cli 收到空列表后提示用户："当前账号未关联 relay server，无法自动配置 AI 工具。请联系管理员。"

### relay.Provider 接口扩展

`ListUserAPIKeys` 和 `CreateUserAPIKey` 已定义在 Section 1 的 `relay.Provider` 基础接口中（见 Section 1 接口定义）。此处仅补充创建时返回的类型：

```go
// APIKeyWithSecret 包含完整的 API key（仅在创建时返回）。
type APIKeyWithSecret struct {
    APIKey
    Secret string `json:"secret"` // 完整的 API key 值，如 "sk-xxx"
}
```

sub2api 实现对应的端点见 Section 1 的 sub2api 端点映射表。

### ae-cli 侧

OAuth 登录成功后，ae-cli 自动调用 `GET /api/v1/providers` 获取 provider 列表（含 API key），缓存到本地。具体的工具发现、provider-工具映射、以及各工具原生配置文件的写入，由独立的 **Spec 2: ae-cli 智能工具发现与自动配置** 处理。

### Session 用量关联

> **Alignment note:** 本节描述的是 2026-03-24 版本的 session/provider 上报合同。若实现 session bootstrap、workspace marker、session-bound primary API key，请优先参考 `2026-03-26-session-pr-attribution-design.md`，并将本文视为旧版本基线。

Session 创建时，ae-cli 将各工具使用的 provider 和 API key 信息传给后端。`tool_configs` 数组支持多工具上报（见 Spec 2）。如果 ae-cli 尚未执行工具发现（Spec 2 未实现时），可传单个 provider 信息作为 fallback。

```json
{
  "id": "...",
  "repo_full_name": "...",
  "branch": "...",
  "tool_configs": [
    {
      "tool_name": "claude",
      "provider_name": "sub2api-claude",
      "relay_api_key_id": 123
    }
  ]
}
```

Session schema 字段泛化：

| 旧字段 | 新字段 | 说明 |
|--------|--------|------|
| `sub2api_user_id` | `relay_user_id` | relay server 用户 ID |
| `sub2api_api_key_id` | `relay_api_key_id` | relay server API key ID |
| — | `provider_name` | 使用的 provider 名称 |
| — | `tool_configs` | 各工具的 provider 和 API key 映射（JSON 数组） |

Labeler 通过 `provider_name` + `relay_api_key_id` + `relay.Provider` 查询用量，链路完整。

### User schema 泛化

| 旧字段 | 新字段 | 说明 |
|--------|--------|------|
| `sub2api_user_id` | `relay_user_id` | relay server 用户 ID |
| `auth_source: "sub2api_sso"` | `auth_source: "relay_sso"` | 认证来源 |

### 数据库迁移

- 先将 `"relay_sso"` 作为新 enum 值添加，不移除 `"sub2api_sso"`
- 运行迁移：`ALTER TABLE users ADD CONSTRAINT ... CHECK (auth_source IN ('sub2api_sso', 'relay_sso', 'ldap'))`
- 迁移已有数据：`UPDATE users SET auth_source = 'relay_sso' WHERE auth_source = 'sub2api_sso'`
- 迁移验证通过后，在后续版本中从 Ent schema 移除 `"sub2api_sso"` enum 值
- 更新所有调用方（如 DevLogin handler）统一使用 `"relay_sso"`

### 好处

- API key 按用户隔离，用量天然按用户区分
- 用户无需手动配置任何 LLM 相关参数
- 管理员可灵活配置多个 provider
- 替换或新增 provider 时，用户只需重新 `ae-cli login` 即可获取新配置
- ae-cli 直接调 provider，延迟更低（不经过后端代理）

## 8. Session 优化

### start 自动触发 login

改造 `ae-cli start` 命令：

1. 检查 `~/.ae-cli/token.json` 是否存在且有效
2. 如果 token 不存在或已过期且 refresh 失败：自动触发 OAuth 登录流程
3. 登录成功后继续创建 session

### Session 自动关联用户

Session schema 新增 `user_id` 字段（User edge）：

| 新增字段 | 类型 | 说明 |
|---------|------|------|
| `user_id` | int, optional, edge to User | 创建 session 的用户 |

改造后端 session 创建逻辑：
- 从 JWT token 中提取 `user_id`，自动设置 session 的 User edge
- 不再需要 ae-cli 在 request body 中显式传 `sub2api_user_id`
- 旧的 `sub2api_user_id` 请求字段标记为 deprecated，后续移除

## Dependencies

```
# 后端新增
# 当前 OAuth server 为自定义轻量实现，无新增框架级 OAuth 依赖

# 后端说明
github.com/Masterminds/squirrel  # 当前代码仍保留该依赖，后续如完全无引用可单独清理

# ae-cli 新增
golang.org/x/oauth2
```

## Security Considerations

- PKCE 防止 authorization code 截获攻击
- state 参数防止 CSRF
- redirect_uri 严格校验：URL 解析后检查 host 为 `localhost` 或 `127.0.0.1`，port 为纯数字，path 为 `/callback`，拒绝其他形式
- token.json 文件权限 0600，原子写入（write-then-rename）防止损坏
- LDAP bind_password 使用 AES-256-GCM 加密存储，密钥使用独立的 `encryption.key`（与 JWT secret 解耦，避免 secret 轮换影响已存储密码）
- relay server API 调用走内网，不暴露到公网
- relay server admin API key 仅在后端 relay.Provider 内部持有（用于 API key 管理），用户的 API key 通过安全通道下发到 ae-cli
- `GET /api/v1/providers` 端点仅限 ae-cli 调用（需 JWT 认证），不应从前端调用；响应中的 API key 不应被日志中间件记录
- ae-cli login 本地 HTTP server 设置 3 分钟超时，防止端口长期占用

## Known Limitations (v1)

- Authorization code 存储在内存中，后端重启会丢失（用户需重新授权）
- relay server（sub2api）TOTP 双因素认证不支持中继，开启 TOTP 的用户需通过 LDAP 登录
- relay server（sub2api）Turnstile 验证需要对内网 IP 豁免或提供 service-to-service 绕过机制
- 不支持 Device Authorization Flow（RFC 8628），后续单独实现
- relay.Provider 当前仅有 sub2api 实现，接口设计基于 sub2api 的 API 能力，后续接入新 relay server 时可能需要微调接口
- `ChatCompletion` 不支持 streaming 响应，AI shell 场景下用户需等待完整响应。后续可扩展 `ChatCompletionStream` 方法返回 `io.Reader`
