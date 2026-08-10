import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import ActivityTeamsView from '@/views/activity/ActivityTeamsView.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/activity', () => ({
  getActivityScope: vi.fn(),
  listActivityMembers: vi.fn(),
  normalizeScope: (value: any) => ({ ...value, teams: value.teams ?? [] }),
}))

async function mountView() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/activity/teams', component: ActivityTeamsView },
      { path: '/activity/teams/:team_id', component: { template: '<div />' } },
    ],
  })
  await router.push('/activity/teams')
  await router.isReady()
  const wrapper = mount(ActivityTeamsView, {
    global: {
      plugins: [router],
      stubs: { AppLayout: { template: '<main><slot /></main>' } },
    },
  })
  await flushPromises()
  return { wrapper, router }
}

describe('ActivityTeamsView', () => {
  beforeEach(async () => {
    await setLocale('en-US')
    vi.clearAllMocks()
  })

  it('uses an Element Plus error alert and retry button without changing reload behavior', async () => {
    const api = await import('@/api/activity')
    vi.mocked(api.getActivityScope)
      .mockRejectedValueOnce(new Error('teams unavailable'))
      .mockResolvedValueOnce({
        data: {
          data: {
            contract_version: 'activity-v1',
            scope_version: 'scope-1',
            can_view_teams: true,
            admin: false,
            representative: true,
            teams: [{ external_id: 'team-alpha', name: 'Team Alpha', display_path: 'Engineering / Team Alpha', member_count: 8 }],
          },
        },
      } as any)
    const { wrapper } = await mountView()

    const alert = wrapper.get('[role="alert"]')
    expect(alert.classes()).toContain('el-alert')
    const retry = wrapper.findAll('button').find((button) => button.text() === 'Retry')
    expect(retry?.classes()).toContain('el-button')

    await retry!.trigger('click')
    await flushPromises()
    expect(api.getActivityScope).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('Team Alpha')
  })

  it('renders only teams returned by the authorized scope without loading members', async () => {
    const api = await import('@/api/activity')
    vi.mocked(api.getActivityScope).mockResolvedValue({
      data: {
        data: {
          contract_version: 'activity-v1',
          scope_version: 'scope-1',
          can_view_teams: true,
          admin: false,
          representative: true,
          teams: [
            { external_id: 'team-alpha', name: 'Team Alpha', display_path: 'Engineering / Team Alpha', member_count: 8 },
            { external_id: 'team-beta', name: 'Team Beta', display_path: 'Engineering / Team Beta', member_count: 5 },
          ],
        },
      },
    } as any)

    const { wrapper, router } = await mountView()

    expect(api.getActivityScope).toHaveBeenCalledOnce()
    expect(api.listActivityMembers).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="activity-team-team-alpha"]').text()).toContain('Team Alpha')
    expect(wrapper.get('[data-testid="activity-team-team-alpha"]').text()).toContain('8 members')
    expect(wrapper.get('[data-testid="activity-team-team-beta"]').attributes('href')).toBe('/activity/teams/team-beta')
    expect(wrapper.text()).not.toContain('Token')
    expect(wrapper.text()).not.toContain('Rank')
    expect(wrapper.text()).not.toContain('Score')

    await wrapper.get('[data-testid="activity-team-team-beta"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/activity/teams/team-beta')
  })

  it('keeps child teams collapsed until the shared department toggle expands their parent', async () => {
    const api = await import('@/api/activity')
    vi.mocked(api.getActivityScope).mockResolvedValue({
      data: {
        data: {
          contract_version: 'activity-v1',
          scope_version: 'scope-1',
          can_view_teams: true,
          admin: false,
          representative: true,
          teams: [
            { external_id: 'engineering', name: 'Engineering', display_path: 'Engineering', member_count: 13 },
            { external_id: 'team-alpha', parent_external_id: 'engineering', name: 'Team Alpha', display_path: 'Engineering / Team Alpha', member_count: 8 },
            { external_id: 'team-beta', parent_external_id: 'engineering', name: 'Team Beta', display_path: 'Engineering / Team Beta', member_count: 5 },
          ],
        },
      },
    } as any)

    const { wrapper } = await mountView()

    expect(wrapper.get('[data-testid="activity-team-engineering"]').text()).toContain('Engineering')
    expect(wrapper.find('[data-testid="activity-team-team-alpha"]').exists()).toBe(false)
    await wrapper.get('[data-testid="activity-team-toggle-engineering"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="activity-team-team-alpha"]').attributes('href')).toBe('/activity/teams/team-alpha')
    expect(wrapper.get('[data-testid="activity-team-team-beta"]').attributes('href')).toBe('/activity/teams/team-beta')
    expect(api.listActivityMembers).not.toHaveBeenCalled()
  })
})
