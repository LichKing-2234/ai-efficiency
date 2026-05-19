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
        if self.path != "/api/v1/providers":
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
                        "api_key": "sk-mock-123",
                        "api_key_id": 123,
                        "default_model": "gpt-5.3-codex",
                        "is_primary": True,
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
test -f "${TMP_HOME}/.claude/settings.json"
test -f "${TMP_HOME}/.ae-cli/env.sh"
test -f "${TMP_HOME}/.zshrc"
test ! -f "${TMP_HOME}/.gemini/settings.json"

grep -F "Configured provider primary for 3 tool(s):" "${OUTPUT_FILE}" >/dev/null
grep -F "  - codex" "${OUTPUT_FILE}" >/dev/null
grep -F "  - claude" "${OUTPUT_FILE}" >/dev/null
grep -F "  - gemini" "${OUTPUT_FILE}" >/dev/null

grep -F "openai_base_url = 'https://relay.example.com/v1'" "${TMP_HOME}/.codex/config.toml" >/dev/null
grep -F '"ANTHROPIC_API_KEY": "sk-mock-123"' "${TMP_HOME}/.claude/settings.json" >/dev/null
grep -F 'export OPENAI_API_KEY="sk-mock-123"' "${TMP_HOME}/.ae-cli/env.sh" >/dev/null
grep -F 'export GEMINI_API_KEY="sk-mock-123"' "${TMP_HOME}/.ae-cli/env.sh" >/dev/null
grep -F 'export GOOGLE_GEMINI_BASE_URL="https://relay.example.com/v1"' "${TMP_HOME}/.ae-cli/env.sh" >/dev/null
grep -F '[ -f "$HOME/.ae-cli/env.sh" ] && source "$HOME/.ae-cli/env.sh"' "${TMP_HOME}/.zshrc" >/dev/null
