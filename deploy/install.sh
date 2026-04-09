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
