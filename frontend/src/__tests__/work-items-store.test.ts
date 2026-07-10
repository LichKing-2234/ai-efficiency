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
})
