<script setup lang="ts">
import { computed } from 'vue'
import {
  CategoryScale,
  Chart as ChartJS,
  Legend,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip,
} from 'chart.js'
import { Line } from 'vue-chartjs'
import { useI18n } from '@/i18n'
import { formatTokenCount } from '@/utils/formatters'
import type { TeamMemberTrendState, TeamOverviewWindow } from '@/types'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend)

const props = defineProps<{
  state: TeamMemberTrendState
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

const chartableSeries = computed(() => {
  return props.state.series.filter((series) => !series.unavailable && series.points.length > 0)
})

const chartLabels = computed(() => {
  const labels = new Set<string>()
  for (const series of chartableSeries.value) {
    for (const point of series.points) {
      labels.add(point.date)
    }
  }
  return [...labels].sort()
})

const timeDimensionLabel = computed(() => {
  if (props.window?.granularity === 'hour') return t('teamUsage.hourly')
  return t('teamUsage.daily')
})

const windowRangeLabel = computed(() => {
  if (!props.window?.start_date || !props.window.end_date) return ''
  return `${props.window.start_date} - ${props.window.end_date}`
})

const chartMetaItems = computed(() => {
  const items = [props.state.unit_label, timeDimensionLabel.value]
  if (windowRangeLabel.value) items.push(windowRangeLabel.value)
  if (props.window?.timezone) items.push(props.window.timezone)
  return items
})

const chartData = computed(() => ({
  labels: chartLabels.value,
  datasets: chartableSeries.value.map((series, index) => {
    const pointsByDate = new Map(series.points.map((point) => [point.date, point.actual_cost]))
    const color = seriesColors[index % seriesColors.length]
    return {
      label: `#${series.rank} ${series.display_name}`,
      data: chartLabels.value.map((date) => pointsByDate.get(date) ?? null),
      borderColor: color,
      backgroundColor: `${color}22`,
      tension: 0.25,
      spanGaps: true,
    }
  }),
}))

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: { intersect: false, mode: 'index' as const },
  plugins: {
    legend: { display: false },
    tooltip: {
      callbacks: {
        label: (context: any) => `${context.dataset.label}: ${formatCost(Number(context.raw ?? 0))} ${props.state.unit_label}`,
      },
    },
  },
  scales: {
    y: {
      beginAtZero: true,
      title: {
        display: true,
        text: props.state.unit_label,
        color: '#64748b',
      },
      ticks: {
        callback: (value: string | number) => `${Number(value).toFixed(2)}`,
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

function formatCost(value: number) {
  return value.toFixed(2)
}

function reasonLabel(reason: string | null | undefined) {
  if (reason === 'scope_too_large') return t('teamUsage.scopeTooLarge')
  return t('teamUsage.unavailable')
}

function seriesTotalCost(series: TeamMemberTrendState['series'][number]) {
  return series.points.reduce((total, point) => total + point.actual_cost, 0)
}

function seriesTotalTokens(series: TeamMemberTrendState['series'][number]) {
  let total = 0
  let hasValue = false
  for (const point of series.points) {
    if (point.total_tokens == null) continue
    total += point.total_tokens
    hasValue = true
  }
  return hasValue ? total : null
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

    <div v-else-if="props.state.series.length === 0" class="px-4 py-4 text-sm text-slate-500">
      -
    </div>

    <div v-else class="grid gap-4 p-4 xl:grid-cols-[minmax(0,1fr)_20rem]">
      <div v-if="chartLabels.length === 0" class="flex h-72 items-center justify-center text-sm text-slate-500">
        -
      </div>
      <div v-else class="h-72 min-w-0">
        <Line :data="chartData" :options="chartOptions" />
      </div>

      <div class="min-w-0 divide-y divide-slate-100 rounded-md border border-slate-200">
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
              <span>{{ formatCost(seriesTotalCost(series)) }} {{ props.state.unit_label }}</span>
              <span>{{ formatTokenCount(seriesTotalTokens(series)) }} {{ t('teamUsage.tokens') }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>
