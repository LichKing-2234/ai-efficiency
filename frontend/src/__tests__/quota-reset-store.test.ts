import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'
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

function setSession(userID: number, role: 'user' | 'admin', token: string) {
  const auth = useAuthStore()
  auth.token = token
  auth.user = {
    id: userID,
    username: role === 'admin' ? 'admin-a' : 'user-b',
    email: role === 'admin' ? 'admin-a@example.com' : 'user-b@example.org',
    role,
    auth_source: 'ldap',
  }
  return auth
}

beforeEach(async () => {
  localStorage.clear()
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
  it('clears admin A state synchronously before non-admin B failed reload', async () => {
    const api = await import('@/api/quotaReset') as any
    const adminMine = { ...failedRequest, id: 101, group_name: 'Admin A Mine' }
    const adminApproval = { ...failedRequest, id: 102, group_name: 'Admin A Approval' }
    const adminHistory = { ...failedRequest, id: 103, group_name: 'Admin A History' }
    const adminQueue = { ...failedRequest, id: 104, group_name: 'Admin A Queue' }
    const auth = setSession(1, 'admin', 'token-admin-a')
    api.listMyQuotaResetRequests.mockResolvedValueOnce(response([adminMine]))
    api.listQuotaResetApprovals.mockImplementation((params?: { scope?: string }) => (
      params?.scope === 'history' ? Promise.resolve(response([adminHistory])) : Promise.resolve(response([adminApproval]))
    ))
    api.listAdminQuotaResetRequests.mockResolvedValueOnce(response([adminQueue]))
    const store = useQuotaResetStore()

    await store.loadQueues()
    store.selectQueue('approvals')
    store.selectFilter('processed')
    await store.loadApprovalHistory()
    expect(store.visibleItems.map(item => item.id)).toEqual([103])

    auth.logout()
    setSession(2, 'user', 'token-user-b')
    api.listMyQuotaResetRequests.mockRejectedValueOnce(new Error('B reload pending'))
    api.listQuotaResetApprovals.mockRejectedValueOnce(new Error('B reload pending'))
    const reload = store.loadQueues()

    expect(store.activeQueue).toBe('mine')
    expect(store.activeFilter).toBe('all')
    expect(store.myRequests).toEqual([])
    expect(store.approvalRequests).toEqual([])
    expect(store.approvalHistoryRequests).toEqual([])
    expect(store.adminRequests).toEqual([])
    expect(store.myTotal).toBe(0)
    expect(store.actionBusy).toBe(false)
    await reload
    expect(store.visibleItems).toEqual([])
  })

  it('ignores deferred core and history responses from admin A after user B becomes active', async () => {
    const api = await import('@/api/quotaReset') as any
    const aMine = deferred<any>()
    const aApprovals = deferred<any>()
    const aHistory = deferred<any>()
    const bMine = { ...failedRequest, id: 201, group_name: 'User B Mine' }
    const bApproval = { ...failedRequest, id: 202, group_name: 'User B Approval' }
    const bHistory = { ...failedRequest, id: 203, group_name: 'User B History' }
    let coreApprovalCalls = 0
    let historyCalls = 0
    api.listMyQuotaResetRequests
      .mockReturnValueOnce(aMine.promise)
      .mockResolvedValueOnce(response([bMine]))
    api.listQuotaResetApprovals.mockImplementation((params?: { scope?: string }) => {
      if (params?.scope === 'history') {
        historyCalls += 1
        return historyCalls === 1 ? aHistory.promise : Promise.resolve(response([bHistory]))
      }
      coreApprovalCalls += 1
      return coreApprovalCalls === 1 ? aApprovals.promise : Promise.resolve(response([bApproval]))
    })
    api.listAdminQuotaResetRequests.mockResolvedValue(response([]))
    setSession(1, 'admin', 'token-admin-a')
    const store = useQuotaResetStore()

    const adminCoreLoad = store.loadQueues()
    store.selectQueue('approvals')
    store.selectFilter('processed')
    const adminHistoryLoad = store.loadApprovalHistory()

    setSession(2, 'user', 'token-user-b')
    const userCoreLoad = store.loadQueues()
    store.selectQueue('approvals')
    store.selectFilter('processed')
    const userHistoryLoad = store.loadApprovalHistory()

    aMine.resolve(response([{ ...failedRequest, id: 211, group_name: 'Stale Admin Mine' }]))
    aApprovals.resolve(response([{ ...failedRequest, id: 212, group_name: 'Stale Admin Approval' }]))
    aHistory.resolve(response([{ ...failedRequest, id: 213, group_name: 'Stale Admin History' }]))
    await Promise.all([adminCoreLoad, adminHistoryLoad, userCoreLoad, userHistoryLoad])

    expect(store.myRequests.map(item => item.id)).toEqual([201])
    expect(store.approvalRequests.map(item => item.id)).toEqual([202])
    expect(store.approvalHistoryRequests.map(item => item.id)).toEqual([203])
    expect(store.adminRequests).toEqual([])
    expect(historyCalls).toBe(2)
  })

  it('ignores an action completion from a prior auth session', async () => {
    const api = await import('@/api/quotaReset') as any
    const staleAction = deferred<any>()
    api.cancelQuotaResetRequest.mockReturnValueOnce(staleAction.promise)
    api.listMyQuotaResetRequests.mockResolvedValue(response([{ ...failedRequest, id: 301, group_name: 'User B Mine' }]))
    api.listQuotaResetApprovals.mockResolvedValue(response([]))
    setSession(1, 'admin', 'token-admin-a')
    const store = useQuotaResetStore()
    const resultPromise = store.cancel(48)

    setSession(2, 'user', 'token-user-b')
    await store.loadQueues()
    const coreCallsBeforeStaleAction = api.listMyQuotaResetRequests.mock.calls.length
    staleAction.resolve({ data: { data: failedRequest } })
    const result = await resultPromise

    expect(result).not.toBe('success')
    expect(api.listMyQuotaResetRequests).toHaveBeenCalledTimes(coreCallsBeforeStaleAction)
    expect(store.myRequests.map(item => item.id)).toEqual([301])
  })

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
