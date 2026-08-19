<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from '@/i18n'
import type { UserUsageGroupPoolUsageItem } from '@/types'

defineProps<{
  item: UserUsageGroupPoolUsageItem
}>()

const { t } = useI18n()
const now = ref(Date.now())
let resetTimer: ReturnType<typeof setInterval> | null = null

function resetDateLabel(value?: string | null) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

function resetCountdown(value?: string | null) {
  if (!value) return ''
  const timestamp = Date.parse(value)
  if (!Number.isFinite(timestamp)) return ''
  const remaining = timestamp - now.value
  if (remaining <= 0) return t('usageDashboard.resetDue')
  const minutes = Math.max(1, Math.ceil(remaining / 60_000))
  if (minutes < 60) return t('usageDashboard.resetInMinutes', { count: minutes })
  const hours = Math.ceil(minutes / 60)
  if (hours < 24) return t('usageDashboard.resetInHours', { count: hours })
  return t('usageDashboard.resetInDays', { count: Math.ceil(hours / 24) })
}

onMounted(() => {
  resetTimer = setInterval(() => {
    now.value = Date.now()
  }, 60_000)
})

onBeforeUnmount(() => {
  if (resetTimer) clearInterval(resetTimer)
  resetTimer = null
})
</script>

<template>
  <div
    class="mt-3 rounded-md border border-slate-200 bg-white px-3 py-2 text-xs text-slate-600"
    :data-testid="`usage-pool-${item.group_id}`"
  >
    <div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
      <span class="font-medium text-slate-700">{{ t('usageDashboard.poolUsageTitle') }}</span>
      <span class="text-sm font-semibold text-slate-900">{{ item.average_weekly_utilization.toFixed(1) }}%</span>
    </div>
    <p class="mt-1">{{ t('usageDashboard.poolUsageCoverage', { valid: item.valid_oauth_accounts, total: item.total_active_oauth_accounts }) }}</p>
    <p class="mt-1 text-slate-500">{{ t('usageDashboard.poolUsageHelp') }}</p>
    <p v-if="item.next_reset_at" class="mt-1">
      {{ t('usageDashboard.poolUsageReset') }}
      <time class="ml-1" :datetime="item.next_reset_at ?? undefined">{{ resetDateLabel(item.next_reset_at) }}</time>
      <span class="ml-1">({{ resetCountdown(item.next_reset_at) }})</span>
    </p>
    <p v-if="item.as_of" class="mt-1 text-slate-400">
      {{ t('usageDashboard.poolUsageAsOf', { value: resetDateLabel(item.as_of) }) }}
    </p>
  </div>
</template>
