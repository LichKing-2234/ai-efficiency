<template>
  <div class="rounded-lg bg-white p-6 shadow dark:bg-gray-800">
    <h3 class="mb-4 text-lg font-semibold text-gray-900 dark:text-gray-100">Usage Trend</h3>
    <div v-if="loading" class="flex h-64 items-center justify-center">
      <div class="h-8 w-8 animate-spin rounded-full border-4 border-blue-600 border-t-transparent"></div>
    </div>
    <div v-else-if="!data || data.length === 0" class="flex h-64 items-center justify-center text-gray-500 dark:text-gray-400">
      No trend data available
    </div>
    <div v-else class="h-64">
      <div class="flex h-full items-end space-x-1">
        <div
          v-for="point in data"
          :key="point.date"
          class="flex flex-1 flex-col items-center"
          :title="`${point.date}: ${formatTokens(point.total_tokens)} tokens`"
        >
          <div class="w-full rounded-t bg-blue-500 transition-all hover:bg-blue-600" :style="{ height: getBarHeight(point.total_tokens) + '%' }"></div>
          <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ formatDate(point.date) }}
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { UsageTrendDataPoint } from '@/types'

const props = defineProps<{
  data: UsageTrendDataPoint[]
  loading: boolean
}>()

function formatTokens(n: number): string {
  if (n >= 1_000_000) {
    return (n / 1_000_000).toFixed(2) + 'M'
  }
  if (n >= 1_000) {
    return (n / 1_000).toFixed(1) + 'K'
  }
  return n.toLocaleString()
}

function formatDate(dateStr: string): string {
  const date = new Date(dateStr)
  return `${date.getMonth() + 1}/${date.getDate()}`
}

function getBarHeight(tokens: number): number {
  if (!props.data || props.data.length === 0) return 0
  const maxTokens = Math.max(...props.data.map(p => p.total_tokens))
  if (maxTokens === 0) return 0
  return (tokens / maxTokens) * 100
}
</script>
