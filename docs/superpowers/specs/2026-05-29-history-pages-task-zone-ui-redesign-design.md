# History Pages Task-Zone UI Redesign

**Date:** 2026-05-29
**Status:** Implemented frontend task-zone redesign
**Scope:** `frontend/src/router/`, `frontend/src/components/`, `frontend/src/views/`, `frontend/src/api/`, `frontend/src/stores/`, `frontend/src/types/`, `frontend/src/__tests__/`, `docs/architecture.md`
**Related:**
- [2026-05-29-company-wide-user-home-ux-design.md](./2026-05-29-company-wide-user-home-ux-design.md)
- [2026-05-28-pr-sync-job-progress-usage-freshness-design.md](./2026-05-28-pr-sync-job-progress-usage-freshness-design.md)
- [2026-05-26-user-cli-setup-checklist-design.md](./2026-05-26-user-cli-setup-checklist-design.md)
- [2026-05-21-user-page-cli-self-serve-design.md](./2026-05-21-user-page-cli-self-serve-design.md)
- [2026-05-19-ae-cli-deterministic-tool-configuration-design.md](./2026-05-19-ae-cli-deterministic-tool-configuration-design.md)
- [2026-04-15-oauth-device-login-design.md](./2026-04-15-oauth-device-login-design.md)
- [2026-03-24-oauth-cli-login-design.md](./2026-03-24-oauth-cli-login-design.md)
- [docs/ui-review/history-pages-task-zone-redesign-review.html](../../ui-review/history-pages-task-zone-redesign-review.html)
- [docs/ui-review/company-wide-onboarding-review.html](../../ui-review/company-wide-onboarding-review.html)

## Implementation Notes

- The implemented frontend keeps the existing route contract and adds a small `frontend/src/i18n.ts` layer for touched shell, page, auth, settings task, admin users, and repository-detail primary labels.
- `/settings` now uses task-zone section components under `frontend/src/components/settings/`: `AIServiceSettings`, `CodePlatformSettings`, `OrganizationLoginSettings`, `DeploymentRuntimeSettings`, and `AdvancedCredentialSettings`.
- `/settings` task sections and primary add/edit dialogs now switch consistently between `en-US` and `zh-CN` while preserving provider names, URLs, and technical enum values as product terms.
- `/admin/users` keeps plaintext relay password copy behind an explicit confirmation, uses bilingual primary labels, and renders mobile user cards instead of a squeezed table.
- `/repos/:id` keeps PR/commit details as advanced disclosure while localizing the repository health, SCM binding, PR summary, filters, and detail controls.
- Deployment apply update, rollback, and restart controls require an explicit confirmation step before invoking the deployment API.
- `/oauth/device` shares `AuthShell` and displays the signed-in account before approve or deny.

## Spec Relationship

- 本文扩展 `2026-05-29-company-wide-user-home-ux-design.md`。前一份 spec 解决登录后默认首页和普通用户入口；本文覆盖历史页面的全量重组。
- 本文采用评审中确认的 **B: 任务分区重设计**。保留现有 URL 和 API 合同，把历史页面按用户任务重新组织为：`我的工作`、`代码与 PR`、`管理`、`登录授权`。
- 本文不改变 `ae-cli login`、`ae-cli discover`、`ae-cli hooks`、`ae-cli init`、`ae-cli sync`、`ae-cli doctor` 的命令语义。
- 本文不改变 relay provider、SCM provider、credential、tool usage event、PR usage snapshot、OAuth device flow 的后端数据合同。
- 本文允许前端组件拆分、页面重排、文案本地化、空状态和高级披露调整。若实现需要新增个人维度或团队维度聚合 API，必须补充单独的数据合同扩展。
- 本文不回写历史 specs。实现完成后，`docs/architecture.md` 必须同步反映新的前端工作区、页面边界和管理员入口。

## Problem

当前系统已经具备首页、用户接入、使用记录、仓库、仓库详情、用户管理、管理后台、登录和 OAuth 授权能力，但这些页面仍按技术资源堆叠：

1. 普通使用者、研发用户、团队负责人和管理员都从同一层导航进入技术细节。
2. `/repos` 更像 repo config 表，不像代码仓库接入和数据新鲜度工作台。
3. `/repos/:id` 有完整 PR usage 和 commit freshness 数据，但默认表格密度高，先暴露字段而不是结论。
4. `/settings` 同时承载 Credentials、SCM Providers、Relay Providers、Deployment、LDAP 和多个弹窗，页面超过 1000 行，维护和理解成本都高。
5. `/admin/users` 直接展示 relay auth password，并把复制密文和复制明文放在默认表格里，风险操作分层不足。
6. `/login`、`/oauth/authorize`、`/oauth/device` 的视觉和语言不统一，像独立 demo，而不是同一套 Auth Experience。
7. 现有中英混合和硬编码文案使“全公司用户”体验不一致。

## Goals

1. 保留现有 URL 合同，同时把导航重组为任务分区：
   - `My Work` / `我的工作`
   - `Code & PR` / `代码与 PR`
   - `Administration` / `管理`
   - Auth public pages / 登录授权页
2. 让普通用户默认只看到个人状态、接入状态、使用记录和下一步动作。
3. 让研发用户可以进入代码仓库、PR 使用新鲜度、CLI 命令和诊断细节，但默认先看到结论。
4. 让管理员能力集中在 Admin Console 中，并按任务拆成 `AI Services`、`Code Platforms`、`Organization & Login`、`Deployment & Runtime`、`Advanced Credentials`。
5. 把风险操作明确分层：API key 明文、relay password 明文、raw event payload、deployment restart、delete 等默认不在首屏直接铺开。
6. 统一登录、OAuth authorize、device login 的视觉、文案、语言切换和错误状态。
7. 建立可分阶段落地的 frontend-only 重构路径。第一阶段优先不新增后端 API。

## Non-Goals

1. 不拆分后端服务。本项目仍是模块化单体。
2. 不新增 team dashboard、组织树、部门权限模型或复杂 audience routing。
3. 不改变后端 attribution、checkpoint、PR sync、relay、SCM、OAuth 的事实链。
4. 不在浏览器中执行本机 CLI 命令，不引入 local daemon、local proxy 或 browser-to-local bridge。
5. 不把所有内部 enum、API field、command output、provider name、model name、branch name、commit SHA 翻译掉。
6. 不在第一版重做整套视觉品牌系统。第一版使用现有 Tailwind + Vue 组件模式，提升信息架构、文案、状态和响应式体验。
7. 不默认新增真实用户样例、真实公司邮箱、真实 token、真实 group 名或真实密码到测试和文档。

## Reviewed Alternatives

### Option A: Local Page Refresh

保留当前导航和页面结构，只修改标题、文案、空状态、表格列和移动端样式。

优点：

- 改动最小。
- 可快速上线。
- 测试更新较少。

缺点：

- `/settings` 仍然臃肿。
- 普通用户、研发和管理员心智仍混在一起。
- 后续会继续以补丁方式演进。

### Option B: Task-Zone Redesign

保留现有 URL 和 API 合同，但把历史页面重新组织成任务分区。默认简单，细节进入高级区。

优点：

- 最符合“全公司用户”的默认心智。
- 不破坏已有深链和 API。
- 可分阶段落地。
- 能把 `/settings` 和 `/admin/users` 的安全风险显式分层。

缺点：

- 需要系统性改前端组件、测试和文案。
- 部分页面需要从表格优先改为摘要优先。

### Option C: Dual Portal Rebuild

拆成 Employee Portal 和 Admin Portal 两套 shell，普通用户和管理员进入不同产品体验。

优点：

- 长期边界最清晰。
- 普通用户体验最干净。

缺点：

- 改动最大。
- 需要新增路由迁移策略。
- 本轮 review 和实现成本过高。

### Decision

采用 **Option B: Task-Zone Redesign**。

## Information Architecture

### Route Contract

第一版保留现有路由：

| Route | Current Component | New Zone | New Page Name | Audience |
| --- | --- | --- | --- | --- |
| `/` | `DashboardView` | My Work | My AI Usage / 我的 AI 使用中心 | All users |
| `/user` | `UserView` | My Work | My Setup / 我的接入 | All users, developers |
| `/events` | `EventsView` | My Work | Usage Records / 使用记录 | All users, admins |
| `/repos` | `RepoListView` | Code & PR | Code Repositories / 代码仓库 | Developers, leads, admins |
| `/repos/:id` | `RepoDetailView` | Code & PR | Repository Detail / 仓库详情 | Developers, leads, admins |
| `/admin/users` | `AdminUsersView` | Administration | Users & Access / 用户与接入 | Admins |
| `/settings` | `SettingsView` | Administration | Admin Console / 管理后台 | Admins |
| `/login` | `LoginView` | Auth | Sign In / 登录 | Public |
| `/oauth/authorize` | `AuthorizePage` | Auth | App Authorization / 应用授权 | Authenticated users |
| `/oauth/device` | `DevicePage` | Auth | Device Login / 设备登录 | Authenticated users |

### Navigation

Desktop sidebar:

1. `My Work` / `我的工作`
   - My AI Usage / 我的 AI 使用中心
   - My Setup / 我的接入
   - Usage Records / 使用记录
2. `Code & PR` / `代码与 PR`
   - Code Repositories / 代码仓库
   - PR Usage Status / PR 使用状态（第一版可不新增路由；作为 `/repos/:id` 或未来聚合入口）
3. `Administration` / `管理`，admin only
   - Users & Access / 用户与接入
   - Admin Console / 管理后台

Mobile shell:

- 顶部栏展示产品名、当前工作区标题、菜单、语言切换。
- 抽屉导航使用相同分区。
- 常用按钮保持至少 44px 触控高度。

### Language

- `en-US` 是源语言。
- `zh-CN` 是公司内 review 和中文优先用户 locale。
- 用户显式选择优先，第一版可继续使用 `localStorage`。
- 未触达的大型 admin form 可以分阶段本地化，但同一页面首屏和主要任务不能中英混杂。

## Page Design

### `/` My AI Usage

Purpose:

- 回答“我是谁、是否接好、有没有数据、下一步做什么”。

Required sections:

1. Account and visibility status.
2. 7-day or platform-visible metrics:
   - repositories visible
   - usage records
   - linked-to-code records
   - connected tools from real provider credential state
3. Setup status:
   - signed in
   - AI access
   - data visibility
   - code link status
4. Next steps:
   - setup this machine
   - connect repository
   - check recent records
5. Recent records summary:
   - time
   - tool
   - repo
   - link status
   - token usage summary

Rules:

- 不得用 workflow count 伪造 connected tools。
- 不得声称已探测本机 CLI 状态，除非数据来自后端事实。
- 数据不可用时显示原因候选和下一步。

### `/user` My Setup

Purpose:

- 让用户完成 AI 工具和代码仓库接入，不要求先理解 provider/group/credential 模型。

Required sections:

1. Account card:
   - username
   - email
   - role
   - auth source
2. AI access card:
   - provider display name
   - group display name
   - platform
   - credential readiness
3. Command reference:
   - install CLI
   - login
   - discover
   - hooks enable
   - repo init
   - doctor
4. Advanced sections:
   - API key reveal/copy/regenerate
   - provider test
   - manual sync
   - hook upload status

Rules:

- `Provider & Group Credential`、`Credential state`、`Current Secret` 不作为默认首屏标题。
- 命令块可复制，且保持原始命令不翻译。
- Regenerate 和 reveal 必须有明确风险提示。

### `/events` Usage Records

Purpose:

- 默认以可读记录解释 AI 工具使用情况；保留排障能力。

Default list columns:

- time
- tool
- repository
- code link status
- token usage
- credits
- requests
- user, admin only

Filters:

- time range
- tool
- code link status
- search
- user search, admin only

Detail drawer:

1. Summary:
   - tool
   - repository
   - observed time
   - user, admin only
2. Token usage.
3. Code link:
   - status
   - commit
   - captured at
   - matched PRs
4. Advanced event data:
   - workspace id
   - tool session id
   - tool event id
   - dedupe key, admin only
   - raw source, admin only
   - raw payload, admin only

Rules:

- Raw event fields are not default table columns.
- Plain English/Chinese labels are used for `bound` and `unbound`.
- Empty state explains no matching usage records.

### `/repos` Code Repositories

Purpose:

- 展示哪些代码仓库已接入、哪些需要绑定、哪些数据新鲜度异常。

Required sections:

1. Repository health summary:
   - total repositories
   - bound repositories
   - unbound repositories
   - repositories with stale or failed PR usage
2. Filters:
   - binding status
   - SCM provider
   - organization/project
   - search
3. Grouped repository list:
   - provider / organization group
   - repo name
   - binding state
   - usage freshness summary
   - last sync or latest signal
   - primary action
4. Add repository dialog:
   - remains admin/developer oriented
   - explains why SCM provider is needed

Current implementation note:

- As of 2026-06-07, `/repos` uses a scoped inventory workbench instead of an inline grouped table: `GET /api/v1/repos/inventory` summarizes Platform -> org/project scopes, while `GET /api/v1/repos` accepts `scm_provider_id`, `scope`, and `binding_state` so table pagination applies only to the selected platform scope.
   - continues parsing GitHub and Bitbucket browse URLs

Rules:

- `Unbound` must explain whether the repo was auto-discovered from usage sync.
- Delete is a secondary or danger action, not the visual primary action.

### `/repos/:id` Repository Detail

Purpose:

- 先给仓库结论，再允许用户进入 PR 和 commit 级 usage detail。

Required sections:

1. Header:
   - repo name
   - full name
   - binding state
   - default branch
   - last sync / freshness
2. Admin binding panel:
   - visible only to admins
   - explains unbound reason
   - save/clear binding actions
3. PR usage summary:
   - total PRs
   - PRs with AI usage
   - pending upload
   - no checkpoint
   - refresh failed
4. PR table default columns:
   - title
   - author
   - PR status
   - usage status
   - token usage summary
   - refreshed at
   - actions
5. Advanced PR detail:
   - input/output/cache/reasoning split
   - commit snapshots
   - freshness reason
   - raw commit SHA

Rules:

- High-density token columns are not all default table columns.
- Freshness reason should be visible as help text, not hidden in raw data only.

### `/admin/users` Users & Access

Purpose:

- 管理本地用户、relay identity 映射和接入风险操作。

Default list columns:

- user
- role
- auth source
- relay mapping status
- access status summary
- created/updated
- actions

Risk operations:

- copy encrypted relay password
- reveal/copy plaintext relay password

Rules:

- Relay auth password is not a default raw column.
- Plaintext copy requires explicit confirmation and warning text.
- Use masked display by default.
- Tests and fixtures must use `alice@example.com`, `bob@example.org`, `test-password` style placeholders.

### `/settings` Admin Console

Purpose:

- 管理系统级配置。第一版可以继续使用 `/settings` 路由，但组件结构必须按任务拆分。

Task tabs or sections:

1. `AI Services` / `AI 服务配置`
   - Relay Providers
   - group routing summary, if available
   - AI provider credentials
2. `Code Platforms` / `代码平台配置`
   - SCM Providers
   - clone protocol
   - API credential binding
   - webhook guidance, if available
3. `Organization & Login` / `组织与登录`
   - LDAP configuration
   - auth options summary
4. `Deployment & Runtime` / `部署与运行`
   - deployment version
   - check update
   - apply update
   - rollback
   - restart
5. `Advanced Credentials` / `高级凭据`
   - generic credential store
   - add/edit credential dialogs

Rules:

- `SettingsView.vue` should be split into smaller components before large behavior additions.
- Deployment restart, rollback and delete actions are risk actions with clear confirmation.
- Credentials display summaries, not raw secret payload.

### `/login`, `/oauth/authorize`, `/oauth/device`

Purpose:

- Provide one consistent Auth Experience.

Login page:

- product name
- language toggle
- recommended login method
- LDAP/SSO selection only if both are available and relevant
- dev login only when enabled
- redirect explanation when coming from OAuth or device flow

OAuth authorize:

- client identity
- requested access
- signed-in account
- deny/authorize actions
- redirect failure state

Device login:

- user code input
- account confirmation
- approve/deny
- terminal return state

Rules:

- 这些页面即使不使用 `AppLayout`，也应共享认证页面的视觉基础组件。
- Avoid mixed Chinese-only and English-only hardcoded content.

## Data Requirements

Phase 1 should reuse existing APIs:

- `/api/v1/auth/me`
- `/api/v1/efficiency/dashboard`
- `/api/v1/user/providers`
- tool usage event APIs
- repo APIs
- PR sync and PR usage APIs
- admin users APIs
- SCM provider, credential, relay provider, LDAP, deployment APIs

Permitted frontend-only derivations:

- connected tools count from unique platforms with existing user group credentials.
- repository binding summary from `RepoConfig.binding_state`.
- usage record token total from existing token fields.
- PR usage summary from existing `usage_status` and token fields.

Not permitted:

- inventing local CLI state.
- representing global metrics as personal metrics without labeling.
- inferring secret availability from masked display alone.

Potential future API extensions:

- `GET /api/v1/me/usage-summary`
- `GET /api/v1/repos/health-summary`
- `GET /api/v1/admin/access-summary`

These are future candidates only. They are not required for Phase 1.

## States And Error Handling

### Loading

- Use stable-height cards or skeleton rows.
- Do not shift core layout dramatically after load.

### Empty

Each empty state explains:

1. what is missing,
2. why it may be missing,
3. what the user can do next.

Examples:

- no usage records: finish setup, commit code, or run `ae-cli doctor`.
- no repositories: add repository or wait for auto-discovered usage sync.
- no provider credentials: open My Setup or ask admin to configure AI services.

### Error

- Backend errors remain precise.
- User-facing messages explain the task impact.
- Admin-only raw errors can be visible in advanced details.

### Permission

- Admin-only navigation remains hidden for regular users.
- Direct access to admin routes continues to redirect or block.
- If adding an access-denied state, it should explain which role is needed.

## Security And Risk UX

The following are risk actions:

- reveal API key
- regenerate API key
- copy relay plaintext password
- copy encrypted password
- delete repository
- delete SCM provider
- delete relay provider
- delete credential
- restart service
- rollback deployment
- apply deployment update

Requirements:

- Risk actions are visually secondary or danger-styled.
- Plaintext secret operations require confirmation.
- Destructive operations require explicit confirmation.
- Default table views should not show full secrets or raw sensitive payloads.
- Test data and docs must not introduce real users, real company emails, real passwords, real tokens, real API keys or real group names.

## Component Architecture

Recommended frontend refactors:

1. Shared shell:
   - `AppLayout`
   - `AppSidebar`
   - route title and mobile drawer support
2. Shared UI primitives:
   - `PageHeader`
   - `MetricCard`
   - `StatusBadge`
   - `EmptyState`
   - `RiskActionButton`
   - `AdvancedDetails`
3. Settings components:
   - `AdminConsoleView`
   - `AIServiceSettings`
   - `CodePlatformSettings`
   - `OrganizationLoginSettings`
   - `DeploymentRuntimeSettings`
   - `AdvancedCredentialSettings`
4. Repo components:
   - `RepoHealthSummary`
   - `RepoGroupedList`
   - `RepoPRSummary`
   - `PRUsageTable`
   - `PRAdvancedDetails`
5. Auth components:
   - `AuthShell`
   - `LanguageToggle`
   - `AuthError`

Component extraction should preserve behavior first. Avoid route or API rewrites in the same step unless explicitly required.

## Testing

Required frontend tests:

- Navigation:
  - regular users see My Work and Code & PR but not Administration.
  - admins see Administration grouped separately.
  - Chinese and English labels switch consistently.
- `/`:
  - connected tools uses user provider credentials, not workflow count.
  - no-data states show next steps.
- `/user`:
  - user-facing labels are default.
  - advanced key operations remain available.
  - command strings remain unchanged.
- `/events`:
  - default table uses readable summary columns.
  - raw event fields are only in advanced details.
  - admin user search remains admin-only.
- `/repos`:
  - binding filter works.
  - health summary is derived correctly.
  - delete remains secondary danger action.
- `/repos/:id`:
  - PR usage summary is visible.
  - advanced PR detail still exposes commit snapshots.
  - admin binding panel remains admin-only.
- `/admin/users`:
  - plaintext password is not default visible.
  - reveal/copy plaintext requires explicit action.
- `/settings`:
  - task tabs/sections render independently.
  - existing create/update/delete flows remain covered.
- Auth pages:
  - login options respect auth options API.
  - OAuth authorize requires sign-in.
  - device login redirects unauthenticated users.

Verification commands:

```bash
cd frontend && pnpm test
cd frontend && pnpm build
cd frontend && pnpm run test:e2e:role
```

`test:e2e:role` is environment sensitive and may be reported separately from unit tests.

## Release Plan

### Phase 1: Shell And My Work

Scope:

- navigation zones
- `AppLayout` responsive refinements
- `/` personal homepage
- `/user` task-first setup page
- `/events` summary-first usage records
- minimal i18n coverage for touched labels

Expected outcome:

- regular company users have a coherent default path.
- no backend contract changes.

### Phase 2: Code And PR

Scope:

- `/repos` health summary and grouped list
- `/repos/:id` conclusion-first repository detail
- PR usage status and freshness summary
- advanced PR/commit detail disclosure

Expected outcome:

- developers and leads can quickly identify repo setup and usage freshness issues.

### Phase 3: Admin Console

Scope:

- split `SettingsView` into task components
- reorganize admin users into Users & Access
- risk action UX for credentials, relay passwords and deployment operations

Expected outcome:

- admins get a maintainable control surface with safer default views.

### Phase 4: Auth Experience

Scope:

- `/login`
- `/oauth/authorize`
- `/oauth/device`
- shared AuthShell and language support

Expected outcome:

- login and authorization flows look like one product and support non-technical users better.

## Architecture Documentation

When implementation completes:

- Update `docs/architecture.md` to describe:
  - new frontend task zones,
  - route-to-zone mapping,
  - admin-only UI boundaries,
  - frontend-only derivations versus backend truth surfaces.
- Do not rewrite older historical specs.
- If Phase 2 or Phase 3 introduces new backend aggregation endpoints, add a new spec or update this spec with a Data Contract addendum.

## Open Questions For Review

1. Should Phase 2 include a new aggregate `PR Usage Status` route, or keep it inside `/repos` and `/repos/:id` for the first implementation?
2. Should `/admin/users` remain a separate route, or become a tab inside `/settings` while preserving redirect compatibility?
3. Should Auth pages be included in the first implementation plan, or remain Phase 4 as proposed?
