import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import AdminUsersView from '@/views/admin/AdminUsersView.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/adminUsers', () => ({
  assignAdminUserSubscription: vi.fn(),
  getAdminUserSubscriptionJob: vi.fn(),
  getLatestAdminUserSubscriptionJob: vi.fn(),
  listAdminUsers: vi.fn(),
  listAdminUserSubscriptionOptions: vi.fn(),
  manageAdminUserSubscriptions: vi.fn(),
  revealAdminUserRelayPassword: vi.fn(),
  startAdminUserSubscriptionJob: vi.fn(),
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

async function mountAdminUsersView(
  path = '/admin/users',
  userListFactory?: (params: any) => {
    items: any[]
    total?: number
    page?: number
    page_size?: number
  },
) {
  const { getLatestAdminUserSubscriptionJob, listAdminUsers, listAdminUserSubscriptionOptions } = await import('@/api/adminUsers')
  ;(listAdminUsers as any).mockImplementation((params: any) => Promise.resolve({
    data: {
      data: userListFactory?.(params) ?? {
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
          {
            id: 8,
            username: 'bob',
            email: 'bob@example.org',
            role: 'user',
            auth_source: 'ldap',
            relay_user_id: 99,
            relay_auth_password: '',
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
  ;(listAdminUserSubscriptionOptions as any).mockResolvedValue({
    data: {
      data: {
        providers: [
          {
            id: 3,
            name: 'sub2api',
            display_name: 'Sub2API',
            groups: [
              {
                group_id: '42',
                group_name: 'Group Alpha',
                platform: 'openai',
                subscription_type: 'subscription',
              },
            ],
          },
        ],
      },
    },
  })
  if (!(getLatestAdminUserSubscriptionJob as any).getMockImplementation()) {
    ;(getLatestAdminUserSubscriptionJob as any).mockResolvedValue({ data: { data: null } })
  }

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

function subscriptionJob(overrides: Record<string, any> = {}) {
  return {
    id: 12,
    status: 'queued',
    phase: 'queued',
    scope: 'selected',
    operation: 'add',
    provider_id: 3,
    group_id: '42',
    total_count: 1,
    processed_count: 0,
    success_count: 0,
    skipped_count: 0,
    failed_count: 0,
    results: [],
    ...overrides,
  }
}

describe('AdminUsersView', () => {
  beforeEach(() => {
    setLocale('en-US')
    vi.resetAllMocks()
  })

  afterEach(() => {
    vi.useRealTimers()
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

  it('adds a subscription for one selected local user through a polled job workflow', async () => {
    vi.useFakeTimers()
    const { getAdminUserSubscriptionJob, manageAdminUserSubscriptions, startAdminUserSubscriptionJob } = await import('@/api/adminUsers')
    ;(startAdminUserSubscriptionJob as any).mockResolvedValue({
      data: { data: subscriptionJob({ status: 'running', phase: 'processing', total_count: 1, processed_count: 0 }) },
    })
    ;(getAdminUserSubscriptionJob as any).mockResolvedValue({
      data: {
        data: subscriptionJob({
          status: 'completed',
          phase: 'completed',
          total_count: 1,
          processed_count: 1,
          success_count: 1,
          results: [{ user_id: 7, username: 'alice', email: 'alice@example.com', status: 'success' }],
        }),
      },
    })

    const { wrapper } = await mountAdminUsersView()

    expect(wrapper.text()).toContain('Subscription management')
    await wrapper.get('[data-testid="select-user-7"]').setValue(true)
    await wrapper.get('[data-testid="subscription-provider"]').setValue('3')
    await wrapper.get('[data-testid="subscription-group"]').setValue('42')
    await wrapper.get('[data-testid="subscription-days"]').setValue('60')
    await wrapper.get('[data-testid="manage-subscriptions-submit"]').trigger('click')
    await flushPromises()

    expect(startAdminUserSubscriptionJob).toHaveBeenCalledWith({
      scope: 'selected',
      user_ids: [7],
      operation: 'add',
      provider_id: 3,
      group_id: '42',
      validity_days: 60,
    })
    expect(manageAdminUserSubscriptions).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Processing: 0 / 1')

    await vi.advanceTimersByTimeAsync(1500)
    await flushPromises()

    expect(getAdminUserSubscriptionJob).toHaveBeenCalledWith(12)
    expect(wrapper.text()).toContain('Completed: 1 succeeded, 0 skipped, 0 failed')
    expect(wrapper.text()).toContain('alice')
  })

  it('shows a real indeterminate select-all checkbox for partial visible selection', async () => {
    const { wrapper } = await mountAdminUsersView()
    const selectAll = wrapper.get('[data-testid="select-all-users"]').element as HTMLInputElement

    expect(selectAll.indeterminate).toBe(false)

    await wrapper.get('[data-testid="select-user-7"]').setValue(true)
    await flushPromises()

    expect(selectAll.checked).toBe(false)
    expect(selectAll.indeterminate).toBe(true)
    expect(selectAll.getAttribute('aria-checked')).toBe('mixed')

    await wrapper.get('[data-testid="select-user-8"]').setValue(true)
    await flushPromises()

    expect(selectAll.checked).toBe(true)
    expect(selectAll.indeterminate).toBe(false)
  })

  it('extends subscriptions for multiple selected local users', async () => {
    const { startAdminUserSubscriptionJob } = await import('@/api/adminUsers')
    ;(startAdminUserSubscriptionJob as any).mockResolvedValue({
      data: { data: subscriptionJob({ status: 'completed', phase: 'completed', total_count: 2, processed_count: 2, success_count: 2 }) },
    })

    const { wrapper } = await mountAdminUsersView()

    await wrapper.get('[data-testid="select-user-7"]').setValue(true)
    await wrapper.get('[data-testid="select-user-8"]').setValue(true)
    await wrapper.get('[data-testid="subscription-operation"]').setValue('extend')
    await wrapper.get('[data-testid="subscription-provider"]').setValue('3')
    await wrapper.get('[data-testid="subscription-group"]').setValue('42')
    await wrapper.get('[data-testid="subscription-days"]').setValue('14')
    await wrapper.get('[data-testid="manage-subscriptions-submit"]').trigger('click')
    await flushPromises()

    expect(startAdminUserSubscriptionJob).toHaveBeenCalledWith({
      scope: 'selected',
      user_ids: [7, 8],
      operation: 'extend',
      provider_id: 3,
      group_id: '42',
      days: 14,
    })
  })

  it('preserves selected users across pagination before submitting selected scope', async () => {
    const { startAdminUserSubscriptionJob } = await import('@/api/adminUsers')
    ;(startAdminUserSubscriptionJob as any).mockResolvedValue({
      data: { data: subscriptionJob({ status: 'completed', phase: 'completed', total_count: 2, processed_count: 2, success_count: 2 }) },
    })

    const { wrapper } = await mountAdminUsersView('/admin/users', (params: any) => ({
      items: params?.page === 2
        ? [
            {
              id: 9,
              username: 'carol',
              email: 'carol@example.net',
              role: 'user',
              auth_source: 'ldap',
              relay_user_id: 101,
              relay_auth_password: '',
              created_at: '2026-05-26T00:00:00Z',
              updated_at: '2026-05-26T01:00:00Z',
            },
          ]
        : [
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
      total: 40,
      page: params?.page ?? 1,
      page_size: params?.page_size ?? 20,
    }))

    await wrapper.get('[data-testid="admin-users-next-page"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="select-user-9"]').setValue(true)
    await wrapper.get('[data-testid="admin-users-prev-page"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="select-user-7"]').setValue(true)
    await wrapper.get('[data-testid="subscription-provider"]').setValue('3')
    await wrapper.get('[data-testid="subscription-group"]').setValue('42')
    await wrapper.get('[data-testid="manage-subscriptions-submit"]').trigger('click')
    await flushPromises()

    expect(startAdminUserSubscriptionJob).toHaveBeenCalledWith({
      scope: 'selected',
      user_ids: [9, 7],
      operation: 'add',
      provider_id: 3,
      group_id: '42',
      validity_days: 30,
    })
  })

  it('removes subscriptions for all mapped users only after explicit confirmation', async () => {
    const { startAdminUserSubscriptionJob } = await import('@/api/adminUsers')
    ;(startAdminUserSubscriptionJob as any).mockResolvedValue({
      data: { data: subscriptionJob({ status: 'completed', phase: 'completed', scope: 'all_mapped', operation: 'remove', total_count: 120, processed_count: 120, success_count: 120 }) },
    })

    const { wrapper } = await mountAdminUsersView()

    await wrapper.get('[data-testid="subscription-scope"]').setValue('all_mapped')
    await wrapper.get('[data-testid="subscription-operation"]').setValue('remove')
    await wrapper.get('[data-testid="subscription-provider"]').setValue('3')
    await wrapper.get('[data-testid="subscription-group"]').setValue('42')
    expect((wrapper.get('[data-testid="manage-subscriptions-submit"]').element as HTMLButtonElement).disabled).toBe(true)

    await wrapper.get('[data-testid="confirm-remove-subscription"]').setValue(true)
    await wrapper.get('[data-testid="manage-subscriptions-submit"]').trigger('click')
    await flushPromises()

    expect(startAdminUserSubscriptionJob).toHaveBeenCalledWith({
      scope: 'all_mapped',
      operation: 'remove',
      provider_id: 3,
      group_id: '42',
    })
  })

  it('recovers the latest running subscription job on mount and keeps polling it', async () => {
    vi.useFakeTimers()
    const { getAdminUserSubscriptionJob, getLatestAdminUserSubscriptionJob } = await import('@/api/adminUsers')
    ;(getLatestAdminUserSubscriptionJob as any).mockResolvedValue({
      data: { data: subscriptionJob({ id: 44, status: 'running', phase: 'processing', total_count: 3, processed_count: 1 }) },
    })
    ;(getAdminUserSubscriptionJob as any).mockResolvedValue({
      data: { data: subscriptionJob({ id: 44, status: 'completed', phase: 'completed', total_count: 3, processed_count: 3, success_count: 2, skipped_count: 1 }) },
    })

    const { wrapper } = await mountAdminUsersView()

    expect(wrapper.text()).toContain('Processing: 1 / 3')

    await vi.advanceTimersByTimeAsync(1500)
    await flushPromises()

    expect(getAdminUserSubscriptionJob).toHaveBeenCalledWith(44)
    expect(wrapper.text()).toContain('Completed: 2 succeeded, 1 skipped, 0 failed')
  })

  it('disables selection controls while a subscription job is active and keeps polling', async () => {
    vi.useFakeTimers()
    const { getAdminUserSubscriptionJob, getLatestAdminUserSubscriptionJob } = await import('@/api/adminUsers')
    ;(getLatestAdminUserSubscriptionJob as any).mockResolvedValue({
      data: { data: subscriptionJob({ id: 45, status: 'running', phase: 'processing', total_count: 2, processed_count: 1 }) },
    })
    ;(getAdminUserSubscriptionJob as any).mockResolvedValue({
      data: { data: subscriptionJob({ id: 45, status: 'running', phase: 'processing', total_count: 2, processed_count: 1 }) },
    })

    const { wrapper } = await mountAdminUsersView()

    expect((wrapper.get('[data-testid="select-user-7"]').element as HTMLInputElement).disabled).toBe(true)
    expect((wrapper.get('[data-testid="select-all-users"]').element as HTMLInputElement).disabled).toBe(true)

    await vi.advanceTimersByTimeAsync(1500)
    await flushPromises()

    expect(getAdminUserSubscriptionJob).toHaveBeenCalledWith(45)
    expect(wrapper.text()).toContain('Processing: 1 / 2')
  })
})
