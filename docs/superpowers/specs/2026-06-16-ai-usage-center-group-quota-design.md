# AI 使用中心 Group Quota 首页扩展设计

**日期:** 2026-06-16
**状态:** Approved design for next implementation
**范围:** `backend/internal/relay/`, `backend/internal/handler/user_usage.go`, `frontend/src/components/user/usage/`, `frontend/src/views/DashboardView.vue`, `frontend/src/types/index.ts`, `frontend/src/i18n.ts`, `frontend/src/__tests__/dashboard-view.test.ts`, `backend/internal/relay/sub2api_test.go`, `docs/architecture.md`
**相关文档:**
- [2026-06-06-user-usage-trend-design.md](./2026-06-06-user-usage-trend-design.md)
- [2026-06-14-user-api-key-first-onboarding-design.md](./2026-06-14-user-api-key-first-onboarding-design.md)
- [2026-06-15-ai-usage-center-lifecycle-home-design.md](./2026-06-15-ai-usage-center-lifecycle-home-design.md)
- [docs/architecture.md](../../architecture.md)

## Spec Relationship

- [2026-06-06-user-usage-trend-design.md](./2026-06-06-user-usage-trend-design.md) 定义了首页嵌入式 usage dashboard 的第一版合同，并明确第一版不展示 `platform quota`。
- 本文不回写该历史 spec 的正文结论，而是在其基础上新增一个 follow-up 合同：首页现在需要展示 **group 级** quota 摘要，用于回答“我在当前主 provider 下可用的各个 group 已使用多少、配额是多少、是否无限”。
- 本文继承 [2026-06-15-ai-usage-center-lifecycle-home-design.md](./2026-06-15-ai-usage-center-lifecycle-home-design.md) 中“首页以 `UserUsageDashboard` 为老用户主区”的结论；group quota 区块插入该 dashboard 顶部，不改变生命周期分流规则。
- 本文不改变 `/user` 页面现有的 provider/group/API key 自助配置合同。[2026-06-14-user-api-key-first-onboarding-design.md](./2026-06-14-user-api-key-first-onboarding-design.md) 仍然是 `/user` 的当前主设计。
- 本文允许 AE 侧扩展 relay adapter 以读取上游 group quota 数据，但不引入 direct DB coupling，也不修改 `sub2api` 仓库源码。

## Problem

当前首页里的 `UserUsageDashboard` 只能回答用户总览问题：

1. 我最近一段时间总共发起了多少请求、消耗了多少 token、花了多少费用。
2. 趋势如何。
3. 哪些模型贡献了主要用量。

它回答不了用户在实际使用 AI relay 时更直接的问题：

1. 我当前可用的各个接入组分别用了多少。
2. 哪些组是有限 quota，哪些组是无限。
3. 哪些组已经有可复用 API key、因此真正可以在本机工具里直接使用。

同时，当前 AE 首页既没有 group quota 合同，也没有把 quota 的失败和 usage dashboard 的失败分开处理：

1. 现有 snapshot 只包含 `stats`、`trend`、`models`。
2. `/user/providers` 只负责接入配置摘要，不适合承载实时 quota。
3. 一旦未来 quota 数据源不可用，如果把它混入现有 usage snapshot 的 fail-fast 逻辑，会让首页整体降级过于激进。

结果是：老用户首页虽然能看“总用量”，但看不到“我有哪些可用 group、每个 group 目前用了多少、是不是无限”，这个信息缺口正好落在首页最应该回答的工作流问题上。

## Goals

1. 在首页 `UserUsageDashboard` 顶部新增 `Group Quotas` 区块。
2. 只展示 **主 provider** 下、且 **已有可用 API key** 的 group。
3. 对每个 group 展示：
   - 已使用金额
   - 配额金额
   - 是否无限
   - 平台与 group 名称
4. 有限 quota 文案统一为 `已使用 / 配额`。
5. 无限 quota 文案统一为 `已使用 / ∞`，使用数学符号，不使用 emoji。
6. relay 返回单位时透传单位；relay 拿不到单位时，默认按美元显示。
7. quota 数据失败时，不影响现有 usage stats/trend/models 渲染：首页只把 quota 区块标记为暂时不可用。
8. 顺手清理已确定废弃的旧首页文案和独立 `/user/usage` 包装页残留。

## Non-Goals

1. 不在首页展示 `platform quota` 聚合视图。
2. 不在首页展示按 provider 的汇总余额或所有 provider 的 group quota。
3. 不在 `/user` 页面增加 quota 展示。
4. 不在本轮改变 `getUserProviders()` 的职责边界，不把实时 quota 塞回 provider/group 配置摘要接口。
5. 不在首页新增“充值”“购买”“管理订阅”等 sub2api 资产操作入口。
6. 不改变当前 usage dashboard 的 range 切换、趋势图和模型分布合同。
7. 不修改 `sub2api` 仓库源码；若当前上游合同没有现成 group quota 字段，AE 只能通过 relay adapter 扩展已存在的上游能力或等待上游提供相应接口。

## User Decisions Captured

本轮设计已确认以下用户决策：

1. 显示的是 **当前用户在 group 维度的余额/配额语义**，最终展示文案采用“已使用 / 配额”。
2. 单位以 relay 返回为准；如果 relay 拿不到单位，AE 默认按美元显示。
3. quota 区块放在首页现有 usage dashboard 内部，位于统计卡片和图表之前。
4. 只显示主 provider 下、且 `credential.state === existing_hidden` 的 group。
5. 有 quota 但拿不到已使用数值时，显示 `-- / 配额`。
6. 无限 quota 显示为 `已使用 / ∞`。
7. finite quota 和 infinite quota 的主文案分别为：
   - 有限：`已使用 / 配额`
   - 无限：`已使用 / ∞`
8. quota 数据源失败或当前 relay 版本不支持时：首页应继续展示现有 usage dashboard，只在 quota 区块显示“暂时不可用”。

## Reviewed Alternatives

### Option A: 扩展现有 `/user/usage/dashboard` snapshot

- 在现有 snapshot 响应里新增 `group_quotas` section。
- 首页继续一次请求拿齐 usage 和 quota 数据。

优点：

- 首页仍然保持单一 snapshot 合同，最符合“AI 使用中心”作为首页聚合视图的定位。
- `/user` 页面接口职责不被污染。
- 可以把 quota 的状态单独编码进 snapshot，而不是让前端额外管理一个并行请求。

缺点：

- AE 后端和 relay adapter 需要扩展新字段与新容错逻辑。

### Option B: 扩展 `/user/providers`

- 在 provider/group 摘要里直接加 quota 字段。

优点：

- group 元信息和 quota 在同一接口里。

缺点：

- `/user/providers` 当前是接入配置摘要，不是实时余额/配额接口。
- 会把 `/user` 页请求变重，并混淆“配置”和“实时资产状态”的边界。

### Option C: 新增独立 `/user/usage/group-quotas`

- 首页同时请求 usage snapshot 和 group quota 独立接口。

优点：

- quota 失败与 usage snapshot 完全解耦。

缺点：

- 前端状态分裂，首页加载流程更复杂。
- 对用户并没有比单一 snapshot 更强的感知收益。

### Decision

采用 **Option A**：扩展现有 `/api/v1/user/usage/dashboard` snapshot，在其中新增 `group_quotas` section。

## Architecture

```text
frontend DashboardView
  |
  v
UserUsageDashboard (embedded)
  GET /api/v1/user/usage/dashboard
      |
      v
AE user_usage handler
  - resolve AE user + relay credentials
  - resolve primary relay provider
  - fetch usage snapshot via relay.Provider
  - fetch eligible groups from primary provider summary
  - fetch quota snapshot via relay adapter extension
  - merge into one homepage response
      |
      v
relay adapter (sub2api)
  - user dashboard stats/trend/models
  - optional group quota source for primary provider groups
```

关键边界：

1. 首页 quota 仍然属于 AE 首页 snapshot 合同的一部分，不属于 `/user` 配置合同。
2. `sub2api` 集成仍然只通过 `backend/internal/relay.Provider` 及其可选扩展接口完成。
3. AE 不直接读取 relay 数据库，不把 quota 逻辑塞进 frontend 自己拼装。
4. usage stats/trend/models 仍保持 2026-06-06 spec 的 fail-fast 逻辑；但 `group_quotas` section 允许独立降级。

## Backend Contract

### Existing endpoint

```text
GET /api/v1/user/usage/dashboard?start_date=...&end_date=...&granularity=...&timezone=...
```

### Response extension

在现有响应中新增：

```json
{
  "configured": true,
  "range": {
    "start_date": "2026-06-01",
    "end_date": "2026-06-06",
    "granularity": "day",
    "timezone": "Asia/Shanghai"
  },
  "stats": {},
  "trend": [],
  "models": [],
  "group_quotas": {
    "status": "ok",
    "unit_label": "USD",
    "message": "",
    "groups": [
      {
        "group_id": "42",
        "group_name": "Group Alpha",
        "platform": "openai",
        "used_amount": 32.4,
        "quota_amount": 100,
        "is_unlimited": false
      },
      {
        "group_id": "43",
        "group_name": "Group Beta",
        "platform": "anthropic",
        "used_amount": 18.2,
        "quota_amount": null,
        "is_unlimited": true
      }
    ]
  }
}
```

建议类型：

- `group_quotas.status`: `ok | empty | unavailable`
- `group_quotas.unit_label`: 例如 `USD`
- `group_quotas.message`: 仅在 `unavailable` 时可选，用于人类可读提示
- `group_quotas.groups[]`:
  - `group_id`
  - `group_name`
  - `platform`
  - `used_amount`，允许为 `null`
  - `quota_amount`，允许为 `null`
  - `is_unlimited`

### Status semantics

#### `status = "ok"`

条件：

- 上游 quota 数据源成功返回
- 经过过滤后仍有 1 个或以上可展示 group

前端行为：

- 显示 `Group Quotas` 区块和各 group 卡片

#### `status = "empty"`

条件：

- 主 provider 下不存在 `credential.state === existing_hidden` 的 group
- 或 quota 数据成功返回，但过滤后没有任何符合条件的 group

前端行为：

- 整个 quota 区块隐藏，不显示空占位

#### `status = "unavailable"`

条件：

- 当前 relay adapter 不支持 group quota
- quota 上游超时、失败或返回不可解析数据

前端行为：

- quota 区块保留标题，但主体显示“暂时不可用”
- `stats`、`trend`、`models` 照常显示

### Unit behavior

1. relay 返回单位时透传到 `unit_label`。
2. relay 未返回单位时，AE 后端默认写入 `USD`。
3. 前端优先根据 `unit_label` 格式化为货币显示；当前设计只要求支持美元默认显示。

## Relay Adapter Design

### Provider interface

现有 `relay.Provider` 保持不变，以避免强迫所有 provider 同步实现 quota 能力。

新增一个可选扩展接口，例如：

```go
type UserGroupQuotaProvider interface {
    GetUserGroupQuotas(ctx context.Context, login, password string, req UserGroupQuotaRequest) (*UserGroupQuotaResponse, error)
}
```

说明：

1. `user_usage.go` 先通过 `relay.Provider` 取 usage snapshot。
2. 然后检查 provider 是否实现 `UserGroupQuotaProvider`。
3. 未实现时，不报 handler 错，而是返回 `group_quotas.status = "unavailable"`。

### Request shape

`UserGroupQuotaRequest` 建议包含：

- `GroupIDs []string`
- `Timezone string`

原因：

1. 首页只需要查询可展示 group，不需要 relay 返回用户全部 group。
2. 通过 AE 侧先过滤出主 provider + existing key group，再把 group ids 传给 relay，避免上游多余开销。

### Response shape

`UserGroupQuotaResponse` 建议包含：

- `UnitLabel string`
- `Groups []UserGroupQuotaItem`

其中 `UserGroupQuotaItem` 至少包含：

- `GroupID string`
- `UsedAmount *float64`
- `QuotaAmount *float64`
- `IsUnlimited bool`

若上游只能返回数值而不返回单位，则 adapter 让 `UnitLabel` 为空，由 handler 兜底写成 `USD`。

## Handler Merge Rules

`backend/internal/handler/user_usage.go` 需要从“只组装 usage snapshot”扩成“组装 usage + group quota”。

推荐步骤：

1. 校验 AE 登录态。
2. 读取本地用户。
3. 检查并解密 relay credentials。
4. 解析 query 参数。
5. 解析 primary relay provider。
6. 调用 `GetUserUsageDashboard(...)` 获取 `stats/trend/models`。
7. 调用当前 `/user` 侧 group 摘要逻辑或等价 service，找出：
   - `is_primary === true` 的 provider
   - 其中 `credential.state === existing_hidden` 的 group
8. 若没有 eligible groups，返回 `group_quotas.status = "empty"`。
9. 若 provider 未实现 quota 扩展接口，返回 `group_quotas.status = "unavailable"`。
10. 若 quota 查询失败，记录 warning log，返回 `group_quotas.status = "unavailable"`。
11. 若 quota 查询成功，按 group id merge 为首页展示结构。

这个流程要求：

1. usage dashboard 主体仍然是关键路径。
2. quota 是可降级 section，不得让其失败覆盖整个 usage snapshot 成功结果。

## Frontend Design

### Placement

quota 区块放在 `UserUsageDashboard` 内部，位置顺序为：

1. range selector / refresh
2. `Group Quotas`
3. `UsageStatsCards`
4. `UsageTrendChart`
5. `UsageModelChart`

不放在 `DashboardView` guide 卡片之上，也不放在 `/user` 页面。

### Component boundary

建议新增独立组件：

```text
frontend/src/components/user/usage/UsageGroupQuotaSection.vue
```

职责：

1. 接收 `groupQuotas` 数据结构
2. 渲染 quota 卡片
3. 处理 `empty` / `unavailable` 两种独立状态

`UserUsageDashboard.vue` 只负责：

1. 加载 snapshot
2. 把 `group_quotas` 传给 quota section
3. 决定 section 显示顺序

### Card rendering rules

每张卡片展示：

1. 顶部小标签：`platform`
2. 主标题：`group_name`
3. 主文案固定为：
   - 有限 quota：`已使用 / 配额`
   - 无限 quota：`已使用 / ∞`
4. 主数值：
   - 有限 quota：`$32.40 / $100.00`
   - 无限 quota：`$18.20 / ∞`
   - 已使用缺失但 quota 存在：`-- / $100.00`

### Section visibility rules

1. `status = ok` 且 `groups.length > 0`：显示 quota 区块。
2. `status = empty`：整个区块隐藏。
3. `status = unavailable`：显示轻量提示卡，例如“Group Quotas 暂时不可用”。
4. 若整页 `configured === false` 或 usage dashboard 主请求进入现有 error state，则沿用当前整页空态/错误态，不单独展示 quota 区块。

### Formatting

1. 使用数学符号 `∞`，不使用 emoji。
2. 默认美元显示：
   - 若 `unit_label == "USD"` 或为空，经后端兜底后前端显示为 `$`
3. 当前设计不要求首页同时支持多种货币混排；若后续上游允许不同 group 返回不同单位，应另起 spec 处理。

## Cleanup Scope

本轮实现允许顺手清理以下已确认废弃内容：

### 1. 独立 `/user/usage` 包装页残留

当前 router 已明确不暴露 `/user/usage`：

- `frontend/src/router/index.ts`
- `frontend/src/__tests__/router.test.ts`

因此可以删除：

- `frontend/src/views/user/UsageView.vue`
- `frontend/src/__tests__/user-usage-view.test.ts`

说明：

- `UserUsageDashboard` 组件本体不能删除，因为首页仍直接使用它。
- `frontend/src/api/userUsage.ts` 与 `frontend/src/__tests__/user-usage-api.test.ts` 保留，因为首页仍依赖该 API。

### 2. 未引用的旧首页文案和导航 key

可以删除：

- `nav.myUsageTrend`
- 旧首页未再使用的一组 `home.*` key，例如：
  - `home.personalStatus`
  - `home.thisWeek`
  - `home.recentActivity`
  - `home.metricRepos*`
  - `home.metricWorkflows*`
  - `home.metricAiPrs*`
  - `home.metricTools*`
  - `home.next*`
  - `home.status*`

要求：

1. 只删除当前代码已无引用的 key。
2. 删除前应先搜索确认，避免误删仍由测试或隐藏视图使用的文案。

## Testing

### Backend relay tests

在 `backend/internal/relay/sub2api_test.go` 增加：

1. 有限 quota 解析测试
2. 无限 quota 解析测试
3. 上游缺失单位时 handler/adapter 默认 `USD` 的测试
4. quota 上游失败时返回可降级结果而不是污染 usage snapshot 主错误流

### Backend handler tests

在 `backend/internal/handler/` 增加或扩展 `user_usage` 测试：

1. usage snapshot 成功 + group quota 成功
2. usage snapshot 成功 + group quota `empty`
3. usage snapshot 成功 + provider 不支持 quota
4. usage snapshot 成功 + quota 请求失败

关键断言：

1. `stats/trend/models` 仍成功返回
2. `group_quotas.status` 正确编码
3. handler 返回 200，而不是整体 5xx

### Frontend tests

在 `frontend/src/__tests__/dashboard-view.test.ts` 增加：

1. `group_quotas.ok` 时渲染 quota 卡片
2. `group_quotas.empty` 时不显示区块
3. `group_quotas.unavailable` 时显示“暂时不可用”，但仍显示 usage stats/trend/models
4. 无限 quota 文案使用 `∞`
5. 金额格式在单位缺失场景下仍按美元显示

### Regression tests for cleanup

1. router 测试继续断言 `/user/usage` 不存在
2. sidebar 测试继续断言没有独立 `/user/usage` 导航入口
3. 若删除 i18n key，需要保证现有 dashboard/home 测试仍通过

## Documentation Updates

实现完成后需要同步更新：

1. `docs/architecture.md`
   - 首页 snapshot 现在由 usage dashboard 主体 + 可降级 group quota section 组成
2. [2026-06-06-user-usage-trend-design.md](./2026-06-06-user-usage-trend-design.md)
   - 在后续记录或相关链接中说明 group quota follow-up 已由本文定义

不要求：

1. 重写 2026-06-06 历史 spec 的第一版非目标段落
2. 回写 `/user` onboarding spec

## Implementation Notes

1. 若当前 sub2api 只有 `by_platform`，没有现成 group 级 quota 数据源，则本设计只定义 AE 侧合同与降级行为；具体上游能力接入方式在实现 plan 中细化。
2. 若上游 quota 能力需要额外登录、额外用户 token 或不同 endpoint，仍应封装在 relay adapter 内，不得泄漏到 handler 或 frontend。
3. 任何“货币换算”“多币种混排”“手动切换单位”都不在本轮范围内。
