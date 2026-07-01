<script setup lang="ts">
import { computed } from 'vue'
import type { UserUsageDashboardStats, UserUsageTrendPoint } from '@/types'
import { useI18n } from '@/i18n'
import { formatTokenCount } from '@/utils/formatters'

const props = withDefaults(defineProps<{
  stats: UserUsageDashboardStats | null
  trend: UserUsageTrendPoint[]
  rangeLabel: string
  hideCost?: boolean
  loading?: boolean
}>(), {
  hideCost: false,
  loading: false,
})

const { t } = useI18n()

const rangeTotals = computed(() => props.trend.reduce(
  (totals, point) => ({
    requests: totals.requests + point.requests,
    inputTokens: totals.inputTokens + point.input_tokens,
    outputTokens: totals.outputTokens + point.output_tokens,
    cacheCreationTokens: totals.cacheCreationTokens + point.cache_creation_tokens,
    cacheReadTokens: totals.cacheReadTokens + point.cache_read_tokens,
    totalTokens: totals.totalTokens + point.total_tokens,
    cost: totals.cost + point.cost,
    actualCost: totals.actualCost + point.actual_cost,
  }),
  {
    requests: 0,
    inputTokens: 0,
    outputTokens: 0,
    cacheCreationTokens: 0,
    cacheReadTokens: 0,
    totalTokens: 0,
    cost: 0,
    actualCost: 0,
  },
))

function formatNumber(n: number): string {
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
  <div class="grid grid-cols-1 gap-4 sm:grid-cols-2" :class="props.hideCost ? 'xl:grid-cols-3' : 'xl:grid-cols-4'">
    <template v-if="props.loading">
      <section
        v-for="index in (props.hideCost ? 3 : 4)"
        :key="index"
        class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm"
        data-testid="usage-stats-loading"
      >
        <div class="h-3 w-20 animate-pulse rounded bg-gray-200"></div>
        <div class="mt-3 h-9 w-28 animate-pulse rounded bg-gray-200"></div>
        <div class="mt-2 h-3 w-24 animate-pulse rounded bg-gray-200"></div>
      </section>
    </template>

    <template v-else>
    <section v-if="!props.hideCost" class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
      <p class="text-xs font-medium uppercase text-gray-500">{{ t('usageDashboard.rangeCost', { range: rangeLabel }) }}</p>
      <p class="mt-2 text-2xl font-semibold text-gray-900">
        ${{ formatCost(rangeTotals.actualCost) }}
      </p>
      <p class="mt-1 text-xs text-gray-500">{{ t('usageDashboard.standard') }}: ${{ formatCost(rangeTotals.cost) }}</p>
    </section>

    <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
      <p class="text-xs font-medium uppercase text-gray-500">{{ t('usageDashboard.rangeRequests', { range: rangeLabel }) }}</p>
      <p class="mt-2 text-2xl font-semibold text-gray-900">
        {{ formatNumber(rangeTotals.requests) }}
      </p>
      <p class="mt-1 text-xs text-gray-500">{{ t('usageDashboard.selectedRange') }}</p>
    </section>

    <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
      <p class="text-xs font-medium uppercase text-gray-500">{{ t('usageDashboard.rangeTokens', { range: rangeLabel }) }}</p>
      <p class="mt-2 text-2xl font-semibold text-gray-900">
        {{ formatTokenCount(rangeTotals.totalTokens) }}
      </p>
      <p class="mt-1 text-xs text-gray-500">
        {{ t('usageDashboard.inputShort') }} {{ formatTokenCount(rangeTotals.inputTokens) }} · {{ t('usageDashboard.outputShort') }} {{ formatTokenCount(rangeTotals.outputTokens) }} ·
        {{ t('usageDashboard.cache') }} {{ formatTokenCount(rangeTotals.cacheCreationTokens + rangeTotals.cacheReadTokens) }}
      </p>
    </section>

    <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
      <p class="text-xs font-medium uppercase text-gray-500">{{ t('usageDashboard.avgResponse') }}</p>
      <p class="mt-2 text-2xl font-semibold text-gray-900">
        {{ formatDuration(stats?.average_duration_ms ?? 0) }}
      </p>
      <p class="mt-1 text-xs text-gray-500">
        {{ t('usageDashboard.rpm') }} {{ formatTokenCount(stats?.rpm ?? 0) }} · {{ t('usageDashboard.tpm') }} {{ formatTokenCount(stats?.tpm ?? 0) }}
      </p>
    </section>
    </template>
  </div>
</template>
