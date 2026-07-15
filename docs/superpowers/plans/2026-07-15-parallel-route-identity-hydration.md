# Parallel Route Identity Hydration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** Draft implementation plan prepared from `docs/performance-contracts-116@5f6c58e`; independent plan review, implementation, task review, delivery, and CI remain pending.

**Goal:** Start public and authenticated non-admin route chunks without waiting for current-user hydration while keeping administrator routes fail-closed and issuing at most one current-user request for a navigation.

**Architecture:** Add a token-aware single-flight identity hydrator to the existing Pinia auth store, then move navigation policy into one testable router guard module. Public and ordinary protected routes start hydration without awaiting it, route confirmation attaches a generation-safe redirect follow-up, and administrator routes await the same shared promise before their lazy component can resolve.

**Tech Stack:** Vue 3, Vue Router 4, Pinia, TypeScript, Vite, Vitest, Vue Test Utils.

## Global Constraints

- Work from `docs/performance-contracts-116@5f6c58e6821dfcd95eefff14ea3426d454ae86cd`; do not stack on sibling performance branches.
- Preserve every current route path, route name, redirect target, OAuth query parameter, and lazy view import.
- A public route with no token issues no `/api/v1/auth/me` request. A public route with a token may start identity hydration, but its lazy chunk and visible shell must not wait for that request.
- An authenticated non-admin route starts its lazy chunk and identity hydration concurrently. It may render its existing page-owned loading state while identity is pending.
- A route with `meta.requireAdmin` must not invoke its lazy component loader or render administrator content until current identity resolves to `role=admin`.
- Invalid-token `401` behavior remains authoritative: access and refresh tokens are cleared; protected navigation converges to `/login` with the original safe redirect; administrator navigation never falls through to protected content.
- Login redirects an already verified user synchronously. When only a token exists, Login renders immediately and redirects to the existing safe target only after hydration verifies the user.
- OAuth authorize and device route chunks render immediately. Device login retains its existing unauthenticated redirect behavior after an invalid stored token is discovered.
- Current-user hydration is single-flight per current token. Concurrent callers share one promise; a settled request is cleared; a new token does not join an older token's request; a response from a logged-out or replaced token cannot repopulate user state.
- A superseded navigation cannot apply a delayed hydration redirect to the newer route.
- Keep authentication authority in the backend and existing Axios authentication/refresh path. Do not add identity caching in Redis, browser persistence for user objects, backend changes, route prefetch libraries, or a second auth store.
- Keep `frontend/src/router/index.ts` responsible for route composition and `frontend/src/router/authGuard.ts` responsible for hydration/navigation policy. Keep API calls in the existing auth store/API modules.
- Tests, docs, route fixtures, usernames, and emails use only synthetic values such as `alice@example.com` and `bob@example.org`.
- Update `docs/architecture.md` only after behavior lands. The active 2026-07-14 performance contract already governs this scheduling change; edit it only if implementation changes that contract, and do not rewrite older auth/OAuth specs.
- Browser role E2E is environment-sensitive and must be reported separately from ordinary Vitest/build results.
- The draft PR targets `docs/performance-contracts-116`, links issue #122 and draft PR #138, remains open for review, and is not merged, released, deployed, or used for Helm work in this plan.
- Maintain this plan as a live ledger: check a step only after it actually runs, and keep the top `Status` consistent with the remaining unchecked work.

## Route Scheduling Matrix

| Destination state | Identity action | Chunk action | Follow-up |
| --- | --- | --- | --- |
| Public, no token | none | start immediately | none |
| OAuth public, token and no user | start shared hydration | start immediately | device redirects only if hydration invalidates the token |
| Login, verified user | none | do not load Login | redirect synchronously to the existing safe target |
| Login, token and no user | start shared hydration | start immediately | redirect only after verified success and only if navigation is still current |
| Ordinary protected, no token | none | do not load protected chunk | redirect to Login with `to.fullPath` |
| Ordinary protected, token and no user | start shared hydration | start immediately | redirect to Login after `401` only if navigation is still current |
| Admin, token and no user | await shared hydration | start only after verified admin | invalid token goes to Login; non-admin goes to `/` |
| Admin, verified admin | none | start immediately | none |

## File Map

- `frontend/src/stores/auth.ts`: own current-user single-flight, token isolation, stale-response protection, login/logout state transitions, and `ensureUser()`.
- `frontend/src/router/authGuard.ts`: own public/protected/admin scheduling plus post-confirmation hydration redirects.
- `frontend/src/router/index.ts`: retain route definitions and install the shared guard; mark only the OAuth device route for delayed invalid-token redirect.
- `frontend/src/__tests__/auth-store.test.ts`: prove one current-user request, retry, token replacement, and stale-response safety.
- `frontend/src/__tests__/router-hydration.test.ts`: prove observable chunk/request ordering, immediate route skeletons, public behavior, admin fail-closed behavior, and navigation-race safety.
- `frontend/src/__tests__/router.test.ts`: retain route registry, safe redirect, and existing authorization regression coverage against the installed production guard.
- `docs/architecture.md`: record the landed frontend routing/identity critical path.
- `docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md`: live execution ledger and delivery evidence.

---

### Task 1: Make Current-User Hydration Token-Aware And Single-Flight

**Files:**
- Modify: `frontend/src/stores/auth.ts`
- Modify: `frontend/src/__tests__/auth-store.test.ts`
- Maintain: `docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md`

**Interfaces:**
- Consumes: existing `getMe()`, `token`, `user`, `fetchMe()`, `login()`, and `logout()` behavior.
- Produces: `ensureUser(): Promise<User | null>` and a token-keyed in-flight request shared by `ensureUser()` and `fetchMe()`.

- [ ] **Step 1: Add deferred single-flight and token-isolation tests**

Add a typed deferred helper to `auth-store.test.ts`, then add these exact cases:

```text
two concurrent ensureUser calls with token-a issue one getMe and resolve to the same alice user
ensureUser with an already loaded user issues zero getMe calls
a transient getMe rejection settles the flight and the next ensureUser retries once
logout while token-a getMe is pending clears tokens and the later token-a response cannot restore user
changing from token-a to token-b starts a separate token-b request and token-a cannot overwrite token-b user
two concurrent fetchMe calls for the same token also share the same request
```

Use only `alice@example.com`, `bob@example.org`, `token-a`, and `token-b`. Assert API call counts before resolving each deferred promise, not only after completion.

- [ ] **Step 2: Run the auth-store tests and record RED**

Run:

```bash
cd frontend && npm test -- src/__tests__/auth-store.test.ts
```

Expected: FAIL because `ensureUser` does not exist and current `fetchMe` starts one request per caller without token-generation protection.

- [ ] **Step 3: Implement the shared current-user request**

In `auth.ts`, keep one closure-owned request record:

```ts
type CurrentUserRequest = {
  token: string | null
  promise: Promise<User | null>
}

let currentUserRequest: CurrentUserRequest | null = null
```

Implement these rules without changing the backend API contract:

```ts
function ensureUser(): Promise<User | null> {
  if (user.value) return Promise.resolve(user.value)
  return fetchMe()
}
```

`fetchMe()` captures the current token, reuses only a request with that exact token, and clears only its own request record in `finally`. It applies a success or error to store state only when the current token still equals the captured token. A current-token `401` calls the existing `logout()` path; a stale request does not clear or replace the newer session. `logout()` clears the request record, and successful login clears the previous user/request before storing the new token and calling `fetchMe()`.

Return `User | null` from both success and handled-error paths so background router hydration never creates an unhandled rejected promise. Preserve the existing non-401 behavior of clearing the current user without clearing the token.

- [ ] **Step 4: Verify focused and store-adjacent GREEN**

Run:

```bash
cd frontend && npm test -- src/__tests__/auth-store.test.ts src/__tests__/app-sidebar.test.ts
cd frontend && npm run build
git diff --check
```

Expected: all tests PASS, TypeScript/build PASS, concurrent same-token calls record one `getMe`, new-token calls remain isolated, and existing login/logout/sidebar behavior remains unchanged.

- [ ] **Step 5: Commit Task 1 and record the checkpoint**

Commit implementation plus completed Steps 1-4:

```bash
git add frontend/src/stores/auth.ts frontend/src/__tests__/auth-store.test.ts docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md
git commit -m "perf(frontend): collapse current user hydration"
```

After the commit succeeds, check Step 5 and commit the ledger checkpoint:

```bash
git add docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md
git commit -m "docs(plan): record identity hydration task 1"
```

---

### Task 2: Schedule Public, Ordinary, And Admin Routes Independently

**Files:**
- Create: `frontend/src/router/authGuard.ts`
- Modify: `frontend/src/router/index.ts`
- Create: `frontend/src/__tests__/router-hydration.test.ts`
- Modify: `frontend/src/__tests__/router.test.ts`
- Maintain: `docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md`

**Interfaces:**
- Consumes: Task 1 `ensureUser(): Promise<User | null>`, `auth.isAuthenticated`, `auth.user`, and `auth.isAdmin`.
- Produces: `installAuthNavigationGuards(router: Router): void`; `router/index.ts` installs it exactly once.

- [ ] **Step 1: Add a production-guard route harness with delayed component loaders**

Create `router-hydration.test.ts` with a memory router, a Pinia instance, deferred `getMe`, and lazy component loaders that return components containing stable skeleton markers. Install the real exported guard rather than duplicating guard logic in the test.

Add these exact observable cases:

```text
ordinary /usage: getMe starts, the usage loader runs, navigation resolves, and data-route-skeleton="usage" renders before getMe resolves
public /login with a token: Login loader and shell render before getMe resolves; verified identity then redirects to the safe query target
public /oauth/authorize with a token: OAuth loader renders before getMe resolves and remains on OAuth after success
public routes without a token: loader renders and getMe call count remains zero
protected /usage invalid token: chunk may render its loading state, then 401 clears tokens and redirects to Login with redirect=/usage
admin /settings: getMe starts but the settings loader remains uncalled until an admin identity resolves
admin /settings with non-admin identity: settings loader is never called and navigation redirects through `/` to `/usage`
admin /settings with invalid token: settings loader is never called and navigation redirects to Login
one navigation: before-guard hydration plus after-confirmation follow-up still records exactly one getMe
superseded Login navigation: delayed successful hydration cannot redirect a later OAuth navigation
OAuth device with an invalid stored token: its chunk renders first, then the delayed invalidation redirects to Login
```

Use deferred-promise assertions and `flushPromises`; do not use elapsed-time thresholds.

- [ ] **Step 2: Run the route ordering tests and record RED**

Run:

```bash
cd frontend && npm test -- src/__tests__/router-hydration.test.ts src/__tests__/router.test.ts
```

Expected: FAIL because the current global guard awaits `fetchMe()` before every lazy route, has no installable scheduling module, and cannot attach navigation-safe background follow-ups.

- [ ] **Step 3: Implement one installable auth navigation policy**

Create `authGuard.ts` with the current safe-redirect validation and one exported installer:

```ts
export function installAuthNavigationGuards(router: Router): void
```

The installer registers one `beforeEach` and one `afterEach`. The before guard resets the previous pending follow-up and applies the Route Scheduling Matrix:

```text
public: start ensureUser without await when token exists and user is absent
Login + verified user: return the existing safe redirect immediately
ordinary protected: reject no-token navigation immediately; otherwise start ensureUser without await
admin: reject no token; await ensureUser when needed; recheck token; then require isAdmin before returning
```

For Login, ordinary protected routes, and the OAuth device route, store at most one pending follow-up containing the destination `fullPath`, follow-up kind, safe redirect, and the Task 1 promise. The after guard attaches to that promise only for the route just confirmed. Before calling `router.replace`, it must recheck that `router.currentRoute.value.fullPath` still equals the captured destination. A later navigation overwrites the pending follow-up, so a superseded request cannot redirect it.

Mark the existing OAuth device route with one explicit meta flag used only for post-hydration invalid-token redirect; keep `meta.public=true`. Move no route and change no lazy import.

Replace the inline guard in `router/index.ts` with exactly one `installAuthNavigationGuards(router)` call. Keep `handleRouterError`, chunk reload recovery, and all route definitions otherwise unchanged.

- [ ] **Step 4: Migrate existing router tests to the production guard**

Remove hand-written simplified guards from `router.test.ts`. Use `installAuthNavigationGuards` on its local routers and retain existing assertions for unauthenticated redirects, protected routes, safe Login redirects, invalid-token cleanup, admin role rejection, route registry, and chunk error handling.

Ensure each test creates a fresh router and Pinia or uses a unique path/query; no test may depend on a previous global-router navigation.

- [ ] **Step 5: Verify focused, full frontend, build, and role behavior**

Run:

```bash
cd frontend && npm test -- src/__tests__/auth-store.test.ts src/__tests__/router-hydration.test.ts src/__tests__/router.test.ts src/__tests__/oauth-authorize-page.test.ts src/__tests__/oauth-device-page.test.ts
cd frontend && npm test
cd frontend && npm run build
cd frontend && npm run test:e2e:role
git diff --check
```

Expected: focused and full Vitest PASS, TypeScript/build PASS, role E2E reports 16/16 when run against this worktree, ordinary/public loaders precede delayed identity completion, admin loaders remain fail-closed, and every navigation records at most one `getMe` request.

Report role E2E separately with the exact worktree server URL and process cleanup. If port 5173 is occupied by another worktree, do not treat that other server as evidence; use an isolated current-worktree port, restore any temporary harness edit byte-for-byte, and stop only the server started for this check.

- [ ] **Step 6: Commit Task 2 and record the checkpoint**

Commit implementation plus completed Steps 1-5:

```bash
git add frontend/src/router/authGuard.ts frontend/src/router/index.ts frontend/src/__tests__/router-hydration.test.ts frontend/src/__tests__/router.test.ts docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md
git commit -m "perf(frontend): parallelize safe route hydration"
```

After the commit succeeds, check Step 6 and commit the ledger checkpoint:

```bash
git add docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md
git commit -m "docs(plan): record route hydration task 2"
```

---

### Task 3: Architecture, Full Verification, Reviews, And Draft PR Delivery

**Files:**
- Modify: `docs/architecture.md`
- Maintain: `docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md`
- Review only: every file changed since `5f6c58e`

**Interfaces:**
- Consumes: Tasks 1-2, issue #122, the active performance contract, and repository agent rules.
- Produces: current architecture truth, complete verification evidence, independent review gates, and a draft PR against the contract branch.

- [ ] **Step 1: Update current architecture after behavior lands**

Update the frontend route/runtime paragraphs in `docs/architecture.md` to state only the landed behavior:

```text
Current-user hydration is single-flight per token. Public and ordinary authenticated route chunks start without waiting for identity; ordinary protected routes converge to Login after invalid-token hydration. Administrator lazy chunks remain blocked until the current role is verified. Login/OAuth shells can render while identity is pending, and delayed redirects are scoped to the navigation that started them.
```

Add `frontend/src/router/authGuard.ts` to the frontend module responsibility row. Do not change backend authentication/token contracts or rewrite historical OAuth specs.

- [ ] **Step 2: Run formatting and full repository verification**

Run exactly:

```bash
cd backend && go test ./...
cd ae-cli && go test ./...
cd frontend && npm test
cd frontend && npm run build
cd frontend && npm run test:e2e:role
bash deploy/test/release-frontend-embed-test.sh
git diff --check
```

Expected: all commands PASS. Report browser role E2E and the embedded frontend build separately as environment-sensitive evidence, including current-worktree server identity and cleanup. Do not check this step if any command is skipped.

- [ ] **Step 3: Perform task reviews and separate final SPEC/standards reviews**

After each Task 1-2 commit range, obtain an independent spec/quality review and resolve every Critical or Important finding with focused RED/GREEN evidence. Then generate one complete base-to-working-tree package from `5f6c58e` and obtain:

```text
SPEC review: issue #122 plus the 2026-07-14 routing/identity and frontend-route test contracts
standards review: AGENTS.md, Vue/Pinia/router conventions, security, navigation races, test quality, and architecture accuracy
```

The final reviews must explicitly answer:

```text
Can a public or ordinary route chunk still be serialized behind /auth/me?
Can an admin loader run before role verification or after non-admin/401 resolution?
Can one navigation issue duplicate current-user requests?
Can logout/token replacement accept a stale identity response?
Can a superseded navigation be redirected by an older hydration promise?
Do Login, OAuth authorize, and OAuth device preserve their visible redirect contracts?
```

Fix all Critical/Important findings and record any intentionally deferred Minor item before checking this step.

- [ ] **Step 4: Commit architecture and verified review evidence**

Set the top status to state that implementation, full verification, and reviews are complete while draft PR CI remains pending. Record exact test counts, role E2E server isolation, review verdicts, and any non-fatal environment warnings. Then run:

```bash
git add docs/architecture.md docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md
git commit -m "docs(architecture): document parallel route hydration"
git status --short
```

Expected: clean worktree after commit.

- [ ] **Step 5: Push and open the correctly based draft PR**

Create ignored `.superpowers/sdd/pr-122.md` with `Closes #122`, dependency on draft PR #138, route scheduling summary, invalid-token/admin safety notes, test/review evidence, and rollback notes. Then run:

```bash
git push -u origin perf/route-hydration-122
gh pr create --draft --base docs/performance-contracts-116 --head perf/route-hydration-122 --title "perf(frontend): parallelize route hydration safely" --body-file .superpowers/sdd/pr-122.md
gh pr view --json number,state,isDraft,baseRefName,headRefName,mergeable,mergeStateStatus,url
```

Expected: open draft PR, exact base/head, mergeable or clean state, no merge/release/deploy action.

- [ ] **Step 6: Require first-round CI, finalize the ledger, and require replacement CI**

Wait for `backend`, `frontend`, `ae-cli`, and `deploy-static` to succeed. Only then check all completed delivery steps, set `Status: Complete`, and commit/push the final ledger:

```bash
git add docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md
git commit -m "docs(plan): record route hydration delivery"
git push
gh pr checks --watch
```

Expected: replacement CI is green for all four jobs.

- [ ] **Step 7: Verify final branch and PR state**

Run:

```bash
git status --short --branch
gh pr view --json number,state,isDraft,baseRefName,headRefName,headRefOid,mergeable,mergeStateStatus,statusCheckRollup,url
```

Expected: clean `perf/route-hydration-122`, local HEAD equals PR head OID, draft PR remains open against `docs/performance-contracts-116`, and both CI rounds are green. Keep the worktree for review iteration; do not merge, tag, release, deploy, or run Helm.

## Self-Review Record

- Spec coverage: Task 1 supplies single-flight/token-safe identity; Task 2 supplies public/non-admin concurrency, admin gating, delayed redirect safety, and route-level evidence; Task 3 supplies current docs, full verification, reviews, and two CI rounds.
- Placeholder scan: no TBD/TODO/unspecified code or test step remains.
- Type consistency: Task 1 produces `ensureUser(): Promise<User | null>`; Task 2 consumes it and produces `installAuthNavigationGuards(router: Router): void`; Task 3 reviews those exact interfaces.
- Route consistency: every existing path/name/import remains; only OAuth device receives an explicit delayed invalid-token meta policy.
- Security consistency: admin chunks await verified identity; 401 remains authoritative; token replacement/logout reject stale user responses; no user object is persisted.
- Test consistency: all ordering/race assertions use deferred promises and call counts, never timing thresholds; every identity and redirect fixture is synthetic.
- Scope control: no backend, Redis, Relay, sub2api, CDN, static-cache, release, deployment, or Helm behavior is added.
