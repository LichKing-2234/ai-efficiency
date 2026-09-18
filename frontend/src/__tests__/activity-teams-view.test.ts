import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import ActivityTeamsView from '@/views/activity/ActivityTeamsView.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/teamUsage', () => ({ getTeamUsageOrganization: vi.fn() }))

function department(id: string, name: string, parent?: string, memberCount = 5, hasChildren = false) {
  return {
    department_external_id: id,
    parent_external_id: parent,
    name,
    display_path: parent ? `Engineering / ${name}` : name,
    depth: parent ? 1 : 0,
    child_count: hasChildren ? 2 : 0,
    has_children: hasChildren,
    direct_member_count: memberCount,
    aggregate_member_count: memberCount,
    connected_member_count: memberCount,
    range_actual_cost: 0,
  }
}

function organizationPage(departments: ReturnType<typeof department>[], nextDepartmentCursor?: string) {
  return { data: { data: { departments, members: [], next_department_cursor: nextDepartmentCursor } } } as any
}

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
    const api = await import('@/api/teamUsage')
    vi.mocked(api.getTeamUsageOrganization)
      .mockRejectedValueOnce(new Error('teams unavailable'))
      .mockResolvedValueOnce(organizationPage([department('team-alpha', 'Team Alpha', undefined, 8)]))
    const { wrapper } = await mountView()

    const alert = wrapper.get('[role="alert"]')
    expect(alert.classes()).toContain('el-alert')
    const retry = wrapper.findAll('button').find((button) => button.text() === 'Retry')
    expect(retry?.classes()).toContain('el-button')

    await retry!.trigger('click')
    await flushPromises()
    expect(api.getTeamUsageOrganization).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('Team Alpha')
  })

  it('renders only departments returned by Team Usage organization', async () => {
    const api = await import('@/api/teamUsage')
    vi.mocked(api.getTeamUsageOrganization).mockResolvedValue(organizationPage([
      department('team-alpha', 'Team Alpha', undefined, 8),
      department('team-beta', 'Team Beta', undefined, 5),
    ]))

    const { wrapper, router } = await mountView()

    expect(api.getTeamUsageOrganization).toHaveBeenCalledOnce()
    expect(wrapper.get('[data-testid="activity-team-team-alpha"]').text()).toContain('Team Alpha')
    expect(wrapper.get('[data-testid="activity-team-team-alpha"]').text()).toContain('8 members')
    expect(wrapper.get('[data-testid="activity-team-team-beta"]').attributes('href')).toBe('/activity/teams/team-beta')
    expect(wrapper.text()).not.toContain('Token')
    expect(wrapper.text()).not.toContain('Rank')

    await wrapper.get('[data-testid="activity-team-team-beta"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/activity/teams/team-beta')
  })

  it('loads child departments only after their parent expands', async () => {
    const api = await import('@/api/teamUsage')
    vi.mocked(api.getTeamUsageOrganization)
      .mockResolvedValueOnce(organizationPage([department('engineering', 'Engineering', undefined, 13, true)]))
      .mockResolvedValueOnce(organizationPage([
        department('team-alpha', 'Team Alpha', 'engineering', 8),
        department('team-beta', 'Team Beta', 'engineering', 5),
      ]))

    const { wrapper } = await mountView()

    expect(wrapper.find('[data-testid="activity-team-team-alpha"]').exists()).toBe(false)
    await wrapper.get('[data-testid="activity-team-toggle-engineering"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="activity-team-team-alpha"]').attributes('href')).toBe('/activity/teams/team-alpha')
    expect(wrapper.get('[data-testid="activity-team-team-beta"]').attributes('href')).toBe('/activity/teams/team-beta')
    expect(api.getTeamUsageOrganization).toHaveBeenNthCalledWith(2, expect.objectContaining({ parent_department_external_id: 'engineering' }))
  })

  it('keeps root teams visible and retries a failed incremental page in place', async () => {
    const api = await import('@/api/teamUsage')
    vi.mocked(api.getTeamUsageOrganization)
      .mockResolvedValueOnce(organizationPage([department('engineering', 'Engineering')], 'root-page-2'))
      .mockRejectedValueOnce(new Error('synthetic root page failure'))
      .mockResolvedValueOnce(organizationPage([department('product', 'Product')]))

    const { wrapper } = await mountView()
    await wrapper.get('[data-testid="activity-teams-more-root"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Engineering')
    expect(wrapper.get('[data-testid="activity-teams-error-root"]').text()).toContain('Teams are temporarily unavailable.')
    await wrapper.get('[data-testid="activity-teams-retry-root"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Engineering')
    expect(wrapper.text()).toContain('Product')
    expect(api.getTeamUsageOrganization).toHaveBeenNthCalledWith(3, expect.objectContaining({ department_cursor: 'root-page-2' }))
  })

  it('keeps child teams visible and retries a failed branch page in place', async () => {
    const api = await import('@/api/teamUsage')
    vi.mocked(api.getTeamUsageOrganization)
      .mockResolvedValueOnce(organizationPage([department('engineering', 'Engineering', undefined, 13, true)]))
      .mockResolvedValueOnce(organizationPage([department('team-alpha', 'Team Alpha', 'engineering')], 'engineering-page-2'))
      .mockRejectedValueOnce(new Error('synthetic branch page failure'))
      .mockResolvedValueOnce(organizationPage([department('team-beta', 'Team Beta', 'engineering')]))

    const { wrapper } = await mountView()
    await wrapper.get('[data-testid="activity-team-toggle-engineering"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="activity-teams-more-engineering"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Team Alpha')
    expect(wrapper.get('[data-testid="activity-teams-error-engineering"]').text()).toContain('Teams are temporarily unavailable.')
    await wrapper.get('[data-testid="activity-teams-retry-engineering"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Team Alpha')
    expect(wrapper.text()).toContain('Team Beta')
    expect(api.getTeamUsageOrganization).toHaveBeenNthCalledWith(4, expect.objectContaining({ department_cursor: 'engineering-page-2' }))
  })
})
