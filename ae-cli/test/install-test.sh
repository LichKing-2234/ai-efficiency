#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_ROOT="$(mktemp -d)"
SERVER_PIDS=()

cleanup() {
  for pid in "${SERVER_PIDS[@]}"; do
    kill "$pid" >/dev/null 2>&1 || true
    wait "$pid" >/dev/null 2>&1 || true
  done
  rm -rf "$TMP_ROOT"
}

trap cleanup EXIT

INSTALLER="$TMP_ROOT/install.sh"
RELEASE_ROOT="$TMP_ROOT/releases"
LATEST_TAG="ae-cli/v0.2.0-preview.1"
PLATFORM_LATEST_TAG="v0.1.0-preview.42"
BRIDGE_TAG="v0.2.0-cli.1"
PINNED_TAG="ae-cli/v0.2.1-preview.1"
LEGACY_PINNED_TAG="v0.2.1-preview.1"
BARE_PINNED_TAG="0.2.1-preview.1"
MISSING_V_CLI_PINNED_TAG="ae-cli/0.2.1-preview.1"
DOT_SUFFIX_CLI_PINNED_TAG="ae-cli/v0.2.1.preview.1"
BAD_CHECKSUM_TAG="ae-cli/v0.2.2-bad"
MISSING_BINARY_TAG="ae-cli/v0.2.3-missing-binary"
PATH_WARNING_TAG="ae-cli/v0.2.4-path-warning"
SYMLINK_TAG="ae-cli/v0.2.5-symlink"
POST_INSTALL_WARNING_TAG="ae-cli/v0.2.6-post-install-warning"

release_version_from_tag() {
  local tag="$1"
  tag="${tag#ae-cli/}"
  tag="${tag#v}"
  printf '%s' "$tag"
}

cp "$ROOT_DIR/ae-cli/install.sh" "$INSTALLER"
chmod +x "$INSTALLER"
test -f "$ROOT_DIR/ae-cli/install.ps1"
grep -q "AE_CLI_INSTALL_SERVER_URL" "$ROOT_DIR/ae-cli/install.ps1"
grep -q "HTTPS_PROXY" "$ROOT_DIR/ae-cli/install.ps1"
grep -q "v0.2.0-cli.1" "$ROOT_DIR/ae-cli/install.ps1"

make_cli_archive() {
  local tag="$1"
  local version
  version="$(release_version_from_tag "$tag")"
  local stage_dir="$TMP_ROOT/stage-$version"
  local release_dir="$RELEASE_ROOT/$tag"
  local archive="ae-cli_${version}_linux_amd64.tar.gz"
  local sha=""

  rm -rf "$stage_dir"
  mkdir -p "$stage_dir" "$release_dir"
  cat >"$stage_dir/ae-cli" <<EOF
#!/usr/bin/env bash
set -euo pipefail
if [[ "\${1:-}" == "hooks" && "\${2:-}" == "refresh-installations" ]]; then
  printf '%s\n' "${tag}" >> "\${HOME}/.ae-cli/refresh-installations.log"
  exit 0
fi
if [[ "\${1:-}" == "update" && "\${2:-}" == "post-install" ]]; then
  mkdir -p "\${HOME}/.ae-cli"
  printf '%s\n' "${tag}" >> "\${HOME}/.ae-cli/post-install.log"
  if [[ "${tag}" == "${POST_INSTALL_WARNING_TAG}" ]]; then
    exit 1
  fi
  exit 0
fi
echo "ae-cli ${tag}"
EOF
  chmod +x "$stage_dir/ae-cli"

  tar -czf "$release_dir/$archive" -C "$stage_dir" ae-cli
  sha="$(openssl dgst -sha256 "$release_dir/$archive" | awk '{print $NF}')"
  printf '%s  %s\n' "$sha" "$archive" >"$release_dir/checksums.txt"
}

make_bad_checksum_archive() {
  local tag="$1"
  local version
  version="$(release_version_from_tag "$tag")"
  local release_dir="$RELEASE_ROOT/$tag"
  local archive="ae-cli_${version}_linux_amd64.tar.gz"

  make_cli_archive "$tag"
  printf '%064d  %s\n' 0 "$archive" >"$release_dir/checksums.txt"
}

make_missing_binary_archive() {
  local tag="$1"
  local version
  version="$(release_version_from_tag "$tag")"
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
  local version
  version="$(release_version_from_tag "$tag")"
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
make_cli_archive "$BRIDGE_TAG"
make_cli_archive "$PINNED_TAG"
make_cli_archive "$LEGACY_PINNED_TAG"
make_cli_archive "$BARE_PINNED_TAG"
make_cli_archive "$MISSING_V_CLI_PINNED_TAG"
make_cli_archive "$DOT_SUFFIX_CLI_PINNED_TAG"
make_bad_checksum_archive "$BAD_CHECKSUM_TAG"
make_missing_binary_archive "$MISSING_BINARY_TAG"
make_cli_archive "$PATH_WARNING_TAG"
make_symlink_archive "$SYMLINK_TAG"
make_cli_archive "$POST_INSTALL_WARNING_TAG"
printf '[{"tag_name":"%s"},{"tag_name":"%s"}]\n' "$PLATFORM_LATEST_TAG" "$LATEST_TAG" >"$TMP_ROOT/latest.json"

PAGINATED_API_URL_FILE="$TMP_ROOT/paginated-api-url.txt"
ruby - "$PAGINATED_API_URL_FILE" "$PLATFORM_LATEST_TAG" "$LATEST_TAG" <<'RUBY' &
require "socket"

url_file, platform_tag, latest_tag = ARGV
server = TCPServer.new("127.0.0.1", 0)
port = server.addr[1]
File.write(url_file, "http://127.0.0.1:#{port}/page1")

def write_response(client, status, headers, body)
  client.write "HTTP/1.1 #{status}\r\n"
  headers.each { |key, value| client.write "#{key}: #{value}\r\n" }
  client.write "Content-Length: #{body.bytesize}\r\n"
  client.write "Connection: close\r\n"
  client.write "\r\n"
  client.write body
end

loop do
  client = server.accept
  request_line = client.gets
  path = request_line.to_s.split[1]
  loop do
    line = client.gets
    break if line.nil? || line == "\r\n"
  end

  case path
  when "/page1"
    body = %([{"tag_name":"#{platform_tag}"}])
    write_response(
      client,
      "200 OK",
      {
        "Content-Type" => "application/json",
        "Link" => "<http://127.0.0.1:#{port}/page2>; rel=\"next\""
      },
      body
    )
  when "/page2"
    body = %([{"tag_name":"#{latest_tag}"}])
    write_response(client, "200 OK", {"Content-Type" => "application/json"}, body)
  else
    write_response(client, "404 Not Found", {"Content-Type" => "text/plain"}, "not found")
  end
  client.close
end
RUBY
SERVER_PIDS+=("$!")

for _ in {1..50}; do
  if [[ -s "$PAGINATED_API_URL_FILE" ]]; then
    break
  fi
  sleep 0.1
done
test -s "$PAGINATED_API_URL_FILE"

LATEST_HOME="$TMP_ROOT/home-latest"
PINNED_HOME="$TMP_ROOT/home-pinned"
BRIDGE_HOME="$TMP_ROOT/home-bridge"
BAD_HOME="$TMP_ROOT/home-bad"
MISSING_HOME="$TMP_ROOT/home-missing"
PATH_WARNING_HOME="$TMP_ROOT/home-path-warning"
CONFIG_HOME="$TMP_ROOT/home-config"
EXISTING_CONFIG_HOME="$TMP_ROOT/home-existing-config"
NETWORK_HOME="$TMP_ROOT/home-network"
mkdir -p "$LATEST_HOME" "$PINNED_HOME" "$BRIDGE_HOME" "$BAD_HOME" "$MISSING_HOME" "$PATH_WARNING_HOME" "$NETWORK_HOME"

LATEST_LOG="$TMP_ROOT/latest.log"
run_installer \
  "$LATEST_HOME" \
  "$LATEST_HOME/.local/bin:/usr/bin:/bin" \
  "file://$TMP_ROOT/latest.json" \
  >"$LATEST_LOG" 2>&1

test -x "$LATEST_HOME/.local/bin/ae-cli"
"$LATEST_HOME/.local/bin/ae-cli" | grep -q "ae-cli ${LATEST_TAG}"
grep -q "${LATEST_TAG}" "$LATEST_HOME/.ae-cli/refresh-installations.log"
grep -q "${LATEST_TAG}" "$LATEST_HOME/.ae-cli/post-install.log"
test -f "$LATEST_HOME/.ae-cli/config.yaml"
grep -q 'url: "https://ai-efficiency.la3.agoralab.co"' "$LATEST_HOME/.ae-cli/config.yaml"
grep -q "Installing ae-cli ${LATEST_TAG}" "$LATEST_LOG"
grep -q "Installed ae-cli ${LATEST_TAG} to $LATEST_HOME/.local/bin/ae-cli" "$LATEST_LOG"
! grep -q "is not in PATH" "$LATEST_LOG"

PAGINATED_HOME="$TMP_ROOT/home-paginated"
mkdir -p "$PAGINATED_HOME"
PAGINATED_LOG="$TMP_ROOT/paginated.log"
run_installer \
  "$PAGINATED_HOME" \
  "$PAGINATED_HOME/.local/bin:/usr/bin:/bin" \
  "$(cat "$PAGINATED_API_URL_FILE")" \
  >"$PAGINATED_LOG" 2>&1

test -x "$PAGINATED_HOME/.local/bin/ae-cli"
"$PAGINATED_HOME/.local/bin/ae-cli" | grep -q "ae-cli ${LATEST_TAG}"
grep -q "Installed ae-cli ${LATEST_TAG} to $PAGINATED_HOME/.local/bin/ae-cli" "$PAGINATED_LOG"

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

BRIDGE_LOG="$TMP_ROOT/bridge.log"
run_installer \
  "$BRIDGE_HOME" \
  "$BRIDGE_HOME/.local/bin:/usr/bin:/bin" \
  "file://$TMP_ROOT/latest.json" \
  "$BRIDGE_TAG" \
  >"$BRIDGE_LOG" 2>&1

test -x "$BRIDGE_HOME/.local/bin/ae-cli"
"$BRIDGE_HOME/.local/bin/ae-cli" | grep -q "ae-cli ${BRIDGE_TAG}"
grep -q "Installed ae-cli ${BRIDGE_TAG} to $BRIDGE_HOME/.local/bin/ae-cli" "$BRIDGE_LOG"

assert_invalid_pinned_tag() {
  local tag="$1"
  local home_dir="$2"
  local log_file="$3"
  local status

  mkdir -p "$home_dir"
  set +e
  run_installer \
    "$home_dir" \
    "$home_dir/.local/bin:/usr/bin:/bin" \
    "file://$TMP_ROOT/latest.json" \
    "$tag" \
    >"$log_file" 2>&1
  status=$?
  set -e

  test "$status" -ne 0
  grep -q "release tag must match ae-cli/vX.Y.Z" "$log_file"
  test ! -e "$home_dir/.local/bin/ae-cli"
}

assert_invalid_pinned_tag "$LEGACY_PINNED_TAG" "$TMP_ROOT/home-legacy-pinned" "$TMP_ROOT/legacy-pinned.log"
assert_invalid_pinned_tag "$BARE_PINNED_TAG" "$TMP_ROOT/home-bare-pinned" "$TMP_ROOT/bare-pinned.log"
assert_invalid_pinned_tag "$MISSING_V_CLI_PINNED_TAG" "$TMP_ROOT/home-missing-v-cli-pinned" "$TMP_ROOT/missing-v-cli-pinned.log"
assert_invalid_pinned_tag "$DOT_SUFFIX_CLI_PINNED_TAG" "$TMP_ROOT/home-dot-suffix-cli-pinned" "$TMP_ROOT/dot-suffix-cli-pinned.log"

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

NETWORK_LOG="$TMP_ROOT/network.log"
set +e
run_installer \
  "$NETWORK_HOME" \
  "$NETWORK_HOME/.local/bin:/usr/bin:/bin" \
  "file://$TMP_ROOT/missing-latest.json" \
  >"$NETWORK_LOG" 2>&1
network_status=$?
set -e

test "$network_status" -ne 0
grep -q "ae-cli downloads releases from GitHub Releases" "$NETWORK_LOG"
grep -q "HTTPS_PROXY" "$NETWORK_LOG"
test ! -e "$NETWORK_HOME/.local/bin/ae-cli"

POST_INSTALL_WARNING_HOME="$TMP_ROOT/home-post-install-warning"
mkdir -p "$POST_INSTALL_WARNING_HOME"
POST_INSTALL_WARNING_LOG="$TMP_ROOT/post-install-warning.log"
run_installer \
  "$POST_INSTALL_WARNING_HOME" \
  "$POST_INSTALL_WARNING_HOME/.local/bin:/usr/bin:/bin" \
  "file://$TMP_ROOT/latest.json" \
  "$POST_INSTALL_WARNING_TAG" \
  >"$POST_INSTALL_WARNING_LOG" 2>&1

test -x "$POST_INSTALL_WARNING_HOME/.local/bin/ae-cli"
grep -q "$POST_INSTALL_WARNING_TAG" "$POST_INSTALL_WARNING_HOME/.ae-cli/post-install.log"
grep -q "legacy AE Codex OTLP cleanup did not complete" "$POST_INSTALL_WARNING_LOG"
grep -q "Installed ae-cli ${POST_INSTALL_WARNING_TAG}" "$POST_INSTALL_WARNING_LOG"

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

mkdir -p "$EXISTING_CONFIG_HOME/.ae-cli"
cat >"$EXISTING_CONFIG_HOME/.ae-cli/config.yaml" <<'EOF'
server:
  url: "https://old.example.com"
  token: "keep-token"
tools:
  codex:
    command: "codex"
EOF

EXISTING_CONFIG_LOG="$TMP_ROOT/existing-config.log"
env -i \
  HOME="$EXISTING_CONFIG_HOME" \
  PATH="$EXISTING_CONFIG_HOME/.local/bin:/usr/bin:/bin" \
  AE_CLI_INSTALL_TEST_OS=linux \
  AE_CLI_INSTALL_TEST_ARCH=amd64 \
  AE_CLI_INSTALL_RELEASE_API_URL="file://$TMP_ROOT/latest.json" \
  AE_CLI_INSTALL_RELEASE_DOWNLOAD_BASE="file://$RELEASE_ROOT" \
  AE_CLI_INSTALL_SERVER_URL="http://localhost:18081" \
  bash "$INSTALLER" "$LATEST_TAG" \
  >"$EXISTING_CONFIG_LOG" 2>&1

test -x "$EXISTING_CONFIG_HOME/.local/bin/ae-cli"
grep -q 'url: "http://localhost:18081"' "$EXISTING_CONFIG_HOME/.ae-cli/config.yaml"
grep -q 'token: "keep-token"' "$EXISTING_CONFIG_HOME/.ae-cli/config.yaml"
grep -q 'command: "codex"' "$EXISTING_CONFIG_HOME/.ae-cli/config.yaml"
grep -q "Updated CLI config at $EXISTING_CONFIG_HOME/.ae-cli/config.yaml" "$EXISTING_CONFIG_LOG"

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
