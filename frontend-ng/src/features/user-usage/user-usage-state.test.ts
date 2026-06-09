import { describe, expect, test } from 'vitest'
import { buildUsageDashboardParams, rangeLabelKey, usageTotalsFromTrend } from './user-usage-state'

describe('user usage state', () => {
  test('builds today as hourly range using local date', () => {
    const params = buildUsageDashboardParams('today', new Date('2026-06-09T10:00:00+08:00'), 'Asia/Shanghai')
    expect(params).toEqual({
      start_date: '2026-06-09',
      end_date: '2026-06-09',
      granularity: 'hour',
      timezone: 'Asia/Shanghai'
    })
  })

  test('builds 7 day and 30 day inclusive day ranges', () => {
    expect(buildUsageDashboardParams('7d', new Date('2026-06-09T10:00:00+08:00'), 'Asia/Shanghai')).toMatchObject({
      start_date: '2026-06-03',
      end_date: '2026-06-09',
      granularity: 'day'
    })
    expect(buildUsageDashboardParams('30d', new Date('2026-06-09T10:00:00+08:00'), 'Asia/Shanghai')).toMatchObject({
      start_date: '2026-05-11',
      end_date: '2026-06-09',
      granularity: 'day'
    })
  })

  test('maps range label keys and sums trend data', () => {
    expect(rangeLabelKey('today')).toBe('usageDashboard.today')
    expect(usageTotalsFromTrend([
      { date: '2026-06-08', requests: 2, input_tokens: 10, output_tokens: 5, cache_creation_tokens: 1, cache_read_tokens: 2, total_tokens: 18, cost: 0.2, actual_cost: 0.1 },
      { date: '2026-06-09', requests: 3, input_tokens: 20, output_tokens: 6, cache_creation_tokens: 2, cache_read_tokens: 3, total_tokens: 31, cost: 0.3, actual_cost: 0.2 }
    ])).toEqual({ requests: 5, tokens: 49, actualCost: 0.30000000000000004, standardCost: 0.5 })
  })
})
