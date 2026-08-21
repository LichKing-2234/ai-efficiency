import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import AppSidebar from '@/components/AppSidebar.vue'
import { setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  getMe: vi.fn(),
  devLogin: vi.fn(),
}))

vi.mock('@/api/workItems', () => ({
  getWorkItemCounts: vi.fn(),
}))

function createTestRouter(initialPath = '/') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div>Dashboard</div>' } },
      { path: '/work-items', component: { template: '<div>Work Items</div>' } },
      { path: '/repos', component: { template: '<div>Repos</div>' } },
      { path: '/attribution', component: { template: '<div>Attribution</div>' } },
      { path: '/activity', component: { template: '<div>Activity</div>' } },
      { path: '/activity/members/:user_id', component: { template: '<div>Member Activity</div>' } },
      { path: '/events', component: { template: '<div>Events</div>' } },
      { path: '/user', component: { template: '<div>User</div>' } },
      { path: '/usage', component: { template: '<div>Usage</div>' } },
      { path: '/usage/team', component: { template: '<div>Team Usage</div>' } },
      { path: '/sessions', component: { template: '<div>Sessions</div>' } },
      { path: '/admin/users', component: { template: '<div>Admin Users</div>' } },
      { path: '/admin/directory/offboarding', component: { template: '<div>Offboarding</div>' } },
      { path: '/admin/relay-planning', component: { template: '<div>Relay Planning</div>' } },
      { path: '/settings', component: { template: '<div>Settings</div>' } },
      { path: '/login', component: { template: '<div>Login</div>' } },
    ],
  })
  return router
}

describe('AppSidebar', () => {
  beforeEach(async () => {
    setActivePinia(createPinia())
    setLocale('en-US')
    vi.clearAllMocks()
    const api = await import('@/api/workItems') as any
    api.getWorkItemCounts.mockResolvedValue({
      data: {
        data: {
          quota_reset_approval_count: 0,
          quota_reset_admin_count: 0,
          ai_access_setup_count: 0,
          offboarding_count: 0,
          total_count: 0,
        },
      },
    })
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

  it('uses semantic route navigation with Element Plus actions and badges', async () => {
    const api = await import('@/api/workItems') as any
    api.getWorkItemCounts.mockResolvedValueOnce({
      data: {
        data: {
          quota_reset_approval_count: 2,
          quota_reset_admin_count: 0,
          ai_access_setup_count: 0,
          offboarding_count: 0,
          total_count: 2,
        },
      },
    })
    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(AppSidebar, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.find('nav').exists()).toBe(true)
    expect(wrapper.get('.h-14 button').classes()).toContain('el-button')
    const badge = wrapper.getComponent({ name: 'ElBadge' })
    expect(badge.props('badgeStyle')).toEqual({ position: 'static', transform: 'none' })
  })

  it('places the language toggle in the sidebar header, away from the account footer', async () => {
    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(AppSidebar, {
      global: { plugins: [createPinia(), router] },
    })

    const header = wrapper.get('.h-14')
    const footer = wrapper.get('div.border-t.p-4')

    expect(header.find('button').exists()).toBe(true)
    expect(footer.find('button[title="Logout"]').exists()).toBe(true)
    expect(footer.find(':scope > div > div').exists()).toBe(true)
  })

  it('omits the desktop brand controls from the mobile navigation surface', async () => {
    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(AppSidebar, {
      props: { mobile: true },
      global: { plugins: [createPinia(), router] },
    })

    expect(wrapper.find('.h-14').exists()).toBe(false)
    expect(wrapper.findAll('button')).toHaveLength(1)
    expect(wrapper.text()).not.toContain('AI Efficiency')
  })

  it('renders mobile navigation as a compact light surface', async () => {
    const router = createTestRouter()
    await router.push('/usage')
    await router.isReady()

    const wrapper = mount(AppSidebar, {
      props: { mobile: true },
      global: { plugins: [createPinia(), router] },
    })

    const sidebar = wrapper.get('aside')
    expect(sidebar.classes()).toContain('w-full')
    expect(sidebar.classes()).toContain('bg-white')
    expect(sidebar.classes()).toContain('text-slate-700')
    expect(sidebar.classes()).not.toContain('bg-gray-900')

    const navigation = wrapper.get('nav')
    expect(navigation.classes()).toContain('flex-none')
    expect(navigation.classes()).not.toContain('flex-1')

    const activeLink = wrapper.get('a[href="/usage"]')
    expect(activeLink.classes()).toContain('min-h-11')
    expect(activeLink.classes()).toContain('bg-blue-50')
    expect(activeLink.classes()).toContain('text-blue-700')

    expect(wrapper.get('div.border-t.p-4').classes()).toContain('border-slate-200')
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
    expect(linkTexts).toContain('Work Items')
    expect(linkTexts).toContain('AI Coding Activity')
    expect(linkTexts).not.toContain('Usage Records')
    expect(linkTexts).not.toContain('Code Repositories')
    expect(linkTexts).toContain('AI Setup & Configuration')
    expect(linkTexts).not.toContain('Team Usage')
    expect(linkTexts).not.toContain('My Usage')
    expect(links.map((l) => l.attributes('href'))).toContain('/usage')
    expect(links.map((l) => l.attributes('href'))).toContain('/work-items')
    expect(links.map((l) => l.attributes('href'))).toContain('/activity')
    expect(links.map((l) => l.attributes('href'))).not.toContain('/events')
    expect(links.map((l) => l.attributes('href'))).not.toContain('/user/usage')
    expect(links.map((l) => l.attributes('href'))).not.toContain('/team-usage')
    expect(links.map((l) => l.attributes('href'))).not.toContain('/usage/team')
    const setupIndex = links.findIndex((link) => link.attributes('href') === '/user')
    const workItemsIndex = links.findIndex((link) => link.attributes('href') === '/work-items')
    expect(setupIndex).toBeGreaterThanOrEqual(0)
    expect(workItemsIndex).toBeGreaterThan(setupIndex)
    expect(wrapper.text()).toContain('My Work')
    expect(wrapper.text()).not.toContain('Code & PR')
    expect(wrapper.text()).not.toContain('Administration')
    expect(linkTexts).not.toContain('Sessions')
    expect(linkTexts).not.toContain('Admin Console')
  })

  it('keeps AI Coding Activity active on descendant routes', async () => {
    const router = createTestRouter()
    await router.push('/activity/members/7')
    await router.isReady()
    const wrapper = mount(AppSidebar, { global: { plugins: [createPinia(), router] } })
    const link = wrapper.get('a[href="/activity"]')
    expect(link.classes()).toContain('bg-gray-800')
  })

  it('shows the Work Items badge when there is pending work', async () => {
    const api = await import('@/api/workItems') as any
    api.getWorkItemCounts.mockResolvedValueOnce({
      data: {
        data: {
          quota_reset_approval_count: 2,
          quota_reset_admin_count: 0,
          ai_access_setup_count: 0,
          offboarding_count: 0,
          total_count: 2,
        },
      },
    })
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
    await flushPromises()

    const badge = wrapper.get('[data-testid="sidebar-work-items-badge"]')
    expect(badge.text()).toBe('2')
  })

  it('hides the Work Items badge when the total is zero', async () => {
    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(AppSidebar, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="sidebar-work-items-badge"]').exists()).toBe(false)
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

  it('renders maintenance links for admin users', async () => {
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

    const links = wrapper.findAll('a')
    const offboardingLink = links.find((l) => l.text() === 'Offboarding Review')
    const relayPlanningLink = links.find((l) => l.text() === 'Relay Planning')
    expect(offboardingLink?.attributes('href')).toBe('/admin/directory/offboarding')
    expect(relayPlanningLink?.attributes('href')).toBe('/admin/relay-planning')
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
    expect(linkTexts).not.toContain('Offboarding Review')
    expect(linkTexts).not.toContain('Relay Planning')
  })

  it('applies active class to current route link', async () => {
    const router = createTestRouter()
    await router.push('/repos')
    await router.isReady()

    const pinia = createPinia()
    const auth = useAuthStore(pinia)
    auth.user = { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin', auth_source: 'sso' }
    const wrapper = mount(AppSidebar, {
      global: { plugins: [pinia, router] },
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

  it('keeps AI Usage Center active for nested usage routes', async () => {
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(AppSidebar, {
      global: { plugins: [createPinia(), router] },
    })

    const usageLink = wrapper.findAll('a').find((a) => a.text() === 'AI Usage Center')
    expect(usageLink).toBeTruthy()
    expect(usageLink!.classes()).toContain('bg-gray-800')
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

  it('displays only the username in the compact account summary', async () => {
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

    expect(wrapper.get('.min-w-0.flex-1.px-1.py-1.text-sm').text()).toBe('testuser')
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

    const accountLines = wrapper.get('.min-w-0.flex-1.px-1.py-1.text-sm').findAll('p')
    expect(accountLines).toHaveLength(1)
    expect(accountLines[0].classes()).toContain('truncate')
    expect(accountLines[0].attributes('title')).toBe('very-long-admin-account@example.com')
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
    const accountSummary = wrapper.get('.min-w-0.flex-1.px-1.py-1.text-sm')
    expect(accountSummary.element.tagName).toBe('DIV')
    expect(accountSummary.attributes('href')).toBeUndefined()
    expect(accountSummary.text()).toBe('testuser')

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

    await wrapper.get('.h-14 button').trigger('click')

    const linkTexts = wrapper.findAll('a').map((l) => l.text())
    expect(wrapper.text()).toContain('我的工作')
    expect(wrapper.text()).not.toContain('代码与 PR')
    expect(linkTexts).toContain('AI 使用中心')
    expect(linkTexts).toContain('AI 接入与配置')
    expect(linkTexts).not.toContain('我的用量')
    expect(linkTexts).toContain('AI 开发动态')
    expect(linkTexts).not.toContain('使用记录')
    expect(linkTexts).not.toContain('团队用量')
    expect(linkTexts).not.toContain('代码仓库')
  })
})
