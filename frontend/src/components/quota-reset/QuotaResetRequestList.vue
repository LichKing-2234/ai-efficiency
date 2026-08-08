<script setup lang="ts">
import { computed } from 'vue'
import QuotaResetWorkflowTimeline from '@/components/quota-reset/QuotaResetWorkflowTimeline.vue'
import { useI18n } from '@/i18n'
import type { QuotaResetRequestSummary, QuotaResetStatus } from '@/types'

const props = defineProps<{
  items: QuotaResetRequestSummary[]
  loading?: boolean
  mode: 'mine' | 'approvals' | 'admin'
  actorUserId?: number
  selectedRequestId?: number
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

function statusType(status: QuotaResetStatus) {
  if (status === 'approved_reset_succeeded') return 'success'
  if (status === 'approved_reset_failed') return 'danger'
  if (status === 'rejected' || status === 'cancelled') return 'info'
  if (status === 'approved_resetting') return 'primary'
  return 'warning'
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

function canExpand(item: QuotaResetRequestSummary) {
  return item.workflow_version === 2 && !!item.workflow_steps?.length
}

function isSelected(item: QuotaResetRequestSummary) {
  return canExpand(item) && props.selectedRequestId === item.id
}
</script>

<template>
  <div class="rounded-lg border border-slate-200 bg-white shadow-sm">
    <div v-if="props.loading" class="p-4 text-sm text-slate-500">{{ t('settings.loading') }}</div>
    <ElEmpty v-else-if="props.items.length === 0" :description="emptyText" :image-size="72" />
    <div v-else class="divide-y divide-slate-200">
      <article
        v-for="item in props.items"
        :key="item.id"
        :class="[
          'p-3',
          isSelected(item)
            ? 'bg-cyan-50 ring-1 ring-inset ring-cyan-200'
            : canExpand(item) ? 'cursor-pointer hover:bg-slate-50' : '',
        ]"
        :data-testid="`quota-reset-row-${item.id}`"
        :aria-expanded="canExpand(item) ? isSelected(item) : undefined"
        @click="canExpand(item) && emit('select', item)"
      >
        <div class="grid gap-2 md:grid-cols-[minmax(0,1fr)_auto]">
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <h3 class="break-words text-sm font-semibold text-slate-950">{{ item.group_name || item.group_id }}</h3>
              <ElTag size="small" round :type="statusType(item.status)">
                {{ statusLabel(item.status) }}
              </ElTag>
            </div>
            <p class="mt-1 text-xs text-slate-500">
              <span>{{ item.group_platform || '-' }}</span>
              <span v-if="item.requester_email"> · {{ item.requester_display_name || item.requester_email }}</span>
            </p>
            <p :data-testid="`quota-reset-reason-${item.id}`" class="mt-2 line-clamp-1 break-words text-sm text-slate-700">{{ item.reason }}</p>
            <p v-if="workflowProgress(item)" class="mt-2 break-words text-xs font-medium text-cyan-800">{{ workflowProgress(item) }}</p>
            <p v-if="item.reset_error" class="mt-2 break-words text-xs font-medium text-red-600">{{ item.reset_error }}</p>
          </div>
          <div class="flex flex-wrap items-start gap-2 md:justify-end">
            <ElButton
              v-if="canExpand(item)"
              type="primary"
              plain
              :data-testid="`quota-reset-workflow-toggle-${item.id}`"
              :aria-expanded="isSelected(item)"
              @click.stop="emit('select', item)"
            >
              {{ t('quotaReset.workflow') }}
            </ElButton>
            <ElButton
              v-if="canCancel(item)"
              :data-testid="`quota-reset-cancel-${item.id}`"
              @click.stop="emit('cancel', item)"
            >
              {{ t('quotaReset.cancelRequest') }}
            </ElButton>
            <ElButton
              v-if="canDecide(item)"
              type="primary"
              :data-testid="`quota-reset-approve-${item.id}`"
              @click.stop="emit('approve', item)"
            >
              {{ t('quotaReset.approve') }}
            </ElButton>
            <ElButton
              v-if="canDecide(item)"
              type="danger"
              plain
              :data-testid="`quota-reset-reject-${item.id}`"
              @click.stop="emit('reject', item)"
            >
              {{ t('quotaReset.reject') }}
            </ElButton>
            <ElButton
              v-if="canRetry(item)"
              type="primary"
              plain
              :data-testid="`quota-reset-retry-${item.id}`"
              @click.stop="emit('retry', item)"
            >
              {{ t('quotaReset.retryReset') }}
            </ElButton>
          </div>
        </div>
        <div
          v-if="isSelected(item)"
          :data-testid="`quota-reset-inline-workflow-${item.id}`"
          class="border-t border-cyan-200 px-3 pb-3 pt-3"
        >
          <QuotaResetWorkflowTimeline :steps="item.workflow_steps ?? []" />
        </div>
      </article>
    </div>
  </div>
</template>
