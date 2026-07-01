# 用户用量概览 Dashboard 设计

**日期**: 2026-06-06
**状态**: 已实现；2026-06-06 follow-up 决定前端不再暴露独立 `/user/usage` 页面，dashboard 嵌入首页“我的 AI 使用中心”

## Follow-up

首页 group-level quota 卡片已由 [2026-06-16-ai-usage-center-group-quota-design.md](./2026-06-16-ai-usage-center-group-quota-design.md) 定义。本文中“第一版不展示 platform quota”的非目标仍保留为当时的一版范围说明，不应被理解为禁止后续首页补充 group 级 quota 摘要。

## 背景

用户需要在 ai-efficiency 平台查看自己的 AI 使用情况。当前分支已经尝试新增 `/user/usage` 页面，但实现偏向“单独的趋势页”：前端分别请求 stats、trend、models 三个接口，页面只展示统计卡、手写柱状趋势和模型列表。

这个方向和预期不一致。预期不是复制 sub2api 的完整用户控制台，也不是只补一个趋势图，而是在 ai-efficiency 中提供一个轻量、稳定、面向个人 AI 工作用量的概览页。

sub2api 当前用户端 Dashboard 是数据合同来源，主要上游路径为：

- `sub2api/backend/internal/handler/usage_handler.go`
- `sub2api/frontend/src/api/usage.ts`
- `sub2api/frontend/src/views/user/DashboardView.vue`
- `sub2api/frontend/src/components/user/dashboard/*`

ai-efficiency 只通过 relay HTTP API 使用这些用户端接口，不直连 sub2api 数据库，不使用 sub2api admin API。

## 目标

在 ai-efficiency 前端提供“我的 AI 用量概览”，但不作为侧边栏里的独立新页面。用量 dashboard 嵌入现有首页 `/` 的“我的 AI 使用中心”，点击侧边栏的“我的 AI 使用中心”停留在首页视图中。该区块替换首页原“最近动态 / Recent Activity”位置，不再额外展示最近动态列表。该区块回答三个问题：

- 我今天和累计用了多少请求、token、费用、响应时间？
- 最近一段时间的 token 使用趋势如何？
- 哪些模型贡献了主要请求、token 和费用？

后端提供一个 AE 侧 snapshot endpoint，前端一次请求获得页面所需数据。

## 非目标

第一版不做以下内容：

- 不展示 sub2api balance。
- 不展示 API key 总数、active API key 数。
- 不展示 platform quota。
- 不展示最近使用记录。
- 不提供创建 key、充值、兑换、余额管理等 sub2api 操作入口。
- 不做 partial success。任一上游 dashboard 子接口失败时，snapshot fail-fast。
- 不引入本地缓存或周期同步。每次页面请求实时通过 relay 读取 sub2api 用户端数据。

## 架构

```text
ai-efficiency frontend
  GET /api/v1/user/usage/dashboard?start_date=...&end_date=...&granularity=...&timezone=...
        |
        v
ai-efficiency handler/user_usage.go
  - read AE auth context
  - fetch local user
  - verify relay credentials are configured
        |
        v
ai-efficiency relay.Provider
  GetUserUsageDashboard(ctx, login, password, params)
        |
        v
sub2api relay implementation
  - login once with user credentials
  - call sub2api user dashboard endpoints with the user JWT
        |
        v
sub2api user API
  GET /api/v1/usage/dashboard/stats
  GET /api/v1/usage/dashboard/trend
  GET /api/v1/usage/dashboard/models
```

关键边界：

- relay identity / credential storage 仍归 AE 本地 `users` 表管理。
- usage 数据隔离由 sub2api 用户 JWT 和 sub2api 用户端接口保证。
- AE 后端负责把 sub2api dashboard 合同裁剪成 AE 页面合同。
- AE 前端不直接知道 sub2api login 或 dashboard 子接口细节。

## API 合同

### Request

```http
GET /api/v1/user/usage/dashboard?start_date=2026-05-31&end_date=2026-06-06&granularity=day&timezone=Asia/Shanghai
```

Query 参数：

| 参数 | 含义 | 默认 |
| --- | --- | --- |
| `start_date` | 起始日期，`YYYY-MM-DD` | 后端按当前 timezone 推导近 7 天 |
| `end_date` | 结束日期，`YYYY-MM-DD` | 后端按当前 timezone 推导今天 |
| `granularity` | `day` 或 `hour` | `day` |
| `timezone` | IANA timezone | 空值时让 sub2api 使用自身默认逻辑 |

### Response

```json
{
  "configured": true,
  "range": {
    "start_date": "2026-05-31",
    "end_date": "2026-06-06",
    "granularity": "day",
    "timezone": "Asia/Shanghai"
  },
  "stats": {
    "total_requests": 120,
    "total_input_tokens": 100000,
    "total_output_tokens": 50000,
    "total_cache_creation_tokens": 1000,
    "total_cache_read_tokens": 2000,
    "total_tokens": 153000,
    "total_cost": 12.34,
    "total_actual_cost": 9.87,
    "today_requests": 10,
    "today_input_tokens": 10000,
    "today_output_tokens": 5000,
    "today_cache_creation_tokens": 100,
    "today_cache_read_tokens": 200,
    "today_tokens": 15300,
    "today_cost": 1.23,
    "today_actual_cost": 0.98,
    "average_duration_ms": 860,
    "rpm": 2,
    "tpm": 3000
  },
  "trend": [
    {
      "date": "2026-06-06",
      "requests": 10,
      "input_tokens": 10000,
      "output_tokens": 5000,
      "cache_creation_tokens": 100,
      "cache_read_tokens": 200,
      "total_tokens": 15300,
      "cost": 1.23,
      "actual_cost": 0.98
    }
  ],
  "models": [
    {
      "model": "example-model",
      "requests": 10,
      "input_tokens": 10000,
      "output_tokens": 5000,
      "cache_creation_tokens": 100,
      "cache_read_tokens": 200,
      "total_tokens": 15300,
      "cost": 1.23,
      "actual_cost": 0.98
    }
  ]
}
```

未配置 relay credentials 时返回 200：

```json
{
  "configured": false,
  "range": {
    "start_date": "2026-05-31",
    "end_date": "2026-06-06",
    "granularity": "day",
    "timezone": "Asia/Shanghai"
  },
  "stats": null,
  "trend": [],
  "models": []
}
```

### 字段裁剪

sub2api 的 `UserDashboardStats` 包含 `total_api_keys`、`active_api_keys`、`by_platform`。这些字段不进入 AE snapshot 合同。

原因：

- API key 数量属于 sub2api 账户管理信息，不是 AE 用量概览必需项。
- `by_platform` 和 platform quota 适合 sub2api 平台配额页面，不放入 AE 第一版。
- 保持 AE 页面聚焦用量，避免用户误以为可以在 AE 管理 sub2api 账户资产。

## 后端设计

### relay 类型

在 `backend/internal/relay/types.go` 中定义 AE 侧 dashboard 类型：

- `UserUsageDashboardParams`
- `UserUsageDashboardResponse`
- `UserUsageDashboardRange`
- `UserUsageDashboardStats`
- `UserUsageTrendPoint`
- `UserUsageModelStat`

类型字段以 sub2api 当前用户 dashboard 合同为准，但只保留 AE 需要的用量字段。

### relay Provider

`backend/internal/relay/provider.go` 增加一个高层方法：

```go
GetUserUsageDashboard(ctx context.Context, login, password string, params UserUsageDashboardParams) (*UserUsageDashboardResponse, error)
```

现有分支中的 `GetUserUsageStats`、`GetUserUsageTrend`、`GetUserUsageModels` 不作为外部 Provider 合同保留。sub2api relay 实现内部可以有私有 helper，但 handler 只依赖一个 snapshot 方法。

这样可以保证：

- handler 只解密一次。
- sub2api 用户登录只做一次。
- 上游请求、错误包装、响应裁剪集中在 relay 层。

### sub2api relay 实现

`backend/internal/relay/sub2api.go` 实现流程：

1. 调用现有 `loginSessionToken(ctx, login, password)` 获取用户 JWT。
2. 构造三个用户端 dashboard 请求：
   - `/api/v1/usage/dashboard/stats`
   - `/api/v1/usage/dashboard/trend`
   - `/api/v1/usage/dashboard/models`
3. stats 子接口保持 sub2api dashboard 原合同，不依赖 AE 选择区间；trend/models 透传 `start_date`、`end_date`、`granularity`、`timezone` 中适用参数。
4. 使用同一个 JWT 调用上游接口。
5. 按 sub2api response envelope 解码并裁剪字段。
6. 任一请求失败时返回错误，不返回部分成功数据。

三个上游请求可以串行实现。若后续性能需要，再在 relay 层用 `errgroup` 并发，但第一版不需要为页面打开额外复杂度。

### handler

`backend/internal/handler/user_usage.go` 改为只暴露一个 handler：

```text
GET /api/v1/user/usage/dashboard
```

handler 职责：

- 校验 AE 登录态。
- 读取当前用户。
- 检查 `relay_auth_password` 是否存在。
- 解密 password。
- 解析 query 参数并做轻量合法性校验。
- 解析 primary relay provider。
- 调用 `relayProvider.GetUserUsageDashboard(...)`。
- 把 `relay.ErrInvalidCredentials` 映射为 `409`，提示用户回 `/user` 修复 AI 服务配置。

handler 不负责拼 sub2api URL，也不逐个调用 stats/trend/models。

### 路由

`backend/internal/handler/router.go` 注册：

```go
userUsage := protected.Group("/user/usage")
userUsage.GET("/dashboard", userUsageHandler.Dashboard)
```

旧的 `/user/usage/stats`、`/user/usage/trend`、`/user/usage/models` 在当前分支中还未发布到稳定版本。实现时可以直接移除，避免形成多套合同。

## 前端设计

### 页面承载

用量 dashboard 的可复用实现位于 `frontend/src/components/user/usage/UserUsageDashboard.vue`，由 `frontend/src/views/DashboardView.vue` 嵌入首页 `/`。`frontend/src/views/user/UsageView.vue` 只保留为薄 wrapper 以便组件级测试和短期兼容；router 不注册 `/user/usage`，侧边栏也不提供该入口。

首页承载位置是原 `home.recentActivity` 区块。实现应删除最近动态列表渲染，把 `UserUsageDashboard` 放在该位置，避免页面同时出现“最近动态”和“我的用量”两个使用相关模块。

所有可见文案必须通过 `frontend/src/i18n.ts` 管理，至少覆盖 `en-US` 和 `zh-CN`。用量 dashboard、统计卡、趋势图、模型表、loading/empty/error/setup 状态、按钮和图表 tooltip 不允许继续硬编码单一英文文案。

区块结构：

1. 顶部工具条
   - 标题：通过 `usageDashboard.title` / `usageDashboard.embeddedTitle` 本地化
   - 时间范围：Today / 7 Days / 30 Days 与对应中文
   - Refresh / 刷新
   - granularity 默认 `day`；Today 使用 `hour`

2. 统计卡
   - Range Cost：按当前选择的 Today / 7 Days / 30 Days 从 trend 汇总，实际扣费为主，标准计费为次要值
   - Range Requests：按当前选择区间从 trend 汇总
   - Range Tokens：按当前选择区间从 trend 汇总，并附 input / output / cache breakdown
   - Avg Response：保留 dashboard stats 中的 average duration / RPM / TPM

stats 合同仍保留 sub2api 的 `today_*` 与 `total_*` 字段，但 AE 页面统计卡主值不得固定读取 `today_*`。当用户切换 7 天或 30 天时，卡片应使用当前 snapshot 的 `trend` 数据重新汇总，避免出现“今日费用”等固定日口径文案或数值。

3. 图表区
   - 左侧：token trend line chart
   - 右侧：model distribution，以表格为主，可保留 doughnut 作为辅助

### 组件

建议保留独立组件，但按 AE 合同重写：

- `UsageStatsCards.vue`
- `UsageTrendChart.vue`
- `UsageModelChart.vue`

`UsageTrendChart.vue` 不再使用手写柱状图。ai-efficiency 当前没有图表库，第一版新增 `chart.js` 和 `vue-chartjs`，与 sub2api Dashboard 的图表栈保持一致，展示：

- input tokens
- output tokens
- cache creation tokens
- cache read tokens
- tooltip 中展示 standard cost / actual cost

`UsageModelChart.vue` 展示：

- model
- requests
- total tokens
- actual cost
- standard cost

### API 层

`frontend/src/api/userUsage.ts` 改为：

```ts
getUserUsageDashboard(params): Promise<ApiResponse<UserUsageDashboardSnapshot>>
```

删除当前三接口 API 封装，前端只消费 snapshot。

### 状态处理

页面状态：

- `loading`
- `snapshot`
- `error`
- `configured`

空态：

- `configured=false`：显示需要完成 AI 服务配置，并链接到 `/user`。
- `stats=null` 或全部为 0：统计卡展示 0，图表显示 empty state。
- snapshot 请求失败：显示整体错误，不展示过期数据。

前端不再通过 `authStore.user?.relay_auth_password` 判断是否配置。是否配置由后端 snapshot response 决定。

## 错误语义

| 场景 | 状态码 | 行为 |
| --- | --- | --- |
| AE 未登录 | `401` | 由现有 auth middleware 处理 |
| relay credentials 未配置 | `200` | `configured=false` |
| 本地用户读取失败 | `422` | 返回错误 |
| relay 密码解密失败 | `422` | 返回错误 |
| primary provider 不存在或无法解析 | `422` | 返回错误 |
| sub2api 用户凭证无效 | `409` | 前端提示回 `/user` 修复配置 |
| sub2api dashboard 子接口失败 | `502` | fail-fast，前端显示整体错误 |

第一版不返回 `partial_errors`，避免页面展示半可信数据。

## 测试计划

### Backend

运行：

```bash
cd backend && go test ./...
```

重点覆盖：

- relay 只登录 sub2api 一次。
- stats/trend/models 请求都带同一个 user JWT。
- trend/models query 参数正确。
- stats 裁剪掉 `total_api_keys`、`active_api_keys`、`by_platform`。
- cache token、rpm、tpm、model cost 字段正确解码。
- handler 未配置 credentials 返回 `configured=false`。
- handler 解密失败返回 `422`。
- provider resolve 失败返回 `422`。
- `relay.ErrInvalidCredentials` 映射为 `409`。
- 成功时返回单一 snapshot。

### Frontend

运行：

```bash
cd frontend && pnpm test
```

重点覆盖：

- 配置缺失空态。
- 成功渲染四张统计卡。
- Today / 7 Days / 30 Days 切换会重发 snapshot 请求。
- Today 使用 hour granularity，其他范围使用 day granularity。
- trend 为空时显示 empty state。
- models 为空时显示 empty state。
- `409` 时提示用户回 `/user` 修复配置。

### Manual smoke

如果图表组件或 chart 依赖发生明显变更，再启动本地前端跑一次浏览器 smoke，确认：

- 页面加载非空。
- 日期范围切换不报错。
- 图表容器在桌面和移动宽度下不溢出。

## 迁移与清理

当前分支已有旧实现，需要在实现阶段做窄范围替换：

- 重写 `docs/superpowers/plans/2026-06-06-user-usage-trend.md`，不要继续执行旧 plan。
- 删除或替换旧的三接口后端 handler。
- 删除或替换旧的三接口前端 API。
- 抽出 `UserUsageDashboard.vue` 和三个 usage 子组件。
- 不保留 `/user/usage` 前端页面路由和侧边栏入口；用量 dashboard 嵌入首页 `/`。

## 文档影响

这次是功能设计和当前分支合同修正，不改变项目级部署架构。

如果实现后 `docs/architecture.md` 已提到用户用量页面或 relay dashboard 合同，应同步更新为单 snapshot endpoint；否则无需修改项目级架构图。
