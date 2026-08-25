# 列表与分页一致性设计

**日期**: 2026-08-25

**状态**: 已在 `feat/frontend-list-pagination-consistency` 实现并通过本地资格验证；尚未合并或发布

## 实现证据

2026-08-25 本分支本地验证结果：

- `npm test`: 59 个测试文件、849 个测试通过；
- Node `20.20.2` 下 `npm run build:measure`: structural assertions 通过，initial shell 为 72,869 gzip bytes，默认英文 `/usage` 为 158,908，默认英文 `/admin/users` 为 254,272，均在现有限额内；
- `npm run test:e2e:role`: 126/126 通过；
- 390、768、1024、1280、1440 px route matrix 在真实多页 mock 下通过 overflow、content-fit、touch-target 和 interaction 检查，并完成人工截图抽查；
- `git diff --check` 通过。

这些结果只证明本地分支实现，不代表默认分支合并、GitHub hosted checks、平台 release 或生产部署。

## 生效边界

本文定义前端列表工作面和集合导航合同。本分支实现与
[`docs/ui-guidelines.md`](../../ui-guidelines.md) 已同步；合并和发布前，默认分支与生产环境仍以各自当前代码为准。

本文是现有 UI 合同的增量设计，不改变以下领域合同：

- [`2026-07-14-end-to-end-page-loading-performance-design.md`](./2026-07-14-end-to-end-page-loading-performance-design.md)
  定义的独立加载、旧响应失效、缓存和页面性能边界；
- [`2026-06-26-team-usage-representative-quota-design.md`](./2026-06-26-team-usage-representative-quota-design.md)
  与 `docs/architecture.md` 定义的 Team Usage snapshot cursor、成员排行和组织分支合同；
- [`2026-08-11-codex-commit-token-attribution-v2-design.md`](./2026-08-11-codex-commit-token-attribution-v2-design.md)
  定义的 Activity 页面与 cursor 完整性边界；
- [`2026-06-22-configurable-directory-sync-design.md`](./2026-06-22-configurable-directory-sync-design.md)
  定义的 Directory History 和 Offboarding 数据合同；
- [`2026-08-19-relay-group-mapping-contract.md`](./2026-08-19-relay-group-mapping-contract.md)
  定义的 Relay Mapping 业务行为。

实现不得为了统一视觉而把 cursor 伪装成可随机跳转页码，也不得把树中不同分支合并成一个全局分页集合。

## 背景

当前前端存在三种合理的集合导航语义，但同一语义又有多套独立视觉实现：

- Admin Users、Repositories、Offboarding、Directory History 和部分 Relay 搜索都能获得精确总数和可寻址页码，但分别使用手写按钮或不同配置的 `ElPagination`；
- Activity 和 Team Ranking 都使用 opaque cursor，但分别使用不同 footer；
- 组织树和部门树按分支追加数据，这与普通页码本来就不是同一种导航；
- 2026-08-10 的 Element Plus 迁移统一了基础控件，但刻意保留了各功能原有业务结构；
- `docs/ui-guidelines.md` 规定了组件库、响应式和 viewport 验证，却没有规定分页分类、信息密度、状态归属和边界行为。

因此，当前差异并非来自 Element Plus，也不能全部由 API 差异解释。根因是功能按页面纵向演进，缺少跨页面的列表与集合导航合同。

## 目标

- 相同数据语义使用相同导航模式和视觉规则。
- 保留 indexed、cursor 和 branch-incremental 三种真实集合语义。
- 为完整页面和嵌入式集合定义稳定、有限的密度差异。
- 统一分页的 URL 状态、页容量、移动端收缩和边界状态。
- 让桌面表格与移动列表属于同一个集合工作面。
- 在不引入镜像 wrapper 的前提下复用真正拥有导航语义的组件。

## 非目标

- 不统一或重写后端分页协议。
- 不把所有列表强制改成数字页码。
- 不引入 Data Grid、虚拟滚动、无限滚动或通用表格框架。
- 不改变各功能的排序、筛选、授权、snapshot、freshness 或 mutation 合同。
- 不在本设计中改变 Quota Reset 的全量抓取和本地筛选行为。
- 不在实现前把本设计回写成 `docs/ui-guidelines.md` 的当前实现事实。

## 设计语言

本文使用以下术语描述 UI 合同。它们是通用交互概念，不写入项目业务领域 glossary `CONTEXT.md`。

**集合工作面（Collection Surface）**：
用户操作一个结果集合的完整界面单元，包含标题或上下文、筛选、内容、状态和集合导航。

**索引分页（Indexed Pagination）**：
具有精确总数和可寻址页序号的集合导航。底层请求可以使用 `page/page_size` 或等价的 `limit/offset`。

**游标翻页（Cursor Pagination）**：
使用 opaque continuation 在稳定排序或 snapshot 中顺序移动的集合导航。它只承诺可用方向，不承诺随机跳页。

**分支增量加载（Branch Incremental Loading）**：
在层级结构的一个具体分支末尾追加该分支下一批直属部门或成员。它不控制其他分支。

“列表呈现”只描述宽屏表格、窄屏分隔行或树等内容布局；“集合导航”描述 indexed、cursor 或 branch-incremental。两者不得混用为同一个模糊的“列表样式”概念。

## 导航分类合同

每个集合按数据能力分类，而不是由页面作者自由选择控件：

1. 有精确 `total`，并可用 page 或 offset 寻址任意页：使用索引分页。
2. 只有 opaque next cursor，或 cursor 与 snapshot / 完整性边界绑定：使用游标翻页。
3. 数据属于树中一个父节点的后续直属集合：使用分支增量加载。

客户端本地 `slice` 与服务端 `page/page_size` 的加载位置不同，但只要用户面对的是精确总数和可寻址页码，均属于索引分页。反之，前端记录了 cursor 栈也不意味着 cursor 已变成随机可寻址页码。

## 集合工作面合同

- 一个集合只使用一个外层工作面。标题或上下文、筛选、内容、empty/error/loading 状态和 footer 属于同一容器。
- 宽屏优先使用 `ElTable` 承载需要横向比较的扫描型数据。
- 窄屏使用同一工作面内的分隔行，不默认把每条记录做成独立阴影卡片。
- 只有记录本身是需要独立分组、状态和操作边界的业务对象时，才使用单项卡片。
- 不在外层集合卡片内再堆叠一组装饰性卡片，也不通过横向页面滚动保留不可用的宽表格。
- 集合导航位于对应集合内容之后。完整页面与嵌入式集合可以使用不同密度，但不得因功能不同随意改变对齐、按钮尺寸、文案或边界行为。
- TailwindCSS 继续负责容器、Grid/Flex、breakpoint 和响应式次序；Element Plus 继续负责表格、按钮、分页和反馈控件。

## 索引分页合同

### 完整页面

完整页面集合使用标准 footer：

- 左侧显示当前真实范围和精确总数，例如“第 1-20 项，共 238 项”；
- 右侧使用 Element Plus 页码导航；
- 默认每页 20 项；
- 用户可选每页 `20 / 50 / 100` 项；
- 桌面显示范围、总数、页容量和数字页码；
- 移动端收缩为“上一页 / 当前页 / 下一页”，不得横向溢出。

完整页面的 `page`、`page_size`、搜索词和筛选条件由 URL 管理。刷新、分享链接及浏览器前进/后退必须恢复同一列表状态。搜索或筛选变化必须回到第 1 页。非法、不支持或超出上限的 URL 值安全归一化到合同允许值，不得形成无限重载。

URL 可以省略默认值，但页面状态与 URL 的双向恢复必须确定且可测试。

### 嵌入式集合

下拉框、弹窗、设置区块或页面内局部业务对象中的集合使用紧凑索引分页：

- 保留数字页码能力，但不显示 page-size selector；
- 使用该集合合同规定的固定页容量；
- 状态保留在所属组件或业务对象内，不写入页面 URL；
- 弹窗或下拉框关闭、所属业务对象切换、搜索条件变化时重置到第 1 页；
- 移动或狭窄容器同样收缩为“上一页 / 当前页 / 下一页”。

所有用户可见页码按 1 起始表达。底层 `limit/offset` 或 0-based 响应必须在 API/store 边界归一化，不得把两种基数泄漏到组件模板。

### 组件边界

索引分页直接使用 `ElPagination`，不新增 `AppPagination` 或只转发 props 的镜像 wrapper。相同场景通过固定 page recipe、共享常量和测试合同保持一致。

完整与紧凑模式必须统一：

- pager count；
- desktop 和 mobile layout；
- total/range 文案；
- disabled、loading 和 error 行为；
- 单页隐藏规则。

## 游标翻页合同

- footer 中“上一页”和“下一页”占据固定位置；当前方向不可用时禁用，不通过隐藏按钮改变布局。
- 后端提供真实 rank range 和 `total_count` 时，中间显示真实范围与总数。
- 后端不提供总数时，不推算或显示虚假总页数。
- 不根据前端 cursor 栈渲染可点击数字页码。
- 当前查询、排序、授权范围或业务 subject 变化时，丢弃不再适用的 cursor 栈并从首批结果重新开始。
- Team Usage 现有 `snapshot_expired` 恢复只重启对应成员或分支集合，继续遵守当前性能合同。

现有 `CursorPager` 可以演进为共享的 cursor 语义组件，并支持可选 range/total。它不是对 `ElPagination` 的改名转发，而是明确拥有 sequential navigation 的固定布局和边界状态。

## 分支增量加载合同

- “加载更多部门”和“加载更多成员”出现在对应分支的末尾。
- 请求期间按钮保留原位、进入 loading/disabled 状态，不清空已加载节点。
- cursor 耗尽后移除该分支入口。
- 一个分支失败时保留该分支已加载内容，并在原位提供错误和重试能力。
- 不使用页面底部全局 paginator 控制多条分支，也不使用嵌套无限滚动自动触发请求。

## 空、单页、加载与失败状态

- 空集合显示统一 empty state，不显示分页。
- 非空但只有一页时不显示分页。
- 多页集合在翻页请求期间保留最近成功内容和 footer 尺寸，禁用导航并显示局部 loading。
- 翻页失败时保留最近成功页及其导航状态，在集合工作面内就地提示；不得清空内容或静默跳回第 1 页。
- 同一工作面在 loading、partial、stale、error、success 和 disabled 状态之间不得改变业务请求时机或合并原本独立的生命周期。

## 页面归类与目标模式

| 界面或集合 | 当前数据能力 | 目标模式 | 状态归属 / 容量 |
| --- | --- | --- | --- |
| Admin Users 用户列表 | `page/page_size/total` | 完整页面索引分页 | URL；20/50/100 |
| Repositories inventory | `page/page_size/total` | 完整页面索引分页 | URL；20/50/100 |
| Directory Offboarding | `page/page_size/total` | 完整页面索引分页 | URL；20/50/100 |
| Admin Users 根部门 | `page/page_size/total` | 嵌入式索引分页 | 局部；固定 25 |
| Admin Department Picker | `page/page_size/total` | 嵌入式索引分页 | 局部；固定 20 |
| Directory run history | `limit/offset` + `total` | 嵌入式索引分页 | 局部；固定 20 |
| Relay target user search | `page/page_size/total` | 嵌入式索引分页 | 目标局部；固定 20 |
| Relay Account search | `page/page_size/total` | 嵌入式索引分页 | target group 局部；固定 20；补齐当前缺失的后续页入口 |
| Relay managed mappings | 完整数组 + 客户端 slice | 嵌入式索引分页 | 页面局部；固定 10 |
| Activity repositories / PRs | opaque cursor | 游标翻页 | 保留当前查询局部 cursor 栈 |
| Activity team members | opaque cursor | 游标翻页 | 保留当前 team 局部 cursor 栈 |
| Team Usage member ranking | snapshot-bound cursor + total | 游标翻页 | 保留真实 rank range / total |
| Team Usage / Activity 组织树 | 分支 cursor | 分支增量加载 | 各父节点独立 |
| Admin Users 子部门 | 分支 page | 分支增量加载 | 各父节点独立；固定 25 |

宽屏表格和窄屏分隔行共用同一份已加载数据、筛选和导航状态，不得为响应式布局各自发起一套分页请求。

## Quota Reset 已知缺口

Quota Reset 当前针对多个服务端分页队列循环抓取所有页面，再在前端执行本地筛选和定位，用户界面不显示分页。

本设计只记录该事实，不把它重新分类为长期 UI 合同，也不在列表一致性实现中改变它。后续必须通过独立设计确认：

- 三个队列的可见分页或增量加载方式；
- 跨页状态筛选和选中请求定位；
- polling、审批 mutation 和页面 freshness；
- 数据增长后的请求数与首屏等待预算。

在该独立设计完成前，不得借列表样式迁移顺带把 Quota Reset 改成普通服务端分页。

## 实现边界

- 主范围应复用现有 API 能力，不为视觉统一改变后端排序、total、cursor 或 snapshot 合同。
- 索引分页直接组合 Element Plus；不新增 `AppList`、`AppTable`、`AppPagination` 等镜像抽象。
- 允许提炼共享常量、URL 参数解析和真正拥有 cursor 导航语义的组件。
- 现有 feature-specific loading、partial、stale、error 和 mutation 生命周期必须保留。
- 稳定的 `data-testid` 尽量保留；行为变化必须更新 rendered page tests，而不是只测 helper。
- 实现可按索引完整页面、索引嵌入式、cursor/branch、响应式工作面四个可独立验证的批次推进，但任一已迁移页面都必须完整满足其目标模式，不能只更换控件外观。

## 测试与验证合同

每个迁移集合至少覆盖：

1. 空集合与单页不显示分页，多页显示正确导航。
2. 精确 range、total、当前页和 disabled 边界正确。
3. 完整页面可从 URL 恢复 page、page size、搜索和筛选；搜索或筛选变化回到第 1 页。
4. `20 / 50 / 100` 仅出现在完整页面，嵌入式集合保持固定容量。
5. cursor 页面不出现数字跳页；有真实 total 时显示真实范围，无 total 时不伪造。
6. 分支 load-more 只更新目标分支，并保留其他分支状态。
7. 翻页 loading 保留当前内容，失败保留最近成功页并就地提示。
8. 宽屏表格和窄屏分隔行共享同一请求和导航状态。
9. 快速搜索、筛选或翻页时，旧响应不能覆盖较新的意图。
10. Quota Reset 请求和展示行为在本范围内保持不变。

前端实现完成后必须运行相关 rendered page tests、完整 `npm test`、`npm run build`、`npm run build:measure` 和 `npm run test:e2e:role`，并按 `docs/ui-guidelines.md` 在 390、768、1024、1280、1440 px 验证集合工作面、文案、分页和操作无裁切、重叠或横向页面溢出。

## 文档交付边界

- 实现分支已新增本文，并在轻量导航文档中增加入口。
- 通用 UI 术语保留在本文，不写入业务领域 `CONTEXT.md`。
- 本设计局部、可渐进实施且容易回退，不创建 ADR。
- 稳定规则已同步到 `docs/ui-guidelines.md`；合并或发布状态必须继续按真实交付证据更新。
- 只有实现改变项目级运行时边界时才更新 `docs/architecture.md`；预期的纯前端收敛不需要架构更新。
- 若后续创建 implementation plan，必须遵守 `AGENTS.md` 的实时 checkbox 和状态维护规则。

## 验收标准

- 相同数据语义不再因页面来源不同呈现不同分页模式。
- 完整页面、嵌入式集合、cursor 集合和树分支各自只有一个明确视觉合同。
- 完整页面分页和筛选可通过 URL 恢复，嵌入式状态不会污染 URL。
- 页面在桌面和移动端保持一个集合工作面，移动端不默认堆叠装饰性卡片。
- 空、单页、loading 和 failure 状态稳定且一致。
- 不伪造 cursor 页码，不用全局 paginator 控制树分支。
- Quota Reset 保持现状并作为独立数据加载缺口后续设计。
