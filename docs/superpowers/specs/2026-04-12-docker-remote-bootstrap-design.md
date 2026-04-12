# Docker Remote Bootstrap Design

**Date:** 2026-04-12  
**Status:** Draft for Review  
**Scope:** `deploy/`, release assets, deployment UX  
**Related:**  
- [2026-04-08-production-deployment-packaging-design.md](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-04-08-production-deployment-packaging-design.md)  
- [2026-04-09-binary-systemd-install-update-design.md](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-04-09-binary-systemd-install-update-design.md)

项目级架构总览见 [`docs/architecture.md`](../../architecture.md)。

## Spec Relationship

- 本文是对 [`2026-04-08-production-deployment-packaging-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-04-08-production-deployment-packaging-design.md) 的部署入口补充与修正。
- `2026-04-08` 定义了 Docker Compose-first 的官方生产交付主线、现有 `deploy/docker-deploy.sh` 的 preflight 职责，以及 updater sidecar 边界。
- 本文不改变 Docker / Compose + updater sidecar 作为主线的结论，只补充一个更接近 `sub2api` 的“空目录远程 bootstrap”体验。
- 本文引入的能力以 **统一命名 `docker-deploy.sh`，但区分 bootstrap path 与 preflight path** 为核心。
- 历史 spec 保留其写作时的设计背景；当前生效的远程 Docker 部署入口合同，以本文为准。

## Overview

当前 `ai-efficiency` 的 Docker 部署体验，要求用户先获得完整仓库或完整 release bundle，再手动进入 `deploy/` 目录复制 `.env`、运行 `deploy/docker-deploy.sh`、最后再执行 `docker compose up -d`。

这和 `sub2api` 当前的用户心智不一致。

`sub2api` 的 `docker-deploy.sh` 实际承担的是：

1. 在空目录下载部署资产
2. 生成 `.env`
3. 创建本地持久化目录
4. 让用户直接执行 `docker compose up -d`

而 `ai-efficiency` 当前的 `deploy/docker-deploy.sh` 只承担：

1. 对已经存在的 `deploy/` 目录做 `.env` 补齐
2. 执行 preflight 检查
3. 校验 compose / relay / external infra 可用性

因此用户虽然看到同名脚本，但实际体验完全不同。

本文定义一个统一入口，使 `ai-efficiency` 在 Docker 方式下也能做到接近 `sub2api` 的体验：

```bash
mkdir -p ai-efficiency-deploy && cd ai-efficiency-deploy
curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/deploy/docker-deploy.sh | TAG=v0.1.0-preview.2 bash
docker compose up -d
```

## Goals

1. 让 `ai-efficiency` 支持在空目录通过远程脚本准备 Docker 部署目录
2. 保持 `docker-deploy.sh` 这个统一命名，不引入平行的 `docker-bootstrap.sh`
3. 默认对齐 `sub2api` 的 bundled + local-directory 持久化体验
4. 让远程 bootstrap 基于 GitHub Release 版本，而不是直接依赖 `main` 分支 deploy 文件
5. 保持现有 updater sidecar、生产 compose、external compose 的边界不变
6. 让本地已有部署目录的场景仍能复用现有 preflight 能力

## Non-Goals

1. 第一版不支持远程脚本自动执行 `docker compose up -d`
2. 第一版不支持 remote bootstrap 直接准备 external 模式
3. 第一版不移除现有 `deploy/docker-compose.yml` 与 `deploy/docker-compose.external.yml`
4. 第一版不要求把 updater sidecar 改成不依赖本地 deploy 目录
5. 第一版不追求“单文件脚本内嵌全部 compose 模板”的实现

## Current State

当前仓库中：

- [`deploy/docker-deploy.sh`](/Users/admin/ai-efficiency/deploy/docker-deploy.sh) 假设脚本位于完整仓库或完整 release bundle 的 `deploy/` 目录内。
- 它会按脚本所在目录推导 `ROOT_DIR`，再读取：
  - `deploy/.env.example`
  - `deploy/docker-compose.yml`
  - `deploy/docker-compose.external.yml`
- [`deploy/docker-compose.yml`](/Users/admin/ai-efficiency/deploy/docker-compose.yml) 与 [`deploy/docker-compose.external.yml`](/Users/admin/ai-efficiency/deploy/docker-compose.external.yml) 都把当前目录挂载到 updater sidecar 的 `/work/deploy`。
- 因此，当前实现要求“部署目录本身”成为运行时的一部分，而不是仅仅在启动前短暂存在。

这意味着：

1. 不能简单把 `deploy/docker-deploy.sh` 通过 `curl | bash` 远程执行后就结束，因为脚本执行时缺少本地 deploy 资产。
2. 也不能只下载镜像 tag 而不落地 deploy 文件，因为 updater sidecar 需要这些本地文件。

## Proposed Design

### Unified Script, Two Paths

`deploy/docker-deploy.sh` 继续保留，但明确承担两条路径：

1. **bootstrap path**
2. **preflight path**

两条路径共享同一个脚本名，但适用上下文不同。

### Bootstrap Path

bootstrap path 适用于：

- 用户在空目录中通过 `curl ... | bash` 远程执行
- 当前目录不存在完整 deploy 资产
- 用户希望脚本把部署目录准备好，但不自动启动服务

bootstrap path 的行为：

1. 解析目标版本
   - 默认：latest stable release
   - 可通过 `TAG=vX.Y.Z` 或 `TAG=vX.Y.Z-preview.N` 显式指定
2. 检测平台
   - 第一版只支持 Linux
   - 架构只支持 `amd64` / `arm64`
3. 下载对应 release 的 backend bundle
4. 校验 `checksums.txt`
5. 解包出 `deploy/` 资产到当前目录
6. 生成根目录用户入口文件：
   - `docker-compose.yml`
   - `.env.example`
   - `.env`
7. 创建本地持久化目录：
   - `data/`
   - `postgres_data/`
   - `redis_data/`
8. 将 `.env` 中的镜像 tag 固定到目标版本
9. 自动生成必要密钥
10. 输出后续启动命令，不自动执行 Compose

### Preflight Path

preflight path 适用于：

- 已经存在完整部署目录
- 用户在本地执行 `./deploy/docker-deploy.sh`
- 用户希望补齐 `.env`、生成缺失密钥、执行配置检查和外部依赖检查

preflight path 的行为保持当前职责：

1. 复制或读取 `deploy/.env`
2. 生成缺失密钥
3. 校验 compose 配置
4. 校验 relay 健康
5. external 模式下校验 DB / Redis 连通性
6. 输出 “preflight ok”

bootstrap path 不自动执行 full preflight。

原因：

- 对齐 `sub2api` 的用户体验
- 避免用户在还没填 relay 配置前就被 preflight 阻断
- 允许用户先完成目录准备，再手工编辑 `.env`

## Directory Layout After Bootstrap

bootstrap 完成后，当前目录结构应类似：

```text
ai-efficiency-deploy/
├── docker-compose.yml
├── .env
├── .env.example
├── deploy/
│   ├── docker-compose.yml
│   ├── docker-compose.external.yml
│   ├── docker-compose.bootstrap.yml
│   ├── .env.example
│   ├── docker-deploy.sh
│   ├── init-db.sql
│   └── ...
├── data/
├── postgres_data/
└── redis_data/
```

其中：

- 根目录 `docker-compose.yml` 是给用户直接运行的默认入口
- `deploy/` 下保留完整官方资产，供 updater sidecar 和排障使用
- 根目录 `.env` 是用户直接编辑的主配置文件

## Bootstrap Compose Flavor

为了真正对齐 `sub2api` 的“目录可打包迁移”体验，bootstrap path 不应直接把当前的生产 `deploy/docker-compose.yml` 原样拷到根目录。

因为当前生产 compose 使用 named volumes，而 `sub2api` 的一键部署默认是 local-directory 持久化。

因此本文定义新增一个专用模板：

- [`deploy/docker-compose.bootstrap.yml`](/Users/admin/ai-efficiency/deploy/docker-compose.bootstrap.yml)

该模板的职责：

1. 语义上仍属于 bundled 模式
2. 运行服务仍为：
   - `backend`
   - `postgres`
   - `redis`
   - `updater`
3. 但数据持久化改为本地目录：
   - `./data`
   - `./postgres_data`
   - `./redis_data`
4. updater sidecar 继续挂载当前目录和 Docker socket，保持在线升级能力

这个模板只服务于 bootstrap UX，不替代现有生产 compose 的角色。

## Version Resolution

第一版版本解析规则：

1. 若设置 `TAG`，严格按该 tag 下载
2. 若未设置 `TAG`，只解析 latest stable release
3. latest prerelease 不作为默认目标

这样既满足 stable 默认路径，也允许显式安装 preview 版本。

## Error Handling

### Directory Safety

bootstrap path 在当前目录已存在以下任意项时，默认失败：

- `docker-compose.yml`
- `.env`
- `deploy/`

原因：

- 远程 bootstrap 不应猜测是否覆盖已有部署
- 避免把用户已有目录混成半成品状态

### Missing Assets

若 release bundle 中缺少以下关键资产，直接失败：

- `deploy/.env.example`
- `deploy/docker-deploy.sh`
- `deploy/docker-compose.bootstrap.yml`

原因：

- 这意味着 release 交付不完整

### Unsupported Platform

第一版在以下情况下直接失败：

- 非 Linux
- 非 `amd64` / `arm64`

### Invalid Tag

当 `TAG` 指向不存在的 release 或缺少目标平台 bundle 时，错误信息必须包含：

- 请求的 tag
- 请求的平台
- 失败的下载对象

## File Changes

### New Files

- [`deploy/docker-compose.bootstrap.yml`](/Users/admin/ai-efficiency/deploy/docker-compose.bootstrap.yml)
  Bootstrap UX 的默认 root compose 模板，使用本地目录持久化。

### Modified Files

- [`deploy/docker-deploy.sh`](/Users/admin/ai-efficiency/deploy/docker-deploy.sh)
  扩展为同时支持 bootstrap path 和 preflight path。
- [`deploy/README.md`](/Users/admin/ai-efficiency/deploy/README.md)
  新增空目录远程 bootstrap 的官方文档入口。

## Release Asset Requirement

为了支持 bootstrap path，GitHub Release backend bundle 必须继续包含完整 `deploy/` 资产，并新增：

- `deploy/docker-compose.bootstrap.yml`

这与当前 bundle 中已包含 `deploy/README.md`、`deploy/.env.example`、`deploy/docker-compose.yml`、`deploy/docker-compose.external.yml` 的模式一致。

## Testing

至少需要覆盖：

1. `bash -n deploy/docker-deploy.sh`
2. `docker compose --env-file deploy/.env.example -f deploy/docker-compose.yml config`
3. `docker compose --env-file deploy/.env.example -f deploy/docker-compose.external.yml config`
4. `docker compose --env-file deploy/.env.example -f deploy/docker-compose.bootstrap.yml config`
5. 针对 bootstrap path 的脚本测试：
   - 指定 tag 时能准备根目录 `docker-compose.yml`
   - 目标目录非空时拒绝覆盖
   - 根目录 `.env` 中的镜像 tag 被正确写入
   - 根目录 `data/`、`postgres_data/`、`redis_data/` 被创建

## Acceptance Criteria

以下条件同时满足，视为本文目标达成：

1. 用户可在空目录执行远程 `docker-deploy.sh` 完成部署目录准备
2. 用户可显式指定 preview tag
3. 准备后的目录可直接执行 `docker compose up -d`
4. 当前 updater sidecar 机制不被移除
5. 本地已有部署目录仍可执行 `./deploy/docker-deploy.sh` 做 preflight
6. 现有 bundled / external 生产 compose 语义不被破坏
