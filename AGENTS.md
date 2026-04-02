# AI Efficiency Platform - Agent Working Rules

## Scope

AI Efficiency Platform（AI 效能平台）是一个独立于 `sub2api` 的系统，用于衡量和优化 AI 辅助开发效能。

- Backend: Go (`Gin` + `Ent`)
- Frontend: Vue 3 (`Vite` + `TailwindCSS` + `Pinia`)
- CLI: Go + `cobra`

## Source of Truth

当代码、旧文档、设计草案之间出现冲突时，按以下顺序决策：

1. 当前代码
2. 最新且最贴近问题域的 spec：
   - `docs/superpowers/specs/2026-04-02-local-session-proxy-design.md`
   - `docs/superpowers/specs/2026-03-26-session-pr-attribution-design.md`
   - `docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md`
3. `docs/architecture.md` 中的项目级架构图和模块说明
4. `docs/superpowers/specs/2026-03-17-ai-efficiency-platform-design.md` 作为历史基线

执行要求：

- 修改 `auth`、`relay`、`sessionbootstrap`、`checkpoint`、`attribution`、`hooks`、`collector`、`proxy` 相关逻辑前，先读对应 spec。
- 不要让历史文档里的旧设计覆盖最新合同。
- 如果你发现“代码已变，但文档还停留在旧说法”，应同时更新文档，而不是继续传播旧描述。

## Architecture Guardrails

- 本项目是模块化单体，不是微服务拆分仓库。优先维持清晰模块边界，而不是引入跨模块隐式耦合。
- `sub2api` 集成必须优先通过 `backend/internal/relay.Provider` 和 HTTP API 完成，不要重新引入 direct DB coupling。
- SCM 集成必须遵循 `backend/internal/scm.SCMProvider` 统一接口。
- Session / PR attribution 相关改动必须明确区分：
  - relay identity / user mapping
  - session bootstrap / runtime metadata
  - git checkpoint
  - attribution / usage aggregation
- `2026-04-02-local-session-proxy-design.md` 目前是设计草案；除非代码里已经实现，否则不要把 local session proxy 写成现状。
- 当前部署是前端构建产物在 Docker build 阶段打包进服务镜像；不要未经验证就声称“前端已编译进 Go 单二进制”。
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
│       ├── efficiency/      # 效能指标聚合
│       ├── oauth/           # OAuth 授权流
│       ├── prsync/          # PR 同步
│       ├── relay/           # Relay/sub2api 抽象
│       ├── repo/            # Repo 配置
│       ├── scm/             # SCM provider 接口与实现
│       ├── sessionbootstrap/ # ae-cli start 生命周期
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
- 样式使用 TailwindCSS utility classes
- 尽量把数据访问和状态转换放在 store / API 层，不要让 view 组件承担过多业务逻辑

### ae-cli

- 使用 `cobra` 组织命令
- 当前登录态存储在 `~/.ae-cli/token.json`
- legacy config 仍可能从 `~/.ae-cli/config.yaml` 读取；在明确迁移完成前不要假设它已经彻底移除
- 涉及 workspace marker、runtime bundle、hooks、collector 的行为时，先核对最新 session 相关 spec 与当前代码实现

## Documentation Rules

以下变更必须同步更新文档：

- 架构边界变化：更新 `docs/architecture.md`
- 合同/流程变化：更新对应 `docs/superpowers/specs/*.md`
- agent 协作规范变化：更新 `AGENTS.md`
- 轻量导航或引用入口变化：必要时同步 `CLAUDE.md`

特别要求：

- 若改动影响 login、OAuth、relay provider、session bootstrap、checkpoint、attribution、local proxy 方向，提交里必须明确这是“当前实现变更”还是“设计文档更新”。
- 不要只改代码不改 spec，也不要只改 spec 却继续让 `AGENTS.md` 保留过时约束。

## Testing

- 后端单元测试：`cd backend && go test ./...`
- ae-cli 测试：`cd ae-cli && go test ./...`
- 前端测试：`cd frontend && pnpm test`
- 环境敏感测试（本地端口监听、TTY、tmux、浏览器/E2E）需与默认单元测试结果分开说明

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
- `docs/superpowers/specs/2026-03-17-ai-efficiency-platform-design.md` — 平台历史基线设计文档
- `docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md` — relay/OAuth/provider 基础设计
- `docs/superpowers/specs/2026-03-26-session-pr-attribution-design.md` — session / PR attribution 当前主设计
- `docs/superpowers/specs/2026-04-02-local-session-proxy-design.md` — local session proxy 草案
- `backend/internal/scm/provider.go` — SCM Provider 统一接口定义
- `backend/internal/relay/provider.go` — relay Provider 统一接口定义
- `backend/ent/schema/` — 所有数据模型定义
