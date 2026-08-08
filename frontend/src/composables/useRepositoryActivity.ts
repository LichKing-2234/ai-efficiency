import { ref } from 'vue'
import { getActivityRepository, normalizeRepository } from '@/api/activity'
import type { ActivityRepositoryResponse, ActivityWindowParams } from '@/types/activity'

const DAY_MS = 24 * 60 * 60 * 1000

function rangeForDays(days: number): ActivityWindowParams {
  const to = new Date()
  return { from: new Date(to.getTime() - days * DAY_MS).toISOString(), to: to.toISOString() }
}

export function useRepositoryActivity(repoID: number) {
  const activity = ref<ActivityRepositoryResponse | null>(null)
  const range = ref<ActivityWindowParams>(rangeForDays(30))
  const loading = ref(false)
  const prLoading = ref(false)
  const error = ref(false)
  const prPageCursors = ref<Array<string | null>>([null])
  const prPageIndex = ref(0)
  let requestSequence = 0

  async function load() {
    const sequence = ++requestSequence
    loading.value = true
    prLoading.value = false
    error.value = false
    try {
      const response = await getActivityRepository(repoID, {
        ...range.value,
        member_limit: 50,
        pr_limit: 20,
        commit_limit: 20,
      })
      if (sequence !== requestSequence) return
      if (!response.data.data) throw new Error('Repository Activity response is empty')
      activity.value = normalizeRepository(response.data.data)
      resetPRPagination()
    } catch (cause) {
      if (sequence !== requestSequence) return
      console.error(cause)
      error.value = true
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
    error.value = false
    try {
      const response = await getActivityRepository(repoID, {
        ...range.value,
        member_limit: 50,
        pr_limit: 20,
        ...(cursor ? { pr_cursor: cursor } : {}),
        commit_limit: 20,
      })
      if (sequence !== requestSequence) return
      if (!response.data.data || !activity.value) throw new Error('Repository PR page is empty')
      activity.value = { ...activity.value, prs: normalizeRepository(response.data.data).prs }
      prPageIndex.value = targetPageIndex
    } catch (cause) {
      if (sequence !== requestSequence) return
      console.error(cause)
      error.value = true
    } finally {
      if (sequence === requestSequence) prLoading.value = false
    }
  }

  function loadNextPRPage() {
    const cursor = activity.value?.prs.next_cursor
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
    void load()
  }

  return {
    activity,
    range,
    loading,
    prLoading,
    error,
    prPageIndex,
    load,
    loadNextPRPage,
    loadPreviousPRPage,
    selectRange,
  }
}
