import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { getActivityV2TeamMemberAvailability } from '@/api/activity'
import { getTeamUsageOrganization } from '@/api/teamUsage'
import type { TeamOverviewMember } from '@/types'
import type { ActivityTeamMemberRow, ActivityTeamMembers } from '@/types/activity'

function queryString(value: unknown) {
  if (Array.isArray(value)) return typeof value[0] === 'string' ? value[0] : ''
  return typeof value === 'string' ? value : ''
}

function localDate(value = new Date()) {
  const year = value.getFullYear()
  const month = String(value.getMonth() + 1).padStart(2, '0')
  const day = String(value.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function isSnapshotExpired(error: unknown) {
  if (typeof error !== 'object' || error == null) return false
  const response = (error as { response?: { status?: number; data?: { message?: string } } }).response
  return response?.status === 409 && response.data?.message === 'snapshot_expired'
}

function memberRow(member: TeamOverviewMember, availableUserIDs: Set<number>): ActivityTeamMemberRow {
  const departmentIDs = member.department_external_ids?.length
    ? member.department_external_ids
    : member.department_external_id ? [member.department_external_id] : []
  return {
    member: {
      user_id: member.user_id,
      directory_member_external_id: member.directory_member_external_id,
      display_name: member.display_name,
      email: member.email,
      department_external_ids: departmentIDs,
    },
    available: member.user_id > 0 && availableUserIDs.has(member.user_id),
  }
}

export function useActivityTeam() {
  const route = useRoute()
  const team = ref<ActivityTeamMembers | null>(null)
  const loading = ref(false)
  const memberLoading = ref(false)
  const error = ref(false)
  const memberPageCursors = ref<Array<string | null>>([null])
  const memberPageIndex = ref(0)
  const browserTimezone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  let requestSequence = 0

  const teamID = computed(() => typeof route.params.team_id === 'string' ? route.params.team_id : '')

  function selectedWindow() {
    const fromQuery = queryString(route.query.from)
    const toQuery = queryString(route.query.to)
    if (/^\d{4}-\d{2}-\d{2}$/.test(fromQuery) && /^\d{4}-\d{2}-\d{2}$/.test(toQuery)) {
      return { from: fromQuery, to: toQuery, timezone: queryString(route.query.timezone) || browserTimezone }
    }
    const to = new Date()
    const from = new Date(to.getFullYear(), to.getMonth(), to.getDate())
    from.setDate(from.getDate() - 29)
    return { from: localDate(from), to: localDate(to), timezone: queryString(route.query.timezone) || browserTimezone }
  }

  function resetMemberPagination() {
    memberPageCursors.value = [null]
    memberPageIndex.value = 0
  }

  async function loadMemberPage(cursor: string | null, targetPageIndex: number, replace = false) {
    if (!replace && memberLoading.value) return
    const sequence = ++requestSequence
    if (replace) loading.value = true
    else memberLoading.value = true
    error.value = false
    const window = selectedWindow()
    try {
      const organizationResponse = await getTeamUsageOrganization({
        start_date: window.from,
        end_date: window.to,
        timezone: window.timezone,
        parent_department_external_id: teamID.value,
        department_limit: 1,
        member_limit: 50,
        member_cursor: cursor || undefined,
      })
      if (sequence !== requestSequence) return
      const organization = organizationResponse.data.data
      if (!organization) throw new Error('Activity team members response is empty')
      const userIDs = Array.from(new Set((organization.members ?? []).map((member) => member.user_id).filter((userID) => userID > 0)))
      const availabilityResponse = await getActivityV2TeamMemberAvailability(teamID.value, { ...window, user_ids: userIDs })
      if (sequence !== requestSequence) return
      const availability = availabilityResponse.data.data
      if (!availability) throw new Error('Activity team availability response is empty')
      const availableUserIDs = new Set(availability.available_user_ids ?? [])
      team.value = {
        contract_version: availability.contract_version,
        scope_version: availability.scope_version,
        team: availability.team,
        members: {
          items: (organization.members ?? []).map((member) => memberRow(member, availableUserIDs)),
          next_cursor: organization.next_member_cursor,
        },
      }
      memberPageIndex.value = targetPageIndex
    } catch (cause) {
      if (sequence !== requestSequence) return
      if (!replace && isSnapshotExpired(cause)) {
        memberLoading.value = false
        resetMemberPagination()
        await load()
        return
      }
      console.error(cause)
      error.value = true
    } finally {
      if (sequence === requestSequence) {
        loading.value = false
        memberLoading.value = false
      }
    }
  }

  async function load() {
    resetMemberPagination()
    await loadMemberPage(null, 0, true)
  }

  function loadNextMembers() {
    const cursor = team.value?.members.next_cursor
    if (!cursor || memberLoading.value) return
    const targetPageIndex = memberPageIndex.value + 1
    memberPageCursors.value[targetPageIndex] = cursor
    void loadMemberPage(cursor, targetPageIndex)
  }

  function loadPreviousMembers() {
    if (memberPageIndex.value <= 0 || memberLoading.value) return
    const targetPageIndex = memberPageIndex.value - 1
    void loadMemberPage(memberPageCursors.value[targetPageIndex] ?? null, targetPageIndex)
  }

  watch(
    [teamID, () => route.query.from, () => route.query.to, () => route.query.timezone],
    () => {
      team.value = null
      void load()
    },
    { immediate: true },
  )

  return {
    team,
    loading,
    memberLoading,
    error,
    memberPageIndex,
    load,
    loadNextMembers,
    loadPreviousMembers,
  }
}
