# GitHub Primary Repo And Release Automation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `LichKing-2234/ai-efficiency` the public GitHub primary repository, add full-repo PR CI, and add GitHub Release + GHCR publishing aligned with the current repo structure.

**Architecture:** GitHub becomes the source-of-truth remote while the existing GitLab remote remains a read-only mirror. GitHub Actions will own PR verification and release automation; GoReleaser will publish multi-platform binary archives for the backend bundle and `ae-cli`, while the release workflow separately builds and pushes the production image to GHCR using the existing `deploy/Dockerfile` so frontend + backend + updater packaging stays aligned with current deployment assets.

**Tech Stack:** GitHub CLI (`gh`), GitHub Actions, GoReleaser v2, Docker Buildx, GHCR, Go 1.24+, pnpm/Vite/Vitest, shell scripts.

**Status:** ✅ 已完成（2026-04-12）

**Replay Status:** 历史完成记录。不要直接按本文逐 task 重跑；如需再次执行或扩展，请基于当前 GitHub repo、CI workflow 和 release 配置重写执行计划。

**Source Of Truth:** 当前 remote、GitHub workflow、GoReleaser 配置和部署默认值以现有仓库配置为准。本文保留实施切片与验收轨迹。

> **Updated:** 2026-04-12 — 基于代码审查回填状态与 checkbox。

---

## File Map

### New files

- `.github/workflows/ci.yml`
  GitHub Actions workflow for PR/push validation across `backend`, `ae-cli`, `frontend`, and deploy static checks.
- `.github/workflows/release.yml`
  GitHub Actions workflow for tag/manual release, GitHub Release publishing, and GHCR image publishing.
- `.goreleaser.yaml`
  GoReleaser configuration for multi-platform backend bundle and `ae-cli` archives plus checksums.

### Modified files

- `.gitignore`
  Ignore generated frontend TypeScript build-info files so GitHub/CI work does not keep the worktree dirty.
- `CLAUDE.md`
  Update the quick-reference remote note so GitHub is primary and GitLab is a mirror.
- `ae-cli/cmd/version.go`
  Switch `ae-cli version` to use build-time version data instead of a hardcoded constant.
- `ae-cli/cmd/version_test.go`
  Verify the version command reads `internal/buildinfo.Version`.
- `backend/internal/config/config.go`
  Update deployment update defaults to the new GitHub repo / GHCR namespace.
- `backend/internal/config/config_test.go`
  Assert the updated deployment update defaults.
- `deploy/.env.example`
  Update default release API / GHCR image values to the GitHub primary repo.
- `deploy/config.example.yaml`
  Update default release API / GHCR image values to the GitHub primary repo.
- `deploy/README.md`
  Document GitHub Release binaries and GHCR image usage.

### Existing files to read before implementation

- `ae-cli/internal/buildinfo/buildinfo.go`
- `ae-cli/cmd/version.go`
- `ae-cli/cmd/version_test.go`
- `backend/internal/config/config.go`
- `backend/internal/config/config_test.go`
- `deploy/Dockerfile`
- `deploy/README.md`
- `deploy/.env.example`
- `deploy/config.example.yaml`
- `/tmp/sub2api-reference/.github/workflows/backend-ci.yml`
- `/tmp/sub2api-reference/.github/workflows/release.yml`
- `/tmp/sub2api-reference/.goreleaser.yaml`

### Current operational context

- Current remote:
  - `origin -> ssh://git@git.agoralab.co/ai/ai-efficiency.git`
- Target GitHub primary repo:
  - `https://github.com/LichKing-2234/ai-efficiency.git`
- Existing dirty generated file:
  - `frontend/tsconfig.app.tsbuildinfo`
- Existing local uncommitted feature work must remain untouched:
  - any unrelated modifications outside this plan’s file list

### Architectural decisions locked in by this plan

1. GitHub becomes `origin`; GitLab is retained as a `gitlab` mirror remote.
2. PR CI runs the full repository verification surface:
   - `cd backend && go test ./...`
   - `cd ae-cli && go test ./...`
   - `cd frontend && pnpm test`
   - `cd frontend && pnpm build`
   - deploy static checks
3. Release supports both:
   - `push` of `v*` tags
   - manual `workflow_dispatch`
4. Release publishes:
   - GitHub Release binary archives
   - GHCR image only (no Docker Hub)
5. Binary archives cover:
   - backend bundle (`server` + `updater`)
   - `ae-cli`
   across `linux + darwin + windows` for supported `amd64/arm64` combinations.
6. GHCR image publishing uses the existing `deploy/Dockerfile` via Docker Buildx in the workflow instead of forcing GoReleaser to own image assembly.

---

### Task 1: Bootstrap GitHub As Primary Remote And Clean Repo Metadata

**Files:**
- Modify: `.gitignore`
- Modify: `CLAUDE.md`

- [x] **Step 1: Verify current remotes and create the GitHub repo**

Run:

```bash
cd /Users/admin/ai-efficiency
git remote -v
gh repo view LichKing-2234/ai-efficiency >/dev/null 2>&1 || true
```

Expected:
- `origin` currently points at `ssh://git@git.agoralab.co/ai/ai-efficiency.git`
- GitHub repo may or may not exist yet

- [x] **Step 2: Rename GitLab remote, create/connect GitHub primary repo, and push current main**

Run:

```bash
cd /Users/admin/ai-efficiency

if git remote get-url origin >/dev/null 2>&1; then
  git remote rename origin gitlab
fi

if gh repo view LichKing-2234/ai-efficiency >/dev/null 2>&1; then
  git remote add origin https://github.com/LichKing-2234/ai-efficiency.git 2>/dev/null || git remote set-url origin https://github.com/LichKing-2234/ai-efficiency.git
else
  gh repo create LichKing-2234/ai-efficiency --public --source=. --remote=origin --push
fi

git push -u origin main
git remote set-url gitlab ssh://git@git.agoralab.co/ai/ai-efficiency.git
git switch -c github-primary-repo-release-automation
```

Expected:
- `origin` points to GitHub
- `gitlab` points to the old GitLab URL
- `main` exists on GitHub
- local feature branch is `github-primary-repo-release-automation`

- [x] **Step 3: Ignore generated frontend build-info files**

Update `.gitignore`:

```gitignore
/frontend/tsconfig.app.tsbuildinfo
/frontend/tsconfig.node.tsbuildinfo
```

- [x] **Step 4: Update quick-reference remote metadata**

Replace the remote line in `CLAUDE.md`:

```md
- Primary remote: `https://github.com/LichKing-2234/ai-efficiency.git`
- GitLab mirror: `ssh://git@git.agoralab.co/ai/ai-efficiency.git`
```

- [x] **Step 5: Verify repo bootstrap state**

Run:

```bash
cd /Users/admin/ai-efficiency
git remote -v
gh repo view LichKing-2234/ai-efficiency --json url,visibility,defaultBranchRef
git status --short
```

Expected:
- `origin` uses GitHub
- `gitlab` uses GitLab
- GitHub repo is public
- only `.gitignore` and `CLAUDE.md` are staged for this task (ignore unrelated pre-existing local changes)

- [x] **Step 6: Commit**

```bash
git add .gitignore CLAUDE.md
git commit -m "chore(repo): bootstrap github primary remote"
```

### Task 2: Make Version Metadata And Deployment Defaults GitHub-Aware

**Files:**
- Modify: `ae-cli/cmd/version.go`
- Modify: `ae-cli/cmd/version_test.go`
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `deploy/.env.example`
- Modify: `deploy/config.example.yaml`
- Test: `ae-cli/cmd/version_test.go`
- Test: `backend/internal/config/config_test.go`

- [x] **Step 1: Write the failing ae-cli version and config-default tests**

Append to `ae-cli/cmd/version_test.go`:

```go
func TestVersionCommandUsesBuildInfoVersion(t *testing.T) {
	oldVersion := buildinfo.Version
	buildinfo.Version = "v9.9.9"
	t.Cleanup(func() {
		buildinfo.Version = oldVersion
	})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"version"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute version command: %v", err)
	}

	if got := buf.String(); got != "ae-cli v9.9.9\n" {
		t.Fatalf("output = %q, want %q", got, "ae-cli v9.9.9\\n")
	}
}
```

Add to `backend/internal/config/config_test.go`:

```go
func TestDeploymentDefaultsPointAtGitHubPrimaryRepo(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}

	if cfg.Deployment.Update.ReleaseAPIURL != "https://api.github.com/repos/LichKing-2234/ai-efficiency/releases/latest" {
		t.Fatalf("release_api_url = %q", cfg.Deployment.Update.ReleaseAPIURL)
	}
	if cfg.Deployment.Update.ImageRepository != "ghcr.io/lichking-2234/ai-efficiency" {
		t.Fatalf("image_repository = %q", cfg.Deployment.Update.ImageRepository)
	}
}
```

- [x] **Step 2: Run the targeted tests to confirm they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./cmd -run 'TestVersionCommandUsesBuildInfoVersion$' -count=1

cd /Users/admin/ai-efficiency/backend
go test ./internal/config -run 'TestDeploymentDefaultsPointAtGitHubPrimaryRepo$' -count=1
```

Expected:
- `ae-cli` test fails because `cmd/version.go` still uses a hardcoded constant
- backend test fails because GitHub release defaults still point at the old owner/repo

- [x] **Step 3: Implement build-info-backed version output and GitHub defaults**

Update `ae-cli/cmd/version.go`:

```go
package cmd

import (
	"fmt"

	"github.com/ai-efficiency/ae-cli/internal/buildinfo"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of ae-cli",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(cmd.OutOrStdout(), "ae-cli "+buildinfo.Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
```

Update `backend/internal/config/config.go` defaults:

```go
v.SetDefault("deployment.update.release_api_url", "https://api.github.com/repos/LichKing-2234/ai-efficiency/releases/latest")
v.SetDefault("deployment.update.image_repository", "ghcr.io/lichking-2234/ai-efficiency")
```

Update `deploy/.env.example`:

```bash
AE_IMAGE_REPOSITORY=ghcr.io/lichking-2234/ai-efficiency
AE_UPDATER_IMAGE_REPOSITORY=ghcr.io/lichking-2234/ai-efficiency
AE_DEPLOYMENT_UPDATE_RELEASE_API_URL=https://api.github.com/repos/LichKing-2234/ai-efficiency/releases/latest
```

Update `deploy/config.example.yaml`:

```yaml
deployment:
  update:
    release_api_url: "https://api.github.com/repos/LichKing-2234/ai-efficiency/releases/latest"
    image_repository: "ghcr.io/lichking-2234/ai-efficiency"
```

- [x] **Step 4: Run the targeted tests**

Run:

```bash
cd /Users/admin/ai-efficiency/ae-cli
go test ./cmd -run 'TestVersionCommandUsesBuildInfoVersion$' -count=1

cd /Users/admin/ai-efficiency/backend
go test ./internal/config -run 'TestDeploymentDefaultsPointAtGitHubPrimaryRepo$' -count=1
```

Expected: PASS

- [x] **Step 5: Commit**

```bash
git add ae-cli/cmd/version.go ae-cli/cmd/version_test.go backend/internal/config/config.go backend/internal/config/config_test.go deploy/.env.example deploy/config.example.yaml
git commit -m "chore(release): point defaults at github primary repo"
```

### Task 3: Add GitHub PR CI Workflow

**Files:**
- Create: `.github/workflows/ci.yml`
- Test: `.github/workflows/ci.yml`

- [x] **Step 1: Create the CI workflow**

Create `.github/workflows/ci.yml`:

```yaml
name: CI

on:
  push:
    branches:
      - main
  pull_request:

permissions:
  contents: read

concurrency:
  group: ci-${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true

jobs:
  backend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-go@v6
        with:
          go-version-file: backend/go.mod
          check-latest: false
          cache-dependency-path: backend/go.sum
      - name: Test backend
        working-directory: backend
        run: go test ./...

  ae-cli:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-go@v6
        with:
          go-version-file: ae-cli/go.mod
          check-latest: false
          cache-dependency-path: ae-cli/go.sum
      - name: Test ae-cli
        working-directory: ae-cli
        run: go test ./...

  frontend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - uses: pnpm/action-setup@v4
        with:
          version: 9
      - uses: actions/setup-node@v6
        with:
          node-version: '20'
          cache: 'pnpm'
          cache-dependency-path: frontend/pnpm-lock.yaml
      - name: Install frontend dependencies
        working-directory: frontend
        run: pnpm install --frozen-lockfile
      - name: Test frontend
        working-directory: frontend
        run: pnpm test
      - name: Build frontend
        working-directory: frontend
        run: pnpm build

  deploy-static:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - name: Validate deploy shell script
        run: bash -n deploy/docker-deploy.sh
      - name: Validate compose configs
        run: |
          validate_compose() {
            local compose_file="$1"
            if docker compose --env-file deploy/.env.example -f "$compose_file" config >/dev/null 2>&1; then
              echo "validated with docker compose: $compose_file"
              return 0
            fi
            if command -v docker-compose >/dev/null 2>&1; then
              docker-compose --env-file deploy/.env.example -f "$compose_file" config >/dev/null
              echo "validated with docker-compose: $compose_file"
              return 0
            fi
            echo "no compatible compose implementation available" >&2
            exit 1
          }

          validate_compose deploy/docker-compose.yml
          validate_compose deploy/docker-compose.external.yml
```

- [x] **Step 2: Validate the workflow syntax locally**

Run:

```bash
cd /Users/admin/ai-efficiency
go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/ci.yml
```

Expected: PASS

- [x] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci(github): add pull request validation workflow"
```

### Task 4: Add GoReleaser Config And Release Workflow

**Files:**
- Create: `.goreleaser.yaml`
- Create: `.github/workflows/release.yml`
- Test: `.goreleaser.yaml`
- Test: `.github/workflows/release.yml`

- [x] **Step 1: Create the GoReleaser config**

Create `.goreleaser.yaml`:

```yaml
version: 2

project_name: ai-efficiency

builds:
  - id: backend-server
    dir: backend
    main: ./cmd/server
    binary: ai-efficiency-server
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
      - -X github.com/ai-efficiency/backend/internal/deployment.BuildVersion={{ .Version }}
      - -X github.com/ai-efficiency/backend/internal/deployment.BuildCommit={{ .Commit }}
      - -X github.com/ai-efficiency/backend/internal/deployment.BuildTime={{ .Date }}

  - id: backend-updater
    dir: backend
    main: ./cmd/updater
    binary: ai-efficiency-updater
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
      - -X github.com/ai-efficiency/backend/internal/deployment.BuildVersion={{ .Version }}
      - -X github.com/ai-efficiency/backend/internal/deployment.BuildCommit={{ .Commit }}
      - -X github.com/ai-efficiency/backend/internal/deployment.BuildTime={{ .Date }}

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
      - -X github.com/ai-efficiency/ae-cli/internal/buildinfo.Version={{ .Version }}

archives:
  - id: backend-bundle
    ids:
      - backend-server
      - backend-updater
    name_template: "ai-efficiency-backend_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    format: tar.gz
    format_overrides:
      - goos: windows
        format: zip
    files:
      - deploy/README.md
      - deploy/config.example.yaml

  - id: ae-cli
    ids:
      - ae-cli
    name_template: "ae-cli_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    format: tar.gz
    format_overrides:
      - goos: windows
        format: zip

checksum:
  name_template: checksums.txt
  algorithm: sha256

release:
  github:
    owner: "{{ .Env.GITHUB_REPO_OWNER }}"
    name: "{{ .Env.GITHUB_REPO_NAME }}"
  draft: false
  prerelease: auto
  name_template: "ai-efficiency {{ .Version }}"
```

- [x] **Step 2: Create the release workflow**

Create `.github/workflows/release.yml`:

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'
  workflow_dispatch:
    inputs:
      tag:
        description: Release tag (for example v0.2.0)
        required: true
        type: string

permissions:
  contents: write
  packages: write

jobs:
  prepare:
    runs-on: ubuntu-latest
    outputs:
      tag: ${{ steps.meta.outputs.tag }}
      version: ${{ steps.meta.outputs.version }}
      major: ${{ steps.meta.outputs.major }}
      minor: ${{ steps.meta.outputs.minor }}
      owner_lower: ${{ steps.owner.outputs.owner_lower }}
      build_time: ${{ steps.meta.outputs.build_time }}
    steps:
      - uses: actions/checkout@v6
        with:
          fetch-depth: 0
      - id: meta
        run: |
          if [ "${{ github.event_name }}" = "workflow_dispatch" ]; then
            TAG="${{ github.event.inputs.tag }}"
          else
            TAG="${GITHUB_REF#refs/tags/}"
          fi

          echo "$TAG" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'

          if [ "${{ github.event_name }}" = "workflow_dispatch" ]; then
            if ! git rev-parse "$TAG" >/dev/null 2>&1; then
              git config user.name "github-actions[bot]"
              git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
              git tag -a "$TAG" -m "Release $TAG"
              git push origin "$TAG"
            fi
          fi

          VERSION="${TAG#v}"
          MAJOR="${VERSION%%.*}"
          REST="${VERSION#*.}"
          MINOR="${REST%%.*}"
          BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

          echo "tag=$TAG" >> "$GITHUB_OUTPUT"
          echo "version=$VERSION" >> "$GITHUB_OUTPUT"
          echo "major=$MAJOR" >> "$GITHUB_OUTPUT"
          echo "minor=$MINOR" >> "$GITHUB_OUTPUT"
          echo "build_time=$BUILD_TIME" >> "$GITHUB_OUTPUT"

      - id: owner
        run: echo "owner_lower=$(echo '${{ github.repository_owner }}' | tr '[:upper:]' '[:lower:]')" >> "$GITHUB_OUTPUT"

  verify:
    needs: prepare
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
        with:
          fetch-depth: 0
          ref: ${{ needs.prepare.outputs.tag }}
      - uses: actions/setup-go@v6
        with:
          go-version-file: backend/go.mod
          check-latest: false
          cache-dependency-path: |
            backend/go.sum
            ae-cli/go.sum
      - uses: pnpm/action-setup@v4
        with:
          version: 9
      - uses: actions/setup-node@v6
        with:
          node-version: '20'
          cache: 'pnpm'
          cache-dependency-path: frontend/pnpm-lock.yaml
      - name: Test backend
        working-directory: backend
        run: go test ./...
      - name: Test ae-cli
        working-directory: ae-cli
        run: go test ./...
      - name: Install frontend dependencies
        working-directory: frontend
        run: pnpm install --frozen-lockfile
      - name: Test frontend
        working-directory: frontend
        run: pnpm test
      - name: Build frontend
        working-directory: frontend
        run: pnpm build
      - name: Validate deploy shell script
        run: bash -n deploy/docker-deploy.sh

  release:
    needs: [prepare, verify]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
        with:
          fetch-depth: 0
          ref: ${{ needs.prepare.outputs.tag }}
      - uses: actions/setup-go@v6
        with:
          go-version-file: backend/go.mod
          check-latest: false
          cache-dependency-path: |
            backend/go.sum
            ae-cli/go.sum
      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v7
        with:
          version: '~> v2'
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GITHUB_REPO_OWNER: ${{ github.repository_owner }}
          GITHUB_REPO_NAME: ${{ github.event.repository.name }}

      - uses: docker/setup-qemu-action@v3
      - uses: docker/setup-buildx-action@v3
      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.repository_owner }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Build and push GHCR image
        uses: docker/build-push-action@v6
        with:
          context: .
          file: deploy/Dockerfile
          platforms: linux/amd64,linux/arm64
          push: true
          tags: |
            ghcr.io/${{ needs.prepare.outputs.owner_lower }}/ai-efficiency:${{ needs.prepare.outputs.tag }}
            ghcr.io/${{ needs.prepare.outputs.owner_lower }}/ai-efficiency:${{ needs.prepare.outputs.version }}
            ghcr.io/${{ needs.prepare.outputs.owner_lower }}/ai-efficiency:${{ needs.prepare.outputs.major }}.${{ needs.prepare.outputs.minor }}
            ghcr.io/${{ needs.prepare.outputs.owner_lower }}/ai-efficiency:${{ needs.prepare.outputs.major }}
            ghcr.io/${{ needs.prepare.outputs.owner_lower }}/ai-efficiency:latest
          build-args: |
            APP_VERSION=${{ needs.prepare.outputs.version }}
            APP_COMMIT=${{ github.sha }}
            APP_BUILD_TIME=${{ needs.prepare.outputs.build_time }}
```

- [x] **Step 3: Validate the workflow and GoReleaser config locally**

Run:

```bash
cd /Users/admin/ai-efficiency
go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/release.yml
go run github.com/goreleaser/goreleaser/v2@latest check --config .goreleaser.yaml
```

Expected: PASS

- [x] **Step 4: Commit**

```bash
git add .github/workflows/release.yml .goreleaser.yaml
git commit -m "ci(github): add release automation"
```

### Task 5: Document GitHub/GHCR Usage And Push The Automation Branch

**Files:**
- Modify: `deploy/README.md`
- Verify: GitHub workflows visible on remote repo

- [x] **Step 1: Update deploy docs for GHCR and GitHub Release usage**

Append to `deploy/README.md`:

````md
## GitHub Release Artifacts

The public GitHub repository publishes:

- `ai-efficiency-backend_<version>_<os>_<arch>.tar.gz|zip`
- `ae-cli_<version>_<os>_<arch>.tar.gz|zip`
- `checksums.txt`

## GHCR Images

Release images are published to:

- `ghcr.io/lichking-2234/ai-efficiency:<tag>`
- `ghcr.io/lichking-2234/ai-efficiency:latest`

Examples:

~~~bash
docker pull ghcr.io/lichking-2234/ai-efficiency:v0.2.0
docker pull ghcr.io/lichking-2234/ai-efficiency:latest
~~~
````

- [x] **Step 2: Run final local validation for the new automation files**

Run:

```bash
cd /Users/admin/ai-efficiency
go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/ci.yml .github/workflows/release.yml
go run github.com/goreleaser/goreleaser/v2@latest check --config .goreleaser.yaml

cd backend && go test ./...
cd ../ae-cli && go test ./...
cd ../frontend && pnpm test && pnpm build
cd ..

docker-compose --env-file deploy/.env.example -f deploy/docker-compose.yml config >/dev/null
docker-compose --env-file deploy/.env.example -f deploy/docker-compose.external.yml config >/dev/null
```

Expected: all commands succeed.

- [x] **Step 3: Commit**

```bash
git add deploy/README.md
git commit -m "docs(deploy): document github release and ghcr usage"
```

- [x] **Step 4: Push the automation branch to GitHub and verify workflows exist**

Run:

```bash
cd /Users/admin/ai-efficiency
git push -u origin github-primary-repo-release-automation
gh workflow list --repo LichKing-2234/ai-efficiency
gh repo view LichKing-2234/ai-efficiency --json url,visibility,defaultBranchRef
```

Expected:
- branch is on GitHub
- workflows are registered
- repo remains public with `main` as default branch

## Self-Review Checklist

- Spec coverage:
  - GitHub primary repo bootstrap: Task 1
  - full-repo PR CI: Task 3
  - tag + manual release support: Task 4
  - GHCR-only image publishing: Task 4
  - GitHub Release binary archives: Task 4
  - GitLab retained as mirror: Task 1
  - deploy/defaults/docs aligned with GitHub primary: Tasks 2 and 5
- Placeholder scan:
  - No `TODO`, `TBD`, or “implement later” markers remain.
- Type consistency:
  - `ReleaseInfo`, `DeploymentStatus`, and build-info variables are referenced by their current repo paths and current package names.
