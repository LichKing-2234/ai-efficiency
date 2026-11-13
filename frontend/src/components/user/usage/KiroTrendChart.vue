<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import type { UserUsageKiroDay } from '@/types'
import { useI18n } from '@/i18n'

const LineChartCanvas = defineAsyncComponent(() => import('@/components/charts/LineChartCanvas.vue'))

const props = defineProps<{
  data: UserUsageKiroDay[]
  loading: boolean
}>()

const { t } = useI18n()

function formatCredits(value: number): string {
  return value.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

const chartData = computed(() => ({
  labels: props.data.map((point) => point.date),
  datasets: [
    {
      label: t('usageDashboard.kiroCreditsColumn'),
      data: props.data.map((point) => point.credits),
      borderColor: '#d97706',
      backgroundColor: '#d9770622',
      fill: true,
      tension: 0.3,
      yAxisID: 'y',
    },
    {
      label: t('usageDashboard.kiroCreditsRequests'),
      data: props.data.map((point) => point.requests),
      borderColor: '#2563eb',
      backgroundColor: '#2563eb22',
      fill: false,
      tension: 0.3,
      yAxisID: 'y1',
    },
  ],
}))

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: { intersect: false, mode: 'index' as const },
  plugins: {
    legend: { position: 'top' as const },
    tooltip: {
      callbacks: {
        label: (context: any) => {
          const label = context.dataset.label ?? ''
          const value = Number(context.raw ?? 0)
          return context.dataset.yAxisID === 'y1' ? `${label}: ${value.toLocaleString()}` : `${label}: ${formatCredits(value)}`
        },
      },
    },
  },
  scales: {
    y: {
      position: 'left' as const,
      ticks: { callback: (value: string | number) => formatCredits(Number(value)) },
    },
    y1: {
      position: 'right' as const,
      grid: { drawOnChartArea: false },
      ticks: { callback: (value: string | number) => Number(value).toLocaleString() },
    },
  },
}))
</script>

<template>
  <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
    <div class="mb-4 flex items-center justify-between">
      <h2 class="text-base font-semibold text-gray-900">{{ t('usageDashboard.kiroCreditsTrend') }}</h2>
    </div>
    <div v-if="loading" class="flex h-72 items-center justify-center text-sm text-gray-500">
      {{ t('usageDashboard.loadingTrend') }}
    </div>
    <div v-else-if="data.length === 0" class="flex h-72 items-center justify-center text-sm text-gray-500">
      {{ t('usageDashboard.noTrendData') }}
    </div>
    <div v-else class="h-72">
      <LineChartCanvas :data="chartData" :options="chartOptions" />
    </div>
  </section>
</template>
