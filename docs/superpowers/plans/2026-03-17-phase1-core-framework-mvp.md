# Phase 1: Core Framework MVP — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Updated:** 2026-03-24 — 基于代码审查同步 plan 与实际实现，标注偏差和额外实现

**Goal:** Build the foundational infrastructure for the AI Efficiency Platform — project scaffolding, dual database connections, authentication, SCM plugin interface with GitHub implementation, repo configuration CRUD with webhook auto-injection, ae-cli basic session management, and the session API.

**Architecture:** Modular monolith in Go (Gin + Ent) with Vue 3 frontend. Independent `ai_efficiency` database with read-only connection to sub2api. SCM integration via plugin interface pattern. ae-cli as a standalone Go CLI using Cobra.

**Tech Stack:** Go 1.26+ (Gin, Ent ORM, zap, squirrel, cobra), Vue 3 (Vite, TailwindCSS, Pinia), PostgreSQL, Redis

**Status:** ✅ 已完成（2026-03-20）

**Replay Status:** 历史完成记录。不要直接按本文逐 task 重跑；如需再次执行，请基于当前代码和最新 spec 重写新的执行计划。

**Source Of Truth:** 已实现行为以当前代码为准。本文中涉及旧 `sub2api_*` 字段、旧目录约定或当时实现偏差的内容，仅反映历史实施背景，不应覆盖当前代码或最新 spec。

**Known Stale Sections:** `sub2apidb`、旧 session schema、旧 SSO 路径、旧 session create API 示例均已过时，仅保留作历史记录。

---

## File Structure

> **审查说明（2026-03-24）：** 以下文件结构与实际实现基本一致。已知偏差用 `⚠️` 标注。

### Backend

```
backend/
├── cmd/server/main.go                    # Server entrypoint
├── go.mod                                # Go module definition
├── go.sum
├── ent/
│   ├── schema/
│   │   ├── scmprovider.go               # SCM provider entity
│   │   ├── repoconfig.go               # Repo configuration entity
│   │   ├── session.go                   # ae-cli session entity
│   │   ├── user.go                      # Local user entity
│   │   ├── webhookdeadletter.go         # Dead letter queue entity
│   │   └── helpers.go                   # ⚠️ 额外：共享 helper（timeNow 等）
│   └── generate.go                      # Ent codegen directive
├── internal/
│   ├── config/
│   │   └── config.go                    # App configuration (viper)
│   ├── auth/
│   │   ├── auth.go                      # Auth service interface + factory
│   │   ├── sso.go                       # ⚠️ sub2api SSO provider（placeholder，Authenticate 返回 nil）
│   │   ├── ldap.go                      # LDAP provider
│   │   ├── middleware.go                # Gin auth middleware
│   │   └── auth_test.go                 # Auth tests
│   ├── scm/
│   │   ├── provider.go                  # ⚠️ SCMProvider 接口 + 类型定义（计划中 types.go 独立，实际合并于此）
│   │   └── github/
│   │       ├── github.go               # GitHub provider implementation
│   │       └── github_test.go          # GitHub provider tests
│   ├── repo/
│   │   ├── service.go                   # Repo config CRUD + webhook injection
│   │   ├── factory.go                   # ⚠️ 额外：SCM provider 工厂（计划中在 scm/provider.go）
│   │   └── repo_test.go                # ⚠️ 偏差：计划为 service_test.go
│   ├── webhook/
│   │   ├── handler.go                   # Webhook HTTP handler + dispatch
│   │   └── webhook_test.go             # ⚠️ 偏差：计划为 handler_test.go
│   ├── handler/
│   │   ├── auth.go                      # Auth HTTP handlers
│   │   ├── scmprovider.go              # SCM provider CRUD handlers
│   │   ├── repo.go                      # Repo config handlers
│   │   ├── session.go                   # Session API handlers (ae-cli)
│   │   ├── interfaces.go               # ⚠️ 额外：handler 接口抽象
│   │   └── router.go                    # Gin router setup
│   ├── sub2apidb/
│   │   └── client.go                    # sub2api read-only DB client (raw SQL + squirrel)
│   ├── middleware/
│   │   └── cors.go                      # CORS middleware
│   └── pkg/
│       ├── crypto.go                    # AES-256-GCM encrypt/decrypt for credentials
│       └── response.go                  # Unified JSON response helpers
└── migrations/                          # (reserved for Atlas, Phase 1 uses Ent auto-migrate)
```

### Frontend

```
frontend/
├── package.json
├── vite.config.ts
├── tsconfig.json
├── tailwind.config.js
├── postcss.config.js
├── index.html
├── env.d.ts
└── src/
    ├── main.ts                          # Vue app entry
    ├── App.vue                          # Root component
    ├── router/
    │   └── index.ts                     # Vue Router setup
    ├── stores/
    │   ├── auth.ts                      # Auth store (Pinia)
    │   └── repo.ts                      # Repo store (Pinia)
    ├── api/
    │   ├── client.ts                    # Axios instance + interceptors
    │   ├── auth.ts                      # Auth API calls
    │   ├── scmProvider.ts               # SCM provider API calls
    │   └── repo.ts                      # Repo config API calls
    ├── views/
    │   ├── LoginView.vue                # Login page
    │   ├── DashboardView.vue            # Placeholder dashboard
    │   └── repos/
    │       ├── RepoListView.vue         # Repo list page
    │       └── RepoDetailView.vue       # Repo detail page
    ├── components/
    │   ├── AppLayout.vue                # Main layout with sidebar
    │   └── AppSidebar.vue               # Navigation sidebar
    └── types/
        └── index.ts                     # TypeScript type definitions
```

### ae-cli

```
ae-cli/
├── cmd/
│   ├── root.go                          # Root cobra command
│   ├── start.go                         # `ae-cli start` command
│   ├── stop.go                          # `ae-cli stop` command
│   ├── run.go                           # `ae-cli run <tool> <prompt>` command
│   ├── version.go                       # Version command
│   ├── ps.go                            # ⚠️ 额外：`ae-cli ps` 进程列表
│   ├── kill.go                          # ⚠️ 额外：`ae-cli kill` 终止命令
│   ├── attach.go                        # ⚠️ 额外：`ae-cli attach` 附加命令
│   └── shell.go                         # ⚠️ 额外：`ae-cli shell` 交互式 shell
├── internal/
│   ├── session/
│   │   └── manager.go                   # Session lifecycle management
│   ├── dispatcher/
│   │   └── dispatcher.go               # AI tool dispatcher
│   ├── client/
│   │   └── client.go                    # HTTP client for efficiency platform API
│   ├── router/
│   │   └── router.go                    # ⚠️ 额外：路由模块
│   ├── tmux/
│   │   └── tmux.go                      # ⚠️ 额外：Tmux 集成
│   └── shell/
│       └── shell.go                     # ⚠️ 额外：Shell 集成
├── config/
│   └── config.go                        # CLI config (~/.ae-cli/config.yaml)
├── go.mod
├── go.sum
└── main.go                              # CLI entrypoint
```

### Deploy

```
deploy/
├── docker-compose.yml                   # PostgreSQL + Redis + backend
├── config.example.yaml                  # Example configuration
└── Dockerfile                           # Multi-stage build (backend + embedded frontend)
```

---

## Implementation Steps

### Step 1: Backend Project Scaffolding + Deploy

Set up the Go module, directory structure, configuration, utility packages, and Docker Compose for local development.

- [x] 1.1 Create `backend/go.mod` with module `github.com/ai-efficiency/backend`, Go 1.22+. Add core dependencies: `gin-gonic/gin`, `entgo.io/ent`, `spf13/viper`, `uber-go/zap`, `Masterminds/squirrel`, `golang-jwt/jwt/v5`, `go-ldap/ldap/v3`, `google/go-github/v60`, `google/uuid`.
- [x] 1.2 Create `backend/internal/config/config.go` — Viper-based config struct. Fields: `Server.Port`, `DB.DSN` (ai_efficiency), `Sub2apiDB.DSN` (read-only), `Auth.JWTSecret`, `Auth.LDAP.URL/BaseDN/BindDN/BindPassword`, `Encryption.Key` (AES-256 key for credentials). Load from `config.yaml` + env vars (`AE_` prefix).
- [x] 1.3 Create `backend/internal/pkg/response.go` — Unified JSON response helpers: `Success(c, data)`, `Error(c, code, msg)`, `PagedResponse` struct with `Total/Page/PageSize/Items`.
- [x] 1.4 Create `backend/internal/pkg/crypto.go` — AES-256-GCM encrypt/decrypt functions for SCM credentials. `Encrypt(plaintext, key) (ciphertext string, err)`, `Decrypt(ciphertext, key) (plaintext string, err)`. Key from config.
- [x] 1.5 Create `backend/internal/middleware/cors.go` — CORS middleware for Gin. Allow configurable origins, methods, headers.
- [x] 1.6 Create `backend/cmd/server/main.go` — Entrypoint: load config, init logger (zap), connect to both databases, run Ent auto-migrate, setup router, start Gin server. Graceful shutdown on SIGINT/SIGTERM.
- [x] 1.7 Create `deploy/docker-compose.yml` — PostgreSQL 16 (with two databases: `ai_efficiency` + `sub2api`), Redis 7. Expose ports 5432, 6379. Volume mounts for data persistence.
- [x] 1.8 Create `deploy/config.example.yaml` — Example config file with all fields documented.
- [x] 1.9 Create `backend/ent/generate.go` — `//go:generate go run -mod=mod entgo.io/ent/cmd/ent generate ./schema`
- [x] 1.10 Verify: `cd backend && go build ./cmd/server/` compiles. Docker Compose starts cleanly.

### Step 2: Ent Schema Definitions + sub2api Read-Only Client

Define all Phase 1 Ent schemas and the raw SQL client for sub2api.

- [x] 2.1 Create `backend/ent/schema/user.go` — Fields: `id` (int, auto), `username` (string, unique), `email` (string, unique), `auth_source` (enum: sub2api_sso/ldap), `sub2api_user_id` (int, optional, nillable), `ldap_dn` (string, optional, nillable), `role` (enum: admin/user, default user), `created_at`, `updated_at`. Indexes on `username`, `email`, `sub2api_user_id`.
- [x] 2.2 Create `backend/ent/schema/scmprovider.go` — Fields: `id` (int, auto), `name` (string), `type` (enum: github/bitbucket_server), `base_url` (string), `credentials` (string, sensitive — encrypted JSON), `status` (enum: active/inactive/error, default active), `created_at`, `updated_at`.
- [x] 2.3 Create `backend/ent/schema/repoconfig.go` — Fields: `id`, `name`, `full_name` (unique per scm_provider), `clone_url`, `default_branch`, `webhook_id` (optional), `webhook_secret` (optional), `ai_score` (int, optional, default 0), `last_scan_at` (optional), `group_id` (optional), `status` (enum: active/webhook_failed/inactive), `created_at`, `updated_at`. Edge: belongs to `scm_provider`.
- [x] 2.4 Create `backend/ent/schema/session.go` — Fields: `id` (uuid, immutable — client-generated), `branch` (string), `sub2api_user_id` (int, optional), `sub2api_api_key_id` (int, optional), `started_at` (time), `ended_at` (time, optional, nillable), `tool_invocations` (json, default `[]`), `status` (enum: active/completed/abandoned, default active), `created_at`. Edge: belongs to `repo_config`.
- [x] 2.5 Create `backend/ent/schema/webhookdeadletter.go` — Fields: `id`, `delivery_id` (string), `event_type` (string), `payload` (json), `error_message` (string), `retry_count` (int, default 0), `max_retries` (int, default 3), `status` (enum: pending/retrying/failed/resolved, default pending), `created_at`, `resolved_at` (optional). Edge: belongs to `repo_config`.
- [x] 2.6 Run `cd backend && go generate ./ent` to generate Ent code. Fix any schema issues.
- [x] 2.7 Create `backend/internal/sub2apidb/client.go` — `Sub2apiClient` struct wrapping `*sql.DB`. Methods: `QueryUsageLogs(ctx, apiKeyID, from, to) ([]UsageLog, error)`, `GetAPIKey(ctx, id) (*APIKey, error)`, `GetUser(ctx, id) (*User, error)`. Use squirrel for query building. Schema probe on init: check required columns exist, log warning if not. Connection uses `default_transaction_read_only=on` in DSN.
- [x] 2.8 Verify: `go generate ./ent` succeeds. `go build ./cmd/server/` compiles with schemas. Server starts and auto-migrates tables into `ai_efficiency` database.

### Step 3: Authentication System

Implement dual auth (sub2api SSO + LDAP) with JWT token issuance.

- [x] 3.1 Create `backend/internal/auth/auth.go` — `AuthProvider` interface: `Authenticate(ctx, username, password) (*UserInfo, error)`. `AuthService` struct that holds SSO + LDAP providers. `Login(ctx, req) (*TokenPair, error)` tries SSO first, falls back to LDAP. `RefreshToken(ctx, refreshToken) (*TokenPair, error)`. JWT generation with configurable expiry (access: 2h, refresh: 7d).
- [x] 3.2 Create `backend/internal/auth/sso.go` — `SSOProvider` struct. Authenticates by reading sub2api users table (via sub2apidb client) and verifying password hash. On success, ensures local user record exists (upsert by `sub2api_user_id`).
- [x] 3.3 Create `backend/internal/auth/ldap.go` — `LDAPProvider` struct. Standard LDAP bind auth: connect → bind with service account → search user → bind with user credentials. On first login, create local user record with `auth_source=ldap`.
- [x] 3.4 Create `backend/internal/auth/middleware.go` — Gin middleware: extract Bearer token from `Authorization` header, validate JWT, set `UserContext` (user_id, username, role) in Gin context. `RequireAuth()` and `RequireAdmin()` middleware functions.
- [x] 3.5 Create `backend/internal/handler/auth.go` — HTTP handlers: `POST /api/v1/auth/login` (accepts `{username, password, source: "sso"|"ldap"}`), `POST /api/v1/auth/refresh`, `GET /api/v1/auth/me`.
- [x] 3.6 Create `backend/internal/auth/auth_test.go` — Unit tests: JWT generation/validation, middleware token extraction, mock SSO/LDAP providers.
- [x] 3.7 Verify: Login endpoint returns JWT. Protected endpoints reject unauthenticated requests. `/auth/me` returns current user.

### Step 4: SCM Plugin Interface + GitHub Implementation

Define the SCM provider interface and implement the GitHub provider.

- [x] 4.1 Create `backend/internal/scm/types.go` — Shared types: `Repo`, `PR`, `PRInfo`, `WebhookEvent`, `EventType` (enum: pr_opened/pr_merged/pr_updated/push), `CreatePRRequest`, `SetStatusRequest`, `CommitFilesRequest`, `ListOpts`, `PRListOpts`, `MergeOpts`, `TreeEntry`.
- [x] 4.2 Create `backend/internal/scm/provider.go` — `SCMProvider` interface as defined in design spec Section 3.1. `ProviderFactory` function: `NewProvider(providerType, baseURL, credentials) (SCMProvider, error)`.
- [x] 4.3 Create `backend/internal/scm/github/github.go` — `Provider` struct implementing `SCMProvider`. Uses `go-github/v60` client. Implement all interface methods. Webhook parsing uses `github.ValidatePayload()` + `github.ParseWebHook()`. For `RegisterWebhook`: create webhook with PR + push events, return webhook ID.
- [x] 4.4 Create `backend/internal/handler/scmprovider.go` — CRUD handlers for SCM providers: `GET /api/v1/scm-providers`, `POST /api/v1/scm-providers` (encrypt credentials before storing), `PUT /api/v1/scm-providers/:id`, `DELETE /api/v1/scm-providers/:id`, `POST /api/v1/scm-providers/:id/test` (instantiate provider, call `ListRepos` with limit 1 to verify connectivity).
- [x] 4.5 Create `backend/internal/scm/github/github_test.go` — Unit tests with mocked HTTP responses: repo listing, webhook registration, webhook payload parsing.
- [x] 4.6 Verify: Can create a GitHub SCM provider via API. Test connection endpoint works. Credentials are encrypted in database.

### Step 5: Repo Configuration CRUD + Webhook Auto-Injection

Implement repo management with automatic webhook lifecycle.

- [x] 5.1 Create `backend/internal/repo/service.go` — `RepoService` struct. Methods:
  - `Create(ctx, req)`: validate SCM access → register webhook (generate secret, call `provider.RegisterWebhook`) → save repo_config with webhook_id/secret → return repo.
  - `Update(ctx, id, req)`: update mutable fields (name, group_id, status).
  - `Delete(ctx, id)`: call `provider.DeleteWebhook` → delete repo_config.
  - `List(ctx, opts)`: paginated list with optional filters (scm_provider_id, status, group_id).
  - `Get(ctx, id)`: single repo with scm_provider info.
  - `TriggerScan(ctx, id)`: placeholder for Phase 2 — just updates `last_scan_at`.
- [x] 5.2 Create `backend/internal/handler/repo.go` — HTTP handlers: `GET /api/v1/repos`, `POST /api/v1/repos`, `PUT /api/v1/repos/:id`, `DELETE /api/v1/repos/:id`, `POST /api/v1/repos/:id/scan` (placeholder).
- [x] 5.3 Create `backend/internal/repo/service_test.go` — Unit tests with mocked SCM provider: create repo with webhook injection, delete repo with webhook cleanup, webhook registration failure sets status to `webhook_failed`.
- [x] 5.4 Verify: Full CRUD flow via API. Webhook auto-registered on create, auto-deleted on remove.

### Step 6: Webhook Receiver + Event Dispatch

Handle incoming SCM webhooks and dispatch to modules.

- [x] 6.1 Create `backend/internal/webhook/handler.go` — `WebhookHandler` struct. HTTP handlers:
  - `POST /api/v1/webhooks/github`: look up repo_config by repo full_name → validate signature using stored webhook_secret → parse payload via `provider.ParseWebhookPayload` → dispatch event.
  - Dispatch logic: fan-out to registered handlers (Phase 1: just log events + store dead letters on failure).
  - Idempotency: check `delivery_id` against dead_letters table to skip duplicates.
- [x] 6.2 Create `backend/internal/webhook/handler_test.go` — Tests: valid signature accepted, invalid signature rejected, duplicate delivery_id skipped, parse failure → dead letter queue.
- [x] 6.3 Verify: Webhook endpoint accepts valid GitHub payloads. Invalid signatures return 401. Failed processing writes to dead_letters table.

### Step 7: Session API (ae-cli <-> Backend)

Implement the session lifecycle endpoints that ae-cli will call.

- [x] 7.1 Create `backend/internal/handler/session.go` — HTTP handlers:
  - `POST /api/v1/sessions`: create session (client-provided UUID as ID, resolve repo_config_id from `repo_full_name`, store branch + sub2api_api_key_id). Return 201.
  - `PUT /api/v1/sessions/:id`: heartbeat update (touch `updated_at`, accept partial updates to `tool_invocations`).
  - `POST /api/v1/sessions/:id/stop`: set `ended_at = now()`, `status = completed`.
  - `POST /api/v1/sessions/:id/invocations`: append tool invocation `{tool, start, end}` to `tool_invocations` JSONB array.
- [x] 7.2 Verify: Full session lifecycle via API — create, heartbeat, add invocations, stop. Session data persists correctly.

### Step 8: Router Assembly + Server Integration

Wire all handlers into the Gin router and verify end-to-end.

- [x] 8.1 Create `backend/internal/handler/router.go` — `SetupRouter(cfg, entClient, sub2apiClient, authService, repoService, webhookHandler) *gin.Engine`. Mount all route groups:
  - `/api/v1/auth/*` — no auth middleware
  - `/api/v1/webhooks/*` — no auth middleware (signature-verified internally)
  - `/api/v1/scm-providers/*` — RequireAuth + RequireAdmin
  - `/api/v1/repos/*` — RequireAuth
  - `/api/v1/sessions/*` — RequireAuth
  - `/api/v1/admin/*` — RequireAdmin
  - CORS middleware on all routes.
- [x] 8.2 Update `backend/cmd/server/main.go` — Wire all dependencies: config → DB connections → Ent client → sub2api client → auth service → repo service → webhook handler → router → server.
- [x] 8.3 Verify: Server starts, all routes registered. Smoke test: login → create SCM provider → create repo → create session → stop session.

### Step 9: ae-cli — Session Management + Tool Dispatcher

Build the CLI tool that developers use to track AI coding sessions.

- [x] 9.1 Create `ae-cli/go.mod` with module `github.com/ai-efficiency/ae-cli`. Dependencies: `spf13/cobra`, `spf13/viper`, `google/uuid`.
- [x] 9.2 Create `ae-cli/config/config.go` — Load `~/.ae-cli/config.yaml`. Struct: `Server.URL`, `Server.Token`, `Sub2api.APIKeyEnv`, `Tools` (map of tool name → `{Command, Args}`).
- [x] 9.3 Create `ae-cli/internal/client/client.go` — HTTP client for efficiency platform API. Methods: `CreateSession(req)`, `Heartbeat(sessionID)`, `StopSession(sessionID)`, `AddInvocation(sessionID, invocation)`. JWT auth via config token.
- [x] 9.4 Create `ae-cli/internal/session/manager.go` — `Manager` struct. `Start()`: detect current git repo + branch (via `git rev-parse`), read sub2api API key from env, generate UUID, call `client.CreateSession`, write session state to `~/.ae-cli/current-session.json`. `Stop()`: call `client.StopSession`, remove state file. `Current()`: read state file, return active session or nil.
- [x] 9.5 Create `ae-cli/internal/dispatcher/dispatcher.go` — `Dispatcher` struct. `Run(toolName, prompt)`: look up tool config, build command (e.g., `claude -p "fix the login bug"`), record start time, execute via `os/exec`, record end time, call `client.AddInvocation`. Stream tool stdout/stderr to terminal.
- [x] 9.6 Create `ae-cli/cmd/root.go` — Root cobra command with persistent flags: `--config` (config file path), `--server` (override server URL).
- [x] 9.7 Create `ae-cli/cmd/start.go` — `ae-cli start` command. Calls `session.Manager.Start()`. Prints session ID and detected repo/branch.
- [x] 9.8 Create `ae-cli/cmd/stop.go` — `ae-cli stop` command. Calls `session.Manager.Stop()`. Prints session summary.
- [x] 9.9 Create `ae-cli/cmd/run.go` — `ae-cli run <tool> <prompt>` command. Validates active session exists, calls `dispatcher.Run()`.
- [x] 9.10 Create `ae-cli/cmd/version.go` — `ae-cli version` command. Prints version string.
- [x] 9.11 Create `ae-cli/main.go` — Entrypoint: `cmd.Execute()`.
- [x] 9.12 Verify: `cd ae-cli && go build -o ae-cli .` compiles. `ae-cli start` creates session on running backend. `ae-cli run claude-code "test"` dispatches and records invocation. `ae-cli stop` ends session.

### Step 10: Frontend Scaffolding

Set up Vue 3 project with routing, auth, and basic pages.

- [x] 10.1 Initialize Vue 3 project in `frontend/` using Vite. Install dependencies: `vue-router`, `pinia`, `axios`, `tailwindcss`, `postcss`, `autoprefixer`. Configure TailwindCSS + PostCSS.
- [x] 10.2 Create `frontend/src/api/client.ts` — Axios instance with base URL from env (`VITE_API_URL`). Request interceptor: attach JWT from localStorage. Response interceptor: on 401, redirect to login.
- [x] 10.3 Create `frontend/src/api/auth.ts` — `login(username, password, source)`, `refresh()`, `getMe()`.
- [x] 10.4 Create `frontend/src/api/scmProvider.ts` — CRUD functions for SCM providers.
- [x] 10.5 Create `frontend/src/api/repo.ts` — CRUD functions for repos.
- [x] 10.6 Create `frontend/src/stores/auth.ts` — Pinia store: `user`, `token`, `isAuthenticated`. Actions: `login`, `logout`, `fetchMe`.
- [x] 10.7 Create `frontend/src/stores/repo.ts` — Pinia store: `repos`, `currentRepo`. Actions: `fetchRepos`, `createRepo`, `deleteRepo`.
- [x] 10.8 Create `frontend/src/types/index.ts` — TypeScript interfaces: `User`, `SCMProvider`, `RepoConfig`, `Session`, `LoginRequest`, `PagedResponse<T>`.
- [x] 10.9 Create `frontend/src/router/index.ts` — Routes: `/login` → LoginView, `/` → DashboardView (requires auth), `/repos` → RepoListView, `/repos/:id` → RepoDetailView. Navigation guard: redirect to `/login` if not authenticated.
- [x] 10.10 Create `frontend/src/components/AppLayout.vue` — Main layout: sidebar + content area. Slot for page content.
- [x] 10.11 Create `frontend/src/components/AppSidebar.vue` — Navigation sidebar: Dashboard, Repos, (placeholder: Analysis, Gating). User info + logout at bottom.
- [x] 10.12 Create `frontend/src/views/LoginView.vue` — Login form: username, password, auth source selector (SSO/LDAP). Calls auth store login, redirects to dashboard on success.
- [x] 10.13 Create `frontend/src/views/DashboardView.vue` — Placeholder dashboard with welcome message and summary cards (total repos, active sessions — hardcoded for now).
- [x] 10.14 Create `frontend/src/views/repos/RepoListView.vue` — Table of configured repos: name, SCM provider, AI score, status, last scan. Add repo button → modal/form. Delete button with confirmation.
- [x] 10.15 Create `frontend/src/views/repos/RepoDetailView.vue` — Repo detail: basic info, webhook status, scan history placeholder, sessions list placeholder.
- [x] 10.16 Verify: `cd frontend && npm run build` succeeds. Dev server shows login page, can navigate to repos page after login.

### Step 11: Deployment + Dockerfile

Multi-stage Docker build embedding frontend into backend binary.

- [x] 11.1 Create `deploy/Dockerfile` — Multi-stage: (1) Node stage: build frontend → `dist/`. (2) Go stage: copy frontend dist into `backend/static/`, build Go binary with `embed` directive. (3) Final stage: minimal image with binary only.
- [x] 11.2 Update `backend/cmd/server/main.go` — Add `//go:embed static/*` for embedded frontend. Serve static files at `/` with SPA fallback (serve `index.html` for non-API routes).
- [x] 11.3 Update `deploy/docker-compose.yml` — Add `backend` service built from Dockerfile. Depends on postgres + redis. Environment variables for config.
- [x] 11.4 Verify: `docker compose up --build` starts full stack. Can access frontend at `http://localhost:8081`, login, and manage repos.

---

## Dependencies Between Steps

```
Step 1 (Scaffolding) ──→ Step 2 (Schemas) ──→ Step 3 (Auth) ──→ Step 8 (Router)
                                            ──→ Step 4 (SCM)  ──→ Step 5 (Repo) ──→ Step 8
                                            ──→ Step 6 (Webhook) ──→ Step 8
                                            ──→ Step 7 (Session API) ──→ Step 8
Step 1 ──→ Step 9 (ae-cli) [independent Go module, needs running backend for integration test]
Step 1 ──→ Step 10 (Frontend) [independent, needs running backend for integration]
Step 8 (Router) ──→ Step 11 (Deploy)
Step 10 (Frontend) ──→ Step 11 (Deploy)
```

Parallelizable after Step 2: Steps 3, 4, 6, 7 can be developed in parallel. Step 9 and Step 10 can start after Step 1.

---

## Conventions

- Go package naming: lowercase, single word (e.g., `auth`, `scm`, `repo`)
- Error handling: wrap with `fmt.Errorf("operation: %w", err)` for context
- Logging: use `zap.Logger` injected via struct fields, not global
- Config: Viper with `AE_` env prefix, YAML file as base
- API responses: always use `pkg.Success()` / `pkg.Error()` helpers
- Ent queries: use `.Where()` predicates, never raw SQL on ai_efficiency DB
- sub2api queries: always raw SQL via squirrel, never Ent
- Frontend: `<script setup lang="ts">`, Composition API only, TailwindCSS for styling
- Commits: Conventional Commits — `feat(backend): add auth middleware`

---

## 审查偏差总结（2026-03-24）

### ❌ 已知偏差

| # | 位置 | 计划要求 | 实际实现 |
|---|------|----------|----------|
| 1 | `auth/sso.go` | sub2api SSO 认证 | **Placeholder**，`Authenticate` 始终返回 nil，标注 TODO |
| 2 | `scm/types.go` | 独立的类型定义文件 | 合并到 `scm/provider.go` |
| 3 | `repo/service_test.go` | 测试文件名 | 实际为 `repo_test.go` + `repo_coverage_test.go` |
| 4 | `webhook/handler_test.go` | 测试文件名 | 实际为 `webhook_test.go` + `webhook_coverage_test.go` |
| 5 | `scm/provider.go` | `ProviderFactory` 工厂函数 | 工厂逻辑移到 `repo/factory.go`，unexported |
| 6 | `auth/auth.go` | `Login` 返回 `(*TokenPair, error)` | 实际返回 `(*TokenPair, *UserInfo, error)` |
| 7 | `router.go` | `SetupRouter` 只接收 Phase 1 依赖 | 实际签名包含 Phase 2 依赖（analysisService 等） |

### ⚠️ 额外实现（计划外）

| # | 内容 | 说明 |
|---|------|------|
| 1 | `POST /api/v1/auth/dev-login` | Debug 模式开发登录（`AE_DEV_LOGIN_ENABLED=true`） |
| 2 | `GET /api/v1/health` | 健康检查端点 |
| 3 | `POST /api/v1/repos/direct` | 无 SCM 验证的直接创建 repo |
| 4 | `GET /api/v1/sessions` + `GET /api/v1/sessions/:id` | Session 列表和详情端点 |
| 5 | ae-cli: `ps`, `kill`, `attach`, `shell` 命令 | 扩展的进程管理和 tmux 集成 |
| 6 | `ent/schema/helpers.go` | 共享 helper 函数 |
| 7 | `handler/interfaces.go` | Handler 接口抽象 |
| 8 | `repo/factory.go` | SCM provider 工厂独立文件 |
| 9 | Phase 2 代码提前混入 | main.go 和 router.go 已引入 analysis、llm、efficiency、prsync 等包 |

### 待修复项

- **高优先级**：实现 `sso.go` 的 sub2api 密码哈希验证（当前依赖 dev-login 绕过）
