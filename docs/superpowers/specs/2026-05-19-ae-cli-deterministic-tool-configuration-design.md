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
3. 选择受支持工具
   - 默认通过 `PATH` 自动检测 `codex`、`claude`、`gemini`；若 `codex` CLI 不存在，还会依次识别 macOS `ChatGPT.app` 和旧 `Codex.app`
   - 可通过可重复或逗号分隔的 `--tool <codex|claude|gemini>` 显式选择工具并跳过安装检测
4. 按工具对应的 relay `group.platform` 选择 credential
   - `codex` -> `openai`
   - `claude` -> `anthropic`
   - `gemini` -> `gemini`
5. 仅对已检测或显式选择且存在匹配 platform credential 的工具写入本地配置

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

- CLI 优先通过 `exec.LookPath` 检测本机是否安装 `codex`、`claude`、`gemini`。
- 对 Codex，若 `codex` CLI 不在 `PATH` 中，CLI 会按顺序继续检测 `~/Applications/ChatGPT.app`、`/Applications/ChatGPT.app`、`~/Applications/Codex.app` 和 `/Applications/Codex.app`。只安装当前 ChatGPT App 或旧 Codex App 时也应写入 `~/.codex/config.toml` 和 `~/.codex/auth.json`，因为 App 与 CLI 共用 `~/.codex` 配置目录。
- 一个或多个 `--tool` 值会显式选择受支持工具并跳过安装检测；该参数可重复，也接受逗号分隔值。显式选择仍要求存在匹配的 platform credential。
- 未传 `--tool` 时，未安装的工具不会报错，只会跳过。
- 已安装但没有匹配 platform credential 的工具也会跳过。例如选中的 provider 只有 `openai` group 时，CLI 只配置 Codex，不会改 Claude 或 Gemini。

## 2026-07-18 Explicit Tool Selection Follow-up

本节记录 2026-07-18 已实现并已并入上方 Current Contract 的显式工具选择与 ChatGPT app 检测扩展。

### Command contract

- `ae-cli discover` 新增可重复的 `--tool` 参数，只接受 `codex`、`claude`、`gemini`。
- Cobra/pflag 使用 string-array 语义原样保留每次 `--tool` occurrence；命令层再按逗号拆分、trim 并逐元素校验，因此以下两种写法等价：

  ```bash
  ae-cli discover --tool codex --tool claude
  ae-cli discover --tool codex,claude
  ```

- 未传 `--tool` 时，保持现有自动检测流程不变。
- 传入一个或多个 `--tool` 时，CLI 跳过本次安装检测，直接尝试配置显式指定的工具。该行为只覆盖工具选择，不改变 provider 或 credential 合同。
- 所有拆分后的元素都必须先完成校验；随后对重复工具名去重，并保持首次出现的顺序。
- 未知或空白工具名（包括 `--tool=`、逗号间的空元素、以及重复 flag 中的空白 occurrence）必须返回明确错误并列出支持的工具，不能静默跳过或回退到安装检测。
- 显式选择不能绕过 credential 校验。指定工具缺少匹配 platform credential 时，继续沿用现有跳过行为，不写对应配置。
- `--dry-run` 与 `--tool` 可以组合；此时输出目标路径但不写文件。

### macOS app detection

- Codex 的 macOS 桌面应用现以 `ChatGPT.app` 分发，但继续复用 `~/.codex/config.toml` 和 `~/.codex/auth.json`。
- 当 `codex` CLI 不在 `PATH` 中时，自动检测按以下顺序查找 app bundle：
  1. `~/Applications/ChatGPT.app`
  2. `/Applications/ChatGPT.app`
  3. `~/Applications/Codex.app`
  4. `/Applications/Codex.app`
- 保留 `Codex.app` 仅用于兼容旧安装；不改变 Claude 和 Gemini 的 PATH-only 检测合同。

### Implementation boundary

- `ae-cli/cmd/discover.go` 使用 `StringArrayVar` 保留每次 `--tool` 的原始 occurrence，并在命令层完成逗号拆分、校验和去重，把用户意图转换为 `toolconfig.InstalledTool` 列表。
- `ae-cli/internal/toolconfig.DetectInstalledTools` 只负责真实安装检测，不增加 force mode，避免把检测事实与显式用户选择混在同一个接口中。
- `ae-cli/internal/toolconfig.ConfigureTools` 继续只消费工具列表和 provider credential，不关心工具来自自动检测还是显式选择。

### Verification

- 命令测试通过 Cobra/pflag 边界覆盖单工具、多工具、逗号分隔、混合输入去重顺序、未知或显式空白工具、显式选择绕过安装检测，以及无 `--tool` 时继续自动检测。
- toolconfig 测试覆盖只安装 `ChatGPT.app` 时识别 Codex，并保留旧 `Codex.app` 回归。
- CLI help 和 mock discover E2E 必须展示并验证新的 `--tool` 合同。

### Config writes

#### Codex

- 写入 `~/.codex/config.toml`
- 当前写入字段：
  - `model_provider = <provider.name>`
  - `model = "gpt-5.4"`
  - `review_model = <model>`
  - `model_reasoning_effort = "xhigh"`
  - `disable_response_storage = true`
  - `network_access = "enabled"`
  - `windows_wsl_setup_acknowledged = true`
  - `model_context_window = 1000000`
  - `model_auto_compact_token_limit = 900000`
  - `[model_providers.<provider.name>]`
    - `name = <provider.name>`
    - `base_url = <provider.base_url>`
    - `wire_api = "responses"`
    - `requires_openai_auth = true`
  - `provider.name` 直接写入配置；若名称包含 TOML bare key 不支持的字符，由序列化器负责加引号
- API key 写入 `~/.codex/auth.json`：
  - `OPENAI_API_KEY = <openai credential.key>`
- 当 Codex 被成功配置时，CLI 会完整重写 `~/.codex/auth.json`，移除与 Codex 无关的其他字段。
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
- 当前 Gemini 3.1 使用要求通过命令输出解释 `GEMINI_MODEL` 的用途，并提示用户在当前 shell 重新加载对应 shell rc 后手动执行：
  - `source "$HOME/.zshrc"` / `source "$HOME/.bashrc"` / `source "$HOME/.profile"`，取决于本次写入的 rc 文件
  - `Set GEMINI_MODEL so Gemini starts with the preview model directly.`
  - `export GEMINI_MODEL="gemini-3.1-pro-preview"`
- `GEMINI_MODEL` 不写入 `~/.ae-cli/env.sh`。该变量只作为当前 shell 的运行提示，避免把预览模型选择持久化进 ae-cli 管理的 env 文件。
- 输出必须提醒用户不要在 Gemini 交互里手动切换模型，否则可能触发无 preview release channel 权限的模型访问错误。

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
