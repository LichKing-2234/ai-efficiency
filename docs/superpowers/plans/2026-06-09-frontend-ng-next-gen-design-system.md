# Frontend NG Next-Gen Design System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild `frontend-ng` toward the reference design in `/Users/czhen/Downloads/ai-efficiency (1)/ai-efficiency-ng` while preserving real backend data, auth, routing, and i18n contracts.

**Architecture:** Treat the reference app as the visual and interaction source of truth, not as a data source. The TanStack Start app remains a BFF: browser code calls same-origin `/api/*`, server routes own cookie reads, Bearer injection, token refresh, and backend proxying. The migration lands in layers: tokens and shadcn primitives first, shell and command interactions second, then screen-by-screen rewrites using shared primitives.

**Tech Stack:** React 19, TanStack Start, TanStack Router, TanStack Query, Tailwind v4, shadcn-style source components, Radix primitives, lucide-react, Recharts/SVG charts, MiSans, Bun.

---

## Current Status

In progress. The foundation pass, shell, command palette, promoted `/usage` route, Overview, Usage, Events, Repos, Repo Detail, My Setup, Admin Users, and Settings have a first visual pass using real backend data and shared shadcn-style primitives. Static verification, tests, build, local server reachability, browser QA, commits, and push are complete for the current pass. Browser QA verifies authenticated desktop and 390px mobile content for `/`, `/usage`, `/events`, `/repos`, `/user`, `/admin/users`, and `/settings`, plus command palette open, language switching, and dark mode. The current iteration is deepening the reference match by extracting reusable SVG chart primitives, moving Usage Analytics off Recharts for the primary token trend/model distribution visuals, and aligning the Overview setup status card with a shared `Ring` progress primitive.

## File Structure

- Modify: `frontend-ng/src/styles.css` - global MiSans, warm-paper tokens, shadcn compatibility tokens, sidebar/chart tokens, layout utilities, motion utilities, CSS-grid table classes.
- Modify: `frontend-ng/src/components/ui/button.tsx` - normalize shadcn button variants to the reference primary/ghost/link/icon dimensions.
- Modify: `frontend-ng/src/components/ui/card.tsx` - normalize card radius, shadow, composition spacing, and hover/accent helper classes.
- Modify: `frontend-ng/src/components/ui/badge.tsx` - add reference tone aliases while preserving current `success`, `warning`, and `ai` usages.
- Modify: `frontend-ng/src/components/primitives/metric-card.tsx` - evolve into a KPI-capable primitive while preserving the existing `MetricCard` export for current screens.
- Create: `frontend-ng/src/components/primitives/charts.tsx` - shared reference-style SVG chart primitives (`Sparkline`, `SparkBars`, `Ring`, `StackedAreaChart`, `BarsH`).
- Test: `frontend-ng/src/components/primitives/charts.test.ts` - locks sparkline and stacked-area path calculations.
- Create: `frontend-ng/src/components/primitives/heatmap-grid.tsx` - shared reference-style 7x24 activity heatmap grid.
- Test: `frontend-ng/src/components/primitives/heatmap-grid.test.ts` - locks fixed grid sizing, intensity normalization, and out-of-range point handling.
- Create: `frontend-ng/src/components/primitives/command-step.tsx` - shared reference-style CLI command row with numbered steps, copy action, and copied feedback.
- Test: `frontend-ng/src/components/primitives/command-step.test.ts` - locks visible shell prompt formatting and clipboard text behavior.
- Create: `frontend-ng/src/components/primitives/field-list.tsx` - shared reference-style compact label/value field list for details and runtime summaries.
- Test: `frontend-ng/src/components/primitives/field-list.test.tsx` - locks mono/truncation field row variants.
- Create: `frontend-ng/src/components/primitives/inset-panel.tsx` - shared reference-style inset message/result panel for route-local notices and responses.
- Test: `frontend-ng/src/components/primitives/inset-panel.test.tsx` - locks muted and comfortable content variants.
- Create: `frontend-ng/src/components/primitives/credential-key-panel.tsx` - shared reference-style API key row with ready/missing visual states and action slots.
- Test: `frontend-ng/src/components/primitives/credential-key-panel.test.tsx` - locks masked ready and missing credential rendering.
- Create: `frontend-ng/src/features/home/home-state.ts` - shared Overview setup readiness derivation used by the setup status card.
- Test: `frontend-ng/src/features/home/home-state.test.ts` - locks account, AI access, repository, recent usage readiness progress calculations, and Overview live activity summaries.
- Create: `frontend-ng/src/components/primitives/tool-glyph.tsx` - reusable tool identity glyph for tables and detail headers.
- Create: `frontend-ng/src/components/primitives/segmented-control.tsx` - shared range/filter segmented control.
- Create: `frontend-ng/src/components/primitives/slide-over.tsx` - shared right-side inspect panel for events/repos detail flows.
- Create: `frontend-ng/src/components/command/command-palette.tsx` - global command palette using real routes/actions.
- Create: `frontend-ng/src/components/ui/command.tsx` - shadcn/cmdk command primitive used by the global command palette.
- Create: `frontend-ng/src/components/ui/dropdown-menu.tsx` - shadcn/Radix dropdown primitive used by the shell language selector.
- Modify: `frontend-ng/src/components/layout/navigation.ts` - add `/usage`, group nav per the reference, preserve admin gating.
- Modify: `frontend-ng/src/components/layout/app-shell.tsx` - collapsible/sidebar shell, top bar command trigger, language dropdown, live status, theme toggle.
- Create: `frontend-ng/src/routes/usage.tsx` - promoted Usage Analytics route.
- Create/Modify: `frontend-ng/src/features/user-usage/usage-page.tsx` - full usage analytics screen backed by existing user usage APIs where available.
- Modify: `frontend-ng/src/features/home/home-page.tsx` - reference-style overview with condensed usage and shared KPI primitives.
- Modify: `frontend-ng/src/features/events/events-page.tsx` - reference-style filters, CSS-grid table, tool glyphs, slide-over detail.
- Modify: `frontend-ng/src/features/repos/repos-page.tsx`, `frontend-ng/src/features/repos/repo-detail-page.tsx` - reference workbench styling while preserving mutations and route contracts.
- Modify: `frontend-ng/src/features/user-setup/user-page.tsx` - reference setup styling and reusable status primitives.
- Modify: `frontend-ng/src/features/admin-users/admin-users-page.tsx`, `frontend-ng/src/features/settings/settings-page.tsx` - admin styling pass using shared cards, fields, and grid tables.
- Modify: `frontend-ng/src/lib/i18n/messages.ts` - add route, command palette, usage analytics, and shell copy for `en-US` and `zh-CN`.
- Modify: `frontend-ng/src/routeTree.gen.ts` - regenerated route tree after adding `/usage`.
- Test: `frontend-ng/src/components/command/command-palette.test.ts` - locks admin command visibility.

## Guardrails

- Do not touch old `frontend/`.
- Do not commit `frontend-ng/.env.local`.
- Do not move browser API calls to real backend origins; keep same-origin `/api/*`.
- Do not replace real API data with the reference app's `AE.*` mock data.
- All visible screen copy goes through `frontend-ng/src/lib/i18n/messages.ts`.
- Use shadcn primitives and semantic tokens first; no one-off raw color palettes in feature pages.
- Admin routes remain gated by backend user role.

## Task 1: Foundation Tokens and Utilities

**Files:**
- Modify: `frontend-ng/src/styles.css`
- Test: `frontend-ng/src/lib/i18n/no-hardcoded-copy.test.ts`

- [x] **Step 1: Inspect reference tokens and current tokens**

  Read `/Users/czhen/Downloads/ai-efficiency (1)/ai-efficiency-ng/DESIGN_SPEC.md`, `/Users/czhen/Downloads/ai-efficiency (1)/ai-efficiency-ng/app.css`, and `frontend-ng/src/styles.css`.

- [x] **Step 2: Replace global tokens**

  Map the reference `--bg`, `--surface`, `--ink`, `--ai`, `--viz-*`, status, radius, elevation, sidebar, chart, and canonical shadcn variables into `frontend-ng/src/styles.css`.

- [x] **Step 3: Preserve compatibility aliases**

  Keep `--ae-*` aliases mapped onto the new token names so existing pages remain stable during incremental migration.

- [x] **Step 4: Add reusable utilities**

  Add `.tnum`, `.mono`, `.grid-paper`, `.kpi-grid`, `.split-2`, `.split-equal`, `.split-rail`, `.split-settings`, `.repo-workbench`, `.ae-table`, `.ae-thead`, `.ae-trow`, `.ae-trow-btn`, `.live-dot`, `.rise`, `.stagger`, and reduced-motion guards.

- [x] **Step 5: Run focused verification**

  Run:

  ```bash
  cd frontend-ng && bun run check
  ```

  Expected: TypeScript passes, or any failures are unrelated to CSS and documented before continuing.

## Task 2: Shared shadcn Primitive Upgrade

**Files:**
- Modify: `frontend-ng/src/components/ui/button.tsx`
- Modify: `frontend-ng/src/components/ui/card.tsx`
- Modify: `frontend-ng/src/components/ui/badge.tsx`
- Modify: `frontend-ng/src/components/primitives/metric-card.tsx`
- Create: `frontend-ng/src/components/primitives/tool-glyph.tsx`
- Create: `frontend-ng/src/components/primitives/segmented-control.tsx`
- Create: `frontend-ng/src/components/primitives/slide-over.tsx`

- [x] **Step 1: Upgrade Button variants**

  Align `default`, `outline`, `ghost`, `link`, `icon`, and `icon-sm` with the reference dimensions and semantic token styling.

- [x] **Step 2: Upgrade Card composition**

  Apply `--r-lg`, `--sh-sm`, reference spacing, and opt-in hover/accent class support while keeping current `CardHeader`, `CardContent`, and `CardFooter` APIs.

- [x] **Step 3: Upgrade Badge tone model**

  Add tone-compatible variants: `neutral`, `ai`, `pos`, `warn`, `neg`; preserve aliases `success -> pos` and `warning -> warn`.

- [x] **Step 4: Upgrade MetricCard into KPI primitive**

  Add icon tile, delta pill, accent mode, optional sparkline slot/data, and tabular number styling without breaking existing `MetricCard` callers.

  Current implementation uses the shared `Sparkline` from `frontend-ng/src/components/primitives/charts.tsx` so KPI spark rendering is not duplicated.

- [x] **Step 5: Add ToolGlyph**

  Implement reusable glyph colors for Claude, Codex, Kiro, and unknown tools using design tokens.

- [x] **Step 6: Add SegmentedControl**

  Implement shadcn-compatible segmented range/filter control using `ToggleGroup` semantics or a focused local primitive if the existing `ToggleGroup` API is too heavy for simple range selection.

- [x] **Step 7: Add SlideOver**

  Implement the right-anchored inspect panel with accessible title, backdrop close, Escape close, sticky header, and token-themed surface.

- [x] **Step 8: Run primitive verification**

  Run:

  ```bash
  cd frontend-ng && bun run check && bun test
  ```

## Task 3: Shell, Navigation, and Command Palette

**Files:**
- Modify: `frontend-ng/src/components/layout/navigation.ts`
- Modify: `frontend-ng/src/components/layout/app-shell.tsx`
- Create: `frontend-ng/src/components/command/command-palette.tsx`
- Modify: `frontend-ng/src/lib/i18n/messages.ts`

- [x] **Step 1: Add `/usage` nav item**

  Add Usage Analytics under Analyze between Overview and Usage Records.

- [x] **Step 2: Replace language toggle with dropdown**

  Use the existing i18n provider to set `en-US` or `zh-CN` through a menu-style control.

  Current implementation uses the shadcn/Radix `DropdownMenu`, `DropdownMenuRadioGroup`, and `DropdownMenuRadioItem` primitives instead of a custom positioned popover.

- [x] **Step 3: Implement collapsible desktop sidebar**

  Match expanded `--rail` width and collapsed icon rail width. Persist collapsed state locally without storing auth data.

- [x] **Step 4: Preserve mobile drawer behavior**

  Keep off-canvas navigation below the 920px breakpoint and close on nav selection.

  Follow-up audit implementation adds `frontend-ng/src/components/ui/sidebar.tsx` as the shell-level shadcn-style Sidebar primitive with provider, layout, header, content, group, menu, footer, rail, and inset slots. `AppShell` now composes the desktop rail from these slots instead of page-local sidebar markup, while preserving the existing cookie-backed collapsed state, role-gated nav visibility, active route styling, and tooltip labels.

  The same audit pass replaces the mobile fixed overlay with the existing shadcn/Radix `Sheet` primitive and keeps an accessible hidden `SheetTitle`. Focused TDD evidence: `bun test src/components/ui/sidebar.test.tsx` first failed because `./sidebar` did not exist, then passed after implementation. Current verification for this checkpoint: `bun test src/components/ui/sidebar.test.tsx`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` pass. Browser QA on `http://127.0.0.1:4317/` verified desktop `1280x720` with one sidebar provider, one sidebar, seven visible sidebar nav buttons, no Sheet, no error boundary, and zero horizontal overflow; mobile `390x844` verified the shadcn Sheet opens, Overview/Admin nav links are visible for the current admin user, no error boundary renders, and horizontal overflow remains zero.

- [x] **Step 5: Add command palette**

  Register `Ctrl/Cmd+K`, provide navigation commands and safe UI actions, and route using TanStack Router.

  Current implementation composes the shadcn/cmdk `Command` primitive inside the shared `Dialog`, with route commands grouped by i18n labels and admin commands hidden for non-admin users.

- [x] **Step 6: Remove duplicate page titles incrementally**

  Keep generic page titles in the top bar. Feature pages should start with content or page-specific toolbars; Overview may keep its hero.

  Follow-up audit implementation tightens `PageHeader variant='toolbar'` so it renders only a right-aligned action row and no page title or description copy. Empty toolbar headers now render `null`, which removes duplicate in-content description blocks from My Setup, User Management, and Admin Console while preserving the Repo Detail sync action. Focused TDD evidence: `bun test src/components/primitives/page.test.tsx` first failed because the toolbar variant still rendered description copy, then passed after implementation. Current verification for this checkpoint: `bun test src/components/primitives/page.test.tsx`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` pass. Browser QA on `http://127.0.0.1:4317/` verified `/user`, `/admin/users`, and `/settings` at desktop `1280x720` with zero duplicate page-level description matches, zero `main h1` nodes, no error boundary, and zero horizontal overflow. Local `/repos` currently has no repo detail link, so Repo Detail toolbar rendering remains covered by TypeScript/build rather than authenticated browser data.

  Follow-up toolbar API audit implementation adds the explicit `PageToolbar` primitive, migrates Repo Detail sync actions to it, and removes empty toolbar calls from My Setup, User Management, and Admin Console so feature pages no longer pass dead `title`/`description` props just to satisfy a hidden header API. Focused TDD evidence: `bun test src/components/primitives/page.test.tsx` first failed because `PageToolbar` was not exported, then passed after implementation. Current verification for this checkpoint: `bun test src/components/primitives/page.test.tsx`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` pass. Browser QA on `/user`, `/admin/users`, and `/settings` at desktop `1280x720` verified zero duplicate page-level descriptions, zero `main h1` nodes, zero empty toolbar-like rows, no error boundary, and zero horizontal overflow.

## Task 4: Usage Route and Overview Rewrite

**Files:**
- Create: `frontend-ng/src/routes/usage.tsx`
- Create/Modify: `frontend-ng/src/features/user-usage/usage-page.tsx`
- Modify: `frontend-ng/src/features/user-usage/user-usage-panel.tsx`
- Modify: `frontend-ng/src/features/home/home-page.tsx`
- Modify: `frontend-ng/src/lib/i18n/messages.ts`
- Modify: `frontend-ng/src/routeTree.gen.ts`

- [x] **Step 1: Promote Usage Analytics route**

  Add `/usage` backed by existing user usage APIs.

- [x] **Step 2: Keep Overview usage condensed**

  Preserve `UserUsagePanel embedded` behavior or replace it with a condensed component backed by the same API data.

- [x] **Step 3: Rewrite Overview hero and KPI strip**

  Match the reference warm-paper hero, setup status, recent usage, and KPI visual treatment using real dashboard/events/provider data.

- [x] **Step 4: Fix interpolation regressions**

  Verify no literal `{identity}`, `{role}`, `{source}`, or `{range}` placeholders render in the UI.

- [x] **Step 5: Replace Usage primary charts with shared SVG primitives**

  Replace the Recharts token trend on Usage Analytics with the shared `StackedAreaChart`, add `BarsH` above the model table, add the shared `HeatmapGrid` for the reference activity heatmap section, and keep all data sourced from the existing `/api/user/usage/dashboard` response.

  Current evidence: `bun run check`, `bun test`, `bun run build`, and browser QA on `/usage` passed. The local dev account returns `configured=false`, so browser QA verifies the configured-empty state, no Recharts DOM, no runtime error, no visible button overflow, and no horizontal overflow at `1280px` and `390px`; data-state chart and heatmap rendering are covered by TypeScript/build, `charts.test.ts`, `heatmap-grid.test.ts`, and `user-usage-state.test.ts`.

  Follow-up section-header audit implementation migrates Usage Analytics token trend, model distribution, and activity heatmap cards to the shared `SectionCardHeader` primitive while preserving the existing backend-driven chart/table/heatmap rendering and range-specific description copy.

  Follow-up section-header audit evidence: `section-card-header.test.tsx` was extended for className passthrough, `user-usage-state.test.ts`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed after migration. `agent-browser` reverified `/usage` in the authenticated session at `1280x720`: no error boundary text and no body horizontal overflow. The local account currently renders the configured-empty state, so visible chart card headers remain covered by primitive tests, TypeScript, and build until configured usage data is available.

- [x] **Step 6: Align Overview setup progress with shared Ring**

  Extract Overview setup readiness into `home-state.ts`, render the setup status card with the shared `Ring` primitive, migrate recent usage into reference-style live activity rows with `ToolGlyph`, and keep checklist labels/statuses backed by real dashboard, provider, and event data.

  Follow-up audit implementation adds the shared `ChecklistRow` primitive for setup readiness rows. Overview now uses it for account, AI access, repository reporting, and recent usage status rows instead of a page-local `StatusLine` surface.

  Current evidence: `home-state.test.ts`, `bun run check`, `bun test`, `bun run build`, `git diff --check`, and browser QA on `/` passed. `agent-browser` verified authenticated desktop `1280x720` and mobile `390x844` states with a likely ring SVG, visible `1/4` setup progress, recent usage header, live dot, "View all records" action, no visible button overflow, no error boundary, and no horizontal overflow. The local account currently has no recent events, so rendered activity-row data is covered by `home-state.test.ts`.

  Follow-up audit evidence: `checklist-row.test.tsx` was added with a red-green cycle, `home-state.test.ts` and `bun run check` passed after migrating Overview setup status rows, and browser QA reverified `/` at desktop and mobile widths.

  Follow-up recent-usage audit implementation adds the shared `UsageActivityRow` primitive and migrates Overview recent usage rows away from page-local row markup while preserving the same backend event summary helper, status badges, tool glyph, token/request/credit metadata, and i18n labels.

  Follow-up recent-usage audit evidence: `usage-activity-row.test.tsx` was added with a red-green cycle, `home-state.test.ts` and `bun run check` passed after migrating Overview recent usage rows. `agent-browser` reverified `/` at `1280x720` and `390x844`: four setup checklist rows, recent usage header, no error boundary text, no horizontal overflow, and no visible button/link overflow. The local account still has no recent event rows, so rendered `UsageActivityRow` data-state markup is covered by primitive tests and TypeScript/build until event data is available.

  Follow-up usage table audit implementation migrates the Usage Analytics model distribution table to the shared `DataGrid` primitive while keeping the existing `/api/user/usage/dashboard` response mapping, `BarsH` chart, model ordering, and numeric formatting unchanged.

  Follow-up usage table audit evidence: `data-grid.test.tsx`, `user-usage-state.test.ts`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed after the migration. `agent-browser` reverified `/usage` at `1280x720` and `390x844`: no error boundary text, no body horizontal overflow, and no visible over-wide interactive controls. The local usage payload currently has no model distribution rows, so visible model-table rendering is covered by primitive tests and TypeScript/build until model data is available.

## Task 5: Events, Repos, Setup, and Admin Screen Pass

**Files:**
- Modify: `frontend-ng/src/features/events/events-page.tsx`
- Modify: `frontend-ng/src/features/repos/repos-page.tsx`
- Modify: `frontend-ng/src/features/repos/repo-detail-page.tsx`
- Modify: `frontend-ng/src/features/user-setup/user-page.tsx`
- Modify: `frontend-ng/src/features/admin-users/admin-users-page.tsx`
- Modify: `frontend-ng/src/features/settings/settings-page.tsx`
- Modify: `frontend-ng/src/lib/i18n/messages.ts`

- [x] **Step 1: Events**

  Implement reference filters, KPI cards, CSS-grid table rows, tool glyphs, token mini-bars, and SlideOver detail.

  Current implementation uses the shared `SegmentedControl` primitive for Tool and Binding filters instead of page-local chip buttons, with `event-filters.ts` helpers preserving the visible `all` value and backend empty filter contract.

  Current implementation also uses the shared `SearchField` primitive for the event text filter, composing the shadcn `InputGroup` parts instead of page-local input wrapper markup.

  Current implementation also uses the shared `InfoTile` primitive for SlideOver token/request/credit stat tiles, including compact numeric and AI accent variants.

  Current implementation also uses the shared `FieldList` primitive for SlideOver session and advanced metadata fields instead of a page-local field-row helper.

  Follow-up audit implementation adds the shared `OptionList` primitive for admin user search candidates and the shared `CodeBlock` primitive for advanced raw payload display, removing two remaining page-local styled surfaces without changing filter, detail, or backend API contracts.

  Follow-up detail audit implementation adds the shared `SectionEyebrow` primitive and migrates Event SlideOver inspect section labels for token breakdown, session, and matched PRs away from the page-local `SectionLabel` helper.

  Follow-up filter audit implementation adds the shared `LabeledSegmentedControl` primitive and migrates the Events Tool and Binding segmented filters away from the page-local `FilterSegment` helper while preserving the existing `SegmentedControl` behavior and backend empty-filter mapping.

  Follow-up row audit implementation adds the shared `TokenMeter` primitive and migrates the Events row token mini-bar away from page-local markup while preserving compact token formatting, minimum non-zero visibility, and the existing Events row grid contract.

  Follow-up advanced-data audit implementation adds the shared `AdvancedDataPanel` primitive and migrates the Events SlideOver advanced data accordion away from page-local composition while preserving FieldList rows, optional raw payload CodeBlock rendering, default-collapsed behavior, and admin-only field selection.

  Current evidence: `event-filters.test.ts`, `search-field.test.tsx`, `info-tile.test.tsx`, `field-list.test.tsx`, `bun run check`, browser QA on `/events`, and full verification passed. `agent-browser` verified two radiogroups, seven radio items, Tool and Binding filters, one shared search input, no error boundary, no visible interactive overflow, and no horizontal overflow at `1280x720` and `390x844`. The current local account has no event rows, so SlideOver data rendering is covered by TypeScript/build and primitive tests while browser QA verifies the route empty/data-light state.

  Follow-up audit evidence: `option-list.test.tsx` and `code-block.test.tsx` were added with a red-green cycle. `bun run check` passed after migrating `/events`. `agent-browser` reverified `/events` at `1280x720` and `390x844`: no error boundary, no horizontal overflow, and no mobile button overflow. The local account still has no event rows or user search candidate rows, so visible `OptionList` and `CodeBlock` instances are covered by primitive tests and TypeScript/build.

  Follow-up detail audit evidence: `section-eyebrow.test.tsx` was added with a red-green cycle, `event-filters.test.ts` and `bun run check` passed after removing the page-local section label helper, and browser QA reverified the `/events` data-light route state. The current local account still has no event rows, so SlideOver section label rendering is covered by primitive tests and TypeScript/build until event detail data is available.

  Follow-up filter audit evidence: `labeled-segmented-control.test.tsx` was added with a red-green cycle, `event-filters.test.ts` and `bun run check` passed after removing the page-local filter segment helper. `agent-browser` verified `/events` at `1280x720` and `390x844` with two shared labeled segmented controls, two radiogroups, seven radio items, no error boundary text, no horizontal overflow, and no visible button overflow. Full verification passed with `bun test`, `bun run check`, `bun run build`, and `git diff --check`.

  Follow-up row audit evidence: `token-meter.test.tsx` was added with a red-green cycle, `event-filters.test.ts` and `bun run check` passed after migrating Events row token bars. `agent-browser` reverified `/events` at `1280x720` and `390x844`: no error boundary text, no horizontal overflow, no visible button/link overflow, and two filter radiogroups. The local account still has no event rows, so visible `TokenMeter` row rendering is covered by primitive tests and TypeScript/build until event data is available.

  Follow-up advanced-data audit evidence: `advanced-data-panel.test.tsx` was added with a red-green cycle, `event-filters.test.ts` and `bun run check` passed after migrating the Events advanced data accordion. `agent-browser` reverified `/events` at `1280x720` and `390x844`: no error boundary text, no horizontal overflow, no visible button/link overflow, and two filter radiogroups. The local account still has no event rows, so visible SlideOver `AdvancedDataPanel` rendering is covered by primitive tests and TypeScript/build until event detail data is available.

  Follow-up table audit implementation migrates the Events list table and clickable event rows to the shared `DataGrid` primitive, keeping the existing filter/query behavior, `TokenMeter`, row click detail mutation, empty state, and backend event payload contract unchanged.

  Follow-up table audit evidence: `data-grid.test.tsx`, `event-filters.test.ts`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed after the migration. `agent-browser` reverified `/events` at `1280x720` and `390x844`: one shared data grid, no error boundary text, no body horizontal overflow, and no visible over-wide interactive controls. The local account still has no event rows, so visible clickable-row rendering is covered by primitive tests and TypeScript/build until event data is available.

  Follow-up token-breakdown audit implementation adds the shared `TokenBreakdown` primitive and migrates the Events SlideOver stacked token bar plus legend rows away from page-local markup while preserving the same backend token fields, color mapping, and locale-aware number formatting.

  Follow-up token-breakdown audit evidence: `token-breakdown.test.tsx`, `event-filters.test.ts`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed after the migration. `agent-browser` reverified `/events` at `1280x720` and `390x844`: no error boundary text, no body horizontal overflow, and no visible over-wide interactive controls. The local account still has no event rows, so visible SlideOver `TokenBreakdown` rendering is covered by primitive tests and TypeScript/build until event detail data is available.

  Follow-up linked-record audit implementation adds the shared `LinkedRecordList` and `LinkedRecordItem` primitives and migrates Events matched PR links away from the page-local `pr-link` class while preserving the existing matched PR label helper, external link target, and `noreferrer` behavior.

  Follow-up linked-record audit evidence: `linked-record-list.test.tsx` was added with a red-green cycle, `event-filters.test.ts`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed after migrating matched PR links. `agent-browser` reverified `/events` at `1280x720` and `390x844`: one shared data grid, no error boundary text, and no body horizontal overflow. The local account still has no event detail rows, so visible `LinkedRecordList` rendering is covered by primitive tests and TypeScript/build until matched PR data is available.

- [x] **Step 2: Repositories**

  Implement reference workbench styling while keeping add, delete, auto-bind, provider selection, webhook repair, and navigation behavior intact.

  Current implementation uses the shared `SectionNav` primitive for the repository scope rail, preserving provider tabs, URL-backed scope selection, and per-scope repository counts through `buildScopeNavItems`.

  Follow-up table audit implementation adds the shared `DataGrid`, `DataGridHeader`, and `DataGridRow` primitives and migrates the Repositories workbench table away from page-local `ae-table`/`ae-thead`/`ae-trow` shell markup while preserving the same grid columns, overflow behavior, row content, delete confirmation controls, and route links.

  Current evidence: `repos-state.test.ts`, `section-nav.test.tsx`, `bun run check`, and browser QA on `/repos` passed. The local account currently returns the empty inventory state, so browser QA verifies authenticated desktop and mobile empty states with no error boundary or horizontal overflow while data-state scope rail mapping is covered by tests and build.

  Follow-up table audit evidence: `data-grid.test.tsx` was added with a red-green cycle, `repos-state.test.ts` and `bun run check` passed after migrating the Repositories workbench table. `agent-browser` reverified `/repos` at `1280x720` and `390x844`: no error boundary text, no horizontal overflow, and no visible button/link overflow. The local account still returns no repository inventory rows, so visible `DataGrid` rendering is covered by primitive tests and TypeScript/build until repo data is available.

  Follow-up add-dialog audit implementation migrates the parsed repository preview panel in the Add Repo dialog to the shared `InsetPanel` and `FieldList` primitives while preserving URL parsing, provider matching, clone protocol toggles, SSH host behavior, and clone URL preview.

  Follow-up add-dialog audit evidence: `repos-state.test.ts`, `field-list.test.tsx`, `inset-panel.test.tsx`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed after the migration. `agent-browser` reverified `/repos` at `1280x720` and `390x844`, then opened Add repo at desktop width and filled `https://github.com/example/repo`; the dialog rendered one shared inset panel, one shared field list, two field items, and the parsed `example/repo` preview with no error boundary or body horizontal overflow.

  Follow-up action-group audit implementation migrates the Repositories table delete controls and Add Repo dialog footer actions to the shared `ActionGroup` primitive while preserving delete confirmation, create disabled state, and dialog behavior.

  Follow-up action-group audit evidence: `action-group.test.tsx`, `repos-state.test.ts`, `repo-detail-state.test.ts`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed. `agent-browser` reverified `/repos` at `1280x720` and `390x844`: no error boundary text and no body horizontal overflow. The local account still has no repository rows, so visible table row action groups are covered by primitive tests and TypeScript/build; the Add Repo dialog renders one shared `ActionGroup` footer at desktop and mobile widths with zero dialog overflow.

  Follow-up pagination audit implementation adds the shared `CardPagerFooter` primitive and migrates the Repositories page footer away from page-local `CardFooter` pagination markup while preserving the same page count copy, total count, previous/next handlers, and disabled states.

  Follow-up pagination audit evidence: `card-pager-footer.test.tsx` was added with a red-green cycle, `repos-state.test.ts`, `repo-detail-state.test.ts`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed after migration. `agent-browser` reverified `/repos` in the authenticated session at `1280x720` and `390x844`: one shared card pager footer rendered, Previous/Next remained disabled for the local empty page, no error boundary text rendered, and body horizontal overflow stayed zero.

- [x] **Step 3: Repository detail**

  Re-skin SCM binding, usage snapshots, webhook status, and PR sync panels with shared cards/tables.

  Current implementation uses the shared `InfoTile` primitive for latest sync job status, phase, fetched, and processed stat tiles instead of a page-local `DetailStat` helper.

  Current implementation also uses the shared `InsetPanel` primitive for sync-disabled and latest-job usage progress notices.

  Follow-up audit implementation adds the shared `UsageSummaryPanel` primitive for expanded PR usage summaries. The expanded PR detail path now composes `InfoTile` and `InsetPanel` for input/output/cache/reasoning/request/credit metrics, status, refreshed-at copy, and refresh/resolve actions instead of page-local metric divs.

  Current evidence: `repo-detail-state.test.ts`, `info-tile.test.tsx`, `inset-panel.test.tsx`, and `bun run check` passed. The current local account exposes no repository detail links in `/repos`, so browser QA verifies the authenticated repository route empty/data-light state with no error boundary or horizontal overflow while repo detail stat and notice rendering is covered by TypeScript/build.

  Follow-up audit evidence: `usage-summary-panel.test.tsx` was added with a red-green cycle, `inset-panel.test.tsx` still passes after the slot extension, and `bun run check` passed after migrating the expanded PR detail summary. The local account still exposes no repository detail rows, so expanded PR detail rendering is covered by primitive tests and TypeScript/build until data is available for browser expansion.

  Follow-up table audit implementation migrates the PR list table shell to the shared `DataGrid` primitive while leaving the expanded commit snapshot shadcn `Table` intact as a nested detail table.

  Follow-up table audit evidence: `data-grid.test.tsx`, `repo-detail-state.test.ts`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed after the migration. `agent-browser` reverified the repository area through `/repos` at `1280x720` and `390x844`: no error boundary text, no body horizontal overflow, and no visible over-wide interactive controls. The local account still exposes no repository detail links, so visible PR-list rendering is covered by primitive tests and TypeScript/build until repository detail data is available.

  Follow-up snapshot-table audit implementation extends the shared `DataGridRow` primitive with a `fullWidth` state for nested empty rows and migrates expanded PR commit usage snapshots away from the remaining shadcn `Table` wrapper while preserving backend snapshot ordering, freshness lookup, status badges, numeric formatting, and empty-state copy.

  Follow-up snapshot-table audit evidence: `data-grid.test.tsx`, `repo-detail-state.test.ts`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed after migrating the expanded commit snapshot grid. `agent-browser` reverified `/repos` at `1280x720` and `390x844`: no error boundary text, no body horizontal overflow, and no rendered `table` elements in the current route state. The current local account still exposes no repository detail links, so route-level browser coverage remains on `/repos` data-light states while snapshot grid rendering is covered by primitive tests and TypeScript/build until repository detail data is available.

  Follow-up action-group audit implementation migrates Repository Detail PR row details/refresh controls to the shared `ActionGroup` primitive while preserving PR expansion state and usage refresh mutation behavior.

  Follow-up action-group audit evidence: `action-group.test.tsx`, `repo-detail-state.test.ts`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed. The current local account still exposes no repository detail links in `/repos`, so visible PR row action rendering remains covered by primitive tests, TypeScript, and build until repository detail data is available.

  Follow-up pagination audit implementation migrates Repository Detail PR pagination to the shared `CardPagerFooter` primitive while preserving the same PR page summary, Previous/Next handlers, and fetch-aware disabled states.

  Follow-up pagination audit evidence: `card-pager-footer.test.tsx`, `repo-detail-state.test.ts`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed. The current local account still exposes no repository detail links, so visible PR pager rendering remains covered by primitive tests, TypeScript, and build until repository detail data is available.

- [x] **Step 4: My Setup**

  Re-skin provider credential setup, status/progress, and key actions with shared field/card primitives.

  Current implementation uses the shared `SelectableCard` primitive for provider selection cards, so active/inactive card states are no longer page-local button styling.

  Current implementation uses the shared `InfoTile` primitive for setup group/platform/credential stat tiles instead of page-local inset tile markup.

  Current implementation uses the shared `CommandStep` primitive for CLI onboarding rows so installer, login, discover, hooks, init, doctor, and Windows commands share reference-style copy affordances instead of page-local command markup.

  Follow-up command-accordion audit implementation adds the shared `CommandAccordion` primitive and migrates the My Setup Windows installer section away from page-local Accordion border/spacing markup while preserving the existing PowerShell install command, copy affordance, and default-collapsed behavior.

  Current implementation uses the shared `InsetPanel` primitive for provider messages and provider-test response blocks.

  Current implementation uses the shared `CredentialKeyPanel` primitive for the API key row, keeping the existing create, regenerate, reveal, copy, toast, and mutation behavior in the page.

  Current evidence: `command-step.test.ts`, `selectable-card.test.tsx`, `provider-button.test.tsx`, `info-tile.test.tsx`, `inset-panel.test.tsx`, `credential-key-panel.test.tsx`, `bun run check`, and browser QA on `/user` passed. `agent-browser` verified the CLI workflow, six numbered command steps, copy affordances, no error boundary, and no horizontal overflow at `1280x720` and `390x844`. The latest `/user` checks rendered one shared inset panel at desktop and mobile widths; the mobile panel width stayed inside the 390px viewport. The local account currently returns no provider rows/selected group, so browser QA verifies the stable empty/data-light path while provider selection and API key panel rendering are covered by `provider-button.test.tsx`, `credential-key-panel.test.tsx`, TypeScript, and build. The mobile run also caught and fixed a hidden command-row width issue by applying `min-width: 0` to split-layout children and the command-step row; the largest visible mobile button width is now `285px` in a `390px` viewport.

  Follow-up command-accordion audit evidence: `command-accordion.test.tsx` was added with a red-green cycle, `command-step.test.ts`, `user-setup-state.test.ts`, `provider-button.test.tsx`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed after migrating the Windows installer section. `agent-browser` reverified `/user` at `1280x720` and `390x844`: one shared command accordion rendered and expanded, no error boundary text, and no body horizontal overflow. The current local account still has no provider rows, so provider data rendering remains covered by existing state and primitive tests.

  Follow-up section-header audit implementation migrates the My Setup CLI workflow and provider test cards to the shared `SectionCardHeader` primitive while preserving the Terminal icon treatment, provider test copy, and all existing command/provider-test behavior. Focused evidence: `section-card-header.test.tsx`, `user-setup-state.test.ts`, and `provider-button.test.tsx` passed alongside `bun test`, `bun run check`, `bun run build`, and `git diff --check`. `agent-browser` reverified `/user` in the authenticated session at `1280x720` and `390x844`: four cards and four shared card headers rendered, no error boundary text, no body horizontal overflow, and no visible over-wide interactive controls. The local account still has no provider rows, so provider data rendering remains covered by existing state and primitive tests.

- [x] **Step 5: Admin Users and Settings**

  Re-skin admin pages with shared grid tables and forms while preserving role guards and backend mutation contracts.

  Current implementation uses the shared `SearchField` primitive for the Admin Users table filter, matching Events search behavior and shadcn `InputGroup` composition.

  Current implementation also uses the shared `SectionNav` primitive for the Settings section rail, replacing page-local button markup while preserving URL-backed section selection.

  Current implementation also uses the shared `InfoTile` primitive for deployment-runtime stat tiles.

  Current implementation also uses the shared `FieldList` primitive for deployment-runtime current/mode/commit summary fields.

  Current implementation also uses the shared `InsetPanel` primitive for Admin Users subscription job messages and plaintext reveal confirmation panels.

  Current evidence: `search-field.test.tsx`, `section-nav.test.tsx`, `field-list.test.tsx`, `inset-panel.test.tsx`, `bun run check`, `bun test`, `bun run build`, and browser QA on `/admin/users` and `/settings` passed. `agent-browser` verified the authenticated Admin Users shared search input at `1280x720` and `390x844`, the Settings shared section rail with five items and one current item at `1280x720` and `390x844`, no error boundary, and no horizontal overflow. The latest Settings deployment-runtime check rendered one shared field list, three field rows, and three info tiles at `1280x720` and `390x844`; the mobile field row width stayed within the 390px viewport. The latest Admin Users route check had no active job/reveal panel data, so the route empty/data-light state is browser-covered while inset notice rendering is covered by primitive tests and TypeScript/build.

  Follow-up OAuth audit implementation adds the shared `AuthInfoPanel` primitive and migrates `/oauth/authorize` plus `/oauth/device` signed-in context strips away from page-local muted boxes while preserving existing OAuth payload, redirect, and login-guard behavior.

  Follow-up OAuth audit evidence: `auth-info-panel.test.tsx` was added with a red-green cycle, `auth-flow-state.test.ts` and `bun run check` passed after migration, and browser QA reverified `/oauth/device`.

  Follow-up table audit implementation migrates the Admin Users table plus Settings relay, SCM, and advanced credential tables to the shared `DataGrid` primitive while preserving selection, search, pagination, reveal confirmation, CRUD dialogs, confirm actions, and backend mutation payloads.

  Follow-up table audit evidence: `data-grid.test.tsx`, `admin-users-state.test.ts`, `settings-payloads.test.ts`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed after the migration. `agent-browser` reverified `/admin/users` and `/settings` at `1280x720` and `390x844`: shared data grids rendered where backend data exists, no error boundary text, no body horizontal overflow, and no visible over-wide interactive controls.

  Follow-up identity audit implementation adds the shared `IdentityAvatar` primitive and migrates Admin Users table initials away from page-local avatar markup while preserving the same username/email fallback behavior.

  Follow-up identity audit evidence: `identity-avatar.test.tsx`, `admin-users-state.test.ts`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed after the migration. `agent-browser` reverified `/admin/users` at `1280x720` and `390x844`: one shared identity avatar rendered in the current data state, no error boundary text, no body horizontal overflow, and no visible over-wide interactive controls.

  Follow-up job-result audit implementation adds the shared `JobResultList` primitive and migrates Admin Users subscription job result rows away from page-local bordered list markup while preserving the existing active/latest job polling, result cap, identity fallback, row messages, and status badges.

  Follow-up job-result audit evidence: `job-result-list.test.tsx` was added with a red-green cycle, `admin-users-state.test.ts`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed after migrating the result list. `agent-browser` reverified `/admin/users` at `1280x720` and `390x844`: no error boundary text and no body horizontal overflow. The current local account has no active job results, so visible `JobResultList` rendering is covered by primitive tests and TypeScript/build until job result data is available.

  Follow-up section-header audit implementation adds the shared `SectionCardHeader` primitive and migrates the Settings AI Services, Code Platforms, and Advanced Credentials cards away from repeated page-local `CardHeader` title/action/description markup while preserving the existing add-dialog actions, icons, and i18n copy.

  Follow-up section-header audit evidence: `section-card-header.test.tsx` was added with a red-green cycle, `bun test src/components/primitives/section-card-header.test.tsx`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed after migration. `agent-browser` reverified `/settings` at `1280x720` and `390x844` in the authenticated session: one shared card header rendered for the active AI Services section, the Add action remained visible, no error boundary text rendered, and body horizontal overflow stayed zero.

  Follow-up auth/admin header audit implementation migrates the login card, OAuth authorize/device card shell, and Admin Users subscription-management card to `SectionCardHeader` while preserving existing form state, OAuth payload/redirect behavior, admin job controls, and i18n copy. Current evidence: `section-card-header.test.tsx`, `auth-flow-state.test.ts`, `admin-users-state.test.ts`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed after migration. `agent-browser` reverified `/login` at `390x844`, `/oauth/authorize` at `1280x720`, `/oauth/device` at `390x844`, and `/admin/users` at `1280x720` plus `390x844`: each route rendered the expected shared card header, no error boundary text, no body horizontal overflow, and no visible over-wide interactive controls.

  Follow-up section-header audit implementation also migrates the Settings Organization Login and Deployment & Runtime cards to `SectionCardHeader`, completing the standard title/description header path for every Settings section card.

  Follow-up section-header audit evidence: `section-card-header.test.tsx`, `settings-payloads.test.ts`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed after the migration. `agent-browser` reverified `/settings?section=organization-login` and `/settings?section=deployment-runtime` in the authenticated session at `1280x720` and `390x844`: each route rendered one shared card header with the expected section title, no error boundary text, and zero body horizontal overflow.

  Follow-up action-group audit implementation adds the shared `ActionGroup` primitive and migrates Settings relay, SCM, and credential table row actions plus their create/edit dialog footer actions away from repeated page-local `flex justify-end gap-2` wrappers while preserving all button variants, confirm actions, disabled states, and backend mutations.

  Follow-up action-group audit evidence: `action-group.test.tsx` was added with a red-green cycle, `bun test src/components/primitives/action-group.test.tsx`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed after migration. `agent-browser` reverified `/settings` in the authenticated session: desktop `1280x720` and mobile `390x844` render without error boundary or body horizontal overflow; the local AI Services table currently has no rows, so visible table row action groups are covered by primitive tests and TypeScript/build, while the Add Relay Provider dialog renders one shared `ActionGroup` footer at both desktop and mobile widths with zero dialog overflow.

## Task 6: Verification, Visual QA, Commit, and Push

**Files:**
- Modify: this plan file as steps complete.

- [x] **Step 1: Static verification**

  Run:

  ```bash
  cd frontend-ng && bun test
  cd frontend-ng && bun run check
  cd frontend-ng && bun run build
  git diff --check
  ```

- [x] **Step 2: Local server**

  Run:

  ```bash
  cd frontend-ng && bun run dev -- --host 127.0.0.1 --port 4317
  ```

- [x] **Step 3: Browser visual verification**

  Use `agent-browser` against `http://127.0.0.1:4317/` at desktop and mobile widths. Verify shell, Overview, Usage, Events, Repos, User, Admin, dark mode, language dropdown, command palette, and slide-over.

  Current evidence from this run:
  - Authenticated desktop route loop passed for `/`, `/usage`, `/events`, `/repos`, `/user`, `/admin/users`, and `/settings` at `1280x720`: no error boundary, no leaked `{current}` / `{total}` / `{ready}` placeholders, no duplicate in-content page title, no horizontal overflow, and no button overflow.
  - Authenticated mobile route loop passed for the same routes at `390x844` using `agent-browser set viewport 390 844`: no error boundary, no leaked placeholders, no duplicate in-content page title, no horizontal overflow, and no button overflow.
  - Overview interaction checks passed for command palette open, shadcn/Radix language dropdown radio menu open, language switch to `zh-CN`, and dark-mode toggle.
  - The admin/settings SSR guard crash was fixed by moving admin role redirects into authenticated client pages.
  - Usage Analytics no longer renders Recharts for the primary token trend; `agent-browser` confirmed `.recharts-wrapper` count is `0`, `/usage` has no horizontal overflow at `1280px` or `390px`, and the local configured-empty state remains usable.
  - Usage Analytics now has the shared `HeatmapGrid` activity section in the configured data path; `heatmap-grid.test.ts` and `user-usage-state.test.ts` cover the 7x24 grid and trend-to-heatmap mapping while local browser QA confirms the configured-empty path remains stable at desktop and mobile widths.
  - Overview setup status now uses the shared `Ring` primitive and derived readiness helper; `agent-browser` confirmed ring presence, visible `1/4` progress, no error boundary, and no horizontal overflow at `1280x720` and `390x844`.
  - Follow-up Overview setup audit added `ChecklistRow` for account, AI access, repository reporting, and recent usage status rows, preserving the same readiness conditions and fix links.
  - Overview recent usage now uses reference-style live activity rows with `ToolGlyph`, binding badges, token/request/credit metadata, and a route link to all records; local browser QA verified the empty state header/action at desktop and mobile widths.
  - My Setup CLI workflow now uses the shared `CommandStep` primitive; `agent-browser` confirmed six numbered command rows, copy affordances, no error boundary, no horizontal overflow, and no visible button overflow at `1280x720` and `390x844`.
  - Follow-up Events audit added `OptionList` and `CodeBlock` primitives and reverified `/events` at `1280x720` and `390x844`; the local account remained data-light, so route stability is browser-covered while candidate-list and raw-payload rendering are covered by primitive tests and TypeScript/build.
  - Follow-up Events detail audit added `SectionEyebrow` for SlideOver inspect section labels; the local account remained data-light, so route stability is browser-covered while detail label rendering is covered by primitive tests and TypeScript/build.
  - Follow-up Repository Detail audit added `UsageSummaryPanel` for expanded PR usage summaries; the local account still exposes no repository detail rows, so route-level browser coverage remains on `/repos` data-light states while primitive tests and TypeScript/build cover the expanded data path.
  - Follow-up OAuth audit added `AuthInfoPanel` for signed-in context strips on `/oauth/authorize` and `/oauth/device`; `/oauth/device` was browser-verified while OAuth payload/redirect helpers remain covered by `auth-flow-state.test.ts`.
  - Follow-up auth/admin header audit reused `SectionCardHeader` on `/login`, `/oauth/authorize`, `/oauth/device`, and `/admin/users`; `agent-browser` confirmed the expected shared card headers, no error boundary, no horizontal overflow, and no visible over-wide controls across desktop/mobile coverage for those routes.

- [x] **Step 4: Commit logical chunks**

  Use Conventional Commits, for example:

  ```bash
  git add docs/superpowers/plans/2026-06-09-frontend-ng-next-gen-design-system.md frontend-ng/src
  git commit -m "refactor(frontend): align next-gen design foundation"
  ```

- [x] **Step 5: Push branch**

  Run:

  ```bash
  git push origin codex/refactor/ng-frontend
  ```

## Self-Review

- Spec coverage: The plan covers tokens, shadcn primitives, shell, `/usage`, all existing primary routes, i18n, auth/proxy guardrails, browser verification, commit, and push.
- Placeholder scan: No `TBD`, `TODO`, or undefined owner placeholders are present.
- Type consistency: Existing exports are preserved where current pages depend on them; new primitives are introduced as additive building blocks before route rewrites.
