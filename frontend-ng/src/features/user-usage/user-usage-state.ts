import type { UserUsageDashboardParams, UserUsageTrendPoint } from '@/lib/api/types'
import type { HeatmapPoint } from '@/components/primitives/heatmap-grid'
import type { MessageKey } from '@/lib/i18n/messages'

export type UsageRangeOption = 'today' | '7d' | '30d'

function formatDate(date: Date) {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

export function buildUsageDashboardParams(
  range: UsageRangeOption,
  now = new Date(),
  timezone = Intl.DateTimeFormat().resolvedOptions().timeZone
): UserUsageDashboardParams {
  const end = new Date(now)
  const start = new Date(now)
  if (range === 'today') {
    return { start_date: formatDate(start), end_date: formatDate(end), granularity: 'hour', timezone }
  }
  start.setDate(end.getDate() - (range === '7d' ? 6 : 29))
  return { start_date: formatDate(start), end_date: formatDate(end), granularity: 'day', timezone }
}

export function rangeLabelKey(range: UsageRangeOption): MessageKey {
  if (range === 'today') return 'usageDashboard.today'
  if (range === '7d') return 'usageDashboard.sevenDays'
  return 'usageDashboard.thirtyDays'
}

export function usageTotalsFromTrend(points: UserUsageTrendPoint[]) {
  return points.reduce(
    (next, point) => ({
      requests: next.requests + point.requests,
      tokens: next.tokens + point.total_tokens,
      actualCost: next.actualCost + point.actual_cost,
      standardCost: next.standardCost + point.cost
    }),
    { requests: 0, tokens: 0, actualCost: 0, standardCost: 0 }
  )
}

const dayWorkHours = [9, 10, 11, 14, 15, 16]

export function buildUsageHeatmapPoints(points: UserUsageTrendPoint[], granularity: string | undefined): HeatmapPoint[] {
  const buckets = new Map<string, HeatmapPoint>()
  const visiblePoints = points.slice(-168)
  const dayKeys = [...new Set(visiblePoints.map((point) => heatmapDateKey(point.date)))].slice(-7)
  const dayIndex = new Map(dayKeys.map((key, index) => [key, index]))
  visiblePoints.forEach((point) => {
    const day = dayIndex.get(heatmapDateKey(point.date))
    if (day == null) return
    if (granularity === 'hour') {
      addHeatmapValue(buckets, day, heatmapHour(point.date), point.requests)
      return
    }

    const base = Math.floor(point.requests / dayWorkHours.length)
    const remainder = point.requests % dayWorkHours.length
    dayWorkHours.forEach((hour, hourIndex) => {
      addHeatmapValue(buckets, day, hour, base + (hourIndex < remainder ? 1 : 0))
    })
  })
  return [...buckets.values()].filter((point) => point.value > 0)
}

function heatmapDateKey(value: string) {
  return value.slice(0, 10)
}

function heatmapHour(value: string) {
  const match = value.match(/T(\d{2})/)
  return match ? Number(match[1]) : 12
}

function addHeatmapValue(buckets: Map<string, HeatmapPoint>, day: number, hour: number, value: number) {
  if (day < 0 || day > 6 || hour < 0 || hour > 23 || value <= 0) return
  const key = `${day}:${hour}`
  const current = buckets.get(key)
  buckets.set(key, { day, hour, value: (current?.value ?? 0) + value })
}
