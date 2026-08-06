import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import LoginView from '@/views/LoginView.vue'
import { setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

// Mock auth API
vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  getMe: vi.fn(),
  devLogin: vi.fn(),
  getAuthOptions: vi.fn(),
}))

function createTestRouter(initialPath = '/login') {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', component: LoginView },
      { path: '/', component: { template: '<div>Dashboard</div>' } },
      { path: '/repos', component: { template: '<div>Repos</div>' } },
      { path: '/attribution', component: { template: '<div>Attribution</div>' } },
    ],
  })
}

describe('LoginView', () => {
  beforeEach(async () => {
    setActivePinia(createPinia())
    localStorage.clear()
    setLocale('en-US')
    vi.clearAllMocks()
    const { getAuthOptions } = await import('@/api/auth')
    ;(getAuthOptions as any).mockResolvedValue({
      data: { data: { ldap_enabled: true, dev_login_enabled: true } },
    })
  })

  it('renders login form', () => {
    const router = createTestRouter()
    const wrapper = mount(LoginView, {
      global: { plugins: [createPinia(), router] },
    })

    expect(wrapper.find('h1').text()).toBe('AI Efficiency Platform')
    expect(wrapper.text()).toContain('Recommended sign-in')
    expect(wrapper.find('[data-testid="auth-language-toggle"]').exists()).toBe(true)
    expect(wrapper.find('input#username').exists()).toBe(true)
    expect(wrapper.find('input#password').exists()).toBe(true)
    expect(wrapper.find('select#source').exists()).toBe(true)
  })

  it('renders dev login button when enabled', async () => {
    const router = createTestRouter()
    const wrapper = mount(LoginView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const buttons = wrapper.findAll('button')
    const devBtn = buttons.find((b) => b.text().includes('Dev Login'))
    expect(devBtn).toBeTruthy()
  })

  it('defaults to LDAP auth source when LDAP is enabled', async () => {
    const router = createTestRouter()
    const wrapper = mount(LoginView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const select = wrapper.find('select#source')
    expect((select.element as HTMLSelectElement).value).toBe('LDAP')
  })

  it('shows only SSO when LDAP is not enabled', async () => {
    const { getAuthOptions } = await import('@/api/auth')
    ;(getAuthOptions as any).mockResolvedValue({
      data: { data: { ldap_enabled: false, dev_login_enabled: true } },
    })
    const router = createTestRouter()
    const wrapper = mount(LoginView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const select = wrapper.find('select#source')
    const options = wrapper.findAll('select#source option')
    expect((select.element as HTMLSelectElement).value).toBe('SSO')
    expect(options.map((option) => (option.element as HTMLOptionElement).value)).toEqual(['SSO'])
  })

  it('hides dev login when disabled', async () => {
    const { getAuthOptions } = await import('@/api/auth')
    ;(getAuthOptions as any).mockResolvedValue({
      data: { data: { ldap_enabled: true, dev_login_enabled: false } },
    })
    const router = createTestRouter()
    const wrapper = mount(LoginView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.text()).not.toContain('DEV MODE')
    expect(wrapper.findAll('button').some((button) => button.text().includes('Dev Login'))).toBe(false)
  })

  it('switches login copy to Chinese', async () => {
    const router = createTestRouter()
    const wrapper = mount(LoginView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    await wrapper.get('[data-testid="auth-language-toggle"]').trigger('click')

    expect(wrapper.text()).toContain('AI 效能平台')
    expect(wrapper.text()).toContain('AI 使用、接入和代码可见性')
    expect(wrapper.text()).toContain('推荐登录方式')
    expect(wrapper.find('label[for="username"]').text()).toContain('邮箱')
  })

  it('shows error on failed login', async () => {
    const { login: mockLogin } = await import('@/api/auth')
    ;(mockLogin as any).mockRejectedValue({
      response: { data: { message: 'Invalid credentials' } },
    })

    const router = createTestRouter()
    const wrapper = mount(LoginView, {
      global: { plugins: [createPinia(), router] },
    })

    await wrapper.find('input#username').setValue('bad')
    await wrapper.find('input#password').setValue('bad')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('Invalid credentials')
  })

  it('dev login uses the store transition, hydrates the user, and redirects', async () => {
    const { devLogin: mockDevLogin, getMe: mockGetMe } = await import('@/api/auth')
    ;(mockDevLogin as any).mockResolvedValue({
      data: { data: { token: 'dev-token', refresh_token: 'dev-refresh' } },
    })
    ;(mockGetMe as any).mockResolvedValue({
      data: {
        data: {
          id: 1,
          username: 'alice',
          email: 'alice@example.com',
          role: 'admin',
          auth_source: 'dev',
        },
      },
    })

    const pinia = createPinia()
    setActivePinia(pinia)
    const auth = useAuthStore(pinia)
    const devLoginTransition = vi.spyOn(auth, 'devLogin')
    const router = createTestRouter()
    await router.push('/login')
    await router.isReady()

    const wrapper = mount(LoginView, {
      global: { plugins: [pinia, router] },
    })
    await flushPromises()

    const buttons = wrapper.findAll('button')
    const devBtn = buttons.find((b) => b.text().includes('Dev Login'))
    await devBtn!.trigger('click')

    await flushPromises()

    expect(devLoginTransition).toHaveBeenCalledTimes(1)
    expect(mockDevLogin).toHaveBeenCalledTimes(1)
    expect(localStorage.getItem('token')).toBe('dev-token')
    expect(localStorage.getItem('refresh_token')).toBe('dev-refresh')
    expect(auth.token).toBe('dev-token')
    expect(auth.user).toEqual({
      id: 1,
      username: 'alice',
      email: 'alice@example.com',
      role: 'admin',
      auth_source: 'dev',
    })
    expect(router.currentRoute.value.path).toBe('/')
  })

  it('dev login returns to a safe requested attribution route', async () => {
    const { devLogin: mockDevLogin, getMe: mockGetMe } = await import('@/api/auth')
    ;(mockDevLogin as any).mockResolvedValue({
      data: { data: { token: 'dev-token', refresh_token: 'dev-refresh' } },
    })
    ;(mockGetMe as any).mockResolvedValue({
      data: { data: { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin' } },
    })

    const router = createTestRouter()
    await router.push('/login?redirect=/attribution')
    await router.isReady()

    const wrapper = mount(LoginView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const devBtn = wrapper.findAll('button').find((button) => button.text().includes('Dev Login'))
    await devBtn!.trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.path).toBe('/attribution')
  })

  // --- New tests for uncovered lines ---

  it('successful login redirects to dashboard', async () => {
    const { login: mockLogin, getMe: mockGetMe } = await import('@/api/auth')
    ;(mockLogin as any).mockResolvedValue({
      data: { data: { token: 'jwt', refresh_token: 'rt' } },
    })
    ;(mockGetMe as any).mockResolvedValue({
      data: { data: { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin' } },
    })

    const router = createTestRouter()
    await router.push('/login')
    await router.isReady()

    const wrapper = mount(LoginView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    await wrapper.find('input#username').setValue('admin')
    await wrapper.find('input#password').setValue('pass')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(mockLogin).toHaveBeenCalledWith({ username: 'admin', password: 'pass', source: 'LDAP' })
    expect(router.currentRoute.value.path).toBe('/')
  })

  it('login redirects to query redirect param', async () => {
    const { login: mockLogin, getMe: mockGetMe } = await import('@/api/auth')
    ;(mockLogin as any).mockResolvedValue({
      data: { data: { token: 'jwt', refresh_token: 'rt' } },
    })
    ;(mockGetMe as any).mockResolvedValue({
      data: { data: { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin' } },
    })

    const router = createTestRouter()
    await router.push('/login?redirect=/repos')
    await router.isReady()

    const wrapper = mount(LoginView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    await wrapper.find('input#username').setValue('admin')
    await wrapper.find('input#password').setValue('pass')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(router.currentRoute.value.path).toBe('/repos')
  })

  it('login sanitizes malicious redirect param', async () => {
    const { login: mockLogin, getMe: mockGetMe } = await import('@/api/auth')
    ;(mockLogin as any).mockResolvedValue({
      data: { data: { token: 'jwt', refresh_token: 'rt' } },
    })
    ;(mockGetMe as any).mockResolvedValue({
      data: { data: { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin' } },
    })

    const router = createTestRouter()
    await router.push('/login?redirect=//evil.com')
    await router.isReady()

    const wrapper = mount(LoginView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    await wrapper.find('input#username').setValue('admin')
    await wrapper.find('input#password').setValue('pass')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    // Should redirect to / instead of //evil.com
    expect(router.currentRoute.value.path).toBe('/')
  })

  it('shows generic error when login fails without message', async () => {
    const { login: mockLogin } = await import('@/api/auth')
    ;(mockLogin as any).mockRejectedValue(new Error('network error'))

    const router = createTestRouter()
    const wrapper = mount(LoginView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    await wrapper.find('input#username').setValue('admin')
    await wrapper.find('input#password').setValue('pass')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('Login failed. Please try again.')
  })

  it('shows loading state during login', async () => {
    const { login: mockLogin } = await import('@/api/auth')
    let resolveLogin: any
    ;(mockLogin as any).mockReturnValue(new Promise((r) => { resolveLogin = r }))

    const router = createTestRouter()
    const wrapper = mount(LoginView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    await wrapper.find('input#username').setValue('admin')
    await wrapper.find('input#password').setValue('pass')
    await wrapper.find('form').trigger('submit')
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('Signing in...')

    // Resolve to clean up
    resolveLogin({ data: { data: { token: 't', refresh_token: 'r' } } })
    await flushPromises()
  })

  it('dev login shows error on failure', async () => {
    const { devLogin: mockDevLogin } = await import('@/api/auth')
    ;(mockDevLogin as any).mockRejectedValue({
      response: { data: { message: 'Dev mode disabled' } },
    })

    const router = createTestRouter()
    const wrapper = mount(LoginView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const devBtn = wrapper.findAll('button').find((b) => b.text().includes('Dev Login'))
    await devBtn!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Dev mode disabled')
  })

  it('dev login shows generic error without message', async () => {
    const { devLogin: mockDevLogin } = await import('@/api/auth')
    ;(mockDevLogin as any).mockRejectedValue(new Error('fail'))

    const router = createTestRouter()
    const wrapper = mount(LoginView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const devBtn = wrapper.findAll('button').find((b) => b.text().includes('Dev Login'))
    await devBtn!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Dev login failed.')
  })

  it('dev login handles null data response', async () => {
    const { devLogin: mockDevLogin } = await import('@/api/auth')
    ;(mockDevLogin as any).mockResolvedValue({
      data: { data: null },
    })

    const router = createTestRouter()
    await router.push('/login')
    await router.isReady()

    const wrapper = mount(LoginView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const devBtn = wrapper.findAll('button').find((b) => b.text().includes('Dev Login'))
    await devBtn!.trigger('click')
    await flushPromises()

    // Should not redirect, no token stored
    expect(localStorage.getItem('token')).toBeNull()
    expect(router.currentRoute.value.path).toBe('/login')
  })

  it('dev login without refresh_token does not store it', async () => {
    const { devLogin: mockDevLogin, getMe: mockGetMe } = await import('@/api/auth')
    ;(mockDevLogin as any).mockResolvedValue({
      data: { data: { token: 'dev-token' } },
    })
    ;(mockGetMe as any).mockResolvedValue({
      data: { data: { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin' } },
    })

    const router = createTestRouter()
    await router.push('/login')
    await router.isReady()

    const wrapper = mount(LoginView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const devBtn = wrapper.findAll('button').find((b) => b.text().includes('Dev Login'))
    await devBtn!.trigger('click')
    await flushPromises()

    expect(localStorage.getItem('token')).toBe('dev-token')
    expect(localStorage.getItem('refresh_token')).toBeNull()
  })

  it('can select LDAP auth source', async () => {
    const router = createTestRouter()
    const wrapper = mount(LoginView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const select = wrapper.find('select#source')
    await select.setValue('LDAP')
    expect((select.element as HTMLSelectElement).value).toBe('LDAP')
  })
})
