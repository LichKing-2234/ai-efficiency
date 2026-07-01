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
  Line: {
    props: ['data', 'options'],
    template: '<div data-test="line-chart" :data-chart="JSON.stringify(data)" :data-options="JSON.stringify(options)" />',
  },
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
    member_count: 3,
    relay_member_count: 2,
    range_actual_cost: 28,
    range_total_tokens: 12900,
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
      department_external_id: 'department-alpha',
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
    rank_basis: 'range_total_tokens',
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
      department_external_id: 'department-alpha',
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
      department_external_id: 'department-alpha-team-one',
      department_display_path: 'Department Alpha / Team One',
      relay_user_id: 1002,
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
      department_external_id: 'department-alpha-team-one',
      department_display_path: 'Department Alpha / Team One',
      relay_user_id: null,
      range_actual_cost: 0,
      today_actual_cost: 0,
      total_actual_cost: 0,
      total_tokens: null,
      subscription_count: null,
      selectable: false,
    },
  ],
  member_tree: [
    {
      department_external_id: 'department-alpha',
      name: 'Department Alpha',
      display_path: 'Department Alpha',
      depth: 0,
      child_count: 1,
      member_count: 3,
      connected_member_count: 2,
      range_actual_cost: 28,
      range_total_tokens: 12900,
      members: [
        {
          rank: 1,
          user_id: 101,
          display_name: 'Alice',
          email: 'alice@example.com',
          department_external_id: 'department-alpha',
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
      children: [
        {
          department_external_id: 'department-alpha-team-one',
          parent_external_id: 'department-alpha',
          name: 'Team One',
          display_path: 'Department Alpha / Team One',
          depth: 1,
          child_count: 0,
          member_count: 2,
          connected_member_count: 1,
          range_actual_cost: 3.5,
          range_total_tokens: 900,
          members: [
            {
              rank: 2,
              user_id: 0,
              directory_member_external_id: 'member-bob',
              display_name: 'Bob',
              email: 'bob@example.org',
              department_external_id: 'department-alpha-team-one',
              department_display_path: 'Department Alpha / Team One',
              relay_user_id: 1002,
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
              department_external_id: 'department-alpha-team-one',
              department_display_path: 'Department Alpha / Team One',
              relay_user_id: null,
              range_actual_cost: 0,
              today_actual_cost: 0,
              total_actual_cost: 0,
              total_tokens: null,
              subscription_count: null,
              selectable: false,
            },
          ],
          children: [],
        },
      ],
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
    expect(wrapper.text()).toContain('Top 12 token usage trend')
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
    expect(trend.text()).toContain('tokens')
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
    expect(wrapper.text()).toContain('当前范围 Token 用量')
    expect(wrapper.text()).toContain('打开')
    expect(wrapper.text()).toContain('Token')
    expect(wrapper.text()).toContain('团队人数')
    expect(wrapper.text()).toContain('已接入人数')
    expect(wrapper.text()).toContain('24.50 USD')
    expect(wrapper.text()).not.toContain('Name')
    expect(wrapper.text()).not.toContain('Email')
    expect(wrapper.text()).not.toContain('Action')
    expect(wrapper.text()).not.toContain('订阅数')
    expect(wrapper.text()).not.toContain('Subscriptions')
    expect(wrapper.findAll('button').map((button) => button.text())).not.toContain('Open')
    const trend = wrapper.get('[data-testid="team-member-trend-chart"]')
    expect(trend.text()).toContain('Token')
    expect(trend.text()).toContain('按天')
    expect(trend.text()).toContain('2026-06-01 - 2026-06-30')
    expect(trend.text()).toContain('Asia/Shanghai')
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

    expect(wrapper.text()).not.toContain('Subscriptions')
    const bobRow = wrapper.get('[data-testid="team-overview-member-directory-member-bob"]')
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
    const aliceRow = wrapper.get('[data-testid="team-overview-member-user-101"]')
    const bobRow = wrapper.get('[data-testid="team-overview-member-directory-member-bob"]')
    const carolRow = wrapper.get('[data-testid="team-overview-member-directory-member-carol"]')
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

  it('renders partial unavailable warning while keeping team content visible', async () => {
    mockGetTeamUsageOverview.mockResolvedValue({
      data: {
        data: {
          ...overviewFixture,
          summary: {
            ...overviewFixture.summary,
            unavailable: true,
            unavailable_reason: 'provider_error',
            range_actual_cost: null,
            range_total_tokens: null,
          },
          top_member_trend: {
            ...overviewFixture.top_member_trend,
            unavailable: true,
            unavailable_reason: 'provider_error',
            series: [],
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

    expect(wrapper.text()).toContain('Selected range totals are temporarily unavailable.')
    expect(wrapper.text()).toContain('Team members')
    expect(wrapper.text()).toContain('Alice')
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

  it('shows selected-window token usage in summary and member details', async () => {
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Token usage in range')
    expect(wrapper.text()).toContain('12.9K')
    const aliceRow = wrapper.get('[data-testid="team-overview-member-user-101"]')
    expect(aliceRow.text()).toContain('12K')
    const bobRow = wrapper.get('[data-testid="team-overview-member-directory-member-bob"]')
    expect(bobRow.text()).toContain('900')
    const carolRow = wrapper.get('[data-testid="team-overview-member-directory-member-carol"]')
    expect(carolRow.text()).toContain('-')
  })

  it('abbreviates large token totals in summary trend legend and member details', async () => {
    const largeTokenFixture: TeamOverviewResponse = structuredClone(overviewFixture)
    largeTokenFixture.summary.range_total_tokens = 12_285_557_755
    largeTokenFixture.top_member_trend.series[0].points = [
      { date: '2026-06-27', actual_cost: 0.75, total_tokens: 3_000_000_000 },
      { date: '2026-06-28', actual_cost: 1.25, total_tokens: 3_052_813_773 },
    ]
    largeTokenFixture.members[0].total_tokens = 805_033_680
    largeTokenFixture.member_tree![0].range_total_tokens = 12_285_557_755
    largeTokenFixture.member_tree![0].members[0].total_tokens = 805_033_680
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: largeTokenFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('12.29B')
    expect(wrapper.text()).toContain('6.05B tokens')
    const ranking = wrapper.get('[data-testid="team-overview-ranking-table"]')
    expect(ranking.text()).toContain('805.03M')
    await wrapper.get('[data-testid="team-overview-organization-view"]').trigger('click')
    const alpha = wrapper.get('[data-testid="team-overview-department-department-alpha"]')
    expect(alpha.text()).toContain('12.29B tokens')
    const aliceRow = wrapper.get('[data-testid="team-overview-member-user-101"]')
    expect(aliceRow.text()).toContain('805.03M')
    expect(wrapper.text()).not.toContain('12,285,557,755')
    expect(wrapper.text()).not.toContain('805,033,680')
  })

  it('defaults member details to ranking view and switches to an admin-style organization tree', async () => {
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.get('[data-testid="team-overview-ranking-view"]').classes()).toContain('bg-gray-900')
    expect(wrapper.get('[data-testid="team-overview-organization-view"]').classes()).not.toContain('bg-gray-900')
    expect(wrapper.get('[data-testid="team-overview-ranking-table"]').text()).toContain('Alice')
    expect(wrapper.find('[data-testid="team-overview-department-department-alpha"]').exists()).toBe(false)

    await wrapper.get('[data-testid="team-overview-organization-view"]').trigger('click')

    const alpha = wrapper.get('[data-testid="team-overview-department-department-alpha"]')
    expect(alpha.attributes('role')).toBe('treeitem')
    expect(alpha.attributes('aria-level')).toBe('1')
    expect(alpha.attributes('tabindex')).toBe('0')
    expect(alpha.classes()).toContain('cursor-pointer')
    expect(alpha.text()).toContain('Department Alpha')
    expect(alpha.text()).toContain('3 members')
    expect(alpha.text()).toContain('2 connected')
    expect(alpha.text()).toContain('28.00 USD')
    expect(alpha.text()).toContain('12.9K tokens')
    const alphaToggle = wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]')
    expect(alphaToggle.text()).toBe('-')
    expect(alphaToggle.classes()).toContain('h-7')
    expect(alphaToggle.classes()).toContain('w-7')
    expect(alphaToggle.classes()).toContain('rounded-md')
    expect(wrapper.find('[data-testid="team-overview-member-tree-header"]').exists()).toBe(false)
    const child = wrapper.get('[data-testid="team-overview-department-department-alpha-team-one"]')
    expect(child.attributes('aria-level')).toBe('2')
    expect(child.attributes('style')).toContain('padding-left: 1.25rem')
    expect(wrapper.text()).toContain('Team One')
    const aliceRow = wrapper.get('[data-testid="team-overview-member-user-101"]')
    expect(aliceRow.attributes('role')).toBe('treeitem')
    expect(aliceRow.attributes('aria-level')).toBe('2')
    expect(aliceRow.classes().join(' ')).not.toContain('rounded-md')
    expect(aliceRow.text()).toContain('Alice')
    expect(aliceRow.text()).toContain('24.50 USD')
    expect(aliceRow.text()).toContain('12K')
    expect(wrapper.text()).toContain('Bob')

    await wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]').trigger('click')
    expect(wrapper.find('[data-testid="team-overview-department-department-alpha-team-one"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="team-overview-member-user-101"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]').text()).toBe('+')

    await wrapper.get('[data-testid="team-overview-department-department-alpha"]').trigger('keydown', { key: 'Enter' })
    expect(wrapper.find('[data-testid="team-overview-department-department-alpha-team-one"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-overview-member-user-101"]').exists()).toBe(true)
  })

  it('collapses organization departments that only contain direct members', async () => {
    const directMembersFixture: TeamOverviewResponse = structuredClone(overviewFixture)
    directMembersFixture.member_tree = [
      {
        ...directMembersFixture.member_tree![0],
        child_count: 0,
        members: directMembersFixture.members,
        children: [],
      },
    ]
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: directMembersFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    await wrapper.get('[data-testid="team-overview-organization-view"]').trigger('click')
    const toggle = wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]')
    expect(toggle.text()).toBe('-')
    expect(wrapper.find('[data-testid="team-overview-member-user-101"]').exists()).toBe(true)

    await toggle.trigger('click')

    expect(wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]').text()).toBe('+')
    expect(wrapper.find('[data-testid="team-overview-member-user-101"]').exists()).toBe(false)
  })

  it('renders organization departments when backend returns null empty member arrays', async () => {
    const nullMembersFixture: TeamOverviewResponse = structuredClone(overviewFixture)
    ;(nullMembersFixture.member_tree![0].children[0] as any).members = null
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: nullMembersFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    await wrapper.get('[data-testid="team-overview-organization-view"]').trigger('click')

    expect(wrapper.get('[data-testid="team-overview-department-department-alpha"]').text()).toContain('Department Alpha')
    expect(wrapper.get('[data-testid="team-overview-department-department-alpha-team-one"]').text()).toContain('Team One')
  })

  it('marks unconnected members in red with localized status', async () => {
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const carolRow = wrapper.get('[data-testid="team-overview-member-directory-member-carol"]')
    expect(carolRow.text()).toContain('Not connected')
    expect(carolRow.classes().join(' ')).toContain('bg-red-50')

    setLocale('zh-CN')
    const zhWrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()
    expect(zhWrapper.get('[data-testid="team-overview-member-directory-member-carol"]').text()).toContain('未接入')
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
          rank_basis: 'range_total_tokens',
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

  it('uses selected-window token totals for the Top 12 trend chart data and axis', () => {
    const wrapper = mount(TeamOverviewMemberTrendChart, {
      props: {
        state: overviewFixture.top_member_trend,
        window: overviewFixture.window,
      },
    })

    const chart = wrapper.get('[data-test="line-chart"]')
    const chartData = JSON.parse(chart.attributes('data-chart') ?? '{}') as {
      datasets: Array<{ data: Array<number | null> }>
    }
    const chartOptions = JSON.parse(chart.attributes('data-options') ?? '{}') as {
      scales: { y: { title: { text: string } } }
    }

    expect(chartData.datasets[0].data).toEqual([5000, 7000])
    expect(chartOptions.scales.y.title.text).toBe('tokens')
  })
})
