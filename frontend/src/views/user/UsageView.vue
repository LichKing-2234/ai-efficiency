<template>
  <div class="container mx-auto px-4 py-8">
    <div class="mb-6 flex items-center justify-between">
      <h1 class="text-3xl font-bold text-gray-900 dark:text-gray-100">My Usage</h1>
      <div class="flex space-x-2">
        <button
          v-for="option in dateRangeOptions"
          :key="option.value"
          :class="[
            'rounded-lg px-4 py-2 text-sm font-medium transition-colors',
            selectedRange === option.value
              ? 'bg-blue-600 text-white'
              : 'bg-gray-200 text-gray-700 hover:bg-gray-300 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600',
          ]"
          @click="selectDateRange(option.value)"
        >
          {{ option.label }}
        </button>
      </div>
    </div>

    <div v-if="!hasRelayPassword" class="rounded-lg bg-yellow-50 p-6 dark:bg-yellow-900/20">
      <div class="flex items-start">
        <svg class="h-6 w-6 text-yellow-600 dark:text-yellow-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
        </svg>
        <div class="ml-3">
          <h3 class="text-sm font-medium text-yellow-800 dark:text-yellow-200">Setup Required</h3>
          <p class="mt-1 text-sm text-yellow-700 dark:text-yellow-300">
            Please complete AI service configuration to view your usage data.
          </p>
          <router-link
            to="/user"
            class="mt-3 inline-block rounded-md bg-yellow-600 px-4 py-2 text-sm font-medium text-white hover:bg-yellow-700"
          >
            Go to Settings
          </router-link>
        </div>
      </div>
    </div>

    <div v-else>
      <UsageStatsCards :stats="stats" />

      <div class="mt-6 grid grid-cols-1 gap-6 lg:grid-cols-2">
        <UsageTrendChart :data="trendData" :loading="trendLoading" />
        <UsageModelChart :data="modelData" :loading="modelLoading" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { getUserUsageStats, getUserUsageTrend, getUserUsageModels } from '@/api/userUsage'
import type { UserUsageStats, UsageTrendDataPoint, UsageModelStat } from '@/types'
import UsageStatsCards from '@/components/user/usage/UsageStatsCards.vue'
import UsageTrendChart from '@/components/user/usage/UsageTrendChart.vue'
import UsageModelChart from '@/components/user/usage/UsageModelChart.vue'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()

const stats = ref<UserUsageStats | null>(null)
const trendData = ref<UsageTrendDataPoint[]>([])
const modelData = ref<UsageModelStat[]>([])
const trendLoading = ref(false)
const modelLoading = ref(false)

const dateRangeOptions = [
  { label: 'Today', value: 'today' },
  { label: '7 Days', value: '7d' },
  { label: '30 Days', value: '30d' },
]

const selectedRange = ref('30d')

const hasRelayPassword = computed(() => {
  return authStore.user?.relay_auth_password !== null
})

function getDateRange(range: string): { start_date: string; end_date: string } {
  const end = new Date()
  const start = new Date()

  switch (range) {
    case 'today':
      break
    case '7d':
      start.setDate(end.getDate() - 6)
      break
    case '30d':
      start.setDate(end.getDate() - 29)
      break
  }

  return {
    start_date: formatDate(start),
    end_date: formatDate(end),
  }
}

function formatDate(date: Date): string {
  return date.toISOString().split('T')[0]
}

async function loadStats() {
  try {
    const res = await getUserUsageStats()
    stats.value = res.data.data ?? null
  } catch (err) {
    console.error('Failed to load usage stats:', err)
  }
}

async function loadTrend() {
  trendLoading.value = true
  try {
    const { start_date, end_date } = getDateRange(selectedRange.value)
    const res = await getUserUsageTrend({ start_date, end_date, granularity: 'day' })
    trendData.value = res.data.data?.trend ?? []
  } catch (err) {
    console.error('Failed to load usage trend:', err)
    trendData.value = []
  } finally {
    trendLoading.value = false
  }
}

async function loadModels() {
  modelLoading.value = true
  try {
    const { start_date, end_date } = getDateRange(selectedRange.value)
    const res = await getUserUsageModels({ start_date, end_date })
    modelData.value = res.data.data?.models ?? []
  } catch (err) {
    console.error('Failed to load model distribution:', err)
    modelData.value = []
  } finally {
    modelLoading.value = false
  }
}

function selectDateRange(range: string) {
  selectedRange.value = range
  loadTrend()
  loadModels()
}

onMounted(() => {
  loadStats()
  loadTrend()
  loadModels()
})
</script>
