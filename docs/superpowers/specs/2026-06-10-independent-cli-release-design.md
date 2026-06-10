# Independent CLI Release Design

**Status:** Approved design; implementation plan pending

## Overview

当前 `ai-efficiency` 采用单一产品发布线：`v*` tag 会触发全仓 release，发布 GitHub Release 二进制、GHCR 平台镜像，并在生产操作中继续推进 Helm。这个模型对平台部署是合理的，但会让 `ae-cli` 的小改动也产生后端镜像和 Helm 发布噪音。

本文定义新的 release 边界：继续把后端、前端、部署资产和平台镜像视为一个平台产品，同时把 `ae-cli` 作为独立发布单元。目标是优先减少无意义的后端和 Helm 发版，并兼顾用户可见的 CLI 版本清晰度。

## Spec Relationship

- 本文补充 release 与版本边界，不改变业务模块架构。
- 平台部署形态仍以 [`2026-04-08-production-deployment-packaging-design.md`](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-04-08-production-deployment-packaging-design.md) 为主：前端构建产物嵌入后端镜像，生产部署面向一个平台应用镜像。
- GitHub Release、GHCR 和 GoReleaser 的历史落地轨迹仍保留在 [`2026-04-08-github-primary-repo-release-automation.md`](/Users/admin/ai-efficiency/docs/superpowers/plans/2026-04-08-github-primary-repo-release-automation.md)，但当前行为以 `.github/workflows/release.yml` 和 `.goreleaser.yaml` 为准。
- 本文不要求拆分仓库，也不要求把前端从后端镜像中拆成独立部署单元。

## Community Context

社区 monorepo release 通常在两个模型之间取舍：

- **Fixed / locked release**：多个包共享一条版本线。Lerna 文档把这描述为所有包版本自动绑定的模型，适合需要同版本协同发布的产品。
- **Independent release**：不同项目各自发布。Nx 支持把 `projectsRelationship` 设为 `independent`，Lerna 的 independent mode 允许各包独立递增版本。Changesets 也提供 `fixed` / `linked` 等 monorepo 选项，用于控制哪些包一起 bump。

本仓库不是纯 JS package monorepo，而是 Go backend、Vue frontend、Go CLI 和部署资产的混合产品仓库。因此不引入 Nx、Lerna 或 Changesets 作为新的 release 编排层。GoReleaser 有 tag prefix 的 monorepo 支持，但官方文档标注该功能属于 GoReleaser Pro。当前更轻量的做法是使用 Git tag 命名空间、独立 GitHub Actions workflow 和独立 GoReleaser 配置表达两个 release unit。

相关参考：

- https://nx.dev/docs/guides/nx-release/release-projects-independently
- https://lerna.js.org/docs/features/version-and-publish
- https://github.com/changesets/changesets/blob/main/docs/config-file-options.md
- https://goreleaser.com/customization/monorepo/
- https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax

## Current State

当前 release 配置呈现 lockstep 行为：

- `.github/workflows/release.yml` 监听 `push.tags: v*` 和手动 `workflow_dispatch`。
- 同一个 release workflow 会运行 backend、`ae-cli`、frontend、release frontend embedding、deploy validation 等验证。
- 同一个 release workflow 会发布 GHCR 平台镜像。
- 同一个 release workflow 会调用 `.goreleaser.yaml` 发布 GitHub Release 二进制。
- `.goreleaser.yaml` 同时定义 `backend-server`、`backend-updater` 和 `ae-cli` builds，并把 backend bundle 与 CLI artifact 放在同一个 GitHub Release。

这种模型的问题不是 correctness，而是 release scope 过宽：当提交只影响 `ae-cli` 时，平台镜像、后端 bundle、GitHub latest release 和后续 Helm rollout 都可能被牵动。

## Goals

1. `ae-cli` 小改动可以独立发布，不触发后端镜像构建和 Helm 发布。
2. 平台 release 继续覆盖后端、前端、部署资产、GHCR 镜像和 Helm 消费的版本。
3. 用户能够区分平台版本和 CLI 版本。
4. 保留一个 repo，避免因过早拆仓引入跨仓同步成本。
5. 保持当前生产部署合同：前端和后端仍作为平台应用一起交付。
6. release trigger 必须明确、可审计，不依赖 tag push 上不可用或易误解的 path filters。

## Non-Goals

1. 不拆分 `backend`、`frontend`、`ae-cli` 到多个仓库。
2. 不把 frontend 拆成独立部署或独立版本线。
3. 不引入 Nx、Lerna、Changesets 作为新的必需 release 工具链。
4. 不要求购买或依赖 GoReleaser Pro。
5. 不在第一阶段自动生成跨 release unit 的完整 changelog。
6. 不改变 Helm chart 的版本策略，除非后续平台发布实现计划明确需要。

## Release Units

### Platform Release Unit

平台 release unit 包含：

- `backend/`
- `frontend/`
- `deploy/`
- 平台级 docs 和配置
- `.github/workflows/release.yml`
- 平台 GoReleaser 配置
- GHCR 镜像
- Helm 发布所消费的 image tag

平台版本继续使用现有语义：

```text
v0.1.0-preview.N
vX.Y.Z
```

平台 release 的 tag namespace 仍为：

```text
v*
```

平台 release 可以继续作为生产 Helm rollout 的来源。CLI-only change 不应创建新的平台 tag。

### CLI Release Unit

CLI release unit 包含：

- `ae-cli/`
- CLI 安装脚本中直接引用 CLI release artifact 的部分
- CLI 使用说明、onboarding 文档和兼容性说明
- CLI 专用 GoReleaser 配置
- CLI 专用 GitHub Actions workflow

CLI 版本使用独立 tag namespace：

```text
ae-cli/vX.Y.Z
ae-cli/vX.Y.Z-preview.N
```

CLI release 只发布 `ae-cli` artifact，不发布 GHCR 镜像、不发布 backend bundle、不更新 Helm。

## Workflow Design

### Platform Workflow

现有 `.github/workflows/release.yml` 保留平台职责，但实现阶段应收窄 GoReleaser 输出：

- 保留 backend、frontend、deploy validation。
- 保留 release frontend embedding test。
- 保留 GHCR image build and push。
- 保留 backend bundle release artifact。
- 从平台 release artifact 中移除 `ae-cli`。
- 保持 `push.tags: v*`。
- 保持手动 dispatch 只接受平台 tag，例如 `v0.1.0-preview.42`。

如果需要平台 release 验证 CLI 与平台 API 的基础兼容性，应以轻量 contract test 表达，而不是把 CLI artifact 纳入平台 release。

### CLI Workflow

新增 CLI 专用 release workflow，例如：

```text
.github/workflows/ae-cli-release.yml
```

触发条件：

```yaml
on:
  push:
    tags:
      - 'ae-cli/v*'
  workflow_dispatch:
    inputs:
      tag:
        description: CLI release tag, for example ae-cli/v0.2.1
```

CLI workflow 的职责：

1. 解析并验证 tag 必须匹配 `^ae-cli/v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$`。
2. checkout 对应 tag。
3. 运行 `cd ae-cli && go test ./...`。
4. 运行 CLI release sanity check，例如 `ae-cli version` 构建元数据测试。
5. 调用 CLI 专用 GoReleaser 配置，只构建和上传 CLI artifact。
6. GitHub Release 名称使用 `ae-cli <version>`。
7. GitHub Release prerelease 判断仍沿用 semver 后缀规则。
8. CLI GitHub Release 必须保持在仓库级 latest 之外。

### GitHub Latest Release Ownership

仓库级 `https://api.github.com/repos/LichKing-2234/ai-efficiency/releases/latest` 仍归平台 release 所有。

这是硬性边界，因为当前平台部署和更新路径依赖 `/releases/latest`：

- `backend/internal/deployment/release_source.go` 通过配置的 release API URL 查询 latest release。
- `deploy/docker-deploy.sh` 和 deploy compose 默认值使用平台 `/releases/latest`。
- 当前 `ae-cli/install.sh`、`ae-cli/install.ps1` 和 `ae-cli update` 也默认读取仓库级 `/releases/latest`，实现阶段必须同步改为 CLI 专用 release discovery。

因此：

- 平台 release 可以设置为 repository latest。
- CLI release 不得设置为 repository latest。
- CLI installer / updater 不得继续把仓库级 `/releases/latest` 当作 CLI latest。
- CLI installer / updater 应通过列出 releases 并筛选 `ae-cli/v*`，或通过新的 CLI 专用 release API 入口，找到最新 CLI release。
- 如果 GitHub Release 或 GoReleaser 默认会把新 release 标记为 latest，CLI workflow 必须显式关闭或在发布后校正。

### Why Not Path Filters For Release Split

GitHub Actions 官方文档说明，path filters 不会在 tag push 时求值。因此 release 分流不能依赖 `paths` 判断本次 tag 是否只改了 `ae-cli/`。

本设计使用 tag namespace 作为 release source of truth：

- 平台 tag 表示平台发布意图。
- CLI tag 表示 CLI 发布意图。

这比根据 changed files 推断 release unit 更明确，也更适合人工审计。

## GoReleaser Design

第一阶段采用两份配置：

```text
.goreleaser.yaml
.goreleaser.ae-cli.yaml
```

平台 `.goreleaser.yaml`：

- 保留 `project_name: ai-efficiency`。
- 保留 `backend-server` 和 `backend-updater` builds。
- 保留 `backend-bundle` archive。
- 移除 `ae-cli` build 和 `ae-cli` archive。

CLI `.goreleaser.ae-cli.yaml`：

- 使用 `project_name: ae-cli`。
- 只定义 `ae-cli` build。
- `dir: ae-cli`。
- `binary: ae-cli`。
- `ldflags` 写入 `github.com/ai-efficiency/ae-cli/internal/buildinfo.Version`。
- 因 tag 带有 `ae-cli/` 前缀，workflow 应把剥离后的版本传给 GoReleaser 或在配置中显式处理版本显示，确保 `ae-cli version` 输出 `vX.Y.Z`，而不是 `ae-cli/vX.Y.Z`。

不使用 GoReleaser Pro 的 `monorepo.tag_prefix`，避免新增商业功能依赖。实现阶段需要验证 OSS GoReleaser 对 slash tag 的 release version 行为；如果默认版本包含前缀，则通过 workflow 环境变量或 build flag 显式传入剥离后的 CLI version。

## Version And Compatibility Contract

平台版本和 CLI 版本彼此独立：

```text
Platform: v0.1.0-preview.42
CLI:      ae-cli v0.2.1
```

兼容性不通过同版本号隐式表达，而通过以下合同表达：

1. CLI 默认应兼容当前生产平台和最近一个平台 release。
2. 如果 CLI 需要新平台 API，CLI release note 必须写明最低平台版本。
3. 如果平台 API 变更会破坏旧 CLI，平台 release note 必须写明最低 CLI 版本或迁移建议。
4. 长期应优先用 capability / endpoint 检测表达兼容性，而不是让 CLI 硬编码过多平台版本判断。

第一阶段不要求实现完整 capability negotiation，但 release 文档和 PR 描述必须明确兼容性影响。

## Developer Workflow

### CLI-Only Change

推荐流程：

1. 修改 `ae-cli/` 和必要的 CLI docs。
2. 跑 `cd ae-cli && go test ./...`。
3. 合并到 `main`。
4. 创建并推送 `ae-cli/vX.Y.Z` tag。
5. 等待 CLI release workflow 完成。
6. 不执行 Helm rollout。

### Platform Change

推荐流程：

1. 修改 backend、frontend、deploy 或平台 docs。
2. 跑对应默认测试。
3. 合并到 `main`。
4. 创建并推送 `vX.Y.Z` 或 `vX.Y.Z-preview.N` tag。
5. 等待平台 release workflow 完成。
6. 按当前生产流程执行 Helm rollout 和 live readiness verification。

### Coordinated Change

当一个功能同时需要平台和 CLI：

1. PR 必须说明这是 coordinated release。
2. 如果平台向后兼容旧 CLI，可以先发平台，再发 CLI。
3. 如果 CLI 向后兼容旧平台，可以先发 CLI，再发平台。
4. 如果两边必须同时升级，应拆成兼容性铺垫、平台发布、CLI 发布三个步骤，避免用户落入不可用中间态。

## Documentation Updates Required During Implementation

实现阶段必须同步更新：

- `docs/architecture.md`：如果架构图或 release 边界描述了 CLI 与平台的交付关系。
- `docs/superpowers/specs/2026-04-08-production-deployment-packaging-design.md`：如果其中 release artifact 描述仍暗示 CLI 必然跟随平台发布。
- `deploy/README.md`：如果安装或升级说明引用 GitHub Release artifact。
- `AGENTS.md` / `CLAUDE.md`：如果 agent 发版规则需要区分 `v*` 和 `ae-cli/v*`。

历史计划文件不应被重写为现状，除非只是标注已被本文 supersede 的范围。

## Testing Strategy

### Platform Release Verification

实现阶段应验证：

- 平台 tag 不触发 CLI release workflow。
- 平台 release workflow 仍跑 backend、frontend、deploy validation。
- 平台 release 仍发布 GHCR image。
- 平台 release 仍发布 backend bundle。
- 平台 release 不再包含 `ae-cli_*` artifact。

### CLI Release Verification

实现阶段应验证：

- `ae-cli/v*` tag 不触发平台 release workflow。
- CLI release workflow 只运行 CLI 测试和 CLI artifact 发布。
- CLI artifact 的 `ae-cli version` 输出剥离前缀后的版本，例如 `ae-cli v0.2.1`。
- CLI release 不发布 GHCR image。
- CLI release 不影响 Helm upgrade inputs。

### Local Validation

实现前可用 `goreleaser release --snapshot --clean -f .goreleaser.ae-cli.yaml` 验证 CLI artifact 配置。

如果 slash tag 导致 OSS GoReleaser 版本推导不符合预期，必须在本地和 GitHub Actions 中用同一套剥离逻辑修正，不能只在 release note 中掩盖。

## Rollout Plan

1. 新增 CLI GoReleaser 配置和 CLI release workflow。
2. 收窄平台 GoReleaser 配置，移除 CLI artifact。
3. 更新 release docs 和 agent 发版规则。
4. 用一个 dry-run 或 snapshot 验证 CLI artifact。
5. 用一个真实 `ae-cli/v*` preview tag 验证 CLI 独立 release。
6. 后续平台 release 时验证 `v*` 路径仍可发布 GHCR、backend bundle，并继续 Helm rollout。

## Implementation Defaults

1. 第一条真实 CLI 验证 tag 使用 `ae-cli/v0.2.0-preview.1`，用于证明独立 release 链路不会触发平台发布。
2. CLI release 不维护仓库级 latest。CLI installer / updater 必须筛选 `ae-cli/v*` releases 来确定最新 CLI 版本。
3. 平台 release 继续维护仓库级 latest，因为平台部署和更新控制面依赖该语义。
4. PR CI 的 changed-file based 提速不进入第一阶段。第一阶段只拆 release，避免把 required checks 策略和发布边界调整混在一起。
