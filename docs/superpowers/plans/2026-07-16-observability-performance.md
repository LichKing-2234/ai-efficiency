# Performance Observability Baseline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver issue #135 with low-cardinality Prometheus request, dependency, database, Redis, application-cache, and sampled browser Web Vitals evidence that can support production p75/p95 analysis without collecting user or request content.

**Architecture:** `backend/internal/telemetry.Metrics` owns one explicit Prometheus registry and exposes narrow observer interfaces to the existing request middleware, Relay transport, work-item cache, database/Redis pool collectors, and Web Vitals ingestion handler. The scrape handler runs on a separately configured metrics listener that is loopback-only by default and never enters the public Gin router. The frontend uses the official `web-vitals` library, makes one stable per-page sampling decision, normalizes the initial route, and submits authenticated, bounded samples that are aggregated directly into fixed-memory histograms rather than stored as raw events.

**Tech Stack:** Go, Gin, `database/sql`, go-redis, Prometheus client_golang, Vue 3, Vue Router, TypeScript, `web-vitals`, Vitest, Grafana dashboard JSON.

## Global Constraints

- Work only in `/Users/admin/ai-efficiency/.worktrees/perf-observability-135` on `perf/observability-135`.
- Base is `docs/performance-contracts-116` at `7f2999a`; the branch explicitly merges `perf/runtime-118` through merge commit `1efd3f0`. The base already contains #119 through merged PR #139.
- Preserve the #118 request ID, normalized route, privacy-safe logs, bounded HTTP clients, request deadlines, readiness semantics, and Relay body-completion timing.
- Preserve the #119 namespace/revision/actor/role-isolated work-item cache, authoritative fallback, no-stale contract, local/distributed refresh collapse, and transactional invalidation.
- The public application router must not expose an unauthenticated scrape route. Metrics use a separate listener, default `127.0.0.1:9090`; Docker may bind `:9090` only on its un-published internal network.
- Metric labels are closed, low-cardinality values only. Never label by request ID, raw path/query, actor, user, email, cache key/value, provider, scope, date range, credentials, SQL parameters, Web Vitals metric ID, or page content.
- Request metrics use normalized Gin route templates, canonical method, status class, and release. Dependency metrics use fixed dependency/operation, canonical method, status class, and release.
- Application cache events use only stable cache name `work_items_counts` and one of `fresh`, `miss`, `stale`, `error`, `refresh`, `lease_acquired`, `lease_wait`, or `lease_failed`.
- Web Vitals accept only `LCP`, `INP`, `CLS`, and `TTFB`; normalized route templates; and a closed navigation-type set. Backend release metadata is authoritative and is not accepted from the browser.
- Web Vitals ingestion is behind the existing authenticated route group, applies a global bounded token bucket and request-body limit, stores no raw events, and returns no submitted content.
- The frontend defaults to a 10 percent sample rate, clamps `VITE_WEB_VITALS_SAMPLE_RATE` to `[0,1]`, captures the initial route without query strings or parameter values, and sends nothing without an access token.
- Use synthetic identities, routes, payloads, and secrets in all tests and examples.
- Do not merge, release, tag, deploy, run Helm, or modify `sub2api`.
- Update each checkbox immediately after the action is actually complete.

**Status:** In progress. Dependency merge `1efd3f0` passed backend `go test ./...`, frontend 39 files/452 tests plus production build, ae-cli `go test ./...`, and `git diff --check` before #135 implementation.

---

### Task 1: Expose Runtime And Pool Metrics On An Internal Listener

**Files:**
- Create: `backend/internal/telemetry/metrics.go`
- Create: `backend/internal/telemetry/metrics_test.go`
- Create: `backend/internal/telemetry/pools.go`
- Create: `backend/internal/telemetry/pools_test.go`
- Create: `backend/cmd/server/metrics_server.go`
- Create: `backend/cmd/server/metrics_server_test.go`
- Modify: `backend/internal/telemetry/dependency.go`
- Modify: `backend/internal/telemetry/dependency_test.go`
- Modify: `backend/internal/middleware/request.go`
- Modify: `backend/internal/middleware/request_test.go`
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `backend/internal/config/writable_config.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/go.mod`
- Modify: `backend/go.sum`
- Modify: `deploy/config.example.yaml`
- Modify: `deploy/.env.example`
- Modify: `deploy/docker-compose.yml`
- Modify: `deploy/docker-compose.bootstrap.yml`
- Modify: `deploy/docker-compose.dev.yml`
- Modify: `deploy/docker-compose.external.yml`
- Modify: `deploy/docker-compose.local.yml`

**Interfaces:**
- Produces `telemetry.NewMetrics(release string) *Metrics`, `(*Metrics).Handler() http.Handler`, `RequestObserver`, `DependencyObserver`, `RegisterDBPool(DBStatsSource)`, and `RegisterRedisPool(RedisPoolStatsSource)`.
- Extends `middleware.RequestTelemetry(logger, release, observers ...telemetry.RequestObserver)` and `telemetry.WrapDependency(logger, release, dependency, operation, observers ...DependencyObserver)` without breaking existing callers.
- Adds `config.MetricsConfig{ListenAddress string}` with default `127.0.0.1:9090` and `AE_METRICS_LISTEN_ADDRESS` binding.

- [x] **Step 1: Add RED registry, request, dependency, pool, listener, and config tests**

  Define an isolated registry and assert exact metric families and label sets:

  ```go
  metrics := telemetry.NewMetrics("test-release")
  observer := metrics.RequestObserver()
  observer.Start("GET")
  observer.Finish("/repos/:id", "GET", "2xx", 25*time.Millisecond, 128)

  body := gatherText(t, metrics.Gatherer())
  requireContains(t, body, `ai_efficiency_http_requests_total{method="GET",release="test-release",route="/repos/:id",status_class="2xx"} 1`)
  requireNotContains(t, body, "request-alpha", "/repos/44", "alice@example.com")
  ```

  Cover request in-flight cleanup, response duration/bytes, body-completion dependency timing, canonical unknown methods, database open/in-use/idle/wait/closure stats, Redis connection/wait/duration/timeout stats, repeated scrapes without duplicate registration, internal metrics mux exposing only `/metrics`, config default/env/YAML persistence, and compose files not publishing port 9090.

  Test evidence (2026-07-16): registry tests fix exact request/dependency count, duration, byte, in-flight, release, route, method, and status labels; middleware/transport tests require normalized completion-only observer calls; pool fakes cover every required `sql.DBStats` and go-redis `PoolStats` field; server/config tests require a dedicated `/metrics`-only mux, loopback default, environment override, and startup validation.

- [x] **Step 2: Run focused tests and record RED**

  ```bash
  cd backend
  go test ./internal/telemetry ./internal/middleware ./internal/config ./cmd/server -run 'Metrics|Pool|RequestTelemetry|DependencyTelemetry' -count=1 -v
  ```

  Expected: compile failures for the absent registry, observers, pool collectors, metrics listener, and config surface.

  RED evidence (2026-07-16): focused tests failed only because `NewMetrics`, observer variadics, `RegisterDBPool`, `RegisterRedisPool`, `MetricsConfig`, and `newMetricsServer` did not exist.

- [x] **Step 3: Implement the explicit Prometheus registry and observers**

  Use `prometheus.NewRegistry`, `promhttp.HandlerFor`, histograms suitable for p75/p95 queries, and fixed label vectors:

  ```go
  type RequestObserver interface {
      Start(method string)
      Finish(route, method, statusClass string, duration time.Duration, responseBytes int)
  }

  type DependencyObserver interface {
      Observe(dependency, operation, method, statusClass string, duration time.Duration)
  }

  type DBStatsSource interface { Stats() sql.DBStats }
  type RedisPoolStatsSource interface { PoolStats() *redis.PoolStats }
  ```

  Export `ai_efficiency_http_requests_total`, request duration/response byte histograms, in-flight requests, dependency request count/duration, database connection/wait/closure metrics, and Redis pool connection/wait/duration/timeout metrics. Bind `release` once at registry construction and never accept arbitrary labels from requests.

  Implementation evidence (2026-07-16): `telemetry.Metrics` now owns an isolated registry, request/dependency counters and histograms, in-flight gauges, scrape handler, and pull-based database/Redis pool collectors. Existing request logs and Relay body-completion semantics remain unchanged while optional observers receive only normalized fields.

- [x] **Step 4: Wire the internal metrics server and operator config**

  Start a separately shutdown-aware HTTP server over `Metrics.ListenAddress`. Its mux serves only `/metrics`; it does not reuse Gin, auth middleware, SPA fallback, or the public port. Keep the config default loopback-only, set Docker compose to `:9090` without a `ports` mapping, and document the internal-network requirement in comments.

  Wiring evidence (2026-07-16): startup injects the registry into request/dependency telemetry, registers the live SQL/Redis pools, and owns a separately shutdown-aware metrics server. Writable YAML and environment configuration use loopback by default; all five Docker compose variants set an internal `:9090` listener without publishing it.

- [x] **Step 5: Verify Task 1 GREEN and checkpoint**

  ```bash
  cd backend
  gofmt -w internal/telemetry/*.go internal/middleware/request*.go internal/config/*.go cmd/server/*.go internal/handler/router.go
  go test ./internal/telemetry ./internal/middleware ./internal/config ./internal/handler ./cmd/server -count=2
  go test -race ./internal/telemetry ./internal/middleware -count=1
  cd ..
  git diff --check
  ```

  Commit: `feat(observability): expose runtime and pool metrics`

  GREEN evidence (2026-07-16): focused GREEN runs, double telemetry/middleware/config/handler/server tests, race-enabled telemetry/middleware, all five `docker compose config` validations, and `git diff --check` passed.

### Task 2: Record Real Work-Item Cache Outcomes

**Files:**
- Modify: `backend/internal/telemetry/metrics.go`
- Modify: `backend/internal/telemetry/metrics_test.go`
- Modify: `backend/internal/workitems/cache.go`
- Modify: `backend/internal/workitems/cache_test.go`
- Create: `backend/internal/workitems/cache_metrics_test.go`
- Modify: `backend/cmd/server/main.go`

**Interfaces:**
- Produces `telemetry.CacheRecorder(name string) CacheObserver` bound once to a validated stable cache name.
- Adds optional `CountsCacheOptions.Metrics interface{ Record(outcome string) }` without changing cache authority or return values.

- [x] **Step 1: Add RED cache metrics tests at the real work-item seam**

  Add a recording observer and registry-backed tests proving:

  ```go
  cache := testCountsCache(t, store, revisions, "test", func(options *CountsCacheOptions) {
      options.Metrics = recorder
  })
  _, _ = cache.GetOrLoad(ctx, 7, "user", loader)
  _, _ = cache.GetOrLoad(ctx, 7, "user", loader)

  requireOutcome(t, recorder, "miss", 1)
  requireOutcome(t, recorder, "refresh", 1)
  requireOutcome(t, recorder, "lease_acquired", 1)
  requireOutcome(t, recorder, "fresh", 1)
  ```

  Cover cold miss, warm hit, one refresh under local collapse, cross-manager lease acquired/wait, malformed value, Redis read/acquire/TTL/write/release failure, authoritative fallback, and the unused-but-exported zero `stale` series. Assert no actor, role, revision, key, token, or cached value appears in gathered labels.

  Test evidence (2026-07-16): the real cache state machine now has recorder-backed cold/warm, 20-caller local collapse, two-manager distributed lease, malformed/schema-safe recovery, read/acquire/TTL/write/release/loader error, authoritative fallback, and stable-label registry tests. The Redis TTL error case also proves the request completes instead of spinning.

- [x] **Step 2: Run focused tests and record RED**

  ```bash
  cd backend
  go test ./internal/workitems ./internal/telemetry ./cmd/server -run 'CacheMetrics|CountsCacheMetrics' -count=1 -v
  ```

  Expected: compile failures for `CountsCacheOptions.Metrics` and the cache recorder.

  RED evidence (2026-07-16): focused tests first failed only because `CountsCacheOptions.Metrics` and `Metrics.CacheRecorder` did not exist. The added TTL-error regression then hung until interrupted because the pre-existing loop checked zero TTL before a non-miss Redis error; reordering that decision made the bounded authoritative fallback GREEN.

- [x] **Step 3: Instrument without changing cache decisions**

  Record events only where the existing cache state machine already decides them: `fresh` on a decoded hit; `miss` on Redis miss or invalid value; `error` on Redis/loader/write/release error; `refresh` immediately before the authoritative loader; `lease_acquired` after successful `SET NX`; `lease_wait` after a held lease; and `lease_failed` on lease acquire/TTL/release errors. Observer calls must be nil-safe and must not affect fallback behavior.

  Implementation evidence (2026-07-16): `CacheRecorder("work_items_counts")` preinitializes the closed outcome set and ignores unknown outcomes. `CountsCache` records once at existing logical decisions, remains nil-safe, collapses refresh metrics with the existing flight/lease ownership, and now treats a non-miss lease TTL error as immediate authoritative fallback rather than lease expiry.

- [x] **Step 4: Verify Task 2 GREEN and checkpoint**

  ```bash
  cd backend
  gofmt -w internal/workitems/cache*.go internal/telemetry/metrics*.go cmd/server/main.go
  go test ./internal/workitems ./internal/telemetry ./cmd/server -count=2
  go test -race ./internal/workitems ./internal/telemetry -count=1
  cd ..
  git diff --check
  ```

  Commit: `feat(observability): instrument work item cache outcomes`

  GREEN evidence (2026-07-16): all focused cache metrics regressions, double workitems/telemetry/server tests, race-enabled workitems/telemetry, and `git diff --check` passed.

### Task 3: Collect Sampled, Privacy-Safe Browser Web Vitals

**Files:**
- Create: `backend/internal/telemetry/webvitals.go`
- Create: `backend/internal/telemetry/webvitals_test.go`
- Create: `backend/internal/handler/web_vitals.go`
- Create: `backend/internal/handler/web_vitals_test.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/cmd/server/main.go`
- Create: `frontend/src/api/telemetry.ts`
- Create: `frontend/src/telemetry/webVitals.ts`
- Create: `frontend/src/__tests__/web-vitals.test.ts`
- Modify: `frontend/src/main.ts`
- Modify: `frontend/package.json`
- Modify: `frontend/package-lock.json`

**Interfaces:**
- Produces `telemetry.WebVitalSample`, `telemetry.NormalizeBrowserRoute(string)`, and `(*Metrics).ObserveWebVital(WebVitalSample) error`.
- Produces `handler.NewWebVitalsHandler(recorder, options)` with a 4 KiB strict JSON body and a default 50 samples/second, 100-sample burst global limiter.
- Produces `frontend/src/api/telemetry.ts::submitWebVital(sample, token, fetchImpl?)` and `startWebVitalsReporting(options?)`.

- [ ] **Step 1: Add RED backend validation, authorization, rate, and privacy tests**

  Cover the closed metric set, finite/non-negative values, route normalization for `/usage/members/:user_id` and `/repos/:id`, query/fragment stripping, unknown route fallback, closed navigation types, server-owned release, histogram units (`CLS` ratio; other metrics milliseconds converted to seconds), strict/limited JSON, 401 without auth, 202 without echo on success, and 429 after limiter exhaustion.

- [ ] **Step 2: Add RED frontend sampling and transport tests**

  Mock `web-vitals` callbacks and assert a sampled authenticated page registers exactly `onLCP`, `onINP`, `onCLS`, and `onTTFB`; a non-sampled or unauthenticated page registers/sends nothing; repeated callbacks preserve the initial normalized route; and the transmitted JSON contains exactly `metric`, `value`, `route`, and `navigation_type`. Assert raw IDs, query strings, user/email, DOM text, and parameter values are absent.

- [ ] **Step 3: Run focused tests and record RED**

  ```bash
  cd backend
  go test ./internal/telemetry ./internal/handler -run 'WebVital' -count=1 -v
  cd ../frontend
  npm test -- src/__tests__/web-vitals.test.ts
  ```

  Expected: compile/import failures for the absent backend handler/recorder and frontend `web-vitals` module.

- [ ] **Step 4: Implement authenticated aggregation and frontend sampling**

  Add `web-vitals@5.3.0`. Capture the initial browser path, normalize it through a closed route table, decide sampling once, and use authenticated `fetch(..., {method: 'POST', keepalive: true})`. Do not use metric IDs or backend-returned page content. Register the handler only under the existing protected `/api/v1/telemetry/web-vitals` group and aggregate immediately into fixed-memory Prometheus histograms.

- [ ] **Step 5: Verify Task 3 GREEN and checkpoint**

  ```bash
  cd backend
  gofmt -w internal/telemetry/webvitals*.go internal/handler/web_vitals*.go internal/handler/router.go cmd/server/main.go
  go test ./internal/telemetry ./internal/handler ./cmd/server -count=2
  cd ../frontend
  npm test -- src/__tests__/web-vitals.test.ts src/__tests__/router.test.ts src/__tests__/client.test.ts
  npm run build
  cd ..
  git diff --check
  ```

  Commit: `feat(observability): collect sampled web vitals`

### Task 4: Add Operator Views, Document, Review, And Publish

**Files:**
- Create: `deploy/observability/README.md`
- Create: `deploy/observability/grafana/ai-efficiency-performance.json`
- Create: `backend/internal/telemetry/dashboard_contract_test.go`
- Modify: `docs/architecture.md`
- Modify: this plan

- [ ] **Step 1: Add RED dashboard contract test**

  Parse the dashboard JSON and require p75/p95 HTTP and dependency histogram queries, p75 LCP/INP/CLS/TTFB queries, database and Redis pool panels, cache event panels, route/release filters, and absence of prohibited user, request-ID, query, key, provider, or scope labels.

- [ ] **Step 2: Build the baseline dashboard and operator contract**

  Add Grafana panels using `histogram_quantile(0.75, ...)` and `histogram_quantile(0.95, ...)`, plus pool gauges/counters and cache event rates. Document the separate listener, Docker internal-network rule, no raw browser-event storage, Prometheus-side retention ownership, scrape example, sample-rate build variable, expected baseline-only interpretation, and that #136 ratifies budgets after sufficient production samples.

- [ ] **Step 3: Update current architecture and plan state**

  Record the explicit registry/listener boundary, request/dependency and pool metric dimensions, work-item event outcomes, protected/rate-limited Web Vitals flow, privacy exclusions, fixed-memory aggregation, and Grafana baseline. Do not rewrite historical specs.

- [ ] **Step 4: Run full verification**

  ```bash
  git diff --check
  cd backend && go vet ./internal/telemetry ./internal/middleware ./internal/workitems ./internal/handler ./cmd/server && go test ./...
  cd ../frontend && npm test && npm run build
  cd ../ae-cli && go test ./...
  ```

  Also run race-enabled telemetry/work-item tests and parse the Grafana JSON through the dashboard contract test.

- [ ] **Step 5: Review against issue #135 and the active performance spec**

  Audit every metric name/label, p75/p95 query, request/dependency completion point, DB/Redis pool field, work-item cold/warm/collapse/lease/fallback event, frontend sampling decision, route/navigation normalization, auth/rate/body boundary, scrape isolation, raw-data retention, configuration/deploy path, compatibility signature, and synthetic test value. Fix every Critical/Important finding and rerun affected verification.

- [ ] **Step 6: Push and open a Draft PR**

  Target `docs/performance-contracts-116`. List Draft PR #138 as the base dependency, Draft PR #143 as the explicit runtime dependency, and merged PR #139/#119 as already present in the base. Close #135. Preserve all worktrees and do not merge or release.

- [ ] **Step 7: Wait for required CI and record final state**

  Record the exact implementation-head run and backend/frontend/ae-cli/deploy-static conclusions, then push one ledger commit and verify final ledger-head CI.
