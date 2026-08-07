import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getActivityTeam, normalizeTeam } from '@/api/activity'
import type { ActivityTeamResponse, ActivityWindowParams } from '@/types/activity'

const DAY_MS = 24 * 60 * 60 * 1000

function rangeForDays(days: number): ActivityWindowParams {
  const to = new Date()
  return { from: new Date(to.getTime() - days * DAY_MS).toISOString(), to: to.toISOString() }
}

function isSnapshotExpired(error: unknown) {
  if (typeof error !== 'object' || error == null) return false
  const response = (error as { response?: { status?: number; data?: { message?: string } } }).response
  return response?.status === 409 && response.data?.message === 'snapshot_expired'
}

export function useActivityTeam() {
  const route = useRoute()
  const router = useRouter()
  const team = ref<ActivityTeamResponse | null>(null)
  const loading = ref(false)
  const memberLoading = ref(false)
  const error = ref(false)
  const range = ref<ActivityWindowParams>(initialRange())
  const memberPageCursors = ref<Array<string | null>>([null])
  const memberPageIndex = ref(0)
  let requestSequence = 0

  const teamID = computed(() => typeof route.params.team_id === 'string' ? route.params.team_id : '')

  function initialRange(): ActivityWindowParams {
    const from = typeof route.query.from === 'string' ? route.query.from : undefined
    const to = typeof route.query.to === 'string' ? route.query.to : undefined
    return from && to ? { from, to } : rangeForDays(30)
  }

  async function load() {
    const sequence = ++requestSequence
    loading.value = true
    memberLoading.value = false
    error.value = false
    try {
      const response = await getActivityTeam(teamID.value, { ...range.value, limit: 50 })
      if (sequence !== requestSequence) return
      if (!response.data.data) throw new Error('Activity team response is empty')
      team.value = normalizeTeam(response.data.data)
      resetMemberPagination()
    } catch (cause) {
      if (sequence !== requestSequence) return
      console.error(cause)
      error.value = true
    } finally {
      if (sequence === requestSequence) loading.value = false
    }
  }

  function resetMemberPagination() {
    memberPageCursors.value = [null]
    memberPageIndex.value = 0
  }

  async function loadMemberPage(cursor: string | null, targetPageIndex: number) {
    if (memberLoading.value) return
    const sequence = ++requestSequence
    memberLoading.value = true
    error.value = false
    try {
      const response = await getActivityTeam(teamID.value, {
        ...range.value,
        limit: 50,
        ...(cursor ? { cursor } : {}),
      })
      if (sequence !== requestSequence) return
      if (!response.data.data || !team.value) throw new Error('Activity team members response is empty')
      team.value = { ...team.value, members: normalizeTeam(response.data.data).members }
      memberPageIndex.value = targetPageIndex
    } catch (cause) {
      if (sequence !== requestSequence) return
      if (isSnapshotExpired(cause)) {
        memberLoading.value = false
        resetMemberPagination()
        await load()
        return
      }
      console.error(cause)
      error.value = true
    } finally {
      if (sequence === requestSequence) memberLoading.value = false
    }
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

  function selectRange(next: ActivityWindowParams) {
    range.value = next
    void router.replace({ query: { ...route.query, from: next.from, to: next.to } })
    void load()
  }

  watch(teamID, () => {
    team.value = null
    resetMemberPagination()
    void load()
  }, { immediate: true })

  return {
    team,
    loading,
    memberLoading,
    error,
    range,
    memberPageIndex,
    load,
    selectRange,
    loadNextMembers,
    loadPreviousMembers,
  }
}
