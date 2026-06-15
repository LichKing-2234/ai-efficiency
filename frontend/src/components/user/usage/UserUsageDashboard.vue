<template>
  <div :class="props.embedded ? 'space-y-6' : 'mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8'">
    <div class="mb-6 flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
      <div>
        <component
          :is="props.embedded ? 'h2' : 'h1'"
          :class="props.embedded ? 'text-base font-semibold text-slate-950' : 'text-2xl font-semibold text-gray-900'"
        >
          {{ props.embedded ? t('usageDashboard.embeddedTitle') : t('usageDashboard.title') }}
        </component>
        <p class="mt-1 text-sm text-gray-500">{{ t('usageDashboard.subtitle') }}</p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <button data-test="range-today" type="button" :class="rangeButtonClass(selectedRange === 'today')" @click="selectRange('today')">
          {{ t('usageDashboard.today') }}
        </button>
        <button data-test="range-7d" type="button" :class="rangeButtonClass(selectedRange === '7d')" @click="selectRange('7d')">
          {{ t('usageDashboard.sevenDays') }}
        </button>
        <button data-test="range-30d" type="button" :class="rangeButtonClass(selectedRange === '30d')" @click="selectRange('30d')">
          {{ t('usageDashboard.thirtyDays') }}
        </button>
        <button
          type="button"
          class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-60"
          :disabled="loading"
          @click="loadDashboard"
        >
          {{ t('usageDashboard.refresh') }}
        </button>
      </div>
    </div>

    <div v-if="loading && !currentSnapshot" class="flex min-h-80 items-center justify-center text-sm text-gray-500">
      {{ t('usageDashboard.loading') }}
    </div>

    <div v-else-if="setupRequired" class="rounded-lg border border-amber-200 bg-amber-50 p-6">
      <h2 class="text-base font-semibold text-amber-900">{{ t('usageDashboard.setupTitle') }}</h2>
      <p class="mt-2 text-sm text-amber-800">{{ t('usageDashboard.setupHelp') }}</p>
      <router-link to="/user" class="mt-4 inline-flex rounded-md bg-amber-600 px-4 py-2 text-sm font-medium text-white hover:bg-amber-700">
        {{ t('usageDashboard.openSetup') }}
      </router-link>
    </div>

    <div v-else-if="errorMessage" class="rounded-lg border border-red-200 bg-red-50 p-6">
      <h2 class="text-base font-semibold text-red-900">{{ errorMessage }}</h2>
      <p class="mt-2 text-sm text-red-800">{{ t('usageDashboard.retryHelp') }}</p>
      <router-link v-if="credentialError" to="/user" class="mt-4 inline-flex rounded-md bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-700">
        {{ t('usageDashboard.openSetup') }}
      </router-link>
    </div>

    <div v-else class="space-y-6">
      <UsageStatsCards
        :stats="currentSnapshot?.stats ?? null"
        :trend="currentSnapshot?.trend ?? []"
        :range-label="snapshotRangeLabel"
        :hide-cost="props.homeMode"
      />
      <div class="grid min-w-0 grid-cols-1 gap-6 xl:grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)]">
        <UsageTrendChart :data="currentSnapshot?.trend ?? []" :loading="loading" />
        <UsageModelChart :data="currentSnapshot?.models ?? []" :loading="loading" :hide-cost="props.homeMode" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getUserUsageDashboard } from '@/api/userUsage'
import { useI18n } from '@/i18n'
import type { UserUsageDashboardParams, UserUsageDashboardSnapshot } from '@/types'
import UsageStatsCards from '@/components/user/usage/UsageStatsCards.vue'
import UsageTrendChart from '@/components/user/usage/UsageTrendChart.vue'
import UsageModelChart from '@/components/user/usage/UsageModelChart.vue'

type RangeOption = 'today' | '7d' | '30d'

const props = withDefaults(defineProps<{
  embedded?: boolean
  homeMode?: boolean
  initialSnapshot?: UserUsageDashboardSnapshot | null
}>(), {
  embedded: false,
  homeMode: false,
  initialSnapshot: null,
})

const selectedRange = ref<RangeOption>('7d')
const snapshotRange = ref<RangeOption>('7d')
const snapshot = ref<UserUsageDashboardSnapshot | null>(null)
const loading = ref(false)
const errorMessage = ref('')
const credentialError = ref(false)
const { t } = useI18n()
let dashboardRequestSeq = 0

const currentSnapshot = computed(() => snapshot.value ?? props.initialSnapshot)
const setupRequired = computed(() => currentSnapshot.value?.configured === false)
const snapshotRangeLabel = computed(() => rangeLabel(snapshotRange.value))

function rangeLabel(range: RangeOption) {
  if (range === 'today') return t('usageDashboard.today')
  if (range === '7d') return t('usageDashboard.sevenDays')
  return t('usageDashboard.thirtyDays')
}

function rangeButtonClass(active: boolean) {
  return [
    'rounded-md px-3 py-2 text-sm font-medium transition-colors',
    active
      ? 'bg-blue-600 text-white'
      : 'border border-gray-300 text-gray-700 hover:bg-gray-50',
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
  const requestedRange = selectedRange.value
  const requestSeq = ++dashboardRequestSeq
  loading.value = true
  errorMessage.value = ''
  credentialError.value = false
  try {
    const res = await getUserUsageDashboard(buildParams(requestedRange))
    if (requestSeq !== dashboardRequestSeq) return
    snapshot.value = res.data.data ?? null
    snapshotRange.value = requestedRange
  } catch (err: any) {
    if (requestSeq !== dashboardRequestSeq) return
    snapshot.value = null
    credentialError.value = err?.response?.status === 409
    errorMessage.value = credentialError.value ? t('usageDashboard.credentialError') : t('usageDashboard.unavailable')
  } finally {
    if (requestSeq === dashboardRequestSeq) {
      loading.value = false
    }
  }
}

function selectRange(range: RangeOption) {
  selectedRange.value = range
  loadDashboard()
}

onMounted(() => {
  if (props.initialSnapshot) {
    snapshotRange.value = selectedRange.value
    return
  }
  loadDashboard()
})
</script>
