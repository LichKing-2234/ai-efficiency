<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import AppLayout from '@/components/AppLayout.vue'
import QuotaResetRequestList from '@/components/quota-reset/QuotaResetRequestList.vue'
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

type QueueMode = 'mine' | 'approvals' | 'admin'
type QueueStatus = 'idle' | 'loading' | 'loaded' | 'error'
type FilterMode = 'all' | 'pending' | 'processed' | 'failed'

const activeQueue = ref<QueueMode>('mine')
const activeFilter = ref<FilterMode>('all')

interface QueueState {
  status: QueueStatus
  items: QuotaResetRequestSummary[]
  total: number
  error: string
  actionBusy: boolean
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

const queues = reactive<Record<QueueMode, QueueState>>({
  mine: createQueueState(),
  approvals: createQueueState(),
  admin: createQueueState(),
})
const queueGenerations: Record<QueueMode, number> = { mine: 0, approvals: 0, admin: 0 }

interface ActiveQueueRequest {
  generation: number
  promise: Promise<void>
}

const activeRequests: Partial<Record<QueueMode, ActiveQueueRequest>> = {}
const filters: FilterMode[] = ['all', 'pending', 'processed', 'failed']
const currentQueue = computed(() => queues[activeQueue.value])
const myTotal = computed(() => queues.mine.total)
const approvalTotal = computed(() => workItems.loading || workItems.error ? 0 : workItems.counts.quota_reset_approval_count)
const adminTotal = computed(() => workItems.loading || workItems.error || !auth.isAdmin ? 0 : workItems.counts.quota_reset_admin_count)
const visibleItems = computed(() => currentQueue.value.items.filter((item) => filterMatches(item.status, activeFilter.value)))

function listQueue(mode: QueueMode) {
  if (mode === 'approvals') return listQuotaResetApprovals()
  if (mode === 'admin') return listAdminQuotaResetRequests()
  return listMyQuotaResetRequests()
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
      const response = await listQueue(mode)
      if (queueGenerations[mode] !== generation) return
      const items = response.data.data?.items ?? []
      state.items = items
      state.total = response.data.data?.total ?? items.length
      state.status = 'loaded'
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
  } catch {
    showToast({ message: t('quotaReset.actionFailed'), tone: 'error' })
  } finally {
    sourceState.actionBusy = false
  }
}

function rejectReason() {
  return window.prompt(t('quotaReset.rejectPrompt'))?.trim() ?? ''
}

function handleCancel(item: QuotaResetRequestSummary) {
  void withAction('mine', () => cancelQuotaResetRequest(item.id))
}

function handleApprove(item: QuotaResetRequestSummary) {
  const sourceQueue = activeQueue.value
  if (sourceQueue === 'admin') {
    void withAction(sourceQueue, () => adminApproveQuotaResetRequest(item.id, {}))
    return
  }
  void withAction(sourceQueue, () => approveQuotaResetRequest(item.id, {}))
}

function handleReject(item: QuotaResetRequestSummary) {
  const decisionReason = rejectReason()
  if (!decisionReason) return
  const sourceQueue = activeQueue.value
  if (sourceQueue === 'admin') {
    void withAction(sourceQueue, () => adminRejectQuotaResetRequest(item.id, { decision_reason: decisionReason }))
    return
  }
  void withAction(sourceQueue, () => rejectQuotaResetRequest(item.id, { decision_reason: decisionReason }))
}

function handleRetry(item: QuotaResetRequestSummary) {
  const sourceQueue = activeQueue.value
  if (sourceQueue === 'admin') {
    void withAction(sourceQueue, () => adminRetryQuotaResetRequest(item.id))
    return
  }
  void withAction(sourceQueue, () => retryQuotaResetRequest(item.id))
}

onMounted(() => {
  void workItems.loadCounts()
  void loadQueue('mine')
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
            <span
              v-if="myTotal > 0"
              data-testid="quota-reset-tab-mine-count"
              :class="queueBadgeClass(activeQueue === 'mine')"
            >
              {{ countBadge(myTotal) }}
            </span>
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
        @cancel="handleCancel"
        @approve="handleApprove"
        @reject="handleReject"
        @retry="handleRetry"
      />
    </div>
  </AppLayout>
</template>
