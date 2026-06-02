# Company-Wide Usability Hardening Design

**Date:** 2026-05-30
**Status:** Implemented and self-tested on 2026-05-30; follow-up UI polish self-tested on 2026-06-01
**Scope:** `frontend/src/components/`, `frontend/src/views/`, `frontend/src/i18n.ts`, `frontend/src/__tests__/`, `frontend/e2e_role_test.py`, `docs/ui-review/`
**Related:**
- [2026-05-29-history-pages-task-zone-ui-redesign-design.md](./2026-05-29-history-pages-task-zone-ui-redesign-design.md)
- [2026-05-29-company-wide-user-home-ux-design.md](./2026-05-29-company-wide-user-home-ux-design.md)
- [2026-05-26-user-cli-setup-checklist-design.md](./2026-05-26-user-cli-setup-checklist-design.md)
- [2026-05-21-global-tool-usage-events-page-design.md](./2026-05-21-global-tool-usage-events-page-design.md)
- [docs/ui-review/company-wide-usability-hardening-review.html](../../ui-review/company-wide-usability-hardening-review.html)

## Spec Relationship

- 本文是 `2026-05-29-history-pages-task-zone-ui-redesign-design.md` 的下一轮可用性加固 spec。前一份 spec 已完成任务分区和多页面重组；本文不推翻该信息架构，而是补齐全公司用户可用性、移动端、双语术语和可访问性验收标准。
- 本文不改变后端 API、路由合同、权限模型、relay / SCM / attribution / checkpoint 数据事实链。
- 本文不要求浏览器执行本机 CLI 命令，不引入 local proxy、daemon 或 browser-to-local bridge。
- 本文把 `en-US` 和 `zh-CN` 视为生产界面合同。中文 HTML review 只是评审材料，不是中文-only 约束。

## Problem

当前任务分区版已经比原技术后台更接近全公司使用场景，但按独立 UI/UX 标准审查后仍有四类明显风险：

1. **移动端不是完整体验。** `/user` 在 390px 宽度存在页面级横向溢出；`/events`、`/repos`、`/repos/:id`、`/settings` 多处依赖横向表格，关键操作会被藏到屏幕右侧。
2. **普通用户仍要理解技术模型。** `provider`、`group`、`credential`、`API key`、`hooks`、`doctor`、`SCM Provider` 等概念仍是默认暴露对象，非研发用户难以判断自己是否需要操作。
3. **中英文术语未完全治理。** 中文界面仍混有 `Group:`、`Platform:`、`API KEY`、`PROMPT`、`BASE URL`、`Path`、`Locator` 等默认文案；这会降低公司内非技术用户信任感。
4. **弹层和详情抽屉可访问性不足。** 移动菜单、详情抽屉、添加/编辑弹窗缺少 dialog 语义、焦点管理、Escape 关闭和键盘等价操作。

## Goals

1. 让 375px 到 1440px 的关键页面都能作为完整工作流使用，不依赖页面级横向滚动。
2. 把普通用户路径改成“状态 -> 下一步 -> 可复制动作 -> 高级细节”，而不是“技术资源 -> 命令清单”。
3. 为 `en-US` / `zh-CN` 建立明确术语合同：哪些必须翻译，哪些作为产品名、命令、enum 或标识符保留。
4. 所有弹层、抽屉、移动菜单和表格行详情都满足基本键盘和 screen reader 语义。
5. 让页面状态可恢复：筛选条件、tab、展开详情和语言选择不应只停留在内存状态。
6. 保持当前路线低风险：第一轮优先 frontend-only，不新增后端数据合同。

## Non-Goals

1. 不重做品牌视觉系统，不引入新的设计系统依赖。
2. 不新增团队看板、组织架构、部门权限或复杂角色选择器。
3. 不把 provider name、model name、repo name、branch name、commit SHA、API enum、CLI command 翻译掉。
4. 不在本轮把所有管理员高级表单改成向导；管理员页只处理可用性和安全风险优先项。
5. 不变更 OAuth、relay、SCM、PR sync、attribution 的后端合同。

## Design Principles

1. **Conclusion first.** 页面先给结论，再给证据。用户应先看到“已完成 / 还差一步 / 需要管理员处理”，再看到 raw fields。
2. **Task language over resource language.** 导航和主标题使用任务语言，例如“设置这台机器”“检查最近记录”“处理待绑定仓库”；技术资源名放在二级说明或高级区。
3. **Mobile is not table shrink.** 移动端使用卡片、摘要行、底部操作或详情页，而不是把桌面表格压进 390px。
4. **Risk actions require intent.** 明文密钥、删除、重启、回滚、重新生成等必须有上下文、二次确认和取消路径。
5. **Bilingual by contract.** 每个被触达的用户可见文案必须有 `en-US` 和 `zh-CN`；不能继续新增硬编码中英文。
6. **Keyboard parity.** 鼠标可达的核心动作必须键盘可达。

## Responsive Requirements

### Global Shell

- At `375px`, content viewport must not exceed document width.
- Mobile top bar must show menu, product title, and language control without overlapping.
- Mobile drawer:
  - has `role="dialog"` and `aria-modal="true"`;
  - moves focus into the drawer when opened;
  - returns focus to the menu button when closed;
  - closes on Escape and overlay click.
- Desktop sidebar may remain fixed width, but main content must not create horizontal page scroll.

### Tables And Lists

- Any table with more than four columns must define a mobile alternative:
  - card list, or
  - priority columns plus detail disclosure, or
  - a dedicated detail page/drawer.
- Horizontal scrolling inside a table container is acceptable only for advanced/raw detail sections, not for primary task lists.
- Primary row actions must remain visible at `375px`.

## Language And Terminology Contract

### Must Localize

- Navigation labels.
- Page titles and subtitles.
- Button labels.
- Empty, loading, error, success, warning states.
- Form labels and help text.
- Dialog titles and risk explanations.
- Status labels shown to normal users.

### Preserve As Technical Terms

- CLI commands and flags.
- API paths and route examples.
- Provider display names supplied by data.
- Model names.
- Repository names, branch names, commit SHAs.
- Backend enum values when displayed only in advanced/raw sections.

### Rename For Users

| Raw/Current Term | en-US User Term | zh-CN User Term | Notes |
| --- | --- | --- | --- |
| Provider | AI access source | AI 接入来源 | Use `provider` only in admin or command context |
| Group | Access group | 接入组 | Avoid raw `Group:` prefix in Chinese UI |
| Credential | Access key | 接入凭据 | Use `credential` only in admin store |
| API Key | AI access key | AI 接入密钥 | Risk copy must explain impact |
| SCM Provider | Code platform binding | 代码平台绑定 | Admin technical forms may keep SCM in secondary text |
| Events | Usage records | 使用记录 | Raw event detail goes under advanced |
| Binding | Code link | 代码关联 | Better for ordinary users |
| Hooks | Automatic Git reporting | 自动 Git 上报 | Command can keep `hooks` |
| Doctor | Setup diagnosis | 接入诊断 | Command can keep `ae-cli doctor` |

## Page Requirements

### `/` My AI Usage

Required changes:

- Replace abstract metrics with user-actionable status:
  - Signed in
  - AI access ready
  - Tool configuration ready/unknown
  - Code reporting active/waiting
  - Recent usage available/no data
- Each negative or unknown state must include one primary next action.
- Recent activity should show actual recent records when available; if no events API data is used, avoid implying activity exists.

Acceptance:

- A non-technical user can answer “Do I need to do anything?” from the first viewport.
- No `--` value appears without explanatory text.

### `/user` My Setup

Required changes:

- Convert the page from command reference into a setup flow:
  1. Account verified.
  2. AI access ready.
  3. Install CLI.
  4. Sign in on this machine.
  5. Configure tools.
  6. Enable automatic Git reporting.
  7. Connect a repository.
  8. Run setup diagnosis.
- Each step has:
  - status,
  - short user-language explanation,
  - primary action or copy command,
  - advanced detail disclosure.
- Keep raw commands copyable. Commands remain untranslated.
- API key reveal/copy/regenerate stays behind explicit confirmation.

Acceptance:

- At 390px, no section overflows the page.
- The command list is not the dominant first viewport.
- A non-technical user sees whether they should ask a developer/admin or proceed themselves.

### `/events` Usage Records

Required changes:

- Desktop may keep table; mobile must use record cards.
- Filters should collapse into a filter panel on mobile.
- Record detail drawer must be a real dialog with focus management.
- Clickable table rows need keyboard equivalents.
- URL query should capture filters: `from`, `to`, `tool`, `binding_status`, `q`, `user_id`, `limit`, `offset`.

Acceptance:

- At 390px, time, tool, repository/code link, token usage, and detail action are readable without horizontal page scroll.
- Detail drawer closes with Escape and returns focus.

### `/repos` Code Repositories

Required changes:

- Mobile repository list uses cards grouped by code platform/org.
- Each repo card shows:
  - repo name,
  - binding status,
  - active/inactive status,
  - primary action,
  - secondary risk action.
- “Review needs binding” is a filter state and must be reflected in URL.
- Add repository dialog must be a real dialog and focus the repo URL input on open.

Acceptance:

- At 390px, the unbound repo action is visible without horizontal scroll.
- Delete is visually secondary and requires confirmation.

### `/repos/:id` Repository Detail

Required changes:

- PR usage summary remains conclusion-first.
- PR list uses cards on mobile and table on desktop.
- Commit snapshot table remains advanced-only.
- Sync progress and failure states use localized, task-level messaging; raw phase counters may remain secondary.

Acceptance:

- At 390px, PR title, status, AI usage status, token usage, refreshed time, and details action are readable.
- Sync PR disabled state explains why when repo is unbound.

### `/settings` Admin Console

Required changes:

- Section tab state should be in URL query/hash.
- Task-zone tabs must use `role="tab"`, `aria-selected`, and keyboard arrow support.
- Admin tables either provide mobile cards or explicitly hide behind an admin desktop-only notice with a supported minimum width.
- Add/edit dialogs must use dialog semantics, focus trap, Escape close, and initial focus.
- Validation errors must be localized.

Acceptance:

- At 390px, admin users can still inspect primary status and perform safe actions.
- Risk actions continue to require confirmation.

### `/admin/users` Users & Access

Required changes:

- Keep mobile cards.
- Desktop and mobile plaintext copy confirmation must explain scope and audit expectation.
- Search/page size/page should be reflected in URL query.
- Copy result feedback should be announced to screen readers.

Acceptance:

- At 390px, user card shows identity, mapping, access status, and actions without overflow.

## Accessibility Requirements

- Every interactive control has visible focus style.
- Icon-only buttons must have accessible names and tooltips if meaning is not obvious.
- Dialogs:
  - `role="dialog"` or native `<dialog>`;
  - `aria-modal="true"`;
  - labelled by a visible heading;
  - focus moves on open and returns on close;
  - Escape closes unless a destructive operation is in progress.
- Tab panels:
  - use `role="tablist"`, `role="tab"`, `role="tabpanel"`;
  - active tab has `aria-selected="true"`;
  - keyboard arrows switch tabs.
- Loading, success, and error messages use `aria-live` where appropriate.
- Heading hierarchy should remain sequential within each page.

## Data And URL Requirements

No new backend API is required for Phase 1.

Frontend should persist/recover:

- Locale: `localStorage` remains acceptable.
- `/events` filters and pagination: URL query.
- `/repos` binding filter: URL query.
- `/settings` active section: query or hash.
- `/admin/users` search and pagination: URL query.

Do not persist secrets in URL, localStorage, sessionStorage, or test fixtures.

## Implementation Phases

### Phase 1: Usability Hardening Without Backend Changes

- Fix page-level mobile overflow.
- Add mobile card layouts for `/events`, `/repos`, `/repos/:id`, `/settings` tables where primary actions exist.
- Add dialog/focus semantics for mobile menu, event detail, repo add dialog, settings dialogs.
- Localize touched hardcoded labels and validation messages.
- Add URL state for filters and tabs.

### Phase 2: Guided Setup

- Redesign `/user` into a setup progress flow.
- Add copy buttons and per-step status.
- Keep commands in advanced sections.
- Add more explicit “ask admin / ask developer” recovery states.

### Phase 3: Evidence And Admin Polish

- Improve recent activity on `/` using event data when available.
- Add screen-reader announcements for copy/result states.
- Tighten admin mobile cards and risk-action copy.

## Testing And Review

Required automated checks:

- `cd frontend && pnpm test`
- `cd frontend && pnpm run test:e2e:role`

Required visual review:

- Desktop: 1280x800 and 1440x900.
- Mobile: 375x812 and 390x900.
- Pages: `/`, `/user`, `/events`, `/repos`, `/repos/:id`, `/settings`, `/admin/users`, `/login`, `/oauth/device`.
- Validate no page-level horizontal overflow at mobile widths.

Required accessibility smoke:

- Keyboard-only navigation through mobile menu, filters, dialogs, table/card detail actions.
- Escape closes dialogs/drawers.
- Focus returns after close.
- Screen reader names exist for icon-only or row action controls.

## Open Questions

1. `/user` 是否应为非研发用户显示“联系研发/管理员完成接入”的默认 CTA，还是仍默认给 CLI 命令？
2. 管理后台是否需要支持完整移动端操作，还是定义为桌面优先、移动端只读/轻操作？
3. 首页 recent activity 是否应新增轻量 API，避免从事件页 API 重复拉取列表？
4. URL 状态是否需要兼容旧分享链接的默认时间范围行为？
