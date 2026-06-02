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

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  getMe: vi.fn(),
  devLogin: vi.fn(),
}))

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

  it('renders recent activity from usage records when available', async () => {
    const { getDashboard } = await import('@/api/efficiency')
    const { getUserProviders } = await import('@/api/user')
    const { listEvents } = await import('@/api/events')
    ;(getDashboard as any).mockResolvedValue({
      data: { data: { total_repos: 1, tracked_workflows: 1, total_ai_prs: 2 } },
    })
    ;(getUserProviders as any).mockResolvedValue({ data: { data: { providers: [] } } })
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
    expect(wrapper.text()).toContain('codex')
    expect(wrapper.text()).toContain('org/repo')
    expect(wrapper.text()).toContain('175')
  })
})
