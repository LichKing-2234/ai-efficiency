# AI Efficiency Platform - Agent Working Rules

## Scope

AI Efficiency Platform（AI 效能平台）是一个独立于 `sub2api` 的系统，用于衡量和优化 AI 辅助开发效能。

- Backend: Go (`Gin` + `Ent`)
- Frontend: Vue 3 (`Vite` + `Element Plus` + `TailwindCSS` + `Pinia`)
- CLI: Go + `cobra`

## Source of Truth

当代码、合同、架构说明之间出现冲突时，按以下顺序决策：

1. 当前代码
2. `docs/contracts/` 下与问题域直接相关的当前合同
3. `docs/architecture.md` 中的当前项目级架构图和模块说明

其他文档角色：

- 未实现的目标状态只由开放的 GitHub Issue/spec 管理，不写入仓库合同。
- 独立且长期有效的架构取舍在确有必要时写入 ADR。
- `docs/history/` 只保留不可替代的历史理由和证据，既不是当前行为合同，也不是待办列表。
- 根目录 `CONTEXT.md` 在已跟踪且与任务相关时定义领域词汇；本地未跟踪副本不属于仓库事实。
- 仓库不维护执行 plan 台账；开放工作、依赖和状态统一由 GitHub 管理。

修改前按触发条件只读取相关合同：

- 修改认证或 OAuth：`docs/contracts/auth-and-oauth.md`
- 修改 Relay 访问、onboarding 或订阅：`docs/contracts/relay-user-access.md`
- 修改 CLI 工具配置：`docs/contracts/cli-tool-configuration.md`
- 修改 repository binding：`docs/contracts/repository-binding.md`
- 修改 release unit 或发布发现：`docs/contracts/release-units.md`
- 修改 Directory 同步或离职处理：`docs/contracts/directory-sync.md`
- 修改 Usage、团队用量或 quota：`docs/contracts/usage-and-quota.md`
- 修改 quota reset：`docs/contracts/quota-reset.md`
- 修改前端加载、Redis 业务缓存、静态资源 serving、HTTP timeout/readiness 或性能可观测性：`docs/contracts/platform-loading.md`
- 修改 Team Usage prewarm worker、`backend/cmd/prewarmer`、prewarm Redis manifest/generation 或 backend fallback：`docs/contracts/team-usage-prewarm.md`
- 修改 attribution、checkpoint、hooks 或 collector：`docs/contracts/attribution-v2.md`
- 修改 Relay Planning、Group mapping、Replan 或 Account 关系：`docs/contracts/relay-group-mapping.md`
- 修改分页、游标、列表响应式或集合导航：`docs/contracts/collection-navigation.md`

执行要求：

- 不要让历史文档覆盖当前代码、合同或架构。
- 如果你发现“代码已变，但文档还停留在旧说法”，应同时更新文档，而不是继续传播旧描述。
- `docs/architecture.md` 必须始终反映**当前最新**的项目级架构、运行时关系和模块边界。
- 处理文档漂移时，优先判断这是：
  - 项目级当前状态变化：更新 `docs/architecture.md`
  - 当前生效合同变化：更新对应 `docs/contracts/*.md`
  - 未实现的目标状态：更新 GitHub Issue/spec
  - 独立的长期设计理由：在确有必要时新增或更新 ADR

## Context Loading Discipline

后续 agent 不应一次性读取所有合同或历史记录，也不应凭历史记忆重造工具。按任务域渐进加载上下文：

- 先用 `docs/architecture.md` 判断当前运行时边界和模块归属。
- 只读取与本次改动直接相关的当前合同；历史记录仅在需要理由或证据时读取。
- 新增 helper、接口、CLI 命令、前端工具函数前，先用 `rg` 检查现有模块是否已有可扩展入口。
- 优先复用已有边界：`backend/internal/relay.Provider`、`backend/internal/scm.SCMProvider`、`ae-cli/internal/toolconfig`、`ae-cli/internal/doctorcheck`、`ae-cli/internal/hooks`、`ae-cli/internal/attributionlocal`、`frontend/src/api`、`frontend/src/utils/userSetupReview.ts`。
- 如果发现规则、合同、架构文档与当前代码不一致，按 Source of Truth 处理，并只更新最贴近当前事实的文档入口。

## Architecture Guardrails

- 本项目是模块化单体，不是微服务拆分仓库。优先维持清晰模块边界，而不是引入跨模块隐式耦合。
- `sub2api` 集成必须优先通过 `backend/internal/relay.Provider` 和 HTTP API 完成，不要重新引入 direct DB coupling。
- SCM 集成必须遵循 `backend/internal/scm.SCMProvider` 统一接口。
- Session / PR attribution 相关改动必须明确区分：
  - relay identity / user mapping
  - workspace marker / legacy runtime metadata
  - git checkpoint
  - attribution / usage aggregation
- Platform Sessions 和 local session proxy 已退役；不要根据历史记录把它们重新写成当前实现或目标状态。
- 当前部署是前端构建产物在 Docker build 阶段嵌入后端二进制，由后端进程同时对外提供 API 与前端静态页面。
- 不要修改 `sub2api` 源码，也不要假设它与本仓库同生命周期部署。

## Project Structure

```text
ai-efficiency/
├── backend/
│   ├── cmd/server/          # 服务入口
│   ├── ent/schema/          # Ent 数据模型
│   └── internal/            # 业务模块
│       ├── analysis/        # AI 友好度分析
│       ├── attribution/     # PR / session 归因
│       ├── auth/            # Relay SSO + LDAP
│       ├── checkpoint/      # Commit checkpoint
│       ├── directorysync/   # Configurable organization directory sync
│       ├── efficiency/      # 效能指标聚合
│       ├── oauth/           # OAuth 授权流
│       ├── prsync/          # PR 同步
│       ├── relay/           # Relay/sub2api 抽象
│       ├── repo/            # Repo 配置
│       ├── scm/             # SCM provider 接口与实现
│       ├── webhook/         # Webhook 处理
│       ├── handler/         # HTTP handlers
│       └── middleware/      # 中间件
├── frontend/src/            # Vue 应用
├── ae-cli/internal/         # CLI runtime / hooks / collector / client
├── deploy/                  # Docker Compose 和镜像构建
└── docs/                    # 架构、当前合同与历史记录
```

## Coding Conventions

### Go Backend

- 遵循 Go 标准项目布局，业务逻辑放在 `internal/`
- 使用 Ent ORM 管理 schema
- 错误处理使用 `fmt.Errorf("context: %w", err)`
- 日志使用 zap structured logging
- handler 保持薄层，业务逻辑尽量落到 service/module
- 模块解耦优先通过 interface，而不是直接跨包操作内部细节

### Vue Frontend

- 组件使用 `<script setup lang="ts">` + Composition API
- 状态管理使用 Pinia stores
- API 调用封装在 `src/api/`
- 通用交互组件直接使用 Element Plus，并通过 Vite resolver 按需自动导入；禁止全量注册和只做转发的镜像 wrapper
- 通用操作图标使用 `@element-plus/icons-vue`，不要重复手写已有图标
- 响应式布局和页面组合继续使用 TailwindCSS utility classes；图表继续使用现有 Chart.js 组件
- 具体选型、尺寸、响应式和体积预算遵循 `docs/ui-guidelines.md`
- 尽量把数据访问和状态转换放在 store / API 层，不要让 view 组件承担过多业务逻辑

### ae-cli

- 使用 `cobra` 组织命令
- 当前登录态存储在 `~/.ae-cli/token.json`
- legacy config 仍可能从 `~/.ae-cli/config.yaml` 读取；在明确迁移完成前不要假设它已经彻底移除
- 涉及 workspace marker、hooks、collector 的行为时，先核对 `docs/contracts/attribution-v2.md` 与当前代码实现

## Documentation Rules

以下变更必须同步更新文档：

- 架构边界变化：更新 `docs/architecture.md`
- 当前行为合同变化：更新对应 `docs/contracts/*.md`
- 未实现工作、依赖或状态变化：更新 GitHub Issue/spec
- 历史理由或证据变化：只在确有不可替代内容时更新 `docs/history/*.md`
- agent 协作规范变化：更新 `AGENTS.md`
- 轻量导航或引用入口变化：必要时同步 `CLAUDE.md`

特别要求：

- 若改动影响 login、OAuth、relay provider、checkpoint 或 attribution，提交里必须明确这是“当前实现变更”还是“合同/架构文档更新”。
- 不要只改代码不改当前合同，也不要只改合同却继续让 `AGENTS.md` 保留过时约束。
- 历史记录保留演进脉络，不追随当前实现，也不承载新待办。

### Test and Example Data Hygiene

- 测试、fixture、合同、历史记录、示例 JSON/命令输出中**不要写入真实用户数据、真实公司域名邮箱、真实密码、真实 token、真实 API key、真实订阅组名**。
- Git 测试仓库必须在首次 commit、rewrite、merge、cherry-pick 或 push 前配置 test-owned empty `core.hooksPath`；只有显式验证 managed hooks 的测试可以启用 hook，且必须同时隔离 `HOME`、Git 配置、凭据、backend 和子进程。
- 统一使用脱敏占位值，例如：
  - 邮箱：`alice@example.com`、`bob@example.org`
  - 用户名：`alice`、`bob`
  - 密码：`test-password`
  - group 名：`Group Alpha`、`Group Beta`
- 如果为了调试临时生成脚本、token、密文转换工具，只能放在临时目录或会话级临时文件中；**不要**把这类调试脚本留在仓库待提交状态。
- 若发现仓库中的旧测试或历史文档包含真实样例，在不影响当前任务边界的前提下，优先一并做脱敏清理；若范围过大，至少不要继续复制、扩散这些真实值到新的文件和改动中。

## Testing

- 后端单元测试：`cd backend && go test ./...`
- ae-cli 默认测试：`cd ae-cli && go test ./...`
- 前端单元测试：`cd frontend && npm test`
- 前端角色回归脚本：`cd frontend && npm run test:e2e:role`
- 环境敏感测试（本地端口监听、TTY、tmux、浏览器/E2E）需与默认单元测试结果分开说明

## Release Units

- Platform releases use `v*` tags and cover backend, frontend, deploy assets, GHCR image publishing, and Helm rollout inputs.
- CLI releases use `ae-cli/v*` tags and publish only `ae-cli` artifacts.
- Do not create a platform `v*` tag for CLI-only changes.
- Do not run Helm rollout for CLI-only `ae-cli/v*` releases.
- Repository-level `/releases/latest` belongs to the platform release line; CLI installer and updater must discover the latest CLI release by filtering `ae-cli/v*` releases.
- `v0.2.0-cli.1` is the only bridge exception for legacy CLI update migration. It must be published by the CLI bridge workflow only, must not build GHCR images or backend bundles, and must not be reused as a normal CLI version line.

## Commit Message Convention

使用 Conventional Commits：

```text
<type>(<scope>): <subject>
```

### Type

- `feat`: 新功能
- `fix`: Bug 修复
- `docs`: 文档变更
- `refactor`: 重构（不改变功能）
- `test`: 测试相关
- `chore`: 构建、CI、依赖等杂项
- `perf`: 性能优化

### Scope

- `backend`, `frontend`, `ae-cli`, `deploy`, `docs`
- 或更细粒度：`scm`, `auth`, `gating`, `analysis`, `efficiency`, `webhook`

### Examples

```text
feat(scm): add GitHub provider implementation
fix(gating): handle nil session in PR evaluation
docs(contracts): update attribution architecture references
refactor(backend): extract SCM provider interface
test(analysis): add unit tests for static rule engine
chore(deploy): update Docker Compose configuration
```

## Important Files

- `docs/architecture.md` — 项目级架构总览与图示
- `docs/contracts/README.md` — 当前行为合同索引与读取触发条件
- `docs/history/README.md` — 非权威历史理由与证据索引
- `docs/ui-guidelines.md` — 当前前端组件库、响应式布局、图表和体积预算合同
- `backend/internal/scm/provider.go` — SCM Provider 统一接口定义
- `backend/internal/relay/provider.go` — relay Provider 统一接口定义
- `backend/ent/schema/` — 所有数据模型定义
