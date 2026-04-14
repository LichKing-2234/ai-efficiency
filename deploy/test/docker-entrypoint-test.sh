#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

BOOTSTRAP_BINARY="$TMP_DIR/bootstrap/ai-efficiency-server"
RUNTIME_BINARY="$TMP_DIR/runtime/ai-efficiency-server"

mkdir -p "$(dirname "$BOOTSTRAP_BINARY")" "$(dirname "$RUNTIME_BINARY")"

cat >"$BOOTSTRAP_BINARY" <<'EOF'
#!/bin/sh
if [ "${1:-}" = "--version" ]; then
  echo "local-dev"
  exit 0
fi
echo "bootstrap"
EOF
chmod +x "$BOOTSTRAP_BINARY"

cat >"$RUNTIME_BINARY" <<'EOF'
#!/bin/sh
if [ "${1:-}" = "--version" ]; then
  echo "local-dev"
  exit 0
fi
echo "runtime"
EOF
chmod +x "$RUNTIME_BINARY"

without_force="$(
  AE_DEPLOYMENT_BOOTSTRAP_BINARY_PATH="$BOOTSTRAP_BINARY" \
  AE_DEPLOYMENT_RUNTIME_BINARY_PATH="$RUNTIME_BINARY" \
  sh "$ROOT_DIR/deploy/docker-entrypoint.sh"
)"

if [[ "$without_force" != "runtime" ]]; then
  echo "expected existing runtime binary to win without force flag, got: $without_force" >&2
  exit 1
fi

with_force="$(
  AE_DEPLOYMENT_BOOTSTRAP_BINARY_PATH="$BOOTSTRAP_BINARY" \
  AE_DEPLOYMENT_RUNTIME_BINARY_PATH="$RUNTIME_BINARY" \
  AE_DEPLOYMENT_FORCE_BOOTSTRAP=true \
  sh "$ROOT_DIR/deploy/docker-entrypoint.sh"
)"

if [[ "$with_force" != "bootstrap" ]]; then
  echo "expected bootstrap binary to replace runtime when force flag is set, got: $with_force" >&2
  exit 1
fi
