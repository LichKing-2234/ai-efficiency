<template>
  <div class="rounded-lg bg-white p-6 shadow dark:bg-gray-800">
    <h3 class="mb-4 text-lg font-semibold text-gray-900 dark:text-gray-100">Model Distribution</h3>
    <div v-if="loading" class="flex h-64 items-center justify-center">
      <div class="h-8 w-8 animate-spin rounded-full border-4 border-purple-600 border-t-transparent"></div>
    </div>
    <div v-else-if="!data || data.length === 0" class="flex h-64 items-center justify-center text-gray-500 dark:text-gray-400">
      No model data available
    </div>
    <div v-else class="space-y-3">
      <div v-for="model in data" :key="model.model" class="flex items-center justify-between">
        <div class="flex items-center space-x-2">
          <div class="h-3 w-3 rounded-full" :style="{ backgroundColor: getModelColor(model.model) }"></div>
          <span class="text-sm font-medium text-gray-900 dark:text-gray-100">{{ model.model }}</span>
        </div>
        <div class="text-right">
          <div class="text-sm font-semibold text-gray-900 dark:text-gray-100">
            {{ formatTokens(model.total_tokens) }}
          </div>
          <div class="text-xs text-gray-500 dark:text-gray-400">
            {{ getPercentage(model.total_tokens) }}% · ${{ model.actual_cost.toFixed(4) }}
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { UsageModelStat } from '@/types'

const props = defineProps<{
  data: UsageModelStat[]
  loading: boolean
}>()

const colors = [
  '#3b82f6', // blue
  '#10b981', // green
  '#8b5cf6', // purple
  '#f59e0b', // amber
  '#ef4444', // red
  '#06b6d4', // cyan
  '#ec4899', // pink
  '#84cc16', // lime
]

function getModelColor(model: string): string {
  let hash = 0
  for (let i = 0; i < model.length; i++) {
    hash = model.charCodeAt(i) + ((hash << 5) - hash)
  }
  return colors[Math.abs(hash) % colors.length]
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) {
    return (n / 1_000_000).toFixed(2) + 'M'
  }
  if (n >= 1_000) {
    return (n / 1_000).toFixed(1) + 'K'
  }
  return n.toLocaleString()
}

function getPercentage(tokens: number): string {
  if (!props.data || props.data.length === 0) return '0'
  const totalTokens = props.data.reduce((sum, m) => sum + m.total_tokens, 0)
  if (totalTokens === 0) return '0'
  return ((tokens / totalTokens) * 100).toFixed(1)
}
</script>
