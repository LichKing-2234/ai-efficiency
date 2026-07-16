import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useWorkItemsStore } from '@/stores/workItems'

vi.mock('@/api/workItems', () => ({
  getWorkItemCounts: vi.fn(),
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

function countsResponse(total: number) {
  return {
    data: {
      data: {
        quota_reset_approval_count: total,
        quota_reset_admin_count: 0,
        ai_access_setup_count: 0,
        offboarding_count: 0,
        total_count: total,
      },
    },
  }
}

describe('work items store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('deduplicates concurrent count loads from the sidebar and work-items page', async () => {
    const api = await import('@/api/workItems') as any
    const pending = deferred<any>()
    api.getWorkItemCounts.mockReturnValue(pending.promise)
    const store = useWorkItemsStore()

    const sidebarLoad = store.loadCounts()
    const pageLoad = store.loadCounts()
    pending.resolve({
      data: {
        data: {
          quota_reset_approval_count: 2,
          quota_reset_admin_count: 0,
          ai_access_setup_count: 0,
          offboarding_count: 0,
          total_count: 2,
        },
      },
    })
    await Promise.all([sidebarLoad, pageLoad])

    expect(api.getWorkItemCounts).toHaveBeenCalledTimes(1)
    expect(store.totalCount).toBe(2)
    expect(store.error).toBe('')
  })

  it('resets counts and ignores a response from the previous authenticated session', async () => {
    const api = await import('@/api/workItems') as any
    const previousSession = deferred<any>()
    api.getWorkItemCounts.mockReturnValueOnce(previousSession.promise)
    const store = useWorkItemsStore()
    const previousLoad = store.loadCounts()

    store.resetCounts()
    api.getWorkItemCounts.mockResolvedValueOnce({
      data: {
        data: {
          quota_reset_approval_count: 1,
          quota_reset_admin_count: 0,
          ai_access_setup_count: 0,
          offboarding_count: 0,
          total_count: 1,
        },
      },
    })
    const currentLoad = store.loadCounts()
    previousSession.resolve({
      data: {
        data: {
          quota_reset_approval_count: 9,
          quota_reset_admin_count: 9,
          ai_access_setup_count: 9,
          offboarding_count: 9,
          total_count: 36,
        },
      },
    })

    await Promise.all([previousLoad, currentLoad])

    expect(store.totalCount).toBe(1)
    expect(store.loaded).toBe(true)
    expect(store.error).toBe('')
  })

  it('queues one fresh request when force loading during an in-flight request', async () => {
    const api = await import('@/api/workItems') as any
    const stale = deferred<any>()
    api.getWorkItemCounts
      .mockReturnValueOnce(stale.promise)
      .mockResolvedValueOnce({
        data: {
          data: {
            quota_reset_approval_count: 0,
            quota_reset_admin_count: 0,
            ai_access_setup_count: 0,
            offboarding_count: 0,
            total_count: 0,
          },
        },
      })
    const store = useWorkItemsStore()
    const initialLoad = store.loadCounts()
    const freshLoad = store.loadCounts({ force: true })
    stale.resolve({
      data: {
        data: {
          quota_reset_approval_count: 1,
          quota_reset_admin_count: 0,
          ai_access_setup_count: 0,
          offboarding_count: 0,
          total_count: 1,
        },
      },
    })

    await Promise.all([initialLoad, freshLoad])

    expect(api.getWorkItemCounts).toHaveBeenCalledTimes(2)
    expect(store.totalCount).toBe(0)
  })

  it('keeps successful counts fresh through 19,999ms and reloads at 20,000ms', async () => {
    vi.useFakeTimers()
    try {
      vi.setSystemTime(1_000)
      const api = await import('@/api/workItems') as any
      const firstResponse = deferred<any>()
      api.getWorkItemCounts
        .mockReturnValueOnce(firstResponse.promise)
        .mockResolvedValueOnce(countsResponse(2))
      const store = useWorkItemsStore()

      const firstLoad = store.loadCounts()
      vi.setSystemTime(5_000)
      firstResponse.resolve(countsResponse(1))
      await firstLoad

      vi.setSystemTime(24_999)
      await store.loadCounts()

      expect(api.getWorkItemCounts).toHaveBeenCalledTimes(1)
      expect(store.totalCount).toBe(1)

      vi.setSystemTime(25_000)
      await store.loadCounts()

      expect(api.getWorkItemCounts).toHaveBeenCalledTimes(2)
      expect(store.totalCount).toBe(2)
    } finally {
      vi.useRealTimers()
    }
  })

  it('shares one forced follow-up and does not queue a third request during it', async () => {
    const api = await import('@/api/workItems') as any
    const initial = deferred<any>()
    const followUp = deferred<any>()
    api.getWorkItemCounts
      .mockReturnValueOnce(initial.promise)
      .mockReturnValueOnce(followUp.promise)
    const store = useWorkItemsStore()

    const initialLoad = store.loadCounts()
    const forcedLoad = store.loadCounts({ force: true })
    const sameForcedLoad = store.loadCounts({ force: true })
    let forcedLoadsSettled = false
    void Promise.all([forcedLoad, sameForcedLoad]).then(() => {
      forcedLoadsSettled = true
    })

    initial.resolve(countsResponse(1))
    await initialLoad
    await Promise.resolve()

    expect(api.getWorkItemCounts).toHaveBeenCalledTimes(2)
    expect(forcedLoadsSettled).toBe(false)

    const forcedDuringFollowUp = store.loadCounts({ force: true })
    let followUpCallerSettled = false
    void forcedDuringFollowUp.then(() => {
      followUpCallerSettled = true
    })
    await Promise.resolve()
    expect(followUpCallerSettled).toBe(false)

    followUp.resolve(countsResponse(2))
    await Promise.all([forcedLoad, sameForcedLoad, forcedDuringFollowUp])

    expect(api.getWorkItemCounts).toHaveBeenCalledTimes(2)
    expect(store.totalCount).toBe(2)
  })

  it('forces a request inside the freshness window', async () => {
    const api = await import('@/api/workItems') as any
    api.getWorkItemCounts
      .mockResolvedValueOnce(countsResponse(1))
      .mockResolvedValueOnce(countsResponse(2))
    const store = useWorkItemsStore()

    await store.loadCounts()
    await store.loadCounts({ force: true })

    expect(api.getWorkItemCounts).toHaveBeenCalledTimes(2)
    expect(store.totalCount).toBe(2)
  })

  it('preserves the badge during invalidation and ignores the superseded response', async () => {
    const api = await import('@/api/workItems') as any
    const superseded = deferred<any>()
    const current = deferred<any>()
    api.getWorkItemCounts
      .mockResolvedValueOnce(countsResponse(1))
      .mockReturnValueOnce(superseded.promise)
      .mockReturnValueOnce(current.promise)
    const store = useWorkItemsStore()
    await store.loadCounts()

    const supersededLoad = store.loadCounts({ force: true })
    store.invalidateCounts()

    expect(store.totalCount).toBe(1)
    expect(store.loaded).toBe(true)

    const currentLoad = store.loadCounts()
    superseded.resolve(countsResponse(9))
    await supersededLoad

    expect(store.totalCount).toBe(1)
    expect(store.loading).toBe(true)

    current.resolve(countsResponse(2))
    await currentLoad

    expect(api.getWorkItemCounts).toHaveBeenCalledTimes(3)
    expect(store.totalCount).toBe(2)
    expect(store.loading).toBe(false)
  })

  it('ignores late rejections from invalidated and reset generations', async () => {
    const api = await import('@/api/workItems') as any
    const invalidated = deferred<any>()
    const currentAfterInvalidation = deferred<any>()
    const previousSession = deferred<any>()
    const currentSession = deferred<any>()
    api.getWorkItemCounts
      .mockResolvedValueOnce(countsResponse(1))
      .mockReturnValueOnce(invalidated.promise)
      .mockReturnValueOnce(currentAfterInvalidation.promise)
      .mockReturnValueOnce(previousSession.promise)
      .mockReturnValueOnce(currentSession.promise)
    const store = useWorkItemsStore()
    await store.loadCounts()

    const invalidatedLoad = store.loadCounts({ force: true })
    store.invalidateCounts()
    const currentLoad = store.loadCounts()
    invalidated.reject(new Error('old invalidated request failed'))
    await invalidatedLoad

    expect(store.error).toBe('')
    expect(store.totalCount).toBe(1)
    expect(store.loading).toBe(true)

    currentAfterInvalidation.resolve(countsResponse(2))
    await currentLoad
    expect(store.totalCount).toBe(2)

    const previousSessionLoad = store.loadCounts({ force: true })
    store.resetCounts()
    const currentSessionLoad = store.loadCounts()
    previousSession.reject(new Error('old session request failed'))
    await previousSessionLoad

    expect(store.error).toBe('')
    expect(store.totalCount).toBe(0)
    expect(store.loading).toBe(true)

    currentSession.resolve(countsResponse(3))
    await currentSessionLoad

    expect(api.getWorkItemCounts).toHaveBeenCalledTimes(5)
    expect(store.error).toBe('')
    expect(store.totalCount).toBe(3)
    expect(store.loading).toBe(false)
  })

  it('reset cancels queued work and prevents the previous session from clearing current loading', async () => {
    const api = await import('@/api/workItems') as any
    const previousSession = deferred<any>()
    const currentSession = deferred<any>()
    api.getWorkItemCounts
      .mockReturnValueOnce(previousSession.promise)
      .mockReturnValueOnce(currentSession.promise)
    const store = useWorkItemsStore()

    const previousLoad = store.loadCounts()
    const queuedPreviousLoad = store.loadCounts({ force: true })
    store.resetCounts()
    await queuedPreviousLoad

    const currentLoad = store.loadCounts()
    previousSession.resolve(countsResponse(9))
    await previousLoad

    expect(store.totalCount).toBe(0)
    expect(store.loading).toBe(true)

    currentSession.resolve(countsResponse(2))
    await currentLoad

    expect(api.getWorkItemCounts).toHaveBeenCalledTimes(2)
    expect(store.totalCount).toBe(2)
    expect(store.loading).toBe(false)
  })

  it('clears freshness on reset and retries a failed normal load', async () => {
    const api = await import('@/api/workItems') as any
    api.getWorkItemCounts
      .mockResolvedValueOnce(countsResponse(1))
      .mockResolvedValueOnce(countsResponse(2))
      .mockRejectedValueOnce(new Error('unavailable'))
      .mockResolvedValueOnce(countsResponse(3))
    const store = useWorkItemsStore()

    await store.loadCounts()
    store.resetCounts()
    await store.loadCounts()
    store.resetCounts()
    await store.loadCounts()
    await store.loadCounts()

    expect(api.getWorkItemCounts).toHaveBeenCalledTimes(4)
    expect(store.totalCount).toBe(3)
    expect(store.error).toBe('')
  })
})
