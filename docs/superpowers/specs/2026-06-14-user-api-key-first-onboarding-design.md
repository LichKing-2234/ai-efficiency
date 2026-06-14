# User API-Key-First Onboarding Design

**Date:** 2026-06-14
**Status:** Approved design for current implementation
**Scope:** `frontend/src/views/UserView.vue`, `frontend/src/utils/userSetupReview.ts`, `frontend/src/i18n.ts`, `frontend/src/__tests__/`, `docs/architecture.md`
**Related:**
- [2026-05-21-user-page-cli-self-serve-design.md](./2026-05-21-user-page-cli-self-serve-design.md)
- [2026-05-26-user-cli-setup-checklist-design.md](./2026-05-26-user-cli-setup-checklist-design.md)
- [2026-05-29-company-wide-user-home-ux-design.md](./2026-05-29-company-wide-user-home-ux-design.md)
- [2026-05-30-company-wide-usability-hardening-design.md](./2026-05-30-company-wide-usability-hardening-design.md)
- [2026-05-19-ae-cli-deterministic-tool-configuration-design.md](./2026-05-19-ae-cli-deterministic-tool-configuration-design.md)
- [2026-06-01-ae-cli-doctor-tool-validation-design.md](./2026-06-01-ae-cli-doctor-tool-validation-design.md)
- [docs/architecture.md](../../architecture.md)

## Spec Relationship

- 本文定义 `/user` 当前生效的 onboarding 信息架构与交互合同。
- 本文替换 `2026-05-26-user-cli-setup-checklist-design.md` 中关于 `/user` “研发 / 非研发”路径切换、`Setup progress` 叙事、以及以 CLI checklist 为页面主框架的设计。
- 本文不改变 `ae-cli login`、`ae-cli discover`、`ae-cli hooks enable --global`、`ae-cli init`、`ae-cli sync`、`ae-cli doctor` 的命令语义；这些合同仍以各自 CLI spec 为准。
- 本文不改变 `/api/v1/user/providers`、group-scoped credential 创建/再生、模型列表、连接测试、relay provider、provider/group 数据模型和权限模型。
- 本文继承 `2026-05-29-company-wide-user-home-ux-design.md` 中“普通用户默认先看个人状态与下一步”的方向，并把 `/user` 从“偏研发 checklist”进一步收口为“个人 AI 接入工作台”。
- 本文继承 `2026-05-30-company-wide-usability-hardening-design.md` 的响应式、可访问性和双语文案要求；如果本文与该 spec 在 `/user` 页面叙事上冲突，以本文为准。
- 本文不回写历史 spec 正文；历史 spec 保留其设计演进记录。

## Problem

当前 `/user` 页面已经具备 provider/group 选择、API key 自助、模型加载、真实连接测试、CLI 命令和手动配置片段，但用户体验仍然被旧的开发者心智主导：

1. 页面主标题和主区块仍然围绕“接入进度”“完成接入”“我是研发 / 我是非研发”，而不是围绕用户最直接的目标。
2. `接入组` 虽然已经是当前后端合同中的真实对象，但在页面中仍然更像技术上下文，而不是主流程入口。
3. API key 创建和连接测试存在，但它们没有构成首屏主链路；用户仍然容易先看到命令、手动片段或高级恢复内容。
4. `ae-cli discover`、hooks、repo attribution、`doctor` 等研发向能力与普通用户的“先拿到可用接入”目标混在同一个页面主叙事中。
5. 现有“非研发”路径虽然提供了手动配置片段，但它仍然是“开发者接入进度”的一个分支，而不是新的主心智。

结果是，页面同时服务太多任务，但没有一句简单的话回答用户：

- 我先做什么？
- 我什么时候能确认这套接入可用？
- 然后我要用哪种方式把它接到工具里？

## Goals

1. 把 `/user` 的首屏主叙事改成：

   ```text
   选择接入组 -> 创建我的 API Key -> 运行连接测试 -> 选择配置方式
   ```

2. 保留当前 provider/group/self-serve/test 的后端合同，不引入新的 onboarding backend API。
3. 去掉“研发 / 非研发”切换，让所有用户共用同一条主流程，只在“配置方式”阶段分流。
4. 保留 `接入组` 这个术语，但让它成为清晰解释后的用户可操作对象，而不是抽象技术背景。
5. 让 API key 创建成为首屏第一主动作，让连接测试成为创建后的默认下一步。
6. 让 `手动配置`、`自动配置`、`CC Switch 配置` 在 API key 可用后出现，并把连接测试保留为推荐的下一步动作，而不是显示门槛。
7. 保留研发能力，但把 CLI、repo attribution、恢复命令、诊断命令下沉为“自动配置”路径或高级区。
8. 为 `CC Switch` 增加明确的 app-specific 一键导入设计合同，而不是停留在说明文字。

## Non-Goals

1. 不修改 provider/group 数据模型，也不把 `接入组` 改名成 `服务来源`、`AI 服务` 等新术语。
2. 不新增浏览器到本机 CLI 的执行桥，不在浏览器里执行 `ae-cli`。
3. 不改变 `ae-cli discover` 当前 provider-scoped 合同，也不把它重写成 group-scoped 工具配置命令。
4. 不新增 universal provider deep link 合同给 `CC Switch`；第一版只使用官方 app-specific provider import 协议。
5. 不把 `/user` 改成多步向导页面，不引入单独 route 跳转式 wizard。
6. 不在本轮改变首页 `/`、`/events`、`/repos`、`/settings` 的主要信息架构；这些页面继续由既有 spec 管理。
7. 不改变现有模型加载和连接测试的 downstream probe 行为。

## Decisions

### 1. Primary Narrative

`/user` 不再把 `Setup progress` 作为页面主标题，也不再让“我是研发 / 我是非研发”决定页面结构。

页面主目标改为：

- `Create My API Key` / `创建我的 API Key`

这不是全局“完成接入”按钮，而是当前选中接入组下的第一主动作。用户第一眼应当理解：

1. 先选择一个接入组。
2. 为自己创建这个组的 API key。
3. 立刻跑一次真实连接测试。
4. API key 可用后即可决定如何配置工具；连接测试用于先验证可用性。

### 2. Information Architecture

`/user` 保留当前 provider -> group 的数据组织关系，但改写主区层级：

1. `接入组选择区`
2. `创建我的 API Key`
3. `连接测试`
4. `配置方式`
5. `高级操作`

推荐布局可以是当前两栏结构的演进：

- 次级栏继续显示 provider 列表和当前 provider 下的 groups。
- 主内容区不再显示 checklist stepper，而是显示当前选中 group 的主流程卡片。

页面中不再出现“研发路径”或“非研发路径”按钮。用户不需要先声明身份，只需要沿着当前 group 完成接入动作。

### 3. State Model

主流程围绕“当前选中的接入组”展开，状态机保持简单：

1. `no_group_selected`
2. `group_selected_without_key`
3. `key_ready_without_test`
4. `test_success`
5. `test_failed`

各状态的页面行为：

#### `no_group_selected`

- 主卡片提示先选择接入组。
- `创建我的 API Key` 按钮不可用。
- `连接测试` 与 `配置方式` 不展示。

#### `group_selected_without_key`

- 主按钮显示 `创建我的 API Key`。
- 展示当前 group 的用途说明、平台、服务入口等必要上下文。
- Key 显示区保留空态说明。
- `连接测试` 和 `配置方式` 不展示。

#### `key_ready_without_test`

- API key reveal/copy/regenerate 仍可用，但退居辅助位置。
- `配置方式` 立即可见。
- `运行连接测试` 仍然是推荐动作，但不再是显示配置方式的门槛。

#### `test_success`

- `连接测试` 显示最近一次成功状态。
- `手动配置`、`自动配置`、`CC Switch 配置` 继续可见。
- 页面不显示“已全部完成”；这里只表示“连接已验证，可以继续配置工具”。

#### `test_failed`

- 保持当前 group 和当前 key 可见。
- 显示失败结果与重试动作。
- `配置方式` 继续可见，因为用户仍然可能需要先走手动或自动配置，再回头重试。

切换 provider 或 group 时：

- 清空当前测试结果。
- 清空当前“已展开的配置方式”视图。
- 保留该 group 已存在的 key 状态。

重新生成 key 时：

- 当前测试结果必须失效。
- `配置方式` 不隐藏，但页面应明确提示“建议重新运行连接测试”。

### 4. Access Group As The First-Class Object

`接入组` 术语保留，但必须补充用户解释，而不是只显示 raw label。

推荐说明文案方向：

- `选择一个接入组，然后为自己创建 API Key，用来连接 AI 工具。`

接入组卡片的主信息优先级：

1. 组的用途说明 / 面向谁
2. 当前是否已有可用 key
3. 平台和服务入口等技术上下文

不再让平台名、provider name 或命令列表占据 group 卡片的第一视觉层级。

### 5. Configuration Methods

API key 可用后，主区展示三种配置方式：

1. `手动配置`
2. `自动配置`
3. `CC Switch 配置`

这三种方式不是无 key 首屏默认内容；它们在 key 可用后展开。

这三种方式在顶层信息架构上必须同级显示，不应通过颜色或独占按钮让 `CC Switch` 看起来高于另外两种方式。差异应通过说明文字和展开后的细节表达，而不是通过卡片层级表达。

#### Manual Configuration

- 复用当前 `frontend/src/utils/userSetupReview.ts` 的片段生成逻辑。
- 继续按现有 platform contract 生成工具配置片段：
  - `openai` -> Codex
  - `anthropic` -> Claude
  - `gemini` -> Gemini
- 默认隐藏 key 明文；复制包含 secret 的片段仍需显式确认。
- 顶层卡片文案应明确它面向：
  - 非研发
  - 需要把配置复制给独立 agent 或第三方客户端的场景
  - 其他方式失效时的 fallback

#### Automatic Configuration

- 自动配置入口继续使用当前 `ae-cli discover --provider <provider>` 路径。
- `ae-cli discover` 的 provider-scoped 合同保持不变。
- UI 必须说明：自动配置基于当前 provider，而不是仅基于当前选中的 group。若同一 provider 下存在其他匹配 platform credential，`discover` 可能一并配置对应已安装工具。
- `hooks enable --global`、`init`、`doctor`、`sync`、`hooks status --uploads`、安装命令、设备登录兜底等内容不再占主流程首屏，而是下沉为自动配置或高级区内容。
- 顶层卡片文案应明确它主要面向研发团队。
- `高级命令参考` 不再作为 `/user` 全局常驻区块，而应只出现在 `自动配置` 面板内部。

#### CC Switch Configuration

- `CC Switch 配置` 是 API key 可用后的第三种路径。
- 它不是泛化说明块，而是工具级导入动作。
- 第一版只承诺 app-specific provider import，不承诺 universal provider deep link import。
- 顶层卡片文案应明确它主要面向非研发用户。
- `CC Switch` 详情面板应提供官方下载入口，指向官方安装或 release 渠道，而不是只给 deeplink 按钮。

### 6. CC Switch Deep Link Contract

`CC Switch` 官方当前支持：

```text
ccswitch://v1/import?resource=provider&app={app}&name={name}&...
```

第一版 `/user` 只生成官方 provider import deep link。

#### Supported Targets

当前页面只为与 selected group platform 匹配的工具展示导入按钮：

| Group Platform | CC Switch App | Button Label |
| --- | --- | --- |
| `openai` | `codex` | `导入到 Codex` |
| `anthropic` | `claude` | `导入到 Claude` |
| `gemini` | `gemini` | `导入到 Gemini` |

不匹配的平台按钮不展示。

#### Required Parameters

第一版使用最小稳定参数集：

- `resource=provider`
- `app=<codex|claude|gemini>`
- `name=<provider display name or provider name + "/" + group name>`
- `endpoint=<selectedProvider.base_url>`
- `apiKey=<selected group key>`
- `enabled=true`
- `model=<selected providerTestModel>` when the page has a selected model for the current group
- `model=gpt-5.4` as the Codex fallback when no explicit model is available

示例：

```text
ccswitch://v1/import?resource=provider&app=codex&name=Relay%20Main%20%2F%20General%20Development&endpoint=https%3A%2F%2Frelay.example.com&apiKey=sk-xxx&enabled=true
```

#### Explicitly Deferred Parameters

第一版不要求使用以下参数：

- `config`
- `configFormat`
- `configUrl`
- `homepage`
- `notes`

原因：

- 当前 `/user` onboarding 的核心目标是稳定导入 provider endpoint 与 API key。
- 为了避免 `CC Switch` 使用各客户端自身的默认模板模型，`/user` 优先对当前平台传入页面已选中的模型；Codex 在没有显式模型时仍传 `gpt-5.4`，避免落回 `gpt-5-codex`。
- 一旦带入 `config` 或 app-specific extended config，就会把 spec 拉进多个客户端各自的高级配置合同。

#### Fallback Behavior

如果本机未安装 `CC Switch` 或 `ccswitch://` 协议未注册：

- 浏览器层打开 deep link 失败属于预期可恢复行为。
- 页面应提供一个简短 fallback 说明，例如：
  - 安装或打开 `CC Switch`
  - 确认协议已注册
  - 失败时回退到 `手动配置`

第一版不要求检测本机是否真的安装了 `CC Switch`。

### 7. Existing Module Migration Rules

#### `frontend/src/views/UserView.vue`

- 移除 `setupAudience` 状态和对应 segmented control。
- 现有 `setupSteps` checklist 不再作为主页面骨架。
- 页面主骨架改为“group-scoped primary flow”。
- 保留：
  - provider list
  - group selection
  - create/regenerate/reveal/copy key
  - get models for selected group/platform
  - real provider test
- 现有 `API Key 和连接测试` 区域应提升为主流程中的关键阶段，而不是页面下半区附属工具。

#### `frontend/src/utils/userSetupReview.ts`

- 保留当前手动配置片段和自动配置命令构建能力。
- 新增 `CC Switch` deep link builder helper。
- 不删除现有 install/login/discover/hooks/repo/doctor/sync helper。

#### `frontend/src/i18n.ts`

需要移除或降级以下叙事：

- `user.setupProgressTitle`
- `user.setupProgressHelp`
- `user.setupAudienceLabel`
- `user.setupAudienceDeveloper`
- `user.setupAudienceNonDeveloper`
- 所有“完成接入”“我是研发 / 我是非研发”导向文案

需要新增或重写为新主叙事的文案：

- `创建我的 API Key`
- `下一步：运行连接测试`
- `连接成功后选择配置方式`
- `手动配置`
- `自动配置`
- `CC Switch 配置`
- `导入到 Codex / Claude / Gemini`
- `切换接入组后需要重新测试`

#### Frontend Tests

`frontend/src/__tests__/user-view.test.ts` 需要从“开发者/非开发者切换 + checklist”改为：

1. 没有 group 时不展示配置方式。
2. 选中 group 且无 key 时，主动作是 `创建我的 API Key`。
3. 创建 key 后，`手动配置 / 自动配置 / CC Switch 配置` 即可见。
4. `运行连接测试` 仍然保留为推荐动作，但不是配置方式显示门槛。
5. 切换 group 或 regenerate key 会重置测试成功态，但不会强制隐藏配置方式。
6. `高级命令参考` 只在 `自动配置` 面板内出现。
7. `CC Switch` 按钮只为匹配 platform 的 app 展示，并生成对应 deep link。

`frontend/src/__tests__/user-setup-review.test.ts` 继续验证手动片段和自动配置 helper；新增 `CC Switch` deep link builder 的单元测试。

### 8. Data Flow And API Requirements

本轮不新增 backend API。

继续使用现有合同：

- `GET /api/v1/user/providers`
- `POST /api/v1/user/providers/:id/groups/:group_id/credential`
- `POST /api/v1/user/providers/:id/groups/:group_id/credential/regenerate`
- `GET /api/v1/user/providers/:id/groups/:group_id/models?platform=...`
- `POST /api/v1/user/providers/:id/test`

连接测试仍然使用当前 selected provider id、group id、platform 和显式模型，保持现有 platform-native downstream probe 行为。

### 9. Copy Contract

`/user` 页面在本轮的 copy 要遵守以下规则：

- 不再以“完成接入”“接入进度”作为标题。
- 不再要求用户先声明自己是不是研发。
- 保留 `接入组` 这个正式名词。
- 首屏动作必须使用动词：
  - `创建我的 API Key`
  - `运行连接测试`
  - `手动配置`
  - `自动配置`
  - `CC Switch 配置`

推荐中文主文案方向：

- 页面副标题：`先选择接入组，创建你的 API Key，确认连接可用后再配置工具。`
- 接入组说明：`接入组决定你能创建哪类 API Key，以及可连接哪些 AI 工具。`

### 10. Acceptance Criteria

1. `/user` 页面不再显示 `我是研发 / 我是非研发` 切换。
2. `/user` 页面不再以 `接入进度` 或“完成接入”为主标题和主叙事。
3. 当前选中接入组无 key 时，主动作是 `创建我的 API Key`。
4. key 创建成功后，主动作默认切换为 `运行连接测试`。
5. `手动配置 / 自动配置 / CC Switch 配置` 在测试成功前不可见。
6. 切换 group 或 regenerate key 会使测试成功态失效。
7. `CC Switch` 使用 `ccswitch://v1/import?resource=provider&app=...` app-specific provider import 协议。
8. `CC Switch` 第一版不承诺 universal provider import deep link。
9. 手动配置与自动配置继续遵守当前 `ae-cli` 工具配置合同。
10. `/user` 现有 provider/group/self-serve/test 的 backend API 合同保持不变。

## External References

- CC Switch deep link protocol:
  - `https://github.com/farion1231/cc-switch/blob/main/docs/user-manual/en/5-faq/5.3-deeplink.md`
- CC Switch online deep link generator:
  - `https://farion1231.github.io/cc-switch/deplink.html`
