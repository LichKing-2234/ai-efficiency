<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import type { UserUsageModelStat } from '@/types'
import { useI18n } from '@/i18n'
import { formatTokenCount } from '@/utils/formatters'

const DoughnutChartCanvas = defineAsyncComponent(() => import('@/components/charts/DoughnutChartCanvas.vue'))

const props = withDefaults(defineProps<{
  data: UserUsageModelStat[]
  loading: boolean
  hideCost?: boolean
}>(), {
  hideCost: false,
})

const { t } = useI18n()

const colors = ['#2563eb', '#16a34a', '#d97706', '#dc2626', '#7c3aed', '#db2777', '#0891b2', '#65a30d']

const chartData = computed(() => ({
  labels: props.data.map((model) => model.model),
  datasets: [{ data: props.data.map((model) => model.total_tokens), backgroundColor: colors }],
}))

const chartOptions = { responsive: true, maintainAspectRatio: false, plugins: { legend: { display: false } } }

</script>

<template>
  <section class="min-w-0 rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
    <h2 class="mb-4 text-base font-semibold text-gray-900">{{ t('usageDashboard.modelDistribution') }}</h2>
    <div v-if="loading" class="flex h-72 items-center justify-center text-sm text-gray-500">
      {{ t('usageDashboard.loadingModels') }}
    </div>
    <div v-else-if="data.length === 0" class="flex h-72 items-center justify-center text-sm text-gray-500">
      {{ t('usageDashboard.noModelData') }}
    </div>
    <div v-else class="grid min-w-0 gap-4 2xl:grid-cols-[180px_minmax(0,1fr)]">
      <div class="h-44">
        <DoughnutChartCanvas :data="chartData" :options="chartOptions" />
      </div>
      <div data-testid="usage-model-table-scroll" class="min-w-0 overflow-x-auto pb-2">
        <table class="min-w-[36rem] w-full text-sm">
          <thead>
            <tr class="border-b border-gray-200 text-left text-xs uppercase text-gray-500">
              <th class="pb-2">{{ t('usageDashboard.model') }}</th>
              <th class="pb-2 text-right">{{ t('usageDashboard.requests') }}</th>
              <th class="pb-2 text-right">{{ t('usageDashboard.tokens') }}</th>
              <th v-if="!props.hideCost" class="pb-2 text-right">{{ t('usageDashboard.actual') }}</th>
              <th v-if="!props.hideCost" class="pb-2 text-right">{{ t('usageDashboard.standard') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="model in data" :key="model.model" class="border-b border-gray-100 last:border-0">
              <td class="max-w-[12rem] truncate py-2 font-medium text-gray-900" :title="model.model">
                {{ model.model }}
              </td>
              <td class="py-2 text-right text-gray-600">{{ model.requests.toLocaleString() }}</td>
              <td class="py-2 text-right text-gray-600">{{ formatTokenCount(model.total_tokens) }}</td>
              <td v-if="!props.hideCost" class="py-2 text-right text-green-600">${{ model.actual_cost.toFixed(4) }}</td>
              <td v-if="!props.hideCost" class="py-2 text-right text-gray-500">${{ model.cost.toFixed(4) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>
