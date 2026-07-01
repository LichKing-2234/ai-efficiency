# Company-Wide Usability Hardening Implementation Plan

> **For agentic workers:** This plan is the active ledger for `docs/superpowers/specs/2026-05-30-company-wide-usability-hardening-design.md`. Update checkboxes as each step is actually completed in the same work session.

**Goal:** Harden the implemented task-zone UI so it is usable by company-wide technical and non-technical users across desktop and mobile, with bilingual terminology, accessible dialogs, recoverable URL state, and task-first setup guidance.

**Architecture:** Keep existing routes and backend APIs. Implement frontend-only layout, i18n, accessibility, and URL-state improvements unless a later approved spec extends backend contracts.

**Tech Stack:** Vue 3, Vite, TypeScript, Pinia, Vue Router, TailwindCSS, Vitest, Playwright.

**Status:** Implementation completed and self-tested on 2026-05-30. Follow-up polish completed and self-tested on 2026-06-01 for local-only setup status wording, dashboard scope wording, mobile event filter summaries, and additional i18n cleanup. Sidebar footer interaction polish completed and self-tested on 2026-06-01; the footer account identity is display-only, language switching moved to the sidebar header, and logout remains the footer utility action. Setup command recommendation polish completed and self-tested on 2026-06-01; setup progress now recommends the platform-specific install command and default browser login, with device login kept as fallback/reference. Command reference dedupe completed and self-tested on 2026-06-01; command reference is now an advanced fallback/recovery section and primary setup commands appear once. Blocked-setup guidance polish completed and self-tested on 2026-06-01; the vague developer escalation was replaced with actionable diagnosis-sharing guidance based on `ae-cli doctor`. Model selector polish completed and self-tested on 2026-06-01; `/user` now loads model choices through a backend user-scoped provider/group endpoint using the selected group's platform and API key, with manual input fallback when model loading is unavailable. The 2026-05-30 baseline verification passed with `pnpm exec vue-tsc -b --pretty false`, `pnpm test`, `pnpm run test:e2e:role`, and 48 Playwright screenshot/overflow checks recorded in `output/playwright/hardening-overflow-report.json` for `/`, `/user`, `/events`, `/repos`, `/repos/:id`, `/settings`, `/admin/users`, `/login`, and `/oauth/device` across 375x812, 390x900, 1280x800, and 1440x900 viewports. The 2026-06-01 follow-ups passed `pnpm exec vue-tsc -b --pretty false`, `pnpm test`, and `pnpm run test:e2e:role`.

## Scope Boundary

Included:

- Mobile overflow fixes and mobile-first alternatives for primary lists.
- `/user` setup flow restructure.
- Dialog/drawer semantics and focus management.
- URL query/hash state for filters and settings tabs.
- Touched bilingual labels and validation messages.
- Focused frontend tests and visual review artifacts.

Excluded:

- Backend API changes.
- New role/department/team dashboard model.
- Browser-to-local CLI execution.
- Full brand redesign.
- Translating commands, repo names, provider display names, model names, branch names, commit SHAs, or raw advanced payload fields.

## File Map

Shared:

- `frontend/src/components/AppLayout.vue`
- `frontend/src/components/AppSidebar.vue`
- `frontend/src/i18n.ts`
- `frontend/src/__tests__/app-sidebar.test.ts`

Pages:

- `frontend/src/views/DashboardView.vue`
- `frontend/src/views/UserView.vue`
- `frontend/src/views/events/EventsView.vue`
- `frontend/src/views/repos/RepoListView.vue`
- `frontend/src/views/repos/RepoDetailView.vue`
- `frontend/src/views/SettingsView.vue`
- `frontend/src/views/admin/AdminUsersView.vue`

Settings components:

- `frontend/src/components/settings/*.vue`

Tests and review:

- `frontend/src/__tests__/*.test.ts`
- `frontend/e2e_role_test.py`
- `output/playwright/`

## Task 1: Add Mobile Overflow Guardrails

- [x] Add or update tests that assert the primary page containers do not require page-level horizontal scroll at 375px/390px.
- [x] Fix `/user` layout overflow caused by desktop grid/content width on mobile.
- [x] Add mobile card layout for `/events` primary usage records.
- [x] Add mobile card layout for `/repos` primary repository list.
- [x] Add mobile card layout for `/repos/:id` PR usage list.
- [x] Add mobile alternatives or explicit mobile behavior for `/settings` primary admin tables.
- [x] Verify screenshots at 375x812 and 390x900 for `/`, `/user`, `/events`, `/repos`, `/repos/:id`, `/settings`, `/admin/users`.

## Task 2: Harden Dialogs, Drawers, And Keyboard Access

- [x] Add reusable lightweight dialog/focus handling or local page-level helpers that match current Vue/Tailwind style.
- [x] Make the mobile navigation drawer a modal dialog with focus return and Escape close.
- [x] Make the event detail drawer keyboard-accessible and labelled.
- [x] Make the add repository dialog keyboard-accessible and focus the repository URL input on open.
- [x] Make settings credential, relay provider, and SCM provider dialogs keyboard-accessible.
- [x] Add keyboard equivalent for clickable event rows and repository/PR detail actions.
- [x] Add or update tests for Escape close and focus return where feasible.

## Task 3: Make Setup Task-First

- [x] Redesign `/user` as a setup progress flow with status, explanation, primary action, and advanced details per step.
- [x] Move raw command blocks below task steps or into collapsible advanced sections.
- [x] Add copy buttons for command blocks without persisting secrets.
- [x] Keep API key reveal/copy/regenerate behind explicit confirmation.
- [x] Add non-technical recovery copy: when to ask an admin, when to ask a developer, and what evidence to provide.
- [x] Update `user-view` tests for task-first labels and command copy behavior.

## Task 4: Normalize Bilingual Terminology

- [x] Add missing `en-US` and `zh-CN` keys for touched labels, validation messages, dialog titles, and risk copy.
- [x] Replace hardcoded `Group:`, `Platform:`, `API KEY`, `PROMPT`, `BASE URL`, `Path`, `Locator`, and similar labels where they are user-facing.
- [x] Keep commands, provider display names, model names, repo names, branch names, commit SHAs, and advanced raw enum values unchanged.
- [x] Add regression tests that switch locale and verify no primary UI section shows the old hardcoded labels.

## Task 5: Persist Recoverable Page State

- [x] Persist `/events` filters and pagination in URL query.
- [x] Persist `/repos` binding filter in URL query.
- [x] Persist `/settings` active section in query or hash and implement tab ARIA semantics.
- [x] Persist `/admin/users` search/page/page size in URL query.
- [x] Add tests for deep-link restore behavior.

## Task 6: Final Verification

- [x] Run `cd frontend && pnpm test`.
- [x] Run `cd frontend && pnpm run test:e2e:role`.
- [x] Capture visual screenshots under `output/playwright/` for desktop and mobile pages.
- [x] Extend visual evidence to `/login`, `/oauth/device`, 1280x800, and 1440x900 as required by the spec.
- [x] Manually inspect screenshots for mobile overflow, clipped text, hidden primary actions, dialog semantics, and language consistency.
- [x] Update `docs/architecture.md` only if implementation changes current frontend architecture or page boundary descriptions.

## Follow-Up Polish 2026-06-01

- [x] Replace false numbered progress for local-only `/user` setup steps with explicit local-check status.
- [x] Rename the dashboard metric section away from the misleading weekly scope.
- [x] Show active `/events` filter summaries while the mobile filter panel is collapsed.
- [x] Move touched fallback errors and status messages into `en-US` / `zh-CN` i18n keys.
- [x] Add focused regression coverage for the follow-up UI behavior.
- [x] Run `cd frontend && pnpm exec vue-tsc -b --pretty false`.
- [x] Run `cd frontend && pnpm test`.
- [x] Run `cd frontend && pnpm run test:e2e:role`.

## Setup Command Recommendation Polish 2026-06-01

- [x] Make setup progress recommend the shell or PowerShell CLI install command based on the browser platform.
- [x] Make setup progress recommend default `ae-cli login`; keep `ae-cli login --device` as the fallback/reference path.
- [x] Update login help text to explain browser login first and device login fallback behavior.
- [x] Add focused regression coverage for platform-specific install command selection and login recommendation.
- [x] Run `cd frontend && pnpm test user-setup-review.test.ts user-view.test.ts`.
- [x] Run `cd frontend && pnpm exec vue-tsc -b --pretty false`.
- [x] Run `cd frontend && pnpm test`.

## Setup Command Reference Dedupe 2026-06-01

- [x] Collapse command reference into an advanced command reference section.
- [x] Remove duplicated primary setup commands from the reference section.
- [x] Keep alternate OS installer, device login fallback, and recovery commands in the advanced section.
- [x] Add focused regression coverage that primary setup commands appear once.
- [x] Run `cd frontend && pnpm test user-view.test.ts`.
- [x] Run `cd frontend && pnpm exec vue-tsc -b --pretty false`.
- [x] Run `cd frontend && pnpm test`.

## Blocked Setup Guidance Polish 2026-06-01

- [x] Remove the vague "ask a developer" guidance from the blocked-setup section.
- [x] Replace it with actionable diagnosis-sharing guidance based on `ae-cli doctor`.
- [x] Add focused regression coverage for the support copy.
- [x] Run `cd frontend && pnpm test user-view.test.ts`.
- [x] Run `cd frontend && pnpm exec vue-tsc -b --pretty false`.
- [x] Run `cd frontend && pnpm test`.

## Sidebar Footer Interaction Polish 2026-06-01

- [x] Make the sidebar footer account identity display-only instead of a hidden `/user` navigation target.
- [x] Move language switching from the account footer to the sidebar header.
- [x] Keep logout as the explicit footer utility button.
- [x] Add focused `AppSidebar` regression coverage for the footer interaction contract.
- [x] Verify the language toggle placement with Playwright screenshot review.
- [x] Run `cd frontend && pnpm test app-sidebar.test.ts`.
- [x] Run `cd frontend && pnpm exec vue-tsc -b --pretty false`.
- [x] Run `cd frontend && pnpm test`.
- [x] Run `cd frontend && pnpm run test:e2e:role`.

## Model Selector Polish 2026-06-01

- [x] Add a user-scoped backend models endpoint for `provider + group + platform`.
- [x] Load model choices from the selected group's platform before running the connection test.
- [x] Render loaded models as a model selector and keep manual input fallback when model loading is unavailable.
- [x] Add frontend API and `/user` regression coverage for platform-specific model choices.
- [x] Add relay regression coverage for OpenAI-compatible `/v1/models` and Gemini native `/v1beta/models`.
- [x] Run `cd frontend && pnpm test user-view.test.ts api-modules.test.ts`.
- [x] Run `cd frontend && pnpm exec vue-tsc -b --pretty false`.
- [x] Run `cd frontend && pnpm test`.
- [x] Run `cd backend && go test ./internal/relay`.
- [x] Run `cd backend && go test ./internal/handler -run '^$'`.
- [x] Verify `/user` model selector with Playwright using mocked user/provider/model APIs; screenshot saved at `output/playwright/user-model-selector.png`.
- [x] Run `cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/handler -run TestUserRelayProviderModelsUsesSelectedGroupAPIKeyAndPlatformEndpoint`.
- [x] Run `cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./...`.
