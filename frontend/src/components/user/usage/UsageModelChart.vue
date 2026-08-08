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
        <ElTable :data="data" row-key="model" class="min-w-[36rem] w-full">
          <ElTableColumn :label="t('usageDashboard.model')" min-width="150">
            <template #default="{ row: model }">
              <ElTooltip :content="model.model" placement="top" :show-after="400">
                <span class="block max-w-[12rem] truncate font-medium text-gray-900">
                {{ model.model }}
                </span>
              </ElTooltip>
            </template>
          </ElTableColumn>
          <ElTableColumn :label="t('usageDashboard.requests')" align="right" min-width="100">
            <template #default="{ row: model }">{{ model.requests.toLocaleString() }}</template>
          </ElTableColumn>
          <ElTableColumn :label="t('usageDashboard.tokens')" align="right" min-width="100">
            <template #default="{ row: model }">{{ formatTokenCount(model.total_tokens) }}</template>
          </ElTableColumn>
          <ElTableColumn v-if="!props.hideCost" :label="t('usageDashboard.actual')" align="right" min-width="100">
            <template #default="{ row: model }"><span class="text-green-600">${{ model.actual_cost.toFixed(4) }}</span></template>
          </ElTableColumn>
          <ElTableColumn v-if="!props.hideCost" :label="t('usageDashboard.standard')" align="right" min-width="100">
            <template #default="{ row: model }"><span class="text-gray-500">${{ model.cost.toFixed(4) }}</span></template>
          </ElTableColumn>
        </ElTable>
      </div>
    </div>
  </section>
</template>
