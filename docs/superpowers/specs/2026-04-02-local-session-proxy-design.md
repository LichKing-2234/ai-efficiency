# Local Session Proxy 设计文档

**Date:** 2026-04-02  
**Status:** Draft for Review
**Scope:** `ae-cli/`, `backend/`, `docs/`  
**Related:**  
- [2026-03-24-oauth-cli-login-design.md](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md)  
- [2026-03-26-session-pr-attribution-design.md](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-26-session-pr-attribution-design.md)

项目级架构总览见 [`docs/architecture.md`](../../architecture.md)。

**Spec Relationship:**
- 本文在 session runtime 方向上有意偏离 [`2026-03-24-oauth-cli-login-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md) 与 [`2026-03-26-session-pr-attribution-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-26-session-pr-attribution-design.md) 中以 backend bootstrap + session-scoped relay key 为中心的主路径。
- 本文把本地 local proxy 观察到的 request usage 提升为核心事实源，并把 `sub2api` 从 session 归因主账调整为上游网关与可选对账来源。
- 前述历史 spec 保留其各自时间点的设计背景与取舍，不在其正文中回写本文的演进方向。当前实现状态请以 [`docs/architecture.md`](../../architecture.md) 为准。

## 概述

本设计定义一个**基于 superpowers session runtime 的本地 session-aware proxy/daemon**。

它的目标不是替代 `ae-cli start`，而是作为 `ae-cli start` 自动拉起的内部组件，承担两类职责：

1. **数据面代理**
   - 暴露 `OpenAI-compatible` 本地接口给 `Codex`
   - 暴露 `Anthropic-compatible` 本地接口给 `Claude`
   - 将请求转发到 `sub2api`
   - 在本地精确记录每个 session/request 的 usage

2. **控制面汇聚**
   - 接收工具 hooks / session events / git checkpoint 事件
   - 将事件归一化后上报给 backend
   - 把 `session -> repo/workspace -> commit -> token usage` 串成同一条链路

本设计的核心转向是：

- **主模型从 `session key / relay api key` 转为 `agent session`**
- `hook` 用于记录 repo / workspace / commit 锚点
- `local proxy` 用于记录请求级 usage
- `backend` 用于持久化、查询和 PR 结算
- `sub2api` 从“session 归因主账”降级为“上游模型网关 + 可选对账来源”

## 背景问题

当前实现里，`ae-cli start` 通过 backend bootstrap 为 session 创建一把专用 relay API key，再用这把 key 去隔离 `sub2api` 主账。

这个方案有几个问题：

1. `sub2api` 的 key 创建能力不完全匹配当前实际 API 契约，导致 bootstrap 路径脆弱
2. 归因模型围绕“session key”展开，但用户真正关心的是：
   - 哪个 agent session 对应哪个 repo/workspace
   - 哪些 commit / PR 来自哪个 session
   - 这个 session 消耗了多少 token
3. 各工具 hooks 文档里主要暴露的是**行为事件**，不是统一的 token/cost 事件
4. 如果坚持“一次 start 一把 key”，会引入高 key churn、清理复杂度和 start 失败面

因此本设计换一个更贴近目标的思路：

- 把“session”作为主实体
- 把“本地代理观察到的请求 usage”作为 token 事实源
- 把“hook + git checkpoint”作为代码锚点

## 目标

1. `ae-cli start` 自动拉起本地 session-bound proxy
2. `Codex` 和 `Claude` 不再直接访问 `sub2api`
3. 工具侧不需要直接配置真实 upstream API key
4. 每个 agent request 都能稳定关联到：
   - `session_id`
   - `workspace_id`
   - `provider_name`
   - `model`
   - `request timestamp`
5. hooks / events 能稳定关联到：
   - `session_id`
   - `repo/workspace`
   - `commit checkpoint`
6. backend 能基于本地 usage + checkpoint 直接做：
   - session usage 汇总
   - commit interval usage 汇总
   - PR attribution

## 非目标

1. 第一版不兼容所有 AI 工具，只覆盖 `Codex` 与 `Claude`
2. 第一版不实现远端流量统一代理，proxy 仅在本机存在
3. 第一版不替代现有 `relay.Provider` 与 backend 里的主分析能力
4. 第一版不删除现有 git checkpoint / attribution 表结构，只是在其上新增 usage 事件来源
5. 第一版不要求 Kiro 同时接入

## 用户体验

### 启动

用户执行：

```bash
ae-cli start
```

行为：

1. backend 创建或恢复 session
2. `ae-cli` 获取当前用户可用的 provider 信息
3. `ae-cli` 在本机启动一个 session-bound proxy
4. `ae-cli` 写入 runtime bundle / workspace marker
5. `ae-cli` 自动为 `Codex` / `Claude` 注入本地 proxy 配置

用户不需要额外执行：

```bash
ae-cli proxy start
```

`proxy start` 只作为内部概念存在，不作为主用户入口。

### 停止

用户执行：

```bash
ae-cli stop
```

行为：

1. 停止 heartbeat
2. flush 本地事件队列
3. 停掉本地 proxy
4. 清理 runtime bundle / marker

proxy 生命周期与 session 绑定，不做用户级常驻服务。

## 术语

### Session Runtime

由 `ae-cli start` 拉起的一组本地运行态：

- workspace marker
- runtime bundle
- shared git hooks
- local proxy
- local event spool

### Local Proxy

本地 HTTP 服务，仅监听 `127.0.0.1`，对外暴露三类接口：

1. OpenAI-compatible inference endpoints
2. Anthropic-compatible inference endpoints
3. Local event ingress endpoints

### Usage Event

由 local proxy 直接观察到的模型请求事件，至少包含：

- `session_id`
- `request_id`
- `provider_name`
- `model`
- `started_at`
- `finished_at`
- `input_tokens`
- `output_tokens`
- `total_tokens`
- `status`

当前实现补充说明（2026-04-16）：

- 已验证 relay 的 OpenAI-compatible `/responses` 原始响应可以返回嵌套 usage detail，例如：
  - `usage.input_tokens_details.cached_tokens`
  - `usage.output_tokens_details.reasoning_tokens`
- 已验证 Anthropic-compatible `/v1/messages` 原始响应会使用不同字段表达 cache 明细，例如：
  - `usage.cache_creation_input_tokens`
  - `usage.cache_read_input_tokens`
  - `usage.cached_tokens`
  - `usage.cache_creation.ephemeral_5m_input_tokens`
  - `usage.cache_creation.ephemeral_1h_input_tokens`
- 当前 local proxy 会把 request-level 基本字段持久化到 `session_usage_events`，并把解析到的 cache / reasoning detail 写入：
  - `raw_metadata.cached_input_tokens`
  - `raw_metadata.reasoning_output_tokens`
- 对 Anthropic-compatible 请求，`raw_metadata.cached_input_tokens` 表示 `cache_creation_input_tokens + cache_read_input_tokens` 的聚合值，并额外保留：
  - `raw_metadata.cache_creation_input_tokens`
  - `raw_metadata.cache_read_input_tokens`
- 对新的 non-stream request usage rows，`session_usage_events.raw_response` 会保存原始 upstream response body
- 经 2026-04-16 的真实 Codex e2e 复测，请求级 `session_usage_events.raw_metadata` 已可与 transcript-side `token_count` 中的 cache / reasoning token 明细对齐
- `agent_metadata_events` 仍不会由 request usage ingest 自动生成；它们依赖 `post_commit` 时附带的 collector snapshot
- 当前 collector 已优先读取 workspace session-local Codex transcript（`<workspace>/.ae/codex-home/`），因此真实 commit 之后可以生成包含 `cached_input_tokens` / `reasoning_tokens` 的 `agent_metadata_events`

### Event Ingress

工具 hooks 与 git hooks 向 local proxy 报送的事件入口。事件种类包括：

- `session_start`
- `user_prompt_submit`
- `pre_tool_use`
- `post_tool_use`
- `stop`
- `post_commit`
- `post_rewrite`

## 总体架构

```text
Codex  ----\
            \
             >  Local Session Proxy  ----> sub2api
            /
Claude ----/

Codex hooks ------\
Claude hooks ------> Local Event Ingress ----> ae-cli local queue ----> backend
Git hooks --------/

Local Proxy request usage --------------------> ae-cli local queue ----> backend
```

关键点：

1. 工具请求先到 local proxy
2. local proxy 在**转发前**就知道当前 session
3. usage 由 local proxy 直接记录
4. hooks 不需要自己做复杂 HTTP 转发逻辑，只要把事件交给 local proxy
5. backend 只接收已经归一化过的 session/request/checkpoint 数据

## Proxy 形态

### 进程模型

第一版采用 **`ae-cli` 内置子进程**，但对用户透明。

- 用户入口：`ae-cli start`
- 内部实现：`ae-cli` 拉起本地 proxy 子进程，记录 pid / port / auth token 到 runtime bundle

不单独引入新的安装物或独立常驻 daemon。

### 监听地址

第一版仅监听：

- `127.0.0.1:<dynamic-port>`

不监听：

- `0.0.0.0`
- Unix socket

原因：

- 兼容 `Codex` / `Claude` 现有 HTTP endpoint 配置方式
- 减少额外平台差异

### 鉴权

local proxy 必须要求一个 **session-scoped local auth token**。

工具配置里使用的是：

- local proxy token

而不是：

- 真实 `sub2api` upstream key

这样可以做到：

1. 用户端不直接拿 upstream provider key
2. proxy 可以确认请求一定属于当前 session

## 对外接口

### 1. OpenAI-compatible 路径

用于 `Codex`。

第一版至少支持：

- `POST /openai/v1/responses`
- `POST /openai/v1/chat/completions`
- `GET /openai/v1/models`（可选，若 Codex 启动需要）

说明：

- `Codex` 统一通过 local proxy 的 OpenAI-compatible endpoint 访问
- 不再依赖 ChatGPT 订阅登录模式作为主路径
- 走 provider/API 模式

### 2. Anthropic-compatible 路径

用于 `Claude`。

第一版至少支持：

- `POST /anthropic/v1/messages`

如果 Claude Code 还会访问其他探测接口，则按实际需要补齐。

### 3. Local event ingress

用于 hooks / events。

第一版建议：

- `POST /events`

请求体：

```json
{
  "event_type": "post_commit",
  "session_id": "uuid",
  "workspace_id": "uuid",
  "source": "git_hook",
  "payload": {}
}
```

也允许本地 `ae-cli hook` 子命令继续存在，但它不再直接上报 backend，而是优先打本地 proxy。

## 工具接入方式

### Codex

`ae-cli start` 自动生成 session-local Codex 配置，使其指向 local proxy。

第一版约束：

- `model_provider` 指向 local proxy
- `base_url = http://127.0.0.1:<port>/openai/v1`
- `env_key = AE_LOCAL_PROXY_TOKEN`
- `wire_api = responses`

`ae-cli shell` / `run` / tmux panes 自动注入：

- `AE_LOCAL_PROXY_TOKEN`
- `CODEX_HOME` 或等价 config path（若需要 session-local config）

### Claude

`ae-cli start` 自动注入：

- `ANTHROPIC_BASE_URL=http://127.0.0.1:<port>/anthropic`
- `ANTHROPIC_AUTH_TOKEN=<session-local-proxy-token>`

如有必要，也可通过 session-local `settings.local.json` 或 `apiKeyHelper` 提供同样效果，但第一版优先环境变量。

## Upstream 认证与转发

### upstream key 模型

第一版不为每个 session 新建 `sub2api` key。

采用：

- 每用户一个稳定 upstream key（或 provider key）

这个 upstream key 只保存在：

- local proxy 内存
- 或 runtime bundle 的受限文件

不会直接下发给 `Codex` / `Claude`。

### 转发行为

local proxy 收到请求后：

1. 校验 local proxy token
2. 补齐 `session_id`
3. 生成本地 `request_id`
4. 记录请求开始事件
5. 使用用户 upstream key 转发到 `sub2api`
6. 记录响应 usage
7. 将 usage event 写入本地 queue，并异步上报 backend

## Hook / Event 模型

### 为什么仍然需要 hooks

hooks 的作用不是记 token，而是记：

- repo/workspace 归属
- prompt/tool/subagent 行为
- commit 锚点

没有 hooks，就很难稳定回答：

- 一个 session 最终影响了哪些 commit
- 某次 commit 属于哪个 agent session

### 事件优先级

第一版对归因的作用排序：

1. `git post-commit / post-rewrite`
   - 强锚点
2. `UserPromptSubmit`
   - 弱任务边界
3. `PreToolUse / PostToolUse`
   - 行为链辅助
4. `Stop`
   - 会话尾部边界

### 工具支持差异

- `Claude`：hooks 最强，事件类型丰富
- `Codex`：有 hooks，但当前工具级覆盖较弱，不能假设所有 tool 都有事件

因此第一版要求：

- hooks 只作为行为与代码锚点
- token 主来源仍然是 local proxy request usage

## Backend 合同

第一版新增两类后端入口：

### 1. Usage ingest

`POST /api/v1/session-usage-events`

用于接收 local proxy 记录的 usage 事件。

字段至少包括：

- `event_id`
- `session_id`
- `workspace_id`
- `provider_name`
- `model`
- `request_id`
- `started_at`
- `finished_at`
- `input_tokens`
- `output_tokens`
- `total_tokens`
- `status`
- `raw_metadata`

### 2. Session event ingest

`POST /api/v1/session-events`

用于接收：

- prompt submit
- tool events
- stop
- other agent lifecycle events

现有 `/checkpoints/*` 保留，git commit 继续走 checkpoint 专用接口。

## 归因模型

### 主模型

主模型改为：

- `session`
  - 有 usage events
  - 有 session events
  - 有 commit checkpoints

### token -> session

由 local proxy 直接记录，无需通过 `api_key_id` 反推。

### session -> commit

由 git checkpoint 负责。

### commit interval -> PR

沿用现有 PR attribution 思路，但 interval usage 来源从：

- `sub2api usage logs by api_key_id`

改为优先使用：

- `session usage events in [prev_checkpoint, current_checkpoint]`

`sub2api` 日志只做可选 reconciliation。

## Failure Model

### local proxy 崩溃

若 proxy 崩溃：

- 当前工具请求失败
- `ae-cli` 需在 heartbeat 或 shell wrapper 中检测并给出明确错误

不做 silent fail-open。

原因：

- inference 主路径已经依赖 local proxy
- 继续 silent fallback 到直连上游会破坏归因一致性

### backend 不可达

若 backend 不可达：

- local proxy 继续记录 usage events 到本地 queue
- git hooks 继续写本地 queue
- `ae-cli flush` 后续补传

### sub2api 不可达

若 upstream 不可达：

- 请求失败直接暴露给工具
- local proxy 仍记录失败请求事件

## 安全边界

1. local proxy 仅监听 `127.0.0.1`
2. upstream key 不直接暴露给工具配置
3. local proxy token 必须是 session-scoped 且高熵
4. runtime bundle 目录权限保持 `0700`
5. session-local secret 文件权限保持 `0600`
6. hooks 只调用 local proxy / `ae-cli`，不在 shell hook 中直接保存明文 key

## Phase 1 最小可交付

第一版只做：

1. `ae-cli start` 自动拉起 local proxy
2. `Codex` 走 OpenAI-compatible local proxy
3. `Claude` 走 Anthropic-compatible local proxy
4. local proxy 记录 request usage 并上报 backend
5. 现有 git hooks 继续生成 checkpoint
6. backend 能按 `session + checkpoint` 汇总 usage

第一版不做：

- Kiro 接入
- 复杂 task graph
- remote daemon
- 完整 reconciliation dashboard

## 迁移策略

1. 保留现有 session / checkpoint / attribution 表与接口
2. 新增 local proxy usage ingest，不立刻删除 relay-log 结算逻辑
3. 在 PR settlement 中优先读取 local usage，缺失时再 fallback 到旧主账逻辑
4. 当 local proxy usage 稳定后，再评估是否彻底移除“session key per start”设计

## 验收标准

1. `ae-cli start` 能自动启动 local proxy
2. `Codex` 可通过 local proxy 正常请求模型
3. `Claude` 可通过 local proxy 正常请求模型
4. 工具配置中不再暴露真实 upstream provider key
5. 每次模型请求都能稳定落到唯一 `session_id`
6. commit 后仍能生成 checkpoint
7. 能对任一 session 直接汇总 token usage
8. 能将 session usage 映射到 commit interval
9. 现有 backend / frontend attribution 查询链路不被破坏
