#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT
RELEASE_TAG="v0.1.0-test"
BAD_RELEASE_TAG="v0.1.0-bad"
MISSING_ASSET_TAG="v0.1.0-missing-asset"

FIXTURE_DIR="$TMP_ROOT/fixture"
WORK_DIR="$TMP_ROOT/work"
RELEASE_DIR="$TMP_ROOT/$RELEASE_TAG"
BAD_RELEASE_DIR="$TMP_ROOT/$BAD_RELEASE_TAG"
MISSING_ASSET_FIXTURE_DIR="$TMP_ROOT/missing-asset-fixture"
MISSING_ASSET_DIR="$TMP_ROOT/$MISSING_ASSET_TAG"
BAD_WORK_DIR="$TMP_ROOT/bad-work"
MISSING_ASSET_WORK_DIR="$TMP_ROOT/missing-asset-work"
INVALID_TAG_WORK_DIR="$TMP_ROOT/invalid-tag-work"
BOOTSTRAP_SCRIPT="$TMP_ROOT/docker-deploy.sh"
mkdir -p "$FIXTURE_DIR/deploy" "$WORK_DIR" "$RELEASE_DIR" "$BAD_RELEASE_DIR" "$BAD_WORK_DIR" \
  "$MISSING_ASSET_FIXTURE_DIR/deploy" "$MISSING_ASSET_DIR" "$MISSING_ASSET_WORK_DIR" "$INVALID_TAG_WORK_DIR"

cp "$ROOT_DIR/deploy/.env.example" "$FIXTURE_DIR/deploy/.env.example"
cp "$ROOT_DIR/deploy/docker-deploy.sh" "$FIXTURE_DIR/deploy/docker-deploy.sh"
cp "$ROOT_DIR/deploy/docker-compose.bootstrap.yml" "$FIXTURE_DIR/deploy/docker-compose.bootstrap.yml"
cp "$ROOT_DIR/deploy/init-db.sql" "$FIXTURE_DIR/deploy/init-db.sql"

cp "$ROOT_DIR/deploy/.env.example" "$MISSING_ASSET_FIXTURE_DIR/deploy/.env.example"
cp "$ROOT_DIR/deploy/docker-deploy.sh" "$MISSING_ASSET_FIXTURE_DIR/deploy/docker-deploy.sh"
cp "$ROOT_DIR/deploy/init-db.sql" "$MISSING_ASSET_FIXTURE_DIR/deploy/init-db.sql"

tar -czf "$RELEASE_DIR/ai-efficiency-backend_0.1.0-test_linux_amd64.tar.gz" -C "$FIXTURE_DIR" deploy
RELEASE_ARCHIVE="$RELEASE_DIR/ai-efficiency-backend_0.1.0-test_linux_amd64.tar.gz"
RELEASE_SHA256="$(openssl dgst -sha256 "$RELEASE_ARCHIVE" | awk '{print $NF}')"
printf '%s  %s\n' "$RELEASE_SHA256" "$(basename "$RELEASE_ARCHIVE")" > "$RELEASE_DIR/checksums.txt"

BAD_RELEASE_ARCHIVE="$BAD_RELEASE_DIR/ai-efficiency-backend_0.1.0-bad_linux_amd64.tar.gz"
cp "$RELEASE_ARCHIVE" "$BAD_RELEASE_ARCHIVE"
printf '%064d  %s\n' 0 "$(basename "$BAD_RELEASE_ARCHIVE")" > "$BAD_RELEASE_DIR/checksums.txt"

MISSING_ASSET_ARCHIVE="$MISSING_ASSET_DIR/ai-efficiency-backend_0.1.0-missing-asset_linux_amd64.tar.gz"
tar -czf "$MISSING_ASSET_ARCHIVE" -C "$MISSING_ASSET_FIXTURE_DIR" deploy
MISSING_ASSET_SHA256="$(openssl dgst -sha256 "$MISSING_ASSET_ARCHIVE" | awk '{print $NF}')"
printf '%s  %s\n' "$MISSING_ASSET_SHA256" "$(basename "$MISSING_ASSET_ARCHIVE")" > "$MISSING_ASSET_DIR/checksums.txt"

printf '{}\n' > "$TMP_ROOT/latest-invalid.json"

cp "$ROOT_DIR/deploy/docker-deploy.sh" "$BOOTSTRAP_SCRIPT"

(
  cd "$WORK_DIR"
  TAG="$RELEASE_TAG" \
  ARCH=amd64 \
  RELEASE_DOWNLOAD_BASE=file://$TMP_ROOT \
  bash "$BOOTSTRAP_SCRIPT"
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
if command -v docker-compose >/dev/null 2>&1; then
  docker-compose -f "$WORK_DIR/docker-compose.yml" config | grep -F 'http://localhost:8081/api/v1/auth/me' >/dev/null
elif docker compose version >/dev/null 2>&1; then
  docker compose -f "$WORK_DIR/docker-compose.yml" config | grep -F 'http://localhost:8081/api/v1/auth/me' >/dev/null
fi

set +e
(
  cd "$BAD_WORK_DIR"
  TAG="$BAD_RELEASE_TAG" \
  ARCH=amd64 \
  RELEASE_DOWNLOAD_BASE=file://$TMP_ROOT \
  bash "$BOOTSTRAP_SCRIPT"
) >"$TMP_ROOT/bad-checksum.log" 2>&1
bad_status=$?
set -e

test "$bad_status" -ne 0
grep -q "checksum verification failed" "$TMP_ROOT/bad-checksum.log"
test ! -e "$BAD_WORK_DIR/docker-compose.yml"
test ! -e "$BAD_WORK_DIR/.env"
test ! -e "$BAD_WORK_DIR/deploy"

set +e
(
  cd "$MISSING_ASSET_WORK_DIR"
  TAG="$MISSING_ASSET_TAG" \
  ARCH=amd64 \
  RELEASE_DOWNLOAD_BASE=file://$TMP_ROOT \
  bash "$BOOTSTRAP_SCRIPT"
) >"$TMP_ROOT/missing-asset.log" 2>&1
missing_asset_status=$?
set -e

test "$missing_asset_status" -ne 0
grep -q "release bundle missing required asset: deploy/docker-compose.bootstrap.yml" "$TMP_ROOT/missing-asset.log"
test ! -e "$MISSING_ASSET_WORK_DIR/docker-compose.yml"
test ! -e "$MISSING_ASSET_WORK_DIR/.env"
test ! -e "$MISSING_ASSET_WORK_DIR/deploy"

set +e
(
  cd "$INVALID_TAG_WORK_DIR"
  ARCH=amd64 \
  RELEASE_API_URL="file://$TMP_ROOT/latest-invalid.json" \
  RELEASE_DOWNLOAD_BASE=file://$TMP_ROOT \
  bash "$BOOTSTRAP_SCRIPT"
) >"$TMP_ROOT/invalid-tag.log" 2>&1
invalid_tag_status=$?
set -e

test "$invalid_tag_status" -ne 0
grep -q "failed to resolve release tag" "$TMP_ROOT/invalid-tag.log"

REPO_FIXTURE="$TMP_ROOT/repo-fixture"
UNRELATED_CWD="$TMP_ROOT/unrelated-cwd"
FAKE_BIN="$TMP_ROOT/fake-bin"
mkdir -p "$REPO_FIXTURE/deploy" "$UNRELATED_CWD" "$FAKE_BIN"

cp "$ROOT_DIR/deploy/docker-deploy.sh" "$REPO_FIXTURE/deploy/docker-deploy.sh"
cp "$ROOT_DIR/deploy/.env.example" "$REPO_FIXTURE/deploy/.env.example"
cp "$ROOT_DIR/deploy/docker-compose.yml" "$REPO_FIXTURE/deploy/docker-compose.yml"

REAL_CURL="$(command -v curl)"
cat >"$FAKE_BIN/curl" <<EOF
#!/usr/bin/env bash
set -euo pipefail
for arg in "\$@"; do
  if [[ "\$arg" == "-o" ]]; then
    exec "$REAL_CURL" "\$@"
  fi
done
exit 0
EOF
chmod +x "$FAKE_BIN/curl"

cat >"$FAKE_BIN/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "compose" ]]; then
  exit 0
fi
exit 0
EOF
chmod +x "$FAKE_BIN/docker"

(
  cd "$UNRELATED_CWD"
  PATH="$FAKE_BIN:$PATH" \
  TAG="$RELEASE_TAG" \
  ARCH=amd64 \
  RELEASE_DOWNLOAD_BASE=file://$TMP_ROOT \
  bash "$REPO_FIXTURE/deploy/docker-deploy.sh"
) >"$TMP_ROOT/absolute-path.log" 2>&1

test ! -e "$UNRELATED_CWD/docker-compose.yml"
test ! -e "$UNRELATED_CWD/.env"
test ! -e "$UNRELATED_CWD/deploy"
test -f "$REPO_FIXTURE/deploy/.env"
