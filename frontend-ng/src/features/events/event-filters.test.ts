import { describe, expect, test } from 'vitest'
import { buildEventQuery, eventDetailPrLabel, eventFiltersForRole, filterToSegment, getEventPagination, segmentToFilter } from './event-filters'

describe('buildEventQuery', () => {
  test('preserves backend event filter parameter names and converts local datetimes to ISO strings', () => {
    expect(buildEventQuery({
      from: '2026-06-01T08:30',
      to: '2026-06-02T18:45',
      tool: 'codex',
      bindingStatus: 'bound',
      q: 'org/repo',
      limit: 50,
      offset: 100,
      userId: 42
    })).toMatchObject({
      from: new Date('2026-06-01T08:30').toISOString(),
      to: new Date('2026-06-02T18:45').toISOString(),
      tool: 'codex',
      binding_status: 'bound',
      q: 'org/repo',
      limit: 50,
      offset: 100,
      user_id: 42
    })
  })

  test('omits pagination and empty filters when summary requests do not need them', () => {
    expect(buildEventQuery({
      from: '',
      to: '',
      tool: '',
      bindingStatus: '',
      q: '',
      limit: 20,
      offset: 0,
      userId: null
    }, { includePagination: false })).toEqual({})
  })
})

describe('getEventPagination', () => {
  test('calculates page bounds from backend total, limit, and offset', () => {
    expect(getEventPagination({ total: 125, limit: 50, offset: 50 })).toEqual({
      currentPage: 2,
      totalPages: 3,
      canGoPrev: true,
      canGoNext: true
    })
  })

  test('disables next page when offset reaches the final page', () => {
    expect(getEventPagination({ total: 60, limit: 50, offset: 50 })).toMatchObject({
      currentPage: 2,
      totalPages: 2,
      canGoNext: false
    })
  })
})

describe('eventFiltersForRole', () => {
  test('drops user filter for non-admin users before building backend query params', () => {
    const filters = {
      from: '',
      to: '',
      tool: '',
      bindingStatus: '',
      q: '',
      limit: 20,
      offset: 0,
      userId: 42
    }

    expect(buildEventQuery(eventFiltersForRole(filters, 'user'))).not.toHaveProperty('user_id')
    expect(buildEventQuery(eventFiltersForRole(filters, 'admin'))).toHaveProperty('user_id', 42)
  })
})

describe('event detail formatting', () => {
  test('formats matched PR labels with scm id, title, and status', () => {
    expect(eventDetailPrLabel({ pr_record_id: 10, scm_pr_id: 42, title: 'Improve attribution', status: 'merged', scm_pr_url: 'https://example.com/pr/42' })).toBe(
      '#42 Improve attribution · merged'
    )
  })
})

describe('segmented filter mapping', () => {
  test('uses all as the visible value for empty backend filters', () => {
    expect(filterToSegment('')).toBe('all')
    expect(filterToSegment('codex')).toBe('codex')
  })

  test('maps all back to an empty backend filter value', () => {
    expect(segmentToFilter('all')).toBe('')
    expect(segmentToFilter('bound')).toBe('bound')
  })
})
