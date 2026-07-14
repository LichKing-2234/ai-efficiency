import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import QuotaResetView from '@/views/QuotaResetView.vue'
import { useAuthStore } from '@/stores/auth'
import { useWorkItemsStore } from '@/stores/workItems'
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

vi.mock('@/api/workItems', () => ({
  getWorkItemCounts: vi.fn(),
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

const failedApprovalRequest = {
  ...approvalRequest,
  status: 'approved_reset_failed',
  reset_error: 'Synthetic reset failure',
}

function countsResponse(approvalCount: number, adminCount = 0) {
  return {
    data: {
      data: {
        quota_reset_approval_count: approvalCount,
        quota_reset_admin_count: adminCount,
        ai_access_setup_count: 0,
        offboarding_count: 0,
        total_count: Math.max(approvalCount, adminCount),
      },
    },
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => {
    resolve = res
  })
  return { promise, resolve }
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
  const workItemsApi = await import('@/api/workItems') as any
  api.listMyQuotaResetRequests.mockResolvedValue({ data: { data: { items: [mineRequest], page: 1, page_size: 20, total: 1 } } })
  api.listQuotaResetApprovals.mockResolvedValue({ data: { data: { items: [approvalRequest], page: 1, page_size: 20, total: 7 } } })
  api.listAdminQuotaResetRequests.mockResolvedValue({ data: { data: { items: [], page: 1, page_size: 20, total: 0 } } })
  api.cancelQuotaResetRequest.mockResolvedValue({ data: { data: { ...mineRequest, status: 'cancelled' } } })
  api.approveQuotaResetRequest.mockResolvedValue({ data: { data: { ...approvalRequest, status: 'approved_reset_succeeded' } } })
  api.rejectQuotaResetRequest.mockResolvedValue({ data: { data: { ...approvalRequest, status: 'rejected' } } })
  api.retryQuotaResetRequest.mockResolvedValue({ data: { data: { ...failedApprovalRequest, status: 'approved_reset_succeeded' } } })
  api.adminApproveQuotaResetRequest.mockResolvedValue({ data: { data: { ...approvalRequest, status: 'approved_reset_succeeded' } } })
  api.adminRejectQuotaResetRequest.mockResolvedValue({ data: { data: { ...approvalRequest, status: 'rejected' } } })
  api.adminRetryQuotaResetRequest.mockResolvedValue({ data: { data: { ...failedApprovalRequest, status: 'approved_reset_succeeded' } } })
  workItemsApi.getWorkItemCounts.mockResolvedValue(countsResponse(2, 3))
})

describe('QuotaResetView', () => {
  it('loads my requests and approval queue, then approves a pending request', async () => {
    const api = await import('@/api/quotaReset') as any
    const workItemsApi = await import('@/api/workItems') as any
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
    expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(2)
  })

  it('invalidates an in-flight count request and keeps the action pending for the fresh generation', async () => {
    const api = await import('@/api/quotaReset') as any
    const workItemsApi = await import('@/api/workItems') as any
    const initialCounts = deferred<any>()
    const freshCounts = deferred<any>()
    workItemsApi.getWorkItemCounts
      .mockReturnValueOnce(initialCounts.promise)
      .mockReturnValueOnce(freshCounts.promise)
    const wrapper = await mountQuotaResetView()
    const workItems = useWorkItemsStore()
    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-approve-2"]').trigger('click')
    await flushPromises()

    expect(api.approveQuotaResetRequest).toHaveBeenCalledWith(2, {})
    expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('Loading...')

    initialCounts.resolve(countsResponse(9))
    await flushPromises()

    expect(workItems.totalCount).toBe(0)
    expect(workItems.loading).toBe(true)
    expect(wrapper.text()).toContain('Loading...')

    freshCounts.resolve(countsResponse(0))
    await flushPromises()

    expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(2)
    expect(workItems.totalCount).toBe(0)
    expect(workItems.loading).toBe(false)
    expect(wrapper.text()).not.toContain('Loading...')
    expect(wrapper.find('[data-testid="quota-reset-tab-approvals-count"]').exists()).toBe(false)
  })

  it.each([
    {
      name: 'cancel',
      role: 'user' as const,
      queue: 'mine',
      selector: '[data-testid="quota-reset-cancel-1"]',
      apiName: 'cancelQuotaResetRequest',
      expectedArgs: [1],
      status: 'pending',
    },
    {
      name: 'reject',
      role: 'user' as const,
      queue: 'approvals',
      selector: '[data-testid="quota-reset-reject-2"]',
      apiName: 'rejectQuotaResetRequest',
      expectedArgs: [2, { decision_reason: 'Synthetic decision' }],
      status: 'pending',
    },
    {
      name: 'retry',
      role: 'user' as const,
      queue: 'approvals',
      selector: '[data-testid="quota-reset-retry-2"]',
      apiName: 'retryQuotaResetRequest',
      expectedArgs: [2],
      status: 'approved_reset_failed',
    },
    {
      name: 'admin approve',
      role: 'admin' as const,
      queue: 'admin',
      selector: '[data-testid="quota-reset-approve-2"]',
      apiName: 'adminApproveQuotaResetRequest',
      expectedArgs: [2, {}],
      status: 'pending',
    },
    {
      name: 'admin reject',
      role: 'admin' as const,
      queue: 'admin',
      selector: '[data-testid="quota-reset-reject-2"]',
      apiName: 'adminRejectQuotaResetRequest',
      expectedArgs: [2, { decision_reason: 'Synthetic decision' }],
      status: 'pending',
    },
    {
      name: 'admin retry',
      role: 'admin' as const,
      queue: 'admin',
      selector: '[data-testid="quota-reset-retry-2"]',
      apiName: 'adminRetryQuotaResetRequest',
      expectedArgs: [2],
      status: 'approved_reset_failed',
    },
  ])('refreshes queues and counts exactly once after $name succeeds', async ({ role, queue, selector, apiName, expectedArgs, status }) => {
    const api = await import('@/api/quotaReset') as any
    const workItemsApi = await import('@/api/workItems') as any
    const queueItem = { ...approvalRequest, status }
    api.listQuotaResetApprovals.mockResolvedValue({ data: { data: { items: [queueItem], page: 1, page_size: 20, total: 1 } } })
    if (role === 'admin') {
      api.listAdminQuotaResetRequests.mockResolvedValue({ data: { data: { items: [queueItem], page: 1, page_size: 20, total: 1 } } })
    }
    const prompt = vi.spyOn(window, 'prompt').mockReturnValue('Synthetic decision')
    const wrapper = await mountQuotaResetView(role)

    try {
      if (queue === 'approvals') {
        await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
      } else if (queue === 'admin') {
        await wrapper.get('[data-testid="quota-reset-tab-admin"]').trigger('click')
      }
      await wrapper.get(selector).trigger('click')
      await flushPromises()

      expect(api[apiName]).toHaveBeenCalledWith(...expectedArgs)
      expect(api.listMyQuotaResetRequests).toHaveBeenCalledTimes(2)
      expect(api.listQuotaResetApprovals).toHaveBeenCalledTimes(2)
      expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(2)
    } finally {
      prompt.mockRestore()
    }
  })

  it('loads admin queue for admins', async () => {
    const api = await import('@/api/quotaReset') as any
    api.listAdminQuotaResetRequests.mockResolvedValue({ data: { data: { items: [approvalRequest], page: 1, page_size: 20, total: 1 } } })

    const wrapper = await mountQuotaResetView('admin')

    expect(api.listAdminQuotaResetRequests).toHaveBeenCalled()
    await wrapper.get('[data-testid="quota-reset-tab-admin"]').trigger('click')
    expect(wrapper.text()).toContain('Group Beta')
  })

  it('uses historical totals for my requests and actionable counts for approval queues', async () => {
    const api = await import('@/api/quotaReset') as any
    api.listMyQuotaResetRequests.mockResolvedValue({ data: { data: { items: [mineRequest], page: 1, page_size: 20, total: 4 } } })
    api.listQuotaResetApprovals.mockResolvedValue({ data: { data: { items: [approvalRequest], page: 1, page_size: 20, total: 7 } } })
    api.listAdminQuotaResetRequests.mockResolvedValue({ data: { data: { items: [approvalRequest], page: 1, page_size: 20, total: 12 } } })

    const wrapper = await mountQuotaResetView('admin')

    expect(wrapper.get('[data-testid="quota-reset-tab-mine-count"]').text()).toBe('4')
    expect(wrapper.get('[data-testid="quota-reset-tab-approvals-count"]').text()).toBe('2')
    expect(wrapper.get('[data-testid="quota-reset-tab-admin-count"]').text()).toBe('3')
  })

  it('does not show approval badges for completed history when actionable counts are zero', async () => {
    const api = await import('@/api/quotaReset') as any
    const workItemsApi = await import('@/api/workItems') as any
    const succeededRequest = {
      ...mineRequest,
      requester_user_id: 99,
      requester_display_name: 'admin',
      requester_email: 'admin@example.com',
      status: 'approved_reset_succeeded',
    }
    api.listMyQuotaResetRequests.mockResolvedValue({ data: { data: { items: [succeededRequest], page: 1, page_size: 20, total: 1 } } })
    api.listQuotaResetApprovals.mockResolvedValue({ data: { data: { items: [succeededRequest], page: 1, page_size: 20, total: 1 } } })
    api.listAdminQuotaResetRequests.mockResolvedValue({ data: { data: { items: [succeededRequest], page: 1, page_size: 20, total: 1 } } })
    workItemsApi.getWorkItemCounts.mockResolvedValue({
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

    const wrapper = await mountQuotaResetView('admin')

    expect(wrapper.get('[data-testid="quota-reset-tab-mine-count"]').text()).toBe('1')
    expect(wrapper.find('[data-testid="quota-reset-tab-approvals-count"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quota-reset-tab-admin-count"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Group Alpha')

    await wrapper.get('[data-testid="quota-reset-tab-admin"]').trigger('click')
    expect(wrapper.text()).toContain('Group Alpha')
  })

  it('renders queue history without waiting for actionable counts', async () => {
    const workItemsApi = await import('@/api/workItems') as any
    const pendingCounts = deferred<any>()
    workItemsApi.getWorkItemCounts.mockReturnValue(pendingCounts.promise)

    const wrapper = await mountQuotaResetView('admin')

    expect(wrapper.text()).toContain('Group Alpha')

    pendingCounts.resolve({
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
    await flushPromises()
  })

  it('keeps queue history available when actionable counts fail to load', async () => {
    const workItemsApi = await import('@/api/workItems') as any
    workItemsApi.getWorkItemCounts.mockRejectedValue(new Error('counts unavailable'))

    const wrapper = await mountQuotaResetView('admin')

    expect(wrapper.text()).toContain('Group Alpha')
    expect(wrapper.find('[data-testid="quota-reset-tab-approvals-count"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quota-reset-tab-admin-count"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Failed to load quota reset requests')
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
