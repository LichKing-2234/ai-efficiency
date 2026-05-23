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

1. 调用 `GET /api/v1/user/providers` 获取当前用户可用的 provider credential；若后端尚未提供该接口，兼容回退到旧 `GET /api/v1/providers`
2. 选择一个 provider
   - 默认取 `is_primary=true`
   - 可通过 `--provider <name>` 显式覆盖
3. 在本机 `PATH` 中检测受支持工具
   - `codex`
   - `claude`
   - `gemini`
4. 按工具对应的 relay `group.platform` 选择 credential
   - `codex` -> `openai`
   - `claude` -> `anthropic`
   - `gemini` -> `gemini`
5. 仅对已安装且存在匹配 platform credential 的工具写入本地配置

## Goals

1. 给当前代码库一个**真实可用**的工具配置入口，而不是停留在未实现 spec。
2. 避免引入后端 LLM 会话管理、多轮 discover 协议、或本地文件读取 tool-call 执行器。
3. 让 Codex、Claude、Gemini 都有明确的配置落点与测试覆盖。
4. 复用当前 `/user` 自助 credential 合同，避免继续依赖旧 provider 级自动创建 key 的接口语义。

## Non-Goals

1. 当前版本不实现 `/api/v1/tools/discover`
2. 当前版本不实现 `ae-cli login` 后自动执行 discover
3. 当前版本不做 LLM 驱动的 per-tool provider inference；仅按后端返回的 `group.platform` 做确定性匹配
4. 当前版本不做 live model request 验证；只保证 CLI 配置文件/环境变量写入合同

## Current Contract

### Provider selection

- `ae-cli discover` 优先从 `GET /api/v1/user/providers` 获取用户可用 provider 列表和 group-scoped credential。
- 对当前 `/user/providers` 合同，CLI 保留每个 `provider + group.platform` 的 active credential，不再把第一个 credential 套用到所有工具。
- 若后端尚未实现 `/api/v1/user/providers`，CLI 可回退到旧 `GET /api/v1/providers`，以兼容旧部署；旧接口没有 group/platform 语义时，仍按 provider-level API key 走历史配置行为。
- 如果用户传入 `--provider <name>`，则按 provider `name` / `display_name` 精确匹配。
- 否则优先使用 `is_primary=true` 的 provider；若不存在 primary，则回退到列表第一项。

### Tool detection

- CLI 仅通过 `exec.LookPath` 检测本机是否安装 `codex`、`claude`、`gemini`。
- 未安装的工具不会报错，只会跳过。
- 已安装但没有匹配 platform credential 的工具也会跳过。例如选中的 provider 只有 `openai` group 时，CLI 只配置 Codex，不会改 Claude 或 Gemini。

### Config writes

#### Codex

- 写入 `~/.codex/config.toml`
- 当前写入字段：
  - `model_provider = "OpenAI"`
  - `model = "gpt-5.4"`
  - `review_model = <model>`
  - `model_reasoning_effort = "xhigh"`
  - `disable_response_storage = true`
  - `network_access = "enabled"`
  - `windows_wsl_setup_acknowledged = true`
  - `model_context_window = 1000000`
  - `model_auto_compact_token_limit = 900000`
  - `[model_providers.OpenAI]`
    - `name = "OpenAI"`
    - `base_url = <provider.base_url>`
    - `wire_api = "responses"`
    - `requires_openai_auth = true`
- API key 写入 `~/.codex/auth.json`：
  - `OPENAI_API_KEY = <openai credential.key>`
- Codex 不再通过 `~/.ae-cli/env.sh` 写入或提示 `OPENAI_API_KEY`。
- Codex 不使用 provider 级 `default_model`。该字段不具备 openai group 专属语义，不能自动套用到 Codex 的 `model` / `review_model`。

#### Claude

- 写入 `~/.claude/settings.json`
- 当前写入字段：
  - `env.ANTHROPIC_BASE_URL = <provider.base_url>`
  - `env.ANTHROPIC_AUTH_TOKEN = <anthropic credential.key>`
  - `env.CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC = "1"`
  - `env.CLAUDE_CODE_ATTRIBUTION_HEADER = "0"`
- CLI 不再为 Claude 写入 top-level `model` 字段。

#### Gemini

- 当前实现**不写** `~/.gemini/settings.json`
- API key / gateway URL 仅通过 `~/.ae-cli/env.sh` 提供：
  - `GEMINI_API_KEY = <gemini credential.key>`
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
