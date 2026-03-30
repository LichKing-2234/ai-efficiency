import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import AppSidebar from '@/components/AppSidebar.vue'

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  getMe: vi.fn(),
  devLogin: vi.fn(),
}))

function createTestRouter(initialPath = '/') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div>Dashboard</div>' } },
      { path: '/repos', component: { template: '<div>Repos</div>' } },
      { path: '/sessions', component: { template: '<div>Sessions</div>' } },
      { path: '/settings', component: { template: '<div>Settings</div>' } },
      { path: '/login', component: { template: '<div>Login</div>' } },
    ],
  })
  return router
}

describe('AppSidebar', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('renders app title', async () => {
    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(AppSidebar, {
      global: { plugins: [createPinia(), router] },
    })

    expect(wrapper.text()).toContain('AI Efficiency')
  })

  it('renders navigation links for Dashboard, Repos, and Sessions', async () => {
    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(AppSidebar, {
      global: { plugins: [createPinia(), router] },
    })

    const links = wrapper.findAll('a')
    const linkTexts = links.map((l) => l.text())

    expect(linkTexts).toContain('Dashboard')
    expect(linkTexts).toContain('Repos')
    expect(linkTexts).toContain('Sessions')
    expect(linkTexts).not.toContain('Settings')
  })

  it('renders Settings link for admin users', async () => {
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

    const links = wrapper.findAll('a')
    const linkTexts = links.map((l) => l.text())
    expect(linkTexts).toContain('Settings')
  })

  it('renders disabled Analysis and Gating items', async () => {
    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(AppSidebar, {
      global: { plugins: [createPinia(), router] },
    })

    expect(wrapper.text()).toContain('Analysis')
    expect(wrapper.text()).toContain('Gating')

    // These should be spans (not links), with cursor-not-allowed
    const disabledItems = wrapper.findAll('span.cursor-not-allowed')
    expect(disabledItems.length).toBe(2)
  })

  it('applies active class to current route link', async () => {
    const router = createTestRouter()
    await router.push('/repos')
    await router.isReady()

    const wrapper = mount(AppSidebar, {
      global: { plugins: [createPinia(), router] },
    })

    const reposLink = wrapper.findAll('a').find((a) => a.text() === 'Repos')
    expect(reposLink).toBeTruthy()
    expect(reposLink!.classes()).toContain('bg-gray-800')
  })

  it('does not apply active class to non-current route links', async () => {
    const router = createTestRouter()
    await router.push('/repos')
    await router.isReady()

    const wrapper = mount(AppSidebar, {
      global: { plugins: [createPinia(), router] },
    })

    const dashboardLink = wrapper.findAll('a').find((a) => a.text() === 'Dashboard')
    expect(dashboardLink).toBeTruthy()
    expect(dashboardLink!.classes()).not.toContain('bg-gray-800')
  })

  it('renders logout button', async () => {
    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(AppSidebar, {
      global: { plugins: [createPinia(), router] },
    })

    const logoutBtn = wrapper.find('button[title="Logout"]')
    expect(logoutBtn.exists()).toBe(true)
  })

  it('displays username from auth store', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const { useAuthStore } = await import('@/stores/auth')
    const auth = useAuthStore(pinia)
    auth.user = { id: 1, username: 'testuser', email: 'test@example.com', role: 'admin', auth_source: 'sso' }

    const wrapper = mount(AppSidebar, {
      global: { plugins: [pinia, router] },
    })

    expect(wrapper.text()).toContain('testuser')
    expect(wrapper.text()).toContain('admin')
  })

  // --- New tests for uncovered lines (handleLogout) ---

  it('logout clears auth and redirects to /login', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const { useAuthStore } = await import('@/stores/auth')
    const auth = useAuthStore(pinia)
    auth.user = { id: 1, username: 'testuser', email: 'test@example.com', role: 'admin', auth_source: 'sso' }
    auth.token = 'some-token'

    const wrapper = mount(AppSidebar, {
      global: { plugins: [pinia, router] },
    })

    const logoutBtn = wrapper.find('button[title="Logout"]')
    await logoutBtn.trigger('click')
    await flushPromises()

    expect(auth.token).toBeNull()
    expect(auth.user).toBeNull()
    expect(router.currentRoute.value.path).toBe('/login')
  })

  it('displays default User when no user is set', async () => {
    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(AppSidebar, {
      global: { plugins: [createPinia(), router] },
    })

    expect(wrapper.text()).toContain('User')
  })

  it('applies active class to Settings when on settings route', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)

    const router = createTestRouter()
    await router.push('/settings')
    await router.isReady()

    const { useAuthStore } = await import('@/stores/auth')
    const auth = useAuthStore(pinia)
    auth.user = { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin', auth_source: 'sso' }

    const wrapper = mount(AppSidebar, {
      global: { plugins: [pinia, router] },
    })

    const settingsLink = wrapper.findAll('a').find((a) => a.text() === 'Settings')
    expect(settingsLink).toBeTruthy()
    expect(settingsLink!.classes()).toContain('bg-gray-800')
  })
})
