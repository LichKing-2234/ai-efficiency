# AI Efficiency Platform

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![GitHub stars](https://img.shields.io/github/stars/LichKing-2234/ai-efficiency?style=social)](https://github.com/LichKing-2234/ai-efficiency/stargazers)

[简体中文](README.zh-CN.md)

AI Efficiency Platform (`ai-efficiency`) is a standalone system for measuring and improving AI-assisted software development efficiency.

## Overview

- Backend: Go (`Gin` + `Ent`) modular monolith
- Frontend: Vue 3 (`Vite` + `Pinia` + `TailwindCSS`)
- CLI: `ae-cli` for login, provider discovery, hooks, collectors, and local tool configuration
- Relay integration: HTTP provider boundary to `sub2api`, not direct DB coupling
- SCM integration: unified provider interface for GitHub and Bitbucket Server

## Current Runtime

- The backend is the central orchestration point for auth, repo management, analysis, attribution, deployment health/version visibility, and webhook handling.
- The frontend is built separately and embedded into the backend binary for deployment.
- The formal CLI workflow is now sessionless: `ae-cli init`, `ae-cli sync`, and `ae-cli doctor`.
- Legacy `ae-cli start/stop/run/...` session commands are no longer shipped in the current CLI binary, and the local-proxy runtime is retired.
- Production deployment currently supports Docker Compose and Linux systemd.

## Repository Layout

```text
ai-efficiency/
├── backend/    # Go backend
├── frontend/   # Vue frontend
├── ae-cli/     # CLI runtime and commands
├── deploy/     # Deployment assets
├── docs/       # Architecture, current contracts, and history
├── AGENTS.md   # Agent working rules
└── CLAUDE.md   # Lightweight navigation notes
```

## Key Documents

- Architecture overview: [`docs/architecture.md`](docs/architecture.md)
- Current behavior contracts: [`docs/contracts/`](docs/contracts/README.md)
- Historical rationale and evidence: [`docs/history/`](docs/history/README.md)
- CLI install and usage: [`ae-cli/README.md`](ae-cli/README.md)
- Deployment guide: [`deploy/README.md`](deploy/README.md)
- License: [`LICENSE`](LICENSE)
- Agent collaboration rules: [`AGENTS.md`](AGENTS.md)

## Source Of Truth

When code, contracts, and architecture documents disagree, prefer:

1. Current code
2. The relevant current contract in [`docs/contracts/`](docs/contracts/README.md)
3. [`docs/architecture.md`](docs/architecture.md)

GitHub Issues own unimplemented target state and work status. ADRs preserve
independently useful architectural rationale, tracked root `CONTEXT.md` owns
domain vocabulary when present, and `docs/history/` is never current behavior
or backlog.

## Development

### Verify

```bash
cd backend && go test ./...
cd ae-cli && go test ./...
cd frontend && npm test
cd frontend && npm run build
```

### Common Entry Points

- Backend server: `cd backend && go run ./cmd/server`
- CLI: `cd ae-cli && go run .`
- Frontend dev server: `cd frontend && npm run dev`

## License

This project is open-sourced under the MIT License. See [`LICENSE`](LICENSE).

## Notes

- This README is the primary English entry point.
- For current runtime boundaries and module responsibilities, read [`docs/architecture.md`](docs/architecture.md).
- For feature-level behavior, prefer the latest spec over historical plans.

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=LichKing-2234/ai-efficiency&type=Date)](https://star-history.com/#LichKing-2234/ai-efficiency&Date)
