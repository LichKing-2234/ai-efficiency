import { describe, it, expect, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import DashboardView from '@/views/DashboardView.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/efficiency', () => ({
  getDashboard: vi.fn(),
}))

vi.mock('@/api/user', () => ({
  getUserProviders: vi.fn(),
}))

vi.mock('@/api/events', () => ({
  listEvents: vi.fn(),
}))

vi.mock('@/api/userUsage', () => ({
  getUserUsageDashboard: vi.fn(() => Promise.resolve({
    data: {
      data: {
        configured: false,
        range: { start_date: '2026-06-01', end_date: '2026-06-06', granularity: 'day', timezone: 'Asia/Shanghai' },
        stats: null,
        trend: [],
        models: [],
      },
    },
  })),
}))

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  getMe: vi.fn(),
  devLogin: vi.fn(),
}))

vi.mock('vue-chartjs', () => ({
  Line: { template: '<div data-test="line-chart" />' },
  Doughnut: { template: '<div data-test="doughnut-chart" />' },
}))

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
}

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: DashboardView },
      { path: '/login', component: { template: '<div>Login</div>' } },
      { path: '/repos', component: { template: '<div>Repos</div>' } },
      { path: '/events', component: { template: '<div>Events</div>' } },
      { path: '/user', component: { template: '<div>User</div>' } },
      { path: '/settings', component: { template: '<div>Settings</div>' } },
    ],
  })
}

describe('DashboardView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    setLocale('en-US')
    vi.clearAllMocks()
  })

  it('renders personal AI usage title', async () => {
    const { getDashboard } = await import('@/api/efficiency')
    const { getUserProviders } = await import('@/api/user')
    const { listEvents } = await import('@/api/events')
    ;(getDashboard as any).mockResolvedValue({
      data: { data: { total_repos: 5, tracked_workflows: 2, total_ai_prs: 10 } },
    })
    ;(getUserProviders as any).mockResolvedValue({ data: { data: { providers: [] } } })
    ;(listEvents as any).mockResolvedValue({ data: { data: { items: [], total: 0, page: 0, page_size: 3 } } })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.find('h1').text()).toContain('My AI Usage')
    expect(wrapper.text()).toContain('Next Steps')
    expect(wrapper.text()).toContain('Platform Signals')
    expect(wrapper.text()).not.toContain('This Week')
  })

  it('displays loading state initially', async () => {
    const { getDashboard } = await import('@/api/efficiency')
    const { getUserProviders } = await import('@/api/user')
    const { listEvents } = await import('@/api/events')
    // Never resolve to keep loading state
    ;(getDashboard as any).mockReturnValue(new Promise(() => {}))
    ;(getUserProviders as any).mockReturnValue(new Promise(() => {}))
    ;(listEvents as any).mockReturnValue(new Promise(() => {}))

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })

    expect(wrapper.text()).toContain('Loading your AI usage')
  })

  it('displays dashboard data after loading', async () => {
    const { getDashboard } = await import('@/api/efficiency')
    const { getUserProviders } = await import('@/api/user')
    const { listEvents } = await import('@/api/events')
    ;(getDashboard as any).mockResolvedValue({
      data: { data: { total_repos: 12, tracked_workflows: 3, total_ai_prs: 42 } },
    })
    ;(getUserProviders as any).mockResolvedValue({ data: { data: { providers: [] } } })
    ;(listEvents as any).mockResolvedValue({ data: { data: { items: [], total: 0, page: 0, page_size: 3 } } })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })

    await new Promise((r) => setTimeout(r, 10))
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('12')
    expect(wrapper.text()).toContain('3')
    expect(wrapper.text()).toContain('42')
    expect(wrapper.text()).toContain('Tracked Workflows')
    expect(wrapper.text()).toContain('Platform Signals')
    expect(wrapper.text()).toContain('Setup Status')
    expect(wrapper.text()).toContain('AI access')
    expect(wrapper.text()).toContain('Code reporting')
    expect(wrapper.text()).toContain('Recent usage')
    expect(wrapper.text()).not.toContain('Active Sessions')
    expect(wrapper.text()).not.toContain('Avg AI Score')
  })

  it('derives connected tools from user provider credentials instead of workflow count', async () => {
    const { getDashboard } = await import('@/api/efficiency')
    const { getUserProviders } = await import('@/api/user')
    const { listEvents } = await import('@/api/events')
    ;(getDashboard as any).mockResolvedValue({
      data: { data: { total_repos: 8, tracked_workflows: 4, total_ai_prs: 2 } },
    })
    ;(getUserProviders as any).mockResolvedValue({
      data: {
        data: {
          providers: [
            {
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
            },
          ],
        },
      },
    })
    ;(listEvents as any).mockResolvedValue({ data: { data: { items: [], total: 0, page: 0, page_size: 3 } } })

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

  it('shows placeholder values when API fails', async () => {
    const { getDashboard } = await import('@/api/efficiency')
    const { getUserProviders } = await import('@/api/user')
    const { listEvents } = await import('@/api/events')
    ;(getDashboard as any).mockRejectedValue(new Error('Network error'))
    ;(getUserProviders as any).mockRejectedValue(new Error('Network error'))
    ;(listEvents as any).mockRejectedValue(new Error('Network error'))

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })

    await new Promise((r) => setTimeout(r, 10))
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('No platform data yet')
    expect(wrapper.text()).toContain('Open My Setup')
  })

  it('replaces recent activity with the relay usage dashboard', async () => {
    const { getDashboard } = await import('@/api/efficiency')
    const { getUserProviders } = await import('@/api/user')
    const { listEvents } = await import('@/api/events')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getDashboard as any).mockResolvedValue({
      data: { data: { total_repos: 1, tracked_workflows: 1, total_ai_prs: 2 } },
    })
    ;(getUserProviders as any).mockResolvedValue({ data: { data: { providers: [] } } })
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })
    ;(listEvents as any).mockResolvedValue({
      data: {
        data: {
          items: [
            {
              id: 12,
              tool: 'codex',
              repo_id: 1,
              repo_name: 'org/repo',
              tool_session_id: 'session',
              dedupe_key: 'event',
              observed_end_at: '2026-05-30T08:00:00Z',
              request_count: 2,
              input_tokens: 100,
              output_tokens: 50,
              cached_input_tokens: 25,
              reasoning_tokens: 0,
              credit_usage: 1,
              source_basename: 'events.jsonl',
              binding_status: 'bound',
            },
          ],
          total: 1,
          page: 0,
          page_size: 3,
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

    expect(listEvents).toHaveBeenCalledWith({ limit: 3, offset: 0 })
    expect(wrapper.text()).toContain('Token Trend')
    expect(wrapper.text()).toContain('Model Distribution')
    expect(wrapper.text()).not.toContain('Recent Activity')
    expect(wrapper.text()).not.toContain('codex')
    expect(wrapper.text()).not.toContain('org/repo')
  })

  it('renders the relay usage dashboard inside the home page', async () => {
    const { getDashboard } = await import('@/api/efficiency')
    const { getUserProviders } = await import('@/api/user')
    const { listEvents } = await import('@/api/events')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getDashboard as any).mockResolvedValue({
      data: { data: { total_repos: 1, tracked_workflows: 1, total_ai_prs: 2 } },
    })
    ;(getUserProviders as any).mockResolvedValue({ data: { data: { providers: [] } } })
    ;(listEvents as any).mockResolvedValue({ data: { data: { items: [], total: 0, page: 0, page_size: 3 } } })
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: usageSnapshot } })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })

    await flushPromises()

    expect(getUserUsageDashboard).toHaveBeenCalled()
    expect(wrapper.text()).toContain('Token Trend')
    expect(wrapper.text()).toContain('Model Distribution')
    expect(wrapper.text()).toContain('example-model')
    expect(wrapper.find('[data-test="line-chart"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="doughnut-chart"]').exists()).toBe(true)
  })
})
