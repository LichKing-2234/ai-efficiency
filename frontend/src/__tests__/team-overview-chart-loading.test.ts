import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import TeamOverviewMemberTrendChart from '@/components/team-usage/TeamOverviewMemberTrendChart.vue'
import { setLocale } from '@/i18n'
import type { TeamDepartmentTrendState, TeamMemberTrendState, TeamOverviewWindow } from '@/types'

const lineCanvasModule = vi.hoisted(() => {
  let gate = Promise.resolve()
  let release = () => {}

  return {
    loads: 0,
    defer() {
      gate = new Promise<void>((resolve) => { release = resolve })
    },
    wait: () => gate,
    resolve() {
      release()
      gate = Promise.resolve()
      release = () => {}
    },
  }
})

vi.mock('@/components/charts/LineChartCanvas.vue', async () => {
  lineCanvasModule.loads += 1
  await lineCanvasModule.wait()
  return {
    __esModule: true,
    default: {
      props: ['data', 'options'],
      template: '<div data-test="line-chart" :data-chart="JSON.stringify(data)" :data-options="JSON.stringify(options)" />',
    },
  }
})

const windowFixture: TeamOverviewWindow = {
  start_date: '2026-06-01',
  end_date: '2026-06-30',
  granularity: 'day',
  today: '2026-06-30',
  rolling_days: 30,
  timezone: 'Asia/Shanghai',
}

const emptyMemberTrend: TeamMemberTrendState = {
  unit_label: 'USD',
  rank_basis: 'range_total_tokens',
  unavailable: false,
  unavailable_reason: null,
  series: [],
}

const emptyDepartmentTrend: TeamDepartmentTrendState = {
  unit_label: 'USD',
  unavailable: false,
  unavailable_reason: null,
  comparison_total_count: 0,
  comparison_truncated: false,
  series: [],
}

describe('TeamOverviewMemberTrendChart loading', () => {
  beforeEach(() => {
    setLocale('en-US')
    vi.clearAllMocks()
  })

  afterEach(() => {
    lineCanvasModule.resolve()
  })

  it('loads one line canvas module only after split trend data is chartable', async () => {
    lineCanvasModule.defer()
    const wrapper = mount(TeamOverviewMemberTrendChart, {
      props: {
        state: emptyMemberTrend,
        departmentTrend: emptyDepartmentTrend,
        window: windowFixture,
      },
    })

    await flushPromises()
    expect(wrapper.text()).toContain('-')
    expect(lineCanvasModule.loads).toBe(0)

    await wrapper.setProps({
      state: {
        ...emptyMemberTrend,
        series: [{
          user_id: 101,
          display_name: 'Alice',
          rank: 1,
          unavailable: true,
          unavailable_reason: 'provider_error',
          points: [],
        }],
      },
      departmentTrend: {
        ...emptyDepartmentTrend,
        comparison_total_count: 1,
        series: [
          {
            series_type: 'team_total',
            display_name: 'Team total',
            unavailable: true,
            unavailable_reason: 'provider_error',
            points: [],
          },
          {
            series_type: 'department',
            department_external_id: 'team-one',
            display_name: 'Team One',
            rank: 1,
            unavailable: false,
            unavailable_reason: null,
            points: [],
          },
        ],
      },
    })

    await flushPromises()
    expect(wrapper.text()).toContain('Team total')
    expect(wrapper.text()).toContain('Team One')
    expect(wrapper.text()).toContain('#1 Alice')
    expect(wrapper.get('[data-testid="team-total-trend-legend"]').classes()).toContain('max-h-64')
    expect(wrapper.get('[data-testid="subteam-trend-legend"]').classes()).toContain('overflow-y-auto')
    expect(wrapper.get('[data-testid="top-member-trend-legend"]').classes()).toContain('max-h-64')
    expect(lineCanvasModule.loads).toBe(0)

    await wrapper.setProps({
      state: {
        ...emptyMemberTrend,
        series: [{
          user_id: 101,
          display_name: 'Alice',
          rank: 1,
          unavailable: false,
          unavailable_reason: null,
          points: [{ date: '2026-06-30', actual_cost: 1, total_tokens: 1000 }],
        }],
      },
      departmentTrend: {
        ...emptyDepartmentTrend,
        comparison_total_count: 1,
        series: [
          {
            series_type: 'team_total',
            display_name: 'Team total',
            unavailable: false,
            unavailable_reason: null,
            points: [{ date: '2026-06-30', actual_cost: 2, total_tokens: 2000 }],
          },
          {
            series_type: 'department',
            department_external_id: 'team-one',
            display_name: 'Team One',
            rank: 1,
            unavailable: false,
            unavailable_reason: null,
            points: [{ date: '2026-06-30', actual_cost: 1, total_tokens: 1000 }],
          },
        ],
      },
    })

    await vi.waitFor(() => expect(lineCanvasModule.loads).toBe(1))
    expect(wrapper.get('[data-testid="team-total-trend-chart"] .h-52').classes()).toContain('h-52')
    expect(wrapper.get('[data-testid="team-comparison-trend-chart"] .h-64').classes()).toContain('h-64')
    expect(wrapper.get('[data-testid="top-member-trend-chart"] .h-64').classes()).toContain('h-64')
    expect(wrapper.findAll('[data-test="line-chart"]')).toHaveLength(0)

    lineCanvasModule.resolve()
    await vi.waitFor(() => expect(wrapper.findAll('[data-test="line-chart"]')).toHaveLength(3))
  })
})
