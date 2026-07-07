<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
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
import type { QuotaResetRequestSummary, QuotaResetStatus } from '@/types'

const { t } = useI18n()
const { showToast } = useToast()
const auth = useAuthStore()

type QueueMode = 'mine' | 'approvals' | 'admin'
type FilterMode = 'all' | 'pending' | 'processed' | 'failed'

const activeQueue = ref<QueueMode>('mine')
const activeFilter = ref<FilterMode>('all')
const myRequests = ref<QuotaResetRequestSummary[]>([])
const approvalRequests = ref<QuotaResetRequestSummary[]>([])
const adminRequests = ref<QuotaResetRequestSummary[]>([])
const loading = ref(false)
const actionBusy = ref(false)
const loadError = ref('')
const filters: FilterMode[] = ['all', 'pending', 'processed', 'failed']

const queueItems = computed(() => {
  if (activeQueue.value === 'approvals') return approvalRequests.value
  if (activeQueue.value === 'admin') return adminRequests.value
  return myRequests.value
})

const visibleItems = computed(() => queueItems.value.filter((item) => filterMatches(item.status, activeFilter.value)))

async function loadQueues() {
  loading.value = true
  loadError.value = ''
  try {
    const requests = [
      listMyQuotaResetRequests(),
      listQuotaResetApprovals(),
    ] as const
    const [mine, approvals] = await Promise.all(requests)
    myRequests.value = mine.data.data?.items ?? []
    approvalRequests.value = approvals.data.data?.items ?? []
    if (auth.isAdmin) {
      const admin = await listAdminQuotaResetRequests()
      adminRequests.value = admin.data.data?.items ?? []
    } else {
      adminRequests.value = []
    }
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

async function withAction(action: () => Promise<unknown>) {
  actionBusy.value = true
  try {
    await action()
    await loadQueues()
    showToast({ message: t('quotaReset.actionSucceeded'), tone: 'success' })
  } catch {
    showToast({ message: t('quotaReset.actionFailed'), tone: 'error' })
  } finally {
    actionBusy.value = false
  }
}

function rejectReason() {
  return window.prompt(t('quotaReset.rejectPrompt'))?.trim() ?? ''
}

function handleCancel(item: QuotaResetRequestSummary) {
  void withAction(() => cancelQuotaResetRequest(item.id))
}

function handleApprove(item: QuotaResetRequestSummary) {
  if (activeQueue.value === 'admin') {
    void withAction(() => adminApproveQuotaResetRequest(item.id, {}))
    return
  }
  void withAction(() => approveQuotaResetRequest(item.id, {}))
}

function handleReject(item: QuotaResetRequestSummary) {
  const decisionReason = rejectReason()
  if (!decisionReason) return
  if (activeQueue.value === 'admin') {
    void withAction(() => adminRejectQuotaResetRequest(item.id, { decision_reason: decisionReason }))
    return
  }
  void withAction(() => rejectQuotaResetRequest(item.id, { decision_reason: decisionReason }))
}

function handleRetry(item: QuotaResetRequestSummary) {
  if (activeQueue.value === 'admin') {
    void withAction(() => adminRetryQuotaResetRequest(item.id))
    return
  }
  void withAction(() => retryQuotaResetRequest(item.id))
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

      <div class="flex flex-wrap gap-2">
        <button
          type="button"
          data-testid="quota-reset-tab-mine"
          :class="['rounded-md px-3 py-2 text-sm font-medium', activeQueue === 'mine' ? 'bg-slate-900 text-white' : 'border border-slate-300 bg-white text-slate-700']"
          @click="activeQueue = 'mine'"
        >
          {{ t('quotaReset.myRequests') }}
        </button>
        <button
          type="button"
          data-testid="quota-reset-tab-approvals"
          :class="['rounded-md px-3 py-2 text-sm font-medium', activeQueue === 'approvals' ? 'bg-slate-900 text-white' : 'border border-slate-300 bg-white text-slate-700']"
          @click="activeQueue = 'approvals'"
        >
          {{ t('quotaReset.myApprovals') }}
        </button>
        <button
          v-if="auth.isAdmin"
          type="button"
          data-testid="quota-reset-tab-admin"
          :class="['rounded-md px-3 py-2 text-sm font-medium', activeQueue === 'admin' ? 'bg-slate-900 text-white' : 'border border-slate-300 bg-white text-slate-700']"
          @click="activeQueue = 'admin'"
        >
          {{ t('quotaReset.adminQueue') }}
        </button>
      </div>

      <div class="flex flex-wrap gap-2">
        <button
          v-for="filter in filters"
          :key="filter"
          type="button"
          :class="['rounded-md px-3 py-1.5 text-sm font-medium', activeFilter === filter ? 'bg-cyan-700 text-white' : 'border border-slate-300 bg-white text-slate-700']"
          @click="activeFilter = filter"
        >
          {{ filterLabel(filter) }}
        </button>
      </div>

      <div v-if="loadError" class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700">
        {{ loadError }}
      </div>

      <QuotaResetRequestList
        :items="visibleItems"
        :loading="loading || actionBusy"
        :mode="activeQueue"
        @cancel="handleCancel"
        @approve="handleApprove"
        @reject="handleReject"
        @retry="handleRetry"
      />
    </div>
  </AppLayout>
</template>
