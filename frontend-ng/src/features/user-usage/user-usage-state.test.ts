import { describe, expect, test } from 'vitest'
import { buildUsageDashboardParams, buildUsageHeatmapPoints, rangeLabelKey, usageTotalsFromTrend } from './user-usage-state'

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
    ])).toEqual({
      cacheCreationTokens: 3,
      cacheReadTokens: 5,
      inputTokens: 30,
      outputTokens: 11,
      requests: 5,
      tokens: 49,
      actualCost: 0.30000000000000004,
      standardCost: 0.5
    })
  })

  test('maps hourly trend data into heatmap day and hour buckets', () => {
    expect(buildUsageHeatmapPoints([
      { date: '2026-06-08T09:00:00+08:00', requests: 2, input_tokens: 0, output_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 20, cost: 0, actual_cost: 0 },
      { date: '2026-06-08T09:30:00+08:00', requests: 3, input_tokens: 0, output_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 30, cost: 0, actual_cost: 0 }
    ], 'hour')).toEqual([
      { day: 0, hour: 9, value: 5 }
    ])
  })

  test('spreads daily trend data across reference work hours', () => {
    expect(buildUsageHeatmapPoints([
      { date: '2026-06-08', requests: 12, input_tokens: 0, output_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 120, cost: 0, actual_cost: 0 }
    ], 'day')).toEqual([
      { day: 0, hour: 9, value: 2 },
      { day: 0, hour: 10, value: 2 },
      { day: 0, hour: 11, value: 2 },
      { day: 0, hour: 14, value: 2 },
      { day: 0, hour: 15, value: 2 },
      { day: 0, hour: 16, value: 2 }
    ])
  })
})
