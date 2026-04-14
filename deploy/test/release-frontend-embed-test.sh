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
git init -q
git config user.name "release-embed-test"
git config user.email "release-embed-test@example.com"
git add .
git commit -qm "baseline"

npm --prefix frontend ci
bash deploy/prepare-release-frontend.sh

test -f backend/internal/web/dist/index.html
test -z "$(git status --short --untracked-files=all -- backend/internal/web/dist)"

(
  cd backend
  AE_ASSERT_EMBEDDED_FRONTEND=1 go test ./internal/web -run TestHasEmbeddedFrontendForReleaseBuilds -count=1
)
