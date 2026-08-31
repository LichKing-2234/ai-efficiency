# Retired Session and Update Directions

> Historical record. Every runtime/update direction in this file is
> superseded, removed, or rejected. Use linked neutral contracts and current
> code for implementation decisions.

This record preserves decision-relevant context from four historical designs:

| Direction | Original durable source | Disposition |
| --- | --- | --- |
| Platform Session and session-key PR attribution | [`3cd9b1cf`](https://github.com/LichKing-2234/ai-efficiency/blob/3cd9b1cfe45e4d0dbe0a7bc929bcb1ab390eb08a/docs/superpowers/specs/2026-03-26-session-pr-attribution-design.md) | Superseded; user/runtime surfaces and schema removed. |
| Local session proxy data plane | [`bc0bc75e`](https://github.com/LichKing-2234/ai-efficiency/blob/bc0bc75e93a973debfa28118c55b6ca6a88501bb/docs/superpowers/specs/2026-04-02-local-session-proxy-design.md) | Implemented experiment, then superseded and removed. |
| In-app deployment controls | [`41fc89fd`](https://github.com/LichKing-2234/ai-efficiency/blob/41fc89fd6433d5c7651ed9a8fc78e5bec483279e/docs/superpowers/specs/2026-04-13-frontend-deployment-control-alignment-design.md) | Mutation/control plane removed; read-only version visibility survives. |
| Unified backend binary self-update | [`bacfb6ce`](https://github.com/LichKing-2234/ai-efficiency/blob/bacfb6cee7f9db08d45b01612139781c9fb344b4/docs/superpowers/specs/2026-04-13-unified-binary-self-update-design.md) | Implemented direction rejected and removed. |

None of these directions is a dormant runtime capability behind a flag. Their
removed surfaces may return only compatibility errors/404s; reintroduction
requires a new current spec and implementation.

## Platform Session and Session-Key Attribution

### Historical direction

The Session/PR model made `ae-cli start` the explicit beginning of development
work. The backend created a platform Session and a dedicated Relay API key;
workspace marker/runtime files carried Session context; local collectors and
Git hooks produced metadata/checkpoints; manual settlement queried usage within
checkpoint intervals for the PR's commits.

The design deliberately called the result **commit interval cost**, not strict
per-commit causality. A request occurs before its eventual commit exists, may
support multiple commits, and may produce no commit. The Session key made the
usage window precise, but could not make the commit relationship causal.

### Why it was superseded

- Explicit start/stop/flush/tool-dispatch rituals made attribution shape the
  developer workflow.
- Per-Session key creation/revocation introduced key churn, bootstrap failure,
  cleanup, and routing complexity.
- Workspace marker/runtime bundles and platform Session recovery created a
  second lifecycle beside the tools' own durable evidence.
- Manual interval settlement remained ambiguous around exploration, rewritten
  commits, missing checkpoints, and multi-commit work.
- The enduring product question became committed-code evidence, not whether a
  platform Session was active.

Workspace identity, commit checkpoints, explicit rewrites, fail-open hook
delivery, and Provider identity survived in narrower forms. The platform
Session, Session-scoped key, bootstrap/heartbeat/stop lifecycle, Sessions UI,
session usage/event ingest, and old PR-settlement subject did not.

Removal occurred through the runtime-surface and schema contractions at
[`80e85a89`](https://github.com/LichKing-2234/ai-efficiency/commit/80e85a898dfd7ff64c11f0a335a6cc78a989e8d2),
[`eacd184a`](https://github.com/LichKing-2234/ai-efficiency/commit/eacd184a02c508bf036f076321c1746c198861ce),
and
[`29770f69`](https://github.com/LichKing-2234/ai-efficiency/commit/29770f69ae430b4bdf3ebc2bf65706bdca40591e).

## Local Session Proxy

### Historical direction

The proxy design moved OpenAI-compatible and Anthropic-compatible inference
through a Session-bound loopback server. The proxy held upstream credentials,
assigned Session/request identity, recorded request-level Token, accepted local
hook events, spooled backend failures, and became the primary usage fact for
Session/commit interval attribution.

It solved a real accounting problem: the proxy could observe request usage
before forwarding and did not need one upstream key per Session. It also made
the inference path depend on an AI Efficiency process. Silent direct fallback
was intentionally forbidden because it would create unobserved usage.

### Why it was removed

- Codex and Claude traffic/configuration became coupled to an extra local data
  plane; a proxy crash became an inference outage.
- The Session runtime, loopback authentication, credential storage, process
  lifecycle, protocol emulation, and request spooling created substantial local
  operational surface.
- Tool protocols and local evidence were heterogeneous, and the first design
  excluded Kiro.
- The user goal favored normal tool operation, no resident proxy/daemon, and
  post-hoc recovery from tool-native artifacts.
- Deterministic commit proof, not local traffic interception, became the formal
  admission boundary.

The experiment informed later privacy, spool, hook, and request-normalization
work, but its proxy server, request routes, Session usage/event APIs, and
Session-first accounting are removed. Current formal attribution reads local
tool evidence without routing inference through AI Efficiency.

## In-App Deployment Controls

### Historical direction

The Settings UI once exposed deployment status, update check/apply, rollback,
and restart. It distinguished Compose and systemd behavior and used health
polling/reload after actions expected to restart the backend. The router's
one-time dynamic-chunk reload guard was developed alongside that recovery UX.

### Why the mutation plane was removed

- Application code was being asked to control the process/image/service that
  owns it, creating a second deployment authority.
- Compose updater, systemd binary replacement, restart behavior, and browser
  recovery required different operational assumptions behind one UI.
- Runtime update state could drift from GitHub Release, image, service-manager,
  and operator state.
- External deployment tooling provides clearer authorization, rollback, and
  exact-artifact evidence.

The backend/frontend mutation surface was removed in
[`da00dc25`](https://github.com/LichKing-2234/ai-efficiency/commit/da00dc254a6d9041528d6e3c2ae88c9df101ce01).
The surviving system area is read-only: administrators may inspect current
build metadata and manually check the latest platform release. It cannot
download, replace, roll back, or restart the backend. The chunk-reload guard
survives as generic frontend bundle recovery, not deployment control.

## Unified Backend Binary Self-Update

### Historical direction

The unified update model attempted to remove the Docker updater sidecar while
preserving in-app apply/rollback/restart. Docker and non-Docker deployments
would run a writable runtime binary, atomically replace it from a checksummed
GitHub Release bundle, retain a `.backup`, and rely on the host/container
supervisor to restart the process.

This promised one product update model across environments and removed
`docker.sock`/Compose control from the updater. Its cost was more fundamental:
the running Docker binary could differ from the immutable image, and update
authority moved inside the process being replaced.

### Why it was rejected

- Mutable runtime binaries weakened image/release provenance and made the
  effective Docker version harder to prove.
- Launcher, writable volume, backup, version comparison, and restart semantics
  formed a new control plane inside every deployment.
- Backend self-update duplicated operator/service-manager responsibilities and
  blurred explicit release/deployment authorization.
- External image recreation or installer-driven replacement is simpler to
  inspect, automate, and roll back.

Backend-managed update, rollback, restart, updater sidecar, and deployment APIs
were removed together at [`da00dc25`](https://github.com/LichKing-2234/ai-efficiency/commit/da00dc254a6d9041528d6e3c2ae88c9df101ce01).
Docker now upgrades by pulling/recreating an image; systemd upgrades through
operator-run installation tooling. The user-level `ae-cli update` command is a
separate CLI release concern and was not retired with backend self-update.

## Current Boundaries

Current behavior is owned by:

- [attribution v2](../contracts/attribution-v2.md) for deterministic committed-
  code Token and event-driven local recovery;
- [CLI tool configuration](../contracts/cli-tool-configuration.md) for current
  non-Session setup/discovery;
- [platform loading](../contracts/platform-loading.md) for embedded frontend,
  health, readiness, and read-only runtime boundaries;
- [release units](../contracts/release-units.md) for platform/CLI artifacts and
  release discovery.

This history authorizes none of the removed routes, commands, schemas,
processes, or update actions.
