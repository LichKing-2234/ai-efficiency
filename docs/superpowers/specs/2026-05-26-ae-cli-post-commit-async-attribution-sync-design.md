# ae-cli Post-Commit Async Attribution Sync Design

**Date:** 2026-05-26  
**Status:** Current design with 2026-05-27 upload-throughput follow-up
**Scope:** `ae-cli/cmd/`, `ae-cli/internal/hooks/`, `ae-cli/internal/attributionlocal/`, `docs/`  
**Related:**  
- [2026-05-13-sessionless-local-tool-attribution-design.md](./2026-05-13-sessionless-local-tool-attribution-design.md)  
- [2026-05-23-global-git-hooks-design.md](./2026-05-23-global-git-hooks-design.md)  
- [`docs/architecture.md`](../../architecture.md)

## Spec Relationship

- 本文收紧当前 sessionless attribution 的 hook 合同：`post-commit` 不再负责在 hook 生命周期内完成完整 usage 扫描与上传。
- `tool-local artifacts -> tool_usage_events -> checkpoint/rewrite -> PR settle` 的主链路不变，仍以 [2026-05-13-sessionless-local-tool-attribution-design.md](./2026-05-13-sessionless-local-tool-attribution-design.md) 为准。
- 本文最初只调整 **何时** 触发 attribution sync、**如何** 持久化待同步状态、以及 **如何** 向用户暴露 pending/error；2026-05-27 的 backlog follow-up 在保留单条 `tool_usage_events` ingest 兼容性的前提下，新增批量 ingest 作为上传吞吐优化，不改变 checkpoint 表结构或 attribution 语义。
- 本文覆盖 `Codex`、`Claude`、`Kiro` 的统一 sync 触发框架，不引入只针对 `Codex` 的特例路径。

## Overview

当前 `ae-cli hook post-commit` 的执行路径是：

1. 解析 repo/workspace 上下文
2. 上传或补传 checkpoint/rewrite hook 事件
3. 在同一个 hook context 内运行 `attributionlocal.SyncEngine.Run(...)`

而 hook 的总超时目前是固定的 `10s`。这在本地 artifact 数量较小、已有增量状态缓存时通常可接受，但在以下场景下会失败：

1. 某个 workspace 首次进入新的 attribution state 路径，没有现成 `scan-state.json`
2. 本机历史 `~/.codex/sessions/**/*.jsonl` 规模很大
3. 某些历史大文件仍属于当前 workspace，需要先做 session ID 识别
4. hook 超时后被脚本 fail-open 吞掉，用户只看到 commit 成功，但今天的 usage 没进入后台

本设计将 `post-commit` 的职责改成：

1. **快速记录**
2. **持久化“该 workspace 需要 usage sync”**
3. **尽力拉起一个脱离 hook 超时的后台 runner**
4. **如果后台拉起失败，则在后续 `ae-cli sync` / `ae-cli doctor` / 下一次 hook 中继续补传**

## Current Problem Statement

需要解决的不是“本地没有 tool events”，而是“commit 后的自动同步路径把 checkpoint 上传和 full artifact scan 绑在一个短 hook 生命周期里，导致冷启动扫描超过 hook 时限，usage 既未即时上传，也没有对用户清晰暴露 backlog 状态”。

因此第一优先级是：

- **保证 commit hook 的短路径稳定、可预测、低时延**

而不是：

- 在 hook 内强行完成全部 usage 上报

## Goals

1. 将 `post-commit` 的主合同收敛为“快速 checkpoint + durable pending sync task + opportunistic background trigger”。
2. 让 `Codex`、`Claude`、`Kiro` 共用同一套异步补传框架，而不是为单个 provider 定制同步路径。
3. 保证 hook 正常路径可控制在用户可接受的 `3s` 内。
4. 保留“commit 后尽快自动补传一次”的体验，但不再让 hook 为 full scan 成功负责。
5. 当后台 runner 未成功完成时，让用户能在后续命令和下一次 hook 中明确看到 pending / error 状态。
6. 复用现有 `scan-state.json`、`spool.json`、workspace hook queue、`SyncEngine` 与 scanner/collector 逻辑，避免重写 provider 解析器。

## Non-Goals

1. 第一版不修改 backend API、`tool_usage_events` 表结构或 checkpoint 表结构；后续 backlog follow-up 只允许增加兼容性 backend API，不删除或改变单条 ingest 合同。
2. 第一版不引入新的常驻 daemon / launch agent / system service。
3. 第一版不改写现有 scanner/collector 的 provider 解析合同，只改调度边界。
4. 第一版不提供新的 GUI 页面来监控 sync task。
5. 第一版不尝试在 hook 内做“轻量预扫描”来抢先上传部分 usage。
6. 第一版不把 task 精细化到 commit 级；task 以 workspace 为聚合粒度。

## User Decisions Captured

本设计固化以下已确认的产品选择：

1. `commit` 后的主目标是：**checkpoint 和待同步状态尽快落盘，usage 异步补传**。
2. `post-commit` 要 **先后台触发一次补传**，失败后保留 backlog，由后续命令或下一次 hook 继续补传。
3. hook 额外引入的可接受时延上限是 **3s 内**。
4. 此次设计覆盖 **所有 tool 的统一框架**，不是只修 `Codex`。
5. 如果后台触发失败或 backlog 积压，需要在 `ae-cli doctor` / `ae-cli sync status` 中明确显示，并在后续 `post-commit` 中输出简短 warning。

## Approaches Considered

### Option A: Inline light scan plus detached full sync

- `post-commit` 内保留一段轻量扫描，再尝试后台 full sync
- 优点：
  - 某些小会话可能在本次 commit 后立刻看到部分 usage
- 缺点：
  - hook 里仍然保留扫描职责，边界不干净
  - 轻量扫描很容易继续膨胀成新的超时源
  - 需要再定义“轻量扫描”与“full sync”的双合同

### Option B: Durable queue plus opportunistic background runner

- `post-commit` 只做快速 checkpoint、pending task 落盘、warning 决策、后台 runner 触发
- 所有 usage 扫描与上传都由后台 runner 负责
- 优点：
  - hook 边界清晰
  - 更符合 `3s` 预算
  - 失败恢复路径统一
  - 所有 tool 共用一个异步调度框架
- 缺点：
  - 本次 commit 之后 usage 出现在后台的时间不再由 hook 直接保证
  - 需要新增 task/lease 的状态管理

### Option C: Always queue, no immediate background trigger

- `post-commit` 永远只落盘，完全依赖后续 `sync/doctor/下一次 hook`
- 优点：
  - 实现最简单
  - hook 最稳定
- 缺点：
  - 放弃“commit 后尽快自动补传一次”的体验
  - 不符合本次已确认的用户意图

### Recommendation

采用 **Option B**。

原因：

1. 它最准确地匹配“只让 hook 做快速记录，usage 由异步 runner 处理”的主目标。
2. 它保留了“commit 后尝试自动补传一次”的体验，不会把全部责任推给后续人工命令。
3. 它为所有 tool 提供一致的调度与恢复骨架，避免再出现 `Codex` 单独例外、其他 provider 走不同同步路径的情况。

## Design

### Architecture

同步链路拆成两个阶段：

1. **Hook fast path**
2. **Background sync runner**

`post-commit` fast path 只做以下动作：

1. 解析 repo/workspace 上下文
2. 上传 checkpoint，失败则继续沿用现有 fail-open queue
3. 写入或刷新 workspace 级 pending sync task
4. 决定是否输出简短 warning
5. 若当前没有活跃 runner，则尝试拉起一个脱离 hook timeout 的后台 runner

它**不再**直接在 hook 上下文中执行 `attributionlocal.SyncEngine.Run(...)`。

后台 runner 的职责是：

1. 读取 pending task
2. 抢占该 workspace 的 lease
3. 先 flush 旧的 hook queue
4. 再运行 `attributionlocal.SyncEngine.Run(...)`
5. 更新 task、`scan-state`、`spool` 和 last error
6. 释放 lease

模块边界建议保持为：

- `ae-cli/internal/hooks`
  - 负责 hook 事件、workspace sync task 入队、warning 输出、runner 触发、lease 管理
- `ae-cli/internal/attributionlocal`
  - 继续负责 scanner / collector / watermark / spool / upload
- 新增轻量 task/runner 层
  - 负责 workspace pending task 与 runner 生命周期，不把这些状态重新塞回 `hook.go` 或 `scanner.go`

### Workspace-Level Sync Task

pending sync task 按 **workspace 粒度** 建模，而不是按 commit 粒度。

推荐路径：

```text
~/.ae-cli/state/attribution/workspaces/<workspace_id>/sync-task.json
```

推荐字段：

```json
{
  "version": 1,
  "workspace_id": "...",
  "repo_root": "...",
  "server_url": "...",
  "auth_subject": "...",
  "repo_config_id": 2,
  "repo_key": "...",
  "status": "pending",
  "last_requested_at": "...",
  "last_started_at": "...",
  "last_completed_at": "...",
  "last_error": "...",
  "attempt_count": 0,
  "runner_pid": 12345,
  "lease_expires_at": "..."
}
```

其中真正的主语是：

- “这个 workspace 目前有新的 tool-local artifacts 需要扫描并上传”

而不是：

- “这个 commit 有一条独立 usage sync 任务”

这样做的原因是当前最重的成本来自 artifact 扫描。连续 commit 多次时，最合理的行为是合并成一个 workspace-level pending task，而不是启动多个相同扫描。

### Coalescing Contract

task 行为采用 **coalesce**：

1. 每次 `post-commit` 都把 task 刷成 `pending`
2. 只更新时间戳和上下文，不追加第二条同类任务
3. 如果当前已有活跃 runner，则不重复拉起 runner
4. 如果当前没有活跃 runner，才尝试新拉起

这意味着：

1. 多次 commit 不会放大成多次 full scan
2. 最新一次 `last_requested_at` 可以代表“截至何时仍有未处理变更”
3. background sync 只需要追平到“当前 workspace 的最新本地状态”

### Lease and Concurrency

runner 必须采用 workspace 级 **lease**，避免多个 runner 同时扫同一个 workspace。

lease 规则：

1. `post-commit` 触发 runner 前先检查 task 的活跃 lease
2. 有未过期 lease 时，不重复拉起 runner
3. runner 启动后需要显式抢占 lease；抢不到 lease 就立刻退出
4. lease 需要过期时间，避免进程崩溃后永久卡死
5. 活跃 lease 必须同时满足 `runner_pid` 仍存活；如果进程已经退出但 task 未写回，`doctor` / `sync status` / 后续 sync 应恢复为 pending 并记录 `last_error`
6. `ae-cli sync` 手工运行时也遵循同一套 lease 规则
7. runner 必须有总运行时上限，避免 backend 连接或本地扫描异常时长期占用进程；超时后按失败处理并保留 pending task

推荐语义：

- `status=pending`
  - 有待处理扫描，但当前没有活跃 runner
- `status=running`
  - 当前已有 runner 在处理
- idle
  - 不要求单独持久化为状态；当任务已追平且无 backlog 时，可删除 task 文件或保留一个无 `pending` 的清洁记录

### Runner Lifecycle

runner 生命周期如下：

1. 读取 `sync-task.json`
2. 校验上下文字段完整性
3. 抢占 lease，写入 `running`、`last_started_at`
4. 先执行 `hooks.Handler.FlushResolved(...)`
5. 再执行 `attributionlocal.SyncEngine.Run(...)`
6. 成功时：
   - 清空 `last_error`
   - 更新 `last_completed_at`
   - 如果运行期间没有新的 `last_requested_at` 需要继续处理，则移除 task 或标记为空闲
7. 失败时：
   - 保留 `pending`
   - 写入 `last_error`
   - 增加 `attempt_count`
   - 释放 lease，等待下次重试

第一版 runner 不要求长时间驻留。它是一次性后台进程，做完本轮工作就退出；如果超过运行时上限仍未完成，则释放 lease 并等待后续 sync 重试。

### Interaction with Existing Hook Queue, Spool, and Scan State

该设计必须复用并尊重现有三类状态：

1. **workspace hook queue**
   - 用于 checkpoint/rewrite fail-open replay
2. **`spool.json`**
   - 用于 tool usage 上传失败时保留待上传 event
3. **`scan-state.json`**
   - 用于 provider 扫描增量水位与 file mod 缓存

新增 `sync-task.json` 不取代这三者，而是只表达：

- “现在该不该启动一次 usage sync runner”

状态职责分工如下：

1. `hooks queue`
   - 保存尚未成功上传的 checkpoint/rewrite 事件
2. `sync-task`
   - 保存 workspace 是否需要一次 usage sync、runner 是否在跑、最后一次错误是什么
3. `scan-state`
   - 保存各 provider 的增量扫描水位
4. `spool`
   - 保存已扫描到但未成功上传的 normalized usage events

### Hook Fast Path Budget

`post-commit` 的正常路径必须控制在用户可接受的 `3s` 内。

实现上应把这视为硬合同，而不是“通常差不多”。

因此第一版必须避免以下行为出现在 hook fast path：

1. 遍历整个 `~/.codex/sessions`
2. 解析大 JSONL 文件
3. 遍历 Kiro IDE / Kiro CLI 全量执行记录
4. 运行完整 `SyncEngine.Run(...)`

hook 中允许的耗时操作仅限：

1. git 元信息读取
2. 轻量 checkpoint 上传或 fail-open queue 写入
3. `sync-task.json` 的读取和写入
4. 一次短小的后台 runner 触发动作

### Background Trigger Contract

本设计要求 `post-commit` 在成功落盘 task 后，**尽力** 触发一次后台 runner。

“尽力” 的定义是：

1. 成功触发则让 runner 去处理，不在 hook 中等待其完成
2. 触发失败也不能让 commit 失败
3. 触发失败时，task 仍保持 `pending`
4. 下次 `ae-cli sync`、`ae-cli doctor`、下一次 hook 仍能继续处理 backlog

该后台触发不要求进程常驻，也不要求系统级守护机制。它只是一次 detached child process。

### User-Facing Behavior

#### post-commit

正常路径：

- 默认静默

异常或 backlog 路径：

- 打印一行简短 warning
- 不打印大段日志
- 不阻塞 commit

warning 触发条件：

1. `sync-task` 落盘失败
2. 后台 runner 拉起失败
3. 当前 workspace 已存在 backlog，且最近一次 `last_error` 非空

建议文案风格：

```text
ae-cli: attribution sync pending for this repo; run 'ae-cli doctor' for details
```

#### ae-cli doctor

`doctor` 需要展示：

1. 当前 workspace 是否存在 pending sync task
2. `status` 是 `pending` 还是 `running`
3. `last_requested_at`
4. `last_completed_at`
5. `attempt_count`
6. `last_error`

它不负责主动触发 full sync，但必须把 backlog 和错误讲清楚。

#### ae-cli sync status

`sync status` 也需要展示同样的 workspace sync task 信息，至少包括：

1. 是否有 pending task
2. 是否有活跃 runner
3. 最近一次错误
4. 最近一次成功时间

### Failure Recovery

恢复合同如下：

1. runner 失败不丢数据
2. task 保持 `pending`
3. 下一次 `post-commit` 只刷新 pending，不重复堆任务
4. `ae-cli sync` 会主动消费 pending task
5. `ae-cli doctor` 会暴露 backlog 和 last error
6. lease 过期后允许回收，避免 crashed runner 永久卡死
7. tool-usage 上传遇到瞬时网关/限流错误（429/502/503/504）时，client 先做短重试；仍失败时保留剩余 events 到 spool，后续 sync 继续推进
8. 单次 tool-usage HTTP 上传必须有独立短超时，避免某个卡住的 backend/HTTP2 响应拖住整个 runner
9. 新扫描出的 events 如果上传中途失败，写入 spool 后仍必须返回 runner failure，不能把 task 删除成成功状态
10. durable sync 必须先把本轮扫描结果写入 spool，再按 `observed_end_at` 从新到旧 replay spool，避免新 commit 的 usage 被历史 backlog 长时间阻塞
11. replay 应优先使用兼容的批量 ingest，在单个 bounded request 内上传多个 `tool_usage_events`；如果 backend 不支持批量接口或批量 payload 出现 validation failure，CLI 必须回退到单条接口以保持版本兼容和错误隔离

如果 `sync-task.json` 本身损坏：

1. 后续命令允许重建 task 文件
2. 不影响已存在的 `spool.json` / `scan-state.json` / hook queue

### Tool Coverage

第一版统一应用于：

1. `Codex`
2. `Claude`
3. `Kiro`

统一的含义是：

1. 三类 provider 都通过同一个 workspace sync task 和同一个后台 runner 触发
2. 不针对 `Codex` 单独保留 hook 内联 sync 的旧逻辑
3. provider-specific 差异只留在现有 parser / scanner / collector 内部

### Migration and Compatibility

第一版兼容策略：

1. 现有 `ae-cli sync` 仍然保留，且会主动消费 pending task
2. 现有 hook queue / spool / scan-state 文件继续生效
3. 新增 `sync-task.json` 后，不要求清理旧状态目录
4. 若某 workspace 已经有健康的 `scan-state.json`，新框架也不改变其 scanner 结果，只改变调度方式

对于当前“只有 `collectors/latest.json`、没有 `scan-state.json`”的 workspace，第一版允许首次 runner 花更长时间完成冷扫描；区别在于：

1. 它不再消耗 hook fast path 的时限
2. 失败时会留下明确的 pending 和 last error

## Testing Strategy

### Unit Tests

至少覆盖：

1. 多次 `post-commit` coalesce 成单个 workspace task
2. 活跃 lease 下不重复拉起 runner
3. runner 成功后 task 清理或转空闲
4. runner 失败后保留 pending 和 `last_error`
5. `ae-cli sync` 会消费已有 pending task
6. `doctor` / `sync status` 输出 pending / running / error
7. warning 只在约定条件下出现
8. runner 进程已退出但 `sync-task.json` 仍为 `running` 时，诊断命令会恢复 stale lease 并允许后续 sync 重试

### Integration Verification

至少验证：

1. 在大 `~/.codex/sessions` 背景下，`post-commit` 快速返回，不再因 full scan 超时
2. detached runner 后续能把当天 `Codex` event 真正传到 `/api/v1/events`
3. `Claude` / `Kiro` 也通过同一 task/runner 骨架被处理
4. runner 失败后，后续 `ae-cli sync` 能继续补传 backlog
5. Codex 从主 checkout 启动、commit 发生在同 repo linked worktree 时，runner 仍能把 Codex event 上传到该 linked worktree 的 `workspace_id`，而不是只上传 checkpoint
6. backend 瞬时 502 不会永久卡住 backlog；retry 失败后仍保留 spool，下一次 sync 可继续推进
7. backend 上传连接卡住时，单次 request 超时和 runner 总超时能让本地进程退出并留下可恢复状态
8. 上传失败导致新扫描 events 进入 spool 时，`sync status` 仍能看到 pending/error，而不是误报 `Sync Task: none`
9. 当历史 backlog 很大时，刚扫描出的新 usage 会在下一次 replay 中优先尝试上传，`/events` 不再等待旧 backlog 全部追平
10. 当历史 backlog 较大时，批量 ingest 可以用 bounded chunk 减少 HTTPS round trip 数；服务端仍按 `dedupe_key` 幂等处理重复 event

## Rollout Notes

第一版实施时应保持范围收敛：

1. 保留单条 backend ingest contract；批量 ingest 只能作为兼容扩展
2. 不重写 scanner/collector 主逻辑
3. 只重构 CLI 里的 sync 调度边界、任务状态与 warning/doctor/status 可见性

完成该设计后，`docs/architecture.md` 也需要更新为：

- hook fast path 只负责 checkpoint 与 pending sync task
- full tool-local scan 由异步 runner 执行

以避免继续把“commit hook 内联 full sync”写成当前架构现状。
