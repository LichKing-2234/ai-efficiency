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
  <div v-if="props.open && props.row" class="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/30 p-4">
    <div class="w-full max-w-md rounded-lg bg-white p-5 shadow-xl">
      <div class="flex items-start justify-between gap-4">
        <div>
          <h2 class="text-base font-semibold text-slate-950">{{ t('teamUsage.rateMultiplier') }}</h2>
          <p class="mt-1 text-sm text-slate-500">{{ props.row.group_name }}</p>
        </div>
        <button type="button" class="text-sm font-medium text-slate-500 hover:text-slate-900" @click="emit('close')">
          {{ t('teamUsage.close') }}
        </button>
      </div>

      <div class="mt-5 inline-flex rounded-md border border-slate-300 bg-slate-50 p-1">
        <button
          type="button"
          class="rounded px-3 py-1.5 text-sm font-medium"
          :class="mode === 'set' ? 'bg-white text-slate-950 shadow-sm' : 'text-slate-600'"
          @click="mode = 'set'"
        >
          {{ t('teamUsage.setMultiplier') }}
        </button>
        <button
          type="button"
          class="rounded px-3 py-1.5 text-sm font-medium"
          :class="mode === 'reset' ? 'bg-white text-slate-950 shadow-sm' : 'text-slate-600'"
          @click="mode = 'reset'"
        >
          {{ t('teamUsage.resetMultiplier') }}
        </button>
      </div>

      <label class="mt-4 block text-sm font-medium text-slate-700" for="team-rate-multiplier">{{ t('teamUsage.multiplier') }}</label>
      <input
        id="team-rate-multiplier"
        v-model="multiplier"
        data-testid="multiplier-input"
        type="number"
        min="0"
        max="10"
        step="0.01"
        :disabled="mode === 'reset'"
        class="mt-1 h-9 w-full rounded-md border border-slate-300 px-3 text-sm text-slate-950 disabled:bg-slate-100 disabled:text-slate-500"
      />
      <p class="mt-2 text-sm text-slate-500">
        {{ t('teamUsage.multiplierHelp', { multiplier: mode === 'reset' ? props.row.inherited_default_multiplier : normalized }) }}
      </p>
      <p v-if="validationMessage" class="mt-2 text-sm text-red-600">{{ validationMessage }}</p>
      <p v-if="props.errorMessage" class="mt-2 text-sm text-red-600">{{ props.errorMessage }}</p>

      <label class="mt-4 block text-sm font-medium text-slate-700" for="team-rate-reason">{{ t('teamUsage.reason') }}</label>
      <input
        id="team-rate-reason"
        v-model="reason"
        type="text"
        class="mt-1 h-9 w-full rounded-md border border-slate-300 px-3 text-sm text-slate-950"
      />

      <div class="mt-5 flex justify-end gap-2">
        <button type="button" class="rounded-md border border-slate-300 px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50" @click="emit('close')">
          {{ t('teamUsage.cancel') }}
        </button>
        <button
          type="button"
          class="rounded-md bg-blue-600 px-3 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-60"
          :disabled="!!validationMessage || props.submitting"
          @click="confirm"
        >
          {{ props.submitting ? t('teamUsage.saving') : t('teamUsage.confirm') }}
        </button>
      </div>
    </div>
  </div>
</template>
