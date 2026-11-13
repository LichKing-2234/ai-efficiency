import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { nextTick } from 'vue'
import UserUsageDashboard from '@/components/user/usage/UserUsageDashboard.vue'
import KiroCreditsDashboard from '@/components/user/usage/KiroCreditsDashboard.vue'
import { setLocale } from '@/i18n'
import { getUserKiroUsage, getUserUsageDashboard, getUserUsageGroupPoolUsage, getUserUsageGroupQuotas } from '@/api/userUsage'

vi.mock('@/api/userUsage', () => ({
  getUserUsageDashboard: vi.fn(),
  getUserUsageGroupQuotas: vi.fn(),
  getUserUsageGroupPoolUsage: vi.fn(),
  getUserKiroUsage: vi.fn(),
}))

vi.mock('@/components/charts/LineChartCanvas.vue', () => ({ __esModule: true, default: { name: 'LineChartCanvas', template: '<div data-testid="line-chart-stub" />' } }))
vi.mock('@/components/charts/DoughnutChartCanvas.vue', () => ({ __esModule: true, default: { name: 'DoughnutChartCanvas', template: '<div data-testid="doughnut-chart-stub" />' } }))

vi.mock('@/api/teamUsage', () => ({
  getTeamUsageSubjectDashboard: vi.fn(),
  updateTeamUsageRateMultiplier: vi.fn(),
}))

vi.mock('@/api/quotaReset', () => ({
  getQuotaResetOptions: vi.fn(),
  createQuotaResetRequest: vi.fn(),
}))

const tokenSnapshot = {
  configured: true,
  range: { start_date: '2026-11-07', end_date: '2026-11-13', granularity: 'day', timezone: 'UTC' },
  stats: {
    total_requests: 10,
    total_input_tokens: 1000,
    total_output_tokens: 100,
    total_cache_creation_tokens: 0,
    total_cache_read_tokens: 0,
    total_tokens: 1100,
    total_cost: 0.1,
    total_actual_cost: 0.1,
    today_requests: 1,
    today_input_tokens: 100,
    today_output_tokens: 10,
    today_cache_creation_tokens: 0,
    today_cache_read_tokens: 0,
    today_tokens: 110,
    today_cost: 0.01,
    today_actual_cost: 0.01,
    average_duration_ms: 120,
    rpm: 1,
    tpm: 2,
  },
  trend: [
    {
      date: '2026-11-13',
      requests: 10,
      input_tokens: 1000,
      output_tokens: 100,
      cache_creation_tokens: 0,
      cache_read_tokens: 0,
      total_tokens: 1100,
      cost: 0.1,
      actual_cost: 0.1,
    },
  ],
  models: [],
}

const kiroSnapshot = {
  available: true,
  configured: true,
  key_found: true,
  status: 'ok',
  as_of: '2026-11-13T07:36:48Z',
  credits: 45.63,
  credits_saved: 23.3,
  requests: 89,
  input_tokens: 5712697,
  output_tokens: 33386,
  lifetime_credits: 45.63,
  lifetime_requests: 89,
  by_model: [
    { model: 'claude-opus-5.5', requests: 83, credits: 45.38, input_tokens: 5653919, output_tokens: 33278 },
  ],
  by_day: [{ date: '2026-11-13', requests: 89, credits: 45.63 }],
  partial: false,
}

function quotaResponse() {
  return {
    data: {
      data: {
        group_quotas: { status: 'empty', unit_label: 'USD', groups: [] },
        quota_freshness: { as_of: '2026-11-15T08:00:00Z', cache_status: 'uncached', source_status: 'ok' },
      },
    },
  }
}

function poolResponse() {
  return {
    data: {
      data: {
        group_pool_usage: { status: 'empty', groups: [] },
        pool_usage_freshness: { as_of: null, cache_status: 'uncached', source_status: 'ok' },
      },
    },
  }
}

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/usage', name: 'Usage', component: UserUsageDashboard },
      { path: '/user', name: 'User', component: { template: '<div>User</div>' } },
    ],
  })
}

async function settleAsyncComponents() {
  for (let index = 0; index < 3; index += 1) {
    await flushPromises()
    await new Promise((resolve) => setTimeout(resolve, 0))
  }
}

async function mountWithSwitch(kiro: unknown) {
  ;(getUserKiroUsage as any).mockResolvedValue({ data: { data: kiro } })
  const router = createTestRouter()
  await router.push('/usage')
  await router.isReady()
  const wrapper = mount(UserUsageDashboard, {
    props: { unitSwitch: true },
    global: { plugins: [router] },
  })
  await flushPromises()
  await nextTick()
  await vi.dynamicImportSettled()
  await settleAsyncComponents()
  return wrapper
}

describe('usage unit switch', () => {
  beforeEach(async () => {
    vi.clearAllMocks()
    await setLocale('en-US')
    ;(getUserUsageDashboard as any).mockReset().mockResolvedValue({ data: { data: tokenSnapshot } })
    ;(getUserUsageGroupQuotas as any).mockReset().mockResolvedValue(quotaResponse())
    ;(getUserUsageGroupPoolUsage as any).mockReset().mockResolvedValue(poolResponse())
  })

  it('switches the personal usage view away from token metrics', async () => {
    const wrapper = await mountWithSwitch(kiroSnapshot)

    const switchControl = wrapper.find('[data-testid="usage-unit-switch"]')
    expect(switchControl.exists()).toBe(true)
    expect(wrapper.text()).toContain('Avg Response')

    await switchControl.findAll('label')[1].find('input[type="radio"]').setValue(true)
    await nextTick()
    await settleAsyncComponents()

    expect(getUserKiroUsage).toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('Avg Response')
    expect(wrapper.text()).toContain('Credits')
    await vi.dynamicImportSettled()
    wrapper.unmount()
  })

  it('renders credits metrics with token estimates labelled as estimates', async () => {
    const wrapper = mount(KiroCreditsDashboard, {
      props: { snapshot: kiroSnapshot as any, rangeLabel: '7 Days' },
      global: { stubs: { ElTable: true, ElTableColumn: true, ElTooltip: true } },
    })
    await settleAsyncComponents()

    expect(wrapper.text()).toContain('45.63')
    expect(wrapper.text()).toContain('Kiro requests')
    expect(wrapper.text()).toContain('Credits per request')
    expect(wrapper.text()).toContain('Tokens (est.)')
    expect(wrapper.text()).not.toContain('$')
    await vi.dynamicImportSettled()
    wrapper.unmount()
  })

  it('renders the credits empty state when the user has no Kiro calls', async () => {
    const wrapper = mount(KiroCreditsDashboard, {
      props: {
        snapshot: {
          ...kiroSnapshot,
          available: false,
          key_found: false,
          status: 'no_key',
          credits: 0,
          requests: 0,
          by_model: [],
          by_day: [],
        } as any,
        rangeLabel: '7 Days',
      },
    })
    await settleAsyncComponents()

    expect(wrapper.text()).toContain('This user has no Kiro CLI calls yet')
    await vi.dynamicImportSettled()
    wrapper.unmount()
  })
})
