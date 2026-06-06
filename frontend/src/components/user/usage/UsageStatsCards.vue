<template>
  <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
    <div class="rounded-lg bg-white p-4 shadow dark:bg-gray-800">
      <div class="flex items-center">
        <div class="rounded-full bg-blue-100 p-2 dark:bg-blue-900/30">
          <svg class="h-6 w-6 text-blue-600 dark:text-blue-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 7h8m0 0v8m0-8l-8 8-4-4-6 6" />
          </svg>
        </div>
        <div class="ml-3">
          <p class="text-sm font-medium text-gray-500 dark:text-gray-400">Total Requests</p>
          <p class="text-2xl font-semibold text-gray-900 dark:text-gray-100">
            {{ stats?.total_requests?.toLocaleString() ?? '0' }}
          </p>
        </div>
      </div>
      <div class="mt-2 text-xs text-gray-500 dark:text-gray-400">
        Today: {{ stats?.today_requests?.toLocaleString() ?? '0' }}
      </div>
    </div>

    <div class="rounded-lg bg-white p-4 shadow dark:bg-gray-800">
      <div class="flex items-center">
        <div class="rounded-full bg-green-100 p-2 dark:bg-green-900/30">
          <svg class="h-6 w-6 text-green-600 dark:text-green-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
          </svg>
        </div>
        <div class="ml-3">
          <p class="text-sm font-medium text-gray-500 dark:text-gray-400">Total Tokens</p>
          <p class="text-2xl font-semibold text-gray-900 dark:text-gray-100">
            {{ formatTokens(stats?.total_tokens ?? 0) }}
          </p>
        </div>
      </div>
      <div class="mt-2 text-xs text-gray-500 dark:text-gray-400">
        Input: {{ formatTokens(stats?.total_input_tokens ?? 0) }} / Output: {{ formatTokens(stats?.total_output_tokens ?? 0) }}
      </div>
    </div>

    <div class="rounded-lg bg-white p-4 shadow dark:bg-gray-800">
      <div class="flex items-center">
        <div class="rounded-full bg-purple-100 p-2 dark:bg-purple-900/30">
          <svg class="h-6 w-6 text-purple-600 dark:text-purple-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3 .895 3 2-1.343 2-3 2m0-8c1.11 0 2.08.402 2.599 1M12 8V7m0 1v8m0 0v1m0-1c-1.11 0-2.08-.402-2.599-1M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
        </div>
        <div class="ml-3">
          <p class="text-sm font-medium text-gray-500 dark:text-gray-400">Total Cost</p>
          <p class="text-2xl font-semibold text-gray-900 dark:text-gray-100">
            ${{ (stats?.total_actual_cost ?? 0).toFixed(4) }}
          </p>
        </div>
      </div>
      <div class="mt-2 text-xs text-gray-500 dark:text-gray-400">
        Standard: ${{ (stats?.total_cost ?? 0).toFixed(4) }}
      </div>
    </div>

    <div class="rounded-lg bg-white p-4 shadow dark:bg-gray-800">
      <div class="flex items-center">
        <div class="rounded-full bg-orange-100 p-2 dark:bg-orange-900/30">
          <svg class="h-6 w-6 text-orange-600 dark:text-orange-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
        </div>
        <div class="ml-3">
          <p class="text-sm font-medium text-gray-500 dark:text-gray-400">Avg Duration</p>
          <p class="text-2xl font-semibold text-gray-900 dark:text-gray-100">
            {{ formatDuration(stats?.average_duration_ms ?? 0) }}
          </p>
        </div>
      </div>
      <div class="mt-2 text-xs text-gray-500 dark:text-gray-400">
        Per request
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { UserUsageStats } from '@/types'

defineProps<{
  stats: UserUsageStats | null
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

function formatDuration(ms: number): string {
  if (ms < 1000) {
    return ms + 'ms'
  }
  return (ms / 1000).toFixed(2) + 's'
}
</script>
