# AI 用量窗口偏好设计

**日期**: 2026-08-24

**状态**: 已在 `codex/issue-357-usage-window-preference` 实现；尚未合并或发布

## 继承关系

本文是以下现有合同的增量设计：

- [2026-06-06-user-usage-trend-design.md](./2026-06-06-user-usage-trend-design.md) 定义个人用量的 Today / 7 Days / 30 Days 窗口、日期计算和 selected-range 汇总。
- [2026-06-26-team-usage-representative-quota-design.md](./2026-06-26-team-usage-representative-quota-design.md) 定义团队用量使用相同窗口语义。
- [2026-08-19-personal-usage-reset-and-oauth-pool-design.md](./2026-08-19-personal-usage-reset-and-oauth-pool-design.md) 定义个人配额和重置时间跟随所选窗口。
- [2026-07-14-end-to-end-page-loading-performance-design.md](./2026-07-14-end-to-end-page-loading-performance-design.md) 定义前端首屏并发、请求失效保护和加载性能边界。

本文只替换 AI Usage 首次挂载时“固定选择 `30d`”的行为：实现后优先使用合法的浏览器偏好，缺失或非法时仍回退 `30d`。本文不改变各窗口的日期、粒度、配额映射、缓存或 API 合同。

Activity 的 `from` / `to` / `timezone` URL 合同继续以 [2026-08-11-codex-commit-token-attribution-v2-design.md](./2026-08-11-codex-commit-token-attribution-v2-design.md) 为准，不由本文覆盖。

## 背景

AI Usage 有三类使用同一窗口语义的界面：

- 个人 `/usage`
- 团队 `/usage/team`
- 成员 `/usage/members/:user_id`

个人和成员界面共用一个 dashboard，团队界面独立承载。三者都支持 `today | 7d | 30d`。用户希望平台只记住最近一次明确选择的窗口，并在下次进入上述任一界面时直接以该窗口发起首个请求。

Activity 使用 `7 | 30 | 90 | custom`，且绝对日期由 URL 表达。它与 AI Usage 的值域和导航合同不同，不属于同一偏好。

## 目标

- 在浏览器本地记住最近一次明确选择的 AI Usage 窗口。
- 让个人、团队和成员用量界面共享同一偏好。
- 在首个数据请求前恢复偏好，避免先请求 `30d` 再切换。
- 保留当前窗口日期语义、请求并发和旧响应失效保护。
- 对缺失、非法或不可访问的浏览器存储安全回退。

## 非目标

- 不同步到后端、其他浏览器或其他设备。
- 不按登录用户隔离偏好。
- 不持久化 Activity 的预设或自定义日期。
- 不建立通用偏好中心、Pinia 持久化插件或新的服务端接口。
- 不实时同步已打开的其他浏览器标签页。
- 不改变 Today / 7 Days / 30 Days 的日期范围、粒度或 API 参数。

## 范围合同

| 界面 | 路由 | 是否读取和写入偏好 |
| --- | --- | --- |
| 个人用量 | `/usage` | 是 |
| 团队用量 | `/usage/team` | 是 |
| 成员用量 | `/usage/members/:user_id` | 是 |
| 个人 / 成员 / 团队 Activity | `/activity*` | 否，继续使用 URL |

个人、团队和成员用量读取同一个 key。用户在任一界面选择窗口后，随后挂载的另一个 AI Usage 界面恢复该选择。

## 偏好合同

### 值域与默认值

合法值仅为 `today`、`7d` 和 `30d`。缺少偏好、存储值不在白名单内，或浏览器存储读取失败时，默认值均为 `30d`。

读取非法值时只忽略并回退，不自动改写或清理存储。默认初始化也不写入存储；只有用户明确选择才形成偏好。

### 所有权与生命周期

偏好属于当前浏览器 profile 和应用 origin，不属于平台用户账号：

- 同一浏览器切换账号后继续使用同一偏好。
- 退出登录不清除偏好。
- 不承诺跨 origin、浏览器或设备同步。
- 已打开的其他标签页不监听 `storage` 事件；它在下一次挂载或刷新时读取最新值。

偏好不包含身份、用量、配额或凭证信息。

### 读取时机

组件在建立首个请求参数前同步读取偏好：

1. 读取并白名单校验存储值。
2. 得到合法偏好或 `30d` fallback。
3. 用该值初始化选择控件。
4. 用同一个值构造首个 dashboard、quota、pool usage 或 team usage 请求。

不得先按 `30d` 请求，再在 mounted 后异步恢复偏好。

### 写入时机

用户明确点击窗口时按以下顺序处理：

1. 立即更新选择控件。
2. 立即尝试写入偏好。
3. 以新窗口发起数据请求。

偏好表达用户意图，不以接口成功为前提。请求失败、超时或被后续请求失效时，不回滚已经写入的偏好。写入存储失败不得阻止选中状态更新或数据请求，也不显示额外错误。

程序初始化、默认 fallback、接口返回窗口和路由切换不得被当成用户选择写回存储。

## 请求与展示不变量

- 当前选择表达用户意图并立即驱动控件和请求。
- 成功数据窗口只在对应请求成功后更新。
- 旧范围请求不得覆盖较新的选择或成功数据。
- 个人页的 dashboard、group quotas 和 group pool usage 继续使用同一个已恢复窗口并按现有合同并发加载。
- 团队页的 summary、trend、members 和 organization 继续使用同一个已恢复绝对范围。

本地偏好只决定首次和后续选择，不成为服务端数据真实性或 freshness 的来源。

## 实现边界

前端使用一个轻量 typed helper 集中拥有：

- `UsageWindow` 联合类型
- 合法值白名单
- `30d` 默认值
- 单一存储 key `ae.usage.window`
- 带异常保护的同步 read / write

helper 允许测试注入最小 storage 接口，并保护 storage 属性访问及 `getItem` / `setItem` 异常。个人/成员 dashboard 和团队界面复用该类型与 helper。实现没有引入 Pinia store、persist plugin、Vue composable 生命周期、后端依赖或额外日期计算重构。

## 测试合同

回归覆盖：

1. 存储缺失时，个人和团队首个请求继续使用 `30d`。
2. `today`、`7d`、`30d` 均可恢复，并直接驱动首个请求。
3. 非法存储值、storage 属性异常和读写异常静默回退或忽略。
4. 个人、团队和成员界面使用同一偏好。
5. 用户选择后立即写入，即使对应请求失败也不回滚。
6. 快速连续切换时，旧请求仍不能覆盖新选择或新数据。
7. Activity 不读取或写入 AI Usage 偏好。

## 验收标准

- 三个 AI Usage 界面共享最近一次明确选择的合法窗口。
- 首个请求直接使用恢复值，不产生额外的默认窗口请求。
- 请求失败不丢失用户已经表达的偏好。
- 无存储、非法值和存储异常均退回现有 `30d` 行为。
- Activity、服务端合同、日期语义和请求竞态保护保持不变。
