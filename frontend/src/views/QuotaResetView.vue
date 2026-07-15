<script setup lang="ts">
import { X } from '@lucide/vue'
import { computed, onMounted, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import AppLayout from '@/components/AppLayout.vue'
import QuotaResetDecisionDialog from '@/components/quota-reset/QuotaResetDecisionDialog.vue'
import QuotaResetRequestList from '@/components/quota-reset/QuotaResetRequestList.vue'
import QuotaResetWorkflowTimeline from '@/components/quota-reset/QuotaResetWorkflowTimeline.vue'
import UsageCenterTabs from '@/components/user/usage/UsageCenterTabs.vue'
import { useModalFocus } from '@/composables/useModalFocus'
import { useToast } from '@/composables/useToast'
import { useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import { useQuotaResetStore, type FilterMode, type QueueMode, type QuotaResetActionResult } from '@/stores/quotaReset'
import { useWorkItemsStore } from '@/stores/workItems'
import { quotaResetStatusClass, quotaResetStatusLabel } from '@/utils/quotaResetRequestStatus'
import type { QuotaResetRequestSummary } from '@/types'

const { t } = useI18n()
const { showToast } = useToast()
const auth = useAuthStore()
const workItems = useWorkItemsStore()
const quotaReset = useQuotaResetStore()
quotaReset.initialize()
const {
  activeQueue,
  activeFilter,
  myTotal,
  actionBusy,
  dataRevision,
  loading,
  loadError,
  visibleItems,
} = storeToRefs(quotaReset)

const selectedRequest = ref<QuotaResetRequestSummary | null>(null)
const requestDetailDialog = ref<HTMLElement | null>(null)
const requestDetailCloseButton = ref<HTMLElement | null>(null)
const mineQueueButton = ref<HTMLButtonElement | null>(null)
const approvalsQueueButton = ref<HTMLButtonElement | null>(null)
const adminQueueButton = ref<HTMLButtonElement | null>(null)
const decisionRequest = ref<QuotaResetRequestSummary | null>(null)
const decisionMode = ref<'approve' | 'reject'>('approve')
const decisionQueue = ref<QueueMode>('approvals')
const filters: FilterMode[] = ['all', 'pending', 'processed', 'failed']
const approvalTotal = computed(() => workItems.loading || workItems.error ? 0 : workItems.counts.quota_reset_approval_count)
const adminTotal = computed(() => workItems.loading || workItems.error || !auth.isAdmin ? 0 : workItems.counts.quota_reset_admin_count)
const requestDetailOpen = computed(() => selectedRequest.value !== null)
const decisionRestoreFocusFallback = computed(() => {
  if (decisionQueue.value === 'admin') return adminQueueButton.value
  if (decisionQueue.value === 'approvals') return approvalsQueueButton.value
  return mineQueueButton.value
})

const { handleKeydown: handleRequestDetailKeydown } = useModalFocus(
  requestDetailOpen,
  requestDetailDialog,
  {
    initialFocus: requestDetailCloseButton,
    restoreFocusFallback: mineQueueButton,
    onClose: closeRequestDetails,
  },
)

function openRequestDetails(item: QuotaResetRequestSummary) {
  selectedRequest.value = item
}

function closeRequestDetails() {
  selectedRequest.value = null
}

function findRequest(requestID: number) {
  return quotaReset.findRequest(requestID)
}

function syncSelectedRequest() {
  if (!selectedRequest.value) return
  selectedRequest.value = findRequest(selectedRequest.value.id) ?? null
}

watch(dataRevision, syncSelectedRequest)

function selectQueue(queue: QueueMode) {
  quotaReset.selectQueue(queue)
}

function selectFilter(filter: FilterMode) {
  quotaReset.selectFilter(filter)
}

function filterLabel(filter: FilterMode) {
  if (filter === 'pending') return t('quotaReset.filter.pending')
  if (filter === 'processed') return t('quotaReset.filter.processed')
  if (filter === 'failed') return t('quotaReset.filter.failed')
  return t('quotaReset.filter.all')
}

function statusLabel(status: QuotaResetRequestSummary['status']) {
  return quotaResetStatusLabel(t, status)
}

const statusClass = quotaResetStatusClass

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

function closeDecisionDialog() {
  decisionRequest.value = null
}

async function handleAction(
  action: Promise<QuotaResetActionResult>,
  options: { closeDecisionOnSuccess?: boolean } = {},
) {
  const result = await action
  if (result === 'success') {
    if (options.closeDecisionOnSuccess) closeDecisionDialog()
    showToast({ message: t('quotaReset.actionSucceeded'), tone: 'success' })
    return
  }
  if (result === 'workflow_advanced') {
    closeDecisionDialog()
    showToast({ message: t('quotaReset.workflowAdvanced'), tone: 'info' })
    return
  }
  if (result === 'failed') showToast({ message: t('quotaReset.actionFailed'), tone: 'error' })
}

function isWorkflowRequest(item: QuotaResetRequestSummary) {
  return (item.workflow?.version ?? 1) >= 2
}

function openDecisionDialog(item: QuotaResetRequestSummary, mode: 'approve' | 'reject') {
  if (actionBusy.value) return
  decisionRequest.value = item
  decisionMode.value = mode
  decisionQueue.value = activeQueue.value
}

function handleCancel(item: QuotaResetRequestSummary) {
  void handleAction(quotaReset.cancel(item.id))
}

function handleApprove(item: QuotaResetRequestSummary) {
  if (isWorkflowRequest(item)) {
    openDecisionDialog(item, 'approve')
    return
  }
  if (activeQueue.value === 'admin') {
    void handleAction(quotaReset.approve(item.id, true))
    return
  }
  void handleAction(quotaReset.approve(item.id, false))
}

function handleReject(item: QuotaResetRequestSummary) {
  openDecisionDialog(item, 'reject')
}

function handleDecisionSubmit(payload: { request_node_id?: number; decision_reason: string }) {
  const item = decisionRequest.value
  if (!item || actionBusy.value) return

  const body = payload.request_node_id !== undefined
    ? { request_node_id: payload.request_node_id, decision_reason: payload.decision_reason }
    : { decision_reason: payload.decision_reason }
  const isAdminQueue = decisionQueue.value === 'admin'
  const action = decisionMode.value === 'approve'
    ? quotaReset.approve(item.id, isAdminQueue, body)
    : quotaReset.reject(item.id, isAdminQueue, body)

  void handleAction(action, { closeDecisionOnSuccess: true })
}

function handleRetry(item: QuotaResetRequestSummary) {
  if (activeQueue.value === 'admin') {
    void handleAction(quotaReset.retry(item, true))
    return
  }
  void handleAction(quotaReset.retry(item, false))
}

onMounted(() => quotaReset.loadQueues())
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
            ref="mineQueueButton"
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
            ref="approvalsQueueButton"
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
            ref="adminQueueButton"
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

        <div data-testid="quota-reset-status-filters" class="flex w-fit max-w-full flex-wrap gap-1 rounded-full bg-slate-100 p-1">
          <button
            v-for="filter in filters"
            :key="filter"
            type="button"
            :data-testid="`quota-reset-filter-${filter}`"
            :class="filterButtonClass(activeFilter === filter)"
            @click="selectFilter(filter)"
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
        :loading="loading"
        :busy="actionBusy"
        :mode="activeQueue"
        @cancel="handleCancel"
        @approve="handleApprove"
        @reject="handleReject"
        @retry="handleRetry"
        @select="openRequestDetails"
      />

      <div
        v-if="selectedRequest"
        class="fixed inset-0 z-40 flex items-center justify-center bg-slate-950/50 p-4"
      >
        <div
          ref="requestDetailDialog"
          class="max-h-[calc(100vh-2rem)] w-full max-w-3xl overflow-y-auto rounded-lg bg-white p-5 shadow-xl"
          data-testid="quota-reset-detail-dialog"
          role="dialog"
          aria-modal="true"
          aria-labelledby="quota-reset-detail-title"
          tabindex="-1"
          @keydown="handleRequestDetailKeydown"
        >
          <div class="flex items-start justify-between gap-4">
            <div class="min-w-0">
              <h2 id="quota-reset-detail-title" class="break-words text-lg font-semibold text-slate-950">
                {{ t('quotaReset.requestDetails') }}
              </h2>
              <p class="mt-1 break-words text-sm text-slate-500">
                {{ selectedRequest.group_name || selectedRequest.group_id }}
              </p>
            </div>
            <button
              ref="requestDetailCloseButton"
              type="button"
              class="flex h-9 w-9 shrink-0 items-center justify-center rounded-md text-slate-500 hover:bg-slate-100 hover:text-slate-900"
              :aria-label="t('app.close')"
              :title="t('app.close')"
              data-testid="quota-reset-detail-close"
              @click="closeRequestDetails"
            >
              <X class="h-5 w-5" aria-hidden="true" />
            </button>
          </div>

          <dl class="mt-5 grid min-w-0 gap-x-6 gap-y-4 sm:grid-cols-2">
            <div class="min-w-0">
              <dt class="text-xs font-semibold uppercase text-slate-500">{{ t('quotaReset.requester') }}</dt>
              <dd class="mt-1 min-w-0 break-words text-sm text-slate-900">
                <span class="font-medium">{{ selectedRequest.requester_display_name || selectedRequest.requester_email }}</span>
                <span v-if="selectedRequest.requester_email" class="block text-slate-500">{{ selectedRequest.requester_email }}</span>
              </dd>
            </div>
            <div class="min-w-0">
              <dt class="text-xs font-semibold uppercase text-slate-500">{{ t('quotaReset.requesterTeams') }}</dt>
              <dd class="mt-1 text-sm text-slate-900">
                <ul v-if="selectedRequest.requester_department_paths.length" class="space-y-1">
                  <li
                    v-for="path in selectedRequest.requester_department_paths"
                    :key="path"
                    class="break-words"
                  >
                    {{ path }}
                  </li>
                </ul>
                <span v-else class="text-slate-500">{{ t('quotaReset.noRequesterTeams') }}</span>
              </dd>
            </div>
            <div class="min-w-0">
              <dt class="text-xs font-semibold uppercase text-slate-500">{{ t('quotaReset.subscriptionGroup') }}</dt>
              <dd class="mt-1 break-words text-sm text-slate-900">
                {{ selectedRequest.group_name || selectedRequest.group_id }}
                <span v-if="selectedRequest.group_platform" class="block text-slate-500">{{ selectedRequest.group_platform }}</span>
              </dd>
            </div>
            <div class="min-w-0">
              <dt class="text-xs font-semibold uppercase text-slate-500">{{ t('quotaReset.resetResult') }}</dt>
              <dd class="mt-1">
                <span class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium" :class="statusClass(selectedRequest.status)">
                  {{ statusLabel(selectedRequest.status) }}
                </span>
              </dd>
            </div>
            <div v-if="selectedRequest.workflow?.current_node" class="min-w-0 sm:col-span-2">
              <dt class="text-xs font-semibold uppercase text-slate-500">{{ t('quotaReset.currentNode') }}</dt>
              <dd class="mt-1 break-words text-sm text-slate-900">{{ selectedRequest.workflow.current_node.label }}</dd>
            </div>
            <div class="min-w-0 sm:col-span-2">
              <dt class="text-xs font-semibold uppercase text-slate-500">{{ t('quotaReset.reason') }}</dt>
              <dd class="mt-1 whitespace-pre-wrap break-words text-sm text-slate-900">{{ selectedRequest.reason }}</dd>
            </div>
            <div v-if="selectedRequest.reset_error" class="min-w-0 sm:col-span-2">
              <dt class="text-xs font-semibold uppercase text-red-600">{{ t('quotaReset.resetError') }}</dt>
              <dd class="mt-1 whitespace-pre-wrap break-words text-sm text-red-700">{{ selectedRequest.reset_error }}</dd>
            </div>
            <div v-if="selectedRequest.decision_reason && !selectedRequest.workflow" class="min-w-0 sm:col-span-2">
              <dt class="text-xs font-semibold uppercase text-slate-500">{{ t('quotaReset.comment') }}</dt>
              <dd class="mt-1 whitespace-pre-wrap break-words text-sm text-slate-900">{{ selectedRequest.decision_reason }}</dd>
            </div>
          </dl>

          <section v-if="selectedRequest.workflow" class="mt-6 border-t border-slate-200 pt-4">
            <h3 class="text-sm font-semibold text-slate-950">{{ t('quotaReset.workflowTimeline') }}</h3>
            <QuotaResetWorkflowTimeline class="mt-2" :workflow="selectedRequest.workflow" />
          </section>
        </div>
      </div>

      <QuotaResetDecisionDialog
        :open="Boolean(decisionRequest)"
        :mode="decisionMode"
        :request="decisionRequest"
        :busy="actionBusy"
        :restore-focus-fallback="decisionRestoreFocusFallback"
        @close="closeDecisionDialog"
        @submit="handleDecisionSubmit"
      />
    </div>
  </AppLayout>
</template>
