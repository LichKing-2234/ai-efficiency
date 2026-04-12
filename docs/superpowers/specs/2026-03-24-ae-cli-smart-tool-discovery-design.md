# ae-cli 智能工具发现与自动配置 Design

**Status:** Proposed current contract; not yet implemented in the current codebase

**Implementation Note:** 当前仓库尚未实现 `/api/v1/tools/discover`、`/api/v1/tools/discover/continue`、`ae-cli discover` 命令，或 `ae-cli/internal/discover/` 包。本文仍是该功能的合同说明，不应被误读为“当前现状”。

## Overview

ae-cli 登录后，利用后端 LLM 能力自动发现用户本地安装的 AI 工具（如 Claude Code、Codex CLI 等），分析工具与 relay provider 的映射关系，并将 API key 和 base URL 直接写入各工具的原生配置文件。用户无需手动配置任何 AI 工具参数。

本 spec 依赖 **Spec 1: OAuth CLI Login & Auth System Enhancement** 中的 OAuth 登录流程和 `GET /api/v1/providers` API Key 下发端点。

**Spec Relationship:**
- 本文相对 [`2026-03-17-ai-efficiency-platform-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-17-ai-efficiency-platform-design.md) 将 ae-cli 工具发现、provider 映射和本地配置写入从平台级基线设计中拆成独立主题。
- 本文建立在 [`2026-03-24-oauth-cli-login-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md) 的 OAuth 登录与 provider delivery 合同之上，不重复定义登录与 provider 管理边界。
- 因此，本文只负责“登录后如何发现和配置工具”，而不回退去修改更早 spec 中对登录链路和 provider 抽象的定义。

## Scope

1. **本地工具发现** — 登录后通过后端 LLM（带本地文件访问 tools）自动检测已安装的 AI 工具
2. **工具-Provider 映射** — LLM 分析各工具应使用哪个 provider（默认取第一个/primary provider）
3. **原生配置写入** — LLM 决定如何写入各工具的原生配置文件（如 `~/.claude/settings.json`、环境变量等）
4. **Session 上报** — ae-cli 启动 session 时从发现缓存中读取 `relay_api_key_id`，上报给后端做用量关联

不包含：
- ae-cli 自身的 tools 配置（`config.yaml` 中的 `tools` 字段完全移除）
- Provider 管理（由 Spec 1 处理）

## Technical Decisions

| 决策 | 选择 | 理由 |
|------|------|------|
| 工具发现方式 | 后端 LLM + 本地文件访问 tools | LLM 可灵活识别各种工具安装方式（PATH、brew、npm global 等），无需硬编码检测逻辑 |
| Provider 映射策略 | 默认取 `is_primary=true` 的 provider | 简单可靠，v1 不需要复杂的多 provider 分配逻辑 |
| 配置写入方式 | LLM 决定 | 各工具配置格式不同（JSON、YAML、env），LLM 可适配各种格式，无需为每个工具硬编码 writer |
| 触发时机 | OAuth 登录成功后自动执行 | 确保用户拿到 provider 列表后立即配置工具，零手动操作 |
| `relay_api_key_id` 存储 | 直接存入 discovered_tools.json 缓存 | 整数 ID 不敏感，避免反向查找的脆弱性（用户可能手动修改工具配置） |

## 1. 整体流程

```
ae-cli login
  → OAuth 登录成功
  → 调用后端工具发现端点（POST /api/v1/tools/discover）
    → 后端自动加载用户的 provider 元数据，注入 LLM prompt
    → 后端 LLM 通过 tool_call 访问用户本地文件系统
    → 发现已安装的 AI 工具列表
    → 分析各工具应使用哪个 provider
    → 后端按 provider_name 回填各工具的配置写入方案（含 secret 与 relay_api_key_id）
  → ae-cli 执行配置写入（按 LLM 返回的方案）
  → 输出配置结果摘要
```

### 手动触发

`ae-cli discover` — 手动触发工具发现（不需要重新登录）。
`ae-cli discover --dry-run` — 仅显示发现结果和配置方案，不执行写入。

## 2. 本地工具发现

### 后端 LLM 端点

新增端点：

| Method | Path | 说明 | 权限 |
|--------|------|------|------|
| POST | `/api/v1/tools/discover` | 工具发现与配置生成（多轮协议起点） | auth |

### 请求/响应

请求体：
```json
{
  "local_context": {
    "os": "darwin",
    "arch": "arm64",
    "home_dir": "/Users/admin",
    "path_dirs": ["/usr/local/bin", "/opt/homebrew/bin", "..."]
  }
}
```

注意：ae-cli 不传 provider 信息。后端从数据库加载当前用户的 provider 元数据，在服务端注入到 LLM system prompt 中。provider secret 不进入 prompt；后端仅在生成最终 `config_actions` 时，按模型选定的 `provider_name` 回填 API key secret 与 `relay_api_key_id`。

响应体：
```json
{
  "discovered_tools": [
    {
      "name": "claude",
      "display_name": "Claude Code",
      "version": "1.2.3",
      "path": "/opt/homebrew/bin/claude",
      "provider_name": "sub2api-claude",
      "relay_api_key_id": 123,
      "config_actions": [
        {
          "type": "write_json",
          "path": "/Users/admin/.claude/settings.json",
          "merge_keys": {
            "env": {
              "ANTHROPIC_BASE_URL": "http://localhost:3000/v1",
              "ANTHROPIC_API_KEY": "sk-user-xxx"
            }
          }
        }
      ]
    },
    {
      "name": "codex",
      "display_name": "OpenAI Codex CLI",
      "version": "0.1.0",
      "path": "/usr/local/bin/codex",
      "provider_name": "sub2api-claude",
      "relay_api_key_id": 123,
      "config_actions": [
        {
          "type": "set_env",
          "file": "~/.ae-cli/env",
          "vars": {
            "OPENAI_BASE_URL": "http://localhost:3000/v1",
            "OPENAI_API_KEY": "sk-user-xxx"
          },
          "source_profile": "~/.zshrc"
        }
      ]
    }
  ]
}
```

注意：上面的响应体只用于说明最终 `complete` 结果的内容形状。真正的 HTTP 协议以本节后面的多轮 `status` 协议为准。

## Authoritative Protocol

`POST /api/v1/tools/discover` 发起多轮会话，`POST /api/v1/tools/discover/continue` 在同一个 `conversation_id` 上继续执行，直到响应进入 `tool_call_required`、`complete` 或 `error` 三种状态之一。

- 前文的一次性 `complete` 响应示例仅用于说明最终结果内容形状，不构成另一套独立 HTTP 合同。
- 最终 `complete` 响应必须包含每个工具的 `provider_name` 和 `relay_api_key_id`，以便 `~/.ae-cli/discovered_tools.json` 能持久化后续 session 关联所需的非敏感元数据。
- 缓存文件不得保存 API key secret；secret 只用于最终本地配置写入动作，不作为 discover cache 的一部分持久化。

### LLM 交互设计

后端使用 `relay.Provider.ChatCompletionWithTools` 调用 LLM，提供以下 tools 供 LLM 检测本地环境：

| Tool | 说明 | 实现方式 |
|------|------|---------|
| `check_command` | 检查命令是否存在及版本 | ae-cli 本地执行 `which <cmd>` + `<cmd> --version` |
| `read_file` | 读取本地文件内容 | ae-cli 本地读取文件 |
| `list_dir` | 列出目录内容 | ae-cli 本地执行 `ls` |

### 多轮 tool_call 协议

这些 tools 不是后端直接执行的，而是 LLM 发出 tool_call 后，ae-cli 在本地执行并将结果返回给后端 LLM 继续推理。

#### 端点定义

| Method | Path | 说明 | 权限 |
|--------|------|------|------|
| POST | `/api/v1/tools/discover` | 初始工具发现请求 | auth |
| POST | `/api/v1/tools/discover/continue` | 继续多轮 tool_call 循环 | auth |

#### 请求/响应协议

初始请求（`POST /api/v1/tools/discover`）：
```json
{
  "local_context": {
    "os": "darwin",
    "arch": "arm64",
    "home_dir": "/Users/admin",
    "path_dirs": ["/usr/local/bin", "/opt/homebrew/bin"]
  }
}
```

注意：不传 providers 信息。后端从数据库加载 provider 元数据并注入到 LLM prompt 中；provider secret 不经过网络传输，也不进入 prompt。

初始响应和继续响应使用统一的 discriminated response：

```json
// 情况 1：LLM 需要执行 tool_call
{
  "status": "tool_call_required",
  "conversation_id": "conv-uuid-xxx",
  "tool_calls": [
    {
      "id": "tc-1",
      "name": "check_command",
      "arguments": { "command": "claude" }
    }
  ]
}

// 情况 2：LLM 完成发现
{
  "status": "complete",
  "conversation_id": "conv-uuid-xxx",
  "discovered_tools": [
    {
      "name": "claude",
      "display_name": "Claude Code",
      "path": "/opt/homebrew/bin/claude",
      "provider_name": "sub2api-claude",
      "relay_api_key_id": 123,
      "config_actions": []
    }
  ]
}

// 情况 3：错误
{
  "status": "error",
  "conversation_id": "conv-uuid-xxx",
  "error": "LLM returned invalid response"
}
```

继续请求（`POST /api/v1/tools/discover/continue`）：
```json
{
  "conversation_id": "conv-uuid-xxx",
  "tool_results": [
    {
      "tool_call_id": "tc-1",
      "result": "claude found at /opt/homebrew/bin/claude, version 1.2.3"
    }
  ]
}
```

#### 约束

- **最大轮数**：20 轮。超过后后端返回 `status: "error"`，ae-cli 输出错误并跳过工具发现
- **单轮超时**：30 秒。LLM 响应超时后 ae-cli 重试一次，仍超时则中止
- **总超时**：3 分钟。整个发现流程的 wall-clock 上限
- **conversation_id**：后端生成的 UUID，用于关联多轮对话。后端在内存中维护 conversation 状态（LLM message history），5 分钟过期自动清理

#### 时序图

```
ae-cli → POST /api/v1/tools/discover { local_context }
  ← { status: "tool_call_required", conversation_id: "conv-1", tool_calls: [{check_command("claude")}] }
ae-cli 本地执行 which claude + claude --version
ae-cli → POST /api/v1/tools/discover/continue { conversation_id: "conv-1", tool_results: [...] }
  ← { status: "tool_call_required", conversation_id: "conv-1", tool_calls: [{read_file("~/.claude/settings.json")}] }
ae-cli 本地读取文件
ae-cli → POST /api/v1/tools/discover/continue { conversation_id: "conv-1", tool_results: [...] }
  ← { status: "complete", conversation_id: "conv-1", discovered_tools: [...] }
```

### LLM System Prompt

```
你是 ae-cli 的工具发现助手。你的任务是：
1. 检测用户本地安装了哪些 AI 编程工具（如 Claude Code、Codex CLI、Cursor 等）
2. 读取各工具的现有配置文件，了解当前配置状态
3. 根据提供的 provider 列表，为每个工具生成配置写入方案

检测策略：
- 使用 check_command 检查常见工具命令：claude, codex, cursor, aider, continue
- 使用 read_file 读取已知配置路径（如 ~/.claude/settings.json）
- 使用 list_dir 探索配置目录

配置写入规则：
- 优先使用工具的原生配置方式（配置文件 > 环境变量）
- 不覆盖用户已有的非 ae-cli 配置
- 使用 merge 而非 overwrite（保留用户其他配置项）
- 默认使用 is_primary=true 的 provider

输出格式：返回 discovered_tools JSON 数组。
```

### 安全约束

#### check_command 允许的命令名

仅允许以下命令：
- `claude`, `codex`, `cursor`, `aider`, `continue`, `copilot`, `cody`

ae-cli 验证 `arguments.command` 在此白名单内，否则返回 `"error: command not in allowlist"`。

#### read_file 允许的路径（glob 模式）

以 `$HOME` 为根的允许路径：
- `~/.claude/**`
- `~/.cursor/**`
- `~/.aider*`
- `~/.config/codex/**`
- `~/.config/continue/**`
- `~/.copilot/**`
- `~/.cody/**`
- `~/.zshrc`
- `~/.bashrc`
- `~/.bash_profile`
- `~/.profile`
- `~/.ae-cli/**`

ae-cli 将请求路径规范化后（resolve symlinks、expand ~）检查是否匹配白名单，否则返回 `"error: path not in allowlist"`。

#### list_dir 允许的目录

仅允许以下目录（深度 1 层，最多返回 100 条目）：
- `~/`
- `~/.claude/`
- `~/.cursor/`
- `~/.config/`
- `~/.config/codex/`
- `~/.config/continue/`

ae-cli 验证目录在白名单内，且限制 `depth=1`、`max_entries=100`。

#### 通用规则

- ae-cli 在执行任何 tool_call 前验证参数合法性
- 所有 tool_call 执行结果截断到 10KB，防止 LLM context 溢出
- tool_call 执行超时 5 秒，超时返回 `"error: execution timeout"`

## 3. 配置写入

### 支持的 config_action 类型

| type | 说明 | 示例 |
|------|------|------|
| `write_json` | 合并写入 JSON 配置文件 | Claude Code `settings.json` |
| `write_yaml` | 合并写入 YAML 配置文件 | — |
| `set_env` | 写入 `~/.ae-cli/env` 并在 shell profile 中注入 source 行 | Codex CLI 的 `OPENAI_API_KEY` |

### 写入逻辑

ae-cli 收到 `config_actions` 后在本地执行：

1. `write_json`：读取现有文件 → deep merge `merge_keys` → 写回（保留用户其他配置）
2. `write_yaml`：同上，YAML 格式
3. `set_env`：
   - 将环境变量写入 `~/.ae-cli/env` 文件（格式：`export KEY=VALUE`，每行一个）
   - 检查用户的 shell profile（`~/.zshrc`、`~/.bashrc` 等）是否已有 `source ~/.ae-cli/env` 行
   - 如果没有，追加一行：`# ae-cli managed\nsource ~/.ae-cli/env`（仅追加一次）
   - 后续更新只修改 `~/.ae-cli/env`，不再触碰 shell profile
   - 好处：隔离 ae-cli 的环境变量，清理时只需删除 `~/.ae-cli/env` 和 profile 中的 source 行

所有写入操作：
- 写入前备份原文件（`.bak`）
- 原子写入（write-then-rename）
- 文件权限 0600（包含 API key）
- 写入后输出变更摘要供用户确认
- 默认交互模式：写入前显示变更摘要，等待用户确认（y/n）
- `ae-cli login --yes`：跳过确认，自动写入（用于 CI/自动化场景）

### 错误处理

| 场景 | 行为 |
|------|------|
| LLM 返回格式错误的 config_actions | 跳过该工具，warn 日志，继续处理其他工具 |
| 现有配置文件 JSON/YAML 解析失败 | 跳过该工具，提示用户手动检查文件 |
| 备份文件已存在（上次失败残留） | 覆盖旧备份，继续写入 |
| 文件系统只读或权限不足 | 跳过该工具，输出错误信息 |
| 部分工具写入成功、部分失败 | 已成功的保留，失败的输出摘要，缓存中仅记录成功的工具 |

### 已知工具配置路径

| 工具 | 配置方式 | 路径 |
|------|---------|------|
| Claude Code | JSON 配置 | `~/.claude/settings.json` 中的 `env` 字段 |
| Codex CLI | 环境变量 | `OPENAI_BASE_URL` + `OPENAI_API_KEY` |
| Cursor | JSON 配置 | `~/.cursor/config.json` |
| Aider | YAML 配置 | `~/.aider.conf.yml` |

这些路径作为 LLM 的参考知识，LLM 可根据实际检测结果调整。

## 4. Session relay_api_key_id 上报

### 设计

ae-cli 启动 session 时，需要上报当前使用的 provider 和 relay API key 信息，用于后端做用量关联。

工具发现完成后，`discovered_tools.json` 缓存中直接存储 `relay_api_key_id`（整数，不敏感），session 启动时直接读取，无需反向查找。

### 流程

```
ae-cli start
  → 读取本地已发现的工具列表（缓存在 ~/.ae-cli/discovered_tools.json）
  → 直接从缓存中读取各工具的 provider_name 和 relay_api_key_id
  → 创建 session 时上报：
    {
      "id": "...",
      "repo_full_name": "...",
      "branch": "...",
      "tool_configs": [
        {
          "tool_name": "claude",
          "provider_name": "sub2api-claude",
          "relay_api_key_id": 123
        }
      ]
    }
```

### 工具发现结果缓存

`~/.ae-cli/discovered_tools.json`：

```json
{
  "discovered_at": "2026-03-24T19:36:00Z",
  "tools": [
    {
      "name": "claude",
      "display_name": "Claude Code",
      "provider_name": "sub2api-claude",
      "relay_api_key_id": 123,
      "config_path": "/Users/admin/.claude/settings.json"
    }
  ]
}
```

ae-cli 启动时检查 `discovered_at` 是否过期（> 7 天），过期则提示用户运行 `ae-cli discover` 重新发现。可通过 `ae-cli discover --refresh` 强制刷新。

## 5. ae-cli 包结构变更

```
ae-cli/
├── internal/
│   ├── discover/
│   │   ├── discover.go     # 工具发现主流程（LLM 多轮 tool_call 循环）
│   │   ├── executor.go     # 本地 tool_call 执行器（check_command、read_file、list_dir）
│   │   ├── actions.go      # config_action 执行器（write_json、write_yaml、set_env）
│   │   └── cache.go        # discovered_tools.json 缓存管理
│   └── session/
│       └── reporter.go     # session 创建时的 relay_api_key_id 上报逻辑
└── config/
    └── config.go           # 移除 Sub2apiConfig、ToolConfig，简化为最小配置
```

### config.go 简化（目标态）

说明：这一节描述的是设计目标，不是当前代码已经完成的状态。当前 ae-cli 代码仍保留 legacy config 读取逻辑，相关清理需要单独实施。

```go
// 移除 ToolConfig、Sub2apiConfig、ServerConfig
// ae-cli 不再需要 config.yaml

// Config 仅保留极少量本地配置（如有需要）
type Config struct {
    // 未来可扩展，当前为空
}
```

所有配置来源：
- Server URL：编译时注入（`buildinfo.ServerURL`）
- Token：`~/.ae-cli/token.json`
- Provider 列表：`GET /api/v1/providers`（登录时获取）
- 工具配置：各工具原生配置文件（LLM 自动写入）
- 工具发现缓存：`~/.ae-cli/discovered_tools.json`

## Dependencies

```
# 后端无新增依赖（复用 Spec 1 的 relay.Provider）

# ae-cli 无新增依赖（JSON/YAML 操作使用标准库 + 已有依赖）
```

## Security Considerations

- LLM tool_call 执行受白名单约束，不允许读取任意文件或执行任意命令
- API key 写入的配置文件权限设为 0600
- 配置写入前备份原文件
- `discovered_tools.json` 不存储 API key 明文，仅存储 `relay_api_key_id`（整数）
- Provider 元数据可用于后端 LLM prompt，但 provider secret 只在最终配置回填阶段使用，不经过 ae-cli ↔ 后端的多轮通信
- 环境变量集中存储在 `~/.ae-cli/env`，shell profile 仅注入一行 `source` 引用，便于清理
- 多轮 tool_call 循环有最大轮数（20）和总超时（3 分钟）限制，防止资源耗尽

## Known Limitations (v1)

- 工具发现依赖后端 LLM 可用性，离线场景不可用
- LLM 可能无法识别非主流或自定义安装的 AI 工具
- 多轮 tool_call 循环可能较慢（取决于 LLM 响应速度和检测轮数）
- 环境变量方式配置的工具需要用户重新加载 shell（`source ~/.ae-cli/env`）或开启新终端才能生效
- v1 仅支持单 provider 映射（所有工具使用同一个 primary provider）

## Verification

当前代码尚未实现本 spec 的功能面。功能落地时，应至少完成以下验证：

- Backend: `cd backend && go test ./...`
- ae-cli: `cd ae-cli && go test ./...`
- Manual acceptance:
  - 初始 discover 请求返回 `tool_call_required` 或 `complete`
  - continue 请求能在同一 `conversation_id` 上推进到 `complete`
  - 最终 `complete` payload 包含 `provider_name` 和 `relay_api_key_id`
  - `~/.ae-cli/discovered_tools.json` 不保存 API key 明文
