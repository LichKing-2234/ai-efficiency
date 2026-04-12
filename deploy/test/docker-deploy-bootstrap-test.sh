#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT
RELEASE_TAG="v0.1.0-test"

FIXTURE_DIR="$TMP_ROOT/fixture"
WORK_DIR="$TMP_ROOT/work"
RELEASE_DIR="$TMP_ROOT/$RELEASE_TAG"
mkdir -p "$FIXTURE_DIR/deploy" "$WORK_DIR" "$RELEASE_DIR"

cp "$ROOT_DIR/deploy/.env.example" "$FIXTURE_DIR/deploy/.env.example"
cp "$ROOT_DIR/deploy/docker-deploy.sh" "$FIXTURE_DIR/deploy/docker-deploy.sh"
cp "$ROOT_DIR/deploy/docker-compose.bootstrap.yml" "$FIXTURE_DIR/deploy/docker-compose.bootstrap.yml"
cp "$ROOT_DIR/deploy/init-db.sql" "$FIXTURE_DIR/deploy/init-db.sql"

tar -czf "$RELEASE_DIR/ai-efficiency-backend_0.1.0-test_linux_amd64.tar.gz" -C "$FIXTURE_DIR" deploy
cat > "$RELEASE_DIR/checksums.txt" <<'EOF'
checksums are currently downloaded but not validated by docker-deploy.sh
EOF

cp "$ROOT_DIR/deploy/docker-deploy.sh" "$WORK_DIR/docker-deploy.sh"

(
  cd "$WORK_DIR"
  TAG="$RELEASE_TAG" \
  ARCH=amd64 \
  RELEASE_DOWNLOAD_BASE=file://$TMP_ROOT \
  bash ./docker-deploy.sh
)

test -f "$WORK_DIR/docker-compose.yml"
test -f "$WORK_DIR/.env"
test -d "$WORK_DIR/deploy"
test -d "$WORK_DIR/data"
test -d "$WORK_DIR/postgres_data"
test -d "$WORK_DIR/redis_data"
cmp -s "$WORK_DIR/.env.example" "$WORK_DIR/deploy/.env.example"
cmp -s "$WORK_DIR/docker-compose.yml" "$WORK_DIR/deploy/docker-compose.bootstrap.yml"
grep -q "^AE_IMAGE_TAG=${RELEASE_TAG}$" "$WORK_DIR/.env"
grep -q "^AE_UPDATER_IMAGE_TAG=${RELEASE_TAG}$" "$WORK_DIR/.env"

validate_compose() {
  local compose_file="$1"
  local compose_dir
  compose_dir="$(cd "$(dirname "$compose_file")" && pwd)"
  local compose_name
  compose_name="$(basename "$compose_file")"
  if (
    cd "$compose_dir"
    docker compose -f "$compose_name" config >/dev/null 2>&1
  ); then
    return 0
  fi
  if command -v docker-compose >/dev/null 2>&1; then
    (
      cd "$compose_dir"
      docker-compose -f "$compose_name" config >/dev/null 2>&1
    )
    return $?
  fi
  echo "no compatible compose implementation available" >&2
  return 1
}

validate_compose "$WORK_DIR/docker-compose.yml"
