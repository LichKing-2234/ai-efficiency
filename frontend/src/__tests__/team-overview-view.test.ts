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
    today_actual_cost: 1.25,
    last_30d_actual_cost: 24.5,
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
      today_actual_cost: 1.25,
      last_30d_actual_cost: 24.5,
      total_tokens: 12000,
      subscription_count: 2,
      selectable: true,
    },
  ],
  top_member_trend: {
    unit_label: 'USD',
    rank_basis: 'last_30d_actual_cost',
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
      today_actual_cost: 1.25,
      last_30d_actual_cost: 24.5,
      total_tokens: 12000,
      subscription_count: 2,
      selectable: true,
    },
    {
      rank: 2,
      user_id: 102,
      display_name: 'Bob',
      email: 'bob@example.org',
      department_display_path: 'Department Alpha / Department Beta',
      relay_user_id: null,
      today_actual_cost: 0,
      last_30d_actual_cost: 3.5,
      total_tokens: 900,
      subscription_count: 0,
      selectable: true,
    },
  ],
}

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div>Dashboard</div>' } },
      { path: '/user', component: { template: '<div>User</div>' } },
      { path: '/events', component: { template: '<div>Events</div>' } },
      { path: '/repos', component: { template: '<div>Repos</div>' } },
      { path: '/team-usage', component: TeamOverviewView },
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
    await router.push('/team-usage')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(mockGetTeamUsageOverview).toHaveBeenCalledWith({ granularity: 'day' })
    expect(wrapper.text()).toContain('Top 12 member usage')
    expect(wrapper.text()).toContain('Alice')
    expect(wrapper.text()).not.toContain('Used / Quota')
    expect(wrapper.text()).not.toContain('Rate multiplier')
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
    await router.push('/team-usage')
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
    await router.push('/team-usage')
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
    await router.push('/team-usage')
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
    await router.push('/team-usage')
    await router.isReady()

    const wrapper = mount(TeamOverviewView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const openButton = wrapper.findAll('button').find((button) => button.text() === 'Open')
    expect(openButton).toBeTruthy()
    await openButton!.trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.path).toBe('/')
    expect(router.currentRoute.value.query.subject_user_id).toBe('101')
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
          rank_basis: 'last_30d_actual_cost',
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
