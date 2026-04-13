# Docker Deploy UX And Config Surface Design

**Status:** Current contract for operator-facing Docker deploy output and default configuration surface

## Overview

本 spec 约束 `deploy/docker-deploy.sh` 与 Docker 默认部署模板的“用户可见层”行为，目标是让 `ai-efficiency` 的 Docker 主路径在体验上对齐 `sub2api` 当前的部署风格：

- 脚本输出按阶段组织，结果清晰，可快速扫读
- 默认 `.env` 只暴露操作者必须理解和必须填写的配置
- 镜像仓库、镜像 tag、updater 内部实现细节不再成为默认路径下的显式配置负担

本文不改变 `2026-04-08-production-deployment-packaging-design.md` 已定义的运行时拓扑、Compose 模式、bootstrap 机制或 updater sidecar 架构；它只收紧“用户怎么感知和操作这条 Docker 主线”的当前合同。

## Spec Relationship

- 本文继承 [`2026-04-08-production-deployment-packaging-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-04-08-production-deployment-packaging-design.md) 中关于 Docker bootstrap、bundled/external Compose 模式、updater sidecar 与本地验证 compose 变体的总体合同。
- 当 `2026-04-08-production-deployment-packaging-design.md` 中关于 Docker 入口、`.env` 模板、脚本输出的描述与本文冲突时，以本文为准。
- 本文不覆盖 [`2026-04-09-binary-systemd-install-update-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-04-09-binary-systemd-install-update-design.md) 的 systemd 安装路径。
- 本文不改变业务模块边界；项目级运行时关系仍以 [`docs/architecture.md`](/Users/admin/ai-efficiency/docs/architecture.md) 为总览。

## Problem Statement

截至 2026-04-13，仓库已经具备 Docker bootstrap 和 preflight 能力，但默认操作者体验仍有两个明显问题：

1. `deploy/docker-deploy.sh` 的输出偏技术细节，缺少阶段化标题、结果汇总和明确 next steps，用户很难像阅读 `sub2api` 当前 `docker-deploy.sh` 一样快速理解“脚本做了什么、生成了什么、下一步该做什么”。
2. `deploy/.env.example` 把“默认操作者必须关心的配置”与“镜像版本、updater 内部实现、更新控制面细节”混在一起，导致默认路径暴露了过多不必要的概念。

当前需要解决的是 Docker 主线的可用性和认知负担，而不是重新设计部署拓扑。

## Goals

1. 让 `deploy/docker-deploy.sh` 的 bootstrap 与 preflight 输出都具备统一、友好的阶段化结构
2. 让默认 `.env` 与 bootstrap 生成的 `.env` 更接近 `sub2api` 的“面向操作者，而非面向实现细节”的配置模型
3. 让默认 Docker 主线直接使用 `latest` 镜像，不要求操作者理解或编辑 image tag
4. 保持现有 bundled/external、bootstrap/preflight、updater sidecar 行为兼容
5. 为高级用户保留版本和镜像源覆盖能力，但不把这些覆盖项放进默认主路径

## Non-Goals

1. 不移除 `deploy/` 中现有的多套 Compose 文件
2. 不取消 external 模式
3. 不移除 updater sidecar 或在线更新能力
4. 不把 Docker 路径改成单二进制或 `systemd` 路径
5. 不重写 release bundle 结构
6. 不要求在这一轮改动里重新设计后端 deployment API

## Design

## Operator-Facing Output Contract

### Shared Principles

- bootstrap 与 preflight 都必须使用统一的输出风格：
  - 标题块
  - 阶段化 `[INFO]`
  - 完成态 `[SUCCESS]`
  - 风险提示 `[WARNING]`
  - 失败态 `[ERROR]`
  - 末尾摘要块
- 用户可见输出必须围绕“结果”而不是“内部函数调用”组织。
- 成功路径必须始终以 “Preparation/Preflight Complete” 和 “Next steps” 收尾。
- 失败路径必须在退出前说明失败发生在哪个阶段，而不是只留下底层错误。

### Bootstrap Output

当脚本在空目录执行 bootstrap 路径时，输出必须按以下节奏组织：

1. 标题块：
   - `AI Efficiency Deployment Preparation`
2. 资产准备阶段：
   - 下载 deploy 资产
   - 准备根目录 `docker-compose.yml`
   - 准备 `.env.example`
   - 准备 `.env`
3. 安全与目录阶段：
   - 生成缺失 secrets
   - 创建 `data/`、`postgres_data/`、`redis_data/`
4. 摘要块：
   - 列出写入的文件
   - 列出创建的目录
   - 列出本次新生成的 secrets
   - 明确说明 secrets 已保存到 `.env`
   - 提示下一步 `docker compose up -d`

bootstrap 路径下允许打印本次新生成的 secret 明文值，因为这是首次准备目录，用户需要立即知晓并保管。

### Preflight Output

当脚本运行已有部署目录或仓库内 preflight 路径时，输出必须按以下节奏组织：

1. 标题块：
   - `AI Efficiency Deployment Preflight`
2. 运行上下文阶段：
   - 当前 layout
   - 当前 mode
   - 使用的 compose file
   - 使用的 env file
3. 校验阶段：
   - 加载或创建 `.env`
   - 生成缺失 secrets
   - 校验 compose 配置
   - 校验 relay health
   - external 模式下额外校验 Postgres / Redis TCP 可达性
4. 摘要块：
   - compose 实现
   - 通过的检查项
   - 本次新生成但未回显的 secrets 名称
   - 下一步启动命令

preflight 路径不得回显 secret 明文值。只允许提示“已生成并写入 `.env`”。

### Failure Contract

脚本保留 `set -euo pipefail` 和非零退出码语义，但用户可见错误输出必须满足：

- 先给一条阶段化错误，例如：
  - `Bootstrap failed during deploy asset download`
  - `Preflight failed during relay health check`
- 再保留底层错误详情

目标是让用户先理解“哪一步失败”，再看技术错误。

## Default Config Surface Contract

### Primary Operator Config

默认 `.env.example` 只应暴露主路径操作者通常需要理解或编辑的配置，主要包括：

- 服务入口：
  - `AE_SERVER_PORT`
  - `AE_SERVER_FRONTEND_URL`
- relay 连接：
  - `AE_RELAY_URL`
  - `AE_RELAY_API_KEY`
  - `AE_RELAY_ADMIN_API_KEY`
- bundled PostgreSQL / Redis 的基础凭据
- external 模式所需的 `AE_DB_DSN`、`AE_REDIS_ADDR`、`AE_REDIS_PASSWORD`、`AE_REDIS_DB`
- LDAP 配置
- 由脚本自动生成但仍属于操作者感知范围的安全项：
  - `AE_AUTH_JWT_SECRET`
  - `AE_ENCRYPTION_KEY`
  - `POSTGRES_PASSWORD`

### Hidden-By-Default Runtime Config

以下变量不应继续作为默认 `.env.example` 的主视图配置项：

- `AE_IMAGE_REPOSITORY`
- `AE_IMAGE_TAG`
- `AE_UPDATER_IMAGE_REPOSITORY`
- `AE_UPDATER_IMAGE_TAG`
- `AE_DEPLOYMENT_UPDATE_RELEASE_API_URL`
- `AE_UPDATER_ENV_FILE`
- `AE_UPDATER_SERVICE_NAME`
- `AE_UPDATER_PROJECT_NAME`

这些值应通过脚本默认值、Compose 默认值或内部约定提供，而不是要求默认操作者理解。

### Docker Image Default

为对齐 `sub2api` 当前 Docker 主线，默认 Docker 路径必须直接使用 `latest` 镜像：

- 应用镜像默认值：`ghcr.io/lichking-2234/ai-efficiency:latest`
- updater 镜像默认值：`ghcr.io/lichking-2234/ai-efficiency:latest`

bootstrap 路径不得再把 `AE_IMAGE_TAG` 或 `AE_UPDATER_IMAGE_TAG` 写入生成后的 `.env`。

### Advanced Overrides

高级用户仍可覆盖镜像仓库、镜像 tag 或 updater 相关变量，但这些覆盖能力应满足：

- 继续受支持
- 不出现在默认 `.env.example`
- 不作为 README 主路径叙事的一部分

允许的覆盖方式可以包括：

- 在 shell 环境中显式传入变量
- 由高级用户手工追加到 `.env`

但默认操作者不应被这些概念干扰。

## Compose And Script Defaults

为支撑更简洁的默认 `.env`，Compose 模板和脚本必须承担默认值提供责任：

- Compose 中的应用镜像与 updater 镜像必须有仓库与 tag 的默认回退值
- 脚本 preflight 不得再要求 image repository/tag 变量存在
- 脚本 preflight 不得再要求 updater 内部实现变量存在，除非该变量确实没有安全默认值
- `COMPOSE_PROJECT_NAME` 如无必要，不应继续作为默认路径下的强制必填项；若 updater 仍需要该值，应通过脚本或 Compose 默认值提供

如果现有 updater 行为仍依赖某些内部变量，优先选择把依赖收敛为内部默认值，而不是继续把变量暴露给操作者。

## Backward Compatibility

本次改动必须保持以下兼容性：

- 现有包含 `AE_IMAGE_TAG`、`AE_UPDATER_IMAGE_TAG` 等变量的 `.env` 文件继续可用
- 现有仓库内 `deploy/docker-deploy.sh` 调用方式继续可用
- bootstrap 后的根目录布局继续保持：
  - `docker-compose.yml`
  - `.env`
  - `.env.example`
  - `deploy/`
  - `data/`
  - `postgres_data/`
  - `redis_data/`
- external 模式入口仍为 `bash deploy/docker-deploy.sh external`

兼容性的重点是“去掉默认暴露”，不是“破坏已有覆盖能力”。

## Documentation Contract

实现本文后，至少需要同步更新：

- `deploy/README.md`
- `deploy/.env.example`
- `docs/architecture.md` 中与 Docker 主线描述直接冲突的部分（仅当实现后项目级描述发生变化时）

其中 README 主叙事必须改为：

1. 先讲 Docker 默认主线
2. 明确脚本会输出哪些阶段和摘要
3. 把高级覆盖与非主线模式放到后文

## Verification

实现本文时至少应覆盖以下验证：

1. bootstrap 测试验证：
   - 输出包含标题块、阶段化成功信息、摘要块和 next steps
   - `.env` 中不再自动写入 `AE_IMAGE_TAG` 与 `AE_UPDATER_IMAGE_TAG`
2. preflight 测试验证：
   - 输出包含 layout、mode、compose file、env file、检查摘要
   - 新生成 secrets 只显示变量名，不显示明文
3. compose 验证：
   - 在默认无 image tag 变量的情况下仍能成功 `config`
4. backward compatibility 验证：
   - 旧 `.env` 中显式设置 image tag 时仍能通过 preflight

## Rollout Notes

这是一份“当前生效合同”，但在实现前它仍代表目标改动而非当前现状。

在代码落地前，不应把以下内容写成已实现事实：

- Docker 默认路径已经改为 `latest`
- `.env.example` 已经移除了镜像和 updater 变量
- `docker-deploy.sh` 已经具备新的摘要式输出
