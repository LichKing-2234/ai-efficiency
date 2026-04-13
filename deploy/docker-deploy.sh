#!/usr/bin/env bash
set -euo pipefail

LAYOUT=""
SCRIPT_DIR=""
SCRIPT_ROOT=""
ROOT_DIR="$PWD"
ENV_FILE="$ROOT_DIR/.env"
MODE="${1:-check}"
COMPOSE_IMPL=""
ARCH="${ARCH:-}"
TMP_DIR=""
TAG="${TAG:-}"
GITHUB_REPO="${GITHUB_REPO:-LichKing-2234/ai-efficiency}"
RELEASE_API_URL="${RELEASE_API_URL:-https://api.github.com/repos/${GITHUB_REPO}/releases/latest}"
RELEASE_DOWNLOAD_BASE="${RELEASE_DOWNLOAD_BASE:-https://github.com/${GITHUB_REPO}/releases/download}"
FLOW_NAME=""
CURRENT_STAGE=""
GENERATED_SECRET_NAMES=()
GENERATED_SECRET_VALUES=()
CHECKS_PASSED=()

print_header() {
  local title="$1"
  printf '\n==========================================\n'
  printf '  %s\n' "$title"
  printf '==========================================\n\n'
}

info() {
  printf '[INFO] %s\n' "$1"
}

success() {
  printf '[SUCCESS] %s\n' "$1"
}

warning() {
  printf '[WARNING] %s\n' "$1"
}

set_stage() {
  CURRENT_STAGE="$1"
  info "$2"
}

record_check() {
  CHECKS_PASSED+=("$1")
}

join_names() {
  local first=1
  local value=""

  for value in "$@"; do
    if [[ $first -eq 1 ]]; then
      printf '%s' "$value"
      first=0
    else
      printf ', %s' "$value"
    fi
  done
}

print_bootstrap_summary() {
  local entry=""

  printf '\n==========================================\n'
  printf '  Preparation Complete!\n'
  printf '==========================================\n\n'

  if [[ "${#GENERATED_SECRET_VALUES[@]}" -gt 0 ]]; then
    printf 'Generated secure credentials:\n'
    for entry in "${GENERATED_SECRET_VALUES[@]}"; do
      printf '  %s\n' "$entry"
    done
    printf '\n'
  fi

  warning "These credentials have been saved to .env file."
  warning "Please keep them secure and do not share publicly!"

  printf '\nDirectory structure:\n'
  printf '  docker-compose.yml\n'
  printf '  .env\n'
  printf '  .env.example\n'
  printf '  data/\n'
  printf '  postgres_data/\n'
  printf '  redis_data/\n'

  printf '\nNext steps:\n'
  printf '  1. Edit .env if you need to customize configuration\n'
  printf '  2. Start services: docker compose up -d\n'
}

print_preflight_summary() {
  local entry=""

  printf '\n==========================================\n'
  printf '  Preflight Complete!\n'
  printf '==========================================\n\n'

  printf 'Checks passed:\n'
  for entry in "${CHECKS_PASSED[@]}"; do
    printf '  %s\n' "$entry"
  done

  if [[ "${#GENERATED_SECRET_NAMES[@]}" -gt 0 ]]; then
    printf '\nGenerated missing secrets: '
    join_names "${GENERATED_SECRET_NAMES[@]}"
    printf '\n'
  fi

  printf '\nNext step:\n'
  printf '  docker compose up -d\n'
}

cleanup() {
  if [[ -n "$TMP_DIR" && -d "$TMP_DIR" ]]; then
    rm -rf "$TMP_DIR"
  fi
}

handle_exit() {
  local exit_code="$1"
  if [[ "$exit_code" -ne 0 && -n "$FLOW_NAME" && -n "$CURRENT_STAGE" ]]; then
    printf '[ERROR] %s failed during %s\n' "$FLOW_NAME" "$CURRENT_STAGE" >&2
  fi
  cleanup
}

trap 'handle_exit $?' EXIT

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "missing required command: $cmd" >&2
    exit 1
  fi
}

generate_secret() {
  openssl rand -hex 32
}

normalize_arch() {
  local machine="$1"

  case "$machine" in
    x86_64|amd64)
      echo "amd64"
      ;;
    arm64|aarch64)
      echo "arm64"
      ;;
    *)
      return 1
      ;;
  esac
}

looks_bootstrapped() {
  local dir="$1"
  [[ -f "$dir/docker-compose.yml" && -d "$dir/deploy" && -f "$dir/.env.example" ]]
}

looks_repo() {
  local dir="$1"
  [[ -f "$dir/deploy/docker-deploy.sh" && -f "$dir/deploy/.env.example" ]]
}

resolve_script_root() {
  local source_path="${BASH_SOURCE[0]-}"

  if [[ -z "$source_path" ]]; then
    return
  fi

  if [[ ! -e "$source_path" ]]; then
    return
  fi

  SCRIPT_DIR="$(cd "$(dirname "$source_path")" && pwd)"
  SCRIPT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
}

detect_layout() {
  local work_dir="$PWD"

  if looks_bootstrapped "$work_dir"; then
    LAYOUT="bootstrapped"
    ROOT_DIR="$work_dir"
    ENV_FILE="$ROOT_DIR/.env"
    return
  fi

  if [[ -n "$SCRIPT_ROOT" ]]; then
    if looks_bootstrapped "$SCRIPT_ROOT"; then
      LAYOUT="bootstrapped"
      ROOT_DIR="$SCRIPT_ROOT"
      ENV_FILE="$ROOT_DIR/.env"
      return
    fi

    if looks_repo "$SCRIPT_ROOT"; then
      LAYOUT="repo"
      ROOT_DIR="$SCRIPT_ROOT"
      ENV_FILE="$ROOT_DIR/deploy/.env"
      return
    fi
  fi

  if looks_repo "$work_dir"; then
    LAYOUT="repo"
    ROOT_DIR="$work_dir"
    ENV_FILE="$ROOT_DIR/deploy/.env"
    return
  fi

  LAYOUT="bootstrap"
  ROOT_DIR="$work_dir"
  ENV_FILE="$ROOT_DIR/.env"
}

bootstrap_requested() {
  [[ "$LAYOUT" == "bootstrap" ]] || [[ "${AE_DOCKER_DEPLOY_BOOTSTRAP:-}" == "1" ]]
}

detect_platform() {
  local machine
  local override_arch=""

  if [[ -n "$ARCH" ]]; then
    override_arch="$ARCH"
    if ! ARCH="$(normalize_arch "$override_arch")"; then
      echo "unsupported architecture override: ${override_arch}" >&2
      exit 1
    fi
    return
  fi

  machine="$(uname -m)"
  if ! ARCH="$(normalize_arch "$machine")"; then
    echo "unsupported architecture: ${machine}" >&2
    exit 1
  fi
}

resolve_tag() {
  local response=""
  local resolved_tag=""

  if [[ -n "${TAG:-}" ]]; then
    printf '%s\n' "$TAG"
    return 0
  fi

  if ! response="$(curl -fsSL "$RELEASE_API_URL" 2>/dev/null)"; then
    return 0
  fi

  resolved_tag="$(
    RELEASE_JSON="$response" python3 - <<'PY'
import json
import os

payload = os.environ.get("RELEASE_JSON", "")
if not payload:
    raise SystemExit(0)

try:
    data = json.loads(payload)
except Exception:
    raise SystemExit(0)

tag = data.get("tag_name")
if isinstance(tag, str):
    print(tag.strip())
PY
  )"

  printf '%s\n' "$resolved_tag"
  return 0
}

download_backend_bundle() {
  local tag="$1"
  local version="${tag#v}"
  local archive="ai-efficiency-backend_${version}_linux_${ARCH}.tar.gz"
  local archive_path="${TMP_DIR}/${archive}"
  local checksums_path="${TMP_DIR}/checksums.txt"
  local expected_sha=""
  local actual_sha=""
  local expected_sha_norm=""
  local actual_sha_norm=""

  curl -fsSL "${RELEASE_DOWNLOAD_BASE}/${tag}/${archive}" -o "${archive_path}"
  curl -fsSL "${RELEASE_DOWNLOAD_BASE}/${tag}/checksums.txt" -o "${checksums_path}"

  expected_sha="$(awk -v name="$archive" 'NF >= 2 && ($2 == name || $2 == ("*" name)) {print $1; exit}' "$checksums_path")"
  if [[ -z "$expected_sha" ]]; then
    echo "missing checksum entry for ${archive}" >&2
    exit 1
  fi

  if [[ ! "$expected_sha" =~ ^[[:xdigit:]]{64}$ ]]; then
    echo "invalid checksum format for ${archive}" >&2
    exit 1
  fi

  actual_sha="$(openssl dgst -sha256 "$archive_path" | awk '{print $NF}')"
  expected_sha_norm="$(printf '%s' "$expected_sha" | tr '[:upper:]' '[:lower:]')"
  actual_sha_norm="$(printf '%s' "$actual_sha" | tr '[:upper:]' '[:lower:]')"
  if [[ "$actual_sha_norm" != "$expected_sha_norm" ]]; then
    echo "checksum verification failed for ${archive}" >&2
    exit 1
  fi

  tar -xzf "${archive_path}" -C "${TMP_DIR}"
}

prepare_bootstrap_root() {
  [[ ! -e "$ROOT_DIR/docker-compose.yml" ]] || { echo "current directory already contains docker-compose.yml" >&2; exit 1; }
  [[ ! -e "$ROOT_DIR/.env" ]] || { echo "current directory already contains .env" >&2; exit 1; }
  [[ ! -e "$ROOT_DIR/deploy" ]] || { echo "current directory already contains deploy/" >&2; exit 1; }
  [[ -d "$TMP_DIR/deploy" ]] || { echo "release bundle missing deploy/ assets" >&2; exit 1; }
  [[ -f "$TMP_DIR/deploy/docker-compose.bootstrap.yml" ]] || { echo "release bundle missing required asset: deploy/docker-compose.bootstrap.yml" >&2; exit 1; }
  [[ -f "$TMP_DIR/deploy/.env.example" ]] || { echo "release bundle missing required asset: deploy/.env.example" >&2; exit 1; }
  [[ -f "$TMP_DIR/deploy/docker-deploy.sh" ]] || { echo "release bundle missing required asset: deploy/docker-deploy.sh" >&2; exit 1; }
  [[ -f "$TMP_DIR/deploy/init-db.sql" ]] || { echo "release bundle missing required asset: deploy/init-db.sql" >&2; exit 1; }

  mkdir -p "$ROOT_DIR/deploy"
  cp -R "${TMP_DIR}/deploy/." "$ROOT_DIR/deploy/"
  cp "$ROOT_DIR/deploy/docker-compose.bootstrap.yml" "$ROOT_DIR/docker-compose.yml"
  cp "$ROOT_DIR/deploy/.env.example" "$ROOT_DIR/.env.example"
  cp "$ROOT_DIR/deploy/.env.example" "$ROOT_DIR/.env"
  mkdir -p "$ROOT_DIR/data" "$ROOT_DIR/postgres_data" "$ROOT_DIR/redis_data"
}

ensure_env() {
  if [[ ! -f "$ENV_FILE" ]]; then
    local env_example
    if [[ "$LAYOUT" == "repo" ]]; then
      env_example="$ROOT_DIR/deploy/.env.example"
    else
      env_example="$ROOT_DIR/.env.example"
    fi
    cp "$env_example" "$ENV_FILE"
  fi
}

check_required_var() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    echo "missing required env var: $name" >&2
    exit 1
  fi
}

set_env_var() {
  local name="$1"
  local value="$2"
  python3 - "$ENV_FILE" "$name" "$value" <<'PY'
import sys
from pathlib import Path

path = Path(sys.argv[1])
name = sys.argv[2]
value = sys.argv[3]
lines = path.read_text().splitlines()
prefix = f"{name}="
for i, line in enumerate(lines):
    if line.startswith(prefix):
        lines[i] = prefix + value
        break
else:
    lines.append(prefix + value)
path.write_text("\n".join(lines) + "\n")
PY
}

ensure_generated_var() {
  local name="$1"
  local generated=""
  if [[ -z "${!name:-}" ]]; then
    generated="$(generate_secret)"
    set_env_var "$name" "$generated"
    export "$name=$generated"
    GENERATED_SECRET_NAMES+=("$name")
    GENERATED_SECRET_VALUES+=("${name}: ${generated}")
  fi
}

source_env_file() {
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
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

resolve_script_root
detect_layout

case "$MODE" in
  check|external)
    ;;
  *)
    echo "usage: $0 [check|external]" >&2
    exit 1
    ;;
esac

if bootstrap_requested; then
  FLOW_NAME="Bootstrap"
  print_header "AI Efficiency Deployment Preparation"

  if [[ "$MODE" == "external" ]]; then
    echo "external mode is not supported during bootstrap" >&2
    exit 1
  fi
  require_cmd curl
  require_cmd openssl
  require_cmd python3
  require_cmd tar
  detect_platform
  TMP_DIR="$(mktemp -d)"

  set_stage "release tag resolution" "Resolving release tag..."
  TAG="$(resolve_tag)"
  if [[ -z "$TAG" ]]; then
    echo "failed to resolve release tag" >&2
    exit 1
  fi
  success "Resolved release tag: ${TAG}"

  set_stage "deploy asset download" "Downloading deploy assets..."
  download_backend_bundle "$TAG"
  success "Downloaded deploy assets"

  set_stage "deployment root preparation" "Preparing deployment files..."
  prepare_bootstrap_root
  success "Prepared docker-compose.yml"
  success "Prepared .env.example"
  success "Prepared .env"
  success "Created data directories"

  set_stage "secret generation" "Generating secure secrets..."
  source_env_file
  ensure_generated_var AE_AUTH_JWT_SECRET
  ensure_generated_var AE_ENCRYPTION_KEY
  ensure_generated_var POSTGRES_PASSWORD
  success "Generated missing secrets"

  print_bootstrap_summary
  exit 0
fi

require_cmd docker
require_cmd curl
require_cmd openssl
require_cmd python3

FLOW_NAME="Preflight"
ensure_env

if [[ "$LAYOUT" == "bootstrapped" && "$MODE" == "external" ]]; then
  echo "external mode is not supported in bootstrapped layout" >&2
  exit 1
fi

if [[ "$LAYOUT" == "bootstrapped" ]]; then
  COMPOSE_FILE="$ROOT_DIR/docker-compose.yml"
else
  COMPOSE_FILE="$ROOT_DIR/deploy/docker-compose.yml"
fi
if [[ "$MODE" == "external" ]]; then
  COMPOSE_FILE="$ROOT_DIR/deploy/docker-compose.external.yml"
fi

print_header "AI Efficiency Deployment Preflight"
info "Layout: $LAYOUT"
if [[ "$MODE" == "external" ]]; then
  info "Mode: external"
else
  info "Mode: bundled"
fi
info "Compose file: $COMPOSE_FILE"
info "Env file: $ENV_FILE"

set_stage "env preparation" "Loading environment..."
source_env_file
ensure_generated_var AE_AUTH_JWT_SECRET
ensure_generated_var AE_ENCRYPTION_KEY
ensure_generated_var POSTGRES_PASSWORD
source_env_file
success "Environment is ready"

check_required_var AE_RELAY_URL
check_required_var AE_AUTH_JWT_SECRET
check_required_var AE_ENCRYPTION_KEY
if [[ "$MODE" != "external" ]]; then
  check_required_var POSTGRES_USER
  check_required_var POSTGRES_PASSWORD
  check_required_var POSTGRES_DB
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

set_stage "compose validation" "Validating compose configuration..."
run_compose_config "$COMPOSE_FILE"
record_check "Compose config via ${COMPOSE_IMPL}"
success "Validated compose configuration"

if [[ "$MODE" == "external" ]]; then
  check_required_var AE_DB_DSN
  check_required_var AE_REDIS_ADDR

  db_host_port="$(extract_db_host_port "$AE_DB_DSN")"
  redis_host_port="$(extract_redis_host_port "$AE_REDIS_ADDR")"

  set_stage "external dependency checks" "Checking PostgreSQL and Redis connectivity..."
  check_tcp "${db_host_port%:*}" "${db_host_port##*:}"
  check_tcp "${redis_host_port%:*}" "${redis_host_port##*:}"
  record_check "PostgreSQL TCP connectivity"
  record_check "Redis TCP connectivity"
  success "External dependency checks passed"
fi

set_stage "relay health check" "Checking relay health..."
check_url "${AE_RELAY_URL%/}/health"
record_check "Relay health"
success "Relay health check passed"

print_preflight_summary
