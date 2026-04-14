#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FRONTEND_DIR="$ROOT_DIR/frontend"
TARGET_DIR="$ROOT_DIR/backend/internal/web/dist"

if ! command -v npm >/dev/null 2>&1; then
  echo "npm is required to build the frontend release bundle" >&2
  exit 1
fi

if [[ ! -f "$FRONTEND_DIR/package.json" ]]; then
  echo "frontend package.json not found: $FRONTEND_DIR/package.json" >&2
  exit 1
fi

npm --prefix "$FRONTEND_DIR" run build

mkdir -p "$TARGET_DIR"
find "$TARGET_DIR" -mindepth 1 -maxdepth 1 ! -name '.gitkeep' -exec rm -rf {} +
cp -R "$FRONTEND_DIR/dist/." "$TARGET_DIR/"
