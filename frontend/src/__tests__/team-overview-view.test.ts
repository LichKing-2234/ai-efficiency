import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import TeamOverviewMemberTrendChart from '@/components/team-usage/TeamOverviewMemberTrendChart.vue'
import TeamOverviewView from '@/views/TeamOverviewView.vue'
import { setLocale } from '@/i18n'
import type { TeamOverviewMember, TeamOverviewResponse, TeamUsageMembersResponse, TeamUsageOrganizationDepartment, TeamUsageOrganizationParams, TeamUsageOrganizationResponse, TeamUsageSummaryResponse, TeamUsageTrendResponse } from '@/types'

vi.mock('@/api/teamUsage', () => ({
  getTeamUsageOverview: vi.fn(),
  getTeamUsageMembers: vi.fn(),
  getTeamUsageOrganization: vi.fn(),
  getTeamUsageSummary: vi.fn(),
  getTeamUsageTrend: vi.fn(),
}))

vi.mock('@/components/charts/LineChartCanvas.vue', () => ({
  __esModule: true,
  default: {
    props: ['data', 'options'],
    template: '<div data-test="line-chart" :data-chart="JSON.stringify(data)" :data-options="JSON.stringify(options)" />',
  },
}))

const mockGetTeamUsageOverview = vi.mocked((await import('@/api/teamUsage')).getTeamUsageOverview)
const mockGetTeamUsageMembers = vi.mocked((await import('@/api/teamUsage')).getTeamUsageMembers)
const mockGetTeamUsageOrganization = vi.mocked((await import('@/api/teamUsage')).getTeamUsageOrganization)
const mockGetTeamUsageSummary = vi.mocked((await import('@/api/teamUsage')).getTeamUsageSummary)
const mockGetTeamUsageTrend = vi.mocked((await import('@/api/teamUsage')).getTeamUsageTrend)

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
  department_trend: {
    unit_label: 'USD',
    unavailable: false,
    unavailable_reason: null,
    comparison_total_count: 2,
    comparison_truncated: false,
    series: [
      {
        series_type: 'team_total',
        display_name: 'Team total',
        rank: 0,
        unavailable: false,
        unavailable_reason: null,
        points: [
          { date: '2026-06-27', actual_cost: 3.75, total_tokens: 5900 },
          { date: '2026-06-28', actual_cost: 4.25, total_tokens: 7000 },
        ],
      },
      {
        series_type: 'department',
        department_external_id: 'department-alpha-team-one',
        display_name: 'Team One',
        rank: 1,
        unavailable: false,
        unavailable_reason: null,
        points: [
          { date: '2026-06-27', actual_cost: 3.0, total_tokens: 900 },
          { date: '2026-06-28', actual_cost: 3.5, total_tokens: 1200 },
        ],
      },
      {
        series_type: 'department',
        department_external_id: 'department-alpha-team-two',
        display_name: 'Team Two',
        rank: 2,
        unavailable: false,
        unavailable_reason: null,
        points: [
          { date: '2026-06-27', actual_cost: 0.75, total_tokens: 5000 },
          { date: '2026-06-28', actual_cost: 0.75, total_tokens: 5800 },
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

const summaryFixture: TeamUsageSummaryResponse = {
  as_of: '2026-07-16T08:00:00Z',
  fresh_until: '2026-07-16T08:00:54Z',
  stale_until: '2026-07-16T08:04:30Z',
  cache_status: 'fresh',
  source_status: 'ok',
  scope_version: 'scope-version-1',
  request_id: 'request-summary-1',
  window: overviewFixture.window,
  summary: overviewFixture.summary,
}

const trendFixture: TeamUsageTrendResponse = {
  as_of: '2026-07-16T08:00:00Z',
  fresh_until: '2026-07-16T08:00:54Z',
  stale_until: '2026-07-16T08:04:30Z',
  cache_status: 'fresh',
  source_status: 'ok',
  scope_version: 'scope-version-1',
  request_id: 'request-trend-1',
  window: overviewFixture.window,
  top_members: overviewFixture.top_members,
  top_member_trend: overviewFixture.top_member_trend,
  department_trend: overviewFixture.department_trend!,
}

const membersFixture: TeamUsageMembersResponse = {
  as_of: '2026-07-16T08:00:00Z',
  fresh_until: '2026-07-16T08:00:54Z',
  stale_until: '2026-07-16T08:04:30Z',
  cache_status: 'fresh',
  source_status: 'ok',
  scope_version: 'scope-version-1',
  request_id: 'request-members-1',
  window: overviewFixture.window,
  items: overviewFixture.members,
  total_count: overviewFixture.members.length,
}

function pagedMember(rank: number, prefix = 'Split Member'): TeamOverviewMember {
  return {
    rank,
    user_id: 1000 + rank,
    display_name: `${prefix} ${rank}`,
    email: `member-${rank}@example.com`,
    department_external_id: 'department-alpha',
    department_external_ids: ['department-alpha'],
    department_display_path: 'Department Alpha',
    relay_user_id: 2000 + rank,
    range_actual_cost: rank,
    today_actual_cost: 0,
    total_actual_cost: rank,
    total_tokens: 10_000 - rank,
    subscription_count: 1,
    selectable: true,
  }
}

function membersPage(startRank: number, count: number, totalCount: number, nextCursor?: string): TeamUsageMembersResponse {
  return {
    ...membersFixture,
    request_id: `request-members-${startRank}`,
    items: Array.from({ length: count }, (_, index) => pagedMember(startRank + index)),
    total_count: totalCount,
    next_cursor: nextCursor,
  }
}

function organizationDepartment(
  id: string,
  name: string,
  options: Partial<TeamUsageOrganizationDepartment> = {},
): TeamUsageOrganizationDepartment {
  return {
    department_external_id: id,
    parent_external_id: null,
    name,
    display_path: name,
    depth: 0,
    child_count: 0,
    has_children: false,
    direct_member_count: 0,
    aggregate_member_count: 0,
    connected_member_count: 0,
    range_actual_cost: 0,
    range_total_tokens: 0,
    ...options,
  }
}

function organizationPage(
  parent: string | null,
  departments: TeamUsageOrganizationDepartment[],
  members: TeamOverviewMember[] = [],
  cursors: { departments?: string; members?: string } = {},
): TeamUsageOrganizationResponse {
  return {
    ...membersFixture,
    request_id: `request-organization-${parent ?? 'root'}`,
    parent_department_external_id: parent,
    departments,
    members,
    next_department_cursor: cursors.departments,
    next_member_cursor: cursors.members,
  }
}

const rootOrganizationDepartment = organizationDepartment('department-alpha', 'Department Alpha', {
  child_count: 2,
  has_children: true,
  direct_member_count: 1,
  aggregate_member_count: 3,
  connected_member_count: 2,
  range_actual_cost: 28,
  range_total_tokens: 12900,
})

function defaultOrganizationResponse(params?: TeamUsageOrganizationParams) {
  const parent = params?.parent_department_external_id
  if (parent === 'department-alpha') {
    return organizationPage('department-alpha', [
      organizationDepartment('department-alpha-team-one', 'Team One', {
        parent_external_id: 'department-alpha', depth: 1, direct_member_count: 2, aggregate_member_count: 2,
      }),
      organizationDepartment('department-alpha-team-two', 'Team Two', {
        parent_external_id: 'department-alpha', depth: 1,
      }),
    ], [overviewFixture.member_tree![0].members[0]])
  }
  if (parent === 'department-alpha-team-one') {
    return organizationPage(parent, [], overviewFixture.member_tree![0].children[0].members)
  }
  return organizationPage(null, [rootOrganizationDepartment])
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
    mockGetTeamUsageSummary.mockResolvedValue({ data: { data: summaryFixture } } as any)
    mockGetTeamUsageTrend.mockResolvedValue({ data: { data: trendFixture } } as any)
    mockGetTeamUsageMembers.mockResolvedValue({ data: { data: membersFixture } } as any)
    mockGetTeamUsageOrganization.mockImplementation(async (params?: TeamUsageOrganizationParams) => ({ data: { data: defaultOrganizationResponse(params) } } as any))
  })

  it('loads a shallow organization root without calling the compatibility overview', async () => {
    const roots = Array.from({ length: 25 }, (_, index) => organizationDepartment(`department-root-${index + 1}`, `Root ${index + 1}`))
    const moreRoots = Array.from({ length: 10 }, (_, index) => organizationDepartment(`department-root-${index + 26}`, `Root ${index + 26}`))
    mockGetTeamUsageOrganization
      .mockResolvedValueOnce({ data: { data: organizationPage(null, roots, [], { departments: 'root-page-2' }) } } as any)
      .mockResolvedValueOnce({ data: { data: organizationPage(null, moreRoots) } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()
    await wrapper.get('[data-testid="team-overview-organization-view"]').trigger('click')

    expect(wrapper.findAll('[data-testid^="team-overview-department-department-root-"]')).toHaveLength(25)
    expect(mockGetTeamUsageOrganization).toHaveBeenCalledTimes(1)
    expect(mockGetTeamUsageOrganization.mock.calls[0][0]).toEqual(expect.objectContaining({ department_limit: 25, member_limit: 50 }))
    expect(mockGetTeamUsageOverview).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="team-overview-departments-more-root"]').trigger('click')
    await flushPromises()
    expect(mockGetTeamUsageOrganization.mock.calls[1][0]).toEqual(expect.objectContaining({ department_cursor: 'root-page-2', member_cursor: undefined }))
    expect(wrapper.findAll('[data-testid^="team-overview-department-department-root-"]')).toHaveLength(35)
  })

  it('loads only the expanded branch and reuses it after collapse', async () => {
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()
    const wrapper = mount(TeamOverviewView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()
    await wrapper.get('[data-testid="team-overview-organization-view"]').trigger('click')

    expect(wrapper.find('[data-testid="team-overview-department-department-alpha-team-one"]').exists()).toBe(false)
    await wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]').trigger('click')
    await flushPromises()

    expect(mockGetTeamUsageOrganization).toHaveBeenCalledTimes(2)
    expect(mockGetTeamUsageOrganization.mock.calls[1][0]).toEqual(expect.objectContaining({ parent_department_external_id: 'department-alpha' }))
    expect(wrapper.find('[data-testid="team-overview-department-department-alpha-team-one"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-overview-member-user-101"]').exists()).toBe(true)

    await wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]').trigger('click')
    await wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]').trigger('click')
    await flushPromises()
    expect(mockGetTeamUsageOrganization).toHaveBeenCalledTimes(2)
  })

  it('opens selectable direct members from a lazy branch', async () => {
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()
    const wrapper = mount(TeamOverviewView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()
    await wrapper.get('[data-testid="team-overview-organization-view"]').trigger('click')
    await wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="team-overview-member-user-101"] button').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('UsageMember')
    expect(router.currentRoute.value.params.user_id).toBe('101')
  })

  it('pages one branch collection at a time', async () => {
    mockGetTeamUsageOrganization
      .mockResolvedValueOnce({ data: { data: organizationPage(null, [rootOrganizationDepartment]) } } as any)
      .mockResolvedValueOnce({ data: { data: organizationPage('department-alpha', [organizationDepartment('department-child-1', 'Child 1')], [pagedMember(1)], { departments: 'departments-page-2', members: 'members-page-2' }) } } as any)
      .mockResolvedValueOnce({ data: { data: organizationPage('department-alpha', [organizationDepartment('department-child-2', 'Child 2')], [pagedMember(1)]) } } as any)
      .mockResolvedValueOnce({ data: { data: organizationPage('department-alpha', [organizationDepartment('department-child-1', 'Child 1')], [pagedMember(2)]) } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()
    const wrapper = mount(TeamOverviewView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()
    await wrapper.get('[data-testid="team-overview-organization-view"]').trigger('click')
    await wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="team-overview-departments-more-department-alpha"]').trigger('click')
    await flushPromises()
    expect(mockGetTeamUsageOrganization.mock.calls[2][0]).toEqual(expect.objectContaining({ department_cursor: 'departments-page-2', member_cursor: undefined }))
    expect(wrapper.find('[data-testid="team-overview-department-department-child-1"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-overview-department-department-child-2"]').exists()).toBe(true)

    await wrapper.get('[data-testid="team-overview-members-more-department-alpha"]').trigger('click')
    await flushPromises()
    expect(mockGetTeamUsageOrganization.mock.calls[3][0]).toEqual(expect.objectContaining({ member_cursor: 'members-page-2', department_cursor: undefined }))
    expect(wrapper.find('[data-testid="team-overview-member-user-1001"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-overview-member-user-1002"]').exists()).toBe(true)
  })

  it('restarts only one organization branch after snapshot expiry', async () => {
    mockGetTeamUsageOrganization
      .mockResolvedValueOnce({ data: { data: organizationPage(null, [rootOrganizationDepartment]) } } as any)
      .mockResolvedValueOnce({ data: { data: organizationPage('department-alpha', [], [pagedMember(1)], { members: 'members-page-2' }) } } as any)
      .mockRejectedValueOnce({ response: { status: 409, data: { message: 'snapshot_expired' } } })
      .mockResolvedValueOnce({ data: { data: organizationPage('department-alpha', [], [pagedMember(1)], { members: 'members-page-2' }) } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()
    const wrapper = mount(TeamOverviewView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()
    await wrapper.get('[data-testid="team-overview-organization-view"]').trigger('click')
    await wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="team-overview-members-more-department-alpha"]').trigger('click')
    await flushPromises()

    expect(mockGetTeamUsageOrganization).toHaveBeenCalledTimes(4)
    expect(mockGetTeamUsageOrganization.mock.calls[3][0]).toEqual(expect.objectContaining({ parent_department_external_id: 'department-alpha', member_cursor: undefined, department_cursor: undefined }))
    expect(mockGetTeamUsageSummary).toHaveBeenCalledTimes(1)
    expect(mockGetTeamUsageTrend).toHaveBeenCalledTimes(1)
    expect(mockGetTeamUsageMembers).toHaveBeenCalledTimes(1)
  })

  it('invalidates recovered branch descendants while preserving sibling branches', async () => {
    const childBeforeRefresh = organizationDepartment('department-alpha-child', 'Alpha Child', {
      parent_external_id: 'department-alpha',
      depth: 1,
      direct_member_count: 1,
      aggregate_member_count: 1,
    })
    const childAfterRefresh = organizationDepartment('department-alpha-child', 'Alpha Child Updated', {
      parent_external_id: 'department-alpha',
      depth: 1,
      direct_member_count: 1,
      aggregate_member_count: 1,
    })
    const sibling = organizationDepartment('department-beta', 'Department Beta', {
      direct_member_count: 1,
      aggregate_member_count: 1,
    })
    mockGetTeamUsageOrganization
      .mockResolvedValueOnce({ data: { data: organizationPage(null, [rootOrganizationDepartment, sibling]) } } as any)
      .mockResolvedValueOnce({ data: { data: organizationPage('department-alpha', [childBeforeRefresh], [], { members: 'alpha-members-page-2' }) } } as any)
      .mockResolvedValueOnce({ data: { data: organizationPage('department-alpha-child', [], [pagedMember(11, 'Stale Child Member')]) } } as any)
      .mockResolvedValueOnce({ data: { data: organizationPage('department-beta', [], [pagedMember(12, 'Sibling Member')]) } } as any)
      .mockRejectedValueOnce({ response: { status: 409, data: { message: 'snapshot_expired' } } })
      .mockResolvedValueOnce({ data: { data: organizationPage('department-alpha', [childAfterRefresh]) } } as any)
      .mockResolvedValueOnce({ data: { data: organizationPage('department-alpha-child', [], [pagedMember(13, 'Fresh Child Member')]) } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()
    const wrapper = mount(TeamOverviewView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()
    await wrapper.get('[data-testid="team-overview-organization-view"]').trigger('click')

    await wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="team-overview-department-toggle-department-alpha-child"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="team-overview-department-toggle-department-beta"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="team-overview-member-user-1011"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-overview-member-user-1012"]').exists()).toBe(true)

    await wrapper.get('[data-testid="team-overview-members-more-department-alpha"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="team-overview-department-department-alpha-child"]').text()).toContain('Alpha Child Updated')
    expect(wrapper.get('[data-testid="team-overview-department-department-alpha-child"]').attributes('aria-expanded')).toBe('false')
    expect(wrapper.find('[data-testid="team-overview-member-user-1011"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="team-overview-member-user-1012"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="team-overview-department-department-beta"]').attributes('aria-expanded')).toBe('true')

    await wrapper.get('[data-testid="team-overview-department-toggle-department-alpha-child"]').trigger('click')
    await flushPromises()

    expect(mockGetTeamUsageOrganization).toHaveBeenCalledTimes(7)
    expect(mockGetTeamUsageOrganization.mock.calls[6][0]).toEqual(expect.objectContaining({
      parent_department_external_id: 'department-alpha-child',
      department_cursor: undefined,
      member_cursor: undefined,
    }))
    expect(wrapper.find('[data-testid="team-overview-member-user-1013"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-overview-member-user-1012"]').exists()).toBe(true)
  })

  it('ignores an invalidated descendant request that resolves after the branch reloads', async () => {
    const child = organizationDepartment('department-alpha-child', 'Alpha Child', {
      parent_external_id: 'department-alpha',
      depth: 1,
      direct_member_count: 1,
      aggregate_member_count: 1,
    })
    let resolveStaleChild!: (value: unknown) => void
    const staleChildRequest = new Promise((resolve) => {
      resolveStaleChild = resolve
    })
    mockGetTeamUsageOrganization
      .mockResolvedValueOnce({ data: { data: organizationPage(null, [rootOrganizationDepartment]) } } as any)
      .mockResolvedValueOnce({ data: { data: organizationPage('department-alpha', [child], [], { members: 'alpha-members-page-2' }) } } as any)
      .mockReturnValueOnce(staleChildRequest as any)
      .mockRejectedValueOnce({ response: { status: 409, data: { message: 'snapshot_expired' } } })
      .mockResolvedValueOnce({ data: { data: organizationPage('department-alpha', [child]) } } as any)
      .mockResolvedValueOnce({ data: { data: organizationPage('department-alpha-child', [], [pagedMember(14, 'Fresh Child Member')]) } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()
    const wrapper = mount(TeamOverviewView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()
    await wrapper.get('[data-testid="team-overview-organization-view"]').trigger('click')

    await wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="team-overview-department-toggle-department-alpha-child"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="team-overview-members-more-department-alpha"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="team-overview-department-toggle-department-alpha-child"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="team-overview-member-user-1014"]').exists()).toBe(true)

    resolveStaleChild({ data: { data: organizationPage('department-alpha-child', [], [pagedMember(15, 'Stale Child Member')]) } })
    await flushPromises()

    expect(wrapper.find('[data-testid="team-overview-member-user-1014"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-overview-member-user-1015"]').exists()).toBe(false)
  })

  it('keeps sibling branches visible when one expansion fails', async () => {
    const beta = organizationDepartment('department-beta', 'Department Beta', { has_children: true, child_count: 1 })
    mockGetTeamUsageOrganization
      .mockResolvedValueOnce({ data: { data: organizationPage(null, [rootOrganizationDepartment, beta]) } } as any)
      .mockResolvedValueOnce({ data: { data: defaultOrganizationResponse({ parent_department_external_id: 'department-alpha' }) } } as any)
      .mockRejectedValueOnce(new Error('synthetic beta branch failure'))
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()
    const wrapper = mount(TeamOverviewView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()
    await wrapper.get('[data-testid="team-overview-organization-view"]').trigger('click')
    await wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="team-overview-department-toggle-department-beta"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="team-overview-department-department-alpha-team-one"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-overview-organization-error-department-beta"]').exists()).toBe(true)
  })

  it('keeps the root range for branch expansion across midnight and resets it on range change', async () => {
    vi.useFakeTimers()
    try {
      vi.setSystemTime(new Date(2026, 6, 16, 23, 59, 59))
      const router = createTestRouter()
      await router.push('/usage/team')
      await router.isReady()
      const wrapper = mount(TeamOverviewView, { global: { plugins: [createPinia(), router] } })
      await flushPromises()
      const rootParams = mockGetTeamUsageOrganization.mock.calls[0]?.[0]
      if (rootParams == null) throw new Error('organization root request was not issued')

      vi.setSystemTime(new Date(2026, 6, 17, 0, 0, 1))
      await wrapper.get('[data-testid="team-overview-organization-view"]').trigger('click')
      await wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]').trigger('click')
      await flushPromises()
      expect(mockGetTeamUsageOrganization.mock.calls[1][0]).toEqual(expect.objectContaining({
        start_date: rootParams.start_date,
        end_date: rootParams.end_date,
        parent_department_external_id: 'department-alpha',
      }))

      await wrapper.get('[data-test="range-7d"]').trigger('click')
      await flushPromises()
      const resetCall = mockGetTeamUsageOrganization.mock.calls[2][0]
      if (resetCall == null) throw new Error('organization range reset request was not issued')
      expect(resetCall.parent_department_external_id).toBeUndefined()
      expect(resetCall.end_date).not.toBe(rootParams.end_date)
      expect(wrapper.find('[data-testid="team-overview-department-department-alpha-team-one"]').exists()).toBe(false)
      expect(wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]').text()).toBe('+')
    } finally {
      vi.useRealTimers()
    }
  })

  it('renders only the 50 split ranking rows from a 500-member result', async () => {
    const compatibilityFixture = structuredClone(overviewFixture)
    compatibilityFixture.members = Array.from({ length: 500 }, (_, index) => pagedMember(index + 1, 'Legacy Member'))
    mockGetTeamUsageMembers.mockResolvedValue({ data: { data: membersPage(1, 50, 500, 'cursor-page-2') } } as any)
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: compatibilityFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const rows = wrapper.findAll('[data-testid^="team-overview-member-user-"]')
    expect(rows).toHaveLength(50)
    expect(wrapper.get('[data-testid="team-overview-ranking-table"]').text()).toContain('Split Member 1')
    expect(wrapper.get('[data-testid="team-overview-ranking-table"]').text()).not.toContain('Legacy Member')
    expect(wrapper.get('[data-testid="team-overview-member-pagination"]').text()).toContain('1-50')
    expect(wrapper.get('[data-testid="team-overview-member-pagination"]').text()).toContain('500')
  })

  it('keeps summary, trend, and organization available while members are delayed', async () => {
    mockGetTeamUsageMembers.mockImplementation(() => new Promise(() => {}) as any)
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="team-overview-summary"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-member-trend-chart"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-overview-members-loading"]').exists()).toBe(true)
    await wrapper.get('[data-testid="team-overview-organization-view"]').trigger('click')
    expect(wrapper.find('[data-testid="team-overview-department-department-alpha"]').exists()).toBe(true)
  })

  it('keeps summary, trend, and organization available when members fail', async () => {
    mockGetTeamUsageMembers.mockRejectedValue(new Error('synthetic members failure'))
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="team-overview-summary"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-member-trend-chart"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-overview-members-error"]').exists()).toBe(true)
    await wrapper.get('[data-testid="team-overview-organization-view"]').trigger('click')
    expect(wrapper.find('[data-testid="team-overview-department-department-alpha"]').exists()).toBe(true)
  })

  it('keeps split member rankings visible when organization root fails', async () => {
    mockGetTeamUsageMembers.mockResolvedValue({ data: { data: membersPage(1, 3, 3) } } as any)
    mockGetTeamUsageOrganization.mockRejectedValue(new Error('synthetic organization failure'))
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.get('[data-testid="team-overview-ranking-table"]').text()).toContain('Split Member 1')
    await wrapper.get('[data-testid="team-overview-organization-view"]').trigger('click')
    expect(wrapper.find('[data-testid="team-overview-organization-error-root"]').exists()).toBe(true)
  })

  it('pages members forward and backward without refetching sibling sections', async () => {
    mockGetTeamUsageMembers
      .mockResolvedValueOnce({ data: { data: membersPage(1, 50, 100, 'cursor-page-2') } } as any)
      .mockResolvedValueOnce({ data: { data: membersPage(51, 50, 100) } } as any)
      .mockResolvedValueOnce({ data: { data: membersPage(1, 50, 100, 'cursor-page-2') } } as any)
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    await wrapper.get('[data-testid="team-overview-members-next"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="team-overview-ranking-table"]').text()).toContain('Split Member 51')
    expect(mockGetTeamUsageMembers.mock.calls[1][0]).toEqual(expect.objectContaining({
      cursor: 'cursor-page-2', granularity: 'day', limit: 50,
    }))

    await wrapper.get('[data-testid="team-overview-members-previous"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="team-overview-ranking-table"]').text()).toContain('Split Member 1')
    expect(mockGetTeamUsageMembers.mock.calls[2][0]).toEqual(expect.objectContaining({ cursor: undefined, limit: 50 }))
    expect(mockGetTeamUsageSummary).toHaveBeenCalledTimes(1)
    expect(mockGetTeamUsageTrend).toHaveBeenCalledTimes(1)
    expect(mockGetTeamUsageOrganization).toHaveBeenCalledTimes(1)
  })

  it('keeps the original absolute range when pagination crosses midnight', async () => {
    vi.useFakeTimers()
    try {
      vi.setSystemTime(new Date(2026, 6, 16, 23, 59, 59))
      mockGetTeamUsageMembers
        .mockResolvedValueOnce({ data: { data: membersPage(1, 50, 100, 'cursor-page-2') } } as any)
        .mockResolvedValueOnce({ data: { data: membersPage(51, 50, 100) } } as any)
      mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
      const router = createTestRouter()
      await router.push('/usage/team')
      await router.isReady()

      const wrapper = mount(TeamOverviewView, {
        global: { plugins: [createPinia(), router] },
      })
      await flushPromises()
      const firstParams = mockGetTeamUsageMembers.mock.calls[0]?.[0]
      if (firstParams == null) throw new Error('initial member request was not issued')

      vi.setSystemTime(new Date(2026, 6, 17, 0, 0, 1))
      await wrapper.get('[data-testid="team-overview-members-next"]').trigger('click')
      await flushPromises()

      expect(mockGetTeamUsageMembers.mock.calls[1][0]).toEqual(expect.objectContaining({
        cursor: 'cursor-page-2',
        start_date: firstParams.start_date,
        end_date: firstParams.end_date,
      }))
    } finally {
      vi.useRealTimers()
    }
  })

  it('restarts only the member section after snapshot expiry', async () => {
    mockGetTeamUsageMembers
      .mockResolvedValueOnce({ data: { data: membersPage(1, 50, 100, 'cursor-page-2') } } as any)
      .mockRejectedValueOnce({ response: { status: 409, data: { message: 'snapshot_expired' } } })
      .mockResolvedValueOnce({ data: { data: membersPage(1, 50, 100, 'cursor-page-2') } } as any)
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    await wrapper.get('[data-testid="team-overview-members-next"]').trigger('click')
    await flushPromises()

    expect(mockGetTeamUsageMembers).toHaveBeenCalledTimes(3)
    expect(mockGetTeamUsageMembers.mock.calls[2][0]).toEqual(expect.objectContaining({ cursor: undefined, limit: 50 }))
    expect(wrapper.get('[data-testid="team-overview-ranking-table"]').text()).toContain('Split Member 1')
    expect(mockGetTeamUsageSummary).toHaveBeenCalledTimes(1)
    expect(mockGetTeamUsageTrend).toHaveBeenCalledTimes(1)
    expect(mockGetTeamUsageOrganization).toHaveBeenCalledTimes(1)
  })

  it('shows stale freshness only on the member ranking section', async () => {
    mockGetTeamUsageMembers.mockResolvedValue({
      data: { data: { ...membersPage(1, 3, 3), cache_status: 'stale', source_status: 'error' } },
    } as any)
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.get('[data-testid="team-members-stale-marker"]').text()).toContain('Showing a recent snapshot')
    expect(wrapper.find('[data-testid="team-summary-stale-marker"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="team-trend-stale-marker"]').exists()).toBe(false)
  })

  it('keeps summary, ranking, and organization root visible while trend is delayed', async () => {
    let resolveTrend!: (value: unknown) => void
    mockGetTeamUsageTrend.mockImplementation(() => new Promise((resolve) => {
      resolveTrend = resolve
    }) as any)
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(mockGetTeamUsageSummary).toHaveBeenCalledWith(expect.objectContaining({ granularity: 'day' }))
    expect(mockGetTeamUsageTrend).toHaveBeenCalledWith(expect.objectContaining({ granularity: 'day' }))
    expect(mockGetTeamUsageOrganization).toHaveBeenCalledWith(expect.objectContaining({ granularity: 'day' }))
    expect(wrapper.text()).toContain('28.00 USD')
    expect(wrapper.text()).toContain('12.9K')
    expect(wrapper.find('[data-testid="team-overview-member-user-101"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-overview-trend-loading"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-member-trend-chart"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="range-today"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="team-overview-content"]').attributes('aria-busy')).toBe('false')
    expect(wrapper.get('[data-testid="team-overview-content"]').classes()).toContain('opacity-100')
    expect(wrapper.find('[data-testid="team-overview-refreshing"]').exists()).toBe(false)

    resolveTrend({ data: { data: trendFixture } })
    await flushPromises()

    expect(wrapper.find('[data-testid="team-overview-trend-loading"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="team-member-trend-chart"]').exists()).toBe(true)
  })

  it('renders available summary values and its local warning while trend is delayed', async () => {
    mockGetTeamUsageSummary.mockResolvedValue({
      data: {
        data: {
          ...summaryFixture,
          summary: {
            ...summaryFixture.summary,
            unavailable: true,
            unavailable_reason: 'range_aggregation_unavailable',
            range_actual_cost: null,
            range_total_tokens: null,
          },
        },
      },
    } as any)
    mockGetTeamUsageTrend.mockImplementation(() => new Promise(() => {}) as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const summary = wrapper.get('[data-testid="team-overview-summary"]')
    expect(summary.text()).toContain('Team members')
    expect(summary.text()).toContain('3')
    expect(summary.text()).toContain('Connected members')
    expect(summary.text()).toContain('2')
    expect(summary.text()).toContain('Selected range totals are temporarily unavailable.')
    expect(wrapper.find('[data-testid="team-overview-trend-loading"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-member-trend-chart"]').exists()).toBe(false)
  })

  it('keeps successful summary and trend visible when organization root fails', async () => {
    mockGetTeamUsageOrganization.mockRejectedValue(new Error('synthetic organization failure'))
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('28.00 USD')
    expect(wrapper.find('[data-testid="team-overview-summary"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-member-trend-chart"]').exists()).toBe(true)
    await wrapper.get('[data-testid="team-overview-organization-view"]').trigger('click')
    expect(wrapper.find('[data-testid="team-overview-organization-error-root"]').exists()).toBe(true)
  })

  it('keeps successful summary and member content visible when trend fails', async () => {
    mockGetTeamUsageTrend.mockRejectedValue(new Error('synthetic trend failure'))
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="team-overview-summary"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-overview-member-user-101"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-overview-trend-error"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-member-trend-chart"]').exists()).toBe(false)
  })

  it('renders an explicit empty trend response without hiding summary or members', async () => {
    mockGetTeamUsageTrend.mockResolvedValue({
      data: {
        data: {
          ...trendFixture,
          top_members: [],
          top_member_trend: { ...trendFixture.top_member_trend, series: [] },
          department_trend: {
            ...trendFixture.department_trend,
            comparison_total_count: 0,
            comparison_truncated: false,
            series: [],
          },
        },
      },
    } as any)
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="team-overview-summary"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-overview-member-user-101"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="team-member-trend-chart"]').text()).toContain('-')
    expect(wrapper.find('[data-test="line-chart"]').exists()).toBe(false)
  })

  it('keeps successful compatibility sections visible when summary fails', async () => {
    mockGetTeamUsageSummary.mockRejectedValue(new Error('synthetic summary failure'))
    const compatibilityFixture = structuredClone(overviewFixture)
    compatibilityFixture.top_member_trend.series = []
    compatibilityFixture.department_trend!.series = []
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: compatibilityFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Team usage is temporarily unavailable.')
    expect(wrapper.text()).toContain('Alice')
    expect(wrapper.find('[data-testid="team-member-trend-chart"]').exists()).toBe(true)
  })

  it('shows stale freshness only on the summary section', async () => {
    mockGetTeamUsageSummary.mockResolvedValue({
      data: { data: { ...summaryFixture, cache_status: 'stale', source_status: 'error' } },
    } as any)
    mockGetTeamUsageOverview.mockImplementation(() => new Promise(() => {}) as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.get('[data-testid="team-summary-stale-marker"]').text()).toContain('Showing a recent snapshot')
    expect(wrapper.find('[data-testid="team-trend-stale-marker"]').exists()).toBe(false)
  })

  it('shows stale freshness only on the trend section', async () => {
    mockGetTeamUsageTrend.mockResolvedValue({
      data: { data: { ...trendFixture, cache_status: 'stale', source_status: 'error' } },
    } as any)
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.get('[data-testid="team-trend-stale-marker"]').text()).toContain('Showing a recent snapshot')
    expect(wrapper.find('[data-testid="team-summary-stale-marker"]').exists()).toBe(false)
  })

  it('ignores trend fields from the compatibility overview', async () => {
    mockGetTeamUsageOverview.mockResolvedValue({
      data: {
        data: {
          ...overviewFixture,
          top_member_trend: {
            ...overviewFixture.top_member_trend,
            series: [{
              ...overviewFixture.top_member_trend.series[0],
              display_name: 'Legacy Trend Member',
            }],
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

    const trend = wrapper.get('[data-testid="team-member-trend-chart"]')
    expect(trend.text()).toContain('Alice')
    expect(trend.text()).not.toContain('Legacy Trend Member')
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

    expect(mockGetTeamUsageOrganization).toHaveBeenCalledWith(expect.objectContaining({ granularity: 'day' }))
    expect(wrapper.text()).toContain('Usage Trends')
    expect(wrapper.text()).not.toContain('Team and Top 12 token usage trend')
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

  it('renders team total independently and compares multiple subteam token trends apart from Top 12 members', async () => {
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const trend = wrapper.get('[data-testid="team-member-trend-chart"]')
    expect(trend.text()).toContain('Team total trend')
    expect(trend.text()).toContain('Group comparison trends')
    expect(trend.text()).toContain('Team total')
    expect(trend.text()).toContain('Team One')
    expect(trend.text()).toContain('Team Two')
    expect(trend.text()).toContain('#1 Alice')
    const totalChart = wrapper.get('[data-testid="team-total-trend-chart"] [data-test="line-chart"]')
    const totalChartData = JSON.parse(totalChart.attributes('data-chart') ?? '{}') as {
      datasets: Array<{ label: string; data: Array<number | null> }>
    }
    const comparisonChart = wrapper.get('[data-testid="team-comparison-trend-chart"] [data-test="line-chart"]')
    const comparisonChartData = JSON.parse(comparisonChart.attributes('data-chart') ?? '{}') as {
      datasets: Array<{ label: string; data: Array<number | null> }>
    }
    const memberChart = wrapper.get('[data-testid="top-member-trend-chart"] [data-test="line-chart"]')
    const memberChartData = JSON.parse(memberChart.attributes('data-chart') ?? '{}') as {
      datasets: Array<{ label: string; data: Array<number | null> }>
    }

    expect(totalChartData.datasets.map((dataset) => dataset.label)).toEqual(['Team total'])
    expect(totalChartData.datasets[0].data).toEqual([5900, 7000])
    expect(comparisonChartData.datasets.map((dataset) => dataset.label)).toEqual(['Team One', 'Team Two'])
    expect(comparisonChartData.datasets[0].data).toEqual([900, 1200])
    expect(comparisonChartData.datasets[1].data).toEqual([5000, 5800])
    expect(memberChartData.datasets.map((dataset) => dataset.label)).toEqual(['#1 Alice'])
    expect(memberChartData.datasets[0].data).toEqual([5000, 7000])
  })

  it('keeps a single leaf team trend as an independent team total chart', async () => {
    const leafFixture: TeamOverviewResponse = structuredClone(overviewFixture)
    leafFixture.department_trend = {
      unit_label: 'USD',
      unavailable: false,
      unavailable_reason: null,
      comparison_total_count: 0,
      comparison_truncated: false,
      series: [
        {
          series_type: 'team_total',
          display_name: 'Team total',
          rank: 0,
          unavailable: false,
          unavailable_reason: null,
          points: [
            { date: '2026-06-27', actual_cost: 3.75, total_tokens: 5900 },
            { date: '2026-06-28', actual_cost: 4.25, total_tokens: 7000 },
          ],
        },
      ],
    }
    leafFixture.top_member_trend = {
      ...leafFixture.top_member_trend,
      series: [],
    }
    const wrapper = mount(TeamOverviewMemberTrendChart, {
      props: {
        state: leafFixture.top_member_trend,
        departmentTrend: leafFixture.department_trend,
        window: leafFixture.window,
      },
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="team-total-trend-chart"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-comparison-trend-chart"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="top-member-trend-chart"]').exists()).toBe(false)
    const totalChart = wrapper.get('[data-testid="team-total-trend-chart"] [data-test="line-chart"]')
    const totalChartData = JSON.parse(totalChart.attributes('data-chart') ?? '{}') as {
      datasets: Array<{ label: string; data: Array<number | null> }>
    }
    expect(totalChartData.datasets.map((dataset) => dataset.label)).toEqual(['Team total'])
    expect(totalChartData.datasets[0].data).toEqual([5900, 7000])
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
    expect(wrapper.text()).toContain('用量趋势')
    expect(wrapper.text()).not.toContain('团队与 Top 12 Token 用量趋势')
    expect(wrapper.find('header h1').exists()).toBe(false)
    expect(wrapper.text()).toContain('当前范围 Token 用量')
    expect(wrapper.text()).toContain('查看明细')
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

    const params = mockGetTeamUsageOrganization.mock.calls[0][0] as {
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

    mockGetTeamUsageOrganization.mockClear()
    await wrapper.get('[data-test="range-7d"]').trigger('click')
    await flushPromises()

    const params = mockGetTeamUsageOrganization.mock.calls[0][0] as {
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
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    let resolveRefresh: (value: any) => void = () => {}
    mockGetTeamUsageSummary.mockImplementationOnce(() => new Promise((resolve) => {
      resolveRefresh = resolve
    }) as any)

    await wrapper.get('[data-test="range-7d"]').trigger('click')

    expect(wrapper.get('[data-test="range-7d"]').classes()).toContain('bg-blue-600')
    expect(wrapper.get('[data-testid="team-overview-refreshing"]').text()).toContain('Updating team usage...')
    expect(wrapper.get('[data-testid="team-overview-content"]').attributes('aria-busy')).toBe('true')
    expect(wrapper.text()).toContain('Alice')

    resolveRefresh({ data: { data: summaryFixture } })
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
    mockGetTeamUsageTrend.mockResolvedValue({
      data: {
        data: {
          ...trendFixture,
          top_member_trend: {
            ...trendFixture.top_member_trend,
            unavailable_reason: 'scope_too_large',
          },
        },
      },
    } as any)
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
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
		mockGetTeamUsageSummary.mockResolvedValue({
			data: {
				data: {
					...summaryFixture,
					summary: {
						...summaryFixture.summary,
						unavailable: true,
						unavailable_reason: 'provider_error',
						range_actual_cost: null,
						range_total_tokens: null,
					},
				},
			},
		} as any)
    mockGetTeamUsageTrend.mockResolvedValue({
      data: {
        data: {
          ...trendFixture,
          top_member_trend: {
            ...trendFixture.top_member_trend,
            unavailable: true,
            unavailable_reason: 'provider_error',
            series: [],
          },
        },
      },
    } as any)
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
		mockGetTeamUsageSummary.mockRejectedValue({
			response: { status: 403, data: { code: 'not_representative' } },
		})
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

  it('renders a local unavailable state when organization root fails', async () => {
    mockGetTeamUsageOrganization.mockRejectedValue(new Error('network unavailable'))
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    await wrapper.get('[data-testid="team-overview-organization-view"]').trigger('click')
    expect(wrapper.get('[data-testid="team-overview-organization-error-root"]').text()).toContain('Team usage is temporarily unavailable.')
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
    const largeTrendFixture: TeamUsageTrendResponse = structuredClone(trendFixture)
    const largeMembersFixture: TeamUsageMembersResponse = structuredClone(membersFixture)
    largeTokenFixture.summary.range_total_tokens = 12_285_557_755
    largeTrendFixture.top_member_trend.series[0].points = [
      { date: '2026-06-27', actual_cost: 0.75, total_tokens: 3_000_000_000 },
      { date: '2026-06-28', actual_cost: 1.25, total_tokens: 3_052_813_773 },
    ]
    largeTokenFixture.members[0].total_tokens = 805_033_680
    largeMembersFixture.items[0].total_tokens = 805_033_680
		mockGetTeamUsageSummary.mockResolvedValue({
			data: {
				data: {
					...summaryFixture,
					summary: largeTokenFixture.summary,
				},
			},
		} as any)
    mockGetTeamUsageTrend.mockResolvedValue({ data: { data: largeTrendFixture } } as any)
    mockGetTeamUsageMembers.mockResolvedValue({ data: { data: largeMembersFixture } } as any)
    mockGetTeamUsageOrganization.mockImplementation(async (params?: TeamUsageOrganizationParams) => ({
      data: {
        data: params?.parent_department_external_id === 'department-alpha'
          ? organizationPage('department-alpha', [], [largeMembersFixture.items[0]])
          : organizationPage(null, [{ ...rootOrganizationDepartment, range_total_tokens: 12_285_557_755 }]),
      },
    } as any))
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
    await wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]').trigger('click')
    await flushPromises()
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
    expect(alphaToggle.text()).toBe('+')
    expect(alphaToggle.classes()).toContain('h-7')
    expect(alphaToggle.classes()).toContain('w-7')
    expect(alphaToggle.classes()).toContain('rounded-md')
    expect(wrapper.find('[data-testid="team-overview-member-tree-header"]').exists()).toBe(false)
    await alphaToggle.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]').text()).toBe('-')
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
    await wrapper.get('[data-testid="team-overview-department-toggle-department-alpha-team-one"]').trigger('click')
    await flushPromises()
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
    const directRoot = { ...rootOrganizationDepartment, child_count: 0, has_children: false, direct_member_count: 3 }
    mockGetTeamUsageOrganization.mockImplementation(async (params?: TeamUsageOrganizationParams) => ({ data: { data: params?.parent_department_external_id ? organizationPage('department-alpha', [], overviewFixture.members) : organizationPage(null, [directRoot]) } } as any))
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    await wrapper.get('[data-testid="team-overview-organization-view"]').trigger('click')
    const toggle = wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]')
    expect(toggle.text()).toBe('+')
    expect(wrapper.find('[data-testid="team-overview-member-user-101"]').exists()).toBe(false)

    await toggle.trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]').text()).toBe('-')
    expect(wrapper.find('[data-testid="team-overview-member-user-101"]').exists()).toBe(true)

    await wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]').trigger('click')
    expect(wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]').text()).toBe('+')
    expect(wrapper.find('[data-testid="team-overview-member-user-101"]').exists()).toBe(false)
  })

  it('keeps the parent department when a branch returns null collections', async () => {
    mockGetTeamUsageOrganization
      .mockResolvedValueOnce({ data: { data: organizationPage(null, [rootOrganizationDepartment]) } } as any)
      .mockResolvedValueOnce({ data: { data: { ...organizationPage('department-alpha', []), departments: null, members: null } } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    await wrapper.get('[data-testid="team-overview-organization-view"]').trigger('click')
    await wrapper.get('[data-testid="team-overview-department-toggle-department-alpha"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="team-overview-department-department-alpha"]').text()).toContain('Department Alpha')
    expect(wrapper.find('[data-testid="team-overview-department-department-alpha-team-one"]').exists()).toBe(false)
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

  it('routes View details action to the selected member detail page', async () => {
    mockGetTeamUsageOverview.mockResolvedValue({ data: { data: overviewFixture } } as any)
    const router = createTestRouter()
    await router.push('/usage/team')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const openButton = wrapper.findAll('button').find((button) => button.text() === 'View details')
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

  it('uses selected-window token totals for the Top 12 trend chart data and axis', async () => {
    const wrapper = mount(TeamOverviewMemberTrendChart, {
      props: {
        state: overviewFixture.top_member_trend,
        departmentTrend: overviewFixture.department_trend,
        window: overviewFixture.window,
      },
    })
    await flushPromises()

    const chart = wrapper.get('[data-testid="top-member-trend-chart"] [data-test="line-chart"]')
    const chartData = JSON.parse(chart.attributes('data-chart') ?? '{}') as {
      datasets: Array<{ label: string; data: Array<number | null> }>
    }
    const chartOptions = JSON.parse(chart.attributes('data-options') ?? '{}') as {
      scales: { y: { title: { text: string } } }
    }

    expect(chartData.datasets.find((dataset) => dataset.label === '#1 Alice')?.data).toEqual([5000, 7000])
    expect(chartData.datasets.find((dataset) => dataset.label === 'Team total')).toBeUndefined()
    expect(chartOptions.scales.y.title.text).toBe('tokens')
  })

  it('keeps all trend legends at the same scrollable max height', () => {
    const state = structuredClone(overviewFixture.top_member_trend)
    state.series = Array.from({ length: 12 }, (_, index) => ({
      user_id: 100 + index,
      display_name: `Member ${index + 1}`,
      rank: index + 1,
      unavailable: false,
      unavailable_reason: null,
      points: [
        { date: '2026-06-28', actual_cost: index + 1, total_tokens: (index + 1) * 1000 },
      ],
    }))
    const wrapper = mount(TeamOverviewMemberTrendChart, {
      props: {
        state,
        departmentTrend: overviewFixture.department_trend,
        window: overviewFixture.window,
      },
    })

    for (const testId of ['team-total-trend-legend', 'subteam-trend-legend', 'top-member-trend-legend']) {
      const legend = wrapper.get(`[data-testid="${testId}"]`)
      expect(legend.classes()).toContain('max-h-64')
      expect(legend.classes()).toContain('overflow-y-auto')
    }
    expect(wrapper.get('[data-testid="top-member-trend-legend"]').text()).toContain('#12 Member 12')
  })
})
