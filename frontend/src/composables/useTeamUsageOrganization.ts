import { getTeamUsageOrganization } from '@/api/teamUsage'
import { useOrganizationBranches, type OrganizationBranchState } from '@/composables/useOrganizationBranches'
import type {
  TeamOverviewMember,
  TeamUsageOrganizationDepartment,
  TeamUsageOverviewParams,
} from '@/types'

export type TeamUsageOrganizationBranchState = OrganizationBranchState<
  TeamUsageOrganizationDepartment,
  TeamOverviewMember
>

function memberKey(member: TeamOverviewMember) {
  return member.user_id > 0
    ? `user:${member.user_id}`
    : `directory:${member.directory_member_external_id || member.email}`
}

export function useTeamUsageOrganization() {
  const browser = useOrganizationBranches<
    TeamUsageOverviewParams,
    TeamUsageOrganizationDepartment,
    TeamOverviewMember
  >({
    async load(params, request) {
      const response = await getTeamUsageOrganization({
        ...params,
        parent_department_external_id: request.parentID ?? undefined,
        department_limit: 25,
        member_limit: 50,
        department_cursor: request.departmentCursor,
        member_cursor: request.memberCursor,
      })
      const data = response.data.data
      if (data == null) throw new Error('organization response is empty')
      return {
        departments: data.departments ?? [],
        members: data.members ?? [],
        nextDepartmentCursor: data.next_department_cursor,
        nextMemberCursor: data.next_member_cursor,
      }
    },
    departmentKey: (department) => department.department_external_id,
    memberKey,
    sortMembers: (left, right) => (left.rank ?? Number.MAX_SAFE_INTEGER) - (right.rank ?? Number.MAX_SAFE_INTEGER),
  })

  return {
    branches: browser.branches,
    rootBranch: browser.rootBranch,
    invalidatedDepartmentIds: browser.invalidatedDepartmentIDs,
    resetVersion: browser.resetVersion,
    branchFor: browser.branchFor,
    reset: browser.reset,
    ensureBranch: browser.ensureBranch,
    loadMoreDepartments: browser.loadMoreDepartments,
    loadMoreMembers: browser.loadMoreMembers,
  }
}
