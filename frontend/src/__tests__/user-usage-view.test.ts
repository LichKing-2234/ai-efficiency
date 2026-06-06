import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import UsageView from '@/views/user/UsageView.vue'

vi.mock('@/api/userUsage', () => ({
  getUserUsageDashboard: vi.fn(),
}))

vi.mock('vue-chartjs', () => ({
  Line: { template: '<div data-test="line-chart" />' },
  Doughnut: { template: '<div data-test="doughnut-chart" />' },
}))

function createRouterForUsage() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/user/usage', component: UsageView },
      { path: '/user', component: { template: '<div>Setup</div>' } },
    ],
  })
}

const snapshot = {
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
}

describe('UsageView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows setup empty state when dashboard is not configured', async () => {
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserUsageDashboard as any).mockResolvedValue({
      data: { data: { configured: false, range: { start_date: '2026-06-01', end_date: '2026-06-06', granularity: 'day' }, stats: null, trend: [], models: [] } },
    })
    const router = createRouterForUsage()
    await router.push('/user/usage')
    await router.isReady()
    const wrapper = mount(UsageView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('Complete AI service configuration')
    expect(wrapper.text()).toContain('Open My Setup')
  })

  it('renders snapshot cards and charts', async () => {
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: snapshot } })
    const router = createRouterForUsage()
    await router.push('/user/usage')
    await router.isReady()
    const wrapper = mount(UsageView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('My AI Usage')
    expect(wrapper.text()).toContain('Today Cost')
    expect(wrapper.text()).toContain('Today Requests')
    expect(wrapper.text()).toContain('Today Tokens')
    expect(wrapper.text()).toContain('Avg Response')
    expect(wrapper.text()).toContain('example-model')
    expect(wrapper.find('[data-test="line-chart"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="doughnut-chart"]').exists()).toBe(true)
  })

  it('uses hour granularity for Today and day granularity for 7 Days', async () => {
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: snapshot } })
    const router = createRouterForUsage()
    await router.push('/user/usage')
    await router.isReady()
    const wrapper = mount(UsageView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()
    await wrapper.get('[data-test="range-today"]').trigger('click')
    await flushPromises()
    expect((getUserUsageDashboard as any).mock.calls.at(-1)[0].granularity).toBe('hour')
    await wrapper.get('[data-test="range-7d"]').trigger('click')
    await flushPromises()
    expect((getUserUsageDashboard as any).mock.calls.at(-1)[0].granularity).toBe('day')
  })

  it('uses local calendar dates for the initial range', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 5, 6, 0, 30, 0))
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: snapshot } })
    const router = createRouterForUsage()
    await router.push('/user/usage')
    await router.isReady()
    mount(UsageView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()
    expect((getUserUsageDashboard as any).mock.calls[0][0]).toMatchObject({
      start_date: '2026-05-31',
      end_date: '2026-06-06',
      granularity: 'day',
    })
  })

  it('shows credential repair message on 409', async () => {
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserUsageDashboard as any).mockRejectedValue({ response: { status: 409 } })
    const router = createRouterForUsage()
    await router.push('/user/usage')
    await router.isReady()
    const wrapper = mount(UsageView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('Relay credentials need attention')
    expect(wrapper.text()).toContain('Open My Setup')
  })
})
