# AI Efficiency Platform

## Quick Reference

- Tech stack: Go 1.24.x toolchain (Gin + Ent) backend, Vue 3 (Vite + TailwindCSS + Pinia) frontend
- Architecture overview: `docs/architecture.md`
- Current behavior contracts: `docs/contracts/README.md`
- Historical rationale and evidence: `docs/history/README.md`; never use history as current behavior or backlog
- Unimplemented target state and work status: GitHub Issues
- Independent architectural rationale: `docs/adr/` when warranted
- Domain vocabulary: tracked root `CONTEXT.md` when present and relevant
- Platform Sessions and the local session proxy are retired
- Default verification:
  - `cd backend && go test ./...`
  - `cd ae-cli && go test ./...`
  - `cd frontend && npm test`
  - `cd frontend && npm run test:e2e:role`
- Primary remote: `https://github.com/LichKing-2234/ai-efficiency.git`
- Release units:
  - Platform: `v*` tags publish backend/frontend/deploy, GHCR image, and Helm inputs.
  - CLI: `ae-cli/v*` tags publish only `ae-cli`; no GHCR image and no Helm rollout.
  - Repository `/releases/latest` stays platform-owned.
  - Bridge: `v0.2.0-cli.1` is the one-time legacy CLI migration exception. Publish it only with the CLI bridge workflow; do not run Helm for it.

## Agent skills

### Issue tracker

Issues and PRDs are tracked in GitHub Issues for `LichKing-2234/ai-efficiency`; PRs are not a request surface. See `docs/agents/issue-tracker.md`.

### Triage labels

Use the five canonical triage labels without aliases. See `docs/agents/triage-labels.md`.

### Domain docs

Use the single-context domain documentation layout. See `docs/agents/domain.md`.

## Commit Convention

Strictly follow Conventional Commits. See AGENTS.md for full spec.

```
<type>(<scope>): <subject>
```

Types: feat, fix, docs, refactor, test, chore, perf
Scopes: backend, frontend, ae-cli, deploy, docs, scm, auth, gating, analysis, efficiency, webhook

## Code Style

- Go: `gofmt`, tabs, standard project layout
- Vue: `<script setup lang="ts">`, Composition API, TailwindCSS
- All files: UTF-8, LF line endings, trailing newline

## Do NOT

- Modify sub2api source code — this project is independent
- Introduce new direct sub2api DB coupling when existing relay/provider APIs already cover the integration
- Commit secrets, config.yaml, or .env files
