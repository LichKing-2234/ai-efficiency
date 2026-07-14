# Embedded App Shell Performance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** Tasks 1-3 are complete and verified. Task 4 Steps 1-5 are complete; Step 6 is next. The branch is stacked on `docs/performance-contracts-116`.

**Goal:** Make the existing embedded SPA materially faster on cold and repeat visits by serving correct gzip/cache headers and removing inactive locale and chart code from the initial data-loading path, without adding a CDN or a separate frontend release unit.

**Architecture:** Replace the raw `http.FileServer` path with one `backend/internal/web` representation server that resolves the actual embedded file before assigning policy, lazily memoizes gzip bytes per file, and serves identity or gzip bytes through `http.ServeContent`. Keep the current route-level chunks, but make locale dictionaries dynamic and mount the app only after the active locale loads; keep chart shells, tables, empty states, and legends light while dynamically importing only canvas renderers after chartable data exists. A Vite manifest measurement command and the release embed test provide durable raw/gzip and dependency-closure evidence.

**Tech Stack:** Go 1.23+, Gin, `embed.FS`, `compress/gzip`, Vue 3, Vite 6, TypeScript, Vitest, Chart.js 4, vue-chartjs 5, Node `zlib`.

## Global Constraints

- Keep one platform release unit: `frontend/dist` is still embedded into the backend binary for Docker and release builds.
- Do not introduce a CDN, separate asset domain, separate frontend deployment, Brotli, HTTP/3, or a new frontend repository/package.
- Hashed files actually served from `/assets/` return `Cache-Control: public, max-age=31536000, immutable`.
- `index.html`, root responses, SPA fallbacks, OAuth browser entry HTML, and non-hashed static files return `Cache-Control: no-cache`.
- Cache classification follows the file actually served. An `/assets/...` miss that resolves to `index.html` is never immutable.
- HTML, JavaScript, CSS, JSON, and SVG negotiate gzip and append `Accept-Encoding` to `Vary` on both gzip and identity responses. `gzip;q=0` must remain identity.
- Preserve existing `Vary` values such as `Origin`; never apply static policy to `/api/*`, OAuth token/device-code/approval APIs, or authenticated API responses.
- Preserve GET/HEAD content type, empty HEAD bodies, SPA fallback behavior, canonical browser-path redirects, exact gzip decompression, and production's absence of fabricated `Last-Modified` values. Set the selected identity or gzip representation's exact `Content-Length` explicitly before `http.ServeContent`, because Go does not synthesize it when `Content-Encoding` is present.
- ETag is intentionally not introduced in this ticket; `no-cache` provides the required HTML revalidation contract.
- The active locale dictionary loads before `app.mount()`. While another locale loads, the current dictionary, locale, storage value, and `<html lang>` remain stable; a failed or superseded load cannot partially switch the UI.
- Locale module loads and concurrent switches are deduplicated. TypeScript enforces that Chinese and English dictionaries have the same 1,002 keys.
- Chart.js and vue-chartjs load only after non-empty chartable data exists. Loading, empty, unavailable, table, metadata, and legend UI remains in synchronous lightweight components with stable existing heights.
- Tests, fixtures, logs, plan evidence, and documentation use only synthetic identities and values.
- Update `docs/architecture.md` only for behavior that lands in this slice. Preserve the active performance spec as the target contract rather than rewriting historical specs.

## Baseline Evidence

The baseline production build at `5f6c58e6821dfcd95eefff14ea3426d454ae86cd` transforms 188 modules:

| Path or static closure | Raw bytes | Deterministic gzip bytes |
| --- | ---: | ---: |
| `dist/index.html` | 468 | 310 |
| Initial CSS | 36,710 | 6,694 |
| Initial JavaScript | 256,518 | 88,629 |
| Initial shell total | 293,696 | 95,633 |
| Chart/common chunk | 174,778 | 61,373 |
| Default `/usage` static dependency closure | 511,991 | 170,012 |

The initial entry contains both locale dictionaries. The chart/common chunk is a static dependency of Dashboard and Team Overview before their API data resolves. `frontend/index.html` also requests the nonexistent `/vite.svg`.

---

### Task 1: Embedded Representation Server

**Files:**
- Modify: `backend/internal/web/frontend.go`
- Modify: `backend/internal/web/frontend_test.go`

**Interfaces:**
- Produces: one package-private `frontendServer` created from `fs.FS`, used by both `ServeEmbeddedFrontend()` and `ServeEmbeddedIndex()`.
- Produces: actual-file resolution that returns either the requested regular file or `index.html` plus an explicit fallback flag.
- Produces: per-server lazy memoization of raw bytes plus one `sync.Once` gzip computation per resolved file.
- Produces: `acceptsGzip(string) bool`, hashed-asset classification, compressible-content classification, and append/deduplicate `Vary` helpers.
- Preserves: `HasEmbeddedFrontend`, `SetFrontendFSForTest`, API/OAuth bypass, `/index.html` canonical redirect, and the public middleware/handler signatures.

- [x] **Step 1: Add failing negotiation, policy, and fallback tests**

  Build a synthetic `dist` tree containing `index.html`, `assets/app-ABCDEFGH.js`, `assets/app-IJKLMNOP.css`, `assets/data-QRSTUVWX.json`, `assets/icon-YZabcdef.svg`, `assets/plain.js`, and a non-compressible PNG. Add table-driven requests that assert:

  - each compressible type returns identity without gzip and exact bytes after gzip decompression when `Accept-Encoding` permits gzip;
  - `gzip;q=0` and `br, gzip;q=0` remain identity, while case-insensitive `GZip`, a positive quality, and wildcard acceptance select gzip;
  - both variants append `Accept-Encoding` without replacing a pre-existing `Vary: Origin`;
  - actual hashed assets are one-year immutable, while root, nested SPA fallback, non-hashed files, `/assets`, and `/assets/missing-ABCDEFGH.js` are `no-cache` HTML or static responses according to the actual resolved file;
  - GET and HEAD have matching content type, encoding, and exact selected-representation `Content-Length`, while HEAD has no body;
  - gzip bytes decompress exactly, `Last-Modified` is absent, `/index.html` retains its canonical redirect, and API/OAuth namespaces receive no static policy.

- [x] **Step 2: Run the embedded frontend tests and record RED**

  Run: `cd backend && go test ./internal/web -run 'Embedded|Frontend|Gzip|Cache|Fallback|Head' -count=1`

  Expected: FAIL because the current `http.FileServer` path emits neither gzip/Vary nor application-owned cache policy, and it exposes test filesystem timestamps.

- [x] **Step 3: Implement the shared representation server**

  Resolve the actual regular file before policy classification, read it from the captured `dist` filesystem, derive content type from the uncompressed file extension, and store a per-file representation whose gzip bytes are produced through `sync.Once` for the five allowed MIME families. Set the exact selected byte length before calling `http.ServeContent` with a zero modification time and a reader over those identity/gzip bytes. Set `Content-Encoding` only for gzip, append/deduplicate `Vary`, and set immutable policy only when the resolved file is a Vite-style hashed asset matching `-[A-Za-z0-9_-]{8,}\.[^/]+$` under `assets/`; all other served files are `no-cache`.

  Parse `Accept-Encoding` as case-insensitive comma-separated tokens with optional quality parameters. Explicit `gzip;q=0` overrides wildcard acceptance. Preserve current fallback and bypass rules, and keep direct OAuth index serving on the same representation implementation.

- [x] **Step 4: Run focused Task 1 verification**

  Run: `cd backend && go test ./internal/web -count=1`

  Expected: PASS with exact decompression, HEAD, fallback, Vary, and cache classification coverage.

- [x] **Step 5: Commit Task 1, then record the checkpoint**

  First check Steps 1-4 and commit the implementation plus those plan updates as `perf(web): compress and cache embedded frontend`. Only after that commit succeeds, check Step 5 and create the separate plan-evidence commit `docs(plan): record embedded delivery task 1`. The task review range includes both commits.

---

### Task 2: OAuth Browser Entries And Release Embed Contract

**Files:**
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/handler/router_frontend_test.go`
- Modify: `backend/internal/oauth/handler_test.go`
- Modify: `backend/internal/web/frontend_test.go`
- Modify: `deploy/test/release-frontend-embed-test.sh`
- Modify: `frontend/index.html`

**Interfaces:**
- Consumes: Task 1's shared `ServeEmbeddedIndex` policy.
- Produces: HEAD routes for `/oauth/authorize` and `/oauth/device` using the same handlers as GET.
- Produces: a release-only embedded HTTP assertion under `AE_ASSERT_EMBEDDED_FRONTEND=1` that finds a real built hashed asset, negotiates gzip, checks immutable/no-cache classification, and verifies decompression byte-for-byte.
- Removes: the invalid `/vite.svg` request without adding a replacement asset.

- [x] **Step 1: Add failing assembled-router and OAuth isolation tests**

  Through the assembled router, assert valid GET and HEAD OAuth authorize/device browser entries return HTML with `Cache-Control: no-cache`, correct gzip/Vary headers, and empty HEAD bodies. Assert invalid authorize JSON, `/oauth/token`, device-code, approval, `/api/*`, and authenticated API responses do not receive `Content-Encoding`, static `Cache-Control`, or static `Vary: Accept-Encoding`.

- [x] **Step 2: Run OAuth/router tests and record RED**

  Run: `cd backend && go test ./internal/oauth ./internal/handler -run 'Embedded|OAuth.*(Head|Cache|Gzip)|Frontend' -count=1`

  Expected: FAIL because OAuth browser HEAD routes do not exist. Task 1 already makes valid GET browser entries pass the shared policy assertions; protocol/API isolation must remain green.

- [x] **Step 3: Wire browser HEAD routes and remove the stale icon reference**

  Register HEAD alongside GET for only the authorize and device browser entry handlers. Keep all protocol POST routes unchanged. Remove the `/vite.svg` link from `frontend/index.html`; do not add a generic replacement icon.

- [x] **Step 4: Extend the clean release embed assertion**

  Expand the environment-gated web test to scan the real embedded `dist/assets/*.js`, select the largest hashed JavaScript asset, request identity and gzip through `ServeEmbeddedFrontend`, and assert immutable policy, exact decompression, and `gzip_bytes < identity_bytes`. Request root HTML and assert `no-cache` plus gzip. Log the two selected transfer lengths as release evidence. Update `deploy/test/release-frontend-embed-test.sh` so the guarded test regex runs both presence and HTTP-policy assertions after its clean frontend build/staging step.

- [x] **Step 5: Run focused Task 2 verification**

  Run separately:

  - `cd backend && go test ./internal/web ./internal/oauth ./internal/handler -count=1`
  - `bash deploy/test/release-frontend-embed-test.sh`

  Expected: PASS; the release test proves a freshly built real asset is embedded, compressed on transfer, exactly decompressible, and classified independently from HTML.

- [x] **Step 6: Commit Task 2, then record the checkpoint**

  First check Steps 1-5 and commit the implementation plus those plan updates as `perf(web): apply embedded browser policy`. Only after that commit succeeds, check Step 6 and create `docs(plan): record embedded delivery task 2`. The task review range includes both commits.

---

### Task 3: Active-Locale-Only Bootstrap And Switching

**Files:**
- Create: `frontend/src/locales/en-US.ts`
- Create: `frontend/src/locales/zh-CN.ts`
- Modify: `frontend/src/i18n.ts`
- Modify: `frontend/src/main.ts`
- Modify: `frontend/vitest.setup.ts`
- Create: `frontend/src/__tests__/i18n.test.ts`
- Modify locale-switch expectations in existing tests only where asynchronous settlement is observable.

**Interfaces:**
- Produces: `MessageKey` and `Messages` types from the English dictionary; Chinese uses `satisfies Messages` with no runtime English import.
- Produces: `initializeI18n(): Promise<void>`, awaited before `app.mount()`.
- Preserves: `setLocale(next: Locale): Promise<void>`, `t`, `useI18n`, `locale`, `languageToggleLabel`, and `toggleLocale`; a cached locale applies synchronously before the returned promise resolves so preloaded tests remain compatible.
- Produces: loaded-dictionary and in-flight-promise caches plus latest-request-wins switching.
- Produces: a test setup helper that preloads both real dictionaries after memory storage is installed, without adding either dictionary to the production entry's static imports.

- [x] **Step 1: Add failing locale loading-state tests**

  Add deferred-loader tests that assert:

  - initial bootstrap requests only the browser/storage-selected locale and resolves before copy is read;
  - two concurrent requests for one locale invoke one loader;
  - during a pending switch, old copy, `locale`, storage, and `<html lang>` remain unchanged;
  - successful load updates all four atomically and subsequent switches reuse the cached dictionary;
  - failed load retains old state and can be retried;
  - when two different switches race, only the latest requested locale commits;
  - English and Chinese compile as complete 1,002-key dictionaries.

- [x] **Step 2: Run the locale tests and record RED**

  Run: `cd frontend && npm test -- src/__tests__/i18n.test.ts src/__tests__/login-view.test.ts src/__tests__/app-sidebar.test.ts`

  Expected: compilation/test failure because both dictionaries are inline, no async bootstrap/cache exists, and switching is currently immediate and synchronous.

- [x] **Step 3: Split dictionaries and implement atomic dynamic loading**

  Move the two existing dictionary objects byte-for-byte into the locale modules. Keep only loader/state logic in `i18n.ts`, using static dynamic-import functions for `./locales/en-US` and `./locales/zh-CN`. Cache both resolved dictionaries and pending promises. Do not change `locale`, storage, document language, or active messages until the latest requested loader succeeds; on failure keep the old state. Before first mount, choose the saved/browser locale and await its loader.

  In Vitest setup, install memory storage first, then dynamically import the i18n test preload helper and await both real dictionaries. This keeps existing `setLocale(...)` calls synchronous once cached while production still has no static locale import.

- [x] **Step 4: Run focused and full locale verification**

  Run separately:

  - `cd frontend && npm test -- src/__tests__/i18n.test.ts src/__tests__/login-view.test.ts src/__tests__/app-sidebar.test.ts src/__tests__/app-layout.test.ts`
  - `cd frontend && npm test`

  Expected: PASS with no untranslated flash, stable pending-switch copy, and existing language controls preserved.

- [x] **Step 5: Commit Task 3, then record the checkpoint**

  First check Steps 1-4 and commit the implementation plus those plan updates as `perf(frontend): lazy load locale dictionaries`. Only after that commit succeeds, check Step 5 and create `docs(plan): record embedded delivery task 3`. The task review range includes both commits.

---

### Task 4: Data-Triggered Chart Runtime And Build Evidence

**Files:**
- Create: `frontend/src/components/charts/LineChartCanvas.vue`
- Create: `frontend/src/components/charts/DoughnutChartCanvas.vue`
- Modify: `frontend/src/components/user/usage/UsageTrendChart.vue`
- Modify: `frontend/src/components/user/usage/UsageModelChart.vue`
- Modify: `frontend/src/components/team-usage/TeamOverviewMemberTrendChart.vue`
- Modify: `frontend/src/__tests__/dashboard-view.test.ts`
- Modify: `frontend/src/__tests__/team-overview-view.test.ts`
- Create: `frontend/scripts/measure-build.mjs`
- Modify: `frontend/package.json`
- Modify: `frontend/vite.config.ts`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Produces: two async canvas-only components containing every runtime import from Chart.js/vue-chartjs; the line canvas registers Category, Linear, Point, Line, Tooltip, Legend, and Filler, while the doughnut canvas registers Arc, Tooltip, and Legend.
- Preserves: current lightweight chart component props, loading/empty/unavailable copy, model table, team metadata/legends, and fixed `h-72`, `h-52`, and `h-64` containers.
- Produces: `npm run build:measure`, which runs a manifest build and a Node script over structured Vite manifest JSON.
- Produces: a report/assertion that the entry static closure contains neither locale dictionary nor Chart.js, Dashboard/Team static closures contain no canvas renderer, `/vite.svg` is absent, and exact raw/gzip shell plus default English `/usage` closure bytes are printed. The `/usage` measurement explicitly seeds the dynamically imported English locale chunk and walks its static imports in addition to the entry and Dashboard static closures.

- [x] **Step 1: Add failing chart import-timing tests**

  Mock the async canvas modules with deferred imports. Assert Dashboard skeleton/API start, loading data, empty trend/models, and the immediate model table do not load Chart.js canvases; non-empty trend/model data loads each required canvas once. Assert Team Overview summary, unavailable/empty series, metadata, and legends render without the line canvas; the first non-empty label set loads the shared line canvas once even when multiple team chart sections are present.

- [x] **Step 2: Run the focused chart tests and record RED**

  Run: `cd frontend && npm test -- src/__tests__/dashboard-view.test.ts src/__tests__/team-overview-view.test.ts`

  Expected: FAIL because the three current chart components import and register Chart.js at module evaluation before data resolves.

- [x] **Step 3: Extract and dynamically import canvas renderers**

  Keep all shells, computed data, tables, unavailable states, and legends in their current components. Replace runtime `Line`/`Doughnut` imports with `defineAsyncComponent` loaders for the two canvas-only modules, and render those children only inside the existing non-empty data branches. Preserve the fixed canvas containers so async resolution cannot shift layout.

- [x] **Step 4: Add structured build measurement and assertions**

  Add a Vite manifest only for `build:measure`. The Node script must parse `dist/.vite/manifest.json`, walk only each chunk's static `imports`, gzip the exact emitted files with Node `zlib`, and print raw/gzip totals for the HTML+entry+CSS shell and default English `/usage` closure. The `/usage` seed set is the entry, its CSS, the Dashboard route chunk, and the manifest entry for `src/locales/en-US.ts`; recursively include each seed's static `imports`, so the active locale is counted even though bootstrap loads it dynamically. It must fail when:

  - emitted HTML contains `/vite.svg`;
  - either locale module belongs to the entry static closure;
  - either canvas module or the Chart.js runtime belongs to the entry, Dashboard, or Team static closure;
  - the expected two dynamic locale modules or async canvases are absent;
  - measured gzip bytes are not smaller than raw bytes.

  Replace the frontend CI build command with `npm run build:measure`; keep release builds on normal `npm run build` so the manifest remains measurement-only.

- [x] **Step 5: Run focused Task 4 verification and record after evidence**

  Run separately:

  - `cd frontend && npm test -- src/__tests__/dashboard-view.test.ts src/__tests__/team-overview-view.test.ts`
  - `cd frontend && npm run build:measure`

  Append the command's exact raw/gzip shell, active-locale `/usage`, locale chunk, and chart chunk values under a dated `After Evidence` table in this plan before checking this step. The measured default `/usage` static closure must exclude the chart runtime and the inactive locale; do not invent a numerical pass threshold beyond those structural requirements.

  **After Evidence (2026-07-15, `npm run build:measure`):**

  | Aggregate | Raw bytes | Gzip bytes |
  | --- | ---: | ---: |
  | Initial shell | 185370 | 64468 |
  | Default English `/usage` | 285922 | 93184 |
  | `en-US` | 56081 | 15211 |
  | `zh-CN` | 54926 | 16435 |
  | Line canvas | 359467 | 125551 |
  | Doughnut canvas | 359435 | 125538 |
  | Chart runtime | 174137 | 61103 |

  Structural assertions passed: the default English `/usage` closure includes `en-US`, excludes `zh-CN`, both canvas modules, Chart.js, and vue-chartjs; entry, Dashboard, and Team static closures likewise exclude both canvas modules and the chart runtime.

- [ ] **Step 6: Commit Task 4, then record the checkpoint**

  First check Steps 1-5 and commit the implementation plus measured plan evidence as `perf(frontend): defer chart runtime until data`. Only after that commit succeeds, check Step 6 and create `docs(plan): record embedded delivery task 4`. The task review range includes both commits.

---

### Task 5: Architecture, Review, And Delivery

**Files:**
- Modify: `docs/architecture.md`
- Modify: this plan as verification and delivery steps complete.

**Interfaces:**
- Consumes: Tasks 1-4 code, measurement output, and review evidence.
- Produces: current architecture documentation, full repository verification, independent spec/standards reviews, a pushed branch, and a draft PR targeting `docs/performance-contracts-116` until PR #138 merges.

- [ ] **Step 1: Update current architecture documentation**

  Document runtime gzip negotiation, actual-file cache classification, OAuth browser-entry reuse, active-locale bootstrap, and data-triggered Chart.js canvases. Do not describe CDN, Brotli, ETag, separate frontend deployment, or later performance tickets as implemented.

- [ ] **Step 2: Run full repository and release verification**

  Run separately:

  - `cd backend && go test ./...`
  - `cd ae-cli && go test ./...`
  - `cd frontend && npm test`
  - `cd frontend && npm run build:measure`
  - `bash deploy/test/release-frontend-embed-test.sh`
  - start Vite, then `cd frontend && npm run test:e2e:role`
  - `git diff --check`

  Expected: all suites and structural build assertions pass. Report listener/browser E2E separately from default unit suites.

- [ ] **Step 3: Perform independent spec and standards reviews**

  Generate a complete branch review package from base `5f6c58e6821dfcd95eefff14ea3426d454ae86cd`. Ask independent reviewers to verify every #117 acceptance criterion and repository standard. Resolve every Critical or Important finding, rerun covering tests, and re-review until clean.

- [ ] **Step 4: Commit documentation, then record the checkpoint**

  First check Steps 1-3 and commit architecture plus review evidence as `docs(architecture): document embedded app delivery`. Only after that commit succeeds, check Step 4 and create `docs(plan): record embedded delivery verification`.

- [ ] **Step 5: Push and open the stacked draft PR**

  Push `perf/web-shell-117` and create a draft PR targeting `docs/performance-contracts-116`. Link `#117`, state the dependency on PR #138, include exact before/after raw and gzip evidence, and report all test/review results. Confirm the final PR head/base/draft/merge state and backend/frontend/ae-cli/deploy-static checks. Only then check Step 5, set the top status to complete, commit `docs(plan): record embedded app shell delivery`, push that final ledger commit, and confirm its replacement CI run also passes.
