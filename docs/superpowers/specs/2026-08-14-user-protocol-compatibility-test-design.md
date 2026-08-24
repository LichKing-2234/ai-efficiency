# User Protocol Compatibility Test Design

**Date:** 2026-08-14
**Status:** Current implementation contract
**Scope:** `/user` protocol compatibility testing, relay group capability metadata, and `POST /api/v1/user/providers/:id/test`
**Related Issues:** [#291](https://github.com/LichKing-2234/ai-efficiency/issues/291), [#292](https://github.com/LichKing-2234/ai-efficiency/issues/292), [#293](https://github.com/LichKing-2234/ai-efficiency/issues/293)

## Spec Relationship

- 本文定义 `/user` 连接测试当前生效的协议兼容性合同。
- 本文替代 `2026-03-24-oauth-cli-login-design.md` 和 `2026-06-14-user-api-key-first-onboarding-design.md` 中“连接测试固定使用 platform-native downstream probe”的描述。
- 本文不改变上述历史 spec 的其他 OAuth、provider delivery、API-key-first onboarding 或配置方式合同，也不回写其正文。
- 当前实现变更仅位于 `ai-efficiency`，通过既有 `backend/internal/relay.Provider` HTTP 边界调用 sub2api；不修改 sub2api 源码。

## Problem

旧 `/user` 连接测试由 group platform 固定选择一个 completion endpoint。对于支持协议转换的接入组，这只能证明一条预设路径，不能回答用户实际关心的“这个接入组、我的 API key、这个模型能否通过指定客户端协议完成请求”。固定 Chat-to-Responses 转换还可能把协议转换链故障误报为账号或接入组不可用。

sub2api 管理端的账号测试与这里也不是同一个证明：前者以账号为测试对象，后者必须经过当前用户、接入组、个人 group-scoped API key、模型、协议和 sub2api group routing/failover。

## Goals

1. 让用户从当前接入组声明的稳定协议中显式选择一条进行兼容性测试。
2. 使用真实个人 group-scoped API key、模型和 sub2api 路由，不使用 RelayProvider admin key。
3. 让成功结果严格对应 provider、group、当前 key、model 和 protocol，避免陈旧异步结果误导用户。
4. 对每种协议验证合法终止状态和非空 assistant text。
5. 将 relay/upstream 返回给 AI Efficiency 的完整错误正文展示给用户，便于诊断协议转换链。

## Non-Goals

- 不改变手动配置、`ae-cli discover`、CC Switch、Hermes、OpenClaw 或 Custom Agent 的生成配置。
- 不自动协商协议，不做 AI Efficiency retry，也不在失败后自动 fallback 到另一协议。
- 不模拟 streaming client；所有 probe 都是小型 non-streaming 请求。
- 不把结果持久化到 backend、local storage 或其他页面。
- 不把连接测试描述成 sub2api account health、生成配置验收或上游服务整体健康检查。
- 不把连接测试成功描述成本机 Claude Code 或其他客户端已经完成配置。

## Capability Contract

`GET /api/v1/user/providers` 的每个 group summary 暴露：

```json
{
  "supported_protocols": ["responses", "chat_completions"],
  "recommended_protocol": "responses"
}
```

后端从 relay group metadata 集中计算稳定能力，前端只渲染后端声明的选项：

| Group platform | Recommended | Compatibility options | Excluded |
|---|---|---|---|
| OpenAI | Responses | Chat Completions；仅 `allow_messages_dispatch=true` 时包含 Messages | 未开启 dispatch 时的 Messages |
| Anthropic / Claude | Messages | Responses、Chat Completions | Gemini native |
| Gemini | GenerateContent | Chat Completions | Responses、Messages |
| Antigravity | Messages | Antigravity GenerateContent | Responses、Chat Completions |
| Grok | Responses | Chat Completions、Messages | Gemini native |
| Composite | Chat Completions | Responses、Messages、GenerateContent | Antigravity dedicated route |

Composite group 会由 sub2api 按请求模型选择实际平台，因此暴露其通用入口支持的全部稳定协议。`antigravity_generate_content` 不属于通用入口：该协议会强制进入 Antigravity 专用路由，绕过 Composite 的按模型选择，因此不对 Composite group 声明。

`allow_messages_dispatch` 必须从 sub2api group response 经 relay types、shared metadata cache 和 user setup DTO 原样保留。该字段加入时 metadata envelope schema 从 1 升到 2；schema 1 payload 必须视为 miss 并刷新，不能把历史缺失字段解释为 `false`。

## Probe Contract

请求：

```json
{
  "platform": "openai",
  "group_id": "42",
  "model": "gpt-5.4",
  "protocol": "responses"
}
```

- `protocol` 缺失时使用当前 group 的 `recommended_protocol`，用于兼容旧客户端。
- backend 重新校验当前用户仍有该 platform/group，并从当前用户的 active keys 中选择对应 group key。
- prompt 固定为 `Reply with OK`，用户不能编辑。
- output token 上限保持较小；请求明确 non-streaming。
- 每次只调用所选协议一次，不 retry、不 fallback。
- Claude/Anthropic Messages probe 携带 sub2api 当前接受的 Claude CLI Client Identity Profile：`claude-cli/<semver>` User-Agent、`X-App`、`anthropic-beta`、`anthropic-version`、Claude Code system text block 和合法格式的 `metadata.user_id`。该 profile 只作用于 `ProtocolCompleter` 的 Claude/Anthropic Messages 连接测试，不改变其他 Relay 请求或生成配置。

路由和成功条件：

| Protocol | Endpoint | Terminal requirement | Text source |
|---|---|---|---|
| Responses | `POST /v1/responses` | `status == "completed"` | message `output_text` |
| Chat Completions | `POST /v1/chat/completions` | first choice has non-empty `finish_reason` | first choice message content |
| Messages | `POST /v1/messages` or Antigravity-prefixed Messages route | non-empty `stop_reason` | text content blocks |
| GenerateContent | `POST /v1beta/models/{model}:generateContent` | at least one candidate has non-empty `finishReason` | candidate text parts |
| Antigravity GenerateContent | `POST /antigravity/v1beta/models/{model}:generateContent` | at least one candidate has non-empty `finishReason` | candidate text parts |

所有协议还必须产生非空 assistant text 才能返回 success。HTTP 2xx 但缺少终止字段、终止状态不合法或文本为空都属于失败。

## Result And Error Semantics

结果身份由以下五项组成：

```text
provider + group + current personal key + model + protocol
```

- 任一项变化立即清除当前结果并使在途请求失效。
- 每次请求使用递增 generation；只有最新 generation 可以写入 result 或 loading state。
- 结果仅存在于当前 `UserView` 实例，刷新或离开页面即消失。
- 非 2xx 响应将 AI Efficiency 实际收到的 sub2api/upstream body 完整附加到错误信息，读取上限为 64 KiB；不增加 AI Efficiency redaction 层。该合同依赖 upstream error contract 不返回敏感信息。

## Architecture Boundaries

- `backend/internal/relay` 定义稳定协议常量、group capability matrix 和可选 `ProtocolCompleter` 扩展。
- sub2api adapter 负责 endpoint、headers、payload、terminal parsing 和 bounded raw error body。
- sub2api adapter 也负责把 Claude/Anthropic Messages 连接测试的 Claude Client Identity Profile 限定在该 probe 请求内；它不把身份信号注入其他平台 probe 或普通 Relay 调用。
- user provider handler 负责身份、group authorization、当前个人 key 选择、默认协议和 capability validation。
- `backend/internal/usersetup` 只把 group capability facts 暴露给 `/user`。
- `frontend/src/views/UserView.vue` 负责选择、标签、current-page state 和 generation guard；它不复制 platform capability matrix。

## Acceptance Criteria

1. 六类 group platform 只暴露 capability matrix 中的稳定协议，且 recommendation 唯一。
2. OpenAI Messages 由 `allow_messages_dispatch` 精确控制，旧 metadata cache 会刷新。
3. 省略 protocol 时按 group recommendation 测试；显式传入未声明协议返回 `400`。
4. 所有 17 条支持的 platform/protocol 路由使用所选个人 group key 和模型。
5. 固定短 prompt、non-streaming、bounded output、无 retry、无 fallback。
6. success 同时要求 HTTP success、合法 terminal response 和非空 assistant text。
7. provider、group、key、model、protocol 任一变化都会清除结果，旧异步请求不能覆盖新状态。
8. 错误包含 AI Efficiency 收到的完整 bounded upstream body。
9. Claude/Anthropic Messages probe 满足 sub2api 当前 Claude CLI 识别合同，且 identity profile 不影响其他平台 probe 或普通 Relay 请求。
10. 生成配置逻辑和 sub2api 源码不变。
