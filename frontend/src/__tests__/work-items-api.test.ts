import { describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  default: {
    get: vi.fn(),
  },
}))

describe('work items api', () => {
  it('loads scoped pending work counts', async () => {
    const client = (await import('@/api/client')).default as any
    const api = await import('@/api/workItems')

    api.getWorkItemCounts()

    expect(client.get).toHaveBeenCalledWith('/work-items/counts')
  })
})
