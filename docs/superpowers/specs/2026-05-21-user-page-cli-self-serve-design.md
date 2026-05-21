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
- 它**不直接改写** `ae-cli discover` 的写入路径或工具探测合同；工具配置写入规则仍以 [`2026-05-19-ae-cli-deterministic-tool-configuration-design.md`](./2026-05-19-ae-cli-deterministic-tool-configuration-design.md) 为准。
- 它修正的是 `/user` 页面如何表达和调用用户态 credential provisioning，避免再引入独立于现有 provider credential 逻辑之外的 `ae-cli-auto` 专用合同。

## Overview

当前前端里：

1. 有登录页
2. 有 repo / events / settings 等页面
3. 没有面向普通开发者的账户页
4. 没有一处把 `ae-cli` 的安装、登录、discover、自助验证和用户自己的 provider credential 信息收拢到一起

结果是：

1. 普通开发者知道系统“有 CLI”，但缺少一个登录后常驻入口来完成自助配置
2. 当前 `GET /api/v1/providers` 更像 `ae-cli discover` 的程序消费接口，不是为 Web 用户界面设计的账户能力
3. 用户无法在前端清楚区分：
   - 自己是谁
   - 当前有哪些 provider
   - 某个 provider 下有哪些可用 platform
   - 当前平台是否已有可复用 credential
   - 为什么旧 key 不能直接 reveal

因此新增一个普通登录用户可访问的新页面：

```text
/user
```

该页面采用“账户页外壳 + task-first 中心流程”的结构：

1. 页面顶部展示只读 profile
2. 页面主体以 CLI 自助配置为中心
3. provider 选择与 platform 选择和 CLI 命令块联动
4. 页面允许用户贴回本地 CLI 输出进行轻量验证，但**不直接探测或执行**本机命令

## Goals

1. 给普通开发者一个登录后常驻的 `User` 页面，而不是把 CLI 自助入口藏在 admin settings 或 README 里。
2. 让用户能按页面引导完成：
   - install
   - login
   - discover
   - verify
3. 让用户能看见自己当前有哪些 provider，并在进入 credential 自助时显式选择 platform。
4. 给用户提供自己的 provider credential 状态说明，以及安全受限的一次性 reveal / copy / regenerate 交互。
5. 保持后端与前端语义清晰分层：
   - CLI 程序消费接口继续服务 `ae-cli`
   - Web 账户页有自己的用户态接口
   - provider 数据归 DB 所有，config 仅用于 startup bootstrap

## Non-Goals

1. 第一版不做 profile 编辑、密码修改、认证设置修改。
2. 第一版不做浏览器直接执行本地 CLI 命令，也不做本机探测。
3. 第一版不把旧 key 的明文“重新读取”出来；历史 existing key 只能通过重新创建得到新的 secret。
4. 第一版不做完整的多 group / 多 key 管理台；只围绕“当前 provider 下按 platform 自助获取可用 credential”。
5. 第一版不改变 `ae-cli discover` 的写入路径、工具探测机制或本地配置落点。
6. 第一版不引入新的 LLM-based tool discovery。

## Approaches Considered

### Option A: Task-First User Hub with Shared Credential Provisioning

- `/user` 是标准登录后页面
- 顶部是轻量 profile
- 中心是 CLI setup checklist
- provider / platform / credential 状态紧贴 checklist 联动
- `/user` 复用统一的 provider credential provisioning 合同

优点：

1. 最符合普通开发者的首次接入和回访排障路径
2. 避免 Web 页面再发明一套独立于现有逻辑之外的 key 语义
3. install / login / discover / verify 顺序清楚

缺点：

1. 需要把页面和后端都收敛到共享策略，而不是只做局部补丁

### Option B: Task-First User Hub with User-Page-Specific Key Lifecycle

- 页面仍采用 task-first 结构
- 但 `/user` 单独维护一个 Web 专用的 key 命名和选择规则

优点：

1. 局部实现速度快

缺点：

1. 容易再次偏离既有 provider credential 逻辑
2. 会重复制造 `ae-cli-auto` 一类的专用合同

### Option C: Read-Only User Hub

- `/user` 只展示 profile / provider / platform 状态
- 不直接执行 create / regenerate

优点：

1. 页面合同最小

缺点：

1. 用户必须退回 CLI/runtime 才能补齐 credential
2. 自助能力不完整

### Recommendation

采用 **Option A: Task-First User Hub with Shared Credential Provisioning**。

原因：

1. 目标用户是普通开发者，而不是管理员。
2. 目标动作是“把 CLI 配通”，不是“浏览账户资料”。
3. `/user` 不应重新定义 `ae-cli-auto` 之类的独立 key 语义，而应复用统一的 provider credential provisioning 合同。

## Information Architecture

### Route and Entry

- 新增路由：`/user`
- 所有已登录用户可访问
- 不要求 admin
- 入口位于侧边栏底部当前用户信息区域；该区域从纯展示改成可点击，进入 `/user`

### Page Structure

页面从上到下分成 4 个区块：

1. `Profile Summary`
2. `Provider & Platform Credential`
3. `CLI Setup Checklist`
4. `Help / FAQ`

在宽屏下可使用两列布局：

- 左列：profile + provider switcher
- 右列：platform credential + CLI checklist

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

### Provider and Platform Credential

该区块展示当前用户**可见的全部 enabled providers**，不只 primary。

每个 provider 至少显示：

1. `display_name`
2. `name`
3. `base_url`
4. `default_model`
5. `is_primary`

页面始终只允许单选一个 provider。默认选中规则：

1. 优先 `is_primary=true`
2. 否则第一项

选中 provider 后，页面再展示该 provider 下可供当前用户操作的 platform 列表。每个 platform 至少显示：

1. `platform`
2. `group_id`
3. `group_label`
4. `credential.state`
5. `credential.name`
6. `credential.api_key_id`
7. `credential.status`

平台列表第一版的目标不是成为完整 group 管理台，而是把“用户在当前 provider 下对哪个 platform 做 credential 自助”表达清楚。

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
4. 为什么历史 key 不能直接 reveal

## Provider Credential Semantics

### Definition

本页中的 credential 指当前用户在某个 `provider + platform` 视角下，由统一 provisioning 合同解析出的“当前可复用或可创建”的 API credential。

它**不是**：

1. 固定名 `ae-cli-auto` key 的别名
2. provider 级单把 key 的概念
3. 页面专属的独立生命周期

### Canonical Provisioning Contract

`/user` 必须复用统一的 provider credential provisioning 合同：

1. 先列出当前 relay user 在该 provider 下的 active keys
2. 只在目标 `platform` 下筛选
3. 优先匹配“用户同名 key”
4. 如果 `username` 本身就是邮箱别名，则按既有逻辑退化成邮箱前缀，例如 `luxuhui`
5. 没有可复用 key 时，按同样的命名规则创建
6. 创建时必须带 `GroupID`

### Group Resolution

当前页面不直接暴露 group 解析细节，但其 provisioning 必须遵循统一策略：

1. 优先走 platform-aware group 解析
2. 如果 provider 不支持 platform-aware 解析，则回退到现有 route binding / default group 逻辑
3. 该逻辑属于 provisioning 内部实现，不应在 `/user` 页面上表现为“runtime fallback provider”之类的额外配置面

### Server States

后端对 platform credential 只暴露两种持久状态：

1. `missing`
   - 当前 `provider + platform` 下没有可复用 credential
2. `existing_hidden`
   - 当前 `provider + platform` 下已有可复用 credential，但系统拿不到旧 secret 明文

### Client Overlay State

前端在当前页面内存里额外维护一个**非持久**状态：

1. `session_visible`
   - 只在当前页面会话内成立
   - 触发条件是用户刚完成：
     - `Create Key`
     - `Regenerate`
   - 此时前端拿到了新 secret 的一次性返回值

`session_visible` 不是后端事实，也不应通过 `GET` 接口重新读取出来。

刷新页面、重新进入页面或丢失本地内存后，状态必须回退为 `existing_hidden`。

### Create, Reveal, Copy, Regenerate Rules

- `Create Key`
  - 仅在当前 `provider + platform` 处于 `missing` 状态时可用
  - 执行一次 ensure/create，并返回新 secret
  - 成功后前端进入 `session_visible`
- `Reveal`
  - 仅在 `session_visible` 状态可用
  - 用于把当前页面内存中的 secret 明文显示出来
- `Copy`
  - 仅在 `session_visible` 状态可用
  - 将当前页面内存中的 secret 复制到剪贴板
- `Regenerate`
  - 在 `existing_hidden` 或 `session_visible` 状态可用
  - 先撤销当前 `provider + platform` 下按统一合同识别出来的可复用同名 active key，再按同一合同新建
  - 成功后前端进入 `session_visible`

页面必须明确解释：

1. 历史旧 key 的明文不会被重新读取
2. 如果需要重新拿到明文，只能通过 create / regenerate 得到一次性新 secret

## API Contract

### Existing Endpoint Reused

继续复用：

```text
GET /api/v1/auth/me
```

用于 `Profile Summary`。

### Why Not Reuse `GET /api/v1/providers`

现有：

```text
GET /api/v1/providers
```

是 `ae-cli discover` 的程序消费接口，当前语义包含：

1. 以 CLI 配置为目标的数据形状
2. 当前实现里仍带有 provider 级自动创建 API key 的副作用
3. 不表达 Web 页需要的 `provider + platform` credential 展示语义

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
2. 返回每个 provider 下可操作的 platform 摘要
3. 返回每个 `provider + platform` 的 credential 持久状态
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
        "platforms": [
          {
            "platform": "openai",
            "group_id": "42",
            "group_label": "OpenAI / Group 42",
            "credential": {
              "state": "existing_hidden",
              "api_key_id": 12345,
              "name": "luxuhui",
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
2. 不返回旧 secret
3. 不返回 `session_visible`

#### `POST /api/v1/user/providers/:id/platforms/:platform/credential`

用途：

1. 当当前 `provider + platform` 处于 `missing` 时创建 credential

行为：

1. 如果当前 `provider + platform` 已存在可复用 credential，则返回冲突错误
2. 成功时返回一次性 secret

建议响应字段：

```json
{
  "data": {
    "api_key_id": 12346,
    "name": "luxuhui",
    "status": "active",
    "secret": "sk-..."
  }
}
```

#### `POST /api/v1/user/providers/:id/platforms/:platform/credential/regenerate`

用途：

1. 撤销当前 `provider + platform` 下旧的可复用 credential
2. 创建一把新的 credential
3. 返回新 secret 的一次性明文

行为：

1. 如果当前合同匹配到多个 active 同名 key，应一并撤销
2. 创建成功后返回新 key 的一次性 secret

### Backend Boundary

这些接口应复用现有 `relay.Provider` 抽象：

1. `ListUserAPIKeys`
2. `CreateUserAPIKey`
3. `RevokeUserAPIKey`

并应共享统一的 provider credential provisioning 策略，而不是再为 `/user` 页面维护独立的 key 选择代码。

不得绕过 provider 抽象重新引入 direct sub2api DB coupling。

## CLI Checklist UX

### Step 1: Install

显示官方安装命令：

```bash
AE_CLI_INSTALL_SERVER_URL=<current-origin> \
curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | bash
```

说明：

1. macOS / Linux 走官方安装脚本
2. Windows 仍走 release 手动安装

### Step 2: Login

显示：

```bash
ae-cli --server <current-origin> login
```

并补充 headless 说明：

```bash
ae-cli --server <current-origin> login --device
```

本页不托管 CLI 登录流程本身，只做明确引导。

### Step 3: Discover

命令仍跟当前选中的 provider 联动：

```bash
ae-cli --server <current-origin> discover --provider <provider-name>
```

页面同时说明：

1. discover 当前仍按 provider 维度工作
2. platform 选择是 `/user` 页面上的 credential 自助视图，不直接改写 discover CLI 形状
3. discover 可能写入的目标，例如：
   - `~/.codex/config.toml`
   - `~/.ae-cli/env.sh`
   - `~/.claude/settings.json`

该说明只解释当前合同，不做本机探测。

### Step 4: Verify

本页采用“用户贴回结果，页面做轻量判断”的方式。

验证输入至少包括：

1. `ae-cli version`
2. `ae-cli --server <current-origin> discover --dry-run --provider <provider-name>`
3. `ae-cli --server <current-origin> doctor`

页面点击 `Review` 后，只做轻量规则判断：

1. `Looks good`
2. `Needs attention`
3. `Cannot determine`

这不是强验证，不应冒充浏览器对本机状态的真实探测。

## Page State and Interaction

### Initial Load

页面首屏并发请求：

1. `GET /api/v1/auth/me`
2. `GET /api/v1/user/providers`

若 provider 列表为空：

1. 仍显示 profile
2. checklist 可见但 provider-sensitive 操作 disabled
3. 页面展示明确说明，例如：
   - 当前账号未关联 relay user
   - 当前环境没有 enabled provider

### Provider Switching

切换 provider 时：

1. install / login 文案保持全局稳定
2. discover 命令切到当前 provider
3. verify 输入草稿按 provider 维度切换
4. platform 列表切到当前 provider

### Platform Switching

切换 platform 时：

1. credential 面板切到当前 `provider + platform` 的状态
2. secret 展示按 `provider + platform` 维度切换和折叠
3. 当前页面的一次性 secret 仅对对应的 `provider + platform` 有效

### Secret Display Rules

即使当前 `provider + platform` 处于 `session_visible`：

1. 页面初始仍以 masked 模式显示
2. 用户需要点击 `Reveal` 才显示明文
3. 页面可以在 provider 或 platform 切换时自动重新折叠 secret 展示，但不应丢失当前项的内存 secret，除非页面刷新或离开

### Regenerate Confirmation

点击 `Regenerate` 前必须弹确认，明确说明：

1. 旧 key 会被撤销
2. 当前本机如果仍在使用旧 key，需要重新运行 `discover`

## Permissions

### Regular User

普通登录用户可以：

1. 访问 `/user`
2. 查看自己的 profile summary
3. 查看当前用户可见 provider 及其 platform 摘要
4. 对自己的 `provider + platform` credential 执行 create / regenerate

### Admin

Admin 也可以访问 `/user`，但：

1. `/user` 仍是“当前登录用户自己的自助页”
2. 它不替代 admin settings
3. provider 的系统级配置仍在 admin surfaces 中维护

## Error Handling

需要明确处理以下错误：

1. 当前用户没有 `relay_user_id`
2. 当前登录账号在 relay 中找不到映射用户
3. provider 已禁用或不存在
4. 当前 provider 下没有可操作的 platform
5. create 时发现当前 `provider + platform` 已存在可复用 credential
6. regenerate 中 revoke 成功但 create 失败
7. clipboard API 不可用

错误文案要求：

1. 对用户可操作的错误，优先给明确下一步
2. 不泄露旧 secret
3. `Regenerate` 中 revoke 成功但 create 失败时，要明确告知用户当前旧 key 已撤销，需要重试创建

## Runtime Boundary and Data Ownership

### Provider Source of Truth

provider 的 source of truth 是 DB，而不是 runtime fallback config 视图。

具体约束：

1. config 仅用于 `startup bootstrap`
2. 第一次启动时，服务可根据 config 将 primary provider 落到 DB
3. `/user`、`settings`、常规 provider 查询都只读写 DB
4. 不再把 runtime fallback provider 当成产品合同暴露给用户界面

### Why Keep Config at All

保留 config/provider bootstrap 的原因仅是：

1. 服务冷启动可用
2. relay SSO/login 能找到 primary provider

这不意味着 `/user` 或 `settings` 继续依赖 runtime config 视图。

## Testing

至少覆盖：

1. `GET /api/v1/user/providers`
   - 无 relay user
   - 单 provider / 单 platform
   - 单 provider / 多 platform
   - 多个 active 同名 key 时的 canonical selection
   - `username` 为邮箱别名时退化成邮箱前缀
2. `POST /api/v1/user/providers/:id/platforms/:platform/credential`
   - missing -> create
   - 已存在可复用 credential -> 409
   - create 时带 `GroupID`
3. `POST /api/v1/user/providers/:id/platforms/:platform/credential/regenerate`
   - revoke + recreate success
   - revoke success but create failure
4. 前端 `/user`
   - provider 切换
   - platform 切换
   - secret reveal/copy 仅在本次页面会话有效
   - `missing / existing_hidden / session_visible` 下的按钮状态
5. 文档
   - `docs/architecture.md` 反映 provider DB ownership 与 startup bootstrap only 边界

## Rollout Notes

推荐分两步：

1. 先修正 `/user` 的后端合同与前端交互
2. 再考虑是否收敛当前 `GET /api/v1/providers` 内部的 provider credential provisioning 实现，避免 CLI 与 `/user` 再次漂移

第一步的目标是纠正当前 `/user` 页面新引入的偏差；第二步才是更广泛的后端合同收敛。
