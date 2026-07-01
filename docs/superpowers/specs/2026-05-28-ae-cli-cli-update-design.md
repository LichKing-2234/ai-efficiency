# ae-cli CLI Update Design

**Status:** Current contract for `ae-cli update check` and `ae-cli update install`

## Overview

本文补上 `ae-cli` 的 CLI 内检查更新与升级入口，但保持它继续复用现有 GitHub Release + installer 分发合同，而不是引入第二套发布/升级机制：

- `ae-cli update check` 查询 GitHub Releases latest 元数据并比较当前 CLI 版本
- `ae-cli update install` 只针对官方用户级安装路径 `~/.local/bin/ae-cli`
- 升级动作复用目标 tag 对应的官方 installer，而不是在 CLI 内复制一份 checksum / 解压 / 配置保留逻辑
- 升级后继续沿用 installer 已有的 managed hook refresh 行为
- 非官方安装路径不做“猜测式覆盖”，而是明确要求用户回到原始安装方式

## Spec Relationship

- 本文继承 [`2026-04-13-ae-cli-user-install-design.md`](./2026-04-13-ae-cli-user-install-design.md) 的 release 分发合同，但覆盖其中“`ae-cli self-update` 不在范围内”的旧约束。
- 本文不改变 `ae-cli/install.sh` / `install.ps1` 的 installer contract；CLI update 只是改为从命令内触发同一条安装链路。
- 本文继承 [`2026-05-23-global-git-hooks-design.md`](./2026-05-23-global-git-hooks-design.md) 中“installer 或 upgrade flow 必须刷新 AE-managed hook scripts”的要求。
- 项目级模块边界不变；`ae-cli` 仍是独立 CLI 分发面，`deploy/install.sh` 仍只负责 backend/systemd。

## Problem Statement

截至 2026-05-28，`ae-cli` 已有：

1. GitHub Release 归档产物
2. Bash / PowerShell 一键安装脚本
3. README 中的远程安装入口

但缺少 CLI 内自助检查更新和升级入口，导致：

1. 用户只能重新记忆 shell / PowerShell 安装命令
2. 现有 installer 的 checksum、配置保留、hook refresh 合同无法通过 CLI 直接复用
3. 旧 spec 明确把 self-update 排除在范围外，和当前产品预期不一致

## Goals

1. 提供 `ae-cli update check`，在不依赖 backend config/login 的情况下检查最新 GitHub Release
2. 提供 `ae-cli update install`，让官方安装路径用户可以直接升级到最新发布版
3. 复用既有 installer 合同，避免在 Go 代码内再复制 checksum / 配置迁移 / hook refresh 逻辑
4. 对非官方安装路径给出明确、可执行的回退指引

## Non-Goals

1. 不做后台自动检查更新
2. 不对 Homebrew、`go install`、自定义路径、手工复制二进制等安装方式做“通用自升级”
3. 不改造 backend deployment/update 模型
4. 不在本次合同内实现 Windows 进程内自替换

## Current Contract

### Command Surface

- `ae-cli update check`
- `ae-cli update install`
- `ae-cli update upgrade` 作为 `install` 的兼容别名

这三个命令都不要求预先加载 CLI config、backend URL、token 或登录态刷新逻辑。

### Version Source

- 当前版本来自 `ae-cli/internal/buildinfo.Version`
- 最新版本来自 GitHub API `releases/latest`
- 版本比较按 semver 处理，包含 prerelease 比较

状态语义：

- `update_available`
- `up_to_date`
- `current_newer`
- `updated`

### Install Eligibility Boundary

`ae-cli update install` 只支持官方管理路径：

- Unix/macOS: `~/.local/bin/ae-cli`
- Windows: `%USERPROFILE%\.local\bin\ae-cli.exe`

如果当前运行中的可执行文件不在该路径，CLI 必须失败并提示用户回到官方 installer，而不是尝试覆盖一个来源不明的安装。

这样可以避免：

- 覆盖 `go install`、包管理器或手工分发的二进制
- 当前 PATH 指向其它路径时出现“升级成功但下次运行不是新版本”的假象

### Upgrade Execution Contract

对于受支持的官方安装路径：

1. `update install` 先做一次 `update check`
2. 如果已是最新版本且没有 `--force`，直接返回 `up_to_date`
3. 如果需要升级，则下载目标 tag 对应的 installer 脚本：
   - Unix/macOS: `https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/<tag>/ae-cli/install.sh`
   - Windows: `https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/<tag>/ae-cli/install.ps1`
4. 运行该 installer，并把目标 tag 作为参数传入
5. installer 继续负责：
   - release checksum 校验
   - 二进制落盘
   - 现有 config 保留 / `server.url` 更新规则
   - managed hook refresh

CLI update 本身不重新实现这些行为，只负责：

- 版本判断
- 官方路径约束
- 拉取目标 tag installer
- 调起 installer

### Windows Contract

当前合同下：

- `ae-cli update check` 支持 Windows
- `ae-cli update install` 在 Windows 返回明确诊断，要求用户重新运行 `ae-cli/install.ps1`

原因是当前发布形态直接覆盖 `ae-cli.exe`，而运行中的 Windows 可执行文件不能安全地被同一进程同步替换。后续如果引入 detached helper / staged replace，再用新 spec 扩展。

## Testing Contract

最小验证面包括：

1. `update check` 能正确报告 `update_available` / `up_to_date` / `current_newer`
2. `update install` 在已是最新版本时不会要求解析当前 executable path
3. `update install` 对非官方路径返回明确错误
4. `update install` 在官方 Unix/macOS 路径上会执行目标 tag installer，并把 tag 作为安装目标参数传入
5. `update` 子命令不会触发 CLI config/login 预加载
