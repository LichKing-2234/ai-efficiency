import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import AdminUsersView from '@/views/admin/AdminUsersView.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/adminUsers', () => ({
  listAdminUsers: vi.fn(),
  revealAdminUserRelayPassword: vi.fn(),
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
      { path: '/admin/users', component: AdminUsersView },
      { path: '/login', component: { template: '<div>Login</div>' } },
    ],
  })
}

async function mountAdminUsersView(path = '/admin/users') {
  const { listAdminUsers } = await import('@/api/adminUsers')
  ;(listAdminUsers as any).mockImplementation((params: any) => Promise.resolve({
    data: {
      data: {
        items: [
          {
            id: 7,
            username: 'alice',
            email: 'alice@example.com',
            role: 'user',
            auth_source: 'ldap',
            relay_user_id: 42,
            relay_auth_password: 'encrypted-relay-password-ciphertext',
            created_at: '2026-05-26T00:00:00Z',
            updated_at: '2026-05-26T01:00:00Z',
          },
        ],
        total: 120,
        page: params?.page ?? 1,
        page_size: params?.page_size ?? 20,
      },
    },
  }))

  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore(pinia)
  auth.token = 'token'
  auth.user = { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin', auth_source: 'relay_sso' }

  const router = createTestRouter()
  await router.push(path)
  await router.isReady()

  const wrapper = mount(AdminUsersView, {
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
  return { wrapper, router, listAdminUsers }
}

describe('AdminUsersView', () => {
  beforeEach(() => {
    setLocale('en-US')
    vi.clearAllMocks()
  })

  it('loads and renders local users with pagination controls', async () => {
    const { wrapper, listAdminUsers } = await mountAdminUsersView()

    expect(listAdminUsers).toHaveBeenCalledWith({ q: '', page: 1, page_size: 20 })
    expect(wrapper.text()).toContain('Users & Access')
    expect(wrapper.text()).toContain('Relay mapping')
    expect(wrapper.text()).toContain('Access status')
    expect(wrapper.text()).toContain('alice')
    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.text()).toContain('ldap')
    expect(wrapper.text()).toContain('42')
    expect(wrapper.text()).toContain('Configured')
    expect(wrapper.text()).not.toContain('encrypted-relay-password-ciphertext')
    expect(wrapper.text()).toContain('120 total')
    expect(wrapper.text()).toContain('Page 1 / 6')
  })

  it('renders the primary admin users workflow in Chinese', async () => {
    setLocale('zh-CN')

    const { wrapper } = await mountAdminUsersView()

    expect(wrapper.text()).toContain('用户与接入')
    expect(wrapper.text()).toContain('管理本地用户、relay 身份映射和凭据风险操作')
    expect(wrapper.text()).toContain('搜索')
    expect(wrapper.text()).toContain('本地用户')
    expect(wrapper.text()).toContain('复制明文')
  })

  it('searches from page one when the search button is clicked', async () => {
    const { wrapper, listAdminUsers } = await mountAdminUsersView()

    await wrapper.get('[data-testid="admin-users-search"]').setValue('alice@example.com')
    await wrapper.get('[data-testid="admin-users-search-button"]').trigger('click')
    await flushPromises()

    expect((listAdminUsers as any).mock.calls.at(-1)[0]).toEqual({ q: 'alice@example.com', page: 1, page_size: 20 })
  })

  it('updates page size and next page params', async () => {
    const { wrapper, listAdminUsers } = await mountAdminUsersView()

    await wrapper.get('[data-testid="admin-users-page-size"]').setValue('50')
    await flushPromises()
    expect((listAdminUsers as any).mock.calls.at(-1)[0]).toEqual({ q: '', page: 1, page_size: 50 })

    await wrapper.get('[data-testid="admin-users-next-page"]').trigger('click')
    await flushPromises()
    expect((listAdminUsers as any).mock.calls.at(-1)[0]).toEqual({ q: '', page: 2, page_size: 50 })
  })

  it('restores and persists search pagination state in the URL query', async () => {
    const { wrapper, router, listAdminUsers } = await mountAdminUsersView('/admin/users?q=alice&page=2&page_size=50')

    expect((listAdminUsers as any).mock.calls[0][0]).toEqual({ q: 'alice', page: 2, page_size: 50 })

    await wrapper.get('[data-testid="admin-users-page-size"]').setValue('20')
    await flushPromises()

    expect(router.currentRoute.value.query.q).toBe('alice')
    expect(router.currentRoute.value.query.page_size).toBeUndefined()
  })

  it('copies encrypted ciphertext without calling reveal', async () => {
    const { wrapper } = await mountAdminUsersView()
    const { revealAdminUserRelayPassword } = await import('@/api/adminUsers')

    await wrapper.get('[data-testid="copy-encrypted-7"]').trigger('click')

    expect(revealAdminUserRelayPassword).not.toHaveBeenCalled()
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('encrypted-relay-password-ciphertext')
  })

  it('requires explicit confirmation before copying plaintext from reveal', async () => {
    const { revealAdminUserRelayPassword } = await import('@/api/adminUsers')
    ;(revealAdminUserRelayPassword as any).mockResolvedValue({
      data: { data: { password: 'test-password' } },
    })

    const { wrapper } = await mountAdminUsersView()
    await wrapper.get('[data-testid="copy-plaintext-7"]').trigger('click')
    await flushPromises()

    expect(revealAdminUserRelayPassword).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Plaintext relay passwords are sensitive')

    await wrapper.get('[data-testid="confirm-copy-plaintext-7"]').trigger('click')
    await flushPromises()

    expect(revealAdminUserRelayPassword).toHaveBeenCalledWith(7)
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('test-password')
    expect(wrapper.text()).toContain('Copied plaintext')
    expect(wrapper.text()).not.toContain('test-password')
  })
})
