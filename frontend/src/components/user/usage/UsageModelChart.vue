<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import type { UserUsageKiroModel, UserUsageModelStat } from '@/types'
import { useUsageUnit } from '@/composables/useUsageUnit'
import { useWideContentLayout } from '@/composables/useMediaQuery'
import { useI18n } from '@/i18n'
import { formatCredits, formatTokenCount } from '@/utils/formatters'
import UsageUnitToggle from '@/components/usage/UsageUnitToggle.vue'

const DoughnutChartCanvas = defineAsyncComponent(() => import('@/components/charts/DoughnutChartCanvas.vue'))

const props = withDefaults(defineProps<{
  data: UserUsageModelStat[]
  kiroData?: UserUsageKiroModel[]
  loading: boolean
  kiroLoading?: boolean
  kiroAvailable?: boolean
  hideCost?: boolean
}>(), {
  kiroData: () => [],
  kiroLoading: false,
  kiroAvailable: false,
  hideCost: false,
})

const { t } = useI18n()
const isDesktop = useWideContentLayout()
const { unit } = useUsageUnit('ae.usage.model.unit')

const colors = ['#2563eb', '#16a34a', '#d97706', '#dc2626', '#7c3aed', '#db2777', '#0891b2', '#65a30d']

const creditsMode = computed(() => unit.value === 'credits')
const activeLoading = computed(() => creditsMode.value ? props.kiroLoading : props.loading)
const noData = computed(() => creditsMode.value ? (!props.kiroAvailable || props.kiroData.length === 0) : props.data.length === 0)

const chartData = computed(() => creditsMode.value
  ? {
      labels: props.kiroData.map((model) => model.model),
      datasets: [{ data: props.kiroData.map((model) => model.credits), backgroundColor: colors }],
    }
  : {
      labels: props.data.map((model) => model.model),
      datasets: [{ data: props.data.map((model) => model.total_tokens), backgroundColor: colors }],
    },
)

const chartOptions = { responsive: true, maintainAspectRatio: false, plugins: { legend: { display: false } } }
</script>

<template>
  <section class="min-w-0 rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
    <div class="mb-4 flex flex-wrap items-center justify-between gap-2">
      <h2 class="text-base font-semibold text-gray-900">{{ t('usageDashboard.modelDistribution') }}</h2>
      <UsageUnitToggle v-model="unit" data-testid="usage-model-unit-toggle" />
    </div>
    <div v-if="activeLoading" class="flex h-72 items-center justify-center text-sm text-gray-500">
      {{ t('usageDashboard.loadingModels') }}
    </div>
    <div v-else-if="noData" class="flex h-72 items-center justify-center text-sm text-gray-500">
      {{ creditsMode && !props.kiroAvailable ? t('usageDashboard.kiroCreditsNoKey') : t('usageDashboard.noModelData') }}
    </div>
    <div v-else class="grid min-w-0 gap-4 2xl:grid-cols-[180px_minmax(0,1fr)]">
      <div data-testid="usage-model-chart-canvas" class="h-44 min-w-0 w-full max-w-full">
        <DoughnutChartCanvas :data="chartData" :options="chartOptions" />
      </div>
      <div
        v-if="isDesktop"
        data-testid="usage-model-table-scroll"
        data-model-list="desktop"
        class="min-w-0"
      >
        <ElTable v-if="!creditsMode" :data="props.data" row-key="model" class="w-full">
          <ElTableColumn :label="t('usageDashboard.model')" min-width="150">
            <template #default="{ row: model }">
              <ElTooltip :content="model.model" placement="top" :show-after="400">
                <span data-model-row class="block max-w-[12rem] truncate font-medium text-gray-900">
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
        <ElTable v-else :data="props.kiroData" row-key="model" class="w-full">
          <ElTableColumn :label="t('usageDashboard.model')" min-width="150">
            <template #default="{ row: model }">
              <ElTooltip :content="model.model" placement="top" :show-after="400">
                <span data-model-row class="block max-w-[12rem] truncate font-medium text-gray-900">
                {{ model.model }}
                </span>
              </ElTooltip>
            </template>
          </ElTableColumn>
          <ElTableColumn :label="t('usageDashboard.requests')" align="right" min-width="100">
            <template #default="{ row: model }">{{ model.requests.toLocaleString() }}</template>
          </ElTableColumn>
          <ElTableColumn :label="t('usageDashboard.kiroCreditsColumn')" align="right" min-width="100">
            <template #default="{ row: model }">{{ formatCredits(model.credits) }}</template>
          </ElTableColumn>
          <ElTableColumn :label="t('usageDashboard.kiroCreditsEstimateTokens')" align="right" min-width="130">
            <template #default="{ row: model }">{{ formatTokenCount(model.input_tokens + model.output_tokens) }}</template>
          </ElTableColumn>
        </ElTable>
      </div>
      <div v-else data-model-list="mobile" class="space-y-3">
        <template v-if="!creditsMode">
          <ElCard v-for="model in props.data" :key="model.model" data-model-row shadow="never">
            <div class="break-words font-medium text-gray-900">{{ model.model }}</div>
            <dl class="mt-3 grid grid-cols-2 gap-3 text-sm">
              <div>
                <dt class="text-xs font-medium uppercase text-slate-500">{{ t('usageDashboard.requests') }}</dt>
                <dd class="mt-1 text-slate-900">{{ model.requests.toLocaleString() }}</dd>
              </div>
              <div>
                <dt class="text-xs font-medium uppercase text-slate-500">{{ t('usageDashboard.tokens') }}</dt>
                <dd class="mt-1 text-slate-900">{{ formatTokenCount(model.total_tokens) }}</dd>
              </div>
              <div v-if="!props.hideCost">
                <dt class="text-xs font-medium uppercase text-slate-500">{{ t('usageDashboard.actual') }}</dt>
                <dd class="mt-1 text-green-600">${{ model.actual_cost.toFixed(4) }}</dd>
              </div>
              <div v-if="!props.hideCost">
                <dt class="text-xs font-medium uppercase text-slate-500">{{ t('usageDashboard.standard') }}</dt>
                <dd class="mt-1 text-gray-500">${{ model.cost.toFixed(4) }}</dd>
              </div>
            </dl>
          </ElCard>
        </template>
        <template v-else>
          <ElCard v-for="model in props.kiroData" :key="model.model" data-model-row shadow="never">
            <div class="break-words font-medium text-gray-900">{{ model.model }}</div>
            <dl class="mt-3 grid grid-cols-2 gap-3 text-sm">
              <div>
                <dt class="text-xs font-medium uppercase text-slate-500">{{ t('usageDashboard.requests') }}</dt>
                <dd class="mt-1 text-slate-900">{{ model.requests.toLocaleString() }}</dd>
              </div>
              <div>
                <dt class="text-xs font-medium uppercase text-slate-500">{{ t('usageDashboard.kiroCreditsColumn') }}</dt>
                <dd class="mt-1 text-slate-900">{{ formatCredits(model.credits) }}</dd>
              </div>
              <div>
                <dt class="text-xs font-medium uppercase text-slate-500">{{ t('usageDashboard.kiroCreditsEstimateTokens') }}</dt>
                <dd class="mt-1 text-slate-900">{{ formatTokenCount(model.input_tokens + model.output_tokens) }}</dd>
              </div>
            </dl>
          </ElCard>
        </template>
      </div>
    </div>
  </section>
</template>
