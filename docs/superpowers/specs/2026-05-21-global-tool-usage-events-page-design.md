# Global Tool Usage Events Page Design

**Date:** 2026-05-21  
**Status:** Proposed current design  
**Scope:** `backend/internal/handler/`, `backend/internal/toolusage/`, `frontend/src/router/`, `frontend/src/views/`, `frontend/src/api/`, `docs/`  
**Related:**  
- [2026-05-13-sessionless-local-tool-attribution-design.md](./2026-05-13-sessionless-local-tool-attribution-design.md)  
- [2026-05-20-pr-usage-snapshots-design.md](./2026-05-20-pr-usage-snapshots-design.md)  
- [`docs/architecture.md`](../../architecture.md)

## Spec Relationship

- 本文定义一个新的全局页面 `/events`，用于浏览已经入库的 `tool_usage_events`。
- 它补足当前产品只到 `Repo -> PR -> Commit` 聚合层的可见性缺口，但**不改变** sessionless attribution 的绑定合同。
- `tool_usage_event -> checkpoint/commit -> PR(best-effort)` 的链路仍以 [`2026-05-13-sessionless-local-tool-attribution-design.md`](./2026-05-13-sessionless-local-tool-attribution-design.md) 为准。
- `PR` 在本页中是派生信息，不是 event 的原生字段；commit/PR 聚合的正式产品合同仍以 [`2026-05-20-pr-usage-snapshots-design.md`](./2026-05-20-pr-usage-snapshots-design.md) 为准。

## Overview

当前前端主要展示三层聚合结果：

1. repo
2. PR
3. commit usage snapshot

这对日常汇总足够，但对以下场景不够：

1. 解释某个 commit 数值为什么是这个值
2. 排查某条上报来自哪个本地来源文件
3. 确认某次上报是否已绑定到 checkpoint / commit
4. 对比不同工具（Codex / Claude / Kiro）的事件级事实上报

因此新增一个独立的全局页面：

```text
/events
```

该页面第一版只展示**后端已入库**的 `tool_usage_events`，不直接展示本地 queue / spool / hooks.jsonl 等尚未上报到后端的状态。

## Goals

1. 提供 event 级可见性，而不只停留在 PR / commit 聚合层。
2. 让 admin 能看到完整的事件调试信息，包括原始来源文件和 `raw_payload`。
3. 让普通登录用户只能看到自己的事件，同时保留足够的浏览与定位能力。
4. 复用当前 `tool_usage_events` 数据模型，避免第一版引入新的本地状态采集协议。
5. 保持 `/events` 为独立全局页面，而不是 repo detail 的附属面板。

## Non-Goals

1. 第一版不展示本地 queue / spool / pending hook 状态。
2. 第一版不做本地 agent 在线状态监控。
3. 第一版不做 BI 级趋势分析、保存筛选器、订阅或告警。
4. 第一版不把 `PR` 当作 event 的强字段；`PR` 仅在明细中反查。
5. 第一版不引入独立的 `/events/:id` 详情页，只使用右侧抽屉。

## Approaches Considered

### Option A: 纯调试页

- `/events` 直接是一张大表 + admin 原始明细
- 优点：实现最短
- 缺点：普通用户价值弱，首页缺少整体感知

### Option B: 概览 + 事件表 + admin 明细抽屉

- 顶部 summary，下面 event 表，右侧抽屉看完整详情
- 优点：兼顾运营浏览和排障下钻
- 缺点：比纯表格多一层接口和前端状态

### Option C: 重型观测台

- 概览、图表、趋势、深链、保存查询一次做全
- 优点：能力最全
- 缺点：明显超出第一版范围

### Recommendation

采用 **Option B**。

原因：

1. 它符合“独立页面 + 事件级可见性 + admin 下钻 raw record”的目标组合。
2. 它保留了面向普通用户的默认浏览形态，不会把页面直接做成只有排障工程师能用的控制台。
3. 它不依赖新的 agent 上报协议，第一版可直接建立在现有 `tool_usage_events` 表上。

## Information Architecture

### Route

- 新增路由：`/events`
- 作为一级全局页面，和 `/repos`、`/settings` 同级
- 使用现有 `AppLayout`

### Page Structure

页面分成两层：

1. **顶部概览区**
2. **下方事件表**

右侧使用抽屉展示 event 明细，不跳转独立详情页。

### Default Time Window

- 默认时间范围：最近 `24h`
- 首屏必须带默认时间范围，避免全量查询造成页面和接口过重

## Permissions

### Admin

admin 可以：

1. 查看所有事件
2. 按用户、repo、tool、绑定状态筛选
3. 查看完整 `raw_source_path`
4. 查看完整 `raw_source_locator`
5. 查看完整 `raw_payload`
6. 在明细中查看 event -> checkpoint -> commit -> PR 的完整链路

### Regular User

普通登录用户：

1. 只能看到自己的事件
2. 不能看到其他用户数据
3. 列表仍显示 `tool + source basename`
4. 明细中不显示完整绝对路径和完整 `raw_payload`
5. 仍可看到必要解释字段：
   - 何时上报
   - 用量指标
   - 是否已绑定
   - 绑定到哪个 checkpoint / commit

## Data Semantics

### Primary Entity

本页的主实体是一条 `tool_usage_event`。

一行列表数据代表一条后端已入库的 normalized usage event，而不是：

1. 一个 repo
2. 一个 PR
3. 一个 commit 聚合
4. 一次本地 hook 上传批次

### Commit and PR Semantics

- `commit` 是 event 的一等绑定对象，通过 `commit_checkpoint_id -> commit_checkpoint.commit_sha` 解析
- `PR` 是派生字段，不直接存在于 `tool_usage_events`
- `PR` 只在明细中根据 `commit_sha` 反查
- 反查结果可能为：
  - `0 个 PR`
  - `1 个 PR`
  - `多个 PR`

因此：

- 列表不直接展示 `PR`
- 明细展示 `matched PRs`

### Source Semantics

- 列表只显示 `tool + source basename`
- 明细显示完整来源字段：
  - `raw_source_path`
  - `raw_source_locator`

这能同时满足浏览可读性和排障可解释性。

## Page UX

### Summary Section

顶部概览区在当前筛选条件下展示：

1. `Total Events`
2. `Bound to Commit`
3. `Unbound`
4. `Tool Distribution`

第一版不做复杂图表，按轻量 summary cards + 一组 tool counts 即可。

### Event Table

列表默认按 `observed_end_at desc` 排序。

每行建议展示：

1. `observed_at`
2. `tool`
3. `source basename`
4. `repo`
5. `binding status`
6. `commit short sha`
7. `input`
8. `output`
9. `cache`
10. `credits`
11. `requests`
12. `user`（admin only）

说明：

- `source basename` 由 `raw_source_path` 提取 basename；无 path 时回退显示 tool/session 级来源标识
- `binding status` 建议直接使用：
  - `bound`
  - `unbound`

### Filters

第一版筛选项：

1. 时间范围（默认 `24h`）
2. `tool`
3. `repo`
4. `binding status`
5. `user`（admin only）
6. 统一搜索框 `q`

统一搜索框匹配：

1. `tool_session_id`
2. `tool_event_id`
3. `dedupe_key`
4. `commit_sha`
5. `source basename`

### Detail Drawer

点击列表行后打开右侧抽屉。

抽屉内容分区：

1. **Basic**
   - `tool`
   - `workspace_id`
   - `tool_session_id`
   - `tool_event_id`
   - `dedupe_key`
   - `observed_start_at`
   - `observed_end_at`
2. **Usage**
   - `input`
   - `output`
   - `cache`
   - `reasoning`
   - `credits`
   - `requests`
   - `context_usage_pct`
3. **Binding**
   - `commit_checkpoint_id`
   - `commit sha`
   - `checkpoint captured_at`
   - `matched PRs`（best-effort）
4. **Source**
   - `source basename`
   - `raw_source_path`
   - `raw_source_locator`
5. **Raw Payload**
   - admin only
   - JSON viewer，默认折叠

## API Design

当前后端只有写接口：

```text
POST /api/v1/tool-usage-events
```

第一版新增三类只读接口。

### 1. Summary

```text
GET /api/v1/events/summary
```

查询参数：

- `from`
- `to`
- `tool`
- `repo_id`
- `binding_status`
- `user_id`（admin only）
- `q`

返回：

- `total_events`
- `bound_events`
- `unbound_events`
- `tool_counts[]`

### 2. Event List

```text
GET /api/v1/events
```

查询参数：

- `from`
- `to`
- `tool`
- `repo_id`
- `binding_status`
- `user_id`（admin only）
- `q`
- `limit`
- `offset`

返回字段应为轻量列表 DTO，而不是完整 ent entity，避免首屏把 `raw_payload` 一次性拉下。

建议列表 DTO 字段：

- `id`
- `tool`
- `workspace_id`
- `repo_id`
- `repo_name`
- `user_id`
- `username`（admin only）
- `tool_session_id`
- `tool_event_id`
- `dedupe_key`
- `observed_end_at`
- `request_count`
- `input_tokens`
- `output_tokens`
- `cached_input_tokens`
- `reasoning_tokens`
- `credit_usage`
- `commit_checkpoint_id`
- `commit_sha`
- `source_basename`
- `binding_status`

### 3. Event Detail

```text
GET /api/v1/events/:id
```

返回完整详情 DTO。

admin 额外字段：

- `raw_source_path`
- `raw_source_locator`
- `raw_payload`
- `user`

普通用户返回脱敏版本：

- 保留 `source_basename`
- `raw_source_path` 不返回或仅返回脱敏路径
- `raw_payload` 不返回

### Authorization Rules

- 非 admin 查询时，后端必须强制追加 `user_id = 当前用户`
- admin 才允许使用 `user_id` 过滤他人数据
- 事件详情接口同理，必须验证事件属于当前用户或当前用户为 admin

## PR Reverse Lookup

由于 `PR` 不是 event 原生字段，明细接口需要基于 `commit_sha` 反查关联 PR。

第一版策略：

1. 若 event 无 `commit_checkpoint_id` 或无法解析 `commit_sha`，则 `matched_prs = []`
2. 若存在 `commit_sha`，则在 `pr_commit_usage_snapshots` 和/或 PR 当前 commit 集中按 `commit_sha` 反查
3. 返回结构为数组：
   - `pr_record_id`
   - `scm_pr_id`
   - `title`
   - `status`
   - `scm_pr_url`

这里必须明确标记为 `matched_prs`，避免被误读成强唯一归属。

## Frontend Structure

建议新增：

- `frontend/src/views/events/EventsView.vue`
- `frontend/src/api/events.ts`
- `frontend/src/__tests__/events-view.test.ts`

路由新增：

- `path: '/events'`

实现边界：

1. 列表页只拿 summary + list DTO
2. 抽屉打开后按 `selectedEventId` 拉 detail DTO
3. 前端不自己做 `PR` 反查拼接，交给后端 detail API

## Error Handling

### Empty States

- 无事件：显示空表态，不自动报错
- 筛选后无结果：显示 “No events match current filters”

### Detail Loading

- 列表成功但详情失败：保留列表，抽屉显示 detail load error
- 不因为 detail 失败影响整个页面

### Authorization

- 普通用户若通过 URL 参数构造其他用户过滤条件，后端必须忽略或拒绝
- admin 权限变化导致的 `403` 应回退到页面级提示，而不是白屏

## Performance

第一版保护策略：

1. 默认 `24h`
2. 列表分页
3. summary 和 list 分开请求
4. 明细按需加载
5. 列表绝不返回完整 `raw_payload`

后端索引已具备部分基础：

- `workspace_id + observed_end_at`
- `commit_checkpoint_id`
- `tool + tool_session_id`

若第一版后查询仍偏重，再追加：

- `repo_config_id + observed_end_at`
- `user_id + observed_end_at`

这属于实现期优化，不是第一页合同前提。

## Testing Strategy

### Backend

1. 非 admin 只能看到自己的事件
2. admin 可以按 `user_id` 看他人事件
3. `binding_status` 过滤正确
4. `q` 能匹配 `tool_session_id` / `tool_event_id` / `dedupe_key` / `commit_sha` / `source basename`
5. detail 接口对 admin 返回 raw 字段，对普通用户返回脱敏版
6. `matched_prs` 反查在 `0/1/N` 场景下都稳定

### Frontend

1. `/events` 路由可访问
2. 首屏先拉 summary + list
3. 默认时间范围为 `24h`
4. 行点击打开抽屉
5. admin 能看到 raw 区块，普通用户看不到
6. 筛选变化会刷新 summary + list

## Rollout

第一版建议顺序：

1. 后端 summary/list/detail 只读接口
2. 前端 `/events` 页面和抽屉
3. 权限与脱敏校验
4. 再决定是否需要把 repo detail 中的一些调试入口跳转到 `/events`

## Why Not Include Local Pending Data Yet

用户明确要求第一版只做后端已入库事件。

这不是功能缺失，而是有意切边界：

1. 本地 queue / spool 当前存在于 `ae-cli` 所在机器
2. 后端页面无法天然读取它们
3. 如果要纳入 `/events`，必须新增本地状态上报协议或本地页面模式

因此第一版不把“本地未上传状态”混入同一个页面合同，避免把 event 浏览页扩展成 agent 运维面。
