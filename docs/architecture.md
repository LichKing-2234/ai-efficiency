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
    DB[("ai_efficiency database<br/>SQLite dev / PostgreSQL prod")]
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
- The frontend is built separately and packaged into the backend image during Docker build. Do not assume Go `embed` or a single self-contained binary unless the code is changed to do that explicitly.
- Official production deployment is now Docker Compose-first.
- The business entrypoint remains the backend service that also serves the frontend bundle.
- A dedicated updater sidecar performs privileged deployment update and rollback operations over the local Docker/Compose control path.
- Public health endpoints expose liveness/readiness, and admin settings expose deployment status plus update controls.

## Current Production Deployment

The current deployment path is organized around one backend image plus a small updater sidecar.

```mermaid
flowchart LR
    Browser["Browser"]
    Backend["Backend + Frontend bundle"]
    Updater["Updater sidecar"]
    DB[("Postgres")]
    Redis[("Redis")]
    Relay["sub2api / relay"]
    DockerHost["Docker / Compose host"]

    Browser --> Backend
    Backend --> DB
    Backend --> Redis
    Backend --> Relay
    Backend --> Updater
    Updater --> DockerHost
```

### Deployment Notes

- Official deploy assets live under `deploy/`.
- `deploy/docker-compose.yml` is the bundled-infra path.
- `deploy/docker-compose.external.yml` is the external-infra path.
- `deploy/docker-deploy.sh` is the preflight entrypoint.
- `deploy/.env.example` is the operator-facing configuration template.
- Backend deployment status and update APIs are now first-class admin surfaces.

## Current Runtime Flow

The implemented runtime centers on backend bootstrap plus a session-bound local proxy started by `ae-cli`.

```mermaid
sequenceDiagram
    participant Dev as Developer
    participant CLI as ae-cli
    participant BE as Backend
    participant Proxy as Local Session Proxy
    participant Relay as Relay / sub2api
    participant WS as Workspace + Hooks
    participant Tool as AI Tooling

    Dev->>CLI: ae-cli login / start
    CLI->>BE: OAuth + session bootstrap
    BE->>Relay: resolve relay identity / manage API key
    Relay-->>BE: user + key metadata
    BE-->>CLI: session metadata + env bundle
    CLI->>Proxy: start proxy with session runtime config
    CLI->>WS: write marker / install hooks / start collectors
    Dev->>Tool: run Codex / Claude / other tools
    Tool->>Proxy: local OpenAI / Anthropic request
    Proxy->>Relay: upstream model request
    Proxy->>BE: session usage + session events\n(spool fallback on failure)
    WS->>BE: checkpoint events + runtime metadata
    BE->>Relay: relay usage lookup fallback for attribution
```

### Runtime Boundaries

- `ae-cli` owns local session setup, workspace state, hooks, collector wiring, and the lifecycle of the local session proxy.
- The backend owns durable state, repo configuration, user/provider mapping, attribution, and SCM/webhook handling.
- Relay/sub2api remains the upstream auth/LLM/usage integration boundary and attribution fallback source.

## Local Session Proxy Rollout

The local session proxy from `2026-04-02-local-session-proxy-design.md` is now partially implemented in the current codebase. `ae-cli start` boots a session-bound proxy for Codex and Claude, but the broader proxy-first attribution model is still in progress.

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

- Current: backend bootstrap, relay provider integration, session metadata, ae-cli-managed local proxy for Codex and Claude, session usage/session event ingest, checkpoints, attribution services
- Remaining direction: broader tool coverage, more unified event ingress semantics, and richer local usage facts so attribution depends less on relay fallback

## Module Responsibilities

### Backend

| Area | Paths | Responsibility |
| --- | --- | --- |
| Auth and identity | `backend/internal/auth`, `backend/internal/oauth` | Relay SSO, LDAP auth, local token issuance, user identity mapping |
| Relay integration | `backend/internal/relay` | Unified relay/sub2api adapter and usage/API key operations |
| SCM integration | `backend/internal/scm`, `backend/internal/webhook`, `backend/internal/prsync` | SCM provider abstraction, webhook ingestion, PR synchronization |
| Repo and analysis | `backend/internal/repo`, `backend/internal/analysis`, `backend/internal/efficiency` | Repo config, AI-friendliness scanning, efficiency aggregation and labeling |
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
