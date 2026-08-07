import { defineComponent, nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useActivityTeams } from '@/composables/useActivityTeams'

vi.mock('@/api/activity', () => ({
  getActivityScope: vi.fn(),
  normalizeScope: (value: any) => ({ ...value, teams: value.teams ?? [] }),
}))

describe('useActivityTeams', () => {
  beforeEach(() => vi.clearAllMocks())

  it('exposes authorized teams through branch-on-demand organization state', async () => {
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
            { external_id: 'engineering', name: 'Engineering', display_path: 'Engineering', member_count: 8 },
            { external_id: 'team-alpha', parent_external_id: 'engineering', name: 'Team Alpha', display_path: 'Engineering / Team Alpha', member_count: 5 },
          ],
        },
      },
    } as any)

    let activityTeams!: ReturnType<typeof useActivityTeams>
    const wrapper = mount(defineComponent({
      setup() {
        activityTeams = useActivityTeams()
        return () => null
      },
    }))
    await flushPromises()

    expect(activityTeams.rootBranch.value?.departments.map((team) => team.external_id)).toEqual(['engineering'])
    expect(activityTeams.branchFor('engineering')).toBeUndefined()

    activityTeams.ensureBranch('engineering')
    await flushPromises()
    await nextTick()

    expect(activityTeams.branchFor('engineering')?.departments.map((team) => team.external_id)).toEqual(['team-alpha'])
    expect(api.getActivityScope).toHaveBeenCalledOnce()
    wrapper.unmount()
  })
})
