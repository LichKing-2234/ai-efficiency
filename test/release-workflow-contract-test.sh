#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PLATFORM_WORKFLOW="$ROOT_DIR/.github/workflows/release.yml"
CLI_WORKFLOW="$ROOT_DIR/.github/workflows/ae-cli-release.yml"
BRIDGE_WORKFLOW="$ROOT_DIR/.github/workflows/ae-cli-bridge-release.yml"

test -f "$PLATFORM_WORKFLOW"
test -f "$CLI_WORKFLOW"
test -f "$BRIDGE_WORKFLOW"

platform_positive_line="$(grep -n "      - 'v\\*'" "$PLATFORM_WORKFLOW" | cut -d: -f1 | head -1)"
platform_negative_line="$(grep -n "      - '!v\\*-cli\\.\\*'" "$PLATFORM_WORKFLOW" | cut -d: -f1 | head -1)"
test -n "$platform_positive_line"
test -n "$platform_negative_line"
test "$platform_positive_line" -lt "$platform_negative_line"
grep -q "Platform release workflow must not publish CLI bridge tag" "$PLATFORM_WORKFLOW"

grep -q "      - 'ae-cli/v\\*'" "$CLI_WORKFLOW"
grep -q -- "--latest=false" "$CLI_WORKFLOW"

grep -q "      - 'v0.2.0-cli.1'" "$BRIDGE_WORKFLOW"
grep -q "CLI bridge release tag v0.2.0-cli.1 must already exist before manual dispatch" "$BRIDGE_WORKFLOW"
grep -q "args: release --snapshot --clean --config .goreleaser.ae-cli.yaml" "$BRIDGE_WORKFLOW"
grep -q "AE_CLI_VERSION: \${{ needs.prepare.outputs.version }}" "$BRIDGE_WORKFLOW"
grep -q "AE_CLI_VERSION_NO_V: \${{ needs.prepare.outputs.version_no_v }}" "$BRIDGE_WORKFLOW"
grep -q -- "--latest" "$BRIDGE_WORKFLOW"
! grep -q -- "--latest=false" "$BRIDGE_WORKFLOW"
! grep -q -- "--prerelease" "$BRIDGE_WORKFLOW"
! grep -q "docker/build-push-action" "$BRIDGE_WORKFLOW"
! grep -q "deploy/Dockerfile" "$BRIDGE_WORKFLOW"
