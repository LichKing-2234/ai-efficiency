import { describe, it, expect, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import DashboardView from '@/views/DashboardView.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/user', () => ({
  getUserProviders: vi.fn(),
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
    const { getUserProviders } = await import('@/api/user')
    ;(getUserProviders as any).mockResolvedValue({ data: { data: { providers: [] } } })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.find('h1').text()).toContain('Complete AI setup first')
    expect(wrapper.text()).toContain('AI Usage Center')
    expect(wrapper.text()).not.toContain('Platform Signals')
  })

  it('displays loading state initially', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    // Never resolve to keep loading state
    ;(getUserProviders as any).mockReturnValue(new Promise(() => {}))
    ;(getUserUsageDashboard as any).mockReturnValue(new Promise(() => {}))

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })

    expect(wrapper.text()).toContain('Loading your AI usage')
  })

  it('displays dashboard data after loading', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserProviders as any).mockResolvedValue({ data: { data: { providers: [] } } })
    ;(getUserUsageDashboard as any).mockResolvedValue({
      data: {
        data: {
          configured: false,
          range: { start_date: '2026-06-01', end_date: '2026-06-06', granularity: 'day', timezone: 'Asia/Shanghai' },
          stats: null,
          trend: [],
          models: [],
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

    expect(wrapper.text()).toContain('Complete AI setup first')
    expect(wrapper.text()).toContain('Go to AI Setup & Configuration')
    expect(wrapper.text()).not.toContain('Check recent records')
    expect(wrapper.text()).not.toContain('Connect a repository')
    expect(wrapper.text()).not.toContain('I am an engineer')
    expect(wrapper.text()).not.toContain('Platform Signals')
  })

  it('uses provider credentials to mark AI setup done inside the guide card', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
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
    ;(getUserUsageDashboard as any).mockResolvedValue({
      data: {
        data: {
          configured: true,
          range: { start_date: '2026-06-01', end_date: '2026-06-06', granularity: 'day', timezone: 'Asia/Shanghai' },
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

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })

    await flushPromises()

    expect(getUserProviders).toHaveBeenCalled()
    expect(wrapper.text()).toContain('AI setup is ready, start your first usage')
    expect(wrapper.text()).toContain('AI setup')
    expect(wrapper.text()).toContain('Done')
    expect(wrapper.text()).not.toContain('Code association')
  })

  it('shows placeholder values when API fails', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserProviders as any).mockRejectedValue(new Error('Network error'))
    ;(getUserUsageDashboard as any).mockRejectedValue(new Error('Network error'))

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('Unable to load your current homepage status')
    expect(wrapper.text()).toContain('Go to AI Setup & Configuration')
  })

  it('renders the embedded usage dashboard without cost cards', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
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

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('Token Trend')
    expect(wrapper.text()).toContain('Model Distribution')
    expect(wrapper.text()).not.toContain('7 Days Cost')
    expect(wrapper.text()).not.toContain('$0.2000')
  })

  it('renders the relay usage dashboard inside the home page', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
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

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })

    await flushPromises()

    expect(getUserUsageDashboard).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('Token Trend')
    expect(wrapper.text()).toContain('Model Distribution')
    expect(wrapper.text()).toContain('example-model')
    expect(wrapper.find('[data-test="line-chart"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="doughnut-chart"]').exists()).toBe(true)
  })

  it('shows the expanded guide card for users without any reusable AI access', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
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
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
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
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
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
    expect(wrapper.text()).not.toContain('Check recent records')
    expect(wrapper.text()).not.toContain('Connect a repository')
    expect(wrapper.text()).not.toContain('I am an engineer')
    expect(wrapper.text()).not.toContain('7 Days Cost')
    expect(wrapper.text()).not.toContain('$0.6000')
  })

  it('does not render the developer helper toggle or cards on the home page', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
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

    expect(wrapper.text()).not.toContain('Check recent records')
    expect(wrapper.text()).not.toContain('Connect a repository')
    expect(wrapper.find('[data-testid="home-developer-toggle"]').exists()).toBe(false)
  })

  it('shows a degraded-state warning instead of misclassifying provider or usage failures as setup-needed', async () => {
    const { getUserProviders } = await import('@/api/user')
    const { getUserUsageDashboard } = await import('@/api/userUsage')
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
})
