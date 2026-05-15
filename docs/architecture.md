# AI Efficiency Platform Architecture

This document is the project-level architecture overview for `ai-efficiency`.

- Use this file for the current system map, runtime relationships, and module boundaries.
- Use the topic-specific specs in `docs/superpowers/specs/` for detailed contracts.
- When documents disagree, prefer the newest relevant spec plus the current code.
- This file should always reflect the latest implemented project-level architecture.
- Topic specs may intentionally preserve point-in-time design decisions and trade-offs; do not rewrite them wholesale just to mirror the latest code if doing so would erase architectural evolution.
- When newer specs supersede or conflict with older specs, record that relationship in the newer spec rather than back-editing historical specs to mirror the latest implementation.

## Source-of-Truth Order

1. Topic-specific current specs:
   - `docs/superpowers/specs/2026-05-14-legacy-session-staged-cutover-design.md`
   - `docs/superpowers/specs/2026-04-15-oauth-device-login-design.md`
   - `docs/superpowers/specs/2026-04-14-llm-settings-runtime-editing-design.md`
   - `docs/superpowers/specs/2026-04-02-local-session-proxy-design.md`
   - `docs/superpowers/specs/2026-03-26-session-pr-attribution-design.md`
   - `docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md`
2. This architecture overview
3. `docs/superpowers/specs/2026-03-17-ai-efficiency-platform-design.md` as the historical baseline

## Current System Context

```mermaid
flowchart LR
    Browser["Browser UI<br/>Vue 3 + Vite + Pinia"]
    CLI["ae-cli<br/>session tooling + hooks + collectors"]
    Proxy["Local Session Proxy<br/>(ae-cli child process)"]
    Tool["Codex / Claude"]
    Backend["ai-efficiency backend<br/>Gin + Ent modular monolith"]
    DB[("ai_efficiency database<br/>PostgreSQL")]
    SCM["SCM providers<br/>GitHub / Bitbucket Server"]
    Relay["Relay provider<br/>sub2api HTTP APIs"]
    Workspace["Developer workspace<br/>repo, git hooks, session marker"]

    Browser <-->|REST API / OAuth| Backend
    CLI <-->|login / bootstrap / events| Backend
    CLI --> Proxy
    Tool --> Proxy
    Proxy --> Relay
    Proxy --> Backend
    Backend <--> DB
    Backend <--> SCM
    Backend <--> Relay
    CLI --> Workspace
    Workspace --> Backend
```

### Notes

- `ai-efficiency` is a standalone system. It integrates with `sub2api` through relay/provider HTTP APIs rather than direct database coupling.
- The backend is the central orchestration point for auth, repo configuration, analysis, attribution, and SCM/webhook workflows.
- Backend runtime relay consumers currently resolve their primary relay instance from `relay.*` config first, and fall back to the enabled primary `RelayProvider` database record when static relay URL config is absent.
- The frontend is built separately and embedded into the backend binary during Docker build, so the backend process serves both API routes and the SPA entrypoint in deployed images.
- Official production deployment now has two supported paths: Docker Compose and Linux systemd.
- The business entrypoint remains the backend service that also serves the frontend bundle.
- Docker/Compose mode now runs the backend from a persistent runtime binary under the deployment state directory and updates that runtime binary directly instead of using an updater sidecar.
- When `AE_CONFIG_PATH` is unset, Docker/Compose and local runtime modes materialize a writable config file under the deployment state directory (or the current working directory outside managed deployment) so admin settings can persist.
- Linux systemd mode installs the backend under `/opt/ai-efficiency`, keeps config in `/etc/ai-efficiency/config.yaml`, and performs binary self-update plus `.backup` rollback.
- `deploy/` also includes non-production `dev` / `local` compose paths for local verification.
- Public health endpoints expose liveness/readiness, and admin settings expose deployment status plus update controls.
- `ae-cli login` now supports both browser PKCE and OAuth device flow. Headless Linux environments are expected to use `ae-cli login --device`, while desktop/browser-capable environments still default to PKCE.

## Current Production Deployment

The current deployment model is split by runtime mode.

```mermaid
flowchart TD
    subgraph Compose["Docker Compose mode"]
    Browser["Browser"]
    Backend["Backend + Frontend bundle"]
    Runtime["Persistent runtime binary<br/>/var/lib/ai-efficiency/runtime"]
    DB[("Postgres")]
    Redis[("Redis")]
    Relay["sub2api / relay"]

    Browser --> Backend
    Backend --> DB
    Backend --> Redis
    Backend --> Relay
    Backend --> Runtime
    end

    subgraph Systemd["Linux systemd mode"]
    Browser2["Browser"]
    Backend2["ai-efficiency-server"]
    Systemctl["systemctl / ai-efficiency.service"]
    FS["/opt + /etc + /var/lib"]
    Relay2["sub2api / relay"]
    DB2[("Postgres")]
    Redis2[("Redis")]

    Browser2 --> Backend2
    Backend2 --> DB2
    Backend2 --> Redis2
    Backend2 --> Relay2
    Backend2 --> Systemctl
    Backend2 --> FS
    end
```

### Deployment Notes

- Official deploy assets live under `deploy/`.
- `deploy/docker-compose.yml` is the bundled-infra path.
- `deploy/docker-compose.external.yml` is the external-infra path.
- `deploy/docker-compose.dev.yml` is the source-build local validation path.
- `deploy/docker-compose.local.yml` is the directory-backed local validation path.
- `deploy/docker-deploy.sh` is the preflight entrypoint.
- `deploy/install.sh` is the Linux systemd installer entrypoint.
- `deploy/ai-efficiency.service` is the packaged systemd unit template.
- `deploy/migrate-sqlite-to-postgres.sh` is the one-time bootstrap path from local SQLite data into the local Postgres test environment.
- `deploy/.env.example` is the operator-facing configuration template.
- Backend deployment status, update, rollback, and restart APIs are first-class admin surfaces across Docker and non-Docker modes.

## Current Runtime Flow

The current implementation still contains two attribution paths, but only one is the formal user-facing workflow:

- formal/sessionless flow centered on `ae-cli init`, `ae-cli sync`, tool-local artifacts, and git checkpoints
- legacy/session-bound compatibility flow centered on `ae-cli start`, backend bootstrap, and the local proxy

```mermaid
sequenceDiagram
    participant Dev as Developer
    participant CLI as ae-cli
    participant Browser as Browser
    participant BE as Backend
    participant Proxy as Local Session Proxy
    participant Relay as Relay / sub2api
    participant WS as Workspace + Hooks
    participant Tool as AI Tooling

    Dev->>CLI: ae-cli login
    alt Browser PKCE login
        CLI->>BE: /oauth/authorize + /oauth/token
    else Device login
        CLI->>BE: /oauth/device/code + /oauth/token polling
        Browser->>BE: /oauth/device/verify
    end
    Dev->>CLI: ae-cli init
    CLI->>WS: install hooks / maintain local attribution state
    Dev->>Tool: run Codex / Claude / other tools
    opt Legacy session-bound compatibility path
        Dev->>CLI: ae-cli start
        CLI->>BE: session bootstrap
        BE->>BE: find or create repo from local git remote
        BE->>Relay: resolve relay identity / manage API key
        Relay-->>BE: user + key metadata
        BE-->>CLI: session metadata + env bundle
        CLI->>Proxy: start proxy with session runtime config
        Tool->>Proxy: local OpenAI / Anthropic request
        Proxy->>Relay: upstream model request
        Proxy->>BE: session usage + session events\n(spool fallback on failure)
    end
    Tool->>WS: write local Codex / Claude / Kiro artifacts
    WS->>WS: short-lived sync scans local artifacts
    WS->>BE: tool_usage_events ingest
    WS->>BE: checkpoint events + rewrite events
    BE->>BE: bind tool_usage_events to commit checkpoints
    BE->>Relay: relay usage lookup fallback for attribution when local usage is absent
```

### Runtime Boundaries

- `ae-cli` owns the primary sessionless CLI workflow: repo-local init, hook management, short-lived attribution sync, and diagnostics.
- `ae-cli` login selection is split between browser PKCE and device flow, but both paths still end in the same backend-issued JWT and `~/.ae-cli/token.json` storage model.
- The backend owns durable state, repo discovery during bootstrap, repo configuration, user/provider mapping, attribution, and SCM/webhook handling.
- The backend OAuth handler now manages both short-lived authorization codes and short-lived device entries in memory.
- Relay/sub2api remains the upstream auth/LLM/usage integration boundary and attribution fallback source.
- SCM providers now reference reusable credentials instead of storing raw secret blobs inline.
- `ae-cli start` is now a hidden legacy compatibility entrypoint. If invoked, it refers users to the sessionless workflow instead of remaining a formal primary path.
- Repo-to-`scm_provider` binding is now an admin-managed lifecycle step rather than a hard precondition for starting a session.
- SCM-dependent features such as scan, PR sync, optimize, and webhook registration require a bound repo and return `repo_unbound` when invoked before binding.

## Attribution Runtime Status

The local session proxy from `2026-04-02-local-session-proxy-design.md` remains in the codebase only as a compatibility/debug path. The formal workflow now uses the sessionless local attribution path that reads local tool artifacts and binds them to checkpoints without requiring a long-lived local daemon.

```mermaid
flowchart LR
    Codex["Codex"]
    Claude["Claude"]
    Hooks["Tool hooks + Git hooks"]
    Proxy["Local Session Proxy"]
    Queue["Local queue / runtime bundle"]
    Backend["ai-efficiency backend"]
    Relay["sub2api / relay"]

    Codex --> Proxy
    Claude --> Proxy
    Hooks --> Proxy
    Proxy --> Relay
    Proxy --> Queue
    Queue --> Backend
```

### Status

- Current formal CLI/runtime path:
  `ae-cli init`, `ae-cli sync`, and `ae-cli doctor`; local artifact parsing; hook-driven sync; `tool_usage_events`; checkpoint binding; PR attribution over checkpoint-bound usage
- Legacy compatibility/debug path:
  backend bootstrap, relay provider integration, session metadata, ae-cli-managed local proxy for Codex and Claude, session usage/session event ingest, and session-focused audit/debug surfaces
- Implemented sessionless pieces:
  `ae-cli` local artifact parsers for Codex JSONL, Claude JSONL, and Kiro JSON; short-lived local sync; git-hook-triggered sync; `tool_usage_events` ingest; checkpoint-time binding; PR attribution that can read checkpoint-bound `tool_usage_events`
- Remaining direction:
  richer Attribution UI, further shrinking legacy read surfaces, and eventual phase-2 deletion of session/local-proxy runtime code and routes

## Module Responsibilities

### Backend

| Area | Paths | Responsibility |
| --- | --- | --- |
| Auth and identity | `backend/internal/auth`, `backend/internal/oauth` | Relay SSO, LDAP auth, local token issuance, user identity mapping |
| Credentials | `backend/internal/credential` | Reusable encrypted secret assets, payload validation, provider credential migration, and credential masking |
| Relay integration | `backend/internal/relay` | Unified relay/sub2api adapter and usage/API key operations |
| SCM integration | `backend/internal/scm`, `backend/internal/webhook`, `backend/internal/prsync` | SCM provider abstraction, webhook ingestion, PR synchronization |
| Repo and analysis | `backend/internal/repo`, `backend/internal/analysis`, `backend/internal/efficiency` | Repo-to-provider binding, provider-backed clone/auth resolution, AI-friendliness scanning, efficiency aggregation and labeling |
| Session and attribution | `backend/internal/sessionbootstrap`, `backend/internal/checkpoint`, `backend/internal/attribution` | Session bootstrap lifecycle, commit checkpoints, PR/session attribution |
| API surface | `backend/internal/handler`, `backend/internal/middleware` | HTTP handlers, routing, auth middleware, settings endpoints |

### Frontend

| Area | Paths | Responsibility |
| --- | --- | --- |
| Views | `frontend/src/views` | Dashboard, repos, sessions, oauth, analysis-facing pages |
| Data access | `frontend/src/api`, `frontend/src/stores` | Backend API clients, state management, request orchestration |
| App shell | `frontend/src/components`, `frontend/src/router` | Layout, navigation, route composition |

### ae-cli

| Area | Paths | Responsibility |
| --- | --- | --- |
| Auth and backend access | `ae-cli/internal/auth`, `ae-cli/internal/client` | Login flow, backend API calls, token usage |
| Session runtime | `ae-cli/internal/session`, `ae-cli/internal/hooks`, `ae-cli/internal/collector` | Session lifecycle, workspace marker/hook management, local metadata collection |
| Tool execution | `ae-cli/internal/dispatcher`, `ae-cli/internal/router`, `ae-cli/internal/shell`, `ae-cli/internal/tmux` | Command dispatch, environment routing, shell/tmux integration |

## Documentation Expectations

Update this file when any of the following changes:

- component boundaries between frontend, backend, ae-cli, SCM, or relay
- runtime flow for login, session bootstrap, hooks, attribution, or local proxying
- source-of-truth precedence across the core specs

Also update the relevant spec in `docs/superpowers/specs/` when the change is contract-level rather than only diagram-level.
