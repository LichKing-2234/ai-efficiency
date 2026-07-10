import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import QuotaResetView from '@/views/QuotaResetView.vue'
import { useAuthStore } from '@/stores/auth'
import { setLocale } from '@/i18n'

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  getMe: vi.fn(),
  devLogin: vi.fn(),
}))

vi.mock('@/api/quotaReset', () => ({
  listMyQuotaResetRequests: vi.fn(),
  listQuotaResetApprovals: vi.fn(),
  listAdminQuotaResetRequests: vi.fn(),
  cancelQuotaResetRequest: vi.fn(),
  approveQuotaResetRequest: vi.fn(),
  rejectQuotaResetRequest: vi.fn(),
  retryQuotaResetRequest: vi.fn(),
  adminApproveQuotaResetRequest: vi.fn(),
  adminRejectQuotaResetRequest: vi.fn(),
  adminRetryQuotaResetRequest: vi.fn(),
}))

const mineRequest = {
  id: 1,
  requester_user_id: 10,
  requester_display_name: 'alice',
  requester_email: 'alice@example.com',
  provider_id: 1,
  group_id: '42',
  group_name: 'Group Alpha',
  group_platform: 'openai',
  reason: 'Need reset for a build investigation',
  status: 'pending',
  resolved_approver_user_ids: [20],
  matched_department_paths: [],
  created_at: '2026-07-07T01:00:00Z',
  updated_at: '2026-07-07T01:00:00Z',
}

const approvalRequest = {
  ...mineRequest,
  id: 2,
  requester_display_name: 'bob',
  requester_email: 'bob@example.org',
  group_name: 'Group Beta',
  reason: 'Need reset for release validation',
}

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/usage/quota-reset', component: QuotaResetView },
      { path: '/usage', component: { template: '<div>Usage</div>' } },
      { path: '/usage/team', component: { template: '<div>Team</div>' } },
    ],
  })
}

async function mountQuotaResetView(role: 'user' | 'admin' = 'user') {
  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore()
  auth.user = { id: role === 'admin' ? 99 : 20, username: role, email: `${role}@example.com`, role, auth_source: 'ldap' }
  const router = createTestRouter()
  await router.push('/usage/quota-reset')
  await router.isReady()
  const wrapper = mount(QuotaResetView, {
    global: { plugins: [pinia, router] },
  })
  await flushPromises()
  return wrapper
}

beforeEach(async () => {
  setLocale('en-US')
  vi.clearAllMocks()
  const api = await import('@/api/quotaReset') as any
  api.listMyQuotaResetRequests.mockResolvedValue({ data: { data: { items: [mineRequest], page: 1, page_size: 20, total: 1 } } })
  api.listQuotaResetApprovals.mockResolvedValue({ data: { data: { items: [approvalRequest], page: 1, page_size: 20, total: 7 } } })
  api.listAdminQuotaResetRequests.mockResolvedValue({ data: { data: { items: [], page: 1, page_size: 20, total: 0 } } })
  api.approveQuotaResetRequest.mockResolvedValue({ data: { data: { ...approvalRequest, status: 'approved_reset_succeeded' } } })
})

describe('QuotaResetView', () => {
  it('loads my requests and approval queue, then approves a pending request', async () => {
    const api = await import('@/api/quotaReset') as any
    const wrapper = await mountQuotaResetView()

    expect(api.listMyQuotaResetRequests).toHaveBeenCalled()
    expect(api.listQuotaResetApprovals).toHaveBeenCalled()
    expect(wrapper.text()).toContain('Group Alpha')
    expect(wrapper.text()).toContain('Need reset for a build investigation')

    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-approve-2"]').trigger('click')
    await flushPromises()

    expect(api.approveQuotaResetRequest).toHaveBeenCalledWith(2, {})
    expect(api.listQuotaResetApprovals).toHaveBeenCalledTimes(2)
  })

  it('loads admin queue for admins', async () => {
    const api = await import('@/api/quotaReset') as any
    api.listAdminQuotaResetRequests.mockResolvedValue({ data: { data: { items: [approvalRequest], page: 1, page_size: 20, total: 1 } } })

    const wrapper = await mountQuotaResetView('admin')

    expect(api.listAdminQuotaResetRequests).toHaveBeenCalled()
    await wrapper.get('[data-testid="quota-reset-tab-admin"]').trigger('click')
    expect(wrapper.text()).toContain('Group Beta')
  })

  it('uses list totals as queue tab badges', async () => {
    const api = await import('@/api/quotaReset') as any
    api.listMyQuotaResetRequests.mockResolvedValue({ data: { data: { items: [mineRequest], page: 1, page_size: 20, total: 4 } } })
    api.listQuotaResetApprovals.mockResolvedValue({ data: { data: { items: [approvalRequest], page: 1, page_size: 20, total: 7 } } })
    api.listAdminQuotaResetRequests.mockResolvedValue({ data: { data: { items: [approvalRequest], page: 1, page_size: 20, total: 12 } } })

    const wrapper = await mountQuotaResetView('admin')

    expect(wrapper.get('[data-testid="quota-reset-tab-mine-count"]').text()).toBe('4')
    expect(wrapper.get('[data-testid="quota-reset-tab-approvals-count"]').text()).toBe('7')
    expect(wrapper.get('[data-testid="quota-reset-tab-admin-count"]').text()).toBe('12')
  })

  it('separates queue switching from lighter status filters', async () => {
    const wrapper = await mountQuotaResetView('admin')

    const queueSelector = wrapper.get('[data-testid="quota-reset-queue-selector"]')
    expect(queueSelector.classes()).toContain('rounded-lg')
    expect(queueSelector.classes()).toContain('bg-slate-100')

    const statusFilters = wrapper.get('[data-testid="quota-reset-status-filters"]')
    expect(statusFilters.classes()).toContain('rounded-full')
    expect(statusFilters.find('[data-testid="quota-reset-filter-all"]').classes()).toContain('text-xs')
  })
})
