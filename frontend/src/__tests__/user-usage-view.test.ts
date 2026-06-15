import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import UsageView from '@/views/user/UsageView.vue'
import { setLocale } from '@/i18n'

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
  trend: [
    { date: '2026-06-05', requests: 20, input_tokens: 2000, output_tokens: 1000, cache_creation_tokens: 40, cache_read_tokens: 60, total_tokens: 3100, cost: 0.5, actual_cost: 0.4 },
    { date: '2026-06-06', requests: 12, input_tokens: 1000, output_tokens: 500, cache_creation_tokens: 20, cache_read_tokens: 30, total_tokens: 1550, cost: 0.25, actual_cost: 0.2 },
  ],
  models: [{ model: 'example-model', requests: 12, input_tokens: 1000, output_tokens: 500, cache_creation_tokens: 20, cache_read_tokens: 30, total_tokens: 1550, cost: 0.25, actual_cost: 0.2 }],
}

function snapshotWithTrend(range: Record<string, string>, trend: typeof snapshot.trend) {
  const totals = trend.reduce(
    (acc, point) => ({
      requests: acc.requests + point.requests,
      input_tokens: acc.input_tokens + point.input_tokens,
      output_tokens: acc.output_tokens + point.output_tokens,
      cache_creation_tokens: acc.cache_creation_tokens + point.cache_creation_tokens,
      cache_read_tokens: acc.cache_read_tokens + point.cache_read_tokens,
      total_tokens: acc.total_tokens + point.total_tokens,
      cost: acc.cost + point.cost,
      actual_cost: acc.actual_cost + point.actual_cost,
    }),
    {
      requests: 0,
      input_tokens: 0,
      output_tokens: 0,
      cache_creation_tokens: 0,
      cache_read_tokens: 0,
      total_tokens: 0,
      cost: 0,
      actual_cost: 0,
    },
  )
  return {
    ...snapshot,
    range: { ...snapshot.range, ...range },
    trend,
    models: [{ ...snapshot.models[0], ...totals }],
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

const todaySnapshot = snapshotWithTrend(
  { start_date: '2026-06-06', end_date: '2026-06-06', granularity: 'hour' },
  [{ date: '09:00', requests: 4, input_tokens: 400, output_tokens: 180, cache_creation_tokens: 10, cache_read_tokens: 20, total_tokens: 610, cost: 0.14, actual_cost: 0.11 }],
)

const thirtyDaySnapshot = snapshotWithTrend(
  { start_date: '2026-05-08', end_date: '2026-06-06', granularity: 'day' },
  [
    { date: '2026-05-10', requests: 40, input_tokens: 4000, output_tokens: 1800, cache_creation_tokens: 60, cache_read_tokens: 140, total_tokens: 6000, cost: 1.3, actual_cost: 1.05 },
    { date: '2026-06-06', requests: 50, input_tokens: 5000, output_tokens: 2500, cache_creation_tokens: 100, cache_read_tokens: 400, total_tokens: 8000, cost: 1.7, actual_cost: 1.4 },
  ],
)

describe('UsageView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    setLocale('en-US')
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
    expect(wrapper.text()).toContain('Open AI Setup')
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
    expect(wrapper.text()).toContain('7 Days Cost')
    expect(wrapper.text()).toContain('7 Days Requests')
    expect(wrapper.text()).toContain('7 Days Tokens')
    expect(wrapper.text()).toContain('$0.6000')
    expect(wrapper.text()).toMatch(/7 Days Requests\s*32\s*Selected range/)
    expect(wrapper.text()).not.toContain('Today Cost')
    expect(wrapper.text()).toContain('Avg Response')
    expect(wrapper.text()).toContain('example-model')
    expect(wrapper.find('[data-test="line-chart"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="doughnut-chart"]').exists()).toBe(true)
    const modelTableScroll = wrapper.get('[data-testid="usage-model-table-scroll"]')
    expect(modelTableScroll.classes()).toContain('overflow-x-auto')
    expect(modelTableScroll.get('table').classes()).toContain('min-w-[36rem]')
  })

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

  it('updates card labels and totals for selected ranges', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 5, 6, 10, 0, 0))
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserUsageDashboard as any).mockImplementation((params: any) => {
      if (params.granularity === 'hour') return Promise.resolve({ data: { data: todaySnapshot } })
      if (params.start_date === '2026-05-08') return Promise.resolve({ data: { data: thirtyDaySnapshot } })
      return Promise.resolve({ data: { data: snapshot } })
    })
    const router = createRouterForUsage()
    await router.push('/user/usage')
    await router.isReady()
    const wrapper = mount(UsageView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('7 Days Cost')
    expect(wrapper.text()).toContain('$0.6000')

    await wrapper.get('[data-test="range-30d"]').trigger('click')
    await flushPromises()
    expect((getUserUsageDashboard as any).mock.calls.at(-1)[0]).toMatchObject({
      start_date: '2026-05-08',
      end_date: '2026-06-06',
      granularity: 'day',
    })
    expect(wrapper.text()).toContain('30 Days Cost')
    expect(wrapper.text()).toContain('$2.4500')
    expect(wrapper.text()).toMatch(/30 Days Requests\s*90\s*Selected range/)
    expect(wrapper.text()).not.toContain('7 Days Cost')

    await wrapper.get('[data-test="range-today"]').trigger('click')
    await flushPromises()
    expect((getUserUsageDashboard as any).mock.calls.at(-1)[0].granularity).toBe('hour')
    expect(wrapper.text()).toContain('Today Cost')
    expect(wrapper.text()).toContain('$0.1100')
    expect(wrapper.text()).toMatch(/Today Requests\s*4\s*Selected range/)
    expect(wrapper.text()).not.toContain('30 Days Cost')
  })

  it('ignores stale responses when range requests resolve out of order', async () => {
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    const first = deferred<{ data: { data: typeof snapshot } }>()
    const second = deferred<{ data: { data: typeof thirtyDaySnapshot } }>()
    ;(getUserUsageDashboard as any)
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise)

    const router = createRouterForUsage()
    await router.push('/user/usage')
    await router.isReady()
    const wrapper = mount(UsageView, { global: { plugins: [createPinia(), router] } })
    await wrapper.vm.$nextTick()
    expect(getUserUsageDashboard).toHaveBeenCalledTimes(1)

    await wrapper.get('[data-test="range-30d"]').trigger('click')
    expect(getUserUsageDashboard).toHaveBeenCalledTimes(2)

    second.resolve({ data: { data: thirtyDaySnapshot } })
    await flushPromises()
    expect(wrapper.text()).toContain('30 Days Cost')
    expect(wrapper.text()).toContain('$2.4500')

    first.resolve({ data: { data: snapshot } })
    await flushPromises()
    expect(wrapper.text()).toContain('30 Days Cost')
    expect(wrapper.text()).toContain('$2.4500')
    expect(wrapper.text()).not.toContain('7 Days Cost')
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
    expect(wrapper.text()).toContain('Open AI Setup')
  })

  it('renders Chinese usage labels when locale is Chinese', async () => {
    setLocale('zh-CN')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: snapshot } })
    const router = createRouterForUsage()
    await router.push('/user/usage')
    await router.isReady()
    const wrapper = mount(UsageView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('我的 AI 用量')
    expect(wrapper.text()).toContain('7 天费用')
    expect(wrapper.text()).not.toContain('今日费用')
    expect(wrapper.text()).toContain('Token 趋势')
    expect(wrapper.text()).toContain('模型分布')
    expect(wrapper.text()).toContain('刷新')
    expect(wrapper.text()).toContain('7 天')
    expect(wrapper.text()).not.toContain('Today Cost')
    expect(wrapper.text()).not.toContain('Model Distribution')
  })
})
