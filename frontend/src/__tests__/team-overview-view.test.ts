import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import TeamOverviewMemberTrendChart from '@/components/team-usage/TeamOverviewMemberTrendChart.vue'
import TeamOverviewView from '@/views/TeamOverviewView.vue'
import { setLocale } from '@/i18n'
import type { TeamOverviewResponse } from '@/types'

vi.mock('@/api/teamUsage', () => ({
  getTeamUsageOverview: vi.fn(),
}))

vi.mock('vue-chartjs', () => ({
  Line: { template: '<canvas data-test="line-chart" />' },
}))

const mockGetTeamUsageOverview = vi.mocked((await import('@/api/teamUsage')).getTeamUsageOverview)

const overviewFixture: TeamOverviewResponse = {
  configured: true,
  is_representative: true,
  window: {
    start_date: '2026-06-01',
    end_date: '2026-06-30',
    granularity: 'day',
    today: '2026-06-28',
    rolling_days: 30,
    timezone: 'Asia/Shanghai',
  },
  summary: {
    unavailable: false,
    unavailable_reason: null,
    member_count: 2,
    relay_member_count: 1,
    range_actual_cost: 24.5,
    today_actual_cost: 1.25,
    total_actual_cost: 24.5,
    unit_label: 'USD',
  },
  top_members: [
    {
      rank: 1,
      user_id: 101,
      display_name: 'Alice',
      email: 'alice@example.com',
      department_display_path: 'Department Alpha',
      relay_user_id: 1001,
      range_actual_cost: 24.5,
      today_actual_cost: 1.25,
      total_actual_cost: 24.5,
      total_tokens: 12000,
      subscription_count: 2,
      selectable: true,
    },
  ],
  top_member_trend: {
    unit_label: 'USD',
    rank_basis: 'range_actual_cost',
    unavailable: false,
    unavailable_reason: null,
    series: [
      {
        user_id: 101,
        display_name: 'Alice',
        rank: 1,
        unavailable: false,
        unavailable_reason: null,
        points: [
          { date: '2026-06-27', actual_cost: 0.75, total_tokens: 5000 },
          { date: '2026-06-28', actual_cost: 1.25, total_tokens: 7000 },
        ],
      },
    ],
  },
  members: [
    {
      rank: 1,
      user_id: 101,
      display_name: 'Alice',
      email: 'alice@example.com',
      department_display_path: 'Department Alpha',
      relay_user_id: 1001,
      range_actual_cost: 24.5,
      today_actual_cost: 1.25,
      total_actual_cost: 24.5,
      total_tokens: 12000,
      subscription_count: 2,
      selectable: true,
    },
    {
      rank: 2,
      user_id: 0,
      directory_member_external_id: 'member-bob',
      display_name: 'Bob',
      email: 'bob@example.org',
      department_display_path: 'Department Alpha / Department Beta',
      relay_user_id: null,
      range_actual_cost: 3.5,
      today_actual_cost: 0,
      total_actual_cost: 3.5,
      total_tokens: 900,
      subscription_count: null,
      selectable: false,
    },
    {
      rank: 3,
      user_id: 0,
      directory_member_external_id: 'member-carol',
      display_name: 'Carol',
      email: 'carol@example.net',
      department_display_path: 'Department Alpha / Department Beta',
      relay_user_id: null,
      range_actual_cost: 0,
      today_actual_cost: 0,
      total_actual_cost: 0,
      total_tokens: null,
      subscription_count: null,
      selectable: false,
    },
  ],
}

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div>Dashboard</div>' } },
      { path: '/usage', name: 'Usage', component: { template: '<div>Usage</div>' } },
      { path: '/usage/members/:user_id', name: 'UsageMember', component: { template: '<div>Member Usage</div>' } },
      { path: '/usage/team', name: 'UsageTeam', component: TeamOverviewView },
      { path: '/user', component: { template: '<div>User</div>' } },
      { path: '/events', component: { template: '<div>Events</div>' } },
      { path: '/repos', component: { template: '<div>Repos</div>' } },
    ],
  })
}

describe('TeamOverviewView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    setLocale('en-US')
    vi.clearAllMocks()
  })

  it('renders top member trend and member table without quota controls', async () => {
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(mockGetTeamUsageOverview).toHaveBeenCalledWith(expect.objectContaining({ granularity: 'day' }))
    expect(wrapper.text()).toContain('Top 12 billing trend')
    expect(wrapper.find('header h1').exists()).toBe(false)
    expect(wrapper.find('[data-test="line-chart"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Alice')
    expect(wrapper.text()).toContain('Billed usage in range')
    expect(wrapper.text()).not.toContain('Used / Quota')
    expect(wrapper.text()).not.toContain('Rate multiplier')
  })

  it('renders each top member once in the top trend chart area', async () => {
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const trend = wrapper.get('[data-testid="team-member-trend-chart"]')
    expect(trend.findAll('table')).toHaveLength(0)
    expect(trend.text().match(/Alice/g)).toHaveLength(1)
  })

  it('renders the Top 12 trend unit and time window next to the chart title', async () => {
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const trend = wrapper.get('[data-testid="team-member-trend-chart"]')
    expect(trend.text()).toContain('USD')
    expect(trend.text()).toContain('Daily')
    expect(trend.text()).toContain('2026-06-01 - 2026-06-30')
    expect(trend.text()).toContain('Asia/Shanghai')
  })

  it('renders team overview chrome in Chinese without English table labels', async () => {
    setLocale('zh-CN')
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('团队概览')
    expect(wrapper.find('header h1').exists()).toBe(false)
    expect(wrapper.text()).toContain('姓名')
    expect(wrapper.text()).toContain('邮箱')
    expect(wrapper.text()).toContain('部门')
    expect(wrapper.text()).toContain('打开')
    expect(wrapper.text()).toContain('Token')
    expect(wrapper.text()).toContain('团队人数')
    expect(wrapper.text()).toContain('已接入人数')
    expect(wrapper.text()).toContain('24.50 USD')
    const tableHeaders = wrapper.findAll('th').map((header) => header.text())
    expect(tableHeaders).toEqual(['姓名', '邮箱', '部门', '当前范围计费用量', '操作'])
    expect(tableHeaders).not.toContain('Name')
    expect(tableHeaders).not.toContain('Email')
    expect(tableHeaders).not.toContain('Department')
    expect(tableHeaders).not.toContain('订阅数')
    expect(tableHeaders).not.toContain('Subscriptions')
    expect(tableHeaders).not.toContain('Action')
    expect(wrapper.findAll('button').map((button) => button.text())).not.toContain('Open')
    const trend = wrapper.get('[data-testid="team-member-trend-chart"]')
    expect(trend.text()).toContain('USD')
    expect(trend.text()).toContain('按天')
    expect(trend.text()).toContain('2026-06-01 - 2026-06-30')
    expect(trend.text()).toContain('Asia/Shanghai')
    expect(trend.text()).not.toContain('tokens')
  })

  it('requests an explicit 30-day overview window on first load', async () => {
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const params = mockGetTeamUsageOverview.mock.calls[0][0] as {
      start_date?: string
      end_date?: string
      granularity?: string
      timezone?: string
    }
    expect(params).toEqual(expect.objectContaining({
      granularity: 'day',
      start_date: expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/),
      end_date: expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/),
      timezone: expect.any(String),
    }))

    const start = new Date(`${params.start_date}T00:00:00Z`)
    const end = new Date(`${params.end_date}T00:00:00Z`)
    const days = Math.round((end.getTime() - start.getTime()) / 86_400_000) + 1
    expect(days).toBe(30)
  })

  it('switches Team Overview between today, 7-day, and 30-day windows', async () => {
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.find('[data-test="range-today"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="range-7d"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="range-30d"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Billed usage in range')

    mockGetTeamUsageOverview.mockClear()
    await wrapper.get('[data-test="range-7d"]').trigger('click')
    await flushPromises()

    const params = mockGetTeamUsageOverview.mock.calls[0][0] as {
      start_date?: string
      end_date?: string
      granularity?: string
    }
    expect(params.granularity).toBe('day')
    const start = new Date(`${params.start_date}T00:00:00Z`)
    const end = new Date(`${params.end_date}T00:00:00Z`)
    const days = Math.round((end.getTime() - start.getTime()) / 86_400_000) + 1
    expect(days).toBe(7)
  })

  it('shows a refresh loading state while keeping the previous Team Overview data visible', async () => {
    mockGetTeamUsageOverview.mockResolvedValueOnce({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    let resolveRefresh: (value: any) => void = () => {}
    mockGetTeamUsageOverview.mockImplementationOnce(() => new Promise((resolve) => {
      resolveRefresh = resolve
    }) as any)

    await wrapper.get('[data-test="range-7d"]').trigger('click')

    expect(wrapper.get('[data-test="range-7d"]').classes()).toContain('bg-blue-600')
    expect(wrapper.get('[data-testid="team-overview-refreshing"]').text()).toContain('Updating team usage...')
    expect(wrapper.get('[data-testid="team-overview-content"]').attributes('aria-busy')).toBe('true')
    expect(wrapper.text()).toContain('Alice')

    resolveRefresh({ data: { data: overviewFixture } })
    await flushPromises()

    expect(wrapper.find('[data-testid="team-overview-refreshing"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="team-overview-content"]').attributes('aria-busy')).toBe('false')
  })

  it('omits subscription count from team member details', async () => {
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.findAll('th').map((header) => header.text())).not.toContain('Subscriptions')
    const bobRow = wrapper.findAll('tbody tr').find((row) => row.text().includes('Bob'))
    if (!bobRow) throw new Error('expected Bob row to be rendered')
    expect(bobRow.text()).toContain('3.50 USD')
  })

  it('disables member open action when member usage is not selectable', async () => {
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Carol')
    const rows = wrapper.findAll('tbody tr')
    const aliceRow = rows.find((row) => row.text().includes('Alice'))
    const bobRow = rows.find((row) => row.text().includes('Bob'))
    const carolRow = rows.find((row) => row.text().includes('Carol'))
    if (!aliceRow || !bobRow || !carolRow) throw new Error('expected Alice, Bob, and Carol rows to be rendered')
    expect(aliceRow.find('button').attributes('disabled')).toBeUndefined()
    expect(bobRow.find('button').attributes('disabled')).toBeDefined()
    expect(carolRow.find('button').attributes('disabled')).toBeDefined()
  })

  it('renders scope-too-large warning when trend reason is scope too large', async () => {
    mockGetTeamUsageOverview.mockResolvedValue({
      data: {
        data: {
          ...overviewFixture,
          summary: {
            ...overviewFixture.summary,
            unavailable_reason: 'relay_unavailable',
          },
          top_member_trend: {
            ...overviewFixture.top_member_trend,
            unavailable_reason: 'scope_too_large',
          },
        },
      },
    } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Team usage is unavailable for this scope size.')
  })

  it('renders no-scope state when overview load is rejected with 403', async () => {
    mockGetTeamUsageOverview.mockRejectedValue({
      response: { status: 403, data: { code: 'not_representative' } },
    })
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('No delegated team scope')
  })

  it('renders unavailable state when overview load fails for another reason', async () => {
    mockGetTeamUsageOverview.mockRejectedValue(new Error('network unavailable'))
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Team usage is temporarily unavailable.')
    expect(wrapper.text()).not.toContain('network unavailable')
  })

  it('routes Open action to AI Usage Center with selected subject query', async () => {
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const openButton = wrapper.findAll('button').find((button) => button.text() === 'Open')
    expect(openButton).toBeTruthy()
    await openButton!.trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.path).toBe('/usage/members/101')
    expect(router.currentRoute.value.query).toEqual({})
  })
})

describe('TeamOverviewMemberTrendChart', () => {
  beforeEach(() => {
    setLocale('en-US')
  })

  it('keeps unavailable member series visible with a user-facing reason', () => {
    const wrapper = mount(TeamOverviewMemberTrendChart, {
      props: {
        state: {
          unit_label: 'USD',
          rank_basis: 'range_actual_cost',
          unavailable: false,
          unavailable_reason: null,
          series: [
            {
              user_id: 103,
              display_name: 'Charlie',
              rank: 3,
              unavailable: true,
              unavailable_reason: 'provider_error',
              points: [],
            },
          ],
        },
      },
    })

    expect(wrapper.text()).toContain('Charlie')
    expect(wrapper.text()).toContain('Team usage is temporarily unavailable.')
    expect(wrapper.text()).not.toContain('provider_error')
  })
})
