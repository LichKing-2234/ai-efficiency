<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import type { UserUsageKiroModel } from '@/types'
import { useI18n } from '@/i18n'
import { formatTokenCount } from '@/utils/formatters'

const DoughnutChartCanvas = defineAsyncComponent(() => import('@/components/charts/DoughnutChartCanvas.vue'))

const props = defineProps<{
  data: UserUsageKiroModel[]
  loading: boolean
}>()

const { t } = useI18n()

const colors = ['#d97706', '#2563eb', '#16a34a', '#dc2626', '#7c3aed', '#db2777', '#0891b2', '#65a30d']

const chartData = computed(() => ({
  labels: props.data.map((model) => model.model),
  datasets: [{ data: props.data.map((model) => model.credits), backgroundColor: colors }],
}))

const chartOptions = { responsive: true, maintainAspectRatio: false, plugins: { legend: { display: false } } }

function formatCredits(value: number): string {
  return value.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}
</script>

<template>
  <section class="min-w-0 rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
    <h2 class="mb-4 text-base font-semibold text-gray-900">{{ t('usageDashboard.kiroCreditsModelDistribution') }}</h2>
    <div v-if="loading" class="flex h-72 items-center justify-center text-sm text-gray-500">
      {{ t('usageDashboard.loadingModels') }}
    </div>
    <div v-else-if="data.length === 0" class="flex h-72 items-center justify-center text-sm text-gray-500">
      {{ t('usageDashboard.noModelData') }}
    </div>
    <div v-else class="grid min-w-0 gap-4 2xl:grid-cols-[180px_minmax(0,1fr)]">
      <div data-testid="kiro-model-chart-canvas" class="h-44 min-w-0 w-full max-w-full">
        <DoughnutChartCanvas :data="chartData" :options="chartOptions" />
      </div>
      <div class="min-w-0">
        <ElTable :data="data" row-key="model" class="w-full" size="small">
          <ElTableColumn :label="t('usageDashboard.model')" min-width="140">
            <template #default="{ row: model }">
              <ElTooltip :content="model.model" placement="top" :show-after="400">
                <span class="block max-w-[12rem] truncate font-medium text-gray-900">{{ model.model }}</span>
              </ElTooltip>
            </template>
          </ElTableColumn>
          <ElTableColumn :label="t('usageDashboard.requests')" align="right" min-width="90">
            <template #default="{ row: model }">{{ model.requests.toLocaleString() }}</template>
          </ElTableColumn>
          <ElTableColumn :label="t('usageDashboard.kiroCreditsColumn')" align="right" min-width="100">
            <template #default="{ row: model }">{{ formatCredits(model.credits) }}</template>
          </ElTableColumn>
          <ElTableColumn :label="t('usageDashboard.kiroCreditsEstimateTokens')" align="right" min-width="120">
            <template #default="{ row: model }">{{ formatTokenCount(model.input_tokens + model.output_tokens) }}</template>
          </ElTableColumn>
        </ElTable>
      </div>
    </div>
  </section>
</template>
