<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import AppLayout from '@/components/AppLayout.vue'
import QuotaResetRequestList from '@/components/quota-reset/QuotaResetRequestList.vue'
import QuotaResetDecisionDialog from '@/components/quota-reset/QuotaResetDecisionDialog.vue'
import QuotaResetWorkflowTimeline from '@/components/quota-reset/QuotaResetWorkflowTimeline.vue'
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

type QueueMode = 'mine' | 'approvals' | 'admin'
type FilterMode = 'all' | 'pending' | 'processed' | 'failed'

const activeQueue = ref<QueueMode>('mine')
const activeFilter = ref<FilterMode>('all')
const myRequests = ref<QuotaResetRequestSummary[]>([])
const approvalRequests = ref<QuotaResetRequestSummary[]>([])
const adminRequests = ref<QuotaResetRequestSummary[]>([])
const myTotal = ref(0)
const loading = ref(false)
const actionBusy = ref(false)
const selectedRequest = ref<QuotaResetRequestSummary | null>(null)
const decisionRequest = ref<QuotaResetRequestSummary | null>(null)
const decisionAction = ref<'approve' | 'reject'>('approve')
const loadError = ref('')
const filters: FilterMode[] = ['all', 'pending', 'processed', 'failed']
const queuePageSize = 100
const approvalTotal = computed(() => workItems.loading || workItems.error ? 0 : workItems.counts.quota_reset_approval_count)
const adminTotal = computed(() => workItems.loading || workItems.error || !auth.isAdmin ? 0 : workItems.counts.quota_reset_admin_count)

const queueItems = computed(() => {
  if (activeQueue.value === 'approvals') return approvalRequests.value
  if (activeQueue.value === 'admin') return adminRequests.value
  return myRequests.value
})

const visibleItems = computed(() => queueItems.value.filter((item) => filterMatches(item.status, activeFilter.value)))

async function loadAllQueuePages(loader: (params: QuotaResetListParams) => ReturnType<typeof listMyQuotaResetRequests>) {
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
async function loadQueues(forceCounts = false) {
  loading.value = true
  loadError.value = ''
  void workItems.loadCounts({ force: forceCounts })
  try {
    const [mine, approvals] = await Promise.all([
      loadAllQueuePages(listMyQuotaResetRequests),
      loadAllQueuePages(listQuotaResetApprovals),
    ])
    myRequests.value = mine.items
    approvalRequests.value = approvals.items
    myTotal.value = mine.total
    adminRequests.value = auth.isAdmin
      ? (await loadAllQueuePages(listAdminQuotaResetRequests)).items
      : []
  } catch {
    loadError.value = t('quotaReset.loadFailed')
  } finally {
    loading.value = false
  }
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

async function withAction(action: () => Promise<unknown>) {
  actionBusy.value = true
  try {
    await action()
    await loadQueues(true)
    showToast({ message: t('quotaReset.actionSucceeded'), tone: 'success' })
    return true
  } catch {
    showToast({ message: t('quotaReset.actionFailed'), tone: 'error' })
    return false
  } finally {
    actionBusy.value = false
  }
}

function handleCancel(item: QuotaResetRequestSummary) {
  void withAction(() => cancelQuotaResetRequest(item.id))
}

function handleDecision(item: QuotaResetRequestSummary, action: 'approve' | 'reject') {
  decisionRequest.value = item
  decisionAction.value = action
}

function submitDecision(item: QuotaResetRequestSummary, comment: string) {
  const submit = decisionAction.value === 'approve'
    ? (activeQueue.value === 'admin' ? adminApproveQuotaResetRequest : approveQuotaResetRequest)
    : (activeQueue.value === 'admin' ? adminRejectQuotaResetRequest : rejectQuotaResetRequest)
  return submit(item.id, { decision_reason: comment })
}

async function confirmDecision(comment: string) {
  const item = decisionRequest.value
  if (!item) return
  if (await withAction(() => submitDecision(item, comment))) {
    decisionRequest.value = null
    selectedRequest.value = queueItems.value.find((request) => request.id === item.id) ?? null
  }
}

function handleSelect(item: QuotaResetRequestSummary) {
  selectedRequest.value = selectedRequest.value?.id === item.id ? null : item
}

function handleRetry(item: QuotaResetRequestSummary) {
  const retry = activeQueue.value === 'admin' ? adminRetryQuotaResetRequest : retryQuotaResetRequest
  void withAction(() => retry(item.id))
}

onMounted(loadQueues)
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
            @click="activeQueue = 'mine'"
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
            @click="activeQueue = 'approvals'"
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
            @click="activeQueue = 'admin'"
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
      </section>

      <div v-if="loadError" class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700">
        {{ loadError }}
      </div>

      <QuotaResetRequestList
        :items="visibleItems"
        :loading="loading || actionBusy"
        :mode="activeQueue"
        :actor-user-id="auth.user?.id"
        @cancel="handleCancel"
        @approve="handleDecision($event, 'approve')"
        @reject="handleDecision($event, 'reject')"
        @retry="handleRetry"
        @select="handleSelect"
      />
      <section v-if="selectedRequest?.workflow_version === 2 && selectedRequest.workflow_steps?.length" class="space-y-3" aria-label="Quota reset workflow details">
        <div>
          <h2 class="text-base font-semibold text-slate-950">{{ t('quotaReset.workflow') }}</h2>
          <p class="mt-1 text-sm text-slate-600">{{ selectedRequest.group_name || selectedRequest.group_id }}</p>
        </div>
        <QuotaResetWorkflowTimeline :steps="selectedRequest.workflow_steps" />
      </section>
    </div>
    <QuotaResetDecisionDialog
      v-if="decisionRequest"
      :action="decisionAction"
      :busy="actionBusy"
      @confirm="confirmDecision"
      @cancel="decisionRequest = null"
    />
  </AppLayout>
</template>
