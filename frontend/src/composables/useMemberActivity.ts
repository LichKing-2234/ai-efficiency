import { computed, ref, watch } from 'vue'
import type { RouteLocationNormalizedLoaded, Router } from 'vue-router'
import { getActivityMember, getActivitySummary, normalizeMemberActivity } from '@/api/activity'
import type { ActivityMemberResponse, ActivityWindowParams } from '@/types/activity'

type Translate = (key: 'activity.loadFailed') => string

export function useMemberActivity(route: RouteLocationNormalizedLoaded, router: Router, t: Translate) {
  const data = ref<ActivityMemberResponse | null>(null)
  const loading = ref(false)
  const prLoading = ref(false)
  const error = ref('')
  const range = ref<ActivityWindowParams>(initialRange(route))
  const prPageCursors = ref<Array<string | null>>([null])
  const prPageIndex = ref(0)
  let requestSequence = 0

  const targetUserID = computed(() => {
    const value = Number(route.params.user_id)
    return Number.isInteger(value) && value > 0 ? value : null
  })

  function request(params: ActivityWindowParams & {
    pr_limit: number
    pr_cursor?: string
    commit_limit: number
    bucket_limit: number
  }) {
    return targetUserID.value == null
      ? getActivitySummary(params)
      : getActivityMember(targetUserID.value, params)
  }

  async function loadActivity() {
    const sequence = ++requestSequence
    loading.value = true
    prLoading.value = false
    error.value = ''
    try {
      const response = await request({ ...range.value, pr_limit: 20, commit_limit: 20, bucket_limit: 20 })
      if (sequence !== requestSequence) return
      if (!response.data.data) throw new Error('Activity response is empty')
      data.value = normalizeMemberActivity(response.data.data)
      resetPRPagination()
    } catch (cause) {
      if (sequence !== requestSequence) return
      console.error(cause)
      error.value = t('activity.loadFailed')
    } finally {
      if (sequence === requestSequence) loading.value = false
    }
  }

  function resetPRPagination() {
    prPageCursors.value = [null]
    prPageIndex.value = 0
  }

  async function loadPRPage(cursor: string | null, targetPageIndex: number) {
    if (prLoading.value) return
    const sequence = ++requestSequence
    prLoading.value = true
    error.value = ''
    try {
      const response = await request({
        ...range.value,
        pr_limit: 20,
        ...(cursor ? { pr_cursor: cursor } : {}),
        commit_limit: 20,
        bucket_limit: 20,
      })
      if (sequence !== requestSequence) return
      if (!response.data.data || !data.value) throw new Error('Activity PR page is empty')
      data.value = { ...data.value, prs: normalizeMemberActivity(response.data.data).prs }
      prPageIndex.value = targetPageIndex
    } catch (cause) {
      if (sequence !== requestSequence) return
      console.error(cause)
      error.value = t('activity.loadFailed')
    } finally {
      if (sequence === requestSequence) prLoading.value = false
    }
  }

  function loadNextPRPage() {
    const cursor = data.value?.prs.next_cursor
    if (!cursor || prLoading.value) return
    const targetPageIndex = prPageIndex.value + 1
    prPageCursors.value[targetPageIndex] = cursor
    void loadPRPage(cursor, targetPageIndex)
  }

  function loadPreviousPRPage() {
    if (prPageIndex.value <= 0 || prLoading.value) return
    const targetPageIndex = prPageIndex.value - 1
    void loadPRPage(prPageCursors.value[targetPageIndex] ?? null, targetPageIndex)
  }

  function selectRange(next: ActivityWindowParams) {
    range.value = next
    void router.replace({ query: { ...route.query, from: next.from, to: next.to } })
    void loadActivity()
  }

  watch(targetUserID, () => {
    data.value = null
    resetPRPagination()
    void loadActivity()
  }, { immediate: true })

  return {
    data,
    loading,
    prLoading,
    error,
    range,
    prPageIndex,
    loadActivity,
    loadNextPRPage,
    loadPreviousPRPage,
    selectRange,
  }
}

function initialRange(route: RouteLocationNormalizedLoaded): ActivityWindowParams {
  const from = typeof route.query.from === 'string' ? route.query.from : undefined
  const to = typeof route.query.to === 'string' ? route.query.to : undefined
  if (from && to) return { from, to }
  const rangeTo = new Date()
  const rangeFrom = new Date(rangeTo.getTime() - 30 * 24 * 60 * 60 * 1000)
  return { from: rangeFrom.toISOString(), to: rangeTo.toISOString() }
}
