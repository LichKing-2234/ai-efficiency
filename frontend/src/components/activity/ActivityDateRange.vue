<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from '@/i18n'
import type { ActivityWindowParams } from '@/types/activity'

const props = defineProps<{ from?: string; to?: string; loading?: boolean }>()
const emit = defineEmits<{ change: [ActivityWindowParams]; refresh: [] }>()
const { t } = useI18n()
const preset = ref<'7' | '30' | '90' | 'custom'>('30')
const customFrom = ref(dateInput(props.from))
const customTo = ref(dateInput(props.to))

function dateInput(value?: string) {
  return value ? value.slice(0, 10) : new Date().toISOString().slice(0, 10)
}

function select(days: 7 | 30 | 90) {
  preset.value = String(days) as '7' | '30' | '90'
  const to = new Date()
  const from = new Date(to.getTime() - days * 24 * 60 * 60 * 1000)
  emit('change', { from: from.toISOString(), to: to.toISOString() })
}

function applyCustom() {
  const from = new Date(`${customFrom.value}T00:00:00`)
  const to = new Date(`${customTo.value}T00:00:00`)
  to.setDate(to.getDate() + 1)
  emit('change', { from: from.toISOString(), to: to.toISOString() })
}
</script>

<template>
  <div class="flex min-w-0 flex-wrap items-center gap-2" aria-label="Activity date range">
    <button
      v-for="days in ([7, 30, 90] as const)"
      :key="days"
      type="button"
      :data-testid="`activity-range-${days}`"
      class="min-h-10 rounded-lg border px-3 py-2 text-sm font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-600 focus-visible:ring-offset-2"
      :class="preset === String(days) ? 'border-cyan-700 bg-cyan-50 text-cyan-900' : 'border-slate-300 bg-white text-slate-700 hover:bg-slate-50'"
      @click="select(days)"
    >
      {{ t(`activity.range${days}` as 'activity.range7') }}
    </button>
    <button type="button" class="min-h-10 rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-700" @click="preset = 'custom'">
      {{ t('activity.custom') }}
    </button>
    <button type="button" class="min-h-10 rounded-lg bg-cyan-700 px-3 py-2 text-sm font-medium text-white hover:bg-cyan-800 disabled:opacity-50" :disabled="loading" @click="emit('refresh')">
      {{ t('activity.refresh') }}
    </button>
  </div>
  <div v-if="preset === 'custom'" class="mt-3 flex w-full flex-wrap items-end gap-2 rounded-xl border border-slate-200 bg-white p-3">
    <label class="text-xs font-medium text-slate-600">{{ t('activity.from') }}<input v-model="customFrom" type="date" class="mt-1 block min-h-10 rounded-lg border border-slate-300 px-3 text-sm" /></label>
    <label class="text-xs font-medium text-slate-600">{{ t('activity.to') }}<input v-model="customTo" type="date" class="mt-1 block min-h-10 rounded-lg border border-slate-300 px-3 text-sm" /></label>
    <button type="button" class="min-h-10 rounded-lg bg-cyan-700 px-4 text-sm font-medium text-white" @click="applyCustom">{{ t('activity.apply') }}</button>
  </div>
</template>
