import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useWorkItemsStore } from '@/stores/workItems'

vi.mock('@/api/workItems', () => ({
  getWorkItemCounts: vi.fn(),
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => {
    resolve = res
  })
  return { promise, resolve }
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
})
