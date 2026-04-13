#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

REPO_FIXTURE="$TMP_ROOT/repo-fixture"
FAKE_BIN="$TMP_ROOT/fake-bin"
mkdir -p "$REPO_FIXTURE/deploy" "$FAKE_BIN"

cp "$ROOT_DIR/deploy/docker-deploy.sh" "$REPO_FIXTURE/deploy/docker-deploy.sh"
cp "$ROOT_DIR/deploy/.env.example" "$REPO_FIXTURE/deploy/.env.example"
cp "$ROOT_DIR/deploy/docker-compose.yml" "$REPO_FIXTURE/deploy/docker-compose.yml"

cat >"$REPO_FIXTURE/deploy/.env" <<'EOF'
AE_SERVER_PORT=8081
AE_SERVER_FRONTEND_URL=http://localhost:8081
POSTGRES_USER=ai_efficiency
POSTGRES_PASSWORD=
POSTGRES_DB=ai_efficiency
REDIS_PASSWORD=
AE_RELAY_URL=http://127.0.0.1/relay
AE_RELAY_API_KEY=
AE_RELAY_ADMIN_API_KEY=
AE_AUTH_JWT_SECRET=
AE_ENCRYPTION_KEY=
EOF

cat >"$FAKE_BIN/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "compose" ]]; then
  exit 0
fi
exit 0
EOF
chmod +x "$FAKE_BIN/docker"

cat >"$FAKE_BIN/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
exit 0
EOF
chmod +x "$FAKE_BIN/curl"

PREFLIGHT_LOG="$TMP_ROOT/preflight.log"
set +e
(
  cd "$REPO_FIXTURE"
  env -i PATH="$FAKE_BIN:$PATH" \
    bash deploy/docker-deploy.sh
) >"$PREFLIGHT_LOG" 2>&1
preflight_status=$?
set -e
if [[ "$preflight_status" -ne 0 ]]; then
  EXPECTED_REASON="missing required env var: COMPOSE_PROJECT_NAME"
  if ! grep -q "$EXPECTED_REASON" "$PREFLIGHT_LOG"; then
    cat "$PREFLIGHT_LOG" >&2
    echo "preflight did not fail for the expected hidden-default reason" >&2
    exit 1
  fi
fi

for secret in AE_AUTH_JWT_SECRET AE_ENCRYPTION_KEY POSTGRES_PASSWORD; do
  count="$(grep -c "^${secret}=" "$REPO_FIXTURE/deploy/.env")"
  if [[ "$count" -ne 1 ]]; then
    echo "expected exactly one ${secret} entry, found ${count}" >&2
    exit 1
  fi
  value="$(grep "^${secret}=" "$REPO_FIXTURE/deploy/.env" | cut -d'=' -f2-)"
  if [[ -z "$value" ]]; then
    echo "generated ${secret} is empty" >&2
    exit 1
  fi
done

for hidden in COMPOSE_PROJECT_NAME AE_IMAGE_TAG AE_UPDATER_IMAGE_TAG; do
  if grep -q "^${hidden}=" "$REPO_FIXTURE/deploy/.env"; then
    echo "unexpected ${hidden} in deploy/.env" >&2
    exit 1
  fi
done

for hidden_repo in AE_IMAGE_REPOSITORY AE_UPDATER_IMAGE_REPOSITORY; do
  if grep -q "^${hidden_repo}=" "$REPO_FIXTURE/deploy/.env"; then
    echo "unexpected ${hidden_repo} in deploy/.env" >&2
    exit 1
  fi
done

grep -q "AI Efficiency Deployment Preflight" "$PREFLIGHT_LOG"
grep -q "Layout: repo" "$PREFLIGHT_LOG"
grep -q "Mode: bundled" "$PREFLIGHT_LOG"
grep -q "Compose file: $REPO_FIXTURE/deploy/docker-compose.yml" "$PREFLIGHT_LOG"
grep -q "Env file: $REPO_FIXTURE/deploy/.env" "$PREFLIGHT_LOG"
grep -q "Generated missing secrets: AE_AUTH_JWT_SECRET, AE_ENCRYPTION_KEY, POSTGRES_PASSWORD" "$PREFLIGHT_LOG"
grep -q "Checks passed:" "$PREFLIGHT_LOG"
grep -q "Next step:" "$PREFLIGHT_LOG"
grep -q "docker compose up -d" "$PREFLIGHT_LOG"
! grep -Eq 'AE_AUTH_JWT_SECRET:[[:space:]][0-9a-f]{64}' "$PREFLIGHT_LOG"
! grep -Eq 'AE_ENCRYPTION_KEY:[[:space:]][0-9a-f]{64}' "$PREFLIGHT_LOG"
! grep -Eq 'POSTGRES_PASSWORD:[[:space:]][0-9a-f]{64}' "$PREFLIGHT_LOG"

exit "$preflight_status"
