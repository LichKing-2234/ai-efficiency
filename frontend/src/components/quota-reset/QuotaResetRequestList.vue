<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '@/i18n'
import type { QuotaResetRequestSummary, QuotaResetStatus } from '@/types'

const props = defineProps<{
  items: QuotaResetRequestSummary[]
  loading?: boolean
  mode: 'mine' | 'approvals' | 'admin'
  actorUserId?: number
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
  return props.mode === 'mine' && item.status === 'pending'
}

function canDecide(item: QuotaResetRequestSummary) {
  return item.status === 'pending' && (props.mode === 'admin'
    || (props.mode === 'approvals' && !!props.actorUserId && item.resolved_approver_user_ids.includes(props.actorUserId)))
}

function canRetry(item: QuotaResetRequestSummary) {
  return item.status === 'approved_reset_failed' && (props.mode === 'admin'
    || (props.mode === 'approvals' && !!props.actorUserId && item.approved_by_user_id === props.actorUserId))
}

function workflowProgress(item: QuotaResetRequestSummary) {
  const steps = item.workflow_steps ?? []
  if (item.workflow_version !== 2 || steps.length === 0) return ''
  const current = Math.min((item.current_step ?? 0) + 1, steps.length)
  const active = steps[item.current_step ?? 0]
  return `${current}/${steps.length}${active?.label ? ` · ${active.label}` : ''}`
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
        class="grid gap-3 p-4 md:grid-cols-[minmax(0,1fr)_auto]"
        :data-testid="`quota-reset-row-${item.id}`"
        @click="emit('select', item)"
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
          <p v-if="workflowProgress(item)" class="mt-2 break-words text-xs font-medium text-cyan-800">{{ workflowProgress(item) }}</p>
          <p v-if="item.reset_error" class="mt-2 break-words text-xs font-medium text-red-600">{{ item.reset_error }}</p>
        </div>
        <div class="flex flex-wrap items-start gap-2 md:justify-end">
          <button
            v-if="canCancel(item)"
            type="button"
            class="rounded-md border border-slate-300 px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
            :data-testid="`quota-reset-cancel-${item.id}`"
            @click.stop="emit('cancel', item)"
          >
            {{ t('quotaReset.cancelRequest') }}
          </button>
          <button
            v-if="canDecide(item)"
            type="button"
            class="rounded-md bg-cyan-700 px-3 py-2 text-sm font-medium text-white hover:bg-cyan-800"
            :data-testid="`quota-reset-approve-${item.id}`"
            @click.stop="emit('approve', item)"
          >
            {{ t('quotaReset.approve') }}
          </button>
          <button
            v-if="canDecide(item)"
            type="button"
            class="rounded-md border border-red-300 px-3 py-2 text-sm font-medium text-red-700 hover:bg-red-50"
            :data-testid="`quota-reset-reject-${item.id}`"
            @click.stop="emit('reject', item)"
          >
            {{ t('quotaReset.reject') }}
          </button>
          <button
            v-if="canRetry(item)"
            type="button"
            class="rounded-md border border-blue-300 px-3 py-2 text-sm font-medium text-blue-700 hover:bg-blue-50"
            :data-testid="`quota-reset-retry-${item.id}`"
            @click.stop="emit('retry', item)"
          >
            {{ t('quotaReset.retryReset') }}
          </button>
        </div>
      </article>
    </div>
  </div>
</template>
