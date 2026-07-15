import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import QuotaResetView from '@/views/QuotaResetView.vue'
import { resetToastsForTest, useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'
import { setLocale } from '@/i18n'
import type { QuotaResetRequestSummary, QuotaResetWorkflowNode } from '@/types'

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

const mineRequest: QuotaResetRequestSummary = {
  id: 1,
  requester_user_id: 10,
  requester_display_name: 'Alice Example',
  requester_email: 'alice@example.com',
  requester_department_paths: ['Engineering / Developer Experience'],
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

const approvalRequest: QuotaResetRequestSummary = {
  ...mineRequest,
  id: 2,
  requester_display_name: 'Bob Example',
  requester_email: 'bob@example.org',
  requester_department_paths: ['Engineering / Runtime'],
  group_name: 'Group Beta',
  reason: 'Need reset for release validation',
}

const workflowNodes: QuotaResetWorkflowNode[] = [
  {
    id: 451,
    position: 0,
    node_type: 'requester_departments',
    label: 'Requester teams',
    departments: [
      { external_id: 'dept-platform', display_path: 'Engineering / Platform', resolution: 'configured' },
      { external_id: 'dept-release', display_path: 'Quality / Release', resolution: 'configured' },
    ],
    status: 'approved',
    admin_fallback_required: false,
    approvers: [
      { user_id: 20, display_name: 'Alex Approver', email: 'alex@example.com', source: 'configured' },
      { user_id: 21, display_name: 'Avery Reviewer', email: 'avery@example.org', source: 'configured' },
    ],
  },
  {
    id: 452,
    position: 1,
    node_type: 'configured_department',
    label: 'Security review',
    departments: [
      { external_id: 'dept-security', display_path: 'Security / Product', resolution: 'configured' },
    ],
    status: 'satisfied_by_prior_approval',
    admin_fallback_required: false,
    approvers: [
      { user_id: 20, display_name: 'Alex Approver', email: 'alex@example.com', source: 'configured' },
    ],
    satisfied_by_decision_id: 901,
  },
  {
    id: 456,
    position: 2,
    node_type: 'configured_department',
    label: 'Release approval',
    departments: [
      { external_id: 'dept-operations', display_path: 'Operations / Release', resolution: 'directory_representative' },
    ],
    status: 'active',
    admin_fallback_required: true,
    approvers: [
      { user_id: 22, display_name: 'Casey Reviewer', email: 'casey@example.com', source: 'directory_representative' },
    ],
  },
  {
    id: 457,
    position: 3,
    node_type: 'configured_department',
    label: 'Operations follow-up',
    departments: [
      { external_id: 'dept-operations', display_path: 'Operations / Follow-up', resolution: 'configured' },
    ],
    status: 'queued',
    admin_fallback_required: false,
    approvers: [
      { user_id: 20, display_name: 'Alex Approver', email: 'alex@example.com', source: 'configured' },
    ],
  },
]

const workflowRequest: QuotaResetRequestSummary = {
  ...approvalRequest,
  id: 3,
  requester_display_name: 'Bob Builder',
  requester_email: 'bob.builder@example.org',
  requester_department_paths: ['Engineering / Platform', 'Quality / Release'],
  workflow: {
    version: 2,
    current_node: workflowNodes[2],
    nodes: workflowNodes,
    decisions: [
      {
        id: 901,
        node_id: 451,
        actor_user_id: 20,
        actor_display_name: 'Alex Approver',
        decision: 'approve',
        comment: 'Approved after reviewing the release evidence.',
        admin_override: true,
        created_at: '2026-07-14T02:00:00Z',
      },
    ],
    can_approve: true,
    can_reject: true,
    can_cancel: false,
    can_retry: false,
  },
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

async function mountQuotaResetView(role: 'user' | 'admin' = 'user', attachTo?: HTMLElement) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore()
  auth.user = { id: role === 'admin' ? 99 : 20, username: role, email: `${role}@example.com`, role, auth_source: 'ldap' }
  const router = createTestRouter()
  await router.push('/usage/quota-reset')
  await router.isReady()
  const wrapper = mount(QuotaResetView, {
    ...(attachTo ? { attachTo } : {}),
    global: { plugins: [pinia, router] },
  })
  await flushPromises()
  return wrapper
}

beforeEach(async () => {
  setLocale('en-US')
  resetToastsForTest()
  vi.clearAllMocks()
  const api = await import('@/api/quotaReset') as any
  const workItemsApi = await import('@/api/workItems') as any
  api.listMyQuotaResetRequests.mockResolvedValue({ data: { data: { items: [mineRequest], page: 1, page_size: 20, total: 1 } } })
  api.listQuotaResetApprovals.mockResolvedValue({ data: { data: { items: [approvalRequest], page: 1, page_size: 20, total: 7 } } })
  api.listAdminQuotaResetRequests.mockResolvedValue({ data: { data: { items: [], page: 1, page_size: 20, total: 0 } } })
  api.approveQuotaResetRequest.mockResolvedValue({ data: { data: { ...approvalRequest, status: 'approved_reset_succeeded' } } })
  workItemsApi.getWorkItemCounts.mockResolvedValue({
    data: {
      data: {
        quota_reset_approval_count: 2,
        quota_reset_admin_count: 3,
        ai_access_setup_count: 0,
        offboarding_count: 0,
        total_count: 3,
      },
    },
  })
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

  it('forces a fresh actionable-count request when an action finishes during an in-flight load', async () => {
    const api = await import('@/api/quotaReset') as any
    const workItemsApi = await import('@/api/workItems') as any
    const initialCounts = deferred<any>()
    workItemsApi.getWorkItemCounts
      .mockReturnValueOnce(initialCounts.promise)
      .mockResolvedValueOnce({
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
    const wrapper = await mountQuotaResetView()
    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-approve-2"]').trigger('click')

    initialCounts.resolve({
      data: {
        data: {
          quota_reset_approval_count: 1,
          quota_reset_admin_count: 0,
          ai_access_setup_count: 0,
          offboarding_count: 0,
          total_count: 1,
        },
      },
    })
    await flushPromises()

    expect(api.approveQuotaResetRequest).toHaveBeenCalledWith(2, {})
    expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-testid="quota-reset-tab-approvals-count"]').exists()).toBe(false)
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

  it('loads v2 decision history only for the processed approval view and keeps pending history visible', async () => {
    const api = await import('@/api/quotaReset') as any
    const priorDecisionOnPendingRequest: QuotaResetRequestSummary = {
      ...workflowRequest,
      id: 44,
      group_name: 'Group History',
      status: 'pending',
      workflow: {
        ...workflowRequest.workflow!,
        can_approve: false,
        can_reject: false,
      },
    }
    api.listQuotaResetApprovals
      .mockResolvedValueOnce({ data: { data: { items: [approvalRequest], page: 1, page_size: 20, total: 1 } } })
      .mockResolvedValueOnce({ data: { data: { items: [priorDecisionOnPendingRequest], page: 1, page_size: 20, total: 1 } } })

    const wrapper = await mountQuotaResetView()
    expect(api.listQuotaResetApprovals).toHaveBeenCalledTimes(1)
    expect(api.listQuotaResetApprovals).toHaveBeenLastCalledWith()

    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-filter-processed"]').trigger('click')
    await flushPromises()

    expect(api.listQuotaResetApprovals).toHaveBeenLastCalledWith({ scope: 'history' })
    expect(wrapper.text()).toContain('Group History')
    expect(wrapper.find('[data-testid="quota-reset-approve-44"]').exists()).toBe(false)
  })

  it('deduplicates repeated processed history loads and keeps loading scoped to the displayed dataset', async () => {
    const api = await import('@/api/quotaReset') as any
    const pendingHistory = deferred<any>()
    const historyRequest = {
      ...workflowRequest,
      id: 45,
      group_name: 'Group History Pending',
      status: 'pending' as const,
      workflow: {
        ...workflowRequest.workflow!,
        can_approve: false,
        can_reject: false,
      },
    }
    api.listQuotaResetApprovals
      .mockResolvedValueOnce({ data: { data: { items: [approvalRequest], page: 1, page_size: 20, total: 1 } } })
      .mockReturnValueOnce(pendingHistory.promise)
      .mockResolvedValueOnce({ data: { data: { items: [], page: 1, page_size: 20, total: 0 } } })

    const wrapper = await mountQuotaResetView()
    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    const processed = wrapper.get('[data-testid="quota-reset-filter-processed"]')
    await processed.trigger('click')
    await processed.trigger('click')
    await flushPromises()

    expect.soft(api.listQuotaResetApprovals).toHaveBeenCalledTimes(2)
    expect.soft(wrapper.text()).toContain('Loading...')

    await wrapper.get('[data-testid="quota-reset-filter-all"]').trigger('click')
    expect(wrapper.text()).not.toContain('Loading...')
    await processed.trigger('click')
    expect.soft(wrapper.text()).toContain('Loading...')

    pendingHistory.resolve({ data: { data: { items: [historyRequest], page: 1, page_size: 20, total: 1 } } })
    await flushPromises()

    expect(api.listQuotaResetApprovals).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('Group History Pending')
    expect(wrapper.text()).not.toContain('Loading...')
  })

  it('refreshes core and admin queues after an action without refetching loaded history', async () => {
    const api = await import('@/api/quotaReset') as any
    let historyCalls = 0
    api.listQuotaResetApprovals.mockImplementation((params?: { scope?: string }) => {
      if (params?.scope === 'history') {
        historyCalls += 1
        if (historyCalls > 1) return Promise.reject(new Error('history unavailable'))
        return Promise.resolve({ data: { data: { items: [workflowRequest], page: 1, page_size: 20, total: 1 } } })
      }
      return Promise.resolve({ data: { data: { items: [approvalRequest], page: 1, page_size: 20, total: 1 } } })
    })
    api.listAdminQuotaResetRequests.mockResolvedValue({
      data: { data: { items: [approvalRequest], page: 1, page_size: 20, total: 1 } },
    })
    api.adminApproveQuotaResetRequest.mockResolvedValue({
      data: { data: { ...approvalRequest, status: 'approved_reset_succeeded' } },
    })

    const wrapper = await mountQuotaResetView('admin')
    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-filter-processed"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Bob Builder')

    await wrapper.get('[data-testid="quota-reset-filter-all"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-tab-admin"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-approve-2"]').trigger('click')
    await flushPromises()

    expect(api.adminApproveQuotaResetRequest).toHaveBeenCalledWith(2, {})
    expect(api.listMyQuotaResetRequests).toHaveBeenCalledTimes(2)
    expect(api.listAdminQuotaResetRequests).toHaveBeenCalledTimes(2)
    expect(historyCalls).toBe(1)
    expect(useToast().toast.tone).toBe('success')
  })

  it('ignores stale history after an action invalidates an in-flight request', async () => {
    const api = await import('@/api/quotaReset') as any
    const staleHistory = deferred<any>()
    const freshHistory = deferred<any>()
    let historyCalls = 0
    api.listQuotaResetApprovals.mockImplementation((params?: { scope?: string }) => {
      if (params?.scope !== 'history') {
        return Promise.resolve({ data: { data: { items: [approvalRequest], page: 1, page_size: 20, total: 1 } } })
      }
      historyCalls += 1
      return historyCalls === 1 ? staleHistory.promise : freshHistory.promise
    })

    const wrapper = await mountQuotaResetView()
    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-filter-processed"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-filter-all"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-approve-2"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="quota-reset-filter-processed"]').trigger('click')
    const freshRequest = { ...workflowRequest, id: 46, group_name: 'Fresh History' }
    freshHistory.resolve({ data: { data: { items: [freshRequest], page: 1, page_size: 20, total: 1 } } })
    await flushPromises()
    expect(wrapper.text()).toContain('Fresh History')

    const staleRequest = { ...workflowRequest, id: 47, group_name: 'Stale History' }
    staleHistory.resolve({ data: { data: { items: [staleRequest], page: 1, page_size: 20, total: 1 } } })
    await flushPromises()

    expect(historyCalls).toBe(2)
    expect(wrapper.text()).toContain('Fresh History')
    expect(wrapper.text()).not.toContain('Stale History')
  })

  it('reloads displayed processed history independently after retry succeeds', async () => {
    const api = await import('@/api/quotaReset') as any
    const freshHistory = deferred<any>()
    const failedHistoryRequest: QuotaResetRequestSummary = {
      ...workflowRequest,
      id: 48,
      group_name: 'Failed History',
      status: 'approved_reset_failed',
      workflow: {
        ...workflowRequest.workflow!,
        current_node: null,
        can_approve: false,
        can_reject: false,
        can_cancel: false,
        can_retry: true,
      },
    }
    let historyCalls = 0
    api.listQuotaResetApprovals.mockImplementation((params?: { scope?: string }) => {
      if (params?.scope !== 'history') {
        return Promise.resolve({ data: { data: { items: [approvalRequest], page: 1, page_size: 20, total: 1 } } })
      }
      historyCalls += 1
      if (historyCalls === 1) {
        return Promise.resolve({ data: { data: { items: [failedHistoryRequest], page: 1, page_size: 20, total: 1 } } })
      }
      return freshHistory.promise
    })
    api.retryQuotaResetRequest.mockResolvedValue({
      data: { data: { ...failedHistoryRequest, status: 'approved_reset_succeeded' } },
    })

    const wrapper = await mountQuotaResetView()
    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-filter-processed"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="quota-reset-retry-48"]').trigger('click')
    await flushPromises()

    expect(api.retryQuotaResetRequest).toHaveBeenCalledWith(48)
    expect.soft(historyCalls).toBe(2)
    expect.soft(wrapper.text()).toContain('Loading...')

    const refreshedHistory = {
      ...failedHistoryRequest,
      group_name: 'Refreshed History',
      status: 'approved_reset_succeeded' as const,
      workflow: { ...failedHistoryRequest.workflow!, can_retry: false },
    }
    freshHistory.resolve({ data: { data: { items: [refreshedHistory], page: 1, page_size: 20, total: 1 } } })
    await flushPromises()

    expect(historyCalls).toBe(2)
    expect(wrapper.text()).toContain('Refreshed History')
    expect(wrapper.text()).not.toContain('Loading...')
    expect(wrapper.find('[data-testid="quota-reset-retry-48"]').exists()).toBe(false)
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

  it('opens request details from a semantic trigger and manages dialog focus', async () => {
    const api = await import('@/api/quotaReset') as any
    api.listMyQuotaResetRequests.mockResolvedValue({
      data: { data: { items: [workflowRequest], page: 1, page_size: 20, total: 1 } },
    })
    const wrapper = await mountQuotaResetView('user', document.body)

    try {
      const row = wrapper.get('[data-testid="quota-reset-row-3"]')
      expect(row.element.tagName).toBe('ARTICLE')
      expect(row.attributes('role')).toBeUndefined()

      const trigger = wrapper.get('[data-testid="quota-reset-view-details-3"]')
      expect(trigger.element.tagName).toBe('BUTTON')
      expect(trigger.attributes('type')).toBe('button')
      expect(trigger.attributes('aria-label')).toBe('View details for Group Beta')
      expect(trigger.attributes('title')).toBe('View details for Group Beta')

      ;(trigger.element as HTMLButtonElement).focus()
      expect(document.activeElement).toBe(trigger.element)
      await trigger.trigger('click')
      await flushPromises()

      const dialog = wrapper.get('[data-testid="quota-reset-detail-dialog"]')
      const closeButton = dialog.get('[data-testid="quota-reset-detail-close"]')
      expect(document.activeElement).toBe(closeButton.element)

      const tabEvent = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })
      closeButton.element.dispatchEvent(tabEvent)
      expect(tabEvent.defaultPrevented).toBe(true)
      expect(document.activeElement).toBe(closeButton.element)

      dialog.element.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      await flushPromises()
      expect(wrapper.find('[data-testid="quota-reset-detail-dialog"]').exists()).toBe(false)
      expect(document.activeElement).toBe(trigger.element)
    } finally {
      wrapper.unmount()
    }
  })

  it('restores focus to a stable queue control when refresh removes the detail trigger', async () => {
    const api = await import('@/api/quotaReset') as any
    api.listMyQuotaResetRequests
      .mockResolvedValueOnce({ data: { data: { items: [mineRequest], page: 1, page_size: 20, total: 1 } } })
      .mockResolvedValueOnce({ data: { data: { items: [], page: 1, page_size: 20, total: 0 } } })
    api.cancelQuotaResetRequest.mockResolvedValue({ data: { data: { ...mineRequest, status: 'cancelled' } } })
    const wrapper = await mountQuotaResetView('user', document.body)

    try {
      const trigger = wrapper.get('[data-testid="quota-reset-view-details-1"]')
      ;(trigger.element as HTMLButtonElement).focus()
      await trigger.trigger('click')
      await flushPromises()
      expect(wrapper.find('[data-testid="quota-reset-detail-dialog"]').exists()).toBe(true)

      await wrapper.get('[data-testid="quota-reset-cancel-1"]').trigger('click')
      await flushPromises()

      expect(wrapper.find('[data-testid="quota-reset-detail-dialog"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="quota-reset-view-details-1"]').exists()).toBe(false)
      expect(document.activeElement).toBe(wrapper.get('[data-testid="quota-reset-tab-mine"]').element)
    } finally {
      wrapper.unmount()
    }
  })

  it('renders ordered node status and prior-approval attribution', async () => {
    const api = await import('@/api/quotaReset') as any
    api.listMyQuotaResetRequests.mockResolvedValue({
      data: { data: { items: [workflowRequest], page: 1, page_size: 20, total: 1 } },
    })

    const wrapper = await mountQuotaResetView()
    await wrapper.get('[data-testid="quota-reset-view-details-3"]').trigger('click')

    const timeline = wrapper.get('[data-testid="quota-reset-workflow-timeline"]')
    const nodes = timeline.findAll('li')
    expect(nodes).toHaveLength(4)
    expect(nodes[0].text()).toContain('Requester teams')
    expect(nodes[1].text()).toContain('Security review')
    expect(nodes[2].text()).toContain('Release approval')
    expect(nodes[3].text()).toContain('Operations follow-up')
    expect(nodes[0].text()).toContain('Approved')
    expect(nodes[0].text()).toContain('Alex Approver')
    expect(nodes[0].text()).toContain('Avery Reviewer')
    expect(nodes[0].text()).toContain('Approved after reviewing the release evidence.')
    expect(nodes[0].text()).toContain('Admin override')
    expect(nodes[1].text()).toContain('Satisfied by prior approval')
    expect(nodes[1].text()).toContain('Alex Approver')
    expect(nodes[1].text()).toContain('Approved after reviewing the release evidence.')
    expect(nodes[2].text()).toContain('Active')
    expect(nodes[2].text()).toContain('Admin fallback')
    expect(nodes[3].text()).toContain('Queued')
  })

  it('uses backend can_approve instead of queue mode', async () => {
    const api = await import('@/api/quotaReset') as any
    const approveOnly = {
      ...workflowRequest,
      id: 31,
      workflow: {
        ...workflowRequest.workflow!,
        can_approve: true,
        can_reject: false,
        can_cancel: true,
        can_retry: true,
      },
    }
    const rejectOnly = {
      ...workflowRequest,
      id: 32,
      workflow: {
        ...workflowRequest.workflow!,
        can_approve: false,
        can_reject: true,
        can_cancel: false,
        can_retry: false,
      },
    }
    api.listQuotaResetApprovals.mockResolvedValue({
      data: { data: { items: [approveOnly, rejectOnly], page: 1, page_size: 20, total: 2 } },
    })

    const wrapper = await mountQuotaResetView()
    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')

    expect(wrapper.find('[data-testid="quota-reset-approve-31"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="quota-reset-reject-31"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quota-reset-cancel-31"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="quota-reset-retry-31"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="quota-reset-approve-32"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quota-reset-reject-32"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="quota-reset-cancel-32"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quota-reset-retry-32"]').exists()).toBe(false)
  })

  it('requires a comment for approve and reject', async () => {
    const api = await import('@/api/quotaReset') as any
    api.listQuotaResetApprovals.mockResolvedValue({
      data: { data: { items: [workflowRequest], page: 1, page_size: 20, total: 1 } },
    })
    const wrapper = await mountQuotaResetView()
    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')

    await wrapper.get('[data-testid="quota-reset-approve-3"]').trigger('click')
    let dialog = wrapper.get('[data-testid="quota-reset-decision-dialog"]')
    await dialog.get('[data-testid="quota-reset-decision-submit"]').trigger('click')
    expect(dialog.text()).toContain('Comment is required')
    expect(api.approveQuotaResetRequest).not.toHaveBeenCalled()

    await dialog.get('textarea').setValue('This must not survive reopening.')
    await dialog.get('[data-testid="quota-reset-decision-cancel"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-reject-3"]').trigger('click')
    dialog = wrapper.get('[data-testid="quota-reset-decision-dialog"]')
    expect((dialog.get('textarea').element as HTMLTextAreaElement).value).toBe('')
    expect(dialog.text()).not.toContain('Comment is required')
    await dialog.get('[data-testid="quota-reset-decision-submit"]').trigger('click')
    expect(dialog.text()).toContain('Comment is required')
    expect(api.rejectQuotaResetRequest).not.toHaveBeenCalled()
  })

  it('submits the current request_node_id', async () => {
    const api = await import('@/api/quotaReset') as any
    const pendingApproval = deferred<any>()
    api.listQuotaResetApprovals.mockResolvedValue({
      data: { data: { items: [workflowRequest], page: 1, page_size: 20, total: 1 } },
    })
    api.approveQuotaResetRequest.mockReturnValue(pendingApproval.promise)
    const wrapper = await mountQuotaResetView()
    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-approve-3"]').trigger('click')

    const dialog = wrapper.get('[data-testid="quota-reset-decision-dialog"]')
    await dialog.get('textarea').setValue('  Approved for the release investigation.  ')
    await dialog.get('[data-testid="quota-reset-decision-submit"]').trigger('click')
    await dialog.get('[data-testid="quota-reset-decision-submit"]').trigger('click')

    expect(api.approveQuotaResetRequest).toHaveBeenCalledTimes(1)
    expect(api.approveQuotaResetRequest).toHaveBeenCalledWith(3, {
      request_node_id: 456,
      decision_reason: 'Approved for the release investigation.',
    })

    pendingApproval.resolve({ data: { data: workflowRequest } })
    await flushPromises()
  })

  it('refreshes from workflow_advanced details', async () => {
    const api = await import('@/api/quotaReset') as any
    const workItemsApi = await import('@/api/workItems') as any
    const unrelatedRequest = { ...workflowRequest, id: 4, reason: 'Keep this newer row unchanged' }
    const advancedRequest = {
      ...workflowRequest,
      reason: 'Authoritative workflow state',
      workflow: {
        ...workflowRequest.workflow!,
        current_node: workflowNodes[3],
        can_approve: false,
        can_reject: false,
      },
    }
    api.listQuotaResetApprovals.mockResolvedValue({
      data: { data: { items: [workflowRequest, unrelatedRequest], page: 1, page_size: 20, total: 2 } },
    })
    api.approveQuotaResetRequest.mockRejectedValue({
      response: {
        status: 409,
        data: {
          message: 'workflow_advanced',
          details: { request: advancedRequest },
        },
      },
    })
    const wrapper = await mountQuotaResetView()
    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-approve-3"]').trigger('click')
    const dialog = wrapper.get('[data-testid="quota-reset-decision-dialog"]')
    await dialog.get('textarea').setValue('Approved from stale state.')
    await dialog.get('[data-testid="quota-reset-decision-submit"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="quota-reset-decision-dialog"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="quota-reset-row-3"]').text()).toContain('Authoritative workflow state')
    expect(wrapper.get('[data-testid="quota-reset-row-4"]').text()).toContain('Keep this newer row unchanged')
    expect(useToast().toast.message).toContain('The workflow advanced while this request was open')
    expect(useToast().toast.tone).toBe('info')
    expect(api.listQuotaResetApprovals).toHaveBeenCalledTimes(1)
    expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(2)
  })

  it('does not render queued future work as actionable', async () => {
    const api = await import('@/api/quotaReset') as any
    const queuedAssignment = {
      ...workflowRequest,
      id: 33,
      workflow: {
        ...workflowRequest.workflow!,
        can_approve: false,
        can_reject: false,
        can_cancel: false,
        can_retry: false,
      },
    }
    api.listQuotaResetApprovals.mockResolvedValue({
      data: { data: { items: [queuedAssignment], page: 1, page_size: 20, total: 1 } },
    })

    const wrapper = await mountQuotaResetView()
    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    expect(wrapper.find('[data-testid="quota-reset-approve-33"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quota-reset-reject-33"]').exists()).toBe(false)

    await wrapper.get('[data-testid="quota-reset-view-details-33"]').trigger('click')
    const timeline = wrapper.get('[data-testid="quota-reset-workflow-timeline"]')
    expect(timeline.text()).toContain('Operations follow-up')
    expect(timeline.findAll('button')).toHaveLength(0)
  })

  it('shows requester display name and every direct team path', async () => {
    const api = await import('@/api/quotaReset') as any
    api.listMyQuotaResetRequests.mockResolvedValue({
      data: { data: { items: [workflowRequest], page: 1, page_size: 20, total: 1 } },
    })

    const wrapper = await mountQuotaResetView()
    await wrapper.get('[data-testid="quota-reset-view-details-3"]').trigger('click')
    const detail = wrapper.get('[data-testid="quota-reset-detail-dialog"]')
    expect(detail.text()).toContain('Bob Builder')
    expect(detail.text()).toContain('bob.builder@example.org')
    expect(detail.text()).toContain('Engineering / Platform')
    expect(detail.text()).toContain('Quality / Release')
  })

  it('keeps legacy v1 approve and reject actions usable without request_node_id', async () => {
    const api = await import('@/api/quotaReset') as any
    const wrapper = await mountQuotaResetView()
    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')

    await wrapper.get('[data-testid="quota-reset-approve-2"]').trigger('click')
    await flushPromises()
    expect(api.approveQuotaResetRequest).toHaveBeenCalledWith(2, {})
    expect(wrapper.find('[data-testid="quota-reset-decision-dialog"]').exists()).toBe(false)

    await wrapper.get('[data-testid="quota-reset-reject-2"]').trigger('click')
    const dialog = wrapper.get('[data-testid="quota-reset-decision-dialog"]')
    await dialog.get('textarea').setValue('Legacy rejection still requires context.')
    await dialog.get('[data-testid="quota-reset-decision-submit"]').trigger('click')
    await flushPromises()
    expect(api.rejectQuotaResetRequest).toHaveBeenCalledWith(2, {
      decision_reason: 'Legacy rejection still requires context.',
    })
  })
})
