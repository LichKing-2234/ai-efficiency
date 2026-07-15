import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useQuotaResetStore } from '@/stores/quotaReset'
import type { QuotaResetRequestSummary } from '@/types'

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

vi.mock('@/api/workItems', () => ({ getWorkItemCounts: vi.fn() }))

const failedRequest: QuotaResetRequestSummary = {
  id: 48,
  requester_user_id: 10,
  requester_display_name: 'Alice Example',
  requester_email: 'alice@example.com',
  requester_department_paths: ['Department Alpha'],
  provider_id: 1,
  group_id: 'group-alpha',
  group_name: 'Group Alpha',
  group_platform: 'openai',
  reason: 'Retry a failed reset',
  status: 'approved_reset_failed',
  resolved_approver_user_ids: [],
  matched_department_paths: [],
  created_at: '2026-07-15T00:00:00Z',
  updated_at: '2026-07-15T00:00:00Z',
  workflow: {
    version: 2,
    current_node: null,
    nodes: [],
    decisions: [],
    can_approve: false,
    can_reject: false,
    can_cancel: false,
    can_retry: true,
  },
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => { resolve = res })
  return { promise, resolve }
}

function response(items: QuotaResetRequestSummary[]) {
  return { data: { data: { items, page: 1, page_size: 20, total: items.length } } }
}

beforeEach(async () => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
  const api = await import('@/api/quotaReset') as any
  const workItems = await import('@/api/workItems') as any
  api.listMyQuotaResetRequests.mockResolvedValue(response([]))
  api.listQuotaResetApprovals.mockResolvedValue(response([]))
  api.listAdminQuotaResetRequests.mockResolvedValue(response([]))
  api.retryQuotaResetRequest.mockResolvedValue({ data: { data: failedRequest } })
  workItems.getWorkItemCounts.mockResolvedValue({
    data: { data: { quota_reset_approval_count: 0, quota_reset_admin_count: 0, ai_access_setup_count: 0, offboarding_count: 0, total_count: 0 } },
  })
})

describe('quota reset store', () => {
  it('deduplicates processed history and rejects stale generations', async () => {
    const api = await import('@/api/quotaReset') as any
    const stale = deferred<any>()
    const fresh = deferred<any>()
    let calls = 0
    api.listQuotaResetApprovals.mockImplementation((params?: { scope?: string }) => {
      if (params?.scope !== 'history') return Promise.resolve(response([]))
      calls += 1
      return calls === 1 ? stale.promise : fresh.promise
    })
    const store = useQuotaResetStore()

    store.selectQueue('approvals')
    store.selectFilter('processed')
    const duplicate = store.loadApprovalHistory()
    expect(calls).toBe(1)
    store.invalidateApprovalHistory()
    store.selectFilter('all')
    store.selectFilter('processed')
    const freshLoad = store.loadApprovalHistory()
    fresh.resolve(response([{ ...failedRequest, id: 49, group_name: 'Fresh history' }]))
    await freshLoad
    stale.resolve(response([{ ...failedRequest, id: 50, group_name: 'Stale history' }]))
    await duplicate

    expect(calls).toBe(2)
    expect(store.approvalHistoryRequests.map(item => item.id)).toEqual([49])
  })

  it('refreshes processed history after retry reconciliation', async () => {
    const api = await import('@/api/quotaReset') as any
    api.listQuotaResetApprovals.mockImplementation((params?: { scope?: string }) => (
      params?.scope === 'history'
        ? Promise.resolve(response([failedRequest]))
        : Promise.resolve(response([]))
    ))
    const store = useQuotaResetStore()
    store.selectQueue('approvals')
    store.selectFilter('processed')
    await store.loadApprovalHistory()

    const result = await store.retry(failedRequest, false)

    expect(result).toBe('success')
    expect(api.retryQuotaResetRequest).toHaveBeenCalledWith(48)
    expect(api.listQuotaResetApprovals.mock.calls.filter(([params]: [{ scope?: string }?]) => params?.scope === 'history')).toHaveLength(2)
  })
})
