<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import type { UserUsageKiroDay, UserUsageTrendPoint } from '@/types'
import { useUsageUnit } from '@/composables/useUsageUnit'
import { useI18n } from '@/i18n'
import { formatCredits, formatTokenCount } from '@/utils/formatters'
import UsageUnitToggle from '@/components/usage/UsageUnitToggle.vue'

const LineChartCanvas = defineAsyncComponent(() => import('@/components/charts/LineChartCanvas.vue'))

const props = withDefaults(defineProps<{
  data: UserUsageTrendPoint[]
  loading: boolean
  kiroByDay?: UserUsageKiroDay[]
  kiroLoading?: boolean
  kiroAvailable?: boolean
}>(), {
  kiroByDay: () => [],
  kiroLoading: false,
  kiroAvailable: false,
})

const { t } = useI18n()
const { unit } = useUsageUnit('ae.usage.trend.unit')

const creditsMode = computed(() => unit.value === 'credits')
const activeLoading = computed(() => creditsMode.value ? props.kiroLoading : props.loading)

function labelOf(date: string) {
  return date.includes('T') ? date.slice(11, 16) : date
}

const chartData = computed(() => {
  if (creditsMode.value) {
    return {
      labels: props.kiroByDay.map((point) => labelOf(point.date)),
      datasets: [
        {
          label: t('usageDashboard.kiroCreditsColumn'),
          data: props.kiroByDay.map((point) => point.credits),
          borderColor: '#d97706',
          backgroundColor: '#d9770622',
          fill: true,
          tension: 0.3,
          yAxisID: 'y',
        },
        {
          label: t('usageDashboard.requests'),
          data: props.kiroByDay.map((point) => point.requests),
          borderColor: '#2563eb',
          backgroundColor: '#2563eb22',
          fill: false,
          tension: 0.3,
          yAxisID: 'y1',
        },
      ],
    }
  }
  return {
    labels: props.data.map((point) => labelOf(point.date)),
    datasets: [
      { label: t('usageDashboard.input'), data: props.data.map((point) => point.input_tokens), borderColor: '#2563eb', backgroundColor: '#2563eb22', fill: true, tension: 0.3 },
      { label: t('usageDashboard.output'), data: props.data.map((point) => point.output_tokens), borderColor: '#16a34a', backgroundColor: '#16a34a22', fill: true, tension: 0.3 },
      { label: t('usageDashboard.cacheCreation'), data: props.data.map((point) => point.cache_creation_tokens), borderColor: '#d97706', backgroundColor: '#d9770622', fill: true, tension: 0.3 },
      { label: t('usageDashboard.cacheRead'), data: props.data.map((point) => point.cache_read_tokens), borderColor: '#0891b2', backgroundColor: '#0891b222', fill: true, tension: 0.3 },
    ],
  }
})

const chartOptions = computed(() => {
  const credits = creditsMode.value
  return {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { intersect: false, mode: 'index' as const },
    plugins: {
      legend: { position: 'top' as const },
      tooltip: {
        callbacks: {
          label: (context: any) => {
            const label = context.dataset.label ?? ''
            if (credits) {
              return context.dataset.yAxisID === 'y1'
                ? `${label}: ${Number(context.raw ?? 0).toLocaleString()}`
                : `${label}: ${formatCredits(Number(context.raw ?? 0))}`
            }
            return `${label}: ${formatTokenCount(Number(context.raw ?? 0))}`
          },
          footer: (items: any[]) => {
            if (credits) return ''
            const index = items[0]?.dataIndex
            const point = props.data[index]
            return point ? `${t('usageDashboard.actual')}: $${point.actual_cost.toFixed(4)} | ${t('usageDashboard.standard')}: $${point.cost.toFixed(4)}` : ''
          },
        },
      },
    },
    scales: credits
      ? {
          y: {
            position: 'left' as const,
            ticks: { callback: (value: string | number) => formatCredits(Number(value)) },
          },
          y1: {
            position: 'right' as const,
            grid: { drawOnChartArea: false },
            ticks: { callback: (value: string | number) => Number(value).toLocaleString() },
          },
        }
      : {
          y: {
            ticks: { callback: (value: string | number) => formatTokenCount(Number(value)) },
          },
        },
  }
})

const isEmpty = computed(() => creditsMode.value ? (!props.kiroAvailable || props.kiroByDay.length === 0) : props.data.length === 0)
</script>

<template>
  <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
    <div class="mb-4 flex flex-wrap items-center justify-between gap-2">
      <h2 class="text-base font-semibold text-gray-900">{{ t('usageDashboard.usageTrend') }}</h2>
      <UsageUnitToggle v-model="unit" data-testid="usage-trend-unit-toggle" />
    </div>
    <div v-if="activeLoading" class="flex h-72 items-center justify-center text-sm text-gray-500">
      {{ t('usageDashboard.loadingTrend') }}
    </div>
    <div v-else-if="isEmpty" class="flex h-72 items-center justify-center text-sm text-gray-500">
      {{ creditsMode && !props.kiroAvailable ? t('usageDashboard.kiroCreditsNoKey') : t('usageDashboard.noTrendData') }}
    </div>
    <div v-else class="h-72">
      <LineChartCanvas :data="chartData" :options="chartOptions" />
    </div>
  </section>
</template>
