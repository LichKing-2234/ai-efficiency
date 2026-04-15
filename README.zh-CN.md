# AI 效能平台

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![GitHub stars](https://img.shields.io/github/stars/LichKing-2234/ai-efficiency?style=social)](https://github.com/LichKing-2234/ai-efficiency/stargazers)

[English](README.md)

AI 效能平台（`ai-efficiency`）是一个独立系统，用于衡量和优化 AI 辅助开发效能。

## 项目概览

- 后端：Go（`Gin` + `Ent`）模块化单体
- 前端：Vue 3（`Vite` + `Pinia` + `TailwindCSS`）
- CLI：`ae-cli`，负责登录、session bootstrap、hooks、collector 和本地工具运行时
- Relay 集成：通过 HTTP provider 边界接入 `sub2api`，不直接耦合数据库
- SCM 集成：通过统一 provider 接口对接 GitHub 和 Bitbucket Server

## 当前运行形态

- 后端是认证、仓库管理、分析、归因、部署控制和 webhook 处理的统一编排中心。
- 前端单独构建，并在部署时嵌入后端二进制。
- `ae-cli start` 会向后端完成 session bootstrap，写入本地 workspace/runtime 状态，并可为 Codex 和 Claude 启动本地 session proxy。
- 当前生产部署支持 Docker Compose 和 Linux systemd 两条路径。

## 仓库结构

```text
ai-efficiency/
├── backend/    # Go backend
├── frontend/   # Vue frontend
├── ae-cli/     # CLI runtime and commands
├── deploy/     # Deployment assets
├── docs/       # Architecture and specs
├── AGENTS.md   # Agent working rules
└── CLAUDE.md   # Lightweight navigation notes
```

## 关键文档

- 架构总览：[`docs/architecture.md`](docs/architecture.md)
- 当前主 specs：`docs/superpowers/specs/`
- CLI 安装与使用：[`ae-cli/README.md`](ae-cli/README.md)
- 部署说明：[`deploy/README.md`](deploy/README.md)
- 开源协议：[`LICENSE`](LICENSE)
- agent 协作规则：[`AGENTS.md`](AGENTS.md)

## 文档优先级

当代码、spec 和历史文档不一致时，优先级为：

1. 当前代码
2. `docs/superpowers/specs/` 下最新且最相关的 spec
3. [`docs/architecture.md`](docs/architecture.md)
4. 历史基线文档

## 本地开发

### 验证命令

```bash
cd backend && go test ./...
cd ae-cli && go test ./...
cd frontend && pnpm test
cd frontend && pnpm build
```

### 常用入口

- 后端服务：`cd backend && go run ./cmd/server`
- 命令行：`cd ae-cli && go run .`
- 前端开发服务：`cd frontend && pnpm dev`

## 开源协议

本项目基于 MIT License 开源，详见 [`LICENSE`](LICENSE)。

## 说明

- 本文件是中文入口文档。
- 当前运行时边界和模块职责请以 [`docs/architecture.md`](docs/architecture.md) 为准。
- 功能级行为请优先参考最新 spec，而不是历史 plan。

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=LichKing-2234/ai-efficiency&type=Date)](https://star-history.com/#LichKing-2234/ai-efficiency&Date)
