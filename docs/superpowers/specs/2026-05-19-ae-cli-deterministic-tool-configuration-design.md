# ae-cli Deterministic Tool Configuration Design

**Date:** 2026-05-19  
**Status:** Implemented current contract  
**Scope:** `ae-cli/`, `backend/internal/handler/provider.go`, `docs/`  
**Related:**  
- [2026-03-24-ae-cli-smart-tool-discovery-design.md](./2026-03-24-ae-cli-smart-tool-discovery-design.md)  
- [2026-03-24-oauth-cli-login-design.md](./2026-03-24-oauth-cli-login-design.md)

项目级架构总览见 [`docs/architecture.md`](../../architecture.md)。

## Spec Relationship

- 本文定义 **当前已实现** 的工具配置合同。
- [`2026-03-24-ae-cli-smart-tool-discovery-design.md`](./2026-03-24-ae-cli-smart-tool-discovery-design.md) 仍保留为更大范围的历史/目标设计：后端 LLM、多轮 `tool_call`、`/api/v1/tools/discover` 等能力当前仍未实现。
- 因此，关于“今天的 `ae-cli` 如何配置本地工具”，以本文为准；历史 smart-discovery spec 不应被误读为当前现状。

## Overview

当前实现提供一个确定性的用户入口：

```bash
ae-cli discover
```

该命令在登录后执行以下流程：

1. 调用 `GET /api/v1/providers`
2. 选择一个 provider
   - 默认取 `is_primary=true`
   - 可通过 `--provider <name>` 显式覆盖
3. 在本机 `PATH` 中检测受支持工具
   - `codex`
   - `claude`
   - `gemini`
4. 按工具的当前官方/本机配置机制写入本地配置

## Goals

1. 给当前代码库一个**真实可用**的工具配置入口，而不是停留在未实现 spec。
2. 避免引入后端 LLM 会话管理、多轮 discover 协议、或本地文件读取 tool-call 执行器。
3. 让 Codex、Claude、Gemini 都有明确的配置落点与测试覆盖。
4. 尽量复用现有 `GET /api/v1/providers` 能力，而不是新造临时后端端点。

## Non-Goals

1. 当前版本不实现 `/api/v1/tools/discover`
2. 当前版本不实现 `ae-cli login` 后自动执行 discover
3. 当前版本不做 per-tool provider inference
4. 当前版本不做 live model request 验证；只保证 CLI 配置文件/环境变量写入合同

## Current Contract

### Provider selection

- `ae-cli discover` 从 `GET /api/v1/providers` 获取用户可用 provider 列表。
- 如果用户传入 `--provider <name>`，则按 provider `name` / `display_name` 精确匹配。
- 否则优先使用 `is_primary=true` 的 provider；若不存在 primary，则回退到列表第一项。

### Tool detection

- CLI 仅通过 `exec.LookPath` 检测本机是否安装 `codex`、`claude`、`gemini`。
- 未安装的工具不会报错，只会跳过。

### Config writes

#### Codex

- 写入 `~/.codex/config.toml`
- 当前写入字段：
  - `openai_base_url = <provider.base_url>`
  - `model = <provider.default_model>`（当后端提供该字段时）
- API key 不直接写入 `config.toml`
- API key 写入 `~/.ae-cli/env.sh` 中的：
  - `OPENAI_API_KEY`

#### Claude

- 写入 `~/.claude/settings.json`
- 当前写入字段：
  - `env.ANTHROPIC_API_KEY = <provider.api_key>`
  - `env.ANTHROPIC_BASE_URL = <provider.base_url>`
  - `model = <provider.default_model>`（当后端提供该字段时）

#### Gemini

- 当前实现**不写** `~/.gemini/settings.json`
- API key / gateway URL 仅通过 `~/.ae-cli/env.sh` 提供：
  - `GEMINI_API_KEY`
  - `GOOGLE_GEMINI_BASE_URL`

#### Shared env bootstrap

- 当任一工具需要 shell environment 时，CLI 会维护：
  - `~/.ae-cli/env.sh`
- 并向当前 shell 对应的 rc 文件追加一次性 source 行：
  - `zsh` -> `~/.zshrc`
  - `bash` -> `~/.bashrc`
  - fallback -> `~/.profile`

## Safety / Merge Rules

- JSON / TOML 配置写入必须尽量保留用户已有字段
- `~/.ae-cli/env.sh` 使用带 marker 的 managed block，避免覆盖整文件
- shell rc source 行必须幂等，不可重复追加

## User Experience

### Normal run

```bash
ae-cli login
ae-cli discover
```

输出应列出：

- 选中的 provider
- 被配置的工具
- 各工具对应写入路径

### Dry run

```bash
ae-cli discover --dry-run
```

- 不写文件
- 仍输出将要配置的工具与目标路径

## Why this exists

当前仓库之前存在两个问题：

1. `ae-cli` 没有任何现行命令真正负责“把 provider 下发到本地工具”
2. 历史 smart-discovery spec 写得很完整，但代码里没有对应实现

因此本轮选择先落一个**确定性、可测试、已实现**的中间合同，为后续是否升级到 LLM discover 留出空间，但不再让“工具配置”停留在 spec-only 状态。
