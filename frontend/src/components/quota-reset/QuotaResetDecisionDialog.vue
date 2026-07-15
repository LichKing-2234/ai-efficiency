<script setup lang="ts">
import { X } from '@lucide/vue'
import { computed, ref, toRef, watch } from 'vue'
import { useModalFocus } from '@/composables/useModalFocus'
import { useI18n } from '@/i18n'
import type { QuotaResetRequestSummary } from '@/types'

const props = defineProps<{
  open: boolean
  mode: 'approve' | 'reject'
  request: QuotaResetRequestSummary | null
  busy?: boolean
  restoreFocusFallback?: HTMLElement | null
}>()

const emit = defineEmits<{
  close: []
  submit: [{ request_node_id?: number; decision_reason: string }]
}>()

const { t } = useI18n()
const dialogRef = ref<HTMLElement | null>(null)
const comment = ref('')
const error = ref('')
const submitLocked = ref(false)

const isV2 = computed(() => (props.request?.workflow?.version ?? 1) >= 2)
const title = computed(() => props.mode === 'approve'
  ? t('quotaReset.approveDecisionTitle')
  : t('quotaReset.rejectDecisionTitle'))
const placeholder = computed(() => props.mode === 'approve'
  ? t('quotaReset.approveCommentPlaceholder')
  : t('quotaReset.rejectCommentPlaceholder'))
const currentNodeLabel = computed(() => props.request?.workflow?.current_node?.label || t('quotaReset.legacyRequest'))
const restoreFocusFallback = computed(() => props.restoreFocusFallback ?? null)

watch(
  () => [props.open, props.request?.id, props.mode] as const,
  () => {
    if (!props.open) return
    comment.value = ''
    error.value = ''
    submitLocked.value = false
  },
  { immediate: true },
)

watch(
  () => props.busy,
  (busy, previousBusy) => {
    if (!busy && previousBusy && props.open) {
      submitLocked.value = false
    }
  },
)

function close() {
  if (props.busy) return
  emit('close')
}

function submit() {
  if (props.busy || submitLocked.value || !props.request) return

  error.value = ''
  const value = comment.value.trim()
  if ((isV2.value || props.mode === 'reject') && !value) {
    error.value = t('quotaReset.commentRequired')
    return
  }

  const nodeID = props.request.workflow?.current_node?.id
  if (isV2.value && !nodeID) {
    error.value = t('quotaReset.workflowAdvanced')
    return
  }

  submitLocked.value = true
  emit('submit', {
    ...(nodeID ? { request_node_id: nodeID } : {}),
    decision_reason: value,
  })
}

const { handleKeydown } = useModalFocus(toRef(props, 'open'), dialogRef, {
  restoreFocusFallback,
  onClose: close,
})
</script>

<template>
  <div
    v-if="props.open && props.request"
    class="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/50 p-4"
  >
    <div
      ref="dialogRef"
      class="w-full max-w-lg rounded-lg bg-white p-5 shadow-xl"
      data-testid="quota-reset-decision-dialog"
      role="dialog"
      aria-modal="true"
      aria-labelledby="quota-reset-decision-title"
      tabindex="-1"
      @keydown="handleKeydown"
    >
      <div class="flex items-start justify-between gap-4">
        <div class="min-w-0">
          <h2 id="quota-reset-decision-title" class="break-words text-lg font-semibold text-slate-950">{{ title }}</h2>
          <p class="mt-1 break-words text-sm text-slate-500">
            {{ t('quotaReset.currentNode') }}: {{ currentNodeLabel }}
          </p>
        </div>
        <button
          type="button"
          class="flex h-9 w-9 shrink-0 items-center justify-center rounded-md text-slate-500 hover:bg-slate-100 hover:text-slate-900 disabled:opacity-50"
          :aria-label="t('app.close')"
          :title="t('app.close')"
          :disabled="props.busy"
          @click="close"
        >
          <X class="h-5 w-5" aria-hidden="true" />
        </button>
      </div>

      <label class="mt-5 block" for="quota-reset-decision-comment">
        <span class="text-sm font-medium text-slate-700">{{ t('quotaReset.comment') }}</span>
        <textarea
          id="quota-reset-decision-comment"
          v-model="comment"
          class="mt-1 min-h-28 w-full resize-y rounded-md border border-slate-300 px-3 py-2 text-sm text-slate-950 focus:border-cyan-600 focus:outline-none focus:ring-2 focus:ring-cyan-100 disabled:bg-slate-100"
          :placeholder="placeholder"
          :disabled="props.busy"
          :aria-invalid="Boolean(error)"
          :aria-describedby="error ? 'quota-reset-decision-error' : undefined"
          @input="error = ''"
        />
      </label>
      <p
        v-if="error"
        id="quota-reset-decision-error"
        class="mt-2 text-sm font-medium text-red-600"
        role="alert"
      >
        {{ error }}
      </p>

      <div class="mt-5 flex flex-wrap justify-end gap-2">
        <button
          type="button"
          data-testid="quota-reset-decision-cancel"
          class="rounded-md border border-slate-300 px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:opacity-50"
          :disabled="props.busy"
          @click="close"
        >
          {{ t('settings.cancel') }}
        </button>
        <button
          type="button"
          data-testid="quota-reset-decision-submit"
          class="rounded-md px-3 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60"
          :class="props.mode === 'approve' ? 'bg-cyan-700 hover:bg-cyan-800' : 'bg-red-700 hover:bg-red-800'"
          :disabled="props.busy || submitLocked"
          @click="submit"
        >
          {{ props.busy ? t('quotaReset.submittingDecision') : (props.mode === 'approve' ? t('quotaReset.approve') : t('quotaReset.reject')) }}
        </button>
      </div>
    </div>
  </div>
</template>
