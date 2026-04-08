#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="$ROOT_DIR/deploy/.env"
MODE="${1:-check}"
COMPOSE_IMPL=""

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "missing required command: $cmd" >&2
    exit 1
  fi
}

ensure_env() {
  if [[ ! -f "$ENV_FILE" ]]; then
    cp "$ROOT_DIR/deploy/.env.example" "$ENV_FILE"
  fi
}

check_required_var() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    echo "missing required env var: $name" >&2
    exit 1
  fi
}

check_url() {
  local url="$1"
  curl -fsS --max-time 5 "$url" >/dev/null
}

check_tcp() {
  local host="$1"
  local port="$2"
  if ! (exec 3<>"/dev/tcp/${host}/${port}") >/dev/null 2>&1; then
    echo "tcp check failed: ${host}:${port}" >&2
    exit 1
  fi
}

extract_db_host_port() {
  local dsn="$1"
  local remain hostport host port
  remain="${dsn#*://}"
  remain="${remain#*@}"
  hostport="${remain%%/*}"
  hostport="${hostport%%\?*}"

  if [[ "$hostport" == \[*\]* ]]; then
    host="${hostport%%]*}"
    host="${host#[}"
    port="${hostport##*]:}"
    if [[ "$port" == "$hostport" ]]; then
      port="5432"
    fi
  else
    host="${hostport%%:*}"
    port="${hostport##*:}"
    if [[ "$port" == "$hostport" ]]; then
      port="5432"
    fi
  fi

  echo "${host}:${port}"
}

extract_redis_host_port() {
  local addr="$1"
  local host="${addr%%:*}"
  local port="${addr##*:}"
  if [[ "$port" == "$addr" ]]; then
    port="6379"
  fi
  echo "${host}:${port}"
}

require_cmd docker
require_cmd curl
ensure_env

set -a
source "$ENV_FILE"
set +a

check_required_var AE_RELAY_URL
check_required_var AE_AUTH_JWT_SECRET
check_required_var AE_ENCRYPTION_KEY
check_required_var COMPOSE_PROJECT_NAME

COMPOSE_FILE="$ROOT_DIR/deploy/docker-compose.yml"
if [[ "$MODE" == "external" ]]; then
  COMPOSE_FILE="$ROOT_DIR/deploy/docker-compose.external.yml"
elif [[ "$MODE" != "check" ]]; then
  echo "usage: $0 [check|external]" >&2
  exit 1
fi

run_compose_config() {
  local compose_file="$1"

  if docker compose --env-file "$ENV_FILE" -f "$compose_file" config >/dev/null 2>&1; then
    COMPOSE_IMPL="docker compose"
    return 0
  fi

  if command -v docker-compose >/dev/null 2>&1; then
    if docker-compose --env-file "$ENV_FILE" -f "$compose_file" config >/dev/null 2>&1; then
      COMPOSE_IMPL="docker-compose"
      return 0
    fi
  fi

  echo "failed to validate compose config with both docker compose and docker-compose" >&2
  return 1
}

run_compose_config "$COMPOSE_FILE"

if [[ "$MODE" == "external" ]]; then
  check_required_var AE_DB_DSN
  check_required_var AE_REDIS_ADDR

  db_host_port="$(extract_db_host_port "$AE_DB_DSN")"
  redis_host_port="$(extract_redis_host_port "$AE_REDIS_ADDR")"

  check_tcp "${db_host_port%:*}" "${db_host_port##*:}"
  check_tcp "${redis_host_port%:*}" "${redis_host_port##*:}"
fi

check_url "${AE_RELAY_URL%/}/health"

echo "compose implementation: ${COMPOSE_IMPL}"
echo "preflight ok"
