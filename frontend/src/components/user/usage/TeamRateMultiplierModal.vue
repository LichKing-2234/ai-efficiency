<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { SubjectSubscriptionGroup, UpdateTeamUsageRateMultiplierRequest } from '@/types'
import { useI18n } from '@/i18n'

const props = defineProps<{
  row: SubjectSubscriptionGroup | null
  open: boolean
  submitting?: boolean
  errorMessage?: string
}>()

const emit = defineEmits<{
  close: []
  confirm: [payload: UpdateTeamUsageRateMultiplierRequest]
}>()

const mode = ref<'set' | 'reset'>('set')
const multiplier = ref('')
const reason = ref('')
const { t } = useI18n()

const normalized = computed(() => Number(multiplier.value))
const isSetMode = computed(() => mode.value === 'set')
const validationMessage = computed(() => {
  if (!isSetMode.value || !props.row) return ''
  if (!Number.isFinite(normalized.value) || normalized.value <= 0) return t('teamUsage.invalidMultiplier')
  if (!/^\d+(\.\d{1,2})?$/.test(String(multiplier.value))) return t('teamUsage.tooManyDecimals')
  if (normalized.value > 10) return t('teamUsage.aboveMaximum')
  return ''
})

watch(
  () => [props.open, props.row?.group_id] as const,
  () => {
    if (!props.open || !props.row) return
    mode.value = 'set'
    multiplier.value = String(props.row.user_multiplier ?? props.row.effective_multiplier)
    reason.value = ''
  },
)

function confirm() {
  if (validationMessage.value) return
  if (mode.value === 'reset') {
    emit('confirm', { mode: 'reset', reason: reason.value.trim() || undefined })
    return
  }
  emit('confirm', {
    mode: 'set',
    rate_multiplier: normalized.value,
    reason: reason.value.trim() || undefined,
  })
}
</script>

<template>
  <ElDialog
    v-if="props.row"
    :model-value="props.open"
    append-to-body
    align-center
    width="min(28rem, calc(100vw - 2rem))"
    :title="t('teamUsage.rateMultiplier')"
    @close="emit('close')"
  >
      <div class="flex items-start justify-between gap-4">
        <div>
          <p class="mt-1 text-sm text-slate-500">{{ props.row.group_name }}</p>
        </div>
      </div>

      <ElRadioGroup v-model="mode" class="mt-5">
        <ElRadioButton value="set">{{ t('teamUsage.setMultiplier') }}</ElRadioButton>
        <ElRadioButton value="reset">{{ t('teamUsage.resetMultiplier') }}</ElRadioButton>
      </ElRadioGroup>

      <label class="mt-4 block text-sm font-medium text-slate-700" for="team-rate-multiplier">{{ t('teamUsage.multiplier') }}</label>
      <ElInput
        id="team-rate-multiplier"
        v-model="multiplier"
        data-testid="multiplier-input"
        type="number"
        :disabled="mode === 'reset'"
        class="mt-1 w-full"
      />
      <p class="mt-2 text-sm text-slate-500">
        {{ t('teamUsage.multiplierHelp', { multiplier: mode === 'reset' ? props.row.inherited_default_multiplier : normalized }) }}
      </p>
      <p v-if="validationMessage" class="mt-2 text-sm text-red-600">{{ validationMessage }}</p>
      <ElAlert v-if="props.errorMessage" class="mt-2" type="error" :closable="false" :title="props.errorMessage" />

      <label class="mt-4 block text-sm font-medium text-slate-700" for="team-rate-reason">{{ t('teamUsage.reason') }}</label>
      <ElInput
        id="team-rate-reason"
        v-model="reason"
        type="text"
        class="mt-1 w-full"
      />

      <template #footer>
        <ElButton @click="emit('close')">
          {{ t('teamUsage.cancel') }}
        </ElButton>
        <ElButton
          type="primary"
          :loading="props.submitting"
          :disabled="!!validationMessage || props.submitting"
          @click="confirm"
        >
          {{ props.submitting ? t('teamUsage.saving') : t('teamUsage.confirm') }}
        </ElButton>
      </template>
  </ElDialog>
</template>
