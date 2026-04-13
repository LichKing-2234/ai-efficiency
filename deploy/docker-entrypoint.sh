#!/usr/bin/env sh
set -eu

STATE_DIR="${AE_DEPLOYMENT_STATE_DIR:-/var/lib/ai-efficiency}"
RUNTIME_DIR="${STATE_DIR}/runtime"
RUNTIME_BINARY="${AE_DEPLOYMENT_RUNTIME_BINARY_PATH:-${RUNTIME_DIR}/ai-efficiency-server}"
BOOTSTRAP_BINARY="/opt/ai-efficiency/bootstrap/ai-efficiency-server"

mkdir -p "$RUNTIME_DIR"

if [ ! -x "$RUNTIME_BINARY" ]; then
  cp "$BOOTSTRAP_BINARY" "$RUNTIME_BINARY"
  chmod 755 "$RUNTIME_BINARY"
fi

exec "$RUNTIME_BINARY"
