# Binary Systemd Install And Update Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Linux `install.sh + systemd` deployment path that downloads GitHub Release binaries, installs them under `/opt/ai-efficiency`, and supports binary self-update / rollback / restart while keeping the existing Docker/updater-sidecar route intact.

**Architecture:** Deployment becomes explicitly dual-path. `compose` mode continues to use image/tag updates through the updater sidecar, while `systemd` mode introduces a backend-owned binary update service that downloads release bundles, verifies checksums, atomically replaces the installed server binary, preserves `.backup`, and restarts the `ai-efficiency` systemd service. The installation path is modeled after `sub2api`: GitHub Release download, system user, fixed install paths, and a systemd unit file.

**Tech Stack:** Go (`gin`, existing deployment/release services, `os/exec` for systemctl integration), shell scripts, systemd unit files, GitHub Release assets, existing frontend deployment settings page.

---

## File Map

### New files

- `backend/internal/deployment/systemd_asset.go`
  Select the correct backend release asset and checksum entry for the current Linux platform.
- `backend/internal/deployment/systemd_asset_test.go`
  Unit tests for archive/checksum selection and platform mismatch behavior.
- `backend/internal/deployment/systemd_binary_updater.go`
  Download, verify, extract, atomically replace, and rollback the installed `ai-efficiency-server`.
- `backend/internal/deployment/systemd_binary_updater_test.go`
  Unit tests for apply/rollback, checksum failure, and restore-on-failure behavior.
- `backend/internal/deployment/systemd_service_manager.go`
  Small abstraction around `systemctl restart/status` for the installed service.
- `backend/internal/deployment/systemd_service_manager_test.go`
  Unit tests for command construction and restart behavior.
- `deploy/install.sh`
  GitHub-Release-driven installer for Linux systemd deployments.
- `deploy/ai-efficiency.service`
  systemd unit template for the installed service.

### Modified files

- `backend/internal/deployment/service.go`
  Route deployment operations by mode: compose via updater client, systemd via binary updater + systemd manager.
- `backend/internal/deployment/service_test.go`
  Extend service tests to cover `systemd` mode status/check/apply/rollback behavior.
- `backend/internal/deployment/release_source.go`
  Extend release metadata shape if needed so asset lists can be consumed by systemd updater logic.
- `backend/internal/deployment/release_source_test.go`
  Cover any release metadata changes needed by systemd asset selection.
- `backend/internal/handler/deployment.go`
  Add a restart action and keep deployment handler mode-agnostic.
- `backend/internal/handler/deployment_http_test.go`
  Cover restart endpoint and systemd-specific response paths.
- `backend/internal/handler/router.go`
  Register a deployment restart route.
- `backend/cmd/server/main.go`
  Wire systemd mode service dependencies into the deployment service while preserving compose mode behavior.
- `frontend/src/api/deployment.ts`
  Add restart action API.
- `frontend/src/types/index.ts`
  Extend deployment/update result typing if restart metadata is exposed.
- `frontend/src/views/SettingsView.vue`
  Add restart button and mode-aware deployment messaging.
- `frontend/src/__tests__/api-modules.test.ts`
  Cover deployment restart API.
- `frontend/src/__tests__/settings-view.test.ts`
  Cover restart control rendering and invocation.
- `deploy/README.md`
  Document the new Linux binary/systemd installation path, required edits, and update semantics.
- `deploy/config.example.yaml`
  Document `deployment.mode=systemd` and any binary-path/systemd settings if added.
- `.goreleaser.yaml`
  Ensure backend bundle includes `deploy/install.sh` and `deploy/ai-efficiency.service`.
- `docs/architecture.md`
  Document the dual-path deployment model: Compose + updater sidecar versus systemd + self-update.

### Existing files to read before implementation

- `backend/internal/deployment/service.go`
- `backend/internal/deployment/service_test.go`
- `backend/internal/deployment/release_source.go`
- `backend/internal/deployment/release_source_test.go`
- `backend/internal/deployment/updater_client.go`
- `backend/internal/handler/deployment.go`
- `backend/internal/handler/deployment_http_test.go`
- `backend/internal/handler/router.go`
- `backend/cmd/server/main.go`
- `frontend/src/api/deployment.ts`
- `frontend/src/views/SettingsView.vue`
- `deploy/README.md`
- `/tmp/sub2api-reference/deploy/install.sh`
- `/tmp/sub2api-reference/deploy/sub2api.service`
- `/tmp/sub2api-reference/backend/internal/service/update_service.go`
- `/tmp/sub2api-reference/backend/internal/handler/admin/system_handler.go`

### Decisions locked in by this plan

1. Docker / Compose keeps using updater sidecar and image/tag replacement.
2. Linux systemd deployments use GitHub Release binary bundles and backend self-update.
3. `systemd` mode is Linux-only; non-Linux release archives are still published but do not get install/service automation.
4. systemd installation path uses:
   - install dir: `/opt/ai-efficiency`
   - config path: `/etc/ai-efficiency/config.yaml`
   - data dir: `/var/lib/ai-efficiency`
   - service name: `ai-efficiency`
5. `systemd` mode uses an explicit restart endpoint; `compose` mode may return a policy/conflict error for restart.
6. The backend release binary must fail fast in release builds without an explicit DB DSN; SQLite fallback remains dev-only.

---

### Task 1: Add Linux Install Assets And Ship Them In Release Bundles

**Files:**
- Create: `deploy/install.sh`
- Create: `deploy/ai-efficiency.service`
- Modify: `.goreleaser.yaml`
- Modify: `deploy/README.md`
- Test: `deploy/install.sh`

- [ ] **Step 1: Write the failing deploy-asset verification steps**

Run:

```bash
cd /Users/admin/ai-efficiency
test -f deploy/install.sh
test -f deploy/ai-efficiency.service
GOPROXY=https://goproxy.cn,direct go run github.com/goreleaser/goreleaser/v2@latest check --config .goreleaser.yaml
bash -n deploy/install.sh
```

Expected:
- `test -f` fails because the files do not exist yet.
- `goreleaser check` may pass now but does not yet include the new deploy files.

- [ ] **Step 2: Create the systemd unit template**

Create `deploy/ai-efficiency.service`:

```ini
[Unit]
Description=AI Efficiency Platform
Documentation=https://github.com/LichKing-2234/ai-efficiency
After=network.target

[Service]
Type=simple
User=ai-efficiency
Group=ai-efficiency
WorkingDirectory=/opt/ai-efficiency
ExecStart=/opt/ai-efficiency/ai-efficiency-server
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=ai-efficiency
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ReadWritePaths=/opt/ai-efficiency /var/lib/ai-efficiency
Environment=AE_CONFIG_PATH=/etc/ai-efficiency/config.yaml
Environment=AE_SERVER_MODE=release

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 3: Create the installer script**

Create `deploy/install.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

GITHUB_REPO="LichKing-2234/ai-efficiency"
INSTALL_DIR="/opt/ai-efficiency"
CONFIG_DIR="/etc/ai-efficiency"
DATA_DIR="/var/lib/ai-efficiency"
SERVICE_NAME="ai-efficiency"
SERVICE_USER="ai-efficiency"

require_root() {
  if [[ "$(id -u)" -ne 0 ]]; then
    echo "please run as root" >&2
    exit 1
  fi
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing required command: $1" >&2; exit 1; }
}

detect_platform() {
  OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
  ARCH="$(uname -m)"
  case "$OS" in
    linux) ;;
    *) echo "unsupported OS: $OS" >&2; exit 1 ;;
  esac
  case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "unsupported architecture: $ARCH" >&2; exit 1 ;;
  esac
}

latest_version() {
  curl -fsSL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" | \
    grep '"tag_name"' | head -1 | sed -E 's/.*"([^"]+)".*/\1/'
}

download_release() {
  local tag="$1"
  local version="${tag#v}"
  local archive="ai-efficiency-backend_${version}_${OS}_${ARCH}.tar.gz"
  local base="https://github.com/${GITHUB_REPO}/releases/download/${tag}"
  curl -fsSL "${base}/${archive}" -o "${TMP_DIR}/${archive}"
  curl -fsSL "${base}/checksums.txt" -o "${TMP_DIR}/checksums.txt"
  local expected actual
  expected="$(grep "${archive}" "${TMP_DIR}/checksums.txt" | awk '{print $1}')"
  actual="$(sha256sum "${TMP_DIR}/${archive}" | awk '{print $1}')"
  [[ -n "$expected" ]] || { echo "missing checksum for ${archive}" >&2; exit 1; }
  [[ "$expected" == "$actual" ]] || { echo "checksum mismatch for ${archive}" >&2; exit 1; }
  tar -xzf "${TMP_DIR}/${archive}" -C "${TMP_DIR}"
}

ensure_user() {
  if ! id "${SERVICE_USER}" >/dev/null 2>&1; then
    useradd -r -s /bin/sh -d "${INSTALL_DIR}" "${SERVICE_USER}"
  fi
}

install_files() {
  mkdir -p "${INSTALL_DIR}" "${CONFIG_DIR}" "${DATA_DIR}"
  cp "${TMP_DIR}/ai-efficiency-server" "${INSTALL_DIR}/ai-efficiency-server"
  chmod 0755 "${INSTALL_DIR}/ai-efficiency-server"
  mkdir -p "${INSTALL_DIR}/deploy"
  cp "${TMP_DIR}/deploy/"* "${INSTALL_DIR}/deploy/" 2>/dev/null || true
  if [[ ! -f "${CONFIG_DIR}/config.yaml" ]]; then
    cp "${TMP_DIR}/deploy/config.example.yaml" "${CONFIG_DIR}/config.yaml"
  fi
  cp "${TMP_DIR}/deploy/ai-efficiency.service" "/etc/systemd/system/${SERVICE_NAME}.service"
  chown -R "${SERVICE_USER}:${SERVICE_USER}" "${INSTALL_DIR}" "${DATA_DIR}"
}

enable_service() {
  systemctl daemon-reload
  systemctl enable "${SERVICE_NAME}"
}

main() {
  require_root
  require_cmd curl
  require_cmd tar
  require_cmd sha256sum
  detect_platform
  TMP_DIR="$(mktemp -d)"
  trap 'rm -rf "${TMP_DIR}"' EXIT
  TAG="${1:-$(latest_version)}"
  download_release "${TAG}"
  ensure_user
  install_files
  enable_service
  echo "Installed ${SERVICE_NAME} ${TAG} to ${INSTALL_DIR}"
  echo "Edit ${CONFIG_DIR}/config.yaml before first start."
  echo "Start with: systemctl start ${SERVICE_NAME}"
}

main "$@"
```

- [ ] **Step 4: Include systemd assets in the backend bundle and docs**

Update `.goreleaser.yaml` backend bundle files:

```yaml
files:
  - deploy/README.md
  - deploy/config.example.yaml
  - deploy/.env.example
  - deploy/docker-compose.yml
  - deploy/docker-compose.external.yml
  - deploy/docker-deploy.sh
  - deploy/init-db.sql
  - deploy/install.sh
  - deploy/ai-efficiency.service
```

Append to `deploy/README.md`:

````md
## Linux Systemd Install

After the first tagged GitHub release, Linux hosts can install the binary release path with:

~~~bash
curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/deploy/install.sh | sudo bash
~~~

The installer:

- downloads the latest GitHub Release backend bundle
- verifies `checksums.txt`
- installs under `/opt/ai-efficiency`
- writes `ai-efficiency.service`
- enables the service

Before first start, edit:

- `/etc/ai-efficiency/config.yaml`
````

- [ ] **Step 5: Verify and commit**

Run:

```bash
cd /Users/admin/ai-efficiency
test -f deploy/install.sh
test -f deploy/ai-efficiency.service
bash -n deploy/install.sh
GOPROXY=https://goproxy.cn,direct go run github.com/goreleaser/goreleaser/v2@latest check --config .goreleaser.yaml
```

Expected: PASS

Commit:

```bash
git add deploy/install.sh deploy/ai-efficiency.service .goreleaser.yaml deploy/README.md
git commit -m "feat(deploy): add systemd install assets"
```

### Task 2: Add Systemd Release Asset Selection And Binary Update Core

**Files:**
- Create: `backend/internal/deployment/systemd_asset.go`
- Create: `backend/internal/deployment/systemd_asset_test.go`
- Create: `backend/internal/deployment/systemd_binary_updater.go`
- Create: `backend/internal/deployment/systemd_binary_updater_test.go`
- Test: `backend/internal/deployment/systemd_asset_test.go`
- Test: `backend/internal/deployment/systemd_binary_updater_test.go`

- [ ] **Step 1: Write the failing unit tests**

Create `backend/internal/deployment/systemd_asset_test.go`:

```go
package deployment

import "testing"

func TestSelectSystemdArchiveAsset(t *testing.T) {
	assets := []ReleaseAsset{
		{Name: "ai-efficiency-backend_1.2.3_linux_amd64.tar.gz", DownloadURL: "https://example.com/linux-amd64.tgz"},
		{Name: "checksums.txt", DownloadURL: "https://example.com/checksums.txt"},
	}

	archive, checksums, err := SelectSystemdReleaseAssets(assets, "linux", "amd64")
	if err != nil {
		t.Fatalf("SelectSystemdReleaseAssets: %v", err)
	}
	if archive.Name != "ai-efficiency-backend_1.2.3_linux_amd64.tar.gz" {
		t.Fatalf("archive = %+v", archive)
	}
	if checksums.Name != "checksums.txt" {
		t.Fatalf("checksums = %+v", checksums)
	}
}
```

Create `backend/internal/deployment/systemd_binary_updater_test.go`:

```go
package deployment

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSystemdBinaryUpdaterApplyAndRollback(t *testing.T) {
	root := t.TempDir()
	installDir := filepath.Join(root, "opt")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	current := filepath.Join(installDir, "ai-efficiency-server")
	if err := os.WriteFile(current, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile current: %v", err)
	}

	archive := filepath.Join(root, "bundle.tar.gz")
	writeTestArchive(t, archive, map[string]string{
		"ai-efficiency-server": "new-binary",
	})
	checksums := filepath.Join(root, "checksums.txt")
	writeChecksumFile(t, checksums, archive)

	updater := NewSystemdBinaryUpdater(SystemdBinaryConfig{
		InstallDir:  installDir,
		BinaryName:  "ai-efficiency-server",
		BackupName:  "ai-efficiency-server.backup",
		DownloadDir: filepath.Join(root, "tmp"),
	})

	result, err := updater.ApplyArchive(context.Background(), archive, checksums)
	if err != nil {
		t.Fatalf("ApplyArchive: %v", err)
	}
	if !result.NeedRestart {
		t.Fatalf("NeedRestart = false, want true")
	}
	data, _ := os.ReadFile(current)
	if string(data) != "new-binary" {
		t.Fatalf("current = %q, want new-binary", string(data))
	}

	if err := updater.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	data, _ = os.ReadFile(current)
	if string(data) != "old-binary" {
		t.Fatalf("current after rollback = %q, want old-binary", string(data))
	}
}
```

- [ ] **Step 2: Run the targeted tests to verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/deployment -run 'TestSelectSystemdArchiveAsset|TestSystemdBinaryUpdaterApplyAndRollback' -count=1
```

Expected: FAIL because the selectors/updater do not exist yet.

- [ ] **Step 3: Implement the asset selector and binary updater**

Create `backend/internal/deployment/systemd_asset.go`:

```go
package deployment

import (
	"fmt"
	"strings"
)

type ReleaseAsset struct {
	Name        string
	DownloadURL string
}

func SelectSystemdReleaseAssets(assets []ReleaseAsset, goos, goarch string) (ReleaseAsset, ReleaseAsset, error) {
	expectedArchive := fmt.Sprintf("ai-efficiency-backend_%s_%s", goos, goarch)
	var archive ReleaseAsset
	var checksums ReleaseAsset
	for _, asset := range assets {
		if strings.Contains(asset.Name, expectedArchive) && strings.HasSuffix(asset.Name, ".tar.gz") {
			archive = asset
		}
		if asset.Name == "checksums.txt" {
			checksums = asset
		}
	}
	if archive.Name == "" {
		return ReleaseAsset{}, ReleaseAsset{}, fmt.Errorf("no backend archive found for %s/%s", goos, goarch)
	}
	if checksums.Name == "" {
		return ReleaseAsset{}, ReleaseAsset{}, fmt.Errorf("checksums.txt not found")
	}
	return archive, checksums, nil
}
```

Create `backend/internal/deployment/systemd_binary_updater.go`:

```go
package deployment

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type SystemdBinaryConfig struct {
	InstallDir  string
	BinaryName  string
	BackupName  string
	DownloadDir string
}

type SystemdOperationResult struct {
	Message     string `json:"message"`
	NeedRestart bool   `json:"need_restart"`
}

type SystemdBinaryUpdater struct {
	cfg SystemdBinaryConfig
}

func NewSystemdBinaryUpdater(cfg SystemdBinaryConfig) *SystemdBinaryUpdater {
	return &SystemdBinaryUpdater{cfg: cfg}
}

func (u *SystemdBinaryUpdater) ApplyArchive(_ context.Context, archivePath, checksumsPath string) (SystemdOperationResult, error) {
	if err := verifyArchiveChecksum(archivePath, checksumsPath); err != nil {
		return SystemdOperationResult{}, err
	}
	extracted, err := extractServerBinary(archivePath, u.cfg.DownloadDir, u.cfg.BinaryName)
	if err != nil {
		return SystemdOperationResult{}, err
	}
	currentPath := filepath.Join(u.cfg.InstallDir, u.cfg.BinaryName)
	backupPath := filepath.Join(u.cfg.InstallDir, u.cfg.BackupName)
	_ = os.Remove(backupPath)
	if err := os.Rename(currentPath, backupPath); err != nil {
		return SystemdOperationResult{}, fmt.Errorf("backup current binary: %w", err)
	}
	if err := os.Rename(extracted, currentPath); err != nil {
		_ = os.Rename(backupPath, currentPath)
		return SystemdOperationResult{}, fmt.Errorf("replace binary: %w", err)
	}
	return SystemdOperationResult{Message: "update completed", NeedRestart: true}, nil
}

func (u *SystemdBinaryUpdater) Rollback(_ context.Context) error {
	currentPath := filepath.Join(u.cfg.InstallDir, u.cfg.BinaryName)
	backupPath := filepath.Join(u.cfg.InstallDir, u.cfg.BackupName)
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("backup not found: %w", err)
	}
	tempPath := currentPath + ".rollback"
	if err := os.Rename(currentPath, tempPath); err != nil {
		return fmt.Errorf("move current binary aside: %w", err)
	}
	if err := os.Rename(backupPath, currentPath); err != nil {
		_ = os.Rename(tempPath, currentPath)
		return fmt.Errorf("restore backup: %w", err)
	}
	_ = os.Remove(tempPath)
	return nil
}

func verifyArchiveChecksum(archivePath, checksumsPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(sum.Sum(nil))

	checksums, err := os.Open(checksumsPath)
	if err != nil {
		return err
	}
	defer checksums.Close()
	scanner := bufio.NewScanner(checksums)
	target := filepath.Base(archivePath)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[1] == target {
			if fields[0] != actual {
				return fmt.Errorf("checksum mismatch for %s", target)
			}
			return nil
		}
	}
	return fmt.Errorf("checksum for %s not found", target)
}

func extractServerBinary(archivePath, outDir, binaryName string) (string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gzr, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if filepath.Base(hdr.Name) != binaryName {
			continue
		}
		dst := filepath.Join(outDir, binaryName)
		out, err := os.Create(dst)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return "", err
		}
		if err := out.Close(); err != nil {
			return "", err
		}
		if err := os.Chmod(dst, 0o755); err != nil {
			return "", err
		}
		return dst, nil
	}
	return "", fmt.Errorf("%s not found in archive", binaryName)
}
```

- [ ] **Step 4: Run the targeted tests and commit**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/deployment -run 'TestSelectSystemdArchiveAsset|TestSystemdBinaryUpdaterApplyAndRollback' -count=1
```

Expected: PASS

Commit:

```bash
git add backend/internal/deployment/systemd_asset.go backend/internal/deployment/systemd_asset_test.go backend/internal/deployment/systemd_binary_updater.go backend/internal/deployment/systemd_binary_updater_test.go
git commit -m "feat(backend): add systemd binary update core"
```

### Task 3: Route Deployment APIs By Mode And Add Restart Support

**Files:**
- Create: `backend/internal/deployment/systemd_service_manager.go`
- Create: `backend/internal/deployment/systemd_service_manager_test.go`
- Modify: `backend/internal/deployment/service.go`
- Modify: `backend/internal/deployment/service_test.go`
- Modify: `backend/internal/handler/deployment.go`
- Modify: `backend/internal/handler/deployment_http_test.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/cmd/server/main.go`
- Test: `backend/internal/deployment/systemd_service_manager_test.go`
- Test: `backend/internal/deployment/service_test.go`
- Test: `backend/internal/handler/deployment_http_test.go`

- [ ] **Step 1: Write the failing restart and mode-routing tests**

Create `backend/internal/deployment/systemd_service_manager_test.go`:

```go
package deployment

import (
	"context"
	"testing"
)

type commandRunnerStub struct {
	args [][]string
	err  error
}

func (r *commandRunnerStub) Run(_ context.Context, name string, args ...string) error {
	r.args = append(r.args, append([]string{name}, args...))
	return r.err
}

func TestSystemdServiceManagerRestart(t *testing.T) {
	runner := &commandRunnerStub{}
	manager := NewSystemdServiceManager(SystemdServiceConfig{
		ServiceName: "ai-efficiency",
	}, runner)

	result, err := manager.Restart(context.Background())
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if !result.NeedRestart {
		t.Fatalf("NeedRestart = false, want true")
	}
	if len(runner.args) != 1 || runner.args[0][0] != "systemctl" || runner.args[0][1] != "restart" || runner.args[0][2] != "ai-efficiency" {
		t.Fatalf("args = %#v", runner.args)
	}
}
```

Extend `backend/internal/deployment/service_test.go`:

```go
func TestDeploymentServiceStatusInSystemdModeUsesReleaseSourceAndNoUpdater(t *testing.T) {
	svc := NewService(
		config.DeploymentConfig{
			Mode: "systemd",
			Update: config.UpdateConfig{
				Enabled: true,
			},
		},
		VersionInfo{Version: "v0.4.0"},
		releaseStub{info: ReleaseInfo{Version: "v0.5.0", URL: "https://example.com/release/v0.5.0"}},
		nil,
	)

	status, err := svc.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if !status.UpdateAvailable {
		t.Fatalf("expected update available, got %+v", status)
	}
}
```

Extend `backend/internal/handler/deployment_http_test.go` with restart coverage:

```go
func TestDeploymentRestartReturnsConflictWhenUnsupported(t *testing.T) {
	env := setupFullTestEnvWithDeployment(t, NewDeploymentHandler(
		deployment.NewHealthService(
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.FuncPinger(func(context.Context) error { return nil }),
			deployment.CurrentVersion(),
		),
		stubDeploymentStatusReader{restartErr: deployment.ErrApplyDisabled},
	))

	w := doFullRequest(env, http.MethodPost, "/api/v1/settings/deployment/restart", nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run the targeted tests to confirm they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/deployment ./internal/handler -run 'TestSystemdServiceManagerRestart|TestDeploymentServiceStatusInSystemdModeUsesReleaseSourceAndNoUpdater|TestDeploymentRestartReturnsConflictWhenUnsupported' -count=1
```

Expected: FAIL because restart and systemd mode routing do not exist yet.

- [ ] **Step 3: Implement service manager, mode routing, and restart endpoint**

Create `backend/internal/deployment/systemd_service_manager.go`:

```go
package deployment

import (
	"context"
	"fmt"
)

type SystemdServiceConfig struct {
	ServiceName string
}

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
}

type SystemdServiceManager struct {
	cfg    SystemdServiceConfig
	runner CommandRunner
}

func NewSystemdServiceManager(cfg SystemdServiceConfig, runner CommandRunner) *SystemdServiceManager {
	return &SystemdServiceManager{cfg: cfg, runner: runner}
}

func (m *SystemdServiceManager) Restart(ctx context.Context) (SystemdOperationResult, error) {
	if err := m.runner.Run(ctx, "systemctl", "restart", m.cfg.ServiceName); err != nil {
		return SystemdOperationResult{}, fmt.Errorf("restart systemd service: %w", err)
	}
	return SystemdOperationResult{Message: "restart initiated", NeedRestart: true}, nil
}
```

Evolve `backend/internal/deployment/service.go`:

```go
type SystemdUpdater interface {
	ApplyArchive(context.Context, string, string) (SystemdOperationResult, error)
	Rollback(context.Context) error
}

type RestartManager interface {
	Restart(context.Context) (SystemdOperationResult, error)
}
```

And extend `Service` with:

```go
	systemdUpdater   SystemdUpdater
	systemdRestarter RestartManager
```

Plus constructor args / methods:

```go
func (s *Service) Restart(ctx context.Context) (UpdateStatus, error) {
	if s.cfg.Mode != "systemd" {
		return UpdateStatus{}, ErrApplyDisabled
	}
	if s.systemdRestarter == nil {
		return UpdateStatus{}, fmt.Errorf("systemd restart is not configured")
	}
	result, err := s.systemdRestarter.Restart(ctx)
	if err != nil {
		return UpdateStatus{}, err
	}
	return UpdateStatus{
		Phase:   "restart_requested",
		Message: result.Message,
	}, nil
}
```

Mode routing in `CheckForUpdate`, `ApplyUpdate`, `RollbackUpdate`:

- `compose` mode keeps existing updater path
- `systemd` mode:
  - `CheckForUpdate` still uses release source
  - `ApplyUpdate` resolves the latest release assets, downloads/verifies/applies via `systemdUpdater`
  - `RollbackUpdate` uses systemd updater rollback

Add handler restart method in `backend/internal/handler/deployment.go`:

```go
func (h *DeploymentHandler) Restart(c *gin.Context) {
	resp, err := h.status.Restart(c.Request.Context())
	if err != nil {
		if deployment.IsPolicyError(err) {
			c.JSON(http.StatusConflict, gin.H{"code": 409, "message": err.Error()})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": resp})
}
```

Register route in `backend/internal/handler/router.go`:

```go
settingsGroup.POST("/deployment/restart", deploymentHandler.Restart)
```

Wire in `backend/cmd/server/main.go`:

```go
systemdManager := deployment.NewSystemdServiceManager(
	deployment.SystemdServiceConfig{ServiceName: "ai-efficiency"},
	deployment.NewExecCommandRunner(),
)
deploymentService := deployment.NewService(
	cfg.Deployment,
	versionInfo,
	releaseSource,
	updaterClient,
	nil, // systemdUpdater wired in next task
	systemdManager,
)
```

- [ ] **Step 4: Run targeted tests and commit**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/deployment ./internal/handler -run 'TestSystemdServiceManagerRestart|TestDeploymentServiceStatusInSystemdModeUsesReleaseSourceAndNoUpdater|TestDeploymentRestartReturnsConflictWhenUnsupported' -count=1
```

Expected: PASS

Commit:

```bash
git add backend/internal/deployment/systemd_service_manager.go backend/internal/deployment/systemd_service_manager_test.go backend/internal/deployment/service.go backend/internal/deployment/service_test.go backend/internal/handler/deployment.go backend/internal/handler/deployment_http_test.go backend/internal/handler/router.go backend/cmd/server/main.go
git commit -m "feat(backend): route deployment actions by mode"
```

### Task 4: Integrate Systemd Binary Self-Update Into Backend

**Files:**
- Modify: `backend/internal/deployment/release_source.go`
- Modify: `backend/internal/deployment/release_source_test.go`
- Modify: `backend/internal/deployment/service.go`
- Modify: `backend/internal/deployment/service_test.go`
- Modify: `backend/cmd/server/main.go`
- Test: `backend/internal/deployment/release_source_test.go`
- Test: `backend/internal/deployment/service_test.go`

- [ ] **Step 1: Write the failing integration tests for systemd apply/rollback**

Extend `backend/internal/deployment/release_source_test.go`:

```go
func TestGitHubReleaseSourceLatestIncludesAssets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.5.0","html_url":"https://example.com/release/v0.5.0","assets":[{"name":"ai-efficiency-backend_0.5.0_linux_amd64.tar.gz","browser_download_url":"https://example.com/archive.tgz"},{"name":"checksums.txt","browser_download_url":"https://example.com/checksums.txt"}]}`))
	}))
	defer srv.Close()

	source := NewGitHubReleaseSource(srv.Client(), srv.URL)
	info, err := source.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if len(info.Assets) != 2 {
		t.Fatalf("assets = %+v", info.Assets)
	}
}
```

Extend `backend/internal/deployment/service_test.go` with a systemd updater stub:

```go
type systemdUpdaterStub struct {
	appliedArchive   string
	appliedChecksums string
	rollbackCalled   bool
	applyErr         error
	rollbackErr      error
}

func (s *systemdUpdaterStub) ApplyArchive(_ context.Context, archivePath, checksumsPath string) (SystemdOperationResult, error) {
	s.appliedArchive = archivePath
	s.appliedChecksums = checksumsPath
	if s.applyErr != nil {
		return SystemdOperationResult{}, s.applyErr
	}
	return SystemdOperationResult{Message: "update completed", NeedRestart: true}, nil
}

func (s *systemdUpdaterStub) Rollback(context.Context) error {
	s.rollbackCalled = true
	return s.rollbackErr
}
```

And the test:

```go
func TestDeploymentServiceApplyAndRollbackInSystemdMode(t *testing.T) {
	source := releaseStub{
		info: ReleaseInfo{
			Version: "v0.5.0",
			URL:     "https://example.com/release/v0.5.0",
			Assets: []ReleaseAsset{
				{Name: "ai-efficiency-backend_0.5.0_linux_amd64.tar.gz", DownloadURL: "https://example.com/archive.tgz"},
				{Name: "checksums.txt", DownloadURL: "https://example.com/checksums.txt"},
			},
		},
	}
	systemdUpdater := &systemdUpdaterStub{}
	svc := NewService(
		config.DeploymentConfig{
			Mode: "systemd",
			Update: config.UpdateConfig{
				Enabled:      true,
				ApplyEnabled: true,
			},
		},
		VersionInfo{Version: "v0.4.0"},
		source,
		nil,
		systemdUpdater,
		nil,
	)

	result, err := svc.ApplyUpdate(context.Background(), ApplyRequest{TargetVersion: "v0.5.0"})
	if err != nil {
		t.Fatalf("ApplyUpdate: %v", err)
	}
	if result.Message != "update completed" {
		t.Fatalf("result = %+v", result)
	}
	if _, err := svc.RollbackUpdate(context.Background()); err != nil {
		t.Fatalf("RollbackUpdate: %v", err)
	}
	if !systemdUpdater.rollbackCalled {
		t.Fatalf("rollbackCalled = false")
	}
}
```

- [ ] **Step 2: Run the targeted tests to confirm they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/deployment -run 'TestGitHubReleaseSourceLatestIncludesAssets|TestDeploymentServiceApplyAndRollbackInSystemdMode' -count=1
```

Expected: FAIL because release assets and systemd mode apply/rollback are not wired yet.

- [ ] **Step 3: Implement systemd mode release handling**

Update `backend/internal/deployment/release_source.go`:

```go
type ReleaseInfo struct {
	Version string         `json:"version"`
	URL     string         `json:"url"`
	Assets  []ReleaseAsset `json:"assets,omitempty"`
}
```

And decode assets from GitHub response:

```go
var payload struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}
```

Then in `Latest(...)`:

```go
assets := make([]ReleaseAsset, 0, len(payload.Assets))
for _, asset := range payload.Assets {
	assets = append(assets, ReleaseAsset{
		Name:        asset.Name,
		DownloadURL: asset.BrowserDownloadURL,
	})
}
return ReleaseInfo{
	Version: payload.TagName,
	URL:     payload.HTMLURL,
	Assets:  assets,
}, nil
```

Update `backend/internal/deployment/service.go` systemd apply path:

```go
if s.cfg.Mode == "systemd" {
	if !s.cfg.Update.Enabled {
		return UpdateStatus{}, ErrUpdatesDisabled
	}
	if !s.cfg.Update.ApplyEnabled {
		return UpdateStatus{}, ErrApplyDisabled
	}
	if s.source == nil || s.systemdUpdater == nil {
		return UpdateStatus{}, fmt.Errorf("systemd updater is not configured")
	}
	release, err := s.source.Latest(ctx)
	if err != nil {
		return UpdateStatus{}, fmt.Errorf("fetch latest release: %w", err)
	}
	archive, checksums, err := SelectSystemdReleaseAssets(release.Assets, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return UpdateStatus{}, err
	}
	result, err := s.systemdUpdater.ApplyArchive(ctx, archive.DownloadURL, checksums.DownloadURL)
	if err != nil {
		return UpdateStatus{}, err
	}
	return UpdateStatus{
		Phase:   "updated",
		Message: result.Message,
	}, nil
}
```

And rollback path:

```go
if s.cfg.Mode == "systemd" {
	if err := s.systemdUpdater.Rollback(ctx); err != nil {
		return UpdateStatus{}, err
	}
	return UpdateStatus{
		Phase:   "rolled_back",
		Message: "rollback completed",
	}, nil
}
```

Wire `systemdUpdater` in `backend/cmd/server/main.go`:

```go
systemdUpdater := deployment.NewSystemdBinaryUpdater(deployment.SystemdBinaryConfig{
	InstallDir:  "/opt/ai-efficiency",
	BinaryName:  "ai-efficiency-server",
	BackupName:  "ai-efficiency-server.backup",
	DownloadDir: filepath.Join(os.TempDir(), "ai-efficiency-update"),
})
deploymentService := deployment.NewService(
	cfg.Deployment,
	versionInfo,
	releaseSource,
	updaterClient,
	systemdUpdater,
	systemdManager,
)
```

- [ ] **Step 4: Run targeted tests and commit**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./internal/deployment -run 'TestGitHubReleaseSourceLatestIncludesAssets|TestDeploymentServiceApplyAndRollbackInSystemdMode' -count=1
```

Expected: PASS

Commit:

```bash
git add backend/internal/deployment/release_source.go backend/internal/deployment/release_source_test.go backend/internal/deployment/service.go backend/internal/deployment/service_test.go backend/cmd/server/main.go
git commit -m "feat(backend): add systemd self-update mode"
```

### Task 5: Expose Restart In Frontend And Finalize Docs

**Files:**
- Modify: `frontend/src/api/deployment.ts`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/views/SettingsView.vue`
- Modify: `frontend/src/__tests__/api-modules.test.ts`
- Modify: `frontend/src/__tests__/settings-view.test.ts`
- Modify: `deploy/README.md`
- Modify: `deploy/config.example.yaml`
- Modify: `docs/architecture.md`
- Verify: full backend/frontend/deploy checks

- [ ] **Step 1: Write the failing frontend tests**

Append to `frontend/src/__tests__/api-modules.test.ts`:

```ts
import { restartDeployment } from '@/api/deployment'

it('calls deployment restart endpoint', async () => {
  mockClient.post.mockResolvedValue({ data: { data: { phase: 'restart_requested' } } })
  await restartDeployment()
  expect(mockClient.post).toHaveBeenCalledWith('/settings/deployment/restart')
})
```

Append to `frontend/src/__tests__/settings-view.test.ts`:

```ts
vi.mock('@/api/deployment', async () => {
  const actual = await vi.importActual<typeof import('@/api/deployment')>('@/api/deployment')
  return {
    ...actual,
    restartDeployment: vi.fn(),
  }
})

it('renders restart control in deployment section', async () => {
  const wrapper = await mountSettings()
  expect(wrapper.text()).toContain('Restart Service')
})
```

- [ ] **Step 2: Run the frontend tests to confirm they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/frontend
pnpm test -- api-modules settings-view
```

Expected: FAIL because restart API and button do not exist.

- [ ] **Step 3: Implement frontend restart control and docs**

Update `frontend/src/api/deployment.ts`:

```ts
export function restartDeployment() {
  return client.post<ApiResponse<UpdateStatus>>('/settings/deployment/restart')
}
```

Update `frontend/src/views/SettingsView.vue`:

```ts
import { getDeploymentStatus, checkForUpdate, applyUpdate, rollbackUpdate, restartDeployment } from '@/api/deployment'
```

Add handler:

```ts
async function handleRestartDeployment() {
  deploymentActionLoading.value = true
  deploymentMessage.value = ''
  deploymentMessageKind.value = ''
  try {
    const res = await restartDeployment()
    applyDeploymentUpdateStatus(res.data.data ?? { phase: 'restart_requested' })
    setDeploymentMessage('success', 'Restart request submitted')
  } catch (e: any) {
    setDeploymentMessage('error', e.response?.data?.message || 'Failed to restart service')
  } finally {
    deploymentActionLoading.value = false
  }
}
```

Add button to deployment section:

```vue
<button @click="handleRestartDeployment" :disabled="deploymentActionLoading" class="rounded-md bg-slate-700 px-4 py-2 text-sm font-medium text-white hover:bg-slate-800 disabled:opacity-50">
  Restart Service
</button>
```

Update `deploy/README.md` with systemd section if not already present, plus:

```md
## Binary / Systemd Mode

This route is Linux-only and is separate from Docker Compose mode.

- install path: `/opt/ai-efficiency`
- config path: `/etc/ai-efficiency/config.yaml`
- service name: `ai-efficiency`
- update path: binary self-update with `.backup` rollback
```

Update `deploy/config.example.yaml`:

```yaml
deployment:
  mode: "systemd" # or bundled/external compose mode where applicable
```

Update `docs/architecture.md` deployment section to explicitly mention:

- Compose mode -> updater sidecar
- systemd mode -> backend binary self-update

- [ ] **Step 4: Run final verification**

Run:

```bash
cd /Users/admin/ai-efficiency/backend
go test ./...

cd /Users/admin/ai-efficiency/frontend
pnpm test
pnpm build

cd /Users/admin/ai-efficiency
bash -n deploy/install.sh
go run github.com/goreleaser/goreleaser/v2@latest check --config .goreleaser.yaml
docker-compose --env-file deploy/.env.example -f deploy/docker-compose.yml config >/dev/null
docker-compose --env-file deploy/.env.example -f deploy/docker-compose.external.yml config >/dev/null
```

Expected: all commands succeed.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/api/deployment.ts frontend/src/types/index.ts frontend/src/views/SettingsView.vue frontend/src/__tests__/api-modules.test.ts frontend/src/__tests__/settings-view.test.ts deploy/README.md deploy/config.example.yaml docs/architecture.md
git commit -m "feat(frontend): add systemd deployment controls"
```

## Self-Review Checklist

- Spec coverage:
  - install.sh + service file: Task 1
  - release asset selection + checksum + atomic replace + backup rollback: Task 2
  - deployment mode split and restart routing: Tasks 3 and 4
  - frontend reuse with restart support: Task 5
  - docs / architecture alignment: Tasks 1 and 5
- Placeholder scan:
  - No `TODO`, `TBD`, or “implement later” markers remain.
- Type consistency:
  - `ReleaseAsset`, `SystemdOperationResult`, `SystemdBinaryConfig`, `SystemdServiceConfig`, and `RequireExplicitDBDSN` naming is consistent across backend tasks.
