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
