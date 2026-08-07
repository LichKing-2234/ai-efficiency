import { onMounted, ref } from 'vue'
import { getActivityScope, normalizeScope } from '@/api/activity'
import { useOrganizationBranches } from '@/composables/useOrganizationBranches'
import type { ActivityScopeResponse } from '@/types/activity'

export function useActivityTeams() {
  const scope = ref<ActivityScopeResponse | null>(null)
  const loading = ref(false)
  const error = ref(false)
  let requestSequence = 0
  let authorizedTeamIDs = new Set<string>()
  let parentTeamIDs = new Set<string>()

  const browser = useOrganizationBranches<
    ActivityScopeResponse,
    ActivityScopeResponse['teams'][number],
    never
  >({
    async load(context, request) {
      const departments = context.teams.filter((team) => {
        const parentID = team.parent_external_id ?? null
        if (request.parentID != null) return parentID === request.parentID
        return parentID == null || !authorizedTeamIDs.has(parentID)
      })
      return { departments, members: [] }
    },
    departmentKey: (team) => team.external_id,
    memberKey: () => '',
  })

  async function load() {
    const sequence = ++requestSequence
    loading.value = true
    error.value = false
    try {
      const response = await getActivityScope()
      if (sequence !== requestSequence) return
      if (!response.data.data) throw new Error('Activity scope response is empty')
      const nextScope = normalizeScope(response.data.data)
      authorizedTeamIDs = new Set(nextScope.teams.map((team) => team.external_id))
      parentTeamIDs = new Set(
        nextScope.teams
          .map((team) => team.parent_external_id)
          .filter((parentID): parentID is string => (
            typeof parentID === 'string'
            && parentID.length > 0
            && authorizedTeamIDs.has(parentID)
          )),
      )
      scope.value = nextScope
      browser.reset(nextScope)
    } catch (cause) {
      if (sequence !== requestSequence) return
      console.error(cause)
      error.value = true
    } finally {
      if (sequence === requestSequence) loading.value = false
    }
  }

  onMounted(() => void load())

  return {
    scope,
    loading,
    error,
    load,
    rootBranch: browser.rootBranch,
    branchFor: browser.branchFor,
    ensureBranch: browser.ensureBranch,
    hasChildren: (teamID: string) => parentTeamIDs.has(teamID),
  }
}
