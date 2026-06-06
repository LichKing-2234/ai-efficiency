<script setup lang="ts">
import type { UserUsageDashboardStats } from '@/types'
import { useI18n } from '@/i18n'

defineProps<{
  stats: UserUsageDashboardStats | null
}>()

const { t } = useI18n()

function formatNumber(n: number): string {
  return n.toLocaleString()
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return n.toLocaleString()
}

function formatCost(n: number): string {
  return n.toFixed(4)
}

function formatDuration(ms: number): string {
  if (ms >= 1000) return `${(ms / 1000).toFixed(2)}s`
  return `${Math.round(ms)}ms`
}
</script>

<template>
  <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
    <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-800">
      <p class="text-xs font-medium uppercase text-gray-500 dark:text-gray-400">{{ t('usageDashboard.todayCost') }}</p>
      <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-gray-100">
        ${{ formatCost(stats?.today_actual_cost ?? 0) }}
      </p>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('usageDashboard.standard') }}: ${{ formatCost(stats?.today_cost ?? 0) }}</p>
    </section>

    <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-800">
      <p class="text-xs font-medium uppercase text-gray-500 dark:text-gray-400">{{ t('usageDashboard.todayRequests') }}</p>
      <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-gray-100">
        {{ formatNumber(stats?.today_requests ?? 0) }}
      </p>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('usageDashboard.total') }}: {{ formatNumber(stats?.total_requests ?? 0) }}</p>
    </section>

    <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-800">
      <p class="text-xs font-medium uppercase text-gray-500 dark:text-gray-400">{{ t('usageDashboard.todayTokens') }}</p>
      <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-gray-100">
        {{ formatTokens(stats?.today_tokens ?? 0) }}
      </p>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
        {{ t('usageDashboard.inputShort') }} {{ formatTokens(stats?.today_input_tokens ?? 0) }} · {{ t('usageDashboard.outputShort') }} {{ formatTokens(stats?.today_output_tokens ?? 0) }} ·
        {{ t('usageDashboard.cache') }} {{ formatTokens((stats?.today_cache_creation_tokens ?? 0) + (stats?.today_cache_read_tokens ?? 0)) }}
      </p>
    </section>

    <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-800">
      <p class="text-xs font-medium uppercase text-gray-500 dark:text-gray-400">{{ t('usageDashboard.avgResponse') }}</p>
      <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-gray-100">
        {{ formatDuration(stats?.average_duration_ms ?? 0) }}
      </p>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
        {{ t('usageDashboard.rpm') }} {{ formatTokens(stats?.rpm ?? 0) }} · {{ t('usageDashboard.tpm') }} {{ formatTokens(stats?.tpm ?? 0) }}
      </p>
    </section>
  </div>
</template>
