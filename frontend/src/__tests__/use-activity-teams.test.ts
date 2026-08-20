import { defineComponent, nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useActivityTeams } from '@/composables/useActivityTeams'

vi.mock('@/api/teamUsage', () => ({ getTeamUsageOrganization: vi.fn() }))

function department(id: string, name: string, parent?: string, hasChildren = false) {
  return {
    department_external_id: id,
    parent_external_id: parent,
    name,
    display_path: parent ? `Engineering / ${name}` : name,
    depth: parent ? 1 : 0,
    child_count: hasChildren ? 1 : 0,
    has_children: hasChildren,
    direct_member_count: 0,
    aggregate_member_count: hasChildren ? 8 : 5,
    connected_member_count: 0,
    range_actual_cost: 0,
  }
}

describe('useActivityTeams', () => {
  beforeEach(() => vi.clearAllMocks())

  it('loads authorized Team Usage organization branches on demand', async () => {
    const api = await import('@/api/teamUsage')
    vi.mocked(api.getTeamUsageOrganization)
      .mockResolvedValueOnce({ data: { data: { departments: [department('engineering', 'Engineering', undefined, true)], members: [] } } } as any)
      .mockResolvedValueOnce({ data: { data: { departments: [department('team-alpha', 'Team Alpha', 'engineering')], members: [] } } } as any)

    let activityTeams!: ReturnType<typeof useActivityTeams>
    const wrapper = mount(defineComponent({
      setup() {
        activityTeams = useActivityTeams()
        return () => null
      },
    }))
    await flushPromises()

    expect(activityTeams.rootBranch.value?.departments.map((team) => team.department_external_id)).toEqual(['engineering'])
    expect(activityTeams.branchFor('engineering')).toBeUndefined()

    activityTeams.ensureBranch('engineering')
    await flushPromises()
    await nextTick()

    expect(activityTeams.branchFor('engineering')?.departments.map((team) => team.department_external_id)).toEqual(['team-alpha'])
    expect(api.getTeamUsageOrganization).toHaveBeenNthCalledWith(2, expect.objectContaining({ parent_department_external_id: 'engineering' }))
    wrapper.unmount()
  })
})
