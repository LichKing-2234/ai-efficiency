<script setup lang="ts">
import { computed } from 'vue'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
  Filler,
} from 'chart.js'
import { Line } from 'vue-chartjs'
import type { UserUsageTrendPoint } from '@/types'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, Filler)

const props = defineProps<{
  data: UserUsageTrendPoint[]
  loading: boolean
}>()

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return n.toLocaleString()
}

const chartData = computed(() => ({
  labels: props.data.map((point) => point.date),
  datasets: [
    { label: 'Input', data: props.data.map((point) => point.input_tokens), borderColor: '#2563eb', backgroundColor: '#2563eb22', fill: true, tension: 0.3 },
    { label: 'Output', data: props.data.map((point) => point.output_tokens), borderColor: '#16a34a', backgroundColor: '#16a34a22', fill: true, tension: 0.3 },
    { label: 'Cache Creation', data: props.data.map((point) => point.cache_creation_tokens), borderColor: '#d97706', backgroundColor: '#d9770622', fill: true, tension: 0.3 },
    { label: 'Cache Read', data: props.data.map((point) => point.cache_read_tokens), borderColor: '#0891b2', backgroundColor: '#0891b222', fill: true, tension: 0.3 },
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
        label: (context: any) => `${context.dataset.label}: ${formatTokens(Number(context.raw ?? 0))}`,
        footer: (items: any[]) => {
          const index = items[0]?.dataIndex
          const point = props.data[index]
          return point ? `Actual: $${point.actual_cost.toFixed(4)} | Standard: $${point.cost.toFixed(4)}` : ''
        },
      },
    },
  },
  scales: {
    y: {
      ticks: {
        callback: (value: string | number) => formatTokens(Number(value)),
      },
    },
  },
}))
</script>

<template>
  <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-800">
    <div class="mb-4 flex items-center justify-between">
      <h2 class="text-base font-semibold text-gray-900 dark:text-gray-100">Token Trend</h2>
    </div>
    <div v-if="loading" class="flex h-72 items-center justify-center text-sm text-gray-500 dark:text-gray-400">
      Loading trend...
    </div>
    <div v-else-if="data.length === 0" class="flex h-72 items-center justify-center text-sm text-gray-500 dark:text-gray-400">
      No trend data available
    </div>
    <div v-else class="h-72">
      <Line :data="chartData" :options="chartOptions" />
    </div>
  </section>
</template>
