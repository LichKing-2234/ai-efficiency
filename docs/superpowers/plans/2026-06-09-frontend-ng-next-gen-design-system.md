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

- [x] **Step 5: Add command palette**

  Register `Ctrl/Cmd+K`, provide navigation commands and safe UI actions, and route using TanStack Router.

  Current implementation composes the shadcn/cmdk `Command` primitive inside the shared `Dialog`, with route commands grouped by i18n labels and admin commands hidden for non-admin users.

- [x] **Step 6: Remove duplicate page titles incrementally**

  Keep generic page titles in the top bar. Feature pages should start with content or page-specific toolbars; Overview may keep its hero.

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

- [x] **Step 6: Align Overview setup progress with shared Ring**

  Extract Overview setup readiness into `home-state.ts`, render the setup status card with the shared `Ring` primitive, migrate recent usage into reference-style live activity rows with `ToolGlyph`, and keep checklist labels/statuses backed by real dashboard, provider, and event data.

  Current evidence: `home-state.test.ts`, `bun run check`, `bun test`, `bun run build`, `git diff --check`, and browser QA on `/` passed. `agent-browser` verified authenticated desktop `1280x720` and mobile `390x844` states with a likely ring SVG, visible `1/4` setup progress, recent usage header, live dot, "View all records" action, no visible button overflow, no error boundary, and no horizontal overflow. The local account currently has no recent events, so rendered activity-row data is covered by `home-state.test.ts`.

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

  Current evidence: `event-filters.test.ts`, `search-field.test.tsx`, `bun run check`, browser QA on `/events`, and full verification passed. `agent-browser` verified two radiogroups, seven radio items, Tool and Binding filters, one shared search input, no error boundary, no visible interactive overflow, and no horizontal overflow at `1280x720` and `390x844`.

- [x] **Step 2: Repositories**

  Implement reference workbench styling while keeping add, delete, auto-bind, provider selection, webhook repair, and navigation behavior intact.

- [x] **Step 3: Repository detail**

  Re-skin SCM binding, usage snapshots, webhook status, and PR sync panels with shared cards/tables.

- [x] **Step 4: My Setup**

  Re-skin provider credential setup, status/progress, and key actions with shared field/card primitives.

  Current implementation uses the shared `CommandStep` primitive for CLI onboarding rows so installer, login, discover, hooks, init, doctor, and Windows commands share reference-style copy affordances instead of page-local command markup.

  Current evidence: `command-step.test.ts`, `bun run check`, and browser QA on `/user` passed. `agent-browser` verified the CLI workflow, six numbered command steps, copy affordances, no error boundary, and no horizontal overflow at `1280x720` and `390x844`. The mobile run also caught and fixed a hidden command-row width issue by applying `min-width: 0` to split-layout children and the command-step row; the largest visible mobile button width is now `285px` in a `390px` viewport.

- [x] **Step 5: Admin Users and Settings**

  Re-skin admin pages with shared grid tables and forms while preserving role guards and backend mutation contracts.

  Current implementation uses the shared `SearchField` primitive for the Admin Users table filter, matching Events search behavior and shadcn `InputGroup` composition.

  Current implementation also uses the shared `SectionNav` primitive for the Settings section rail, replacing page-local button markup while preserving URL-backed section selection.

  Current evidence: `search-field.test.tsx`, `section-nav.test.tsx`, `bun run check`, `bun test`, `bun run build`, and browser QA on `/admin/users` and `/settings` passed. `agent-browser` verified the authenticated Admin Users shared search input at `1280x720` and `390x844`, the Settings shared section rail with five items and one current item at `1280x720` and `390x844`, no error boundary, and no horizontal overflow.

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
  - Overview recent usage now uses reference-style live activity rows with `ToolGlyph`, binding badges, token/request/credit metadata, and a route link to all records; local browser QA verified the empty state header/action at desktop and mobile widths.
  - My Setup CLI workflow now uses the shared `CommandStep` primitive; `agent-browser` confirmed six numbered command rows, copy affordances, no error boundary, no horizontal overflow, and no visible button overflow at `1280x720` and `390x844`.

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
