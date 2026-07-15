<script setup lang="ts">
import { useI18n } from '@/i18n'
import type { QuotaResetWorkflowStep } from '@/types'

defineProps<{
  steps: QuotaResetWorkflowStep[]
  currentStep?: number
}>()

const { t } = useI18n()

function statusLabel(status: QuotaResetWorkflowStep['status']) {
  return t(`quotaReset.workflowStatus.${status}`)
}
</script>

<template>
  <ol class="divide-y divide-slate-200 border-y border-slate-200" data-testid="quota-reset-workflow-timeline">
    <li v-for="(step, index) in steps" :key="`${index}-${step.label}`" class="grid gap-2 py-3 sm:grid-cols-[2rem_minmax(0,1fr)_auto]">
      <span class="flex h-7 w-7 items-center justify-center rounded-full bg-slate-100 text-xs font-semibold text-slate-700">{{ index + 1 }}</span>
      <div class="min-w-0">
        <div class="break-words text-sm font-medium text-slate-900">{{ step.label || t('quotaReset.adminFallback') }}</div>
        <div v-if="step.decision" class="mt-1 text-sm text-slate-600">
          <span class="font-medium">{{ step.decision.actor_display_name || `User #${step.decision.actor_user_id}` }}</span>
          <span> · {{ step.decision.comment }}</span>
        </div>
        <div v-else-if="step.admin_fallback" class="mt-1 text-xs text-amber-700">{{ t('quotaReset.adminFallback') }}</div>
      </div>
      <span :class="['h-fit rounded-full px-2 py-1 text-xs font-medium', index === (currentStep ?? 0) && step.status === 'active' ? 'bg-cyan-50 text-cyan-800' : 'bg-slate-100 text-slate-600']">
        {{ statusLabel(step.status) }}
      </span>
    </li>
  </ol>
</template>
