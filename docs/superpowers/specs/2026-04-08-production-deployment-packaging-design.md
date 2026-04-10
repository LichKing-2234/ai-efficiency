# Production Deployment Packaging Design

## Overview

本 spec 定义 `ai-efficiency` 的官方生产部署形态、交付边界、配置模型、预检机制与升级路径，并补充 `deploy/` 目录下用于本地验证的非生产 compose 变体。

目标不是把 `ai-efficiency` 变成“零依赖单机程序”，而是把它定义成一个**独立产品**：可单独交付、可统一部署、可在启动前检查外部依赖是否 ready，并以稳定的运维入口对外提供服务。

本文参考 `sub2api` 当前的生产交付体验，吸收其统一部署入口、`.env` 配置模型、Docker Compose 编排与健康检查思路；同时保留 `ai-efficiency` 作为独立系统的边界，不假设它与 `sub2api` 同源码、同生命周期、同部署单元。

## Spec Relationship

- 本文是部署与打包方向的新增合同，不覆盖 [`2026-03-24-oauth-cli-login-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-24-oauth-cli-login-design.md)、[`2026-03-26-session-pr-attribution-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-03-26-session-pr-attribution-design.md)、[`2026-04-02-local-session-proxy-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-04-02-local-session-proxy-design.md) 中关于 relay、session、proxy 的业务合同。
- 本文补充“产品如何交付与部署”的合同；业务模块边界仍以 [`docs/architecture.md`](/Users/admin/ai-efficiency/docs/architecture.md) 为当前项目级总览。
- 本文明确区分“当前代码现状”和“目标交付合同”。除非代码已经实现，否则不要把本文中的目标部署体验写成当前现状。

## Scope

本文覆盖：

1. 官方推荐的生产部署主线
2. 应用镜像与编排边界
3. 外部依赖模型与 readiness 预检
4. 面向运维的配置模型
5. 健康检查与降级语义
6. 在线更新与回滚能力
7. 升级与版本演进的基本路径
8. 面向本地验证的 `dev` / `local` compose 变体
9. 去除 SQLite 运行时依赖的阶段性路线

本文不覆盖：

- Kubernetes / Helm 方案
- 完整的二进制 + `systemd` 安装实现
- `sub2api` 自身的部署与升级
- 业务功能层的 API 合同变更
- 将现有生产 compose 改造成直接复用 SQLite 文件的长期运行模式
- 一次性完成全部测试基础设施从 SQLite 到 Postgres 的迁移

## Current State

截至本文撰写时，仓库已有以下基础：

- `deploy/Dockerfile` 采用多阶段构建，构建时会先生成前端产物，再构建后端镜像。
- `deploy/docker-compose.yml` 已可启动 `backend`、`postgres`、`redis` 三个服务。
- 后端配置已经支持环境变量覆盖，使用 `AE_` 前缀。

但当前代码仍未构成本文定义的完整官方交付体验，至少还缺少：

- 面向生产的统一部署入口脚本
- 面向运维的 `.env` 模板与生成流程
- 对外部 `sub2api`、数据库、Redis 的系统化 preflight 检查
- 清晰区分 `liveness` / `readiness` / `degraded` 的健康语义
- 产品级在线更新入口与回滚能力
- 成熟的升级入口与回滚说明
- 参考 `sub2api` 的本地 `dev` / `local` compose 变体
- 统一本地开发与生产运行时的数据库口径

因此，本文描述的是**目标合同**，不是对当前实现状态的复述。

## Goals

1. 将 `ai-efficiency` 定义为一个独立产品，而不是附属于 `sub2api` 的脚本集合
2. 提供统一、低摩擦、可重复的生产部署入口
3. 允许外部依赖存在，但要求部署前能明确验证其 readiness
4. 对运维暴露清晰的配置模型、健康状态和升级路径
5. 提供与 `sub2api` 行为能力对齐的在线更新能力：检测新版本、一键应用更新、支持回滚
6. 保持与当前模块化单体架构一致，不引入新的跨模块隐式耦合
7. 在不污染生产 compose 语义的前提下，为 `deploy/` 提供面向本地验证的 `dev` / `local` 路径
8. 让 backend 运行时彻底摆脱 SQLite，仅保留 Postgres 作为唯一数据库
9. 将测试侧 SQLite 迁移拆到后续阶段，避免与运行时切换绑定在同一批次

## Non-Goals

1. 不追求“单二进制零依赖”交付
2. 不要求将 `sub2api` 纳入 `ai-efficiency` 同一 Compose 栈
3. 不强制所有生产环境都自带本地 `Postgres` / `Redis`
4. 不在 v1 同时提供 Compose、Kubernetes、`systemd` 三套等价主线
5. 不要求 v1 覆盖任意第三方自定义部署拓扑的无差别在线更新
6. 不把现有 `deploy/docker-compose.yml` 和 `deploy/docker-compose.external.yml` 改造成兼容 SQLite 文件复用的本地测试入口
7. 不在本轮同时完成所有 SQLite 测试迁移

## Deployment Positioning

### Product Definition

`ai-efficiency` 在生产环境中的官方定位是：

> 一个独立交付的业务系统，官方推荐通过 Docker Compose 部署；部署流程允许依赖外部 `Postgres`、`Redis`、`sub2api`，并在启动前检查这些依赖是否 ready。

这个定义有三个边界：

1. **独立产品**：可以单独发版、单独升级、单独运维
2. **允许外部依赖**：不把外部基础设施的存在视为“不是一件部署”
3. **单一业务入口**：前端和后端作为同一个产品入口交付，而不是要求运维再单独部署第二套 Web 服务

### Recommended Production Path

官方推荐的生产部署主线为：

- 单机 `Docker Compose`
- 统一部署脚本
- `.env` 作为运维主配置
- 应用镜像包含前端构建产物与后端服务

`systemd` 安装路径允许在后续版本补充，但不是 v1 的官方主线。

## Runtime and Packaging Boundaries

### Application Image

官方应用镜像应承载：

- 后端 API 服务
- 前端构建产物
- 应用启动所需的最小运行时依赖

运维应把 `ai-efficiency` 视为一个应用镜像，而不是前后端两套独立部署单元。

### External Dependencies

生产部署允许依赖以下外部能力：

- `Postgres`
- `Redis`
- `sub2api`
- 可选的反向代理 / TLS 终止层

其中：

- `sub2api` 始终视为**外部集成边界**
- 不要求 `sub2api` 与 `ai-efficiency` 同机、同网络、同生命周期
- 不允许为了部署方便重新引入 direct DB coupling 到 `sub2api`

### Production Compose Modes

Docker Compose 必须支持两种模式：

1. **Bundled Infra Mode**
   - 启动 `backend + postgres + redis`
   - 适用于快速自托管和中小规模部署

2. **External Infra Mode**
   - 只启动 `backend`
   - `Postgres`、`Redis`、`sub2api` 都连接外部地址
   - 适用于企业已有基础设施的场景

无论哪种模式，`sub2api` 都不纳入 `ai-efficiency` 官方 Compose 编排。

### Local Validation Compose Modes

除生产 compose 外，`deploy/` 还应补充两种**非生产**本地验证路径，参考 `sub2api` 的 `docker-compose.dev.yml` 与 `docker-compose.local.yml` 设计：

1. **Dev Compose Mode**
   - 文件建议为 `deploy/docker-compose.dev.yml`
   - 面向“当前仓库源码改动验证”
   - 使用本地源码构建应用容器
   - 启动 `backend + postgres + redis`
   - 不包含 updater sidecar

2. **Local Compose Mode**
   - 文件建议为 `deploy/docker-compose.local.yml`
   - 面向“长期保留或可搬运的本地测试环境”
   - 仍启动 `backend + postgres + redis`
   - 使用本地目录挂载保存应用数据、Postgres 数据和 Redis 数据
   - 不包含 updater sidecar

这两条路径的目标是本地验证和开发测试，不是生产交付主线。

### SQLite Removal Roadmap

SQLite 的移除分两阶段进行：

1. **Phase 1: Runtime Removal**
   - backend 运行时仅支持 Postgres
   - 本地 `dev` / `local` compose 仅支持 Postgres
   - 删除 SQLite 迁移脚本、样例配置和当前主文档中的 SQLite 运行口径

2. **Phase 2: Test Migration**
   - 将测试基座从 SQLite 迁移到 Postgres
   - 删除 `go-sqlite3` 依赖
   - 清理遗留的 SQLite-only 测试辅助与文档背景

本文当前要求实现的是 **Phase 1**。Phase 2 是后续独立批次，不与运行时切换绑定交付。

## Deployment Entry Point

### Official Entry

官方部署入口定义为部署脚本，例如：

- `deploy/docker-deploy.sh`

它的职责是：

1. 准备部署目录中的必要文件
2. 生成或校验 `.env`
3. 生成必须的随机密钥
4. 执行 preflight 检查
5. 输出下一步启动或升级指令

官方推荐的运维路径应是“先执行部署脚本，再启动 Compose”，而不是要求运维手工理解所有配置细节。

### Manual Path

仍然允许高级用户手工执行：

- 复制 `.env.example`
- 手动修改配置
- 直接运行 `docker compose up -d`

但文档主叙事应以官方脚本入口为准。

### Local Validation Entry

本地验证路径允许直接通过 compose 文件启动，而不是强制经过生产 preflight：

- `docker compose -f deploy/docker-compose.dev.yml up --build`
- `docker compose -f deploy/docker-compose.local.yml up -d`

它们属于开发/测试入口，不应覆盖生产部署脚本的职责。

## Configuration Model

### Main Operator Configuration

运维主配置文件定义为：

- `deploy/.env.example` 作为模板
- 部署目录中的 `.env` 作为实际配置

设计原则：

- 日常部署以 `.env` 为中心
- 能不要求改 YAML 就不要求改 YAML
- 变量命名按功能分组，避免散乱

### Advanced Configuration

高级用户可选挂载更细粒度的配置文件，例如：

- `config.yaml`

但这条路径是高级覆盖机制，不应取代 `.env` 成为官方主配置面。

### Config Groups

`.env` 至少应包含以下分组：

1. 镜像与版本
2. 服务监听与公开端口
3. `Postgres` 连接
4. `Redis` 连接
5. `sub2api` / relay 连接
6. JWT / encryption / 安全密钥
7. 在线更新配置，例如版本源、更新代理、更新通道
8. 初始管理员或引导开关
9. 数据目录与持久化选项

### Mapping Strategy

部署层面的 `.env` 变量与应用内部配置之间，需要有稳定的映射规则：

- 对已有 `AE_` 配置项保持兼容
- 部署脚本和 Compose 可以把更运维友好的变量名映射成应用读取的环境变量
- 不要求业务代码直接感知所有部署层别名

目标是既保留当前配置读取方式，又提供更适合运维的部署界面。

### Runtime Database Contract

当前生效合同应收敛为：

- backend 在所有运行模式下都只支持 Postgres
- `DB.DSN` 必须显式配置为 Postgres DSN
- 本地开发不再依赖 `backend/ai_efficiency.db`

历史 SQLite 数据可以在迁移完成前作为离线备份保留，但不再属于系统运行时设计的一部分。

## Preflight and Readiness

### Preflight Requirements

官方部署脚本在启动前必须执行 preflight 检查。

最少包括：

1. Docker / Docker Compose 可用性
2. 端口占用检查
3. 数据目录存在且可写
4. `.env` 必需字段完整
5. 安全密钥满足最小长度或格式要求
6. `Postgres` 可连通
7. `Redis` 可连通
8. `sub2api` 可连通且满足最小 API 契约

### Failure Semantics

preflight 失败时：

- 默认阻止继续启动
- 输出明确的缺失项与修复建议
- 区分“配置错误”、“依赖不可达”、“版本不兼容”、“权限问题”

不允许把 preflight 失败降级成模糊日志然后继续半启动。

### sub2api Contract Check

对 `sub2api` 的 preflight 不只检查 TCP 可达，还应检查最小集成契约，例如：

- 健康端点可访问
- 管理 API 或业务 API 的基础认证方式可用
- 所需的 relay/provider 配置项齐全

本文不绑定具体端点细节，但要求实现时把“是否满足最小集成合同”作为显式检查项，而不是仅做 ping。

## Health Model

### Health Levels

运行时应至少区分三种状态：

1. **Liveness**
   - 进程是否存活

2. **Readiness**
   - 当前是否具备对外提供完整服务的条件

3. **Degraded**
   - 核心站点存活，但部分依赖异常，导致部分能力不可用

### Expected Behavior

示例：

- 后端进程正常，但数据库不可连接：`not ready`
- 后端进程正常，数据库和 Redis 正常，但 `sub2api` 不可达：`degraded` 或按功能分级的 `not ready`
- 全部关键依赖正常：`ready`

具体哪个依赖属于“阻断启动”还是“允许降级”应在实现时明确，但不能只返回单一的 `"ok"`。

## Online Update

### Product Contract

为了与 `sub2api` 对齐，`ai-efficiency` 必须提供产品级在线更新能力，而不是只保留手动升级文档。

最小行为合同为：

1. 管理后台可检测是否存在新版本
2. 管理员可触发一键更新
3. 更新过程会下载并应用新版本
4. 更新失败或升级后异常时，系统支持回滚到上一个可运行版本

这里对齐的是**行为能力**，不是要求实现机制必须与 `sub2api` 完全相同。

### Supported Scope

在线更新至少必须覆盖官方支持的生产部署主线。

在本文定义下，这首先指向官方 Docker Compose 部署路径。若后续补充 `systemd` 安装路径，应优先复用同一套“检测更新 / 应用更新 / 回滚”的产品接口与状态模型，而不是再发明第二套完全不同的交互。

### Update Flow

建议的在线更新流程为：

1. 管理员在后台点击“检测更新”
2. 后端查询官方版本源，比较当前版本与最新可用版本
3. UI 展示可升级版本、关键提示与风险说明
4. 管理员确认后触发受控升级
5. 系统执行更新前检查，必要时提示备份
6. 系统下载并应用新版本
7. 系统完成重启与健康检查
8. 若更新失败或健康检查不通过，提供回滚入口或自动回滚能力

### Update Configuration

在线更新相关配置至少应包括：

- 官方版本源或发布通道
- 可选更新代理配置
- 是否允许自动检查更新
- 当前部署是否支持在线应用更新的能力标记

这类配置应纳入 `.env` 主配置面，而不是要求运维直接改内部实现文件。

### Safety Requirements

在线更新必须满足以下要求：

1. 仅管理员可见、可操作
2. 更新前能识别当前部署形态是否受支持
3. 更新前执行最小版本兼容与环境检查
4. 更新过程对用户有可见状态，不允许静默失败
5. 回滚路径必须明确且可执行

### Manual Fallback

即使存在在线更新能力，系统仍应保留手动升级路径作为兜底。

如果当前部署不满足在线更新前提，后台必须明确提示“当前环境仅支持手动升级”，而不是暴露一个不可用的按钮。

## Upgrade Path

### V1 Upgrade Experience

v1 的升级体验定义为：

1. 保留现有 `.env`
2. 支持在管理后台检测新版本
3. 支持管理员触发在线更新
4. 更新过程中保留明确的手动兜底路径
5. 应用在启动过程中完成数据库迁移

### Upgrade Requirements

升级文档与脚本应尽量提供：

- 升级前备份建议
- 数据卷保留说明
- 破坏性配置变更提示
- 最小回滚指引
- 在线更新失败时的恢复步骤

本文要求 v1 即具备后台可见的在线更新能力。

## Observability and Operations

官方部署方案至少应给运维提供：

1. 服务日志查看入口
2. 健康检查入口
3. 配置错误的显式反馈
4. 常见依赖异常的诊断提示
5. 版本信息、更新状态与最近一次更新结果

部署文档应围绕以下任务组织：

- 首次安装
- 修改配置
- 升级
- 查看状态
- 排查依赖异常

## Documentation and Messaging

对外文案应统一为：

- `ai-efficiency` 是独立产品
- 官方推荐 Docker Compose 部署
- 前端与后端作为单一应用镜像交付
- 运行时允许依赖外部 `Postgres`、`Redis`、`sub2api`
- 部署前会进行依赖 readiness 检查
- `sub2api` 是外部集成边界，不属于同一部署生命周期

不应使用以下容易误导的表述：

- “单二进制部署”
- “零依赖部署”
- “与 sub2api 一起一键全托管”

## Acceptance Criteria

当以下条件满足时，可以认为本 spec 被实现：

1. 仓库提供官方部署入口脚本
2. 仓库提供 `.env.example` 与面向运维的部署说明
3. Docker Compose 支持 bundled infra 与 external infra 两种模式
4. 应用镜像作为单一业务入口交付
5. 部署前能显式检查 `Postgres`、`Redis`、`sub2api` readiness
6. 运行时健康状态不再只有单一存活检查
7. 管理后台提供“检测更新”入口
8. 系统支持一键应用更新
9. 系统支持明确可执行的回滚路径
10. 升级路径有明确文档和最小可行操作说明

## Rollout Notes

建议按以下顺序落地：

1. 先统一 Compose 和 `.env` 模型
2. 再补部署脚本与 preflight
3. 然后实现在线更新与回滚主路径
4. 再完善健康检查语义
5. 最后补升级文档与 `systemd` 备选路径

这样可以先建立稳定的官方生产主线，再逐步扩展交付方式，而不会在 v1 同时维护过多部署模型。
