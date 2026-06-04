# User CLI Setup Checklist Design

**Date:** 2026-05-26  
**Status:** Approved design for current implementation; revised 2026-06-04 for audience split and GitHub release connectivity guidance
**Scope:** `frontend/src/views/UserView.vue`, `frontend/src/utils/userSetupReview.ts`, `frontend/src/types/index.ts`, `frontend/src/__tests__/`, `docs/architecture.md`  
**Related:**  
- [2026-05-21-user-page-cli-self-serve-design.md](./2026-05-21-user-page-cli-self-serve-design.md)  
- [2026-05-19-ae-cli-deterministic-tool-configuration-design.md](./2026-05-19-ae-cli-deterministic-tool-configuration-design.md)  
- [2026-05-23-global-git-hooks-design.md](./2026-05-23-global-git-hooks-design.md)  
- [docs/architecture.md](../../architecture.md)

## Spec Relationship

- 本文更新 `/user` 页面中 `CLI Setup Checklist` 的当前用户引导合同。
- 本文不改变 `ae-cli login`、`ae-cli discover`、`ae-cli init`、`ae-cli hooks`、`ae-cli sync` 或 `ae-cli doctor` 的命令语义。
- `/user` 仍然不在浏览器里执行本机 CLI 命令，也不直接探测用户机器状态。
- 本文继承 `2026-05-21-user-page-cli-self-serve-design.md` 的 `/user` 页面定位，但替换其中 `Install / Login / Discover / Verify` 的旧 checklist 结构。
- 当前实现中的 paste-based `Verify` 只做浏览器端输出文本关键字判断，不能证明本机 CLI 或 repo attribution 链路真实可用；本文将验证语义收口到 `ae-cli doctor`。
- 2026-06-04 修订：`/user` 接入进度增加“研发 / 非研发”路径选择。研发路径继续使用 `ae-cli` 完成本机工具配置、hooks 和 repo attribution；非研发路径只引导用户把 provider URL、platform、group 和 API key 手动填入本地 AI 工具配置，不要求安装或运行 `ae-cli`。
- 2026-06-04 修订：`ae-cli` installer / update 仍通过 GitHub Releases 获取版本和安装包；页面和脚本必须把 GitHub 连通性与代理配置提示显式化。

## Problem

当前 `/user` 的 CLI 引导存在三个问题：

1. `Verify` 要求用户粘贴 `ae-cli version`、`ae-cli discover --dry-run` 和 `ae-cli doctor` 输出，再由前端做弱关键字判断。这个动作不能执行真实诊断，也容易给用户“页面已验证”的错误信号。
2. Checklist 只覆盖安装、登录和 `discover`，没有明确引导 `ae-cli init`，也没有解释 `init` 必须在具体 Git repo 目录下执行。
3. 机器级动作和 repo 级动作混在一起。`discover` 与 `hooks enable --global` 属于一次性机器设置；`cd <repo>`、`init`、`doctor` 属于每个需要上报的 repo 的接入动作。

这会让用户完成页面步骤后仍然不知道：

- 是否需要进入目标 repo。
- `ae-cli init` 是否安全重复执行。
- hooks 生效后是否还需要手动 `sync`。
- `doctor` 和旧 `Verify` 哪一个才是可信诊断入口。

## Goals

1. 把 `/user` 的 CLI 引导改为两段式 scope：
   - `Machine Setup`
   - `Per-Repo Setup`
2. 删除 paste-based `Verify` 表单和浏览器端 review 逻辑。
3. 明确推荐完整上手路径：

   ```bash
   ae-cli login
   ae-cli discover --provider <provider>
   ae-cli hooks enable --global
   cd <repo>
   ae-cli init
   ae-cli doctor
   ```

4. 说明 `ae-cli init` 是当前 repo 的 registration/cache bootstrap，幂等执行；已存在 repo 时刷新本地状态和 eligibility cache，未存在 repo 时通过后端 ensure 创建 unbound repo。
5. 说明 `ae-cli sync` 是 manual backfill / recovery 工具，不是 hooks 正常生效后的主流程步骤。
6. 让用户能够区分一次性机器配置和每个 repo 的必需接入动作。
7. 让非研发用户能在同一个接入进度中选择手动本地配置路径，避免被引导到 `ae-cli`、Git hooks 或 repo attribution 步骤。
8. 在安装/更新 `ae-cli` 前暴露 GitHub release 连通性检查，并在 GitHub release API 或下载失败时提示配置 `HTTPS_PROXY` / `HTTP_PROXY`。

## Non-Goals

1. 不新增浏览器到本机 CLI 的执行桥、agent、daemon 或 local proxy。
2. 不改变 `ae-cli init` 默认 `--hooks none` 的 CLI 行为。
3. 不改变 `ae-cli hooks enable --global` 的 hook ownership 合同。
4. 不把 `ae-cli sync` 从 CLI 中移除，也不降低它作为补传/恢复工具的地位。
5. 不在本轮解决 provider group selector、multi-provider advanced routing 或 model selection。
6. 不回写历史 spec，把历史设计全文改成当前状态。
7. 非研发路径不承诺代码仓库使用记录自动上报，也不替代研发路径的 checkpoint / attribution 能力。

## Decisions

### Audience Split

`Setup progress` 顶部展示一个二选一 segmented control：

- `我是研发` / `I'm a developer`
- `我是非研发` / `I'm not a developer`

默认选择研发路径，以保持现有开发者 onboarding 行为。

非研发路径只展示：

1. `Account verified`
2. `Confirm AI access`
3. `Manual local configuration`

`Manual local configuration` 显示当前选中 provider/group 的手动配置参数：

- provider URL
- platform
- group
- API key 获取说明

该路径必须明确说明 `ae-cli is not required` / `不需要 ae-cli`。它不展示 `ae-cli login`、`ae-cli discover`、`ae-cli hooks enable --global`、`ae-cli init` 或 `ae-cli doctor` 作为主流程步骤。

### Two-Scope Checklist

研发路径中的 `CLI Setup Checklist` 分成两个可扫描的区块。

#### Machine Setup

Machine-level setup 是一台开发机器通常只需要做一次的动作：

1. `Install CLI`
2. `Login`
3. `Configure local AI tools`
4. `Enable automatic Git hooks`

`Install CLI` 继续展示 macOS/Linux 和 Windows PowerShell 安装命令。

`Install CLI` 同时展示 GitHub release 连通性检查命令。macOS/Linux 使用：

```bash
curl -fsSI --connect-timeout 5 https://api.github.com/repos/LichKing-2234/ai-efficiency/releases/latest
```

Windows PowerShell 使用：

```powershell
iwr -UseB -Method Head https://api.github.com/repos/LichKing-2234/ai-efficiency/releases/latest
```

如果连通性检查失败，用户应先配置 `HTTPS_PROXY` / `HTTP_PROXY` 再执行 installer。

`Login` 展示：

```bash
ae-cli login
ae-cli login --device
```

`Configure local AI tools` 展示基于当前选中 provider 生成的命令：

```bash
ae-cli discover --provider <provider>
```

`Enable automatic Git hooks` 展示：

```bash
ae-cli hooks enable --global
```

该步骤应说明它是机器级 Git hook 配置。主流程不展示 `--force`；如果命令因已有非 AE hook 配置失败，用户应先阅读输出或排障文档，再决定是否手动使用 `--force`。

#### Per-Repo Setup

Per-repo setup 是每个需要上报的 Git repo 都应执行的动作：

```bash
cd <repo>
ae-cli init
ae-cli doctor
```

`cd <repo>` 必须作为显式步骤展示。没有进入具体 Git repo，CLI 无法检测 repo root、remote、branch、git common dir，也无法派生 workspace id。

`ae-cli init` 放在 `doctor` 前面，而不是 doctor-first：

- 多次执行是安全的。
- 已存在 repo 时，后端返回已有 repo 或刷新 metadata；CLI 记录/刷新本地 eligibility cache。
- 未存在 repo 时，后端 ensure 会创建 unbound repo，使后续 hooks 和 sync 能获得 `repo_config_id`。
- 用户无需先判断后台是否已配置该 repo。

`ae-cli doctor` 是可信诊断入口，替代旧 `Verify`。

### Recovery Commands

主流程下面保留弱化的 recovery 区块，命名为 `Manual backfill / recovery`。

展示命令：

```bash
ae-cli sync
ae-cli hooks status --uploads
```

该区块说明：

- hooks 正常生效后，不需要每次手动 `sync`。
- `sync` 用于 hooks 未启用、网络失败、pending queue、或需要立即补传本地 artifacts 的场景。
- `hooks status --uploads` 用于查看 hook 安装状态和 upload ledger 摘要。

### Remove Paste-Based Verify

删除：

- `Verify` step
- `version` / `discover` / `doctor` textarea
- `Review` button
- `reviewVerifyOutput` UI 调用
- `VerifyReviewItem` / `VerifyReviewSummary` 类型，如果没有其他调用方

如果保留 command builder helper 文件名 `userSetupReview.ts`，它应只承担 setup command construction，不再承担 verify output parsing。

## Data Flow

本轮前端不增加 API 调用。

已有数据流保持不变：

1. `/user` 通过 `GET /api/v1/user/providers` 获取当前用户可用 providers 和 groups。
2. 用户选中的 provider name 用于生成 `ae-cli discover --provider <provider>`。
3. Install command 继续使用当前 `window.location.origin` 作为 `AE_CLI_INSTALL_SERVER_URL`。
4. 非研发手动配置路径复用当前 provider/group 数据，不增加 API 调用。
5. GitHub release 连通性检查命令是页面展示的用户侧命令；浏览器页面本身不对用户机器或网络执行探测。

浏览器页面不读取本机文件，不执行本机命令，不判断 hooks 是否真实安装。

## Error Handling And User Expectations

页面只展示推荐命令和命令定位，不承诺命令已成功执行。

Expected guidance:

- `ae-cli hooks enable --global` 失败时，用户应先查看 CLI 输出，常见原因是已有 non-AE `core.hooksPath`。
- `ae-cli init` 失败时，常见原因是当前目录不是 Git repo、没有 `remote.origin.url`、未登录或后端不可达。
- `ae-cli doctor` 输出是最终诊断来源。
- `ae-cli sync` 仅用于手动补传/恢复，不作为正常 hooks path 的必跑步骤。
- `ae-cli` installer 和 `ae-cli update install` 如果无法访问 GitHub release API、raw install script 或 release artifact，应在错误信息中说明 release 来源是 GitHub Releases，并提示用户配置 `HTTPS_PROXY` / `HTTP_PROXY` 后重试。
- 非研发用户选择手动配置路径后，页面只提供 provider URL、platform、group 和 API key 获取说明；如果实际工具需要更细配置，由对应工具或支持人员继续处理。

## Testing

Frontend tests should cover:

1. `/user` renders `Machine Setup` and `Per-Repo Setup`.
2. Install commands still include macOS/Linux and Windows variants.
3. Login commands still include browser login and device login.
4. Discover command updates when selected provider changes.
5. Checklist renders `ae-cli hooks enable --global`.
6. Per-repo setup renders `cd <repo>`, `ae-cli init`, and `ae-cli doctor`.
7. Recovery renders `ae-cli sync` and `ae-cli hooks status --uploads`.
8. Old paste-based Verify textarea placeholders and `Review` button no longer render.
9. Command builder tests cover the new command helper functions.
10. `/user` renders the developer / non-developer path selector.
11. Non-developer path renders manual local configuration values and does not render `ae-cli` commands inside setup progress.
12. Command builder tests cover GitHub release connectivity commands.
13. `ae-cli` installer/update tests cover GitHub release failure guidance with `HTTPS_PROXY`.

Recommended verification:

```bash
cd frontend && pnpm test
```

Because the 2026-06-04 revision touches frontend plus `ae-cli` installer/update guidance, targeted verification should include frontend tests, `ae-cli/internal/update` tests, and the installer smoke script.

## Documentation

Update `docs/architecture.md` so the `/user` surface describes the current two-scope checklist:

- provider-aware install/login/discover guidance
- developer / non-developer setup path split
- GitHub release connectivity/proxy guidance for install/update
- machine-level global hook setup
- per-repo `init` / `doctor` readiness flow
- recovery-only `sync`

Do not rewrite historical specs except by adding this new spec and linking current behavior from project-level architecture.
