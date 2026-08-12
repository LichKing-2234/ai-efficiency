# v2 上报接入与个人 Activity 状态实施台账

**日期：** 2026-08-12  
**状态：** 功能实现、全量测试和真实浏览器矩阵完成；#271、#268、#232、#233、#234、#237、#238、#239、#270 均已完成实现、回归与逐项设计审计；hosted CI 暴露的 frontend 体积余量问题已按 #234 合同完成本地修复，等待新 exact head CI 与 staging 交付。
**设计合同：** [Codex Commit Token Attribution v2](../specs/2026-08-11-codex-commit-token-attribution-v2-design.md)  
**交付 PR：** [#235](https://github.com/LichKing-2234/ai-efficiency/pull/235)

## 执行边界

- 本轮交付 #232、#233、#234、#237、#238、#239、#268、#270、#271。
- 不执行 #251 formal cutover、v1 数据清理、production 发布或部署。
- 所有行为测试只经过已确认的公开 seam：CLI 命令输出/状态、认证 HTTP handler/DTO、渲染后的 `/user` 与个人 `/activity` 交互。
- 每个 Issue 均按一个可观察行为切片执行红灯、最小绿灯；不得测试私有 helper。
- 每完成一个 Issue，立即对照 v2 spec 与 Issue 正文检查偏离，并用 `rg` 检查是否重复实现现有 helper、SQL、接口或 readiness 真相源。
- 复用 `ae-cli/internal/reporting`、`ae-cli/internal/hooks`、`ae-cli/internal/attributionlocal`、`backend/internal/activity`、`frontend/src/api` 与 `frontend/src/utils/userSetupReview.ts`；禁止 workaround 或平行实现。

## 基线

- [x] 新 worktree `/Users/admin/ai-efficiency/.worktrees/activity-reporting-onboarding-v2` 基于 `origin/main@452f1833`。
- [x] 根 checkout 与旧 PR worktree 保持不动。
- [x] 已读取当前 architecture、v2 spec 与 v2 总实施台账。
- [x] `ae-cli` 基线测试通过：`go test ./...`（2026-08-12）。
- [x] backend 基线测试通过：`go test ./...`（2026-08-12）。
- [x] frontend 基线通过：`npm test`（759/759）、`vue-tsc --noEmit`、`npm run build:measure`；initial shell 72,550/72,800 B gzip，`/usage` 159,014/159,500 B gzip（2026-08-12）。

## Issue 执行台账

### #232 — CLI 正常接入自动激活 v2 上报

- [x] 红灯：新登录、已有有效登录与成功 discover 的公开命令行为。
- [x] 绿灯：复用同一 v2-safe activation helper，best-effort 且可重试部分成功。
- [x] 覆盖 disabled/revoked、reporter 恢复、OTLP 缺失、安全清理 managed OTel、dry-run/失败 discover。
- [x] 设计偏离与重复实现检查完成。

### #268 — shadow/formal cutover 兼容

- [x] 红灯：持久化 server-advertised contract、v1 `upgrade_required` 后继续 v2、formal 不新建 v1 baseline 且不发正常 v1 请求。
- [x] 绿灯：拒绝不算 ACK，保留 pending；doctor/sync 展示 minimum CLI version；未知或变更合同 fail closed。
- [x] 覆盖未知/矛盾合同、响应丢失与本地数据保留。
- [x] 设计偏离与重复实现检查完成。

### #271 — backend 单一协议合同与 epoch 隔离

- [x] 红灯：默认/环境覆盖/非法启动合同、formal v1 409 no-write、formal v2 ACK、同维度 shadow/formal pool 隔离。
- [x] 绿灯：一个已验证 runtime contract 注入 enrollment、v1、v2 与 materialization；所有生产写路径继承 epoch。
- [x] canonical pool identity/lookup 包含 epoch，非法显式合同不得静默回退默认值。
- [x] 设计偏离与重复实现检查完成。

### #233 — 个人 v2 readiness 与能力合同

- [x] 红灯：认证 DTO/handler 的 capabilities、五态、formal direct/shared 与负例。
- [x] 绿灯：复用 `backend/internal/activity` 正式 epoch readiness 查询，不新增平行 SQL/Redis 真相源。
- [x] 覆盖全历史 `latest_accepted_at`、no-store、查询失败局部可重试与 capability-off 路由边界。
- [x] 随 #234 覆盖 personal-only 挂载、等待态可见性轮询和局部 retry。
- [x] 设计偏离与重复实现检查完成。

### #234 — `/user` 与个人 `/activity` 接入体验

- [x] 红灯：两条独立流程、正常/高级命令、五态短引导、active 常驻状态。
- [x] 绿灯：capability-off 不挂载；命令正文只在 `/user`；等待态按可见性轮询。
- [x] 覆盖 i18n、accessibility、route-local/lazy copy 与 responsive。
- [x] 设计偏离与重复实现检查完成。

### #237 — 额度重置显式选择接入组

- [x] 红灯：空初态、required、高亮、选中关闭、重开清空。
- [x] 绿灯与桌面/移动回归完成。
- [x] 设计偏离与重复实现检查完成。

### #238 — 已有 key 不跳过接入组选择

- [x] 红灯：已有 key 初态、显式进入 key、新建 key 前进、provider/group 切换。
- [x] 绿灯完成。
- [x] 设计偏离与重复实现检查完成。

### #239 — 非 teleported Select 选择后关闭

- [x] 红灯：无 native label 祖先、可访问名称、单次 change、关闭浮层。
- [x] 绿灯完成并审计同模式控件。
- [x] 设计偏离与重复实现检查完成。

### #270 — 仓库行去除重复绑定标签

- [x] 红灯：移除顶部 `已绑定`，保留 `状态 / 正常` 与 `绑定 / 已绑定`。
- [x] 绿灯并验证 desktop table/mobile card。
- [x] 设计偏离与重复实现检查完成。

## 全量验证与交付

- [x] `cd ae-cli && go test ./...`
- [x] `cd backend && go test ./...`
- [x] `cd frontend && npm test`
- [x] `cd frontend && npm exec vue-tsc -- --noEmit`
- [x] `cd frontend && npm run build:measure`
- [x] `cd frontend && npm run test:e2e:role`
- [x] 390/768/1024/1280/1440 浏览器矩阵、键盘/焦点、无页面级横向溢出。
- [x] `git diff --check` 与最终 spec/Issue/代码逐项审计。
- [x] PR #235 标题、正文和 head 更新为本轮真实含义。
- [ ] Hosted CI 对 exact PR head 全绿。
- [ ] Staging 部署 exact candidate，并回读镜像/提交、health、capabilities、CLI/Activity 行为。
- [ ] production 保持不变。
- [ ] 合并后再按 hosted main/CI/readback 更新或关闭 Issues。

## 红绿证据

### #232 / 切片 1：已有有效登录态

- 红灯：`go test ./cmd -run TestLoginCommandActivatesReportingWhenValidTokenExists -count=1` 失败；当前命令在打印 `Already logged in` 后早退，安装/启用调用均为 0，且没有 baseline/hooks。
- 绿灯：同一测试通过；已有有效登录态现在经过共享 `activateV2Reporting`，完成 enrollment、baseline、`reporting_enabled=true`、`otel_enabled=false`、managed OTel 精确清理和全局 hooks。

### #232 / 切片 2：成功 discover

- 红灯：`go test ./cmd -run TestDiscoverCommandActivatesReportingAndPreservesSelectedProvider -count=1` 失败；工具配置成功，但 enrollment/enable 调用仍为 0，当前 discover 没有激活上报。
- 绿灯：同一测试通过；非 dry-run discover 先用现有 reporting config 持久化 `relay_provider_id`，再复用 `activateV2Reporting`。配置成功但激活失败只输出可恢复 warning。

### #232 / 切片 3：discover 激活边界

- 红灯：`TestDiscoverCommandDoesNotActivateWhenNoSupportedToolIsDetected` 与 `TestDiscoverCommandDoesNotActivateWhenNoToolConfigurationIsWritten` 分别发现 no-tool 与 no-matching-credential 路径仍调用 activation。
- 绿灯：activation 只发生在非 dry-run 且 `ConfigureTools` 实际返回至少一个 `Configured` 工具之后；配置错误、dry-run、no-tool 和空配置均为零 activation，成功配置后的 activation 错误只产生 warning。

### #232 / 切片 4：恢复、幂等与安全边界

- 回归：disabled installation 由同一 enable 调用恢复；revoked enrollment 失败后不会进入 enable，已有 login 仍成功并给出 degraded warning。
- 回归：只缺 reporter token 时才 rotate；只缺 legacy OTLP token 不 rotate；hook 首次失败前已持久化 enabled/reporting credential，下一次 activation 可补齐 hook 且不 rotate。
- 回归：只移除 endpoint 与 installation credential 都精确匹配的 AE-managed Codex OTel；用户管理的 exporter 保持不变。显式 `ae-cli attribution enable` 复用同一 helper，formal 模式不创建 baseline，也不再误报 “Baseline recorded”。
- 全量：`cd ae-cli && go test ./...` 全绿（2026-08-12）。
- 设计审计：对齐 v2 spec §6/§13 与当前 login/discover/toolconfig 合同；normal path 为 install → login → discover，explicit enable/hooks/init/sync/doctor 仅高级恢复。`rg` 证明 activation、enrollment、baseline 与 enable 各只有一个生产实现；未引入 daemon、设备 UI、OTLP 默认开启、release 或 deploy。

### #268 / 切片 1：formal v2 ACK

- 红灯：`go test ./cmd -run TestSyncCommandAcceptsFormalV2Acknowledgement -count=1` 失败并返回 `invalid v2 claim acknowledgement`；当前 ACK 校验硬编码 `shadow_v2`。
- 绿灯：同一命令测试通过；ACK 现在校验 server-advertised `ledger_epoch + v1_write_policy + minimum_cli_version` 合同，接受一致的 shadow/formal 组合并拒绝未知或矛盾组合。

### #268 / 切片 2：v1 拒绝后继续 v2

- 红灯：`go test ./cmd -run TestSyncCommandContinuesV2AfterV1UpgradeRequired -count=1` 在 v1 bucket 的 409 `upgrade_required` 处终止；v2 endpoint 未被调用。
- 绿灯：同一命令测试通过；client 将结构化 409 转为 typed protocol error，compact engine 保留 v1 pending/seen state并记录 minimum version，runner 仅对该精确错误继续 v2 delivery。

### #268 / 切片 3：诊断状态

- 红灯：`go test ./cmd -run TestSyncStatusShowsV1UpgradeRequirement -count=1` 失败；sync status 只显示 v2 claim 数量，没有已记录的 v1 `upgrade_required` / `minimum_cli_version`。
- 绿灯：同一测试通过；`sync status` 和复用同一状态输出的 doctor 都展示 actionable protocol state，不进入正常 Activity readiness UI。

### #268 / 切片 4：持久合同与 formal v2-only 行为

- 红灯：`TestLoginCommandPersistsFormalProtocolWithoutCreatingV1Baseline` 发现 enrollment 合同未写入 `reporting.json`，formal activation 仍创建 v1 baseline；`TestSyncCommandUsesPersistedFormalProtocolWithoutV1BaselineOrRequest` 发现 formal sync 仍要求 compact state。
- 绿灯：enrollment/rotate/enable 必须返回同一个有效合同并写入现有 `reporting.Config.Protocol`；formal 模式不创建、不读取 v1 baseline，不发送正常 v1 请求，直接校验并消费 explicit v2 ACK。

### #268 / 切片 5：lossless/fail-closed 边界

- 回归：两个合法但不一致的 shadow/formal ACK 均返回错误且不消费 Request claim；响应丢失保留全部本地 claim；普通 v1 错误终止当前 pass 且不进入 v2；transition-era 409 保留 v1 pending、不推进 seen atoms，并在同一 pass 继续 v2。
- 回归：shadow `accept` 的有效 login 仍创建 baseline，transition command seam 仍发出 v1 请求；缺失、未知或矛盾合同均无法成为 enabled compact uploader。
- 全量：`cd ae-cli && go test ./...` 全绿（2026-08-12）。
- 设计审计：对齐 v2 spec §6/§14；合同唯一持久化在 `reporting.Config.Protocol`，`CompactState.V1WritePolicy/MinimumCLIVersion` 仅记录实际 v1 409 诊断；没有从 capability/env 推断合同，没有第二套 validator，也未执行 cutover/reset/release/deploy。

### #268 / 切片 4：backend protocol DTO

- 红灯：`go test ./internal/handler -run TestV2ClaimHTTPReplayAuthorizationAndEpochIsolation -count=1` 失败；enrollment 缺少 `protocol`，v2 ACK 也没有 `v1_write_policy`。
- 绿灯：同一认证 HTTP 测试通过；一个 `attributionledger.ProtocolContract` 从 runtime config 注入 installation、v1 ledger、v2 claim 与 pool epoch，默认 shadow/accept，并由唯一 validator 拒绝矛盾组合。

### #271 / 切片 1：epoch-isolated durable pools

- 红灯：`go test ./internal/attributionpool -run TestMaterializeRequestClaimKeepsLedgerEpochsIsolated -count=1` 在 formal contribution 命中已有 shadow canonical key，返回 `canonical attribution usage pool conflict`。
- 绿灯：canonical identity 与 coverage-only pool identity 均纳入 claim-group ledger epoch；相同 provider/user/model/bucket/commit 的 shadow/formal Token pool 与 coverage pool 各自独立。

### #271 / 切片 2：formal v1 write gate 与 v2 ACK

- 红灯：认证 HTTP vertical slice 中 bucket batch 已返回 409，但 revision endpoint 仍进入 v1 查询并返回 422。
- 绿灯：bucket/revision 两个 v1 mutation seam 均返回结构化 `409 upgrade_required + minimum_cli_version` 且不写 bucket；同一 formal runtime 的 v2 ingest 返回 formal ACK。

### #271 / 切片 3：fail-closed startup 与可部署配置

- 红灯：非法 formal/accept 合同先返回无关的 missing-dependency 错误；writable config 往返丢失 formal contract；deploy examples 未声明三个协议字段。
- 绿灯：生产 router 在依赖初始化前用唯一 validator 拒绝非法合同；service constructors 只接收已验证合同；config/env/writable-config/五套 Compose 示例共享默认 `shadow_v2 + accept + empty minimum`。
- 回归：`cd backend && go test ./...` 全绿（2026-08-12）；`git diff --check` 全绿。
- 设计审计：保持 v2 spec §6/§8/§14 的事件驱动、formal epoch 隔离与显式 cutover 顺序；未开启 capability、未切换 Activity、未执行 release/reset/deploy。`rg` 证明 backend 只有 `NormalizeProtocolContract` 一个语义 validator，pool 写入均继承 group/runtime epoch。

### #233 / backend 切片 1：显式 capabilities

- 红灯：`TestAuthMeDefaultsReportingCapabilitiesOff`、`TestAuthMeReportsConfiguredReportingCapabilities` 与 `TestSetupRouterRejectsReadinessCapabilityWithoutSetup` 先证明 `/auth/me` 没有显式合同且非法组合可启动。
- 绿灯：`attribution.setup_available` / `readiness_available` 进入 config、环境变量、writable config、五套 Compose 和 production router；两者默认 false，readiness 只能在 setup=true 且单一协议合同为 formal v2 时启动。

### #233 / backend 切片 2：五态与正式 readiness

- 红灯：认证 `GET /api/v1/attribution/status` 先为 404；随后 enabled installation + formal direct/shared pool 仍停在 `waiting_for_data`。
- 绿灯：installation 模块只聚合 `not_enrolled` / `revoked` / `disabled` / enabled-candidate；handler 仅对 enabled-candidate 调用 `activity.Service.V2PersonalReadiness`。Activity 原有 formal direct/shared SQL 一次返回 `MIN(created_at)` 和 `MAX(created_at)`，overview 保留 first，onboarding 使用 full-history latest；没有第二份 pool SQL 或 Redis readiness cache。
- 负例：v1 bucket、shadow pool、uncommitted pool 与 `inherited_non_counting` 均保持 waiting；另一 revoked installation 不会降级已有 enabled+formal 用户；DTO 不含 installation count/list/id/last-seen。
- HTTP 边界：capability-off 不挂载 route；成功响应 `Cache-Control: no-store`；PostgreSQL readiness 失败返回局部 `503` 和 `details.retryable=true`，不伪装成 waiting。
- 聚焦回归：`go test ./internal/activity ./internal/attributionledger ./internal/config` 全绿；`go test ./internal/handler -count=1` 全绿（148.039s，2026-08-12）。
- 最终审计：协议 epoch 是 Activity/readiness 唯一运行时来源；shadow 协议使 Activity epoch 为空，formal 协议才注入 formal epoch。#234 已通过前端公开 seam 证明 personal-only 挂载、30 秒可见性轮询和局部失败边界，#233 无第二 readiness 真相源。

### #234 / 切片 1：能力边界与两条独立流程

- 红灯：`/user` 原自动配置区混合 install/login/hooks/repo init/doctor/sync，且 `/activity` 没有 v2 readiness；capability-off 与 personal-only 没有公开行为覆盖。
- 绿灯：API key/接入组区域只保留选中 provider 的 discover 配置；独立 full-width 上报区只在 `setup_available=true` 时挂载，readiness 关闭时不请求状态；个人 `/activity` 仅在 `readiness_available=true` 时挂载 compact guide，member 页面零挂载、零请求。

### #234 / 切片 2：命令、状态与轮询

- 绿灯：正常路径严格为 install → login → discover；高级区默认折叠但始终可发现，仅包含 status/doctor/sync/upload status/repo fallback，且 `ae-cli init --hooks repo` 明确为 global managed hooks 不可用时的恢复路径。所有状态均不展示显式 enable 或 OTel 命令。
- 绿灯：active 为常驻低强调状态并使用 `latest_accepted_at`；compact guide 不含命令，只跳转 `/user`。waiting 每 30 秒轮询、hidden 暂停、visible 立即检查、active 停止；readiness 失败局部可重试且不阻塞 Activity analytics。
- 测试根因修复：组合测试暴露旧 wrapper 未卸载导致全局 `visibilitychange` listener 泄漏；统一在 `afterEach` unmount 后组合筛选 10/10、相关四套回归 85/85 通过，未放宽调用次数或改 production 轮询算法。

### #234 / 切片 3：历史视觉回归与文案合同

- 红灯：新 v2 高级命令 Collapse 再次把 `px-4` 放到 Element Plus 根节点，复现原 PR 已修复的断边风险。
- 绿灯：外层保留连续边框和 `overflow-hidden`，横向 padding 仅放到 title/content；回归测试锁定三层职责。#234 Issue 同步补充该验收项，已关闭的 #236 不因替代交互而重复 reopen。
- 文案明确限定为 Codex HTTP 上报，不承诺 Claude/Gemini/Kiro/WebSocket/v1/AE OTel；route-local 中英文 copy 通过 i18n 与命令语义回归。
- 聚焦回归：五个相关文件 94/94；`vue-tsc --noEmit` 通过；首次 build 捕获并修正 browser timer ID 类型后 `build:measure` 通过，initial shell 72,665/72,800 B gzip，`/usage` 159,095/159,500 B gzip。最终文案/样式后的体积测量留在全量验证再回读。
- 设计审计：只存在一个 `frontend/src/api/attribution.ts` endpoint owner 和一个 `ReportingReadinessGuide` 轮询 owner；未新增 store、第二 API、WebSocket、Redis readiness 或设备级 UI。对齐 v2 spec §13 与 #233/#234，未重复实现 Activity formal readiness 真相源。

### #237 / 额度重置显式选择接入组

- 红灯：现有 modal 每次打开都把第一条 group 静默写入 `selectedGroupID`，导致未选择就展示第一组用量，提交只会报 reason required；非 teleported Select 还嵌在 native `label` 内。
- 绿灯：每次打开显式清空 group/reason/error；空选时用 cyan required field 和明确 placeholder 突出第一动作，用量摘要只在明确选择后出现。Select 脱离 native label 并保留 `aria-label`/`aria-required`，选择 Element Plus option 后 `aria-expanded=false`。
- 回归：组件 4/4 覆盖空初态、group required、选中 `group_id`、用量切换、弹层关闭、重开清空和中文术语；Usage 页面真实入口 1/1 改为先显式选择再提交；i18n 9/9，字典合同同步为 1027/1027。
- 设计审计：复用现有 `QuotaResetRequestModal`、`getQuotaResetOptions` 和 submit event，没有新 store/API/helper；v2 attribution 合同无关且未受改动。#239 继续负责同类 non-teleported Select 的全局模式审计，#237 只修当前入口。

### #238 / 已有 key 不跳过接入组选择

- 红灯：初始 provider 默认组已有 key 时，`onboardingActiveStep` 从 0 变 1 的 watcher 自动把 visible step 切到密钥面板，直接显示 masked secret；选择另一个已有 key 的 group 也被再次传送到 key step。
- 绿灯：初始化已有 key 只更新进度/可达步骤，visible step 保持接入组选择；用户显式点击第 2 步后才渲染密钥。当前交互中新建 key 成功会明确前进到第 2 步；provider/group 切换始终回第 1 步并清除 test/confirm 状态。
- 回归：新增已有 key 初态/显式导航断言；扩充 new-key 前进与 provider/group 切换用例；调整只读 secret/model 测试先显式进入 key step。`user-view.test.ts` 38/38 通过。
- 设计审计：仅收敛现有 `onboardingVisibleStep` 状态机，没有新增第二个 step owner、helper、store 或 API；API key 接入体验与 v2 reporting 区仍保持独立。

### #239 / 非 teleported Select 选择后关闭

- 红灯：源码审计定位 11 个 `teleported=false` Select 嵌套 native `label`：Admin 用户筛选/订阅管理、额度重置审批设置、Directory Sync schedule。label activation 可能把 option click 再传给 Select 并重开；同批审计还发现若干非嵌套 Select 只有视觉 label、没有显式可访问名称。
- 绿灯：受影响 native label 改为无点击激活语义的 `div + span`，Select 保留原 class、model、disabled、filter/remote/clearable/change 行为，并统一增加 `aria-label`。默认 teleported 控件和非 Select popconfirm 未改。
- 证明：Vue AST 审计所有 26 个 `teleported=false` 控件，零 Select 留在 native label 内，且每个 Select 均有 `aria-label`/`aria-labelledby`；Admin、Quota settings、Directory、User 四套完整回归 170/170。
- 代表交互：Admin access-status、Quota webhook channel、Directory schedule 选择后均为 `aria-expanded=false`；Admin 数据请求只增加一次，证明单次 change。测试同时覆盖筛选、远程 department/approver、disabled credential 与订阅操作控件的可访问名称。
- 设计审计：纯 Element Plus 模板语义修复，没有 wrapper、directive、全局 click workaround 或第二状态源；与 v2 attribution 合同无关。

### #270 / 仓库行去除重复绑定标签

- 红灯：响应式单一 row DOM 对 bound Repository 同时渲染顶部 `Bound` tag 和详情 `Binding / Bound`，精确行内文本出现 2 次。
- 绿灯：删除 identity 区顶部 binding tag 和多余 flex shell，保留详情 `Status / Active`、`Binding / Bound`；unbound 详情保留 `Binding / Needs binding` 和 `Bind platform` 动作。
- 回归：精确行为先红后绿；`repo-list-view.test.ts` 65/65 通过。row 仍是单一响应式 DOM，移动端 block、桌面 `xl:grid`，没有维护两套 table/card 表达或新增状态 helper。
- 设计审计：只删除重复展示，Repository inventory/filter/health summary、status tag、binding detail、actions 和后端合同均不变；符合用户对原 PR 能力的保留决策和 #270。

### 真实浏览器矩阵 / 新 UI 补测

- 红灯：现有角色矩阵的 `/auth/me` 未声明 `reporting_capabilities`，也未 mock `/api/v1/attribution/status` 与 `/api/v1/user/quota-reset/options`；因此旧 126 条检查虽绿，但从未渲染新 reporting guide 或真实打开申请弹窗。
- 绿灯：只扩展现有 HTTP boundary mock 和 route exercise；`/user` 验证 full guide、正常命令、默认折叠与键盘展开的连续边框，个人 `/activity` 验证 command-free active compact guide，member Activity 验证零引导，`/usage` 验证空组选项、选择关闭与用量后显，`/repos` 验证每行单一 binding 表达。
- 完整矩阵：`126/126`，覆盖 390/768/1024/1280/1440；截图写入 `/tmp/ae-e2e-role-v2-final/`。所有页面无 document/body/main 横向溢出，视觉复核通过。

### #234 / Hosted CI 体积预算修复

- 红灯：PR head `0bb9d02f` 的 hosted frontend CI 在 Node 20 测得 initial shell `72,819/72,800 B gzip`；本地 Node 26 为 `72,678 B`，说明原本只留 122 B 的余量不足以覆盖 Node/zlib 差异。现有 72,800 阈值保持不变。
- 根因：本 PR 没有修改 initial-shell TypeScript 模块；新增 lazy route UI 使用了 9 个主干未生成的 Tailwind utilities，它们进入全局入口 CSS。未改 auth、router chunk recovery、session 逻辑或构建阈值。
- 绿灯：Reporting guide 与 quota modal 改为复用主干已有的颜色、tracking、ring 和 code-block utilities，并删除无意义的 `transition-colors`；空接入组现在以已有 `border + ring` 组合保持明确高亮。没有新增 CSS helper、wrapper、依赖或构建特例。
- 回归：相关三套组件测试 `63/63`、frontend 全量 `774/774`、`vue-tsc --noEmit`、角色 E2E 重跑和 `git diff --check` 均通过。`build:measure` 为 initial shell `72,605/72,800 B gzip`，保留 195 B 本地余量；`/usage` 为 `159,052/159,500 B gzip`。
- 设计审计：改动只收敛 #234/#237 的表现层 utility 选择，不改变五态 readiness、轮询、命令、额度重置显式选择、Element Plus 交互或 v2 协议；仍复用原组件和既有 Tailwind 设计系统，不存在 workaround 或重复实现。

### 最终审计

- 审计发现 installation 状态查询失败原先只返回裸 503，与 readiness 的局部可重试合同不一致；新增公开 handler 红灯后统一为 `details.retryable=true`，两类失败聚焦回归与 backend 全量测试均通过。
- `git diff --check`、全部变更 Go 文件 `gofmt -l`、真实数据/凭据模式和 debug/temp 文件扫描均无结果；未新增平行 API、store、SQL、readiness cache 或 runtime epoch 来源。
- GitHub #234、#237、#270 已补充真实浏览器验收项；#271 继续承接 backend 协议/epoch 隔离，#268 继续承接 CLI 兼容，不新增逃逸 E2E Issue。

后续按 Issue 在每个切片完成时继续追加：最小实现、通过测试与设计审计结论。
