#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

APP_DIR="$TMP_DIR/app"
SERVER_BINARY="$APP_DIR/ai-efficiency-server"
ENTRYPOINT="$TMP_DIR/docker-entrypoint.sh"

mkdir -p "$APP_DIR"

cat >"$SERVER_BINARY" <<'EOF'
#!/bin/sh
if [ "${1:-}" = "--version" ]; then
  echo "local-dev"
  exit 0
fi
echo "server"
EOF
chmod +x "$SERVER_BINARY"

sed "s|/app/ai-efficiency-server|$SERVER_BINARY|" "$ROOT_DIR/deploy/docker-entrypoint.sh" >"$ENTRYPOINT"

output="$(sh "$ENTRYPOINT")"

if [[ "$output" != "server" ]]; then
  echo "expected entrypoint to exec server binary, got: $output" >&2
  exit 1
fi
