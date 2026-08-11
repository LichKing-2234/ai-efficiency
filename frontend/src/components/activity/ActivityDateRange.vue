<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from '@/i18n'
import { activityV2Text } from './activityV2Text'
import type { ActivityWindowParams } from '@/types/activity'

const props = defineProps<{ from?: string; to?: string; loading?: boolean }>()
const emit = defineEmits<{ change: [ActivityWindowParams]; refresh: [] }>()
const { locale, t } = useI18n()
const preset = ref<'7' | '30' | '90' | 'custom'>('30')
const customFrom = ref(dateInput(props.from))
const customTo = ref(dateInput(props.to))
const customError = computed(() => {
  if (!customFrom.value || !customTo.value) return activityV2Text(locale.value, 'customRangeRequired')
  if (customFrom.value > customTo.value) return activityV2Text(locale.value, 'customRangeOrder')
  const from = new Date(`${customFrom.value}T12:00:00`)
  const to = new Date(`${customTo.value}T12:00:00`)
  const days = Math.round((to.getTime() - from.getTime()) / 86_400_000) + 1
  return days > 90 ? activityV2Text(locale.value, 'customRangeMaximum') : ''
})

function localDate(value = new Date()) {
  const year = value.getFullYear()
  const month = String(value.getMonth() + 1).padStart(2, '0')
  const day = String(value.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function dateInput(value?: string) {
  return value ? value.slice(0, 10) : localDate()
}

function select(days: 7 | 30 | 90) {
  preset.value = String(days) as '7' | '30' | '90'
  const to = new Date()
  const from = new Date(to.getFullYear(), to.getMonth(), to.getDate())
  from.setDate(from.getDate() - days + 1)
  customFrom.value = localDate(from)
  customTo.value = localDate(to)
  emit('change', { from: customFrom.value, to: customTo.value })
}

function applyCustom() {
  if (customError.value) return
  emit('change', { from: customFrom.value, to: customTo.value })
}

watch(() => [props.from, props.to], () => {
  customFrom.value = dateInput(props.from)
  customTo.value = dateInput(props.to)
})
</script>

<template>
  <div data-testid="activity-date-range" class="w-full min-w-0 lg:w-auto">
    <div class="flex min-w-0 flex-wrap items-center gap-2 lg:justify-end" aria-label="Activity date range">
      <ElRadioGroup v-model="preset" class="!flex !flex-wrap">
        <ElRadioButton
          v-for="days in ([7, 30, 90] as const)"
          :key="days"
          :value="String(days)"
          :data-testid="`activity-range-${days}`"
          @click="select(days)"
        >
          {{ t(`activity.range${days}` as 'activity.range7') }}
        </ElRadioButton>
        <ElRadioButton value="custom" data-testid="activity-range-custom" @click="preset = 'custom'">
          {{ t('activity.custom') }}
        </ElRadioButton>
      </ElRadioGroup>
      <ElButton
        data-testid="activity-range-refresh"
        type="primary"
        class="min-h-10 !ml-0"
        :loading="loading"
        @click="emit('refresh')"
      >
        {{ t('activity.refresh') }}
      </ElButton>
    </div>
    <div
      v-if="preset === 'custom'"
      data-testid="activity-custom-panel"
      class="mt-3 grid w-full gap-3 rounded-xl border border-slate-200 bg-white p-3 sm:grid-cols-2 lg:grid-cols-[minmax(0,12rem)_minmax(0,12rem)_auto] lg:items-end"
    >
      <label class="min-w-0 text-xs font-medium text-slate-600">
        {{ t('activity.from') }}
        <ElInput v-model="customFrom" data-testid="activity-custom-from" type="date" class="mt-1" />
      </label>
      <label class="min-w-0 text-xs font-medium text-slate-600">
        {{ t('activity.to') }}
        <ElInput v-model="customTo" data-testid="activity-custom-to" type="date" class="mt-1" />
      </label>
      <ElButton
        data-testid="activity-range-apply"
        type="primary"
        class="min-h-10 w-full !ml-0 sm:col-span-2 lg:col-span-1 lg:w-auto"
        :disabled="Boolean(customError)"
        @click="applyCustom"
      >
        {{ t('activity.apply') }}
      </ElButton>
      <p v-if="customError" role="alert" class="text-sm text-red-600 sm:col-span-2 lg:col-span-3">{{ customError }}</p>
    </div>
  </div>
</template>
