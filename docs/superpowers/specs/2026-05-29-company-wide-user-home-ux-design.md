# Company-Wide User Home UX Design

**Date:** 2026-05-29
**Status:** Proposed design for review
**Scope:** `frontend/src/router/`, `frontend/src/components/`, `frontend/src/views/`, `frontend/src/api/`, `frontend/src/stores/`, `frontend/src/types/`, `frontend/src/__tests__/`, `docs/architecture.md`
**Related:**
- [2026-05-21-user-page-cli-self-serve-design.md](./2026-05-21-user-page-cli-self-serve-design.md)
- [2026-05-26-user-cli-setup-checklist-design.md](./2026-05-26-user-cli-setup-checklist-design.md)
- [2026-05-19-ae-cli-deterministic-tool-configuration-design.md](./2026-05-19-ae-cli-deterministic-tool-configuration-design.md)
- [2026-05-28-pr-sync-job-progress-usage-freshness-design.md](./2026-05-28-pr-sync-job-progress-usage-freshness-design.md)
- [docs/architecture.md](../../architecture.md)
- [docs/ui-review/company-wide-onboarding-review.html](../../ui-review/company-wide-onboarding-review.html)

## Spec Relationship

- 本文定义 AI 效能平台面向全公司用户的登录后默认体验，选择评审稿中的 **B「我的 AI 使用中心」** 作为普通使用者默认入口。
- 评审 HTML 使用中文是为了本轮产品评审沟通；它不是“最终产品只使用中文”的约束。当前产品以英文 UI 为基础，本文要求实现多语言文案策略，避免把既有英文体验直接替换为中文-only。
- 本文不改变 `ae-cli login`、`ae-cli discover`、`ae-cli hooks`、`ae-cli init`、`ae-cli sync` 或 `ae-cli doctor` 的命令语义；CLI 合同仍以相关 CLI specs 为准。
- 本文不改变 relay provider、user provider credential、group credential、PR usage snapshot、tool usage event 的后端数据合同。
- 本文重新组织前端信息架构、页面命名、默认入口、空状态、渐进披露和响应式 shell。若实现过程中需要新增后端字段，必须补充单独 contract 或扩展本文的 Data Requirements。
- 本文不回写历史 specs。实现完成后，`docs/architecture.md` 必须同步反映新的前端入口、普通用户视角、管理视角和响应式布局边界。

## Problem

当前系统已经具备 repo、events、settings、`/user` 自助接入、admin users 等能力，但默认体验更像研发或管理员控制台：

1. 登录后的首页是全局 Dashboard，指标命名偏技术，普通员工不容易判断“我现在要做什么”。
2. 导航直接暴露 `Events`、`Repos`、`Settings`、`Providers`、`Credentials` 等工程或运维词汇，非技术用户理解成本高。
3. `/user` 页面承担了 CLI 接入和 credential 自助，但标题、说明和优先级仍偏“regular developers”，不适合作为全公司默认使用入口。
4. 原始事件页适合排障和审计，但不适合作为普通用户理解自己 AI 使用记录的首屏。
5. 当前固定侧边栏在窄屏上占用空间过大，移动端和小屏笔记本体验不友好。

目标用户不再只是假设为研发。系统需要同时服务：

- 普通使用者：想知道自己的 AI 工具是否接好了、本周有没有数据、下一步该做什么。
- 研发用户：需要进入 CLI、repo、doctor、使用记录和 PR 归因细节。
- 团队负责人：想看团队接入状态、趋势、异常和可跟进对象。
- 管理员 / 专家：需要配置 provider、credential、LDAP、SCM、deployment 和排障事件。

## Goals

1. 登录后默认入口改为面向普通使用者的 **My AI Usage** / **我的 AI 使用中心**，先回答“我是谁、接入是否正常、本周有什么结果、下一步做什么”。
2. 保留当前研发自助接入能力，但把 CLI 命令、provider group、API key、doctor 等技术细节放进 `My Setup` / `我的接入` 或高级区块。
3. 把原始 `Events` 从默认技术名调整为更友好的 `Usage Records` / `使用记录`，默认展示可理解摘要，原始事件细节通过高级展开或详情抽屉进入。
4. 把管理员配置收敛到 `Admin Console` / `管理后台` 或专家模式，避免普通用户首屏看到 SCM Providers、Credentials、Relay Providers、Deployment、LDAP 等配置域。
5. 改善响应式 shell：桌面端可保留侧栏，平板和手机使用顶部栏 + 抽屉导航，避免固定 240px 侧栏压缩内容。
6. 建立统一的空状态、加载状态、错误状态和权限受限状态，让无数据用户也能知道下一步。
7. 建立 `en-US` / `zh-CN` 双语文案合同。英文保留为默认 / source locale，中文作为公司内普通用户友好版本；实现不能只把硬编码英文替换成硬编码中文。
8. 使用更清晰的业务化术语，但不破坏代码和 API 内部的 provider / event / repo / credential 命名。

## Non-Goals

1. 第一版不新增组织架构、团队权限或复杂人群分层模型；已有角色和已有 API 能支持的内容优先。
2. 第一版不在浏览器里执行本机 CLI 命令，不引入 local proxy、daemon 或 browser-to-local bridge。
3. 第一版不改变后端 attribution、checkpoint、PR usage snapshot 或 relay key creation 的事实链。
4. 第一版不重写 `/settings` 的全部配置能力，只调整入口、分组和默认可见性。
5. 第一版不做营销型 landing page。登录后第一屏必须是可使用的工作台，不是产品介绍页。
6. 第一版不把历史 spec 全量改写成当前 UI 叙述。
7. 第一版不要求后端返回本地化错误文案，也不翻译 raw CLI output、provider name、model name、repo name、branch name、commit SHA 或 API enum。

## Reviewed Alternatives

### Option A: Role Split Gateway

登录后先问用户“我是使用者 / 研发 / 负责人 / 管理员”，再进入不同工作台。

优点：

- 对跨角色用户友好。
- 能解释系统为什么有不同能力。

缺点：

- 首次进入多一步选择。
- 对已经登录的普通用户仍然显得像入口页，不像工作台。

### Option B: My AI Usage Center

登录后默认进入个人中心，首屏展示个人接入状态、本周 AI 使用、数据状态和下一步操作。研发和管理员能力通过导航或高级入口进入。

优点：

- 最符合全公司普通使用者的默认心智。
- 对无数据用户也能给出明确下一步。
- 不牺牲研发和管理员能力，只是调整优先级。

缺点：

- 需要重新定义 `/` 与 `/user` 的关系。
- 当前 Dashboard 的全局指标需要挪到团队或管理视角。

### Option C: Team Operations Dashboard

登录后默认看团队趋势、排名、接入覆盖和异常提醒。

优点：

- 适合负责人和运营推广。
- 可以支撑团队经营和 adoption 观察。

缺点：

- 普通使用者容易觉得和自己无关。
- 需要更明确的团队归属和权限数据。

### Option D: Simple Mode + Expert Mode

保留当前技术页面，但默认显示简单模式；用户可切换专家模式看到原始 events、providers、credentials、deployment 等。

优点：

- 对管理员和研发排障友好。
- 改动可以分阶段落地。

缺点：

- 如果没有新的普通用户首页，只做模式切换仍然解决不了默认入口问题。

### Decision

采用 **Option B: My AI Usage Center** 作为第一版主方案。

Option A 作为可选的首次进入 / 跨角色入口参考；Option C 作为团队负责人后续视图；Option D 作为专家/管理能力的承载方式。

## Information Architecture

### Default Route

`/` 从当前技术 Dashboard 调整为普通用户首页：`My AI Usage` / `我的 AI 使用中心`。

页面默认对所有已登录用户可访问。管理员登录后也先看到个人视角，但导航中额外显示已经实现的管理或专家入口。`团队看板` 在第一版不是必需独立路由；如果本轮没有实现团队视图，不应在导航中放置空入口。

### Primary Navigation

默认导航使用业务化标签，但不能把当前英文产品直接改成中文-only。英文为默认 / source locale，中文为 `zh-CN` locale。

| Current Label | en-US Label | zh-CN Label | Default Audience | Notes |
| --- | --- | --- | --- | --- |
| Dashboard | My AI Usage | 我的 AI 使用中心 | 所有用户 | `/` 默认首页 |
| User | My Setup | 我的接入 | 所有用户，研发优先 | 保留 CLI setup 和 provider group credential |
| Events | Usage Records | 使用记录 | 所有用户 | 默认摘要化，原始字段进入高级详情 |
| Repos | Code Repositories | 代码仓库 | 研发、负责人、管理员 | Repo detail 继续承载 PR usage 和 freshness |
| Users | User Management | 用户管理 | 管理员 | 只对 admin 可见 |
| Settings | Admin Console | 管理后台 | 管理员 / 专家 | 聚合 provider、credential、relay、LDAP、deployment |

第一版不要求新增独立 team dashboard / `团队看板` 路由。如果现有数据不足，可把团队/全局指标保留在管理员可见的 `Admin Console` / `管理后台` 或后续 spec 中。

### App Shell

桌面端：

- 左侧导航保留，但标签改为本地化后的业务语义。
- 当前用户区域明确作为 `My Setup` / `我的接入` 或个人菜单入口。
- 管理员专属入口和普通入口视觉分组，避免混在同一层级。

平板和手机：

- 顶部栏展示产品名、当前页面标题、菜单按钮、用户菜单。
- 导航进入抽屉或全屏菜单。
- 内容区不得依赖固定侧栏宽度。
- 常用主操作按钮保持至少 44px 触控高度。

## Language Strategy

### Locale Policy

第一版支持两个界面语言：

- `en-US`: 默认 / source locale，保留当前英文产品基础，适合跨区域或习惯英文工具链的用户。
- `zh-CN`: 中文本地化，服务公司内非技术或中文优先用户。

Locale selection:

1. 用户显式选择的语言优先，第一版可存储在 `localStorage`，不要求新增后端用户偏好字段。
2. 没有显式选择时，使用浏览器语言匹配 `zh-CN`；否则回退到 `en-US`。
3. 用户菜单或设置入口应提供轻量语言切换，不把语言切换藏到 admin-only 页面。

### Implementation Boundary

当前前端没有现成 i18n 层。实现本 spec 时，应为本轮触达的 shell 和页面引入最小可维护的文案层：

- app shell / navigation
- `/` personal home
- `/user` setup wording touched by this redesign
- `/events` user-facing labels touched by this redesign
- shared empty / loading / error states touched by this redesign

未触达的大型 admin form 可以继续英文，除非实现计划明确覆盖。不要为了第一版多语言一次性重写所有 settings 文案。

Backend API fields, route names, TypeScript types, enum values, command strings, provider names, model names, repo names, branch names, commit SHAs, raw event fields, and raw CLI output remain untranslated.

### Review Artifact Relationship

`docs/ui-review/company-wide-onboarding-review.html` 是中文评审稿，用于确认信息架构和普通用户心智。实现阶段应把其中的中文文案落到 `zh-CN` locale，同时补齐对应 `en-US` 文案；不能直接把 HTML 中文 copy 当作唯一生产文案。

## Page Design

### `/` My AI Usage Center

首屏从上到下回答四个问题：

1. 我当前账号是谁。
2. 我的 AI 接入是否正常。
3. 本周有没有产生可见使用数据。
4. 我下一步应该做什么。

Recommended sections:

1. `Personal Status` / `个人状态条`
   - 显示用户名、角色、登录来源、数据更新时间。
   - 如果数据延迟或不可用，显示具体状态而不是空白。
2. `This Week` / `本周概览`
   - `AI 使用次数`
   - `关联到代码的使用`
   - `AI PR / PR 使用快照`
   - `已接入工具`
   - 指标不可用时显示 `暂无数据` + 原因 + 下一步，而不是 `--` 独自出现。
3. `Setup Status` / `接入状态`
   - `CLI 已登录`
   - `AI 工具配置`
   - `Git hooks`
   - `当前仓库初始化`
   - 页面不能声称已探测本机状态；只能基于后端数据、已知事件和用户 action 给出“平台看到的状态”。
4. `Next Steps` / `下一步`
   - 未接入：进入 `My Setup` / `我的接入`，按机器级和 repo 级步骤完成配置。
   - 有接入但无数据：运行 `ae-cli doctor`，检查 hooks 和最近 commit。
   - 有数据但未关联 repo/PR：进入 `Code Repositories` / `代码仓库` 或查看 `Usage Records` / `使用记录` 的绑定状态。
   - 数据正常：查看本周趋势或最近记录。
5. `Recent Usage Records` / `最近使用记录`
   - 展示最近几条可理解记录：工具、时间、repo、绑定状态、使用量摘要。
   - 详情跳到 `Usage Records` / `使用记录`。

### `/user` My Setup

`/user` 调整为 `My Setup` / `我的接入`，保留现有 `/user` self-serve 合同：

- profile summary
- provider-first, group-second credential self-serve
- Machine Setup
- Per-Repo Setup
- recovery commands
- provider test action

UI 表达从“regular developers”调整为“接入 AI 工具和代码仓库”。技术命令仍保留，但必须按任务折叠：

1. 基础状态和下一步优先。
2. 命令块默认可复制。
3. 解释文案避免要求用户先理解 provider / credential / group 的内部模型。
4. 高级 credential、reveal、regenerate、provider test 操作保留在明确的 `高级` 或 `密钥管理`区域。

### `/events` Usage Records

`/events` 调整为 `Usage Records` / `使用记录`。

默认列表面向普通用户：

- 时间
- 工具
- 使用量
- 关联状态
- 关联仓库 / PR
- 状态解释

高级详情保留原始事件字段：

- event id
- binding status
- source fields
- checkpoint / repo metadata
- raw timestamps

管理员仍可跨用户搜索；普通用户默认只看自己的记录。若当前 API 已经按权限处理，前端只做入口与显示调整。

### `/repos` Code Repositories

`/repos` 调整为 `Code Repositories` / `代码仓库`。

默认目标是解释“哪些仓库已接入，哪些仓库的数据可用，PR 使用快照是否新鲜”。文案应沿用 PR sync freshness 的具体状态，不把延迟或无数据简单显示成失败。

### Admin / Expert Area

`/settings` 调整为 `Admin Console` / `管理后台`，并在页面内按任务分组：

- AI 服务配置：Relay Providers、provider credential、group routing
- 代码平台配置：SCM Providers、webhook、repo sync
- 组织与登录：LDAP、auth options、local users
- 部署与运行：deployment、health、diagnostics

普通用户不应在默认导航看到该入口。管理员和专家用户仍可访问完整配置能力。

## Terminology

前端显示层采用以下双语术语映射：

| Internal Term | en-US Term | zh-CN Term | Notes |
| --- | --- | --- | --- |
| Dashboard | My AI Usage | 我的 AI 使用中心 | 默认首页 |
| User | My Setup | 我的接入 | CLI 和工具接入 |
| Events | Usage Records | 使用记录 | 摘要优先，原始事件高级可见 |
| Repos | Code Repositories | 代码仓库 | repo/PR 使用状态 |
| Providers | AI Services | AI 服务 | 管理后台或高级上下文使用 |
| Credentials | Keys / Credentials | 密钥 / 凭据 | 默认隐藏在高级区 |
| Group | Available Group | 可用分组 | 仅在接入或管理语境解释 |
| Binding Status | Link Status | 关联状态 | 用状态说明解释 |
| Doctor | Diagnostics | 诊断 | 命令仍显示 `ae-cli doctor` |

代码和 API 命名不必同步改成本地化术语。实现应优先在组件文案和页面标题层完成术语转换，避免大范围重命名内部类型。

## Data Requirements

第一版优先复用现有 API：

- `/api/v1/efficiency/dashboard` 或现有 dashboard API：提供总览指标。
- `/api/v1/user/providers`：提供 provider/group/credential 和接入引导所需状态。
- tool usage events APIs：提供最近记录、summary、列表、详情。
- repo / PR usage APIs：提供 repo 接入、PR 使用快照和 freshness 状态。
- `/api/v1/auth/me`：提供当前用户身份。

如果缺少个人维度指标，前端应降级：

1. 显示可解释空状态。
2. 给出下一步入口。
3. 不伪造已探测的本机状态。
4. 不把全局公司指标误展示成“我的”指标。

若实现需要新增个人首页聚合 API，API 合同应保持薄层聚合，不改变底层 attribution 或 relay 模块职责。

## States And Error Handling

### Empty States

无数据必须说明原因候选和下一步：

- 未完成接入：进入 `My Setup` / `我的接入`。
- 已接入但无最近提交：等待下一次 commit 或手动 `ae-cli sync`。
- 有使用但未关联 repo：运行 `ae-cli doctor`，查看 `Usage Records` / `使用记录` 的关联状态。
- 后端暂时不可用：保留页面结构，显示刷新操作和错误说明。

### Loading States

首屏使用稳定高度的 skeleton 或 loading rows，避免卡片高度在加载完成后大幅跳动。

### Permission States

普通用户看不到 admin-only 操作。若直接访问 admin route，沿用当前路由守卫回到 `/`，并在可行时显示无权限说明。

### Advanced Disclosure

以下内容默认不在普通用户首屏铺开：

- API key 明文
- raw event payload
- provider base URL
- relay admin details
- deployment config
- LDAP config
- SCM credential payload

这些内容应通过 advanced / `高级`、details / `详情`、`Admin Console` / `管理后台` 或 admin-only 页面访问。

## Visual And Interaction Guidelines

1. 整体风格应是企业级、清晰、可扫描，而不是营销页或装饰型首页。
2. 颜色使用高对比浅色工作台为主，青蓝可作为主色，橙色只用于关键 action 或提醒，不做大面积渐变。
3. 卡片只用于指标、重复列表项、表单分组和详情抽屉；不要把整页 section 包成层层嵌套卡片。
4. 图标按钮使用现有图标体系或 lucide/heroicons；不要用文字胶囊替代常见图标操作。
5. 所有交互元素必须有可见 focus state，hover 不能成为唯一反馈。
6. 表格在移动端应转为摘要列表或横向可滚动容器，不能让文本重叠或挤出按钮。
7. 页面文案直接服务任务，不在应用内写“如何使用这个页面”的长说明。

## Testing

Frontend tests should cover:

1. `/` renders the personal home as the authenticated default route in both `en-US` and `zh-CN`.
2. Sidebar / mobile navigation show `My AI Usage` / `我的 AI 使用中心`, `My Setup` / `我的接入`, `Usage Records` / `使用记录`, and `Code Repositories` / `代码仓库`, and hide admin entries for non-admin users.
3. Admin users can still see `User Management` / `用户管理` and `Admin Console` / `管理后台`.
4. `/user` still renders Machine Setup, Per-Repo Setup, provider selection, group selection, credential actions, and recovery commands from the current checklist contract.
5. `/events` can render ordinary usage-record labels while preserving raw detail access.
6. Empty states render actionable guidance when personal metrics, events, providers, or repos are absent.
7. Error states do not collapse the page shell.
8. Navigation and key cards pass accessible label and keyboard focus checks.

Manual / visual verification:

- 375px phone viewport
- 768px tablet viewport
- 1024px laptop viewport
- 1440px desktop viewport

Recommended commands for frontend-only implementation:

```bash
cd frontend && pnpm test
```

If implementation touches shared route guards, API clients, backend aggregation, or CLI command construction, run the corresponding backend or `ae-cli` tests in addition to frontend tests.

## Documentation

Implementation must update `docs/architecture.md` to reflect:

- `/` as the company-wide personal AI usage center.
- `/user` as `My Setup` / `我的接入`, preserving the current CLI setup and provider/group credential contract.
- `/events` as user-facing usage records with advanced raw detail.
- admin-only `Admin Console` / `管理后台` / expert surfaces.
- responsive app shell responsibilities.
- `en-US` / `zh-CN` frontend language strategy and the boundary between localized UI copy and untranslated backend/runtime facts.

Historical specs should remain as design history unless a new implementation contract supersedes them.

## Rollout Notes

The recommended implementation order is:

1. Rename navigation labels and restructure app shell responsively.
2. Convert `/` into the personal usage center using existing data and honest empty states.
3. Reword `/user` into `My Setup` / `我的接入` while preserving its current CLI and credential behavior.
4. Reframe `/events` as `Usage Records` / `使用记录`, keeping raw details available.
5. Group admin settings under `Admin Console` / `管理后台`.
6. Update `docs/architecture.md` and frontend tests.

This is a single frontend-focused implementation plan if no backend aggregation API is added. If personal home metrics require new backend aggregation, split that API work into its own plan section before coding.
