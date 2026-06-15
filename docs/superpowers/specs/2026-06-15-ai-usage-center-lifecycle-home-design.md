# AI 使用中心生命周期分流首页设计

**日期:** 2026-06-15
**状态:** Approved design for next implementation
**范围:** `frontend/src/views/DashboardView.vue`, `frontend/src/components/user/usage/UserUsageDashboard.vue`, `frontend/src/i18n.ts`, `frontend/src/__tests__/dashboard-view.test.ts`, `frontend/src/__tests__/user-usage-view.test.ts`, `docs/architecture.md`
**相关文档:**
- [2026-05-29-company-wide-user-home-ux-design.md](./2026-05-29-company-wide-user-home-ux-design.md)
- [2026-06-06-user-usage-trend-design.md](./2026-06-06-user-usage-trend-design.md)
- [2026-06-14-user-api-key-first-onboarding-design.md](./2026-06-14-user-api-key-first-onboarding-design.md)
- [docs/architecture.md](../../architecture.md)

## Spec Relationship

- 本文定义 `/` 当前生效的首页合同：`AI Usage Center` / `AI 使用中心` 不再对所有用户展示同一套“平台概览”，而是按用户生命周期在“完成 AI 接入”和“查看我的用量”之间分流。
- 本文继承 [2026-05-29-company-wide-user-home-ux-design.md](./2026-05-29-company-wide-user-home-ux-design.md) 中“登录后默认进入个人首页”的方向，但替换其中“先回答我是谁、接入是否正常、本周有什么结果、下一步做什么”的统一首页叙事。新的首页优先级改为：
  - 新用户：先完成 `AI 接入与配置`
  - 老用户：先看“我的用量”
- 本文继承 [2026-06-06-user-usage-trend-design.md](./2026-06-06-user-usage-trend-design.md) 中“用量 dashboard 嵌入首页”的结论，但收紧首页用量区的呈现目标：首页主区不再以费用作为第一视角内容。
- 本文依赖 [2026-06-14-user-api-key-first-onboarding-design.md](./2026-06-14-user-api-key-first-onboarding-design.md) 中 `/user` 页面当前合同；首页的新手引导卡片只负责把用户带到 `/user`，不复制 `/user` 的详细操作。
- 本文不改变 `getUserProviders`、`getUserUsageDashboard`、`getDashboard`、`listEvents`、`/user`、relay provider、group credential、usage snapshot、tool usage event 的后端合同。
- 本文不回写历史 spec 正文。实现完成后，应更新 `docs/architecture.md` 反映新的首页状态分流和模块边界。

## Problem

当前首页 `DashboardView.vue` 虽然已经嵌入了个人用量 dashboard，但主叙事仍然停留在统一的“平台概览页”：

1. 首页主区仍由 `Personal Status`、`Platform Signals`、`Setup Status`、`Next Steps` 组成，所有用户无论成熟度如何都先看到同一套内容。
2. `Tracked Workflows`、`AI PRs`、`Code reporting` 这类平台或研发视角指标对普通用户并不是首要问题。
3. 新用户进入首页时，真正需要的是“我现在该怎么完成 AI 接入”；当前首页没有把这件事做成唯一主目标。
4. 老用户进入首页时，真正需要的是“我最近用了多少、趋势怎样、是否异常”；当前首页让用量区和平台信号并列，主次不清。
5. 当前首页为了判断 `Recent usage` 仍额外依赖 `listEvents`，但“是否有最近记录”并不是首页生命周期分流的最准确信号。

结果是：首页同时承担平台摘要、接入提示、数据存在性判断、下一步入口和个人用量概览，既不聚焦新用户，也不聚焦老用户。

## Goals

1. 把首页改成**生命周期分流首页**：
   - 新用户：首页主角是“新手引导卡片”
   - 老用户：首页主角是“我的用量”
2. 明确定义“新用户 / 老用户”的判断规则，并避免把接口失败误判成“新用户”。
3. 新用户首页默认展开引导卡片，把用户直接带到 `/user` 的 `AI 接入与配置`。
4. 老用户首页默认收起引导卡片，让 `UserUsageDashboard` 成为首页主角。
5. 老用户首页里的用量区尽量保持现状，但首页首屏不展示费用相关摘要。
6. 移除当前首页里不符合普通用户第一视角的主区块地位：`Platform Signals`、`Setup Status`、`Next Steps` 不能再以当前形态占据首页主区。
7. 保留 `Usage Records` / `使用记录` 与 `Code Repositories` / `代码仓库` 作为辅助入口，而不是首页主视觉。

## Non-Goals

1. 本轮不修改 `/user` 的信息架构、命令语义、provider/group 选择、API key、连接测试或配置方式合同。
2. 本轮不修改 AE 后端、relay provider 或 sub2api usage snapshot 合同。
3. 本轮不新增新的首页路由，不引入“首次进入 wizard route”。
4. 本轮不要求把研发或平台摘要完全删除；如果业务上仍需保留，可以降级到老用户首页底部的轻量辅助区。
5. 本轮不改变独立用量页 wrapper 的存在方式，也不新增新的用量 API。
6. 本轮不要求在首页展示完整 CLI 教程、事件明细或平台排障细节。

## Reviewed Alternatives

### Option A: 双态首页

- 新用户：首页主区展开引导卡片。
- 老用户：首页主区展示“我的用量”，引导卡片默认收起。

优点：

- 最符合“新用户先接入、老用户先看用量”的目标。
- 首页主次清晰，能显著降低普通用户的理解成本。

缺点：

- 需要重排当前首页信息架构，而不是只换文案。
- 测试需要覆盖两个首页主态以及异常态。

### Option B: 固定骨架，只切第一块

- 首页整体布局保留，只把第一块按用户阶段切换成引导卡片或用量摘要。

优点：

- 实现成本最低。

缺点：

- 旧的“平台概览页”心智仍然很强，改善有限。

### Option C: 双入口导航首页

- 首页同时给出“去 AI 接入”和“看我的用量”两个大入口，再让用户选择。

优点：

- 结构清楚。

缺点：

- 首页会退化成目录页，而不是真正的个人首页。
- 与“系统按用户生命周期做默认分流”的目标不一致。

### Decision

采用 **Option A: 双态首页**。

## Lifecycle State Model

首页生命周期分流不再依赖旧的 `Recent usage` 或 `Setup Status` 文案块，而是依赖两个核心判断：

1. `AI 接入是否可用`
2. `是否已有有效用量数据`

### State Inputs

#### 1. `aiAccessReady`

来源：`getUserProviders()`

判定规则：

- 当前用户至少存在一个 `group`
- 且该 group 的 `credential.state === 'existing_hidden'`

这个判断沿用当前 `/user` 页面与首页已存在的“可用 AI 访问”事实链，不新增后端字段。

#### 2. `usageDataReady`

来源：`getUserUsageDashboard(...)`

判定规则：

- snapshot 请求成功
- 且 `configured === true`
- 且满足以下任一条件：
  - `stats.total_requests > 0`
  - `stats.total_tokens > 0`
  - `trend` 中存在任何 `requests > 0` 或 `total_tokens > 0` 的点
  - `models` 中存在任何 `requests > 0` 或 `total_tokens > 0` 的模型

说明：

- 不能仅以 `configured === true` 判断老用户，因为“已配置但从未产生过有效使用”仍应被视为新用户首页。
- 不能仅以 `listEvents()` 判断是否是老用户，因为首页主目标是“可理解的用量状态”，而不是“是否已有原始事件页数据”。

### Lifecycle States

#### State 1: `needs_setup`

条件：

- `aiAccessReady === false`

首页行为：

- 首页进入“新用户首页”
- 新手引导卡片默认展开
- 主标题为 `先完成 AI 接入`
- 主 CTA 指向 `/user`
- 用量区保留，但以空状态或弱化状态展示，不抢主视觉

#### State 2: `setup_ready_waiting_for_first_usage`

条件：

- `aiAccessReady === true`
- `usageDataReady === false`

首页行为：

- 仍进入“新用户首页”
- 新手引导卡片默认展开
- 主标题改为 `AI 已接入，开始第一次使用`
- 主 CTA 仍优先指向 `/user`
- 用量区保留空状态，解释“已接入，但还没有首次有效用量数据”

#### State 3: `established_user`

条件：

- `aiAccessReady === true`
- `usageDataReady === true`

首页行为：

- 首页进入“老用户首页”
- 新手引导卡片默认收起
- `UserUsageDashboard` 成为首页主角
- 费用相关摘要不在首页首屏展示

#### State 4: `degraded_error`

条件：

- `getUserProviders()` 或 `getUserUsageDashboard()` 关键请求失败

首页行为：

- 首屏先展示明确异常提示
- 不把用户误判成 `needs_setup`
- 在异常提示下方保留当前可安全展示的内容

说明：

- `getDashboard()` 失败不影响用户生命周期主态；它只影响辅助信号或底部摘要。
- `listEvents()` 失败不影响用户生命周期主态；首页不再依赖 events 成为主判断。

## Page Layout Contract

首页从上到下只允许有一个“主角模块”。

### 新用户首页

顺序：

1. 展开的新手引导卡片
2. 轻量辅助入口区
3. 弱化的用量区空状态或当前用量区
4. 辅助入口：`Usage Records`、`Code Repositories`

当前 `Platform Signals`、`Setup Status`、`Next Steps` 不再保留为三个并列大区块。它们的职责分别重组为：

- `Setup Status` -> 并入新手引导卡片
- `Next Steps` -> 改成轻量辅助入口
- `Platform Signals` -> 从首页主区移除；如保留，只能作为底部轻量摘要

### 老用户首页

顺序：

1. 收起的新手引导卡片摘要条
2. `UserUsageDashboard` 主区
3. 辅助入口区：`Usage Records`、`Code Repositories`
4. 可选的底部轻量平台摘要

### Hero Replacement

当前首页顶部 `Personal Status` hero 不再单独作为大标题卡片存在。首页顶部主视觉由生命周期主卡片承担：

- 新用户：展开的引导卡片承担 hero 职责
- 老用户：收起摘要条承担顶层状态提示

用户账号信息可以保留，但应降级为次级信息，不得继续与主目标竞争视觉层级。

## Guide Card Contract

新手引导卡片是首页唯一的“引导型主模块”，其职责只有两件事：

1. 说明当前用户所处阶段
2. 把用户带到 `/user`

### Default Expansion

- 新用户：默认展开
- 老用户：默认收起

用户可手动展开或收起，但第一版不要求持久化用户偏好；默认状态始终由生命周期判断决定。

### Titles

- `needs_setup` -> `先完成 AI 接入`
- `setup_ready_waiting_for_first_usage` -> `AI 已接入，开始第一次使用`
- `established_user` 收起态 -> `AI 接入已完成`

### Content Structure

卡片内部不复制 `/user` 的完整步骤详情，只显示摘要：

1. 当前阶段说明
2. 最多 3 条状态信号
3. 一个主 CTA

### Status Signals

卡片内的 3 条状态信号固定为：

1. `AI 接入`
   - 是否已有至少一个可用接入组和可用 key
2. `代码关联`
   - 是否已有仓库 / 上报链路开始工作
   - 可复用当前 `codeReportingActive` 的事实来源
3. `最近使用`
   - 是否已有有效用量数据

每条只显示用户可理解的状态值，例如：

- `已完成`
- `待完成`
- `等待首次使用`
- `未知`

不能使用只有颜色没有文案的状态表示。

### CTA

主 CTA 固定指向 `/user`，文案按状态变化：

- `前往 AI 接入与配置`
- `继续 AI 接入`
- `检查 AI 接入配置`

第一版不建议在这张卡片里放多个并列主按钮。

## Established User Usage Contract

老用户首页里的用量主区以当前 `UserUsageDashboard` 为基础，目标是“尽量保持现状，但首页首屏去掉费用”。

### Keep

保留以下能力：

- 时间范围切换
- 请求数
- Token
- 平均响应
- 趋势
- 模型分布
- 刷新

### Remove From Home First View

首页首屏不再展示：

- `today_cost`
- `total_cost`
- `actual_cost`
- 任何“费用”作为第一排核心摘要卡

说明：

- 这只是首页嵌入态的呈现约束，不强制改变所有复用场景的底层 snapshot 合同。
- 第一版可以通过 `UserUsageDashboard` 的嵌入态分支、prop、或首页专用显示模式来隐藏费用摘要，而不是修改后端返回字段。

### Auxiliary Actions

老用户首页保留两个辅助入口：

- `查看使用记录`
- `查看代码仓库`

它们是继续深挖的入口，不是首页主视觉。

## Data Flow

首页生命周期判断最少依赖两个数据源：

1. `getUserProviders()`
2. `getUserUsageDashboard(...)`

推荐实现方式：

- `DashboardView.vue` 在首页级别拿到这两个结果
- 首页先完成生命周期判断
- 再把需要的 snapshot 或显示模式传给 `UserUsageDashboard`

原因：

- 首页主态已经依赖用量 snapshot，不能继续让 `UserUsageDashboard` 完全自管而首页毫不知情
- 这样可以避免首页先按一种状态渲染，再被嵌入用量区的异步状态推翻

`getDashboard()` 只作为辅助数据源：

- 为 `代码关联` 等次级状态提供事实来源
- 或为底部轻量平台摘要提供内容

`listEvents()` 不再参与首页主态判断；如果首页实现不再需要它，允许从首页首屏请求中移除。

## Error Handling

### Providers Failed

- 首页显示“无法确认 AI 接入状态”的异常提示
- 不显示“你是新用户，请先接入”这种误导性文案

### Usage Snapshot Failed

- 首页显示“无法加载当前用量状态”的异常提示
- 保留引导卡片或辅助入口，但不把用户强制归类为“没有数据的新用户”

### Auxiliary Data Failed

- `getDashboard()` 失败：
  - `代码关联` 信号显示 `未知`
  - 不影响首页主态
- `listEvents()` 失败：
  - 不影响首页主态
  - 最多影响“使用记录入口”的文案或弱提示

## Copy Contract

首页必须统一使用用户动作语言，而不是平台内部术语。首页主区避免继续突出：

- `Platform Signals`
- `Tracked Workflows`
- `AI PRs`
- `Code reporting`

替换原则：

- 用“完成 AI 接入”替代“Follow My Setup / 完成接入状态”
- 用“查看我的用量”替代“平台信号”
- 用“查看使用记录 / 查看代码仓库”替代平台或排障视角的标签

## Testing

前端回归至少覆盖以下场景：

1. `needs_setup`
   - 首页默认展开引导卡片
   - 主 CTA 指向 `/user`
   - 用量区不抢主视觉
2. `setup_ready_waiting_for_first_usage`
   - 首页仍默认展开引导卡片
   - 标题为“已接入，开始第一次使用”类文案
3. `established_user`
   - 首页默认收起引导卡片
   - 用量区主标题 / 主视觉在引导卡片之上
   - 首页首屏不显示费用摘要
4. `degraded_error`
   - providers 或 usage snapshot 失败时，首页先显示异常提示
   - 不误判成新用户
5. 手动展开 / 收起引导卡片
   - 同一渲染周期内交互正确
   - 默认状态仍由生命周期决定
6. 文案断言
   - 首页不再以 `Platform Signals`、`Setup Status`、`Next Steps` 当前形态作为主区标题
   - 新的导航与首页文案保持一致

## Acceptance Criteria

实现完成后，必须满足：

1. 新用户打开 `/`，首屏第一目标是去 `/user` 完成 `AI 接入与配置`。
2. 已接入但没有有效用量数据的用户，仍先看到引导卡片，而不是先看到平台指标。
3. 老用户打开 `/`，首屏第一目标是查看“我的用量”。
4. 首页嵌入态的用量首屏不展示费用摘要。
5. `Platform Signals`、`Setup Status`、`Next Steps` 不再以当前并列大区块形态占据首页主区。
6. 关键接口失败时，首页不把用户误判为新用户。
