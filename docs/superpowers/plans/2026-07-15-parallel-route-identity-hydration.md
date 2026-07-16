# Parallel Route Identity Hydration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** Implementation, remediation, architecture documentation, full local verification, task/final reviews, draft PR delivery, first/replacement CI, and the final ledger commit are complete. The 2026-07-16 base-sync conflict resolution and required local replay verification are also complete. Post-merge final-current-head CI remains tracked only by the external GitHub gate below.

**Goal:** Start public and authenticated non-admin route chunks without waiting for current-user hydration while keeping administrator routes fail-closed and making every login, logout, refresh, and delayed redirect safe across browser-session and navigation races.

**Architecture:** Add one generation-aware browser-session owner below both Pinia and Axios. Normal login, Dev Login, logout, and same-session access-token rotation all use that owner; Axios stamps the originating session generation on requests, never writes or retries after that generation is invalidated, and emits a shared auth-expiry signal instead of navigating. A production router guard starts identity hydration without awaiting it for public and ordinary protected routes, awaits it for administrators, and gates every delayed redirect by both session and monotonic navigation generations.

**Tech Stack:** Vue 3, Vue Router 4.5, Pinia 2, TypeScript 5.6, Axios 1.7, Vite 6, Vitest 4, Vue Test Utils, Python 3, Playwright.

## Global Constraints

- Work from `docs/performance-contracts-116@5f6c58e6821dfcd95eefff14ea3426d454ae86cd`; do not stack on sibling performance branches.
- Preserve every current route path, route name, safe redirect target, OAuth query parameter, and lazy view import.
- A public route with no access token issues no `/api/v1/auth/me` request. A public route with a token may start identity hydration, but its lazy chunk and visible shell do not wait for that request.
- An authenticated non-admin route starts its lazy chunk and identity hydration concurrently. It may render its existing page-owned loading state while identity is pending.
- A route with `meta.requireAdmin` never invokes its lazy component loader or renders administrator content until current identity resolves to `role=admin` for the still-current navigation.
- Browser credentials have one owner. Normal login, Dev Login, logout, Axios refresh, request Authorization headers, Pinia token state, and `localStorage` must not write credentials through independent paths.
- A credential replacement always advances the logical session generation, even if the replacement reuses the same access-token string. Logout and final refresh failure also advance it. A successful access-token refresh within the same logical session rotates credentials without advancing it.
- An HTTP request is bound to the session generation that supplied its Authorization header. After that generation is replaced, cleared, or expired, its response cannot refresh credentials, clear a newer session, retry under a newer session, or update Pinia identity.
- Same-session A-to-A2 refresh updates Pinia and `localStorage`, retries the original request at most once under A2, and lets the original current-user flight resolve without starting a duplicate `/auth/me` request.
- Current-user hydration is single-flight per logical session generation. Concurrent callers share one promise; a settled request is cleared; a replacement generation starts a new request even when it reuses the same token string.
- Every promise-driven route follow-up captures the session generation that started its hydration and requires exact equality immediately before redirecting. Replace, clear, and expire transitions invalidate the old follow-up without requiring another navigation; same-session A-to-A2 rotation preserves the generation and remains eligible.
- Final refresh failure clears only the matching session generation and emits one shared auth-expiry signal. Axios must not set `window.location`, call the router, or independently choose a redirect.
- The current router destination owns auth-expiry policy: protected routes go to Login with their original safe `fullPath`; Login and OAuth Authorize remain visible; OAuth Device redirects to Login with its own safe `fullPath`.
- Every navigation receives a monotonic generation. After every awaited administrator hydration, the guard rejects a superseded generation before returning any non-admin or invalid-session redirect. Path equality is never sufficient, including Login A -> OAuth -> Login A.
- Keep backend authentication authoritative and preserve the existing one-refresh/one-retry HTTP contract. Do not add Redis identity caching, persisted browser user objects, backend changes, route-prefetch libraries, or a second auth store.
- Keep API calls in `frontend/src/api`, route composition in `frontend/src/router/index.ts`, route/session policy in `frontend/src/router/authGuard.ts`, and current-user state in the existing Pinia auth store.
- Use only synthetic identities and secrets such as `alice@example.com`, `bob@example.org`, `test-password`, `token-a`, `token-a2`, and `token-b` in tests and documentation.
- Update `docs/architecture.md` only after behavior lands. The active 2026-07-14 performance contract already governs this scheduling change; edit it only if implementation changes that contract, and do not rewrite historical OAuth specs.
- Browser role E2E is environment-sensitive and must be reported separately. It must run against a strict-port Vite process started from this worktree and selected through `AE_E2E_BASE_URL`; never use an already-running server as evidence.
- The draft PR targets `docs/performance-contracts-116`, links issue #122 and draft PR #138, remains open for review, and is not merged, released, deployed, or used for Helm work.
- Maintain this file as a live ledger. Check a box only after that action succeeds, update it in the same working turn, and keep `Status` aligned with every unchecked local, review, environment, and CI action.
- Never set `Status: Complete` before the final current-head CI succeeds. The final current-head CI and PR-state check remain an external GitHub delivery gate after the final ledger commit, so they are intentionally not represented by a pre-checked ledger box.

## Browser Session Interfaces

Create `frontend/src/auth/browserSession.ts` as the only persistence and generation boundary:

```ts
export interface BrowserTokenPair {
  accessToken: string
  refreshToken: string | null
}

export interface BrowserSessionSnapshot {
  generation: number
  accessToken: string | null
  refreshToken: string | null
}

export type BrowserSessionTransitionKind = 'replace' | 'rotate' | 'clear' | 'expire'

export interface BrowserSessionTransition {
  kind: BrowserSessionTransitionKind
  previous: BrowserSessionSnapshot
  current: BrowserSessionSnapshot
}

export interface AuthExpiryEvent {
  expiredGeneration: number
  clearedGeneration: number
}

export function readBrowserSession(): BrowserSessionSnapshot
export function replaceBrowserSession(tokens: BrowserTokenPair): BrowserSessionSnapshot
export function rotateBrowserSession(expectedGeneration: number, tokens: BrowserTokenPair): BrowserSessionSnapshot | null
export function clearBrowserSession(): BrowserSessionSnapshot
export function expireBrowserSession(expectedGeneration: number): AuthExpiryEvent | null
export function readLatestAuthExpiry(): AuthExpiryEvent | null
export function onBrowserSessionTransition(listener: (event: BrowserSessionTransition) => void): () => void
export function onAuthExpiry(listener: (event: AuthExpiryEvent) => void): () => void
```

`replaceBrowserSession`, `clearBrowserSession`, and `expireBrowserSession` increment the generation. `rotateBrowserSession` compares `expectedGeneration`, preserves that generation on success, and returns `null` without writing on mismatch. Every transition persists access and refresh tokens atomically from the caller's perspective, removes absent values rather than inheriting a previous login's refresh token, then synchronously notifies listeners. `expireBrowserSession` publishes exactly one expiry event only when it clears the expected current generation.

The auth store continues to expose its existing `token`, `user`, `isAuthenticated`, `isAdmin`, `login`, `logout`, and `fetchMe` members, and adds:

```ts
function ensureUser(): Promise<User | null>
function devLogin(): Promise<User | null>
```

The returned Pinia `token` remains writable for existing test/setup compatibility, but its setter delegates to `replaceBrowserSession` or `clearBrowserSession`; production views do not assign it. `login()` and `devLogin()` parse `AuthTokenPayload`, replace the session even when the token string is unchanged, and await the new generation's shared identity request.

The router exports one installable production policy with test cleanup:

```ts
export function installAuthNavigationGuards(router: Router): () => void
```

The returned disposer removes both Vue Router guards and the auth-expiry subscription. `router/index.ts` installs it once and ignores the production-lifetime disposer.

## Route And Session Matrix

| Destination / transition | Identity and session action | Chunk action | Redirect / stale-work rule |
| --- | --- | --- | --- |
| Public, no token | none | start immediately | none |
| OAuth Authorize, token/no user | start shared hydration | start immediately | remain on OAuth after success or expiry |
| OAuth Device, token/no user | start shared hydration | start immediately | remain after success; expiry goes to Login with `redirect=/oauth/device...` |
| Login, verified user | none | do not load Login | synchronously use existing safe redirect |
| Login, token/no user | start shared hydration | start immediately | verified success redirects only for the current navigation generation; expiry leaves Login visible |
| Ordinary protected, no token | none | do not load protected chunk | Login with captured safe `to.fullPath` |
| Ordinary protected, token/no user | start shared hydration | start immediately | expiry goes to Login only for the current confirmed navigation |
| Admin, token/no user | await shared hydration | start only after verified admin | after await, a superseded navigation returns no redirect; current invalid goes to Login and current non-admin goes to `/` |
| Admin, verified admin | none | start immediately | none |
| Logout during refresh A | invalidate A | unrelated current chunk unchanged | A refresh cannot write, expire again, or retry |
| Login B during refresh/hydration A | replace A with B | B identity request starts | A refresh cannot write/retry; A hydration resolves `null`; A's pending route follow-up fails its session-generation check |
| Same-token replacement | advance generation despite equal token text | new identity request starts | older response resolves `null` and cannot update identity or use the replacement user's state for a delayed redirect |
| Same-session refresh A -> A2 | rotate within the captured generation | current route continues | Pinia/storage become A2; original request retries once; one current-user flight and its still-current route follow-up remain valid |
| Final current-session refresh failure | clear matching generation and emit expiry | destination already allowed by route policy may stay visible | router applies the current destination's policy; Axios never hard-navigates |
| Admin pending -> OAuth -> admin resolves non-admin/401 | newer OAuth navigation advances navigation generation | admin loader stays uncalled; OAuth loader remains current | old admin guard returns no redirect before Vue Router cancels it |

## File Map

- Create `frontend/src/auth/browserSession.ts`: own browser token persistence, logical session generations, compare-and-rotate semantics, transition notifications, and the auth-expiry signal.
- Modify `frontend/src/api/client.ts`: stamp requests with their originating generation, key refresh single-flight by generation, use compare-and-rotate, reject stale retries, publish expiry, and remove hard navigation.
- Modify `frontend/src/stores/auth.ts`: mirror session-owner transitions into Pinia, own login/Dev Login/logout transitions, and provide generation-keyed current-user single-flight.
- Modify `frontend/src/views/LoginView.vue`: call `auth.devLogin()` and stop writing Pinia/localStorage credentials directly.
- Modify `frontend/src/__tests__/auth-store.test.ts`: prove identity single-flight, same-token replacement, stale requests resolving `null`, stale response rejection, and all store-owned credential transitions.
- Modify `frontend/src/__tests__/client.test.ts`: exercise the real registered Axios interceptor with deferred refresh/logout/login races, coherent A-to-A2 rotation including a legitimate pending Login follow-up, no hard navigation, and route-level expiry policy.
- Create `frontend/src/__tests__/client-real-axios.test.ts`: exercise the real Axios request pipeline and custom-adapter dispatch boundary after retry-interceptor validation.
- Modify `frontend/src/__tests__/login-view.test.ts`: prove Dev Login enters through the store transition rather than view-owned credential writes.
- Create `frontend/src/router/authGuard.ts`: own safe redirect resolution, parallel hydration scheduling, admin fail-closed awaits, navigation generations, delayed follow-ups, and current-route expiry policy.
- Modify `frontend/src/router/index.ts`: retain routes/imports/error handling, add the OAuth Device expiry meta policy, and install the shared guard exactly once.
- Create `frontend/src/__tests__/router-hydration.test.ts`: prove lazy-loader/request ordering, visible shells, single-flight, admin gating, navigation supersession, and different-token/same-token session replacement without navigation.
- Modify `frontend/src/__tests__/router.test.ts`: replace simplified duplicate guards with the production installer while retaining route registry and authorization regressions.
- Modify `frontend/e2e_role_test.py`: read an optional `AE_E2E_BASE_URL`, preserving `http://localhost:5173` as the direct-invocation default.
- Modify `docs/architecture.md`: record only the landed session/refresh/routing critical path and the new guard/session module responsibilities.
- Maintain `docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md`: record only completed steps and earned evidence.

---

### Task 1: Unify Browser Session Ownership And Current-User Hydration

**Files:**
- Create: `frontend/src/auth/browserSession.ts`
- Modify: `frontend/src/api/client.ts`
- Modify: `frontend/src/stores/auth.ts`
- Modify: `frontend/src/views/LoginView.vue`
- Modify: `frontend/src/__tests__/auth-store.test.ts`
- Modify: `frontend/src/__tests__/client.test.ts`
- Modify: `frontend/src/__tests__/login-view.test.ts`
- Maintain: `docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md`

**Interfaces:**
- Consumes: existing `AuthTokenPayload`, `login()`, `devLogin()`, `getMe()`, Axios request/response interceptors, Pinia auth state, and work-item count reset.
- Produces: every `Browser Session Interface` above plus store `ensureUser(): Promise<User | null>` and `devLogin(): Promise<User | null>`.
- Invariant for Task 2: an expiry is published only after the matching generation is cleared; `readLatestAuthExpiry()` returns that event so a navigation that confirms after publication can still apply it.

- [x] **Step 1: Write failing store/session tests before implementation**

Extend `auth-store.test.ts` with a typed deferred helper and these exact cases. Use `readBrowserSession()` for generation assertions and assert `getMe` call counts before resolving deferred promises:

```text
two concurrent ensureUser/fetchMe callers in generation A issue one getMe and resolve to alice
ensureUser with an already loaded user issues no getMe
a transient getMe rejection settles the flight and the next call retries
logout while generation A getMe is pending clears both tokens and A cannot restore user
normal login B while generation A getMe is pending starts a B request and A cannot overwrite bob
two normal logins that both return token-a create distinct generations and distinct getMe requests
the older different-token and same-token requests both resolve null after replacement and cannot return their fetched alice value to callers
login with no refresh token removes a refresh token left by the previous generation
mocked current-generation 401 expires the session once; stale-generation 401 does not clear the replacement
```

Preserve the existing missing-token, missing-response-data, work-item reset, and non-401 behavior tests. Replace direct production-style localStorage writes in this file with session-owner setup except the one explicit startup-persistence test.

- [x] **Step 2: Write failing real-interceptor and Dev Login tests**

Enhance the hoisted Axios harness in `client.test.ts` so `client.get('/auth/me')` executes the registered request interceptor, returns a controlled 401 through the registered response interceptor, and lets the retried callable client resolve a synthetic `/auth/me` response. Add deferred cases that use the real auth store and real registered client interceptor:

```text
logout during refresh A leaves access/refresh absent; the late refresh cannot write, retry, or emit a second expiry
normal login B during refresh A preserves B tokens and bob user; A cannot retry under B
Dev Login during refresh A installs the synthetic admin session through auth.devLogin; A cannot write or retry afterward
a 401 from a request stamped with old generation A after login B neither refreshes nor expires B
successful same-session A-to-A2 refresh retries /auth/me exactly once and leaves auth.token, localStorage, and alice user on A2
two same-generation 401 responses share one refresh request
final refresh failure clears the matching session and emits once without changing window.location
credential auth endpoint 401 and uncredentialed request 401 do not refresh or expire a session
```

Update `login-view.test.ts` so its Dev Login case calls the real store path over the mocked auth API and proves `auth.devLogin()` installs `dev-token`/`dev-refresh`, loads the synthetic admin, and redirects. Add a component-level spy on `auth.devLogin` and assert it is the credential transition invoked by the button; remove the view's direct `apiDevLogin` import so there is no second production writer.

- [x] **Step 3: Run focused tests and record RED**

Run:

```bash
(cd frontend && npm test -- src/__tests__/auth-store.test.ts src/__tests__/client.test.ts src/__tests__/login-view.test.ts)
```

Expected: FAIL because `browserSession.ts`, `ensureUser`, store-owned `devLogin`, request generation stamps, compare-and-rotate refresh, and the expiry signal do not exist; current client tests also observe the forbidden hard redirect.

**RED evidence (2026-07-15):** The exact focused command failed as expected: `auth-store.test.ts` and `client.test.ts` could not resolve the missing `@/auth/browserSession` module, while the new LoginView regression failed because the auth store did not yet expose `devLogin` (17 existing LoginView tests still passed).

- [x] **Step 4: Implement the session owner and Pinia adapter minimally**

Create the focused module directory, then create `browserSession.ts` with the exact interfaces above:

```bash
mkdir -p frontend/src/auth
```

Keep `generation` module-local, read both token keys from `localStorage` in `readBrowserSession()`, persist both keys before synchronous listeners run, and copy snapshots into events so later writes cannot mutate prior evidence.

In `auth.ts`, subscribe to `onBrowserSessionTransition` for the store's lifetime and dispose the listener with the store scope. Use a writable computed `token` adapter so existing setup code delegates into the owner. On `replace`, `clear`, or `expire`, synchronously clear `user`, clear the current-user request record, and reset previous-user work-item counts; on `rotate`, update the exposed token without discarding a verified same-session user.

Use this request record and no token-string-only reuse:

```ts
type CurrentUserRequest = {
  generation: number
  promise: Promise<User | null>
}

let currentUserRequest: CurrentUserRequest | null = null
```

`ensureUser()` returns the loaded user, returns `null` without a token, or delegates to `fetchMe()`. `fetchMe()` captures `readBrowserSession().generation`, reuses only that generation's record, and applies success/non-401/401 results only while the captured generation is current. If `getMe()` succeeds after the generation changed, the shared request promise resolves `null`; it must not return the fetched `User` while merely withholding that user from Pinia. A current mocked 401 calls `expireBrowserSession(capturedGeneration)`; a real client expiry has already advanced the generation, so the store does not clear or emit again. The `finally` block clears only its own request record.

Factor one private `installLoginPayload(payload: AuthTokenPayload): Promise<User | null>` that validates the access token, calls `replaceBrowserSession`, then calls `ensureUser`. Both `login(req)` and `devLogin()` use it. `LoginView.handleDevLogin()` becomes:

```ts
const user = await auth.devLogin()
if (user) {
  router.push('/')
}
```

Keep the existing null-data behavior by remaining on Login without installing a session or redirecting.

- [x] **Step 5: Make Axios generation-aware and remove hard navigation**

In `client.ts`, replace `refreshPromise` with an identity-bearing flight:

```ts
type AuthenticatedRequestConfig = InternalAxiosRequestConfig & {
  _authGeneration?: number
  _retry?: boolean
}

type RefreshFlight = {
  generation: number
  promise: Promise<string | null>
}
```

The request interceptor reads one `BrowserSessionSnapshot`, installs its access token, and stamps `_authGeneration`. The response interceptor refreshes only when that stamp still equals the current generation, shares a flight only for that generation, and clears only the same flight object in `finally`.

`refreshAccessToken(captured)` posts the captured refresh token, parses the current response variants, throws on a malformed response with no access token, and calls `rotateBrowserSession(captured.generation, nextTokens)`. A `null` rotation therefore means only that the session was replaced while refresh was pending: reject the original error without writing, retrying, expiring, or joining the replacement. Before retry, recheck generation and use the rotated access token. On a final failure or missing refresh token, call `expireBrowserSession(capturedGeneration)` only if it is still current, then reject the original error. Delete `clearAuthAndRedirect` and every `window.location` write.

- [x] **Step 6: Verify focused GREEN, adjacent regressions, and build**

Run:

```bash
(cd frontend && npm test -- src/__tests__/auth-store.test.ts src/__tests__/client.test.ts src/__tests__/login-view.test.ts src/__tests__/app-sidebar.test.ts src/__tests__/oauth-authorize-page.test.ts src/__tests__/oauth-device-page.test.ts)
(cd frontend && npm run build)
git diff --check
```

Expected: all selected tests PASS; build/type-check PASS; no old refresh writes/retries after logout or B login; same-token replacement starts a new identity flight; A-to-A2 refresh keeps one identity flight and synchronizes store/storage; Dev Login uses the store transition; no hard navigation remains.

**GREEN evidence (2026-07-15):** The initial exact focused plus adjacent command passed 77/77 tests across 6 files. After the C1 review remediation below, the exact command passed 79/79 tests across the same 6 files. `npm run build` completed `vue-tsc -b` and the Vite production build successfully, `git diff --check` passed, and the broader shared-client regression run passed 444/444 tests across all 39 frontend test files.

- [x] **Step 7: Obtain Task 1 review and commit only after fixes are green**

Generate the ignored review diff and `.superpowers/sdd/task-1-brief.md` with the test evidence, interfaces, and five required race scenarios:

```bash
git add -N frontend/src/auth/browserSession.ts
git diff --binary HEAD > .superpowers/sdd/task-1-review.diff
```

Obtain an independent task-level spec/quality review over that package. Fix every Critical or Important finding with a new focused RED/GREEN cycle.

**Review remediation evidence (2026-07-15):** The first independent review returned `TASK REVIEW FAIL` with Critical C1: after generation A rotated to A2 and scheduled `client(originalRequest)`, the callable Axios retry re-ran the request interceptor, which could restamp the A-originated retry under replacement generation B or retain the A2 header after logout before adapter dispatch. The harness now makes the callable client re-run the registered request interceptor before its synthetic adapter.

The focused remediation command was:

```bash
(cd frontend && npm test -- src/__tests__/client.test.ts -t 'rejects retry A')
```

The RED run failed both explicit window cases because each retry incorrectly resolved with the synthetic adapter's HTTP 200 after (1) replacement B and (2) logout/clear. After the minimal production guard, the same command passed 2/2: retry-bound configs retain their originating `_authGeneration`, and the request interceptor rejects a retry before adapter dispatch when that generation no longer matches or the current session has no token. The legitimate same-generation A-to-A2 case still passed within the complete 15/15 `client.test.ts` run. The exact Step 6 suite then passed 79/79, the full frontend suite passed 444/444 across 39 files, the production build passed, and `git diff --check` passed. Independent re-review returned `TASK REVIEW PASS` with 0 Critical, 0 Important, and 0 Minor findings after its own real-Axios custom-adapter probe passed 3/3. Step 7 remains unchecked until the Task 1 commit succeeds.

After review passes, check Steps 1-6 and record their actual evidence, then commit:

```bash
git add frontend/src/auth/browserSession.ts frontend/src/api/client.ts frontend/src/stores/auth.ts frontend/src/views/LoginView.vue frontend/src/__tests__/auth-store.test.ts frontend/src/__tests__/client.test.ts frontend/src/__tests__/login-view.test.ts docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md
git commit -m "perf(frontend): isolate browser auth generations"
```

Only after that commit succeeds, check Step 7, stage this ledger, and amend the still-unpushed Task 1 commit so the successful commit checkbox is not pre-checked:

```bash
git add docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md
git commit --amend --no-edit
git status --short
```

Expected: Task 1 review passes and the tracked worktree is clean.

---

### Task 2: Schedule Routes Safely And Make Role E2E Worktree-Selectable

**Files:**
- Create: `frontend/src/router/authGuard.ts`
- Modify: `frontend/src/router/index.ts`
- Create: `frontend/src/__tests__/router-hydration.test.ts`
- Modify: `frontend/src/__tests__/router.test.ts`
- Modify: `frontend/src/__tests__/client.test.ts`
- Modify: `frontend/e2e_role_test.py`
- Maintain: `docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md`

**Interfaces:**
- Consumes: Task 1 `ensureUser()`, browser-session snapshots, `readLatestAuthExpiry()`, `onAuthExpiry()`, and existing route meta.
- Produces: `installAuthNavigationGuards(router: Router): () => void` and `AE_E2E_BASE_URL` with default `http://localhost:5173`.
- Follow-up contract: each `PendingHydration` owns the exact `sessionGeneration` read before its `ensureUser()` call; its callback compares that value to `readBrowserSession().generation` and uses only the promise's `User | null` result.
- Admin rule: capture `navigationGeneration` before `await auth.ensureUser()` and compare it immediately afterward, before returning Login, `/`, or any other redirect.

- [x] **Step 1: Add failing production-guard ordering and navigation-race tests**

Create `router-hydration.test.ts` with a fresh Pinia, memory router, deferred mocked `getMe`, and lazy component loaders returning components with stable `data-route-skeleton` markers. Install the exported production guard and dispose it after each test. Add these observable cases without elapsed-time assertions:

```text
/usage with token: getMe starts, usage loader runs, navigation confirms, and usage skeleton renders before identity resolves
/login with token: Login loader/shell renders first; verified identity redirects to the safe query target
/oauth/authorize with token: OAuth loader/shell renders first and remains after verified identity
public routes without token: loader renders and getMe count stays zero
/settings with pending identity: settings loader stays uncalled until an admin resolves
/settings non-admin and 401: settings loader stays uncalled; current navigation redirects to / or Login respectively
one navigation: before/after follow-up paths still issue one getMe
pending Login -> OAuth: old successful hydration cannot redirect OAuth
Login A -> OAuth -> Login A: only the newest Login generation may replace, exactly once
/login?redirect=/repos, no navigation change: pending generation-A hydration then normal login with different token B; A resolves null, causes zero router.replace calls, and B completion performs only LoginView's existing component-owned router.push
/login?redirect=/repos, no navigation change: pending generation-A hydration then Dev Login that reuses token-a but installs a new session generation; A resolves null, causes zero router.replace calls, and Dev Login completion performs only LoginView's existing component-owned router.push
pending Admin -> OAuth Authorize then non-admin resolves: OAuth remains current and admin loader stays uncalled
pending Admin -> OAuth Authorize then current-session 401 resolves: OAuth remains current and admin loader stays uncalled
OAuth Device invalid identity: its chunk/shell renders before delayed redirect to Login
```

For both pending Login replacement cases, mount the real `LoginView` over mocked auth APIs, keep the Login navigation unchanged while A and B are independently deferred, and spy separately on `router.replace` (guard-owned) and `router.push` (component-owned). Resolve A after `replaceBrowserSession` has installed B, assert A's promise value is `null`, then resolve B and prove exactly the component redirect occurs. For both pending Admin -> OAuth cases, assert the OAuth loader has run before resolving identity and assert `settingsLoader` remains at zero after all promises settle. The old admin guard must return no redirect when its captured generation is no longer current.

**Step 1 RED test coverage (2026-07-15):** Added `router-hydration.test.ts` with stable route-shell markers and the complete ordering/session/navigation matrix above, including real `LoginView` different-token and same-token replacement without navigation, repeated Login generations, administrator supersession, and OAuth Device expiry behavior. A direct run failed at the intended missing production seam, `@/router/authGuard`, before any production file existed.

- [x] **Step 2: Add failing real-interceptor route-expiry tests and harness selection probe**

Extend the real Axios harness in `client.test.ts` with memory routes that use `installAuthNavigationGuards`. Drive `/auth/me` through the registered response interceptor and a failed deferred refresh, rather than mocking `getMe`, then prove:

```text
confirmed protected /usage -> /login?redirect=/usage
confirmed Login stays on Login and preserves its safe redirect query
confirmed OAuth Authorize stays on OAuth Authorize
confirmed OAuth Device -> /login?redirect=/oauth/device
no case changes window.location
an expiry published while navigation is pending is applied after the destination confirms, not to the previous route
/login?redirect=/repos with generation A: real /auth/me 401 refreshes A to A2 without changing generation, retried /auth/me resolves alice, and the legitimate pending Login follow-up calls router.replace('/repos') exactly once
```

Run this pre-implementation environment-variable probe:

```bash
(cd frontend && AE_E2E_BASE_URL=http://127.0.0.1:41732 python3 -c "import os, e2e_role_test; assert e2e_role_test.BASE == os.environ['AE_E2E_BASE_URL']")
```

Expected: FAIL because the harness still hard-codes `http://localhost:5173`.

**Step 2 RED evidence (2026-07-15):** Extended the registered Axios interceptor harness with confirmed-destination expiry policy, pending-navigation destination ownership, and legitimate same-generation A-to-A2 Login follow-up cases. The exact environment probe exited 1 with `AssertionError`, because `e2e_role_test.BASE` still ignored `AE_E2E_BASE_URL` as expected.

- [x] **Step 3: Run focused route tests and record RED**

Run:

```bash
(cd frontend && npm test -- src/__tests__/router-hydration.test.ts src/__tests__/router.test.ts src/__tests__/client.test.ts)
```

Expected: FAIL because the inline global guard serializes every lazy route, no installable generation-aware guard exists, admin redirects are not supersession-safe, and the client expiry has no destination-owned policy.

**Exact RED evidence (2026-07-15):** The exact focused command exited 1. Existing `router.test.ts` remained green at 18/18, while both new/extended suites failed during import because `@/router/authGuard` did not exist: 2 failed files, 1 passed file, and 18 existing tests passed. This is the expected pre-production failure for the missing installable route/session policy.

- [x] **Step 4: Implement the production navigation policy**

Create `authGuard.ts` with safe redirect validation and one `installAuthNavigationGuards` implementation. Maintain these closure-owned monotonic records:

```ts
type PendingHydration = {
  navigationGeneration: number
  sessionGeneration: number
  fullPath: string
  routeName: RouteRecordName | null | undefined
  kind: 'login' | 'protected' | 'oauth-device'
  safeRedirect: string | null
  promise: Promise<User | null>
}

type NavigationAttempt = {
  navigationGeneration: number
  fullPath: string
  routeName: RouteRecordName | null | undefined
}

let navigationGeneration = 0
let activeNavigation: NavigationAttempt | null = null
let confirmedNavigation: NavigationAttempt | null = null
let pendingHydration: PendingHydration | null = null
```

Each `beforeEach` increments `navigationGeneration` before any branch and stores the resulting destination identity in `activeNavigation`. Apply the Route And Session Matrix:

```text
public: when token exists and user is absent, call ensureUser without await
Login + verified user: return the existing safe redirect synchronously
ordinary protected: reject no-token immediately; otherwise call ensureUser without await
admin: reject no-token; when identity is absent await the shared ensureUser promise
```

The administrator branch captures its generation before the await. Immediately after the await, compare it to the latest `navigationGeneration`. On mismatch, return `undefined` and let Vue Router cancel the superseded navigation. Only a matching generation may recheck token/user and return Login for invalid identity, `/` for non-admin, or allow the lazy admin loader.

Store at most one `PendingHydration` for Login, ordinary protected, or OAuth Device. Read one `BrowserSessionSnapshot` immediately before starting `ensureUser()` and copy its `generation` into the pending record associated with that exact promise. `afterEach` ignores failed/cancelled navigations and updates `confirmedNavigation` only when `to.name` and `to.fullPath` still match `activeNavigation`; this also covers OAuth Authorize, which has no redirect follow-up.

Every promise-driven callback uses its resolved `User | null`; it must not substitute a later `auth.user`. Immediately before any promise-driven `router.replace`, require all of the following:

```text
active and confirmed navigation generations equal pending.navigationGeneration
current route name and fullPath equal the pending route identity
readBrowserSession().generation equals pending.sessionGeneration exactly
the resolved result itself authorizes that follow-up
```

An ordinary `replace`, `clear`, or `expire` advances the generation, so the equality check invalidates A even when the route and navigation generation did not change and B later populated `auth.user`. A same-session A-to-A2 `rotateBrowserSession` preserves the generation, so a legitimate retried A hydration may still complete the original Login follow-up. A same-path later navigation remains independently invalidated by its newer navigation generation. Expiry-driven redirects continue through the separate auth-expiry signal/current-destination policy, not through a stale hydration promise.

Subscribe to `onAuthExpiry`. If a navigation is pending, retain the event and let successful `afterEach` apply it to the confirmed destination via `readLatestAuthExpiry`; do not redirect the previous route. If no navigation is pending, schedule one microtask and recheck confirmed generation, route identity/fullPath, cleared generation, and absence of a token before applying:

```text
Login -> no redirect
OAuthAuthorize -> no redirect
meta.redirectOnAuthExpiry -> Login with safe current fullPath
non-public route -> Login with safe current fullPath
other public route -> no redirect
```

Return a disposer that unregisters `beforeEach`, `afterEach`, and the expiry listener. In `router/index.ts`, mark only OAuth Device with `meta.redirectOnAuthExpiry = true`, remove the inline guard, and call `installAuthNavigationGuards(router)` once. Preserve all route records, lazy imports, `handleRouterError`, and chunk reload behavior.

**Step 4 GREEN evidence (2026-07-15):** Added the generation-aware installable guard and production wiring, including parallel public/ordinary hydration, fail-closed administrator awaits, exact session/navigation follow-up checks, confirmed-destination expiry policy, and the OAuth Device meta marker. The first focused production run passed 37/37 tests across `router-hydration.test.ts` and the real-interceptor `client.test.ts`.

- [x] **Step 5: Make the role harness select an explicit worktree server**

In `e2e_role_test.py`, retain the current direct-run default and normalize only trailing slashes:

```py
BASE = os.environ.get("AE_E2E_BASE_URL", "http://localhost:5173").rstrip("/")
```

Update the usage docstring with both the default command and `AE_E2E_BASE_URL=http://127.0.0.1:PORT npm run test:e2e:role`. Do not change mocked role behavior, test counts, screenshots, API route patterns, or `API` unless a failing test proves it is needed.

Re-run the probe from Step 2. Expected: PASS with `BASE == http://127.0.0.1:41732`.

**Step 5 GREEN evidence (2026-07-15):** `e2e_role_test.py` now reads `AE_E2E_BASE_URL`, preserves the localhost direct-run default, and strips only trailing slashes. The exact environment probe exited 0 with the selected `http://127.0.0.1:41732` base.

- [x] **Step 6: Migrate existing router regressions to the production guard**

In `router.test.ts`, remove simplified hand-written guards. Install and dispose `installAuthNavigationGuards` on fresh local routers and Pinia instances. Retain route registry, unauthenticated safe redirect, authenticated route access, Login safe redirect, invalid-token cleanup, admin role rejection, OAuth device meta, and chunk-error assertions. Use a unique navigation or fresh router for every case; no test may depend on singleton-router state from a prior case.

**Step 6 GREEN evidence (2026-07-15):** Replaced every hand-written regression guard with the production installer on fresh memory routers and Pinia instances, retained the route registry and chunk-error assertions, and added the OAuth Device expiry meta assertion plus administrator-loader rejection. The exact three-file route/interceptor command passed 54/54 tests.

- [x] **Step 7: Verify focused/full frontend, build, and isolated role E2E**

Run:

```bash
(cd frontend && npm test -- src/__tests__/auth-store.test.ts src/__tests__/client.test.ts src/__tests__/login-view.test.ts src/__tests__/router-hydration.test.ts src/__tests__/router.test.ts src/__tests__/oauth-authorize-page.test.ts src/__tests__/oauth-device-page.test.ts)
(cd frontend && npm test)
(cd frontend && npm run build)
git diff --check
```

Then run this self-contained command from the worktree root. It chooses an available loopback port, requires this Vite PID to remain alive, waits for this server, passes the exact URL to Playwright, and kills only the owned process through `trap`:

```bash
(
  set -eu
  cd frontend
  worktree=$(git rev-parse --show-toplevel)
  port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')
  base_url="http://127.0.0.1:${port}"
  vite_log=$(mktemp -t ae-route-hydration-vite.XXXXXX)
  npm run dev -- --host 127.0.0.1 --port "$port" --strictPort >"$vite_log" 2>&1 &
  vite_pid=$!
  cleanup() {
    kill "$vite_pid" 2>/dev/null || true
    wait "$vite_pid" 2>/dev/null || true
    rm -f "$vite_log"
  }
  trap cleanup EXIT INT TERM
  ready=0
  for _ in $(seq 1 80); do
    if ! kill -0 "$vite_pid" 2>/dev/null; then
      cat "$vite_log"
      exit 1
    fi
    if curl --fail --silent --show-error "$base_url/login" >/dev/null; then
      ready=1
      break
    fi
    sleep 0.25
  done
  if [ "$ready" -ne 1 ]; then
    cat "$vite_log"
    exit 1
  fi
  printf 'role-e2e worktree=%s pid=%s base=%s\n' "$worktree" "$vite_pid" "$base_url"
  AE_E2E_BASE_URL="$base_url" npm run test:e2e:role
)
```

Expected: focused/full Vitest and build PASS; different-token and same-token Login replacement invalidate A's route follow-up without a navigation change; legitimate same-generation A-to-A2 rotation preserves it; role E2E reports all current checks passing against the printed current-worktree URL; the trap stops the printed PID. Report E2E separately from normal unit/build results.

**Step 7 local verification evidence (2026-07-15):** The exact focused command passed 98/98 tests across 7 files; the full frontend suite passed 465/465 tests across 40 files; `vue-tsc -b` and the Vite production build completed successfully; and `git diff --check` passed. The self-contained strict-port role run printed `worktree=/Users/admin/ai-efficiency/.worktrees/perf-route-hydration-122`, `pid=64312`, and `base=http://127.0.0.1:65274`, then passed 16/16 Playwright checks. After the command exited, separate `kill -0` and loopback probes confirmed PID `64312` was cleaned and port `65274` was closed. This environment-sensitive result is recorded separately from Vitest/build evidence.

**Step 7 self-review remediation evidence (2026-07-15):** Self-review found that an already handled `readLatestAuthExpiry()` event could be replayed by a later tokenless OAuth Device navigation. The focused real-interceptor/router regression, `npm test -- src/__tests__/client.test.ts -t 'does not replay a handled expiry'`, failed 1/1 because the later route returned to Login instead of remaining on OAuth Device. The pending-destination replay extension failed for the same reason, while the concurrent-newer-expiry case already passed. The guard now advances a closure-owned monotonic consumed-expiry generation only after the latest event, cleared session, and confirmed destination all still match; no-op Login/OAuth Authorize/public policy also consumes the event, and a stale queued callback cannot consume a newer event. `PendingHydration.sessionGeneration` is now read on the line immediately before its `ensureUser()` call. The expiry-focused run passed 8/8, the three-file route/interceptor run passed 56/56, the exact 7-file command passed 100/100, the full frontend suite passed 467/467 across 40 files, the production build and `git diff --check` passed, and a fresh self-contained role E2E passed 16/16 at `pid=3761`, `base=http://127.0.0.1:60699`. Post-exit probes confirmed PID `3761` was not alive and the loopback URL no longer served `/login`.

- [x] **Step 8: Obtain Task 2 review and commit only after fixes are green**

Generate the ignored review diff and `.superpowers/sdd/task-2-brief.md` with the exact server URL/PID cleanup evidence, route/session matrix, admin supersession cases, and real-interceptor outcomes:

```bash
git add -N frontend/src/router/authGuard.ts frontend/src/__tests__/router-hydration.test.ts
git diff --binary HEAD > .superpowers/sdd/task-2-review.diff
```

Obtain an independent task-level spec/quality review over that package. Fix every Critical or Important finding with focused RED/GREEN evidence and rerun Step 7 after the last production change.

**Task 2 review evidence (2026-07-15):** Independent review returned `TASK REVIEW PASS` with 0 Critical, 0 Important, and 0 Minor findings. The reviewer independently passed the exact 7-file suite at 100/100, the full frontend suite at 467/467, the production build, `git diff --check`, the `AE_E2E_BASE_URL` normalization probe, and all 16/16 role checks against an owned strict-port Vite process at `http://127.0.0.1:57513`; PID `87963` and the selected port were confirmed cleaned afterward. Step 8 remains unchecked until the Task 2 commit succeeds.

After review passes, check Steps 1-7 and record earned evidence, then commit:

```bash
git add frontend/src/router/authGuard.ts frontend/src/router/index.ts frontend/src/__tests__/router-hydration.test.ts frontend/src/__tests__/router.test.ts frontend/src/__tests__/client.test.ts frontend/e2e_role_test.py docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md
git commit -m "perf(frontend): parallelize safe route hydration"
```

Only after that commit succeeds, check Step 8 and amend the still-unpushed Task 2 commit:

```bash
git add docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md
git commit --amend --no-edit
git status --short
```

Expected: Task 2 review passes and the tracked worktree is clean.

---

### Task 3: Architecture, Full Verification, Final Reviews, And Three-Round CI Delivery

**Files:**
- Modify: `frontend/src/api/client.ts`
- Modify: `frontend/src/router/authGuard.ts`
- Modify: `frontend/src/__tests__/client.test.ts`
- Create: `frontend/src/__tests__/client-real-axios.test.ts`
- Modify: `docs/architecture.md`
- Maintain: `docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md`
- Review only: every file changed from `5f6c58e6821dfcd95eefff14ea3426d454ae86cd`

**Interfaces:**
- Consumes: Tasks 1-2, issue #122, the active performance contract, task-review evidence, and repository agent rules.
- Produces: current architecture truth, complete local verification, independent final SPEC/standards gates, an open draft PR, two evidence-bearing CI rounds, and a third final-current-head CI gate whose source of truth is GitHub.

- [x] **Step 1: Update current architecture after behavior lands**

Update the frontend runtime/auth paragraphs and module table in `docs/architecture.md` with only landed behavior:

```text
Browser access/refresh credentials are owned by one generation-aware frontend session boundary used by Pinia and Axios. Login, Dev Login, logout, and refresh cannot accept work from an invalidated generation; same-session refresh synchronizes the store and browser persistence. Public and ordinary authenticated route chunks start without waiting for identity, administrator loaders remain blocked until the current role is verified, and delayed redirects are scoped to monotonic navigation generations. Final refresh failure emits a shared expiry event; the current route chooses Login/OAuth behavior.
```

Add `frontend/src/auth/browserSession.ts` and `frontend/src/router/authGuard.ts` to the existing frontend module responsibilities. Do not change backend token contracts or rewrite historical OAuth specs.

**Step 1 architecture evidence (2026-07-15):** Updated only current `docs/architecture.md` with the landed generation-aware browser-session owner shared by Pinia and Axios, generation-safe login/logout/refresh and current-user hydration, parallel public/ordinary route chunks, fail-closed administrator loaders, monotonic navigation-generation follow-ups, and confirmed-destination expiry policy. Added explicit browser-session/identity and route/session-policy module responsibilities. Historical OAuth specs, the active performance contract, and backend token contracts were not changed.

- [x] **Step 2: Run complete repository verification with the same isolated role command**

Run exactly:

```bash
(cd backend && go test ./...)
(cd ae-cli && go test ./...)
(cd frontend && npm test)
(cd frontend && npm run build)
bash deploy/test/release-frontend-embed-test.sh
git diff --check
```

Then run the same self-contained role command used by Task 2:

```bash
(
  set -eu
  cd frontend
  worktree=$(git rev-parse --show-toplevel)
  port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')
  base_url="http://127.0.0.1:${port}"
  vite_log=$(mktemp -t ae-route-hydration-vite.XXXXXX)
  npm run dev -- --host 127.0.0.1 --port "$port" --strictPort >"$vite_log" 2>&1 &
  vite_pid=$!
  cleanup() {
    kill "$vite_pid" 2>/dev/null || true
    wait "$vite_pid" 2>/dev/null || true
    rm -f "$vite_log"
  }
  trap cleanup EXIT INT TERM
  ready=0
  for _ in $(seq 1 80); do
    if ! kill -0 "$vite_pid" 2>/dev/null; then
      cat "$vite_log"
      exit 1
    fi
    if curl --fail --silent --show-error "$base_url/login" >/dev/null; then
      ready=1
      break
    fi
    sleep 0.25
  done
  if [ "$ready" -ne 1 ]; then
    cat "$vite_log"
    exit 1
  fi
  printf 'role-e2e worktree=%s pid=%s base=%s\n' "$worktree" "$vite_pid" "$base_url"
  AE_E2E_BASE_URL="$base_url" npm run test:e2e:role
)
```

Expected: all commands PASS. Record exact Go/Vitest/E2E counts, frontend module count, embed result, printed worktree/base/PID, and cleanup. Do not check this step if any command is skipped; keep environment-sensitive E2E evidence separate.

**Step 2 repository verification evidence (2026-07-15):** Every exact command above exited 0. Backend `go test ./...` passed all 68 packages (32 packages with tests and 36 without test files); ae-cli `go test ./...` passed all 19 packages (17 packages with tests and 2 without test files). Frontend Vitest passed 40/40 files and 467/467 tests. `vue-tsc -b` plus the Vite production build passed with 190 modules transformed. The frontend-embed harness rebuilt 190 modules and ended with `ok github.com/ai-efficiency/backend/internal/web`. `git diff --check` passed.

**Step 2 environment-sensitive role evidence (2026-07-15):** The exact self-contained dynamic-port/strictPort/readiness/trap command printed `worktree=/Users/admin/ai-efficiency/.worktrees/perf-route-hydration-122`, `pid=5478`, and `base=http://127.0.0.1:54344`, then passed 16/16 Playwright role checks with 0 failures. After the command exited, separate `kill -0` and loopback `/login` probes confirmed PID `5478` was cleaned and port `54344` was closed.

**Final-review remediation verification (2026-07-15):** After the last production change, the affected real-Axios/router command passed 64/64 tests across 4 files, the exact Task 2 seven-file command passed 105/105, and the full frontend suite passed 475/475 across 41 files. Backend passed all 68 packages (32 with tests), ae-cli passed all 19 packages (17 with tests), `vue-tsc -b` and Vite passed with 190 modules, the embed harness rebuilt 190 modules and passed `backend/internal/web`, and `git diff --check 5f6c58e` passed. The embed harness emitted only its existing dependency-install/audit warnings.

**Final-review remediation role evidence (2026-07-15):** The exact strict-port command printed `worktree=/Users/admin/ai-efficiency/.worktrees/perf-route-hydration-122`, `pid=48095`, and `base=http://127.0.0.1:51954`, then passed 16/16 role checks. Independent post-exit probes reported `pid_alive=false` and `serving=false` for that PID and loopback port.

- [x] **Step 3: Run independent final SPEC and standards reviews**

Generate the ignored complete base-to-working-tree package:

```bash
git diff --binary 5f6c58e6821dfcd95eefff14ea3426d454ae86cd > .superpowers/sdd/final-review-5f6c58e..HEAD-route-hydration.diff
```

Obtain two independent reviews over that exact package:

```text
SPEC: issue #122 plus routing/identity and frontend-route tests in the 2026-07-14 performance contract
STANDARDS: AGENTS.md, Vue/Pinia/Axios/router conventions, credential security, session/navigation races, test quality, architecture accuracy, and live-ledger compliance
```

Both reviewers must explicitly answer:

```text
Can a public or ordinary chunk still be serialized behind /auth/me?
Can an admin loader run before role verification or after non-admin/401 resolution?
Can a superseded admin guard redirect a newer OAuth navigation?
Can one navigation issue duplicate current-user requests?
Can logout, normal login B, same-token replacement, or Dev Login accept stale A work?
Can same-session A-to-A2 refresh retry once and keep Pinia/storage/user coherent?
Can a pending Login hydration redirect after different-token or same-token session replacement when navigation itself did not change, or borrow B's later auth.user?
Does exact pending.sessionGeneration equality reject replace/clear/expire while preserving a legitimate same-generation A-to-A2 rotation follow-up?
Can Axios choose a hard redirect or lose the protected safe redirect?
Do protected, Login, OAuth Authorize, and OAuth Device handle real-interceptor expiry correctly?
Does role E2E prove this worktree's strict-port server and cleanup?
```

Complete every item in `Self-Review Checklist For The Implementer` and check it only after its evidence exists. Fix every Critical or Important finding. After the last production change, rerun the affected focused RED/GREEN test, all of Step 2, regenerate the package, and obtain fresh PASS/APPROVED verdicts. Record intentionally deferred Minor findings before checking this step.

**First final-review remediation (2026-07-15):** The first SPEC review returned 0 Critical / 1 Important / 0 Minor, and the first standards review returned 1 Critical / 1 Important / 0 Minor. The shared Important proved that a failed lazy or aborted navigation stranded `activeNavigation`, and that an expiry-driven Login failure was marked consumed before confirmation. The standards Critical proved with real Axios that replacement B or logout could land after retry-interceptor validation but before custom-adapter dispatch, allowing one stale A2 dispatch.

The clean focused RED run failed 4/29 targeted tests: both real-Axios replacement/logout cases resolved through a forbidden second adapter call, the failed lazy attempt left `Usage` instead of `Login`, and the aborted expiry redirect left the later confirmed `OAuthDevice` route instead of replaying to Login. The legal same-generation A-to-A2 custom-adapter retry and the cancelled-old/newer-pending destination case were already green. The minimal fix resolves the configured adapter through `axios.getAdapter`, wraps it once through a `WeakSet`, and synchronously rechecks generation/token immediately before directly delegating. The router now restores only the still-active failed attempt to the last confirmed route, preserves a newer pending attempt, retains failed redirect expiry, and consumes redirecting expiry only after the matching Login navigation confirms. The affected 4-file GREEN passed 63/63; exact/full/build/repository/E2E results are recorded in Step 2. The final package must be regenerated and fresh independent SPEC and standards PASS verdicts are still required, so Step 3 remains unchecked.

**Same-path SPEC re-review remediation (2026-07-15):** The next independent SPEC re-review found one Important: failure settlement still compared only route name/fullPath, so an older `/repos` A1 failure could settle a newer pending `/repos` A2 attempt after an intervening OAuth navigation. The formal reviewer sequence failed RED 1/1 with `redirect=/usage` instead of `/repos`. The guard now binds every normalized `to` object to its exact `NavigationAttempt` in a `WeakMap`; successful `afterEach`, failed `afterEach`, and `onError` settle or confirm only when that object's generation still equals the active generation. The targeted test passed GREEN 1/1, and the affected/exact/full/repository/E2E rerun is recorded above. At that remediation checkpoint, fresh independent verdicts were still required and Step 3 remained unchecked.

**Fresh final-review evidence (2026-07-15):** The regenerated 4,289-line / 204,226-byte package with SHA-256 `f0450588fb20dc1ebc8d2c226599fc77c134c75d7ffba23ff9619edde096b5b2` matched a fresh base-to-working-tree diff byte-for-byte. Fresh independent SPEC review returned `SPEC PASS` with 0 Critical, 0 Important, and 0 Minor findings after 108/108 focused tests and an additional same-path `onError` probe. Fresh independent standards review returned `QUALITY APPROVED` with 0 Critical, 0 Important, and 0 Minor findings after 6/6 adapter/router probes, the same 108/108 focused tests, the production build, and package/diff integrity checks. No Minor finding was deferred.

- [x] **Step 4: Commit architecture and earned verification/review evidence**

After Steps 1-3 are actually complete, check them and set `Status` to state: implementation, local verification, and final reviews complete; draft PR and CI pending. Commit current architecture plus earned evidence:

First commit the final-review production/test remediation that was reviewed with Steps 1-3:

```bash
git add frontend/src/api/client.ts frontend/src/router/authGuard.ts frontend/src/__tests__/client.test.ts frontend/src/__tests__/client-real-axios.test.ts
git commit -m "fix(frontend): close auth generation race windows"
```

Then commit current architecture plus earned evidence:

```bash
git add docs/architecture.md docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md
git commit -m "docs(architecture): document parallel route hydration"
```

Only after the commit succeeds, check Step 4 and amend this still-unpushed documentation commit:

```bash
git add docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md
git commit --amend --no-edit
git status --short
```

Expected: tracked worktree clean; ignored review packages/reports may remain.

- [x] **Step 5: Push, open the draft PR, and require first-round CI**

Create ignored `.superpowers/sdd/pr-122.md` with `Closes #122`, dependency on draft PR #138, session/route scheduling summary, invalid-token/admin safety, exact local test/review evidence, rollback notes, and no merge/deploy claim. Run:

```bash
git push -u origin perf/route-hydration-122
gh pr create --draft --base docs/performance-contracts-116 --head perf/route-hydration-122 --title "perf(frontend): parallelize route hydration safely" --body-file .superpowers/sdd/pr-122.md
gh pr view --json number,state,isDraft,baseRefName,headRefName,headRefOid,mergeable,mergeStateStatus,url
gh pr checks --watch --fail-fast
```

Expected: open draft, exact base/head, mergeable/clean or non-conflicting state, and first-round `backend`, `frontend`, `ae-cli`, and `deploy-static` checks green for the displayed `headRefOid`. Only after all four checks succeed, check Step 5 and record the head OID, check names, conclusions, run IDs/URLs, and timestamp. Do not yet claim replacement or final-current-head CI.

**First-round CI evidence (2026-07-15):** Draft PR #144 was open and mergeable/clean with base `docs/performance-contracts-116`, head `perf/route-hydration-122`, and head OID `d64451c01432bc13e20529951235c4be2eed93c8`. Actions run [`29397422663`](https://github.com/LichKing-2234/ai-efficiency/actions/runs/29397422663) completed successfully: `backend` 2m44s, `frontend` 54s, `ae-cli` 29s, and `deploy-static` 9s. This records first-round evidence only; replacement and final-current-head CI remain pending.

- [x] **Step 6: Commit first-round evidence, then require and verify replacement CI**

Commit only the already-earned first-round evidence and push it:

```bash
git add docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md
git commit -m "docs(plan): record first route hydration CI"
git push
gh pr checks --watch --fail-fast
```

After replacement CI is green, verify it belongs to the current evidence commit and verify PR state:

```bash
head_oid=$(git rev-parse HEAD)
test "$(git status --short)" = ""
test "$(gh pr view --json headRefOid --jq .headRefOid)" = "$head_oid"
gh pr view --json number,state,isDraft,baseRefName,headRefName,headRefOid,mergeable,mergeStateStatus,statusCheckRollup,url
```

Expected: all four replacement checks green on `head_oid`, tracked worktree clean, draft PR still open with exact base/head, and no merge/release/deploy action. Only now check Step 6 and record the replacement head/check/run evidence. Set `Status` to: implementation/reviews/local verification/first and replacement CI complete; final ledger commit and final-current-head CI pending. Never set Complete.

**Replacement CI evidence (2026-07-15):** Evidence commit `5856bcc3dfaa24ea53804877d1abf4532ebe5787` matched the clean local branch and PR #144 head. Actions run [`29397675913`](https://github.com/LichKing-2234/ai-efficiency/actions/runs/29397675913) passed `backend` 2m49s, `frontend` 1m0s, `ae-cli` 28s, and `deploy-static` 15s. GitHub reported the PR open/draft, mergeable/clean, with the exact base and head branches. The final ledger commit and its own CI remain pending.

- [x] **Step 7: Create the final ledger-only commit without pre-checking its own success**

Create the final ledger commit from evidence already earned in Steps 5-6:

```bash
git add docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md
git commit -m "docs(plan): finalize route hydration evidence"
```

Only after that commit succeeds, check Step 7, keep `Status` explicitly at final-current-head CI pending, and amend the unpushed commit so its own successful creation is recorded without inventing CI evidence:

```bash
git add docs/superpowers/plans/2026-07-15-parallel-route-identity-hydration.md
git commit --amend --no-edit
git status --short
git push
```

Expected: final ledger-only commit pushed and tracked worktree clean. The push starts CI round three. Do not edit this plan, make another commit, or set Complete after this point.

#### External Final-Current-Head Delivery Gate (GitHub Is The Source Of Truth)

This gate deliberately has no ledger checkbox: checking it would create a newer unverified commit. After Step 7's final push, run:

```bash
final_head=$(git rev-parse HEAD)
gh pr checks --watch --fail-fast
test "$(git status --short)" = ""
test "$(gh pr view --json headRefOid --jq .headRefOid)" = "$final_head"
gh pr view --json number,state,isDraft,baseRefName,headRefName,headRefOid,mergeable,mergeStateStatus,statusCheckRollup,url
```

Delivery passes only when all four third-round jobs are green for `final_head`, the worktree is clean, the PR is still open/draft with base `docs/performance-contracts-116` and head `perf/route-hydration-122`, and it is non-conflicting. GitHub checks/PR state record this mutable final gate; the committed ledger honestly records that the gate was pending when its own commit was created. Keep the worktree for review iteration; do not merge, tag, release, deploy, or run Helm.

#### 2026-07-16 Base-Sync Replay

- [x] Merge the latest `origin/docs/performance-contracts-116` without rebasing and resolve the sole `docs/architecture.md` conflict by preserving both the route/session generation-safe hydration contract and the base branch's Work Items revision invalidation plus paginated Directory offboarding contract.
- [x] Replay the required local verification after conflict resolution: `git diff --check`, backend `go test ./...` and `go vet ./...`, frontend `npm test` and `npm run build`, ae-cli `go test ./...`, the frontend-embed release harness, and the isolated role E2E command.

**Base-sync evidence (2026-07-16):** The merge incorporated base commit `7f2999a561454cb514c399839d38d3d691e590e5`; only `docs/architecture.md` conflicted. The resolved architecture retains the generation-aware browser session owner, parallel public/ordinary route hydration, fail-closed administrator loaders, and exact navigation-attempt settlement alongside the PostgreSQL Work Items revision, Redis read model, generation-safe browser refresh, and bounded paginated Directory offboarding descriptions. Backend `go test ./...` and `go vet ./...` exited 0, ae-cli `go test ./...` exited 0, frontend Vitest passed 41/41 files and 497/497 tests, and the production build transformed 190 modules. The frontend-embed harness rebuilt 190 modules and passed `backend/internal/web`; `git diff --check` passed. The worktree-owned strict-port role run used PID `1453` at `http://127.0.0.1:50703`, passed 16/16 checks, and post-exit probes confirmed both the PID and listener were gone. Push, PR mergeability, and post-merge current-head CI remain external GitHub delivery evidence and are not pre-checked in this committed ledger.

## Self-Review Checklist For The Implementer

- [x] Every browser credential writer/import found by `rg` is either the new session owner or an explicit test fixture; normal login, Dev Login, logout, and refresh use the owner.
- [x] Request generation is captured when Authorization is attached, not when the 401 happens; retries recheck generation and token synchronously at the final configured-adapter invocation boundary.
- [x] Logout and login-B races prove old refresh cannot write, retry, reach either default/custom adapter, expire, or clear the newer state.
- [x] Same-token replacement advances generation, while A-to-A2 refresh preserves generation and synchronizes store/storage.
- [x] `fetchMe()` resolves stale-generation success to `null`; `PendingHydration` captures its starting session generation and never substitutes a newer store user.
- [x] Deferred Login tests keep navigation unchanged and prove both different-token and same-token replacement suppress A's `router.replace`, while a same-generation A-to-A2 rotation permits the legitimate follow-up.
- [x] Admin guards check navigation generation after await and before every redirect; non-admin and 401 Admin -> OAuth races are covered.
- [x] Axios contains no `window.location` navigation; real response-interceptor tests cover protected, Login, OAuth Authorize, and OAuth Device, and real-Axios tests cover the post-interceptor adapter window.
- [x] Delayed route work checks monotonic navigation generation plus route identity/fullPath; each lifecycle callback resolves the exact attempt generation from its normalized route object, same-path reuse cannot fool it, failed active attempts restore confirmed state, and stale cancelled attempts cannot replace a genuinely pending destination.
- [x] `AE_E2E_BASE_URL` is covered by a failing/passing probe and both Task 2 and Task 3 use the exact same strict-port/start/readiness/trap command.
- [x] No checkbox, status, test count, review verdict, PR state, or CI result is recorded before it exists.
- [x] Final current-head CI is left to the explicit external GitHub gate after the final ledger-only commit; the plan never claims Complete in advance.

**Task 3 implementer self-review evidence (2026-07-15):** Production credential-write `rg` results resolve to `browserSession.ts`; the other access/refresh writes are explicit test/E2E fixtures, while login, Dev Login, logout, refresh rotation, request stamping, and expiry call the session boundary. Code inspection and real-Axios tests confirmed request-time `_authGeneration`, generation-checked rotation/expiry, a synchronous adapter-boundary generation/token check with direct configured-adapter delegation, stale `fetchMe()` returning `null`, exact pending session/navigation/route checks, exact normalized-route-object to attempt-generation lifecycle settlement, failed-attempt restoration that does not supersede a newer pending destination, post-await administrator supersession checks, and no `window.location` reference in Axios. Regressions cover logout, login B, Dev Login, same-token replacement, A-to-A2 rotation, unchanged-navigation Login follow-ups, Admin-to-OAuth races, same-path concurrent attempts, failed/cancelled navigation, failed expiry redirect replay, and destination policy for protected, Login, OAuth Authorize, and OAuth Device routes. The current `AE_E2E_BASE_URL` probe exited 0, and an exact command-template comparison found the self-contained role command exactly twice in this plan. Steps 3-7 and the external final-current-head gate remain unearned and pending.

## Plan Self-Review Record

- Spec coverage: Tasks 1-2 cover all four routing/identity contract clauses and the browser-session safety needed to make background hydration correct; Task 3 covers current architecture, full verification, independent gates, and delivery.
- Critical review closure: Task 1 gives normal login, Dev Login, logout, and Axios one generation owner; request stamps, compare-and-rotate, and the synchronous final adapter-boundary check stop stale refresh write/retry/dispatch; stale identity promises resolve `null`; A-to-A2 stays coherent.
- Session-follow-up closure: `PendingHydration.sessionGeneration` is captured with its promise and checked exactly before redirect; different-token/same-token replacement without navigation invalidates A, while same-generation rotation remains valid.
- Admin-race closure: Task 2 requires a post-await navigation-generation check before every admin redirect and exact non-admin/401 Admin -> OAuth cases.
- Redirect-policy closure: Axios emits expiry without navigating; the production guard applies protected/Login/OAuth Authorize/OAuth Device policy through real-interceptor tests.
- Expiry-consumption closure: each matching expiry is consumed once by the still-current confirmed destination; exact per-navigation object/generation identity prevents older same-path failures from clobbering newer pending work, redirecting expiry is consumed only after Login confirms, failed redirects remain replayable, later tokenless public navigation cannot replay a handled event, and an older queued callback cannot discard a concurrently published newer expiry.
- E2E closure: the harness accepts `AE_E2E_BASE_URL`; Tasks 2 and 3 repeat one self-contained dynamic-port, strict-PID, readiness, trap-cleanup command.
- Ledger closure: three CI rounds are explicit; no replacement/final evidence is pre-checked; the final mutable gate remains in GitHub after a clean final ledger commit.
- Type consistency: session functions, event fields, store return types, request stamp, `PendingHydration.sessionGeneration`, pending route record, guard installer/disposer, and E2E variable use the same names in File Map, tasks, tests, and review gates.
- Scope control: no backend, Redis, Relay, sub2api, CDN, asset-serving, release, deployment, or Helm implementation is included.
