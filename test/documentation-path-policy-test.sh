#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

fail() {
  echo "documentation path policy: $*" >&2
  exit 1
}

if [[ -n "$(git ls-files -- 'docs/superpowers/**')" ]]; then
  fail "tracked files remain under the retired documentation path"
fi

if [[ -e docs/superpowers || -L docs/superpowers ]]; then
  fail "the retired documentation path exists"
fi

navigation_files=(
  AGENTS.md
  CLAUDE.md
  README.md
  README.zh-CN.md
  docs/architecture.md
)

for required in docs/contracts/README.md docs/history/README.md "${navigation_files[@]}"; do
  [[ -f "$required" ]] || fail "required navigation file is missing: $required"
done

if grep -R -F -n 'docs/superpowers' docs/contracts "${navigation_files[@]}"; then
  fail "current navigation or contracts reference the retired documentation path"
fi

if grep -R -F -n '/Users/' docs/contracts docs/history "${navigation_files[@]}"; then
  fail "retained documentation contains a machine-specific checkout path"
fi

echo "documentation path policy passed"
