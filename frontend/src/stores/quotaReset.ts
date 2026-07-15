import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import {
  adminApproveQuotaResetRequest,
  adminRejectQuotaResetRequest,
  adminRetryQuotaResetRequest,
  approveQuotaResetRequest,
  cancelQuotaResetRequest,
  listAdminQuotaResetRequests,
  listMyQuotaResetRequests,
  listQuotaResetApprovals,
  rejectQuotaResetRequest,
  retryQuotaResetRequest,
  type QuotaResetApproveInput,
  type QuotaResetRejectInput,
} from '@/api/quotaReset'
import { useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import { useWorkItemsStore } from '@/stores/workItems'
import type { QuotaResetRequestSummary } from '@/types'

export type QueueMode = 'mine' | 'approvals' | 'admin'
export type FilterMode = 'all' | 'pending' | 'processed' | 'failed'
export type QuotaResetActionResult = 'success' | 'workflow_advanced' | 'failed' | 'busy'

interface WorkflowAdvancedDetails {
  request: QuotaResetRequestSummary | null
}

function workflowAdvancedDetails(error: unknown): WorkflowAdvancedDetails | null {
  const response = (error as {
    response?: {
      status?: number
      data?: { message?: string; details?: { request?: unknown } }
    }
  })?.response
  if (response?.status !== 409 || response.data?.message !== 'workflow_advanced') return null

  const request = response.data.details?.request
  if (!request || typeof request !== 'object' || typeof (request as { id?: unknown }).id !== 'number') {
    return { request: null }
  }
  return { request: request as QuotaResetRequestSummary }
}

function isWorkflowRequest(item: QuotaResetRequestSummary) {
  return (item.workflow?.version ?? 1) >= 2
}

export const useQuotaResetStore = defineStore('quotaReset', () => {
  const { t } = useI18n()
  const auth = useAuthStore()
  const workItems = useWorkItemsStore()
  const activeQueue = ref<QueueMode>('mine')
  const activeFilter = ref<FilterMode>('all')
  const myRequests = ref<QuotaResetRequestSummary[]>([])
  const approvalRequests = ref<QuotaResetRequestSummary[]>([])
  const approvalHistoryRequests = ref<QuotaResetRequestSummary[]>([])
  const adminRequests = ref<QuotaResetRequestSummary[]>([])
  const myTotal = ref(0)
  const approvalHistoryLoaded = ref(false)
  const coreLoading = ref(false)
  const approvalHistoryLoading = ref(false)
  const actionBusy = ref(false)
  const coreLoadError = ref('')
  const approvalHistoryError = ref('')
  const dataRevision = ref(0)
  let approvalHistoryGeneration = 0
  let approvalHistoryLoadPromise: Promise<void> | null = null

  const displayingApprovalHistory = computed(() => (
    activeQueue.value === 'approvals' && activeFilter.value === 'processed'
  ))
  const loading = computed(() => (
    displayingApprovalHistory.value ? approvalHistoryLoading.value : coreLoading.value
  ))
  const loadError = computed(() => (
    displayingApprovalHistory.value ? approvalHistoryError.value : coreLoadError.value
  ))
  const queueItems = computed(() => {
    if (activeQueue.value === 'approvals') {
      return activeFilter.value === 'processed' ? approvalHistoryRequests.value : approvalRequests.value
    }
    if (activeQueue.value === 'admin') return adminRequests.value
    return myRequests.value
  })
  const visibleItems = computed(() => queueItems.value.filter(item => filterMatches(item, activeFilter.value)))

  function filterMatches(item: QuotaResetRequestSummary, filter: FilterMode) {
    const { status } = item
    if (filter === 'all') return true
    if (filter === 'pending') return status === 'pending' || status === 'approved_resetting'
    if (filter === 'failed') return status === 'approved_reset_failed'
    if (activeQueue.value === 'approvals' && isWorkflowRequest(item)) return true
    return status === 'approved_reset_succeeded' || status === 'rejected' || status === 'cancelled'
  }

  function findRequest(requestID: number) {
    return queueItems.value.find(item => item.id === requestID)
      ?? myRequests.value.find(item => item.id === requestID)
      ?? approvalRequests.value.find(item => item.id === requestID)
      ?? approvalHistoryRequests.value.find(item => item.id === requestID)
      ?? adminRequests.value.find(item => item.id === requestID)
  }

  async function loadQueues(forceCounts = false) {
    coreLoading.value = true
    coreLoadError.value = ''
    void workItems.loadCounts({ force: forceCounts })
    try {
      const [mine, approvals] = await Promise.all([
        listMyQuotaResetRequests(),
        listQuotaResetApprovals(),
      ])
      myRequests.value = mine.data.data?.items ?? []
      approvalRequests.value = approvals.data.data?.items ?? []
      myTotal.value = mine.data.data?.total ?? myRequests.value.length
      if (auth.isAdmin) {
        const admin = await listAdminQuotaResetRequests()
        adminRequests.value = admin.data.data?.items ?? []
      } else {
        adminRequests.value = []
      }
      dataRevision.value += 1
    } catch {
      coreLoadError.value = t('quotaReset.loadFailed')
    } finally {
      coreLoading.value = false
    }
  }

  function loadApprovalHistory(): Promise<void> {
    if (approvalHistoryLoaded.value) return Promise.resolve()
    if (approvalHistoryLoadPromise) return approvalHistoryLoadPromise

    approvalHistoryLoading.value = true
    approvalHistoryError.value = ''
    const requestGeneration = approvalHistoryGeneration
    const request = (async () => {
      try {
        const history = await listQuotaResetApprovals({ scope: 'history' })
        if (requestGeneration !== approvalHistoryGeneration) return
        approvalHistoryRequests.value = history.data.data?.items ?? []
        approvalHistoryLoaded.value = true
        dataRevision.value += 1
      } catch {
        if (requestGeneration !== approvalHistoryGeneration) return
        approvalHistoryError.value = t('quotaReset.loadFailed')
      } finally {
        if (requestGeneration === approvalHistoryGeneration) {
          approvalHistoryLoading.value = false
          approvalHistoryLoadPromise = null
        }
      }
    })()
    approvalHistoryLoadPromise = request
    return request
  }

  function invalidateApprovalHistory() {
    approvalHistoryGeneration += 1
    approvalHistoryLoadPromise = null
    approvalHistoryLoaded.value = false
    approvalHistoryLoading.value = false
    approvalHistoryError.value = ''
    approvalHistoryRequests.value = []
    dataRevision.value += 1
  }

  function selectQueue(queue: QueueMode) {
    activeQueue.value = queue
    if (queue === 'approvals' && activeFilter.value === 'processed') {
      void loadApprovalHistory()
    }
  }

  function selectFilter(filter: FilterMode) {
    activeFilter.value = filter
    if (filter === 'processed' && activeQueue.value === 'approvals') {
      void loadApprovalHistory()
    }
  }

  function replaceMatchingRequest(requestID: number, replacement: QuotaResetRequestSummary) {
    if (replacement.id !== requestID) return false
    const replace = (items: QuotaResetRequestSummary[]) => items.map(item => (
      item.id === requestID ? replacement : item
    ))
    myRequests.value = replace(myRequests.value)
    approvalRequests.value = replace(approvalRequests.value)
    approvalHistoryRequests.value = replace(approvalHistoryRequests.value)
    adminRequests.value = replace(adminRequests.value)
    dataRevision.value += 1
    return true
  }

  async function runAction(requestID: number, action: () => Promise<unknown>): Promise<QuotaResetActionResult> {
    if (actionBusy.value) return 'busy'
    actionBusy.value = true
    try {
      await action()
      const refreshDisplayedHistory = displayingApprovalHistory.value
      invalidateApprovalHistory()
      if (refreshDisplayedHistory) void loadApprovalHistory()
      await loadQueues(true)
      return 'success'
    } catch (error) {
      const advanced = workflowAdvancedDetails(error)
      if (!advanced) return 'failed'
      if (advanced.request && replaceMatchingRequest(requestID, advanced.request)) {
        void workItems.loadCounts({ force: true })
      } else {
        await loadQueues(true)
      }
      return 'workflow_advanced'
    } finally {
      actionBusy.value = false
    }
  }

  function cancel(requestID: number) {
    return runAction(requestID, () => cancelQuotaResetRequest(requestID))
  }

  function approve(requestID: number, admin: boolean, data: QuotaResetApproveInput = {}) {
    return runAction(requestID, () => (
      admin ? adminApproveQuotaResetRequest(requestID, data) : approveQuotaResetRequest(requestID, data)
    ))
  }

  function reject(requestID: number, admin: boolean, data: QuotaResetRejectInput) {
    return runAction(requestID, () => (
      admin ? adminRejectQuotaResetRequest(requestID, data) : rejectQuotaResetRequest(requestID, data)
    ))
  }

  function retry(item: QuotaResetRequestSummary, admin: boolean) {
    return runAction(item.id, () => (
      admin ? adminRetryQuotaResetRequest(item.id) : retryQuotaResetRequest(item.id)
    ))
  }

  return {
    activeQueue,
    activeFilter,
    myRequests,
    approvalRequests,
    approvalHistoryRequests,
    adminRequests,
    myTotal,
    actionBusy,
    dataRevision,
    loading,
    loadError,
    visibleItems,
    loadQueues,
    loadApprovalHistory,
    invalidateApprovalHistory,
    selectQueue,
    selectFilter,
    findRequest,
    cancel,
    approve,
    reject,
    retry,
  }
})
