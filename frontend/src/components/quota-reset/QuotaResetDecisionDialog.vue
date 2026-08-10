<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from '@/i18n'
const props = defineProps<{
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
</script>
<template>
  <ElDialog
    :model-value="true"
    append-to-body
    width="min(28rem, calc(100vw - 2rem))"
    :title="t(action === 'approve' ? 'quotaReset.approveTitle' : 'quotaReset.rejectTitle')"
    data-testid="quota-reset-decision-dialog"
    @close="emit('cancel')"
  >
    <form role="dialog" :aria-label="t(action === 'approve' ? 'quotaReset.approveTitle' : 'quotaReset.rejectTitle')" @submit.prevent="canSubmit && emit('confirm', comment.trim())">
      <label class="mt-4 block">
        <span class="text-sm font-medium text-slate-700">{{ t('quotaReset.decisionComment') }}</span>
        <ElInput
          v-model="comment"
          data-testid="quota-reset-decision-comment"
          type="textarea"
          :rows="4"
          class="mt-1 w-full"
          :placeholder="t('quotaReset.decisionCommentPlaceholder')"
        />
      </label>
      <div class="mt-4 flex justify-end gap-2">
        <ElButton :disabled="busy" @click="emit('cancel')">
          {{ t('settings.cancel') }}
        </ElButton>
        <ElButton
          native-type="submit"
          data-testid="quota-reset-decision-confirm"
          :type="action === 'approve' ? 'primary' : 'danger'"
          :loading="busy"
          :disabled="!canSubmit"
        >
          {{ busy ? t('settings.saving') : t(action === 'approve' ? 'quotaReset.approve' : 'quotaReset.reject') }}
        </ElButton>
      </div>
    </form>
  </ElDialog>
</template>
