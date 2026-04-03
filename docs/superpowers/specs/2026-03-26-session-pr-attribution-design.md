# Session / PR Attribution 设计文档

**Date:** 2026-03-26
**Status:** Review Requested
**Scope:** `ae-cli/`, `backend/`, `relay provider`, `docs/`
**Related:** [2026-03-17-ai-efficiency-platform-design.md](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-17-ai-efficiency-platform-design.md)

项目级架构总览与运行时关系图见 [`docs/architecture.md`](../../architecture.md)。

**Spec Relationship:**
- 本文相对 [`2026-03-17-ai-efficiency-platform-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-17-ai-efficiency-platform-design.md) 将 session / PR attribution 从平台级基线中收敛为独立合同。
- 本文相对 [`2026-03-24-oauth-cli-login-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md) 进一步细化并部分改写了用户身份主键、session bootstrap、session-scoped relay API key 生命周期、以及 PR attribution 主链路。
- 因此，若本文与更早 spec 在上述主题上存在冲突，应以本文为准；更早 spec 保留其各自时间点的设计背景与取舍。

## 概述

本设计定义一条可验证的端到端链路，用于把 `ae-cli` 启动的开发会话、`sub2api` 主账用量、`Codex / Claude / Kiro` 本地会话元数据，以及最终的 PR 结果连接起来。

第一版目标不是“自动化一切”，而是先建立一条可靠、可审计、可手动结算的主链路：

1. `ae-cli login` 建立用户身份
2. `ae-cli start` 创建 session，并为 session 生成一把专用 relay API key
3. 本地 collector 采集 `Codex / Claude / Kiro` 会话元数据
4. Git hooks 在 commit 时写入 checkpoint
5. 用户或管理员手动触发 PR 结算
6. 后端汇总主账与副账，得到 PR 的整体 token 消耗与验证结果

本设计明确区分：

- **主账**：`sub2api` usage logs，作为最终 PR token/cost 的认定依据
- **副账**：`Codex / Claude / Kiro` 本地会话元数据，用于交叉验证，不直接决定 PR 最终成本
- **commit interval cost**：两个 commit checkpoint 之间的区间成本
- **PR total cost**：PR 当前 commit 集合对应的总体可归因成本

本设计**不承诺**“严格意义上的每个 commit 因果 token 成本”。第一版对外口径应为：

- `session cost`：可精确
- `usage log cost`：可精确
- `commit interval cost`：可归因，但不是严格 commit 因果成本
- `PR total cost`：可作为最终主指标

## 术语

### AI Efficiency User

`ai-efficiency` 本地用户，由 SSO 或 LDAP 登录产生。

### Relay Identity

用户在 relay/sub2api 侧的稳定身份。**本文明确使用 `sub2api user` 作为稳定身份实体**，而不是 `sub2api upstream account`。

稳定身份主键使用 `username`，而不是邮箱。邮箱仅作为展示字段和辅助信息。

### Upstream Account

`sub2api` 的管理员侧上游账号资源，即 `admin/accounts`。它属于 provider routing/pool 层，不是用户主身份。请求运行时最终命中的 `account_id` 由 group/pool 路由决定，并在 `usage_logs` 中记录。

### Session

由 `ae-cli start` 显式创建的开发会话，是第一版唯一的用量隔离边界。

第一版约束：

- `sub2api user : API key = 1 : n`
- `session : primary API key = 1 : 1`

### Workspace

本地某一个工作目录，既兼容普通仓库，也兼容 linked worktree 和多个独立 clone。workspace 是执行上下文，不是第一版的主隔离边界。

### Workspace ID

`workspace_id` 必须是**可重复推导**的稳定值，不能由不同组件各自随机生成。

第一版统一使用：

- `UUIDv5("ae-workspace", canonical_repo_root + "\x1f" + canonical_workspace_root + "\x1f" + canonical_git_dir + "\x1f" + canonical_git_common_dir)`

其中 `canonical_*` 表示：

- 绝对路径
- 路径标准化
- 完成符号链接解析

要求：

- `ae-cli`
- hooks
- backend

都使用完全一致的 derivation 规则。

### Workspace Marker

工作目录根下的 `/.ae/session.json`。只保存**非敏感运行态**，例如：

- `session_id`
- `workspace_id`
- `runtime_ref`
- `provider_name`
- `relay_api_key_id`

不直接保存 API key secret。

### Runtime Bundle

位于 `~/.ae-cli/runtime/<session-id>/` 的用户级运行态目录，保存敏感数据和恢复数据，例如：

- 环境变量 bundle
- collector 缓存
- 待补传事件队列

### Commit Checkpoint

commit 时由 hook 上报的锚点事件。它记录 commit 所在的 session、workspace、git 上下文，以及当时的 agent metadata 快照。

### Commit Interval Cost

相邻两个 checkpoint（或 `session.started_at -> 首个 checkpoint`）之间，`primary API key` 在 `sub2api` 主账中的精确增量成本。

### PR Attribution Run

一次手动 PR 结算执行。它产出当前 PR 的最新归因摘要，并保留完整结算记录以供审计。

## 目标

1. `ae-cli` 必须先登录，未登录禁止 `start`
2. 用户身份以 `username` 为稳定主键，LDAP 与 relay/sub2api 侧通过 `username` 对齐
3. 每个 session 生成一把 session 专用 primary API key
4. 运行时持续采集 `Codex / Claude / Kiro` 本地会话元数据
5. 在 commit 时建立可追溯的 checkpoint 链
6. 允许对任意 PR 手动触发结算，并得到：
   - 主账 token/cost
   - 副账 metadata 摘要
   - attribution status / confidence / validation result
7. 后续自动化应建立在现有落库数据之上，而不是推翻模型

## 非目标

1. 第一版不做 PR opened/updated/merged 自动结算
2. 第一版不承诺严格的 per-commit 因果 token 成本
3. 第一版不自动在 agent 内部新建 worktree 时切换 session/key
4. 第一版不为每个 session 自动创建新的 `sub2api upstream account`
5. 第一版不把 `Kiro credit` 强行折算成 token

## 身份与凭证模型

### 身份主键

稳定身份主键使用 `username`。

原因：

- LDAP 邮箱与 sub2api 邮箱域可能不同，例如 `agora.io` 与 `shengwang.cn`
- 邮箱不是可靠主键
- 用户名在企业身份系统中更稳定，且手工创建 relay 用户时也可以统一遵守该主键

### 登录流程

`ae-cli` 必须先登录。

支持两条路径：

#### Relay SSO

1. `ae-cli login`
2. `ai-efficiency` 调 relay SSO 鉴权
3. 直接拿到 `relay_user_id + username`
4. 本地用户绑定 `relay_user_id`

#### LDAP

1. `ae-cli login`
2. `ai-efficiency` 先完成 LDAP 认证
3. 使用 LDAP 的 `username` 去 relay/sub2api 查人
4. 找到则绑定 `relay_user_id`
5. 找不到则自动创建 `sub2api user`
6. 创建成功后绑定 `relay_user_id`

#### LDAP 自动建人契约

第一版必须显式约定自动建人行为，避免不同实现产生不同身份语义。

当 LDAP 用户在 sub2api 中不存在时：

1. 使用 `username` 作为稳定主键
2. `email` 直接使用 LDAP 返回邮箱；若 LDAP 缺失邮箱，则使用可追溯的企业兜底邮箱
3. `password` 使用高熵随机值，由 `ai-efficiency` 生成且**不回传用户**
4. `notes` 或等价字段写入来源标记，例如 `provisioned_by_ai_efficiency_ldap`
5. 若 sub2api 中已存在同 `username` 用户，则只绑定，不自动覆盖其邮箱

这保证：

- 主身份由 `username` 决定
- 邮箱差异不会造成错绑
- 不要求用户后续使用 sub2api 本地密码登录

### Relay Provider 能力扩展

当前 `relay.Provider` 只有 `FindUserByEmail`，不足以支撑 username 主键模型与 session key 生命周期管理。

第一版需要补充：

- `FindUserByUsername`
- `CreateUser`
- `CreateUserAPIKey(user_id, name, group_id, expires_at)`
- `RevokeUserAPIKey(key_id)` 或等价的禁用能力
- `ListUsageLogsByAPIKeyExact(api_key_id, from, to)` 或等价的精确时间窗主账查询能力

`sub2api` 源码本身已具备：

- admin 创建 user 能力
- repo 层按 `username` 查询能力
- repo 层按精确时间窗过滤 `usage_logs` 的能力

因此缺口主要在 relay provider 接口与 HTTP 适配层，而不是底层数据能力。

### Session 与 API Key

第一版明确采用：

- `sub2api user : API key = 1 : n`
- `session : primary API key = 1 : 1`

`ae-cli start` 时：

1. 确认当前用户已经映射到稳定的 `sub2api user`
2. 为当前 session 创建一把专用 primary API key
3. 将该 key 绑定到指定 group/pool
4. 后续所有主账用量都以这把 key 为精确归因依据

**术语对齐**：本文中的“session primary API key”是概念名。第一版在 backend/session schema、API 请求和 marker 中统一落为现有字段名 `relay_api_key_id`，不在 v1 做字段重命名。

**说明**：本文中的稳定身份实体是 `sub2api user`。`sub2api upstream account` 仍由 group/pool 路由在运行时选择，并以 `usage_logs.account_id` 记录执行落点。

### Group / Pool 解析规则

第一版必须定义 session key 绑定到哪个 group/pool 的确定性规则。

推荐优先级：

1. repo 级显式绑定
   - `repo_config.relay_provider_name`
   - `repo_config.relay_group_id`
2. 系统级默认 relay provider 与默认 group
3. 若仍无法解析，则 `ae-cli start` 失败，拒绝启动可计费 session

要求：

- 解析结果必须写入 bootstrap 响应
- 同一 repo 的相同启动上下文必须得到同一 provider/group
- group 解析失败不能 silently fallback 到任意默认值

### Session Key 生命周期

第一版 session key 不能无限期存活，否则会污染主账。

要求：

1. key 名称包含可追溯 session 标识，例如 `ae-session-<short-session-id>`
2. key 创建时就带明确过期时间
3. `ae-cli stop` 时 best-effort 立即 revoke/disable
4. backend 定时清理已结束或 abandoned session 的残留 key
5. 若恢复 session 时发现 key 已失效，则废弃旧 session，重新 bootstrap

## 总体架构

### 数据分层

- **主账**：`sub2api usage_logs`
- **副账**：`Codex / Claude / Kiro` 本地会话元数据
- **锚点层**：`commit checkpoints`
- **汇总层**：`PR attribution runs` 与 `pr_records` 最新摘要

### 第一版主链路

1. `ae-cli login`
2. `ae-cli start`
3. 后端通过显式 bootstrap API 创建 session、解析 relay identity、发放 session key
4. 本地写入 workspace marker 与 runtime bundle
5. agent 进程继承环境变量
6. collector 采集本地 metadata
7. hook 在 commit 时上报 checkpoint
8. 用户手动触发 PR 结算
9. 后端计算 PR total cost

### Bootstrap API

第一版不应复用现有简单的 `POST /api/v1/sessions` 直接建 session 流程，而应增加专门的 bootstrap API。

建议接口：

- `POST /api/v1/sessions/bootstrap`

建议请求字段：

- `repo_full_name`
- `branch_snapshot`
- `head_sha`
- `workspace_root`
- `git_dir`
- `git_common_dir`
- `workspace_id`

建议返回字段：

- `session_id`
- `started_at`
- `relay_user_id`
- `relay_api_key_id`
- `provider_name`
- `group_id`
- `route_binding_source`
- `runtime_ref`
- `env_bundle`
- `key_expires_at`

现有 `POST /api/v1/sessions` 可保留为兼容接口，但 `ae-cli v1` 的新流程应统一走 bootstrap API。

## 本地运行态存储

### Workspace Marker

路径：`<workspace_root>/.ae/session.json`

该文件只保存非敏感运行态。建议结构：

```json
{
  "session_id": "sess-uuid",
  "workspace_id": "ws-uuid",
  "runtime_ref": "runtime/sess-uuid",
  "provider_name": "sub2api",
  "relay_api_key_id": 12345,
  "created_at": "2026-03-26T21:00:00Z",
  "last_seen_at": "2026-03-26T21:05:00Z"
}
```

### Runtime Bundle

路径：`~/.ae-cli/runtime/<session-id>/`

用于保存敏感和易变数据：

- `env.json`：环境变量 bundle
- `collectors/`：collector 最近快照
- `queue/`：待补传事件

要求：

- 目录权限 `0700`
- 文件权限 `0600`

### Git Ignore

`ae-cli start` 自动将 `/.ae/` 写入：

- `git rev-parse --git-path info/exclude`

不修改仓库中被跟踪的 `.gitignore`。

若派生 workspace 首次通过 `env bootstrap` 补写 marker，则 hook 在写 `/.ae/session.json` **之前**也必须确保当前 workspace 对应 repo 的 `info/exclude` 已包含 `/.ae/`，避免 marker 作为未跟踪文件暴露给用户。

### 状态优先级与迁移

当前 `ae-cli` 仍使用全局状态文件。第一版需要定义迁移和优先级，避免多 workspace 行为不一致。

新优先级：

1. 当前 workspace 的 `/.ae/session.json`
2. 当前进程环境变量
3. 兼容旧版的 `~/.ae-cli/current-session.json`
4. 无状态

迁移规则：

- 若旧全局状态文件存在，且指向当前 workspace 对应的 active session，则首次访问时物化为 `/.ae/session.json`
- 物化成功后，workspace 相关命令以 marker 为准
- 旧全局状态文件仅作为兼容读取，不再作为新主状态源

## 环境变量模型

环境变量是第一版最可靠的上下文载体，用于在 agent 内部派生的新 workspace 中自举 session 绑定。

### Canonical AE 变量

- `AE_SESSION_ID`
- `AE_RUNTIME_REF`
- `AE_RELAY_API_KEY_ID`
- `AE_PROVIDER_NAME`
- `AE_ENV_VERSION`

### Tool-facing 变量

由 provider adapter 生成，按实际工具需要导出，例如：

- `OPENAI_API_KEY`
- `OPENAI_BASE_URL`
- `ANTHROPIC_API_KEY`
- `ANTHROPIC_BASE_URL`

第一版优先使用环境变量，不默认改写 `Codex / Claude / Kiro` 的配置文件。

## Hook 设计

### 安装方式

hook 不放在 workspace 下的 `/.ae/hooks`。

原因：

- 派生 worktree 创建时，workspace 下的 `/.ae/hooks` 不存在
- 相对 `core.hooksPath` 会在新 worktree 中失效或解析到不存在路径

第一版采用共享 hook 目录：

- 目录：`$(git rev-parse --git-common-dir)/ae-hooks`
- `core.hooksPath` 指向该绝对路径

这样普通仓库和 linked worktree 共用同一套 hooks。

### 与现有 Hooks 的兼容

兼容现有 hooks 是第一版硬要求。

安装流程必须：

1. 检查现有 `core.hooksPath`
2. 若已有自定义 hooks path，则记录为 `legacy_hooks_path`
3. AE 共享 hook runner 在完成自身逻辑后，继续链式执行同名 legacy hook
4. 若未设置 `core.hooksPath`，但默认 hooks 目录下存在同名可执行 hook，也必须链式执行
5. 若检测到无法安全链式执行（递归、自引用、无权限），则 `ae-cli start` 拒绝接管 hooks，并明确报错

禁止直接覆盖并丢弃原有 hook 逻辑。

### 触发点

第一版仅使用两个 hook：

- `post-commit`
- `post-rewrite`

hook 自身保持极薄，只负责调用：

- `ae-cli hook post-commit`
- `ae-cli hook post-rewrite`

所有业务逻辑都在 `ae-cli hook` 子命令中实现，避免 shell hook 自己拼复杂逻辑和 HTTP 请求。

## Hook 数据来源

每次 hook 运行时，按固定顺序取数据：

### A. Git 现场

- `git rev-parse --show-toplevel`
- `git rev-parse --git-dir`
- `git rev-parse --git-common-dir`
- `git rev-parse HEAD`
- `git rev-list --parents -n 1 HEAD`
- `git symbolic-ref --short -q HEAD`

### B. Workspace Marker

- `<workspace_root>/.ae/session.json`

### C. 环境变量

- `AE_SESSION_ID`
- `AE_RUNTIME_REF`
- `AE_RELAY_API_KEY_ID`
- `AE_PROVIDER_NAME`

### D. Runtime Collector Cache

- `~/.ae-cli/runtime/<session-id>/collectors/...`

## Hook 工作流

### `post-commit`

1. 读取 Git 现场，得到 `repo/workspace/commit/branch/head`
2. 解析当前 workspace marker
3. 若 marker 不存在，则尝试读取环境变量
4. 若 marker 不存在但环境变量存在：
   - 将当前 commit 视为 `env bootstrap`
   - 先确保当前 repo 的 `info/exclude` 忽略 `/.ae/`
   - 自动为当前 workspace 补写 `/.ae/session.json`
5. 若 marker 与环境变量都不存在：
   - 本次 checkpoint 标记为 `unbound`
6. 从 collector cache 读取当前累计 metadata 快照
7. 组装 `commit checkpoint event`
8. 上报后端；失败则写本地 queue，commit 本身不失败

### `post-rewrite`

1. 读取 rewrite 输入
2. 获取 `old_commit_sha -> new_commit_sha`
3. 绑定当前 session/workspace
4. 上报 `commit rewrite event`
5. 上报失败同样 fail-open

### Hook Event Idempotency

fail-open 与本地 retry queue 必须配套显式去重，否则补传会重复写 checkpoint。

第一版要求：

- `post-commit event_id = sha256("checkpoint" + repo_config_id + commit_sha)`
- `post-rewrite event_id = sha256("rewrite" + repo_config_id + old_commit_sha + new_commit_sha + rewrite_type)`

本地 queue、HTTP 上报和数据库落库都必须携带同一个 `event_id`。

后端处理要求：

- checkpoint 以 `event_id` 或等价唯一键 UPSERT
- rewrite event 以 `event_id` 或等价唯一键 UPSERT

## 派生 Workspace 的映射策略

第一版**不尝试在 worktree 创建瞬间拦截和分裂 session**。

派生 workspace 的 session/key 映射依赖于：

- commit 时的 workspace marker
- 或 commit 时继承下来的环境变量

因此：

- 新 worktree 创建时，不自动生成新 session
- 如果 agent 进程继承了父 session 环境变量，那么新 worktree 的**第一次 commit** 可以通过 env 自举绑定到当前 session
- 若用户在没有 marker、也没有 env 的上下文中首次 commit，则该 checkpoint 为 `unbound`

结论：

- 第一版的唯一隔离边界仍然是 `session / primary API key`
- workspace 只是执行现场与分析维度，不是自动切账边界

## Agent Metadata Collector

collector 在 session 存续期间运行，负责将本地工具的会话信息归一化为**累计快照**，供 hook 和结算使用。

### 设计原则

1. collector 只做 best-effort，不阻塞开发
2. collector 输出**累计值**，结算时通过 delta 计算区间成本
3. collector 原始片段保存在 `raw_payload`
4. 不要求所有工具都有 token 口径；不同工具允许不同单位

### Codex

从本地 session 文件中提取：

- `source_session_id`
- `total_token_usage.input_tokens`
- `total_token_usage.cached_input_tokens`
- `total_token_usage.output_tokens`
- `total_token_usage.reasoning_output_tokens`
- `total_token_usage.total_tokens`

### Claude

从本地 session JSONL 提取 assistant 消息中的 usage 字段，并在本地累计：

- `input_tokens`
- `output_tokens`
- `cache_creation_input_tokens`
- `cache_read_input_tokens`

### Kiro

第一版仅采：

- `conversation_id`
- `credit_usage`
- `context_usage_pct`

不强制折算成 token。

## 数据模型

### `sessions` 扩展

在现有 `sessions` 基础上增加：

- `relay_user_id`
- `relay_api_key_id`
- `provider_name`
- `runtime_ref`
- `initial_workspace_root`
- `initial_git_dir`
- `initial_git_common_dir`
- `head_sha_at_start`
- `last_seen_at`

说明：

- `initial_*` 只保存启动快照
- session 本身不绑定单一 workspace 作为隔离边界

### `session_workspaces`

记录一个 session 在哪些 workspace 中被观察到。

该表仅记录**已绑定 session** 的 workspace 观察关系。

建议字段：

- `id`
- `session_id`
- `workspace_id`
- `workspace_root`
- `git_dir`
- `git_common_dir`
- `first_seen_at`
- `last_seen_at`
- `binding_source`：`marker|env_bootstrap|manual`

### `agent_metadata_events`

记录归一化后的 agent 元数据快照。

该表要求 `session_id` 非空，因为第一版 collector 仅在已知 session 语境下运行。

建议字段：

- `id`
- `session_id`
- `workspace_id`
- `source`：`codex|claude|kiro`
- `source_session_id`
- `usage_unit`：`token|credit|unknown`
- `input_tokens`
- `output_tokens`
- `cached_input_tokens`
- `reasoning_tokens`
- `credit_usage`
- `context_usage_pct`
- `raw_payload`
- `observed_at`

### `commit_checkpoints`

commit 归因锚点。

建议字段：

- `id`
- `session_id`（nullable，允许 `unbound`）
- `workspace_id`
- `repo_config_id`
- `commit_sha`
- `parent_shas`
- `branch_snapshot`
- `head_snapshot`
- `binding_source`
- `agent_snapshot`
- `captured_at`

要求：

- `workspace_id` 非空
- `session_id` 可为空
- 唯一键至少覆盖 `(repo_config_id, commit_sha)`
- hook 补传必须使用稳定 `event_id` 保证 idempotency

### `commit_rewrites`

记录 amend / rebase / squash 的 rewrite 关系。

建议字段：

- `id`
- `session_id`（nullable，允许 `unbound`）
- `workspace_id`
- `rewrite_type`
- `old_commit_sha`
- `new_commit_sha`
- `captured_at`

要求：

- 唯一键至少覆盖 `(repo_config_id, old_commit_sha, new_commit_sha, rewrite_type)`
- 允许无 session 但必须有 workspace/git 上下文

### `pr_attribution_runs`

记录每次手动 PR 结算。

建议字段：

- `id`
- `pr_record_id`
- `trigger_mode`：第一版固定 `manual`
- `triggered_by`
- `status`：`completed|failed`
- `result_classification`：`clear|ambiguous`
- `matched_commit_shas`
- `matched_session_ids`
- `primary_usage_summary`
- `metadata_summary`
- `validation_summary`
- `error_message`
- `created_at`

### `pr_records` 摘要字段扩展

补充：

- `attribution_status`：`not_run|clear|ambiguous|failed`
- `attribution_confidence`
- `primary_token_count`
- `primary_token_cost`
- `metadata_summary`
- `last_attributed_at`
- `last_attribution_run_id`

### 旧 Heuristic 字段的并存规则

现有 `pr_records` 中的 heuristic 字段（例如旧 labeler 写入的 `session_ids`、`token_cost`、`ai_label`）在第一版不会立即删除，但必须降级为 legacy fallback。

规则：

1. 手动 PR 结算**不得**覆盖 legacy heuristic 字段
2. 新 UI / API 一旦存在 attribution summary，应优先展示 attribution 结果
3. 仅当 PR 从未执行过 attribution run 时，才允许回退展示 legacy heuristic 字段

这样可以避免同一 PR 同时出现两套互相矛盾的成本口径

## SCM 前置能力

手动 PR 结算必须能拿到 PR 当前 commit 集合。

第一版需要扩展 `SCMProvider`：

- `ListPRCommits(ctx, repoFullName string, prID int) ([]string, error)`

没有该能力，settlement 无法确定“当前 PR 的 commit 集合”，整个归因流程无法成立。

## Relay / Sub2api API 能力需求

第一版需要 relay provider 支持：

- `FindUserByUsername(username)`
- `CreateUser(username, email, ...)`
- `CreateUserAPIKey(user_id, name, group_id, expires_at)`
- `RevokeUserAPIKey(key_id)`
- `ListUsageLogsByAPIKeyExact(api_key_id, from, to)`

推荐的精确主账查询方式是**按 usage logs 明细拉取**，再由 `ai-efficiency` 本地聚合。原因：

- manual 结算频率较低
- 明细可直接保留 `account_id` 证据
- 后续可同时支持 aggregate 与 debug drilldown

目标返回字段至少包括：

- `request_id`
- `created_at`
- `api_key_id`
- `user_id`
- `account_id`
- `group_id`
- `model`
- `input_tokens`
- `output_tokens`
- `cache_tokens`
- `total_tokens`
- `total_cost`
- `actual_cost`

## 手动 PR 结算流程

接口建议：

- `POST /api/v1/prs/:id/settle`

### 结算步骤

1. 从 SCM 获取 PR 当前 commit 集合
2. 结合 `commit_rewrites` 解析当前有效 commit 集合
3. 在 `commit_checkpoints` 中找到这些 commit 的 checkpoint
4. 关联出候选 session
5. 对每个候选 session，按时间排序**该 session 的所有 checkpoint**，而不是只排序 matched PR checkpoints
6. 为每个 checkpoint 定义其所属区间：
   - `from = 上一个 overall checkpoint.captured_at`
   - 若不存在上一个 overall checkpoint，则 `from = session.started_at`
   - `to = 当前 checkpoint.captured_at`
7. 仅当“当前 checkpoint 的 commit_sha 属于 PR 当前 commit 集合”时，才把该区间纳入 PR settlement
8. 所有以非 PR commit 结尾的区间都从 PR settlement 中排除
9. 用 `relay_api_key_id + [from, to)` 拉取 `sub2api usage logs`
10. 聚合得到该区间的：
   - `token`
   - `cost`
   - `account_id` breakdown
11. 读取相同区间的 agent metadata 快照 delta
12. 汇总所有区间，生成一条 `pr_attribution_run`
13. 将最新摘要写回 `pr_record`

补充说明：

- 若某个 PR 的首个 matched checkpoint 在该 session 中没有更早的 overall checkpoint，则该首个区间从 `session.started_at` 开始
- 这类区间可能包含首个 commit 之前的未提交探索成本
- 因此该场景不应得到 `high confidence`

### 关键口径

PR 结算使用的是：

- `commit interval cost`

而不是：

- `strict per-commit causal cost`

PR 的主指标为：

- `PR total attributable token`
- `PR total attributable cost`

## Commit Interval Cost 口径

严格意义上的“每个 commit 的 token 消耗”无法通过被动采集方案精确得到。

原因：

1. 请求发生时，commit 还不存在
2. 一次请求可能服务 0 个、1 个或多个 commit
3. 区间切窗只能得到“锚点之间的开发成本”，不是因果成本

因此第一版对外口径统一为：

- `commit interval cost`

定义：

- 以 commit checkpoint 为终点
- 以前一 checkpoint 或 `session.started_at` 为起点
- 统计该区间内 `primary API key` 在 `sub2api usage logs` 的精确增量

## 验证与置信度

### Validation Result

第一版只做标记，不自动纠偏：

- `consistent`
- `metadata_missing`
- `primary_missing`
- `mismatch`

### Confidence

- `high`
  - 所有 matched checkpoint 都有明确 session 绑定
  - 主账查询完整
  - 无明显 workspace/session 缺口
- `medium`
  - 存在跨 workspace 或中间未纳入 PR 的 session 活动
  - 但主账和 checkpoint 仍可计算
- `low`
  - 主账或 metadata 缺失
  - 存在 `unbound` checkpoint

### Result Classification

- `clear`
- `ambiguous`

### Ambiguous 条件

以下任一情况直接进入 `ambiguous`：

1. PR commit 无法映射到任何 checkpoint
2. 命中的 checkpoint 为 `unbound`
3. session 缺少 `relay_api_key_id`
4. 精确主账查询不可用
5. rewrite 链不完整，无法恢复当前 commit 映射
6. 需要的 SCM commit 枚举能力不可用

## 失败与恢复策略

### Hook Fail-open

hook 绝不阻止开发者提交代码。

上报失败时：

- commit 正常完成
- 事件写入本地 queue
- 后续由 `ae-cli flush` 或会话恢复流程补传

### `ae-cli start` 恢复

1. 查当前 workspace 的 `/.ae/session.json`
2. 读取 `session_id`
3. 到 backend 校验 session 是否仍为 `active`
4. 从 `~/.ae-cli/runtime/<session-id>/` 恢复 runtime bundle
5. 任一环失败则废弃旧状态并新建 session

## 安全要求

1. `/.ae/` 不保存 secret
2. `~/.ae-cli/runtime/` 保存 secret，权限必须最小化
3. 上报日志禁止打印 API key secret
4. raw payload 允许保留，但必须可控并便于后续脱敏
5. stop/shutdown 时允许清理 runtime bundle，但不得影响已落库主链

## 第一版验收标准

1. `ae-cli login` 成为 `start` 的强前置条件
2. 能基于 `username` 完成稳定 relay/sub2api 身份映射
3. `ae-cli start` 能创建 session 与 primary API key
4. workspace marker 与 runtime bundle 能正确写入
5. 共享 hooks 在普通仓库与 linked worktree 中都可工作
6. 在 commit 时能产生 checkpoint
7. 派生 workspace 在首次 commit 时若继承 env，可完成 `env bootstrap`
8. 能持续采集 `Codex / Claude / Kiro` 元数据
9. 能手动对一个 PR 触发结算
10. 能输出：
    - 主账 token/cost
    - metadata summary
    - validation result
    - confidence / ambiguous 标记

## 后续演进

本设计故意将自动化放到第二阶段：

1. 复用当前 `commit checkpoints + attribution runs`
2. 在 webhook 或定时任务上自动触发相同结算流程
3. 逐步增加更深的工具集成与更强的 workspace rebind 能力

第一版不依赖这些自动化能力成立。
