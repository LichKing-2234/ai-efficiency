#!/usr/bin/env bash
set -euo pipefail

BIN_PATH="${1:-}"
if [[ -z "${BIN_PATH}" ]]; then
  echo "usage: $0 <ae-cli-binary>" >&2
  exit 1
fi

if [[ ! -x "${BIN_PATH}" ]]; then
  echo "binary is not executable: ${BIN_PATH}" >&2
  exit 1
fi

TMP_HOME="$(mktemp -d)"
TMP_BIN="$(mktemp -d)"
PORT="${AE_CLI_DISCOVER_E2E_PORT:-}"
SERVER_PID=""

cleanup() {
  if [[ -n "${SERVER_PID}" ]]; then
    kill "${SERVER_PID}" >/dev/null 2>&1 || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  rm -rf "${TMP_HOME}" "${TMP_BIN}"
}
trap cleanup EXIT

if [[ -z "${PORT}" ]]; then
  PORT="$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
fi

for name in codex claude gemini; do
  cat > "${TMP_BIN}/${name}" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "${TMP_BIN}/${name}"
done

mkdir -p "${TMP_HOME}/.ae-cli"
cat > "${TMP_HOME}/.ae-cli/token.json" <<EOF
{
  "access_token": "mock-token",
  "refresh_token": "mock-refresh",
  "expires_at": "2099-01-01T00:00:00Z",
  "server_url": "http://127.0.0.1:${PORT}"
}
EOF

python3 - <<'PY' "${PORT}" &
from http.server import BaseHTTPRequestHandler, HTTPServer
import json
import sys

port = int(sys.argv[1])

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != "/api/v1/user/providers":
            self.send_response(404)
            self.end_headers()
            return
        body = {
            "code": 200,
            "data": {
                "providers": [
                    {
                        "name": "primary",
                        "display_name": "Primary Relay",
                        "base_url": "https://relay.example.com/v1",
                        "default_model": "claude-sonnet-4-20250514",
                        "is_primary": True,
                        "groups": [
                            {
                                "group_id": "42",
                                "group_name": "Group Alpha",
                                "platform": "openai",
                                "credential": {
                                    "api_key_id": 123,
                                    "key": "sk-openai-123",
                                    "status": "active",
                                },
                            },
                            {
                                "group_id": "43",
                                "group_name": "Group Beta",
                                "platform": "anthropic",
                                "credential": {
                                    "api_key_id": 124,
                                    "key": "sk-anthropic-123",
                                    "status": "active",
                                },
                            },
                            {
                                "group_id": "44",
                                "group_name": "Group Gamma",
                                "platform": "gemini",
                                "credential": {
                                    "api_key_id": 125,
                                    "key": "sk-gemini-123",
                                    "status": "active",
                                },
                            }
                        ],
                    }
                ]
            },
        }
        payload = json.dumps(body).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, fmt, *args):
        pass

HTTPServer(("127.0.0.1", port), Handler).serve_forever()
PY
SERVER_PID=$!
sleep 1

OUTPUT_FILE="${TMP_HOME}/discover.out"
HOME="${TMP_HOME}" PATH="${TMP_BIN}:${PATH}" SHELL=/bin/zsh "${BIN_PATH}" discover > "${OUTPUT_FILE}"

test -f "${TMP_HOME}/.codex/config.toml"
test -f "${TMP_HOME}/.codex/auth.json"
test -f "${TMP_HOME}/.claude/settings.json"
test -f "${TMP_HOME}/.ae-cli/env.sh"
test -f "${TMP_HOME}/.zshrc"
test ! -f "${TMP_HOME}/.gemini/settings.json"

grep -F "Configured provider primary for 3 tool(s):" "${OUTPUT_FILE}" >/dev/null
grep -F "  - codex" "${OUTPUT_FILE}" >/dev/null
grep -F "  - claude" "${OUTPUT_FILE}" >/dev/null
grep -F "  - gemini" "${OUTPUT_FILE}" >/dev/null

grep -F "model_provider = 'OpenAI'" "${TMP_HOME}/.codex/config.toml" >/dev/null
grep -F "model = 'gpt-5.4'" "${TMP_HOME}/.codex/config.toml" >/dev/null
grep -F "review_model = 'gpt-5.4'" "${TMP_HOME}/.codex/config.toml" >/dev/null
if grep -F 'claude-sonnet-4-20250514' "${TMP_HOME}/.codex/config.toml" >/dev/null; then
  echo "Codex config should not use provider default_model" >&2
  exit 1
fi
grep -F "model_reasoning_effort = 'xhigh'" "${TMP_HOME}/.codex/config.toml" >/dev/null
grep -F "disable_response_storage = true" "${TMP_HOME}/.codex/config.toml" >/dev/null
grep -F "network_access = 'enabled'" "${TMP_HOME}/.codex/config.toml" >/dev/null
grep -F "windows_wsl_setup_acknowledged = true" "${TMP_HOME}/.codex/config.toml" >/dev/null
grep -F "model_context_window = 1000000" "${TMP_HOME}/.codex/config.toml" >/dev/null
grep -F "model_auto_compact_token_limit = 900000" "${TMP_HOME}/.codex/config.toml" >/dev/null
grep -F "[model_providers.OpenAI]" "${TMP_HOME}/.codex/config.toml" >/dev/null
grep -F "base_url = 'https://relay.example.com/v1'" "${TMP_HOME}/.codex/config.toml" >/dev/null
grep -F "wire_api = 'responses'" "${TMP_HOME}/.codex/config.toml" >/dev/null
grep -F "requires_openai_auth = true" "${TMP_HOME}/.codex/config.toml" >/dev/null
grep -F '"OPENAI_API_KEY": "sk-openai-123"' "${TMP_HOME}/.codex/auth.json" >/dev/null
grep -F '"ANTHROPIC_AUTH_TOKEN": "sk-anthropic-123"' "${TMP_HOME}/.claude/settings.json" >/dev/null
grep -F '"ANTHROPIC_BASE_URL": "https://relay.example.com/v1"' "${TMP_HOME}/.claude/settings.json" >/dev/null
grep -F '"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1"' "${TMP_HOME}/.claude/settings.json" >/dev/null
grep -F '"CLAUDE_CODE_ATTRIBUTION_HEADER": "0"' "${TMP_HOME}/.claude/settings.json" >/dev/null
grep -F 'export GEMINI_API_KEY="sk-gemini-123"' "${TMP_HOME}/.ae-cli/env.sh" >/dev/null
grep -F 'export GOOGLE_GEMINI_BASE_URL="https://relay.example.com/v1"' "${TMP_HOME}/.ae-cli/env.sh" >/dev/null
if grep -F 'OPENAI_API_KEY' "${TMP_HOME}/.ae-cli/env.sh" >/dev/null; then
  echo "OPENAI_API_KEY should be stored in ~/.codex/auth.json, not env.sh" >&2
  exit 1
fi
grep -F '[ -f "$HOME/.ae-cli/env.sh" ] && source "$HOME/.ae-cli/env.sh"' "${TMP_HOME}/.zshrc" >/dev/null
