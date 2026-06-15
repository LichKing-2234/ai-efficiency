import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import AppSidebar from '@/components/AppSidebar.vue'
import { setLocale } from '@/i18n'

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
      { path: '/events', component: { template: '<div>Events</div>' } },
      { path: '/user', component: { template: '<div>User</div>' } },
      { path: '/sessions', component: { template: '<div>Sessions</div>' } },
      { path: '/admin/users', component: { template: '<div>Admin Users</div>' } },
      { path: '/settings', component: { template: '<div>Settings</div>' } },
      { path: '/login', component: { template: '<div>Login</div>' } },
    ],
  })
  return router
}

describe('AppSidebar', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    setLocale('en-US')
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

  it('places the language toggle in the sidebar header, away from the account footer', async () => {
    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(AppSidebar, {
      global: { plugins: [createPinia(), router] },
    })

    const header = wrapper.get('[data-testid="sidebar-header"]')
    const footer = wrapper.get('[data-testid="sidebar-footer"]')

    expect(header.find('[data-testid="language-toggle"]').exists()).toBe(true)
    expect(footer.find('[data-testid="language-toggle"]').exists()).toBe(false)
    expect(footer.find('[data-testid="sidebar-account-summary"]').exists()).toBe(true)
  })

  it('renders friendly navigation links for regular users', async () => {
    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(AppSidebar, {
      global: { plugins: [createPinia(), router] },
    })

    const links = wrapper.findAll('a')
    const linkTexts = links.map((l) => l.text())

    expect(linkTexts).toContain('AI Usage Center')
    expect(linkTexts).toContain('Usage Records')
    expect(linkTexts).toContain('Code Repositories')
    expect(linkTexts).toContain('AI Setup & Configuration')
    expect(linkTexts).not.toContain('My Usage')
    expect(links.map((l) => l.attributes('href'))).not.toContain('/user/usage')
    expect(wrapper.text()).toContain('My Work')
    expect(wrapper.text()).toContain('Code & PR')
    expect(wrapper.text()).not.toContain('Administration')
    expect(linkTexts).not.toContain('Sessions')
    expect(linkTexts).not.toContain('Admin Console')
  })

  it('renders Admin Console link for admin users', async () => {
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
    expect(linkTexts).toContain('Admin Console')
  })

  it('renders Users & Access link for admin users', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const { useAuthStore } = await import('@/stores/auth')
    const auth = useAuthStore(pinia)
    auth.user = { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin', auth_source: 'relay_sso' }

    const wrapper = mount(AppSidebar, {
      global: { plugins: [pinia, router] },
    })

    const linkTexts = wrapper.findAll('a').map((l) => l.text())
    expect(wrapper.text()).toContain('Administration')
    expect(linkTexts).toContain('Users & Access')
  })

  it('hides Users & Access link for regular users', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const { useAuthStore } = await import('@/stores/auth')
    const auth = useAuthStore(pinia)
    auth.user = { id: 2, username: 'alice', email: 'alice@example.com', role: 'user', auth_source: 'ldap' }

    const wrapper = mount(AppSidebar, {
      global: { plugins: [pinia, router] },
    })

    const linkTexts = wrapper.findAll('a').map((l) => l.text())
    expect(linkTexts).not.toContain('Users & Access')
  })

  it('applies active class to current route link', async () => {
    const router = createTestRouter()
    await router.push('/repos')
    await router.isReady()

    const wrapper = mount(AppSidebar, {
      global: { plugins: [createPinia(), router] },
    })

    const reposLink = wrapper.findAll('a').find((a) => a.text() === 'Code Repositories')
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

    const dashboardLink = wrapper.findAll('a').find((a) => a.text() === 'AI Usage Center')
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

  it('truncates long account identity text in the footer', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const { useAuthStore } = await import('@/stores/auth')
    const auth = useAuthStore(pinia)
    auth.user = {
      id: 1,
      username: 'very-long-admin-account@example.com',
      email: 'alice@example.com',
      role: 'admin',
      auth_source: 'sso',
    }

    const wrapper = mount(AppSidebar, {
      global: { plugins: [pinia, router] },
    })

    const accountLines = wrapper.get('[data-testid="sidebar-account-summary"]').findAll('p')
    expect(accountLines[0].classes()).toContain('truncate')
    expect(accountLines[0].attributes('title')).toBe('very-long-admin-account@example.com')
    expect(accountLines[1].classes()).toContain('truncate')
    expect(accountLines[1].attributes('title')).toBe('admin')
  })

  it('keeps the footer account identity separate from setup navigation', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const { useAuthStore } = await import('@/stores/auth')
    const auth = useAuthStore(pinia)
    auth.user = { id: 1, username: 'testuser', email: 'test@example.com', role: 'user', auth_source: 'sso' }

    const wrapper = mount(AppSidebar, {
      global: { plugins: [pinia, router] },
    })

    const setupLinks = wrapper.findAll('a').filter((link) => link.text() === 'AI Setup & Configuration')
    expect(setupLinks).toHaveLength(1)
    expect(setupLinks[0].attributes('href')).toBe('/user')

    expect(wrapper.find('[data-testid="sidebar-account-link"]').exists()).toBe(false)
    const accountSummary = wrapper.get('[data-testid="sidebar-account-summary"]')
    expect(accountSummary.element.tagName).toBe('DIV')
    expect(accountSummary.attributes('href')).toBeUndefined()
    expect(accountSummary.text()).toContain('testuser')
    expect(accountSummary.text()).toContain('user')

    await accountSummary.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/')
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

  it('applies active class to Admin Console when on settings route', async () => {
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

    const settingsLink = wrapper.findAll('a').find((a) => a.text() === 'Admin Console')
    expect(settingsLink).toBeTruthy()
    expect(settingsLink!.classes()).toContain('bg-gray-800')
  })

  it('switches navigation labels to Chinese for review', async () => {
    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(AppSidebar, {
      global: { plugins: [createPinia(), router] },
    })

    await wrapper.get('[data-testid="language-toggle"]').trigger('click')

    const linkTexts = wrapper.findAll('a').map((l) => l.text())
    expect(wrapper.text()).toContain('我的工作')
    expect(wrapper.text()).toContain('代码与 PR')
    expect(linkTexts).toContain('AI 使用中心')
    expect(linkTexts).toContain('AI 接入与配置')
    expect(linkTexts).not.toContain('我的用量')
    expect(linkTexts).toContain('使用记录')
    expect(linkTexts).toContain('代码仓库')
  })
})
