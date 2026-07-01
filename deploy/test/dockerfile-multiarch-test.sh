#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DOCKERFILE="$ROOT_DIR/deploy/Dockerfile"

grep -Eq '^FROM --platform=\$BUILDPLATFORM node:20-alpine AS frontend-builder$' "$DOCKERFILE"
grep -Eq '^FROM --platform=\$BUILDPLATFORM golang:1\.24-alpine AS backend-builder$' "$DOCKERFILE"
grep -Eq '^ARG TARGETOS$' "$DOCKERFILE"
grep -Eq '^ARG TARGETARCH$' "$DOCKERFILE"
grep -q 'GOOS="${TARGETOS:-linux}"' "$DOCKERFILE"
grep -q 'GOARCH="${TARGETARCH:-$(go env GOARCH)}"' "$DOCKERFILE"
grep -q 'COPY --from=backend-builder /app/server /app/ai-efficiency-server' "$DOCKERFILE"
grep -q 'chmod +x /docker-entrypoint.sh /app/ai-efficiency-server' "$DOCKERFILE"
grep -q 'exec /app/ai-efficiency-server' "$ROOT_DIR/deploy/docker-entrypoint.sh"
