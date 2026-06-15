# AI 使用中心生命周期分流首页 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rework `/` into a lifecycle-aware personal home so new users are pushed toward `AI 接入与配置`, established users land on usage first, and the embedded home usage view removes cost from the first-row summary.

**Architecture:** Keep the current frontend-only route and API surface. Use `getUserProviders()` plus the existing usage snapshot to classify homepage state, render a default-expanded or default-collapsed guide card accordingly, and keep the embedded usage dashboard as the mature-user primary module without changing backend contracts.

**Tech Stack:** Vue 3, TypeScript, TailwindCSS, Vitest, Vue Test Utils, Vue Router, existing `frontend/src/i18n.ts`, Markdown docs.

**Status:** Implemented and verified on 2026-06-15. Homepage lifecycle branching now defaults new users to the AI setup guide, defaults established users to usage-first home content, and hides cost from the embedded home usage summary while leaving the standalone usage page unchanged.

---

## Scope Boundary

Included:

- Homepage lifecycle state classification in `DashboardView.vue`.
- New-user expanded guide card and established-user collapsed guide summary.
- Reprioritized homepage layout that demotes the old `Platform Signals / Setup Status / Next Steps` pattern.
- Embedded usage dashboard home-mode hiding cost cards while preserving request/token/latency/trend/model views.
- Updated bilingual home copy, homepage tests, and architecture docs.

Excluded:

- Backend API changes.
- `/user` behavior changes.
- Changes to the standalone usage wrapper route beyond keeping existing behavior valid.
- Changes to provider/group credential contracts.
- New routes or local preference persistence for the guide card open/closed state.

## File Map

Implementation files:

- Modify: `frontend/src/views/DashboardView.vue`
  Add lifecycle classification, guide-card state, new layout order, and reduced dependence on `listEvents` for homepage identity.
- Modify: `frontend/src/components/user/usage/UserUsageDashboard.vue`
  Accept a home-embedded mode that can reuse a caller-provided initial snapshot and render the embedded variant without first-row cost cards.
- Modify: `frontend/src/components/user/usage/UsageStatsCards.vue`
  Add an option to omit the cost summary card while keeping range request/token/latency cards stable.
- Modify: `frontend/src/i18n.ts`
  Replace legacy homepage copy with lifecycle-based guide-card, mature-user, and degraded-state wording in both locales.
- Modify: `docs/architecture.md`
  Replace the current “personal AI usage center” homepage description with the lifecycle-aware homepage boundary and guide-card summary.

Tests:

- Modify: `frontend/src/__tests__/dashboard-view.test.ts`
  Add lifecycle-state regression coverage for new-user, setup-ready-but-no-usage, established-user, and degraded-error states.
- Modify: `frontend/src/__tests__/user-usage-view.test.ts`
  Keep standalone usage wrapper behavior green while adding coverage that home-embedded cost hiding does not leak into the standalone page.

## Task 1: Redesign Homepage Tests Around Lifecycle States

**Files:**
- Modify: `frontend/src/__tests__/dashboard-view.test.ts`
- Test: `frontend/src/__tests__/user-usage-view.test.ts`

- [x] **Step 1: Write failing homepage lifecycle tests in `frontend/src/__tests__/dashboard-view.test.ts`**

Add these tests near the existing home-page assertions so the new state model is enforced before implementation:

```ts
it('shows the expanded guide card for users without any reusable AI access', async () => {
  const { getDashboard } = await import('@/api/efficiency')
  const { getUserProviders } = await import('@/api/user')
  const { getUserUsageDashboard } = await import('@/api/userUsage')
  ;(getDashboard as any).mockResolvedValue({ data: { data: { total_repos: 0, tracked_workflows: 0, total_ai_prs: 0 } } })
  ;(getUserProviders as any).mockResolvedValue({
    data: {
      data: {
        providers: [
          {
            id: 1,
            name: 'prod',
            display_name: 'Production',
            base_url: 'https://relay.example.com',
            default_model: 'gpt-5.4',
            is_primary: true,
            groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai', credential: { state: 'missing' } }],
          },
        ],
      },
    },
  })
  ;(getUserUsageDashboard as any).mockResolvedValue({
    data: {
      data: {
        configured: false,
        range: { start_date: '2026-06-09', end_date: '2026-06-15', granularity: 'day', timezone: 'Asia/Shanghai' },
        stats: null,
        trend: [],
        models: [],
      },
    },
  })

  const router = createTestRouter()
  await router.push('/')
  await router.isReady()
  const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
  await flushPromises()

  expect(wrapper.text()).toContain('Complete AI setup first')
  expect(wrapper.text()).toContain('Go to AI Setup & Configuration')
  expect(wrapper.text()).not.toContain('Platform Signals')
})

it('keeps the guide card expanded when AI access exists but no effective usage data is available', async () => {
  const { getDashboard } = await import('@/api/efficiency')
  const { getUserProviders } = await import('@/api/user')
  const { getUserUsageDashboard } = await import('@/api/userUsage')
  ;(getDashboard as any).mockResolvedValue({ data: { data: { total_repos: 0, tracked_workflows: 0, total_ai_prs: 0 } } })
  ;(getUserProviders as any).mockResolvedValue({
    data: {
      data: {
        providers: [
          {
            id: 1,
            name: 'prod',
            display_name: 'Production',
            base_url: 'https://relay.example.com',
            default_model: 'gpt-5.4',
            is_primary: true,
            groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai', credential: { state: 'existing_hidden' } }],
          },
        ],
      },
    },
  })
  ;(getUserUsageDashboard as any).mockResolvedValue({
    data: {
      data: {
        configured: true,
        range: { start_date: '2026-06-09', end_date: '2026-06-15', granularity: 'day', timezone: 'Asia/Shanghai' },
        stats: {
          total_requests: 0,
          total_input_tokens: 0,
          total_output_tokens: 0,
          total_cache_creation_tokens: 0,
          total_cache_read_tokens: 0,
          total_tokens: 0,
          total_cost: 0,
          total_actual_cost: 0,
          today_requests: 0,
          today_input_tokens: 0,
          today_output_tokens: 0,
          today_cache_creation_tokens: 0,
          today_cache_read_tokens: 0,
          today_tokens: 0,
          today_cost: 0,
          today_actual_cost: 0,
          average_duration_ms: 0,
          rpm: 0,
          tpm: 0,
        },
        trend: [],
        models: [],
      },
    },
  })

  const router = createTestRouter()
  await router.push('/')
  await router.isReady()
  const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
  await flushPromises()

  expect(wrapper.text()).toContain('AI setup is ready, start your first usage')
  expect(wrapper.text()).toContain('Go to AI Setup & Configuration')
})

it('shows usage first and collapses the guide card for established users', async () => {
  const { getDashboard } = await import('@/api/efficiency')
  const { getUserProviders } = await import('@/api/user')
  const { getUserUsageDashboard } = await import('@/api/userUsage')
  ;(getDashboard as any).mockResolvedValue({ data: { data: { total_repos: 3, tracked_workflows: 2, total_ai_prs: 5 } } })
  ;(getUserProviders as any).mockResolvedValue({
    data: {
      data: {
        providers: [
          {
            id: 1,
            name: 'prod',
            display_name: 'Production',
            base_url: 'https://relay.example.com',
            default_model: 'gpt-5.4',
            is_primary: true,
            groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai', credential: { state: 'existing_hidden' } }],
          },
        ],
      },
    },
  })
  ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })

  const router = createTestRouter()
  await router.push('/')
  await router.isReady()
  const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
  await flushPromises()

  expect(wrapper.text()).toContain('AI setup completed')
  expect(wrapper.text()).toContain('View setup guidance')
  expect(wrapper.text()).toContain('My Usage')
  expect(wrapper.text()).not.toContain('7 Days Cost')
  expect(wrapper.text()).not.toContain('$0.6000')
})

it('shows a degraded-state warning instead of misclassifying provider or usage failures as setup-needed', async () => {
  const { getDashboard } = await import('@/api/efficiency')
  const { getUserProviders } = await import('@/api/user')
  const { getUserUsageDashboard } = await import('@/api/userUsage')
  ;(getDashboard as any).mockResolvedValue({ data: { data: { total_repos: 0, tracked_workflows: 0, total_ai_prs: 0 } } })
  ;(getUserProviders as any).mockRejectedValue(new Error('provider network error'))
  ;(getUserUsageDashboard as any).mockRejectedValue(new Error('usage network error'))

  const router = createTestRouter()
  await router.push('/')
  await router.isReady()
  const wrapper = mount(DashboardView, { global: { plugins: [createPinia(), router] } })
  await flushPromises()

  expect(wrapper.text()).toContain('Unable to load your current homepage status')
  expect(wrapper.text()).not.toContain('Complete AI setup first')
})
```

- [x] **Step 2: Run the focused homepage tests to verify they fail**

Run: `cd frontend && pnpm test -- src/__tests__/dashboard-view.test.ts`

Expected: FAIL because the current homepage still renders `Platform Signals / Next Steps`, does not classify lifecycle state, and still shows the cost card inside embedded usage.

- [x] **Step 3: Add a standalone usage regression that proves home-only cost hiding will not change the full usage page**

Append this test to `frontend/src/__tests__/user-usage-view.test.ts`:

```ts
it('keeps cost visible on the standalone usage page', async () => {
  const { getUserUsageDashboard } = await import('@/api/userUsage')
  ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: snapshot } })
  const router = createRouterForUsage()
  await router.push('/user/usage')
  await router.isReady()
  const wrapper = mount(UsageView, { global: { plugins: [createPinia(), router] } })
  await flushPromises()

  expect(wrapper.text()).toContain('7 Days Cost')
  expect(wrapper.text()).toContain('$0.6000')
})
```

- [x] **Step 4: Run the standalone usage test to verify it passes before implementation**

Run: `cd frontend && pnpm test -- src/__tests__/user-usage-view.test.ts`

Expected: PASS, proving the current standalone usage page still exposes the cost card and giving a baseline guard for later home-only branching.

- [ ] **Step 5: Commit the red/guard test work**

```bash
git add frontend/src/__tests__/dashboard-view.test.ts frontend/src/__tests__/user-usage-view.test.ts
git commit -m "test(frontend): define lifecycle homepage states"
```

## Task 2: Implement Lifecycle Homepage State, Guide Card, And Embedded Usage Mode

**Files:**
- Modify: `frontend/src/views/DashboardView.vue`
- Modify: `frontend/src/components/user/usage/UserUsageDashboard.vue`
- Modify: `frontend/src/components/user/usage/UsageStatsCards.vue`
- Modify: `frontend/src/i18n.ts`
- Test: `frontend/src/__tests__/dashboard-view.test.ts`
- Test: `frontend/src/__tests__/user-usage-view.test.ts`

- [x] **Step 1: Add the minimal embedded usage option to hide the cost card without touching standalone usage**

Update `frontend/src/components/user/usage/UsageStatsCards.vue` props and template:

```ts
const props = withDefaults(defineProps<{
  stats: UserUsageDashboardStats | null
  trend: UserUsageTrendPoint[]
  rangeLabel: string
  hideCost?: boolean
}>(), {
  hideCost: false,
})
```

Replace the first section with a conditional and keep the remaining request/token/latency cards unchanged:

```vue
<section
  v-if="!props.hideCost"
  class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm"
>
  <p class="text-xs font-medium uppercase text-gray-500">{{ t('usageDashboard.rangeCost', { range: rangeLabel }) }}</p>
  <p class="mt-2 text-2xl font-semibold text-gray-900">
    ${{ formatCost(rangeTotals.actualCost) }}
  </p>
  <p class="mt-1 text-xs text-gray-500">{{ t('usageDashboard.standard') }}: ${{ formatCost(rangeTotals.cost) }}</p>
</section>
```

- [x] **Step 2: Teach `UserUsageDashboard.vue` to render a home-embedded mode without rebreaking standalone usage**

Add props and computed branches:

```ts
const props = withDefaults(defineProps<{
  embedded?: boolean
  homeMode?: boolean
}>(), {
  embedded: false,
  homeMode: false,
})

const showCostCard = computed(() => !props.homeMode)
```

Pass the new flag to stats cards:

```vue
<UsageStatsCards
  :stats="snapshot?.stats ?? null"
  :trend="snapshot?.trend ?? []"
  :range-label="snapshotRangeLabel"
  :hide-cost="!showCostCard"
/>
```

Keep the rest of the load flow untouched in this step.

- [x] **Step 3: Add homepage lifecycle classification helpers in `frontend/src/views/DashboardView.vue`**

Replace the current `listEvents`-driven setup summary model with explicit lifecycle state:

```ts
import { getUserUsageDashboard } from '@/api/userUsage'
import type {
  DashboardData,
  UserProviderSummary,
  UserUsageDashboardSnapshot,
} from '@/types'

type HomeLifecycleState =
  | 'needs_setup'
  | 'setup_ready_waiting_for_first_usage'
  | 'established_user'
  | 'degraded_error'

const usageSnapshot = ref<UserUsageDashboardSnapshot | null>(null)
const usageLoadFailed = ref(false)

function hasReusableAiAccess(providers: UserProviderSummary[]) {
  return providers.some((provider) =>
    provider.groups.some((group) => group.credential.state === 'existing_hidden'),
  )
}

function hasEffectiveUsage(snapshot: UserUsageDashboardSnapshot | null) {
  if (!snapshot || snapshot.configured !== true) return false
  if ((snapshot.stats?.total_requests ?? 0) > 0) return true
  if ((snapshot.stats?.total_tokens ?? 0) > 0) return true
  if (snapshot.trend.some((point) => point.requests > 0 || point.total_tokens > 0)) return true
  return snapshot.models.some((model) => model.requests > 0 || model.total_tokens > 0)
}

const aiAccessReady = computed(() => !providersLoadFailed.value && hasReusableAiAccess(userProviders.value))
const usageDataReady = computed(() => hasEffectiveUsage(usageSnapshot.value))
const homeLifecycleState = computed<HomeLifecycleState>(() => {
  if (providersLoadFailed.value || usageLoadFailed.value) return 'degraded_error'
  if (!aiAccessReady.value) return 'needs_setup'
  if (!usageDataReady.value) return 'setup_ready_waiting_for_first_usage'
  return 'established_user'
})
const guideExpandedByDefault = computed(() => homeLifecycleState.value !== 'established_user')
```

- [x] **Step 4: Load the homepage usage snapshot in `DashboardView.vue` and stop using `listEvents` to decide lifecycle**

Replace the existing `Promise.allSettled` block with:

```ts
onMounted(async () => {
  const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone
  const [dashboardResult, providersResult, usageResult] = await Promise.allSettled([
    getDashboard(),
    getUserProviders(),
    getUserUsageDashboard({
      start_date: new Date(Date.now() - 6 * 24 * 60 * 60 * 1000).toISOString().slice(0, 10),
      end_date: new Date().toISOString().slice(0, 10),
      granularity: 'day',
      timezone,
    }),
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

  if (usageResult.status === 'fulfilled') {
    usageSnapshot.value = usageResult.value.data.data ?? null
  } else {
    usageLoadFailed.value = true
    usageSnapshot.value = null
  }

  loading.value = false
})
```

Remove the `listEvents` import, `recentEvents`, `eventsLoadFailed`, and `hasRecentUsage` branches from homepage identity logic. Leave event navigation as a static link in the new layout.

- [x] **Step 5: Replace the homepage template with the lifecycle guide-card-first layout**

In `frontend/src/views/DashboardView.vue`, replace the existing hero + platform-signal + setup-status + next-steps structure with:

```vue
<section class="space-y-4">
  <div
    class="rounded-xl border border-slate-200 bg-white p-5 shadow-sm"
    :data-testid="`home-guide-${homeLifecycleState}`"
  >
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <p class="text-sm font-semibold text-cyan-700">{{ t('home.title') }}</p>
        <h1 class="mt-2 text-2xl font-semibold text-slate-950">
          {{ homeLifecycleState === 'needs_setup'
            ? t('home.guideNeedsSetupTitle')
            : homeLifecycleState === 'setup_ready_waiting_for_first_usage'
              ? t('home.guideWaitingUsageTitle')
              : homeLifecycleState === 'degraded_error'
                ? t('home.guideErrorTitle')
                : t('home.guideReadyTitle') }}
        </h1>
        <p class="mt-2 max-w-3xl text-sm text-slate-600">
          {{ homeLifecycleState === 'needs_setup'
            ? t('home.guideNeedsSetupHelp')
            : homeLifecycleState === 'setup_ready_waiting_for_first_usage'
              ? t('home.guideWaitingUsageHelp')
              : homeLifecycleState === 'degraded_error'
                ? t('home.guideErrorHelp')
                : t('home.guideReadyHelp') }}
        </p>
      </div>
      <RouterLink
        v-if="homeLifecycleState !== 'established_user'"
        to="/user"
        class="inline-flex rounded-md bg-cyan-700 px-4 py-2 text-sm font-medium text-white hover:bg-cyan-800"
      >
        {{ t('home.openSetup') }}
      </RouterLink>
      <button
        v-else
        type="button"
        class="inline-flex rounded-md border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
      >
        {{ t('home.viewSetupGuidance') }}
      </button>
    </div>

    <div v-if="guideExpandedByDefault" class="mt-4 grid gap-3 sm:grid-cols-3">
      <div class="rounded-lg bg-slate-50 p-4 text-sm">
        <div class="font-medium text-slate-950">{{ t('home.guideSignalAiAccess') }}</div>
        <div class="mt-2 text-slate-600">{{ aiAccessReady ? t('home.guideDone') : t('home.guideTodo') }}</div>
      </div>
      <div class="rounded-lg bg-slate-50 p-4 text-sm">
        <div class="font-medium text-slate-950">{{ t('home.guideSignalReporting') }}</div>
        <div class="mt-2 text-slate-600">{{ codeReportingActive ? t('home.guideDone') : t('home.guideTodo') }}</div>
      </div>
      <div class="rounded-lg bg-slate-50 p-4 text-sm">
        <div class="font-medium text-slate-950">{{ t('home.guideSignalUsage') }}</div>
        <div class="mt-2 text-slate-600">{{ usageDataReady ? t('home.guideDone') : t('home.guideWaiting') }}</div>
      </div>
    </div>
  </div>

  <div class="grid gap-3 sm:grid-cols-2">
    <RouterLink to="/events" class="rounded-lg border border-slate-200 bg-white p-4 text-sm shadow-sm hover:border-slate-400">
      <div class="font-semibold text-slate-950">{{ t('home.nextRecordsTitle') }}</div>
      <p class="mt-2 text-xs text-slate-600">{{ t('home.nextRecordsText') }}</p>
    </RouterLink>
    <RouterLink to="/repos" class="rounded-lg border border-slate-200 bg-white p-4 text-sm shadow-sm hover:border-slate-400">
      <div class="font-semibold text-slate-950">{{ t('home.nextRepoTitle') }}</div>
      <p class="mt-2 text-xs text-slate-600">{{ t('home.nextRepoText') }}</p>
    </RouterLink>
  </div>
</section>

<UserUsageDashboard embedded home-mode />
```

In this step, delete the old `Platform Signals`, `Setup Status`, and `Next Steps` sections entirely.

- [x] **Step 6: Add homepage lifecycle copy to `frontend/src/i18n.ts`**

Add and use these keys in both locales:

```ts
'home.guideNeedsSetupTitle': 'Complete AI setup first',
'home.guideNeedsSetupHelp': 'You do not have usable AI access yet. Start in AI Setup & Configuration, then come back once your tools are ready.',
'home.guideWaitingUsageTitle': 'AI setup is ready, start your first usage',
'home.guideWaitingUsageHelp': 'Your AI access is ready, but there is no effective usage data yet. Finish local tool setup and send a real request.',
'home.guideReadyTitle': 'AI setup completed',
'home.guideReadyHelp': 'Your AI setup is ready. Expand this guidance any time if you need to review setup status or reconnect tools.',
'home.guideErrorTitle': 'Unable to load your current homepage status',
'home.guideErrorHelp': 'Some homepage data failed to load. Retry after checking AI setup or provider connectivity.',
'home.guideSignalAiAccess': 'AI setup',
'home.guideSignalReporting': 'Code association',
'home.guideSignalUsage': 'Recent usage',
'home.guideDone': 'Done',
'home.guideTodo': 'Needs attention',
'home.guideWaiting': 'Waiting for first usage',
'home.viewSetupGuidance': 'View setup guidance',
```

Also rewrite the existing homepage title/subtitle keys away from the old platform-dashboard framing where needed.

- [x] **Step 7: Run the focused tests to verify lifecycle homepage behavior passes**

Run: `cd frontend && pnpm test -- src/__tests__/dashboard-view.test.ts src/__tests__/user-usage-view.test.ts`

Expected: PASS, including the new lifecycle-state assertions and the standalone usage cost guard.

- [ ] **Step 8: Commit the homepage implementation**

```bash
git add frontend/src/views/DashboardView.vue frontend/src/components/user/usage/UserUsageDashboard.vue frontend/src/components/user/usage/UsageStatsCards.vue frontend/src/i18n.ts frontend/src/__tests__/dashboard-view.test.ts frontend/src/__tests__/user-usage-view.test.ts
git commit -m "feat(frontend): add lifecycle-aware AI usage home"
```

## Task 3: Update Architecture Docs And Run Full Verification

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/plans/2026-06-15-ai-usage-center-lifecycle-home.md`
- Test: `frontend/src/__tests__/dashboard-view.test.ts`
- Test: `frontend/src/__tests__/user-usage-view.test.ts`

- [x] **Step 1: Update `docs/architecture.md` to describe the lifecycle-aware homepage**

Replace the current `My Work` homepage description with wording like:

```md
- `My Work`: `/` is now a lifecycle-aware personal home. New users land on an expanded guide card that pushes them to `/user` to complete AI setup; established users land on the embedded usage dashboard first, with the setup guide collapsed by default. `Usage Records` and `Code Repositories` remain secondary drill-down paths rather than the homepage's primary narrative.
```

If the current notes mention `Platform Signals`, `Setup Status`, or `Next Steps` as the homepage's primary structure, remove or rewrite those references.

- [x] **Step 2: Mark the plan status accurately before final verification**

At the top of this plan, add a status line only after code and docs work are complete. Use:

```md
**Status:** Implementation in progress; final verification steps remain unchecked.
```

Do not mark later verification checkboxes yet.

- [x] **Step 3: Run the full frontend verification commands**

Run:

```bash
cd frontend && pnpm exec vue-tsc -b --pretty false
cd frontend && pnpm test
cd frontend && pnpm build
```

Expected:

- `vue-tsc` exits 0.
- `pnpm test` exits 0.
- `pnpm build` exits 0.

- [x] **Step 4: Update the plan status and verification checkboxes with actual results**

After the commands pass, update this plan file:

```md
**Status:** Implemented and verified on 2026-06-15. Homepage lifecycle branching now defaults new users to the AI setup guide, defaults established users to usage-first home content, and hides cost from the embedded home usage summary while leaving the standalone usage page unchanged.
```

Also add checked verification bullets under a `## Final Verification` section:

```md
## Final Verification

- [x] Run `cd frontend && pnpm exec vue-tsc -b --pretty false`.
- [x] Run `cd frontend && pnpm test`.
- [x] Run `cd frontend && pnpm build`.
- [x] Update `docs/architecture.md` for the lifecycle-aware homepage boundary.
```

- [ ] **Step 5: Commit docs and verification updates**

```bash
git add docs/architecture.md docs/superpowers/plans/2026-06-15-ai-usage-center-lifecycle-home.md
git commit -m "docs(architecture): describe lifecycle AI usage home"
```

## Self-Review

Spec coverage checklist:

- Lifecycle state model: Task 2 steps 3-5.
- New-user expanded guide card: Task 2 steps 5-6.
- Established-user collapsed guide summary: Task 2 steps 5-6.
- Embedded homepage usage without cost summary: Task 2 steps 1-2.
- Standalone usage unchanged: Task 1 steps 3-4 and Task 2 step 7.
- Architecture update: Task 3 step 1.
- Full verification: Task 3 steps 3-4.

Placeholder scan:

- No `TODO`, `TBD`, or “similar to above” shortcuts remain.
- All test, implementation, and verification steps include exact files and commands.

Type consistency:

- `HomeLifecycleState`, `aiAccessReady`, `usageDataReady`, and `guideExpandedByDefault` names are reused consistently across template, tests, and plan text.
- `homeMode` is the single `UserUsageDashboard` prop for homepage-specific presentation.

## Final Verification

- [x] Run `cd frontend && pnpm exec vue-tsc -b --pretty false`.
- [x] Run `cd frontend && pnpm test`.
- [x] Run `cd frontend && pnpm build`.
- [x] Update `docs/architecture.md` for the lifecycle-aware homepage boundary.
