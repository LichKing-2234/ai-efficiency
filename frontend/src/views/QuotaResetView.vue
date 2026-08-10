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
import { getTeamUsageScope } from '@/api/teamUsage'
import { useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import { useWorkItemsStore } from '@/stores/workItems'
import type { QuotaResetRequestSummary, QuotaResetStatus } from '@/types'

const { t } = useI18n()
const auth = useAuthStore()
const workItems = useWorkItemsStore()
const route = useRoute()
const hasTeamUsageScope = ref(false)

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
    ElMessage.success(t('quotaReset.actionSucceeded'))
    return true
  } catch {
    ElMessage.error(t('quotaReset.actionFailed'))
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
  void getTeamUsageScope()
    .then((response) => {
      hasTeamUsageScope.value = response.data.data?.is_representative === true
    })
    .catch(() => {
      hasTeamUsageScope.value = false
    })
  void workItems.loadCounts()
  void loadQueue(activeQueue.value)
})
</script>

<template>
  <AppLayout>
    <div class="space-y-6">
      <div>
        <h1 class="text-2xl font-semibold text-slate-950">{{ t('quotaReset.title') }}</h1>
        <p class="mt-1 text-sm text-slate-600">{{ t('quotaReset.subtitle') }}</p>
      </div>
      <UsageCenterTabs active="quota-reset" :show-team="hasTeamUsageScope" show-quota-reset />

      <section class="space-y-3" aria-label="Quota reset queues and filters">
        <ElRadioGroup
          data-testid="quota-reset-queue-selector"
          :model-value="activeQueue"
          :class="['!grid min-w-0 w-full gap-1 sm:!inline-grid sm:w-auto', auth.isAdmin ? 'grid-cols-3' : 'grid-cols-2']"
        >
          <ElRadioButton
            data-testid="quota-reset-tab-mine"
            class="min-w-0 w-full [&>span]:w-full [&>span]:!whitespace-normal [&>span]:!px-1"
            value="mine"
            @click="selectQueue('mine')"
          >
            {{ t('quotaReset.myRequests') }}
          </ElRadioButton>
          <ElRadioButton
            data-testid="quota-reset-tab-approvals"
            class="min-w-0 w-full [&>span]:w-full [&>span]:!whitespace-normal [&>span]:!px-1"
            value="approvals"
            @click="selectQueue('approvals')"
          >
            {{ t('quotaReset.myApprovals') }}
            <span
              v-if="approvalTotal > 0"
              data-testid="quota-reset-tab-approvals-count"
              class="ml-1 shrink-0 text-xs font-semibold tabular-nums opacity-75"
            >
              {{ countBadge(approvalTotal) }}
            </span>
          </ElRadioButton>
          <ElRadioButton
            v-if="auth.isAdmin"
            data-testid="quota-reset-tab-admin"
            class="min-w-0 w-full [&>span]:w-full [&>span]:!whitespace-normal [&>span]:!px-1"
            value="admin"
            @click="selectQueue('admin')"
          >
            {{ t('quotaReset.adminQueue') }}
            <span
              v-if="adminTotal > 0"
              data-testid="quota-reset-tab-admin-count"
              class="ml-1 shrink-0 text-xs font-semibold tabular-nums opacity-75"
            >
              {{ countBadge(adminTotal) }}
            </span>
          </ElRadioButton>
        </ElRadioGroup>

        <div class="flex flex-wrap items-center justify-between gap-3">
          <label class="flex min-w-0 flex-1 items-center gap-2 text-sm text-slate-600 sm:flex-none">
            <span class="shrink-0">{{ t('quotaReset.statusFilter') }}</span>
            <ElSelect
              v-model="activeFilter"
              data-testid="quota-reset-status-filter"
              class="flex-1 sm:w-44 sm:flex-none"
            >
              <ElOption
                v-for="filter in filters"
                :key="filter"
                :value="filter"
                :label="filterLabel(filter)"
                :data-testid="`quota-reset-filter-${filter}`"
              />
            </ElSelect>
          </label>
          <ElButton
            data-testid="quota-reset-refresh"
            :loading="currentQueue.status === 'loading'"
            :disabled="currentQueue.status === 'loading' || currentQueue.actionBusy"
            @click="refreshActiveQueue"
          >
            {{ t('quotaReset.refresh') }}
          </ElButton>
        </div>
      </section>

      <ElAlert v-if="currentQueue.error" type="error" :closable="false" :title="currentQueue.error" />

      <QuotaResetRequestList
        v-if="!currentQueue.error"
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
