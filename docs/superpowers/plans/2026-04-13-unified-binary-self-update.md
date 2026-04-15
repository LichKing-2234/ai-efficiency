# Unified Binary Self-Update Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Docker updater sidecar model with a `sub2api`-style backend-managed binary self-update model shared by Docker and non-Docker deployments.

**Architecture:** Collapse deployment update behavior into a single backend update service that always downloads the platform backend bundle, verifies checksums, atomically replaces the runtime backend binary, and then asks the environment to restart the process. Docker mode will stop using a privileged updater sidecar and instead run a launcher/entrypoint that boots the backend from a writable persistent runtime path; non-Docker mode will reuse the same update/rollback semantics against its install path.

**Tech Stack:** Go (`gin`, `net/http`, `os`, `filepath`), Bash, Docker Compose, release bundles, Markdown docs, shell regression tests.

**Status:** ✅ 已完成（2026-04-15）

**Replay Status:** 历史完成记录。不要直接按本文逐 task 重跑；如需再次执行或扩展，请基于当前 deployment 资产、deployment 服务和最新 spec 重写执行计划。

> **Updated:** 2026-04-15 — 基于 `backend/internal/deployment` focused tests、`deploy/test/docker-deploy-*.sh` 和 frontend focused tests 回填 checkbox。

---

## File Map

### Create

- `deploy/docker-entrypoint.sh`
  Docker launcher that ensures a writable runtime binary exists under the deployment state directory and `exec`s it.
- `backend/internal/deployment/runtime_binary.go`
  Shared helpers for locating runtime binary paths, bootstrap binaries, `.backup`, and version comparison.
- `backend/internal/deployment/runtime_binary_test.go`
  Unit tests for runtime path resolution, bootstrap copy behavior, and version preference rules.
- `backend/internal/deployment/selfupdate.go`
  Shared self-update and rollback logic that downloads bundles, verifies checksums, extracts backend binaries, and atomically swaps runtime binaries.
- `backend/internal/deployment/selfupdate_test.go`
  Unit tests for update/rollback flows and restore-on-failure behavior.

### Modify

- `deploy/Dockerfile`
  Package launcher + bootstrap backend binary and make the container start through the launcher instead of directly running the backend.
- `deploy/docker-compose.yml`
  Remove updater sidecar, `docker.sock`, updater-specific env vars, and mount the state directory for runtime binary persistence.
- `deploy/docker-compose.bootstrap.yml`
  Same as above for the bootstrapped root-layout stack.
- `deploy/docker-compose.external.yml`
  Same as above for the external-infra production stack.
- `deploy/docker-compose.dev.yml`
  Optionally align launcher/runtime-path behavior for local source-build validation without reintroducing a sidecar.
- `deploy/docker-compose.local.yml`
  Optionally align launcher/runtime-path behavior for the persistent local validation path.
- `deploy/.env.example`
  Remove remaining updater-sidecar-only variables and document the runtime state directory semantics.
- `deploy/docker-deploy.sh`
  Stop referencing updater sidecar expectations in bootstrap/preflight, and add migration/preflight checks for old sidecar-based deployment layouts.
- `backend/internal/deployment/service.go`
  Replace updater-client orchestration with direct self-update orchestration and unify update behavior across modes.
- `backend/internal/deployment/updater_client.go`
  Remove or reduce to compatibility shims if no longer used.
- `backend/internal/deployment/updater_server.go`
  Remove once all callers and tests migrate away from the sidecar model.
- `backend/internal/deployment/updater_client_test.go`
  Remove or rewrite as direct self-update tests if the client disappears.
- `backend/internal/deployment/updater_server_test.go`
  Remove or replace with shared self-update tests.
- `backend/cmd/server/main.go`
  Wire the backend deployment service directly, pass runtime binary/self-update configuration, and stop wiring updater client URLs for Docker mode.
- `backend/internal/config/config.go`
  Remove updater-sidecar-only config and add any runtime-binary/launcher config defaults needed for both modes.
- `backend/internal/config/config_test.go`
  Update config expectations for the unified model.
- `backend/internal/handler/deployment.go`
  Keep existing API shape but ensure status/update/restart behavior reflects self-update semantics instead of sidecar semantics.
- `frontend/src/api/deployment.ts`
  API shape can likely stay stable; only adjust if response semantics change.
- `frontend/src/views/SettingsView.vue`
  Update deployment copy/messages to reflect self-update + restart semantics without mentioning updater sidecars.
- `frontend/src/__tests__/settings-view.test.ts`
  Update assertions for new deployment status text and behavior.
- `docs/architecture.md`
  Replace Docker sidecar references with launcher/runtime binary self-update.
- `docs/superpowers/specs/2026-04-08-production-deployment-packaging-design.md`
  Add explicit relationship note that Docker update control plane has been superseded by the unified self-update model.
- `docs/superpowers/specs/2026-04-09-binary-systemd-install-update-design.md`
  Add relationship note that its binary self-update semantics are now shared across Docker and non-Docker modes.
- `deploy/README.md`
  Explain launcher/runtime binary behavior, Docker restart semantics, and migration from the old sidecar model.
- `deploy/test/docker-deploy-bootstrap-test.sh`
  Update bootstrap expectations to reflect the absence of updater sidecar and any new layout/migration checks.
- `deploy/test/docker-deploy-preflight-test.sh`
  Update preflight expectations for no-sidecar Docker mode and old-layout detection/migration hints.

### Read Before Implementing

- `docs/superpowers/specs/2026-04-13-unified-binary-self-update-design.md`
- `docs/superpowers/specs/2026-04-08-production-deployment-packaging-design.md`
- `docs/superpowers/specs/2026-04-09-binary-systemd-install-update-design.md`
- `backend/internal/deployment/service.go`
- `backend/internal/deployment/updater_client.go`
- `backend/internal/deployment/updater_server.go`
- `backend/cmd/server/main.go`
- `deploy/Dockerfile`
- `deploy/docker-compose.yml`
- `deploy/docker-compose.bootstrap.yml`
- `deploy/docker-compose.external.yml`
- `frontend/src/views/SettingsView.vue`

### Boundaries Locked By This Plan

1. Docker mode will no longer perform online updates by editing image tags and calling `docker compose pull/up`.
2. Docker mode will no longer require a privileged updater sidecar or `docker.sock` mount.
3. Docker and non-Docker modes will share one backend self-update implementation.
4. Runtime version truth moves from Docker image tag state to the runtime binary actually being executed.
5. Frontend/API surface remains conceptually the same: check, update, rollback, restart.

---

### Task 1: Lock The New Deployment Model With Failing Tests

**Files:**
- Modify: `backend/internal/deployment/updater_server_test.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `deploy/test/docker-deploy-bootstrap-test.sh`
- Modify: `deploy/test/docker-deploy-preflight-test.sh`
- Test: `backend/internal/deployment/updater_server_test.go`
- Test: `backend/internal/config/config_test.go`
- Test: `deploy/test/docker-deploy-bootstrap-test.sh`
- Test: `deploy/test/docker-deploy-preflight-test.sh`

- [x] **Step 1: Add failing backend tests that describe sidecar removal and unified self-update behavior**

Append to `backend/internal/config/config_test.go`:

```go
func TestLoadDeploymentConfigDoesNotRequireUpdaterURL(t *testing.T) {
	t.Setenv("AE_DEPLOYMENT_MODE", "bundled")
	t.Setenv("AE_DEPLOYMENT_STATE_DIR", "/var/lib/ai-efficiency")

	cfg, err := Load("/nonexistent/config.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Deployment.Update.UpdaterURL != "" {
		t.Fatalf("deployment updater url = %q, want empty in unified self-update mode", cfg.Deployment.Update.UpdaterURL)
	}
}
```

Append to `backend/internal/deployment/updater_server_test.go`:

```go
func TestUpdaterServerIsNoLongerTheDockerUpdatePath(t *testing.T) {
	t.Skip("replace with self-update tests once sidecar code is removed")
}
```

Also add a new failing self-update test stub at the bottom of `backend/internal/deployment/updater_server_test.go`:

```go
func TestUnifiedSelfUpdateWillReplaceRuntimeBinaryWithoutComposeTagMutation(t *testing.T) {
	t.Fatalf("pending self-update implementation")
}
```

- [x] **Step 2: Add failing deploy shell assertions for no-sidecar Docker mode**

In `deploy/test/docker-deploy-bootstrap-test.sh`, add assertions after successful bootstrap:

```bash
! grep -q 'updater:' "$WORK_DIR/docker-compose.yml"
! grep -q '/var/run/docker.sock' "$WORK_DIR/docker-compose.yml"
grep -q '/var/lib/ai-efficiency/runtime' "$WORK_DIR/docker-compose.yml"
```

In `deploy/test/docker-deploy-preflight-test.sh`, add a repo-layout fixture that contains a legacy sidecar compose snippet and assert preflight emits a migration warning, for example:

```bash
LEGACY_FIXTURE="$TMP_ROOT/repo-fixture-legacy"
mkdir -p "$LEGACY_FIXTURE/deploy"
cp "$ROOT_DIR/deploy/docker-deploy.sh" "$LEGACY_FIXTURE/deploy/docker-deploy.sh"
cp "$ROOT_DIR/deploy/.env.example" "$LEGACY_FIXTURE/deploy/.env.example"
cat >"$LEGACY_FIXTURE/deploy/docker-compose.yml" <<'EOF'
services:
  updater:
    image: ghcr.io/lichking-2234/ai-efficiency:latest
EOF
cp "$REPO_FIXTURE/deploy/.env" "$LEGACY_FIXTURE/deploy/.env"

set +e
(
  cd "$LEGACY_FIXTURE"
  env -i PATH="$FAKE_BIN:$PATH" bash deploy/docker-deploy.sh
) >"$TMP_ROOT/legacy-preflight.log" 2>&1
legacy_status=$?
set -e

test "$legacy_status" -ne 0
grep -q "legacy Docker updater-sidecar deployment detected" "$TMP_ROOT/legacy-preflight.log"
```

- [x] **Step 3: Run the targeted tests and confirm they fail**

Run:

```bash
cd backend
go test ./internal/config ./internal/deployment -run 'TestLoadDeploymentConfigDoesNotRequireUpdaterURL|TestUnifiedSelfUpdateWillReplaceRuntimeBinaryWithoutComposeTagMutation' -count=1
cd ..
bash deploy/test/docker-deploy-bootstrap-test.sh
bash deploy/test/docker-deploy-preflight-test.sh
```

Expected:

- backend tests fail because config and deployment code still assume updater-sidecar semantics
- bootstrap/preflight shell tests fail because production compose files still include updater sidecar and `docker.sock`

- [x] **Step 4: Commit the failing-test checkpoint**

```bash
git add backend/internal/config/config_test.go backend/internal/deployment/updater_server_test.go deploy/test/docker-deploy-bootstrap-test.sh deploy/test/docker-deploy-preflight-test.sh
git commit -m "test(deploy): lock unified self-update model"
```

---

### Task 2: Introduce Shared Runtime-Binary Update Primitives

**Files:**
- Create: `backend/internal/deployment/runtime_binary.go`
- Create: `backend/internal/deployment/runtime_binary_test.go`
- Create: `backend/internal/deployment/selfupdate.go`
- Create: `backend/internal/deployment/selfupdate_test.go`
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Test: `backend/internal/deployment/runtime_binary_test.go`
- Test: `backend/internal/deployment/selfupdate_test.go`
- Test: `backend/internal/config/config_test.go`

- [x] **Step 1: Write failing unit tests for runtime path resolution and binary replacement**

Create `backend/internal/deployment/runtime_binary_test.go`:

```go
package deployment

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeBinaryPaths(t *testing.T) {
	root := t.TempDir()
	paths := RuntimeBinaryPaths(root)
	if paths.RuntimeBinary != filepath.Join(root, "runtime", "ai-efficiency-server") {
		t.Fatalf("runtime binary = %q", paths.RuntimeBinary)
	}
	if paths.BackupBinary != filepath.Join(root, "runtime", "ai-efficiency-server.backup") {
		t.Fatalf("backup binary = %q", paths.BackupBinary)
	}
}

func TestPreferExistingRuntimeBinaryWhenBootstrapVersionIsOlder(t *testing.T) {
	root := t.TempDir()
	runtimeDir := filepath.Join(root, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runtimePath := filepath.Join(runtimeDir, "ai-efficiency-server")
	if err := os.WriteFile(runtimePath, []byte("runtime-newer"), 0o755); err != nil {
		t.Fatalf("write runtime: %v", err)
	}
	bootstrapPath := filepath.Join(root, "bootstrap-server")
	if err := os.WriteFile(bootstrapPath, []byte("bootstrap-older"), 0o755); err != nil {
		t.Fatalf("write bootstrap: %v", err)
	}

	chosen, copied, err := EnsureRuntimeBinary(root, bootstrapPath, "v0.6.0", "v0.5.0")
	if err != nil {
		t.Fatalf("EnsureRuntimeBinary: %v", err)
	}
	if chosen != runtimePath || copied {
		t.Fatalf("chosen=%q copied=%v", chosen, copied)
	}
}
```

Create `backend/internal/deployment/selfupdate_test.go`:

```go
package deployment

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestApplyBinaryUpdateReplacesRuntimeBinaryAndCreatesBackup(t *testing.T) {
	root := t.TempDir()
	paths := RuntimeBinaryPaths(root)
	if err := os.MkdirAll(filepath.Dir(paths.RuntimeBinary), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(paths.RuntimeBinary, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("write runtime: %v", err)
	}
	newBinary := filepath.Join(root, "new-binary")
	if err := os.WriteFile(newBinary, []byte("new-binary"), 0o755); err != nil {
		t.Fatalf("write new binary: %v", err)
	}

	if err := ApplyBinarySwap(context.Background(), paths, newBinary); err != nil {
		t.Fatalf("ApplyBinarySwap: %v", err)
	}
	got, _ := os.ReadFile(paths.RuntimeBinary)
	if string(got) != "new-binary" {
		t.Fatalf("runtime binary = %q", string(got))
	}
	backup, _ := os.ReadFile(paths.BackupBinary)
	if string(backup) != "old-binary" {
		t.Fatalf("backup binary = %q", string(backup))
	}
}
```

- [x] **Step 2: Run the new targeted tests to confirm they fail**

Run:

```bash
cd backend
go test ./internal/deployment ./internal/config -run 'TestRuntimeBinaryPaths|TestPreferExistingRuntimeBinaryWhenBootstrapVersionIsOlder|TestApplyBinaryUpdateReplacesRuntimeBinaryAndCreatesBackup|TestLoadDeploymentConfigDoesNotRequireUpdaterURL' -count=1
```

Expected: FAIL because the new runtime/self-update helpers and relaxed config do not exist yet.

- [x] **Step 3: Implement the runtime-binary helpers and config defaults**

Create `backend/internal/deployment/runtime_binary.go` with:

```go
package deployment

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type RuntimePaths struct {
	RuntimeDir    string
	RuntimeBinary string
	BackupBinary  string
}

func RuntimeBinaryPaths(stateDir string) RuntimePaths {
	runtimeDir := filepath.Join(stateDir, "runtime")
	return RuntimePaths{
		RuntimeDir:    runtimeDir,
		RuntimeBinary: filepath.Join(runtimeDir, "ai-efficiency-server"),
		BackupBinary:  filepath.Join(runtimeDir, "ai-efficiency-server.backup"),
	}
}

func EnsureRuntimeBinary(stateDir, bootstrapBinary, runtimeVersion, bootstrapVersion string) (string, bool, error) {
	paths := RuntimeBinaryPaths(stateDir)
	if err := os.MkdirAll(paths.RuntimeDir, 0o755); err != nil {
		return "", false, fmt.Errorf("mkdir runtime dir: %w", err)
	}
	if _, err := os.Stat(paths.RuntimeBinary); os.IsNotExist(err) {
		if err := copyExecutable(bootstrapBinary, paths.RuntimeBinary); err != nil {
			return "", false, err
		}
		return paths.RuntimeBinary, true, nil
	}
	if runtimeVersion == "" || bootstrapVersion == "" {
		return paths.RuntimeBinary, false, nil
	}
	if CompareVersions(runtimeVersion, bootstrapVersion) >= 0 {
		return paths.RuntimeBinary, false, nil
	}
	if err := copyExecutable(bootstrapBinary, paths.RuntimeBinary); err != nil {
		return "", false, err
	}
	return paths.RuntimeBinary, true, nil
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source binary: %w", err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create runtime binary: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copy runtime binary: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close runtime binary: %w", err)
	}
	if err := os.Chmod(dst, 0o755); err != nil {
		return fmt.Errorf("chmod runtime binary: %w", err)
	}
	return nil
}
```

Create `backend/internal/deployment/selfupdate.go` with:

```go
package deployment

import (
	"context"
	"fmt"
	"os"
)

func ApplyBinarySwap(_ context.Context, paths RuntimePaths, newBinaryPath string) error {
	_ = os.Remove(paths.BackupBinary)
	if err := os.Rename(paths.RuntimeBinary, paths.BackupBinary); err != nil {
		return fmt.Errorf("backup current runtime binary: %w", err)
	}
	if err := os.Rename(newBinaryPath, paths.RuntimeBinary); err != nil {
		if restoreErr := os.Rename(paths.BackupBinary, paths.RuntimeBinary); restoreErr != nil {
			return fmt.Errorf("replace runtime binary: %w (restore backup: %v)", err, restoreErr)
		}
		return fmt.Errorf("replace runtime binary: %w", err)
	}
	return nil
}

func RollbackBinarySwap(paths RuntimePaths) error {
	if _, err := os.Stat(paths.BackupBinary); err != nil {
		return fmt.Errorf("stat backup binary: %w", err)
	}
	_ = os.Remove(paths.RuntimeBinary)
	if err := os.Rename(paths.BackupBinary, paths.RuntimeBinary); err != nil {
		return fmt.Errorf("restore backup binary: %w", err)
	}
	return nil
}
```

In `backend/internal/config/config.go`, remove the updater-sidecar default:

```go
v.SetDefault("deployment.update.updater_url", "")
```

And ensure `UpdateConfig` no longer requires a non-empty updater URL in code paths that remain after refactor.

- [x] **Step 4: Re-run the targeted tests and confirm they pass**

Run:

```bash
cd backend
go test ./internal/deployment ./internal/config -run 'TestRuntimeBinaryPaths|TestPreferExistingRuntimeBinaryWhenBootstrapVersionIsOlder|TestApplyBinaryUpdateReplacesRuntimeBinaryAndCreatesBackup|TestLoadDeploymentConfigDoesNotRequireUpdaterURL' -count=1
```

Expected: PASS.

- [x] **Step 5: Commit the shared runtime/self-update primitives**

```bash
git add backend/internal/deployment/runtime_binary.go backend/internal/deployment/runtime_binary_test.go backend/internal/deployment/selfupdate.go backend/internal/deployment/selfupdate_test.go backend/internal/config/config.go backend/internal/config/config_test.go
git commit -m "feat(backend): add shared binary self-update primitives"
```

---

### Task 3: Replace Docker Sidecar Deployment With Launcher-Based Runtime Binary Execution

**Files:**
- Create: `deploy/docker-entrypoint.sh`
- Modify: `deploy/Dockerfile`
- Modify: `deploy/docker-compose.yml`
- Modify: `deploy/docker-compose.bootstrap.yml`
- Modify: `deploy/docker-compose.external.yml`
- Modify: `deploy/.env.example`
- Modify: `deploy/test/docker-deploy-bootstrap-test.sh`
- Modify: `deploy/test/docker-deploy-preflight-test.sh`
- Test: `deploy/test/docker-deploy-bootstrap-test.sh`
- Test: `deploy/test/docker-deploy-preflight-test.sh`

- [x] **Step 1: Add failing shell assertions for no-sidecar launcher mode**

In `deploy/test/docker-deploy-bootstrap-test.sh`, after the successful bootstrap assertions, add:

```bash
! grep -q '^  updater:' "$WORK_DIR/docker-compose.yml"
! grep -q '/var/run/docker.sock' "$WORK_DIR/docker-compose.yml"
grep -q 'docker-entrypoint.sh' "$WORK_DIR/docker-compose.yml"
grep -q '/var/lib/ai-efficiency/runtime/ai-efficiency-server' "$WORK_DIR/docker-compose.yml"
```

In `deploy/test/docker-deploy-preflight-test.sh`, add a negative check for the legacy sidecar layout warning:

```bash
LEGACY_FIXTURE="$TMP_ROOT/repo-fixture-legacy"
mkdir -p "$LEGACY_FIXTURE/deploy"
cp "$ROOT_DIR/deploy/docker-deploy.sh" "$LEGACY_FIXTURE/deploy/docker-deploy.sh"
cp "$ROOT_DIR/deploy/.env.example" "$LEGACY_FIXTURE/deploy/.env.example"
cat >"$LEGACY_FIXTURE/deploy/docker-compose.yml" <<'EOF'
services:
  updater:
    image: ghcr.io/lichking-2234/ai-efficiency:latest
EOF
cp "$REPO_FIXTURE/deploy/.env" "$LEGACY_FIXTURE/deploy/.env"

set +e
(
  cd "$LEGACY_FIXTURE"
  env -i PATH="$FAKE_BIN:$PATH" bash deploy/docker-deploy.sh
) >"$TMP_ROOT/legacy-preflight.log" 2>&1
legacy_status=$?
set -e
test "$legacy_status" -ne 0
grep -q "legacy Docker updater-sidecar deployment detected" "$TMP_ROOT/legacy-preflight.log"
```

- [x] **Step 2: Run the shell tests and confirm they fail**

Run:

```bash
bash deploy/test/docker-deploy-bootstrap-test.sh
bash deploy/test/docker-deploy-preflight-test.sh
```

Expected: FAIL because compose files still include the sidecar and no launcher/runtime binary path exists yet.

- [x] **Step 3: Implement the launcher and remove the sidecar from production compose**

Create `deploy/docker-entrypoint.sh`:

```bash
#!/usr/bin/env sh
set -eu

STATE_DIR="${AE_DEPLOYMENT_STATE_DIR:-/var/lib/ai-efficiency}"
RUNTIME_DIR="${STATE_DIR}/runtime"
RUNTIME_BINARY="${RUNTIME_DIR}/ai-efficiency-server"
BOOTSTRAP_BINARY="/opt/ai-efficiency/bootstrap/ai-efficiency-server"

mkdir -p "$RUNTIME_DIR"

if [ ! -x "$RUNTIME_BINARY" ]; then
  cp "$BOOTSTRAP_BINARY" "$RUNTIME_BINARY"
  chmod 755 "$RUNTIME_BINARY"
fi

exec "$RUNTIME_BINARY"
```

In `deploy/Dockerfile`, copy the bootstrap binary and entrypoint:

```dockerfile
COPY --from=builder /app/backend/bin/server /opt/ai-efficiency/bootstrap/ai-efficiency-server
COPY deploy/docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh
ENTRYPOINT ["/docker-entrypoint.sh"]
```

In the three production compose files:

- remove the entire `updater:` service
- remove `depends_on: updater`
- remove `/var/run/docker.sock` mounts
- keep the backend image but add:

```yaml
volumes:
  - appstate:/var/lib/ai-efficiency
```

and for bootstrap:

```yaml
volumes:
  - ./data:/var/lib/ai-efficiency
```

The backend service must no longer carry updater-sidecar-only env vars.

In `deploy/.env.example`, remove any remaining updater-sidecar variables and add a short comment above the security section:

```dotenv
# Docker runtime state, including the self-updating backend binary, is stored under
# AE_DEPLOYMENT_STATE_DIR (default: /var/lib/ai-efficiency) inside the mounted app state volume.
```

- [x] **Step 4: Teach preflight to reject legacy sidecar layouts**

Add to `deploy/docker-deploy.sh` a check before compose validation:

```bash
if grep -q '^[[:space:]]*updater:' "$COMPOSE_FILE"; then
  echo "legacy Docker updater-sidecar deployment detected; refresh deploy assets before continuing" >&2
  exit 1
fi
```

- [x] **Step 5: Re-run the shell tests and confirm they pass**

Run:

```bash
bash deploy/test/docker-deploy-bootstrap-test.sh
bash deploy/test/docker-deploy-preflight-test.sh
```

Expected: PASS.

- [x] **Step 6: Commit the Docker launcher migration**

```bash
git add deploy/docker-entrypoint.sh deploy/Dockerfile deploy/docker-compose.yml deploy/docker-compose.bootstrap.yml deploy/docker-compose.external.yml deploy/.env.example deploy/docker-deploy.sh deploy/test/docker-deploy-bootstrap-test.sh deploy/test/docker-deploy-preflight-test.sh
git commit -m "refactor(deploy): replace docker updater sidecar with launcher"
```

---

### Task 4: Replace Sidecar-Oriented Deployment Service With Unified Self-Update Service

**Files:**
- Modify: `backend/internal/deployment/service.go`
- Modify: `backend/internal/deployment/updater_client.go`
- Modify: `backend/internal/deployment/updater_client_test.go`
- Modify: `backend/internal/deployment/updater_server.go`
- Modify: `backend/internal/deployment/updater_server_test.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/internal/handler/deployment.go`
- Test: `backend/internal/deployment/service_test.go`
- Test: `backend/internal/handler/deployment_http_test.go`

- [x] **Step 1: Add failing service tests for unified self-update orchestration**

Append to `backend/internal/deployment/service_test.go`:

```go
func TestApplyUpdateUsesSelfUpdatePathWithoutUpdaterClient(t *testing.T) {
	t.Fatal("pending unified self-update service implementation")
}

func TestRollbackUpdateUsesRuntimeBackupWithoutUpdaterClient(t *testing.T) {
	t.Fatal("pending unified self-update service implementation")
}
```

Append to `backend/internal/handler/deployment_http_test.go`:

```go
func TestApplyUpdateReturnsNeedRestartForSelfUpdate(t *testing.T) {
	t.Fatal("pending deployment handler self-update response test")
}
```

- [x] **Step 2: Run the targeted backend tests to confirm they fail**

Run:

```bash
cd backend
go test ./internal/deployment ./internal/handler -run 'TestApplyUpdateUsesSelfUpdatePathWithoutUpdaterClient|TestRollbackUpdateUsesRuntimeBackupWithoutUpdaterClient|TestApplyUpdateReturnsNeedRestartForSelfUpdate' -count=1
```

Expected: FAIL because deployment service still routes through updater client semantics.

- [x] **Step 3: Implement the unified self-update service**

In `backend/internal/deployment/service.go`, replace updater-client orchestration with a direct self-update executor. The service should own:

- current runtime binary version detection
- latest release lookup
- bundle download + checksum verification
- runtime binary swap / rollback
- update status reporting

Use a structure like:

```go
type RuntimeUpdater interface {
	Apply(context.Context, ApplyRequest) (UpdateStatus, error)
	Rollback(context.Context) (UpdateStatus, error)
}
```

and wire that into `Service` instead of `Updater`.

In `backend/cmd/server/main.go`, stop constructing an updater HTTP client for Docker mode and instead construct the runtime self-update executor with:

- deployment mode
- state dir
- current build version
- release source client

`backend/internal/handler/deployment.go` should keep returning the same API envelope but populate it from the unified update service.

- [x] **Step 4: Re-run the targeted backend tests and confirm they pass**

Run:

```bash
cd backend
go test ./internal/deployment ./internal/handler -run 'TestApplyUpdateUsesSelfUpdatePathWithoutUpdaterClient|TestRollbackUpdateUsesRuntimeBackupWithoutUpdaterClient|TestApplyUpdateReturnsNeedRestartForSelfUpdate' -count=1
```

Expected: PASS.

- [x] **Step 5: Commit the unified update service migration**

```bash
git add backend/internal/deployment/service.go backend/internal/deployment/updater_client.go backend/internal/deployment/updater_client_test.go backend/internal/deployment/updater_server.go backend/internal/deployment/updater_server_test.go backend/cmd/server/main.go backend/internal/handler/deployment.go backend/internal/deployment/service_test.go backend/internal/handler/deployment_http_test.go
git commit -m "feat(backend): unify deployment self-update flow"
```

---

### Task 5: Update Frontend Copy, Docs, And Full Verification

**Files:**
- Modify: `frontend/src/views/SettingsView.vue`
- Modify: `frontend/src/__tests__/settings-view.test.ts`
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-04-08-production-deployment-packaging-design.md`
- Modify: `docs/superpowers/specs/2026-04-09-binary-systemd-install-update-design.md`
- Modify: `deploy/README.md`
- Test: `frontend/src/__tests__/settings-view.test.ts`
- Test: `backend/internal/deployment/...`
- Test: `deploy/test/docker-deploy-bootstrap-test.sh`
- Test: `deploy/test/docker-deploy-preflight-test.sh`

- [x] **Step 1: Update frontend deployment copy for unified self-update**

In `frontend/src/views/SettingsView.vue`, change any updater-sidecar-specific wording to text like:

```ts
setDeploymentMessage('success', 'Update request submitted. Restart the service when the binary swap completes.')
```

and ensure any Docker-mode-specific copy no longer mentions pulling images or updater sidecars.

In `frontend/src/__tests__/settings-view.test.ts`, update assertions to match the new copy.

- [x] **Step 2: Update architecture and spec relationship docs**

In `docs/architecture.md`, replace lines describing:

- Docker updater sidecar
- privileged Docker control path

with language describing:

- launcher-based runtime binary execution
- backend-managed self-update across Docker and non-Docker

In `docs/superpowers/specs/2026-04-08-production-deployment-packaging-design.md`, add a short note near the top:

```md
> Update note (2026-04-13): Docker online update behavior is superseded by `2026-04-13-unified-binary-self-update-design.md`. Historical discussion of updater sidecars is preserved here for evolution context.
```

In `docs/superpowers/specs/2026-04-09-binary-systemd-install-update-design.md`, add:

```md
> Relationship note (2026-04-13): The binary self-update model described here is now the baseline for both Docker and non-Docker runtime modes; this spec remains the historical design entry for the non-Docker path.
```

- [x] **Step 3: Rewrite `deploy/README.md` to match the unified update model**

Add language such as:

```md
Docker mode now runs the backend from a persistent runtime binary under `AE_DEPLOYMENT_STATE_DIR`.
Online update and rollback no longer depend on a separate updater sidecar or Docker socket access.
After an update or rollback request completes, restart the service/container to run the swapped binary.
```

Remove any remaining mention of updater sidecars from the operator docs.

- [x] **Step 4: Run the full verification suite for this feature**

Run:

```bash
cd backend
go test ./internal/deployment ./internal/handler ./internal/config -count=1
cd ..
bash deploy/test/docker-deploy-bootstrap-test.sh
bash deploy/test/docker-deploy-preflight-test.sh
cd frontend
pnpm test --runInBand src/__tests__/settings-view.test.ts
```

Expected: all commands pass.

- [x] **Step 5: Review the diff and commit the docs/UI alignment**

Run:

```bash
git diff -- docs frontend deploy backend/internal/deployment backend/internal/handler backend/cmd/server
```

Expected: only files relevant to the unified self-update migration are touched.

Then commit:

```bash
git add frontend/src/views/SettingsView.vue frontend/src/__tests__/settings-view.test.ts docs/architecture.md docs/superpowers/specs/2026-04-08-production-deployment-packaging-design.md docs/superpowers/specs/2026-04-09-binary-systemd-install-update-design.md deploy/README.md
git commit -m "docs(deploy): document unified self-update model"
```
