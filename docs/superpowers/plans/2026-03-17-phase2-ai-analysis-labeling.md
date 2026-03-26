# Phase 2: AI 分析与标签 — 实施计划

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Updated:** 2026-03-24 — 基于代码审查同步 plan 与实际实现，标注偏差和缺失项

**Goal:** 在 Phase 1 核心框架基础上，实现 AI 友好度分析、PR 自动标签、基础效能仪表盘。

**Architecture:** 扩展 Phase 1 的模块化单体架构，新增 analysis、efficiency 模块。静态规则引擎 + LLM 辅助分析双引擎评分。PR 标签通过 session-token 关联实现。

**Tech Stack:** Go 1.26+ (Gin, Ent ORM, squirrel), Vue 3 (Vite, TailwindCSS, Pinia), sub2api API (LLM 路由)

**Status:** ✅ 主体已完成（2026-03-20）；缺口需另行补齐

**Replay Status:** 历史完成记录，不适合直接按本文逐 task 重跑。若要补齐缺口或重新执行，应基于当前代码和最新 spec 拆分成新的执行计划。

**Source Of Truth:** 已实现的分析、效能和 PR 数据链路以当前代码为准。本文中仍引用旧 `sub2api_*` 归因逻辑的部分，仅代表当时计划，不应覆盖当前代码或最新 spec。

**Known Stale Sections:** LLM 设置 UI 中的 `Sub2api URL/API Key`、旧 labeler 的 `sub2api_api_key_id` 归因逻辑、以及 `sub2apidb` 相关描述均已过时。

---

## File Structure

> **审查说明（2026-03-24）：** 以下文件结构与实际实现基本一致。已知偏差用 `⚠️` 标注。

### Backend (新增)

```
backend/
├── ent/schema/
│   ├── aiscanresult.go              # AI 扫描结果 entity
│   ├── prrecord.go                  # PR 记录 entity（⚠️ ai_label 多了 pending 默认值）
│   └── efficiencymetric.go          # 效能指标聚合 entity（⚠️ user_id 无 Edge 关联 User）
├── internal/
│   ├── analysis/
│   │   ├── scanner.go               # Scanner 接口定义（⚠️ StaticScanner 拆分到 static.go）
│   │   ├── static.go                # ⚠️ 额外：StaticScanner 实现
│   │   ├── service.go               # 分析服务，协调扫描流程
│   │   ├── cloner.go                # Repo shallow clone + 缓存管理
│   │   ├── optimizer.go             # 自动优化 PR 生成
│   │   ├── rules/
│   │   │   ├── types.go             # ⚠️ 额外：DimensionScore/Suggestion 类型定义
│   │   │   ├── ai_files.go          # AI 辅助文件检查规则
│   │   │   ├── structure.go         # 项目结构检查规则
│   │   │   ├── docs.go              # 文档完整度检查规则
│   │   │   └── testing.go           # 测试覆盖检查规则
│   │   └── llm/
│   │       └── analyzer.go          # LLM 辅助分析（via sub2api）（⚠️ 预算控制未实现）
│   ├── efficiency/
│   │   ├── labeler.go               # PR 自动标签逻辑（⚠️ 不调用 SCM AddLabels）
│   │   └── aggregator.go            # 效能指标聚合计算
│   ├── handler/
│   │   ├── pr.go                    # PR 记录 API handlers
│   │   ├── settings.go              # LLM 配置管理 handlers
│   │   ├── efficiency.go            # ⚠️ 额外：效能 handler 独立文件
│   │   └── chat.go                  # Repo 上下文 LLM 对话 handler（Step 10）
│   └── scm/bitbucket/
│       └── bitbucket.go             # Bitbucket Server SCM 插件
```

### Frontend (新增)

```
frontend/src/
├── api/
│   ├── analysis.ts                  # 扫描相关 API
│   ├── pr.ts                        # PR 记录 API
│   ├── efficiency.ts                # 效能指标 API
│   ├── settings.ts                  # LLM 配置 API
│   ├── chat.ts                      # ⚠️ 额外：LLM 对话 API
│   └── session.ts                   # ⚠️ 额外：Session API
└── views/
    └── analysis/
        └── ScanResultView.vue       # 扫描结果详情页
```

---

## Implementation Steps

### Step 1: 新增 Ent Schema

新增 3 个数据模型支撑 Phase 2 功能。

- [x] 1.1 Create `backend/ent/schema/aiscanresult.go` — Fields: `repo_config_id` (FK), `score` (int, 0-100), `dimensions` (JSON — 各维度评分明细), `suggestions` (JSON — 改进建议列表), `scan_type` (enum: static/llm/full), `commit_sha` (string), `created_at`. Edge: belongs to `repo_config`.
- [x] 1.2 Create `backend/ent/schema/prrecord.go` — Fields: `repo_config_id` (FK), `scm_pr_id` (int), `scm_pr_url` (string), `author`, `title`, `source_branch`, `target_branch`, `status` (enum: open/merged/closed), `labels` (JSON array), `lines_added`, `lines_deleted` (int), `changed_files` (JSON), `session_ids` (JSON array), `token_cost` (float), `ai_ratio` (float), `ai_label` (enum: ai_via_sub2api/no_ai_detected), `merged_at`, `cycle_time_hours`, `created_at`, `updated_at`. Edge: belongs to `repo_config`.
- [x] 1.3 Create `backend/ent/schema/efficiencymetric.go` — Fields: `repo_config_id` (FK), `user_id` (FK, optional), `period_type` (enum: daily/weekly/monthly), `period_start` (time), `total_prs`, `ai_prs`, `human_prs` (int), `avg_cycle_time_hours` (float), `total_tokens` (int), `total_token_cost` (float), `ai_vs_human_ratio` (float), `created_at`. Edges: belongs to `repo_config`, belongs to `user` (optional).
- [x] 1.4 Run `cd backend && go generate ./ent` to generate Ent code. Fix any schema issues.
- [x] 1.5 Verify: `go generate ./ent && go build ./...` compiles with new schemas.

### Step 2: AI 友好度静态规则引擎

实现 `backend/internal/analysis/` 模块，静态规则权重 60%，满分 60 分。

| 维度 | 满分 | 检查项 |
|------|------|--------|
| AI 辅助文件 | 20 | AGENTS.md, CLAUDE.md, .cursorrules, .editorconfig, .prettierrc |
| 项目结构 | 15 | 目录深度、单文件过大、命名一致性 |
| 文档完整度 | 15 | README.md, API 文档, 注释密度 |
| 测试覆盖 | 10 | 测试文件存在性, CI 配置, 测试框架 |

- [x] 2.1 Create `backend/internal/analysis/scanner.go` — `Scanner` interface: `Scan(ctx, repoPath) (*ScanResult, error)`. `StaticScanner` struct implementing the interface. `ScanResult` struct with `Score`, `Dimensions`, `Suggestions`.
- [x] 2.2 Create `backend/internal/analysis/rules/ai_files.go` — AI 辅助文件检查规则（满分 20）。检查 AGENTS.md, CLAUDE.md, .cursorrules, .editorconfig, .prettierrc 是否存在。
- [x] 2.3 Create `backend/internal/analysis/rules/structure.go` — 项目结构检查规则（满分 15）。检查目录深度、单文件过大、命名一致性。
- [x] 2.4 Create `backend/internal/analysis/rules/docs.go` — 文档完整度检查规则（满分 15）。检查 README.md, API 文档, 注释密度。
- [x] 2.5 Create `backend/internal/analysis/rules/testing.go` — 测试覆盖检查规则（满分 10）。检查测试文件存在性, CI 配置, 测试框架。
- [x] 2.6 Create `backend/internal/analysis/cloner.go` — Repo shallow clone + 缓存管理。克隆到 `{DATA_DIR}/repos/{repo_config_id}/`，首次 `--depth 1`，后续 `fetch + reset`。
- [x] 2.7 Create `backend/internal/analysis/service.go` — `AnalysisService` struct。Methods: `RunScan(ctx, repoConfigID)` 协调 clone + static scan + LLM scan + 存储结果, `GetLatestScan(ctx, repoConfigID)`, `ListScans(ctx, repoConfigID, limit)`.
- [x] 2.8 Update `backend/internal/handler/repo.go` — 改造 `POST /api/v1/repos/:id/scan` 从占位实现为调用 AnalysisService.RunScan。新增 `GET /api/v1/repos/:id/scans` 和 `GET /api/v1/repos/:id/scans/latest`。
- [x] 2.9 Verify: 对示例 repo 目录执行静态扫描，验证评分逻辑。

### Step 3: LLM 辅助分析

通过 sub2api API 调用 LLM，权重 40%，满分 40 分。

- [x] 3.1 Create `backend/internal/analysis/llm/analyzer.go` — `Analyzer` struct。通过 sub2api API 调用 LLM。分析维度：代码可读性、模块耦合度、依赖健康度、AI 协作建议。预算控制：单次扫描 token 上限（默认 100K），单 repo 每日扫描上限（默认 3 次），超出预算跳过 LLM 仅返回静态评分。`sync.RWMutex` 保护 cfg 实现线程安全热更新。
- [x] 3.2 Create `backend/internal/handler/settings.go` — `SettingsHandler` struct。`GET /api/v1/settings/llm` 返回当前配置（API key 脱敏），`PUT /api/v1/settings/llm` 接收配置写入 config.yaml 并热更新 analyzer，`POST /api/v1/settings/llm/test` 验证连通性。
- [x] 3.3 Update `backend/internal/handler/router.go` — 添加 settings 路由组（admin only）。
- [x] 3.4 Update `backend/cmd/server/main.go` — 创建 `SettingsHandler`，传入 configPath + llmAnalyzer。
- [x] 3.5 Create `frontend/src/api/settings.ts` — `getLLMConfig()`, `updateLLMConfig(data)`, `testLLMConnection()`。
- [x] 3.6 Update `frontend/src/views/SettingsView.vue` — 添加 "LLM Configuration" 卡片：Sub2api URL, API Key, Model 字段，Status badge，Test Connection + Save 按钮。
- [x] 3.7 Verify: mock sub2api 响应，验证 LLM 分析流程和预算控制。

### Step 4: 自动优化 PR

根据扫描结果自动创建优化 PR。

- [x] 4.1 Create `backend/internal/analysis/optimizer.go` — `Optimizer` struct。`PreviewOptimization(ctx, repoConfigID)` 生成文件预览（old/new content）。`ConfirmOptimization(ctx, repoConfigID, files)` 创建分支 `ai-efficiency/optimize-{timestamp}`，提交修复文件，创建 PR。支持自动修复：创建 AGENTS.md（基于 LLM 分析结果）、补充 .editorconfig。
- [x] 4.2 Update `backend/internal/handler/repo.go` — 新增 `POST /api/v1/repos/:id/optimize/preview` 和 `POST /api/v1/repos/:id/optimize/confirm`。
- [x] 4.3 Verify: mock SCM provider，验证分支创建、文件提交、PR 创建流程。

### Step 5: PR 记录 + Webhook 事件处理

改造 webhook dispatch 为实际业务处理，新增 PR 记录 API。

- [x] 5.1 Update `backend/internal/webhook/handler.go` — 从纯日志改为实际业务处理。PR opened/updated → 创建/更新 `pr_records`。PR merged → 更新状态 + 触发效能计算。Push to default branch → 触发 AI 友好度重新扫描。
- [x] 5.2 Create `backend/internal/handler/pr.go` — `PRHandler` struct。`GET /api/v1/repos/:id/prs` 列出 PR 记录（支持分页、时间范围过滤），`GET /api/v1/prs/:id` PR 详情，`POST /api/v1/repos/:id/sync-prs` 主动同步 PR。⚠️ 路由偏差：计划为 `/prs/sync`，实际为 `/sync-prs`，前后端已统一。
- [x] 5.3 Update `backend/internal/handler/router.go` — 添加 PR 路由。
- [x] 5.4 Verify: 模拟 webhook payload，验证 PR 记录创建和状态更新。

### Step 6: PR 自动标签（Session-Token 关联）

当 PR webhook 到达时，通过 session 关联查询 token 消耗并打标签。

- [x] 6.1 Create `backend/internal/efficiency/labeler.go` — `Labeler` struct。`LabelPR(ctx, prRecord)`: 查找匹配的 session（repo + branch + 时间窗口），用 session 的 sub2api_api_key_id 查询 sub2api usage_logs（via sub2apidb client），计算 token_cost 和 ai_ratio，打标签 `ai_via_sub2api` 或 `no_ai_detected`（via SCM provider AddLabels）。
- [x] 6.2 Update `backend/internal/webhook/handler.go` — PR merged 事件触发 labeler.LabelPR。
- [x] 6.3 Verify: 创建 session + 模拟 PR webhook，验证标签逻辑。

### Step 7: 效能指标计算

PR 级别实时指标 + 聚合定时任务。

- [x] 7.1 Create `backend/internal/efficiency/aggregator.go` — `Aggregator` struct。`AggregateForRepo(ctx, repoConfigID, periodType, periodStart)` 按 daily/weekly/monthly 聚合：total_prs, ai_prs, human_prs, avg_cycle_time, total_tokens, total_token_cost, ai_vs_human_ratio。Upsert 写入 `efficiency_metrics` 表。`AggregateAll(ctx)` 遍历所有 repo 执行聚合。
- [x] 7.2 Create efficiency API handlers — `GET /api/v1/efficiency/dashboard` 仪表盘概览，`GET /api/v1/efficiency/repos/:id/metrics` repo 级别指标，`GET /api/v1/efficiency/repos/:id/trend` 趋势数据。
- [x] 7.3 Update `backend/internal/handler/router.go` — 添加 efficiency 路由组。
- [x] 7.4 Verify: 插入测试 PR 数据，验证聚合计算和 API 返回。

### Step 8: 效能仪表盘前端

替换占位数据为真实 API，新增扫描结果详情页。

- [x] 8.1 Update `frontend/src/views/DashboardView.vue` — 替换占位数据为真实 API 数据。统计卡片：Total Repos, Active Sessions, Avg AI Score, AI PRs。
- [x] 8.2 Create `frontend/src/api/analysis.ts` — `triggerScan()`, `listScans()`, `getLatestScan()`, `optimizePreview()`, `optimizeConfirm()`。
- [x] 8.3 Create `frontend/src/api/pr.ts` — `listPRs()`, `getPR()`, `syncPRs()`。
- [x] 8.4 Create `frontend/src/api/efficiency.ts` — `getDashboard()`, `getRepoMetrics()`, `getRepoTrend()`。
- [x] 8.5 Update `frontend/src/views/repos/RepoDetailView.vue` — 增强：扫描历史（可点击跳转详情）、PR 列表（分页 + 时间过滤）、效能趋势图。
- [x] 8.6 Create `frontend/src/views/analysis/ScanResultView.vue` — 扫描结果详情：评分总览、各维度评分条形图、建议列表（区分 auto-fixable / manual）、历史扫描切换。
- [x] 8.7 Update `frontend/src/router/index.ts` — 添加 `/repos/:repoId/scans` 和 `/repos/:repoId/scans/:scanId` 路由。
- [x] 8.8 Verify: `cd frontend && npm run build` succeeds. 页面渲染正常。

### Step 9: Bitbucket Server SCM 插件

实现 SCMProvider 接口的 Bitbucket Server 版本。

- [x] 9.1 Create `backend/internal/scm/bitbucket/bitbucket.go` — `Provider` struct implementing `scm.SCMProvider`。Bitbucket Server REST API v1.0。Bearer token 认证。Full name 格式 `project/repo`。
- [x] 9.2 Update `backend/internal/webhook/handler.go` — 添加 `POST /api/v1/webhooks/bitbucket` 入口，Bitbucket webhook 格式适配。
- [x] 9.3 Update SCM provider factory — 添加 bitbucket_server provider 创建。
- [x] 9.4 Verify: 接口合规测试 + mock API 测试。

### Step 10: Repo 上下文 LLM 对话

> **来源：** `docs/superpowers/specs/2026-03-18-repo-chat-and-scan-prompts-design.md` Feature 1

实现 Repo 页面侧边抽屉式 LLM 对话，用户可基于 repo 扫描结果和代码与 LLM 交互。

- [x] 10.1 Create `backend/internal/handler/chat.go` — `ChatHandler` struct。`POST /api/v1/repos/:id/chat` 接收 `message` + `history`，加载 repo info + 最新 scan result + 文件树，组装 system prompt（含 repo context），调用 sub2api LLM，返回响应。含 tool calling 支持。
- [x] 10.2 Update `backend/internal/analysis/llm/analyzer.go` — 新增 `Chat()` 方法，复用 LLM 调用逻辑。
- [x] 10.3 Update `backend/internal/handler/router.go` — 注册 `POST /repos/:id/chat` 路由（RequireAuth）。
- [x] 10.4 Update `backend/cmd/server/main.go` — 创建 ChatHandler，传入 SetupRouter。
- [x] 10.5 Create `frontend/src/api/chat.ts` — `sendChatMessage(repoId, message, history)`。
- [x] 10.6 Create `frontend/src/components/RepoChat.vue` — 侧边抽屉 Chat 面板组件：消息列表 + 输入框 + 发送按钮，loading/error/空状态。
- [x] 10.7 Update `frontend/src/views/repos/RepoDetailView.vue` — 引入 RepoChat，添加浮动按钮。
- [x] 10.8 Verify: 对已扫描的 repo 发送 chat 请求，验证 LLM 响应。

### Step 11: 可配置 Scan Prompt

> **来源：** `docs/superpowers/specs/2026-03-18-repo-chat-and-scan-prompts-design.md` Feature 2

实现全局 + repo 级别 override 的 system prompt 和 user prompt template。

- [x] 11.1 Update `backend/internal/config/config.go` — `LLMConfig` 增加 `SystemPrompt`, `UserPromptTemplate` 字段。
- [x] 11.2 Update `backend/internal/analysis/llm/analyzer.go` — `Analyze()` 从 config 读 prompt，新增 `resolvePrompts()` 方法实现三层 fallback（硬编码默认值 → 全局配置 → repo override）。模板使用 `strings.ReplaceAll` 替换 `{repo_context}` 占位符。
- [x] 11.3 Update `backend/internal/handler/settings.go` — `GET/PUT /api/v1/settings/llm` 增加 `system_prompt`, `user_prompt_template` 字段。
- [x] 11.4 Update `frontend/src/views/SettingsView.vue` — LLM 配置区域增加 System Prompt / User Prompt Template textarea。
- [x] 11.5 Verify: 修改全局 prompt 后触发扫描，验证 LLM 使用新 prompt。

---

## Dependencies Between Steps

```
Step 1 (Ent Schema) ──→ Step 2 (静态规则引擎) ──→ Step 3 (LLM 分析) ──→ Step 4 (自动优化 PR)
Step 1 ──→ Step 5 (PR 记录 + Webhook) ──→ Step 6 (PR 自动标签) ──→ Step 7 (效能指标)
Step 7 ──→ Step 8 (效能仪表盘前端)
Step 9 (Bitbucket 插件) — 可并行，无依赖
```

---

## Conventions

- 所有新代码遵循 Phase 1 的代码风格和模式
- 每个 Step 完成后跑全量测试确认无回归
- Go proxy: `GOPROXY=https://goproxy.cn,direct`
- 前端测试: `vitest --run`
- 后端测试: `go test ./... -count=1`
- API responses: use `pkg.Success()` / `pkg.Error()` helpers
- sub2api queries: always raw SQL via squirrel, never Ent
- Frontend: `<script setup lang="ts">`, Composition API, TailwindCSS
- Commits: Conventional Commits — `feat(backend): add analysis engine`

---

## 审查偏差总结（2026-03-24）

### ❌ 功能性缺失（需后续补齐）

| # | 位置 | 计划要求 | 实际实现 | 严重程度 |
|---|------|----------|----------|----------|
| 1 | `llm/analyzer.go` | 预算控制：单次 token 上限、每日扫描上限、超出跳过 LLM | **完全未实现**，`Analyze()` 无任何预算检查 | 高 |
| 2 | `webhook/handler.go` | Push to default branch 触发 AI 友好度重新扫描 | 只记日志，不触发扫描 | 中 |
| 3 | `efficiency/labeler.go` | 通过 SCM provider `AddLabels()` 在 PR 上打标签 | 只更新数据库 `ai_label` 字段，不调用 SCM API | 中 |
| 4 | `ent/schema/efficiencymetric.go` | `user_id` 通过 Edge 关联 User entity | 只是普通 Int 字段，无 Edge 关联 | 低 |

### ❌ 结构性偏差

| # | 位置 | 计划要求 | 实际实现 |
|---|------|----------|----------|
| 1 | `prrecord.go` | `ai_label` enum 只有 2 个值 | 多了 `"pending"` 默认值（合理增强） |
| 2 | 路由 | `POST /repos/:id/prs/sync` | 实际为 `POST /repos/:id/sync-prs`，前后端已统一 |
| 3 | `scanner.go` | 包含 Scanner 接口 + StaticScanner | StaticScanner 拆分到 `static.go` |

### ⚠️ 额外实现（计划外）

| # | 内容 | 说明 |
|---|------|------|
| 1 | `POST /repos/:id/optimize` | 一步式优化 legacy flow（计划只有 preview + confirm） |
| 2 | `POST /efficiency/aggregate` | 手动触发聚合 API（admin only） |
| 3 | `analysis/static.go` | StaticScanner 独立文件 |
| 4 | `analysis/rules/types.go` | 类型定义独立文件 |
| 5 | `handler/efficiency.go` | 效能 handler 独立文件 |
| 6 | PR opened 时也触发 labeler | 计划只要求 merged 时触发 |

### 待修复优先级

1. **高**：实现 LLM 预算控制 — 无预算限制可能导致 token 成本失控
2. **中**：Push 事件触发重新扫描 — 代码变更后应自动更新 AI 友好度评分
3. **中**：Labeler 调用 SCM AddLabels — PR 上应显示实际标签，而非仅数据库记录
4. **低**：efficiencymetric user_id Edge 关联 — 影响 Ent eager loading，可手动 join 绕过
