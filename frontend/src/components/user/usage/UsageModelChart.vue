<script setup lang="ts">
import { computed } from 'vue'
import { Chart as ChartJS, ArcElement, Tooltip, Legend } from 'chart.js'
import { Doughnut } from 'vue-chartjs'
import type { UserUsageModelStat } from '@/types'
import { useI18n } from '@/i18n'

ChartJS.register(ArcElement, Tooltip, Legend)

const props = defineProps<{
  data: UserUsageModelStat[]
  loading: boolean
}>()

const { t } = useI18n()

const colors = ['#2563eb', '#16a34a', '#d97706', '#dc2626', '#7c3aed', '#db2777', '#0891b2', '#65a30d']

const chartData = computed(() => ({
  labels: props.data.map((model) => model.model),
  datasets: [{ data: props.data.map((model) => model.total_tokens), backgroundColor: colors }],
}))

const chartOptions = { responsive: true, maintainAspectRatio: false, plugins: { legend: { display: false } } }

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return n.toLocaleString()
}
</script>

<template>
  <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-800">
    <h2 class="mb-4 text-base font-semibold text-gray-900 dark:text-gray-100">{{ t('usageDashboard.modelDistribution') }}</h2>
    <div v-if="loading" class="flex h-72 items-center justify-center text-sm text-gray-500 dark:text-gray-400">
      {{ t('usageDashboard.loadingModels') }}
    </div>
    <div v-else-if="data.length === 0" class="flex h-72 items-center justify-center text-sm text-gray-500 dark:text-gray-400">
      {{ t('usageDashboard.noModelData') }}
    </div>
    <div v-else class="grid gap-4 lg:grid-cols-[180px_1fr]">
      <div class="h-44">
        <Doughnut :data="chartData" :options="chartOptions" />
      </div>
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead>
            <tr class="border-b border-gray-200 text-left text-xs uppercase text-gray-500 dark:border-gray-700 dark:text-gray-400">
              <th class="pb-2">{{ t('usageDashboard.model') }}</th>
              <th class="pb-2 text-right">{{ t('usageDashboard.requests') }}</th>
              <th class="pb-2 text-right">{{ t('usageDashboard.tokens') }}</th>
              <th class="pb-2 text-right">{{ t('usageDashboard.actual') }}</th>
              <th class="pb-2 text-right">{{ t('usageDashboard.standard') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="model in data" :key="model.model" class="border-b border-gray-100 last:border-0 dark:border-gray-700">
              <td class="max-w-[12rem] truncate py-2 font-medium text-gray-900 dark:text-gray-100" :title="model.model">
                {{ model.model }}
              </td>
              <td class="py-2 text-right text-gray-600 dark:text-gray-300">{{ model.requests.toLocaleString() }}</td>
              <td class="py-2 text-right text-gray-600 dark:text-gray-300">{{ formatTokens(model.total_tokens) }}</td>
              <td class="py-2 text-right text-green-600 dark:text-green-400">${{ model.actual_cost.toFixed(4) }}</td>
              <td class="py-2 text-right text-gray-500 dark:text-gray-400">${{ model.cost.toFixed(4) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>
