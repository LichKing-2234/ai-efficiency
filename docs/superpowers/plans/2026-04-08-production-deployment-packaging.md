# Production Deployment Packaging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the approved production deployment packaging spec into a working Docker Compose-first deployment flow with `.env`-driven config, preflight readiness checks, public health/readiness endpoints, admin-visible online update controls with rollback, plus local `dev` / `local` compose variants and a one-time SQLite-to-Postgres migration path for local testing.

**Architecture:** Keep `ai-efficiency` as the single business entrypoint, but add a small deployment/update control plane. The backend exposes read-only deployment metadata plus admin update APIs; a separate updater binary runs as an internal sidecar for privileged `docker compose` apply/rollback operations. Official deployment is driven by `.env` + Compose files + `deploy/docker-deploy.sh`, while the backend reports readiness for Postgres, Redis, and relay/sub2api. In addition, `deploy/docker-compose.dev.yml` and `deploy/docker-compose.local.yml` provide non-production local validation paths without the updater sidecar, and `deploy/migrate-sqlite-to-postgres.sh` seeds local Postgres once from `backend/ai_efficiency.db`.

**Tech Stack:** Go (`gin`, `ent`, `viper`, `zap`, `go-redis/v9`), Vue 3 + Pinia + Vitest, Docker Compose, POSIX shell.

---

## File Map

### New files

- `backend/internal/deployment/version.go`
  Build/version metadata and normalized runtime version payloads.
- `backend/internal/deployment/version_test.go`
  Unit tests for injected version/build metadata behavior.
- `backend/internal/deployment/health.go`
  Liveness/readiness evaluator for DB, Redis, and relay/sub2api.
- `backend/internal/deployment/health_test.go`
  Unit tests for ready/degraded/not-ready classification.
- `backend/internal/deployment/release_source.go`
  Release lookup client for the official version source.
- `backend/internal/deployment/release_source_test.go`
  Unit tests for release parsing and error handling.
- `backend/internal/deployment/service.go`
  Backend-facing deployment service combining version info, readiness, release lookup, and updater client calls.
- `backend/internal/deployment/service_test.go`
  Unit tests for deployment status and update orchestration.
- `backend/internal/deployment/updater_client.go`
  Backend HTTP client for the internal updater sidecar.
- `backend/internal/deployment/updater_client_test.go`
  Tests for updater sidecar request/response handling.
- `backend/internal/deployment/updater_server.go`
  Internal updater HTTP handlers plus file-backed rollback state.
- `backend/internal/deployment/updater_server_test.go`
  Tests for apply/rollback state transitions without shelling to real Docker.
- `backend/internal/handler/deployment.go`
  Public health endpoints plus admin deployment/update endpoints.
- `backend/internal/handler/deployment_http_test.go`
  HTTP tests for the new deployment routes.
- `backend/cmd/updater/main.go`
  Updater sidecar entrypoint.
- `frontend/src/api/deployment.ts`
  Frontend API wrapper for deployment status, update check, apply, and rollback.
- `deploy/.env.example`
  Official operator-facing environment template.
- `deploy/docker-compose.external.yml`
  Compose file for external Postgres/Redis mode.
- `deploy/docker-compose.dev.yml`
  Local source-build compose path for development/testing without the updater sidecar.
- `deploy/docker-compose.local.yml`
  Local directory-backed compose path for persistent local testing without the updater sidecar.
- `deploy/docker-deploy.sh`
  Official deploy/preflight helper script.
- `deploy/migrate-sqlite-to-postgres.sh`
  One-shot helper that migrates `backend/ai_efficiency.db` into the local Postgres test environment.
- `deploy/README.md`
  Operator deployment and upgrade guide.

### Modified files

- `backend/go.mod`
  Add Redis dependency if not already present.
- `backend/internal/config/config.go`
  Add Redis + deployment/update config sections and defaults.
- `backend/internal/config/config_test.go`
  Cover new config sections and `AE_` env mapping.
- `backend/cmd/server/main.go`
  Wire Redis client, deployment service, deployment handler, and updater client.
- `backend/internal/handler/router.go`
  Register public health routes and admin deployment/update routes.
- `deploy/Dockerfile`
  Build both `server` and `updater` binaries and inject version metadata.
- `deploy/docker-compose.yml`
  Convert to `.env`-driven official bundled deployment, add updater sidecar, and add healthchecks/volumes.
- `deploy/config.example.yaml`
  Reflect Redis and deployment/update config sections for advanced operators.
- `frontend/src/types/index.ts`
  Add deployment/update response types shared by the UI.
- `frontend/src/views/SettingsView.vue`
  Add deployment status, update check, apply, and rollback controls.
- `frontend/src/__tests__/api-modules.test.ts`
  Cover the new deployment API module.
- `frontend/src/__tests__/settings-view.test.ts`
  Cover the new deployment UI card and update interactions.
- `docs/architecture.md`
  Update the project-level runtime/deployment description after implementation.

### Existing files to read before implementation

- `backend/internal/config/config.go`
- `backend/internal/handler/settings.go`
- `backend/internal/handler/router.go`
- `backend/cmd/server/main.go`
- `deploy/Dockerfile`
- `deploy/docker-compose.yml`
- `frontend/src/views/SettingsView.vue`
- `frontend/src/__tests__/settings-view.test.ts`
- `docs/superpowers/specs/2026-04-08-production-deployment-packaging-design.md`

### Architectural decisions locked in by this plan

1. Official production deployment remains Docker Compose-first.
2. Online update is required, but the privileged Docker operations run in a dedicated updater sidecar, not in the business server process.
3. Backend health reporting distinguishes `live`, `ready`, `degraded`, and `not_ready`.
4. Public health routes are unauthenticated; deployment/update routes remain admin-only.
5. The updater stores rollback state in a mounted writable state directory so rollback survives backend container replacement.
6. `deploy/docker-compose.yml` and `deploy/docker-compose.external.yml` remain production-focused and keep the updater sidecar.
7. `deploy/docker-compose.dev.yml` and `deploy/docker-compose.local.yml` are non-production local validation paths and do not run the updater sidecar.
8. Local SQLite reuse is handled through a one-time migration into Postgres, not by running SQLite inside the Docker test path.
9. SQLite is removed from the runtime path, and backend tests now run on the Postgres-backed test helper.

---

### Task 1: Add Deployment Config, Redis Config, And Build Metadata

**Files:**
- Create: `backend/internal/deployment/version.go`
- Create: `backend/internal/deployment/version_test.go`
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `deploy/Dockerfile`
- Modify: `deploy/config.example.yaml`
- Modify: `backend/go.mod`
- Test: `backend/internal/config/config_test.go`
- Test: `backend/internal/deployment/version_test.go`

- [ ] **Step 1: Write the failing config and version tests**

Append to `backend/internal/config/config_test.go`:

```go
func TestLoadDeploymentAndRedisConfigFromEnv(t *testing.T) {
	t.Setenv("AE_REDIS_ADDR", "redis:6379")
	t.Setenv("AE_REDIS_PASSWORD", "redis-pass")
	t.Setenv("AE_REDIS_DB", "2")
	t.Setenv("AE_DEPLOYMENT_MODE", "bundled")
	t.Setenv("AE_DEPLOYMENT_STATE_DIR", "/var/lib/ai-efficiency")
	t.Setenv("AE_DEPLOYMENT_UPDATE_ENABLED", "true")
	t.Setenv("AE_DEPLOYMENT_UPDATE_APPLY_ENABLED", "true")
	t.Setenv("AE_DEPLOYMENT_UPDATE_RELEASE_API_URL", "https://api.github.com/repos/ai-efficiency/releases/latest")
	t.Setenv("AE_DEPLOYMENT_UPDATE_UPDATER_URL", "http://updater:8090")

	cfg, err := Load("/nonexistent/config.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Redis.Addr != "redis:6379" {
		t.Fatalf("redis addr = %q, want redis:6379", cfg.Redis.Addr)
	}
	if cfg.Redis.DB != 2 {
		t.Fatalf("redis db = %d, want 2", cfg.Redis.DB)
	}
	if cfg.Deployment.Mode != "bundled" {
		t.Fatalf("deployment mode = %q, want bundled", cfg.Deployment.Mode)
	}
	if !cfg.Deployment.Update.Enabled || !cfg.Deployment.Update.ApplyEnabled {
		t.Fatalf("deployment update flags = %+v, want both true", cfg.Deployment.Update)
	}
	if cfg.Deployment.Update.UpdaterURL != "http://updater:8090" {
		t.Fatalf("updater url = %q", cfg.Deployment.Update.UpdaterURL)
	}
}
```

Create `backend/internal/deployment/version_test.go`:

```go
package deployment

import "testing"

func TestCurrentVersionUsesInjectedBuildInfo(t *testing.T) {
	BuildVersion = "v0.4.0"
	BuildCommit = "abc1234"
	BuildTime = "2026-04-08T12:00:00Z"

	info := CurrentVersion()
	if info.Version != "v0.4.0" {
		t.Fatalf("version = %q, want v0.4.0", info.Version)
	}
	if info.Commit != "abc1234" {
		t.Fatalf("commit = %q, want abc1234", info.Commit)
	}
	if info.BuildTime != "2026-04-08T12:00:00Z" {
		t.Fatalf("build time = %q, want injected value", info.BuildTime)
	}
}
```

- [ ] **Step 2: Run the targeted backend tests to confirm they fail**

Run:

```bash
cd backend
go test ./internal/config ./internal/deployment -run 'TestLoadDeploymentAndRedisConfigFromEnv|TestCurrentVersionUsesInjectedBuildInfo' -count=1
```

Expected: FAIL because `Config` has no `Redis`/`Deployment` fields yet and `deployment.CurrentVersion()` does not exist.

- [ ] **Step 3: Implement Redis/deployment config and version metadata**

In `backend/internal/config/config.go`, add:

```go
type Config struct {
	Server     ServerConfig     `mapstructure:"server"`
	DB         DBConfig         `mapstructure:"db"`
	Redis      RedisConfig      `mapstructure:"redis"`
	Auth       AuthConfig       `mapstructure:"auth"`
	Encryption EncryptionConfig `mapstructure:"encryption"`
	Analysis   AnalysisConfig   `mapstructure:"analysis"`
	Relay      RelayConfig      `mapstructure:"relay"`
	Deployment DeploymentConfig `mapstructure:"deployment"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type DeploymentConfig struct {
	Mode     string       `mapstructure:"mode"`
	StateDir string       `mapstructure:"state_dir"`
	Update   UpdateConfig `mapstructure:"update"`
}

type UpdateConfig struct {
	Enabled         bool   `mapstructure:"enabled"`
	ApplyEnabled    bool   `mapstructure:"apply_enabled"`
	ReleaseAPIURL   string `mapstructure:"release_api_url"`
	UpdaterURL      string `mapstructure:"updater_url"`
	ImageRepository string `mapstructure:"image_repository"`
	Channel         string `mapstructure:"channel"`
}
```

Also add defaults:

```go
v.SetDefault("redis.addr", "redis:6379")
v.SetDefault("redis.db", 0)
v.SetDefault("deployment.mode", "bundled")
v.SetDefault("deployment.state_dir", "/var/lib/ai-efficiency")
v.SetDefault("deployment.update.enabled", true)
v.SetDefault("deployment.update.apply_enabled", true)
v.SetDefault("deployment.update.channel", "stable")
v.SetDefault("deployment.update.release_api_url", "https://api.github.com/repos/ai-efficiency/ai-efficiency/releases/latest")
v.SetDefault("deployment.update.updater_url", "http://updater:8090")
```

Create `backend/internal/deployment/version.go`:

```go
package deployment

import "strings"

var (
	BuildVersion = "dev"
	BuildCommit  = "unknown"
	BuildTime    = ""
)

type VersionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
}

func CurrentVersion() VersionInfo {
	version := strings.TrimSpace(BuildVersion)
	if version == "" {
		version = "dev"
	}
	commit := strings.TrimSpace(BuildCommit)
	if commit == "" {
		commit = "unknown"
	}
	return VersionInfo{
		Version:   version,
		Commit:    commit,
		BuildTime: strings.TrimSpace(BuildTime),
	}
}
```

Update `deploy/config.example.yaml` with:

```yaml
redis:
  addr: "localhost:6379"
  password: ""
  db: 0

deployment:
  mode: "bundled"
  state_dir: "/var/lib/ai-efficiency"
  update:
    enabled: true
    apply_enabled: true
    channel: "stable"
    release_api_url: "https://api.github.com/repos/ai-efficiency/ai-efficiency/releases/latest"
    updater_url: "http://updater:8090"
    image_repository: "ghcr.io/ai-efficiency/ai-efficiency"
```

Update `deploy/Dockerfile` build stage:

```dockerfile
ARG APP_VERSION=dev
ARG APP_COMMIT=unknown
ARG APP_BUILD_TIME=
RUN CGO_ENABLED=0 GOOS=linux go build \
  -ldflags="-X github.com/ai-efficiency/backend/internal/deployment.BuildVersion=${APP_VERSION} -X github.com/ai-efficiency/backend/internal/deployment.BuildCommit=${APP_COMMIT} -X github.com/ai-efficiency/backend/internal/deployment.BuildTime=${APP_BUILD_TIME}" \
  -o /app/server ./cmd/server/
```

- [ ] **Step 4: Add the Redis dependency and re-run the targeted backend tests**

Run:

```bash
cd backend
go get github.com/redis/go-redis/v9
go test ./internal/config ./internal/deployment -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/go.mod backend/go.sum backend/internal/config/config.go backend/internal/config/config_test.go backend/internal/deployment/version.go backend/internal/deployment/version_test.go deploy/Dockerfile deploy/config.example.yaml
git commit -m "feat(backend): add deployment config and version metadata"
```

### Task 2: Add Public Health/Readiness And Admin Deployment Status Endpoints

**Files:**
- Create: `backend/internal/deployment/health.go`
- Create: `backend/internal/deployment/health_test.go`
- Create: `backend/internal/handler/deployment.go`
- Create: `backend/internal/handler/deployment_http_test.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/internal/handler/router.go`
- Test: `backend/internal/deployment/health_test.go`
- Test: `backend/internal/handler/deployment_http_test.go`

- [ ] **Step 1: Write the failing health and deployment HTTP tests**

Create `backend/internal/deployment/health_test.go`:

```go
package deployment

import (
	"context"
	"errors"
	"testing"
)

type pingStub struct {
	err error
}

func (p pingStub) Ping(context.Context) error { return p.err }

func TestHealthServiceReadyAndDegradedStates(t *testing.T) {
	svc := NewHealthService(
		pingStub{},
		pingStub{},
		pingStub{err: errors.New("relay down")},
		CurrentVersion(),
	)

	report := svc.Ready(context.Background())
	if report.Status != "degraded" {
		t.Fatalf("status = %q, want degraded", report.Status)
	}
	if len(report.Checks) != 3 {
		t.Fatalf("checks = %d, want 3", len(report.Checks))
	}
}
```

Create `backend/internal/handler/deployment_http_test.go`:

```go
package handler

import (
	"net/http"
	"testing"
)

func TestHealthReadyRouteReturnsDeploymentPayload(t *testing.T) {
	env := setupFullTestEnv(t)

	w := doFullRequest(env, http.MethodGet, "/api/v1/health/ready", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	resp := parseFullResponse(t, w)
	data := resp["data"].(map[string]any)
	if _, ok := data["status"].(string); !ok {
		t.Fatalf("missing status in ready payload: %+v", data)
	}
	if _, ok := data["version"].(map[string]any); !ok {
		t.Fatalf("missing version payload: %+v", data)
	}
}

func TestDeploymentStatusRequiresAdmin(t *testing.T) {
	env := setupFullTestEnv(t)
	token := createFullNonAdminToken(t, env)

	w := doFullRequestWithToken(env, http.MethodGet, "/api/v1/settings/deployment", nil, token)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run the targeted backend tests to confirm they fail**

Run:

```bash
cd backend
go test ./internal/deployment ./internal/handler -run 'TestHealthServiceReadyAndDegradedStates|TestHealthReadyRouteReturnsDeploymentPayload|TestDeploymentStatusRequiresAdmin' -count=1
```

Expected: FAIL because there is no health service and the new routes do not exist.

- [ ] **Step 3: Implement the health service and deployment status handler**

Create `backend/internal/deployment/health.go`:

```go
package deployment

import "context"

type Pinger interface {
	Ping(context.Context) error
}

type FuncPinger func(context.Context) error

func (f FuncPinger) Ping(ctx context.Context) error { return f(ctx) }

type CheckResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type ReadyReport struct {
	Status  string       `json:"status"`
	Version VersionInfo  `json:"version"`
	Checks  []CheckResult `json:"checks"`
}

type HealthService struct {
	db      Pinger
	redis   Pinger
	relay   Pinger
	version VersionInfo
}

func NewHealthService(db, redis, relay Pinger, version VersionInfo) *HealthService {
	return &HealthService{db: db, redis: redis, relay: relay, version: version}
}

func (s *HealthService) Live() map[string]any {
	return map[string]any{
		"status":  "live",
		"version": s.version,
	}
}

func (s *HealthService) Ready(ctx context.Context) ReadyReport {
	checks := []CheckResult{
		runCheck(ctx, "database", s.db),
		runCheck(ctx, "redis", s.redis),
		runCheck(ctx, "relay", s.relay),
	}
	status := "ready"
	for _, check := range checks {
		if check.Status == "down" && check.Name == "database" {
			status = "not_ready"
			break
		}
		if check.Status == "down" {
			status = "degraded"
		}
	}
	return ReadyReport{Status: status, Version: s.version, Checks: checks}
}

func runCheck(ctx context.Context, name string, p Pinger) CheckResult {
	if err := p.Ping(ctx); err != nil {
		return CheckResult{Name: name, Status: "down", Message: err.Error()}
	}
	return CheckResult{Name: name, Status: "up"}
}
```

Create `backend/internal/handler/deployment.go`:

```go
package handler

import (
	"context"
	"net/http"

	"github.com/ai-efficiency/backend/internal/deployment"
	"github.com/gin-gonic/gin"
)

type deploymentStatusReader interface {
	Status(context.Context) (deployment.DeploymentStatus, error)
	CheckForUpdate(context.Context) (deployment.DeploymentStatus, error)
	ApplyUpdate(context.Context, deployment.ApplyRequest) (deployment.UpdateStatus, error)
	RollbackUpdate(context.Context) (deployment.UpdateStatus, error)
}

type DeploymentHandler struct {
	health *deployment.HealthService
	status deploymentStatusReader
}

func NewDeploymentHandler(health *deployment.HealthService, status deploymentStatusReader) *DeploymentHandler {
	return &DeploymentHandler{health: health, status: status}
}

func (h *DeploymentHandler) Live(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": h.health.Live()})
}

func (h *DeploymentHandler) Ready(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": h.health.Ready(c.Request.Context())})
}

func (h *DeploymentHandler) Status(c *gin.Context) {
	payload, err := h.status.Status(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": payload})
}
```

Wire in `backend/internal/handler/router.go`:

```go
api.GET("/health/live", deploymentHandler.Live)
api.GET("/health/ready", deploymentHandler.Ready)

settingsGroup.GET("/deployment", deploymentHandler.Status)
```

And in `backend/cmd/server/main.go`, initialize Redis + health service:

```go
var sqlDB *sql.DB

// inside both sqlite/postgres branches, assign:
sqlDB = db

redisClient := redis.NewClient(&redis.Options{
	Addr:     cfg.Redis.Addr,
	Password: cfg.Redis.Password,
	DB:       cfg.Redis.DB,
})
healthSvc := deployment.NewHealthService(
	deployment.FuncPinger(func(ctx context.Context) error { return sqlDB.PingContext(ctx) }),
	deployment.FuncPinger(func(ctx context.Context) error { return redisClient.Ping(ctx).Err() }),
	deployment.FuncPinger(func(ctx context.Context) error {
		if relayProvider == nil {
			return nil
		}
		return relayProvider.Ping(ctx)
	}),
	deployment.CurrentVersion(),
)
releaseSource := deployment.NewGitHubReleaseSource(http.DefaultClient, cfg.Deployment.Update.ReleaseAPIURL)
updaterClient := deployment.NewUpdaterClient(http.DefaultClient, cfg.Deployment.Update.UpdaterURL)
deploymentSvc := deployment.NewService(cfg.Deployment, deployment.CurrentVersion(), releaseSource, updaterClient)
deploymentHandler := handler.NewDeploymentHandler(healthSvc, deploymentSvc)
```

- [ ] **Step 4: Run the targeted backend tests**

Run:

```bash
cd backend
go test ./internal/deployment ./internal/handler -run 'TestHealthServiceReadyAndDegradedStates|TestHealthReadyRouteReturnsDeploymentPayload|TestDeploymentStatusRequiresAdmin' -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/deployment/health.go backend/internal/deployment/health_test.go backend/internal/handler/deployment.go backend/internal/handler/deployment_http_test.go backend/internal/handler/router.go backend/cmd/server/main.go
git commit -m "feat(backend): add deployment health and status endpoints"
```

### Task 3: Add Release Check And Admin Update APIs In The Backend

**Files:**
- Create: `backend/internal/deployment/release_source.go`
- Create: `backend/internal/deployment/release_source_test.go`
- Create: `backend/internal/deployment/service.go`
- Create: `backend/internal/deployment/service_test.go`
- Create: `backend/internal/deployment/updater_client.go`
- Create: `backend/internal/deployment/updater_client_test.go`
- Modify: `backend/internal/handler/deployment.go`
- Modify: `backend/internal/handler/deployment_http_test.go`
- Modify: `backend/cmd/server/main.go`
- Test: `backend/internal/deployment/release_source_test.go`
- Test: `backend/internal/deployment/service_test.go`
- Test: `backend/internal/deployment/updater_client_test.go`
- Test: `backend/internal/handler/deployment_http_test.go`

- [ ] **Step 1: Write the failing release-check and admin update tests**

Create `backend/internal/deployment/release_source_test.go`:

```go
package deployment

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubReleaseSourceLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.5.0","html_url":"https://example.com/release/v0.5.0","published_at":"2026-04-08T12:00:00Z"}`))
	}))
	defer srv.Close()

	src := NewGitHubReleaseSource(srv.Client(), srv.URL)
	info, err := src.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if info.Version != "v0.5.0" {
		t.Fatalf("version = %q, want v0.5.0", info.Version)
	}
}
```

Create `backend/internal/deployment/service_test.go`:

```go
package deployment

import (
	"context"
	"testing"

	cfgpkg "github.com/ai-efficiency/backend/internal/config"
)

type updaterStub struct {
	appliedVersion string
}

func (u *updaterStub) Status(context.Context) (UpdateStatus, error) {
	return UpdateStatus{Phase: "idle"}, nil
}
func (u *updaterStub) Apply(_ context.Context, req ApplyRequest) (UpdateStatus, error) {
	u.appliedVersion = req.TargetVersion
	return UpdateStatus{Phase: "updating", TargetVersion: req.TargetVersion}, nil
}
func (u *updaterStub) Rollback(context.Context) (UpdateStatus, error) {
	return UpdateStatus{Phase: "rolling_back"}, nil
}

type releaseStub struct{}

func (releaseStub) Latest(context.Context) (ReleaseInfo, error) {
	return ReleaseInfo{Version: "v0.5.0", URL: "https://example.com/v0.5.0"}, nil
}

func TestDeploymentServiceCheckAndApplyUpdate(t *testing.T) {
	updater := &updaterStub{}
	svc := NewService(cfgpkg.DeploymentConfig{
		Mode: "bundled",
		Update: cfgpkg.UpdateConfig{
			Enabled:      true,
			ApplyEnabled: true,
		},
	}, VersionInfo{Version: "v0.4.0"}, releaseStub{}, updater)

	status, err := svc.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if !status.UpdateAvailable {
		t.Fatalf("expected update available, got %+v", status)
	}
	if _, err := svc.ApplyUpdate(context.Background(), ApplyRequest{TargetVersion: "v0.5.0"}); err != nil {
		t.Fatalf("ApplyUpdate: %v", err)
	}
	if updater.appliedVersion != "v0.5.0" {
		t.Fatalf("applied version = %q, want v0.5.0", updater.appliedVersion)
	}
}
```

Create `backend/internal/deployment/updater_client_test.go`:

```go
package deployment

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpdaterClientApplyAndRollback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apply":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"phase":"updating","target_version":"v0.5.0"}`))
		case "/rollback":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"phase":"rolling_back","target_version":"v0.4.0"}`))
		case "/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"phase":"idle"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewUpdaterClient(srv.Client(), srv.URL)
	status, err := client.Apply(context.Background(), ApplyRequest{TargetVersion: "v0.5.0"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if status.TargetVersion != "v0.5.0" {
		t.Fatalf("target version = %q, want v0.5.0", status.TargetVersion)
	}
	rollback, err := client.Rollback(context.Background())
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if rollback.Phase != "rolling_back" {
		t.Fatalf("phase = %q, want rolling_back", rollback.Phase)
	}
}
```

Extend `backend/internal/handler/deployment_http_test.go`:

```go
func TestDeploymentUpdateRoutesRequireAdmin(t *testing.T) {
	env := setupFullTestEnv(t)
	token := createFullNonAdminToken(t, env)

	for _, path := range []string{
		"/api/v1/settings/deployment/update/check",
		"/api/v1/settings/deployment/update/apply",
		"/api/v1/settings/deployment/update/rollback",
	} {
		w := doFullRequestWithToken(env, http.MethodPost, path, map[string]any{}, token)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s: expected 403, got %d", path, w.Code)
		}
	}
}
```

- [ ] **Step 2: Run the targeted backend tests to confirm they fail**

Run:

```bash
cd backend
go test ./internal/deployment ./internal/handler -run 'TestGitHubReleaseSourceLatest|TestDeploymentServiceCheckAndApplyUpdate|TestDeploymentUpdateRoutesRequireAdmin' -count=1
```

Expected: FAIL because the release source, deployment service, updater client, and update endpoints do not exist.

- [ ] **Step 3: Implement release lookup, deployment service, updater client, and admin update routes**

Create `backend/internal/deployment/release_source.go`:

```go
package deployment

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type ReleaseInfo struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

type ReleaseSource interface {
	Latest(context.Context) (ReleaseInfo, error)
}

type GitHubReleaseSource struct {
	client *http.Client
	url    string
}

func NewGitHubReleaseSource(client *http.Client, url string) *GitHubReleaseSource {
	return &GitHubReleaseSource{client: client, url: url}
}

func (s *GitHubReleaseSource) Latest(ctx context.Context) (ReleaseInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return ReleaseInfo{}, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return ReleaseInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ReleaseInfo{}, fmt.Errorf("release lookup returned %s", resp.Status)
	}
	var payload struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ReleaseInfo{}, err
	}
	return ReleaseInfo{Version: payload.TagName, URL: payload.HTMLURL}, nil
}
```

Create `backend/internal/deployment/updater_client.go`:

```go
package deployment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type ApplyRequest struct {
	TargetVersion string `json:"target_version"`
}

type UpdateStatus struct {
	Phase         string `json:"phase"`
	TargetVersion string `json:"target_version,omitempty"`
	Message       string `json:"message,omitempty"`
}

type Updater interface {
	Status(context.Context) (UpdateStatus, error)
	Apply(context.Context, ApplyRequest) (UpdateStatus, error)
	Rollback(context.Context) (UpdateStatus, error)
}

type UpdaterClient struct {
	baseURL string
	client  *http.Client
}

func NewUpdaterClient(client *http.Client, baseURL string) *UpdaterClient {
	return &UpdaterClient{client: client, baseURL: strings.TrimRight(baseURL, "/")}
}

func (c *UpdaterClient) Status(ctx context.Context) (UpdateStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/status", nil)
	if err != nil {
		return UpdateStatus{}, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return UpdateStatus{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return UpdateStatus{}, fmt.Errorf("updater status returned %s", resp.Status)
	}
	var status UpdateStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return UpdateStatus{}, err
	}
	return status, nil
}

func (c *UpdaterClient) Apply(ctx context.Context, reqBody ApplyRequest) (UpdateStatus, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return UpdateStatus{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/apply", bytes.NewReader(body))
	if err != nil {
		return UpdateStatus{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return UpdateStatus{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return UpdateStatus{}, fmt.Errorf("updater apply returned %s", resp.Status)
	}
	var status UpdateStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return UpdateStatus{}, err
	}
	return status, nil
}

func (c *UpdaterClient) Rollback(ctx context.Context) (UpdateStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/rollback", nil)
	if err != nil {
		return UpdateStatus{}, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return UpdateStatus{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return UpdateStatus{}, fmt.Errorf("updater rollback returned %s", resp.Status)
	}
	var status UpdateStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return UpdateStatus{}, err
	}
	return status, nil
}
```

Create `backend/internal/deployment/service.go`:

```go
package deployment

import (
	"context"

	"github.com/ai-efficiency/backend/internal/config"
)

type Service struct {
	cfg     config.DeploymentConfig
	version VersionInfo
	source  ReleaseSource
	updater Updater
}

type DeploymentStatus struct {
	Version         VersionInfo   `json:"version"`
	UpdateAvailable bool          `json:"update_available"`
	LatestRelease   *ReleaseInfo  `json:"latest_release,omitempty"`
	UpdateStatus    UpdateStatus  `json:"update_status"`
	Mode            string        `json:"mode"`
}

func NewService(cfg config.DeploymentConfig, version VersionInfo, source ReleaseSource, updater Updater) *Service {
	return &Service{cfg: cfg, version: version, source: source, updater: updater}
}

func (s *Service) Status(ctx context.Context) (DeploymentStatus, error) {
	updateStatus := UpdateStatus{Phase: "disabled"}
	if s.updater != nil {
		current, err := s.updater.Status(ctx)
		if err != nil {
			return DeploymentStatus{}, err
		}
		updateStatus = current
	}
	return DeploymentStatus{
		Version:      s.version,
		Mode:         s.cfg.Mode,
		UpdateStatus: updateStatus,
	}, nil
}

func (s *Service) CheckForUpdate(ctx context.Context) (DeploymentStatus, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return DeploymentStatus{}, err
	}
	if s.source == nil {
		return status, nil
	}
	latest, err := s.source.Latest(ctx)
	if err != nil {
		return DeploymentStatus{}, err
	}
	status.LatestRelease = &latest
	status.UpdateAvailable = latest.Version != s.version.Version
	return status, nil
}

func (s *Service) ApplyUpdate(ctx context.Context, req ApplyRequest) (UpdateStatus, error) {
	return s.updater.Apply(ctx, req)
}

func (s *Service) RollbackUpdate(ctx context.Context) (UpdateStatus, error) {
	return s.updater.Rollback(ctx)
}
```

Add update handlers in `backend/internal/handler/deployment.go`:

```go
func (h *DeploymentHandler) CheckForUpdate(c *gin.Context) {
	payload, err := h.status.CheckForUpdate(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": payload})
}

func (h *DeploymentHandler) ApplyUpdate(c *gin.Context) {
	var req deployment.ApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid request"})
		return
	}
	payload, err := h.status.ApplyUpdate(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": payload})
}

func (h *DeploymentHandler) RollbackUpdate(c *gin.Context) {
	payload, err := h.status.RollbackUpdate(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": payload})
}
```

Register routes in `backend/internal/handler/router.go`:

```go
settingsGroup.GET("/deployment", deploymentHandler.Status)
settingsGroup.POST("/deployment/update/check", deploymentHandler.CheckForUpdate)
settingsGroup.POST("/deployment/update/apply", deploymentHandler.ApplyUpdate)
settingsGroup.POST("/deployment/update/rollback", deploymentHandler.RollbackUpdate)
```

- [ ] **Step 4: Run the targeted backend tests**

Run:

```bash
cd backend
go test ./internal/deployment ./internal/handler -run 'TestGitHubReleaseSourceLatest|TestDeploymentServiceCheckAndApplyUpdate|TestDeploymentUpdateRoutesRequireAdmin' -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/deployment/release_source.go backend/internal/deployment/release_source_test.go backend/internal/deployment/service.go backend/internal/deployment/service_test.go backend/internal/deployment/updater_client.go backend/internal/deployment/updater_client_test.go backend/internal/handler/deployment.go backend/internal/handler/deployment_http_test.go backend/internal/handler/router.go backend/cmd/server/main.go
git commit -m "feat(backend): add deployment update control plane"
```

### Task 4: Implement The Updater Sidecar And Official Deploy Assets

**Files:**
- Create: `backend/internal/deployment/updater_server.go`
- Create: `backend/internal/deployment/updater_server_test.go`
- Create: `backend/cmd/updater/main.go`
- Create: `deploy/.env.example`
- Create: `deploy/docker-compose.external.yml`
- Create: `deploy/docker-deploy.sh`
- Modify: `deploy/Dockerfile`
- Modify: `deploy/docker-compose.yml`
- Modify: `deploy/config.example.yaml`
- Test: `backend/internal/deployment/updater_server_test.go`
- Verify: `bash -n deploy/docker-deploy.sh`
- Verify: `docker compose --env-file deploy/.env.example -f deploy/docker-compose.yml config`
- Verify: `docker compose --env-file deploy/.env.example -f deploy/docker-compose.external.yml config`

- [ ] **Step 1: Write the failing updater-sidecar test**

Create `backend/internal/deployment/updater_server_test.go`:

```go
package deployment

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type composeRunnerStub struct {
	calls [][]string
}

func (r *composeRunnerStub) Run(_ context.Context, args ...string) error {
	r.calls = append(r.calls, args)
	return nil
}

func TestUpdaterServerApplyAndRollbackRewriteEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("AE_IMAGE_TAG=v0.4.0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	runner := &composeRunnerStub{}
	server := NewUpdaterServer(UpdaterConfig{
		ComposeFile: "deploy/docker-compose.yml",
		EnvFile:     envFile,
		ServiceName: "backend",
		StateDir:    dir,
	}, runner)

	if _, err := server.Apply(context.Background(), ApplyRequest{TargetVersion: "v0.5.0"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("ReadFile env: %v", err)
	}
	if string(data) != "AE_IMAGE_TAG=v0.5.0\n" {
		t.Fatalf("env file = %q, want updated tag", string(data))
	}
	if _, err := server.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	data, _ = os.ReadFile(envFile)
	if string(data) != "AE_IMAGE_TAG=v0.4.0\n" {
		t.Fatalf("env file after rollback = %q, want old tag", string(data))
	}
}
```

- [ ] **Step 2: Run the targeted updater test to confirm it fails**

Run:

```bash
cd backend
go test ./internal/deployment -run 'TestUpdaterServerApplyAndRollbackRewriteEnvFile$' -count=1
```

Expected: FAIL because the updater server and rollback state logic do not exist.

- [ ] **Step 3: Implement the updater server, updater entrypoint, and official deployment files**

Create `backend/internal/deployment/updater_server.go`:

```go
package deployment

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

type ComposeRunner interface {
	Run(context.Context, ...string) error
}

type UpdaterConfig struct {
	ComposeFile string
	EnvFile     string
	ServiceName string
	StateDir    string
}

type UpdaterServer struct {
	cfg    UpdaterConfig
	runner ComposeRunner
}

func NewUpdaterServer(cfg UpdaterConfig, runner ComposeRunner) *UpdaterServer {
	return &UpdaterServer{cfg: cfg, runner: runner}
}

func (s *UpdaterServer) Apply(ctx context.Context, req ApplyRequest) (UpdateStatus, error) {
	current, err := readEnvVar(s.cfg.EnvFile, "AE_IMAGE_TAG")
	if err != nil {
		return UpdateStatus{}, err
	}
	if err := os.WriteFile(filepath.Join(s.cfg.StateDir, "rollback-image-tag"), []byte(current), 0o600); err != nil {
		return UpdateStatus{}, err
	}
	if err := writeEnvVar(s.cfg.EnvFile, "AE_IMAGE_TAG", req.TargetVersion); err != nil {
		return UpdateStatus{}, err
	}
	if err := s.runner.Run(ctx, "pull", s.cfg.ServiceName); err != nil {
		return UpdateStatus{}, err
	}
	if err := s.runner.Run(ctx, "up", "-d", s.cfg.ServiceName); err != nil {
		return UpdateStatus{}, err
	}
	return UpdateStatus{Phase: "updating", TargetVersion: req.TargetVersion}, nil
}

func (s *UpdaterServer) Rollback(ctx context.Context) (UpdateStatus, error) {
	previous, err := os.ReadFile(filepath.Join(s.cfg.StateDir, "rollback-image-tag"))
	if err != nil {
		return UpdateStatus{}, err
	}
	if err := writeEnvVar(s.cfg.EnvFile, "AE_IMAGE_TAG", strings.TrimSpace(string(previous))); err != nil {
		return UpdateStatus{}, err
	}
	if err := s.runner.Run(ctx, "up", "-d", s.cfg.ServiceName); err != nil {
		return UpdateStatus{}, err
	}
	return UpdateStatus{Phase: "rolling_back", TargetVersion: strings.TrimSpace(string(previous))}, nil
}

func readEnvVar(path, key string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	prefix := key + "="
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix), nil
		}
	}
	return "", os.ErrNotExist
}

func writeEnvVar(path, key, value string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	prefix := key + "="
	replaced := false
	for i, line := range lines {
		if strings.HasPrefix(line, prefix) {
			lines[i] = prefix + value
			replaced = true
		}
	}
	if !replaced {
		lines = append(lines, prefix+value)
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}
```

Create `backend/cmd/updater/main.go`:

```go
package main

import (
	"context"
	"net/http"
	"os"
	"os/exec"

	"github.com/ai-efficiency/backend/internal/deployment"
	"github.com/gin-gonic/gin"
)

type dockerComposeRunner struct {
	composeFile string
	envFile     string
}

func NewDockerComposeRunner(composeFile, envFile string) deployment.ComposeRunner {
	return &dockerComposeRunner{composeFile: composeFile, envFile: envFile}
}

func (r *dockerComposeRunner) Run(ctx context.Context, args ...string) error {
	cmdArgs := append([]string{"compose", "--env-file", r.envFile, "-f", r.composeFile}, args...)
	cmd := exec.CommandContext(ctx, "docker", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func main() {
	cfg := deployment.UpdaterConfig{
		ComposeFile: os.Getenv("AE_UPDATER_COMPOSE_FILE"),
		EnvFile:     os.Getenv("AE_UPDATER_ENV_FILE"),
		ServiceName: os.Getenv("AE_UPDATER_SERVICE_NAME"),
		StateDir:    os.Getenv("AE_DEPLOYMENT_STATE_DIR"),
	}
	server := deployment.NewUpdaterServer(cfg, NewDockerComposeRunner(cfg.ComposeFile, cfg.EnvFile))

	r := gin.New()
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.POST("/apply", func(c *gin.Context) {
		var req deployment.ApplyRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request"})
			return
		}
		status, err := server.Apply(c.Request.Context(), req)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, status)
	})
	r.POST("/rollback", func(c *gin.Context) {
		status, err := server.Rollback(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, status)
	})
	r.GET("/status", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"phase": "idle"}) })

	_ = r.Run(":8090")
}
```

Update `deploy/Dockerfile` to build both binaries:

```dockerfile
RUN CGO_ENABLED=0 GOOS=linux go build \
  -ldflags="-X github.com/ai-efficiency/backend/internal/deployment.BuildVersion=${APP_VERSION} -X github.com/ai-efficiency/backend/internal/deployment.BuildCommit=${APP_COMMIT} -X github.com/ai-efficiency/backend/internal/deployment.BuildTime=${APP_BUILD_TIME}" \
  -o /app/server ./cmd/server/
RUN CGO_ENABLED=0 GOOS=linux go build \
  -ldflags="-X github.com/ai-efficiency/backend/internal/deployment.BuildVersion=${APP_VERSION} -X github.com/ai-efficiency/backend/internal/deployment.BuildCommit=${APP_COMMIT} -X github.com/ai-efficiency/backend/internal/deployment.BuildTime=${APP_BUILD_TIME}" \
  -o /app/updater ./cmd/updater/

COPY --from=backend-builder /app/server .
COPY --from=backend-builder /app/updater .
```

Create `deploy/.env.example`:

```bash
AE_IMAGE_REPOSITORY=ghcr.io/ai-efficiency/ai-efficiency
AE_IMAGE_TAG=v0.1.0
AE_SERVER_PORT=8081
AE_SERVER_MODE=release
AE_SERVER_FRONTEND_URL=http://localhost:8081
AE_DB_DSN=postgres://postgres:postgres@postgres:5432/ai_efficiency?sslmode=disable
AE_REDIS_ADDR=redis:6379
AE_REDIS_PASSWORD=
AE_REDIS_DB=0
AE_RELAY_URL=http://sub2api.example.com
AE_RELAY_API_KEY=
AE_RELAY_ADMIN_API_KEY=
AE_AUTH_JWT_SECRET=
AE_ENCRYPTION_KEY=
AE_DEPLOYMENT_MODE=bundled
AE_DEPLOYMENT_STATE_DIR=/var/lib/ai-efficiency
AE_DEPLOYMENT_UPDATE_ENABLED=true
AE_DEPLOYMENT_UPDATE_APPLY_ENABLED=true
AE_DEPLOYMENT_UPDATE_RELEASE_API_URL=https://api.github.com/repos/ai-efficiency/ai-efficiency/releases/latest
AE_DEPLOYMENT_UPDATE_UPDATER_URL=http://updater:8090
AE_UPDATER_ENV_FILE=/work/deploy/.env
AE_UPDATER_SERVICE_NAME=backend
```

Update `deploy/docker-compose.yml`:

```yaml
services:
  postgres:
    image: postgres:16-alpine
    profiles: ["bundled"]
    env_file: .env
  redis:
    image: redis:7-alpine
    profiles: ["bundled"]
    env_file: .env
  backend:
    image: ${AE_IMAGE_REPOSITORY}:${AE_IMAGE_TAG}
    env_file: .env
    volumes:
      - appstate:/var/lib/ai-efficiency
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
      updater:
        condition: service_healthy
  updater:
    image: ${AE_IMAGE_REPOSITORY}:${AE_IMAGE_TAG}
    command: ["./updater"]
    env_file: .env
    environment:
      AE_UPDATER_COMPOSE_FILE: /work/deploy/docker-compose.yml
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./:/work/deploy
      - appstate:/var/lib/ai-efficiency
volumes:
  appstate:
```

Create `deploy/docker-compose.external.yml`:

```yaml
services:
  backend:
    image: ${AE_IMAGE_REPOSITORY}:${AE_IMAGE_TAG}
    env_file: .env
    ports:
      - "${AE_SERVER_PORT:-8081}:8081"
    volumes:
      - appstate:/var/lib/ai-efficiency
    depends_on:
      updater:
        condition: service_healthy

  updater:
    image: ${AE_IMAGE_REPOSITORY}:${AE_IMAGE_TAG}
    command: ["./updater"]
    env_file: .env
    environment:
      AE_UPDATER_COMPOSE_FILE: /work/deploy/docker-compose.external.yml
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./:/work/deploy
      - appstate:/var/lib/ai-efficiency
volumes:
  appstate:
```

Create `deploy/docker-deploy.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${ROOT_DIR}/.env"
MODE="${1:-check}"
COMPOSE_FILE="${ROOT_DIR}/docker-compose.yml"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing required command: $1" >&2; exit 1; }
}

ensure_env() {
  if [[ ! -f "${ENV_FILE}" ]]; then
    cp "${ROOT_DIR}/.env.example" "${ENV_FILE}"
  fi
}

check_required_var() {
  local name="$1"
  if ! grep -q "^${name}=" "${ENV_FILE}"; then
    echo "missing required env var: ${name}" >&2
    exit 1
  fi
}

check_url() {
  local url="$1"
  curl -fsS --max-time 5 "${url}" >/dev/null || { echo "dependency not ready: ${url}" >&2; exit 1; }
}

check_tcp() {
  local host="$1"
  local port="$2"
  (echo >"/dev/tcp/${host}/${port}") >/dev/null 2>&1 || { echo "dependency not ready: ${host}:${port}" >&2; exit 1; }
}

require_cmd docker
require_cmd curl
ensure_env
check_required_var AE_RELAY_URL
check_required_var AE_AUTH_JWT_SECRET
check_required_var AE_ENCRYPTION_KEY
if [[ "${MODE}" == "external" ]]; then
  COMPOSE_FILE="${ROOT_DIR}/docker-compose.external.yml"
  DB_TARGET="$(grep '^AE_DB_DSN=' "${ENV_FILE}" | sed -E 's|.*@([^:/]+):([0-9]+).*|\1 \2|')"
  REDIS_TARGET="$(grep '^AE_REDIS_ADDR=' "${ENV_FILE}" | cut -d= -f2)"
  check_tcp "$(echo "${DB_TARGET}" | awk '{print $1}')" "$(echo "${DB_TARGET}" | awk '{print $2}')"
  check_tcp "${REDIS_TARGET%:*}" "${REDIS_TARGET#*:}"
fi
docker compose --env-file "${ENV_FILE}" -f "${COMPOSE_FILE}" config >/dev/null
check_url "$(grep '^AE_RELAY_URL=' "${ENV_FILE}" | cut -d= -f2)/health"
echo "preflight ok"
```

- [ ] **Step 4: Run the updater test and deployment asset verification**

Run:

```bash
cd backend
go test ./internal/deployment -run 'TestUpdaterServerApplyAndRollbackRewriteEnvFile$' -count=1
cd ..
bash -n deploy/docker-deploy.sh
docker compose --env-file deploy/.env.example -f deploy/docker-compose.yml config >/dev/null
docker compose --env-file deploy/.env.example -f deploy/docker-compose.external.yml config >/dev/null
```

Expected: all commands succeed.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/deployment/updater_server.go backend/internal/deployment/updater_server_test.go backend/cmd/updater/main.go deploy/.env.example deploy/docker-compose.yml deploy/docker-compose.external.yml deploy/docker-deploy.sh deploy/Dockerfile deploy/config.example.yaml
git commit -m "feat(deploy): add updater sidecar and official compose assets"
```

### Task 5: Add Deployment Status And Update Controls To The Admin UI

**Files:**
- Create: `frontend/src/api/deployment.ts`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/views/SettingsView.vue`
- Modify: `frontend/src/__tests__/api-modules.test.ts`
- Modify: `frontend/src/__tests__/settings-view.test.ts`
- Test: `frontend/src/__tests__/api-modules.test.ts`
- Test: `frontend/src/__tests__/settings-view.test.ts`

- [ ] **Step 1: Write the failing frontend API and view tests**

Append to `frontend/src/__tests__/api-modules.test.ts`:

```ts
import { getDeploymentStatus, checkForUpdate, applyUpdate, rollbackUpdate } from '@/api/deployment'

it('calls deployment endpoints', async () => {
  await getDeploymentStatus()
  expect(client.get).toHaveBeenCalledWith('/settings/deployment')

  await checkForUpdate()
  expect(client.post).toHaveBeenCalledWith('/settings/deployment/update/check')

  await applyUpdate({ target_version: 'v0.5.0' })
  expect(client.post).toHaveBeenCalledWith('/settings/deployment/update/apply', { target_version: 'v0.5.0' })

  await rollbackUpdate()
  expect(client.post).toHaveBeenCalledWith('/settings/deployment/update/rollback')
})
```

Append to `frontend/src/__tests__/settings-view.test.ts`:

```ts
vi.mock('@/api/deployment', () => ({
  getDeploymentStatus: vi.fn().mockResolvedValue({
    data: {
      data: {
        version: { version: 'v0.4.0', commit: 'abc1234', build_time: '2026-04-08T12:00:00Z' },
        mode: 'bundled',
        update_available: true,
        latest_release: { version: 'v0.5.0', url: 'https://example.com/v0.5.0' },
        update_status: { phase: 'idle' },
      },
    },
  }),
  checkForUpdate: vi.fn(),
  applyUpdate: vi.fn(),
  rollbackUpdate: vi.fn(),
}))

it('renders deployment status and update controls', async () => {
  const wrapper = await mountSettings()
  expect(wrapper.text()).toContain('Deployment')
  expect(wrapper.text()).toContain('v0.4.0')
  expect(wrapper.text()).toContain('v0.5.0')
  expect(wrapper.text()).toContain('Check Updates')
  expect(wrapper.text()).toContain('Apply Update')
  expect(wrapper.text()).toContain('Rollback')
})
```

- [ ] **Step 2: Run the targeted frontend tests to confirm they fail**

Run:

```bash
cd frontend
pnpm test -- --run api-modules settings-view
```

Expected: FAIL because `@/api/deployment` does not exist and `SettingsView` has no deployment card.

- [ ] **Step 3: Implement the deployment API module, shared types, and settings UI**

Create `frontend/src/api/deployment.ts`:

```ts
import client from './client'
import type { ApiResponse, DeploymentStatus, ApplyUpdateRequest, UpdateStatus } from '@/types'

export function getDeploymentStatus() {
  return client.get<ApiResponse<DeploymentStatus>>('/settings/deployment')
}

export function checkForUpdate() {
  return client.post<ApiResponse<DeploymentStatus>>('/settings/deployment/update/check')
}

export function applyUpdate(data: ApplyUpdateRequest) {
  return client.post<ApiResponse<UpdateStatus>>('/settings/deployment/update/apply', data)
}

export function rollbackUpdate() {
  return client.post<ApiResponse<UpdateStatus>>('/settings/deployment/update/rollback')
}
```

Extend `frontend/src/types/index.ts`:

```ts
export interface VersionInfo {
  version: string
  commit: string
  build_time: string
}

export interface ReleaseInfo {
  version: string
  url: string
}

export interface UpdateStatus {
  phase: string
  target_version?: string
  message?: string
}

export interface DeploymentStatus {
  version: VersionInfo
  mode: string
  update_available: boolean
  latest_release?: ReleaseInfo
  update_status: UpdateStatus
}

export interface ApplyUpdateRequest {
  target_version: string
}
```

In `frontend/src/views/SettingsView.vue`, add deployment state:

```ts
import { getDeploymentStatus, checkForUpdate, applyUpdate, rollbackUpdate } from '@/api/deployment'
import type { DeploymentStatus } from '@/types'

const deployment = ref<DeploymentStatus | null>(null)
const deploymentLoading = ref(false)
const deploymentActionLoading = ref(false)
const deploymentMessage = ref('')

async function fetchDeploymentStatus() {
  deploymentLoading.value = true
  try {
    const res = await getDeploymentStatus()
    deployment.value = res.data.data
  } finally {
    deploymentLoading.value = false
  }
}
```

Call it during mount:

```ts
onMounted(async () => {
  await Promise.all([fetchProviders(), fetchLLMConfig(), fetchLDAPConfig(), fetchDeploymentStatus()])
})
```

Add the deployment card in the template:

```vue
<section class="rounded-lg bg-white p-6 shadow">
  <div class="flex items-center justify-between">
    <div>
      <h2 class="text-lg font-semibold text-gray-900">Deployment</h2>
      <p class="text-sm text-gray-500">Current version {{ deployment?.version.version || 'unknown' }}</p>
    </div>
    <span class="rounded-full bg-gray-100 px-3 py-1 text-xs font-medium text-gray-700">
      {{ deployment?.mode || 'unknown' }}
    </span>
  </div>

  <div class="mt-4 space-y-2 text-sm text-gray-600">
    <div>Commit: {{ deployment?.version.commit || 'unknown' }}</div>
    <div>Update phase: {{ deployment?.update_status.phase || 'idle' }}</div>
    <div v-if="deployment?.latest_release">Latest release: {{ deployment.latest_release.version }}</div>
  </div>

  <div class="mt-4 flex gap-3">
    <button class="rounded-md border px-4 py-2 text-sm" @click="handleCheckUpdates">Check Updates</button>
    <button class="rounded-md bg-emerald-600 px-4 py-2 text-sm text-white" @click="handleApplyUpdate">Apply Update</button>
    <button class="rounded-md bg-amber-600 px-4 py-2 text-sm text-white" @click="handleRollbackUpdate">Rollback</button>
  </div>
</section>
```

- [ ] **Step 4: Run the targeted frontend tests and build**

Run:

```bash
cd frontend
pnpm test -- --run api-modules settings-view
pnpm build
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/api/deployment.ts frontend/src/types/index.ts frontend/src/views/SettingsView.vue frontend/src/__tests__/api-modules.test.ts frontend/src/__tests__/settings-view.test.ts
git commit -m "feat(frontend): add deployment update controls"
```

### Task 6: Document The New Deployment Runtime And Verify The Whole Feature

**Files:**
- Create: `deploy/README.md`
- Modify: `docs/architecture.md`
- Verify: `cd backend && go test ./...`
- Verify: `cd frontend && pnpm test`
- Verify: `cd frontend && pnpm build`
- Verify: `docker compose --env-file deploy/.env.example -f deploy/docker-compose.yml config`
- Verify: `docker compose --env-file deploy/.env.example -f deploy/docker-compose.external.yml config`

- [ ] **Step 1: Update architecture.md to match the implemented deployment model**

Add to `docs/architecture.md`:

```md
- Official production deployment is Docker Compose-first.
- The business entrypoint remains the backend service that also serves the frontend bundle.
- An internal updater sidecar performs privileged compose update/rollback operations for admin-triggered online updates.
- Public health endpoints expose liveness/readiness, and admin settings expose deployment/update state.
```

Update the runtime diagram to include:

```mermaid
flowchart LR
    Browser --> Backend
    Backend --> DB
    Backend --> Redis
    Backend --> Relay
    Backend --> Updater
    Updater --> DockerHost
```

- [ ] **Step 2: Create the operator guide**

Create `deploy/README.md`:

````md
# Production Deployment Guide

## Bundled mode

~~~bash
cp deploy/.env.example deploy/.env
./deploy/docker-deploy.sh
docker compose --env-file deploy/.env -f deploy/docker-compose.yml up -d
~~~

## External mode

~~~bash
cp deploy/.env.example deploy/.env
./deploy/docker-deploy.sh external
docker compose --env-file deploy/.env -f deploy/docker-compose.external.yml up -d
~~~

## Online update

- Open the admin Settings page
- Use **Check Updates**
- Use **Apply Update** to move to the latest release
- Use **Rollback** if the new release fails health checks
````

- [ ] **Step 3: Run the full verification suite**

Run:

```bash
cd backend
go test ./...
cd ../frontend
pnpm test
pnpm build
cd ..
docker compose --env-file deploy/.env.example -f deploy/docker-compose.yml config >/dev/null
docker compose --env-file deploy/.env.example -f deploy/docker-compose.external.yml config >/dev/null
```

Expected: all commands succeed.

- [ ] **Step 4: Commit**

```bash
git add docs/architecture.md deploy/README.md
git commit -m "docs(deploy): document production deployment flow"
```

### Task 7: Add Local Dev/Local Compose Paths And One-Time SQLite Migration

**Files:**
- Create: `deploy/docker-compose.dev.yml`
- Create: `deploy/docker-compose.local.yml`
- Create: `deploy/migrate-sqlite-to-postgres.sh`
- Modify: `deploy/README.md`
- Modify: `docs/architecture.md`
- Verify: compose config + migration script syntax

- [ ] **Step 1: Write the failing asset checks**

Run:

```bash
cd <repo-root>
test -f deploy/docker-compose.dev.yml
test -f deploy/docker-compose.local.yml
test -f deploy/migrate-sqlite-to-postgres.sh
```

Expected: FAIL because the local dev/local compose assets do not exist yet.

- [ ] **Step 2: Add `docker-compose.dev.yml`**

Create `deploy/docker-compose.dev.yml` as the local source-build path:

- services: `backend`, `postgres`, `redis`
- no updater sidecar
- `backend` builds from local source using `deploy/Dockerfile`
- `AE_DB_DSN` points to the local Postgres container
- `AE_REDIS_ADDR` points to the local Redis container
- `AE_SERVER_MODE=debug`
- ports bind to localhost-friendly defaults for local testing

- [ ] **Step 3: Add `docker-compose.local.yml`**

Create `deploy/docker-compose.local.yml` as the directory-backed local test path:

- services: `backend`, `postgres`, `redis`
- no updater sidecar
- persist app/Postgres/Redis state under local directories such as:
  - `./data`
  - `./postgres_data`
  - `./redis_data`
- keep the runtime contract aligned with `docker-compose.dev.yml`
- document that this file is for long-lived local test environments, not production

- [ ] **Step 4: Add one-time SQLite migration helper**

Create `deploy/migrate-sqlite-to-postgres.sh`:

- source SQLite file: `backend/ai_efficiency.db`
- target: the Postgres instance started by the local test compose path
- implementation should use a one-shot containerized migrator so the host does not need extra tools preinstalled
- default behavior:
  - fail if the SQLite source file is missing
  - fail if Postgres is not ready
  - fail if the target database is not empty
- optional `--force-reset` path may reset the local target database before retrying

The script should print a short summary of what it migrated and clearly state that it is a one-time bootstrap helper, not an ongoing sync tool.

- [ ] **Step 5: Update docs**

Extend `deploy/README.md` with:

- when to use `docker-compose.yml`
- when to use `docker-compose.external.yml`
- when to use `docker-compose.dev.yml`
- when to use `docker-compose.local.yml`
- how to run the one-time SQLite-to-Postgres migration

Extend `docs/architecture.md` so the deployment section distinguishes:

- production Compose mode with updater sidecar
- local dev/local compose validation paths without updater sidecar

- [ ] **Step 6: Run verification**

Run:

```bash
cd <repo-root>
bash -n deploy/migrate-sqlite-to-postgres.sh
docker-compose --env-file deploy/.env.example -f deploy/docker-compose.dev.yml config >/dev/null
docker-compose --env-file deploy/.env.example -f deploy/docker-compose.local.yml config >/dev/null
```

Expected: all commands succeed.

- [ ] **Step 7: Commit**

```bash
git add deploy/docker-compose.dev.yml deploy/docker-compose.local.yml deploy/migrate-sqlite-to-postgres.sh deploy/README.md docs/architecture.md
git commit -m "feat(deploy): add local compose validation paths"
```

## Self-Review Checklist

- Spec coverage:
  - Docker Compose-first deployment: Tasks 1, 4, 6
  - `.env` operator config: Task 4
  - preflight readiness checks: Task 4
  - public liveness/readiness semantics: Task 2
  - admin deployment/update UI: Tasks 3 and 5
  - online update + rollback: Tasks 3 and 4
  - local dev/local compose validation paths: Task 7
  - one-time SQLite to Postgres local migration: Task 7
  - architecture/docs alignment: Tasks 6 and 7
- Placeholder scan:
  - No `TODO`, `TBD`, or “implement later” markers remain.
- Type consistency:
  - `VersionInfo`, `ReleaseInfo`, `UpdateStatus`, and `ApplyRequest` names are reused consistently across backend and frontend tasks.
