<template>
  <div class="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
    <div class="mb-6 flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
      <div>
        <h1 class="text-2xl font-semibold text-gray-900 dark:text-gray-100">My AI Usage</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">Usage and cost from your configured AI relay account.</p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <button data-test="range-today" type="button" :class="rangeButtonClass(selectedRange === 'today')" @click="selectRange('today')">
          Today
        </button>
        <button data-test="range-7d" type="button" :class="rangeButtonClass(selectedRange === '7d')" @click="selectRange('7d')">
          7 Days
        </button>
        <button data-test="range-30d" type="button" :class="rangeButtonClass(selectedRange === '30d')" @click="selectRange('30d')">
          30 Days
        </button>
        <button
          type="button"
          class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-60 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-800"
          :disabled="loading"
          @click="loadDashboard"
        >
          Refresh
        </button>
      </div>
    </div>

    <div v-if="loading && !snapshot" class="flex min-h-80 items-center justify-center text-sm text-gray-500 dark:text-gray-400">
      Loading usage dashboard...
    </div>

    <div v-else-if="setupRequired" class="rounded-lg border border-amber-200 bg-amber-50 p-6 dark:border-amber-800 dark:bg-amber-950/30">
      <h2 class="text-base font-semibold text-amber-900 dark:text-amber-100">Complete AI service configuration</h2>
      <p class="mt-2 text-sm text-amber-800 dark:text-amber-200">Usage data is available after your relay credentials are configured.</p>
      <router-link to="/user" class="mt-4 inline-flex rounded-md bg-amber-600 px-4 py-2 text-sm font-medium text-white hover:bg-amber-700">
        Open My Setup
      </router-link>
    </div>

    <div v-else-if="errorMessage" class="rounded-lg border border-red-200 bg-red-50 p-6 dark:border-red-800 dark:bg-red-950/30">
      <h2 class="text-base font-semibold text-red-900 dark:text-red-100">{{ errorMessage }}</h2>
      <p class="mt-2 text-sm text-red-800 dark:text-red-200">Try refreshing after checking your setup.</p>
      <router-link v-if="credentialError" to="/user" class="mt-4 inline-flex rounded-md bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-700">
        Open My Setup
      </router-link>
    </div>

    <div v-else class="space-y-6">
      <UsageStatsCards :stats="snapshot?.stats ?? null" />
      <div class="grid grid-cols-1 gap-6 xl:grid-cols-[1.35fr_1fr]">
        <UsageTrendChart :data="snapshot?.trend ?? []" :loading="loading" />
        <UsageModelChart :data="snapshot?.models ?? []" :loading="loading" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getUserUsageDashboard } from '@/api/userUsage'
import type { UserUsageDashboardParams, UserUsageDashboardSnapshot } from '@/types'
import UsageStatsCards from '@/components/user/usage/UsageStatsCards.vue'
import UsageTrendChart from '@/components/user/usage/UsageTrendChart.vue'
import UsageModelChart from '@/components/user/usage/UsageModelChart.vue'

type RangeOption = 'today' | '7d' | '30d'

const selectedRange = ref<RangeOption>('7d')
const snapshot = ref<UserUsageDashboardSnapshot | null>(null)
const loading = ref(false)
const errorMessage = ref('')
const credentialError = ref(false)

const setupRequired = computed(() => snapshot.value?.configured === false)

function rangeButtonClass(active: boolean) {
  return [
    'rounded-md px-3 py-2 text-sm font-medium transition-colors',
    active
      ? 'bg-blue-600 text-white'
      : 'border border-gray-300 text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-800',
  ]
}

function formatDate(date: Date): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function buildParams(range: RangeOption): UserUsageDashboardParams {
  const end = new Date()
  const start = new Date(end)
  const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone

  if (range === 'today') {
    return { start_date: formatDate(start), end_date: formatDate(end), granularity: 'hour', timezone }
  }
  if (range === '7d') {
    start.setDate(end.getDate() - 6)
  } else {
    start.setDate(end.getDate() - 29)
  }
  return { start_date: formatDate(start), end_date: formatDate(end), granularity: 'day', timezone }
}

async function loadDashboard() {
  loading.value = true
  errorMessage.value = ''
  credentialError.value = false
  try {
    const res = await getUserUsageDashboard(buildParams(selectedRange.value))
    snapshot.value = res.data.data ?? null
  } catch (err: any) {
    snapshot.value = null
    credentialError.value = err?.response?.status === 409
    errorMessage.value = credentialError.value ? 'Relay credentials need attention' : 'Usage dashboard is temporarily unavailable'
  } finally {
    loading.value = false
  }
}

function selectRange(range: RangeOption) {
  selectedRange.value = range
  loadDashboard()
}

onMounted(() => {
  loadDashboard()
})
</script>
