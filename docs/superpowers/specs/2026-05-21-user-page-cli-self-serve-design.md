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

- 本文定义一个新的登录后普通用户页面 `/user`，用于承载账户信息、CLI 安装/登录/配置引导，以及用户自己的 managed API key 自助能力。
- 它**不改变** `ae-cli install.sh` 的分发合同；CLI 安装入口仍以 [`2026-04-13-ae-cli-user-install-design.md`](./2026-04-13-ae-cli-user-install-design.md) 为准。
- 它**不改变** `ae-cli login` 的 PKCE / device flow 合同；CLI 登录协议仍以 [`2026-04-15-oauth-device-login-design.md`](./2026-04-15-oauth-device-login-design.md) 为准。
- 它**不改变** `ae-cli discover` 的确定性配置合同；工具配置写入规则仍以 [`2026-05-19-ae-cli-deterministic-tool-configuration-design.md`](./2026-05-19-ae-cli-deterministic-tool-configuration-design.md) 为准。
- 它补的是“普通开发者如何在 Web 里被引导完成 install -> login -> discover -> verify，并理解自己当前 provider / managed key 状态”的产品表面，不是新的 CLI runtime 协议。

## Overview

当前前端里：

1. 有登录页
2. 有 repo / events / settings 等页面
3. 没有面向普通开发者的账户页
4. 没有一处把 `ae-cli` 的安装、登录、discover、自助验证和用户自己的 API key 信息收拢到一起

结果是：

1. 普通开发者知道系统“有 CLI”，但缺少一个登录后常驻入口来完成自助配置
2. 当前 `GET /api/v1/providers` 更像 `ae-cli discover` 的程序消费接口，不是为 Web 用户界面设计的账户能力
3. 用户无法在前端清楚区分：
   - 自己是谁
   - 当前有哪些 provider
   - 哪个 provider 正在被用于 CLI discover
   - 当前 managed key 是否存在
   - 为什么旧 key 不能直接 reveal

因此新增一个普通登录用户可访问的新页面：

```text
/user
```

该页面采用“账户页外壳 + task-first 中心流程”的结构：

1. 页面顶部展示只读 profile
2. 页面主体以 CLI 自助配置为中心
3. provider 选择和 managed key 状态与 CLI 命令块联动
4. 页面允许用户贴回本地 CLI 输出进行轻量验证，但**不直接探测或执行**本机命令

## Goals

1. 给普通开发者一个登录后常驻的 `User` 页面，而不是把 CLI 自助入口藏在 admin settings 或 README 里。
2. 让用户能按页面引导完成：
   - install
   - login
   - discover
   - verify
3. 让用户能看见自己当前有哪些 provider，并显式选择 CLI discover 将使用哪个 provider。
4. 给用户提供自己的 managed API key 状态说明，以及安全受限的 reveal / copy / regenerate 交互。
5. 保持后端与前端语义清晰分层：
   - CLI 程序消费接口继续服务 `ae-cli`
   - Web 账户页有自己的用户态接口

## Non-Goals

1. 第一版不做 profile 编辑、密码修改、认证设置修改。
2. 第一版不做浏览器直接执行本地 CLI 命令，也不做本机探测。
3. 第一版不把旧 key 的明文“重新读取”出来；历史 existing key 只能通过 regenerate 得到新的 secret。
4. 第一版不做多 key 管理台；只围绕系统托管的 `ae-cli-auto` managed key。
5. 第一版不改变 `ae-cli discover` 的写入路径、provider 选择合同或工具探测机制。
6. 第一版不引入新的 LLM-based tool discovery。

## Approaches Considered

### Option A: Task-First User Hub

- `/user` 是标准登录后页面
- 顶部是轻量 profile
- 中心是 CLI setup checklist
- provider / managed key 紧贴 checklist 联动

优点：

1. 最符合普通开发者的首次接入和回访排障路径
2. install / login / discover / verify 顺序清楚
3. 页面虽然叫 `User`，但不会退化成纯资料展示页

缺点：

1. 比传统账户页更强调任务，而不是 profile

### Option B: Standard Account Tabs

- Profile / CLI Setup / API Keys 分成平级 tab

优点：

1. 信息架构最传统
2. 资料、命令、secret 区隔很清楚

缺点：

1. onboarding 路径被拆散
2. install / login / discover / verify 不再是一个连续任务流

### Option C: Toolbox Overview

- 把 profile、install、login、discover、keys、verify 全做成并列工具卡片

优点：

1. 适合熟悉系统的回访用户

缺点：

1. 首次使用的叙事弱
2. 页面更像 utility dashboard，不像可自解释的用户页

### Recommendation

采用 **Option A: Task-First User Hub**。

原因：

1. 目标用户是普通开发者，而不是管理员。
2. 目标动作是“把 CLI 配通”，不是“浏览账户资料”。
3. 该方案能在不破坏账户页语义的前提下，把 CLI setup 当成页面主任务。

## Information Architecture

### Route and Entry

- 新增路由：`/user`
- 所有已登录用户可访问
- 不要求 admin
- 入口位于侧边栏底部当前用户信息区域；该区域从纯展示改成可点击，进入 `/user`

### Page Structure

页面从上到下分成 4 个区块：

1. `Profile Summary`
2. `Provider & Managed Key`
3. `CLI Setup Checklist`
4. `Help / FAQ`

在宽屏下可使用两列布局：

- 左列：profile + provider switcher
- 右列：CLI checklist + managed key

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

### Provider and Managed Key

该区块展示当前用户**可见的全部 enabled providers**，不只 primary。

每个 provider 至少显示：

1. `display_name`
2. `name`
3. `base_url`
4. `default_model`
5. `is_primary`
6. 当前 managed key 状态

页面始终只允许单选一个 provider。默认选中规则：

1. 优先 `is_primary=true`
2. 否则第一项

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

## Managed Key Semantics

### Definition

本页中的 “managed key” 指当前用户在某个 relay provider 下，由平台自助管理的：

```text
name == "ae-cli-auto"
status == "active"
```

的 API key。

### Canonical Selection

如果某个 provider 下存在多个 active 的 `ae-cli-auto` key：

1. 列表页按 `created_at desc` 选择最新的一把作为当前 managed key
2. 页面不在第一版暴露多 key 管理 UI
3. `Regenerate` 时应撤销该 provider 下所有 active 的 `ae-cli-auto` key，再创建一把新的 managed key

这样可以把页面语义稳定成“每个用户-每个 provider 视角下只有一把当前 managed key”。

### Server States

后端对 managed key 只暴露两种持久状态：

1. `missing`
   - 当前 provider 下没有 active 的 `ae-cli-auto` key
2. `existing_hidden`
   - 当前 provider 下有 active 的 `ae-cli-auto` key，但系统拿不到旧 secret 明文

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
  - 仅在 `missing` 状态可用
  - 创建一把新的 managed key
  - 成功后前端进入 `session_visible`
- `Reveal`
  - 仅在 `session_visible` 状态可用
  - 用于把当前页面内存中的 secret 明文显示出来
- `Copy`
  - 仅在 `session_visible` 状态可用
  - 将当前页面内存中的 secret 复制到剪贴板
- `Regenerate`
  - 在 `existing_hidden` 或 `session_visible` 状态可用
  - 先 revoke 当前 managed key，再 create
  - 成功后前端进入 `session_visible`

页面必须明确解释：

1. 历史旧 key 的明文不会被重新读取
2. 如果需要重新拿到明文，只能通过 regenerate 新 key

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
2. 必要时自动创建 `ae-cli-auto` key 的副作用
3. 不表达 Web 页需要的 managed-key 展示语义

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
2. 返回每个 provider 的 managed key 持久状态
3. **不自动创建** key

建议响应字段：

```json
{
  "data": {
    "providers": [
      {
        "id": 1,
        "name": "sub2api-prod",
        "display_name": "sub2api Production",
        "base_url": "https://...",
        "default_model": "claude-sonnet-4-20250514",
        "is_primary": true,
        "managed_key": {
          "state": "existing_hidden",
          "api_key_id": 12345,
          "name": "ae-cli-auto",
          "status": "active",
          "created_at": "2026-05-21T01:02:03Z",
          "last_used_at": "2026-05-21T09:00:00Z"
        }
      }
    ],
    "message": ""
  }
}
```

约束：

1. `managed_key.state` 只返回 `missing | existing_hidden`
2. 不返回旧 secret
3. 不返回 `session_visible`

#### `POST /api/v1/user/providers/:id/managed-key`

用途：

1. 当 provider 处于 `missing` 时创建 managed key

行为：

1. 如果当前 provider 已存在 active `ae-cli-auto` key，则返回冲突错误
2. 成功时返回一次性 secret

建议响应字段：

```json
{
  "data": {
    "api_key_id": 12346,
    "name": "ae-cli-auto",
    "status": "active",
    "secret": "sk-..."
  }
}
```

#### `POST /api/v1/user/providers/:id/managed-key/regenerate`

用途：

1. 撤销当前 provider 下旧的 active managed key
2. 创建一把新的 managed key
3. 返回新 secret 的一次性明文

行为：

1. 如果当前 provider 下存在多个 active `ae-cli-auto` key，应一并撤销
2. 创建成功后返回新 key 的一次性 secret

### Backend Boundary

这些接口应复用现有 `relay.Provider` 抽象：

1. `ListUserAPIKeys`
2. `CreateUserAPIKey`
3. `RevokeUserAPIKey`

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

命令必须跟当前选中的 provider 联动：

```bash
ae-cli --server <current-origin> discover --provider <provider-name>
```

页面同时说明 discover 可能写入的目标，例如：

1. `~/.codex/config.toml`
2. `~/.ae-cli/env.sh`
3. `~/.claude/settings.json`

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
4. managed key 面板切到当前 provider 的状态

### Secret Display Rules

即使当前 provider 处于 `session_visible`：

1. 页面初始仍以 masked 模式显示
2. 用户需要点击 `Reveal` 才显示明文
3. 页面可以在 provider 切换时自动重新折叠 secret 展示，但不应丢失当前 provider 的内存 secret，除非页面刷新或离开

### Regenerate Confirmation

点击 `Regenerate` 前必须弹确认，明确说明：

1. 旧 key 会被撤销
2. 当前本机如果仍在使用旧 key，需要重新运行 `discover`

## Permissions

### Regular User

普通登录用户可以：

1. 访问 `/user`
2. 查看自己的 profile summary
3. 查看自己可见的 providers
4. 创建 / 重建自己的 managed key
5. 在当前页面会话内 reveal / copy 新创建的 secret

### Admin

admin 使用相同页面，不额外获得另一个 admin-only 视角。  
`/user` 的目标是“当前登录者的自助配置页”，不是用户管理台。

## Error Handling

1. 当前账号未映射到 relay user 时，不返回 500；返回用户可读提示。
2. 某个 provider 的 key 创建失败时，只影响该 provider 面板，不清空整页。
3. `Regenerate` 中 revoke 成功但 create 失败时，要明确告知用户当前旧 key 已撤销，需要重试创建。
4. `Verify` 无法判断时，返回 `Cannot determine`，而不是伪造成功。

## Security Notes

1. 一次性 secret 只保存在前端内存里。
2. 不写入 `localStorage`。
3. 不写入 URL。
4. 不通过后续 `GET` 接口返回旧 secret。
5. 历史 key 的明文永不回读。

## Testing

### Backend

至少覆盖：

1. `GET /api/v1/user/providers`
   - 无 relay user
   - 无 enabled provider
   - `missing`
   - `existing_hidden`
   - 多个 active managed keys 时的 canonical selection
2. `POST /api/v1/user/providers/:id/managed-key`
   - 缺失时创建成功
   - 已存在时冲突
3. `POST /api/v1/user/providers/:id/managed-key/regenerate`
   - revoke + create 成功
   - revoke 失败
   - revoke 成功但 create 失败

### Frontend

至少覆盖：

1. `/user` 路由与侧边栏入口
2. provider 切换时命令联动
3. managed key 在 `missing / existing_hidden / session_visible` 下的按钮状态
4. secret reveal / copy 的前端内存语义
5. verify 文本贴回后的轻量状态判断

## Documentation Impact

本 spec 是**设计文档更新**，不是当前实现变更。

在代码真正落地时，应同步更新：

1. `docs/architecture.md`
   - 增加 `/user` 页面及其与 `ae-cli` / relay provider 的关系
2. 必要的前端导航说明
3. 若实现阶段调整了用户态 key 接口合同，应回写本文而不是写散在 PR 描述里
