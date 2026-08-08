import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { getWorkItemCounts } from '@/api/workItems'
import { registerSessionResourceReset } from '@/stores/sessionResources'
import type { WorkItemCounts } from '@/types'

const countsFreshnessMs = 20_000

const emptyCounts: WorkItemCounts = {
  quota_reset_approval_count: 0,
  quota_reset_admin_count: 0,
  ai_access_setup_count: 0,
  offboarding_count: 0,
  total_count: 0,
}

function normalizeCounts(data?: Partial<WorkItemCounts> | null): WorkItemCounts {
  return {
    quota_reset_approval_count: Number(data?.quota_reset_approval_count ?? 0),
    quota_reset_admin_count: Number(data?.quota_reset_admin_count ?? 0),
    ai_access_setup_count: Number(data?.ai_access_setup_count ?? 0),
    offboarding_count: Number(data?.offboarding_count ?? 0),
    total_count: Number(data?.total_count ?? 0),
  }
}

export function formatWorkItemCount(count: number) {
  return count > 99 ? '99+' : String(Math.max(0, count))
}

export const useWorkItemsStore = defineStore('workItems', () => {
  const counts = ref<WorkItemCounts>({ ...emptyCounts })
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref('')
  let freshUntil = 0
  let sessionGeneration = 0
  let freshnessGeneration = 0
  let nextRequestId = 0

  interface ActiveRequest {
    id: number
    sessionGeneration: number
    freshnessGeneration: number
    promise: Promise<void>
  }

  interface QueuedForce {
    afterRequestId: number
    sessionGeneration: number
    freshnessGeneration: number
    started: boolean
    promise: Promise<void>
    resolve: () => void
  }

  let activeRequest: ActiveRequest | null = null
  let queuedForce: QueuedForce | null = null

  const totalCount = computed(() => counts.value.total_count)
  const badgeLabel = computed(() => formatWorkItemCount(totalCount.value))

  function isCurrentRequest(request: ActiveRequest) {
    return activeRequest === request
      && request.sessionGeneration === sessionGeneration
      && request.freshnessGeneration === freshnessGeneration
  }

  function clearQueuedForce() {
    const queued = queuedForce
    queuedForce = null
    queued?.resolve()
  }

  function finishRequest(request: ActiveRequest) {
    if (activeRequest === request) {
      activeRequest = null
      loading.value = false
    }

    const queued = queuedForce
    if (!queued || queued.started || queued.afterRequestId !== request.id) return
    if (queued.sessionGeneration !== sessionGeneration || queued.freshnessGeneration !== freshnessGeneration) {
      clearQueuedForce()
      return
    }

    queued.started = true
    const followUp = startRequest()
    void followUp.then(() => {
      if (queuedForce === queued) queuedForce = null
      queued.resolve()
    })
  }

  async function runRequest(request: ActiveRequest) {
    try {
      const res = await getWorkItemCounts()
      if (!isCurrentRequest(request)) return
      counts.value = normalizeCounts(res.data.data)
      loaded.value = true
      freshUntil = Date.now() + countsFreshnessMs
    } catch {
      if (!isCurrentRequest(request)) return
      error.value = 'failed'
      if (!loaded.value) {
        counts.value = { ...emptyCounts }
      }
    } finally {
      finishRequest(request)
    }
  }

  function startRequest() {
    loading.value = true
    error.value = ''
    const request: ActiveRequest = {
      id: ++nextRequestId,
      sessionGeneration,
      freshnessGeneration,
      promise: Promise.resolve(),
    }
    activeRequest = request
    request.promise = runRequest(request)
    return request.promise
  }

  function queueForcedFollowUp(request: ActiveRequest) {
    if (queuedForce
      && queuedForce.sessionGeneration === sessionGeneration
      && queuedForce.freshnessGeneration === freshnessGeneration) {
      return queuedForce.promise
    }

    let resolve!: () => void
    const promise = new Promise<void>((done) => {
      resolve = done
    })
    queuedForce = {
      afterRequestId: request.id,
      sessionGeneration,
      freshnessGeneration,
      started: false,
      promise,
      resolve,
    }
    return promise
  }

  function loadCounts(options: { force?: boolean } = {}): Promise<void> {
    if (!options.force && loaded.value && Date.now() < freshUntil) {
      return Promise.resolve()
    }
    if (activeRequest) {
      return options.force ? queueForcedFollowUp(activeRequest) : activeRequest.promise
    }
    return startRequest()
  }

  function invalidateCounts() {
    freshnessGeneration += 1
    freshUntil = 0
    activeRequest = null
    loading.value = false
    clearQueuedForce()
  }

  function resetCounts() {
    sessionGeneration += 1
    freshnessGeneration += 1
    freshUntil = 0
    activeRequest = null
    clearQueuedForce()
    counts.value = { ...emptyCounts }
    loading.value = false
    loaded.value = false
    error.value = ''
  }

  registerSessionResourceReset('workItems', resetCounts)

  return { counts, loading, loaded, error, totalCount, badgeLabel, loadCounts, invalidateCounts, resetCounts }
})
