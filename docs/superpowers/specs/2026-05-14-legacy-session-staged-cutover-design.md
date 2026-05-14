# Legacy Session / Local Proxy Staged Cutover 设计文档

**Date:** 2026-05-14  
**Status:** Partially Implemented  
**Scope:** `ae-cli/`, `backend/`, `frontend/`, `docs/`  
**Related:**  
- [2026-03-26-session-pr-attribution-design.md](./2026-03-26-session-pr-attribution-design.md)  
- [2026-04-02-local-session-proxy-design.md](./2026-04-02-local-session-proxy-design.md)  
- [2026-05-13-sessionless-local-tool-attribution-design.md](./2026-05-13-sessionless-local-tool-attribution-design.md)  
- [docs/architecture.md](../../architecture.md)

项目级当前实现状态见 [`docs/architecture.md`](../../architecture.md)。

**Spec Relationship:**
- 本文不回写历史 session / local-proxy 设计文档，而是定义一个新的阶段性 cutover 合同。
- 本文承接 [`2026-05-13-sessionless-local-tool-attribution-design.md`](./2026-05-13-sessionless-local-tool-attribution-design.md) 的方向，但明确区分：
  - sessionless attribution 是新的正式归因路径
  - legacy session / local proxy 在 phase 1 中仅保留为历史兼容与排障对象
- 本文相对 [`2026-03-26-session-pr-attribution-design.md`](./2026-03-26-session-pr-attribution-design.md) 与 [`2026-04-02-local-session-proxy-design.md`](./2026-04-02-local-session-proxy-design.md) 的核心变化是：
  - `session` 不再是开发者的正式工作流主语
  - local proxy 不再是正式数据面
  - CLI、前端导航和文档都要切到 sessionless 主入口

## 概述

当前代码已经形成双轨：

1. legacy 轨：
   - `ae-cli start/stop/run/...`
   - backend session bootstrap / heartbeat / stop
   - local proxy
   - `session_usage_events` / `session_events`
   - 前端 `Sessions`
2. sessionless 轨：
   - 本地 artifact 解析
   - hooks 触发 sync
   - `tool_usage_events`
   - checkpoint-time binding
   - PR attribution 读 checkpoint-bound usage

问题不在于“双轨共存”，而在于**用户主入口仍然站在 legacy 轨上**。这会带来三个结果：

1. CLI 用户心智仍然是 “先 start 一个 session”
2. 前端用户心智仍然是 “Sessions 是主视图”
3. backend 和文档仍然要同时维护两套“正式路径”

本设计定义一个 **staged cutover**：

- phase 1 不做 destructive schema cleanup
- phase 1 先把 **用户主入口** 切到 sessionless
- 当前代码已经进一步下线：
  - `/sessions` 前端页与 backend 读接口
  - `/session-usage-events`、`/session-events`
  - local proxy package 与对应 CLI client 面

## 目标

1. `ae-cli` 正式入口改成 sessionless 工作流
2. `Sessions` 退出前端主导航
3. 新功能与新文档不再依赖 local proxy / session runtime
4. legacy session / local proxy 保留最小兼容能力，避免一次性破坏历史数据和旧链接
5. PR attribution、repo attribution、workspace checkpoint 继续正常工作

## 非目标

1. phase 1 不删除 legacy 数据表
2. phase 1 不立即删除 backend 所有 `/sessions*` 读接口
3. phase 1 不重写整个前端信息架构
4. phase 1 不一次性删掉所有历史测试
5. phase 1 不把 workspace / commit / repo attribution 新页面一步做到完整

## 核心决策

| Topic | Decision | Reason |
| --- | --- | --- |
| 用户主语 | 从 `session` 切换到 `repo / PR / workspace / checkpoint` | 与 sessionless attribution 一致 |
| CLI 主入口 | 使用 `ae-cli init` / `ae-cli sync` / `ae-cli doctor` | 避免继续强化 `start/stop` 心智 |
| 旧 CLI 命令 | 保留极薄兼容壳子，但从帮助和主文档中移除 | 受控切换，降低突然 break 风险 |
| 前端导航 | `Sessions` 退出主导航，换成 `Attribution` | 用户主入口必须反映正式模型 |
| `Sessions` 页面 | 降级为隐藏 debug 页面 | 历史排障仍有价值，但不应是主视图 |
| local proxy | 不再作为正式数据面 | 新归因只走 sessionless 链 |
| backend legacy API | phase 1 保留读兼容，停止继续依赖写链路 | 避免一次性大爆炸 |
| schema cleanup | 延后到 phase 2 | 先切用户面和运行时，再做 destructive cleanup |

## Phase 1 范围

### CLI

新增正式入口：

- `ae-cli init`
- `ae-cli sync`
- `ae-cli doctor`

语义：

- `init`
  - 安装或更新 shared git hooks
  - 初始化本地 attribution 状态目录
  - 校验 repo/workspace 是否满足 sessionless 路径
- `sync`
  - 手动触发一次本地扫描与补传
- `doctor`
  - 检查 hooks、workspace identity、本地 artifacts、spool、backend 连通性

legacy 命令：

- `start`
- `stop`
- `run`
- `attach`
- `ps`
- `shell`
- `flush`

phase 1 处理：

1. 不再出现在 README 主文档和用户推荐路径中
2. 命令仍可存在于二进制中
3. 执行时直接返回明确错误：
   - legacy workflow 已下线
   - 应使用 `init/sync/doctor`

### 前端

主导航：

- 用 `Attribution` 替换 `Sessions`

`Attribution` phase 1：

1. 不强求立即补全新的独立大页
2. 先作为新的导航入口和文案锚点
3. repo / PR 现有视图继续保留
4. 文案上明确 attribution 是正式入口

`Sessions`：

1. 从侧边栏移除
2. 路由保留
3. 页面标题与说明标成 legacy/debug
4. 不再作为常规导航目标

Dashboard：

1. 不再把 `Active Sessions` 当成首页主指标
2. phase 1 允许先替换成中性 attribution/usage 占位指标

### Backend

phase 1 保留但降级：

- `/sessions`
- `/session-usage-events`
- `/session-events`
- session detail read model
- provider-credentials

约束：

1. 新代码不得继续扩展这些路径
2. 新的正式归因链只允许依赖：
   - `tool_usage_events`
   - checkpoint
   - commit rewrite
   - PR settlement

### Runtime

local proxy 在 phase 1 中不再是正式数据面。

这不要求当场删除 proxy 代码，而是要求：

1. 用户主入口不再依赖它
2. 正式文档不再推荐它
3. 新归因正确性不再建立在 proxy/request usage 之上

## 兼容策略

### 为什么不立刻硬删

如果现在直接删除：

- CLI 现有命令会立即全部 break
- 前端旧链接和排障页会同时消失
- backend 历史数据读链会一起失效
- 任何遗漏都会很难定位

phase 1 采用 **入口切换 + 兼容壳子 + 历史只读** 的方式：

1. 用户主路径先切走
2. 历史路径保留最小可观测性
3. 等系统稳定后，再进入 destructive cleanup

## 测试要求

### CLI

必须覆盖：

1. `init` 成功初始化 hooks / 本地状态
2. `sync` 成功触发本地扫描与补传
3. `doctor` 能输出关键检查项
4. `start/stop/run/attach/ps/shell/flush` 改为明确失败并提示迁移

### Frontend

必须覆盖：

1. 侧边栏不再显示 `Sessions`
2. 新增 `Attribution` 导航入口
3. `/sessions` 页面文案明确标成 legacy/debug

### Backend

必须覆盖：

1. `toolusage/checkpoint/handler` 现有正式归因链保持通过
2. legacy session 读接口仍然可访问
3. 不再有新调用路径依赖 `/tool-usage-events/bind`

## Phase 2 方向

phase 2 再做真正删除：

1. 删除 local proxy runtime
2. 删除 `start/stop/run/attach/ps/shell/flush` 命令实现
3. 收缩或删除 `/sessions*`、`/session-usage-events`、`/session-events`
4. 删除前端 `Sessions` 页面
5. 清理 legacy schema / tests / docs

## 风险

1. `Attribution` phase 1 如果只是换导航名但没有足够的信息承接，用户会觉得“只是改名”
2. legacy CLI 命令如果直接 panic 或 silently no-op，会造成更难排障的问题；必须明确失败
3. backend 如果仍有隐藏路径依赖 local proxy 写链，会导致“表面切换，实则双轨”

## 验收标准

phase 1 完成后，应满足：

1. 新用户文档不再要求 `ae-cli start/stop/flush`
2. `Sessions` 不再是主导航
3. CLI 新正式入口变为 `init/sync/doctor`
4. 旧 CLI 命令明确提示已下线
5. 正式归因链只依赖 sessionless 路径
6. targeted tests 通过
