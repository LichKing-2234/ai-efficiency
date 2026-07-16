<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from '@/i18n'
const props = defineProps<{
  open: boolean
  action: 'approve' | 'reject'
  busy?: boolean
}>()
const emit = defineEmits<{
  confirm: [comment: string]
  cancel: []
}>()
const { t } = useI18n()
const comment = ref('')
const canSubmit = computed(() => comment.value.trim().length > 0 && !props.busy)
watch(() => props.open, (open) => {
  if (open) comment.value = ''
})
function confirm() {
  if (canSubmit.value) emit('confirm', comment.value.trim())
}
</script>
<template>
  <div v-if="open" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" data-testid="quota-reset-decision-dialog" @click.self="emit('cancel')">
    <div class="w-full max-w-md rounded-lg bg-white p-5 shadow-xl" role="dialog" aria-modal="true" :aria-label="t(action === 'approve' ? 'quotaReset.approveTitle' : 'quotaReset.rejectTitle')">
      <h2 class="text-base font-semibold text-slate-950">
        {{ t(action === 'approve' ? 'quotaReset.approveTitle' : 'quotaReset.rejectTitle') }}
      </h2>
      <label class="mt-4 block">
        <span class="text-sm font-medium text-slate-700">{{ t('quotaReset.decisionComment') }}</span>
        <textarea v-model="comment" data-testid="quota-reset-decision-comment" rows="4" class="mt-1 w-full resize-y rounded-md border border-slate-300 px-3 py-2 text-sm" :placeholder="t('quotaReset.decisionCommentPlaceholder')" />
      </label>
      <div class="mt-4 flex justify-end gap-2">
        <button type="button" class="rounded-md border border-slate-300 px-3 py-2 text-sm font-medium text-slate-700" :disabled="busy" @click="emit('cancel')">
          {{ t('settings.cancel') }}
        </button>
        <button type="button" data-testid="quota-reset-decision-confirm" :class="['rounded-md px-3 py-2 text-sm font-medium text-white disabled:opacity-50', action === 'approve' ? 'bg-cyan-700' : 'bg-red-700']" :disabled="!canSubmit" @click="confirm">
          {{ busy ? t('settings.saving') : t(action === 'approve' ? 'quotaReset.approve' : 'quotaReset.reject') }}
        </button>
      </div>
    </div>
  </div>
</template>
