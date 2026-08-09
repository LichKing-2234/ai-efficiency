<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import { useI18n } from '@/i18n'
import { formatTokenCount } from '@/utils/formatters'
import type { TeamDepartmentTrendState, TeamMemberTrendState, TeamOverviewWindow } from '@/types'

const LineChartCanvas = defineAsyncComponent(() => import('@/components/charts/LineChartCanvas.vue'))

const props = defineProps<{
  state: TeamMemberTrendState
  departmentTrend?: TeamDepartmentTrendState | null
  window?: TeamOverviewWindow | null
}>()

const { t } = useI18n()

const seriesColors = [
  '#2563eb',
  '#16a34a',
  '#d97706',
  '#0891b2',
  '#dc2626',
  '#7c3aed',
  '#059669',
  '#ea580c',
  '#4f46e5',
  '#be123c',
  '#0f766e',
  '#9333ea',
]

const departmentSeriesColors = [
  '#111827',
  '#0f766e',
  '#7c2d12',
  '#1d4ed8',
  '#a21caf',
  '#0369a1',
  '#4d7c0f',
]

const trendLegendPanelClasses = 'min-w-0 self-start max-h-64 overflow-y-auto divide-y divide-slate-100 rounded-md border border-slate-200 pr-1'

const departmentTrendSeries = computed(() => props.departmentTrend?.series ?? [])

const teamTotalTrendSeries = computed(() => {
  return departmentTrendSeries.value.filter((series) => series.series_type === 'team_total')
})

const subteamTrendSeries = computed(() => {
  return departmentTrendSeries.value.filter((series) => series.series_type !== 'team_total')
})

const chartableTeamTotalSeries = computed(() => {
  return teamTotalTrendSeries.value.filter((series) => !series.unavailable && series.points.length > 0)
})

const chartableSubteamSeries = computed(() => {
  return subteamTrendSeries.value.filter((series) => !series.unavailable && series.points.length > 0)
})

const chartableMemberSeries = computed(() => {
  return props.state.series.filter((series) => !series.unavailable && series.points.length > 0)
})

const hasAnyTrendSeries = computed(() => {
  return departmentTrendSeries.value.length > 0 || props.state.series.length > 0
})

const timeDimensionLabel = computed(() => {
  if (props.window?.granularity === 'hour') return t('teamUsage.hourly')
  return t('teamUsage.daily')
})

const tokenUnitLabel = computed(() => t('teamUsage.tokens'))

const windowRangeLabel = computed(() => {
  if (!props.window?.start_date || !props.window.end_date) return ''
  return `${props.window.start_date} - ${props.window.end_date}`
})

const chartMetaItems = computed(() => {
  const items = [tokenUnitLabel.value, timeDimensionLabel.value]
  if (windowRangeLabel.value) items.push(windowRangeLabel.value)
  if (props.window?.timezone) items.push(props.window.timezone)
  return items
})

type TrendChartPoint = { date: string; total_tokens?: number | null }
type TrendChartSeries = {
  label: string
  points: TrendChartPoint[]
  color: string
  borderWidth?: number
}

const teamTotalChartData = computed(() => buildTrendChartData(
  chartableTeamTotalSeries.value.map((series, index) => departmentChartSeries(series, index)),
))

const comparisonChartData = computed(() => buildTrendChartData(
  chartableSubteamSeries.value.map((series, index) => departmentChartSeries(series, index)),
))

const memberChartData = computed(() => buildTrendChartData(
  chartableMemberSeries.value.map((series, index) => memberChartSeries(series, index)),
))

const sparseTeamTotalTrend = computed(() => shouldUseSparsePresentation(
  teamTotalChartData.value.labels,
  teamTotalTrendSeries.value.length,
))
const sparseComparisonTrend = computed(() => shouldUseSparsePresentation(
  comparisonChartData.value.labels,
  subteamTrendSeries.value.length,
))
const sparseMemberTrend = computed(() => shouldUseSparsePresentation(
  memberChartData.value.labels,
  props.state.series.length,
))

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: { intersect: false, mode: 'index' as const },
  plugins: {
    legend: { display: false },
    tooltip: {
      callbacks: {
        label: (context: any) => `${context.dataset.label}: ${formatTokenTooltipValue(context.raw)} ${tokenUnitLabel.value}`,
      },
    },
  },
  scales: {
    y: {
      beginAtZero: true,
      title: {
        display: true,
        text: tokenUnitLabel.value,
        color: '#64748b',
      },
      ticks: {
        callback: (value: string | number) => formatTokenCount(Number(value)),
      },
    },
    x: {
      title: {
        display: true,
        text: timeDimensionLabel.value,
        color: '#64748b',
      },
    },
  },
}))

function buildTrendChartData(seriesItems: TrendChartSeries[]) {
  const labels = trendChartLabels(seriesItems)
  return {
    labels,
    datasets: seriesItems.map((series) => {
      const pointsByDate = new Map(series.points.map((point) => [point.date, point.total_tokens ?? null]))
      return {
        label: series.label,
        data: labels.map((date) => pointsByDate.get(date) ?? null),
        borderColor: series.color,
        backgroundColor: `${series.color}22`,
        borderWidth: series.borderWidth,
        tension: 0.25,
        spanGaps: true,
      }
    }),
  }
}

function trendChartLabels(seriesItems: TrendChartSeries[]) {
  const labels = new Set<string>()
  for (const series of seriesItems) {
    for (const point of series.points) {
      labels.add(point.date)
    }
  }
  return [...labels].sort()
}

function shouldUseSparsePresentation(labels: string[], seriesCount: number) {
  return labels.length > 0 && labels.length <= 2 && seriesCount <= 3
}

function sparsePointValue(point: TrendChartPoint) {
  if (point.total_tokens == null) return '-'
  return `${formatTokenCount(point.total_tokens)} ${tokenUnitLabel.value}`
}

function departmentChartSeries(series: TeamDepartmentTrendState['series'][number], index: number): TrendChartSeries {
  return {
    label: departmentSeriesLabel(series),
    points: series.points,
    color: departmentSeriesColor(series, index),
    borderWidth: series.series_type === 'team_total' ? 3 : 2,
  }
}

function memberChartSeries(series: TeamMemberTrendState['series'][number], index: number): TrendChartSeries {
  const color = seriesColors[index % seriesColors.length]
  return {
    label: `#${series.rank} ${series.display_name}`,
    points: series.points,
    color,
  }
}

function formatCost(value: number) {
  return value.toFixed(2)
}

function formatTokenTooltipValue(value: unknown) {
  if (value == null) return '-'
  const numericValue = Number(value)
  if (!Number.isFinite(numericValue)) return '-'
  return formatTokenCount(numericValue)
}

function reasonLabel(reason: string | null | undefined) {
  if (reason === 'scope_too_large') return t('teamUsage.scopeTooLarge')
  return t('teamUsage.unavailable')
}

function seriesTotalCost(series: { points: Array<{ actual_cost: number }> }) {
  return series.points.reduce((total, point) => total + point.actual_cost, 0)
}

function seriesTotalTokens(series: { points: Array<{ total_tokens?: number | null }> }) {
  let total = 0
  let hasValue = false
  for (const point of series.points) {
    if (point.total_tokens == null) continue
    total += point.total_tokens
    hasValue = true
  }
  return hasValue ? total : null
}

function departmentSeriesColor(series: TeamDepartmentTrendState['series'][number], index: number) {
  if (series.series_type === 'team_total') return departmentSeriesColors[0]
  return departmentSeriesColors[(departmentSeriesIndex(series, index) % (departmentSeriesColors.length - 1)) + 1]
}

function departmentSeriesLabel(series: TeamDepartmentTrendState['series'][number]) {
  if (series.series_type === 'team_total') return t('teamUsage.teamTotal')
  return series.display_name
}

function departmentSeriesKey(series: TeamDepartmentTrendState['series'][number], index: number) {
  if (series.series_type === 'team_total') return 'team-total'
  return `department:${series.department_external_id || series.display_name || index}`
}

function departmentSeriesIndex(series: TeamDepartmentTrendState['series'][number], fallbackIndex: number) {
  if (series.series_type === 'team_total') return 0
  const index = subteamTrendSeries.value.findIndex((candidate) => candidate === series)
  return index >= 0 ? index : fallbackIndex
}

function seriesKey(series: TeamMemberTrendState['series'][number]) {
  return series.user_id > 0 ? `user:${series.user_id}` : `directory:${series.directory_member_external_id || series.display_name}`
}
</script>

<template>
  <section data-testid="team-member-trend-chart" class="rounded-lg border border-slate-200 bg-white shadow-sm">
    <div class="border-b border-slate-200 px-4 py-3">
      <h2 class="text-base font-semibold text-slate-950">{{ t('teamUsage.topMembers') }}</h2>
      <div class="mt-1 flex flex-wrap gap-x-2 gap-y-1 text-xs font-medium text-slate-500">
        <template v-for="(item, index) in chartMetaItems" :key="item">
          <span>{{ item }}</span>
          <span v-if="index < chartMetaItems.length - 1" aria-hidden="true">·</span>
        </template>
      </div>
    </div>

    <div v-if="props.state.unavailable" class="px-4 py-4 text-sm text-slate-500">
      {{ reasonLabel(props.state.unavailable_reason) }}
    </div>

    <div v-else-if="!hasAnyTrendSeries" class="px-4 py-4 text-sm text-slate-500">
      -
    </div>

    <div v-else class="space-y-5 p-4">
      <div
        v-if="teamTotalTrendSeries.length > 0"
        data-testid="team-total-trend-chart"
        :class="sparseTeamTotalTrend ? '' : 'grid gap-4 xl:grid-cols-[minmax(0,1fr)_20rem]'"
      >
        <div class="min-w-0">
          <h3 class="text-sm font-semibold text-slate-900">{{ t('teamUsage.teamTotalTrend') }}</h3>
          <div v-if="teamTotalChartData.labels.length === 0" class="mt-2 flex h-52 items-center justify-center text-sm text-slate-500">
            -
          </div>
          <div
            v-else-if="sparseTeamTotalTrend"
            data-testid="team-total-trend-sparse"
            class="mt-2 divide-y divide-slate-200 border-y border-slate-200"
          >
            <div
              v-for="(series, index) in teamTotalTrendSeries"
              :key="departmentSeriesKey(series, index)"
              class="py-3 sm:flex sm:items-start sm:justify-between sm:gap-6"
            >
              <div>
                <div class="text-sm font-medium text-slate-900">{{ departmentSeriesLabel(series) }}</div>
                <div v-if="series.unavailable" class="mt-1 text-xs text-slate-500">{{ reasonLabel(series.unavailable_reason) }}</div>
                <div v-else class="mt-1 text-xs text-slate-500">
                  {{ formatTokenCount(seriesTotalTokens(series)) }} {{ tokenUnitLabel }} ·
                  {{ formatCost(seriesTotalCost(series)) }} {{ props.departmentTrend?.unit_label ?? props.state.unit_label }}
                </div>
              </div>
              <dl v-if="!series.unavailable" class="mt-2 grid min-w-56 gap-1 text-xs sm:mt-0">
                <div v-for="point in series.points" :key="point.date" class="flex items-center justify-between gap-6">
                  <dt class="text-slate-500">{{ point.date }}</dt>
                  <dd class="font-medium tabular-nums text-slate-900">{{ sparsePointValue(point) }}</dd>
                </div>
              </dl>
            </div>
          </div>
          <div v-else class="mt-2 h-52 min-w-0">
            <LineChartCanvas :data="teamTotalChartData" :options="chartOptions" />
          </div>
        </div>

        <div
          v-if="!sparseTeamTotalTrend"
          data-testid="team-total-trend-legend"
          :class="trendLegendPanelClasses"
        >
          <div
            v-for="(series, index) in teamTotalTrendSeries"
            :key="departmentSeriesKey(series, index)"
            class="flex items-start gap-3 px-3 py-2"
          >
            <span
              class="mt-1 h-2.5 w-2.5 shrink-0 rounded-full"
              :style="{ backgroundColor: series.unavailable ? '#94a3b8' : departmentSeriesColor(series, index) }"
            />
            <div class="min-w-0 flex-1">
              <div class="truncate text-sm font-medium text-slate-900">
                {{ departmentSeriesLabel(series) }}
              </div>
              <div v-if="series.unavailable" class="mt-0.5 text-xs text-slate-500">
                {{ reasonLabel(series.unavailable_reason) }}
              </div>
              <div v-else class="mt-0.5 flex flex-wrap gap-x-3 gap-y-1 text-xs text-slate-500">
                <span>{{ formatTokenCount(seriesTotalTokens(series)) }} {{ tokenUnitLabel }}</span>
                <span>{{ formatCost(seriesTotalCost(series)) }} {{ props.departmentTrend?.unit_label ?? props.state.unit_label }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <div
        v-if="subteamTrendSeries.length > 0"
        data-testid="team-comparison-trend-chart"
        :class="sparseComparisonTrend ? '' : 'grid gap-4 xl:grid-cols-[minmax(0,1fr)_20rem]'"
      >
        <div class="min-w-0">
          <h3 class="text-sm font-semibold text-slate-900">{{ t('teamUsage.subteamTrends') }}</h3>
          <div v-if="comparisonChartData.labels.length === 0" class="mt-2 flex h-64 items-center justify-center text-sm text-slate-500">
            -
          </div>
          <div
            v-else-if="sparseComparisonTrend"
            data-testid="team-comparison-trend-sparse"
            class="mt-2 divide-y divide-slate-200 border-y border-slate-200"
          >
            <div
              v-for="(series, index) in subteamTrendSeries"
              :key="departmentSeriesKey(series, index)"
              class="py-3 sm:flex sm:items-start sm:justify-between sm:gap-6"
            >
              <div>
                <div class="text-sm font-medium text-slate-900">{{ departmentSeriesLabel(series) }}</div>
                <div v-if="series.unavailable" class="mt-1 text-xs text-slate-500">{{ reasonLabel(series.unavailable_reason) }}</div>
                <div v-else class="mt-1 text-xs text-slate-500">
                  {{ formatTokenCount(seriesTotalTokens(series)) }} {{ tokenUnitLabel }} ·
                  {{ formatCost(seriesTotalCost(series)) }} {{ props.departmentTrend?.unit_label ?? props.state.unit_label }}
                </div>
              </div>
              <dl v-if="!series.unavailable" class="mt-2 grid min-w-56 gap-1 text-xs sm:mt-0">
                <div v-for="point in series.points" :key="point.date" class="flex items-center justify-between gap-6">
                  <dt class="text-slate-500">{{ point.date }}</dt>
                  <dd class="font-medium tabular-nums text-slate-900">{{ sparsePointValue(point) }}</dd>
                </div>
              </dl>
            </div>
          </div>
          <div v-else class="mt-2 h-64 min-w-0">
            <LineChartCanvas :data="comparisonChartData" :options="chartOptions" />
          </div>
        </div>

        <div
          v-if="!sparseComparisonTrend"
          data-testid="subteam-trend-legend"
          :class="trendLegendPanelClasses"
        >
          <div
            v-for="(series, index) in subteamTrendSeries"
            :key="departmentSeriesKey(series, index)"
            class="flex items-start gap-3 px-3 py-2"
          >
            <span
              class="mt-1 h-2.5 w-2.5 shrink-0 rounded-full"
              :style="{ backgroundColor: series.unavailable ? '#94a3b8' : departmentSeriesColor(series, index) }"
            />
            <div class="min-w-0 flex-1">
              <div class="truncate text-sm font-medium text-slate-900">
                {{ departmentSeriesLabel(series) }}
              </div>
              <div v-if="series.unavailable" class="mt-0.5 text-xs text-slate-500">
                {{ reasonLabel(series.unavailable_reason) }}
              </div>
              <div v-else class="mt-0.5 flex flex-wrap gap-x-3 gap-y-1 text-xs text-slate-500">
                <span>{{ formatTokenCount(seriesTotalTokens(series)) }} {{ tokenUnitLabel }}</span>
                <span>{{ formatCost(seriesTotalCost(series)) }} {{ props.departmentTrend?.unit_label ?? props.state.unit_label }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <div
        v-if="props.state.series.length > 0"
        data-testid="top-member-trend-chart"
        :class="sparseMemberTrend ? '' : 'grid gap-4 xl:grid-cols-[minmax(0,1fr)_20rem]'"
      >
        <div class="min-w-0">
          <h3 class="text-sm font-semibold text-slate-900">{{ t('teamUsage.topMembersLegend') }}</h3>
          <div v-if="memberChartData.labels.length === 0" class="mt-2 flex h-64 items-center justify-center text-sm text-slate-500">
            -
          </div>
          <div
            v-else-if="sparseMemberTrend"
            data-testid="top-member-trend-sparse"
            class="mt-2 divide-y divide-slate-200 border-y border-slate-200"
          >
            <div
              v-for="series in props.state.series"
              :key="seriesKey(series)"
              class="py-3 sm:flex sm:items-start sm:justify-between sm:gap-6"
            >
              <div>
                <div class="text-sm font-medium text-slate-900">#{{ series.rank }} {{ series.display_name }}</div>
                <div v-if="series.unavailable" class="mt-1 text-xs text-slate-500">{{ reasonLabel(series.unavailable_reason) }}</div>
                <div v-else class="mt-1 text-xs text-slate-500">
                  {{ formatTokenCount(seriesTotalTokens(series)) }} {{ tokenUnitLabel }} ·
                  {{ formatCost(seriesTotalCost(series)) }} {{ props.state.unit_label }}
                </div>
              </div>
              <dl v-if="!series.unavailable" class="mt-2 grid min-w-56 gap-1 text-xs sm:mt-0">
                <div v-for="point in series.points" :key="point.date" class="flex items-center justify-between gap-6">
                  <dt class="text-slate-500">{{ point.date }}</dt>
                  <dd class="font-medium tabular-nums text-slate-900">{{ sparsePointValue(point) }}</dd>
                </div>
              </dl>
            </div>
          </div>
          <div v-else class="mt-2 h-64 min-w-0">
            <LineChartCanvas :data="memberChartData" :options="chartOptions" />
          </div>
        </div>

        <div
          v-if="!sparseMemberTrend"
          data-testid="top-member-trend-legend"
          :class="trendLegendPanelClasses"
        >
          <div
            v-for="(series, index) in props.state.series"
            :key="seriesKey(series)"
            class="flex items-start gap-3 px-3 py-2"
          >
            <span
              class="mt-1 h-2.5 w-2.5 shrink-0 rounded-full"
              :style="{ backgroundColor: series.unavailable ? '#94a3b8' : seriesColors[index % seriesColors.length] }"
            />
            <div class="min-w-0 flex-1">
              <div class="truncate text-sm font-medium text-slate-900">
                #{{ series.rank }} {{ series.display_name }}
              </div>
              <div v-if="series.unavailable" class="mt-0.5 text-xs text-slate-500">
                {{ reasonLabel(series.unavailable_reason) }}
              </div>
              <div v-else class="mt-0.5 flex flex-wrap gap-x-3 gap-y-1 text-xs text-slate-500">
                <span>{{ formatTokenCount(seriesTotalTokens(series)) }} {{ tokenUnitLabel }}</span>
                <span>{{ formatCost(seriesTotalCost(series)) }} {{ props.state.unit_label }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>
