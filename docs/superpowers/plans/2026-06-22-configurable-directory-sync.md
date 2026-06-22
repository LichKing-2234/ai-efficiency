# Configurable Directory Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build configurable organization-directory sync, current directory facts, admin offboarding review, relay/sub2api user disablement, and local token revocation without adding vendor-specific directory code.

**Architecture:** Add a `directorysync` backend module inside the modular monolith. Directory sources store a safe HTTP DSL that references credentials by name, runs validate/preview/apply jobs, persists normalized current departments and members from successful full-company apply runs, and derives offboarding candidates by email. Confirmed offboarding calls an optional relay disable capability and sets a per-user auth token revocation floor.

**Tech Stack:** Go, Gin, Ent, PostgreSQL/SQLite test migrations, `gopkg.in/yaml.v3`, Vue 3, Vite/Vitest, TailwindCSS, existing credential and relay provider boundaries.

**Status:** In progress on 2026-06-22. Tasks 1-5 are implemented; Postgres-backed package tests are currently blocked by the local test database connection.

## Global Constraints

- Use only generic HTTP DSL support; do not add Feishu, WeCom, LDAP group sync, or other vendor SDK code.
- Match directory members to local users by normalized email only.
- Directory Sync must not automatically assign, extend, or remove relay/sub2api subscriptions.
- Offboarding requires explicit admin confirmation and disables the upstream relay/sub2api user plus revokes local AI Efficiency tokens.
- Validate and preview runs must not update current directory facts or offboarding candidates.
- Failed apply runs must not update current directory facts or offboarding candidates.
- Only latest complete successful `full_company` apply runs can drive offboarding candidates.
- Examples, tests, templates, prompts, and command output must not contain real company domains, real employee data, real subscription groups, real API keys, real tokens, or real internal URLs.
- Built-in templates must use placeholders such as `https://directory.example.com`, `X-Directory-API-Key`, `directory_api_key`, `alice@example.com`, `bob@example.org`, `Department Alpha`, and `Department Beta`.
- Keep the existing modular-monolith deployment model; do not add a separate sync service.

---

## File Map

- Modify: `backend/ent/schema/user.go`
  Add `token_valid_after` as nullable timestamp.
- Create: `backend/ent/schema/directory_source.go`
  Persist directory source settings, DSL text, schedule flags, and last run references.
- Create: `backend/ent/schema/directory_sync_run.go`
  Persist validate/preview/apply run state, counts, warnings, summaries, and redacted preview diff.
- Create: `backend/ent/schema/directory_department.go`
  Persist current canonical department facts for a source.
- Create: `backend/ent/schema/directory_member.go`
  Persist current canonical member facts for a source and email-matched local user id.
- Create: `backend/ent/schema/directory_offboarding_action.go`
  Persist explicit admin offboarding actions and retryable status.
- Regenerate: `backend/ent/**`, `backend/ent/migrate/schema.go`
  Generated Ent model/query/update/migration code.
- Modify: `backend/internal/auth/auth.go`
  Reject access and refresh tokens issued before `users.token_valid_after`; expose user revocation helper.
- Modify: `backend/internal/auth/middleware.go`
  Pass request context into access-token validation so revocation can load the user row.
- Modify: `backend/internal/relay/provider.go`
  Add optional `UserDisabler` interface.
- Modify: `backend/internal/relay/sub2api.go`
  Implement `DisableUser` through the sub2api admin user update boundary.
- Create: `backend/internal/directorysync/dsl.go`
  Parse and validate JSON/YAML DSL.
- Create: `backend/internal/directorysync/jsonpath.go`
  Evaluate the supported JSONPath-like subset for object fields and arrays.
- Create: `backend/internal/directorysync/executor.go`
  Execute safe GET steps with credential header injection, query templating, foreach, limits, and normalized results.
- Create: `backend/internal/directorysync/service.go`
  Own source CRUD, validate, preview/apply runs, facts replacement, candidate derivation, offboarding action, and scheduler tick.
- Create: `backend/internal/directorysync/*_test.go`
  Unit tests for DSL validation, JSONPath, executor behavior, apply semantics, candidate logic, and offboarding.
- Create: `backend/internal/handler/directory.go`
  Admin HTTP handlers for sources, runs, facts, candidates, and disable action.
- Modify: `backend/internal/handler/router.go`
  Wire admin directory routes.
- Modify: `backend/cmd/server/main.go`
  Create the directory service and start/stop its in-process scheduler.
- Modify: `backend/internal/handler/handler_coverage_test.go`
  Keep full router setup compiling with the expanded `SetupRouter` signature.
- Modify: `backend/internal/handler/*directory*_test.go`
  Add admin API tests for validate, preview/apply run creation, candidates, and disable action.
- Create: `frontend/src/api/directory.ts`
  Add admin directory API wrappers.
- Modify: `frontend/src/types/index.ts`
  Add directory source, run, member, department, candidate, and action types.
- Create: `frontend/src/components/settings/DirectorySyncSettings.vue`
  Add source list, edit form, safe templates, AI prompt helper, validate/preview/run controls, and latest run state.
- Modify: `frontend/src/components/settings/OrganizationLoginSettings.vue`
  Render LDAP settings and the Directory Sync block together.
- Create: `frontend/src/views/admin/DirectoryOffboardingView.vue`
  Add offboarding candidates page with email confirmation and disable action.
- Modify: `frontend/src/router/index.ts`
  Add admin-only `/admin/directory/offboarding` route.
- Modify: `frontend/src/views/SettingsView.vue`
  Fetch credentials as template credential options and keep Organization & Login mounted for directory settings.
- Modify: `frontend/src/__tests__/settings-view.test.ts`
  Verify settings renders Directory Sync, safe templates, and AI prompt helper.
- Create: `frontend/src/__tests__/directory-sync-settings.test.ts`
  Verify API calls for save, validate, preview, run now, and prompt safety.
- Create: `frontend/src/__tests__/directory-offboarding-view.test.ts`
  Verify candidate listing, email confirmation, disable API call, and copy that subscriptions are not removed.
- Modify: `frontend/src/__tests__/router.test.ts`
  Verify admin-only offboarding route metadata.
- Modify: `docs/architecture.md`
  Document the new module, scheduler, admin surfaces, relay disable boundary, and token revocation floor.

## Task 1: Ent Schemas and Token Revocation Floor

**Files:**
- Modify: `backend/ent/schema/user.go`
- Create: `backend/ent/schema/directory_source.go`
- Create: `backend/ent/schema/directory_sync_run.go`
- Create: `backend/ent/schema/directory_department.go`
- Create: `backend/ent/schema/directory_member.go`
- Create: `backend/ent/schema/directory_offboarding_action.go`
- Regenerate: `backend/ent/**`

**Interfaces:**
- Produces: Ent clients for `DirectorySource`, `DirectorySyncRun`, `DirectoryDepartment`, `DirectoryMember`, `DirectoryOffboardingAction`
- Produces: nullable `User.TokenValidAfter`

- [x] **Step 1: Write schema regression tests**

Add tests that create a source, successful run, department, member, offboarding action, and user token revocation timestamp through Ent.

Run: `cd backend && go test ./internal/directorysync ./internal/auth`

Expected: FAIL because the new Ent schemas and user field do not exist yet.

- [x] **Step 2: Add Ent schemas**

Add the schema files from the file map with enum values from the approved spec and indexes on source/run/status/email fields.

- [x] **Step 3: Generate Ent code**

Run: `cd backend && go generate ./ent`

Expected: Ent generation completes and adds the new generated files.

- [x] **Step 4: Verify generated packages**

Run: `cd backend && go test ./ent/...`

Expected: PASS.

## Task 2: Auth Token Revocation

**Files:**
- Modify: `backend/internal/auth/auth.go`
- Modify: `backend/internal/auth/middleware.go`
- Modify: `backend/internal/auth/auth_service_test.go`

**Interfaces:**
- Produces: `ValidateAccessToken(ctx context.Context, tokenStr string) (jwt.MapClaims, error)`
- Produces: `RevokeUserTokens(ctx context.Context, userID int, revokedAt time.Time) error`

- [x] **Step 1: Write failing auth tests**

Add tests proving access tokens and refresh tokens issued before `token_valid_after` are rejected, and tokens issued after the revocation floor are accepted.

Run: `cd backend && go test ./internal/auth -run 'TokenRevocation|RefreshToken'`

Expected: FAIL because token validation does not check the user row.

- [x] **Step 2: Implement revocation checks**

Load the user by `user_id`, compare token `iat` to `token_valid_after`, reject revoked tokens, and update middleware to pass request context.

- [ ] **Step 3: Verify auth tests**

Current result: `cd backend && go test ./internal/auth -run 'TestGenerateAndValidateAccessToken|TestValidateAccessTokenRejectsRefreshToken|TestValidateTokenExpired|TestValidateTokenWrongSecret|TestValidateTokenInvalid|TestValidateRefreshToken|TestValidateAccessTokenWrongSigningMethod|TestGenerateTokenPairForUser'` passes, and `cd backend && go test ./internal/auth -run '^$'` compiles. `cd backend && go test ./internal/auth -run 'TokenRevocation'` is blocked by local Postgres connection failure before assertions run.

Run: `cd backend && go test ./internal/auth -run 'TokenRevocation|RefreshToken'`

Expected: PASS.

## Task 3: Directory DSL, JSONPath, and Executor

**Files:**
- Create: `backend/internal/directorysync/dsl.go`
- Create: `backend/internal/directorysync/jsonpath.go`
- Create: `backend/internal/directorysync/executor.go`
- Create: `backend/internal/directorysync/dsl_test.go`
- Create: `backend/internal/directorysync/jsonpath_test.go`
- Create: `backend/internal/directorysync/executor_test.go`

**Interfaces:**
- Produces: `ParseDSL(raw string) (*DSL, error)`
- Produces: `ValidateDSL(ctx context.Context, cfg *DSL, credentialExists func(context.Context, string) bool) []ValidationIssue`
- Produces: `Executor.Execute(ctx context.Context, cfg *DSL, credentials CredentialResolver) (*ExecutionResult, error)`

- [x] **Step 1: Write failing parser and validation tests**

Cover safe YAML template parsing, unsupported methods, non-HTTPS URLs, missing credential refs, duplicate step ids, missing member email mapping, and missing department id/name mappings.

Run: `cd backend && go test ./internal/directorysync -run 'TestParseDSL|TestValidateDSL'`

Expected: FAIL because the package does not exist yet.

- [x] **Step 2: Write failing JSONPath and executor tests**

Cover `$.data.items`, nested fields, foreach over prior step items, header credential injection, query template rendering, invalid email warnings, duplicate email warnings, item caps, and response-size limits.

Run: `cd backend && go test ./internal/directorysync -run 'TestJSONPath|TestExecutor'`

Expected: FAIL because evaluator and executor do not exist yet.

- [x] **Step 3: Implement parser, validator, JSONPath subset, and executor**

Use `encoding/json` and `gopkg.in/yaml.v3`; support only spec-approved first-version DSL features.

- [x] **Step 4: Verify directorysync unit tests**

Run: `cd backend && go test ./internal/directorysync -run 'TestParseDSL|TestValidateDSL|TestJSONPath|TestExecutor'`

Expected: PASS.

## Task 4: Directory Service, Runs, Facts, Candidates, and Offboarding

**Files:**
- Create: `backend/internal/directorysync/service.go`
- Create: `backend/internal/directorysync/service_test.go`
- Modify: `backend/internal/relay/provider.go`
- Modify: `backend/internal/relay/sub2api.go`
- Modify: `backend/internal/relay/sub2api_test.go`

**Interfaces:**
- Produces: `Service.ListSources`, `Service.CreateSource`, `Service.UpdateSource`, `Service.DeleteSource`
- Produces: `Service.ValidateSource`, `Service.StartRun`, `Service.GetRun`, `Service.ListRuns`
- Produces: `Service.ListDepartments`, `Service.ListMembers`, `Service.ListOffboardingCandidates`
- Produces: `Service.DisableRelayUserForCandidate`
- Produces: `relay.UserDisabler.DisableUser(ctx context.Context, userID int64) error`

- [x] **Step 1: Write failing service tests**

Cover preview not updating facts, failed apply not updating facts, successful apply replacing facts, email matching, candidate derivation, provider without disable capability returning validation error, and confirmed offboarding calling relay disable plus token revocation.

Run: `cd backend && go test ./internal/directorysync -run 'TestService'`

Expected: FAIL because service behavior is not implemented.

- [x] **Step 2: Write failing sub2api disable test**

Add an HTTP test proving `DisableUser` sends a safe admin user update and treats non-2xx responses as errors.

Run: `cd backend && go test ./internal/relay -run TestSub2apiDisableUser`

Expected: FAIL because `DisableUser` does not exist.

- [x] **Step 3: Implement service and relay disable capability**

Persist run status transitions, update current facts only after complete successful apply, derive candidates from current members, persist offboarding actions, call `relay.UserDisabler`, and call `auth.Service.RevokeUserTokens`.

- [ ] **Step 4: Verify backend service tests**

Current result: `cd backend && go test ./internal/relay -run TestDisableUser` passes, and `cd backend && go test ./internal/directorysync -run '^$'` compiles. `cd backend && go test ./internal/directorysync -run 'TestService'` is blocked by local Postgres connection failure before service assertions run.

Run: `cd backend && go test ./internal/directorysync ./internal/relay ./internal/auth`

Expected: PASS.

## Task 5: Admin HTTP API and Scheduler Wiring

**Files:**
- Create: `backend/internal/handler/directory.go`
- Create: `backend/internal/handler/directory_test.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/handler/handler_coverage_test.go`
- Modify: `backend/cmd/server/main.go`

**Interfaces:**
- Produces admin-only endpoints under `/api/v1/admin/directory/...`
- Produces in-process scheduler startup and shutdown tied to server context

- [x] **Step 1: Write failing handler tests**

Cover source CRUD, static validate, preview/apply run start, run detail, facts list, candidate list, and disable action requiring `confirm_email`.

Run: `cd backend && go test ./internal/handler -run 'Directory|SetupRouter'`

Expected: FAIL because routes are not registered.

- [x] **Step 2: Implement handler and router wiring**

Use `pkg.Success`, `pkg.Created`, `pkg.Error`, existing admin middleware, and dependency injection through `SetupRouter`.

- [x] **Step 3: Wire scheduler in server**

Instantiate `directorysync.Service` with Ent, credential resolver, auth service, and provider resolver; start its scheduler goroutine and stop it on server shutdown.

- [x] **Step 4: Verify handler tests**

Run: `cd backend && go test ./internal/handler -run 'Directory|SetupRouter'`

Expected: PASS.

## Task 6: Frontend Settings and Offboarding UI

**Files:**
- Create: `frontend/src/api/directory.ts`
- Modify: `frontend/src/types/index.ts`
- Create: `frontend/src/components/settings/DirectorySyncSettings.vue`
- Modify: `frontend/src/components/settings/OrganizationLoginSettings.vue`
- Create: `frontend/src/views/admin/DirectoryOffboardingView.vue`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/views/SettingsView.vue`
- Modify: `frontend/src/__tests__/settings-view.test.ts`
- Create: `frontend/src/__tests__/directory-sync-settings.test.ts`
- Create: `frontend/src/__tests__/directory-offboarding-view.test.ts`
- Modify: `frontend/src/__tests__/router.test.ts`

**Interfaces:**
- Produces: source management UI inside Organization & Login settings
- Produces: offboarding review UI at `/admin/directory/offboarding`
- Produces: safe template and AI prompt helper copy

- [ ] **Step 1: Write failing frontend tests**

Cover rendering Directory Sync, safe template values, copy AI prompt safety text, validate/preview/save/run API calls, candidate listing, email confirmation, and no-auto-subscription-removal copy.

Run: `cd frontend && pnpm test -- settings-view directory-sync-settings directory-offboarding-view router`

Expected: FAIL because components, API wrappers, and route do not exist yet.

- [ ] **Step 2: Implement API wrappers and types**

Add typed functions for all admin directory endpoints and shared TypeScript interfaces.

- [ ] **Step 3: Implement settings component**

Render source list, editor, templates, AI prompt helper, validate/preview/run buttons, and redacted result summaries with stable Tailwind layout.

- [ ] **Step 4: Implement offboarding page and route**

Render candidates, require exact email confirmation before disable, call disable API, and refresh candidates after action.

- [ ] **Step 5: Verify frontend targeted tests**

Run: `cd frontend && pnpm test -- settings-view directory-sync-settings directory-offboarding-view router`

Expected: PASS.

## Task 7: Architecture Documentation and Plan Ledger

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/plans/2026-06-22-configurable-directory-sync.md`

- [ ] **Step 1: Update architecture**

Document `directorysync`, persisted current directory facts, scheduler, admin settings/offboarding surfaces, relay disable boundary, and `users.token_valid_after`.

- [ ] **Step 2: Scan for unsafe example data**

Run a repo-local safety scan across the new spec, this plan, backend directorysync files, and frontend directory UI files for internal domains, bearer tokens, cloud keys, real employee emails, real department names, and real subscription group names.

Expected: no matches in new examples, tests, or templates. Safe placeholders such as `directory.example.com`, `alice@example.com`, and `Department Alpha` are allowed.

- [ ] **Step 3: Mark completed plan items**

Update this plan only for steps actually completed in this implementation run.

## Task 8: Full Verification

**Files:**
- Verify entire backend/frontend surfaces touched by this feature.

- [ ] **Step 1: Backend full test**

Run: `cd backend && go test ./...`

Expected: PASS.

- [ ] **Step 2: Frontend full test**

Run: `cd frontend && pnpm test`

Expected: PASS.

- [ ] **Step 3: Git diff hygiene**

Run: `git diff --check`

Expected: no whitespace errors.

- [ ] **Step 4: Final safety scan**

Run: `rg -n "directory\\.example\\.com|alice@example\\.com|bob@example\\.org|Department Alpha|Department Beta|Group Alpha|Group Beta" docs/superpowers/plans/2026-06-22-configurable-directory-sync.md backend/internal/directorysync frontend/src`

Expected: matches only for safe placeholder values.

## Self-Review

- Spec coverage: Tasks cover generic HTTP DSL, source CRUD, validate/preview/apply, schedule, persisted facts, email matching, offboarding candidates, relay disable, token revocation, frontend templates, prompt helper, offboarding UI, and architecture docs.
- Placeholder scan: The plan avoids unresolved placeholder markers and uses only synthetic example values.
- Type consistency: Backend service, handler, relay, and frontend API names are defined before later tasks reference them.
