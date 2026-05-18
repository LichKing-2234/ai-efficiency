import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import DashboardView from '@/views/DashboardView.vue'

vi.mock('@/api/efficiency', () => ({
  getDashboard: vi.fn(),
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
      { path: '/settings', component: { template: '<div>Settings</div>' } },
    ],
  })
}

describe('DashboardView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('renders dashboard title', async () => {
    const { getDashboard } = await import('@/api/efficiency')
    ;(getDashboard as any).mockResolvedValue({
      data: { data: { total_repos: 5, tracked_workflows: 2, total_ai_prs: 10 } },
    })

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })

    expect(wrapper.find('h1').text()).toContain('Welcome back')
  })

  it('displays loading state initially', async () => {
    const { getDashboard } = await import('@/api/efficiency')
    // Never resolve to keep loading state
    ;(getDashboard as any).mockReturnValue(new Promise(() => {}))

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })

    expect(wrapper.text()).toContain('Loading...')
  })

  it('displays dashboard data after loading', async () => {
    const { getDashboard } = await import('@/api/efficiency')
    ;(getDashboard as any).mockResolvedValue({
      data: { data: { total_repos: 12, tracked_workflows: 3, total_ai_prs: 42 } },
    })

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
    expect(wrapper.text()).not.toContain('Active Sessions')
    expect(wrapper.text()).not.toContain('Avg AI Score')
  })

  it('shows placeholder values when API fails', async () => {
    const { getDashboard } = await import('@/api/efficiency')
    ;(getDashboard as any).mockRejectedValue(new Error('Network error'))

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(DashboardView, {
      global: { plugins: [createPinia(), router] },
    })

    await new Promise((r) => setTimeout(r, 10))
    await wrapper.vm.$nextTick()

    // When API fails, dashboard is null, so '--' placeholders are shown
    expect(wrapper.text()).toContain('--')
  })
})
