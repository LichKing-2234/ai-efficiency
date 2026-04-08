# AI Efficiency Platform

## Quick Reference

- Tech stack: Go 1.26+ (Gin + Ent) backend, Vue 3 (Vite + TailwindCSS + Pinia) frontend
- Architecture overview: `docs/architecture.md`
- Design specs: `docs/superpowers/specs/`
- Implementation plans: `docs/superpowers/plans/`
- Current auth/provider baseline: `docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md`
- Latest session attribution draft: `docs/superpowers/specs/2026-03-26-session-pr-attribution-design.md`
- Local session proxy remains a draft in `docs/superpowers/specs/2026-04-02-local-session-proxy-design.md` unless code proves otherwise
- Primary remote: `https://github.com/LichKing-2234/ai-efficiency.git`
- GitLab legacy remote: `ssh://git@git.agoralab.co/ai/ai-efficiency.git`

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
