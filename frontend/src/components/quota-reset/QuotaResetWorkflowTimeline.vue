<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '@/i18n'
import type {
  QuotaResetDecision,
  QuotaResetNodeStatus,
  QuotaResetWorkflow,
  QuotaResetWorkflowNode,
} from '@/types'

const props = defineProps<{
  workflow: QuotaResetWorkflow
}>()

const { t } = useI18n()

const orderedNodes = computed(() => [...props.workflow.nodes].sort((left, right) => left.position - right.position))

function nodeStatusLabel(status: QuotaResetNodeStatus) {
  switch (status) {
    case 'active':
      return t('quotaReset.node.active')
    case 'approved':
      return t('quotaReset.node.approved')
    case 'satisfied_by_prior_approval':
      return t('quotaReset.node.satisfied_by_prior_approval')
    case 'skipped_no_approver':
      return t('quotaReset.node.skipped_no_approver')
    case 'rejected':
      return t('quotaReset.node.rejected')
    default:
      return t('quotaReset.node.queued')
  }
}

function nodeDotClass(status: QuotaResetNodeStatus) {
  switch (status) {
    case 'active':
      return 'bg-cyan-700 text-white'
    case 'approved':
    case 'satisfied_by_prior_approval':
      return 'bg-emerald-100 text-emerald-800'
    case 'skipped_no_approver':
      return 'bg-amber-100 text-amber-800'
    case 'rejected':
      return 'bg-red-100 text-red-700'
    default:
      return 'bg-slate-100 text-slate-600'
  }
}

function decisionForNode(node: QuotaResetWorkflowNode) {
  return props.workflow.decisions.find((decision) => decision.node_id === node.id)
}

function reusedDecisionForNode(node: QuotaResetWorkflowNode) {
  if (!node.satisfied_by_decision_id) return undefined
  return props.workflow.decisions.find((decision) => decision.id === node.satisfied_by_decision_id)
}

function attributedDecision(node: QuotaResetWorkflowNode): QuotaResetDecision | undefined {
  if (node.status === 'satisfied_by_prior_approval') {
    return reusedDecisionForNode(node)
  }
  return decisionForNode(node)
}

function approverNames(node: QuotaResetWorkflowNode) {
  return node.approvers
    .map((approver) => approver.display_name.trim())
    .filter(Boolean)
    .join(', ')
}
</script>

<template>
  <ol
    class="divide-y divide-slate-200"
    data-testid="quota-reset-workflow-timeline"
    :aria-label="t('quotaReset.workflowTimeline')"
  >
    <li
      v-for="node in orderedNodes"
      :key="node.id"
      class="grid min-w-0 gap-2 py-3 sm:grid-cols-[2rem_minmax(0,1fr)]"
      :aria-current="node.status === 'active' ? 'step' : undefined"
    >
      <span
        class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-xs font-semibold"
        :class="nodeDotClass(node.status)"
        aria-hidden="true"
      >
        {{ node.position + 1 }}
      </span>
      <div class="min-w-0">
        <div class="flex min-w-0 flex-wrap items-center gap-2">
          <p class="min-w-0 break-words text-sm font-semibold text-slate-900">{{ node.label }}</p>
          <span class="text-xs font-medium text-slate-500">{{ nodeStatusLabel(node.status) }}</span>
          <span
            v-if="node.admin_fallback_required"
            class="rounded-full bg-amber-50 px-2 py-0.5 text-xs font-medium text-amber-800"
          >
            {{ t('quotaReset.adminFallback') }}
          </span>
          <span
            v-if="attributedDecision(node)?.admin_override"
            class="rounded-full bg-blue-50 px-2 py-0.5 text-xs font-medium text-blue-700"
          >
            {{ t('quotaReset.adminOverride') }}
          </span>
        </div>
        <p v-if="approverNames(node)" class="mt-1 break-words text-xs text-slate-500">
          {{ t('quotaReset.approvers') }}: {{ approverNames(node) }}
        </p>
        <p
          v-if="node.status === 'satisfied_by_prior_approval' && reusedDecisionForNode(node)"
          class="mt-2 whitespace-pre-wrap break-words text-sm text-emerald-700"
        >
          {{ t('quotaReset.priorApprovalAttribution', {
            actor: reusedDecisionForNode(node)!.actor_display_name,
            comment: reusedDecisionForNode(node)!.comment,
          }) }}
        </p>
        <p
          v-else-if="decisionForNode(node)"
          class="mt-2 whitespace-pre-wrap break-words text-sm text-slate-700"
        >
          {{ t('quotaReset.decisionAttribution', {
            actor: decisionForNode(node)!.actor_display_name,
            comment: decisionForNode(node)!.comment,
          }) }}
        </p>
      </div>
    </li>
  </ol>
</template>
