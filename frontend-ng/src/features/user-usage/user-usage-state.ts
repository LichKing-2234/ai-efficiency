import type { UserUsageDashboardParams, UserUsageTrendPoint } from '@/lib/api/types'
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
