import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import UsageTrendChart from '@/components/user/usage/UsageTrendChart.vue'
import TeamOverviewMemberTable from '@/components/team-usage/TeamOverviewMemberTable.vue'
import TeamOverviewMemberTrendChart from '@/components/team-usage/TeamOverviewMemberTrendChart.vue'
import { setLocale } from '@/i18n'

vi.mock('@/components/charts/LineChartCanvas.vue', () => ({
  __esModule: true,
  default: {
    props: ['data', 'options'],
    template: '<div data-testid="line-chart" :data-chart="JSON.stringify(data)" />',
  },
}))

const tokenTrend = [
  { date: '2026-11-13', requests: 2, input_tokens: 100, output_tokens: 20, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 120, cost: 0.01, actual_cost: 0.01 },
]

const kiroByDay = [
  { date: '2026-11-13', requests: 3, credits: 0.06 },
]

const member = {
  user_id: 1,
  display_name: 'Alice',
  email: 'alice@example.com',
  department_display_path: 'Department Alpha',
  relay_user_id: 1,
  total_tokens: 1000,
  range_actual_cost: 1,
  today_actual_cost: 0,
  total_actual_cost: 1,
  selectable: true,
  subscription_count: 1,
}

const creditsByUser = {
  1: {
    user_id: 1,
    relay_user_id: 1,
    key_found: true,
    credits: 12.5,
    credits_saved: 1,
    requests: 3,
    input_tokens: 100,
    output_tokens: 10,
    by_model: [],
    by_day: [],
    partial: false,
  },
}

async function toggleToCredits(wrapper: ReturnType<typeof mount>) {
  await wrapper.findAll('label').find((label) => label.text() === 'Credits')!.find('input[type="radio"]').setValue(true)
  await nextTick()
  await flushPromises()
}

describe('token/credits switches', () => {
  beforeEach(async () => {
    localStorage.clear()
    await setLocale('en-US')
  })

  it('switches the personal trend to credits and requests', async () => {
    const wrapper = mount(UsageTrendChart, {
      props: { data: tokenTrend, loading: false, kiroByDay, kiroLoading: false, kiroAvailable: true },
    })
    await flushPromises()
    await vi.dynamicImportSettled()
    expect(wrapper.text()).toContain('Usage trend')

    await toggleToCredits(wrapper)

    const chart = wrapper.get('[data-testid="line-chart"]')
    expect(chart.attributes('data-chart')).toContain('0.06')
    expect(chart.attributes('data-chart')).toContain('Credits')
  })

  it('shows a credits ranking for the team member table', async () => {
    const wrapper = mount(TeamOverviewMemberTable, {
      props: {
        members: [member],
        creditsByUser,
        creditsByDepartment: { 'department-alpha': 12.5 },
        organizationBranchFor: () => undefined,
      },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Token/Credits usage in range')
    expect(wrapper.text()).toContain('1K/12.50')

    await wrapper.get('[data-testid="team-overview-credits-view"]').find('input[type="radio"]').setValue(true)
    await flushPromises()

    expect(wrapper.get('[data-testid="team-overview-credits-ranking-table"]').text()).toContain('12.50')
    expect(wrapper.text()).toContain('Credits')
  })

  it('switches the team trend to member credits', async () => {
    const wrapper = mount(TeamOverviewMemberTrendChart, {
      props: {
        state: { unit_label: 'tokens', rank_basis: 'range_total_tokens', unavailable: false, unavailable_reason: null, series: [] },
        departmentTrend: null,
        window: { start_date: '2026-11-13', end_date: '2026-11-13', granularity: 'day', today: '2026-11-13', rolling_days: 7, timezone: 'UTC' },
        tokensTotal: 1000,
        creditsTotal: 12.5,
        creditsUsers: [{
          user_id: 1,
          relay_user_id: 1,
          key_found: true,
          credits: 12.5,
          credits_saved: 0,
          requests: 3,
          input_tokens: 100,
          output_tokens: 10,
          by_model: [],
          by_day: [{ date: '2026-11-13', requests: 3, credits: 12.5 }],
          partial: false,
          display_name: 'Alice',
        }],
      },
    })
    await flushPromises()
    await vi.dynamicImportSettled()

    await toggleToCredits(wrapper)

    const chart = wrapper.get('[data-testid="line-chart"]')
    expect(chart.attributes('data-chart')).toContain('Alice')
    expect(chart.attributes('data-chart')).toContain('12.5')
  })
})
