#!/usr/bin/env bash
set -euo pipefail

GITHUB_REPO="LichKing-2234/ai-efficiency"

if [[ -z "${HOME:-}" ]]; then
  echo "HOME must be set to determine the installation directory" >&2
  exit 1
fi

INSTALL_DIR="${HOME}/.local/bin"
TARGET_PATH="${INSTALL_DIR}/ae-cli"
CONFIG_DIR="${HOME}/.ae-cli"
CONFIG_PATH="${CONFIG_DIR}/config.yaml"
RELEASE_API_URL="${AE_CLI_INSTALL_RELEASE_API_URL:-https://api.github.com/repos/${GITHUB_REPO}/releases/latest}"
RELEASE_DOWNLOAD_BASE="${AE_CLI_INSTALL_RELEASE_DOWNLOAD_BASE:-https://github.com/${GITHUB_REPO}/releases/download}"
TMP_DIR=""
TEMP_TARGET=""
CONFIG_SERVER_URL="${AE_CLI_INSTALL_SERVER_URL:-https://ai-efficiency.la3.agoralab.co}"
CONFIG_SERVER_URL_EXPLICIT=0
if [[ -n "${AE_CLI_INSTALL_SERVER_URL+x}" ]]; then
  CONFIG_SERVER_URL_EXPLICIT=1
fi
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

github_release_proxy_help() {
  cat >&2 <<'EOF'
ae-cli downloads releases from GitHub Releases. This request failed before the installer could resolve or download the release.
If your network cannot reach GitHub directly, configure a proxy and rerun, for example:
  export HTTPS_PROXY=http://127.0.0.1:7890
  export HTTP_PROXY=http://127.0.0.1:7890
EOF
}

trim_whitespace() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

validate_server_url() {
  local value="$1"
  [[ "$value" =~ ^https?://[^[:space:]]+$ ]]
}

existing_config_path() {
  if [[ -f "${CONFIG_DIR}/config.yaml" ]]; then
    printf '%s\n' "${CONFIG_DIR}/config.yaml"
    return 0
  fi
  if [[ -f "${CONFIG_DIR}/config.yml" ]]; then
    printf '%s\n' "${CONFIG_DIR}/config.yml"
    return 0
  fi
  return 1
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
    amd64|arm64) ;;
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
  local release_json=""

  if ! release_json="$(curl -fsSL "$RELEASE_API_URL")"; then
    github_release_proxy_help
    exit 1
  fi

  tag="$(printf '%s\n' "$release_json" | awk -F'"' '/"tag_name"/ { print $4; exit }')"
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

  if ! curl -fsSL "${base}/${archive}" -o "${TMP_DIR}/${archive}"; then
    github_release_proxy_help
    exit 1
  fi
  if ! curl -fsSL "${base}/checksums.txt" -o "${TMP_DIR}/checksums.txt"; then
    github_release_proxy_help
    exit 1
  fi

  expected="$(grep -F "  ${archive}" "${TMP_DIR}/checksums.txt" | awk '{print $1}' | head -1)"
  actual="$(sha256_file "${TMP_DIR}/${archive}")"
  if [[ -z "$expected" ]]; then
    echo "missing checksum for ${archive}" >&2
    exit 1
  fi
  if [[ "$expected" != "$actual" ]]; then
    echo "checksum verification failed for ${archive}" >&2
    exit 1
  fi

  if ! tar -tzf "${TMP_DIR}/${archive}" | grep -Fx "ae-cli" >/dev/null 2>&1; then
    echo "release archive missing ae-cli" >&2
    exit 1
  fi

  tar -xzf "${TMP_DIR}/${archive}" -C "${TMP_DIR}" ae-cli
  if [[ -L "${TMP_DIR}/ae-cli" ]]; then
    echo "release archive ae-cli must be a regular file" >&2
    exit 1
  fi
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

prompt_server_url() {
  if [[ -n "$(existing_config_path 2>/dev/null || true)" ]]; then
    return 0
  fi

  CONFIG_SERVER_URL="$(trim_whitespace "$CONFIG_SERVER_URL")"
  if [[ -n "$CONFIG_SERVER_URL" ]]; then
    if ! validate_server_url "$CONFIG_SERVER_URL"; then
      echo "invalid AE_CLI_INSTALL_SERVER_URL: must start with http:// or https://" >&2
      exit 1
    fi
    return 0
  fi

  if [[ ! -r /dev/tty || ! -w /dev/tty ]]; then
    return 0
  fi

  if [[ ! -t 1 ]]; then
    return 0
  fi

  if ! exec 9<>/dev/tty; then
    return 0
  fi

  while true; do
    printf 'AI Efficiency backend URL (optional, e.g. https://ae.example.com): ' >&9
    IFS= read -r CONFIG_SERVER_URL <&9 || CONFIG_SERVER_URL=""
    CONFIG_SERVER_URL="$(trim_whitespace "$CONFIG_SERVER_URL")"
    if [[ -z "$CONFIG_SERVER_URL" ]]; then
      exec 9>&-
      exec 9<&-
      return 0
    fi
    if validate_server_url "$CONFIG_SERVER_URL"; then
      exec 9>&-
      exec 9<&-
      return 0
    fi
    echo "Please enter a full http:// or https:// URL, or leave blank to skip." >&9
  done
}

write_cli_config() {
  local existing=""

  CONFIG_SERVER_URL="$(trim_whitespace "$CONFIG_SERVER_URL")"
  if [[ -n "$CONFIG_SERVER_URL" ]] && ! validate_server_url "$CONFIG_SERVER_URL"; then
    echo "invalid AE_CLI_INSTALL_SERVER_URL: must start with http:// or https://" >&2
    exit 1
  fi

  if existing="$(existing_config_path 2>/dev/null || true)" && [[ -n "$existing" ]]; then
    if [[ "$CONFIG_SERVER_URL_EXPLICIT" -eq 1 && -n "$CONFIG_SERVER_URL" ]]; then
      update_existing_cli_config "$existing"
      echo "Updated CLI config at ${existing}"
      return 0
    fi
    echo "Using existing CLI config at ${existing}"
    return 0
  fi

  if [[ -z "$CONFIG_SERVER_URL" ]]; then
    echo "No CLI config written. Configure the backend URL later in ${CONFIG_PATH} or pass --server."
    return 0
  fi

  mkdir -p "$CONFIG_DIR"
  cat >"$CONFIG_PATH" <<EOF
server:
  url: "${CONFIG_SERVER_URL}"
EOF
  chmod 0600 "$CONFIG_PATH"
  echo "Wrote CLI config to ${CONFIG_PATH}"
}

refresh_managed_hooks() {
  if [[ -x "$TARGET_PATH" ]]; then
    "$TARGET_PATH" hooks refresh-installations >/dev/null 2>&1 || {
      echo "Warning: installed ae-cli but failed to refresh managed hook scripts." >&2
    }
  fi
}

update_existing_cli_config() {
  local existing="$1"
  local tmp="${existing}.tmp.$$"

  awk -v new_url="$CONFIG_SERVER_URL" '
    BEGIN {
      in_server = 0
      saw_server = 0
      wrote_url = 0
    }
    function emit_url(indent) {
      print indent "url: \"" new_url "\""
      wrote_url = 1
    }
    /^server:[[:space:]]*$/ {
      if (in_server && !wrote_url) {
        emit_url("  ")
      }
      in_server = 1
      saw_server = 1
      wrote_url = 0
      print
      next
    }
    in_server && /^[^[:space:]][^:]*:/ {
      if (!wrote_url) {
        emit_url("  ")
      }
      in_server = 0
    }
    in_server && /^[[:space:]]*url:[[:space:]]*/ {
      match($0, /^[[:space:]]*/)
      emit_url(substr($0, RSTART, RLENGTH))
      next
    }
    {
      print
    }
    END {
      if (in_server && !wrote_url) {
        emit_url("  ")
      }
      if (!saw_server) {
        print ""
        print "server:"
        print "  url: \"" new_url "\""
      }
    }
  ' "$existing" >"$tmp"
  mv "$tmp" "$existing"
  chmod 0600 "$existing"
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
  prompt_server_url
  write_cli_config
  refresh_managed_hooks
  echo "Installed ae-cli ${tag} to ${TARGET_PATH}"

  if ! path_contains_install_dir; then
    echo "Warning: ${INSTALL_DIR} is not in PATH."
    echo "Add it to your shell profile, for example:"
    echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
  fi
}

main "$@"
