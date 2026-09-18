# AI 效能平台

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![GitHub stars](https://img.shields.io/github/stars/LichKing-2234/ai-efficiency?style=social)](https://github.com/LichKing-2234/ai-efficiency/stargazers)

[English](README.md)

AI 效能平台（`ai-efficiency`）是一个独立系统，用于衡量和优化 AI 辅助开发效能。

## 项目概览

- 后端：Go（`Gin` + `Ent`）模块化单体
- 前端：Vue 3（`Vite` + `Pinia` + `TailwindCSS`）
- CLI：`ae-cli`，负责登录、provider 发现、hooks、collector 和本地工具配置
- Relay 集成：通过 HTTP provider 边界接入 `sub2api`，不直接耦合数据库
- SCM 集成：通过统一 provider 接口对接 GitHub 和 Bitbucket Server

## 当前运行形态

- 后端是认证、仓库管理、分析、归因、部署健康与版本可见性、webhook 处理的统一编排中心。
- 前端单独构建，并在部署时嵌入后端二进制。
- `ae-cli` 的正式工作流已经切到 sessionless：`ae-cli init`、`ae-cli sync`、`ae-cli doctor`。
- 旧的 `ae-cli start/stop/run/...` 命令已不再包含在当前 CLI 二进制中，本地 session proxy 运行时也已退役。
- 当前生产部署支持 Docker Compose 和 Linux systemd 两条路径。

## 仓库结构

```text
ai-efficiency/
├── backend/    # Go backend
├── frontend/   # Vue frontend
├── ae-cli/     # CLI runtime and commands
├── deploy/     # Deployment assets
├── docs/       # 架构、当前合同与历史记录
├── AGENTS.md   # Agent working rules
└── CLAUDE.md   # Lightweight navigation notes
```

## 关键文档

- 架构总览：[`docs/architecture.md`](docs/architecture.md)
- 当前行为合同：[`docs/contracts/`](docs/contracts/README.md)
- 历史理由与证据：[`docs/history/`](docs/history/README.md)
- CLI 安装与使用：[`ae-cli/README.md`](ae-cli/README.md)
- 部署说明：[`deploy/README.md`](deploy/README.md)
- 开源协议：[`LICENSE`](LICENSE)
- agent 协作规则：[`AGENTS.md`](AGENTS.md)

## 文档优先级

当代码、合同和架构文档不一致时，优先级为：

1. 当前代码
2. [`docs/contracts/`](docs/contracts/README.md) 下直接相关的当前合同
3. [`docs/architecture.md`](docs/architecture.md)

未实现的目标状态和工作进度由 GitHub Issues 管理；确有独立价值的架构理由写入 ADR；已跟踪的根目录 `CONTEXT.md` 在存在时负责领域词汇；`docs/history/` 既不是当前行为合同，也不是待办列表。

## 本地开发

### 验证命令

```bash
cd backend && go test ./...
cd ae-cli && go test ./...
cd frontend && npm test
cd frontend && npm run build
```

### 常用入口

- 后端服务：`cd backend && go run ./cmd/server`
- 命令行：`cd ae-cli && go run .`
- 前端开发服务：`cd frontend && npm run dev`

## 开源协议

本项目基于 MIT License 开源，详见 [`LICENSE`](LICENSE)。

## 说明

- 本文件是中文入口文档。
- 当前运行时边界和模块职责请以 [`docs/architecture.md`](docs/architecture.md) 为准。
- 功能级当前行为请查看相关合同；未实现的目标状态由 GitHub Issues 管理。

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=LichKing-2234/ai-efficiency&type=Date)](https://star-history.com/#LichKing-2234/ai-efficiency&Date)
