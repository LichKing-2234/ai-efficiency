<script setup lang="ts">
import { Eye } from '@lucide/vue'
import { computed } from 'vue'
import { useI18n } from '@/i18n'
import type { QuotaResetRequestSummary, QuotaResetStatus } from '@/types'

const props = defineProps<{
  items: QuotaResetRequestSummary[]
  loading?: boolean
  busy?: boolean
  mode: 'mine' | 'approvals' | 'admin'
}>()

const emit = defineEmits<{
  cancel: [QuotaResetRequestSummary]
  approve: [QuotaResetRequestSummary]
  reject: [QuotaResetRequestSummary]
  retry: [QuotaResetRequestSummary]
  select: [QuotaResetRequestSummary]
}>()

const { t } = useI18n()

const emptyText = computed(() => {
  if (props.mode === 'mine') return t('quotaReset.noMyRequests')
  if (props.mode === 'approvals') return t('quotaReset.noApprovals')
  return t('quotaReset.noAdminRequests')
})

function statusLabel(status: QuotaResetStatus) {
  switch (status) {
    case 'approved_resetting':
      return t('quotaReset.status.approved_resetting')
    case 'approved_reset_succeeded':
      return t('quotaReset.status.approved_reset_succeeded')
    case 'approved_reset_failed':
      return t('quotaReset.status.approved_reset_failed')
    case 'rejected':
      return t('quotaReset.status.rejected')
    case 'cancelled':
      return t('quotaReset.status.cancelled')
    default:
      return t('quotaReset.status.pending')
  }
}

function statusClass(status: QuotaResetStatus) {
  if (status === 'approved_reset_succeeded') return 'bg-emerald-50 text-emerald-700'
  if (status === 'approved_reset_failed') return 'bg-red-50 text-red-700'
  if (status === 'rejected' || status === 'cancelled') return 'bg-slate-100 text-slate-600'
  if (status === 'approved_resetting') return 'bg-blue-50 text-blue-700'
  return 'bg-amber-50 text-amber-700'
}

function canCancel(item: QuotaResetRequestSummary) {
  return item.workflow?.can_cancel ?? (props.mode === 'mine' && item.status === 'pending')
}

function canApprove(item: QuotaResetRequestSummary) {
  return item.workflow?.can_approve ?? (
    (props.mode === 'approvals' || props.mode === 'admin') && item.status === 'pending'
  )
}

function canReject(item: QuotaResetRequestSummary) {
  return item.workflow?.can_reject ?? (
    (props.mode === 'approvals' || props.mode === 'admin') && item.status === 'pending'
  )
}

function canRetry(item: QuotaResetRequestSummary) {
  return item.workflow?.can_retry ?? (
    (props.mode === 'approvals' || props.mode === 'admin') && item.status === 'approved_reset_failed'
  )
}

function viewDetailsLabel(item: QuotaResetRequestSummary) {
  return t('quotaReset.viewDetails', { group: item.group_name || item.group_id })
}
</script>

<template>
  <div class="rounded-lg border border-slate-200 bg-white shadow-sm">
    <div v-if="props.loading" class="p-4 text-sm text-slate-500">{{ t('settings.loading') }}</div>
    <div v-else-if="props.items.length === 0" class="p-4 text-sm text-slate-500">{{ emptyText }}</div>
    <div v-else class="divide-y divide-slate-200">
      <article
        v-for="item in props.items"
        :key="item.id"
        class="grid min-w-0 gap-3 p-4 focus-within:bg-slate-50 hover:bg-slate-50 md:grid-cols-[minmax(0,1fr)_auto]"
        :data-testid="`quota-reset-row-${item.id}`"
      >
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <h3 class="break-words text-sm font-semibold text-slate-950">{{ item.group_name || item.group_id }}</h3>
            <span class="rounded-full px-2 py-0.5 text-xs font-medium" :class="statusClass(item.status)">
              {{ statusLabel(item.status) }}
            </span>
          </div>
          <p class="mt-1 text-xs text-slate-500">
            <span>{{ item.group_platform || '-' }}</span>
            <span v-if="item.requester_email"> · {{ item.requester_display_name || item.requester_email }}</span>
          </p>
          <p class="mt-2 line-clamp-2 break-words text-sm text-slate-700">{{ item.reason }}</p>
          <p v-if="item.reset_error" class="mt-2 break-words text-xs font-medium text-red-600">{{ item.reset_error }}</p>
        </div>
        <div class="flex flex-wrap items-start gap-2 md:justify-end">
          <button
            type="button"
            class="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border border-slate-300 text-slate-600 hover:bg-white hover:text-slate-950 disabled:cursor-not-allowed disabled:opacity-50"
            :data-testid="`quota-reset-view-details-${item.id}`"
            :aria-label="viewDetailsLabel(item)"
            :title="viewDetailsLabel(item)"
            :disabled="props.busy"
            @click="emit('select', item)"
          >
            <Eye class="h-4 w-4" aria-hidden="true" />
          </button>
          <button
            v-if="canCancel(item)"
            type="button"
            class="rounded-md border border-slate-300 px-3 py-2 text-sm font-medium text-slate-700 hover:bg-white disabled:cursor-not-allowed disabled:opacity-50"
            :data-testid="`quota-reset-cancel-${item.id}`"
            :disabled="props.busy"
            @click.stop="emit('cancel', item)"
          >
            {{ t('quotaReset.cancelRequest') }}
          </button>
          <button
            v-if="canApprove(item)"
            type="button"
            class="rounded-md bg-cyan-700 px-3 py-2 text-sm font-medium text-white hover:bg-cyan-800 disabled:cursor-not-allowed disabled:opacity-50"
            :data-testid="`quota-reset-approve-${item.id}`"
            :disabled="props.busy"
            @click.stop="emit('approve', item)"
          >
            {{ t('quotaReset.approve') }}
          </button>
          <button
            v-if="canReject(item)"
            type="button"
            class="rounded-md border border-red-300 px-3 py-2 text-sm font-medium text-red-700 hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-50"
            :data-testid="`quota-reset-reject-${item.id}`"
            :disabled="props.busy"
            @click.stop="emit('reject', item)"
          >
            {{ t('quotaReset.reject') }}
          </button>
          <button
            v-if="canRetry(item)"
            type="button"
            class="rounded-md border border-blue-300 px-3 py-2 text-sm font-medium text-blue-700 hover:bg-blue-50 disabled:cursor-not-allowed disabled:opacity-50"
            :data-testid="`quota-reset-retry-${item.id}`"
            :disabled="props.busy"
            @click.stop="emit('retry', item)"
          >
            {{ t('quotaReset.retryReset') }}
          </button>
        </div>
      </article>
    </div>
  </div>
</template>
