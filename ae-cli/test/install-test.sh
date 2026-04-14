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
SYMLINK_TAG="v0.2.5-symlink"

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

make_symlink_archive() {
  local tag="$1"
  local version="${tag#v}"
  local stage_dir="$TMP_ROOT/stage-$version-symlink"
  local release_dir="$RELEASE_ROOT/$tag"
  local archive="ae-cli_${version}_linux_amd64.tar.gz"
  local sha=""

  rm -rf "$stage_dir"
  mkdir -p "$stage_dir" "$release_dir"
  printf '#!/usr/bin/env bash\necho symlink target\n' >"$stage_dir/real-binary"
  chmod +x "$stage_dir/real-binary"
  ln -s real-binary "$stage_dir/ae-cli"
  tar -czf "$release_dir/$archive" -C "$stage_dir" ae-cli real-binary
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
make_symlink_archive "$SYMLINK_TAG"
printf '{"tag_name":"%s"}\n' "$LATEST_TAG" >"$TMP_ROOT/latest.json"

LATEST_HOME="$TMP_ROOT/home-latest"
PINNED_HOME="$TMP_ROOT/home-pinned"
BAD_HOME="$TMP_ROOT/home-bad"
MISSING_HOME="$TMP_ROOT/home-missing"
PATH_WARNING_HOME="$TMP_ROOT/home-path-warning"
CONFIG_HOME="$TMP_ROOT/home-config"
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

SYMLINK_HOME="$TMP_ROOT/home-symlink"
mkdir -p "$SYMLINK_HOME"
SYMLINK_LOG="$TMP_ROOT/symlink.log"
set +e
run_installer \
  "$SYMLINK_HOME" \
  "$SYMLINK_HOME/.local/bin:/usr/bin:/bin" \
  "file://$TMP_ROOT/latest.json" \
  "$SYMLINK_TAG" \
  >"$SYMLINK_LOG" 2>&1
symlink_status=$?
set -e

test "$symlink_status" -ne 0
grep -q "release archive ae-cli must be a regular file" "$SYMLINK_LOG"
test ! -e "$SYMLINK_HOME/.local/bin/ae-cli"

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

CONFIG_LOG="$TMP_ROOT/config.log"
env -i \
  HOME="$CONFIG_HOME" \
  PATH="$CONFIG_HOME/.local/bin:/usr/bin:/bin" \
  AE_CLI_INSTALL_TEST_OS=linux \
  AE_CLI_INSTALL_TEST_ARCH=amd64 \
  AE_CLI_INSTALL_RELEASE_API_URL="file://$TMP_ROOT/latest.json" \
  AE_CLI_INSTALL_RELEASE_DOWNLOAD_BASE="file://$RELEASE_ROOT" \
  AE_CLI_INSTALL_SERVER_URL="https://ae.example.com" \
  bash "$INSTALLER" "$LATEST_TAG" \
  >"$CONFIG_LOG" 2>&1

test -x "$CONFIG_HOME/.local/bin/ae-cli"
test -f "$CONFIG_HOME/.ae-cli/config.yaml"
grep -q 'url: "https://ae.example.com"' "$CONFIG_HOME/.ae-cli/config.yaml"

HOMELESS_LOG="$TMP_ROOT/homeless.log"
set +e
env -i \
  PATH="/usr/bin:/bin" \
  AE_CLI_INSTALL_TEST_OS=linux \
  AE_CLI_INSTALL_TEST_ARCH=amd64 \
  AE_CLI_INSTALL_RELEASE_API_URL="file://$TMP_ROOT/latest.json" \
  AE_CLI_INSTALL_RELEASE_DOWNLOAD_BASE="file://$RELEASE_ROOT" \
  bash "$INSTALLER" "$LATEST_TAG" \
  >"$HOMELESS_LOG" 2>&1
homeless_status=$?
set -e

test "$homeless_status" -ne 0
grep -q "HOME must be set to determine the installation directory" "$HOMELESS_LOG"
