import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import QuotaResetView from '@/views/QuotaResetView.vue'
import { useAuthStore } from '@/stores/auth'
import { useWorkItemsStore } from '@/stores/workItems'
import { setLocale } from '@/i18n'
import type { QuotaResetWorkflowDecision, QuotaResetWorkflowStep } from '@/types'

type ExactKeys<T, Keys extends PropertyKey> = Exclude<keyof T, Keys> extends never
  ? Exclude<Keys, keyof T> extends never ? true : false
  : false

const publicWorkflowTypeContract = [
  true satisfies ExactKeys<QuotaResetWorkflowDecision, 'actor_user_id' | 'actor_display_name' | 'comment' | 'decided_at'>,
  true satisfies ExactKeys<QuotaResetWorkflowStep, 'step_number' | 'label' | 'admin_fallback' | 'status' | 'decision' | 'satisfied_by_step_number'>,
]
void publicWorkflowTypeContract

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

vi.mock('@/api/teamUsage', () => ({
  getTeamUsageScope: vi.fn(),
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
  workflow_version: 2,
  current_step: 0,
  workflow_steps: [
    {
      step_number: 1,
      label: 'Company / Group Beta',
      admin_fallback: false,
      status: 'active',
    },
    {
      step_number: 2,
      label: 'Company / Security',
      admin_fallback: false,
      status: 'queued',
    },
  ],
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
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
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

async function mountQuotaResetView(
  role: 'user' | 'admin' = 'user',
  initialPath = '/usage/quota-reset',
) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore()
  auth.user = { id: role === 'admin' ? 99 : 20, username: role, email: `${role}@example.com`, role, auth_source: 'ldap' }
  const router = createTestRouter()
  await router.push(initialPath)
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
  const teamUsageApi = await import('@/api/teamUsage') as any
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
  it('opens approval deep links in the approval queue and selects the request', async () => {
    const wrapper = await mountQuotaResetView(
      'user',
      '/usage/quota-reset?queue=approvals&request_id=2',
    )

    const approvals = wrapper.get('[data-testid="quota-reset-tab-approvals"]')
    expect(approvals.classes()).toContain('el-radio-button')
    expect((approvals.get('input[type="radio"]').element as HTMLInputElement).checked).toBe(true)
    expect(wrapper.find('[data-testid="quota-reset-workflow-timeline"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Group Beta')
  })

  it('falls back to my requests for invalid deep-link parameters', async () => {
    const wrapper = await mountQuotaResetView(
      'user',
      '/usage/quota-reset?queue=unknown&request_id=invalid',
    )

    expect((wrapper.get('[data-testid="quota-reset-tab-mine"] input[type="radio"]').element as HTMLInputElement).checked).toBe(true)
    expect(wrapper.text()).toContain('Group Alpha')
  })

  it('loads the active mine queue and work-item counts on mount', async () => {
    const api = await import('@/api/quotaReset') as any
    const workItemsApi = await import('@/api/workItems') as any
    const wrapper = await mountQuotaResetView()

    expect(api.listMyQuotaResetRequests).toHaveBeenCalledTimes(1)
    expect(api.listQuotaResetApprovals).not.toHaveBeenCalled()
    expect(api.listAdminQuotaResetRequests).not.toHaveBeenCalled()
    expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="quota-reset-tab-admin"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Group Alpha')
    expect(wrapper.text()).toContain('Need reset for a build investigation')
  })


  it('loads approvals on first selection and reuses them on repeated visits', async () => {
    const api = await import('@/api/quotaReset') as any
    const wrapper = await mountQuotaResetView()

    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    await flushPromises()

    expect(api.listQuotaResetApprovals).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('Group Beta')

    await wrapper.get('[data-testid="quota-reset-tab-mine"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    await flushPromises()

    expect(api.listQuotaResetApprovals).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('Group Beta')
  })

  it('loads the admin queue only after an administrator selects it', async () => {
    const api = await import('@/api/quotaReset') as any
    api.listAdminQuotaResetRequests.mockResolvedValue({ data: { data: { items: [approvalRequest], page: 1, page_size: 20, total: 1 } } })
    const wrapper = await mountQuotaResetView('admin')

    expect(wrapper.find('[data-testid="quota-reset-tab-admin"]').exists()).toBe(true)
    expect(api.listAdminQuotaResetRequests).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="quota-reset-tab-admin"]').trigger('click')
    await flushPromises()

    expect(api.listAdminQuotaResetRequests).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('Group Beta')
  })

  it('deduplicates a delayed approval load and keeps the visible mine queue available', async () => {
    const api = await import('@/api/quotaReset') as any
    const pendingApprovals = deferred<any>()
    api.listQuotaResetApprovals.mockReturnValue(pendingApprovals.promise)
    const wrapper = await mountQuotaResetView()

    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-tab-mine"]').trigger('click')

    expect(wrapper.text()).toContain('Group Alpha')
    expect(wrapper.text()).not.toContain('Loading...')

    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-tab-mine"]').trigger('click')
    expect(api.listQuotaResetApprovals).toHaveBeenCalledTimes(1)

    pendingApprovals.resolve({ data: { data: { items: [approvalRequest], page: 1, page_size: 20, total: 1 } } })
    await flushPromises()
    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')

    expect(api.listQuotaResetApprovals).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('Group Beta')
  })

  it('contains a hidden queue failure until that queue is selected again', async () => {
    const api = await import('@/api/quotaReset') as any
    const pendingApprovals = deferred<any>()
    api.listQuotaResetApprovals.mockReturnValue(pendingApprovals.promise)
    const wrapper = await mountQuotaResetView()

    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-tab-mine"]').trigger('click')
    pendingApprovals.reject(new Error('approvals unavailable'))
    await flushPromises()

    expect(wrapper.text()).toContain('Group Alpha')
    expect(wrapper.text()).not.toContain('Failed to load quota reset requests')

    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    expect(wrapper.text()).toContain('Failed to load quota reset requests')
    expect(wrapper.text()).not.toContain('Group Alpha')
  })

  it('refreshes only the active queue and never serves its old rows after failure', async () => {
    const api = await import('@/api/quotaReset') as any
    const pendingMine = deferred<any>()
    const wrapper = await mountQuotaResetView('admin')
    api.listMyQuotaResetRequests.mockReturnValueOnce(pendingMine.promise)

    await wrapper.get('[data-testid="quota-reset-refresh"]').trigger('click')

    expect(api.listMyQuotaResetRequests).toHaveBeenCalledTimes(2)
    expect(api.listQuotaResetApprovals).not.toHaveBeenCalled()
    expect(api.listAdminQuotaResetRequests).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('Group Alpha')
    expect(wrapper.text()).toContain('Loading...')

    pendingMine.reject(new Error('mine unavailable'))
    await flushPromises()

    expect(wrapper.text()).toContain('Failed to load quota reset requests')
    expect(wrapper.text()).not.toContain('Group Alpha')
  })

  it('shows a queue load failure without a contradictory empty state and retries in place', async () => {
    const api = await import('@/api/quotaReset') as any
    api.listMyQuotaResetRequests.mockRejectedValueOnce(new Error('mine unavailable'))
    const wrapper = await mountQuotaResetView()

    expect(wrapper.text()).toContain('Failed to load quota reset requests')
    expect(wrapper.text()).not.toContain('No quota reset requests yet.')

    await wrapper.get('[data-testid="quota-reset-refresh"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Group Alpha')
    expect(wrapper.text()).not.toContain('Failed to load quota reset requests')
  })

  it('refreshes invalidated counts without blocking refreshed queue history', async () => {
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
    await flushPromises()
    await wrapper.get('[data-testid="quota-reset-approve-2"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-decision-comment"]').setValue('Approved after review')
    await wrapper.get('form[role="dialog"]').trigger('submit')
    await flushPromises()

    expect(api.approveQuotaResetRequest).toHaveBeenCalledWith(2, { decision_reason: 'Approved after review' })
    expect(api.listQuotaResetApprovals).toHaveBeenCalledTimes(2)
    expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('Group Beta')
    expect(wrapper.text()).not.toContain('Loading...')

    initialCounts.resolve(countsResponse(9))
    await flushPromises()

    expect(workItems.totalCount).toBe(0)
    expect(workItems.loading).toBe(true)
    expect(wrapper.text()).toContain('Group Beta')
    expect(wrapper.text()).not.toContain('Loading...')

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
      expectedArgs: [2, { decision_reason: 'Synthetic decision' }],
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
  ])('refreshes only the source queue and counts after $name succeeds', async ({ name, role, queue, selector, apiName, expectedArgs, status }) => {
    const api = await import('@/api/quotaReset') as any
    const workItemsApi = await import('@/api/workItems') as any
    const queueItem = {
      ...approvalRequest,
      status,
      approved_by_user_id: status === 'approved_reset_failed' ? 20 : undefined,
    }
    api.listQuotaResetApprovals.mockResolvedValue({ data: { data: { items: [queueItem], page: 1, page_size: 20, total: 1 } } })
    if (role === 'admin') {
      api.listAdminQuotaResetRequests.mockResolvedValue({ data: { data: { items: [queueItem], page: 1, page_size: 20, total: 1 } } })
    }
    const wrapper = await mountQuotaResetView(role)

    if (queue === 'approvals') {
      await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    } else if (queue === 'admin') {
      await wrapper.get('[data-testid="quota-reset-tab-admin"]').trigger('click')
    }
    await flushPromises()
    await wrapper.get(selector).trigger('click')
    if (name.includes('approve') || name.includes('reject')) {
      expect(wrapper.find('[data-testid="quota-reset-decision-dialog"]').exists()).toBe(true)
      await wrapper.get('[data-testid="quota-reset-decision-comment"]').setValue('Synthetic decision')
      await wrapper.get('form[role="dialog"]').trigger('submit')
    }
    await flushPromises()

    expect(api[apiName]).toHaveBeenCalledWith(...expectedArgs)
    expect(api.listMyQuotaResetRequests).toHaveBeenCalledTimes(queue === 'mine' ? 2 : 1)
    expect(api.listQuotaResetApprovals).toHaveBeenCalledTimes(queue === 'approvals' ? 2 : 0)
    expect(api.listAdminQuotaResetRequests).toHaveBeenCalledTimes(queue === 'admin' ? 2 : 0)
    expect(workItemsApi.getWorkItemCounts).toHaveBeenCalledTimes(2)
  })

  it('invalidates only the overlapping admin queue after cancellation', async () => {
    const api = await import('@/api/quotaReset') as any
    api.listAdminQuotaResetRequests.mockResolvedValue({ data: { data: { items: [approvalRequest], page: 1, page_size: 20, total: 1 } } })
    const wrapper = await mountQuotaResetView('admin')

    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="quota-reset-tab-admin"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="quota-reset-tab-mine"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-cancel-1"]').trigger('click')
    await flushPromises()

    expect(api.listMyQuotaResetRequests).toHaveBeenCalledTimes(2)
    expect(api.listQuotaResetApprovals).toHaveBeenCalledTimes(1)
    expect(api.listAdminQuotaResetRequests).toHaveBeenCalledTimes(1)

    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="quota-reset-tab-admin"]').trigger('click')
    await flushPromises()

    expect(api.listQuotaResetApprovals).toHaveBeenCalledTimes(1)
    expect(api.listAdminQuotaResetRequests).toHaveBeenCalledTimes(2)
  })

  it('does not let a hidden mutation action block the newly visible queue', async () => {
    const api = await import('@/api/quotaReset') as any
    const pendingAction = deferred<any>()
    api.approveQuotaResetRequest.mockReturnValue(pendingAction.promise)
    const wrapper = await mountQuotaResetView()

    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="quota-reset-approve-2"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-tab-mine"]').trigger('click')

    expect(wrapper.text()).toContain('Group Alpha')
    expect(wrapper.text()).not.toContain('Loading...')

    pendingAction.resolve({ data: { data: { ...approvalRequest, status: 'approved_reset_succeeded' } } })
    await flushPromises()

    expect(wrapper.text()).toContain('Group Alpha')
    expect(wrapper.text()).not.toContain('Loading...')
  })

  it('does not show decision actions to an earlier approver after the workflow advances', async () => {
    const api = await import('@/api/quotaReset') as any
    api.listQuotaResetApprovals.mockResolvedValue({
      data: {
        data: {
          items: [{
            ...approvalRequest,
            current_step: 1,
            resolved_approver_user_ids: [30],
            workflow_steps: [
              {
                ...approvalRequest.workflow_steps[0],
                status: 'approved',
                decision: {
                  actor_user_id: 20,
                  actor_display_name: 'user',
                  comment: 'Approved first step',
                  decided_at: '2026-07-15T01:00:00Z',
                },
              },
              { ...approvalRequest.workflow_steps[1], status: 'active' },
            ],
          }],
          page: 1,
          page_size: 20,
          total: 1,
        },
      },
    })

    const wrapper = await mountQuotaResetView()
    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')

    expect(wrapper.find('[data-testid="quota-reset-approve-2"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quota-reset-reject-2"]').exists()).toBe(false)
    await wrapper.get('[data-testid="quota-reset-row-2"]').trigger('click')
    expect(wrapper.text()).toContain('Approved first step')
  })

  it('loads processed approval history beyond the first API page', async () => {
    const api = await import('@/api/quotaReset') as any
    const firstPage = Array.from({ length: 100 }, (_, index) => ({
      ...approvalRequest,
      id: 1000 + index,
    }))
    const archivedRequest = {
      ...approvalRequest,
      id: 1100,
      group_name: 'Archived Group',
      status: 'approved_reset_succeeded',
    }
    api.listQuotaResetApprovals.mockImplementation((params?: { page?: number }) => Promise.resolve({
      data: {
        data: params?.page === 2
          ? { items: [archivedRequest], page: 2, page_size: 100, total: 101 }
          : { items: firstPage, page: 1, page_size: 100, total: 101 },
      },
    }))

    const wrapper = await mountQuotaResetView()
    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    wrapper.findComponent({ name: 'ElSelect' }).vm.$emit('update:modelValue', 'processed')
    await flushPromises()

    expect(api.listQuotaResetApprovals).toHaveBeenCalledWith({ page: 2, page_size: 100 })
    expect(wrapper.text()).toContain('Archived Group')
  })

  it('shows actionable counts only for approval queues', async () => {
    const api = await import('@/api/quotaReset') as any
    api.listMyQuotaResetRequests.mockResolvedValue({ data: { data: { items: [mineRequest], page: 1, page_size: 20, total: 4 } } })
    api.listQuotaResetApprovals.mockResolvedValue({ data: { data: { items: [approvalRequest], page: 1, page_size: 20, total: 7 } } })
    api.listAdminQuotaResetRequests.mockResolvedValue({ data: { data: { items: [approvalRequest], page: 1, page_size: 20, total: 12 } } })

    const wrapper = await mountQuotaResetView('admin')

    expect(wrapper.find('[data-testid="quota-reset-tab-mine-count"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="quota-reset-tab-approvals-count"]').text()).toBe('2')
    expect(wrapper.get('[data-testid="quota-reset-tab-admin-count"]').text()).toBe('3')
  })

  it('expands workflow details inline from a compact approval row', async () => {
    const wrapper = await mountQuotaResetView()

    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    const row = wrapper.get('[data-testid="quota-reset-row-2"]')
    expect(row.classes()).toContain('p-3')
    const layout = row.get('[data-testid="quota-reset-row-layout-2"]')
    expect(layout.classes()).toContain('xl:grid-cols-[minmax(0,1fr)_auto]')
    expect(layout.classes()).not.toContain('md:grid-cols-[minmax(0,1fr)_auto]')
    expect(row.get('[data-testid="quota-reset-reason-2"]').classes()).not.toContain('line-clamp-1')

    await row.trigger('click')
    expect(row.attributes('aria-expanded')).toBe('true')
    expect(row.classes()).toContain('bg-cyan-50')
    expect(row.find('[data-testid="quota-reset-inline-workflow-2"]').exists()).toBe(true)

    await row.trigger('click')
    expect(row.attributes('aria-expanded')).toBe('false')
    expect(row.find('[data-testid="quota-reset-inline-workflow-2"]').exists()).toBe(false)
  })

  it('shows a workflow toggle that expands and collapses from the keyboard', async () => {
    const wrapper = await mountQuotaResetView()

    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    const row = wrapper.get('[data-testid="quota-reset-row-2"]')
    const toggle = row.get('[data-testid="quota-reset-workflow-toggle-2"]')
    expect(toggle.element.tagName).toBe('BUTTON')
    expect(toggle.attributes('type')).toBe('button')
    expect(toggle.text()).toBe('Approval progress')
    expect(toggle.attributes('aria-expanded')).toBe('false')

    await toggle.trigger('keydown', { key: 'Enter' })
    await toggle.trigger('keyup', { key: 'Enter' })
    toggle.element.dispatchEvent(new MouseEvent('click', { bubbles: true, detail: 0 }))
    await flushPromises()
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(row.find('[data-testid="quota-reset-inline-workflow-2"]').exists()).toBe(true)

    await toggle.trigger('keydown', { key: ' ' })
    await toggle.trigger('keyup', { key: ' ' })
    toggle.element.dispatchEvent(new MouseEvent('click', { bubbles: true, detail: 0 }))
    await flushPromises()
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(row.find('[data-testid="quota-reset-inline-workflow-2"]').exists()).toBe(false)
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

    expect(wrapper.find('[data-testid="quota-reset-tab-mine-count"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quota-reset-tab-approvals-count"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quota-reset-tab-admin-count"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Group Alpha')

    await wrapper.get('[data-testid="quota-reset-tab-admin"]').trigger('click')
    await flushPromises()
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

    expect(wrapper.find('[data-testid="usage-center-tabs"]').exists()).toBe(false)

    const queueSelector = wrapper.get('[data-testid="quota-reset-queue-selector"]')
    expect(queueSelector.classes()).toContain('min-w-0')
    expect(queueSelector.classes()).toContain('el-radio-group')
    expect(queueSelector.classes()).toContain('sm:!inline-grid')
    expect(queueSelector.classes()).not.toContain('sm:!inline-flex')
    expect(wrapper.get('[data-testid="quota-reset-tab-approvals-count"]').classes()).not.toContain('!hidden')
    expect(wrapper.get('[data-testid="quota-reset-tab-admin-count"]').classes()).not.toContain('!hidden')
    for (const queue of ['mine', 'approvals', 'admin']) {
      const option = wrapper.get(`[data-testid="quota-reset-tab-${queue}"]`)
      expect(option.classes()).toContain('w-full')
      expect(option.classes()).toContain('[&>span]:w-full')
    }
    expect((queueSelector.get('[data-testid="quota-reset-tab-mine"] input').element as HTMLInputElement).checked).toBe(true)

    const statusFilter = wrapper.get('[data-testid="quota-reset-status-filter"]')
    expect(statusFilter.classes()).toContain('el-select')
    expect(statusFilter.classes()).toContain('flex-1')
    expect(statusFilter.attributes('data-testid')).toBe('quota-reset-status-filter')
    expect(wrapper.text()).toContain('Status')
  })

  it('requires a decision comment and shows workflow history', async () => {
    const api = await import('@/api/quotaReset') as any
    const historicalRequest = {
      ...approvalRequest,
      current_step: 1,
      workflow_steps: [
        {
          ...approvalRequest.workflow_steps[0],
          status: 'approved',
          decision: {
            actor_user_id: 21,
            actor_display_name: 'Team Lead',
            comment: 'Initial department approved',
            decided_at: '2026-07-15T01:00:00Z',
          },
        },
        { ...approvalRequest.workflow_steps[1], status: 'active' },
      ],
    }
    api.listQuotaResetApprovals.mockResolvedValue({ data: { data: { items: [historicalRequest], page: 1, page_size: 20, total: 1 } } })
    const wrapper = await mountQuotaResetView()

    await wrapper.get('[data-testid="quota-reset-tab-approvals"]').trigger('click')
    await wrapper.get('[data-testid="quota-reset-row-2"]').trigger('click')
    expect(wrapper.find('[data-testid="quota-reset-workflow-timeline"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Initial department approved')
    expect(wrapper.text()).toContain('Company / Security')

    await wrapper.get('[data-testid="quota-reset-approve-2"]').trigger('click')
    expect(wrapper.get('[data-testid="quota-reset-decision-confirm"]').attributes('disabled')).toBeDefined()
    expect(api.approveQuotaResetRequest).not.toHaveBeenCalled()
  })
})
