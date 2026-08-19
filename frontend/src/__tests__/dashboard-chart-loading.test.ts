import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import DashboardView from '@/views/DashboardView.vue'
import { setLocale } from '@/i18n'
import { getUserUsageDashboard } from '@/api/userUsage'

vi.mock('@/api/user', () => ({
  getUserProviders: vi.fn(),
}))

vi.mock('@/api/userUsage', () => ({
  getUserUsageDashboard: vi.fn(),
  getUserUsageGroupQuotas: vi.fn(() => Promise.resolve({
    data: {
      data: {
        group_quotas: { status: 'empty', unit_label: 'USD', groups: [] },
        quota_freshness: { as_of: '2026-07-15T08:00:00Z', cache_status: 'uncached', source_status: 'ok' },
      },
    },
  })),
  getUserUsageGroupPoolUsage: vi.fn(() => Promise.resolve({
    data: {
      data: {
        group_pool_usage: { status: 'empty', groups: [] },
        pool_usage_freshness: { as_of: null, cache_status: 'uncached', source_status: 'ok' },
      },
    },
  })),
}))

vi.mock('@/api/teamUsage', () => ({
  getTeamUsageScope: vi.fn(() => Promise.resolve({
    data: { data: { is_representative: false, departments: [] } },
  })),
  listTeamUsageSubjects: vi.fn(),
  getTeamUsageSubjectDashboard: vi.fn(),
  getTeamUsageAudit: vi.fn(),
  updateTeamUsageRateMultiplier: vi.fn(),
}))

vi.mock('@/api/quotaReset', () => ({
  getQuotaResetOptions: vi.fn(),
  createQuotaResetRequest: vi.fn(),
}))

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  getMe: vi.fn(),
  devLogin: vi.fn(),
}))

const canvasModules = vi.hoisted(() => {
  let lineGate = Promise.resolve()
  let doughnutGate = Promise.resolve()
  let releaseLine = () => {}
  let releaseDoughnut = () => {}

  function defer() {
    lineGate = new Promise<void>((resolve) => { releaseLine = resolve })
    doughnutGate = new Promise<void>((resolve) => { releaseDoughnut = resolve })
  }

  function resolveLine() {
    releaseLine()
    lineGate = Promise.resolve()
    releaseLine = () => {}
  }

  function resolveDoughnut() {
    releaseDoughnut()
    doughnutGate = Promise.resolve()
    releaseDoughnut = () => {}
  }

  return {
    lineLoads: 0,
    doughnutLoads: 0,
    defer,
    waitForLine: () => lineGate,
    waitForDoughnut: () => doughnutGate,
    resolveLine,
    resolveDoughnut,
    releaseAll() {
      resolveLine()
      resolveDoughnut()
    },
  }
})

vi.mock('@/components/charts/LineChartCanvas.vue', async () => {
  canvasModules.lineLoads += 1
  await canvasModules.waitForLine()
  return {
    __esModule: true,
    default: {
      props: ['data', 'options'],
      template: '<div data-test="line-chart" :data-chart="JSON.stringify(data)" :data-options="JSON.stringify(options)" />',
    },
  }
})

vi.mock('@/components/charts/DoughnutChartCanvas.vue', async () => {
  canvasModules.doughnutLoads += 1
  await canvasModules.waitForDoughnut()
  return {
    __esModule: true,
    default: {
      props: ['data', 'options'],
      template: '<div data-test="doughnut-chart" :data-chart="JSON.stringify(data)" :data-options="JSON.stringify(options)" />',
    },
  }
})

const usageSnapshot = {
  configured: true,
  range: { start_date: '2026-06-01', end_date: '2026-06-06', granularity: 'day', timezone: 'Asia/Shanghai' },
  stats: {
    total_requests: 100,
    total_input_tokens: 10000,
    total_output_tokens: 5000,
    total_cache_creation_tokens: 200,
    total_cache_read_tokens: 300,
    total_tokens: 15500,
    total_cost: 2.5,
    total_actual_cost: 2,
    today_requests: 12,
    today_input_tokens: 1000,
    today_output_tokens: 500,
    today_cache_creation_tokens: 20,
    today_cache_read_tokens: 30,
    today_tokens: 1550,
    today_cost: 0.25,
    today_actual_cost: 0.2,
    average_duration_ms: 850,
    rpm: 2,
    tpm: 3000,
  },
  trend: [{ date: '2026-06-06', requests: 12, input_tokens: 1000, output_tokens: 500, cache_creation_tokens: 20, cache_read_tokens: 30, total_tokens: 1550, cost: 0.25, actual_cost: 0.2 }],
  models: [{ model: 'example-model', requests: 12, input_tokens: 1000, output_tokens: 500, cache_creation_tokens: 20, cache_read_tokens: 30, total_tokens: 1550, cost: 0.25, actual_cost: 0.2 }],
  group_quotas: { status: 'empty', unit_label: 'USD', message: '', groups: [] },
  usage_freshness: {
    as_of: '2026-07-15T08:00:00Z',
    fresh_until: '2026-07-15T08:00:27Z',
    stale_until: '2026-07-15T08:01:48Z',
    cache_status: 'miss',
    source_status: 'ok',
  },
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => {
    resolve = res
  })
  return { promise, resolve }
}

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: DashboardView },
      { path: '/usage', component: DashboardView },
      { path: '/usage/team', component: { template: '<div>Team Usage</div>' } },
      { path: '/usage/quota-reset', component: { template: '<div>Quota Reset</div>' } },
      { path: '/work-items', component: { template: '<div>Work Items</div>' } },
      { path: '/repos', component: { template: '<div>Repos</div>' } },
      { path: '/events', component: { template: '<div>Events</div>' } },
      { path: '/user', component: { template: '<div>User</div>' } },
    ],
  })
}

describe('DashboardView chart loading', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    setLocale('en-US')
    vi.clearAllMocks()
  })

  afterEach(() => {
    canvasModules.releaseAll()
  })

  it('loads chart canvases only after chartable dashboard data exists', async () => {
    canvasModules.defer()
    const firstRequest = deferred<any>()
    const refreshRequest = deferred<any>()
    ;(getUserUsageDashboard as any)
      .mockReturnValueOnce(firstRequest.promise)
      .mockReturnValueOnce(refreshRequest.promise)
      .mockResolvedValue({ data: { data: usageSnapshot } })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()
    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })

    expect(getUserUsageDashboard).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('Loading usage dashboard')
    expect(canvasModules.lineLoads).toBe(0)
    expect(canvasModules.doughnutLoads).toBe(0)

    firstRequest.resolve({
      data: {
        data: { ...usageSnapshot, trend: [], models: [] },
      },
    })

    await vi.waitFor(() => {
      expect(wrapper.text()).toContain('No trend data available')
      expect(wrapper.text()).toContain('No model data available')
    })
    expect(canvasModules.lineLoads).toBe(0)
    expect(canvasModules.doughnutLoads).toBe(0)

    await wrapper.get('[data-test="range-7d"]').trigger('click')
    expect(getUserUsageDashboard).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('Loading trend...')
    expect(wrapper.text()).toContain('Loading models...')
    expect(canvasModules.lineLoads).toBe(0)
    expect(canvasModules.doughnutLoads).toBe(0)

    refreshRequest.resolve({ data: { data: usageSnapshot } })
    await flushPromises()

    await vi.waitFor(() => {
      expect(canvasModules.lineLoads).toBe(1)
      expect(canvasModules.doughnutLoads).toBe(1)
    })
    expect(wrapper.text()).toContain('example-model')
    expect(wrapper.find('[data-test="line-chart"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="doughnut-chart"]').exists()).toBe(false)
    const trendSection = wrapper.findAll('section').find((section) => section.text().includes('Token Trend'))
    const modelSection = wrapper.findAll('section').find((section) => section.text().includes('Model Distribution'))
    expect(trendSection?.get('.h-72').classes()).toContain('h-72')
    expect(modelSection?.get('.h-44').classes()).toContain('h-44')

    canvasModules.resolveLine()
    canvasModules.resolveDoughnut()
    await vi.waitFor(() => {
      expect(wrapper.find('[data-test="line-chart"]').exists()).toBe(true)
      expect(wrapper.find('[data-test="doughnut-chart"]').exists()).toBe(true)
    })

    const line = wrapper.get('[data-test="line-chart"]')
    const doughnut = wrapper.get('[data-test="doughnut-chart"]')
    expect(JSON.parse(line.attributes('data-chart') ?? '{}').labels).toEqual(['2026-06-06'])
    expect(JSON.parse(doughnut.attributes('data-chart') ?? '{}').labels).toEqual(['example-model'])
    expect(JSON.parse(line.attributes('data-options') ?? '{}').maintainAspectRatio).toBe(false)
    expect(JSON.parse(doughnut.attributes('data-options') ?? '{}').maintainAspectRatio).toBe(false)

    await wrapper.get('[data-test="range-today"]').trigger('click')
    await flushPromises()
    expect(canvasModules.lineLoads).toBe(1)
    expect(canvasModules.doughnutLoads).toBe(1)
  })
})
