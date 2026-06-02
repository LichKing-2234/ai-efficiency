import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import AuthorizePage from '@/views/oauth/AuthorizePage.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/oauth', () => ({
  approveAuthorization: vi.fn(),
}))

function createTestRouter(initialPath = '/oauth/authorize?client_id=ae-cli&redirect_uri=http%3A%2F%2F127.0.0.1%2Fcallback&state=state-1') {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', component: { template: '<div>Login</div>' } },
      { path: '/oauth/authorize', component: AuthorizePage },
    ],
  })
}

async function mountAuthorize(authenticated = true) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore(pinia)
  if (authenticated) {
    auth.token = 'jwt-token'
    auth.user = { id: 1, username: 'alice', email: 'alice@example.com', role: 'user', auth_source: 'sso' }
  }

  const router = createTestRouter()
  await router.push('/oauth/authorize?client_id=ae-cli&redirect_uri=http%3A%2F%2F127.0.0.1%2Fcallback&state=state-1')
  await router.isReady()

  const wrapper = mount(AuthorizePage, {
    global: { plugins: [pinia, router] },
  })
  await flushPromises()
  return wrapper
}

describe('AuthorizePage', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    setLocale('en-US')
    vi.clearAllMocks()
  })

  it('renders the unified auth experience for signed-in users', async () => {
    const wrapper = await mountAuthorize(true)

    expect(wrapper.text()).toContain('AI Efficiency Platform')
    expect(wrapper.text()).toContain('App Authorization')
    expect(wrapper.text()).toContain('Authorization request')
    expect(wrapper.text()).not.toContain('Recommended sign-in')
    expect(wrapper.text()).toContain('ae-cli')
    expect(wrapper.text()).toContain('Requested access')
    expect(wrapper.text()).toContain('Signed-in account')
    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.find('[data-testid="auth-language-toggle"]').exists()).toBe(true)
  })

  it('shows a sign-in path for unauthenticated users', async () => {
    const wrapper = await mountAuthorize(false)

    expect(wrapper.text()).toContain('Sign in to continue')
    expect(wrapper.find('a[href^="/login?redirect="]').exists()).toBe(true)
  })
})
