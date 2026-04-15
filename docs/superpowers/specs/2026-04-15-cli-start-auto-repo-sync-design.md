# CLI Start Auto Repo Sync 设计文档

**Date:** 2026-04-15  
**Status:** Review Requested  
**Scope:** `ae-cli/`, `backend/`, `frontend/`, `docs/`  
**Related:**  
- [2026-03-26-session-pr-attribution-design.md](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-26-session-pr-attribution-design.md)  
- [2026-04-14-scm-credentials-provider-binding-design.md](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-04-14-scm-credentials-provider-binding-design.md)

项目级架构总览与当前实现状态见 [`docs/architecture.md`](../../architecture.md)。

**Spec Relationship:**
- 本文修改 [`2026-03-26-session-pr-attribution-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-26-session-pr-attribution-design.md) 中 `ae-cli start` 必须命中已存在 `repo_config` 的 bootstrap 假设。新的合同改为：bootstrap 对 repo 采用 `find-or-create`，未知 repo 不再因为“未预注册”而失败。
- 本文修改 [`2026-04-14-scm-credentials-provider-binding-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-04-14-scm-credentials-provider-binding-design.md) 中“每个 repo 必须始终绑定一个 active SCM provider”的约束。新的合同改为：repo 与 SCM provider 的关系由 admin 管理，repo 可以先存在于 unbound 状态，后续再绑定。
- 前述历史 spec 保留其各自时间点的设计背景与取舍，不回写其正文来伪装当前演进方向。

## 概述

当前代码要求 repo 必须先在前端手动录入，`ae-cli start` 才能成功 bootstrap session。

实际效果是：

1. 开发者在本地已有一个真实 Git repo
2. `ae-cli start` 已经能稳定拿到 `origin`、branch、head、workspace/git 现场
3. backend 却因为 repo 没有预先落库而直接报错
4. repo 与 `scm_provider` 的管理职责被错误地前置到开发者启动链路

本设计将 repo 的**发现与落库**和 repo 的 **SCM provider 绑定**拆开：

- `ae-cli start` 负责把当前本地 repo 自动同步到 backend
- backend 负责把 repo 持久化为 durable state
- admin 后续再统一管理 repo 与 `scm_provider` 的绑定关系
- 未绑定 repo 仍允许 session / checkpoint / usage / attribution 主链路运行
- 只有真正依赖 SCM provider 的能力才在运行时显式拒绝

这保证：

- 开发者不再被“先去前端加 repo”拦截
- admin 仍然保留 repo 与 provider 关系的最终控制权
- 现有 modular monolith 边界不被破坏

## 背景问题

当前实现中：

1. `ae-cli start` 从本地 Git 读取 `origin` remote URL，并把它作为 `repo_full_name` 发给 `POST /api/v1/sessions/bootstrap`
2. `backend/internal/sessionbootstrap.Service.Bootstrap` 只会按 `repo_config.full_name` 或 `repo_config.clone_url` 查现有 repo
3. 若未命中，bootstrap 直接报 `repo not found`
4. `repo_config` 当前又强制要求绑定 `scm_provider`

这条链路有三个问题：

1. **启动路径承担了错误的前置条件**
   - 本地已经存在真实 repo，但 session bootstrap 仍被后台配置阻断
2. **repo durable state 与 provider binding 被耦合**
   - repo 是否存在，和 repo 是否已经绑定到某个 SCM provider，不应该是同一个门槛
3. **前端手工录入变成开发链路硬依赖**
   - 这与 `ae-cli start` 作为 session bootstrap 主入口的定位冲突

## 目标

1. `ae-cli start` 对未知 repo 自动同步并落库
2. repo 可以在未绑定 `scm_provider` 的情况下存在
3. repo 与 `scm_provider` 的绑定关系继续由 admin 管理
4. bootstrap 不再因为 repo 未预注册而失败
5. session / checkpoint / usage / attribution 主链路继续可用
6. 依赖 SCM provider 的能力对 unbound repo 返回显式错误
7. 避免在 `ae-cli start` 中猜测或强绑 provider

## 非目标

1. 第一版不在 `ae-cli start` 中自动推断或自动绑定 `scm_provider`
2. 第一版不引入单独的 `discovered_repo` / `pending_repo` 新实体
3. 第一版不让普通开发者修改 repo 与 `scm_provider` 的绑定关系
4. 第一版不改变 relay provider / route binding 的核心规则
5. 第一版不把所有旧 spec 重写成“repo 从一开始就允许 unbound”

## 核心决策

| Topic | Decision | Reason |
| --- | --- | --- |
| Repo discovery | `ae-cli start` 通过 bootstrap 自动发现并落库 repo | 去掉前端手工预注册前置条件 |
| Repo binding | repo 与 `scm_provider` 解耦，repo 可先 unbound | 把 durable repo state 与 admin 配置边界拆开 |
| Provider ownership | repo 与 `scm_provider` 的关系仍由 admin 管理 | 保持运营与权限边界清晰 |
| Bootstrap behavior | bootstrap 从 lookup-only 改为 `find-or-create` | 启动链路必须对未知 repo 友好 |
| Repo identity | 引入 `repo_key` 作为稳定主匹配键 | 避免 SSH/HTTPS remote 造成重复 repo |
| Unbound behavior | 未绑定 repo 允许 session 主链路运行 | 启动与归因不应依赖 SCM provider |
| SCM-dependent features | scan / webhook / PR sync / optimize 等仅在 bound repo 上运行 | 明确失败面，避免隐式降级 |
| Error model | unbound repo 返回显式 `repo_unbound` 错误 | 前后端都能稳定处理 |

## Repo 身份模型

### `repo_key`

新增 `repo_key` 作为 repo 的稳定身份键。

要求：

- 全局唯一
- 必填
- 与 `scm_provider` 是否已绑定无关
- 能把同一 repo 的常见 remote 表达收敛到同一个 key

第一版 derivation 规则：

1. 取本地 Git `origin` remote URL
2. 规范化：
   - host 小写
   - 移除协议差异
   - 移除 SSH user 前缀
   - 移除尾部 `.git`
   - 统一路径分隔符
3. 对已知 URL 形态做 provider-aware path 提取：
   - GitHub：`owner/repo`
   - Bitbucket Server HTTP clone：`scm/<project>/<repo>`
   - Bitbucket Server browse/API 形态：`projects/<project>/repos/<repo>`
4. 生成：
   - `<normalized-host>/<normalized-path>`

示例：

- `https://github.com/org/repo.git` -> `github.com/org/repo`
- `git@github.com:org/repo.git` -> `github.com/org/repo`
- `https://bitbucket.example.com/scm/proj/repo.git` -> `bitbucket.example.com/proj/repo`
- `ssh://git@bitbucket.example.com/proj/repo.git` -> `bitbucket.example.com/proj/repo`

### 其他 repo 元数据

repo 自动创建时仍保存：

- `name`
- `full_name`
- `clone_url`
- `default_branch`

但它们的角色调整为：

- `repo_key` 是主匹配键
- `full_name` / `clone_url` / `default_branch` 是 best-effort 元数据
- 后续绑定 `scm_provider` 后，backend 可以再用 provider API 刷新它们

## 数据模型变更

### `repo_config`

合同变更：

1. `repo_config` 新增 `repo_key`
2. `repo_config -> scm_provider` 从 required 改为 optional
3. `repo_config` 允许在 `scm_provider_id = null` 时存在

第一版不新增单独的持久化 `binding_status` 字段，而是定义一个派生状态：

- `bound`：`scm_provider_id != null`
- `unbound`：`scm_provider_id == null`

原因：

- 绑定状态本质上来自 relation 是否存在
- 避免再引入一个与 relation 重复、容易漂移的状态字段

唯一性规则：

1. `repo_key` 全局唯一
2. 现有 `full_name + scm_provider` 约束可保留为兼容索引，但 repo 主查找与去重统一以 `repo_key` 为准

### 自动创建时的字段规则

自动创建 repo 时：

- `repo_key`：由 bootstrap 现场推导
- `name`：remote path 最后一个段
- `full_name`：能解析出 provider-style name 时使用该值，否则使用 path-derived fallback
- `clone_url`：直接保存当前本地 `origin` remote URL
- `default_branch`：优先使用当前 repo HEAD 对应 branch；若不可用则回退到 `main`
- `scm_provider_id`：留空
- `group_id` / `relay_provider_name` / `relay_group_id`：留空，继续按既有 relay 默认规则解析
- `status`：维持现有 repo operational status 语义，不把 unbound 混进 `active/webhook_failed/inactive`

## Bootstrap 合同变更

### 总体行为

`POST /api/v1/sessions/bootstrap` 从“必须命中已有 repo”改为：

1. 根据请求中的 remote 信息推导 `repo_key`
2. 按 `repo_key` 查找 repo
3. 若命中，复用已有 repo
4. 若未命中，自动创建一条 unbound repo
5. 然后继续正常创建 session

### 请求字段

现有请求体中的 repo 字段保持兼容，但语义收敛为“本地 Git remote 事实”，而不再要求它必须是库中已有 `repo_config.full_name`。

第一版兼容策略：

- 现有 `repo_full_name` 字段继续保留，用于承载 `origin` remote URL 或兼容旧客户端的 repo 标识
- backend 在 bootstrap 内部自行做 `repo_key` 推导
- 若后续需要清理命名歧义，再新增更准确的 `repo_remote_url` 字段并通过新 spec 演进

### 幂等与更新规则

bootstrap 命中已有 repo 时：

- 必须复用已有 repo 记录
- 不新建重复 repo
- 可以 best-effort 更新以下元数据：
  - `clone_url`
  - `default_branch`
  - `name`
  - `full_name`

更新原则：

- 不因某次本地 remote 的短期差异而重写 admin 明确配置的 provider relation
- 不因为 repo 处于 unbound 状态而拒绝 session bootstrap

## Repo 与 SCM Provider 的管理边界

repo 与 `scm_provider` 的关系由 admin 管理，具体含义是：

1. 开发者执行 `ae-cli start` 时，不做 provider 猜测
2. backend 自动创建 repo 时，不自动绑定 provider
3. admin 在前端 repo 管理界面里统一完成绑定或调整绑定

绑定完成后，backend 可以执行 bind-time best-effort 操作：

- 用 provider API 校验 repo 是否存在
- 刷新 repo 元数据
- 注册 webhook

但这些都不再是 `ae-cli start` 的前置条件。

## Unbound Repo 的运行时语义

### 允许的能力

对 unbound repo，以下主链路继续可用：

- `ae-cli start`
- session bootstrap / heartbeat / stop
- workspace marker / runtime bundle
- git checkpoint 上报
- session usage event / session event 上报
- PR attribution 中不依赖 SCM API 的内部归因计算

### 禁止的能力

以下能力要求 repo 必须已绑定 `scm_provider`：

- repo scan
- webhook 注册 / 删除
- PR sync
- changed-file lookup
- optimize preview / confirm / create PR
- 任何需要通过 `backend/internal/scm.SCMProvider` 执行的 repo 级 API

### 错误模型

对 unbound repo 调用 SCM-dependent 能力时：

- 返回 `409 Conflict`
- 稳定错误码：`repo_unbound`
- 稳定消息：`repo is not bound to an scm provider`

要求：

- backend handler 不再把这类错误包成泛化的 `500`
- frontend 看到 `repo_unbound` 时显示明确操作提示

## API 变更

### `POST /api/v1/sessions/bootstrap`

行为变更：

- repo 未预注册时不再返回 `repo not found`
- 改为自动创建 unbound repo，再继续 bootstrap

### Repo 读取接口

`GET /api/v1/repos` 与 `GET /api/v1/repos/:id` 应暴露 repo 当前绑定状态。

第一版建议返回：

- `binding_state`: `bound | unbound`

它是派生字段，不要求落库存储。

### Repo 更新接口

`PUT /api/v1/repos/:id` 需要支持 admin 绑定或清空 `scm_provider_id`。

要求：

- 仅 admin 可操作
- `scm_provider_id` 可设置为具体 provider
- `scm_provider_id` 也可显式清空

### Repo 创建接口

admin 手工补录入口应与新合同保持一致：

- `POST /api/v1/repos/direct` 不再强制要求 `scm_provider_id`
- admin 可以先创建 unbound repo，后续再绑定
- 现有依赖 provider 验证的 `POST /api/v1/repos` 仍可保留 provider-required 语义

## 前端管理界面

repo 管理界面需要反映新的 repo 生命周期：

1. repo 列表显示 `Unbound` 标记
2. 支持按 `binding_state` 过滤
3. repo 详情页提供 admin-only 的 SCM provider 绑定入口
4. 对 unbound repo 的 scan / sync / optimize 等操作显示明确禁用状态或错误提示
5. 现有 “Add Repo” 入口降级为 admin 的手工补录工具，而不再是开发者 `ae-cli start` 的前置步骤

## 对现有模块边界的影响

本设计不改变以下原则：

- 项目仍是 modular monolith
- repo durable state 仍归 `backend/internal/repo`
- SCM 集成仍统一经过 `backend/internal/scm.SCMProvider`
- relay/sub2api 集成仍统一经过 `backend/internal/relay.Provider`

变化点仅在于：

- repo durable state 不再依赖 SCM provider relation 先就绪
- `sessionbootstrap` 在 repo 发现阶段直接调用 repo find-or-create 逻辑

## 迁移策略

### Schema / data migration

1. 为 `repo_config` 增加 `repo_key`
2. 对现有 repo 使用已存 `clone_url` 回填 `repo_key`
3. 将 `repo_config -> scm_provider` 改为 optional

### 兼容策略

实现落地过程中：

1. backend 先支持：
   - 旧 repo：通过 `full_name` / `clone_url` 兼容命中
   - 新 repo：通过 `repo_key` 主匹配
2. ae-cli 在 server 升级前，仍可能收到旧版 `repo not found`
3. rollout 完成后，bootstrap 主查找逻辑应统一迁移到 `repo_key`

## 测试要求

### Backend

新增或调整测试覆盖：

1. bootstrap 对未知 repo 自动创建 unbound repo
2. bootstrap 再次启动同一 repo 时复用已有 repo，而不是新建
3. SSH / HTTPS remote 指向同一 repo 时命中同一个 `repo_key`
4. unbound repo 调用 scan / sync / optimize 等接口时返回 `409 repo_unbound`
5. admin 可以把 unbound repo 绑定到 provider，也可以清空绑定

### ae-cli

新增或调整测试覆盖：

1. 在空 repo 库场景下，`ae-cli start` 仍能成功 bootstrap
2. `ae-cli start` 不再把“repo not found”视为正常前置条件
3. CLI 继续把本地 `origin` remote 作为 repo 发现事实发送给 backend

### Frontend

新增或调整测试覆盖：

1. repo 列表能显示 `Unbound`
2. admin 可以完成 repo 绑定
3. unbound repo 的受限操作显示明确提示

## 取舍说明

### 为什么不在 `ae-cli start` 时自动猜 provider

因为 provider 绑定属于 admin 管理面，而不是开发者启动链路。

自动猜 provider 的问题是：

- 规则容易不透明
- 误绑代价高
- 会把配置问题伪装成启动成功

本设计宁可让 repo 先以 unbound 形式存在，也不在 start 阶段做隐式绑定。

### 为什么不新建 `discovered_repo` 实体

因为当前系统里的 session、checkpoint、usage、attribution 都已经围绕 `repo_config` 建模。

若再引入一层 `discovered_repo -> repo_config`：

- 需要额外 promotion 流程
- 需要两套 repo 标识
- 对当前目标“去掉手工预注册拦截”来说收益不够

因此第一版直接扩展 `repo_config` 的生命周期即可。
