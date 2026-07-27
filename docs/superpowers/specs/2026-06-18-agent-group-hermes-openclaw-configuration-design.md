# Agent Group Hermes/OpenClaw Configuration Design

**Date:** 2026-06-18
**Status:** Implemented current contract
**Scope:** `frontend/src/views/UserView.vue`, `frontend/src/utils/userSetupReview.ts`, `frontend/src/i18n.ts`, `frontend/src/__tests__/user-setup-review.test.ts`, `frontend/src/__tests__/user-view.test.ts`, `docs/architecture.md`
**Related:**
- [2026-06-14-user-api-key-first-onboarding-design.md](./2026-06-14-user-api-key-first-onboarding-design.md)
- [2026-05-26-user-cli-setup-checklist-design.md](./2026-05-26-user-cli-setup-checklist-design.md)
- [2026-05-19-ae-cli-deterministic-tool-configuration-design.md](./2026-05-19-ae-cli-deterministic-tool-configuration-design.md)
- [docs/architecture.md](../../architecture.md)

## Spec Relationship

- 本文定义 `/user` 页面在 `Agent` 接入组下的配置方式扩展合同。
- 本文继承 [2026-06-14-user-api-key-first-onboarding-design.md](./2026-06-14-user-api-key-first-onboarding-design.md) 的 API-key-first 主流程：选择接入组、创建 API key、运行连接测试、选择配置方式。
- 本文不改变 `ae-cli discover` 当前合同。[2026-05-19-ae-cli-deterministic-tool-configuration-design.md](./2026-05-19-ae-cli-deterministic-tool-configuration-design.md) 仍然只覆盖当前已实现的 Codex、Claude Code、Gemini 本机配置写入。
- 本文不回写历史 spec 正文；历史 spec 保留当时的设计背景。
- 本文中的 CC Switch、Hermes Agent、OpenClaw 外部能力判断基于 2026-06-18 的官方文档/仓库核对，后续实现时如上游合同变化，应以当时的上游文档和当前代码为准。

## External References

- CC Switch deep link protocol: <https://github.com/farion1231/cc-switch/blob/main/docs/user-manual/en/5-faq/5.3-deeplink.md>
- CC Switch deep link parser source: <https://github.com/farion1231/cc-switch/blob/main/src-tauri/src/deeplink/parser.rs>
- CC Switch provider import source: <https://github.com/farion1231/cc-switch/blob/main/src-tauri/src/deeplink/provider.rs>
- CC Switch Hermes support release note: <https://github.com/farion1231/cc-switch/blob/main/docs/release-notes/v3.14.0-en.md>
- CC Switch Claude Desktop deep link drift issue: <https://github.com/farion1231/cc-switch/issues/3112>
- Hermes Agent configuration: <https://hermes-agent.nousresearch.com/docs/user-guide/configuration>
- Hermes Agent CLI commands: <https://hermes-agent.nousresearch.com/docs/reference/cli-commands>
- Hermes Agent providers: <https://hermes-agent.nousresearch.com/docs/integrations/providers>
- OpenClaw onboard CLI: <https://docs.openclaw.ai/cli/onboard>
- OpenClaw config CLI: <https://docs.openclaw.ai/cli/config>
- OpenClaw configuration reference: <https://docs.openclaw.ai/gateway/configuration-reference>

## Problem

`/user` 现在按配置方式组织本机工具接入：

1. 手动配置
2. 自动配置
3. CC Switch 配置

这个结构适合普通 Codex、Claude Code、Gemini 工具配置，但对 Agent 类接入组不够准确：

1. `Agent` 接入组的目标客户端是 Hermes Agent、OpenClaw 和自定义 Agent，而不是 Codex、Claude Code、Gemini。
2. 如果 Agent 接入组继续显示 Codex/Claude/Gemini 片段，会让用户误把 Agent 专用额度或路由配置进普通开发工具。
3. Hermes/OpenClaw 的官方接入主要是 CLI onboarding、配置文件、或 CC Switch 管理导入；这些命令式配置不应该归入 `ae-cli 自动配置`。
4. CC Switch deep link 是 CC Switch 自己的导入协议，不等同于 Hermes/OpenClaw 官方是否提供自有 deep link。CC Switch 当前源码已接受 `app=openclaw` 和 `app=hermes`，且 provider import 对 OpenClaw/Hermes 的默认协议模式正好是 Agent 分支需要的 OpenAI-compatible Chat Completions。
5. Claude Desktop 虽已被 CC Switch 管理，但当前不应作为稳定 deep link target 出现在 `/user`。

结果是：页面需要在普通接入组和 Agent 接入组之间明确分流，避免同一个配置区域同时混入普通工具和 Agent 客户端。

## Goals

1. 当且仅当当前接入组名称严格以 `Agent` 开头时，启用 Agent 客户端配置体验。
2. `Agent` 接入组只展示 Hermes Agent、OpenClaw 和 Custom Agent，不展示 Codex、Claude Code、Gemini 配置或导入。
3. 普通接入组保持现状，继续展示 Codex、Claude Code、Gemini 对应的手动配置、CC Switch 导入和 `ae-cli` 自动配置路径。
4. 将 Hermes/OpenClaw 的命令式 onboarding 和文件片段归入 `手动配置`。
5. 将 Hermes/OpenClaw 的 CC Switch provider import 归入 `应用导入`，不伪装成官方自有 deep link；对所有 Agent platform 明确说明导入使用 OpenAI-compatible `/v1` endpoint。
6. 对 Custom Agent 提供平台化通用模板，用于任何支持当前协议的自定义 Agent。
7. 不改变后端 provider/group/API key 合同，不新增后端 API。
8. 不改变 `ae-cli discover`、hooks、repo attribution 或 doctor 的命令语义。

## Non-Goals

1. 不在本轮实现 `ae-cli discover` 对 Hermes Agent 或 OpenClaw 的本地检测/写入。
2. 不新增浏览器到本机 CLI 的执行桥，不在浏览器里直接执行 Hermes/OpenClaw 命令。
3. 不把 Hermes/OpenClaw 的官方 CLI onboarding 归类为自动配置；用户复制并执行命令仍属于手动配置。
4. 不为 Claude Desktop 生成 CC Switch deep link。Claude Desktop 可以由 CC Switch 应用内管理，但当前不是 `/user` 生成链接的稳定合同。
5. 不把非 `Agent` 前缀的 group 名称做模糊匹配。`agent*`、`MyAgent*` 都不触发 Agent 配置体验。
6. 不改变 API key 默认隐藏和敏感复制确认规则。
7. 不在本轮引入后端 capability flag。后续如果需要，可用后端显式 capability 替换前端前缀判断。
8. 不承诺 CC Switch deep link 能完整设置 Hermes/OpenClaw 的所有 provider protocol 字段；手动配置仍是非默认协议的准确配置入口。

## User Decisions Captured

本轮设计已确认以下用户决策：

1. Hermes Agent 和 OpenClaw 先作为轻量扩展处理，不纳入 `ae-cli discover` 当前合同。
2. 三种配置方式继续保留，但语义需要调整为：
   - 手动配置
   - 应用导入
   - `ae-cli` 自动配置
3. 命令式配置属于手动配置。
4. CC Switch deep link 是否支持某个 app，应以 `farion1231/cc-switch` 的 deep link/managed app 合同为判断依据。
5. 只有严格以 `Agent` 开头的接入组才需要 Hermes/OpenClaw 指引。
6. `Agent` 接入组不再展示原来的 Codex、Claude Code、Gemini 配置和导入方式。
7. `Agent` 接入组的手动配置还需要兼容自定义 Agent。

## Decisions

### 1. Group Classification

前端新增一个集中 helper，用于判断当前接入组是否进入 Agent 配置体验：

```ts
function isAgentAccessGroup(groupName: string | undefined | null) {
  return Boolean(groupName?.startsWith('Agent'))
}
```

该判断必须严格区分大小写，并要求字面前缀 `Agent`，不要求前缀后带横杠：

| group name | Agent experience |
| --- | --- |
| `Agentopenai` | yes |
| `Agentanthropic` | yes |
| `Agentgemini` | yes |
| `AgentAlpha` | yes |
| `Agent` | yes |
| `agentopenai` | no |
| `MyAgentopenai` | no |

第一版不新增后端字段。实现时应把判断集中在 helper 层，不在 Vue template 中散落字符串判断。

### 2. Configuration Method IA

普通接入组继续显示当前三种配置方式：

1. `手动配置`
2. `ae-cli 自动配置`
3. `CC Switch 配置`

`Agent` 接入组显示两种主要方式：

1. `手动配置`
2. `应用导入`

`Agent` 接入组应隐藏 `ae-cli 自动配置`卡片。原因：

1. `ae-cli discover` 当前不配置 Hermes/OpenClaw。
2. `ae-cli` 的 repo attribution 能力是研发上报链路，不是 Agent 客户端配置主路径。
3. 在 Agent 组中展示 `ae-cli 自动配置`容易让用户误以为它会写 Hermes/OpenClaw 配置。

如果未来需要在 Agent 组中解释 `ae-cli` 的 repo attribution，应放在高级说明或研发上报区，不放在配置方式卡片中。

### 3. Manual Configuration for Agent Groups

`Agent` 接入组的手动配置只展示三个目标：

1. Hermes Agent
2. OpenClaw
3. Custom Agent

不展示 Codex、Claude Code、Gemini 的原有片段。

#### Hermes Agent

Hermes Agent 手动配置应优先提供官方模型配置路径和必要文件片段：

- `hermes model` / `hermes config set` 方向的命令式配置
- `~/.hermes/config.yaml` 方向的文件片段，前提是片段能与官方配置合同保持一致

Hermes Agent 配置内容统一使用 OpenAI-compatible Chat Completions endpoint。原因是 Hermes custom provider 的稳定配置面以 `base_url` + `api_mode: chat_completions` 表达这类接入；因此 Agent 接入组的 `platform` 只表示后端 group/model 来源，不再决定 Hermes 客户端协议。

| platform | Hermes manual output |
| --- | --- |
| `openai` | OpenAI-compatible `/v1` endpoint/key/model |
| `anthropic` | OpenAI-compatible `/v1` endpoint/key/model |
| `gemini` | OpenAI-compatible `/v1` endpoint/key/model |

如果 provider `base_url` 已经以版本段结尾（如 `/v1` 或 `/api/coding/paas/v4`），前端保留该版本化 URL；否则追加 `/v1`。

Hermes 片段必须同时写入 active main model 和 custom provider 定义，避免用户只添加 provider 却继续使用旧 provider：

```yaml
model:
  provider: "custom:prod"
  default: "gpt-5.4"
  base_url: "https://prod.example.com/v1"
  api_mode: "chat_completions"

custom_providers:
  - name: "prod"
    base_url: "https://prod.example.com/v1"
    api_key: "sk-agent"
    api_mode: "chat_completions"
    models:
      "gpt-5.4":
        context_length: 200000
```

#### OpenClaw

OpenClaw 手动配置应提供官方 onboard/config 路径和必要文件片段：

- `openclaw onboard`
- `openclaw onboard --non-interactive ...`
- `openclaw config set ...` / `openclaw config patch ...`
- `~/.openclaw/openclaw.json` 方向的片段，前提是片段能与官方 configuration reference 保持一致

OpenClaw 配置内容同样统一使用 OpenAI-compatible Chat Completions：

| platform | OpenClaw manual output |
| --- | --- |
| `openai` | `api: "openai-completions"` + `/v1` endpoint/key/model |
| `anthropic` | `api: "openai-completions"` + `/v1` endpoint/key/model |
| `gemini` | `api: "openai-completions"` + `/v1` endpoint/key/model |

这里不生成 `anthropic-messages` 或 `google-generative-ai` native provider 片段；Agent 客户端按 `/v1/chat/completions` 工作，native platform 仍由后端连接测试和普通组配置路径覆盖。

OpenClaw 片段必须输出可合并到 `~/.openclaw/openclaw.json` 的 `models.providers.<provider>` 结构，而不是只输出 provider 内部字段：

```json
{
  "models": {
    "mode": "merge",
    "providers": {
      "prod": {
        "baseUrl": "https://prod.example.com/v1",
        "apiKey": "sk-agent",
        "api": "openai-completions",
        "models": [
          { "id": "gpt-5.4", "name": "gpt-5.4" }
        ]
      }
    }
  }
}
```

#### Custom Agent

Custom Agent 是 `Agent` 手动配置的兜底能力。它不假设具体客户端文件路径，只提供当前接入组的标准 provider 参数和可复制模板。

Custom Agent 展示内容：

- `platform`
- `base_url`
- `api_key`
- 当前选择/测试使用的 `model`，如果页面已有可用模型值

按 platform 生成通用模板：

| platform | template |
| --- | --- |
| `openai` | `OPENAI_API_KEY`, `OPENAI_BASE_URL=<provider.base_url>/v1`, OpenAI-compatible JSON 示例 |
| `anthropic` | `OPENAI_API_KEY`, `OPENAI_BASE_URL=<provider.base_url>/v1`, OpenAI-compatible JSON 示例 |
| `gemini` | `OPENAI_API_KEY`, `OPENAI_BASE_URL=<provider.base_url>/v1`, OpenAI-compatible JSON 示例 |

Custom Agent 不进入应用导入，因为没有稳定 app target。

### 4. App Import for Agent Groups

普通组当前的 CC Switch 导入继续保持：

- `openai` -> Codex
- `anthropic` -> Claude Code
- `gemini` -> Gemini

`Agent` 接入组的应用导入只展示：

- `Import to Hermes Agent`
- `Import to OpenClaw`

不展示：

- `Import to Codex`
- `Import to Claude`
- `Import to Gemini`
- `Import to Claude Desktop`

应用导入使用 CC Switch 的 `ccswitch://v1/import?...` provider import 合同。实现不依赖 CC Switch 在线 generator 的当前选项列表，而由 AE 前端按源码合同生成链接。

OpenClaw 可作为稳定目标处理，因为 CC Switch deep link parser 接受 `openclaw` app 目标。

Hermes 可作为稳定 app target 处理，但需要兼容性提示：

1. 生成 `app=hermes` 的 provider import link。
2. UI 必须提示用户需要较新版本 CC Switch。
3. 如果导入失败，提示升级 CC Switch 或回退到手动配置。
4. UI 说明 Agent 导入使用 OpenAI-compatible `/v1` endpoint，因为 Hermes/OpenClaw 默认 provider 协议就是 Chat Completions。

Claude Desktop 不作为本轮 deep link 目标：

1. CC Switch 已支持 Claude Desktop 管理。
2. 当前 provider deep link parser 只接受 `claude`、`codex`、`gemini`、`opencode`、`openclaw`、`hermes`。
3. 因此 `/user` 不生成 `claude-desktop` / `claudedesktop` deep link。

### 5. Platform-Specific Import Config

CC Switch import builder 应区分两层能力：

1. **URL import layer:** 当前 CC Switch provider deep link 稳定接收 `endpoint`、`apiKey`、`model`、`enabled` 等 URL 参数。
2. **App protocol layer:** 当前 CC Switch 对 OpenClaw/Hermes 的 provider import 会写入默认协议模式：
   - OpenClaw: `api = "openai-completions"`
   - Hermes: `api_mode = "chat_completions"`

因此 AE 第一版不把 `openai` / `anthropic` / `gemini` platform 语义映射为 native Agent 协议。它应当：

- 对所有 `Agent` platform 生成 Hermes/OpenClaw import link。
- 在 link 中携带 Agent 归一化后的 `/v1` endpoint、API key、model、provider name。
- 使用 CC Switch 默认的 OpenClaw `api = "openai-completions"` 和 Hermes `api_mode = "chat_completions"`。
- 不再提示 Anthropic/Gemini Agent group 手动调整 OpenClaw `api` 或 Hermes `api_mode`，因为 Agent 分支统一走 OpenAI-compatible Chat Completions。

目标结构：

```ts
type AgentImportApp = 'hermes' | 'openclaw'
type ProviderPlatform = 'openai' | 'anthropic' | 'gemini'

type AgentProviderImportInput = {
  app: AgentImportApp
  platform: ProviderPlatform
  name: string
  endpoint: string // normalized Agent endpoint, normally <provider.base_url>/v1
  apiKey: string
  model?: string
  enabled?: boolean
}
```

link 输出原则：

1. `app=openclaw` 使用 `ccswitch://v1/import?resource=provider&app=openclaw&name=...&endpoint=<provider.base_url>/v1&apiKey=...&model=...&enabled=true`。
2. `app=hermes` 使用 `ccswitch://v1/import?resource=provider&app=hermes&name=...&endpoint=<provider.base_url>/v1&apiKey=...&model=...&enabled=true`。
3. 不为 OpenClaw/Hermes 复用 Codex 的 `auth/config.toml` JSON payload。
4. 不为 OpenClaw/Hermes 复用 Claude 的 `env` JSON payload。
5. 不在 URL 明文 query 之外额外展开不被 CC Switch 当前 parser 使用的协议字段。

手动配置输出原则：

1. Agent group 的 Hermes/OpenClaw/Custom Agent 统一使用 OpenAI-compatible key/base URL 字段。
2. 普通 group 仍按 `openai` / `anthropic` / `gemini` platform 输出对应 Codex/Claude/Gemini 配置。
3. Agent group 不按 `anthropic` 或 `gemini` native client protocol 生成 Hermes/OpenClaw/Custom Agent 配置；这些 Agent 客户端统一使用 OpenAI-compatible Chat Completions。

### 6. API Key Safety

Agent 配置继续沿用当前 `/user` 敏感信息策略：

1. API key 默认隐藏。
2. 包含 key 的手动片段复制前必须走现有确认流程。
3. 不在普通只读说明里直接明文展示 key。
4. CC Switch import link 包含 key，因此只在当前页面已有 key 且用户已进入应用导入区域时生成；如果现有实现会在 DOM href 中包含 key，应保持与当前 CC Switch 导入一致的风险提示。

### 7. Unsupported Platforms

如果未来出现 `platform` 不在 `openai`、`anthropic`、`gemini` 内：

- 普通组保持当前 unsupported 行为。
- `Agent` 组手动配置显示 Custom Agent 的通用参数摘要，但不生成不确定命令。
- `Agent` 组应用导入不生成 Hermes/OpenClaw link，并提示当前 platform 暂不支持应用导入。

## Data Flow

本轮不新增 API。

现有数据流保持：

1. `/user` 通过 `GET /api/v1/user/providers` 获取 provider、group 和 credential 摘要。
2. 用户选择 provider/group。
3. 页面使用当前 group 的 `group_name` 判定是否启用 Agent 配置体验。
4. 页面使用当前 group 的 `platform`、provider `base_url`、当前 credential key 和模型选择生成手动配置片段与 CC Switch import links。
5. 连接测试仍使用现有 provider/group/model/prompt 流程，不因为 Agent 组而改变后端请求合同。

## Implementation Notes

### Helper Layer

`frontend/src/utils/userSetupReview.ts` 应继续作为配置片段和 import link 的主要生成面，但需要拆清楚普通工具和 Agent 工具：

- `isAgentAccessGroup(groupName)`
- `buildManualConfigSnippets(input)`：普通组仍生成 Codex/Claude/Gemini；Agent 组生成 Hermes/OpenClaw/Custom Agent
- `buildCCSwitchProviderImportLink(input)`：保留普通 app
- `buildAgentProviderImportLink(input)` 或扩展现有 builder：生成 Hermes/OpenClaw
- `resolveCCSwitchAppsForGroup(platform, groupName)`：普通组返回当前平台默认 app；Agent 组返回 `hermes/openclaw`

避免在 `UserView.vue` template 中散落：

```ts
selectedGroup.group_name.startsWith('Agent')
```

### UI Layer

`UserView.vue` 中的配置方式卡片应根据 group 类型分支：

普通组：

- 手动配置
- ae-cli 自动配置
- CC Switch 配置

Agent 组：

- 手动配置
- 应用导入

Agent 组文案避免“CC Switch 配置”这个过窄标题，使用更准确的 “应用导入”。面板内说明这是通过 CC Switch deep link 导入到 Hermes/OpenClaw。

### i18n

新增或调整双语文案：

- Agent group manual configuration title/help
- Hermes Agent title/help
- OpenClaw title/help
- Custom Agent title/help
- App import title/help
- Import to Hermes Agent
- Import to OpenClaw
- Hermes CC Switch version compatibility warning
- Unsupported Agent platform import warning

文案不应使用“完成”“已验证”等会暗示浏览器已经检查本机状态的词。

## Error Handling

1. 没有 API key：不生成包含 secret 的片段或 import link，提示先创建当前接入组 API key。
2. 没有 selected group：隐藏配置方式区域，保持当前页面行为。
3. `Agent` group + unsupported platform：只展示通用参数摘要，不生成不确定命令或 import link。
4. Hermes import 失败：页面无法检测本机失败，只能在文案中提示升级 CC Switch 或改用手动配置。
5. OpenClaw/Hermes 命令不可用：页面不探测本机，只提供安装/官方入口提示；真实失败由用户本机命令输出处理。

## Testing

### Unit Tests

`frontend/src/__tests__/user-setup-review.test.ts` 应覆盖：

1. `isAgentAccessGroup('Agentopenai') === true`
2. `isAgentAccessGroup('Agent') === true`
3. `isAgentAccessGroup('agentopenai') === false`
4. `isAgentAccessGroup('MyAgentopenai') === false`
5. 普通 `openai` 组只生成 Codex 手动配置，不生成 Hermes/OpenClaw/Custom Agent。
6. `Agent` + `openai` 组生成 Hermes/OpenClaw/Custom Agent，不生成 Codex。
7. `Agent` + `anthropic` 组生成 Hermes/OpenClaw/Custom Agent，不生成 Claude Code。
8. `Agent` + `gemini` 组生成 Hermes/OpenClaw/Custom Agent，不生成 Gemini。
9. Agent group 的 CC Switch import apps 只包含 `hermes/openclaw`。
10. 普通 group 的 CC Switch import apps 继续按 `codex/claude/gemini` 生成。
11. Claude Desktop 不出现在任何 generated import target 中。
12. Agent group 的 Hermes/OpenClaw import link 使用 `endpoint`、`apiKey`、`model` URL 参数，不复用 Codex/Claude/Gemini 的 app-specific config payload。

### View Tests

`frontend/src/__tests__/user-view.test.ts` 应覆盖：

1. 普通 group 显示三张配置方式卡：手动、ae-cli 自动、CC Switch。
2. `Agent` group 不显示 ae-cli 自动配置卡。
3. `Agent` group 的手动配置面板显示 Hermes Agent、OpenClaw、Custom Agent。
4. `Agent` group 的应用导入面板显示 Hermes/OpenClaw，不显示 Codex/Claude/Gemini。
5. Hermes import 区显示版本兼容提示。
6. `Agent` + `anthropic` 或 `Agent` + `gemini` 的应用导入面板显示 OpenAI-compatible `/v1` endpoint 说明。
7. API key 未创建时，Agent import link 不可用或不渲染。

### Manual Verification

手动验收至少覆盖：

1. 普通 OpenAI group：页面行为与当前线上一致。
2. `Agent` OpenAI group：只看到 Hermes/OpenClaw/Custom Agent。
3. `Agent` Anthropic group：只看到 Hermes/OpenClaw/Custom Agent，变量名使用 `OPENAI_API_KEY` / `OPENAI_BASE_URL=<provider.base_url>/v1`。
4. `Agent` Gemini group：只看到 Hermes/OpenClaw/Custom Agent，变量名使用 `OPENAI_API_KEY` / `OPENAI_BASE_URL=<provider.base_url>/v1`。
5. 点击 OpenClaw import link 能拉起支持 deep link 的 CC Switch。
6. `Agent` Anthropic/Gemini group 的应用导入文案提醒用户导入使用 OpenAI-compatible `/v1` endpoint。
7. 点击 Hermes import link 时，如 CC Switch 版本不支持，页面已有明确回退说明。

## Documentation Updates

实现本设计时必须同步更新：

1. `docs/architecture.md`：如果 `/user` 当前配置方式和 Agent group 分支被描述为项目级当前状态，应更新。
2. 本 spec：如果实现中发现 CC Switch Hermes/OpenClaw import config 字段与本文假设不同，应在本文或 follow-up spec 中修正当前合同。
3. `AGENTS.md`：仅当 agent 协作规范变化时才更新；本功能不默认需要修改。

## Acceptance Criteria

1. 普通 group 的 Codex/Claude/Gemini 配置体验不回退。
2. 只有 `Agent` 严格前缀 group 进入 Agent 配置体验。
3. `Agent` group 不展示 Codex/Claude/Gemini 配置或导入。
4. `Agent` group 手动配置包含 Hermes Agent、OpenClaw、Custom Agent。
5. `Agent` group 应用导入只包含 Hermes Agent 和 OpenClaw。
6. `Agent` group 不展示 `ae-cli` 自动配置卡。
7. Hermes import 有 CC Switch 版本兼容提示。
8. `Agent` Anthropic/Gemini group 的应用导入提示用户导入使用 OpenAI-compatible `/v1` endpoint。
9. Claude Desktop 不作为 generated deep link target。
10. API key 敏感复制确认规则保持不变。
11. 相关 helper 和 view tests 覆盖普通组与 Agent 组分支。
