# PR Usage Snapshots 设计文档

**Date:** 2026-05-20  
**Status:** 设计已确认，待实现  
**Scope:** `frontend/`, `backend/`, `ae-cli/`, `docs/`  
**Related:**  
- [2026-05-13-sessionless-local-tool-attribution-design.md](./2026-05-13-sessionless-local-tool-attribution-design.md)  
- [2026-05-14-legacy-session-staged-cutover-design.md](./2026-05-14-legacy-session-staged-cutover-design.md)  
- [2026-04-15-cli-start-auto-repo-sync-design.md](./2026-04-15-cli-start-auto-repo-sync-design.md)  
- [docs/architecture.md](../../architecture.md)

项目级当前实现状态见 [`docs/architecture.md`](../../architecture.md)。

## Spec Relationship

- 本文定义 repo 详情页中 **PR usage 可视化** 的新合同。
- 本文不再把用户可见产品心智建立在 `AI Label`、`Attribution`、`Confidence`、`Settle` 上，而是直接建立在 `tool_usage_events -> commit_checkpoint -> PR` 这条 sessionless 正式链路上。
- 本文承接 [`2026-05-13-sessionless-local-tool-attribution-design.md`](./2026-05-13-sessionless-local-tool-attribution-design.md) 中“tool-local artifacts + checkpoint binding”的方向，但进一步收口到一个更窄、更产品化的目标：
  - 用户在现有 PR 视图内直接看到 PR 总用量
  - 用户在现有 PR 视图内展开查看 commit 明细
- 本文不回写历史 PR attribution spec，把旧的 `settle`/`attribution run` 设计伪装成当前产品路径；新的产品合同以本文为准，历史 spec 继续保留其演进背景。

## Overview

当前 repo 详情页的 PR 表格仍然暴露一组偏“归因系统内部状态”的字段：

- `AI Label`
- `Attribution`
- `Confidence`
- `Settle`

这组字段的问题不在于它们完全错误，而在于它们对当前用户目标来说过度复杂。用户要看的不是“这条 PR 的归因流程是否跑过”，而是：

1. 这条 PR 一共消耗了多少 input / output / cache / reasoning / credits / requests
2. 这条 PR 下面每个 commit 分别消耗了多少

同时，这个设计必须满足两个现实约束：

1. repo 里的 PR 可能很多，不能在每次 `Sync PRs` 时对全历史 PR 全量重算 usage
2. `ae-cli` / hooks / checkpoint 主链要先能把本地 workspace 与系统侧 repo/project 稳定关联，否则后续 usage 与 checkpoint 根本无法落到同一 repo scope

在这些约束下，本文选择一条产品与实现都更收敛的路径：

- 前端只保留现有 PR 视图
- PR 列表直接显示 usage summary
- PR 详情直接显示 commit 明细
- 后端引入专用 PR usage snapshot 模型
- `Sync PRs` 只增量刷新活跃 PR
- `ae-cli init` 与 `ae-cli sync`/hooks 都支持自动 ensure 系统侧 repo/project 存在

## Goals

1. 去掉 repo PR 视图中与当前用户目标不匹配的 `AI Label` / `Attribution` / `Confidence` / `Settle`
2. 在现有 PR 表格中直接展示 PR usage summary
3. 在 PR 详情展开区展示 commit usage 明细
4. 对 `Codex` / `Claude` 以 token 指标为主，对 `Kiro` 以 credit / request_count 为主
5. 在 PR 很多的 repo 中，保持 `Sync PRs` 的刷新成本与“活跃 PR 数量”相关，而不是与全历史 PR 数量线性相关
6. 把 repo/project 自动确保纳入正式主链，避免“本地 hooks 正常运行，但系统里根本没有对应 repo”导致整条链断掉

## Non-Goals

1. 不新增独立 repo 级 commit 页面
2. 不把 `Kiro credit` 强行折算成 token
3. 不承诺“严格因果意义上的 per-request / per-commit usage”
4. 不复活 `Settle` 作为新的用户触发入口
5. 不把当前 spec 扩展成通用报表系统
6. 第一版不要求把所有历史 PR 一次性回填完 usage snapshot

## Current Reality

### 已具备的正式链路

当前代码中已经具备以下正式链路能力：

1. `ae-cli` 能把本地工具 usage 作为 `tool_usage_events` 上报到 backend
2. `tool_usage_events` 已包含：
   - `input_tokens`
   - `output_tokens`
   - `cached_input_tokens`
   - `reasoning_tokens`
   - `credit_usage`
   - `request_count`
3. `post-commit` 正常路径先上报 checkpoint，再触发本地 attribution sync
4. backend 会将 `tool_usage_events` 绑定到 commit checkpoint
5. backend 已具备 `SCMProvider.ListPRCommits()`，可在刷新某个 PR snapshot 时读取当前 PR commit 集合

因此，从“事实源 -> checkpoint -> PR commit 集合”的角度，当前系统已经具备做 PR/commit usage snapshot 的基础链路。

### 当前采集链的明确 gap

虽然主链方向已具备，但当前采集层还不能被描述成“已完全满足目标合同”。至少存在以下 gap：

1. 当前正式 `Codex` 路径已经收口到 session `jsonl`，不再把 `logs_2.sqlite` 视为 PR usage 路径的必需依赖；实现方不得把 sqlite 适配缺口描述成当前 spec blocker
2. 部分真实 `Codex` session `jsonl` 的 `token_count` 不带 `response_id`；因此 scanner / parser 必须为同一文件中的每条 usage 更新生成稳定的行级 event identity，而不是把整文件压成一条记录
3. 当前 attributionlocal scanner 的 `ScanState` 仍只跟踪文件修改时间，尚未形成按行 watermark 的完成态增量扫描；在 parser identity 规则变化后，历史 session 文件可能触发一次性 backfill
4. `Kiro` 当前 `ObservedAt` 主要来自文件修改时间而非每个 turn 的精确事件时间，因此 commit 级绑定精度弱于 `Claude` / `Codex`

本文要求实现方在代码和文档中明确承认这些 gap，不得把当前采集现状表述成“已 fully done”。

## UI Contract

### Repo Detail PR List

repo 详情页保留现有 PR 列表，不新增独立 commit 页面。

从列表中移除：

- `AI Label`
- `Attribution`
- `Confidence`
- `Settle`

列表新增或保留的列：

- `Title`
- `Author`
- `Status`
- `Input`
- `Output`
- `Cache`
- `Reasoning`
- `Credits`
- `Requests`
- `Created`
- `Actions`

显示口径：

1. `Codex` / `Claude` 主要填 token 列
2. `Kiro` 主要填 `Credits` / `Requests`
3. 某列对某工具无意义时显示 `0`
4. “未计算”与“已计算但确实为 0”必须可区分：
   - `—`：snapshot 尚未生成
   - `0`：snapshot 已生成且该指标为零

### PR Details

`Details` 展开区不再显示 attribution vocabulary，例如：

- `Validation Reason`
- `Intervals`
- `Last Attributed`

展开区改为一个直接面向用户的 `Commits` 明细表，每行一个 commit。

字段：

- `Commit SHA`
- `Captured At`
- `Input`
- `Output`
- `Cache`
- `Reasoning`
- `Credits`
- `Requests`

补充显示规则：

1. 若某条 commit 命中 PR commit 集合但没有任何 checkpoint-bound usage，仍然显示该 commit，usage 列展示 `0`
2. 若明细未生成，展开时允许单条 PR 触发补算
3. 不再暴露 `Settle` 这类“系统内部归因动作”按钮

## Data Model

### `pr_records` summary fields

不再继续复用当前 `attribution_*`、`metadata_summary`、`last_attribution_run` 这组字段承载用户最终要看的 PR usage 主合同。

`pr_records` 新增正式 summary 字段：

- `usage_input_tokens`
- `usage_output_tokens`
- `usage_cached_input_tokens`
- `usage_reasoning_tokens`
- `usage_credit_usage`
- `usage_request_count`
- `usage_commit_count`
- `usage_refreshed_at`
- `usage_commit_snapshot_hash`

含义：

- `usage_*` 字段承载当前 PR 列表直接显示的值
- `usage_commit_count` 用于快速判断该 PR 当前 snapshot 对应多少个 commit
- `usage_refreshed_at` 用于判断是否过期
- `usage_commit_snapshot_hash` 用于检测 PR commit 集合是否变化，避免不必要的全量明细重写

### `pr_commit_usage_snapshots`

新增 `pr_commit_usage_snapshots` 表，专门服务 PR 详情里的 commit 明细。

字段：

- `pr_record_id`
- `commit_sha`
- `commit_checkpoint_id`
- `captured_at`
- `input_tokens`
- `output_tokens`
- `cached_input_tokens`
- `reasoning_tokens`
- `credit_usage`
- `request_count`
- `sort_order`
- `created_at`
- `updated_at`

约束：

1. 同一 `pr_record_id + commit_sha` 唯一
2. 不承担 repo 级通用 commit 索引职责
3. 只作为 PR details 渲染快照，不作为全局 commit 事实表替代品

## Refresh Strategy

### `Sync PRs`

`POST /api/v1/repos/:id/sync-prs` 的正式语义升级为：

1. 同步 PR metadata
2. 增量刷新活跃 PR 的 usage summary / commit snapshots

### 活跃 PR 定义

第一版活跃 PR 定义：

1. 所有 `open` PR
2. 最近时间窗口内的 PR
3. 本次 sync 中新建的 PR
4. 本次 sync 中 metadata 发生变化的 PR

时间窗口应配置化，默认可先与前端 PR 默认时间范围保持一致。

### 详情补算

当用户展开某条 PR 详情时：

1. 若该 PR 没有 commit snapshots，则只补算这一条 PR
2. 若该 PR 是 `open` 且 snapshot 已过期，则只补算这一条 PR
3. 详情补算不得升级成 repo 全量刷新

### 刷新成本边界

实现必须保证：

- `Sync PRs` 的主要成本与活跃 PR 数量相关
- 不能在每次 sync 时对所有历史 closed/merged PR 全量执行 `ListPRCommits + usage 聚合`

## PR Snapshot Calculation

某条 PR snapshot 的计算链路为：

1. 调用 `SCMProvider.ListPRCommits(repoFullName, prID)` 读取当前 PR commit 集合
2. 使用 `commit_rewrites` 扩展候选 SHA，兼容 rebase / rewrite 后的映射
3. 查询这些 commit 对应的 `commit_checkpoints`
4. 对每个 checkpoint 读取其绑定的 `tool_usage_events`
5. 按 commit 聚合：
   - `input_tokens`
   - `output_tokens`
   - `cached_input_tokens`
   - `reasoning_tokens`
   - `credit_usage`
   - `request_count`
6. 将 commit 明细写入 `pr_commit_usage_snapshots`
7. 对该 PR 下所有 commit summary 求和，写回 `pr_records.usage_*`

本设计的正式口径是：

- `PR current commit set usage snapshot`

而不是：

- `strict causal attribution`

## Repo / Project Ensure Contract

### Problem

当前正式代码路径存在一个关键断点：

1. `ae-cli init` 当前只安装 hooks，不会在系统中创建 repo/project
2. `checkpoint` 路径当前默认只做 `resolveRepoConfig`，查不到 repo 就直接报 `repo not found`
3. backend 虽然已有 `FindOrCreateFromRemote()` 能按 remote 自动创建 unbound repo，但尚未接入当前 `ae-cli init/sync/hooks/checkpoint` 正式主路径

如果不把系统侧 repo/project 自动确保纳入主链，后续 usage 与 checkpoint 即使本地都生成成功，也无法稳定关联到 backend repo scope。

### Required behavior

本设计要求 **A + B 双保险**：

#### A. `ae-cli init` 尽早 ensure

`ae-cli init` 在安装 hooks 的同时，必须 best-effort 调用后端 ensure repo 动作：

- 输入：
  - `remote.origin.url`
  - 当前 branch
- 行为：
  - 命中已有 repo 则复用
  - 未命中则创建 unbound repo
- 输出：
  - 明确告诉用户 repo ensure 成功或失败

要求：

1. repo ensure 失败不应阻止 hooks 安装
2. 但必须把失败暴露给用户，不能静默吞掉
3. 若当前未登录或无法获取 backend auth，则 `init` 仍安装 hooks，但必须明确输出 `repo ensure skipped` 或等价提示，而不是假装系统侧关联已经完成

#### B. `ae-cli sync` / hooks / checkpoint 懒创建兜底

即使用户没有成功执行 `init`，以下入口也必须能补建 repo：

- `ae-cli sync`
- `post-commit`
- `post-rewrite`
- backend checkpoint / rewrite 正式写路径

兜底逻辑：

1. 先按 remote identity / repo key 查找 repo
2. 未命中时调用正式 ensure repo 逻辑创建 unbound repo
3. 再继续写 checkpoint / rewrite / usage

### Backend service contract

backend 必须把现有 `FindOrCreateFromRemote(remoteURL, branch)` 从“内部能力”提升为正式主链能力。

正式能力定义：

- 输入：
  - `remoteURL`
  - `branch`
- 输出：
  - 一个已存在或新创建的 `repo_config`
- 特征：
  - 幂等
  - 基于 repo identity / repo key 去重
  - 可创建 `scm_provider_id = null` 的 unbound repo

`checkpoint` / `rewrite` 正式写路径在 repo lookup miss 时，必须走 ensure 流程，而不是直接报 `repo not found`。

### UX visibility

`ae-cli doctor` 必须把 repo association 作为显式检查项。

前端 repo 页或相关管理页至少要能让用户区分：

- repo 已存在且 bound
- repo 已存在但 unbound
- 当前本地 repo 还未在系统侧建立记录

## Collection Requirements For This Spec

为满足本设计，采集层至少需要以下保证：

1. `Claude` parser 继续按 message id 去重，并输出稳定的 token 字段
2. `Kiro` parser 继续以 `credit_usage + request_count` 为主，不以 token 真值为前提
3. `Codex` sqlite parser 必须接入正式 scanner 作为主路径；JSONL fallback 只能作为 fallback
4. scanner 必须补齐 watermark / 增量扫描状态，而不是长期依赖全量重扫
5. `tool_usage_events` 的 `dedupe_key` 仍是服务端幂等主键
6. hooks fail-open、spool replay、manual `ae-cli sync` 必须继续成立

如果这些保证尚未全部实现，spec 与代码都必须清晰标注“当前已具备 / 当前仍缺失”的边界。

## Error Handling

### PR summary display

- summary 未生成：显示 `—`
- summary 已生成且为零：显示 `0`

### Commit detail display

- commit 存在但没有 checkpoint-bound usage：明细里显示该 commit，usage 为 `0`
- detail 刷新失败但已有旧 snapshot：继续显示旧 snapshot，并显示 `usage_refreshed_at`

### Ensure repo failure

- `init`：显示失败，但 hooks 仍安装完成
- `sync`：应返回明确错误，告诉用户系统侧 repo ensure 未成功
- hooks：保持 fail-open，但事件要进入 queue/spool，后续 `sync` / `doctor` 可见

## Testing

### Backend

1. `Sync PRs` 只刷新活跃 PR
2. PR summary 能正确汇总 token 路径与 credit 路径
3. `commit_rewrites` 生效时，rebased PR 仍能命中旧 usage
4. 详情补算只刷新单个 PR
5. repo lookup miss 时，ensure repo 会成功复用或创建 unbound repo
6. checkpoint / rewrite 正式写路径能在 repo 先前不存在时继续落库

### ae-cli

1. `init` 会 best-effort ensure repo，并清晰输出结果
2. `sync` 在 repo 不存在时能先 ensure 再上报 usage
3. `post-commit` 正常路径保持 `checkpoint -> tool_usage`
4. `Codex` sqlite 主路径被正式接入 scanner
5. scanner watermark 能避免长期全量重扫

### Frontend

1. PR 列表移除 `AI Label / Attribution / Confidence / Settle`
2. PR 列表显示 usage summary
3. `Details` 展示 commit 明细
4. `—` 与 `0` 的语义正确
5. 大量 PR 时分页、月份筛选与详情展开仍可用

## Rollout Notes

建议 rollout 顺序：

1. backend repo ensure 主链接入
2. `ae-cli init/sync/hooks` 接 ensure repo
3. `Codex` sqlite 主路径与 scanner watermark 补齐
4. backend PR snapshot 模型与刷新逻辑
5. frontend PR 视图切换到 usage summary / commit details

这样可以先保证“系统里一定能有 repo/project 记录”，再把 PR usage 产品面稳定切过去。
