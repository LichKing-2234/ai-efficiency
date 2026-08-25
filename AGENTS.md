# AI Efficiency Platform - Agent Working Rules

## Scope

AI Efficiency Platform（AI 效能平台）是一个独立于 `sub2api` 的系统，用于衡量和优化 AI 辅助开发效能。

- Backend: Go (`Gin` + `Ent`)
- Frontend: Vue 3 (`Vite` + `Element Plus` + `TailwindCSS` + `Pinia`)
- CLI: Go + `cobra`

## Source of Truth

当代码、旧文档、设计草案之间出现冲突时，按以下顺序决策：

1. 当前代码
2. 最新且最贴近问题域的 spec：
   - `docs/superpowers/specs/2026-08-25-list-and-pagination-consistency-design.md`
   - `docs/superpowers/specs/2026-08-24-usage-window-preference-design.md`
   - `docs/superpowers/specs/2026-08-24-relay-replan-baseline-roster-design.md`
   - `docs/superpowers/specs/2026-08-19-relay-group-mapping-contract.md`
   - `docs/superpowers/specs/2026-08-11-codex-commit-token-attribution-v2-design.md`
   - `docs/superpowers/specs/2026-08-19-personal-usage-reset-and-oauth-pool-design.md`
   - `docs/superpowers/specs/2026-08-05-codex-token-attribution-ledger-poc-design.md`
   - `docs/superpowers/specs/2026-07-25-stateless-team-usage-prewarm-worker-design.md`
   - `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md`
   - `docs/superpowers/specs/2026-06-22-configurable-directory-sync-design.md`
   - `docs/superpowers/specs/2026-05-19-ae-cli-deterministic-tool-configuration-design.md`
   - `docs/superpowers/specs/2026-05-14-legacy-session-staged-cutover-design.md`
   - `docs/superpowers/specs/2026-05-13-sessionless-local-tool-attribution-design.md`
   - `docs/superpowers/specs/2026-04-15-oauth-device-login-design.md`
   - `docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md`
3. `docs/architecture.md` 中的项目级架构图和模块说明
4. `docs/superpowers/specs/2026-03-17-ai-efficiency-platform-design.md` 作为历史基线

执行要求：

- 修改 `auth`、`relay`、`checkpoint`、`attribution`、`hooks`、`collector`、legacy session compatibility、`proxy` 相关逻辑前，先读对应 spec。
- 修改前端加载编排、Redis 业务缓存、Team Overview 拆分、静态资源 serving、HTTP timeout/readiness 或性能可观测性前，先读 `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md`。
- 修改 Team Usage prewarm worker、`backend/cmd/prewarmer`、prewarm Redis manifest/generation，或 backend 侧 prewarm 读取与 fallback 前，先读 `docs/superpowers/specs/2026-07-25-stateless-team-usage-prewarm-worker-design.md`。它是 Team Usage 冷路径的当前合同，取代 07-14 中的对应部分。
- 不要让历史文档里的旧设计覆盖最新合同。
- 如果你发现“代码已变，但文档还停留在旧说法”，应同时更新文档，而不是继续传播旧描述。
- `docs/architecture.md` 必须始终反映**当前最新**的项目级架构、运行时关系和模块边界。
- `docs/superpowers/specs/*.md` 允许保留其写作当时的设计背景、取舍和演进轨迹；不要为了“跟上最新代码”而把历史 spec 全量重写成现状。
- 处理文档漂移时，优先判断这是：
  - 项目级当前状态变化：更新 `docs/architecture.md`
  - 当前生效合同变化：更新对应最新 spec
  - 新 spec 与旧 spec 存在冲突或继承关系：在**新的 spec** 中明确说明并关联历史 spec；不要回写历史 spec 正文

## Context Loading Discipline

后续 agent 不应一次性读取所有 specs/plans，也不应凭历史记忆重造工具。按任务域渐进加载上下文：

- 先用 `docs/architecture.md` 判断当前运行时边界和模块归属。
- 只读取与本次改动直接相关的最新 spec；不要把历史 spec 当作当前实现合同。
- 新增 helper、接口、CLI 命令、前端工具函数前，先用 `rg` 检查现有模块是否已有可扩展入口。
- 优先复用已有边界：`backend/internal/relay.Provider`、`backend/internal/scm.SCMProvider`、`ae-cli/internal/toolconfig`、`ae-cli/internal/doctorcheck`、`ae-cli/internal/hooks`、`ae-cli/internal/attributionlocal`、`frontend/src/api`、`frontend/src/utils/userSetupReview.ts`。
- 如果发现规则、spec、架构文档与当前代码不一致，按 Source of Truth 处理，并只更新最贴近当前合同的文档入口。

## Architecture Guardrails

- 本项目是模块化单体，不是微服务拆分仓库。优先维持清晰模块边界，而不是引入跨模块隐式耦合。
- `sub2api` 集成必须优先通过 `backend/internal/relay.Provider` 和 HTTP API 完成，不要重新引入 direct DB coupling。
- SCM 集成必须遵循 `backend/internal/scm.SCMProvider` 统一接口。
- Session / PR attribution 相关改动必须明确区分：
  - relay identity / user mapping
  - workspace marker / legacy runtime metadata
  - git checkpoint
  - attribution / usage aggregation
- `2026-04-02-local-session-proxy-design.md` 目前是设计草案；除非代码里已经实现，否则不要把 local session proxy 写成现状。
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
└── docs/                    # 架构与 specs
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
- 涉及 workspace marker、hooks、collector 的行为时，先核对最新 session 相关 spec 与当前代码实现

## Documentation Rules

以下变更必须同步更新文档：

- 架构边界变化：更新 `docs/architecture.md`
- 合同/流程变化：更新对应 `docs/superpowers/specs/*.md`
- agent 协作规范变化：更新 `AGENTS.md`
- 轻量导航或引用入口变化：必要时同步 `CLAUDE.md`

特别要求：

- 若改动影响 login、OAuth、relay provider、legacy session compatibility、checkpoint、attribution、local proxy 方向，提交里必须明确这是“当前实现变更”还是“设计文档更新”。
- 不要只改代码不改 spec，也不要只改 spec 却继续让 `AGENTS.md` 保留过时约束。
- 不要把所有旧 spec 都机械同步到最新实现；要保留架构和设计的演进脉络。
- 一旦某份 spec 成为历史设计记录，后续演进应优先写入新的 spec，并由新的 spec 解释它与历史 spec 的关系；不要反向修改历史 spec 来追最新实现。

### Test and Example Data Hygiene

- 测试、fixture、spec、plan、示例 JSON/命令输出中**不要写入真实用户数据、真实公司域名邮箱、真实密码、真实 token、真实 API key、真实订阅组名**。
- 统一使用脱敏占位值，例如：
  - 邮箱：`alice@example.com`、`bob@example.org`
  - 用户名：`alice`、`bob`
  - 密码：`test-password`
  - group 名：`Group Alpha`、`Group Beta`
- 如果为了调试临时生成脚本、token、密文转换工具，只能放在临时目录或会话级临时文件中；**不要**把这类调试脚本留在仓库待提交状态。
- 若发现仓库中的旧测试或历史文档包含真实样例，在不影响当前任务边界的前提下，优先一并做脱敏清理；若范围过大，至少不要继续复制、扩散这些真实值到新的文件和改动中。

### Plan Tracking

当仓库中已经存在 `docs/superpowers/plans/*.md` 且当前工作与该 plan 对应时，执行 agent 必须把 plan 当作**活的执行台账**维护，而不是只在最后补文档。

- 每完成一个 step，必须在同一轮工作中及时更新对应 checkbox；不要等到整份 plan 做完后再一次性回填。
- 只有在**本轮实际完成**对应动作后才能勾选该 step。尤其是测试、build、手动验收、环境验证类 step，没有实际跑过就必须保持未勾选。
- 如果代码主体已完成，但仍有手动验收、环境敏感验证、外部依赖验证未跑，必须在 plan 顶部明确写出当前状态与剩余未勾选项；不要让顶部 `Status` 与下方 checkbox 相互矛盾。
- 若某份 plan 已被替代、暂停、或只作为历史记录保留，必须在顶部显式标注 `Status` / `Replay Status`，避免执行者误以为 checkbox 只是忘了更新。
- plan 的状态说明语言必须与文档主体的主语言保持一致。英文 plan 的 `Status`、`Replay Status`、`Known Remaining Gaps` 等说明也应使用英文；中文 plan 则使用中文。除非有明确理由，不要在同一份 plan 中混用中英文状态说明。

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
docs(specs): update session attribution architecture references
refactor(backend): extract SCM provider interface
test(analysis): add unit tests for static rule engine
chore(deploy): update Docker Compose configuration
```

## Important Files

- `docs/architecture.md` — 项目级架构总览与图示
- `docs/superpowers/specs/2026-08-25-list-and-pagination-consistency-design.md` — 当前列表工作面与分页一致性合同：区分索引分页、游标翻页和分支增量加载，统一完整页面与嵌入式集合的响应式和边界行为
- `docs/superpowers/specs/2026-08-24-usage-window-preference-design.md` — 当前浏览器级用量窗口偏好合同：个人、团队和成员用量共享最近一次 Today / 7 Days / 30 Days 选择，Activity 继续由 URL 管理
- `docs/superpowers/specs/2026-08-24-relay-replan-baseline-roster-design.md` — 当前 Replan 初始成员合同：只恢复上次 Confirm 名单，其他候选人不选中、不分配、不推荐
- `docs/superpowers/specs/2026-08-19-relay-group-mapping-contract.md` — 当前 Relay 部门 x Platform 映射维护、Account 关系、成员迁移、确认指纹与失败重试合同
- `docs/superpowers/specs/2026-08-11-codex-commit-token-attribution-v2-design.md` — 2026-08-12 cutover 后生效的 production v2 合同：sub2api 官方 Token、确定性 commit proof、90 天 hot claim、长期 usage pool、Activity/Usage/Repos 页面边界与 #252 稳定窗口清理门禁
- `docs/superpowers/specs/2026-08-05-codex-token-attribution-ledger-poc-design.md` — 已完成 reset 的历史 compact POC 合同；仅用于解释 v1 JSONL Token、OTLP correlation、bucket/revision 的设计背景，不再代表当前 formal Activity
- `docs/ui-guidelines.md` — 当前前端组件库、响应式布局、图表和体积预算合同
- `docs/superpowers/specs/2026-07-25-stateless-team-usage-prewarm-worker-design.md` — 当前 Team Usage prewarm worker、Redis generation/manifest 与 backend 只读 fallback 主设计
- `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md` — 当前全站加载性能、Redis read model、Team Overview 拆分和 serving/observability 主设计
- `docs/superpowers/specs/2026-06-22-configurable-directory-sync-design.md` — 当前可配置组织架构同步、部门视图、离职禁用与 token 失效主设计
- `docs/superpowers/specs/2026-05-19-ae-cli-deterministic-tool-configuration-design.md` — 当前 `ae-cli discover` / tool config 主设计
- `docs/superpowers/specs/2026-05-14-legacy-session-staged-cutover-design.md` — 当前 legacy session cutover 主设计
- `docs/superpowers/specs/2026-05-13-sessionless-local-tool-attribution-design.md` — 当前 sessionless attribution 主设计
- `docs/superpowers/specs/2026-03-17-ai-efficiency-platform-design.md` — 平台历史基线设计文档
- `docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md` — relay/OAuth/provider 基础设计
- `docs/superpowers/specs/2026-03-26-session-pr-attribution-design.md` — session / PR attribution 当前主设计
- `docs/superpowers/specs/2026-04-02-local-session-proxy-design.md` — local session proxy 草案
- `backend/internal/scm/provider.go` — SCM Provider 统一接口定义
- `backend/internal/relay/provider.go` — relay Provider 统一接口定义
- `backend/ent/schema/` — 所有数据模型定义
