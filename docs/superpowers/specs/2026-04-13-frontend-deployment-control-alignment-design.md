# Frontend Deployment Control Alignment Design

**Status:** Current contract for settings-page deployment control behavior

## Overview

本文定义 `ai-efficiency` 前端在 deployment/update 方向的控制方式，目标是让管理端交互更接近 `sub2api` 当前做法，但保持与本仓库现有 backend deployment contract 一致。

这次对齐不是把 `sub2api` 的实现整套搬进来，而是只对齐以下原则：

- deployment 控制入口继续只放在 **设置页**
- 前端默认不引入全局版本 badge 或跨页面 deployment store
- 前端默认不做持续性的 deployment phase 自动轮询
- 前端在“服务会被重启或暂时不可用”的动作后，用 `/health` 探测恢复并刷新页面
- 前端继续保留针对前端 bundle 切换造成的 chunk load failure 的一次性 reload 保护

## Spec Relationship

- 本文补充 [`2026-04-09-binary-systemd-install-update-design.md`](./2026-04-09-binary-systemd-install-update-design.md) 中“前端 deployment 设置页按 mode 分流”的 UI/交互合同。
- 本文不改变 [`2026-04-08-production-deployment-packaging-design.md`](./2026-04-08-production-deployment-packaging-design.md) 与 [`2026-04-09-binary-systemd-install-update-design.md`](./2026-04-09-binary-systemd-install-update-design.md) 中 backend deployment API、compose updater sidecar、systemd binary update 的职责划分。
- 当本文与更早的“前端可能演进到更强全局状态管理”的讨论冲突时，以本文为准：当前生效合同是 **settings page only**。

## Scope

本文覆盖：

1. deployment 控制入口放置位置
2. 设置页中的 update / rollback / restart 交互
3. compose 与 systemd 模式下的前端差异化恢复行为
4. 服务恢复后的页面 reload 逻辑
5. 前端 bundle 更新导致 chunk 加载失败时的 reload 保护

本文不覆盖：

1. 侧边栏 / 顶栏 / 全局 badge 形式的版本提示
2. 全局 deployment store
3. deployment phase 的长期自动轮询
4. WebSocket / SSE 驱动的 deployment 状态流
5. backend deployment API 结构变更

## Current State

截至 2026-04-13，代码已经具备这些基础：

- backend 已提供 deployment status、check、apply、rollback、restart API
- compose 模式通过 updater sidecar 执行 image/tag 更新
- systemd 模式通过 binary updater 执行 apply/rollback，并通过 restart manager 执行重启
- 前端设置页已经有 deployment 区域，并按 mode 展示基本的按钮差异

当前前端不足之处在于：

- deployment 交互仍偏“发起请求后显示消息”，没有稳定的恢复探测约束
- 设置页还没有明确写成“只在必要动作后探测 `/health` 并刷新”的合同
- “是否持续轮询 deployment phase” 尚未被当前 spec 明确否定，容易继续扩散为更重的状态机实现

## Goals

1. 让设置页成为 deployment 控制的唯一入口
2. 让前端行为与 `sub2api` 更一致：
   - 初始加载拉一次版本/状态
   - 用户手动触发 check/update/restart
   - 只在服务恢复阶段做 `/health` 探测
3. 避免引入比当前需求更重的全局状态管理
4. 对齐 backend 的 mode-aware contract，而不是对齐 `sub2api` 的底层升级机制

## Non-Goals

1. 不在本轮引入自动 deployment phase 轮询
2. 不在本轮把 deployment 能力提升到全局导航
3. 不要求前端展示完整的 updater 内部状态机
4. 不要求 compose 与 systemd 强行使用同一套前端恢复分支

## Approved Approach

本轮采用“`sub2api` 风格，对齐前端交互，但尊重 backend mode 差异”的方案。

核心原则：

- **入口只在设置页**
- **页面加载时读取一次 deployment status**
- **点击 Check Updates 时手动刷新状态**
- **不引入自动 phase 轮询**
- **只有在服务预期会暂时不可用时，前端才进入 `/health` 恢复探测**

## UI Contract

### Control Placement

deployment 控制继续放在 `frontend/src/views/SettingsView.vue` 的 deployment 区域，不新增：

- 侧边栏版本 badge
- 顶栏更新条
- Dashboard 版本入口

### Initial Load

设置页进入时前端执行一次 deployment status 拉取，用于展示：

- current version
- current mode
- latest release（若可获取）
- update status（仅展示当前接口返回值，不做额外推断）

### Manual Refresh

用户点击 `Check Updates` 时，前端再次请求 deployment check/update status 接口并刷新显示。

前端不应在后台持续轮询这个接口。

## Mode-Aware Action Behavior

### Compose / Bundled / External Mode

对于 compose 语义的部署模式，`Apply Update` 与 `Rollback` 都可能导致 backend 服务重启或短暂不可用。

因此前端在这两个动作成功提交后应：

1. 更新页面提示文案
2. 进入 `/health` 恢复探测
3. backend 恢复后执行 `window.location.reload()`
4. 如果到达最大重试次数仍未探测成功，也执行一次 reload，让浏览器重新尝试加载新服务

### Systemd Mode

对于 `systemd` 模式：

- `Apply Update` / `Rollback` 只代表二进制切换已完成，**不等于服务已自动重启**
- 前端在这两个动作成功后只更新提示，不进入 `/health` 恢复探测
- `Restart Service` 成功提交后，前端才进入 `/health` 恢复探测并在恢复后 reload

这与 `sub2api` 当前“update 后需要显式 restart”的交互模型保持一致。

## Recovery Probe Contract

### Trigger Conditions

前端仅在这些条件下触发恢复探测：

- compose 模式下的 `Apply Update`
- compose 模式下的 `Rollback`
- 任意模式下的 `Restart Service`

前端不因以下情况触发恢复探测：

- 初次进入设置页
- 手动 `Check Updates`
- systemd 模式下的 `Apply Update`
- systemd 模式下的 `Rollback`

### Probe Mechanism

恢复探测使用：

- `GET /api/v1/health`
- `cache: "no-cache"`

探测语义：

1. 固定重试次数
2. 固定重试间隔
3. 任意一次返回 `response.ok` 则立即 reload
4. 若全部失败，最后仍执行一次 reload

原因是服务可能已经恢复，但某次请求被瞬时网络错误或浏览器缓存行为影响，不应让前端永久停留在“等待中”。

## Frontend Bundle Update Safety

当 deployment 操作替换前端 bundle 后，已打开页面中的旧 chunk URL 可能失效。

前端路由层必须继续保留一次性 reload 保护：

- 检测 dynamic import/chunk load failure
- 在短时间窗口内只自动 reload 一次
- 避免无限 reload loop

该行为不依赖 deployment mode，属于通用前端升级保护。

## Implementation Shape

推荐的最小实现边界：

- `frontend/src/views/SettingsView.vue`
  - 保持 deployment 区作为唯一控制入口
  - 只编排按钮交互、消息和模式分支
- `frontend/src/utils/deploymentRecovery.ts`
  - 统一放置 `/health` 恢复探测与 chunk reload helper
- `frontend/src/router/index.ts`
  - 只负责 chunk load failure -> reload once

不推荐在本轮引入：

- `frontend/src/stores/deployment.ts`
- 跨页面共享的 deployment polling service
- 全局 deployment overlay

## Testing Contract

前端测试至少覆盖：

1. compose 模式下 `Apply Update` 会触发恢复探测
2. compose 模式下 `Rollback` 会触发恢复探测
3. systemd 模式下 `Apply Update` 不触发恢复探测
4. `Restart Service` 会触发恢复探测
5. `/health` 在恢复成功后 reload
6. `/health` 在达到最大重试次数后也会 reload
7. chunk load failure 只在时间窗口内自动 reload 一次

## Rationale

为什么不做自动 phase 轮询：

- `sub2api` 当前并没有完整的 deployment phase 自动轮询，主要是“用户触发 + restart 后 `/health` 探测”
- 当前 backend 的 `update_status` 在 compose updater 路线上也不是持久化状态机，不足以支撑高质量的前端长期轮询
- 对当前需求来说，自动 phase 轮询会引入更多状态复杂度，但不会显著提升成功路径体验

为什么保留 mode-aware 恢复差异：

- 这是当前 backend 合同的一部分，不是前端可随意抹平的细节
- compose 路线的 `apply/rollback` 与 systemd 路线的 `apply/rollback` 语义本来就不同
- 直接照搬 `sub2api` 的“只有 restart 后才恢复探测”会在 compose 模式下留下断页窗口

## Acceptance Criteria

满足以下条件即可认为本合同落地：

1. deployment 控制仍只存在于设置页
2. 设置页不会在后台持续自动轮询 deployment 状态
3. compose 模式下 `apply/rollback` 后页面可自动恢复
4. systemd 模式下 `apply/rollback` 后页面不会错误进入恢复等待
5. restart 后页面可自动恢复
6. bundle 更新导致的 chunk failure 会触发一次性 reload 保护
