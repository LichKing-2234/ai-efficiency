<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import QuotaResetRequestList from '@/components/quota-reset/QuotaResetRequestList.vue'
import QuotaResetDecisionDialog from '@/components/quota-reset/QuotaResetDecisionDialog.vue'
import UsageCenterTabs from '@/components/user/usage/UsageCenterTabs.vue'
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
  type QuotaResetListParams,
} from '@/api/quotaReset'
import { useToast } from '@/composables/useToast'
import { useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import { useWorkItemsStore } from '@/stores/workItems'
import type { QuotaResetRequestSummary, QuotaResetStatus } from '@/types'

const { t } = useI18n()
const { showToast } = useToast()
const auth = useAuthStore()
const workItems = useWorkItemsStore()
const route = useRoute()

type QueueMode = 'mine' | 'approvals' | 'admin'
type QueueStatus = 'idle' | 'loading' | 'loaded' | 'error'
type FilterMode = 'all' | 'pending' | 'processed' | 'failed'
type QueueLoader = (params: QuotaResetListParams) => ReturnType<typeof listMyQuotaResetRequests>

interface QueueState {
  status: QueueStatus
  items: QuotaResetRequestSummary[]
  total: number
  error: string
  actionBusy: boolean
}

interface ActiveQueueRequest {
  generation: number
  promise: Promise<void>
}

function firstQueryValue(value: unknown) {
  return Array.isArray(value) ? value[0] : value
}

function initialQueue(): QueueMode {
  const value = firstQueryValue(route.query.queue)
  if (value === 'approvals' || value === 'mine') return value
  if (value === 'admin' && auth.isAdmin) return value
  return 'mine'
}

function initialRequestID() {
  const value = firstQueryValue(route.query.request_id)
  if (typeof value !== 'string' || !/^[1-9]\d*$/.test(value)) return null
  return Number(value)
}

function createQueueState(): QueueState {
  return {
    status: 'idle',
    items: [],
    total: 0,
    error: '',
    actionBusy: false,
  }
}

const activeQueue = ref<QueueMode>(initialQueue())
const activeFilter = ref<FilterMode>('all')
const selectedRequest = ref<QuotaResetRequestSummary | null>(null)
const decisionRequest = ref<QuotaResetRequestSummary | null>(null)
const decisionAction = ref<'approve' | 'reject'>('approve')
const decisionQueue = ref<QueueMode | null>(null)
const queues = reactive<Record<QueueMode, QueueState>>({
  mine: createQueueState(),
  approvals: createQueueState(),
  admin: createQueueState(),
})
const queueGenerations: Record<QueueMode, number> = { mine: 0, approvals: 0, admin: 0 }
const activeRequests: Partial<Record<QueueMode, ActiveQueueRequest>> = {}
const filters: FilterMode[] = ['all', 'pending', 'processed', 'failed']
const queuePageSize = 100

const currentQueue = computed(() => queues[activeQueue.value])
const approvalTotal = computed(() => workItems.loading || workItems.error ? 0 : workItems.counts.quota_reset_approval_count)
const adminTotal = computed(() => workItems.loading || workItems.error || !auth.isAdmin ? 0 : workItems.counts.quota_reset_admin_count)
const visibleItems = computed(() => currentQueue.value.items.filter((item) => filterMatches(item.status, activeFilter.value)))

function queueLoader(mode: QueueMode): QueueLoader {
  if (mode === 'approvals') return listQuotaResetApprovals
  if (mode === 'admin') return listAdminQuotaResetRequests
  return listMyQuotaResetRequests
}

async function loadAllQueuePages(loader: QueueLoader) {
  const items: QuotaResetRequestSummary[] = []
  for (let page = 1; ; page += 1) {
    const response = await loader({ page, page_size: queuePageSize })
    const data = response.data.data
    const pageItems = data?.items ?? []
    items.push(...pageItems)
    const total = data?.total ?? items.length
    if (items.length >= total || pageItems.length < queuePageSize) return { items, total }
  }
}

function invalidateQueue(mode: QueueMode) {
  queueGenerations[mode] += 1
  delete activeRequests[mode]
  const state = queues[mode]
  state.status = 'idle'
  state.items = []
  state.total = 0
  state.error = ''
}

function syncSelectedRequest(mode: QueueMode, items: QuotaResetRequestSummary[]) {
  if (activeQueue.value !== mode) return

  const selectedID = selectedRequest.value?.id ?? initialRequestID()
  if (selectedID === null) return
  const request = items.find((item) => item.id === selectedID)
  selectedRequest.value = request?.workflow_steps?.length ? request : null
}

function loadQueue(mode: QueueMode, options: { force?: boolean } = {}): Promise<void> {
  if (mode === 'admin' && !auth.isAdmin) return Promise.resolve()

  const state = queues[mode]
  const activeRequest = activeRequests[mode]
  if (!options.force) {
    if (activeRequest) return activeRequest.promise
    if (state.status !== 'idle') return Promise.resolve()
  }

  const generation = ++queueGenerations[mode]
  state.status = 'loading'
  state.items = []
  state.total = 0
  state.error = ''

  const request: ActiveQueueRequest = {
    generation,
    promise: Promise.resolve(),
  }
  activeRequests[mode] = request
  request.promise = (async () => {
    try {
      const result = await loadAllQueuePages(queueLoader(mode))
      if (queueGenerations[mode] !== generation) return
      state.items = result.items
      state.total = result.total
      state.status = 'loaded'
      syncSelectedRequest(mode, result.items)
    } catch {
      if (queueGenerations[mode] !== generation) return
      state.items = []
      state.total = 0
      state.error = t('quotaReset.loadFailed')
      state.status = 'error'
    } finally {
      if (activeRequests[mode] === request) delete activeRequests[mode]
    }
  })()
  return request.promise
}

function selectQueue(mode: QueueMode) {
  if (mode === 'admin' && !auth.isAdmin) return
  activeQueue.value = mode
  void loadQueue(mode)
}

function refreshActiveQueue() {
  void loadQueue(activeQueue.value, { force: true })
}

function filterMatches(status: QuotaResetStatus, filter: FilterMode) {
  if (filter === 'all') return true
  if (filter === 'pending') return status === 'pending' || status === 'approved_resetting'
  if (filter === 'failed') return status === 'approved_reset_failed'
  return status === 'approved_reset_succeeded' || status === 'rejected' || status === 'cancelled'
}

function filterLabel(filter: FilterMode) {
  if (filter === 'pending') return t('quotaReset.filter.pending')
  if (filter === 'processed') return t('quotaReset.filter.processed')
  if (filter === 'failed') return t('quotaReset.filter.failed')
  return t('quotaReset.filter.all')
}

function countBadge(count: number) {
  return count > 99 ? '99+' : String(count)
}

function queueButtonClass(active: boolean) {
  return [
    'inline-flex min-h-10 items-center justify-center rounded-md px-3 py-2 text-sm font-medium transition-colors',
    active
      ? 'bg-white text-slate-950 shadow-sm ring-1 ring-slate-200'
      : 'text-slate-600 hover:bg-white/70',
  ]
}

function queueBadgeClass(active: boolean) {
  return [
    'ml-2 inline-flex min-w-6 justify-center rounded-full px-1.5 text-xs font-semibold',
    active ? 'bg-slate-900 text-white' : 'bg-white text-slate-600 ring-1 ring-slate-200',
  ]
}

function filterButtonClass(active: boolean) {
  return [
    'rounded-full px-3 py-1 text-xs font-medium transition-colors',
    active
      ? 'bg-white text-slate-950 shadow-sm ring-1 ring-slate-200'
      : 'text-slate-500 hover:bg-white/70',
  ]
}

const overlappingQueues: Record<QueueMode, QueueMode[]> = {
  mine: ['admin'],
  approvals: ['admin'],
  admin: ['mine', 'approvals'],
}

async function withAction(sourceQueue: QueueMode, action: () => Promise<unknown>) {
  const sourceState = queues[sourceQueue]
  sourceState.actionBusy = true
  try {
    await action()
    const refreshTargets = new Set<QueueMode>([sourceQueue])
    for (const mode of overlappingQueues[sourceQueue]) {
      if (mode === 'admin' && !auth.isAdmin) continue
      invalidateQueue(mode)
      if (activeQueue.value === mode) refreshTargets.add(mode)
    }
    workItems.invalidateCounts()
    void workItems.loadCounts({ force: true })
    await Promise.all(Array.from(refreshTargets, (mode) => loadQueue(mode, { force: true })))
    showToast({ message: t('quotaReset.actionSucceeded'), tone: 'success' })
    return true
  } catch {
    showToast({ message: t('quotaReset.actionFailed'), tone: 'error' })
    return false
  } finally {
    sourceState.actionBusy = false
  }
}

function handleCancel(item: QuotaResetRequestSummary) {
  void withAction('mine', () => cancelQuotaResetRequest(item.id))
}

function handleDecision(item: QuotaResetRequestSummary, action: 'approve' | 'reject') {
  decisionRequest.value = item
  decisionAction.value = action
  decisionQueue.value = activeQueue.value
}

function submitDecision(item: QuotaResetRequestSummary, comment: string, sourceQueue: QueueMode) {
  const submit = decisionAction.value === 'approve'
    ? (sourceQueue === 'admin' ? adminApproveQuotaResetRequest : approveQuotaResetRequest)
    : (sourceQueue === 'admin' ? adminRejectQuotaResetRequest : rejectQuotaResetRequest)
  return submit(item.id, { decision_reason: comment })
}

async function confirmDecision(comment: string) {
  const item = decisionRequest.value
  const sourceQueue = decisionQueue.value
  if (!item || !sourceQueue) return
  if (await withAction(sourceQueue, () => submitDecision(item, comment, sourceQueue))) {
    decisionRequest.value = null
    decisionQueue.value = null
  }
}

function handleSelect(item: QuotaResetRequestSummary) {
  selectedRequest.value = selectedRequest.value?.id === item.id ? null : item
}

function handleRetry(item: QuotaResetRequestSummary) {
  const sourceQueue = activeQueue.value
  const retry = sourceQueue === 'admin' ? adminRetryQuotaResetRequest : retryQuotaResetRequest
  void withAction(sourceQueue, () => retry(item.id))
}

function closeDecisionDialog() {
  decisionRequest.value = null
  decisionQueue.value = null
}

onMounted(() => {
  void workItems.loadCounts()
  void loadQueue(activeQueue.value)
})
</script>

<template>
  <AppLayout>
    <div class="space-y-6">
      <UsageCenterTabs active="quota-reset" show-quota-reset />
      <div>
        <h1 class="text-2xl font-semibold text-slate-950">{{ t('quotaReset.title') }}</h1>
        <p class="mt-1 text-sm text-slate-600">{{ t('quotaReset.subtitle') }}</p>
      </div>

      <section class="space-y-3" aria-label="Quota reset queues and filters">
        <div
          data-testid="quota-reset-queue-selector"
          :class="['grid w-full gap-1 rounded-lg bg-slate-100 p-1 sm:w-auto', auth.isAdmin ? 'grid-cols-3' : 'grid-cols-2']"
        >
          <button
            type="button"
            data-testid="quota-reset-tab-mine"
            :class="queueButtonClass(activeQueue === 'mine')"
            @click="selectQueue('mine')"
          >
            {{ t('quotaReset.myRequests') }}
          </button>
          <button
            type="button"
            data-testid="quota-reset-tab-approvals"
            :class="queueButtonClass(activeQueue === 'approvals')"
            @click="selectQueue('approvals')"
          >
            {{ t('quotaReset.myApprovals') }}
            <span
              v-if="approvalTotal > 0"
              data-testid="quota-reset-tab-approvals-count"
              :class="queueBadgeClass(activeQueue === 'approvals')"
            >
              {{ countBadge(approvalTotal) }}
            </span>
          </button>
          <button
            v-if="auth.isAdmin"
            type="button"
            data-testid="quota-reset-tab-admin"
            :class="queueButtonClass(activeQueue === 'admin')"
            @click="selectQueue('admin')"
          >
            {{ t('quotaReset.adminQueue') }}
            <span
              v-if="adminTotal > 0"
              data-testid="quota-reset-tab-admin-count"
              :class="queueBadgeClass(activeQueue === 'admin')"
            >
              {{ countBadge(adminTotal) }}
            </span>
          </button>
        </div>

        <div class="flex flex-wrap items-center justify-between gap-3">
          <div data-testid="quota-reset-status-filters" class="flex w-fit max-w-full flex-wrap gap-1 rounded-full bg-slate-100 p-1">
            <button
              v-for="filter in filters"
              :key="filter"
              type="button"
              :data-testid="`quota-reset-filter-${filter}`"
              :class="filterButtonClass(activeFilter === filter)"
              @click="activeFilter = filter"
            >
              {{ filterLabel(filter) }}
            </button>
          </div>
          <button
            type="button"
            data-testid="quota-reset-refresh"
            class="rounded-md border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50"
            :disabled="currentQueue.status === 'loading' || currentQueue.actionBusy"
            @click="refreshActiveQueue"
          >
            {{ t('quotaReset.refresh') }}
          </button>
        </div>
      </section>

      <div v-if="currentQueue.error" class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700">
        {{ currentQueue.error }}
      </div>

      <QuotaResetRequestList
        :items="visibleItems"
        :loading="currentQueue.status === 'loading' || currentQueue.actionBusy"
        :mode="activeQueue"
        :actor-user-id="auth.user?.id"
        :selected-request-id="selectedRequest?.id"
        @cancel="handleCancel"
        @approve="handleDecision($event, 'approve')"
        @reject="handleDecision($event, 'reject')"
        @retry="handleRetry"
        @select="handleSelect"
      />
    </div>
    <QuotaResetDecisionDialog
      v-if="decisionRequest"
      :action="decisionAction"
      :busy="decisionQueue ? queues[decisionQueue].actionBusy : false"
      @confirm="confirmDecision"
      @cancel="closeDecisionDialog"
    />
  </AppLayout>
</template>
