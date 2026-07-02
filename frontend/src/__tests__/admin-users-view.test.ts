import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import AppToastHost from '@/components/AppToastHost.vue'
import AdminUsersView from '@/views/admin/AdminUsersView.vue'
import { setLocale } from '@/i18n'
import { resetToastsForTest } from '@/composables/useToast'

vi.mock('@/api/adminUsers', () => ({
  assignAdminUserSubscription: vi.fn(),
  disableAdminUserAccess: vi.fn(),
  getAdminUserSubscriptionJob: vi.fn(),
	  getLatestAdminUserSubscriptionJob: vi.fn(),
	  listAdminUserDepartments: vi.fn(),
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
	  const { getLatestAdminUserSubscriptionJob, listAdminUserDepartments, listAdminUsers, listAdminUserSubscriptionOptions } = await import('@/api/adminUsers')
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
		            department: {
		              external_id: 'dept-alpha',
		              name: 'Department Alpha',
		              path: '1.781448',
		              display_path: 'Department Alpha',
		            },
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
	  ;(listAdminUserDepartments as any).mockResolvedValue({
	    data: {
	      data: {
	        items: [
		          {
			            external_id: 'dept-alpha',
			            name: 'Department Alpha',
			            path: '1.781448',
			            display_path: 'Department Alpha',
			            depth: 0,
		            child_count: 1,
		            member_count: 1,
		            matched_user_count: 1,
		            subtree_member_count: 2,
		            subtree_matched_user_count: 2,
		          },
		          {
		            external_id: 'dept-alpha-team-one',
			            parent_external_id: 'dept-alpha',
			            name: 'Team One',
			            path: '1.781448.1683962',
			            display_path: 'Department Alpha / Team One',
		            depth: 1,
		            child_count: 0,
		            member_count: 1,
		            matched_user_count: 1,
		            subtree_member_count: 1,
		            subtree_matched_user_count: 1,
		            representative_count: 2,
		            matched_representative_count: 1,
		          },
			          {
			            external_id: 'dept-beta',
			            name: 'Department Beta',
			            path: '1.1178135',
			            display_path: 'Department Beta',
			            depth: 0,
			            child_count: 0,
			            member_count: 2,
			            matched_user_count: 1,
			            subtree_member_count: 2,
			            subtree_matched_user_count: 1,
			          },
			          {
			            external_id: 'dept-gamma',
			            parent_external_id: 'dept-missing',
			            name: 'Department Gamma',
			            path: '1.999999',
			            display_path: 'Department Gamma',
			            depth: 0,
			            child_count: 0,
			            member_count: 1,
			            matched_user_count: 1,
			            subtree_member_count: 1,
			            subtree_matched_user_count: 1,
			          },
			        ],
	      },
	    },
	  })
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
          components: { AppToastHost },
          template: '<div><slot /><AppToastHost /></div>',
        },
      },
    },
  })
  await flushPromises()
	  return { wrapper, router, listAdminUserDepartments, listAdminUsers }
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
    resetToastsForTest()
  })

  afterEach(() => {
    vi.useRealTimers()
    resetToastsForTest()
  })

  it('loads and renders local users with pagination controls', async () => {
    const { wrapper, listAdminUsers } = await mountAdminUsersView()

    expect(listAdminUsers).toHaveBeenCalledWith({ q: '', page: 1, page_size: 20 })
    expect(wrapper.text()).toContain('Users & Access')
    expect(wrapper.text()).toContain('Relay mapping')
	    expect(wrapper.text()).toContain('Access status')
	    expect(wrapper.text()).toContain('Department')
	    expect(wrapper.text()).toContain('Department Alpha')
	    expect(wrapper.text()).toContain('alice')
    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.text()).toContain('ldap')
    expect(wrapper.text()).toContain('42')
    expect(wrapper.text()).toContain('Configured')
    expect(wrapper.text()).not.toContain('encrypted-relay-password-ciphertext')
    expect(wrapper.text()).toContain('120 total')
    expect(wrapper.text()).toContain('Page 1 / 6')
	  })

	  it('filters users by department and keeps the filter in the URL', async () => {
	    const { wrapper, router, listAdminUsers } = await mountAdminUsersView()

	    await wrapper.get('[data-testid="admin-users-department-filter"]').setValue('dept-alpha')
	    await flushPromises()

	    expect((listAdminUsers as any).mock.calls.at(-1)[0]).toEqual({
	      q: '',
	      department_id: 'dept-alpha',
	      page: 1,
	      page_size: 20,
	    })
	    expect(router.currentRoute.value.query.department_id).toBe('dept-alpha')
	  })

  it('filters users by access status and keeps the filter in the URL', async () => {
    const { wrapper, router, listAdminUsers } = await mountAdminUsersView()

    await wrapper.get('[data-testid="admin-users-access-status-filter"]').setValue('disabled')
    await flushPromises()

    expect((listAdminUsers as any).mock.calls.at(-1)[0]).toEqual({
      q: '',
      access_status: 'disabled',
      page: 1,
      page_size: 20,
    })
    expect(router.currentRoute.value.query.access_status).toBe('disabled')
  })

  it('renders disabled access status in Chinese', async () => {
    setLocale('zh-CN')

    const { wrapper } = await mountAdminUsersView('/admin/users', (params) => ({
      items: [
        {
          id: 9,
          username: 'carol',
          email: 'carol@example.net',
          role: 'user',
          auth_source: 'ldap',
          relay_user_id: 77,
          relay_auth_password: 'encrypted-disabled-password-ciphertext',
          access_status: 'disabled',
          token_valid_after: '2026-06-26T09:00:00Z',
          offboarding_status: 'succeeded',
          created_at: '2026-05-26T00:00:00Z',
          updated_at: '2026-06-26T09:00:00Z',
        },
      ],
      total: 1,
      page: params?.page ?? 1,
      page_size: params?.page_size ?? 20,
    }))

    expect(wrapper.text()).toContain('已禁用')
    expect(wrapper.text()).not.toContain('encrypted-disabled-password-ciphertext')
  })

		  it('renders the department view inside admin users and drills into a department filter', async () => {
	    const { wrapper, router, listAdminUserDepartments, listAdminUsers } = await mountAdminUsersView()

	    await wrapper.get('[data-testid="admin-users-view-departments"]').trigger('click')
	    await flushPromises()

	    expect(listAdminUserDepartments).toHaveBeenCalled()
	    expect(wrapper.text()).toContain('Departments')
	    expect(wrapper.text()).toContain('Department Alpha')
	    expect(wrapper.text()).toContain('1 member')
	    expect(wrapper.text()).toContain('1 matched user')

	    await wrapper.get('[data-testid="admin-users-department-open-dept-alpha"]').trigger('click')
	    await flushPromises()

	    expect(router.currentRoute.value.query.view).toBeUndefined()
	    expect(router.currentRoute.value.query.department_id).toBe('dept-alpha')
		    expect((listAdminUsers as any).mock.calls.at(-1)[0]).toEqual({
		      q: '',
		      department_id: 'dept-alpha',
		      page: 1,
		      page_size: 20,
		    })
		  })

		  it('renders departments as a hierarchy with subtree counts', async () => {
		    const { wrapper } = await mountAdminUsersView()

		    await wrapper.get('[data-testid="admin-users-view-departments"]').trigger('click')
		    await flushPromises()

		    const alpha = wrapper.get('[data-testid="admin-users-department-open-dept-alpha"]')
		    const child = wrapper.get('[data-testid="admin-users-department-open-dept-alpha-team-one"]')
		    const beta = wrapper.get('[data-testid="admin-users-department-open-dept-beta"]')
		    expect(alpha.attributes('aria-level')).toBe('1')
		    expect(child.attributes('aria-level')).toBe('2')
		    expect(beta.attributes('aria-level')).toBe('1')
		    expect(child.attributes('style')).toContain('padding-left: 1.25rem')
		    expect(alpha.text()).toContain('2 total members')
		    expect(alpha.text()).toContain('2 total matched')
		    expect(child.text()).toContain('1 total member')
		    expect(child.text()).toContain('1 / 2 representatives matched')
		    const alphaToggle = wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]')
		    expect(alphaToggle.text()).toBe('-')
		    expect(alphaToggle.classes()).toContain('h-7')
		    expect(alphaToggle.classes()).toContain('w-7')
		    expect(alphaToggle.classes()).toContain('rounded-md')
		  })

		  it('collapses department descendants and hides raw source paths from labels', async () => {
		    const { wrapper } = await mountAdminUsersView()

		    expect(wrapper.html()).toContain('Department Alpha')
		    expect(wrapper.html()).not.toContain('1.781448')

		    await wrapper.get('[data-testid="admin-users-view-departments"]').trigger('click')
		    await flushPromises()

		    expect(wrapper.find('[data-testid="admin-users-department-open-dept-alpha-team-one"]').exists()).toBe(true)
		    expect(wrapper.find('[data-testid="admin-users-department-open-dept-gamma"]').exists()).toBe(true)
		    expect(wrapper.html()).toContain('Department Alpha / Team One')
		    expect(wrapper.html()).toContain('Department Gamma')
		    expect(wrapper.html()).not.toContain('1.781448.1683962')

		    await wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]').trigger('click')
		    await flushPromises()

		    expect(wrapper.find('[data-testid="admin-users-department-open-dept-alpha-team-one"]').exists()).toBe(false)
		    expect(wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]').text()).toBe('+')

		    await wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]').trigger('click')
		    await flushPromises()

		    expect(wrapper.find('[data-testid="admin-users-department-open-dept-alpha-team-one"]').exists()).toBe(true)
		  })

		  it('keeps keyboard activation on the department toggle scoped to expansion', async () => {
		    const { wrapper, router, listAdminUsers } = await mountAdminUsersView()

		    await wrapper.get('[data-testid="admin-users-view-departments"]').trigger('click')
		    await flushPromises()

		    const alphaToggle = wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]')
		    expect(alphaToggle.attributes('aria-label')).toBe('Collapse department')
		    expect(wrapper.find('[data-testid="admin-users-department-open-dept-alpha-team-one"]').exists()).toBe(true)

		    await alphaToggle.trigger('keydown', { key: 'Enter' })
		    await flushPromises()

		    expect(router.currentRoute.value.query.department_id).toBeUndefined()
		    expect((listAdminUsers as any).mock.calls.at(-1)[0]).toEqual({
		      q: '',
		      page: 1,
		      page_size: 20,
		    })
		    expect(wrapper.find('[data-testid="admin-users-department-open-dept-alpha-team-one"]').exists()).toBe(false)
		    expect(wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]').attributes('aria-label')).toBe('Expand department')

		    await wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]').trigger('keydown', { key: ' ' })
		    await flushPromises()

		    expect(router.currentRoute.value.query.department_id).toBeUndefined()
		    expect(wrapper.find('[data-testid="admin-users-department-open-dept-alpha-team-one"]').exists()).toBe(true)
		    expect(wrapper.get('[data-testid="admin-users-department-toggle-dept-alpha"]').attributes('aria-label')).toBe('Collapse department')
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
    await flushPromises()

    expect(revealAdminUserRelayPassword).not.toHaveBeenCalled()
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('encrypted-relay-password-ciphertext')
    expect(wrapper.get('[data-testid="app-toast"]').text()).toContain('Copied encrypted')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
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

  it('requires email confirmation before disabling user access', async () => {
    const { disableAdminUserAccess } = await import('@/api/adminUsers')
    ;(disableAdminUserAccess as any).mockResolvedValue({
      data: {
        data: {
          status: 'disabled',
          relay_user_id: 42,
          relay_disabled_at: '2026-07-01T12:00:00Z',
        },
      },
    })

    const { wrapper } = await mountAdminUsersView()
    await wrapper.get('[data-testid="disable-access-7"]').trigger('click')
    await flushPromises()

    expect(disableAdminUserAccess).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('After disabling, this user will no longer be able to access AI services')

    await wrapper.get('[data-testid="disable-access-confirm-email-7"]').setValue('alice@example.com')
    await wrapper.get('[data-testid="confirm-disable-access-7"]').trigger('click')
    await flushPromises()

    expect(disableAdminUserAccess).toHaveBeenCalledWith(7, { confirm_email: 'alice@example.com' })
    expect(wrapper.text()).toContain('Disabled alice@example.com')
  })

  it('does not offer the disable action for already disabled users', async () => {
    const { wrapper } = await mountAdminUsersView('/admin/users', (params) => ({
      items: [
        {
          id: 7,
          username: 'alice',
          email: 'alice@example.com',
          role: 'user',
          auth_source: 'ldap',
          relay_user_id: 42,
          relay_auth_password: 'encrypted-relay-password-ciphertext',
          access_status: 'disabled',
          token_valid_after: '2026-07-01T12:00:00Z',
          created_at: '2026-05-26T00:00:00Z',
          updated_at: '2026-05-26T01:00:00Z',
        },
      ],
      total: 1,
      page: params?.page ?? 1,
      page_size: params?.page_size ?? 20,
    }))

    expect(wrapper.find('[data-testid="disable-access-7"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Disabled')
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

	  it('passes department filters to current-filter subscription jobs', async () => {
	    const { startAdminUserSubscriptionJob } = await import('@/api/adminUsers')
	    ;(startAdminUserSubscriptionJob as any).mockResolvedValue({
	      data: { data: subscriptionJob({ status: 'completed', phase: 'completed', scope: 'current_filter', total_count: 1, processed_count: 1, success_count: 1 }) },
	    })

	    const { wrapper } = await mountAdminUsersView()
	    await wrapper.get('[data-testid="admin-users-department-filter"]').setValue('dept-alpha')
	    await flushPromises()
	    await wrapper.get('[data-testid="subscription-scope"]').setValue('current_filter')
	    await wrapper.get('[data-testid="subscription-provider"]').setValue('3')
	    await wrapper.get('[data-testid="subscription-group"]').setValue('42')
	    await wrapper.get('[data-testid="manage-subscriptions-submit"]').trigger('click')
	    await flushPromises()

	    expect(startAdminUserSubscriptionJob).toHaveBeenCalledWith({
	      scope: 'current_filter',
	      filters: { q: '', department_id: 'dept-alpha', access_status: '' },
	      operation: 'add',
	      provider_id: 3,
	      group_id: '42',
	      validity_days: 30,
	    })
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

  it('resets subscription quota for all mapped users only after explicit confirmation', async () => {
    const { startAdminUserSubscriptionJob } = await import('@/api/adminUsers')
    ;(startAdminUserSubscriptionJob as any).mockResolvedValue({
      data: { data: subscriptionJob({ status: 'completed', phase: 'completed', scope: 'all_mapped', operation: 'reset_quota', total_count: 120, processed_count: 120, success_count: 120 }) },
    })

    const { wrapper } = await mountAdminUsersView()

    await wrapper.get('[data-testid="subscription-scope"]').setValue('all_mapped')
    await wrapper.get('[data-testid="subscription-operation"]').setValue('reset_quota')
    await wrapper.get('[data-testid="subscription-provider"]').setValue('3')
    await wrapper.get('[data-testid="subscription-group"]').setValue('42')
    expect((wrapper.get('[data-testid="manage-subscriptions-submit"]').element as HTMLButtonElement).disabled).toBe(true)

    await wrapper.get('[data-testid="confirm-reset-subscription-quota"]').setValue(true)
    await wrapper.get('[data-testid="manage-subscriptions-submit"]').trigger('click')
    await flushPromises()

    expect(startAdminUserSubscriptionJob).toHaveBeenCalledWith({
      scope: 'all_mapped',
      operation: 'reset_quota',
      provider_id: 3,
      group_id: '42',
    })
  })

  it('resets subscription quota for one selected user', async () => {
    const { startAdminUserSubscriptionJob } = await import('@/api/adminUsers')
    ;(startAdminUserSubscriptionJob as any).mockResolvedValue({
      data: { data: subscriptionJob({ status: 'completed', phase: 'completed', operation: 'reset_quota', total_count: 1, processed_count: 1, success_count: 1 }) },
    })

    const { wrapper } = await mountAdminUsersView()

    await wrapper.get('[data-testid="select-user-7"]').setValue(true)
    await wrapper.get('[data-testid="subscription-operation"]').setValue('reset_quota')
    await wrapper.get('[data-testid="subscription-provider"]').setValue('3')
    await wrapper.get('[data-testid="subscription-group"]').setValue('42')
    await wrapper.get('[data-testid="confirm-reset-subscription-quota"]').setValue(true)
    await wrapper.get('[data-testid="manage-subscriptions-submit"]').trigger('click')
    await flushPromises()

    expect(startAdminUserSubscriptionJob).toHaveBeenCalledWith({
      scope: 'selected',
      user_ids: [7],
      operation: 'reset_quota',
      provider_id: 3,
      group_id: '42',
    })
  })

  it('requires a fresh reset quota confirmation after the target scope changes', async () => {
    const { wrapper } = await mountAdminUsersView()

    await wrapper.get('[data-testid="select-user-7"]').setValue(true)
    await wrapper.get('[data-testid="subscription-operation"]').setValue('reset_quota')
    await wrapper.get('[data-testid="subscription-provider"]').setValue('3')
    await wrapper.get('[data-testid="subscription-group"]').setValue('42')
    await wrapper.get('[data-testid="confirm-reset-subscription-quota"]').setValue(true)
    expect((wrapper.get('[data-testid="manage-subscriptions-submit"]').element as HTMLButtonElement).disabled).toBe(false)

    await wrapper.get('[data-testid="subscription-scope"]').setValue('all_mapped')
    await flushPromises()

    expect((wrapper.get('[data-testid="confirm-reset-subscription-quota"]').element as HTMLInputElement).checked).toBe(false)
    expect((wrapper.get('[data-testid="manage-subscriptions-submit"]').element as HTMLButtonElement).disabled).toBe(true)
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
