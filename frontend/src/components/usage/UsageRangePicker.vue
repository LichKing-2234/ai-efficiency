<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '@/i18n'
import { customSelection, formatDateInput, presetSelection } from '@/utils/usageRange'
import type { UsageRangeSelection } from '@/utils/usageRange'
import type { UsageWindow } from '@/utils/usageWindowPreference'

const props = withDefaults(defineProps<{
  modelValue: UsageRangeSelection
  disabled?: boolean
}>(), {
  disabled: false,
})

const emit = defineEmits<{
  'update:modelValue': [value: UsageRangeSelection]
}>()

const { t } = useI18n()
const today = computed(() => formatDateInput(new Date()))

let lastEmitted = ''

function selectionKey(selection: UsageRangeSelection) {
  return `${selection.range}|${selection.startDate}|${selection.endDate}`
}

// A radio click reaches us twice (button click plus group change). Collapse the
// duplicate, but let a later re-click of the same preset request a refresh.
function emitSelection(next: UsageRangeSelection) {
  const key = selectionKey(next)
  if (key === lastEmitted) return
  lastEmitted = key
  emit('update:modelValue', next)
  setTimeout(() => {
    if (lastEmitted === key) lastEmitted = ''
  }, 0)
}

function selectPreset(range: UsageWindow) {
  emitSelection(presetSelection(range))
}

function handleGroupChange(value: string | number | boolean | undefined) {
  if (value === 'today' || value === '7d' || value === '30d') selectPreset(value)
}

function changeDate(which: 'start' | 'end', event: Event) {
  const value = (event.target as HTMLInputElement).value
  const current = props.modelValue
  const startDate = which === 'start' ? value : current.startDate
  const endDate = which === 'end' ? value : current.endDate
  if (!startDate || !endDate) return
  emitSelection(customSelection(startDate, endDate))
}
</script>

<template>
  <div class="flex max-w-full shrink-0 flex-wrap items-center gap-2 sm:flex-nowrap">
    <ElRadioGroup
      :model-value="modelValue.range"
      :disabled="props.disabled"
      class="max-w-full shrink-0 flex-wrap sm:flex-nowrap sm:overflow-x-auto"
      @change="handleGroupChange"
    >
      <ElRadioButton data-test="range-today" value="today" @click="selectPreset('today')">
        {{ t('usageDashboard.today') }}
      </ElRadioButton>
      <ElRadioButton data-test="range-7d" value="7d" @click="selectPreset('7d')">
        {{ t('usageDashboard.sevenDays') }}
      </ElRadioButton>
      <ElRadioButton data-test="range-30d" value="30d" @click="selectPreset('30d')">
        {{ t('usageDashboard.thirtyDays') }}
      </ElRadioButton>
    </ElRadioGroup>
    <div class="flex shrink-0 items-center gap-1">
      <input
        data-test="range-custom-start"
        type="date"
        :value="modelValue.startDate"
        :max="today"
        :disabled="props.disabled"
        :aria-label="t('activity.from')"
        class="rounded border border-gray-300 px-2 py-1 text-sm"
        @change="changeDate('start', $event)"
      >
      <input
        data-test="range-custom-end"
        type="date"
        :value="modelValue.endDate"
        :max="today"
        :disabled="props.disabled"
        :aria-label="t('activity.to')"
        class="rounded border border-gray-300 px-2 py-1 text-sm"
        @change="changeDate('end', $event)"
      >
    </div>
  </div>
</template>
