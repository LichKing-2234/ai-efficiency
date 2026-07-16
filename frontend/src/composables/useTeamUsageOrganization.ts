import { computed, ref } from 'vue'
import { getTeamUsageOrganization } from '@/api/teamUsage'
import type {
  TeamOverviewMember,
  TeamUsageOrganizationDepartment,
  TeamUsageOrganizationParams,
  TeamUsageOverviewParams,
} from '@/types'

export interface TeamUsageOrganizationBranchState {
  parentDepartmentExternalID: string | null
  departments: TeamUsageOrganizationDepartment[]
  members: TeamOverviewMember[]
  nextDepartmentCursor?: string
  nextMemberCursor?: string
  loading: boolean
  departmentLoading: boolean
  memberLoading: boolean
  loaded: boolean
  error: boolean
  requestSeq: number
}

type BranchLoadMode = 'replace' | 'departments' | 'members'

const rootBranchKey = '__root__'

function branchKey(parentDepartmentExternalID: string | null) {
  return parentDepartmentExternalID ?? rootBranchKey
}

function emptyBranch(parentDepartmentExternalID: string | null): TeamUsageOrganizationBranchState {
  return {
    parentDepartmentExternalID,
    departments: [],
    members: [],
    loading: false,
    departmentLoading: false,
    memberLoading: false,
    loaded: false,
    error: false,
    requestSeq: 0,
  }
}

function mergeDepartments(
  current: TeamUsageOrganizationDepartment[],
  incoming: TeamUsageOrganizationDepartment[],
) {
  const byID = new Map(current.map((department) => [department.department_external_id, department]))
  for (const department of incoming) byID.set(department.department_external_id, department)
  return Array.from(byID.values())
}

function memberKey(member: TeamOverviewMember) {
  return member.user_id > 0
    ? `user:${member.user_id}`
    : `directory:${member.directory_member_external_id || member.email}`
}

function mergeMembers(current: TeamOverviewMember[], incoming: TeamOverviewMember[]) {
  const byID = new Map(current.map((member) => [memberKey(member), member]))
  for (const member of incoming) byID.set(memberKey(member), member)
  return Array.from(byID.values()).sort((left, right) => (left.rank ?? Number.MAX_SAFE_INTEGER) - (right.rank ?? Number.MAX_SAFE_INTEGER))
}

function isSnapshotExpired(error: unknown) {
  if (typeof error !== 'object' || error == null) return false
  const response = (error as { response?: { status?: number; data?: { message?: string } } }).response
  return response?.status === 409 && response.data?.message === 'snapshot_expired'
}

export function useTeamUsageOrganization() {
  const branches = ref<Record<string, TeamUsageOrganizationBranchState>>({})
  const resetVersion = ref(0)
  let fixedParams: TeamUsageOverviewParams | null = null
  let generation = 0

  const rootBranch = computed(() => branches.value[rootBranchKey])

  function branchFor(parentDepartmentExternalID: string) {
    return branches.value[branchKey(parentDepartmentExternalID)]
  }

  function writeBranch(key: string, branch: TeamUsageOrganizationBranchState) {
    branches.value = { ...branches.value, [key]: branch }
  }

  async function loadBranch(
    parentDepartmentExternalID: string | null,
    mode: BranchLoadMode,
    recoverSnapshot = true,
    expectedGeneration = generation,
  ): Promise<void> {
    if (fixedParams == null || expectedGeneration !== generation) return
    const key = branchKey(parentDepartmentExternalID)
    const current = branches.value[key] ?? emptyBranch(parentDepartmentExternalID)
    const requestSeq = current.requestSeq + 1
    const next: TeamUsageOrganizationBranchState = {
      ...current,
      requestSeq,
      error: false,
      loading: mode === 'replace',
      departmentLoading: mode === 'departments',
      memberLoading: mode === 'members',
    }
    if (mode === 'replace') {
      next.departments = []
      next.members = []
      next.nextDepartmentCursor = undefined
      next.nextMemberCursor = undefined
      next.loaded = false
    }
    writeBranch(key, next)

    const requestParams: TeamUsageOrganizationParams = {
      ...fixedParams,
      parent_department_external_id: parentDepartmentExternalID ?? undefined,
      department_limit: 25,
      member_limit: 50,
      department_cursor: mode === 'departments' ? current.nextDepartmentCursor : undefined,
      member_cursor: mode === 'members' ? current.nextMemberCursor : undefined,
    }
    try {
      const response = await getTeamUsageOrganization(requestParams)
      if (expectedGeneration !== generation || branches.value[key]?.requestSeq !== requestSeq) return
      const data = response.data.data
      if (data == null) throw new Error('organization response is empty')
      const latest = branches.value[key] ?? next
      if (mode === 'departments') {
        writeBranch(key, {
          ...latest,
          departments: mergeDepartments(latest.departments, data.departments ?? []),
          nextDepartmentCursor: data.next_department_cursor,
          departmentLoading: false,
          loaded: true,
        })
      } else if (mode === 'members') {
        writeBranch(key, {
          ...latest,
          members: mergeMembers(latest.members, data.members ?? []),
          nextMemberCursor: data.next_member_cursor,
          memberLoading: false,
          loaded: true,
        })
      } else {
        writeBranch(key, {
          ...latest,
          departments: data.departments ?? [],
          members: data.members ?? [],
          nextDepartmentCursor: data.next_department_cursor,
          nextMemberCursor: data.next_member_cursor,
          loading: false,
          loaded: true,
        })
      }
    } catch (error) {
      if (expectedGeneration !== generation || branches.value[key]?.requestSeq !== requestSeq) return
      if (recoverSnapshot && mode !== 'replace' && isSnapshotExpired(error)) {
        return loadBranch(parentDepartmentExternalID, 'replace', false, expectedGeneration)
      }
      const latest = branches.value[key] ?? next
      writeBranch(key, {
        ...latest,
        loading: false,
        departmentLoading: false,
        memberLoading: false,
        error: true,
      })
    } finally {
      const latest = branches.value[key]
      if (expectedGeneration === generation && latest?.requestSeq === requestSeq) {
        writeBranch(key, {
          ...latest,
          loading: false,
          departmentLoading: false,
          memberLoading: false,
        })
      }
    }
  }

  function reset(params: TeamUsageOverviewParams) {
    generation += 1
    resetVersion.value += 1
    fixedParams = { ...params }
    branches.value = {}
    void loadBranch(null, 'replace', true, generation)
  }

  function ensureBranch(parentDepartmentExternalID: string) {
    const branch = branchFor(parentDepartmentExternalID)
    if (branch?.loaded || branch?.loading) return
    void loadBranch(parentDepartmentExternalID, 'replace')
  }

  function loadMoreDepartments(parentDepartmentExternalID: string | null) {
    const branch = branches.value[branchKey(parentDepartmentExternalID)]
    if (!branch?.nextDepartmentCursor || branch.loading || branch.departmentLoading || branch.memberLoading) return
    void loadBranch(parentDepartmentExternalID, 'departments')
  }

  function loadMoreMembers(parentDepartmentExternalID: string) {
    const branch = branchFor(parentDepartmentExternalID)
    if (!branch?.nextMemberCursor || branch.loading || branch.departmentLoading || branch.memberLoading) return
    void loadBranch(parentDepartmentExternalID, 'members')
  }

  return {
    branches,
    rootBranch,
    resetVersion,
    branchFor,
    reset,
    ensureBranch,
    loadMoreDepartments,
    loadMoreMembers,
  }
}
