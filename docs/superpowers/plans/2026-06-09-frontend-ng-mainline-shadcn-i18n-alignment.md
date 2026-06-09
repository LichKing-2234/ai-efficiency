# Frontend NG Mainline Shadcn I18n Alignment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring `frontend-ng/` back into parity with the latest merged `main` frontend capabilities while standardizing UI on shadcn/ui composition and adding React i18n guardrails.

**Architecture:** Keep the TanStack Start BFF-lite boundary already established in `frontend-ng`: browser code calls same-origin `/api/*`, and only server routes proxy to the Go backend. Add missing typed contracts for latest mainline APIs, then build shared shadcn/i18n primitives before touching route pages so page code consumes a consistent system instead of hardcoding controls, copy, and state markup.

**Tech Stack:** Bun, TanStack Start, React 19, TanStack Router, TanStack Query, Tailwind CSS v4, shadcn/ui radix-nova, lucide-react, MiSans, i18next/react-i18next, Recharts through shadcn Chart.

---

## Current Baseline

- Branch `codex/refactor/ng-frontend` has merged `origin/main` as of 2026-06-09.
- Latest `main` capabilities that are not yet aligned in `frontend-ng`:
  - Personal usage dashboard via `GET /api/v1/user/usage/dashboard`, embedded on `/`.
  - Repo inventory workbench via `GET /api/v1/repos/inventory`.
  - Batch webhook repair via `POST /api/v1/repos/repair-webhooks`.
  - Repo detail webhook repair via `POST /api/v1/repos/:id/repair-webhook`.
  - Vue i18n keys for `usageDashboard`, updated repo inventory/webhook copy, and repo detail webhook repair copy.
- `frontend-ng` shadcn context:
  - Framework: TanStack Start.
  - Style/base: `radix-nova` / `radix`.
  - Tailwind: v4, global CSS at `frontend-ng/src/styles.css`.
  - Icons: `lucide`.
  - Installed UI components: `badge`, `button`, `card`, `dialog`, `input`, `table`, `textarea`.
- `frontend-ng` has no i18n dependency yet.
- `frontend-ng/src/lib/api/server.ts` allowlist already covers all required new paths through `/api/v1/user/*` and `/api/v1/repos*`.

## File Structure

Create:

- `frontend-ng/src/lib/i18n/messages.ts` - English and Chinese resource dictionaries, migrated from current Vue `frontend/src/i18n.ts` for all active `frontend-ng` surfaces.
- `frontend-ng/src/lib/i18n/i18n.tsx` - React provider, locale persistence, `useI18n`, interpolation, and language toggle.
- `frontend-ng/src/lib/i18n/no-hardcoded-copy.test.ts` - regression guard for Han characters outside zh-CN resources and route/component hardcoded UI copy.
- `frontend-ng/src/components/primitives/app-alert.tsx` - thin project wrapper around shadcn `Alert` for success/warning/error/info states.
- `frontend-ng/src/components/primitives/confirm-action.tsx` - AlertDialog-based destructive confirmation helper.
- `frontend-ng/src/components/primitives/page-empty.tsx` - Empty-based project empty-state helper.
- `frontend-ng/src/features/user-usage/user-usage-state.ts` - range/date/format helpers for personal usage dashboard.
- `frontend-ng/src/features/user-usage/user-usage-state.test.ts` - focused helper tests.
- `frontend-ng/src/features/user-usage/user-usage-panel.tsx` - shadcn-card/chart personal usage dashboard.
- `frontend-ng/src/features/repos/repo-inventory-state.ts` - inventory selection, URL search, and webhook repair summary helpers.
- `frontend-ng/src/features/repos/repo-inventory-state.test.ts` - focused repo inventory helper tests.
- `frontend-ng/src/features/repos/repo-webhook-state.ts` - repo detail webhook repair eligibility/result helpers.
- `frontend-ng/src/features/repos/repo-webhook-state.test.ts` - focused webhook repair tests.

Modify:

- `frontend-ng/package.json` and `frontend-ng/bun.lock` - add `i18next`, `react-i18next`, and shadcn/chart dependency changes produced by the CLI.
- `frontend-ng/src/routes/__root.tsx` - wrap app with i18n provider and set `<html lang>`.
- `frontend-ng/src/components/layout/navigation.ts` - move labels/sections into translation keys and add localized metadata.
- `frontend-ng/src/components/layout/app-shell.tsx` - add language toggle and translate shell copy.
- `frontend-ng/src/lib/api/types.ts` - add mainline repo inventory, webhook repair, and user usage dashboard types.
- `frontend-ng/src/lib/api/index.ts` - add typed calls for new mainline endpoints.
- `frontend-ng/src/lib/format.ts` - locale-aware number, compact number, date, percent, token, duration, and currency helpers.
- `frontend-ng/src/features/home/home-page.tsx` - embed personal usage dashboard and use translated copy.
- `frontend-ng/src/features/repos/repos-page.tsx` - migrate from grouped flat repo list to inventory workbench with provider/scope selection and batch webhook repair.
- `frontend-ng/src/features/repos/repo-detail-page.tsx` - add admin-only webhook repair alert/action and localize copy.
- `frontend-ng/src/features/auth/login-page.tsx`, `frontend-ng/src/features/oauth/oauth-pages.tsx`, `frontend-ng/src/features/events/events-page.tsx`, `frontend-ng/src/features/user-setup/user-page.tsx`, `frontend-ng/src/features/admin-users/admin-users-page.tsx`, `frontend-ng/src/features/settings/settings-page.tsx` - replace raw form/select/confirm/callout/empty markup with shadcn components and translation keys.
- `frontend-ng/src/components/ui/*` - add shadcn source components through the CLI, then review generated files.
- `docs/superpowers/specs/2026-06-05-frontend-ng-tanstack-start-migration-design.md` - update Current Implementation Snapshot and Acceptance Criteria notes after implementation.

Do not modify:

- Existing `frontend/` Vue implementation except as a read-only reference.
- Go backend behavior. The required backend endpoints already exist on merged `main`.
- Existing backend embedded frontend serving or deploy mainline cutover logic.

## Task 1: Install Shadcn And I18n Foundation Dependencies

**Files:**
- Modify: `frontend-ng/package.json`
- Modify: `frontend-ng/bun.lock`
- Create via CLI: `frontend-ng/src/components/ui/alert.tsx`
- Create via CLI: `frontend-ng/src/components/ui/alert-dialog.tsx`
- Create via CLI: `frontend-ng/src/components/ui/checkbox.tsx`
- Create via CLI: `frontend-ng/src/components/ui/empty.tsx`
- Create via CLI: `frontend-ng/src/components/ui/field.tsx`
- Create via CLI: `frontend-ng/src/components/ui/input-group.tsx`
- Create via CLI: `frontend-ng/src/components/ui/pagination.tsx`
- Create via CLI: `frontend-ng/src/components/ui/progress.tsx`
- Create via CLI: `frontend-ng/src/components/ui/select.tsx`
- Create via CLI: `frontend-ng/src/components/ui/separator.tsx`
- Create via CLI: `frontend-ng/src/components/ui/sheet.tsx`
- Create via CLI: `frontend-ng/src/components/ui/skeleton.tsx`
- Create via CLI: `frontend-ng/src/components/ui/spinner.tsx`
- Create via CLI: `frontend-ng/src/components/ui/tabs.tsx`
- Create via CLI: `frontend-ng/src/components/ui/toggle-group.tsx`
- Create via CLI: `frontend-ng/src/components/ui/tooltip.tsx`
- Create via CLI: `frontend-ng/src/components/ui/chart.tsx`

- [ ] **Step 1: Reconfirm project context**

Run:

```bash
cd frontend-ng && bunx --bun shadcn@latest info --json
```

Expected: JSON reports `frameworkName: "tanstack-start"`, `base: "radix"`, `style: "radix-nova"`, `tailwindVersion: "v4"`, `iconLibrary: "lucide"`.

- [ ] **Step 2: Install runtime i18n/chart dependencies**

Run:

```bash
cd frontend-ng && bun add i18next react-i18next recharts
```

Expected: `package.json` includes `i18next`, `react-i18next`, and `recharts`; `bun.lock` changes.

- [ ] **Step 3: Add missing official shadcn components**

Run:

```bash
cd frontend-ng && bunx --bun shadcn@latest add alert alert-dialog checkbox empty field input-group pagination progress select separator sheet skeleton spinner tabs toggle-group tooltip chart
```

Expected: files are added under `frontend-ng/src/components/ui/`; no existing custom files are deleted.

- [ ] **Step 4: Inspect generated UI files**

Run:

```bash
cd frontend-ng && rg "@/components/ui|lucide-react|data-slot|SelectGroup|TabsList|DialogTitle|AlertDialogTitle" src/components/ui -n
```

Expected:

- Imports use `@/components/ui` and `@/lib/utils`.
- Select files export `SelectGroup` and generated usage can support `SelectItem` inside `SelectGroup`.
- Tabs files export `TabsList`.
- AlertDialog files export `AlertDialogTitle`.
- Icons import from `lucide-react`.

- [ ] **Step 5: Run typecheck after dependency install**

Run:

```bash
cd frontend-ng && bun run check
```

Expected: PASS.

- [ ] **Step 6: Commit dependency and generated component baseline**

Run:

```bash
git add frontend-ng/package.json frontend-ng/bun.lock frontend-ng/src/components/ui
git commit -m "chore(frontend): add frontend-ng shadcn primitives"
```

Expected: commit created.

## Task 2: Add React I18n Base And Guards

**Files:**
- Create: `frontend-ng/src/lib/i18n/messages.ts`
- Create: `frontend-ng/src/lib/i18n/i18n.tsx`
- Create: `frontend-ng/src/lib/i18n/no-hardcoded-copy.test.ts`
- Modify: `frontend-ng/src/routes/__root.tsx`
- Modify: `frontend-ng/src/lib/format.ts`

- [ ] **Step 1: Write failing i18n behavior tests**

Create `frontend-ng/src/lib/i18n/no-hardcoded-copy.test.ts` with:

```ts
import { describe, expect, test } from 'vitest'
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { formatMessage, messages, supportedLocales } from './messages'

const ROOT = new URL('../../', import.meta.url).pathname
const allowedLiteralFiles = new Set([
  'lib/i18n/messages.ts',
  'lib/i18n/no-hardcoded-copy.test.ts',
  'lib/api/server.ts',
  'lib/auth/gateway.ts',
  'lib/auth/cookies.ts',
  'routeTree.gen.ts'
])

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const full = join(dir, name)
    if (name === 'node_modules' || name === '.output') return []
    if (statSync(full).isDirectory()) return walk(full)
    return /\.(tsx?|css)$/.test(name) ? [full] : []
  })
}

describe('frontend-ng i18n resources', () => {
  test('defines the same message keys for every locale', () => {
    const [first, ...rest] = supportedLocales
    const expected = Object.keys(messages[first]).sort()
    for (const locale of rest) {
      expect(Object.keys(messages[locale]).sort()).toEqual(expected)
    }
  })

  test('formats simple interpolation without leaking braces', () => {
    expect(formatMessage('en-US', 'common.pageCount', { current: 2, total: 5 })).toBe('Page 2 / 5')
    expect(formatMessage('zh-CN', 'common.pageCount', { current: 2, total: 5 })).toBe('第 2 / 5 页')
  })

  test('keeps Chinese copy only in the zh-CN resource table', () => {
    const offenders = walk(ROOT)
      .map((file) => relative(ROOT, file))
      .filter((file) => !allowedLiteralFiles.has(file))
      .filter((file) => /[\u3400-\u9fff]/.test(readFileSync(join(ROOT, file), 'utf8')))
    expect(offenders).toEqual([])
  })
})
```

- [ ] **Step 2: Run the new guard and verify it fails**

Run:

```bash
cd frontend-ng && bun test src/lib/i18n/no-hardcoded-copy.test.ts
```

Expected: FAIL because `messages.ts` does not exist.

- [ ] **Step 3: Create message resources**

Create `frontend-ng/src/lib/i18n/messages.ts` with the complete resource table required by the currently mounted `frontend-ng` routes. Seed route names and mainline capability copy from `frontend/src/i18n.ts` for `nav`, `home`, `usageDashboard`, `repos`, `repoDetail`, `events`, `user`, `adminUsers`, `settings`, `auth`, `oauth`, and `common`, then keep all future page copy in this file:

```ts
export const supportedLocales = ['en-US', 'zh-CN'] as const
export type Locale = (typeof supportedLocales)[number]
export type MessageKey = keyof typeof messages['en-US']

export const defaultLocale: Locale = 'en-US'

export const messages = {
  'en-US': {
    'common.loading': 'Loading...',
    'common.refresh': 'Refresh',
    'common.cancel': 'Cancel',
    'common.confirm': 'Confirm',
    'common.create': 'Create',
    'common.update': 'Update',
    'common.delete': 'Delete',
    'common.copy': 'Copy',
    'common.close': 'Close',
    'common.empty': 'No data available.',
    'common.pageCount': 'Page {current} / {total}',
    'app.title': 'AI Efficiency',
    'app.fullTitle': 'AI Efficiency Platform',
    'nav.myWorkSection': 'My Work',
    'nav.codeSection': 'Code & PR',
    'nav.adminSection': 'Administration',
    'nav.authSection': 'Auth',
    'nav.myUsage': 'My AI Usage',
    'nav.usageRecords': 'Usage Records',
    'nav.codeRepositories': 'Code Repositories',
    'nav.mySetup': 'My Setup',
    'nav.userManagement': 'Users & Access',
    'nav.adminConsole': 'Admin Console',
    'nav.languageToggle': '中文',
    'nav.logout': 'Sign out',
    'nav.openMenu': 'Open navigation',
    'nav.closeMenu': 'Close navigation',
    'nav.toggleTheme': 'Toggle theme',
    'auth.loadingAccount': 'Loading account...',
    'auth.guest': 'Guest',
    'auth.notSignedIn': 'not signed in',
    'usageDashboard.title': 'My AI Usage',
    'usageDashboard.embeddedTitle': 'My Usage',
    'usageDashboard.subtitle': 'Usage and cost from your configured AI relay account.',
    'usageDashboard.today': 'Today',
    'usageDashboard.sevenDays': '7 Days',
    'usageDashboard.thirtyDays': '30 Days',
    'usageDashboard.setupTitle': 'Complete AI service configuration',
    'usageDashboard.setupHelp': 'Usage data is available after your relay credentials are configured.',
    'usageDashboard.credentialError': 'Relay credentials need attention',
    'usageDashboard.unavailable': 'Usage dashboard is temporarily unavailable',
    'usageDashboard.retryHelp': 'Try refreshing after checking your setup.',
    'usageDashboard.rangeCost': '{range} Cost',
    'usageDashboard.rangeRequests': '{range} Requests',
    'usageDashboard.rangeTokens': '{range} Tokens',
    'usageDashboard.selectedRange': 'Selected range',
    'usageDashboard.avgResponse': 'Avg Response',
    'usageDashboard.standard': 'Standard',
    'usageDashboard.input': 'Input',
    'usageDashboard.output': 'Output',
    'usageDashboard.cacheCreation': 'Cache Creation',
    'usageDashboard.cacheRead': 'Cache Read',
    'usageDashboard.actual': 'Actual',
    'usageDashboard.tokenTrend': 'Token Trend',
    'usageDashboard.modelDistribution': 'Model Distribution',
    'usageDashboard.noTrendData': 'No trend data available',
    'usageDashboard.noModelData': 'No model data available',
    'repos.title': 'Repositories',
    'repos.subtitle': 'Repository health, SCM binding state, and PR usage freshness come from the Go backend APIs.',
    'repos.health': 'Repository Health',
    'repos.healthHelp': 'Inventory is grouped by code platform and scope.',
    'repos.allBindings': 'All bindings',
    'repos.bound': 'Bound',
    'repos.unbound': 'Needs binding',
    'repos.addRepo': 'Add repository',
    'repos.autoBind': 'Auto-bind unbound',
    'repos.autoBinding': 'Auto-binding...',
    'repos.autoBindComplete': 'Auto-bind complete',
    'repos.autoBindFailed': 'Auto-bind failed',
    'repos.autoBindSummary': '{bound} bound · {noMatch} no match · {ambiguous} ambiguous · {webhookFailed} webhook failed · {errors} errors',
    'repos.repairWebhooks': 'Repair failed webhooks',
    'repos.webhookRepairing': 'Repairing webhooks...',
    'repos.webhookRepairComplete': 'Webhook repair complete',
    'repos.webhookRepairFailed': 'Webhook repair failed',
    'repos.webhookRepairSummary': '{repaired} repaired · {alreadyRegistered} already registered · {failed} failed',
    'repos.reviewNeedsBinding': 'Review needs binding',
    'repos.totalRepositories': 'Total repositories',
    'repos.boundRepositories': 'Bound repositories',
    'repos.activeConfigs': 'Active configs',
    'repos.platformSection': 'Code Platforms',
    'repos.scopeSection': 'Scopes',
    'repos.scopeSearch': 'Search scopes',
    'repos.scopeCount': '{count} repos',
    'repos.repositoriesInScope': 'repositories in scope',
    'repos.selectedScope': 'Selected scope',
    'repos.scopedEmpty': 'No repositories in this scope.',
    'repoDetail.repairWebhook': 'Repair webhook',
    'repoDetail.webhookRepairing': 'Repairing webhook...',
    'repoDetail.webhookRepairNeeded': 'Webhook is missing or failed for this bound repository.',
    'repoDetail.forceReplaceWebhook': 'Force replace existing webhook',
    'repoDetail.webhookRepaired': 'Webhook repaired',
    'repoDetail.webhookRepairComplete': 'Webhook repair complete',
    'repoDetail.webhookRepairFailed': 'Webhook repair failed'
  },
  'zh-CN': {
    'common.loading': '加载中...',
    'common.refresh': '刷新',
    'common.cancel': '取消',
    'common.confirm': '确认',
    'common.create': '创建',
    'common.update': '更新',
    'common.delete': '删除',
    'common.copy': '复制',
    'common.close': '关闭',
    'common.empty': '暂无数据。',
    'common.pageCount': '第 {current} / {total} 页',
    'app.title': 'AI Efficiency',
    'app.fullTitle': 'AI 效能平台',
    'nav.myWorkSection': '我的工作',
    'nav.codeSection': '代码与 PR',
    'nav.adminSection': '管理',
    'nav.authSection': '授权',
    'nav.myUsage': '我的 AI 用量',
    'nav.usageRecords': '用量记录',
    'nav.codeRepositories': '代码仓库',
    'nav.mySetup': '我的接入',
    'nav.userManagement': '用户与权限',
    'nav.adminConsole': '管理控制台',
    'nav.languageToggle': 'English',
    'nav.logout': '退出登录',
    'nav.openMenu': '打开导航',
    'nav.closeMenu': '关闭导航',
    'nav.toggleTheme': '切换主题',
    'auth.loadingAccount': '正在加载账号...',
    'auth.guest': '访客',
    'auth.notSignedIn': '未登录',
    'usageDashboard.title': '我的 AI 用量',
    'usageDashboard.embeddedTitle': '我的用量',
    'usageDashboard.subtitle': '查看你已配置 AI relay 账号的用量和费用。',
    'usageDashboard.today': '今天',
    'usageDashboard.sevenDays': '7 天',
    'usageDashboard.thirtyDays': '30 天',
    'usageDashboard.setupTitle': '完成 AI 服务配置',
    'usageDashboard.setupHelp': '配置 relay 凭据后即可查看用量数据。',
    'usageDashboard.credentialError': 'Relay 凭据需要处理',
    'usageDashboard.unavailable': '用量概览暂时不可用',
    'usageDashboard.retryHelp': '检查接入配置后再刷新。',
    'usageDashboard.rangeCost': '{range}费用',
    'usageDashboard.rangeRequests': '{range}请求',
    'usageDashboard.rangeTokens': '{range} Token',
    'usageDashboard.selectedRange': '当前范围',
    'usageDashboard.avgResponse': '平均响应',
    'usageDashboard.standard': '标准计费',
    'usageDashboard.input': '输入',
    'usageDashboard.output': '输出',
    'usageDashboard.cacheCreation': '缓存写入',
    'usageDashboard.cacheRead': '缓存读取',
    'usageDashboard.actual': '实际',
    'usageDashboard.tokenTrend': 'Token 趋势',
    'usageDashboard.modelDistribution': '模型分布',
    'usageDashboard.noTrendData': '暂无趋势数据',
    'usageDashboard.noModelData': '暂无模型数据',
    'repos.title': '代码仓库',
    'repos.subtitle': '仓库健康状态、SCM 绑定和 PR 用量新鲜度来自 Go backend API。',
    'repos.health': '仓库健康状态',
    'repos.healthHelp': '按代码平台和范围组织仓库清单。',
    'repos.allBindings': '全部绑定',
    'repos.bound': '已绑定',
    'repos.unbound': '需要绑定',
    'repos.addRepo': '添加仓库',
    'repos.autoBind': '自动绑定未绑定仓库',
    'repos.autoBinding': '正在自动绑定...',
    'repos.autoBindComplete': '自动绑定完成',
    'repos.autoBindFailed': '自动绑定失败',
    'repos.autoBindSummary': '已绑定 {bound} · 未匹配 {noMatch} · 多重匹配 {ambiguous} · Webhook 失败 {webhookFailed} · 错误 {errors}',
    'repos.repairWebhooks': '修复失败的 Webhook',
    'repos.webhookRepairing': 'Webhook 修复中...',
    'repos.webhookRepairComplete': 'Webhook 修复完成',
    'repos.webhookRepairFailed': 'Webhook 修复失败',
    'repos.webhookRepairSummary': '已修复 {repaired} · 已存在 {alreadyRegistered} · 失败 {failed}',
    'repos.reviewNeedsBinding': '查看需要绑定',
    'repos.totalRepositories': '仓库总数',
    'repos.boundRepositories': '已绑定仓库',
    'repos.activeConfigs': '活跃配置',
    'repos.platformSection': '代码平台',
    'repos.scopeSection': '范围',
    'repos.scopeSearch': '搜索范围',
    'repos.scopeCount': '{count} 个仓库',
    'repos.repositoriesInScope': '范围内仓库',
    'repos.selectedScope': '当前范围',
    'repos.scopedEmpty': '此范围内暂无仓库。',
    'repoDetail.repairWebhook': '修复 Webhook',
    'repoDetail.webhookRepairing': 'Webhook 修复中...',
    'repoDetail.webhookRepairNeeded': '此已绑定仓库的 Webhook 缺失或注册失败。',
    'repoDetail.forceReplaceWebhook': '强制替换已有 Webhook',
    'repoDetail.webhookRepaired': 'Webhook 已修复',
    'repoDetail.webhookRepairComplete': 'Webhook 修复完成',
    'repoDetail.webhookRepairFailed': 'Webhook 修复失败'
  }
} as const

export function isLocale(value: string | null | undefined): value is Locale {
  return supportedLocales.includes(value as Locale)
}

export function formatMessage(locale: Locale, key: MessageKey, values: Record<string, string | number> = {}) {
  const template = messages[locale][key] || messages[defaultLocale][key] || key
  return template.replace(/\{(\w+)\}/g, (_, name) => String(values[name] ?? `{${name}}`))
}
```

- [ ] **Step 4: Create React provider**

Create `frontend-ng/src/lib/i18n/i18n.tsx`:

```tsx
import i18next from 'i18next'
import { I18nextProvider, initReactI18next, useTranslation } from 'react-i18next'
import { createContext, useContext, useEffect, useMemo, useState } from 'react'
import { defaultLocale, isLocale, messages, type Locale, type MessageKey } from './messages'

const STORAGE_KEY = 'ae.locale'

void i18next.use(initReactI18next).init({
  lng: defaultLocale,
  fallbackLng: defaultLocale,
  resources: Object.fromEntries(
    Object.entries(messages).map(([locale, table]) => [locale, { translation: table }])
  ),
  interpolation: { escapeValue: false }
})

const LocaleContext = createContext<{ locale: Locale; setLocale: (locale: Locale) => void }>({
  locale: defaultLocale,
  setLocale: () => undefined
})

export function I18nProvider({ children }: { children: React.ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(() => {
    if (typeof window === 'undefined') return defaultLocale
    const saved = window.localStorage.getItem(STORAGE_KEY)
    return isLocale(saved) ? saved : defaultLocale
  })

  useEffect(() => {
    void i18next.changeLanguage(locale)
    document.documentElement.lang = locale
    if (typeof window !== 'undefined') window.localStorage.setItem(STORAGE_KEY, locale)
  }, [locale])

  const value = useMemo(() => ({ locale, setLocale: setLocaleState }), [locale])
  return (
    <LocaleContext.Provider value={value}>
      <I18nextProvider i18n={i18next}>{children}</I18nextProvider>
    </LocaleContext.Provider>
  )
}

export function useI18n() {
  const { locale, setLocale } = useContext(LocaleContext)
  const { t: rawT } = useTranslation()
  const t = (key: MessageKey, values?: Record<string, string | number>) => rawT(key, values)
  const toggleLocale = () => setLocale(locale === 'en-US' ? 'zh-CN' : 'en-US')
  return { locale, setLocale, toggleLocale, t }
}

export { i18next }
```

- [ ] **Step 5: Wrap root and set translated loading copy**

Modify `frontend-ng/src/routes/__root.tsx`:

```tsx
import { I18nProvider, useI18n } from '@/lib/i18n/i18n'
```

Wrap the existing `QueryClientProvider` body:

```tsx
<QueryClientProvider client={queryClient}>
  <I18nProvider>
    <AuthFrame>
      <Outlet />
    </AuthFrame>
    <Toaster />
  </I18nProvider>
</QueryClientProvider>
```

Inside `AuthFrame`, add:

```tsx
const { t } = useI18n()
```

Replace:

```tsx
<LoadingState label='Loading account...' />
```

with:

```tsx
<LoadingState label={t('auth.loadingAccount')} />
```

- [ ] **Step 6: Make format helpers locale-aware**

Modify `frontend-ng/src/lib/format.ts`:

```ts
export function number(value: number | undefined | null, locale = 'en-US') {
  if (value == null || Number.isNaN(value)) return '-'
  return new Intl.NumberFormat(locale).format(value)
}

export function compact(value: number | undefined | null, locale = 'en-US') {
  if (value == null || Number.isNaN(value)) return '-'
  return new Intl.NumberFormat(locale, { notation: 'compact', maximumFractionDigits: 1 }).format(value)
}

export function currency(value: number | undefined | null, locale = 'en-US') {
  if (value == null || Number.isNaN(value)) return '-'
  return new Intl.NumberFormat(locale, { style: 'currency', currency: 'USD', maximumFractionDigits: 4 }).format(value)
}

export function durationMs(value: number | undefined | null, locale = 'en-US') {
  if (value == null || Number.isNaN(value)) return '-'
  if (value < 1000) return `${number(Math.round(value), locale)} ms`
  return `${number(Number((value / 1000).toFixed(2)), locale)} s`
}

export function dateTime(value: string | undefined | null, locale = 'en-US') {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleString(locale)
}

export function percent(value: number | undefined | null, locale = 'en-US') {
  if (value == null || Number.isNaN(value)) return '-'
  return new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 0 }).format(value)
}
```

- [ ] **Step 7: Run tests**

Run:

```bash
cd frontend-ng && bun test src/lib/i18n/no-hardcoded-copy.test.ts && bun run check
```

Expected: PASS.

- [ ] **Step 8: Commit i18n foundation**

Run:

```bash
git add frontend-ng/src/lib/i18n frontend-ng/src/routes/__root.tsx frontend-ng/src/lib/format.ts frontend-ng/package.json frontend-ng/bun.lock
git commit -m "feat(frontend): add frontend-ng i18n foundation"
```

Expected: commit created.

## Task 3: Add Mainline API Types And Client Methods

**Files:**
- Modify: `frontend-ng/src/lib/api/types.ts`
- Modify: `frontend-ng/src/lib/api/index.ts`
- Modify: `frontend-ng/src/features/repos/repos-state.test.ts`
- Modify: `frontend-ng/src/features/repos/repos-state.ts`

- [ ] **Step 1: Write failing API helper tests for inventory and webhook summaries**

Append to `frontend-ng/src/features/repos/repos-state.test.ts`:

```ts
import type { RepoInventoryProviderSummary, RepoWebhookRepairBatchResult, RepoWebhookRepairItem } from '@/lib/api/types'
import {
  compareInventoryProviders,
  firstScope,
  repoRepairMessage,
  webhookRepairBatchMessage,
  canRepairWebhook
} from './repos-state'

test('sorts inventory providers with unbound last and stable platform priority', () => {
  const rows: RepoInventoryProviderSummary[] = [
    { provider_key: 'unbound', name: 'Unbound', type: 'unbound', total_repos: 1, bound_repos: 0, unbound_repos: 1, active_repos: 0, webhook_failed_repos: 0, scopes: [] },
    { provider_key: 'bb', provider_id: 2, name: 'Bitbucket', type: 'bitbucket_server', total_repos: 2, bound_repos: 2, unbound_repos: 0, active_repos: 2, webhook_failed_repos: 1, scopes: [] },
    { provider_key: 'gh', provider_id: 1, name: 'GitHub', type: 'github', total_repos: 3, bound_repos: 3, unbound_repos: 0, active_repos: 3, webhook_failed_repos: 0, scopes: [] }
  ]
  expect([...rows].sort(compareInventoryProviders).map((row) => row.provider_key)).toEqual(['gh', 'bb', 'unbound'])
})

test('reads first scope and summarizes webhook repair results', () => {
  const provider: RepoInventoryProviderSummary = {
    provider_key: 'gh',
    provider_id: 1,
    name: 'GitHub',
    type: 'github',
    total_repos: 2,
    bound_repos: 2,
    unbound_repos: 0,
    active_repos: 1,
    webhook_failed_repos: 1,
    scopes: [{ scope: 'org', total_repos: 2, bound_repos: 2, unbound_repos: 0, active_repos: 1, webhook_failed_repos: 1 }]
  }
  expect(firstScope(provider)).toBe('org')

  const batch: RepoWebhookRepairBatchResult = {
    summary: { scanned: 3, repaired: 1, already_registered: 1, failed: 1 },
    items: []
  }
  expect(webhookRepairBatchMessage(batch)).toEqual({ repaired: 1, alreadyRegistered: 1, failed: 1 })
})

test('classifies repo detail webhook repair eligibility and result', () => {
  expect(canRepairWebhook({ role: 'admin', bindingState: 'bound', status: 'webhook_failed', webhookId: 'old' })).toBe(true)
  expect(canRepairWebhook({ role: 'admin', bindingState: 'bound', status: 'active', webhookId: '' })).toBe(true)
  expect(canRepairWebhook({ role: 'user', bindingState: 'bound', status: 'webhook_failed', webhookId: 'old' })).toBe(false)
  expect(canRepairWebhook({ role: 'admin', bindingState: 'unbound', status: 'webhook_failed', webhookId: 'old' })).toBe(false)

  const failed: RepoWebhookRepairItem = {
    repo_config_id: 9,
    full_name: 'org/repo',
    previous_status: 'webhook_failed',
    status: 'webhook_failed',
    webhook_status: 'failed',
    error: 'bitbucket API returned 502'
  }
  expect(repoRepairMessage(failed)).toEqual({ kind: 'error', error: 'bitbucket API returned 502' })
})
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
cd frontend-ng && bun test src/features/repos/repos-state.test.ts
```

Expected: FAIL because new types and helper functions do not exist.

- [ ] **Step 3: Add API types**

Add to `frontend-ng/src/lib/api/types.ts` near `RepoConfig`:

```ts
export interface RepoInventoryScopeSummary {
  scope: string
  total_repos: number
  bound_repos: number
  unbound_repos: number
  active_repos: number
  webhook_failed_repos: number
}

export interface RepoInventoryProviderSummary {
  provider_key: string
  provider_id?: number
  name: string
  type: string
  base_url?: string
  total_repos: number
  bound_repos: number
  unbound_repos: number
  active_repos: number
  webhook_failed_repos: number
  scopes: RepoInventoryScopeSummary[]
}

export interface RepoListParams {
  page?: number
  pageSize?: number
  scmProviderId?: number
  status?: string
  groupId?: string
  scope?: string
  bindingState?: 'bound' | 'unbound'
}

export interface RepoWebhookRepairRequest {
  force: boolean
}

export interface RepoWebhookRepairSummary {
  scanned: number
  repaired: number
  already_registered: number
  failed: number
}

export interface RepoWebhookRepairItem {
  repo_config_id: number
  full_name: string
  previous_status: string
  status: string
  webhook_status: 'registered' | 'already_registered' | 'failed'
  webhook_id?: string
  callback_url?: string
  error?: string
}

export interface RepoWebhookRepairBatchResult {
  summary: RepoWebhookRepairSummary
  items: RepoWebhookRepairItem[]
}
```

Update `RepoConfig`:

```ts
  webhook_id?: string | null
```

Add user usage types near user provider types:

```ts
export interface UserUsageDashboardParams {
  start_date?: string
  end_date?: string
  granularity?: 'day' | 'hour'
  timezone?: string
}

export interface UserUsageDashboardRange {
  start_date: string
  end_date: string
  granularity: 'day' | 'hour' | string
  timezone?: string
}

export interface UserUsageDashboardStats {
  total_requests: number
  total_input_tokens: number
  total_output_tokens: number
  total_cache_creation_tokens: number
  total_cache_read_tokens: number
  total_tokens: number
  total_cost: number
  total_actual_cost: number
  today_requests: number
  today_input_tokens: number
  today_output_tokens: number
  today_cache_creation_tokens: number
  today_cache_read_tokens: number
  today_tokens: number
  today_cost: number
  today_actual_cost: number
  average_duration_ms: number
  rpm: number
  tpm: number
}

export interface UserUsageTrendPoint {
  date: string
  requests: number
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  total_tokens: number
  cost: number
  actual_cost: number
}

export interface UserUsageModelStat {
  model: string
  requests: number
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  total_tokens: number
  cost: number
  actual_cost: number
}

export interface UserUsageDashboardSnapshot {
  configured: boolean
  range: UserUsageDashboardRange
  stats: UserUsageDashboardStats | null
  trend: UserUsageTrendPoint[]
  models: UserUsageModelStat[]
}
```

- [ ] **Step 4: Add API methods**

Modify imports in `frontend-ng/src/lib/api/index.ts` to include the new types, then update `api`:

```ts
  userUsage: {
    dashboard: (params?: UserUsageDashboardParams) =>
      apiFetch<UserUsageDashboardSnapshot>(`/user/usage/dashboard${encodeQuery(params)}`)
  },
  repos: {
    list: (paramsOrPage: RepoListParams | number = 1, pageSize = 20) => {
      const params = typeof paramsOrPage === 'number'
        ? { page: paramsOrPage, page_size: pageSize }
        : {
            page: paramsOrPage.page ?? 1,
            page_size: paramsOrPage.pageSize ?? 20,
            scm_provider_id: paramsOrPage.scmProviderId,
            status: paramsOrPage.status,
            group_id: paramsOrPage.groupId,
            scope: paramsOrPage.scope,
            binding_state: paramsOrPage.bindingState
          }
      return apiFetch<PagedResponse<RepoConfig>>(`/repos${encodeQuery(params)}`)
    },
    inventory: () => apiFetch<RepoInventoryProviderSummary[]>('/repos/inventory'),
    repairFailedWebhooks: (data: RepoWebhookRepairRequest = { force: false }) =>
      apiFetch<RepoWebhookRepairBatchResult>('/repos/repair-webhooks', { method: 'POST', body: JSON.stringify(data) }),
    repairWebhook: (id: number, data: RepoWebhookRepairRequest = { force: false }) =>
      apiFetch<RepoWebhookRepairItem>(`/repos/${id}/repair-webhook`, { method: 'POST', body: JSON.stringify(data) }),
```

Keep existing `get`, `createDirect`, `autoBindUnbound`, `update`, `delete`, `prs`, `syncPRs`, and `latestPRSyncJob` methods.

- [ ] **Step 5: Add repo helper implementations**

Append to `frontend-ng/src/features/repos/repos-state.ts`:

```ts
import type {
  RepoInventoryProviderSummary,
  RepoWebhookRepairBatchResult,
  RepoWebhookRepairItem
} from '@/lib/api/types'

export function compareInventoryProviders(a: RepoInventoryProviderSummary, b: RepoInventoryProviderSummary) {
  if (a.provider_key === 'unbound') return 1
  if (b.provider_key === 'unbound') return -1
  const priority = (provider: RepoInventoryProviderSummary) => {
    if (provider.type === 'github') return 0
    if (provider.type === 'bitbucket_server' || provider.type === 'bitbucket') return 1
    return 2
  }
  return priority(a) - priority(b) || a.name.localeCompare(b.name) || a.provider_key.localeCompare(b.provider_key)
}

export function firstScope(provider: RepoInventoryProviderSummary | null | undefined) {
  return provider?.scopes[0]?.scope ?? ''
}

export function webhookRepairBatchMessage(result: RepoWebhookRepairBatchResult) {
  return {
    repaired: result.summary.repaired,
    alreadyRegistered: result.summary.already_registered,
    failed: result.summary.failed
  }
}

export function canRepairWebhook(state: {
  role?: string
  bindingState?: 'bound' | 'unbound'
  status?: string
  webhookId?: string | null
}) {
  return state.role === 'admin'
    && state.bindingState === 'bound'
    && (state.status === 'webhook_failed' || !state.webhookId)
}

export function repoRepairMessage(item: RepoWebhookRepairItem): { kind: 'success' | 'error'; error?: string } {
  if (item.webhook_status === 'failed' || item.status === 'webhook_failed' || item.error) {
    return { kind: 'error', error: item.error || 'Webhook repair failed' }
  }
  return { kind: 'success' }
}
```

- [ ] **Step 6: Run tests and typecheck**

Run:

```bash
cd frontend-ng && bun test src/features/repos/repos-state.test.ts && bun run check
```

Expected: PASS.

- [ ] **Step 7: Commit API parity contracts**

Run:

```bash
git add frontend-ng/src/lib/api/types.ts frontend-ng/src/lib/api/index.ts frontend-ng/src/features/repos/repos-state.ts frontend-ng/src/features/repos/repos-state.test.ts
git commit -m "feat(frontend): add frontend-ng mainline repo contracts"
```

Expected: commit created.

## Task 4: Implement Personal Usage Dashboard On Home

**Files:**
- Create: `frontend-ng/src/features/user-usage/user-usage-state.ts`
- Create: `frontend-ng/src/features/user-usage/user-usage-state.test.ts`
- Create: `frontend-ng/src/features/user-usage/user-usage-panel.tsx`
- Modify: `frontend-ng/src/features/home/home-page.tsx`
- Modify: `frontend-ng/src/components/layout/navigation.ts`

- [ ] **Step 1: Write failing range helper tests**

Create `frontend-ng/src/features/user-usage/user-usage-state.test.ts`:

```ts
import { describe, expect, test } from 'vitest'
import { buildUsageDashboardParams, rangeLabelKey, usageTotalsFromTrend } from './user-usage-state'

describe('user usage state', () => {
  test('builds today as hourly range using local date', () => {
    const params = buildUsageDashboardParams('today', new Date('2026-06-09T10:00:00+08:00'), 'Asia/Shanghai')
    expect(params).toEqual({
      start_date: '2026-06-09',
      end_date: '2026-06-09',
      granularity: 'hour',
      timezone: 'Asia/Shanghai'
    })
  })

  test('builds 7 day and 30 day inclusive day ranges', () => {
    expect(buildUsageDashboardParams('7d', new Date('2026-06-09T10:00:00+08:00'), 'Asia/Shanghai')).toMatchObject({
      start_date: '2026-06-03',
      end_date: '2026-06-09',
      granularity: 'day'
    })
    expect(buildUsageDashboardParams('30d', new Date('2026-06-09T10:00:00+08:00'), 'Asia/Shanghai')).toMatchObject({
      start_date: '2026-05-11',
      end_date: '2026-06-09',
      granularity: 'day'
    })
  })

  test('maps range label keys and sums trend data', () => {
    expect(rangeLabelKey('today')).toBe('usageDashboard.today')
    expect(usageTotalsFromTrend([
      { date: '2026-06-08', requests: 2, input_tokens: 10, output_tokens: 5, cache_creation_tokens: 1, cache_read_tokens: 2, total_tokens: 18, cost: 0.2, actual_cost: 0.1 },
      { date: '2026-06-09', requests: 3, input_tokens: 20, output_tokens: 6, cache_creation_tokens: 2, cache_read_tokens: 3, total_tokens: 31, cost: 0.3, actual_cost: 0.2 }
    ])).toEqual({ requests: 5, tokens: 49, actualCost: 0.30000000000000004, standardCost: 0.5 })
  })
})
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
cd frontend-ng && bun test src/features/user-usage/user-usage-state.test.ts
```

Expected: FAIL because helper file does not exist.

- [ ] **Step 3: Implement usage helpers**

Create `frontend-ng/src/features/user-usage/user-usage-state.ts`:

```ts
import type { MessageKey } from '@/lib/i18n/messages'
import type { UserUsageDashboardParams, UserUsageTrendPoint } from '@/lib/api/types'

export type UsageRangeOption = 'today' | '7d' | '30d'

function formatDate(date: Date) {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

export function buildUsageDashboardParams(range: UsageRangeOption, now = new Date(), timezone = Intl.DateTimeFormat().resolvedOptions().timeZone): UserUsageDashboardParams {
  const end = new Date(now)
  const start = new Date(now)
  if (range === 'today') {
    return { start_date: formatDate(start), end_date: formatDate(end), granularity: 'hour', timezone }
  }
  start.setDate(end.getDate() - (range === '7d' ? 6 : 29))
  return { start_date: formatDate(start), end_date: formatDate(end), granularity: 'day', timezone }
}

export function rangeLabelKey(range: UsageRangeOption): MessageKey {
  if (range === 'today') return 'usageDashboard.today'
  if (range === '7d') return 'usageDashboard.sevenDays'
  return 'usageDashboard.thirtyDays'
}

export function usageTotalsFromTrend(points: UserUsageTrendPoint[]) {
  return points.reduce(
    (next, point) => ({
      requests: next.requests + point.requests,
      tokens: next.tokens + point.total_tokens,
      actualCost: next.actualCost + point.actual_cost,
      standardCost: next.standardCost + point.cost
    }),
    { requests: 0, tokens: 0, actualCost: 0, standardCost: 0 }
  )
}
```

- [ ] **Step 4: Implement usage panel with shadcn components**

Create `frontend-ng/src/features/user-usage/user-usage-panel.tsx` with:

```tsx
import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { MetricCard } from '@/components/primitives/metric-card'
import { api } from '@/lib/api'
import { compact, currency, durationMs, number } from '@/lib/format'
import { useI18n } from '@/lib/i18n/i18n'
import { buildUsageDashboardParams, rangeLabelKey, type UsageRangeOption } from './user-usage-state'
import { Area, AreaChart, Bar, BarChart, CartesianGrid, XAxis, YAxis } from 'recharts'

export function UserUsagePanel({ embedded = false }: { embedded?: boolean }) {
  const { locale, t } = useI18n()
  const [range, setRange] = useState<UsageRangeOption>('7d')
  const query = useQuery({
    queryKey: ['user-usage-dashboard', range],
    queryFn: () => api.userUsage.dashboard(buildUsageDashboardParams(range))
  })
  const snapshot = query.data
  const rangeLabel = t(rangeLabelKey(range))

  return (
    <Card>
      <CardHeader className='flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between'>
        <div>
          <CardTitle>{embedded ? t('usageDashboard.embeddedTitle') : t('usageDashboard.title')}</CardTitle>
          <CardDescription>{t('usageDashboard.subtitle')}</CardDescription>
        </div>
        <div className='flex items-center gap-2'>
          <ToggleGroup type='single' value={range} onValueChange={(value) => value && setRange(value as UsageRangeOption)}>
            <ToggleGroupItem value='today'>{t('usageDashboard.today')}</ToggleGroupItem>
            <ToggleGroupItem value='7d'>{t('usageDashboard.sevenDays')}</ToggleGroupItem>
            <ToggleGroupItem value='30d'>{t('usageDashboard.thirtyDays')}</ToggleGroupItem>
          </ToggleGroup>
          <Button variant='outline' disabled={query.isFetching} onClick={() => void query.refetch()}>
            {t('common.refresh')}
          </Button>
        </div>
      </CardHeader>
      <CardContent className='flex flex-col gap-4'>
        {query.isLoading ? <div className='text-muted-foreground text-sm'>{t('common.loading')}</div> : null}
        {snapshot?.configured === false ? (
          <Alert>
            <AlertTitle>{t('usageDashboard.setupTitle')}</AlertTitle>
            <AlertDescription>{t('usageDashboard.setupHelp')}</AlertDescription>
            <Button asChild className='mt-3' size='sm'><Link to='/user'>{t('usageDashboard.openSetup')}</Link></Button>
          </Alert>
        ) : null}
        {query.error ? (
          <Alert variant='destructive'>
            <AlertTitle>{query.error.message.includes('409') ? t('usageDashboard.credentialError') : t('usageDashboard.unavailable')}</AlertTitle>
            <AlertDescription>{t('usageDashboard.retryHelp')}</AlertDescription>
          </Alert>
        ) : null}
        {snapshot?.configured !== false && snapshot ? (
          <>
            <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-4'>
              <MetricCard label={t('usageDashboard.rangeCost', { range: rangeLabel })} value={currency(snapshot.stats?.total_actual_cost ?? 0, locale)} helper={`${t('usageDashboard.standard')}: ${currency(snapshot.stats?.total_cost ?? 0, locale)}`} />
              <MetricCard label={t('usageDashboard.rangeRequests', { range: rangeLabel })} value={number(snapshot.stats?.total_requests ?? 0, locale)} helper={t('usageDashboard.selectedRange')} />
              <MetricCard label={t('usageDashboard.rangeTokens', { range: rangeLabel })} value={compact(snapshot.stats?.total_tokens ?? 0, locale)} />
              <MetricCard label={t('usageDashboard.avgResponse')} value={durationMs(snapshot.stats?.average_duration_ms ?? 0, locale)} helper={`RPM ${compact(snapshot.stats?.rpm ?? 0, locale)} · TPM ${compact(snapshot.stats?.tpm ?? 0, locale)}`} />
            </div>
            <div className='grid gap-4 xl:grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)]'>
              <Card>
                <CardHeader><CardTitle>{t('usageDashboard.tokenTrend')}</CardTitle></CardHeader>
                <CardContent>
                  {snapshot.trend.length ? (
                    <ChartContainer config={{ input: { label: t('usageDashboard.input') }, output: { label: t('usageDashboard.output') } }} className='h-64'>
                      <AreaChart data={snapshot.trend}>
                        <CartesianGrid vertical={false} />
                        <XAxis dataKey='date' tickLine={false} axisLine={false} />
                        <YAxis tickLine={false} axisLine={false} />
                        <ChartTooltip content={<ChartTooltipContent />} />
                        <Area type='monotone' dataKey='input_tokens' stackId='tokens' fill='var(--chart-1)' stroke='var(--chart-1)' />
                        <Area type='monotone' dataKey='output_tokens' stackId='tokens' fill='var(--chart-2)' stroke='var(--chart-2)' />
                      </AreaChart>
                    </ChartContainer>
                  ) : (
                    <Empty><EmptyHeader><EmptyTitle>{t('usageDashboard.noTrendData')}</EmptyTitle></EmptyHeader></Empty>
                  )}
                </CardContent>
              </Card>
              <Card>
                <CardHeader><CardTitle>{t('usageDashboard.modelDistribution')}</CardTitle></CardHeader>
                <CardContent>
                  {snapshot.models.length ? (
                    <ChartContainer config={{ tokens: { label: t('usageDashboard.tokens') } }} className='h-64'>
                      <BarChart data={snapshot.models}>
                        <CartesianGrid vertical={false} />
                        <XAxis dataKey='model' tickLine={false} axisLine={false} />
                        <YAxis tickLine={false} axisLine={false} />
                        <ChartTooltip content={<ChartTooltipContent />} />
                        <Bar dataKey='total_tokens' fill='var(--chart-3)' radius={4} />
                      </BarChart>
                    </ChartContainer>
                  ) : (
                    <Empty>
                      <EmptyHeader><EmptyTitle>{t('usageDashboard.noModelData')}</EmptyTitle></EmptyHeader>
                      <EmptyContent />
                    </Empty>
                  )}
                </CardContent>
              </Card>
            </div>
          </>
        ) : null}
      </CardContent>
    </Card>
  )
}
```

- [ ] **Step 5: Embed dashboard on home**

Modify `frontend-ng/src/features/home/home-page.tsx`:

```tsx
import { UserUsagePanel } from '@/features/user-usage/user-usage-panel'
import { useI18n } from '@/lib/i18n/i18n'
```

Inside `HomePage`, add:

```tsx
const { t, locale } = useI18n()
```

Replace the top hero title/copy with translated keys and insert the usage panel after the hero card:

```tsx
<UserUsagePanel embedded />
```

Update existing `number(...)`, `compact(...)`, and `dateTime(...)` calls to pass `locale`.

- [ ] **Step 6: Run focused tests**

Run:

```bash
cd frontend-ng && bun test src/features/user-usage/user-usage-state.test.ts && bun run check
```

Expected: PASS.

- [ ] **Step 7: Commit personal usage dashboard**

Run:

```bash
git add frontend-ng/src/features/user-usage frontend-ng/src/features/home/home-page.tsx frontend-ng/src/components/layout/navigation.ts
git commit -m "feat(frontend): add frontend-ng usage dashboard"
```

Expected: commit created.

## Task 5: Align Repos Page With Inventory Workbench And Batch Webhook Repair

**Files:**
- Modify: `frontend-ng/src/features/repos/repos-state.ts`
- Modify: `frontend-ng/src/features/repos/repos-state.test.ts`
- Modify: `frontend-ng/src/features/repos/repos-page.tsx`

- [ ] **Step 1: Write failing search serialization tests**

Append to `frontend-ng/src/features/repos/repos-state.test.ts`:

```ts
import { buildRepoListParams, buildRepoSearch, parseRepoSearch } from './repos-state'

test('parses and serializes repo workbench URL state', () => {
  expect(parseRepoSearch({ binding: 'unbound', provider: 'gh', scope: 'org', page: '2', page_size: '50' })).toEqual({
    binding: 'unbound',
    provider: 'gh',
    scope: 'org',
    page: 2,
    pageSize: 50
  })
  expect(parseRepoSearch({ binding: 'bad', page: '-1', page_size: 'NaN' })).toEqual({
    binding: 'all',
    provider: '',
    scope: '',
    page: 1,
    pageSize: 20
  })
  expect(buildRepoSearch({ binding: 'all', provider: '', scope: '', page: 1, pageSize: 20 })).toEqual({})
  expect(buildRepoSearch({ binding: 'bound', provider: 'gh', scope: 'org', page: 3, pageSize: 100 })).toEqual({
    binding: 'bound',
    provider: 'gh',
    scope: 'org',
    page: '3',
    page_size: '100'
  })
})

test('builds repo list params from selected inventory provider and scope', () => {
  const provider: RepoInventoryProviderSummary = {
    provider_key: 'gh',
    provider_id: 1,
    name: 'GitHub',
    type: 'github',
    total_repos: 3,
    bound_repos: 3,
    unbound_repos: 0,
    active_repos: 2,
    webhook_failed_repos: 1,
    scopes: []
  }
  expect(buildRepoListParams({ provider, scope: 'org', binding: 'bound', page: 2, pageSize: 50 })).toEqual({
    page: 2,
    pageSize: 50,
    scmProviderId: 1,
    bindingState: 'bound',
    scope: 'org'
  })
  expect(buildRepoListParams({ provider: { ...provider, provider_key: 'unbound', provider_id: undefined }, scope: 'unknown', binding: 'all', page: 1, pageSize: 20 })).toMatchObject({
    bindingState: 'unbound'
  })
})
```

- [ ] **Step 2: Implement URL/list helper functions**

Append to `frontend-ng/src/features/repos/repos-state.ts`:

```ts
export interface RepoWorkbenchSearch {
  binding: RepoBindingFilter
  provider: string
  scope: string
  page: number
  pageSize: number
}

function positiveInt(value: unknown, fallback: number) {
  const parsed = Number.parseInt(String(value ?? ''), 10)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback
}

export function parseRepoSearch(search: Record<string, unknown>): RepoWorkbenchSearch {
  const binding = search.binding === 'bound' || search.binding === 'unbound' ? search.binding : 'all'
  return {
    binding,
    provider: typeof search.provider === 'string' ? search.provider : '',
    scope: typeof search.scope === 'string' ? search.scope : '',
    page: positiveInt(search.page, 1),
    pageSize: positiveInt(search.page_size, 20)
  }
}

export function buildRepoSearch(state: RepoWorkbenchSearch) {
  const next: Record<string, string> = {}
  if (state.binding !== 'all') next.binding = state.binding
  if (state.provider) next.provider = state.provider
  if (state.scope) next.scope = state.scope
  if (state.page > 1) next.page = String(state.page)
  if (state.pageSize !== 20) next.page_size = String(state.pageSize)
  return next
}

export function buildRepoListParams(state: {
  provider: RepoInventoryProviderSummary | null
  scope: string
  binding: RepoBindingFilter
  page: number
  pageSize: number
}) {
  const params: import('@/lib/api/types').RepoListParams = {
    page: state.page,
    pageSize: state.pageSize
  }
  if (!state.provider) return params
  if (state.provider.provider_key === 'unbound') {
    params.bindingState = 'unbound'
  } else {
    if (state.provider.provider_id) params.scmProviderId = state.provider.provider_id
    if (state.binding !== 'all') params.bindingState = state.binding
  }
  if (state.scope) params.scope = state.scope
  return params
}
```

- [ ] **Step 3: Refactor `ReposPage` query model**

In `frontend-ng/src/features/repos/repos-page.tsx`:

- Use `useSearch` and `useNavigate` from TanStack Router.
- Replace the flat `api.repos.list(page, pageSize)` query with:

```tsx
const inventory = useQuery({ queryKey: ['repos', 'inventory'], queryFn: api.repos.inventory })
const providers = useMemo(() => [...(inventory.data ?? [])].sort(compareInventoryProviders), [inventory.data])
const selectedProvider = providers.find((item) => item.provider_key === search.provider) ?? providers.find((item) => item.provider_key !== 'unbound') ?? providers[0] ?? null
const selectedScope = selectedProvider?.scopes.some((scope) => scope.scope === search.scope) ? search.scope : firstScope(selectedProvider)
const repos = useQuery({
  queryKey: ['repos', 'workbench', selectedProvider?.provider_key, selectedScope, search.binding, search.page, search.pageSize],
  queryFn: () => api.repos.list(buildRepoListParams({
    provider: selectedProvider,
    scope: selectedScope,
    binding: search.binding,
    page: search.page,
    pageSize: search.pageSize
  })),
  enabled: !!selectedProvider && !!selectedScope,
  placeholderData: keepPreviousData
})
```

- Replace raw provider buttons with shadcn `Tabs`.
- Replace binding/page-size native `select` with shadcn `Select`.
- Render scope list as `Button variant={active ? 'default' : 'ghost'}` inside a `Card`.
- Show admin-only batch actions:

```tsx
{me.data?.role === 'admin' ? (
  <Button variant='outline' disabled={webhookRepair.isPending} onClick={() => webhookRepair.mutate({ force: false })}>
    {webhookRepair.isPending ? t('repos.webhookRepairing') : t('repos.repairWebhooks')}
  </Button>
) : null}
```

- Use `Alert` for auto-bind and webhook repair messages.
- Use `Empty` for inventory empty and scoped repo empty states.
- Keep `Add repository` dialog behavior and two-step delete confirmation, but replace inline confirm buttons with `ConfirmAction`.

- [ ] **Step 4: Run focused tests**

Run:

```bash
cd frontend-ng && bun test src/features/repos/repos-state.test.ts && bun run check
```

Expected: PASS.

- [ ] **Step 5: Commit repo workbench**

Run:

```bash
git add frontend-ng/src/features/repos/repos-state.ts frontend-ng/src/features/repos/repos-state.test.ts frontend-ng/src/features/repos/repos-page.tsx
git commit -m "feat(frontend): align frontend-ng repo workbench"
```

Expected: commit created.

## Task 6: Add Repo Detail Webhook Repair

**Files:**
- Create: `frontend-ng/src/features/repos/repo-webhook-state.ts`
- Create: `frontend-ng/src/features/repos/repo-webhook-state.test.ts`
- Modify: `frontend-ng/src/features/repos/repo-detail-page.tsx`

- [ ] **Step 1: Write failing webhook detail tests**

Create `frontend-ng/src/features/repos/repo-webhook-state.test.ts`:

```ts
import { describe, expect, test } from 'vitest'
import { canShowWebhookRepair, repoWebhookRepairNotice } from './repo-webhook-state'

describe('repo webhook repair state', () => {
  test('shows repair only for admin bound repositories with failed or missing webhook', () => {
    expect(canShowWebhookRepair({ role: 'admin', bindingState: 'bound', status: 'webhook_failed', webhookId: 'old' })).toBe(true)
    expect(canShowWebhookRepair({ role: 'admin', bindingState: 'bound', status: 'active', webhookId: null })).toBe(true)
    expect(canShowWebhookRepair({ role: 'user', bindingState: 'bound', status: 'webhook_failed', webhookId: 'old' })).toBe(false)
    expect(canShowWebhookRepair({ role: 'admin', bindingState: 'unbound', status: 'webhook_failed', webhookId: 'old' })).toBe(false)
  })

  test('formats backend repair result into success or error notice', () => {
    expect(repoWebhookRepairNotice({ repo_config_id: 1, full_name: 'org/repo', previous_status: 'webhook_failed', status: 'active', webhook_status: 'registered' })).toEqual({ kind: 'success', key: 'repoDetail.webhookRepaired' })
    expect(repoWebhookRepairNotice({ repo_config_id: 1, full_name: 'org/repo', previous_status: 'webhook_failed', status: 'webhook_failed', webhook_status: 'failed', error: 'api failed' })).toEqual({ kind: 'error', key: 'repoDetail.webhookRepairFailed', detail: 'api failed' })
  })
})
```

- [ ] **Step 2: Implement webhook helpers**

Create `frontend-ng/src/features/repos/repo-webhook-state.ts`:

```ts
import type { MessageKey } from '@/lib/i18n/messages'
import type { RepoWebhookRepairItem } from '@/lib/api/types'

export function canShowWebhookRepair(state: {
  role?: string
  bindingState?: 'bound' | 'unbound'
  status?: string
  webhookId?: string | null
}) {
  return state.role === 'admin'
    && state.bindingState === 'bound'
    && (state.status === 'webhook_failed' || !state.webhookId)
}

export function repoWebhookRepairNotice(item: RepoWebhookRepairItem): { kind: 'success' | 'error'; key: MessageKey; detail?: string } {
  if (item.webhook_status === 'failed' || item.status === 'webhook_failed' || item.error) {
    return { kind: 'error', key: 'repoDetail.webhookRepairFailed', detail: item.error }
  }
  return { kind: 'success', key: item.webhook_status === 'registered' ? 'repoDetail.webhookRepaired' : 'repoDetail.webhookRepairComplete' }
}
```

- [ ] **Step 3: Add repair UI to repo detail**

Modify `frontend-ng/src/features/repos/repo-detail-page.tsx`:

- Query current user:

```tsx
const me = useQuery({ queryKey: ['auth', 'me'], queryFn: api.auth.me })
```

- Add state:

```tsx
const [webhookRepairForce, setWebhookRepairForce] = useState(false)
const [webhookRepairNotice, setWebhookRepairNotice] = useState<{ kind: 'success' | 'error'; message: string } | null>(null)
```

- Add mutation:

```tsx
const repairWebhook = useMutation({
  mutationFn: () => api.repos.repairWebhook(repoId, { force: webhookRepairForce }),
  onSuccess: (item) => {
    const notice = repoWebhookRepairNotice(item)
    setWebhookRepairNotice({ kind: notice.kind, message: notice.detail ? `${t(notice.key)}: ${notice.detail}` : t(notice.key) })
    void qc.invalidateQueries({ queryKey: ['repo', repoId] })
  },
  onError: (error) => setWebhookRepairNotice({ kind: 'error', message: error instanceof Error ? error.message : t('repoDetail.webhookRepairFailed') })
})
```

- Render before PR summary:

```tsx
{canShowWebhookRepair({
  role: me.data?.role,
  bindingState: repo.data?.binding_state,
  status: repo.data?.status,
  webhookId: repo.data?.webhook_id
}) ? (
  <Alert>
    <AlertTitle>{t('repoDetail.webhookRepairNeeded')}</AlertTitle>
    <AlertDescription>
      {repo.data?.webhook_id ? (
        <Field orientation='horizontal'>
          <Checkbox checked={webhookRepairForce} onCheckedChange={(value) => setWebhookRepairForce(value === true)} />
          <FieldLabel>{t('repoDetail.forceReplaceWebhook')}</FieldLabel>
        </Field>
      ) : null}
    </AlertDescription>
    <Button className='mt-3' disabled={repairWebhook.isPending} onClick={() => repairWebhook.mutate()}>
      {repairWebhook.isPending ? t('repoDetail.webhookRepairing') : t('repoDetail.repairWebhook')}
    </Button>
  </Alert>
) : null}
{webhookRepairNotice ? (
  <Alert variant={webhookRepairNotice.kind === 'error' ? 'destructive' : 'default'}>
    <AlertTitle>{webhookRepairNotice.message}</AlertTitle>
  </Alert>
) : null}
```

- [ ] **Step 4: Run focused tests**

Run:

```bash
cd frontend-ng && bun test src/features/repos/repo-webhook-state.test.ts && bun run check
```

Expected: PASS.

- [ ] **Step 5: Commit repo detail repair**

Run:

```bash
git add frontend-ng/src/features/repos/repo-webhook-state.ts frontend-ng/src/features/repos/repo-webhook-state.test.ts frontend-ng/src/features/repos/repo-detail-page.tsx
git commit -m "feat(frontend): add frontend-ng repo webhook repair"
```

Expected: commit created.

## Task 7: Shadcn-Harden Existing Pages

**Files:**
- Create: `frontend-ng/src/components/primitives/app-alert.tsx`
- Create: `frontend-ng/src/components/primitives/confirm-action.tsx`
- Create: `frontend-ng/src/components/primitives/page-empty.tsx`
- Modify: all files under `frontend-ng/src/features/**/*.tsx`
- Modify: `frontend-ng/src/components/layout/app-shell.tsx`
- Modify: `frontend-ng/src/components/primitives/data-state.tsx`

- [ ] **Step 1: Create primitive wrappers**

Create `frontend-ng/src/components/primitives/app-alert.tsx`:

```tsx
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'

export function AppAlert({ title, description, tone = 'info' }: { title: string; description?: string; tone?: 'info' | 'success' | 'warning' | 'error' }) {
  return (
    <Alert variant={tone === 'error' ? 'destructive' : 'default'} data-tone={tone}>
      <AlertTitle>{title}</AlertTitle>
      {description ? <AlertDescription>{description}</AlertDescription> : null}
    </Alert>
  )
}
```

Create `frontend-ng/src/components/primitives/confirm-action.tsx`:

```tsx
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle, AlertDialogTrigger } from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'

export function ConfirmAction({
  trigger,
  title,
  description,
  confirmLabel,
  cancelLabel,
  onConfirm,
  disabled
}: {
  trigger: React.ReactNode
  title: string
  description: string
  confirmLabel: string
  cancelLabel: string
  onConfirm: () => void
  disabled?: boolean
}) {
  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>{trigger}</AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{cancelLabel}</AlertDialogCancel>
          <AlertDialogAction asChild>
            <Button variant='destructive' disabled={disabled} onClick={onConfirm}>{confirmLabel}</Button>
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
```

Create `frontend-ng/src/components/primitives/page-empty.tsx`:

```tsx
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'

export function PageEmpty({ title, description, action }: { title: string; description?: string; action?: React.ReactNode }) {
  return (
    <Empty>
      <EmptyHeader>
        <EmptyTitle>{title}</EmptyTitle>
        {description ? <EmptyDescription>{description}</EmptyDescription> : null}
      </EmptyHeader>
      {action ? <EmptyContent>{action}</EmptyContent> : null}
    </Empty>
  )
}
```

- [ ] **Step 2: Replace native selects**

For every `rg "<select" frontend-ng/src -n` hit, replace native selects with shadcn `Select`:

```tsx
<Select value={value} onValueChange={setValue}>
  <SelectTrigger>
    <SelectValue placeholder={placeholder} />
  </SelectTrigger>
  <SelectContent>
    <SelectGroup>
      {options.map((option) => (
        <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
      ))}
    </SelectGroup>
  </SelectContent>
</Select>
```

Expected after edit:

```bash
rg "<select" frontend-ng/src -n
```

returns no matches.

- [ ] **Step 3: Replace raw checkbox labels**

For every `rg "<input type='checkbox'|<input type=\"checkbox\"" frontend-ng/src -n` hit, use:

```tsx
<Field orientation='horizontal'>
  <Checkbox checked={checked} onCheckedChange={(value) => setChecked(value === true)} />
  <FieldLabel>{label}</FieldLabel>
</Field>
```

Expected after edit:

```bash
rg "<input type=['\\\"]checkbox" frontend-ng/src -n
```

returns no matches.

- [ ] **Step 4: Replace `window.confirm`**

For every `rg "window\\.confirm" frontend-ng/src -n` hit, use `ConfirmAction` with explicit title/description from i18n keys.

Expected after edit:

```bash
rg "window\\.confirm" frontend-ng/src -n
```

returns no matches.

- [ ] **Step 5: Replace raw callouts and empty states**

Replace custom warning/success/error `<div className='text-[var(--ae-warn)]...'>` surfaces with `AppAlert` or shadcn `Alert`.

Replace empty cards like:

```tsx
<Card><CardContent>No repositories match this filter.</CardContent></Card>
```

with:

```tsx
<PageEmpty title={t('common.empty')} />
```

Expected after edit:

```bash
rg "text-\\[var\\(--ae-warn\\)|text-\\[var\\(--ae-pos\\)|No repositories match|No usage records yet|No access group" frontend-ng/src -n
```

returns no page-level matches outside UI primitives and translation resources.

- [ ] **Step 6: Replace manual details/summary**

For every `rg "<details|<summary" frontend-ng/src -n` hit, replace with shadcn `Accordion` if added, or with `Collapsible` after running:

```bash
cd frontend-ng && bunx --bun shadcn@latest add accordion
```

Expected after edit:

```bash
rg "<details|<summary" frontend-ng/src -n
```

returns no matches.

- [ ] **Step 7: Run shadcn hardening scans**

Run:

```bash
rg "<select|window\\.confirm|<details|<summary|<input type=['\\\"]checkbox|space-[xy]-" frontend-ng/src -n
rg "className=\\{`|dark:" frontend-ng/src -n
cd frontend-ng && bun run check
```

Expected: first two scans have no matches except false positives in tests or shadcn generated UI; typecheck passes.

- [ ] **Step 8: Commit shadcn hardening**

Run:

```bash
git add frontend-ng/src/components frontend-ng/src/features frontend-ng/src/routes frontend-ng/src/lib/i18n frontend-ng/src/styles.css frontend-ng/package.json frontend-ng/bun.lock
git commit -m "refactor(frontend): standardize frontend-ng shadcn surfaces"
```

Expected: commit created.

## Task 8: Complete I18n Migration For Frontend NG Copy

**Files:**
- Modify: `frontend-ng/src/lib/i18n/messages.ts`
- Modify: all user-facing `frontend-ng/src/**/*.tsx`
- Modify: `frontend-ng/src/lib/i18n/no-hardcoded-copy.test.ts`

- [ ] **Step 1: Run hardcoded copy scan**

Run:

```bash
cd frontend-ng && rg "\"[A-Z][^\"]{2,}\"|'[A-Z][^']{2,}'" src --glob '*.tsx' --glob '!src/lib/i18n/messages.ts' --glob '!src/routeTree.gen.ts'
```

Expected: many matches before migration.

- [ ] **Step 2: Move shell and nav copy to i18n**

Update `frontend-ng/src/components/layout/navigation.ts` to use translation keys:

```ts
type NavItem = {
  to: '/' | '/events' | '/repos' | '/user' | '/admin/users' | '/settings'
  labelKey: MessageKey
  sectionKey: MessageKey
  icon: LucideIcon
  admin?: boolean
}
```

Update `AppShell`:

```tsx
const { t, toggleLocale } = useI18n()
...
<span>{t(item.labelKey)}</span>
...
<Button variant='ghost' size='sm' onClick={toggleLocale}>{t('nav.languageToggle')}</Button>
```

- [ ] **Step 3: Move route page copy to i18n**

For each file below, replace visible strings with keys in `messages.ts`:

- `frontend-ng/src/features/auth/login-page.tsx`
- `frontend-ng/src/features/oauth/oauth-pages.tsx`
- `frontend-ng/src/features/home/home-page.tsx`
- `frontend-ng/src/features/events/events-page.tsx`
- `frontend-ng/src/features/repos/repos-page.tsx`
- `frontend-ng/src/features/repos/repo-detail-page.tsx`
- `frontend-ng/src/features/user-setup/user-page.tsx`
- `frontend-ng/src/features/admin-users/admin-users-page.tsx`
- `frontend-ng/src/features/settings/settings-page.tsx`

Use this pattern:

```tsx
const { t } = useI18n()
<PageHeader title={t('repos.title')} description={t('repos.subtitle')} />
```

For interpolated strings use:

```tsx
t('common.pageCount', { current: page, total: totalPages })
```

- [ ] **Step 4: Strengthen no-hardcoded-copy test**

Add this assertion to `frontend-ng/src/lib/i18n/no-hardcoded-copy.test.ts`:

```ts
test('does not leave obvious user-visible literals in TSX route surfaces', () => {
  const allowed = [/data-testid=/, /aria-label=/, /title=/, /className=/, /import /, /from /]
  const offenders = walk(ROOT)
    .map((file) => relative(ROOT, file))
    .filter((file) => file.endsWith('.tsx'))
    .filter((file) => !allowedLiteralFiles.has(file))
    .flatMap((file) => {
      const lines = readFileSync(join(ROOT, file), 'utf8').split('\n')
      return lines
        .map((line, index) => ({ file, line: index + 1, text: line }))
        .filter(({ text }) => />([A-Z][A-Za-z ,.'&:/-]{3,})</.test(text) || /(placeholder|title)=['"][A-Z][A-Za-z ,.'&:/-]{3,}['"]/.test(text))
        .filter(({ text }) => !allowed.some((pattern) => pattern.test(text)))
        .map(({ file, line, text }) => `${file}:${line}: ${text.trim()}`)
    })
  expect(offenders).toEqual([])
})
```

- [ ] **Step 5: Run i18n guard**

Run:

```bash
cd frontend-ng && bun test src/lib/i18n/no-hardcoded-copy.test.ts
```

Expected: PASS.

- [ ] **Step 6: Commit i18n migration**

Run:

```bash
git add frontend-ng/src
git commit -m "refactor(frontend): localize frontend-ng surfaces"
```

Expected: commit created.

## Task 9: Update Migration Spec And Run Full Verification

**Files:**
- Modify: `docs/superpowers/specs/2026-06-05-frontend-ng-tanstack-start-migration-design.md`

- [ ] **Step 1: Update implementation snapshot**

Add bullets under `Current Implementation Snapshot`:

```markdown
- After merging `main` on 2026-06-09, `frontend-ng/` aligns the latest Vue user usage dashboard, repo inventory workbench, batch webhook repair, and repo detail webhook repair flows.
- `frontend-ng/` now has React i18n resources and tests for English and `zh-CN`; user-visible copy should be added through `src/lib/i18n/messages.ts`.
- `frontend-ng/` page controls use shadcn/ui source components for selects, toggles, field layout, alerts, empty states, pagination, confirmations, and charts.
```

- [ ] **Step 2: Run full frontend-ng checks**

Run:

```bash
cd frontend-ng && bun run test
cd frontend-ng && bun run check
cd frontend-ng && bun run build
```

Expected:

- Tests pass.
- Typecheck exits 0.
- Build exits 0.
- Known warnings are acceptable only if they are the existing Node `module.register()` deprecation or Vite ineffective dynamic import warning; document any other warning.

- [ ] **Step 3: Run proxy/auth boundary scans**

Run:

```bash
rg "localStorage|sessionStorage|Authorization: Bearer|Authorization|Bearer|document.cookie|VITE_BACKEND_URL|AE_FRONTEND_BACKEND_URL|http://localhost:8081" frontend-ng/src frontend-ng -n
```

Expected:

- Token/backend origin matches only in `frontend-ng/src/lib/api/server.ts`, `frontend-ng/src/lib/auth/cookies.ts`, and README/config docs.
- Browser feature/client code does not attach Bearer and does not read/write app tokens.

- [ ] **Step 4: Run route/API parity scans**

Run:

```bash
rg "userUsage|usage/dashboard|repos/inventory|repair-webhooks|repairWebhook|webhook_id" frontend-ng/src -n
rg "<select|window\\.confirm|<details|<summary|<input type=['\\\"]checkbox" frontend-ng/src -n
rg "[\\p{Han}]" frontend-ng/src --glob '!src/lib/i18n/messages.ts' --glob '!src/lib/i18n/no-hardcoded-copy.test.ts' --glob '!src/routeTree.gen.ts' -n
```

Expected:

- First scan finds typed API/page usage for new mainline capabilities.
- Second scan returns no matches.
- Third scan returns no matches outside allowed i18n files.

- [ ] **Step 5: Run diff check**

Run:

```bash
git diff --check -- frontend-ng docs/superpowers/specs/2026-06-05-frontend-ng-tanstack-start-migration-design.md
```

Expected: no output.

- [ ] **Step 6: Commit docs and verification-ready state**

Run:

```bash
git add docs/superpowers/specs/2026-06-05-frontend-ng-tanstack-start-migration-design.md
git commit -m "docs(frontend): update frontend-ng alignment status"
```

Expected: commit created.

- [ ] **Step 7: Push branch**

Run:

```bash
git push origin codex/refactor/ng-frontend
```

Expected: branch pushes and PR #76 updates.

## Self-Review

Spec coverage:

- Latest `main` frontend APIs and views are covered by Tasks 3-6.
- shadcn component standardization is covered by Tasks 1 and 7.
- i18n framework, resource parity, and hardcoded-copy guard are covered by Tasks 2 and 8.
- Existing auth/proxy constraints are preserved and verified in Task 9.
- Existing Vue frontend, backend serve, and deploy cutover remain untouched.

Placeholder scan:

- This plan has no `TBD`, `TODO`, `fill in details`, or “write tests for the above” placeholders.
- Code snippets define the helper names used by later tasks.

Type consistency:

- `RepoInventoryProviderSummary`, `RepoWebhookRepairItem`, and `UserUsageDashboardSnapshot` names match the merged Vue/backend type contracts.
- `MessageKey`, `Locale`, `useI18n`, `formatMessage`, and `I18nProvider` are defined before downstream tasks use them.
- `canShowWebhookRepair` and `repoWebhookRepairNotice` are separate repo-detail helpers; broader repo workbench helpers remain in `repos-state.ts`.
