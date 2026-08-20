import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createPinia } from 'pinia'
import ActivityTeamView from '@/views/activity/ActivityTeamView.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/activity', () => ({
  getActivityV2TeamMemberAvailability: vi.fn(),
  getActivityV2Overview: vi.fn(),
  listActivityV2Repositories: vi.fn(),
  listActivityV2PullRequests: vi.fn(),
}))
vi.mock('@/api/teamUsage', () => ({ getTeamUsageOrganization: vi.fn() }))
vi.mock('@/api/workItems', () => ({
  getWorkItemCounts: vi.fn().mockResolvedValue({ data: { data: { total_count: 0 } } }),
}))

const overview = {
  contract_version: 'activity-v2', scope_version: 'scope-1', from: '2026-07-14', to: '2026-08-12', timezone: 'UTC', committed_tokens: 400,
  claim_coverage: { complete: true, lower_bound: false }, scm_coverage: { complete: true, affected_repositories: 0 },
  ratio: { state: 'denominator_unavailable', committed_tokens: 0 },
  trend: [{ date: '2026-08-12', direct_tokens: 0, shared_tokens: 0, involved_tokens: 0 }], readiness: { state: 'active' },
}

describe('ActivityTeamView privacy contract', () => {
  beforeEach(async () => {
    setLocale('en-US')
    const activity = await import('@/api/activity')
    const teamUsage = await import('@/api/teamUsage')
    vi.mocked(teamUsage.getTeamUsageOrganization).mockResolvedValue({ data: { data: {
      members: [{
        user_id: 7, display_name: 'Alice', email: 'alice@example.com', department_external_ids: ['team-alpha'],
        department_display_path: 'Engineering / Team Alpha', range_actual_cost: 0, today_actual_cost: 0, total_actual_cost: 0, selectable: true,
      }],
    } } } as any)
    vi.mocked(activity.getActivityV2TeamMemberAvailability).mockResolvedValue({ data: { data: {
      contract_version: 'activity-v2', scope_version: 'scope-1',
      team: { external_id: 'team-alpha', name: 'Team Alpha', display_path: 'Engineering / Team Alpha', member_count: 1 },
      available_user_ids: [7],
    } } } as any)
    vi.mocked(activity.getActivityV2Overview).mockResolvedValue({ data: { data: overview } } as any)
    vi.mocked(activity.listActivityV2Repositories).mockResolvedValue({ data: { data: { items: [] } } } as any)
    vi.mocked(activity.listActivityV2PullRequests).mockResolvedValue({ data: { data: { items: [] } } } as any)
  })

  it('renders team analytics but only identity, department, and formal-v2 availability for each member', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: [
      { path: '/activity/teams/:team_id', component: ActivityTeamView },
      { path: '/activity/members/:user_id', component: { template: '<div />' } },
      { path: '/usage', component: { template: '<div />' } },
      { path: '/user', component: { template: '<div />' } },
      { path: '/work-items', component: { template: '<div />' } },
    ] })
    await router.push('/activity/teams/team-alpha?from=2026-07-14&to=2026-08-12&timezone=UTC')
    await router.isReady()
    const wrapper = mount(ActivityTeamView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(wrapper.find('[data-testid="activity-v2-analytics"]').exists()).toBe(true)
    const member = wrapper.get('[data-testid="activity-member-7"]')
    expect(member.text()).toContain('Alice')
    expect(member.text()).toContain('alice@example.com')
    expect(member.text()).toContain('team-alpha')
    expect(member.text()).toContain('Available')
    expect(member.text()).not.toMatch(/Token|PR|commit|Repository|7|3|9/)
    const activity = await import('@/api/activity')
    expect(activity.getActivityV2TeamMemberAvailability).toHaveBeenCalledWith('team-alpha', {
      from: '2026-07-14', to: '2026-08-12', timezone: 'UTC', user_ids: [7],
    })
  })
})
