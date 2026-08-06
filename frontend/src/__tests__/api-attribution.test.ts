import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  default: { get: vi.fn() },
}))

import client from '@/api/client'
import { getAttributionReport } from '@/api/attribution'

const mockClient = client as unknown as { get: ReturnType<typeof vi.fn> }

describe('attribution API', () => {
  beforeEach(() => vi.clearAllMocks())

  it('loads the compact report with an explicit time range', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { repositories: [] } } })
    const params = { from: '2026-08-01T00:00:00Z', to: '2026-08-08T00:00:00Z' }
    await getAttributionReport(params)
    expect(mockClient.get).toHaveBeenCalledWith('/attribution/report', { params })
  })
})
