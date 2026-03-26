# AI Efficiency Platform - AI Agent Guidelines

## Project Overview

AI Efficiency Platform（AI 效能平台）是一个与 sub2api 平行的独立系统，用于衡量和优化 AI 辅助开发的效能。技术栈：Go (Gin + Ent) 后端 + Vue 3 (Vite + TailwindCSS + Pinia) 前端。

## Project Structure

```
ai-efficiency/
├── backend/          # Go 后端（Gin + Ent ORM）
│   ├── cmd/server/   # 入口
│   ├── ent/schema/   # 数据模型定义
│   └── internal/     # 业务逻辑
│       ├── auth/     # 认证（relay SSO + LDAP）
│       ├── scm/      # SCM 插件架构（GitHub, Bitbucket Server）
│       ├── repo/     # Repo 配置管理
│       ├── analysis/ # AI 友好度分析引擎
│       ├── gating/   # PR Gating 规则引擎
│       ├── efficiency/ # 效能指标计算
│       ├── relay/    # relay/sub2api 统一抽象
│       ├── oauth/    # OAuth 授权与 token 流程
│       ├── webhook/  # Webhook 事件处理
│       ├── handler/  # HTTP handlers
│       └── pkg/      # 公共工具包
├── frontend/         # Vue 3 前端
├── ae-cli/           # 轻量 CLI 工具（session 追踪 + AI 工具调度）
├── deploy/           # Docker Compose 部署
└── docs/superpowers/specs/ # 设计文档
```

## Coding Conventions

### Go Backend
- 遵循 Go 标准项目布局，业务逻辑放在 `internal/`
- 使用 Ent ORM 管理自有数据库 schema
- 当前 relay/sub2api 集成优先通过 `backend/internal/relay.Provider` 和 HTTP API 完成
- 错误处理：使用 `fmt.Errorf("context: %w", err)` 包装错误
- 日志：使用 zap structured logging
- 接口优先：模块间通过 interface 解耦（如 SCMProvider 接口）

### Vue Frontend
- 组件使用 `<script setup lang="ts">` + Composition API
- 状态管理使用 Pinia stores
- 样式使用 TailwindCSS utility classes
- API 调用封装在 `src/api/` 目录

### ae-cli
- Go CLI，使用 cobra 框架
- 当前登录态存储：`~/.ae-cli/token.json`
- legacy config 仍可能从 `~/.ae-cli/config.yaml` 读取；在明确迁移前不要假设它已完全移除

## Commit Message Convention

使用 Conventional Commits 规范：

```
<type>(<scope>): <subject>

<body>

<footer>
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
```
feat(scm): add GitHub provider implementation
fix(gating): handle nil session in PR evaluation
docs(specs): update data model with webhook_dead_letters table
refactor(backend): extract SCM provider interface
test(analysis): add unit tests for static rule engine
chore(deploy): update Docker Compose configuration
```

## Key Design Decisions

- 独立数据库 `ai_efficiency`，当前 relay/sub2api 集成优先走 HTTP API 与 `relay.Provider`
- SCM 集成使用插件架构（统一 SCMProvider 接口）
- PR 与 token 消耗通过 ae-cli session 精确关联
- PR Gating 使用 JSONB 条件表达式的可配置规则引擎
- 前端嵌入后端二进制，单二进制部署

## Testing

- 后端单元测试：`cd backend && go test ./...`
- ae-cli 测试：`cd ae-cli && go test ./...`
- 前端测试：`cd frontend && pnpm test`
- 环境敏感测试（本地端口监听、TTY、tmux、浏览器/E2E）需与默认单元测试结果分开说明

## Important Files

- `docs/superpowers/specs/2026-03-17-ai-efficiency-platform-design.md` — 平台历史基线设计文档
- `docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md` — relay/OAuth 基础设计
- `docs/superpowers/specs/2026-03-26-session-pr-attribution-design.md` — 最新 session / PR 归因设计草案
- `backend/internal/scm/provider.go` — SCM Provider 统一接口定义
- `backend/internal/relay/provider.go` — relay Provider 统一接口定义
- `backend/ent/schema/` — 所有数据模型定义
