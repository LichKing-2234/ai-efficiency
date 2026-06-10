# Independent CLI Release Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split `ae-cli` publishing from the platform release so CLI-only changes do not create backend images, backend bundles, or Helm rollouts.

**Architecture:** Keep one repository and two release units. Platform releases continue to use `v*` tags and own repository-level `/releases/latest`; CLI releases use `ae-cli/v*` tags, publish only CLI artifacts, and explicitly stay out of repository-level latest. CLI installers and `ae-cli update` discover the newest CLI release by filtering releases for the `ae-cli/v*` tag namespace instead of reading the platform latest endpoint.

**Tech Stack:** GitHub Actions, GoReleaser v2, Go 1.24+, Bash, PowerShell, GitHub Releases API.

**Status:** Plan written; implementation not started.

**Source Spec:** [`docs/superpowers/specs/2026-06-10-independent-cli-release-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-06-10-independent-cli-release-design.md)

---

## File Map

### Create

- `.goreleaser.ae-cli.yaml`
  CLI-only GoReleaser config. Builds only `ae-cli`, writes the stripped CLI version into `buildinfo.Version`, publishes artifacts to the `ae-cli/v*` GitHub Release, and sets `release.make_latest: false`.

- `.github/workflows/ae-cli-release.yml`
  CLI-only release workflow. Triggered by `ae-cli/v*` tags or manual dispatch with a matching tag. Runs CLI tests, publishes CLI artifacts, and verifies repository latest still points to a platform release.

### Modify

- `.goreleaser.yaml`
  Platform GoReleaser config. Remove the `ae-cli` build and archive so platform releases publish only backend server/updater bundles.

- `.github/workflows/release.yml`
  Platform release workflow. Remove the release-time `ae-cli-test` job from the platform release gate so platform publishing is not blocked by CLI-only test surface.

- `ae-cli/internal/update/update.go`
  Change CLI update discovery from repository `/releases/latest` to release-list filtering for `ae-cli/v*`. Keep semver comparison against stripped `vX.Y.Z` versions while retaining the full release tag for installer invocation.

- `ae-cli/internal/update/update_test.go`
  Add release-list tests proving platform releases are ignored, the newest CLI release is selected, and the installer receives the full `ae-cli/v*` release tag.

- `ae-cli/install.sh`
  Change default release discovery from repository latest to release-list filtering for `ae-cli/v*`. Accept both `ae-cli/vX.Y.Z` and legacy `vX.Y.Z` pinned arguments, but resolve latest from CLI releases only.

- `ae-cli/install.ps1`
  Match the Bash installer: filter release list for `ae-cli/v*`, accept pinned `ae-cli/vX.Y.Z` or `vX.Y.Z`, and derive archive names from the stripped version.

- `ae-cli/test/install-test.sh`
  Update installer fixtures to include both platform and CLI releases, then assert the installer chooses the CLI release and still supports a pinned full CLI tag.

- `ae-cli/README.md`
  Document the independent CLI tag namespace and clarify that CLI update/install reads CLI releases, not platform latest.

- `deploy/README.md`
  Remove `ae-cli_*` from the platform release artifact list and point CLI installation docs back to `ae-cli/README.md`.

- `AGENTS.md`
  Add release rules for `v*` platform tags and `ae-cli/v*` CLI-only tags.

- `CLAUDE.md`
  Add a quick-reference release note so future agents do not treat CLI-only releases as platform releases.

- `docs/architecture.md`
  Add the implemented release boundary: platform image/bundle and CLI artifacts are separate release units in one repo.

- `docs/superpowers/specs/2026-06-10-independent-cli-release-design.md`
  Update status after implementation lands.

### Tests And Validation Commands

- `cd ae-cli && go test ./...`
- `bash ae-cli/test/install-test.sh`
- `bash -n ae-cli/install.sh`
- `pwsh -NoProfile -Command '$ErrorActionPreference = "Stop"; [scriptblock]::Create((Get-Content -Raw ae-cli/install.ps1)) | Out-Null'`
- `goreleaser check --config .goreleaser.yaml`
- `AE_CLI_VERSION=v0.2.0-preview.1 AE_CLI_VERSION_NO_V=0.2.0-preview.1 goreleaser release --snapshot --clean --config .goreleaser.ae-cli.yaml`

---

### Task 1: Add CLI Release Discovery To The Go Update Package

**Files:**
- Modify: `ae-cli/internal/update/update.go`
- Modify: `ae-cli/internal/update/update_test.go`
- Test: `ae-cli/internal/update/update_test.go`

- [x] **Step 1: Add failing tests for independent CLI discovery**

Append these tests to `ae-cli/internal/update/update_test.go`:

```go
func TestCheckForUpdateSelectsLatestIndependentCLIRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"tag_name":"v0.1.0-preview.42","html_url":"https://example.com/releases/v0.1.0-preview.42"},
			{"tag_name":"ae-cli/v0.2.0","html_url":"https://example.com/releases/ae-cli/v0.2.0"},
			{"tag_name":"ae-cli/v0.1.9","html_url":"https://example.com/releases/ae-cli/v0.1.9"}
		]`))
	}))
	defer srv.Close()

	result, err := CheckForUpdate(context.Background(), CheckOptions{
		CurrentVersion: "v0.1.0",
		ReleaseAPIURL:  srv.URL,
	})
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if !result.UpdateAvailable {
		t.Fatal("expected update to be available")
	}
	if result.LatestVersion != "v0.2.0" {
		t.Fatalf("latest version = %q, want v0.2.0", result.LatestVersion)
	}
	if result.LatestTag != "ae-cli/v0.2.0" {
		t.Fatalf("latest tag = %q, want ae-cli/v0.2.0", result.LatestTag)
	}
	if result.ReleaseURL != "https://example.com/releases/ae-cli/v0.2.0" {
		t.Fatalf("release URL = %q", result.ReleaseURL)
	}
}

func TestCheckForUpdateRejectsReleaseListWithoutCLIRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"tag_name":"v0.1.0-preview.42","html_url":"https://example.com/releases/v0.1.0-preview.42"}
		]`))
	}))
	defer srv.Close()

	_, err := CheckForUpdate(context.Background(), CheckOptions{
		CurrentVersion: "v0.1.0",
		ReleaseAPIURL:  srv.URL,
	})
	if err == nil {
		t.Fatal("expected missing CLI release to fail")
	}
	if !strings.Contains(err.Error(), "no ae-cli release found") {
		t.Fatalf("error = %q, want no ae-cli release found", err)
	}
}
```

- [x] **Step 2: Update the existing update tests to use release-list responses**

In `TestCheckForUpdateReportsAvailableRelease`, replace the response body with:

```go
_, _ = w.Write([]byte(`[{"tag_name":"ae-cli/v0.2.0","html_url":"https://example.com/releases/ae-cli/v0.2.0"}]`))
```

Change the final assertion in that test to:

```go
if result.LatestTag != "ae-cli/v0.2.0" {
	t.Fatalf("latest tag = %q, want ae-cli/v0.2.0", result.LatestTag)
}
if result.LatestVersion != "v0.2.0" {
	t.Fatalf("latest version = %q, want v0.2.0", result.LatestVersion)
}
```

In `TestInstallLatestDoesNotRequireExecutablePathWhenAlreadyUpToDate`, replace the response body with:

```go
_, _ = w.Write([]byte(`[{"tag_name":"ae-cli/v0.2.0"}]`))
```

In `TestInstallLatestRunsInstallerScriptForManagedUnixInstall`, replace the `/latest` response body with:

```go
_, _ = w.Write([]byte(`[{"tag_name":"ae-cli/v0.2.0"}]`))
```

In `TestInstallLatestRejectsUnofficialInstallPath`, replace the response body with:

```go
_, _ = w.Write([]byte(`[{"tag_name":"ae-cli/v0.2.0"}]`))
```

In `TestInstallLatestRunsInstallerScriptForManagedUnixInstall`, update the expected installed version and installer argument:

```go
if result.InstalledVersion != "v0.2.0" {
	t.Fatalf("installed version = %q, want v0.2.0", result.InstalledVersion)
}
if string(data) != "ae-cli/v0.2.0" {
	t.Fatalf("installer arg = %q, want ae-cli/v0.2.0", string(data))
}
if !strings.Contains(out.String(), "installed ae-cli/v0.2.0") {
	t.Fatalf("output = %q, want installer output", out.String())
}
```

- [x] **Step 3: Run the focused failing tests**

Run:

```bash
cd ae-cli && go test ./internal/update
```

Expected: FAIL. The failures should show that `fetchLatestRelease` cannot decode an array response or still normalizes `ae-cli/v0.2.0` as an unsupported version.

- [x] **Step 4: Implement CLI release-list discovery**

In `ae-cli/internal/update/update.go`, replace the default release API URL and release response type with:

```go
const (
	cliReleaseTagPrefix         = "ae-cli/"
	defaultReleaseAPIURL        = "https://api.github.com/repos/LichKing-2234/ai-efficiency/releases?per_page=100"
	defaultInstallScriptURL     = "https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/%s"
	updateRequestTimeout        = 10 * time.Second
)

type releaseResponse struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}
```

Update `CheckForUpdate` to compare stripped versions and retain the full release tag:

```go
	latest, err := fetchLatestRelease(ctx, opts.ReleaseAPIURL)
	if err != nil {
		return CheckResult{}, err
	}
	latestReleaseTag, latestVersionTag, err := normalizeCLIReleaseTag(latest.TagName)
	if err != nil {
		return CheckResult{}, fmt.Errorf("normalize latest tag: %w", err)
	}

	comparison, err := compareVersions(latestVersionTag, currentTag)
```

Update the result fields:

```go
	return CheckResult{
		CurrentVersion:  currentTag,
		LatestVersion:   latestVersionTag,
		LatestTag:       latestReleaseTag,
		ReleaseURL:      latest.HTMLURL,
		UpdateAvailable: updateAvailable,
		Status:          status,
	}, nil
```

Replace `fetchLatestRelease` with:

```go
func fetchLatestRelease(ctx context.Context, overrideURL string) (*releaseResponse, error) {
	url := strings.TrimSpace(overrideURL)
	if url == "" {
		url = defaultReleaseAPIURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build latest release request: %w", err)
	}
	resp, err := httpDo(req)
	if err != nil {
		return nil, fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch latest release: unexpected status %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read latest release response: %w", err)
	}

	var releases []releaseResponse
	if err := json.Unmarshal(data, &releases); err == nil {
		for _, release := range releases {
			if _, _, tagErr := normalizeCLIReleaseTag(release.TagName); tagErr == nil {
				return &release, nil
			}
		}
		return nil, fmt.Errorf("no ae-cli release found")
	}

	var single releaseResponse
	if err := json.Unmarshal(data, &single); err != nil {
		return nil, fmt.Errorf("decode latest release: %w", err)
	}
	if _, _, err := normalizeCLIReleaseTag(single.TagName); err != nil {
		return nil, fmt.Errorf("no ae-cli release found")
	}
	return &single, nil
}
```

Add this helper below `fetchLatestRelease`:

```go
func normalizeCLIReleaseTag(value string) (releaseTag string, versionTag string, err error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", "", fmt.Errorf("version must not be empty")
	}
	versionPart := trimmed
	if strings.HasPrefix(versionPart, cliReleaseTagPrefix) {
		versionPart = strings.TrimPrefix(versionPart, cliReleaseTagPrefix)
	} else if strings.HasPrefix(versionPart, "v") {
		return "", "", fmt.Errorf("release tag %q is not an ae-cli release", trimmed)
	}
	versionTag, err = normalizeTag(versionPart)
	if err != nil {
		return "", "", err
	}
	return cliReleaseTagPrefix + versionTag, versionTag, nil
}
```

Replace `defaultInstallScriptURLFormat` and `installScriptURLForTag` with:

```go
func installScriptURLForTag(tag string) string {
	scriptName := "install.sh"
	if runtime.GOOS == "windows" {
		scriptName = "install.ps1"
	}
	return fmt.Sprintf(defaultInstallScriptURL, scriptName)
}
```

- [x] **Step 5: Run update package tests**

Run:

```bash
cd ae-cli && go test ./internal/update
```

Expected: PASS.

- [x] **Step 6: Commit the Go update discovery change**

Run:

```bash
git add ae-cli/internal/update/update.go ae-cli/internal/update/update_test.go
git commit -m "fix(ae-cli): discover independent cli releases"
```

Expected: commit succeeds.

---

### Task 2: Update Bash Installer For Independent CLI Releases

**Files:**
- Modify: `ae-cli/install.sh`
- Modify: `ae-cli/test/install-test.sh`
- Test: `ae-cli/test/install-test.sh`

- [x] **Step 1: Update installer fixtures to model platform and CLI releases**

In `ae-cli/test/install-test.sh`, replace the tag constants:

```bash
LATEST_TAG="ae-cli/v0.2.0-preview.1"
PLATFORM_LATEST_TAG="v0.1.0-preview.42"
PINNED_TAG="ae-cli/v0.2.1-preview.1"
LEGACY_PINNED_TAG="v0.2.1-preview.1"
BAD_CHECKSUM_TAG="ae-cli/v0.2.2-bad"
MISSING_BINARY_TAG="ae-cli/v0.2.3-missing-binary"
PATH_WARNING_TAG="ae-cli/v0.2.4-path-warning"
SYMLINK_TAG="ae-cli/v0.2.5-symlink"
```

Add this helper after the constants:

```bash
release_version_from_tag() {
  local tag="$1"
  tag="${tag#ae-cli/}"
  tag="${tag#v}"
  printf '%s' "$tag"
}
```

In `make_cli_archive`, `make_bad_checksum_archive`, `make_missing_binary_archive`, and `make_symlink_archive`, replace:

```bash
local version="${tag#v}"
```

with:

```bash
local version
version="$(release_version_from_tag "$tag")"
```

Replace the latest JSON fixture:

```bash
printf '[{"tag_name":"%s"},{"tag_name":"%s"}]\n' "$PLATFORM_LATEST_TAG" "$LATEST_TAG" >"$TMP_ROOT/latest.json"
```

Add a legacy pinned archive after `make_cli_archive "$PINNED_TAG"`:

```bash
make_cli_archive "$LEGACY_PINNED_TAG"
```

- [x] **Step 2: Add installer assertions for CLI release selection and legacy pinned compatibility**

After the existing pinned install block, add:

```bash
LEGACY_PINNED_HOME="$TMP_ROOT/home-legacy-pinned"
mkdir -p "$LEGACY_PINNED_HOME"
LEGACY_PINNED_LOG="$TMP_ROOT/legacy-pinned.log"
run_installer \
  "$LEGACY_PINNED_HOME" \
  "$LEGACY_PINNED_HOME/.local/bin:/usr/bin:/bin" \
  "file://$TMP_ROOT/latest.json" \
  "$LEGACY_PINNED_TAG" \
  >"$LEGACY_PINNED_LOG" 2>&1

test -x "$LEGACY_PINNED_HOME/.local/bin/ae-cli"
"$LEGACY_PINNED_HOME/.local/bin/ae-cli" | grep -q "ae-cli ${LEGACY_PINNED_TAG}"
grep -q "Installed ae-cli ${LEGACY_PINNED_TAG} to $LEGACY_PINNED_HOME/.local/bin/ae-cli" "$LEGACY_PINNED_LOG"
```

In the latest install assertions, keep:

```bash
"$LATEST_HOME/.local/bin/ae-cli" | grep -q "ae-cli ${LATEST_TAG}"
grep -q "Installing ae-cli ${LATEST_TAG}" "$LATEST_LOG"
```

This proves the installer ignored `v0.1.0-preview.42` and selected `ae-cli/v0.2.0-preview.1`.

- [x] **Step 3: Run the failing installer test**

Run:

```bash
bash ae-cli/test/install-test.sh
```

Expected: FAIL. The installer should fail to locate archives for `ae-cli/v*` because `download_release` still derives versions with `${tag#v}`.

- [x] **Step 4: Implement CLI release filtering in `install.sh`**

In `ae-cli/install.sh`, add:

```bash
CLI_RELEASE_TAG_PREFIX="ae-cli/"
```

Replace the default release API URL:

```bash
RELEASE_API_URL="${AE_CLI_INSTALL_RELEASE_API_URL:-https://api.github.com/repos/${GITHUB_REPO}/releases?per_page=100}"
```

Add this helper before `latest_tag`:

```bash
release_version_from_tag() {
  local tag="$1"
  tag="${tag#${CLI_RELEASE_TAG_PREFIX}}"
  tag="${tag#v}"
  printf '%s' "$tag"
}
```

Replace `latest_tag` with:

```bash
latest_tag() {
  local tag=""
  local release_json=""

  if ! release_json="$(curl -fsSL "$RELEASE_API_URL")"; then
    github_release_proxy_help
    exit 1
  fi

  tag="$(printf '%s\n' "$release_json" | awk -F'"' '
    /"tag_name"[[:space:]]*:/ {
      for (i = 1; i <= NF; i++) {
        if ($i == "tag_name") {
          candidate = $(i + 2)
          if (candidate ~ /^ae-cli\/v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$/) {
            print candidate
            exit
          }
        }
      }
    }
  ')"
  if [[ -z "$tag" ]]; then
    echo "failed to resolve ae-cli release tag" >&2
    exit 1
  fi

  printf '%s\n' "$tag"
}
```

In `download_release`, replace:

```bash
local version="${tag#v}"
```

with:

```bash
local version
version="$(release_version_from_tag "$tag")"
```

- [x] **Step 5: Run Bash syntax and installer tests**

Run:

```bash
bash -n ae-cli/install.sh
bash ae-cli/test/install-test.sh
```

Expected: both commands PASS.

- [x] **Step 6: Commit the Bash installer change**

Run:

```bash
git add ae-cli/install.sh ae-cli/test/install-test.sh
git commit -m "fix(ae-cli): install from cli release tags"
```

Expected: commit succeeds.

---

### Task 3: Update PowerShell Installer For Independent CLI Releases

**Files:**
- Modify: `ae-cli/install.ps1`
- Test: `ae-cli/install.ps1`

- [ ] **Step 1: Change the PowerShell release API default**

In `ae-cli/install.ps1`, replace:

```powershell
$ReleaseApiUrl = if ($env:AE_CLI_INSTALL_RELEASE_API_URL) { $env:AE_CLI_INSTALL_RELEASE_API_URL } else { "https://api.github.com/repos/$Repo/releases/latest" }
```

with:

```powershell
$CliReleaseTagPattern = "^ae-cli/v\d+\.\d+\.\d+([-.][0-9A-Za-z.-]+)?$"
$ReleaseApiUrl = if ($env:AE_CLI_INSTALL_RELEASE_API_URL) { $env:AE_CLI_INSTALL_RELEASE_API_URL } else { "https://api.github.com/repos/$Repo/releases?per_page=100" }
```

- [ ] **Step 2: Add a PowerShell release-version helper**

Add this function before `Get-LatestTag`:

```powershell
function Get-ReleaseVersion([string]$Tag) {
  $value = $Tag.Trim()
  if ($value.StartsWith("ae-cli/")) {
    $value = $value.Substring("ae-cli/".Length)
  }
  if ($value.StartsWith("v")) {
    $value = $value.Substring(1)
  }
  return $value
}
```

- [ ] **Step 3: Replace `Get-LatestTag` with CLI release filtering**

Replace the body of `Get-LatestTag` with:

```powershell
function Get-LatestTag {
  try {
    $releases = Invoke-RestMethod -Uri $ReleaseApiUrl -UseBasicParsing
  } catch {
    Write-GitHubReleaseProxyHelp
    throw
  }

  foreach ($release in @($releases)) {
    $tagName = [string]$release.tag_name
    if ($tagName -match $CliReleaseTagPattern) {
      return $tagName
    }
  }

  throw "failed to resolve ae-cli release tag"
}
```

- [ ] **Step 4: Derive archive names from stripped versions**

Replace:

```powershell
$ReleaseVersion = $Tag.TrimStart("v")
```

with:

```powershell
$ReleaseVersion = Get-ReleaseVersion $Tag
```

- [ ] **Step 5: Run PowerShell syntax validation**

Run:

```bash
pwsh -NoProfile -Command '$ErrorActionPreference = "Stop"; [scriptblock]::Create((Get-Content -Raw ae-cli/install.ps1)) | Out-Null'
```

Expected: PASS with no output.

- [ ] **Step 6: Commit the PowerShell installer change**

Run:

```bash
git add ae-cli/install.ps1
git commit -m "fix(ae-cli): update powershell installer release discovery"
```

Expected: commit succeeds.

---

### Task 4: Split GoReleaser Configuration

**Files:**
- Modify: `.goreleaser.yaml`
- Create: `.goreleaser.ae-cli.yaml`
- Test: `.goreleaser.yaml`
- Test: `.goreleaser.ae-cli.yaml`

- [ ] **Step 1: Remove `ae-cli` from platform GoReleaser config**

In `.goreleaser.yaml`, delete the build block with:

```yaml
  - id: ae-cli
    dir: ae-cli
```

Delete the archive block with:

```yaml
  - id: ae-cli
    ids:
      - ae-cli
```

Keep `backend-server`, `backend-updater`, and `backend-bundle` unchanged.

- [ ] **Step 2: Create the CLI-only GoReleaser config**

Create `.goreleaser.ae-cli.yaml`:

```yaml
version: 2

project_name: ae-cli

builds:
  - id: ae-cli
    dir: ae-cli
    main: .
    binary: ae-cli
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    ignore:
      - goos: windows
        goarch: arm64
    ldflags:
      - -s -w
      - -X github.com/ai-efficiency/ae-cli/internal/buildinfo.Version={{ .Env.AE_CLI_VERSION }}

archives:
  - id: ae-cli
    ids:
      - ae-cli
    name_template: "ae-cli_{{ .Env.AE_CLI_VERSION_NO_V }}_{{ .Os }}_{{ .Arch }}"
    formats:
      - tar.gz
    format_overrides:
      - goos: windows
        formats:
          - zip

checksum:
  name_template: checksums.txt
  algorithm: sha256

release:
  github:
    owner: '{{ envOrDefault "GITHUB_REPO_OWNER" "ai-efficiency" }}'
    name: '{{ envOrDefault "GITHUB_REPO_NAME" "ai-efficiency" }}'
  draft: false
  prerelease: auto
  make_latest: false
  name_template: "ae-cli {{ .Env.AE_CLI_VERSION }}"
```

- [ ] **Step 3: Validate GoReleaser configs**

Run:

```bash
goreleaser check --config .goreleaser.yaml
AE_CLI_VERSION=v0.2.0-preview.1 AE_CLI_VERSION_NO_V=0.2.0-preview.1 goreleaser check --config .goreleaser.ae-cli.yaml
```

Expected: both commands PASS.

- [ ] **Step 4: Snapshot build the CLI artifacts**

Run:

```bash
AE_CLI_VERSION=v0.2.0-preview.1 AE_CLI_VERSION_NO_V=0.2.0-preview.1 goreleaser release --snapshot --clean --config .goreleaser.ae-cli.yaml
```

Expected: PASS and `dist/` contains `ae-cli_0.2.0-preview.1_<os>_<arch>` archives plus `checksums.txt`.

- [ ] **Step 5: Verify snapshot version metadata**

Run this on the host platform binary path printed by GoReleaser:

```bash
./dist/ae-cli_darwin_arm64_v8.0/ae-cli version || ./dist/ae-cli_linux_amd64_v1/ae-cli version
```

Expected: output contains:

```text
ae-cli v0.2.0-preview.1
```

If the local dist path differs, use `find dist -type f -name ae-cli -perm -111 -print` to locate the binary and run `version` on that binary.

- [ ] **Step 6: Commit the GoReleaser split**

Run:

```bash
git add .goreleaser.yaml .goreleaser.ae-cli.yaml
git commit -m "chore(release): split cli goreleaser config"
```

Expected: commit succeeds.

---

### Task 5: Add CLI-Only Release Workflow

**Files:**
- Create: `.github/workflows/ae-cli-release.yml`
- Test: `.github/workflows/ae-cli-release.yml`

- [ ] **Step 1: Create the CLI release workflow**

Create `.github/workflows/ae-cli-release.yml`:

```yaml
name: ae-cli Release

on:
  push:
    tags:
      - 'ae-cli/v*'
  workflow_dispatch:
    inputs:
      tag:
        description: CLI release tag, for example ae-cli/v0.2.0-preview.1
        required: true
        type: string

permissions:
  contents: write

jobs:
  prepare:
    runs-on: ubuntu-latest
    outputs:
      tag: ${{ steps.meta.outputs.tag }}
      version: ${{ steps.meta.outputs.version }}
      version_no_v: ${{ steps.meta.outputs.version_no_v }}
      checkout_ref: ${{ steps.meta.outputs.checkout_ref }}
      commit_sha: ${{ steps.meta.outputs.commit_sha }}
    steps:
      - uses: actions/checkout@v6
        with:
          fetch-depth: 0
      - id: meta
        env:
          EVENT_NAME: ${{ github.event_name }}
          INPUT_TAG: ${{ github.event.inputs.tag }}
        run: |
          set -euo pipefail
          if [ "$EVENT_NAME" = "workflow_dispatch" ]; then
            TAG="$INPUT_TAG"
          else
            TAG="${GITHUB_REF#refs/tags/}"
          fi

          echo "$TAG" | grep -Eq '^ae-cli/v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$'
          VERSION="${TAG#ae-cli/}"
          VERSION_NO_V="${VERSION#v}"

          if [ "$EVENT_NAME" = "push" ]; then
            CHECKOUT_REF="refs/tags/$TAG"
            COMMIT_SHA="$(git rev-list -n 1 "refs/tags/$TAG")"
          else
            if git rev-parse "refs/tags/$TAG" >/dev/null 2>&1; then
              CHECKOUT_REF="refs/tags/$TAG"
              COMMIT_SHA="$(git rev-list -n 1 "refs/tags/$TAG")"
            else
              CHECKOUT_REF="${GITHUB_SHA}"
              COMMIT_SHA="${GITHUB_SHA}"
            fi
          fi

          echo "tag=$TAG" >> "$GITHUB_OUTPUT"
          echo "version=$VERSION" >> "$GITHUB_OUTPUT"
          echo "version_no_v=$VERSION_NO_V" >> "$GITHUB_OUTPUT"
          echo "checkout_ref=$CHECKOUT_REF" >> "$GITHUB_OUTPUT"
          echo "commit_sha=$COMMIT_SHA" >> "$GITHUB_OUTPUT"

  test:
    name: verify / ae-cli
    needs: prepare
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
        with:
          fetch-depth: 0
          ref: ${{ needs.prepare.outputs.checkout_ref }}
      - uses: actions/setup-go@v6
        with:
          go-version-file: ae-cli/go.mod
          check-latest: false
          cache-dependency-path: ae-cli/go.sum
      - name: Test ae-cli
        working-directory: ae-cli
        run: go test ./...
      - name: Test ae-cli installer
        run: bash ae-cli/test/install-test.sh

  ensure-release-tag:
    needs: [prepare, test]
    runs-on: ubuntu-latest
    outputs:
      release_ref: ${{ steps.tag.outputs.release_ref }}
    steps:
      - uses: actions/checkout@v6
        with:
          fetch-depth: 0
          ref: ${{ needs.prepare.outputs.checkout_ref }}
      - id: tag
        name: Ensure CLI release tag exists
        env:
          TAG: ${{ needs.prepare.outputs.tag }}
        run: |
          set -euo pipefail
          if [ "${{ github.event_name }}" = "workflow_dispatch" ] && ! git rev-parse "refs/tags/$TAG" >/dev/null 2>&1; then
            git config user.name "github-actions[bot]"
            git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
            git tag -a "$TAG" -m "Release $TAG"
            git push origin "refs/tags/$TAG:refs/tags/$TAG"
          fi

          git fetch --tags --force origin
          git rev-parse "refs/tags/$TAG" >/dev/null
          echo "release_ref=refs/tags/$TAG" >> "$GITHUB_OUTPUT"

  release:
    name: release / ae-cli binaries
    needs: [prepare, ensure-release-tag]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
        with:
          fetch-depth: 0
          ref: ${{ needs.ensure-release-tag.outputs.release_ref }}
      - uses: actions/setup-go@v6
        with:
          go-version-file: ae-cli/go.mod
          check-latest: false
          cache-dependency-path: ae-cli/go.sum
      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v7
        with:
          version: '~> v2'
          args: release --clean --config .goreleaser.ae-cli.yaml
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GITHUB_REPO_OWNER: ${{ github.repository_owner }}
          GITHUB_REPO_NAME: ${{ github.event.repository.name }}
          AE_CLI_VERSION: ${{ needs.prepare.outputs.version }}
          AE_CLI_VERSION_NO_V: ${{ needs.prepare.outputs.version_no_v }}
      - name: Verify CLI release is not repository latest
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          TAG: ${{ needs.prepare.outputs.tag }}
          REPO: ${{ github.repository }}
        run: |
          set -euo pipefail
          latest_tag="$(gh api "repos/$REPO/releases/latest" --jq .tag_name)"
          if [ "$latest_tag" = "$TAG" ]; then
            echo "CLI release $TAG must not become repository latest" >&2
            exit 1
          fi
```

- [ ] **Step 2: Validate workflow YAML structure**

Run:

```bash
ruby -e 'require "yaml"; YAML.load_file(".github/workflows/ae-cli-release.yml"); puts "ok"'
```

Expected:

```text
ok
```

- [ ] **Step 3: Commit the CLI workflow**

Run:

```bash
git add .github/workflows/ae-cli-release.yml
git commit -m "chore(release): add cli release workflow"
```

Expected: commit succeeds.

---

### Task 6: Narrow Platform Release Scope

**Files:**
- Modify: `.github/workflows/release.yml`
- Test: `.github/workflows/release.yml`

- [ ] **Step 1: Remove the platform release `ae-cli-test` job**

In `.github/workflows/release.yml`, delete the whole job:

```yaml
  ae-cli-test:
    name: verify / ae-cli
```

Delete through its final `run: go test ./...` line.

- [ ] **Step 2: Remove `ae-cli-test` from the platform verify gate**

In the `verify.needs` list, remove:

```yaml
      - ae-cli-test
```

The final list must be:

```yaml
    needs:
      - backend-test
      - frontend-test
      - release-frontend-embedding-test
      - deploy-validation
```

- [ ] **Step 3: Validate workflow YAML structure**

Run:

```bash
ruby -e 'require "yaml"; YAML.load_file(".github/workflows/release.yml"); puts "ok"'
```

Expected:

```text
ok
```

- [ ] **Step 4: Commit the platform workflow narrowing**

Run:

```bash
git add .github/workflows/release.yml
git commit -m "chore(release): narrow platform release checks"
```

Expected: commit succeeds.

---

### Task 7: Update Release Documentation And Agent Rules

**Files:**
- Modify: `ae-cli/README.md`
- Modify: `deploy/README.md`
- Modify: `AGENTS.md`
- Modify: `CLAUDE.md`
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-06-10-independent-cli-release-design.md`

- [ ] **Step 1: Update CLI README examples**

In `ae-cli/README.md`, change the specific install example from:

```bash
curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | bash -s -- v0.2.0
```

to:

```bash
curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | bash -s -- ae-cli/v0.2.0
```

Replace:

```markdown
Check whether a newer GitHub Release is available:
```

with:

```markdown
Check whether a newer `ae-cli/v*` GitHub Release is available:
```

In the "Update behavior" list, add:

```markdown
- CLI updates ignore platform `v*` releases; the platform repository latest release is reserved for backend/frontend/deploy updates
```

- [ ] **Step 2: Update deploy README platform artifact list**

In `deploy/README.md`, find the section that lists release artifacts near "After the first tagged GitHub release". Remove the line:

```markdown
- `ae-cli_<version>_<os>_<arch>.tar.gz|zip`
```

Add this sentence after the platform artifact list:

```markdown
`ae-cli` artifacts are published by independent `ae-cli/v*` releases; see [`../ae-cli/README.md`](../ae-cli/README.md) for CLI installation and updates.
```

- [ ] **Step 3: Add release rules to AGENTS.md**

In `AGENTS.md`, under "Testing" or near "Commit Message Convention", add:

```markdown
## Release Units

- Platform releases use `v*` tags and cover backend, frontend, deploy assets, GHCR image publishing, and Helm rollout inputs.
- CLI releases use `ae-cli/v*` tags and publish only `ae-cli` artifacts.
- Do not create a platform `v*` tag for CLI-only changes.
- Do not run Helm rollout for CLI-only `ae-cli/v*` releases.
- Repository-level `/releases/latest` belongs to the platform release line; CLI installer and updater must discover the latest CLI release by filtering `ae-cli/v*` releases.
```

- [ ] **Step 4: Add release rules to CLAUDE.md**

In `CLAUDE.md`, under "Quick Reference", add:

```markdown
- Release units:
  - Platform: `v*` tags publish backend/frontend/deploy, GHCR image, and Helm inputs.
  - CLI: `ae-cli/v*` tags publish only `ae-cli`; no GHCR image and no Helm rollout.
  - Repository `/releases/latest` stays platform-owned.
```

- [ ] **Step 5: Add the implemented release boundary to architecture docs**

In `docs/architecture.md`, add this short paragraph near the current project-level module overview:

```markdown
Release units remain in one repository but are published separately. Platform releases use `v*` tags for the backend/frontend/deploy unit, GHCR image, and Helm-consumed image tags. `ae-cli` releases use `ae-cli/v*` tags and publish only CLI artifacts; CLI installer and updater discovery filters that tag namespace instead of using the platform-owned repository latest release.
```

- [ ] **Step 6: Mark the design spec as implemented**

In `docs/superpowers/specs/2026-06-10-independent-cli-release-design.md`, replace:

```markdown
**Status:** Approved design; implementation plan pending
```

with:

```markdown
**Status:** Implemented release boundary; first live CLI release validation pending
```

- [ ] **Step 7: Commit documentation updates**

Run:

```bash
git add ae-cli/README.md deploy/README.md AGENTS.md CLAUDE.md docs/architecture.md docs/superpowers/specs/2026-06-10-independent-cli-release-design.md
git commit -m "docs(release): document independent cli releases"
```

Expected: commit succeeds.

---

### Task 8: Run Local Verification

**Files:**
- Test: `.goreleaser.yaml`
- Test: `.goreleaser.ae-cli.yaml`
- Test: `ae-cli/install.sh`
- Test: `ae-cli/install.ps1`
- Test: `ae-cli/internal/update`

- [ ] **Step 1: Run ae-cli Go tests**

Run:

```bash
cd ae-cli && go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run installer tests**

Run:

```bash
bash ae-cli/test/install-test.sh
```

Expected: PASS.

- [ ] **Step 3: Run shell syntax validation**

Run:

```bash
bash -n ae-cli/install.sh
```

Expected: PASS with no output.

- [ ] **Step 4: Run PowerShell syntax validation**

Run:

```bash
pwsh -NoProfile -Command '$ErrorActionPreference = "Stop"; [scriptblock]::Create((Get-Content -Raw ae-cli/install.ps1)) | Out-Null'
```

Expected: PASS with no output.

- [ ] **Step 5: Run GoReleaser config validation**

Run:

```bash
goreleaser check --config .goreleaser.yaml
AE_CLI_VERSION=v0.2.0-preview.1 AE_CLI_VERSION_NO_V=0.2.0-preview.1 goreleaser check --config .goreleaser.ae-cli.yaml
```

Expected: both commands PASS.

- [ ] **Step 6: Run CLI snapshot release**

Run:

```bash
AE_CLI_VERSION=v0.2.0-preview.1 AE_CLI_VERSION_NO_V=0.2.0-preview.1 goreleaser release --snapshot --clean --config .goreleaser.ae-cli.yaml
```

Expected: PASS and no GitHub Release is created because this is a snapshot.

- [ ] **Step 7: Commit final plan checkbox updates**

After every earlier checked step in this plan reflects actual completed work, run:

```bash
git add docs/superpowers/plans/2026-06-10-independent-cli-release.md
git commit -m "docs(plans): track independent cli release execution"
```

Expected: commit succeeds.

---

### Task 9: Live Release Validation

**Files:**
- Test: GitHub Actions `ae-cli Release`
- Test: GitHub Releases
- Test: GitHub repository latest release

- [ ] **Step 1: Push implementation commits**

Run:

```bash
git status --short --branch
git push origin main
```

Expected: `main` pushes successfully and local branch is aligned with `origin/main`.

- [ ] **Step 2: Create the first CLI preview tag**

Run:

```bash
git tag -a ae-cli/v0.2.0-preview.1 -m "Release ae-cli/v0.2.0-preview.1"
git push origin refs/tags/ae-cli/v0.2.0-preview.1
```

Expected: tag push succeeds and triggers only the `ae-cli Release` workflow.

- [ ] **Step 3: Verify the CLI release workflow**

Run:

```bash
gh run list --workflow "ae-cli Release" --limit 1
```

Expected: newest run is for `ae-cli/v0.2.0-preview.1`.

Then poll:

```bash
run_id="$(gh run list --workflow "ae-cli Release" --json databaseId --jq '.[0].databaseId')"
gh run watch "$run_id"
```

Expected: workflow completes successfully.

- [ ] **Step 4: Verify the platform release workflow was not triggered by the CLI tag**

Run:

```bash
gh run list --workflow "Release" --limit 5
```

Expected: no run was created for `ae-cli/v0.2.0-preview.1`.

- [ ] **Step 5: Verify CLI release assets**

Run:

```bash
gh release view ae-cli/v0.2.0-preview.1 --json tagName,name,isPrerelease,isDraft,assets --jq '{tagName,name,isPrerelease,isDraft,assets:[.assets[].name]}'
```

Expected: output includes `ae-cli_0.2.0-preview.1_<os>_<arch>` archives and `checksums.txt`, with no backend bundle assets.

- [ ] **Step 6: Verify repository latest still points to a platform release**

Run:

```bash
gh api repos/LichKing-2234/ai-efficiency/releases/latest --jq .tag_name
```

Expected: output starts with `v`, not `ae-cli/`.

- [ ] **Step 7: Verify installer can install the CLI release**

Run in a temporary HOME:

```bash
tmp_home="$(mktemp -d)"
HOME="$tmp_home" PATH="$tmp_home/.local/bin:$PATH" bash ae-cli/install.sh ae-cli/v0.2.0-preview.1
"$tmp_home/.local/bin/ae-cli" version
rm -rf "$tmp_home"
```

Expected:

```text
ae-cli v0.2.0-preview.1
```

- [ ] **Step 8: Commit live validation evidence**

Update the top `Status` line in this plan to:

```markdown
**Status:** Implemented and live CLI release validation passed for `ae-cli/v0.2.0-preview.1`.
```

Then run:

```bash
git add docs/superpowers/plans/2026-06-10-independent-cli-release.md
git commit -m "docs(plans): record cli release validation"
git push origin main
```

Expected: plan status is pushed to `origin/main`.
