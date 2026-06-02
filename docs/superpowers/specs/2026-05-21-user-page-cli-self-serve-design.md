# User Page CLI Self-Serve Design

**Date:** 2026-05-21  
**Status:** Proposed current design  
**Scope:** `backend/internal/handler/`, `backend/internal/relay/`, `frontend/src/router/`, `frontend/src/views/`, `frontend/src/components/`, `frontend/src/api/`, `frontend/src/types/`, `docs/`  
**Related:**  
- [2026-04-13-ae-cli-user-install-design.md](./2026-04-13-ae-cli-user-install-design.md)  
- [2026-04-15-oauth-device-login-design.md](./2026-04-15-oauth-device-login-design.md)  
- [2026-05-19-ae-cli-deterministic-tool-configuration-design.md](./2026-05-19-ae-cli-deterministic-tool-configuration-design.md)  
- [2026-03-24-oauth-cli-login-design.md](./2026-03-24-oauth-cli-login-design.md)  
- [`docs/architecture.md`](../../architecture.md)

## Spec Relationship

- 本文定义一个新的登录后普通用户页面 `/user`，用于承载账户信息、CLI 安装/登录/配置引导，以及用户自己的 provider credential 自助能力。
- 它**不改变** `ae-cli install.sh` 的分发合同；CLI 安装入口仍以 [`2026-04-13-ae-cli-user-install-design.md`](./2026-04-13-ae-cli-user-install-design.md) 为准。
- 它**不改变** `ae-cli login` 的 PKCE / device flow 合同；CLI 登录协议仍以 [`2026-04-15-oauth-device-login-design.md`](./2026-04-15-oauth-device-login-design.md) 为准。
- 它本轮**不直接改写** `ae-cli discover` 的命令形状；工具配置写入规则仍以 [`2026-05-19-ae-cli-deterministic-tool-configuration-design.md`](./2026-05-19-ae-cli-deterministic-tool-configuration-design.md) 为准。
- 它修正的是 `/user` 页面如何表达和调用用户态 credential provisioning：从错误的 `provider + platform` 折叠视图，改成 `provider + group` 视图，并以当前 relay user 的 user-scoped group facts 作为 group 来源；adapter 可以用 provider-wide group list 解析 `allowed_groups` ID，但不能把 provider 下全部 active groups 暴露给用户。
- 本文当前还约束 `/user` 创建 / 重建 key 的写入身份：sub2api `/api/v1/keys` 要求当前 relay user 的用户态 JWT，RelayProvider admin API key 不能代替用户凭据创建 key。后端必须在创建 / 重建 key 前确保本地用户已绑定 relay user 且有可用的用户态写入凭据：Relay SSO 登录会保存用户输入的 relay password；如果 SSO 登录邮箱在 relay 侧不存在，后端会用该 SSO 密码创建 relay user 并保存；LDAP 登录和 `/user` 写入路径不能使用 LDAP bind password，而是使用后端生成的高熵 relay-side password。若 relay user 不存在，后端通过 relay admin API 创建用户、保存生成密码，并显式分配 relay 默认订阅；若 sub2api 已在 admin user create 期间完成默认订阅分配，后端重复 assign 遇到 409 `SUBSCRIPTION_ASSIGN_CONFLICT` 时视为幂等成功。若 relay user 已存在但本地没有可解密密码，或本地密码已经失效，后端可轮换该 relay user 的生成密码、加密保存，然后用用户态 JWT 创建 / 重建 key。对旧版本已经创建出的 LDAP-provisioned relay user，如果其 `notes=provisioned_by_ai_efficiency_ldap` 且还没有 group facts，后续 LDAP 登录可补默认订阅。LDAP bind password 仍然只能用于 LDAP bind，不能写入本地或转发到 relay。

## Overview

当前前端里：

1. 有登录页
2. 有 repo / events / settings 等页面
3. 没有面向普通开发者的账户页
4. 没有一处把 `ae-cli` 的安装、登录、discover、自助验证和用户自己的 provider credential 信息收拢到一起

结果是：

1. 普通开发者知道系统“有 CLI”，但缺少一个登录后常驻入口来完成自助配置
2. 旧 `GET /api/v1/providers` 更像早期 `ae-cli discover` 的程序消费接口，不是为 Web 用户界面设计的账户能力
3. 用户无法在前端清楚区分：
   - 自己是谁
   - 当前有哪些 provider
   - 当前 provider 下自己到底有哪些已订阅 / 已授权 group
   - 每个 group 属于哪个 platform
   - 当前 group 是否已有可复用 credential
   - 为什么 key 默认只做部分 mask 展示，但仍可复制完整值

因此新增一个普通登录用户可访问的新页面：

```text
/user
```

该页面采用“账户页外壳 + task-first 中心流程”的结构：

1. 页面顶部展示只读 profile
2. 页面主体以 CLI 自助配置为中心
3. provider 选择与 group 选择和 CLI 命令块联动
4. 页面允许用户贴回本地 CLI 输出进行轻量验证，但**不直接探测或执行**本机命令

## Goals

1. 给普通开发者一个登录后常驻的 `User` 页面，而不是把 CLI 自助入口藏在 admin settings 或 README 里。
2. 让用户能按页面引导完成：
   - install
   - login
   - discover
   - verify
3. 让用户能看见自己当前有哪些 provider，并在进入 credential 自助时显式选择 group。
4. 让 `/user` 只展示当前用户有订阅 / 有权限的 group，而不是把 provider 下的全部 active groups 暴露给用户。
5. 给用户提供自己的 group-scoped provider credential 状态说明，以及安全受限的 reveal / copy / regenerate 交互。
6. 保持后端与前端语义清晰分层：
   - CLI 程序消费接口继续服务 `ae-cli`
   - Web 账户页有自己的用户态接口
   - provider 数据归 DB 所有，config 仅用于 startup bootstrap

## Non-Goals

1. 第一版不做 profile 编辑、密码修改、认证设置修改。
2. 第一版不做浏览器直接执行本地 CLI 命令，也不做本机探测。
3. 第一版不在列表视图直接铺满展示 API key 明文；existing key 默认以部分 mask 展示，完整值通过 Copy 或 Reveal 获取。
4. 第一版不做完整的 group 管理台；只围绕“当前用户在当前 provider 下有权使用的 groups”做自助 credential 页面。
5. 第一版不在本 spec 内同时重写 `ae-cli discover` 为 group-first；如果 provider 下存在多个 allowed groups，discover 的 group selector 作为后续合同收口项单独处理。
6. 第一版不引入新的 LLM-based tool discovery。

## Approaches Considered

### Option A: Task-First User Hub with Provider-First, Group-Second Credential Provisioning

- `/user` 是标准登录后页面
- 顶部是轻量 profile
- 中心是 CLI setup checklist
- provider 仍是一层选择
- 选中 provider 后展示当前 relay user 在该 provider 下的 allowed groups
- create / regenerate / reveal / copy 都作用在 `provider + group`

优点：

1. 最符合普通开发者的首次接入和回访排障路径
2. 避免把 `platform` 错当成可唯一选择的一级对象
3. 避免把“已有 key”误当成“有订阅的 group”
4. install / login / discover / verify 顺序清楚

缺点：

1. 需要为 relay adapter 增加“当前用户 allowed groups”能力
2. 需要后续单独考虑 discover 的 group selector 收口

### Option B: Task-First User Hub with Platform-First Approximation

- 页面仍采用 task-first 结构
- 但继续把 provider 下的 active groups 折叠成 `platforms[]`

优点：

1. 复用当前错误实现改动最少

缺点：

1. `platform` 和 `group` 并非 1:1，会直接丢失信息
2. 不能保证只显示当前用户真正有订阅的 group
3. 会继续扩散错误合同

### Option C: Read-Only Group Visibility

- `/user` 只展示 provider + allowed groups
- 不直接执行 create / regenerate

优点：

1. 页面合同最小

缺点：

1. 用户必须退回 CLI/runtime 才能补齐 credential
2. 自助能力不完整

### Recommendation

采用 **Option A: Task-First User Hub with Provider-First, Group-Second Credential Provisioning**。

原因：

1. 目标用户是普通开发者，而不是管理员。
2. 目标动作是“把 CLI 配通”，不是“浏览账户资料”。
3. `group` 才是订阅 / 授权 / 路由的真实对象；`platform` 只是 group 属性。

## Information Architecture

### Route and Entry

- 新增路由：`/user`
- 所有已登录用户可访问
- 不要求 admin
- 入口位于侧边栏底部当前用户信息区域；该区域从纯展示改成可点击，进入 `/user`

### Page Structure

页面从上到下分成 4 个区块：

1. `Profile Summary`
2. `Provider & Group Credential`
3. `CLI Setup Checklist`
4. `Help / FAQ`

在宽屏下可使用两列布局：

- 左列：profile + provider switcher
- 右列：group credential + CLI checklist

在窄屏下按单列堆叠。

### Profile Summary

该区块只读显示：

1. `username`
2. `email`
3. `role`
4. `auth_source`

第一版不提供编辑动作。其目标只是回答：

1. 我当前是哪个账号
2. 我现在将以哪个身份做 CLI login / provider 配置

### Provider and Group Credential

该区块展示当前用户**可见的全部 enabled providers**，不只 primary。

每个 provider 至少显示：

1. `display_name`
2. `name`
3. `base_url`
4. `is_primary`

响应中可以继续包含 `default_model` 以兼容 CLI / discover 相关消费方，但 `/user` 的 `Provider & Group Credential` 区块不把它展示成 credential 元数据。provider test 的 `model` 必须由用户在测试表单里显式选择或输入；页面应优先按当前 `provider + group + platform` 加载可用模型候选，加载失败或无候选时保留手动输入兜底。

页面始终只允许单选一个 provider。默认选中规则：

1. 优先 `is_primary=true`
2. 否则第一项

选中 provider 后，页面再展示该 provider 下当前 relay user 的 `allowed groups`。每个 group 至少显示：

1. `group_id`
2. `group_name`
3. `platform`
4. `credential.state`
5. `credential.name`
6. `credential.api_key_id`
7. `credential.status`

group 是页面上的一级选择对象。`platform` 只是每个 group 的属性，用来帮助用户理解该 group 属于 OpenAI / Anthropic / Gemini 等哪条路由面。

### CLI Setup Checklist

Checklist 固定为 4 步：

1. `Install`
2. `Login`
3. `Discover`
4. `Verify`

该区块是页面中心，不拆成独立 tab。

### Help / FAQ

底部补充常见问题，例如：

1. `~/.local/bin` 不在 `PATH`
2. `login` 需要使用 `--device`
3. `discover` 没有检测到工具
4. 为什么 key 默认只显示部分 mask

## Provider Credential Semantics

### Definition

本页中的 credential 指当前用户在某个 `provider + group` 视角下，由统一 provisioning 合同解析出的“当前可复用或可创建”的 API credential。

它**不是**：

1. 固定名 `ae-cli-auto` key 的别名
2. provider 级单把 key 的概念
3. platform 级折叠概念
4. 页面专属的独立生命周期

### Group Source of Truth

`/user` 页面中的 groups 必须以当前 relay user 的 user-scoped group facts 作为来源。对 sub2api adapter 来说，这包括：

1. relay user detail 或 admin user list 中的 `allowed_groups`
   - 可能是 group object 数组
   - 也可能是 group ID 数组；此时 adapter 必须通过 group list 解析 ID 对应的 `name` / `platform` / subscription metadata
2. relay user detail 或 admin user list 中的 active `subscriptions`
   - subscription group 只有在当前用户存在 active subscription fact 时才可展示
   - sub2api detail endpoint 可能省略 subscriptions；adapter 可以 fallback 到 admin users list 中同一个 `user_id` 的 user-scoped facts

明确禁止以下替代来源：

1. provider 下所有 active groups 的全量枚举
2. 通过“已有 key”倒推出 group
3. 通过 platform 折叠 group 后的近似视图

如果后端当前 adapter 里没有 user-scoped group fact 能力，则应先扩展 relay adapter，再暴露 `/user` 页面，而不是继续沿用错误的 `platforms[]` 合同。

### Canonical Provisioning Contract

`/user` 必须复用统一的 provider credential provisioning 合同：

1. 先列出当前 relay user 在该 provider 下的 active keys
2. 只在目标 `group_id` 下筛选
3. 优先匹配“用户同名 key”
4. 如果 `username` 本身就是邮箱别名，则按既有逻辑退化成邮箱前缀，例如 `alice`
5. 没有可复用 key 时，按同样的命名规则创建
6. 创建时必须带目标 `GroupID`

### Group Resolution

当前页面不再把 group 解析包装成 platform-aware 默认解析问题。对于 `/user`：

1. group 由 relay user-scoped facts 给出；`allowed_groups` 为 ID 时由 adapter 解析成完整 group
2. 用户点击 create / regenerate 时，目标 `group_id` 就是用户选中的 group
3. 不允许再用 “当前 platform 对应默认 group” 替代显式 group 选择

### Server States

后端对 group credential 只暴露两种持久状态：

1. `missing`
   - 当前 `provider + group` 下没有可复用 credential
2. `existing_hidden`
   - 当前 `provider + group` 下已有可复用 credential
   - 当 relay `ListUserAPIKeys` 返回 key 明文时，后端在该状态下返回 `credential.key`
   - 前端默认只显示部分 mask 值，但复制动作使用完整 key

### Client Overlay State

前端在当前页面内存里额外维护一个**非持久**状态：

1. `session_visible`
   - 只在当前页面会话内成立
   - 触发条件是用户刚完成：
     - `Create Key`
     - `Regenerate`
   - 此时前端拿到了新 secret 的一次性返回值

`session_visible` 不是后端事实，也不应通过 `GET` 接口重新读取出来。

刷新页面、重新进入页面或丢失本地内存后，状态仍以 `GET /api/v1/user/providers` 返回的 `existing_hidden` 为准；如果该响应带有 `credential.key`，页面仍可 mask 展示并复制完整 key。

### Relay Write Credential Semantics

`Create Key` / `Regenerate` 必须用当前 relay user 的身份写入 sub2api 用户态 key 接口，而不是把 RelayProvider admin key 伪装成用户态写入。只要本地存在可解密的 `relay_auth_password`，后端就可以用它为当前 relay user 获取用户态 JWT；这个规则同样适用于 relay 侧角色为 `admin` 的用户。

`/user` create/regenerate 阶段必须主动补齐写入凭据，而不是把缺失凭据暴露成用户操作阻断。正确来源包括：

1. Relay SSO 登录成功时，SSO provider 把用户输入的 relay password 作为 `RelayAuthPassword` 传给 auth service，后端用 `encryption.key` 加密保存。
2. Relay SSO 登录遇到 `invalid credentials` 时，如果登录名是邮箱且 relay 侧按 email / canonical username 查不到既有用户，后端可用用户输入的 SSO password 创建 relay user；如果 relay 用户已存在，则仍按密码错误处理，不覆盖既有密码。
3. LDAP 新用户在 relay 侧没有账号时，LDAP 登录期 relay identity provisioning 创建 relay user，并保存后端生成的高熵 relay-side password。创建后后端读取 relay `default_subscriptions` 设置并逐条调用 relay subscription assign API，使新用户具备可创建 API key 的 group entitlement；如果当前 sub2api 已经在 admin user create 内部分配默认订阅，重复 assign 返回 409 `SUBSCRIPTION_ASSIGN_CONFLICT` 时按已有订阅处理，不让首次 LDAP 登录失败。旧版本已创建但缺少 group facts 的 `provisioned_by_ai_efficiency_ldap` relay user，在后续 LDAP 登录解析身份时也可补同一组默认订阅。若本地 LDAP 用户的历史 `relay_user_id` 被后续登录修复到另一个带该 provisioning note 的 relay user，或本地用户记录缺失但登录解析到既有系统 provisioned relay user，后端会为目标 relay user 轮换新的生成密码并保存，以免本地继续持有旧 relay 身份的密码或没有可写密码。
4. `/user` create/regenerate 发现本地用户没有 `relay_user_id`，或本地保存的 `relay_user_id` 在当前 relay/sub2api 已不存在时，通过当前 provider 按 email / canonical username 重新解析 relay user；解析不到则创建 relay user、保存生成密码，再继续 key 写入。
5. `/user` create/regenerate 发现本地没有可解密 `relay_auth_password`，或用旧密码创建 key 失败时，后端通过 relay admin API 轮换生成密码、保存后重试一次用户态 key 写入。

LDAP 登录复用既有本地 relay SSO 用户记录时，后端会把本地 `auth_source` 更新为 `ldap`，但优先保留之前 SSO 保存的 `relay_auth_password`；如果后续写入发现该密码失效，再按上面的轮换规则修复。LDAP bind password 只用于 LDAP bind，不能使用、保存或转发给 relay。

### Create, Reveal, Copy, Regenerate Rules

- `Create Key`
  - 仅在当前 `provider + group` 处于 `missing` 状态时可用
  - 执行一次 ensure/create，并返回新 secret
  - 如果当前账号缺少 relay write credential，后端先解析/创建 relay user、轮换并保存生成密码，再用用户态 JWT 创建 key
  - 成功后前端进入 `session_visible`
- `Reveal`
  - 当当前页面内存存在新 secret，或 `GET` 返回了 `credential.key` 时可用
  - 用于把当前 key 明文显示出来
- `Copy`
  - 当当前页面内存存在新 secret，或 `GET` 返回了 `credential.key` 时可用
  - 将完整 key 复制到剪贴板
- `Regenerate`
  - 在 `existing_hidden` 或 `session_visible` 状态可用
  - 先确保 relay write credential 可用，再将当前 `provider + group` 下按统一合同识别出的旧 credential 标记为不可用，最后按同一合同新建
  - 如果已有本地 relay password 失效，后端轮换并保存生成密码后重试用户态写入
  - 成功后前端进入 `session_visible`

页面必须明确解释：

1. 页面默认部分 mask 展示 API key，避免列表视图直接铺满明文
2. 只要当前 relay 响应提供 key 值，用户随时可以复制完整 key
3. 如果某个 relay 响应不提供 key 值，页面应说明该 key 当前不可复制，需要 regenerate 得到新 key

## API Contract

### Existing Endpoint Reused

继续复用：

```text
GET /api/v1/auth/me
```

用于 `Profile Summary`。

### Why Not Build `/user` On Legacy `GET /api/v1/providers`

旧接口：

```text
GET /api/v1/providers
```

曾是 `ae-cli discover` 的程序消费接口，语义包含：

1. 以 CLI 配置为目标的数据形状
2. 当前实现里仍带有 provider 级自动创建 API key 的副作用
3. 不表达 Web 页需要的 `provider + group` 展示语义

因此 `/user` 页面不应直接复用它。

### New User-Surface Endpoints

建议新增用户态接口命名空间：

```text
/api/v1/user/...
```

第一版需要：

#### `GET /api/v1/user/providers`

用途：

1. 返回当前用户可见的 enabled providers
2. 返回每个 provider 下当前 relay user 的 `allowed groups`
3. 返回每个 `provider + group` 的 credential 持久状态
4. **不自动创建** credential

建议响应字段：

```json
{
  "data": {
    "providers": [
      {
        "id": 1,
        "name": "sub2api",
        "display_name": "sub2api",
        "base_url": "https://sub2api.example/v1",
        "default_model": "gpt-5.4",
        "is_primary": true,
        "groups": [
          {
            "group_id": "42",
            "group_name": "OpenAI / Group 42",
            "platform": "openai",
            "credential": {
              "state": "existing_hidden",
              "api_key_id": 12345,
              "key": "sk-...",
              "name": "alice",
              "status": "active",
              "created_at": "2026-05-21T01:02:03Z",
              "last_used_at": "2026-05-21T09:00:00Z"
            }
          }
        ]
      }
    ],
    "message": ""
  }
}
```

约束：

1. `credential.state` 只返回 `missing | existing_hidden`
2. 对当前用户自己的现有 API key，如果 relay list 响应包含 `key`，则返回 `credential.key`；前端负责 mask 展示、完整复制
3. 不返回 `session_visible`

#### `POST /api/v1/user/providers/:id/groups/:group_id/credential`

用途：

1. 当当前 `provider + group` 处于 `missing` 时创建 credential

行为：

1. 如果当前 `provider + group` 已存在可复用 credential，则返回冲突错误
2. 成功时返回一次性 secret

建议响应字段：

```json
{
  "data": {
    "api_key_id": 12346,
    "name": "alice",
    "status": "active",
    "secret": "sk-..."
  }
}
```

#### `POST /api/v1/user/providers/:id/groups/:group_id/credential/regenerate`

用途：

1. 让当前 `provider + group` 下旧的可复用 credential 失效
2. 创建一把新的 credential
3. 返回新 secret 的一次性明文

#### `GET /api/v1/user/providers/:id/groups/:group_id/models?platform=<platform>`

用途：

1. 为 `/user` 连接测试表单加载当前 `provider + group + platform` 下可用的模型候选
2. 使用当前用户在该 group 下自己的 active API key 调用 relay/sub2api，而不是使用 RelayProvider admin key
3. 将不同平台模型列表响应归一成简单选择项

建议响应字段：

```json
{
  "data": {
    "models": [
      {
        "id": "gpt-5.4",
        "display_name": "GPT-5.4"
      }
    ],
    "message": ""
  }
}
```

行为：

1. 路由只要求登录态，不要求 admin role
2. 后端按 `provider + group_id + platform` 选择当前用户自己的 active key
3. OpenAI / Anthropic 兼容列表优先读取 sub2api `GET /v1/models` 的 `data[].id`
4. Gemini 原生列表读取 `GET /v1beta/models` 的 `models[].name`，并去掉 `models/` 前缀后返回给前端选择
5. 未找到当前用户在该 group + platform 下可用 key 时返回空 `models` 和可读 `message`，前端保留手动输入兜底

#### `POST /api/v1/user/providers/:id/test`

用途：

1. 让普通用户从 `/user` 页面测试自己在当前 provider 下的 active API key
2. 使用页面当前选中 group 的 `platform` 和用户显式选择或输入的具体 `model`
3. 发送一次真实 chat completion 请求，返回连接结果和可选响应内容
4. admin Settings 的 Relay Providers 表只保留管理 CRUD，不再提供 Test 入口

请求字段：

```json
{
  "platform": "openai",
  "group_id": "42",
  "model": "gpt-5.4",
  "prompt": "Hi"
}
```

行为：

1. 路由只要求登录态，不要求 admin role；不存在对应的 `/api/v1/admin/providers/:id/test` 管理端测试合同
2. 后端仍通过 `relay.Provider` 列出当前 relay user 的 API keys
3. 后端按 `provider + group_id + platform` 选择当前用户自己的 active key，而不是使用 RelayProvider admin key 或同 platform 其他 group 的 key
4. `model` 必须由调用方提供；页面不把 provider `default_model` 当作测试合同。页面可以从 user-scoped models endpoint 预填候选，但提交测试时仍必须带明确模型值
5. 未找到当前用户在该 group + platform 下可用 key 时返回 `success: false`

### Backend Boundary

这些接口应复用现有 `relay.Provider` 抽象：

1. `ListUserAPIKeys`
2. `CreateUserAPIKey`
3. `UpdateUserAPIKeyStatus`
4. `ListModelsForPlatform` optional extension

同时应扩展 relay adapter，增加“读取当前用户 `allowed_groups`”的能力。  
不得绕过 provider 抽象重新引入 direct sub2api DB coupling。

## CLI Checklist UX

### Step 1: Install

显示官方安装命令：

```bash
curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | AE_CLI_INSTALL_SERVER_URL=<current-origin> bash
```

同时显示 Windows PowerShell 安装命令：

```powershell
$env:AE_CLI_INSTALL_SERVER_URL = "<current-origin>"; iwr -UseB https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.ps1 | iex
```

### Step 2: Login

显示：

```bash
ae-cli login
```

并补充 headless 说明：

```bash
ae-cli login --device
```

### Step 3: Discover

本 spec 本轮不改 discover 命令形状，仍沿用：

```bash
ae-cli discover --provider <provider-name>
```

但页面必须明确说明：

1. 当前 `/user` 已经是 group-first 合同
2. 如果某个 provider 下当前用户有多个 allowed groups，discover 未来需要 group selector 才能完全对齐
3. 当前页面不会用错误的默认 group 猜测来掩盖这个合同缺口

### Step 4: Verify

本页采用“用户贴回结果，页面做轻量判断”的方式。

验证输入至少包括：

1. `ae-cli version`
2. `ae-cli discover --dry-run --provider <provider-name>`
3. `ae-cli doctor`

## Runtime Boundary and Data Ownership

### Provider Source of Truth

provider 的 source of truth 是 DB，而不是 runtime fallback config 视图。

具体约束：

1. config 仅用于 `startup bootstrap`
2. 第一次启动时，服务可根据 config 将 primary provider 落到 DB
3. `/user`、`settings`、常规 provider 查询都只读写 DB
4. 不再把 runtime fallback provider 当成产品合同暴露给用户界面

### Group Source of Truth

group 的 source of truth 是当前 relay user 的 user-scoped group facts：`allowed_groups` 及 active subscription entries。adapter 可以读取 provider group list 来解析 `allowed_groups` ID 的详情，但不能把 provider 下 active groups 全量枚举成用户可见 group，也不能从已有 key 反推 group。

## Testing

至少覆盖：

1. `GET /api/v1/user/providers`
   - 只返回当前用户 allowed groups
   - 同 platform 多 group 不折叠
   - group 的 `platform` 只是属性
2. `POST /api/v1/user/providers/:id/groups/:group_id/credential`
   - missing -> create
3. `POST /api/v1/user/providers/:id/groups/:group_id/credential/regenerate`
   - old credential inactive + recreate success
4. 前端 `/user`
   - provider 切换
   - group 切换
   - API key 默认部分 mask 展示
   - 完整 key 可从当前页面内存的新 secret 或 `credential.key` 复制

## Rollout Notes

推荐分两步：

1. 先把 `/user` 从 `platforms[]` 收敛到 `groups[]`
2. 再单独处理 discover 的 group-aware selector

第一步的目标是纠正当前 `/user` 页面错误地把 platform 当成 group 一级对象的偏差；第二步才是 CLI discover 合同跟进。
