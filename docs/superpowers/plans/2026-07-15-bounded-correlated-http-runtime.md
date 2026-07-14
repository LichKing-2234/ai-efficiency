# Bounded And Correlated HTTP Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** Ready to implement. Task 1 is next. The branch is stacked on `docs/performance-contracts-116`.

**Goal:** Bound inbound headers, downstream HTTP work, and readiness while making every browser-to-Relay request path safely correlatable through low-cardinality structured telemetry.

**Architecture:** Keep the modular monolith and introduce one shared bounded HTTP-client factory under `backend/internal/httpclient`. Add request identity and structured timing under `backend/internal/telemetry` plus Gin middleware, use the request context to propagate correlation into a Relay transport wrapper, and keep readiness independent from liveness with one parallel two-second dependency budget.

**Tech Stack:** Go 1.23+, `net/http`, Gin, zap, PostgreSQL, Redis, existing Relay/SCM/Directory provider boundaries.

## Global Constraints

- Work from `docs/performance-contracts-116@5f6c58e6821dfcd95eefff14ea3426d454ae86cd`; do not stack on #117 or #119.
- Keep one modular-monolith backend and one embedded frontend release unit.
- Do not modify `sub2api`, introduce direct database coupling, or add a separate proxy/service.
- Preserve all public provider interfaces. Constructor extensions must be backward-compatible where tests or packages already call them.
- Server defaults are `read_header_timeout_seconds: 5` and `idle_timeout_seconds: 120`.
- Readiness has one `readiness_timeout_seconds: 2` budget shared by all dependency checks.
- Shared downstream defaults are connect 5s, TLS handshake 5s, response header 15s, overall 30s, idle connection 90s, 100 total idle connections, 20 idle connections per host, and 50 total connections per host.
- Existing stricter overall budgets remain stricter: version checks stay at 10s and quota notification webhooks stay at 5s.
- Database down is `not_ready` with HTTP 503. Redis or Relay down/not-configured is `degraded` with HTTP 200. Liveness performs no dependency work.
- Incoming `X-Request-ID` is accepted only when it is 1-128 ASCII characters from `[A-Za-z0-9._-]`; otherwise generate a UUID. Return the selected ID on every response.
- Request telemetry uses Gin route templates. Requests with no matched template use the single fixed route `unmatched`; raw paths and queries are never fallback labels.
- Request logs contain only normalized route, method, status class, duration, response bytes, release, and request ID. Dependency logs use fixed dependency/operation names and never contain URL, query, body, actor, credential, or downstream response text.
- Request IDs may appear in logs and downstream headers but never as metric labels. #118 adds structured zap events only; Prometheus/cache/pool metrics and browser Web Vitals belong to #135.
- Cancellation propagates through request contexts. No deadline path may detach or leave background HTTP requests running.
- Tests, fixtures, logs, and documentation use only synthetic identities such as `alice@example.com`.
- Update `docs/architecture.md` only after behavior lands. Do not rewrite historical specs.

## Baseline Evidence

At `5f6c58e6821dfcd95eefff14ea3426d454ae86cd` on 2026-07-15:

- `cd backend && go test ./...`: PASS.
- `cd ae-cli && go test ./...`: PASS.
- `cd frontend && npm test`: PASS, 39 files and 429 tests.
- `backend/cmd/server/main.go` constructs an `http.Server` with only `Addr` and `Handler`.
- Runtime Relay and provider-created Relay clients use `http.DefaultClient`; Bitbucket and settings create unbounded clients.
- Readiness checks database, Redis, and Relay serially and the handler always returns HTTP 200.
- The router has no shared request ID or normalized request/dependency timing middleware.

---

### Task 1: Runtime Budgets And Shared HTTP Client

**Files:**
- Create: `backend/internal/httpclient/client.go`
- Create: `backend/internal/httpclient/client_test.go`
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `backend/internal/config/writable_config.go`
- Modify: `backend/cmd/server/main.go`
- Create: `backend/cmd/server/http_server_test.go`
- Modify: `deploy/config.example.yaml`
- Modify: this plan

**Interfaces:**
- Produces `httpclient.Options` with `ConnectTimeout`, `TLSHandshakeTimeout`, `ResponseHeaderTimeout`, `OverallTimeout`, `IdleConnTimeout`, `MaxIdleConns`, `MaxIdleConnsPerHost`, and `MaxConnsPerHost`.
- Produces `type TransportWrapper func(http.RoundTripper) http.RoundTripper`.
- Produces `httpclient.New(options Options, wrappers ...TransportWrapper) *http.Client`; wrappers are applied in declaration order around one bounded `*http.Transport`.
- Produces `newHTTPServer(addr string, handler http.Handler, cfg config.ServerConfig) *http.Server`.
- Produces config fields `Server.ReadHeaderTimeoutSeconds`, `Server.IdleTimeoutSeconds`, `Server.ReadinessTimeoutSeconds`, and top-level `HTTPClient HTTPClientConfig`.

- [x] **Step 1: Add failing config and client deadline tests**

  Add config assertions for the exact defaults and `AE_SERVER_READ_HEADER_TIMEOUT_SECONDS`, `AE_SERVER_IDLE_TIMEOUT_SECONDS`, `AE_SERVER_READINESS_TIMEOUT_SECONDS`, and the individual `AE_HTTP_CLIENT_*` overrides. Assert generated writable YAML retains the non-secret runtime budget fields.

  Add `httpclient` tests with a synthetic listener/transport:

  ```go
  func TestNewBoundsResponseHeadersAndOverallRequest(t *testing.T)
  func TestNewConfiguresBoundedConnectionPool(t *testing.T)
  func TestNewAppliesTransportWrappersInOrder(t *testing.T)
  ```

  The response-header case accepts a connection but withholds headers beyond 40ms. The overall case returns headers then withholds the body. Both clients use test-only millisecond budgets and must return a timeout without inspecting raw endpoint data.

- [x] **Step 2: Run Task 1 tests and record RED**

  Run separately:

  - `cd backend && go test ./internal/config -run 'HTTP|Timeout|Writable' -count=1`
  - `cd backend && go test ./internal/httpclient -count=1`

  Expected: FAIL because the config fields and `httpclient` package do not exist.

  Evidence (2026-07-15): both commands failed as expected. The config package reported missing `ServerConfig` timeout fields and `HTTPClientConfig`; the httpclient package reported missing `New`, `Options`, and `TransportWrapper`.

- [x] **Step 3: Implement the bounded client and configuration**

  `httpclient.New` must create its own transport using a `net.Dialer`, set all exact timeout/pool fields, and set `http.Client.Timeout` to `OverallTimeout`. Do not mutate `http.DefaultTransport` or `http.DefaultClient`. Apply wrappers to the private transport and return one reusable client.

  Add integer-second config fields and defaults:

  ```go
  type HTTPClientConfig struct {
      ConnectTimeoutSeconds        int `mapstructure:"connect_timeout_seconds"`
      TLSHandshakeTimeoutSeconds   int `mapstructure:"tls_handshake_timeout_seconds"`
      ResponseHeaderTimeoutSeconds int `mapstructure:"response_header_timeout_seconds"`
      OverallTimeoutSeconds        int `mapstructure:"overall_timeout_seconds"`
      IdleConnTimeoutSeconds       int `mapstructure:"idle_conn_timeout_seconds"`
      MaxIdleConns                 int `mapstructure:"max_idle_conns"`
      MaxIdleConnsPerHost          int `mapstructure:"max_idle_conns_per_host"`
      MaxConnsPerHost              int `mapstructure:"max_conns_per_host"`
  }
  ```

  Bind all environment keys and persist the fields through `EnsureWritableConfigFile`. Add the exact values to `deploy/config.example.yaml` with comments that version/webhook clients retain stricter overall budgets.

  Evidence (2026-07-15): the focused config command passed in 0.200s and the httpclient package command passed in 0.277s after implementation.

- [x] **Step 4: Add failing server field and slow-header tests**

  Test `newHTTPServer` field values. Start it on `127.0.0.1:0` with a test-only 50ms `ReadHeaderTimeout`, write an incomplete request header over TCP, and assert the server closes the connection before a one-second test deadline. The test owns and closes its listener/server.

  Run: `cd backend && go test ./cmd/server -run 'HTTPServer|SlowHeader' -count=1`

  Expected: FAIL because `newHTTPServer` does not exist.

  Evidence (2026-07-15): the focused server command failed as expected with `undefined: newHTTPServer` at both constructor call sites.

- [x] **Step 5: Implement and verify the server constructor**

  Construct the production server through `newHTTPServer`, mapping the two configured values to `time.Duration). Do not introduce a short `ReadTimeout` or `WriteTimeout` during the current synchronous Team Overview migration.

  Run separately:

  - `cd backend && go test ./cmd/server -run 'HTTPServer|SlowHeader' -count=1`
  - `cd backend && go test ./internal/config ./internal/httpclient ./cmd/server -count=1`
  - `git diff --check`

  Expected: PASS. Record the slow-header listener test separately as environment-sensitive evidence.

  Evidence (2026-07-15): the environment-sensitive slow-header listener command passed in 0.363s; the combined config/httpclient/server command passed in 0.193s, 0.427s, and 0.781s respectively; `git diff --check` passed.

- [x] **Step 6: Commit Task 1 and record the checkpoint**

  Commit implementation plus checked Steps 1-5:

  `feat(runtime): bound HTTP server and client budgets`

  After the commit succeeds, check Step 6 and commit:

  `docs(plan): record runtime budget task 1`

  Checkpoint (2026-07-15): implementation commit `ef2b044d3508261994612fca730507809600d6db`.

---

### Task 2: Adopt Bounded Clients At Production Boundaries

**Files:**
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/internal/directorysync/executor.go`
- Modify: `backend/internal/directorysync/executor_test.go`
- Modify: `backend/internal/handler/provider.go`
- Modify: `backend/internal/handler/settings.go`
- Modify: relevant handler tests
- Modify: `backend/internal/quotareset/notification.go`
- Modify: `backend/internal/quotareset/notification_test.go`
- Modify: `backend/internal/repo/service.go`
- Modify: `backend/internal/repo/factory.go`
- Modify: relevant repo tests
- Modify: `backend/internal/scm/github/github.go`
- Modify: `backend/internal/scm/bitbucket/bitbucket.go`
- Modify: relevant SCM tests
- Modify: this plan

**Interfaces:**
- `directorysync.NewExecutor` continues consuming `ExecutorOptions.HTTPClient`.
- `handler.NewProviderHandler(..., clients ...*http.Client)` and `handler.NewSettingsHandler(..., clients ...*http.Client)` preserve existing calls while using the optional bounded client.
- `quotareset.NewWebhookNotifier(..., clients ...*http.Client)` preserves its 5s overall limit.
- `repo.ServiceOptions.HTTPClient *http.Client` supplies SCM constructors.
- GitHub and Bitbucket `New(..., clients ...*http.Client)` preserve existing callers and use the supplied client in production.

- [ ] **Step 1: Add failing constructor/wiring tests**

  For each boundary, inject a client whose transport records calls and whose timeout is a sentinel value. Assert the constructor uses that exact client for one synthetic operation. Add a `cmd/server` wiring test or a pure helper test that proves:

  - Relay, directory, provider-created Relay, settings, and SCM use a shared 30s bounded client configuration.
  - version check uses a separate bounded client with a 10s overall timeout.
  - quota webhooks use a separate bounded client with a 5s overall timeout.

  No test may call a real service.

- [ ] **Step 2: Run Task 2 tests and record RED**

  Run:

  ```bash
  cd backend && go test ./internal/directorysync ./internal/handler ./internal/quotareset ./internal/repo ./internal/scm/github ./internal/scm/bitbucket ./cmd/server -run 'HTTPClient|Bounded|Injected|Timeout' -count=1
  ```

  Expected: FAIL because several constructors still create or use default clients.

- [ ] **Step 3: Extend constructors without changing provider contracts**

  Store an injected client once per long-lived handler/service/provider. When an optional client is absent, retain a non-nil compatibility fallback for tests, but production `main.go` must pass bounded clients explicitly.

  GitHub must construct `go-github` with `gh.NewClient(injectedClient)`; Bitbucket must store the injected client. `repo.ServiceOptions.HTTPClient` must flow through `newGitHubProvider` and `newBitbucketProvider`.

- [ ] **Step 4: Wire one reusable pool per boundary**

  Convert `cfg.HTTPClient` into `httpclient.Options` in a pure helper. Create reusable clients once during startup, inject them into Runtime Relay, ProviderHandler, SettingsHandler, Directory Executor, Repo/SCM, version check, and quota notifier. Do not create a fresh transport per request.

- [ ] **Step 5: Verify focused and broad boundary suites**

  Run separately:

  - the focused command from Step 2;
  - `cd backend && go test ./internal/relay ./internal/directorysync ./internal/handler ./internal/quotareset ./internal/repo ./internal/scm/... ./internal/versioncheck ./cmd/server -count=1`;
  - `git diff --check`.

  Expected: PASS, with no unbounded production `http.DefaultClient` or `&http.Client{}` left at these boundaries.

- [ ] **Step 6: Commit Task 2 and record the checkpoint**

  Commit implementation plus checked Steps 1-5:

  `fix(runtime): bound downstream HTTP clients`

  After the commit succeeds, check Step 6 and commit:

  `docs(plan): record bounded clients task 2`

---

### Task 3: Deadline-Bounded Readiness Semantics

**Files:**
- Modify: `backend/internal/health/health.go`
- Modify: `backend/internal/health/health_test.go`
- Modify: `backend/internal/handler/health.go`
- Modify: `backend/internal/handler/health_http_test.go`
- Modify: `backend/cmd/server/main.go`
- Modify: this plan

**Interfaces:**
- Produces `health.WithReadyTimeout(time.Duration) Option`; `NewService` keeps existing positional pingers and accepts optional options.
- `Service.Ready(ctx)` preserves `ReadyReport` JSON and deterministic check order: database, Redis, Relay.
- `HealthHandler.Ready` maps only `not_ready` to HTTP 503; `ready` and `degraded` remain HTTP 200.

- [ ] **Step 1: Add failing deadline and HTTP semantic tests**

  Add a context-aware blocking DB pinger and assert a 40ms configured readiness budget returns a `not_ready` report well before one second. Add parallel probes whose individual delay would exceed the shared budget if run serially.

  Handler cases:

  ```go
  database down       => 503, status not_ready
  redis down          => 200, status degraded
  relay down          => 200, status degraded
  all up              => 200, status ready
  live with blockers  => 200 without invoking any pinger
  ```

- [ ] **Step 2: Run readiness tests and record RED**

  Run: `cd backend && go test ./internal/health ./internal/handler -run 'Ready|Readiness|Live' -count=1`

  Expected: FAIL because readiness is serial, unbounded, and always returns HTTP 200.

- [ ] **Step 3: Implement one parallel readiness budget**

  Derive one child context with the configured overall timeout, launch the three checks concurrently, and collect into fixed indexes. A pinger that returns after the deadline remains `down/unavailable`; do not expose its raw error. All production pingers already consume the supplied context.

  Preserve precedence: database down wins `not_ready`; only when database is up may Redis/Relay yield `degraded`.

- [ ] **Step 4: Wire and verify readiness**

  Pass `cfg.Server.ReadinessTimeoutSeconds` through `health.WithReadyTimeout`. Run separately:

  - `cd backend && go test ./internal/health ./internal/handler -run 'Ready|Readiness|Live' -count=1`
  - `cd backend && go test ./internal/health ./internal/handler ./cmd/server -count=1`
  - `git diff --check`.

  Expected: PASS. Tests must use context-aware blockers so no goroutine leaks after completion.

- [ ] **Step 5: Commit Task 3 and record the checkpoint**

  Commit implementation plus checked Steps 1-4:

  `fix(runtime): bound readiness and return 503`

  After the commit succeeds, check Step 5 and commit:

  `docs(plan): record readiness task 3`

---

### Task 4: Request Correlation And Low-Cardinality Telemetry

**Files:**
- Create: `backend/internal/telemetry/request.go`
- Create: `backend/internal/telemetry/request_test.go`
- Create: `backend/internal/telemetry/dependency.go`
- Create: `backend/internal/telemetry/dependency_test.go`
- Create: `backend/internal/middleware/request.go`
- Create: `backend/internal/middleware/request_test.go`
- Modify: `backend/internal/middleware/cors.go`
- Modify: `backend/internal/middleware/cors_test.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/handler/router_frontend_test.go`
- Modify: `backend/cmd/server/main.go`
- Modify: this plan

**Interfaces:**
- Produces `telemetry.RequestID(ctx context.Context) string`, `telemetry.WithRequestID(ctx, id)`, and `telemetry.HeaderRequestID = "X-Request-ID"`.
- Produces `middleware.RequestTelemetry(logger *zap.Logger, release string) gin.HandlerFunc`; it owns selection/response of the request ID and one terminal `http_request` event.
- Produces `telemetry.WrapDependency(logger *zap.Logger, release, dependency, operation string) httpclient.TransportWrapper`.
- Production Relay uses fixed labels `dependency="relay"` and `operation="http_request"`.

- [ ] **Step 1: Add failing request-ID and CORS tests**

  Table-test missing, valid, 129-character, whitespace, slash, control-character, and Unicode incoming IDs. Valid IDs are preserved; all invalid IDs are replaced by a valid UUID. Assert the selected value is in the request context during the handler and in `X-Request-ID` on successful, error, OPTIONS, 404, and embedded/static responses.

  Assert CORS includes `X-Request-ID` in both `Access-Control-Allow-Headers` and `Access-Control-Expose-Headers`.

- [ ] **Step 2: Add failing low-cardinality request-log tests and record RED**

  Use a zap observer and two requests such as `/users/7?email=alice@example.com` and `/users/99?email=bob@example.org` against one Gin route `/users/:id`. Both events must contain:

  ```text
  event=http_request
  route=/users/:id
  method=GET
  status_class=2xx
  duration_ms=<non-negative>
  response_bytes=<exact Gin writer size>
  release=test-release
  request_id=<selected ID>
  ```

  Neither raw path, query, email, body, nor route parameter may occur in the event message or structured fields. Unmatched paths always log `route=unmatched`.

  Run: `cd backend && go test ./internal/telemetry ./internal/middleware -run 'Request|CORS|Cardinality|Privacy' -count=1`

  Expected: FAIL because the packages/middleware do not exist.

- [ ] **Step 3: Implement request correlation before every other middleware**

  Validate/generate the ID, replace `c.Request` with a clone carrying the context value, set the response header before `c.Next()`, then log after completion using `c.FullPath()` or exactly `unmatched`. Use `c.Writer.Size()`; do not wrap the writer and risk losing `Flusher` or `Hijacker`.

  The production router must order middleware as request telemetry, recovery, CORS, canonical redirect, embedded frontend, then route/group handlers.

- [ ] **Step 4: Add failing Relay dependency transport tests**

  Wrap a synthetic transport and assert:

  - the incoming request ID is forwarded as `X-Request-ID`;
  - one `dependency_request` event records fixed dependency/operation, method, status class, duration, release, and request ID;
  - timeout/error records an error class without raw URL, query, response text, or credentials;
  - requests without a correlation context do not fabricate a metrics-style label.

  Run: `cd backend && go test ./internal/telemetry ./internal/relay -run 'Dependency|RequestID|Telemetry' -count=1`

  Expected: FAIL because the transport wrapper is not wired.

- [ ] **Step 5: Implement Relay telemetry and full router coverage**

  Wrap only the production Relay client's private bounded transport, so all existing direct `client.Do` calls are covered without changing `relay.Provider`. Do not classify from URL path; `relay/http_request` is the only operation label in this slice.

  Run separately:

  - `cd backend && go test ./internal/telemetry ./internal/middleware ./internal/relay -count=1`
  - `cd backend && go test ./internal/handler ./cmd/server -run 'Router|Request|CORS|Frontend|Runtime' -count=1`
  - `git diff --check`.

  Expected: PASS, with exactly one request event per handled request and one dependency event per Relay round trip.

- [ ] **Step 6: Commit Task 4 and record the checkpoint**

  Commit implementation plus checked Steps 1-5:

  `feat(runtime): correlate request and Relay timing`

  After the commit succeeds, check Step 6 and commit:

  `docs(plan): record request telemetry task 4`

---

### Task 5: Architecture, Review, Verification, And Delivery

**Files:**
- Modify: `docs/architecture.md`
- Modify: this plan

**Interfaces:**
- Consumes Tasks 1-4 and their test/review evidence.
- Produces current runtime documentation, full-repository verification, independent reviews, a pushed branch, and a draft PR targeting `docs/performance-contracts-116`.

- [ ] **Step 1: Update current architecture documentation**

  Document explicit server header/idle budgets, bounded downstream pools/deadlines, two-second parallel readiness with 503 only for database failure, request-ID propagation, normalized request logs, and fixed Relay dependency timing. State that #135 metrics/Web Vitals and final #136 production budget ratification remain future work.

- [ ] **Step 2: Run full verification**

  Run separately:

  - `cd backend && go test ./...`
  - `cd ae-cli && go test ./...`
  - `cd frontend && npm test`
  - `cd frontend && npm run build`
  - `bash deploy/test/release-frontend-embed-test.sh`
  - start Vite and run `cd frontend && npm run test:e2e:role`
  - `git diff --check`

  Report the slow-header listener test and role E2E as environment-sensitive checks. Do not mark either complete unless actually run.

- [ ] **Step 3: Perform independent task and whole-branch reviews**

  Every Task 1-4 receives an independent spec/quality review against its recorded base and head. Resolve every Critical or Important finding and rerun covering tests. Then generate a branch package from `5f6c58e` and obtain final SPEC and standards reviews for all #118 acceptance criteria, privacy constraints, timeout ordering, and repository standards.

- [ ] **Step 4: Commit architecture and verification evidence**

  Commit checked Steps 1-3:

  `docs(architecture): document bounded HTTP runtime`

  After the commit succeeds, check Step 4 and commit:

  `docs(plan): record runtime delivery verification`

- [ ] **Step 5: Push and open the stacked draft PR**

  Push `perf/runtime-118` and create a draft PR targeting `docs/performance-contracts-116`. Link #118 and PR #138, list exact defaults, readiness semantics, telemetry privacy/cardinality, tests, and review results. Confirm head/base/draft/merge state plus backend/frontend/ae-cli/deploy-static checks.

  Only after the first CI run passes, check Step 5, set the top status to complete, commit `docs(plan): record bounded runtime delivery`, push it, and confirm the replacement CI run also passes.
