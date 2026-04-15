# AI Efficiency Platform

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![GitHub stars](https://img.shields.io/github/stars/LichKing-2234/ai-efficiency?style=social)](https://github.com/LichKing-2234/ai-efficiency/stargazers)

[简体中文](README.zh-CN.md)

AI Efficiency Platform (`ai-efficiency`) is a standalone system for measuring and improving AI-assisted software development efficiency.

## Overview

- Backend: Go (`Gin` + `Ent`) modular monolith
- Frontend: Vue 3 (`Vite` + `Pinia` + `TailwindCSS`)
- CLI: `ae-cli` for login, session bootstrap, hooks, collectors, and local tool runtime
- Relay integration: HTTP provider boundary to `sub2api`, not direct DB coupling
- SCM integration: unified provider interface for GitHub and Bitbucket Server

## Current Runtime

- The backend is the central orchestration point for auth, repo management, analysis, attribution, deployment control, and webhook handling.
- The frontend is built separately and embedded into the backend binary for deployment.
- `ae-cli start` bootstraps a session with the backend, writes local workspace/runtime state, and can start a local session proxy for Codex and Claude.
- Production deployment currently supports Docker Compose and Linux systemd.

## Repository Layout

```text
ai-efficiency/
├── backend/    # Go backend
├── frontend/   # Vue frontend
├── ae-cli/     # CLI runtime and commands
├── deploy/     # Deployment assets
├── docs/       # Architecture and specs
├── AGENTS.md   # Agent working rules
└── CLAUDE.md   # Lightweight navigation notes
```

## Key Documents

- Architecture overview: [`docs/architecture.md`](docs/architecture.md)
- Current topic specs: `docs/superpowers/specs/`
- CLI install and usage: [`ae-cli/README.md`](ae-cli/README.md)
- Deployment guide: [`deploy/README.md`](deploy/README.md)
- License: [`LICENSE`](LICENSE)
- Agent collaboration rules: [`AGENTS.md`](AGENTS.md)

## Source Of Truth

When code, specs, and historical documents disagree, prefer:

1. Current code
2. The newest relevant spec in `docs/superpowers/specs/`
3. [`docs/architecture.md`](docs/architecture.md)
4. Historical baseline documents

## Development

### Verify

```bash
cd backend && go test ./...
cd ae-cli && go test ./...
cd frontend && pnpm test
cd frontend && pnpm build
```

### Common Entry Points

- Backend server: `cd backend && go run ./cmd/server`
- CLI: `cd ae-cli && go run .`
- Frontend dev server: `cd frontend && pnpm dev`

## License

This project is open-sourced under the MIT License. See [`LICENSE`](LICENSE).

## Notes

- This README is the primary English entry point.
- For current runtime boundaries and module responsibilities, read [`docs/architecture.md`](docs/architecture.md).
- For feature-level behavior, prefer the latest spec over historical plans.

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=LichKing-2234/ai-efficiency&type=Date)](https://star-history.com/#LichKing-2234/ai-efficiency&Date)
