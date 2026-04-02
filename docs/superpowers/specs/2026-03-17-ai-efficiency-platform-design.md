# AI Efficiency Platform（AI 效能平台）设计文档

**Date:** 2026-03-17
**Updated:** 2026-03-24 — 基于 Phase 1/2 实现审查，同步 spec 与实际代码
**Status:** Approved
**Scope:** `backend/`, `frontend/`, `ae-cli/`, `deploy/`

**Current Alignment Note (2026-03-26):**
- 本文是平台级历史基线文档。
- 认证、provider 集成、session API key 归因等核心合同，当前应以 `2026-03-24-oauth-cli-login-design.md` 和 `2026-03-26-session-pr-attribution-design.md` 为准。
- 文中仍出现的 `sub2api` 数据库直连、`sub2api_*` 字段、旧 session 示例，仅代表早期设计背景，不应覆盖当前代码和后续设计。
- 项目级架构图与模块总览见 [`docs/architecture.md`](../../architecture.md)；本文仍保留为历史基线说明。

## 概述

AI 效能平台是一个与 sub2api 平行的独立系统，用于衡量和优化 AI 辅助开发的效能。它通过对接多种 SCM 平台（GitHub、Bitbucket Server 等），分析项目的 AI 友好度，追踪 PR 与 token 消耗的精确关联，并提供可配置的 PR 质量门禁。

## 核心组件

1. **效能平台后端** — Go 模块化单体服务
2. **效能平台前端** — Vue 3 SPA，与 sub2api UI 风格一致
3. **ae-cli** — 轻量 CLI 工具，session 追踪 + AI 编码工具调度器
4. **SCM 插件** — GitHub + Bitbucket Server，webhook 自动注入

## 架构决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 部署关系 | 独立服务，共享 PostgreSQL 实例 | 低耦合，独立演进 |
| 架构风格 | 模块化单体 | 和 sub2api 一致，部署简单，后续可拆分 |
| 技术栈 | Go (Gin + Ent) + Vue 3 (Vite + TailwindCSS + Pinia) | 与 sub2api 完全一致，团队技能复用 |
| 数据库 | 独立数据库 `ai_efficiency` + relay API 集成 | 平台数据库独立；当前集成优先通过 relay/sub2api HTTP API 完成 |
| 用户认证 | relay SSO + LDAP | 企业场景双认证 |
| SCM 集成 | API 直连 + 插件架构 | 统一接口，可扩展 |
| AI 友好度分析 | 静态规则 + LLM 辅助 | 基础评分快速可靠，LLM 补充深层分析 |
| PR-Token 关联 | ae-cli session 追踪 | 精确关联，消除并行开发干扰，不改动 sub2api |
| PR Gating | 标签 + 可配置规则引擎 | 灵活适配不同团队策略 |

## 整体架构与项目结构

```
ai-efficiency/
├── backend/
│   ├── cmd/server/              # 入口
│   ├── ent/schema/              # Ent ORM 数据模型
│   ├── internal/
│   │   ├── config/              # 配置管理
│   │   ├── auth/                # 双认证：sub2api SSO + LDAP
│   │   ├── scm/                 # SCM 插件架构
│   │   │   ├── provider.go      # 统一 SCM Provider 接口
│   │   │   ├── github/          # GitHub 实现
│   │   │   └── bitbucket/       # Bitbucket Server 实现
│   │   ├── repo/                # Repo 配置管理、联合 repo 依赖图
│   │   ├── analysis/            # AI 友好度分析引擎
│   │   │   ├── rules/           # 静态规则检查
│   │   │   └── llm/             # LLM 辅助分析（通过 sub2api 路由）
│   │   ├── gating/              # PR 标签 + 规则引擎（Phase 3）
│   │   ├── efficiency/          # 效能指标计算与聚合 + PR 自动标签
│   │   ├── prsync/              # PR 同步服务（从 SCM 拉取 PR 列表）
│   │   ├── webhook/             # Webhook 事件接收与分发
│   │   ├── handler/             # HTTP handlers
│   │   ├── middleware/          # 中间件
│   │   └── pkg/                 # 公共工具包
│   └── migrations/
├── frontend/                    # Vue 3 + Vite + TailwindCSS + Pinia
│   └── src/
│       ├── views/
│       │   ├── dashboard/       # 效能仪表盘
│       │   ├── repos/           # Repo 配置管理
│       │   ├── analysis/        # AI 友好度分析报告
│       │   └── gating/          # PR Gating 规则配置
│       ├── api/
│       ├── stores/
│       ├── components/
│       └── router/
├── ae-cli/                      # 轻量 CLI 工具
│   ├── cmd/                     # CLI 命令定义
│   ├── internal/
│   │   ├── session/             # Session 管理
│   │   ├── dispatcher/          # AI 工具调度
│   │   └── client/              # 效能平台 API 客户端
│   └── config/                  # CLI 配置
└── deploy/                      # Docker Compose 部署
```

关键设计决策：
- 当前交付形态为前端构建产物随 backend 服务镜像一起发布；是否编译进单二进制应以当前代码为准
- LLM 分析调用通过 sub2api 的 API 网关路由，形成闭环

## 数据模型

### 效能平台自有数据库（ai_efficiency，读写）

```
scm_providers
┌──────────────────┐
│ id               │  PK
│ name             │  SCM 实例名称
│ type             │  github / bitbucket_server
│ base_url         │  API 基础地址
│ credentials      │  加密存储的凭证 (JSONB)
│ status           │  active / inactive / error
│ created_at       │
│ updated_at       │
└──────────────────┘

repo_configs
┌──────────────────┐
│ id               │  PK
│ scm_provider_id  │  FK → scm_providers
│ name             │  仓库名称
│ full_name        │  完整名称 (org/repo)
│ clone_url        │  克隆地址
│ default_branch   │  默认分支
│ webhook_id       │  SCM 侧的 webhook ID
│ webhook_secret   │  webhook 签名密钥
│ ai_score         │  最新 AI 友好度评分 (0-100)
│ last_scan_at     │  最近一次扫描时间
│ group_id         │  逻辑分组（联合 repo 用）
│ status           │  active / webhook_failed / inactive
│ created_at       │
│ updated_at       │
└──────────────────┘

repo_dependencies
┌──────────────────┐
│ id               │  PK
│ source_id        │  FK → repo_configs（依赖方）
│ target_id        │  FK → repo_configs（被依赖方）
│ dep_type         │  api_call / library / shared_db
│ metadata         │  JSONB（依赖详情）
│ created_at       │
└──────────────────┘

ai_scan_results
┌──────────────────┐
│ id               │  PK
│ repo_config_id   │  FK → repo_configs
│ score            │  综合评分 (0-100)
│ dimensions       │  JSONB（各维度评分明细）
│ suggestions      │  JSONB（改进建议列表）
│ scan_type        │  static / llm / full
│ commit_sha       │  扫描时的 commit
│ created_at       │
└──────────────────┘

sessions
┌──────────────────────┐
│ id                   │  PK (UUID，ae-cli 生成，直接作为主键)
│ repo_config_id       │  FK → repo_configs
│ branch               │  工作分支
│ relay_user_id        │  relay 用户 ID（可选）
│ relay_api_key_id     │  relay API Key ID（可选）
│ provider_name        │  provider 名称（可选）
│ tool_configs         │  JSONB（多工具 provider / key 映射）
│ started_at           │  session 开始时间
│ ended_at             │  session 结束时间（NULL 表示进行中）
│ tool_invocations     │  JSONB [{tool, start, end}, ...]
│ status               │  active / completed / abandoned
│ created_at           │
└──────────────────────┘

pr_records
┌──────────────────────┐
│ id                   │  PK
│ repo_config_id       │  FK → repo_configs
│ scm_pr_id            │  SCM 平台的 PR ID
│ scm_pr_url           │  PR 链接
│ author               │  PR 作者
│ title                │  PR 标题
│ source_branch        │  源分支
│ target_branch        │  目标分支
│ status               │  open / merged / closed
│ labels               │  TEXT[]（标签列表）
│ lines_added          │  新增行数
│ lines_deleted        │  删除行数
│ changed_files        │  JSONB（变更文件路径列表，gating file_patterns 用）
│ approvals            │  审批通过数（从 SCM API 获取）
│ merged_at            │  合并时间
│ cycle_time_hours     │  PR 创建到合并的小时数
│ session_ids          │  TEXT[]（关联的 ae-cli session ID 列表）
│ token_cost           │  关联的总 token 消耗
│ ai_ratio             │  AI 参与度 (0.0-1.0)
│ ai_label             │  ENUM: pending / ai_via_sub2api / no_ai_detected（默认 pending）
│ gating_result        │  JSONB（规则引擎评估结果）
│ created_at           │
│ updated_at           │
└──────────────────────┘

gating_rules
┌──────────────────┐
│ id               │  PK
│ repo_config_id   │  FK → repo_configs（NULL 表示全局规则）
│ name             │  规则名称
│ description      │  规则描述
│ condition        │  JSONB（规则条件表达式）
│ schema_version   │  条件 schema 版本号（默认 1，用于向前兼容）
│ action           │  pass / warn / block
│ enabled          │  是否启用
│ priority         │  执行优先级
│ created_at       │
│ updated_at       │
└──────────────────┘

efficiency_metrics
┌──────────────────────┐
│ id                   │  PK
│ repo_config_id       │  FK → repo_configs
│ user_id              │  FK → users（可选，NULL 表示 repo 级别聚合）
│ period_type          │  daily / weekly / monthly
│ period_start         │  统计周期起始
│ total_prs            │  总 PR 数
│ ai_prs               │  AI 辅助 PR 数
│ human_prs            │  纯人工 PR 数
│ avg_cycle_time_hours │  平均 Cycle Time
│ total_tokens         │  总 token 消耗
│ total_token_cost     │  总 token 成本
│ cost_per_line        │  每行代码的 token 成本
│ ai_vs_human_ratio    │  AI/人工 PR 比例
│ revert_rate_ai       │  AI PR revert 率
│ revert_rate_human    │  人工 PR revert 率
│ review_pass_rate_ai  │  AI PR review 通过率
│ review_pass_rate_human│ 人工 PR review 通过率
│ created_at           │
└──────────────────────┘
说明：user_id 为 NULL 时表示 repo 级别聚合，非 NULL 时表示该用户在该 repo 的个人指标。
团队维度通过 repo_config 的 group_id 聚合查询实现，不单独建表。

webhook_dead_letters
┌──────────────────┐
│ id               │  PK
│ repo_config_id   │  FK → repo_configs
│ delivery_id      │  SCM 平台的 delivery ID
│ event_type       │  事件类型
│ payload          │  JSONB（原始 payload）
│ error_message    │  处理失败的错误信息
│ retry_count      │  已重试次数
│ max_retries      │  最大重试次数（默认 3）
│ status           │  pending / retrying / failed / resolved
│ created_at       │
│ resolved_at      │
└──────────────────┘

users（效能平台本地用户表）
┌──────────────────┐
│ id               │  PK
│ username         │
│ email            │
│ auth_source      │  relay_sso / ldap
│ relay_user_id    │  关联 relay 用户（可选）
│ ldap_dn          │  LDAP DN（可选）
│ role             │  admin / user
│ created_at       │
│ updated_at       │
└──────────────────┘
```

### Relay 集成（当前实现）

**当前策略：优先通过 `relay.Provider` 和 relay/sub2api HTTP API 集成，而不是直接读取 sub2api 数据库。**

当前代码中的认证、LLM 调用、provider/API key 管理、session `tool_configs` 处理，都以 `backend/internal/relay.Provider` 为核心抽象。

历史上曾考虑通过 `database/sql` + 查询构建器直接读取 sub2api 数据库；这段设计在平台早期阶段有参考价值，但不再是当前实现的主合同。

当前应优先参考：
- `backend/internal/relay/provider.go`
- `backend/internal/auth/sso.go`
- `backend/internal/handler/provider.go`
- `docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md`
- `docs/superpowers/specs/2026-03-26-session-pr-attribution-design.md`

## SCM 插件架构

### 统一接口

```go
type SCMProvider interface {
    // 基础操作
    GetRepo(ctx context.Context, fullName string) (*Repo, error)
    ListRepos(ctx context.Context, opts ListOpts) ([]*Repo, error)
    CloneRepo(ctx context.Context, fullName, dest string) error

    // PR 操作
    CreatePR(ctx context.Context, req CreatePRRequest) (*PR, error)
    GetPR(ctx context.Context, repoFullName string, prID int) (*PR, error)
    ListPRs(ctx context.Context, repoFullName string, opts PRListOpts) ([]*PR, error)
    GetPRChangedFiles(ctx context.Context, repoFullName string, prID int) ([]string, error)
    GetPRApprovals(ctx context.Context, repoFullName string, prID int) (int, error)
    AddLabels(ctx context.Context, repoFullName string, prID int, labels []string) error
    SetPRStatus(ctx context.Context, req SetStatusRequest) error
    MergePR(ctx context.Context, repoFullName string, prID int, opts MergeOpts) error

    // Webhook 管理
    RegisterWebhook(ctx context.Context, repoFullName string, events []string, secret string) (webhookID string, err error)
    DeleteWebhook(ctx context.Context, repoFullName string, webhookID string) error
    ParseWebhookPayload(r *http.Request, secret string) (*WebhookEvent, error)

    // 文件操作
    GetFileContent(ctx context.Context, repoFullName, path, ref string) ([]byte, error)
    GetTree(ctx context.Context, repoFullName, ref string) ([]*TreeEntry, error)
    CreateBranch(ctx context.Context, repoFullName, branchName, baseSHA string) error
    CommitFiles(ctx context.Context, req CommitFilesRequest) (sha string, err error)
}
```

先实现 `github.Provider` 和 `bitbucket.Provider`，后续扩展只需新增一个实现。

### Webhook 事件统一模型

```go
type WebhookEvent struct {
    Type         EventType  // pr_opened, pr_merged, pr_updated, push
    RepoFullName string
    PR           *PRInfo
    Sender       string
    Raw          []byte
}
```

### Webhook 自动注入

Repo 配置流程中自动注册 webhook：

```
添加 Repo 配置
├─ 1. 验证 SCM 凭证 + repo 可访问性
├─ 2. 自动注册 webhook（PR events + push events）
├─ 3. 生成并存储 webhook_secret
├─ 4. 保存 repo_config（含 webhook_id）
└─ 5. 触发首次 AI 友好度扫描

删除 Repo 配置时自动清理 webhook
```

### Webhook 事件分发

webhook handler 接收事件后分发给各模块：
- `gating` 模块：PR 打开/更新时执行规则检查、打标签
- `efficiency` 模块：PR 合并时计算效能指标
- `analysis` 模块：push 到默认分支时触发 AI 友好度重新扫描

## 认证系统

```
┌──────────────────────────────────────────────────────┐
│                  Auth Middleware                      │
│  ┌─────────────┐  ┌──────────────┐  ┌────────────┐  │
│  │ sub2api SSO │  │  LDAP Auth   │  │ Dev Login  │  │
│  │  (TODO)     │  │              │  │ (debug)    │  │
│  │ 读取 sub2api│  │ ldap.Dial()  │  │ 无密码验证 │  │
│  │ user 表验证 │  │ Bind+Search  │  │ 自动创建   │  │
│  │ JWT token   │  │ 首次登录自动 │  │ admin 用户 │  │
│  │             │  │ 创建本地用户 │  │            │  │
│  └─────────────┘  └──────────────┘  └────────────┘  │
│         ↓                ↓               ↓           │
│       统一的 UserContext (user_id, role)              │
└──────────────────────────────────────────────────────┘
```

- sub2api SSO：**当前为 placeholder**（`sso.go` 的 `Authenticate` 返回 nil），待实现 sub2api 密码哈希验证
- LDAP：标准 LDAP bind 认证，首次登录时在效能平台自己的数据库创建用户记录
- Dev Login：开发模式专用（`AE_DEV_LOGIN_ENABLED=true`），`POST /api/v1/auth/dev-login` 接受任意用户名，自动创建 admin 用户并返回 JWT。仅 `server.mode=debug` 时可用
- 两种方式产出统一的 `UserContext`，后续中间件和 handler 不感知认证来源

## AI 友好度分析引擎

### 评分体系

```
AI 友好度总分 (0-100)
├── 静态规则检查 (权重 60%)
│   ├── AI 辅助文件 (20分)
│   │   ├── AGENTS.md 存在且内容完整
│   │   ├── CLAUDE.md / .cursorrules / .github/copilot-instructions.md
│   │   └── .editorconfig / .prettierrc 等格式化配置
│   ├── 项目结构 (15分)
│   │   ├── 目录结构清晰度（扁平 vs 深层嵌套）
│   │   ├── 模块化程度（单文件过大检测）
│   │   └── 命名规范一致性
│   ├── 文档完整度 (15分)
│   │   ├── README.md 存在且有效
│   │   ├── API 文档 / OpenAPI spec
│   │   └── 内联注释密度
│   └── 测试覆盖 (10分)
│       ├── 测试文件存在性
│       ├── CI 配置（GitHub Actions / Jenkinsfile 等）
│       └── 测试框架配置
│
└── LLM 辅助分析 (权重 40%)
    ├── 代码可读性评估
    ├── 模块间耦合度分析
    ├── 依赖管理健康度
    └── AI 协作改进建议
```

### 分析流程

```
配置 Repo → 触发扫描
              │
              ├─ 1. Clone/Pull 最新代码
              ├─ 2. 静态规则引擎扫描（快，秒级）
              ├─ 3. LLM 分析（通过 sub2api 路由，分钟级）
              ├─ 4. 综合评分 + 生成报告
              └─ 5. 可选：自动创建优化 PR
                    ├─ 创建 AGENTS.md
                    ├─ 补充 .editorconfig
                    └─ 其他建议的文件变更
```

### 自动优化 PR

分析完成后，如果发现可自动修复的问题（如缺少 AGENTS.md），系统自动创建分支、提交文件、发起 PR。PR 描述中包含 AI 友好度评分和改进说明。用户可在效能平台配置哪些优化项允许自动 PR，哪些只生成建议。

### 可配置 Scan Prompt

三层 fallback：`repo override > 全局配置 > 硬编码默认值`

全局配置（`config.yaml`）：
```yaml
analysis:
  llm:
    system_prompt: "You are a code quality analyzer. Respond ONLY with valid JSON."
    user_prompt_template: "Analyze the following repository...\n{repo_context}\n..."
```

Repo 级别 Override（`repo_configs` 表 `scan_prompt_override` JSON 字段）：
```go
type ScanPromptOverride struct {
    SystemPrompt       string `json:"system_prompt,omitempty"`
    UserPromptTemplate string `json:"user_prompt_template,omitempty"`
}
```

合并逻辑：`resolvePrompts(repoOverride)` — 硬编码默认值 → 全局配置覆盖 → repo 级别覆盖。模板引擎使用 `strings.ReplaceAll` 进行 `{repo_context}` 占位符替换。

API 变更：
- `GET/PUT /api/v1/settings/llm` — 增加 `system_prompt`, `user_prompt_template`
- `GET/PUT /api/v1/repos/:id` — 增加 `scan_prompt_override`

前端：Settings 页面 LLM 配置区域增加 System Prompt / User Prompt Template textarea；Repo 详情页增加 Scan Settings 折叠区域。

## Repo 上下文 LLM 对话

### 交互设计

- RepoDetailView 右下角浮动按钮，点击展开右侧 Chat 面板（约 400px 宽）
- 对话历史保持在 Vue ref 中，刷新页面清空（session 级别）
- 支持 loading 状态、错误提示、空状态引导

### API

```
POST /api/v1/repos/:id/chat
```

请求体：`{ "message": "...", "history": [{"role": "user/assistant", "content": "..."}] }`
响应：`{ "code": 200, "data": { "reply": "...", "tokens_used": 1234 } }`

### 后端处理流程

1. 加载 repo 基本信息 + 最新扫描结果
2. 从 clone 缓存读取文件树 + key files（缓存不存在则跳过，仅基于扫描结果回答）
3. 组装 system prompt（含 repo context：score、dimensions、suggestions、file tree、key files）
4. 将 history + 用户新消息发给 sub2api `/v1/chat/completions`
5. 返回 LLM 响应

### 限制与安全

- history 最多 20 条消息，超出截断最早的
- history role 仅允许 `user` 和 `assistant`
- 单条 message content 最大 4000 字符
- LLM `max_tokens` 设为 4096
- 每用户每小时最多 60 次 chat 请求（内存计数器，重启清零）
- Chat endpoint 需要 RequireAuth 中间件

### LLM 分析调用路径

效能平台 → sub2api API（使用专用 API Key）→ 上游 AI 服务。LLM 分析的 token 消耗也被 sub2api 追踪，形成闭环。

### LLM 分析预算控制

- 单次扫描 token 上限：可配置，默认 100K tokens
- 单 repo 每日扫描上限：可配置，默认 3 次
- 全局每日 token 预算：可配置，默认 1M tokens
- 超出预算时行为：跳过 LLM 分析部分，仅返回静态规则评分，记录告警
- 大型 repo 策略：超过 10K 文件时，LLM 分析仅采样核心目录（src/、lib/、pkg/ 等），不扫描全量代码

### Repo Clone 策略

- 克隆目标：`{DATA_DIR}/repos/{repo_config_id}/` 持久化缓存
- 首次扫描：shallow clone（`--depth 1`），仅拉取默认分支最新代码
- 后续扫描：`git fetch --depth 1 && git reset --hard origin/{branch}`
- 并发控制：同一 repo 的扫描任务串行执行（基于 repo_config_id 的分布式锁）
- 磁盘管理：定时任务清理超过 30 天未扫描的 repo 缓存
- LFS 处理：默认跳过 LFS 文件（`GIT_LFS_SKIP_SMUDGE=1`），不影响代码分析

## ae-cli：Session 追踪与 AI 工具调度器

### 核心职责

1. 管理开发 session（绑定 repo + branch）
2. 调度任意 AI 编码工具（codex, kiro, claude code, cursor, opencode 等）
3. 在 session 期间记录上下文到效能平台（不改动 sub2api）

### 工作流

```bash
$ ae-cli start                    # 自动检测当前 repo + branch
→ 创建 session，注册到效能平台
→ 记录 session_id, repo, branch, start_time
→ 记录当前 sub2api API Key（从环境变量或配置读取）

$ ae-cli run claude-code "fix the login bug"
$ ae-cli run codex "add unit tests"
$ ae-cli run kiro "refactor auth module"
→ 调度指定的 AI 工具执行任务
→ 记录每次调用的 start/end 时间戳到效能平台

$ ae-cli stop
→ 结束 session，记录 end_time
```

### 工具配置（历史基线）

```yaml
# ~/.ae-cli/config.yaml
server:
  url: "http://localhost:8081"    # 效能平台地址
  token: "jwt-token-here"        # 认证 token

sub2api:
  api_key_env: "SUB2API_API_KEY" # 从环境变量读取 API Key

tools:
  claude-code:
    command: "claude"
    args: ["-p"]
  codex:
    command: "codex"
    args: []
  kiro:
    command: "kiro-cli"
    args: []
  cursor:
    command: "cursor"
    args: ["--cli"]
  opencode:
    command: "opencode"
    args: []
  # 支持自定义工具
```

### PR 关联逻辑（历史基线，已被后续设计细化）

```
PR webhook 到达
     │
     ▼
查找匹配的 session：
WHERE repo = pr.repo
  AND branch = pr.source_branch
  AND provider_name / relay_api_key_id 与当前 session 归因规则匹配
     │
     ▼
用 session 的时间窗口查询 usage logs：
WHERE api_key_id = session.relay_api_key_id
  AND created_at BETWEEN session.started_at
      AND COALESCE(session.ended_at, NOW())
     │
     ▼
精确的 token 消耗 = 该 session 内的所有 usage_logs
（排除了并行开发其他 repo 的干扰）
```

**边界情况处理：**

- **Session 仍在进行中时 PR 已打开**：使用 `COALESCE(ended_at, NOW())` 查询当前已有的 token 消耗。PR 合并时重新计算最终值。
- **一个 PR 关联多个 session**：同一 repo + branch 可能有多个 session（开发者多次 start/stop），`pr_records.session_ids` 为数组，聚合所有匹配 session 的 token 消耗。
- **当前设计补充**：session / PR 归因规则已经在 `2026-03-26-session-pr-attribution-design.md` 中进一步细化；本文这一节只保留高层背景，不作为最新精确归因合同。
- **Session 超时自动关闭**：服务端定时任务每 30 分钟扫描，将超过 8h 无心跳的 active session 标记为 abandoned。ae-cli 每 5 分钟发送心跳。

### 标签策略

- `ai_via_sub2api`：存在匹配的 session 且有 sub2api token 消耗
- `no_ai_detected`：无匹配 session 或无 sub2api token 消耗记录

## PR Gating 规则引擎

### 工作流

```
SCM Webhook (PR opened/updated)
        │
        ▼
  Webhook Handler
        │
        ▼
  1. 识别 PR 来源（查找 repo_config，匹配 session）
        │
        ▼
  2. 自动打标签（基于 session 的 token 关联）
        │
        ▼
  3. 规则引擎评估（加载 gating_rules，逐条评估）
        │
        ▼
  4. 执行动作
     - pass: 设置 commit status = success
     - warn: status = success + PR comment
     - block: status = failure
```

### 规则条件语法（JSONB）

```json
// 示例：要求 PR 必须通过 sub2api 使用 AI 开发
{
  "operator": "AND",
  "conditions": [
    {"field": "ai_label", "op": "eq", "value": "ai_via_sub2api"},
    {"field": "token_cost", "op": "gt", "value": 0}
  ]
}

// 示例：大 PR 必须有人工 review
{
  "operator": "AND",
  "conditions": [
    {"field": "lines_changed", "op": "gt", "value": 500},
    {"field": "approvals", "op": "gte", "value": 1}
  ]
}
```

支持的字段：`ai_label`、`ai_ratio`、`token_cost`、`labels`、`lines_changed`（= lines_added + lines_deleted）、`approvals`（从 SCM API 实时获取并缓存到 pr_records）、`author`、`target_branch`、`file_patterns`（匹配 pr_records.changed_files）。

支持的操作：`eq`、`gt`、`gte`、`lt`、`lte`、`contains`、`matches`（正则）。

支持 `AND` / `OR` / `NOT` 组合。

## 效能指标体系

### PR 级别指标（实时，每个 PR 合并时计算）

- `token_cost`：该 PR 关联的总 token 消耗
- `cost_per_line`：token_cost / lines_changed
- `ai_ratio`：AI 参与度
- `cycle_time`：PR 创建到合并的小时数

### 聚合指标（定时任务，按 daily/weekly/monthly 聚合）

- `total_prs` / `ai_prs` / `human_prs`
- `avg_cycle_time` / `avg_cost_per_line`
- `total_token_cost`
- AI vs 人工 PR 质量对比：revert 率、review 通过率、bug fix 后续 PR 率
- 团队/开发者维度排行

### 趋势分析

- Token 消耗趋势（日/周/月）
- AI 采用率趋势
- 效率提升趋势（cycle time 变化）
- 异常检测（token 消耗突增/骤降告警）

### 联合 Repo 视图

- 依赖图可视化
- 跨 repo 变更影响范围分析
- 项目级别聚合指标

## HTTP API 规范

### 通用约定

- 基础路径：`/api/v1`
- 认证：`Authorization: Bearer <jwt-token>`
- 分页：`?page=1&page_size=20`，响应包含 `total`、`page`、`page_size`
- 错误响应格式：`{"code": 400, "message": "...", "details": {}}`
- 时间格式：RFC 3339（`2026-03-17T08:00:00Z`）

### 端点列表

| 方法 | 路径 | 说明 |
|------|------|------|
| **认证** | | |
| POST | `/api/v1/auth/login` | 登录（sub2api SSO 或 LDAP） |
| POST | `/api/v1/auth/dev-login` | 开发登录（debug 模式，`AE_DEV_LOGIN_ENABLED=true`） |
| POST | `/api/v1/auth/refresh` | 刷新 JWT token |
| GET | `/api/v1/auth/me` | 获取当前用户信息（RequireAuth） |
| **SCM Providers** | | |
| GET | `/api/v1/scm-providers` | 列出所有 SCM 实例 |
| POST | `/api/v1/scm-providers` | 创建 SCM 实例 |
| PUT | `/api/v1/scm-providers/:id` | 更新 SCM 实例 |
| DELETE | `/api/v1/scm-providers/:id` | 删除 SCM 实例 |
| POST | `/api/v1/scm-providers/:id/test` | 测试 SCM 连接 |
| **Repo 配置** | | |
| GET | `/api/v1/repos` | 列出已配置的 repo |
| POST | `/api/v1/repos` | 添加 repo（自动注册 webhook） |
| POST | `/api/v1/repos/direct` | 直接添加 repo（跳过 SCM 验证，dev 模式） |
| GET | `/api/v1/repos/:id` | 获取单个 repo 详情 |
| PUT | `/api/v1/repos/:id` | 更新 repo 配置 |
| DELETE | `/api/v1/repos/:id` | 删除 repo（自动清理 webhook） |
| POST | `/api/v1/repos/:id/scan` | 手动触发 AI 友好度扫描 |
| GET | `/api/v1/repos/:id/scans` | 获取扫描历史 |
| GET | `/api/v1/repos/:id/scans/latest` | 获取最新扫描结果 |
| POST | `/api/v1/repos/:id/optimize` | 一步式自动优化 |
| POST | `/api/v1/repos/:id/optimize/preview` | 预览优化变更 |
| POST | `/api/v1/repos/:id/optimize/confirm` | 确认并执行优化 |
| POST | `/api/v1/repos/:id/chat` | Repo 上下文 LLM 对话 |
| **联合 Repo** | | |
| GET | `/api/v1/repos/:id/dependencies` | 获取 repo 依赖关系 |
| PUT | `/api/v1/repos/:id/dependencies` | 更新依赖关系 |
| **Sessions (ae-cli 调用)** | | |
| POST | `/api/v1/sessions` | 创建 session（ae-cli start） |
| GET | `/api/v1/sessions` | 列出 sessions |
| GET | `/api/v1/sessions/:id` | 获取 session 详情 |
| PUT | `/api/v1/sessions/:id` | 更新 session（心跳、工具调用记录） |
| POST | `/api/v1/sessions/:id/stop` | 结束 session（ae-cli stop） |
| POST | `/api/v1/sessions/:id/invocations` | 记录工具调用 |
| **PR 记录** | | |
| GET | `/api/v1/repos/:id/prs` | 列出 PR 记录 |
| GET | `/api/v1/prs/:id` | 获取 PR 详情（含 token 消耗、gating 结果） |
| POST | `/api/v1/repos/:id/sync-prs` | 从 SCM 同步 PR 列表 |
| **Gating 规则** | | |
| GET | `/api/v1/repos/:id/gating-rules` | 列出 repo 的 gating 规则 |
| POST | `/api/v1/repos/:id/gating-rules` | 创建 gating 规则 |
| PUT | `/api/v1/gating-rules/:id` | 更新规则 |
| DELETE | `/api/v1/gating-rules/:id` | 删除规则 |
| GET | `/api/v1/gating-rules/global` | 列出全局规则 |
| **效能指标** | | |
| GET | `/api/v1/efficiency/dashboard` | 仪表盘概览数据 |
| GET | `/api/v1/efficiency/repos/:id/metrics` | repo 级别效能指标 |
| GET | `/api/v1/efficiency/repos/:id/trend` | repo 效能趋势 |
| GET | `/api/v1/efficiency/users/:id/metrics` | 用户级别效能指标 |
| GET | `/api/v1/efficiency/ranking` | 效能排行（按 repo/user） |
| POST | `/api/v1/efficiency/aggregate` | 手动触发效能聚合（admin only） |
| **LLM 配置** | | |
| GET | `/api/v1/settings/llm` | 获取 LLM 配置（API key 脱敏） |
| PUT | `/api/v1/settings/llm` | 更新 LLM 配置（热更新，无需重启） |
| POST | `/api/v1/settings/llm/test` | 测试 LLM 连接 |
| **Webhook 接收** | | |
| POST | `/api/v1/webhooks/github` | GitHub webhook 入口 |
| POST | `/api/v1/webhooks/bitbucket` | Bitbucket webhook 入口 |
| **系统** | | |
| GET | `/api/v1/health` | 健康检查 |
| **Webhook 死信队列** | | |
| GET | `/api/v1/admin/dead-letters` | 列出失败的 webhook 事件 |
| POST | `/api/v1/admin/dead-letters/:id/retry` | 重试失败事件 |

### 关键请求/响应示例

**创建 Session（ae-cli → 效能平台）：**
```json
POST /api/v1/sessions
{
  "id": "uuid-generated-by-cli",
  "repo_full_name": "org/repo-name",
  "branch": "feature-x",
  "tool_configs": [
    {
      "tool_name": "claude",
      "provider_name": "sub2api-claude",
      "relay_api_key_id": 123
    }
  ]
}
→ 201 {"id": "uuid", "status": "active", "started_at": "..."}
```

**Gating 规则评估结果（PR 详情中返回）：**
```json
{
  "gating_result": {
    "overall": "blocked",
    "rules": [
      {"rule_id": 1, "name": "require-ai", "result": "pass"},
      {"rule_id": 2, "name": "require-review", "result": "blocked", "reason": "0 approvals, need >= 1"}
    ]
  }
}
```

## 错误处理

### SCM API 调用失败
- 重试策略：指数退避，最多 3 次
- webhook 注册失败：标记 repo 状态为 `webhook_failed`，前端提示用户手动配置
- PR 操作失败：记录错误日志，不阻塞主流程

### sub2api 只读连接
- 连接断开：自动重连，效能指标计算降级（显示缓存数据）
- 查询超时：usage_logs 查询加 LIMIT + 分页
- 数据不一致：定时任务重新聚合修正

### ae-cli
- 网络断开：本地缓存 session 数据，恢复后同步
- 工具调度失败：记录错误，不影响 session 追踪
- session 异常结束：超时自动关闭（可配置，默认 8h）

### Webhook 处理
- 幂等性：基于 webhook delivery ID 去重
- 处理失败：写入死信队列，支持手动重试
- 顺序保证：同一 PR 的事件串行处理

## 安全考虑

- SCM 凭证加密存储（AES-256-GCM）
- webhook secret 随机生成，验证签名
- sub2api 只读连接使用受限权限的数据库用户
- LDAP 连接支持 TLS
- ae-cli 与效能平台通信使用 JWT 认证

## 数据库迁移策略


- 效能平台自有数据库使用 Ent 的内置自动迁移（`client.Schema.Create(ctx)`），和 sub2api 一致
- 生产环境可选切换到 Atlas（Ent 官方推荐的迁移工具），生成版本化的 SQL 迁移文件
- sub2api 只读连接不涉及迁移，仅在启动时做 schema 探测

## 分期交付计划


### Phase 1 — 核心框架（MVP）✅ 已完成（2026-03-20）
- 项目脚手架（Go + Vue 3，和 sub2api 一致）
- 双数据库连接（自有库读写 + sub2api 只读）
- 认证系统（LDAP + Dev Login；**sub2api SSO 为 placeholder，待实现密码哈希验证**）
- SCM Provider 插件接口 + GitHub 实现
- Repo 配置 CRUD + webhook 自动注入
- ae-cli 基础版（session 管理 + 工具调度）+ 扩展命令（ps/kill/attach/shell + tmux 集成）
- 效能平台 ↔ ae-cli 的 session API
- 健康检查端点

### Phase 2 — AI 分析与标签 ✅ 已完成（2026-03-20）
- AI 友好度静态规则引擎（权重 60%，满分 60 分）
- LLM 辅助分析（通过 sub2api 路由，权重 40%，满分 40 分）
- LLM 配置管理（前端 Settings 页面 + 热更新，无需重启）
- 自动优化 PR（创建 AGENTS.md 等，支持 preview + confirm 两步流程）
- PR 自动标签（基于 session 的 token 关联，数据库标签）
- PR 同步（从 SCM 拉取 PR 列表）
- Bitbucket Server SCM 插件实现
- 基础效能仪表盘
- Repo 上下文 LLM 对话功能

**Phase 2 已知缺失项（需后续补齐）：**
- LLM 预算控制未实现（token 上限、每日扫描上限、超出预算跳过 LLM）
- Push to default branch 不触发 AI 友好度重新扫描（webhook handler 只记日志）
- Labeler 不调用 SCM AddLabels（只更新数据库字段，PR 上无实际标签）
- efficiencymetric.user_id 未通过 Ent Edge 关联 User

### Phase 3 — Gating 与深度分析（待实施）
- PR Gating 规则引擎
- 完整效能指标体系（AI vs 人工对比、Cycle Time 等）
- 联合 Repo 依赖关系建模与影响分析
- 团队/个人效能排行与趋势
- 异常检测与告警
