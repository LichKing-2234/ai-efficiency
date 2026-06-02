# History Pages Task-Zone Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build Phase 1 of the task-zone redesign: a coherent My Work experience with navigation zones, responsive shell, personal home, task-first setup, summary-first usage records, and minimal bilingual copy.

**Architecture:** Keep existing routes and backend APIs. Add a small frontend i18n layer, reorganize the shared shell, and update only `/`, `/user`, and `/events` as production My Work pages in this phase. Use frontend-only derivations from existing API responses; do not invent local CLI state or add backend contracts.

**Tech Stack:** Vue 3, Vite, TypeScript, Pinia, Vue Router, TailwindCSS, Vitest, Playwright CLI for visual review.

**Status:** Historical Phase 1 plan, superseded by the full frontend task-zone refactor.
**Replay Status:** Do not execute this checkbox list as the active ledger. The worktree implementation covered this Phase 1 scope plus Phase 2 Code & PR, Phase 3 Admin Console, and Phase 4 Auth Experience in one pass. The original checkboxes below are retained as the design-time plan; current audit status is tracked immediately below.

**Current Implementation Audit:**

- [x] Navigation zones and bilingual shell implemented.
- [x] My Work pages redesigned for personal usage, setup, and readable usage records.
- [x] Code & PR pages redesigned for repository health and PR usage summary.
- [x] Admin Console split into task-zone components: AI Services, Code Platforms, Organization & Login, Deployment & Runtime, and Advanced Credentials.
- [x] Deployment apply, rollback, and restart actions require explicit confirmation before API calls.
- [x] Auth pages share `AuthShell`; device login shows signed-in account confirmation.
- [x] Fresh full-suite verification for this final worktree state.
- [x] Fresh visual screenshot review after the final Settings, Admin Users, Repo Detail, and Device changes.

---

## Scope Boundary

This plan implements only Phase 1 from `docs/superpowers/specs/2026-05-29-history-pages-task-zone-ui-redesign-design.md`.

Included:

- Navigation zones and responsive shell.
- Minimal `en-US` / `zh-CN` i18n for touched labels.
- `/` My AI Usage.
- `/user` My Setup.
- `/events` Usage Records.
- Light label updates for `/repos`, `/admin/users`, and `/settings` so navigation does not point to old names.
- `docs/architecture.md` frontend section update.

Excluded:

- `/repos` health summary and `/repos/:id` PR redesign.
- `SettingsView.vue` component split.
- `/admin/users` risk-action redesign.
- `/login`, `/oauth/authorize`, `/oauth/device` redesign.
- New backend APIs.

If execution starts from a dirty worktree with prototype UI changes already present, preserve those changes when they satisfy the tests below. Do not revert user or prior-agent changes unless the user explicitly asks.

---

## File Map

### Shared Language And Shell

- Create: `frontend/src/i18n.ts`
- Modify: `frontend/src/components/AppLayout.vue`
- Modify: `frontend/src/components/AppSidebar.vue`
- Test: `frontend/src/__tests__/app-sidebar.test.ts`

### My Work Pages

- Modify: `frontend/src/views/DashboardView.vue`
- Test: `frontend/src/__tests__/dashboard-view.test.ts`
- Modify: `frontend/src/views/UserView.vue`
- Test: `frontend/src/__tests__/user-view.test.ts`
- Modify: `frontend/src/views/events/EventsView.vue`
- Test: `frontend/src/__tests__/events-view.test.ts`

### Light Navigation Label Alignment

- Modify: `frontend/src/views/repos/RepoListView.vue`
- Test: `frontend/src/__tests__/repo-list-view.test.ts`
- Modify: `frontend/src/views/admin/AdminUsersView.vue`
- Test: `frontend/src/__tests__/admin-users-view.test.ts`
- Modify: `frontend/src/views/SettingsView.vue`
- Test: `frontend/src/__tests__/settings-view.test.ts`

### Docs And Visual Review

- Modify: `docs/architecture.md`
- Create or update screenshots under: `output/playwright/`

---

## Task 1: Add Minimal I18n And Navigation Zones

**Files:**
- Create: `frontend/src/i18n.ts`
- Modify: `frontend/src/components/AppSidebar.vue`
- Test: `frontend/src/__tests__/app-sidebar.test.ts`

- [ ] **Step 1: Write failing sidebar tests**

Add this import to `frontend/src/__tests__/app-sidebar.test.ts`:

```ts
import { setLocale } from '@/i18n'
```

Update `beforeEach` in that file:

```ts
beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en-US')
  vi.clearAllMocks()
})
```

Add these tests:

```ts
it('groups regular navigation into My Work and Code & PR zones', async () => {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = createTestRouter()
  await router.push('/')
  await router.isReady()

  const { useAuthStore } = await import('@/stores/auth')
  const auth = useAuthStore(pinia)
  auth.user = { id: 2, username: 'alice', email: 'alice@example.com', role: 'user', auth_source: 'sso' }

  const wrapper = mount(AppSidebar, {
    global: { plugins: [pinia, router] },
  })

  expect(wrapper.text()).toContain('My Work')
  expect(wrapper.text()).toContain('Code & PR')
  expect(wrapper.text()).not.toContain('Administration')
  expect(wrapper.findAll('a').map((a) => a.text())).toEqual(
    expect.arrayContaining(['My AI Usage', 'My Setup', 'Usage Records', 'Code Repositories'])
  )
})

it('groups admin-only navigation under Administration', async () => {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = createTestRouter()
  await router.push('/')
  await router.isReady()

  const { useAuthStore } = await import('@/stores/auth')
  const auth = useAuthStore(pinia)
  auth.user = { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin', auth_source: 'sso' }

  const wrapper = mount(AppSidebar, {
    global: { plugins: [pinia, router] },
  })

  expect(wrapper.text()).toContain('Administration')
  expect(wrapper.findAll('a').map((a) => a.text())).toEqual(
    expect.arrayContaining(['Users & Access', 'Admin Console'])
  )
})

it('switches navigation labels to Chinese', async () => {
  const router = createTestRouter()
  await router.push('/')
  await router.isReady()

  const wrapper = mount(AppSidebar, {
    global: { plugins: [createPinia(), router] },
  })

  await wrapper.get('[data-testid="language-toggle"]').trigger('click')

  expect(wrapper.text()).toContain('我的工作')
  expect(wrapper.text()).toContain('代码与 PR')
  expect(wrapper.findAll('a').map((a) => a.text())).toEqual(
    expect.arrayContaining(['我的 AI 使用中心', '我的接入', '使用记录', '代码仓库'])
  )
})
```

- [ ] **Step 2: Run sidebar tests and verify failure**

Run:

```bash
cd frontend && pnpm vitest run src/__tests__/app-sidebar.test.ts
```

Expected: FAIL because `@/i18n` and grouped labels are not implemented.

- [ ] **Step 3: Implement `frontend/src/i18n.ts`**

Create `frontend/src/i18n.ts` with this structure. This first version contains the shell and navigation keys; each page task below adds its own explicit key block.

```ts
import { computed, ref } from 'vue'

export type Locale = 'en-US' | 'zh-CN'

const STORAGE_KEY = 'ae.locale'

const messages = {
  'en-US': {
    'app.title': 'AI Efficiency',
    'nav.myWorkSection': 'My Work',
    'nav.codeSection': 'Code & PR',
    'nav.adminSection': 'Administration',
    'nav.myUsage': 'My AI Usage',
    'nav.mySetup': 'My Setup',
    'nav.usageRecords': 'Usage Records',
    'nav.codeRepositories': 'Code Repositories',
    'nav.userManagement': 'Users & Access',
    'nav.adminConsole': 'Admin Console',
    'nav.languageToggle': '中文',
    'nav.logout': 'Logout',
    'nav.menu': 'Menu',
  },
  'zh-CN': {
    'app.title': 'AI 效能平台',
    'nav.myWorkSection': '我的工作',
    'nav.codeSection': '代码与 PR',
    'nav.adminSection': '管理',
    'nav.myUsage': '我的 AI 使用中心',
    'nav.mySetup': '我的接入',
    'nav.usageRecords': '使用记录',
    'nav.codeRepositories': '代码仓库',
    'nav.userManagement': '用户与接入',
    'nav.adminConsole': '管理后台',
    'nav.languageToggle': 'English',
    'nav.logout': '退出',
    'nav.menu': '菜单',
  },
} as const

export type MessageKey = keyof typeof messages['en-US']

function browserLocale(): Locale {
  if (typeof localStorage !== 'undefined') {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved === 'en-US' || saved === 'zh-CN') return saved
  }
  if (typeof navigator !== 'undefined' && navigator.language.toLowerCase().startsWith('zh')) {
    return 'zh-CN'
  }
  return 'en-US'
}

const locale = ref<Locale>(browserLocale())

function syncDocumentLanguage(next: Locale) {
  if (typeof document !== 'undefined') document.documentElement.lang = next
}

syncDocumentLanguage(locale.value)

export function setLocale(next: Locale) {
  locale.value = next
  if (typeof localStorage !== 'undefined') localStorage.setItem(STORAGE_KEY, next)
  syncDocumentLanguage(next)
}

export function t(key: MessageKey) {
  return messages[locale.value][key] || messages['en-US'][key] || key
}

export function useI18n() {
  const languageToggleLabel = computed(() => t('nav.languageToggle'))
  function toggleLocale() {
    setLocale(locale.value === 'en-US' ? 'zh-CN' : 'en-US')
  }
  return { locale, languageToggleLabel, setLocale, t, toggleLocale }
}
```

Each page task below adds its own listed keys to both locales so `MessageKey` stays type-safe.

- [ ] **Step 4: Update `AppSidebar.vue` to use zones**

In `frontend/src/components/AppSidebar.vue`, import and initialize i18n:

```ts
import { useI18n } from '@/i18n'

const { languageToggleLabel, t, toggleLocale } = useI18n()
```

Render navigation in three groups:

```vue
<nav class="flex-1 space-y-1 px-2 py-4">
  <div class="px-3 pb-2 text-[11px] font-semibold uppercase tracking-wide text-gray-500">
    {{ t('nav.myWorkSection') }}
  </div>
  <RouterLink to="/" class="flex items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800" active-class="bg-gray-800" @click="handleNavigate">
    {{ t('nav.myUsage') }}
  </RouterLink>
  <RouterLink to="/user" class="flex items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800" active-class="bg-gray-800" @click="handleNavigate">
    {{ t('nav.mySetup') }}
  </RouterLink>
  <RouterLink to="/events" class="flex items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800" active-class="bg-gray-800" @click="handleNavigate">
    {{ t('nav.usageRecords') }}
  </RouterLink>

  <div class="mt-5 border-t border-gray-800 pt-4">
    <div class="px-3 pb-2 text-[11px] font-semibold uppercase tracking-wide text-gray-500">
      {{ t('nav.codeSection') }}
    </div>
    <RouterLink to="/repos" class="flex items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800" active-class="bg-gray-800" @click="handleNavigate">
      {{ t('nav.codeRepositories') }}
    </RouterLink>
  </div>

  <div v-if="auth.isAdmin" class="mt-5 border-t border-gray-800 pt-4">
    <div class="px-3 pb-2 text-[11px] font-semibold uppercase tracking-wide text-gray-500">
      {{ t('nav.adminSection') }}
    </div>
    <RouterLink to="/admin/users" class="flex items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800" active-class="bg-gray-800" @click="handleNavigate">
      {{ t('nav.userManagement') }}
    </RouterLink>
    <RouterLink to="/settings" class="mt-1 flex items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800" active-class="bg-gray-800" @click="handleNavigate">
      {{ t('nav.adminConsole') }}
    </RouterLink>
  </div>
</nav>
```

Keep the existing icons if present; this snippet shows the required structure and labels.

Add the language button near the account area:

```vue
<button
  data-testid="language-toggle"
  class="w-full rounded-md border border-gray-700 px-3 py-2 text-left text-sm font-medium text-gray-200 hover:bg-gray-800"
  @click="toggleLocale"
>
  {{ languageToggleLabel }}
</button>
```

- [ ] **Step 5: Run sidebar tests and verify pass**

Run:

```bash
cd frontend && pnpm vitest run src/__tests__/app-sidebar.test.ts
```

Expected: PASS.

- [ ] **Step 6: Commit Task 1**

Run:

```bash
git add frontend/src/i18n.ts frontend/src/components/AppSidebar.vue frontend/src/__tests__/app-sidebar.test.ts
git commit -m "feat(frontend): add task-zone navigation"
```

Expected: commit succeeds and contains only Task 1 files.

---

## Task 2: Make `AppLayout` Responsive

**Files:**
- Modify: `frontend/src/components/AppLayout.vue`
- Test: covered by `frontend/src/__tests__/app-sidebar.test.ts` and page mount tests.

- [ ] **Step 1: Write a failing mobile shell assertion**

In `frontend/src/__tests__/app-sidebar.test.ts`, add a mount test for mobile shell through a simple route component if an `AppLayout` test file does not exist:

```ts
import AppLayout from '@/components/AppLayout.vue'

it('renders mobile menu and language controls in the app shell', () => {
  const wrapper = mount(AppLayout, {
    global: {
      plugins: [createPinia(), createTestRouter()],
      stubs: { RouterLink: { template: '<a><slot /></a>' } },
    },
    slots: { default: '<div>Page content</div>' },
  })

  expect(wrapper.text()).toContain('Menu')
  expect(wrapper.text()).toContain('AI Efficiency')
  expect(wrapper.text()).toContain('中文')
})
```

- [ ] **Step 2: Run the focused test and verify failure**

Run:

```bash
cd frontend && pnpm vitest run src/__tests__/app-sidebar.test.ts
```

Expected: FAIL because the current shell does not expose the mobile controls.

- [ ] **Step 3: Implement responsive `AppLayout.vue`**

Use this structure:

```vue
<script setup lang="ts">
import { ref } from 'vue'
import AppSidebar from './AppSidebar.vue'
import { useI18n } from '@/i18n'

const mobileNavOpen = ref(false)
const { languageToggleLabel, t, toggleLocale } = useI18n()
</script>

<template>
  <div class="min-h-screen bg-slate-50 md:flex">
    <header class="sticky top-0 z-30 flex h-14 items-center justify-between border-b border-slate-200 bg-white px-4 md:hidden">
      <button class="rounded-md border border-slate-300 px-3 py-2 text-sm font-medium text-slate-700" @click="mobileNavOpen = true">
        {{ t('nav.menu') }}
      </button>
      <div class="text-sm font-semibold text-slate-900">{{ t('app.title') }}</div>
      <button class="rounded-md border border-slate-300 px-3 py-2 text-sm font-medium text-slate-700" @click="toggleLocale">
        {{ languageToggleLabel }}
      </button>
    </header>

    <AppSidebar class="hidden md:flex" />

    <main class="min-h-screen flex-1 overflow-auto p-4 sm:p-6 lg:p-8">
      <slot />
    </main>

    <div v-if="mobileNavOpen" class="fixed inset-0 z-40 md:hidden">
      <button class="absolute inset-0 bg-slate-950/50" aria-label="Close navigation" @click="mobileNavOpen = false" />
      <AppSidebar class="relative h-full w-80 max-w-[86vw]" @navigate="mobileNavOpen = false" />
    </div>
  </div>
</template>
```

- [ ] **Step 4: Run layout/sidebar tests and verify pass**

Run:

```bash
cd frontend && pnpm vitest run src/__tests__/app-sidebar.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit Task 2**

Run:

```bash
git add frontend/src/components/AppLayout.vue frontend/src/__tests__/app-sidebar.test.ts
git commit -m "feat(frontend): add responsive app shell"
```

Expected: commit succeeds and contains only Task 2 files.

---

## Task 3: Convert `/` To My AI Usage

**Files:**
- Modify: `frontend/src/views/DashboardView.vue`
- Modify: `frontend/src/i18n.ts`
- Test: `frontend/src/__tests__/dashboard-view.test.ts`

- [ ] **Step 1: Write failing dashboard tests**

In `frontend/src/__tests__/dashboard-view.test.ts`, mock `@/api/user`:

```ts
vi.mock('@/api/user', () => ({
  getUserProviders: vi.fn(),
}))
```

Add this test:

```ts
it('derives connected tools from user provider credentials instead of workflow count', async () => {
  const { getDashboard } = await import('@/api/efficiency')
  const { getUserProviders } = await import('@/api/user')
  ;(getDashboard as any).mockResolvedValue({
    data: { data: { total_repos: 8, tracked_workflows: 4, total_ai_prs: 2 } },
  })
  ;(getUserProviders as any).mockResolvedValue({
    data: {
      data: {
        providers: [{
          id: 1,
          name: 'prod',
          display_name: 'Production',
          base_url: 'https://relay.example.com',
          default_model: 'claude-sonnet',
          is_primary: true,
          groups: [
            { group_id: '1', group_name: 'Group Alpha', platform: 'anthropic', credential: { state: 'existing_hidden' } },
            { group_id: '2', group_name: 'Group Beta', platform: 'openai', credential: { state: 'missing' } },
            { group_id: '3', group_name: 'Group Gamma', platform: 'anthropic', credential: { state: 'existing_hidden' } },
          ],
        }],
      },
    },
  })

  const router = createTestRouter()
  await router.push('/')
  await router.isReady()

  const wrapper = mount(DashboardView, {
    global: { plugins: [createPinia(), router] },
  })
  await flushPromises()

  expect(getUserProviders).toHaveBeenCalled()
  expect(wrapper.text()).toContain('Connected tools')
  expect(wrapper.text()).toContain('Configured from your relay access groups')
  expect(wrapper.text()).toContain('1')
  expect(wrapper.text()).not.toContain('Codex, Claude, Kiro when configured')
})
```

Also update existing title/loading/error expectations:

```ts
expect(wrapper.find('h1').text()).toContain('My AI Usage')
expect(wrapper.text()).toContain('Next Steps')
expect(wrapper.text()).toContain('Loading your AI usage')
expect(wrapper.text()).toContain('No platform data yet')
expect(wrapper.text()).toContain('Open My Setup')
```

- [ ] **Step 2: Run dashboard tests and verify failure**

Run:

```bash
cd frontend && pnpm vitest run src/__tests__/dashboard-view.test.ts
```

Expected: FAIL because dashboard still uses old copy or computes connected tools from workflow count.

- [ ] **Step 3: Add dashboard i18n keys**

Add these English keys to `frontend/src/i18n.ts`:

```ts
'home.title': 'My AI Usage',
'home.subtitle': 'A simple view of your AI setup, recent platform signals, and next steps.',
'home.personalStatus': 'Personal Status',
'home.thisWeek': 'This Week',
'home.setupStatus': 'Setup Status',
'home.nextSteps': 'Next Steps',
'home.recentActivity': 'Recent Activity',
'home.signalsScope': 'Platform-visible signals',
'home.loading': 'Loading your AI usage...',
'home.noData': 'No platform data yet',
'home.noDataHelp': 'Finish setup or wait for the next commit-backed upload before expecting usage records here.',
'home.openSetup': 'Open My Setup',
'home.viewRecords': 'View Usage Records',
'home.metricRepos': 'Code repositories',
'home.metricReposHelp': 'Repositories visible to the platform',
'home.metricWorkflows': 'Tracked Workflows',
'home.metricWorkflowsHelp': 'Reporting paths currently tracked',
'home.metricAiPrs': 'AI PRs',
'home.metricAiPrsHelp': 'PRs with AI usage signals',
'home.metricTools': 'Connected tools',
'home.metricToolsHelp': 'Configured from your relay access groups',
'home.metricToolsHelpNone': 'Open My Setup to create tool access',
'home.metricToolsHelpUnavailable': 'Open My Setup to verify tool access',
'home.recentLoaded': 'Dashboard data loaded from API. Use Usage Records for event-level detail and Code Repositories for PR usage freshness.',
'home.statusAccount': 'Account',
'home.statusCli': 'CLI setup',
'home.statusData': 'Data visibility',
'home.statusAccountReady': 'Signed in',
'home.statusCliGuide': 'Follow My Setup',
'home.statusDataSeen': 'Platform data available',
'home.statusDataMissing': 'Waiting for data',
'home.nextSetupTitle': 'Set up this machine',
'home.nextSetupText': 'Install the CLI, sign in, configure tools, and enable hooks.',
'home.nextRepoTitle': 'Connect a repository',
'home.nextRepoText': 'Run init and doctor inside each repository you want to report.',
'home.nextRecordsTitle': 'Check recent records',
'home.nextRecordsText': 'Review whether usage is linked to commits and repositories.',
```

Add matching Chinese keys:

```ts
'home.title': '我的 AI 使用中心',
'home.subtitle': '查看你的 AI 接入状态、近期平台信号和下一步动作。',
'home.personalStatus': '个人状态',
'home.thisWeek': '本周概览',
'home.setupStatus': '接入状态',
'home.nextSteps': '下一步',
'home.recentActivity': '最近动态',
'home.signalsScope': '平台可见信号',
'home.loading': '正在加载你的 AI 使用情况...',
'home.noData': '暂无平台数据',
'home.noDataHelp': '完成接入，或等待下一次带 commit 的上传后再查看使用记录。',
'home.openSetup': '打开我的接入',
'home.viewRecords': '查看使用记录',
'home.metricRepos': '代码仓库',
'home.metricReposHelp': '平台可见的代码仓库',
'home.metricWorkflows': '已追踪流程',
'home.metricWorkflowsHelp': '当前已追踪的上报路径',
'home.metricAiPrs': 'AI PR',
'home.metricAiPrsHelp': '带有 AI 使用信号的 PR',
'home.metricTools': '已接入工具',
'home.metricToolsHelp': '来自你的 relay 接入组配置',
'home.metricToolsHelpNone': '打开我的接入创建工具访问',
'home.metricToolsHelpUnavailable': '打开我的接入确认工具访问',
'home.recentLoaded': '首页数据已从 API 加载。使用记录可看事件明细，代码仓库可看 PR 使用新鲜度。',
'home.statusAccount': '账号',
'home.statusCli': 'CLI 接入',
'home.statusData': '数据可见性',
'home.statusAccountReady': '已登录',
'home.statusCliGuide': '按我的接入完成',
'home.statusDataSeen': '平台已有数据',
'home.statusDataMissing': '等待数据',
'home.nextSetupTitle': '设置这台机器',
'home.nextSetupText': '安装 CLI、登录、配置工具并启用 hooks。',
'home.nextRepoTitle': '接入代码仓库',
'home.nextRepoText': '在每个需要上报的仓库中执行 init 和 doctor。',
'home.nextRecordsTitle': '检查最近记录',
'home.nextRecordsText': '确认使用记录是否已关联到 commit 和仓库。',
```

- [ ] **Step 4: Implement provider-backed connected tool count**

In `frontend/src/views/DashboardView.vue`, import user providers:

```ts
import { getUserProviders } from '@/api/user'
import { useI18n } from '@/i18n'
import type { DashboardData, UserProviderSummary } from '@/types'
```

Add state and load both APIs:

```ts
const { t } = useI18n()
const dashboard = ref<DashboardData | null>(null)
const userProviders = ref<UserProviderSummary[]>([])
const loading = ref(true)
const loadFailed = ref(false)
const providersLoadFailed = ref(false)

onMounted(async () => {
  const [dashboardResult, providersResult] = await Promise.allSettled([
    getDashboard(),
    getUserProviders(),
  ])

  if (dashboardResult.status === 'fulfilled') {
    dashboard.value = dashboardResult.value.data.data ?? null
  } else {
    loadFailed.value = true
    dashboard.value = null
  }

  if (providersResult.status === 'fulfilled') {
    userProviders.value = providersResult.value.data.data?.providers ?? []
  } else {
    providersLoadFailed.value = true
    userProviders.value = []
  }

  loading.value = false
})
```

Add connected tool derivation:

```ts
const connectedToolCount = computed(() => {
  if (providersLoadFailed.value) return undefined
  const platforms = new Set<string>()
  for (const provider of userProviders.value) {
    for (const group of provider.groups) {
      if (group.credential.state === 'existing_hidden') platforms.add(group.platform)
    }
  }
  return platforms.size
})

const connectedToolHelp = computed(() => {
  if (providersLoadFailed.value) return t('home.metricToolsHelpUnavailable')
  return connectedToolCount.value ? t('home.metricToolsHelp') : t('home.metricToolsHelpNone')
})
```

Render a personal home page with:

- personal status block,
- metric cards,
- setup status,
- next steps,
- recent activity.

Use `RouterLink` for `/user`, `/repos`, and `/events`.

- [ ] **Step 5: Run dashboard tests and verify pass**

Run:

```bash
cd frontend && pnpm vitest run src/__tests__/dashboard-view.test.ts
```

Expected: PASS.

- [ ] **Step 6: Commit Task 3**

Run:

```bash
git add frontend/src/views/DashboardView.vue frontend/src/i18n.ts frontend/src/__tests__/dashboard-view.test.ts
git commit -m "feat(frontend): add personal AI usage home"
```

Expected: commit succeeds and contains only Task 3 files.

---

## Task 4: Make `/user` Task-First

**Files:**
- Modify: `frontend/src/views/UserView.vue`
- Modify: `frontend/src/i18n.ts`
- Test: `frontend/src/__tests__/user-view.test.ts`

- [ ] **Step 1: Write failing user-facing label test**

Add this test to `frontend/src/__tests__/user-view.test.ts`:

```ts
it('uses user-facing setup labels instead of raw credential labels', async () => {
  const { wrapper } = await mountUserView()

  expect(wrapper.text()).toContain('Your account')
  expect(wrapper.text()).toContain('AI access')
  expect(wrapper.text()).toContain('Access group')
  expect(wrapper.text()).toContain('Ready to use')
  expect(wrapper.text()).toContain('API key and connection test')
  expect(wrapper.text()).toContain('Command reference')
  expect(wrapper.text()).not.toContain('Profile Summary')
  expect(wrapper.text()).not.toContain('Provider & Group Credential')
  expect(wrapper.text()).not.toContain('Credential state')
  expect(wrapper.text()).not.toContain('Current Secret')
})
```

- [ ] **Step 2: Run user view test and verify failure**

Run:

```bash
cd frontend && pnpm vitest run src/__tests__/user-view.test.ts
```

Expected: FAIL because old raw labels are still present.

- [ ] **Step 3: Add user setup i18n keys**

Add these English keys:

```ts
'user.title': 'My Setup',
'user.subtitle': 'Set up AI tools and code repositories without starting from admin settings.',
'user.refresh': 'Refresh',
'user.accountTitle': 'Your account',
'user.username': 'Username',
'user.email': 'Email',
'user.role': 'Role',
'user.authSource': 'Sign-in source',
'user.aiAccessTitle': 'AI access',
'user.loading': 'Loading...',
'user.primary': 'Primary',
'user.accessTitle': 'Access group',
'user.readyToUse': 'Ready to use',
'user.needsSetup': 'Needs setup',
'user.readyWithKey': 'This group already has reusable AI access. The key is masked until you reveal or copy it.',
'user.readyNoKey': 'This group has reusable AI access, but the relay response did not include the key value.',
'user.missingKey': 'No reusable AI access exists for this group yet.',
'user.apiKeyTitle': 'API key',
'user.createKey': 'Create Key',
'user.regenerate': 'Regenerate',
'user.hide': 'Hide',
'user.reveal': 'Reveal',
'user.copy': 'Copy',
'user.testTitle': 'API key and connection test',
'user.testHelp': 'Sends a real chat completion through this access group.',
'user.platform': 'Platform',
'user.model': 'Model',
'user.prompt': 'Prompt',
'user.testing': 'Testing...',
'user.runTest': 'Run Test',
'user.commandReference': 'Command reference',
'user.machineSetup': 'Machine Setup',
'user.installCli': '1. Install CLI',
'user.login': '2. Login',
'user.configureTools': '3. Configure local AI tools',
'user.enableHooks': '4. Enable automatic Git hooks',
'user.hooksHelp': 'Machine-level hook setup. It only reports backend-known eligible repositories.',
'user.perRepoSetup': 'Per-Repo Setup',
'user.repoStep1': '1. Go to the repo you want to report',
'user.repoStep2': '2. Initialize repo attribution',
'user.repoStep3': '3. Diagnose setup',
'user.manualRecovery': 'Manual backfill / recovery',
'user.manualSync': 'Run a manual attribution sync',
'user.hookStatus': 'Inspect hook upload status',
'user.selectProviderCommand': 'Select an access provider to build the discover command.',
```

Add matching Chinese keys:

```ts
'user.title': '我的接入',
'user.subtitle': '不用从管理配置开始，也能完成 AI 工具和代码仓库接入。',
'user.refresh': '刷新',
'user.accountTitle': '你的账号',
'user.username': '用户名',
'user.email': '邮箱',
'user.role': '角色',
'user.authSource': '登录来源',
'user.aiAccessTitle': 'AI 访问',
'user.loading': '加载中...',
'user.primary': '主入口',
'user.accessTitle': '接入组',
'user.readyToUse': '可使用',
'user.needsSetup': '需设置',
'user.readyWithKey': '这个接入组已有可复用的 AI 访问。密钥默认隐藏，可按需显示或复制。',
'user.readyNoKey': '这个接入组已有可复用的 AI 访问，但 relay 响应没有返回密钥值。',
'user.missingKey': '这个接入组还没有可复用的 AI 访问。',
'user.apiKeyTitle': 'API Key',
'user.createKey': '创建 Key',
'user.regenerate': '重新生成',
'user.hide': '隐藏',
'user.reveal': '显示',
'user.copy': '复制',
'user.testTitle': 'API Key 和连接测试',
'user.testHelp': '通过当前接入组发起一次真实 chat completion。',
'user.platform': '平台',
'user.model': '模型',
'user.prompt': 'Prompt',
'user.testing': '测试中...',
'user.runTest': '运行测试',
'user.commandReference': '命令参考',
'user.machineSetup': '机器设置',
'user.installCli': '1. 安装 CLI',
'user.login': '2. 登录',
'user.configureTools': '3. 配置本地 AI 工具',
'user.enableHooks': '4. 启用自动 Git hooks',
'user.hooksHelp': '机器级 hook 设置，只会上报后端已知且符合条件的仓库。',
'user.perRepoSetup': '单仓库设置',
'user.repoStep1': '1. 进入需要上报的仓库',
'user.repoStep2': '2. 初始化仓库归因',
'user.repoStep3': '3. 诊断接入',
'user.manualRecovery': '手动补传 / 恢复',
'user.manualSync': '运行一次手动归因同步',
'user.hookStatus': '查看 hook 上传状态',
'user.selectProviderCommand': '选择接入入口后生成 discover 命令。',
```

- [ ] **Step 4: Add credential status helpers**

In `frontend/src/views/UserView.vue`, add:

```ts
const readyAccessGroupCount = computed(() =>
  providers.value.reduce(
    (count, provider) => count + provider.groups.filter((group) => group.credential.state === 'existing_hidden').length,
    0
  )
)

const totalAccessGroupCount = computed(() =>
  providers.value.reduce((count, provider) => count + provider.groups.length, 0)
)

function credentialStatusLabel(state: string) {
  return state === 'existing_hidden' ? t('user.readyToUse') : t('user.needsSetup')
}

function credentialStatusHelp(state: string, hasKey?: boolean) {
  if (state !== 'existing_hidden') return t('user.missingKey')
  return hasKey ? t('user.readyWithKey') : t('user.readyNoKey')
}
```

- [ ] **Step 5: Replace default section titles**

Replace the old titles in `UserView.vue`:

```vue
<h2>{{ t('user.accountTitle') }}</h2>
<h2>{{ t('user.aiAccessTitle') }}</h2>
<h2>{{ t('user.accessTitle') }}</h2>
<h2>{{ t('user.commandReference') }}</h2>
```

Replace raw credential state copy with:

```vue
<div class="font-medium text-gray-900">{{ credentialStatusLabel(selectedGroup.credential.state) }}</div>
<div class="mt-2">Group: {{ selectedGroup.group_name }}</div>
<div class="mt-1">Platform: {{ selectedGroup.platform }}</div>
<div class="mt-2">{{ credentialStatusHelp(selectedGroup.credential.state, !!selectedGroup.credential.key) }}</div>
```

Keep command strings unchanged:

```vue
<pre>{{ installCommand }}</pre>
<pre>{{ loginCommand }}</pre>
<pre>{{ discoverCommand || t('user.selectProviderCommand') }}</pre>
<pre>{{ hooksGlobalCommand }}</pre>
<pre>{{ repoInitCommand }}</pre>
<pre>{{ doctorCommand }}</pre>
<pre>{{ syncCommand }}</pre>
```

- [ ] **Step 6: Run user view tests and verify pass**

Run:

```bash
cd frontend && pnpm vitest run src/__tests__/user-view.test.ts
```

Expected: PASS.

- [ ] **Step 7: Commit Task 4**

Run:

```bash
git add frontend/src/views/UserView.vue frontend/src/i18n.ts frontend/src/__tests__/user-view.test.ts
git commit -m "feat(frontend): make setup page task-first"
```

Expected: commit succeeds and contains only Task 4 files.

---

## Task 5: Make `/events` Summary-First

**Files:**
- Modify: `frontend/src/views/events/EventsView.vue`
- Modify: `frontend/src/i18n.ts`
- Test: `frontend/src/__tests__/events-view.test.ts`

- [ ] **Step 1: Write failing usage records tests**

In `frontend/src/__tests__/events-view.test.ts`, update the mount test assertions:

```ts
expect(wrapper.text()).toContain('Usage Records')
expect(wrapper.text()).toContain('Total Records')
expect(wrapper.text()).toContain('Recent usage')
expect(wrapper.text()).toContain('Code link')
expect(wrapper.text()).toContain('Token usage')
expect(wrapper.text()).toContain('Linked')
expect(wrapper.text()).not.toContain('detail.jsonl')
```

Update the non-admin detail test:

```ts
expect(wrapper.text()).toContain('Record detail')
expect(wrapper.text()).toContain('Code link')
expect(wrapper.text()).toContain('Advanced event data')
expect(wrapper.text()).not.toContain('Dedupe Key')
expect(wrapper.text()).not.toContain('Raw Payload')
```

- [ ] **Step 2: Run events tests and verify failure**

Run:

```bash
cd frontend && pnpm vitest run src/__tests__/events-view.test.ts
```

Expected: FAIL because the page still exposes old event table fields by default.

- [ ] **Step 3: Add events i18n keys**

Add these English keys:

```ts
'events.title': 'Usage Records',
'events.subtitle': 'Review AI tool usage in a readable list, with raw event detail available when needed.',
'events.totalRecords': 'Total Records',
'events.linkedToCommit': 'Linked to Commit',
'events.needsLinking': 'Needs Linking',
'events.tools': 'Tools',
'events.refresh': 'Refresh',
'events.loading': 'Loading...',
'events.from': 'From',
'events.to': 'To',
'events.tool': 'Tool',
'events.binding': 'Code link',
'events.search': 'Search',
'events.all': 'All',
'events.bound': 'Linked',
'events.unbound': 'Needs linking',
'events.searchPlaceholder': 'tool, event id, commit, source',
'events.user': 'User',
'events.userSearchPlaceholder': 'Search email or username',
'events.searching': 'Searching...',
'events.clear': 'Clear',
'events.clearTime': 'Clear Time',
'events.applyFilters': 'Apply Filters',
'events.recentUsage': 'Recent usage',
'events.totalSuffix': 'total',
'events.prev': 'Prev',
'events.next': 'Next',
'events.page': 'Page',
'events.empty': 'No usage records match current filters.',
'events.observed': 'Time',
'events.repository': 'Repository',
'events.codeLink': 'Code link',
'events.tokenUsage': 'Token usage',
'events.credits': 'Credits',
'events.requests': 'Requests',
'events.recordDetail': 'Record detail',
'events.close': 'Close',
'events.loadingDetail': 'Loading detail...',
'events.basic': 'Summary',
'events.observedAt': 'Observed',
'events.usage': 'Token usage',
'events.input': 'Input',
'events.output': 'Output',
'events.cache': 'Cache',
'events.reasoning': 'Reasoning',
'events.codeStatus': 'Status',
'events.commit': 'Commit',
'events.capturedAt': 'Captured At',
'events.matchedPrs': 'Matched PRs',
'events.noMatchedPrs': 'No matched PRs',
'events.advancedData': 'Advanced event data',
'events.workspace': 'Workspace',
'events.toolSession': 'Tool Session',
'events.toolEvent': 'Tool Event',
'events.dedupeKey': 'Dedupe Key',
'events.source': 'Source',
'events.rawPayload': 'Raw Payload',
```

Add matching Chinese keys:

```ts
'events.title': '使用记录',
'events.subtitle': '用更容易理解的列表查看 AI 工具使用情况，需要时再进入原始事件详情。',
'events.totalRecords': '记录总数',
'events.linkedToCommit': '已关联 Commit',
'events.needsLinking': '待关联',
'events.tools': '工具',
'events.refresh': '刷新',
'events.loading': '加载中...',
'events.from': '开始',
'events.to': '结束',
'events.tool': '工具',
'events.binding': '代码关联',
'events.search': '搜索',
'events.all': '全部',
'events.bound': '已关联',
'events.unbound': '待关联',
'events.searchPlaceholder': '工具、事件 ID、commit、来源',
'events.user': '用户',
'events.userSearchPlaceholder': '搜索邮箱或用户名',
'events.searching': '搜索中...',
'events.clear': '清除',
'events.clearTime': '清除时间',
'events.applyFilters': '应用筛选',
'events.recentUsage': '最近使用',
'events.totalSuffix': '条',
'events.prev': '上一页',
'events.next': '下一页',
'events.page': '第',
'events.empty': '没有匹配当前筛选的使用记录。',
'events.observed': '时间',
'events.repository': '仓库',
'events.codeLink': '代码关联',
'events.tokenUsage': 'Token 用量',
'events.credits': 'Credits',
'events.requests': '请求',
'events.recordDetail': '记录详情',
'events.close': '关闭',
'events.loadingDetail': '正在加载详情...',
'events.basic': '摘要',
'events.observedAt': '发生时间',
'events.usage': 'Token 用量',
'events.input': '输入',
'events.output': '输出',
'events.cache': '缓存',
'events.reasoning': '推理',
'events.codeStatus': '状态',
'events.commit': 'Commit',
'events.capturedAt': '采集时间',
'events.matchedPrs': '匹配 PR',
'events.noMatchedPrs': '无匹配 PR',
'events.advancedData': '高级事件数据',
'events.workspace': 'Workspace',
'events.toolSession': 'Tool Session',
'events.toolEvent': 'Tool Event',
'events.dedupeKey': 'Dedupe Key',
'events.source': '来源',
'events.rawPayload': 'Raw Payload',
```

- [ ] **Step 4: Implement summary helpers**

In `frontend/src/views/events/EventsView.vue`, add:

```ts
function formatTokenUsage(row: ToolUsageEventRow) {
  const input = row.input_tokens ?? 0
  const output = row.output_tokens ?? 0
  const cached = row.cached_input_tokens ?? 0
  const totalTokens = input + output + cached
  return totalTokens > 0 ? formatCount(totalTokens) : '—'
}

function bindingStatusLabel(value?: string | null) {
  if (value === 'bound') return t('events.bound')
  if (value === 'unbound') return t('events.unbound')
  return '—'
}
```

- [ ] **Step 5: Replace default event table columns**

Default table headers must be:

```vue
<th>{{ t('events.observed') }}</th>
<th>{{ t('events.tool') }}</th>
<th>{{ t('events.repository') }}</th>
<th>{{ t('events.codeLink') }}</th>
<th>{{ t('events.tokenUsage') }}</th>
<th>{{ t('events.credits') }}</th>
<th>{{ t('events.requests') }}</th>
<th v-if="isAdmin">{{ t('events.user') }}</th>
```

Default row cells must not render `row.source_basename`:

```vue
<td>{{ formatDate(row.observed_end_at) }}</td>
<td>{{ row.tool }}</td>
<td>{{ row.repo_name }}</td>
<td>
  <div :class="row.binding_status === 'bound' ? 'text-emerald-700' : 'text-amber-700'">
    {{ bindingStatusLabel(row.binding_status) }}
  </div>
  <div class="font-mono text-xs text-gray-500">{{ shortSha(row.commit_sha) }}</div>
</td>
<td>{{ formatTokenUsage(row) }}</td>
<td>{{ formatDecimal(row.credit_usage) }}</td>
<td>{{ formatCount(row.request_count) }}</td>
<td v-if="isAdmin">{{ row.username || '—' }}</td>
```

- [ ] **Step 6: Move raw fields into advanced details**

In the detail drawer, render summary sections first, then advanced details:

```vue
<h2>{{ t('events.recordDetail') }}</h2>

<h3>{{ t('events.basic') }}</h3>
<dl>
  <div><dt>{{ t('events.tool') }}</dt><dd>{{ selectedEvent.tool }}</dd></div>
  <div><dt>{{ t('events.repository') }}</dt><dd>{{ selectedEvent.repo_name || '—' }}</dd></div>
  <div><dt>{{ t('events.observedAt') }}</dt><dd>{{ formatDate(selectedEvent.observed_end_at) }}</dd></div>
</dl>

<h3>{{ t('events.codeLink') }}</h3>
<dl>
  <div><dt>{{ t('events.codeStatus') }}</dt><dd>{{ bindingStatusLabel(selectedEvent.binding_status) }}</dd></div>
  <div><dt>{{ t('events.commit') }}</dt><dd class="font-mono">{{ selectedEvent.commit_sha || '—' }}</dd></div>
</dl>

<details>
  <summary>{{ t('events.advancedData') }}</summary>
  <dl>
    <div><dt>{{ t('events.workspace') }}</dt><dd class="font-mono">{{ selectedEvent.workspace_id }}</dd></div>
    <div><dt>{{ t('events.toolSession') }}</dt><dd class="font-mono">{{ selectedEvent.tool_session_id }}</dd></div>
    <div v-if="isAdmin"><dt>{{ t('events.dedupeKey') }}</dt><dd class="font-mono">{{ selectedEvent.dedupe_key }}</dd></div>
  </dl>
  <pre v-if="isAdmin && selectedEvent.raw_payload">{{ JSON.stringify(selectedEvent.raw_payload, null, 2) }}</pre>
</details>
```

- [ ] **Step 7: Run events tests and verify pass**

Run:

```bash
cd frontend && pnpm vitest run src/__tests__/events-view.test.ts
```

Expected: PASS.

- [ ] **Step 8: Commit Task 5**

Run:

```bash
git add frontend/src/views/events/EventsView.vue frontend/src/i18n.ts frontend/src/__tests__/events-view.test.ts
git commit -m "feat(frontend): summarize usage records"
```

Expected: commit succeeds and contains only Task 5 files.

---

## Task 6: Align Touched Legacy Page Labels

**Files:**
- Modify: `frontend/src/views/repos/RepoListView.vue`
- Modify: `frontend/src/views/admin/AdminUsersView.vue`
- Modify: `frontend/src/views/SettingsView.vue`
- Modify: `frontend/src/i18n.ts`
- Test: `frontend/src/__tests__/repo-list-view.test.ts`
- Test: `frontend/src/__tests__/admin-users-view.test.ts`
- Test: `frontend/src/__tests__/settings-view.test.ts`

- [ ] **Step 1: Write failing label tests**

In `repo-list-view.test.ts`, assert:

```ts
expect(wrapper.find('h1').text()).toContain('Code Repositories')
```

In `admin-users-view.test.ts`, assert:

```ts
expect(wrapper.find('h1').text()).toContain('Users & Access')
```

In `settings-view.test.ts`, assert:

```ts
expect(wrapper.find('h1').text()).toContain('Admin Console')
```

- [ ] **Step 2: Run focused tests and verify failure**

Run:

```bash
cd frontend && pnpm vitest run src/__tests__/repo-list-view.test.ts src/__tests__/admin-users-view.test.ts src/__tests__/settings-view.test.ts
```

Expected: FAIL if pages still render old labels.

- [ ] **Step 3: Add label keys and replace headings**

Add this key to both locales in `frontend/src/i18n.ts`:

```ts
'repos.title': 'Code Repositories',
```

```ts
'repos.title': '代码仓库',
```

Confirm the Task 1 keys remain:

```ts
'nav.userManagement': 'Users & Access',
'nav.adminConsole': 'Admin Console',
```

In `RepoListView.vue`:

```vue
<h1 class="text-2xl font-bold text-gray-900">{{ t('repos.title') }}</h1>
```

In `AdminUsersView.vue`:

```vue
<h1 class="text-2xl font-bold text-gray-900">{{ t('nav.userManagement') }}</h1>
```

In `SettingsView.vue`:

```vue
<h1 class="text-2xl font-bold text-gray-900">{{ t('nav.adminConsole') }}</h1>
```

Add any missing imports:

```ts
import { useI18n } from '@/i18n'

const { t } = useI18n()
```

- [ ] **Step 4: Run focused tests and verify pass**

Run:

```bash
cd frontend && pnpm vitest run src/__tests__/repo-list-view.test.ts src/__tests__/admin-users-view.test.ts src/__tests__/settings-view.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit Task 6**

Run:

```bash
git add frontend/src/views/repos/RepoListView.vue frontend/src/views/admin/AdminUsersView.vue frontend/src/views/SettingsView.vue frontend/src/i18n.ts frontend/src/__tests__/repo-list-view.test.ts frontend/src/__tests__/admin-users-view.test.ts frontend/src/__tests__/settings-view.test.ts
git commit -m "feat(frontend): align legacy page labels"
```

Expected: commit succeeds and contains only Task 6 files.

---

## Task 7: Update Architecture Documentation

**Files:**
- Modify: `docs/architecture.md`

- [ ] **Step 1: Find the frontend architecture section**

Run:

```bash
rg -n "Frontend|front-end|Vue|AppLayout|Dashboard|Settings" docs/architecture.md
```

Expected: command prints the section that describes the frontend app, routes, or UI boundaries.

- [ ] **Step 2: Update `docs/architecture.md`**

Add this subsection under the existing frontend architecture section. If `docs/architecture.md` has no frontend subsection, place it after the project-level runtime overview:

```md
### Frontend Task Zones

The Vue frontend keeps the existing route contract while grouping pages by user task:

- `My Work`: `/`, `/user`, and `/events` provide the ordinary user path for personal AI usage, setup, and usage records.
- `Code & PR`: `/repos` and `/repos/:id` provide repository and PR usage workflows for developers, leads, and admins.
- `Administration`: `/admin/users` and `/settings` are admin-only surfaces for users, access, providers, credentials, login configuration, and deployment/runtime controls.
- Auth pages: `/login`, `/oauth/authorize`, and `/oauth/device` remain public or pre-app flows and do not use the main app shell.

Phase 1 of the task-zone redesign is frontend-only. It derives connected tool counts from `/api/v1/user/providers`, keeps command strings unchanged, and does not claim local CLI state unless that state is backed by server data.
```

- [ ] **Step 3: Run docs diff check**

Run:

```bash
git diff --check -- docs/architecture.md
```

Expected: no output.

- [ ] **Step 4: Commit Task 7**

Run:

```bash
git add docs/architecture.md
git commit -m "docs(architecture): document frontend task zones"
```

Expected: commit succeeds and contains only `docs/architecture.md`.

---

## Task 8: Full Verification And Visual Review

**Files:**
- Create/update screenshots under `output/playwright/`

- [ ] **Step 1: Run full frontend test suite**

Run:

```bash
cd frontend && pnpm test
```

Expected: all Vitest files pass.

- [ ] **Step 2: Run production build**

Run:

```bash
cd frontend && pnpm build
```

Expected: `vue-tsc -b && vite build` exits 0.

- [ ] **Step 3: Run whitespace check**

Run:

```bash
git diff --check
```

Expected: no output.

- [ ] **Step 4: Start or reuse the local frontend**

If a dev server is already running on `http://127.0.0.1:5173`, reuse it. Otherwise run:

```bash
cd frontend && pnpm dev --host 127.0.0.1
```

Expected: Vite serves the app on a local port.

- [ ] **Step 5: Capture desktop and mobile screenshots**

Run with the Playwright CLI wrapper:

```bash
export CODEX_HOME="${CODEX_HOME:-$HOME/.codex}"
export PWCLI="$CODEX_HOME/skills/playwright/scripts/playwright_cli.sh"
mkdir -p output/playwright
"$PWCLI" open http://127.0.0.1:5173/
"$PWCLI" resize 1440 900
"$PWCLI" screenshot --filename output/playwright/task-zone-phase1-home-desktop.png --full-page
"$PWCLI" goto http://127.0.0.1:5173/user
"$PWCLI" screenshot --filename output/playwright/task-zone-phase1-user-desktop.png --full-page
"$PWCLI" goto http://127.0.0.1:5173/events
"$PWCLI" screenshot --filename output/playwright/task-zone-phase1-events-desktop.png --full-page
"$PWCLI" resize 375 812
"$PWCLI" goto http://127.0.0.1:5173/
"$PWCLI" screenshot --filename output/playwright/task-zone-phase1-home-mobile.png --full-page
```

Expected:

- Desktop home shows grouped sidebar and My AI Usage.
- Desktop setup page shows Your account, AI access, Access group, Command reference.
- Desktop usage records page shows summary columns, not raw source basename in the default table.
- Mobile home shows top bar, no overlapping cards, and stacked content.

- [ ] **Step 6: Commit final verification artifacts only if desired**

Do not commit `output/playwright/` unless the branch convention requires review screenshots. If screenshots are committed, use:

```bash
git add output/playwright/task-zone-phase1-*.png
git commit -m "test(frontend): add task-zone review screenshots"
```

Expected: commit succeeds only when screenshots are intentionally part of the review artifact.

---

## Final Verification Checklist

- [ ] `cd frontend && pnpm test` passes.
- [ ] `cd frontend && pnpm build` passes.
- [ ] `git diff --check` passes.
- [ ] Desktop screenshot for `/` reviewed.
- [ ] Mobile screenshot for `/` reviewed.
- [ ] Desktop screenshot for `/user` reviewed.
- [ ] Desktop screenshot for `/events` reviewed.
- [ ] No backend API contracts changed.
- [ ] No real users, real company emails, real passwords, real tokens, real API keys, or real group names added to tests or docs.
- [ ] `docs/architecture.md` reflects the Phase 1 frontend task zones.
