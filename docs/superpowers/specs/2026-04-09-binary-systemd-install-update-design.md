# Binary Systemd Install And Update Design

**Status:** Current contract for Linux binary / systemd installation and self-update

> Relationship note (2026-04-13): The binary self-update model described here is now the baseline for both Docker and non-Docker runtime modes; this spec remains the historical design entry for the non-Docker path.

## Overview

本文定义 `ai-efficiency` 的 **binary + systemd** 交付与升级路线。

目标不是替换当前已经建立的 Docker Compose 主线，而是在其旁边补上一条与 `sub2api` 更接近的二进制安装路径：

- Docker / Compose 路线继续使用 **updater sidecar + image/tag update**
- binary / systemd 路线使用 **GitHub Release 二进制包 + 原子替换 + backup/rollback + systemctl restart**

这样可以让两种部署方式都遵循各自更自然的运行时模型，而不是强行用一套升级机制覆盖所有环境。

## Spec Relationship

- 本文是 [`2026-04-08-production-deployment-packaging-design.md`](./2026-04-08-production-deployment-packaging-design.md) 的后续补充合同。
- `2026-04-08` 负责定义项目级生产部署主线、Compose 模式、部署脚本、在线更新目标能力。
- 本文只补充 **binary/systemd** 路线，不改变 `2026-04-08` 中已确认的 Docker / Compose 主线。
- 对 Docker 路线，仍以 image/tag 驱动和 updater sidecar 为主。
- 对 binary/systemd 路线，本文引入 `sub2api` 风格的 self-update / rollback / restart。

## Scope

本文覆盖：

1. binary/systemd 安装目录与文件布局
2. `deploy/install.sh` 的职责与生命周期命令
3. `deploy/ai-efficiency.service` 的运行模型
4. binary 模式下的更新、回滚、重启链路
5. 后端 deployment/update API 在 `compose` 与 `systemd` 模式下的分流
6. 发布产物对 binary/systemd 路线的要求

本文不覆盖：

- Docker Compose 路线的 updater sidecar 细节
- GitHub Actions / GoReleaser 的完整实现细节
- Kubernetes / Helm
- Windows Service 或 macOS Launchd

## Current State

截至 2026-04-12，当前代码已经具备本文定义的大部分 systemd 路线能力：

- `deploy/install.sh` 与 `deploy/ai-efficiency.service` 已存在并随 release bundle 分发。
- backend 已具备 GitHub Release 查询、systemd 资产选择、binary 下载 / checksum / 原子替换 / `.backup` 回滚逻辑。
- deployment 服务已经按 `compose` 与 `systemd` 模式分流，并提供 check / apply / rollback / restart 能力。
- 前端 deployment 设置页已经对 `systemd` 模式暴露 restart 与 mode-aware 提示。

因此本文不再只是“新增合同”，而是当前已落地 systemd 路线的设计与边界说明。具体运行时行为仍以当前代码和 [`docs/architecture.md`](/Users/admin/ai-efficiency/docs/architecture.md) 为准。

## Goals

1. 让 `ai-efficiency` 具备与 `sub2api` 类似的 binary/systemd 安装与升级体验
2. 保持 Docker 路线和 binary 路线分工清晰
3. 保证 binary 路线支持：
   - 检测更新
   - 下载 release
   - checksum 校验
   - 原子替换
   - `.backup` 回滚
   - service restart
4. 不破坏当前 Compose 路线和 updater sidecar 的职责边界

## Non-Goals

1. 不让 Docker 路线改成容器内自替换二进制
2. 不让 systemd 路线依赖 updater sidecar
3. 不支持非-Linux 的 service manager
4. 不在 v1 同时实现完整安装器 UI 或交互式 setup wizard

## Deployment Mode Split

### Compose Mode

`compose` 模式继续保持当前方向：

- 运行入口：Docker Compose
- 升级对象：image/tag
- 执行者：updater sidecar
- 前端触发后，backend 调 updater sidecar，sidecar 执行 compose 变更

### Systemd Mode

`systemd` 模式新增如下能力：

- 运行入口：GitHub Release 二进制包 + systemd unit
- 升级对象：本机安装的 `ai-efficiency-server` 二进制
- 执行者：backend 内部 binary update service
- 前端触发后，backend 直接下载、校验、替换、回滚、重启

### Why Split

这是一个明确的运行时边界选择：

- Docker 路线更适合 **镜像替换**
- binary/systemd 路线更适合 **二进制自更新**

两条路径共用同一组管理界面和大部分 deployment API，但不共用同一套底层升级机制。

## Binary Installation Layout

### Install Directories

建议固定为：

- 安装目录：`/opt/ai-efficiency`
- 配置目录：`/etc/ai-efficiency`
- 数据目录：`/var/lib/ai-efficiency`
- systemd unit：`/etc/systemd/system/ai-efficiency.service`

### Installed Files

至少包括：

- `/opt/ai-efficiency/ai-efficiency-server`
- `/opt/ai-efficiency/ai-efficiency-server.backup`（存在时）
- `/opt/ai-efficiency/deploy/` 下的辅助发布文件（可选但推荐）
- `/etc/ai-efficiency/ai-efficiency.env` 或 `/etc/ai-efficiency/config.yaml`

### System User

建议使用专用系统用户：

- user: `ai-efficiency`
- group: `ai-efficiency`

该用户只负责运行服务，不承担交互式登录用途。

## Install Script

### File

新增：

- `deploy/install.sh`

### Responsibilities

脚本负责：

1. 检测 OS / arch
2. 获取 latest release 或指定版本
3. 下载对应 release 资产
4. 下载并校验 `checksums.txt`
5. 解压并安装二进制到 `/opt/ai-efficiency`
6. 创建系统用户和目录
7. 写入 / 安装 `ai-efficiency.service`
8. `systemctl daemon-reload`
9. `systemctl enable ai-efficiency`
10. 可选启动服务

### Supported Commands

建议支持这些命令风格：

- 默认：安装 latest
- `install`
- `install -v <tag>`
- `upgrade`
- `rollback <tag>` 或 `install -v <tag>` 作为回退
- `uninstall`
- `list-versions`

### Release Source

`install.sh` 的下载源是 GitHub Releases，而不是本地构建目录。

这点必须与当前 GitHub 主仓库 / release automation 对齐。

## Systemd Unit

### File

新增：

- `deploy/ai-efficiency.service`

### Core Shape

建议保持与 `sub2api` 同类结构：

```ini
[Unit]
Description=AI Efficiency Platform
After=network.target

[Service]
Type=simple
User=ai-efficiency
Group=ai-efficiency
WorkingDirectory=/opt/ai-efficiency
ExecStart=/opt/ai-efficiency/ai-efficiency-server
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=ai-efficiency
EnvironmentFile=/etc/ai-efficiency/ai-efficiency.env

[Install]
WantedBy=multi-user.target
```

### Hardening

可加入最小安全项：

- `NoNewPrivileges=true`
- `PrivateTmp=true`
- `ProtectHome=true`
- `ReadWritePaths=/opt/ai-efficiency /var/lib/ai-efficiency`

## Binary Update Flow

### Check Update

前端继续调用当前 deployment check/update API。

在 `systemd` 模式下，backend：

1. 查询 GitHub latest release
2. 对比当前版本与 latest release tag
3. 返回：
   - current version
   - latest version
   - has update
   - release metadata

### Apply Update

在 `systemd` 模式下，backend 执行：

1. 选择当前平台对应 release 资产
2. 下载压缩包
3. 下载 `checksums.txt`
4. 校验 checksum
5. 解压出 `ai-efficiency-server`
6. 将当前可执行文件重命名为 `.backup`
7. 原子替换新二进制
8. 返回：
   - update completed
   - need restart = true

### Rollback

在 `systemd` 模式下，backend 执行：

1. 检查 `.backup` 是否存在
2. 用 `.backup` 替换当前二进制
3. 返回：
   - rollback completed
   - need restart = true

### Restart

在 `systemd` 模式下，backend 执行：

- `systemctl restart ai-efficiency`

建议异步触发，避免在 HTTP 请求尚未返回前就把自己杀掉。

## Deployment API Routing

deployment/update 相关 API 保持统一，但内部按 `deployment.mode` 分流：

### `deployment.mode=compose`

- `Status` / `CheckForUpdate`: 走当前 Compose 路线
- `ApplyUpdate` / `RollbackUpdate`: 调 updater sidecar

### `deployment.mode=systemd`

- `Status` / `CheckForUpdate`: 走 GitHub Release 查询
- `ApplyUpdate` / `RollbackUpdate`: 走 binary update service
- `Restart`: 走 systemd restart

## Release Artifact Requirements

为支持 `install.sh`，GitHub Release 需要提供：

- Linux `amd64` backend bundle
- Linux `arm64` backend bundle
- `checksums.txt`

其中 backend bundle 至少包含：

- `ai-efficiency-server`
- `deploy/README.md`
- `deploy/config.example.yaml`
- `deploy/install.sh`
- `deploy/ai-efficiency.service`

`ae-cli` release 仍独立发布，但不属于 systemd 安装的核心输入。

## Failure Semantics

### Download / Checksum Failure

- 不替换当前二进制
- 直接返回失败

### Replace Failure

- 尝试恢复 `.backup`
- 若恢复失败，明确上报为高优先级运维故障

### Restart Failure

- 更新已完成但 service 未成功重启
- 前端应收到明确错误，而不是误报升级完全成功

## Acceptance Criteria

当以下条件满足时，可以认为本文所述合同被实现：

1. `deploy/install.sh` 可从 GitHub Release 安装 latest 或指定版本
2. `deploy/ai-efficiency.service` 可被 systemd 正常加载
3. Linux systemd 环境下可完成安装、启动、enable
4. backend 在 `systemd` 模式下支持 check / update / rollback / restart
5. binary 更新使用 checksum + 原子替换 + `.backup`
6. Docker 路线继续走 updater sidecar，不被 binary 路线污染

## Rollout Order

建议顺序：

1. 先补 release 资产对 systemd 路线的支持
2. 再补 `install.sh` + service file
3. 再补 backend 的 binary self-update service
4. 最后把 deployment mode 分流接到前端现有管理界面
