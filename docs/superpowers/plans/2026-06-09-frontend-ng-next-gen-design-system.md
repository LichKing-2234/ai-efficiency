# Frontend NG Next-Gen Design System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild `frontend-ng` toward the reference design in `/Users/czhen/Downloads/ai-efficiency (1)/ai-efficiency-ng` while preserving real backend data, auth, routing, and i18n contracts.

**Architecture:** Treat the reference app as the visual and interaction source of truth, not as a data source. The TanStack Start app remains a BFF: browser code calls same-origin `/api/*`, server routes own cookie reads, Bearer injection, token refresh, and backend proxying. The migration lands in layers: tokens and shadcn primitives first, shell and command interactions second, then screen-by-screen rewrites using shared primitives.

**Tech Stack:** React 19, TanStack Start, TanStack Router, TanStack Query, Tailwind v4, shadcn-style source components, Radix primitives, lucide-react, Recharts/SVG charts, MiSans, Bun.

---

## Current Status

In progress. The foundation pass, shell, command palette, promoted `/usage` route, Overview, Usage, Events, Repos, Repo Detail, My Setup, Admin Users, and Settings have a first visual pass using real backend data and shared shadcn-style primitives. Static verification, tests, build, local server reachability, browser QA, commits, and push are complete for the current pass. Browser QA verifies authenticated desktop and 390px mobile content for `/`, `/usage`, `/events`, `/repos`, `/user`, `/admin/users`, and `/settings`, plus command palette open, language switching, and dark mode. The current iteration is deepening the reference match by extracting reusable SVG chart primitives, moving Usage Analytics off Recharts for the primary token trend/model distribution visuals, and aligning the Overview setup status card with a shared `Ring` progress primitive.

Follow-up OAuth/auth redirect diagnosis fixed the `/oauth/device` unauthenticated local-dev loop that produced React maximum update-depth warnings. Root cause: public OAuth pages and the root auth frame were using router `location.href` as the login redirect source, which could become an absolute URL or an already-nested `/login?...` URL during transitions; the login redirect then repeatedly wrapped itself. Current implementation derives route-local redirect paths from `pathname + search`, sanitizes `/login` redirect sources back to `/`, disables retry loops for public OAuth auth checks, and prevents OAuth pages from navigating again once the router is already on `/login`. Verification for this checkpoint: `bun test src/features/auth/auth-flow-state.test.ts`, `bun run check`, `ulimit -n 65536 && bun test --timeout 20000`, `bun run build`, `git diff --check`, and agent-browser smoke on `http://127.0.0.1:4339/oauth/device` showing `http://127.0.0.1:4339/login?redirect=%2Foauth%2Fdevice`, no error boundary, zero horizontal overflow, and no dev-server update-depth warning.

## File Structure

- Modify: `frontend-ng/src/styles.css` - global MiSans, warm-paper tokens, shadcn compatibility tokens, sidebar/chart tokens, layout utilities, motion utilities, CSS-grid table classes.
- Modify: `frontend-ng/src/components/ui/button.tsx` - normalize shadcn button variants to the reference primary/ghost/link/icon dimensions.
- Modify: `frontend-ng/src/components/ui/card.tsx` - normalize card radius, shadow, composition spacing, and hover/accent helper classes.
- Modify: `frontend-ng/src/components/ui/badge.tsx` - add reference tone aliases while preserving current `success`, `warning`, and `ai` usages.
- Modify: `frontend-ng/src/components/primitives/metric-card.tsx` - evolve into a KPI-capable primitive while preserving the existing `MetricCard` export for current screens.
- Create: `frontend-ng/src/components/primitives/charts.tsx` - shared reference-style SVG chart primitives (`Sparkline`, `SparkBars`, `Ring`, `StackedAreaChart`, `BarsH`).
- Test: `frontend-ng/src/components/primitives/charts.test.ts` - locks sparkline and stacked-area path calculations.
- Create: `frontend-ng/src/components/primitives/chart-legend.tsx` - shared inline swatch legend primitive for chart cards.
- Test: `frontend-ng/src/components/primitives/chart-legend.test.tsx` - locks legend slots, wrapping, and swatch rendering.
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

  Follow-up chart legend audit implementation adds the shared `ChartLegend` primitive and migrates the Usage Analytics token trend legend away from page-local swatch markup while preserving the same `StackedAreaChart` keys, token colors, and i18n labels.

  Follow-up chart legend audit evidence: `chart-legend.test.tsx` was added with a red-green cycle; it first failed because `./chart-legend` did not exist, then passed after implementation. Focused verification passed with `bun test src/components/primitives/chart-legend.test.tsx src/components/primitives/charts.test.ts src/features/user-usage/user-usage-state.test.ts`, `bun run check`, and `git diff --check`. Full verification passed with `ulimit -n 65536 && bun test --timeout 20000` and `bun run build`. `agent-browser` reverified `/usage` on the existing `4339` dev server with no error boundary, no body horizontal overflow, and no button/link overflow; the local guest/loading state did not expose configured chart content, so visible `ChartLegend` data-state rendering remains covered by the primitive SSR test, TypeScript, and build until usage data is available.

- [x] **Step 6: Align Overview setup progress with shared Ring**

  Extract Overview setup readiness into `home-state.ts`, render the setup status card with the shared `Ring` primitive, migrate recent usage into reference-style live activity rows with `ToolGlyph`, and keep checklist labels/statuses backed by real dashboard, provider, and event data.

  Follow-up audit implementation adds the shared `ChecklistRow` primitive for setup readiness rows. Overview now uses it for account, AI access, repository reporting, and recent usage status rows instead of a page-local `StatusLine` surface.

  Current evidence: `home-state.test.ts`, `bun run check`, `bun test`, `bun run build`, `git diff --check`, and browser QA on `/` passed. `agent-browser` verified authenticated desktop `1280x720` and mobile `390x844` states with a likely ring SVG, visible `1/4` setup progress, recent usage header, live dot, "View all records" action, no visible button overflow, no error boundary, and no horizontal overflow. The local account currently has no recent events, so rendered activity-row data is covered by `home-state.test.ts`.

  Follow-up audit evidence: `checklist-row.test.tsx` was added with a red-green cycle, `home-state.test.ts` and `bun run check` passed after migrating Overview setup status rows, and browser QA reverified `/` at desktop and mobile widths.

  Follow-up recent-usage audit implementation adds the shared `UsageActivityRow` primitive and migrates Overview recent usage rows away from page-local row markup while preserving the same backend event summary helper, status badges, tool glyph, token/request/credit metadata, and i18n labels.

  Follow-up recent-usage audit evidence: `usage-activity-row.test.tsx` was added with a red-green cycle, `home-state.test.ts` and `bun run check` passed after migrating Overview recent usage rows. `agent-browser` reverified `/` at `1280x720` and `390x844`: four setup checklist rows, recent usage header, no error boundary text, no horizontal overflow, and no visible button/link overflow. The local account still has no recent event rows, so rendered `UsageActivityRow` data-state markup is covered by primitive tests and TypeScript/build until event data is available.

  Follow-up usage table audit implementation migrates the Usage Analytics model distribution table to the shared `DataGrid` primitive while keeping the existing `/api/user/usage/dashboard` response mapping, `BarsH` chart, model ordering, and numeric formatting unchanged.

  Follow-up usage table audit evidence: `data-grid.test.tsx`, `user-usage-state.test.ts`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed after the migration. `agent-browser` reverified `/usage` at `1280x720` and `390x844`: no error boundary text, no body horizontal overflow, and no visible over-wide interactive controls. The local usage payload currently has no model distribution rows, so visible model-table rendering is covered by primitive tests and TypeScript/build until model data is available.

  Follow-up usage controls audit implementation migrates the Usage Analytics title/range/refresh control row to shared `FilterRow` and `ActionGroup` primitives. The same range state, `SegmentedControl`, refresh mutation, embedded title behavior, and backend dashboard query contract remain unchanged while the page no longer hand-rolls wrapped action-row layout.

  Follow-up usage controls audit evidence: `user-usage-panel-composition.test.ts` was added with a red-green cycle; it first failed because `user-usage-panel.tsx` did not import or render `ActionGroup`/`FilterRow`, then passed after migration. Focused verification passed with `bun test src/features/user-usage/user-usage-panel-composition.test.ts src/features/user-usage/user-usage-state.test.ts src/components/primitives/action-group.test.tsx src/components/primitives/filter-row.test.tsx`, `bun run check`, and `git diff --check`. `agent-browser` opened `/usage` on the local dev server and verified no error boundary, no body horizontal overflow, and no visible over-wide buttons/links; that unauthenticated local route stayed at `Guest / Loading account`, so `ActionGroup` and `FilterRow` rendering remains covered by the composition test until authenticated usage content is available.

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

  Follow-up info-tile-grid audit implementation migrates the Events SlideOver token/request/credit metric row to the shared `InfoTileGrid` primitive, removing the remaining page-local three-column metric grid while preserving the same compact numeric tiles, AI credit accent, and backend event detail values.

  Follow-up info-tile-grid audit evidence: `events-page-composition.test.ts` was added with a red-green cycle; it first failed because `events-page.tsx` imported only `InfoTile` and still rendered `grid grid-cols-3 gap-2`, then passed after migrating to `InfoTileGrid columns={3}`. Focused verification passed with `bun test src/features/events/events-page-composition.test.ts src/components/primitives/info-tile.test.tsx src/features/events/event-filters.test.ts`, `bun run check`, and `git diff --check`. Full verification passed with `ulimit -n 65536 && bun test --timeout 20000` and `bun run build`. `agent-browser` reverified `/events` on the existing `4339` dev server with no error boundary, no body horizontal overflow, and no button/link overflow; the local guest/loading state did not expose the SlideOver detail content, so visible detail stat-grid rendering remains covered by the composition guard, `InfoTileGrid` SSR test, TypeScript, and build until event data is available.

  Follow-up filter-row audit implementation adds the shared `FilterRow` primitive for wrapped control rows inside filter bands and compact metadata/badge rows inside detail panels. Events now uses it for both stacked filter-control rows and the SlideOver badge row instead of page-local `flex flex-wrap ... gap-2` wrappers, while preserving search, segmented filters, time filters, admin user search, clear-user, and detail badge behavior.

  Follow-up filter-row audit evidence: `filter-row.test.tsx` and the extended `events-page-composition.test.ts` were added with a red-green cycle; `filter-row.test.tsx` first failed because `./filter-row` did not exist, and the Events composition guard first failed because `events-page.tsx` did not import or render `FilterRow`. Focused verification passed with `bun test src/components/primitives/filter-row.test.tsx src/features/events/events-page-composition.test.ts src/features/events/event-filters.test.ts src/components/primitives/card-filter-bar.test.tsx` and `bun run check`.

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

  Follow-up workbench shell audit implementation migrates the Repositories top filter band, provider tab band, scope rail heading, selected-scope heading, and workbench empty-state padding to shared `CardFilterBar`, `SectionCardHeader`, and shadcn `CardContent` composition. Existing URL-backed binding/provider/scope/page state, provider tabs, scope navigation, pagination, add/delete, auto-bind, and webhook repair behavior remain unchanged.

  Follow-up workbench shell audit evidence: `repos-page-composition.test.ts` was added with a red-green cycle; it first failed because `repos-page.tsx` did not import or render `CardFilterBar`/`SectionCardHeader`, then passed after migration. Focused verification passed with `bun test src/features/repos/repos-page-composition.test.ts src/features/repos/repos-state.test.ts src/features/repos/repo-binding.test.ts src/features/repos/repo-webhook-state.test.ts src/components/primitives/card-filter-bar.test.tsx src/components/primitives/section-card-header.test.tsx`, `bun run check`, and `git diff --check`.

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

  Follow-up linked-record audit implementation extends the shared `LinkedRecordItem` primitive with optional description and trailing slots, then migrates Repository Detail PR title links away from page-local anchor styling while preserving the external PR target, `noreferrer`, title, `#scm_pr_id · author` metadata, and trailing external-link affordance.

  Follow-up linked-record audit evidence: `repo-detail-page-composition.test.ts` was added with a red-green cycle; it first failed because `repo-detail-page.tsx` did not import or render `LinkedRecordItem`, then passed after migration. Focused verification passed with `bun test src/features/repos/repo-detail-page-composition.test.ts src/components/primitives/linked-record-list.test.tsx src/features/repos/repo-detail-state.test.ts`, `bun run check`, and `git diff --check`. Full verification passed with `ulimit -n 65536 && bun test --timeout 20000` and `bun run build`. `agent-browser` reverified `/repos/1` and `/repos` on the existing `4339` dev server with no error boundary, no body horizontal overflow, and no button/link overflow; both routes redirected to login in the local unauthenticated state, so visible PR linked-record rows remain covered by the composition guard, primitive SSR test, TypeScript, and build until repository detail data is available.

  Follow-up KPI-grid audit implementation migrates the Repository Detail PR metric strip from a route-local `grid gap-4 sm:grid-cols-4` wrapper to the shared `kpi-grid` design utility, keeping the same backend PR summary values and `MetricCard` rendering while aligning the page with Overview, Usage, Events, and Repositories KPI layout tokens.

  Follow-up KPI-grid audit evidence: `repo-detail-page-composition.test.ts` was extended with a red-green cycle; it first failed because `repo-detail-page.tsx` still rendered the page-local grid wrapper, then passed after the container moved to `kpi-grid`. Focused verification passed with `bun test src/features/repos/repo-detail-page-composition.test.ts src/features/repos/repo-detail-state.test.ts src/features/repos/repo-binding.test.ts src/features/repos/repo-webhook-state.test.ts`, `bun run check`, and `git diff --check`.

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

  Follow-up simple-header audit implementation migrates the My Setup account-access card, Overview recent-usage card, and Repository Detail latest sync and SCM binding cards to `SectionCardHeader` while preserving badge/action/icon treatment and leaving the wider PR filter and provider-group toolbars as local layouts. Current evidence: `section-card-header.test.tsx`, `home-state.test.ts`, `repo-detail-state.test.ts`, `user-setup-state.test.ts`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed after migration. `agent-browser` reverified `/` at `1280x720`: two shared card headers rendered, no error boundary text, no body horizontal overflow, and no visible over-wide interactive controls. The authenticated browser session became unstable before a reliable new `/user` content-state capture, and the local account still exposes no repository detail links, so the user and repo-detail rendering paths for this narrow header change remain covered by focused tests, TypeScript, and build until an authenticated data-bearing browser session is available.

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

  Follow-up deployment action-row audit implementation extends the Settings page composition guard and migrates the Deployment & Runtime check/apply/rollback/restart action row to `ActionGroup wrap` instead of a page-local `flex gap-2` wrapper, preserving the existing confirm actions, disabled states, restart/update mutations, and button variants.

  Follow-up deployment action-row audit evidence: `settings-page-composition.test.ts` first failed because `settings-page.tsx` still rendered `<div className='flex gap-2'>`, then passed after migrating the row to `<ActionGroup wrap className='justify-start'>`. Focused verification passed with `bun test src/features/settings/settings-page-composition.test.ts src/components/primitives/action-group.test.tsx src/features/settings/settings-payloads.test.ts`, `bun run check`, and `git diff --check`. Full verification passed with `ulimit -n 65536 && bun test --timeout 20000` and `bun run build`. `agent-browser` reverified `/settings?section=deployment-runtime` on the existing `4339` dev server with no error boundary, no body horizontal overflow, and no button/link overflow; the local unauthenticated state redirected to login, so visible deployment action-row rendering remains covered by the composition guard, `ActionGroup` SSR test, TypeScript, and build until authenticated Settings content is available.

  Follow-up pagination audit implementation migrates the Admin Users table footer to the shared `CardPagerFooter` primitive while preserving the existing page summary, Previous/Next disabled states, and page mutation handlers.

  Follow-up pagination audit evidence: `card-pager-footer.test.tsx` now covers className passthrough, `admin-users-state.test.ts`, `bun test`, `bun run check`, `bun run build`, and `git diff --check` passed after migration. `agent-browser` could not produce a fresh browser capture in this run because its daemon returned `Resource temporarily unavailable (os error 35)`, so the visible Admin Users footer remains covered by the existing primitive tests, TypeScript, and build until the browser daemon is healthy again.

  Follow-up Events pagination audit implementation migrates the Usage Records table controls to `CardPagerFooter`, moving page size, Previous/Next, page count, and total summary into the shared footer and removing the extra table title row so the Events table is closer to the reference design's direct table card.

  Follow-up Events pagination audit evidence: `card-pager-footer.test.tsx`, `event-filters.test.ts`, `bun test --timeout 20000`, `bun run check`, `bun run build`, and `git diff --check` passed after migration. A first focused test attempt hit environment timeouts while stale browser/TypeScript processes were still running; after clearing the stuck `tsc` process and increasing the test timeout, the same focused tests and full suite passed. Fresh `agent-browser` visual QA remains pending because many stale agent-browser/Chrome processes left the daemon busy, and those global browser processes were not killed to avoid disrupting user state.

  Follow-up Repos workbench audit implementation removes the extra `CardHeader` title/description chrome from the Repositories workbench and makes the provider selector a direct top tabs strip, matching the reference workbench card structure more closely while preserving URL-backed provider/scope selection, filters, pagination, and backend data usage.

  Follow-up Repos workbench audit evidence: `repos-state.test.ts`, `repo-binding.test.ts`, `repo-webhook-state.test.ts`, `bun test --timeout 20000`, `bun run check`, `bun run build`, and `git diff --check` passed after migration. `agent-browser` could open `/repos` in a fresh session and confirmed no error boundary or horizontal overflow, but that session redirected to `/login?redirect=%2Frepos`; authenticated Repos content-state visual QA remains pending an available logged-in browser session.

  Follow-up localdev auth audit implementation keeps the backend proxy target as the single `AE_FRONTEND_BACKEND_URL` contract and tightens `/oauth2/local` handoff so an online session must redirect to localhost with app tokens plus a valid backend target. If the deployed frontend has no valid backend API origin configured, handoff now returns `503` instead of redirecting without `backend_url` and letting localhost silently fall back to the wrong backend. Browser code remains same-origin `/api/*`; the local TanStack server reads the HttpOnly `ae_backend_url` cookie and performs the server-side proxy.

  Follow-up localdev auth evidence: `local-handoff.server.test.ts` was added with a red-green cycle. The first business assertion failed because missing backend target still returned `302`; after adding `getLocalHandoffBackendUrl`, the focused handoff/cookie tests passed. Direct HTTP evidence against the local dev server also showed `/oauth2/local?access_token=...&refresh_token=...&backend_url=...` writes `ae_app_access`, `ae_app_refresh`, and `ae_backend_url` as localhost HttpOnly cookies. A fresh unauthenticated request to `https://ai-efficiency-web.la3.agoralab.co/oauth2/local?target=http://127.0.0.1:4317` was redirected by the gateway to `https://oauth.agoralab.co/oauth/authorize?...&redirect_uri=https://ai-efficiency-web.la3.agoralab.co/oauth2/callback`, confirming the gateway redirect URI path is currently correct; authenticated end-to-end online-to-local handoff remains pending a usable logged-in browser session.

  Follow-up entity-header audit implementation adds the shared `EntityCardHeader` primitive for card headers that combine leading visual identity/progress, title/description, and wrapped right-side controls. Overview setup progress, My Setup provider detail, and Repository Detail PR filters now use this primitive instead of page-local `CardHeader` compositions, preserving the same Ring progress, provider group buttons, and PR month/page-size selectors.

  Follow-up entity-header audit evidence: `entity-card-header.test.tsx` was added and passed, with focused coverage from `home-state.test.ts`, `user-setup-state.test.ts`, and `repo-detail-state.test.ts`. `bun test --timeout 20000`, `bun run check`, `bun run build`, and `git diff --check` passed after migrating the three call sites. Fresh `agent-browser` sessions for `/` and `/user` redirected to `/login?redirect=...`, with no error boundary, no body horizontal overflow, and no mobile button overflow; authenticated content-state visual QA for the new `EntityCardHeader` call sites remains pending a reusable logged-in browser session or local handoff.

  Follow-up info-tile grid audit implementation adds `InfoTileGrid` as the shared responsive layout wrapper for small `InfoTile` stat matrices. My Setup provider group stats, Repository Detail latest sync stats, and Settings deployment-runtime stats now use the shared grid instead of repeating page-local `grid gap-3 md:grid-cols-*` markup.

  Follow-up info-tile grid audit evidence: `info-tile.test.tsx` first failed because `InfoTileGrid` did not exist, then passed after adding the primitive and migrating the three call sites. Focused verification passed with `bun test src/components/primitives/info-tile.test.tsx src/features/user-setup/user-setup-state.test.ts src/features/repos/repo-detail-state.test.ts src/features/settings/settings-payloads.test.ts && bun run check`; a source scan confirmed the old `grid gap-3 md:grid-cols-3/4` feature-page pattern was removed. Full verification passed with `ulimit -n 65536 && bun test --timeout 20000`, `bun run build`, and `git diff --check`. Fresh `agent-browser` smoke on the existing `4339` dev server verified `/user`, `/settings?section=deployment-runtime`, and `/repos/1` with no error boundary, no body horizontal overflow, and no button overflow; local guest/data-light states did not render the new grids, so authenticated content-state grid rendering is covered by `InfoTileGrid` SSR tests, focused state tests, TypeScript, and build.

  Follow-up filter-bar audit implementation adds the shared `CardFilterBar` primitive for reference-style card filter/tool bands. Events now uses the stacked variant for search/tool/binding/time/admin filters, and Admin Users uses the default wrapped variant for search/page-size/refresh/current-job controls instead of page-local `CardContent` toolbar markup.

  Follow-up filter-bar audit evidence: `card-filter-bar.test.tsx` was added and passed, with focused coverage from `event-filters.test.ts` and `admin-users-state.test.ts`. `bun test --timeout 20000`, `bun run check`, `bun run build`, and `git diff --check` passed after migrating the Events and Admin Users filter bars. Fresh `agent-browser` coverage for `/events` redirected to `/login?redirect=%2Fevents` with no error boundary and no horizontal overflow; `/admin/users` on mobile stayed in the authenticated loading-account state with no error boundary, no body overflow, and no oversized buttons. Authenticated content-state filter-bar DOM verification remains pending a reusable logged-in browser session or local handoff.

  Follow-up Repos controls audit implementation reuses existing primitives instead of page-local controls: the top repository actions now use `ActionGroup wrap`, and the add-repository clone protocol selector now uses `LabeledSegmentedControl` instead of two manually styled active-state buttons.

  Follow-up Repos controls audit evidence: focused coverage from `repos-state.test.ts`, `repo-binding.test.ts`, `repo-webhook-state.test.ts`, `labeled-segmented-control.test.tsx`, and `action-group.test.tsx` passed. `bun test --timeout 20000`, `bun run check`, `bun run build`, and `git diff --check` passed after migrating the Repos controls. Fresh `agent-browser` coverage for `/repos` reached the local authenticated loading-account state with no error boundary and no horizontal overflow; authenticated content-state verification for the repository actions and add-repository dialog remains pending a reusable logged-in browser session or local handoff.

  Follow-up Settings LDAP audit implementation extracts `LdapSettingsForm` so the Organization Login section uses shadcn `FieldGroup`, `Field`, `FieldLabel`, `Input`, `Checkbox`, and shared `ActionGroup` instead of page-local placeholder-only inputs and hand-written action row markup. `SettingsPage` still owns backend data, mutations, and messages while the extracted form owns form semantics and layout.

  Follow-up Settings LDAP audit evidence: `ldap-settings-form.test.tsx` was added and passed with `settings-payloads.test.ts`. `bun test --timeout 20000`, `bun run check`, `bun run build`, and `git diff --check` passed after extracting the LDAP form. Fresh `agent-browser` coverage for `/settings?section=organization-login` reached the local authenticated loading-account state with no error boundary and no horizontal overflow; authenticated content-state verification for the LDAP form remains covered by SSR/component tests until a reusable logged-in browser session or local handoff is available.

  Follow-up Settings Relay audit implementation extracts `RelayProviderForm` so the Relay provider dialog uses shadcn `FieldGroup`, `Field`, `FieldLabel`, `Input`, `Checkbox`, and shared `ActionGroup` instead of placeholder-only inputs and inline dialog action markup. The dialog page still owns create/update mutations and edit-mode state while the extracted form owns field semantics, disabled-name edit behavior, validation gating, and error presentation.

  Follow-up Settings Relay audit evidence: `relay-provider-form.test.tsx` was added and passed with `ldap-settings-form.test.tsx` and `settings-payloads.test.ts`. `bun test --timeout 20000`, `bun run check`, `bun run build`, and `git diff --check` passed after extracting the Relay form. Fresh `agent-browser` coverage for `/settings?section=ai-services` redirected to `/login?redirect=%2Fsettings%3Fsection%3Dai-services` with no error boundary and no horizontal overflow; authenticated Relay dialog visual verification remains covered by SSR/component tests until a reusable logged-in browser session or local handoff is available.

  Follow-up Settings SCM audit implementation extracts `ScmProviderForm` so the SCM provider dialog uses shadcn `FieldGroup`, `Field`, `FieldLabel`, `Input`, dynamic credential `Select`, shared `ActionGroup`, and `LabeledSegmentedControl` for fixed provider/clone protocol option sets instead of placeholder-only inputs and inline action markup. The extracted form preserves edit-mode provider-type immutability and shows SSH host / clone credential fields only for SSH clone mode.

  Follow-up Settings SCM audit evidence: `scm-provider-form.test.tsx` was added and passed with the Settings form tests and `settings-payloads.test.ts`. `bun test --timeout 20000`, `bun run check`, `bun run build`, and `git diff --check` passed after extracting the SCM form. Fresh `agent-browser` coverage for `/settings?section=code-platforms` reached the local authenticated loading-account state with no error boundary and no horizontal overflow; authenticated SCM dialog visual verification remains covered by SSR/component tests until a reusable logged-in browser session or local handoff is available.

  Follow-up Settings Credential audit implementation extracts `CredentialForm` so the advanced credential dialog uses shadcn `FieldGroup`, `Field`, `FieldLabel`, `Input`, `Textarea`, shared `ActionGroup`, and `LabeledSegmentedControl` for credential kind selection instead of placeholder-only inputs and inline secret-kind branches. The extracted form preserves create-mode secret requirements and edit-mode blank-secret submission behavior.

  Follow-up Settings Credential audit evidence: `credential-form.test.tsx` was added and passed with the Settings form tests and `settings-payloads.test.ts`. `bun test --timeout 20000`, `bun run check`, `bun run build`, and `git diff --check` passed after extracting the Credential form. Fresh `agent-browser` coverage for `/settings?section=advanced-credentials` reached the local authenticated loading-account state with no error boundary and no horizontal overflow; authenticated Credential dialog visual verification remains covered by SSR/component tests until a reusable logged-in browser session or local handoff is available.

  Follow-up Login audit implementation extracts `LoginForm` so `/login` uses shadcn `FieldGroup`, `Field`, `FieldLabel`, `Input`, and `Select` semantics instead of page-local placeholder-only fields. `LoginPage` still owns auth option loading, manual login, dev login, redirects, and mutation behavior, while the extracted form owns field labels, validation gating, source selection, and error presentation.

  Follow-up Login audit evidence: `login-form.test.tsx` was added and passed with `auth-flow-state.test.ts`. `bun test src/features/auth/login-form.test.tsx src/features/auth/auth-flow-state.test.ts`, `bun test --timeout 20000`, `bun run check`, `bun run build`, and `git diff --check` passed after extracting the form. `agent-browser` reverified `/login` at `http://127.0.0.1:4317/login`: one shared field group rendered with username, password, login source, sign-in, and dev-login controls; no error boundary rendered, body horizontal overflow was zero, and button overflow was zero.

  Follow-up OAuth form/action audit implementation adds shared `DeviceCodeField` and `OAuthActionGroup` exports so `/oauth/device` uses shadcn `Field`/`FieldLabel`/`Input` semantics and both OAuth approval routes reuse `ActionGroup` instead of page-local `flex gap-2` action rows. Existing OAuth authorize payloads, device-code normalization, approve/deny mutations, redirect behavior, and signed-in context rendering remain owned by the page.

  Follow-up OAuth form/action audit evidence: `oauth-pages.test.tsx` was added and passed with `auth-flow-state.test.ts`. `bun test src/features/oauth/oauth-pages.test.tsx src/features/auth/auth-flow-state.test.ts`, `bun test --timeout 20000`, `bun run check`, `bun run build`, and `git diff --check` passed after extracting the device-code field and action group.

  Follow-up Admin subscription audit implementation extracts `AdminSubscriptionForm` so the subscription-management controls use shadcn `FieldGroup`, `Field`, `FieldLabel`, `Select`, `Input`, `Checkbox`, and shared `ActionGroup` instead of page-local grid controls without field labels. `AdminUsersPage` still owns selected users, provider/group defaults, subscription job mutation, polling, messages, and backend payload construction.

  Follow-up Admin subscription audit evidence: `admin-subscription-form.test.tsx` was added and passed with `admin-users-state.test.ts`. `bun test src/features/admin-users/admin-subscription-form.test.tsx src/features/admin-users/admin-users-state.test.ts`, `bun test --timeout 20000`, `bun run check`, `bun run build`, and `git diff --check` passed after extracting the form. Fresh `agent-browser` coverage for `/admin/users` reached the local unauthenticated loading-account state with no error boundary, no body horizontal overflow, and no button overflow; authenticated subscription form content-state rendering remains covered by SSR/component tests until a reusable logged-in browser session or local handoff is available.

  Follow-up Repos create-form audit implementation extracts `RepoCreateForm` so the add-repository dialog uses shadcn `FieldGroup`, `Field`, `FieldLabel`, `Select`, `Input`, shared `InsetPanel`, `LabeledSegmentedControl`, `FieldList`, and `ActionGroup` instead of page-local form markup and hardcoded visible URL placeholder text. `ReposPage` still owns dialog state, URL parsing, provider matching, clone URL preview, default-branch state, create mutation, errors, and cache invalidation.

  Follow-up Repos create-form audit evidence: `repo-create-form.test.tsx` was added and passed with `repos-state.test.ts`, `repo-binding.test.ts`, and `repo-webhook-state.test.ts` after rerunning with `ulimit -n 65536` because the first Bun test attempt hit the local file-descriptor limit. `ulimit -n 65536 && bun test --timeout 20000`, `bun run check`, `bun run build`, and `git diff --check` passed after extracting the form. Fresh `agent-browser` coverage for `/repos` on the existing `4317` dev server returned a stale `500 HTTPError` even for `/login` and `/`, so a fresh current-code dev server was started on `4321`; `/repos` on `4321` reached the local unauthenticated loading-account state with no error boundary, no body horizontal overflow, and no button overflow. Authenticated add-repository dialog content-state rendering remains covered by SSR/component tests until a reusable logged-in browser session or local handoff is available.

  Follow-up Provider test form audit implementation extracts `ProviderTestForm` so My Setup provider testing uses shadcn `FieldGroup`, `Field`, `FieldLabel`, `Input`, `Select`, and `Textarea` plus shared `ActionGroup`, `AppAlert`, and `InsetPanel` instead of page-local form markup. `UserPage` still owns provider/group selection, credential state, model loading, provider test mutation, backend payload construction, and result state.

  Follow-up Provider test form audit evidence: `provider-test-form.test.tsx` was added. The focused test first failed because Radix Select option text is not present in static SSR markup, then passed after the selected model label was exposed through the trigger's accessible label. `ulimit -n 65536 && bun test src/features/user-setup/provider-test-form.test.tsx src/features/user-setup/user-setup-state.test.ts`, `ulimit -n 65536 && bun test --timeout 20000`, `bun run check`, `bun run build`, and `git diff --check` passed after extracting the form. Fresh `agent-browser` coverage for `/user` on a new `4322` dev server redirected to `/login?redirect=%2Fuser`; the redirected page had no error boundary, no body horizontal overflow, one field group, and no visible button overflow. Authenticated provider test content-state rendering remains covered by SSR/component tests, TypeScript, and build until a reusable logged-in browser session or local handoff is available.

  Follow-up Select composition audit implementation adds `select-composition.test.ts` to enforce the shadcn Select contract that `SelectItem` nodes are grouped inside `SelectGroup`. Existing Select menus in Login, Admin subscription controls, Admin Users pagination, Repos filters/create dialog, Repo Detail binding/PR controls, Settings SCM credential controls, and Events pagination now use `SelectGroup` without changing values, labels, query state, or backend payload behavior.

  Follow-up Select composition audit evidence: `select-composition.test.ts` first failed with direct `SelectItem` usage across the affected files, then passed after adding `SelectGroup` wrappers. Focused verification passed with `ulimit -n 65536 && bun test src/components/ui/select-composition.test.ts src/features/auth/login-form.test.tsx src/features/admin-users/admin-subscription-form.test.tsx src/features/repos/repo-create-form.test.tsx src/features/settings/scm-provider-form.test.tsx src/features/events/event-filters.test.ts src/features/repos/repos-state.test.ts src/features/repos/repo-detail-state.test.ts`. Full verification passed with `bun run check`, `ulimit -n 65536 && bun test --timeout 20000`, `bun run build`, and `git diff --check`. Fresh `agent-browser` coverage on a new `4323` dev server verified `/login`, `/repos`, and `/admin/users` with no error boundary, no body horizontal overflow, and no button overflow; `/login` rendered one field group, while `/repos` and `/admin/users` rendered the local unauthenticated/data-light states.

  Follow-up Select field primitive audit implementation adds `SelectField` as the shared shadcn form select wrapper around `Field`, `FieldLabel`, `Select`, `SelectTrigger`, `SelectContent`, and `SelectGroup`. Login source, Admin subscription scope/operation/provider/group, Repo create provider, Settings SCM credential selects, and My Setup provider-test model selection now use `SelectField` so extracted forms no longer hand-roll the label/select/group composition.

  Follow-up Select field primitive audit evidence: `select-field.test.tsx` was added with a red-green cycle. Focused verification passed with `ulimit -n 65536 && bun test src/components/primitives/select-field.test.tsx src/components/ui/select-composition.test.ts src/features/auth/login-form.test.tsx src/features/admin-users/admin-subscription-form.test.tsx src/features/repos/repo-create-form.test.tsx src/features/settings/scm-provider-form.test.tsx src/features/user-setup/provider-test-form.test.tsx`. Full verification passed with `bun run check`, `ulimit -n 65536 && bun test --timeout 20000`, `bun run build`, and `git diff --check`. Fresh `agent-browser` coverage on a new `4324` dev server verified `/login`, `/user`, and `/admin/users` with no error boundary, no body horizontal overflow, and no button overflow; `/login` rendered one field group and one select trigger, while `/user` and `/admin/users` rendered the local unauthenticated/data-light states.

  Follow-up Text field primitive audit implementation adds `TextField` as the shared shadcn form text wrapper around `Field`, `FieldLabel`, `Input`, and `Textarea`. Login credentials, Admin subscription days, Repo create URL/SSH host/default branch, Settings SCM name/base URL/SSH host, Settings advanced credential fields, and My Setup provider-test fallback model/platform/prompt now use `TextField` so extracted forms no longer hand-roll label/input/textarea composition.

  Follow-up Text field primitive audit evidence: `text-field.test.tsx` was added with a red-green cycle. Focused verification passed with `ulimit -n 65536 && bun test src/components/primitives/text-field.test.tsx src/features/auth/login-form.test.tsx src/features/settings/scm-provider-form.test.tsx src/features/settings/credential-form.test.tsx src/features/repos/repo-create-form.test.tsx src/features/user-setup/provider-test-form.test.tsx src/features/admin-users/admin-subscription-form.test.tsx`. Full verification passed with `bun run check`, `ulimit -n 65536 && bun test --timeout 20000`, `bun run build`, and `git diff --check`. Fresh `agent-browser` coverage on a new `4325` dev server verified `/login`, `/user`, `/settings`, and `/admin/users` with no error boundary, no body horizontal overflow, and no button overflow; `/login` rendered three fields, two inputs, and one select trigger, while `/user`, `/settings`, and `/admin/users` rendered local unauthenticated/data-light states.

  Follow-up access-group selector audit implementation migrates the My Setup provider-group selector buttons inside `EntityCardHeader` to the shared `ActionGroup` primitive. Provider selection state, active button variants, model/test reset behavior, and backend provider/group query contracts remain unchanged while the selector row no longer relies on raw mapped actions.

  Follow-up access-group selector audit evidence: `user-page-composition.test.ts` was added with a red-green cycle; it first failed because `user-page.tsx` did not import or render `ActionGroup` for provider-group selectors, then passed after migration. Focused verification passed with `bun test src/features/user-setup/user-page-composition.test.ts src/features/user-setup/user-setup-state.test.ts src/components/primitives/action-group.test.tsx`. `agent-browser` opened `/user` on the existing local dev server and verified no error boundary, no body horizontal overflow, and no visible over-wide buttons/links; that route remained at `Guest / Loading account`, so authenticated provider selector rendering remains covered by the composition guard, primitive tests, TypeScript, and build.

  Follow-up Checkbox field primitive audit implementation adds `CheckboxField` as the shared shadcn horizontal checkbox wrapper around `Field`, `Checkbox`, and `FieldLabel`. Relay provider primary/enabled, LDAP StartTLS, and Admin subscription remove confirmation now use `CheckboxField` so extracted forms no longer hand-roll horizontal checkbox composition; dense table selection checkboxes and page-local webhook force controls remain intentionally page-owned.

  Follow-up Checkbox field primitive audit evidence: `checkbox-field.test.tsx` was added with a red-green cycle. Focused verification passed with `ulimit -n 65536 && bun test src/components/primitives/checkbox-field.test.tsx src/features/settings/relay-provider-form.test.tsx src/features/settings/ldap-settings-form.test.tsx src/features/admin-users/admin-subscription-form.test.tsx && bun run check`. Full verification passed with `ulimit -n 65536 && bun test --timeout 20000`, `bun run build`, and `git diff --check`. Fresh `agent-browser` coverage on a new `4326` dev server verified `/login`, `/settings`, and `/admin/users` with no error boundary, no body horizontal overflow, and no button overflow; `/settings` and `/admin/users` rendered local unauthenticated/data-light states, while checkbox content-state rendering remains covered by SSR/component tests.

  Follow-up Settings text field composition audit implementation adds `text-field-composition.test.ts` to keep simple Settings forms from regressing to raw `Input`/`Textarea`/`FieldLabel` composition. Relay provider and LDAP settings text inputs now use the shared `TextField` primitive while preserving edit-mode disabled relay names, password field types, checkbox fields, action buttons, and payload update behavior.

  Follow-up Settings text field composition audit evidence: the new composition guard first failed on `ldap-settings-form.tsx` and `relay-provider-form.tsx`, then passed after both forms moved to `TextField`. Focused verification passed with `ulimit -n 65536 && bun test src/components/primitives/text-field-composition.test.ts src/features/settings/relay-provider-form.test.tsx src/features/settings/ldap-settings-form.test.tsx && bun run check`. Full verification passed with `ulimit -n 65536 && bun test --timeout 20000`, `bun run build`, and `git diff --check`. Browser QA was attempted on a fresh `4327` dev server, but the `agent-browser` command did not return output while multiple daemon instances were present; local HTTP smoke still confirmed `/settings` returned `200 text/html` from the fresh server.

  Follow-up Toolbar select primitive audit implementation adds `ToolbarSelect` as the shared compact shadcn Select wrapper for filter bars and pagination controls. Repos binding/page-size filters, Events page-size pagination, and Admin Users page-size controls now use `ToolbarSelect`, keeping raw list-page Select composition out of route files while preserving URL/search state transitions and backend query parameters.

  Follow-up Toolbar select primitive audit evidence: `toolbar-select.test.tsx` and `toolbar-select-composition.test.ts` were added with a red-green cycle; the composition guard first failed on `admin-users-page.tsx`, `events-page.tsx`, and `repos-page.tsx`, then passed after those list controls moved to `ToolbarSelect`. Focused verification passed with `ulimit -n 65536 && bun test src/components/primitives/toolbar-select.test.tsx src/components/primitives/toolbar-select-composition.test.ts src/components/ui/select-composition.test.ts src/features/events/event-filters.test.ts src/features/repos/repos-state.test.ts src/features/admin-users/admin-users-state.test.ts src/lib/i18n/no-hardcoded-copy.test.ts && bun run check`. Full verification passed with `ulimit -n 65536 && bun test --timeout 20000`, `bun run build`, and `git diff --check`. Fresh `agent-browser` coverage on a new `4328` dev server verified `/repos`, `/events`, and `/admin/users` with no error boundary, no body horizontal overflow, and no button overflow; these local routes rendered unauthenticated/data-light states with zero visible select triggers, while the migrated list-control content states remain covered by SSR/component tests.

  Follow-up Repo Detail primitive composition audit implementation extends the toolbar-select guard to `repo-detail-page.tsx`. Repo Detail SCM binding, PR merged-range, and PR page-size controls now use `ToolbarSelect`; the webhook force-replace control now uses `CheckboxField`. This keeps the route focused on data wiring and mutations while sharing the same shadcn Select/Checkbox composition as the rest of the redesigned surface.

  Follow-up Repo Detail primitive composition audit evidence: the extended composition guard first failed on `repo-detail-page.tsx`, then passed after the route moved to shared primitives. Focused verification passed with `ulimit -n 65536 && bun test src/components/primitives/toolbar-select.test.tsx src/components/primitives/toolbar-select-composition.test.ts src/components/primitives/checkbox-field.test.tsx src/components/ui/select-composition.test.ts src/features/repos/repo-detail-state.test.ts src/features/repos/repo-binding.test.ts src/features/repos/repo-webhook-state.test.ts src/lib/i18n/no-hardcoded-copy.test.ts && bun run check`. Full verification passed with `ulimit -n 65536 && bun test --timeout 20000`, `bun run build`, and `git diff --check`. Fresh `agent-browser` coverage on a new `4329` dev server verified `/repos/1` and `/repos` with no error boundary, no body horizontal overflow, and no button overflow; the local unauthenticated/data-light state rendered zero visible select triggers and checkbox fields, while the migrated Repo Detail content state remains covered by primitive/composition tests, TypeScript, and build.

  Follow-up Events filter text field audit implementation extends the text-field composition guard to `events-page.tsx`. Usage Records date range and admin user search controls now use `TextField` instead of route-local raw `Input`, with explicit i18n labels for from/to time while preserving existing filter state, user search refetch, and backend query parameter construction.

  Follow-up Events filter text field audit evidence: the extended composition guard first failed on `events-page.tsx`, then passed after the filter controls moved to `TextField`. Focused verification passed with `ulimit -n 65536 && bun test src/components/primitives/text-field-composition.test.ts src/components/primitives/text-field.test.tsx src/features/events/event-filters.test.ts src/lib/i18n/no-hardcoded-copy.test.ts && bun run check`. Full verification passed with `ulimit -n 65536 && bun test --timeout 20000`, `bun run build`, and `git diff --check`. Fresh `agent-browser` coverage on a new `4330` dev server opened `/events`, which redirected to `/login?redirect=%2Fevents` in the local unauthenticated state; the smoke still verified no error boundary, no body horizontal overflow, and no button overflow, while the migrated Events filter content state remains covered by primitive/composition tests, event filter tests, TypeScript, and build.

  Follow-up Repo Create text field audit implementation extends the text-field composition guard to `repo-create-form.tsx`. The direct repository dialog preview clone URL is now a labeled read-only `TextField` instead of a route-local raw `Input`, with a shared i18n label and unchanged clone URL generation, parsed repository summary, clone protocol switching, and create payload behavior.

  Follow-up Repo Create text field audit evidence: the extended composition guard first failed on `repo-create-form.tsx`, then passed after the preview clone URL moved to `TextField`. Focused verification passed with `ulimit -n 65536 && bun test src/components/primitives/text-field-composition.test.ts src/features/repos/repo-create-form.test.tsx src/features/repos/repos-state.test.ts src/lib/i18n/no-hardcoded-copy.test.ts && bun run check`. Full verification passed with `ulimit -n 65536 && bun test --timeout 20000`, `bun run build`, and `git diff --check`. Fresh `agent-browser` coverage on a new `4331` dev server verified `/repos` with no error boundary, no body horizontal overflow, and no button overflow; the local guest/loading state did not expose the add-repo dialog or preview clone URL label, while the migrated dialog content state remains covered by `RepoCreateForm` SSR tests, repo state tests, TypeScript, and build.

  Follow-up Admin Users data-grid checkbox audit implementation adds `DataGridCheckbox` as the shared table-selection checkbox wrapper. The Admin Users visible-row and per-row selection cells now use the shared primitive with i18n-backed accessible labels instead of importing raw shadcn `Checkbox` directly, while preserving visible selection, indeterminate selection, and subscription job payload behavior.

  Follow-up Admin Users data-grid checkbox audit evidence: the new composition guard first failed on `admin-users-page.tsx`, then passed after table selection moved to `DataGridCheckbox`. Focused verification passed with `ulimit -n 65536 && bun test src/components/primitives/data-grid-checkbox.test.tsx src/components/primitives/data-grid-checkbox-composition.test.ts src/features/admin-users/admin-users-state.test.ts src/lib/i18n/no-hardcoded-copy.test.ts && bun run check`. Full verification passed with `ulimit -n 65536 && bun test --timeout 20000`, `bun run build`, and `git diff --check`. Fresh `agent-browser` coverage on a new `4332` dev server verified `/admin/users` with no error boundary, no body horizontal overflow, and no button overflow; the local guest/loading state did not expose the admin data grid or checkbox labels, while the migrated table selection content state remains covered by `DataGridCheckbox` SSR tests, Admin Users state tests, TypeScript, and build.

  Follow-up Admin Users action-group audit implementation adds an `ActionGroup` composition guard and migrates encrypted/plaintext relay password row actions away from page-local flex wrapper markup while preserving copy, reveal-toggle, toast, and disabled-state behavior.

  Follow-up Admin Users action-group audit evidence: `action-group-composition.test.ts` first failed on `admin-users-page.tsx`, then passed after the row actions moved to `ActionGroup`. Focused verification passed with `bun test src/components/primitives/action-group-composition.test.ts src/components/primitives/action-group.test.tsx src/features/admin-users/admin-users-state.test.ts && bun run check`. Full verification passed with `ulimit -n 65536 && bun test --timeout 20000`, `bun run build`, and `git diff --check`. Fresh `agent-browser` coverage on the existing `4339` dev server verified `/admin/users` with no error boundary, no body horizontal overflow, and no button overflow; the local guest/loading state did not expose row action groups, while authenticated content-state row rendering is covered by the composition guard, Admin Users state tests, TypeScript, and build.

  Follow-up OAuth device code text field audit implementation extends the text-field composition guard to `oauth-pages.tsx`. The device authorization code entry now uses the shared `TextField` primitive instead of route-local `Field` and raw `Input` composition, while preserving device code normalization, approval/denial actions, and unauthenticated redirect behavior.

  Follow-up OAuth device code text field audit evidence: the extended composition guard first failed on `oauth-pages.tsx`, then passed after `DeviceCodeField` moved to `TextField`. Focused verification passed with `ulimit -n 65536 && bun test src/components/primitives/text-field-composition.test.ts src/components/primitives/text-field.test.tsx src/features/oauth/oauth-pages.test.tsx src/features/auth/auth-flow-state.test.ts src/lib/i18n/no-hardcoded-copy.test.ts && bun run check`. Full verification passed with `ulimit -n 65536 && bun test --timeout 20000`, `bun run build`, and `git diff --check`. Fresh `agent-browser` coverage on a new `4333` dev server verified `/oauth/device` rendered the device-code field with no error boundary, no body horizontal overflow, and no button overflow. The dev server also emitted repeated TanStack Router `Maximum update depth exceeded` warnings on `/oauth/device`; an A/B run on a new `4334` dev server after temporarily reverting this follow-up patch reproduced the same warning, so this is tracked as an existing route/runtime issue rather than a regression from the `TextField` migration.

  Follow-up Settings page raw-control import audit implementation adds a page-shell composition guard for `settings-page.tsx` and removes stale raw shadcn form-control imports. The Settings page already delegates editable controls to shared form components (`RelayProviderForm`, `ScmProviderForm`, `CredentialForm`, and `LdapSettingsForm`); this cleanup makes the source match that architecture and keeps future audits focused on real controls.

  Follow-up Settings page raw-control import audit evidence: the new composition guard first failed on `settings-page.tsx`, then passed after removing the dead imports. Focused verification passed with `ulimit -n 65536 && bun test src/features/settings/settings-page-composition.test.ts src/features/settings/settings-payloads.test.ts src/features/settings/relay-provider-form.test.tsx src/features/settings/scm-provider-form.test.tsx src/features/settings/credential-form.test.tsx src/features/settings/ldap-settings-form.test.tsx src/lib/i18n/no-hardcoded-copy.test.ts && bun run check`. Full verification passed with `ulimit -n 65536 && bun test --timeout 20000`, `bun run build`, and `git diff --check`. Fresh `agent-browser` coverage on a new `4335` dev server verified `/settings` with no error boundary, no body horizontal overflow, and no button overflow; the local guest/loading state did not expose Settings content or dialogs, while the delegated form content states remain covered by Settings form SSR tests, payload tests, TypeScript, and build.

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
  - Follow-up simple-header audit reused `SectionCardHeader` for Overview recent usage, My Setup account access, and Repository Detail latest sync / SCM binding. `agent-browser` confirmed the Overview card headers at `1280x720`; the later authenticated session was unstable, so `/user` and repository-detail visible checks for this narrow header change are covered by focused tests, TypeScript, and build rather than a fresh content-state browser capture.
  - Follow-up Events audit added `OptionList` and `CodeBlock` primitives and reverified `/events` at `1280x720` and `390x844`; the local account remained data-light, so route stability is browser-covered while candidate-list and raw-payload rendering are covered by primitive tests and TypeScript/build.
  - Follow-up Events detail audit added `SectionEyebrow` for SlideOver inspect section labels; the local account remained data-light, so route stability is browser-covered while detail label rendering is covered by primitive tests and TypeScript/build.
  - Follow-up Repository Detail audit added `UsageSummaryPanel` for expanded PR usage summaries; the local account still exposes no repository detail rows, so route-level browser coverage remains on `/repos` data-light states while primitive tests and TypeScript/build cover the expanded data path.
  - Follow-up OAuth audit added `AuthInfoPanel` for signed-in context strips on `/oauth/authorize` and `/oauth/device`; `/oauth/device` was browser-verified while OAuth payload/redirect helpers remain covered by `auth-flow-state.test.ts`.
  - Follow-up auth/admin header audit reused `SectionCardHeader` on `/login`, `/oauth/authorize`, `/oauth/device`, and `/admin/users`; `agent-browser` confirmed the expected shared card headers, no error boundary, no horizontal overflow, and no visible over-wide controls across desktop/mobile coverage for those routes.
  - Follow-up Admin Users pagination audit reused `CardPagerFooter` for the users table pagination row. Browser QA was attempted, but `agent-browser` returned `Resource temporarily unavailable (os error 35)` from the daemon; this specific visual check is pending a healthy browser session while test/build coverage is complete.
  - Follow-up Events pagination audit reused `CardPagerFooter` for Usage Records table pagination and removed the extra table title row to better match the reference table card. Browser QA is pending the same agent-browser daemon recovery; test, typecheck, and build coverage passed.
  - Follow-up Repos workbench audit removed the extra workbench `CardHeader` and moved provider selection to a direct tabs strip, matching the reference card more closely. Fresh browser QA reached `/login?redirect=%2Frepos` with no error boundary or horizontal overflow; authenticated Repos content-state verification remains pending.

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
