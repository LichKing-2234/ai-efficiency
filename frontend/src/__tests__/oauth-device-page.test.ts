import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import DevicePage from '@/views/oauth/DevicePage.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/oauth', () => ({
  verifyDeviceAuthorization: vi.fn(),
}))

function createTestRouter(initialPath = '/oauth/device') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', component: { template: '<div>Login</div>' } },
      { path: '/oauth/device', component: DevicePage },
    ],
  })
  return router
}

describe('DevicePage', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
    setLocale('en-US')
    vi.clearAllMocks()
  })

  it('redirects unauthenticated users to login with redirect query', async () => {
    const router = createTestRouter()
    await router.push('/oauth/device')
    await router.isReady()

    mount(DevicePage, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe('/oauth/device')
  })

  it('submits approval and shows success state', async () => {
    const { verifyDeviceAuthorization } = await import('@/api/oauth')
    ;(verifyDeviceAuthorization as any).mockResolvedValue({ status: 'approved' })

    const pinia = createPinia()
    setActivePinia(pinia)
    const auth = useAuthStore()
    auth.token = 'jwt-token'
    auth.user = { id: 1, username: 'alice', email: 'alice@example.com', role: 'user', auth_source: 'sso' }

    const router = createTestRouter()
    await router.push('/oauth/device')
    await router.isReady()

    const wrapper = mount(DevicePage, {
      global: { plugins: [pinia, router] },
    })

    expect(wrapper.text()).toContain('AI Efficiency Platform')
    expect(wrapper.text()).toContain('Device Login')
    expect(wrapper.text()).toContain('Device approval')
    expect(wrapper.text()).not.toContain('Recommended sign-in')
    expect(wrapper.text()).toContain('Signed-in account')
    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.find('[data-testid="auth-language-toggle"]').exists()).toBe(true)
    expect(wrapper.find('[label-for="user-code"]').text()).toContain('User code')
    await wrapper.get('input[data-testid="device-code"]').setValue('abcd-efgh')
    await wrapper.find('button[data-action="approve"]').trigger('click')
    await flushPromises()

    expect(verifyDeviceAuthorization).toHaveBeenCalledWith({
      user_code: 'abcd-efgh',
      approved: true,
    })
    expect(wrapper.text()).toContain('Approved. You can return to the terminal.')
  })

  it('shows server validation errors for invalid codes', async () => {
    const { verifyDeviceAuthorization } = await import('@/api/oauth')
    ;(verifyDeviceAuthorization as any).mockRejectedValue({
      response: { data: { message: 'Code invalid or expired' } },
    })

    const pinia = createPinia()
    setActivePinia(pinia)
    const auth = useAuthStore()
    auth.token = 'jwt-token'

    const router = createTestRouter()
    await router.push('/oauth/device')
    await router.isReady()

    const wrapper = mount(DevicePage, {
      global: { plugins: [pinia, router] },
    })

    await wrapper.get('input[data-testid="device-code"]').setValue('bad-code')
    await wrapper.find('button[data-action="deny"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Code invalid or expired')
  })

  it('presents device decisions as Element Plus actions', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const auth = useAuthStore()
    auth.token = 'jwt-token'

    const router = createTestRouter()
    await router.push('/oauth/device')
    await router.isReady()

    const wrapper = mount(DevicePage, {
      global: { plugins: [pinia, router] },
    })

    expect(wrapper.get('[data-action="deny"]').classes()).toContain('el-button')
    expect(wrapper.get('[data-action="approve"]').classes()).toContain('el-button')
  })
})
