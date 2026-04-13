# ae-cli User Install Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an official `ae-cli` one-command installer that downloads the correct GitHub Release archive, verifies `checksums.txt`, installs to `~/.local/bin/ae-cli`, and documents the CLI install path separately from backend deployment.

**Architecture:** The installer remains a standalone Bash script at `ae-cli/install.sh`, separate from `deploy/install.sh`. Tests use a shell fixture that synthesizes local release archives and calls the installer through `file://` URLs, so the install flow is exercised end-to-end without touching the real home directory or the network. Documentation lives under `ae-cli/README.md`, while `deploy/README.md` only cross-links to it and keeps backend deployment as its primary scope.

**Tech Stack:** Bash, GitHub Release asset conventions from `.goreleaser.yaml`, `curl`, `tar`, `sha256sum`/`shasum`, Markdown.

---

## File Map

### New files

- `ae-cli/install.sh`
  User-level installer for `ae-cli` that resolves the release tag, downloads the archive, verifies checksum integrity, and installs to `~/.local/bin/ae-cli`.
- `ae-cli/test/install-test.sh`
  Shell fixture test covering latest install, pinned install, checksum failure, missing binary failure, and PATH warning behavior.
- `ae-cli/README.md`
  Official CLI install and verification guide, clearly separated from backend deployment docs.

### Modified files

- `deploy/README.md`
  Add a brief “developer CLI” cross-reference so backend deploy docs no longer act as the implicit entrypoint for `ae-cli` installation.

### Existing files to read before implementation

- `docs/superpowers/specs/2026-04-13-ae-cli-user-install-design.md`
- `.goreleaser.yaml`
- `deploy/install.sh`
- `deploy/test/docker-deploy-bootstrap-test.sh`
- `deploy/README.md`

### Decisions Locked By This Plan

1. The CLI installer stays separate from `deploy/install.sh`.
2. The install target is fixed to `~/.local/bin/ae-cli`; tests should change `HOME`, not the documented install path.
3. The installer warns about missing `PATH` entries but never edits shell profiles.
4. Test-only release URL / platform overrides are allowed as undocumented env hooks so the shell fixture can stay hermetic.

---

### Task 1: Add Shell Fixture Coverage And Implement The Installer

**Files:**
- Create: `ae-cli/test/install-test.sh`
- Create: `ae-cli/install.sh`
- Test: `ae-cli/test/install-test.sh`

- [ ] **Step 1: Write the failing shell fixture test**

Create `ae-cli/test/install-test.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

INSTALLER="$TMP_ROOT/install.sh"
RELEASE_ROOT="$TMP_ROOT/releases"
LATEST_TAG="v0.2.0-test"
PINNED_TAG="v0.2.1-test"
BAD_CHECKSUM_TAG="v0.2.2-bad"
MISSING_BINARY_TAG="v0.2.3-missing-binary"
PATH_WARNING_TAG="v0.2.4-path-warning"

cp "$ROOT_DIR/ae-cli/install.sh" "$INSTALLER"
chmod +x "$INSTALLER"

make_cli_archive() {
  local tag="$1"
  local version="${tag#v}"
  local stage_dir="$TMP_ROOT/stage-$version"
  local release_dir="$RELEASE_ROOT/$tag"
  local archive="ae-cli_${version}_linux_amd64.tar.gz"
  local sha=""

  rm -rf "$stage_dir"
  mkdir -p "$stage_dir" "$release_dir"
  cat >"$stage_dir/ae-cli" <<EOF
#!/usr/bin/env bash
set -euo pipefail
echo "ae-cli ${tag}"
EOF
  chmod +x "$stage_dir/ae-cli"

  tar -czf "$release_dir/$archive" -C "$stage_dir" ae-cli
  sha="$(openssl dgst -sha256 "$release_dir/$archive" | awk '{print $NF}')"
  printf '%s  %s\n' "$sha" "$archive" >"$release_dir/checksums.txt"
}

make_bad_checksum_archive() {
  local tag="$1"
  local version="${tag#v}"
  local release_dir="$RELEASE_ROOT/$tag"
  local archive="ae-cli_${version}_linux_amd64.tar.gz"

  make_cli_archive "$tag"
  printf '%064d  %s\n' 0 "$archive" >"$release_dir/checksums.txt"
}

make_missing_binary_archive() {
  local tag="$1"
  local version="${tag#v}"
  local stage_dir="$TMP_ROOT/stage-$version-missing"
  local release_dir="$RELEASE_ROOT/$tag"
  local archive="ae-cli_${version}_linux_amd64.tar.gz"
  local sha=""

  rm -rf "$stage_dir"
  mkdir -p "$stage_dir" "$release_dir"
  printf 'not the cli binary\n' >"$stage_dir/README.txt"
  tar -czf "$release_dir/$archive" -C "$stage_dir" README.txt
  sha="$(openssl dgst -sha256 "$release_dir/$archive" | awk '{print $NF}')"
  printf '%s  %s\n' "$sha" "$archive" >"$release_dir/checksums.txt"
}

run_installer() {
  local home_dir="$1"
  local path_value="$2"
  local latest_url="$3"
  shift 3

  env -i \
    HOME="$home_dir" \
    PATH="$path_value" \
    AE_CLI_INSTALL_TEST_OS=linux \
    AE_CLI_INSTALL_TEST_ARCH=amd64 \
    AE_CLI_INSTALL_RELEASE_API_URL="$latest_url" \
    AE_CLI_INSTALL_RELEASE_DOWNLOAD_BASE="file://$RELEASE_ROOT" \
    bash "$INSTALLER" "$@"
}

make_cli_archive "$LATEST_TAG"
make_cli_archive "$PINNED_TAG"
make_bad_checksum_archive "$BAD_CHECKSUM_TAG"
make_missing_binary_archive "$MISSING_BINARY_TAG"
make_cli_archive "$PATH_WARNING_TAG"
printf '{"tag_name":"%s"}\n' "$LATEST_TAG" >"$TMP_ROOT/latest.json"

LATEST_HOME="$TMP_ROOT/home-latest"
PINNED_HOME="$TMP_ROOT/home-pinned"
BAD_HOME="$TMP_ROOT/home-bad"
MISSING_HOME="$TMP_ROOT/home-missing"
PATH_WARNING_HOME="$TMP_ROOT/home-path-warning"
mkdir -p "$LATEST_HOME" "$PINNED_HOME" "$BAD_HOME" "$MISSING_HOME" "$PATH_WARNING_HOME"

LATEST_LOG="$TMP_ROOT/latest.log"
run_installer \
  "$LATEST_HOME" \
  "$LATEST_HOME/.local/bin:/usr/bin:/bin" \
  "file://$TMP_ROOT/latest.json" \
  >"$LATEST_LOG" 2>&1

test -x "$LATEST_HOME/.local/bin/ae-cli"
"$LATEST_HOME/.local/bin/ae-cli" | grep -q "ae-cli ${LATEST_TAG}"
grep -q "Installing ae-cli ${LATEST_TAG}" "$LATEST_LOG"
grep -q "Installed ae-cli ${LATEST_TAG} to $LATEST_HOME/.local/bin/ae-cli" "$LATEST_LOG"
! grep -q "is not in PATH" "$LATEST_LOG"

PINNED_LOG="$TMP_ROOT/pinned.log"
run_installer \
  "$PINNED_HOME" \
  "$PINNED_HOME/.local/bin:/usr/bin:/bin" \
  "file://$TMP_ROOT/latest.json" \
  "$PINNED_TAG" \
  >"$PINNED_LOG" 2>&1

test -x "$PINNED_HOME/.local/bin/ae-cli"
"$PINNED_HOME/.local/bin/ae-cli" | grep -q "ae-cli ${PINNED_TAG}"
grep -q "Installed ae-cli ${PINNED_TAG} to $PINNED_HOME/.local/bin/ae-cli" "$PINNED_LOG"

BAD_LOG="$TMP_ROOT/bad.log"
set +e
run_installer \
  "$BAD_HOME" \
  "$BAD_HOME/.local/bin:/usr/bin:/bin" \
  "file://$TMP_ROOT/latest.json" \
  "$BAD_CHECKSUM_TAG" \
  >"$BAD_LOG" 2>&1
bad_status=$?
set -e

test "$bad_status" -ne 0
grep -q "checksum verification failed" "$BAD_LOG"
test ! -e "$BAD_HOME/.local/bin/ae-cli"

MISSING_LOG="$TMP_ROOT/missing.log"
set +e
run_installer \
  "$MISSING_HOME" \
  "$MISSING_HOME/.local/bin:/usr/bin:/bin" \
  "file://$TMP_ROOT/latest.json" \
  "$MISSING_BINARY_TAG" \
  >"$MISSING_LOG" 2>&1
missing_status=$?
set -e

test "$missing_status" -ne 0
grep -q "release archive missing ae-cli" "$MISSING_LOG"
test ! -e "$MISSING_HOME/.local/bin/ae-cli"

printf '# existing zsh config\n' >"$PATH_WARNING_HOME/.zshrc"
printf '# existing bash config\n' >"$PATH_WARNING_HOME/.bashrc"
cp "$PATH_WARNING_HOME/.zshrc" "$TMP_ROOT/zshrc.expected"
cp "$PATH_WARNING_HOME/.bashrc" "$TMP_ROOT/bashrc.expected"

PATH_WARNING_LOG="$TMP_ROOT/path-warning.log"
run_installer \
  "$PATH_WARNING_HOME" \
  "/usr/bin:/bin" \
  "file://$TMP_ROOT/latest.json" \
  "$PATH_WARNING_TAG" \
  >"$PATH_WARNING_LOG" 2>&1

test -x "$PATH_WARNING_HOME/.local/bin/ae-cli"
grep -q "Warning: $PATH_WARNING_HOME/.local/bin is not in PATH." "$PATH_WARNING_LOG"
grep -q "export PATH=\"$PATH_WARNING_HOME/.local/bin:\$PATH\"" "$PATH_WARNING_LOG"
cmp -s "$PATH_WARNING_HOME/.zshrc" "$TMP_ROOT/zshrc.expected"
cmp -s "$PATH_WARNING_HOME/.bashrc" "$TMP_ROOT/bashrc.expected"
```

- [ ] **Step 2: Run the shell fixture to verify the red state**

Run:

```bash
cd /Users/admin/ai-efficiency
bash ae-cli/test/install-test.sh
```

Expected:
- FAIL because `ae-cli/install.sh` does not exist yet.
- The first visible failure should be the `cp "$ROOT_DIR/ae-cli/install.sh" "$INSTALLER"` line.

- [ ] **Step 3: Write the minimal installer to satisfy the fixture**

Create `ae-cli/install.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

GITHUB_REPO="LichKing-2234/ai-efficiency"
INSTALL_DIR="${HOME}/.local/bin"
TARGET_PATH="${INSTALL_DIR}/ae-cli"
RELEASE_API_URL="${AE_CLI_INSTALL_RELEASE_API_URL:-https://api.github.com/repos/${GITHUB_REPO}/releases/latest}"
RELEASE_DOWNLOAD_BASE="${AE_CLI_INSTALL_RELEASE_DOWNLOAD_BASE:-https://github.com/${GITHUB_REPO}/releases/download}"
TMP_DIR=""
TEMP_TARGET=""
OS=""
ARCH=""

cleanup() {
  if [[ -n "$TEMP_TARGET" ]]; then
    rm -f "$TEMP_TARGET"
  fi
  if [[ -n "$TMP_DIR" ]]; then
    rm -rf "$TMP_DIR"
  fi
}

trap cleanup EXIT

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

sha256_file() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
    return 0
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
    return 0
  fi
  echo "missing required command: sha256sum or shasum" >&2
  exit 1
}

detect_platform() {
  OS="${AE_CLI_INSTALL_TEST_OS:-$(uname -s | tr '[:upper:]' '[:lower:]')}"
  ARCH="${AE_CLI_INSTALL_TEST_ARCH:-$(uname -m)}"

  case "$OS" in
    linux|darwin) ;;
    *)
      echo "unsupported OS: $OS" >&2
      exit 1
      ;;
  esac

  case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *)
      echo "unsupported architecture: $ARCH" >&2
      exit 1
      ;;
  esac
}

latest_tag() {
  local tag=""
  tag="$(
    curl -fsSL "$RELEASE_API_URL" | awk -F'"' '/"tag_name"/ { print $4; exit }'
  )"
  if [[ -z "$tag" ]]; then
    echo "failed to resolve release tag" >&2
    exit 1
  fi
  printf '%s\n' "$tag"
}

download_release() {
  local tag="$1"
  local version="${tag#v}"
  local archive="ae-cli_${version}_${OS}_${ARCH}.tar.gz"
  local base="${RELEASE_DOWNLOAD_BASE%/}/${tag}"
  local expected=""
  local actual=""

  curl -fsSL "${base}/${archive}" -o "${TMP_DIR}/${archive}"
  curl -fsSL "${base}/checksums.txt" -o "${TMP_DIR}/checksums.txt"

  expected="$(awk "/  ${archive}\$/ { print \\$1; exit }" "${TMP_DIR}/checksums.txt")"
  actual="$(sha256_file "${TMP_DIR}/${archive}")"
  if [[ -z "$expected" ]]; then
    echo "missing checksum for ${archive}" >&2
    exit 1
  fi
  if [[ "$expected" != "$actual" ]]; then
    echo "checksum verification failed for ${archive}" >&2
    exit 1
  fi

  tar -xzf "${TMP_DIR}/${archive}" -C "${TMP_DIR}"
  if [[ ! -f "${TMP_DIR}/ae-cli" ]]; then
    echo "release archive missing ae-cli" >&2
    exit 1
  fi
}

install_binary() {
  mkdir -p "$INSTALL_DIR"
  TEMP_TARGET="${INSTALL_DIR}/.ae-cli.tmp.$$"
  cp "${TMP_DIR}/ae-cli" "$TEMP_TARGET"
  chmod 0755 "$TEMP_TARGET"
  mv "$TEMP_TARGET" "$TARGET_PATH"
  TEMP_TARGET=""
}

path_contains_install_dir() {
  case ":${PATH:-}:" in
    *":${INSTALL_DIR}:"*) return 0 ;;
    *) return 1 ;;
  esac
}

main() {
  require_cmd curl
  require_cmd tar
  detect_platform
  TMP_DIR="$(mktemp -d)"

  local tag="${1:-$(latest_tag)}"
  echo "Installing ae-cli ${tag}..."
  download_release "$tag"
  install_binary
  echo "Installed ae-cli ${tag} to ${TARGET_PATH}"

  if ! path_contains_install_dir; then
    echo "Warning: ${INSTALL_DIR} is not in PATH."
    echo "Add it to your shell profile, for example:"
    echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
  fi
}

main "$@"
```

- [ ] **Step 4: Run the shell fixture to verify green**

Run:

```bash
cd /Users/admin/ai-efficiency
bash ae-cli/test/install-test.sh
bash -n ae-cli/install.sh
```

Expected:
- PASS with no output from `ae-cli/test/install-test.sh`
- PASS from `bash -n ae-cli/install.sh`

- [ ] **Step 5: Commit the installer slice**

Run:

```bash
cd /Users/admin/ai-efficiency
git add ae-cli/install.sh ae-cli/test/install-test.sh
git commit -m "feat(ae-cli): add user install script"
```

Expected:
- A commit containing the installer and its shell fixture coverage

---

### Task 2: Add CLI Install Docs And Distinguish Them From Backend Deployment Docs

**Files:**
- Create: `ae-cli/README.md`
- Modify: `deploy/README.md`
- Test: `ae-cli/README.md`

- [ ] **Step 1: Write the failing documentation checks**

Run:

```bash
cd /Users/admin/ai-efficiency
test -f ae-cli/README.md
rg -n "This guide covers backend deployment only" deploy/README.md
```

Expected:
- `test -f ae-cli/README.md` fails because the file does not exist yet.
- The `rg` command returns no match because `deploy/README.md` does not yet separate backend deploy docs from CLI install docs explicitly.

- [ ] **Step 2: Create the CLI README and add the deploy cross-reference**

Create `ae-cli/README.md`:

````markdown
# ae-cli

## Install

Install the latest release:

```bash
curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | bash
```

Install a specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | bash -s -- v0.2.0
```

The installer:

- downloads the matching GitHub Release archive
- verifies `checksums.txt`
- installs `ae-cli` to `~/.local/bin/ae-cli`
- prints a warning if `~/.local/bin` is not on `PATH`

## Verify

```bash
ae-cli version
```

## Windows

Windows users should download `ae-cli_<version>_<os>_<arch>.zip` from GitHub Releases and place `ae-cli.exe` on `PATH` manually.

## Relationship To Backend Deployment

- `ae-cli/install.sh` installs the developer CLI.
- `deploy/install.sh` installs the backend service for Linux systemd deployments.
````

Update `deploy/README.md` by inserting this section after the overview block and before `## Empty Directory Bootstrap`:

```markdown
## Developer CLI

This guide covers backend deployment only. For the user-level CLI installer, see [`../ae-cli/README.md`](../ae-cli/README.md).
```

- [ ] **Step 3: Verify the documentation contract**

Run:

```bash
cd /Users/admin/ai-efficiency
test -f ae-cli/README.md
grep -F "curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | bash" ae-cli/README.md
rg -n "~/.local/bin/ae-cli" ae-cli/README.md
rg -n "This guide covers backend deployment only" deploy/README.md
rg -n '`ae-cli/install.sh`|`deploy/install.sh`' ae-cli/README.md
```

Expected:
- All commands pass.
- `ae-cli/README.md` contains the official remote install commands, the `~/.local/bin` target, and the Windows manual-install note.
- `deploy/README.md` now explicitly points CLI users to the separate CLI documentation.

- [ ] **Step 4: Commit the documentation slice**

Run:

```bash
cd /Users/admin/ai-efficiency
git add ae-cli/README.md deploy/README.md
git commit -m "docs(ae-cli): document user install path"
```

Expected:
- A commit containing the new CLI README and the deploy-doc cross-reference

---

## Final Verification

- [ ] **Step 1: Re-run the full installer verification and documentation checks**

Run:

```bash
cd /Users/admin/ai-efficiency
bash ae-cli/test/install-test.sh
bash -n ae-cli/install.sh
test -f ae-cli/README.md
rg -n "This guide covers backend deployment only" deploy/README.md
git status --short
```

Expected:
- All verification commands pass.
- `git status --short` only shows the expected tracked changes for this plan.
