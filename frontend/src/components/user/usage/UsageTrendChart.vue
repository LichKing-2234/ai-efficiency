<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import type { UserUsageTrendPoint } from '@/types'
import { useI18n } from '@/i18n'
import { formatTokenCount } from '@/utils/formatters'

const LineChartCanvas = defineAsyncComponent(() => import('@/components/charts/LineChartCanvas.vue'))

const props = defineProps<{
  data: UserUsageTrendPoint[]
  loading: boolean
}>()

const { t } = useI18n()

const chartData = computed(() => ({
  labels: props.data.map((point) => point.date),
  datasets: [
    { label: t('usageDashboard.input'), data: props.data.map((point) => point.input_tokens), borderColor: '#2563eb', backgroundColor: '#2563eb22', fill: true, tension: 0.3 },
    { label: t('usageDashboard.output'), data: props.data.map((point) => point.output_tokens), borderColor: '#16a34a', backgroundColor: '#16a34a22', fill: true, tension: 0.3 },
    { label: t('usageDashboard.cacheCreation'), data: props.data.map((point) => point.cache_creation_tokens), borderColor: '#d97706', backgroundColor: '#d9770622', fill: true, tension: 0.3 },
    { label: t('usageDashboard.cacheRead'), data: props.data.map((point) => point.cache_read_tokens), borderColor: '#0891b2', backgroundColor: '#0891b222', fill: true, tension: 0.3 },
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
        label: (context: any) => `${context.dataset.label}: ${formatTokenCount(Number(context.raw ?? 0))}`,
        footer: (items: any[]) => {
          const index = items[0]?.dataIndex
          const point = props.data[index]
          return point ? `${t('usageDashboard.actual')}: $${point.actual_cost.toFixed(4)} | ${t('usageDashboard.standard')}: $${point.cost.toFixed(4)}` : ''
        },
      },
    },
  },
  scales: {
    y: {
      ticks: {
        callback: (value: string | number) => formatTokenCount(Number(value)),
      },
    },
  },
}))
</script>

<template>
  <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
    <div class="mb-4 flex items-center justify-between">
      <h2 class="text-base font-semibold text-gray-900">{{ t('usageDashboard.tokenTrend') }}</h2>
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
