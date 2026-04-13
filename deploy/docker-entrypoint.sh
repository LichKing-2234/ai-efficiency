#!/usr/bin/env sh
set -eu

STATE_DIR="${AE_DEPLOYMENT_STATE_DIR:-/var/lib/ai-efficiency}"
RUNTIME_DIR="${STATE_DIR}/runtime"
RUNTIME_BINARY="${RUNTIME_DIR}/ai-efficiency-server"
BOOTSTRAP_BINARY="/opt/ai-efficiency/bootstrap/ai-efficiency-server"

mkdir -p "$(dirname "$RUNTIME_BINARY")"

read_version() {
  if [ ! -x "$1" ]; then
    return 1
  fi
  "$1" --version 2>/dev/null | head -n 1 | tr -d '\r'
}

version_weight() {
  version="$(printf '%s' "$1" | sed 's/^v//')"
  core="${version%%-*}"
  suffix=""
  if [ "$core" != "$version" ]; then
    suffix="${version#*-}"
  fi

  OLD_IFS=${IFS}
  IFS=.
  set -- $core
  IFS=${OLD_IFS}
  major=${1:-0}
  minor=${2:-0}
  patch=${3:-0}

  prerelease_rank=1
  prerelease_num=0
  if [ -n "$suffix" ]; then
    prerelease_rank=0
    case "$suffix" in
      preview.*)
        prerelease_num="${suffix#preview.}"
        ;;
      *)
        return 1
        ;;
    esac
  fi

  printf '%08d%08d%08d%01d%08d\n' "$major" "$minor" "$patch" "$prerelease_rank" "$prerelease_num"
}

copy_bootstrap_binary() {
  cp "$BOOTSTRAP_BINARY" "$RUNTIME_BINARY"
  chmod 755 "$RUNTIME_BINARY"
}

if [ ! -x "$RUNTIME_BINARY" ]; then
  copy_bootstrap_binary
else
  runtime_version="$(read_version "$RUNTIME_BINARY" || true)"
  bootstrap_version="$(read_version "$BOOTSTRAP_BINARY" || true)"
  runtime_weight="$(version_weight "$runtime_version" || true)"
  bootstrap_weight="$(version_weight "$bootstrap_version" || true)"

  if [ -n "$runtime_weight" ] && [ -n "$bootstrap_weight" ] && [ "$bootstrap_weight" \> "$runtime_weight" ]; then
    copy_bootstrap_binary
  fi
fi

exec "$RUNTIME_BINARY"
