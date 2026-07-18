import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import QuotaResetView from '@/views/QuotaResetView.vue'
import { useAuthStore } from '@/stores/auth'
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
  it('opens approval deep links in the approval queue and selects the request', async () => {
    const wrapper = await mountQuotaResetView(
      'user',
      '/usage/quota-reset?queue=approvals&request_id=2',
    )

    expect(wrapper.get('[data-testid="quota-reset-tab-approvals"]').classes()).toContain('bg-white')
    expect(wrapper.find('[data-testid="quota-reset-workflow-timeline"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Group Beta')
  })

  it('falls back to my requests for invalid deep-link parameters', async () => {
    const wrapper = await mountQuotaResetView(
      'user',
      '/usage/quota-reset?queue=unknown&request_id=invalid',
    )

    expect(wrapper.get('[data-testid="quota-reset-tab-mine"]').classes()).toContain('bg-white')
    expect(wrapper.text()).toContain('Group Alpha')
  })

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
    expect(wrapper.find('[data-testid="quota-reset-decision-dialog"]').exists()).toBe(true)
    await wrapper.get('[data-testid="quota-reset-decision-comment"]').setValue('Usage spike confirmed')
    await wrapper.get('form[role="dialog"]').trigger('submit')
    await flushPromises()

    expect(api.approveQuotaResetRequest).toHaveBeenCalledWith(2, { decision_reason: 'Usage spike confirmed' })
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
    await wrapper.get('[data-testid="quota-reset-decision-comment"]').setValue('Approved after review')
    await wrapper.get('form[role="dialog"]').trigger('submit')

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

    expect(api.approveQuotaResetRequest).toHaveBeenCalledWith(2, { decision_reason: 'Approved after review' })
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
    await wrapper.get('[data-testid="quota-reset-filter-processed"]').trigger('click')

    expect(api.listQuotaResetApprovals).toHaveBeenCalledWith({ page: 2, page_size: 100 })
    expect(wrapper.text()).toContain('Archived Group')
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
