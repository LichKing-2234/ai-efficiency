# Unified Binary Self-Update Design

**Status:** Current contract for unifying Docker and non-Docker updates around backend-managed binary self-update

## Overview

本 spec 定义 `ai-efficiency` 后续统一的更新模型：

- Docker 模式去掉 updater sidecar
- 非 Docker 模式继续走后端二进制自更新
- 两条部署路径都由后端进程自己下载 release bundle、替换运行中的后端二进制，并在完成后触发重启

目标不是把所有部署方式压成一个“单一安装脚本”，而是把“如何检查更新、如何应用更新、如何回滚”的运行时语义统一起来，向 `sub2api` 的更新模型靠拢，去掉当前 Docker updater sidecar 带来的额外控制平面和 `.env` tag 状态机复杂度。

## Spec Relationship

- 本文更新 [`2026-04-08-production-deployment-packaging-design.md`](2026-04-08-production-deployment-packaging-design.md) 中关于 Docker 在线更新控制平面的合同。
- 本文替换 Docker 模式下“backend + updater sidecar + compose pull/up”这条更新路径。
- 本文继承 [`2026-04-09-binary-systemd-install-update-design.md`](2026-04-09-binary-systemd-install-update-design.md) 中已经存在的“二进制自更新”方向，但把它从 systemd 专属能力扩展成 Docker 与非 Docker 共享的统一模型。
- 本文不改变 [`2026-04-13-docker-deploy-ux-and-config-surface-design.md`](2026-04-13-docker-deploy-ux-and-config-surface-design.md) 中关于 Docker 默认配置面收敛、bootstrap/preflight 输出体验的合同；它只改变“更新机制如何落地”。
- 项目级运行时关系在实现落地后需要同步更新 [`docs/architecture.md`](../../architecture.md)。

## Problem Statement

截至 2026-04-13，`ai-efficiency` 的部署与更新模型存在一个明显分裂：

1. Docker 模式依赖 updater sidecar、`docker.sock`、Compose 控制路径和 `.env` 里的 image tag 状态。
2. 非 Docker 模式已经朝“下载 release bundle 并替换后端二进制”的方向演进。
3. 默认 Docker UX 已经收敛到“隐藏 image tag / updater 实现细节”，但 updater sidecar 仍然要求一部分 tag 状态显式存在，造成合同漂移。
4. 这套 Docker updater 控制平面相比 `sub2api` 更复杂，也更容易在 `.env`、Compose、updater 状态之间产生不一致。

当前需要解决的问题不是“再修一次 updater sidecar”，而是决定更新模型本身是否继续成立。

## Goals

1. 让 Docker 与非 Docker 模式共享同一套更新语义
2. 去掉 Docker updater sidecar 和 `docker.sock` 控制依赖
3. 让默认 Docker 路径不再维护 image tag 状态机
4. 保留“检查更新 / 应用更新 / 回滚 / 重启”这套产品能力
5. 让后端更新后端自身时同时完成前端静态资源版本切换
6. 尽量对齐 `sub2api` 当前“后端二进制自更新 + 重启”的模型

## Non-Goals

1. 不保留 Docker 模式下的 compose pull/up 在线更新链路
2. 不要求 Docker 镜像继续作为运行中后端版本的唯一真源
3. 不把 Docker 模式重新设计成 Kubernetes/Helm 方案
4. 不在本次合同里统一所有本地开发 compose 变体的实现细节
5. 不追求与 `sub2api` 的文件布局、脚本名、安装目录逐字一致

## Recommended Model

推荐采用统一的“后端二进制自更新”模型：

- 后端检查 GitHub latest release
- 后端下载与当前平台匹配的 backend bundle
- 后端校验 `checksums.txt`
- 后端原子替换当前运行二进制
- 后端保留 `.backup` 供 rollback 使用
- 前端继续通过已有 deployment UI 调用 update / rollback / restart API

这套模型在 Docker 与非 Docker 里共用同一个后端 update service，只在“运行二进制的实际路径”和“重启方式”上有环境差异。

## Runtime Layout

## Shared Principles

- 运行中的后端二进制必须位于一个可写、可原子重命名、可持久化的路径。
- 更新与回滚必须在同一文件系统内完成 rename，以保证替换原子性。
- `.backup` 文件必须与当前运行二进制位于同一目录。
- 前端资源继续随同后端二进制一起发布，因此替换后端二进制就等于替换前端 bundle。

## Docker Mode

Docker 模式不再运行 updater sidecar。官方运行时布局改为：

- 镜像内保留一个 launcher / entrypoint
- 镜像内保留一个 bootstrap backend binary
- 持久化 state dir 中保留真正运行的 backend binary

建议的持久化路径：

- `AE_DEPLOYMENT_STATE_DIR=/var/lib/ai-efficiency`
- 运行二进制路径：`${AE_DEPLOYMENT_STATE_DIR}/runtime/ai-efficiency-server`
- 回滚备份路径：`${AE_DEPLOYMENT_STATE_DIR}/runtime/ai-efficiency-server.backup`

Docker 容器启动时：

1. launcher 检查 runtime binary 是否存在
2. 若不存在，则从镜像内 bootstrap binary 复制到 runtime path
3. 若存在，则继续使用 runtime path 中已有二进制
4. launcher 最终 `exec` runtime path 中的 backend binary

为了避免“镜像升级但 runtime binary 永远不变”造成语义混乱，launcher 需要额外遵循一个比较规则：

- 如果 runtime binary 不存在：复制镜像内 bootstrap binary
- 如果 runtime binary 存在且其版本高于或等于镜像内 bootstrap binary：保留 runtime binary
- 如果镜像内 bootstrap binary 版本高于 runtime binary：允许用镜像内 bootstrap binary 覆盖 runtime binary
- 如果版本不可解析：默认保留已有 runtime binary，并在日志中提示

这样做的目的不是让镜像重新成为真源，而是让“手工更新镜像”和“运行时自更新”至少遵循一个可解释的版本比较规则。

## Non-Docker Mode

非 Docker 模式继续使用宿主安装目录里的运行二进制，例如：

- `/opt/ai-efficiency/ai-efficiency-server`
- `/opt/ai-efficiency/ai-efficiency-server.backup`

这里不再区分“systemd 专属 update service”和“Docker 专属 update service”；后端更新逻辑统一，只是 runtime path 来自不同部署模式。

## Update Flow

## Check Update

前端继续通过 deployment API 调用“检查更新”。

后端统一执行：

1. 读取当前运行二进制版本
2. 查询 GitHub `releases/latest`
3. 返回：
   - current version
   - latest version
   - has update
   - release info
   - build/deploy mode

Docker 与非 Docker 的差别只影响“当前版本从哪个运行二进制路径读取”，不影响 release 查询逻辑。

## Apply Update

`apply update` 的统一步骤如下：

1. 解析目标版本
2. 定位当前平台对应的 backend bundle
3. 下载 archive 与 `checksums.txt`
4. 校验 checksum
5. 解压出新的 backend binary 到临时文件
6. 将当前 runtime binary rename 为 `.backup`
7. 将新的 backend binary rename 到 runtime path
8. 返回 `need_restart: true`

Docker 模式下不再执行：

- 修改 `.env` image tag
- `docker compose pull`
- `docker compose up -d`

## Rollback

`rollback` 的统一步骤如下：

1. 检查 `.backup` 是否存在
2. 若存在，则把 `.backup` rename 回 runtime path
3. 返回 `need_restart: true`

回滚同样不再依赖 Docker image tag。

## Restart

前端保留“restart”按钮，但实现语义统一为“让当前后端进程退出并由宿主拉起”。

Docker 模式下：

- backend 进程退出
- Compose/service policy 负责把容器重新拉起
- launcher 再次从 runtime path 启动 backend binary

非 Docker 模式下：

- backend 进程退出
- systemd 或其它进程管理器负责把服务重新拉起

## Config Surface

这个统一模型落地后，Docker 主线应继续隐藏并进一步移除这些 updater sidecar 相关变量：

- `AE_UPDATER_IMAGE_REPOSITORY`
- `AE_UPDATER_IMAGE_TAG`
- `AE_UPDATER_ENV_FILE`
- `AE_UPDATER_SERVICE_NAME`
- `AE_UPDATER_PROJECT_NAME`
- `AE_DEPLOYMENT_UPDATE_UPDATER_URL`

更新模型保留的核心配置应仅包括：

- `AE_DEPLOYMENT_MODE`
- `AE_DEPLOYMENT_STATE_DIR`
- `AE_DEPLOYMENT_UPDATE_ENABLED`
- `AE_DEPLOYMENT_UPDATE_APPLY_ENABLED`
- `AE_DEPLOYMENT_UPDATE_RELEASE_API_URL`

默认路径下不要求 `.env` 显式包含 `AE_IMAGE_TAG`。  
如果操作者显式写了 `AE_IMAGE_TAG`，它只表示 Docker 启动时的镜像来源偏好，不再是在线更新状态机的主数据源。

## API Surface

前端与后端的 deployment API 可以保持现有接口形式：

- check updates
- apply update
- rollback
- restart

但返回值语义应更新为：

- `update` / `rollback` 表示 runtime binary 已替换
- `need_restart` 表示必须拉起新进程才能生效

Docker 模式与非 Docker 模式不再在 API 语义上分叉。

## Migration Strategy

### Existing Docker Deployments

旧的 Docker 部署目录可能仍包含：

- updater sidecar 服务
- `docker.sock` 挂载
- updater 相关 `.env` 变量

迁移到新模型时，需要：

1. 刷新 compose 资产
2. 删除 updater sidecar 服务定义
3. 删除 `docker.sock` 挂载
4. 保留并迁移 state dir
5. 初始化 `${AE_DEPLOYMENT_STATE_DIR}/runtime/ai-efficiency-server`

`docker-deploy.sh` 在检测到旧布局时，应明确提示当前目录仍是旧 Docker update model，需要刷新部署资产，而不是静默沿用旧逻辑。

### Existing Non-Docker Deployments

非 Docker 模式主要是服务重构，而不是模型切换。

迁移重点：

- 统一 update service 实现
- 统一 version detection
- 统一 rollback backup 语义

## Tradeoffs

### Benefits

- Docker 与非 Docker 共享同一套更新逻辑
- 默认 Docker UX 彻底摆脱 image tag 状态机
- 去掉 updater sidecar、`docker.sock` 和额外控制平面
- 更接近 `sub2api` 已经验证过的产品心智

### Costs

- Docker 运行时不再是“纯镜像不可变实例”
- 需要维护 launcher 与 runtime binary 的版本比较规则
- 运维需要理解“当前容器镜像版本”和“当前运行二进制版本”可能短期不同步

## Risks

1. Docker 容器重启后是否稳定继续使用 runtime path 中的二进制
2. 镜像升级与 runtime 自更新并存时，运维是否会混淆当前真正生效的版本
3. launcher 对版本比较失败时，是否会意外覆盖 runtime binary
4. 容器内文件权限、可执行权限与 volume 挂载路径是否稳定可写

## Mitigations

1. deployment status 中显式展示：
   - current running version
   - latest release version
   - deployment mode
   - runtime binary path
   - backup presence
2. launcher 对版本不可解析时默认保留 runtime binary，不做覆盖
3. bootstrap/preflight 明确检查 runtime state dir 是否可写
4. Docker 官方文档明确说明：
   - 镜像用于 bootstrap 与恢复
   - runtime binary 才是当前进程版本真源

## Documentation Contract

实现本文后，至少需要同步更新：

- `docs/architecture.md`
- `deploy/README.md`
- `docs/superpowers/specs/2026-04-08-production-deployment-packaging-design.md`
- `docs/superpowers/specs/2026-04-09-binary-systemd-install-update-design.md`

更新原则：

- `docs/architecture.md` 反映“当前实现”
- 老 spec 不全量重写，只在新的 spec 中说明被替换关系

## Verification

实现本文时至少需要覆盖以下验证：

1. Docker 模式：
   - 无 updater sidecar 仍可检查更新
   - apply update 能替换 runtime binary
   - restart 后新版本生效
   - rollback 后旧版本恢复
2. 非 Docker 模式：
   - apply update / rollback 继续有效
3. launcher：
   - runtime binary 缺失时可初始化
   - runtime binary 版本较新时不会被镜像内 bootstrap binary 覆盖
   - 镜像内 bootstrap binary 版本较新时可以覆盖 runtime binary
4. deployment UI：
   - Docker 与非 Docker 模式都能展示统一状态
   - update / rollback / restart 的用户反馈一致

## Rollout Notes

这是一份新的当前合同。  
在代码落地前，它仍然描述的是目标更新模型，而不是当前实现事实。

在实现完成之前，不应把以下内容写成现状：

- Docker 模式已移除 updater sidecar
- Docker 模式已改为 backend-managed binary self-update
- Docker 与非 Docker 已完全共用同一套 update service
