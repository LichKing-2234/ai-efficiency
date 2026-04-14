#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_DIR="$(mktemp -d)"
WORK_DIR="$TMP_DIR/repo"

cleanup() {
  rm -rf "$TMP_DIR"
}

trap cleanup EXIT

mkdir -p "$WORK_DIR"
tar -C "$ROOT_DIR" \
  --exclude=.git \
  --exclude=frontend/node_modules \
  --exclude=frontend/dist \
  --exclude=backend/internal/web/dist \
  -cf - . | tar -xf - -C "$WORK_DIR"

cd "$WORK_DIR"
npm --prefix frontend ci
bash deploy/prepare-release-frontend.sh

test -f backend/internal/web/dist/index.html

(
  cd backend
  AE_ASSERT_EMBEDDED_FRONTEND=1 go test ./internal/web -run TestHasEmbeddedFrontendForReleaseBuilds -count=1
)
