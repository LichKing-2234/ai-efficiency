# ae-cli User Install Design

**Status:** Current contract for user-level `ae-cli` installation via GitHub Releases

## Overview

本文定义 `ae-cli` 的一键安装路径，目标是补上一条类似 `deploy/install.sh` 的远程安装入口，但保持 `ae-cli` 与后端/systemd 部署路径解耦：

- `ae-cli` 通过 GitHub Releases 的独立归档发布
- 用户通过远程 shell 脚本安装到 `~/.local/bin/ae-cli`
- 安装脚本负责平台识别、版本解析、checksum 校验与落盘
- 首装时安装脚本负责引导 backend URL，并写入 `~/.ae-cli/config.yaml`
- 安装脚本不修改 shell profile，只在 `PATH` 缺失时给出明确提示

本文解决的是“开发者如何快速拿到 CLI”，不是“如何管理后端服务部署”。

## Spec Relationship

- 本文补充当前仓库中 `ae-cli` 仅有 release 归档、缺少官方一键安装入口的空白。
- 本文不改变 [`2026-04-09-binary-systemd-install-update-design.md`](./2026-04-09-binary-systemd-install-update-design.md) 中 backend/systemd 路线；该文已明确 `ae-cli` release 独立发布，但不属于 systemd 安装核心输入。
- 本文不改变 [`2026-04-13-unified-binary-self-update-design.md`](./2026-04-13-unified-binary-self-update-design.md) 中 backend deployment/update 的统一更新模型；`ae-cli` 仍是独立客户端分发面。
- 项目级模块边界仍以 [`docs/architecture.md`](../../architecture.md) 为总览：`ae-cli` 是独立 CLI，不与 deploy runtime 混成同一安装器。

## Problem Statement

截至 2026-04-13，仓库已经具备 `ae-cli` 的跨平台 release 归档产物，但缺少面向最终用户的安装入口：

1. release 能产出 `ae-cli_<version>_<os>_<arch>.tar.gz` 归档，但用户仍需手动下载、解压、拷贝到 PATH。
2. 仓库已有 backend 的 `deploy/install.sh`，但 `ae-cli` 没有对等的一键安装体验，产品表面不一致。
3. `ae-cli` 的主要使用场景是开发者本地机器，更适合用户级安装路径，而不是 root/systemd 级安装模型。

当前需要补的是“官方推荐安装路径”，而不是再发明一套 CLI 包管理系统。

## Goals

1. 提供官方远程安装命令，使用户可直接安装 `ae-cli`
2. 默认安装到用户级目录 `~/.local/bin`，不要求 `sudo`
3. 复用 GitHub Releases 与 `checksums.txt`，不引入第二套分发格式
4. 在 macOS / Linux 上提供一致体验
5. 保持失败语义清晰，避免半安装状态

## Non-Goals

1. 不实现 `ae-cli install`、`ae-cli self-update`、`ae-cli uninstall` 子命令
2. 不自动修改 `~/.zshrc`、`~/.bashrc` 或其它 shell profile
3. 不支持 Windows PowerShell 安装脚本
4. 不把 backend 与 CLI 合并到同一个安装脚本
5. 不引入 Homebrew/tap、apt、yum 等包管理器集成作为本合同的一部分

## Current State

当前代码已具备以下前提：

- `.goreleaser.yaml` 已生成 `ae-cli` 独立 build 与 archive：
  - binary: `ae-cli`
  - archive: `ae-cli_<version>_<os>_<arch>.tar.gz`
- release 已生成全局 `checksums.txt`
- `deploy/install.sh` 已作为 backend/systemd 路线的远程安装入口存在
- `ae-cli/install.sh` 已提供远程安装入口
- `ae-cli/README.md` 已提供安装文档入口
- `ae-cli/test/install-test.sh` 已覆盖安装成功/失败与配置写入场景

因此本文描述的是当前已落地合同，而不是待实现草案。

## Design

## Installation Entry Point

### Official Command

官方推荐安装命令为：

```bash
curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | bash
```

安装指定版本：

```bash
curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | bash -s -- v0.2.0
```

脚本接受的主输入只有一个可选参数：

- `tag`

无参数时安装 latest release；有参数时安装指定 tag。

非交互场景可通过环境变量预置 backend URL：

```bash
AE_CLI_INSTALL_SERVER_URL=https://ae.example.com \
curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | bash
```

### Why A Separate Script

`ae-cli` 的安装目标用户、权限模型和运行环境与 backend/systemd 完全不同：

- backend installer 面向服务部署
- `ae-cli` installer 面向开发者工作站

因此必须使用独立入口，而不是扩展 `deploy/install.sh` 去同时承担两种职责。

## Install Location Contract

默认安装路径固定为：

- `~/.local/bin/ae-cli`

脚本必须：

- 自动创建 `~/.local/bin`
- 将二进制安装为 `~/.local/bin/ae-cli`
- 赋予可执行权限

本文不要求支持通过环境变量或 flags 自定义安装目录。先把默认主路径做稳定。

## Platform Detection Contract

### Supported OS

脚本只支持：

- `linux`
- `darwin`

遇到其它 `uname -s` 结果时，必须直接退出并提示当前脚本不支持该平台。

### Supported Arch

脚本至少支持：

- `x86_64` -> `amd64`
- `arm64` / `aarch64` -> `arm64`

遇到不支持的架构时，必须直接退出并提示不支持。

### Windows

Windows 不走本 bash 安装路径。

脚本在 Windows-like 环境或不兼容 shell 中无需做兼容实现；文档只需提示用户使用 release 手动安装。

## Release Resolution Contract

### Release Source

安装源固定为 GitHub Releases，不从源码构建，不依赖本地 Go 工具链。

默认 latest 查询：

- `https://api.github.com/repos/LichKing-2234/ai-efficiency/releases/latest`

指定版本下载基址：

- `https://github.com/LichKing-2234/ai-efficiency/releases/download/<tag>`

### Asset Names

脚本必须按当前 GoReleaser 命名约定解析资产：

- archive: `ae-cli_<version>_<os>_<arch>.tar.gz`
- checksum: `checksums.txt`

其中：

- `<version>` 为去掉 `v` 前缀后的 tag 版本号
- `<os>` / `<arch>` 为脚本标准化后的平台值

如果目标 release 中不存在对应 archive 或不存在 `checksums.txt` 条目，脚本必须失败退出。

## Checksum Verification Contract

安装流程必须做 release checksum 校验：

1. 下载目标 archive
2. 下载 `checksums.txt`
3. 从 `checksums.txt` 中查找 archive 对应 sha256
4. 计算本地 archive sha256
5. 对比结果

校验失败时：

- 不得写入 `~/.local/bin/ae-cli`
- 必须输出明确错误，例如 `checksum verification failed`

这样保证 `ae-cli` 与 backend installer 一样遵循 release integrity contract。

## Install Flow Contract

成功路径固定为：

1. 解析平台
2. 解析目标 tag
3. 下载 archive 和 `checksums.txt` 到临时目录
4. 校验 checksum
5. 解压 archive
6. 确认归档内包含 `ae-cli` 可执行文件
7. 创建 `~/.local/bin`
8. 将 `ae-cli` 拷贝到 `~/.local/bin/ae-cli`
9. `chmod 0755`
10. 如果本地不存在 CLI 配置，则尝试获取 backend URL 并写入 `~/.ae-cli/config.yaml`
11. 输出安装结果摘要

安装脚本允许覆盖已存在的 `~/.local/bin/ae-cli`。这视为重装/升级，不额外保留 backup 文件。

## CLI Config Bootstrap Contract

安装脚本除了安装 binary，还负责首装时的最小 CLI 配置引导。

### Config Path

安装脚本写入：

- `~/.ae-cli/config.yaml`

CLI 运行时同时兼容读取：

- `~/.ae-cli/config.yaml`
- `~/.ae-cli/config.yml`

### Backend URL Bootstrap

如果本地尚不存在 CLI config：

1. 先读取 `AE_CLI_INSTALL_SERVER_URL`
2. 若未设置且当前安装是交互式终端，提示用户输入 backend URL
3. 若拿到非空 URL，则写入 `server.url`
4. 若用户留空或当前环境非交互，则跳过写入并打印后续配置提示

如果本地已存在 `config.yaml` 或 `config.yml`：

- 安装脚本不得覆盖
- 只打印 “using existing config” 类提示

### Written Config Shape

最小写入内容为：

```yaml
server:
  url: "https://ae.example.com"
```

安装脚本不负责写入 token，也不负责生成 `tools` 列表。

### Tool Availability

当前 CLI 实现在 `tools` 字段缺失时，会从本地 `PATH` 自动探测常见工具（`claude`、`codex`、`kiro`）。因此安装脚本不再需要为了“让 CLI 有工具可用”而生成冗长的 `tools` 配置块。

## PATH Handling Contract

脚本必须检查 `~/.local/bin` 是否已出现在当前 `PATH`。

如果已存在：

- 成功摘要中正常打印安装路径即可

如果不存在：

- 仍然视为安装成功
- 额外打印明确提示，说明当前 shell 可能找不到 `ae-cli`
- 提供手动处理建议，例如把 `~/.local/bin` 加入 shell profile 或当前 shell 的 `PATH`

脚本不得：

- 自动修改 `~/.zshrc`
- 自动修改 `~/.bashrc`
- 自动创建任何 “managed PATH block”

## Output Contract

脚本输出应保持简洁、可扫读，至少包括：

- 正在安装哪个 tag
- 若写入或复用 CLI config，打印对应结果
- 安装到哪个路径
- 安装完成提示
- 若 PATH 缺失，则打印单独 warning

建议风格与 `deploy/install.sh` 保持一致：偏结果导向，不输出无关调试噪声。

## Failure Semantics

失败时必须满足以下要求：

- 返回非零退出码
- 不留下半安装的 `~/.local/bin/ae-cli`
- 错误信息优先说明失败阶段，再补底层错误

至少覆盖这些失败场景：

1. latest tag 解析失败
2. 不支持的 OS
3. 不支持的架构
4. archive 下载失败
5. `checksums.txt` 下载失败
6. release 中缺少 archive checksum 记录
7. checksum 校验失败
8. archive 解压后缺少 `ae-cli`

## Documentation Contract

实现本文后，至少需要新增或更新：

- `ae-cli` 安装文档入口
- 远程安装命令示例
- 指定版本安装示例
- Windows 用户的手动安装提示

文档主叙事应清楚区分：

- backend 部署安装：`deploy/install.sh`
- 开发者 CLI 安装：`ae-cli/install.sh`

不得继续让用户从 backend deploy README 中推断 CLI 如何安装。

## Verification

实现本文时至少应覆盖以下验证：

1. latest 安装成功
2. 指定 tag 安装成功
3. checksum 错误时失败且目标路径未生成 `ae-cli`
4. release 缺少 archive 时失败
5. archive 缺少 `ae-cli` 文件时失败
6. `PATH` 不含 `~/.local/bin` 时只打印提示，不修改 profile

测试形态建议复用现有 deploy shell fixture 风格，在仓库内新增 CLI 安装脚本测试，而不是只靠手工验证。

## Rollout Notes

在实现落地前，不应把以下内容写成已实现事实：

- `ae-cli/install.sh` 已存在
- 用户已经可以通过 `curl | bash` 安装 `ae-cli`
- `ae-cli` 文档已经提供官方安装入口

当前状态仍然是“有 release 归档，但没有对等 installer”；本文定义的是下一步应落地的合同。
