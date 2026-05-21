import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import UserView from '@/views/UserView.vue'

vi.mock('@/api/user', () => ({
  getUserProviders: vi.fn(),
  createManagedKey: vi.fn(),
  regenerateManagedKey: vi.fn(),
}))

Object.assign(navigator, {
  clipboard: {
    writeText: vi.fn(),
  },
})

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div>Dashboard</div>' } },
      { path: '/user', component: UserView },
      { path: '/login', component: { template: '<div>Login</div>' } },
    ],
  })
}

async function mountUserView() {
  const { getUserProviders } = await import('@/api/user')
  ;(getUserProviders as any).mockResolvedValue({
    data: {
      data: {
        providers: [
          {
            id: 1,
            name: 'staging',
            display_name: 'Staging',
            base_url: 'https://staging.example.com',
            default_model: 'claude-sonnet',
            is_primary: false,
            managed_key: { state: 'missing' },
          },
          {
            id: 2,
            name: 'prod',
            display_name: 'Production',
            base_url: 'https://prod.example.com',
            default_model: 'claude-sonnet',
            is_primary: true,
            managed_key: { state: 'existing_hidden', api_key_id: 22 },
          },
        ],
        message: '',
      },
    },
  })

  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore(pinia)
  auth.token = 'token'
  auth.user = { id: 1, username: 'alice', email: 'alice@example.com', role: 'user', auth_source: 'sso' }

  const router = createTestRouter()
  await router.push('/user')
  await router.isReady()

  const wrapper = mount(UserView, {
    global: {
      plugins: [pinia, router],
      stubs: {
        AppLayout: {
          template: '<div><slot /></div>',
        },
      },
    },
  })
  await flushPromises()
  return { wrapper, router }
}

describe('UserView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.stubGlobal('confirm', vi.fn(() => true))
  })

  it('loads profile and provider data, selects primary provider by default, and renders commands', async () => {
    const { wrapper } = await mountUserView()
    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.text()).toContain('Production')
    expect(wrapper.text()).toContain('ae-cli --server http://localhost')
    expect(wrapper.text()).toContain('discover --provider prod')
  })

  it('switches providers and updates the discover command', async () => {
    const { wrapper } = await mountUserView()
    await wrapper.get('[data-testid="provider-1"]').trigger('click')
    expect(wrapper.text()).toContain('discover --provider staging')
  })

  it('reveals and copies a newly created secret only from session state', async () => {
    const { createManagedKey } = await import('@/api/user')
    ;(createManagedKey as any).mockResolvedValue({
      data: { data: { api_key_id: 7, name: 'ae-cli-auto', status: 'active', secret: 'sk-new' } },
    })

    const { wrapper } = await mountUserView()
    await wrapper.get('[data-testid="provider-1"]').trigger('click')
    await wrapper.get('[data-testid="create-key"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).not.toContain('sk-new')
    await wrapper.get('[data-testid="reveal-key"]').trigger('click')
    expect(wrapper.text()).toContain('sk-new')
    await wrapper.get('[data-testid="copy-key"]').trigger('click')
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('sk-new')
  })

  it('shows regenerate confirmation for existing hidden keys', async () => {
    const { regenerateManagedKey } = await import('@/api/user')
    ;(regenerateManagedKey as any).mockResolvedValue({
      data: { data: { api_key_id: 8, name: 'ae-cli-auto', status: 'active', secret: 'sk-regen' } },
    })

    const { wrapper } = await mountUserView()
    await wrapper.get('[data-testid="regenerate-key"]').trigger('click')
    await flushPromises()

    expect(globalThis.confirm).toHaveBeenCalled()
    expect(wrapper.text()).toContain('sk-regen')
  })
})
