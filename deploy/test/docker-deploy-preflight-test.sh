#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

REPO_FIXTURE="$TMP_ROOT/repo-fixture"
REPO_FIXTURE_REAL=""
FAKE_BIN="$TMP_ROOT/fake-bin"
mkdir -p "$REPO_FIXTURE/deploy" "$FAKE_BIN"
REPO_FIXTURE_REAL="$(cd "$REPO_FIXTURE" && pwd -P)"

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
  cat "$PREFLIGHT_LOG" >&2
  echo "expected bundled preflight fixture to succeed" >&2
  exit 1
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
grep -q "Compose file: $REPO_FIXTURE_REAL/deploy/docker-compose.yml" "$PREFLIGHT_LOG"
grep -q "Env file: $REPO_FIXTURE_REAL/deploy/.env" "$PREFLIGHT_LOG"
grep -q "Generated missing secrets: AE_AUTH_JWT_SECRET, AE_ENCRYPTION_KEY, POSTGRES_PASSWORD" "$PREFLIGHT_LOG"
grep -q "Checks passed:" "$PREFLIGHT_LOG"
grep -q "Next step:" "$PREFLIGHT_LOG"
grep -q "docker compose up -d" "$PREFLIGHT_LOG"
! grep -Eq 'AE_AUTH_JWT_SECRET:[[:space:]][0-9a-f]{64}' "$PREFLIGHT_LOG"
! grep -Eq 'AE_ENCRYPTION_KEY:[[:space:]][0-9a-f]{64}' "$PREFLIGHT_LOG"
! grep -Eq 'POSTGRES_PASSWORD:[[:space:]][0-9a-f]{64}' "$PREFLIGHT_LOG"

NEGATIVE_FIXTURE="$TMP_ROOT/repo-fixture-missing-relay"
NEGATIVE_FIXTURE_REAL=""
mkdir -p "$NEGATIVE_FIXTURE/deploy"
NEGATIVE_FIXTURE_REAL="$(cd "$NEGATIVE_FIXTURE" && pwd -P)"

cp "$ROOT_DIR/deploy/docker-deploy.sh" "$NEGATIVE_FIXTURE/deploy/docker-deploy.sh"
cp "$ROOT_DIR/deploy/.env.example" "$NEGATIVE_FIXTURE/deploy/.env.example"
cp "$ROOT_DIR/deploy/docker-compose.yml" "$NEGATIVE_FIXTURE/deploy/docker-compose.yml"

cat >"$NEGATIVE_FIXTURE/deploy/.env" <<'EOF'
AE_SERVER_PORT=8081
AE_SERVER_FRONTEND_URL=http://localhost:8081
POSTGRES_USER=ai_efficiency
POSTGRES_PASSWORD=
POSTGRES_DB=ai_efficiency
REDIS_PASSWORD=
AE_RELAY_URL=
AE_RELAY_API_KEY=
AE_RELAY_ADMIN_API_KEY=
AE_AUTH_JWT_SECRET=
AE_ENCRYPTION_KEY=
EOF

NEGATIVE_LOG="$TMP_ROOT/preflight-missing-relay.log"
set +e
(
  cd "$NEGATIVE_FIXTURE"
  env -i PATH="$FAKE_BIN:$PATH" \
    bash deploy/docker-deploy.sh
) >"$NEGATIVE_LOG" 2>&1
negative_status=$?
set -e
if [[ "$negative_status" -eq 0 ]]; then
  cat "$NEGATIVE_LOG" >&2
  echo "expected missing relay fixture to fail" >&2
  exit 1
fi
grep -q "missing required env var: AE_RELAY_URL" "$NEGATIVE_LOG"
grep -q "Preflight failed during env preparation" "$NEGATIVE_LOG"
grep -q "Compose file: $NEGATIVE_FIXTURE_REAL/deploy/docker-compose.yml" "$NEGATIVE_LOG"

EXTERNAL_FIXTURE="$TMP_ROOT/repo-fixture-external"
EXTERNAL_FIXTURE_REAL=""
mkdir -p "$EXTERNAL_FIXTURE/deploy"
EXTERNAL_FIXTURE_REAL="$(cd "$EXTERNAL_FIXTURE" && pwd -P)"

cp "$ROOT_DIR/deploy/docker-deploy.sh" "$EXTERNAL_FIXTURE/deploy/docker-deploy.sh"
cp "$ROOT_DIR/deploy/.env.example" "$EXTERNAL_FIXTURE/deploy/.env.example"
cp "$ROOT_DIR/deploy/docker-compose.external.yml" "$EXTERNAL_FIXTURE/deploy/docker-compose.external.yml"

cat >"$EXTERNAL_FIXTURE/deploy/.env" <<'EOF'
AE_SERVER_FRONTEND_URL=http://localhost:8081
AE_DB_DSN=postgres://postgres:postgres@127.0.0.1:45432/ai_efficiency?sslmode=disable
AE_REDIS_ADDR=127.0.0.1:46379
AE_REDIS_PASSWORD=
AE_REDIS_DB=0
AE_RELAY_URL=http://127.0.0.1/relay
AE_RELAY_API_KEY=
AE_RELAY_ADMIN_API_KEY=
AE_AUTH_JWT_SECRET=
AE_ENCRYPTION_KEY=
EOF

TCP_SERVER_SCRIPT="$TMP_ROOT/tcp-server.py"
cat >"$TCP_SERVER_SCRIPT" <<'EOF'
import socket
import sys
import threading

port = int(sys.argv[1])
sock = socket.socket()
sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
sock.bind(("127.0.0.1", port))
sock.listen()

def loop():
    while True:
        conn, _ = sock.accept()
        conn.close()

threading.Thread(target=loop, daemon=True).start()
threading.Event().wait()
EOF

python3 "$TCP_SERVER_SCRIPT" 45432 >/dev/null 2>&1 &
postgres_pid=$!
python3 "$TCP_SERVER_SCRIPT" 46379 >/dev/null 2>&1 &
redis_pid=$!
trap 'kill "$postgres_pid" "$redis_pid" 2>/dev/null || true; rm -rf "$TMP_ROOT"' EXIT

EXTERNAL_LOG="$TMP_ROOT/preflight-external.log"
set +e
(
  cd "$EXTERNAL_FIXTURE"
  env -i PATH="$FAKE_BIN:$PATH" \
    bash deploy/docker-deploy.sh external
) >"$EXTERNAL_LOG" 2>&1
external_status=$?
set -e
kill "$postgres_pid" "$redis_pid" 2>/dev/null || true
trap 'rm -rf "$TMP_ROOT"' EXIT

if [[ "$external_status" -ne 0 ]]; then
  cat "$EXTERNAL_LOG" >&2
  echo "expected external preflight fixture to succeed without POSTGRES_* vars" >&2
  exit 1
fi
grep -q "Mode: external" "$EXTERNAL_LOG"
grep -q "Compose file: $EXTERNAL_FIXTURE_REAL/deploy/docker-compose.external.yml" "$EXTERNAL_LOG"
grep -q "External dependency checks passed" "$EXTERNAL_LOG"
