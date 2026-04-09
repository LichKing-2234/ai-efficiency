#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODE="local"
FORCE_RESET="false"
SQLITE_PATH="$ROOT_DIR/backend/ai_efficiency.db"
ENV_FILE="$ROOT_DIR/deploy/.env"
COMPOSE_IMPL=""

usage() {
  cat <<'EOF'
usage: deploy/migrate-sqlite-to-postgres.sh [dev|local] [--force-reset] [--sqlite-path PATH]

One-time bootstrap helper that migrates backend/ai_efficiency.db into the
Postgres service used by the local Docker Compose validation paths.
EOF
}

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "missing required command: $cmd" >&2
    exit 1
  fi
}

run_compose() {
  local compose_file="$1"
  shift

  if docker compose -f "$compose_file" "$@" >/dev/null 2>&1; then
    docker compose -f "$compose_file" "$@"
    return 0
  fi

  if command -v docker-compose >/dev/null 2>&1; then
    docker-compose -f "$compose_file" "$@"
    return 0
  fi

  echo "docker compose or docker-compose is required" >&2
  exit 1
}

pick_compose_impl() {
  local compose_file="$1"
  if docker compose -f "$compose_file" config >/dev/null 2>&1; then
    COMPOSE_IMPL="docker compose"
    return 0
  fi
  if command -v docker-compose >/dev/null 2>&1 && docker-compose -f "$compose_file" config >/dev/null 2>&1; then
    COMPOSE_IMPL="docker-compose"
    return 0
  fi
  echo "failed to validate compose file: $compose_file" >&2
  exit 1
}

compose_cmd() {
  local compose_file="$1"
  shift
  if [[ "$COMPOSE_IMPL" == "docker compose" ]]; then
    docker compose -f "$compose_file" "$@"
  else
    docker-compose -f "$compose_file" "$@"
  fi
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      dev|local)
        MODE="$1"
        shift
        ;;
      --force-reset)
        FORCE_RESET="true"
        shift
        ;;
      --sqlite-path)
        SQLITE_PATH="$2"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        echo "unknown argument: $1" >&2
        usage >&2
        exit 1
        ;;
    esac
  done
}

ensure_env() {
  if [[ ! -f "$ENV_FILE" ]]; then
    ENV_FILE="$ROOT_DIR/deploy/.env.example"
  fi
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
}

wait_for_postgres() {
  local compose_file="$1"
  local attempts=0
  until compose_cmd "$compose_file" exec -T postgres pg_isready -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-ai_efficiency}" >/dev/null 2>&1; do
    attempts=$((attempts + 1))
    if [[ $attempts -ge 30 ]]; then
      echo "postgres is not ready after waiting" >&2
      exit 1
    fi
    sleep 2
  done
}

ensure_empty_or_reset() {
  local compose_file="$1"
  local table_count
  table_count="$(compose_cmd "$compose_file" exec -T postgres psql -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-ai_efficiency}" -Atqc "SELECT count(*) FROM information_schema.tables WHERE table_schema='public';")"
  if [[ "${table_count:-0}" == "0" ]]; then
    return 0
  fi

  if [[ "$FORCE_RESET" != "true" ]]; then
    echo "target database is not empty; rerun with --force-reset to recreate public schema" >&2
    exit 1
  fi

  compose_cmd "$compose_file" exec -T postgres psql -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-ai_efficiency}" -v ON_ERROR_STOP=1 -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"
}

compose_network() {
  local compose_file="$1"
  local container_id
  container_id="$(compose_cmd "$compose_file" ps -q postgres)"
  if [[ -z "$container_id" ]]; then
    echo "could not determine postgres container id" >&2
    exit 1
  fi
  docker inspect -f '{{range $name, $_ := .NetworkSettings.Networks}}{{println $name}}{{end}}' "$container_id" | head -n1 | tr -d '\r'
}

run_pgloader() {
  local network_name="$1"
  local encoded_password
  encoded_password="$(python3 -c 'import sys, urllib.parse; print(urllib.parse.quote(sys.argv[1], safe=""))' "${POSTGRES_PASSWORD:-postgres}")"
  docker run --rm \
    --network "$network_name" \
    -v "$SQLITE_PATH:/workspace/ai_efficiency.db:ro" \
    dimitri/pgloader:latest \
    pgloader \
    "sqlite:///workspace/ai_efficiency.db" \
    "postgresql://${POSTGRES_USER:-postgres}:${encoded_password}@postgres:5432/${POSTGRES_DB:-ai_efficiency}?sslmode=disable"
}

print_summary() {
  local compose_file="$1"
  local table_count
  table_count="$(compose_cmd "$compose_file" exec -T postgres psql -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-ai_efficiency}" -Atqc "SELECT count(*) FROM information_schema.tables WHERE table_schema='public';")"
  echo "sqlite bootstrap completed"
  echo "source: $SQLITE_PATH"
  echo "mode: $MODE"
  echo "public tables: ${table_count:-0}"
}

main() {
  parse_args "$@"
  require_cmd docker
  require_cmd python3
  ensure_env

  if [[ ! -f "$SQLITE_PATH" ]]; then
    echo "sqlite source not found: $SQLITE_PATH" >&2
    exit 1
  fi

  local compose_file="$ROOT_DIR/deploy/docker-compose.local.yml"
  if [[ "$MODE" == "dev" ]]; then
    compose_file="$ROOT_DIR/deploy/docker-compose.dev.yml"
  fi

  pick_compose_impl "$compose_file"
  compose_cmd "$compose_file" up -d postgres
  wait_for_postgres "$compose_file"
  ensure_empty_or_reset "$compose_file"

  local network_name
  network_name="$(compose_network "$compose_file")"
  run_pgloader "$network_name"
  print_summary "$compose_file"
}

main "$@"
