import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import DirectoryOffboardingView from '@/views/admin/DirectoryOffboardingView.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/directory', () => ({
  listDirectoryOffboardingCandidates: vi.fn(),
  disableDirectoryRelayUser: vi.fn(),
}))

function createRouterForOffboarding() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div>Home</div>' } },
      { path: '/admin/directory/offboarding', component: DirectoryOffboardingView },
    ],
  })
}

async function mountOffboarding() {
  const api = await import('@/api/directory') as any
  api.listDirectoryOffboardingCandidates.mockResolvedValue({
    data: {
      data: {
        items: [
          {
            user_id: 7,
            username: 'bob',
            email: 'bob@example.org',
            auth_source: 'ldap',
            relay_user_id: 99,
            reason: 'missing_from_latest_full_company_directory',
            directory_run_id: 3,
          },
        ],
      },
    },
  })
  api.disableDirectoryRelayUser.mockResolvedValue({ data: { data: { id: 8, status: 'succeeded' } } })

  const router = createRouterForOffboarding()
  await router.push('/admin/directory/offboarding')
  await router.isReady()
  const wrapper = mount(DirectoryOffboardingView, {
    global: {
      plugins: [router],
      stubs: { AppLayout: { template: '<div><slot /></div>' } },
    },
  })
  await flushPromises()
  return { wrapper, api }
}

describe('DirectoryOffboardingView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    setLocale('en-US')
  })

  it('requires email confirmation before disabling relay user', async () => {
    const { wrapper, api } = await mountOffboarding()

    expect(api.listDirectoryOffboardingCandidates).toHaveBeenCalledWith({ q: '' })
    expect(wrapper.text()).toContain('Directory Offboarding')
    expect(wrapper.text()).toContain('bob@example.org')
    expect(wrapper.text()).toContain('After disabling, this user will no longer be able to access AI services')
    expect(wrapper.find('input[type="number"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="disable-relay-user-7"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="confirm-email-7"]').setValue('bob@example.org')
    await wrapper.get('[data-testid="disable-relay-user-7"]').trigger('click')
    await flushPromises()

    expect(api.disableDirectoryRelayUser).toHaveBeenCalledWith(7, {
      confirm_email: 'bob@example.org',
      reason: 'missing_from_latest_full_company_directory',
    })
  })

  it('switches offboarding copy to Chinese', async () => {
    setLocale('zh-CN')
    const { wrapper } = await mountOffboarding()

    expect(wrapper.text()).toContain('组织架构离职处理')
    expect(wrapper.text()).toContain('禁用后，该用户将无法继续使用 AI 接入')
    expect(wrapper.text()).toContain('禁用 AI 接入')
  })
})
